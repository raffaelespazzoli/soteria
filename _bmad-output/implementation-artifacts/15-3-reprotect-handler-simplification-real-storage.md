# Story 15.3: Reprotect Handler Simplification for Real Storage

Status: review

## Story

As a developer,
I want the reprotect handler to skip the `SetSource` call (which incorrectly promotes to primary) and instead verify the secondary state and call `ResyncVolume` only when a stale primary is detected,
so that reprotect works correctly with real Ceph where mirroring is automatic once roles are set.

## Acceptance Criteria

**AC1: Remove SetSource from reprotect Phase 1**
Given the reprotect handler in `pkg/engine/reprotect.go`
When Phase 1 executes
Then `SetSource` is NOT called
And Phase 1 is replaced with a state verification: call `GetReplicationStatus` on each VG to determine its current role

**AC2: State verification (idempotent)**
Given Phase 1 runs state verification
When the VG's replication status reports `RoleTarget` (secondary state — expected after planned failover)
Then verification passes and health monitoring begins
And no state mutations are performed (no driver write calls)

**AC3: Post-disaster reprotect handles stale primary**
Given a disaster failover occurred (source was unreachable)
When the old primary comes back online with its VR still reporting `RoleSource` (stale primary)
And reprotect is initiated
Then Phase 1 detects the stale primary state via `GetReplicationStatus`
And calls `ResyncVolume` on that VG to transition it to resyncing from the new primary
And does NOT wait for resync completion (reprotect is fire-and-forget for sync)
And health monitoring begins immediately after all VGs are processed

**AC4: Health monitoring unchanged**
Given Phase 1 completes (verification or resync kick-off)
When Phase 2 executes
Then health monitoring behavior is unchanged (poll `GetReplicationStatus`, report `Replicating` condition)
And execution succeeds once health monitoring is kicked off (does not wait for full sync)

**AC5: Backward compatibility with noop driver**
Given the noop driver
When reprotect runs
Then `GetReplicationStatus` returns `RoleSource` + `HealthHealthy` (noop always reports source/healthy for existing VGs)
And the handler treats `RoleSource` as stale-primary → calls `ResyncVolume` (noop ResyncVolume returns nil)
And health monitoring completes immediately (noop reports `HealthHealthy`)
And existing unit tests continue to pass after updating assertions

## Tasks / Subtasks

- [x] Task 1: Replace SetSource with state verification in Phase 1 (AC: 1, 2, 3)
  - [x] 1.1: Remove the `SetSource` call loop (lines 168-198 of `reprotect.go`)
  - [x] 1.2: Add new Phase 1 logic: for each VG, call `GetReplicationStatus` to read current role
  - [x] 1.3: If `RoleTarget` → VG is already in correct state, add to `successfulVGs` with a "Verified" step status
  - [x] 1.4: If `RoleSource` → stale primary detected, call `ResyncVolume` on that VG, add to `successfulVGs` with a "ResyncRequested" step status
  - [x] 1.5: If `GetReplicationStatus` returns error → mark VG as failed (same as current SetSource failure handling)
  - [x] 1.6: Update step name constants: rename `StepReprotectSetSource` to `StepReprotectStateVerification` (or add new constant)

- [x] Task 2: Update ReprotectHandler struct and constants (AC: 1)
  - [x] 2.1: Update the Tier 2 architecture comment at top of file to describe new Phase 1 logic
  - [x] 2.2: Rename or add step constants to reflect verification + resync semantics
  - [x] 2.3: Update `Execute` method godoc

