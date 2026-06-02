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

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
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

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers/csiextension"
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
			VolumeReplicationDriver: soteriav1alpha1.VolumeReplicationDriverConfig{Type: "noop"},
			MaxConcurrentFailovers:  5,
			PrimarySite:             testPrimarySite,
			SecondarySite:           testSecondarySite,
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

func newReconcilerForPreflight(
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

func TestReconcile_Preflight_PopulatedOnSuccess(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "2"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}

	r, c := newReconcilerForPreflight([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup)

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

	declaredDriver := plan.Spec.VolumeReplicationDriver.Type
	for _, wave := range updated.Status.Preflight.Waves {
		for _, vm := range wave.VMs {
			if vm.StorageBackend != declaredDriver {
				t.Errorf("VM %s StorageBackend = %q, want %q", vm.Name, vm.StorageBackend, declaredDriver)
			}
		}
	}
}

func TestReconcile_Preflight_DeclaredDriverStamped(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}

	r, c := newReconcilerForPreflight([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nsLookup)

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

	declaredDriver := plan.Spec.VolumeReplicationDriver.Type
	for _, wave := range updated.Status.Preflight.Waves {
		for _, vm := range wave.VMs {
			if vm.StorageBackend != declaredDriver {
				t.Errorf("VM %s StorageBackend = %q, want %q", vm.Name, vm.StorageBackend, declaredDriver)
			}
		}
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Ready should be True")
	}
}

func TestReconcile_Preflight_UpdatesEveryReconcileCycle(t *testing.T) {
	plan := newTestPlan()
	initialVMs := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	mock := &mockVMDiscoverer{vms: initialVMs}
	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}}

	r, c := newReconcilerForPreflight([]client.Object{plan}, mock, nsLookup)

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
	peerTime := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	plan := newTestPlan()
	// Pre-populate SecondarySiteDiscovery (simulating passive site already
	// wrote). VMs must match the active site's discovery to avoid triggering
	// VMsMismatch; the peer's distinct LastDiscoveryTime proves the field
	// was preserved rather than overwritten.
	plan.Status.SecondarySiteDiscovery = &soteriav1alpha1.SiteDiscovery{
		VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "default"},
			{Name: "vm-2", Namespace: "default"},
		},
		DiscoveredVMCount: 2,
		LastDiscoveryTime: peerTime,
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

	// Peer site's SiteDiscovery is preserved with its original timestamp.
	if updated.Status.SecondarySiteDiscovery == nil {
		t.Fatal("SecondarySiteDiscovery should be preserved")
	}
	if updated.Status.SecondarySiteDiscovery.DiscoveredVMCount != 2 {
		t.Errorf("SecondarySiteDiscovery.DiscoveredVMCount = %d, want 2",
			updated.Status.SecondarySiteDiscovery.DiscoveredVMCount)
	}
	if !updated.Status.SecondarySiteDiscovery.LastDiscoveryTime.Equal(&peerTime) {
		t.Errorf("SecondarySiteDiscovery.LastDiscoveryTime = %v, want %v (peer's original time)",
			updated.Status.SecondarySiteDiscovery.LastDiscoveryTime, peerTime)
	}
}

// ---------- compareSiteDiscovery unit tests ----------

func newSiteDiscovery(vms ...soteriav1alpha1.DiscoveredVM) *soteriav1alpha1.SiteDiscovery {
	return &soteriav1alpha1.SiteDiscovery{
		VMs:               vms,
		DiscoveredVMCount: len(vms),
		LastDiscoveryTime: metav1.Now(),
	}
}

func TestCompareSiteDiscovery_BothAgree(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-a"},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-a"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
	)

	inSync, cond := compareSiteDiscovery(plan, primary, secondary)
	if !inSync {
		t.Error("Expected inSync=true")
	}
	if cond.Reason != reasonVMsAgreed {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonVMsAgreed)
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("Status = %v, want True", cond.Status)
	}
}

func TestCompareSiteDiscovery_PrimaryOnlyVMs(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-extra", Namespace: "default"},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
	)

	inSync, cond := compareSiteDiscovery(plan, primary, secondary)
	if inSync {
		t.Error("Expected inSync=false")
	}
	if cond.Reason != reasonVMsMismatch {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonVMsMismatch)
	}
	if !contains(cond.Message, "VMs on primary but not secondary") {
		t.Errorf("Message should list primary-only VMs, got: %s", cond.Message)
	}
	if !contains(cond.Message, "default/vm-extra") {
		t.Errorf("Message should contain 'default/vm-extra', got: %s", cond.Message)
	}
}

func TestCompareSiteDiscovery_SecondaryOnlyVMs(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-sec-only", Namespace: "ns-b"},
	)

	inSync, cond := compareSiteDiscovery(plan, primary, secondary)
	if inSync {
		t.Error("Expected inSync=false")
	}
	if !contains(cond.Message, "VMs on secondary but not primary") {
		t.Errorf("Message should list secondary-only VMs, got: %s", cond.Message)
	}
	if !contains(cond.Message, "ns-b/vm-sec-only") {
		t.Errorf("Message should contain 'ns-b/vm-sec-only', got: %s", cond.Message)
	}
}

func TestCompareSiteDiscovery_BothSideExtras(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-shared", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-p-only", Namespace: "default"},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-shared", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-s-only", Namespace: "default"},
	)

	inSync, cond := compareSiteDiscovery(plan, primary, secondary)
	if inSync {
		t.Error("Expected inSync=false")
	}
	if !contains(cond.Message, "VMs on primary but not secondary") {
		t.Errorf("Message should list primary-only, got: %s", cond.Message)
	}
	if !contains(cond.Message, "VMs on secondary but not primary") {
		t.Errorf("Message should list secondary-only, got: %s", cond.Message)
	}
}

func TestCompareSiteDiscovery_OneSideEmpty(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
	)
	secondary := newSiteDiscovery() // zero VMs but valid discovery

	inSync, cond := compareSiteDiscovery(plan, primary, secondary)
	if inSync {
		t.Error("Expected inSync=false")
	}
	if cond.Reason != reasonVMsMismatch {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonVMsMismatch)
	}
	if !contains(cond.Message, "discovered 0 VMs") {
		t.Errorf("Message should mention 0 VMs, got: %s", cond.Message)
	}
	if !contains(cond.Message, testSecondarySite) {
		t.Errorf("Message should mention site name %q, got: %s", testSecondarySite, cond.Message)
	}
}

func TestCompareSiteDiscovery_OneSideNil(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
	)

	inSync, cond := compareSiteDiscovery(plan, primary, nil)
	if inSync {
		t.Error("Expected inSync=false")
	}
	if cond.Reason != reasonWaitingForDiscovery {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonWaitingForDiscovery)
	}
	if !contains(cond.Message, testSecondarySite) {
		t.Errorf("Message should mention waiting site, got: %s", cond.Message)
	}
}

