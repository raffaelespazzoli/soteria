# Story 11.4: Health Monitoring Resolves Driver from Plan Spec

Status: done

## Story

As a DRPlan controller performing replication health polling,
I want to resolve the storage driver from the plan's declared `volumeReplicationDriver` field,
So that health monitoring is deterministic and no longer requires runtime PVC → StorageClass → provisioner lookups.

## Background

### Current State

The `DRPlanReconciler.pollReplicationHealth` path resolves a driver per VolumeGroup via `resolveDriverForVG`:
1. For each VM in the VG, call `PVCResolver.ResolvePVCNames` to get PVC names
2. For each PVC, fetch the PVC object and extract `storageClassName`
3. Call `Registry.GetDriverForPVC(ctx, scName, SCLister)` to resolve provisioner → driver
4. Fall back to `Registry.GetDriver("")` if no SC found

This requires `PVCResolver`, `SCLister`, and the `Get` client on the reconciler just for driver resolution. With the explicit `volumeReplicationDriver` field, this collapses to `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)`.

### Scope

This story simplifies `resolveDriverForVG` and removes `PVCResolver` and `SCLister` dependencies from the health polling path. The `getPVC` helper may also be removed if unused after this change.

## Acceptance Criteria

1. **AC1 — resolveDriverForVG uses declared driver:** `resolveDriverForVG` calls `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)` instead of iterating VMs → PVCs → storage classes. The method signature changes to accept the plan's driver name.

2. **AC2 — pollReplicationHealth passes driver name:** `pollReplicationHealth` receives the plan's `VolumeReplicationDriver` value and passes it to `pollSingleVG` → `resolveDriverForVG`.

3. **AC3 — PVCResolver dependency removed from health path:** The health polling methods no longer use `r.PVCResolver` for driver resolution. If `PVCResolver` is only used in health.go, remove the field from `DRPlanReconciler`. If used elsewhere (e.g., VG ID resolution), keep it but document that health no longer needs it for driver lookup.

4. **AC4 — SCLister dependency removed from health path:** The health polling methods no longer use `r.SCLister` for driver resolution. If `SCLister` is used elsewhere on the reconciler, keep the field but document the change.

5. **AC5 — resolveVolumeGroupID unchanged:** The `resolveVolumeGroupID` method still uses `PVCResolver` to collect PVC names for the `VolumeGroupSpec`. This is for VG identity, not driver resolution, and remains unchanged.

6. **AC6 — No fallback logic:** No `"fallback: true"` flag or message for VGs without PVC storage classes. The driver is always known from the plan.

7. **AC7 — getPVC evaluated for deletion:** If `getPVC` is only used by `resolveDriverForVG`, delete it. If used by `resolveVolumeGroupID` or elsewhere, keep it.

8. **AC8 — Health tests updated:** Tests in `pkg/controller/drplan/health_test.go` are updated to remove SC/PVC mocking for driver resolution. Tests verify that the declared driver is used.

9. **AC9 — Reconciler health wiring updated:** The reconciler's call to `pollReplicationHealth` passes the plan's driver name.

## Tasks / Subtasks

- [x] Task 1: Simplify resolveDriverForVG (AC: #1, #6)
  - [x] 1.1 Change `resolveDriverForVG` to accept a `driverName string` parameter
  - [x] 1.2 Replace PVC → SC → provisioner iteration with `r.Registry.GetDriver(driverName)`
  - [x] 1.3 Remove fallback return value — the driver is always known

- [x] Task 2: Update pollSingleVG and pollReplicationHealth (AC: #2, #9)
  - [x] 2.1 Thread the plan's `VolumeReplicationDriver` through `pollReplicationHealth` → `pollSingleVG` → `resolveDriverForVG`
  - [x] 2.2 Update `pollReplicationHealth` signature to accept `driverName string` or the full `DRPlan`

- [x] Task 3: Clean up dependencies (AC: #3, #4, #7)
  - [x] 3.1 Remove PVC/SC resolution code from `resolveDriverForVG`
  - [x] 3.2 Evaluate if `PVCResolver` and `SCLister` are still needed on `DRPlanReconciler` for other purposes
  - [x] 3.3 Evaluate if `getPVC` helper is still needed; delete if orphaned

- [x] Task 4: Update tests (AC: #8)
  - [x] 4.1 Update `pkg/controller/drplan/health_test.go` — remove SC/PVC mocking in driver resolution tests
  - [x] 4.2 Add test: verify `resolveDriverForVG` uses the declared driver name
  - [x] 4.3 Run `make test` — all tests pass
  - [x] 4.4 Run `make lint-fix && make lint` — zero new lint issues (3 pre-existing issues in unrelated files)

## Dev Notes

### Key Locations

| File | Function | Change |
|------|----------|--------|
| `pkg/controller/drplan/health.go:52-75` | `pollReplicationHealth` | Accept driver name, pass to `pollSingleVG` |
| `pkg/controller/drplan/health.go:79-130` | `pollSingleVG` | Accept driver name, pass to `resolveDriverForVG` |
| `pkg/controller/drplan/health.go:139-175` | `resolveDriverForVG` | Replace PVC iteration with `Registry.GetDriver(driverName)` |
| `pkg/controller/drplan/health.go:178-189` | `getPVC` | Delete if orphaned |
| `pkg/controller/drplan/reconciler.go` | `pollReplicationHealth` call site | Pass `plan.Spec.VolumeReplicationDriver` |

### Before and After

**Before:**
```go
func (r *DRPlanReconciler) resolveDriverForVG(
    ctx context.Context, vg soteriav1alpha1.VolumeGroupInfo,
) (drv drivers.StorageProvider, fallback bool, err error) {
    // 40 lines: iterate VMs → PVCs → storage classes → GetDriverForPVC
}
```

**After:**
```go
func (r *DRPlanReconciler) resolveDriverForVG(
    ctx context.Context, driverName string,
) (drivers.StorageProvider, error) {
    return r.Registry.GetDriver(driverName)
}
```

### What NOT to Change

- `resolveVolumeGroupID` — still uses `PVCResolver` for VG identity (PVC names in `VolumeGroupSpec`)
- `pkg/engine/executor.go` — Story 11.2
- `internal/preflight/` — Story 11.3
- `console-plugin/` — Story 11.5
- Health status mapping (`mapReplicationStatus`) — unchanged
- Health condition computation (`computeReplicationCondition`) — unchanged
- Health transition detection (`detectHealthTransitions`) — unchanged

### Dependency

- **Depends on Story 11.1** — the `VolumeReplicationDriver` field must exist on `DRPlanSpec` and `"noop"` must resolve via `Registry.GetDriver`.

### Previous Story Intelligence

- **Story 5.1 (Replication Health Monitoring):** Original implementation of health polling with driver resolution. This story simplifies that path.

### Build Commands

```bash
make test                 # All unit tests
make lint-fix && make lint # Code style
```

### Review Findings

- [x] [Review][Patch] Unknown-driver tests do not match production registry behavior [`pkg/controller/drplan/health_test.go`] — Added comments clarifying that these tests intentionally use an empty registry (no fallback) to exercise the error-handling path, which diverges from production wiring where the noop fallback would mask the error.
