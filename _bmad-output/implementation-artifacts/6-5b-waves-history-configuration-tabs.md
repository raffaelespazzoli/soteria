# Story 6.5b: Waves, History & Configuration Tabs

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want the Plan Detail's Waves, History, and Configuration tabs populated with wave composition, execution history, and plan metadata,
So that I can drill into plan structure, review past executions, and inspect configuration details.

## Acceptance Criteria

1. **AC1 — Waves tab (WaveCompositionTree):** When the Waves tab is selected, a `WaveCompositionTree` component renders the wave hierarchy using PatternFly TreeView with custom renderers (UX-DR9). Each wave node shows: wave label (e.g., "Wave 1"), VM count, and aggregate replication health. Waves default to collapsed and expand on click. Expanded waves show DRGroup chunk visualization (based on `maxConcurrentFailovers`) with per-VM rows: name, namespace, storage backend, consistency level, replication health, and RPO. Namespace-consistent VMs are visually grouped.

2. **AC2 — History tab (ExecutionHistoryTable):** When the History tab is selected, a PatternFly Table (compact) lists all DRExecution records for this plan (UX-DR11, FR42). Columns: Date, Mode (Planned/Disaster), Result (status badge), Duration, RPO, Triggered By. Row click navigates to the Execution Detail view via `Link` to `/disaster-recovery/executions/:name`.

3. **AC3 — Configuration tab (PlanConfiguration):** When the Configuration tab is selected, a PatternFly `DescriptionList` shows plan metadata: name, label selector, wave label, maxConcurrentFailovers, creation date. Labels and annotations are visible. A PatternFly `CodeBlock` shows the DRPlan CRD spec in YAML (read-only).

4. **AC4 — Configuration tab (ReplicationHealthIndicator expanded):** The Configuration tab includes a `ReplicationHealthIndicator` in expanded variant showing per-volume-group health, RPO, and freshness (UX-DR8). Each volume group row shows: name, health status badge, RPO value, and "last checked" timestamp.

5. **AC5 — History tab empty state:** When no DRExecution records exist for this plan, a compact PatternFly `EmptyState` displays: "No executions yet — trigger a planned migration to validate your DR plan" (UX-DR12).

6. **AC6 — Accessibility:** All three tab panels pass `jest-axe` with zero violations. WaveCompositionTree uses ARIA tree role; keyboard arrow keys expand/collapse nodes. ExecutionHistoryTable rows are keyboard-navigable. Screen readers can access complete status strings for all health indicators and badges.

## Tasks / Subtasks

