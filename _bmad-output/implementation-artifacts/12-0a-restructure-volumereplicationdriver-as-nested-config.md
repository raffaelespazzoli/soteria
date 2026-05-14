# Story 12.0a: Restructure VolumeReplicationDriver as Nested Config

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want the `volumeReplicationDriver` field on DRPlanSpec restructured from a flat string to a nested configuration object,
So that driver-specific parameters (like `volumeReplicationClass`) can be scoped under the driver without polluting the top-level spec structure.

## Background

### Current State

Story 11.1 added `VolumeReplicationDriver string` as a flat field on `DRPlanSpec` with `+kubebuilder:validation:Enum=noop`. All downstream consumers (executor, health monitoring, preflight, console) read `plan.Spec.VolumeReplicationDriver` as a string. The field is required, immutable, and validated both at the OpenAPI level (kubebuilder Enum marker) and programmatically in `ValidateDRPlan`.

### Why This Changes

Epic 12 introduces a `csi-extension` driver that needs additional configuration — specifically a `volumeReplicationClass` to stamp on VolumeReplication/VolumeGroupReplication CRs. The original design placed `volumeReplicationClass` as a separate top-level field on DRPlanSpec. This has three problems:

1. **No scoping** — `volumeReplicationClass` only makes sense when the driver is `csi-extension`, but as a top-level field it's required for all plans including `noop` where it's meaningless.
2. **No extensibility** — Future drivers may need their own parameters. Each one would add another top-level field, creating a flat namespace collision risk.
3. **No discriminated union** — Kubernetes API conventions use nested structs (like `PersistentVolumeSpec.CSI`, `PersistentVolumeSpec.NFS`) to group driver-specific config under the driver selector.

### Design Decision

Restructure `VolumeReplicationDriver` from a flat string to a `VolumeReplicationDriverConfig` struct with a `Type` field that carries the driver enum. This story does ONLY the restructuring — no new fields are added. Story 12.0 then adds `VolumeReplicationClass` inside the struct.

## Acceptance Criteria

1. **AC1 — New struct type:** `VolumeReplicationDriverConfig` struct added to `types.go` with a single `Type string` field carrying `+kubebuilder:validation:Required` and `+kubebuilder:validation:Enum=noop` markers. JSON tag: `type`.

2. **AC2 — DRPlanSpec field change:** `DRPlanSpec.VolumeReplicationDriver` type changes from `string` to `VolumeReplicationDriverConfig`. JSON tag remains `volumeReplicationDriver`. The `+kubebuilder:validation:Required` marker stays on the field. The Enum marker moves to the `Type` field inside the struct.

3. **AC3 — Create validation updated:** `ValidateDRPlan` in `validation.go` references `.VolumeReplicationDriver.Type` instead of `.VolumeReplicationDriver`. The switch statement and `field.Required`/`field.NotSupported` checks use `specPath.Child("volumeReplicationDriver").Child("type")` for the field path.

4. **AC4 — Update validation updated:** `ValidateDRPlanUpdate` compares `newPlan.Spec.VolumeReplicationDriver != oldPlan.Spec.VolumeReplicationDriver` (struct equality via generated DeepEqual or field-by-field). The immutability error message references `volumeReplicationDriver` (the whole struct is immutable).

5. **AC5 — Executor updated:** All `plan.Spec.VolumeReplicationDriver` usages in `pkg/engine/executor.go` changed to `plan.Spec.VolumeReplicationDriver.Type`. This affects the `driverName` assignment in `Execute`, `ExecuteWaveHandler`, `ExecuteFromWave`, and `ExecuteRetry`.

6. **AC6 — DRPlan controller updated:** `pkg/controller/drplan/reconciler.go` usages of `plan.Spec.VolumeReplicationDriver` updated to `.VolumeReplicationDriver.Type` (health polling driverName and preflight CompositionInput). `pkg/controller/drplan/health.go` similarly updated.

7. **AC7 — DRExecution controller updated:** `pkg/controller/drexecution/reconciler.go` usage of `plan.Spec.VolumeReplicationDriver` updated to `.VolumeReplicationDriver.Type`.

8. **AC8 — Console plugin updated:** `DRPlanSpec` interface in `console-plugin/src/models/types.ts` changes `volumeReplicationDriver: string` to `volumeReplicationDriver: { type: string }`. `PlanConfiguration.tsx` changes `plan.spec?.volumeReplicationDriver` to `plan.spec?.volumeReplicationDriver?.type`.

