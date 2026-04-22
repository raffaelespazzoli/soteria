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
)

func TestValidateDRPlan(t *testing.T) {
	tests := []struct {
		name       string
		plan       *DRPlan
		wantErrors int
		wantFields []string
	}{
		{
			name: "valid plan",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 4,
					PrimarySite:            "dc-west",
					SecondarySite:          "dc-east",
				},
			},
			wantErrors: 0,
		},
		{
			name: "empty waveLabel",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "",
					MaxConcurrentFailovers: 4,
					PrimarySite:            "dc-west",
					SecondarySite:          "dc-east",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.waveLabel"},
		},
		{
			name: "maxConcurrentFailovers zero",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 0,
					PrimarySite:            "dc-west",
					SecondarySite:          "dc-east",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.maxConcurrentFailovers"},
		},
		{
			name: "maxConcurrentFailovers negative",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: -1,
					PrimarySite:            "dc-west",
					SecondarySite:          "dc-east",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.maxConcurrentFailovers"},
		},
		{
			name: "multiple errors: empty waveLabel + maxConcurrent zero",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "",
					MaxConcurrentFailovers: 0,
					PrimarySite:            "dc-west",
					SecondarySite:          "dc-east",
				},
			},
			wantErrors: 2,
			wantFields: []string{"spec.waveLabel", "spec.maxConcurrentFailovers"},
		},
		{
			name: "minimal valid plan",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 2,
					PrimarySite:            "dc-west",
					SecondarySite:          "dc-east",
				},
			},
			wantErrors: 0,
		},
		{
			name: "missing primarySite",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 4,
					PrimarySite:            "",
					SecondarySite:          "dc-east",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.primarySite"},
		},
		{
			name: "missing secondarySite",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 4,
					PrimarySite:            "dc-west",
					SecondarySite:          "",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.secondarySite"},
		},
		{
			name: "primarySite equals secondarySite",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 4,
					PrimarySite:            "dc-west",
					SecondarySite:          "dc-west",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.secondarySite"},
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

func TestValidateDRExecution(t *testing.T) {
	tests := []struct {
		name       string
		exec       *DRExecution
		wantErrors int
		wantFields []string
	}{
		{
			name: "valid planned_migration",
			exec: &DRExecution{
				Spec: DRExecutionSpec{
					PlanName: "my-plan",
					Mode:     ExecutionModePlannedMigration,
				},
			},
			wantErrors: 0,
		},
		{
			name: "valid disaster",
			exec: &DRExecution{
				Spec: DRExecutionSpec{
					PlanName: "my-plan",
					Mode:     ExecutionModeDisaster,
				},
			},
			wantErrors: 0,
		},
		{
			name: "valid reprotect",
			exec: &DRExecution{
				Spec: DRExecutionSpec{
					PlanName: "my-plan",
					Mode:     ExecutionModeReprotect,
				},
			},
			wantErrors: 0,
		},
		{
			name: "empty planName",
			exec: &DRExecution{
				Spec: DRExecutionSpec{
					PlanName: "",
					Mode:     ExecutionModePlannedMigration,
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.planName"},
		},
		{
			name: "invalid mode",
			exec: &DRExecution{
				Spec: DRExecutionSpec{
					PlanName: "my-plan",
					Mode:     "invalid",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.mode"},
		},
		{
			name: "multiple errors: empty planName + invalid mode",
			exec: &DRExecution{
				Spec: DRExecutionSpec{
					PlanName: "",
					Mode:     "bogus",
				},
			},
			wantErrors: 2,
			wantFields: []string{"spec.planName", "spec.mode"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateDRExecution(tt.exec)
			if len(errs) != tt.wantErrors {
				t.Fatalf("ValidateDRExecution() returned %d errors, want %d: %v", len(errs), tt.wantErrors, errs)
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

func TestValidateDRExecutionUpdate(t *testing.T) {
	base := &DRExecution{
		Spec: DRExecutionSpec{
			PlanName: "my-plan",
			Mode:     ExecutionModePlannedMigration,
		},
	}

	t.Run("no spec changes", func(t *testing.T) {
		errs := ValidateDRExecutionUpdate(base, base)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("planName changed", func(t *testing.T) {
		changed := &DRExecution{
			Spec: DRExecutionSpec{PlanName: "other-plan", Mode: ExecutionModePlannedMigration},
		}
		errs := ValidateDRExecutionUpdate(changed, base)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "spec.planName" {
			t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.planName")
		}
	})

	t.Run("mode changed", func(t *testing.T) {
		changed := &DRExecution{
			Spec: DRExecutionSpec{PlanName: "my-plan", Mode: ExecutionModeDisaster},
		}
		errs := ValidateDRExecutionUpdate(changed, base)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "spec.mode" {
			t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.mode")
		}
	})

	t.Run("both changed", func(t *testing.T) {
		changed := &DRExecution{
			Spec: DRExecutionSpec{PlanName: "other", Mode: ExecutionModeDisaster},
		}
		errs := ValidateDRExecutionUpdate(changed, base)
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
		}
	})
}

func TestValidateDRPlanUpdate(t *testing.T) {
	validPlan := &DRPlan{
		Spec: DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}
	invalidPlan := &DRPlan{
		Spec: DRPlanSpec{
			WaveLabel:              "",
			MaxConcurrentFailovers: 0,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
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

	t.Run("primarySite changed", func(t *testing.T) {
		changed := &DRPlan{
			Spec: DRPlanSpec{
				WaveLabel:              "soteria.io/wave",
				MaxConcurrentFailovers: 4,
				PrimarySite:            "dc-north",
				SecondarySite:          "dc-east",
			},
		}
		errs := ValidateDRPlanUpdate(changed, validPlan)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "spec.primarySite" {
			t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.primarySite")
		}
	})

	t.Run("secondarySite changed", func(t *testing.T) {
		changed := &DRPlan{
			Spec: DRPlanSpec{
				WaveLabel:              "soteria.io/wave",
				MaxConcurrentFailovers: 4,
				PrimarySite:            "dc-west",
				SecondarySite:          "dc-south",
			},
		}
		errs := ValidateDRPlanUpdate(changed, validPlan)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "spec.secondarySite" {
			t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.secondarySite")
		}
	})

	t.Run("both sites changed", func(t *testing.T) {
		changed := &DRPlan{
			Spec: DRPlanSpec{
				WaveLabel:              "soteria.io/wave",
				MaxConcurrentFailovers: 4,
				PrimarySite:            "dc-north",
				SecondarySite:          "dc-south",
			},
		}
		errs := ValidateDRPlanUpdate(changed, validPlan)
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
		}
	})
}
