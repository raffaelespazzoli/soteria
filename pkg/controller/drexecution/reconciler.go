/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Tier 2 – Architecture:
// The DRExecution reconciler validates newly created DRExecution resources,
// applies state machine transitions on the referenced DRPlan, sets initial
// execution status, and dispatches the wave executor to orchestrate DRGroup
// execution across waves. Idempotency is two-layered: terminal results
// (Succeeded/Failed) cause an immediate skip, while a set startTime gates
// the setup phase so plan transitions are never repeated on re-reconcile.
// PartiallySucceeded executions are re-openable via the retry annotation
// (soteria.io/retry-groups) — the controller detects the annotation, validates
// preconditions, re-executes failed groups, and removes the annotation.

package drexecution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/engine"
	"github.com/soteria-project/soteria/pkg/metrics"
)

// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumereplications,verbs=get;list;watch
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumegroupreplications,verbs=get;list;watch

// DRExecutionReconciler watches DRExecution resources and drives the DR
// workflow engine. It validates execution requests against the state machine,
// transitions the referenced DRPlan to an in-progress phase, dispatches the
// wave executor, and records the final result. On startup, it detects
// in-progress executions (StartTime != nil, Result == "") and resumes them
// from their last checkpoint.
type DRExecutionReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Recorder         events.EventRecorder
	WaveExecutor     *engine.WaveExecutor
	Handler          engine.DRGroupHandler
	VMManager        engine.VMManager
	ResumeAnalyzer   *engine.ResumeAnalyzer
	ReprotectHandler *engine.ReprotectHandler
	// LocalSite is the --site-name flag value identifying which cluster this
	// controller instance runs on. Used to compute the reconcile role
	// (Owner/Step0/None) for each DRExecution based on the transition phase
	// and the plan's primarySite/secondarySite.
	LocalSite string
	// APIReader bypasses the informer cache, reading directly from the API
	// server (aggregated API → ScyllaDB). Used by the fresh-read terminal
	// guard to close the stale-read window from ScyllaDB CDC eventual
	// consistency. When nil, the guard is skipped (backward compat for tests).
	APIReader client.Reader
	// setupDone tracks executions that have already completed setup in this
	// process lifetime. Prevents redundant setup patches when ScyllaDB's
	// eventual consistency causes fresh reads to miss prior writes, which
	// would otherwise cause a burst of duplicate reconciles that trigger
	// rate-limiter backoff.
	setupDone sync.Map
}

//nolint:gocyclo // Dispatcher with sequential branches for site-aware routing, resume, retry, and wave execution.
func (r *DRExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", req.Name)
	logger.V(1).Info("Reconciling DRExecution")

	var exec soteriav1alpha1.DRExecution
	if err := r.Get(ctx, req.NamespacedName, &exec); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Idempotency: skip if the execution has reached a fully-closed terminal result.
	// Only Succeeded and Failed are final — PartiallySucceeded is re-openable via
	// the retry annotation and must fall through to the retry path below.
	if exec.Status.Result == soteriav1alpha1.ExecutionResultSucceeded ||
		exec.Status.Result == soteriav1alpha1.ExecutionResultFailed {
		r.setupDone.Delete(exec.Name)
		if _, hasRetry := exec.Annotations[engine.RetryGroupsAnnotation]; hasRetry {
			logger.Info("Cleaning stale retry annotation from terminal execution",
				"result", exec.Status.Result)
			r.removeRetryAnnotation(ctx, &exec)
			r.setRetryRejectedCondition(ctx, &exec, fmt.Sprintf(
				"retry not allowed: execution already %s", exec.Status.Result))
		}
		logger.V(1).Info("DRExecution already completed, skipping", "result", exec.Status.Result)
		return ctrl.Result{}, nil
	}

	// Ensure the plan-name label exists before any label-dependent path
	// (concurrency checks, VM event routing). Serves as a backfill for
	// executions created before server-side label stamping was added.
	if err := r.ensurePlanNameLabel(ctx, &exec, req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}

	// Fetch the referenced DRPlan early — needed for site-aware role gating.
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Referenced DRPlan not found", "plan", exec.Spec.PlanName)
			return r.failExecution(ctx, &exec, "PlanNotFound",
				fmt.Sprintf("DRPlan %q not found", exec.Spec.PlanName))
		}
		return ctrl.Result{}, err
	}

	// AC2 (Story 13.7): Fresh-read terminal guard — close the ScyllaDB CDC
	// stale-read window. The informer cache may hold a stale copy (Result="")
	// even after the owner site wrote a terminal result. A direct API server
	// read reflects the latest ScyllaDB state.
	if r.APIReader != nil && !exec.Status.IsTerminal() {
		var fresh soteriav1alpha1.DRExecution
		if err := r.APIReader.Get(ctx, req.NamespacedName, &fresh); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Fresh read returned NotFound, execution deleted")
				return ctrl.Result{}, nil
			}
			logger.V(1).Info("Fresh read failed, proceeding with cached state", "error", err)
		} else {
			if fresh.Status.IsTerminal() {
				logger.V(1).Info("Fresh read reveals terminal execution, skipping stale reconcile",
					"result", fresh.Status.Result)
				return ctrl.Result{}, nil
			}
			exec = fresh
		}
	}

	// Site-aware reconcile ownership: compute the role and dispatch.
	if r.LocalSite != "" {
		if result, done, err := r.dispatchByRole(ctx, &exec, &plan); done || err != nil {
			// For new executions where no site has a valid target (EffectivePhase
			// returns the rest state for unsupported mode/phase combinations),
			// fall through so setup can validate and reject the transition
			// instead of both sites silently skipping.
			effectivePhase := engine.EffectivePhase(plan.Status.Phase, exec.Spec.Mode)
			if exec.Status.StartTime == nil && err == nil && effectivePhase == plan.Status.Phase {
				// Fall through — setup will validate and fail the transition.
			} else {
				return result, err
			}
		}
	} else if plan.Spec.PrimarySite != "" && plan.Spec.SecondarySite != "" {
		// AC1 (Story 13.7): Mandatory --site-name for multi-site deployments.
		// Without LocalSite the controller cannot determine ownership and both
		// sites would race on Status().Update(), causing checkpoint write
		// conflicts (UAT-13.005) and immutability violations (UAT-13.006).
		logger.Error(nil, "LocalSite not configured for multi-site plan, skipping reconciliation",
			"plan", plan.Name, "primarySite", plan.Spec.PrimarySite,
			"secondarySite", plan.Spec.SecondarySite)
		r.event(&exec, corev1.EventTypeWarning, "SiteConfigMissing", "Validation",
			fmt.Sprintf("--site-name not configured; cannot reconcile multi-site plan %s", plan.Name))
		return ctrl.Result{}, nil
	}

	// Resume path: in-progress execution needs resume after restart.
	// StartTime != nil means the controller already dispatched this execution.
	// !IsTerminal() means execution is still in-progress (no persisted outcome yet).
	// The setupDone guard handles ScyllaDB eventual consistency: after
	// reconcileSetup patches StartTime, subsequent reads may still return
	// the old state without StartTime, causing redundant setup loops that
	// trigger rate-limiter backoff.
	_, setupAlreadyDone := r.setupDone.Load(exec.Name)
	if setupAlreadyDone && exec.Status.StartTime == nil && exec.Status.Result == "" {
		// Stale entry: execution was deleted and re-created with the same
		// name. Clear the guard so setup runs for the new instance.
		r.setupDone.Delete(exec.Name)
		setupAlreadyDone = false
	}
	if (exec.Status.StartTime != nil || setupAlreadyDone) && !exec.Status.IsTerminal() {
		if exec.Spec.Mode == soteriav1alpha1.ExecutionModeReprotect {
			return r.reconcileReprotectResume(ctx, &exec)
		}

		// Compatibility: single-site executions upgraded mid-Step 0 with
		// the legacy ResyncPending condition are routed into the
		// health-wait gate so they complete under the new protocol.
		if r.LocalSite == "" && meta.IsStatusConditionTrue(exec.Status.Conditions, "ResyncPending") {
			return r.reconcileResyncGate(ctx, &exec, &plan)
		}

		// Wave progress path: if any wave has WaitingForVMReady groups, drive
		// the readiness state machine instead of re-executing handler operations.
		if hasWaitingForVMReady(&exec) {
			return r.reconcileWaveProgress(ctx, &exec, &plan)
		}

		// Multi-site planned migration: the target site must participate in
		// Step 0 by running reconcileTargetStep0 (inside
		// reconcileWaveExecution). Route there before reconcileResume,
		// which would otherwise block on the Step0Complete wait gate.
		if r.LocalSite != "" && exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration {
			localStatus := getSiteStatus(&exec, r.LocalSite)
			step0Done := localStatus.Step0Complete
			if !step0Done {
				step0Done = meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Complete")
			}
			if !step0Done {
				return r.reconcileWaveExecution(ctx, &exec, &plan)
			}
		}

		// Wave-by-wave continuation: if waves are initialized and no groups
		// are InProgress (no crash recovery needed), continue through the
		// wave execution pipeline so the VM readiness gate is enforced
		// between every pair of waves — not just wave 0.
		if len(exec.Status.Waves) > 0 && !hasInProgressGroups(&exec) {
			return r.reconcileWaveExecution(ctx, &exec, &plan)
		}

		return r.reconcileResume(ctx, &exec)
	}

	// Retry path: PartiallySucceeded + retry annotation.
	if exec.Status.Result == soteriav1alpha1.ExecutionResultPartiallySucceeded {
		return r.reconcileRetry(ctx, &exec)
	}

	// Setup phase: validate, set startTime, transition the plan.
	// Gated on startTime so these steps never repeat on re-reconcile.
	if exec.Status.StartTime == nil {
		return r.reconcileSetup(ctx, &exec, &plan)
	}

	// Re-protect dispatch: storage-only, not wave-based.
	if exec.Spec.Mode == soteriav1alpha1.ExecutionModeReprotect {
		return r.reconcileReprotect(ctx, &exec, &plan)
	}

	// Wave-by-wave execution with VM readiness gates.
	return r.reconcileWaveExecution(ctx, &exec, &plan)
}

// defaultVMReadyTimeout is applied when DRPlan.Spec.VMReadyTimeout is nil.
const defaultVMReadyTimeout = 5 * time.Minute

// vmReadySafetyRequeue is the safety-net poll interval for VM readiness when
// no VM watch event arrives. Ensures progress even with missed watch events.
const vmReadySafetyRequeue = 10 * time.Second

// defaultStep0Timeout is the maximum duration to wait for VR demotion
// health and cross-site Step 0 completion during planned failover.
const defaultStep0Timeout = 10 * time.Minute

