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

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/fake"
)

// --- mockVMManager for failover handler tests ---

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

func (m *mockVMManager) IsVMReady(_ context.Context, name, namespace string) (bool, error) {
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

const (
	testVMKey       = "ns1/vm-db01"
	statusSucceeded = "Succeeded"
	statusFailed    = "Failed"
)

func gracefulConfig() FailoverConfig {
	return FailoverConfig{GracefulShutdown: true}
}

func disasterConfig() FailoverConfig {
	return FailoverConfig{GracefulShutdown: false}
}

// --- Planned migration (GracefulShutdown=true) tests ---

func TestFailoverHandler_Graceful_FullSuccess(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	ctx := context.Background()

	if err := handler.PreExecute(ctx, groups); err != nil {
		t.Fatalf("PreExecute failed: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 1 || stops[0] != testVMKey {
		t.Errorf("Expected VM stop %s, got %v", testVMKey, stops)
	}

	// PreExecute should only stop VMs (no driver calls).
	if drv.Called("StopReplication") {
		t.Error("PreExecute should not call StopReplication (per-group handles it)")
	}

	if err := handler.ExecuteGroup(ctx, groups[0]); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	starts := vm.getStarts()
	if len(starts) != 1 || starts[0] != testVMKey {
		t.Errorf("Expected VM start %s, got %v", testVMKey, starts)
	}

	if !drv.Called("StopReplication") {
		t.Error("Expected StopReplication to be called in per-group path")
	}
	if drv.Called("SetSource") {
		t.Error("SetSource should not be called during failover (belongs in reprotect)")
	}
}

func TestFailoverHandler_Graceful_Step0_StopVMFails(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	vm.failOn[testVMKey] = errors.New("connection refused")

	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
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

func TestFailoverHandler_Graceful_PerGroup_UnifiedPath(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	if err := handler.ExecuteGroup(context.Background(), group); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	if !drv.Called("StopReplication") {
		t.Error("Graceful per-group should call StopReplication (idempotent no-op after Step 0)")
	}
	if drv.Called("SetSource") {
		t.Error("Graceful per-group should not call SetSource (reprotect handles it)")
	}

	starts := vm.getStarts()
	if len(starts) != 1 || starts[0] != testVMKey {
		t.Errorf("Expected VM start %s, got %v", testVMKey, starts)
	}
}

func TestFailoverHandler_Graceful_PerGroup_StartVMFails(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	vm.failOn[testVMKey] = errors.New("vm start timeout")

	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
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

func TestFailoverHandler_Graceful_PerGroup_StepStatusRecorded(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
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

	// Unified path: 1 StopReplication + 2 StartVM = 3 steps
	if len(steps) != 3 {
		t.Fatalf("Expected 3 step statuses (1 StopReplication + 2 StartVM), got %d", len(steps))
	}

	if steps[0].Name != StepStopReplication {
		t.Errorf("Step 0: name = %q, want %q", steps[0].Name, StepStopReplication)
	}
	for i := 1; i < 3; i++ {
		if steps[i].Name != StepStartVM {
			t.Errorf("Step %d: name = %q, want %q", i, steps[i].Name, StepStartVM)
		}
	}
	for i, step := range steps {
		if step.Status != statusSucceeded {
			t.Errorf("Step %d: status = %q, want Succeeded", i, step.Status)
		}
		if step.Timestamp == nil {
			t.Errorf("Step %d: timestamp should not be nil", i)
		}
	}
}

func TestFailoverHandler_Graceful_ContextCancelled(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
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

func TestFailoverHandler_Graceful_EmptyGroups(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	if err := handler.PreExecute(context.Background(), nil); err != nil {
		t.Fatalf("PreExecute with empty groups should succeed: %v", err)
	}

	group := ExecutionGroup{
		Chunk:     DRGroupChunk{Name: "empty-group"},
		Driver:    drv,
		WaveIndex: 0,
	}
	if err := handler.ExecuteGroup(context.Background(), group); err != nil {
		t.Fatalf("ExecuteGroup with empty chunk should succeed: %v", err)
	}

	if drv.Called("StopReplication") {
		t.Error("No driver calls should be made for empty groups")
	}
}

func TestFailoverHandler_Graceful_MultiNamespace(t *testing.T) {
	drv := fake.New()

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
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

func TestFailoverHandler_Graceful_Step0_DeduplicatesVMs(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

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

	// PreExecute should not call any driver methods.
	if drv.Called("StopReplication") || drv.Called("CreateVolumeGroup") {
		t.Error("PreExecute should not call storage driver methods")
	}
}

// --- Disaster failover (GracefulShutdown=false) tests ---

func TestFailoverHandler_DisasterConfig_NoStep0(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)}

	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute should be a no-op for disaster: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 0 {
		t.Errorf("Disaster mode should not stop VMs in Step 0, got %d stops", len(stops))
	}
	if drv.Called("StopReplication") {
		t.Error("Disaster mode Step 0 should not call StopReplication")
	}
}

func TestFailoverHandler_DisasterConfig_StopReplicationAndStartVM(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	if err := handler.ExecuteGroup(context.Background(), group); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	if !drv.Called("StopReplication") {
		t.Error("Expected StopReplication to be called for disaster")
	}
	if drv.Called("SetSource") {
		t.Error("SetSource should not be called during failover")
	}

	calls := drv.CallsTo("StopReplication")
	if len(calls) != 1 {
		t.Fatalf("Expected 1 StopReplication call, got %d", len(calls))
	}

	starts := vm.getStarts()
	if len(starts) != 1 || starts[0] != testVMKey {
		t.Errorf("Expected VM start %s, got %v", testVMKey, starts)
	}
}

func TestFailoverHandler_DisasterConfig_NoSetSource(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	if err := handler.ExecuteGroup(context.Background(), group); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	if drv.Called("SetSource") {
		t.Error("Disaster mode per-group should NOT call SetSource (reprotect handles it)")
	}
}

// --- Disaster failover comprehensive tests (Story 4.4) ---

func TestFailover_Disaster_FullSuccess(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-2", Name: "vg-app"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{
		{Name: "vm-db01", Namespace: "ns1"},
		{Name: "vm-app01", Namespace: "ns1"},
	}
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		makeVolumeGroupInfo("vg-db", "ns1", "vm-db01"),
		makeVolumeGroupInfo("vg-app", "ns1", "vm-app01"),
	}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	groups := []ExecutionGroup{group}
	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute should be no-op for disaster: %v", err)
	}
	if len(vm.getStops()) != 0 {
		t.Error("No VMs should be stopped in disaster mode")
	}

	steps, err := handler.ExecuteGroupWithSteps(context.Background(), group)
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", err)
	}

	// 2 StopReplication + 2 StartVM = 4 steps
	if len(steps) != 4 {
		t.Fatalf("Expected 4 steps, got %d", len(steps))
	}

	for _, s := range steps {
		if s.Status != statusSucceeded {
			t.Errorf("Step %q should be Succeeded, got %q", s.Name, s.Status)
		}
	}

	starts := vm.getStarts()
	if len(starts) != 2 {
		t.Errorf("Expected 2 VM starts, got %d", len(starts))
	}

	stopCalls := drv.CallsTo("StopReplication")
	if len(stopCalls) != 2 {
		t.Fatalf("Expected 2 StopReplication calls, got %d", len(stopCalls))
	}

	if drv.Called("SetSource") {
		t.Error("Disaster failover must not call SetSource (reprotect handles it)")
	}
}

