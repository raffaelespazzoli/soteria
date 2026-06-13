# Story 13.6: DRExecution Mutates VR/VGR ReplicationState During Transitions

Status: done

## Story

As a platform engineer,
I want the DRExecution controller to change `spec.replicationState` on VR/VGR objects during DR transitions,
So that the replication direction follows the VM migration: `primary` on the site becoming active, `secondary` on the site becoming passive.

## Background

### Context Within Epic 13

Epic 13 establishes DRPlan as the lifecycle owner of VR/VGR objects. Earlier stories in this epic handle:
- **13.1:** DRExecution OwnerReference for cascade delete
- **13.2:** DRPlan createOrUpdate VR/VGR with site-aware state (includes StopReplication always setting `secondary`, removing `flipReplicationState` and the `resync` case from the noop controller)
- **13.3:** Dual finalizers on VR/VGR objects
- **13.4:** DRPlan watches VR/VGR for reactive replication health
- **13.5:** Remove CreateVolumeGroup from engine/reprotect/health paths (resolve paths use `GetVolumeGroup` instead)

**This story (13.6)** restructures the failover handler to use the correct driver calls for the new StopReplication=secondary semantics. After 13.2 removes the flip, the current per-group path (`StopReplication → StartVM`) would leave the target volume read-only (secondary = VMs can't write). This story changes it to `SetSource → StartVM` and adds `StopReplication` to Step0 for planned migration.

### Replication State Model (Post-13.2)

After Story 13.2 lands:
- **`StopReplication`** always sets `spec.replicationState = secondary` (read-only target — no longer a flip; `flipReplicationState` helper is removed)
- **`SetSource`** always sets `spec.replicationState = primary` (writable source — unchanged from 12.4)
- **`resync`** state is eliminated — only `primary`/`secondary` exist

The semantic model:
- `SetSource` = **promote** — "this volume becomes the writable source, VMs run here"
- `StopReplication` = **demote** — "this volume becomes the read-only target, VMs don't run here"

### State Table Invariant

At every rest state, the site where VMs are running has VR/VGR with `spec.replicationState = primary` (writable), and the other site has `secondary` (read-only):

| Rest State | Active Site VR/VGR | Passive Site VR/VGR |
|---|---|---|
| SteadyState | `primary` (A) | `secondary` (B) |
| FailedOver | `primary` (B) | `secondary` (A) |
| DRedSteadyState | `primary` (B) | `secondary` (A) |
| FailedBack | `primary` (A) | `secondary` (B) |

### Transition Flow Details

**Failover (planned_migration) — SteadyState → FailedOver:**
1. Step 0 (source/primary site A): `StopVM` + `StopReplication` → A's VR/VGR set to `secondary` (demoted, read-only)
2. Per-group on Owner (target/secondary site B): `SetSource` → B's VR/VGR set to `primary` (promoted, writable)
3. Per-group: `StartVM` on target site B
4. **Result:** B = `primary` (VMs running), A = `secondary`

**Failover (disaster) — SteadyState → FailedOver:**
1. Step 0: NO-OP (source site unreachable) — A's VR/VGR stays `primary` (stale until recovery)
2. Per-group on Owner (target site B): `SetSource` → B's VR/VGR set to `primary` (promoted, writable)
3. Per-group: `StartVM` on target site B
4. **Result:** B = `primary` (VMs running), A = `primary` (stale — DRPlan reconciler will fix to `secondary` when site recovers)

**Reprotect — FailedOver → DRedSteadyState:**
1. `StopReplication` on Owner's VR/VGR → sets `secondary` (tolerated failure — may be no-op if already secondary)
2. `SetSource` on Owner's VR/VGR → sets `primary` (confirms writable source)
3. Health monitoring until replication established
4. **Result:** matches rest-state table

**Failback (planned_migration from DRedSteadyState) — DRedSteadyState → FailedBack:**
1. Step 0 (source/active site B): `StopVM` + `StopReplication` → B's VR/VGR set to `secondary` (demoted)
2. Per-group on Owner (target/original primary site A): `SetSource` → A's VR/VGR set to `primary` (promoted, writable)
3. Per-group: `StartVM` on target site A
4. **Result:** A = `primary` (VMs running), B = `secondary`

**Restore (reprotect from FailedBack) — FailedBack → SteadyState:**
1. `StopReplication` on Owner → sets `secondary` (tolerated)
2. `SetSource` on Owner → sets `primary` (confirms source)
3. **Result:** back to initial SteadyState configuration

## Acceptance Criteria

1. **AC1 — Failover per-group uses SetSource (not StopReplication):** Given the FailoverHandler's `ExecuteGroup`/`ExecuteGroupWithSteps` methods, when the per-group path runs on the Owner (target site), then it calls `driver.SetSource` (which sets `primary`, making the volume writable) instead of `driver.StopReplication`, and VMs start on the now-writable volume.

2. **AC2 — Step0 adds StopReplication for planned migration:** Given a planned_migration failover (GracefulShutdown=true), when `PreExecute` (Step0) runs on the source site, then after stopping VMs it calls `driver.StopReplication` for each volume group (setting `secondary` — demoting to read-only). For disaster failover (GracefulShutdown=false), Step0 does NOT call StopReplication (source may be unreachable).

3. **AC3 — Failback symmetric reverse:** Given a failback execution (planned_migration from DRedSteadyState), when the Owner (target) processes each DRGroup, then `SetSource` promotes the target VR/VGR to `primary` (writable), and Step0 on the source calls `StopReplication` to demote to `secondary`.

4. **AC4 — Reprotect unchanged:** Given the ReprotectHandler, the existing `StopReplication + SetSource` sequence is structurally unchanged. `StopReplication` sets `secondary` (tolerant), `SetSource` sets `primary` (confirms writable source).

5. **AC5 — State table invariant:** Given any rest state (SteadyState, FailedOver, DRedSteadyState, FailedBack), when the transition completes, the site where VMs are running has VR/VGR with `spec.replicationState = primary` (writable) and the other site has `secondary` (read-only).

6. **AC6 — Tests:** Failover tests updated: `SetSource` IS called during failover (not StopReplication). Step0 tests verify StopReplication for planned only. Table-driven integration test validates the full 8-phase cycle invariant. All existing tests pass with zero regressions.

## Dependencies

- **Story 13.2** (HARD): StopReplication must be changed to always set `secondary` (no flip). The `flipReplicationState` helper and `flipReplicationStates` method are removed.
- **Story 13.5** (HARD): Resolve paths must use `GetVolumeGroup` instead of `CreateVolumeGroup` before this story's tests can validate the end-to-end flow.
- **Stories 13.1, 13.3, 13.4** (SOFT): These are independent lifecycle features but their presence validates the full Epic 13 integration.

## Tasks / Subtasks

- [x] Task 1: Restructure failover per-group handler (AC: #1)
  - [x] 1.1 In `pkg/engine/failover.go` `ExecuteGroup`: replace `driver.StopReplication(ctx, vgID)` with `driver.SetSource(ctx, vgID)` in the VG loop
  - [x] 1.2 In `pkg/engine/failover.go` `ExecuteGroupWithSteps`: replace `driver.StopReplication` with `driver.SetSource`, update step name constant from `StepStopReplication` to a new `StepSetSource` constant (or rename existing)
  - [x] 1.3 Update `GroupError.StepName` references in the VG loop from `StepStopReplication` to `StepSetSource`
  - [x] 1.4 Update comments/doc-strings in `ExecuteGroup` and `ExecuteGroupWithSteps` to reflect `SetSource → StartVM` flow

- [x] Task 2: Add StopReplication to Step0 for planned migration (AC: #2)
  - [x] 2.1 In `pkg/engine/failover.go` `PreExecute`: after stopping VMs (existing code), add a loop over volume groups calling `driver.StopReplication(ctx, vgID)` for each VG
  - [x] 2.2 Gate with `if h.Config.GracefulShutdown` — only call StopReplication for planned migration (disaster Step0 remains no-op)
  - [x] 2.3 The VG resolution in Step0 needs access to the VolumeGroupInfo list (thread from WaveExecutor or resolve in PreExecute)
  - [x] 2.4 Handle StopReplication errors gracefully in Step0 — log warning but don't fail (source is being abandoned anyway)

- [x] Task 3: Update failover handler documentation (AC: #1, #2)
  - [x] 3.1 Update `pkg/engine/failover.go` file-level doc comment: `SetSource → StartVM` replaces `StopReplication → StartVM`
  - [x] 3.2 Update `pkg/engine/doc.go` "Wave executor" paragraph: per-group path is now `SetSource → StartVM`
  - [x] 3.3 Update `pkg/engine/doc.go` Step0 description: planned migration adds `StopReplication` after VM stop

- [x] Task 4: Update failover tests (AC: #1, #2, #6)
  - [x] 4.1 Update `pkg/engine/failover_test.go` — all assertions that check `drv.Called("StopReplication")` in per-group path become `drv.Called("SetSource")` assertions
  - [x] 4.2 Remove/update tests that assert "SetSource should not be called during failover" — SetSource IS now called
  - [x] 4.3 Add Step0 tests: planned migration calls StopReplication after StopVM; disaster mode does NOT call StopReplication in Step0
  - [x] 4.4 Add table-driven test: full planned migration with VR/VGR state assertions (StopReplication on source → secondary, SetSource on target → primary)
  - [x] 4.5 Add table-driven test: full disaster failover (no StopReplication on source, SetSource on target → primary)

- [x] Task 5: Verify reprotect handler (AC: #4)
  - [x] 5.1 Confirm reprotect handler is structurally unchanged (StopReplication + SetSource per VG)
  - [x] 5.2 Verify test assertions match new semantics (StopReplication = always secondary, SetSource = always primary)

- [x] Task 6: State table invariant integration test (AC: #5, #6)
  - [x] 6.1 Create table-driven integration test: full 8-phase cycle (SteadyState → FailedOver → DRedSteadyState → FailedBack → SteadyState)
  - [x] 6.2 At each rest state, assert VR/VGR `spec.replicationState` matches state table
  - [x] 6.3 Verify both VR (single-VM) and VGR (multi-VM) paths
  - [x] 6.4 Test disaster failover path (source stays primary/stale until DRPlan reconciler fixes it)

- [x] Task 7: Verify and finalize (AC: all)
  - [x] 7.1 Run `make test` — all tests pass
  - [x] 7.2 Run `make lint-fix && make lint` — zero lint issues
  - [x] 7.3 Verify no regressions in existing tests

## Dev Notes

### Key Insight: Handler Restructuring Required

After Story 13.2 changes `StopReplication` to always set `secondary` (read-only), the current failover per-group path (`StopReplication → StartVM`) would leave the target volume read-only — **VMs can't write to a secondary/read-only volume**. This story restructures the handler:

- **Per-group on Owner (target):** `SetSource → StartVM` (SetSource sets `primary` = writable, then VMs start)
- **Step0 on source (planned only):** `StopVM` + `StopReplication` (StopReplication sets `secondary` = read-only/demoted)

This aligns with the semantic model: `SetSource` = promote (writable), `StopReplication` = demote (read-only).

### Files to Modify

| File | Action | Notes |
|------|--------|-------|
| `pkg/engine/failover.go` | **Modified** | Per-group: `SetSource` replaces `StopReplication`; Step0: add `StopReplication` after StopVM for planned; update constants/comments |
| `pkg/engine/doc.go` | **Modified** | Update handler documentation to reflect new per-group path and Step0 behavior |
| `pkg/engine/failover_test.go` | **Modified** | All per-group tests assert `SetSource` (not StopReplication); add Step0 StopReplication tests; remove "SetSource should not be called during failover" assertions |
| `pkg/engine/reprotect_test.go` | **Modified** | Verify existing StopReplication+SetSource assertions still correct |
| `pkg/drivers/csiextension/driver_test.go` | **Modified** | Remove flip-based test expectations (verify 13.2 already done) |
| `pkg/drivers/csiextension/integration_test.go` | **Modified** | Lifecycle tests validate new flow |

### What NOT to Change

- `pkg/drivers/interface.go` — StorageProvider interface is unchanged (still 6 methods)
- `pkg/engine/reprotect.go` — handler logic is structurally unchanged (StopReplication+SetSource sequence)
- `pkg/controller/drplan/` — DRPlan reconciler changes are in 13.2/13.4
- Noop driver — `pkg/drivers/noop/driver.go` is unchanged

### Test Patterns from Epic 12

Follow the established patterns from Stories 12.4 and 12.6:

- **Table-driven tests** with subtests for each transition type (VR × VGR × state combination)
- **Fake driver stubs** use `OnStopReplication` / `OnSetSource` from `pkg/drivers/fake/driver.go`
- **Integration tests** use envtest with real VR/VGR CRDs registered
- **State assertions** verify `spec.replicationState` on VR/VGR CRs after each operation
- **`-race` flag** for concurrent access safety

### Current Handler Code (to be changed)

**`pkg/engine/failover.go` ExecuteGroup (current — per-group on Owner):**
```go
for _, vg := range group.Chunk.VolumeGroups {
    driver := group.DriverForVG(vg.Name)
    vgID, err := resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
    // ...
    if err := driver.StopReplication(ctx, vgID); err != nil {  // ← CHANGE TO SetSource
        return &GroupError{StepName: StepStopReplication, ...}
    }
}
// then StartVM loop...
```

**After this story:**
```go
for _, vg := range group.Chunk.VolumeGroups {
    driver := group.DriverForVG(vg.Name)
    vgID, err := resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
    // ...
    if err := driver.SetSource(ctx, vgID); err != nil {  // ← SetSource = promote to primary/writable
        return &GroupError{StepName: StepSetSource, ...}
    }
}
// then StartVM loop...
```

**Step0 PreExecute (current — source site, planned only):**
```go
func (h *FailoverHandler) PreExecute(ctx context.Context, input ExecutionInput) error {
    if !h.Config.GracefulShutdown { return nil }
    // Stop VMs...
    return nil
}
```

**After this story:**
```go
func (h *FailoverHandler) PreExecute(ctx context.Context, input ExecutionInput) error {
    if !h.Config.GracefulShutdown { return nil }
    // Stop VMs...
    // Demote source VR/VGR to secondary (read-only)
    for _, vg := range allVolumeGroups(input) {
        driver := driverForVG(vg)
        vgID := resolveVGID(vg)
        if err := driver.StopReplication(ctx, vgID); err != nil {
            logger.V(1).Info("StopReplication in Step0 failed, proceeding", "vg", vg.Name, "error", err)
        }
    }
    return nil
}
```

### State Table Invariant Test Skeleton

The integration test should exercise the full cycle and assert state at each rest point:

```go
func TestStateTableInvariant_FullCycle(t *testing.T) {
    // Setup: create VR/VGR CRs in primary/secondary state (SteadyState)
    // Phase 1: Planned failover
    //   Step0 on source: StopReplication → source=secondary
    //   Per-group on target: SetSource → target=primary
    //   StartVM on target
    //   Assert: FailedOver — target=primary, source=secondary
    // Phase 2: Reprotect
    //   StopReplication → secondary (tolerated), SetSource → primary
    //   Assert: DRedSteadyState — target=primary, source=secondary
    // Phase 3: Failback
    //   Step0 on target: StopReplication → target=secondary
    //   Per-group on source: SetSource → source=primary
    //   StartVM on source
    //   Assert: FailedBack — source=primary, target=secondary
    // Phase 4: Restore
    //   StopReplication → secondary, SetSource → primary
    //   Assert: SteadyState — source=primary, target=secondary
}
```

### Step0 Design Considerations

- Step0 currently has access to VMs (for StopVM) but NOT volume groups. The handler may need VolumeGroupInfo passed via `ExecutionInput` or resolved from the execution spec.
- StopReplication errors in Step0 are logged as warnings but don't fail the execution — the source is being abandoned and the target promotion via SetSource is what matters.
- For disaster failover, Step0 is a no-op (source unreachable). The source VR/VGR stays `primary` (stale) until the site recovers and the DRPlan reconciler updates it to `secondary` based on the new ActiveSite.

### Project Structure Notes

- All test files co-located in same package (`_test.go` in same directory)
- Integration tests in `pkg/drivers/csiextension/` use envtest (real K8s API + etcd for VR/VGR CRDs)
- Engine tests in `pkg/engine/` use fake driver stubs from `pkg/drivers/fake/`

### Build Commands

```bash
make test                     # All tests
make lint-fix && make lint    # Lint check
```

### Previous Story Intelligence

**Story 5.7 (Driver Interface Simplification & Workflow Symmetry):** Simplified Step0 to "StopVM-only" and established the per-group path as `StopReplication → StartVM`. This story REVERSES the Step0 simplification (adds StopReplication back for planned migration) and changes per-group to `SetSource → StartVM`.

**Story 12.4 (StopReplication & SetSource):** Established the current flip-based StopReplication with `flipReplicationStates` helper. This story's 13.2 prerequisite changes that to always-secondary. Key patterns: `crSet` type, `updateReplicationState` shared helper, table-driven tests with VR/VGR × state combinations.

**Story 12.6 (Conformance Suite & Integration):** Established the integration test patterns with `conformanceAdapter` bridging CSI model to conformance suite. The lifecycle tests (Create → StopReplication → SetSource → GetReplicationStatus → Delete) are the template for this story's state table invariant test.

**Story 13.5 (Remove CreateVolumeGroup from resolve paths):** Changes `resolveVolumeGroupID` in `pkg/engine/failover.go`, `resolveVGID` in `pkg/controller/drexecution/reconciler.go`, and `resolveVolumeGroupID` in `pkg/controller/drplan/health.go` to use `GetVolumeGroup` instead of `CreateVolumeGroup`. Test stubs change from `OnCreateVolumeGroup` to `OnGetVolumeGroup`. This story (13.6) builds on top of those resolve changes.

### References

- [Source: `_bmad-output/planning-artifacts/epics.md` lines 2969-3015 — Story 13.6 epic definition]
- [Source: `pkg/drivers/csiextension/helpers.go` — crSet, flipReplicationState, listCRsForVG, updateReplicationState]
- [Source: `pkg/drivers/csiextension/driver.go` — StopReplication, SetSource implementations]
- [Source: `pkg/engine/failover.go` — FailoverHandler, resolveVolumeGroupID, ExecuteGroupWithSteps]
- [Source: `pkg/engine/reprotect.go` — ReprotectHandler, Execute phase 1 (StopReplication + SetSource)]
- [Source: `pkg/controller/volumereplication/reconciler.go` — stateForReplicationState (resync case removed by 13.2)]
- [Source: `_bmad-output/project-context.md` — project conventions, testing rules, CRD management rule #12]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- `TestWaveExecutor_PartialFailure_ReportsPartiallySucceeded` failed after Task 1 changes because error message changed from "StopReplication" to "SetSource" — fixed by updating the assertion in `executor_test.go`
- `dupl` lint error in `failover_test.go` from nearly identical `runReprotect` logic in Phase2/Phase4 of `TestStateTableInvariant_FullCycle` — resolved by extracting shared `runReprotect` helper function

### Completion Notes List

- **Task 1:** Renamed `StepStopReplication` constant to `StepSetSource` in `failover.go`. Replaced `driver.StopReplication` with `driver.SetSource` in both `ExecuteGroup` and `ExecuteGroupWithSteps` per-group VG loops. Updated all `GroupError.StepName` references and log messages to reflect "Promoting volume group to primary (SetSource)" semantics.
- **Task 2:** Extended `PreExecute` (Step0) for planned migration: after stopping VMs, iterates through all volume groups across all execution groups and calls `driver.StopReplication` to demote source VR/VGR to secondary. Errors are logged as warnings but do not fail the execution (source is being abandoned). Disaster Step0 remains a no-op as expected.
- **Task 3:** Updated file-level doc comment in `failover.go`, `FailoverConfig` struct comment, and `doc.go` wave executor and Step0 descriptions to reflect the new `SetSource → StartVM` per-group path and `StopVM + StopReplication` Step0 for planned migration.
- **Task 4:** Updated 11 existing tests to assert `SetSource` instead of `StopReplication` in per-group path. Renamed `NoSetSource` tests to `NoStopReplication` variants. Added Step0 tests (planned calls StopReplication, disaster does not). Added table-driven tests for planned migration and disaster failover with complete driver call assertions.
- **Task 5:** Verified `pkg/engine/reprotect.go` is structurally unchanged — `StopReplication + SetSource` sequence preserved. No modifications required.
- **Task 6:** Added `TestStateTableInvariant_FullCycle` (4-phase table-driven: SteadyState→FailedOver→DRedSteadyState→FailedBack→SteadyState), `TestStateTableInvariant_DisasterFailover`, and `TestStateTableInvariant_MultipleVolumeGroups_VR_and_VGR`. Each asserts driver calls match the state table invariant (primary where VMs run, secondary where they don't). Refactored reprotect phases into shared `runReprotect` helper to eliminate dupl lint errors.
- **Task 7:** All unit tests pass (`make test`), all integration tests pass (`make integration`), lint clean for story-modified files. Pre-existing dupl/errcheck issues in `reconciler_test.go`/`health_test.go` not introduced by this story.
- Updated `_bmad-output/project-context.md` to reflect new failover handler behavior.
- Updated `pkg/engine/executor_test.go` assertion from "StopReplication" to "SetSource" in `TestWaveExecutor_PartialFailure_ReportsPartiallySucceeded`.

### File List

- `pkg/engine/failover.go` — Modified: per-group `SetSource` replaces `StopReplication`, Step0 adds `StopReplication` for planned migration, updated constants/comments
- `pkg/engine/failover_test.go` — Modified: updated per-group assertions to `SetSource`, renamed tests, added Step0/table-driven/state-table-invariant tests
- `pkg/engine/doc.go` — Modified: updated wave executor and Step0 documentation
- `pkg/engine/executor_test.go` — Modified: updated `StepSetSource` assertion in partial failure test
- `_bmad-output/project-context.md` — Modified: updated unified handler model description

## Change Log

- 2026-06-12: Story 13.6 implemented — failover per-group path changed from StopReplication→StartVM to SetSource→StartVM; Step0 extended with StopReplication for planned migration; 3 state-table-invariant tests added covering full 8-phase cycle, disaster path, and mixed VR/VGR volume groups
- 2026-06-13: Review patches applied — Step0 fail-fast on resolve and StopReplication errors (preserves no-dual-primary invariant), FullCycle test rewritten as sequential with shared driver and cumulative assertions, resolve-error and StopReplication-error fail-fast tests added, MultiNamespace test GetVolumeGroup stubs added, tolerance language removed from doc comments

### Review Findings

- [x] [Review][Patch] Fail planned migration if any Step 0 source demotion fails instead of marking `Step0Complete` [`pkg/engine/failover.go:171`]
- [x] [Review][Patch] Do not silently continue when Step 0 cannot resolve a volume group [`pkg/engine/failover.go:164`]
- [x] [Review][Patch] Replace fake-driver call-count “state invariant” tests with CR-level replicationState assertions across a real sequential cycle and both sites [`pkg/engine/failover_test.go:1198`]