func TestCompareSiteDiscovery_BothNil(t *testing.T) {
	plan := newTestPlan()

	inSync, cond := compareSiteDiscovery(plan, nil, nil)
	if inSync {
		t.Error("Expected inSync=false")
	}
	if cond.Reason != reasonWaitingForDiscovery {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonWaitingForDiscovery)
	}
	if !contains(cond.Message, "both sites") {
		t.Errorf("Message should mention both sites waiting, got: %s", cond.Message)
	}
}

// ---------- compareDiskTopology unit tests ----------

func TestCompareDiskTopology_AllDisksMatch(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root-a", StorageClass: "ceph-rbd"},
			{Name: "datadisk", PVCName: "pvc-data-a", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-a", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root-b", StorageClass: "local-path"},
		}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "datadisk", PVCName: "different-pvc-data", StorageClass: "ceph-rbd"},
			{Name: "rootdisk", PVCName: "different-pvc-root", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-a", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "other-pvc", StorageClass: "local-path"},
		}},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if !consistent {
		t.Errorf("Expected consistent=true, got false: %s", cond.Message)
	}
	if cond.Reason != reasonDisksAgreed {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDisksAgreed)
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("Status = %v, want True", cond.Status)
	}
}

func TestCompareDiskTopology_DiskCountMismatch(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "web01", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
			{Name: "datadisk", PVCName: "pvc-data", StorageClass: "ceph-rbd"},
		}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "web01", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root-sec", StorageClass: "ceph-rbd"},
		}},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if consistent {
		t.Error("Expected consistent=false for count mismatch")
	}
	if cond.Reason != reasonDiskMismatch {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDiskMismatch)
	}
	if !contains(cond.Message, "count mismatch") {
		t.Errorf("Message should mention count mismatch, got: %s", cond.Message)
	}
	if !contains(cond.Message, "default/web01") {
		t.Errorf("Message should identify the VM, got: %s", cond.Message)
	}
}

func TestCompareDiskTopology_DiskNameMismatch(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "db01", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
			{Name: "logdisk", PVCName: "pvc-log", StorageClass: "ceph-rbd"},
		}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "db01", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root-s", StorageClass: "ceph-rbd"},
			{Name: "datadisk", PVCName: "pvc-data-s", StorageClass: "ceph-rbd"},
		}},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if consistent {
		t.Error("Expected consistent=false for name mismatch")
	}
	if cond.Reason != reasonDiskMismatch {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDiskMismatch)
	}
	if !contains(cond.Message, "name mismatch") {
		t.Errorf("Message should mention name mismatch, got: %s", cond.Message)
	}
}

func TestCompareDiskTopology_StorageClassMismatch(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "db01", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "datadisk", PVCName: "pvc-data", StorageClass: "ceph-rbd"},
		}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "db01", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "datadisk", PVCName: "pvc-data-s", StorageClass: "local-path"},
		}},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if consistent {
		t.Error("Expected consistent=false for storage class mismatch")
	}
	if cond.Reason != reasonDiskMismatch {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDiskMismatch)
	}
	if !contains(cond.Message, "storage class mismatch") {
		t.Errorf("Message should mention storage class mismatch, got: %s", cond.Message)
	}
	if !contains(cond.Message, "ceph-rbd") || !contains(cond.Message, "local-path") {
		t.Errorf("Message should mention both storage classes, got: %s", cond.Message)
	}
}

func TestCompareDiskTopology_MixedMismatches(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-a", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "p-root", StorageClass: "ceph-rbd"},
			{Name: "datadisk", PVCName: "p-data", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-b", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "p-root-b", StorageClass: "ceph-rbd"},
		}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-a", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "s-root", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-b", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "s-root-b", StorageClass: "local-path"},
		}},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if consistent {
		t.Error("Expected consistent=false for mixed mismatches")
	}
	if cond.Reason != reasonDiskMismatch {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDiskMismatch)
	}
	if !contains(cond.Message, "default/vm-a") || !contains(cond.Message, "default/vm-b") {
		t.Errorf("Message should mention both VMs, got: %s", cond.Message)
	}
}

func TestCompareDiskTopology_OneSideNoDisksYet(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
		}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: nil},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if consistent {
		t.Error("Expected consistent=false when waiting for disk discovery")
	}
	if cond.Reason != reasonWaitingForDiskDiscovery {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonWaitingForDiskDiscovery)
	}
	if !contains(cond.Message, "default/vm-1") {
		t.Errorf("Message should mention the VM waiting, got: %s", cond.Message)
	}
}

func TestCompareDiskTopology_BothSidesNoDisks(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: nil},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: nil},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if !consistent {
		t.Errorf("Expected consistent=true (empty == empty), got false: %s", cond.Message)
	}
	if cond.Reason != reasonDisksAgreed {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDisksAgreed)
	}
}

func TestCompareDiskTopology_VMsWithEmptyDisksOnBothSides(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "stateless-vm", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "stateless-vm", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{}},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if !consistent {
		t.Errorf("Expected consistent=true (no-PVC VMs), got false: %s", cond.Message)
	}
	if cond.Reason != reasonDisksAgreed {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDisksAgreed)
	}
}

func TestCompareDiskTopology_VMsOnlyOnOneSide(t *testing.T) {
	plan := newTestPlan()
	primary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "shared-vm", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "primary-only", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-extra", StorageClass: "ceph-rbd"},
		}},
	)
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "shared-vm", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root-sec", StorageClass: "ceph-rbd"},
		}},
	)

	consistent, cond := compareDiskTopology(plan, primary, secondary)
	if !consistent {
		t.Errorf("Expected consistent=true (VMs only on one site are handled by SitesInSync), got: %s", cond.Message)
	}
	if cond.Reason != reasonDisksAgreed {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDisksAgreed)
	}
}

func TestCompareDiskTopology_PrimaryNil(t *testing.T) {
	plan := newTestPlan()
	secondary := newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc", StorageClass: "ceph-rbd"},
		}},
	)

	consistent, cond := compareDiskTopology(plan, nil, secondary)
	if consistent {
		t.Error("Expected consistent=false when primary is nil")
	}
	if cond.Reason != reasonWaitingForDiskDiscovery {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonWaitingForDiskDiscovery)
	}
}

func TestCompareDiskTopology_BothNil(t *testing.T) {
	plan := newTestPlan()

	consistent, cond := compareDiskTopology(plan, nil, nil)
	if consistent {
		t.Error("Expected consistent=false when both nil")
	}
	if cond.Reason != reasonWaitingForDiskDiscovery {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonWaitingForDiskDiscovery)
	}
}

