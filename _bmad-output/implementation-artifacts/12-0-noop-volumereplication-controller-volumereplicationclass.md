# Story 12.0: Noop VolumeReplication Controller + volumeReplicationClass Field

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want a `volumeReplicationClass` field inside the VolumeReplicationDriver config and a noop controller that simulates CSI Addons VolumeReplication/VolumeGroupReplication operations,
So that on-cluster UAT can validate the full CSI Extension driver lifecycle (Stories 12.1–12.6) without real storage infrastructure.

## Background

### Current State

Story 12.0a restructured `DRPlanSpec.VolumeReplicationDriver` from a flat string to a `VolumeReplicationDriverConfig` struct with a `Type` field. The struct currently contains only `Type string`. The noop driver is the only admissible value. No CSI Addons types exist in the project yet.

### Why This Story

Epic 12 introduces a CSI Extension storage driver that translates StorageProvider calls into VolumeReplication and VolumeGroupReplication CRs (from the `kubernetes-csi-addons` project). On real clusters, a CSI Addons sidecar controller reconciles these CRs by talking to the storage backend. For development and UAT, no storage backend exists — so a noop controller is needed to simulate successful CSI Addons operations on-cluster.

The `volumeReplicationClass` field tells the CSI Extension driver which VolumeReplicationClass to stamp on created VR/VGR CRs. It is placed inside the `VolumeReplicationDriverConfig` struct because it is driver-specific configuration — only meaningful when `type: csi-extension`. The noop controller watches for CRs with `soteria-noop` as the class and reconciles their status to simulate success.

### Scope

Two deliverables:
1. **`VolumeReplicationClass` field on `VolumeReplicationDriverConfig`** — optional string with `+optional` marker. Programmatic validation makes it required when `type == "csi-extension"` and rejects it when `type == "noop"`. Immutable (covered by the whole-struct immutability from Story 12.0a).
2. **Noop VolumeReplication controller** — new package `pkg/controller/volumereplication/` that watches VolumeReplication + VolumeGroupReplication CRs, filtering by `spec.volumeReplicationClass == "soteria-noop"`, and sets status to simulate successful replication.

### Design — VolumeReplicationClass Placement

```yaml
# noop driver — volumeReplicationClass not specified (optional, forbidden for noop)
spec:
  volumeReplicationDriver:
    type: noop

# csi-extension driver — volumeReplicationClass required
spec:
  volumeReplicationDriver:
    type: csi-extension
    volumeReplicationClass: my-replication-class
```

This design:
- Enforces that `volumeReplicationClass` only makes sense for `csi-extension`
- Supports different replication classes without affecting the top-level spec
- Supports different drivers with additional parameters without affecting the top-level spec

## Acceptance Criteria

1. **AC1 — New field on VolumeReplicationDriverConfig:** `VolumeReplicationClass string` field added to `VolumeReplicationDriverConfig` in `types.go` with `+optional` marker. JSON tag: `volumeReplicationClass,omitempty`.

2. **AC2 — Contextual create validation:** `ValidateDRPlan` in `validation.go` enforces:
   - When `Type == "csi-extension"`: `VolumeReplicationClass` must be non-empty (`field.Required`)
   - When `Type == "noop"`: `VolumeReplicationClass` must be empty (`field.Forbidden`, "not applicable for noop driver")

3. **AC3 — Immutability covered by struct comparison:** The whole-struct immutability check from Story 12.0a covers `VolumeReplicationClass` automatically (Go struct equality). No additional immutability code needed — verify with a test.

4. **AC4 — CSI Addons dependency:** `github.com/csi-addons/kubernetes-csi-addons` added to `go.mod`. CSI Addons `replication.storage` v1alpha1 types registered in scheme (both in `cmd/soteria/main.go` and test suites).

