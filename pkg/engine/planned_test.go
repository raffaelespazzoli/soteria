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

package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/fake"
)

// --- mockVMManager for planned migration tests ---

type mockVMManager struct {
	mu      sync.Mutex
	stops   []string
	starts  []string
	failOn  map[string]error
	running map[string]bool
}

func newMockVMManager() *mockVMManager {
	return &mockVMManager{
		failOn:  make(map[string]error),
		running: make(map[string]bool),
	}
}

func (m *mockVMManager) StopVM(_ context.Context, name, namespace string) error {
	key := namespace + "/" + name
	m.mu.Lock()
	m.stops = append(m.stops, key)
	m.mu.Unlock()
	if err, ok := m.failOn[key]; ok {
		return err
	}
	return nil
}

func (m *mockVMManager) StartVM(_ context.Context, name, namespace string) error {
	key := namespace + "/" + name
	m.mu.Lock()
	m.starts = append(m.starts, key)
	m.mu.Unlock()
	if err, ok := m.failOn[key]; ok {
		return err
	}
	return nil
}

func (m *mockVMManager) IsVMRunning(_ context.Context, name, namespace string) (bool, error) {
	key := namespace + "/" + name
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running[key], nil
}

func (m *mockVMManager) getStops() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.stops))
	copy(out, m.stops)
	return out
}

func (m *mockVMManager) getStarts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.starts))
	copy(out, m.starts)
	return out
}

// --- Test helpers ---

func makeExecutionGroup(
	name string, vms []VMReference, vgs []soteriav1alpha1.VolumeGroupInfo,
	drv drivers.StorageProvider, wave int,
) ExecutionGroup {
	return ExecutionGroup{
		Chunk: DRGroupChunk{
			Name:         name,
			VMs:          vms,
			VolumeGroups: vgs,
		},
		Driver:    drv,
		WaveIndex: wave,
	}
}

func makeVolumeGroupInfo(name, namespace string, vmNames ...string) soteriav1alpha1.VolumeGroupInfo {
	return soteriav1alpha1.VolumeGroupInfo{
		Name:             name,
		Namespace:        namespace,
		ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM,
		VMNames:          vmNames,
	}
}

// --- Tests ---

func TestPlannedMigration_FullSuccess(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleNonReplicated,
			Health: drivers.HealthUnknown,
		},
	})

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	ctx := context.Background()

	if err := handler.PreExecute(ctx, groups); err != nil {
		t.Fatalf("PreExecute failed: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 1 || stops[0] != "ns1/vm-db01" {
		t.Errorf("Expected VM stop ns1/vm-db01, got %v", stops)
	}

	if err := handler.ExecuteGroup(ctx, groups[0]); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	starts := vm.getStarts()
	if len(starts) != 1 || starts[0] != "ns1/vm-db01" {
		t.Errorf("Expected VM start ns1/vm-db01, got %v", starts)
	}

	if !drv.Called("StopReplication") {
		t.Error("Expected StopReplication to be called")
	}
	if !drv.Called("SetSource") {
		t.Error("Expected SetSource to be called")
	}
}

func TestPlannedMigration_Step0_StopVMFails(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	vm.failOn["ns1/vm-db01"] = errors.New("connection refused")

	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	err := handler.PreExecute(context.Background(), groups)
	if err == nil {
		t.Fatal("PreExecute should fail when StopVM fails")
	}
	if !strings.Contains(err.Error(), "stopping origin VM ns1/vm-db01") {
		t.Errorf("Error message should mention the VM: %v", err)
	}
}

func TestPlannedMigration_Step0_StopReplicationFails(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnStopReplication("vg-1").Return(errors.New("storage backend error"))

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	err := handler.PreExecute(context.Background(), groups)
	if err == nil {
		t.Fatal("PreExecute should fail when StopReplication fails")
	}
	if !strings.Contains(err.Error(), "stopping replication for volume group vg-db") {
		t.Errorf("Error message should mention volume group: %v", err)
	}
}

func TestPlannedMigration_Step0_SyncTimeout(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	// Return a status that never syncs (Source role, Degraded health).
	for range 100 {
		drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
			ReplicationStatus: &drivers.ReplicationStatus{
				Role:   drivers.RoleSource,
				Health: drivers.HealthDegraded,
			},
		})
	}

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      50 * time.Millisecond,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	err := handler.PreExecute(context.Background(), groups)
	if err == nil {
		t.Fatal("PreExecute should fail on sync timeout")
	}
	if !strings.Contains(err.Error(), "sync timeout") {
		t.Errorf("Error message should mention sync timeout: %v", err)
	}
}

func TestPlannedMigration_Step0_SyncCompletes(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	// First 2 polls: still syncing. Third poll: synced.
	drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleSource, Health: drivers.HealthSyncing},
	})
	drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleSource, Health: drivers.HealthSyncing},
	})
	drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleNonReplicated, Health: drivers.HealthUnknown},
	})

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute should succeed after 3 polls: %v", err)
	}

	statusCalls := drv.CallCount("GetReplicationStatus")
	if statusCalls < 3 {
		t.Errorf("Expected at least 3 GetReplicationStatus calls, got %d", statusCalls)
	}
}

func TestPlannedMigration_PerGroup_SetSourceFails(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnSetSource("vg-1").Return(errors.New("promotion failed"))

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	err := handler.ExecuteGroup(context.Background(), group)
	if err == nil {
		t.Fatal("ExecuteGroup should fail when SetSource fails")
	}
	if !strings.Contains(err.Error(), "SetSource") {
		t.Errorf("Error should mention SetSource step: %v", err)
	}
}