// ---------- Reconciler integration tests for agreement ----------

func newReconcilerWithSite(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
	localSite string,
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
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		LocalSite:       localSite,
	}, fakeClient
}

func TestReconcile_SitesInSync_WaveFormationProceeds(t *testing.T) {
	plan := newTestPlan()
	plan.Status.ActiveSite = testSecondarySite
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "default"},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "default"},
	)

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithSite([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, testSecondarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True when sites agree")
	}

	sisCond := findCondition(updated.Status.Conditions, conditionTypeSitesInSync)
	if sisCond == nil {
		t.Fatal("SitesInSync condition not found")
	}
	if sisCond.Status != metav1.ConditionTrue {
		t.Errorf("SitesInSync.Status = %v, want True", sisCond.Status)
	}
	if sisCond.Reason != reasonVMsAgreed {
		t.Errorf("SitesInSync.Reason = %q, want %q", sisCond.Reason, reasonVMsAgreed)
	}

	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be populated when sites agree")
	}
}

func TestReconcile_SitesOutOfSync_WavesCleared(t *testing.T) {
	plan := newTestPlan()
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-3", Namespace: "default"},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "default"},
	)
	// Pre-populate waves to verify they get cleared.
	plan.Status.Waves = []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-1", Namespace: "default"}}},
	}
	plan.Status.DiscoveredVMCount = 1

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-3", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithSite([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, testPrimarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
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
	if readyCond.Reason != reasonSitesOutOfSync {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonSitesOutOfSync)
	}

	sisCond := findCondition(updated.Status.Conditions, conditionTypeSitesInSync)
	if sisCond == nil {
		t.Fatal("SitesInSync condition not found")
	}
	if sisCond.Status != metav1.ConditionFalse {
		t.Errorf("SitesInSync.Status = %v, want False", sisCond.Status)
	}
	if sisCond.Reason != reasonVMsMismatch {
		t.Errorf("SitesInSync.Reason = %q, want %q", sisCond.Reason, reasonVMsMismatch)
	}

	if len(updated.Status.Waves) != 0 {
		t.Errorf("Waves should be cleared on mismatch, got %d", len(updated.Status.Waves))
	}
	if updated.Status.DiscoveredVMCount != 0 {
		t.Errorf("DiscoveredVMCount should be 0 on mismatch, got %d", updated.Status.DiscoveredVMCount)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight should be populated on mismatch even when previously nil")
	}
	if updated.Status.Preflight.SitesInSync {
		t.Error("Preflight.SitesInSync should be false on mismatch")
	}
	if updated.Status.Preflight.SiteDiscoveryDelta == "" {
		t.Error("Preflight.SiteDiscoveryDelta should be non-empty on mismatch")
	}
}

func TestReconcile_BothSiteDiscoveryNil_FirstReconcilePopulatesActive(t *testing.T) {
	plan := newTestPlan()
	// Both SiteDiscovery fields are nil — first reconcile for a site-aware
	// plan. The active site populates its own SiteDiscovery in-memory before
	// the agreement check, so compareSiteDiscovery sees current-cycle data
	// and reports WaitingForDiscovery for the peer (UAT-8.001 fix).

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithSite([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, testPrimarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	sisCond := findCondition(updated.Status.Conditions, conditionTypeSitesInSync)
	if sisCond == nil {
		t.Fatal("SitesInSync condition should be set on first reconcile")
	}
	if sisCond.Reason != reasonWaitingForDiscovery {
		t.Errorf("SitesInSync.Reason = %q, want %q", sisCond.Reason, reasonWaitingForDiscovery)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True — WaitingForDiscovery should not block")
	}

	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be populated — WaitingForDiscovery proceeds")
	}
}

func TestReconcile_NoSiteAware_BothSiteDiscoveryNil_SkipsAgreementCheck(t *testing.T) {
	plan := newTestPlan()
	// LocalSite is "" — non-site-aware deployment. Agreement check skipped.

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	sisCond := findCondition(updated.Status.Conditions, conditionTypeSitesInSync)
	if sisCond != nil {
		t.Error("SitesInSync condition should NOT be set without site-aware mode")
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True — agreement check should be skipped")
	}

	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be populated for non-site-aware plans")
	}
}

func TestReconcile_WaitingForDiscovery_ProceedsNormally(t *testing.T) {
	plan := newTestPlan()
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
	)
	// SecondarySiteDiscovery is nil — waiting for discovery.

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithSite([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, testPrimarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True when waiting for discovery (should proceed)")
	}

	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be populated — WaitingForDiscovery does not block")
	}

	sisCond := findCondition(updated.Status.Conditions, conditionTypeSitesInSync)
	if sisCond == nil {
		t.Fatal("SitesInSync condition should be set even when waiting")
	}
	if sisCond.Reason != reasonWaitingForDiscovery {
		t.Errorf("SitesInSync.Reason = %q, want %q", sisCond.Reason, reasonWaitingForDiscovery)
	}
}

func TestReconcile_NoLocalSite_SkipsAgreementCheck(t *testing.T) {
	plan := newTestPlan()
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default"},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-different", Namespace: "default"},
	)

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})
	// r.LocalSite is "" — backward compat mode

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	sisCond := findCondition(updated.Status.Conditions, conditionTypeSitesInSync)
	if sisCond != nil {
		t.Error("SitesInSync condition should NOT be set when LocalSite is empty")
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True — agreement check should be skipped")
	}
}

// ---------- Reconciler integration tests for disk agreement ----------

func TestReconcile_DisksConsistent_WaveFormationProceeds(t *testing.T) {
	plan := newTestPlan()
	plan.Status.ActiveSite = testSecondarySite
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-p-root", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-p-root-2", StorageClass: "ceph-rbd"},
		}},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-s-root", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-s-root-2", StorageClass: "ceph-rbd"},
		}},
	)

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithSite([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, testSecondarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True when disks are consistent")
	}

	dcCond := findCondition(updated.Status.Conditions, conditionTypeDisksConsistent)
	if dcCond == nil {
		t.Fatal("DisksConsistent condition not found")
	}
	if dcCond.Status != metav1.ConditionTrue {
		t.Errorf("DisksConsistent.Status = %v, want True", dcCond.Status)
	}
	if dcCond.Reason != reasonDisksAgreed {
		t.Errorf("DisksConsistent.Reason = %q, want %q", dcCond.Reason, reasonDisksAgreed)
	}

	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be populated when disks are consistent")
	}
}

