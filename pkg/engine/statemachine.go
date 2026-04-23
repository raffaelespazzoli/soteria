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

// Tier 2 – Architecture:
// statemachine.go implements the DR lifecycle state machine as pure functions.
// The DRPlan's .status.phase is the authoritative state; these functions
// validate whether a requested transition is legal and return the target phase.
// No mutable state is held — the controller reads current phase from the API,
// calls Transition or CompleteTransition, and writes the result back.
//
// 8-phase symmetric lifecycle (4 rest states, 4 transition states):
//
//   SteadyState ──(planned_migration|disaster)──► FailingOver ──(complete)──► FailedOver
//   FailedOver ──(reprotect)──► Reprotecting ──(complete)──► DRedSteadyState
//   DRedSteadyState ──(planned_migration|disaster)──► FailingBack ──(complete)──► FailedBack
//   FailedBack ──(reprotect)──► ReprotectingBack ──(complete)──► SteadyState

package engine

import (
	"errors"
	"fmt"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// ErrInvalidPhaseTransition is returned when a requested state transition
// is not allowed by the DR lifecycle state machine.
var ErrInvalidPhaseTransition = errors.New("invalid phase transition")

// validTransitions maps (currentPhase, executionMode) → target in-progress phase.
var validTransitions = map[string]map[soteriav1alpha1.ExecutionMode]string{
	soteriav1alpha1.PhaseSteadyState: {
		soteriav1alpha1.ExecutionModePlannedMigration: soteriav1alpha1.PhaseFailingOver,
		soteriav1alpha1.ExecutionModeDisaster:         soteriav1alpha1.PhaseFailingOver,
	},
	soteriav1alpha1.PhaseFailedOver: {
		soteriav1alpha1.ExecutionModeReprotect: soteriav1alpha1.PhaseReprotecting,
	},
	soteriav1alpha1.PhaseDRedSteadyState: {
		soteriav1alpha1.ExecutionModePlannedMigration: soteriav1alpha1.PhaseFailingBack,
		soteriav1alpha1.ExecutionModeDisaster:         soteriav1alpha1.PhaseFailingBack,
	},
	soteriav1alpha1.PhaseFailedBack: {
		soteriav1alpha1.ExecutionModeReprotect: soteriav1alpha1.PhaseReprotectingBack,
	},
}

// completionTransitions maps an in-progress phase to its completion target.
var completionTransitions = map[string]string{
	soteriav1alpha1.PhaseFailingOver:      soteriav1alpha1.PhaseFailedOver,
	soteriav1alpha1.PhaseReprotecting:     soteriav1alpha1.PhaseDRedSteadyState,
	soteriav1alpha1.PhaseFailingBack:      soteriav1alpha1.PhaseFailedBack,
	soteriav1alpha1.PhaseReprotectingBack: soteriav1alpha1.PhaseSteadyState,
}

// terminalPhases are rest states where no execution is in progress.
var terminalPhases = map[string]bool{
	soteriav1alpha1.PhaseSteadyState:     true,
	soteriav1alpha1.PhaseFailedOver:      true,
	soteriav1alpha1.PhaseDRedSteadyState: true,
	soteriav1alpha1.PhaseFailedBack:      true,
}

// Transition validates whether the requested execution mode is legal given the
// current phase and returns the target in-progress phase.
func Transition(currentPhase string, mode soteriav1alpha1.ExecutionMode) (string, error) {
	targets, ok := validTransitions[currentPhase]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from phase %q", ErrInvalidPhaseTransition, mode, currentPhase)
	}
	target, ok := targets[mode]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from phase %q", ErrInvalidPhaseTransition, mode, currentPhase)
	}
	return target, nil
}

// CompleteTransition advances an in-progress phase to its completion target.
func CompleteTransition(currentPhase string) (string, error) {
	target, ok := completionTransitions[currentPhase]
	if !ok {
		return "", fmt.Errorf("%w: phase %q is not an in-progress phase", ErrInvalidPhaseTransition, currentPhase)
	}
	return target, nil
}

// ValidStartingPhases returns the phases that accept the given execution mode.
func ValidStartingPhases(mode soteriav1alpha1.ExecutionMode) []string {
	var phases []string
	for phase, modes := range validTransitions {
		if _, ok := modes[mode]; ok {
			phases = append(phases, phase)
		}
	}
	return phases
}

// IsTerminalPhase returns true for rest phases where no execution is
// in progress (SteadyState, FailedOver, DRedSteadyState, FailedBack).
func IsTerminalPhase(phase string) bool {
	return terminalPhases[phase]
}

// EffectivePhase derives the transient phase from a rest state and the active
// execution mode. When mode is empty (idle), returns the rest phase unchanged.
// When mode is non-empty, looks up the transient phase via validTransitions.
// Returns restPhase if the combination is unknown.
func EffectivePhase(restPhase string, activeExecMode soteriav1alpha1.ExecutionMode) string {
	if activeExecMode == "" {
		return restPhase
	}
	modes, ok := validTransitions[restPhase]
	if !ok {
		return restPhase
	}
	transient, ok := modes[activeExecMode]
	if !ok {
		return restPhase
	}
	return transient
}

// RestStateAfterCompletion chains Transition + CompleteTransition to go directly
// from a rest state to the next rest state given an execution mode. This avoids
// exposing transient phases outside the state machine — callers never need to
// know the intermediate transient phase.
func RestStateAfterCompletion(currentRestPhase string, mode soteriav1alpha1.ExecutionMode) (string, error) {
	transient, err := Transition(currentRestPhase, mode)
	if err != nil {
		return "", err
	}
	return CompleteTransition(transient)
}

// ActiveSiteForPhase returns the expected activeSite for a given lifecycle
// phase. Phases where workloads reside on the primary site return primarySite;
// phases where workloads reside on the secondary site return secondarySite.
func ActiveSiteForPhase(phase, primarySite, secondarySite string) string {
	switch phase {
	case soteriav1alpha1.PhaseSteadyState,
		soteriav1alpha1.PhaseFailingOver,
		soteriav1alpha1.PhaseFailedBack,
		soteriav1alpha1.PhaseReprotectingBack:
		return primarySite
	case soteriav1alpha1.PhaseFailedOver,
		soteriav1alpha1.PhaseReprotecting,
		soteriav1alpha1.PhaseDRedSteadyState,
		soteriav1alpha1.PhaseFailingBack:
		return secondarySite
	default:
		return ""
	}
}
