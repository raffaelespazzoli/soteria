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
		Driver:     drv,
		WaveIndex:  wave,
		DriverType: "noop",
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
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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
		t.Fatalf("PreExecute should return nil, got: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 1 || stops[0] != testVMKey {
		t.Errorf("Expected VM stop %s, got %v", testVMKey, stops)
	}

	stopRepCalls := drv.CallsTo("StopReplication")
	if len(stopRepCalls) != 1 {
		t.Errorf("Step 0 should call StopReplication once, got %d calls", len(stopRepCalls))
	}
	if drv.Called("ResyncVolume") {
		t.Error("Step 0 should NOT call ResyncVolume")
	}

	if err := handler.ExecuteGroup(ctx, groups[0]); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	starts := vm.getStarts()
	if len(starts) != 1 || starts[0] != testVMKey {
		t.Errorf("Expected VM start %s, got %v", testVMKey, starts)
	}

	if !drv.Called("SetSource") {
		t.Error("Expected SetSource to be called in per-group path")
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

func TestFailoverHandler_Graceful_Step0_ReverseWaveShutdownOrder(t *testing.T) {
	drv := fake.New()
	for _, vgName := range []string{"vg-db", "vg-app1", "vg-app2", "vg-web1", "vg-web2", "vg-web3"} {
		id := drivers.VolumeGroupID("noop-ns1/" + vgName)
		drv.OnGetVolumeGroup(id).ReturnResult(fake.Response{
			VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: id, Name: vgName},
		})
	}

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	// Build 3 waves in ascending order (as BuildExecutionGroups returns):
	// Wave 0: db, Wave 1: appservers, Wave 2: webservers
	groups := []ExecutionGroup{
		makeExecutionGroup("wave-1-group-0",
			[]VMReference{{Name: "vm-db", Namespace: "ns1"}},
			[]soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db")},
			drv, 0),
		makeExecutionGroup("wave-2-group-0",
			[]VMReference{
				{Name: "vm-app1", Namespace: "ns1"},
				{Name: "vm-app2", Namespace: "ns1"},
			},
			[]soteriav1alpha1.VolumeGroupInfo{
				makeVolumeGroupInfo("vg-app1", "ns1", "vm-app1"),
				makeVolumeGroupInfo("vg-app2", "ns1", "vm-app2"),
			},
			drv, 1),
		makeExecutionGroup("wave-3-group-0",
			[]VMReference{
				{Name: "vm-web1", Namespace: "ns1"},
				{Name: "vm-web2", Namespace: "ns1"},
				{Name: "vm-web3", Namespace: "ns1"},
			},
			[]soteriav1alpha1.VolumeGroupInfo{
				makeVolumeGroupInfo("vg-web1", "ns1", "vm-web1"),
				makeVolumeGroupInfo("vg-web2", "ns1", "vm-web2"),
				makeVolumeGroupInfo("vg-web3", "ns1", "vm-web3"),
			},
			drv, 2),
	}

	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute failed: %v", err)
	}

	stops := vm.getStops()
	// VMs should be stopped in reverse wave order: webservers first, then
	// appservers, then db last — so dependants drain before dependencies.
	expected := []string{
		"ns1/vm-web3", "ns1/vm-web2", "ns1/vm-web1",
		"ns1/vm-app2", "ns1/vm-app1",
		"ns1/vm-db",
	}
	if len(stops) != len(expected) {
		t.Fatalf("Expected %d VM stops, got %d: %v", len(expected), len(stops), stops)
	}
	for i, exp := range expected {
		if stops[i] != exp {
			t.Errorf("Stop order mismatch at index %d: expected %s, got %s\nFull order: %v",
				i, exp, stops[i], stops)
		}
	}
}

func TestFailoverHandler_Graceful_PerGroup_SetSourceAndStartVM(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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

	if !drv.Called("SetSource") {
		t.Error("Per-group should call SetSource to promote target to primary (writable)")
	}
	if drv.Called("StopReplication") {
		t.Error("Per-group should not call StopReplication (Step0 handles demotion for planned)")
	}

	starts := vm.getStarts()
	if len(starts) != 1 || starts[0] != testVMKey {
		t.Errorf("Expected VM start %s, got %v", testVMKey, starts)
	}
}