// otherSite returns the remote site name given the plan's topology.
// Panics if localSite matches neither — callers guard via dispatchByRole.
func (r *DRExecutionReconciler) otherSite(plan *soteriav1alpha1.DRPlan) string {
	if r.LocalSite == plan.Spec.PrimarySite {
		return plan.Spec.SecondarySite
	}
	return plan.Spec.PrimarySite
}

// getSiteStatus returns the coordination status for a given site.
// Returns the zero value if the site entry does not yet exist.
func getSiteStatus(exec *soteriav1alpha1.DRExecution, site string) soteriav1alpha1.SiteCoordinationStatus {
	if exec.Status.SiteStatuses == nil {
		return soteriav1alpha1.SiteCoordinationStatus{}
	}
	return exec.Status.SiteStatuses[site]
}

// setSiteStatus writes coordination signals to the local site's entry,
// initializing the map if needed. Sets LastUpdated automatically.
func setSiteStatus(exec *soteriav1alpha1.DRExecution, site string, status soteriav1alpha1.SiteCoordinationStatus) {
	if exec.Status.SiteStatuses == nil {
		exec.Status.SiteStatuses = make(map[string]soteriav1alpha1.SiteCoordinationStatus)
	}
	now := metav1.Now()
	status.LastUpdated = &now
	exec.Status.SiteStatuses[site] = status
}

// reconcileWaveExecution drives the wave-by-wave execution pipeline with VM
// readiness gates between waves. On each reconcile it either: (a) initializes
// waves, (b) executes the current wave's handler ops, (c) checks VM readiness
// for a WaitingForVMReady wave, or (d) finishes the execution.
func (r *DRExecutionReconciler) reconcileWaveExecution(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	// Step 0 for multi-site planned migration: the target site Step 0
	// does not require WaveExecutor, so check before the nil guard.
	var step0Done bool
	if r.LocalSite != "" {
		localStatus := getSiteStatus(exec, r.LocalSite)
		step0Done = localStatus.Step0Complete
		if !step0Done {
			step0Done = meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Complete")
		}
		if exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration && !step0Done {
			return r.reconcileTargetStep0(ctx, exec, plan)
		}
	} else {
		step0Done = meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Complete")
	}

	if r.WaveExecutor == nil {
		return ctrl.Result{}, nil
	}

	hdl, err := r.resolveHandler(exec.Spec.Mode)
	if err != nil {
		return r.failExecution(ctx, exec, "HandlerResolutionFailed", err.Error(), plan)
	}

	// Step 0 for planned migration (single-site path): run PreExecute
	// (idempotent: StopVM + StopReplication), then check VRs reached
	// role=Target (state=Secondary), promote, and set Step0Complete via
	// reconcileResyncGate.
	if exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration && !step0Done {
		if ph, ok := hdl.(interface {
			PreExecute(ctx context.Context, groups []engine.ExecutionGroup) error
		}); ok {
			allGroups, err := r.WaveExecutor.BuildExecutionGroups(ctx, plan)
			if err != nil {
				logger.Error(err, "Failed to build execution groups for pre-execution")
				return r.failExecution(ctx, exec, "PreExecutionFailed",
					fmt.Sprintf("building execution groups: %v", err), plan)
			}
			if err := ph.PreExecute(ctx, allGroups); err != nil {
				logger.Error(err, "Pre-execution (Step 0) failed")
				r.event(exec, corev1.EventTypeWarning, "Step0Failed", "PlannedMigration",
					fmt.Sprintf("Step 0 failed: %v", err))
				return r.failExecution(ctx, exec, "PreExecutionFailed",
					fmt.Sprintf("pre-execution failed: %v", err), plan)
			}
		}
		// Anchor the demotion timeout to PreExecute completion so that
		// VM shutdown time does not consume the timeout budget.
		if !meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Started") {
			execPatch := client.MergeFrom(exec.DeepCopy())
			meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
				Type:               "Step0Started",
				Status:             metav1.ConditionTrue,
				Reason:             "PreExecuteCompleted",
				Message:            "Step 0 pre-execution completed, waiting for VRs to reach Secondary",
				ObservedGeneration: exec.Generation,
			})
			if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
				return ctrl.Result{}, err
			}
		}
		return r.reconcileResyncGate(ctx, exec, plan)
	}

	// Initialize waves if not yet done.
	if len(exec.Status.Waves) == 0 {
		if err := r.WaveExecutor.InitializeWaves(ctx, exec, plan); err != nil {
			logger.Error(err, "Wave initialization failed")
			return ctrl.Result{}, err
		}
		// If InitializeWaves already finished (0 VMs), return.
		if exec.Status.IsTerminal() {
			r.recordExecutionMetrics(exec)
			return ctrl.Result{}, nil
		}
	}

	// Find the current wave: first wave with non-terminal groups.
	waveIdx := r.findCurrentWave(exec)
	if waveIdx < 0 {
		return r.finishWaveExecution(ctx, exec, plan)
	}

	wave := &exec.Status.Waves[waveIdx]

	// If this wave has WaitingForVMReady groups, check readiness.
	if waveHasWaitingForVMReady(wave) {
		return r.reconcileWaveProgress(ctx, exec, plan)
	}

	// Execute handler operations for pending groups in this wave.
	r.WaveExecutor.ExecuteWaveHandler(ctx, waveIdx, hdl, exec, plan)

	// Post-process: convert Completed → WaitingForVMReady for failover modes.
	if exec.Spec.Mode != soteriav1alpha1.ExecutionModeReprotect {
		if err := r.convertToWaitingForVMReady(ctx, exec, waveIdx); err != nil {
			logger.Error(err, "Failed to persist WaitingForVMReady transition")
			return ctrl.Result{}, err
		}
	}

	// If the wave now has WaitingForVMReady groups, yield and requeue.
	if waveHasWaitingForVMReady(wave) {
		logger.Info("Wave handler complete, waiting for VM readiness",
			"wave", waveIdx)
		return ctrl.Result{RequeueAfter: vmReadySafetyRequeue}, nil
	}

	// If all groups terminal (no VMs to wait for — e.g., all failed), advance.
	if r.findCurrentWave(exec) < 0 {
		return r.finishWaveExecution(ctx, exec, plan)
	}

	// More waves to process — requeue immediately.
	return ctrl.Result{RequeueAfter: 1 * time.Millisecond}, nil
}

// convertToWaitingForVMReady changes Completed groups in a wave to
// WaitingForVMReady and sets VMReadyStartTime on the wave. Returns an error
// if the status persistence fails so the caller can retry on the next reconcile
// instead of proceeding with an unpersisted state transition.
func (r *DRExecutionReconciler) convertToWaitingForVMReady(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, waveIdx int,
) error {
	wave := &exec.Status.Waves[waveIdx]
	anyConverted := false
	for i := range wave.Groups {
		if wave.Groups[i].Result == soteriav1alpha1.DRGroupResultCompleted {
			wave.Groups[i].Result = soteriav1alpha1.DRGroupResultWaitingForVMReady
			wave.Groups[i].CompletionTime = nil
			anyConverted = true
		}
	}
	if anyConverted {
		now := metav1.Now()
		wave.VMReadyStartTime = &now
		wave.CompletionTime = nil
		if err := r.WaveExecutor.PersistStatus(ctx, exec); err != nil {
			return fmt.Errorf("persisting WaitingForVMReady status for wave %d: %w", waveIdx, err)
		}
	}
	return nil
}

// reconcileWaveProgress checks VM readiness for groups in the WaitingForVMReady
// state. When all VMs in a group are ready, the group transitions to Completed.
// When the timeout expires, the group transitions to Failed with mode-dependent
// policy (AC4): disaster=fail-forward, planned_migration=fail-fast.
func (r *DRExecutionReconciler) reconcileWaveProgress(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	if r.VMManager == nil {
		// No VMManager — treat all WaitingForVMReady as ready.
		r.markAllWaitingAsReady(exec)
		if err := r.WaveExecutor.PersistStatus(ctx, exec); err != nil {
			return ctrl.Result{}, err
		}
		return r.continueAfterWaveReady(ctx, exec, plan)
	}

	timeout := defaultVMReadyTimeout
	if plan.Spec.VMReadyTimeout != nil {
		timeout = plan.Spec.VMReadyTimeout.Duration
	}

	waveIdx := r.findWaveWithWaiting(exec)
	if waveIdx < 0 {
		return r.continueAfterWaveReady(ctx, exec, plan)
	}

	wave := &exec.Status.Waves[waveIdx]
	allReady := true
	anyTimedOut := false

	for i := range wave.Groups {
		group := &wave.Groups[i]
		if group.Result != soteriav1alpha1.DRGroupResultWaitingForVMReady {
			continue
		}

		groupReady := true
		for _, vmName := range group.VMNames {
			ns := r.resolveVMNamespaceFromPlan(plan, waveIdx, vmName)
			if ns == "" {
				logger.Info("Could not resolve namespace for VM, treating as not ready",
					"vm", vmName, "wave", waveIdx)
				groupReady = false
				continue
			}
			ready, err := r.VMManager.IsVMReady(ctx, vmName, ns)
			if err != nil {
				logger.V(1).Info("IsVMReady check failed, will retry",
					"vm", vmName, "namespace", ns, "error", err)
				groupReady = false
				continue
			}
			if !ready {
				groupReady = false
			}
		}

		if groupReady {
			now := metav1.Now()
			group.Result = soteriav1alpha1.DRGroupResultCompleted
			group.CompletionTime = &now
			var duration string
			if wave.VMReadyStartTime != nil {
				duration = now.Sub(wave.VMReadyStartTime.Time).Truncate(time.Second).String()
			}
			for _, vmName := range group.VMNames {
				msg := fmt.Sprintf("VM %s reached Running", vmName)
				if duration != "" {
					msg += " in " + duration
				}
				group.Steps = append(group.Steps, soteriav1alpha1.StepStatus{
					Name:      engine.StepWaitVMReady,
					Status:    "Succeeded",
					Message:   msg,
					Timestamp: &now,
				})
			}
			logger.Info("Group VMs ready", "wave", waveIdx, "group", group.Name)
			continue
		}

		// Check timeout.
		if wave.VMReadyStartTime != nil && time.Since(wave.VMReadyStartTime.Time) > timeout {
			now := metav1.Now()
			group.Result = soteriav1alpha1.DRGroupResultFailed
			group.CompletionTime = &now
			group.Error = "VM did not reach Running state within timeout"
			for _, vmName := range group.VMNames {
				group.Steps = append(group.Steps, soteriav1alpha1.StepStatus{
					Name:      engine.StepWaitVMReady,
					Status:    "Failed",
					Message:   fmt.Sprintf("VM %s did not reach Running within %s", vmName, timeout),
					Timestamp: &now,
				})
			}
			logger.Info("Group VM readiness timed out",
				"wave", waveIdx, "group", group.Name, "timeout", timeout)
			anyTimedOut = true
			continue
		}

		allReady = false
	}

	if err := r.WaveExecutor.PersistStatus(ctx, exec); err != nil {
		return ctrl.Result{}, err
	}

	// Mode-dependent timeout policy (AC4).
	if anyTimedOut && exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration {
		logger.Info("Planned migration fail-fast: aborting execution due to VM readiness timeout")
		return r.failExecution(ctx, exec, "VMReadyTimeout",
			"VM readiness timeout in planned_migration mode — aborting execution", plan)
	}

	if !allReady {
		return ctrl.Result{RequeueAfter: vmReadySafetyRequeue}, nil
	}

	// All groups in the wave are now terminal — set wave completion time.
	now := metav1.Now()
	wave.CompletionTime = &now
	if err := r.WaveExecutor.PersistStatus(ctx, exec); err != nil {
		return ctrl.Result{}, err
	}

	return r.continueAfterWaveReady(ctx, exec, plan)
}

