# Story 15.2: Planned Failover Resync Guard (Event-Driven)

Status: ready-for-dev

## Story

As a developer,
I want the planned failover Step 0 to request a resync on the target VR/VGR and wait (event-driven) for completion before proceeding,
so that zero data loss is guaranteed during planned migrations with asynchronous replication.

## Acceptance Criteria

**AC1: ResyncVolume call in Step 0**
Given a planned failover (GracefulShutdown=true)
When PreExecute runs Step 0
Then after stopping all origin VMs, `ResyncVolume` is called on each target VG
And `StopReplication` (demote source) is NOT called until resync completes

**AC2: Event-driven resync wait**
Given ResyncVolume has been called on target VGs
When the DRExecution reconciler returns
Then the DRExecution has a `ResyncPending=True` condition
And no `RequeueAfter` polling loop is used for checking resync state
And the DRExecution controller has a `.Watches()` for VolumeReplication/VolumeGroupReplication with a status predicate

**AC3: VR/VGR status watch on DRExecution controller**
Given the DRExecution controller's `SetupWithManager`
When the controller is initialized
Then it watches VR and VGR resources with a predicate filtering on `status.state` and `status.conditions[Completed]` changes
And the event handler maps VR/VGR → DRExecution via the `soteria.io/drplan` label (find active DRExecution for that plan)

**AC4: Reconciler gate on resync completion**
Given the DRExecution has `ResyncPending=True` and reconciliation is triggered by a VR status change
When all target VRs have `status.state=Secondary && conditions[Completed].status=True`
Then the `ResyncPending` condition is removed
And `StopReplication` is called on each source VG (demote to secondary)
And wave execution proceeds (SetSource + StartVM on target)

**AC5: Timeout safety net**
Given the DRExecution has `ResyncPending=True`
When `RequeueAfter(resyncTimeout)` fires (configurable, default 10m)
And the target VRs have NOT completed resync
Then the execution fails with a clear error message indicating resync timeout
And no partial promotion occurs (rest-state invariant preserved)

**AC6: Disaster failover unchanged**
Given a disaster failover (GracefulShutdown=false)
When PreExecute runs
Then no ResyncVolume call is made (source unreachable)
And per-group execution proceeds immediately (SetSource + StartVM)

**AC7: Checkpoint compatibility**
Given the DRExecution with ResyncPending state
When a checkpoint is written
Then the resync-wait state is captured via the `ResyncPending` condition on DRExecution.Status.Conditions
And on resume after crash, the reconciler re-evaluates VR status (does not re-call ResyncVolume if already in resync)

## Tasks / Subtasks

- [ ] Task 1: Modify PreExecute in `pkg/engine/failover.go` to call ResyncVolume on target VGs (AC: 1, 6)
  - [ ] 1.1: After StopVM loop, add ResyncVolume loop over target VGs (call on each unique VG before StopReplication)
  - [ ] 1.2: Remove the StopReplication call from PreExecute — demote moves to the reconciler after resync completes
  - [ ] 1.3: Return a new sentinel value or error type indicating "resync-requested, await completion" so the reconciler knows to set the condition
  - [ ] 1.4: Add unit tests for the modified PreExecute (ResyncVolume called, StopReplication NOT called)

- [ ] Task 2: Add `ResyncPending` condition and resync gate to reconciler (AC: 2, 4, 5, 7)
  - [ ] 2.1: Define `ConditionResyncPending = "ResyncPending"` constant in the drexecution package
  - [ ] 2.2: In `reconcileWaveExecution`, after Step 0 PreExecute completes, set `ResyncPending=True` condition with `RequeueAfter(resyncTimeout)` as safety net
  - [ ] 2.3: Add resync gate check in the resume path: if `ResyncPending=True`, evaluate VR/VGR status before proceeding
  - [ ] 2.4: Implement `checkResyncComplete` helper: queries target VR/VGR CRs, checks all have `status.state=Secondary && conditions[Completed].status=True`
  - [ ] 2.5: On resync complete: remove `ResyncPending` condition, call StopReplication on source VGs, set `Step0Complete`, proceed to waves
  - [ ] 2.6: On timeout (safety-net RequeueAfter fires + resync incomplete): fail execution with reason `ResyncTimeout`
  - [ ] 2.7: Add unit tests for the resync gate (complete, timeout, resume-after-crash)

