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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	fakedrv "github.com/soteria-project/soteria/pkg/drivers/fake"
	"github.com/soteria-project/soteria/pkg/drivers/noop"
	"github.com/soteria-project/soteria/pkg/engine"

	ctrl "sigs.k8s.io/controller-runtime"
)

// mockPVCResolver returns empty PVC names — suitable for unit tests that
// use fake/noop drivers where PVC resolution is not needed.
type mockPVCResolver struct{}

func (m mockPVCResolver) ResolvePVCNames(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

// mockSCLister returns a fixed provisioner for any storage class name.
type mockSCLister struct {
	provisioner string
}

func (m *mockSCLister) GetProvisioner(_ context.Context, _ string) (string, error) {
	return m.provisioner, nil
}

func newHealthTestReconciler(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
	registry *drivers.Registry,
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
		Registry:        registry,
		SCLister:        &mockSCLister{provisioner: noop.ProvisionerName},
		PVCResolver:     mockPVCResolver{},
	}, fakeClient
}

func TestPollReplicationHealth_HealthyVG(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnCreateVolumeGroup().ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1-id", Name: "vm-default-vm-1"},
	})
	now := time.Now()
	zero := time.Duration(0)
	fakeDriver.OnGetReplicationStatus("vg-1-id").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:         drivers.RoleSource,
			Health:       drivers.HealthHealthy,
			LastSyncTime: &now,
			EstimatedRPO: &zero,
		},
	})

	registry := drivers.NewRegistry()
	registry.SetFallbackDriver(func() drivers.StorageProvider { return fakeDriver })

	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, registry)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if len(updated.Status.ReplicationHealth) != 1 {
		t.Fatalf("ReplicationHealth entries = %d, want 1", len(updated.Status.ReplicationHealth))
	}

	h := updated.Status.ReplicationHealth[0]
	if h.Health != soteriav1alpha1.HealthStatusHealthy {
		t.Errorf("Health = %q, want Healthy", h.Health)
	}
	if h.EstimatedRPO != "0s" {
		t.Errorf("EstimatedRPO = %q, want 0s", h.EstimatedRPO)
	}
	if h.LastChecked.IsZero() {
		t.Error("LastChecked should be populated")
	}

	if result.RequeueAfter != requeueInterval {
		t.Errorf("RequeueAfter = %v, want %v (healthy)", result.RequeueAfter, requeueInterval)
	}

	replCond := findCondition(updated.Status.Conditions, conditionTypeReplicationHealthy)
	if replCond == nil {
		t.Fatal("ReplicationHealthy condition not found")
	}
	if replCond.Status != metav1.ConditionTrue {
		t.Errorf("ReplicationHealthy.Status = %v, want True", replCond.Status)
	}
	if replCond.Reason != reasonAllHealthy {
		t.Errorf("ReplicationHealthy.Reason = %q, want %q", replCond.Reason, reasonAllHealthy)
	}
}

func TestPollReplicationHealth_SyncingVG(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnCreateVolumeGroup().ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1-id", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("vg-1-id").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Health: drivers.HealthSyncing,
		},
	})

	registry := drivers.NewRegistry()
	registry.SetFallbackDriver(func() drivers.StorageProvider { return fakeDriver })

	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, registry)

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

	if len(updated.Status.ReplicationHealth) != 1 {
		t.Fatalf("ReplicationHealth entries = %d, want 1", len(updated.Status.ReplicationHealth))
	}
	h := updated.Status.ReplicationHealth[0]
	if h.Health != soteriav1alpha1.HealthStatusSyncing {
		t.Errorf("Health = %q, want Syncing", h.Health)
	}
}

func TestMapReplicationStatus_AllHealthStates(t *testing.T) {
	now := metav1.Now()
	vg := soteriav1alpha1.VolumeGroupInfo{
		Name:      "test-vg",
		Namespace: "test-ns",
	}

	tests := []struct {
		name       string
		driverH    drivers.ReplicationHealth
		wantStatus soteriav1alpha1.VolumeGroupHealthStatus
	}{
		{"Healthy", drivers.HealthHealthy, soteriav1alpha1.HealthStatusHealthy},
		{"Degraded", drivers.HealthDegraded, soteriav1alpha1.HealthStatusDegraded},
		{"Syncing", drivers.HealthSyncing, soteriav1alpha1.HealthStatusSyncing},
		{"Unknown", drivers.HealthUnknown, soteriav1alpha1.HealthStatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := drivers.ReplicationStatus{Health: tt.driverH}
			result := mapReplicationStatus(vg, status, now)
			if result.Health != tt.wantStatus {
				t.Errorf("Health = %q, want %q", result.Health, tt.wantStatus)
			}
		})
	}
}

