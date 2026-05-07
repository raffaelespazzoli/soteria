# Story 9.4: Volume Group Disk Enrichment in Preflight

Status: done

## Story

As a platform engineer,
I want the preflight report's volume group entries to show which disks and PVCs belong to each group,
So that I can verify the storage composition of each volume group before execution.

## Acceptance Criteria

1. **AC1 — New API types for enriched VG entries:** `PreflightChunk.VolumeGroups` changes from `[]string` to `[]PreflightVolumeGroup` (v1alpha1 breaking change). `PreflightVolumeGroup` has fields: `name` (string), `site` (string — which site this view is from), `disks []VolumeGroupDisk`. `VolumeGroupDisk` has fields: `name` (disk name), `pvcName` (PVC name), `pvcNamespace` (PVC namespace). `make manifests generate` regenerates deepcopy and OpenAPI without errors.

2. **AC2 — VM-level VG disk enrichment:** When a volume group has VM-level consistency (single VM), `PreflightVolumeGroup.disks` contains all disks from that VM (sourced from `DiscoveredVM.Disks`), and `site` is populated from `r.LocalSite`.

3. **AC3 — Namespace-level VG disk enrichment:** When a volume group has namespace-level consistency (multiple VMs), `PreflightVolumeGroup.disks` contains all disks from all member VMs in that namespace, sorted by VM name then disk name for deterministic output.

4. **AC4 — No additional PVC GETs:** The VG-to-disk mapping is derived entirely from `DiscoveredVM.Disks` (populated by Story 9.1) and VG membership (`VMNames` on `VolumeGroupInfo`). No additional PVC resolution calls are made during preflight composition.

5. **AC5 — Console plugin TypeScript types updated:** The Console plugin TypeScript interfaces in `console-plugin/src/models/types.ts` are updated to reflect the new `PreflightVolumeGroup` shape. `PreflightChunk.volumeGroups` changes from `string[]` to `PreflightVolumeGroup[]`.