func TestFailoverHandler_Graceful_PerGroup_StartVMFails(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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

	// Per-group path: 1 SetSource + 2 StartVM = 3 steps
	if len(steps) != 3 {
		t.Fatalf("Expected 3 step statuses (1 SetSource + 2 StartVM), got %d", len(steps))
	}

	if steps[0].Name != StepSetSource {
		t.Errorf("Step 0: name = %q, want %q", steps[0].Name, StepSetSource)
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

	if drv.Called("SetSource") || drv.Called("StopReplication") {
		t.Error("No driver calls should be made for empty groups")
	}
}

func TestFailoverHandler_Graceful_MultiNamespace(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns-web/vg-web").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns-web/vg-web", Name: "vg-web"},
	})
	drv.OnGetVolumeGroup("noop-ns-api/vg-api").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns-api/vg-api", Name: "vg-api"},
	})

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
		t.Fatalf("PreExecute should return nil, got: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 2 {
		t.Errorf("Expected 2 VM stops (different namespaces), got %d: %v", len(stops), stops)
	}

	stopRepCalls := drv.CallsTo("StopReplication")
	if len(stopRepCalls) != 2 {
		t.Errorf("Expected 2 StopReplication calls (one per VG), got %d", len(stopRepCalls))
	}
}

func TestFailoverHandler_Graceful_Step0_DeduplicatesVMs(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	drv.OnGetVolumeGroup("noop-ns1/vg-app").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-app", Name: "vg-app"},
	})

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
		t.Fatalf("PreExecute should return nil, got: %v", err)
	}

	stops := vm.getStops()
	if len(stops) != 1 {
		t.Errorf("VM should only be stopped once (deduplicated), got %d stops: %v", len(stops), stops)
	}

	stopRepCalls := drv.CallsTo("StopReplication")
	if len(stopRepCalls) != 2 {
		t.Errorf("Step 0 should call StopReplication for each VG (2 unique VGs), got %d calls", len(stopRepCalls))
	}
	if drv.Called("ResyncVolume") {
		t.Error("Step 0 should NOT call ResyncVolume")
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
	if drv.Called("ResyncVolume") {
		t.Error("Disaster mode Step 0 should not call ResyncVolume")
	}
}

func TestFailoverHandler_DisasterConfig_SetSourceAndStartVM(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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

	if !drv.Called("SetSource") {
		t.Error("Expected SetSource to be called in per-group to promote target to primary")
	}
	if drv.Called("StopReplication") {
		t.Error("StopReplication should not be called in per-group path")
	}

	calls := drv.CallsTo("SetSource")
	if len(calls) != 1 {
		t.Fatalf("Expected 1 SetSource call, got %d", len(calls))
	}

	starts := vm.getStarts()
	if len(starts) != 1 || starts[0] != testVMKey {
		t.Errorf("Expected VM start %s, got %v", testVMKey, starts)
	}
}

func TestFailoverHandler_DisasterConfig_NoStopReplication(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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

	if drv.Called("StopReplication") {
		t.Error("Disaster mode per-group should NOT call StopReplication (SetSource promotes target)")
	}
}

// --- Disaster failover comprehensive tests (Story 4.4) ---

func TestFailover_Disaster_FullSuccess(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	drv.OnGetVolumeGroup("noop-ns1/vg-app").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-app", Name: "vg-app"},
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

	// 2 SetSource + 2 StartVM = 4 steps
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

	setCalls := drv.CallsTo("SetSource")
	if len(setCalls) != 2 {
		t.Fatalf("Expected 2 SetSource calls, got %d", len(setCalls))
	}

	if drv.Called("StopReplication") {
		t.Error("Disaster failover per-group must not call StopReplication")
	}
}