// continueAfterWaveReady checks if more waves remain and either advances to
// the next wave or finishes the execution.
func (r *DRExecutionReconciler) continueAfterWaveReady(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	if r.findCurrentWave(exec) < 0 {
		return r.finishWaveExecution(ctx, exec, plan)
	}
	return ctrl.Result{RequeueAfter: 1 * time.Millisecond}, nil
}

// finishWaveExecution computes the final result, records metrics, and emits events.
func (r *DRExecutionReconciler) finishWaveExecution(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	result := r.WaveExecutor.ComputeResult(exec)
	if err := r.WaveExecutor.FinishExecution(ctx, exec, plan, result, ""); err != nil {
		return ctrl.Result{}, err
	}
	r.recordExecutionMetrics(exec)
	r.event(exec, corev1.EventTypeNormal, "ExecutionCompleted", "WaveExecution",
		fmt.Sprintf("Execution completed: %s", exec.Status.Result))
	r.event(plan, corev1.EventTypeNormal, "ExecutionCompleted", "WaveExecution",
		fmt.Sprintf("Execution completed for plan %s: %s", plan.Name, exec.Status.Result))
	return ctrl.Result{}, nil
}

// findCurrentWave returns the index of the first wave with non-terminal groups
// (Pending, InProgress, or WaitingForVMReady). Returns -1 if all waves are terminal.
func (r *DRExecutionReconciler) findCurrentWave(exec *soteriav1alpha1.DRExecution) int {
	for i, wave := range exec.Status.Waves {
		for _, group := range wave.Groups {
			switch group.Result {
			case soteriav1alpha1.DRGroupResultPending,
				soteriav1alpha1.DRGroupResultInProgress,
				soteriav1alpha1.DRGroupResultWaitingForVMReady:
				return i
			}
		}
	}
	return -1
}

// findWaveWithWaiting returns the index of the first wave with WaitingForVMReady groups.
func (r *DRExecutionReconciler) findWaveWithWaiting(exec *soteriav1alpha1.DRExecution) int {
	for i, wave := range exec.Status.Waves {
		for _, group := range wave.Groups {
			if group.Result == soteriav1alpha1.DRGroupResultWaitingForVMReady {
				return i
			}
		}
	}
	return -1
}

// hasInProgressGroups returns true if any group in any wave is InProgress,
// indicating an in-flight operation that was interrupted (crash recovery).
func hasInProgressGroups(exec *soteriav1alpha1.DRExecution) bool {
	for _, wave := range exec.Status.Waves {
		for _, group := range wave.Groups {
			if group.Result == soteriav1alpha1.DRGroupResultInProgress {
				return true
			}
		}
	}
	return false
}

// hasWaitingForVMReady returns true if any wave has groups in WaitingForVMReady state.
func hasWaitingForVMReady(exec *soteriav1alpha1.DRExecution) bool {
	for _, wave := range exec.Status.Waves {
		if waveHasWaitingForVMReady(&wave) {
			return true
		}
	}
	return false
}

// waveHasWaitingForVMReady returns true if any group in the wave is WaitingForVMReady.
func waveHasWaitingForVMReady(wave *soteriav1alpha1.WaveStatus) bool {
	for _, group := range wave.Groups {
		if group.Result == soteriav1alpha1.DRGroupResultWaitingForVMReady {
			return true
		}
	}
	return false
}

// markAllWaitingAsReady converts all WaitingForVMReady groups to Completed.
// Used when VMManager is nil (tests without KubeVirt).
func (r *DRExecutionReconciler) markAllWaitingAsReady(exec *soteriav1alpha1.DRExecution) {
	now := metav1.Now()
	for wi := range exec.Status.Waves {
		for gi := range exec.Status.Waves[wi].Groups {
			g := &exec.Status.Waves[wi].Groups[gi]
			if g.Result == soteriav1alpha1.DRGroupResultWaitingForVMReady {
				g.Result = soteriav1alpha1.DRGroupResultCompleted
				g.CompletionTime = &now
			}
		}
	}
}

// resolveVMNamespaceFromPlan finds the namespace for a VM by searching the
// plan's wave status.
func (r *DRExecutionReconciler) resolveVMNamespaceFromPlan(
	plan *soteriav1alpha1.DRPlan, waveIdx int, vmName string,
) string {
	if waveIdx < len(plan.Status.Waves) {
		for _, dvm := range plan.Status.Waves[waveIdx].VMs {
			if dvm.Name == vmName {
				return dvm.Namespace
			}
		}
	}
	return ""
}

// reconcileReprotect dispatches the ReprotectHandler for re-protect and restore
// executions. Re-protect is storage-only (no waves, no VM operations):
// StopReplication + SetSource + health monitoring for all volume groups.
func (r *DRExecutionReconciler) reconcileReprotect(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	if r.ReprotectHandler == nil {
		return r.failExecution(ctx, exec, "ReprotectNotConfigured",
			"ReprotectHandler not configured", plan)
	}

	r.event(exec, corev1.EventTypeNormal, "ReprotectStarted", "Dispatch",
		fmt.Sprintf("Re-protect started for plan %s", plan.Name))

	// Discover volume groups from the plan's wave status. Unlike wave-based
	// execution (which re-discovers VMs at runtime), re-protect reads from
	// plan.Status.Waves populated by the DRPlan controller. If waves are
	// empty the plan may not have been reconciled since VMs were labelled.
	vgEntries, err := r.buildVolumeGroupEntries(ctx, plan)
	if err != nil {
		logger.Error(err, "Failed to build volume group entries for re-protect")
		return r.failExecution(ctx, exec, "VolumeGroupResolutionFailed",
			fmt.Sprintf("discovering volume groups for re-protect: %v", err), plan)
	}
	if len(vgEntries) == 0 {
		r.event(exec, corev1.EventTypeWarning, "NoVolumeGroups", "Dispatch",
			fmt.Sprintf("No volume groups found for re-protect on plan %s; "+
				"plan wave status may be empty or stale", plan.Name))
	}

	input := engine.ReprotectInput{
		Execution:    exec,
		Plan:         plan,
		VolumeGroups: vgEntries,
	}

	// Capture plan state before Execute, which mutates plan.Status.Conditions
	// in-place. The pre-execution base ensures MergeFrom includes condition
	// changes in the final patch (not just the phase advance).
	planPreExec := plan.DeepCopy()

	result, execErr := r.ReprotectHandler.Execute(ctx, input)

	if execErr != nil && result == nil {
		logger.Error(execErr, "Re-protect execution failed")
		return r.failExecution(ctx, exec, "ReprotectFailed",
			fmt.Sprintf("re-protect failed: %v", execErr), plan)
	}

	// Context cancellation (leader election loss, shutdown): do NOT write a
	// terminal result — let the new leader re-reconcile and resume via
	// reconcileReprotectResume. All driver operations are idempotent.
	if ctx.Err() != nil {
		logger.Info("Re-protect interrupted, will resume on next reconcile")
		return ctrl.Result{}, ctx.Err()
	}

	// Record the execution result.
	now := metav1.Now()
	execResult := result.Result()
	execPatch := client.MergeFrom(exec.DeepCopy())
	exec.Status.Result = execResult
	exec.Status.Phase = soteriav1alpha1.ResultToPhase(execResult)
	exec.Status.IsActive = false
	exec.Status.CompletionTime = &now
	if exec.Status.StartTime != nil {
		exec.Status.Duration = exec.Status.CompletionTime.Sub(exec.Status.StartTime.Time).
			Truncate(time.Second).String()
	}

	condStatus := metav1.ConditionTrue
	condReason := "ReprotectSucceeded"
	switch execResult {
	case soteriav1alpha1.ExecutionResultFailed:
		condStatus = metav1.ConditionFalse
		condReason = "ReprotectFailed"
	case soteriav1alpha1.ExecutionResultPartiallySucceeded:
		condReason = "ReprotectPartiallySucceeded"
	}
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus,
		Reason:             condReason,
		Message:            fmt.Sprintf("Re-protect completed: %s", execResult),
		ObservedGeneration: exec.Generation,
	})
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:   "ReprotectPhase",
		Status: metav1.ConditionTrue,
		Reason: "Complete",
		Message: fmt.Sprintf("Role setup: %d/%d, healthy: %d/%d",
			result.SetupSucceeded, result.TotalVGs, result.HealthyVGs, result.TotalVGs),
		ObservedGeneration: exec.Generation,
	})
	if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
		logger.Error(err, "Failed to update DRExecution result after re-protect")
		return ctrl.Result{}, err
	}

	r.recordExecutionMetrics(exec)

	// Emit completion events.
	r.event(exec, corev1.EventTypeNormal, "ReprotectRoleSetupComplete", "RoleSetup",
		fmt.Sprintf("Re-protect role setup complete: %d/%d volume groups succeeded",
			result.SetupSucceeded, result.TotalVGs))

	if result.TimedOut {
		r.event(exec, corev1.EventTypeWarning, "ReprotectTimeout", "HealthMonitoring",
			fmt.Sprintf("Re-protect health monitoring timed out: %d/%d volume groups healthy",
				result.HealthyVGs, result.TotalVGs))
	} else if execResult != soteriav1alpha1.ExecutionResultFailed {
		r.event(exec, corev1.EventTypeNormal, "ReprotectHealthy", "HealthMonitoring",
			fmt.Sprintf("All %d volume groups report healthy replication", result.HealthyVGs))
	}

	// Advance DRPlan phase on success or partial success.
	if execResult == soteriav1alpha1.ExecutionResultSucceeded ||
		execResult == soteriav1alpha1.ExecutionResultPartiallySucceeded {
		previousPhase := plan.Status.Phase
		newPhase, err := engine.RestStateAfterCompletion(plan.Status.Phase, exec.Spec.Mode)
		if err != nil {
			logger.Error(err, "Could not complete phase transition", "currentPhase", plan.Status.Phase)
		} else {
			planPatch := client.MergeFrom(planPreExec)
			plan.Status.Phase = newPhase
			plan.Status.ActiveSite = engine.ActiveSiteForPhase(newPhase, plan.Spec.PrimarySite, plan.Spec.SecondarySite)
			if err := r.Status().Patch(ctx, plan, planPatch); err != nil {
				logger.Error(err, "Failed to advance DRPlan phase",
					"plan", plan.Name, "targetPhase", newPhase)
				return ctrl.Result{}, err
			}
			logger.Info("Advanced DRPlan phase",
				"plan", plan.Name, "from", previousPhase, "to", newPhase,
				"activeSite", plan.Status.ActiveSite)
		}
	}

	return ctrl.Result{}, nil
}

