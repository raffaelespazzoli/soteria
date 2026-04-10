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

package drplan

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// mockVMDiscoverer implements engine.VMDiscoverer for unit tests.
type mockVMDiscoverer struct {
	vms []engine.VMReference
	err error
}

func (m *mockVMDiscoverer) DiscoverVMs(_ context.Context, _ metav1.LabelSelector) ([]engine.VMReference, error) {
	return m.vms, m.err
}

var planKey = types.NamespacedName{Name: "plan-1", Namespace: "default"}

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	_ = kubevirtv1.AddToScheme(s)
	return s
}

func newTestPlan(name string) *soteriav1alpha1.DRPlan {
	return &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app.kubernetes.io/part-of": "erp-system"},
			},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 5,
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase: soteriav1alpha1.PhaseSteadyState,
		},
	}
}

func newReconciler(objs []client.Object, discoverer engine.VMDiscoverer) (*DRPlanReconciler, client.Client) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.DRPlan{}).
		Build()

	return &DRPlanReconciler{
		Client:       fakeClient,
		Scheme:       scheme,
		VMDiscoverer: discoverer,
		Recorder:     record.NewFakeRecorder(10),
	}, fakeClient
}

func TestReconcile_VMsDiscovered_StatusPopulated(t *testing.T) {
	plan := newTestPlan("plan-1")
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "2"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if result.RequeueAfter != requeueInterval {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, requeueInterval)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get updated plan: %v", err)
	}

	if updated.Status.DiscoveredVMCount != 3 {
		t.Errorf("DiscoveredVMCount = %d, want 3", updated.Status.DiscoveredVMCount)
	}
	if len(updated.Status.Waves) != 2 {
		t.Fatalf("len(Waves) = %d, want 2", len(updated.Status.Waves))
	}
	if updated.Status.Waves[0].WaveKey != "1" || updated.Status.Waves[1].WaveKey != "2" {
		t.Errorf("WaveKeys = [%q, %q], want [\"1\", \"2\"]",
			updated.Status.Waves[0].WaveKey, updated.Status.Waves[1].WaveKey)
	}
	if len(updated.Status.Waves[0].VMs) != 2 {
		t.Errorf("Wave 1 VM count = %d, want 2", len(updated.Status.Waves[0].VMs))
	}

	readyCond := findCondition(updated.Status.Conditions, conditionTypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not found")
	}
	if readyCond.Status != metav1.ConditionTrue {
		t.Errorf("Ready.Status = %v, want True", readyCond.Status)
	}
	if readyCond.Reason != reasonDiscovered {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonDiscovered)
	}
}

func TestReconcile_NoVMs_ReadyFalse(t *testing.T) {
	plan := newTestPlan("plan-1")
	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: nil})

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get updated plan: %v", err)
	}

	if updated.Status.DiscoveredVMCount != 0 {
		t.Errorf("DiscoveredVMCount = %d, want 0", updated.Status.DiscoveredVMCount)
	}
	if len(updated.Status.Waves) != 0 {
		t.Errorf("len(Waves) = %d, want 0", len(updated.Status.Waves))
	}

	readyCond := findCondition(updated.Status.Conditions, conditionTypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not found")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready.Status = %v, want False", readyCond.Status)
	}
	if readyCond.Reason != reasonNoVMs {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonNoVMs)
	}
}

func TestReconcile_VMAdded_StatusUpdated(t *testing.T) {
	plan := newTestPlan("plan-1")
	initialVMs := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	mock := &mockVMDiscoverer{vms: initialVMs}
	r, c := newReconciler([]client.Object{plan}, mock)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("First Reconcile() error: %v", err)
	}

	mock.vms = append(mock.vms, engine.VMReference{
		Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"},
	})

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Second Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get updated plan: %v", err)
	}

	if updated.Status.DiscoveredVMCount != 2 {
		t.Errorf("DiscoveredVMCount = %d, want 2", updated.Status.DiscoveredVMCount)
	}
	if len(updated.Status.Waves[0].VMs) != 2 {
		t.Errorf("Wave 1 VM count = %d, want 2", len(updated.Status.Waves[0].VMs))
	}
}

