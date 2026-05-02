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
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// stubReader implements client.Reader for testing the admission webhook.
type stubReader struct {
	plans map[string]*soteriav1alpha1.DRPlan
}

func (r *stubReader) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	plan, ok := r.plans[key.Name]
	if !ok {
		return errors.NewNotFound(schema.GroupResource{
			Group:    "soteria.io",
			Resource: "drplans",
		}, key.Name)
	}
	p, ok := obj.(*soteriav1alpha1.DRPlan)
	if !ok {
		return errors.NewBadRequest("unexpected object type")
	}
	plan.DeepCopyInto(p)
	return nil
}

func (r *stubReader) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return nil
}

func makeExecRequest(exec *soteriav1alpha1.DRExecution, op admissionv1.Operation) admission.Request {
	raw, _ := json.Marshal(exec)
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: op,
			Name:      exec.Name,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

func TestDRExecutionValidator_ValidCREATE_Accepted(t *testing.T) {
	tests := []struct {
		name      string
		planPhase string
		mode      soteriav1alpha1.ExecutionMode
	}{
		{
			name:      "planned migration from steady state",
			planPhase: soteriav1alpha1.PhaseSteadyState,
			mode:      soteriav1alpha1.ExecutionModePlannedMigration,
		},
		{
			name:      "reprotect from failed over",
			planPhase: soteriav1alpha1.PhaseFailedOver,
			mode:      soteriav1alpha1.ExecutionModeReprotect,
		},
		{
			name:      "reprotect from failed back",
			planPhase: soteriav1alpha1.PhaseFailedBack,
			mode:      soteriav1alpha1.ExecutionModeReprotect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activeSite := "dc-west"
			switch tt.planPhase {
			case soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.PhaseReprotecting,
				soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.PhaseFailingBack:
				activeSite = "dc-east"
			}
			reader := &stubReader{
				plans: map[string]*soteriav1alpha1.DRPlan{
					"my-plan": {
						ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
						Spec: soteriav1alpha1.DRPlanSpec{
							PrimarySite:   "dc-west",
							SecondarySite: "dc-east",
						},
						Status: soteriav1alpha1.DRPlanStatus{
							Phase:      tt.planPhase,
							ActiveSite: activeSite,
						},
					},
				},
			}

			v := &DRExecutionValidator{reader: reader}
			exec := &soteriav1alpha1.DRExecution{
				ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
				Spec: soteriav1alpha1.DRExecutionSpec{
					PlanName: "my-plan",
					Mode:     tt.mode,
				},
			}

			resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
			if !resp.Allowed {
				t.Errorf("expected allowed, got denied: %v", resp.Result)
			}
		})
	}
}

func TestDRExecutionValidator_MissingPlanName_Denied(t *testing.T) {
	v := &DRExecutionValidator{reader: &stubReader{}}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if resp.Allowed {
		t.Error("expected denied, got allowed")
	}
	if resp.Result == nil || !strings.Contains(resp.Result.Message, "planName") {
		msg := ""
		if resp.Result != nil {
			msg = resp.Result.Message
		}
		t.Errorf("expected message containing 'planName', got %q", msg)
	}
}

func TestDRExecutionValidator_InvalidMode_Denied(t *testing.T) {
	v := &DRExecutionValidator{reader: &stubReader{}}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     "invalid_mode",
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if resp.Allowed {
		t.Error("expected denied, got allowed")
	}
	if resp.Result == nil || !strings.Contains(resp.Result.Message, "invalid_mode") {
		msg := ""
		if resp.Result != nil {
			msg = resp.Result.Message
		}
		t.Errorf("expected message containing 'invalid_mode', got %q", msg)
	}
}

func TestDRExecutionValidator_PlanNotFound_Denied(t *testing.T) {
	v := &DRExecutionValidator{reader: &stubReader{plans: map[string]*soteriav1alpha1.DRPlan{}}}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "nonexistent",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if resp.Allowed {
		t.Error("expected denied, got allowed")
	}
	if resp.Result == nil || !strings.Contains(resp.Result.Message, "not found") {
		msg := ""
		if resp.Result != nil {
			msg = resp.Result.Message
		}
		t.Errorf("expected message containing 'not found', got %q", msg)
	}
}

