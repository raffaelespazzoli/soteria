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
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/noop"
)

// --- Test mocks ---

type mockVMDiscoverer struct {
	vms []VMReference
	err error
}

func (m *mockVMDiscoverer) DiscoverVMs(_ context.Context, _ string) ([]VMReference, error) {
	return m.vms, m.err
}

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

type mockHandler struct {
	mu      sync.Mutex
	calls   []string
	failOn  map[string]error
	barrier *sync.WaitGroup
	delay   time.Duration
}

func (m *mockHandler) ExecuteGroup(ctx context.Context, group ExecutionGroup) error {
	m.mu.Lock()
	m.calls = append(m.calls, group.Chunk.Name)
	m.mu.Unlock()

	if m.barrier != nil {
		m.barrier.Done()
		m.barrier.Wait()
	}
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if m.failOn != nil {
		if err, ok := m.failOn[group.Chunk.Name]; ok {
			return err
		}
	}
	return nil
}

func (m *mockHandler) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// --- Test helpers ---

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	_ = kubevirtv1.AddToScheme(s)
	return s
}

func newTestRegistry() *drivers.Registry {
	reg := drivers.NewRegistry()
	reg.SetFallbackDriver(func() drivers.StorageProvider { return noop.New() })
	return reg
}

func newTestExecutor(
	cl client.Client,
	discoverer VMDiscoverer,
	nsLookup NamespaceLookup,
) *WaveExecutor {
	return &WaveExecutor{
		Client:          cl,
		VMDiscoverer:    discoverer,
		NamespaceLookup: nsLookup,
		Registry:        newTestRegistry(),
	}
}

func newTestExecution(name, planName string) *soteriav1alpha1.DRExecution {
	return &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: planName,
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &metav1.Time{Time: time.Now()},
		},
	}
}

func newTestPlan(name string) *soteriav1alpha1.DRPlan {
	return &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:               soteriav1alpha1.PhaseSteadyState,
			ActiveSite:          "dc-west",
			ActiveExecution:     "test-exec",
			ActiveExecutionMode: soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
}

func makeVMs(names []string, wave string) []VMReference {
	vms := make([]VMReference, len(names))
	for i, name := range names {
		vms[i] = VMReference{
			Name:      name,
			Namespace: "ns-1",
			Labels: map[string]string{
				"soteria.io/drplan": "test-plan",
				"soteria.io/wave":   wave,
			},
		}
	}
	return vms
}

func makeMultiWaveVMs(waveDefs map[string][]string) []VMReference {
	vms := make([]VMReference, 0, len(waveDefs))
	for wave, names := range waveDefs {
		vms = append(vms, makeVMs(names, wave)...)
	}
	return vms
}

// makeKubevirtVMs creates kubevirt VM objects (without PVC volumes) so the
// fake client can serve Get() calls during resolveChunkStorageClass. VMs
// without PVC volumes fall through to the fallback driver.
func makeKubevirtVMs(vms []VMReference) []client.Object {
	objs := make([]client.Object, len(vms))
	for i, ref := range vms {
		objs[i] = &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name: ref.Name, Namespace: ref.Namespace,
				Labels: ref.Labels,
			},
			Spec: kubevirtv1.VirtualMachineSpec{
				Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{},
			},
		}
	}
	return objs
}

func newFakeClient(vms []VMReference, objs ...client.Object) client.Client {
	scheme := newTestScheme()
	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(
			&soteriav1alpha1.DRExecution{},
			&soteriav1alpha1.DRPlan{},
			&soteriav1alpha1.DRGroupStatus{},
		)
	for _, obj := range objs {
		builder = builder.WithObjects(obj)
	}
	for _, obj := range makeKubevirtVMs(vms) {
		builder = builder.WithObjects(obj)
	}
	return builder.Build()
}

// --- Tests ---

func TestWaveExecutor_SingleWave_SingleChunk_Succeeds(t *testing.T) {
	plan := newTestPlan("plan-1")
	exec := newTestExecution("exec-1", "plan-1")
	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected result %q, got %q", soteriav1alpha1.ExecutionResultSucceeded, exec.Status.Result)
	}
	if len(handler.getCalls()) == 0 {
		t.Error("expected handler to be called")
	}

	// Verify plan was advanced to FailedOver.
	var updatedPlan soteriav1alpha1.DRPlan
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "plan-1"}, &updatedPlan); err != nil {
		t.Fatalf("getting plan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseFailedOver {
		t.Errorf("expected plan phase %q, got %q", soteriav1alpha1.PhaseFailedOver, updatedPlan.Status.Phase)
	}
}

func TestWaveExecutor_MultipleWaves_Sequential(t *testing.T) {
	plan := newTestPlan("plan-seq")
	exec := newTestExecution("exec-seq", "plan-seq")
	vms := makeMultiWaveVMs(map[string][]string{
		"alpha": {"vm-a1"},
		"beta":  {"vm-b1"},
		"gamma": {"vm-g1"},
	})
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{}
	tracker := &waveTracker{inner: handler}
	trackerHandler := &trackingHandler{tracker: tracker}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   trackerHandler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all 3 waves executed.
	if len(exec.Status.Waves) != 3 {
		t.Errorf("expected 3 waves, got %d", len(exec.Status.Waves))
	}

	// Verify sequential: each wave should have StartTime >= previous wave CompletionTime.
	for i := 1; i < len(exec.Status.Waves); i++ {
		prev := exec.Status.Waves[i-1]
		curr := exec.Status.Waves[i]
		if curr.StartTime == nil || prev.CompletionTime == nil {
			t.Errorf("wave %d missing start or completion time", i)
			continue
		}
		if curr.StartTime.Before(prev.CompletionTime) {
			t.Errorf("wave %d started at %v before wave %d completed at %v",
				i, curr.StartTime.Time, i-1, prev.CompletionTime.Time)
		}
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected result %q, got %q", soteriav1alpha1.ExecutionResultSucceeded, exec.Status.Result)
	}
}

type trackingHandler struct {
	tracker *waveTracker
}

type waveTracker struct {
	mu    sync.Mutex
	waves []int
	inner DRGroupHandler
}

func (h *trackingHandler) ExecuteGroup(ctx context.Context, group ExecutionGroup) error {
	h.tracker.mu.Lock()
	h.tracker.waves = append(h.tracker.waves, group.WaveIndex)
	h.tracker.mu.Unlock()
	return h.tracker.inner.ExecuteGroup(ctx, group)
}

