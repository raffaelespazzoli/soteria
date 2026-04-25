# Story 5.7: Driver Interface Simplification & Workflow Symmetry

Status: ready-for-dev

## Story

As a storage vendor engineer and DR operator,
I want the StorageProvider interface to contain only the methods the orchestrator actually uses, with no force-flag leaking implementation concerns into the engine,
So that driver implementations are simpler, the failover/failback code paths are truly symmetric, and the system is easier to reason about and test.

## Background — Design Correction

This story is a correction that addresses design insights identified during the Epic 5 implementation and party-mode design review:

1. **`SetTarget` is never called by the engine.** The `StorageProvider` interface defines `SetTarget`, but no engine handler invokes it. In a paired replication setup, when one site calls `SetSource`, the paired site implicitly becomes the target — the pairing is an admin precondition. `SetTarget` is dead interface surface area that storage vendor engineers are forced to implement for no reason.

2. **`Force` flags leak implementation concerns.** The `Force` field on `SetSourceOptions`, `SetTargetOptions`, and `StopReplicationOptions` tells the driver how to handle unreachable peers. This is the driver's internal concern — not the orchestrator's. The driver should always be resilient to unreachable peers. Removing Force eliminates the Options structs entirely (they become empty).

3. **Planned migration and disaster per-group paths diverge unnecessarily.** Currently, `FailoverHandler.ExecuteGroup` branches on `Config.GracefulShutdown`: planned skips `StopReplication` (done in Step 0), disaster calls `StopReplication(force=true)`. With Force removed and StopReplication being idempotent, both paths can use the same code: `StopReplication → StartVM`. In the planned case, Step 0 already moved volumes to NonReplicated, so the per-group `StopReplication` is an idempotent no-op.

4. **Step 0 should only stop VMs.** Currently `PreExecute` does three things: stop VMs, stop replication, and poll for sync. With the unified per-group path always calling `StopReplication`, the replication stop and sync wait in Step 0 are redundant. Step 0 becomes a single operation: stop all origin VMs.

5. **VM operations are fire-and-return, not poll.** Story 5.6 refactored VM lifecycle to event-driven watches. The reconciler yields after `StopVM`/`StartVM` and gets re-invoked by watch events. No polling loops in the handler.

## Acceptance Criteria

1. **AC1 — Remove `SetTarget` from `StorageProvider`:** Delete `SetTarget(ctx, id, opts)` from the `StorageProvider` interface in `pkg/drivers/interface.go`. Delete `SetTargetOptions` from `pkg/drivers/types.go`. Remove from fake driver, no-op driver, conformance suite, and all test stubs.

2. **AC2 — Remove `Force` from all Options structs:** Remove the `Force` field from `SetSourceOptions` and `StopReplicationOptions`. If the structs become empty, remove the structs entirely and change method signatures to not accept options (e.g., `StopReplication(ctx, id) error`). The driver is responsible for handling unreachable peers internally — this is not the orchestrator's concern.

3. **AC3 — Unified per-group failover path:** `FailoverHandler.ExecuteGroup` has a single code path for both planned and disaster: `StopReplication → StartVM`. No branching on `Config.GracefulShutdown` or `Config.Force`. `StopReplication` is idempotent — when called after Step 0 has already moved volumes to NonReplicated, it is a no-op.

4. **AC4 — Step 0 is VM-stop only:** `FailoverHandler.PreExecute` with `GracefulShutdown=true` only calls `StopVM` for all origin VMs. It does NOT call `StopReplication`, does NOT poll `GetReplicationStatus`, and does NOT wait for sync. The `waitForSync`, `resolvedVG` type, `syncPollInterval`, `syncTimeout`, and `defaultSyncPollInterval`/`defaultSyncTimeout` constants are deleted.

5. **AC5 — Simplified `FailoverConfig`:** `FailoverConfig` is reduced to `{ GracefulShutdown bool }`. The `Force` field is removed. The controller maps: `planned_migration → {GracefulShutdown: true}`, `disaster → {GracefulShutdown: false}`.

