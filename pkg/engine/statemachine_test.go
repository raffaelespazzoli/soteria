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
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestTransition_ValidTransitions(t *testing.T) {
	tests := []struct {
		name         string
		currentPhase string
		mode         soteriav1alpha1.ExecutionMode
		wantPhase    string
	}{
		{
			name:         "SteadyState + planned_migration → FailingOver",
			currentPhase: soteriav1alpha1.PhaseSteadyState,
			mode:         soteriav1alpha1.ExecutionModePlannedMigration,
			wantPhase:    soteriav1alpha1.PhaseFailingOver,
		},
		{
			name:         "SteadyState + disaster → FailingOver",
			currentPhase: soteriav1alpha1.PhaseSteadyState,
			mode:         soteriav1alpha1.ExecutionModeDisaster,
			wantPhase:    soteriav1alpha1.PhaseFailingOver,
		},
		{
			name:         "FailedOver + reprotect → Reprotecting",
			currentPhase: soteriav1alpha1.PhaseFailedOver,
			mode:         soteriav1alpha1.ExecutionModeReprotect,
			wantPhase:    soteriav1alpha1.PhaseReprotecting,
		},
		{
			name:         "DRedSteadyState + planned_migration → FailingBack",
			currentPhase: soteriav1alpha1.PhaseDRedSteadyState,
			mode:         soteriav1alpha1.ExecutionModePlannedMigration,
			wantPhase:    soteriav1alpha1.PhaseFailingBack,
		},
		{
			name:         "DRedSteadyState + disaster → FailingBack",
			currentPhase: soteriav1alpha1.PhaseDRedSteadyState,
			mode:         soteriav1alpha1.ExecutionModeDisaster,
			wantPhase:    soteriav1alpha1.PhaseFailingBack,
		},
		{
			name:         "FailedBack + reprotect → ReprotectingBack",
			currentPhase: soteriav1alpha1.PhaseFailedBack,
			mode:         soteriav1alpha1.ExecutionModeReprotect,
			wantPhase:    soteriav1alpha1.PhaseReprotectingBack,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Transition(tt.currentPhase, tt.mode)
			if err != nil {
				t.Fatalf("Transition() unexpected error: %v", err)
			}
			if got != tt.wantPhase {
				t.Errorf("Transition() = %q, want %q", got, tt.wantPhase)
			}
		})
	}
}

func TestTransition_InvalidTransitions(t *testing.T) {
	allPhases := []string{
		soteriav1alpha1.PhaseSteadyState,
		soteriav1alpha1.PhaseFailingOver,
		soteriav1alpha1.PhaseFailedOver,
		soteriav1alpha1.PhaseReprotecting,
		soteriav1alpha1.PhaseDRedSteadyState,
		soteriav1alpha1.PhaseFailingBack,
		soteriav1alpha1.PhaseFailedBack,
		soteriav1alpha1.PhaseReprotectingBack,
	}
	allModes := []soteriav1alpha1.ExecutionMode{
		soteriav1alpha1.ExecutionModePlannedMigration,
		soteriav1alpha1.ExecutionModeDisaster,
		soteriav1alpha1.ExecutionModeReprotect,
	}

	validSet := map[string]bool{
		"SteadyState/planned_migration":     true,
		"SteadyState/disaster":              true,
		"FailedOver/reprotect":              true,
		"DRedSteadyState/planned_migration": true,
		"DRedSteadyState/disaster":          true,
		"FailedBack/reprotect":              true,
	}

	for _, phase := range allPhases {
		for _, mode := range allModes {
			key := phase + "/" + string(mode)
			if validSet[key] {
				continue
			}
			t.Run(key, func(t *testing.T) {
				_, err := Transition(phase, mode)
				if err == nil {
					t.Fatal("Transition() expected error, got nil")
				}
				if !errors.Is(err, ErrInvalidPhaseTransition) {
					t.Errorf("Transition() error = %v, want ErrInvalidPhaseTransition", err)
				}
			})
		}
	}
}

func TestTransition_UnknownPhase_ReturnsError(t *testing.T) {
	_, err := Transition("Bogus", soteriav1alpha1.ExecutionModePlannedMigration)
	if err == nil {
		t.Fatal("Transition() expected error for unknown phase, got nil")
	}
	if !errors.Is(err, ErrInvalidPhaseTransition) {
		t.Errorf("Transition() error = %v, want ErrInvalidPhaseTransition", err)
	}
}

