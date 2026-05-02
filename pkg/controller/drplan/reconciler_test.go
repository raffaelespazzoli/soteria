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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/soteria-project/soteria/internal/preflight"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// mockVMDiscoverer implements engine.VMDiscoverer for unit tests.
type mockVMDiscoverer struct {
	vms []engine.VMReference
	err error
}

func (m *mockVMDiscoverer) DiscoverVMs(_ context.Context, _ string) ([]engine.VMReference, error) {
	return m.vms, m.err
}

// mockNamespaceLookup implements engine.NamespaceLookup for unit tests.
type mockNamespaceLookup struct {
	levels map[string]soteriav1alpha1.ConsistencyLevel
}

func (m *mockNamespaceLookup) GetConsistencyLevel(
	_ context.Context, namespace string,
) (soteriav1alpha1.ConsistencyLevel, error) {
	if level, ok := m.levels[namespace]; ok {
		return level, nil
	}
	return soteriav1alpha1.ConsistencyLevelVM, nil
}

const (
	testPrimarySite   = "dc-west"
	testSecondarySite = "dc-east"
)

var planKey = types.NamespacedName{Name: "plan-1"}

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	_ = kubevirtv1.AddToScheme(s)
	return s
}

func newTestPlan() *soteriav1alpha1.DRPlan {
	return &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "plan-1",
			Generation: 1,
		},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 5,
			PrimarySite:            testPrimarySite,
			SecondarySite:          testSecondarySite,
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:      soteriav1alpha1.PhaseSteadyState,
			ActiveSite: testPrimarySite,
		},
	}
}

func newReconciler(
	objs []client.Object, discoverer engine.VMDiscoverer,
) (*DRPlanReconciler, client.Client) {
	emptyLevels := map[string]soteriav1alpha1.ConsistencyLevel{}
	return newReconcilerWithNSLookup(
		objs, discoverer, &mockNamespaceLookup{levels: emptyLevels},
	)
}

func newReconcilerWithNSLookup(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
	nsLookup engine.NamespaceLookup,
) (*DRPlanReconciler, client.Client) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.DRPlan{}).
		Build()

	return &DRPlanReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		VMDiscoverer:    discoverer,
		NamespaceLookup: nsLookup,
		Recorder:        events.NewFakeRecorder(10),
	}, fakeClient
}

func TestReconcile_VMsDiscovered_StatusPopulated(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "2"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
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

	readyCond := findReadyCondition(updated.Status.Conditions)
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
	plan := newTestPlan()
	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: nil})

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
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

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil {
		t.Fatal("Ready condition not found")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready.Status = %v, want False", readyCond.Status)
	}
	if readyCond.Reason != reasonNoVMs {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonNoVMs)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight report should be populated even with zero VMs")
	}
	if updated.Status.Preflight.TotalVMs != 0 {
		t.Errorf("Preflight.TotalVMs = %d, want 0", updated.Status.Preflight.TotalVMs)
	}
	if updated.Status.Preflight.GeneratedAt == nil {
		t.Error("Preflight.GeneratedAt should not be nil")
	}
}

func TestReconcile_VMAdded_StatusUpdated(t *testing.T) {
	plan := newTestPlan()
	initialVMs := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	mock := &mockVMDiscoverer{vms: initialVMs}
	r, c := newReconciler([]client.Object{plan}, mock)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("First Reconcile() error: %v", err)
	}

	mock.vms = append(mock.vms, engine.VMReference{
		Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"},
	})

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
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

func TestReconcile_WaveLabelValueChanged_VMMoved(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	mock := &mockVMDiscoverer{vms: vms}
	r, c := newReconciler([]client.Object{plan}, mock)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})

	mock.vms[1].Labels["soteria.io/wave"] = "2"
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
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
		NamespacedName: types.NamespacedName{Name: "nonexistent"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0 (no requeue)", result.RequeueAfter)
	}
}