9. **AC9 — Test fixture sweep (Go):** All Go test files that construct `DRPlan` or `DRPlanSpec` objects are updated. Typed fixtures: `VolumeReplicationDriver: "noop"` → `VolumeReplicationDriver: v1alpha1.VolumeReplicationDriverConfig{Type: "noop"}`. Unstructured map fixtures: `"volumeReplicationDriver": "noop"` → `"volumeReplicationDriver": map[string]any{"type": "noop"}`.

10. **AC10 — Test fixture sweep (Console):** All console-plugin test files that construct DRPlanSpec objects are updated: `volumeReplicationDriver: 'noop'` → `volumeReplicationDriver: { type: 'noop' }`.

11. **AC11 — Validation tests updated:** Existing validation tests in `validation_test.go` for `VolumeReplicationDriver` updated to use the struct construction. No new tests needed — this is a mechanical refactor of existing test cases.

12. **AC12 — Sample CRD updated:** `config/samples/soteria_v1alpha1_drplan.yaml` changes from `volumeReplicationDriver: noop` to nested YAML `volumeReplicationDriver:\n  type: noop`.

13. **AC13 — Codegen clean:** `make manifests generate` runs cleanly. DeepCopy methods generated for `VolumeReplicationDriverConfig`. OpenAPI schema reflects the field as an object with a `type` property instead of a string.

14. **AC14 — Doc updates:** `pkg/engine/doc.go` driver resolution description updated to reference `plan.Spec.VolumeReplicationDriver.Type`.

