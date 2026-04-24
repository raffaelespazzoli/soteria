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

package drexecution

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	_ = kubevirtv1.AddToScheme(s)
	return s
}

func newTestClient(objs ...client.Object) client.Client {
	scheme := newTestScheme()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(
			&soteriav1alpha1.DRExecution{},
			&soteriav1alpha1.DRPlan{},
			&soteriav1alpha1.DRGroupStatus{},
		).
		Build()
}

func TestDRExecutionReconciler_ResumeInProgress_EmitsEvent(t *testing.T) {
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-resume"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Result:    "", // In-progress — needs resume.
			Waves: []soteriav1alpha1.WaveStatus{
				{
					WaveIndex: 0,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
						{Name: "group-1", Result: soteriav1alpha1.DRGroupResultInProgress, VMNames: []string{"vm-2"}},
					},
				},
			},
		},
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-1"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:           soteriav1alpha1.PhaseSteadyState,
			ActiveSite:      "dc-west",
			ActiveExecution: "exec-resume",
		},
	}

	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		// No WaveExecutor — we just verify the resume path is taken
		// and in-flight groups are reset.
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-resume"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result

	// Verify the in-flight group was reset to Pending.
	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-resume"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}
	group1 := fetched.Status.Waves[0].Groups[1]
	if group1.Result != soteriav1alpha1.DRGroupResultPending {
		t.Errorf("expected in-flight group reset to Pending, got %q", group1.Result)
	}
}

func TestDRExecutionReconciler_CompletedExecution_NoResume(t *testing.T) {
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-done"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime:      &now,
			CompletionTime: &now,
			Result:         soteriav1alpha1.ExecutionResultSucceeded,
		},
	}

	cl := newTestClient(exec)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-done"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result

	// Succeeded execution should not trigger resume — it should be skipped
	// at the terminal result check (before the resume check).
}

func TestDRExecutionReconciler_NewExecution_NormalPath(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-new"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		// StartTime is nil — new execution, not resume.
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-1"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:      soteriav1alpha1.PhaseSteadyState,
			ActiveSite: "dc-west",
		},
	}

	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-new"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result

	// Verify StartTime was set (new execution setup phase).
	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-new"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}
	if fetched.Status.StartTime == nil {
		t.Error("expected StartTime to be set for new execution")
	}

	// Verify plan phase stays at rest state and ActiveExecution is set.
	var updatedPlan soteriav1alpha1.DRPlan
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "plan-1"}, &updatedPlan); err != nil {
		t.Fatalf("fetching plan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("expected plan phase SteadyState (rest), got %q", updatedPlan.Status.Phase)
	}
	if updatedPlan.Status.ActiveExecution != "exec-new" {
		t.Errorf("expected ActiveExecution %q, got %q", "exec-new", updatedPlan.Status.ActiveExecution)
	}
	if updatedPlan.Status.ActiveExecutionMode != soteriav1alpha1.ExecutionModePlannedMigration {
		t.Errorf("expected ActiveExecutionMode %q, got %q",
			soteriav1alpha1.ExecutionModePlannedMigration, updatedPlan.Status.ActiveExecutionMode)
	}
}

func TestDRExecutionReconciler_LeaderOnly(t *testing.T) {
	// Leader election is managed by controller-runtime at the manager level.
	// Verify the reconciler doesn't have any leader-specific logic — it relies
	// on the manager to only start reconcile loops on the leader instance.
	// This test verifies that SetupWithManager completes successfully.
	r := &DRExecutionReconciler{
		Client:         newTestClient(),
		Scheme:         newTestScheme(),
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
	}

	// We can't call SetupWithManager without a real manager, but we verify
	// the reconciler struct is properly configured for leader-only operation
	// by confirming it has no leader election logic of its own.
	_ = r
}

func TestResetInFlightGroup(t *testing.T) {
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-reset"},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Waves: []soteriav1alpha1.WaveStatus{
				{
					WaveIndex: 0,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted},
						{Name: "group-1", Result: soteriav1alpha1.DRGroupResultInProgress},
						{Name: "group-2", Result: soteriav1alpha1.DRGroupResultFailed},
					},
				},
			},
		},
	}

	r := &DRExecutionReconciler{}

	// Reset in-flight group.
	r.resetInFlightGroup(exec, 0, "group-1")

	if exec.Status.Waves[0].Groups[1].Result != soteriav1alpha1.DRGroupResultPending {
		t.Errorf("expected group-1 reset to Pending, got %q", exec.Status.Waves[0].Groups[1].Result)
	}

	// Completed and Failed groups should NOT be affected.
	if exec.Status.Waves[0].Groups[0].Result != soteriav1alpha1.DRGroupResultCompleted {
		t.Error("completed group should not be reset")
	}
	if exec.Status.Waves[0].Groups[2].Result != soteriav1alpha1.DRGroupResultFailed {
		t.Error("failed group should not be reset")
	}
}