- [x] Task 3: Update unit tests (AC: 1, 2, 3, 4, 5)
  - [x] 3.1: Update `TestReprotect_FullSuccess` — remove SetSource assertion, add GetReplicationStatus + ResyncVolume assertions (noop driver returns RoleSource → triggers ResyncVolume)
  - [x] 3.2: Add `TestReprotect_SecondaryState_SkipsResync` — fake driver returns RoleTarget, verify no ResyncVolume call, verify health monitoring proceeds
  - [x] 3.3: Add `TestReprotect_StalePrimary_CallsResyncVolume` — fake driver returns RoleSource, verify ResyncVolume called, verify health monitoring proceeds immediately
  - [x] 3.4: Update `TestReprotect_SetSourceFails_VGMarkedFailed` → rename to `TestReprotect_StatusCheckFails_VGMarkedFailed` — fake driver returns error on GetReplicationStatus
  - [x] 3.5: Update `TestReprotect_AllSetSourceFail_ExecutionFails` → rename to `TestReprotect_AllStatusCheckFail_ExecutionFails`
  - [x] 3.6: Update `TestReprotect_DriverCallsMade` — verify no SetSource call, verify GetReplicationStatus called, verify ResyncVolume called when RoleSource
  - [x] 3.7: Add `TestReprotect_MixedStates_SomeSecondary_SomePrimary` — mixed VG states, verify correct handling of each
  - [x] 3.8: Verify all existing passing tests still pass (health monitoring, checkpointing, context cancellation, step status)

- [x] Task 4: Update `pkg/engine/doc.go` (AC: 1)
  - [x] 4.1: Update the Re-protect handler section to describe new Phase 1 (state verification + conditional ResyncVolume)

- [x] Task 5: Verify and finalize
  - [x] 5.1: Run `make lint-fix` — fix any issues
  - [x] 5.2: Run `make test` — all unit + envtest tests pass
  - [x] 5.3: Verify Tier 1/2/3 doc compliance

### Review Findings

- [x] [Review][Patch] Reprotect should yield after requesting resync and resume on sync completion [pkg/engine/reprotect.go:254] — Story 15.3 requires fire-and-forget reprotect semantics. After `ResyncVolume`, the controller should set status to resyncing, yield, and rely on a later wake-up when VR/VGR status shows sync completion instead of blocking inside synchronous health polling.
  - **Fixed**: Execute() now returns `ErrResyncRequested` after Phase 1 when any VG went through ResyncVolume. The reconciler sets `ResyncPending` condition and yields. On next reconcile (VR/VGR watch event), idempotent replay sees `RoleTarget` and proceeds to Phase 2.

## Dev Notes

### Why SetSource Is Wrong with Real Ceph

The current Phase 1 calls `SetSource` on each VG, which patches the VR's `spec.replicationState` to `primary`. With real Ceph RBD mirroring:

- After **planned failover**: the old primary (east) was already demoted to secondary in Step 0 via `StopReplication`. The new primary (west) was promoted via `SetSource` in per-group execution. Reprotect runs on east. Calling `SetSource(east)` would promote east to primary — creating a **dual-primary** situation where both sites think they're the replication source. This corrupts data.

- After **disaster failover**: the old primary (east) was unreachable, so Step 0 was skipped. The new primary (west) was force-promoted via `SetSource`. When east comes back, its VR still reports `Primary` state (it never received a demotion). Calling `SetSource(east)` is a no-op (already primary) — it does NOT fix the situation. What's needed is `ResyncVolume(east)` which transitions east to resync from west (the actual current primary).

### Implementation Pattern — New Phase 1 Logic

Replace the `SetSource` loop with:

```go
for _, vg := range input.VolumeGroups {
    if ctx.Err() != nil {
        return nil, ctx.Err()
    }

    status, err := vg.Driver.GetReplicationStatus(ctx, vg.VGID)
    now := metav1.Now()
    if err != nil {
        logger.Info("Could not verify replication state",
            "vg", vg.Info.Name, "error", err)
        steps = append(steps, soteriav1alpha1.StepStatus{
            Name:      StepReprotectStateVerification,
            Status:    reprotectStatusFailed,
            Message:   fmt.Sprintf("State verification failed for %s: %v", vg.Info.Name, err),
            Timestamp: &now,
        })
        failedVGNames = append(failedVGNames, vg.Info.Name)
        continue
    }

    switch status.Role {
    case drivers.RoleTarget:
        // Expected after planned failover — VR is already secondary.
        logger.Info("VG confirmed in secondary state", "vg", vg.Info.Name)
        steps = append(steps, soteriav1alpha1.StepStatus{
            Name:      StepReprotectStateVerification,
            Status:    reprotectStatusSucceeded,
            Message:   fmt.Sprintf("Verified %s in secondary state", vg.Info.Name),
            Timestamp: &now,
        })
        successfulVGs = append(successfulVGs, vg)

    case drivers.RoleSource:
        // Post-disaster: stale primary. Kick off resync to pull from new primary.
        logger.Info("Stale primary detected, requesting resync",
            "vg", vg.Info.Name)
        resyncErr := vg.Driver.ResyncVolume(ctx, vg.VGID)
        if resyncErr != nil {
            logger.Info("ResyncVolume failed for stale primary",
                "vg", vg.Info.Name, "error", resyncErr)
            steps = append(steps, soteriav1alpha1.StepStatus{
                Name:      StepReprotectStateVerification,
                Status:    reprotectStatusFailed,
                Message:   fmt.Sprintf("ResyncVolume failed for %s: %v", vg.Info.Name, resyncErr),
                Timestamp: &now,
            })
            failedVGNames = append(failedVGNames, vg.Info.Name)
            continue
        }
        steps = append(steps, soteriav1alpha1.StepStatus{
            Name:      StepReprotectStateVerification,
            Status:    reprotectStatusSucceeded,
            Message:   fmt.Sprintf("Resync requested for stale primary %s", vg.Info.Name),
            Timestamp: &now,
        })
        successfulVGs = append(successfulVGs, vg)

    default:
        // NonReplicated or Unknown — unexpected, treat as verification failure.
        logger.Info("Unexpected replication role during reprotect",
            "vg", vg.Info.Name, "role", status.Role)
        steps = append(steps, soteriav1alpha1.StepStatus{
            Name:      StepReprotectStateVerification,
            Status:    reprotectStatusFailed,
            Message:   fmt.Sprintf("Unexpected role %s for %s", status.Role, vg.Info.Name),
            Timestamp: &now,
        })
        failedVGNames = append(failedVGNames, vg.Info.Name)
    }

    h.writeCheckpoint(ctx, input.Execution)
}
```

### Noop Driver Behavior

The noop driver's `GetReplicationStatus` returns `RoleSource` + `HealthHealthy` for any existing VG (the noop driver tracks `VolumeRole` and always creates VGs as Source). Since `RoleSource` triggers the resync path, the noop `ResyncVolume` will be called (returns nil immediately). This is correct behavior — the noop driver doesn't distinguish between planned and disaster paths, and ResyncVolume is a no-op.

Existing tests use the `fake` driver, not `noop`. The `fake` driver's `GetReplicationStatus` returns whatever is programmed via `OnGetReplicationStatus().ReturnResult(...)`. Tests must be updated to program the expected role.

### Fake Driver Test Programming

For the updated tests, program the fake driver to return the appropriate state:

```go
// Simulate post-planned-failover (VR is secondary):
d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
    ReplicationStatus: &drivers.ReplicationStatus{
        Role:   drivers.RoleTarget,
        Health: drivers.HealthHealthy,
    },
})

// Simulate post-disaster (VR is stale primary):
d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
    ReplicationStatus: &drivers.ReplicationStatus{
        Role:   drivers.RoleSource,
        Health: drivers.HealthHealthy,
    },
})
```

### Constants Update

```go
const (
    StepReprotectStateVerification = "StateVerification"  // Replaces StepReprotectSetSource
    StepReprotectHealthMonitoring  = "HealthMonitoring"   // Unchanged
)
```

Keep `StepReprotectSetSource` as deprecated (unexported or removed) to avoid breaking external references. Since it's in the `engine` package (internal), safe to rename directly.

### Tier 2 Architecture Comment Update

Replace the current file header comment's Phase 1 description:

**Current:** "Phase 1 — Role setup: for each VG, call SetSource to set the local VR to primary (writable)."

**New:** "Phase 1 — State verification: for each VG, check replication role via GetReplicationStatus. If already secondary (expected after planned failover), proceed. If stale primary (post-disaster), call ResyncVolume to initiate sync from the new primary. No blocking wait — health monitoring handles progress tracking."

### File Inventory

