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

// Package admission implements validating admission webhooks for Soteria
// resources. The DRPlan webhook validates field-level constraints
// (maxConcurrentFailovers, site names) as defense-in-depth alongside the aggregated API
// server's strategy layer. The VirtualMachine webhook validates plan existence
// (issuing a warning when the referenced DRPlan is missing, to support GitOps
// ordering) and namespace-level wave consistency (rejecting VMs whose wave
// label conflicts with siblings in the same namespace-level namespace).
// VM exclusivity is structurally guaranteed by the soteria.io/drplan label
// convention — a label key can have only one value, so a VM belongs to at most
// one plan. Throttle capacity (maxConcurrentFailovers vs group size) is
// enforced by the controller's reconciliation loop via Ready=False status
// conditions, not at admission time.
package admission
