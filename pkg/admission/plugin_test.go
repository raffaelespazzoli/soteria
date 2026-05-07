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

package admission

import (
	"context"
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// stubGetter implements rest.Getter for testing the admission plugin.
type stubGetter struct {
	plans map[string]*soteriav1alpha1.DRPlan
}

func (s *stubGetter) Get(_ context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	plan, ok := s.plans[name]
	if !ok {
		return nil, &notFoundError{name: name}
	}
	return plan.DeepCopy(), nil
}

type notFoundError struct{ name string }

func (e *notFoundError) Error() string {
	return fmt.Sprintf("drplans %q not found", e.name)
}
func (e *notFoundError) Status() metav1.Status {
	return metav1.Status{Reason: metav1.StatusReasonNotFound}
}

func makePluginExecAttributes(exec *soteriav1alpha1.DRExecution, op admission.Operation) admission.Attributes {
	return admission.NewAttributesRecord(
		exec,
		nil,
		schema.GroupVersionKind{Group: "soteria.io", Version: "v1alpha1", Kind: "DRExecution"},
		"",
		exec.Name,
		schema.GroupVersionResource{Group: "soteria.io", Version: "v1alpha1", Resource: "drexecutions"},
		"",
		op,
		nil,
		false,
		nil,
	)
}

func makePluginPlanAttributes(
	plan *soteriav1alpha1.DRPlan, oldPlan *soteriav1alpha1.DRPlan, op admission.Operation,
) admission.Attributes {
	var old runtime.Object
	if oldPlan != nil {
		old = oldPlan
	}
	return admission.NewAttributesRecord(
		plan,
		old,
		schema.GroupVersionKind{Group: "soteria.io", Version: "v1alpha1", Kind: "DRPlan"},
		"",
		plan.Name,
		schema.GroupVersionResource{Group: "soteria.io", Version: "v1alpha1", Resource: "drplans"},
		"",
		op,
		nil,
		false,
		nil,
	)
}

// --- DRExecution admission tests ---

func TestPlugin_DRExecution_ValidCREATE_Allowed(t *testing.T) {
	tests := []struct {
		name      string
		planPhase string
		mode      soteriav1alpha1.ExecutionMode
	}{
		{"planned migration from steady state",
			soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModePlannedMigration},
		{"disaster from steady state",
			soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModeDisaster},
		{"reprotect from failed over",
			soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModeReprotect},
		{"planned migration from DRed steady state",
			soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModePlannedMigration},
		{"reprotect from failed back",
			soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModeReprotect},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSoteriaAdmissionPlugin()
			p.SetDRPlanStorage(&stubGetter{
				plans: map[string]*soteriav1alpha1.DRPlan{
					"my-plan": {
						ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
						Spec: soteriav1alpha1.DRPlanSpec{
							PrimarySite:   "dc-west",
							SecondarySite: "dc-east",
						},
						Status: soteriav1alpha1.DRPlanStatus{Phase: tt.planPhase},
					},
				},
			})

			exec := &soteriav1alpha1.DRExecution{
				ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
				Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: tt.mode},
			}

			err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
			if err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
		})
	}
}

func TestPlugin_DRExecution_MissingPlanName_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "planName") {
		t.Errorf("expected error containing 'planName', got %q", err.Error())
	}
}

func TestPlugin_DRExecution_InvalidMode_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: "invalid_mode"},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_mode") {
		t.Errorf("expected error containing 'invalid_mode', got %q", err.Error())
	}
}

func TestPlugin_DRExecution_PlanNotFound_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{plans: map[string]*soteriav1alpha1.DRPlan{}})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "nonexistent",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error containing 'not found', got %q", err.Error())
	}
}

func TestPlugin_DRExecution_ActiveExecution_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase:           soteriav1alpha1.PhaseSteadyState,
					ActiveExecution: "existing-exec",
				},
			},
		},
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "new-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "existing-exec") {
		t.Errorf("expected error containing 'existing-exec', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "concurrent") {
		t.Errorf("expected error containing 'concurrent', got %q", err.Error())
	}
}

func TestPlugin_DRExecution_InvalidPhaseTransition_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseFailedOver},
			},
		},
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), soteriav1alpha1.PhaseFailedOver) {
		t.Errorf("expected error containing current phase, got %q", err.Error())
	}
}

func TestPlugin_DRExecution_SitesOutOfSync_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase: soteriav1alpha1.PhaseSteadyState,
					Conditions: []metav1.Condition{{
						Type:               "SitesInSync",
						Status:             metav1.ConditionFalse,
						Reason:             "VMsMismatch",
						LastTransitionTime: metav1.Now(),
					}},
				},
			},
		},
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sites do not agree") {
		t.Errorf("expected error about site disagreement, got %q", err.Error())
	}
}

func TestPlugin_DRExecution_SitesInSync_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase: soteriav1alpha1.PhaseSteadyState,
					Conditions: []metav1.Condition{{
						Type:               "SitesInSync",
						Status:             metav1.ConditionTrue,
						Reason:             "VMsAgreed",
						LastTransitionTime: metav1.Now(),
					}},
				},
			},
		},
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err != nil {
		t.Errorf("expected allowed, got error: %v", err)
	}
}