func TestFailover_Disaster_SetSourceFails(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	drv.OnSetSource("noop-ns1/vg-db").Return(errors.New("set source failed: storage backend error"))

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
		t.Fatal("ExecuteGroupWithSteps should fail when SetSource fails")
	}
	if !strings.Contains(err.Error(), StepSetSource) {
		t.Errorf("Error should mention SetSource step: %v", err)
	}
	if !strings.Contains(err.Error(), "vg-db") {
		t.Errorf("Error should mention volume group name: %v", err)
	}

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (failed SetSource), got %d", len(steps))
	}
	if steps[0].Name != StepSetSource || steps[0].Status != statusFailed {
		t.Errorf("Step should be failed SetSource: %+v", steps[0])
	}

	if len(vm.getStarts()) != 0 {
		t.Error("No VMs should start when SetSource fails")
	}
}

func TestFailover_Disaster_StartVMFails(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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

	// SetSource succeeded, StartVM failed — 2 steps total
	if len(steps) != 2 {
		t.Fatalf("Expected 2 steps (SetSource succeeded, StartVM failed), got %d", len(steps))
	}
	if steps[0].Name != StepSetSource || steps[0].Status != statusSucceeded {
		t.Errorf("First step should be succeeded SetSource: %+v", steps[0])
	}
	if steps[1].Name != StepStartVM || steps[1].Status != statusFailed {
		t.Errorf("Second step should be failed StartVM: %+v", steps[1])
	}
}

func TestFailover_Disaster_StepStatusRecorded(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	drv.OnGetVolumeGroup("noop-ns1/vg-app").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-app", Name: "vg-app"},
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

	// 2 SetSource + 2 StartVM = 4 steps
	if len(steps) != 4 {
		t.Fatalf("Expected 4 steps, got %d", len(steps))
	}

	expectedNames := []string{StepSetSource, StepSetSource, StepStartVM, StepStartVM}
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

	if drv.Called("StopReplication") {
		t.Error("Per-group path should not call StopReplication")
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

	if drv.Called("SetSource") || drv.Called("StopReplication") {
		t.Error("No driver calls should be made for empty groups")
	}
	if len(vm.getStarts()) != 0 {
		t.Error("No VM starts should occur for empty groups")
	}
}

func TestFailover_Disaster_ContextCancelled(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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

func TestFailover_Disaster_NoStopReplicationInPerGroup(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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

	if drv.Called("StopReplication") {
		t.Error("Disaster per-group must not call StopReplication (SetSource promotes target)")
	}

	drv.Reset()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})

	handler2 := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}
	_, err := handler2.ExecuteGroupWithSteps(context.Background(), group)
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", err)
	}
	if drv.Called("StopReplication") {
		t.Error("StopReplication must not be called in per-group path (via steps path)")
	}
}

func TestFailover_Disaster_MultipleVolumeGroups(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	drv.OnGetVolumeGroup("noop-ns1/vg-logs").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-logs", Name: "vg-logs"},
	})
	drv.OnGetVolumeGroup("noop-ns1/vg-config").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-config", Name: "vg-config"},
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

	// 3 SetSource + 2 StartVM = 5
	if len(steps) != 5 {
		t.Fatalf("Expected 5 steps, got %d", len(steps))
	}

	for i := range 3 {
		if steps[i].Name != StepSetSource {
			t.Errorf("Step %d should be SetSource, got %q", i, steps[i].Name)
		}
	}
	for i := 3; i < 5; i++ {
		if steps[i].Name != StepStartVM {
			t.Errorf("Step %d should be StartVM, got %q", i, steps[i].Name)
		}
	}

	setCalls := drv.CallsTo("SetSource")
	if len(setCalls) != 3 {
		t.Fatalf("Expected 3 SetSource calls, got %d", len(setCalls))
	}

	if drv.Called("StopReplication") {
		t.Error("Per-group path should not call StopReplication")
	}
}

