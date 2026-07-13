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
			Spec: soteriav1alpha1.DRPlanSpec{
				VolumeReplicationDriver: soteriav1alpha1.VolumeReplicationDriverConfig{Type: "noop"},
				PrimarySite:             "dc-west",
				SecondarySite:           "dc-east",
			},
			Status: soteriav1alpha1.DRPlanStatus{
				Phase:      soteriav1alpha1.PhaseFailedOver,
				ActiveSite: "dc-east",
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
	// Phase 1: both VGs report RoleTarget (secondary — planned failover path).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	d.OnGetReplicationStatus("vg-2").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	// Phase 2: health monitoring sees healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	d.OnGetReplicationStatus("vg-2").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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

	if !d.Called("GetReplicationStatus") {
		t.Error("expected GetReplicationStatus to be called")
	}
	if d.Called("ResyncVolume") {
		t.Error("ResyncVolume should not be called for VGs already in secondary state")
	}
	if d.Called("SetSource") {
		t.Error("reprotect should not call SetSource")
	}
	if d.Called("StopReplication") {
		t.Error("reprotect should not call StopReplication")
	}
}

func TestReprotect_StatusCheckFails_VGMarkedFailed(t *testing.T) {
	d1 := fake.New()
	// VG-1: healthy secondary.
	d1.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	// Phase 2 health check.
	d1.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})

	d2 := fake.New()
	// VG-2: GetReplicationStatus returns error.
	d2.OnGetReplicationStatus("vg-2").Return(errors.New("status check failed"))

	d3 := fake.New()
	// VG-3: healthy secondary.
	d3.OnGetReplicationStatus("vg-3").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	// Phase 2 health check.
	d3.OnGetReplicationStatus("vg-3").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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
		t.Errorf("Result() = %s, want PartiallySucceeded (mixed status check failure)", got)
	}
}

func TestReprotect_AllStatusCheckFail_ExecutionFails(t *testing.T) {
	d := fake.New()
	d.OnGetReplicationStatus().Return(errors.New("status check failed"))
	d.OnGetReplicationStatus().Return(errors.New("status check failed"))

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
		t.Fatal("expected error when all status checks fail")
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
	// Phase 1: RoleTarget (secondary, passes verification).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	// Phase 2: Always return Syncing — never Healthy.
	for range 100 {
		d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
			ReplicationStatus: &drivers.ReplicationStatus{
				Role:   drivers.RoleTarget,
				Health: drivers.HealthSyncing,
			},
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
	// Phase 1: RoleTarget (secondary, passes verification).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	// Phase 2 — first poll: Syncing, second poll: Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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

	// Phase 1 + Phase 2: at least 3 calls (1 verification + 2 health polls).
	if d.CallCount("GetReplicationStatus") < 3 {
		t.Errorf("expected at least 3 GetReplicationStatus calls, got %d",
			d.CallCount("GetReplicationStatus"))
	}
}

func TestReprotect_ResumeFromHealthMonitoring(t *testing.T) {
	// Simulate a resume scenario: VG is already Target (secondary) from a
	// prior run. State verification passes, health monitoring sees Healthy.
	d := fake.New()
	// Phase 1: RoleTarget (already secondary).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	// Phase 2: Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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
	// Phase 1: RoleTarget (secondary, verification passes).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	// Phase 2: two polls — first Syncing, then Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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

	// Checkpoints: 1 after state verification + 2 during health monitoring (one per poll).
	calls := cp.GetCalls()
	if len(calls) < 3 {
		t.Errorf("expected at least 3 checkpoint writes (1 verification + 2 health polls), got %d", len(calls))
	}
}

func TestReprotect_ContextCancelled(t *testing.T) {
	d := fake.New()
	// Phase 1: RoleTarget passes verification.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	// Phase 2: always Syncing — context will cancel before health completes.
	for range 100 {
		d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
			ReplicationStatus: &drivers.ReplicationStatus{
				Role:   drivers.RoleTarget,
				Health: drivers.HealthSyncing,
			},
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
	// Phase 1: RoleTarget (passes verification).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	// Phase 2: Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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

	// Should have: StateVerification(Succeeded), HealthMonitoring(Succeeded).
	if len(result.Steps) < 2 {
		t.Fatalf("expected at least 2 steps, got %d", len(result.Steps))
	}

	stepNames := make(map[string]string)
	for _, s := range result.Steps {
		stepNames[s.Name] = s.Status
	}

	if stepNames[StepReprotectStateVerification] != reprotectStatusSucceeded {
		t.Errorf("expected StateVerification Succeeded, got %q", stepNames[StepReprotectStateVerification])
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

func TestReprotect_SecondaryState_SkipsResync(t *testing.T) {
	d := fake.New()
	// Phase 1: RoleTarget (already secondary — expected after planned failover).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})
	// Phase 2: Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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
	if result.SetupSucceeded != 1 {
		t.Errorf("expected 1 succeeded, got %d", result.SetupSucceeded)
	}
	if d.Called("ResyncVolume") {
		t.Error("ResyncVolume should not be called for VGs already in secondary state")
	}
	if d.Called("SetSource") {
		t.Error("SetSource should not be called by reprotect")
	}
}

func TestReprotect_PrimaryOnActiveSite_PassesVerification(t *testing.T) {
	d := fake.New()
	// Phase 1: RoleSource (primary on active site — legitimate).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthHealthy,
		},
	})
	// Phase 2: health monitoring polls until healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthHealthy,
		},
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
	if result.SetupSucceeded != 1 {
		t.Errorf("expected 1 succeeded, got %d", result.SetupSucceeded)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded, got %s", result.Result())
	}
	if d.Called("ResyncVolume") {
		t.Error("Owner site should never call ResyncVolume during reprotect")
	}
	if d.Called("StopReplication") {
		t.Error("Owner site should never call StopReplication during reprotect")
	}
	if d.Called("SetSource") {
		t.Error("SetSource should not be called by reprotect")
	}
}