func TestReconcile_DiscoveryError_ReadyFalseWithBackoff(t *testing.T) {
	plan := newTestPlan()
	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{err: fmt.Errorf("connection refused")})

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
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

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil {
		t.Fatal("Ready condition not found")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready.Status = %v, want False", readyCond.Status)
	}
	if readyCond.Reason != reasonError {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonError)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight report should be populated even on discovery error")
	}
	if updated.Status.Preflight.TotalVMs != 0 {
		t.Errorf("Preflight.TotalVMs = %d, want 0", updated.Status.Preflight.TotalVMs)
	}
	if updated.Status.Preflight.GeneratedAt == nil {
		t.Error("Preflight.GeneratedAt should not be nil")
	}
	hasDiscoveryWarning := false
	for _, w := range updated.Status.Preflight.Warnings {
		if len(w) > 0 && contains(w, "VM discovery failed") {
			hasDiscoveryWarning = true
			break
		}
	}
	if !hasDiscoveryWarning {
		t.Errorf("Expected warning about VM discovery failure, got: %v",
			updated.Status.Preflight.Warnings)
	}
}

func TestEnqueueForVM_MatchesOnePlan(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-1",
			Namespace: "default",
			Labels:    map[string]string{soteriav1alpha1.DRPlanLabel: "plan-1"},
		},
	}

	r.enqueueForVM(vm, q)
	if q.Len() != 1 {
		t.Fatalf("expected 1 request, got %d", q.Len())
	}
	item, _ := q.Get()
	if item.Name != "plan-1" || item.Namespace != "" {
		t.Errorf("request = %v, want plan-1 (cluster-scoped)", item.NamespacedName)
	}
}

func TestEnqueueForVM_NoLabel_NoRequests(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-1",
			Namespace: "default",
			Labels:    map[string]string{"app": "unrelated"},
		},
	}

	r.enqueueForVM(vm, q)
	if q.Len() != 0 {
		t.Errorf("expected 0 requests, got %d", q.Len())
	}
}

func TestEnqueueForVM_EmptyLabel_NoRequests(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-1",
			Namespace: "default",
			Labels:    map[string]string{soteriav1alpha1.DRPlanLabel: ""},
		},
	}

	r.enqueueForVM(vm, q)
	if q.Len() != 0 {
		t.Errorf("expected 0 requests for empty label, got %d", q.Len())
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

func TestReconcile_VMLevel_IndividualVolumeGroups(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}
	r, c := newReconcilerWithNSLookup([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Fatal("Expected Ready=True")
	}

	if len(updated.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(updated.Status.Waves))
	}
	if len(updated.Status.Waves[0].Groups) != 2 {
		t.Errorf("Wave[0] groups = %d, want 2 (individual VM groups)", len(updated.Status.Waves[0].Groups))
	}
	for _, g := range updated.Status.Waves[0].Groups {
		if g.ConsistencyLevel != soteriav1alpha1.ConsistencyLevelVM {
			t.Errorf("Group %q level = %q, want vm", g.Name, g.ConsistencyLevel)
		}
	}
}

