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
//   - DR lifecycle state machine (statemachine.go): defines the 8-phase symmetric
//     DRPlan lifecycle — 4 rest states (SteadyState, FailedOver, DRedSteadyState,
//     FailedBack) and 4 transition states (FailingOver, Reprotecting, FailingBack,
//     ReprotectingBack). Transition() maps (currentPhase, executionMode) to the
//     target in-progress phase. CompleteTransition() advances in-progress phases to
//     their completion targets. All functions are pure — no mutable state is held;
//     the DRPlan's .status.phase field is the authoritative state.
//
//     Full cycle: SteadyState → FailingOver → FailedOver → Reprotecting →
//     DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState.
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
//     interface abstracts per-group workflow steps; a NoOpHandler (handler_noop.go)
//     enables testing the executor loop without real storage operations. The
//     executor uses fail-forward semantics: a failed DRGroup does not block
//     siblings or subsequent waves. Status updates are serialized via mutex and
//     written to the DRExecution status subresource after each group completes.
//
//   - Unified FailoverHandler (failover.go): implements both planned migration and
//     disaster failover through a single DRGroupHandler driven by FailoverConfig
//     — not the execution mode string. The controller maps mode → config:
//     planned_migration → {GracefulShutdown: true, Force: false}
//     disaster          → {GracefulShutdown: false, Force: true}
//     When GracefulShutdown=true, PreExecute runs Step 0 (stop VMs, stop
//     replication, sync wait) and per-group calls StopReplication+SetSource+StartVM.
//     When GracefulShutdown=false (disaster), PreExecute is a no-op because the
//     origin site is unreachable — no VM stopping, no replication stopping, no sync
//     wait. Per-group execution skips StopReplication and uses SetSource(force=true)
//     to force-promote target volumes. The same handler handles both failover (from
//     SteadyState) and failback (from DRedSteadyState) — direction is encoded in
//     state machine phases, not handler logic.
//
//   - VMManager interface (vm.go): abstracts KubeVirt VM lifecycle control for
//     stopping origin VMs (Step 0) and starting target VMs (per-DRGroup).
//     KubeVirtVMManager patches VirtualMachine.Spec.RunStrategy via merge patch.
//     NoOpVMManager (vm_noop.go) enables testing and dev/CI without KubeVirt.
//
//   - Fail-forward error model (executor.go, failover.go): When a DRGroup fails,
//     the executor records the failure in DRGroupExecutionStatus but continues
//     executing remaining groups and subsequent waves. GroupError provides
//     structured error propagation — handlers return *GroupError{StepName, Target,
//     Err} so the executor can record step-level detail (step name + affected
//     resource) without parsing error strings. Non-GroupError errors fall back to
//     err.Error(). Result computation: all Completed → Succeeded; mixed → Partially
//     Succeeded; no Completed → Failed. CompleteTransition is only called for
//     Succeeded or PartiallySucceeded — Failed leaves the plan in its in-progress
//     phase for manual intervention.
//
//   - DRGroupStatus lifecycle (executor.go): For each DRGroup chunk, the executor
//     creates a cluster-scoped DRGroupStatus resource (named
//     "<executionName>-<groupName>") at the start of group execution with Phase=
//     InProgress. Handlers call StepRecorder.RecordStep() after each operation to
//     append StepStatus entries in real-time. On completion, the executor sets
//     Phase=Completed or Phase=Failed. Owner references on DRGroupStatus point to
//     the parent DRExecution for automatic garbage collection.
//
//   - PVC name resolution (pvc_resolver.go): KubeVirtPVCResolver reads a VM's
//     Spec.Template.Spec.Volumes and extracts PersistentVolumeClaim.ClaimName
//     references. Non-PVC volumes (containerDisk, cloudInitNoCloud) are silently
//     ignored. NoOpPVCResolver returns empty slices for dev/CI without KubeVirt.
//
//   - Per-DRGroup failure events (executor.go): The executor emits a GroupFailed
//     Kubernetes event on the DRExecution when a group fails, and a GroupCompleted
//     event on success. Final execution result events (ExecutionSucceeded,
//     ExecutionPartiallySucceeded, ExecutionFailed) are emitted on completion.
//
// All engine functions are pure or accept interfaces for dependency injection,
// keeping the DRPlan and DRExecution controllers testable at every level.
package engine
