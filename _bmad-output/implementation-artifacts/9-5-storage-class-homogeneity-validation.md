# Story 9.5: Storage Class Homogeneity Validation

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want the system to validate that all disks within a volume group use the same storage class,
So that volume group operations are guaranteed to target a single storage driver.

## Acceptance Criteria

1. **AC1 — Homogeneous VG passes:** When a volume group's member VMs all have disks using the same `storageClass` (from `DiscoveredVM.Disks`), the validation passes and the `DisksConsistent` condition is not degraded by this check.

2. **AC2 — Heterogeneous VG blocks with StorageClassMixed:** When a volume group has member disks with different storage classes, `DisksConsistent` is set to `False` with reason `StorageClassMixed`. The condition message identifies the VG and conflicting classes (e.g., "Volume group ns-erp-database: mixed storage classes [ceph-rbd, local-path] — all disks must use the same storage class"). `Ready` transitions to `False`. Wave formation proceeds (waves are needed to discover VGs) but execution is blocked via the `DisksConsistent=False` admission gate from Story 9.3.

3. **AC3 — Validation runs after VG formation:** The storage class homogeneity check runs after `ResolveVolumeGroups` produces VGs, iterating all VGs and collecting distinct storage classes from `DiscoveredVM.Disks` for member VMs. If any VG has >1 distinct class, the check fails.

4. **AC4 — Stateless VMs skipped:** VMs with no disks (empty/nil `Disks` — stateless VMs with only containerDisk, cloudInitNoCloud) do not contribute to storage class counting. If a VG has only stateless VMs (all disks empty), validation passes trivially.