func TestReconcile_DiskMismatch_WavesCleared(t *testing.T) {
	plan := newTestPlan()
	plan.Status.ActiveSite = testPrimarySite
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
			{Name: "datadisk", PVCName: "pvc-data", StorageClass: "ceph-rbd"},
		}},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root-sec", StorageClass: "ceph-rbd"},
		}},
	)
	plan.Status.Waves = []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-1", Namespace: "default"}}},
	}
	plan.Status.DiscoveredVMCount = 1

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithSite([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, testPrimarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
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
	if readyCond.Reason != reasonDisksOutOfSync {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonDisksOutOfSync)
	}

	dcCond := findCondition(updated.Status.Conditions, conditionTypeDisksConsistent)
	if dcCond == nil {
		t.Fatal("DisksConsistent condition not found")
	}
	if dcCond.Status != metav1.ConditionFalse {
		t.Errorf("DisksConsistent.Status = %v, want False", dcCond.Status)
	}
	if dcCond.Reason != reasonDiskMismatch {
		t.Errorf("DisksConsistent.Reason = %q, want %q", dcCond.Reason, reasonDiskMismatch)
	}

	if len(updated.Status.Waves) != 0 {
		t.Errorf("Waves should be cleared on disk mismatch, got %d", len(updated.Status.Waves))
	}
	if updated.Status.DiscoveredVMCount != 0 {
		t.Errorf("DiscoveredVMCount should be 0 on mismatch, got %d", updated.Status.DiscoveredVMCount)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight should be populated on disk mismatch")
	}
	if updated.Status.Preflight.DisksConsistent {
		t.Error("Preflight.DisksConsistent should be false on mismatch")
	}
	if updated.Status.Preflight.DiskDiscoveryDelta == "" {
		t.Error("Preflight.DiskDiscoveryDelta should be non-empty on mismatch")
	}
}

func TestReconcile_WaitingForDiskDiscovery_ProceedsNormally(t *testing.T) {
	plan := newTestPlan()
	plan.Status.ActiveSite = testPrimarySite
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
		}},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "default", Disks: nil},
	)

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithSite([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, testPrimarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True when waiting for disk discovery (should proceed)")
	}

	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be populated — WaitingForDiskDiscovery does not block")
	}

	dcCond := findCondition(updated.Status.Conditions, conditionTypeDisksConsistent)
	if dcCond == nil {
		t.Fatal("DisksConsistent condition should be set even when waiting")
	}
	if dcCond.Reason != reasonWaitingForDiskDiscovery {
		t.Errorf("DisksConsistent.Reason = %q, want %q", dcCond.Reason, reasonWaitingForDiskDiscovery)
	}
}

func newReconcilerWithSiteEnricherAndNSLookup(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
	nsLookup engine.NamespaceLookup,
	localSite string,
	enricher engine.DiskEnricher,
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
		LocalSite:       localSite,
		DiskEnricher:    enricher,
	}, fakeClient
}

// ---------- validateVGStorageClassHomogeneity unit tests ----------

func TestValidateVGStorageClassHomogeneity_SingleVMSingleSC(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "default-vm-1", Namespace: "default", VMNames: []string{"vm-1"}},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
				{Name: "datadisk", PVCName: "pvc-data", StorageClass: "ceph-rbd"},
			}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 0 {
		t.Errorf("Expected no mixed VGs, got %d: %+v", len(results), results)
	}
}

func TestValidateVGStorageClassHomogeneity_SingleVMTwoSCs(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "default-vm-1", Namespace: "default", VMNames: []string{"vm-1"}},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
				{Name: "datadisk", PVCName: "pvc-data", StorageClass: "local-path"},
			}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 1 {
		t.Fatalf("Expected 1 mixed VG, got %d", len(results))
	}
	if results[0].VGName != "default-vm-1" {
		t.Errorf("VGName = %q, want default-vm-1", results[0].VGName)
	}
	if len(results[0].Classes) != 2 || results[0].Classes[0] != "ceph-rbd" || results[0].Classes[1] != "local-path" {
		t.Errorf("Classes = %v, want [ceph-rbd, local-path]", results[0].Classes)
	}
}

func TestValidateVGStorageClassHomogeneity_NamespaceLevelAllSameSC(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "ns-erp-database", Namespace: "ns-erp", VMNames: []string{"db-1", "db-2", "db-3"},
			ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "db-1", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
			}},
			{Name: "db-2", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "ceph-rbd"},
			}},
			{Name: "db-3", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-3", StorageClass: "ceph-rbd"},
			}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 0 {
		t.Errorf("Expected no mixed VGs for same SC, got %d: %+v", len(results), results)
	}
}

func TestValidateVGStorageClassHomogeneity_NamespaceLevelMixedSC(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "ns-erp-database", Namespace: "ns-erp", VMNames: []string{"db-1", "db-2", "db-3"},
			ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "db-1", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
			}},
			{Name: "db-2", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "ceph-rbd"},
			}},
			{Name: "db-3", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-3", StorageClass: "local-path"},
			}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 1 {
		t.Fatalf("Expected 1 mixed VG, got %d", len(results))
	}
	if results[0].VGName != "ns-erp-database" {
		t.Errorf("VGName = %q, want ns-erp-database", results[0].VGName)
	}
	if len(results[0].Classes) != 2 {
		t.Errorf("Classes = %v, want 2 classes", results[0].Classes)
	}
}

func TestValidateVGStorageClassHomogeneity_MixedPlusStatelessVM(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "ns-app", Namespace: "ns-app", VMNames: []string{"app-1", "stateless-1"},
			ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "app-1", Namespace: "ns-app", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
			}},
			{Name: "stateless-1", Namespace: "ns-app", Disks: nil},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 0 {
		t.Errorf("Expected no mixed VGs (stateless VM contributes nothing), got %d: %+v", len(results), results)
	}
}

func TestValidateVGStorageClassHomogeneity_AllStatelessVMs(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "ns-ephemeral", Namespace: "ns-ephemeral", VMNames: []string{"s1", "s2"},
			ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "s1", Namespace: "ns-ephemeral", Disks: nil},
			{Name: "s2", Namespace: "ns-ephemeral", Disks: []soteriav1alpha1.DiscoveredDisk{}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 0 {
		t.Errorf("Expected no mixed VGs (all stateless), got %d: %+v", len(results), results)
	}
}

func TestValidateVGStorageClassHomogeneity_EmptyStorageClassExcluded(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "default-vm-1", Namespace: "default", VMNames: []string{"vm-1"}},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
				{Name: "datadisk", PVCName: "pvc-data", StorageClass: ""},
			}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 0 {
		t.Errorf("Expected no mixed VGs (empty SC excluded), got %d: %+v", len(results), results)
	}
}