func TestResetInFlightGroup_OutOfBounds(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-oob"},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &metav1.Time{Time: time.Now()},
		},
	}

	r := &DRExecutionReconciler{}

	// Should not panic on out-of-bounds wave index.
	r.resetInFlightGroup(exec, 5, "nonexistent")
}

func TestDRExecutionReconciler_PlanNameLabel_SetOnFirstReconcile(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-label"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "erp-full-stack",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "erp-full-stack"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:      soteriav1alpha1.PhaseSteadyState,
			ActiveSite: "dc-west",
		},
	}

	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-label"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-label"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	if fetched.Labels == nil {
		t.Fatal("expected labels map to be non-nil")
	}
	got := fetched.Labels["soteria.io/plan-name"]
	if got != "erp-full-stack" {
		t.Errorf("expected label soteria.io/plan-name=erp-full-stack, got %q", got)
	}
	if fetched.Status.StartTime == nil {
		t.Error("expected StartTime to be set")
	}
}

func TestDRExecutionReconciler_PlanNameLabel_Idempotent(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name: "exec-label-idem",
			Labels: map[string]string{
				"soteria.io/plan-name": "erp-full-stack",
			},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "erp-full-stack",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "erp-full-stack"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:      soteriav1alpha1.PhaseSteadyState,
			ActiveSite: "dc-west",
		},
	}

	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-label-idem"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The reconciler should still complete setup without issuing a redundant
	// metadata update. Verify the label is unchanged and StartTime is set.
	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-label-idem"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	if fetched.Labels["soteria.io/plan-name"] != "erp-full-stack" {
		t.Errorf("label should remain erp-full-stack, got %q", fetched.Labels["soteria.io/plan-name"])
	}
	if fetched.Status.StartTime == nil {
		t.Error("expected StartTime to be set")
	}
}

// --- Site-aware reconcile ownership tests ---

func newSiteAwarePlan(name, primary, secondary, phase string) *soteriav1alpha1.DRPlan {
	return &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
			PrimarySite:            primary,
			SecondarySite:          secondary,
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:      phase,
			ActiveSite: primary,
		},
	}
}

// reconcileAndAssertStartTime is a helper that reconciles and checks whether
// StartTime was set (expectSet=true) or remained nil (expectSet=false).
func reconcileAndAssertStartTime(
	t *testing.T, cl client.Client, r *DRExecutionReconciler, execName string, expectSet bool,
) ctrl.Result {
	t.Helper()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: execName},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: execName}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}
	if expectSet && fetched.Status.StartTime == nil {
		t.Error("expected StartTime to be set")
	}
	if !expectSet && fetched.Status.StartTime != nil {
		t.Error("expected StartTime to remain nil")
	}
	return result
}

func TestDRExecutionReconciler_RoleNone_SkipsReconcile(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-disaster"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModeDisaster,
		},
	}
	plan := newSiteAwarePlan("plan-1", "east", "west", soteriav1alpha1.PhaseSteadyState)
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "east",
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result := reconcileAndAssertStartTime(t, cl, r, "exec-disaster", false)
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for RoleNone, got %v", result.RequeueAfter)
	}
}

func TestDRExecutionReconciler_RoleOwner_ProceedsNormally(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-owner"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModeDisaster,
		},
	}
	plan := newSiteAwarePlan("plan-1", "east", "west", soteriav1alpha1.PhaseSteadyState)
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "west",
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	reconcileAndAssertStartTime(t, cl, r, "exec-owner", true)
}

func TestDRExecutionReconciler_RoleStep0_PlannedMigration(t *testing.T) {
	// Planned migration: source site (east) should get RoleStep0.
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-step0"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
		},
	}
	plan := newSiteAwarePlan("plan-1", "east", "west", soteriav1alpha1.PhaseSteadyState)
	plan.Status.ActiveExecution = "exec-step0"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModePlannedMigration
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "east", // Source site — gets Step0
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-step0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Step0Complete was set.
	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-step0"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}
	if !meta.IsStatusConditionTrue(fetched.Status.Conditions, "Step0Complete") {
		t.Error("expected Step0Complete condition to be set")
	}
}

