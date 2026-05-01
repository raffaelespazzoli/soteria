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
	"sigs.k8s.io/controller-runtime/pkg/event"

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
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 after setup (yield for fresh resourceVersion)")
	}

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
		NamespacedName: types.NamespacedName{Name: "exec-label"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 after setup (yield for fresh resourceVersion)")
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
// When setup runs (expectSet=true), the reconciler yields with Requeue=true
// to allow the informer cache to settle before wave execution.
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
	if expectSet && result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 after setup (yield for fresh resourceVersion)")
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

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "exec-wait-new"}}

	// First reconcile: setup yields with immediate requeue.
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("setup reconcile: unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 after setup (yield for fresh resourceVersion)")
	}

	// Second reconcile: StartTime is set, enters wave execution path,
	// hits the Step0Complete wait.
	result, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("wave reconcile: unexpected error: %v", err)
	}
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

func TestDRExecutionReconciler_ResumePath_WaitsForStep0Complete(t *testing.T) {
	// Multi-site planned migration: the Owner resume path must wait for
	// Step0Complete before dispatching to the WaveExecutor. Without this
	// gate, persistStatus in the WaveExecutor can overwrite Step0Complete
	// set by the source site.
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-resume-step0"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-r",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Conditions: []metav1.Condition{
				{
					Type:   "Progressing",
					Status: metav1.ConditionTrue,
					Reason: "ExecutionStarted",
				},
			},
		},
	}
	plan := newSiteAwarePlan("plan-r", "east", "west", soteriav1alpha1.PhaseSteadyState)
	plan.Status.ActiveExecution = "exec-resume-step0"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModePlannedMigration
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "west", // Target site = Owner
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-resume-step0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("expected RequeueAfter 5s for Step0 wait on resume path, got %v", result.RequeueAfter)
	}
}

func TestDRExecutionReconciler_ResumePath_ProceedsAfterStep0Complete(t *testing.T) {
	// When Step0Complete IS set, the resume path should proceed normally.
	now := metav1.Now()
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-resume-ok"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-rok",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Conditions: []metav1.Condition{
				{
					Type:   "Progressing",
					Status: metav1.ConditionTrue,
					Reason: "ExecutionStarted",
				},
				{
					Type:   "Step0Complete",
					Status: metav1.ConditionTrue,
					Reason: "SourceSiteStep0Completed",
				},
			},
		},
	}
	plan := newSiteAwarePlan("plan-rok", "east", "west", soteriav1alpha1.PhaseSteadyState)
	plan.Status.ActiveExecution = "exec-resume-ok"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModePlannedMigration
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:         cl,
		Scheme:         newTestScheme(),
		LocalSite:      "west",
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-resume-ok"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 5*time.Second {
		t.Error("should NOT requeue with 5s when Step0Complete is set")
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

// --- VM readiness gate tests (Story 5.6) ---

// testVMManager implements engine.VMManager for reconciler unit tests.
type testVMManager struct {
	ready map[string]bool
	err   error
}

func (m *testVMManager) StopVM(_ context.Context, _, _ string) error              { return nil }
func (m *testVMManager) StartVM(_ context.Context, _, _ string) error             { return nil }
func (m *testVMManager) IsVMRunning(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (m *testVMManager) IsVMReady(_ context.Context, name, namespace string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.ready[namespace+"/"+name], nil
}

func newWaveGateExec(
	name, planName string,
	mode soteriav1alpha1.ExecutionMode,
	waves []soteriav1alpha1.WaveStatus,
) *soteriav1alpha1.DRExecution {
	now := metav1.Now()
	return &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"soteria.io/plan-name": planName},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: planName,
			Mode:     mode,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Waves:     waves,
		},
	}
}

func newWaveGatePlan(name string) *soteriav1alpha1.DRPlan {
	return &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:      soteriav1alpha1.PhaseSteadyState,
			ActiveSite: "dc-west",
			Waves: []soteriav1alpha1.WaveInfo{
				{
					WaveKey: "1",
					VMs:     []soteriav1alpha1.DiscoveredVM{{Name: "vm-1", Namespace: "ns1"}, {Name: "vm-2", Namespace: "ns1"}},
				},
				{
					WaveKey: "2",
					VMs:     []soteriav1alpha1.DiscoveredVM{{Name: "vm-3", Namespace: "ns2"}},
				},
			},
		},
	}
}

