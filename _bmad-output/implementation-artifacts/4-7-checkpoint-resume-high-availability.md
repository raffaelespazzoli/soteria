# Story 4.7: Checkpoint, Resume & High Availability

Status: ready-for-dev

## Story

As an operator,
I want in-progress executions to resume from the last checkpoint after a pod restart with at most one in-flight DRGroup lost,
So that DR operations survive orchestrator failures.

## Acceptance Criteria

1. **AC1 — Resume after pod restart:** When the orchestrator pod restarts, the new leader acquires the Kubernetes Lease (NFR2), loads all in-progress DRExecutions from the API server, and resumes each from its last checkpointed state. Completed DRGroups remain completed; at most one in-flight DRGroup operation is lost and retried (NFR1).

2. **AC2 — Per-DRGroup checkpoint writes:** When a DRGroup completes (success or failure), the DRExecution `.status` is updated immediately via the Kubernetes API (kube-apiserver, NOT direct ScyllaDB). The checkpoint includes per-DRGroup result, timestamps, error details, and the current wave progress.

3. **AC3 — Checkpoint write failure handling:** When a checkpoint write fails (e.g., API server temporarily unreachable), the engine retries with exponential backoff. The engine does NOT proceed to the next DRGroup until the checkpoint is persisted. After max retries are exhausted, the group is marked Failed and execution continues fail-forward.

4. **AC4 — Leader election controls workflow engine only:** Leader election (`ctrl.Options{LeaderElection: true}`) gates only the workflow engine reconciliation. All replicas continue serving API requests through the aggregated API server. The leader lease ID is `soteria.io` (already configured in `cmd/soteria/main.go`).

5. **AC5 — Leader failover and resume:** When the active instance fails, a standby instance acquires the Kubernetes Lease within the configured lease duration. The standby resumes all in-progress executions from their last checkpoints (NFR2). No manual re-trigger is required (NFR1).

6. **AC6 — Concurrent execution independence:** Multiple DRPlan executions (separate plans, disjoint VM sets) run independently without interference (NFR11). Checkpointing for one execution does not block another. Each execution's checkpoint writes are independent.

7. **AC7 — State reconstruction on startup:** On startup, controller-runtime syncs informer caches and queues a reconcile for every existing DRExecution. The reconciler does NOT issue a separate `List` — the per-resource reconcile naturally covers all in-progress executions. For each, it reconstructs the execution context from `DRExecution.Status.Waves[]` and `DRGroupStatus` resources, determining which groups are completed, which are failed, and which wave/groups need to resume.

8. **AC8 — In-flight DRGroup idempotent retry:** When an in-flight DRGroup (one that was `InProgress` at crash time) is retried after restart, driver operations are idempotent — re-calling `SetSource` or `StartVM` on an already-completed operation is safe. The `DRGroupStatus` steps are preserved from the pre-crash attempt; new steps are appended.

9. **AC9 — Checkpoint granularity:** Checkpoints are per-DRGroup (not per-step, not per-wave). This is the natural execution boundary — concurrent operations within a DRGroup retry together. A completed wave with all groups Completed/Failed is fully checkpointed.

10. **AC10 — DRExecution status as source of truth:** The DRExecution `.status` (persisted via kube-apiserver → ScyllaDB) is the sole source of truth for resume decisions. No in-memory state survives across reconcile calls or pod restarts. On resume, the reconciler reads `.status` and makes all decisions from that data.

11. **AC11 — Leader election configuration:** The controller manager's leader election is configurable via `--leader-elect` (default false), `--leader-elect-lease-duration` (default 15s), `--leader-elect-renew-deadline` (default 10s), and `--leader-elect-retry-period` (default 2s). The lease ID is `soteria.io`.

12. **AC12 — Checkpoint metrics:** Prometheus metrics are emitted for checkpoint operations: `soteria_checkpoint_writes_total` (counter, labels: execution, result=success|failure), `soteria_checkpoint_write_duration_seconds` (histogram), `soteria_checkpoint_retries_total` (counter). These use the `soteria_` prefix per project convention.

13. **AC13 — Resume event emission:** When a DRExecution is resumed after restart, the controller emits an `ExecutionResumed` event on the DRExecution with the resume point details (wave index, completed groups count, in-flight groups being retried).

14. **AC14 — Unit tests:** Comprehensive tests covering: (a) checkpoint write succeeds — status persisted; (b) checkpoint write fails — retried with backoff; (c) checkpoint write exhausts retries — group marked Failed; (d) resume from mid-wave — completed groups skipped, in-flight retried; (e) resume from between waves — next wave starts; (f) resume with no in-progress executions — no-op; (g) concurrent checkpoints — independent writes; (h) state reconstruction — correct groups identified for resume; (i) idempotent retry — driver calls safe on already-completed operations; (j) leader election gating — only leader reconciles executions.

## Tasks / Subtasks