func TestReconcile_NamespaceLevel_SingleVolumeGroup(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"default": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	r, c := newReconcilerWithNSLookup([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Fatal("Expected Ready=True")
	}

	if len(updated.Status.Waves[0].Groups) != 1 {
		t.Fatalf("Wave[0] groups = %d, want 1", len(updated.Status.Waves[0].Groups))
	}
	if updated.Status.Waves[0].Groups[0].ConsistencyLevel != soteriav1alpha1.ConsistencyLevelNamespace {
		t.Errorf("Group level = %q, want namespace", updated.Status.Waves[0].Groups[0].ConsistencyLevel)
	}
	if len(updated.Status.Waves[0].Groups[0].VMNames) != 3 {
		t.Errorf("Group VMNames count = %d, want 3", len(updated.Status.Waves[0].Groups[0].VMNames))
	}
}

func TestReconcile_WaveConflict_ReadyFalse(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "2"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"default": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	r, c := newReconcilerWithNSLookup([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil {
		t.Fatal("Ready condition not found")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready.Status = %v, want False", readyCond.Status)
	}
	if readyCond.Reason != reasonWaveConflict {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonWaveConflict)
	}

	for _, w := range updated.Status.Waves {
		if len(w.Groups) != 0 {
			t.Errorf("Wave %q should have no groups on conflict, got %d", w.WaveKey, len(w.Groups))
		}
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight report should be populated even on wave conflict")
	}
	if updated.Status.Preflight.TotalVMs != 2 {
		t.Errorf("Preflight.TotalVMs = %d, want 2", updated.Status.Preflight.TotalVMs)
	}
	hasConflictWarning := false
	for _, w := range updated.Status.Preflight.Warnings {
		if contains(w, "Wave conflict") {
			hasConflictWarning = true
			break
		}
	}
	if !hasConflictWarning {
		t.Errorf("Expected wave conflict warning, got: %v",
			updated.Status.Preflight.Warnings)
	}
}

func TestReconcile_NamespaceGroupExceedsThrottle_ReadyFalse(t *testing.T) {
	plan := newTestPlan()
	plan.Spec.MaxConcurrentFailovers = 2
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"default": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	r, c := newReconcilerWithNSLookup([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil {
		t.Fatal("Ready condition not found")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready.Status = %v, want False", readyCond.Status)
	}
	if readyCond.Reason != reasonGroupExceedsThrottle {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonGroupExceedsThrottle)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight report should be populated even on throttle error")
	}
	if updated.Status.Preflight.TotalVMs != 3 {
		t.Errorf("Preflight.TotalVMs = %d, want 3", updated.Status.Preflight.TotalVMs)
	}
	hasThrottleWarning := false
	for _, w := range updated.Status.Preflight.Warnings {
		if contains(w, "exceeds maxConcurrentFailovers") {
			hasThrottleWarning = true
			break
		}
	}
	if !hasThrottleWarning {
		t.Errorf("Expected throttle warning, got: %v",
			updated.Status.Preflight.Warnings)
	}
}

func TestReconcile_WaveConflictResolved_ReadyTrue(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "2"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"default": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	mock := &mockVMDiscoverer{vms: vms}
	r, c := newReconcilerWithNSLookup([]client.Object{plan}, mock, nsLookup)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})

	mock.vms[1].Labels["soteria.io/wave"] = "1"
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Errorf("Expected Ready=True after conflict resolved, got %v", readyCond)
	}
}

func TestReconcile_MixedConsistency_CorrectGrouping(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-ns-1", Namespace: "ns-level", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-ns-2", Namespace: "ns-level", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-ind-1", Namespace: "vm-level", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-ind-2", Namespace: "vm-level", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"ns-level": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	r, c := newReconcilerWithNSLookup([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Fatal("Expected Ready=True")
	}

	if len(updated.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(updated.Status.Waves))
	}
	wave := updated.Status.Waves[0]
	if len(wave.Groups) != 3 {
		t.Fatalf("Wave groups = %d, want 3 (1 ns-level + 2 vm-level)", len(wave.Groups))
	}

	nsCount := 0
	vmCount := 0
	for _, g := range wave.Groups {
		if g.ConsistencyLevel == soteriav1alpha1.ConsistencyLevelNamespace {
			nsCount++
		} else {
			vmCount++
		}
	}
	if nsCount != 1 || vmCount != 2 {
		t.Errorf("Group breakdown: ns=%d, vm=%d, want ns=1, vm=2", nsCount, vmCount)
	}
}

func TestNsConsistencyAnnotationChangePredicate_Create(t *testing.T) {
	p := nsConsistencyAnnotationChangePredicate()
	if p.Create(event.CreateEvent{}) {
		t.Error("Create should return false")
	}
}

func TestNsConsistencyAnnotationChangePredicate_Delete(t *testing.T) {
	p := nsConsistencyAnnotationChangePredicate()
	if p.Delete(event.DeleteEvent{}) {
		t.Error("Delete should return false")
	}
}

