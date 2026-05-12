# Story 10.9: Merge persistStatus and Checkpoint Into Single Write

Status: ready-for-dev

## Story

As a DR operator running planned migrations across a multi-DC ScyllaDB-backed environment,
I want each DRGroup completion to produce a single atomic status update (instead of two back-to-back writes),
so that checkpoint retries caused by informer-cache staleness between `persistStatus` and `writeCheckpoint` are eliminated.

## Background

### Problem Statement (UAT-10.003)

Story 10.8 serialized chunk execution within waves, eliminating inter-chunk resource-version conflicts. However, UAT Run 3 still shows 3–5 checkpoint retries per group on every planned migration. The retries succeed within the 6-attempt budget, but they add latency and log noise on every transition.

### Root Cause

Within `executeGroup`, each DRGroup completion produces **two consecutive status writes**:

1. `setGroupStatus` → calls `persistStatus` → `Get` (from cache) → `Status().Update` (to ScyllaDB)
2. `writeCheckpoint` → calls `KubeCheckpointer.WriteCheckpoint` → `Get` (from cache) → `Status().Update` (to ScyllaDB)

Both `Get` calls use the controller-runtime **cached client** (`mgr.GetClient()`). After write #1 advances the resourceVersion in ScyllaDB (e.g., rv=5→rv=6), write #2's `Get` reads from the informer cache, which hasn't yet received the CDC update. It reads stale rv=5 and the `Status().Update` is rejected with "object has been modified." The exponential backoff retries until the cache catches up.

### Current Code Flow (per group completion)

```
executeGroup:
  setGroupStatus(status{Result: Completed})
    └─ persistStatus  ──► Get(cache:rv=5) ──► Status().Update(rv=5→6)  ✓
  writeCheckpoint
    └─ KubeCheckpointer.WriteCheckpoint
       └─ Get(cache:rv=5 STALE) ──► Status().Update(rv=5) ──► CONFLICT
          retry → Get(cache:rv=6) ──► Status().Update(rv=6→7) ✓
```

### Target Code Flow (single write)

```
executeGroup:
  persistStatusWithCheckpoint(status{Result: Completed})
    └─ Get(cache) ──► merge conditions ──► Status().Update  ✓
    └─ on conflict: Get(cache) ──► merge ──► Status().Update  ✓
       on exhaustion: mark group Failed + emit event
```

## Acceptance Criteria

1. **AC1 — Single write per group completion:** The success path in `executeGroup` produces exactly one `Status().Update` call per group (merging what was previously `setGroupStatus` + `writeCheckpoint`). The separate `persistStatus` → `writeCheckpoint` sequence is eliminated for group completions.

2. **AC2 — Condition merging preserved:** The merged write path merges conditions from the fetched DRExecution (same as current `persistStatus` behavior): conditions present in the fresh `Get` but absent from the in-memory copy are carried forward. This ensures conditions set by other controllers (e.g., `Step0Complete`) survive the status update.

3. **AC3 — Checkpoint failure semantics preserved:** If the merged write exhausts all retries, the group is marked `Failed` with error `"checkpoint write failed after retries"` (same as current `writeCheckpoint` failure path). Fail-forward continues to subsequent chunks.