// buildVolumeGroupEntries collects all volume groups from the plan's wave
// status, resolves a driver per VG, and resolves VolumeGroupIDs via
// GetVolumeGroup. This gives the ReprotectHandler everything it needs
// without depending on the wave executor.
func (r *DRExecutionReconciler) buildVolumeGroupEntries(
	ctx context.Context, plan *soteriav1alpha1.DRPlan,
) ([]engine.VolumeGroupEntry, error) {
	if r.WaveExecutor == nil {
		return nil, fmt.Errorf("WaveExecutor required for VG resolution")
	}

	driverType := plan.Spec.VolumeReplicationDriver.Type
	var entries []engine.VolumeGroupEntry
	seen := make(map[string]bool)

	for _, wave := range plan.Status.Waves {
		for _, vg := range wave.Groups {
			key := vg.Namespace + "/" + vg.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			drv, err := r.WaveExecutor.ResolveVGDriver(ctx, driverType)
			if err != nil {
				return nil, fmt.Errorf("resolving driver for volume group %s: %w", vg.Name, err)
			}

			vgID, err := r.resolveVGID(ctx, drv, driverType, vg)
			if err != nil {
				return nil, fmt.Errorf("resolving volume group ID for %s: %w", vg.Name, err)
			}

			entries = append(entries, engine.VolumeGroupEntry{
				Info:   vg,
				Driver: drv,
				VGID:   vgID,
			})
		}
	}
	return entries, nil
}

// reconcileReprotectResume handles the resume path for in-progress re-protect
// executions after a pod restart. Unlike wave-based resume (which skips
// completed waves), re-protect uses an idempotent-replay model: the entire
// workflow is re-executed from scratch. This is safe because every driver
// operation (StopReplication, SetSource, GetReplicationStatus) is idempotent
// and produces the same outcome on repeated calls. The trade-off is a
// slightly longer recovery time vs. adding phase-checkpoint complexity.
func (r *DRExecutionReconciler) reconcileReprotectResume(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)
	logger.Info("Resuming re-protect execution (idempotent replay)")

	if err := r.verifyExclusiveExecution(ctx, exec); err != nil {
		return r.failExecution(ctx, exec, "ConcurrencyConflict", err.Error())
	}

	plan, err := r.fetchPlan(ctx, exec.Spec.PlanName)
	if err != nil {
		return r.failExecution(ctx, exec, "PlanNotFound",
			fmt.Sprintf("DRPlan %q not found: %v", exec.Spec.PlanName, err))
	}

	r.event(exec, corev1.EventTypeNormal, "ReprotectResumed", "Checkpoint",
		"Resuming re-protect execution after restart (idempotent replay)")

	return r.reconcileReprotect(ctx, exec, plan)
}

// resolveVGID computes a deterministic VolumeGroupID and validates that the
// VR/VGR exists via GetVolumeGroup. The DRPlan reconciler owns VR/VGR
// creation; this path is read-only.
func (r *DRExecutionReconciler) resolveVGID(
	ctx context.Context, drv drivers.StorageProvider, driverType string, vg soteriav1alpha1.VolumeGroupInfo,
) (drivers.VolumeGroupID, error) {
	vgID := drivers.VolumeGroupIDFor(driverType, vg.Namespace, vg.Name)
	if _, err := drv.GetVolumeGroup(ctx, vgID); err != nil {
		if errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			return "", fmt.Errorf("VR/VGR not yet created by DRPlan reconciler for volume group %s: %w", vg.Name, err)
		}
		return "", fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
	}
	return vgID, nil
}

// reconcileResume handles the resume path for in-progress executions after
// a pod restart or leader failover. It analyzes the execution status to
// determine the resume point, resets in-flight groups to Pending, and
// dispatches the wave executor from the resume wave.
func (r *DRExecutionReconciler) reconcileResume(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	// Multi-site planned migration: the Owner must wait for Step 0 to
	// complete before running waves, even on the resume path.
	if r.LocalSite != "" && exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration {
		localStatus := getSiteStatus(exec, r.LocalSite)
		step0Done := localStatus.Step0Complete
		if !step0Done {
			step0Done = meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Complete")
		}
		if !step0Done {
			logger.V(1).Info("Waiting for Step 0 to complete (resume path)")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	if r.ResumeAnalyzer == nil {
		logger.V(1).Info("ResumeAnalyzer not configured, skipping resume")
		return ctrl.Result{}, nil
	}

	resumePoint := r.ResumeAnalyzer.AnalyzeExecution(exec)
	if resumePoint.IsComplete {
		logger.V(1).Info("Execution analysis shows complete, skipping resume")
		return ctrl.Result{}, nil
	}

	logger.Info("Resuming execution",
		"waveIndex", resumePoint.WaveIndex,
		"completedGroups", len(resumePoint.CompletedGroups),
		"failedGroups", len(resumePoint.FailedGroups),
		"inFlightGroups", len(resumePoint.InFlightGroups),
		"pendingGroups", len(resumePoint.PendingGroups))

	// Reset in-flight groups (InProgress at crash time) to Pending for retry.
	for _, groupName := range resumePoint.InFlightGroups {
		r.resetInFlightGroup(exec, resumePoint.WaveIndex, groupName)
	}
	if len(resumePoint.InFlightGroups) > 0 {
		if err := r.Status().Update(ctx, exec); err != nil {
			logger.Error(err, "Failed to reset in-flight groups")
			return ctrl.Result{}, err
		}
	}

	// Emit ExecutionResumed event.
	r.event(exec, corev1.EventTypeNormal, "ExecutionResumed", "Checkpoint",
		fmt.Sprintf("Resuming execution from wave %d: %d completed, %d failed, %d retrying",
			resumePoint.WaveIndex,
			len(resumePoint.CompletedGroups),
			len(resumePoint.FailedGroups),
			len(resumePoint.InFlightGroups)))

	// Verify exclusivity: self-fail if a competing non-terminal execution exists.
	if err := r.verifyExclusiveExecution(ctx, exec); err != nil {
		return r.failExecution(ctx, exec, "ConcurrencyConflict", err.Error())
	}

	plan, err := r.fetchPlan(ctx, exec.Spec.PlanName)
	if err != nil {
		return r.failExecution(ctx, exec, "PlanNotFound",
			fmt.Sprintf("DRPlan %q not found: %v", exec.Spec.PlanName, err))
	}

	// Resolve handler for the execution mode.
	drHandler, err := r.resolveHandler(exec.Spec.Mode)
	if err != nil {
		return r.failExecution(ctx, exec, "HandlerResolutionFailed", err.Error(), plan)
	}

	// Build the set of groups to skip in the resume wave (completed + failed).
	skipGroups := make(map[string]bool,
		len(resumePoint.CompletedGroups)+len(resumePoint.FailedGroups))
	for _, name := range resumePoint.CompletedGroups {
		skipGroups[name] = true
	}
	for _, name := range resumePoint.FailedGroups {
		skipGroups[name] = true
	}

	// Dispatch execution.
	if r.WaveExecutor != nil {
		execInput := engine.ExecuteInput{
			Execution: exec,
			Plan:      plan,
			Handler:   drHandler,
		}

		if len(exec.Status.Waves) == 0 {
			// No waves initialized before crash — run the full execution
			// pipeline (discover → chunk → execute) instead of resume.
			if err := r.WaveExecutor.Execute(ctx, execInput); err != nil {
				logger.Error(err, "Full re-execution failed after resume with no waves")
				return ctrl.Result{}, err
			}
		} else {
			if err := r.WaveExecutor.ExecuteFromWave(ctx, execInput, resumePoint.WaveIndex, skipGroups); err != nil {
				logger.Error(err, "Resume execution failed")
				return ctrl.Result{}, err
			}
		}

		r.recordExecutionMetrics(exec)

		r.event(exec, corev1.EventTypeNormal, "ExecutionCompleted", "WaveExecution",
			fmt.Sprintf("Resumed execution completed: %s", exec.Status.Result))
	}

	return ctrl.Result{}, nil
}

// resetInFlightGroup finds a group by name in the specified wave and resets
// its Result from InProgress to Pending for retry after crash.
func (r *DRExecutionReconciler) resetInFlightGroup(
	exec *soteriav1alpha1.DRExecution, waveIdx int, groupName string,
) {
	if waveIdx >= len(exec.Status.Waves) {
		return
	}
	for i := range exec.Status.Waves[waveIdx].Groups {
		if exec.Status.Waves[waveIdx].Groups[i].Name == groupName &&
			exec.Status.Waves[waveIdx].Groups[i].Result == soteriav1alpha1.DRGroupResultInProgress {
			exec.Status.Waves[waveIdx].Groups[i].Result = soteriav1alpha1.DRGroupResultPending
		}
	}
}

// failExecution marks a DRExecution as Failed with a descriptive condition.
func (r *DRExecutionReconciler) failExecution(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	reason, message string,
	_ ...*soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "reason", reason)

	now := metav1.Now()
	patch := client.MergeFrom(exec.DeepCopy())
	exec.Status.Result = soteriav1alpha1.ExecutionResultFailed
	exec.Status.Phase = soteriav1alpha1.ExecutionPhaseFailed
	exec.Status.IsActive = false
	if exec.Status.StartTime == nil {
		exec.Status.StartTime = &now
	}
	exec.Status.CompletionTime = &now
	exec.Status.Duration = exec.Status.CompletionTime.Sub(exec.Status.StartTime.Time).
		Truncate(time.Second).String()
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: exec.Generation,
	})

	if err := r.Status().Patch(ctx, exec, patch); err != nil {
		logger.Error(err, "Failed to update DRExecution failure status")
		return ctrl.Result{}, err
	}

	r.recordExecutionMetrics(exec)

	return ctrl.Result{}, nil
}

