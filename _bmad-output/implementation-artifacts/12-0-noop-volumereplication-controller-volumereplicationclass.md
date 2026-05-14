# Story 12.0: Noop VolumeReplication Controller + volumeReplicationClass Field

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want a `volumeReplicationClass` field on DRPlanSpec and a noop controller that simulates CSI Addons VolumeReplication/VolumeGroupReplication operations,
So that on-cluster UAT can validate the full CSI Extension driver lifecycle (Stories 12.1–12.6) without real storage infrastructure.

## Background

### Current State

The DRPlanSpec has `volumeReplicationDriver` (enum: `noop`, required, immutable) added in Story 11.1. The StorageProvider 6-method interface in `pkg/drivers/interface.go` abstracts volume replication. The noop driver (`pkg/drivers/noop/driver.go`) implements all 6 methods as no-ops. No CSI Addons types exist in the project yet.

### Why This Story

Epic 12 introduces a CSI Extension storage driver that translates StorageProvider calls into VolumeReplication and VolumeGroupReplication CRs (from the `kubernetes-csi-addons` project). On real clusters, a CSI Addons sidecar controller reconciles these CRs by talking to the storage backend. For development and UAT, no storage backend exists — so a noop controller is needed to simulate successful CSI Addons operations on-cluster.

The `volumeReplicationClass` field tells the CSI Extension driver which VolumeReplicationClass to stamp on created VR/VGR CRs. The noop controller watches for CRs with `soteria-noop` as the class and reconciles their status to simulate success.

### Scope

Two deliverables:
1. **`volumeReplicationClass` field on DRPlanSpec** — free-form string (not enum), required, immutable. Follows the same validation pattern as `primarySite`/`secondarySite`.
2. **Noop VolumeReplication controller** — new package `pkg/controller/volumereplication/` that watches VolumeReplication + VolumeGroupReplication CRs, filtering by `spec.volumeReplicationClass == "soteria-noop"`, and sets status to simulate successful replication.

## Acceptance Criteria

1. **AC1 — New field on DRPlanSpec:** `VolumeReplicationClass string` field added to `DRPlanSpec` in `pkg/apis/soteria.io/v1alpha1/types.go` with `+kubebuilder:validation:Required` marker. JSON tag: `volumeReplicationClass`. Free-form string — no Enum marker.

2. **AC2 — Immutability enforcement:** `ValidateDRPlanUpdate` in `pkg/apis/soteria.io/v1alpha1/validation.go` rejects updates that change `volumeReplicationClass` with `field.Forbidden(..., "field is immutable")`, following the same pattern as `primarySite`/`secondarySite`/`volumeReplicationDriver`.

3. **AC3 — Create validation:** `ValidateDRPlan` in `validation.go` rejects empty `volumeReplicationClass` with `field.Required`.

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

7. **AC7 — Controller registration:** Controller registered in the manager entry point (`cmd/soteria/` — verify exact path; file may not be committed yet) with `SetupWithManager`. The noop class value (`"soteria-noop"`) is configurable via a constant. CSI Addons scheme added alongside existing scheme registrations.

8. **AC8 — doc.go and RBAC:** `pkg/controller/volumereplication/doc.go` with package description and `+kubebuilder:rbac` markers for `volumereplications` and `volumegroupreplications` resources in the `replication.storage.openshift.io` group (get, list, watch, update, patch for status subresource).

9. **AC9 — Sample CRD updated:** `config/samples/soteria_v1alpha1_drplan.yaml` includes `volumeReplicationClass: soteria-noop`.

10. **AC10 — Codegen:** `make manifests generate` runs cleanly. DeepCopy and OpenAPI schemas reflect the new field.

11. **AC11 — Test fixture sweep:** All test files that construct `DRPlan` or `DRPlanSpec` objects are updated to include `VolumeReplicationClass: "soteria-noop"`. Mechanical sweep — no test logic changes.

12. **AC12 — Validation tests:** New tests in `validation_test.go`:
    - `TestValidateDRPlan_VolumeReplicationClass_Required` — empty value rejected
    - `TestValidateDRPlanUpdate_VolumeReplicationClass_Immutable` — changed value rejected
    - `TestValidateDRPlanUpdate_VolumeReplicationClass_Unchanged` — same value accepted