4. **AC4 — Exponential backoff with context:** The merged write uses `ExponentialBackoffWithContext` (matching `KubeCheckpointer`'s retry strategy) rather than `RetryOnConflict` (which uses a simpler backoff). This is because ScyllaDB's CDC propagation delay requires longer inter-retry gaps than standard etcd.

5. **AC5 — Failure-path writes unchanged:** When a group **fails** (handler error, driver resolution error), the existing two-write pattern (`setGroupStatus` with `Result=Failed` + `writeCheckpoint`) can also be merged. The failure-path checkpoint is optional (group is already Failed), so exhaustion does not double-mark the group.

6. **AC6 — InProgress status write unchanged:** The initial `setGroupStatus` call at the start of `executeGroup` (setting `Result=InProgress`) does NOT include a checkpoint and continues to use `persistStatus` alone. Only the terminal status writes (Completed, Failed) are merged with the checkpoint.

7. **AC7 — Wave-level checkpoint unchanged:** The `writeCheckpoint` call at the end of `executeWave` (wave completion timestamp) is left as-is. Only per-group completion writes are merged.

8. **AC8 — Prometheus metrics preserved:** `soteria_checkpoint_writes_total`, `soteria_checkpoint_write_duration_seconds`, and `soteria_checkpoint_retries_total` continue to be recorded. The merged write path emits the same metric values as the separate checkpoint path.

9. **AC9 — Checkpointer interface unchanged:** The `Checkpointer` interface and `KubeCheckpointer` type remain available for wave-level checkpoints. The `NoOpCheckpointer` continues to work for testing. No Checkpointer API changes.

10. **AC10 — Test updates:**
    - Existing `TestWaveExecutor_SequentialChunks_CheckpointOrdering` — update expected checkpoint call count (wave-level checkpoints only, no per-group checkpoint calls). Or if using `NoOpCheckpointer` as a callback, adjust expectations to reflect the new single-write path.
    - New test: `TestWaveExecutor_MergedWriteConditionMerge` — verify that conditions set by another controller (e.g., mock a Step0Complete condition) survive the merged status+checkpoint write.
    - New test: `TestWaveExecutor_MergedWriteExhaustion` — verify that when the merged write exhausts retries, the group is marked Failed (not Completed) and execution continues fail-forward.
    - Existing checkpoint-related tests in `executor_test.go` updated to reflect new write pattern.

11. **AC11 — doc.go updated:** Update the "Per-DRGroup checkpointing" paragraph in `pkg/engine/doc.go` to describe the merged write: status update and checkpoint are a single atomic `Status().Update` for group completions, eliminating the cache-staleness retry window.

## Tasks / Subtasks

- [ ] Task 1: Create `persistStatusAndCheckpoint` method on WaveExecutor (AC: #1, #2, #3, #4, #8)
  - [ ] 1.1 New method `persistStatusAndCheckpoint(ctx, exec, groupName)` that:
    - Takes `statusMu` lock to snapshot the in-memory status
    - Uses `ExponentialBackoffWithContext` with ScyllaRetry-derived backoff
    - Inside retry loop: `Get` → `mergeConditions` → `Status().Update`
    - Records checkpoint Prometheus metrics (writes total, duration, retries)
    - On success: returns nil
    - On exhaustion: returns `ErrCheckpointFailed`
  - [ ] 1.2 Use the same `ScyllaRetry`-derived backoff config that `KubeCheckpointer` uses (200ms base, 2x factor, 0.3 jitter, 10s cap, 8 steps)

- [ ] Task 2: Refactor `executeGroup` success path (AC: #1, #3, #6)
  - [ ] 2.1 Replace the `setGroupStatus` + `writeCheckpoint` pair at `executor.go:545-565` with a single call: update in-memory status under `statusMu`, then call `persistStatusAndCheckpoint`
  - [ ] 2.2 On `ErrCheckpointFailed`, mark group Failed (same as current behavior)
  - [ ] 2.3 Keep the initial `setGroupStatus` call (InProgress) unchanged — it uses `persistStatus` only

- [ ] Task 3: Refactor `executeGroup` failure paths (AC: #5)
  - [ ] 3.1 Replace the `setGroupStatus` + `writeCheckpoint` pair for handler-error path (`executor.go:529-539`) with merged write
  - [ ] 3.2 Replace the `setGroupStatus` + `writeCheckpoint` pair for driver-resolution-error path (`executor.go:488-497`) with merged write
  - [ ] 3.3 On failure paths, checkpoint exhaustion does not change the already-Failed result

- [ ] Task 4: Verify wave-level checkpoint is unchanged (AC: #7)
  - [ ] 4.1 Confirm `writeCheckpoint` call at `executor.go:453` (wave completion) still calls `Checkpointer.WriteCheckpoint`
  - [ ] 4.2 Confirm `executeRetryWave` wave-level checkpoint is unchanged

- [ ] Task 5: Update doc.go (AC: #11)
  - [ ] 5.1 Update "Per-DRGroup checkpointing" paragraph to describe merged write
  - [ ] 5.2 Update "Checkpoint timing guarantee" paragraph — the race window between two writes is eliminated

- [ ] Task 6: Update tests (AC: #10)
  - [ ] 6.1 Update `TestWaveExecutor_SequentialChunks_CheckpointOrdering` — adjust checkpoint call expectations (per-group completions no longer call `Checkpointer.WriteCheckpoint`; only wave-level calls remain)
  - [ ] 6.2 Add `TestWaveExecutor_MergedWriteConditionMerge` — inject a condition on the DRExecution between `Get` and `Update` to verify `mergeConditions` preserves it
  - [ ] 6.3 Add `TestWaveExecutor_MergedWriteExhaustion` — configure a client that always returns conflict, verify group is marked Failed and execution continues
  - [ ] 6.4 Update any checkpoint-count assertions in existing tests
  - [ ] 6.5 Run `make test` — all unit tests pass
  - [ ] 6.6 Run `make lint` — zero lint issues

## Dev Notes

### Key Locations

| File | Function/Section | Change |
|------|------------------|--------|
| `pkg/engine/executor.go:1076-1098` | `persistStatus` | Reference — condition merge logic to reuse |
| `pkg/engine/executor.go:1115-1128` | `setGroupStatus` | Will be partially replaced for terminal writes |
| `pkg/engine/executor.go:568-593` | `writeCheckpoint` | Will no longer be called for per-group completions |
| `pkg/engine/executor.go:458-566` | `executeGroup` | Main refactoring target — 3 call sites |
| `pkg/engine/checkpoint.go:76-130` | `KubeCheckpointer.WriteCheckpoint` | Reference for backoff config and metrics; interface unchanged |
| `pkg/engine/doc.go:126-142` | Per-DRGroup checkpointing paragraph | Update description |
| `pkg/engine/executor_test.go` | Multiple checkpoint tests | Update expectations |

### What NOT to Change

- `Checkpointer` interface — keep for wave-level checkpoints and testing
- `KubeCheckpointer` — still used for wave-level `writeCheckpoint` calls
- `NoOpCheckpointer` — still used for test checkpoint call recording
- `persistStatus` — keep for InProgress status writes (no checkpoint needed)
- `setGroupStatus` — keep for InProgress status writes
- `executeRetryWave` / `executeRetryGroup` — apply the same merged pattern if they follow the same two-write sequence; otherwise leave unchanged
- `statusMu` — still needed for in-memory status serialization

### Design Decisions

1. **Why not use an uncached client for `Get`?** The controller-runtime cached client is the standard pattern. Introducing a direct (uncached) client would add wiring complexity and diverge from the project's architecture. The root cause is the two-write pattern, not the cached client.

2. **Why `ExponentialBackoffWithContext` over `RetryOnConflict`?** ScyllaDB's CDC propagation to informer caches can take 200–500ms. The standard `RetryOnConflict` uses client-go's default backoff (10ms base, 5 steps, factor 1.0) which is tuned for etcd. The ScyllaDB-tuned backoff (200ms base, 2x factor, 0.3 jitter) gives the cache time to converge.

3. **Why keep wave-level checkpoints separate?** Wave-level checkpoints record completion timestamps only. They don't produce the same cache-staleness problem because they follow the last group checkpoint (which has already updated the resourceVersion). The wave `writeCheckpoint` typically succeeds on the first attempt.

### Previous Story Learnings (10.8)

- Surgical changes to `executor.go` review cleanly when the change is focused
- Always verify that existing test assertions on checkpoint call counts are updated
- The `NoOpCheckpointer.GetCalls()` and `GetTerminalCounts()` are the primary test hooks for checkpoint verification
- Run full test suite before and after to confirm no regressions

### Retry Path Considerations

Check `executeRetryGroup` for the same two-write pattern. If it also calls `setGroupStatus` + `writeCheckpoint` back-to-back, apply the same merge. The retry path in `ExecuteRetry` / `executeRetryWave` / `executeRetryGroup` follows similar patterns to `executeGroup`.

### Project Structure Notes

- All changes confined to `pkg/engine/` — no API, CRD, or controller changes
- No `make manifests` or `make generate` needed (no type changes)
- `make lint-fix && make test` is sufficient

### References

- [Source: UAT-10.003 analysis in user-acceptance-test-epic-10.md]
- [Source: pkg/engine/executor.go — setGroupStatus, persistStatus, writeCheckpoint, executeGroup]
- [Source: pkg/engine/checkpoint.go — KubeCheckpointer.WriteCheckpoint, ScyllaRetry backoff config]
- [Source: pkg/engine/doc.go — Per-DRGroup checkpointing paragraph]
- [Source: Story 10.8 — 10-8-sequential-chunk-execution-within-waves.md]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