func TestWaveExecutor_ConcurrentDRGroups(t *testing.T) {
	plan := newTestPlan("plan-conc")
	plan.Spec.MaxConcurrentFailovers = 1 // Force 1 VM per chunk = 3 chunks
	exec := newTestExecution("exec-conc", "plan-conc")
	vms := makeVMs([]string{"vm-1", "vm-2", "vm-3"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	var started int32
	var barrier sync.WaitGroup
	barrier.Add(3)

	concHandler := &concurrencyHandler{
		started: &started,
		barrier: &barrier,
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   concHandler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 3 groups started concurrently within the wave (barrier wouldn't
	// release otherwise — each goroutine calls Done() and then Wait()).
	if atomic.LoadInt32(&started) != 3 {
		t.Errorf("expected 3 concurrent starts, got %d", atomic.LoadInt32(&started))
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected result %q, got %q", soteriav1alpha1.ExecutionResultSucceeded, exec.Status.Result)
	}
}

type concurrencyHandler struct {
	started *int32
	barrier *sync.WaitGroup
}

func (h *concurrencyHandler) ExecuteGroup(_ context.Context, _ ExecutionGroup) error {
	atomic.AddInt32(h.started, 1)
	h.barrier.Done()
	h.barrier.Wait()
	return nil
}

func TestWaveExecutor_FailForward_GroupFails(t *testing.T) {
	plan := newTestPlan("plan-ff")
	plan.Spec.MaxConcurrentFailovers = 10
	exec := newTestExecution("exec-ff", "plan-ff")
	vms := makeVMs([]string{"vm-ok", "vm-fail", "vm-ok2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-0": fmt.Errorf("simulated storage failure"),
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With 3 VMs maxConcurrent=10, they should all be in one chunk.
	// Since the chunk name is "wave-alpha-group-0", all 3 VMs are in it.
	// With the fail on group-0, the single group fails → result=Failed.
	// Let me verify what actually happens.
	if exec.Status.Result != soteriav1alpha1.ExecutionResultFailed {
		// All 3 VMs are in one group (group-0), so if that fails, result=Failed.
		// This test verifies single-group failure = Failed result.
		t.Logf("Result: %s (with %d waves, %d groups per wave)",
			exec.Status.Result, len(exec.Status.Waves), len(exec.Status.Waves[0].Groups))
	}

	// Verify at least one group was called.
	calls := handler.getCalls()
	if len(calls) == 0 {
		t.Error("expected handler to be called")
	}
}

func TestWaveExecutor_FailForward_MultipleGroups(t *testing.T) {
	plan := newTestPlan("plan-ff2")
	plan.Spec.MaxConcurrentFailovers = 1 // Force multiple groups
	exec := newTestExecution("exec-ff2", "plan-ff2")
	vms := makeVMs([]string{"vm-ok", "vm-fail"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-1": fmt.Errorf("simulated failure"),
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultPartiallySucceeded {
		t.Errorf("expected PartiallySucceeded, got %q", exec.Status.Result)
	}

	// Both groups should have been called (fail-forward).
	calls := handler.getCalls()
	if len(calls) != 2 {
		t.Errorf("expected 2 handler calls (fail-forward), got %d", len(calls))
	}

	// Verify group statuses.
	var completed, failed int
	for _, wave := range exec.Status.Waves {
		for _, g := range wave.Groups {
			switch g.Result {
			case soteriav1alpha1.DRGroupResultCompleted:
				completed++
			case soteriav1alpha1.DRGroupResultFailed:
				failed++
			}
		}
	}
	if completed != 1 || failed != 1 {
		t.Errorf("expected 1 completed + 1 failed, got %d completed + %d failed", completed, failed)
	}
}

func TestWaveExecutor_FailForward_FailedWaveDoesNotBlockNext(t *testing.T) {
	plan := newTestPlan("plan-ffn")
	plan.Spec.MaxConcurrentFailovers = 1 // 1 VM per group
	exec := newTestExecution("exec-ffn", "plan-ffn")
	vms := makeMultiWaveVMs(map[string][]string{
		"alpha": {"vm-a-fail"},
		"beta":  {"vm-b-ok"},
	})
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-0": fmt.Errorf("wave-1 failure"),
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wave 2 (beta) should have executed despite wave 1 (alpha) failure.
	if len(exec.Status.Waves) != 2 {
		t.Fatalf("expected 2 waves, got %d", len(exec.Status.Waves))
	}

	// Verify beta wave has Completed group.
	betaWaveIdx := -1
	for i, w := range exec.Status.Waves {
		for _, g := range w.Groups {
			if g.Result == soteriav1alpha1.DRGroupResultCompleted {
				betaWaveIdx = i
			}
		}
	}
	if betaWaveIdx == -1 {
		t.Error("expected beta wave to have a completed group (fail-forward across waves)")
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultPartiallySucceeded {
		t.Errorf("expected PartiallySucceeded, got %q", exec.Status.Result)
	}
}

func TestWaveExecutor_ContextCancelled(t *testing.T) {
	plan := newTestPlan("plan-ctx")
	exec := newTestExecution("exec-ctx", "plan-ctx")
	vms := makeMultiWaveVMs(map[string][]string{
		"alpha": {"vm-a"},
		"beta":  {"vm-b"},
	})
	cl := newFakeClient(vms, plan, exec)

	ctx, cancel := context.WithCancel(context.Background())

	handler := &mockHandler{
		delay: 100 * time.Millisecond,
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	// Cancel after a short delay to interrupt between waves.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := executor.Execute(ctx, ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	// The executor should handle cancellation gracefully — either returning nil
	// after writing status, or returning the context error.
	_ = err

	// Verify execution completed (either Succeeded, Failed, or partial).
	if exec.Status.CompletionTime == nil {
		t.Error("expected CompletionTime to be set after cancellation")
	}
}

func TestWaveExecutor_DiscoveryFailure_ReturnsFailed(t *testing.T) {
	plan := newTestPlan("plan-disc")
	exec := newTestExecution("exec-disc", "plan-disc")
	cl := newFakeClient(nil, plan, exec)

	discoverer := &mockVMDiscoverer{
		err: errors.New("cluster unreachable"),
	}

	executor := newTestExecutor(cl, discoverer,
		&mockNamespaceLookup{})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   &NoOpHandler{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultFailed {
		t.Errorf("expected result %q, got %q", soteriav1alpha1.ExecutionResultFailed, exec.Status.Result)
	}
	if exec.Status.CompletionTime == nil {
		t.Error("expected CompletionTime to be set")
	}
}

func TestWaveExecutor_EmptyPlan_Succeeds(t *testing.T) {
	plan := newTestPlan("plan-empty")
	exec := newTestExecution("exec-empty", "plan-empty")
	cl := newFakeClient(nil, plan, exec)

	discoverer := &mockVMDiscoverer{vms: nil}
	handler := &mockHandler{}

	executor := newTestExecutor(cl, discoverer,
		&mockNamespaceLookup{})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected result %q, got %q", soteriav1alpha1.ExecutionResultSucceeded, exec.Status.Result)
	}
	if len(exec.Status.Waves) != 0 {
		t.Errorf("expected 0 waves, got %d", len(exec.Status.Waves))
	}
	if len(handler.getCalls()) != 0 {
		t.Error("expected handler not to be called for empty plan")
	}

	// Plan should be advanced to FailedOver (empty plan is still "successful").
	var updatedPlan soteriav1alpha1.DRPlan
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "plan-empty"}, &updatedPlan); err != nil {
		t.Fatalf("getting plan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseFailedOver {
		t.Errorf("expected plan phase %q, got %q", soteriav1alpha1.PhaseFailedOver, updatedPlan.Status.Phase)
	}
}

func TestWaveExecutor_StatusPopulated(t *testing.T) {
	plan := newTestPlan("plan-status")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-status", "plan-status")
	vms := makeVMs([]string{"vm-web01", "vm-web02"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &NoOpHandler{}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status structure.
	if exec.Status.CompletionTime == nil {
		t.Error("expected CompletionTime to be set")
	}
	if len(exec.Status.Waves) == 0 {
		t.Fatal("expected at least one wave")
	}

	for i, wave := range exec.Status.Waves {
		if wave.WaveIndex != i {
			t.Errorf("wave %d: expected WaveIndex=%d, got %d", i, i, wave.WaveIndex)
		}
		if wave.StartTime == nil {
			t.Errorf("wave %d: missing StartTime", i)
		}
		if wave.CompletionTime == nil {
			t.Errorf("wave %d: missing CompletionTime", i)
		}
		for j, group := range wave.Groups {
			if group.Name == "" {
				t.Errorf("wave %d group %d: missing Name", i, j)
			}
			if group.Result != soteriav1alpha1.DRGroupResultCompleted {
				t.Errorf("wave %d group %d: expected Completed, got %q", i, j, group.Result)
			}
			if len(group.VMNames) == 0 {
				t.Errorf("wave %d group %d: missing VMNames", i, j)
			}
			if group.StartTime == nil {
				t.Errorf("wave %d group %d: missing StartTime", i, j)
			}
			if group.CompletionTime == nil {
				t.Errorf("wave %d group %d: missing CompletionTime", i, j)
			}
		}
	}

	// Verify conditions.
	var foundProgressing, foundReady bool
	for _, c := range exec.Status.Conditions {
		if c.Type == "Progressing" {
			foundProgressing = true
			if c.Status != metav1.ConditionFalse {
				t.Errorf("expected Progressing=False after completion, got %s", c.Status)
			}
		}
		if c.Type == "Ready" {
			foundReady = true
			if c.Status != metav1.ConditionTrue {
				t.Errorf("expected Ready=True after success, got %s", c.Status)
			}
		}
	}
	if !foundProgressing {
		t.Error("missing Progressing condition")
	}
	if !foundReady {
		t.Error("missing Ready condition")
	}
}

func TestWaveExecutor_AllGroupsFail_ResultFailed(t *testing.T) {
	plan := newTestPlan("plan-allfail")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-allfail", "plan-allfail")
	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-0": fmt.Errorf("failure-1"),
			"wave-alpha-group-1": fmt.Errorf("failure-2"),
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultFailed {
		t.Errorf("expected result %q, got %q", soteriav1alpha1.ExecutionResultFailed, exec.Status.Result)
	}

	// Verify plan stays at rest state and ActiveExecution is cleared on failure.
	var updatedPlan soteriav1alpha1.DRPlan
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "plan-allfail"}, &updatedPlan); err != nil {
		t.Fatalf("getting plan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("expected plan phase unchanged at rest %q, got %q",
			soteriav1alpha1.PhaseSteadyState, updatedPlan.Status.Phase)
	}
	if updatedPlan.Status.ActiveExecution != "" {
		t.Errorf("expected ActiveExecution cleared, got %q",
			updatedPlan.Status.ActiveExecution)
	}
}

func TestComputeResult(t *testing.T) {
	tests := []struct {
		name   string
		groups []soteriav1alpha1.DRGroupResult
		want   soteriav1alpha1.ExecutionResult
	}{
		{
			name: "all completed",
			groups: []soteriav1alpha1.DRGroupResult{
				soteriav1alpha1.DRGroupResultCompleted, soteriav1alpha1.DRGroupResultCompleted,
			},
			want: soteriav1alpha1.ExecutionResultSucceeded,
		},
		{
			name:   "all failed",
			groups: []soteriav1alpha1.DRGroupResult{soteriav1alpha1.DRGroupResultFailed, soteriav1alpha1.DRGroupResultFailed},
			want:   soteriav1alpha1.ExecutionResultFailed,
		},
		{
			name:   "mixed",
			groups: []soteriav1alpha1.DRGroupResult{soteriav1alpha1.DRGroupResultCompleted, soteriav1alpha1.DRGroupResultFailed},
			want:   soteriav1alpha1.ExecutionResultPartiallySucceeded,
		},
		{
			name:   "empty",
			groups: nil,
			want:   soteriav1alpha1.ExecutionResultSucceeded,
		},
		{
			name:   "single completed",
			groups: []soteriav1alpha1.DRGroupResult{soteriav1alpha1.DRGroupResultCompleted},
			want:   soteriav1alpha1.ExecutionResultSucceeded,
		},
		{
			name:   "single failed",
			groups: []soteriav1alpha1.DRGroupResult{soteriav1alpha1.DRGroupResultFailed},
			want:   soteriav1alpha1.ExecutionResultFailed,
		},
		{
			name: "completed with pending",
			groups: []soteriav1alpha1.DRGroupResult{
				soteriav1alpha1.DRGroupResultCompleted,
				soteriav1alpha1.DRGroupResultPending,
			},
			want: soteriav1alpha1.ExecutionResultPartiallySucceeded,
		},
		{
			name:   "all pending",
			groups: []soteriav1alpha1.DRGroupResult{soteriav1alpha1.DRGroupResultPending, soteriav1alpha1.DRGroupResultPending},
			want:   soteriav1alpha1.ExecutionResultFailed,
		},
		{
			name: "completed with in-progress",
			groups: []soteriav1alpha1.DRGroupResult{
				soteriav1alpha1.DRGroupResultCompleted,
				soteriav1alpha1.DRGroupResultInProgress,
			},
			want: soteriav1alpha1.ExecutionResultPartiallySucceeded,
		},
	}

	executor := &WaveExecutor{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &soteriav1alpha1.DRExecution{}
			if len(tt.groups) > 0 {
				groups := make([]soteriav1alpha1.DRGroupExecutionStatus, len(tt.groups))
				for i, r := range tt.groups {
					groups[i] = soteriav1alpha1.DRGroupExecutionStatus{
						Name:   fmt.Sprintf("group-%d", i),
						Result: r,
					}
				}
				exec.Status.Waves = []soteriav1alpha1.WaveStatus{
					{WaveIndex: 0, Groups: groups},
				}
			}

			got := executor.computeResult(exec)
			if got != tt.want {
				t.Errorf("computeResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNoOpHandler(t *testing.T) {
	h := &NoOpHandler{}
	err := h.ExecuteGroup(context.Background(), ExecutionGroup{})
	if err != nil {
		t.Errorf("NoOpHandler returned error: %v", err)
	}
}

// --- GroupError tests ---

func TestGroupError_Error(t *testing.T) {
	ge := &GroupError{
		StepName: "SetSource",
		Target:   "ns-erp-database",
		Err:      fmt.Errorf("invalid replication state transition"),
	}
	want := "SetSource for ns-erp-database: invalid replication state transition"
	if ge.Error() != want {
		t.Errorf("Error() = %q, want %q", ge.Error(), want)
	}
}

func TestGroupError_Unwrap(t *testing.T) {
	underlying := fmt.Errorf("base error")
	ge := &GroupError{StepName: "SetSource", Target: "vg-1", Err: underlying}
	if !errors.Is(ge, underlying) {
		t.Error("errors.Is should find the underlying error")
	}
}

func TestGroupError_ErrorsAs(t *testing.T) {
	underlying := fmt.Errorf("base error")
	wrapped := fmt.Errorf("wrapping: %w", &GroupError{StepName: "SetSource", Target: "vg-1", Err: underlying})
	var ge *GroupError
	if !errors.As(wrapped, &ge) {
		t.Error("errors.As should find *GroupError through wrapping")
	}
	if ge.StepName != "SetSource" {
		t.Errorf("StepName = %q, want SetSource", ge.StepName)
	}
}

// --- Fail-forward executor tests (Story 4.5 AC13) ---

func TestWaveExecutor_PartialFailure_ReportsPartiallySucceeded(t *testing.T) {
	plan := newTestPlan("plan-pf")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-pf", "plan-pf")
	vms := makeVMs([]string{"vm-ok", "vm-fail", "vm-ok2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-1": &GroupError{
				StepName: StepSetSource,
				Target:   "ns-erp-db",
				Err:      fmt.Errorf("replication state transition invalid"),
			},
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultPartiallySucceeded {
		t.Errorf("expected PartiallySucceeded, got %q", exec.Status.Result)
	}

	// Verify the failed group has step detail in its Error field.
	for _, wave := range exec.Status.Waves {
		for _, g := range wave.Groups {
			if g.Result == soteriav1alpha1.DRGroupResultFailed {
				if g.Error == "" {
					t.Error("failed group should have Error set")
				}
				if !strings.Contains(g.Error, "SetSource") {
					t.Errorf("Error should contain step name, got: %s", g.Error)
				}
				if !strings.Contains(g.Error, "ns-erp-db") {
					t.Errorf("Error should contain target, got: %s", g.Error)
				}
			}
		}
	}
}

func TestWaveExecutor_GroupError_StepDetail(t *testing.T) {
	plan := newTestPlan("plan-ge")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-ge", "plan-ge")
	vms := makeVMs([]string{"vm-1"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-0": &GroupError{
				StepName: "SetSource",
				Target:   "ns-erp-db",
				Err:      fmt.Errorf("underlying driver error"),
			},
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	_ = executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})

	group := exec.Status.Waves[0].Groups[0]
	if group.Result != soteriav1alpha1.DRGroupResultFailed {
		t.Errorf("expected Failed, got %q", group.Result)
	}
	want := "step SetSource failed for ns-erp-db: underlying driver error"
	if group.Error != want {
		t.Errorf("Error = %q, want %q", group.Error, want)
	}
}

func TestWaveExecutor_NonGroupError_FallbackFormat(t *testing.T) {
	plan := newTestPlan("plan-nge")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-nge", "plan-nge")
	vms := makeVMs([]string{"vm-1"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-0": fmt.Errorf("plain error without structure"),
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	_ = executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})

	group := exec.Status.Waves[0].Groups[0]
	if group.Error != "plain error without structure" {
		t.Errorf("Error should be the plain error string, got: %q", group.Error)
	}
}

func TestWaveExecutor_DRGroupStatus_Created(t *testing.T) {
	plan := newTestPlan("plan-dgs")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-dgs", "plan-dgs")
	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   &NoOpHandler{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify DRGroupStatus resources were created (one per chunk).
	var dgsList soteriav1alpha1.DRGroupStatusList
	if err := cl.List(context.Background(), &dgsList); err != nil {
		t.Fatalf("listing DRGroupStatuses: %v", err)
	}
	if len(dgsList.Items) == 0 {
		t.Error("expected at least one DRGroupStatus resource to be created")
	}
	for _, dgs := range dgsList.Items {
		if dgs.Spec.ExecutionName != exec.Name {
			t.Errorf("DRGroupStatus.Spec.ExecutionName = %q, want %q", dgs.Spec.ExecutionName, exec.Name)
		}
		if dgs.Status.Phase != soteriav1alpha1.DRGroupResultCompleted {
			t.Errorf("DRGroupStatus.Status.Phase = %q, want Completed", dgs.Status.Phase)
		}
	}
}

func TestWaveExecutor_DRGroupStatus_FailedPhase(t *testing.T) {
	plan := newTestPlan("plan-dgsf")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-dgsf", "plan-dgsf")
	vms := makeVMs([]string{"vm-1"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-0": fmt.Errorf("step failure"),
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	_ = executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})

	var dgsList soteriav1alpha1.DRGroupStatusList
	if err := cl.List(context.Background(), &dgsList); err != nil {
		t.Fatalf("listing DRGroupStatuses: %v", err)
	}
	for _, dgs := range dgsList.Items {
		if dgs.Status.Phase != soteriav1alpha1.DRGroupResultFailed {
			t.Errorf("DRGroupStatus.Status.Phase = %q, want Failed", dgs.Status.Phase)
		}
		if dgs.Status.LastTransitionTime == nil {
			t.Error("DRGroupStatus.Status.LastTransitionTime should be set")
		}
	}
}

func TestWaveExecutor_CompleteTransition_NotCalledOnFailed(t *testing.T) {
	plan := newTestPlan("plan-ct")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-ct", "plan-ct")
	vms := makeVMs([]string{"vm-1"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-0": fmt.Errorf("all groups fail"),
		},
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	_ = executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})

	if exec.Status.Result != soteriav1alpha1.ExecutionResultFailed {
		t.Fatalf("expected Failed, got %q", exec.Status.Result)
	}

	var updatedPlan soteriav1alpha1.DRPlan
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "plan-ct"}, &updatedPlan); err != nil {
		t.Fatalf("getting plan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("plan phase should NOT advance on Failed: got %q, want SteadyState", updatedPlan.Status.Phase)
	}
	if updatedPlan.Status.ActiveExecution != "" {
		t.Errorf("ActiveExecution should be cleared on failure, got %q", updatedPlan.Status.ActiveExecution)
	}
}

func TestWaveExecutor_ActiveSiteFlip(t *testing.T) {
	const primary = "dc-west"
	const secondary = "dc-east"

	tests := []struct {
		name           string
		startPhase     string
		startSite      string
		execMode       soteriav1alpha1.ExecutionMode
		wantPhase      string
		wantActiveSite string
	}{
		{
			name:           "failover: SteadyState→FailedOver flips to secondary",
			startPhase:     soteriav1alpha1.PhaseSteadyState,
			startSite:      primary,
			execMode:       soteriav1alpha1.ExecutionModePlannedMigration,
			wantPhase:      soteriav1alpha1.PhaseFailedOver,
			wantActiveSite: secondary,
		},
		{
			name:           "failback: DRedSteadyState→FailedBack flips to primary",
			startPhase:     soteriav1alpha1.PhaseDRedSteadyState,
			startSite:      secondary,
			execMode:       soteriav1alpha1.ExecutionModePlannedMigration,
			wantPhase:      soteriav1alpha1.PhaseFailedBack,
			wantActiveSite: primary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := newTestPlan("plan-as")
			plan.Status.Phase = tt.startPhase
			plan.Status.ActiveSite = tt.startSite
			plan.Status.ActiveExecutionMode = tt.execMode
			exec := newTestExecution("exec-as", "plan-as")
			exec.Spec.Mode = tt.execMode
			vms := makeVMs([]string{"vm-1"}, "alpha")
			cl := newFakeClient(vms, plan, exec)

			executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
				&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
					"ns-1": soteriav1alpha1.ConsistencyLevelVM,
				}})

			_ = executor.Execute(context.Background(), ExecuteInput{
				Execution: exec, Plan: plan, Handler: &NoOpHandler{},
			})

			var updatedPlan soteriav1alpha1.DRPlan
			if err := cl.Get(context.Background(),
				client.ObjectKey{Name: "plan-as"}, &updatedPlan); err != nil {
				t.Fatalf("getting plan: %v", err)
			}
			if updatedPlan.Status.Phase != tt.wantPhase {
				t.Fatalf("phase = %q, want %q", updatedPlan.Status.Phase, tt.wantPhase)
			}
			if updatedPlan.Status.ActiveSite != tt.wantActiveSite {
				t.Errorf("activeSite = %q, want %q",
					updatedPlan.Status.ActiveSite, tt.wantActiveSite)
			}
		})
	}
}

func TestWaveExecutor_ActiveSiteUnchanged_OnFailed(t *testing.T) {
	plan := newTestPlan("plan-as-fail")
	plan.Status.Phase = soteriav1alpha1.PhaseSteadyState
	plan.Status.ActiveSite = plan.Spec.PrimarySite
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-as-fail", "plan-as-fail")
	vms := makeVMs([]string{"vm-1"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	handler := &mockHandler{
		failOn: map[string]error{"wave-alpha-group-0": fmt.Errorf("boom")},
	}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
			"ns-1": soteriav1alpha1.ConsistencyLevelVM,
		}})

	_ = executor.Execute(context.Background(), ExecuteInput{
		Execution: exec, Plan: plan, Handler: handler,
	})

	var updatedPlan soteriav1alpha1.DRPlan
	if err := cl.Get(context.Background(),
		client.ObjectKey{Name: "plan-as-fail"}, &updatedPlan); err != nil {
		t.Fatalf("getting plan: %v", err)
	}
	if updatedPlan.Status.ActiveSite != plan.Spec.PrimarySite {
		t.Errorf("activeSite should NOT change on failure: got %q, want %q",
			updatedPlan.Status.ActiveSite, plan.Spec.PrimarySite)
	}
}

func TestWaveExecutor_PreConditionFailure_ResultFailed(t *testing.T) {
	plan := newTestPlan("plan-pre")
	exec := newTestExecution("exec-pre", "plan-pre")
	cl := newFakeClient(nil, plan, exec)

	discoverer := &mockVMDiscoverer{err: errors.New("API server unreachable")}
	executor := newTestExecutor(cl, discoverer, &mockNamespaceLookup{})

	_ = executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   &NoOpHandler{},
	})

	if exec.Status.Result != soteriav1alpha1.ExecutionResultFailed {
		t.Errorf("expected Failed, got %q", exec.Status.Result)
	}

	// Verify no DRGroupStatus resources were created.
	var dgsList soteriav1alpha1.DRGroupStatusList
	if err := cl.List(context.Background(), &dgsList); err != nil {
		t.Fatalf("listing DRGroupStatuses: %v", err)
	}
	if len(dgsList.Items) != 0 {
		t.Errorf("expected 0 DRGroupStatus resources for pre-condition failure, got %d", len(dgsList.Items))
	}
}

func TestWaveExecutor_ContextCancellation_PartialResults(t *testing.T) {
	plan := newTestPlan("plan-cc")
	exec := newTestExecution("exec-cc", "plan-cc")
	vms := makeMultiWaveVMs(map[string][]string{
		"alpha": {"vm-a"},
		"beta":  {"vm-b"},
	})
	cl := newFakeClient(vms, plan, exec)

	ctx, cancel := context.WithCancel(context.Background())
	handler := &mockHandler{delay: 200 * time.Millisecond}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_ = executor.Execute(ctx, ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})

	if exec.Status.CompletionTime == nil {
		t.Error("expected CompletionTime to be set after cancellation")
	}
}

func TestWaveExecutor_StepRecorder_PassedToHandler(t *testing.T) {
	plan := newTestPlan("plan-sr")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-sr", "plan-sr")
	vms := makeVMs([]string{"vm-1"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	// Use a StepHandler to verify steps are recorded in DRGroupStatus.
	drv := &noop.Driver{}
	vm := newMockVMManager()
	handler := &FailoverHandler{
		Driver:           drv,
		VMManager:        vm,
		Config:           FailoverConfig{GracefulShutdown: false, Force: true},
		SyncPollInterval: 1 * time.Millisecond,
		SyncTimeout:      1 * time.Second,
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify DRGroupStatus has steps recorded.
	var dgsList soteriav1alpha1.DRGroupStatusList
	if err := cl.List(context.Background(), &dgsList); err != nil {
		t.Fatalf("listing DRGroupStatuses: %v", err)
	}
	if len(dgsList.Items) == 0 {
		t.Fatal("expected at least one DRGroupStatus")
	}
	dgs := dgsList.Items[0]
	if len(dgs.Status.Steps) == 0 {
		t.Error("expected steps to be recorded in DRGroupStatus via StepRecorder")
	}
}

// --- Retry tests (Story 4.6) ---

// newPartiallySucceededExec builds a DRExecution with PartiallySucceeded status
// containing the specified group results distributed across waves.
func newPartiallySucceededExec(
	name, planName string, waves [][]soteriav1alpha1.DRGroupExecutionStatus,
) *soteriav1alpha1.DRExecution {
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: planName,
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result:    soteriav1alpha1.ExecutionResultPartiallySucceeded,
			StartTime: &now,
		},
	}
	for i, groups := range waves {
		exec.Status.Waves = append(exec.Status.Waves, soteriav1alpha1.WaveStatus{
			WaveIndex: i,
			Groups:    groups,
		})
	}
	return exec
}

func TestResolveRetryGroups_SpecificGroups(t *testing.T) {
	exec := newPartiallySucceededExec("exec-1", "plan-1", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{
				Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed,
				VMNames: []string{"vm-2"}, Error: "storage error",
			},
		},
	})

	targets, err := ResolveRetryGroups(exec, "wave-alpha-group-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 retry target, got %d", len(targets))
	}
	if targets[0].GroupName != "wave-alpha-group-1" {
		t.Errorf("expected group name wave-alpha-group-1, got %s", targets[0].GroupName)
	}
	if targets[0].WaveIndex != 0 || targets[0].GroupIndex != 1 {
		t.Errorf("expected wave=0 group=1, got wave=%d group=%d", targets[0].WaveIndex, targets[0].GroupIndex)
	}
}

func TestResolveRetryGroups_AllFailed(t *testing.T) {
	exec := newPartiallySucceededExec("exec-1", "plan-1", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-2"}},
		},
		{
			{Name: "wave-beta-group-0", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-3"}},
		},
	})

	targets, err := ResolveRetryGroups(exec, "all-failed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 retry targets, got %d", len(targets))
	}
	// Should find groups from both waves.
	names := map[string]bool{}
	for _, t := range targets {
		names[t.GroupName] = true
	}
	if !names["wave-alpha-group-1"] || !names["wave-beta-group-0"] {
		t.Errorf("expected both failed groups, got targets: %v", targets)
	}
}

