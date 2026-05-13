# Story 11.3: Preflight Reports Declared Driver

Status: ready-for-dev

## Story

As a platform engineer reviewing a DRPlan's preflight report,
I want the reported storage backend to reflect the plan's declared `volumeReplicationDriver`,
So that the preflight report is consistent with the driver that will actually be used during execution.

## Background

### Current State

The preflight pipeline resolves storage backends per-VM through `TypedStorageBackendResolver.ResolveBackends`:
1. For each VM, read the KubeVirt VM → extract PVC claim names
2. For each PVC, read the PVC → get `storageClassName`
3. For each storage class, call `SCLister.GetProvisioner` → get CSI provisioner
4. Verify `Registry.GetDriver(provisioner)` succeeds → store provisioner as backend

This produces a `map[string]string` of `"namespace/vmName" → provisioner`. `ComposeReport` stamps each `PreflightVM.StorageBackend` from this map, defaulting unknown VMs to `"unknown"`.

### What Changes

With the driver declared on the plan, the preflight no longer needs to *derive* the backend — it already knows it. The `StorageBackends` map in `CompositionInput` is replaced by the plan's `VolumeReplicationDriver` value. Every VM gets the same backend: the declared driver.

The `TypedStorageBackendResolver` is simplified from a resolver to a validator: it can optionally warn if a VM's PVC storage classes don't resolve to the declared driver, but this is a secondary concern.

### Scope

This story changes `CompositionInput`, `ComposeReport`, `collectWarnings`, and the reconciler's preflight composition call. The `TypedStorageBackendResolver` is simplified but not deleted — it may be useful for future validation. The `StorageBackendResolver` interface may be narrowed or removed.

## Acceptance Criteria

1. **AC1 — CompositionInput carries VolumeReplicationDriver:** `CompositionInput` gains a `VolumeReplicationDriver string` field, replacing the `StorageBackends map[string]string` field. The map field is removed.

2. **AC2 — ComposeReport stamps declared driver:** In `ComposeReport`, every `PreflightVM.StorageBackend` is set to `input.VolumeReplicationDriver`. No per-VM map lookup. No `"unknown"` or `"none"` fallbacks.

3. **AC3 — collectWarnings simplified:** The `collectWarnings` function no longer iterates `StorageBackends` for `"unknown"` or `"none"` values. The `backendUnknown` and `backendNone` constants in `checks.go` are removed if no longer referenced.

4. **AC4 — Reconciler simplified:** In `pkg/controller/drplan/reconciler.go`, the `composePreflightReport` method no longer calls `StorageResolver.ResolveBackends`. Instead, it passes `plan.Spec.VolumeReplicationDriver` directly into `CompositionInput.VolumeReplicationDriver`.

5. **AC5 — StorageBackendResolver dependency relaxed:** The `StorageResolver` field on `DRPlanReconciler` is no longer required for preflight composition. It may be retained for future validation or removed entirely if unused.

6. **AC6 — TypedStorageBackendResolver simplified or deprecated:** `TypedStorageBackendResolver.ResolveBackends` is either simplified to a validation-only method or the entire type is deprecated/removed. If retained, it warns when discovered SCs don't match the declared driver.

7. **AC7 — Preflight tests updated:** Tests in `internal/preflight/checks_test.go` and `internal/preflight/storage_test.go` are updated to reflect the new `CompositionInput` shape. New tests verify that `ComposeReport` stamps the declared driver for all VMs.

8. **AC8 — Reconciler preflight tests updated:** Tests in `pkg/controller/drplan/reconciler_test.go` that verify preflight composition are updated to pass `VolumeReplicationDriver` instead of `StorageBackends`.

9. **AC9 — No API type changes:** `PreflightVM.StorageBackend` field remains — its *value source* changes (from derived to declared), but the API shape is unchanged.

## Tasks / Subtasks

