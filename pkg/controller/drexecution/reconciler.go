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
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
// +kubebuilder:rbac:groups=soteria.io,resources=drgroupstatuses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=soteria.io,resources=drgroupstatuses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list

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
}

func (r *DRExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", req.Name)
	logger.V(1).Info("Reconciling DRExecution")

	var exec soteriav1alpha1.DRExecution
	if err := r.Get(ctx, req.NamespacedName, &exec); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Idempotency: skip if the execution has reached a terminal result.
	// PartiallySucceeded is re-openable via the retry annotation — handle
	// that path separately below.
	if exec.Status.Result == soteriav1alpha1.ExecutionResultSucceeded ||
		exec.Status.Result == soteriav1alpha1.ExecutionResultFailed {
		logger.V(1).Info("DRExecution already completed, skipping", "result", exec.Status.Result)
		return ctrl.Result{}, nil
	}

	// Fetch the referenced DRPlan early — needed for site-aware role gating.
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Referenced DRPlan not found", "plan", exec.Spec.PlanName)
			return r.failExecution(ctx, &exec, "PlanNotFound",
				fmt.Sprintf("DRPlan %q not found", exec.Spec.PlanName))
		}
		return ctrl.Result{}, err
	}

	// Site-aware reconcile ownership: compute the role and dispatch.
	if r.LocalSite != "" {
		if result, done, err := r.dispatchByRole(ctx, &exec, &plan); done || err != nil {
			return result, err
		}
	}

	// Resume path: in-progress execution needs resume after restart.
	// StartTime != nil means the controller already dispatched this execution.
	// Result == "" (empty) means execution is still in-progress (not terminal).
	if exec.Status.StartTime != nil && exec.Status.Result == "" {
		if exec.Spec.Mode == soteriav1alpha1.ExecutionModeReprotect {
			return r.reconcileReprotectResume(ctx, &exec)
		}

		// Wave progress path: if any wave has WaitingForVMReady groups, drive
		// the readiness state machine instead of re-executing handler operations.
		if hasWaitingForVMReady(&exec) {
			return r.reconcileWaveProgress(ctx, &exec, &plan)
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
		result, done, err := r.reconcileSetup(ctx, &exec, &plan, req.NamespacedName)
		if err != nil || done {
			return result, err
		}
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

// reconcileWaveExecution drives the wave-by-wave execution pipeline with VM
// readiness gates between waves. On each reconcile it either: (a) initializes
// waves, (b) executes the current wave's handler ops, (c) checks VM readiness
// for a WaitingForVMReady wave, or (d) finishes the execution.
func (r *DRExecutionReconciler) reconcileWaveExecution(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	if r.WaveExecutor == nil {
		return ctrl.Result{}, nil
	}

	hdl, err := r.resolveHandler(exec.Spec.Mode)
	if err != nil {
		return r.failExecution(ctx, exec, "HandlerResolutionFailed", err.Error(), plan)
	}

	// Step 0 for planned migration.
	step0Done := meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Complete")
	if exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration && !step0Done {
		if r.LocalSite != "" {
			logger.V(1).Info("Waiting for source site to complete Step 0")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
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
			execPatch := client.MergeFrom(exec.DeepCopy())
			meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
				Type:               "Step0Complete",
				Status:             metav1.ConditionTrue,
				Reason:             "PreExecutionCompleted",
				Message:            "Step 0 completed successfully",
				ObservedGeneration: exec.Generation,
			})
			if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
				return ctrl.Result{}, err
			}
			r.event(exec, corev1.EventTypeNormal, "PlannedMigrationStarted", "PlannedMigration",
				fmt.Sprintf("Planned migration Step 0 completed for plan %s", plan.Name))
		}
	}

	// Initialize waves if not yet done.
	if len(exec.Status.Waves) == 0 {
		if err := r.WaveExecutor.InitializeWaves(ctx, exec, plan); err != nil {
			logger.Error(err, "Wave initialization failed")
			return ctrl.Result{}, err
		}
		// If InitializeWaves already finished (0 VMs), return.
		if exec.Status.Result != "" {
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
	exec.Status.CompletionTime = &now

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

	// Advance DRPlan phase and clear ActiveExecution on success or partial
	// success (AC6: timeout still advances). On failure, clear ActiveExecution
	// only — phase stays at the current rest state (self-healing).
	if execResult == soteriav1alpha1.ExecutionResultSucceeded ||
		execResult == soteriav1alpha1.ExecutionResultPartiallySucceeded {
		previousPhase := plan.Status.Phase
		newPhase, err := engine.RestStateAfterCompletion(plan.Status.Phase, exec.Spec.Mode)
		if err != nil {
			logger.Error(err, "Could not complete phase transition", "currentPhase", plan.Status.Phase)
		} else {
			planPatch := client.MergeFrom(planPreExec)
			plan.Status.Phase = newPhase
			plan.Status.ActiveExecution = ""
			plan.Status.ActiveExecutionMode = ""
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

	// Always clear ActiveExecution when it wasn't already cleared above.
	if plan.Status.ActiveExecution != "" {
		planPatch := client.MergeFrom(planPreExec)
		plan.Status.ActiveExecution = ""
		plan.Status.ActiveExecutionMode = ""
		if err := r.Status().Patch(ctx, plan, planPatch); err != nil {
			logger.Error(err, "Failed to clear ActiveExecution on DRPlan", "plan", plan.Name)
			return ctrl.Result{}, err
		}
		logger.Info("Cleared ActiveExecution on DRPlan", "plan", plan.Name)
	}

	return ctrl.Result{}, nil
}

// buildVolumeGroupEntries collects all volume groups from the plan's wave
// status, resolves a driver per VG, and resolves VolumeGroupIDs via
// CreateVolumeGroup (idempotent). This gives the ReprotectHandler everything
// it needs without depending on the wave executor.
func (r *DRExecutionReconciler) buildVolumeGroupEntries(
	ctx context.Context, plan *soteriav1alpha1.DRPlan,
) ([]engine.VolumeGroupEntry, error) {
	if r.WaveExecutor == nil {
		return nil, fmt.Errorf("WaveExecutor required for VG resolution")
	}

	var entries []engine.VolumeGroupEntry
	seen := make(map[string]bool)

	for _, wave := range plan.Status.Waves {
		for _, vg := range wave.Groups {
			key := vg.Namespace + "/" + vg.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			drv, err := r.WaveExecutor.ResolveVGDriver(ctx, vg)
			if err != nil {
				return nil, fmt.Errorf("resolving driver for volume group %s: %w", vg.Name, err)
			}

			vgID, err := r.resolveVGID(ctx, drv, vg)
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

	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		if errors.IsNotFound(err) {
			return r.failExecution(ctx, exec, "PlanNotFound",
				fmt.Sprintf("DRPlan %q not found during re-protect resume", exec.Spec.PlanName))
		}
		return ctrl.Result{}, err
	}

	if plan.Status.ActiveExecution != exec.Name {
		return r.failExecution(ctx, exec, "StaleExecution",
			fmt.Sprintf("execution %q is not the active execution for plan %q (active: %q)",
				exec.Name, plan.Name, plan.Status.ActiveExecution))
	}

	r.event(exec, corev1.EventTypeNormal, "ReprotectResumed", "Checkpoint",
		"Resuming re-protect execution after restart (idempotent replay)")

	return r.reconcileReprotect(ctx, exec, &plan)
}

// resolveVGID resolves a VolumeGroupInfo to a driver-level VolumeGroupID
// via CreateVolumeGroup (idempotent).
func (r *DRExecutionReconciler) resolveVGID(
	ctx context.Context, drv drivers.StorageProvider, vg soteriav1alpha1.VolumeGroupInfo,
) (drivers.VolumeGroupID, error) {
	var pvcNames []string
	if r.WaveExecutor != nil && r.WaveExecutor.PVCResolver != nil {
		for _, vmName := range vg.VMNames {
			names, err := r.WaveExecutor.PVCResolver.ResolvePVCNames(ctx, vmName, vg.Namespace)
			if err != nil {
				return "", fmt.Errorf("resolving PVC names for VM %s/%s: %w", vg.Namespace, vmName, err)
			}
			pvcNames = append(pvcNames, names...)
		}
	}

	info, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      vg.Name,
		Namespace: vg.Namespace,
		PVCNames:  pvcNames,
	})
	if err != nil {
		return "", fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
	}
	return info.ID, nil
}

// reconcileResume handles the resume path for in-progress executions after
// a pod restart or leader failover. It analyzes the execution status to
// determine the resume point, resets in-flight groups to Pending, and
// dispatches the wave executor from the resume wave.
func (r *DRExecutionReconciler) reconcileResume(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	// Multi-site planned migration: the Owner must wait for the source site
	// to complete Step 0 before running waves, even on the resume path.
	if r.LocalSite != "" &&
		exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration &&
		!meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Complete") {
		logger.V(1).Info("Waiting for source site to complete Step 0 (resume path)")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
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

	// Fetch the referenced DRPlan.
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		if errors.IsNotFound(err) {
			return r.failExecution(ctx, exec, "PlanNotFound",
				fmt.Sprintf("DRPlan %q not found during resume", exec.Spec.PlanName))
		}
		return ctrl.Result{}, err
	}

	if plan.Status.ActiveExecution != exec.Name {
		return r.failExecution(ctx, exec, "StaleExecution",
			fmt.Sprintf("execution %q is not the active execution for plan %q (active: %q)",
				exec.Name, plan.Name, plan.Status.ActiveExecution))
	}

	// Resolve handler for the execution mode.
	drHandler, err := r.resolveHandler(exec.Spec.Mode)
	if err != nil {
		return r.failExecution(ctx, exec, "HandlerResolutionFailed", err.Error(), &plan)
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
			Plan:      &plan,
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
// When plan is non-nil and its ActiveExecution matches exec.Name, clears the
// pointer so the plan returns to its rest state — this is the self-healing
// property that prevents stuck transient phases.
func (r *DRExecutionReconciler) failExecution(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	reason, message string,
	plan ...*soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "reason", reason)

	now := metav1.Now()
	patch := client.MergeFrom(exec.DeepCopy())
	exec.Status.Result = soteriav1alpha1.ExecutionResultFailed
	if exec.Status.StartTime == nil {
		exec.Status.StartTime = &now
	}
	exec.Status.CompletionTime = &now
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

	// Clear ActiveExecution on the plan if this execution owns the pointer.
	if len(plan) > 0 && plan[0] != nil && plan[0].Status.ActiveExecution == exec.Name {
		planPatch := client.MergeFrom(plan[0].DeepCopy())
		plan[0].Status.ActiveExecution = ""
		plan[0].Status.ActiveExecutionMode = ""
		if err := r.Status().Patch(ctx, plan[0], planPatch); err != nil {
			logger.Error(err, "Failed to clear ActiveExecution on DRPlan", "plan", plan[0].Name)
			return ctrl.Result{}, err
		}
		logger.Info("Cleared ActiveExecution on DRPlan", "plan", plan[0].Name)
	}

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
			VMManager:        r.VMManager,
			Config:           engine.FailoverConfig{GracefulShutdown: true, Force: false},
			SyncPollInterval: 2 * time.Second,
			SyncTimeout:      10 * time.Minute,
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
			VMManager:        r.VMManager,
			Config:           engine.FailoverConfig{GracefulShutdown: false, Force: true},
			SyncPollInterval: 2 * time.Second,
			SyncTimeout:      10 * time.Minute,
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

	// VM health validation for all VMs in retry groups.
	if r.WaveExecutor != nil && r.WaveExecutor.VMHealthValidator != nil {
		for _, target := range targets {
			groupStatus := exec.Status.Waves[target.WaveIndex].Groups[target.GroupIndex]
			for _, vmName := range groupStatus.VMNames {
				ns := r.resolveVMNamespace(exec, target, vmName)
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

	// Fetch the plan for chunk reconstruction.
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		logger.Error(err, "Failed to fetch DRPlan for retry")
		return ctrl.Result{}, err
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

// resolveVMNamespace finds the namespace for a VM in the retry target's wave.
func (r *DRExecutionReconciler) resolveVMNamespace(
	exec *soteriav1alpha1.DRExecution, target engine.RetryTarget, vmName string,
) string {
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(context.Background(), client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		return ""
	}
	if target.WaveIndex < len(plan.Status.Waves) {
		for _, dvm := range plan.Status.Waves[target.WaveIndex].VMs {
			if dvm.Name == vmName {
				return dvm.Namespace
			}
		}
	}
	return ""
}

// recordExecutionMetrics observes the failover duration histogram and increments
// the execution counter when a DRExecution reaches a terminal state.
func (r *DRExecutionReconciler) recordExecutionMetrics(exec *soteriav1alpha1.DRExecution) {
	if exec.Status.StartTime == nil || exec.Status.CompletionTime == nil || exec.Status.Result == "" {
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

// ensurePlanNameLabel sets soteria.io/plan-name on the DRExecution for
// history queries (FR42). Uses retry.RetryOnConflict to handle concurrent
// resourceVersion bumps from status patches or informer replays.
func (r *DRExecutionReconciler) ensurePlanNameLabel(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, key client.ObjectKey,
) error {
	if exec.Labels != nil && exec.Labels["soteria.io/plan-name"] == exec.Spec.PlanName {
		return nil
	}
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, key, exec); err != nil {
			return err
		}
		if exec.Labels != nil && exec.Labels["soteria.io/plan-name"] == exec.Spec.PlanName {
			return nil
		}
		if exec.Labels == nil {
			exec.Labels = make(map[string]string)
		}
		exec.Labels["soteria.io/plan-name"] = exec.Spec.PlanName
		return r.Update(ctx, exec)
	})
	if err != nil {
		logger.Error(err, "Failed to set plan-name label", "label", "soteria.io/plan-name")
		return err
	}

	if err := r.Get(ctx, key, exec); err != nil {
		return err
	}
	logger.Info("Set plan-name label", "label", "soteria.io/plan-name", "value", exec.Spec.PlanName)
	return nil
}

// reconcileSetup validates the execution mode, transitions the DRPlan phase,
// sets the concurrency guard, and initializes the execution's StartTime.
// Returns (result, done=true, err) when the caller should return immediately
// (either because of an error or because the execution was failed).
func (r *DRExecutionReconciler) reconcileSetup(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
	key client.ObjectKey,
) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name)

	if exec.Spec.Mode != soteriav1alpha1.ExecutionModePlannedMigration &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeDisaster &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeReprotect {
		result, err := r.failExecution(ctx, exec, "InvalidMode",
			fmt.Sprintf("unsupported execution mode %q", exec.Spec.Mode))
		return result, true, err
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
		return result, true, fErr
	}

	planPatch := client.MergeFrom(plan.DeepCopy())
	plan.Status.ActiveExecution = exec.Name
	plan.Status.ActiveExecutionMode = exec.Spec.Mode
	if err := r.Status().Patch(ctx, plan, planPatch); err != nil {
		logger.Error(err, "Failed to set ActiveExecution on DRPlan", "plan", plan.Name)
		return ctrl.Result{}, true, err
	}

	logger.Info("Set ActiveExecution on DRPlan",
		"plan", plan.Name, "activeExecution", exec.Name, "phase", previousPhase)

	if err := r.ensurePlanNameLabel(ctx, exec, key); err != nil {
		return ctrl.Result{}, true, err
	}

	now := metav1.Now()
	execPatch := client.MergeFrom(exec.DeepCopy())
	exec.Status.StartTime = &now
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionTrue,
		Reason:             "ExecutionStarted",
		Message:            fmt.Sprintf("Execution started for plan %s in %s mode", plan.Name, exec.Spec.Mode),
		ObservedGeneration: exec.Generation,
	})

	if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
		logger.Error(err, "Failed to update DRExecution status")
		return ctrl.Result{}, true, err
	}

	reason, action, verb := startEventFields(targetPhase)
	r.event(plan, corev1.EventTypeNormal, reason, action,
		fmt.Sprintf("%s started for plan %s in %s mode via execution %s",
			verb, plan.Name, exec.Spec.Mode, exec.Name))

	logger.Info("DRExecution setup complete",
		"plan", plan.Name, "mode", exec.Spec.Mode, "effectivePhase", targetPhase)
	return ctrl.Result{}, false, nil
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
	default:
		return ctrl.Result{}, false, nil
	}
}