- [x] Task 1: Replace Waves tab placeholder with WaveCompositionTree (AC: #1)
  - [x] 1.1 Create `src/components/DRPlanDetail/WaveCompositionTree.tsx` — accepts `plan: DRPlan`, renders PatternFly `TreeView` with `TreeViewDataItem[]`
  - [x] 1.2 Build wave data from `plan.status.waves[]`: each wave becomes a tree node with `name: "Wave {n}"`, `title` showing VM count and aggregate health badge, `children` containing DRGroup chunks
  - [x] 1.3 DRGroup chunk children: group VMs by `maxConcurrentFailovers` threshold; each chunk node shows "DRGroup chunk {n} (maxConcurrent: {value})"
  - [x] 1.4 Per-VM leaf nodes: render name, namespace, storage backend (PatternFly `Label`), consistency level (namespace name or "VM-level"), `ReplicationHealthIndicator` compact from Story 6.3 for health + RPO
  - [x] 1.5 Aggregate health per wave: compute worst-case health across all VMs in the wave — Error > Degraded > Unknown > Healthy
  - [x] 1.6 Namespace-consistent VMs: VMs sharing a namespace get a blue info `Label` showing "NS: {namespace}"
  - [x] 1.7 Default all waves to collapsed (`defaultExpanded: false` on TreeViewDataItem)
  - [x] 1.8 Wire into `DRPlanDetailPage.tsx` — replace Waves tab placeholder content

- [x] Task 2: Replace History tab placeholder with ExecutionHistoryTable (AC: #2, #5)
  - [x] 2.1 Create `src/components/DRPlanDetail/ExecutionHistoryTable.tsx` — accepts `executions: DRExecution[]`, `planName: string`
  - [x] 2.2 Filter executions by `spec.planName === planName` (defensive — the hook should already filter, but guard against full-list data)
  - [x] 2.3 Render PatternFly Table (composable, compact) with columns: Date (`status.startTime` formatted), Mode (`spec.mode` — display "Planned Migration" or "Disaster"), Result (`ExecutionResultBadge` from Story 6.3), Duration (computed from `startTime`/`completionTime` using `formatDuration` from Story 6.3), RPO (from execution status or "N/A"), Triggered By (from annotations or "N/A")
  - [x] 2.4 Sort by Date descending (most recent first) — default sort
  - [x] 2.5 Row click navigates via `Link` to `/disaster-recovery/executions/${execution.metadata.name}`
  - [x] 2.6 Empty state: when `executions.length === 0`, render PatternFly `EmptyState` (variant="sm") with `EmptyStateIcon` (CubesIcon or similar), title "No executions yet", body "Trigger a planned migration to validate your DR plan"
  - [x] 2.7 Wire into `DRPlanDetailPage.tsx` — replace History tab placeholder; use `useDRExecutions(planName)` hook from Story 6.1

- [x] Task 3: Replace Configuration tab placeholder with PlanConfiguration (AC: #3, #4)
  - [x] 3.1 Create `src/components/DRPlanDetail/PlanConfiguration.tsx` — accepts `plan: DRPlan`
  - [x] 3.2 Render PatternFly `DescriptionList` (horizontal, compact) with terms: Name (`metadata.name`), Label Selector (`spec.labelSelector`), Wave Label (`spec.waveLabel`), Max Concurrent Failovers (`spec.maxConcurrentFailovers`), Primary Site (`spec.primarySite`), Secondary Site (`spec.secondarySite`), Created (`metadata.creationTimestamp` formatted)
  - [x] 3.3 Render labels section: iterate `metadata.labels` as PatternFly `Label` components in a `LabelGroup`
  - [x] 3.4 Render annotations section: iterate `metadata.annotations` as key-value pairs (skip internal kubernetes.io annotations)
  - [x] 3.5 Create `src/components/DRPlanDetail/ReplicationHealthExpanded.tsx` — expanded variant of ReplicationHealthIndicator showing per-volume-group table: VG Name, Health badge, RPO, Last Checked. Data from `plan.status.conditions` (ReplicationHealthy) and volume group data if available
  - [x] 3.6 Render PatternFly `CodeBlock` with `code={yamlDump(plan.spec)}` — YAML serialization of `plan.spec` as read-only. Use `js-yaml` if available in the template, otherwise JSON.stringify with formatting
  - [x] 3.7 Wire into `DRPlanDetailPage.tsx` — replace Configuration tab placeholder

- [x] Task 4: Wire all tabs into DRPlanDetailPage (AC: #1–#5)
  - [x] 4.1 Modify `DRPlanDetailPage.tsx` — import `WaveCompositionTree`, `ExecutionHistoryTable`, `PlanConfiguration`
  - [x] 4.2 Add `useDRExecutions(planName)` call to fetch execution data for History tab
  - [x] 4.3 Replace Waves tab `TabContent` placeholder with `<WaveCompositionTree plan={plan} />`
  - [x] 4.4 Replace History tab `TabContent` placeholder with `<ExecutionHistoryTable executions={executions} planName={name} />`
  - [x] 4.5 Replace Configuration tab `TabContent` placeholder with `<PlanConfiguration plan={plan} />`
  - [x] 4.6 Handle loading state for executions (Skeleton or Spinner in History tab while loading)

- [x] Task 5: Accessibility (AC: #6)
  - [x] 5.1 WaveCompositionTree: PatternFly TreeView provides `role="tree"` and `role="treeitem"` automatically — verify it renders correctly. Arrow keys navigate and expand/collapse nodes.
  - [x] 5.2 Per-VM health in WaveCompositionTree: ensure each health indicator is readable as a single string (e.g., "erp-db-1, odf-storage, VM-level consistency, replication healthy, RPO 8 seconds")
  - [x] 5.3 ExecutionHistoryTable: standard PatternFly Table accessibility — rows are navigable via keyboard, status badges include text labels alongside color
  - [x] 5.4 PlanConfiguration: DescriptionList has native accessibility support. CodeBlock is read-only, accessible by default.
  - [x] 5.5 Empty state: EmptyState component is screen-reader-friendly by default

- [x] Task 6: Tests (AC: #1–#6)
  - [x] 6.1 Create `tests/components/WaveCompositionTree.test.tsx`:
    - Renders wave nodes with correct labels ("Wave 1", "Wave 2", etc.)
    - Shows VM count and aggregate health per wave header
    - Waves default to collapsed (VM details not visible)
    - Expanding a wave reveals DRGroup chunks and per-VM rows
    - Per-VM rows show name, namespace, storage, health badge, RPO
    - Namespace-consistent VMs show "NS: {namespace}" label
    - Renders correctly with empty waves array (edge case)
  - [x] 6.2 Create `tests/components/ExecutionHistoryTable.test.tsx`:
    - Renders table with correct columns: Date, Mode, Result, Duration, RPO, Triggered By
    - Rows display DRExecution data correctly
    - Result column uses ExecutionResultBadge (Succeeded/PartiallySucceeded/Failed)
    - Mode displays "Planned Migration" for `planned_migration`, "Disaster" for `disaster`
    - Row click navigates to execution detail (Link renders with correct href)
    - Empty state renders when no executions exist
    - Default sort is date descending
  - [x] 6.3 Create `tests/components/PlanConfiguration.test.tsx`:
    - DescriptionList renders all plan metadata fields
    - Labels render as PatternFly Label components
    - CodeBlock renders plan spec as YAML/JSON
    - ReplicationHealthExpanded renders per-VG health table
    - Handles plan with no labels/annotations gracefully
  - [x] 6.4 Run `jest-axe` on WaveCompositionTree — zero violations
  - [x] 6.5 Run `jest-axe` on ExecutionHistoryTable (with data and empty state) — zero violations
  - [x] 6.6 Run `jest-axe` on PlanConfiguration — zero violations
  - [x] 6.7 Verify `yarn build` succeeds with all new components

### Review Findings

- [x] [Review][Patch] Support `Syncing` replication health without crashing plan-detail indicators [`console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx:16`]
- [x] [Review][Patch] Render wave children when `status.waves[].vms` exist but `groups` are absent [`console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx:160`]
- [x] [Review][Patch] Make execution-detail navigation work for the full history row, not only the Date cell [`console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx:82`]
- [x] [Review][Patch] Preserve zero-second RPO values in the replication-health table instead of showing them as missing [`console-plugin/src/components/DRPlanDetail/ReplicationHealthExpanded.tsx:51`]
- [x] [Review][Patch] Show degraded/error counts in the wave aggregate health badge [`console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx:53`]
- [x] [Review][Patch] Always render the Label Selector field in configuration, even when the plan has no selector [`console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx:57`]

## Dev Notes

### Dependency on Stories 6.1, 6.2, 6.3, and 6.5

**From Story 6.1:**
- `src/models/types.ts` — DRPlan, DRExecution TypeScript interfaces with full status fields
- `src/hooks/useDRResources.ts` — `useDRPlan(name)`, `useDRExecution(name)`, `useDRExecutions(planName?)` hooks
- PatternFly 5 + Console SDK + Jest + jest-axe + RTL configured

**From Story 6.2:**
- React Router v7 import pattern: `import { useParams, Link } from 'react-router'` (NOT `react-router-dom`)
- Default exports for page components (required by `$codeRef` / webpack module federation)

**From Story 6.3:**
- `src/components/shared/ReplicationHealthIndicator.tsx` — compact variant (icon + health label + RPO). **Reuse in WaveCompositionTree per-VM rows.**
- `src/components/shared/PhaseBadge.tsx` — phase status badge. Not directly used in 6.5b tabs but available.
- `src/components/shared/ExecutionResultBadge.tsx` — execution result badge (Succeeded/PartiallySucceeded/Failed). **Reuse in ExecutionHistoryTable.**
- `src/utils/drPlanUtils.ts` — `getEffectivePhase(plan)`, `getReplicationHealth(plan)`. **Reuse for health aggregation.**
- `src/utils/formatters.ts` — `formatDuration(start, end)`, `formatRPO(seconds)`, `formatRelativeTime(date)`. **Reuse in all three tabs.**
- Mock pattern for `useK8sWatchResource`: `jest.fn(() => [mockData, true, null])`

**From Story 6.5:**
- `src/components/DRPlanDetail/DRPlanDetailPage.tsx` — page shell with four tabs; Waves, History, Configuration tabs currently render placeholder `TabContent`. **This file is the integration point — replace placeholders.**
- `src/components/DRPlanDetail/PlanHeader.tsx` — plan header in Overview tab (unchanged)
- `src/components/DRPlanDetail/DRLifecycleDiagram.tsx` — lifecycle diagram in Overview tab (unchanged)
- `src/components/DRPlanDetail/TransitionProgressBanner.tsx` — progress banner (unchanged)
- The page already calls `useDRPlan(name)` and `useDRExecution(activeExecName)` — add `useDRExecutions(name)` for History tab

### PatternFly TreeView API

Use PatternFly's `TreeView` component from `@patternfly/react-core`:

```typescript
import { TreeView, TreeViewDataItem } from '@patternfly/react-core';

const waveItems: TreeViewDataItem[] = plan.status?.waves?.map((wave, idx) => ({
  name: `Wave ${idx + 1}`,
  title: (
    <span>
      Wave {idx + 1} — {wave.groups?.reduce((sum, g) => sum + (g.vms?.length ?? 0), 0)} VMs
      <AggregateHealthBadge groups={wave.groups} />
    </span>
  ),
  id: `wave-${idx}`,
  children: buildDRGroupChunks(wave.groups, plan.spec.maxConcurrentFailovers),
  defaultExpanded: false,
})) ?? [];

<TreeView data={waveItems} aria-label="Wave composition" />
```

**Key API details:**
- `TreeViewDataItem.name` — text label (used for filtering and accessibility)
- `TreeViewDataItem.title` — ReactNode for custom rendering (can include badges, icons)
- `TreeViewDataItem.children` — nested TreeViewDataItem array
- `TreeViewDataItem.defaultExpanded` — initial expand state (set `false` for all waves)
- `TreeViewDataItem.id` — unique identifier
- Keyboard: Arrow keys expand/collapse, Enter selects
- TreeView provides `role="tree"` and `role="treeitem"` automatically

### DRGroup Chunk Visualization

VMs within a wave are chunked into DRGroups based on `maxConcurrentFailovers`. The chunking logic in the engine (`pkg/engine/chunker.go`) respects namespace-level consistency grouping (VMs sharing a namespace are grouped together). For the tree, approximate this:

```typescript
function buildDRGroupChunks(
  groups: DRGroupStatus[] | undefined,
  maxConcurrent: number
): TreeViewDataItem[] {
  if (!groups?.length) return [];

  const chunkSize = maxConcurrent || groups.length;
  const chunks: TreeViewDataItem[] = [];

  for (let i = 0; i < groups.length; i += chunkSize) {
    const chunk = groups.slice(i, i + chunkSize);
    chunks.push({
      name: `DRGroup chunk ${Math.floor(i / chunkSize) + 1}`,
      title: (
        <span>DRGroup chunk {Math.floor(i / chunkSize) + 1} (maxConcurrent: {chunkSize})</span>
      ),
      id: `chunk-${i}`,
      children: chunk.flatMap(g => buildVMNodes(g)),
      defaultExpanded: true,
    });
  }
  return chunks;
}
```

**Per-VM leaf nodes:**

```typescript
function buildVMNodes(group: DRGroupStatus): TreeViewDataItem[] {
  return (group.vms ?? []).map(vm => ({
    name: vm.name,
    title: <VMNodeContent vm={vm} namespace={group.namespace} />,
    id: `vm-${vm.name}`,
  }));
}
```

### VMNodeContent — Per-VM Row in Tree

Each VM leaf shows: name, namespace, storage backend, consistency level, health, RPO.

```typescript
function VMNodeContent({ vm, namespace }: { vm: VMInfo; namespace?: string }) {
  return (
    <div style={{ display: 'flex', gap: 'var(--pf-v5-global--spacer--sm)', alignItems: 'center', flexWrap: 'wrap' }}>
      <span style={{ fontWeight: 'var(--pf-v5-global--FontWeight--semi-bold)' }}>{vm.name}</span>
      <Label isCompact>{vm.storageBackend ?? 'unknown'}</Label>
      {namespace ? (
        <Label isCompact color="blue">NS: {namespace}</Label>
      ) : (
        <span style={{ fontSize: 'var(--pf-v5-global--FontSize--sm)', color: 'var(--pf-v5-global--Color--200)' }}>VM-level</span>
      )}
      <ReplicationHealthIndicator health={vm.health} rpo={vm.rpo} compact />
    </div>
  );
}
```

### Aggregate Health Computation

Compute worst-case health across all VMs in a wave for the collapsed header badge:

```typescript
function getAggregateHealth(groups: DRGroupStatus[]): 'Healthy' | 'Degraded' | 'Error' | 'Unknown' {
  const allHealthStatuses = groups.flatMap(g =>
    (g.vms ?? []).map(vm => vm.health ?? 'Unknown')
  );
  if (allHealthStatuses.includes('Error')) return 'Error';
  if (allHealthStatuses.includes('Degraded')) return 'Degraded';
  if (allHealthStatuses.includes('Unknown')) return 'Unknown';
  return 'Healthy';
}
```

Display in wave header:
- All Healthy → green `Label` "All Healthy"
- N Degraded → yellow `Label` "N Degraded"
- N Error → red `Label` "N Error"

### ExecutionHistoryTable — Compact Table Pattern

Reuse the composable Table API from Story 6.3:

```typescript
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import { Link } from 'react-router';

function ExecutionHistoryTable({ executions, planName }: Props) {
  const filtered = executions.filter(e => e.spec?.planName === planName);
  const sorted = [...filtered].sort((a, b) =>
    new Date(b.status?.startTime ?? 0).getTime() - new Date(a.status?.startTime ?? 0).getTime()
  );

  if (sorted.length === 0) return <HistoryEmptyState />;

  return (
    <Table aria-label="Execution history" variant="compact">
      <Thead>
        <Tr>
          <Th>Date</Th>
          <Th>Mode</Th>
          <Th>Result</Th>
          <Th>Duration</Th>
          <Th>RPO</Th>
          <Th>Triggered By</Th>
        </Tr>
      </Thead>
      <Tbody>
        {sorted.map(exec => (
          <Tr key={exec.metadata?.name} isClickable>
            <Td>
              <Link to={`/disaster-recovery/executions/${exec.metadata?.name}`}>
                {formatDate(exec.status?.startTime)}
              </Link>
            </Td>
            <Td>{formatMode(exec.spec?.mode)}</Td>
            <Td><ExecutionResultBadge result={exec.status?.result} /></Td>
            <Td>{formatDuration(exec.status?.startTime, exec.status?.completionTime)}</Td>
            <Td>{exec.status?.rpoSeconds ? formatRPO(exec.status.rpoSeconds) : 'N/A'}</Td>
            <Td>{exec.metadata?.annotations?.['soteria.io/triggered-by'] ?? 'N/A'}</Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
  );
}
```

**Mode display mapping:**
- `planned_migration` → "Planned Migration"
- `disaster` → "Disaster"
- `reprotect` → "Re-protect"

### PlanConfiguration — DescriptionList Pattern

```typescript
import {
  DescriptionList, DescriptionListGroup, DescriptionListTerm,
  DescriptionListDescription, CodeBlock, CodeBlockCode,
  Label, LabelGroup,
} from '@patternfly/react-core';

<DescriptionList isHorizontal isCompact>
  <DescriptionListGroup>
    <DescriptionListTerm>Name</DescriptionListTerm>
    <DescriptionListDescription>{plan.metadata?.name}</DescriptionListDescription>
  </DescriptionListGroup>
  <DescriptionListGroup>
    <DescriptionListTerm>Label Selector</DescriptionListTerm>
    <DescriptionListDescription>
      <code>{plan.spec?.labelSelector}</code>
    </DescriptionListDescription>
  </DescriptionListGroup>
  {/* ... wave label, maxConcurrentFailovers, primarySite, secondarySite, created */}
</DescriptionList>
```

### ReplicationHealthExpanded — Per-VG Breakdown

The expanded ReplicationHealthIndicator shows a table of per-volume-group health. Data source is `plan.status.conditions` (for the aggregate `ReplicationHealthy` condition) and potentially `plan.status.volumeGroupHealth` if available:

```typescript
function ReplicationHealthExpanded({ plan }: { plan: DRPlan }) {
  const healthCondition = plan.status?.conditions?.find(
    c => c.type === 'ReplicationHealthy'
  );
  const vgHealth = plan.status?.volumeGroupHealth ?? [];

  if (vgHealth.length === 0) {
    return (
      <div>
        <ReplicationHealthIndicator health={healthCondition} compact={false} />
        <Text component="small" style={{ color: 'var(--pf-v5-global--Color--200)' }}>
          Per-volume-group breakdown not available
        </Text>
      </div>
    );
  }

  return (
    <Table aria-label="Replication health by volume group" variant="compact">
      <Thead>
        <Tr>
          <Th>Volume Group</Th>
          <Th>Health</Th>
          <Th>RPO</Th>
          <Th>Last Checked</Th>
        </Tr>
      </Thead>
      <Tbody>
        {vgHealth.map(vg => (
          <Tr key={vg.name}>
            <Td>{vg.name}</Td>
            <Td><ReplicationHealthIndicator health={vg.status} rpo={vg.rpoSeconds} compact /></Td>
            <Td>{vg.rpoSeconds ? formatRPO(vg.rpoSeconds) : 'N/A'}</Td>
            <Td>{vg.lastChecked ? formatRelativeTime(vg.lastChecked) : 'N/A'}</Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
  );
}
```

### YAML CodeBlock for Plan Spec

Use PatternFly's `CodeBlock` + `CodeBlockCode` for the read-only YAML view:

```typescript
import { CodeBlock, CodeBlockCode } from '@patternfly/react-core';

const specYaml = JSON.stringify(plan.spec, null, 2);
// If js-yaml is available: yaml.dump(plan.spec, { indent: 2 })

<CodeBlock>
  <CodeBlockCode id="plan-spec-yaml">{specYaml}</CodeBlockCode>
</CodeBlock>
```

If `js-yaml` is not in the template dependencies, use `JSON.stringify` with 2-space indent as a fallback. Do NOT add `js-yaml` as a dependency unless it's already present — check `package.json` first.

### Empty State Pattern (History Tab)

```typescript
import {
  EmptyState, EmptyStateIcon, EmptyStateBody,
  EmptyStateHeader,
} from '@patternfly/react-core';
import { CubesIcon } from '@patternfly/react-icons';

function HistoryEmptyState() {
  return (
    <EmptyState variant="sm">
      <EmptyStateHeader
        titleText="No executions yet"
        icon={<EmptyStateIcon icon={CubesIcon} />}
        headingLevel="h3"
      />
      <EmptyStateBody>
        Trigger a planned migration to validate your DR plan
      </EmptyStateBody>
    </EmptyState>
  );
}
```

### Non-Negotiable Constraints

- **PatternFly 5 ONLY** — `TreeView`, `Table`, `DescriptionList`, `CodeBlock`, `EmptyState`, `Label`, `LabelGroup` from `@patternfly/react-core` and `@patternfly/react-table`. No other UI libraries.
- **CSS custom properties only** — `--pf-v5-global--*` tokens. No hardcoded colors/spacing.
- **Console SDK hooks only** — `useDRPlan(name)`, `useDRExecutions(planName)` for data. No direct API calls.
- **No external state libraries** — no Redux, Zustand, or MobX.
- **Imports from `react-router`** — NOT `react-router-dom`.
- **Reuse `ReplicationHealthIndicator`** compact from Story 6.3 — do NOT duplicate.
- **Reuse `ExecutionResultBadge`** from Story 6.3 — do NOT duplicate.
- **Reuse `formatDuration`, `formatRPO`, `formatRelativeTime`** from `src/utils/formatters.ts` — do NOT duplicate.
- **Reuse `getReplicationHealth`** from `src/utils/drPlanUtils.ts` — do NOT duplicate.
- **No separate CSS files** — inline styles with PatternFly tokens.
- **Default export** on `DRPlanDetailPage` only — tab content components use named exports.

### What NOT to Do

- **Do NOT modify the Overview tab** — Story 6.5 already handles the DRLifecycleDiagram, PlanHeader, and TransitionProgressBanner. Leave that tab untouched.
- **Do NOT implement the pre-flight confirmation modal** — Story 7.1 handles that.
- **Do NOT implement the ExecutionGanttChart** — that's Epic 7 (Story 7.2).
- **Do NOT implement toast notifications** — that's Story 7.4.
- **Do NOT implement plan creation UI** — that's post-v1.
- **Do NOT add sorting controls to the ExecutionHistoryTable** — default sort (date descending) is sufficient for v1. Sorting can be added in a polish pass.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT modify DRDashboard, AlertBannerSystem, or toolbar components** — this story only touches `DRPlanDetail/`.
- **Do NOT use SVG or D3** for the WaveCompositionTree — PatternFly TreeView handles the hierarchy.
- **Do NOT add `js-yaml` unless it's already in `package.json`** — use JSON.stringify as fallback.

### Testing Approach

**Mock DRPlan data with waves:**

```typescript
const mockPlanWithWaves: DRPlan = {
  metadata: { name: 'erp-full-stack', uid: '1', creationTimestamp: '2026-04-02T10:00:00Z',
    labels: { 'app.kubernetes.io/part-of': 'erp-system' },
    annotations: { 'soteria.io/description': 'ERP full-stack DR plan' } },
  spec: {
    labelSelector: 'app.kubernetes.io/part-of=erp-system',
    waveLabel: 'soteria.io/wave',
    maxConcurrentFailovers: 4,
    primarySite: 'dc1-prod',
    secondarySite: 'dc2-prod',
  },
  status: {
    phase: 'SteadyState',
    activeSite: 'dc1-prod',
    vmCount: 12,
    waveCount: 3,
    waves: [
      {
        index: 0,
        groups: [
          {
            name: 'drgroup-1',
            namespace: undefined,
            vms: [
              { name: 'erp-db-1', health: 'Healthy', rpo: 8, storageBackend: 'odf-storage' },
              { name: 'erp-db-2', health: 'Healthy', rpo: 8, storageBackend: 'odf-storage' },
              { name: 'erp-db-3', health: 'Degraded', rpo: 45, storageBackend: 'dell-storage' },
            ],
          },
        ],
      },
      {
        index: 1,
        groups: [
          {
            name: 'drgroup-2',
            namespace: 'erp-apps',
            vms: [
              { name: 'erp-app-1', health: 'Healthy', rpo: 8, storageBackend: 'odf-storage' },
              { name: 'erp-app-2', health: 'Healthy', rpo: 8, storageBackend: 'odf-storage' },
              { name: 'erp-app-3', health: 'Healthy', rpo: 8, storageBackend: 'odf-storage' },
              { name: 'erp-app-4', health: 'Healthy', rpo: 12, storageBackend: 'dell-storage' },
            ],
          },
          {
            name: 'drgroup-3',
            namespace: undefined,
            vms: [
              { name: 'erp-app-5', health: 'Healthy', rpo: 12, storageBackend: 'dell-storage' },
            ],
          },
        ],
      },
      {
        index: 2,
        groups: [
          {
            name: 'drgroup-4',
            namespace: undefined,
            vms: [
              { name: 'erp-web-1', health: 'Healthy', rpo: 10, storageBackend: 'odf-storage' },
              { name: 'erp-web-2', health: 'Healthy', rpo: 10, storageBackend: 'odf-storage' },
              { name: 'erp-web-3', health: 'Healthy', rpo: 10, storageBackend: 'odf-storage' },
              { name: 'erp-web-4', health: 'Healthy', rpo: 10, storageBackend: 'odf-storage' },
            ],
          },
        ],
      },
    ],
    conditions: [
      { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s', lastTransitionTime: '2026-04-25T15:00:00Z' },
    ],
  },
};
```

**Mock DRExecution data:**

```typescript
const mockExecutions: DRExecution[] = [
  {
    metadata: { name: 'erp-full-stack-failover-001', uid: 'e1',
      annotations: { 'soteria.io/triggered-by': 'carlos@corp' } },
    spec: { planName: 'erp-full-stack', mode: 'disaster' },
    status: { result: 'PartiallySucceeded', startTime: '2026-03-18T03:14:00Z', completionTime: '2026-03-18T03:36:41Z', rpoSeconds: 47 },
  },
  {
    metadata: { name: 'erp-full-stack-migration-002', uid: 'e2',
      annotations: { 'soteria.io/triggered-by': 'maya@corp' } },
    spec: { planName: 'erp-full-stack', mode: 'planned_migration' },
    status: { result: 'Succeeded', startTime: '2026-04-20T14:32:00Z', completionTime: '2026-04-20T14:49:22Z', rpoSeconds: 0 },
  },
];
```

**WaveCompositionTree tests:**
- Renders 3 wave nodes with labels
- Wave 1 header shows "3 VMs" and "1 Degraded" badge (due to erp-db-3)
- Wave 2 header shows "5 VMs" and "All Healthy"
- Waves are collapsed by default (no per-VM details visible initially)
- After expanding Wave 1, per-VM rows visible with name, storage, health
- Namespace-consistent VMs (erp-app-1..3 in `erp-apps`) show "NS: erp-apps" badge
- Empty waves array renders without error

**ExecutionHistoryTable tests:**
- Renders 2 rows with correct data
- Most recent execution appears first (Apr 20 before Mar 18)
- Mode "disaster" displays as "Disaster", "planned_migration" as "Planned Migration"
- Result column shows correct badge (PartiallySucceeded=warning, Succeeded=success)
- Row contains Link to `/disaster-recovery/executions/erp-full-stack-failover-001`
- Empty state renders with "No executions yet" message when no executions

**PlanConfiguration tests:**
- DescriptionList renders: Name, Label Selector, Wave Label, Max Concurrent Failovers
- Labels from metadata render as PatternFly Label components
- CodeBlock renders plan spec content
- Handles missing optional fields gracefully

**Accessibility:** `jest-axe` via `toHaveNoViolations` on:
- WaveCompositionTree with mock wave data
- ExecutionHistoryTable with data
- ExecutionHistoryTable empty state
- PlanConfiguration

**Build verification:** `yarn build` must succeed.

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   ├── AlertBannerSystem.tsx       # (from 6.4) — unchanged
│   │   ├── DRDashboard.tsx             # (from 6.3) — unchanged
│   │   ├── DRDashboardPage.tsx         # (from 6.2) — unchanged
│   │   ├── DRDashboardToolbar.tsx      # (from 6.3) — unchanged
│   │   └── DRPlanActions.tsx           # (from 6.3) — unchanged
│   ├── DRPlanDetail/
│   │   ├── DRPlanDetailPage.tsx        # MODIFIED — tab placeholders replaced with real components
│   │   ├── DRLifecycleDiagram.tsx      # (from 6.5) — unchanged
│   │   ├── PlanHeader.tsx              # (from 6.5) — unchanged
│   │   ├── TransitionProgressBanner.tsx # (from 6.5) — unchanged
│   │   ├── WaveCompositionTree.tsx     # NEW — wave hierarchy TreeView
│   │   ├── ExecutionHistoryTable.tsx   # NEW — execution history table
│   │   ├── PlanConfiguration.tsx       # NEW — plan metadata + YAML
│   │   └── ReplicationHealthExpanded.tsx # NEW — per-VG health table
│   ├── ExecutionDetail/
│   │   └── ExecutionDetailPage.tsx     # (from 6.2) — unchanged
│   └── shared/
│       ├── DRBreadcrumb.tsx            # (from 6.2) — unchanged
│       ├── ReplicationHealthIndicator.tsx # (from 6.3) — reused in WaveCompositionTree
│       ├── PhaseBadge.tsx              # (from 6.3) — unchanged
│       └── ExecutionResultBadge.tsx    # (from 6.3) — reused in ExecutionHistoryTable
├── hooks/
│   ├── useDRResources.ts              # (from 6.1) — unchanged
│   ├── useDashboardState.ts           # (from 6.2) — unchanged
│   └── useFilterParams.ts             # (from 6.3) — unchanged
├── models/
│   └── types.ts                       # (from 6.1) — may need VMInfo/VolumeGroupHealth types
└── utils/
    ├── formatters.ts                  # (from 6.3) — reused (formatDuration, formatRPO, formatRelativeTime)
    └── drPlanUtils.ts                 # (from 6.3) — reused (getReplicationHealth)
```

**New test files:**
```
console-plugin/tests/
└── components/
    ├── WaveCompositionTree.test.tsx     # NEW
    ├── ExecutionHistoryTable.test.tsx   # NEW
    └── PlanConfiguration.test.tsx       # NEW
```

### Project Structure Notes

- All four new components (`WaveCompositionTree`, `ExecutionHistoryTable`, `PlanConfiguration`, `ReplicationHealthExpanded`) are placed in `src/components/DRPlanDetail/` — they are plan-detail-specific, not shared
- `DRPlanDetailPage.tsx` is the only file modified from Story 6.5 — replacing tab placeholders with real components and adding the `useDRExecutions` hook call
- The `types.ts` file may need additions if `waves[]` sub-types (VMInfo, VolumeGroupHealth) are not yet defined. Check before creating new types — they may already exist from Story 6.1's DRPlan interface
- No new hooks or utilities needed — reuses existing from Stories 6.1 and 6.3
- Story 6.6 will add polished status badges, empty states for the dashboard, and comprehensive accessibility sweeps — do not over-invest in polish here

### References

- [Source: _bmad-output/planning-artifacts/epics.md § Story 6.5b] — Acceptance criteria
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Plan Detail View] — Waves tab TreeView, History tab table, Configuration tab DescriptionList + YAML
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § WaveCompositionTree] — Component anatomy: wave → DRGroup chunk → VM, per-VM columns, aggregate health
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § ReplicationHealthIndicator] — Compact and expanded variants, per-VG health breakdown
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Loading & Empty State Patterns] — "No executions yet" empty state
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Accessibility Considerations] — TreeView ARIA tree role, keyboard navigation
- [Source: _bmad-output/planning-artifacts/epic-6-wireframes.canvas.tsx § WaveTreeWireframe] — Interactive wireframe with DRGroup chunks, per-VM detail, namespace consistency badges
- [Source: _bmad-output/planning-artifacts/epic-6-wireframes.canvas.tsx § PlanDetailWireframe (History Tab)] — Execution history table columns and sample data
- [Source: _bmad-output/planning-artifacts/epic-6-wireframes.canvas.tsx § PlanDetailWireframe (Configuration Tab)] — DescriptionList fields, per-VG health table, CodeBlock for YAML
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 5
- [Source: _bmad-output/planning-artifacts/architecture.md § CRD Status Patterns] — DRExecution result enum, wave/group status model
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK hooks, PatternFly-only, no state libraries
- [Source: _bmad-output/project-context.md § DRPlan 8-phase lifecycle] — Rest-state-only model, EffectivePhase
- [Source: _bmad-output/implementation-artifacts/6-5-plan-detail-shell-overview-tab.md] — DRPlanDetailPage tab shell, placeholder pattern, test mock data
- [Source: _bmad-output/implementation-artifacts/6-3-dr-dashboard-table-toolbar.md] — Composable Table API, ReplicationHealthIndicator, ExecutionResultBadge, formatters, mock patterns
- [Source: _bmad-output/implementation-artifacts/6-1-console-plugin-project-initialization.md] — CRD TypeScript interfaces, useDRResources hooks
- [Source: _bmad-output/implementation-artifacts/6-4-alert-banner-system.md] — PatternFly Alert/EmptyState patterns

### Previous Story Intelligence

**Story 6.5 (Plan Detail Shell & Overview Tab) established:**
- `DRPlanDetailPage.tsx` with four PatternFly Tabs — Overview is fully implemented, Waves/History/Configuration are placeholders
- Tab state managed via `useState<string | number>(0)` with `activeKey` / `onSelect`
- `useDRPlan(name)` and `useDRExecution(activeExecName)` already called in the page — add `useDRExecutions(name)` for History
- PatternFly `Tabs`/`Tab`/`TabTitleText`/`TabContent` import pattern
- Mock data pattern for testing with `mockSteadyStatePlan`

**Story 6.4 (Alert Banner System) established:**
- PatternFly `Alert` inline pattern, `AlertActionLink` for action links
- `React.useMemo` for derived state from plan data
- `jest-axe` accessibility testing pattern

**Story 6.3 (DR Dashboard Table & Toolbar) established:**
- Composable Table API: `Table`, `Thead`, `Tr`, `Th`, `Tbody`, `Td` from `@patternfly/react-table`
- `ReplicationHealthIndicator` compact — icon + health label + RPO in one line
- `ExecutionResultBadge` — Succeeded (green), PartiallySucceeded (yellow), Failed (red)
- `formatDuration(start, end)`, `formatRPO(seconds)`, `formatRelativeTime(date)` in `src/utils/formatters.ts`
- `getReplicationHealth(plan)` in `src/utils/drPlanUtils.ts`
- Mock pattern: `jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({ useK8sWatchResource: jest.fn(() => [...]) }))`
- Mock pattern: `jest.mock('react-router', () => ({ ...jest.requireActual('react-router'), useParams: () => ({ name: 'erp-full-stack' }), Link: ({ to, children }: any) => <a href={to}>{children}</a> }))`

### Git Intelligence

Recent commits (last 5):
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings
- `c7916df` — Mark Story 5.7 as done in sprint status
- `d494cef` — Fix ScyllaDB write contention in DRExecution reconciler
- `f127e6f` — Implement Story 5.7: driver interface simplification & workflow symmetry
- `15e0ab8` — Add Soteria overview presentation

All recent work is Go backend. Stories 6.1–6.5 are ready-for-dev but not yet implemented. This story builds on all five.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (via Cursor)

### Debug Log References

- PF6 TreeView API: `name` is `React.ReactNode` (not string); `title` is compact-variant-only. All custom rendering goes through `name`.
- PF6 EmptyState API: No separate `EmptyStateHeader`/`EmptyStateIcon` components. Use `titleText`, `icon`, `headingLevel` props directly on `EmptyState`.
- PF6 Label color: `"gold"` is not valid; use `"yellow"` for warning/degraded.
- `js-yaml` not in dependencies — used `JSON.stringify(plan.spec, null, 2)` as fallback for CodeBlock.
- Added `labelSelector?: string` to DRPlanSpec and `rpoSeconds?: number` to DRExecutionStatus (both exist on Go CRDs but were missing from TS types).

### Completion Notes List

- **WaveCompositionTree**: PatternFly TreeView with wave → DRGroup chunk → VM hierarchy. Health data cross-referenced from `plan.status.replicationHealth[]` by VG name. Storage backend looked up from preflight data. Aggregate health (worst-case) displayed per wave header. Namespace-consistent VMs show blue "NS:" label.
- **ExecutionHistoryTable**: Compact PatternFly Table with 6 columns. Defensive planName filter. Date-descending sort. Reuses `ExecutionResultBadge` and `formatDuration`/`formatRPO` from Story 6.3. PF6 EmptyState for no-history case.
- **PlanConfiguration**: Horizontal compact DescriptionList with 7 metadata fields. Labels as LabelGroup. Annotations filtered to exclude internal kubernetes.io prefixes. JSON CodeBlock for plan spec.
- **ReplicationHealthExpanded**: Per-VG health table with Health badge, RPO, Last Checked columns. Falls back to overall health indicator when no VG breakdown available.
- **DRPlanDetailPage**: Wired all three tab components. Added `useDRExecutions(name!)` hook call. History tab shows Skeleton while loading.
- **Tests**: 44 new tests across 3 test files. All jest-axe accessibility audits pass with zero violations. Existing 203 tests unaffected (0 regressions). Webpack build verified.

### Change Log

- 2026-04-25: Implemented Story 6.5b — Waves, History & Configuration tabs (4 new components, 3 new test files, 2 modified files)

### File List

**New files:**
- `console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx`
- `console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx`
- `console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx`
- `console-plugin/src/components/DRPlanDetail/ReplicationHealthExpanded.tsx`
- `console-plugin/tests/components/WaveCompositionTree.test.tsx`
- `console-plugin/tests/components/ExecutionHistoryTable.test.tsx`
- `console-plugin/tests/components/PlanConfiguration.test.tsx`

**Modified files:**
- `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` — replaced tab placeholders with real components, added useDRExecutions hook
- `console-plugin/src/models/types.ts` — added `labelSelector` to DRPlanSpec, `rpoSeconds` to DRExecutionStatus
- `console-plugin/tests/components/DRPlanDetailPage.test.tsx` — added useDRExecutions mock, updated placeholder test