- [ ] Task 3: Add VR/VGR watch to DRExecution controller's SetupWithManager (AC: 3)
  - [ ] 3.1: Add `.Watches()` for `replicationv1alpha1.VolumeReplication` with `vrStatusChangePredicate()` and `mapVRToDRExecution` handler
  - [ ] 3.2: Add `.Watches()` for `replicationv1alpha1.VolumeGroupReplication` with same predicate and handler
  - [ ] 3.3: Implement `mapVRToDRExecution`: read `soteria.io/drplan` label → find active DRExecution for that plan (same pattern as `mapVMToDRExecution`)
  - [ ] 3.4: Import and reuse `vrStatusChangePredicate` from the drplan package (or extract to shared package)
  - [ ] 3.5: Add RBAC markers for `replication.storage.openshift.io` VR/VGR `get;list;watch`
  - [ ] 3.6: Add unit tests for `mapVRToDRExecution` handler

- [ ] Task 4: Add `ResyncTimeout` field to DRPlanSpec (AC: 5)
  - [ ] 4.1: Add `ResyncTimeout *metav1.Duration` field to DRPlanSpec with `+kubebuilder:default="10m"` and `+optional`
  - [ ] 4.2: Run `make manifests generate` to regenerate CRD and DeepCopy
  - [ ] 4.3: Update sample CRD YAML with the new field
  - [ ] 4.4: Update console-plugin DRPlanSpec TypeScript interface (add `resyncTimeout?: string`)

- [ ] Task 5: Handle multi-site Step 0 flow (cross-site coordination via DRExecution conditions) (AC: 1, 2, 7)
  - [ ] 5.1: In `reconcileStep0` (source site): after StopVM, set `VMsStopped=True` condition on DRExecution, return with `RequeueAfter(resyncTimeout)` to wait for `ResyncComplete`
  - [ ] 5.2: In `reconcileStep0`: on re-reconcile, check for `ResyncComplete=True` condition (set by target site)
  - [ ] 5.3: On ResyncComplete: call StopReplication on LOCAL primary VRs, remove `ResyncPending`/`ResyncComplete`, set `Step0Complete`
  - [ ] 5.4: On timeout (resyncTimeout elapsed without ResyncComplete): fail execution with `ResyncTimeout`
  - [ ] 5.5: In target site (RoleOwner) path: add pre-wave gate checking `VMsStopped=True`, call ResyncVolume on LOCAL secondary VRs, set `ResyncPending=True`
  - [ ] 5.6: In target site: when VR watch fires and resync complete, set `ResyncComplete=True` condition
  - [ ] 5.7: Update tests for multi-site Step 0 coordination path

- [ ] Task 6: Handle single-site Step 0 flow (AC: 1, 2, 7)
  - [ ] 6.1: In `reconcileWaveExecution` Step 0 block (single-site path): after PreExecute, set `ResyncPending=True` and return with `RequeueAfter(resyncTimeout)`
  - [ ] 6.2: Add resync-pending gate before wave initialization: if `ResyncPending=True`, check completion
  - [ ] 6.3: On resync complete: call StopReplication, remove `ResyncPending`, set `Step0Complete`, proceed
  - [ ] 6.4: On timeout: fail execution
  - [ ] 6.5: Update tests for single-site Step 0 path

- [ ] Task 7: Update documentation and finalize
  - [ ] 7.1: Update `pkg/engine/doc.go` — document the ResyncVolume step in PreExecute
  - [ ] 7.2: Update `pkg/controller/drexecution/doc.go` — document VR/VGR watch and resync gate
  - [ ] 7.3: Update `_bmad-output/project-context.md` — add resync guard to the unified handler model description
  - [ ] 7.4: Run `make lint-fix` — fix any issues
  - [ ] 7.5: Run `make test` — all unit + envtest tests pass
  - [ ] 7.6: Verify Tier 1/2/3 doc compliance

## Dev Notes

### Architecture Overview — Resync Guard Flow

The resync guard ensures zero data loss in planned failovers by completing data synchronization before any promotion. The flow is:

```
Step 0 (source site for multi-site, local for single-site):
  1. StopVM (all origin VMs) — existing, unchanged
  2. ResyncVolume (all TARGET VGs) — NEW: requests sync from primary
  3. Return from PreExecute — signal resync-in-progress
  4. Set ResyncPending=True condition on DRExecution
  5. Return ctrl.Result{RequeueAfter: resyncTimeout} as safety net
  
VR/VGR watch triggers reconcile when status changes:
  6. Check all target VRs: status.state=Secondary && conditions[Completed]=True
  7. If complete: StopReplication (demote source) → remove ResyncPending → set Step0Complete
  8. If timeout: fail execution with ResyncTimeout
  
Target site continues:
  9. Wave execution proceeds (SetSource + StartVM on target)
```

### Implementation Pattern — Reusing Existing Patterns

**VR/VGR watch:** Reuses the exact `vrStatusChangePredicate()` from `pkg/controller/drplan/reconciler.go` (Story 13.4). The predicate checks `status.state`, `status.conditions`, and `status.lastSyncTime`. The handler maps VR/VGR → DRExecution using the same `soteria.io/drplan` label lookup, but instead of enqueuing the DRPlan, it finds the active DRExecution for that plan.

**Event-driven gate:** Mirrors the VM readiness gate pattern (Story 5.6): set condition → watch fires → reconciler checks → proceed or wait. The `ResyncPending` condition is analogous to `WaitingForVMReady`.

**mapVRToDRExecution:** Follows the same pattern as `mapVMToDRExecution` (line 1600-1625 of `reconciler.go`):
```go
func (r *DRExecutionReconciler) mapVRToDRExecution(
    ctx context.Context, obj client.Object,
) []reconcile.Request {
    planName := obj.GetLabels()[drivers.LabelDRPlan]
    if planName == "" {
        return nil
    }
    var execList soteriav1alpha1.DRExecutionList
    if err := r.List(ctx, &execList, client.MatchingLabels{
        soteriav1alpha1.PlanNameLabel: planName,
    }); err != nil {
        return nil
    }
    for i := range execList.Items {
        if !execList.Items[i].Status.IsTerminal() {
            return []reconcile.Request{{
                NamespacedName: types.NamespacedName{Name: execList.Items[i].Name},
            }}
        }
    }
    return nil
}
```

### Implementation Pattern — Modified PreExecute

Current Step 0 in `pkg/engine/failover.go` (lines 113-175):
1. StopVM (all VMs) ← unchanged
2. StopReplication (all VGs) ← **REMOVED** from here, moved to reconciler after resync

New Step 0:
1. StopVM (all VMs) ← unchanged
2. ResyncVolume (all **target** VGs) ← **NEW**

**Critical distinction:** ResyncVolume is called on the **target** VGs (the ones that will become primary after failover). The target VGs have VRs in `secondary` state — calling `ResyncVolume` on them triggers a sync-pull from the current primary. After sync completes, the **source** VGs are demoted via StopReplication.

The target VGs are determined by the site role: if `plan.Status.ActiveSite == plan.Spec.PrimarySite`, the target is on the secondary site. The CSI extension driver's VR/VGR CRs for the target VGs carry `soteria.io/site-role=secondary` labels.

**Signal to reconciler:** PreExecute needs to indicate that resync was requested and the reconciler should wait. Options:
- **Option A (recommended):** PreExecute returns nil (success) after calling ResyncVolume. The reconciler knows to check for resync-pending because it was a planned migration and Step0Complete is not yet set. The reconciler sets `ResyncPending=True` after PreExecute returns.
- Option B: PreExecute returns a new sentinel error type. Less clean — breaks the existing pattern where errors fail the execution.

### Implementation Pattern — Resync Completion Check