// reconcileStep0 runs the source site's Step 0 for planned migration:
// stop VMs, stop replication, wait for sync. Once complete, it sets the
// Step0Complete condition on the DRExecution status so the target site
// (watching with RequeueAfter) can proceed with wave execution. This
// method is idempotent — if Step0Complete is already set, it returns
// immediately without touching the DRExecution again.
func (r *DRExecutionReconciler) reconcileStep0(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "role", "Step0")

	// Idempotent: if Step0Complete already set, source site's job is done.
	if meta.IsStatusConditionTrue(exec.Status.Conditions, "Step0Complete") {
		logger.V(1).Info("Step0Complete already set, source site work is done")
		return ctrl.Result{}, nil
	}

	// Guard: Step 0 only applies to in-progress planned migration executions.
	if exec.Status.StartTime == nil {
		logger.V(1).Info("Execution not yet started, waiting for target site setup")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	logger.Info("Running Step 0 (source site planned migration)")

	// Step 0 logic: stop VMs → stop replication → wait for sync.
	// This reuses the existing PreExecute path from the FailoverHandler.
	drHandler, err := r.resolveHandler(exec.Spec.Mode)
	if err != nil {
		logger.Error(err, "Failed to resolve handler for Step 0")
		return ctrl.Result{}, err
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

	// Mark Step0Complete so the target site can proceed with waves.
	execPatch := client.MergeFrom(exec.DeepCopy())
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Step0Complete",
		Status:             metav1.ConditionTrue,
		Reason:             "SourceSiteStep0Completed",
		Message:            "Source site completed Step 0 (stop VMs, stop replication, sync wait)",
		ObservedGeneration: exec.Generation,
	})
	if err := r.Status().Patch(ctx, exec, execPatch); err != nil {
		logger.Error(err, "Failed to set Step0Complete condition")
		return ctrl.Result{}, err
	}

	r.event(exec, corev1.EventTypeNormal, "Step0Completed", "PlannedMigration",
		fmt.Sprintf("Source site Step 0 completed for plan %s", plan.Name))

	logger.Info("Step 0 completed, source site work is done")
	return ctrl.Result{}, nil
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
	// active DRExecution via the soteria.io/drplan label → DRPlan.ActiveExecution.
	if r.VMManager != nil {
		bld = bld.Watches(
			&kubevirtv1.VirtualMachine{},
			handler.EnqueueRequestsFromMapFunc(r.mapVMToDRExecution),
			builder.WithPredicates(vmPrintableStatusChanged()),
		)
	}

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
// (if any) by reading the soteria.io/drplan label → DRPlan → ActiveExecution.
func (r *DRExecutionReconciler) mapVMToDRExecution(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	planName := obj.GetLabels()[soteriav1alpha1.DRPlanLabel]
	if planName == "" {
		return nil
	}

	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, types.NamespacedName{Name: planName}, &plan); err != nil {
		return nil
	}

	if plan.Status.ActiveExecution == "" {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: plan.Status.ActiveExecution},
	}}
}
