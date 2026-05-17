# Story 13.3: Dual Finalizers on VR/VGR Objects

Status: ready-for-dev

## Story

As a platform engineer,
I want VR/VGR objects to carry two finalizers (one per site),
So that both Soteria controller instances clean up their local resources before the object is garbage collected, and DRPlan deletion triggers VR/VGR cleanup.

## Acceptance Criteria

1. **AC1 — Finalizer constants:** Two finalizer constants defined in `pkg/drivers/csiextension/constants.go`: `soteria.io/site-primary` and `soteria.io/site-secondary`.

2. **AC2 — Finalizer added on creation:** When the DRPlan controller creates or updates a VR/VGR on the local cluster, it adds its site-specific finalizer (primary site adds `soteria.io/site-primary`, secondary adds `soteria.io/site-secondary`). The finalizer is added only if not already present.

3. **AC3 — Finalizer removed on DRPlan deletion:** When a DRPlan has a non-nil `DeletionTimestamp`, the DRPlan controller: (a) deletes all VR/VGR owned by this plan (by label `soteria.io/drplan`), (b) removes its site-specific finalizer from each VR/VGR before or during deletion, (c) removes the DRPlan's own finalizer only after all VR/VGR finalizers are cleared.

4. **AC4 — DRPlan finalizer:** On first reconcile, the DRPlan controller adds `soteria.io/volume-replication` to the DRPlan's metadata.finalizers. This prevents DRPlan deletion until VR/VGR cleanup is complete.

5. **AC5 — Cross-site cleanup:** With both `soteria.io/site-primary` and `soteria.io/site-secondary` finalizers present, removing one leaves the object alive; removing both allows GC.

6. **AC6 — Degraded mode (one site down):** When a DRPlan is deleted while one site is unreachable, the reachable site removes its finalizer. The VR/VGR stays in Terminating state until the other site recovers and removes its finalizer.