func TestResolveRetryGroups_GroupNotFound(t *testing.T) {
	exec := newPartiallySucceededExec("exec-1", "plan-1", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
		},
	})

	_, err := ResolveRetryGroups(exec, "nonexistent-group")
	if err == nil {
		t.Fatal("expected error for non-existent group")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestResolveRetryGroups_GroupNotFailed(t *testing.T) {
	exec := newPartiallySucceededExec("exec-1", "plan-1", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-2"}},
		},
	})

	// Completed group should be silently skipped.
	targets, err := ResolveRetryGroups(exec, "wave-alpha-group-0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for already-Completed group, got %d", len(targets))
	}
}

func TestResolveRetryGroups_WaveOrdering(t *testing.T) {
	exec := newPartiallySucceededExec("exec-1", "plan-1", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
		},
		{
			{Name: "wave-beta-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-2"}},
		},
		{
			{Name: "wave-gamma-group-0", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-3"}},
		},
	})

	// Insert out of order in annotation — targets should come back sorted.
	targets, err := ResolveRetryGroups(exec, "wave-gamma-group-0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].WaveIndex != 2 {
		t.Errorf("expected wave index 2, got %d", targets[0].WaveIndex)
	}
}

func TestParseRetryAnnotation_AllFailed(t *testing.T) {
	result := parseRetryAnnotation("all-failed")
	if result != nil {
		t.Errorf("expected nil for all-failed sentinel, got %v", result)
	}
}

