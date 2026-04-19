# Story 4.4: Disaster Failover Workflow

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to execute a disaster failover that force-promotes target volumes and starts VMs wave by wave while ignoring origin errors,
So that workloads recover quickly when the primary DC is down.

## Acceptance Criteria

1. **AC1 — Disaster failover handler:** `pkg/engine/disaster.go` implements the `DRGroupHandler` interface (from Story 4.2) with a `DisasterFailoverHandler`. For each DRGroup, it calls `SetSource(force=true)` on each volume group, then starts the target VMs via `VMManager`. Origin site errors are logged as warnings but never fail the execution (FR10).

2. **AC2 — No Step 0 pre-execution:** `DisasterFailoverHandler` does NOT implement a `PreExecute` method (or its implementation is a no-op returning nil). The origin site is assumed unreachable — no VM stopping, no replication stopping, no sync wait occurs before wave execution begins. This is the fundamental difference from planned migration.

3. **AC3 — Force-promote via SetSource:** Every `SetSource` call uses `SetSourceOptions{Force: true}`. This tells the storage driver to promote target volumes to Source role even if the paired origin is unreachable. The driver handles internal split-brain semantics — the orchestrator only sets the flag.

4. **AC4 — RPO recording:** After `SetSource` succeeds for a volume group, the handler calls `GetReplicationStatus` to read `LastSyncTime` and `EstimatedRPO`. The per-DRGroup `StepStatus` message includes the observed RPO (e.g., `"Set source for volume group ns-erp-database (RPO: ~47s)"`). The handler tracks the maximum RPO across all volume groups and records it in a final summary step.

5. **AC5 — Origin error tolerance:** If `GetReplicationStatus` fails after a successful `SetSource` (e.g., origin-side metadata unavailable), the handler logs the error at V(1) and records RPO as `"unknown"` — it does NOT fail the DRGroup. The `SetSource(force=true)` call itself failing is a real error and fails the DRGroup.

6. **AC6 — Per-DRGroup execution steps:** For each DRGroup within a wave, `ExecuteGroup` executes these steps in order: (a) `SetSource(force=true)` on each volume group in the group (no `StopReplication` — origin is down), (b) `StartVM` for each VM in the group. Each step is recorded as a `StepStatus` entry. If `SetSource` fails, the DRGroup is marked `Failed` with the step name and error.

7. **AC7 — Per-step status recording:** Each operation within a DRGroup is recorded in the DRGroupStatus as a `StepStatus` entry with `Name` (e.g., `"SetSource"`, `"StartVM"`), `Status` (`"Succeeded"`/`"Failed"`), `Message`, and `Timestamp`. This mirrors the planned migration step recording pattern.

8. **AC8 — Controller integration:** The DRExecution controller dispatches `DisasterFailoverHandler` when `exec.Spec.Mode == "disaster"`. The controller does NOT call `PreExecute` for disaster mode (or calls the no-op version). It passes the handler directly to the wave executor. After execution completes, `CompleteTransition` advances the DRPlan phase as usual.

9. **AC9 — Unit tests:** `pkg/engine/disaster_test.go` has table-driven tests covering: (a) successful disaster failover — all DRGroups succeed, VMs started, RPO recorded; (b) SetSource failure — group fails, error includes step name and volume group; (c) StartVM failure — group fails; (d) GetReplicationStatus failure after successful SetSource — RPO recorded as "unknown", group succeeds; (e) per-step status recording — all steps recorded with correct names and timestamps; (f) empty DRGroup — no driver calls, succeeds immediately; (g) context cancellation — graceful exit; (h) multiple volume groups in one DRGroup — all promoted before any VM started. Tests use fake driver (`pkg/drivers/fake/`) and mock `VMManager`.

## Tasks / Subtasks