// resolveHandler selects the appropriate DRGroupHandler based on execution mode.
// FailoverHandler is used for both planned_migration and disaster — the config
// drives behavior, not the mode string. When VMManager is not configured (e.g.,
// integration tests), falls back to the injected Handler or NoOpHandler.
// Reprotect is dispatched via reconcileReprotect and never reaches this method.
func (r *DRExecutionReconciler) resolveHandler(
	mode soteriav1alpha1.ExecutionMode,
) (engine.DRGroupHandler, error) {
	switch mode {
	case soteriav1alpha1.ExecutionModePlannedMigration:
		if r.VMManager == nil {
			if r.Handler != nil {
				return r.Handler, nil
			}
			return nil, fmt.Errorf(
				"VMManager not configured; planned migration requires a VMManager")
		}
		return &engine.FailoverHandler{
			VMManager: r.VMManager,
			Config:    engine.FailoverConfig{GracefulShutdown: true},
		}, nil
	case soteriav1alpha1.ExecutionModeDisaster:
		if r.VMManager == nil {
			if r.Handler != nil {
				return r.Handler, nil
			}
			return nil, fmt.Errorf(
				"VMManager not configured; disaster failover requires a VMManager")
		}
		return &engine.FailoverHandler{
			VMManager: r.VMManager,
			Config:    engine.FailoverConfig{GracefulShutdown: false},
		}, nil
	case soteriav1alpha1.ExecutionModeReprotect:
		return &engine.NoOpHandler{}, nil
	}
	if r.Handler != nil {
		return r.Handler, nil
	}
	return &engine.NoOpHandler{}, nil
}

// reconcileRetry handles the retry path for PartiallySucceeded executions.
// Triggered when the operator adds the soteria.io/retry-groups annotation.
func (r *DRExecutionReconciler) reconcileRetry(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	annotation, hasAnnotation := exec.Annotations[engine.RetryGroupsAnnotation]
	if !hasAnnotation {
		logger.V(1).Info("PartiallySucceeded execution without retry annotation, skipping")
		return ctrl.Result{}, nil
	}

	// Guard: if any group is InProgress, a retry is already running — wait.
	for _, wave := range exec.Status.Waves {
		for _, group := range wave.Groups {
			if group.Result == soteriav1alpha1.DRGroupResultInProgress {
				logger.V(1).Info("Retry already in progress, waiting", "group", group.Name)
				return ctrl.Result{}, nil
			}
		}
	}

	// Resolve retry targets from the annotation.
	targets, err := engine.ResolveRetryGroups(exec, annotation)
	if err != nil {
		logger.Info("Retry group resolution failed", "error", err)
		r.removeRetryAnnotation(ctx, exec)
		r.setRetryRejectedCondition(ctx, exec, fmt.Sprintf("retry group resolution failed: %v", err))
		r.event(exec, corev1.EventTypeWarning, "RetryRejected", "RetryAction",
			fmt.Sprintf("Retry rejected for execution %s: %v", exec.Name, err))
		return ctrl.Result{}, nil
	}

	if len(targets) == 0 {
		logger.Info("No failed groups to retry, removing annotation")
		r.removeRetryAnnotation(ctx, exec)
		return ctrl.Result{}, nil
	}

	// Fetch the plan early — needed for VM namespace resolution and chunk reconstruction.
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		logger.Error(err, "Failed to fetch DRPlan for retry")
		return ctrl.Result{}, err
	}

	// VM health validation for all VMs in retry groups.
	if r.WaveExecutor != nil && r.WaveExecutor.VMHealthValidator != nil {
		for _, target := range targets {
			groupStatus := exec.Status.Waves[target.WaveIndex].Groups[target.GroupIndex]
			for _, vmName := range groupStatus.VMNames {
				ns := r.resolveVMNamespaceFromPlan(&plan, target.WaveIndex, vmName)
				if err := r.WaveExecutor.VMHealthValidator.ValidateVMHealth(ctx, vmName, ns); err != nil {
					logger.Info("VM health validation failed, rejecting retry",
						"vm", vmName, "namespace", ns, "error", err)
					r.removeRetryAnnotation(ctx, exec)
					r.setRetryRejectedCondition(ctx, exec, err.Error())
					r.event(exec, corev1.EventTypeWarning, "RetryRejected", "RetryAction",
						fmt.Sprintf("Retry rejected for execution %s: %v", exec.Name, err))
					return ctrl.Result{}, nil
				}
			}
		}
	}

	// Resolve handler.
	drHandler, err := r.resolveHandler(exec.Spec.Mode)
	if err != nil {
		logger.Error(err, "Failed to resolve handler for retry")
		r.removeRetryAnnotation(ctx, exec)
		r.setRetryRejectedCondition(ctx, exec, fmt.Sprintf("handler resolution failed: %v", err))
		return ctrl.Result{}, nil
	}

	// Emit RetryStarted event.
	groupNames := make([]string, len(targets))
	for i, t := range targets {
		groupNames[i] = t.GroupName
	}
	r.event(exec, corev1.EventTypeNormal, "RetryStarted", "RetryAction",
		fmt.Sprintf("Retry started for execution %s: groups %s",
			exec.Name, strings.Join(groupNames, ", ")))

	// Execute retry.
	retryInput := engine.RetryInput{
		Execution:    exec,
		Plan:         &plan,
		Handler:      drHandler,
		RetryTargets: targets,
	}
	if err := r.WaveExecutor.ExecuteRetry(ctx, retryInput); err != nil {
		logger.Error(err, "Retry execution failed")
		return ctrl.Result{}, err
	}

	// Emit per-group and completion events.
	for _, target := range targets {
		groupStatus := exec.Status.Waves[target.WaveIndex].Groups[target.GroupIndex]
		switch groupStatus.Result {
		case soteriav1alpha1.DRGroupResultCompleted:
			r.event(exec, corev1.EventTypeNormal, "GroupRetrySucceeded", "RetryAction",
				fmt.Sprintf("DRGroup %s retry succeeded (attempt %d)",
					target.GroupName, groupStatus.RetryCount))
		case soteriav1alpha1.DRGroupResultFailed:
			r.event(exec, corev1.EventTypeWarning, "GroupRetryFailed", "RetryAction",
				fmt.Sprintf("DRGroup %s retry failed (attempt %d): %s",
					target.GroupName, groupStatus.RetryCount, groupStatus.Error))
		}
	}

	r.event(exec, corev1.EventTypeNormal, "RetryCompleted", "RetryAction",
		fmt.Sprintf("Retry completed for execution %s: result %s", exec.Name, exec.Status.Result))

	// Remove annotation after retry completes.
	r.removeRetryAnnotation(ctx, exec)

	return ctrl.Result{}, nil
}

// removeRetryAnnotation removes the retry annotation from the DRExecution.
func (r *DRExecutionReconciler) removeRetryAnnotation(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
) {
	logger := log.FromContext(ctx)

	if err := r.Get(ctx, client.ObjectKeyFromObject(exec), exec); err != nil {
		logger.V(1).Info("Could not re-fetch DRExecution for annotation removal", "error", err)
		return
	}
	if _, ok := exec.Annotations[engine.RetryGroupsAnnotation]; !ok {
		return
	}
	delete(exec.Annotations, engine.RetryGroupsAnnotation)
	if err := r.Update(ctx, exec); err != nil {
		logger.V(1).Info("Could not remove retry annotation", "error", err)
	}
}

// setRetryRejectedCondition sets a RetryRejected condition on the execution.
func (r *DRExecutionReconciler) setRetryRejectedCondition(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, message string,
) {
	logger := log.FromContext(ctx)

	if err := r.Get(ctx, client.ObjectKeyFromObject(exec), exec); err != nil {
		logger.V(1).Info("Could not re-fetch DRExecution for condition update", "error", err)
		return
	}

	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "RetryRejected",
		Status:             metav1.ConditionTrue,
		Reason:             "RetryRejected",
		Message:            message,
		ObservedGeneration: exec.Generation,
	})
	if err := r.Status().Update(ctx, exec); err != nil {
		logger.V(1).Info("Could not set RetryRejected condition", "error", err)
	}
}

// recordExecutionMetrics observes the failover duration histogram and increments
// the execution counter when a DRExecution reaches a terminal state.
func (r *DRExecutionReconciler) recordExecutionMetrics(exec *soteriav1alpha1.DRExecution) {
	if exec.Status.StartTime == nil || exec.Status.CompletionTime == nil || !exec.Status.IsTerminal() {
		return
	}
	durationSeconds := exec.Status.CompletionTime.Sub(exec.Status.StartTime.Time).Seconds()
	metrics.RecordExecutionCompletion(
		string(exec.Spec.Mode), string(exec.Status.Result), durationSeconds)
}

func startEventFields(phase string) (reason, action, verb string) {
	switch phase {
	case soteriav1alpha1.PhaseFailingBack:
		return "FailbackStarted", "FailbackAction", "Failback"
	case soteriav1alpha1.PhaseReprotecting:
		return "ReprotectStarted", "ReprotectAction", "Reprotect"
	case soteriav1alpha1.PhaseReprotectingBack:
		return "RestoreStarted", "RestoreAction", "Restore"
	default:
		return "FailoverStarted", "FailoverAction", "Failover"
	}
}

// Tier 2 – Architecture:
// verifyExclusiveExecution is the third layer of the DRExecution concurrency
// model. Layer 1 (admission gate) catches most concurrent creates via a
// best-effort LIST. Layer 2 (SERIAL INSERT) provides Paxos-level ordering
// across DCs on INSERT IF NOT EXISTS. This layer is the safety net: it lists
// DRExecutions for the plan using ScyllaRetry backoff (tolerating eventual
// consistency lag) and self-fails this execution if a competing non-terminal
// execution exists — the competing execution (which won the SERIAL INSERT
// race) will proceed.
func (r *DRExecutionReconciler) verifyExclusiveExecution(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
) error {
	return retry.RetryOnConflict(engine.ScyllaRetry, func() error {
		var execList soteriav1alpha1.DRExecutionList
		if err := r.List(ctx, &execList, client.MatchingLabels{
			soteriav1alpha1.PlanNameLabel: exec.Spec.PlanName,
		}); err != nil {
			return err
		}

		selfVisible := false
		for i := range execList.Items {
			other := &execList.Items[i]
			if other.Name == exec.Name {
				selfVisible = true
				continue
			}
			if !other.Status.IsTerminal() {
				return fmt.Errorf(
					"competing non-terminal execution %q found for plan %q; self-failing",
					other.Name, exec.Spec.PlanName)
			}
		}

		// If this execution is not yet visible in the list, the informer
		// cache is stale. Return a conflict error to trigger ScyllaRetry
		// backoff rather than falsely declaring exclusivity.
		if !selfVisible {
			return apierrors.NewConflict(
				soteriav1alpha1.Resource("drexecutions"), exec.Name,
				fmt.Errorf("self not visible in label-filtered list; cache may be stale"))
		}

		return nil
	})
}

