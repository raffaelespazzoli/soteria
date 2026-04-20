# Story 4.3: Planned Migration Workflow

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to execute a planned migration that gracefully stops origin VMs, waits for final replication sync, then promotes and starts VMs wave by wave with zero data loss,
So that I can migrate workloads during maintenance windows.

## Acceptance Criteria

1. **AC1 — Planned migration handler:** `pkg/engine/planned.go` implements the `DRGroupHandler` interface (from Story 4.2) with a `PlannedMigrationHandler`. The per-DRGroup execution calls `StopReplication` (force=false) on each volume group in the group, then `SetSource` (force=false) on each volume group, then starts the target VMs via a `VMManager` interface. RPO=0 is guaranteed because replication is stopped before promotion (FR9).

2. **AC2 — Step 0 pre-execution phase:** `PlannedMigrationHandler` exposes a `PreExecute(ctx, groups []ExecutionGroup) error` method that runs before the wave executor starts. Step 0 stops all origin VMs (graceful shutdown via `VMManager.StopVM`), calls `StopReplication(force=false)` on all volume groups across all waves, and polls `GetReplicationStatus` until all volume groups report `HealthHealthy` or `RoleNonReplicated` — ensuring the final replication sync is complete before promotion begins.

3. **AC3 — VMManager interface:** `pkg/engine/vm.go` defines a `VMManager` interface with `StopVM(ctx, name, namespace string) error`, `StartVM(ctx, name, namespace string) error`, and `IsVMRunning(ctx, name, namespace string) (bool, error)`. A `KubeVirtVMManager` implementation in the same file uses a `client.Client` to patch `VirtualMachine.Spec.RunStrategy` to `Halted` (stop) or `Always` (start). Idempotent: `StopVM` returns nil if already stopped; `StartVM` returns nil if already running.

4. **AC4 — Sync wait with timeout and polling:** The Step 0 sync wait polls `GetReplicationStatus` at a configurable interval (default 2s) with a configurable timeout (default 10m). If the timeout expires before all volume groups reach sync completion, Step 0 returns an error and the execution is marked `Failed`. The polling uses structured logging at V(1) for each poll iteration.

5. **AC5 — Origin unreachable during Step 0:** When `StopVM` or `StopReplication` returns an error during Step 0 (e.g., origin site unreachable), the execution is marked `Failed` with a descriptive error indicating which origin VM or volume group was unreachable. The operator can then re-attempt as planned migration (if origin recovers) or switch to disaster mode. DRPlan phase is not advanced on failure.

6. **AC6 — Per-DRGroup execution steps:** For each DRGroup within a wave, `ExecuteGroup` executes these steps in order: (a) `StopReplication(force=false)` on each volume group, (b) `SetSource(force=false)` on each volume group, (c) `StartVM` for each VM in the group. Each step is recorded as a `StepStatus` entry. If any step fails, the DRGroup is marked `Failed` with the step name and error.

7. **AC7 — Per-step status recording:** Each operation within a DRGroup is recorded in the DRGroupStatus as a `StepStatus` entry with `Name` (e.g., "StopReplication", "SetSource", "StartVM"), `Status` ("Succeeded"/"Failed"), `Message`, and `Timestamp`. This provides visibility into exactly which step failed for troubleshooting.

8. **AC8 — Controller integration:** The DRExecution controller dispatches `PlannedMigrationHandler` when `exec.Spec.Mode == "planned_migration"`. After Story 4.2's executor validates and transitions the plan, the controller calls `handler.PreExecute` (Step 0), then passes the handler to the wave executor. If `PreExecute` fails, the execution is marked `Failed` without running any waves.

9. **AC9 — No-op VMManager for testing:** `pkg/engine/vm_noop.go` provides a `NoOpVMManager` that returns nil for all operations. Used in unit tests and dev/CI environments where KubeVirt is not available.

10. **AC10 — Unit tests:** `pkg/engine/planned_test.go` has table-driven tests covering: (a) successful planned migration — Step 0 completes, all DRGroups succeed, VMs started; (b) StopReplication failure in Step 0 — execution fails, no waves run; (c) origin VM unreachable in Step 0 — execution fails with descriptive error; (d) sync timeout — execution fails; (e) SetSource failure in per-DRGroup step — group fails, others continue; (f) StartVM failure — group fails; (g) per-step status recording — all steps recorded with correct names and timestamps; (h) empty DRGroup — no driver calls, succeeds immediately. Tests use fake driver (`pkg/drivers/fake/`) and `NoOpVMManager` or mock `VMManager`.

## Tasks / Subtasks