- [ ] Task 1: Implement disaster failover handler (AC: #1, #2, #3, #6, #7)
  - [ ] 1.1 Create `pkg/engine/disaster.go` with copyright header and Tier 2 architecture block comment explaining the disaster failover workflow: no Step 0, force-promote, origin error tolerance
  - [ ] 1.2 Define `DisasterFailoverHandler` struct with fields: `Driver drivers.StorageProvider`, `VMManager VMManager`
  - [ ] 1.3 Implement `PreExecute(ctx context.Context, groups []ExecutionGroup) error` — return nil immediately (no-op for disaster mode)
  - [ ] 1.4 Implement `ExecuteGroup(ctx context.Context, group ExecutionGroup) error` — the per-DRGroup handler
  - [ ] 1.5 Phase 1 — Force-promote all volume groups: for each VolumeGroup in the group, resolve `VolumeGroupID` via `resolveVolumeGroupID` (reuse from planned.go or extract to shared helper), call `driver.SetSource(ctx, vgID, SetSourceOptions{Force: true})`, record `StepStatus{Name: StepSetSource, Status: "Succeeded"}`. On error, record `StepStatus{Status: "Failed"}` and return immediately
  - [ ] 1.6 Phase 2 — Start target VMs: for each VM in the group, call `VMManager.StartVM(ctx, vm.Name, vm.Namespace)`, record `StepStatus{Name: StepStartVM}`. On error, record failure and return
  - [ ] 1.7 Define step name constants: reuse `StepSetSource` and `StepStartVM` from `planned.go` (or define shared constants in a common location like `executor.go`)

- [ ] Task 2: Implement RPO recording (AC: #4, #5)
  - [ ] 2.1 After each successful `SetSource`, call `driver.GetReplicationStatus(ctx, vgID)` to read `LastSyncTime`
  - [ ] 2.2 Calculate RPO as `time.Since(*status.LastSyncTime)` if `LastSyncTime` is non-nil; otherwise RPO is `"unknown"`
  - [ ] 2.3 Include RPO in the SetSource `StepStatus.Message`: `"Set source for volume group %s (RPO: ~%s)"` or `"Set source for volume group %s (RPO: unknown)"`
  - [ ] 2.4 If `GetReplicationStatus` returns an error, log at V(1): `"Could not read replication status for RPO"` with volume group name and error, set RPO to `"unknown"` — do NOT fail the DRGroup
  - [ ] 2.5 Track maximum RPO across all volume groups in the handler's execution scope (use a field or local variable within `ExecuteGroup`)

- [ ] Task 3: Extract shared helpers from planned migration (AC: #1)
  - [ ] 3.1 If `resolveVolumeGroupID` is defined as an unexported function in `planned.go`, either: (a) extract it to a shared file `pkg/engine/helpers.go`, or (b) duplicate it in `disaster.go` (acceptable if small). Prefer extraction to avoid drift
  - [ ] 3.2 If step name constants (`StepSetSource`, `StepStartVM`, `StepStopReplication`) are defined in `planned.go`, move them to `executor.go` or a shared `pkg/engine/steps.go` so both handlers can reference them
  - [ ] 3.3 If the `recordStep` helper for creating `StepStatus` entries exists in `planned.go`, extract to a shared location

- [ ] Task 4: Wire handler in DRExecution controller (AC: #8)
  - [ ] 4.1 In `pkg/controller/drexecution/reconciler.go`, update the handler dispatch switch: when `exec.Spec.Mode == v1alpha1.ExecutionModeDisaster`, create `DisasterFailoverHandler` with the resolved driver and `VMManager`
  - [ ] 4.2 For disaster mode, skip `handler.PreExecute()` call entirely (or call the no-op version). Do NOT call `StopReplication` or attempt any origin-side operations
  - [ ] 4.3 Pass the handler to `WaveExecutor.Execute()` as the `DRGroupHandler`

- [ ] Task 5: Unit tests for disaster failover handler (AC: #9)
  - [ ] 5.1 Create `pkg/engine/disaster_test.go`
  - [ ] 5.2 Reuse `mockVMManager` from `planned_test.go` (or extract to `testutil_test.go` if not already shared)
  - [ ] 5.3 Test: `TestDisasterFailover_FullSuccess` — all DRGroups succeed with `SetSource(force=true)`, VMs started, RPO recorded in step messages
  - [ ] 5.4 Test: `TestDisasterFailover_SetSourceFails` — driver returns error on SetSource → group fails, step records failure, error includes volume group name
  - [ ] 5.5 Test: `TestDisasterFailover_StartVMFails` — VMManager returns error → group fails, step records failure
  - [ ] 5.6 Test: `TestDisasterFailover_GetReplicationStatusFails` — SetSource succeeds but GetReplicationStatus fails → RPO recorded as "unknown", group still succeeds
  - [ ] 5.7 Test: `TestDisasterFailover_RPORecording` — verify RPO appears in StepStatus message for each volume group, max RPO tracked
  - [ ] 5.8 Test: `TestDisasterFailover_StepStatusRecorded` — verify all steps (SetSource per VG + StartVM per VM) recorded with correct names, timestamps, statuses
  - [ ] 5.9 Test: `TestDisasterFailover_EmptyGroup` — no volume groups, ExecuteGroup succeeds trivially
  - [ ] 5.10 Test: `TestDisasterFailover_ContextCancelled` — context cancelled mid-execution, returns ctx.Err()
  - [ ] 5.11 Test: `TestDisasterFailover_ForceFlag` — verify `SetSourceOptions{Force: true}` is passed to driver (check via fake driver call recording)
  - [ ] 5.12 Test: `TestDisasterFailover_NoStopReplication` — verify `StopReplication` is never called on the driver (disaster skips this entirely)
  - [ ] 5.13 Test: `TestDisasterFailover_MultipleVolumeGroups` — 3 VGs in one group: all SetSource called before any StartVM, correct step ordering
  - [ ] 5.14 Test: `TestDisasterFailover_PreExecute_Noop` — `PreExecute` returns nil without calling any driver or VMManager methods

- [ ] Task 6: Update documentation and verify (AC: #1)
  - [ ] 6.1 Update `pkg/engine/doc.go` to cover the disaster failover workflow and its difference from planned migration
  - [ ] 6.2 Add godoc block comment on `disaster.go` explaining: no Step 0, force=true, origin error tolerance, RPO recording
  - [ ] 6.3 Run `make manifests` to regenerate RBAC/webhook configs (in case any markers changed)
  - [ ] 6.4 Run `make generate` if types changed
  - [ ] 6.5 Run `make test` — all unit tests pass
  - [ ] 6.6 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 6.7 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4.4 of Epic 4 (DR Workflow Engine — Full Lifecycle). It implements the disaster failover workflow — the second `DRGroupHandler`, purpose-built for when the primary DC is unreachable. Stories 4.05 (driver registry convergence), 4.1 (state machine + controller), 4.2 (wave executor framework), and 4.3 (planned migration + VMManager) are prerequisites.

**Story 4.4 scope:** The `DisasterFailoverHandler` implementing `DRGroupHandler` with force-promoted `SetSource` and origin error tolerance. No Step 0, no replication stopping, no sync wait. Also adds RPO recording from `GetReplicationStatus.LastSyncTime` after promotion. The VMManager interface and `KubeVirtVMManager` implementation are already created in Story 4.3 — this story reuses them.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite — must be done |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — must be done |
| 4.2 | Wave executor framework + controller dispatch | Prerequisite — provides DRGroupHandler interface + WaveExecutor |
| 4.3 | Planned migration workflow + VMManager | Prerequisite — provides VMManager, step recording patterns, resolveVolumeGroupID helper |
| **4.4** | **Disaster failover workflow** | **This story — second DRGroupHandler, force-promote path** |
| 4.5 | Fail-forward error handling & partial success | Enhances executor error handling |
| 4.6 | Failed DRGroup retry | Retry mechanism targeting specific failed groups |
| 4.7 | Checkpoint, resume & HA | Per-DRGroup persistence, async execution, pod restart resume |
| 4.8 | Re-protect & failback workflows | Reprotecting + FailingBack paths |

### Existing Code to Reuse (Critical — Do NOT Reinvent)

| File | What It Provides | How Disaster Failover Uses It |
|------|-----------------|-------------------------------|
| `pkg/engine/planned.go` (Story 4.3) | `PlannedMigrationHandler`, `resolveVolumeGroupID`, step name constants (`StepSetSource`, `StepStartVM`), step recording pattern | Disaster handler mirrors the per-DRGroup structure but with force=true and no StopReplication step |
| `pkg/engine/vm.go` (Story 4.3) | `VMManager` interface, `KubeVirtVMManager`, `NoOpVMManager` | Handler calls `VMManager.StartVM` — same interface as planned migration |
| `pkg/engine/executor.go` (Story 4.2) | `WaveExecutor`, `DRGroupHandler`, `ExecutionGroup` | Disaster failover implements `DRGroupHandler`; executor drives wave sequencing |
| `pkg/engine/statemachine.go` (Story 4.1) | `Transition`, `CompleteTransition` | Controller calls after execution completes |
| `pkg/drivers/interface.go` | `StorageProvider` — `SetSource`, `GetReplicationStatus`, `CreateVolumeGroup` | Handler calls `SetSource(force=true)` and `GetReplicationStatus` for RPO |
| `pkg/drivers/types.go` | `VolumeGroupID`, `SetSourceOptions`, `ReplicationStatus`, `VolumeRole`, `ReplicationHealth` | All types used by the handler |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing without real storage |
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRExecution`, `DRGroupExecutionStatus`, `StepStatus`, `ExecutionMode`, `ExecutionResult`, `VolumeGroupInfo` | Status recording, step tracking, mode checking |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/controller/drexecution/reconciler.go` | Story 4.3 adds `PlannedMigrationHandler` dispatch for `planned_migration` mode; `disaster` mode uses `NoOpHandler` placeholder | Replace `NoOpHandler` with `DisasterFailoverHandler` when `exec.Spec.Mode == "disaster"` |
| `pkg/engine/doc.go` | Covers discovery, consistency, chunking, wave executor, planned migration | Add disaster failover workflow documentation |

### Shared Code Extraction (from Story 4.3)

Story 4.3 creates several helpers in `planned.go` that disaster.go also needs. Depending on how 4.3 organized them:

| Helper | Current Location (Story 4.3) | Action for Story 4.4 |
|--------|------------------------------|----------------------|
| `resolveVolumeGroupID(ctx, driver, vg)` | `pkg/engine/planned.go` (unexported) | Extract to `pkg/engine/helpers.go` or duplicate (function is ~10 lines) |
| `StepSetSource`, `StepStartVM`, `StepStopReplication` | `pkg/engine/planned.go` (constants) | Move to shared location (e.g., `executor.go` or `steps.go`) |
| `recordStep(...)` or step recording pattern | `pkg/engine/planned.go` | Extract or duplicate — should be a shared utility |
| `mockVMManager` | `pkg/engine/planned_test.go` | Extract to `pkg/engine/testutil_test.go` for sharing between test files |

If Story 4.3 already extracted these to shared locations, simply import them. If not, extract as part of this story (minor refactor, no behavior change).

### New Files to Create

| File | Purpose |
|------|---------|
| `pkg/engine/disaster.go` | Disaster failover workflow — `DRGroupHandler` with force-promote + RPO recording |
| `pkg/engine/disaster_test.go` | Comprehensive disaster failover unit tests |
| `pkg/engine/helpers.go` | Shared helpers extracted from planned.go (if not already extracted) — `resolveVolumeGroupID`, step constants |
| `pkg/engine/testutil_test.go` | Shared test helpers — `mockVMManager` (if not already extracted) |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/engine/planned.go` | Planned migration handler — do not change its behavior; only extract shared helpers if needed |
| `pkg/engine/planned_test.go` | Planned migration tests — no changes (unless extracting mockVMManager) |
| `pkg/engine/executor.go` | Executor framework — disaster handler implements its interface, does not modify it |
| `pkg/engine/executor_test.go` | Executor tests — no changes |
| `pkg/engine/handler_noop.go` | No-op handler stays as default — no changes |
| `pkg/engine/vm.go` | VMManager interface — use, don't modify |
| `pkg/engine/vm_noop.go` | NoOpVMManager — use for tests, don't modify |
| `pkg/engine/chunker.go` | Complete and tested — no changes |
| `pkg/engine/discovery.go` | VM discovery — no changes |
| `pkg/engine/consistency.go` | Consistency resolution — no changes |
| `pkg/engine/statemachine.go` | State machine — call it, don't modify |
| `pkg/apis/soteria.io/v1alpha1/types.go` | Types are complete — `StepStatus`, `DRGroupExecutionStatus` already support step recording |
| `pkg/drivers/interface.go` | Stable StorageProvider interface |
| `pkg/drivers/types.go` | Domain types — no changes |
| `pkg/drivers/registry.go` | Driver registry — use, don't modify |
| `pkg/drivers/fake/driver.go` | Fake driver for tests — use, don't modify |
| `pkg/drivers/noop/driver.go` | No-op driver — use as reference, don't modify |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes |
| `pkg/admission/*` | Admission webhooks — no changes |

### Key Implementation Decisions

**1. Disaster failover has NO Step 0 — go directly to per-DRGroup execution.**

The origin site is assumed unreachable. There is no point in stopping origin VMs (can't reach them), stopping replication (can't coordinate with origin), or waiting for sync (no sync will happen). The handler goes directly to force-promoting target volumes and starting target VMs.

The `DisasterFailoverHandler` struct implements:
- `PreExecute(ctx, groups)` — returns nil immediately (no-op)
- `ExecuteGroup(ctx, group)` — per-DRGroup handler with force-promote

**2. SetSource uses Force=true — the critical disaster flag.**

```go
err := handler.Driver.SetSource(ctx, vgID, drivers.SetSourceOptions{Force: true})
```

The `Force` flag tells the storage driver to promote volumes to Source role even when the paired target (origin) is unreachable. Without this flag, drivers that check origin-side state would fail. This is the architectural cornerstone of disaster failover — the driver interface was designed with this flag specifically for this scenario (see `pkg/drivers/interface.go` comments).

**3. No StopReplication call in disaster mode.**

Planned migration calls `StopReplication(force=false)` before `SetSource` to flush pending writes. Disaster mode skips this entirely:
- The origin is unreachable — `StopReplication` would fail
- `SetSource(force=true)` handles promotion without requiring prior replication stop
- The volume role transition is: Target → Source (force) — the driver handles the NonReplicated intermediate state internally when force=true

**4. RPO recording provides operational visibility.**

After each successful `SetSource`, the handler queries `GetReplicationStatus` to read `LastSyncTime`. RPO is calculated as `time.Since(*lastSyncTime)`. This is informational only — it does not gate execution. If the query fails (origin metadata unavailable), RPO is recorded as `"unknown"`.

```go
status, err := handler.Driver.GetReplicationStatus(ctx, vgID)
if err != nil {
    logger.V(1).Info("Could not read replication status for RPO", "volumeGroup", vg.Name, "error", err)
    rpoStr = "unknown"
} else if status.LastSyncTime != nil {
    rpo := time.Since(*status.LastSyncTime)
    rpoStr = rpo.Truncate(time.Second).String()
} else {
    rpoStr = "unknown"
}
```

**5. Origin errors are tolerated — only SetSource and StartVM failures are real.**

| Operation | Failure behavior |
|-----------|-----------------|
| `SetSource(force=true)` | Real failure → DRGroup fails (driver can't promote) |
| `GetReplicationStatus` (for RPO) | Tolerated → log warning, RPO="unknown", continue |
| `StartVM` | Real failure → DRGroup fails |

**6. The controller dispatch is a simple switch update.**

Story 4.3 already establishes the dispatch pattern:

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
    handler = &engine.DisasterFailoverHandler{
        Driver:    resolvedDriver,
        VMManager: r.VMManager,
    }
}
```

Story 4.4 replaces the `NoOpHandler{}` placeholder for disaster mode with the real handler. `DisasterFailoverHandler` is simpler than `PlannedMigrationHandler` — it has no sync timeout/interval configuration.

**7. VolumeGroupID resolution reuses the same pattern as planned migration.**

The `resolveVolumeGroupID` helper calls `driver.CreateVolumeGroup(spec)` which is idempotent. Same approach, same code — extract to a shared helper if not already done in Story 4.3.

**8. Per-DRGroup step ordering: all SetSource before any StartVM.**

Within a single DRGroup, ALL volume groups must be promoted before ANY VM is started. This ensures all data volumes are read-write before the VM boots and attempts to access them.

```
For DRGroup:
  1. SetSource(force=true) for VolumeGroup A  → record StepStatus
  2. SetSource(force=true) for VolumeGroup B  → record StepStatus
  3. StartVM for VM-1                         → record StepStatus
  4. StartVM for VM-2                         → record StepStatus
```

### Disaster Failover Workflow Sequence

```
1. Controller receives DRExecution (mode=disaster)
   ↓
2. State machine validates: SteadyState → FailingOver (Story 4.1)
   ↓
3. Wave executor discovers VMs → groups → chunks (Story 4.2)
   ↓
4. NO Step 0 — skip directly to per-wave execution
   ↓
5. Per wave (sequential):
   Per DRGroup (concurrent):
     a. SetSource(force=true) on each volume group → record StepStatus + RPO
     b. StartVM for each VM in group → record StepStatus
   ↓
6. Compute result: Succeeded / PartiallySucceeded / Failed
   ↓
7. CompleteTransition: FailingOver → FailedOver
```

### Disaster vs Planned Migration Comparison

| Aspect | Planned Migration (Story 4.3) | Disaster Failover (Story 4.4) |
|--------|------|---------|
| Step 0 (PreExecute) | Yes — stop VMs, stop replication, wait sync | No — returns nil immediately |
| SetSource force | `false` — both sites healthy | `true` — force promote |
| StopReplication | Called in Step 0 AND per-DRGroup (idempotent) | Never called |
| Origin VM handling | Gracefully stopped in Step 0 | Ignored (origin assumed unreachable) |
| RPO guarantee | RPO=0 (sync guaranteed) | RPO>0 (data loss since last sync) |
| RPO recording | Not applicable (RPO=0 by design) | Recorded from GetReplicationStatus.LastSyncTime |
| Origin errors | Fail execution | Log and ignore |
| Sync timeout config | SyncPollInterval, SyncTimeout fields | Not applicable |
| Handler fields | Driver, VMManager, SyncPollInterval, SyncTimeout | Driver, VMManager only |

### Volume Role Transitions During Disaster Failover

```
Origin volumes:    Source → (unreachable, no transition applied)
Target volumes:    Target → Source (via SetSource force=true — driver handles internal transitions)
```

In disaster mode, the driver's `SetSource(force=true)` handles the Target→Source promotion internally, potentially skipping the NonReplicated intermediate state or handling it atomically. The orchestrator does not need to manage role transition ordering — `Force` delegates that responsibility to the driver.

### StepStatus Recording

The disaster handler records the same step types as planned migration, minus `StopReplication`:

```yaml
steps:
  - name: SetSource
    status: Succeeded
    message: "Set source for volume group ns-erp-database (RPO: ~47s)"
    timestamp: "2026-04-18T03:15:02Z"
  - name: SetSource
    status: Succeeded
    message: "Set source for volume group ns-erp-appserver (RPO: ~52s)"
    timestamp: "2026-04-18T03:15:03Z"
  - name: StartVM
    status: Succeeded
    message: "Started VM vm-db01"
    timestamp: "2026-04-18T03:15:04Z"
  - name: StartVM
    status: Succeeded
    message: "Started VM vm-app01"
    timestamp: "2026-04-18T03:15:05Z"
```

### Code Patterns to Follow

**Structured logging (controller-runtime convention):**
```go
logger := log.FromContext(ctx)
logger.Info("Executing disaster failover for DRGroup", "group", group.Chunk.Name, "vmCount", len(group.Chunk.VMs))
logger.V(1).Info("Force-promoting volume group", "volumeGroup", vg.Name)
logger.V(1).Info("Could not read replication status for RPO", "volumeGroup", vg.Name, "error", err)
logger.V(1).Info("Starting target VM", "vm", vm.Name, "namespace", vm.Namespace)
```

**Error wrapping (project convention):**
```go
return fmt.Errorf("setting source for volume group %s: %w", vg.Name, err)
return fmt.Errorf("starting VM %s/%s: %w", vm.Namespace, vm.Name, err)
return fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
```

**Event recording (from controller pattern):**
```go
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "DisasterFailoverStarted", "DisasterFailover",
    "Disaster failover started for plan %s", plan.Name)
```

### DRExecution is Cluster-Scoped

Both DRPlan and DRExecution are cluster-scoped resources (no namespace). Use `client.ObjectKey{Name: name}` for lookups. VMs are namespace-scoped — `VMManager` methods take both name and namespace.

### Test Strategy

**Unit tests** (`pkg/engine/disaster_test.go`): Use fake driver from `pkg/drivers/fake/`, mock `VMManager`, and verify:
- Force flag: `SetSourceOptions{Force: true}` passed to every SetSource call
- No StopReplication: verify driver's StopReplication is never called
- Step ordering: all SetSource before any StartVM within a DRGroup
- RPO recording: `GetReplicationStatus` called after each SetSource, RPO in message
- RPO tolerance: `GetReplicationStatus` failure does not fail the group
- Error handling: SetSource failure and StartVM failure produce correct error messages
- StepStatus recording: all operations recorded with names and timestamps
- Context cancellation: graceful exit
- PreExecute no-op: returns nil without touching driver or VMManager

**Mock VMManager (reused from Story 4.3):**
```go
type mockVMManager struct {
    mu       sync.Mutex
    stops    []string            // "namespace/name" in call order
    starts   []string
    failOn   map[string]error    // "namespace/name" → error to return
    running  map[string]bool     // "namespace/name" → running state
}
```

**Fake driver verification:**
```go
// Verify force=true was passed
calls := fakeDriver.Calls("SetSource")
for _, call := range calls {
    opts := call.Args[1].(drivers.SetSourceOptions)
    assert.True(t, opts.Force, "disaster failover must use Force=true")
}

// Verify StopReplication was never called
assert.Empty(t, fakeDriver.Calls("StopReplication"), "disaster failover must not call StopReplication")
```

### Previous Story Intelligence

**From Story 4.3 (Planned Migration Workflow):**
- `PlannedMigrationHandler` structure: `Driver`, `VMManager`, `SyncPollInterval`, `SyncTimeout` — disaster is simpler: only `Driver` + `VMManager`
- `resolveVolumeGroupID` helper: calls `driver.CreateVolumeGroup(spec)` idempotently — reuse directly
- Step name constants: `StepSetSource`, `StepStartVM`, `StepStopReplication` — reuse (minus StopReplication)
- Step recording pattern: `StepStatus{Name, Status, Message, Timestamp}` with `metav1.Now()` — follow exactly
- `mockVMManager` test helper: configurable success/failure, records calls — extract and reuse
- VMManager `StartVM` is idempotent: returns nil if already running
- Per-DRGroup execution: all volume operations before any VM operations — follow this ordering
- Error wrapping style: `"<operation> for <subject>: %w"` — follow exactly
- PVC name resolution note: for Story 4.3/4.4, pass empty PVC names to `CreateVolumeGroup` if the driver resolves PVCs internally. The no-op/fake driver does not require PVC names

**From Story 4.2 (DRGroup Chunking & Wave Executor):**
- `DRGroupHandler` is a single-method interface: `ExecuteGroup(ctx, group ExecutionGroup) error`
- `ExecutionGroup` bundles `DRGroupChunk` + resolved `StorageProvider` driver + `WaveIndex int`
- The executor runs synchronously in the reconcile loop (async is Story 4.7)
- Status updates use mutex-serialized writes via `updateGroupStatus`
- The controller calls `PreExecute` before dispatching the executor for planned migration; for disaster mode, skip it or call the no-op
- Do NOT use `errgroup` for within-wave concurrency — use `sync.WaitGroup` (fail-forward)
- The controller calls `CompleteTransition` after execution finishes (Succeeded or PartiallySucceeded only)

**From Story 4.1 (DR State Machine & Execution Controller):**
- `Transition(PhaseSteadyState, ExecutionModeDisaster)` → `PhaseFailingOver`
- `CompleteTransition(PhaseFailingOver)` → `PhaseFailedOver`
- Idempotency: `exec.Status.StartTime != nil` means already processed
- Controller uses `k8s.io/client-go/tools/events` (new events API), not `k8s.io/client-go/tools/record`
- All resources are cluster-scoped — use `client.ObjectKey{Name: name}` without namespace

**From Story 4.05 (Driver Registry Fallback & Preflight Convergence):**
- `--noop-fallback` flag enables dev/CI without real storage
- When noop-fallback is enabled, `NoOpVMManager` should also be used (no real KubeVirt)
- Driver resolution: `Registry.GetDriver(provisioner)` via `SCLister.GetProvisioner`

**From Epic 3 (Storage Driver Framework):**
- Fake driver (`pkg/drivers/fake/`) has programmable `On*/Return/ReturnResult` API — use `OnSetSource().Return(nil)` and `OnGetReplicationStatus().ReturnResult(...)` for tests
- All driver methods are idempotent — `SetSource` on already-Source returns nil
- `SetSource(force=true)` tells the driver to proceed even if the paired target is unreachable
- `GetReplicationStatus` returns `ReplicationStatus{Role, Health, LastSyncTime, EstimatedRPO}` — read `LastSyncTime` for RPO calculation

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
- `pkg/engine/disaster.go` — disaster failover workflow (architecture: `pkg/engine/disaster.go`)
- `pkg/engine/disaster_test.go` — unit tests
- `pkg/engine/helpers.go` — shared engine helpers (if extracted)
- `pkg/controller/drexecution/reconciler.go` — enhanced controller dispatch
- No changes to `cmd/soteria/main.go` — disaster handler creation is in the controller, wiring is already complete from Story 4.3

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.4] — Story acceptance criteria (BDD format)
- [Source: _bmad-output/planning-artifacts/prd.md#FR10] — Disaster recovery execution: force-promote, start VMs, ignore origin errors
- [Source: _bmad-output/planning-artifacts/prd.md#FR11] — Wave-sequential, within-wave concurrent execution
- [Source: _bmad-output/planning-artifacts/prd.md#FR13] — Fail-forward error handling with PartiallySucceeded reporting
- [Source: _bmad-output/planning-artifacts/prd.md#FR18] — Human-triggered only, no auto-failover
- [Source: _bmad-output/planning-artifacts/prd.md#FR19] — Execution mode specified at runtime, not on plan
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/engine/disaster.go` in directory structure
- [Source: _bmad-output/planning-artifacts/architecture.md#Controller Patterns] — Reconcile return patterns, structured logging
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — Registration, idempotency, typed errors, context
- [Source: _bmad-output/planning-artifacts/architecture.md#Engine Boundary] — Engine owns workflow execution, receives plan and driver
- [Source: _bmad-output/planning-artifacts/architecture.md#CRD Status Patterns] — StepStatus for per-step recording
- [Source: _bmad-output/project-context.md#Critical Implementation Rules] — No in-memory state across reconciles, fail-forward model
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — 7-method interface, role model, idempotency
- [Source: _bmad-output/project-context.md#Testing Rules] — envtest over fake client, test naming conventions
- [Source: pkg/drivers/interface.go] — StorageProvider interface: SetSource with Force flag, GetReplicationStatus with LastSyncTime/EstimatedRPO
- [Source: pkg/drivers/types.go] — VolumeGroupID, VolumeRole, ReplicationHealth, SetSourceOptions{Force bool}, ReplicationStatus{LastSyncTime, EstimatedRPO}
- [Source: pkg/drivers/errors.go] — ErrVolumeGroupNotFound, ErrInvalidTransition
- [Source: pkg/drivers/noop/driver.go] — Reference implementation: role transitions, idempotency, Force flag handling
- [Source: pkg/drivers/fake/driver.go] — Programmable fake for unit tests (On*/Return/ReturnResult API)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — DRExecution, StepStatus, ExecutionModeDisaster, ExecutionResult, VolumeGroupInfo, DRGroupExecutionStatus
- [Source: pkg/engine/chunker.go] — DRGroupChunk, WaveChunks (contains VolumeGroups and VMs)
- [Source: pkg/engine/discovery.go] — VMDiscoverer interface, VMReference, TypedVMDiscoverer
- [Source: pkg/controller/drexecution/reconciler.go] — Current controller with handler dispatch pattern
- [Source: _bmad-output/implementation-artifacts/4-3-planned-migration-workflow.md] — Previous story: planned migration handler, VMManager, resolveVolumeGroupID, step recording
- [Source: _bmad-output/implementation-artifacts/4-2-drgroup-chunking-wave-executor.md] — Previous story: executor framework, DRGroupHandler interface, ExecutionGroup, fail-forward semantics
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — Previous story: state machine, controller setup, admission webhook
- [Source: _bmad-output/implementation-artifacts/4-05-driver-registry-fallback-preflight-convergence.md] — Previous story: registry fallback, noop-fallback flag

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