func TestPlannedMigration_PerGroup_StartVMFails(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	vm.failOn["ns1/vm-db01"] = errors.New("vm start timeout")

	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	err := handler.ExecuteGroup(context.Background(), group)
	if err == nil {
		t.Fatal("ExecuteGroup should fail when StartVM fails")
	}
	if !strings.Contains(err.Error(), "StartVM") {
		t.Errorf("Error should mention StartVM step: %v", err)
	}
}

func TestPlannedMigration_PerGroup_StepStatusRecorded(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{
		{Name: "vm-db01", Namespace: "ns1"},
		{Name: "vm-db02", Namespace: "ns1"},
	}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01", "vm-db02")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	steps, err := handler.ExecuteGroupWithSteps(context.Background(), group)
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", err)
	}

	// Expected: 1 StopReplication + 1 SetSource + 2 StartVM = 4 steps
	if len(steps) != 4 {
		t.Fatalf("Expected 4 step statuses, got %d", len(steps))
	}

	expectedNames := []string{StepStopReplication, StepSetSource, StepStartVM, StepStartVM}
	for i, step := range steps {
		if step.Name != expectedNames[i] {
			t.Errorf("Step %d: name = %q, want %q", i, step.Name, expectedNames[i])
		}
		if step.Status != "Succeeded" {
			t.Errorf("Step %d: status = %q, want Succeeded", i, step.Status)
		}
		if step.Timestamp == nil {
			t.Errorf("Step %d: timestamp should not be nil", i)
		}
	}
}

func TestPlannedMigration_ContextCancelled(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := handler.PreExecute(ctx, groups)
	if err == nil {
		t.Fatal("PreExecute should fail when context is cancelled")
	}
}

func TestPlannedMigration_EmptyGroups(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	// PreExecute with empty groups.
	if err := handler.PreExecute(context.Background(), nil); err != nil {
		t.Fatalf("PreExecute with empty groups should succeed: %v", err)
	}

	// ExecuteGroup with empty chunk.
	group := ExecutionGroup{
		Chunk:     DRGroupChunk{Name: "empty-group"},
		Driver:    drv,
		WaveIndex: 0,
	}
	if err := handler.ExecuteGroup(context.Background(), group); err != nil {
		t.Fatalf("ExecuteGroup with empty chunk should succeed: %v", err)
	}

	if drv.Called("StopReplication") || drv.Called("SetSource") {
		t.Error("No driver calls should be made for empty groups")
	}
}

func TestPlannedMigration_MultiNamespace(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-web"},
	})
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-2", Name: "vg-api"},
	})
	drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleNonReplicated},
	})
	drv.OnGetReplicationStatus("vg-2").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleNonReplicated},
	})

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	groups := []ExecutionGroup{
		makeExecutionGroup("g-0",
			[]VMReference{{Name: "web01", Namespace: "ns-web"}},
			[]soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-web", "ns-web", "web01")},
			drv, 0),
		makeExecutionGroup("g-1",
			[]VMReference{{Name: "api01", Namespace: "ns-api"}},
			[]soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-api", "ns-api", "api01")},
			drv, 1),
	}

	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute failed: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 2 {
		t.Errorf("Expected 2 VM stops (different namespaces), got %d: %v", len(stops), stops)
	}
}

func TestPlannedMigration_VolumeGroupIDCaching(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleNonReplicated,
			Health: drivers.HealthUnknown,
		},
	})

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	ctx := context.Background()

	// PreExecute resolves vg-db → vg-1 (first CreateVolumeGroup call).
	if err := handler.PreExecute(ctx, groups); err != nil {
		t.Fatalf("PreExecute failed: %v", err)
	}

	// ExecuteGroup should reuse cached ID — no additional CreateVolumeGroup.
	if err := handler.ExecuteGroup(ctx, groups[0]); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	createCalls := drv.CallCount("CreateVolumeGroup")
	if createCalls != 1 {
		t.Errorf("Expected 1 CreateVolumeGroup call (cached), got %d", createCalls)
	}
}

func TestPlannedMigration_Step0_DeduplicatesVMs(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-2", Name: "vg-app"},
	})
	drv.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleNonReplicated},
	})
	drv.OnGetReplicationStatus("vg-2").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleNonReplicated},
	})

	vm := newMockVMManager()
	handler := &PlannedMigrationHandler{
		Driver:           drv,
		VMManager:        vm,
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	// Same VM appears in two groups (e.g., different volume groups sharing a VM).
	sharedVM := VMReference{Name: "vm-db01", Namespace: "ns1"}
	groups := []ExecutionGroup{
		makeExecutionGroup("g-0", []VMReference{sharedVM}, []soteriav1alpha1.VolumeGroupInfo{
			makeVolumeGroupInfo("vg-db", "ns1", "vm-db01"),
		}, drv, 0),
		makeExecutionGroup("g-1", []VMReference{sharedVM}, []soteriav1alpha1.VolumeGroupInfo{
			makeVolumeGroupInfo("vg-app", "ns1", "vm-db01"),
		}, drv, 0),
	}

	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute failed: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 1 {
		t.Errorf("VM should only be stopped once (deduplicated), got %d stops: %v", len(stops), stops)
	}
}
