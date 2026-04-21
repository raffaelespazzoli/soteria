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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/fake"
)

func newReprotectInput(vgs []VolumeGroupEntry) ReprotectInput {
	return ReprotectInput{
		Execution: &soteriav1alpha1.DRExecution{
			ObjectMeta: metav1.ObjectMeta{Name: "exec-reprotect"},
			Spec: soteriav1alpha1.DRExecutionSpec{
				PlanName: "plan-1",
				Mode:     soteriav1alpha1.ExecutionModeReprotect,
			},
		},
		Plan: &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-1"},
			Status: soteriav1alpha1.DRPlanStatus{
				Phase: soteriav1alpha1.PhaseReprotecting,
			},
		},
		VolumeGroups: vgs,
	}
}

func makeVGEntry(name string, drv drivers.StorageProvider, vgID drivers.VolumeGroupID) VolumeGroupEntry {
	return VolumeGroupEntry{
		Info: soteriav1alpha1.VolumeGroupInfo{
			Name:      name,
			Namespace: "default",
			VMNames:   []string{"vm-1"},
		},
		Driver: drv,
		VGID:   vgID,
	}
}

func TestReprotect_FullSuccess(t *testing.T) {
	d := fake.New()
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})
	d.OnGetReplicationStatus("vg-2").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{
		makeVGEntry("vg-1", d, "vg-1"),
		makeVGEntry("vg-2", d, "vg-2"),
	}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded, got %s", result.Result())
	}
	if result.SetupSucceeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", result.SetupSucceeded)
	}
	if result.SetupFailed != 0 {
		t.Errorf("expected 0 failed, got %d", result.SetupFailed)
	}
	if result.HealthyVGs != 2 {
		t.Errorf("expected 2 healthy, got %d", result.HealthyVGs)
	}
	if result.TimedOut {
		t.Error("expected no timeout")
	}

	if !d.Called("StopReplication") {
		t.Error("expected StopReplication to be called")
	}
	if !d.Called("SetSource") {
		t.Error("expected SetSource to be called")
	}
	if d.CallCount("StopReplication") != 2 {
		t.Errorf("expected 2 StopReplication calls, got %d", d.CallCount("StopReplication"))
	}
	if d.CallCount("SetSource") != 2 {
		t.Errorf("expected 2 SetSource calls, got %d", d.CallCount("SetSource"))
	}
}

func TestReprotect_StopReplicationFails_Tolerated(t *testing.T) {
	d := fake.New()
	d.OnStopReplication("vg-1").Return(errors.New("site unreachable"))
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded despite StopReplication failure, got %s", result.Result())
	}
	if result.SetupSucceeded != 1 {
		t.Errorf("expected 1 succeeded, got %d", result.SetupSucceeded)
	}

	// Verify StopReplication was called and SetSource proceeded.
	if d.CallCount("StopReplication") != 1 {
		t.Errorf("expected 1 StopReplication call, got %d", d.CallCount("StopReplication"))
	}
	if d.CallCount("SetSource") != 1 {
		t.Errorf("expected 1 SetSource call, got %d", d.CallCount("SetSource"))
	}

	// Verify a warning step was recorded for StopReplication.
	var foundWarning bool
	for _, step := range result.Steps {
		if step.Name == StepReprotectStopReplication && step.Status == reprotectStatusWarning {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected a Warning step for StopReplication failure")
	}
}

func TestReprotect_SetSourceFails_VGMarkedFailed(t *testing.T) {
	d1 := fake.New()
	d1.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	d2 := fake.New()
	d2.OnSetSource("vg-2").Return(errors.New("source setup failed"))

	d3 := fake.New()
	d3.OnGetReplicationStatus("vg-3").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{
		makeVGEntry("vg-1", d1, "vg-1"),
		makeVGEntry("vg-2", d2, "vg-2"),
		makeVGEntry("vg-3", d3, "vg-3"),
	}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SetupSucceeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", result.SetupSucceeded)
	}
	if result.SetupFailed != 1 {
		t.Errorf("expected 1 failed, got %d", result.SetupFailed)
	}
	if len(result.FailedVGs) != 1 || result.FailedVGs[0] != "vg-2" {
		t.Errorf("expected FailedVGs=[vg-2], got %v", result.FailedVGs)
	}
	if got := result.Result(); got != soteriav1alpha1.ExecutionResultPartiallySucceeded {
		t.Errorf("Result() = %s, want PartiallySucceeded (mixed setup failure)", got)
	}
}