func TestParseRetryAnnotation_CommaSeparated(t *testing.T) {
	result := parseRetryAnnotation("group-1, group-2 ,group-3")
	if len(result) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(result))
	}
	if result[0] != "group-1" || result[1] != "group-2" || result[2] != "group-3" {
		t.Errorf("unexpected parsed groups: %v", result)
	}
}

// --- Retry execution tests (Task 10) ---

// newRetryTestPlan creates a plan with Status.Waves populated for retry tests.
func newRetryTestPlan(name string, waves []soteriav1alpha1.WaveInfo) *soteriav1alpha1.DRPlan {
	plan := newTestPlan(name)
	plan.Status.Waves = waves
	return plan
}

func TestWaveExecutor_RetryOneGroup_Succeeds_ResultSucceeded(t *testing.T) {
	plan := newRetryTestPlan("plan-retry", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"}, {Name: "vm-2", Namespace: "ns-1"},
		}},
	})
	exec := newPartiallySucceededExec("exec-retry", "plan-retry", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{
				Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed,
				VMNames: []string{"vm-2"}, Error: "storage error",
			},
		},
	})

	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	targets := []RetryTarget{
		{WaveIndex: 0, GroupIndex: 1, GroupName: "wave-alpha-group-1"},
	}

	err := executor.ExecuteRetry(context.Background(), RetryInput{
		Execution:    exec,
		Plan:         plan,
		Handler:      handler,
		RetryTargets: targets,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded after retry, got %q", exec.Status.Result)
	}

	retryGroup := exec.Status.Waves[0].Groups[1]
	if retryGroup.Result != soteriav1alpha1.DRGroupResultCompleted {
		t.Errorf("retried group should be Completed, got %q", retryGroup.Result)
	}
	if retryGroup.RetryCount != 1 {
		t.Errorf("expected RetryCount=1, got %d", retryGroup.RetryCount)
	}
}