15. **AC15 — All existing tests pass:** `make test`, `make lint`, and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Create VolumeReplicationDriverConfig struct (AC: #1, #2, #13)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `VolumeReplicationDriverConfig` struct with `Type string` field and kubebuilder markers
  - [ ] 1.2 Change `DRPlanSpec.VolumeReplicationDriver` from `string` to `VolumeReplicationDriverConfig`
  - [ ] 1.3 Run `make manifests generate` to regenerate DeepCopy and OpenAPI schemas

- [ ] Task 2: Update validation (AC: #3, #4)
  - [ ] 2.1 In `ValidateDRPlan`, update `switch plan.Spec.VolumeReplicationDriver` to `switch plan.Spec.VolumeReplicationDriver.Type`
  - [ ] 2.2 Update field paths from `specPath.Child("volumeReplicationDriver")` to `specPath.Child("volumeReplicationDriver").Child("type")`
  - [ ] 2.3 In `ValidateDRPlanUpdate`, update immutability check to compare the full struct (Go struct comparison works since both fields are strings)

- [ ] Task 3: Update executor (AC: #5, #14)
  - [ ] 3.1 In `pkg/engine/executor.go`, update all `plan.Spec.VolumeReplicationDriver` to `plan.Spec.VolumeReplicationDriver.Type` (4 locations: Execute, ExecuteWaveHandler, ExecuteFromWave, ExecuteRetry)
  - [ ] 3.2 Update `pkg/engine/doc.go` driver resolution description

- [ ] Task 4: Update controllers (AC: #6, #7)
  - [ ] 4.1 In `pkg/controller/drplan/reconciler.go`, update `plan.Spec.VolumeReplicationDriver` to `.VolumeReplicationDriver.Type` (pollReplicationHealth call + CompositionInput assignment)
  - [ ] 4.2 In `pkg/controller/drplan/health.go`, update any direct references
  - [ ] 4.3 In `pkg/controller/drexecution/reconciler.go`, update `plan.Spec.VolumeReplicationDriver` to `.VolumeReplicationDriver.Type`

- [ ] Task 5: Update console plugin (AC: #8)
  - [ ] 5.1 In `console-plugin/src/models/types.ts`, change `volumeReplicationDriver: string` to `volumeReplicationDriver: { type: string }`
  - [ ] 5.2 In `console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx`, change `plan.spec?.volumeReplicationDriver` to `plan.spec?.volumeReplicationDriver?.type`

- [ ] Task 6: Go test fixture sweep (AC: #9, #11)
  - [ ] 6.1 Search all `*_test.go` files for `VolumeReplicationDriver: "noop"` — replace with `VolumeReplicationDriver: v1alpha1.VolumeReplicationDriverConfig{Type: "noop"}`
  - [ ] 6.2 Search for unstructured map-style specs with `"volumeReplicationDriver": "noop"` — replace with `"volumeReplicationDriver": map[string]any{"type": "noop"}`
  - [ ] 6.3 Update validation tests: struct construction in Required/Immutable/Unchanged/InvalidValue test cases

- [ ] Task 7: Console plugin test fixture sweep (AC: #10)
  - [ ] 7.1 Search all console-plugin test files for `volumeReplicationDriver: 'noop'` — replace with `volumeReplicationDriver: { type: 'noop' }`

- [ ] Task 8: Update sample CRD (AC: #12)
  - [ ] 8.1 In `config/samples/soteria_v1alpha1_drplan.yaml`, change `volumeReplicationDriver: noop` to nested YAML

- [ ] Task 9: Verify all tests pass (AC: #15)
  - [ ] 9.1 Run `make test` — all unit tests pass
  - [ ] 9.2 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 9.3 Run integration tests — no regressions
  - [ ] 9.4 Run `cd console-plugin && npm test` — all console tests pass

## Dev Notes

### Key Locations

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | New `VolumeReplicationDriverConfig` struct; field type change |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | `.VolumeReplicationDriver` → `.VolumeReplicationDriver.Type` (5 locations) |
| `pkg/apis/soteria.io/v1alpha1/validation_test.go` | Struct construction updates (~13 typed fixtures) |
| `pkg/engine/executor.go` | `.VolumeReplicationDriver` → `.VolumeReplicationDriver.Type` (4 locations) |
| `pkg/engine/doc.go` | Text update for driver resolution |
| `pkg/controller/drplan/reconciler.go` | `.VolumeReplicationDriver` → `.VolumeReplicationDriver.Type` (2 locations) |
| `pkg/controller/drplan/health.go` | `.VolumeReplicationDriver` → `.VolumeReplicationDriver.Type` |
| `pkg/controller/drexecution/reconciler.go` | `.VolumeReplicationDriver` → `.VolumeReplicationDriver.Type` (1 location) |
| `console-plugin/src/models/types.ts` | Interface shape change |
| `console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx` | Display access path change |
| `config/samples/soteria_v1alpha1_drplan.yaml` | Nested YAML |
| ~19 typed Go test files | `VolumeReplicationDriver: "noop"` → struct |
| ~3 unstructured Go test files | `"volumeReplicationDriver": "noop"` → nested map |
| ~17 console-plugin test files | `volumeReplicationDriver: 'noop'` → `{ type: 'noop' }` |

### Type Design

```go
// VolumeReplicationDriverConfig configures the volume replication driver and
// its associated parameters for a DRPlan. The Type field selects the driver
// implementation; additional fields carry driver-specific configuration.
type VolumeReplicationDriverConfig struct {
	// Type is the registered driver that handles volume replication
	// operations for this plan's volumes. Immutable after creation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=noop
	Type string `json:"type"`
}
```

```go
type DRPlanSpec struct {
	// VolumeReplicationDriver configures the volume replication driver and
	// its associated parameters. Immutable after creation.
	// +kubebuilder:validation:Required
	VolumeReplicationDriver VolumeReplicationDriverConfig `json:"volumeReplicationDriver"`

	// ... existing fields unchanged ...
}
```

### YAML Before/After

Before (current):
```yaml
spec:
  volumeReplicationDriver: noop
  maxConcurrentFailovers: 1
  primarySite: dc1
  secondarySite: dc2
```

After:
```yaml
spec:
  volumeReplicationDriver:
    type: noop
  maxConcurrentFailovers: 1
  primarySite: dc1
  secondarySite: dc2
```

### Validation Change Detail

```go
// Before:
switch plan.Spec.VolumeReplicationDriver {
case "":
    allErrs = append(allErrs, field.Required(specPath.Child("volumeReplicationDriver"), ""))
case "noop":
    // valid
default:
    allErrs = append(allErrs, field.NotSupported(...))
}

// After:
drvPath := specPath.Child("volumeReplicationDriver")
switch plan.Spec.VolumeReplicationDriver.Type {
case "":
    allErrs = append(allErrs, field.Required(drvPath.Child("type"), ""))
case "noop":
    // valid
default:
    allErrs = append(allErrs, field.NotSupported(drvPath.Child("type"), ...))
}
```

### Immutability Check

Go struct comparison works for `VolumeReplicationDriverConfig` because all fields are comparable types (strings). The check remains a single line:

```go
if newPlan.Spec.VolumeReplicationDriver != oldPlan.Spec.VolumeReplicationDriver {
    allErrs = append(allErrs, field.Forbidden(specPath.Child("volumeReplicationDriver"), "field is immutable"))
}
```

This implicitly covers both `Type` and any future fields added to the struct.

### Test Fixture Patterns

**Typed Go fixtures:**
```go
// Before:
DRPlanSpec{
    VolumeReplicationDriver: "noop",
    MaxConcurrentFailovers:  1,
    PrimarySite:             "dc1",
    SecondarySite:           "dc2",
}

// After:
DRPlanSpec{
    VolumeReplicationDriver: v1alpha1.VolumeReplicationDriverConfig{Type: "noop"},
    MaxConcurrentFailovers:  1,
    PrimarySite:             "dc1",
    SecondarySite:           "dc2",
}
```

**Unstructured map fixtures:**
```go
// Before:
"spec": map[string]any{
    "volumeReplicationDriver": "noop",
    ...
}

// After:
"spec": map[string]any{
    "volumeReplicationDriver": map[string]any{"type": "noop"},
    ...
}
```

**Console-plugin TS fixtures:**
```typescript
// Before:
spec: { maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b', volumeReplicationDriver: 'noop' }

// After:
spec: { maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b', volumeReplicationDriver: { type: 'noop' } }
```

### Known Go test file locations (typed `DRPlanSpec{}` constructions):

- `pkg/apis/soteria.io/v1alpha1/validation_test.go` (~13 fixtures)
- `pkg/admission/plugin_test.go` (~18 fixtures)
- `pkg/admission/drplan_validator_test.go` (~10 fixtures)
- `pkg/admission/drexecution_validator_test.go` (~9 fixtures)
- `pkg/admission/vm_validator_test.go` (~2 fixtures)
- `pkg/controller/drplan/reconciler_test.go`
- `pkg/controller/drexecution/reconciler_test.go` (~6 fixtures)
- `pkg/engine/executor_test.go`
- `pkg/engine/reprotect_test.go`
- `internal/preflight/checks_test.go` (~15 fixtures)
- `pkg/registry/drplan/strategy_test.go` (~3 fixtures)
- `test/integration/controller/suite_test.go`
- `test/integration/controller/drplan_test.go`
- `test/integration/controller/drplan_consistency_test.go`
- `test/integration/controller/drexecution_test.go` (~6 fixtures)
- `test/integration/replication/replication_test.go`
- `test/integration/storage/store_test.go`
- `test/integration/storage/watch_test.go`
- `test/integration/admission/vm_webhook_test.go` (~4 fixtures)

### Known Go test file locations (unstructured map specs):

- `test/integration/apiserver/admission_test.go` — `createDRPlan` helper (~2 fixtures)
- `test/integration/apiserver/apiserver_test.go` — inline specs (~3 fixtures)
- `test/integration/rbac/rbac_test.go` — `newDRPlan` helper (~1 fixture)

### Known console-plugin test file locations (~17 files, ~24 spec objects):

- `console-plugin/tests/components/PlanConfiguration.test.tsx`
- `console-plugin/tests/components/DRPlanDetailPage.test.tsx`
- `console-plugin/tests/components/DRLifecycleDiagram.test.tsx`
- `console-plugin/tests/components/DRPlanActions.test.tsx`
- `console-plugin/tests/components/KeyboardAccessibility.test.tsx`
- `console-plugin/tests/components/DiskDisagreementAlert.test.tsx`
- `console-plugin/tests/components/AlertBannerSystem.test.tsx`
- `console-plugin/tests/components/DRDashboardPage.test.tsx`
- `console-plugin/tests/components/Accessibility.test.tsx`
- `console-plugin/tests/components/TransitionProgressBanner.test.tsx`
- `console-plugin/tests/components/SiteDisagreementAlert.test.tsx`
- `console-plugin/tests/components/SiteDiscoverySection.test.tsx`
- `console-plugin/tests/components/DRDashboardToolbar.test.tsx`
- `console-plugin/tests/components/DRDashboard.test.tsx`
- `console-plugin/tests/components/WaveCompositionTree.test.tsx`
- `console-plugin/tests/utils/drPlanUtils.test.ts`
- `console-plugin/tests/hooks/usePreflightData.test.ts`

### What NOT to Change

- `pkg/drivers/` — driver registry uses string names, not the struct; no changes needed
- `pkg/apiserver/critical_fields.go` — `volumeReplicationDriver` is immutable; cannot trigger critical-field writes
- `pkg/admission/plugin.go` — delegates to `ValidateDRPlan`/`ValidateDRPlanUpdate`
- `internal/preflight/` — `CompositionInput.VolumeReplicationDriver` is already a string (the driver name); keep it as-is, just read from `.Type`
- `pkg/controller/drplan/reconciler.go` preflight input — `VolumeReplicationDriver` on `CompositionInput` stays `string`; the reconciler just reads `.Type` when populating it

### Previous Story Intelligence

- **Story 11.1 (Add VolumeReplicationDriver):** Original field addition. Fixture sweep pattern across ~22 Go files + ~17 console test files. This story follows the same sweep pattern but with struct construction instead of string assignment.
- **Story 8.1 (Remove waveLabel):** Largest fixture sweep (~54 files). Established the systematic search-and-replace approach.

### Build Commands

```bash
make manifests generate   # Regenerate CRDs/DeepCopy/OpenAPI after types.go changes
make test                 # All unit tests
make lint-fix && make lint # Code style
cd console-plugin && npm test   # Console plugin tests
```

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