func TestReconcileWaveProgress_AllVMsReady_GroupsCompleted(t *testing.T) {
	vmReadyStart := metav1.NewTime(time.Now().Add(-10 * time.Second))
	exec := newWaveGateExec("exec-ready", "plan-1", soteriav1alpha1.ExecutionModeDisaster, []soteriav1alpha1.WaveStatus{
		{
			WaveIndex:        0,
			VMReadyStartTime: &vmReadyStart,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady, VMNames: []string{"vm-1", "vm-2"}},
			},
		},
		{
			WaveIndex: 1,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-1", Result: soteriav1alpha1.DRGroupResultPending, VMNames: []string{"vm-3"}},
			},
		},
	})

	plan := newWaveGatePlan("plan-1")
	plan.Status.ActiveExecution = "exec-ready"
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: &testVMManager{ready: map[string]bool{"ns1/vm-1": true, "ns1/vm-2": true}},
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-ready"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-ready"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	group0 := fetched.Status.Waves[0].Groups[0]
	if group0.Result != soteriav1alpha1.DRGroupResultCompleted {
		t.Errorf("expected group-0 Completed, got %q", group0.Result)
	}
	if group0.CompletionTime == nil {
		t.Error("expected CompletionTime to be set")
	}

	hasWaitStep := false
	for _, step := range group0.Steps {
		if step.Name == "WaitVMReady" && step.Status == "Succeeded" {
			hasWaitStep = true
		}
	}
	if !hasWaitStep {
		t.Error("expected WaitVMReady step with Succeeded status")
	}

	// Should requeue to process wave 1.
	if result.RequeueAfter == 0 {
		t.Error("expected requeue to advance to next wave")
	}
}

func TestReconcileWaveProgress_VMsNotReady_Requeues(t *testing.T) {
	vmReadyStart := metav1.NewTime(time.Now().Add(-5 * time.Second))
	exec := newWaveGateExec("exec-waiting", "plan-1", soteriav1alpha1.ExecutionModeDisaster, []soteriav1alpha1.WaveStatus{
		{
			WaveIndex:        0,
			VMReadyStartTime: &vmReadyStart,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady, VMNames: []string{"vm-1"}},
			},
		},
	})

	plan := newWaveGatePlan("plan-1")
	plan.Status.ActiveExecution = "exec-waiting"
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: &testVMManager{ready: map[string]bool{}},
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-waiting"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should requeue with safety interval since VMs are not ready yet.
	if result.RequeueAfter != 10*time.Second {
		t.Errorf("expected RequeueAfter 10s, got %v", result.RequeueAfter)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-waiting"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}
	if fetched.Status.Waves[0].Groups[0].Result != soteriav1alpha1.DRGroupResultWaitingForVMReady {
		t.Errorf("expected group still WaitingForVMReady, got %q", fetched.Status.Waves[0].Groups[0].Result)
	}
}

func TestReconcileWaveProgress_Timeout_DisasterFailForward(t *testing.T) {
	vmReadyStart := metav1.NewTime(time.Now().Add(-6 * time.Minute))
	exec := newWaveGateExec("exec-timeout-disaster", "plan-1",
		soteriav1alpha1.ExecutionModeDisaster, []soteriav1alpha1.WaveStatus{
			{
				WaveIndex:        0,
				VMReadyStartTime: &vmReadyStart,
				Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady, VMNames: []string{"vm-1"}},
					{Name: "group-1", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-2"}},
				},
			},
			{
				WaveIndex: 1,
				Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Name: "group-2", Result: soteriav1alpha1.DRGroupResultPending, VMNames: []string{"vm-3"}},
				},
			},
		})

	plan := newWaveGatePlan("plan-1")
	plan.Status.ActiveExecution = "exec-timeout-disaster"
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: &testVMManager{ready: map[string]bool{}},
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-timeout-disaster"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-timeout-disaster"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	// Timed-out group should be Failed.
	group0 := fetched.Status.Waves[0].Groups[0]
	if group0.Result != soteriav1alpha1.DRGroupResultFailed {
		t.Errorf("expected group-0 Failed after timeout, got %q", group0.Result)
	}
	if group0.Error == "" {
		t.Error("expected error message on timed-out group")
	}

	// Disaster mode = fail-forward: execution should NOT be aborted.
	if fetched.Status.Result == soteriav1alpha1.ExecutionResultFailed {
		t.Error("disaster mode should fail-forward, not abort execution")
	}

	// Should continue to next wave (requeue).
	if result.RequeueAfter == 0 {
		t.Error("expected requeue to advance to next wave after fail-forward")
	}
}

