# Story 4.4: Disaster Failover Workflow

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the unified FailoverHandler to support disaster mode with force-promotion, RPO recording, and origin error tolerance,
So that workloads recover quickly when the primary DC is down.

## Acceptance Criteria

1. **AC1 — Disaster mode in unified handler:** `FailoverHandler` (from Story 4.1b) implements disaster mode when configured with `FailoverConfig{GracefulShutdown: false, Force: true, RecordRPO: true}`. For each DRGroup, it calls `SetSource(force=true)` on each volume group, then starts the target VMs via `VMManager`. Origin site errors are logged as warnings but never fail the execution (FR10).

2. **AC2 — No Step 0 pre-execution:** `PreExecute` returns nil when `GracefulShutdown=false` (already implemented in Story 4.1b). The origin site is assumed unreachable — no VM stopping, no replication stopping, no sync wait occurs before wave execution begins. This is the fundamental difference from planned migration.

3. **AC3 — Force-promote via SetSource:** Every `SetSource` call in disaster mode uses `SetSourceOptions{Force: true}` (driven by `FailoverConfig.Force`). This tells the storage driver to promote target volumes to Source role even if the paired origin is unreachable. The driver handles internal split-brain semantics — the orchestrator only sets the flag.

4. **AC4 — RPO recording:** When `RecordRPO=true`, after `SetSource` succeeds for a volume group, `FailoverHandler` calls `GetReplicationStatus` to read `LastSyncTime` and `EstimatedRPO`. The per-DRGroup `StepStatus` message includes the observed RPO (e.g., `"Set source for volume group ns-erp-database (RPO: ~47s)"`). The handler tracks the maximum RPO across all volume groups and records it in a final summary step.

5. **AC5 — Origin error tolerance:** If `GetReplicationStatus` fails after a successful `SetSource` (e.g., origin-side metadata unavailable), `FailoverHandler` logs the error at V(1) and records RPO as `"unknown"` — it does NOT fail the DRGroup. The `SetSource(force=true)` call itself failing is a real error and fails the DRGroup.

6. **AC6 — Per-DRGroup execution steps:** For each DRGroup within a wave, `FailoverHandler.ExecuteGroup` executes these steps in order: (a) `SetSource(force=true)` on each volume group in the group (no `StopReplication` — origin is down), (b) `StartVM` for each VM in the group. Each step is recorded as a `StepStatus` entry. If `SetSource` fails, the DRGroup is marked `Failed` with the step name and error.

7. **AC7 — Per-step status recording:** Each operation within a DRGroup is recorded in the DRGroupStatus as a `StepStatus` entry with `Name` (e.g., `"SetSource"`, `"StartVM"`), `Status` (`"Succeeded"`/`"Failed"`), `Message`, and `Timestamp`. This mirrors the planned migration step recording pattern.

8. **AC8 — Controller integration:** Story 4.1b wires `FailoverHandler` for disaster mode. Verify the controller creates `FailoverHandler` with `FailoverConfig{GracefulShutdown: false, Force: true, RecordRPO: true}` when `exec.Spec.Mode == "disaster"`, passes it to the wave executor, and that `PreExecute` is skipped or no-op for that config. After execution completes, `CompleteTransition` advances the DRPlan phase as usual.

9. **AC9 — Unit tests:** Add disaster-specific tests in `pkg/engine/failover_test.go` (preferred) or `pkg/engine/disaster_test.go` with table-driven cases covering: (a) successful disaster failover — all DRGroups succeed, VMs started, RPO recorded; (b) SetSource failure — group fails, error includes step name and volume group; (c) StartVM failure — group fails; (d) GetReplicationStatus failure after successful SetSource — RPO recorded as "unknown", group succeeds; (e) per-step status recording — all steps recorded with correct names and timestamps; (f) empty DRGroup — no driver calls, succeeds immediately; (g) context cancellation — graceful exit; (h) multiple volume groups in one DRGroup — all promoted before any VM started; (i) `Force=true` and no `StopReplication` for disaster config. Tests use fake driver (`pkg/drivers/fake/`) and mock `VMManager`.

## Tasks / Subtasks

