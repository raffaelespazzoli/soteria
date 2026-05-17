# Story 13.4: DRPlan Watches VR/VGR for Replication Health

Status: ready-for-dev

## Story

As a platform engineer,
I want the DRPlan controller to watch VR/VGR status changes and update replication health reactively,
so that health updates are timely and event-driven rather than purely poll-based.

## Background

### Current Health Monitoring (Poll-Only)

The DRPlan controller polls replication health on every reconcile cycle via `pollReplicationHealth` in `pkg/controller/drplan/health.go`. This iterates all VGs in the plan's waves, resolves a driver, calls `GetReplicationStatus`, and maps the result to `VolumeGroupHealth` entries on `DRPlanStatus`. The requeue interval is 10m (normal) or 30s (degraded). This means a VR/VGR status change can take up to 10 minutes to be reflected in the DRPlan status.

### What This Story Adds

A secondary watch on VolumeReplication and VolumeGroupReplication CRDs in `SetupWithManager`, with a status-only predicate that filters out spec/metadata-only changes. When a VR/VGR status changes, an event handler reads the `soteria.io/drplan` label and enqueues a reconcile request for the owning DRPlan. The existing poll-based path is retained as a fallback safety net.

### Pattern Reference: Existing VM Watch

The DRPlan controller already watches VirtualMachine resources with a similar pattern:
- `vmRelevantChangePredicate()` filters to label changes only
- `vmEventHandler()` reads `soteria.io/drplan` label and enqueues the plan
- The handler uses `handler.Funcs` with typed Create/Update/Delete callbacks

This story follows the exact same pattern, adapted for VR/VGR with a status-change predicate instead of a label-change predicate.

## Acceptance Criteria

1. **AC1 — Secondary watch on VR/VGR:** `SetupWithManager` watches `VolumeReplication` and `VolumeGroupReplication` resources with a predicate that fires only on `status.state` or `status.conditions` changes.

2. **AC2 — Event-to-DRPlan mapping:** The watch handler reads the `soteria.io/drplan` label from VR/VGR objects to determine the owning DRPlan and enqueues a reconcile request for that DRPlan.

3. **AC3 — Health derived from VR/VGR status.state:** The DRPlan controller reconciling after a VR/VGR status change reads `status.state` from VR/VGR objects (via the existing `GetReplicationStatus` driver method) and maps to the existing `VolumeGroupHealth` model. No change to `pollReplicationHealth` or `mapReplicationStatus` — these already work correctly.

4. **AC4 — Poll-based health retained as fallback:** The periodic requeue (10m normal / 30s degraded) is retained as a safety net. The watch provides faster reaction to state changes between poll intervals.

