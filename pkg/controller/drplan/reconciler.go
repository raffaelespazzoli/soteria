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

// reconciler.go implements the DRPlan reconciliation loop.
//
// Architecture: On every reconcile the controller fetches the DRPlan, discovers
// VMs carrying the soteria.io/drplan label for this plan, partitions them into
// waves via engine.GroupByWave, then resolves volume group consistency from
// namespace annotations (engine.ResolveVolumeGroups) and chunks the result into
// DRGroups respecting maxConcurrentFailovers (engine.ChunkWaves). If
// namespace-level VMs span multiple waves, the plan is marked Ready=False with
// reason WaveConflict. If a namespace group exceeds the throttle, Ready=False
// with reason NamespaceGroupExceedsThrottle. Secondary watches on kubevirt
// VirtualMachines (label-change predicate with a custom event handler that
// enqueues both old and new plan on label change) and core Namespaces
// (consistency-annotation predicate) trigger reconciliation when VM membership
// or namespace consistency configuration changes.

package drplan

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
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

	"github.com/soteria-project/soteria/internal/preflight"
)

const (
	conditionTypeReady         = "Ready"
	reasonDiscovered           = "VMsDiscovered"
	reasonNoVMs                = "NoVMsDiscovered"
	reasonError                = "DiscoveryError"
	reasonWaveConflict         = "WaveConflict"
	reasonGroupExceedsThrottle = "NamespaceGroupExceedsThrottle"

	requeueInterval = 10 * time.Minute
)

// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// DRPlanReconciler reconciles DRPlan objects by discovering VMs, grouping them
// into execution waves, resolving volume group consistency, and chunking into
// DRGroups.
type DRPlanReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	VMDiscoverer    engine.VMDiscoverer
	NamespaceLookup engine.NamespaceLookup
	StorageResolver preflight.StorageBackendResolver
	Recorder        events.EventRecorder
	// Registry resolves CSI provisioner → StorageProvider for health polling.
	// When nil, replication health monitoring is skipped (backward compat).
	Registry    *drivers.Registry
	SCLister    drivers.StorageClassLister
	PVCResolver engine.PVCResolver
	// LocalSite is the --site-name flag value identifying which cluster
	// this controller instance runs on. Used to optimize VM discovery
	// and health polling to the local site.
	LocalSite string
}