func TestNsConsistencyAnnotationChangePredicate_AnnotationAdded(t *testing.T) {
	p := nsConsistencyAnnotationChangePredicate()
	old := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	}
	updated := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Annotations: map[string]string{
				soteriav1alpha1.ConsistencyAnnotation: "namespace",
			},
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}) {
		t.Error("Update adding consistency annotation should return true")
	}
}

func TestNsConsistencyAnnotationChangePredicate_UnrelatedAnnotation(t *testing.T) {
	p := nsConsistencyAnnotationChangePredicate()
	old := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	}
	updated := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-ns",
			Annotations: map[string]string{"unrelated": "value"},
		},
	}
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}) {
		t.Error("Update with unrelated annotation should return false")
	}
}

func TestNsConsistencyAnnotationChangePredicate_NoChange(t *testing.T) {
	p := nsConsistencyAnnotationChangePredicate()
	annotations := map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	}
	old := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns", Annotations: annotations},
	}
	updated := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns", Annotations: annotations},
	}
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}) {
		t.Error("Update with same annotation value should return false")
	}
}

func TestNsConsistencyAnnotationChangePredicate_Generic(t *testing.T) {
	p := nsConsistencyAnnotationChangePredicate()
	if p.Generic(event.GenericEvent{}) {
		t.Error("Generic should return false")
	}
}

func TestMapNamespaceToDRPlans_MatchesOne(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "target-ns",
			Labels: map[string]string{"soteria.io/wave": "1"}},
	}
	r, _ := newReconciler(
		[]client.Object{plan}, &mockVMDiscoverer{vms: vms},
	)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "plan-1",
		},
	})

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "target-ns"},
	}
	requests := r.mapNamespaceToDRPlans(context.Background(), ns)
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	if requests[0].Name != "plan-1" {
		t.Errorf("request = %v, want plan-1", requests[0].NamespacedName)
	}
}

func TestMapNamespaceToDRPlans_MatchesNone(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "other-ns",
			Labels: map[string]string{"soteria.io/wave": "1"}},
	}
	r, _ := newReconciler(
		[]client.Object{plan}, &mockVMDiscoverer{vms: vms},
	)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "plan-1",
		},
	})

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated-ns"},
	}
	requests := r.mapNamespaceToDRPlans(context.Background(), ns)
	if len(requests) != 0 {
		t.Errorf("expected 0 requests, got %d", len(requests))
	}
}

// findReadyCondition returns the Ready condition, or nil.
func findReadyCondition(conditions []metav1.Condition) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionTypeReady {
			return &conditions[i]
		}
	}
	return nil
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// mockStorageBackendResolver implements preflight.StorageBackendResolver for unit tests.
type mockStorageBackendResolver struct {
	backends map[string]string
	warnings []string
	err      error
}

func (m *mockStorageBackendResolver) ResolveBackends(
	_ context.Context, vms []engine.VMReference,
) (map[string]string, []string, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if m.backends != nil {
		return m.backends, m.warnings, nil
	}
	result := make(map[string]string, len(vms))
	for _, vm := range vms {
		result[vm.Namespace+"/"+vm.Name] = "odf"
	}
	return result, m.warnings, nil
}

// Compile-time check.
var _ preflight.StorageBackendResolver = (*mockStorageBackendResolver)(nil)

func newReconcilerWithStorage(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
	nsLookup engine.NamespaceLookup,
	storage preflight.StorageBackendResolver,
) (*DRPlanReconciler, client.Client) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.DRPlan{}).
		Build()

	return &DRPlanReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		VMDiscoverer:    discoverer,
		NamespaceLookup: nsLookup,
		StorageResolver: storage,
		Recorder:        events.NewFakeRecorder(10),
	}, fakeClient
}

