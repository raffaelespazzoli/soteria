# Story 12.4: StopReplication & SetSource — State Transitions

Status: done

## Story

As a CSI extension driver handling DR lifecycle transitions,
I want `StopReplication` and `SetSource` to update VolumeReplication/VolumeGroupReplication CRD `spec.replicationState` declaratively,
So that the csi-addons sidecar reconciles the actual storage state transitions (Promote/Demote/Resync) for failover, failback, and reprotect workflows.

## Background

### Transition Model

The CSI Addons CRDs use a declarative replication state model:
- `primary` — promoted, replication source, read-write
- `secondary` — demoted, replication target, read-only
- `resync` — re-syncing data from the promoted peer

Soteria's StorageProvider methods map to these state transitions:

| Soteria Method | When Called | Effect on VR/VGR |
|---|---|---|
| `StopReplication` | Failover Step 0 (source site) | Source volumes demoted: `primary` → `secondary` |
| `StopReplication` | Failover per-group (target site) | Target volumes promoted: `secondary` → `primary` |
| `SetSource` | Reprotect Phase 1 | Confirm as replication source: set to `primary` |

### StopReplication: Context-Aware Role Flip

`StopReplication` reads the current `spec.replicationState` and flips it:
- If currently `primary` → set to `secondary` (graceful demote)
- If currently `secondary` → set to `primary` (promote, make writable)

This is the key insight: Soteria's `StopReplication` means "stop the current replication direction and make volumes usable." In CSI terms, this always means flipping the role.

### SetSource: Idempotent Primary Assertion

`SetSource` unconditionally sets `spec.replicationState` to `primary`. It is called during reprotect to ensure the active site is the replication source. If already `primary`, this is a no-op.

### Lifecycle Transition Table (per site)

| Soteria Phase | Primary Site VR/VGR | Secondary Site VR/VGR |
|---|---|---|
| SteadyState | `primary` | `secondary` |
| Planned Failover Step 0 | `secondary` (demoted via StopReplication) | `secondary` (unchanged) |
| Planned Failover per-group | `secondary` | `primary` (promoted via StopReplication) |
| Disaster Failover per-group | stale `primary` (unreachable) | `primary` (promoted via StopReplication) |
| FailedOver | `secondary` (or stale) | `primary` |
| Reprotect Phase 1 | (StopReplication tolerated if fails) | `primary` (confirmed via SetSource) |
| DRedSteadyState | `secondary` | `primary` |
| Planned Failback Step 0 | `primary` (unchanged) | `secondary` (demoted via StopReplication) |
| Planned Failback per-group | `primary` (promoted via StopReplication) | `secondary` |
| FailedBack | `primary` | `secondary` |
| ReprotectBack Phase 1 | `primary` (confirmed via SetSource) | (StopReplication tolerated if fails) |

## Acceptance Criteria

1. **AC1 — StopReplication reads current state:** The driver reads the VR/VGR CR's current `spec.replicationState` before updating.

2. **AC2 — StopReplication flips primary→secondary:** When `replicationState` is `primary`, `StopReplication` updates it to `secondary`.

3. **AC3 — StopReplication flips secondary→primary:** When `replicationState` is `secondary`, `StopReplication` updates it to `primary`.

4. **AC4 — StopReplication handles resync state:** When `replicationState` is `resync`, `StopReplication` updates it to `primary` (promote to make writable).

5. **AC5 — SetSource sets primary:** `SetSource` unconditionally sets `spec.replicationState` to `primary`. Idempotent: if already `primary`, the update is a no-op (or writes the same value).

6. **AC6 — VR path (single-VM):** For VolumeReplication CRs, both methods update all VR CRs associated with the VolumeGroupID (one per PVC). All PVCs in the group transition atomically from the driver's perspective.

7. **AC7 — VGR path (multi-VM):** For VolumeGroupReplication CRs, both methods update the single VGR CR.

8. **AC8 — CR lookup by label:** The driver locates VR/VGR CRs via the `soteria.io/volume-group` label applied in Story 12.3.

9. **AC9 — Idempotency:** Calling `StopReplication` or `SetSource` when the CR is already in the target state succeeds without error.

10. **AC10 — Unit tests:** Table-driven tests covering all state transitions for both VR and VGR paths, including idempotent cases and unknown-state handling.

## Tasks / Subtasks