5. **AC5 — Tests:** Watch predicate tests verify filtering on status changes only. Event handler tests verify correct DRPlan enqueue from label. All existing health monitoring tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Create VR/VGR status-change predicate (AC: #1)
  - [ ] 1.1 Add `vrStatusChangePredicate()` function in `reconciler.go` — returns `predicate.Funcs` that:
    - Create: returns `false` (new VR/VGR won't have meaningful status yet; poll will pick it up)
    - Delete: returns `false` (deletion is not a status change; poll handles stale health)
    - Update: compares `status.state` and `status.conditions` between old and new; returns `true` only if different
    - Generic: returns `false`
  - [ ] 1.2 The predicate must type-assert to access `.Status.State` and `.Status.Conditions` since VR/VGR types come from `replicationv1alpha1`

- [ ] Task 2: Create VR/VGR event handler (AC: #2)
  - [ ] 2.1 Add `vrEventHandler()` method on `DRPlanReconciler` — returns `handler.Funcs` with an `UpdateFunc` that calls `r.enqueueForVR(e.ObjectNew, q)`
  - [ ] 2.2 Add `enqueueForVR(obj client.Object, q reqQueue)` — reads `soteria.io/drplan` label via `csiextension.LabelDRPlan` constant, enqueues reconcile request (same pattern as `enqueueForVM`)

- [ ] Task 3: Wire watches in SetupWithManager (AC: #1, #2)
  - [ ] 3.1 Add two `.Watches()` calls to the controller builder — one for `&replicationv1alpha1.VolumeReplication{}` and one for `&replicationv1alpha1.VolumeGroupReplication{}`
  - [ ] 3.2 Both use `vrEventHandler()` and `builder.WithPredicates(vrStatusChangePredicate())`
  - [ ] 3.3 Import `replicationv1alpha1` in reconciler.go

- [ ] Task 4: Add RBAC markers (AC: #1)
  - [ ] 4.1 Verify RBAC for VR/VGR watch — the `get;list;watch` verbs on `replication.storage.openshift.io` are already present in `pkg/controller/volumereplication/doc.go`; check if the DRPlan controller also needs its own markers or if the existing ones cover it (they likely do since RBAC is per-ClusterRole, not per-controller)

- [ ] Task 5: Unit tests (AC: #5)
  - [ ] 5.1 Test `vrStatusChangePredicate` — Create returns false, Delete returns false, Generic returns false
  - [ ] 5.2 Test `vrStatusChangePredicate` — Update with status.state change returns true
  - [ ] 5.3 Test `vrStatusChangePredicate` — Update with status.conditions change returns true
  - [ ] 5.4 Test `vrStatusChangePredicate` — Update with spec-only change returns false
  - [ ] 5.5 Test `vrStatusChangePredicate` — Update with metadata-only change returns false
  - [ ] 5.6 Test `enqueueForVR` — object with `soteria.io/drplan` label enqueues correct plan
  - [ ] 5.7 Test `enqueueForVR` — object without label enqueues nothing
  - [ ] 5.8 Test `enqueueForVR` — object with empty label enqueues nothing

- [ ] Task 6: Verify (AC: all)
  - [ ] 6.1 Run `make manifests generate` — regenerate RBAC if markers changed
  - [ ] 6.2 Run `make test` — all tests pass
  - [ ] 6.3 Run `make lint-fix && make lint` — zero lint issues
  - [ ] 6.4 Update `doc.go` with Story 13.4 description (Tier 1 compliance)

## Dev Notes

### Key Locations

| File | Action |
|------|--------|
| `pkg/controller/drplan/reconciler.go` | Modified — add `vrStatusChangePredicate()`, `vrEventHandler()`, `enqueueForVR()`, update `SetupWithManager`, add `replicationv1alpha1` import |
| `pkg/controller/drplan/reconciler_test.go` | Modified — add predicate and event handler tests |
| `pkg/controller/drplan/doc.go` | Modified — update package doc with VR/VGR watch description |

### Predicate Implementation Strategy

The VR/VGR status fields to compare are:
- `Status.State` — a `string` indicating the observed replication state (`Primary`, `Secondary`, `Resync`, `Unknown`)
- `Status.Conditions` — a `[]metav1.Condition` slice

For the predicate's `UpdateFunc`, type-assert `e.ObjectOld` and `e.ObjectNew` to `*replicationv1alpha1.VolumeReplication` (or `*replicationv1alpha1.VolumeGroupReplication`). Since both VR and VGR have the same status fields, the predicate can handle both types via a shared interface or conditional type assertion.

**Recommended approach:** Use conditional type switches:

```go
func vrStatusChangePredicate() predicate.Predicate {
    return predicate.Funcs{
        CreateFunc:  func(_ event.CreateEvent) bool { return false },
        DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
        GenericFunc: func(_ event.GenericEvent) bool { return false },
        UpdateFunc: func(e event.UpdateEvent) bool {
            return vrStatusDiffers(e.ObjectOld, e.ObjectNew)
        },
    }
}
```

Where `vrStatusDiffers` handles `*replicationv1alpha1.VolumeReplication` and `*replicationv1alpha1.VolumeGroupReplication` via type switch, comparing `.Status.State` (string equality) and `.Status.Conditions` (reflect.DeepEqual or manual comparison).

### Event Handler Pattern

Follow the exact same pattern as `vmEventHandler()` / `enqueueForVM()`:

```go
func (r *DRPlanReconciler) vrEventHandler() handler.Funcs {
    return handler.Funcs{
        UpdateFunc: func(_ context.Context, e event.TypedUpdateEvent[client.Object], q reqQueue) {
            r.enqueueForVR(e.ObjectNew, q)
        },
    }
}

func (r *DRPlanReconciler) enqueueForVR(obj client.Object, q reqQueue) {
    planName := obj.GetLabels()[csiextension.LabelDRPlan]
    if planName == "" {
        return
    }
    q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: planName}})
}
```

Key differences from the VM handler:
- Only `UpdateFunc` is needed (Create/Delete return false in the predicate)
- Uses `csiextension.LabelDRPlan` constant ("soteria.io/drplan") instead of `soteriav1alpha1.DRPlanLabel`
- DRPlan is cluster-scoped so `Namespace` is always empty

### Import Required

```go
import (
    replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
    "github.com/soteria-project/soteria/pkg/drivers/csiextension"
)
```

The `replicationv1alpha1` package is already a dependency (used by `pkg/controller/volumereplication/` and `pkg/drivers/csiextension/`). The scheme registration for these types is already done in `cmd/main.go` (from Story 12.0).

### RBAC Notes

RBAC markers for `replication.storage.openshift.io` VR/VGR resources with `get;list;watch` verbs already exist in `pkg/controller/volumereplication/doc.go`. Since RBAC markers are aggregated into a single `ClusterRole` by `make manifests`, no new markers should be needed in the DRPlan controller. Verify this by checking `config/rbac/role.yaml` after `make manifests` — if VR/VGR rules are present, no action needed. The story's epic tech notes say "RBAC markers for VR/VGR watch already exist from Story 12.0".

### What NOT to Change

- `health.go` — `pollReplicationHealth`, `pollSingleVG`, `mapReplicationStatus`, `resolveVolumeGroupID` all remain unchanged. The watch triggers reconcile, and the existing health polling path runs within that reconcile
- `pkg/controller/volumereplication/` — the noop VR controller is separate
- `pkg/drivers/csiextension/` — the driver layer is unchanged
- Poll-based requeue intervals — 10m normal, 30s degraded are retained as safety net

### Scope

~2 modified prod files (reconciler.go, doc.go), ~1 modified test file (reconciler_test.go). This is a small, focused story.

### Dependencies

- **Depends on Stories 13.1-13.3 conceptually** (VR/VGR objects must exist with `soteria.io/drplan` labels for the watch to be useful), but the watch setup itself is independent — it simply watches for CRs that happen to exist
- **No dependency on Story 13.5 or 13.6** — those are downstream

### Previous Story Intelligence

**From Epic 12 (CSI Extension Driver):**
- Story 12.0 registered CSI Addons VR/VGR types in the scheme and added RBAC markers — these are prerequisites for this watch
- Story 12.5 implemented `GetReplicationStatus` with health/role mapping — this is the existing code that runs during reconcile to poll health; the watch simply triggers reconcile sooner
- Story 12.6 conformance tests verified the driver contract end-to-end
- Import alias for CSI Addons types: `replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"`
- VR/VGR CRs carry `soteria.io/drplan` label (set by `csiextension.LabelDRPlan` constant in `pkg/drivers/csiextension/constants.go`)

**From existing DRPlan controller patterns:**
- `vmRelevantChangePredicate()` at line 1458 of reconciler.go — exact pattern to follow for the predicate
- `vmEventHandler()` at line 1058 — exact pattern to follow for the handler
- `enqueueForVM()` at line 1085 — exact pattern to follow for enqueue
- `SetupWithManager()` at line 1036 — add new `.Watches()` calls here
- `reqQueue` type alias at line 1052 — reuse for handler signatures

### Build Commands

```bash
make manifests generate
make test
make lint-fix && make lint
```

### Project Context Reference

See `_bmad-output/project-context.md` for:
- Controller watch patterns and predicate conventions
- Structured logging style (Info(0) = state transitions, V(1) = normal ops)
- RBAC marker conventions
- Testing with envtest for controller tests
- Tier 1/2/3 documentation requirements

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

### Change Log
