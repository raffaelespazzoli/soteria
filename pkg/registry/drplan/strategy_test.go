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
	"sync/atomic"
	"testing"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// stubExecLister implements rest.Lister for testing the table convertor's
// DRExecution-derived effective phase and active execution columns.
type stubExecLister struct {
	executions []soteriav1alpha1.DRExecution
	listCalls  atomic.Int32
}

func (s *stubExecLister) NewList() runtime.Object {
	return &soteriav1alpha1.DRExecutionList{}
}

func (s *stubExecLister) List(_ context.Context, _ *metainternalversion.ListOptions) (runtime.Object, error) {
	s.listCalls.Add(1)
	return &soteriav1alpha1.DRExecutionList{Items: s.executions}, nil
}

func (s *stubExecLister) ConvertToTable(_ context.Context, _ runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	return &metav1.Table{}, nil
}

// errExecLister returns an error from List, simulating a broken cache or storage.
type errExecLister struct{}

func (e *errExecLister) NewList() runtime.Object {
	return &soteriav1alpha1.DRExecutionList{}
}

func (e *errExecLister) List(_ context.Context, _ *metainternalversion.ListOptions) (runtime.Object, error) {
	return nil, fmt.Errorf("simulated storage error")
}

func (e *errExecLister) ConvertToTable(_ context.Context, _ runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	return &metav1.Table{}, nil
}

func TestStrategy_NamespaceScoped_ReturnsFalse(t *testing.T) {
	if Strategy.NamespaceScoped() {
		t.Error("DRPlan strategy must be cluster-scoped (NamespaceScoped() == false)")
	}
}

func TestGetAttrs_ReturnsNameField(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-plan",
			Labels: map[string]string{"tier": "frontend"},
		},
	}

	lbls, flds, err := GetAttrs(plan)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if lbls["tier"] != "frontend" {
		t.Errorf("expected label tier=frontend, got %v", lbls)
	}

	if flds["metadata.name"] != "my-plan" {
		t.Errorf("expected metadata.name=my-plan, got %q", flds["metadata.name"])
	}
}

func TestGetAttrs_DoesNotIncludeNamespace(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-plan",
			Namespace: "leftover-ns",
		},
	}

	_, flds, err := GetAttrs(plan)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if _, ok := flds["metadata.namespace"]; ok {
		t.Error("cluster-scoped DRPlan GetAttrs must not include metadata.namespace")
	}
}

func TestGetAttrs_WrongType_ReturnsError(t *testing.T) {
	wrong := &soteriav1alpha1.DRExecution{}
	_, _, err := GetAttrs(wrong)
	if err == nil {
		t.Error("GetAttrs should return an error for non-DRPlan objects")
	}
}

func TestMatchDRPlan_UsesGetAttrs(t *testing.T) {
	pred := MatchDRPlan(nil, nil)
	if pred.GetAttrs == nil {
		t.Error("MatchDRPlan predicate must have GetAttrs set")
	}
}

func TestPrepareForCreate_SetsActiveSiteToPrimarySite(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}

	Strategy.PrepareForCreate(context.Background(), plan)

	if plan.Status.ActiveSite != "dc-west" {
		t.Errorf("expected activeSite = %q, got %q", "dc-west", plan.Status.ActiveSite)
	}
	if plan.Status.Phase != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("expected phase = %q, got %q", soteriav1alpha1.PhaseSteadyState, plan.Status.Phase)
	}
}

func TestPrepareForUpdate_PreservesStatus(t *testing.T) {
	oldPlan := &soteriav1alpha1.DRPlan{
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase:      soteriav1alpha1.PhaseFailedOver,
			ActiveSite: "dc-east",
		},
	}
	newPlan := &soteriav1alpha1.DRPlan{
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 8,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}

	Strategy.PrepareForUpdate(context.Background(), newPlan, oldPlan)

	if newPlan.Status.ActiveSite != "dc-east" {
		t.Errorf("expected activeSite preserved as %q, got %q", "dc-east", newPlan.Status.ActiveSite)
	}
	if newPlan.Status.Phase != soteriav1alpha1.PhaseFailedOver {
		t.Errorf("expected phase preserved as %q, got %q", soteriav1alpha1.PhaseFailedOver, newPlan.Status.Phase)
	}
}