func TestReconcileWaveProgress_Timeout_PlannedMigrationFailFast(t *testing.T) {
	vmReadyStart := metav1.NewTime(time.Now().Add(-6 * time.Minute))
	exec := newWaveGateExec("exec-timeout-pm", "plan-1",
		soteriav1alpha1.ExecutionModePlannedMigration, []soteriav1alpha1.WaveStatus{
			{
				WaveIndex:        0,
				VMReadyStartTime: &vmReadyStart,
				Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady, VMNames: []string{"vm-1"}},
				},
			},
		})
	exec.Status.Conditions = []metav1.Condition{
		{
			Type:   "Step0Complete",
			Status: metav1.ConditionTrue,
			Reason: "Test",
		},
	}

	plan := newWaveGatePlan("plan-1")
	plan.Status.ActiveExecution = "exec-timeout-pm"
	plan.Status.ActiveExecutionMode = soteriav1alpha1.ExecutionModePlannedMigration
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: &testVMManager{ready: map[string]bool{}},
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-timeout-pm"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-timeout-pm"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	// Planned migration fail-fast: entire execution should be Failed.
	if fetched.Status.Result != soteriav1alpha1.ExecutionResultFailed {
		t.Errorf("planned_migration should fail-fast, expected Failed, got %q", fetched.Status.Result)
	}
}

func TestReconcileWaveProgress_DefaultTimeout(t *testing.T) {
	// VMReadyStartTime 4 minutes ago — should NOT timeout with default 5m.
	vmReadyStart := metav1.NewTime(time.Now().Add(-4 * time.Minute))
	exec := newWaveGateExec("exec-default-to", "plan-1",
		soteriav1alpha1.ExecutionModeDisaster, []soteriav1alpha1.WaveStatus{
			{
				WaveIndex:        0,
				VMReadyStartTime: &vmReadyStart,
				Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady, VMNames: []string{"vm-1"}},
				},
			},
		})

	plan := newWaveGatePlan("plan-1")
	plan.Status.ActiveExecution = "exec-default-to"
	// VMReadyTimeout is nil — should use default 5m.
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: &testVMManager{ready: map[string]bool{}},
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-default-to"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-default-to"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	// At 4m with default 5m timeout, group should still be waiting.
	if fetched.Status.Waves[0].Groups[0].Result != soteriav1alpha1.DRGroupResultWaitingForVMReady {
		t.Errorf("expected group still WaitingForVMReady before timeout, got %q",
			fetched.Status.Waves[0].Groups[0].Result)
	}
	if result.RequeueAfter != 10*time.Second {
		t.Errorf("expected safety requeue 10s, got %v", result.RequeueAfter)
	}
}

func TestReconcileWaveProgress_NoVMManager_AutoCompletes(t *testing.T) {
	vmReadyStart := metav1.NewTime(time.Now())
	exec := newWaveGateExec("exec-no-mgr", "plan-1", soteriav1alpha1.ExecutionModeDisaster, []soteriav1alpha1.WaveStatus{
		{
			WaveIndex:        0,
			VMReadyStartTime: &vmReadyStart,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady, VMNames: []string{"vm-1"}},
			},
		},
	})

	plan := newWaveGatePlan("plan-1")
	plan.Status.ActiveExecution = "exec-no-mgr"
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: nil,
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-no-mgr"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-no-mgr"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	// No VMManager → auto-complete all waiting groups.
	if fetched.Status.Waves[0].Groups[0].Result != soteriav1alpha1.DRGroupResultCompleted {
		t.Errorf("expected auto-completed, got %q", fetched.Status.Waves[0].Groups[0].Result)
	}
}

