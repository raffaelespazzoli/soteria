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
	"testing"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("Failed to add replication scheme: %v", err)
	}
	return s
}

func newReconciler(t *testing.T, objs ...client.Object) *VolumeReplicationReconciler {
	t.Helper()
	s := newScheme(t)
	cb := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(objs...)
	return &VolumeReplicationReconciler{
		Client: cb.Build(),
		Scheme: s,
	}
}

func makeVR(
	name, ns, class string,
	state replicationv1alpha1.ReplicationState,
) *replicationv1alpha1.VolumeReplication {
	return &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Generation: 1,
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: class,
			ReplicationState:       state,
			DataSource: corev1.TypedLocalObjectReference{
				APIGroup: ptrString("v1"),
				Kind:     "PersistentVolumeClaim",
				Name:     "test-pvc",
			},
		},
	}
}

func makeVGR(
	name, ns, vrClass string,
	state replicationv1alpha1.ReplicationState,
) *replicationv1alpha1.VolumeGroupReplication {
	return &replicationv1alpha1.VolumeGroupReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Generation: 1,
		},
		Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
			VolumeGroupReplicationClassName: "some-vgr-class",
			VolumeReplicationClassName:      vrClass,
			ReplicationState:                state,
			Source: replicationv1alpha1.VolumeGroupReplicationSource{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
		},
	}
}

func ptrString(s string) *string { return &s }

func findCondition(
	conditions []metav1.Condition,
	condType string,
) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// assertReconcileResult checks no error and no requeue.
func assertReconcileResult(t *testing.T, result ctrl.Result, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("unexpected requeue after %v", result.RequeueAfter)
	}
}

// assertReplicationStatus validates the common status fields written by the
// noop reconciler (state, observedGeneration, timestamps, conditions).
func assertReplicationStatus(
	t *testing.T,
	state replicationv1alpha1.State,
	observedGen int64,
	lastCompletion, lastSync *metav1.Time,
	conditions []metav1.Condition,
	wantState replicationv1alpha1.State,
	wantReplicating metav1.ConditionStatus,
) {
	t.Helper()
	if state != wantState {
		t.Errorf("state = %q, want %q", state, wantState)
	}
	if observedGen != 1 {
		t.Errorf("observedGeneration = %d, want 1", observedGen)
	}
	if lastCompletion == nil {
		t.Error("lastCompletionTime should be set")
	}
	if lastSync == nil {
		t.Error("lastSyncTime should be set")
	}
	assertConditions(t, conditions, wantReplicating)
}

func TestReconcileVolumeReplication(t *testing.T) {
	tests := []struct {
		name            string
		state           replicationv1alpha1.ReplicationState
		wantState       replicationv1alpha1.State
		wantReplicating metav1.ConditionStatus
	}{
		{
			name:            "primary state",
			state:           replicationv1alpha1.Primary,
			wantState:       replicationv1alpha1.PrimaryState,
			wantReplicating: metav1.ConditionTrue,
		},
		{
			name:            "secondary state",
			state:           replicationv1alpha1.Secondary,
			wantState:       replicationv1alpha1.SecondaryState,
			wantReplicating: metav1.ConditionFalse,
		},
		{
			name:            "resync state",
			state:           replicationv1alpha1.Resync,
			wantState:       replicationv1alpha1.SecondaryState,
			wantReplicating: metav1.ConditionFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := makeVR("test-vr", "default", NoopVolumeReplicationClass, tt.state)
			r := newReconciler(t, vr)

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Name: "test-vr", Namespace: "default",
			}}
			result, err := r.ReconcileVolumeReplication(context.Background(), req)
			assertReconcileResult(t, result, err)

			var updated replicationv1alpha1.VolumeReplication
			if err := r.Get(context.Background(), req.NamespacedName, &updated); err != nil {
				t.Fatalf("failed to get updated VR: %v", err)
			}

			assertReplicationStatus(t,
				updated.Status.State, updated.Status.ObservedGeneration,
				updated.Status.LastCompletionTime, updated.Status.LastSyncTime,
				updated.Status.Conditions,
				tt.wantState, tt.wantReplicating)
		})
	}
}

func TestReconcileVolumeReplication_SkipNonNoop(t *testing.T) {
	vr := makeVR("test-vr", "default", "ceph-rbd", replicationv1alpha1.Primary)
	r := newReconciler(t, vr)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Name: "test-vr", Namespace: "default",
	}}
	result, err := r.ReconcileVolumeReplication(context.Background(), req)
	assertReconcileResult(t, result, err)

	var updated replicationv1alpha1.VolumeReplication
	if err := r.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get VR: %v", err)
	}

	if updated.Status.State != "" {
		t.Errorf("status.state should be empty for non-noop class, got %q", updated.Status.State)
	}
	if len(updated.Status.Conditions) != 0 {
		t.Errorf("conditions should be empty for non-noop class, got %d", len(updated.Status.Conditions))
	}
}

func TestReconcileVolumeReplication_NotFound(t *testing.T) {
	s := newScheme(t)
	r := &VolumeReplicationReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).Build(),
		Scheme: s,
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Name: "deleted-vr", Namespace: "default",
	}}
	result, err := r.ReconcileVolumeReplication(context.Background(), req)
	assertReconcileResult(t, result, err)
}

