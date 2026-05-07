# Story 9.3: Cross-Site Disk Agreement Validation

Status: done

## Story

As a platform engineer,
I want the system to validate that each VM has the same disk topology (count, names, storage classes) across both sites,
So that executions never proceed when storage layout is inconsistent between sites.

## Acceptance Criteria

1. **AC1 — Per-VM disk topology comparison:** When both `primarySiteDiscovery` and `secondarySiteDiscovery` are populated with disk data, the active-site reconciler compares each VM present on both sites: disk count, sorted disk names, and per-disk `storageClass`. The `pvcName` field is explicitly excluded from the comparison (PVC names may differ across sites due to CDI cloning, DataVolume imports, etc.).

2. **AC2 — DisksConsistent=True when disks match:** When all VMs have matching disk topology across both sites, a new condition `DisksConsistent` is set to `True` with reason `DisksAgreed`.

3. **AC3 — DisksConsistent=False on mismatch:** When one or more VMs have mismatched disk topology (different count, missing disks, or different storage classes), `DisksConsistent` is set to `False` with reason `DiskMismatch`. The condition message lists the delta per VM (e.g., "VM default/web01: primary disks [rootdisk(ceph-rbd), datadisk(ceph-rbd)] vs secondary disks [rootdisk(ceph-rbd)] — count mismatch"). Messages are capped using the existing `writeCappedList` pattern. `Ready` transitions to `False`.

4. **AC4 — WaitingForDiskDiscovery:** When disk data is not yet available from one or both sites (disks nil/empty on `DiscoveredVM`), `DisksConsistent` is `False` with reason `WaitingForDiskDiscovery`. The reconciler proceeds with wave formation (same pattern as `WaitingForDiscovery` for VMs — no blocking on first deploy).

5. **AC5 — Admission gate:** When a DRPlan has `DisksConsistent=False`, the admission layer rejects DRExecution CREATE: "Cannot start execution: disk topology does not match across sites. Resolve disk differences first." This applies to all execution modes.

