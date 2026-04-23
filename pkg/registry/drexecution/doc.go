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

// Package drexecution implements the REST storage registry for DRExecution
// resources in the aggregated API server.
//
// # Audit Record Lifecycle
//
// A DRExecution progresses through a one-way lifecycle:
//
//	create → in-progress (StartTime set) → terminal (Result set) → immutable
//
// Once a terminal result is written (Succeeded, Failed, or PartiallySucceeded),
// the record becomes the compliance evidence for that execution. Each record
// contains per-wave, per-DRGroup, per-step status, timestamps, VM names, and
// error messages — sufficient for SOX, ISO 22301, and SOC 2 audits without
// external log lookups.
//
// # Three-Layer Immutability Model
//
//  1. Spec immutability: DRExecutionSpec (planName, mode) is frozen at creation.
//     PrepareForUpdate replaces incoming status with old status on main-resource
//     updates; ValidateUpdate rejects spec changes outright.
//
//  2. Status immutability: StatusStrategy.ValidateUpdate rejects status updates
//     when Result is Succeeded or Failed. PartiallySucceeded is intentionally
//     re-openable to support retry of failed DRGroups (FR14).
//
//  3. Delete protection: AuditProtectedREST blocks deletion of any DRExecution
//     whose Status.Result is non-empty (any completed state). In-progress
//     executions (Result == "") can be deleted for operational cleanup.
//
// # RBAC Design
//
// Operators are restricted to get/list/watch/create on drexecutions — no
// update, patch, or delete. The controller's service account has update/patch
// for status management. Delete is blocked by AuditProtectedREST (a wrapper
// around the generic store) as a defense-in-depth measure for cluster admins
// with broader permissions.
//
// # Plan-Name Label Convention
//
// The DRExecution reconciler sets label soteria.io/plan-name at execution start
// (gated on StartTime == nil). This enables per-plan history queries:
//
//	kubectl get drexecutions -l soteria.io/plan-name=erp-full-stack
//
// # Field Selector Support
//
// GetAttrs exposes spec.planName as a field selector, enabling server-side
// filtering:
//
//	kubectl get drexecutions --field-selector spec.planName=erp-full-stack
package drexecution