func TestValidateVGStorageClassHomogeneity_MultipleVGsOneHeterogeneous(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "default-vm-1", Namespace: "default", VMNames: []string{"vm-1"}},
		{Name: "default-vm-2", Namespace: "default", VMNames: []string{"vm-2"}},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
			}},
			{Name: "vm-2", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "ceph-rbd"},
				{Name: "datadisk", PVCName: "pvc-3", StorageClass: "local-path"},
			}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 1 {
		t.Fatalf("Expected 1 mixed VG, got %d", len(results))
	}
	if results[0].VGName != "default-vm-2" {
		t.Errorf("VGName = %q, want default-vm-2", results[0].VGName)
	}
}

func TestValidateVGStorageClassHomogeneity_SingleVMSingleDiskSingleSC(t *testing.T) {
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		{Name: "default-vm-1", Namespace: "default", VMNames: []string{"vm-1"}},
	}
	waves := []soteriav1alpha1.WaveInfo{
		{WaveKey: "1", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "default", Disks: []soteriav1alpha1.DiscoveredDisk{
				{Name: "rootdisk", PVCName: "pvc-root", StorageClass: "ceph-rbd"},
			}},
		}},
	}

	results := validateVGStorageClassHomogeneity(vgs, waves)
	if len(results) != 0 {
		t.Errorf("Expected no mixed VGs, got %d: %+v", len(results), results)
	}
}

func TestBuildMixedSCMessage_SingleVG(t *testing.T) {
	mixed := []MixedVGResult{
		{VGName: "ns-erp-database", Classes: []string{"ceph-rbd", "local-path"}},
	}
	msg := buildMixedSCMessage(mixed)
	if !contains(msg, "ns-erp-database") {
		t.Errorf("Message should contain VG name, got: %s", msg)
	}
	if !contains(msg, "ceph-rbd") || !contains(msg, "local-path") {
		t.Errorf("Message should contain both storage classes, got: %s", msg)
	}
}

func TestBuildMixedSCMessage_MultipleVGs(t *testing.T) {
	mixed := []MixedVGResult{
		{VGName: "vg-1", Classes: []string{"a", "b"}},
		{VGName: "vg-2", Classes: []string{"c", "d"}},
	}
	msg := buildMixedSCMessage(mixed)
	if !contains(msg, "vg-1") || !contains(msg, "vg-2") {
		t.Errorf("Message should contain both VG names, got: %s", msg)
	}
}

// ---------- Reconciler integration tests for StorageClassMixed ----------

func TestReconcile_StorageClassMixed_BlocksReady(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "ns-erp", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "ns-erp", Labels: map[string]string{"soteria.io/wave": "1"}},
	}
	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"ns-erp": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	enricher := &mockDiskEnricher{disksByVM: map[string][]soteriav1alpha1.DiscoveredDisk{
		"vm-1": {
			{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
		},
		"vm-2": {
			{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "local-path"},
		},
	}}

	mock := &mockVMDiscoverer{vms: vms}
	r, c := newReconcilerWithSiteEnricherAndNSLookup(
		[]client.Object{plan}, mock, nsLookup, "", enricher)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
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
	if readyCond.Reason != reasonDisksOutOfSync {
		t.Errorf("Ready.Reason = %q, want %q", readyCond.Reason, reasonDisksOutOfSync)
	}

	dcCond := findCondition(updated.Status.Conditions, conditionTypeDisksConsistent)
	if dcCond == nil {
		t.Fatal("DisksConsistent condition not found")
	}
	if dcCond.Status != metav1.ConditionFalse {
		t.Errorf("DisksConsistent.Status = %v, want False", dcCond.Status)
	}
	if dcCond.Reason != reasonStorageClassMixed {
		t.Errorf("DisksConsistent.Reason = %q, want %q", dcCond.Reason, reasonStorageClassMixed)
	}
	if !contains(dcCond.Message, "ceph-rbd") || !contains(dcCond.Message, "local-path") {
		t.Errorf("DisksConsistent.Message should mention both SCs, got: %s", dcCond.Message)
	}

	if updated.Status.Preflight == nil {
		t.Fatal("Preflight should be populated on SC homogeneity failure")
	}
	if updated.Status.Preflight.DisksConsistent {
		t.Error("Preflight.DisksConsistent should be false")
	}
	if updated.Status.Preflight.DiskDiscoveryDelta == "" {
		t.Error("Preflight.DiskDiscoveryDelta should be non-empty")
	}

	// Waves should NOT be cleared (unlike disk mismatch from 9.3).
	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be preserved on SC homogeneity failure")
	}
}

func TestReconcile_StorageClassHomogeneous_Passes(t *testing.T) {
	plan := newTestPlan()
	plan.Status.ActiveSite = testPrimarySite
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "ceph-rbd"},
		}},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-1-sec", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-2-sec", StorageClass: "ceph-rbd"},
		}},
	)
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "ns-erp", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "ns-erp", Labels: map[string]string{"soteria.io/wave": "1"}},
	}
	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"ns-erp": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	enricher := &mockDiskEnricher{disksByVM: map[string][]soteriav1alpha1.DiscoveredDisk{
		"vm-1": {
			{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
		},
		"vm-2": {
			{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "ceph-rbd"},
		},
	}}

	mock2 := &mockVMDiscoverer{vms: vms}
	r, c := newReconcilerWithSiteEnricherAndNSLookup(
		[]client.Object{plan}, mock2, nsLookup, testPrimarySite, enricher)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True when all VGs are homogeneous")
	}

	dcCond := findCondition(updated.Status.Conditions, conditionTypeDisksConsistent)
	if dcCond == nil {
		t.Fatal("DisksConsistent condition not found")
	}
	if dcCond.Status != metav1.ConditionTrue {
		t.Errorf("DisksConsistent.Status = %v, want True", dcCond.Status)
	}
	if dcCond.Reason != reasonDisksAgreed {
		t.Errorf("DisksConsistent.Reason = %q, want %q", dcCond.Reason, reasonDisksAgreed)
	}
}

