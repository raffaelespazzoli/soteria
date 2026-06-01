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
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestStrategy_NamespaceScoped_ReturnsFalse(t *testing.T) {
	if Strategy.NamespaceScoped() {
		t.Error("DRExecution strategy must be cluster-scoped (NamespaceScoped() == false)")
	}
}

func TestPrepareForCreate_SetsPhaseAndIsActive(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-phase"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	Strategy.PrepareForCreate(context.Background(), exec)

	if exec.Status.Phase != soteriav1alpha1.ExecutionPhasePending {
		t.Errorf("expected Phase %q, got %q", soteriav1alpha1.ExecutionPhasePending, exec.Status.Phase)
	}
	if !exec.Status.IsActive {
		t.Error("expected IsActive to be true after PrepareForCreate")
	}
	if exec.Status.Result != "" {
		t.Errorf("expected empty Result, got %q", exec.Status.Result)
	}
}

func TestPrepareForCreate_StampsTriggeredByAnnotation(t *testing.T) {
	ctx := request.WithUser(context.Background(), &user.DefaultInfo{
		Name: "carlos@corp",
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-1"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	Strategy.PrepareForCreate(ctx, exec)

	got := exec.Annotations[soteriav1alpha1.TriggeredByAnnotation]
	if got != "carlos@corp" {
		t.Errorf("expected triggered-by annotation %q, got %q", "carlos@corp", got)
	}
}

func TestPrepareForCreate_PreservesExistingAnnotations(t *testing.T) {
	ctx := request.WithUser(context.Background(), &user.DefaultInfo{
		Name: "maya@corp",
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "exec-2",
			Annotations: map[string]string{"custom": "value"},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModeDisaster,
		},
	}

	Strategy.PrepareForCreate(ctx, exec)

	if exec.Annotations["custom"] != "value" {
		t.Error("existing annotations must be preserved")
	}
	if exec.Annotations[soteriav1alpha1.TriggeredByAnnotation] != "maya@corp" {
		t.Errorf("expected triggered-by %q, got %q", "maya@corp", exec.Annotations[soteriav1alpha1.TriggeredByAnnotation])
	}
}

func TestPrepareForCreate_OverwritesClientSuppliedTriggeredBy(t *testing.T) {
	ctx := request.WithUser(context.Background(), &user.DefaultInfo{
		Name: "server-identity",
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name: "exec-3",
			Annotations: map[string]string{
				soteriav1alpha1.TriggeredByAnnotation: "spoofed-user",
			},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModeReprotect,
		},
	}

	Strategy.PrepareForCreate(ctx, exec)

	got := exec.Annotations[soteriav1alpha1.TriggeredByAnnotation]
	if got != "server-identity" {
		t.Errorf("triggered-by must be overwritten with server identity, got %q", got)
	}
}

func TestPrepareForCreate_NoUserInContext_SkipsAnnotation(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-4"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	Strategy.PrepareForCreate(context.Background(), exec)

	if exec.Annotations != nil {
		if _, ok := exec.Annotations[soteriav1alpha1.TriggeredByAnnotation]; ok {
			t.Error("should not stamp triggered-by when no user in context")
		}
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

// --- OwnerReference tests (Story 13.1) ---

// stubPlanGetter implements rest.Getter for testing OwnerReference logic.
type stubPlanGetter struct {
	plan *soteriav1alpha1.DRPlan
	err  error
}

func (s *stubPlanGetter) Get(_ context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.plan != nil && s.plan.Name == name {
		return s.plan, nil
	}
	return nil, fmt.Errorf("drplans %q not found", name)
}

func TestPrepareForCreate_SetsOwnerReference(t *testing.T) {
	planUID := types.UID("plan-uid-abc-123")
	Strategy.SetPlanStorage(&stubPlanGetter{
		plan: &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-plan",
				UID:  planUID,
			},
		},
	})
	t.Cleanup(func() { Strategy.SetPlanStorage(nil) })

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-owner"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	Strategy.PrepareForCreate(context.Background(), exec)

	if len(exec.OwnerReferences) != 1 {
		t.Fatalf("expected 1 OwnerReference, got %d", len(exec.OwnerReferences))
	}

	ref := exec.OwnerReferences[0]
	if ref.APIVersion != soteriav1alpha1.SchemeGroupVersion.String() {
		t.Errorf("expected APIVersion %q, got %q", soteriav1alpha1.SchemeGroupVersion.String(), ref.APIVersion)
	}
	if ref.Kind != "DRPlan" {
		t.Errorf("expected Kind %q, got %q", "DRPlan", ref.Kind)
	}
	if ref.Name != "my-plan" {
		t.Errorf("expected Name %q, got %q", "my-plan", ref.Name)
	}
	if ref.UID != planUID {
		t.Errorf("expected UID %q, got %q", planUID, ref.UID)
	}
}

func TestPrepareForCreate_OwnerReference_ControllerAndBlockOwnerDeletion(t *testing.T) {
	Strategy.SetPlanStorage(&stubPlanGetter{
		plan: &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{
				Name: "plan-ctrl",
				UID:  types.UID("uid-ctrl"),
			},
		},
	})
	t.Cleanup(func() { Strategy.SetPlanStorage(nil) })

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-ctrl"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-ctrl",
			Mode:     soteriav1alpha1.ExecutionModeDisaster,
		},
	}

	Strategy.PrepareForCreate(context.Background(), exec)

	if len(exec.OwnerReferences) != 1 {
		t.Fatalf("expected 1 OwnerReference, got %d", len(exec.OwnerReferences))
	}

	ref := exec.OwnerReferences[0]
	if ref.Controller == nil || !*ref.Controller {
		t.Error("expected Controller=true on OwnerReference")
	}
	if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
		t.Error("expected BlockOwnerDeletion=true on OwnerReference")
	}
}

func TestPrepareForCreate_NilPlanGetter_NoOwnerReference(t *testing.T) {
	Strategy.SetPlanStorage(nil)

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-nil-getter"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-1",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	Strategy.PrepareForCreate(context.Background(), exec)

	if len(exec.OwnerReferences) != 0 {
		t.Errorf("expected no OwnerReferences when planGetter is nil, got %d", len(exec.OwnerReferences))
	}
	// Verify other PrepareForCreate mutations still applied
	if exec.Status.Phase != soteriav1alpha1.ExecutionPhasePending {
		t.Errorf("expected Phase %q, got %q", soteriav1alpha1.ExecutionPhasePending, exec.Status.Phase)
	}
}

func TestPrepareForCreate_PlanNotFound_NoOwnerReference(t *testing.T) {
	Strategy.SetPlanStorage(&stubPlanGetter{
		err: fmt.Errorf("drplans %q not found", "missing-plan"),
	})
	t.Cleanup(func() { Strategy.SetPlanStorage(nil) })

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-not-found"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "missing-plan",
			Mode:     soteriav1alpha1.ExecutionModeReprotect,
		},
	}

	Strategy.PrepareForCreate(context.Background(), exec)

	if len(exec.OwnerReferences) != 0 {
		t.Errorf("expected no OwnerReferences when plan not found, got %d", len(exec.OwnerReferences))
	}
	// Verify other PrepareForCreate mutations still applied
	if exec.Labels[soteriav1alpha1.PlanNameLabel] != "missing-plan" {
		t.Errorf("expected plan-name label to be set even when OwnerReference fails")
	}
}