// fetchPlan fetches the DRPlan by name with ScyllaRetry backoff to tolerate
// informer cache lag.
func (r *DRExecutionReconciler) fetchPlan(
	ctx context.Context, planName string,
) (*soteriav1alpha1.DRPlan, error) {
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: planName}, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// ensurePlanNameLabel sets soteria.io/plan-name on the DRExecution for
// concurrency queries and history lookups. PrepareForCreate sets this label
// server-side, but executions created before this change may lack it.
func (r *DRExecutionReconciler) ensurePlanNameLabel(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, key client.ObjectKey,
) error {
	if exec.Labels != nil && exec.Labels[soteriav1alpha1.PlanNameLabel] == exec.Spec.PlanName {
		return nil
	}
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	err := retry.RetryOnConflict(engine.ScyllaRetry, func() error {
		if err := r.Get(ctx, key, exec); err != nil {
			return err
		}
		if exec.Labels != nil && exec.Labels[soteriav1alpha1.PlanNameLabel] == exec.Spec.PlanName {
			return nil
		}
		patch := client.MergeFrom(exec.DeepCopy())
		if exec.Labels == nil {
			exec.Labels = make(map[string]string)
		}
		exec.Labels[soteriav1alpha1.PlanNameLabel] = exec.Spec.PlanName
		return r.Patch(ctx, exec, patch)
	})
	if err != nil {
		logger.Error(err, "Failed to set plan-name label", "label", soteriav1alpha1.PlanNameLabel)
		return err
	}

	logger.Info("Set plan-name label", "label", soteriav1alpha1.PlanNameLabel, "value", exec.Spec.PlanName)
	return nil
}

// reconcileSetup validates the execution mode, transitions the DRPlan phase,
// sets the concurrency guard, and initializes the execution's StartTime.
// Always yields after completing setup (RequeueAfter) so the next reconcile
// starts with a fresh resourceVersion, avoiding write contention from
// ScyllaDB eventual consistency.
func (r *DRExecutionReconciler) reconcileSetup(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	if exec.Spec.Mode != soteriav1alpha1.ExecutionModePlannedMigration &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeDisaster &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeReprotect {
		result, err := r.failExecution(ctx, exec, "InvalidMode",
			fmt.Sprintf("unsupported execution mode %q", exec.Spec.Mode))
		return result, err
	}

	previousPhase := plan.Status.Phase
	targetPhase, err := engine.Transition(previousPhase, exec.Spec.Mode)
	if err != nil {
		validPhases := engine.ValidStartingPhases(exec.Spec.Mode)
		sort.Strings(validPhases)
		logger.Info("Invalid phase transition",
			"plan", plan.Name, "currentPhase", previousPhase, "mode", exec.Spec.Mode)
		result, fErr := r.failExecution(ctx, exec, "InvalidPhaseTransition",
			fmt.Sprintf("cannot %s from phase %q on plan %q; valid starting phases: %s",
				exec.Spec.Mode, previousPhase, plan.Name, strings.Join(validPhases, ", ")))
		return result, fErr
	}

	now := metav1.Now()
	execPatch := client.MergeFrom(exec.DeepCopy())
	exec.Status.StartTime = &now
	exec.Status.Phase = soteriav1alpha1.ExecutionPhaseExecuting
	exec.Status.IsActive = true
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionTrue,
		Reason:             "ExecutionStarted",
		Message:            fmt.Sprintf("Execution started for plan %s in %s mode", plan.Name, exec.Spec.Mode),
		ObservedGeneration: exec.Generation,
	})

	if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
		logger.Error(err, "Failed to update DRExecution status")
		return ctrl.Result{}, err
	}

	reason, action, verb := startEventFields(targetPhase)
	r.event(plan, corev1.EventTypeNormal, reason, action,
		fmt.Sprintf("%s started for plan %s in %s mode via execution %s",
			verb, plan.Name, exec.Spec.Mode, exec.Name))

	r.setupDone.Store(exec.Name, true)

	logger.Info("DRExecution setup complete, yielding for fresh resourceVersion",
		"plan", plan.Name, "mode", exec.Spec.Mode, "effectivePhase", targetPhase)
	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

// dispatchByRole computes the reconcile role for this controller instance and
// dispatches accordingly. Returns (result, done=true, err) if the role was
// handled (RoleNone or RoleStep0), or (_, false, nil) if the caller should
// continue with the normal Owner path.
func (r *DRExecutionReconciler) dispatchByRole(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	if r.LocalSite != plan.Spec.PrimarySite && r.LocalSite != plan.Spec.SecondarySite {
		logger.Error(nil, "LocalSite does not match plan topology, skipping reconcile",
			"localSite", r.LocalSite,
			"primarySite", plan.Spec.PrimarySite,
			"secondarySite", plan.Spec.SecondarySite)
		return ctrl.Result{}, true, nil
	}

	effectivePhase := engine.EffectivePhase(plan.Status.Phase, exec.Spec.Mode)
	role := engine.ReconcileRole(effectivePhase, exec.Spec.Mode,
		r.LocalSite, plan.Spec.PrimarySite, plan.Spec.SecondarySite)
	logger.V(1).Info("Computed reconcile role",
		"role", role, "effectivePhase", effectivePhase,
		"localSite", r.LocalSite, "mode", exec.Spec.Mode)

	switch role {
	case engine.RoleNone:
		logger.V(1).Info("Skipping reconcile, not the owning site")
		return ctrl.Result{}, true, nil
	case engine.RoleStep0:
		result, err := r.reconcileStep0(ctx, exec, plan)
		return result, true, err
	case engine.RoleReprotectPassive:
		result, err := r.reconcileReprotectPassive(ctx, exec, plan)
		return result, true, err
	default:
		return ctrl.Result{}, false, nil
	}
}

// reconcileReprotectPassive runs on the inactive site during reprotect.
// It ensures local VRs are in the correct secondary role and verifies
// replication health.
//
// After planned migration the VRs are already secondary and receiving
// replication from the new primary — this is typically a no-op that just
// confirms health.
//
// After disaster recovery the VRs may still be in a stale primary state
// because the site was down during failover; this function demotes them
// to secondary and then waits for replication health.
func (r *DRExecutionReconciler) reconcileReprotectPassive(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "role", "ReprotectPassive")
	logger.Info("Reconciling reprotect on passive site")

	if r.WaveExecutor == nil {
		logger.V(1).Info("WaveExecutor not configured, nothing to do on passive site")
		return ctrl.Result{}, nil
	}

	vgEntries, err := r.buildVolumeGroupEntries(ctx, plan)
	if err != nil {
		logger.Error(err, "Failed to build volume group entries for passive reprotect")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if len(vgEntries) == 0 {
		logger.V(1).Info("No volume groups found on passive site")
		return ctrl.Result{}, nil
	}

	allHealthy := true
	for _, vg := range vgEntries {
		status, err := vg.Driver.GetReplicationStatus(ctx, vg.VGID)
		if err != nil {
			if errors.Is(err, drivers.ErrVolumeGroupNotFound) {
				logger.V(1).Info("Volume group not found on passive site, skipping",
					"vg", vg.Info.Name)
				continue
			}
			logger.Error(err, "Could not read replication status on passive site",
				"vg", vg.Info.Name)
			allHealthy = false
			continue
		}

		switch status.Role {
		case drivers.RoleTarget:
			if status.Health != drivers.HealthHealthy {
				logger.Info("VG is secondary, waiting for healthy replication",
					"vg", vg.Info.Name, "health", status.Health)
				allHealthy = false
			} else {
				logger.V(1).Info("VG confirmed secondary and healthy",
					"vg", vg.Info.Name)
			}

		case drivers.RoleSource:
			logger.Info("Stale primary detected on passive site, demoting",
				"vg", vg.Info.Name)
			if stopErr := vg.Driver.StopReplication(ctx, vg.VGID); stopErr != nil {
				logger.Error(stopErr, "StopReplication failed for stale primary",
					"vg", vg.Info.Name)
				allHealthy = false
				continue
			}
			logger.Info("Demoted stale primary, will re-check on next reconcile",
				"vg", vg.Info.Name)
			allHealthy = false

		default:
			logger.Info("VG in non-replicated state on passive site",
				"vg", vg.Info.Name, "role", status.Role)
			allHealthy = false
		}
	}

	if !allHealthy {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	logger.Info("All local VRs are secondary and healthy on passive site")
	return ctrl.Result{}, nil
}

// reconcileStep0 runs the source site's Step 0 for planned migration.
// The flow is:
//  1. Guard: if DemotionComplete already set, skip to waiting for
//     Step0Complete from the target site.
//  2. Guard: if execution not yet started, wait.
//  3. Run PreExecute (stops VMs + StopReplication, returns nil on success).
//  4. Set Step0Started condition to anchor the demotion timeout baseline
//     (so VM shutdown does not consume the timeout budget).
//  5. Check local VRs reached role=Target (state=Secondary) via checkVRsHealthy.
//  6. When confirmed, set DemotionComplete in local site status.
//  7. Wait for Step0Complete from the target site via reconcileSourceStep0Wait.
//
// This method is idempotent — PreExecute operations (StopVM, StopReplication)
// are safe to call multiple times.
func (r *DRExecutionReconciler) reconcileStep0(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "role", "Step0")

	localStatus := getSiteStatus(exec, r.LocalSite)

	// If DemotionComplete already set, skip to waiting for Step0Complete from target.
	if localStatus.DemotionComplete {
		return r.reconcileSourceStep0Wait(ctx, exec, plan)
	}

	// Guard: Step 0 only applies to in-progress planned migration executions.
	if exec.Status.StartTime == nil {
		logger.V(1).Info("Execution not yet started, waiting for target site setup")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	logger.Info("Running Step 0 (source site planned migration)")

	// Run PreExecute: stops VMs + StopReplication (demote source VRs).
	var drHandler engine.DRGroupHandler
	if r.VMManager != nil {
		drHandler = &engine.FailoverHandler{
			VMManager: r.VMManager,
			Config:    engine.FailoverConfig{GracefulShutdown: true},
		}
	} else if r.Handler != nil {
		drHandler = r.Handler
	} else {
		return ctrl.Result{}, fmt.Errorf(
			"VMManager not configured; planned migration requires a VMManager")
	}

	if ph, ok := drHandler.(interface {
		PreExecute(ctx context.Context, groups []engine.ExecutionGroup) error
	}); ok && r.WaveExecutor != nil {
		allGroups, err := r.WaveExecutor.BuildExecutionGroups(ctx, plan)
		if err != nil {
			logger.Error(err, "Failed to build execution groups for Step 0")
			return ctrl.Result{}, err
		}
		if err := ph.PreExecute(ctx, allGroups); err != nil {
			logger.Error(err, "Step 0 pre-execution failed")
			r.event(exec, corev1.EventTypeWarning, "Step0Failed", "PlannedMigration",
				fmt.Sprintf("Step 0 failed on source site: %v", err))
			return ctrl.Result{}, err
		}
	}

	// Anchor the demotion timeout to PreExecute completion so that
	// VM shutdown time does not consume the timeout budget.
	if !meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Started") {
		execPatch := client.MergeFrom(exec.DeepCopy())
		meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
			Type:               "Step0Started",
			Status:             metav1.ConditionTrue,
			Reason:             "PreExecuteCompleted",
			Message:            "Step 0 pre-execution completed, waiting for VRs to reach Secondary",
			ObservedGeneration: exec.Generation,
		})
		if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// After PreExecute, check local VRs reached role=Target (state=Secondary).
	healthy, err := r.checkVRsHealthy(ctx, plan)
	if err != nil {
		logger.Error(err, "Demotion role check failed, will retry")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if !healthy {
		// Check timeout: multi-site path must not hang indefinitely if VRs
		// never reach state=Secondary after StopReplication.
		step0Timeout := defaultStep0Timeout
		if plan.Spec.ResyncTimeout != nil {
			step0Timeout = plan.Spec.ResyncTimeout.Duration
		}
		baseline := exec.Status.StartTime
		if step0Started := meta.FindStatusCondition(exec.Status.Conditions, "Step0Started"); step0Started != nil {
			baseline = &step0Started.LastTransitionTime
		}
		if baseline != nil && time.Since(baseline.Time) > step0Timeout {
			pending := r.pendingVGs(ctx, plan)
			logger.Info("Demotion timeout exceeded, VRs did not reach Secondary state",
				"timeout", step0Timeout, "pendingVGs", pending)
			r.event(exec, corev1.EventTypeWarning, "DemotionTimeout", "PlannedMigration",
				fmt.Sprintf("VRs did not reach state=Secondary within %s for plan %s",
					step0Timeout, plan.Name))
			return r.failExecution(ctx, exec, "DemotionTimeout",
				fmt.Sprintf("VRs did not reach state=Secondary within %s; pending: %s",
					step0Timeout, pending), plan)
		}
		logger.V(1).Info("Demoted VRs not yet Secondary, waiting")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Demotion confirmed (role=Target / state=Secondary) — set DemotionComplete in local site status.
	execPatch := client.MergeFrom(exec.DeepCopy())
	localStatus.DemotionComplete = true
	setSiteStatus(exec, r.LocalSite, localStatus)
	if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
		logger.Error(err, "Failed to set DemotionComplete in site status")
		return ctrl.Result{}, err
	}

	r.event(exec, corev1.EventTypeNormal, "DemotionComplete", "PlannedMigration",
		fmt.Sprintf("Source site demotion completed for plan %s", plan.Name))

	logger.Info("Step 0: demotion complete, waiting for target site to set Step0Complete")
	return r.reconcileSourceStep0Wait(ctx, exec, plan)
}

