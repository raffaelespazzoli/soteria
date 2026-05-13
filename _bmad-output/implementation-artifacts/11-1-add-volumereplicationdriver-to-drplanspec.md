# Story 11.1: Add VolumeReplicationDriver Field to DRPlanSpec

Status: ready-for-dev

## Story

As a platform engineer,
I want to explicitly declare which volume replication driver a DRPlan uses,
So that driver selection is deterministic, visible in the CR, and validated at admission time rather than implicitly derived from storage classes at runtime.

## Background

### Current State

The storage driver is derived implicitly through a runtime chain: PVC → StorageClass → CSI provisioner → driver registry → StorageProvider. This was originally designed as FR21 — "driver selection is implicit; there is no StorageProviderConfig CRD." The orchestrator discovers which driver handles each VM's volumes by inspecting existing cluster state.

### Why This Changes

As Soteria adds real storage drivers beyond noop, the plan must explicitly declare which driver handles volume replication. This eliminates fragility (SC must be accessible at runtime), opacity (can't see the driver from the CR alone), and coupling (SC changes between preflight and execution cause silent behavior changes).

### Design Decision

Add `volumeReplicationDriver` as a required, immutable field on `DRPlanSpec`. The only admissible value for v1alpha1 is `noop`. The kubebuilder Enum marker enforces this at the API level. Future drivers extend the enum.

The noop driver is currently registered under the provisioner name `noop.soteria.io` in the driver registry. This story adds a second registration under the plan-level name `noop` so that `Registry.GetDriver("noop")` resolves correctly. The provisioner-name registration (`noop.soteria.io`) is preserved for backward compatibility with the preflight SC cross-check path (Story 11.3).

## Acceptance Criteria

1. **AC1 — New field on DRPlanSpec:** `VolumeReplicationDriver string` field added to `DRPlanSpec` in `pkg/apis/soteria.io/v1alpha1/types.go` with markers `+kubebuilder:validation:Required` and `+kubebuilder:validation:Enum=noop`. JSON tag: `volumeReplicationDriver`.

2. **AC2 — Immutability enforcement:** `ValidateDRPlanUpdate` in `pkg/apis/soteria.io/v1alpha1/validation.go` rejects updates that change `volumeReplicationDriver` with `field.Forbidden(..., "field is immutable")`, following the same pattern as `primarySite`/`secondarySite`.

3. **AC3 — Create validation:** `ValidateDRPlan` in `validation.go` rejects empty `volumeReplicationDriver` with `field.Required`.

4. **AC4 — Noop driver dual registration:** The noop driver in `pkg/drivers/noop/driver.go` registers under both `noop.soteria.io` (existing CSI provisioner name) and `noop` (plan-level driver name) via `init()`. `Registry.GetDriver("noop")` returns a valid `StorageProvider`.

5. **AC5 — Critical field detection unchanged:** `detectDRPlanCriticalFields` in `pkg/apiserver/critical_fields.go` is NOT modified — `volumeReplicationDriver` is immutable (cannot change), so it can never trigger a critical-field write.

6. **AC6 — Sample CRD updated:** `config/samples/soteria_v1alpha1_drplan.yaml` includes `volumeReplicationDriver: noop` with an explanatory comment.

7. **AC7 — Codegen:** `make manifests generate` runs cleanly. DeepCopy and OpenAPI schemas reflect the new field.

8. **AC8 — Test fixture sweep:** All test files that construct `DRPlan` or `DRPlanSpec` objects are updated to include `VolumeReplicationDriver: "noop"`. This is a mechanical sweep — no test logic changes.

9. **AC9 — Validation tests:** New tests in `pkg/apis/soteria.io/v1alpha1/validation_test.go`:
   - `TestValidateDRPlan_VolumeReplicationDriver_Required` — empty value rejected
   - `TestValidateDRPlanUpdate_VolumeReplicationDriver_Immutable` — changed value rejected
   - `TestValidateDRPlanUpdate_VolumeReplicationDriver_Unchanged` — same value accepted

10. **AC10 — Noop dual-registration test:** New test in `pkg/drivers/noop/registration_test.go` verifying `drivers.GetDriver("noop")` returns a non-nil provider.

## Tasks / Subtasks

- [ ] Task 1: Add field to DRPlanSpec (AC: #1, #7)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `VolumeReplicationDriver string` to `DRPlanSpec` with kubebuilder markers and JSON tag
  - [ ] 1.2 Run `make manifests generate` to regenerate DeepCopy and OpenAPI schemas

- [ ] Task 2: Add validation (AC: #2, #3)
  - [ ] 2.1 In `pkg/apis/soteria.io/v1alpha1/validation.go`, add `VolumeReplicationDriver` required check in `ValidateDRPlan`
  - [ ] 2.2 In `ValidateDRPlanUpdate`, add immutability check for `VolumeReplicationDriver`

- [ ] Task 3: Noop driver dual registration (AC: #4)
  - [ ] 3.1 In `pkg/drivers/noop/driver.go` `init()`, add `drivers.RegisterDriver("noop", ...)` alongside existing `noop.soteria.io` registration

- [ ] Task 4: Update sample CRD (AC: #6)
  - [ ] 4.1 In `config/samples/soteria_v1alpha1_drplan.yaml`, add `volumeReplicationDriver: noop`

- [ ] Task 5: Test fixture sweep (AC: #8)
  - [ ] 5.1 Search all `*.go` and `*_test.go` files for `DRPlanSpec{` or `DRPlan{` constructions
  - [ ] 5.2 Add `VolumeReplicationDriver: "noop"` to every fixture
  - [ ] 5.3 Verify `make test` passes with all fixtures updated

- [ ] Task 6: Validation tests (AC: #9, #10)
  - [ ] 6.1 Add `TestValidateDRPlan_VolumeReplicationDriver_Required` to validation_test.go
  - [ ] 6.2 Add `TestValidateDRPlanUpdate_VolumeReplicationDriver_Immutable` to validation_test.go
  - [ ] 6.3 Add `TestValidateDRPlanUpdate_VolumeReplicationDriver_Unchanged` to validation_test.go
  - [ ] 6.4 Add `TestGetDriver_PlanLevelName` to `pkg/drivers/noop/registration_test.go`

- [ ] Task 7: Verify all tests pass (AC: #7)
  - [ ] 7.1 Run `make test` — all unit tests pass
  - [ ] 7.2 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 7.3 Run integration tests — no regressions

## Dev Notes

### Key Locations

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `VolumeReplicationDriver` field to `DRPlanSpec` |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Required + immutable validation |
| `pkg/drivers/noop/driver.go` | Dual registration (`noop` + `noop.soteria.io`) |
| `config/samples/soteria_v1alpha1_drplan.yaml` | Add field to sample |
| ~15+ test files | Fixture sweep |

### Field Design

```go
type DRPlanSpec struct {
    // VolumeReplicationDriver is the registered driver that handles volume
    // replication operations for this plan's volumes. Immutable after creation.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Enum=noop
    VolumeReplicationDriver string `json:"volumeReplicationDriver"`

    // ... existing fields unchanged ...
}
```

### Noop Driver Registration

The noop driver currently registers as `noop.soteria.io` (CSI provisioner name convention). This story adds a second `RegisterDriver("noop", ...)` call. The registry allows different names to map to different factories — each call creates a new instance, which is fine for the stateless noop driver.

### Test Fixture Sweep Pattern

Same pattern as Story 8.1 (removed `waveLabel` — ~54 files) and Story 10.4 (removed `ActiveExecution` — ~14 files). Search for all `DRPlanSpec{` and `DRPlan{` constructions. Every fixture needs `VolumeReplicationDriver: "noop"` since the field is required.

Known test file locations (non-exhaustive):
- `pkg/apis/soteria.io/v1alpha1/validation_test.go`
- `pkg/controller/drplan/reconciler_test.go`
- `pkg/controller/drexecution/reconciler_test.go`
- `pkg/engine/executor_test.go`
- `pkg/apiserver/critical_fields_test.go`
- `pkg/apiserver/plugin_test.go`
- `internal/preflight/*_test.go`
- `test/integration/controller/*_test.go`

### What NOT to Change

- `pkg/apiserver/critical_fields.go` — `volumeReplicationDriver` is immutable; it cannot trigger a critical-field write
- `pkg/engine/executor.go` — driver resolution changes are Story 11.2
- `internal/preflight/storage.go` — preflight changes are Story 11.3
- `pkg/controller/drplan/health.go` — health changes are Story 11.4
- `console-plugin/` — console changes are Story 11.5

### Previous Story Intelligence

- **Story 8.1 (Remove waveLabel):** Established the fixture sweep pattern for adding/removing spec fields. ~54 files modified.
- **Story 10.4 (Remove ActiveExecution):** Same pattern for status fields. ~14 files.
- **Story 4.9 (Site Topology Fields):** Added `primarySite`/`secondarySite` as immutable fields with the same validation pattern this story follows.

### Build Commands

```bash
make manifests generate   # Regenerate CRDs/DeepCopy
make test                 # All unit tests
make lint-fix && make lint # Code style
```