func TestReconcileVolumeGroupReplication(t *testing.T) {
	tests := []struct {
		name            string
		state           replicationv1alpha1.ReplicationState
		wantState       replicationv1alpha1.State
		wantReplicating metav1.ConditionStatus
	}{
		{
			name:            "primary state",
			state:           replicationv1alpha1.Primary,
			wantState:       replicationv1alpha1.PrimaryState,
			wantReplicating: metav1.ConditionTrue,
		},
		{
			name:            "secondary state",
			state:           replicationv1alpha1.Secondary,
			wantState:       replicationv1alpha1.SecondaryState,
			wantReplicating: metav1.ConditionFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vgr := makeVGR("test-vgr", "default", NoopVolumeReplicationClass, tt.state)
			r := newReconciler(t, vgr)

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Name: "test-vgr", Namespace: "default",
			}}
			result, err := r.ReconcileVolumeGroupReplication(context.Background(), req)
			assertReconcileResult(t, result, err)

			var updated replicationv1alpha1.VolumeGroupReplication
			if err := r.Get(context.Background(), req.NamespacedName, &updated); err != nil {
				t.Fatalf("failed to get updated VGR: %v", err)
			}

			assertReplicationStatus(t,
				updated.Status.State, updated.Status.ObservedGeneration,
				updated.Status.LastCompletionTime, updated.Status.LastSyncTime,
				updated.Status.Conditions,
				tt.wantState, tt.wantReplicating)
		})
	}
}

func TestReconcileVolumeGroupReplication_SkipNonNoop(t *testing.T) {
	vgr := makeVGR("test-vgr", "default", "ceph-rbd", replicationv1alpha1.Primary)
	r := newReconciler(t, vgr)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Name: "test-vgr", Namespace: "default",
	}}
	result, err := r.ReconcileVolumeGroupReplication(context.Background(), req)
	assertReconcileResult(t, result, err)

	var updated replicationv1alpha1.VolumeGroupReplication
	if err := r.Get(context.Background(), req.NamespacedName, &updated); err != nil {
		t.Fatalf("failed to get VGR: %v", err)
	}

	if updated.Status.State != "" {
		t.Errorf("status.state should be empty for non-noop class, got %q", updated.Status.State)
	}
}

func TestReconcileVolumeReplication_Idempotent(t *testing.T) {
	vr := makeVR("test-vr", "default", NoopVolumeReplicationClass, replicationv1alpha1.Primary)
	r := newReconciler(t, vr)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Name: "test-vr", Namespace: "default",
	}}

	result, err := r.ReconcileVolumeReplication(context.Background(), req)
	assertReconcileResult(t, result, err)

	var after1st replicationv1alpha1.VolumeReplication
	if err := r.Get(context.Background(), req.NamespacedName, &after1st); err != nil {
		t.Fatalf("failed to get VR after 1st reconcile: %v", err)
	}

	result, err = r.ReconcileVolumeReplication(context.Background(), req)
	assertReconcileResult(t, result, err)

	var after2nd replicationv1alpha1.VolumeReplication
	if err := r.Get(context.Background(), req.NamespacedName, &after2nd); err != nil {
		t.Fatalf("failed to get VR after 2nd reconcile: %v", err)
	}

	if after2nd.ResourceVersion != after1st.ResourceVersion {
		t.Errorf("resourceVersion changed between reconciles (%s → %s): "+
			"status was written when already up-to-date",
			after1st.ResourceVersion, after2nd.ResourceVersion)
	}
	if after2nd.Status.State != after1st.Status.State {
		t.Errorf("state changed between reconciles: %q → %q",
			after1st.Status.State, after2nd.Status.State)
	}
	if len(after2nd.Status.Conditions) != len(after1st.Status.Conditions) {
		t.Errorf("condition count changed: %d → %d",
			len(after1st.Status.Conditions), len(after2nd.Status.Conditions))
	}
	for i, c := range after1st.Status.Conditions {
		if after2nd.Status.Conditions[i].LastTransitionTime != c.LastTransitionTime {
			t.Errorf("condition %q lastTransitionTime changed between reconciles",
				c.Type)
		}
	}
}

func assertConditions(
	t *testing.T,
	conditions []metav1.Condition,
	wantReplicating metav1.ConditionStatus,
) {
	t.Helper()

	if len(conditions) != 5 {
		t.Fatalf("expected 5 conditions, got %d", len(conditions))
	}

	checks := []struct {
		condType   string
		wantStatus metav1.ConditionStatus
	}{
		{replicationv1alpha1.ConditionCompleted, metav1.ConditionTrue},
		{replicationv1alpha1.ConditionDegraded, metav1.ConditionFalse},
		{replicationv1alpha1.ConditionResyncing, metav1.ConditionFalse},
		{replicationv1alpha1.ConditionValidated, metav1.ConditionTrue},
		{replicationv1alpha1.ConditionReplicating, wantReplicating},
	}

	for _, check := range checks {
		c := findCondition(conditions, check.condType)
		if c == nil {
			t.Errorf("condition %q not found", check.condType)
			continue
		}
		if c.Status != check.wantStatus {
			t.Errorf("condition %q: status = %q, want %q",
				check.condType, c.Status, check.wantStatus)
		}
	}
}
