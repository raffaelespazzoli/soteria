# Story 15.12: Event-Driven Discovery — Eliminate Unconditional Status Writes & LWW Race

Status: ready-for-dev

## Context

E2e testing of the disaster-snapshot lifecycle (Story 15.9) exposed a ScyllaDB Last-Write-Wins (LWW) race during east recovery after a disaster failover. The root cause is that the DRPlan reconciler unconditionally writes plan status on every reconcile cycle — even when nothing has changed — because `siteDiscoveryDue` is always `true` in site-aware mode.

### Root Cause Analysis

When east restarts after a disaster:

1. East's controller reads the DRPlan from local ScyllaDB **before** cross-DC sync completes → stale data: `Phase=SteadyState, ActiveSite=east`
2. Since `LocalSite ("east") == ActiveSite ("east")`, the reconciler enters the active-site path
3. The active-site reconciler runs `updateStatus`, which always writes because `siteDiscoveryDue` is unconditionally `true` (line 1582: `siteDiscoveryDue := r.LocalSite != "" && r.siteDiscoveryField(plan) != ""`)
4. The status patch goes through the aggregated API server, which reads the stale plan, applies the merge patch, and writes the **full plan blob** back to ScyllaDB via `LOCAL_SERIAL` CAS
5. The `CriticalFieldDetector` doesn't trigger cross-DC SERIAL because Phase/ActiveSite didn't change *from east's stale perspective*
6. East's new write (stale data, new timestamp) overwrites west's correct `Phase=FailedOver` via LWW replication
7. The subsequent reprotect execution is rejected: "cannot reprotect from phase SteadyState"

### Why `siteDiscoveryDue` Is Always True

`siteDiscoveryDue` bypasses the change-detection guard in `updateStatus` because it does not compare old vs new values — it simply checks "am I in site-aware mode?" The intent was to keep `SiteDiscovery.LastDiscoveryTime` fresh so the cross-site agreement check could detect controller liveness. However:

- `LastDiscoveryTime` is set to `metav1.Now()` on every cycle (line 1628), guaranteeing a diff
- This causes a ScyllaDB write on every 10-minute requeue even when no VMs changed
- The write includes the full plan blob with potentially stale Phase/ActiveSite values

### Secondary Issue: Missing VM Disk Change Watch

The `vmRelevantChangePredicate` (line 1721) only fires on label changes. Disk additions to a VM (which require new VR/VGR creation) are not detected until the next 10-minute requeue. With event-driven discovery, disk changes should trigger immediate reconciliation.

## Story

As a DR operator,
I want the DRPlan controller to write status only when actual differences are detected,
so that a recovering site's controller does not clobber the surviving site's correct plan state via ScyllaDB LWW.

## Design

### Principle

The reconciler already runs the full pipeline (discovery + health polling) on every invocation regardless of what triggered the wake-up. The 10-minute `requeueInterval` remains as a safety-net for missed events and health polling. The only change is that `updateStatus` and `reconcilePassiveSite` skip the write when nothing has actually changed.

### Change 1: Replace `siteDiscoveryDue` With Actual Comparison

Replace the unconditional flag with a comparison of the discovered VM list against the stored `SiteDiscovery`:

```go
// Before:
siteDiscoveryDue := r.LocalSite != "" && r.siteDiscoveryField(plan) != ""

// After:
siteDiscoveryDue := r.LocalSite != "" && r.siteDiscoveryChanged(plan, waves)
```

New helper `siteDiscoveryChanged(plan, waves)`:
- Extracts the current site's `SiteDiscovery.VMs` from plan status
- Compares against `collectVMsFromWaves(waves)` (sorted)
- Returns `true` only if the VM list actually differs (name/namespace/disk topology)

`LastDiscoveryTime` is updated only when VMs changed.

### Change 2: Same Comparison in Passive Site Path