6. **AC6 — Tests:** Table-driven tests cover: disks match, disk count mismatch, disk name mismatch, storage class mismatch, mixed mismatches, one side no disks yet, VMs with empty disks (no-PVC VMs). Admission tests verify rejection when `DisksConsistent=False`. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [x] Task 1: Add condition constants for DisksConsistent (AC: #2, #3, #4)
  - [x] 1.1 Add `conditionTypeDisksConsistent`, `reasonDisksAgreed`, `reasonDiskMismatch`, `reasonWaitingForDiskDiscovery`, `reasonDisksOutOfSync` constants to `pkg/controller/drplan/reconciler.go` alongside the existing `conditionTypeSitesInSync` constants

- [x] Task 2: Create `compareDiskTopology` pure function (AC: #1, #2, #3, #4)
  - [x] 2.1 Add `compareDiskTopology(plan, primary, secondary *SiteDiscovery) (consistent bool, condition metav1.Condition)` to `pkg/controller/drplan/reconciler.go`
  - [x] 2.2 Implement waiting-for-disk-discovery check: when either side has VMs with nil/empty disks, return `WaitingForDiskDiscovery`
  - [x] 2.3 Implement per-VM disk comparison: for each VM present on both sites, sort disks by name and compare count, names, and storageClass (exclude pvcName)
  - [x] 2.4 Build delta message per mismatched VM using `writeCappedList` pattern
  - [x] 2.5 Return `DisksAgreed` when all VMs match, `DiskMismatch` when any differ

- [x] Task 3: Create `evaluateDiskAgreement` reconciler method (AC: #1, #3, #4)
  - [x] 3.1 Add `evaluateDiskAgreement(ctx, req, plan) (*metav1.Condition, bool, error)` following exact pattern of `evaluateSiteAgreement`
  - [x] 3.2 Skip when `LocalSite == ""` or both SiteDiscovery are nil
  - [x] 3.3 On `DiskMismatch`: clear waves, set `Ready=False` with `reasonDisksOutOfSync`, set `DisksConsistent=False`, patch preflight, emit warning event, return blocked=true
  - [x] 3.4 On `WaitingForDiskDiscovery`: log V(1), return blocked=false (proceed with wave formation)
  - [x] 3.5 On `DisksAgreed`: emit recovery event if previously `False`, return blocked=false

- [x] Task 4: Wire disk agreement into reconciler flow (AC: #1, #2, #3, #4)
  - [x] 4.1 Call `evaluateDiskAgreement` after `evaluateSiteAgreement` in `Reconcile` (only if not blocked by site agreement)
  - [x] 4.2 Pass `DisksConsistent` condition through to `updateStatus` alongside `SitesInSync`
  - [x] 4.3 Extend `updateStatus` signature to accept `disksConsistentCond ...metav1.Condition` (or restructure the variadic to accept multiple optional conditions)
  - [x] 4.4 Add `detectDisksConsistentChange` (follow `detectSitesInSyncChange` pattern)
  - [x] 4.5 Include `disksConsistentChanged` in the `anyChanged` check

- [x] Task 5: Add preflight enrichment for DisksConsistent (AC: #3)
  - [x] 5.1 Add `DisksConsistent bool` and `DiskDiscoveryDelta string` fields to `PreflightReport` in `pkg/apis/soteria.io/v1alpha1/types.go`
  - [x] 5.2 Enrich preflight on the success path (parallel to `SitesInSync` enrichment at lines 304-314)
  - [x] 5.3 Enrich preflight on the mismatch early-patch path inside `evaluateDiskAgreement`
  - [x] 5.4 Run `make manifests generate`

- [x] Task 6: Add admission gate for DisksConsistent (AC: #5)
  - [x] 6.1 Add `DisksConsistent=False` check to `DRExecutionValidator.Handle` in `pkg/admission/drexecution_validator.go` — immediately after the existing `SitesInSync` check (line ~117)
  - [x] 6.2 Rejection message: "Cannot start execution: disk topology does not match across sites. Resolve disk differences first."

- [x] Task 7: Unit tests for `compareDiskTopology` (AC: #6)
  - [x] 7.1 Add tests to `pkg/controller/drplan/reconciler_test.go` following the `TestCompareSiteDiscovery_*` pattern:
    - All VMs disks match (different PVC names OK) → `DisksAgreed`
    - Disk count mismatch on one VM → `DiskMismatch` with delta message
    - Disk name mismatch → `DiskMismatch`
    - Storage class mismatch → `DiskMismatch`
    - Mixed mismatches (count + storageClass on different VMs) → `DiskMismatch`
    - One side has no disks yet → `WaitingForDiskDiscovery`
    - Both sides no disks → `WaitingForDiskDiscovery`
    - VMs with empty disks on both sides (no-PVC VMs) → `DisksAgreed` (empty == empty is valid)
    - VMs only on one site (set mismatch) → comparison only runs on intersection, `SitesInSync` handles the rest

- [x] Task 8: Reconciler integration tests for disk agreement (AC: #6)
  - [x] 8.1 Add `TestReconcile_DisksConsistent_*` tests following `TestReconcile_SitesInSync_*` pattern:
    - Disks match → `DisksConsistent=True`, waves formed, `Ready=True`
    - Disk mismatch → `DisksConsistent=False`, waves cleared, `Ready=False`
    - Waiting for disk discovery → `DisksConsistent=False` (WaitingForDiskDiscovery), waves formed, `Ready=True`

- [x] Task 9: Admission tests for DisksConsistent (AC: #6)
  - [x] 9.1 Add `TestDRExecutionValidator_RejectWhenDisksInconsistent` to `pkg/admission/drexecution_validator_test.go` following `TestDRExecutionValidator_RejectWhenSitesOutOfSync` pattern
  - [x] 9.2 Add `TestDRExecutionValidator_AllowWhenDisksConsistent` following `TestDRExecutionValidator_AllowWhenSitesInSync` pattern
  - [x] 9.3 Add test verifying both `SitesInSync=True` AND `DisksConsistent=True` required for admission allow

- [x] Task 10: Run make manifests generate, lint, test (AC: #6)
  - [x] 10.1 `make manifests generate` — zero errors
  - [x] 10.2 `make lint-fix` — zero new lint errors
  - [x] 10.3 `make test` — all tests pass, zero regressions

### Review Findings

- [x] [Review][Patch] Add envtest coverage for the new disk-agreement reconcile paths [pkg/controller/drplan/reconciler_test.go:1865]

## Dev Notes

### Scope & Approach

This is a Go backend story. All changes are in `pkg/controller/drplan/`, `pkg/admission/`, and `pkg/apis/soteria.io/v1alpha1/`. No console-plugin changes. This story adds a **second cross-site validation dimension** (disk topology) alongside the existing VM set agreement (`SitesInSync`).

**Change pattern:** Add condition constants → create comparison function → create evaluator method → wire into reconciler → add admission gate → add preflight fields → tests → regenerate manifests.

**Prerequisite:** Story 9.1 must be implemented first. This story assumes `DiscoveredVM` has a `Disks []DiscoveredDisk` field with `name`, `pvcName`, and `storageClass` fields.

### Critical: This Story Does NOT Block Wave Formation on WaitingForDiskDiscovery

Same pattern as `SitesInSync` with `WaitingForDiscovery` — disk data may not be available on first deploy or when a new VM is added. The reconciler proceeds with wave formation and only blocks when there is an **active mismatch** (`DiskMismatch`). This avoids deadlocking the first-deploy scenario.

### Critical: DisksConsistent is a Separate Condition from SitesInSync

`SitesInSync` validates VM **presence** (same set of `{namespace/name}` tuples). `DisksConsistent` validates disk **topology** (for VMs present on both sites, do they have the same disk layout?). These are orthogonal checks:

- `SitesInSync=True, DisksConsistent=True` → healthy
- `SitesInSync=True, DisksConsistent=False` → VMs match but disk topology differs
- `SitesInSync=False` → VM set mismatch (disk check may still run on intersection, or be skipped)

When `SitesInSync` is `False` with `VMsMismatch`, `evaluateSiteAgreement` already blocks and returns early. The disk check only runs after site agreement passes (or when `SitesInSync` is `WaitingForDiscovery` / `VMsAgreed`).

### Critical: Comparison Logic — pvcName Excluded, storageClass Included

For each VM in the intersection of both sites' discovery:
1. Sort disks by `Name` (deterministic order)
2. Compare disk count
3. Compare sorted disk names
4. Compare `storageClass` per disk (by name match)
5. **Explicitly exclude `pvcName`** — PVC names may differ across sites (CDI cloning, DataVolume imports create different PVC names)

```go
func compareDiskTopology(
    plan *soteriav1alpha1.DRPlan,
    primary, secondary *soteriav1alpha1.SiteDiscovery,
) (consistent bool, condition metav1.Condition) {
    // ... build VM lookup maps: namespace/name → []DiscoveredDisk
    // ... for each VM in intersection:
    //     sort primary disks by name, sort secondary disks by name
    //     compare count, then name-by-name, then storageClass-by-name
    //     if mismatch: record delta entry
}
```

### Critical: Delta Message Format

Follow the epic requirement exactly:

```
VM default/web01: primary disks [rootdisk(ceph-rbd), datadisk(ceph-rbd)] vs secondary disks [rootdisk(ceph-rbd)] — count mismatch
```

For storage class mismatch:
```
VM default/db01: disk "datadisk" storage class mismatch: primary=ceph-rbd, secondary=local-path
```

Cap per-VM delta entries using `writeCappedList` (existing `maxDeltaEntriesPerSide = 20`).

### Critical: Handling VMs with No Disks (No-PVC VMs)

A VM with only non-PVC volumes (containerDisk, cloudInitNoCloud) has empty/nil `Disks` on both sites — this is **valid and consistent**. Empty == empty. The comparison function should treat two VMs with nil/empty disks as "disks match" for that VM.

### Critical: WaitingForDiskDiscovery Detection

"Disk data not yet available" means at least one VM on at least one site has `nil`/empty `Disks` while the same VM on the other site has populated disks. This indicates the DiskEnricher hasn't run yet on one site.

Edge case: if a VM genuinely has no PVC-backed disks (only containerDisk), its disks are empty on both sites — this is NOT waiting for discovery; it's a valid match (empty == empty).

Detection logic:
```go
for _, vm in intersection {
    primaryDisks := primaryDiskMap[key]
    secondaryDisks := secondaryDiskMap[key]
    if (len(primaryDisks) > 0 && len(secondaryDisks) == 0) ||
       (len(primaryDisks) == 0 && len(secondaryDisks) > 0) {
        // One side has disks, other doesn't — waiting for discovery
        waitingVMs = append(waitingVMs, key)
    }
}
```

If all intersection VMs have `len(primaryDisks) == 0 && len(secondaryDisks) == 0` (all VMs are diskless), treat as `DisksAgreed` — there are no disks to disagree on.

### Critical: evaluateDiskAgreement Follows evaluateSiteAgreement Exactly

The `evaluateDiskAgreement` method must follow the exact pattern of `evaluateSiteAgreement` (lines 413-487 of `reconciler.go`):

1. Skip when `LocalSite == ""` or both SiteDiscovery nil → return `nil, false, nil`
2. Call `compareDiskTopology(plan, primary, secondary)`
3. On `DiskMismatch`:
   - Log warning
   - Emit event if new mismatch (check previous condition)
   - Re-fetch plan (`r.Get`)
   - Patch with `client.MergeFrom`: clear waves, set `Ready=False`/`reasonDisksOutOfSync`, set `DisksConsistent=False`, enrich preflight
   - Return `&cond, true, nil`
4. On `WaitingForDiskDiscovery`:
   - Log V(1)
   - Return `&cond, false, nil` (proceed with wave formation)
5. On `DisksAgreed`:
   - Log V(1)
   - Emit recovery event if previously `False`
   - Return `&cond, false, nil`

### Critical: updateStatus Signature Change

The current `updateStatus` uses `sitesInSyncCond ...metav1.Condition` as a variadic parameter. For `DisksConsistent`, there are two approaches:

**Option A — Expand variadic to accept multiple conditions:** The variadic already only uses `sitesInSyncCond[0]`. Extend it to also check `[1]` for `DisksConsistent`. Callers pass 0, 1, or 2 optional conditions. Apply `detectSitesInSyncChange` on `[0]` and `detectDisksConsistentChange` on `[1]`.

**Option B — Separate parameter:** Add `disksConsistentCond *metav1.Condition` as a named parameter. Cleaner but requires updating all call sites.

**Recommended: Option A** — keeps the variadic pattern and minimizes call-site changes. Rename the variadic parameter to `extraConditions ...metav1.Condition` and iterate over them. Both `SitesInSync` and `DisksConsistent` conditions use `meta.SetStatusCondition` which is keyed by `Type` — duplicates are impossible.

Implementation:
```go
func (r *DRPlanReconciler) updateStatus(
    ctx context.Context, req ctrl.Request, plan *soteriav1alpha1.DRPlan,
    waves []soteriav1alpha1.WaveInfo, totalVMs int,
    preflightReport *soteriav1alpha1.PreflightReport,
    condition metav1.Condition,
    replicationHealth []soteriav1alpha1.VolumeGroupHealth,
    extraConditions ...metav1.Condition,
) (ctrl.Result, error) {
    // ...
    extraCondChanged := detectExtraConditionChanges(plan.Status.Conditions, extraConditions)
    // ...
    for _, ec := range extraConditions {
        meta.SetStatusCondition(&plan.Status.Conditions, ec)
    }
}
```

And refactor `detectSitesInSyncChange` → `detectExtraConditionChanges` to iterate all incoming conditions:
```go
func detectExtraConditionChanges(existing []metav1.Condition, incoming []metav1.Condition) bool {
    for _, inc := range incoming {
        old := meta.FindStatusCondition(existing, inc.Type)
        if old == nil || old.Status != inc.Status || old.Reason != inc.Reason || old.Message != inc.Message {
            return true
        }
    }
    return false
}
```

### Critical: Reconcile Flow — Where Disk Check Fits

After `evaluateSiteAgreement` returns, add `evaluateDiskAgreement`:

```go
// Lines 156-162 of reconciler.go:
sitesInSyncCond, blocked, err := r.evaluateSiteAgreement(ctx, req, &plan)
if err != nil { return ctrl.Result{}, err }
if blocked { return ctrl.Result{RequeueAfter: 30 * time.Second}, nil }

// NEW: Disk topology agreement check
disksConsistentCond, diskBlocked, err := r.evaluateDiskAgreement(ctx, req, &plan)
if err != nil { return ctrl.Result{}, err }
if diskBlocked { return ctrl.Result{RequeueAfter: 30 * time.Second}, nil }

// ... rest of reconcile unchanged, pass both conditions to updateStatus ...
```

On the success path (lines 316-319), pass both conditions:
```go
var extraConds []metav1.Condition
if sitesInSyncCond != nil { extraConds = append(extraConds, *sitesInSyncCond) }
if disksConsistentCond != nil { extraConds = append(extraConds, *disksConsistentCond) }
return r.updateStatus(ctx, req, &plan, waves, result.TotalVMs, report, readyCond, replicationHealth, extraConds...)
```

### Critical: Admission Gate Implementation

Add immediately after the `SitesInSync` check in `DRExecutionValidator.Handle` (line ~117):

```go
// Reject when disk topology does not match across sites.
if dcCond := meta.FindStatusCondition(plan.Status.Conditions, "DisksConsistent"); dcCond != nil &&
    dcCond.Status == metav1.ConditionFalse {
    return admission.Denied(
        "Cannot start execution: disk topology does not match across sites. Resolve disk differences first.")
}
```

**Note:** Story 9.2 (admission plugin migration) may or may not be implemented by the time this story is worked. If 9.2 is complete, add the `DisksConsistent` check to the new `SoteriaAdmissionPlugin.Validate` method instead of `DRExecutionValidator.Handle`. If 9.2 is not yet implemented, add to the existing webhook handler. Check which admission path is active before implementing.

### Critical: PreflightReport API Fields

Add two new fields to `PreflightReport` in `types.go` (parallel to `SitesInSync`/`SiteDiscoveryDelta`):

```go
// DisksConsistent indicates whether all VMs have matching disk topology
// across primary and secondary sites. Only meaningful in site-aware mode
// when disk discovery has been performed on both sites.
DisksConsistent bool `json:"disksConsistent"`
// DiskDiscoveryDelta describes per-VM disk topology differences between
// sites when DisksConsistent is false (omitted when disks agree).
DiskDiscoveryDelta string `json:"diskDiscoveryDelta,omitempty"`
```

Run `make manifests generate` after adding these fields.

### Critical: Error Handling

Disk topology comparison is a pure function — it cannot fail. The `evaluateDiskAgreement` wrapper handles the `r.Get` and `r.Status().Patch` errors the same way `evaluateSiteAgreement` does.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `compareSiteDiscovery(plan, primary, secondary)` | `reconciler.go:489-595` | Follow for `compareDiskTopology` |
| `evaluateSiteAgreement(ctx, req, plan)` | `reconciler.go:413-487` | Follow for `evaluateDiskAgreement` |
| `writeCappedList(b, items, max)` | `reconciler.go:590-599` | Reuse directly for delta messages |
| `conditionTypeSitesInSync` + reasons | `reconciler.go:76-81` | Follow for `conditionTypeDisksConsistent` + reasons |
| `detectSitesInSyncChange(existing, incoming)` | `reconciler.go:1009-1019` | Follow for `detectExtraConditionChanges` |
| `updateStatus(..., sitesInSyncCond)` | `reconciler.go:853-965` | Extend variadic to accept DisksConsistent |
| `meta.FindStatusCondition(conditions, "SitesInSync")` | `drexecution_validator.go:112-116` | Follow for `DisksConsistent` admission check |
| `TestCompareSiteDiscovery_*` | `reconciler_test.go:1441-1597` | Follow for `TestCompareDiskTopology_*` |
| `TestDRExecutionValidator_RejectWhenSitesOutOfSync` | `drexecution_validator_test.go:339-382` | Follow for `DisksConsistent` rejection test |
| Preflight enrichment (SitesInSync) | `reconciler.go:304-314` | Follow for `DisksConsistent` enrichment |
| `newSiteDiscovery(vms...)` helper | `reconciler_test.go:1443-1449` | Extend to accept `DiscoveredVM` with disks |
| `newReconcilerWithSite(objs, discoverer, site)` | `reconciler_test.go:1601-1621` | Reuse for disk agreement integration tests |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/controller/drplan/reconciler.go` | Add constants, `compareDiskTopology`, `evaluateDiskAgreement`, refactor `updateStatus` variadic, wire into `Reconcile` | ~120 lines added, ~15 lines modified |
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `DisksConsistent` + `DiskDiscoveryDelta` fields to `PreflightReport` | ~6 lines |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-generated | `make generate` |
| `pkg/admission/drexecution_validator.go` | Add `DisksConsistent=False` admission check | ~5 lines |
| `pkg/controller/drplan/reconciler_test.go` | Add `TestCompareDiskTopology_*` and `TestReconcile_DisksConsistent_*` tests | ~200 lines |
| `pkg/admission/drexecution_validator_test.go` | Add `DisksConsistent` rejection/allow tests | ~80 lines |
| `config/crd/bases/soteria.io_drplans.yaml` | Auto-generated (new preflight fields) | `make manifests` |

### Execution Order

1. Task 5.1 (API types — preflight fields) → `make manifests generate`
2. Task 1 (condition constants)
3. Task 2 (`compareDiskTopology` pure function)
4. Task 7 (unit tests for comparison function — validate before wiring)
5. Task 3 (`evaluateDiskAgreement` method)
6. Task 4 (wire into reconciler + refactor `updateStatus`)
7. Task 8 (reconciler integration tests)
8. Task 6 (admission gate)
9. Task 9 (admission tests)
10. Task 10 (manifests, lint, full test suite)

### Previous Story Learnings (from 9.1 and 9.2)

- **Self-healing via reconcile loop** — partial/missing disk data fills in on subsequent cycles; no explicit retry needed
- **`collectVMsFromWaves` propagates all VM data** — disk enrichment in waves automatically propagates to active-site SiteDiscovery
- **Admission checks use `meta.FindStatusCondition` by string type** — condition type constants in the reconciler are package-private strings; the admission layer uses the same literal string `"DisksConsistent"`
- **9.2 may or may not be merged** — check whether admission is via webhook (`drexecution_validator.go`) or in-process plugin (`plugin.go`) before implementing AC5
- **`compareSiteDiscovery` only checks VM presence** — it does NOT check disk topology; this story adds the disk-level validation dimension

### Project Structure Notes

- API types live in `pkg/apis/soteria.io/v1alpha1/types.go` (sample-apiserver pattern, not kubebuilder `api/` convention)
- Controllers live in `pkg/controller/drplan/` (not `internal/controller/`)
- Admission webhooks in `pkg/admission/` — condition check added here
- Generated files: `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml` — never hand-edit
- Run `make manifests generate` after any `types.go` change

### References

- [Source: pkg/controller/drplan/reconciler.go#L68-L85] — Condition type/reason constants (add DisksConsistent constants here)
- [Source: pkg/controller/drplan/reconciler.go#L413-L487] — evaluateSiteAgreement (follow for evaluateDiskAgreement)
- [Source: pkg/controller/drplan/reconciler.go#L489-L599] — compareSiteDiscovery + writeCappedList (follow for compareDiskTopology)
- [Source: pkg/controller/drplan/reconciler.go#L151-L162] — Reconcile flow: site agreement check (add disk check after)
- [Source: pkg/controller/drplan/reconciler.go#L296-L319] — Success path: preflight enrichment + updateStatus (enrich + pass disk condition)
- [Source: pkg/controller/drplan/reconciler.go#L853-L965] — updateStatus (extend variadic for DisksConsistent)
- [Source: pkg/controller/drplan/reconciler.go#L1009-L1019] — detectSitesInSyncChange (refactor to generic detectExtraConditionChanges)
- [Source: pkg/admission/drexecution_validator.go#L112-L116] — SitesInSync admission gate (add DisksConsistent gate after)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L143-L172] — PreflightReport (add DisksConsistent + DiskDiscoveryDelta)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L250-L269] — DiscoveredVM + SiteDiscovery types (Disks field from 9.1)
- [Source: pkg/controller/drplan/reconciler_test.go#L1441-L1597] — compareSiteDiscovery unit tests (follow for disk topology tests)
- [Source: pkg/controller/drplan/reconciler_test.go#L1599-L1640] — Reconciler agreement integration tests (follow for disk tests)
- [Source: pkg/admission/drexecution_validator_test.go#L339-L424] — SitesInSync admission tests (follow for DisksConsistent tests)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-9.3] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, naming conventions, testing pyramid

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor)

### Debug Log References

None — clean implementation with no debugging needed.

### Completion Notes List

- Added `DisksConsistent` and `DiskDiscoveryDelta` fields to PreflightReport (types.go)
- Added condition constants: `conditionTypeDisksConsistent`, `reasonDisksAgreed`, `reasonDiskMismatch`, `reasonWaitingForDiskDiscovery`, `reasonDisksOutOfSync`
- Implemented `compareDiskTopology` pure function with helper `compareDiskSets` and `formatDiskList` — per-VM disk comparison excluding pvcName, checking count/names/storageClass
- Implemented `evaluateDiskAgreement` method following exact pattern of `evaluateSiteAgreement` — blocks on DiskMismatch, proceeds on WaitingForDiskDiscovery, emits recovery events
- Wired disk agreement check into Reconcile flow after site agreement check
- Refactored `detectSitesInSyncChange` → `detectExtraConditionChanges` (generalized for multiple extra conditions)
- Refactored updateStatus variadic condition application to iterate all extra conditions
- Enriched preflight report with disk consistency data on both success and mismatch paths
- Added DisksConsistent admission gate to both DRExecutionValidator.Handle (webhook) and SoteriaAdmissionPlugin.validateDRExecution (aggregated API plugin)
- 11 unit tests for compareDiskTopology covering all scenarios (match, count/name/storageClass mismatch, mixed, waiting, nil, empty, one-side-only VMs)
- 3 reconciler integration tests (disks consistent → Ready=True; mismatch → Ready=False/waves cleared; waiting → proceeds normally)
- 5 admission tests (reject inconsistent, allow consistent, require both SitesInSync+DisksConsistent for webhook; reject/allow for plugin)
- Coverage: pkg/controller/drplan 85.8% → 87.8%, pkg/admission 87.1% → 87.4%
- Zero regressions in unit tests, zero regressions in integration tests
- Zero new lint errors (2 pre-existing: goconst in executor_test.go, unparam in writeCappedList)

### File List

- `pkg/apis/soteria.io/v1alpha1/types.go` — Added DisksConsistent + DiskDiscoveryDelta fields to PreflightReport
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — Auto-generated (make generate)
- `pkg/controller/drplan/reconciler.go` — Added constants, compareDiskTopology, evaluateDiskAgreement, refactored updateStatus/detectExtraConditionChanges, wired into Reconcile
- `pkg/admission/drexecution_validator.go` — Added DisksConsistent=False admission check
- `pkg/admission/plugin.go` — Added DisksConsistent=False admission check
- `pkg/controller/drplan/reconciler_test.go` — Added 11 compareDiskTopology unit tests + 3 reconciler integration tests
- `pkg/admission/drexecution_validator_test.go` — Added 3 DisksConsistent admission tests
- `pkg/admission/plugin_test.go` — Added 2 DisksConsistent plugin admission tests
