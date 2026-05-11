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

// Package admission implements admission validation for Soteria resources.
//
// DRPlan and DRExecution validation runs in-process within the aggregated API
// server via SoteriaAdmissionPlugin (plugin.go), which implements
// k8s.io/apiserver/pkg/admission.ValidationInterface. The plugin performs
// DRExecution CREATE cross-object checks (plan existence, concurrency gate,
// phase transition, SitesInSync) using direct REST storage lookups, and
// DRPlan CREATE/UPDATE field validation as defense-in-depth alongside the
// registry strategy layer.
//
// The DRExecution concurrency guard uses a three-layer model:
//  1. Admission gate (this package): best-effort LIST of DRExecutions
//     with the soteria.io/plan-name label; rejects if any non-terminal
//     execution exists.
//  2. SERIAL INSERT: DRExecution storage creates use gocql.Serial
//     consistency for cross-DC Paxos ordering on INSERT IF NOT EXISTS.
//  3. Reconciler exclusivity check: verifyExclusiveExecution lists
//     DRExecutions with ScyllaRetry backoff; self-fails if a competing
//     non-terminal execution is found.
//
// This three-layer model replaced the former plan-status concurrency pointer,
// decoupling DRPlan status from execution lifecycle.
//
// The VirtualMachine webhook remains on the controller-runtime webhook server
// path (ValidatingWebhookConfiguration vvm.kb.io) because VMs are standard
// KubeVirt CRDs served by kube-apiserver, not the aggregated API. It validates
// plan existence (issuing a warning when the referenced DRPlan is missing, to
// support GitOps ordering) and namespace-level wave consistency (rejecting VMs
// whose wave label conflicts with siblings in the same namespace).
//
// VM exclusivity is structurally guaranteed by the soteria.io/drplan label
// convention — a label key can have only one value, so a VM belongs to at most
// one plan. Throttle capacity (maxConcurrentFailovers vs group size) is
// enforced by the controller's reconciliation loop via Ready=False status
// conditions, not at admission time.
package admission