func TestFailover_Disaster_StopReplicationFails(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnStopReplication("vg-1").Return(errors.New("force stop failed: storage backend error"))

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	steps, err := handler.ExecuteGroupWithSteps(context.Background(), group)
	if err == nil {
		t.Fatal("ExecuteGroupWithSteps should fail when StopReplication fails")
	}
	if !strings.Contains(err.Error(), StepStopReplication) {
		t.Errorf("Error should mention StopReplication step: %v", err)
	}
	if !strings.Contains(err.Error(), "vg-db") {
		t.Errorf("Error should mention volume group name: %v", err)
	}

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (failed StopReplication), got %d", len(steps))
	}
	if steps[0].Name != StepStopReplication || steps[0].Status != statusFailed {
		t.Errorf("Step should be failed StopReplication: %+v", steps[0])
	}

	if len(vm.getStarts()) != 0 {
		t.Error("No VMs should start when StopReplication fails")
	}
}

func TestFailover_Disaster_StartVMFails(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	vm.failOn["ns1/vm-db01"] = errors.New("VM boot timeout")

	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	steps, err := handler.ExecuteGroupWithSteps(context.Background(), group)
	if err == nil {
		t.Fatal("ExecuteGroupWithSteps should fail when StartVM fails")
	}
	if !strings.Contains(err.Error(), StepStartVM) {
		t.Errorf("Error should mention StartVM step: %v", err)
	}

	// StopReplication succeeded, StartVM failed — 2 steps total
	if len(steps) != 2 {
		t.Fatalf("Expected 2 steps (StopReplication succeeded, StartVM failed), got %d", len(steps))
	}
	if steps[0].Name != StepStopReplication || steps[0].Status != statusSucceeded {
		t.Errorf("First step should be succeeded StopReplication: %+v", steps[0])
	}
	if steps[1].Name != StepStartVM || steps[1].Status != statusFailed {
		t.Errorf("Second step should be failed StartVM: %+v", steps[1])
	}
}

func TestFailover_Disaster_StepStatusRecorded(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-2", Name: "vg-app"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{
		{Name: "vm-db01", Namespace: "ns1"},
		{Name: "vm-app01", Namespace: "ns1"},
	}
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		makeVolumeGroupInfo("vg-db", "ns1", "vm-db01"),
		makeVolumeGroupInfo("vg-app", "ns1", "vm-app01"),
	}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	steps, err := handler.ExecuteGroupWithSteps(context.Background(), group)
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", err)
	}

	// 2 StopReplication + 2 StartVM = 4 steps
	if len(steps) != 4 {
		t.Fatalf("Expected 4 steps, got %d", len(steps))
	}

	expectedNames := []string{StepStopReplication, StepStopReplication, StepStartVM, StepStartVM}
	for i, step := range steps {
		if step.Name != expectedNames[i] {
			t.Errorf("Step %d: name = %q, want %q", i, step.Name, expectedNames[i])
		}
		if step.Status != statusSucceeded {
			t.Errorf("Step %d: status = %q, want Succeeded", i, step.Status)
		}
		if step.Timestamp == nil {
			t.Errorf("Step %d: timestamp should not be nil", i)
		}
	}

	for _, step := range steps {
		if step.Name == "SetSource" {
			t.Error("Disaster mode should not have SetSource steps (reprotect handles it)")
		}
	}
}