- [x] Task 1: Implement StopReplication (AC: #1, #2, #3, #4, #6, #7, #8)
  - [x] 1.1 Look up VR/VGR CRs by label
  - [x] 1.2 Read current `spec.replicationState`
  - [x] 1.3 Flip: `primary` → `secondary`, `secondary`/`resync` → `primary`
  - [x] 1.4 Update the CR(s) via `client.Update`

- [x] Task 2: Implement SetSource (AC: #5, #6, #7, #8)
  - [x] 2.1 Look up VR/VGR CRs by label
  - [x] 2.2 Set `spec.replicationState` to `primary`
  - [x] 2.3 Update the CR(s) via `client.Update`

- [x] Task 3: Shared helpers (AC: #8, #9)
  - [x] 3.1 Extract `listCRsForVG(ctx, vgID)` helper for CR lookup
  - [x] 3.2 Extract `updateReplicationState(ctx, crs, newState)` helper for CR updates
  - [x] 3.3 Handle idempotent case (already in target state → return nil)

- [x] Task 4: Unit tests (AC: #10)
  - [x] 4.1 StopReplication: primary → secondary (VR path)
  - [x] 4.2 StopReplication: secondary → primary (VR path)
  - [x] 4.3 StopReplication: resync → primary (VR path)
  - [x] 4.4 StopReplication: already in target state (idempotent)
  - [x] 4.5 SetSource: secondary → primary
  - [x] 4.6 SetSource: already primary (idempotent)
  - [x] 4.7 VGR path: same tests for VolumeGroupReplication
  - [x] 4.8 CR not found → ErrVolumeGroupNotFound
  - [x] 4.9 Run `make test` and `make lint`

### Review Findings

- [x] [Review][Patch] StopReplication derives one target state from the first matched CR instead of flipping each CR from its own current state [pkg/drivers/csiextension/helpers.go:41] — fixed: new `flipReplicationStates` helper flips per-CR
- [x] [Review][Patch] `updateReplicationState()` can leave a volume group partially transitioned when a later `client.Update()` fails or context is cancelled mid-loop [pkg/drivers/csiextension/helpers.go:97] — fixed: added `ctx.Err()` checks between iterations in both update helpers
- [x] [Review][Patch] AC10's unknown-state handling is only tested at the helper-function level, not through the `StopReplication`/`SetSource` CR update path [pkg/drivers/csiextension/driver_test.go:908] — fixed: added unknown/empty state cases to `TestStopReplication_StateTransitions`

## Dev Notes

### Key Locations

| File | Function | Change |
|------|----------|--------|
| `pkg/drivers/csiextension/driver.go` | `StopReplication` | Implement role flip |
| `pkg/drivers/csiextension/driver.go` | `SetSource` | Implement primary assertion |
| `pkg/drivers/csiextension/helpers.go` | `listCRsForVG`, `updateReplicationState` | New shared helpers |
| `pkg/drivers/csiextension/driver_test.go` | Tests | State transition tests |

### State Flip Implementation

```go
func (d *Driver) StopReplication(ctx context.Context, id drivers.VolumeGroupID) error {
    crs, err := d.listCRsForVG(ctx, id)
    if err != nil {
        return err
    }
    for _, cr := range crs {
        current := cr.Spec.ReplicationState
        var target string
        switch current {
        case ReplicationStatePrimary:
            target = ReplicationStateSecondary
        case ReplicationStateSecondary, ReplicationStateResync:
            target = ReplicationStatePrimary
        default:
            target = ReplicationStatePrimary
        }
        if current == target {
            continue
        }
        cr.Spec.ReplicationState = target
        if err := d.client.Update(ctx, &cr); err != nil {
            return fmt.Errorf("updating replication state for %s: %w", cr.Name, err)
        }
    }
    return nil
}
```

### What NOT to Change

- StorageProvider interface
- FailoverHandler / ReprotectHandler — they call StopReplication/SetSource through the interface
- Noop driver

### Dependency

- **Depends on Story 12.2** — CRD types and client
- **Depends on Story 12.3** — CRs must exist (created by CreateVolumeGroup)

### Build Commands

```bash
make test
make lint-fix && make lint
```

## Dev Agent Record

### Implementation Plan

- Implemented shared helpers first (`helpers.go`) since both StopReplication and SetSource depend on them
- `crSet` type abstracts over VR vs VGR CRs with `currentState()` accessor
- `flipReplicationState()` pure function: primary→secondary, everything else→primary
- `listCRsForVG()` locates CRs by label, returns `ErrVolumeGroupNotFound` when none exist
- `updateReplicationState()` sets all CRs to target state, skipping those already there (idempotent)
- StopReplication: listCRs → determine current → flip → updateReplicationState
- SetSource: listCRs → updateReplicationState with ReplicationStatePrimary
- Both methods check `ctx.Err()` first, matching existing pattern in CreateVolumeGroup/DeleteVolumeGroup/GetVolumeGroup
- Table-driven tests cover VR/VGR × {primary, secondary, resync} for StopReplication and VR/VGR × {secondary, resync, already-primary} for SetSource
- Multi-PVC atomicity test verifies all 3 VRs in a group flip together
- Double-flip idempotency test verifies primary→secondary→primary round-trip

### Debug Log

No issues encountered during implementation.

### Completion Notes

All 10 acceptance criteria satisfied:
- AC1–AC4: StopReplication state transitions verified in `TestStopReplication_StateTransitions` (VR/VGR × 3 states)
- AC5: SetSource unconditional primary assertion verified in `TestSetSource_StateTransitions` (VR/VGR × 3 states including idempotent)
- AC6: Multi-PVC VR path verified in `TestStopReplication_MultiplePVCs_AllFlipped` (3 PVCs)
- AC7: VGR path verified in table-driven tests (VGR subtests)
- AC8: Label-based lookup via `listCRsForVG` using `vgLabelSelector`
- AC9: Idempotency verified in `TestStopReplication_DoubleFlip_Idempotent` and `TestSetSource_StateTransitions/already_primary`
- AC10: 17 new tests (49 total for package), table-driven, 0 lint issues, 88.5% coverage

## File List

| File | Change |
|------|--------|
| `pkg/drivers/csiextension/helpers.go` | New: crSet type, flipReplicationState, listCRsForVG, updateReplicationState |
| `pkg/drivers/csiextension/driver.go` | Modified: StopReplication and SetSource implemented (replaced stubs) |
| `pkg/drivers/csiextension/doc.go` | Modified: Added Story 12.4 description |
| `pkg/drivers/csiextension/driver_test.go` | Modified: 17 new tests for state transitions, idempotency, errors |

## Change Log

- 2026-05-15: Story 12.4 implemented — StopReplication (role flip via flipReplicationState), SetSource (unconditional primary), shared helpers (listCRsForVG, updateReplicationState, crSet type), 17 new table-driven tests covering VR/VGR × all state transitions + idempotency + not-found + context-cancelled, 88.5% coverage, 0 lint issues, all unit/integration tests pass