func TestCompleteTransition_ValidCompletions(t *testing.T) {
	tests := []struct {
		name         string
		currentPhase string
		wantPhase    string
	}{
		{
			name:         "FailingOver → FailedOver",
			currentPhase: soteriav1alpha1.PhaseFailingOver,
			wantPhase:    soteriav1alpha1.PhaseFailedOver,
		},
		{
			name:         "Reprotecting → DRedSteadyState",
			currentPhase: soteriav1alpha1.PhaseReprotecting,
			wantPhase:    soteriav1alpha1.PhaseDRedSteadyState,
		},
		{
			name:         "FailingBack → FailedBack",
			currentPhase: soteriav1alpha1.PhaseFailingBack,
			wantPhase:    soteriav1alpha1.PhaseFailedBack,
		},
		{
			name:         "ReprotectingBack → SteadyState",
			currentPhase: soteriav1alpha1.PhaseReprotectingBack,
			wantPhase:    soteriav1alpha1.PhaseSteadyState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CompleteTransition(tt.currentPhase)
			if err != nil {
				t.Fatalf("CompleteTransition() unexpected error: %v", err)
			}
			if got != tt.wantPhase {
				t.Errorf("CompleteTransition() = %q, want %q", got, tt.wantPhase)
			}
		})
	}
}

func TestCompleteTransition_InvalidPhase_ReturnsError(t *testing.T) {
	nonInProgressPhases := []string{
		soteriav1alpha1.PhaseSteadyState,
		soteriav1alpha1.PhaseFailedOver,
		soteriav1alpha1.PhaseDRedSteadyState,
		soteriav1alpha1.PhaseFailedBack,
		"Unknown",
	}

	for _, phase := range nonInProgressPhases {
		t.Run(phase, func(t *testing.T) {
			_, err := CompleteTransition(phase)
			if err == nil {
				t.Fatal("CompleteTransition() expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidPhaseTransition) {
				t.Errorf("CompleteTransition() error = %v, want ErrInvalidPhaseTransition", err)
			}
		})
	}
}

func TestTransition_ErrorMessage_ContainsPhases(t *testing.T) {
	_, err := Transition(soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.ExecutionModePlannedMigration)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, soteriav1alpha1.PhaseFailedOver) {
		t.Errorf("error message %q does not contain current phase %q", msg, soteriav1alpha1.PhaseFailedOver)
	}
	if !strings.Contains(msg, string(soteriav1alpha1.ExecutionModePlannedMigration)) {
		t.Errorf("error message %q does not contain requested mode %q", msg, soteriav1alpha1.ExecutionModePlannedMigration)
	}
}

func TestValidStartingPhases(t *testing.T) {
	tests := []struct {
		name       string
		mode       soteriav1alpha1.ExecutionMode
		wantPhases []string
	}{
		{
			name:       "planned_migration",
			mode:       soteriav1alpha1.ExecutionModePlannedMigration,
			wantPhases: []string{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.PhaseDRedSteadyState},
		},
		{
			name:       "disaster",
			mode:       soteriav1alpha1.ExecutionModeDisaster,
			wantPhases: []string{soteriav1alpha1.PhaseSteadyState, soteriav1alpha1.PhaseDRedSteadyState},
		},
		{
			name:       "reprotect",
			mode:       soteriav1alpha1.ExecutionModeReprotect,
			wantPhases: []string{soteriav1alpha1.PhaseFailedOver, soteriav1alpha1.PhaseFailedBack},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidStartingPhases(tt.mode)
			sort.Strings(got)
			sort.Strings(tt.wantPhases)
			if len(got) != len(tt.wantPhases) {
				t.Fatalf("ValidStartingPhases() = %v, want %v", got, tt.wantPhases)
			}
			for i := range got {
				if got[i] != tt.wantPhases[i] {
					t.Errorf("ValidStartingPhases()[%d] = %q, want %q", i, got[i], tt.wantPhases[i])
				}
			}
		})
	}
}

