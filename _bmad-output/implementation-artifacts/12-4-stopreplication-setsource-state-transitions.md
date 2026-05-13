# Story 12.4: StopReplication & SetSource — State Transitions

Status: ready-for-dev

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

- [ ] Task 1: Implement StopReplication (AC: #1, #2, #3, #4, #6, #7, #8)
  - [ ] 1.1 Look up VR/VGR CRs by label
  - [ ] 1.2 Read current `spec.replicationState`
  - [ ] 1.3 Flip: `primary` → `secondary`, `secondary`/`resync` → `primary`
  - [ ] 1.4 Update the CR(s) via `client.Update`

- [ ] Task 2: Implement SetSource (AC: #5, #6, #7, #8)
  - [ ] 2.1 Look up VR/VGR CRs by label
  - [ ] 2.2 Set `spec.replicationState` to `primary`
  - [ ] 2.3 Update the CR(s) via `client.Update`

- [ ] Task 3: Shared helpers (AC: #8, #9)
  - [ ] 3.1 Extract `listCRsForVG(ctx, vgID)` helper for CR lookup
  - [ ] 3.2 Extract `updateReplicationState(ctx, crs, newState)` helper for CR updates
  - [ ] 3.3 Handle idempotent case (already in target state → return nil)

- [ ] Task 4: Unit tests (AC: #10)
  - [ ] 4.1 StopReplication: primary → secondary (VR path)
  - [ ] 4.2 StopReplication: secondary → primary (VR path)
  - [ ] 4.3 StopReplication: resync → primary (VR path)
  - [ ] 4.4 StopReplication: already in target state (idempotent)
  - [ ] 4.5 SetSource: secondary → primary
  - [ ] 4.6 SetSource: already primary (idempotent)
  - [ ] 4.7 VGR path: same tests for VolumeGroupReplication
  - [ ] 4.8 CR not found → ErrVolumeGroupNotFound
  - [ ] 4.9 Run `make test` and `make lint`

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
