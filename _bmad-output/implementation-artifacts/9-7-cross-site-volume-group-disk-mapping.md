# Story 9.7: Cross-Site Volume Group Disk Mapping

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want each disk in a PreflightVolumeGroup to show its PVC mapping on both sites,
So that I can verify cross-site disk correspondence before triggering a DR operation.

## Background

Story 9.4 introduced per-disk PVC enrichment in the preflight report, but only from the active site's perspective (using `CompositionInput.LocalSite` and wave VM data). UAT-9.001 identified that users need to see which PVC on the partner site corresponds to each local disk — the current single-site view leaves a visibility gap.

## Acceptance Criteria

1. **AC1 — DiskSiteMapping API type:** A new `DiskSiteMapping` type is added to `pkg/apis/soteria.io/v1alpha1/types.go` with fields: `site` (string), `pvcName` (string, omitempty), `pvcNamespace` (string, omitempty). Each `VolumeGroupDisk` gains a `sites []DiskSiteMapping` field (+listType=atomic). The top-level `Site` field is removed from `PreflightVolumeGroup`. The `PVCName` and `PVCNamespace` fields on `VolumeGroupDisk` are removed (replaced by per-site data in `Sites`). `make manifests generate` succeeds.

2. **AC2 — Cross-site enrichment in preflight composition:** When both `primarySiteDiscovery` and `secondarySiteDiscovery` are populated on the DRPlan, `enrichVolumeGroup` produces disk entries with entries in `Sites` for both sites (matched by disk name). A site entry is omitted only when that site's SiteDiscovery is nil (first reconcile). The `CompositionInput` gains access to both SiteDiscovery objects.

3. **AC3 — Console WaveCompositionTree cross-site rendering:** `buildVGDiskNodes` renders per-site PVC mapping for each disk — each disk shows site labels with PVC name + namespace instead of the current flat `disk.pvcName (disk.pvcNamespace)` display. The top-level VG site label is removed.

4. **AC4 — Backward compatibility:** Plans created before this change (single-site `Site` + flat `PVCName`/`PVCNamespace`) are rendered gracefully in the Console (fallback to single-site view). The `typeof` guard for `string[]` backward compat remains.

