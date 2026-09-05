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
		PVCResolver:     mockPVCResolver{},
	}, fakeClient
}

func TestPollReplicationHealth_HealthyVG(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnGetVolumeGroup("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-default/vm-default-vm-1", Name: "vm-default-vm-1"},
	})
	now := time.Now()
	fakeDriver.OnGetReplicationStatus("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:         drivers.RoleSource,
			Health:       drivers.HealthHealthy,
			LastSyncTime: &now,
		},
	})

	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

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
	fakeDriver.OnGetVolumeGroup("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-default/vm-default-vm-1", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Health: drivers.HealthSyncing,
		},
	})

	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

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

func TestPollReplicationHealth_VolumeGroupNotFound(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnGetVolumeGroup("noop-default/vm-default-vm-1").Return(drivers.ErrVolumeGroupNotFound)

	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

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
		t.Errorf("Health = %q, want Unknown when VG not found", h.Health)
	}
	if !contains(h.Message, "VR/VGR not yet created") {
		t.Errorf("Message = %q, want to mention VR/VGR not yet created", h.Message)
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
		{"NotReplicating", drivers.HealthNotReplicating, soteriav1alpha1.HealthStatusNotReplicating},
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
	fakeDriver.OnGetVolumeGroup("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-default/vm-default-vm-1", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Health: drivers.HealthDegraded,
		},
	})

	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

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
	fakeDriver.OnGetVolumeGroup("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-default/vm-default-vm-1", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		Err: fmt.Errorf("storage array unreachable"),
	})

	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

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

func TestReconcile_ActiveDRExecution_SkipsPolling(t *testing.T) {
	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return noop.New() })

	plan := newTestPlan()

	activeExec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "exec-1",
			Labels: map[string]string{soteriav1alpha1.PlanNameLabel: "plan-1"},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan, activeExec}, &mockVMDiscoverer{vms: vms}, registry)

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

func TestReconcile_NoDRExecution_PollsHealth(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnGetVolumeGroup("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-default/vm-default-vm-1", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthHealthy,
		},
	})

	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

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
		t.Fatalf("ReplicationHealth entries = %d, want 1 (polling should proceed with no DRExecutions)",
			len(updated.Status.ReplicationHealth))
	}
}

func TestReconcile_TerminalDRExecution_PollsHealth(t *testing.T) {
	fakeDriver := fakedrv.New()
	fakeDriver.OnGetVolumeGroup("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-default/vm-default-vm-1", Name: "vm-default-vm-1"},
	})
	fakeDriver.OnGetReplicationStatus("noop-default/vm-default-vm-1").ReturnResult(fakedrv.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthHealthy,
		},
	})

	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

	plan := newTestPlan()

	terminalExec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "exec-done",
			Labels: map[string]string{soteriav1alpha1.PlanNameLabel: "plan-1"},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: soteriav1alpha1.ExecutionResultSucceeded,
		},
	}

	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newHealthTestReconciler([]client.Object{plan, terminalExec}, &mockVMDiscoverer{vms: vms}, registry)

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
		t.Fatalf("ReplicationHealth entries = %d, want 1 (polling should proceed with terminal DRExecution)",
			len(updated.Status.ReplicationHealth))
	}
}

func TestResolveDriverForVG_UsesDeclaredDriverName(t *testing.T) {
	fakeDriver := fakedrv.New()
	registry := drivers.NewRegistry()
	registry.RegisterDriver(noop.PlanDriverName, func() drivers.StorageProvider { return fakeDriver })

	r := &DRPlanReconciler{Registry: registry}

	drv, err := r.resolveDriverForVG(context.Background(), noop.PlanDriverName)
	if err != nil {
		t.Fatalf("resolveDriverForVG() error: %v", err)
	}
	if drv != fakeDriver {
		t.Error("Expected the driver registered under the plan-level name")
	}
}

// Uses an empty registry (no fallback) to verify the error-handling path.
// Production wiring via drivers/all sets a noop fallback, so this path only
// fires if a future driver is registered without a fallback.
func TestResolveDriverForVG_UnknownDriver_ReturnsError(t *testing.T) {
	registry := drivers.NewRegistry()

	r := &DRPlanReconciler{Registry: registry}

	_, err := r.resolveDriverForVG(context.Background(), "nonexistent-driver")
	if err == nil {
		t.Fatal("Expected error for unregistered driver name")
	}
}