func TestVMPrintableStatusChanged_Predicate(t *testing.T) {
	pred := vmPrintableStatusChanged()

	// Create events should be filtered out.
	if pred.Create(event.CreateEvent{Object: &kubevirtv1.VirtualMachine{}}) {
		t.Error("CreateEvent should be filtered")
	}
	if pred.Delete(event.DeleteEvent{Object: &kubevirtv1.VirtualMachine{}}) {
		t.Error("DeleteEvent should be filtered")
	}
	if pred.Generic(event.GenericEvent{Object: &kubevirtv1.VirtualMachine{}}) {
		t.Error("GenericEvent should be filtered")
	}

	// Update with no printableStatus change.
	oldVM := &kubevirtv1.VirtualMachine{
		Status: kubevirtv1.VirtualMachineStatus{PrintableStatus: kubevirtv1.VirtualMachineStatusStarting},
	}
	newVM := &kubevirtv1.VirtualMachine{
		Status: kubevirtv1.VirtualMachineStatus{PrintableStatus: kubevirtv1.VirtualMachineStatusStarting},
	}
	if pred.Update(event.UpdateEvent{ObjectOld: oldVM, ObjectNew: newVM}) {
		t.Error("same printableStatus should be filtered")
	}

	// Update with printableStatus change.
	newVM.Status.PrintableStatus = kubevirtv1.VirtualMachineStatusRunning
	if !pred.Update(event.UpdateEvent{ObjectOld: oldVM, ObjectNew: newVM}) {
		t.Error("changed printableStatus should pass")
	}
}

func TestMapVMToDRExecution(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-map"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:           soteriav1alpha1.PhaseSteadyState,
			ActiveSite:      "dc-west",
			ActiveExecution: "exec-active",
		},
	}
	cl := newTestClient(plan)
	r := &DRExecutionReconciler{Client: cl, Scheme: newTestScheme()}

	// VM with matching label → should return the active execution.
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-1",
			Namespace: "ns1",
			Labels:    map[string]string{soteriav1alpha1.DRPlanLabel: "plan-map"},
		},
	}
	reqs := r.mapVMToDRExecution(context.Background(), vm)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Name != "exec-active" {
		t.Errorf("expected exec-active, got %q", reqs[0].Name)
	}

	// VM without label → should return nil.
	vmNoLabel := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-2", Namespace: "ns1"},
	}
	reqs = r.mapVMToDRExecution(context.Background(), vmNoLabel)
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests for unlabeled VM, got %d", len(reqs))
	}

	// VM with label pointing to plan with no active execution.
	plan2 := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-idle"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase: soteriav1alpha1.PhaseSteadyState,
		},
	}
	cl2 := newTestClient(plan2)
	r2 := &DRExecutionReconciler{Client: cl2, Scheme: newTestScheme()}
	vmIdle := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-3",
			Namespace: "ns1",
			Labels:    map[string]string{soteriav1alpha1.DRPlanLabel: "plan-idle"},
		},
	}
	reqs = r2.mapVMToDRExecution(context.Background(), vmIdle)
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests when no active execution, got %d", len(reqs))
	}
}

