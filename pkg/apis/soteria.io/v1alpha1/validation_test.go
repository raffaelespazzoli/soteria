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

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateDRPlan(t *testing.T) {
	tests := []struct {
		name       string
		plan       *DRPlan
		wantErrors int
		wantFields []string
	}{
		{
			name: "valid plan with matchLabels",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 4,
				},
			},
			wantErrors: 0,
		},
		{
			name: "valid plan with matchExpressions",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod"}},
						},
					},
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 2,
				},
			},
			wantErrors: 0,
		},
		{
			name: "invalid vmSelector matchExpressions operator",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "env", Operator: "InvalidOp", Values: []string{"prod"}},
						},
					},
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 4,
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.vmSelector"},
		},
		{
			name: "empty vmSelector — no matchLabels or matchExpressions",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector:             metav1.LabelSelector{},
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 4,
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.vmSelector"},
		},
		{
			name: "empty waveLabel",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
					WaveLabel:              "",
					MaxConcurrentFailovers: 4,
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.waveLabel"},
		},
		{
			name: "maxConcurrentFailovers zero",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 0,
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.maxConcurrentFailovers"},
		},
		{
			name: "maxConcurrentFailovers negative",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: -1,
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.maxConcurrentFailovers"},
		},
		{
			name: "multiple errors: empty waveLabel + maxConcurrent zero",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
					WaveLabel:              "",
					MaxConcurrentFailovers: 0,
				},
			},
			wantErrors: 2,
			wantFields: []string{"spec.waveLabel", "spec.maxConcurrentFailovers"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateDRPlan(tt.plan)

			if len(errs) != tt.wantErrors {
				t.Fatalf("ValidateDRPlan() returned %d errors, want %d: %v", len(errs), tt.wantErrors, errs)
			}

			for i, wantField := range tt.wantFields {
				if i >= len(errs) {
					break
				}
				if errs[i].Field != wantField {
					t.Errorf("error[%d].Field = %q, want %q", i, errs[i].Field, wantField)
				}
			}
		})
	}
}

func TestValidateDRPlanUpdate(t *testing.T) {
	validPlan := &DRPlan{
		Spec: DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
		},
	}
	invalidPlan := &DRPlan{
		Spec: DRPlanSpec{
			VMSelector:             metav1.LabelSelector{},
			WaveLabel:              "",
			MaxConcurrentFailovers: 0,
		},
	}

	t.Run("valid update", func(t *testing.T) {
		errs := ValidateDRPlanUpdate(validPlan, validPlan)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("invalid update validates new object", func(t *testing.T) {
		errs := ValidateDRPlanUpdate(invalidPlan, validPlan)
		if len(errs) == 0 {
			t.Error("expected errors for invalid new plan, got 0")
		}
	})
}