5. **AC5 — Noop VolumeReplication controller:** New reconciler in `pkg/controller/volumereplication/reconciler.go` that:
   - Watches `VolumeReplication` CRs from `replication.storage.openshift.io/v1alpha1`
   - Filters to only reconcile CRs where `spec.volumeReplicationClass == "soteria-noop"`
   - Maps `spec.replicationState` to `status.state` (`"primary"` → `PrimaryState`, `"secondary"` → `SecondaryState`, `"resync"` → `SecondaryState`)
   - Sets status conditions: `Completed=True`, `Degraded=False`, `Resyncing=False`, `Validated=True`, `Replicating=True` (when primary) / `Replicating=False` (when secondary)
   - Sets `lastSyncTime`, `lastCompletionTime` to current time, `observedGeneration` to current generation

6. **AC6 — Noop VolumeGroupReplication controller:** Same reconciler also watches `VolumeGroupReplication` CRs and:
   - Filters to only reconcile CRs where `spec.volumeReplicationClassName == "soteria-noop"`
   - Sets embedded `VolumeReplicationStatus` fields identically to AC5
   - `PersistentVolumeClaimsRefList` left empty (noop has no real PVCs)

7. **AC7 — Controller registration:** Controller registered in the manager entry point (`cmd/soteria/` — verify exact path) with `SetupWithManager`. The noop class value (`"soteria-noop"`) is configurable via a constant. CSI Addons scheme added alongside existing scheme registrations.

8. **AC8 — doc.go and RBAC:** `pkg/controller/volumereplication/doc.go` with package description and `+kubebuilder:rbac` markers for `volumereplications` and `volumegroupreplications` resources in the `replication.storage.openshift.io` group (get, list, watch, update, patch for status subresource).

9. **AC9 — Sample CRD unchanged for noop:** `config/samples/soteria_v1alpha1_drplan.yaml` uses `type: noop` — no `volumeReplicationClass` needed. Add a comment showing the csi-extension example.

10. **AC10 — Codegen:** `make manifests generate` runs cleanly. DeepCopy and OpenAPI schemas reflect the new field.

11. **AC11 — No test fixture sweep needed:** Since `volumeReplicationClass` is optional and forbidden for noop, existing test fixtures with `VolumeReplicationDriverConfig{Type: "noop"}` remain valid. No sweep required.

12. **AC12 — Validation tests:** New tests in `validation_test.go`:
    - `TestValidateDRPlan_VolumeReplicationClass_ForbiddenForNoop` — non-empty value with `type: noop` rejected
    - `TestValidateDRPlan_VolumeReplicationClass_RequiredForCSIExtension` — empty value with `type: csi-extension` rejected (requires extending enum first or using a validation-only test that bypasses enum check)
    - `TestValidateDRPlanUpdate_VolumeReplicationClass_Immutable` — changed value rejected (covered by struct comparison, but explicit test confirms)

13. **AC13 — Noop controller unit tests:** Table-driven tests using fake `client.Client` (not envtest) in `pkg/controller/volumereplication/reconciler_test.go`:
    - VR with `soteria-noop` class + `primary` state → status Primary + conditions correct
    - VR with `soteria-noop` class + `secondary` state → status Secondary + conditions correct
    - VR with `soteria-noop` class + `resync` state → status Secondary + conditions correct
    - VR with different class (e.g. `ceph-rbd`) → ignored (no status update)
    - VR not found → no error (deleted between queue and reconcile)
    - VGR with `soteria-noop` class → status set correctly
    - VGR with different class → ignored
    - Idempotent re-reconciliation: already-correct status unchanged

    Note: Integration test suite (`test/integration/controller/suite_test.go`) is NOT modified in this story — the noop VR/VGR controller is validated via unit tests with fake client. CSI Addons CRDs are not installed in the envtest environment.

14. **AC14 — Console plugin updated:** `VolumeReplicationDriverConfig` TS interface in `types.ts` gains `volumeReplicationClass?: string`. No display changes — the console does not display `volumeReplicationClass` in this story.

15. **AC15 — Console plugin test fixtures unchanged:** Since the field is optional and noop doesn't use it, no console test fixture updates are needed.

16. **AC16 — All existing tests pass:** `make test` and integration tests pass with zero regressions.

## Tasks / Subtasks

