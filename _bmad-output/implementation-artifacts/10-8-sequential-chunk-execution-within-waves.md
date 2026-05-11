# Story 10.8: Sequential Chunk Execution Within Waves

Status: ready-for-dev

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

- [ ] Task 1: Serialize `executeWave` (AC: #1, #2, #6)
  - [ ] 1.1 In `pkg/engine/executor.go`, replace the `sync.WaitGroup` + goroutine fan-out in `executeWave` (lines 436-444) with a plain `for` loop calling `e.executeGroup` directly
  - [ ] 1.2 Add `ctx.Err()` check between iterations to respect context cancellation
  - [ ] 1.3 Remove `"sync"` import if no longer used in this file (check other usages of `sync.WaitGroup` and `sync.Mutex` — `statusMu` still uses `sync.Mutex`)

- [ ] Task 2: Serialize `executeRetryWave` (AC: #5, #6)
  - [ ] 2.1 In `pkg/engine/executor.go`, replace the `sync.WaitGroup` + goroutine fan-out in `executeRetryWave` (lines 1367-1375) with a plain `for` loop calling `e.executeRetryGroup` directly
  - [ ] 2.2 Add `ctx.Err()` check between retry group iterations

- [ ] Task 3: Update doc.go (AC: #7)
  - [ ] 3.1 Update the "Wave executor" paragraph: "concurrent DRGroups within each wave" → "DRGroup chunks within each wave sequentially, with VM-level parallelism within each chunk"
  - [ ] 3.2 Update the "Per-DRGroup checkpointing" paragraph: remove/rephrase "Concurrent DRPlan executions use separate DRExecution resources and separate checkpoint write paths — no shared mutex between executions" (the statusMu concern is reduced; keep the note about separate executions being independent)
  - [ ] 3.3 Update "Checkpoint timing guarantee" — the "at most one in-flight DRGroup" guarantee is now automatic (only one chunk runs at a time)

- [ ] Task 4: Update tests (AC: #8)
  - [ ] 4.1 Rename `TestWaveExecutor_ConcurrentDRGroups` to `TestWaveExecutor_SequentialChunks` and replace barrier-based concurrency assertion with ordering assertion. Use a handler that records start/completion timestamps per group and assert group-0.CompletionTime <= group-1.StartTime
  - [ ] 4.2 Add `TestWaveExecutor_SequentialChunks_CheckpointOrdering` — 3 VMs with maxConcurrentFailovers=1 (3 chunks), verify NoOpCheckpointer.GetCalls() returns `["wave-1-group-0", "wave-1-group-1", "wave-1-group-2", "wave-0"]` in that exact order
  - [ ] 4.3 Verify `TestWaveExecutor_FailForward_MultipleGroups` still passes (sequential fail-forward)
  - [ ] 4.4 Add ordering assertion to retry test: verify sequential retry execution within a wave

- [ ] Task 5: Verify all tests pass (AC: #9)
  - [ ] 5.1 Run `make test` — all unit tests pass
  - [ ] 5.2 Run `make lint` — zero lint issues
  - [ ] 5.3 Run `make integration` (if applicable) — no regressions

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
