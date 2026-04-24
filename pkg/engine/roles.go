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
// roles.go provides pure functions for site-aware reconcile ownership.
// During a DR transition, exactly one site owns the execution (the target
// site — the one becoming active). In planned migration mode, the source
// site has a limited Step 0 role (stop VMs, stop replication, sync wait)
// before handing off to the target. In disaster mode, the source site does
// nothing (it may be down). These functions are stateless and fully testable
// with table-driven tests — no API calls or side effects.

package engine

import (
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// Role represents the reconciler's responsibility for a given DRExecution.
type Role int

const (
	// RoleNone means this site has no work to do for the current transition.
	RoleNone Role = iota
	// RoleOwner means this site owns the execution: runs waves (SetSource,
	// StartVM, WaitVMReady) or re-protect operations.
	RoleOwner
	// RoleStep0 means this site runs Step 0 only (planned migration source):
	// stop VMs, stop replication, wait for sync, then set Step0Complete.
	RoleStep0
)

// String returns a human-readable representation for logging.
func (r Role) String() string {
	switch r {
	case RoleOwner:
		return "Owner"
	case RoleStep0:
		return "Step0"
	default:
		return "None"
	}
}

// TargetSiteForPhase returns the site that is becoming active during a
// transition phase. This is the inverse of ActiveSiteForPhase (which returns
// the currently active site). During FailingOver/Reprotecting the target is
// secondarySite; during FailingBack/ReprotectingBack the target is primarySite.
// Returns empty string for rest states (no transition in progress).
func TargetSiteForPhase(phase, primarySite, secondarySite string) string {
	switch phase {
	case soteriav1alpha1.PhaseFailingOver,
		soteriav1alpha1.PhaseReprotecting:
		return secondarySite
	case soteriav1alpha1.PhaseFailingBack,
		soteriav1alpha1.PhaseReprotectingBack:
		return primarySite
	default:
		return ""
	}
}

// ReconcileRole computes the reconciler's role for a DRExecution based on
// the current transition phase, execution mode, and site topology.
//
// The target site (becoming active) is always the Owner. The source site
// gets RoleStep0 only in planned_migration mode (for FailingOver and
// FailingBack phases). In all other cases (disaster mode, reprotect
// phases, rest states), the non-target site gets RoleNone.
func ReconcileRole(
	phase string,
	mode soteriav1alpha1.ExecutionMode,
	localSite, primarySite, secondarySite string,
) Role {
	target := TargetSiteForPhase(phase, primarySite, secondarySite)

	// Rest states have no target — no execution work to do.
	if target == "" {
		return RoleNone
	}

	if localSite == target {
		return RoleOwner
	}

	// Source site gets Step 0 only during failover/failback in planned migration.
	if mode == soteriav1alpha1.ExecutionModePlannedMigration {
		switch phase {
		case soteriav1alpha1.PhaseFailingOver,
			soteriav1alpha1.PhaseFailingBack:
			return RoleStep0
		}
	}

	return RoleNone
}
