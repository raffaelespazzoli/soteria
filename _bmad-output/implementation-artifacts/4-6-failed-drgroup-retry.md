# Story 4.6: Failed DRGroup Retry

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to manually retry a failed DRGroup when the VM is in a healthy state, and have the orchestrator reject retries when the state is unpredictable,
So that I can recover from transient failures safely.

## Acceptance Criteria

1. **AC1 — Retry trigger via annotation:** The operator retries failed DRGroups by adding the annotation `soteria.io/retry-groups` to the DRExecution. The annotation value is a comma-separated list of group names (e.g., `"wave-alpha-group-1,wave-beta-group-0"`) or the special value `"all-failed"` to retry every group with `Result == Failed`.

2. **AC2 — PartiallySucceeded precondition:** Retry is only accepted when `DRExecution.Status.Result == PartiallySucceeded`. The controller rejects retry (removes annotation, sets a `RetryRejected` condition) when the result is `Succeeded` (nothing to retry) or `Failed` (all groups failed — plan phase was not advanced, operator should create a new DRExecution).

3. **AC3 — VM health validation:** Before re-executing a group, the controller validates that every VM in the group exists and is in a known, healthy state on the original site. If any VM is in a non-standard state, the retry is rejected with a clear error: `"VM <name> is in an unpredictable state — manual intervention required"` (FR15). The DRGroup remains `Failed` and the annotation is removed.

4. **AC4 — Group re-execution:** When preconditions are met, each retry group is re-executed using the same handler (planned migration or disaster) that the original execution used. The DRGroupExecutionStatus transitions from `Failed` to `InProgress` to `Completed` (or back to `Failed` if the retry itself fails). The corresponding DRGroupStatus resource Phase also transitions to `InProgress` and its Steps are cleared for the retry attempt.

5. **AC5 — Result upgrade on full success:** When all retried groups complete successfully and all DRGroups across the execution are now `Completed`, the DRExecution result is updated from `PartiallySucceeded` to `Succeeded` (FR14).

6. **AC6 — Result stays PartiallySucceeded on partial retry:** When a retry succeeds for some groups but others remain `Failed` (either not retried or retry failed), the result stays `PartiallySucceeded`.

7. **AC7 — Retry failure returns group to Failed:** When a retry itself fails, the DRGroup returns to `Failed` with the new error message. The operator can attempt another retry if preconditions still hold.

8. **AC8 — No retry — no action:** When the operator decides not to retry, the DRGroup remains `Failed` and the DRExecution result stays `PartiallySucceeded`. No further action is taken automatically.

9. **AC9 — Wave ordering during retry:** Retry groups are re-executed respecting original wave ordering — groups from wave N are retried before groups from wave N+1. Groups within the same wave are retried concurrently (same semantics as initial execution).

10. **AC10 — Strategy relaxation for PartiallySucceeded:** The `drexecutionStatusStrategy.ValidateUpdate` is relaxed to allow status updates when the existing `Result` is `PartiallySucceeded`. `Succeeded` and `Failed` remain fully immutable.

11. **AC11 — Retry count tracking:** Each `DRGroupExecutionStatus` records a `RetryCount` field (default 0) that increments on each retry attempt. This provides an audit trail of how many times a group was retried.

12. **AC12 — Retry events:** The controller emits events on the DRExecution for: retry started (`RetryStarted`), group retry succeeded (`GroupRetrySucceeded`), group retry failed (`GroupRetryFailed`), retry rejected (`RetryRejected`), and overall retry result (`RetryCompleted`).

13. **AC13 — Annotation cleanup:** After retry completes (all specified groups retried regardless of individual outcomes), the controller removes the `soteria.io/retry-groups` annotation from the DRExecution. This allows the operator to trigger subsequent retries by re-adding the annotation.

14. **AC14 — Concurrent retry prevention:** If a retry is already in progress (any group has `Result == InProgress`), the controller ignores new retry annotations until the current retry completes.

15. **AC15 — Unit tests:** Comprehensive table-driven tests covering: (a) retry one failed group — succeeds, result updated to Succeeded; (b) retry one of two failed groups — succeeds, result stays PartiallySucceeded; (c) retry all-failed — all succeed, result Succeeded; (d) retry fails — group back to Failed with new error, result stays PartiallySucceeded; (e) retry on Succeeded execution — rejected; (f) retry on Failed execution — rejected; (g) VM health validation fails — rejected with descriptive error; (h) retry group not found in execution — rejected; (i) retry group already Completed — skipped; (j) wave ordering maintained during retry; (k) concurrent retry prevented; (l) annotation cleaned up after retry; (m) retry count incremented; (n) DRGroupStatus phase transitions during retry; (o) events emitted per retry outcome.

## Tasks / Subtasks

