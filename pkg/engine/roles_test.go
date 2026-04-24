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

package engine

import (
	"testing"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestTargetSiteForPhase(t *testing.T) {
	tests := []struct {
		name    string
		phase   string
		primary string
		second  string
		want    string
	}{
		{"FailingOver targets secondary", soteriav1alpha1.PhaseFailingOver, "east", "west", "west"},
		{"Reprotecting targets secondary", soteriav1alpha1.PhaseReprotecting, "east", "west", "west"},
		{"FailingBack targets primary", soteriav1alpha1.PhaseFailingBack, "east", "west", "east"},
		{"ReprotectingBack targets primary", soteriav1alpha1.PhaseReprotectingBack, "east", "west", "east"},
		{"SteadyState returns empty", soteriav1alpha1.PhaseSteadyState, "east", "west", ""},
		{"FailedOver returns empty", soteriav1alpha1.PhaseFailedOver, "east", "west", ""},
		{"DRedSteadyState returns empty", soteriav1alpha1.PhaseDRedSteadyState, "east", "west", ""},
		{"FailedBack returns empty", soteriav1alpha1.PhaseFailedBack, "east", "west", ""},
		{"empty phase returns empty", "", "east", "west", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TargetSiteForPhase(tt.phase, tt.primary, tt.second)
			if got != tt.want {
				t.Errorf("TargetSiteForPhase(%q) = %q, want %q", tt.phase, got, tt.want)
			}
		})
	}
}

