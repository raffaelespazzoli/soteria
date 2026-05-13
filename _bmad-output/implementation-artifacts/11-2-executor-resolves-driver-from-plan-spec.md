# Story 11.2: Executor Resolves Driver from Plan Spec

Status: done

## Story

As a DR execution engine,
I want to resolve the storage driver from the plan's declared `volumeReplicationDriver` field,
So that driver resolution is deterministic and no longer requires runtime StorageClass API lookups during execution.

## Background

### Current State

The `WaveExecutor` resolves drivers via a multi-step runtime chain:
1. `resolveDrivers` → per-VolumeGroup via `ResolveVGDriver`
2. `ResolveVGDriver` → `resolveVGStorageClass` (reads KubeVirt VMs → PVCs → storage class names)
3. `resolveVGStorageClass` → `Registry.GetDriverForPVC` (SC name → provisioner → driver)

This requires `SCLister`, `CoreClient`, and `Client` dependencies on the executor just for driver resolution. With the explicit `volumeReplicationDriver` field on DRPlanSpec (Story 11.1), all of this collapses to a single `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)` call.

### Scope

This story simplifies the executor's driver resolution path. The `resolveDriver` (chunk-level) and `ResolveVGDriver` (VG-level) methods both change to use the declared driver. The `resolveVGStorageClass` helper and `SCLister` dependency are removed from the executor.

The `SCLister` field is removed from `WaveExecutor`. The `CoreClient` field is retained because it's used for PVC resolution in the handler path (not just driver resolution).

## Acceptance Criteria

1. **AC1 — resolveDrivers uses declared driver:** `resolveDrivers` resolves a single `StorageProvider` via `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)` and assigns it to all VolumeGroups in the chunk. No per-VG SC iteration.

2. **AC2 — resolveDriver uses declared driver:** The chunk-level `resolveDriver` fallback also uses `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)` instead of per-PVC SC lookup.

3. **AC3 — ResolveVGDriver simplified:** `ResolveVGDriver` uses `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)` directly. The `resolveVGStorageClass` helper is deleted.

4. **AC4 — VolumeReplicationDriver accessible to executor:** `WaveExecutor` gains a `VolumeReplicationDriver string` field, set from `plan.Spec.VolumeReplicationDriver` by the reconciler at construction time. Alternatively, the plan is passed through `ExecuteInput` (which already carries `Plan`), and the executor reads it from there.

5. **AC5 — SCLister removed from WaveExecutor:** The `SCLister drivers.StorageClassLister` field is removed from `WaveExecutor`. All construction sites (reconciler, integration tests) are updated.

6. **AC6 — resolveVGStorageClass deleted:** The `resolveVGStorageClass` method on `WaveExecutor` is deleted entirely. Its functionality is no longer needed.

7. **AC7 — doc.go updated:** The executor section in `pkg/engine/doc.go` is updated to reflect that the driver is resolved from `plan.Spec.VolumeReplicationDriver` rather than from PVC storage classes.

8. **AC8 — Executor tests updated:** All executor tests that set up `SCLister`, mock storage classes, or assert on SC resolution are updated to reflect the simplified path. Tests verify that the executor uses the plan-level driver name.

9. **AC9 — Integration tests updated:** `test/integration/controller/suite_test.go` WaveExecutor construction updated to remove `SCLister` field. Integration tests pass.

10. **AC10 — No API/schema changes:** No changes to CRD types or stored data format. This is a pure internal simplification.

## Tasks / Subtasks