func TestReconcile_StorageClassMixed_PrefersOverDiskAgreed(t *testing.T) {
	plan := newTestPlan()
	// Sites agree on disk topology (9.3 would return DisksAgreed), but VG
	// storage class is mixed (9.5 overrides to StorageClassMixed).
	plan.Status.ActiveSite = testPrimarySite
	plan.Status.PrimarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "local-path"},
		}},
	)
	plan.Status.SecondarySiteDiscovery = newSiteDiscovery(
		soteriav1alpha1.DiscoveredVM{Name: "vm-1", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-1-sec", StorageClass: "ceph-rbd"},
		}},
		soteriav1alpha1.DiscoveredVM{Name: "vm-2", Namespace: "ns-erp", Disks: []soteriav1alpha1.DiscoveredDisk{
			{Name: "rootdisk", PVCName: "pvc-2-sec", StorageClass: "local-path"},
		}},
	)

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "ns-erp", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "ns-erp", Labels: map[string]string{"soteria.io/wave": "1"}},
	}
	nsLookup := &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
		"ns-erp": soteriav1alpha1.ConsistencyLevelNamespace,
	}}
	enricher := &mockDiskEnricher{disksByVM: map[string][]soteriav1alpha1.DiscoveredDisk{
		"vm-1": {
			{Name: "rootdisk", PVCName: "pvc-1", StorageClass: "ceph-rbd"},
		},
		"vm-2": {
			{Name: "rootdisk", PVCName: "pvc-2", StorageClass: "local-path"},
		},
	}}

	mock3 := &mockVMDiscoverer{vms: vms}
	r, c := newReconcilerWithSiteEnricherAndNSLookup(
		[]client.Object{plan}, mock3, nsLookup, testPrimarySite, enricher)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	// DisksConsistent should be False/StorageClassMixed, overriding DisksAgreed.
	dcCond := findCondition(updated.Status.Conditions, conditionTypeDisksConsistent)
	if dcCond == nil {
		t.Fatal("DisksConsistent condition not found")
	}
	if dcCond.Status != metav1.ConditionFalse {
		t.Errorf("DisksConsistent.Status = %v, want False", dcCond.Status)
	}
	if dcCond.Reason != reasonStorageClassMixed {
		t.Errorf("DisksConsistent.Reason = %q, want %q (should override DisksAgreed)", dcCond.Reason, reasonStorageClassMixed)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionFalse {
		t.Error("Expected Ready=False when VG SC is mixed")
	}
}

// ---------------------------------------------------------------------------
// Finalizer tests (Story 13.3)
// ---------------------------------------------------------------------------

func newCSIExtensionPlan() *soteriav1alpha1.DRPlan {
	p := newTestPlan()
	p.Spec.VolumeReplicationDriver.Type = "csi-extension"
	return p
}

func TestReconcile_CSIExtension_AddsDRPlanFinalizer(t *testing.T) {
	plan := newCSIExtensionPlan()

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{})

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: planKey,
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 after adding finalizer")
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Get plan: %v", err)
	}

	found := false
	for _, f := range updated.Finalizers {
		if f == csiextension.FinalizerVolumeReplication {
			found = true
		}
	}
	if !found {
		t.Errorf("DRPlan missing %s finalizer; finalizers = %v",
			csiextension.FinalizerVolumeReplication, updated.Finalizers)
	}
}

func TestReconcile_NoopDriver_SkipsDRPlanFinalizer(t *testing.T) {
	plan := newTestPlan()
	plan.Spec.VolumeReplicationDriver.Type = "noop"

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}
	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: planKey,
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Get plan: %v", err)
	}

	for _, f := range updated.Finalizers {
		if f == csiextension.FinalizerVolumeReplication {
			t.Errorf("noop driver plan should NOT have %s finalizer",
				csiextension.FinalizerVolumeReplication)
		}
	}
}

func newCSIExtensionScheme() *runtime.Scheme {
	s := newTestScheme()
	_ = replicationv1alpha1.AddToScheme(s)
	return s
}

func newReconcilerWithCSIScheme(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
) (*DRPlanReconciler, client.Client) {
	scheme := newCSIExtensionScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.DRPlan{}).
		Build()

	return &DRPlanReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		VMDiscoverer:    discoverer,
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		LocalSite:       testPrimarySite,
	}, fakeClient
}

func TestReconcile_DRPlanDeletion_RemovesSiteFinalizerFromVR(t *testing.T) {
	now := metav1.Now()
	plan := newCSIExtensionPlan()
	plan.Finalizers = []string{csiextension.FinalizerVolumeReplication}
	plan.DeletionTimestamp = &now
	plan.Status.ActiveSite = testPrimarySite

	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-ext-vr-1",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: plan.Name},
			Finalizers: []string{
				csiextension.FinalizerSitePrimary,
				csiextension.FinalizerSiteSecondary,
			},
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: "ceph-rbd",
			ReplicationState:       replicationv1alpha1.Primary,
		},
	}

	r, c := newReconcilerWithCSIScheme([]client.Object{plan, vr}, &mockVMDiscoverer{})
	r.LocalSite = testPrimarySite

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: planKey,
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	// VR still has the secondary finalizer, so DRPlan should requeue.
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when remote finalizer remains")
	}

	// VR should no longer have the primary finalizer.
	var updatedVR replicationv1alpha1.VolumeReplication
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(vr), &updatedVR); err != nil {
		t.Fatalf("Get VR: %v", err)
	}
	for _, f := range updatedVR.Finalizers {
		if f == csiextension.FinalizerSitePrimary {
			t.Error("VR should not have primary finalizer after cleanup")
		}
	}
	if len(updatedVR.Finalizers) != 1 || updatedVR.Finalizers[0] != csiextension.FinalizerSiteSecondary {
		t.Errorf("VR finalizers = %v, want [%s]",
			updatedVR.Finalizers, csiextension.FinalizerSiteSecondary)
	}

	// DRPlan finalizer should still be present (remote finalizer remains).
	var updatedPlan soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updatedPlan); err != nil {
		t.Fatalf("Get plan: %v", err)
	}
	found := false
	for _, f := range updatedPlan.Finalizers {
		if f == csiextension.FinalizerVolumeReplication {
			found = true
		}
	}
	if !found {
		t.Error("DRPlan finalizer should remain while remote VR finalizer exists")
	}
}

func TestReconcile_DRPlanDeletion_RemovesDRPlanFinalizerWhenClean(t *testing.T) {
	now := metav1.Now()
	plan := newCSIExtensionPlan()
	plan.Finalizers = []string{csiextension.FinalizerVolumeReplication}
	plan.DeletionTimestamp = &now
	plan.Status.ActiveSite = testPrimarySite

	// VR has only the local (primary) finalizer — cleanup will make it fully clean.
	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-ext-vr-1",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: plan.Name},
			Finalizers: []string{
				csiextension.FinalizerSitePrimary,
			},
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: "ceph-rbd",
			ReplicationState:       replicationv1alpha1.Primary,
		},
	}

	r, c := newReconcilerWithCSIScheme([]client.Object{plan, vr}, &mockVMDiscoverer{})
	r.LocalSite = testPrimarySite

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: planKey,
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	// DRPlan finalizer was removed and since DeletionTimestamp was set,
	// the fake client GC's the object. NotFound is the expected outcome.
	var updatedPlan soteriav1alpha1.DRPlan
	err = c.Get(context.Background(), planKey, &updatedPlan)
	if err == nil {
		for _, f := range updatedPlan.Finalizers {
			if f == csiextension.FinalizerVolumeReplication {
				t.Error("DRPlan finalizer should be removed after all VR/VGR cleanup is done")
			}
		}
	}
}