6. **AC6 — ReprotectHandler updated:** `ReprotectHandler.Execute` calls `StopReplication` and `SetSource` without options structs (or with empty structs if retained for future extensibility). The `Force: true` on `StopReplication` and `Force: false` on `SetSource` calls are removed.

7. **AC7 — Conformance suite updated:** The lifecycle test in `pkg/drivers/conformance/suite.go` is updated to remove `SetTarget` steps. The sequence becomes: `CreateVolumeGroup → SetSource → GetReplicationStatus(Source) → StopReplication → GetReplicationStatus(NonReplicated) → DeleteVolumeGroup`. Idempotency and error tests for `SetTarget` are removed. Context cancellation tests for `SetTarget` are removed.

8. **AC8 — Fake driver updated:** `pkg/drivers/fake/driver.go` removes `OnSetTarget`, `SetTarget`, and all `SetTargetOptions` references. `SetSource` and `StopReplication` call signatures updated (no options or empty options).

9. **AC9 — `doc.go` updated:** Both `pkg/engine/doc.go` and `pkg/drivers/fake/doc.go` are updated to reflect the simplified interface and unified failover path. No references to `SetTarget`, `Force`, or the old branching per-group logic.

10. **AC10 — All existing tests pass:** No regressions. Failover, reprotect, state machine, conformance, and integration tests all pass. Coverage for the engine package remains at or above 80%.

## Tasks / Subtasks