// Uses an empty registry (no fallback) to exercise the graceful-degradation
// path where driver resolution fails. In production the noop fallback would
// mask this; the test validates that health reports Unknown rather than
// crashing when the registry cannot resolve the plan's driver.
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
			LastChecked: now},
	}
	new := []soteriav1alpha1.VolumeGroupHealth{
		{Name: "vg-1", Namespace: "ns", Health: soteriav1alpha1.HealthStatusHealthy,
			LastChecked: later},
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

// ---------------------------------------------------------------------------
// reconcileVolumeReplication tests (Story 13.2)
// ---------------------------------------------------------------------------

func TestReconcileVolumeReplication_PrimarySite_CallsCreateVolumeGroupWithPrimary(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-vr", Generation: 1},
		Spec: soteriav1alpha1.DRPlanSpec{
			VolumeReplicationDriver: soteriav1alpha1.VolumeReplicationDriverConfig{
				Type:                   "noop",
				VolumeReplicationClass: "ceph-rbd",
			},
			MaxConcurrentFailovers: 5,
			PrimarySite:            testPrimarySite,
			SecondarySite:          testSecondarySite,
		},
		Status: soteriav1alpha1.DRPlanStatus{
			ActiveSite: testPrimarySite,
		},
	}

	waves := []soteriav1alpha1.WaveInfo{
		{
			WaveKey: "1",
			Groups: []soteriav1alpha1.VolumeGroupInfo{
				{Name: "vm-default-app1", Namespace: "default", VMNames: []string{"app1"}},
			},
		},
	}

	fakeDriver := fakedrv.New()
	registry := drivers.NewRegistry()
	registry.RegisterDriver("noop", func() drivers.StorageProvider { return fakeDriver })

	pvcResolver := &mockPVCResolverWithNames{names: map[string][]string{
		"default/app1": {"data-pvc", "logs-pvc"},
	}}

	r := &DRPlanReconciler{
		Client:          fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(plan).Build(),
		Scheme:          newTestScheme(),
		VMDiscoverer:    &mockVMDiscoverer{},
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		Registry:        registry,
		PVCResolver:     pvcResolver,
		LocalSite:       testPrimarySite,
	}

	if err := r.reconcileVolumeReplication(context.Background(), plan, waves); err != nil {
		t.Logf("reconcileVolumeReplication returned error: %v", err)
	}

	calls := fakeDriver.CallsTo("CreateVolumeGroup")
	if len(calls) != 1 {
		t.Fatalf("expected 1 CreateVolumeGroup call, got %d", len(calls))
	}

	spec := calls[0].Args[0].(drivers.VolumeGroupSpec)
	if spec.Labels[drivers.SiteRoleLabel] != drivers.SiteRolePrimary {
		t.Errorf("site role = %q, want %q", spec.Labels[drivers.SiteRoleLabel], drivers.SiteRolePrimary)
	}
	if spec.Labels[drivers.LabelDRPlan] != "plan-vr" {
		t.Errorf("drplan label = %q, want plan-vr", spec.Labels[drivers.LabelDRPlan])
	}
	if spec.Labels[drivers.VolumeReplicationClassLabel] != "ceph-rbd" {
		t.Errorf("vrClass = %q, want ceph-rbd", spec.Labels[drivers.VolumeReplicationClassLabel])
	}
}

func TestReconcileVolumeReplication_SecondarySite_CallsCreateVolumeGroupWithSecondary(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-vr", Generation: 1},
		Spec: soteriav1alpha1.DRPlanSpec{
			VolumeReplicationDriver: soteriav1alpha1.VolumeReplicationDriverConfig{
				Type:                   "noop",
				VolumeReplicationClass: "ceph-rbd",
			},
			MaxConcurrentFailovers: 5,
			PrimarySite:            testPrimarySite,
			SecondarySite:          testSecondarySite,
		},
		Status: soteriav1alpha1.DRPlanStatus{
			ActiveSite: testPrimarySite,
		},
	}

	waves := []soteriav1alpha1.WaveInfo{
		{
			WaveKey: "1",
			Groups: []soteriav1alpha1.VolumeGroupInfo{
				{Name: "vm-default-app1", Namespace: "default", VMNames: []string{"app1"}},
			},
		},
	}

	fakeDriver := fakedrv.New()
	registry := drivers.NewRegistry()
	registry.RegisterDriver("noop", func() drivers.StorageProvider { return fakeDriver })

	pvcResolver := &mockPVCResolverWithNames{names: map[string][]string{
		"default/app1": {"data-pvc"},
	}}

	r := &DRPlanReconciler{
		Client:          fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(plan).Build(),
		Scheme:          newTestScheme(),
		VMDiscoverer:    &mockVMDiscoverer{},
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		Registry:        registry,
		PVCResolver:     pvcResolver,
		LocalSite:       testSecondarySite,
	}

	if err := r.reconcileVolumeReplication(context.Background(), plan, waves); err != nil {
		t.Logf("reconcileVolumeReplication returned error: %v", err)
	}

	calls := fakeDriver.CallsTo("CreateVolumeGroup")
	if len(calls) != 1 {
		t.Fatalf("expected 1 CreateVolumeGroup call, got %d", len(calls))
	}

	spec := calls[0].Args[0].(drivers.VolumeGroupSpec)
	if spec.Labels[drivers.SiteRoleLabel] != drivers.SiteRoleSecondary {
		t.Errorf("site role = %q, want %q", spec.Labels[drivers.SiteRoleLabel], drivers.SiteRoleSecondary)
	}
}