func TestReconcile_Preflight_PopulatedOnSuccess(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "2"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}
	storage := &mockStorageBackendResolver{
		backends: map[string]string{
			"default/vm-1": "odf",
			"default/vm-2": "odf",
			"default/vm-3": "dell-powerstore",
		},
	}

	r, c := newReconcilerWithStorage([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup, storage)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight report not populated")
	}
	if updated.Status.Preflight.TotalVMs != 3 {
		t.Errorf("Preflight.TotalVMs = %d, want 3", updated.Status.Preflight.TotalVMs)
	}
	if len(updated.Status.Preflight.Waves) != 2 {
		t.Errorf("Preflight.Waves = %d, want 2", len(updated.Status.Preflight.Waves))
	}
	if updated.Status.Preflight.GeneratedAt == nil {
		t.Error("Preflight.GeneratedAt should not be nil")
	}

	wave1 := updated.Status.Preflight.Waves[0]
	for _, vm := range wave1.VMs {
		if vm.StorageBackend != "odf" {
			t.Errorf("Wave1 VM %s storage = %q, want odf", vm.Name, vm.StorageBackend)
		}
	}
	wave2 := updated.Status.Preflight.Waves[1]
	if len(wave2.VMs) != 1 || wave2.VMs[0].StorageBackend != "dell-powerstore" {
		t.Errorf("Wave2 VM storage = %q, want dell-powerstore", wave2.VMs[0].StorageBackend)
	}
}

func TestReconcile_Preflight_StorageResolutionFailure_StillPopulated(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}
	storage := &mockStorageBackendResolver{
		err: fmt.Errorf("connection refused"),
	}

	r, c := newReconcilerWithStorage([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup, storage)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight report should be populated even with storage errors")
	}

	hasStorageWarning := false
	for _, w := range updated.Status.Preflight.Warnings {
		if len(w) > 0 {
			hasStorageWarning = true
			break
		}
	}
	if !hasStorageWarning {
		t.Error("Expected warning about storage resolution failure")
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Ready should be True even when storage resolution fails")
	}
}

func TestReconcile_Preflight_UnknownStorageBackends_WarningsAdded(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}
	storage := &mockStorageBackendResolver{
		backends: map[string]string{"default/vm-1": "unknown"},
		warnings: []string{"VM default/vm-1: could not determine storage backend from PVC storage class"},
	}

	r, c := newReconcilerWithStorage([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup, storage)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight report not populated")
	}
	if len(updated.Status.Preflight.Warnings) == 0 {
		t.Error("Expected warnings for unknown storage backend")
	}
}

func TestReconcile_Preflight_UpdatesEveryReconcileCycle(t *testing.T) {
	plan := newTestPlan()
	initialVMs := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	mock := &mockVMDiscoverer{vms: initialVMs}
	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}
	storage := &mockStorageBackendResolver{}

	r, c := newReconcilerWithStorage([]client.Object{plan}, mock, nsLookup, storage)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("First Reconcile() error: %v", err)
	}

	var first soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &first); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}
	if first.Status.Preflight == nil || first.Status.Preflight.TotalVMs != 1 {
		t.Fatal("First preflight should have 1 VM")
	}

	mock.vms = append(mock.vms, engine.VMReference{
		Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"},
	})

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Second Reconcile() error: %v", err)
	}

	var second soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &second); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}
	if second.Status.Preflight == nil || second.Status.Preflight.TotalVMs != 2 {
		t.Errorf("Second preflight TotalVMs = %d, want 2",
			second.Status.Preflight.TotalVMs)
	}
}