func TestReconcile_DRPlanDeletion_NoVRVGR_RemovesFinalizer(t *testing.T) {
	now := metav1.Now()
	plan := newCSIExtensionPlan()
	plan.Finalizers = []string{csiextension.FinalizerVolumeReplication}
	plan.DeletionTimestamp = &now

	r, c := newReconcilerWithCSIScheme([]client.Object{plan}, &mockVMDiscoverer{})

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: planKey,
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	// DRPlan was deleted (no finalizers remain + DeletionTimestamp set → GC'd).
	var updatedPlan soteriav1alpha1.DRPlan
	err = c.Get(context.Background(), planKey, &updatedPlan)
	if err == nil {
		for _, f := range updatedPlan.Finalizers {
			if f == csiextension.FinalizerVolumeReplication {
				t.Error("DRPlan finalizer should be removed when no VR/VGR exist")
			}
		}
	}
}

func TestReconcile_DRPlanDeletion_PostFailover_RemovesCorrectFinalizer(t *testing.T) {
	now := metav1.Now()
	plan := newCSIExtensionPlan()
	plan.Finalizers = []string{csiextension.FinalizerVolumeReplication}
	plan.DeletionTimestamp = &now
	plan.Status.ActiveSite = testSecondarySite

	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-ext-vr-failover",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: plan.Name},
			Finalizers: []string{
				csiextension.FinalizerSitePrimary,
				csiextension.FinalizerSiteSecondary,
			},
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: "ceph-rbd",
			ReplicationState:       replicationv1alpha1.Secondary,
		},
	}

	r, c := newReconcilerWithCSIScheme([]client.Object{plan, vr}, &mockVMDiscoverer{})
	r.LocalSite = testPrimarySite

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: planKey,
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when remote finalizer remains")
	}

	var updatedVR replicationv1alpha1.VolumeReplication
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(vr), &updatedVR); err != nil {
		t.Fatalf("Get VR: %v", err)
	}

	for _, f := range updatedVR.Finalizers {
		if f == csiextension.FinalizerSitePrimary {
			t.Error("VR should not have primary finalizer — physical primary site must remove it even after failover")
		}
	}
	if len(updatedVR.Finalizers) != 1 || updatedVR.Finalizers[0] != csiextension.FinalizerSiteSecondary {
		t.Errorf("VR finalizers = %v, want [%s]",
			updatedVR.Finalizers, csiextension.FinalizerSiteSecondary)
	}
}

func TestReconcile_DRPlanDeletion_VGR_RemovesSiteFinalizer(t *testing.T) {
	now := metav1.Now()
	plan := newCSIExtensionPlan()
	plan.Finalizers = []string{csiextension.FinalizerVolumeReplication}
	plan.DeletionTimestamp = &now
	plan.Status.ActiveSite = testPrimarySite

	vgr := &replicationv1alpha1.VolumeGroupReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-ext-vgr-1",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: plan.Name},
			Finalizers: []string{
				csiextension.FinalizerSitePrimary,
				csiextension.FinalizerSiteSecondary,
			},
		},
		Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
			VolumeGroupReplicationClassName: "ceph-rbd-group",
			ReplicationState:                replicationv1alpha1.Primary,
		},
	}

	r, c := newReconcilerWithCSIScheme([]client.Object{plan, vgr}, &mockVMDiscoverer{})
	r.LocalSite = testPrimarySite

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: planKey,
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when remote finalizer remains")
	}

	var updatedVGR replicationv1alpha1.VolumeGroupReplication
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(vgr), &updatedVGR); err != nil {
		t.Fatalf("Get VGR: %v", err)
	}
	for _, f := range updatedVGR.Finalizers {
		if f == csiextension.FinalizerSitePrimary {
			t.Error("VGR should not have primary finalizer after cleanup")
		}
	}
	if len(updatedVGR.Finalizers) != 1 || updatedVGR.Finalizers[0] != csiextension.FinalizerSiteSecondary {
		t.Errorf("VGR finalizers = %v, want [%s]",
			updatedVGR.Finalizers, csiextension.FinalizerSiteSecondary)
	}
}

// --- VR/VGR status change predicate tests ---

func TestVRStatusChangePredicate_Create_ReturnsFalse(t *testing.T) {
	p := vrStatusChangePredicate()
	if p.Create(event.CreateEvent{}) {
		t.Error("Create should return false for VR status predicate")
	}
}

func TestVRStatusChangePredicate_Delete_ReturnsFalse(t *testing.T) {
	p := vrStatusChangePredicate()
	if p.Delete(event.DeleteEvent{}) {
		t.Error("Delete should return false for VR status predicate")
	}
}

func TestVRStatusChangePredicate_Generic_ReturnsFalse(t *testing.T) {
	p := vrStatusChangePredicate()
	if p.Generic(event.GenericEvent{}) {
		t.Error("Generic should return false for VR status predicate")
	}
}

func TestVRStatusChangePredicate_Update_StateChange_ReturnsTrue(t *testing.T) {
	p := vrStatusChangePredicate()
	old := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.SecondaryState,
		},
	}
	newObj := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.PrimaryState,
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("Update with status.state change should return true")
	}
}

func TestVRStatusChangePredicate_Update_ConditionsChange_ReturnsTrue(t *testing.T) {
	p := vrStatusChangePredicate()
	old := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.PrimaryState,
			Conditions: []metav1.Condition{
				{Type: "Completed", Status: metav1.ConditionFalse},
			},
		},
	}
	newObj := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.PrimaryState,
			Conditions: []metav1.Condition{
				{Type: "Completed", Status: metav1.ConditionTrue},
			},
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("Update with status.conditions change should return true")
	}
}

func TestVRStatusChangePredicate_Update_SpecOnlyChange_ReturnsFalse(t *testing.T) {
	p := vrStatusChangePredicate()
	old := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{Name: "vr-1"},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			ReplicationState: replicationv1alpha1.Secondary,
		},
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.SecondaryState,
		},
	}
	newObj := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{Name: "vr-1"},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			ReplicationState: replicationv1alpha1.Primary,
		},
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.SecondaryState,
		},
	}
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("Update with spec-only change should return false")
	}
}

func TestVRStatusChangePredicate_Update_MetadataOnlyChange_ReturnsFalse(t *testing.T) {
	p := vrStatusChangePredicate()
	old := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "vr-1",
			Labels: map[string]string{"app": "old"},
		},
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.PrimaryState,
		},
	}
	newObj := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "vr-1",
			Labels: map[string]string{"app": "new"},
		},
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State: replicationv1alpha1.PrimaryState,
		},
	}
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("Update with metadata-only change should return false")
	}
}