func TestPlugin_DRExecution_DisksInconsistent_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase: soteriav1alpha1.PhaseSteadyState,
					Conditions: []metav1.Condition{
						{
							Type:               "SitesInSync",
							Status:             metav1.ConditionTrue,
							Reason:             "VMsAgreed",
							LastTransitionTime: metav1.Now(),
						},
						{
							Type:               "DisksConsistent",
							Status:             metav1.ConditionFalse,
							Reason:             "DiskMismatch",
							Message:            "VM default/web01: count mismatch",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
		},
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "disk topology does not match") {
		t.Errorf("expected error about disk topology, got %q", err.Error())
	}
}

func TestPlugin_DRExecution_DisksConsistent_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase: soteriav1alpha1.PhaseSteadyState,
					Conditions: []metav1.Condition{
						{
							Type:               "SitesInSync",
							Status:             metav1.ConditionTrue,
							Reason:             "VMsAgreed",
							LastTransitionTime: metav1.Now(),
						},
						{
							Type:               "DisksConsistent",
							Status:             metav1.ConditionTrue,
							Reason:             "DisksAgreed",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
		},
	})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Create), nil)
	if err != nil {
		t.Errorf("expected allowed, got error: %v", err)
	}
}

func TestPlugin_DRExecution_NonCreateOperation_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	p.SetDRPlanStorage(&stubGetter{})

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec:       soteriav1alpha1.DRExecutionSpec{PlanName: "my-plan", Mode: soteriav1alpha1.ExecutionModePlannedMigration},
	}

	err := p.Validate(context.Background(), makePluginExecAttributes(exec, admission.Update), nil)
	if err != nil {
		t.Errorf("expected UPDATE to be allowed, got error: %v", err)
	}
}

// --- DRPlan admission tests ---

func TestPlugin_DRPlan_ValidCreate_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-ok"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}

	err := p.Validate(context.Background(), makePluginPlanAttributes(plan, nil, admission.Create), nil)
	if err != nil {
		t.Errorf("expected allowed, got error: %v", err)
	}
}

func TestPlugin_DRPlan_InvalidMaxConcurrent_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-bad"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 0,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}

	err := p.Validate(context.Background(), makePluginPlanAttributes(plan, nil, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "maxConcurrentFailovers") {
		t.Errorf("expected error containing 'maxConcurrentFailovers', got %q", err.Error())
	}
}

func TestPlugin_DRPlan_MissingSites_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-no-sites"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
		},
	}

	err := p.Validate(context.Background(), makePluginPlanAttributes(plan, nil, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "primarySite") {
		t.Errorf("expected error containing 'primarySite', got %q", err.Error())
	}
}

func TestPlugin_DRPlan_EqualSites_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-same-site"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-west",
		},
	}

	err := p.Validate(context.Background(), makePluginPlanAttributes(plan, nil, admission.Create), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "secondarySite") {
		t.Errorf("expected error containing 'secondarySite', got %q", err.Error())
	}
}

func TestPlugin_DRPlan_UpdateImmutableSite_Denied(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	oldPlan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-immutable"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}
	newPlan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-immutable"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-north",
			SecondarySite:          "dc-east",
		},
	}

	err := p.Validate(context.Background(), makePluginPlanAttributes(newPlan, oldPlan, admission.Update), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "primarySite") {
		t.Errorf("expected error containing 'primarySite', got %q", err.Error())
	}
}

func TestPlugin_DRPlan_ValidUpdate_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-update"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 8,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}

	err := p.Validate(context.Background(), makePluginPlanAttributes(plan, plan, admission.Update), nil)
	if err != nil {
		t.Errorf("expected allowed, got error: %v", err)
	}
}

func TestPlugin_DRPlan_DeleteOperation_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-del"},
		Spec: soteriav1alpha1.DRPlanSpec{
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}

	err := p.Validate(context.Background(), makePluginPlanAttributes(plan, nil, admission.Delete), nil)
	if err != nil {
		t.Errorf("expected DELETE to be allowed, got error: %v", err)
	}
}

// --- Subresource and unknown resource tests ---

func TestPlugin_SubresourceRequest_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
	}
	attrs := admission.NewAttributesRecord(
		exec, nil,
		schema.GroupVersionKind{Group: "soteria.io", Version: "v1alpha1", Kind: "DRExecution"},
		"", "test-exec",
		schema.GroupVersionResource{Group: "soteria.io", Version: "v1alpha1", Resource: "drexecutions"},
		"status",
		admission.Update, nil, false, nil,
	)

	err := p.Validate(context.Background(), attrs, nil)
	if err != nil {
		t.Errorf("expected subresource request to be allowed, got error: %v", err)
	}
}

func TestPlugin_UnknownResource_Allowed(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()

	attrs := admission.NewAttributesRecord(
		nil, nil,
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		"default", "my-deploy",
		schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		"",
		admission.Create, nil, false, nil,
	)

	err := p.Validate(context.Background(), attrs, nil)
	if err != nil {
		t.Errorf("expected unknown resource to be allowed, got error: %v", err)
	}
}

func TestPlugin_Handles_CreateAndUpdate(t *testing.T) {
	p := NewSoteriaAdmissionPlugin()
	if !p.Handles(admission.Create) {
		t.Error("expected Handles(Create) to be true")
	}
	if !p.Handles(admission.Update) {
		t.Error("expected Handles(Update) to be true")
	}
	if p.Handles(admission.Delete) {
		t.Error("expected Handles(Delete) to be false")
	}
}