5. **AC5 — Tests:** `enrichVolumeGroup` tests validate cross-site output (both sites populated, single site only, nil SiteDiscovery). WaveCompositionTree tests validate per-site disk rendering and backward compat for old format. All existing tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: API type changes (AC: #1)
  - [ ] 1.1 Add `DiskSiteMapping` type to `pkg/apis/soteria.io/v1alpha1/types.go`: `Site string`, `PVCName string` (omitempty), `PVCNamespace string` (omitempty)
  - [ ] 1.2 Add `Sites []DiskSiteMapping` field to `VolumeGroupDisk` (+listType=atomic, omitempty)
  - [ ] 1.3 Remove `PVCName` and `PVCNamespace` fields from `VolumeGroupDisk`
  - [ ] 1.4 Remove `Site` field from `PreflightVolumeGroup`
  - [ ] 1.5 Run `make manifests generate` to regenerate deepcopy, OpenAPI, CRDs

- [ ] Task 2: Extend `CompositionInput` and `enrichVolumeGroup` (AC: #2)
  - [ ] 2.1 Add `PrimarySiteDiscovery *soteriav1alpha1.SiteDiscovery` and `SecondarySiteDiscovery *soteriav1alpha1.SiteDiscovery` fields to `CompositionInput`
  - [ ] 2.2 In `ComposeReport`, build two VM disk indexes — one from waves (local site) and one from the partner site's SiteDiscovery — using a new `buildSiteDiscoveryDiskIndex` helper
  - [ ] 2.3 Update `enrichVolumeGroup` signature: replace `localSite string` and single `vmDiskIndex` with the plan's `primarySite`/`secondarySite` strings and both disk indexes
  - [ ] 2.4 Inside `enrichVolumeGroup`, for each disk, populate `Sites` by matching disk name against both indexes. Use the plan's `spec.primarySite`/`spec.secondarySite` as the site label for each entry
  - [ ] 2.5 Handle nil SiteDiscovery: omit the site entry when a site's discovery data is nil (first reconcile scenario)

- [ ] Task 3: Update reconciler to pass SiteDiscovery to CompositionInput (AC: #2)
  - [ ] 3.1 In `composePreflightReport` in `pkg/controller/drplan/reconciler.go`, populate `PrimarySiteDiscovery` and `SecondarySiteDiscovery` from `plan.Status.PrimarySiteDiscovery` and `plan.Status.SecondarySiteDiscovery`

- [ ] Task 4: Update Console TypeScript types (AC: #3, #4)
  - [ ] 4.1 Add `DiskSiteMapping` interface to `console-plugin/src/models/types.ts`: `{ site: string; pvcName?: string; pvcNamespace?: string }`
  - [ ] 4.2 Add `sites?: DiskSiteMapping[]` to `VolumeGroupDisk` interface
  - [ ] 4.3 Remove `pvcName` and `pvcNamespace` from `VolumeGroupDisk` — BUT keep them as optional for backward compat with old plans
  - [ ] 4.4 Remove `site` from `PreflightVolumeGroup` — BUT keep it as optional for backward compat with old plans

- [ ] Task 5: Update `WaveCompositionTree` rendering (AC: #3, #4)
  - [ ] 5.1 Update `buildVGDiskNodes` to render per-site PVC mapping: each disk shows its site entries with site label + PVC name + namespace
  - [ ] 5.2 Update `buildVGNodes` to remove the VG-level site `Label` (site info is now per-disk)
  - [ ] 5.3 Add backward compat: if a disk has `pvcName`/`pvcNamespace` but no `sites`, render the old flat format. If the VG still has `site`, show it at VG level as fallback

- [ ] Task 6: Go unit tests — cross-site enrichment (AC: #5)
  - [ ] 6.1 Add `TestComposeReport_VGDiskEnrichment_CrossSite` — both sites populated, verify each disk has two `Sites` entries
  - [ ] 6.2 Add `TestComposeReport_VGDiskEnrichment_SingleSiteOnly` — only primary SiteDiscovery, verify each disk has one `Sites` entry
  - [ ] 6.3 Add `TestComposeReport_VGDiskEnrichment_NilSiteDiscovery` — both nil, verify empty sites (same as current no-waves behavior)
  - [ ] 6.4 Add `TestComposeReport_VGDiskEnrichment_DiskOnOneSiteOnly` — a disk name that exists on primary but not secondary, verify only one site entry
  - [ ] 6.5 Update existing VG enrichment tests (`TestComposeReport_VGDiskEnrichment_VMLevel`, etc.) for the new `Sites` structure
  - [ ] 6.6 Update `assertVG` helper — remove `wantSite` param. Update `assertDisk` — assert `Sites` entries instead of flat PVCName/PVCNamespace

- [ ] Task 7: TypeScript tests — WaveCompositionTree cross-site (AC: #5)
  - [ ] 7.1 Update existing VG disk test fixture in `WaveCompositionTree.test.tsx` to use `sites` instead of flat `pvcName/pvcNamespace`
  - [ ] 7.2 Add test: disk node shows per-site PVC mapping with site labels
  - [ ] 7.3 Add test: backward compat — old format with flat `pvcName`/`pvcNamespace` (no `sites`) renders correctly
  - [ ] 7.4 Add test: backward compat — VG with `site` field (no per-disk `sites`) renders VG-level site label
  - [ ] 7.5 jest-axe passes on all new states

## Dev Notes

### Scope & Approach

This is a cross-cutting story touching Go API types, preflight composition, the reconciler, and the Console plugin. The core change is restructuring from single-site VG/disk data to per-disk cross-site mapping.

**Change pattern:** API types → preflight composition → reconciler wiring → TS types → UI rendering → Go tests → TS tests.

### Critical: API Type Restructuring

**Current state** (from Story 9.4):

```go
type VolumeGroupDisk struct {
    Name         string `json:"name"`
    PVCName      string `json:"pvcName,omitempty"`
    PVCNamespace string `json:"pvcNamespace,omitempty"`
}

type PreflightVolumeGroup struct {
    Name  string            `json:"name"`
    Site  string            `json:"site,omitempty"`
    Disks []VolumeGroupDisk `json:"disks,omitempty"`
}
```

**Target state** (this story):

```go
type DiskSiteMapping struct {
    Site         string `json:"site"`
    PVCName      string `json:"pvcName,omitempty"`
    PVCNamespace string `json:"pvcNamespace,omitempty"`
}

type VolumeGroupDisk struct {
    Name  string            `json:"name"`
    Sites []DiskSiteMapping `json:"sites,omitempty"`
}

type PreflightVolumeGroup struct {
    Name  string            `json:"name"`
    Disks []VolumeGroupDisk `json:"disks,omitempty"`
}
```

Key changes:
- `PVCName`/`PVCNamespace` move from `VolumeGroupDisk` into `DiskSiteMapping` entries
- `Site` moves from `PreflightVolumeGroup` into per-disk `DiskSiteMapping` entries
- Each disk now has a `Sites` slice — typically 2 entries (one per site)

### Critical: enrichVolumeGroup Restructuring

**Current** `enrichVolumeGroup` builds a single-site view from wave VMs:

```go
func enrichVolumeGroup(
    vg soteriav1alpha1.VolumeGroupInfo,
    localSite string,
    vmDiskIndex map[vmKey][]soteriav1alpha1.DiscoveredDisk,
) soteriav1alpha1.PreflightVolumeGroup
```

**New** `enrichVolumeGroup` must accept two disk indexes and two site names:

```go
func enrichVolumeGroup(
    vg soteriav1alpha1.VolumeGroupInfo,
    primarySite string,
    secondarySite string,
    primaryDiskIndex map[vmKey][]soteriav1alpha1.DiscoveredDisk,
    secondaryDiskIndex map[vmKey][]soteriav1alpha1.DiscoveredDisk,
) soteriav1alpha1.PreflightVolumeGroup
```

The function builds a merged disk view:
1. Collect all unique disk names from both indexes for the VG's member VMs
2. For each disk name, check both indexes — add a `DiskSiteMapping` entry for each site that has the disk
3. Sort disks by name (deterministic output)
4. Skip site entry if the site index is nil (nil SiteDiscovery = nil index)

### Critical: Building Partner Site Disk Index

A new helper `buildSiteDiscoveryDiskIndex` builds a vmKey→disks map from a `SiteDiscovery` object (unlike `buildVMDiskIndex` which reads from `WaveInfo`):

```go
func buildSiteDiscoveryDiskIndex(sd *soteriav1alpha1.SiteDiscovery) map[vmKey][]soteriav1alpha1.DiscoveredDisk {
    if sd == nil {
        return nil
    }
    index := make(map[vmKey][]soteriav1alpha1.DiscoveredDisk, len(sd.VMs))
    for _, vm := range sd.VMs {
        index[vmKey{vm.Namespace, vm.Name}] = vm.Disks
    }
    return index
}
```

### Critical: Determining Which SiteDiscovery Is Local vs Partner

The reconciler has `r.LocalSite` (e.g., "dc1"). The plan has `spec.primarySite` and `spec.secondarySite`. The preflight composition needs to map local site → primary or secondary SiteDiscovery. But `enrichVolumeGroup` doesn't need to know which is "local" — it just needs both sites' disk data and site names. Pass `plan.Spec.PrimarySite`/`plan.Spec.SecondarySite` as site labels and `plan.Status.PrimarySiteDiscovery`/`plan.Status.SecondarySiteDiscovery` as the data sources.

### Critical: CompositionInput Changes

Add two fields:

```go
type CompositionInput struct {
    Plan                   *soteriav1alpha1.DRPlan
    DiscoveryResult        *engine.DiscoveryResult
    ConsistencyResult      *engine.ConsistencyResult
    ChunkResult            *engine.ChunkResult
    StorageBackends        map[string]string
    Waves                  []soteriav1alpha1.WaveInfo
    LocalSite              string
    PrimarySiteDiscovery   *soteriav1alpha1.SiteDiscovery   // NEW
    SecondarySiteDiscovery *soteriav1alpha1.SiteDiscovery   // NEW
}
```

In `ComposeReport`:
- Build primary disk index from `input.PrimarySiteDiscovery`
- Build secondary disk index from `input.SecondarySiteDiscovery`
- Pass both indexes + site names (`input.Plan.Spec.PrimarySite`, `input.Plan.Spec.SecondarySite`) to `enrichVolumeGroup`
- The existing `buildVMDiskIndex(input.Waves)` can be removed if waves always match one of the site discoveries. However, waves carry the local site's data — use it AS the local site index for determinism. Actually, waves are built from the active site's discovery, so they should match the active site's SiteDiscovery. Use the SiteDiscovery objects directly instead to avoid ambiguity.

**Design decision:** Use SiteDiscovery objects as the canonical source for both indexes. The wave-based `buildVMDiskIndex` is no longer needed for VG enrichment — it was a workaround before we had both SiteDiscovery objects available. Keep `buildVMDiskIndex` if it's used elsewhere in `ComposeReport` (check for other callers), otherwise remove it.

### Critical: Reconciler Wiring

In `composePreflightReport` (`pkg/controller/drplan/reconciler.go`), add:

```go
input := preflight.CompositionInput{
    Plan:                   plan,
    DiscoveryResult:        discovery,
    ConsistencyResult:      consistency,
    ChunkResult:            chunks,
    StorageBackends:        storageBackends,
    Waves:                  waves,
    LocalSite:              r.LocalSite,
    PrimarySiteDiscovery:   plan.Status.PrimarySiteDiscovery,   // NEW
    SecondarySiteDiscovery: plan.Status.SecondarySiteDiscovery, // NEW
}
```

### Critical: Console TypeScript Types

**New interface:**

```typescript
export interface DiskSiteMapping {
  site: string;
  pvcName?: string;
  pvcNamespace?: string;
}
```

**Updated `VolumeGroupDisk`:**

```typescript
export interface VolumeGroupDisk {
  name: string;
  // New: per-site PVC mapping
  sites?: DiskSiteMapping[];
  // Deprecated: flat PVC fields (backward compat with pre-9.7 plans)
  pvcName?: string;
  pvcNamespace?: string;
}
```

**Updated `PreflightVolumeGroup`:**

```typescript
export interface PreflightVolumeGroup {
  name: string;
  // Deprecated: VG-level site (backward compat with pre-9.7 plans)
  site?: string;
  disks?: VolumeGroupDisk[];
}
```

Keep `pvcName`, `pvcNamespace`, and `site` as optional fields for backward compat — they'll be populated on plans reconciled before 9.7. The rendering logic checks `disk.sites` first, falls back to flat fields.

### Critical: WaveCompositionTree Rendering Changes

**`buildVGDiskNodes` update:**

Current rendering: `disk.name → disk.pvcName (disk.pvcNamespace)`

New rendering for disks with `sites`:
```
disk.name
  ├─ dc1: pvc-root (erp-db)
  └─ dc2: pvc-root-dr (erp-db)
```

Each site mapping is a child node under the disk node. If `disk.sites` is populated, render per-site children. If not (backward compat), render the flat `pvcName (pvcNamespace)` as before.

**`buildVGNodes` update:**

Remove the VG-level site `Label` — site info is now per-disk. If the VG still has `site` (backward compat), show it as fallback.

### Critical: Backward Compatibility Strategy

Three backward compat scenarios in the Console:

1. **`string[]` volumeGroups** (pre-9.4 plans): Handled by existing `typeof` guard in `getChunkVolumeGroups` → `buildLegacyVGNodes`. No change needed.

2. **`PreflightVolumeGroup` with `site` + flat `pvcName`/`pvcNamespace`** (9.4–9.6 plans): Disks have flat fields, no `sites`. Render `disk.name → pvcName (pvcNamespace)` as before. VG has `site` → show VG-level label.

3. **`PreflightVolumeGroup` with per-disk `sites`** (9.7+ plans): Disks have `sites` array. Render per-site children. VG has no `site`.

Detection logic in `buildVGDiskNodes`:
```typescript
if (disk.sites?.length) {
  // New format: render per-site rows
} else if (disk.pvcName) {
  // Old format: render flat pvcName (pvcNamespace)
}
```

### Critical: CSS/Styling Rules

- **No hardcoded colors** — use PF6 tokens: `var(--pf-t--global--*, var(--pf-v5-global--*))`
- Site labels in disk children: use PatternFly `Label` isCompact with distinct colors per site (e.g., blue/cyan) — or plain text with bold site name
- Text sizes: minimum 14px for status text (bridge-call readability)

### Critical: v1alpha1 Breaking Change

This is a v1alpha1 API, so breaking changes are acceptable. The Go type change (removing `PVCName`/`PVCNamespace` from `VolumeGroupDisk` and `Site` from `PreflightVolumeGroup`) is clean. All callers are internal. The only external consumer is the Console plugin, which handles backward compat via runtime type detection.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `buildVMDiskIndex` helper | `internal/preflight/checks.go:147-161` | Follow for `buildSiteDiscoveryDiskIndex` |
| `enrichVolumeGroup` | `internal/preflight/checks.go:165-197` | Restructure for cross-site |
| VG enrichment tests | `internal/preflight/checks_test.go:445-771` | Follow pattern for cross-site tests |
| `assertVG` / `assertDisk` helpers | `internal/preflight/checks_test.go:749-771` | Update for new `Sites` structure |
| `getChunkVolumeGroups` backward compat | `WaveCompositionTree.tsx:192-199` | Extend with `sites` detection |
| `buildVGDiskNodes` | `WaveCompositionTree.tsx:200-212` | Restructure for per-site rendering |
| `buildVGNodes` site label | `WaveCompositionTree.tsx:222-242` | Remove VG-level site label |
| PF6 token dual-fallback | All console components | `var(--pf-t--global--*, var(--pf-v5-global--*))` |
| jest-axe in tests | All `*.test.tsx` | `const { container } = render(...); expect(await axe(container)).toHaveNoViolations()` |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `DiskSiteMapping`, restructure `VolumeGroupDisk`, remove `Site` from `PreflightVolumeGroup` | ~15 lines changed |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-generated | `make generate` |
| `internal/preflight/checks.go` | Add `buildSiteDiscoveryDiskIndex`, restructure `enrichVolumeGroup`, extend `CompositionInput` | ~60 lines changed |
| `pkg/controller/drplan/reconciler.go` | Pass SiteDiscovery to CompositionInput | ~2 lines added |
| `console-plugin/src/models/types.ts` | Add `DiskSiteMapping`, extend `VolumeGroupDisk`, update `PreflightVolumeGroup` | ~10 lines |
| `console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx` | Update `buildVGDiskNodes` for per-site rendering, update `buildVGNodes` to remove VG-level site | ~30 lines changed |
| `internal/preflight/checks_test.go` | Add cross-site tests, update existing VG tests for `Sites` structure | ~100 lines |
| `console-plugin/tests/components/WaveCompositionTree.test.tsx` | Update VG fixtures for `sites`, add cross-site rendering tests | ~50 lines |

### Execution Order

1. Task 1 (API types) — foundation; run `make manifests generate` immediately after
2. Task 2 (preflight enrichment) — restructure `enrichVolumeGroup` for cross-site
3. Task 3 (reconciler wiring) — pass SiteDiscovery into CompositionInput
4. Task 6 (Go tests) — validate Go changes compile and pass before moving to TS
5. Task 4 (TS types) — add `DiskSiteMapping` interface
6. Task 5 (WaveCompositionTree rendering) — update UI
7. Task 7 (TS tests) — validate Console changes

### Previous Story Learnings (from 9.4 and 9.6)

- **Story 9.4 established the VG enrichment pattern** — `enrichVolumeGroup` with `vmDiskIndex` built from waves. Story 9.7 extends this to use both sites' SiteDiscovery objects instead of just local wave data.
- **Story 9.6 established the backward compat pattern** — `typeof` guard for `string[]` in `getChunkVolumeGroups` and `buildLegacyVGNodes`. Story 9.7 adds a second backward compat layer for the flat `pvcName`/`pvcNamespace` fields.
- **`WaveCompositionTree` was recently restructured** (UAT-9.002 commit `89e0701`) to group VMs and VGs under distinct sub-nodes with icons. The VG rendering is in `buildVGNodes`/`buildVGDiskNodes` — work within this structure.
- **VG enrichment tests use `assertVG`/`assertDisk` helpers** — these must be updated for the new `Sites` structure. Consider adding an `assertSiteMapping` helper.
- **`make manifests generate` is mandatory** after any types.go change — the AGENTS.md rules explicitly require this.
- **PF6 token dual-fallback is mandatory** — every color/spacing token must have `var(--pf-t--*, var(--pf-v5-*))` pattern.
- **jest-axe on every component state** — no PR merges without accessibility audit passing.

### Project Structure Notes

- Go API types: `pkg/apis/soteria.io/v1alpha1/types.go` (not under `api/`)
- Preflight composition: `internal/preflight/checks.go` + `checks_test.go`
- DRPlan reconciler: `pkg/controller/drplan/reconciler.go`
- Console plugin types: `console-plugin/src/models/types.ts`
- WaveCompositionTree: `console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx`
- WaveCompositionTree tests: `console-plugin/tests/components/WaveCompositionTree.test.tsx`
- Auto-generated files (DO NOT EDIT): `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L209-L246] — VolumeGroupDisk, PreflightVolumeGroup, PreflightChunk (restructure these)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L283-L307] — DiscoveredDisk, DiscoveredVM (input for disk indexes)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L133-L140] — PrimarySiteDiscovery, SecondarySiteDiscovery on DRPlanStatus
- [Source: internal/preflight/checks.go#L42-L52] — CompositionInput (extend with SiteDiscovery)
- [Source: internal/preflight/checks.go#L147-L197] — buildVMDiskIndex + enrichVolumeGroup (restructure)
- [Source: internal/preflight/checks.go#L99-L113] — enrichVolumeGroup call site in ComposeReport
- [Source: pkg/controller/drplan/reconciler.go#L1236-L1248] — CompositionInput construction in composePreflightReport
- [Source: internal/preflight/checks_test.go#L445-L771] — VG enrichment tests (update + add cross-site)
- [Source: console-plugin/src/models/types.ts#L166-L183] — VolumeGroupDisk, PreflightVolumeGroup TS interfaces
- [Source: console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx#L186-L242] — buildVGDiskNodes, buildVGNodes
- [Source: console-plugin/tests/components/WaveCompositionTree.test.tsx#L250-L342] — VG disk composition tests
- [Source: _bmad-output/planning-artifacts/epics.md#Story-9.7] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, PF6 tokens, SDK constraints

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