func TestReconcile_WaveLabelChanged_VMMoved(t *testing.T) {
	plan := newTestPlan("plan-1")
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	mock := &mockVMDiscoverer{vms: vms}
	r, c := newReconciler([]client.Object{plan}, mock)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1", Namespace: "default"},
	})

	mock.vms[1].Labels["soteria.io/wave"] = "2"
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if len(updated.Status.Waves) != 2 {
		t.Fatalf("len(Waves) = %d, want 2", len(updated.Status.Waves))
	}
	if len(updated.Status.Waves[0].VMs) != 1 || len(updated.Status.Waves[1].VMs) != 1 {
		t.Errorf("Wave VM counts = [%d, %d], want [1, 1]",
			len(updated.Status.Waves[0].VMs), len(updated.Status.Waves[1].VMs))
	}
}

func TestReconcile_PlanNotFound_NoError(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0 (no requeue)", result.RequeueAfter)
	}
}

func TestReconcile_DiscoveryError_ReadyFalseWithBackoff(t *testing.T) {
	plan := newTestPlan("plan-1")
	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{err: fmt.Errorf("connection refused")})

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("Reconcile() expected error, got nil")
	}
	if result.RequeueAfter == 0 {
		t.Error("Expected non-zero RequeueAfter for error backoff")
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findCondition(updated.Status.Conditions, conditionTypeReady)
	if readyCond == nil {
		t.Fatal("Ready condition not found")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready.Status = %v, want False", readyCond.Status)
	}
	if readyCond.Reason != reasonError {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonError)
	}
}

func TestMapVMToDRPlans_MatchesOne(t *testing.T) {
	plan := newTestPlan("plan-1")
	r, _ := newReconciler([]client.Object{plan}, &mockVMDiscoverer{})

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-1",
			Namespace: "default",
			Labels:    map[string]string{"app.kubernetes.io/part-of": "erp-system"},
		},
	}

	requests := r.mapVMToDRPlans(context.Background(), vm)
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	if requests[0].Name != "plan-1" || requests[0].Namespace != "default" {
		t.Errorf("request = %v, want plan-1/default", requests[0].NamespacedName)
	}
}

func TestMapVMToDRPlans_MatchesTwo(t *testing.T) {
	plan1 := newTestPlan("plan-1")
	plan2 := newTestPlan("plan-2")
	r, _ := newReconciler([]client.Object{plan1, plan2}, &mockVMDiscoverer{})

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-1",
			Namespace: "default",
			Labels:    map[string]string{"app.kubernetes.io/part-of": "erp-system"},
		},
	}

	requests := r.mapVMToDRPlans(context.Background(), vm)
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}

	names := map[string]bool{}
	for _, req := range requests {
		names[req.Name] = true
	}
	if !names["plan-1"] || !names["plan-2"] {
		t.Errorf("expected plan-1 and plan-2, got %v", names)
	}
}

func TestMapVMToDRPlans_MatchesNone(t *testing.T) {
	plan := newTestPlan("plan-1")
	r, _ := newReconciler([]client.Object{plan}, &mockVMDiscoverer{})

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-1",
			Namespace: "default",
			Labels:    map[string]string{"app": "unrelated"},
		},
	}

	requests := r.mapVMToDRPlans(context.Background(), vm)
	if len(requests) != 0 {
		t.Errorf("expected 0 requests, got %d", len(requests))
	}
}

func TestVMRelevantChangePredicate_Create(t *testing.T) {
	p := vmRelevantChangePredicate()
	if !p.Create(event.CreateEvent{}) {
		t.Error("Create should return true")
	}
}

func TestVMRelevantChangePredicate_Delete(t *testing.T) {
	p := vmRelevantChangePredicate()
	if !p.Delete(event.DeleteEvent{}) {
		t.Error("Delete should return true")
	}
}

func TestVMRelevantChangePredicate_Update_LabelChange(t *testing.T) {
	p := vmRelevantChangePredicate()
	old := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"soteria.io/wave": "1"}},
	}
	new := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"soteria.io/wave": "2"}},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: new}) {
		t.Error("Update with label change should return true")
	}
}

func TestVMRelevantChangePredicate_Update_NoLabelChange(t *testing.T) {
	p := vmRelevantChangePredicate()
	labels := map[string]string{"soteria.io/wave": "1"}
	old := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
	}
	new := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
	}
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: new}) {
		t.Error("Update with no label change should return false")
	}
}

func TestVMRelevantChangePredicate_Generic(t *testing.T) {
	p := vmRelevantChangePredicate()
	if !p.Generic(event.GenericEvent{}) {
		t.Error("Generic should return true")
	}
}

// findCondition returns the condition with the given type, or nil.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// Ensure reconcile.Reconciler is implemented.
var _ reconcile.Reconciler = (*DRPlanReconciler)(nil)