func TestConvertToTable_DeriveEffectivePhase_FromExecution(t *testing.T) {
	tc := &DRPlanTableConvertor{
		drexecutionLister: &stubExecLister{
			executions: []soteriav1alpha1.DRExecution{{
				ObjectMeta: metav1.ObjectMeta{Name: "exec-pm-1"},
				Spec: soteriav1alpha1.DRExecutionSpec{
					PlanName: "my-plan",
					Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
				},
				Status: soteriav1alpha1.DRExecutionStatus{},
			}},
		},
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
		Status:     soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseSteadyState},
	}

	table, err := tc.ConvertToTable(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("ConvertToTable returned error: %v", err)
	}
	if len(table.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(table.Rows))
	}
	cells := table.Rows[0].Cells
	if cells[2] != soteriav1alpha1.PhaseFailingOver {
		t.Errorf("Effective Phase = %q, want %q", cells[2], soteriav1alpha1.PhaseFailingOver)
	}
	if cells[5] != "exec-pm-1" {
		t.Errorf("Active Execution = %q, want %q", cells[5], "exec-pm-1")
	}
}

func TestConvertToTable_NoActiveExecution_ShowsRestPhase(t *testing.T) {
	tc := &DRPlanTableConvertor{
		drexecutionLister: &stubExecLister{executions: nil},
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "idle-plan"},
		Status:     soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseFailedOver},
	}

	table, err := tc.ConvertToTable(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("ConvertToTable returned error: %v", err)
	}
	cells := table.Rows[0].Cells
	if cells[2] != soteriav1alpha1.PhaseFailedOver {
		t.Errorf("Effective Phase = %q, want rest phase %q", cells[2], soteriav1alpha1.PhaseFailedOver)
	}
	if cells[5] != "" {
		t.Errorf("Active Execution = %q, want empty", cells[5])
	}
}

func TestConvertToTable_TerminalExecution_Ignored(t *testing.T) {
	tc := &DRPlanTableConvertor{
		drexecutionLister: &stubExecLister{
			executions: []soteriav1alpha1.DRExecution{{
				ObjectMeta: metav1.ObjectMeta{Name: "exec-done"},
				Spec: soteriav1alpha1.DRExecutionSpec{
					PlanName: "my-plan",
					Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
				},
				Status: soteriav1alpha1.DRExecutionStatus{
					Result: soteriav1alpha1.ExecutionResultSucceeded,
				},
			}},
		},
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
		Status:     soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseSteadyState},
	}

	table, err := tc.ConvertToTable(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("ConvertToTable returned error: %v", err)
	}
	cells := table.Rows[0].Cells
	if cells[2] != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("Effective Phase = %q, want rest phase %q (terminal exec should be ignored)",
			cells[2], soteriav1alpha1.PhaseSteadyState)
	}
	if cells[5] != "" {
		t.Errorf("Active Execution = %q, want empty (terminal exec ignored)", cells[5])
	}
}

func TestConvertToTable_NilLister_FallsBackToRestPhase(t *testing.T) {
	tc := &DRPlanTableConvertor{drexecutionLister: nil}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy-plan"},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase: soteriav1alpha1.PhaseSteadyState,
		},
	}

	table, err := tc.ConvertToTable(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("ConvertToTable returned error: %v", err)
	}
	cells := table.Rows[0].Cells
	if cells[2] != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("Effective Phase = %q, want %q (fallback to rest phase)", cells[2], soteriav1alpha1.PhaseSteadyState)
	}
	if cells[5] != "" {
		t.Errorf("Active Execution = %q, want empty (nil lister cannot derive active execution)", cells[5])
	}
}

