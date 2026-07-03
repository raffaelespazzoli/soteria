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

func TestDRExecutionStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		result ExecutionResult
		want   bool
	}{
		{name: "empty result is not terminal", result: "", want: false},
		{name: "Succeeded is terminal", result: ExecutionResultSucceeded, want: true},
		{name: "Failed is terminal", result: ExecutionResultFailed, want: true},
		{name: "PartiallySucceeded is terminal", result: ExecutionResultPartiallySucceeded, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DRExecutionStatus{Result: tt.result}
			if got := s.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResultToPhase(t *testing.T) {
	tests := []struct {
		name   string
		result ExecutionResult
		want   ExecutionPhase
	}{
		{name: "Succeeded maps to Succeeded", result: ExecutionResultSucceeded, want: ExecutionPhaseSucceeded},
		{name: "PartiallySucceeded maps to PartiallySucceeded", result: ExecutionResultPartiallySucceeded, want: ExecutionPhasePartiallySucceeded},
		{name: "Failed maps to Failed", result: ExecutionResultFailed, want: ExecutionPhaseFailed},
		{name: "empty result maps to Pending", result: "", want: ExecutionPhasePending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResultToPhase(tt.result)
			if got != tt.want {
				t.Errorf("ResultToPhase(%q) = %q, want %q", tt.result, got, tt.want)
			}
		})
	}
}

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
					VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
					MaxConcurrentFailovers:  4,
					PrimarySite:             "dc-west",
					SecondarySite:           "dc-east",
				},
			},
			wantErrors: 0,
		},
		{
			name: "maxConcurrentFailovers zero",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
					MaxConcurrentFailovers:  0,
					PrimarySite:             "dc-west",
					SecondarySite:           "dc-east",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.maxConcurrentFailovers"},
		},
		{
			name: "maxConcurrentFailovers negative",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
					MaxConcurrentFailovers:  -1,
					PrimarySite:             "dc-west",
					SecondarySite:           "dc-east",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.maxConcurrentFailovers"},
		},
		{
			name: "minimal valid plan",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
					MaxConcurrentFailovers:  2,
					PrimarySite:             "dc-west",
					SecondarySite:           "dc-east",
				},
			},
			wantErrors: 0,
		},
		{
			name: "missing primarySite",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
					MaxConcurrentFailovers:  4,
					PrimarySite:             "",
					SecondarySite:           "dc-east",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.primarySite"},
		},
		{
			name: "missing secondarySite",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
					MaxConcurrentFailovers:  4,
					PrimarySite:             "dc-west",
					SecondarySite:           "",
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.secondarySite"},
		},
		{
			name: "primarySite equals secondarySite",
			plan: &DRPlan{
				Spec: DRPlanSpec{
					VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
					MaxConcurrentFailovers:  4,
					PrimarySite:             "dc-west",
					SecondarySite:           "dc-west",
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
			VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
			MaxConcurrentFailovers:  4,
			PrimarySite:             "dc-west",
			SecondarySite:           "dc-east",
		},
	}
	invalidPlan := &DRPlan{
		Spec: DRPlanSpec{
			VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
			MaxConcurrentFailovers:  0,
			PrimarySite:             "dc-west",
			SecondarySite:           "dc-east",
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
				VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
				MaxConcurrentFailovers:  4,
				PrimarySite:             "dc-north",
				SecondarySite:           "dc-east",
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
				VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
				MaxConcurrentFailovers:  4,
				PrimarySite:             "dc-west",
				SecondarySite:           "dc-south",
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
				VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
				MaxConcurrentFailovers:  4,
				PrimarySite:             "dc-north",
				SecondarySite:           "dc-south",
			},
		}
		errs := ValidateDRPlanUpdate(changed, validPlan)
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("volumeReplicationDriver changed", func(t *testing.T) {
		changed := &DRPlan{
			Spec: DRPlanSpec{
				VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "other"},
				MaxConcurrentFailovers:  4,
				PrimarySite:             "dc-west",
				SecondarySite:           "dc-east",
			},
		}
		errs := ValidateDRPlanUpdate(changed, validPlan)
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors (unsupported + immutable), got %d: %v", len(errs), errs)
		}
		wantFields := map[string]bool{
			"spec.volumeReplicationDriver.type": true,
			"spec.volumeReplicationDriver":      true,
		}
		for _, e := range errs {
			if !wantFields[e.Field] {
				t.Errorf("unexpected error.Field = %q", e.Field)
			}
		}
	})

	t.Run("volumeReplicationDriver unchanged", func(t *testing.T) {
		samePlan := &DRPlan{
			Spec: DRPlanSpec{
				VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "noop"},
				MaxConcurrentFailovers:  8,
				PrimarySite:             "dc-west",
				SecondarySite:           "dc-east",
			},
		}
		errs := ValidateDRPlanUpdate(samePlan, validPlan)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})
}