6. **AC6 — Tests:** Tests verify VG disk enrichment for VM-level and namespace-level groups, VMs with no disks (stateless), mixed VGs in a single chunk, and that existing preflight tests are updated for the new type. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [x] Task 1: Add VolumeGroupDisk and PreflightVolumeGroup API types (AC: #1)
  - [x] 1.1 Add `VolumeGroupDisk` struct to `pkg/apis/soteria.io/v1alpha1/types.go` with fields: `Name string`, `PVCName string` (omitempty), `PVCNamespace string` (omitempty)
  - [x] 1.2 Add `PreflightVolumeGroup` struct with fields: `Name string`, `Site string` (omitempty), `Disks []VolumeGroupDisk` (omitempty, +listType=atomic)
  - [x] 1.3 Change `PreflightChunk.VolumeGroups` from `[]string` to `[]PreflightVolumeGroup`
  - [x] 1.4 Run `make manifests generate` to regenerate deepcopy and OpenAPI

- [x] Task 2: Extend CompositionInput with wave/site data (AC: #4)
  - [x] 2.1 Add `Waves []soteriav1alpha1.WaveInfo` field to `CompositionInput` in `internal/preflight/checks.go`
  - [x] 2.2 Add `LocalSite string` field to `CompositionInput`
  - [x] 2.3 In `composePreflightReport` (reconciler.go), populate `input.Waves` with the freshly-built waves and `input.LocalSite` with `r.LocalSite`

- [x] Task 3: Enrich PreflightVolumeGroup entries in ComposeReport (AC: #2, #3, #4)
  - [x] 3.1 In `ComposeReport` (`internal/preflight/checks.go`), build a VM lookup map from `input.Waves`: key = `namespace/name` → `DiscoveredVM`
  - [x] 3.2 Replace the current `pc.VolumeGroups = append(pc.VolumeGroups, vg.Name)` loop with enriched PreflightVolumeGroup construction
  - [x] 3.3 For each VG in a chunk, iterate VMNames sorted alphabetically, look up each VM's DiscoveredDisk data, build VolumeGroupDisk entries (name from DiscoveredDisk.Name, pvcName from DiscoveredDisk.PVCName, pvcNamespace from VG.Namespace)
  - [x] 3.4 Sort disks within each PreflightVolumeGroup by VM name then disk name for deterministic output
  - [x] 3.5 Set `PreflightVolumeGroup.Site` from `input.LocalSite`

- [x] Task 4: Update Console plugin TypeScript types (AC: #5)
  - [x] 4.1 Add `VolumeGroupDisk` interface to `console-plugin/src/models/types.ts` with fields: `name`, `pvcName?`, `pvcNamespace?`
  - [x] 4.2 Add `PreflightVolumeGroup` interface with fields: `name`, `site?`, `disks?`
  - [x] 4.3 Change `PreflightChunk.volumeGroups` from `string[]` to `PreflightVolumeGroup[]`
  - [x] 4.4 Update any Console plugin test fixtures that reference `volumeGroups` as string arrays

- [x] Task 5: Unit tests for VG disk enrichment (AC: #6)
  - [x] 5.1 Update existing preflight tests in `internal/preflight/checks_test.go` to use `[]PreflightVolumeGroup` instead of `[]string`
  - [x] 5.2 Add test: VM-level VG (single VM with 2 PVC disks) → PreflightVolumeGroup with 2 VolumeGroupDisk entries
  - [x] 5.3 Add test: namespace-level VG (3 VMs, each with disks) → PreflightVolumeGroup with all disks sorted by VM name then disk name
  - [x] 5.4 Add test: VG with stateless VM (no disks) → PreflightVolumeGroup with empty disks
  - [x] 5.5 Add test: chunk with multiple VGs → each VG has independent PreflightVolumeGroup with correct disk mapping
  - [x] 5.6 Add test: VM with missing PVC (empty pvcName from 9.1 self-healing) → VolumeGroupDisk with name set but pvcName empty

- [x] Task 6: Run make manifests generate, lint, test (AC: #6)
  - [x] 6.1 `make manifests generate` — zero errors
  - [x] 6.2 `make lint-fix` — zero new lint errors
  - [x] 6.3 `make test` — all tests pass, zero regressions

## Dev Notes

### Scope & Approach

This story spans Go backend and Console plugin TypeScript types. Changes are in `pkg/apis/soteria.io/v1alpha1/`, `internal/preflight/`, `pkg/controller/drplan/`, and `console-plugin/src/models/`. No new controllers or webhooks. The story enriches the existing preflight composition pipeline with per-VG disk data by leveraging the `DiscoveredVM.Disks` data populated by Story 9.1.

**Change pattern:** Add API types → extend CompositionInput → enrich ComposeReport → update TS types → update tests → regenerate manifests.

**Prerequisite:** Story 9.1 must be implemented first. This story assumes `DiscoveredVM` has a `Disks []DiscoveredDisk` field with `Name`, `PVCName`, and `StorageClass` fields.

### Critical: v1alpha1 Breaking Change on PreflightChunk.VolumeGroups

`PreflightChunk.VolumeGroups` changes from `[]string` to `[]PreflightVolumeGroup`. This is a v1alpha1 API-level breaking change — acceptable at the alpha stage but must be called out. Clients consuming the preflight report will get richer data but the JSON shape changes from:

```json
{"volumeGroups": ["vm-default-web01", "vm-default-web02"]}
```

to:

```json
{"volumeGroups": [
  {"name": "vm-default-web01", "site": "dc1", "disks": [
    {"name": "rootdisk", "pvcName": "web01-root", "pvcNamespace": "default"},
    {"name": "datadisk", "pvcName": "web01-data", "pvcNamespace": "default"}
  ]}
]}
```

### Critical: Disk Data Source — No New PVC GETs

All disk data comes from `DiscoveredVM.Disks` which is populated during VM discovery by Story 9.1's `DiskEnricher`. The preflight composer reads this data from the freshly-built `WaveInfo.VMs[]` — no additional PVC resolution is needed. This is a pure data reshaping operation.

### Critical: CompositionInput Extension

The current `CompositionInput` struct in `internal/preflight/checks.go` does not include wave data or site information. To access `DiscoveredVM.Disks`, extend it:

```go
type CompositionInput struct {
    Plan              *soteriav1alpha1.DRPlan
    DiscoveryResult   *engine.DiscoveryResult
    ConsistencyResult *engine.ConsistencyResult
    ChunkResult       *engine.ChunkResult
    StorageBackends   map[string]string
    Waves             []soteriav1alpha1.WaveInfo  // NEW — for disk data access
    LocalSite         string                      // NEW — for site field
}
```

In the reconciler's `composePreflightReport`, the fresh `waves []soteriav1alpha1.WaveInfo` (built earlier in the reconcile cycle with disk enrichment from 9.1) must be passed into `CompositionInput.Waves`.

### Critical: VM Lookup Map for Disk Resolution

Build a lookup from the wave data to efficiently find VM disks:

```go
type vmKey struct{ namespace, name string }
vmDisks := make(map[vmKey][]soteriav1alpha1.DiscoveredDisk)
for _, wave := range input.Waves {
    for _, vm := range wave.VMs {
        vmDisks[vmKey{vm.Namespace, vm.Name}] = vm.Disks
    }
}
```

Then for each VG in a chunk:

```go
pvg := soteriav1alpha1.PreflightVolumeGroup{
    Name: vg.Name,
    Site: input.LocalSite,
}
sortedVMNames := make([]string, len(vg.VMNames))
copy(sortedVMNames, vg.VMNames)
sort.Strings(sortedVMNames)

for _, vmName := range sortedVMNames {
    disks := vmDisks[vmKey{vg.Namespace, vmName}]
    for _, d := range disks {
        pvg.Disks = append(pvg.Disks, soteriav1alpha1.VolumeGroupDisk{
            Name:         d.Name,
            PVCName:      d.PVCName,
            PVCNamespace: vg.Namespace,
        })
    }
}
pc.VolumeGroups = append(pc.VolumeGroups, pvg)
```

Disks within each VM are already sorted by name (from `DiskEnricher` in 9.1). Iterating VMs in sorted order ensures the overall sort is "VM name then disk name" as required by AC3.

### Critical: VMs with No Disks (Stateless VMs)

A VM with only non-PVC volumes (containerDisk, cloudInitNoCloud) has empty/nil `Disks` from Story 9.1. For such VMs, the `PreflightVolumeGroup.Disks` will be empty — this is valid. The VG still appears in the preflight report with its name and site, just with no disk entries.

### Critical: Where to Place New API Types in types.go

Place `VolumeGroupDisk` and `PreflightVolumeGroup` immediately before the existing `PreflightChunk` struct (around line 202 in types.go). This groups all preflight-related types together.

```go
// VolumeGroupDisk describes a single disk's PVC backing within a volume group.
type VolumeGroupDisk struct {
    // Name is the disk name from the VM's domain.devices.disks spec.
    Name string `json:"name"`
    // PVCName is the PersistentVolumeClaim backing this disk.
    // Empty when the PVC has not yet been created (DataVolume provisioning).
    PVCName string `json:"pvcName,omitempty"`
    // PVCNamespace is the namespace of the backing PVC.
    PVCNamespace string `json:"pvcNamespace,omitempty"`
}

// PreflightVolumeGroup describes a volume group enriched with per-disk PVC
// topology in the pre-flight report.
type PreflightVolumeGroup struct {
    // Name is the volume group identifier (e.g. "ns-erp-database" or "vm-default-web01").
    Name string `json:"name"`
    // Site is the cluster site this volume group view is from (e.g. "dc1").
    Site string `json:"site,omitempty"`
    // Disks lists the disks and their PVC backing within this volume group.
    // Sorted by VM name then disk name for deterministic output.
    // +listType=atomic
    Disks []VolumeGroupDisk `json:"disks,omitempty"`
}
```

### Critical: PreflightChunk.VolumeGroups Field Change

Change from:
```go
// VolumeGroups lists the volume group names in this chunk.
// +listType=atomic
VolumeGroups []string `json:"volumeGroups,omitempty"`
```

To:
```go
// VolumeGroups contains the volume groups in this chunk, enriched with
// per-disk PVC topology.
// +listType=atomic
VolumeGroups []PreflightVolumeGroup `json:"volumeGroups,omitempty"`
```

### Critical: Current Code That Builds VolumeGroups — Must Replace

In `internal/preflight/checks.go` (lines 104-106), the current code appends VG names as strings:

```go
for _, vg := range chunk.VolumeGroups {
    pc.VolumeGroups = append(pc.VolumeGroups, vg.Name)
}
```

This must be replaced with the enriched `PreflightVolumeGroup` construction described above.

### Critical: Console Plugin Type Updates

In `console-plugin/src/models/types.ts`, add new interfaces and update the PreflightChunk interface:

```typescript
export interface VolumeGroupDisk {
  name: string;
  pvcName?: string;
  pvcNamespace?: string;
}

export interface PreflightVolumeGroup {
  name: string;
  site?: string;
  disks?: VolumeGroupDisk[];
}

export interface PreflightChunk {
  name: string;
  vmCount: number;
  vmNames?: string[];
  volumeGroups?: PreflightVolumeGroup[];  // was: string[]
}
```

**Console plugin test impact:** Search for any test fixtures in `console-plugin/tests/` that create `PreflightChunk` objects with `volumeGroups: ['name1', 'name2']` — these must be updated to use `PreflightVolumeGroup` objects: `volumeGroups: [{ name: 'name1' }, { name: 'name2' }]`. The WaveCompositionTree and PlanConfiguration components may reference VG data.

### Critical: Reconciler Wire-Up Changes

In `composePreflightReport` (`pkg/controller/drplan/reconciler.go` lines 815-851), add the new fields to CompositionInput:

```go
input := preflight.CompositionInput{
    Plan:              plan,
    DiscoveryResult:   discovery,
    ConsistencyResult: consistency,
    ChunkResult:       chunks,
    StorageBackends:   storageBackends,
    Waves:             waves,      // NEW — pass the freshly-built WaveInfo data
    LocalSite:         r.LocalSite, // NEW — for PreflightVolumeGroup.Site
}
```

The `waves` parameter must be passed into `composePreflightReport` — this requires extending the function signature:

```go
func (r *DRPlanReconciler) composePreflightReport(
    ctx context.Context,
    plan *soteriav1alpha1.DRPlan,
    discovery *engine.DiscoveryResult,
    consistency *engine.ConsistencyResult,
    chunks *engine.ChunkResult,
    vms []engine.VMReference,
    waves []soteriav1alpha1.WaveInfo,  // NEW
) *soteriav1alpha1.PreflightReport {
```

Find the call site of `composePreflightReport` in the reconciler and pass the `waves` variable. The waves are built earlier in the reconcile cycle (before `composePreflightReport` is called) and passed to `updateStatus` — the same variable is reused here.

### Critical: DiskEnricher Sort Guarantee

Story 9.1's `DiskEnricher` iterates `domain.devices.disks[]` in their spec order (not sorted by name). For deterministic preflight output per AC3, the VG disk enrichment code must sort disks explicitly. Since we iterate VMs in sorted order (by name) and each VM's disks may not be name-sorted, sort the final `pvg.Disks` slice by `(VMName, DiskName)`. But since `VolumeGroupDisk` doesn't store VMName, ensure deterministic output by:

1. Iterating VMs in sorted name order
2. Within each VM, sorting its disks by Name before appending

```go
for _, vmName := range sortedVMNames {
    disks := vmDisks[vmKey{vg.Namespace, vmName}]
    sortedDisks := make([]soteriav1alpha1.DiscoveredDisk, len(disks))
    copy(sortedDisks, disks)
    sort.Slice(sortedDisks, func(i, j int) bool {
        return sortedDisks[i].Name < sortedDisks[j].Name
    })
    for _, d := range sortedDisks {
        pvg.Disks = append(pvg.Disks, soteriav1alpha1.VolumeGroupDisk{...})
    }
}
```

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `PreflightChunk` struct with `VolumeGroups` | `pkg/apis/soteria.io/v1alpha1/types.go:202-214` | Modify field type |
| `ComposeReport` and `CompositionInput` | `internal/preflight/checks.go:1-108` | Extend with Waves + LocalSite |
| VG name append loop | `internal/preflight/checks.go:104-106` | Replace with enriched PreflightVolumeGroup |
| `composePreflightReport` | `pkg/controller/drplan/reconciler.go:815-851` | Extend signature and CompositionInput |
| `VolumeGroupInfo.VMNames` | `pkg/apis/soteria.io/v1alpha1/types.go:271-287` | Use for VM-to-VG membership lookup |
| `DRGroupChunk.VolumeGroups` (engine) | `pkg/engine/chunker.go:48-54` | Source of VGs per chunk |
| `DiscoveredVM.Disks` (after 9.1) | `pkg/apis/soteria.io/v1alpha1/types.go:250-256` | Source of disk data |
| `PreflightReport` struct | `pkg/apis/soteria.io/v1alpha1/types.go:143-172` | No changes needed |
| Console plugin `PreflightChunk` | `console-plugin/src/models/types.ts:157-162` | Update volumeGroups type |
| Console plugin `PreflightReport` | `console-plugin/src/models/types.ts:129-140` | No changes needed |
| `+listType=atomic` marker | Used on all `[]DiscoveredVM`, `[]PreflightChunk` | Same for new slice fields |
| `omitempty` on optional string fields | `DiscoveredDisk.PVCName` pattern from 9.1 | Same for VolumeGroupDisk |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `VolumeGroupDisk`, `PreflightVolumeGroup` types; change `PreflightChunk.VolumeGroups` type | ~25 lines added, ~3 lines changed |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-generated | `make generate` |
| `internal/preflight/checks.go` | Extend `CompositionInput`, update `ComposeReport` VG building | ~30 lines changed |
| `pkg/controller/drplan/reconciler.go` | Extend `composePreflightReport` signature, pass waves + LocalSite | ~5 lines changed |
| `console-plugin/src/models/types.ts` | Add `VolumeGroupDisk`, `PreflightVolumeGroup` interfaces; update `PreflightChunk` | ~15 lines |
| `internal/preflight/checks_test.go` | Update existing tests, add VG disk enrichment tests | ~120 lines |
| `console-plugin/tests/**` | Update test fixtures using `volumeGroups` as string arrays | ~10 lines |
| `config/crd/bases/soteria.io_drplans.yaml` | Auto-generated (new types in preflight) | `make manifests` |

### Execution Order

1. Task 1 (API types) — foundation for everything else; `make manifests generate`
2. Task 2 (extend CompositionInput + reconciler wire-up) — plumbing for data flow
3. Task 3 (enrich ComposeReport) — core enrichment logic
4. Task 5 (unit tests) — validate enrichment before console changes
5. Task 4 (console plugin TS types + test fixture updates)
6. Task 6 (manifests, lint, full test suite)

### Previous Story Learnings (from 9.1, 9.2, 9.3)

- **`DiscoveredVM.Disks` is the single source of truth for disk data** — populated by DiskEnricher in the reconcile loop, available on WaveInfo.VMs and SiteDiscovery.VMs
- **Self-healing via reconcile loop** — if disk data is missing on one cycle, it fills in on the next (30s); preflight VGs with empty disks are valid
- **`collectVMsFromWaves` propagates all VM data to active-site SiteDiscovery** — disk enrichment in waves automatically enriches SiteDiscovery
- **Use cached `client.Reader` for PVC resolution** — but in this story, no PVC GETs are needed (data already in DiscoveredVM.Disks)
- **Admission plugin migration (9.2) does not affect this story** — preflight composition runs in the reconciler, not in admission
- **API types in `pkg/apis/soteria.io/v1alpha1/types.go`** — sample-apiserver pattern, not kubebuilder `api/` convention
- **`internal/preflight/` is a package under `internal/`** — not importable by external drivers, only by the reconciler

### Project Structure Notes

- API types live in `pkg/apis/soteria.io/v1alpha1/types.go` (sample-apiserver pattern, not kubebuilder `api/` convention)
- Preflight composition logic lives in `internal/preflight/checks.go` (internal package)
- Controllers live in `pkg/controller/drplan/` (not `internal/controller/`)
- Console plugin types in `console-plugin/src/models/types.ts`
- Generated files: `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml` — never hand-edit
- Run `make manifests generate` after any `types.go` change

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L143-L172] — PreflightReport struct (no changes needed)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L202-L214] — PreflightChunk struct (change VolumeGroups type)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L250-L256] — DiscoveredVM struct (Disks field from 9.1)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L271-L287] — VolumeGroupInfo struct (VMNames for membership lookup)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L289-L301] — WaveInfo struct (VMs with DiscoveredVM including Disks)
- [Source: internal/preflight/checks.go#L95-L108] — Current VG building in ComposeReport (replace with enriched VGs)
- [Source: pkg/controller/drplan/reconciler.go#L815-L851] — composePreflightReport (extend with waves + LocalSite)
- [Source: pkg/engine/chunker.go#L48-L54] — DRGroupChunk.VolumeGroups (source of VGs per chunk)
- [Source: pkg/engine/executor.go#L1587-L1619] — buildChunkInput (maps VGs to waves)
- [Source: pkg/engine/consistency.go#L89-L199] — ResolveVolumeGroups (VG formation logic)
- [Source: console-plugin/src/models/types.ts#L129-L162] — Console plugin preflight types (update)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-9.4] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, naming conventions, testing pyramid

## Dev Agent Record

### Agent Model Used

Claude Opus 4 (2026-05-07)

### Debug Log References

None — clean implementation with no debug issues.

### Completion Notes List

- Added `VolumeGroupDisk` and `PreflightVolumeGroup` API types to `types.go` with proper kubebuilder markers (+listType=atomic, omitempty)
- Changed `PreflightChunk.VolumeGroups` from `[]string` to `[]PreflightVolumeGroup` (v1alpha1 breaking change)
- Extended `CompositionInput` with `Waves []WaveInfo` and `LocalSite string` fields
- Extended `composePreflightReport` reconciler method signature to accept `waves` parameter; all 6 call sites updated
- Implemented `buildVMDiskIndex` (namespace/name → DiscoveredDisk lookup) and `enrichVolumeGroup` (deterministic VG disk enrichment with VM-name-then-disk-name sorting)
- No additional PVC GETs — all disk data sourced from `DiscoveredVM.Disks` via `WaveInfo`
- Added `VolumeGroupDisk` and `PreflightVolumeGroup` TypeScript interfaces to console plugin types
- Updated existing test that referenced `chunk.VolumeGroups[0]` as string to use `.Name`
- Added 6 new unit tests covering: VM-level VG, namespace-level VG, stateless VM, multiple VGs, missing PVC, no waves fallback
- Added `assertVG` and `assertDisk` test helpers to reduce cyclomatic complexity
- No console plugin test fixtures referenced `volumeGroups` as string arrays (Task 4.4 — no changes needed)
- `make manifests generate` — zero errors; deepcopy and OpenAPI regenerated
- `make lint-fix` — zero new lint errors (2 pre-existing: goconst in executor_test.go, unparam in reconciler.go)
- `make test` — all unit tests pass, zero regressions, preflight coverage 90.6% → 91.9%
- `make integration` — all 6 integration test suites pass, zero regressions

### File List

- `pkg/apis/soteria.io/v1alpha1/types.go` — added VolumeGroupDisk, PreflightVolumeGroup types; changed PreflightChunk.VolumeGroups type
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — auto-generated (make generate)
- `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` — auto-generated (make generate)
- `internal/preflight/checks.go` — extended CompositionInput, added buildVMDiskIndex/enrichVolumeGroup, updated ComposeReport VG building
- `pkg/controller/drplan/reconciler.go` — extended composePreflightReport signature, passed waves+LocalSite, updated all 6 call sites
- `internal/preflight/checks_test.go` — updated existing test, added 6 new VG disk enrichment tests with assertVG/assertDisk helpers
- `console-plugin/src/models/types.ts` — added VolumeGroupDisk, PreflightVolumeGroup interfaces; updated PreflightChunk.volumeGroups type
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — status updated
- `_bmad-output/implementation-artifacts/9-4-volume-group-disk-enrichment-in-preflight.md` — story file updated
