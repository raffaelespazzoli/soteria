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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

type mockVMDiscoverer struct {
	vms map[string][]engine.VMReference
	err error
}

func (m *mockVMDiscoverer) DiscoverVMs(_ context.Context, planName string) ([]engine.VMReference, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.vms[planName], nil
}

type mockNSLookup struct {
	levels map[string]soteriav1alpha1.ConsistencyLevel
	err    error
}

func (m *mockNSLookup) GetConsistencyLevel(_ context.Context, ns string) (soteriav1alpha1.ConsistencyLevel, error) {
	if m.err != nil {
		return "", m.err
	}
	if level, ok := m.levels[ns]; ok {
		return level, nil
	}
	return soteriav1alpha1.ConsistencyLevelVM, nil
}

func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	return s
}

func makeRequest(plan *soteriav1alpha1.DRPlan, op admissionv1.Operation) admission.Request {
	raw, _ := json.Marshal(plan)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: op,
			Name:      plan.Name,
			Namespace: plan.Namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
	if op == admissionv1.Update {
		req.OldObject = runtime.RawExtension{Raw: raw}
	}
	return req
}

func TestDRPlanValidator_FieldValidation(t *testing.T) {
	tests := []struct {
		name        string
		plan        *soteriav1alpha1.DRPlan
		op          admissionv1.Operation
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "valid plan CREATE — allowed",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-ok"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: 4,
					PrimarySite: "dc-west", SecondarySite: "dc-east",
				},
			},
			op:          admissionv1.Create,
			wantAllowed: true,
		},
		{
			name: "missing waveLabel — denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-no-wave"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "", MaxConcurrentFailovers: 4,
					PrimarySite: "dc-west", SecondarySite: "dc-east",
				},
			},
			op:          admissionv1.Create,
			wantAllowed: false,
			wantMessage: "waveLabel",
		},
		{
			name: "invalid maxConcurrentFailovers — denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-bad-max"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: 0,
					PrimarySite: "dc-west", SecondarySite: "dc-east",
				},
			},
			op:          admissionv1.Create,
			wantAllowed: false,
			wantMessage: "maxConcurrentFailovers",
		},
		{
			name: "missing primarySite — denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-no-primary"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: 4,
					PrimarySite: "", SecondarySite: "dc-east",
				},
			},
			op:          admissionv1.Create,
			wantAllowed: false,
			wantMessage: "primarySite",
		},
		{
			name: "missing secondarySite — denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-no-secondary"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: 4,
					PrimarySite: "dc-west", SecondarySite: "",
				},
			},
			op:          admissionv1.Create,
			wantAllowed: false,
			wantMessage: "secondarySite",
		},
		{
			name: "equal sites — denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-same-site"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: 4,
					PrimarySite: "dc-west", SecondarySite: "dc-west",
				},
			},
			op:          admissionv1.Create,
			wantAllowed: false,
			wantMessage: "secondarySite",
		},
		{
			name: "valid plan UPDATE — allowed",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-update"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: 8,
					PrimarySite: "dc-west", SecondarySite: "dc-east",
				},
			},
			op:          admissionv1.Update,
			wantAllowed: true,
		},
		{
			name: "invalid UPDATE (zero maxConcurrent) — denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-bad-update"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: -1,
					PrimarySite: "dc-west", SecondarySite: "dc-east",
				},
			},
			op:          admissionv1.Update,
			wantAllowed: false,
			wantMessage: "maxConcurrentFailovers",
		},
		{
			name: "DELETE operation — allowed",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-del"},
				Spec: soteriav1alpha1.DRPlanSpec{
					WaveLabel: "wave", MaxConcurrentFailovers: 4,
					PrimarySite: "dc-west", SecondarySite: "dc-east",
				},
			},
			op:          admissionv1.Delete,
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildScheme()
			v := &DRPlanValidator{
				decoder: admission.NewDecoder(scheme),
			}

			resp := v.Handle(context.Background(), makeRequest(tt.plan, tt.op))
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v; result: %v", resp.Allowed, tt.wantAllowed, resp.Result)
			}
			if tt.wantMessage != "" && (resp.Result == nil || !strings.Contains(resp.Result.Message, tt.wantMessage)) {
				msg := ""
				if resp.Result != nil {
					msg = resp.Result.Message
				}
				t.Errorf("expected message containing %q, got %q", tt.wantMessage, msg)
			}
		})
	}
}

func TestDRPlanValidator_SiteImmutability(t *testing.T) {
	scheme := buildScheme()
	v := &DRPlanValidator{decoder: admission.NewDecoder(scheme)}

	oldPlan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-immutable"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel: "wave", MaxConcurrentFailovers: 4,
			PrimarySite: "dc-west", SecondarySite: "dc-east",
		},
	}
	newPlan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-immutable"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel: "wave", MaxConcurrentFailovers: 4,
			PrimarySite: "dc-north", SecondarySite: "dc-east",
		},
	}

	oldRaw, _ := json.Marshal(oldPlan)
	newRaw, _ := json.Marshal(newPlan)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Name:      "plan-immutable",
			Object:    runtime.RawExtension{Raw: newRaw},
			OldObject: runtime.RawExtension{Raw: oldRaw},
		},
	}

	resp := v.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected denial when primarySite changes on update")
	}
	if resp.Result == nil || !strings.Contains(resp.Result.Message, "primarySite") {
		msg := ""
		if resp.Result != nil {
			msg = resp.Result.Message
		}
		t.Errorf("expected message containing 'primarySite', got %q", msg)
	}
}