func TestFailover_Disaster_EmptyGroup(t *testing.T) {
	drv := fake.New()
	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	group := ExecutionGroup{
		Chunk:     DRGroupChunk{Name: "empty-group"},
		Driver:    drv,
		WaveIndex: 0,
	}

	if err := handler.ExecuteGroup(context.Background(), group); err != nil {
		t.Fatalf("ExecuteGroup with empty chunk should succeed: %v", err)
	}

	steps, err := handler.ExecuteGroupWithSteps(context.Background(), group)
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps with empty chunk should succeed: %v", err)
	}

	// No VGs → no RPOSummary; no VMs → no steps at all
	if len(steps) != 0 {
		t.Errorf("Expected 0 steps for empty group, got %d", len(steps))
	}

	if drv.Called("StopReplication") || drv.Called("SetSource") {
		t.Error("No driver calls should be made for empty groups")
	}
	if len(vm.getStarts()) != 0 {
		t.Error("No VM starts should occur for empty groups")
	}
}

func TestFailover_Disaster_ContextCancelled(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := handler.ExecuteGroup(ctx, group)
	if err == nil {
		t.Fatal("ExecuteGroup should fail when context is cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Error should be context.Canceled, got: %v", err)
	}

	_, err = handler.ExecuteGroupWithSteps(ctx, group)
	if err == nil {
		t.Fatal("ExecuteGroupWithSteps should fail when context is cancelled")
	}
}

func TestFailover_Disaster_NoSetSource(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	if err := handler.ExecuteGroup(context.Background(), group); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	if drv.Called("SetSource") {
		t.Error("Disaster failover must never call SetSource (reprotect handles it)")
	}

	drv.Reset()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	handler2 := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}
	_, err := handler2.ExecuteGroupWithSteps(context.Background(), group)
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", err)
	}
	if drv.Called("SetSource") {
		t.Error("SetSource must never be called for disaster config (via steps path)")
	}
}

func TestFailover_Disaster_MultipleVolumeGroups(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-2", Name: "vg-logs"},
	})
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-3", Name: "vg-config"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{
		{Name: "vm-db01", Namespace: "ns1"},
		{Name: "vm-app01", Namespace: "ns1"},
	}
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		makeVolumeGroupInfo("vg-db", "ns1", "vm-db01"),
		makeVolumeGroupInfo("vg-logs", "ns1", "vm-db01"),
		makeVolumeGroupInfo("vg-config", "ns1", "vm-app01"),
	}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	steps, err := handler.ExecuteGroupWithSteps(context.Background(), group)
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", err)
	}

	// 3 StopReplication + 2 StartVM = 5
	if len(steps) != 5 {
		t.Fatalf("Expected 5 steps, got %d", len(steps))
	}

	for i := range 3 {
		if steps[i].Name != StepStopReplication {
			t.Errorf("Step %d should be StopReplication, got %q", i, steps[i].Name)
		}
	}
	for i := 3; i < 5; i++ {
		if steps[i].Name != StepStartVM {
			t.Errorf("Step %d should be StartVM, got %q", i, steps[i].Name)
		}
	}

	stopCalls := drv.CallsTo("StopReplication")
	if len(stopCalls) != 3 {
		t.Fatalf("Expected 3 StopReplication calls, got %d", len(stopCalls))
	}

	if drv.Called("SetSource") {
		t.Error("Disaster mode should not call SetSource")
	}
}

func TestFailover_Disaster_PreExecute_NoGracefulShutdown(t *testing.T) {
	drv := fake.New()
	drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{
		{Name: "vm-db01", Namespace: "ns1"},
		{Name: "vm-app01", Namespace: "ns2"},
	}
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		makeVolumeGroupInfo("vg-db", "ns1", "vm-db01"),
	}
	groups := []ExecutionGroup{
		makeExecutionGroup("g-0", vms, vgs, drv, 0),
	}

	err := handler.PreExecute(context.Background(), groups)
	if err != nil {
		t.Fatalf("PreExecute should return nil for GracefulShutdown=false: %v", err)
	}

	if len(vm.getStops()) != 0 {
		t.Error("No VMs should be stopped when GracefulShutdown=false")
	}
	if drv.Called("StopReplication") {
		t.Error("StopReplication should not be called when GracefulShutdown=false")
	}
	if drv.Called("GetReplicationStatus") {
		t.Error("GetReplicationStatus should not be called during PreExecute for disaster")
	}
	if drv.Called("CreateVolumeGroup") {
		t.Error("CreateVolumeGroup should not be called during PreExecute for disaster")
	}
}