func TestReconcile_PassiveSite_WritesSiteDiscovery(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "ns-b", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})
	r.LocalSite = testSecondarySite // passive (plan ActiveSite == "dc-west")

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %v, want 30s", result.RequeueAfter)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.SecondarySiteDiscovery == nil {
		t.Fatal("SecondarySiteDiscovery should be populated")
	}
	if updated.Status.SecondarySiteDiscovery.DiscoveredVMCount != 2 {
		t.Errorf("DiscoveredVMCount = %d, want 2",
			updated.Status.SecondarySiteDiscovery.DiscoveredVMCount)
	}
	if updated.Status.SecondarySiteDiscovery.LastDiscoveryTime.IsZero() {
		t.Error("LastDiscoveryTime should not be zero")
	}

	// Passive site must NOT modify active-site-owned fields.
	if len(updated.Status.Waves) != 0 {
		t.Errorf("Waves should not be modified by passive site, got %d", len(updated.Status.Waves))
	}
	if updated.Status.DiscoveredVMCount != 0 {
		t.Errorf("DiscoveredVMCount (active) should be 0, got %d", updated.Status.DiscoveredVMCount)
	}
	if updated.Status.Preflight != nil {
		t.Error("Preflight should not be modified by passive site")
	}
}

func TestReconcile_ActiveSite_WritesSiteDiscovery(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "2"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})
	r.LocalSite = testPrimarySite // active

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.PrimarySiteDiscovery == nil {
		t.Fatal("PrimarySiteDiscovery should be populated")
	}
	if updated.Status.PrimarySiteDiscovery.DiscoveredVMCount != 3 {
		t.Errorf("PrimarySiteDiscovery.DiscoveredVMCount = %d, want 3",
			updated.Status.PrimarySiteDiscovery.DiscoveredVMCount)
	}
	if updated.Status.PrimarySiteDiscovery.LastDiscoveryTime.IsZero() {
		t.Error("LastDiscoveryTime should not be zero")
	}

	// Active-site normal behavior should still work.
	if len(updated.Status.Waves) != 2 {
		t.Errorf("Waves = %d, want 2", len(updated.Status.Waves))
	}
	if updated.Status.DiscoveredVMCount != 3 {
		t.Errorf("DiscoveredVMCount = %d, want 3", updated.Status.DiscoveredVMCount)
	}
	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True")
	}
}

func TestReconcile_PassiveSite_DoesNotModifyActiveStatus(t *testing.T) {
	plan := newTestPlan()
	// Pre-populate active-site-owned fields from a prior active reconcile.
	plan.Status.Waves = []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-1", Namespace: "default"}}},
	}
	plan.Status.DiscoveredVMCount = 5
	plan.Status.Conditions = []metav1.Condition{
		{Type: conditionTypeReady, Status: metav1.ConditionTrue, Reason: reasonDiscovered,
			LastTransitionTime: metav1.Now()},
	}

	vms := []engine.VMReference{
		{Name: "vm-peer-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})
	r.LocalSite = testSecondarySite // passive

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	// Active-owned fields must remain unchanged.
	if len(updated.Status.Waves) != 1 {
		t.Errorf("Waves should remain 1, got %d", len(updated.Status.Waves))
	}
	if updated.Status.DiscoveredVMCount != 5 {
		t.Errorf("DiscoveredVMCount should remain 5, got %d", updated.Status.DiscoveredVMCount)
	}
	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Ready condition should remain True")
	}

	// Passive site's own field is populated.
	if updated.Status.SecondarySiteDiscovery == nil {
		t.Fatal("SecondarySiteDiscovery should be populated")
	}
	if updated.Status.SecondarySiteDiscovery.DiscoveredVMCount != 1 {
		t.Errorf("SecondarySiteDiscovery.DiscoveredVMCount = %d, want 1",
			updated.Status.SecondarySiteDiscovery.DiscoveredVMCount)
	}
}

func TestReconcile_PassiveSite_DiscoveryError_NoStatusCorruption(t *testing.T) {
	plan := newTestPlan()
	plan.Status.Waves = []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-1", Namespace: "default"}}},
	}
	plan.Status.DiscoveredVMCount = 1

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{err: fmt.Errorf("timeout")})
	r.LocalSite = testSecondarySite // passive

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() should return nil error on passive discovery failure, got: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %v, want 30s", result.RequeueAfter)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	// No status patch should have been made.
	if updated.Status.SecondarySiteDiscovery != nil {
		t.Error("SecondarySiteDiscovery should remain nil on discovery error")
	}
	if len(updated.Status.Waves) != 1 {
		t.Errorf("Waves should remain unchanged, got %d", len(updated.Status.Waves))
	}
	if updated.Status.DiscoveredVMCount != 1 {
		t.Errorf("DiscoveredVMCount should remain 1, got %d", updated.Status.DiscoveredVMCount)
	}
}