// reconcileSourceStep0Wait waits for the target site to set Step0Complete
// in its SiteStatuses entry after promoting its VRs. Applies a timeout based
// on plan.Spec.ResyncTimeout. On timeout, fails the execution. On completion,
// the source site's job is done.
func (r *DRExecutionReconciler) reconcileSourceStep0Wait(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "role", "Step0")

	step0Timeout := defaultStep0Timeout
	if plan.Spec.ResyncTimeout != nil {
		step0Timeout = plan.Spec.ResyncTimeout.Duration
	}

	// Check timeout using the local site's LastUpdated (DemotionComplete write
	// time) as the baseline.
	localStatus := getSiteStatus(exec, r.LocalSite)
	if localStatus.LastUpdated != nil && time.Since(localStatus.LastUpdated.Time) > step0Timeout {
		logger.Info("Step 0 timeout exceeded waiting for target site", "timeout", step0Timeout)
		r.event(exec, corev1.EventTypeWarning, "Step0Timeout", "PlannedMigration",
			fmt.Sprintf("Step 0 timed out after %s waiting for target site for plan %s",
				step0Timeout, plan.Name))
		return r.failExecution(ctx, exec, "Step0Timeout",
			fmt.Sprintf("Target site did not set Step0Complete within %s", step0Timeout), plan)
	}

	// Wait for Step0Complete from the target site.
	remote := r.otherSite(plan)
	remoteStatus := getSiteStatus(exec, remote)
	if !remoteStatus.Step0Complete {
		logger.V(1).Info("Waiting for Step0Complete from target site", "remoteSite", remote)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	logger.Info("Step 0 completed, source site work is done")
	r.event(exec, corev1.EventTypeNormal, "Step0Completed", "PlannedMigration",
		fmt.Sprintf("Source site Step 0 completed for plan %s", plan.Name))

	return ctrl.Result{}, nil
}

// reconcileTargetStep0 handles the target site's role in multi-site planned
// migration Step 0. The target site:
//  1. Waits for DemotionComplete from siteStatuses[otherSite] (source has
//     demoted its VRs).
//  2. Calls SetSource (promote) on all local VRs.
//  3. Sets Step0Complete in siteStatuses[localSite] so wave execution proceeds.
func (r *DRExecutionReconciler) reconcileTargetStep0(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "role", "TargetSite")

	localStatus := getSiteStatus(exec, r.LocalSite)

	// Idempotent: if Step0Complete already set, target's job is done.
	if localStatus.Step0Complete {
		logger.V(1).Info("Step0Complete already set, target site Step 0 is done")
		return ctrl.Result{}, nil
	}

	remote := r.otherSite(plan)
	remoteStatus := getSiteStatus(exec, remote)

	if !remoteStatus.DemotionComplete {
		logger.V(1).Info("Waiting for source site to complete demotion")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	logger.Info("Source site demotion complete, promoting local VRs")

	// Call SetSource on all local VRs to promote to primary.
	if r.WaveExecutor != nil {
		allGroups, err := r.WaveExecutor.BuildExecutionGroups(ctx, plan)
		if err != nil {
			logger.Error(err, "Failed to build execution groups for SetSource")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		type vgKey struct{ name, namespace string }
		seenVG := make(map[vgKey]bool)
		for _, g := range allGroups {
			for _, vg := range g.Chunk.VolumeGroups {
				k := vgKey{name: vg.Name, namespace: vg.Namespace}
				if seenVG[k] {
					continue
				}
				seenVG[k] = true

				driver := g.DriverForVG(vg.Name)
				vgID := drivers.VolumeGroupIDFor(g.DriverType, vg.Namespace, vg.Name)
				if err := driver.SetSource(ctx, vgID); err != nil {
					if errors.Is(err, drivers.ErrVolumeGroupNotFound) {
						logger.Info("VR/VGR not yet created on target site, waiting",
							"volumeGroup", vg.Name)
						return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
					}
					logger.Error(err, "SetSource failed on target site", "volumeGroup", vg.Name)
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}
			}
		}
	}

	// Set Step0Complete in local site status.
	execPatch := client.MergeFrom(exec.DeepCopy())
	localStatus.Step0Complete = true
	setSiteStatus(exec, r.LocalSite, localStatus)
	if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
		logger.Error(err, "Failed to set Step0Complete in site status")
		return ctrl.Result{}, err
	}

	r.event(exec, corev1.EventTypeNormal, "Step0Completed", "PlannedMigration",
		fmt.Sprintf("Target site Step 0 completed for plan %s (VRs promoted)", plan.Name))

	logger.Info("Step 0 completed, target site VRs promoted")
	return ctrl.Result{}, nil
}

// reconcileResyncGate handles the single-site Step 0 completion path for
// planned migration. After PreExecute has stopped VMs and called
// StopReplication (demoting source VRs), this method:
//  1. Checks VRs reached role=Target (state=Secondary) via checkVRsHealthy.
//  2. When confirmed, calls SetSource on all VGs to promote to primary.
//  3. Sets Step0Complete condition.
//
// If the timeout (plan.Spec.ResyncTimeout) expires before VRs reach
// state=Secondary, the execution is failed.
func (r *DRExecutionReconciler) reconcileResyncGate(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	step0Timeout := defaultStep0Timeout
	if plan.Spec.ResyncTimeout != nil {
		step0Timeout = plan.Spec.ResyncTimeout.Duration
	}

	// Check VRs reached role=Target (state=Secondary).
	healthy, err := r.checkVRsHealthy(ctx, plan)
	if err != nil {
		logger.Error(err, "Failed to check VR role")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if !healthy {
		baseline := exec.Status.StartTime
		if step0Started := meta.FindStatusCondition(exec.Status.Conditions, "Step0Started"); step0Started != nil {
			baseline = &step0Started.LastTransitionTime
		}
		if baseline != nil && time.Since(baseline.Time) > step0Timeout {
			logger.Info("Demotion timeout exceeded, VRs did not reach Secondary state", "timeout", step0Timeout)
			r.event(exec, corev1.EventTypeWarning, "Step0Timeout", "PlannedMigration",
				fmt.Sprintf("Step 0 timed out after %s for plan %s", step0Timeout, plan.Name))
			return r.failExecution(ctx, exec, "Step0Timeout",
				fmt.Sprintf("VRs did not reach state=Secondary within %s", step0Timeout), plan)
		}
		logger.V(1).Info("Demoted VRs not yet Secondary, waiting")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	logger.Info("VRs confirmed Secondary, promoting to primary (SetSource)")

	// VRs confirmed Secondary — call SetSource on all VGs to promote to primary.
	if r.WaveExecutor != nil {
		allGroups, err := r.WaveExecutor.BuildExecutionGroups(ctx, plan)
		if err != nil {
			logger.Error(err, "Failed to build execution groups for SetSource")
			return r.failExecution(ctx, exec, "SetSourceFailed",
				fmt.Sprintf("building execution groups: %v", err), plan)
		}

		type vgKey struct{ name, namespace string }
		seenVG := make(map[vgKey]bool)
		for _, g := range allGroups {
			for _, vg := range g.Chunk.VolumeGroups {
				k := vgKey{name: vg.Name, namespace: vg.Namespace}
				if seenVG[k] {
					continue
				}
				seenVG[k] = true

				driver := g.DriverForVG(vg.Name)
				vgID := drivers.VolumeGroupIDFor(g.DriverType, vg.Namespace, vg.Name)
				if err := driver.SetSource(ctx, vgID); err != nil {
					logger.Error(err, "SetSource failed", "volumeGroup", vg.Name)
					return r.failExecution(ctx, exec, "SetSourceFailed",
						fmt.Sprintf("promoting volume group %s to primary: %v",
							vg.Name, err), plan)
				}
			}
		}
	}

	// Set Step0Complete condition.
	execPatch := client.MergeFrom(exec.DeepCopy())
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Step0Complete",
		Status:             metav1.ConditionTrue,
		Reason:             "DemotionAndPromotionCompleted",
		Message:            "VRs demoted, confirmed Secondary, and promoted to primary",
		ObservedGeneration: exec.Generation,
	})
	if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
		return ctrl.Result{}, err
	}

	r.event(exec, corev1.EventTypeNormal, "Step0Completed", "PlannedMigration",
		fmt.Sprintf("Step 0 completed for plan %s (demote + promote)", plan.Name))

	logger.Info("Step 0 completed (demote + promote)")
	return ctrl.Result{RequeueAfter: 1 * time.Millisecond}, nil
}