7. **AC7 — Tests:** Finalizer addition on create, finalizer removal on DRPlan deletion, GC only after both finalizers removed, all existing tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Define finalizer constants (AC: #1)
  - [ ] 1.1 Add `FinalizerSitePrimary = "soteria.io/site-primary"` to `pkg/drivers/csiextension/constants.go`
  - [ ] 1.2 Add `FinalizerSiteSecondary = "soteria.io/site-secondary"` to `pkg/drivers/csiextension/constants.go`
  - [ ] 1.3 Add `FinalizerVolumeReplication = "soteria.io/volume-replication"` — either in `csiextension/constants.go` or in the DRPlan reconciler (whichever makes more sense given import paths; the reconciler needs this constant for the DRPlan finalizer)

- [ ] Task 2: Add site-specific finalizer on VR/VGR creation (AC: #2)
  - [ ] 2.1 Modify `createVRs` in `pkg/drivers/csiextension/driver.go` to set the site-specific finalizer in `ObjectMeta.Finalizers` on each VR before `client.Create`
  - [ ] 2.2 Modify `createVGR` to set the site-specific finalizer on the VGR before `client.Create`
  - [ ] 2.3 The site finalizer is determined from `spec.Labels[SiteRoleLabel]`: primary → `FinalizerSitePrimary`, secondary → `FinalizerSiteSecondary`
  - [ ] 2.4 For the AlreadyExists idempotent path, add a post-create patch to add the finalizer if the existing object doesn't have it (handles adoption of pre-existing CRs from Story 13.2)

- [ ] Task 3: Add DRPlan finalizer on first reconcile (AC: #4)
  - [ ] 3.1 In `pkg/controller/drplan/reconciler.go` Reconcile(), after fetching the DRPlan, check for `FinalizerVolumeReplication`
  - [ ] 3.2 If absent, add it using `controllerutil.AddFinalizer` + `r.Update(ctx, &plan)`, then requeue
  - [ ] 3.3 Only add the DRPlan finalizer when the plan uses the `csi-extension` driver (check `plan.Spec.VolumeReplicationDriver.Type == "csi-extension"`). Noop plans don't manage VR/VGR objects

- [ ] Task 4: Handle DRPlan deletion — VR/VGR cleanup (AC: #3, #5, #6)
  - [ ] 4.1 In `Reconcile()`, after finalizer check, if `plan.DeletionTimestamp != nil`, enter deletion cleanup path
  - [ ] 4.2 List all VR/VGR with label `soteria.io/drplan=<plan-name>` (reuse `LabelDRPlan` from csiextension constants)
  - [ ] 4.3 For each VR/VGR, remove the local site's finalizer using `controllerutil.RemoveFinalizer` + `r.Patch`
  - [ ] 4.4 After finalizer removal, delete the VR/VGR objects (issuing `client.Delete`)
  - [ ] 4.5 Once all VR/VGR are cleaned up (or confirmed deleted), remove `FinalizerVolumeReplication` from the DRPlan via `controllerutil.RemoveFinalizer` + `r.Update`
  - [ ] 4.6 If VR/VGR listing/patch fails (site unreachable), return error to requeue — the other site will eventually clean up its finalizer

- [ ] Task 5: RBAC markers (AC: #2, #3)
  - [ ] 5.1 Add/verify RBAC marker in `pkg/controller/drplan/reconciler.go` for `replication.storage.openshift.io` VR/VGR resources: `get;list;watch;update;patch;delete`
  - [ ] 5.2 Run `make manifests` to regenerate RBAC

- [ ] Task 6: Unit tests (AC: #7)
  - [ ] 6.1 Test finalizer added on VR create (driver-level)
  - [ ] 6.2 Test finalizer added on VGR create (driver-level)
  - [ ] 6.3 Test DRPlan finalizer added on first reconcile (reconciler-level)
  - [ ] 6.4 Test DRPlan deletion removes site finalizer from VR/VGR
  - [ ] 6.5 Test VR/VGR not GC'd until both site finalizers removed
  - [ ] 6.6 Test deletion requeues if VR/VGR cleanup fails

- [ ] Task 7: Verify (AC: all)
  - [ ] 7.1 Run `make test` — all tests pass
  - [ ] 7.2 Run `make lint-fix && make lint` — zero lint issues
  - [ ] 7.3 Run `make manifests generate` — RBAC/CRD regenerated
  - [ ] 7.4 Verify doc.go updated for any modified packages

## Dev Notes

### Key File Locations

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/constants.go` | Modified — add finalizer constants |
| `pkg/drivers/csiextension/driver.go` | Modified — add finalizers to VR/VGR in `createVRs`/`createVGR` |
| `pkg/controller/drplan/reconciler.go` | Modified — DRPlan finalizer + deletion cleanup path |
| `pkg/drivers/csiextension/driver_test.go` | Modified — add finalizer assertions to create tests |
| `pkg/controller/drplan/reconciler_test.go` | Modified — add finalizer/deletion tests |
| `pkg/drivers/csiextension/doc.go` | Modified — update package doc with finalizer info |

### Architecture & Design Decisions

**Finalizer pattern follows standard Kubernetes conventions:**
- DRPlan gets its own finalizer (`soteria.io/volume-replication`) to block deletion until cleanup completes
- VR/VGR objects get site-specific finalizers (`soteria.io/site-primary`, `soteria.io/site-secondary`)
- Two-site cleanup is naturally resilient: each site removes its own finalizer independently
- If one site is down, the other site removes its finalizer and the VR/VGR stays in Terminating; the down site cleans up on recovery

**Site finalizer derivation logic:**
```go
func finalizerForSiteRole(labels map[string]string) string {
    if labels[SiteRoleLabel] == SiteRoleSecondary {
        return FinalizerSiteSecondary
    }
    return FinalizerSitePrimary
}
```
This mirrors `replicationStateFromLabels` in `driver.go` (line 81–86).

**DRPlan deletion flow:**
1. DRPlan gets `DeletionTimestamp` (user deletes it)
2. `soteria.io/volume-replication` finalizer blocks GC
3. Reconciler enters cleanup: lists VR/VGR by `soteria.io/drplan` label
4. For each VR/VGR: removes local site finalizer via patch, then issues delete
5. Once all VR/VGR are gone (or only remote finalizers remain), removes DRPlan finalizer
6. DRPlan is GC'd

**Conditional finalizer — csi-extension only:**
The DRPlan finalizer should only be added when `plan.Spec.VolumeReplicationDriver.Type == "csi-extension"`. Noop driver plans don't create VR/VGR objects, so they shouldn't block on VR/VGR cleanup.

### What NOT to Change

- `pkg/drivers/interface.go` — StorageProvider interface unchanged
- `pkg/drivers/noop/` — noop driver does not deal with finalizers
- `pkg/drivers/fake/` — fake driver unchanged
- `pkg/controller/volumereplication/` — noop VR controller unchanged
- Existing VR/VGR `spec.replicationState` logic — finalizers are orthogonal to replication state
- Story 13.2 `createOrUpdate` semantics — this story adds finalizers to the existing create path

### Existing Code Patterns to Reuse

**Setting finalizers on created objects:**
Standard Kubernetes pattern — set `ObjectMeta.Finalizers` before `client.Create`. Use `controllerutil.AddFinalizer` / `controllerutil.RemoveFinalizer` from `sigs.k8s.io/controller-runtime/pkg/controller/controllerutil` for the reconciler paths.

**Label-based VR/VGR listing:**
Reuse `vgLabelSelector(vgName)` from `driver.go` for volume-group-scoped queries. For plan-scoped deletion, use `client.MatchingLabels{LabelDRPlan: plan.Name}` which lists all VR/VGR for the plan.

**Existing RBAC markers:**
`pkg/controller/volumereplication/doc.go` already has RBAC for `replication.storage.openshift.io` resources (line 17–20). The DRPlan reconciler needs its own markers since it's a different controller.

**Strategic merge patch for finalizer removal:**
Use `client.MergeFrom(obj.DeepCopy())` + `r.Patch()` for finalizer removal on VR/VGR — this follows project convention (project-context.md rule: prefer MergeFrom patches over Update for metadata changes in multi-controller environments).

### Dependencies

- **Depends on Story 13.2** — VR/VGR creation in the DRPlan reconciler must be in place before finalizers can be added on creation
- **Story 13.4 depends on this** — the VR/VGR watch setup assumes VR/VGR objects have finalizers and won't be prematurely GC'd

### Previous Story Intelligence (Epic 12)

**From Epic 12 (CSI Extension driver):**
- `createVRs` and `createVGR` in `driver.go` use `client.Create` with `AlreadyExists` skip for idempotency — finalizer must be set on the ObjectMeta before the Create call
- Labels are built by `vgLabels()` helper — finalizers are set separately on `ObjectMeta.Finalizers`
- Driver tests use `fake.NewClientBuilder().WithScheme(testScheme(t))` for the fake client — finalizers are fully supported by the fake client
- All VR/VGR CRs carry `LabelDRPlan` label (`soteria.io/drplan`) for plan-scoped identification — this is the selector for deletion cleanup
- Story 12.3 established `parseVGID` for namespace+name extraction from VolumeGroupID
- The `crSet` type in `helpers.go` is used by `listCRsForVG` — could potentially be reused for plan-scoped listing but uses volume-group labels, not plan labels

**Project-context.md rule #12 (CRD-management stories):**
This story manages external CRDs (VR/VGR) — heightened review scrutiny required:
- RBAC markers must include all verbs used (get, list, watch, update, patch, delete)
- Namespace scoping must be explicit
- Idempotency must handle AlreadyExists and NotFound gracefully
- Concurrent controller races addressed (two sites operate independently on same objects)

### Build Commands

```bash
make manifests generate
make test
make lint-fix && make lint
```

### Scope Estimate

~3 modified prod files, ~3 modified test files

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

### Change Log