func (r *DRPlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drplan", req.NamespacedName)

	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, req.NamespacedName, &plan); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("DRPlan not found, likely deleted")
			metrics.DeletePlanMetrics(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if r.LocalSite != "" {
		// Determine which site currently owns the VMs. Default to
		// PrimarySite when ActiveSite has not been set yet (initial state).
		activeSite := plan.Status.ActiveSite
		if activeSite == "" {
			activeSite = plan.Spec.PrimarySite
		}
		logger.V(1).Info("Site-aware DRPlan reconciliation",
			"localSite", r.LocalSite,
			"activeSite", activeSite,
			"primarySite", plan.Spec.PrimarySite,
			"secondarySite", plan.Spec.SecondarySite)

		// Each Soteria instance only talks to its local kube-apiserver
		// and local ScyllaDB. VMs exist exclusively on the active site,
		// so discovery on the non-active site returns 0 VMs and would
		// overwrite the correct plan status via ScyllaDB replication.
		if r.LocalSite != activeSite {
			logger.V(1).Info("Skipping discovery and health polling, not the active site")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}

	logger.Info("Starting reconciliation")

	vms, err := r.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
	if err != nil {
		logger.Error(err, "Failed to discover VMs")
		r.event(&plan, "Warning", "DiscoveryFailed", err.Error())
		report := r.composePreflightReport(ctx, &plan, nil, nil, nil, nil)
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("VM discovery failed: %v", err))
		_, statusErr := r.updateStatus(ctx, req, &plan, nil, 0, report, metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             reasonError,
			Message:            err.Error(),
			ObservedGeneration: plan.Generation,
		}, nil)
		if statusErr != nil {
			logger.Error(statusErr, "Failed to update status after discovery error")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	result := engine.GroupByWave(vms)

	waves := make([]soteriav1alpha1.WaveInfo, len(result.Waves))
	for i, wg := range result.Waves {
		discoveredVMs := make([]soteriav1alpha1.DiscoveredVM, len(wg.VMs))
		for j, vm := range wg.VMs {
			discoveredVMs[j] = soteriav1alpha1.DiscoveredVM{
				Name:      vm.Name,
				Namespace: vm.Namespace,
			}
		}
		waves[i] = soteriav1alpha1.WaveInfo{
			WaveKey: wg.WaveKey,
			VMs:     discoveredVMs,
		}
	}

	if result.TotalVMs == 0 {
		report := r.composePreflightReport(ctx, &plan, &result, nil, nil, vms)
		return r.updateStatus(ctx, req, &plan, waves, result.TotalVMs, report, metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             reasonNoVMs,
			Message:            "No VMs have the soteria.io/drplan label for this plan",
			ObservedGeneration: plan.Generation,
		}, nil)
	}

	// Resolve volume group consistency from namespace annotations.
	consistency, err := engine.ResolveVolumeGroups(ctx, vms, r.NamespaceLookup)
	if err != nil {
		logger.Error(err, "Failed to resolve volume groups")
		report := r.composePreflightReport(ctx, &plan, &result, nil, nil, vms)
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("Volume group resolution failed: %v", err))
		return r.updateStatus(ctx, req, &plan, waves, result.TotalVMs, report, metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             reasonError,
			Message:            fmt.Sprintf("Volume group resolution failed: %v", err),
			ObservedGeneration: plan.Generation,
		}, nil)
	}

	if len(consistency.WaveConflicts) > 0 {
		msg := formatWaveConflicts(consistency.WaveConflicts)
		logger.Info("Detected wave conflict", "conflicts", len(consistency.WaveConflicts))
		r.event(&plan, "Warning", "WaveConflictDetected", msg)
		report := r.composePreflightReport(ctx, &plan, &result, consistency, nil, vms)
		return r.updateStatus(ctx, req, &plan, waves, result.TotalVMs, report, metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             reasonWaveConflict,
			Message:            msg,
			ObservedGeneration: plan.Generation,
		}, nil)
	}

	// Populate volume groups on each wave.
	groupsByWave := buildGroupsByWave(consistency.VolumeGroups, vms)
	for i := range waves {
		waves[i].Groups = groupsByWave[waves[i].WaveKey]
	}

	nsLevelCount, vmLevelCount := countGroupLevels(consistency.VolumeGroups)
	logger.Info("Resolved volume groups", "namespaceLevel", nsLevelCount, "vmLevel", vmLevelCount)

	// Chunk waves into DRGroups respecting maxConcurrentFailovers.
	chunkInput := engine.ChunkInput{
		WaveGroups: make([]engine.WaveGroupWithVolumes, len(waves)),
	}
	for i, w := range waves {
		chunkInput.WaveGroups[i] = engine.WaveGroupWithVolumes{
			WaveKey:      w.WaveKey,
			VolumeGroups: w.Groups,
		}
	}

	chunkResult := engine.ChunkWaves(chunkInput, plan.Spec.MaxConcurrentFailovers)
	if len(chunkResult.Errors) > 0 {
		msg := formatChunkErrors(chunkResult.Errors, plan.Spec.MaxConcurrentFailovers)
		logger.Info("Chunking failed", "errors", len(chunkResult.Errors))
		r.event(&plan, "Warning", "ChunkingFailed", msg)
		report := r.composePreflightReport(
			ctx, &plan, &result, consistency, &chunkResult, vms)
		return r.updateStatus(ctx, req, &plan, waves, result.TotalVMs, report, metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             reasonGroupExceedsThrottle,
			Message:            msg,
			ObservedGeneration: plan.Generation,
		}, nil)
	}

	r.event(&plan, "Normal", "ConsistencyResolved",
		fmt.Sprintf("Resolved %d volume groups (%d namespace-level, %d VM-level)",
			len(consistency.VolumeGroups), nsLevelCount, vmLevelCount))

	// Resolve storage backends and compose the preflight report.
	report := r.composePreflightReport(ctx, &plan, &result, consistency, &chunkResult, vms)
	logger.Info("Preflight report generated",
		"totalVMs", report.TotalVMs, "warnings", len(report.Warnings))

	// Poll replication health when the driver infrastructure is wired and no
	// execution is active (the engine owns driver interactions during execution).
	var replicationHealth []soteriav1alpha1.VolumeGroupHealth
	if r.Registry != nil && plan.Status.ActiveExecution == "" {
		replicationHealth = r.pollReplicationHealth(ctx, &plan, waves)
		logger.V(1).Info("Replication health polled",
			"totalVGs", len(replicationHealth))
	}

	readyCond := metav1.Condition{
		Type:               conditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             reasonDiscovered,
		Message:            "VMs discovered and grouped into waves",
		ObservedGeneration: plan.Generation,
	}

	return r.updateStatus(ctx, req, &plan, waves, result.TotalVMs, report, readyCond, replicationHealth)
}

