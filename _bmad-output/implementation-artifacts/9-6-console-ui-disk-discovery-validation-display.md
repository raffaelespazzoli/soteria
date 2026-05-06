# Story 9.6: Console UI — Disk Discovery & Validation Display

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the Console to show per-VM disk details, volume group disk composition, and disk validation alerts,
So that I can understand and troubleshoot disk-level DR protection.

## Acceptance Criteria

1. **AC1 — Per-VM disk detail in Site Discovery:** Each VM row in the `SiteDiscoverySection` two-column comparison expands to show its disks (disk name, PVC name, storage class). Disk mismatches between sites are highlighted (different count, missing disk, different storage class). Matching disks are shown in default style.

2. **AC2 — Volume group disk composition in Waves tab:** Each volume group node in `WaveCompositionTree` shows its `PreflightVolumeGroup.disks` (disk name, PVC name, PVC namespace). The site label is shown on the volume group.

3. **AC3 — DisksConsistent=False danger alert (DiskMismatch):** When the `DisksConsistent` condition is `False`, a PatternFly Alert (variant=danger, inline) appears alongside the existing `SitesInSync` alert: "Disk topology does not match across sites — DR operations are blocked". The alert message includes the condition's delta details. An `AlertActionLink` scrolls to the Configuration tab's site discovery section.

4. **AC4 — DisksConsistent=False danger alert (StorageClassMixed):** When the `DisksConsistent` condition is `False` with reason `StorageClassMixed`, the alert message indicates the mixed storage class issue: "Volume group \<name\> has mixed storage classes — all disks must use the same storage class".

5. **AC5 — DRLifecycleDiagram blocked on DisksConsistent=False:** All transition action buttons are disabled with tooltip: "Blocked: disk topology inconsistent across sites". The `isBlocked` check is extended to include `DisksConsistent` alongside `SitesInSync`.

6. **AC6 — Dashboard warning icon & disabled actions:** Plan rows with `DisksConsistent: False` show a warning icon. Kebab menu actions are disabled with tooltip indicating disk inconsistency.

7. **AC7 — Accessibility:** jest-axe audit passes on all states (disks match, disk mismatch, storage class mixed). Disk comparison tables are keyboard navigable. Mismatch indicators have screen reader text.

## Tasks / Subtasks