`reconcilePassiveSite` currently patches unconditionally (line 764-770). Add a comparison of discovered VMs against stored `SiteDiscovery.VMs`. Skip the patch when identical.

### Change 3: Expand `vmRelevantChangePredicate` for Disk Changes

Expand the update predicate to also fire when `vm.Spec.Template.Spec.Volumes` changes (covers disk add/remove):

```go
UpdateFunc: func(e event.UpdateEvent) bool {
    if !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
        return true
    }
    oldVM, ok1 := e.ObjectOld.(*kubevirtv1.VirtualMachine)
    newVM, ok2 := e.ObjectNew.(*kubevirtv1.VirtualMachine)
    if ok1 && ok2 {
        return !reflect.DeepEqual(
            oldVM.Spec.Template.Spec.Volumes,
            newVM.Spec.Template.Spec.Volumes,
        )
    }
    return false
},
```

### What Does NOT Change

- The 10-minute `requeueInterval` (safety net for missed events + health polling)
- The `degradedRequeueInterval` (30s for unhealthy replication)
- The full pipeline execution on every reconcile (discovery + agreement + VR reconciliation + health polling)
- `SiteDiscovery` struct in `types.go` (schema unchanged)
- `CriticalFieldDetector` logic
- Health polling (`pollReplicationHealth`)
- `PreflightReport.GeneratedAt` handling (already stripped by `preflightReportChanged`)
- Condition `LastTransitionTime` handling (already only updates on status change)

## Acceptance Criteria

**AC1: No status write when VMs unchanged**
Given a DRPlan with 6 discovered VMs in SteadyState
When the reconciler runs (via 10-minute requeue) with no VM changes
Then `updateStatus` returns "Status unchanged, skipping patch" (no ScyllaDB write)
And `SiteDiscovery.LastDiscoveryTime` retains its previous value

**AC2: Status write when VMs change**
Given a DRPlan with 6 discovered VMs
When a 7th VM is added with the plan's `soteria.io/drplan` label
Then the reconciler detects the VM list difference
And `updateStatus` writes the updated waves, site discovery, and conditions
And `SiteDiscovery.LastDiscoveryTime` is refreshed to `metav1.Now()`

**AC3: Passive site same behavior**
Given a passive-site reconciler with 6 discovered VMs
When reconciliation runs with no VM changes on the passive site
Then `reconcilePassiveSite` skips the status patch
And when a VM is added on the passive site, the patch is issued

**AC4: VM disk change triggers reconciliation**
Given a VM associated with a DRPlan
When a new disk (PVC-backed volume) is added to the VM spec
Then the `vmRelevantChangePredicate` fires
And the DRPlan reconciler runs and detects the disk topology change

**AC5: LWW race eliminated — disaster-snapshot e2e passes**
Given the disaster-snapshot lifecycle e2e test (east stop → disaster on west → east restart → reprotect)
When east's controller restarts and reads stale plan data from local ScyllaDB
Then the reconciler detects no actual changes (same VMs, same conditions) and skips the write
And the plan's `Phase=FailedOver` / `ActiveSite=west` set by the disaster execution is preserved
And the subsequent reprotect execution succeeds

**AC6: Health polling continues working**
Given a DRPlan with replication health monitoring enabled
When the 10-minute requeue fires
Then `pollReplicationHealth` runs and health data is compared
And a status write occurs only if health actually changed (new degraded VG, recovered VG, or lastSyncTime update)

**AC7: All existing tests pass**
Given the full test suite (`make test`)
When run after the changes
Then all unit and integration tests pass
And `make lint-fix` reports no new lint issues

## Tasks / Subtasks