func (r *DRPlanReconciler) event(
	plan *soteriav1alpha1.DRPlan, eventType, reason, msg string,
) {
	if r.Recorder != nil {
		r.Recorder.Eventf(plan, nil, eventType, reason, "Reconcile", msg)
	}
}

func (r *DRPlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&soteriav1alpha1.DRPlan{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&kubevirtv1.VirtualMachine{},
			r.vmEventHandler(),
			builder.WithPredicates(vmRelevantChangePredicate()),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.mapNamespaceToDRPlans),
			builder.WithPredicates(nsConsistencyAnnotationChangePredicate()),
		).
		Complete(r)
}

type reqQueue = workqueue.TypedRateLimitingInterface[reconcile.Request]

// vmEventHandler returns a handler.Funcs that enqueues reconcile requests
// for the DRPlan(s) referenced by the VM's soteria.io/drplan label. On
// update, both old and new plan names are enqueued so that label changes
// promptly reconcile both the departing and arriving plan.
func (r *DRPlanReconciler) vmEventHandler() handler.Funcs {
	return handler.Funcs{
		CreateFunc: func(
			_ context.Context,
			e event.TypedCreateEvent[client.Object],
			q reqQueue,
		) {
			r.enqueueForVM(e.Object, q)
		},
		UpdateFunc: func(
			_ context.Context,
			e event.TypedUpdateEvent[client.Object],
			q reqQueue,
		) {
			r.enqueueForVM(e.ObjectOld, q)
			r.enqueueForVM(e.ObjectNew, q)
		},
		DeleteFunc: func(
			_ context.Context,
			e event.TypedDeleteEvent[client.Object],
			q reqQueue,
		) {
			r.enqueueForVM(e.Object, q)
		},
	}
}

// enqueueForVM reads the soteria.io/drplan label from a VM and enqueues
// a reconcile request for the named plan. DRPlan is cluster-scoped, so the
// namespace is always empty. O(1) — no DRPlan list needed.
func (r *DRPlanReconciler) enqueueForVM(obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if obj == nil {
		return
	}
	planName := obj.GetLabels()[soteriav1alpha1.DRPlanLabel]
	if planName == "" {
		return
	}
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: planName,
		},
	})
}

// mapNamespaceToDRPlans returns reconcile requests for every DRPlan that has
// already discovered VMs in the changed namespace. This ensures that a
// consistency-annotation change (e.g. adding soteria.io/consistency-level)
// re-evaluates volume groups promptly instead of waiting for the periodic
// requeue.
func (r *DRPlanReconciler) mapNamespaceToDRPlans(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	logger := log.FromContext(ctx)
	nsName := obj.GetName()

	var planList soteriav1alpha1.DRPlanList
	if err := r.List(ctx, &planList); err != nil {
		logger.Error(err, "Failed to list DRPlans for namespace mapping")
		return nil
	}

	var requests []reconcile.Request
	for i := range planList.Items {
		plan := &planList.Items[i]
		if planReferencesNamespace(plan, nsName) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: plan.Name,
				},
			})
		}
	}

	logger.V(2).Info("Namespace annotation change mapped to DRPlans",
		"namespace", nsName, "matchedPlans", len(requests))

	return requests
}

// planReferencesNamespace returns true if any VM in the plan's discovered
// waves belongs to the given namespace.
func planReferencesNamespace(
	plan *soteriav1alpha1.DRPlan, namespace string,
) bool {
	for _, wave := range plan.Status.Waves {
		for _, vm := range wave.VMs {
			if vm.Namespace == namespace {
				return true
			}
		}
	}
	return false
}

// nsConsistencyAnnotationChangePredicate fires only when the
// soteria.io/consistency-level annotation is added, changed, or removed.
// Other namespace mutations (labels, other annotations, status) are ignored.
func nsConsistencyAnnotationChangePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldVal := e.ObjectOld.GetAnnotations()[soteriav1alpha1.ConsistencyAnnotation]
			newVal := e.ObjectNew.GetAnnotations()[soteriav1alpha1.ConsistencyAnnotation]
			return oldVal != newVal
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}
}