5. **AC5 — Tests:** Table-driven unit tests cover: homogeneous VG passes, heterogeneous VG fails with message, VG with mixed plus stateless VMs, namespace-level multi-VM VG (all same SC passes, mixed fails), VG where all VMs are stateless (passes), VG with single VM single SC (passes). Reconciler integration tests verify the condition is set and Ready=False on mixed SC. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [x] Task 1: Add `reasonStorageClassMixed` condition constant (AC: #2)
  - [x] 1.1 Add `reasonStorageClassMixed = "StorageClassMixed"` to the condition constants block in `pkg/controller/drplan/reconciler.go` alongside the `conditionTypeDisksConsistent` constants added by Story 9.3

- [x] Task 2: Create `validateVGStorageClassHomogeneity` pure function (AC: #1, #2, #3, #4)
  - [x] 2.1 Add `validateVGStorageClassHomogeneity(vgs []soteriav1alpha1.VolumeGroupInfo, waves []soteriav1alpha1.WaveInfo) []MixedVGResult` pure function to `pkg/controller/drplan/reconciler.go`
  - [x] 2.2 Add `MixedVGResult` struct: `VGName string`, `Classes []string` (sorted)
  - [x] 2.3 Build a VM lookup map from waves: key = `namespace/name` → `[]DiscoveredDisk`
  - [x] 2.4 For each VG: iterate sorted `VMNames`, collect storage classes from each VM's disks, skip VMs with empty/nil disks
  - [x] 2.5 If a VG has >1 distinct storage class → add to results
  - [x] 2.6 Return empty slice when all VGs are homogeneous

- [x] Task 3: Wire validation into reconciler (AC: #2, #3)
  - [x] 3.1 After `ResolveVolumeGroups` and before the wave conflict check, call `validateVGStorageClassHomogeneity`
  - [x] 3.2 If mixed VGs found: build `metav1.Condition` with `Type=conditionTypeDisksConsistent`, `Status=False`, `Reason=reasonStorageClassMixed`, message from `buildMixedSCMessage` using `writeCappedList`
  - [x] 3.3 Override the `disksConsistentCond` from `evaluateDiskAgreement` (9.3) — cross-site disks may agree but VG SC homogeneity fails
  - [x] 3.4 Compose preflight report (nil chunks — chunking has not run), set `DisksConsistent=false` and `DiskDiscoveryDelta` with the SC violation message on the preflight
  - [x] 3.5 Set `Ready=False` with reason `reasonDisksOutOfSync` (reuse from 9.3)
  - [x] 3.6 Call `updateStatus` with the overridden conditions and return early (same pattern as wave conflict early return)

- [x] Task 4: Unit tests for `validateVGStorageClassHomogeneity` (AC: #5)
  - [x] 4.1 Add tests to `pkg/controller/drplan/reconciler_test.go`:
    - Single VM VG, one SC (`ceph-rbd` on all disks) → empty result (passes)
    - Single VM VG, two SCs (`ceph-rbd` + `local-path`) → MixedVGResult with both classes
    - Namespace-level VG (3 VMs), all same SC → empty result (passes)
    - Namespace-level VG (3 VMs), one VM has different SC → MixedVGResult
    - VG with one VM that has disks + one stateless VM (no disks) → only the VM with disks contributes; single SC passes
    - VG with all stateless VMs (no disks) → empty result (passes trivially)
    - VG with VMs that have empty storageClass (PVC not yet resolved from 9.1 self-healing) → empty strings are excluded from distinct count (or treated as a class — see Dev Notes)
    - Multiple VGs: one homogeneous, one heterogeneous → only the heterogeneous VG in results
    - VG with single VM, single disk, single SC → passes

- [x] Task 5: Reconciler integration tests (AC: #5)
  - [x] 5.1 Add `TestReconcile_StorageClassMixed_BlocksReady` — VG with mixed SCs → `DisksConsistent=False/StorageClassMixed`, `Ready=False`
  - [x] 5.2 Add `TestReconcile_StorageClassHomogeneous_Passes` — all VGs homogeneous → `DisksConsistent` not degraded, `Ready=True`
  - [x] 5.3 Add `TestReconcile_StorageClassMixed_PrefersOverDiskAgreed` — cross-site disks agree (9.3) but VG SCs mixed (9.5) → `DisksConsistent=False/StorageClassMixed` takes precedence

- [x] Task 6: Run make manifests generate, lint, test (AC: #5)
  - [x] 6.1 `make manifests generate` — zero errors
  - [x] 6.2 `make lint-fix` — zero new lint errors
  - [x] 6.3 `make test` — all tests pass, zero regressions

### Review Findings

- [x] [Review][Patch] StorageClassMixed uses a misleading admission error message [`pkg/admission/plugin.go:151`]
- [x] [Review][Patch] Homogeneous-path integration test does not exercise site-aware `DisksConsistent` behavior [`pkg/controller/drplan/reconciler_test.go:2633`]

## Dev Notes

### Scope & Approach

This is a Go backend story. All changes are in `pkg/controller/drplan/reconciler.go` and `pkg/controller/drplan/reconciler_test.go`. No API type changes, no admission changes, no console-plugin changes.

**Change pattern:** Add condition constant → create pure validation function → wire into reconciler → unit tests → integration tests → lint + test.

**Prerequisites:** Stories 9.1 (DiscoveredDisk type + disk enrichment) and 9.3 (DisksConsistent condition infrastructure + admission gate). This story assumes:
- `DiscoveredVM` has a `Disks []DiscoveredDisk` field with `Name`, `PVCName`, and `StorageClass`
- `DisksConsistent` condition type, `reasonDisksAgreed`, `reasonDiskMismatch`, `reasonWaitingForDiskDiscovery` constants exist
- `evaluateDiskAgreement` method exists and runs before VG formation
- `DRExecutionValidator` (or `SoteriaAdmissionPlugin` from 9.2) already gates on `DisksConsistent=False`

### Critical: No Admission Changes Needed

The admission gate from Story 9.3 checks `DisksConsistent=False` **regardless of reason** (`DiskMismatch`, `WaitingForDiskDiscovery`, or now `StorageClassMixed`). Story 9.5 only needs to set the condition — the existing gate blocks execution automatically.

### Critical: Relationship Between 9.3 and 9.5 Checks

These are sequential, complementary checks with a priority ordering:

1. **Cross-site disk topology agreement (9.3)** — `evaluateDiskAgreement` runs BEFORE VG formation. If disk topology mismatches across sites (`DiskMismatch`), it blocks early and the reconciler never reaches VG formation.
2. **VG storage class homogeneity (9.5)** — runs AFTER VG formation. If cross-site disks agree (or are in `WaitingForDiskDiscovery` state), the reconciler proceeds to VG formation. Then this check validates that each VG's member disks use a single storage class.

The `disksConsistentCond` variable flows through both checks. Story 9.3's `evaluateDiskAgreement` returns the initial value (e.g., `DisksAgreed`). Story 9.5's check may **override** it to `StorageClassMixed` if a VG has heterogeneous SCs. The final condition is what gets persisted to status.

### Critical: Where in Reconciler Flow (Insertion Point)

After `ResolveVolumeGroups` returns `ConsistencyResult`, before the wave conflict check:

```go
// Existing (step 5): VG formation
consistency, err := engine.ResolveVolumeGroups(ctx, vms, nsLookup)
if err != nil { ... }

// NEW (9.5): VG storage class homogeneity check
mixedVGs := validateVGStorageClassHomogeneity(consistency.VolumeGroups, waves)
if len(mixedVGs) > 0 {
    msg := buildMixedSCMessage(mixedVGs)
    scCond := metav1.Condition{
        Type:               conditionTypeDisksConsistent,
        Status:             metav1.ConditionFalse,
        Reason:             reasonStorageClassMixed,
        Message:            msg,
        ObservedGeneration: plan.Generation,
    }
    disksConsistentCond = &scCond

    readyCond := metav1.Condition{
        Type:               conditionTypeReady,
        Status:             metav1.ConditionFalse,
        Reason:             reasonDisksOutOfSync,
        Message:            msg,
        ObservedGeneration: plan.Generation,
    }

    report := r.composePreflightReport(ctx, &plan, discovery, consistency, nil, vms, waves)
    report.DisksConsistent = false
    report.DiskDiscoveryDelta = msg

    var extraConds []metav1.Condition
    if sitesInSyncCond != nil { extraConds = append(extraConds, *sitesInSyncCond) }
    extraConds = append(extraConds, scCond)
    return r.updateStatus(ctx, req, &plan, waves, result.TotalVMs, report, readyCond, nil, extraConds...)
}

// Existing (step 6): wave conflict check
if consistency.Conflicts != nil { ... }
```

The `waves` variable (from `GroupByWave`) and `consistency.VolumeGroups` (from `ResolveVolumeGroups`) are both available at this point. The wave VMs carry `DiscoveredVM.Disks` data from Story 9.1's enrichment.

### Critical: The Pure Function — validateVGStorageClassHomogeneity

```go
type MixedVGResult struct {
    VGName  string
    Classes []string
}

func validateVGStorageClassHomogeneity(
    vgs []soteriav1alpha1.VolumeGroupInfo,
    waves []soteriav1alpha1.WaveInfo,
) []MixedVGResult {
    type vmKey struct{ namespace, name string }
    vmDisks := make(map[vmKey][]soteriav1alpha1.DiscoveredDisk)
    for _, wave := range waves {
        for _, vm := range wave.VMs {
            vmDisks[vmKey{vm.Namespace, vm.Name}] = vm.Disks
        }
    }

    var results []MixedVGResult
    for _, vg := range vgs {
        classes := make(map[string]struct{})
        for _, vmName := range vg.VMNames {
            disks := vmDisks[vmKey{vg.Namespace, vmName}]
            for _, d := range disks {
                if d.StorageClass != "" {
                    classes[d.StorageClass] = struct{}{}
                }
            }
        }
        if len(classes) > 1 {
            sorted := make([]string, 0, len(classes))
            for c := range classes {
                sorted = append(sorted, c)
            }
            sort.Strings(sorted)
            results = append(results, MixedVGResult{VGName: vg.Name, Classes: sorted})
        }
    }
    return results
}
```

### Critical: Message Building — Follow writeCappedList Pattern

Use the `writeCappedList` function (from Story 9.3 / `compareSiteDiscovery`) to cap message length:

```go
func buildMixedSCMessage(mixed []MixedVGResult) string {
    var items []string
    for _, m := range mixed {
        items = append(items, fmt.Sprintf(
            "Volume group %s: mixed storage classes %v — all disks must use the same storage class",
            m.VGName, m.Classes))
    }
    var b strings.Builder
    writeCappedList(&b, items, maxDeltaEntriesPerSide)
    return b.String()
}
```

### Critical: Empty StorageClass Handling

A disk with empty `StorageClass` (from 9.1 self-healing — PVC not yet resolved) should NOT be counted as a distinct class. The function skips empty strings: `if d.StorageClass != ""`. This means:
- A VG where all disks have empty SC (PVCs not resolved) → `classes` map is empty → `len(classes) <= 1` → passes. This is correct because the SC data isn't available yet; the next reconcile cycle will fill it in.
- A VG with one disk `ceph-rbd` and one disk `""` (not yet resolved) → `classes = {"ceph-rbd"}` → passes. On next cycle when PVC resolves, if it's `local-path`, it becomes mixed.

### Critical: Stateless VM Handling

VMs with only non-PVC volumes (containerDisk, cloudInitNoCloud) have empty `Disks` from Story 9.1. They contribute zero entries to the `classes` map. This means:
- VG with 2 VMs: VM-A has `ceph-rbd` disks, VM-B is stateless → `classes = {"ceph-rbd"}` → passes
- VG with all stateless VMs → `classes` is empty → `len(classes) = 0` → passes trivially

### Critical: Defense-in-Depth with Executor

The executor already validates VG storage class homogeneity at execution time via `resolveVGStorageClass` in `pkg/engine/executor.go` (lines 861-927). That function does live PVC reads against the API server. Story 9.5 adds reconcile-time validation using pre-enriched `DiscoveredVM.Disks` data — catching issues early and blocking via the `DisksConsistent` condition before execution is even attempted.

**Do NOT modify `resolveVGStorageClass` or `resolveChunkStorageClass`** — they remain as execution-time defense-in-depth.

### Critical: Waves Are NOT Cleared

Unlike `evaluateDiskAgreement` (9.3) which clears waves on `DiskMismatch`, the storage class homogeneity check does NOT clear waves. Waves are needed for:
1. The preflight report (showing VG composition)
2. Debugging (user needs to see which VGs have mixed SCs)
3. The wave/VG data was already computed and is useful

This follows the same pattern as the wave conflict check, which sets `Ready=False` but keeps waves in status.

### Critical: updateStatus Call — Passing Nil replicationHealth

When returning early from the homogeneity check, `replicationHealth` is nil (health polling hasn't run). Pass `nil` to `updateStatus` — it handles nil health gracefully.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `conditionTypeDisksConsistent` + reasons | `reconciler.go` (from 9.3) | Add `reasonStorageClassMixed` alongside |
| `evaluateSiteAgreement` early-return pattern | `reconciler.go:413-487` | Follow for early-return structure |
| Wave conflict early-return pattern | `reconciler.go:228-239` | Follow for 9.5 early-return (keeps waves, Ready=False) |
| `writeCappedList(b, items, max)` | `reconciler.go:590-599` | Reuse for SC violation message |
| `compareDiskTopology` pure function pattern | `reconciler.go` (from 9.3) | Follow for `validateVGStorageClassHomogeneity` |
| `updateStatus(... extraConditions ...)` | `reconciler.go:853-965` (refactored by 9.3) | Pass StorageClassMixed condition |
| `TestCompareDiskTopology_*` test pattern | `reconciler_test.go` (from 9.3) | Follow for `TestValidateVGStorageClassHomogeneity_*` |
| `TestReconcile_DisksConsistent_*` integration | `reconciler_test.go` (from 9.3) | Follow for `TestReconcile_StorageClassMixed_*` |
| `newReconcilerWithSite(objs, discoverer, site)` | `reconciler_test.go:1601-1621` | Reuse for integration tests |
| VM lookup map `vmKey{namespace, name}` | Story 9.4 preflight enrichment pattern | Same map construction |
| `ResolveVolumeGroups` → `ConsistencyResult.VolumeGroups` | `pkg/engine/consistency.go:100-197` | Source of VG definitions |
| `VolumeGroupInfo.VMNames` | `pkg/apis/soteria.io/v1alpha1/types.go:271-287` | VM membership per VG |
| `DiscoveredVM.Disks` | `types.go:250-256` (after 9.1) | Source of per-VM storage class data |
| Condition construction with `ObservedGeneration` | All condition-setting code in reconciler | Include `plan.Generation` |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/controller/drplan/reconciler.go` | Add `reasonStorageClassMixed` constant, `MixedVGResult` struct, `validateVGStorageClassHomogeneity` pure function, `buildMixedSCMessage` helper, wire into reconcile flow | ~60 lines added, ~10 lines modified |
| `pkg/controller/drplan/reconciler_test.go` | Add `TestValidateVGStorageClassHomogeneity_*` unit tests, `TestReconcile_StorageClassMixed_*` integration tests | ~200 lines |

No API type changes, no admission changes, no console-plugin changes, no generated file changes.

### Execution Order

1. Task 1 (condition constant)
2. Task 2 (pure function + helper)
3. Task 4 (unit tests — validate function before wiring into reconciler)
4. Task 3 (wire into reconciler)
5. Task 5 (integration tests)
6. Task 6 (manifests, lint, full test suite)

### Previous Story Learnings (from 9.1, 9.2, 9.3, 9.4)

- **`DiscoveredVM.Disks` is the single source of truth for disk data** — populated by DiskEnricher in the reconcile loop, available on `WaveInfo.VMs` and `SiteDiscovery.VMs`
- **Self-healing via reconcile loop** — if disk data is missing or storageClass is empty, it fills in on the next cycle (30s); VGs with incomplete data pass trivially (empty SC excluded from count)
- **`evaluateDiskAgreement` (9.3) blocks BEFORE VG formation on `DiskMismatch`** — if cross-site disks disagree, the reconciler never reaches the homogeneity check. 9.5 only runs when cross-site disks agree or are waiting for discovery
- **Admission gate from 9.3 gates on `DisksConsistent=False` regardless of reason** — no admission changes needed in 9.5
- **`compareSiteDiscovery` / `compareDiskTopology` are pure functions** — follow the same pattern for `validateVGStorageClassHomogeneity` (no side effects, easy to test)
- **Wave conflict pattern** — keeps waves in status, sets `Ready=False`, composes preflight with warnings. Follow this for 9.5 (don't clear waves)
- **`writeCappedList` caps delta messages** — reuse for SC violation messages
- **Executor's `resolveVGStorageClass` is defense-in-depth** — do NOT remove or modify it
- **API types in `pkg/apis/soteria.io/v1alpha1/types.go`** — sample-apiserver pattern, not kubebuilder `api/` convention
- **VM lookup map pattern** — `vmKey{namespace, name} → []DiscoveredDisk` used in 9.3 and 9.4; reuse the same pattern

### Project Structure Notes

- API types live in `pkg/apis/soteria.io/v1alpha1/types.go` (sample-apiserver pattern, not kubebuilder `api/` convention)
- Controllers live in `pkg/controller/drplan/` (not `internal/controller/`)
- Generated files: `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml` — never hand-edit
- Run `make manifests generate` after any `types.go` change (not needed in this story unless 9.3 types are incomplete)
- `reasonStorageClassMixed` constant is package-private to `pkg/controller/drplan/`

### References

- [Source: pkg/controller/drplan/reconciler.go#L68-L85] — Condition type/reason constants (add reasonStorageClassMixed here; DisksConsistent constants from 9.3)
- [Source: pkg/controller/drplan/reconciler.go#L151-L162] — Reconcile flow: site agreement + disk agreement checks (9.5 runs after)
- [Source: pkg/controller/drplan/reconciler.go#L213-L239] — ResolveVolumeGroups + wave conflict check (insert 9.5 between these)
- [Source: pkg/controller/drplan/reconciler.go#L296-L319] — Success path: preflight enrichment + updateStatus
- [Source: pkg/controller/drplan/reconciler.go#L413-L487] — evaluateSiteAgreement (pattern reference for early-return)
- [Source: pkg/controller/drplan/reconciler.go#L489-L599] — compareSiteDiscovery + writeCappedList (reuse writeCappedList for message building)
- [Source: pkg/controller/drplan/reconciler.go#L853-L965] — updateStatus (pass StorageClassMixed condition via extraConditions)
- [Source: pkg/engine/executor.go#L861-L927] — resolveVGStorageClass (existing execution-time defense-in-depth; do NOT modify)
- [Source: pkg/engine/consistency.go#L100-L197] — ResolveVolumeGroups (produces VGs that 9.5 validates)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L250-L269] — DiscoveredVM + DiscoveredDisk (after 9.1)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L271-L287] — VolumeGroupInfo (VMNames for membership lookup)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L289-L301] — WaveInfo (VMs with DiscoveredVM including Disks)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L143-L172] — PreflightReport (DisksConsistent + DiskDiscoveryDelta fields from 9.3)
- [Source: pkg/controller/drplan/reconciler_test.go#L1441-L1597] — compareSiteDiscovery/DiskTopology test patterns
- [Source: pkg/controller/drplan/reconciler_test.go#L1599-L1640] — Reconciler agreement integration test patterns
- [Source: _bmad-output/planning-artifacts/epics.md#Story-9.5] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, naming conventions, testing pyramid

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (cursor)

### Debug Log References

None required — clean implementation with no debugging needed.

### Completion Notes List

- Added `reasonStorageClassMixed = "StorageClassMixed"` constant alongside existing DisksConsistent constants
- Created `MixedVGResult` struct and `validateVGStorageClassHomogeneity` pure function that collects distinct storage classes per VG from DiscoveredVM.Disks data, excluding empty StorageClass strings and stateless VMs (nil/empty Disks)
- Created `buildMixedSCMessage` helper using `writeCappedList` for capped message formatting
- Wired validation into reconciler between `ResolveVolumeGroups` and wave conflict check — overrides `disksConsistentCond` from 9.3 when VG SCs are mixed, sets `Ready=False/DisksOutOfSync`, composes preflight with `DisksConsistent=false` and violation message, returns early with waves preserved (follows wave conflict pattern, not disk mismatch pattern)
- 9 unit tests covering: single VM single SC (passes), single VM two SCs (fails), namespace-level all same SC (passes), namespace-level mixed SC (fails), mixed+stateless VM (passes), all stateless VMs (passes), empty StorageClass excluded (passes), multiple VGs one heterogeneous (only heterogeneous in results), single VM single disk single SC (passes)
- 2 message builder tests covering single and multiple VG messages
- 3 reconciler integration tests: StorageClassMixed blocks Ready, homogeneous passes, mixed overrides DisksAgreed from 9.3
- Coverage increased from 87.8% to 88.6% for pkg/controller/drplan
- Zero new lint issues, zero regressions, all tests pass

### File List

- `pkg/controller/drplan/reconciler.go` — Added `reasonStorageClassMixed` constant, `MixedVGResult` struct, `validateVGStorageClassHomogeneity` pure function, `buildMixedSCMessage` helper, reconciler wiring between VG resolution and wave conflict check
- `pkg/controller/drplan/reconciler_test.go` — Added `newReconcilerWithSiteEnricherAndNSLookup` helper, 9 `TestValidateVGStorageClassHomogeneity_*` unit tests, 2 `TestBuildMixedSCMessage_*` tests, 3 `TestReconcile_StorageClass*` integration tests