func TestDRExecutionValidator_PlanInWrongPhase_Denied(t *testing.T) {
	reader := &stubReader{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase:      soteriav1alpha1.PhaseFailedOver,
					ActiveSite: "dc-east",
				},
			},
		},
	}

	v := &DRExecutionValidator{reader: reader}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if resp.Allowed {
		t.Error("expected denied, got allowed")
	}
	msg := ""
	if resp.Result != nil {
		msg = resp.Result.Message
	}
	if !strings.Contains(msg, soteriav1alpha1.PhaseFailedOver) {
		t.Errorf("expected message containing current phase %q, got %q",
			soteriav1alpha1.PhaseFailedOver, msg)
	}
	if !strings.Contains(msg, "SteadyState") {
		t.Errorf("expected message listing valid phases, got %q", msg)
	}
}

func TestDRExecutionValidator_ActiveExecution_Denied(t *testing.T) {
	reader := &stubReader{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase:           soteriav1alpha1.PhaseSteadyState,
					ActiveSite:      "dc-west",
					ActiveExecution: "existing-exec",
				},
			},
		},
	}

	v := &DRExecutionValidator{reader: reader}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "new-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if resp.Allowed {
		t.Error("expected denied when ActiveExecution is set, got allowed")
	}
	msg := ""
	if resp.Result != nil {
		msg = resp.Result.Message
	}
	if !strings.Contains(msg, "existing-exec") {
		t.Errorf("expected message containing active execution name, got %q", msg)
	}
	if !strings.Contains(msg, "concurrent") {
		t.Errorf("expected message mentioning concurrent, got %q", msg)
	}
}

func TestDRExecutionValidator_EmptyActiveExecution_Allowed(t *testing.T) {
	reader := &stubReader{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase:      soteriav1alpha1.PhaseSteadyState,
					ActiveSite: "dc-west",
				},
			},
		},
	}

	v := &DRExecutionValidator{reader: reader}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "new-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if !resp.Allowed {
		t.Errorf("expected allowed when ActiveExecution is empty, got denied: %v", resp.Result)
	}
}

func TestDRExecutionValidator_NonCreateOperation_Allowed(t *testing.T) {
	v := &DRExecutionValidator{reader: &stubReader{}}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Update))
	if !resp.Allowed {
		t.Errorf("expected UPDATE to be allowed, got denied: %v", resp.Result)
	}
}

func TestDRExecutionValidator_RejectWhenSitesOutOfSync(t *testing.T) {
	reader := &stubReader{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase:      soteriav1alpha1.PhaseSteadyState,
					ActiveSite: "dc-west",
					Conditions: []metav1.Condition{
						{
							Type:               "SitesInSync",
							Status:             metav1.ConditionFalse,
							Reason:             "VMsMismatch",
							Message:            "VMs on primary but not secondary: [default/vm-extra]",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
		},
	}

	v := &DRExecutionValidator{reader: reader}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if resp.Allowed {
		t.Error("expected denied when SitesInSync is False, got allowed")
	}
	msg := ""
	if resp.Result != nil {
		msg = resp.Result.Message
	}
	if !strings.Contains(msg, "sites do not agree") {
		t.Errorf("expected message about site disagreement, got %q", msg)
	}
}

func TestDRExecutionValidator_AllowWhenSitesInSync(t *testing.T) {
	reader := &stubReader{
		plans: map[string]*soteriav1alpha1.DRPlan{
			"my-plan": {
				ObjectMeta: metav1.ObjectMeta{Name: "my-plan"},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:   "dc-west",
					SecondarySite: "dc-east",
				},
				Status: soteriav1alpha1.DRPlanStatus{
					Phase:      soteriav1alpha1.PhaseSteadyState,
					ActiveSite: "dc-west",
					Conditions: []metav1.Condition{
						{
							Type:               "SitesInSync",
							Status:             metav1.ConditionTrue,
							Reason:             "VMsAgreed",
							Message:            "Both sites agree on VM inventory",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
		},
	}

	v := &DRExecutionValidator{reader: reader}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "test-exec"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}

	resp := v.Handle(context.Background(), makeExecRequest(exec, admissionv1.Create))
	if !resp.Allowed {
		t.Errorf("expected allowed when SitesInSync is True, got denied: %v", resp.Result)
	}
}
