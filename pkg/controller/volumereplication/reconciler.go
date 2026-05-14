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

package volumereplication

import (
	"context"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const NoopVolumeReplicationClass = "soteria-noop"

type VolumeReplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *VolumeReplicationReconciler) ReconcileVolumeReplication(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	var vr replicationv1alpha1.VolumeReplication
	if err := r.Get(ctx, req.NamespacedName, &vr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if vr.Spec.VolumeReplicationClass != NoopVolumeReplicationClass {
		return ctrl.Result{}, nil
	}

	if statusUpToDate(&vr.Status, vr.Spec.ReplicationState, vr.Generation) {
		return ctrl.Result{}, nil
	}

	logger := log.FromContext(ctx)
	logger.Info("Reconciling VolumeReplication",
		"name", vr.Name, "namespace", vr.Namespace,
		"replicationState", vr.Spec.ReplicationState)

	applyNoopStatus(
		&vr.Status,
		vr.Spec.ReplicationState, vr.Generation,
	)

	if err := r.Status().Update(ctx, &vr); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *VolumeReplicationReconciler) ReconcileVolumeGroupReplication(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	var vgr replicationv1alpha1.VolumeGroupReplication
	if err := r.Get(ctx, req.NamespacedName, &vgr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if vgr.Spec.VolumeReplicationClassName != NoopVolumeReplicationClass {
		return ctrl.Result{}, nil
	}

	if statusUpToDate(&vgr.Status.VolumeReplicationStatus, vgr.Spec.ReplicationState, vgr.Generation) {
		return ctrl.Result{}, nil
	}

	logger := log.FromContext(ctx)
	logger.Info("Reconciling VolumeGroupReplication",
		"name", vgr.Name, "namespace", vgr.Namespace,
		"replicationState", vgr.Spec.ReplicationState)

	applyNoopStatus(
		&vgr.Status.VolumeReplicationStatus,
		vgr.Spec.ReplicationState, vgr.Generation,
	)

	if err := r.Status().Update(ctx, &vgr); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// statusUpToDate returns true when the existing status already reflects the
// desired noop state for the given replication state and generation, making a
// status write unnecessary. Timestamps are intentionally excluded from the
// comparison — only semantic fields (state, generation, condition types and
// statuses) determine staleness.
func statusUpToDate(
	status *replicationv1alpha1.VolumeReplicationStatus,
	rs replicationv1alpha1.ReplicationState,
	generation int64,
) bool {
	state, replicating := stateForReplicationState(rs)
	if status.State != state || status.ObservedGeneration != generation {
		return false
	}
	if len(status.Conditions) != 5 {
		return false
	}
	replicatingStatus := metav1.ConditionFalse
	if replicating {
		replicatingStatus = metav1.ConditionTrue
	}
	expected := map[string]metav1.ConditionStatus{
		replicationv1alpha1.ConditionCompleted:   metav1.ConditionTrue,
		replicationv1alpha1.ConditionDegraded:    metav1.ConditionFalse,
		replicationv1alpha1.ConditionResyncing:   metav1.ConditionFalse,
		replicationv1alpha1.ConditionValidated:   metav1.ConditionTrue,
		replicationv1alpha1.ConditionReplicating: replicatingStatus,
	}
	for _, c := range status.Conditions {
		want, ok := expected[c.Type]
		if !ok || c.Status != want {
			return false
		}
	}
	return true
}

// applyNoopStatus stamps a successful noop status onto the shared
// VolumeReplicationStatus struct used by both VR and VGR.
func applyNoopStatus(
	status *replicationv1alpha1.VolumeReplicationStatus,
	rs replicationv1alpha1.ReplicationState,
	generation int64,
) {
	now := metav1.Now()
	state, replicating := stateForReplicationState(rs)

	status.State = state
	status.ObservedGeneration = generation
	status.LastCompletionTime = &now
	status.LastSyncTime = &now
	status.Conditions = buildConditions(replicating, now, generation)
}

func stateForReplicationState(
	rs replicationv1alpha1.ReplicationState,
) (replicationv1alpha1.State, bool) {
	switch rs {
	case replicationv1alpha1.Primary:
		return replicationv1alpha1.PrimaryState, true
	case replicationv1alpha1.Secondary, replicationv1alpha1.Resync:
		return replicationv1alpha1.SecondaryState, false
	default:
		return replicationv1alpha1.UnknownState, false
	}
}

func buildConditions(replicating bool, now metav1.Time, generation int64) []metav1.Condition {
	replicatingStatus := metav1.ConditionFalse
	if replicating {
		replicatingStatus = metav1.ConditionTrue
	}

	return []metav1.Condition{
		{
			Type:               replicationv1alpha1.ConditionCompleted,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             replicationv1alpha1.Success,
			ObservedGeneration: generation,
		},
		{
			Type:               replicationv1alpha1.ConditionDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             replicationv1alpha1.Healthy,
			ObservedGeneration: generation,
		},
		{
			Type:               replicationv1alpha1.ConditionResyncing,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             replicationv1alpha1.NotResyncing,
			ObservedGeneration: generation,
		},
		{
			Type:               replicationv1alpha1.ConditionValidated,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             replicationv1alpha1.PrerequisiteMet,
			ObservedGeneration: generation,
		},
		{
			Type:               replicationv1alpha1.ConditionReplicating,
			Status:             replicatingStatus,
			LastTransitionTime: now,
			Reason:             conditionReplicatingReason(replicating),
			ObservedGeneration: generation,
		},
	}
}

func conditionReplicatingReason(replicating bool) string {
	if replicating {
		return replicationv1alpha1.Replicating
	}
	return replicationv1alpha1.NotReplicating
}

func (r *VolumeReplicationReconciler) SetupVolumeReplicationController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&replicationv1alpha1.VolumeReplication{}).
		Complete(reconcile.Func(r.ReconcileVolumeReplication))
}

func (r *VolumeReplicationReconciler) SetupVolumeGroupReplicationController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&replicationv1alpha1.VolumeGroupReplication{}).
		Complete(reconcile.Func(r.ReconcileVolumeGroupReplication))
}