```go
func (r *DRExecutionReconciler) checkResyncComplete(
    ctx context.Context, plan *soteriav1alpha1.DRPlan,
) (bool, error) {
    // List VR/VGR CRs for this plan's target VGs
    // Target = site that is currently secondary (will become primary after failover)
    targetSiteRole := drivers.SiteRoleSecondary  // target VRs are currently secondary
    
    var vrList replicationv1alpha1.VolumeReplicationList
    if err := r.List(ctx, &vrList, client.MatchingLabels{
        drivers.LabelDRPlan:  plan.Name,
        drivers.SiteRoleLabel: targetSiteRole,  // only check target site VRs
    }); err != nil {
        return false, err
    }
    
    for _, vr := range vrList.Items {
        if vr.Status.State != replicationv1alpha1.SecondaryState {
            return false, nil
        }
        completed := meta.FindStatusCondition(vr.Status.Conditions, "Completed")
        if completed == nil || completed.Status != metav1.ConditionTrue {
            return false, nil
        }
    }
    
    // Same check for VGR
    var vgrList replicationv1alpha1.VolumeGroupReplicationList
    if err := r.List(ctx, &vgrList, client.MatchingLabels{
        drivers.LabelDRPlan:  plan.Name,
        drivers.SiteRoleLabel: targetSiteRole,
    }); err != nil {
        return false, err
    }
    
    for _, vgr := range vgrList.Items {
        if vgr.Status.State != replicationv1alpha1.SecondaryState {
            return false, nil
        }
        completed := meta.FindStatusCondition(vgr.Status.Conditions, "Completed")
        if completed == nil || completed.Status != metav1.ConditionTrue {
            return false, nil
        }
    }
    
    // If no VR/VGR found, resync is trivially complete (noop driver)
    return true, nil
}
```

### Implementation Pattern — vrStatusChangePredicate Sharing

The `vrStatusChangePredicate()` and `vrStatusDiffers()` functions currently live in `pkg/controller/drplan/reconciler.go`. They need to be accessible by the DRExecution controller. Options:

1. **Extract to a shared internal package** (e.g., `pkg/controller/internal/predicates/vr.go`) — cleanest, prevents import cycles
2. **Duplicate** in `pkg/controller/drexecution/` — pragmatic, only ~50 lines, avoids refactoring the DRPlan controller

**Recommended: Option 1** — extract `vrStatusChangePredicate()`, `vrStatusDiffers()`, and `lastSyncTimeEqual()` to `pkg/controller/internal/predicates/vr.go`. Both controllers import from there.

### Implementation Pattern — ResyncTimeout on DRPlanSpec

Add alongside `VMReadyTimeout`:

```go
type DRPlanSpec struct {
    // ... existing fields ...
    
    // ResyncTimeout is the maximum duration to wait for VR/VGR resync
    // completion during planned failover Step 0 before declaring a timeout.
    // Only applies to planned_migration mode. Default: 10m.
    // +optional
    // +kubebuilder:default="10m"
    ResyncTimeout *metav1.Duration `json:"resyncTimeout,omitempty"`
}
```

Usage in reconciler:
```go
resyncTimeout := 10 * time.Minute  // default
if plan.Spec.ResyncTimeout != nil {
    resyncTimeout = plan.Spec.ResyncTimeout.Duration
}
```

### Critical Design Decisions

1. **ResyncVolume on TARGET VGs, StopReplication on SOURCE VGs:** The resync pulls data from source→target. Once complete, the source is demoted. Calling them in this order guarantees no data loss.

2. **Event-driven, not polling:** The VR/VGR watch fires when `status.state` or `status.conditions` change. The `RequeueAfter(resyncTimeout)` is a safety net for missed watch events or hung resync — not the primary mechanism.

3. **ResyncPending is execution-scoped:** Goes on `DRExecution.Status.Conditions`, not DRPlan. Multiple plans could exist but only the active execution's resync matters.

4. **Crash recovery (AC7):** On resume, the reconciler sees `ResyncPending=True` and re-evaluates VR status. It does NOT re-call ResyncVolume because:
   - If resync already started, calling again is idempotent (VR already in resync state)
   - The reconciler just checks status — if complete, proceed; if not, wait

5. **StopReplication moves from PreExecute to reconciler:** This is the key architectural change. PreExecute now does StopVM + ResyncVolume (both synchronous, both must succeed). The asynchronous wait (resync completion) and subsequent StopReplication (demote source) are handled by the reconciler's event-driven loop.