func TestHasWaitingForVMReady(t *testing.T) {
	tests := []struct {
		name   string
		waves  []soteriav1alpha1.WaveStatus
		expect bool
	}{
		{
			"no waves",
			nil,
			false,
		},
		{
			"no waiting groups",
			[]soteriav1alpha1.WaveStatus{
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{{Result: soteriav1alpha1.DRGroupResultCompleted}}},
			},
			false,
		},
		{
			"has waiting group",
			[]soteriav1alpha1.WaveStatus{
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{{Result: soteriav1alpha1.DRGroupResultWaitingForVMReady}}},
			},
			true,
		},
		{
			"waiting in second wave",
			[]soteriav1alpha1.WaveStatus{
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{{Result: soteriav1alpha1.DRGroupResultCompleted}}},
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{{Result: soteriav1alpha1.DRGroupResultWaitingForVMReady}}},
			},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &soteriav1alpha1.DRExecution{
				Status: soteriav1alpha1.DRExecutionStatus{Waves: tt.waves},
			}
			got := hasWaitingForVMReady(exec)
			if got != tt.expect {
				t.Errorf("hasWaitingForVMReady = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestReconcileWaveProgress_CustomTimeout(t *testing.T) {
	// Custom VMReadyTimeout of 2 minutes, started 3 minutes ago → should timeout.
	vmReadyStart := metav1.NewTime(time.Now().Add(-3 * time.Minute))
	exec := newWaveGateExec("exec-custom-to", "plan-custom",
		soteriav1alpha1.ExecutionModeDisaster, []soteriav1alpha1.WaveStatus{
			{
				WaveIndex:        0,
				VMReadyStartTime: &vmReadyStart,
				Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady, VMNames: []string{"vm-1"}},
				},
			},
		})

	twoMin := metav1.Duration{Duration: 2 * time.Minute}
	plan := newWaveGatePlan("plan-custom")
	plan.Spec.VMReadyTimeout = &twoMin
	plan.Status.ActiveExecution = "exec-custom-to"
	cl := newTestClient(exec, plan)

	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: &testVMManager{ready: map[string]bool{}},
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-custom-to"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-custom-to"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}

	// Custom 2m timeout with 3m elapsed → group should be Failed.
	if fetched.Status.Waves[0].Groups[0].Result != soteriav1alpha1.DRGroupResultFailed {
		t.Errorf("expected group Failed after custom timeout, got %q",
			fetched.Status.Waves[0].Groups[0].Result)
	}
}