func TestComputeRPO_DriverEstimatedRPO(t *testing.T) {
	d := 47 * time.Second
	status := drivers.ReplicationStatus{
		EstimatedRPO: &d,
	}
	got := computeRPO(status)
	if got != "47s" {
		t.Errorf("computeRPO = %q, want 47s", got)
	}
}

func TestComputeRPO_FromLastSyncTime(t *testing.T) {
	past := time.Now().Add(-2 * time.Minute)
	status := drivers.ReplicationStatus{
		LastSyncTime: &past,
	}
	got := computeRPO(status)
	if got == rpoUnknown {
		t.Errorf("computeRPO should not be unknown when LastSyncTime is set")
	}
	if got == "" {
		t.Errorf("computeRPO should produce a non-empty string")
	}
}

func TestComputeRPO_Unknown(t *testing.T) {
	status := drivers.ReplicationStatus{}
	got := computeRPO(status)
	if got != rpoUnknown {
		t.Errorf("computeRPO = %q, want %q", got, rpoUnknown)
	}
}

func TestComputeReplicationCondition_AllHealthy(t *testing.T) {
	health := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
		{Name: "vg-2", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
	}
	cond := computeReplicationCondition(health, 1)
	if cond == nil {
		t.Fatal("Expected non-nil condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("Status = %v, want True", cond.Status)
	}
	if cond.Reason != reasonAllHealthy {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonAllHealthy)
	}
}

func TestComputeReplicationCondition_MixedHealth(t *testing.T) {
	health := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
		{Name: "vg-2", Namespace: "ns", Health: soteriav1alpha1.HealthStatusDegraded},
		{Name: "vg-3", Namespace: "ns", Health: soteriav1alpha1.HealthStatusError},
	}
	cond := computeReplicationCondition(health, 1)
	if cond == nil {
		t.Fatal("Expected non-nil condition")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("Status = %v, want False", cond.Status)
	}
	if cond.Reason != reasonDegraded {
		t.Errorf("Reason = %q, want %q", cond.Reason, reasonDegraded)
	}
	if !contains(cond.Message, "ns/vg-2") {
		t.Errorf("Message should mention ns/vg-2, got: %q", cond.Message)
	}
	if !contains(cond.Message, "ns/vg-3") {
		t.Errorf("Message should mention ns/vg-3, got: %q", cond.Message)
	}
}

func TestComputeReplicationCondition_NoVGs(t *testing.T) {
	cond := computeReplicationCondition(nil, 1)
	if cond != nil {
		t.Errorf("Expected nil condition for empty health, got %v", cond)
	}
}

func TestDetectHealthTransitions_DegradedAndRecovered(t *testing.T) {
	old := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
		{Name: "vg-2", Namespace: "ns", Health: soteriav1alpha1.HealthStatusDegraded},
	}
	new := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusDegraded},
		{Name: "vg-2", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
	}

	degraded, recovered := detectHealthTransitions(old, new)
	if len(degraded) != 1 || degraded[0].Name != "vg-1" {
		t.Errorf("degraded = %v, want [vg-1]", degraded)
	}
	if len(recovered) != 1 || recovered[0].Name != "vg-2" {
		t.Errorf("recovered = %v, want [vg-2]", recovered)
	}
}

func TestDetectHealthTransitions_NoOldState(t *testing.T) {
	new := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusDegraded},
	}
	degraded, recovered := detectHealthTransitions(nil, new)
	if len(degraded) != 0 {
		t.Errorf("expected no degraded on first reconcile, got %d", len(degraded))
	}
	if len(recovered) != 0 {
		t.Errorf("expected no recovered on first reconcile, got %d", len(recovered))
	}
}

func TestDetectHealthTransitions_NoChange(t *testing.T) {
	health := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
	}
	degraded, recovered := detectHealthTransitions(health, health)
	if len(degraded) != 0 || len(recovered) != 0 {
		t.Errorf("expected no transitions on unchanged health")
	}
}

func TestReconcile_DegradedHealth_ShorterRequeue(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnCreateVolumeGroup().ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1-id", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("vg-1-id").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Health: drivers.HealthDegraded,
		},
	})

	registry := drivers.NewRegistry()
	registry.SetFallbackDriver(func() drivers.StorageProvider { return fakeDriver })

	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, registry)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	if result.RequeueAfter != degradedRequeueInterval {
		t.Errorf("RequeueAfter = %v, want %v (degraded)", result.RequeueAfter, degradedRequeueInterval)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	replCond := findCondition(updated.Status.Conditions, conditionTypeReplicationHealthy)
	if replCond == nil {
		t.Fatal("ReplicationHealthy condition not found")
	}
	if replCond.Status != metav1.ConditionFalse {
		t.Errorf("ReplicationHealthy.Status = %v, want False", replCond.Status)
	}
	if replCond.Reason != reasonDegraded {
		t.Errorf("ReplicationHealthy.Reason = %q, want %q", replCond.Reason, reasonDegraded)
	}
}