- [ ] Task 1: Implement disaster RPO recording in `FailoverHandler` (AC: #4, #5, #1)
  - [ ] 1.1 Extend `pkg/engine/failover.go` (`FailoverHandler` from Story 4.1b): when `FailoverConfig.RecordRPO` is true, after each successful `SetSource`, call `driver.GetReplicationStatus(ctx, vgID)` to read `LastSyncTime`
  - [ ] 1.2 Calculate RPO as `time.Since(*status.LastSyncTime)` if `LastSyncTime` is non-nil; otherwise RPO is `"unknown"`
  - [ ] 1.3 Include RPO in the SetSource `StepStatus.Message`: `"Set source for volume group %s (RPO: ~%s)"` or `"Set source for volume group %s (RPO: unknown)"`
  - [ ] 1.4 If `GetReplicationStatus` returns an error, log at V(1): `"Could not read replication status for RPO"` with volume group name and error, set RPO to `"unknown"` — do NOT fail the DRGroup
  - [ ] 1.5 Track maximum RPO across all volume groups in the handler's execution scope (field or local variable within `ExecuteGroup`, consistent with 4.1b patterns)

- [ ] Task 2: Unit tests — `FailoverHandler` with disaster config (AC: #9, #3, #6, #7)
  - [ ] 2.1 Add cases in `pkg/engine/failover_test.go` (preferred) or create `pkg/engine/disaster_test.go` in the same package
  - [ ] 2.2 Reuse shared test helpers (`mockVMManager`, fake driver setup) from existing engine tests
  - [ ] 2.3 Test: `TestFailover_Disaster_FullSuccess` — disaster config, all DRGroups succeed with `SetSource(force=true)`, VMs started, RPO recorded in step messages
  - [ ] 2.4 Test: `TestFailover_Disaster_SetSourceFails` — driver returns error on SetSource → group fails, step records failure, error includes volume group name
  - [ ] 2.5 Test: `TestFailover_Disaster_StartVMFails` — VMManager returns error → group fails, step records failure
  - [ ] 2.6 Test: `TestFailover_Disaster_GetReplicationStatusFails` — SetSource succeeds but GetReplicationStatus fails → RPO recorded as "unknown", group still succeeds
  - [ ] 2.7 Test: `TestFailover_Disaster_RPORecording` — verify RPO appears in StepStatus message for each volume group, max RPO tracked when `RecordRPO=true`
  - [ ] 2.8 Test: `TestFailover_Disaster_StepStatusRecorded` — verify all steps (SetSource per VG + StartVM per VM) recorded with correct names, timestamps, statuses
  - [ ] 2.9 Test: `TestFailover_Disaster_EmptyGroup` — no volume groups, ExecuteGroup succeeds trivially
  - [ ] 2.10 Test: `TestFailover_Disaster_ContextCancelled` — context cancelled mid-execution, returns ctx.Err()
  - [ ] 2.11 Test: `TestFailover_Disaster_ForceFlag` — verify `SetSourceOptions{Force: true}` is passed to driver for disaster config
  - [ ] 2.12 Test: `TestFailover_Disaster_NoStopReplication` — verify `StopReplication` is never called on the driver for disaster config
  - [ ] 2.13 Test: `TestFailover_Disaster_MultipleVolumeGroups` — multiple VGs in one group: all SetSource called before any StartVM, correct step ordering
  - [ ] 2.14 Test: `TestFailover_Disaster_PreExecute_NoGracefulShutdown` — `PreExecute` returns nil without graceful shutdown work when `GracefulShutdown=false`

- [ ] Task 3: Documentation, controller verification, and build (AC: #8, #1, #2)
  - [ ] 3.1 Update `pkg/engine/doc.go` to describe disaster mode as `FailoverConfig` on `FailoverHandler` (contrast with planned migration config)
  - [ ] 3.2 Add or extend godoc on `FailoverHandler` / `FailoverConfig` in `failover.go` explaining disaster mode: `GracefulShutdown=false`, `Force=true`, `RecordRPO=true`, origin error tolerance, RPO recording
  - [ ] 3.3 Verify `pkg/controller/drexecution/reconciler.go` constructs `FailoverHandler` with disaster `FailoverConfig` for `ExecutionModeDisaster` (Story 4.1b); adjust only if gaps remain
  - [ ] 3.4 Run `make manifests` to regenerate RBAC/webhook configs (in case any markers changed)
  - [ ] 3.5 Run `make generate` if types changed
  - [ ] 3.6 Run `make test` — all unit tests pass
  - [ ] 3.7 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 3.8 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4.4 of Epic 4 (DR Workflow Engine — Full Lifecycle). It extends the unified `FailoverHandler` introduced in Story 4.1b for disaster mode when the primary DC is unreachable. Stories 4.05 (driver registry convergence), 4.1 (state machine + controller), 4.1b (unified `FailoverHandler` + `FailoverConfig` + controller wiring for planned and disaster modes), 4.2 (wave executor framework), and 4.3 (planned migration path + VMManager patterns) are prerequisites.

**Story 4.4 scope:** Disaster-specific behavior on `FailoverHandler`: RPO recording when `FailoverConfig.RecordRPO=true`, with `GracefulShutdown=false` and `Force=true` for force-promoted `SetSource` and origin error tolerance. No Step 0, no replication stopping, no sync wait for disaster config. The VMManager interface and `KubeVirtVMManager` implementation come from Story 4.3 — this story reuses them via the unified handler.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite — must be done |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — must be done |
| 4.1b | Unified `FailoverHandler` + `FailoverConfig` + controller dispatch for planned vs disaster | Prerequisite — provides shared handler, graceful vs disaster branching |
| 4.2 | Wave executor framework + controller dispatch | Prerequisite — provides DRGroupHandler interface + WaveExecutor |
| 4.3 | Planned migration workflow + VMManager | Prerequisite — provides VMManager, step recording patterns, helpers reused by `failover.go` |
| **4.4** | **Disaster mode completion (RPO + tests + verification)** | **This story — extends 4.1b `FailoverHandler` for disaster RPO and validates config** |
| 4.5 | Fail-forward error handling & partial success | Enhances executor error handling |
| 4.6 | Failed DRGroup retry | Retry mechanism targeting specific failed groups |
| 4.7 | Checkpoint, resume & HA | Per-DRGroup persistence, async execution, pod restart resume |
| 4.8 | Re-protect & failback workflows | Reprotecting + FailingBack paths |

### Existing Code to Reuse (Critical — Do NOT Reinvent)

| File | What It Provides | How Disaster Mode Uses It |
|------|-----------------|-------------------------------|
| `pkg/engine/failover.go` (Story 4.1b) | `FailoverHandler`, `FailoverConfig{GracefulShutdown, Force, RecordRPO}`, per-DRGroup execution, step recording | Extend with RPO recording when `RecordRPO=true`; disaster path uses `Force=true`, skips graceful shutdown and `StopReplication` |
| `pkg/engine/vm.go` (Story 4.3) | `VMManager` interface, `KubeVirtVMManager`, `NoOpVMManager` | `FailoverHandler` calls `VMManager.StartVM` — same as planned migration |
| `pkg/engine/executor.go` (Story 4.2) | `WaveExecutor`, `DRGroupHandler`, `ExecutionGroup` | `FailoverHandler` implements `DRGroupHandler`; executor drives wave sequencing |
| `pkg/engine/statemachine.go` (Story 4.1) | `Transition`, `CompleteTransition` | Controller calls after execution completes |
| `pkg/drivers/interface.go` | `StorageProvider` — `SetSource`, `GetReplicationStatus`, `CreateVolumeGroup` | `SetSource(force=true)` and `GetReplicationStatus` for RPO when enabled |
| `pkg/drivers/types.go` | `VolumeGroupID`, `SetSourceOptions`, `ReplicationStatus`, `VolumeRole`, `ReplicationHealth` | All types used by the handler |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing without real storage |
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRExecution`, `DRGroupExecutionStatus`, `StepStatus`, `ExecutionMode`, `ExecutionResult`, `VolumeGroupInfo` | Status recording, step tracking, mode checking |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/engine/failover.go` | Story 4.1b introduces `FailoverHandler` and disaster vs planned branching | Add RPO recording when `RecordRPO=true` (if not already present from 4.1b); keep disaster path aligned with AC |
| `pkg/controller/drexecution/reconciler.go` | Story 4.1b wires `FailoverHandler` with mode-specific `FailoverConfig` | Verify disaster config; adjust only if wiring or `PreExecute` gating is incomplete |
| `pkg/engine/doc.go` | Covers discovery, consistency, chunking, wave executor, planned migration | Document disaster mode as `FailoverConfig` on unified handler |

### Shared Code (Stories 4.1b / 4.3)

`FailoverHandler` in `failover.go` should already consolidate helpers and step constants previously associated with `planned.go`. If Story 4.1b left helpers in `planned.go`, reuse through the unified handler rather than reintroducing a separate disaster file. Do not duplicate `resolveVolumeGroupID` / step recording logic if 4.1b centralizes it.

### New Files to Create

| File | Purpose |
|------|---------|
| *(optional)* `pkg/engine/disaster_test.go` | Additional disaster-only tests if not folded into `failover_test.go` |

No new `disaster.go` — disaster behavior lives in `failover.go` with `FailoverConfig`.

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/engine/planned.go` | Legacy or thin wrapper — avoid unrelated refactors; prefer changes in `failover.go` per 4.1b |
| `pkg/engine/planned_test.go` | Planned migration tests — no changes unless shared test extraction is already agreed |
| `pkg/engine/executor.go` | Executor framework — `FailoverHandler` implements its interface, does not modify executor internals |
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

`FailoverHandler` with `FailoverConfig{GracefulShutdown: false, Force: true, RecordRPO: true}` implements:
- `PreExecute(ctx, groups)` — returns nil when `GracefulShutdown=false` (no graceful shutdown work)
- `ExecuteGroup(ctx, group)` — per-DRGroup handler with force-promote and optional RPO recording

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

When `FailoverConfig.RecordRPO=true`, after each successful `SetSource`, `FailoverHandler` queries `GetReplicationStatus` to read `LastSyncTime`. RPO is calculated as `time.Since(*lastSyncTime)`. This is informational only — it does not gate execution. If the query fails (origin metadata unavailable), RPO is recorded as `"unknown"`.

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

**6. Controller dispatch is owned by Story 4.1b.**

Story 4.1b constructs one `FailoverHandler` type with mode-specific `FailoverConfig` (planned vs disaster). Story 4.4 verifies disaster wiring and completes any missing RPO behavior; it does not introduce a second handler type.

```go
// Illustrative — align with actual 4.1b reconciler code
handler := &engine.FailoverHandler{
    Driver:    resolvedDriver,
    VMManager: r.VMManager,
    Config:    failoverConfigForMode(exec.Spec.Mode),
}
```

**7. VolumeGroupID resolution reuses the same pattern as planned migration.**

The `resolveVolumeGroupID` helper (in `failover.go` or shared engine helpers after 4.1b) calls `driver.CreateVolumeGroup(spec)` which is idempotent. Reuse the implementation from Story 4.1b / 4.3 consolidation — do not fork.

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
4. NO Step 0 — `FailoverConfig.GracefulShutdown=false` skips graceful shutdown; proceed directly to per-wave execution
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

| Aspect | Planned migration config (`FailoverHandler`) | Disaster config (`FailoverHandler`) |
|--------|------|---------|
| Step 0 (PreExecute) | `GracefulShutdown=true` — stop VMs, stop replication, wait sync | `GracefulShutdown=false` — returns nil without that work |
| SetSource force | `Force=false` when both sites healthy | `Force=true` — force promote |
| StopReplication | Called in graceful path (Step 0 / per-DRGroup as designed) | Not called for disaster config |
| Origin VM handling | Gracefully stopped when graceful shutdown enabled | Ignored (origin assumed unreachable) |
| RPO guarantee | RPO=0 (sync guaranteed) by design | RPO>0 (data loss since last sync) |
| RPO recording | Typically off or informational per config | `RecordRPO=true` — from `GetReplicationStatus.LastSyncTime` |
| Origin errors | Fail execution in graceful path | Log and ignore for RPO/status reads per AC |
| Sync timeout config | Planned fields on config/handler | Not applicable for disaster |
| Handler type | `FailoverHandler` | Same `FailoverHandler`, different `FailoverConfig` |

### Volume Role Transitions During Disaster Failover

```
Origin volumes:    Source → (unreachable, no transition applied)
Target volumes:    Target → Source (via SetSource force=true — driver handles internal transitions)
```

In disaster mode, the driver's `SetSource(force=true)` handles the Target→Source promotion internally, potentially skipping the NonReplicated intermediate state or handling it atomically. The orchestrator does not need to manage role transition ordering — `Force` delegates that responsibility to the driver.

### StepStatus Recording

`FailoverHandler` in disaster mode records the same step types as planned migration, minus `StopReplication`:

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

**Unit tests** (`pkg/engine/failover_test.go` or optional `disaster_test.go` in the same package): Use fake driver from `pkg/drivers/fake/`, mock `VMManager`, construct `FailoverHandler` with disaster `FailoverConfig`, and verify:
- Force flag: `SetSourceOptions{Force: true}` passed to every SetSource call when `Config.Force=true`
- No StopReplication: verify driver's `StopReplication` is never called for disaster config
- Step ordering: all SetSource before any StartVM within a DRGroup
- RPO recording: when `RecordRPO=true`, `GetReplicationStatus` called after each SetSource, RPO in message
- RPO tolerance: `GetReplicationStatus` failure does not fail the group
- Error handling: SetSource failure and StartVM failure produce correct error messages
- StepStatus recording: all operations recorded with names and timestamps
- Context cancellation: graceful exit
- PreExecute with `GracefulShutdown=false`: returns nil without graceful shutdown driver/VM work

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

**From Story 4.1b (Unified Failover Handler):**
- `FailoverHandler` with `FailoverConfig{GracefulShutdown, Force, RecordRPO}` — single type for planned and disaster modes; Story 4.4 adds or completes `RecordRPO` behavior and disaster-focused tests
- Controller should already pass disaster `FailoverConfig` when `ExecutionModeDisaster` — verify and patch gaps only

**From Story 4.3 (Planned Migration Workflow):**
- Planned path patterns (volume resolution, step names, `StepStatus` shape) live in or feed `failover.go` after 4.1b — follow existing constants and helpers there
- `resolveVolumeGroupID` helper: calls `driver.CreateVolumeGroup(spec)` idempotently — reuse from unified handler
- Step name constants: `StepSetSource`, `StepStartVM`, `StepStopReplication` — disaster path omits `StopReplication`
- Step recording pattern: `StepStatus{Name, Status, Message, Timestamp}` with `metav1.Now()` — follow exactly
- `mockVMManager` test helper: configurable success/failure, records calls — reuse from engine tests
- VMManager `StartVM` is idempotent: returns nil if already running
- Per-DRGroup execution: all volume operations before any VM operations — follow this ordering
- Error wrapping style: `"<operation> for <subject>: %w"` — follow exactly
- PVC name resolution note: for Story 4.3/4.4, pass empty PVC names to `CreateVolumeGroup` if the driver resolves PVCs internally. The no-op/fake driver does not require PVC names

**From Story 4.2 (DRGroup Chunking & Wave Executor):**
- `DRGroupHandler` is a single-method interface: `ExecuteGroup(ctx, group ExecutionGroup) error`
- `ExecutionGroup` bundles `DRGroupChunk` + resolved `StorageProvider` driver + `WaveIndex int`
- The executor runs synchronously in the reconcile loop (async is Story 4.7)
- Status updates use mutex-serialized writes via `updateGroupStatus`
- The controller calls `PreExecute` when appropriate for the handler; for `FailoverHandler` with `GracefulShutdown=false`, `PreExecute` returns nil without origin work
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

Implementation centers on the unified handler (update `architecture.md` project structure if it still lists `pkg/engine/disaster.go`):
- `pkg/engine/failover.go` — `FailoverHandler`, `FailoverConfig`, planned and disaster execution paths
- `pkg/engine/failover_test.go` — include disaster-mode cases (optional `disaster_test.go` for extra grouping)
- `pkg/controller/drexecution/reconciler.go` — verify `FailoverHandler` construction for disaster mode (Story 4.1b)
- No changes to `cmd/soteria/main.go` — wiring remains in the controller from Story 4.1b

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.4] — Story acceptance criteria (BDD format)
- [Source: _bmad-output/planning-artifacts/prd.md#FR10] — Disaster recovery execution: force-promote, start VMs, ignore origin errors
- [Source: _bmad-output/planning-artifacts/prd.md#FR11] — Wave-sequential, within-wave concurrent execution
- [Source: _bmad-output/planning-artifacts/prd.md#FR13] — Fail-forward error handling with PartiallySucceeded reporting
- [Source: _bmad-output/planning-artifacts/prd.md#FR18] — Human-triggered only, no auto-failover
- [Source: _bmad-output/planning-artifacts/prd.md#FR19] — Execution mode specified at runtime, not on plan
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — align with `pkg/engine/failover.go` (unified handler); update architecture if it still references `disaster.go`
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
- [Source: _bmad-output/implementation-artifacts/4-1b-state-machine-symmetry-unified-failover-handler.md] — Prerequisite: unified `FailoverHandler`, `FailoverConfig`, controller wiring
- [Source: _bmad-output/implementation-artifacts/4-3-planned-migration-workflow.md] — Previous story: planned migration path, VMManager, step recording patterns feeding the unified handler
- [Source: _bmad-output/implementation-artifacts/4-2-drgroup-chunking-wave-executor.md] — Previous story: executor framework, DRGroupHandler interface, ExecutionGroup, fail-forward semantics
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — Previous story: state machine, controller setup, admission webhook
- [Source: _bmad-output/implementation-artifacts/4-05-driver-registry-fallback-preflight-convergence.md] — Previous story: registry fallback, noop-fallback flag

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