func TestFailover_Disaster_PreExecute_NoGracefulShutdown(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
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
	if drv.Called("GetVolumeGroup") {
		t.Error("GetVolumeGroup should not be called during PreExecute for disaster")
	}
}

func TestFailover_ResolveVolumeGroupNotFound(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").Return(drivers.ErrVolumeGroupNotFound)

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	group := makeExecutionGroup("wave-1-group-0", vms, vgs, drv, 0)

	err := handler.ExecuteGroup(context.Background(), group)
	if err == nil {
		t.Fatal("ExecuteGroup should fail when VG not found")
	}
	if !strings.Contains(err.Error(), "VR/VGR not yet created by DRPlan reconciler") {
		t.Errorf("Error should mention DRPlan reconciler, got: %v", err)
	}
	if !strings.Contains(err.Error(), "vg-db") {
		t.Errorf("Error should mention volume group name, got: %v", err)
	}
}

// --- Step 0 StopReplication tests (AC1, AC2) ---

func TestFailoverHandler_Step0_PlannedMigration_CallsStopReplication(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	drv.OnGetVolumeGroup("noop-ns1/vg-logs").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-logs", Name: "vg-logs"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	vms := []VMReference{
		{Name: "vm-db01", Namespace: "ns1"},
		{Name: "vm-app01", Namespace: "ns1"},
	}
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		makeVolumeGroupInfo("vg-db", "ns1", "vm-db01"),
		makeVolumeGroupInfo("vg-logs", "ns1", "vm-app01"),
	}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute should return nil, got: %v", err)
	}

	vmStops := vm.getStops()
	if len(vmStops) != 2 {
		t.Errorf("Expected 2 VM stops, got %d", len(vmStops))
	}

	stopRepCalls := drv.CallsTo("StopReplication")
	if len(stopRepCalls) != 2 {
		t.Fatalf("Expected 2 StopReplication calls (one per VG), got %d", len(stopRepCalls))
	}

	if drv.Called("ResyncVolume") {
		t.Error("Step 0 should NOT call ResyncVolume")
	}
	if drv.Called("SetSource") {
		t.Error("Step 0 should not call SetSource (per-group handles promotion)")
	}
}

func TestFailoverHandler_Step0_DisasterMode_NoResyncVolume(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute should be no-op for disaster: %v", err)
	}

	if len(vm.getStops()) != 0 {
		t.Error("No VMs should be stopped in disaster Step 0")
	}
	if drv.Called("ResyncVolume") {
		t.Error("Disaster Step 0 should not call ResyncVolume (source unreachable)")
	}
	if drv.Called("StopReplication") {
		t.Error("Disaster Step 0 should not call StopReplication")
	}
	if drv.Called("SetSource") {
		t.Error("Disaster Step 0 should not call SetSource")
	}
	if drv.Called("GetVolumeGroup") {
		t.Error("Disaster Step 0 should not resolve volume groups")
	}
}

func TestFailoverHandler_Step0_StopReplicationError_FailsFast(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	drv.OnStopReplication("noop-ns1/vg-db").Return(errors.New("storage backend unreachable"))

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	err := handler.PreExecute(context.Background(), groups)
	if err == nil {
		t.Fatal("PreExecute should fail when Step 0 StopReplication fails")
	}
	if !strings.Contains(err.Error(), "demoting volume group vg-db") {
		t.Errorf("Error should mention StopReplication failure, got: %v", err)
	}

	if !drv.Called("StopReplication") {
		t.Error("StopReplication should have been called before failing")
	}
}

func TestFailoverHandler_Step0_ResolveError_FailsFast(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").Return(drivers.ErrVolumeGroupNotFound)

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	err := handler.PreExecute(context.Background(), groups)
	if err == nil {
		t.Fatal("PreExecute should fail when Step 0 cannot resolve a volume group")
	}
	if !strings.Contains(err.Error(), "resolving volume group vg-db for StopReplication") {
		t.Errorf("Error should mention resolve failure for StopReplication, got: %v", err)
	}
}

// --- Table-driven failover transition tests (AC5, AC6) ---