- [x] Task 1: Add `VolumeReplicationClass` to VolumeReplicationDriverConfig (AC: #1, #10)
  - [x] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `VolumeReplicationClass string` to `VolumeReplicationDriverConfig` with `+optional` marker and JSON tag `volumeReplicationClass,omitempty`. Place after `Type`.
  - [x] 1.2 Run `make manifests generate` to regenerate DeepCopy and OpenAPI schemas

- [x] Task 2: Add contextual validation (AC: #2, #3)
  - [x] 2.1 In `ValidateDRPlan` (`validation.go`), after the type switch: when `type == "noop"` and `VolumeReplicationClass != ""`, append `field.Forbidden` error
  - [x] 2.2 When `type == "csi-extension"` and `VolumeReplicationClass == ""`, append `field.Required` error (note: the csi-extension enum value is added in Story 12.1, so this validation branch can be added now but won't be reachable until 12.1 extends the enum)
  - [x] 2.3 Verify immutability is covered by the existing struct comparison from Story 12.0a

- [x] Task 3: Add CSI Addons dependency (AC: #4)
  - [x] 3.1 Run `go get github.com/csi-addons/kubernetes-csi-addons` to add the module
  - [x] 3.2 Add CSI Addons replication v1alpha1 scheme registration in manager entry point (`cmd/soteria/` — verify exact file path before editing)
  - [x] 3.3 Run `go mod tidy`

- [x] Task 4: Create noop VolumeReplication controller (AC: #5, #6, #7, #8)
  - [x] 4.1 Create `pkg/controller/volumereplication/doc.go` with package doc and RBAC markers
  - [x] 4.2 Create `pkg/controller/volumereplication/reconciler.go` with `VolumeReplicationReconciler` struct
  - [x] 4.3 Implement `Reconcile` for VolumeReplication: fetch CR, skip if class != noop, set status
  - [x] 4.4 Implement `Reconcile` for VolumeGroupReplication: same pattern
  - [x] 4.5 Implement `SetupWithManager` watching both VR and VGR types
  - [x] 4.6 Register controller in `cmd/soteria/main.go`

- [x] Task 5: Update sample CRD (AC: #9)
  - [x] 5.1 In `config/samples/soteria_v1alpha1_drplan.yaml`, add comment showing csi-extension example with `volumeReplicationClass`

- [x] Task 6: Update console plugin types (AC: #14)
  - [x] 6.1 In `console-plugin/src/models/types.ts`, add `volumeReplicationClass?: string` to the `volumeReplicationDriver` interface

- [x] Task 7: Validation tests (AC: #12)
  - [x] 7.1 Add `TestValidateDRPlan_VolumeReplicationClass_ForbiddenForNoop`
  - [x] 7.2 Add `TestValidateDRPlan_VolumeReplicationClass_RequiredForCSIExtension` (may need to temporarily bypass enum to test csi-extension path)
  - [x] 7.3 Add `TestValidateDRPlanUpdate_VolumeReplicationClass_Immutable`

- [x] Task 8: Noop controller tests (AC: #13)
  - [x] 8.1 Create `pkg/controller/volumereplication/reconciler_test.go` with table-driven tests
  - [x] 8.2 Test VR reconciliation for primary/secondary/resync states
  - [x] 8.3 Test VR skip for non-noop class
  - [x] 8.4 Test VGR reconciliation for noop class
  - [x] 8.5 Test VGR skip for non-noop class
  - [x] 8.6 Test not-found (deleted CR) returns no error
  - [x] 8.7 Test idempotent re-reconciliation

- [x] Task 9: Verify all tests pass (AC: #16)
  - [x] 9.1 Run `make test` — all unit tests pass
  - [x] 9.2 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [x] 9.3 Run integration tests — no regressions

### Review Findings

- [x] [Review][Patch] Noop reconciler rewrites already-correct status on every reconcile [`pkg/controller/volumereplication/reconciler.go`]
  `applyNoopStatus()` always stamps fresh `LastCompletionTime`, `LastSyncTime`, and condition `LastTransitionTime` values, and both reconcile paths always call `Status().Update()` for `soteria-noop` objects. That makes the controller non-idempotent for already-correct resources, risks self-triggered reconcile churn from its own status updates, and does not satisfy AC13's "already-correct status unchanged" intent. The current idempotency test only compares `State` and condition count, so it would not catch this mutation pattern.

## Dev Notes

### Key Locations

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `VolumeReplicationClass` field to `VolumeReplicationDriverConfig` |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Contextual required/forbidden validation |
| `pkg/apis/soteria.io/v1alpha1/validation_test.go` | 3 new validation tests |
| `pkg/controller/volumereplication/doc.go` | Package doc + RBAC markers |
| `pkg/controller/volumereplication/reconciler.go` | Noop reconciler |
| `pkg/controller/volumereplication/reconciler_test.go` | Unit tests |
| `cmd/soteria/` (verify exact path) | CSI Addons scheme + controller registration |
| `config/samples/soteria_v1alpha1_drplan.yaml` | Comment with csi-extension example |
| `console-plugin/src/models/types.ts` | Add `volumeReplicationClass?: string` to interface |

### Field Design

```go
type VolumeReplicationDriverConfig struct {
	// Type is the registered driver that handles volume replication
	// operations for this plan's volumes. Immutable after creation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=noop
	Type string `json:"type"`

	// VolumeReplicationClass is the VolumeReplicationClass name stamped on
	// VolumeReplication and VolumeGroupReplication CRs created by the CSI
	// Extension driver. Only applicable when Type is csi-extension.
	// Immutable after creation (covered by struct-level immutability).
	// +optional
	VolumeReplicationClass string `json:"volumeReplicationClass,omitempty"`
}
```

**Why optional:** The field is driver-specific. For `noop`, there's no VolumeReplicationClass to stamp — specifying one would be confusing. For `csi-extension`, it's required because every VR/VGR CR needs a class. Programmatic validation enforces this contextual requirement.

**Why no test fixture sweep:** Unlike Story 11.1's `volumeReplicationDriver` (required for all plans), `volumeReplicationClass` is optional with `omitempty`. Existing noop fixtures don't specify it. Go's zero value (`""`) is valid for noop. No sweep needed.

### Contextual Validation Design

```go
drvPath := specPath.Child("volumeReplicationDriver")
switch plan.Spec.VolumeReplicationDriver.Type {
case "noop":
    if plan.Spec.VolumeReplicationDriver.VolumeReplicationClass != "" {
        allErrs = append(allErrs, field.Forbidden(
            drvPath.Child("volumeReplicationClass"),
            "not applicable for noop driver",
        ))
    }
case "csi-extension":
    if plan.Spec.VolumeReplicationDriver.VolumeReplicationClass == "" {
        allErrs = append(allErrs, field.Required(
            drvPath.Child("volumeReplicationClass"), ""))
    }
}
```

### CSI Addons Types Reference

**Import path:** `github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1`

**API group:** `replication.storage.openshift.io` / version `v1alpha1`

**VolumeReplication (VR):**
- `spec.volumeReplicationClass` — string, matches the plan's VolumeReplicationClass
- `spec.replicationState` — enum: `primary`, `secondary`, `resync`
- `spec.dataSource` — TypedLocalObjectReference (PVC)
- `status.state` — `Primary`, `Secondary`, `Unknown`
- `status.conditions` — `[]metav1.Condition` with types: `Completed`, `Degraded`, `Resyncing`, `Validated`, `Replicating`
- `status.lastSyncTime`, `lastCompletionTime`, `lastSyncBytes`, `lastSyncDuration`, `observedGeneration`

**VolumeGroupReplication (VGR):**
- `spec.volumeGroupReplicationClassName` — the VGR class (NOT used for filtering)
- `spec.volumeReplicationClassName` — the VR class name; noop controller filters on this field
- `spec.replicationState` — same enum as VR
- `spec.source.selector` — PVC label selector
- `status` — embeds `VolumeReplicationStatus` inline + `persistentVolumeClaimsRefList`

### Noop Controller Design

```go
const NoopVolumeReplicationClass = "soteria-noop"

type VolumeReplicationReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}
```

**Reconcile logic (VolumeReplication):**
1. Fetch VR by NamespacedName. If NotFound → return nil (deleted).
2. If `spec.volumeReplicationClass != NoopVolumeReplicationClass` → skip (return nil).
3. Compute desired state from `spec.replicationState`:
   - `"primary"` → `State: PrimaryState`, `Replicating=True`
   - `"secondary"` → `State: SecondaryState`, `Replicating=False`
   - `"resync"` → `State: SecondaryState`, `Replicating=False`
4. Build conditions: `Completed=True`, `Degraded=False`, `Resyncing=False`, `Validated=True`, `Replicating` per above.
5. Set `ObservedGeneration`, `LastCompletionTime`, `LastSyncTime` to now.
6. Status subresource update via `r.Status().Update(ctx, &vr)`.

**Reconcile logic (VolumeGroupReplication):** Same pattern, filtering on `spec.volumeReplicationClassName == NoopVolumeReplicationClass`.

**SetupWithManager:** Single reconciler struct, two setup methods:
- `SetupVolumeReplicationController(mgr) error` — `For(&VolumeReplication{})`
- `SetupVolumeGroupReplicationController(mgr) error` — `For(&VolumeGroupReplication{})`

### What NOT to Change

- `pkg/drivers/` — no driver changes needed; `volumeReplicationClass` is for external CRs, not the in-process driver registry
- `pkg/engine/` — engine does not use `volumeReplicationClass` (it uses `volumeReplicationDriver.Type` for driver resolution)
- `internal/preflight/` — preflight does not inspect `volumeReplicationClass`
- `pkg/controller/drplan/health.go` — health monitoring uses the driver type, not the class
- `pkg/apiserver/critical_fields.go` — `volumeReplicationDriver` is immutable; cannot trigger critical-field writes
- `pkg/admission/plugin.go` — admission plugin delegates to `ValidateDRPlan`/`ValidateDRPlanUpdate`; no admission-specific logic needed
- `pkg/registry/drplan/strategy.go` — table convertor columns are metadata/status only; no spec columns
- `test/integration/controller/suite_test.go` — noop VR/VGR controller is NOT registered here
- Existing test fixtures — `volumeReplicationClass` is optional for noop, no sweep needed

### Previous Story Intelligence

- **Story 12.0a (Restructure VolumeReplicationDriver):** Converted flat string to `VolumeReplicationDriverConfig{Type}`. This story adds the second field to that struct.
- **Story 11.1 (Add VolumeReplicationDriver to DRPlanSpec):** Established the required+immutable field pattern. This story's validation is contextual (required only for csi-extension) rather than universal.
- **Story 4.9 (Site Topology Fields):** Added `primarySite`/`secondarySite` as immutable fields — the original immutability validation pattern.

### Git Intelligence

Recent commits (all Epic 11 + 12.0a):
- Story 12.0a restructured VolumeReplicationDriver to nested config
- `062a1ab` Fix 9 TypeScript type-check errors in console-plugin
- `016174c` Story 11.5: Console UI displays volume replication driver
- `d89a977` Story 11.1: Add VolumeReplicationDriver to DRPlanSpec

### Build Commands

```bash
make manifests generate   # Regenerate CRDs/DeepCopy/OpenAPI after types.go changes
make test                 # All unit tests
make lint-fix && make lint # Code style
cd console-plugin && npm test   # Console plugin tests
```

### Project Structure Notes

- New package `pkg/controller/volumereplication/` follows existing `pkg/controller/drplan/` and `pkg/controller/drexecution/` patterns
- CSI Addons types are external — imported, not vendored
- RBAC markers on `doc.go` will be picked up by `make manifests` to generate cluster role rules

### References

- [Source: `pkg/apis/soteria.io/v1alpha1/types.go` — VolumeReplicationDriverConfig struct and DRPlanSpec]
- [Source: `pkg/apis/soteria.io/v1alpha1/validation.go` — ValidateDRPlan/ValidateDRPlanUpdate]
- [Source: `pkg/controller/drplan/doc.go` — controller doc.go pattern]
- [Source: `pkg/controller/drexecution/doc.go` — RBAC markers + doc.go pattern]
- [Source: `pkg/drivers/noop/driver.go` — noop pattern and registration]
- [Source: CSI Addons VolumeReplication types — `api/replication.storage/v1alpha1/volumereplication_types.go`]
- [Source: CSI Addons VolumeGroupReplication types — `api/replication.storage/v1alpha1/volumegroupreplication_types.go`]

## Dev Agent Record

### Agent Model Used

Claude Opus 4 (via Cursor)

### Debug Log References

- Initial dupl lint failure in reconciler.go (VR and VGR reconcile methods flagged as duplicate code) — resolved by extracting `applyNoopStatus` helper that operates on the shared `VolumeReplicationStatus` struct
- `vrAdapter` type caused `expected pointer` error from fake client's `Status().Update` — eliminated adapter pattern in favor of direct method calls with shared `applyNoopStatus` helper
- Deprecated `result.Requeue` in tests — replaced with `result.RequeueAfter != 0` check

### Completion Notes List

- `VolumeReplicationClass` optional string field added to `VolumeReplicationDriverConfig` with `+optional` marker and `omitempty` JSON tag
- Contextual validation: `field.Forbidden` when type=noop + class set; `field.Required` when type=csi-extension + class empty (csi-extension branch unreachable until Story 12.1 extends the Type enum)
- Immutability verified by existing struct-level comparison from Story 12.0a — explicit test confirms
- `github.com/csi-addons/kubernetes-csi-addons@v0.14.0` added to go.mod; replication v1alpha1 scheme registered in init()
- New `pkg/controller/volumereplication/` package with `VolumeReplicationReconciler` watching VR and VGR CRs filtered by `soteria-noop` class
- `applyNoopStatus` shared helper stamps state/conditions/timestamps onto the embedded `VolumeReplicationStatus` (avoids dupl lint between VR and VGR reconcile methods)
- Two separate controllers registered via `SetupVolumeReplicationController` and `SetupVolumeGroupReplicationController` using `reconcile.Func` wrapper
- RBAC markers on doc.go for `replication.storage.openshift.io` resources (volumereplications + volumegroupreplications)
- Sample CRD updated with csi-extension example comment
- Console plugin `DRPlanSpec.volumeReplicationDriver` interface extended with `volumeReplicationClass?: string`
- 3 new validation tests + 8 controller tests (with subtests: VR primary/secondary/resync, VGR primary/secondary, skip non-noop, not-found, idempotent)
- 85.7% coverage on new controller package, 10.0% on v1alpha1 types (unchanged), 0 lint issues
- All existing unit tests (20 packages), integration tests (10 suites), and console plugin tests (602) pass with zero regressions

### File List

**New files:**
- `pkg/controller/volumereplication/doc.go`
- `pkg/controller/volumereplication/reconciler.go`
- `pkg/controller/volumereplication/reconciler_test.go`

**Modified files:**
- `pkg/apis/soteria.io/v1alpha1/types.go` — VolumeReplicationClass field added to VolumeReplicationDriverConfig
- `pkg/apis/soteria.io/v1alpha1/validation.go` — contextual noop/csi-extension validation
- `pkg/apis/soteria.io/v1alpha1/validation_test.go` — 3 new tests
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — auto-generated
- `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` — auto-generated
- `cmd/soteria/main.go` — CSI Addons scheme registration + VR/VGR controller setup
- `config/samples/soteria_v1alpha1_drplan.yaml` — csi-extension example comment
- `config/rbac/role.yaml` — auto-generated RBAC rules for replication.storage.openshift.io
- `console-plugin/src/models/types.ts` — volumeReplicationClass optional field
- `go.mod` — github.com/csi-addons/kubernetes-csi-addons v0.14.0 added
- `go.sum` — updated

### Change Log

- 2026-05-14: Story 12.0 implemented — VolumeReplicationClass field + noop VolumeReplication controller + CSI Addons dependency + contextual validation + 11 new tests
