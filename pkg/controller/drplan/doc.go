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

// Package drplan implements the DRPlan controller for VM auto-discovery and
// wave grouping. It watches DRPlan resources and kubevirt VirtualMachines,
// re-discovering VMs whenever a plan or VM changes. Discovered VMs are grouped
// into execution waves based on a configurable wave label and written to the
// DRPlan's status subresource. The controller uses event-driven reconciliation
// with a safety-net RequeueAfter fallback to ensure eventual consistency.
package drplan