func TestFailover_PlannedMigration_FullTransition(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    gracefulConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	// Step 0: StopVM + StopReplication → returns nil
	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute should return nil, got: %v", err)
	}

	if len(vm.getStops()) != 1 {
		t.Error("Step 0 should stop VMs")
	}
	stopRepCalls := drv.CallsTo("StopReplication")
	if len(stopRepCalls) != 1 {
		t.Errorf("Step 0 should call StopReplication once, got %d", len(stopRepCalls))
	}
	if drv.Called("ResyncVolume") {
		t.Error("Step 0 should not call ResyncVolume")
	}

	// Per-group on target site: SetSource → target promoted to primary (writable)
	steps, execErr := handler.ExecuteGroupWithSteps(context.Background(), groups[0])
	if execErr != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", execErr)
	}

	setCalls := drv.CallsTo("SetSource")
	if len(setCalls) != 1 {
		t.Errorf("Per-group should call SetSource once, got %d", len(setCalls))
	}

	// StartVM on target
	if len(vm.getStarts()) != 1 {
		t.Error("Per-group should start VM")
	}

	// Verify step sequence: SetSource → StartVM
	if len(steps) != 2 {
		t.Fatalf("Expected 2 steps (SetSource + StartVM), got %d", len(steps))
	}
	if steps[0].Name != StepSetSource {
		t.Errorf("First step should be SetSource, got %q", steps[0].Name)
	}
	if steps[1].Name != StepStartVM {
		t.Errorf("Second step should be StartVM, got %q", steps[1].Name)
	}
}

func TestFailover_Disaster_FullTransition(t *testing.T) {
	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{
		VMManager: vm,
		Config:    disasterConfig(),
	}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	// Step 0: no-op (source unreachable)
	if err := handler.PreExecute(context.Background(), groups); err != nil {
		t.Fatalf("PreExecute should be no-op: %v", err)
	}

	if len(vm.getStops()) != 0 {
		t.Error("Disaster Step 0 should not stop VMs")
	}
	if drv.Called("StopReplication") {
		t.Error("Disaster Step 0 should not call StopReplication")
	}

	// Per-group on target: SetSource → primary, then StartVM
	steps, err := handler.ExecuteGroupWithSteps(context.Background(), groups[0])
	if err != nil {
		t.Fatalf("ExecuteGroupWithSteps failed: %v", err)
	}

	setCalls := drv.CallsTo("SetSource")
	if len(setCalls) != 1 {
		t.Errorf("Per-group should call SetSource once, got %d", len(setCalls))
	}

	if drv.Called("StopReplication") {
		t.Error("Disaster per-group should not call StopReplication")
	}

	if len(vm.getStarts()) != 1 {
		t.Error("Per-group should start VM")
	}

	if len(steps) != 2 {
		t.Fatalf("Expected 2 steps (SetSource + StartVM), got %d", len(steps))
	}
	if steps[0].Name != StepSetSource {
		t.Errorf("First step should be SetSource, got %q", steps[0].Name)
	}
	if steps[1].Name != StepStartVM {
		t.Errorf("Second step should be StartVM, got %q", steps[1].Name)
	}
}

// --- State table invariant test (AC5, AC6) ---
//
// Exercises the full 8-phase cycle sequentially with a shared driver:
//   SteadyState → FailedOver → DRedSteadyState → FailedBack → SteadyState
//
// Verifies the correct driver call pattern (StopReplication + SetSource per
// transition) accumulates correctly across the entire lifecycle. This is an
// engine-layer test: it asserts call sequences, not CR-level
// spec.replicationState values. CR-level assertions (primary/secondary on
// real VR/VGR objects) are covered by pkg/drivers/csiextension/integration_test.go.