- [ ] Task 1: Implement Checkpointer for per-DRGroup status persistence (AC: #2, #3, #9)
  - [ ] 1.1 Create `pkg/engine/checkpoint.go` with `Checkpointer` interface: `WriteCheckpoint(ctx context.Context, exec *DRExecution) error` — patches DRExecution `.status` via kube-apiserver
  - [ ] 1.2 Implement `KubeCheckpointer` struct with `client.Client` (controller-runtime client) that performs a status subresource patch (`client.MergeFrom`) on the DRExecution
  - [ ] 1.3 Implement retry logic with exponential backoff: initial=100ms, factor=2, max=5s, `wait.Backoff{Steps: 6}` (1 initial attempt + 5 retries). Use `k8s.io/apimachinery/pkg/util/wait.ExponentialBackoffWithContext` for context-aware cancellation
  - [ ] 1.4 On retry exhaustion, return a sentinel error `ErrCheckpointFailed` that the executor can handle
  - [ ] 1.5 Re-fetch the DRExecution before each retry attempt to get the latest `resourceVersion` (avoid conflict errors)
  - [ ] 1.6 Implement `NoOpCheckpointer` for unit testing: records calls, always succeeds (configurable to fail for error-path tests)
  - [ ] 1.7 Add `Checkpointer` field to `WaveExecutor` struct

- [ ] Task 2: Integrate checkpointing into WaveExecutor (AC: #2, #3, #9)
  - [ ] 2.1 In `pkg/engine/executor.go`, after each DRGroup completes (success or failure) in `executeGroup`, call `checkpointer.WriteCheckpoint(ctx, exec)` before proceeding to the next group
  - [ ] 2.2 If `WriteCheckpoint` returns `ErrCheckpointFailed` (retries exhausted), mark the group as `Failed` with error `"checkpoint write failed after retries"` and continue fail-forward
  - [ ] 2.3 Checkpoint writes are serialized per-execution via the existing `updateGroupStatus` mutex — the mutex is already held when updating group status, so the checkpoint write happens within the same critical section
  - [ ] 2.4 For concurrent DRGroup completions within a wave, each group writes its own checkpoint independently (the mutex serializes them to avoid write conflicts)
  - [ ] 2.5 After all groups in a wave complete and before starting the next wave, write a wave-level checkpoint (update `WaveStatus.CompletionTime`)

- [ ] Task 3: Implement execution state reconstruction (AC: #7, #8, #10)
  - [ ] 3.1 Create `pkg/engine/resume.go` with `ResumeAnalyzer` that examines a DRExecution status and determines the resume point
  - [ ] 3.2 Define `ResumePoint` struct: `WaveIndex int`, `CompletedGroups []string`, `FailedGroups []string`, `InFlightGroups []string`, `PendingGroups []string`, `IsComplete bool`
  - [ ] 3.3 Implement `AnalyzeExecution(exec *DRExecution) ResumePoint` — walks `Status.Waves[]` to find: the first wave with any non-terminal group (InProgress or Pending), all groups by result within that wave, and whether all prior waves are complete
  - [ ] 3.4 Groups with `Result == InProgress` at analysis time are treated as in-flight (crashed mid-execution) — they need retry
  - [ ] 3.5 Groups with no result (not in status at all — wave not yet started) are pending
  - [ ] 3.6 A fully completed execution (`Result == Succeeded|PartiallySucceeded|Failed`) returns `IsComplete: true` — no resume needed

- [ ] Task 4: Enhance DRExecution reconciler for resume-on-startup (AC: #1, #5, #7, #13)
  - [ ] 4.1 In `pkg/controller/drexecution/reconciler.go`, add `Recorder record.EventRecorder` and `ResumeAnalyzer *ResumeAnalyzer` fields to `DRExecutionReconciler`. Wire `Recorder` from `mgr.GetEventRecorderFor("drexecution-controller")` in `main.go` (see Task 6.6)
  - [ ] 4.2 At the start of `Reconcile`, fetch the DRExecution and determine the reconcile path. **Ordering:** (1) if `StartTime != nil` AND `Result == ""` → resume path; (2) if `Result == PartiallySucceeded` AND retry annotation present → retry path (Story 4.6); (3) if `StartTime == nil` → new execution path; (4) if `Result` is terminal (Succeeded/PartiallySucceeded/Failed) → no-op. **Invariant:** `StartTime` is set exactly once — when the controller first dispatches to the executor for a new execution. It is never set before dispatch.
  - [ ] 4.3 If resume is needed, call `ResumeAnalyzer.AnalyzeExecution(exec)` to determine the resume point
  - [ ] 4.4 For in-flight groups (crashed mid-execution), reset their `DRGroupExecutionStatus.Result` to `Pending` and re-execute them — driver operations are idempotent
  - [ ] 4.5 For the resume wave, skip completed/failed groups and execute only pending + in-flight groups using `WaveExecutor.ExecuteFromWave` with the filtered chunk set
  - [ ] 4.6 Emit `ExecutionResumed` event on the DRExecution with resume details: `"Resuming execution from wave %d: %d completed, %d failed, %d retrying"`
  - [ ] 4.7 Continue normal execution for remaining waves after the resume wave completes
  - [ ] 4.8 The reconciler's standard path (new execution) remains unchanged — resume only kicks in when `StartTime != nil` AND `Result == ""`
  - [ ] 4.9 Resume is idempotent: if reconcile fires again during execution, the `Result == ""` check combined with the executor's single-flight guard (set StartTime + dispatch once) prevents double-dispatch

- [ ] Task 5: Add resume-aware execution to WaveExecutor (AC: #1, #7, #8)
  - [ ] 5.1 In `pkg/engine/executor.go`, add `ExecuteFromWave(ctx context.Context, input ExecutionInput, startWaveIndex int, skipGroups map[string]bool) error` that starts execution from a specific wave, skipping already-completed groups
  - [ ] 5.2 For the resume wave, filter out completed and failed groups from the chunk set before executing
  - [ ] 5.3 For subsequent waves (after resume wave), execute normally (all groups)
  - [ ] 5.4 The existing `Execute` method calls `ExecuteFromWave(ctx, input, 0, nil)` for backward compatibility

- [ ] Task 6: Enhance leader election configuration and wire components (AC: #4, #11, #13)
  - [ ] 6.1 In `cmd/soteria/main.go`, add flags: `--leader-elect-lease-duration` (default 15s), `--leader-elect-renew-deadline` (default 10s), `--leader-elect-retry-period` (default 2s)
  - [ ] 6.2 Pass these to `ctrl.Options`: `LeaseDuration`, `RenewDeadline`, `RetryPeriod`
  - [ ] 6.3 Verify that leader election is already wired (it is — `LeaderElection: enableLeaderElection`, `LeaderElectionID: "soteria.io"`)
  - [ ] 6.4 Add structured log message on startup: `"Leader election configured"` with lease duration, renew deadline, retry period
  - [ ] 6.5 The aggregated API server goroutine runs independently of leader election — this is already the case (`server.GenericAPIServer.PrepareRun().RunWithContext(ctx)` runs on the main goroutine, `mgr.Start(ctx)` runs separately)
  - [ ] 6.6 Create `KubeCheckpointer` with `mgr.GetClient()` and pass to `WaveExecutor`
  - [ ] 6.7 Create `ResumeAnalyzer` and pass to `DRExecutionReconciler`
  - [ ] 6.8 Wire `Recorder` on `DRExecutionReconciler` from `mgr.GetEventRecorderFor("drexecution-controller")` (required for AC13 events)

- [ ] Task 7: Add checkpoint Prometheus metrics (AC: #12)
  - [ ] 7.1 Create `pkg/metrics/metrics.go` (file does not exist yet — `pkg/metrics/` only has `doc.go`). Define: `CheckpointWritesTotal` (CounterVec, labels: `execution`, `result`), `CheckpointWriteDuration` (Histogram, buckets: 10ms to 10s), `CheckpointRetriesTotal` (Counter). All use `soteria_` prefix per convention
  - [ ] 7.2 Register metrics with `metrics.Registry` from `sigs.k8s.io/controller-runtime/pkg/metrics` in an `init()` function
  - [ ] 7.3 In `KubeCheckpointer.WriteCheckpoint`, instrument: increment `CheckpointWritesTotal` with result label, observe duration in `CheckpointWriteDuration`, increment `CheckpointRetriesTotal` per retry attempt

- [ ] Task 8: Unit tests for Checkpointer (AC: #14a, #14b, #14c, #14g)
  - [ ] 8.1 Create `pkg/engine/checkpoint_test.go`
  - [ ] 8.2 Test: `TestKubeCheckpointer_WriteSucceeds` — status patch applied, no retries
  - [ ] 8.3 Test: `TestKubeCheckpointer_WriteFailsOnce_RetriesAndSucceeds` — first attempt fails (conflict), second succeeds
  - [ ] 8.4 Test: `TestKubeCheckpointer_WriteExhaustsRetries_ReturnsError` — all retries fail, returns `ErrCheckpointFailed`
  - [ ] 8.5 Test: `TestKubeCheckpointer_RefetchesResourceVersion` — verifies re-GET before each retry
  - [ ] 8.6 Test: `TestKubeCheckpointer_ConcurrentCheckpoints_Independent` — two executions checkpoint simultaneously without interference
  - [ ] 8.7 Test: `TestNoOpCheckpointer_RecordsCalls` — verify call recording for test assertions

- [ ] Task 9: Unit tests for ResumeAnalyzer (AC: #14d, #14e, #14f, #14h)
  - [ ] 9.1 Create `pkg/engine/resume_test.go`
  - [ ] 9.2 Test: `TestResumeAnalyzer_MidWave_IdentifiesResumePoint` — wave 1 has 2 Completed + 1 InProgress → resume point at wave 1, 1 in-flight group
  - [ ] 9.3 Test: `TestResumeAnalyzer_BetweenWaves_StartsNextWave` — wave 0 fully Completed, wave 1 empty → resume at wave 1
  - [ ] 9.4 Test: `TestResumeAnalyzer_NoInProgress_NoResume` — all waves complete (Succeeded) → `IsComplete: true`
  - [ ] 9.5 Test: `TestResumeAnalyzer_PartiallySucceeded_NoResume` — result PartiallySucceeded → `IsComplete: true` (retry is story 4.6, not resume)
  - [ ] 9.6 Test: `TestResumeAnalyzer_AllGroupsPending_StartsFromBeginning` — StartTime set but no groups have results → resume from wave 0
  - [ ] 9.7 Test: `TestResumeAnalyzer_MultipleInFlightGroups_AllRetried` — 3 InProgress groups → all in `InFlightGroups`
  - [ ] 9.8 Test: `TestResumeAnalyzer_MixedWaveStates` — wave 0 Completed, wave 1 has mix of Completed + InProgress + Pending

- [ ] Task 10: Unit tests for resume-aware executor (AC: #14d, #14e, #14i)
  - [ ] 10.1 In `pkg/engine/executor_test.go`, add resume-specific section
  - [ ] 10.2 Test: `TestWaveExecutor_ExecuteFromWave_SkipsCompletedGroups` — wave 1 resume skips 2 completed groups, executes 1 pending
  - [ ] 10.3 Test: `TestWaveExecutor_ExecuteFromWave_RetriesInFlightGroup` — in-flight group re-executed, driver calls idempotent
  - [ ] 10.4 Test: `TestWaveExecutor_ExecuteFromWave_ContinuesNextWave` — after resume wave, next wave executes normally
  - [ ] 10.5 Test: `TestWaveExecutor_Execute_BackwardCompatible` — existing Execute calls work unchanged
  - [ ] 10.6 Test: `TestWaveExecutor_CheckpointAfterEachGroup` — verify checkpointer called after each group completion

- [ ] Task 11: Unit tests for reconciler resume path (AC: #14d, #14f, #14j)
  - [ ] 11.1 Test: `TestDRExecutionReconciler_ResumeInProgress_EmitsEvent` — in-progress execution triggers resume, `ExecutionResumed` event emitted
  - [ ] 11.2 Test: `TestDRExecutionReconciler_CompletedExecution_NoResume` — Succeeded execution does not trigger resume
  - [ ] 11.3 Test: `TestDRExecutionReconciler_NewExecution_NormalPath` — new execution (no StartTime) takes normal path, not resume
  - [ ] 11.4 Test: `TestDRExecutionReconciler_LeaderOnly` — verify reconciler only runs on leader instance (controller-runtime handles this, but verify the setup)

- [ ] Task 12: Integration considerations for checkpoint timing (AC: #2, #3, #6)
  - [ ] 12.1 Verify that `updateGroupStatus` (mutex-protected) + `WriteCheckpoint` happen atomically from the executor's perspective — no group starts until the previous group's checkpoint is written
  - [ ] 12.2 Verify that concurrent DRPlan executions use separate DRExecution resources and separate checkpoint write paths — no shared mutex between executions
  - [ ] 12.3 Document in `pkg/engine/doc.go` the checkpoint timing guarantee: "At most one in-flight DRGroup can be lost on crash — the last group whose checkpoint write completed is the recovery point"

- [ ] Task 13: Update documentation and verify (AC: all)
  - [ ] 13.1 Update `pkg/engine/doc.go` to cover: checkpoint mechanism, resume-on-startup flow, leader election gating, checkpoint metrics
  - [ ] 13.2 Add godoc on `Checkpointer` interface explaining the distributed systems rationale: checkpoint via kube-apiserver ensures consistency through the standard Kubernetes optimistic concurrency model
  - [ ] 13.3 Add godoc on `ResumeAnalyzer` explaining the state reconstruction algorithm
  - [ ] 13.4 Add godoc on `ExecuteFromWave` explaining the resume execution flow
  - [ ] 13.5 Run `make manifests` — no CRD changes expected (no type modifications)
  - [ ] 13.6 Run `make generate` — no deepcopy changes expected
  - [ ] 13.7 Run `make test` — all unit tests pass (new + existing)
  - [ ] 13.8 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 13.9 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4.7 of Epic 4 (DR Workflow Engine — Full Lifecycle). It implements checkpoint persistence and crash-recovery resume for DR workflow executions, plus proper leader election configuration for high availability (NFR1, NFR2, NFR11). All preceding Epic 4 stories (4.05 through 4.6) are prerequisites — they build the executor, handlers, state machine, DRGroupStatus lifecycle, and retry mechanism that this story hardens with checkpoint/resume.

**Story 4.7 scope:** Per-DRGroup checkpoint writes to DRExecution.Status via kube-apiserver, state reconstruction on startup, resume-aware execution (skip completed groups, retry in-flight groups), leader election configuration, and checkpoint metrics. This story does NOT add new CRDs or modify existing types — it uses the existing DRExecution status structure as the checkpoint store.

**Prerequisites:** Stories 4.05 through 4.6 MUST be implemented and merged before this story. The engine files (`executor.go`, `statemachine.go`, `planned.go`, `disaster.go`, `vm_health.go`, `pvc_resolver.go`) and the full DRExecution reconciler do not exist in the codebase today — they are delivered by those stories. The types in `pkg/apis/soteria.io/v1alpha1/types.go` (including Story 4.6's `RetryCount` field) must be in place. This story adds checkpoint/resume on top of the complete executor infrastructure.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — provides Transition, CompleteTransition, controller skeleton |
| 4.2 | Wave executor framework + controller dispatch | Prerequisite — provides WaveExecutor, DRGroupHandler, fail-forward, updateGroupStatus |
| 4.3 | Planned migration workflow + VMManager | Prerequisite — provides PlannedMigrationHandler |
| 4.4 | Disaster failover workflow | Prerequisite — provides DisasterFailoverHandler |
| 4.5 | Fail-forward error handling & partial success | Prerequisite — provides GroupError, StepRecorder, DRGroupStatus lifecycle, PVCResolver |
| 4.6 | Failed DRGroup retry | Prerequisite — provides retry mechanism, VMHealthValidator, strategy relaxation |
| **4.7** | **Checkpoint, resume & HA** | **This story — checkpoint writes, resume-on-startup, leader election, metrics** |
| 4.8 | Re-protect & failback workflows | Reprotecting + FailingBack paths |

### Critical Design Decision: Checkpoint Granularity

**Per-DRGroup** is the checkpoint boundary, not per-step and not per-wave.

| Granularity | Verdict | Reason |
|-------------|---------|--------|
| Per-step | Rejected | Too many API server writes (one per driver call); high latency overhead; partial group state is complex to resume from |
| **Per-DRGroup** | **Selected** | Natural execution boundary; matches fail-forward unit; concurrent ops within a DRGroup retry together; driver operations are idempotent so retry is safe; matches architecture doc recommendation |
| Per-wave | Rejected | Too coarse — losing an entire wave of DRGroups on crash violates NFR1 (at most one in-flight DRGroup lost) |

### Critical Design Decision: Checkpoint Storage Path

**kube-apiserver (DRExecution `.status` subresource)** is the checkpoint store.

The architecture document explicitly states: "the DRExecution `.status` is updated immediately via the Kubernetes API (through kube-apiserver, not direct ScyllaDB)." The controller communicates via standard client-go through kube-apiserver — never touches ScyllaDB directly. This ensures:
- Optimistic concurrency via resourceVersion
- Admission validation (if any)
- Audit trail through API server audit logs
- Consistent with all other controller status writes

The data path: controller → kube-apiserver → aggregated API server → ScyllaDB storage.Interface → ScyllaDB. The controller never bypasses this chain.

### Critical Design Decision: Resume Strategy

**Read-status-and-resume** on reconcile, not startup hook.

| Option | Verdict | Reason |
|--------|---------|--------|
| Manager startup callback | Rejected | Runs before informer caches are synced; would need raw client calls; not standard controller-runtime pattern |
| **Reconcile-based resume** | **Selected** | Controller-runtime triggers reconcile for all existing resources on startup (cache sync); standard pattern; no special startup code needed; each DRExecution reconcile checks if resume is needed |
| Separate resume controller | Rejected | Unnecessary duplication; the DRExecution reconciler is the natural owner |

When the manager starts and caches sync, controller-runtime queues a reconcile for every existing DRExecution. The reconciler checks each one: if `StartTime != nil` AND `Result == ""`, it's an in-progress execution that needs resume. This naturally handles leader failover — the new leader's reconciler picks up all in-progress executions.

### Critical Design Decision: In-Flight Group Recovery

When a DRGroup was `InProgress` at crash time, we **retry the entire group**.

**Why this is safe:** All StorageProvider driver methods are idempotent (architecture mandate, enforced by conformance tests). Re-calling `SetSource(force=false)` on a volume that's already Source is a no-op. Re-calling `StartVM` on an already-running VM is a no-op. The DRGroupStatus `Steps` from the pre-crash attempt are preserved — the retry appends new steps (the handler records steps via StepRecorder regardless of prior state).

**What's lost:** At most the work of one DRGroup that was mid-execution. If the DRGroup had 3 VMs and 2 had already been promoted when the crash occurred, the retry re-promotes all 3 (the first 2 are idempotent no-ops) and starts the third.

### Critical Design Decision: Resume vs Retry Reconcile Ordering

The reconciler has three non-overlapping code paths. Their conditions are mutually exclusive:

| Condition | Path | Owner |
|-----------|------|-------|
| `StartTime == nil` | New execution — first dispatch | Story 4.1 |
| `StartTime != nil` AND `Result == ""` | Resume after crash — pick up from checkpoint | **Story 4.7** |
| `Result == PartiallySucceeded` AND retry annotation present | Manual retry of failed groups | Story 4.6 |
| `Result` is terminal (Succeeded/PartiallySucceeded without annotation/Failed) | No-op — nothing to do | N/A |

**Key invariant:** `StartTime` is set exactly once by the controller when it first dispatches to the executor. It is never set during webhook admission or spec validation. This guarantees `StartTime == nil` identifies a brand-new DRExecution that has never been dispatched. `Result == ""` (empty) means execution is in-progress — it transitions to a terminal value only when all waves complete or the execution is abandoned.

### Critical Design Decision: Checkpoint Write Failure Handling

**Retry with backoff, then fail-forward.**

The checkpoint write goes through the Kubernetes API, which can transiently fail (leader election churn, network blips, API server rolling restart). The retry policy:
- Initial backoff: 100ms
- Backoff factor: 2x
- Max backoff: 5s
- Max retries: 5
- Total worst-case delay: ~6s (100ms + 200ms + 400ms + 800ms + 1.6s + 3.2s)

If all retries fail, the group is marked `Failed` with error `"checkpoint write failed after retries"` and execution continues fail-forward. The operator can retry the group via Story 4.6's retry annotation. This is a pragmatic choice: a persistent API server outage should not hang the execution indefinitely.

### Existing Code to Reuse (Critical — Do NOT Reinvent)

| File | What It Provides | How This Story Uses It |
|------|-----------------|----------------------|
| `pkg/engine/executor.go` (Stories 4.2-4.6) | `WaveExecutor`, `DRGroupHandler`, `ExecutionGroup`, `executeWave`, `executeGroup`, `updateGroupStatus`, `ExecuteRetry`, fail-forward loop | Add `Checkpointer` field; call `WriteCheckpoint` after each group in `executeGroup`; add `ExecuteFromWave` for resume |
| `pkg/engine/planned.go` (Story 4.3) | `PlannedMigrationHandler` | Unchanged — handler is checkpoint-agnostic |
| `pkg/engine/disaster.go` (Story 4.4) | `DisasterFailoverHandler` | Unchanged — handler is checkpoint-agnostic |
| `pkg/engine/statemachine.go` (Story 4.1) | `Transition`, `CompleteTransition` | Unchanged — state machine is checkpoint-agnostic; resume does NOT re-transition (plan phase was already advanced) |
| `pkg/engine/vm_health.go` (Story 4.6) | `VMHealthValidator` | Unchanged — used by retry, not checkpoint |
| `pkg/engine/pvc_resolver.go` (Story 4.5) | `PVCResolver` | Reuse for driver resolution during resume re-execution |
| `pkg/drivers/interface.go` | `StorageProvider` — all 7 methods are idempotent | Idempotency guarantee makes in-flight retry safe |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing resume with configurable driver responses |
| `pkg/drivers/registry.go` | `Registry.GetDriver`, `StorageClassLister` | Driver resolution for resumed groups |
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRExecution`, `DRExecutionStatus`, `WaveStatus`, `DRGroupExecutionStatus`, `DRGroupStatus`, `ExecutionResult`, `DRGroupResult` | All types reused — no modifications needed. `DRGroupResult` includes `Pending`, `InProgress`, `Completed`, `Failed` which are sufficient for checkpoint/resume state |
| `pkg/controller/drexecution/reconciler.go` (Stories 4.1-4.6) | DRExecution reconciler with new-execution flow and retry flow | Add resume detection at reconcile start; call `ResumeAnalyzer` + `ExecuteFromWave` |
| `cmd/soteria/main.go` | Manager with `LeaderElection: enableLeaderElection`, `LeaderElectionID: "soteria.io"` | Add lease duration/renew/retry flags; create `KubeCheckpointer`; pass to `WaveExecutor` |
| `pkg/metrics/metrics.go` | Prometheus metrics with `soteria_` prefix | Add checkpoint metrics |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/engine/executor.go` (Stories 4.2-4.6) | `WaveExecutor` with `Execute`, `ExecuteRetry`, no checkpointing | Add `Checkpointer` field; call `WriteCheckpoint` after each group; add `ExecuteFromWave` method; `Execute` delegates to `ExecuteFromWave(ctx, input, 0, nil)` |
| `pkg/controller/drexecution/reconciler.go` (Stories 4.1-4.6) | Handles new executions and retry | Add resume detection: check `StartTime != nil && Result == ""` → analyze → `ExecuteFromWave`; emit `ExecutionResumed` event |
| `cmd/soteria/main.go` (Stories 4.1-4.6) | Leader election with `--leader-elect` flag only | Add `--leader-elect-lease-duration`, `--leader-elect-renew-deadline`, `--leader-elect-retry-period` flags; create `KubeCheckpointer`; pass to executor |
| `pkg/metrics/metrics.go` | Does not exist yet (`pkg/metrics/` only has `doc.go`) | **Create** this file with checkpoint metrics: `soteria_checkpoint_writes_total`, `soteria_checkpoint_write_duration_seconds`, `soteria_checkpoint_retries_total` |
| `pkg/engine/doc.go` | Covers discovery, consistency, chunking, executor, handlers, fail-forward, retry | Add checkpoint mechanism, resume-on-startup, leader election sections |

### New Files to Create

| File | Purpose |
|------|---------|
| `pkg/engine/checkpoint.go` | `Checkpointer` interface, `KubeCheckpointer` (status patch + retry), `NoOpCheckpointer` (testing), `ErrCheckpointFailed` sentinel |
| `pkg/engine/checkpoint_test.go` | Checkpointer unit tests |
| `pkg/engine/resume.go` | `ResumeAnalyzer`, `ResumePoint` struct, `AnalyzeExecution` method |
| `pkg/engine/resume_test.go` | ResumeAnalyzer unit tests |
| `pkg/metrics/metrics.go` | Prometheus metrics: checkpoint counters, histogram, retry counter. Registered via `init()` with controller-runtime `metrics.Registry` |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/engine/statemachine.go` | State machine — not checkpoint-aware; resume does not re-transition |
| `pkg/engine/planned.go` | Handler — checkpoint-agnostic; no changes needed |
| `pkg/engine/disaster.go` | Handler — checkpoint-agnostic; no changes needed |
| `pkg/engine/chunker.go` | Chunker — no changes |
| `pkg/engine/discovery.go` | VM discovery — reuse for resume driver resolution |
| `pkg/engine/consistency.go` | Consistency resolution — no changes |
| `pkg/engine/vm.go` | VMManager — no changes |
| `pkg/engine/vm_health.go` | VMHealthValidator — no changes |
| `pkg/engine/pvc_resolver.go` | PVCResolver — reuse for resume, no changes |
| `pkg/engine/handler_noop.go` | NoOpHandler — no changes |
| `pkg/apis/soteria.io/v1alpha1/types.go` | All types sufficient — no new fields needed for checkpoint/resume |
| `pkg/registry/drexecution/strategy.go` | Strategy — no changes |
| `pkg/drivers/*` | All driver code — no changes |
| `pkg/admission/*` | Admission webhooks — no changes |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes |

### Key Implementation Patterns

**1. Checkpoint write via status subresource patch:**

```go
type KubeCheckpointer struct {
    Client client.Client
}

func (c *KubeCheckpointer) WriteCheckpoint(ctx context.Context, exec *v1alpha1.DRExecution) error {
    var lastErr error
    backoff := wait.Backoff{
        Duration: 100 * time.Millisecond,
        Factor:   2.0,
        Cap:      5 * time.Second,
        Steps:    6,
    }
    err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
        fresh := &v1alpha1.DRExecution{}
        if err := c.Client.Get(ctx, client.ObjectKeyFromObject(exec), fresh); err != nil {
            lastErr = err
            return false, nil
        }
        fresh.Status = exec.Status
        if err := c.Client.Status().Update(ctx, fresh); err != nil {
            lastErr = err
            return false, nil
        }
        return true, nil
    })
    if err != nil {
        return fmt.Errorf("checkpoint write failed after retries: %w", lastErr)
    }
    return nil
}
```

**2. Resume detection in reconciler:**

```go
func (r *DRExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx).WithValues("drexecution", req.Name)
    
    var exec v1alpha1.DRExecution
    if err := r.Get(ctx, req.NamespacedName, &exec); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Resume path: in-progress execution needs resume after restart
    if exec.Status.StartTime != nil && exec.Status.Result == "" {
        resumePoint := r.ResumeAnalyzer.AnalyzeExecution(&exec)
        if !resumePoint.IsComplete {
            logger.Info("Resuming execution", "waveIndex", resumePoint.WaveIndex,
                "completedGroups", len(resumePoint.CompletedGroups),
                "inFlightGroups", len(resumePoint.InFlightGroups))
            // ... dispatch to ExecuteFromWave
        }
    }
    // ... existing new-execution and retry paths
}
```

**3. State reconstruction logic:**

```go
func (a *ResumeAnalyzer) AnalyzeExecution(exec *v1alpha1.DRExecution) ResumePoint {
    // Terminal results — no resume
    if exec.Status.Result != "" {
        return ResumePoint{IsComplete: true}
    }
    
    for i, wave := range exec.Status.Waves {
        for _, group := range wave.Groups {
            switch group.Result {
            case v1alpha1.DRGroupResultCompleted:
                // completed — skip on resume
            case v1alpha1.DRGroupResultFailed:
                // failed — skip on resume (retry is story 4.6)
            case v1alpha1.DRGroupResultInProgress:
                // in-flight at crash — needs retry
            default:
                // pending — needs execution
            }
        }
        // If wave has any non-terminal groups, this is the resume wave
    }
    // ...
}
```

**4. Structured logging (project convention):**

```go
logger.Info("Checkpoint written", "execution", exec.Name, "wave", waveIdx, "group", groupName)
logger.Info("Checkpoint write failed, retrying", "execution", exec.Name, "attempt", attempt, "error", err)
logger.Info("Checkpoint write exhausted retries", "execution", exec.Name, "group", groupName, "lastError", err)
logger.Info("Resuming execution", "execution", exec.Name, "waveIndex", resumePoint.WaveIndex,
    "completed", len(resumePoint.CompletedGroups), "inFlight", len(resumePoint.InFlightGroups))
logger.Info("Execution resume complete", "execution", exec.Name, "result", result)
```

**5. Event emission:**

```go
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "ExecutionResumed", "Checkpoint",
    "Resumed execution %s from wave %d: %d completed, %d in-flight retrying",
    exec.Name, resumePoint.WaveIndex, len(resumePoint.CompletedGroups), len(resumePoint.InFlightGroups))
```

**6. Context propagation (architecture mandate):**

All methods in `checkpoint.go` and `resume.go` MUST accept `ctx context.Context` as the first parameter and propagate it to all client calls. Never create `context.Background()` or `context.TODO()` — always use the `ctx` from the reconcile handler. This enables cancellation on leader election loss and request tracing.

**7. Error wrapping (project convention):**

```go
return fmt.Errorf("writing checkpoint for execution %s group %s: %w", exec.Name, groupName, err)
return fmt.Errorf("analyzing execution %s for resume: %w", exec.Name, err)
return fmt.Errorf("resuming execution %s from wave %d: %w", exec.Name, waveIdx, err)
```

### Leader Election Architecture

```
┌──────────────────────────────────────────────────┐
│              Kubernetes Cluster                   │
│                                                   │
│  ┌─────────────────┐    ┌─────────────────┐      │
│  │ Pod A (Leader)   │    │ Pod B (Standby)  │      │
│  │                  │    │                  │      │
│  │ ┌──────────────┐ │    │ ┌──────────────┐ │      │
│  │ │ API Server   │ │    │ │ API Server   │ │      │
│  │ │ (active)     │ │    │ │ (active)     │ │      │
│  │ └──────────────┘ │    │ └──────────────┘ │      │
│  │                  │    │                  │      │
│  │ ┌──────────────┐ │    │ ┌──────────────┐ │      │
│  │ │ Controller   │ │    │ │ Controller   │ │      │
│  │ │ Manager      │ │    │ │ Manager      │ │      │
│  │ │ (LEADER)     │ │    │ │ (standby)    │ │      │
│  │ │ - reconciles │ │    │ │ - NO reconcile│ │      │
│  │ │ - checkpoints│ │    │ │ - waiting     │ │      │
│  │ └──────────────┘ │    │ └──────────────┘ │      │
│  └─────────────────┘    └─────────────────┘      │
│                                                   │
│  ┌─────────────────────────────────┐              │
│  │ Kubernetes Lease: soteria.io    │              │
│  │ holder: pod-a                   │              │
│  │ leaseDuration: 15s              │              │
│  │ renewDeadline: 10s              │              │
│  └─────────────────────────────────┘              │
└──────────────────────────────────────────────────┘
```

Both pods serve API requests (active/active for reads). Only the leader runs the controller manager's reconcile loops (workflow engine). On leader failure, Pod B acquires the lease and its controller manager starts reconciling, picking up in-progress executions via checkpoint resume.

### Checkpoint Timing Diagram

```
Wave 1:
  Group A starts → Group A completes → Checkpoint A → ack
  Group B starts → Group B completes → Checkpoint B → ack
  Group C starts → Group C fails    → Checkpoint C → ack
  Wave 1 complete → Wave checkpoint → ack

Wave 2:
  Group D starts → [CRASH HERE]
  
After restart:
  Resume → Read status → Wave 1 complete (A,B=Completed, C=Failed)
                        → Wave 2, Group D was InProgress → retry Group D
  Group D starts (idempotent retry) → Group D completes → Checkpoint D → ack
  Wave 2 complete → ...
```

**Invariant:** At most Group D's work is lost (the one in-flight group at crash time). Groups A, B, C are checkpointed and never re-executed.

### Resume Flow

```
1. Controller manager starts, caches sync
   ↓
2. Controller-runtime queues reconcile for all DRExecutions
   ↓
3. For each DRExecution:
   a. Fetch from API server
   b. Check: StartTime != nil AND Result == "" ?
      ↓ Yes                           ↓ No
      Resume path                     Normal/retry/skip path
      ↓
   c. AnalyzeExecution → ResumePoint
      ↓
   d. For InProgress groups: reset to Pending (they'll be re-executed)
   e. Emit ExecutionResumed event
   f. Call ExecuteFromWave(ctx, input, resumePoint.WaveIndex, completedGroups)
      ↓
   g. Skip completed/failed groups in resume wave
   h. Execute pending + reset in-flight groups
   i. Continue with remaining waves
      ↓
   j. Normal completion: result computation, CompleteTransition (if not already done)
```

### RBAC Requirements

No new RBAC markers needed beyond what Stories 4.1-4.6 established. The DRExecution controller already has:

```go
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drgroupstatuses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
```

The status update RBAC covers checkpoint writes. The `coordination.k8s.io/leases` RBAC is handled automatically by controller-runtime's leader election.

### Test Strategy

**Checkpointer tests** (`pkg/engine/checkpoint_test.go`): Use controller-runtime fake client to simulate API server interactions. Configure the fake client to return errors for specific calls to test retry behavior. Verify status patches are applied correctly.

**ResumeAnalyzer tests** (`pkg/engine/resume_test.go`): Pure unit tests — no Kubernetes client needed. Construct `DRExecution` objects with various status states and verify the `ResumePoint` output.

**Executor resume tests** (`pkg/engine/executor_test.go`): Use mock handlers and `NoOpCheckpointer` to test `ExecuteFromWave`. Verify that completed groups are skipped, in-flight groups are retried, and subsequent waves execute normally.

**Reconciler resume tests** (envtest or mock): Test the resume detection path in the reconciler. Create a DRExecution with `StartTime` set and `Result` empty, trigger reconcile, verify resume logic is invoked and event is emitted.

### Previous Story Intelligence

**From Story 4.6 (Failed DRGroup Retry):**
- Retry annotation mechanism is orthogonal to checkpoint/resume — retry handles PartiallySucceeded results, checkpoint handles in-progress (empty result) executions
- Strategy relaxation for PartiallySucceeded does not affect checkpoint: checkpoint operates on in-progress executions with no result yet
- `RetryCount` field exists on `DRGroupExecutionStatus` — resume does not increment this (only retry does)
- Event recording pattern established: use `r.Recorder.Eventf` with structured messages

**From Story 4.5 (Fail-Forward Error Handling & Partial Success):**
- `GroupError` and `StepRecorder` — checkpoint-agnostic; handlers record steps regardless
- DRGroupStatus lifecycle: Created → InProgress → Completed/Failed — resume respects this lifecycle
- `updateGroupStatus` mutex pattern — checkpoint writes integrate with this mutex
- `CompleteTransition` gating: only called for Succeeded/PartiallySucceeded — resume must call this after final result computation if not already called

**From Story 4.2 (DRGroup Chunking & Wave Executor):**
- `WaveExecutor.Execute` — the main entry point. This story adds `ExecuteFromWave` alongside it
- `sync.WaitGroup` for within-wave concurrency — same pattern in resume
- No-op handler — useful for resume unit tests

**From Story 4.1 (DR State Machine & Execution Controller):**
- Controller idempotency: reconcile checks `StartTime != nil` for existing executions — resume adds a new code path for `StartTime != nil && Result == ""`
- `SetupWithManager` watches DRExecution — cache sync on startup triggers reconcile for all existing resources

**From Epic 3 (Storage Driver Framework):**
- All driver methods are idempotent — this is the foundation that makes in-flight DRGroup retry safe after crash
- Conformance tests verify idempotency — no additional conformance testing needed for checkpoint

### Build Commands

```bash
make manifests    # Regenerate RBAC (no CRD changes expected)
make generate     # No deepcopy changes expected
make test         # All unit tests (new + existing)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make build        # Verify compilation
```

### Project Structure Notes

All files align with the architecture document:
- `pkg/engine/checkpoint.go` — new: Checkpointer interface + KubeCheckpointer + NoOpCheckpointer
- `pkg/engine/resume.go` — new: ResumeAnalyzer + ResumePoint + state reconstruction
- `pkg/engine/executor.go` — enhanced with Checkpointer field and ExecuteFromWave method
- `pkg/controller/drexecution/reconciler.go` — enhanced with resume detection path
- `cmd/soteria/main.go` — enhanced with leader election tuning flags and KubeCheckpointer wiring
- `pkg/metrics/metrics.go` — enhanced with checkpoint metrics

The engine boundary is maintained: `pkg/engine/` knows about plans, drivers, and the Kubernetes API client for checkpoints — it does NOT know about ScyllaDB, CDC, or aggregated API server internals.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.7] — Story acceptance criteria (BDD format): resume after restart, checkpoint writes, leader election, concurrent execution independence
- [Source: _bmad-output/planning-artifacts/epics.md#NFR1] — Resume from last checkpoint, at most one in-flight DRGroup lost
- [Source: _bmad-output/planning-artifacts/epics.md#NFR2] — Active/passive via Kubernetes Leases, standby resumes operations
- [Source: _bmad-output/planning-artifacts/epics.md#NFR11] — Multiple concurrent DRPlan executions without interference
- [Source: _bmad-output/planning-artifacts/prd.md#Reliability] — NFR1-NFR5: checkpoint resume, leader election, 99% success target
- [Source: _bmad-output/planning-artifacts/prd.md#Cross-Site Shared State] — FR26-FR30: both clusters serve same data, local writes during partition
- [Source: _bmad-output/planning-artifacts/prd.md#Audit & Compliance] — FR41-FR43: immutable DRExecution records, execution history
- [Source: _bmad-output/planning-artifacts/architecture.md#API & Communication Patterns] — Checkpointing: per-DRGroup, DRExecution status updated after each group
- [Source: _bmad-output/planning-artifacts/architecture.md#Infrastructure & Deployment] — Single Go binary, leader election for workflow engine only, all replicas serve API
- [Source: _bmad-output/planning-artifacts/architecture.md#Controller Patterns] — Reconcile returns, context propagation, structured logging
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — All 7 methods idempotent — safe to retry after crash
- [Source: _bmad-output/planning-artifacts/architecture.md#Anti-Patterns] — No in-memory state across reconcile calls; no direct ScyllaDB from controller
- [Source: _bmad-output/planning-artifacts/architecture.md#Engine Boundary] — Engine writes checkpoints via Kubernetes API, does not know ScyllaDB
- [Source: _bmad-output/project-context.md#Checkpointing & Resilience] — Per-DRGroup checkpoint, pod restart resumes, driver idempotency
- [Source: _bmad-output/project-context.md#Critical Don't-Miss Rules] — No in-memory state, no direct ScyllaDB, no context.Background in library code
- [Source: _bmad-output/project-context.md#Framework-Specific Rules] — Leader election via ctrl.Options, kube-apiserver only path
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — DRExecution, DRExecutionStatus, WaveStatus, DRGroupExecutionStatus, DRGroupResult (Pending/InProgress/Completed/Failed)
- [Source: cmd/soteria/main.go] — LeaderElection: enableLeaderElection, LeaderElectionID: "soteria.io"
- [Source: pkg/controller/drexecution/reconciler.go] — Current stub reconciler
- [Source: _bmad-output/implementation-artifacts/4-6-failed-drgroup-retry.md] — Previous story: retry mechanism, VMHealthValidator, strategy relaxation, event patterns
- [Source: _bmad-output/implementation-artifacts/4-5-fail-forward-error-handling-partial-success.md] — Previous story: GroupError, StepRecorder, DRGroupStatus lifecycle, updateGroupStatus mutex

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
