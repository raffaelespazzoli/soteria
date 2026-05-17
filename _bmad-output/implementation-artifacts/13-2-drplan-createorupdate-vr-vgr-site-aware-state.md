# Story 13.2: DRPlan CreateOrUpdate VR/VGR with Site-Aware State

Status: ready-for-dev

## Story

As a platform engineer,
I want the DRPlan controller to create or update VolumeReplication and VolumeGroupReplication objects on each site with the correct replication state,
So that VR/VGR lifecycle is managed by the DRPlan and the replication state always reflects the site's current role.

## Acceptance Criteria

1. **AC1 — CreateOrUpdate in DRPlan reconciler:** Given a DRPlan with discovered VMs and resolved volume groups, when the DRPlan controller reconciles (active or passive site), then it calls `createOrUpdate` (via the CSI Extension driver) for each volume group's VR/VGR on the local cluster, and the `createOrUpdate` uses `controllerutil.CreateOrUpdate` semantics (create if absent, update spec if changed).

2. **AC2 — Site-aware replicationState derivation:** Given the DRPlan controller with `LocalSite` configured, when creating or updating VR/VGR on the local cluster, then `spec.replicationState` is set to `primary` if `LocalSite == plan.Status.ActiveSite` (or `plan.Spec.PrimarySite` when ActiveSite is empty), and `spec.replicationState` is set to `secondary` if `LocalSite != ActiveSite`.

3. **AC3 — Each site creates independently:** Given two Soteria controller instances (one per site), when both reconcile the same DRPlan, then each creates VR/VGR only on its own cluster (local Kubernetes API), the primary site creates with `replicationState: primary`, and the secondary site creates with `replicationState: secondary`.

4. **AC4 — No createOrUpdate during active execution:** Given a DRPlan with an active (non-terminal) DRExecution, when the DRPlan controller reconciles, then it skips the createOrUpdate of VR/VGR (the DRExecution controller owns replication state changes during execution).

5. **AC5 — CSI Extension driver CreateVolumeGroup uses createOrUpdate:** Given the CSI Extension driver, when `CreateVolumeGroup` is called, then it uses `createOrUpdate` semantics (not create-only with AlreadyExists skip), and on update, only `spec.replicationState` is mutated (DataSource and class are immutable).

6. **AC6 — StopReplication always sets secondary:** Given the CSI Extension driver's `StopReplication` method, when called on a VR/VGR with any `spec.replicationState`, then it sets `spec.replicationState` to `secondary` (not a flip), and the `flipReplicationState` helper is replaced with unconditional `secondary` assignment.

7. **AC7 — Noop controller drops resync case:** Given the noop VolumeReplication controller's `stateForReplicationState` function, when mapping `spec.replicationState` to status, then only `primary` and `secondary` cases are handled (the `Resync` case is removed from the switch), and `statusUpToDate` is simplified accordingly.