- [ ] Task 1: Extend TypeScript types for disk data (AC: #1, #2)
  - [ ] 1.1 Add `DiscoveredDisk` interface to `src/models/types.ts`: `{ name: string; pvcName: string; storageClass: string }`
  - [ ] 1.2 Add `disks?: DiscoveredDisk[]` to existing `DiscoveredVM` interface
  - [ ] 1.3 Add `PreflightVolumeGroup` interface: `{ name: string; site: string; disks: VolumeGroupDisk[] }`
  - [ ] 1.4 Add `VolumeGroupDisk` interface: `{ name: string; pvcName: string; pvcNamespace: string }`
  - [ ] 1.5 Change `PreflightChunk.volumeGroups` from `string[]` to `PreflightVolumeGroup[]`
  - [ ] 1.6 Add `disksConsistent?: boolean` and `diskDiscoveryDelta?: string` to `PreflightReport`

- [ ] Task 2: Add `getDisksConsistent` utility (AC: #3, #4, #5, #6)
  - [ ] 2.1 Add `DisksConsistentStatus` interface to `src/utils/drPlanUtils.ts`: `{ consistent: boolean; reason?: string; message?: string }`
  - [ ] 2.2 Add `getDisksConsistent(plan: DRPlan): DisksConsistentStatus` — reads `DisksConsistent` condition; returns `{ consistent: true }` if condition absent (backward compat)
  - [ ] 2.3 Export for use in DRPlanDetailPage, DRDashboard, DRLifecycleDiagram

- [ ] Task 3: Create `DiskDisagreementAlert` component (AC: #3, #4)
  - [ ] 3.1 Create `src/components/DRPlanDetail/DiskDisagreementAlert.tsx` following `SiteDisagreementAlert` pattern
  - [ ] 3.2 Props: `plan: DRPlan`, `onSwitchToConfig: () => void`
  - [ ] 3.3 Return null if `getDisksConsistent(plan).consistent`
  - [ ] 3.4 Render PatternFly `Alert` variant=danger, inline, title contextual to reason:
    - `DiskMismatch`: "Disk topology does not match across sites — DR operations are blocked"
    - `StorageClassMixed`: "Volume group storage classes are mixed — DR operations are blocked"
    - `WaitingForDiskDiscovery`: "Waiting for disk discovery from both sites"
  - [ ] 3.5 Include condition message as alert body (delta details)
  - [ ] 3.6 `AlertActionLink` "View disk details" → calls `onSwitchToConfig`

- [ ] Task 4: Wire `DiskDisagreementAlert` into DRPlanDetailPage (AC: #3, #4, #5)
  - [ ] 4.1 Import `DiskDisagreementAlert` and `getDisksConsistent` in `DRPlanDetailPage.tsx`
  - [ ] 4.2 Compute `disksConsistent` from `getDisksConsistent(plan)` alongside `sitesInSync`
  - [ ] 4.3 Render `DiskDisagreementAlert` below `SiteDisagreementAlert` in the Overview tab's `aria-live` region
  - [ ] 4.4 Extend `isBlocked` prop to DRLifecycleDiagram: `isBlocked={!sitesInSync.inSync || !disksConsistent.consistent}`
  - [ ] 4.5 Compute `blockedTooltip` dynamically: if `!sitesInSync.inSync` use site tooltip, else if `!disksConsistent.consistent` use "Blocked: disk topology inconsistent across sites"

- [ ] Task 5: Extend `SiteDiscoverySection` with per-VM disk expandable rows (AC: #1)
  - [ ] 5.1 Add expandable row state in `SiteColumn` (per-VM expansion via PatternFly `ExpandableRowContent` or inline toggle)
  - [ ] 5.2 When expanded, show nested table: Disk Name | PVC Name | Storage Class
  - [ ] 5.3 Add disk comparison logic: for VMs present on both sites, compare disks by name — highlight missing disks and different storage classes with warning background + icon
  - [ ] 5.4 Pass cross-site comparison data into each `SiteColumn` (partner site's VM disks indexed by `vmKey`)
  - [ ] 5.5 VMs with no disks (stateless) show "No PVC disks" in muted text

- [ ] Task 6: Extend `WaveCompositionTree` with VG disk composition (AC: #2)
  - [ ] 6.1 In `buildDRGroupChunks` or `buildDiscoveredVMNodes`, read `PreflightVolumeGroup` from `plan.status?.preflight?.waves[*].chunks[*].volumeGroups`
  - [ ] 6.2 Render disk list under each volume group node: "disk-name → pvc-name (pvc-namespace)" with site label
  - [ ] 6.3 If `volumeGroups` is still `string[]` (backward compat for plans not yet enriched), render group name only

- [ ] Task 7: Extend Dashboard with DisksConsistent warning (AC: #6)
  - [ ] 7.1 In `DRDashboard.tsx` `enrichPlans`, compute `disksConsistent` per plan alongside `sitesInSync`
  - [ ] 7.2 Add `ExclamationTriangleIcon` + `Tooltip` "Disk topology inconsistent" when `!disksConsistent.consistent` (alongside existing site sync icon)
  - [ ] 7.3 Extend `DRPlanActions` `isDisabled` check: disabled when `!sitesInSync.inSync || !disksConsistent.consistent`
  - [ ] 7.4 Extend `disabledTooltip` to include disk inconsistency message

- [ ] Task 8: Tests — DiskDisagreementAlert (AC: #7)
  - [ ] 8.1 Create `tests/components/DiskDisagreementAlert.test.tsx`
  - [ ] 8.2 Test: renders null when DisksConsistent absent (backward compat)
  - [ ] 8.3 Test: renders null when DisksConsistent=True
  - [ ] 8.4 Test: renders danger alert with DiskMismatch title
  - [ ] 8.5 Test: renders danger alert with StorageClassMixed title
  - [ ] 8.6 Test: renders WaitingForDiskDiscovery alert (info, not danger)
  - [ ] 8.7 Test: action link calls onSwitchToConfig
  - [ ] 8.8 jest-axe passes on all states

- [ ] Task 9: Tests — SiteDiscoverySection disk expansion (AC: #7)
  - [ ] 9.1 Update `tests/components/SiteDiscoverySection.test.tsx`
  - [ ] 9.2 Test: VM row is expandable when disks present
  - [ ] 9.3 Test: expanded row shows disk table (name, PVC, SC)
  - [ ] 9.4 Test: disk mismatch highlighted (different SC on same disk)
  - [ ] 9.5 Test: missing disk on one site highlighted
  - [ ] 9.6 Test: stateless VM shows "No PVC disks"
  - [ ] 9.7 jest-axe passes on expanded/collapsed states

- [ ] Task 10: Tests — WaveCompositionTree disk composition (AC: #7)
  - [ ] 10.1 Update `tests/components/WaveCompositionTree.test.tsx`
  - [ ] 10.2 Test: VG node shows disk list with PVC details
  - [ ] 10.3 Test: VG node shows site label
  - [ ] 10.4 Test: backward compat — string[] volumeGroups render as names only
  - [ ] 10.5 jest-axe passes

- [ ] Task 11: Tests — DRPlanDetailPage integration (AC: #7)
  - [ ] 11.1 Update `tests/components/DRPlanDetailPage.test.tsx`
  - [ ] 11.2 Test: DiskDisagreementAlert renders when DisksConsistent=False
  - [ ] 11.3 Test: DRLifecycleDiagram isBlocked when DisksConsistent=False (sites in sync)
  - [ ] 11.4 Test: both alerts render when both conditions False

- [ ] Task 12: Tests — DRDashboard DisksConsistent (AC: #7)
  - [ ] 12.1 Update `tests/components/DRDashboard.test.tsx`
  - [ ] 12.2 Test: warning icon appears for plan with DisksConsistent=False
  - [ ] 12.3 Test: kebab actions disabled when DisksConsistent=False
  - [ ] 12.4 jest-axe passes

## Dev Notes

### Scope & Approach

This is a TypeScript/React Console plugin story. All changes are in `console-plugin/`. No Go backend changes.

**Change pattern:** Types → utility function → alert component → wire into detail page → extend site discovery section → extend wave tree → extend dashboard → tests.

**Prerequisites:** Stories 9.1–9.5 (backend disk enrichment + validation conditions). The backend will populate:
- `DiscoveredVM.disks` on `SiteDiscovery.vms` and `WaveInfo.vms`
- `DisksConsistent` condition with reasons `DisksAgreed`, `DiskMismatch`, `WaitingForDiskDiscovery`, `StorageClassMixed`
- `PreflightReport.disksConsistent` boolean and `diskDiscoveryDelta` string
- `PreflightChunk.volumeGroups` as `[]PreflightVolumeGroup` (enriched type from 9.4)

### Critical: Follow the SitesInSync Pattern Exactly

Story 8.4 established the exact pattern for condition-driven blocking + alerts. Story 9.6 is the disk-level mirror of 8.4:

| Aspect | Story 8.4 (SitesInSync) | Story 9.6 (DisksConsistent) |
|--------|------------------------|---------------------------|
| Utility function | `getSitesInSync()` | `getDisksConsistent()` |
| Alert component | `SiteDisagreementAlert` | `DiskDisagreementAlert` |
| DRLifecycleDiagram prop | `isBlocked={!sitesInSync.inSync}` | Extend: `\|\| !disksConsistent.consistent` |
| Dashboard icon | `ExclamationTriangleIcon` + Tooltip | Same pattern, different message |
| Actions disabled | `DRPlanActions isDisabled` | Extend check |
| Scroll target | `#site-discovery-section` | Same target (disk details are in the same section) |

### Critical: TypeScript Type Changes

The `DiscoveredVM` interface currently has only `name` and `namespace`. After 9.1 the backend populates `disks` on this type:

```typescript
export interface DiscoveredDisk {
  name: string;
  pvcName: string;
  storageClass: string;
}

export interface DiscoveredVM {
  name: string;
  namespace: string;
  disks?: DiscoveredDisk[];  // NEW — from 9.1
}
```

The `PreflightChunk.volumeGroups` changes from `string[]` to the enriched type:

```typescript
export interface VolumeGroupDisk {
  name: string;
  pvcName: string;
  pvcNamespace: string;
}

export interface PreflightVolumeGroup {
  name: string;
  site: string;
  disks: VolumeGroupDisk[];
}

export interface PreflightChunk {
  name: string;
  vmCount: number;
  vmNames?: string[];
  volumeGroups?: PreflightVolumeGroup[];  // CHANGED — from string[] to enriched type
}
```

### Critical: Backward Compatibility

The `volumeGroups` field on `PreflightChunk` may still be `string[]` for plans that haven't been reconciled since 9.4 was deployed. Detect at runtime:

```typescript
const vgs = chunk.volumeGroups ?? [];
if (vgs.length > 0 && typeof vgs[0] === 'string') {
  // Legacy: render as plain names
} else {
  // Enriched: render disk details
}
```

### Critical: Disk Cross-Site Comparison Logic

For the `SiteDiscoverySection` per-VM disk expansion, compare disks between sites by `DiscoveredDisk.name` (not PVC name — PVC names differ across sites):

```typescript
function compareDisksBySite(
  primaryDisks: DiscoveredDisk[] | undefined,
  secondaryDisks: DiscoveredDisk[] | undefined,
): { matches: DiskComparison[]; primaryOnly: DiscoveredDisk[]; secondaryOnly: DiscoveredDisk[] }
```

A disk "matches" if both sites have a disk with the same `name`. Within a match, highlight if `storageClass` differs. Disks present on one site but not the other are "primaryOnly" / "secondaryOnly".

### Critical: Expandable Rows in SiteDiscoverySection

Use PatternFly's composable table expandable row pattern:
- Each VM row gets an expand toggle cell
- When expanded, render a nested content row with the disk detail table
- Use `ExpandableRowContent` from `@patternfly/react-table`
- Track expanded state per VM as `Set<string>` (using `vmKey`)

### Critical: CSS/Styling Rules

- **No hardcoded colors** — use PF6 tokens: `var(--pf-t--global--*, var(--pf-v5-global--*))`
- Mismatch highlighting: use `--pf-t--global--color--status--warning--default` (same pattern as existing VM mismatch rows)
- Storage class mismatch: use danger variant `--pf-t--global--color--status--danger--default`
- Text sizes: minimum 14px for status text (bridge-call readability)

### Critical: Accessibility Requirements

- All disk comparison tables must have proper `aria-label` attributes
- Expandable rows need `aria-expanded` state
- Mismatch indicators need `aria-hidden="true"` on decorative icons with adjacent screen reader text (`.pf-v5-u-screen-reader` class)
- Keyboard: Tab through expandable toggles, Enter/Space to expand
- `AlertActionLink` in `DiskDisagreementAlert` must be keyboard-focusable
- Use PatternFly's built-in accessibility — don't reinvent

### Critical: Data Fetching — No New Hooks Needed

All disk data arrives via existing `useDRPlan()` hook (which watches the DRPlan resource). The `DisksConsistent` condition lives on `plan.status.conditions`, and disk data lives on `plan.status.primarySiteDiscovery.vms[*].disks` and `plan.status.waves[*].vms[*].disks`. No additional API calls required.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| Condition-driven utility | `drPlanUtils.ts:getSitesInSync()` | Follow for `getDisksConsistent()` |
| Danger alert with action link | `SiteDisagreementAlert.tsx` | Follow for `DiskDisagreementAlert` |
| Per-VM table with mismatch highlighting | `SiteDiscoverySection.tsx` | Extend with expandable disk rows |
| Dashboard warning icon + tooltip | `DRDashboard.tsx:270-294` | Extend for DisksConsistent |
| `isBlocked` + `blockedTooltip` props | `DRLifecycleDiagram.tsx:167-185` | Extend condition |
| `DRPlanActions isDisabled` | `DRDashboard.tsx` + `DRPlanActions.tsx` | Extend condition |
| TreeView VG rendering | `WaveCompositionTree.tsx:213-256` | Extend with disk nodes |
| `aria-live` region for alerts | `DRPlanDetailPage.tsx:121-125` | Add DiskDisagreementAlert in same region |
| PF6 token dual-fallback | All components | `var(--pf-t--global--*, var(--pf-v5-global--*))` |
| Screen reader text for icons | `SiteDiscoverySection.tsx:152` | `.pf-v5-u-screen-reader` on icon sibling |
| jest-axe in tests | All `*.test.tsx` | `const { container } = render(...); expect(await axe(container)).toHaveNoViolations()` |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `src/models/types.ts` | Add `DiscoveredDisk`, `PreflightVolumeGroup`, `VolumeGroupDisk` interfaces; extend `DiscoveredVM`, `PreflightChunk`, `PreflightReport` | ~25 lines added |
| `src/utils/drPlanUtils.ts` | Add `DisksConsistentStatus` interface + `getDisksConsistent()` function | ~15 lines |
| `src/components/DRPlanDetail/DiskDisagreementAlert.tsx` | **NEW** — danger alert for DisksConsistent=False | ~50 lines |
| `src/components/DRPlanDetail/DRPlanDetailPage.tsx` | Import + wire DiskDisagreementAlert, extend isBlocked/tooltip | ~10 lines modified |
| `src/components/DRPlanDetail/SiteDiscoverySection.tsx` | Add expandable disk rows with cross-site comparison | ~120 lines added |
| `src/components/DRPlanDetail/WaveCompositionTree.tsx` | Render VG disk details under VG nodes | ~40 lines added |
| `src/components/DRDashboard/DRDashboard.tsx` | Add DisksConsistent icon + extend disabled check | ~15 lines modified |
| `src/components/DRDashboard/DRPlanActions.tsx` | Extend isDisabled condition | ~3 lines modified |
| `tests/components/DiskDisagreementAlert.test.tsx` | **NEW** — full test coverage | ~120 lines |
| `tests/components/SiteDiscoverySection.test.tsx` | Add disk expansion tests | ~80 lines added |
| `tests/components/WaveCompositionTree.test.tsx` | Add VG disk tests | ~50 lines added |
| `tests/components/DRPlanDetailPage.test.tsx` | Add DisksConsistent integration tests | ~40 lines added |
| `tests/components/DRDashboard.test.tsx` | Add DisksConsistent warning tests | ~30 lines added |

### Execution Order

1. Task 1 (types) — foundation for everything else
2. Task 2 (utility function) — needed by all UI components
3. Task 3 (DiskDisagreementAlert component)
4. Task 4 (wire into DRPlanDetailPage)
5. Task 5 (SiteDiscoverySection disk expansion)
6. Task 6 (WaveCompositionTree VG disk nodes)
7. Task 7 (Dashboard warning)
8. Tasks 8–12 (tests — can be done in parallel or after each feature task)

### Previous Story Learnings (from 8.4 and 9.1–9.5)

- **Story 8.4 established the exact UI pattern** — `SiteDisagreementAlert` + `getSitesInSync` + `isBlocked` prop + Dashboard icon. Story 9.6 is a mirror of this for disks. Follow it precisely.
- **`useK8sWatchResource` delivers all data via existing plan watch** — no new hooks or API calls needed. Disk data is on the plan resource.
- **PF6 token dual-fallback is mandatory** — every color/spacing token must have `var(--pf-t--*, var(--pf-v5-*))` pattern for backward compat during PF5→PF6 migration.
- **jest-axe on every component state** — Epic 6/7 enforced this as a gate. No PR merges without accessibility audit passing.
- **`isBlocked` is a simple boolean prop** — `DRLifecycleDiagram` doesn't need to know which condition blocked. The tooltip explains it.
- **Data-exists gate (rule #10)** — verify `DiscoveredVM.disks` is actually populated by the backend before building UI around it. The field is optional (`disks?`) and may be empty for plans not yet reconciled with 9.1 code. Handle gracefully (show "Disks not yet discovered" or hide section).
- **Backward compat for PreflightChunk.volumeGroups** — old plans will have `string[]`, new plans will have `PreflightVolumeGroup[]`. Type guard at runtime.

### Project Structure Notes

- Console plugin source: `console-plugin/src/` (TypeScript/React, separate from Go code)
- Components organized by page: `DRDashboard/`, `DRPlanDetail/`, `ExecutionDetail/`, `shared/`
- Tests: `console-plugin/tests/components/` (co-located by component name)
- Types: `console-plugin/src/models/types.ts` (single file for all CRD interfaces)
- Utilities: `console-plugin/src/utils/drPlanUtils.ts` (plan-related helpers)
- PatternFly 6 + React + Webpack module federation — standard OCP dynamic plugin architecture
- No external UI libraries allowed (project-context rule)
- Data fetching: Console SDK hooks exclusively (`useK8sWatchResource`) — no direct API calls

### References

- [Source: console-plugin/src/models/types.ts#L108-L118] — DiscoveredVM + VolumeGroupInfo interfaces (extend these)
- [Source: console-plugin/src/models/types.ts#L129-L162] — PreflightReport + PreflightChunk (extend these)
- [Source: console-plugin/src/utils/drPlanUtils.ts#L104-L122] — SitesInSyncStatus + getSitesInSync (follow pattern)
- [Source: console-plugin/src/components/DRPlanDetail/SiteDisagreementAlert.tsx] — Alert pattern to mirror
- [Source: console-plugin/src/components/DRPlanDetail/SiteDiscoverySection.tsx] — Two-column VM comparison (extend with disks)
- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx#L118-L136] — Tab layout, alert wiring, isBlocked
- [Source: console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx#L213-L256] — TreeView VG rendering (extend)
- [Source: console-plugin/src/components/DRDashboard/DRDashboard.tsx#L270-L294] — Warning icon pattern
- [Source: _bmad-output/planning-artifacts/epics.md#Story-9.6] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, PF6 tokens, SDK constraints

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