- [ ] Task 1: Remove `SetTarget` from the driver interface (AC: #1)
  - [ ] 1.1 In `pkg/drivers/interface.go`, delete the `SetTarget` method and its doc comment from `StorageProvider`
  - [ ] 1.2 In `pkg/drivers/types.go`, delete `SetTargetOptions` struct
  - [ ] 1.3 In `pkg/drivers/noop/driver.go`, delete `SetTarget` method
  - [ ] 1.4 In `pkg/drivers/fake/driver.go`, delete `OnSetTarget`, `SetTarget` methods
  - [ ] 1.5 In `pkg/drivers/fake/doc.go`, remove `SetTarget` references
  - [ ] 1.6 In `pkg/drivers/interface_test.go`, delete `SetTarget` from mock
  - [ ] 1.7 In `pkg/drivers/registry_test.go`, delete `SetTarget` from stub
  - [ ] 1.8 In `pkg/drivers/fake/driver_test.go`, delete all `SetTarget`-related tests
  - [ ] 1.9 Update interface doc comment to show 3-method role model: `SetSource`, `StopReplication`, `GetReplicationStatus` (volume roles: NonReplicated, Source)
  - [ ] 1.10 Run `make build` to verify all compile errors are resolved

- [ ] Task 2: Remove `Force` from Options structs (AC: #2)
  - [ ] 2.1 In `pkg/drivers/types.go`, delete `Force` field from `SetSourceOptions` — if struct is now empty, delete the struct and update `SetSource` signature to `SetSource(ctx, id) error`
  - [ ] 2.2 In `pkg/drivers/types.go`, delete `Force` field from `StopReplicationOptions` — if struct is now empty, delete the struct and update `StopReplication` signature to `StopReplication(ctx, id) error`
  - [ ] 2.3 Update all `StorageProvider` implementations (noop, fake) to match new signatures
  - [ ] 2.4 Update all conformance tests to match new signatures
  - [ ] 2.5 Update all engine code (`failover.go`, `reprotect.go`) to match new signatures
  - [ ] 2.6 Update all test code to match new signatures
  - [ ] 2.7 Run `make build` to verify all compile errors are resolved

- [ ] Task 3: Simplify `FailoverConfig` (AC: #5)
  - [ ] 3.1 In `pkg/engine/failover.go`, remove `Force` field from `FailoverConfig`
  - [ ] 3.2 In `pkg/controller/drexecution/reconciler.go`, update `resolveHandler` dispatch: `planned_migration → {GracefulShutdown: true}`, `disaster → {GracefulShutdown: false}`
  - [ ] 3.3 Update all `FailoverConfig` construction in tests

- [ ] Task 4: Unify `ExecuteGroup` (AC: #3)
  - [ ] 4.1 In `pkg/engine/failover.go`, rewrite `ExecuteGroup` to a single path: `StopReplication → StartVM` for all groups, no branching on `Config.GracefulShutdown`
  - [ ] 4.2 In `pkg/engine/failover.go`, rewrite `ExecuteGroupWithSteps` to match — single path with step recording
  - [ ] 4.3 Update tests in `pkg/engine/failover_test.go` — remove tests that assert different planned/disaster per-group behavior, replace with unified tests
  - [ ] 4.4 Verify the graceful per-group `StopReplication` is idempotent (no-op when already NonReplicated)

- [ ] Task 5: Simplify `PreExecute` to VM-stop only (AC: #4)
  - [ ] 5.1 In `pkg/engine/failover.go`, rewrite `PreExecute`: when `GracefulShutdown=true`, only call `StopVM` for all unique origin VMs, then return
  - [ ] 5.2 Delete `waitForSync` method, `resolvedVG` type, `isSynced` function
  - [ ] 5.3 Delete `syncPollInterval()`, `syncTimeout()` methods
  - [ ] 5.4 Delete `SyncPollInterval`, `SyncTimeout` fields from `FailoverHandler`
  - [ ] 5.5 Delete `defaultSyncPollInterval`, `defaultSyncTimeout` constants
  - [ ] 5.6 Delete `vgIDCache`, `cacheMu`, `initCacheLocked`, `resolveVolumeGroupID` from `FailoverHandler` (no longer needed in PreExecute — per-group resolution is in ExecuteGroup)
  - [ ] 5.7 Update tests — remove sync-wait tests, update PreExecute tests to verify VM-stop only

- [ ] Task 6: Update `ReprotectHandler` (AC: #6)
  - [ ] 6.1 In `pkg/engine/reprotect.go`, update `StopReplication` calls to match new signature (no options)
  - [ ] 6.2 In `pkg/engine/reprotect.go`, update `SetSource` calls to match new signature (no options)
  - [ ] 6.3 Update `pkg/engine/reprotect_test.go` to match new signatures
  - [ ] 6.4 Remove any assertions on `Force` parameter values in reprotect tests

- [ ] Task 7: Update conformance suite (AC: #7)
  - [ ] 7.1 In `pkg/drivers/conformance/suite.go`, remove `SetTarget` steps from lifecycle test
  - [ ] 7.2 Remove `SetTarget` idempotency tests
  - [ ] 7.3 Remove `SetTarget` context cancellation tests
  - [ ] 7.4 Remove `SetTarget` not-found error tests
  - [ ] 7.5 Update all remaining test calls to use new signatures (no options structs)
  - [ ] 7.6 Update `pkg/drivers/conformance/doc.go` lifecycle description

- [ ] Task 8: Update docs and verify (AC: #9, #10)
  - [ ] 8.1 Update `pkg/engine/doc.go` — remove SetTarget/Force references, document unified per-group path
  - [ ] 8.2 Update `pkg/drivers/fake/doc.go` — remove SetTarget examples
  - [ ] 8.3 Update `pkg/engine/roles.go` comment that references SetSource
  - [ ] 8.4 Run `make manifests` (no CRD changes expected)
  - [ ] 8.5 Run `make generate` (no deepcopy changes expected)
  - [ ] 8.6 Run `make test` — all tests pass
  - [ ] 8.7 Run `make lint-fix && make lint`
  - [ ] 8.8 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is a correction story that applies design insights from the Epic 5 party-mode review. It simplifies the `StorageProvider` interface and unifies the failover handler code paths. The change is backward-incompatible for driver implementations (pre-v1, this is expected).

### Symmetry Principle

The design enforces a strict symmetry rule:

- **FailingOver and FailingBack** use the same `FailoverHandler`, same code path, same operations. The only variable is `GracefulShutdown` (whether Step 0 runs). The per-group path is always `StopReplication → StartVM`.
- **Reprotecting and ReprotectingBack** use the same `ReprotectHandler`, same code path, same operations: `StopReplication → SetSource → health monitoring`.
- **Planned vs Disaster** differ only in whether Step 0 runs (StopVM). Per-group is identical.

### Updated Transition Table

| Transition | Planned Migration | Disaster |
|---|---|---|
| **FailingOver** | Step 0: `StopVM` all origin VMs. Per-group: `StopReplication` (idempotent no-op) → `StartVM` | Per-group: `StopReplication` → `StartVM` |
| **Reprotecting** | Storage-only: `StopReplication` → `SetSource` → health monitoring | Same (mode is always `reprotect`) |
| **FailingBack** | Identical to FailingOver (sites swapped) | Identical to FailingOver disaster (sites swapped) |
| **ReprotectingBack** | Identical to Reprotecting (direction swapped) | Same (mode is always `reprotect`) |

### Updated StorageProvider Role Model

Before (3 roles, 4 transitions):
```
NonReplicated → Source   (SetSource)
NonReplicated → Target   (SetTarget)  ← REMOVED
Source        → NonReplicated (StopReplication)
Target        → NonReplicated (StopReplication)
```

After (2 roles used by engine, 2 transitions):
```
NonReplicated → Source        (SetSource)
Source        → NonReplicated (StopReplication)
```

Note: Target role still exists in `drivers.ReplicationRole` for `GetReplicationStatus` reporting — the *other* site's driver may report its volumes as Target. But the engine never explicitly *sets* a volume to Target.

### Files to Modify

| File | Changes |
|------|---------|
| `pkg/drivers/interface.go` | Remove `SetTarget`, update role model doc |
| `pkg/drivers/types.go` | Remove `SetTargetOptions`, `Force` from `SetSourceOptions`/`StopReplicationOptions` |
| `pkg/drivers/noop/driver.go` | Remove `SetTarget`, update signatures |
| `pkg/drivers/fake/driver.go` | Remove `SetTarget`/`OnSetTarget`, update signatures |
| `pkg/drivers/fake/doc.go` | Remove `SetTarget` examples |
| `pkg/drivers/fake/driver_test.go` | Remove `SetTarget` tests, update signatures |
| `pkg/drivers/interface_test.go` | Remove `SetTarget` from mock |
| `pkg/drivers/registry_test.go` | Remove `SetTarget` from stub |
| `pkg/drivers/conformance/suite.go` | Remove `SetTarget` lifecycle/idempotency/error tests |
| `pkg/drivers/conformance/doc.go` | Update lifecycle description |
| `pkg/engine/failover.go` | Unified `ExecuteGroup`, simplified `PreExecute`, delete sync infra |
| `pkg/engine/failover_test.go` | Unified tests, remove planned/disaster branching tests |
| `pkg/engine/reprotect.go` | Update call signatures |
| `pkg/engine/reprotect_test.go` | Update call signatures, remove Force assertions |
| `pkg/engine/doc.go` | Update for simplified interface and unified path |
| `pkg/engine/roles.go` | Update comment |
| `pkg/engine/executor.go` | Update `GroupError` comment if needed |
| `pkg/engine/executor_test.go` | Update test data |
| `pkg/controller/drexecution/reconciler.go` | Simplified `FailoverConfig` dispatch |

### Build Commands

```bash
make manifests    # Verify no CRD changes needed
make generate     # Verify no deepcopy changes needed
make test         # All tests pass
make lint-fix     # Auto-fix
make lint         # Verify
make build        # Compile
```

### References

- [Source: Party-mode design review session — Epic 5 sprint]
- [Source: pkg/drivers/interface.go] — Current StorageProvider interface
- [Source: pkg/engine/failover.go] — Current FailoverHandler with branching paths
- [Source: pkg/engine/reprotect.go] — Current ReprotectHandler with Force flags
- [Source: _bmad-output/implementation-artifacts/4-1b-state-machine-symmetry-unified-failover-handler.md] — Symmetry design context
- [Source: _bmad-output/implementation-artifacts/5-6-event-driven-wave-gate-vm-readiness.md] — Event-driven VM lifecycle

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