func TestReconcile_DriverError_ErrorHealth(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnCreateVolumeGroup().ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1-id", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("vg-1-id").ReturnResult(fakedrv.Response{
		Err: fmt.Errorf("storage array unreachable"),
	})

	registry := drivers.NewRegistry()
	registry.SetFallbackDriver(func() drivers.StorageProvider { return fakeDriver })

	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, registry)

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

	if len(updated.Status.ReplicationHealth) != 1 {
		t.Fatalf("ReplicationHealth entries = %d, want 1", len(updated.Status.ReplicationHealth))
	}

	h := updated.Status.ReplicationHealth[0]
	if h.Health != soteriav1alpha1.HealthStatusError {
		t.Errorf("Health = %q, want Error", h.Health)
	}
	if !contains(h.Message, "storage array unreachable") {
		t.Errorf("Message = %q, want to contain error text", h.Message)
	}
	if h.EstimatedRPO != rpoUnknown {
		t.Errorf("EstimatedRPO = %q, want %q", h.EstimatedRPO, rpoUnknown)
	}
}

func TestReconcile_RegistryNil_NoHealthFields(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms})

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

	if len(updated.Status.ReplicationHealth) != 0 {
		t.Errorf("ReplicationHealth should be empty when Registry is nil, got %d entries",
			len(updated.Status.ReplicationHealth))
	}

	replCond := findCondition(updated.Status.Conditions, conditionTypeReplicationHealthy)
	if replCond != nil {
		t.Error("ReplicationHealthy condition should not be set when Registry is nil")
	}
}

func TestReconcile_ActiveExecution_SkipsPolling(t *testing.T) {
	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.ProvisionerName, func() drivers.StorageProvider { return noop.New() })
	registry.SetFallbackDriver(func() drivers.StorageProvider { return noop.New() })

	plan := newTestPlan()
	plan.Status.ActiveExecution = "exec-1"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModePlannedMigration

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, registry)

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

	if len(updated.Status.ReplicationHealth) != 0 {
		t.Errorf("ReplicationHealth should be empty during active execution, got %d entries",
			len(updated.Status.ReplicationHealth))
	}
}

func TestReconcile_DriverNotFound_UnknownHealth(t *testing.T) {
	registry := drivers.NewRegistry()

	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, registry)

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

	if len(updated.Status.ReplicationHealth) != 1 {
		t.Fatalf("ReplicationHealth entries = %d, want 1", len(updated.Status.ReplicationHealth))
	}

	h := updated.Status.ReplicationHealth[0]
	if h.Health != soteriav1alpha1.HealthStatusUnknown {
		t.Errorf("Health = %q, want Unknown", h.Health)
	}
	if !contains(h.Message, "driver") {
		t.Errorf("Message = %q, want to mention driver", h.Message)
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Ready condition should remain True when driver not found")
	}
}

func TestReplicationHealthChanged_DifferentHealth(t *testing.T) {
	old := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
	}
	new := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusDegraded},
	}
	if !replicationHealthChanged(old, new) {
		t.Error("Expected change detected")
	}
}

func TestReplicationHealthChanged_SameHealth(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Minute))
	old := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy,
			EstimatedRPO: "0s", LastChecked: now},
	}
	new := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy,
			EstimatedRPO: "0s", LastChecked: later},
	}
	if replicationHealthChanged(old, new) {
		t.Error("Expected no change — only LastChecked differs")
	}
}

func TestReplicationHealthChanged_DifferentLengths(t *testing.T) {
	old := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
	}
	new := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
		{Name: "vg-2", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy},
	}
	if !replicationHealthChanged(old, new) {
		t.Error("Expected change detected for different lengths")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{47 * time.Second, "47s"},
		{2*time.Minute + 30*time.Second, "2m30s"},
		{time.Hour + 15*time.Minute, "1h15m0s"},
		{-5 * time.Second, "0s"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.in)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestJoinMax(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e", "f"}
	got := joinMax(items, 3)
	if got != "a, b, c ... and 3 more" {
		t.Errorf("joinMax = %q", got)
	}

	got = joinMax(items[:2], 5)
	if got != "a, b" {
		t.Errorf("joinMax = %q, want 'a, b'", got)
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