8. **AC8 — Tests:** Reconciler tests verify VR/VGR creation with correct replicationState per site role. Driver tests verify createOrUpdate idempotency (create + update path). StopReplication tests verify it always sets `secondary` regardless of input state. Resync test cases in the noop controller are removed or replaced. Tests verify no createOrUpdate during active execution. All existing tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Refactor CSI Extension driver CreateVolumeGroup to createOrUpdate semantics (AC: #5)
  - [ ] 1.1 In `createVRs()`: replace `client.Create` + AlreadyExists skip with `controllerutil.CreateOrUpdate` pattern — on update, mutate only `spec.replicationState` (leave DataSource and VolumeReplicationClass untouched)
  - [ ] 1.2 In `createVGR()`: replace `client.Create` + AlreadyExists skip with `controllerutil.CreateOrUpdate` pattern — on update, mutate only `spec.replicationState` (leave Source selector, class fields untouched)
  - [ ] 1.3 Update driver unit tests for createOrUpdate idempotency (create path + update path with state change)

- [ ] Task 2: Refactor CSI Extension driver StopReplication to always set secondary (AC: #6)
  - [ ] 2.1 Replace `flipReplicationStates()` call in `StopReplication` with `updateReplicationState(ctx, set, ReplicationStateSecondary)`
  - [ ] 2.2 Delete `flipReplicationStates()` method and `flipReplicationState()` helper function from `helpers.go`
  - [ ] 2.3 Update StopReplication tests to verify unconditional `secondary` output regardless of input state (remove flip assertions)

- [ ] Task 3: Remove Resync case from noop VolumeReplication controller (AC: #7)
  - [ ] 3.1 In `stateForReplicationState()`: remove `replicationv1alpha1.Resync` from the `case replicationv1alpha1.Secondary` line
  - [ ] 3.2 Simplify `statusUpToDate` if any Resync-specific logic exists
  - [ ] 3.3 Remove/update Resync test case in `reconciler_test.go`

- [ ] Task 4: Add VR/VGR createOrUpdate to DRPlan reconciler (AC: #1, #2, #3, #4)
  - [ ] 4.1 Add a new method `reconcileVolumeReplication(ctx, plan, waves)` on `DRPlanReconciler` that iterates volume groups and calls `driver.CreateVolumeGroup` with site-aware labels
  - [ ] 4.2 Derive site role: if `LocalSite == plan.Status.ActiveSite` (fallback to `plan.Spec.PrimarySite` when empty) → `SiteRolePrimary`, else → `SiteRoleSecondary`
  - [ ] 4.3 Gate: skip call if `r.hasActiveExecution(ctx, plan.Name)` is true
  - [ ] 4.4 Wire the new method into the reconcile loop (after wave formation, before health polling)
  - [ ] 4.5 Add RBAC markers for `replication.storage.openshift.io` VR/VGR resources (get, list, watch, create, update, patch)

- [ ] Task 5: Tests (AC: #8)
  - [ ] 5.1 Reconciler unit tests: verify createOrUpdate called with correct site role for primary vs secondary site
  - [ ] 5.2 Reconciler unit tests: verify no createOrUpdate when active execution exists
  - [ ] 5.3 Driver integration tests: createOrUpdate idempotency (create new + update existing with different state)
  - [ ] 5.4 StopReplication tests: always secondary regardless of input state (primary→secondary, secondary→secondary)

- [ ] Task 6: Verify (AC: all)
  - [ ] 6.1 `make test` — all tests pass
  - [ ] 6.2 `make lint-fix && make lint` — zero lint issues
  - [ ] 6.3 `make manifests generate` — RBAC markers generate correctly

## Dev Notes

### Key Files to Modify

| File | Action | Notes |
|------|--------|-------|
| `pkg/drivers/csiextension/driver.go` | Modify | `createVRs()` and `createVGR()` → adopt `controllerutil.CreateOrUpdate` semantics |
| `pkg/drivers/csiextension/helpers.go` | Modify | Delete `flipReplicationState()` function and `flipReplicationStates()` method; `StopReplication` calls `updateReplicationState(ctx, set, ReplicationStateSecondary)` |
| `pkg/drivers/csiextension/driver_test.go` | Modify | Update StopReplication tests (always secondary), add createOrUpdate tests |
| `pkg/controller/volumereplication/reconciler.go` | Modify | Remove `replicationv1alpha1.Resync` from `stateForReplicationState` switch |
| `pkg/controller/volumereplication/reconciler_test.go` | Modify | Remove/update Resync test case |
| `pkg/controller/drplan/reconciler.go` | Modify | Add `reconcileVolumeReplication()` method + wire into reconcile loop |
| `pkg/controller/drplan/reconciler_test.go` | Modify | Add tests for VR/VGR creation with site-aware state |

### Architecture & Pattern Compliance

**CreateOrUpdate pattern:** Use `controllerutil.CreateOrUpdate` from `sigs.k8s.io/controller-runtime/pkg/controller/controllerutil`. The mutate function should:
- Set labels (always)
- Set `spec.replicationState` (always — this is the mutable field)
- Set `spec.dataSource` / `spec.source` / class fields only on create (immutable after creation)

Example structure:
```go
vr := &replicationv1alpha1.VolumeReplication{
    ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
}
_, err := controllerutil.CreateOrUpdate(ctx, d.client, vr, func() error {
    vr.Labels = crLabels
    vr.Spec.ReplicationState = state
    if vr.CreationTimestamp.IsZero() {
        // Only set on create — immutable after
        vr.Spec.VolumeReplicationClass = vrClass
        vr.Spec.DataSource = corev1.TypedLocalObjectReference{...}
    }
    return nil
})
```

**Site-aware state derivation logic in reconciler:**
```go
func (r *DRPlanReconciler) siteReplicationRole(plan *soteriav1alpha1.DRPlan) string {
    activeSite := plan.Status.ActiveSite
    if activeSite == "" {
        activeSite = plan.Spec.PrimarySite
    }
    if r.LocalSite == activeSite {
        return csiextension.SiteRolePrimary
    }
    return csiextension.SiteRoleSecondary
}
```

**Active execution gate:** Reuse existing `r.hasActiveExecution(ctx, plan.Name)` — same pattern as health polling at line 393 of reconciler.go.

### StopReplication Simplification Rationale

Soteria never uses `spec.replicationState = resync`. The flip semantics (`primary→secondary, secondary→primary`) are incorrect for the DR model where `StopReplication` always means "demote to secondary — this volume is no longer the source". The existing `SetSource` already unconditionally sets `primary`. Making `StopReplication` unconditionally set `secondary` creates a clean, predictable model:
- `SetSource` = promote to primary (always) — "become the writable source, VMs run here"
- `StopReplication` = demote to secondary (always) — "become the read-only target, VMs don't run here"

**IMPORTANT: Handler restructuring required in Story 13.6.** The current failover handler calls `StopReplication` on the Owner (target) site to promote it (via flip: secondary→primary). After this story's change, that would BREAK the handler (target stays secondary = read-only, VMs can't write). Story 13.6 restructures the failover handler to call `SetSource → StartVM` instead of `StopReplication → StartVM`, and adds `StopReplication` to Step0 for planned migration.

### RBAC Considerations

The DRPlan reconciler needs RBAC for VR/VGR CRDs. Add these markers to the reconciler:
```go
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumereplications,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumegroupreplications,verbs=get;list;watch;create;update;patch
```

These may already exist from Story 12.0 (noop controller). Verify existing RBAC covers the DRPlan controller's ClusterRole (they are separate ClusterRoles — noop controller has its own, DRPlan reconciler needs its own markers).

### Critical Rule 12 Compliance

Per project-context.md rule #12 (CRD-management stories need extra review scrutiny):
- RBAC markers must include all verbs actually used (get/list/watch/create/update/patch)
- Namespace scoping is explicit (VR/VGR are namespace-scoped; DRPlan is cluster-scoped — the driver receives namespace from VolumeGroupSpec)
- CreateOrUpdate handles AlreadyExists gracefully (that's what controllerutil.CreateOrUpdate does)
- No informer cache issues — the reconciler calls the driver which uses direct client (not cached) for mutations
- Concurrent controller races: the noop controller also reconciles VR/VGR but only updates status — no spec conflicts with the DRPlan reconciler's spec mutations

### Project Structure Notes

- CSI extension driver lives in `pkg/drivers/csiextension/` — external to kubebuilder structure
- Noop VolumeReplication controller lives in `pkg/controller/volumereplication/`
- DRPlan controller lives in `pkg/controller/drplan/`
- All use `sigs.k8s.io/controller-runtime/pkg/client` for Kubernetes API access

### What NOT to Change

- `pkg/drivers/csiextension/status.go` — health monitoring (Story 12.5) is untouched
- `pkg/drivers/csiextension/constants.go` — existing constants are reused, no new ones needed
- `pkg/engine/failover.go` — handler restructuring (StopReplication→SetSource) deferred to Story 13.6; VG resolution changes deferred to Story 13.5
- `pkg/controller/drexecution/` — DRExecution controller changes deferred to Story 13.6
- `pkg/controller/drplan/health.go` — health polling changes deferred to Story 13.5
- `pkg/drivers/conformance/` — conformance suite untouched

### Dependencies

- **Depends on:** Epic 12 (CSI Extension driver complete — all 6 StorageProvider methods implemented)
- **Depended on by:** Story 13.3 (dual finalizers), Story 13.5 (remove CreateVolumeGroup from engine/health paths)

### Previous Story Intelligence

**From Story 12.6 (Conformance Suite & Integration Testing):**
- The conformance adapter bridges CSI-extension behavior differences (no NonReplicated state in CSI model)
- VR/VGR status must be simulated in tests (the noop controller handles this in real clusters)
- Integration tests use `interceptor.Funcs` for API error injection
- Coverage was at 90.9% — maintain or improve

**From Story 12.3 (CreateVolumeGroup/DeleteVolumeGroup/GetVolumeGroup):**
- Current `CreateVolumeGroup` uses `client.Create` + AlreadyExists skip — this story replaces that with `controllerutil.CreateOrUpdate`
- VolumeGroupID format: `csi-ext-<namespace>/<name>` — unchanged
- Rendering rule: `vm-*` → VR per PVC, `ns-*` → VGR — unchanged
- `SiteRoleLabel` in `VolumeGroupSpec.Labels` already carries the site role — reuse for state derivation

**From Story 12.4 (StopReplication & SetSource):**
- Current StopReplication uses `flipReplicationStates()` — this story simplifies to unconditional secondary
- SetSource already unconditionally sets primary via `updateReplicationState(ctx, set, ReplicationStatePrimary)` — unchanged
- `crSet` type and `listCRsForVG` helper remain — only the flip logic is replaced

### Testing Patterns

- Driver tests: envtest with real VR/VGR CRDs registered in scheme (from Story 12.6)
- Reconciler tests: envtest with mock driver via fake (from `pkg/drivers/fake/`)
- Table-driven tests with descriptive names: `TestCreateVolumeGroup_CreateOrUpdate_ExistingVR_UpdatesState`
- Coverage target: maintain 90%+ for csiextension package

### Build Commands

```bash
make manifests generate  # Regenerate RBAC after adding markers
make test               # All tests pass
make lint-fix && make lint  # Zero lint issues
```

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

## Change Log

- **2026-05-17:** Story created — comprehensive developer guide for DRPlan createOrUpdate VR/VGR with site-aware state