6. **Multi-site cross-cluster VR access constraint:** VR/VGR CRs are standard namespaced Kubernetes resources stored in each cluster's local etcd (NOT in ScyllaDB). Each site's controller can only access its own site's VR CRs. This has important implications for the multi-site flow:

   **Multi-site coordination flow (source=east, target=west):**
   ```
   East (RoleStep0):                    West (RoleOwner):
   ─────────────────                    ─────────────────
   1. StopVM (local VMs)
   2. Set VMsStopped=True condition
      (on DRExecution in ScyllaDB)
   3. Return, wait for ResyncComplete
                                        4. See VMsStopped=True
                                        5. ResyncVolume on LOCAL secondary VRs
                                        6. Set ResyncPending=True
                                        7. VR watch fires when resync completes
                                        8. Remove ResyncPending
                                        9. Set ResyncComplete=True condition
   10. See ResyncComplete=True
   11. StopReplication on LOCAL primary VRs
   12. Set Step0Complete=True
                                        13. See Step0Complete=True
                                        14. Proceed: SetSource + StartVM (waves)
   ```

   New conditions on DRExecution (all visible cross-site via ScyllaDB):
   - `VMsStopped` — source site signals VMs are stopped, safe to resync
   - `ResyncPending` — target site is waiting for resync to complete
   - `ResyncComplete` — target site confirms all VRs have completed resync
   - `Step0Complete` — existing condition, source site confirms full Step 0 done

   **Single-site flow (LocalSite==""):** The controller has access to ALL VR CRs (single cluster). The reconciler calls ResyncVolume on secondary VRs, waits for completion, then calls StopReplication on primary VRs — all locally. Uses `ResyncPending` and `Step0Complete` conditions (no need for `VMsStopped`/`ResyncComplete` since no cross-site coordination).

### File Inventory

| File | Action | Lines Changed (est.) |
|------|--------|---------------------|
| `pkg/engine/failover.go` | Modify PreExecute: add ResyncVolume, remove StopReplication | ~30 |
| `pkg/engine/failover_test.go` | Update PreExecute tests | ~60 |
| `pkg/controller/drexecution/reconciler.go` | Add resync gate, VR/VGR watch, mapVRToDRExecution, multi-site coordination | ~200 |
| `pkg/controller/drexecution/reconciler_test.go` | Add resync gate + watch + multi-site coordination tests | ~200 |
| `pkg/controller/internal/predicates/vr.go` | Extract vrStatusChangePredicate (new file) | ~60 |
| `pkg/controller/internal/predicates/vr_test.go` | Extract predicate tests (new file) | ~40 |
| `pkg/controller/drplan/reconciler.go` | Import shared predicate, remove local defs | ~-50 |
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add ResyncTimeout to DRPlanSpec | ~8 |
| `pkg/controller/drexecution/doc.go` | Update architecture doc | ~10 |
| `pkg/engine/doc.go` | Update PreExecute description | ~5 |
| `_bmad-output/project-context.md` | Update unified handler model | ~5 |
| `config/samples/` | Update sample CRD | ~2 |

**Total: ~8 modified files, ~2 new files, ~570 lines changed**

### RBAC Requirements

Add to `pkg/controller/drexecution/reconciler.go`:

```go
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumereplications,verbs=get;list;watch
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumegroupreplications,verbs=get;list;watch
```

These are watch-only — the DRExecution controller reads VR/VGR status but never writes to them (the CSI extension driver handles writes via the StorageProvider interface).

### Compilation Guardrails

Adding `ResyncTimeout` to `DRPlanSpec` requires:
1. `make manifests generate` — regenerates CRD YAML and DeepCopy
2. Fixture sweep in test files that construct `DRPlanSpec` — the new field is optional (`omitempty`) so no breakage expected
3. Console plugin TypeScript interface update — non-breaking (optional field)

Extracting predicates to a shared package requires:
1. Update `pkg/controller/drplan/reconciler.go` imports
2. Ensure no import cycles between `pkg/controller/drplan/` and `pkg/controller/drexecution/`

### Domain Context — Resync in Ceph RBD Mirroring

When a VR is in `secondary` state and you set `spec.replicationState = resync`:
1. CSI Addons sidecar tells Ceph to pull any un-replicated journal entries from the peer primary
2. During resync: `status.state=Secondary`, `conditions[Completed].status=False`
3. After full sync: `status.state=Secondary`, `conditions[Completed].status=True`
4. The secondary now has all data from the primary — zero data loss guaranteed for promotion

Time to complete resync depends on:
- Amount of unreplicated data (delta since last sync)
- Network bandwidth between sites
- Ceph cluster I/O load

For small deltas: seconds. For large deltas: minutes. The 10m default timeout is generous but configurable.

### Previous Story Intelligence

