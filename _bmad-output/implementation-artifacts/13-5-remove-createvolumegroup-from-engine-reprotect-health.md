# Story 13.5: Remove CreateVolumeGroup from Engine, Reprotect & Health Paths

Status: done

## Story

As a developer,
I want the failover engine, reprotect handler, and health polling to use `GetVolumeGroup` instead of `CreateVolumeGroup` for VG ID resolution,
So that VR/VGR creation responsibility is exclusively owned by the DRPlan reconciler (Story 13.2), and execution/monitoring paths only read.

## Background

Three resolve functions currently call `driver.CreateVolumeGroup` as an idempotent "get-or-create" to obtain a `VolumeGroupID`:

1. **`resolveVolumeGroupID`** in `pkg/engine/failover.go:88-112` — used by `ExecuteGroup` and `ExecuteGroupWithSteps` during failover/failback
2. **`resolveVGID`** on `DRExecutionReconciler` in `pkg/controller/drexecution/reconciler.go:790-815` — used by `buildVolumeGroupEntries` for reprotect
3. **`resolveVolumeGroupID`** on `DRPlanReconciler` in `pkg/controller/drplan/health.go:141-166` — used by `pollSingleVG` for health monitoring

With Story 13.2, the DRPlan reconciler owns VR/VGR creation. These three paths should switch to the read-only `GetVolumeGroup` method. The VolumeGroupID can be computed deterministically as `csi-ext-<namespace>/<name>` (established in Story 12.3), but using `GetVolumeGroup` validates the VR/VGR exists.

Additionally, all three functions resolve PVC names via `PVCResolver` before calling `CreateVolumeGroup`. Since `GetVolumeGroup` takes only a `VolumeGroupID` (not a `VolumeGroupSpec`), the PVC resolution calls are no longer needed in these paths.

## Acceptance Criteria

1. **AC1 — Engine failover path uses GetVolumeGroup:** `resolveVolumeGroupID` in `pkg/engine/failover.go` calls `driver.GetVolumeGroup` instead of `driver.CreateVolumeGroup`. If VG is not found (`ErrVolumeGroupNotFound`), the error propagates with a clear message indicating the DRPlan reconciler has not yet created the VR/VGR.

2. **AC2 — DRExecution reprotect path uses GetVolumeGroup:** `resolveVGID` in `pkg/controller/drexecution/reconciler.go` calls `drv.GetVolumeGroup` instead of `drv.CreateVolumeGroup`.

3. **AC3 — DRPlan health path uses GetVolumeGroup:** `resolveVolumeGroupID` in `pkg/controller/drplan/health.go` calls `drv.GetVolumeGroup` instead of `drv.CreateVolumeGroup`.

4. **AC4 — PVC resolution removed from resolve paths:** `PVCResolver` calls are removed from all three resolve functions. The VG ID is computed deterministically from the VG name/namespace using `vgIDFromNamespace` (the `csi-ext-<namespace>/<name>` format from Story 12.3).

5. **AC5 — Tests updated:** All tests that stub `OnCreateVolumeGroup` for resolve purposes are updated to stub `OnGetVolumeGroup` instead. New tests verify `ErrVolumeGroupNotFound` is handled gracefully with a descriptive error. All existing tests pass with zero regressions, 0 lint issues.

## Tasks / Subtasks