func formatWaveConflicts(conflicts []engine.WaveConflict) string {
	var msg strings.Builder
	msg.WriteString("Namespace-level VMs span multiple waves:")
	for _, c := range conflicts {
		msg.WriteString(fmt.Sprintf(" namespace %q VMs %v in waves %v;", c.Namespace, c.VMNames, c.WaveKeys))
	}
	return msg.String()
}

func formatChunkErrors(
	chunkErrors []engine.ChunkError, maxConcurrent int,
) string {
	var msg strings.Builder
	for i, e := range chunkErrors {
		if i > 0 {
			msg.WriteString("; ")
		}
		fmt.Fprintf(&msg,
			"maxConcurrentFailovers (%d) is less than"+
				" namespace+wave group size (%d)"+
				" for namespace %s wave %s",
			maxConcurrent, e.GroupSize, e.Namespace, e.WaveKey)
	}
	return msg.String()
}

// buildGroupsByWave assigns VolumeGroups to the wave they belong to.
func buildGroupsByWave(
	groups []soteriav1alpha1.VolumeGroupInfo,
	vms []engine.VMReference,
) map[string][]soteriav1alpha1.VolumeGroupInfo {
	vmWave := make(map[string]string, len(vms))
	for _, vm := range vms {
		key := vm.Namespace + "/" + vm.Name
		vmWave[key] = vm.Labels[soteriav1alpha1.WaveLabel]
	}

	result := make(map[string][]soteriav1alpha1.VolumeGroupInfo)
	for _, g := range groups {
		if len(g.VMNames) > 0 {
			waveKey := vmWave[g.Namespace+"/"+g.VMNames[0]]
			result[waveKey] = append(result[waveKey], g)
		}
	}
	return result
}

func countGroupLevels(groups []soteriav1alpha1.VolumeGroupInfo) (nsLevel, vmLevel int) {
	for _, g := range groups {
		if g.ConsistencyLevel == soteriav1alpha1.ConsistencyLevelNamespace {
			nsLevel++
		} else {
			vmLevel++
		}
	}
	return
}

func (r *DRPlanReconciler) composePreflightReport(
	ctx context.Context,
	plan *soteriav1alpha1.DRPlan,
	discovery *engine.DiscoveryResult,
	consistency *engine.ConsistencyResult,
	chunks *engine.ChunkResult,
	vms []engine.VMReference,
) *soteriav1alpha1.PreflightReport {
	logger := log.FromContext(ctx)

	storageBackends := make(map[string]string)
	var storageWarnings []string

	if r.StorageResolver != nil && len(vms) > 0 {
		var err error
		storageBackends, storageWarnings, err = r.StorageResolver.ResolveBackends(ctx, vms)
		if err != nil {
			logger.Error(err, "Storage backend resolution failed")
			storageWarnings = append(storageWarnings,
				fmt.Sprintf("Storage backend resolution failed: %v", err))
		}
	}

	input := preflight.CompositionInput{
		Plan:              plan,
		DiscoveryResult:   discovery,
		ConsistencyResult: consistency,
		ChunkResult:       chunks,
		StorageBackends:   storageBackends,
	}

	now := metav1.Now()
	report := preflight.ComposeReport(input, now)
	report.Warnings = append(storageWarnings, report.Warnings...)

	return report
}