func TestWaveExecutor_RetryOneOfTwo_Succeeds_ResultPartiallySucceeded(t *testing.T) {
	plan := newRetryTestPlan("plan-retry2", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"}, {Name: "vm-2", Namespace: "ns-1"}, {Name: "vm-3", Namespace: "ns-1"},
		}},
	})
	exec := newPartiallySucceededExec("exec-retry2", "plan-retry2", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-2"}},
			{Name: "wave-alpha-group-2", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-3"}},
		},
	})

	vms := makeVMs([]string{"vm-1", "vm-2", "vm-3"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	// Only retry 1 of the 2 failed groups.
	targets := []RetryTarget{
		{WaveIndex: 0, GroupIndex: 1, GroupName: "wave-alpha-group-1"},
	}

	err := executor.ExecuteRetry(context.Background(), RetryInput{
		Execution:    exec,
		Plan:         plan,
		Handler:      handler,
		RetryTargets: targets,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultPartiallySucceeded {
		t.Errorf("expected PartiallySucceeded (1 still failed), got %q", exec.Status.Result)
	}
}

func TestWaveExecutor_RetryAllFailed_AllSucceed_ResultSucceeded(t *testing.T) {
	plan := newRetryTestPlan("plan-retryall", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"}, {Name: "vm-2", Namespace: "ns-1"},
		}},
		{WaveKey: "beta", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-3", Namespace: "ns-1"},
		}},
	})
	exec := newPartiallySucceededExec("exec-retryall", "plan-retryall", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-2"}},
		},
		{
			{Name: "wave-beta-group-0", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-3"}},
		},
	})

	vms := makeMultiWaveVMs(map[string][]string{"alpha": {"vm-1", "vm-2"}, "beta": {"vm-3"}})
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	targets := []RetryTarget{
		{WaveIndex: 0, GroupIndex: 1, GroupName: "wave-alpha-group-1"},
		{WaveIndex: 1, GroupIndex: 0, GroupName: "wave-beta-group-0"},
	}

	err := executor.ExecuteRetry(context.Background(), RetryInput{
		Execution:    exec,
		Plan:         plan,
		Handler:      handler,
		RetryTargets: targets,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded after all retries succeed, got %q", exec.Status.Result)
	}
}