func TestValidateDRPlan_VolumeReplicationDriver_Required(t *testing.T) {
	plan := &DRPlan{
		Spec: DRPlanSpec{
			VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: ""},
			MaxConcurrentFailovers:  4,
			PrimarySite:             "dc-west",
			SecondarySite:           "dc-east",
		},
	}
	errs := ValidateDRPlan(plan)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "spec.volumeReplicationDriver.type" {
		t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.volumeReplicationDriver.type")
	}
}

func TestValidateDRPlan_VolumeReplicationClass_ForbiddenForNoop(t *testing.T) {
	plan := &DRPlan{
		Spec: DRPlanSpec{
			VolumeReplicationDriver: VolumeReplicationDriverConfig{
				Type:                   "noop",
				VolumeReplicationClass: "some-class",
			},
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}
	errs := ValidateDRPlan(plan)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "spec.volumeReplicationDriver.volumeReplicationClass" {
		t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.volumeReplicationDriver.volumeReplicationClass")
	}
	if errs[0].Type != "FieldValueForbidden" {
		t.Errorf("error.Type = %q, want FieldValueForbidden", errs[0].Type)
	}
}

func TestValidateDRPlan_VolumeReplicationClass_RequiredForCSIExtension(t *testing.T) {
	plan := &DRPlan{
		Spec: DRPlanSpec{
			VolumeReplicationDriver: VolumeReplicationDriverConfig{
				Type:                   "csi-extension",
				VolumeReplicationClass: "",
			},
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}
	errs := ValidateDRPlan(plan)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "spec.volumeReplicationDriver.volumeReplicationClass" {
		t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.volumeReplicationDriver.volumeReplicationClass")
	}
	if errs[0].Type != "FieldValueRequired" {
		t.Errorf("error.Type = %q, want FieldValueRequired", errs[0].Type)
	}
}

func TestValidateDRPlanUpdate_VolumeReplicationClass_Immutable(t *testing.T) {
	oldPlan := &DRPlan{
		Spec: DRPlanSpec{
			VolumeReplicationDriver: VolumeReplicationDriverConfig{
				Type:                   "csi-extension",
				VolumeReplicationClass: "class-a",
			},
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}
	newPlan := &DRPlan{
		Spec: DRPlanSpec{
			VolumeReplicationDriver: VolumeReplicationDriverConfig{
				Type:                   "csi-extension",
				VolumeReplicationClass: "class-b",
			},
			MaxConcurrentFailovers: 4,
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
		},
	}
	errs := ValidateDRPlanUpdate(newPlan, oldPlan)
	foundImmutable := false
	for _, e := range errs {
		if e.Field == "spec.volumeReplicationDriver" && e.Type == "FieldValueForbidden" {
			foundImmutable = true
		}
	}
	if !foundImmutable {
		t.Errorf("expected immutability error on spec.volumeReplicationDriver, got: %v", errs)
	}
}

func TestValidateShadowPV(t *testing.T) {
	tests := []struct {
		name       string
		spv        *ShadowPV
		wantErrors int
		wantFields []string
	}{
		{
			name: "valid spec with 2 entries from different clusters",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{
					PVs: []ShadowPVEntry{
						{ClusterName: "east", PVName: "pv-data-1"},
						{ClusterName: "west", PVName: "pv-data-2"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "empty PVs list",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{
					PVs: []ShadowPVEntry{},
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.pvs"},
		},
		{
			name: "nil PVs list",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{},
			},
			wantErrors: 1,
			wantFields: []string{"spec.pvs"},
		},
		{
			name: "empty clusterName",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{
					PVs: []ShadowPVEntry{
						{ClusterName: "", PVName: "pv-data-1"},
					},
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.pvs[0].clusterName"},
		},
		{
			name: "empty pvName",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{
					PVs: []ShadowPVEntry{
						{ClusterName: "east", PVName: ""},
					},
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.pvs[0].pvName"},
		},
		{
			name: "duplicate entries",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{
					PVs: []ShadowPVEntry{
						{ClusterName: "east", PVName: "pv-data-1"},
						{ClusterName: "east", PVName: "pv-data-1"},
					},
				},
			},
			wantErrors: 1,
			wantFields: []string{"spec.pvs[1]"},
		},
		{
			name: "same cluster different pvNames is valid",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{
					PVs: []ShadowPVEntry{
						{ClusterName: "east", PVName: "pv-data-1"},
						{ClusterName: "east", PVName: "pv-data-2"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "different clusters same pvName is valid",
			spv: &ShadowPV{
				Spec: ShadowPVSpec{
					PVs: []ShadowPVEntry{
						{ClusterName: "east", PVName: "pv-data-1"},
						{ClusterName: "west", PVName: "pv-data-1"},
					},
				},
			},
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateShadowPV(tt.spv)
			if len(errs) != tt.wantErrors {
				t.Fatalf("ValidateShadowPV() returned %d errors, want %d: %v", len(errs), tt.wantErrors, errs)
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

func TestValidateShadowPVUpdate(t *testing.T) {
	old := &ShadowPV{
		Spec: ShadowPVSpec{
			PVs: []ShadowPVEntry{
				{ClusterName: "east", PVName: "pv-data-1"},
			},
		},
	}

	t.Run("valid update", func(t *testing.T) {
		updated := &ShadowPV{
			Spec: ShadowPVSpec{
				PVs: []ShadowPVEntry{
					{ClusterName: "east", PVName: "pv-data-1"},
					{ClusterName: "west", PVName: "pv-data-2"},
				},
			},
		}
		errs := ValidateShadowPVUpdate(updated, old)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("invalid update validates new object", func(t *testing.T) {
		invalid := &ShadowPV{
			Spec: ShadowPVSpec{PVs: []ShadowPVEntry{}},
		}
		errs := ValidateShadowPVUpdate(invalid, old)
		if len(errs) == 0 {
			t.Error("expected errors for invalid new ShadowPV, got 0")
		}
	})

	t.Run("drplan label changed", func(t *testing.T) {
		oldLabeled := &ShadowPV{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"soteria.io/drplan": "plan-a"},
			},
			Spec: ShadowPVSpec{
				PVs: []ShadowPVEntry{{ClusterName: "east", PVName: "pv-data-1"}},
			},
		}
		newLabeled := &ShadowPV{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"soteria.io/drplan": "plan-b"},
			},
			Spec: ShadowPVSpec{
				PVs: []ShadowPVEntry{{ClusterName: "east", PVName: "pv-data-1"}},
			},
		}
		errs := ValidateShadowPVUpdate(newLabeled, oldLabeled)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error for drplan label change, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != `metadata.labels[soteria.io/drplan]` {
			t.Errorf("error.Field = %q, want %q", errs[0].Field, `metadata.labels[soteria.io/drplan]`)
		}
	})

	t.Run("drplan label removed", func(t *testing.T) {
		oldLabeled := &ShadowPV{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"soteria.io/drplan": "plan-a"},
			},
			Spec: ShadowPVSpec{
				PVs: []ShadowPVEntry{{ClusterName: "east", PVName: "pv-data-1"}},
			},
		}
		newNoLabel := &ShadowPV{
			Spec: ShadowPVSpec{
				PVs: []ShadowPVEntry{{ClusterName: "east", PVName: "pv-data-1"}},
			},
		}
		errs := ValidateShadowPVUpdate(newNoLabel, oldLabeled)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error for drplan label removal, got %d: %v", len(errs), errs)
		}
	})

	t.Run("drplan label unchanged", func(t *testing.T) {
		oldLabeled := &ShadowPV{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"soteria.io/drplan": "plan-a"},
			},
			Spec: ShadowPVSpec{
				PVs: []ShadowPVEntry{{ClusterName: "east", PVName: "pv-data-1"}},
			},
		}
		newLabeled := &ShadowPV{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"soteria.io/drplan": "plan-a"},
			},
			Spec: ShadowPVSpec{
				PVs: []ShadowPVEntry{
					{ClusterName: "east", PVName: "pv-data-1"},
					{ClusterName: "west", PVName: "pv-data-2"},
				},
			},
		}
		errs := ValidateShadowPVUpdate(newLabeled, oldLabeled)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors when drplan label unchanged, got %d: %v", len(errs), errs)
		}
	})
}

func TestValidateDRPlan_VolumeReplicationDriver_InvalidValue(t *testing.T) {
	plan := &DRPlan{
		Spec: DRPlanSpec{
			VolumeReplicationDriver: VolumeReplicationDriverConfig{Type: "not-a-real-driver"},
			MaxConcurrentFailovers:  4,
			PrimarySite:             "dc-west",
			SecondarySite:           "dc-east",
		},
	}
	errs := ValidateDRPlan(plan)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "spec.volumeReplicationDriver.type" {
		t.Errorf("error.Field = %q, want %q", errs[0].Field, "spec.volumeReplicationDriver.type")
	}
}
