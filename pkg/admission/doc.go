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
// resources. The DRPlan webhook enforces cross-resource constraints that the
// aggregated API server's per-object strategy validation cannot check: VM
// exclusivity across plans, namespace-level wave consistency, and
// maxConcurrentFailovers capacity. The VirtualMachine webhook enforces the
// same exclusivity and wave-consistency constraints from the VM mutation side,
// catching violations when VM labels change rather than when DRPlans change.
// Together, the two webhooks ensure constraints hold regardless of which
// resource is mutated. Per-object field validation is handled by the strategy
// layer (pkg/registry) and is also invoked by the DRPlan webhook as
// defense-in-depth.
package admission