| File | Action | Lines Changed (est.) |
|------|--------|---------------------|
| `pkg/engine/reprotect.go` | Replace SetSource loop with state verification + conditional ResyncVolume | ~50 |
| `pkg/engine/reprotect_test.go` | Update existing tests, add new state-based tests | ~80 |
| `pkg/engine/doc.go` | Update Re-protect handler description | ~10 |

**Total: ~3 modified files, ~140 lines changed**

### Key Constraints

- **Do NOT add ResyncVolume to the `ReprotectHandler` struct** — it's accessed through the `VolumeGroupEntry.Driver` (the same `StorageProvider` interface already has `ResyncVolume` from Story 15.1)
- **Do NOT wait for resync completion in Phase 1** — reprotect is fire-and-forget for the sync. Health monitoring (Phase 2) will eventually show progress as the VR transitions through Syncing → Healthy
- **Do NOT modify `pkg/drivers/` files** — all driver implementations are unchanged. This story only modifies the engine layer
- **Do NOT change the `ReprotectResult` struct** — the same result classification applies (all failed → Failed, some failed → PartiallySucceeded, all succeeded → check health timeout)
- **Do NOT remove the `successfulVGs` / `failedVGNames` tracking** — same fail-forward semantics apply, just the criteria for success/failure changes from "SetSource succeeded" to "state verification succeeded"
- **Phase 2 (health monitoring) is completely unchanged** — no modifications to `monitorHealth`, `countHealthy`, or `updateHealthConditions`
- **Checkpoint writes per-VG remain** — same `h.writeCheckpoint(ctx, input.Execution)` after each VG in Phase 1
- **The `metrics.ReprotectVGSetupDuration` metric remains** — rename reference to "state verification" in docs only, metric name stays (avoid breaking dashboards)

### Testing Strategy

**Updated tests (fake driver programmable responses):**

| Test | Fake GetReplicationStatus Response | Expected Behavior |
|------|-------------------------------------|-------------------|
| `TestReprotect_FullSuccess` (noop-like) | `RoleSource` + `HealthHealthy` | ResyncVolume called, health monitoring passes |
| `TestReprotect_SecondaryState_SkipsResync` | `RoleTarget` + `HealthHealthy` | No ResyncVolume, health monitoring passes |
| `TestReprotect_StalePrimary_CallsResyncVolume` | `RoleSource` + `HealthSyncing` | ResyncVolume called, health monitoring reports syncing then healthy |
| `TestReprotect_StatusCheckFails_VGMarkedFailed` | Error from GetReplicationStatus | VG marked failed, PartiallySucceeded |
| `TestReprotect_AllStatusCheckFail_ExecutionFails` | All return error | Failed |
| `TestReprotect_MixedStates` | Mix of RoleTarget and RoleSource | Correct path for each |

**Assertions to update:**
- Remove `d.Called("SetSource")` / `d.CallCount("SetSource")` assertions
- Add `d.Called("GetReplicationStatus")` assertions
- Add conditional `d.Called("ResyncVolume")` assertions based on programmed role

### Previous Story Intelligence

Story 15.1 added `ResyncVolume` to the StorageProvider interface:
- `pkg/drivers/interface.go` — 7-method interface (including ResyncVolume)
- `pkg/drivers/fake/driver.go` — `OnResyncVolume(vgID)` + call recording
- `pkg/drivers/noop/driver.go` — returns nil for existing VGs
- Calling pattern: `vg.Driver.ResyncVolume(ctx, vg.VGID)` returns `error`

Story 15.2 established the resync guard pattern for planned failover:
- ResyncVolume is called on **target** (secondary) VGs during Step 0 to pull data from primary
- After resync completes (event-driven), StopReplication demotes the source
- Story 15.3's use of ResyncVolume is different: it's called on a **stale primary** (post-disaster) to transition it to resyncing from the new primary

### Ceph Behavior Reference

When `ResyncVolume` is called on a VR that is in `Primary` state (stale primary after disaster):
1. CSI Addons sidecar transitions the VR to `Resync` state
2. Ceph demotes it from primary and starts pulling data from the peer (now the actual primary)
3. VR status transitions: `Primary` → `Secondary` (with `Completed=False` during resync)
4. Eventually: `Secondary` + `Completed=True` (fully synced)
5. `GetReplicationStatus` will report: `RoleTarget` + `HealthSyncing` during resync, then `RoleTarget` + `HealthHealthy` when complete