func TestReconcile_NoLocalSite_NoSiteDiscovery(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})
	// LocalSite is "" (backward compat)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.PrimarySiteDiscovery != nil {
		t.Error("PrimarySiteDiscovery should be nil when LocalSite is empty")
	}
	if updated.Status.SecondarySiteDiscovery != nil {
		t.Error("SecondarySiteDiscovery should be nil when LocalSite is empty")
	}

	// Normal reconcile behavior should still work.
	if updated.Status.DiscoveredVMCount != 1 {
		t.Errorf("DiscoveredVMCount = %d, want 1", updated.Status.DiscoveredVMCount)
	}
}

func TestReconcile_ActiveSite_DiscoveryError_PreservesSiteDiscovery(t *testing.T) {
	plan := newTestPlan()
	existingDiscovery := &soteriav1alpha1.SiteDiscovery{
		VMs:               []soteriav1alpha1.DiscoveredVM{{Name: "vm-prior", Namespace: "default"}},
		DiscoveredVMCount: 1,
		LastDiscoveryTime: metav1.Now(),
	}
	plan.Status.PrimarySiteDiscovery = existingDiscovery

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{err: fmt.Errorf("net timeout")})
	r.LocalSite = testPrimarySite // active

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err == nil {
		t.Fatal("Reconcile() should return error on active-site discovery failure")
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.PrimarySiteDiscovery == nil {
		t.Fatal("PrimarySiteDiscovery should be preserved on discovery error")
	}
	if updated.Status.PrimarySiteDiscovery.DiscoveredVMCount != 1 {
		t.Errorf("PrimarySiteDiscovery.DiscoveredVMCount = %d, want 1 (preserved)",
			updated.Status.PrimarySiteDiscovery.DiscoveredVMCount)
	}
}

func TestReconcile_ActiveSite_PreservesPeerSiteDiscovery(t *testing.T) {
	plan := newTestPlan()
	// Pre-populate SecondarySiteDiscovery (simulating passive site already wrote).
	plan.Status.SecondarySiteDiscovery = &soteriav1alpha1.SiteDiscovery{
		VMs:               []soteriav1alpha1.DiscoveredVM{{Name: "peer-vm-1", Namespace: "default"}},
		DiscoveredVMCount: 1,
		LastDiscoveryTime: metav1.Now(),
	}

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})
	r.LocalSite = testPrimarySite // active

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	// Active site's SiteDiscovery is populated.
	if updated.Status.PrimarySiteDiscovery == nil {
		t.Fatal("PrimarySiteDiscovery should be populated")
	}
	if updated.Status.PrimarySiteDiscovery.DiscoveredVMCount != 2 {
		t.Errorf("PrimarySiteDiscovery.DiscoveredVMCount = %d, want 2",
			updated.Status.PrimarySiteDiscovery.DiscoveredVMCount)
	}

	// Peer site's SiteDiscovery is preserved.
	if updated.Status.SecondarySiteDiscovery == nil {
		t.Fatal("SecondarySiteDiscovery should be preserved")
	}
	if updated.Status.SecondarySiteDiscovery.DiscoveredVMCount != 1 {
		t.Errorf("SecondarySiteDiscovery.DiscoveredVMCount = %d, want 1",
			updated.Status.SecondarySiteDiscovery.DiscoveredVMCount)
	}
	if updated.Status.SecondarySiteDiscovery.VMs[0].Name != "peer-vm-1" {
		t.Errorf("SecondarySiteDiscovery VM name = %q, want peer-vm-1",
			updated.Status.SecondarySiteDiscovery.VMs[0].Name)
	}
}

// Ensure reconcile.Reconciler is implemented.
var _ reconcile.Reconciler = (*DRPlanReconciler)(nil)