func TestVRStatusChangePredicate_Update_VGR_StateChange_ReturnsTrue(t *testing.T) {
	p := vrStatusChangePredicate()
	old := &replicationv1alpha1.VolumeGroupReplication{
		Status: replicationv1alpha1.VolumeGroupReplicationStatus{
			VolumeReplicationStatus: replicationv1alpha1.VolumeReplicationStatus{
				State: replicationv1alpha1.SecondaryState,
			},
		},
	}
	newObj := &replicationv1alpha1.VolumeGroupReplication{
		Status: replicationv1alpha1.VolumeGroupReplicationStatus{
			VolumeReplicationStatus: replicationv1alpha1.VolumeReplicationStatus{
				State: replicationv1alpha1.PrimaryState,
			},
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("VGR update with status.state change should return true")
	}
}

func TestVRStatusChangePredicate_Update_VGR_ConditionsChange_ReturnsTrue(t *testing.T) {
	p := vrStatusChangePredicate()
	old := &replicationv1alpha1.VolumeGroupReplication{
		Status: replicationv1alpha1.VolumeGroupReplicationStatus{
			VolumeReplicationStatus: replicationv1alpha1.VolumeReplicationStatus{
				State: replicationv1alpha1.PrimaryState,
				Conditions: []metav1.Condition{
					{Type: "Completed", Status: metav1.ConditionFalse},
				},
			},
		},
	}
	newObj := &replicationv1alpha1.VolumeGroupReplication{
		Status: replicationv1alpha1.VolumeGroupReplicationStatus{
			VolumeReplicationStatus: replicationv1alpha1.VolumeReplicationStatus{
				State: replicationv1alpha1.PrimaryState,
				Conditions: []metav1.Condition{
					{Type: "Completed", Status: metav1.ConditionTrue},
				},
			},
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("VGR update with status.conditions change should return true")
	}
}

func TestVRStatusChangePredicate_Update_LastSyncTimeChange_ReturnsTrue(t *testing.T) {
	p := vrStatusChangePredicate()
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Minute))
	old := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State:        replicationv1alpha1.PrimaryState,
			LastSyncTime: &now,
		},
	}
	newObj := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State:        replicationv1alpha1.PrimaryState,
			LastSyncTime: &later,
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("Update with only lastSyncTime change should return true")
	}
}

func TestVRStatusChangePredicate_Update_LastSyncTimeNilToSet_ReturnsTrue(t *testing.T) {
	p := vrStatusChangePredicate()
	now := metav1.Now()
	old := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State:        replicationv1alpha1.PrimaryState,
			LastSyncTime: nil,
		},
	}
	newObj := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State:        replicationv1alpha1.PrimaryState,
			LastSyncTime: &now,
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("Update with lastSyncTime nil->set should return true")
	}
}

func TestVRStatusChangePredicate_Update_VGR_LastSyncTimeChange_ReturnsTrue(t *testing.T) {
	p := vrStatusChangePredicate()
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Minute))
	old := &replicationv1alpha1.VolumeGroupReplication{
		Status: replicationv1alpha1.VolumeGroupReplicationStatus{
			VolumeReplicationStatus: replicationv1alpha1.VolumeReplicationStatus{
				State:        replicationv1alpha1.PrimaryState,
				LastSyncTime: &now,
			},
		},
	}
	newObj := &replicationv1alpha1.VolumeGroupReplication{
		Status: replicationv1alpha1.VolumeGroupReplicationStatus{
			VolumeReplicationStatus: replicationv1alpha1.VolumeReplicationStatus{
				State:        replicationv1alpha1.PrimaryState,
				LastSyncTime: &later,
			},
		},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("VGR update with only lastSyncTime change should return true")
	}
}

func TestVRStatusChangePredicate_Update_IdenticalStatus_ReturnsFalse(t *testing.T) {
	p := vrStatusChangePredicate()
	now := metav1.Now()
	old := &replicationv1alpha1.VolumeReplication{
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State:        replicationv1alpha1.PrimaryState,
			LastSyncTime: &now,
			Conditions: []metav1.Condition{
				{Type: "Completed", Status: metav1.ConditionTrue},
			},
		},
	}
	newObj := old.DeepCopy()
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: newObj}) {
		t.Error("Update with identical status should return false")
	}
}

// --- VR/VGR enqueueForVR tests ---

func TestEnqueueForVR_WithLabel_EnqueuesCorrectPlan(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vr-1",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: "my-plan"},
		},
	}

	r.enqueueForVR(vr, q)
	if q.Len() != 1 {
		t.Fatalf("expected 1 request, got %d", q.Len())
	}
	item, _ := q.Get()
	if item.Name != "my-plan" || item.Namespace != "" {
		t.Errorf("request = %v, want my-plan (cluster-scoped)", item.NamespacedName)
	}
}

func TestEnqueueForVR_WithoutLabel_EnqueuesNothing(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vr-1",
			Namespace: "default",
			Labels:    map[string]string{"app": "unrelated"},
		},
	}

	r.enqueueForVR(vr, q)
	if q.Len() != 0 {
		t.Errorf("expected 0 requests, got %d", q.Len())
	}
}

func TestEnqueueForVR_EmptyLabel_EnqueuesNothing(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vr-1",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: ""},
		},
	}

	r.enqueueForVR(vr, q)
	if q.Len() != 0 {
		t.Errorf("expected 0 requests for empty label, got %d", q.Len())
	}
}

func TestEnqueueForVR_NilObject_EnqueuesNothing(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	r.enqueueForVR(nil, q)
	if q.Len() != 0 {
		t.Errorf("expected 0 requests for nil object, got %d", q.Len())
	}
}

func TestVREventHandler_Update_EnqueuesBothOldAndNew(t *testing.T) {
	r, _ := newReconciler(nil, &mockVMDiscoverer{})

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	h := r.vrEventHandler()

	oldVR := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vr-1",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: "old-plan"},
		},
	}
	newVR := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vr-1",
			Namespace: "default",
			Labels:    map[string]string{csiextension.LabelDRPlan: "new-plan"},
		},
	}

	h.UpdateFunc(context.Background(), event.TypedUpdateEvent[client.Object]{
		ObjectOld: oldVR,
		ObjectNew: newVR,
	}, q)

	if q.Len() != 2 {
		t.Fatalf("expected 2 requests (old + new plan), got %d", q.Len())
	}

	got := map[string]bool{}
	for range 2 {
		item, _ := q.Get()
		got[item.Name] = true
		q.Done(item)
	}
	if !got["old-plan"] || !got["new-plan"] {
		t.Errorf("expected both old-plan and new-plan enqueued, got %v", got)
	}
}

// Ensure reconcile.Reconciler is implemented.
var _ reconcile.Reconciler = (*DRPlanReconciler)(nil)