### Project Structure Notes

- All modifications are in `pkg/engine/` — no new packages, no new files
- The `ResyncVolume` method is accessed through the existing `drivers.StorageProvider` interface on `VolumeGroupEntry.Driver`
- No RBAC changes needed (engine layer doesn't have RBAC markers — those are on controllers)
- No `make manifests generate` needed (no CRD or type changes)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.3]
- [Source: pkg/engine/reprotect.go — current SetSource-based Phase 1 (lines 160-198)]
- [Source: pkg/engine/reprotect_test.go — existing test patterns]
- [Source: pkg/engine/doc.go — Re-protect handler architecture description (lines 179-196)]
- [Source: pkg/drivers/interface.go — StorageProvider.ResyncVolume, GetReplicationStatus]
- [Source: pkg/drivers/types.go — RoleSource, RoleTarget, RoleNonReplicated constants]
- [Source: pkg/drivers/fake/driver.go — OnResyncVolume, OnGetReplicationStatus]
- [Source: _bmad-output/implementation-artifacts/15-1-resyncvolume-driver-method-csi-extension.md — ResyncVolume implementation]
- [Source: _bmad-output/implementation-artifacts/15-2-planned-failover-resync-guard-event-driven.md — resync pattern context]
- [Source: _bmad-output/project-context.md — StorageProvider Driver Framework, unified handler model]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (via Cursor)

### Debug Log References

- `TestStateTableInvariant_FullCycle` failed initially because the shared fake driver was not programmed with `Role` in `GetReplicationStatus` responses for the reprotect phases. Fixed by adding `Role: drivers.RoleSource` for Phase 1 state verification and `Role: drivers.RoleTarget` for Phase 2 health monitoring. Cumulative count assertions updated: reprotect now adds ResyncVolume calls instead of SetSource calls.

### Completion Notes List

- Replaced `SetSource` call in reprotect Phase 1 with state verification via `GetReplicationStatus` + conditional `ResyncVolume` for stale primaries
- Renamed `StepReprotectSetSource` constant to `StepReprotectStateVerification`
- Updated Tier 2 architecture comment, Execute method godoc, Result method godoc
- Updated doc.go re-protect handler section to describe new Phase 1 logic
- Updated all existing reprotect tests to program `GetReplicationStatus` with Role field for Phase 1 verification and Phase 2 health monitoring
- Added 3 new tests: `TestReprotect_SecondaryState_SkipsResync`, `TestReprotect_StalePrimary_CallsResyncVolume`, `TestReprotect_MixedStates_SomeSecondary_SomePrimary`
- Updated `TestStateTableInvariant_FullCycle` in failover_test.go for new reprotect behavior
- All 17 reprotect tests pass, full test suite passes with 0 regressions, 0 lint issues in engine package

### File List

- `pkg/engine/reprotect.go` — Modified: replaced SetSource loop with GetReplicationStatus + ResyncVolume state verification, renamed constant, updated comments and godoc
- `pkg/engine/reprotect_test.go` — Modified: updated all existing reprotect tests, added 3 new tests (SecondaryState_SkipsResync, StalePrimary_CallsResyncVolume, MixedStates)
- `pkg/engine/failover_test.go` — Modified: updated TestStateTableInvariant_FullCycle runReprotect helper and cumulative count assertions
- `pkg/engine/doc.go` — Modified: updated re-protect handler architecture description
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — Modified: story status ready-for-dev → in-progress → review
- `_bmad-output/implementation-artifacts/15-3-reprotect-handler-simplification-real-storage.md` — Modified: task checkboxes, Dev Agent Record, File List, Change Log, Status

### Change Log

- 2026-07-03: Implemented Story 15.3 — Reprotect handler simplified for real storage. Removed SetSource from Phase 1, replaced with GetReplicationStatus state verification + conditional ResyncVolume for stale primaries. Updated all tests, added 3 new tests. All unit tests pass, 0 lint issues.