- [x] Task 1: Pass VolumeReplicationDriver to executor (AC: #4)
  - [x] 1.1 Decide on mechanism: read from `input.Plan.Spec.VolumeReplicationDriver` in `Execute`/`ExecuteWaveHandler`/`ExecuteRetry`, or add a field to `WaveExecutor`. Prefer reading from `input.Plan` since it's already available.

- [x] Task 2: Simplify resolveDrivers and resolveDriver (AC: #1, #2)
  - [x] 2.1 In `resolveDrivers`, replace per-VG `ResolveVGDriver` iteration with single `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)`, assign same driver to all VGs
  - [x] 2.2 In `resolveDriver`, replace per-PVC SC lookup with `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)`

- [x] Task 3: Simplify ResolveVGDriver (AC: #3)
  - [x] 3.1 Replace `resolveVGStorageClass` + `GetDriverForPVC` with `Registry.GetDriver(plan.Spec.VolumeReplicationDriver)`

- [x] Task 4: Delete resolveVGStorageClass (AC: #6)
  - [x] 4.1 Delete `resolveVGStorageClass` method from `executor.go`
  - [x] 4.2 Delete the chunk-level `resolveChunkStorageClass` if it exists and is only used for driver resolution

- [x] Task 5: Remove SCLister from WaveExecutor (AC: #5)
  - [x] 5.1 Remove `SCLister drivers.StorageClassLister` field from `WaveExecutor` struct
  - [x] 5.2 Update reconciler construction of `WaveExecutor` (remove SCLister assignment)
  - [x] 5.3 Update integration test construction (`suite_test.go`)

- [x] Task 6: Update doc.go (AC: #7)
  - [x] 6.1 Update executor documentation to describe plan-level driver resolution

- [x] Task 7: Update tests (AC: #8, #9)
  - [x] 7.1 Update executor unit tests — remove SC-related mocking, verify driver resolved from plan spec
  - [x] 7.2 Update integration tests — remove SCLister from WaveExecutor construction
  - [x] 7.3 Run `make test` — all tests pass
  - [x] 7.4 Run `make lint-fix && make lint` — zero lint issues (1 pre-existing goconst from 11.1)

### Review Findings

- [x] [Review][Patch] Executor tests do not verify that a non-default `plan.Spec.VolumeReplicationDriver` value is actually used during driver resolution [`pkg/engine/executor_test.go:114`] — fixed: added `TestWaveExecutor_Execute_ResolvesNamedDriver` and `TestWaveExecutor_Execute_FailsForUnregisteredDriver`

## Dev Agent Record

### Implementation Plan

Read `plan.Spec.VolumeReplicationDriver` from the `ExecuteInput.Plan` already available in Execute/ExecuteWaveHandler/ExecuteFromWave/ExecuteRetry. Thread `driverName string` parameter through executeWave → executeGroup → resolveDrivers. All driver resolution collapses to `Registry.GetDriver(driverName)`.

### Debug Log

No issues encountered. Compilation, unit tests, integration tests, and lint all passed on first attempt.

### Completion Notes

- **resolveDrivers** simplified to single `Registry.GetDriver(driverName)` call, assigning the same driver to all VGs
- **resolveDriver** deleted entirely (was only called from resolveDrivers for the no-VG fallback case)
- **resolveChunkStorageClass** deleted entirely (was only called from resolveDriver)
- **resolveVGStorageClass** deleted entirely (was only called from ResolveVGDriver)
- **ResolveVGDriver** simplified to `Registry.GetDriver(driverName)` — signature changed from `(ctx, vg VolumeGroupInfo)` to `(ctx, driverName string)`
- **SCLister** removed from WaveExecutor struct, cmd/soteria/main.go, and test/integration/controller/suite_test.go
- **doc.go** updated with driver resolution description
- **Unused imports** removed from executor.go (kubevirtv1, types)
- All existing tests pass unchanged — the noop driver registered under "noop" plan-level name resolves correctly
- Engine coverage improved from 83.4% to 86.7% due to removal of dead code paths
- Net code deletion: ~170 lines removed from executor.go (4 methods deleted), 0 new test files needed

## File List

| File | Change |
|------|--------|
| `pkg/engine/executor.go` | Simplified resolveDrivers/ResolveVGDriver, deleted resolveDriver/resolveChunkStorageClass/resolveVGStorageClass, removed SCLister field, threaded driverName through executeWave/executeGroup |
| `pkg/engine/executor_test.go` | Updated makeKubevirtVMs comment |
| `pkg/engine/doc.go` | Added driver resolution description to wave executor section |
| `pkg/controller/drexecution/reconciler.go` | Updated ResolveVGDriver call to pass driverName |
| `cmd/soteria/main.go` | Removed SCLister from WaveExecutor construction |
| `test/integration/controller/suite_test.go` | Removed SCLister from WaveExecutor construction |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | Status updated |
| `_bmad-output/implementation-artifacts/11-2-executor-resolves-driver-from-plan-spec.md` | Story file updated |

## Change Log

- Story 11.2 implementation: Executor resolves driver from plan spec (Date: 2026-05-13)

## Dev Notes

### Key Locations

| File | Function | Change |
|------|----------|--------|
| `pkg/engine/executor.go:158-171` | `WaveExecutor` struct | Remove `SCLister` field |
| `pkg/engine/executor.go:811-838` | `resolveDrivers` | Use `Registry.GetDriver(driverName)` |
| `pkg/engine/executor.go:840-854` | `ResolveVGDriver` | Use `Registry.GetDriver(driverName)` |
| `pkg/engine/executor.go:856-920` | `resolveVGStorageClass` | Delete entirely |
| `pkg/engine/executor.go:740-809` | `resolveChunkStorageClass` | Evaluate for deletion |
| `pkg/engine/doc.go` | Package doc | Update driver resolution description |
| `test/integration/controller/suite_test.go:145-152` | WaveExecutor construction | Remove `SCLister` |
| `pkg/engine/executor_test.go` | Various tests | Remove SC mocking |

### Design Decision: Read from input.Plan

The `ExecuteInput` struct already carries `Plan *soteriav1alpha1.DRPlan`. Rather than adding a new field to `WaveExecutor`, the executor reads `input.Plan.Spec.VolumeReplicationDriver` directly. This avoids stale-field bugs and keeps the executor stateless with respect to plan config.

The `resolveDrivers`/`resolveDriver` methods need access to the plan's driver name. Thread it through as a parameter or store it in a local variable at the top of `Execute`/`ExecuteWaveHandler`.

### What NOT to Change

- `Registry.GetDriverForPVC` — still exists in the registry package for preflight (Story 11.3)
- `StorageClassLister` interface — still exists for preflight
- `KubeStorageClassLister` — still used by preflight and health (until Story 11.4)
- `pkg/controller/drplan/health.go` — health monitoring changes are Story 11.4
- `internal/preflight/storage.go` — preflight changes are Story 11.3
- Handler internals (`FailoverHandler`, `StepHandler`) — they receive a `StorageProvider` from the executor, they don't resolve it themselves

### Dependency

- **Depends on Story 11.1** — the `VolumeReplicationDriver` field must exist on `DRPlanSpec` and the noop driver must be registered under the name `"noop"`.

### Previous Story Intelligence

- **Story 5.7 (Driver Interface Simplification):** Established the pattern of simplifying driver-related code paths in the executor. 18 prod files, 10 test files.
- **Story 10.8 (Sequential Chunk Execution):** Recent executor modification — surgical changes with focused test updates.

### Build Commands

```bash
make test                 # All unit tests
make lint-fix && make lint # Code style
```