13. **AC13 — Noop controller unit tests:** Table-driven tests using fake `client.Client` (not envtest) in `pkg/controller/volumereplication/reconciler_test.go`:
    - VR with `soteria-noop` class + `primary` state → status Primary + conditions correct
    - VR with `soteria-noop` class + `secondary` state → status Secondary + conditions correct
    - VR with `soteria-noop` class + `resync` state → status Secondary + conditions correct
    - VR with different class (e.g. `ceph-rbd`) → ignored (no status update)
    - VR not found → no error (deleted between queue and reconcile)
    - VGR with `soteria-noop` class → status set correctly
    - VGR with different class → ignored
    - Idempotent re-reconciliation: already-correct status unchanged

    Note: Integration test suite (`test/integration/controller/suite_test.go`) is NOT modified in this story — the noop VR/VGR controller is validated via unit tests with fake client. CSI Addons CRDs are not installed in the envtest environment. Future stories may add integration coverage.

14. **AC14 — All existing tests pass:** `make test` and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Add `volumeReplicationClass` to DRPlanSpec (AC: #1, #10)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `VolumeReplicationClass string` to `DRPlanSpec` with `+kubebuilder:validation:Required` marker and JSON tag `volumeReplicationClass`. Place after `VolumeReplicationDriver`.
  - [ ] 1.2 Run `make manifests generate` to regenerate DeepCopy and OpenAPI schemas

- [ ] Task 2: Add validation (AC: #2, #3)
  - [ ] 2.1 In `ValidateDRPlan` (`validation.go`), add required check for `volumeReplicationClass` (empty → `field.Required`)
  - [ ] 2.2 In `ValidateDRPlanUpdate`, add immutability check for `volumeReplicationClass` (changed → `field.Forbidden`)

- [ ] Task 3: Add CSI Addons dependency (AC: #4)
  - [ ] 3.1 Run `go get github.com/csi-addons/kubernetes-csi-addons` to add the module
  - [ ] 3.2 Add CSI Addons replication v1alpha1 scheme registration in manager entry point (`cmd/soteria/` — verify exact file path before editing)
  - [ ] 3.3 Run `go mod tidy`

- [ ] Task 4: Create noop VolumeReplication controller (AC: #5, #6, #7, #8)
  - [ ] 4.1 Create `pkg/controller/volumereplication/doc.go` with package doc and RBAC markers
  - [ ] 4.2 Create `pkg/controller/volumereplication/reconciler.go` with `VolumeReplicationReconciler` struct
  - [ ] 4.3 Implement `Reconcile` for VolumeReplication: fetch CR, skip if class != noop, set status
  - [ ] 4.4 Implement `Reconcile` for VolumeGroupReplication: same pattern
  - [ ] 4.5 Implement `SetupWithManager` watching both VR and VGR types
  - [ ] 4.6 Register controller in `cmd/soteria/main.go`

- [ ] Task 5: Update sample CRD (AC: #9)
  - [ ] 5.1 In `config/samples/soteria_v1alpha1_drplan.yaml`, add `volumeReplicationClass: soteria-noop`

- [ ] Task 6: Test fixture sweep (AC: #11)
  - [ ] 6.1 Search all `*.go` and `*_test.go` files for `DRPlanSpec{` or `DRPlan{` typed constructions — add `VolumeReplicationClass: "soteria-noop"` to every fixture
  - [ ] 6.2 Search for **unstructured** map-style DRPlan specs (e.g., `"spec": map[string]any{`) — these also need `"volumeReplicationClass": "soteria-noop"`. Critical files: `test/integration/apiserver/admission_test.go` (`createDRPlan` helper), `test/integration/apiserver/apiserver_test.go` (inline specs), `test/integration/rbac/rbac_test.go` (`newDRPlan` helper)
  - [ ] 6.3 Update console-plugin `DRPlanSpec` interface in `console-plugin/src/models/types.ts` — add `volumeReplicationClass: string`
  - [ ] 6.4 Search all console-plugin test files for DRPlanSpec object constructions and add `volumeReplicationClass: 'soteria-noop'`

- [ ] Task 7: Validation tests (AC: #12)
  - [ ] 7.1 Add `TestValidateDRPlan_VolumeReplicationClass_Required`
  - [ ] 7.2 Add `TestValidateDRPlanUpdate_VolumeReplicationClass_Immutable`
  - [ ] 7.3 Add `TestValidateDRPlanUpdate_VolumeReplicationClass_Unchanged`

- [ ] Task 8: Noop controller tests (AC: #13)
  - [ ] 8.1 Create `pkg/controller/volumereplication/reconciler_test.go` with table-driven tests
  - [ ] 8.2 Test VR reconciliation for primary/secondary/resync states
  - [ ] 8.3 Test VR skip for non-noop class
  - [ ] 8.4 Test VGR reconciliation for noop class
  - [ ] 8.5 Test VGR skip for non-noop class
  - [ ] 8.6 Test not-found (deleted CR) returns no error
  - [ ] 8.7 Test idempotent re-reconciliation

- [ ] Task 9: Verify all tests pass (AC: #14)
  - [ ] 9.1 Run `make test` — all unit tests pass
  - [ ] 9.2 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 9.3 Run integration tests — no regressions

## Dev Notes

### Key Locations

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `VolumeReplicationClass` field to `DRPlanSpec` |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Required + immutable validation |
| `pkg/apis/soteria.io/v1alpha1/validation_test.go` | 3 new validation tests |
| `pkg/controller/volumereplication/doc.go` | Package doc + RBAC markers |
| `pkg/controller/volumereplication/reconciler.go` | Noop reconciler |
| `pkg/controller/volumereplication/reconciler_test.go` | Unit tests |
| `cmd/soteria/` (verify exact path) | CSI Addons scheme + controller registration |
| `config/samples/soteria_v1alpha1_drplan.yaml` | Add field to sample |
| `console-plugin/src/models/types.ts` | Add `volumeReplicationClass: string` to `DRPlanSpec` |
| ~19 typed Go test files + ~3 unstructured Go test files + ~17 console-plugin test files | Fixture sweep |

### Field Design

```go
type DRPlanSpec struct {
    // VolumeReplicationDriver is the registered driver that handles volume
    // replication operations for this plan's volumes. Immutable after creation.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Enum=noop
    VolumeReplicationDriver string `json:"volumeReplicationDriver"`

    // VolumeReplicationClass is the VolumeReplicationClass name stamped on
    // VolumeReplication and VolumeGroupReplication CRs created by the CSI
    // Extension driver. Free-form string — validated only for non-empty.
    // Immutable after creation.
    // +kubebuilder:validation:Required
    VolumeReplicationClass string `json:"volumeReplicationClass"`

    // ... existing fields unchanged ...
}
```

**Why free-form (not enum):** Unlike `volumeReplicationDriver` which maps to an in-process driver registry, `volumeReplicationClass` is an opaque string passed through to external CRs. Real deployments use vendor-specific class names (e.g., `dell-powerstore-replication`, `ceph-rbd-replication`). Enumerating these in Soteria's API would couple it to storage vendors.

### CSI Addons Types Reference

**Import path:** `github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1`

**API group:** `replication.storage.openshift.io` / version `v1alpha1`

**VolumeReplication (VR):**
- `spec.volumeReplicationClass` — string, matches DRPlanSpec's value
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

**SetupWithManager:** Use two controller registrations or a single controller with `Watches()` for the second type. Preferred: two separate controllers (`For(VolumeReplication)` and `For(VolumeGroupReplication)`) to keep predicates clean. Or a single reconciler struct with two `SetupWithManager` calls creating two controllers.

**Recommended approach:** Single reconciler struct, two setup methods:
- `SetupVolumeReplicationController(mgr) error` — `For(&VolumeReplication{})`
- `SetupVolumeGroupReplicationController(mgr) error` — `For(&VolumeGroupReplication{})`

### Validation Pattern

Follow the existing pattern in `validation.go` exactly. For `volumeReplicationClass`, since it is free-form (not enum), the create validation is just a non-empty check:

```go
if plan.Spec.VolumeReplicationClass == "" {
    allErrs = append(allErrs, field.Required(specPath.Child("volumeReplicationClass"), ""))
}
```

Update validation — immutability:

```go
if newPlan.Spec.VolumeReplicationClass != oldPlan.Spec.VolumeReplicationClass {
    allErrs = append(allErrs, field.Forbidden(specPath.Child("volumeReplicationClass"), "field is immutable"))
}
```

### Test Fixture Sweep Pattern

Same pattern as Story 11.1 (~22 Go test files, ~17 console-plugin test files). Every `DRPlanSpec{}` construction needs `VolumeReplicationClass: "soteria-noop"`. Every console-plugin spec object needs `volumeReplicationClass: 'soteria-noop'`.

Known Go test file locations (typed `DRPlanSpec{}` constructions):
- `pkg/apis/soteria.io/v1alpha1/validation_test.go`
- `pkg/controller/drplan/reconciler_test.go`
- `pkg/controller/drexecution/reconciler_test.go`
- `pkg/engine/executor_test.go`
- `pkg/engine/reprotect_test.go`
- `pkg/admission/plugin_test.go`
- `pkg/admission/drplan_validator_test.go`
- `pkg/admission/drexecution_validator_test.go`
- `pkg/admission/vm_validator_test.go`
- `internal/preflight/checks_test.go`
- `pkg/registry/drplan/strategy_test.go`
- `test/integration/controller/*_test.go`
- `test/integration/replication/*_test.go`
- `test/integration/storage/*_test.go`

Known Go test file locations (unstructured `map[string]any` specs — easy to miss):
- `test/integration/apiserver/admission_test.go` — `createDRPlan` helper builds spec as map
- `test/integration/apiserver/apiserver_test.go` — inline DRPlan spec maps
- `test/integration/rbac/rbac_test.go` — `newDRPlan` helper builds spec as map

Note: `pkg/apiserver/critical_fields_test.go` builds status-only `DRPlan{}` fixtures (no spec) — does NOT need sweeping.

Known console-plugin test file locations (from Story 11.5):
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

Also update TypeScript `DRPlanSpec` interface in `console-plugin/src/models/types.ts` to add `volumeReplicationClass: string`.

### What NOT to Change

- `pkg/drivers/` — no driver changes needed; `volumeReplicationClass` is for external CRs, not the in-process driver registry
- `pkg/engine/` — engine does not use `volumeReplicationClass` (it uses `volumeReplicationDriver` for driver resolution)
- `internal/preflight/` — preflight does not inspect `volumeReplicationClass`
- `pkg/controller/drplan/health.go` — health monitoring uses the driver, not the class
- `pkg/apiserver/critical_fields.go` — `volumeReplicationClass` is immutable; cannot trigger critical-field writes
- `pkg/admission/plugin.go` — admission plugin delegates to `ValidateDRPlan`/`ValidateDRPlanUpdate`; no admission-specific logic needed for the new field
- `pkg/registry/drplan/strategy.go` — table convertor columns are metadata/status only (Name, Phase, VMs, etc.); no spec columns, no changes needed
- `test/integration/controller/suite_test.go` — noop VR/VGR controller is NOT registered here; unit tests with fake client cover behavior; CSI Addons CRDs are not installed in envtest
- `PlanConfiguration` component in console plugin — this story adds the TypeScript type only; displaying `volumeReplicationClass` in the UI is deferred to a future story if needed

### Previous Story Intelligence

- **Story 11.1 (Add VolumeReplicationDriver to DRPlanSpec):** Established the exact pattern for this story's Part A. Required+immutable field, validation in `ValidateDRPlan` + `ValidateDRPlanUpdate`, test fixture sweep (~22 Go files + ~17 console test files), sample CRD update. Key difference: 11.1 used Enum marker; 12.0 does NOT (free-form string).
- **Story 11.5 (Console UI Displays Volume Replication Driver):** Updated console-plugin `DRPlanSpec` TS interface and swept test fixtures. Same sweep needed for `volumeReplicationClass`.
- **Story 4.9 (Site Topology Fields):** Added `primarySite`/`secondarySite` as immutable fields — the original immutability validation pattern.
- **Epic 11 retrospective:** Noted Story 12.0 scope: "Noop VolumeReplication Controller + volumeReplicationClass field on DRPlanSpec."

### Git Intelligence

Recent commits (all Epic 11):
- `062a1ab` Fix 9 TypeScript type-check errors in console-plugin
- `016174c` Story 11.5: Console UI displays volume replication driver
- `d89a977` Story 11.1: Add VolumeReplicationDriver to DRPlanSpec

Patterns: clean single-story commits, fixture sweeps are done as part of each story, codegen always follows type changes.

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

- [Source: `pkg/apis/soteria.io/v1alpha1/types.go` — DRPlanSpec struct, lines 84–105]
- [Source: `pkg/apis/soteria.io/v1alpha1/validation.go` — ValidateDRPlan/ValidateDRPlanUpdate, lines 40–94]
- [Source: `pkg/controller/drplan/doc.go` — controller doc.go pattern]
- [Source: `pkg/controller/drexecution/doc.go` — RBAC markers + doc.go pattern]
- [Source: `pkg/drivers/noop/driver.go` — noop pattern and registration, lines 223–236]
- [Source: sprint-status.yaml comments — Epic 12 story definitions]
- [Source: CSI Addons VolumeReplication types — `api/replication.storage/v1alpha1/volumereplication_types.go`]
- [Source: CSI Addons VolumeGroupReplication types — `api/replication.storage/v1alpha1/volumegroupreplication_types.go`]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
