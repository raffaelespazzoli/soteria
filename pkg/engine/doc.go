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

// Package engine implements the DR workflow execution engine. It provides:
//
//   - DR lifecycle state machine (statemachine.go): defines the 6 DRPlan phases
//     (SteadyState, FailingOver, FailedOver, Reprotecting, DRedSteadyState,
//     FailingBack) and validates transitions between them. Transition() maps
//     (currentPhase, executionMode) to the target in-progress phase.
//     CompleteTransition() advances in-progress phases to their completion
//     targets. All functions are pure — no mutable state is held; the DRPlan's
//     .status.phase field is the authoritative state.
//
//   - VM discovery and wave grouping (discovery.go): abstracts Kubernetes API
//     access behind the VMDiscoverer interface, partitions VMs into ordered waves
//     by label value. The production path uses controller-runtime's cached client;
//     unit tests inject mocks.
//
//   - Namespace-level volume consistency resolution (consistency.go): reads
//     namespace annotations (soteria.io/consistency-level) to determine how VM
//     disks are grouped into VolumeGroups. Namespace-level consistency groups all
//     VMs in a namespace into a single VolumeGroup for crash-consistent snapshots.
//     VM-level consistency (the default) creates individual VolumeGroups per VM.
//     Detects wave conflicts when namespace-level VMs span multiple waves.
//
//   - DRGroup chunking (chunker.go): partitions VolumeGroups within each wave
//     into DRGroup chunks respecting maxConcurrentFailovers. Namespace-level
//     VolumeGroups are indivisible units that cannot be split across chunks.
//
//   - Wave executor (executor.go): orchestrates DR execution by running the full
//     discover → group → chunk pipeline at execution time, then executing waves
//     sequentially with concurrent DRGroups within each wave. The DRGroupHandler
//     interface abstracts per-group workflow steps (planned migration, disaster
//     failover); a NoOpHandler (handler_noop.go) enables testing the executor
//     loop without real storage operations. The executor uses fail-forward
//     semantics: a failed DRGroup does not block siblings or subsequent waves.
//     Status updates are serialized via mutex and written to the DRExecution
//     status subresource after each group completes.
//
// All engine functions are pure or accept interfaces for dependency injection,
// keeping the DRPlan and DRExecution controllers testable at every level.
package engine