func (r *DRPlanReconciler) updateStatus(
	ctx context.Context,
	req ctrl.Request,
	plan *soteriav1alpha1.DRPlan,
	waves []soteriav1alpha1.WaveInfo,
	totalVMs int,
	preflightReport *soteriav1alpha1.PreflightReport,
	condition metav1.Condition,
	replicationHealth []soteriav1alpha1.VolumeGroupHealth,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.Get(ctx, req.NamespacedName, plan); err != nil {
		return ctrl.Result{}, err
	}

	oldWaves := plan.Status.Waves
	oldCondition := meta.FindStatusCondition(plan.Status.Conditions, condition.Type)
	conditionChanged := oldCondition == nil ||
		oldCondition.Status != condition.Status ||
		oldCondition.Reason != condition.Reason ||
		oldCondition.Message != condition.Message ||
		oldCondition.ObservedGeneration != condition.ObservedGeneration
	wavesChanged := !reflect.DeepEqual(oldWaves, waves)
	countChanged := plan.Status.DiscoveredVMCount != totalVMs
	genChanged := plan.Status.ObservedGeneration != plan.Generation
	reportChanged := preflightReportChanged(plan.Status.Preflight, preflightReport)
	healthChanged := replicationHealthChanged(plan.Status.ReplicationHealth, replicationHealth)

	anyChanged := conditionChanged || wavesChanged || countChanged ||
		genChanged || reportChanged || healthChanged
	if !anyChanged {
		logger.V(1).Info("Status unchanged, skipping patch")
		requeue := requeueInterval
		if anyNonHealthy(plan.Status.ReplicationHealth) {
			requeue = degradedRequeueInterval
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	// Detect health transitions for event emission before mutating status.
	degradedVGs, recoveredVGs := detectHealthTransitions(
		plan.Status.ReplicationHealth, replicationHealth)

	patch := client.MergeFrom(plan.DeepCopy())

	plan.Status.Waves = waves
	plan.Status.DiscoveredVMCount = totalVMs
	plan.Status.ObservedGeneration = plan.Generation
	plan.Status.Preflight = preflightReport
	meta.SetStatusCondition(&plan.Status.Conditions, condition)

	if replicationHealth != nil {
		plan.Status.ReplicationHealth = replicationHealth
		if replCond := computeReplicationCondition(replicationHealth, plan.Generation); replCond != nil {
			meta.SetStatusCondition(&plan.Status.Conditions, *replCond)
		}
	} else if plan.Status.ReplicationHealth != nil {
		plan.Status.ReplicationHealth = nil
		meta.RemoveStatusCondition(&plan.Status.Conditions, conditionTypeReplicationHealthy)
	}

	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		logger.Error(err, "Failed to patch DRPlan status")
		return ctrl.Result{}, err
	}

	// Record Prometheus metrics after a successful status patch.
	metrics.RecordPlanVMs(plan.Name, totalVMs)
	if wavesChanged {
		logger.Info("Discovery completed", "totalVMs", totalVMs, "waves", len(waves))
		r.event(plan, "Normal", "DiscoveryCompleted",
			fmt.Sprintf("Discovered %d VMs across %d waves", totalVMs, len(waves)))
	}

	// Emit events on health transitions (only when previous state existed).
	for _, vg := range degradedVGs {
		r.event(plan, "Warning", "ReplicationDegraded",
			fmt.Sprintf("Volume group %s/%s replication health changed to %s",
				vg.Namespace, vg.Name, vg.Health))
	}
	if len(recoveredVGs) > 0 && allHealthy(replicationHealth) {
		r.event(plan, "Normal", "ReplicationHealthy",
			"All volume groups returned to healthy replication")
	}

	requeue := requeueInterval
	if anyNonHealthy(replicationHealth) {
		requeue = degradedRequeueInterval
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// replicationHealthChanged compares two VolumeGroupHealth slices, ignoring
// LastChecked timestamps to avoid infinite requeue loops.
func replicationHealthChanged(old, new []soteriav1alpha1.VolumeGroupHealth) bool {
	if len(old) != len(new) {
		return true
	}
	for i := range old {
		if old[i].Name != new[i].Name ||
			old[i].Namespace != new[i].Namespace ||
			old[i].Health != new[i].Health ||
			old[i].Message != new[i].Message {
			return true
		}
		oldSync := old[i].LastSyncTime
		newSync := new[i].LastSyncTime
		if (oldSync == nil) != (newSync == nil) {
			return true
		}
		if oldSync != nil && !oldSync.Equal(newSync) {
			return true
		}
	}
	return false
}

// preflightReportChanged compares two preflight reports ignoring the
// GeneratedAt timestamp so that a timestamp-only difference does not trigger
// a status patch (which would re-queue the controller in an infinite loop).
func preflightReportChanged(old, new *soteriav1alpha1.PreflightReport) bool {
	if old == nil && new == nil {
		return false
	}
	if old == nil || new == nil {
		return true
	}
	oldCopy := old.DeepCopy()
	newCopy := new.DeepCopy()
	oldCopy.GeneratedAt = nil
	newCopy.GeneratedAt = nil
	return !reflect.DeepEqual(oldCopy, newCopy)
}

// vmRelevantChangePredicate filters VM events to only those that affect DRPlan
// composition: creates, deletes, and label changes. Status-only updates are
// ignored to avoid unnecessary reconciliation cycles.
func vmRelevantChangePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return true
		},
	}
}