func TestReconcileVolumeReplication_SkippedDuringActiveExecution(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-vr", Generation: 1},
		Spec: soteriav1alpha1.DRPlanSpec{
			VolumeReplicationDriver: soteriav1alpha1.VolumeReplicationDriverConfig{
				Type:                   "noop",
				VolumeReplicationClass: "ceph-rbd",
			},
			MaxConcurrentFailovers: 5,
			PrimarySite:            testPrimarySite,
			SecondarySite:          testSecondarySite,
		},
		Status: soteriav1alpha1.DRPlanStatus{
			ActiveSite: testPrimarySite,
		},
	}

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "exec-1",
			Labels: map[string]string{soteriav1alpha1.PlanNameLabel: "plan-vr"},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-vr",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	waves := []soteriav1alpha1.WaveInfo{
		{
			WaveKey: "1",
			Groups: []soteriav1alpha1.VolumeGroupInfo{
				{Name: "vm-default-app1", Namespace: "default", VMNames: []string{"app1"}},
			},
		},
	}

	fakeDriver := fakedrv.New()
	registry := drivers.NewRegistry()
	registry.RegisterDriver("noop", func() drivers.StorageProvider { return fakeDriver })

	pvcResolver := &mockPVCResolverWithNames{names: map[string][]string{
		"default/app1": {"data-pvc"},
	}}

	scheme := newTestScheme()
	r := &DRPlanReconciler{
		Client:          fake.NewClientBuilder().WithScheme(scheme).WithObjects(plan, exec).Build(),
		Scheme:          scheme,
		VMDiscoverer:    &mockVMDiscoverer{},
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		Registry:        registry,
		PVCResolver:     pvcResolver,
		LocalSite:       testPrimarySite,
	}

	if err := r.reconcileVolumeReplication(context.Background(), plan, waves); err != nil {
		t.Logf("reconcileVolumeReplication returned error: %v", err)
	}

	calls := fakeDriver.CallsTo("CreateVolumeGroup")
	if len(calls) != 0 {
		t.Errorf("expected 0 CreateVolumeGroup calls during active execution, got %d", len(calls))
	}
}

func TestReconcileVolumeReplication_FallbackToPrimarySiteWhenActiveSiteEmpty(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-vr", Generation: 1},
		Spec: soteriav1alpha1.DRPlanSpec{
			VolumeReplicationDriver: soteriav1alpha1.VolumeReplicationDriverConfig{
				Type:                   "noop",
				VolumeReplicationClass: "ceph-rbd",
			},
			MaxConcurrentFailovers: 5,
			PrimarySite:            testPrimarySite,
			SecondarySite:          testSecondarySite,
		},
		Status: soteriav1alpha1.DRPlanStatus{
			ActiveSite: "",
		},
	}

	waves := []soteriav1alpha1.WaveInfo{
		{
			WaveKey: "1",
			Groups: []soteriav1alpha1.VolumeGroupInfo{
				{Name: "vm-default-app1", Namespace: "default", VMNames: []string{"app1"}},
			},
		},
	}

	fakeDriver := fakedrv.New()
	registry := drivers.NewRegistry()
	registry.RegisterDriver("noop", func() drivers.StorageProvider { return fakeDriver })

	pvcResolver := &mockPVCResolverWithNames{names: map[string][]string{
		"default/app1": {"data-pvc"},
	}}

	r := &DRPlanReconciler{
		Client:          fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(plan).Build(),
		Scheme:          newTestScheme(),
		VMDiscoverer:    &mockVMDiscoverer{},
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		Registry:        registry,
		PVCResolver:     pvcResolver,
		LocalSite:       testPrimarySite,
	}

	if err := r.reconcileVolumeReplication(context.Background(), plan, waves); err != nil {
		t.Logf("reconcileVolumeReplication returned error: %v", err)
	}

	calls := fakeDriver.CallsTo("CreateVolumeGroup")
	if len(calls) != 1 {
		t.Fatalf("expected 1 CreateVolumeGroup call, got %d", len(calls))
	}

	spec := calls[0].Args[0].(drivers.VolumeGroupSpec)
	if spec.Labels[drivers.SiteRoleLabel] != drivers.SiteRolePrimary {
		t.Errorf("site role = %q, want %q (fallback to PrimarySite when ActiveSite empty)",
			spec.Labels[drivers.SiteRoleLabel], drivers.SiteRolePrimary)
	}
}

// mockPVCResolverWithNames returns configured PVC names keyed by "namespace/vmName".
type mockPVCResolverWithNames struct {
	names map[string][]string
}

func (m *mockPVCResolverWithNames) ResolvePVCNames(_ context.Context, vmName, namespace string) ([]string, error) {
	key := namespace + "/" + vmName
	if names, ok := m.names[key]; ok {
		return names, nil
	}
	return nil, nil
}