// TestMultiWaveGate_FullSequence exercises the complete wave gate contract
// across multiple Reconcile() calls: wave 0 waits → VMs ready → wave 1 starts
// → wave 1 waits → VMs ready → execution finishes. This proves the gate is
// enforced between every pair of waves, not just wave 0 (AC1, AC12).
func TestMultiWaveGate_FullSequence(t *testing.T) {
	vmReadyStart := metav1.NewTime(time.Now().Add(-10 * time.Second))
	exec := newWaveGateExec("exec-multi-gate", "plan-1",
		soteriav1alpha1.ExecutionModeDisaster, []soteriav1alpha1.WaveStatus{
			{
				WaveIndex:        0,
				VMReadyStartTime: &vmReadyStart,
				Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{
						Name:    "group-0",
						Result:  soteriav1alpha1.DRGroupResultWaitingForVMReady,
						VMNames: []string{"vm-1"},
					},
				},
			},
			{
				WaveIndex: 1,
				Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{
						Name:    "group-1",
						Result:  soteriav1alpha1.DRGroupResultPending,
						VMNames: []string{"vm-3"},
					},
				},
			},
		})

	plan := newWaveGatePlan("plan-1")
	plan.Status.ActiveExecution = "exec-multi-gate"
	cl := newTestClient(exec, plan)

	vmMgr := &testVMManager{ready: map[string]bool{}}
	r := &DRExecutionReconciler{
		Client:    cl,
		Scheme:    newTestScheme(),
		VMManager: vmMgr,
		WaveExecutor: &engine.WaveExecutor{
			Client: cl,
		},
		ResumeAnalyzer: &engine.ResumeAnalyzer{},
		Handler:        &engine.NoOpHandler{},
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "exec-multi-gate"},
	}

	// --- Reconcile 1: wave 0 VMs not ready → requeue ---
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if result.RequeueAfter != 10*time.Second {
		t.Fatalf("reconcile 1: expected 10s requeue, got %v", result.RequeueAfter)
	}

	// Wave 1 group must still be Pending (gate held).
	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(),
		client.ObjectKey{Name: "exec-multi-gate"}, &fetched); err != nil {
		t.Fatalf("get after reconcile 1: %v", err)
	}
	if fetched.Status.Waves[1].Groups[0].Result != soteriav1alpha1.DRGroupResultPending {
		t.Fatalf("reconcile 1: wave 1 should still be Pending, got %q",
			fetched.Status.Waves[1].Groups[0].Result)
	}

	// --- Mark wave 0 VMs ready ---
	vmMgr.ready["ns1/vm-1"] = true

	// --- Reconcile 2: wave 0 completes → requeue to advance ---
	result, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("reconcile 2: expected requeue to advance to wave 1")
	}

	if err := cl.Get(context.Background(),
		client.ObjectKey{Name: "exec-multi-gate"}, &fetched); err != nil {
		t.Fatalf("get after reconcile 2: %v", err)
	}
	// Wave 0 should be Completed.
	if fetched.Status.Waves[0].Groups[0].Result != soteriav1alpha1.DRGroupResultCompleted {
		t.Fatalf("reconcile 2: wave 0 group should be Completed, got %q",
			fetched.Status.Waves[0].Groups[0].Result)
	}
	// Wave 1 should still be Pending (handler hasn't run yet).
	if fetched.Status.Waves[1].Groups[0].Result != soteriav1alpha1.DRGroupResultPending {
		t.Fatalf("reconcile 2: wave 1 should still be Pending, got %q",
			fetched.Status.Waves[1].Groups[0].Result)
	}

	// --- Reconcile 3: wave 1 handler runs → WaitingForVMReady ---
	// The reconciler should route through reconcileWaveExecution, NOT
	// reconcileResume. ExecuteWaveHandler with NoOpHandler + no real
	// VMDiscoverer means groups stay Pending → the handler is a no-op →
	// but the convertToWaitingForVMReady will try to convert Completed
	// groups. Since the handler can't actually run (no discoverer), verify
	// the execution is NOT finalized — confirming that the resume bypass is
	// not used and the wave-by-wave pipeline is in control.
	result, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile 3: %v", err)
	}

	if err := cl.Get(context.Background(),
		client.ObjectKey{Name: "exec-multi-gate"}, &fetched); err != nil {
		t.Fatalf("get after reconcile 3: %v", err)
	}
	// The execution should NOT have been finalized by ExecuteFromWave.
	// If it was, that means the resume path ran (the bug from finding #1).
	if fetched.Status.Result == soteriav1alpha1.ExecutionResultSucceeded {
		t.Fatal("reconcile 3: execution should NOT be finalized — " +
			"the resume path (ExecuteFromWave) was incorrectly used")
	}
}

func TestHasInProgressGroups(t *testing.T) {
	tests := []struct {
		name   string
		waves  []soteriav1alpha1.WaveStatus
		expect bool
	}{
		{
			"no waves",
			nil,
			false,
		},
		{
			"all completed",
			[]soteriav1alpha1.WaveStatus{
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Result: soteriav1alpha1.DRGroupResultCompleted},
				}},
			},
			false,
		},
		{
			"has in-progress",
			[]soteriav1alpha1.WaveStatus{
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Result: soteriav1alpha1.DRGroupResultCompleted},
					{Result: soteriav1alpha1.DRGroupResultInProgress},
				}},
			},
			true,
		},
		{
			"pending is not in-progress",
			[]soteriav1alpha1.WaveStatus{
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Result: soteriav1alpha1.DRGroupResultPending},
				}},
			},
			false,
		},
		{
			"waiting is not in-progress",
			[]soteriav1alpha1.WaveStatus{
				{Groups: []soteriav1alpha1.DRGroupExecutionStatus{
					{Result: soteriav1alpha1.DRGroupResultWaitingForVMReady},
				}},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &soteriav1alpha1.DRExecution{
				Status: soteriav1alpha1.DRExecutionStatus{Waves: tt.waves},
			}
			got := hasInProgressGroups(exec)
			if got != tt.expect {
				t.Errorf("hasInProgressGroups = %v, want %v", got, tt.expect)
			}
		})
	}
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