func TestTransition_ConcurrentCalls(t *testing.T) {
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([]string, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = Transition(
				soteriav1alpha1.PhaseSteadyState,
				soteriav1alpha1.ExecutionModePlannedMigration,
			)
		}(i)
	}
	wg.Wait()

	for i := range goroutines {
		if errs[i] != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, errs[i])
		}
		if results[i] != soteriav1alpha1.PhaseFailingOver {
			t.Errorf("goroutine %d: got %q, want %q", i, results[i], soteriav1alpha1.PhaseFailingOver)
		}
	}
}

func TestFullLifecycle_8Phases(t *testing.T) {
	// SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState →
	// FailingBack → FailedBack → ReprotectingBack → SteadyState

	phase := soteriav1alpha1.PhaseSteadyState

	// 1. Failover: SteadyState → FailingOver
	next, err := Transition(phase, soteriav1alpha1.ExecutionModeDisaster)
	if err != nil {
		t.Fatalf("Transition(SteadyState, disaster): %v", err)
	}
	if next != soteriav1alpha1.PhaseFailingOver {
		t.Fatalf("Expected FailingOver, got %s", next)
	}
	phase = next

	// 2. Complete failover: FailingOver → FailedOver
	next, err = CompleteTransition(phase)
	if err != nil {
		t.Fatalf("CompleteTransition(FailingOver): %v", err)
	}
	if next != soteriav1alpha1.PhaseFailedOver {
		t.Fatalf("Expected FailedOver, got %s", next)
	}
	phase = next

	// 3. Reprotect: FailedOver → Reprotecting
	next, err = Transition(phase, soteriav1alpha1.ExecutionModeReprotect)
	if err != nil {
		t.Fatalf("Transition(FailedOver, reprotect): %v", err)
	}
	if next != soteriav1alpha1.PhaseReprotecting {
		t.Fatalf("Expected Reprotecting, got %s", next)
	}
	phase = next

	// 4. Complete reprotect: Reprotecting → DRedSteadyState
	next, err = CompleteTransition(phase)
	if err != nil {
		t.Fatalf("CompleteTransition(Reprotecting): %v", err)
	}
	if next != soteriav1alpha1.PhaseDRedSteadyState {
		t.Fatalf("Expected DRedSteadyState, got %s", next)
	}
	phase = next

	// 5. Failback: DRedSteadyState → FailingBack
	next, err = Transition(phase, soteriav1alpha1.ExecutionModePlannedMigration)
	if err != nil {
		t.Fatalf("Transition(DRedSteadyState, planned_migration): %v", err)
	}
	if next != soteriav1alpha1.PhaseFailingBack {
		t.Fatalf("Expected FailingBack, got %s", next)
	}
	phase = next

	// 6. Complete failback: FailingBack → FailedBack
	next, err = CompleteTransition(phase)
	if err != nil {
		t.Fatalf("CompleteTransition(FailingBack): %v", err)
	}
	if next != soteriav1alpha1.PhaseFailedBack {
		t.Fatalf("Expected FailedBack, got %s", next)
	}
	phase = next

	// 7. Restore: FailedBack → ReprotectingBack
	next, err = Transition(phase, soteriav1alpha1.ExecutionModeReprotect)
	if err != nil {
		t.Fatalf("Transition(FailedBack, reprotect): %v", err)
	}
	if next != soteriav1alpha1.PhaseReprotectingBack {
		t.Fatalf("Expected ReprotectingBack, got %s", next)
	}
	phase = next

	// 8. Complete restore: ReprotectingBack → SteadyState
	next, err = CompleteTransition(phase)
	if err != nil {
		t.Fatalf("CompleteTransition(ReprotectingBack): %v", err)
	}
	if next != soteriav1alpha1.PhaseSteadyState {
		t.Fatalf("Expected SteadyState, got %s", next)
	}
}

func TestIsTerminalPhase(t *testing.T) {
	tests := []struct {
		phase string
		want  bool
	}{
		{soteriav1alpha1.PhaseSteadyState, true},
		{soteriav1alpha1.PhaseFailingOver, false},
		{soteriav1alpha1.PhaseFailedOver, true},
		{soteriav1alpha1.PhaseReprotecting, false},
		{soteriav1alpha1.PhaseDRedSteadyState, true},
		{soteriav1alpha1.PhaseFailingBack, false},
		{soteriav1alpha1.PhaseFailedBack, true},
		{soteriav1alpha1.PhaseReprotectingBack, false},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			got := IsTerminalPhase(tt.phase)
			if got != tt.want {
				t.Errorf("IsTerminalPhase(%q) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}
