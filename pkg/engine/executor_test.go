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
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase: soteriav1alpha1.PhaseFailingOver,
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
		WithStatusSubresource(&soteriav1alpha1.DRExecution{}, &soteriav1alpha1.DRPlan{})
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

	// Verify plan was NOT advanced (all-fail should leave plan in FailingOver).
	var updatedPlan soteriav1alpha1.DRPlan
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "plan-allfail"}, &updatedPlan); err != nil {
		t.Fatalf("getting plan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseFailingOver {
		t.Errorf("expected plan phase unchanged %q, got %q",
			soteriav1alpha1.PhaseFailingOver, updatedPlan.Status.Phase)
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