- [ ] Task 1: Update CompositionInput (AC: #1)
  - [ ] 1.1 In `internal/preflight/checks.go`, replace `StorageBackends map[string]string` with `VolumeReplicationDriver string` in `CompositionInput`

- [ ] Task 2: Update ComposeReport (AC: #2)
  - [ ] 2.1 In `ComposeReport`, set `PreflightVM.StorageBackend` to `input.VolumeReplicationDriver` for every VM
  - [ ] 2.2 Remove the per-VM map lookup and `backendUnknown` fallback

- [ ] Task 3: Simplify collectWarnings (AC: #3)
  - [ ] 3.1 Remove the `StorageBackends` iteration loop from `collectWarnings`
  - [ ] 3.2 Remove `backendUnknown` and `backendNone` constants if no longer referenced

- [ ] Task 4: Simplify reconciler preflight path (AC: #4, #5)
  - [ ] 4.1 In `pkg/controller/drplan/reconciler.go` `composePreflightReport`, remove the `StorageResolver.ResolveBackends` call
  - [ ] 4.2 Set `CompositionInput.VolumeReplicationDriver` to `plan.Spec.VolumeReplicationDriver`
  - [ ] 4.3 Evaluate whether `StorageResolver` field can be removed from `DRPlanReconciler`

- [ ] Task 5: Simplify or remove TypedStorageBackendResolver (AC: #6)
  - [ ] 5.1 Evaluate whether `TypedStorageBackendResolver` is used anywhere besides reconciler preflight
  - [ ] 5.2 If unused, delete `internal/preflight/storage.go` and `storage_test.go`; remove `StorageBackendResolver` interface
  - [ ] 5.3 If retained for future validation, simplify and document the change

- [ ] Task 6: Update tests (AC: #7, #8)
  - [ ] 6.1 Update `internal/preflight/checks_test.go` — pass `VolumeReplicationDriver` instead of `StorageBackends` map
  - [ ] 6.2 Update or remove `internal/preflight/storage_test.go` tests if resolver is deleted
  - [ ] 6.3 Update `pkg/controller/drplan/reconciler_test.go` preflight tests
  - [ ] 6.4 Add test: `TestComposeReport_StampsDeclaredDriver` — verify all VMs get the declared driver as StorageBackend
  - [ ] 6.5 Run `make test` — all tests pass
  - [ ] 6.6 Run `make lint-fix && make lint` — zero lint issues

## Dev Notes

### Key Locations

| File | Change |
|------|--------|
| `internal/preflight/checks.go` | `CompositionInput` field change, `ComposeReport` simplification, `collectWarnings` cleanup |
| `internal/preflight/storage.go` | Simplify or delete `TypedStorageBackendResolver` |
| `pkg/controller/drplan/reconciler.go` | Remove `ResolveBackends` call in `composePreflightReport` |
| `cmd/soteria/main.go` | May remove `storageResolver` wiring if type is deleted |
| `internal/preflight/checks_test.go` | Update `CompositionInput` construction |
| `internal/preflight/storage_test.go` | Update or delete |
| `pkg/controller/drplan/reconciler_test.go` | Update preflight test fixtures |

### Before and After

**Before (derived):**
```go
storageBackends, storageWarnings, err = r.StorageResolver.ResolveBackends(ctx, vms)
input := preflight.CompositionInput{
    StorageBackends: storageBackends,
    // ...
}
// ComposeReport: backend := input.StorageBackends["ns/vm"] or "unknown"
```

**After (declared):**
```go
input := preflight.CompositionInput{
    VolumeReplicationDriver: plan.Spec.VolumeReplicationDriver,
    // ...
}
// ComposeReport: backend := input.VolumeReplicationDriver (for all VMs)
```

### What NOT to Change

- `PreflightVM.StorageBackend` API field — shape unchanged, only value source changes
- `pkg/engine/executor.go` — Story 11.2
- `pkg/controller/drplan/health.go` — Story 11.4
- `console-plugin/` — Story 11.5
- SC homogeneity validation (Story 9.5 logic) — still validates PVCs within a VG use the same SC; this is orthogonal to which driver handles the VG

### Dependency

- **Depends on Story 11.1** — the `VolumeReplicationDriver` field must exist on `DRPlanSpec`.

### Previous Story Intelligence

- **Story 4.05 (Driver Registry Fallback & Preflight Convergence):** Originally wired preflight to use registry-based resolution. This story unwires it.
- **Story 5.1 (Replication Health Monitoring):** Established the pattern of wiring `SCLister` into reconciler and resolver. This story removes it from the preflight path.

### Build Commands

```bash
make test                 # All unit tests
make lint-fix && make lint # Code style
```