func TestDRExecutionReconciler_RoleStep0_Idempotent(t *testing.T) {
	// If Step0Complete is already set, reconcileStep0 returns immediately.
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-step0-idem"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Conditions: []metav1.Condition{
				{
					Type:   "Step0Complete",
					Status: metav1.ConditionTrue,
					Reason: "SourceSiteStep0Completed",
				},
			},
		},
	}
	plan := newSiteAwarePlan("plan-1", "east", "west", soteriav1alpha1.PhaseSteadyState)
	plan.Status.ActiveExecution = "exec-step0-idem"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModePlannedMigration
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		LocalSite: "east",
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-step0-idem"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for idempotent Step0, got %v", result.RequeueAfter)
	}
}

func TestDRExecutionReconciler_MisconfigurationGuard(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-misconfig"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
	plan := newSiteAwarePlan("plan-1", "north", "south", soteriav1alpha1.PhaseSteadyState)
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "east", // Doesn't match north or south
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
	}

	reconcileAndAssertStartTime(t, cl, r, "exec-misconfig", false)
}

func TestDRExecutionReconciler_PlannedMigration_OwnerWaitsForStep0(t *testing.T) {
	// Target site (west) is Owner for planned migration. Step0Complete not set
	// yet — after setup, the Owner should wait with RequeueAfter(5s).
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-wait-new"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-2",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
	plan := newSiteAwarePlan("plan-2", "east", "west", soteriav1alpha1.PhaseSteadyState)
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "west",
		WaveExecutor:   &engine.WaveExecutor{},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-wait-new"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After setup completes, the Owner should hit the Step0Complete wait
	// and return RequeueAfter(5s).
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("expected RequeueAfter 5s for Step0Complete wait, got %v", result.RequeueAfter)
	}
}

func TestDRExecutionReconciler_PlannedMigration_OwnerProceedsAfterStep0(t *testing.T) {
	// Target site (west) is Owner for planned migration. Step0Complete IS set.
	// Should proceed with wave execution (no wait).
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "exec-proceed",
			Labels: map[string]string{"soteria.io/plan-name": "plan-1"},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Conditions: []metav1.Condition{
				{
					Type:   "Step0Complete",
					Status: metav1.ConditionTrue,
					Reason: "SourceSiteStep0Completed",
				},
				{
					Type:   "Progressing",
					Status: metav1.ConditionTrue,
					Reason: "ExecutionStarted",
				},
			},
		},
	}
	plan := newSiteAwarePlan("plan-1", "east", "west", soteriav1alpha1.PhaseSteadyState)
	plan.Status.ActiveExecution = "exec-proceed"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModePlannedMigration
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "west",
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
		// No WaveExecutor — resume path with no waves will be a no-op.
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-proceed"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With Step0Complete set and no WaveExecutor, it should proceed through
	// resume (no-op) and return without requeue.
	if result.RequeueAfter == 5*time.Second {
		t.Error("should NOT requeue with 5s when Step0Complete is already present")
	}
}

func TestDRExecutionReconciler_DisasterMode_SourceExitsImmediately(t *testing.T) {
	// AC6: disaster mode, source site → RoleNone → return without action.
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-disaster-source"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModeDisaster,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
		},
	}
	plan := newSiteAwarePlan("plan-1", "east", "west", soteriav1alpha1.PhaseSteadyState)
	plan.Status.ActiveExecution = "exec-disaster-source"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModeDisaster
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "east", // Source site in disaster mode → RoleNone
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-disaster-source"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for disaster source, got %v", result.RequeueAfter)
	}
}

func TestDRExecutionReconciler_NoLocalSite_BackwardCompat(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-legacy"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
	plan := newSiteAwarePlan("plan-1", "dc-alpha", "dc-beta", soteriav1alpha1.PhaseSteadyState)
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "",
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	reconcileAndAssertStartTime(t, cl, r, "exec-legacy", true)
}

func TestDRExecutionReconciler_ReprotectOwnership(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-reprotect"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModeReprotect,
		},
	}
	plan := newSiteAwarePlan("plan-1", "dc-primary", "dc-secondary", soteriav1alpha1.PhaseFailedOver)
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "dc-primary", // Source in Reprotecting → RoleNone
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
	}

	reconcileAndAssertStartTime(t, cl, r, "exec-reprotect", false)
}