func TestReprotect_PrimaryOnActiveSite_HealthMonitoring(t *testing.T) {
	d := fake.New()
	// Phase 1: RoleSource (primary on active site) passes verification.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthSyncing,
		},
	})
	// Phase 2: health monitoring — first poll syncing, then healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthSyncing,
		},
	})
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthHealthy,
		},
	})

	cp := &NoOpCheckpointer{}
	h := &ReprotectHandler{
		Checkpointer:       cp,
		HealthPollInterval: 10 * time.Millisecond,
		HealthTimeout:      1 * time.Second,
	}

	vgs := []VolumeGroupEntry{makeVGEntry("vg-1", d, "vg-1")}
	input := newReprotectInput(vgs)

	result, err := h.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result() != soteriav1alpha1.ExecutionResultSucceeded {
		t.Errorf("expected Succeeded, got %s", result.Result())
	}
	if result.HealthyVGs != 1 {
		t.Errorf("expected 1 healthy, got %d", result.HealthyVGs)
	}
	if d.Called("ResyncVolume") {
		t.Error("Owner site should never call ResyncVolume during reprotect")
	}
}

func TestReprotect_MixedRoles_BothPass_ErrorVGFails(t *testing.T) {
	d1 := fake.New()
	// VG-1: RoleTarget (secondary — passes verification).
	d1.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
	})

	d2 := fake.New()
	// VG-2: RoleSource (primary on active site — passes verification).
	d2.OnGetReplicationStatus("vg-2").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthHealthy,
		},
	})

	d3 := fake.New()
	// VG-3: GetReplicationStatus error — marked as failed.
	d3.OnGetReplicationStatus("vg-3").Return(errors.New("cannot reach storage"))

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
	if len(result.FailedVGs) != 1 || result.FailedVGs[0] != "vg-3" {
		t.Errorf("expected FailedVGs=[vg-3], got %v", result.FailedVGs)
	}

	if d1.Called("ResyncVolume") {
		t.Error("VG-1 should not call ResyncVolume during reprotect")
	}
	if d2.Called("ResyncVolume") {
		t.Error("VG-2 should not call ResyncVolume during reprotect")
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

func TestReprotect_DriverCallsMade(t *testing.T) {
	d := fake.New()
	// Phase 1: RoleSource (primary on active site) passes verification.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleSource,
			Health: drivers.HealthHealthy,
		},
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

	if d.Called("SetSource") {
		t.Error("reprotect should not call SetSource")
	}
	if d.Called("StopReplication") {
		t.Error("reprotect should not call StopReplication")
	}
	if !d.Called("GetReplicationStatus") {
		t.Error("expected GetReplicationStatus to be called")
	}
	if d.Called("ResyncVolume") {
		t.Error("Owner site should never call ResyncVolume during reprotect")
	}
}

// TestReprotect_HealthConditionsUpdated verifies that Replicating conditions
// are updated on both DRExecution and DRPlan during health monitoring.
func TestReprotect_HealthConditionsUpdated(t *testing.T) {
	d := fake.New()
	// Phase 1: RoleTarget (passes verification).
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	// Phase 2: first poll Syncing, second Healthy.
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthSyncing,
		},
	})
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &drivers.ReplicationStatus{
			Role:   drivers.RoleTarget,
			Health: drivers.HealthHealthy,
		},
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