func TestReprotect_AllSetSourceFail_ExecutionFails(t *testing.T) {
	d := fake.New()
	d.OnSetSource().Return(errors.New("source failed"))
	d.OnSetSource().Return(errors.New("source failed"))

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{
		makeVGEntry("vg-1", d, "vg-1"),
		makeVGEntry("vg-2", d, "vg-2"),
	}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err == nil {
		t.Fatal("expected error when all SetSource fail")
	}
	if result.Result() != soteriav1alpha1.ExecutionResultFailed {
		t.Errorf("expected Failed, got %s", result.Result())
	}
	if result.SetupSucceeded != 0 {
		t.Errorf("expected 0 succeeded, got %d", result.SetupSucceeded)
	}
	if result.SetupFailed != 2 {
		t.Errorf("expected 2 failed, got %d", result.SetupFailed)
	}
}

func TestReprotect_HealthMonitoringTimeout(t *testing.T) {
	d := fake.New()
	// Always return Syncing — never Healthy.
	for range 100 {
		d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
			ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthSyncing},
		})
	}

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 5 * time.Millisecond,
		HealthTimeout:      50 * time.Millisecond,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error (timeout should not return error): %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultPartiallySucceeded {
		t.Errorf("expected PartiallySucceeded on timeout, got %s", result.Result())
	}
	if !result.TimedOut {
		t.Error("expected TimedOut=true")
	}
	if result.SetupSucceeded != 1 {
		t.Errorf("expected 1 setup succeeded, got %d", result.SetupSucceeded)
	}
}

func TestReprotect_HealthMonitoringCompletes(t *testing.T) {
	d := fake.New()
	// First poll: Syncing, second poll: Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthSyncing},
	})
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded, got %s", result.Result())
	}
	if result.HealthyVGs != 1 {
		t.Errorf("expected 1 healthy, got %d", result.HealthyVGs)
	}
	if result.TimedOut {
		t.Error("expected no timeout")
	}

	// Should have polled at least twice (Syncing then Healthy).
	if d.CallCount("GetReplicationStatus") < 2 {
		t.Errorf("expected at least 2 GetReplicationStatus calls, got %d",
			d.CallCount("GetReplicationStatus"))
	}
}

func TestReprotect_ResumeFromHealthMonitoring(t *testing.T) {
	// Simulate a resume scenario: role setup already done, VGs are already
	// Source. StopReplication and SetSource are idempotent — calling them
	// again succeeds. The handler should proceed to health monitoring.
	d := fake.New()
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded on resume, got %s", result.Result())
	}
}

func TestReprotect_CheckpointWrittenPerPoll(t *testing.T) {
	d := fake.New()
	// Two polls: first Syncing, then Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthSyncing},
	})
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded, got %s", result.Result())
	}

	// Checkpoints: 1 after role setup + 2 during health monitoring (one per poll).
	calls := cp.GetCalls()
	if len(calls) < 3 {
		t.Errorf("expected at least 3 checkpoint writes (1 role setup + 2 health polls), got %d", len(calls))
	}
}

