# Story 10.8: Sequential Chunk Execution Within Waves

Status: done

## Story

As a DR operator running planned migrations across a multi-DC ScyllaDB-backed environment,
I want DRGroup chunks within a wave to execute sequentially (one after another) rather than concurrently,
So that checkpoint writes are uncontested, eliminating false PartiallySucceeded outcomes caused by resource version conflicts between concurrent group writers.

## Background

### Problem Statement

UAT-10.001 demonstrated that concurrent chunks within a wave race to checkpoint progress on the shared DRExecution resource. When multiple groups complete nearly simultaneously, they each attempt `Get` → `Status().Update()` on the same DRExecution, causing optimistic concurrency conflicts. With ScyllaDB eventual consistency amplifying resourceVersion staleness across DCs, the 8-retry budget can be exhausted, marking a group as Failed even though all VM operations succeeded.

### Root Cause (from UAT log)

Wave 3 of `fedora-app-pm-01` had 2 groups (`wave-3-group-0`: webserver-1+2, `wave-3-group-1`: webserver-3). Both ran as concurrent goroutines. Group-1 exhausted checkpoint retries because group-0's checkpoints (and the subsequent wave-level checkpoint) kept bumping the resourceVersion, invalidating group-1's writes.

### Design Decision

**Option A (fail-forward preserved):** Serialize chunks within a wave. Each chunk completes independently regardless of prior chunk failures. Internal VM-level parallelism within each chunk (StopReplication per VG, StartVM per VM) is preserved via the handler's goroutines.

The `maxConcurrentFailovers` field on DRPlanSpec already controls the **chunk size** (how many VMs process together). After this change it also implicitly controls the **concurrency boundary** — all VMs within a chunk run in parallel, but chunks themselves are sequential.

### Architecture Change

```
BEFORE:
  Wave N:
    chunk-0 ──┐
    chunk-1 ──┤── all goroutines, wg.Wait()
    chunk-2 ──┘
    wave-level checkpoint

AFTER:
  Wave N:
    chunk-0 → (VM ops in parallel) → checkpoint → done
    chunk-1 → (VM ops in parallel) → checkpoint → done
    chunk-2 → (VM ops in parallel) → checkpoint → done
    wave-level checkpoint (completion time only)
```

## Acceptance Criteria

1. **AC1 — Sequential chunk execution:** `executeWave` iterates chunks sequentially via a plain `for` loop. Chunk N+1 starts only after chunk N's `executeGroup` returns (including its checkpoint write). No `sync.WaitGroup` or goroutine fan-out for chunks.

2. **AC2 — Fail-forward preserved:** A failed chunk (result=Failed) does NOT prevent subsequent chunks from executing. The loop continues to the next chunk unconditionally.

3. **AC3 — Internal VM-level parallelism preserved:** Within `executeGroup`, the handler's `ExecuteGroupWithSteps` / `ExecuteGroup` call continues to run VM operations concurrently (StopReplication per VG, StartVM per VM are parallelized inside `FailoverHandler` / `StepHandler`). No change to handler internals.

4. **AC4 — Checkpoint uncontested:** With sequential chunks, only one goroutine writes to the DRExecution at any given time. The existing retry budget (8 steps) handles cross-DC replication lag only — not same-process contention.

5. **AC5 — Retry path serialized:** `executeRetryWave` also serializes retry targets within a wave using a plain `for` loop (matching the primary execution path). Retry waves are already sequential (Story 4.6); this makes retry groups within a wave sequential too.

6. **AC6 — Context cancellation respected:** The sequential loop checks `ctx.Err()` between chunks. If cancelled, remaining chunks are skipped (same pattern as wave-level cancellation in `Execute`).

7. **AC7 — doc.go updated:** Update the `Wave executor` paragraph in `pkg/engine/doc.go` to say "chunks within each wave sequentially" instead of "concurrent DRGroups within each wave". Remove the "Concurrent DRPlan executions use separate DRExecution resources" sentence about statusMu since the concurrent-writer concern is eliminated for same-execution groups.

8. **AC8 — Test updates:** 
   - `TestWaveExecutor_ConcurrentDRGroups` → rename to `TestWaveExecutor_SequentialChunks` and replace the barrier-based concurrency assertion with a sequential-ordering assertion (group-0 completion timestamp < group-1 start timestamp).
   - `TestWaveExecutor_FailForward_MultipleGroups` — verify fail-forward still works with sequential chunks (group-0 fails, group-1 still runs).
   - New test: `TestWaveExecutor_SequentialChunks_CheckpointOrdering` — with 3 chunks, verify checkpoint calls arrive in order (chunk-0, chunk-1, chunk-2) using NoOpCheckpointer.GetCalls().
   - `TestWaveExecutor_RetryWaveOrdering` — verify retry groups within a wave are sequential (no concurrent goroutines).