- [ ] Task 1: Refactor engine failover resolve (AC: #1, #4)
  - [ ] 1.1 Rewrite `resolveVolumeGroupID` in `pkg/engine/failover.go` to compute `vgID` deterministically and call `driver.GetVolumeGroup(ctx, vgID)` — drop `PVCResolver` param and `CreateVolumeGroup` call
  - [ ] 1.2 Update `resolveVolumeGroupID` signature: remove `pvcResolver PVCResolver` parameter
  - [ ] 1.3 Update all callers in `failover.go` (`ExecuteGroup`, `ExecuteGroupWithSteps`) to drop the `group.PVCResolver` argument
  - [ ] 1.4 Add descriptive error wrapping for `ErrVolumeGroupNotFound` case: `"VR/VGR not yet created by DRPlan reconciler for volume group %s"`

- [ ] Task 2: Refactor DRExecution reprotect resolve (AC: #2, #4)
  - [ ] 2.1 Rewrite `resolveVGID` in `pkg/controller/drexecution/reconciler.go` to compute `vgID` deterministically and call `drv.GetVolumeGroup(ctx, vgID)` — drop `PVCResolver` usage
  - [ ] 2.2 Remove `r.WaveExecutor.PVCResolver` references from `resolveVGID`

- [ ] Task 3: Refactor DRPlan health resolve (AC: #3, #4)
  - [ ] 3.1 Rewrite `resolveVolumeGroupID` in `pkg/controller/drplan/health.go` to compute `vgID` deterministically and call `drv.GetVolumeGroup(ctx, vgID)` — drop `PVCResolver` usage
  - [ ] 3.2 Remove `r.PVCResolver` references from `resolveVolumeGroupID`

- [ ] Task 4: Update tests (AC: #5)
  - [ ] 4.1 Update `pkg/engine/failover_test.go`: replace all `drv.OnCreateVolumeGroup()` stubs with `drv.OnGetVolumeGroup()` stubs returning the same `VolumeGroupInfo`
  - [ ] 4.2 Add test in `failover_test.go` for `ErrVolumeGroupNotFound` propagation with descriptive message
  - [ ] 4.3 Update `pkg/controller/drplan/health_test.go`: replace all `fakeDriver.OnCreateVolumeGroup()` stubs with `fakeDriver.OnGetVolumeGroup()` stubs
  - [ ] 4.4 Add test in `health_test.go` for `ErrVolumeGroupNotFound` returning Unknown health with descriptive message
  - [ ] 4.5 Update any DRExecution reconciler tests that stub `CreateVolumeGroup` for reprotect VG resolution

- [ ] Task 5: Update doc comments (AC: all)
  - [ ] 5.1 Update godoc on all three resolve functions to reflect `GetVolumeGroup` usage
  - [ ] 5.2 Update `pkg/engine/doc.go` if it references `CreateVolumeGroup` in resolve context

- [ ] Task 6: Verify (AC: all)
  - [ ] 6.1 Run `make test` — all tests pass
  - [ ] 6.2 Run `make lint-fix && make lint` — zero lint issues

### Review Findings

- [x] [Review][Patch] Reusing `GetVolumeGroup` with the default noop registry breaks noop plans because each lookup returns a fresh in-memory driver [`pkg/drivers/registry.go:96`]
- [x] [Review][Patch] Add a DRExecution regression test for `resolveVGID` and `buildVolumeGroupEntries` when `GetVolumeGroup` returns `ErrVolumeGroupNotFound` [`pkg/controller/drexecution/reconciler.go:795`]

## Dev Notes

### Key Locations — Production Code

| File | Action | Current Function |
|------|--------|------------------|
| `pkg/engine/failover.go` | Modify | `resolveVolumeGroupID` (lines 85-112) — remove PVCResolver param, switch to deterministic ID + GetVolumeGroup |
| `pkg/controller/drexecution/reconciler.go` | Modify | `resolveVGID` (lines 790-815) — remove PVCResolver usage, switch to deterministic ID + GetVolumeGroup |
| `pkg/controller/drplan/health.go` | Modify | `resolveVolumeGroupID` (lines 138-166) — remove PVCResolver usage, switch to deterministic ID + GetVolumeGroup |

### Key Locations — Test Code

| File | Action | Changes |
|------|--------|---------|
| `pkg/engine/failover_test.go` | Modify | ~15 `OnCreateVolumeGroup` → `OnGetVolumeGroup` replacements + 1 new ErrVolumeGroupNotFound test |
| `pkg/controller/drplan/health_test.go` | Modify | ~6 `OnCreateVolumeGroup` → `OnGetVolumeGroup` replacements + 1 new not-found test |
| `pkg/controller/drexecution/reconciler_test.go` or integration tests | Modify (if applicable) | Replace any CreateVolumeGroup stubs in reprotect test paths |

### VolumeGroupID Deterministic Computation

The VolumeGroupID format is `csi-ext-<namespace>/<name>` (from Story 12.3). The function `vgIDFromNamespace` in `pkg/drivers/csiextension/driver.go:60-62`:

```go
func vgIDFromNamespace(namespace, name string) drivers.VolumeGroupID {
    return drivers.VolumeGroupID(vgIDPrefix + namespace + "/" + name)
}
```

However, `vgIDFromNamespace` is unexported and package-private to `csiextension`. The resolve functions in `engine` and `controller` packages cannot import it directly. Two options:

**Option A (Recommended): Inline the computation** — The format `csi-ext-<namespace>/<name>` is simple enough to compute inline in each resolve function without importing the driver package. This avoids coupling engine/controller to a specific driver implementation.

**Option B: Export from drivers package** — Export a `VolumeGroupIDFromSpec(namespace, name string)` function. This creates a dependency on the ID format being standardized.

**Decision: Use Option A.** The resolve functions should compute the ID deterministically. The `GetVolumeGroup` call serves as validation that the VR/VGR actually exists — if the DRPlan reconciler hasn't created them yet, `GetVolumeGroup` returns `ErrVolumeGroupNotFound`.

**Important:** The noop driver uses a different ID format (`noop-<name>`). The resolve functions are called with the plan's declared driver, so the ID format must match the driver in use. Rather than hardcoding `csi-ext-` prefix, the cleanest approach is to compute the ID deterministically only for the `GetVolumeGroup` call. Since `GetVolumeGroup` takes a `VolumeGroupID`, we need the ID.

**Revised approach:** Simply call `driver.GetVolumeGroup` with a deterministic ID for the csi-extension driver. But for the noop driver, `GetVolumeGroup` also works — it just looks up by the same ID format. The key insight: each driver's `CreateVolumeGroup` was producing a deterministic ID from `(namespace, name)`. We just need each driver's `GetVolumeGroup` to work with IDs computed from the same `(namespace, name)` input. Since both drivers already do this, we can keep using a helper.

**Final approach:** The simplest change is to keep calling `driver.GetVolumeGroup(ctx, vgID)` where `vgID` is computed as `drivers.VolumeGroupID(fmt.Sprintf("csi-ext-%s/%s", vg.Namespace, vg.Name))` for the csi-extension driver. But since the resolve function is driver-agnostic, we need a driver-agnostic way to compute the ID.

**Cleanest solution:** Instead of computing the ID externally, just call `GetVolumeGroup` with the same `VolumeGroupID` that `CreateVolumeGroup` would have returned. Since `CreateVolumeGroup` is deterministic from `(name, namespace)`, add a new `VolumeGroupID` method to `StorageProvider` interface — but that's scope creep.

**Practical solution:** Add a package-level helper `ComputeVolumeGroupID(driverType, namespace, name string) VolumeGroupID` in `pkg/drivers/` that encodes the driver-specific prefix. This is justified because the ID format is a driver contract:

```go
// In pkg/drivers/id.go (new file, ~10 lines)
func ComputeVolumeGroupID(driverType, namespace, name string) VolumeGroupID {
    switch driverType {
    case "csi-extension":
        return VolumeGroupID("csi-ext-" + namespace + "/" + name)
    default: // noop and future drivers
        return VolumeGroupID("noop-" + name)
    }
}
```

**OR even simpler:** Since the resolve functions already have access to the driver instance, we can call `driver.GetVolumeGroup` with a well-known ID. But `GetVolumeGroup` needs a `VolumeGroupID` we don't have without creating first.

**FINAL ANSWER — Use the approach from the epic:** The epic says "the ID format is `csi-ext-<namespace>/<name>` (from Story 12.3) — this can be computed deterministically without calling `GetVolumeGroup` at all, but using `GetVolumeGroup` validates the VR/VGR exists." The simplest implementation: compute the ID deterministically using a shared helper in `pkg/drivers/`, then validate with `GetVolumeGroup`. The helper belongs in `pkg/drivers/` because the ID format is part of the driver contract. Keep it minimal:

```go
// pkg/drivers/id.go
package drivers

// VolumeGroupIDFor computes the deterministic VolumeGroupID for a driver.
// Each driver produces IDs from (namespace, name) in a fixed format.
func VolumeGroupIDFor(driverType, namespace, name string) VolumeGroupID {
    switch driverType {
    case "csi-extension":
        return VolumeGroupID("csi-ext-" + namespace + "/" + name)
    default:
        return VolumeGroupID("noop-" + name)
    }
}
```

Then the resolve functions become:

```go
func resolveVolumeGroupID(ctx context.Context, driver drivers.StorageProvider,
    driverType string, vg soteriav1alpha1.VolumeGroupInfo) (drivers.VolumeGroupID, error) {
    vgID := drivers.VolumeGroupIDFor(driverType, vg.Namespace, vg.Name)
    if _, err := driver.GetVolumeGroup(ctx, vgID); err != nil {
        if errors.Is(err, drivers.ErrVolumeGroupNotFound) {
            return "", fmt.Errorf("VR/VGR not yet created by DRPlan reconciler for volume group %s: %w", vg.Name, err)
        }
        return "", fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
    }
    return vgID, nil
}
```

### Noop Driver ID Format

Check the noop driver's `CreateVolumeGroup` to confirm its ID format before implementing the shared helper. The noop driver in `pkg/drivers/noop/driver.go` produces IDs like `noop-<name>`. Verify this matches.

### What NOT to Change

- `StorageProvider` interface — no new methods
- `CreateVolumeGroup` or `GetVolumeGroup` implementations in any driver
- DRPlan reconciler's VR/VGR creation logic (that's Story 13.2)
- `ExecutionGroup.PVCResolver` field — it may still be used by other paths (check before removing from struct)
- Failover handler per-group flow (`StopReplication → StartVM`) — this changes in Story 13.6 to `SetSource → StartVM`; this story only touches the resolve functions

### PVCResolver Removal Scope

The `PVCResolver` parameter/usage is removed from the three resolve functions only. Check whether `PVCResolver` is used elsewhere before removing it from struct fields:

- `ExecutionGroup.PVCResolver` — check if any other code besides `resolveVolumeGroupID` reads it
- `DRPlanReconciler.PVCResolver` — check if health.go is the only consumer
- `WaveExecutor.PVCResolver` — check if `resolveVGID` is the only consumer in drexecution

**Do NOT remove PVCResolver from struct definitions** if other code still uses it. The struct field removal (if warranted) should be a follow-up or part of a later story.

### Fake Driver API for Tests

The fake driver (`pkg/drivers/fake/driver.go`) already supports:
- `drv.OnGetVolumeGroup(vgID)` — programs a reaction for GetVolumeGroup with specific ID
- `drv.OnGetVolumeGroup()` — programs a reaction matching any GetVolumeGroup call

Test updates replace `OnCreateVolumeGroup()` with `OnGetVolumeGroup("vg-1")` (or whatever VG ID the test uses). The `ReturnResult` API is identical:

```go
// Before:
drv.OnCreateVolumeGroup().ReturnResult(fake.Response{
    VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
})

// After:
drv.OnGetVolumeGroup("vg-1").ReturnResult(fake.Response{
    VolumeGroupInfo: &drivers.VolumeGroupInfo{ID: "vg-1", Name: "vg-db"},
})
```

Note: `OnGetVolumeGroup` takes a `VolumeGroupID` arg to match against, while `OnCreateVolumeGroup` matches by `VolumeGroupSpec`. The test VG IDs need to match what the new deterministic computation produces. If the test VG has `Name: "vg-db"` and `Namespace: "ns1"`, the ID would be `csi-ext-ns1/vg-db`. Update test VG IDs accordingly, or use `OnGetVolumeGroup()` (no args) for any-match.

### Error Handling for ErrVolumeGroupNotFound

The `ErrVolumeGroupNotFound` from `GetVolumeGroup` should be wrapped with a descriptive message explaining the most likely cause — the DRPlan reconciler hasn't created VR/VGR yet. This helps operators diagnose timing issues where execution starts before the DRPlan has reconciled VR/VGR.

```go
if errors.Is(err, drivers.ErrVolumeGroupNotFound) {
    return "", fmt.Errorf("VR/VGR not yet created by DRPlan reconciler for volume group %s: %w", vg.Name, err)
}
```

### Dependencies

- **Depends on Story 13.2** — DRPlan must create VR/VGR before execution can run. This story changes the resolve functions but the actual creation happens in 13.2.
- **Independent for implementation** — This story can be implemented and tested independently. The tests mock the driver, so `GetVolumeGroup` returning success simulates VR/VGR already existing.

### Previous Story Intelligence

**Story 12.3 (CreateVolumeGroup/DeleteVolumeGroup/GetVolumeGroup):**
- Established the `csi-ext-<namespace>/<name>` VolumeGroupID format
- `GetVolumeGroup` works via label-based lookup (`soteria.io/volume-group`) — returns `ErrVolumeGroupNotFound` when no CRs exist
- `parseVGID` extracts namespace and name from a VolumeGroupID

**Story 12.6 (Conformance Suite & Integration Testing):**
- Conformance adapter bridges csi-extension model to abstract StorageProvider
- `GetVolumeGroup` is well-tested in isolation

**Epic 12 patterns:**
- Review patches are common — expect 2-4 review findings per story
- Defer non-scope items explicitly in review findings section
- Coverage targets: 85%+ for modified packages

### Build Commands

```bash
make test
make lint-fix && make lint
```

### Project Structure Notes

- `pkg/engine/` — driver-agnostic execution logic, imports `pkg/drivers/` interface
- `pkg/controller/drexecution/` — DRExecution reconciler, uses `WaveExecutor` for VG resolution
- `pkg/controller/drplan/` — DRPlan reconciler, uses `Registry` + `PVCResolver` for health polling
- `pkg/drivers/` — interface + types; `pkg/drivers/fake/` for test double; `pkg/drivers/csiextension/` for real driver
- Adding `pkg/drivers/id.go` for the `VolumeGroupIDFor` helper follows the existing `pkg/drivers/` organization

### References

- [Source: `pkg/engine/failover.go` lines 85-112] — current `resolveVolumeGroupID` with `CreateVolumeGroup`
- [Source: `pkg/controller/drexecution/reconciler.go` lines 790-815] — current `resolveVGID` with `CreateVolumeGroup`
- [Source: `pkg/controller/drplan/health.go` lines 138-166] — current `resolveVolumeGroupID` with `CreateVolumeGroup`
- [Source: `pkg/drivers/csiextension/driver.go` lines 258-272] — `GetVolumeGroup` implementation
- [Source: `pkg/drivers/csiextension/driver.go` lines 60-62] — `vgIDFromNamespace` deterministic ID computation
- [Source: `pkg/drivers/fake/driver.go` lines 157-161] — `OnGetVolumeGroup` fake API
- [Source: `_bmad-output/planning-artifacts/epics.md` lines 2922-2967] — Epic 13 Story 13.5 specification

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