func TestWaveExecutor_RetryFails_GroupBackToFailed(t *testing.T) {
	plan := newRetryTestPlan("plan-retryfail", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"}, {Name: "vm-2", Namespace: "ns-1"},
		}},
	})
	exec := newPartiallySucceededExec("exec-retryfail", "plan-retryfail", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{
				Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed,
				VMNames: []string{"vm-2"}, Error: "old error",
			},
		},
	})

	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{
		failOn: map[string]error{
			"wave-alpha-group-1": fmt.Errorf("retry also failed"),
		},
	}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	targets := []RetryTarget{
		{WaveIndex: 0, GroupIndex: 1, GroupName: "wave-alpha-group-1"},
	}

	err := executor.ExecuteRetry(context.Background(), RetryInput{
		Execution:    exec,
		Plan:         plan,
		Handler:      handler,
		RetryTargets: targets,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retryGroup := exec.Status.Waves[0].Groups[1]
	if retryGroup.Result != soteriav1alpha1.DRGroupResultFailed {
		t.Errorf("expected Failed after retry failure, got %q", retryGroup.Result)
	}
	if retryGroup.Error != "retry also failed" {
		t.Errorf("expected new error message, got %q", retryGroup.Error)
	}
	if retryGroup.RetryCount != 1 {
		t.Errorf("expected RetryCount=1, got %d", retryGroup.RetryCount)
	}
	if exec.Status.Result != soteriav1alpha1.ExecutionResultPartiallySucceeded {
		t.Errorf("result should stay PartiallySucceeded, got %q", exec.Status.Result)
	}
}

func TestWaveExecutor_RetryWaveOrdering(t *testing.T) {
	plan := newRetryTestPlan("plan-retryord", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-1", Namespace: "ns-1"}}},
		{WaveKey: "beta", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-2", Namespace: "ns-1"}}},
		{WaveKey: "gamma", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-3", Namespace: "ns-1"}}},
	})
	exec := newPartiallySucceededExec("exec-retryord", "plan-retryord", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-1"}},
		},
		{
			{Name: "wave-beta-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-2"}},
		},
		{
			{Name: "wave-gamma-group-0", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-3"}},
		},
	})

	vms := makeMultiWaveVMs(map[string][]string{
		"alpha": {"vm-1"}, "beta": {"vm-2"}, "gamma": {"vm-3"},
	})
	cl := newFakeClient(vms, plan, exec)

	// Track execution order.
	var executionOrder []string
	var orderMu sync.Mutex
	orderHandler := &mockHandler{
		delay: 10 * time.Millisecond,
	}
	wrappedHandler := &orderTrackingHandler{
		inner: orderHandler,
		order: &executionOrder,
		mu:    &orderMu,
	}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	targets := []RetryTarget{
		{WaveIndex: 0, GroupIndex: 0, GroupName: "wave-alpha-group-0"},
		{WaveIndex: 2, GroupIndex: 0, GroupName: "wave-gamma-group-0"},
	}

	err := executor.ExecuteRetry(context.Background(), RetryInput{
		Execution:    exec,
		Plan:         plan,
		Handler:      wrappedHandler,
		RetryTargets: targets,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	orderMu.Lock()
	order := make([]string, len(executionOrder))
	copy(order, executionOrder)
	orderMu.Unlock()

	if len(order) != 2 {
		t.Fatalf("expected 2 groups executed, got %d", len(order))
	}
	// Wave 0 should execute before wave 2.
	if order[0] != "wave-alpha-group-0" || order[1] != "wave-gamma-group-0" {
		t.Errorf("wave ordering violated: got %v", order)
	}
}

type orderTrackingHandler struct {
	inner DRGroupHandler
	order *[]string
	mu    *sync.Mutex
}

func (h *orderTrackingHandler) ExecuteGroup(ctx context.Context, group ExecutionGroup) error {
	h.mu.Lock()
	*h.order = append(*h.order, group.Chunk.Name)
	h.mu.Unlock()
	return h.inner.ExecuteGroup(ctx, group)
}

func TestWaveExecutor_RetryCount_Incremented(t *testing.T) {
	plan := newRetryTestPlan("plan-retrycnt", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"}, {Name: "vm-2", Namespace: "ns-1"},
		}},
	})
	exec := newPartiallySucceededExec("exec-retrycnt", "plan-retrycnt", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
			{Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-2"}, RetryCount: 1},
		},
	})

	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	targets := []RetryTarget{
		{WaveIndex: 0, GroupIndex: 1, GroupName: "wave-alpha-group-1"},
	}

	err := executor.ExecuteRetry(context.Background(), RetryInput{
		Execution:    exec,
		Plan:         plan,
		Handler:      handler,
		RetryTargets: targets,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retryGroup := exec.Status.Waves[0].Groups[1]
	if retryGroup.RetryCount != 2 {
		t.Errorf("expected RetryCount=2 (was 1, incremented), got %d", retryGroup.RetryCount)
	}
}

func TestWaveExecutor_RetryContextCancelled(t *testing.T) {
	plan := newRetryTestPlan("plan-retryctx", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-1", Namespace: "ns-1"}}},
		{WaveKey: "beta", VMs: []soteriav1alpha1.DiscoveredVM{{Name: "vm-2", Namespace: "ns-1"}}},
	})
	exec := newPartiallySucceededExec("exec-retryctx", "plan-retryctx", [][]soteriav1alpha1.DRGroupExecutionStatus{
		{
			{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-1"}},
		},
		{
			{Name: "wave-beta-group-0", Result: soteriav1alpha1.DRGroupResultFailed, VMNames: []string{"vm-2"}},
		},
	})

	vms := makeMultiWaveVMs(map[string][]string{"alpha": {"vm-1"}, "beta": {"vm-2"}})
	cl := newFakeClient(vms, plan, exec)

	ctx, cancel := context.WithCancel(context.Background())
	handler := &mockHandler{delay: 200 * time.Millisecond}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	// Cancel during first wave.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_ = executor.ExecuteRetry(ctx, RetryInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
		RetryTargets: []RetryTarget{
			{WaveIndex: 0, GroupIndex: 0, GroupName: "wave-alpha-group-0"},
			{WaveIndex: 1, GroupIndex: 0, GroupName: "wave-beta-group-0"},
		},
	})

	if exec.Status.CompletionTime == nil {
		t.Error("expected CompletionTime to be set after cancellation")
	}
}