9. **AC9 — No API/schema changes:** No changes to CRD types, OpenAPI, or stored data format. Existing DRExecution records remain valid. This is a pure scheduling change.

## Tasks / Subtasks

- [x] Task 1: Serialize `executeWave` (AC: #1, #2, #6)
  - [x] 1.1 In `pkg/engine/executor.go`, replace the `sync.WaitGroup` + goroutine fan-out in `executeWave` with a plain `for` loop calling `e.executeGroup` directly
  - [x] 1.2 Add `ctx.Err()` check between iterations to respect context cancellation
  - [x] 1.3 Verified `"sync"` import still needed (`sync.Mutex` used by `statusMu` and `driverCacheMu`)

- [x] Task 2: Serialize `executeRetryWave` (AC: #5, #6)
  - [x] 2.1 In `pkg/engine/executor.go`, replace the `sync.WaitGroup` + goroutine fan-out in `executeRetryWave` with a plain `for` loop calling `e.executeRetryGroup` directly
  - [x] 2.2 Add `ctx.Err()` check between retry group iterations

- [x] Task 3: Update doc.go (AC: #7)
  - [x] 3.1 Updated the "Wave executor" paragraph: "concurrent DRGroups within each wave" → "DRGroup chunks within each wave also executing sequentially. VM-level parallelism within each chunk is preserved by the handler's internal goroutines"
  - [x] 3.2 Updated the "Per-DRGroup checkpointing" paragraph: replaced concurrent-writer language with "sequential chunk execution" framing, kept note about separate executions being independent
  - [x] 3.3 Updated "Checkpoint timing guarantee" — "only one chunk writes to the DRExecution at any given time" replaces the old statusMu-centric description

- [x] Task 4: Update tests (AC: #8)
  - [x] 4.1 Renamed `TestWaveExecutor_ConcurrentDRGroups` to `TestWaveExecutor_SequentialChunks`, replaced barrier-based concurrency assertion with `sequentialOrderHandler` that records start/completion timestamps per group and asserts group-0.completion <= group-1.start
  - [x] 4.2 Added `TestWaveExecutor_SequentialChunks_CheckpointOrdering` — 3 VMs with maxConcurrentFailovers=1 (3 chunks), verifies 4 checkpoint calls (3 groups + 1 wave) and deterministic group name ordering via sequentialOrderHandler events
  - [x] 4.3 Verified `TestWaveExecutor_FailForward_MultipleGroups` still passes (sequential fail-forward)
  - [x] 4.4 Expanded `TestWaveExecutor_RetryWaveOrdering` — added 2 failed groups in same wave (alpha) to verify intra-wave sequential retry ordering (group-0 before group-1), plus cross-wave ordering (alpha before gamma)

- [x] Task 5: Verify all tests pass (AC: #9)
  - [x] 5.1 Run `make test` — all unit tests pass
  - [x] 5.2 Run `make lint` — zero lint issues
  - [x] 5.3 Run `make integration` — all integration tests pass, no regressions

## Dev Notes

### Key locations

| File | Function | Change |
|------|----------|--------|
| `pkg/engine/executor.go:426-449` | `executeWave` | Replace WaitGroup+goroutines with sequential for-loop |
| `pkg/engine/executor.go:1358-1378` | `executeRetryWave` | Same pattern — sequential for-loop |
| `pkg/engine/doc.go` | Package doc | Update architecture description |
| `pkg/engine/executor_test.go:331-368` | `TestWaveExecutor_ConcurrentDRGroups` | Rename + rewrite assertions |

### What NOT to change

- `FailoverHandler` / `StepHandler` internals — VM-level parallelism stays
- `ChunkWaves` / `chunkSingleWave` — chunking logic unchanged
- `KubeCheckpointer` — retry budget stays at 8 (adequate for cross-DC lag without contention)
- `statusMu` — keep the mutex; it still protects in-memory status updates from the handler's internal goroutines calling `setGroupStatus` via callbacks (even though groups are sequential, handler internals may still mutate state from goroutines)
- `ExecuteFromWave` (resume path) — it already iterates waves sequentially and calls `executeWave` per wave; no change needed at that level

### Previous story learnings (10.5–10.7)

- Stories touching executor.go have had smooth reviews when changes are surgical
- Always run existing test suite first to confirm no pre-existing flakiness
- The `concurrencyHandler` in tests uses a barrier pattern that proves concurrency — this must be replaced, not just deleted

### UAT validation plan

After implementation, redeploy to etl6/etl7 and re-run the same planned migration (3-wave, maxConcurrentFailovers: 2). Wave 3 should produce 2 sequential groups that both checkpoint successfully → Result=Succeeded (not PartiallySucceeded).

### Review Findings

- [x] [Review][Patch] Cancelled wave is still marked complete [`pkg/engine/executor.go:441`] — `executeWave` breaks on `ctx.Err()` but still stamps wave `CompletionTime`, writes a wave checkpoint, and logs `"Wave execution completed"`, so a partially executed wave can be recorded as finished. **Fixed:** guarded completion-time/checkpoint/log behind `ctx.Err() == nil` in both `executeWave` and `executeRetryWave`.
- [x] [Review][Patch] Stale executor comments still describe intra-wave concurrency [`pkg/engine/executor.go:324`] — `ExecuteWaveHandler` and `ExecuteRetry` comments still say groups run concurrently within a wave, contradicting the new sequential execution model. **Fixed:** updated both comments to say "sequentially".
- [x] [Review][Patch] Checkpoint-ordering test does not verify checkpoint ordering [`pkg/engine/executor_test.go:405`] — `TestWaveExecutor_SequentialChunks_CheckpointOrdering` only checks call count and handler event order; `NoOpCheckpointer.GetCalls()` returns execution names only (`pkg/engine/checkpoint.go:158`), so AC8's ordered-checkpoint assertion is not actually covered. **Fixed:** added `TerminalCounts` tracking to `NoOpCheckpointer` and monotonic-ordering assertion (`[1, 2, 3, 3]`) to the test.
- [x] [Review][Defer] Filtered-wave execution uses slice index as status slot [`pkg/engine/executor.go:332`] — deferred, pre-existing. `ExecuteWaveHandler`/`ExecuteFromWave` rebuild filtered chunk slices, then `executeWave` passes the filtered-loop index into `setGroupStatus`/`getGroupVMNames`; if earlier groups are skipped, updates can land in the wrong `wave.Groups` slot.

## Dev Agent Record

### Implementation Plan

Surgical replacement of WaitGroup+goroutine fan-out in both `executeWave` and `executeRetryWave` with plain `for` loops. Added `ctx.Err()` checks between iterations for cancellation support. Kept `statusMu` (still needed for handler internal goroutines). Updated doc.go to reflect sequential chunk execution model. Replaced concurrency barrier test with timestamp-based sequential ordering test, added checkpoint ordering test, expanded retry ordering test to cover intra-wave sequentiality.

### Debug Log

No issues encountered during implementation. All tests passed on first run.

### Completion Notes

- `executeWave`: WaitGroup+goroutine fan-out → plain `for` loop with `ctx.Err()` check
- `executeRetryWave`: Same pattern applied
- `sync` import retained (needed for `sync.Mutex` on `statusMu` and `driverCacheMu`)
- `concurrencyHandler` test type → replaced with `sequentialOrderHandler` (timestamps)
- `sync/atomic` import removed from test file (no longer needed)
- doc.go: Wave executor, checkpoint, and checkpoint timing paragraphs updated
- executor.go file-level comment and WaveExecutor struct comment updated
- 4 tests pass: SequentialChunks, SequentialChunks_CheckpointOrdering, FailForward_MultipleGroups, RetryWaveOrdering

## File List

- `pkg/engine/executor.go` — modified (executeWave, executeRetryWave, file comment, struct comment)
- `pkg/engine/doc.go` — modified (Wave executor, Per-DRGroup checkpointing, Checkpoint timing)
- `pkg/engine/executor_test.go` — modified (renamed+rewritten TestWaveExecutor_SequentialChunks, added TestWaveExecutor_SequentialChunks_CheckpointOrdering, expanded TestWaveExecutor_RetryWaveOrdering, removed concurrencyHandler, added sequentialOrderHandler, removed sync/atomic import)

## Change Log

- 2026-05-11: Implemented Story 10.8 — Sequential Chunk Execution Within Waves. Replaced parallel goroutine fan-out with sequential for-loop in executeWave and executeRetryWave, preserving fail-forward semantics and VM-level parallelism. Updated doc.go and tests. All unit/integration tests pass, 0 lint issues.
