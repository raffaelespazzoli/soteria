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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestStrategy_NamespaceScoped_ReturnsFalse(t *testing.T) {
	if Strategy.NamespaceScoped() {
		t.Error("DRExecution strategy must be cluster-scoped (NamespaceScoped() == false)")
	}
}

func TestGetAttrs_ReturnsNameField(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "exec-1",
			Labels: map[string]string{"mode": "planned"},
		},
	}

	lbls, flds, err := GetAttrs(exec)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if lbls["mode"] != "planned" {
		t.Errorf("expected label mode=planned, got %v", lbls)
	}

	if flds["metadata.name"] != "exec-1" {
		t.Errorf("expected metadata.name=exec-1, got %q", flds["metadata.name"])
	}
}

func TestGetAttrs_DoesNotIncludeNamespace(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "exec-1",
			Namespace: "leftover-ns",
		},
	}

	_, flds, err := GetAttrs(exec)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if _, ok := flds["metadata.namespace"]; ok {
		t.Error("cluster-scoped DRExecution GetAttrs must not include metadata.namespace")
	}
}

func TestGetAttrs_WrongType_ReturnsError(t *testing.T) {
	wrong := &soteriav1alpha1.DRPlan{}
	_, _, err := GetAttrs(wrong)
	if err == nil {
		t.Error("GetAttrs should return an error for non-DRExecution objects")
	}
}

func TestMatchDRExecution_UsesGetAttrs(t *testing.T) {
	pred := MatchDRExecution(nil, nil)
	if pred.GetAttrs == nil {
		t.Error("MatchDRExecution predicate must have GetAttrs set")
	}
}

// --- Status strategy relaxation tests (Story 4.6) ---

func TestStatusStrategy_PartiallySucceeded_AllowsUpdate(t *testing.T) {
	oldExec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-1"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: soteriav1alpha1.ExecutionResultPartiallySucceeded,
		},
	}
	newExec := oldExec.DeepCopy()
	newExec.Status.Result = soteriav1alpha1.ExecutionResultSucceeded

	errs := StatusStrategy.ValidateUpdate(context.Background(), newExec, oldExec)
	if len(errs) != 0 {
		t.Errorf("expected no errors for PartiallySucceeded → update, got: %v", errs)
	}
}

func TestStatusStrategy_Succeeded_BlocksUpdate(t *testing.T) {
	oldExec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-1"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: soteriav1alpha1.ExecutionResultSucceeded,
		},
	}
	newExec := oldExec.DeepCopy()
	newExec.Status.Result = soteriav1alpha1.ExecutionResultFailed

	errs := StatusStrategy.ValidateUpdate(context.Background(), newExec, oldExec)
	if len(errs) == 0 {
		t.Error("expected errors for Succeeded → update, got none")
	}
}

func TestStatusStrategy_Failed_BlocksUpdate(t *testing.T) {
	oldExec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-1"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: soteriav1alpha1.ExecutionResultFailed,
		},
	}
	newExec := oldExec.DeepCopy()
	newExec.Status.Result = soteriav1alpha1.ExecutionResultSucceeded

	errs := StatusStrategy.ValidateUpdate(context.Background(), newExec, oldExec)
	if len(errs) == 0 {
		t.Error("expected errors for Failed → update, got none")
	}
}

func TestStatusStrategy_EmptyResult_AllowsUpdate(t *testing.T) {
	oldExec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-1"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: "",
		},
	}
	newExec := oldExec.DeepCopy()
	newExec.Status.Result = soteriav1alpha1.ExecutionResultSucceeded

	errs := StatusStrategy.ValidateUpdate(context.Background(), newExec, oldExec)
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty result → update (execution in progress), got: %v", errs)
	}
}

// --- Field selector tests (Story 5.4) ---

func TestGetAttrs_IncludesSpecPlanName(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-fs"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "erp-full-stack",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	_, flds, err := GetAttrs(exec)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if flds["spec.planName"] != "erp-full-stack" {
		t.Errorf("expected spec.planName=erp-full-stack, got %q", flds["spec.planName"])
	}
}

func TestMatchDRExecution_FieldSelector_PlanName(t *testing.T) {
	execA := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-a"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "plan-a"},
	}
	execB := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-b"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "plan-b"},
	}

	sel, err := fields.ParseSelector("spec.planName=plan-a")
	if err != nil {
		t.Fatalf("parsing field selector: %v", err)
	}

	pred := MatchDRExecution(labels.Everything(), sel)

	matchA, err := pred.Matches(execA)
	if err != nil {
		t.Fatalf("matching exec-a: %v", err)
	}
	if !matchA {
		t.Error("exec-a should match spec.planName=plan-a")
	}

	matchB, err := pred.Matches(execB)
	if err != nil {
		t.Fatalf("matching exec-b: %v", err)
	}
	if matchB {
		t.Error("exec-b should not match spec.planName=plan-a")
	}
}
