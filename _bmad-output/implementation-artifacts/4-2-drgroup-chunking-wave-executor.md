# Story 4.2: DRGroup Chunking & Wave Executor

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want waves executed sequentially with DRGroups within a wave executed concurrently, respecting maxConcurrentFailovers throttling,
So that failover proceeds in a controlled, wave-ordered manner without exhausting storage or compute resources.

## Acceptance Criteria

1. **AC1 — Wave executor framework:** `pkg/engine/executor.go` defines a `WaveExecutor` that accepts a DRPlan, DRExecution, and a `DRGroupHandler` interface. Waves are executed strictly in sequence: wave N+1 does not start until wave N completes (FR11). Within each wave, DRGroup chunks execute concurrently using goroutines bounded by the number of chunks.

2. **AC2 — DRGroupHandler interface:** `pkg/engine/executor.go` defines a `DRGroupHandler` interface with a single method `ExecuteGroup(ctx context.Context, group ExecutionGroup) error` that Stories 4.3 (planned migration) and 4.4 (disaster failover) will implement. The executor calls this handler for each DRGroup chunk without knowing which driver methods are invoked — it is workflow-agnostic.

3. **AC3 — Execution pipeline reuse:** The executor reuses the existing discovery → consistency → chunking pipeline (`VMDiscoverer.DiscoverVMs` → `ResolveVolumeGroups` → `ChunkWaves`) at execution start to obtain the authoritative list of waves and DRGroup chunks. It does NOT rely on stale `DRPlan.Status.Waves` from the last reconcile cycle.

4. **AC4 — Per-DRGroup status tracking:** After each DRGroup completes (success or failure), the executor updates `DRExecution.Status.Waves[].Groups[]` with: `Result` (Pending → InProgress → Completed/Failed), `StartTime`, `CompletionTime`, `Error` (if failed), and `VMNames`. Per-wave `StartTime` and `CompletionTime` are also recorded. All status writes go through the Kubernetes status subresource.

5. **AC5 — Overall execution result:** When all waves complete: if every DRGroup is `Completed`, the execution `Result` is `Succeeded`. If any DRGroup is `Failed` but at least one succeeded, the result is `PartiallySucceeded`. If the executor cannot start at all (e.g., discovery fails), the result is `Failed`. `CompletionTime` is set when the executor finishes.

6. **AC6 — Fail-forward within a wave:** When a DRGroup fails, other DRGroups in the same wave continue executing. A failed wave does NOT block subsequent waves — the executor proceeds to the next wave (FR13). Failed DRGroups are recorded but do not halt the engine.

7. **AC7 — Controller integration:** The DRExecution controller in `pkg/controller/drexecution/reconciler.go` is enhanced to: after the state machine validates and transitions the plan (Story 4.1), dispatch the wave executor in a goroutine, passing the appropriate `DRGroupHandler`. Until Stories 4.3/4.4 provide real handlers, a no-op handler is used that succeeds immediately (allowing the full executor loop to be tested).

8. **AC8 — Context cancellation:** The executor respects `context.Context` cancellation. If the context is cancelled (e.g., pod shutdown), in-flight DRGroups are cancelled and the executor writes the current state to `DRExecution.Status` before returning. DRGroups that had not started remain `Pending`.

9. **AC9 — ExecutionGroup type:** The executor defines an `ExecutionGroup` struct that bundles a `DRGroupChunk` with its resolved `StorageProvider` driver (resolved via the driver registry from PVC storage classes). The `DRGroupHandler` receives this bundle so it does not need to resolve drivers itself.

10. **AC10 — Unit tests:** The executor has table-driven and scenario tests covering: (a) single wave with one chunk — succeeds; (b) multiple waves execute sequentially; (c) multiple DRGroups in a wave execute concurrently; (d) one DRGroup fails, others continue (fail-forward); (e) failed wave does not block next wave; (f) context cancellation stops execution; (g) discovery failure returns Failed result; (h) empty plan (no VMs) returns Succeeded with zero waves; (i) status is correctly populated with per-wave and per-group details. Tests use the fake driver from `pkg/drivers/fake/` and mock `VMDiscoverer`/`NamespaceLookup`.

## Tasks / Subtasks