- [ ] Task 1: Add `siteDiscoveryChanged` helper to `reconciler.go` — compares `collectVMsFromWaves(waves)` (sorted) against stored `SiteDiscovery.VMs` using `reflect.DeepEqual` or field-by-field comparison (AC: 1, 2)
- [ ] Task 2: Replace `siteDiscoveryDue` unconditional flag with `siteDiscoveryChanged` call (AC: 1, 2)
- [ ] Task 3: Conditionally set `LastDiscoveryTime` in `updateStatus` — only when `siteDiscoveryDue` is true (AC: 1, 2)
- [ ] Task 4: Add early-return comparison in `reconcilePassiveSite` — skip patch when discovered VMs match stored `SiteDiscovery.VMs` (AC: 3)
- [ ] Task 5: Expand `vmRelevantChangePredicate` — add `Volumes` comparison for disk add/remove detection (AC: 4)
- [ ] Task 6: Update unit tests in `reconciler_test.go` — add test for "unchanged VMs skip write", update existing tests that assumed unconditional write (AC: 1, 7)
- [ ] Task 7: Add unit test for disk-change predicate in `reconciler_test.go` (AC: 4, 7)
- [ ] Task 8: Run `make lint-fix && make test` (AC: 7)
- [ ] Task 9: Build, deploy, and run disaster-snapshot e2e test (AC: 5, 6)

## Dev Notes

### Key Files to Modify

| File | Change |
|------|--------|
| `pkg/controller/drplan/reconciler.go` | `siteDiscoveryChanged` helper, `siteDiscoveryDue` replacement, `LastDiscoveryTime` conditional, `reconcilePassiveSite` early return, `vmRelevantChangePredicate` expansion |
| `pkg/controller/drplan/reconciler_test.go` | New tests for no-change skip, disk-change predicate; update existing `siteDiscovery` tests |

### Architecture Constraints

- **No API/schema changes**: `SiteDiscovery` struct is unchanged; `LastDiscoveryTime` field remains but is only set on actual VM-list changes
- **Idempotent reconciliation**: The reconciler still runs the full pipeline on every invocation; only the write is gated
- **10-minute requeue preserved**: Safety net for missed events + health polling; `updateStatus` decides whether to write
- **`CriticalFieldDetector` unchanged**: The fix operates upstream — no stale data reaches ScyllaDB, so the detector's scope doesn't need expansion
- **After editing Go**: Run `make lint-fix && make test`

### What NOT to Change

- `types.go` / CRD schema (no `make manifests generate` needed)
- DRExecution reconciler
- Engine / executor
- Console UI
- ScyllaDB storage layer
- Health polling logic (`pollReplicationHealth`, `replicationHealthChanged`)

### Previous Story Intelligence

Story 8.2 introduced `SiteDiscovery` with `LastDiscoveryTime` and the unconditional write pattern. The design assumed that a fresh timestamp was needed for cross-site liveness detection. This story replaces that assumption with actual change detection — the cross-site agreement check (`evaluateSiteAgreement`) already compares VM lists directly and doesn't depend on `LastDiscoveryTime` freshness.

Story 13.4 added VR/VGR watches to `SetupWithManager`. Combined with the existing VM and Namespace watches, event-driven coverage is complete for all discovery-relevant mutations.

### References

- [Source: pkg/controller/drplan/reconciler.go:1582] — `siteDiscoveryDue` unconditional flag
- [Source: pkg/controller/drplan/reconciler.go:1620-1631] — `LastDiscoveryTime` unconditional refresh
- [Source: pkg/controller/drplan/reconciler.go:764-770] — passive-site unconditional patch
- [Source: pkg/controller/drplan/reconciler.go:1718-1736] — `vmRelevantChangePredicate`
- [Source: pkg/apiserver/critical_fields.go:60-71] — `detectDRPlanCriticalFields`
- [Source: pkg/storage/scylladb/store.go:929-968] — `casUpdateWithConsistency` (LWW risk path)
- [Source: _bmad-output/implementation-artifacts/8-2-per-site-vm-discovery-reporting.md] — original SiteDiscovery design
- [Source: _bmad-output/implementation-artifacts/user-acceptance-test-epic-15.md] — e2e test findings