func TestReprotect_ContextCancelled(t *testing.T) {
	d := fake.New()
	for range 100 {
		d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
			ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthSyncing},
		})
	}

	h := &ReprotectHandler{
		HealthPollInterval: 5 * time.Millisecond,
		HealthTimeout:      10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	_, err := h.Execute(ctx, newReprotectInput(vgs))
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestReprotect_StepStatusRecorded(t *testing.T) {
	d := fake.New()
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	h := &ReprotectHandler{
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	result, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: StopReplication(Succeeded), SetSource(Succeeded), HealthMonitoring(Succeeded).
	if len(result.Steps) < 3 {
		t.Fatalf("expected at least 3 steps, got %d", len(result.Steps))
	}

	stepNames := make(map[string]string)
	for _, s := range result.Steps {
		stepNames[s.Name] = s.Status
	}

	if stepNames[StepReprotectStopReplication] != reprotectStatusSucceeded {
		t.Errorf("expected StopReplication Succeeded, got %q", stepNames[StepReprotectStopReplication])
	}
	if stepNames[StepReprotectSetSource] != reprotectStatusSucceeded {
		t.Errorf("expected SetSource Succeeded, got %q", stepNames[StepReprotectSetSource])
	}
	if stepNames[StepReprotectHealthMonitoring] != reprotectStatusSucceeded {
		t.Errorf("expected HealthMonitoring Succeeded, got %q", stepNames[StepReprotectHealthMonitoring])
	}

	for _, s := range result.Steps {
		if s.Timestamp == nil {
			t.Errorf("step %s has nil Timestamp", s.Name)
		}
	}
}

func TestReprotect_EmptyVolumeGroups(t *testing.T) {
	h := &ReprotectHandler{
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	result, err := h.Execute(context.Background(), newReprotectInput(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded for empty VGs, got %s", result.Result())
	}
	if result.TotalVGs != 0 {
		t.Errorf("expected 0 total VGs, got %d", result.TotalVGs)
	}
}

func TestReprotect_ForceFlags(t *testing.T) {
	d := fake.New()
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	h := &ReprotectHandler{
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}

	_, err := h.Execute(context.Background(), newReprotectInput(vgs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify StopReplication used Force=true.
	stopCalls := d.CallsTo("StopReplication")
	if len(stopCalls) != 1 {
		t.Fatalf("expected 1 StopReplication call, got %d", len(stopCalls))
	}
	stopOpts, ok := stopCalls[0].Args[1].(drivers.StopReplicationOptions)
	if !ok {
		t.Fatal("StopReplication args[1] is not StopReplicationOptions")
	}
	if !stopOpts.Force {
		t.Error("expected StopReplication Force=true")
	}

	// Verify SetSource used Force=false.
	srcCalls := d.CallsTo("SetSource")
	if len(srcCalls) != 1 {
		t.Fatalf("expected 1 SetSource call, got %d", len(srcCalls))
	}
	srcOpts, ok := srcCalls[0].Args[1].(drivers.SetSourceOptions)
	if !ok {
		t.Fatal("SetSource args[1] is not SetSourceOptions")
	}
	if srcOpts.Force {
		t.Error("expected SetSource Force=false")
	}
}

// TestReprotect_HealthConditionsUpdated verifies that Replicating conditions
// are updated on both DRExecution and DRPlan during health monitoring.
func TestReprotect_HealthConditionsUpdated(t *testing.T) {
	d := fake.New()
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthSyncing},
	})
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{Health: drivers.HealthHealthy},
	})

	h := &ReprotectHandler{
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}
	input := newReprotectInput(vgs)

	_, err := h.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check Replicating condition on execution.
	var found bool
	for _, c := range input.Execution.Status.Conditions {
		if c.Type == "Replicating" {
			found = true
			if c.Reason != "SyncInProgress" {
				t.Errorf("expected Reason=SyncInProgress, got %s", c.Reason)
			}
			break
		}
	}
	if !found {
		t.Error("expected Replicating condition on DRExecution")
	}

	// Check Replicating condition on plan.
	found = false
	for _, c := range input.Plan.Status.Conditions {
		if c.Type == "Replicating" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Replicating condition on DRPlan")
	}
}

// TestReprotect_ResultMethod verifies the Result() classification logic.
func TestReprotect_ResultMethod(t *testing.T) {
	tests := []struct {
		name     string
		result   ReprotectResult
		expected soteriav1alpha1.ExecutionResult
	}{
		{
			name:     "all succeeded",
			result:   ReprotectResult{SetupSucceeded: 2, TotalVGs: 2, HealthyVGs: 2},
			expected: soteriav1alpha1.ExecutionResultSucceeded,
		},
		{
			name:     "timed out",
			result:   ReprotectResult{SetupSucceeded: 2, TotalVGs: 2, HealthyVGs: 1, TimedOut: true},
			expected: soteriav1alpha1.ExecutionResultPartiallySucceeded,
		},
		{
			name:     "partial setup failure",
			result:   ReprotectResult{SetupSucceeded: 1, SetupFailed: 1, TotalVGs: 2, HealthyVGs: 1},
			expected: soteriav1alpha1.ExecutionResultPartiallySucceeded,
		},
		{
			name:     "all failed",
			result:   ReprotectResult{SetupSucceeded: 0, SetupFailed: 2, TotalVGs: 2},
			expected: soteriav1alpha1.ExecutionResultFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Result(); got != tt.expected {
				t.Errorf("Result() = %s, want %s", got, tt.expected)
			}
		})
	}
}

// --- State machine verification tests for Task 12 ---