- [ ] Task 1: Define executor types and DRGroupHandler interface (AC: #1, #2, #9)
  - [ ] 1.1 Create `pkg/engine/executor.go` with copyright header and package doc block comment
  - [ ] 1.2 Define `DRGroupHandler` interface: `ExecuteGroup(ctx context.Context, group ExecutionGroup) error`
  - [ ] 1.3 Define `ExecutionGroup` struct: `DRGroupChunk` + `Driver drivers.StorageProvider` + `WaveIndex int`
  - [ ] 1.4 Define `WaveExecutor` struct with fields: `Client client.Client`, `VMDiscoverer VMDiscoverer`, `NamespaceLookup NamespaceLookup`, `Registry *drivers.Registry`, `SCLister drivers.StorageClassLister`
  - [ ] 1.5 Define `ExecuteInput` struct: `Execution *soteriav1alpha1.DRExecution`, `Plan *soteriav1alpha1.DRPlan`, `Handler DRGroupHandler`

- [ ] Task 2: Implement the execution pipeline (AC: #1, #3)
  - [ ] 2.1 Implement `WaveExecutor.Execute(ctx context.Context, input ExecuteInput) error` as the main entry point
  - [ ] 2.2 Step 1: Discover VMs using `e.VMDiscoverer.DiscoverVMs(ctx, plan.Name)` — re-discovers at execution time
  - [ ] 2.3 Step 2: Group by wave using `GroupByWave(vms, plan.Spec.WaveLabel)`
  - [ ] 2.4 Step 3: Resolve volume groups using `ResolveVolumeGroups(ctx, vms, plan.Spec.WaveLabel, e.NamespaceLookup)`
  - [ ] 2.5 Step 4: Chunk waves using `ChunkWaves(chunkInput, plan.Spec.MaxConcurrentFailovers)` — reuse existing chunker
  - [ ] 2.6 Step 5: Initialize `DRExecution.Status.Waves` with wave count and per-group Pending entries
  - [ ] 2.7 Write initial status via `r.Status().Update()`

- [ ] Task 3: Implement sequential wave execution with concurrent DRGroups (AC: #1, #4, #6, #8)
  - [ ] 3.1 Implement `executeWave(ctx context.Context, waveIdx int, chunks []DRGroupChunk, handler DRGroupHandler, exec *DRExecution) error` — runs all chunks in a wave concurrently
  - [ ] 3.2 Set wave `StartTime` at wave start, `CompletionTime` when all groups finish
  - [ ] 3.3 For each DRGroup chunk: launch goroutine, call `executeGroup(ctx, waveIdx, groupIdx, chunk, handler, exec)`
  - [ ] 3.4 Use `errgroup.Group` (from `golang.org/x/sync/errgroup`) to manage concurrent goroutines — collect errors without stopping siblings (fail-forward: do NOT use errgroup's cancel-on-first-error; instead collect all results)
  - [ ] 3.5 Actually use a `sync.WaitGroup` + per-group error channel or result slice instead of errgroup, since errgroup cancels context on error and we need fail-forward semantics
  - [ ] 3.6 After all groups complete, determine wave outcome and continue to next wave regardless of failures (fail-forward)
  - [ ] 3.7 Check `ctx.Err()` before starting each wave — if cancelled, write current state and return

- [ ] Task 4: Implement per-DRGroup execution (AC: #4, #9)
  - [ ] 4.1 Implement `executeGroup(ctx context.Context, waveIdx, groupIdx int, chunk DRGroupChunk, handler DRGroupHandler, exec *DRExecution) DRGroupResult`
  - [ ] 4.2 Set group status to `InProgress` with `StartTime = metav1.Now()`
  - [ ] 4.3 Resolve the `StorageProvider` driver for the group's VMs via `e.Registry` and `e.SCLister` — for groups with mixed storage classes, resolve per-VM (or use the first VM's storage class for the group); package into `ExecutionGroup`
  - [ ] 4.4 Call `handler.ExecuteGroup(ctx, executionGroup)` — this is the pluggable workflow step
  - [ ] 4.5 On success: set group `Result = Completed`, `CompletionTime = metav1.Now()`
  - [ ] 4.6 On failure: set group `Result = Failed`, `Error = err.Error()`, `CompletionTime = metav1.Now()`
  - [ ] 4.7 After each group completes, update `DRExecution.Status` via status subresource

- [ ] Task 5: Implement overall result calculation and completion (AC: #5)
  - [ ] 5.1 After all waves complete, compute overall `ExecutionResult`: scan all groups — all Completed → `Succeeded`, any Failed + any Completed → `PartiallySucceeded`, all Failed or pre-execution failure → `Failed`
  - [ ] 5.2 Set `DRExecution.Status.Result` and `CompletionTime`
  - [ ] 5.3 Call `engine.CompleteTransition(plan.Status.Phase)` to advance the DRPlan to its completion phase (e.g., FailingOver → FailedOver) — only if result is Succeeded or PartiallySucceeded
  - [ ] 5.4 Update DRPlan status via status subresource
  - [ ] 5.5 Emit events on the DRExecution and DRPlan for completion

- [ ] Task 6: Create no-op DRGroupHandler for testing (AC: #7)
  - [ ] 6.1 Create `pkg/engine/handler_noop.go` with `NoOpHandler` struct implementing `DRGroupHandler`
  - [ ] 6.2 `ExecuteGroup` returns nil immediately — used until Stories 4.3/4.4 provide real workflow handlers
  - [ ] 6.3 This handler is the default in the controller until real handlers are wired

- [ ] Task 7: Enhance DRExecution controller to dispatch executor (AC: #7)
  - [ ] 7.1 Add `WaveExecutor *engine.WaveExecutor` field to `DRExecutionReconciler`
  - [ ] 7.2 Add `Handler engine.DRGroupHandler` field to `DRExecutionReconciler` (injected; defaults to `NoOpHandler`)
  - [ ] 7.3 After Story 4.1's state machine validation and transition, call `e.WaveExecutor.Execute(ctx, input)` — execute synchronously within the reconcile loop (the reconciler will be enhanced for async in Story 4.7 when checkpoint/resume is added)
  - [ ] 7.4 After execution completes, call `CompleteTransition` to advance the DRPlan phase
  - [ ] 7.5 Update DRExecution status with final result
  - [ ] 7.6 Emit completion event on DRPlan and DRExecution

- [ ] Task 8: Update main.go wiring (AC: #7)
  - [ ] 8.1 Create `WaveExecutor` with `VMDiscoverer`, `NamespaceLookup`, `Registry`, `SCLister` from existing resolver infrastructure
  - [ ] 8.2 Pass `WaveExecutor` and `NoOpHandler` to `DRExecutionReconciler`
  - [ ] 8.3 Ensure `VMDiscoverer` and `NamespaceLookup` instances are shared between DRPlan and DRExecution controllers

- [ ] Task 9: Wave executor unit tests (AC: #10)
  - [ ] 9.1 Create `pkg/engine/executor_test.go`
  - [ ] 9.2 Define `mockDRGroupHandler` implementing `DRGroupHandler` — configurable success/failure per group name, records call order
  - [ ] 9.3 Test: `TestWaveExecutor_SingleWave_SingleChunk_Succeeds` — one wave, one chunk, handler succeeds → Result=Succeeded
  - [ ] 9.4 Test: `TestWaveExecutor_MultipleWaves_Sequential` — 3 waves, verify wave N+1 starts after wave N completes (use timing or call ordering)
  - [ ] 9.5 Test: `TestWaveExecutor_ConcurrentDRGroups` — wave with 3 chunks, verify all 3 start before any completes (use barrier/waitgroup in mock handler)
  - [ ] 9.6 Test: `TestWaveExecutor_FailForward_GroupFails` — one DRGroup fails, others complete → PartiallySucceeded
  - [ ] 9.7 Test: `TestWaveExecutor_FailForward_FailedWaveDoesNotBlockNext` — wave 1 has a failed group, wave 2 still executes
  - [ ] 9.8 Test: `TestWaveExecutor_ContextCancelled` — cancel context mid-execution, verify graceful stop
  - [ ] 9.9 Test: `TestWaveExecutor_DiscoveryFailure_ReturnsFailed` — VMDiscoverer returns error → Result=Failed
  - [ ] 9.10 Test: `TestWaveExecutor_EmptyPlan_Succeeds` — no VMs discovered → Result=Succeeded with zero waves
  - [ ] 9.11 Test: `TestWaveExecutor_StatusPopulated` — verify StartTime, CompletionTime, VMNames, per-group details are all set
  - [ ] 9.12 Test: `TestWaveExecutor_AllGroupsFail_ResultFailed` — every group fails → Result=Failed (not PartiallySucceeded)

- [ ] Task 10: Integration tests (AC: #10)
  - [ ] 10.1 Add DRExecution controller integration tests to `test/integration/controller/` that verify: create DRPlan in SteadyState with VMs → create DRExecution → controller dispatches executor → DRExecution status has waves/groups → plan transitions
  - [ ] 10.2 Test with no-op handler: execution should complete with Succeeded, plan advances to FailedOver
  - [ ] 10.3 Verify events emitted on DRPlan and DRExecution

- [ ] Task 11: Update documentation and verify (AC: #1)
  - [ ] 11.1 Update `pkg/engine/doc.go` to cover the wave executor and DRGroupHandler interface
  - [ ] 11.2 Add godoc block comment on `executor.go` explaining the execution pipeline: discover → group → chunk → execute waves → checkpoint
  - [ ] 11.3 Update RBAC markers on DRExecution reconciler if new permissions needed
  - [ ] 11.4 Run `make manifests` to regenerate RBAC/webhook configs
  - [ ] 11.5 Run `make generate` if types changed
  - [ ] 11.6 Run `make test` — all unit tests pass
  - [ ] 11.7 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 11.8 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4.2 of Epic 4 (DR Workflow Engine — Full Lifecycle). It creates the wave executor — the runtime orchestration engine that drives DR operations wave by wave. Story 4.1 (state machine + controller setup) is a prerequisite. Story 4.05 (driver registry convergence) is also a prerequisite as the executor resolves drivers at runtime.

**Story 4.2 scope:** The executor framework, wave sequencing, concurrent DRGroup execution, status management, and controller integration. The **actual per-DRGroup workflow steps** (which driver methods to call in what order) are deferred to Stories 4.3 (planned migration) and 4.4 (disaster failover). A `DRGroupHandler` interface abstracts the per-group workflow, and a no-op handler enables full end-to-end testing of the executor loop.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite — must be done |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — must be done |
| **4.2** | **Wave executor framework + controller dispatch** | **This story** |
| 4.3 | Planned migration workflow | Implements `DRGroupHandler` for planned_migration mode |
| 4.4 | Disaster failover workflow | Implements `DRGroupHandler` for disaster mode |
| 4.5 | Fail-forward error handling & partial success | Enhances executor error handling (4.2 provides the foundation) |
| 4.6 | Failed DRGroup retry | Retry mechanism targeting specific failed groups |
| 4.7 | Checkpoint, resume & HA | Per-DRGroup persistence, async execution, pod restart resume |
| 4.8 | Re-protect & failback workflows | Reprotecting + FailingBack paths |

### Existing Code to Reuse (Critical — Do NOT Reinvent)

| File | What It Provides | How Executor Uses It |
|------|-----------------|---------------------|
| `pkg/engine/discovery.go` | `VMDiscoverer` interface, `TypedVMDiscoverer`, `GroupByWave` | Re-discover VMs at execution start |
| `pkg/engine/consistency.go` | `ResolveVolumeGroups`, `NamespaceLookup` | Resolve volume groups for discovered VMs |
| `pkg/engine/chunker.go` | `ChunkWaves`, `DRGroupChunk`, `ChunkResult` | Partition VolumeGroups into DRGroup chunks per wave |
| `pkg/engine/statemachine.go` | `Transition`, `CompleteTransition` (Story 4.1) | Advance DRPlan phase after execution completes |
| `pkg/drivers/registry.go` | `Registry.GetDriver`, `GetDriverForPVC` | Resolve driver for each DRGroup's VMs |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing the executor without real storage |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/controller/drexecution/reconciler.go` | Story 4.1 adds state machine validation + transition; returns `ctrl.Result{}, nil` after setup | Add `WaveExecutor` and `Handler` fields; after state machine setup, call `executor.Execute()` to dispatch engine; write final result |
| `cmd/soteria/main.go` | Story 4.1 adds `Recorder` to `DRExecutionReconciler` | Add `WaveExecutor` and `Handler` wiring; share `VMDiscoverer` and `NamespaceLookup` between DRPlan and DRExecution controllers |
| `pkg/engine/doc.go` | Covers discovery, consistency, chunking | Add wave executor and DRGroupHandler documentation |

### New Files to Create

| File | Purpose |
|------|---------|
| `pkg/engine/executor.go` | Wave executor — sequential waves, concurrent DRGroups, status management |
| `pkg/engine/executor_test.go` | Comprehensive executor unit tests |
| `pkg/engine/handler_noop.go` | No-op DRGroupHandler for testing the executor loop |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/engine/chunker.go` | Already complete and tested — executor calls `ChunkWaves`, does not modify it |
| `pkg/engine/chunker_test.go` | 100% coverage on chunking scenarios — no changes |
| `pkg/engine/discovery.go` | VM discovery — no changes |
| `pkg/engine/discovery_test.go` | Discovery tests — no changes |
| `pkg/engine/consistency.go` | Consistency resolution — no changes |
| `pkg/engine/consistency_test.go` | Consistency tests — no changes |
| `pkg/engine/statemachine.go` | Story 4.1 creates this — executor calls it but does not modify |
| `pkg/apis/soteria.io/v1alpha1/types.go` | Types are already complete — use them as-is |
| `pkg/drivers/interface.go` | Stable StorageProvider interface |
| `pkg/drivers/registry.go` | Driver registry — use, don't modify |
| `pkg/drivers/fake/driver.go` | Fake driver for tests — use, don't modify |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes |
| `pkg/admission/*` | Admission webhooks — no changes |

### Key Implementation Decisions

**1. The executor runs the full discovery → consistency → chunking pipeline at execution time.**
Do NOT use `DRPlan.Status.Waves` or `DRPlan.Status.Preflight` for execution. These are stale from the last reconcile cycle. Re-running discovery ensures the executor works with the current VM set. The pipeline is fast (< 10s per NFR11) and reuses the same functions the DRPlan controller already calls.

**2. DRGroupHandler is a simple single-method interface.**
```go
type DRGroupHandler interface {
    ExecuteGroup(ctx context.Context, group ExecutionGroup) error
}
```
Stories 4.3 and 4.4 implement this. Story 4.2 provides `NoOpHandler` that returns nil. The executor does not know or care what happens inside a DRGroup — it only manages wave sequencing, concurrency, and status.

**3. Fail-forward means NO early termination on DRGroup failure.**
When a DRGroup fails, the executor records the error and continues with remaining groups in the wave. After the wave completes, it proceeds to the next wave. The executor never stops due to a single group failure. Only context cancellation or discovery-level failures cause early termination.

Do NOT use `golang.org/x/sync/errgroup` for within-wave concurrency — `errgroup.WithContext` cancels all sibling goroutines when one returns an error, which is the opposite of fail-forward. Use `sync.WaitGroup` + per-group result collection instead.

**4. Status updates happen after each DRGroup completes.**
This provides real-time visibility into execution progress. Each update is a status subresource write via `r.Status().Update(ctx, exec)`. Note: concurrent DRGroups must not race on status writes — serialize status updates using a mutex or channel within the executor.

**5. The executor runs synchronously in the reconcile loop for now.**
Story 4.7 (Checkpoint, Resume & HA) will move execution to an async model with checkpointing. For Story 4.2, the executor blocks the reconcile goroutine. This is acceptable because:
- Only one DRExecution is active per plan at a time
- The idempotency check (`startTime != nil`) prevents re-dispatch on re-reconcile
- Story 4.7 will introduce the async model with goroutine management

**6. Driver resolution: one driver per DRGroup based on VolumeGroup storage class.**
Each DRGroup chunk contains VolumeGroups. The executor resolves the storage driver for each VolumeGroup by looking up the PVC storage class → provisioner → registry. For Story 4.2, the `ExecutionGroup` bundles the chunk with a resolved driver. If different VolumeGroups within a chunk have different drivers (heterogeneous storage), the handler receives one `ExecutionGroup` per VolumeGroup (Story 4.3/4.4 will handle this). For now, the executor resolves the driver for the first VM's PVC in the chunk — this covers the common case. Story 4.5 will refine multi-driver handling.

**7. CompleteTransition is called after execution finishes.**
The state machine `CompleteTransition(plan.Status.Phase)` advances the DRPlan from its in-progress phase (e.g., FailingOver) to the next steady state (e.g., FailedOver). This is called only when the overall result is `Succeeded` or `PartiallySucceeded`. If the execution `Failed` entirely, the plan phase is not advanced — it stays in the in-progress phase for manual intervention.

### DRExecution Lifecycle (After Story 4.2)

```
1. Operator creates DRExecution (kubectl/Console)
   ↓
2. Admission webhook validates (Story 4.1: planName, mode, plan phase)
   ↓
3. Controller reconciles:
   a. Idempotency check — startTime already set? → skip
   b. State machine validates transition (Story 4.1)
   c. Transition DRPlan phase: SteadyState → FailingOver (Story 4.1)
   d. Set DRExecution.Status.StartTime (Story 4.1)
   ↓
4. Wave executor dispatched (Story 4.2 — THIS STORY):
   a. Discover VMs → group by wave → resolve consistency → chunk
   b. Initialize DRExecution.Status.Waves with Pending groups
   c. For each wave (sequential):
      - Set wave StartTime
      - For each DRGroup (concurrent):
        * Set group InProgress
        * Resolve driver via registry
        * Call handler.ExecuteGroup() → NoOpHandler returns nil
        * Set group Completed/Failed with CompletionTime
        * Write status checkpoint
      - Set wave CompletionTime
   d. Compute overall result (Succeeded/PartiallySucceeded/Failed)
   e. CompleteTransition: FailingOver → FailedOver
   f. Set DRExecution CompletionTime + final result
   ↓
5. Reconcile returns ctrl.Result{}, nil
```

### Status Structure During Execution

```yaml
status:
  result: InProgress  # → Succeeded | PartiallySucceeded | Failed
  startTime: "2026-04-18T10:00:00Z"
  completionTime: null  # set when executor finishes
  waves:
    - waveIndex: 0
      startTime: "2026-04-18T10:00:01Z"
      completionTime: "2026-04-18T10:00:05Z"
      groups:
        - name: "wave-alpha-group-0"
          result: Completed
          vmNames: ["vm-web01", "vm-web02"]
          startTime: "2026-04-18T10:00:01Z"
          completionTime: "2026-04-18T10:00:03Z"
        - name: "wave-alpha-group-1"
          result: Failed
          vmNames: ["vm-db01"]
          error: "setting volume vm-db01 to source: replication not ready"
          startTime: "2026-04-18T10:00:01Z"
          completionTime: "2026-04-18T10:00:04Z"
    - waveIndex: 1
      startTime: "2026-04-18T10:00:06Z"
      completionTime: "2026-04-18T10:00:08Z"
      groups:
        - name: "wave-beta-group-0"
          result: Completed
          vmNames: ["vm-api01"]
          startTime: "2026-04-18T10:00:06Z"
          completionTime: "2026-04-18T10:00:08Z"
  conditions:
    - type: Progressing
      status: "True"
      reason: ExecutionStarted
```

### Concurrency Within a Wave — Do NOT Use errgroup

```go
func (e *WaveExecutor) executeWave(
    ctx context.Context, waveIdx int, chunks []DRGroupChunk,
    handler DRGroupHandler, exec *soteriav1alpha1.DRExecution,
) {
    var wg sync.WaitGroup
    results := make([]error, len(chunks))

    for i, chunk := range chunks {
        wg.Add(1)
        go func(idx int, c DRGroupChunk) {
            defer wg.Done()
            results[idx] = e.executeGroup(ctx, waveIdx, idx, c, handler, exec)
        }(i, chunk)
    }

    wg.Wait()
}
```

### Status Update Serialization

Concurrent DRGroups must not race on `DRExecution.Status` writes. Use a mutex:

```go
type WaveExecutor struct {
    Client         client.Client
    // ...
    statusMu       sync.Mutex
}

func (e *WaveExecutor) updateGroupStatus(
    ctx context.Context, exec *soteriav1alpha1.DRExecution,
    waveIdx, groupIdx int, status soteriav1alpha1.DRGroupExecutionStatus,
) error {
    e.statusMu.Lock()
    defer e.statusMu.Unlock()

    // Re-fetch exec to get latest resourceVersion before update
    if err := e.Client.Get(ctx, client.ObjectKeyFromObject(exec), exec); err != nil {
        return err
    }
    exec.Status.Waves[waveIdx].Groups[groupIdx] = status
    return e.Client.Status().Update(ctx, exec)
}
```

Re-fetching before update is critical to avoid `409 Conflict` errors when multiple goroutines update the same DRExecution concurrently. The mutex serializes writes; the re-fetch ensures the latest `resourceVersion`.

### Driver Resolution for DRGroups

Each `DRGroupChunk` contains `VolumeGroups` which have `VMNames`. To resolve the driver:
1. For each VM in the chunk, look up its PVCs via `CoreClient.PersistentVolumeClaims(namespace).List()` (or use a PVC cache)
2. Get the PVC's `storageClassName`
3. Call `Registry.GetDriver(provisioner)` via `SCLister.GetProvisioner(ctx, storageClassName)`

For Story 4.2, the driver resolution can be simplified: resolve the driver for the first PVC found in the group. Story 4.5 (fail-forward error handling) will handle mixed-driver groups and per-VolumeGroup driver resolution.

If driver resolution fails for a DRGroup (e.g., `ErrDriverNotFound`), the group is marked `Failed` with the error — fail-forward continues with remaining groups.

### RBAC Requirements

The DRExecution controller already has RBAC markers from Story 4.1:
```go
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
```

The executor may need additional RBAC for VM reads (already on the DRPlan controller — verify the DRExecution controller has `kubevirt.io` VM list/watch). If using PVC reads for driver resolution, ensure `core/v1/persistentvolumeclaims` get/list is in RBAC markers.

### Code Patterns to Follow

**Structured logging (controller-runtime convention):**
```go
logger := log.FromContext(ctx)
logger.Info("Starting wave execution", "wave", waveIdx, "chunks", len(chunks))
logger.Info("DRGroup completed", "wave", waveIdx, "group", chunk.Name, "result", "Completed")
logger.Error(err, "DRGroup failed", "wave", waveIdx, "group", chunk.Name)
```

**Event recording (established pattern from DRPlan controller):**
```go
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "ExecutionCompleted", "WaveExecution",
    "Execution completed for plan %s: %s", plan.Name, exec.Status.Result)
```

**Status condition pattern:**
```go
meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
    Type:               "Progressing",
    Status:             metav1.ConditionFalse,
    Reason:             "ExecutionCompleted",
    Message:            fmt.Sprintf("Execution completed: %s", result),
    ObservedGeneration: exec.Generation,
})
```

**Error wrapping (project convention):**
```go
return fmt.Errorf("executing wave %d group %s: %w", waveIdx, chunk.Name, err)
```

### Test Strategy

**Unit tests** (`pkg/engine/executor_test.go`): Use mock `VMDiscoverer`, mock `NamespaceLookup`, fake driver, and mock `DRGroupHandler`. Verify:
- Wave ordering (sequential execution via call timestamps or barriers)
- DRGroup concurrency (multiple groups start before any completes)
- Fail-forward (failed group doesn't block siblings or next wave)
- Status population (all fields correctly set)
- Context cancellation (graceful stop)

**Mock DRGroupHandler:**
```go
type mockHandler struct {
    mu       sync.Mutex
    calls    []string        // records group names in call order
    failOn   map[string]error // group name → error to return
    barrier  *sync.WaitGroup // optional: wait for all groups to start
}

func (m *mockHandler) ExecuteGroup(ctx context.Context, group engine.ExecutionGroup) error {
    m.mu.Lock()
    m.calls = append(m.calls, group.Chunk.Name)
    m.mu.Unlock()
    if m.barrier != nil {
        m.barrier.Done()
        m.barrier.Wait() // ensures all groups started concurrently
    }
    if err, ok := m.failOn[group.Chunk.Name]; ok {
        return err
    }
    return nil
}
```

**Integration tests** (`test/integration/controller/`): Use envtest with real DRPlan/DRExecution CRDs, verify end-to-end flow: create plan → create VMs → create execution → controller dispatches executor → status populated → plan phase advanced.

### Previous Story Intelligence

**From Story 4.1 (DR State Machine & Execution Controller):**
- State machine is pure functions: `Transition(currentPhase, mode) → (newPhase, error)`, `CompleteTransition(currentPhase) → (newPhase, error)`
- DRExecution controller validates mode, checks plan phase, transitions plan, sets startTime
- Controller returns `ctrl.Result{}, nil` after setup — THIS STORY fills in the engine dispatch
- Idempotency: `exec.Status.StartTime != nil` means already processed — do not re-dispatch
- Uses `k8s.io/client-go/tools/events` (new events API), not `k8s.io/client-go/tools/record`
- All resources are cluster-scoped — use `client.ObjectKey{Name: name}` without namespace

**From Story 4.05 (Driver Registry Fallback & Preflight Convergence):**
- Driver resolution unified through registry + `StorageClassLister`
- `--noop-fallback` flag enables dev/CI without real storage
- `KubeStorageClassLister` in `internal/preflight/` provides real Kubernetes implementation
- `TypedStorageBackendResolver` uses `Registry` + `SCLister` — same pattern for executor

**From Epic 3 (Storage Driver Framework):**
- Fake driver (`pkg/drivers/fake/`) has programmable `On*/Return/ReturnResult` API — use for executor tests
- All driver methods are idempotent — executor can safely retry (but that's Story 4.7)
- `drivers.DefaultRegistry` is the process-wide singleton

### DRPlan Reconciler as Reference

The DRPlan reconciler in `pkg/controller/drplan/reconciler.go` demonstrates the full discovery → consistency → chunking pipeline. The executor reuses the SAME functions:

```go
// From DRPlan reconciler (lines 125-210) — executor follows the same pattern:
result, err := r.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
waves := engine.GroupByWave(result.VMs, plan.Spec.WaveLabel)
consistency, err := engine.ResolveVolumeGroups(ctx, vms, plan.Spec.WaveLabel, r.NamespaceLookup)
chunkResult := engine.ChunkWaves(chunkInput, plan.Spec.MaxConcurrentFailovers)
```

The DRPlan reconciler uses `TypedVMDiscoverer` (with controller-runtime cached client). The executor should use the same discoverer instance — share it between controllers via main.go wiring.

### Chunk Naming Convention

Chunk names follow the pattern `wave-<waveKey>-group-<n>` where waveKey is the wave label value (e.g., `"alpha"`, `"beta"`, `"1"`) and n is 0-indexed. These names map directly to `DRGroupExecutionStatus.Name` in the DRExecution status. Example: `wave-alpha-group-0`, `wave-alpha-group-1`, `wave-beta-group-0`.

### DRExecution is Cluster-Scoped

Both DRPlan and DRExecution are cluster-scoped resources (no namespace). Use `client.ObjectKey{Name: name}` for lookups. In tests, create objects without namespace.

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
- `pkg/engine/executor.go` — wave executor (architecture: `pkg/engine/executor.go`)
- `pkg/engine/handler_noop.go` — pluggable handler pattern
- `pkg/controller/drexecution/reconciler.go` — enhanced controller
- `cmd/soteria/main.go` — wiring

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.2] — Story acceptance criteria (BDD format)
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/engine/executor.go` in directory structure
- [Source: _bmad-output/planning-artifacts/architecture.md#Controller Patterns] — Reconcile return patterns, structured logging
- [Source: _bmad-output/planning-artifacts/architecture.md#API & Communication Patterns] — Checkpointing, error model, fail-forward
- [Source: _bmad-output/planning-artifacts/architecture.md#Engine Boundary] — Engine owns workflow execution, receives plan and driver
- [Source: _bmad-output/project-context.md#Critical Implementation Rules] — No in-memory state across reconciles, fail-forward model
- [Source: _bmad-output/project-context.md#Testing Rules] — envtest over fake client, test naming conventions
- [Source: pkg/engine/chunker.go] — Existing ChunkWaves, DRGroupChunk, ChunkResult types
- [Source: pkg/engine/discovery.go] — VMDiscoverer interface, TypedVMDiscoverer, GroupByWave
- [Source: pkg/engine/consistency.go] — ResolveVolumeGroups, NamespaceLookup interface
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — DRExecution, DRExecutionStatus, WaveStatus, DRGroupExecutionStatus, ExecutionResult, DRGroupResult
- [Source: pkg/controller/drexecution/reconciler.go] — Current controller (skeleton from retro, enhanced by Story 4.1)
- [Source: pkg/controller/drplan/reconciler.go#L125-210] — Reference pipeline: discover → group → resolve → chunk
- [Source: pkg/drivers/interface.go] — StorageProvider 7-method contract
- [Source: pkg/drivers/registry.go] — Registry.GetDriver, GetDriverForPVC, StorageClassLister
- [Source: pkg/drivers/fake/driver.go] — Programmable fake for unit tests
- [Source: cmd/soteria/main.go] — Controller wiring, event broadcaster setup, DRExecution reconciler registration
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — Previous story: state machine, controller setup, admission webhook
- [Source: _bmad-output/implementation-artifacts/4-05-driver-registry-fallback-preflight-convergence.md] — Previous story: registry fallback, preflight convergence

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