- [x] Task 1: Define VMManager interface (AC: #3, #9)
  - [x] 1.1 Create `pkg/engine/vm.go` with copyright header and package doc block comment
  - [x] 1.2 Define `VMManager` interface: `StopVM(ctx context.Context, name, namespace string) error`, `StartVM(ctx context.Context, name, namespace string) error`, `IsVMRunning(ctx context.Context, name, namespace string) (bool, error)`
  - [x] 1.3 Implement `KubeVirtVMManager` struct with `Client client.Client` field
  - [x] 1.4 `StopVM`: fetch `kubevirtv1.VirtualMachine`, check if already halted (idempotent), patch `Spec.RunStrategy` to `kubevirtv1.RunStrategyHalted`. Use a merge patch (not strategic merge — external KubeVirt types). Log at V(1): `"Stopping VM"` with name/namespace
  - [x] 1.5 `StartVM`: fetch `kubevirtv1.VirtualMachine`, check if already running (idempotent), patch `Spec.RunStrategy` to `kubevirtv1.RunStrategyAlways`. Log at V(1): `"Starting VM"` with name/namespace
  - [x] 1.6 `IsVMRunning`: fetch VM, return `true` if `Spec.RunStrategy` is `Always` or `RerunOnFailure`
  - [x] 1.7 Create `pkg/engine/vm_noop.go` with `NoOpVMManager` returning nil for all methods

- [x] Task 2: Implement planned migration handler (AC: #1, #6, #7)
  - [x] 2.1 Create `pkg/engine/planned.go` with copyright header and Tier 2 architecture block comment explaining the planned migration workflow: Step 0 (global pre-execution) + per-DRGroup handler
  - [x] 2.2 Define `PlannedMigrationHandler` struct with fields: `Driver drivers.StorageProvider`, `VMManager VMManager`, `SyncPollInterval time.Duration`, `SyncTimeout time.Duration`
  - [x] 2.3 Implement `ExecuteGroup(ctx context.Context, group ExecutionGroup) error` — the per-DRGroup handler: (a) for each VolumeGroup: call `StopReplication(force=false)` via driver, record StepStatus; (b) for each VolumeGroup: call `SetSource(force=false)` via driver, record StepStatus; (c) for each VM: call `StartVM`, record StepStatus
  - [x] 2.4 Return first error encountered (driver or VMManager error), with error message including the step name and volume group/VM name for diagnostics
  - [x] 2.5 Define step name constants: `StepStopReplication = "StopReplication"`, `StepSetSource = "SetSource"`, `StepStartVM = "StartVM"`

- [x] Task 3: Implement Step 0 pre-execution (AC: #2, #4, #5)
  - [x] 3.1 Implement `PreExecute(ctx context.Context, groups []ExecutionGroup) error` on `PlannedMigrationHandler`
  - [x] 3.2 Phase 1 — Stop all origin VMs: iterate all groups, collect unique VM names (deduplicate across groups), call `VMManager.StopVM` for each. On error, return immediately with descriptive error: `"stopping origin VM %s/%s: %w"`
  - [x] 3.3 Phase 2 — Stop replication on all volume groups: iterate all groups, call `driver.StopReplication(ctx, vgID, StopReplicationOptions{Force: false})` for each VolumeGroup. On error, return: `"stopping replication for volume group %s: %w"`
  - [x] 3.4 Phase 3 — Wait for sync completion: poll `driver.GetReplicationStatus` for each volume group at `SyncPollInterval` (default 2s). A volume group is "synced" when `Role == RoleNonReplicated` or `Health == HealthHealthy`. Use `time.NewTicker` and `ctx.Done()` for cancellation-aware polling. If timeout expires, return: `"sync timeout: %d of %d volume groups not synced after %v"`
  - [x] 3.5 Log progress at V(1) during sync wait: `"Polling replication status"` with `synced`/`total` counts

- [x] Task 4: Integrate Volume Group ID resolution (AC: #1, #6)
  - [x] 4.1 Implemented `resolveVolumeGroupID` helper using idempotent `CreateVolumeGroup`
  - [x] 4.2 Cache resolved VolumeGroupIDs within the handler execution via `vgIDCache` map
  - [x] 4.3 If `CreateVolumeGroup` fails, return error: `"resolving volume group %s: %w"`

- [x] Task 5: Wire handler in DRExecution controller (AC: #8)
  - [x] 5.1 In `pkg/controller/drexecution/reconciler.go`, add `VMManager engine.VMManager` field to `DRExecutionReconciler`
  - [x] 5.2 Added `resolveHandler` method: checks mode, if `planned_migration` creates `PlannedMigrationHandler` with VMManager
  - [x] 5.3 Controller calls `PreExecute(ctx, allGroups)` via interface assertion before dispatching the wave executor. On failure: sets `Result=Failed`, reason `PreExecutionFailed`, emits `Step0Failed` event
  - [x] 5.4 Added `BuildExecutionGroups` to WaveExecutor to expose discover→group→chunk pipeline for PreExecute

- [x] Task 6: Update main.go wiring (AC: #8)
  - [x] 6.1 Create `KubeVirtVMManager` with `mgr.GetClient()` and pass it to `DRExecutionReconciler`
  - [x] 6.2 If `--noop-fallback` is enabled, use `NoOpVMManager` instead of `KubeVirtVMManager` for dev/CI environments

- [x] Task 7: Unit tests for VMManager (AC: #3, #10)
  - [x] 7.1 Create `pkg/engine/vm_test.go`
  - [x] 7.2 Test: `TestKubeVirtVMManager_StopVM_Succeeds` — patches RunStrategy to Halted
  - [x] 7.3 Test: `TestKubeVirtVMManager_StopVM_AlreadyStopped` — idempotent, returns nil
  - [x] 7.4 Test: `TestKubeVirtVMManager_StartVM_Succeeds` — patches RunStrategy to Always
  - [x] 7.5 Test: `TestKubeVirtVMManager_StartVM_AlreadyRunning` — idempotent, returns nil
  - [x] 7.6 Test: `TestKubeVirtVMManager_StopVM_NotFound` — returns error
  - [x] 7.7 Test: `TestKubeVirtVMManager_IsVMRunning` — correct status for each RunStrategy (5 subtests)
  - [x] 7.8 Test: `TestNoOpVMManager_AllMethods` — all return nil/false

- [x] Task 8: Unit tests for planned migration handler (AC: #10)
  - [x] 8.1 Create `pkg/engine/planned_test.go`
  - [x] 8.2 Define `mockVMManager` implementing `VMManager` — configurable success/failure per VM name, records calls
  - [x] 8.3 Test: `TestPlannedMigration_FullSuccess` — Step 0 completes, all DRGroups succeed
  - [x] 8.4 Test: `TestPlannedMigration_Step0_StopVMFails` — origin VM unreachable, descriptive error
  - [x] 8.5 Test: `TestPlannedMigration_Step0_StopReplicationFails` — driver error
  - [x] 8.6 Test: `TestPlannedMigration_Step0_SyncTimeout` — timeout triggers failure
  - [x] 8.7 Test: `TestPlannedMigration_Step0_SyncCompletes` — syncs after 3 polls
  - [x] 8.8 Test: `TestPlannedMigration_PerGroup_SetSourceFails` — SetSource error with step name
  - [x] 8.9 Test: `TestPlannedMigration_PerGroup_StartVMFails` — StartVM error with step name
  - [x] 8.10 Test: `TestPlannedMigration_PerGroup_StepStatusRecorded` — all step types recorded
  - [x] 8.11 Test: `TestPlannedMigration_ContextCancelled` — context cancelled
  - [x] 8.12 Test: `TestPlannedMigration_EmptyGroups` — empty groups succeed trivially
  - [x] 8.13 Test: `TestPlannedMigration_VolumeGroupIDCaching` — cached IDs avoid redundant CreateVolumeGroup
  - [x] 8.14 Test: `TestPlannedMigration_Step0_DeduplicatesVMs` — shared VMs stopped once
  - [x] 8.15 Test: `TestPlannedMigration_MultiNamespace` — cross-namespace groups

- [x] Task 9: Update documentation and verify (AC: #1)
  - [x] 9.1 Update `pkg/engine/doc.go` to cover the planned migration workflow and VMManager interface
  - [x] 9.2 Add godoc block comment on `planned.go` explaining the two-phase workflow: Step 0 (global) + per-DRGroup handler
  - [x] 9.3 Update RBAC markers on DRExecution reconciler: add `kubevirt.io` VMs patch;update permission for RunStrategy updates
  - [x] 9.4 Run `make manifests` to regenerate RBAC/webhook configs
  - [x] 9.5 No type changes — `make generate` not needed
  - [x] 9.6 Run `make test` — all unit tests pass (82.3% engine coverage)
  - [x] 9.7 Run `make lint-fix` — no new lint errors (only pre-existing goconst in preflight)
  - [x] 9.8 Run `make build` — compiles cleanly

### Review Findings

- [x] [Review][Patch] Per-step status recording is not wired into the real execution path — Added `StepHandler` interface in executor.go, wired into `executeGroup`; added `Steps` field to `DRGroupExecutionStatus`
- [x] [Review][Patch] `PlannedMigrationHandler` shares an unsynchronized `vgIDCache` — Added `sync.Mutex` (`cacheMu`) protecting all cache reads/writes
- [x] [Review][Patch] Planned migration silently falls back to `NoOpHandler` when `VMManager` is nil — `resolveHandler` now returns error; reconciler fails execution with `HandlerResolutionFailed`
- [x] [Review][Patch] Reconcile retries can rerun Step 0 — Added `Step0Complete` condition; PreExecute guarded by `meta.IsStatusConditionTrue` check
- [x] [Review][Patch] `failExecution` overwrites `StartTime` — Guarded with `if exec.Status.StartTime == nil`

## Dev Notes

### Architecture Context

This is Story 4.3 of Epic 4 (DR Workflow Engine — Full Lifecycle). It implements the planned migration workflow — the first real `DRGroupHandler` that drives actual DR operations through the StorageProvider driver. Stories 4.05 (driver registry convergence), 4.1 (state machine + controller), and 4.2 (wave executor framework) are prerequisites.

**Story 4.3 scope:** The `PlannedMigrationHandler` implementing both a global pre-execution phase (Step 0: stop VMs, stop replication, sync wait) and the per-DRGroup handler for the wave executor (SetSource + start VMs). Also introduces the `VMManager` interface for KubeVirt VM lifecycle control. The **disaster failover** handler is Story 4.4 — it skips Step 0, uses `SetSource(force=true)`, and ignores origin errors.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite — must be done |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — must be done |
| 4.2 | Wave executor framework + controller dispatch | Prerequisite — provides DRGroupHandler interface + WaveExecutor |
| **4.3** | **Planned migration workflow** | **This story — first real DRGroupHandler** |
| 4.4 | Disaster failover workflow | Second DRGroupHandler — uses force=true, skips Step 0 |
| 4.5 | Fail-forward error handling & partial success | Enhances executor error handling (4.2/4.3 provide the foundation) |
| 4.6 | Failed DRGroup retry | Retry mechanism targeting specific failed groups |
| 4.7 | Checkpoint, resume & HA | Per-DRGroup persistence, async execution, pod restart resume |
| 4.8 | Re-protect & failback workflows | Reprotecting + FailingBack paths |

### Existing Code to Reuse (Critical — Do NOT Reinvent)

| File | What It Provides | How Planned Migration Uses It |
|------|-----------------|-------------------------------|
| `pkg/engine/executor.go` (Story 4.2) | `WaveExecutor`, `DRGroupHandler`, `ExecutionGroup` | Planned migration implements `DRGroupHandler`; executor drives wave sequencing |
| `pkg/engine/statemachine.go` (Story 4.1) | `Transition`, `CompleteTransition` | Controller calls after execution completes |
| `pkg/drivers/interface.go` | `StorageProvider` — 7 methods | Handler calls `StopReplication`, `SetSource`, `GetReplicationStatus`, `CreateVolumeGroup` |
| `pkg/drivers/types.go` | `VolumeGroupID`, `VolumeGroupSpec`, `SetSourceOptions`, `StopReplicationOptions`, `ReplicationStatus`, `VolumeRole`, `ReplicationHealth` | All types used by the handler |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing without real storage |
| `pkg/drivers/noop/driver.go` | Reference driver with idempotent role transitions | Dev/CI testing, role transition reference |
| `pkg/engine/discovery.go` | `VMDiscoverer`, `VMReference`, `GroupByWave` | Executor re-discovers VMs at execution time |
| `pkg/engine/chunker.go` | `DRGroupChunk`, `ChunkWaves` | Executor chunks VMs into DRGroups |
| `pkg/engine/consistency.go` | `ResolveVolumeGroups`, `NamespaceLookup` | Executor resolves consistency before chunking |
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRExecution`, `DRGroupExecutionStatus`, `StepStatus`, `ExecutionMode`, `ExecutionResult`, `VolumeGroupInfo` | Status recording, step tracking, mode checking |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/controller/drexecution/reconciler.go` | Story 4.2 adds `WaveExecutor` + `NoOpHandler` dispatch | Add `VMManager` field; check mode for `planned_migration` → create `PlannedMigrationHandler`; call `PreExecute` before executor; pass handler to executor |
| `cmd/soteria/main.go` | Story 4.2 wires `WaveExecutor` + `NoOpHandler` | Create `KubeVirtVMManager` (or `NoOpVMManager` when `--noop-fallback`); pass to `DRExecutionReconciler` |
| `pkg/engine/doc.go` | Covers discovery, consistency, chunking, wave executor | Add planned migration workflow and VMManager documentation |

### New Files to Create

| File | Purpose |
|------|---------|
| `pkg/engine/planned.go` | Planned migration workflow — Step 0 pre-execution + `DRGroupHandler` implementation |
| `pkg/engine/planned_test.go` | Comprehensive planned migration unit tests |
| `pkg/engine/vm.go` | `VMManager` interface + `KubeVirtVMManager` implementation |
| `pkg/engine/vm_noop.go` | `NoOpVMManager` for testing and dev/CI |
| `pkg/engine/vm_test.go` | VMManager unit tests |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/engine/executor.go` | Story 4.2 creates this — planned migration implements its interface, does not modify it |
| `pkg/engine/executor_test.go` | Executor tests — no changes |
| `pkg/engine/handler_noop.go` | No-op handler stays as default — no changes |
| `pkg/engine/chunker.go` | Complete and tested — no changes |
| `pkg/engine/discovery.go` | VM discovery — no changes |
| `pkg/engine/consistency.go` | Consistency resolution — no changes |
| `pkg/engine/statemachine.go` | State machine — call it, don't modify it |
| `pkg/apis/soteria.io/v1alpha1/types.go` | Types are complete — `StepStatus`, `DRGroupExecutionStatus` already support step recording |
| `pkg/drivers/interface.go` | Stable StorageProvider interface |
| `pkg/drivers/types.go` | Domain types — no changes |
| `pkg/drivers/registry.go` | Driver registry — use, don't modify |
| `pkg/drivers/fake/driver.go` | Fake driver for tests — use, don't modify |
| `pkg/drivers/noop/driver.go` | No-op driver — use as reference, don't modify |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes |
| `pkg/admission/*` | Admission webhooks — no changes |

### Key Implementation Decisions

**1. Planned migration has two phases: Step 0 (global) + per-DRGroup (via wave executor).**

Step 0 is a global pre-execution phase that stops ALL origin VMs and replication before any wave starts. This guarantees RPO=0 because no new writes can arrive after replication is stopped and flushed. The per-DRGroup phase then promotes target volumes to Source and starts target VMs wave-by-wave.

The `PlannedMigrationHandler` struct implements both:
- `PreExecute(ctx, groups)` — Step 0 global phase
- `ExecuteGroup(ctx, group)` — per-DRGroup handler for the wave executor

**2. VMManager abstracts KubeVirt VM control.**

KubeVirt VMs are controlled via `VirtualMachine.Spec.RunStrategy`:
- `Halted` — VM is stopped
- `Always` — VM is running and restarts on failure

The `VMManager` interface decouples the planned migration handler from KubeVirt API details, enabling testing with `NoOpVMManager` or mock implementations.

```go
type VMManager interface {
    StopVM(ctx context.Context, name, namespace string) error
    StartVM(ctx context.Context, name, namespace string) error
    IsVMRunning(ctx context.Context, name, namespace string) (bool, error)
}
```

`KubeVirtVMManager` uses `client.Client` to patch the VM resource. It must use a merge patch (not a strategic merge patch) because KubeVirt types are external and may not have strategic merge patch metadata.

**3. VolumeGroupID resolution via CreateVolumeGroup (idempotent).**

The `DRGroupChunk` from the chunker contains API-level `VolumeGroupInfo` (name, namespace, VMNames) but NOT driver-level `VolumeGroupID`s. The planned migration handler resolves IDs by calling `driver.CreateVolumeGroup(spec)` which is idempotent — it returns the existing group if name+namespace match. This is the same approach the no-op driver uses.

```go
func resolveVolumeGroupID(ctx context.Context, driver drivers.StorageProvider, vg v1alpha1.VolumeGroupInfo) (drivers.VolumeGroupID, error) {
    info, err := driver.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
        Name:      vg.Name,
        Namespace: vg.Namespace,
        PVCNames:  extractPVCNamesFromVMs(vg), // PVCs extracted from VM specs
    })
    if err != nil {
        return "", fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
    }
    return info.ID, nil
}
```

Note: PVC names are not stored in `VolumeGroupInfo` (the API type only has `VMNames`). The handler needs to resolve PVC names from the VMs' disk specifications. For Story 4.3, the handler can extract PVC names from the `ExecutionGroup.Chunk.VMs` references combined with a PVC lookup, OR pass empty PVC names to `CreateVolumeGroup` if the driver resolves PVCs internally. The no-op/fake driver does not require PVC names for operation. Story 4.5 will refine PVC resolution.

**4. Step 0 stops VMs before stopping replication.**

Order matters: VMs must stop writing before replication is stopped to ensure all data is flushed. The sequence is:
1. Stop all origin VMs (no new writes)
2. Stop replication on all volume groups (initiates final flush)
3. Poll `GetReplicationStatus` until volumes are `NonReplicated` (flush complete)

This guarantees RPO=0 for planned migration.

**5. Per-DRGroup execution calls driver methods with force=false.**

Planned migration assumes both sites are available. All driver calls use `Force: false`:
- `StopReplication(ctx, vgID, StopReplicationOptions{Force: false})`
- `SetSource(ctx, vgID, SetSourceOptions{Force: false})`

Disaster failover (Story 4.4) uses `Force: true` because the origin site may be unreachable.

**6. Sync wait uses polling, not watch.**

The `GetReplicationStatus` method is a polling API — there is no watch mechanism for replication status. The handler polls at configurable intervals (default 2s) with a configurable timeout (default 10m). This is acceptable because:
- Planned migration is a maintenance window operation, not latency-critical
- The NFR7 requirement (< 5s for live updates) applies to the Console, not to the sync wait
- Polling at 2s is well within acceptable overhead

**7. The controller dispatches based on execution mode.**

```go
var handler engine.DRGroupHandler
switch exec.Spec.Mode {
case v1alpha1.ExecutionModePlannedMigration:
    handler = &engine.PlannedMigrationHandler{
        Driver:           resolvedDriver,
        VMManager:        r.VMManager,
        SyncPollInterval: 2 * time.Second,
        SyncTimeout:      10 * time.Minute,
    }
case v1alpha1.ExecutionModeDisaster:
    handler = engine.NoOpHandler{} // Story 4.4 replaces this
}
```

Story 4.4 will add the disaster handler. Until then, disaster mode uses `NoOpHandler`.

**8. Step 0 vs per-DRGroup: why not everything in ExecuteGroup?**

Step 0 must complete for ALL VMs/volumes BEFORE any wave starts because:
- Stopping replication is all-or-nothing — you can't partially stop replication and start promoting
- The sync wait must cover all volume groups to guarantee RPO=0
- If Step 0 fails, NO volumes should be promoted (no partial state)

The per-DRGroup phase (SetSource + StartVM) is wave-ordered because:
- Application dependencies between waves (e.g., databases before app servers)
- Throttling via `maxConcurrentFailovers`
- Fail-forward semantics (one DRGroup can fail without blocking others)

### Planned Migration Workflow Sequence

```
1. Controller receives DRExecution (mode=planned_migration)
   ↓
2. State machine validates: SteadyState → FailingOver (Story 4.1)
   ↓
3. Wave executor discovers VMs → groups → chunks (Story 4.2)
   ↓
4. Step 0 — PreExecute (THIS STORY):
   a. Stop ALL origin VMs (VMManager.StopVM for each)
   b. StopReplication(force=false) on ALL volume groups
   c. Poll GetReplicationStatus until all synced (RPO=0 guaranteed)
   ↓ (if Step 0 fails → execution Failed, plan phase NOT advanced)
5. Per wave (sequential):
   Per DRGroup (concurrent):
     a. StopReplication(force=false) on group's volume groups [idempotent after Step 0]
     b. SetSource(force=false) on group's volume groups
     c. StartVM for each VM in group
     d. Record StepStatus for each operation
   ↓
6. Compute result: Succeeded / PartiallySucceeded / Failed
   ↓
7. CompleteTransition: FailingOver → FailedOver
```

### Volume Role Transitions During Planned Migration

```
Origin volumes:    Source → NonReplicated (via StopReplication in Step 0)
Target volumes:    Target → NonReplicated (via StopReplication in Step 0)
                   NonReplicated → Source  (via SetSource in per-DRGroup)
```

The role model requires transitions through `NonReplicated`. After Step 0, all volumes are `NonReplicated`. The per-DRGroup phase promotes target volumes to `Source` (the new active site).

### StepStatus Recording

The `DRGroupStatusState.Steps` field supports per-step tracking. The planned migration records:

```yaml
steps:
  - name: StopReplication
    status: Succeeded
    message: "Stopped replication for volume group ns-erp-database"
    timestamp: "2026-04-18T10:00:02Z"
  - name: SetSource
    status: Succeeded
    message: "Set source for volume group ns-erp-database"
    timestamp: "2026-04-18T10:00:03Z"
  - name: StartVM
    status: Succeeded
    message: "Started VM vm-db01"
    timestamp: "2026-04-18T10:00:04Z"
  - name: StartVM
    status: Succeeded
    message: "Started VM vm-db02"
    timestamp: "2026-04-18T10:00:05Z"
```

### RBAC Requirements

The DRExecution controller needs additional RBAC for VM manipulation:

```go
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch;patch;update
```

This adds to the existing RBAC from Stories 4.1/4.2. The DRPlan controller already has kubevirt VM list/watch; the DRExecution controller needs patch/update for RunStrategy changes.

### Code Patterns to Follow

**Structured logging (controller-runtime convention):**
```go
logger := log.FromContext(ctx)
logger.Info("Starting Step 0: stopping origin VMs", "vmCount", len(vmNames))
logger.V(1).Info("Stopping origin VM", "vm", vmName, "namespace", namespace)
logger.Info("Step 0 complete: replication sync verified", "volumeGroups", len(groups))
logger.V(1).Info("Polling replication status", "synced", syncedCount, "total", totalCount)
```

**Error wrapping (project convention):**
```go
return fmt.Errorf("stopping origin VM %s/%s: %w", namespace, name, err)
return fmt.Errorf("stopping replication for volume group %s: %w", vg.Name, err)
return fmt.Errorf("setting source for volume group %s: %w", vg.Name, err)
return fmt.Errorf("starting VM %s/%s: %w", namespace, name, err)
return fmt.Errorf("sync timeout: %d of %d volume groups not synced after %v", remaining, total, timeout)
```

**Event recording (from DRPlan controller pattern):**
```go
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "PlannedMigrationStarted", "PlannedMigration",
    "Planned migration started for plan %s", plan.Name)
r.Recorder.Eventf(&exec, nil, corev1.EventTypeWarning, "Step0Failed", "PlannedMigration",
    "Step 0 failed: %v", err)
```

### KubeVirt VM RunStrategy Reference

KubeVirt `VirtualMachine.Spec.RunStrategy` values:
- `Always` — VM should always be running; kubevirt restarts it if it stops
- `RerunOnFailure` — VM restarts only on non-zero exit
- `Manual` — VM controlled via start/stop subresources
- `Halted` — VM should not be running; kubevirt stops it if running

For planned migration:
- **Stop:** Patch `RunStrategy` to `Halted` — KubeVirt will gracefully shut down the VM's guest OS via ACPI shutdown signal, then stop the virt-launcher pod
- **Start:** Patch `RunStrategy` to `Always` — KubeVirt creates a virt-launcher pod and boots the VM

Graceful shutdown timeout is controlled by `VirtualMachine.Spec.Template.Spec.TerminationGracePeriodSeconds` (default 30s in KubeVirt). The `StopVM` implementation does NOT need to manage this timeout — KubeVirt handles it.

Import path: `kubevirtv1 "kubevirt.io/api/core/v1"` — the project already uses this import throughout (discovery, admission, tests). The scheme registration `kubevirtv1.AddToScheme(scheme)` is already done in `cmd/soteria/main.go` and test suites.

### Test Strategy

**Unit tests** (`pkg/engine/planned_test.go`): Use fake driver from `pkg/drivers/fake/`, mock `VMManager`, and verify:
- Step 0 ordering: VMs stopped → replication stopped → sync wait
- Sync polling: correct polling interval, timeout behavior
- Per-DRGroup steps: StopReplication → SetSource → StartVM
- Error handling: each step failure produces correct error message
- StepStatus recording: all operations recorded with names and timestamps
- Context cancellation: graceful exit

**Mock VMManager:**
```go
type mockVMManager struct {
    mu       sync.Mutex
    stops    []string            // records "namespace/name" in call order
    starts   []string
    failOn   map[string]error    // "namespace/name" → error to return
    running  map[string]bool     // "namespace/name" → running state
}
```

**VMManager tests** (`pkg/engine/vm_test.go`): Use controller-runtime fake client with kubevirt scheme registered. Create VMs with specific RunStrategy values and verify patches.

### Previous Story Intelligence

**From Story 4.2 (DRGroup Chunking & Wave Executor):**
- `DRGroupHandler` is a single-method interface: `ExecuteGroup(ctx, group ExecutionGroup) error`
- `ExecutionGroup` bundles `DRGroupChunk` + resolved `StorageProvider` driver + `WaveIndex int`
- The executor runs synchronously in the reconcile loop (async is Story 4.7)
- Status updates use mutex-serialized writes via `updateGroupStatus`
- The executor does NOT call `PreExecute` — the controller must call it before dispatching the executor
- Do NOT use `errgroup` for within-wave concurrency — use `sync.WaitGroup` (fail-forward)
- The controller calls `CompleteTransition` after execution finishes (Succeeded or PartiallySucceeded only)

**From Story 4.1 (DR State Machine & Execution Controller):**
- `Transition(PhaseSteadyState, ExecutionModePlannedMigration)` → `PhaseFailingOver`
- `CompleteTransition(PhaseFailingOver)` → `PhaseFailedOver`
- Idempotency: `exec.Status.StartTime != nil` means already processed
- Controller uses `k8s.io/client-go/tools/events` (new events API), not `k8s.io/client-go/tools/record`
- All resources are cluster-scoped — use `client.ObjectKey{Name: name}` without namespace

**From Story 4.05 (Driver Registry Fallback & Preflight Convergence):**
- `--noop-fallback` flag enables dev/CI without real storage
- When noop-fallback is enabled, `NoOpVMManager` should also be used (no real KubeVirt)
- `KubeStorageClassLister` in `internal/preflight/` provides real Kubernetes SC lookups
- Driver resolution: `Registry.GetDriver(provisioner)` via `SCLister.GetProvisioner`

**From Epic 3 (Storage Driver Framework):**
- Fake driver (`pkg/drivers/fake/`) has programmable `On*/Return/ReturnResult` API
- All driver methods are idempotent — StopReplication on NonReplicated returns nil
- `CreateVolumeGroup` is idempotent by name+namespace — returns existing if matched
- `StopReplication` on NonReplicated returns nil (idempotent) — safe to call in both Step 0 and per-DRGroup

### Disaster vs Planned Migration Comparison

| Aspect | Planned Migration (Story 4.3) | Disaster (Story 4.4) |
|--------|------|---------|
| Step 0 | Yes — stop VMs, stop replication, wait sync | No — origin assumed unreachable |
| SetSource force | `false` — both sites healthy | `true` — force promote |
| StopReplication force | `false` | `true` or skipped |
| Origin VM handling | Gracefully stopped | Ignored (may be unreachable) |
| RPO guarantee | RPO=0 (sync guaranteed) | RPO>0 (data loss possible) |
| Origin errors | Fail execution | Log and ignore |

### DRExecution is Cluster-Scoped

Both DRPlan and DRExecution are cluster-scoped resources (no namespace). Use `client.ObjectKey{Name: name}` for lookups. VMs however are namespace-scoped — `VMManager` methods take both name and namespace.

### Build Commands

```bash
make manifests    # Regenerate RBAC + webhook configs after adding markers
make generate     # Regenerate deepcopy if types changed
make test         # All unit tests (new + existing)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make build        # Verify compilation
make integration  # Integration tests
```

### Project Structure Notes

All files align with the architecture document:
- `pkg/engine/planned.go` — planned migration workflow (architecture: `pkg/engine/planned.go`)
- `pkg/engine/vm.go` — VMManager abstraction (engine boundary owns VM lifecycle during execution)
- `pkg/engine/vm_noop.go` — test/dev utility
- `pkg/controller/drexecution/reconciler.go` — enhanced controller
- `cmd/soteria/main.go` — wiring

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.3] — Story acceptance criteria (BDD format)
- [Source: _bmad-output/planning-artifacts/prd.md#FR9] — Planned migration: stop VMs, stop replication, transition to Source, start target VMs
- [Source: _bmad-output/planning-artifacts/prd.md#FR11] — Wave-sequential, within-wave concurrent execution
- [Source: _bmad-output/planning-artifacts/prd.md#FR18] — Human-triggered only, no auto-failover
- [Source: _bmad-output/planning-artifacts/prd.md#FR19] — Execution mode specified at runtime, not on plan
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/engine/planned.go` in directory structure
- [Source: _bmad-output/planning-artifacts/architecture.md#Controller Patterns] — Reconcile return patterns, structured logging
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — Registration, idempotency, typed errors, context
- [Source: _bmad-output/planning-artifacts/architecture.md#Engine Boundary] — Engine owns workflow execution, receives plan and driver
- [Source: _bmad-output/planning-artifacts/architecture.md#CRD Status Patterns] — StepStatus for per-step recording
- [Source: _bmad-output/project-context.md#Critical Implementation Rules] — No in-memory state across reconciles, fail-forward model
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — 7-method interface, role model, idempotency
- [Source: _bmad-output/project-context.md#Testing Rules] — envtest over fake client, test naming conventions
- [Source: pkg/drivers/interface.go] — StorageProvider interface: SetSource, StopReplication, GetReplicationStatus, CreateVolumeGroup
- [Source: pkg/drivers/types.go] — VolumeGroupID, VolumeRole, ReplicationHealth, SetSourceOptions, StopReplicationOptions, ReplicationStatus
- [Source: pkg/drivers/noop/driver.go] — Reference implementation: role transitions, idempotency, Force flag handling
- [Source: pkg/drivers/fake/driver.go] — Programmable fake for unit tests
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — DRExecution, StepStatus, ExecutionMode, ExecutionResult, VolumeGroupInfo, DRGroupExecutionStatus
- [Source: pkg/engine/chunker.go] — DRGroupChunk, WaveChunks (contains VolumeGroups and VMs)
- [Source: pkg/engine/discovery.go] — VMDiscoverer interface, VMReference, TypedVMDiscoverer (uses kubevirtv1.VirtualMachine)
- [Source: pkg/controller/drexecution/reconciler.go] — Current controller skeleton
- [Source: pkg/controller/drplan/reconciler.go] — Reference reconciler pattern (events, status updates, logging, kubevirt imports)
- [Source: cmd/soteria/main.go] — Controller wiring, event broadcaster setup, noop-fallback flag
- [Source: _bmad-output/implementation-artifacts/4-2-drgroup-chunking-wave-executor.md] — Previous story: executor framework, DRGroupHandler interface, ExecutionGroup
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — Previous story: state machine, controller setup, admission webhook
- [Source: _bmad-output/implementation-artifacts/4-05-driver-registry-fallback-preflight-convergence.md] — Previous story: registry fallback, noop-fallback flag

## Dev Agent Record

### Agent Model Used

Cursor Agent (Opus 4.6)

### Debug Log References

None — clean implementation, no debug issues.

### Completion Notes List

- Implemented VMManager interface with KubeVirtVMManager (merge patch on RunStrategy) and NoOpVMManager
- Implemented PlannedMigrationHandler with two-phase design: PreExecute (Step 0: stop VMs → stop replication → sync wait) and ExecuteGroup (StopReplication → SetSource → StartVM per DRGroup)
- VolumeGroupID resolution via idempotent CreateVolumeGroup with in-handler caching between Step 0 and per-group execution
- Controller dispatch: resolveHandler selects PlannedMigrationHandler for planned_migration mode; PreExecute called via interface assertion before wave executor; PreExecutionFailed reason on Step 0 error
- Added BuildExecutionGroups to WaveExecutor to expose discover→group→chunk pipeline for PreExecute input
- main.go wires KubeVirtVMManager (or NoOpVMManager when --noop-fallback)
- RBAC updated: kubevirt.io VMs now have patch;update verbs for RunStrategy changes
- 24 new tests: 9 VMManager tests (including 5 IsVMRunning subtests) + 15 planned migration tests
- All existing tests pass — no regressions; engine coverage 82.3%
- Only pre-existing lint issue remains (goconst in preflight, not this story's code)

### File List

**New files:**
- pkg/engine/vm.go — VMManager interface + KubeVirtVMManager implementation
- pkg/engine/vm_noop.go — NoOpVMManager for testing/dev
- pkg/engine/vm_test.go — VMManager unit tests (9 tests)
- pkg/engine/planned.go — PlannedMigrationHandler (Step 0 + per-DRGroup)
- pkg/engine/planned_test.go — Planned migration unit tests (15 tests)

**Modified files:**
- pkg/controller/drexecution/reconciler.go — VMManager field, resolveHandler (returns error), PreExecute with Step0Complete guard, failExecution StartTime guard
- pkg/engine/executor.go — StepHandler interface, executeGroup wires step recording, BuildExecutionGroups method
- pkg/apis/soteria.io/v1alpha1/types.go — Added Steps field to DRGroupExecutionStatus
- cmd/soteria/main.go — Wire KubeVirtVMManager / NoOpVMManager to DRExecutionReconciler
- pkg/engine/doc.go — Added planned migration and VMManager documentation
- config/rbac/role.yaml — Regenerated: kubevirt.io VMs now include patch;update verbs
- _bmad-output/implementation-artifacts/sprint-status.yaml — Updated 4.3 status
- _bmad-output/implementation-artifacts/4-3-planned-migration-workflow.md — Task checkboxes, dev record, review findings