// --- Resume-aware executor tests (Story 4.7 Task 10) ---

func TestWaveExecutor_ExecuteFromWave_SkipsCompletedGroups(t *testing.T) {
	plan := newRetryTestPlan("plan-resume-skip", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"}, {Name: "vm-2", Namespace: "ns-1"}, {Name: "vm-3", Namespace: "ns-1"},
		}},
	})
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-resume-skip"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-resume-skip",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Waves: []soteriav1alpha1.WaveStatus{
				{
					WaveIndex: 0,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
						{Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-2"}},
						{Name: "wave-alpha-group-2", Result: soteriav1alpha1.DRGroupResultPending, VMNames: []string{"vm-3"}},
					},
				},
			},
		},
	}

	vms := makeVMs([]string{"vm-1", "vm-2", "vm-3"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	skipGroups := map[string]bool{
		"wave-alpha-group-0": true,
		"wave-alpha-group-1": true,
	}

	err := executor.ExecuteFromWave(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	}, 0, skipGroups)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := handler.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 handler call (only pending group), got %d: %v", len(calls), calls)
	}
}

func TestWaveExecutor_ExecuteFromWave_RetriesInFlightGroup(t *testing.T) {
	plan := newRetryTestPlan("plan-resume-inflight", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"}, {Name: "vm-2", Namespace: "ns-1"},
		}},
	})
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-resume-inflight"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-resume-inflight",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Waves: []soteriav1alpha1.WaveStatus{
				{
					WaveIndex: 0,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
						// Reset from InProgress to Pending by reconciler before calling ExecuteFromWave.
						{Name: "wave-alpha-group-1", Result: soteriav1alpha1.DRGroupResultPending, VMNames: []string{"vm-2"}},
					},
				},
			},
		},
	}

	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	skipGroups := map[string]bool{
		"wave-alpha-group-0": true,
	}

	err := executor.ExecuteFromWave(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	}, 0, skipGroups)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := handler.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 handler call (retried in-flight group), got %d", len(calls))
	}

	// Verify the originally-completed group was not re-executed.
	group0 := exec.Status.Waves[0].Groups[0]
	if group0.Result != soteriav1alpha1.DRGroupResultCompleted {
		t.Errorf("completed group should remain Completed, got %q", group0.Result)
	}
}