Story 15.1 added `ResyncVolume` to the StorageProvider interface:
- `pkg/drivers/interface.go` — 7-method interface now includes ResyncVolume
- `pkg/drivers/csiextension/driver.go` — `ResyncVolume` calls `updateReplicationState(ctx, set, ReplicationStateResync)` via `listCRsForVG`
- `pkg/drivers/noop/driver.go` — no-op (returns nil for existing VGs)
- `pkg/drivers/fake/driver.go` — `OnResyncVolume` + call recording
- `pkg/drivers/conformance/suite.go` — lifecycle + idempotency + error tests

Key patterns from 15.1:
- ResyncVolume follows SetSource/StopReplication pattern exactly
- The `listCRsForVG` helper handles both VR and VGR transparently
- `ReplicationStateResync` constant already existed in `pkg/drivers/csiextension/constants.go`

### Key Constraints

- **Do NOT poll for resync completion** — use the VR/VGR watch for event-driven notification. The `RequeueAfter` is a safety net only
- **Do NOT promote (SetSource) until resync is confirmed complete** — this is the core invariant protecting against data loss
- **Do NOT modify the DRPlan controller's VR/VGR watch** — Story 13.4's watch remains unchanged; the DRExecution controller adds its own independent watch with a different event mapper
- **Do NOT block on ResyncVolume** — the driver call returns immediately after patching `spec.replicationState`. The actual data sync is performed by the CSI Addons sidecar asynchronously
- **Noop driver path must work** — when using noop driver, no VR/VGR CRs exist. `checkResyncComplete` should return true immediately when no matching VR/VGR are found (trivially complete)
- **Disaster failover must be unaffected** — `GracefulShutdown=false` skips PreExecute entirely, no ResyncVolume call

### Testing Strategy

**Unit tests (pkg/engine/failover_test.go):**
- `TestPreExecute_PlannedMigration_CallsResyncVolume` — verifies ResyncVolume called on all target VGs
- `TestPreExecute_PlannedMigration_DoesNotCallStopReplication` — StopReplication not called (moved to reconciler)
- `TestPreExecute_Disaster_SkipsResyncVolume` — disaster path unchanged

**Unit tests (pkg/controller/drexecution/reconciler_test.go):**
- `TestResyncGate_Complete_Proceeds` — VR status shows resync complete → removes condition, calls StopReplication
- `TestResyncGate_Incomplete_Waits` — VR status not ready → returns RequeueAfter
- `TestResyncGate_Timeout_FailsExecution` — timeout fires before completion → fails with ResyncTimeout
- `TestResyncGate_Resume_ReEvaluates` — crash recovery re-checks VR status
- `TestMapVRToDRExecution_ActiveExecution` — maps VR event to active exec
- `TestMapVRToDRExecution_NoActiveExecution` — no enqueue when no active exec
- `TestMapVRToDRExecution_NoPlanLabel` — no enqueue when label missing

**Integration tests (envtest):**
- Full planned migration with ResyncPending → completion flow
- Timeout path
- Disaster failover (resync NOT called)

### Project Structure Notes

- All files are in existing packages — no new directories except `pkg/controller/internal/predicates/` (new shared predicates package)
- `pkg/controller/internal/` is unexported — safe from external import issues
- The resync guard follows the exact same condition-based gating pattern as `Step0Complete` and `WaitingForVMReady`
- RBAC markers follow the existing pattern in the file (watch-only for VR/VGR)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.2]
- [Source: pkg/engine/failover.go — PreExecute Step 0 implementation]
- [Source: pkg/controller/drexecution/reconciler.go — DRExecution controller, SetupWithManager, mapVMToDRExecution]
- [Source: pkg/controller/drplan/reconciler.go — vrStatusChangePredicate, vrEventHandler, enqueueForVR patterns]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRExecutionStatus.Conditions, DRPlanSpec.VMReadyTimeout]
- [Source: pkg/drivers/types.go — LabelDRPlan, SiteRoleLabel constants]
- [Source: pkg/drivers/csiextension/constants.go — SiteRolePrimary/Secondary, LabelDRPlan re-export]
- [Source: _bmad-output/implementation-artifacts/15-1-resyncvolume-driver-method-csi-extension.md — Previous story]
- [Source: _bmad-output/project-context.md — StorageProvider Driver Framework, unified handler model]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