func TestStateTableInvariant_FullCycle(t *testing.T) {
	ctx := context.Background()

	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})
	vm := newMockVMManager()

	runPlannedFailover := func(t *testing.T, label string) {
		t.Helper()
		handler := &FailoverHandler{VMManager: vm, Config: gracefulConfig()}
		vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
		vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
		groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}
		if err := handler.PreExecute(ctx, groups); err != nil {
			t.Fatalf("%s PreExecute should return nil, got: %v", label, err)
		}
		if err := handler.ExecuteGroup(ctx, groups[0]); err != nil {
			t.Fatalf("%s ExecuteGroup failed: %v", label, err)
		}
	}

	runReprotect := func(t *testing.T, label string) {
		t.Helper()
		rh := &ReprotectHandler{HealthPollInterval: 10 * time.Millisecond, HealthTimeout: 50 * time.Millisecond}
		// Phase 1: RoleSource (primary on active site — legitimate, passes).
		drv.OnGetReplicationStatus("noop-ns1/vg-db").ReturnResult(fake.Response{
			ReplicationStatus: &drivers.ReplicationStatus{
				Role:   drivers.RoleSource,
				Health: drivers.HealthHealthy,
			},
		})
		entry := VolumeGroupEntry{
			Info:   makeVolumeGroupInfo("vg-db", "ns1", "vm-db01"),
			Driver: drv,
			VGID:   "noop-ns1/vg-db",
		}
		input := ReprotectInput{
			Execution:    &soteriav1alpha1.DRExecution{},
			Plan:         &soteriav1alpha1.DRPlan{},
			VolumeGroups: []VolumeGroupEntry{entry},
		}
		result, err := rh.Execute(ctx, input)
		if err != nil {
			t.Fatalf("%s Execute failed: %v", label, err)
		}
		if result.SetupSucceeded != 1 {
			t.Errorf("%s: expected 1 VG setup succeeded, got %d", label, result.SetupSucceeded)
		}
	}

	// PreExecute calls StopReplication (demote) on each source VG. Reprotect
	// on the Owner site only verifies state and monitors health — no driver
	// mutations. ResyncVolume is never called by the engine.
	assertCumulativeCounts := func(t *testing.T, label string, wantStop, wantSet, wantStarts, wantStops int) {
		t.Helper()
		if got := len(drv.CallsTo("StopReplication")); got != wantStop {
			t.Errorf("%s: cumulative StopReplication calls = %d, want %d", label, got, wantStop)
		}
		if got := len(drv.CallsTo("SetSource")); got != wantSet {
			t.Errorf("%s: cumulative SetSource calls = %d, want %d", label, got, wantSet)
		}
		if got := len(vm.getStarts()); got != wantStarts {
			t.Errorf("%s: cumulative VM starts = %d, want %d", label, got, wantStarts)
		}
		if got := len(vm.getStops()); got != wantStops {
			t.Errorf("%s: cumulative VM stops = %d, want %d", label, got, wantStops)
		}
		if got := len(drv.CallsTo("ResyncVolume")); got != 0 {
			t.Errorf("%s: cumulative ResyncVolume calls = %d, want 0", label, got)
		}
	}

	// Phase 1: Planned failover (SteadyState → FailedOver)
	// Step0: StopReplication(1) + StopVM(1), Per-group: SetSource(1) + StartVM(1)
	runPlannedFailover(t, "Phase1_PlannedFailover")
	assertCumulativeCounts(t, "after Phase1", 1, 1, 1, 1)

	// Phase 2: Reprotect (FailedOver → DRedSteadyState)
	// Owner site verifies RoleSource (legitimate primary), no driver mutations.
	runReprotect(t, "Phase2_Reprotect")
	assertCumulativeCounts(t, "after Phase2", 1, 1, 1, 1)

	// Phase 3: Failback (DRedSteadyState → FailedBack)
	// Step0: StopReplication(+1=2) + StopVM(+1=2), Per-group: SetSource(+1=2) + StartVM(+1=2)
	runPlannedFailover(t, "Phase3_Failback")
	assertCumulativeCounts(t, "after Phase3", 2, 2, 2, 2)

	// Phase 4: Restore (FailedBack → SteadyState)
	// Owner site verifies RoleSource (legitimate primary), no driver mutations.
	runReprotect(t, "Phase4_Restore")
	assertCumulativeCounts(t, "after Phase4", 2, 2, 2, 2)
}