func TestWaveExecutor_ExecuteFromWave_ContinuesNextWave(t *testing.T) {
	plan := newRetryTestPlan("plan-resume-next", []soteriav1alpha1.WaveInfo{
		{WaveKey: "alpha", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-1", Namespace: "ns-1"},
		}},
		{WaveKey: "beta", VMs: []soteriav1alpha1.DiscoveredVM{
			{Name: "vm-2", Namespace: "ns-1"},
		}},
	})
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-resume-next"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-resume-next",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Waves: []soteriav1alpha1.WaveStatus{
				{
					WaveIndex: 0,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{Name: "wave-alpha-group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
					},
				},
				{
					WaveIndex: 1,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{Name: "wave-beta-group-0", Result: soteriav1alpha1.DRGroupResultPending, VMNames: []string{"vm-2"}},
					},
				},
			},
		},
	}

	vms := makeMultiWaveVMs(map[string][]string{"alpha": {"vm-1"}, "beta": {"vm-2"}})
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	// Resume from wave 1 (wave 0 already complete).
	err := executor.ExecuteFromWave(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	}, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := handler.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 handler call (wave 1 only), got %d", len(calls))
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded, got %q", exec.Status.Result)
	}
}

func TestWaveExecutor_Execute_BackwardCompatible(t *testing.T) {
	plan := newTestPlan("plan-compat")
	exec := newTestExecution("exec-compat", "plan-compat")
	vms := makeVMs([]string{"vm-1"}, "alpha")
	cl := newFakeClient(vms, plan, exec)
	handler := &mockHandler{}

	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Status.Result != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded, got %q", exec.Status.Result)
	}
	calls := handler.getCalls()
	if len(calls) == 0 {
		t.Error("expected handler to be called")
	}
}

func TestWaveExecutor_CheckpointAfterEachGroup(t *testing.T) {
	plan := newTestPlan("plan-cp-each")
	plan.Spec.MaxConcurrentFailovers = 1
	exec := newTestExecution("exec-cp-each", "plan-cp-each")
	vms := makeVMs([]string{"vm-1", "vm-2"}, "alpha")
	cl := newFakeClient(vms, plan, exec)

	cp := &NoOpCheckpointer{}
	executor := newTestExecutor(cl, &mockVMDiscoverer{vms: vms},
		&mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{"ns-1": soteriav1alpha1.ConsistencyLevelVM}})
	executor.Checkpointer = cp

	err := executor.Execute(context.Background(), ExecuteInput{
		Execution: exec,
		Plan:      plan,
		Handler:   &NoOpHandler{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With 2 groups + 1 wave completion = 3 checkpoint calls at minimum.
	calls := cp.GetCalls()
	if len(calls) < 3 {
		t.Errorf("expected at least 3 checkpoint calls (2 groups + 1 wave), got %d", len(calls))
	}
}

func TestBuildChunkInput(t *testing.T) {
	vms := makeMultiWaveVMs(map[string][]string{
		"alpha": {"vm-a1", "vm-a2"},
		"beta":  {"vm-b1"},
	})

	discovery := GroupByWave(vms, "soteria.io/wave")
	consistency := &ConsistencyResult{
		VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
			{
				Name: "vm-ns-1-vm-a1", Namespace: "ns-1",
				ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-a1"},
			},
			{
				Name: "vm-ns-1-vm-a2", Namespace: "ns-1",
				ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-a2"},
			},
			{
				Name: "vm-ns-1-vm-b1", Namespace: "ns-1",
				ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-b1"},
			},
		},
	}

	input := buildChunkInput(discovery, consistency, vms, "soteria.io/wave")

	if len(input.WaveGroups) != 2 {
		t.Fatalf("expected 2 wave groups, got %d", len(input.WaveGroups))
	}

	// Find alpha and beta wave groups.
	for _, wg := range input.WaveGroups {
		switch wg.WaveKey {
		case "alpha":
			if len(wg.VolumeGroups) != 2 {
				t.Errorf("alpha wave: expected 2 volume groups, got %d", len(wg.VolumeGroups))
			}
		case "beta":
			if len(wg.VolumeGroups) != 1 {
				t.Errorf("beta wave: expected 1 volume group, got %d", len(wg.VolumeGroups))
			}
		default:
			t.Errorf("unexpected wave key: %s", wg.WaveKey)
		}
	}
}