- [ ] Task 1: Add RetryCount to DRGroupExecutionStatus (AC: #11)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `RetryCount int` field to `DRGroupExecutionStatus` with JSON tag `retryCount,omitempty`
  - [ ] 1.2 Run `make generate` to regenerate deepcopy methods
  - [ ] 1.3 Run `make manifests` to update CRD schema

- [ ] Task 2: Define retry annotation constant and VM health validator (AC: #1, #3)
  - [ ] 2.1 In `pkg/engine/executor.go`, add constant `RetryGroupsAnnotation = "soteria.io/retry-groups"` and `RetryAllFailed = "all-failed"`
  - [ ] 2.2 Define `VMHealthValidator` interface in `pkg/engine/executor.go`: `ValidateVMHealth(ctx context.Context, vmName, namespace string) error` — returns nil if VM is healthy, descriptive error if not
  - [ ] 2.3 Implement `KubeVirtVMHealthValidator` in `pkg/engine/vm_health.go`: fetches `kubevirtv1.VirtualMachine`, checks that the VM exists and is in a known state (not in error/unknown conditions)
  - [ ] 2.4 Implement `NoOpVMHealthValidator` for testing: always returns nil (all VMs healthy)
  - [ ] 2.5 Add `VMHealthValidator` field to `WaveExecutor`

- [ ] Task 3: Implement retry group resolution (AC: #1, #2, #9)
  - [ ] 3.1 In `pkg/engine/executor.go`, implement `resolveRetryGroups(exec *DRExecution, annotation string) ([]retryTarget, error)` that parses the annotation value, validates each group exists in the execution status and has `Result == Failed`, returns structured retry targets sorted by wave index
  - [ ] 3.2 Define `retryTarget` struct: `WaveIndex int`, `GroupIndex int`, `GroupStatus *DRGroupExecutionStatus`, `Chunk DRGroupChunk` (the chunk is reconstructed from the group's VMNames and volume group info)
  - [ ] 3.3 Handle `"all-failed"` by scanning all waves/groups for `Result == Failed`
  - [ ] 3.4 Return error if any named group is not found or is not in `Failed` state
  - [ ] 3.5 Skip groups that are already `Completed` (not an error — silently ignore)

- [ ] Task 4: Implement retry execution (AC: #4, #5, #6, #7, #9, #11)
  - [ ] 4.1 In `pkg/engine/executor.go`, implement `ExecuteRetry(ctx context.Context, input RetryInput) error` as the retry entry point
  - [ ] 4.2 Define `RetryInput` struct: `Execution *DRExecution`, `Plan *DRPlan`, `Handler DRGroupHandler`, `RetryTargets []retryTarget`
  - [ ] 4.3 Group retry targets by wave index, execute waves sequentially (respecting original ordering)
  - [ ] 4.4 Within each wave, execute retry groups concurrently using `sync.WaitGroup` (same fail-forward pattern as initial execution)
  - [ ] 4.5 For each retry group: set `DRGroupExecutionStatus.Result = InProgress`, increment `RetryCount`, call `handler.ExecuteGroup(ctx, group)`
  - [ ] 4.6 On success: set `Result = Completed`, update `CompletionTime`. On failure: set `Result = Failed` with new error, update `CompletionTime`
  - [ ] 4.7 Update DRGroupStatus resource: set `Phase = InProgress` at start, clear old `Steps`, record new steps during retry, set final `Phase`
  - [ ] 4.8 After all retry groups complete, recompute overall result: all Completed → `Succeeded`, any Failed remaining → `PartiallySucceeded`
  - [ ] 4.9 Status writes use the same mutex-protected `updateGroupStatus` pattern from initial execution

- [ ] Task 5: Implement VM health validation (AC: #3)
  - [ ] 5.1 In `pkg/engine/vm_health.go`, implement `KubeVirtVMHealthValidator.ValidateVMHealth`: fetch VM via `client.Get`, check VM exists
  - [ ] 5.2 Check VM status conditions for known error states (e.g., VM has been started on the target site, VM is in migration, VM is in an unrecoverable error)
  - [ ] 5.3 Return `fmt.Errorf("VM %s/%s is in an unpredictable state — manual intervention required: %s", namespace, name, reason)` on health check failure
  - [ ] 5.4 For each retry group, validate ALL VMs before executing any — if any VM fails validation, reject the entire retry with the failing VM's error

- [ ] Task 6: Integrate retry into DRExecution controller (AC: #1, #2, #12, #13, #14)
  - [ ] 6.1 In `pkg/controller/drexecution/reconciler.go`, add retry detection logic: after the existing reconcile flow (which handles new executions), check if the DRExecution has the `soteria.io/retry-groups` annotation
  - [ ] 6.2 Guard: if `Result != PartiallySucceeded`, remove annotation, set `RetryRejected` condition with descriptive message, emit `RetryRejected` event, return
  - [ ] 6.3 Guard: if any group has `Result == InProgress`, retry is already running — skip (do not remove annotation)
  - [ ] 6.4 Call `resolveRetryGroups` to parse and validate the annotation
  - [ ] 6.5 For each retry group, call `VMHealthValidator.ValidateVMHealth` for every VM. If any fails, remove annotation, set condition, emit event, return
  - [ ] 6.6 Emit `RetryStarted` event with list of group names
  - [ ] 6.7 Re-discover VMs and resolve drivers for retry groups (same discovery pipeline as initial execution)
  - [ ] 6.8 Reconstruct `DRGroupChunk` from DRGroupExecutionStatus (VMNames, volume group info from DRPlan status)
  - [ ] 6.9 Call `WaveExecutor.ExecuteRetry(ctx, retryInput)` synchronously
  - [ ] 6.10 After retry: remove `soteria.io/retry-groups` annotation from DRExecution metadata (update main resource)
  - [ ] 6.11 Emit completion events: `GroupRetrySucceeded` / `GroupRetryFailed` per group, `RetryCompleted` for overall outcome
  - [ ] 6.12 The controller `SetupWithManager` should also watch for annotation changes — this is handled by the default reconcile trigger (any DRExecution update triggers reconcile)

- [ ] Task 7: Relax DRExecution status strategy for PartiallySucceeded (AC: #10)
  - [ ] 7.1 In `pkg/registry/drexecution/strategy.go`, modify `drexecutionStatusStrategy.ValidateUpdate` to allow status updates when the old `Result` is `PartiallySucceeded`
  - [ ] 7.2 Keep `Succeeded` and `Failed` as immutable (no status changes allowed)
  - [ ] 7.3 Add comments explaining the retry exception: PartiallySucceeded is re-openable for group retry per FR14
  - [ ] 7.4 Update strategy tests to verify: PartiallySucceeded → status update allowed, Succeeded → blocked, Failed → blocked

- [ ] Task 8: Wire new components in main.go (AC: #3)
  - [ ] 8.1 In `cmd/soteria/main.go`, create `KubeVirtVMHealthValidator` (or `NoOpVMHealthValidator` when `--noop-fallback`) and pass to `WaveExecutor`
  - [ ] 8.2 Ensure RBAC marker for kubevirt.io VM reads exists on DRExecution reconciler (should already exist from Story 4.5)

- [ ] Task 9: Unit tests for retry group resolution (AC: #15)
  - [ ] 9.1 Create or extend `pkg/engine/executor_test.go` with retry-specific tests
  - [ ] 9.2 Test: `TestResolveRetryGroups_SpecificGroups` — annotation `"wave-alpha-group-1"` → returns 1 retry target matching the failed group
  - [ ] 9.3 Test: `TestResolveRetryGroups_AllFailed` — annotation `"all-failed"` with 2 failed groups → returns both
  - [ ] 9.4 Test: `TestResolveRetryGroups_GroupNotFound` — annotation references non-existent group → error
  - [ ] 9.5 Test: `TestResolveRetryGroups_GroupNotFailed` — annotation references a Completed group → silently skipped, no error
  - [ ] 9.6 Test: `TestResolveRetryGroups_WaveOrdering` — groups from waves 0 and 2 → targets sorted by wave index

- [ ] Task 10: Unit tests for retry execution (AC: #15)
  - [ ] 10.1 Test: `TestWaveExecutor_RetryOneGroup_Succeeds_ResultSucceeded` — 2 groups total (1 Completed, 1 Failed), retry the failed → all Completed → `Succeeded`
  - [ ] 10.2 Test: `TestWaveExecutor_RetryOneOfTwo_Succeeds_ResultPartiallySucceeded` — 3 groups (1 Completed, 2 Failed), retry 1 of 2 failed → result stays `PartiallySucceeded`
  - [ ] 10.3 Test: `TestWaveExecutor_RetryAllFailed_AllSucceed_ResultSucceeded` — all failed groups retried, all succeed → `Succeeded`
  - [ ] 10.4 Test: `TestWaveExecutor_RetryFails_GroupBackToFailed` — retry fails → group back to `Failed` with new error, result stays `PartiallySucceeded`
  - [ ] 10.5 Test: `TestWaveExecutor_RetryWaveOrdering` — retry groups from waves 0 and 2, verify wave 0 completes before wave 2 starts
  - [ ] 10.6 Test: `TestWaveExecutor_RetryCount_Incremented` — RetryCount goes from 0 → 1 on first retry, 1 → 2 on second
  - [ ] 10.7 Test: `TestWaveExecutor_RetryDRGroupStatus_Updated` — DRGroupStatus Phase transitions: Failed → InProgress → Completed, Steps cleared and repopulated
  - [ ] 10.8 Test: `TestWaveExecutor_RetryContextCancelled` — cancel during retry, in-progress groups cancelled, result recomputed

- [ ] Task 11: Unit tests for retry precondition validation (AC: #15)
  - [ ] 11.1 Test: `TestDRExecutionRetry_SucceededExecution_Rejected` — annotation on Succeeded → rejected, annotation removed
  - [ ] 11.2 Test: `TestDRExecutionRetry_FailedExecution_Rejected` — annotation on Failed → rejected, annotation removed
  - [ ] 11.3 Test: `TestDRExecutionRetry_VMHealthFails_Rejected` — VM in non-standard state → rejected with "unpredictable state" error
  - [ ] 11.4 Test: `TestDRExecutionRetry_ConcurrentRetryPrevented` — group already InProgress → retry skipped
  - [ ] 11.5 Test: `TestDRExecutionRetry_AnnotationCleanup` — after retry completes, annotation is removed

- [ ] Task 12: Unit tests for VM health validator (AC: #15)
  - [ ] 12.1 Create `pkg/engine/vm_health_test.go`
  - [ ] 12.2 Test: `TestKubeVirtVMHealthValidator_HealthyVM` — VM exists, no error conditions → nil
  - [ ] 12.3 Test: `TestKubeVirtVMHealthValidator_VMNotFound` — VM does not exist → error
  - [ ] 12.4 Test: `TestKubeVirtVMHealthValidator_VMInErrorState` — VM has error condition → error with descriptive message
  - [ ] 12.5 Test: `TestNoOpVMHealthValidator_AlwaysHealthy` — returns nil regardless

- [ ] Task 13: Unit tests for strategy relaxation (AC: #15)
  - [ ] 13.1 In `pkg/registry/drexecution/strategy_test.go`, add test: `TestStatusStrategy_PartiallySucceeded_AllowsUpdate` — status update from PartiallySucceeded allowed
  - [ ] 13.2 Test: `TestStatusStrategy_Succeeded_BlocksUpdate` — status update from Succeeded → forbidden (unchanged)
  - [ ] 13.3 Test: `TestStatusStrategy_Failed_BlocksUpdate` — status update from Failed → forbidden (unchanged)
  - [ ] 13.4 Test: `TestStatusStrategy_EmptyResult_AllowsUpdate` — status update from empty result allowed (initial execution in progress)

- [ ] Task 14: Update documentation and verify (AC: all)
  - [ ] 14.1 Update `pkg/engine/doc.go` to cover: retry mechanism, annotation trigger, VM health validation, retry execution flow
  - [ ] 14.2 Add godoc comment on `VMHealthValidator` explaining its role in retry precondition validation
  - [ ] 14.3 Add godoc comment on `ExecuteRetry` explaining the retry flow
  - [ ] 14.4 Run `make manifests` to regenerate RBAC/CRD schemas (RetryCount field)
  - [ ] 14.5 Run `make generate` for deepcopy regeneration
  - [ ] 14.6 Run `make test` — all unit tests pass
  - [ ] 14.7 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 14.8 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4.6 of Epic 4 (DR Workflow Engine — Full Lifecycle). It implements the manual retry mechanism for failed DRGroups within a PartiallySucceeded DRExecution (FR14, FR15). All preceding Epic 4 stories (4.05, 4.1, 4.2, 4.3, 4.4, 4.5) are prerequisites.

**Story 4.6 scope:** Annotation-triggered retry of failed DRGroups, VM health validation as a precondition gate, strategy relaxation to allow PartiallySucceeded re-opening, retry execution respecting wave ordering, result recomputation after retry. The retry reuses the same `DRGroupHandler` (planned migration or disaster) and the same executor infrastructure — it does NOT create a new DRExecution.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite — done |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — provides Transition, CompleteTransition, controller |
| 4.1b | 8-phase state machine + unified FailoverHandler | Prerequisite — provides FailoverHandler, FailedBack, ReprotectingBack phases |
| 4.2 | Wave executor framework + controller dispatch | Prerequisite — provides WaveExecutor, DRGroupHandler, ExecutionGroup, fail-forward |
| 4.3 | Planned migration workflow + VMManager | Prerequisite — provides FailoverHandler |
| 4.4 | Disaster failover workflow | Prerequisite — provides FailoverHandler |
| 4.5 | Fail-forward error handling & partial success | Prerequisite — provides GroupError, StepRecorder, DRGroupStatus lifecycle, PVCResolver |
| **4.6** | **Failed DRGroup retry** | **This story — annotation-triggered retry, VM health, strategy relaxation** |
| 4.7 | Checkpoint, resume & HA | Builds on DRGroupStatus for resume state |
| 4.8 | Re-protect & failback workflows | Reprotecting + FailingBack paths |

### Critical Design Decision: Why Annotation-Triggered Retry

**Constraint:** `DRExecution.Spec` is immutable after creation (strategy enforces this). We cannot add retry parameters to spec.

**Options considered:**

| Option | Verdict | Reason |
|--------|---------|--------|
| Annotation on DRExecution | **Selected** | No new CRDs, no spec mutation needed, simple UX (kubectl annotate), reconciler detects annotation change |
| New DRRetry CRD | Rejected | Over-engineered for v1alpha1; adds CRD, strategy, registry, admission webhook overhead |
| Sub-resource `/retry` | Rejected | Requires custom REST endpoint in the aggregated API server — high complexity for this story |
| New DRExecution per retry | Rejected | Epics explicitly say "DRExecution result is updated from PartiallySucceeded to Succeeded" |

**Annotation pattern:** The operator annotates the DRExecution and the controller reconciles:

```bash
# Retry specific groups
kubectl annotate drexecution failover-2026-04-18 soteria.io/retry-groups=wave-alpha-group-1

# Retry all failed groups
kubectl annotate drexecution failover-2026-04-18 soteria.io/retry-groups=all-failed
```

### Critical Design Decision: Strategy Relaxation

The current `drexecutionStatusStrategy.ValidateUpdate` blocks ALL status updates when `Result` is terminal (`Succeeded`, `PartiallySucceeded`, `Failed`). For retry to work, `PartiallySucceeded` must be re-openable.

**Change:** Allow status updates when `oldExec.Status.Result == PartiallySucceeded`. Keep `Succeeded` and `Failed` fully immutable.

**Rationale:** `PartiallySucceeded` means "some groups succeeded, some failed" — the execution is semantically incomplete. Allowing retry to complete those failed groups is a natural extension. `Succeeded` is truly final. `Failed` means the plan phase was not advanced (Story 4.5 AC12) — the operator should create a new DRExecution.

**Audit safety:** Individual group retries are tracked via `RetryCount` and DRGroupStatus resources. The original failure details are preserved in events. The strategy relaxation does not break audit — it enhances it by allowing recovery.

### Critical Design Decision: Why Retry is Limited to PartiallySucceeded

| DRExecution Result | Retry Allowed? | Reason |
|-------------------|---------------|--------|
| `PartiallySucceeded` | Yes | Some groups succeeded, some failed. Plan phase was advanced (FailingOver → FailedOver). Failed groups can be retried to achieve full success. |
| `Failed` | No | All groups failed OR pre-condition failure. Plan phase was NOT advanced — stays in FailingOver. Operator should fix the issue and create a new DRExecution. |
| `Succeeded` | No | Nothing to retry — all groups completed. |

### Existing Code to Reuse (Critical — Do NOT Reinvent)

| File | What It Provides | How This Story Uses It |
|------|-----------------|----------------------|
| `pkg/engine/executor.go` (Story 4.2+4.5) | `WaveExecutor`, `DRGroupHandler`, `ExecutionGroup`, `executeWave`, `executeGroup`, `updateGroupStatus`, mutex pattern, `StepRecorder`, `GroupError` | Reuse the same per-group execution and status update infrastructure. Add `ExecuteRetry` method to `WaveExecutor`. |
| `pkg/engine/failover.go` (Stories 4.3–4.4) | `FailoverHandler` | Same handler for planned migration and disaster retries — no changes |
| `pkg/engine/vm.go` (Story 4.3) | `VMManager`, `KubeVirtVMManager` | VM health validator accesses same KubeVirt VMs — share scheme and client |
| `pkg/engine/statemachine.go` (Story 4.1) | `CompleteTransition` | NOT called during retry — plan phase was already advanced during initial execution |
| `pkg/engine/pvc_resolver.go` (Story 4.5) | `PVCResolver`, `KubeVirtPVCResolver` | Reuse for driver resolution during retry |
| `pkg/drivers/interface.go` | `StorageProvider` 7-method contract | Driver calls unchanged — retry calls same methods |
| `pkg/drivers/registry.go` | `Registry.GetDriver`, `StorageClassLister` | Driver resolution for retry groups |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing retry with configurable step failures |
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRGroupStatus`, `DRGroupStatusState`, `StepStatus`, `DRGroupResult`, `ExecutionResult`, `DRGroupExecutionStatus` | All types reused — add `RetryCount` field only |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRGroupExecutionStatus` has Name, Result, VMNames, Error, StartTime, CompletionTime | Add `RetryCount int` field |
| `pkg/registry/drexecution/strategy.go` | `StatusStrategy.ValidateUpdate` blocks all terminal results | Allow updates when `Result == PartiallySucceeded` |
| `pkg/engine/executor.go` (Story 4.5) | `WaveExecutor` with `Execute`, no retry | Add `ExecuteRetry` method, `resolveRetryGroups` helper, `VMHealthValidator` field, retry annotation constant |
| `pkg/controller/drexecution/reconciler.go` (Stories 4.1-4.5) | Handles new executions: validate → transition → dispatch engine | Add retry detection after existing flow: detect annotation → validate → re-execute → cleanup |
| `cmd/soteria/main.go` (Stories 4.1-4.5) | Wires executor, handlers, VMManager, PVCResolver | Add VMHealthValidator wiring |

### New Files to Create

| File | Purpose |
|------|---------|
| `pkg/engine/vm_health.go` | `VMHealthValidator` interface + `KubeVirtVMHealthValidator` + `NoOpVMHealthValidator` |
| `pkg/engine/vm_health_test.go` | VM health validator unit tests |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/engine/statemachine.go` | State machine — NOT called during retry (plan phase already advanced) |
| `pkg/engine/failover.go` | Handler — use for retry, don't modify |
| `pkg/engine/chunker.go` | Chunker — no changes |
| `pkg/engine/discovery.go` | VM discovery — reuse for driver resolution, don't modify |
| `pkg/engine/consistency.go` | Consistency resolution — reuse, don't modify |
| `pkg/engine/vm.go` | VMManager — reuse, don't modify |
| `pkg/engine/pvc_resolver.go` | PVCResolver — reuse for retry driver resolution, don't modify |
| `pkg/engine/handler_noop.go` | NoOpHandler — no changes needed |
| `pkg/drivers/interface.go` | Stable StorageProvider interface |
| `pkg/drivers/types.go` | Domain types — no changes |
| `pkg/drivers/registry.go` | Driver registry — use, don't modify |
| `pkg/drivers/fake/driver.go` | Fake driver — use for tests, don't modify |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes |
| `pkg/admission/*` | Admission webhooks — no changes (retry validation is in the controller, not a webhook) |

### Key Implementation Decisions

**1. Retry reuses the same `DRGroupHandler` — the handler doesn't know it's a retry.**

The retry calls `handler.ExecuteGroup(ctx, group)` with the same `ExecutionGroup` struct. The handler (`pkg/engine/failover.go`) is idempotent by design (all driver methods are idempotent). The handler doesn't need retry awareness — it executes the same steps as the initial attempt.

**2. Retry does NOT call `CompleteTransition` — the plan phase was already advanced.**

During initial execution, `CompleteTransition` was called for `PartiallySucceeded` (Story 4.5 AC12 — partial recovery still advances the plan). The plan is already in `FailedOver` (or `SteadyState` after failback). Retry does not change the plan phase — it only upgrades the execution result.

**3. Retry groups are reconstructed from DRExecution status + DRPlan status.**

The executor doesn't re-run full discovery. Instead, it reconstructs `DRGroupChunk` from:
- `DRGroupExecutionStatus.VMNames` — the VMs in the group
- `DRPlan.Status.Waves[].Groups[]` — the volume groups for those VMs
- Driver resolution per VolumeGroup (same as Story 4.5)

```go
func reconstructChunk(groupStatus DRGroupExecutionStatus, waveInfo WaveInfo) DRGroupChunk {
    // Map VMNames to VMReferences from the plan's wave info
    // Find matching VolumeGroupInfos that contain these VMs
    // Build DRGroupChunk
}
```

This avoids the cost of full re-discovery and ensures the retry uses exactly the same VMs as the initial attempt.

**4. VM health validation checks VM existence and basic status.**

The `KubeVirtVMHealthValidator` performs lightweight checks:
- VM resource exists (not deleted)
- VM is not actively running on the target site (which would mean a split-brain scenario)
- VM does not have terminal error conditions

This is NOT a deep storage health check — storage health was already checked during the initial execution. The validator prevents retry when the VM has been manually tampered with or is in an inconsistent state.

```go
type VMHealthValidator interface {
    ValidateVMHealth(ctx context.Context, vmName, namespace string) error
}
```

**5. Retry annotation is cleaned up after retry completes.**

The controller removes the annotation after all specified groups are retried (regardless of individual outcomes). This allows the operator to re-annotate for another retry attempt. The annotation lifecycle:

```
1. No annotation → normal reconcile (initial execution or no-op)
2. Annotation added → controller detects, validates, executes retry
3. Annotation removed by controller → reconcile triggered, no-op (no annotation)
4. Operator can re-annotate for another retry attempt
```

**6. Concurrent retry prevention via InProgress check.**

Before starting retry, the controller checks if any group has `Result == InProgress`. If so, a retry is already running (either from the initial execution or a previous retry annotation). The controller skips the new retry and leaves the annotation for the next reconcile cycle.

**7. Retry execution flow.**

```
1. Controller detects `soteria.io/retry-groups` annotation
   ↓
2. Validate: Result == PartiallySucceeded?
   ↓ Yes                        ↓ No
   Continue                     Reject, remove annotation, emit RetryRejected event
   ↓
3. Resolve retry targets: parse annotation, find matching Failed groups
   ↓
4. Validate VM health for all VMs in retry groups
   ↓ Healthy                    ↓ Unhealthy
   Continue                     Reject, remove annotation, emit RetryRejected event
   ↓
5. Emit RetryStarted event
   ↓
6. For each retry group (wave-ordered):
   a. Set group InProgress, increment RetryCount
   b. Update DRGroupStatus (Phase=InProgress, clear Steps)
   c. Resolve driver via registry
   d. Call handler.ExecuteGroup(ctx, group)
   e. Set group Completed/Failed, update DRGroupStatus
   f. Emit GroupRetrySucceeded/GroupRetryFailed event
   ↓
7. Recompute overall result:
   All Completed → Succeeded
   Any Failed remaining → PartiallySucceeded
   ↓
8. Update DRExecution status
   ↓
9. Remove annotation, emit RetryCompleted event
```

**8. DRGroupStatus lifecycle during retry.**

```
Before retry:  Phase=Failed, Steps=[{SetSource, Failed, "error msg"}]
   ↓
Retry starts:  Phase=InProgress, Steps=[] (cleared)
   ↓
Step recorded: Phase=InProgress, Steps=[{SetSource, Succeeded, "ok"}]
   ↓
Retry done:    Phase=Completed (or Failed with new error)
```

Old steps from the failed attempt are cleared when retry starts. The DRGroupStatus represents the CURRENT state, not the full history — history is in events.

### RBAC Requirements

No new RBAC markers needed beyond what Stories 4.1-4.5 established. The DRExecution controller already has:

```go
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drgroupstatuses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=soteria.io,resources=drgroupstatuses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch;patch;update
```

The `update;patch` on `drexecutions` (main resource) covers annotation removal. VM reads are already present for VMManager.

### Code Patterns to Follow

**Retry annotation parsing:**
```go
func parseRetryAnnotation(value string) []string {
    if value == RetryAllFailed {
        return nil // sentinel — caller resolves all failed groups
    }
    groups := strings.Split(value, ",")
    result := make([]string, 0, len(groups))
    for _, g := range groups {
        g = strings.TrimSpace(g)
        if g != "" {
            result = append(result, g)
        }
    }
    return result
}
```

**Structured logging (controller-runtime convention):**
```go
logger := log.FromContext(ctx)
logger.Info("Retry requested", "execution", exec.Name, "groups", retryGroupNames)
logger.Info("Retry group completed", "group", groupName, "result", result, "retryCount", retryCount)
logger.Info("Retry rejected", "execution", exec.Name, "reason", "VM in unpredictable state", "vm", vmName)
logger.Info("Retry completed", "execution", exec.Name, "result", newResult, "retriedGroups", len(targets))
```

**Event recording pattern:**
```go
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "RetryStarted", "RetryAction",
    "Retry started for execution %s: groups %s", exec.Name, strings.Join(groupNames, ", "))

r.Recorder.Eventf(&exec, nil, corev1.EventTypeWarning, "RetryRejected", "RetryAction",
    "Retry rejected for execution %s: %s", exec.Name, reason)

r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "GroupRetrySucceeded", "RetryAction",
    "DRGroup %s retry succeeded (attempt %d)", groupName, retryCount)

r.Recorder.Eventf(&exec, nil, corev1.EventTypeWarning, "GroupRetryFailed", "RetryAction",
    "DRGroup %s retry failed (attempt %d): %v", groupName, retryCount, err)
```

**Error wrapping (project convention):**
```go
return fmt.Errorf("validating VM health for retry group %s: %w", groupName, err)
return fmt.Errorf("resolving driver for retry group %s volume group %s: %w", groupName, vgName, err)
return fmt.Errorf("executing retry for group %s: %w", groupName, err)
```

### Test Strategy

**Unit tests** (`pkg/engine/executor_test.go` — retry section): Use the same mock `DRGroupHandler`, fake K8s client (for DRGroupStatus), mock `VMHealthValidator`, and verify:
- Retry target resolution — annotation parsing, group lookup, wave ordering
- VM health validation — healthy VMs pass, unhealthy VMs reject retry
- Retry execution — group transitions, result recomputation, RetryCount increment
- Wave ordering during retry — groups from lower waves execute first
- Concurrent retry prevention — InProgress group blocks new retry
- Annotation cleanup — removed after retry completes

**VM health validator tests** (`pkg/engine/vm_health_test.go`): Use controller-runtime fake client with kubevirt scheme, create VMs with various status conditions.

**Strategy tests** (`pkg/registry/drexecution/strategy_test.go`): Verify the relaxation for PartiallySucceeded while Succeeded and Failed remain immutable.

### Previous Story Intelligence

**From Story 4.5 (Fail-Forward Error Handling & Partial Success):**
- `GroupError` provides structured error propagation — retry uses the same error type
- `StepRecorder` enables real-time DRGroupStatus updates during retry
- DRGroupStatus lifecycle established: Created → InProgress → Completed/Failed. Retry extends this: Failed → InProgress → Completed/Failed
- Per-VolumeGroup driver resolution — retry uses the same resolution pattern
- `PVCResolver` — reuse for driver resolution during retry
- Result computation rules: all Completed → Succeeded, any Failed + any Completed → PartiallySucceeded
- `CompleteTransition` called for Succeeded AND PartiallySucceeded — plan phase already advanced before retry

**From Story 4.4 (Disaster Failover Workflow):**
- `FailoverHandler` — reused during disaster retry without modification
- RPO recording — retry does not re-record RPO (initial recording is sufficient)
- Step name constants shared — retry records same step names

**From Story 4.3 (Planned Migration Workflow):**
- `FailoverHandler` — reused during planned migration retry without modification
- `resolveVolumeGroupID` — retry reconstructs chunks from execution status, bypassing this
- `VMManager` — not directly used in retry (VM health validator replaces the need)

**From Story 4.2 (DRGroup Chunking & Wave Executor):**
- `sync.WaitGroup` for within-wave concurrency — retry uses same pattern
- `updateGroupStatus` with mutex — retry uses same serialization pattern
- `ExecutionGroup` struct — retry populates the same struct for handler calls
- No-op handler — useful for retry unit tests

**From Story 4.1 (DR State Machine & Execution Controller):**
- Controller idempotency via `startTime != nil` — retry adds a new code path AFTER initial execution completes
- `CompleteTransition` — NOT called during retry
- All resources are cluster-scoped

**From Story 4.05 (Driver Registry Fallback & Preflight Convergence):**
- `Registry.GetDriver(provisioner)` with noop-fallback for dev/CI
- `StorageClassLister.GetProvisioner(ctx, scName)` for storage class resolution
- When `--noop-fallback` is enabled, use `NoOpVMHealthValidator`

**From Epic 3 (Storage Driver Framework):**
- All driver methods are idempotent — retry is inherently safe
- Fake driver has `On*/Return` API — reset between test cases to configure different retry outcomes

### DRExecution Status Example (After Retry Succeeds)

```yaml
# Before retry:
status:
  result: PartiallySucceeded
  waves:
    - waveIndex: 0
      groups:
        - name: "wave-alpha-group-0"
          result: Completed
          vmNames: ["vm-web01"]
        - name: "wave-alpha-group-1"
          result: Failed
          retryCount: 0
          error: "step SetSource failed for ns-erp-database: replication not ready"
          vmNames: ["vm-db01"]

# After retry (wave-alpha-group-1 retried successfully):
status:
  result: Succeeded
  waves:
    - waveIndex: 0
      groups:
        - name: "wave-alpha-group-0"
          result: Completed
          vmNames: ["vm-web01"]
        - name: "wave-alpha-group-1"
          result: Completed
          retryCount: 1
          vmNames: ["vm-db01"]
          startTime: "2026-04-18T11:00:00Z"
          completionTime: "2026-04-18T11:00:30Z"
```

### DRGroupStatus Example (During Retry)

```yaml
# During retry:
apiVersion: soteria.io/v1alpha1
kind: DRGroupStatus
metadata:
  name: failover-2026-04-18-wave-alpha-group-1
spec:
  executionName: failover-2026-04-18
  waveIndex: 0
  groupName: wave-alpha-group-1
  vmNames: ["vm-db01"]
status:
  phase: InProgress
  lastTransitionTime: "2026-04-18T11:00:00Z"
  steps:
    - name: SetSource
      status: Succeeded
      message: "Set source for volume group ns-erp-database"
      timestamp: "2026-04-18T11:00:15Z"
```

### Build Commands

```bash
make manifests    # Regenerate CRD (RetryCount field) + RBAC
make generate     # Regenerate deepcopy for updated types
make test         # All unit tests (new + existing)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make build        # Verify compilation
```

### Project Structure Notes

All files align with the architecture document:
- `pkg/engine/executor.go` — enhanced with `ExecuteRetry`, retry annotation constant, `VMHealthValidator` field
- `pkg/engine/vm_health.go` — new: VM health validation for retry preconditions
- `pkg/controller/drexecution/reconciler.go` — enhanced with retry detection and orchestration
- `pkg/registry/drexecution/strategy.go` — relaxed for PartiallySucceeded retry
- `pkg/apis/soteria.io/v1alpha1/types.go` — RetryCount field added
- `cmd/soteria/main.go` — VMHealthValidator wiring

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.6] — Story acceptance criteria (BDD format): retry trigger, VM validation, result update, rejection on non-standard state
- [Source: _bmad-output/planning-artifacts/epics.md#FR14] — Operator can manually retry a failed DRGroup if VM is in healthy state
- [Source: _bmad-output/planning-artifacts/epics.md#FR15] — Orchestrator rejects retry when starting state is non-standard or unpredictable
- [Source: _bmad-output/planning-artifacts/architecture.md#API & Communication Patterns] — Error model: fail-forward with PartiallySucceeded, checkpointing boundary
- [Source: _bmad-output/planning-artifacts/architecture.md#CRD Status Patterns] — DRExecution result enum, DRGroupResult, DRGroupStatusState
- [Source: _bmad-output/planning-artifacts/architecture.md#Controller Patterns] — Reconcile return patterns, structured logging, context propagation
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — All 7 methods must be idempotent — safe to retry
- [Source: _bmad-output/planning-artifacts/architecture.md#Engine Boundary] — Engine owns workflow execution, receives plan and driver
- [Source: _bmad-output/project-context.md#Critical Implementation Rules] — Fail-forward model, no in-memory state, structured logging
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — Idempotent methods, safe retry
- [Source: _bmad-output/project-context.md#Domain-Specific Safety Rules] — Reject retry if VM in non-standard state
- [Source: _bmad-output/project-context.md#Testing Rules] — envtest over fake client, test naming conventions
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L202-218] — ExecutionResult (Succeeded/PartiallySucceeded/Failed), DRGroupResult (Pending/InProgress/Completed/Failed)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L237-248] — DRExecutionStatus with Waves/Result/CompletionTime
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L261-274] — DRGroupExecutionStatus (Name/Result/VMNames/Error — RetryCount to be added)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L284-323] — DRGroupStatus CRD with Steps, Phase
- [Source: pkg/registry/drexecution/strategy.go#L141-154] — StatusStrategy.ValidateUpdate forbids terminal results — to be relaxed for PartiallySucceeded
- [Source: pkg/drivers/interface.go] — StorageProvider interface (all methods idempotent)
- [Source: pkg/drivers/errors.go] — Sentinel errors: ErrVolumeGroupNotFound, ErrInvalidTransition, ErrDriverNotFound
- [Source: pkg/drivers/registry.go] — Registry.GetDriver, StorageClassLister
- [Source: pkg/drivers/fake/driver.go] — Programmable fake for unit tests
- [Source: _bmad-output/implementation-artifacts/4-5-fail-forward-error-handling-partial-success.md] — Previous story: GroupError, StepRecorder, DRGroupStatus lifecycle, PVCResolver, result computation, fail-forward flow
- [Source: _bmad-output/implementation-artifacts/4-4-disaster-failover-workflow.md] — Previous story: disaster handler, RPO recording
- [Source: _bmad-output/implementation-artifacts/4-3-planned-migration-workflow.md] — Previous story: planned migration handler, VMManager
- [Source: _bmad-output/implementation-artifacts/4-2-drgroup-chunking-wave-executor.md] — Previous story: executor framework, DRGroupHandler, fail-forward, sync.WaitGroup, updateGroupStatus
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — Previous story: state machine, CompleteTransition, admission webhook, controller setup

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Review Findings

- [x] [Review][Defer] Terminal-result annotation cleanup — Succeeded/Failed executions skip before inspecting retry annotation; no RetryRejected condition or annotation removal [reconciler.go:85-92] — deferred, tracked for next story
- [x] [Review][Defer] resolveVMNamespace uses context.Background() and silently returns "" — violates project rule #4, can produce misleading VM health rejections [reconciler.go:484] — deferred, tracked for next story
- [x] [Review][Defer] Duplicate group names in annotation not deduplicated — parseRetryAnnotation preserves dupes, can double-execute same group concurrently [executor.go:961-973] — deferred, tracked for next story

### Debug Log References

### Completion Notes List

### File List

- pkg/apis/soteria.io/v1alpha1/types.go (modified — RetryCount field)
- pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go (auto-generated)
- pkg/engine/executor.go (modified — retry support: annotation parsing, group resolution, ExecuteRetry, VMHealthValidator interface)
- pkg/engine/vm_health.go (new — KubeVirtVMHealthValidator, NoOpVMHealthValidator)
- pkg/engine/vm_health_test.go (new — 5 VM health tests)
- pkg/engine/executor_test.go (modified — 19 retry tests)
- pkg/engine/doc.go (modified — retry mechanism docs)
- pkg/controller/drexecution/reconciler.go (modified — reconcileRetry, annotation lifecycle, VM health gate)
- pkg/registry/drexecution/strategy.go (modified — PartiallySucceeded relaxation)
- pkg/registry/drexecution/strategy_test.go (modified — 4 strategy tests)
- cmd/soteria/main.go (modified — VMHealthValidator wiring)