// checkVRsHealthy checks whether all VR/VGR CRs for a plan show
// role=Target (Secondary). After StopReplication (demotion), VRs transition
// to state=Secondary which maps to role=Target. Health conditions
// (Completed/Degraded) are not checked because, after demotion with no
// active primary, CSI Addons cannot produce a valid health signal — the
// demotion snapshot is delivered by rbd-mirror, not by a sync cycle.
// When no VR/VGR CRs exist (noop driver), returns true immediately.
func (r *DRExecutionReconciler) checkVRsHealthy(
	ctx context.Context, plan *soteriav1alpha1.DRPlan,
) (bool, error) {
	if r.WaveExecutor == nil || len(plan.Status.Waves) == 0 {
		return true, nil
	}

	driverType := plan.Spec.VolumeReplicationDriver.Type

	seen := make(map[string]bool)
	for _, wave := range plan.Status.Waves {
		for _, vg := range wave.Groups {
			key := vg.Namespace + "/" + vg.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			drv, err := r.WaveExecutor.ResolveVGDriver(ctx, driverType)
			if err != nil {
				return false, fmt.Errorf("resolving driver for VG %s: %w", vg.Name, err)
			}

			vgID := drivers.VolumeGroupIDFor(driverType, vg.Namespace, vg.Name)
			status, err := drv.GetReplicationStatus(ctx, vgID)
			if err != nil {
				if errors.Is(err, drivers.ErrVolumeGroupNotFound) {
					continue
				}
				return false, fmt.Errorf("checking replication status for VG %s: %w", vg.Name, err)
			}

			if status.Role == drivers.RoleNonReplicated {
				continue
			}
			if status.Role != drivers.RoleTarget {
				return false, nil
			}
		}
	}

	return true, nil
}

// pendingVGs returns a comma-separated list of VG names whose role is not yet
// Target (Secondary). Used in timeout messages to identify blocking VGs.
func (r *DRExecutionReconciler) pendingVGs(
	ctx context.Context, plan *soteriav1alpha1.DRPlan,
) string {
	if r.WaveExecutor == nil || len(plan.Status.Waves) == 0 {
		return ""
	}

	driverType := plan.Spec.VolumeReplicationDriver.Type

	var pending []string
	seen := make(map[string]bool)
	for _, wave := range plan.Status.Waves {
		for _, vg := range wave.Groups {
			key := vg.Namespace + "/" + vg.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			drv, err := r.WaveExecutor.ResolveVGDriver(ctx, driverType)
			if err != nil {
				continue
			}

			vgID := drivers.VolumeGroupIDFor(driverType, vg.Namespace, vg.Name)
			status, err := drv.GetReplicationStatus(ctx, vgID)
			if err != nil || status.Role == drivers.RoleNonReplicated {
				continue
			}
			if status.Role != drivers.RoleTarget {
				pending = append(pending, vg.Name)
			}
		}
	}

	return strings.Join(pending, ", ")
}

func (r *DRExecutionReconciler) event(
	obj client.Object, eventType, reason, action, msg string,
) {
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, eventType, reason, action, msg)
	}
}

// specOrAnnotationChanged is a predicate that suppresses reconciles triggered
// by status-only or label-only updates on DRExecution. During execution the
// wave executor and checkpointer write status frequently; without this filter
// each write re-enqueues the reconciler, creating a self-contention storm that
// exhausts checkpoint retries on wave-1.
//
// We still trigger on annotation changes so that the retry annotation
// (soteria.io/retry-groups) is picked up promptly.
func specOrAnnotationChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool { return true },
		DeleteFunc: func(_ event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return true
			}
			if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
				return true
			}
			oldAnnotations := e.ObjectOld.GetAnnotations()
			newAnnotations := e.ObjectNew.GetAnnotations()
			if len(oldAnnotations) != len(newAnnotations) {
				return true
			}
			for k, v := range oldAnnotations {
				if newAnnotations[k] != v {
					return true
				}
			}
			return false
		},
		GenericFunc: func(_ event.GenericEvent) bool { return true },
	}
}

func (r *DRExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	bld := ctrl.NewControllerManagedBy(mgr).
		For(&soteriav1alpha1.DRExecution{},
			builder.WithPredicates(specOrAnnotationChanged()))

	// Watch VirtualMachines for printableStatus changes so the wave gate can
	// detect when VMs reach Running. The mapper routes VM events to the
	// active DRExecution by querying DRExecutions with the soteria.io/plan-name
	// label (derived from DRExecution resources, not DRPlan status).
	if r.VMManager != nil {
		bld = bld.Watches(
			&kubevirtv1.VirtualMachine{},
			handler.EnqueueRequestsFromMapFunc(r.mapVMToDRExecution),
			builder.WithPredicates(vmPrintableStatusChanged()),
		)
	}

	// Watch VolumeReplication and VolumeGroupReplication for status changes
	// so the resync gate can detect completion event-driven rather than by
	// polling. The mapper routes VR/VGR events to the active DRExecution
	// via the soteria.io/drplan label on the VR/VGR → soteria.io/plan-name
	// label on DRExecution.
	bld = bld.Watches(
		&replicationv1alpha1.VolumeReplication{},
		handler.EnqueueRequestsFromMapFunc(r.mapVRToDRExecution),
		builder.WithPredicates(vrStatusChangePredicate()),
	).Watches(
		&replicationv1alpha1.VolumeGroupReplication{},
		handler.EnqueueRequestsFromMapFunc(r.mapVRToDRExecution),
		builder.WithPredicates(vrStatusChangePredicate()),
	)

	return bld.Complete(r)
}

// vmPrintableStatusChanged filters VM update events to only those where
// status.printableStatus changed. This prevents reconcile noise from frequent
// VM condition updates that don't indicate a readiness state change.
func vmPrintableStatusChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(_ event.CreateEvent) bool { return false },
		DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
		GenericFunc: func(_ event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldVM, ok1 := e.ObjectOld.(*kubevirtv1.VirtualMachine)
			newVM, ok2 := e.ObjectNew.(*kubevirtv1.VirtualMachine)
			if !ok1 || !ok2 {
				return false
			}
			return oldVM.Status.PrintableStatus != newVM.Status.PrintableStatus
		},
	}
}

// mapVMToDRExecution maps a VirtualMachine event to the active DRExecution
// (if any) by querying DRExecutions with the soteria.io/plan-name label.
func (r *DRExecutionReconciler) mapVMToDRExecution(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	planName := obj.GetLabels()[soteriav1alpha1.DRPlanLabel]
	if planName == "" {
		return nil
	}

	var execList soteriav1alpha1.DRExecutionList
	if err := r.List(ctx, &execList, client.MatchingLabels{
		soteriav1alpha1.PlanNameLabel: planName,
	}); err != nil {
		return nil
	}

	for i := range execList.Items {
		if !execList.Items[i].Status.IsTerminal() {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{Name: execList.Items[i].Name},
			}}
		}
	}
	return nil
}

// vrStatusChangePredicate filters VR/VGR update events to only those where
// status.state, status.conditions, or status.lastSyncTime changed. Create,
// Delete, and Generic events are suppressed — the resync gate only cares
// about in-flight status transitions.
func vrStatusChangePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(_ event.CreateEvent) bool { return false },
		DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
		GenericFunc: func(_ event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return vrStatusDiffers(e.ObjectOld, e.ObjectNew)
		},
	}
}

// vrStatusDiffers returns true if the replication status (state, conditions,
// or lastSyncTime) differs between old and new objects.
func vrStatusDiffers(oldObj, newObj client.Object) bool {
	switch oldVR := oldObj.(type) {
	case *replicationv1alpha1.VolumeReplication:
		newVR, ok := newObj.(*replicationv1alpha1.VolumeReplication)
		if !ok {
			return false
		}
		if oldVR.Status.State != newVR.Status.State {
			return true
		}
		if !reflect.DeepEqual(oldVR.Status.Conditions, newVR.Status.Conditions) {
			return true
		}
		return !lastSyncTimeEqual(oldVR.Status.LastSyncTime, newVR.Status.LastSyncTime)

	case *replicationv1alpha1.VolumeGroupReplication:
		newVGR, ok := newObj.(*replicationv1alpha1.VolumeGroupReplication)
		if !ok {
			return false
		}
		if oldVR.Status.State != newVGR.Status.State {
			return true
		}
		if !reflect.DeepEqual(oldVR.Status.Conditions, newVGR.Status.Conditions) {
			return true
		}
		return !lastSyncTimeEqual(oldVR.Status.LastSyncTime, newVGR.Status.LastSyncTime)
	}
	return false
}

// lastSyncTimeEqual returns true if both times are nil, or both are non-nil
// and represent the same instant.
func lastSyncTimeEqual(a, b *metav1.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(b)
}

// mapVRToDRExecution maps a VolumeReplication or VolumeGroupReplication event
// to the active DRExecution (if any) by reading the soteria.io/drplan label
// and querying DRExecutions with the soteria.io/plan-name label.
func (r *DRExecutionReconciler) mapVRToDRExecution(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	planName := obj.GetLabels()[drivers.LabelDRPlan]
	if planName == "" {
		return nil
	}

	var execList soteriav1alpha1.DRExecutionList
	if err := r.List(ctx, &execList, client.MatchingLabels{
		soteriav1alpha1.PlanNameLabel: planName,
	}); err != nil {
		return nil
	}

	for i := range execList.Items {
		if !execList.Items[i].Status.IsTerminal() {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{Name: execList.Items[i].Name},
			}}
		}
	}
	return nil
}