func TestStateTableInvariant_DisasterFailover(t *testing.T) {
	ctx := context.Background()

	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-db").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-db", Name: "vg-db"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{VMManager: vm, Config: disasterConfig()}

	vms := []VMReference{{Name: "vm-db01", Namespace: "ns1"}}
	vgs := []soteriav1alpha1.VolumeGroupInfo{makeVolumeGroupInfo("vg-db", "ns1", "vm-db01")}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	// Step 0: no-op (source unreachable) — NO StopReplication
	if err := handler.PreExecute(ctx, groups); err != nil {
		t.Fatalf("PreExecute should be no-op: %v", err)
	}
	if drv.Called("StopReplication") {
		t.Error("Disaster Step 0 should not call StopReplication")
	}
	if len(vm.getStops()) != 0 {
		t.Error("Disaster Step 0 should not stop VMs")
	}

	// Per-group: SetSource → target=primary (writable), then StartVM
	if err := handler.ExecuteGroup(ctx, groups[0]); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	setCalls := drv.CallsTo("SetSource")
	if len(setCalls) != 1 {
		t.Errorf("Expected 1 SetSource call, got %d", len(setCalls))
	}
	if drv.Called("StopReplication") {
		t.Error("Disaster per-group should not call StopReplication")
	}
	if len(vm.getStarts()) != 1 {
		t.Error("Per-group should start 1 VM")
	}
}

func TestStateTableInvariant_MultipleVolumeGroups_VR_and_VGR(t *testing.T) {
	ctx := context.Background()

	drv := fake.New()
	drv.OnGetVolumeGroup("noop-ns1/vg-single").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-single", Name: "vg-single"},
	})
	drv.OnGetVolumeGroup("noop-ns1/vg-multi").ReturnResult(fake.Response{
		VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "noop-ns1/vg-multi", Name: "vg-multi"},
	})

	vm := newMockVMManager()
	handler := &FailoverHandler{VMManager: vm, Config: gracefulConfig()}

	vms := []VMReference{
		{Name: "vm-single", Namespace: "ns1"},
		{Name: "vm-multi-a", Namespace: "ns1"},
		{Name: "vm-multi-b", Namespace: "ns1"},
	}
	vgs := []soteriav1alpha1.VolumeGroupInfo{
		makeVolumeGroupInfo("vg-single", "ns1", "vm-single"),
		makeVolumeGroupInfo("vg-multi", "ns1", "vm-multi-a", "vm-multi-b"),
	}
	groups := []ExecutionGroup{makeExecutionGroup("g-0", vms, vgs, drv, 0)}

	// Step 0: StopVM for all 3 VMs, StopReplication for both VGs
	if err := handler.PreExecute(ctx, groups); err != nil {
		t.Fatalf("PreExecute should return nil, got: %v", err)
	}

	if len(vm.getStops()) != 3 {
		t.Errorf("Expected 3 VM stops, got %d", len(vm.getStops()))
	}
	stopRepCalls := drv.CallsTo("StopReplication")
	if len(stopRepCalls) != 2 {
		t.Errorf("Expected 2 StopReplication calls (one per VG), got %d", len(stopRepCalls))
	}
	if drv.Called("ResyncVolume") {
		t.Error("Step 0 should NOT call ResyncVolume")
	}

	// Per-group: SetSource for both VGs, StartVM for all 3 VMs
	if err := handler.ExecuteGroup(ctx, groups[0]); err != nil {
		t.Fatalf("ExecuteGroup failed: %v", err)
	}

	setCalls := drv.CallsTo("SetSource")
	if len(setCalls) != 2 {
		t.Errorf("Expected 2 SetSource calls (VR + VGR), got %d", len(setCalls))
	}
	if len(vm.getStarts()) != 3 {
		t.Errorf("Expected 3 VM starts, got %d", len(vm.getStarts()))
	}
}