func TestTransition_PlannedMigration_FromDRedSteadyState(t *testing.T) {
	phase, err := Transition(soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModePlannedMigration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase != soteriav1alpha1.PhaseFailingBack {
		t.Errorf("expected FailingBack, got %s", phase)
	}
}

func TestTransition_Disaster_FromDRedSteadyState(t *testing.T) {
	phase, err := Transition(soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModeDisaster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase != soteriav1alpha1.PhaseFailingBack {
		t.Errorf("expected FailingBack, got %s", phase)
	}
}

func TestCompleteTransition_FailingBack_ReturnsFailedBack(t *testing.T) {
	phase, err := CompleteTransition(soteriav1alpha1.PhaseFailingBack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase != soteriav1alpha1.PhaseFailedBack {
		t.Errorf("expected FailedBack, got %s", phase)
	}
}

func TestReprotectFromFailedBack_RestoreToSteadyState(t *testing.T) {
	// Reprotect from FailedBack → ReprotectingBack.
	phase, err := Transition(soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModeReprotect)
	if err != nil {
		t.Fatalf("Transition(FailedBack, reprotect): %v", err)
	}
	if phase != soteriav1alpha1.PhaseReprotectingBack {
		t.Errorf("expected ReprotectingBack, got %s", phase)
	}

	// CompleteTransition(ReprotectingBack) → SteadyState.
	final, err := CompleteTransition(phase)
	if err != nil {
		t.Fatalf("CompleteTransition(ReprotectingBack): %v", err)
	}
	if final != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("expected SteadyState, got %s", final)
	}
}

// --- Full lifecycle test for Task 13 ---

func TestFullDRLifecycle_EightPhases(t *testing.T) {
	type step struct {
		name string
		fn   func(string) (string, error)
	}

	steps := []step{
		{"failover start", func(phase string) (string, error) {
			return Transition(phase, soteriav1alpha1.ExecutionModeDisaster)
		}},
		{"failover complete", func(phase string) (string, error) {
			return CompleteTransition(phase)
		}},
		{"reprotect start", func(phase string) (string, error) {
			return Transition(phase, soteriav1alpha1.ExecutionModeReprotect)
		}},
		{"reprotect complete", func(phase string) (string, error) {
			return CompleteTransition(phase)
		}},
		{"failback start", func(phase string) (string, error) {
			return Transition(phase, soteriav1alpha1.ExecutionModePlannedMigration)
		}},
		{"failback complete", func(phase string) (string, error) {
			return CompleteTransition(phase)
		}},
		{"restore start", func(phase string) (string, error) {
			return Transition(phase, soteriav1alpha1.ExecutionModeReprotect)
		}},
		{"restore complete", func(phase string) (string, error) {
			return CompleteTransition(phase)
		}},
	}

	expectedPhases := []string{
		soteriav1alpha1.PhaseFailingOver,
		soteriav1alpha1.PhaseFailedOver,
		soteriav1alpha1.PhaseReprotecting,
		soteriav1alpha1.PhaseDRedSteadyState,
		soteriav1alpha1.PhaseFailingBack,
		soteriav1alpha1.PhaseFailedBack,
		soteriav1alpha1.PhaseReprotectingBack,
		soteriav1alpha1.PhaseSteadyState,
	}

	phase := soteriav1alpha1.PhaseSteadyState
	for i, s := range steps {
		next, err := s.fn(phase)
		if err != nil {
			t.Fatalf("step %d (%s) from %s: %v", i+1, s.name, phase, err)
		}
		if next != expectedPhases[i] {
			t.Errorf("step %d (%s): expected %s, got %s", i+1, s.name, expectedPhases[i], next)
		}
		phase = next
	}

	if phase != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("lifecycle did not return to SteadyState, ended at %s", phase)
	}

	// Verify the cycle can be repeated.
	next, err := Transition(phase, soteriav1alpha1.ExecutionModePlannedMigration)
	if err != nil {
		t.Fatalf("second cycle start failed: %v", err)
	}
	if next != soteriav1alpha1.PhaseFailingOver {
		t.Errorf("second cycle: expected FailingOver, got %s", next)
	}
}

func TestFullDRLifecycle_EightPhases_WithDisasterFailback(t *testing.T) {
	phase := soteriav1alpha1.PhaseSteadyState

	// Disaster failover.
	phase, _ = Transition(phase, soteriav1alpha1.ExecutionModeDisaster)
	phase, _ = CompleteTransition(phase)

	// Re-protect.
	phase, _ = Transition(phase, soteriav1alpha1.ExecutionModeReprotect)
	phase, _ = CompleteTransition(phase)

	// Disaster failback from DRedSteadyState.
	next, err := Transition(phase, soteriav1alpha1.ExecutionModeDisaster)
	if err != nil {
		t.Fatalf("disaster failback: %v", err)
	}
	if next != soteriav1alpha1.PhaseFailingBack {
		t.Errorf("expected FailingBack, got %s", next)
	}
	phase = next

	phase, _ = CompleteTransition(phase)
	if phase != soteriav1alpha1.PhaseFailedBack {
		t.Errorf("expected FailedBack, got %s", phase)
	}

	// Restore.
	phase, _ = Transition(phase, soteriav1alpha1.ExecutionModeReprotect)
	phase, _ = CompleteTransition(phase)

	if phase != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("expected SteadyState after full cycle, got %s", phase)
	}
}