func TestReconcileRole_AllCombinations(t *testing.T) {
	const (
		east = "east"
		west = "west"
	)

	phases := []string{
		soteriav1alpha1.PhaseSteadyState,
		soteriav1alpha1.PhaseFailingOver,
		soteriav1alpha1.PhaseFailedOver,
		soteriav1alpha1.PhaseReprotecting,
		soteriav1alpha1.PhaseDRedSteadyState,
		soteriav1alpha1.PhaseFailingBack,
		soteriav1alpha1.PhaseFailedBack,
		soteriav1alpha1.PhaseReprotectingBack,
	}

	modes := []soteriav1alpha1.ExecutionMode{
		soteriav1alpha1.ExecutionModePlannedMigration,
		soteriav1alpha1.ExecutionModeDisaster,
		soteriav1alpha1.ExecutionModeReprotect,
	}

	sites := []string{east, west}

	tests := []struct {
		phase     string
		mode      soteriav1alpha1.ExecutionMode
		localSite string
		want      Role
	}{
		// FailingOver: target is west (secondary).
		// east→west failover: east is source, west is target.
		{soteriav1alpha1.PhaseFailingOver, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleOwner},
		{soteriav1alpha1.PhaseFailingOver, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleStep0},
		{soteriav1alpha1.PhaseFailingOver, soteriav1alpha1.ExecutionModeDisaster, west, RoleOwner},
		{soteriav1alpha1.PhaseFailingOver, soteriav1alpha1.ExecutionModeDisaster, east, RoleNone},
		{soteriav1alpha1.PhaseFailingOver, soteriav1alpha1.ExecutionModeReprotect, west, RoleOwner},
		{soteriav1alpha1.PhaseFailingOver, soteriav1alpha1.ExecutionModeReprotect, east, RoleNone},

		// Reprotecting: target is west (secondary). No Step 0 for reprotect.
		{soteriav1alpha1.PhaseReprotecting, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleOwner},
		{soteriav1alpha1.PhaseReprotecting, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleNone},
		{soteriav1alpha1.PhaseReprotecting, soteriav1alpha1.ExecutionModeDisaster, west, RoleOwner},
		{soteriav1alpha1.PhaseReprotecting, soteriav1alpha1.ExecutionModeDisaster, east, RoleNone},
		{soteriav1alpha1.PhaseReprotecting, soteriav1alpha1.ExecutionModeReprotect, west, RoleOwner},
		{soteriav1alpha1.PhaseReprotecting, soteriav1alpha1.ExecutionModeReprotect, east, RoleNone},

		// FailingBack: target is east (primary).
		// west→east failback: west is source, east is target.
		{soteriav1alpha1.PhaseFailingBack, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleOwner},
		{soteriav1alpha1.PhaseFailingBack, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleStep0},
		{soteriav1alpha1.PhaseFailingBack, soteriav1alpha1.ExecutionModeDisaster, east, RoleOwner},
		{soteriav1alpha1.PhaseFailingBack, soteriav1alpha1.ExecutionModeDisaster, west, RoleNone},
		{soteriav1alpha1.PhaseFailingBack, soteriav1alpha1.ExecutionModeReprotect, east, RoleOwner},
		{soteriav1alpha1.PhaseFailingBack, soteriav1alpha1.ExecutionModeReprotect, west, RoleNone},

		// ReprotectingBack: target is east (primary). No Step 0.
		{soteriav1alpha1.PhaseReprotectingBack, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleOwner},
		{soteriav1alpha1.PhaseReprotectingBack, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleNone},
		{soteriav1alpha1.PhaseReprotectingBack, soteriav1alpha1.ExecutionModeDisaster, east, RoleOwner},
		{soteriav1alpha1.PhaseReprotectingBack, soteriav1alpha1.ExecutionModeDisaster, west, RoleNone},
		{soteriav1alpha1.PhaseReprotectingBack, soteriav1alpha1.ExecutionModeReprotect, east, RoleOwner},
		{soteriav1alpha1.PhaseReprotectingBack, soteriav1alpha1.ExecutionModeReprotect, west, RoleNone},

		// Rest states: always RoleNone regardless of mode or site.
		{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleNone},
		{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleNone},
		{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModeDisaster, east, RoleNone},
		{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModeDisaster, west, RoleNone},
		{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModeReprotect, east, RoleNone},
		{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.ExecutionModeReprotect, west, RoleNone},

		{soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleNone},
		{soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleNone},
		{soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModeDisaster, east, RoleNone},
		{soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModeDisaster, west, RoleNone},
		{soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModeReprotect, east, RoleNone},
		{soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModeReprotect, west, RoleNone},

		{soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleNone},
		{soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleNone},
		{soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModeDisaster, east, RoleNone},
		{soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModeDisaster, west, RoleNone},
		{soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModeReprotect, east, RoleNone},
		{soteriav1alpha1.PhaseDRedSteadyState, soteriav1alpha1.ExecutionModeReprotect, west, RoleNone},

		{soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModePlannedMigration, east, RoleNone},
		{soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModePlannedMigration, west, RoleNone},
		{soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModeDisaster, east, RoleNone},
		{soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModeDisaster, west, RoleNone},
		{soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModeReprotect, east, RoleNone},
		{soteriav1alpha1.PhaseFailedBack, soteriav1alpha1.ExecutionModeReprotect, west, RoleNone},
	}

	for _, tt := range tests {
		t.Run(tt.phase+"/"+string(tt.mode)+"/"+tt.localSite, func(t *testing.T) {
			got := ReconcileRole(tt.phase, tt.mode, tt.localSite, east, west)
			if got != tt.want {
				t.Errorf("ReconcileRole(%q, %q, %q, east, west) = %v, want %v",
					tt.phase, tt.mode, tt.localSite, got, tt.want)
			}
		})
	}

	// Verify every phase x mode x site combination is explicitly covered.
	covered := make(map[string]bool)
	for _, tt := range tests {
		key := tt.phase + "/" + string(tt.mode) + "/" + tt.localSite
		covered[key] = true
	}
	for _, phase := range phases {
		for _, mode := range modes {
			for _, site := range sites {
				key := phase + "/" + string(mode) + "/" + site
				if !covered[key] {
					t.Errorf("combination not covered: %s", key)
				}
			}
		}
	}
}

func TestReconcileRole_EmptyPhase(t *testing.T) {
	got := ReconcileRole("", soteriav1alpha1.ExecutionModePlannedMigration, "east", "east", "west")
	if got != RoleNone {
		t.Errorf("empty phase should return RoleNone, got %v", got)
	}
}

func TestRole_String(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{RoleNone, "None"},
		{RoleOwner, "Owner"},
		{RoleStep0, "Step0"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.role.String(); got != tt.want {
				t.Errorf("Role(%d).String() = %q, want %q", tt.role, got, tt.want)
			}
		})
	}
}