func TestConvertToTable_BulkList_SingleQuery(t *testing.T) {
	lister := &stubExecLister{
		executions: []soteriav1alpha1.DRExecution{{
			ObjectMeta: metav1.ObjectMeta{Name: "exec-1"},
			Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "plan-a", Mode: soteriav1alpha1.ExecutionModeDisaster},
			Status:     soteriav1alpha1.DRExecutionStatus{},
		}},
	}
	tc := &DRPlanTableConvertor{drexecutionLister: lister}

	planList := &soteriav1alpha1.DRPlanList{
		Items: []soteriav1alpha1.DRPlan{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-a"},
				Status:     soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseSteadyState},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-b"},
				Status:     soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseFailedOver},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-c"},
				Status:     soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseSteadyState},
			},
		},
	}

	table, err := tc.ConvertToTable(context.Background(), planList, nil)
	if err != nil {
		t.Fatalf("ConvertToTable returned error: %v", err)
	}
	if len(table.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(table.Rows))
	}

	calls := lister.listCalls.Load()
	if calls != 1 {
		t.Errorf("List was called %d times, want exactly 1 (bulk query, not per-plan)", calls)
	}

	// plan-a should have derived effective phase from the active execution
	if table.Rows[0].Cells[2] != soteriav1alpha1.PhaseFailingOver {
		t.Errorf("plan-a Effective Phase = %q, want %q", table.Rows[0].Cells[2], soteriav1alpha1.PhaseFailingOver)
	}
	if table.Rows[0].Cells[5] != "exec-1" {
		t.Errorf("plan-a Active Execution = %q, want %q", table.Rows[0].Cells[5], "exec-1")
	}
	// plan-b should show rest phase (no active exec)
	if table.Rows[1].Cells[2] != soteriav1alpha1.PhaseFailedOver {
		t.Errorf("plan-b Effective Phase = %q, want %q", table.Rows[1].Cells[2], soteriav1alpha1.PhaseFailedOver)
	}
	if table.Rows[1].Cells[5] != "" {
		t.Errorf("plan-b Active Execution = %q, want empty", table.Rows[1].Cells[5])
	}
}

func TestConvertToTable_ListError_ShowsIdleNotFallback(t *testing.T) {
	tc := &DRPlanTableConvertor{drexecutionLister: &errExecLister{}}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-err"},
		Status: soteriav1alpha1.DRPlanStatus{
			Phase: soteriav1alpha1.PhaseSteadyState,
		},
	}

	table, err := tc.ConvertToTable(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("ConvertToTable returned error: %v", err)
	}
	cells := table.Rows[0].Cells
	if cells[2] != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("Effective Phase = %q, want %q (must not fall back to stale status)",
			cells[2], soteriav1alpha1.PhaseSteadyState)
	}
	if cells[5] != "" {
		t.Errorf("Active Execution = %q, want empty (must not fall back to stale status)", cells[5])
	}
}

func TestConvertToTable_DuplicateActive_KeepsFirst(t *testing.T) {
	tc := &DRPlanTableConvertor{
		drexecutionLister: &stubExecLister{
			executions: []soteriav1alpha1.DRExecution{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "exec-first"},
					Spec: soteriav1alpha1.DRExecutionSpec{
						PlanName: "my-plan",
						Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
					},
					Status: soteriav1alpha1.DRExecutionStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "exec-second"},
					Spec: soteriav1alpha1.DRExecutionSpec{
						PlanName: "my-plan",
						Mode:     soteriav1alpha1.ExecutionModeDisaster,
					},
					Status: soteriav1alpha1.DRExecutionStatus{},
				},
			},
		},
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
		Status:     soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseSteadyState},
	}

	table, err := tc.ConvertToTable(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("ConvertToTable returned error: %v", err)
	}
	cells := table.Rows[0].Cells
	if cells[5] != "exec-first" {
		t.Errorf("Active Execution = %q, want %q (first-wins policy)", cells[5], "exec-first")
	}
}
