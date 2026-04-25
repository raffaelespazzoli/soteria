# Story 6.3: DR Dashboard Table & Toolbar

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want a sortable, filterable dashboard table showing all DRPlans with status, replication health, and last execution,
So that I can assess DR posture for 500+ plans at a glance.

## Acceptance Criteria

1. **AC1 — Table rendering:** The DR Dashboard page renders a PatternFly Table (composable, compact variant) with columns: Name (link to plan detail), Phase (status badge), Active On (cluster name), Protected (ReplicationHealthIndicator compact — icon + health label + RPO), Last Execution (date + result badge), Actions (kebab menu). Data is fetched via the `useDRPlans()` hook from Story 6.1. (UX-DR1, FR35)

2. **AC2 — Sorting:** Clicking a column header sorts the table by that column. Default sort is by Protected column: Error first, then Degraded, then Unknown, then Healthy — problems surface to the top. (UX-DR1)

3. **AC3 — Toolbar filters:** A toolbar above the table provides: text search filtering by plan name (debounced, 300ms), dropdown multi-select filters for Phase, Active On, Protected, and Last Execution. Filters use additive AND logic. Active filter chips display below the toolbar with individual clear buttons and "Clear all". Result count shows: "Showing N of M plans". (UX-DR15)

4. **AC4 — URL filter persistence:** Active filters are reflected in the URL query parameters (e.g., `?phase=FailedOver&protected=Error`), enabling shareable filtered views. Navigating to a URL with filter parameters pre-populates the toolbar. (UX-DR15)

5. **AC5 — ReplicationHealthIndicator compact:** The Protected column renders a compact inline element per row: icon + health label + "RPO Ns" in a single line. Healthy = green checkmark, Degraded = yellow warning triangle, Error = red error circle, Unknown = gray question mark. Full status is accessible as a single screen-reader string. (UX-DR8)

6. **AC6 — Actions kebab menu:** Each row has a kebab menu. Only valid state transitions appear as menu items (e.g., SteadyState shows Failover and Planned Migration; FailedOver shows Reprotect). Invalid actions are hidden, not disabled. Menu items are stubs that log the action to console — Story 7.1 wires them to the pre-flight modal. (UX-DR19)

7. **AC7 — 500-plan performance:** The table renders and responds to sort, filter, and scroll operations without visible lag at 500 plans. (UX-DR1)

8. **AC8 — Responsive layout:** At 1920px+ all columns are visible with generous spacing. At 1440px the table fits without horizontal scroll. At 1024px (minimum supported) all columns remain visible in compact layout. (UX-DR20)

9. **AC9 — Accessibility:** All status indicators use icon + text label in addition to color (never color alone). `jest-axe` reports zero violations on the dashboard table. Keyboard navigation works: Tab to rows, Enter to navigate to plan detail. (UX-DR16)

## Tasks / Subtasks

- [ ] Task 1: Create DRDashboard table component (AC: #1)
  - [ ] 1.1 Create `src/components/DRDashboard/DRDashboard.tsx` — replace the placeholder from Story 6.1 with a PatternFly `Table` (composable API, compact variant) that renders DRPlan data from `useDRPlans()`
  - [ ] 1.2 Define column definitions: Name, Phase, Active On, Protected, Last Execution, Actions
  - [ ] 1.3 Wire the Name column as a `Link` to `/disaster-recovery/plans/:name`
  - [ ] 1.4 Integrate `DRDashboardPage.tsx` from Story 6.2 to render the `DRDashboard` component

- [ ] Task 2: Create ReplicationHealthIndicator compact component (AC: #5)
  - [ ] 2.1 Create `src/components/shared/ReplicationHealthIndicator.tsx` — compact variant rendering icon + health label + "RPO Ns" inline
  - [ ] 2.2 Map health states to PatternFly icons: `CheckCircleIcon` (Healthy/green), `ExclamationTriangleIcon` (Degraded/yellow), `ExclamationCircleIcon` (Error/red), `QuestionCircleIcon` (Unknown/gray)
  - [ ] 2.3 Use PatternFly CSS custom properties exclusively for colors: `--pf-v5-global--success-color--100`, `--pf-v5-global--warning-color--100`, `--pf-v5-global--danger-color--100`, `--pf-v5-global--disabled-color--100`
  - [ ] 2.4 Add `aria-label` with full status string: "Replication healthy, RPO 12 seconds"

- [ ] Task 3: Create status badge components (AC: #1, #9)
  - [ ] 3.1 Create `src/components/shared/PhaseBadge.tsx` — PatternFly `Label` with DR-specific color mapping: SteadyState/DRedSteadyState = green solid, FailedOver/FailedBack = blue solid, transient phases = blue outlined + spinner icon
  - [ ] 3.2 Create `src/components/shared/ExecutionResultBadge.tsx` — PatternFly `Label` for Succeeded (green), PartiallySucceeded (yellow), Failed (red)
  - [ ] 3.3 Add icon + text on all badges (never color alone)

- [ ] Task 4: Implement sorting (AC: #2)
  - [ ] 4.1 Add `useSortable` hook or implement sort state with `React.useState` for active column + direction
  - [ ] 4.2 Implement custom sort comparator for Protected column: Error (0) > Degraded (1) > Unknown (2) > Healthy (3)
  - [ ] 4.3 Set default sort to Protected column ascending (worst-first)
  - [ ] 4.4 Wire `Th` sortParams for all sortable columns (all except Actions)

- [ ] Task 5: Implement toolbar with filters (AC: #3, #4)
  - [ ] 5.1 Create `src/components/DRDashboard/DRDashboardToolbar.tsx` — PatternFly `Toolbar` + `ToolbarContent` + `ToolbarFilter` + `ToolbarItem`
  - [ ] 5.2 Add text search `TextInput` with 300ms debounce for plan name filtering
  - [ ] 5.3 Add `Select` (multi-select variant) dropdowns for Phase, Active On, Protected, Last Execution
  - [ ] 5.4 Phase filter options: SteadyState, FailedOver, FailingOver, Reprotecting, DRedSteadyState, FailingBack, FailedBack, Restoring
  - [ ] 5.5 Active On filter: dynamically populated from plan data (unique `status.activeSite` values)
  - [ ] 5.6 Protected filter options: Healthy, Degraded, Error, Unknown
  - [ ] 5.7 Last Execution filter options: Succeeded, PartiallySucceeded, Failed, Never
  - [ ] 5.8 Render active filter chips below toolbar with individual clear and "Clear all"
  - [ ] 5.9 Display "Showing N of M plans" count

- [ ] Task 6: URL filter synchronization (AC: #4)
  - [ ] 6.1 Create `src/hooks/useFilterParams.ts` — syncs filter state to/from URL search params using `useLocation` and `useNavigate` from `react-router`
  - [ ] 6.2 On filter change → update URL params (replace, not push — avoid polluting browser history)
  - [ ] 6.3 On mount → parse URL params and pre-populate filters
  - [ ] 6.4 Integrate with `useDashboardState` from Story 6.2 for scroll/filter preservation on navigation

- [ ] Task 7: Actions kebab menu (AC: #6)
  - [ ] 7.1 Create `src/components/DRDashboard/DRPlanActions.tsx` — PatternFly `Dropdown` with `KebabToggle` per row
  - [ ] 7.2 Implement `getValidActions(plan)` utility in `src/utils/drPlanActions.ts` that returns valid transitions based on effective phase (EffectivePhase logic from project-context.md)
  - [ ] 7.3 SteadyState → [Failover, Planned Migration]; FailedOver → [Reprotect]; DRedSteadyState → [Failback]; FailedBack → [Restore]; transient phases → [] (no actions)
  - [ ] 7.4 Menu items are stubs: `console.log('Action:', action, 'Plan:', plan.metadata.name)` — Story 7.1 wires to pre-flight modal

- [ ] Task 8: Utility functions (AC: #1, #2, #5)
  - [ ] 8.1 Create `src/utils/formatters.ts` with: `formatRPO(seconds)` → "RPO 12s", `formatDuration(start, end)` → "2m 34s", `formatRelativeTime(date)` → "5 min ago"
  - [ ] 8.2 Create `src/utils/drPlanUtils.ts` with: `getEffectivePhase(plan)` → derives transient phase from activeExecution/activeExecutionMode, `getReplicationHealth(plan)` → extracts health from conditions, `getLastExecution(executions, planName)` → finds most recent execution

- [ ] Task 9: Tests (AC: #1–#9)
  - [ ] 9.1 Create `tests/components/DRDashboard.test.tsx` — table renders with mock plan data, columns present, plan name links to detail
  - [ ] 9.2 Create `tests/components/ReplicationHealthIndicator.test.tsx` — all 4 health states render correct icon/color/label, accessibility string present
  - [ ] 9.3 Create `tests/components/PhaseBadge.test.tsx` — all 8 phases render correct color variant and icon
  - [ ] 9.4 Create `tests/components/DRDashboardToolbar.test.tsx` — filters render, filter chips appear on selection, clear all works, result count updates
  - [ ] 9.5 Create `tests/components/DRPlanActions.test.tsx` — valid actions appear per phase, no actions for transient phases
  - [ ] 9.6 Create `tests/utils/drPlanUtils.test.ts` — getEffectivePhase, getReplicationHealth, getLastExecution
  - [ ] 9.7 Run `jest-axe` on DRDashboard, ReplicationHealthIndicator, PhaseBadge — zero violations
  - [ ] 9.8 Verify `yarn build` succeeds with all new components

## Dev Notes

### Dependency on Stories 6.1 and 6.2

This story depends on both previous stories being complete:

**From Story 6.1:**
- `src/models/types.ts` — DRPlan, DRExecution, DRGroupStatus TypeScript interfaces
- `src/hooks/useDRResources.ts` — `useDRPlans()`, `useDRExecutions()` hooks wrapping `useK8sWatchResource`
- PatternFly 5 + Console SDK + Jest + jest-axe + RTL configured
- `console-plugin/` fully scaffolded with webpack module federation

**From Story 6.2:**
- `src/components/DRDashboard/DRDashboardPage.tsx` — page wrapper that renders `<DRDashboard />`
- `src/hooks/useDashboardState.ts` — module-level state for scroll/filter preservation
- `src/components/shared/DRBreadcrumb.tsx` — reusable breadcrumb
- Route `/disaster-recovery` → DRDashboardPage
- `exposedModules` in `package.json` for DRDashboardPage

### PatternFly Table — Composable API

Use PatternFly's **composable** Table API (not the deprecated `Table` with `cells`/`rows` props). The composable API uses standard HTML-like `<Table>`, `<Thead>`, `<Tr>`, `<Th>`, `<Tbody>`, `<Td>` components from `@patternfly/react-table`:

```typescript
import { Table, Thead, Tr, Th, Tbody, Td, ThProps } from '@patternfly/react-table';

<Table aria-label="DR Plans" variant="compact">
  <Thead>
    <Tr>
      <Th sort={getSortParams(0)}>Name</Th>
      <Th sort={getSortParams(1)}>Phase</Th>
      {/* ... */}
    </Tr>
  </Thead>
  <Tbody>
    {sortedFilteredPlans.map(plan => (
      <Tr key={plan.metadata.name}>
        <Td><Link to={`/disaster-recovery/plans/${plan.metadata.name}`}>{plan.metadata.name}</Link></Td>
        {/* ... */}
      </Tr>
    ))}
  </Tbody>
</Table>
```

The composable API gives full control over sorting, filtering, and rendering — no magic, no hidden state.

### PatternFly Toolbar Pattern

Use `Toolbar` + `ToolbarContent` + `ToolbarItem` + `ToolbarFilter` from `@patternfly/react-core`. Filter chips are managed by `ToolbarFilter`'s `chips`, `deleteChip`, and `categoryName` props:

```typescript
import {
  Toolbar, ToolbarContent, ToolbarItem, ToolbarFilter,
  SearchInput, Select, SelectOption,
} from '@patternfly/react-core';

<Toolbar clearAllFilters={onClearAll}>
  <ToolbarContent>
    <ToolbarItem>
      <SearchInput value={searchText} onChange={onSearchChange} placeholder="Filter by name..." />
    </ToolbarItem>
    <ToolbarFilter chips={phaseFilters} deleteChip={onDeletePhase} categoryName="Phase">
      <Select variant="checkbox" onSelect={onPhaseSelect} selections={phaseFilters} placeholderText="Phase">
        {PHASE_OPTIONS.map(p => <SelectOption key={p} value={p} />)}
      </Select>
    </ToolbarFilter>
    {/* Active On, Protected, Last Execution filters follow same pattern */}
  </ToolbarContent>
</Toolbar>
```

**Important:** PatternFly 5 may use `MenuToggle` + `Select` (new composable Select) instead of the legacy `Select` component. Check which API the installed PatternFly version provides. The template-pinned version determines which API to use.

### EffectivePhase Derivation (Client-Side)

`DRPlan.status.phase` holds **only rest states** (SteadyState, FailedOver, DRedSteadyState, FailedBack). Transient phases are derived from `activeExecution` + `activeExecutionMode`:

```typescript
type RestPhase = 'SteadyState' | 'FailedOver' | 'DRedSteadyState' | 'FailedBack';
type TransientPhase = 'FailingOver' | 'Reprotecting' | 'FailingBack' | 'Restoring';

function getEffectivePhase(plan: DRPlan): RestPhase | TransientPhase {
  if (!plan.status?.activeExecution) return plan.status?.phase ?? 'SteadyState';
  const mode = plan.status.activeExecutionMode;
  switch (plan.status.phase) {
    case 'SteadyState':
      return mode === 'planned_migration' || mode === 'disaster' ? 'FailingOver' : plan.status.phase;
    case 'FailedOver':
      return mode === 'reprotect' ? 'Reprotecting' : plan.status.phase;
    case 'DRedSteadyState':
      return mode === 'planned_migration' || mode === 'disaster' ? 'FailingBack' : plan.status.phase;
    case 'FailedBack':
      return mode === 'reprotect' ? 'Restoring' : plan.status.phase;
    default:
      return plan.status?.phase ?? 'SteadyState';
  }
}
```

This mirrors the Go `EffectivePhase()` helper from `pkg/engine/`. Place in `src/utils/drPlanUtils.ts`.

### Replication Health Extraction

Replication health is stored in `DRPlan.status.conditions` as a `ReplicationHealthy` condition:

```typescript
function getReplicationHealth(plan: DRPlan): {
  status: 'Healthy' | 'Degraded' | 'Error' | 'Unknown';
  rpoSeconds: number | null;
} {
  const condition = plan.status?.conditions?.find(c => c.type === 'ReplicationHealthy');
  if (!condition) return { status: 'Unknown', rpoSeconds: null };

  const rpoStr = condition.message?.match(/RPO: (\d+)s/)?.[1];
  const rpoSeconds = rpoStr ? parseInt(rpoStr, 10) : null;

  switch (condition.status) {
    case 'True': return { status: 'Healthy', rpoSeconds };
    case 'False':
      return { status: condition.reason === 'Degraded' ? 'Degraded' : 'Error', rpoSeconds };
    default: return { status: 'Unknown', rpoSeconds: null };
  }
}
```

### Protected Column Sort Comparator

The default sort must surface problems to the top. Sort order: Error (0) > Degraded (1) > Unknown (2) > Healthy (3):

```typescript
const HEALTH_SORT_ORDER: Record<string, number> = {
  Error: 0, Degraded: 1, Unknown: 2, Healthy: 3,
};

function compareProtected(a: DRPlan, b: DRPlan): number {
  const healthA = getReplicationHealth(a).status;
  const healthB = getReplicationHealth(b).status;
  return (HEALTH_SORT_ORDER[healthA] ?? 99) - (HEALTH_SORT_ORDER[healthB] ?? 99);
}
```

### Last Execution Resolution

The dashboard needs to show the most recent DRExecution per plan. Use `useDRExecutions()` from Story 6.1 and index by `spec.planName`:

```typescript
function getLastExecution(executions: DRExecution[], planName: string): DRExecution | null {
  return executions
    .filter(e => e.spec?.planName === planName)
    .sort((a, b) => {
      const timeA = new Date(a.status?.startTime ?? 0).getTime();
      const timeB = new Date(b.status?.startTime ?? 0).getTime();
      return timeB - timeA;
    })[0] ?? null;
}
```

### Valid Actions per Phase (Kebab Menu)

Actions map directly from the DR state machine. Only valid transitions from the current rest phase are shown. Transient phases (in-progress execution) show no actions:

| Effective Phase | Available Actions |
|---|---|
| SteadyState | Failover (danger), Planned Migration |
| FailedOver | Reprotect |
| DRedSteadyState | Failback |
| FailedBack | Restore |
| Any transient | (empty — no actions during execution) |

Failover uses `danger` variant in the dropdown item. All others use default. These are stubs for now — Story 7.1 connects them to the pre-flight confirmation modal.

### URL Filter Synchronization

Filters sync to URL search params for shareable filtered views. Use `useLocation` and `useNavigate` from `react-router`:

```typescript
import { useLocation, useNavigate } from 'react-router';

function useFilterParams() {
  const location = useLocation();
  const navigate = useNavigate();
  const params = new URLSearchParams(location.search);

  const setFilters = (filters: Record<string, string[]>) => {
    const newParams = new URLSearchParams();
    Object.entries(filters).forEach(([key, values]) => {
      values.forEach(v => newParams.append(key, v));
    });
    navigate({ search: newParams.toString() }, { replace: true });
  };

  return { params, setFilters };
}
```

Use `replace: true` to avoid polluting browser history on every filter change. Integrate with `useDashboardState` from Story 6.2 to persist filter state across navigation.

### Debounced Search

Use a simple `useDebounce` hook for the text search input. Do NOT install lodash or any debounce library:

```typescript
function useDebounce<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = React.useState(value);
  React.useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
```

### 500-Plan Performance

At 500 plans, the table must remain responsive. Key strategies:
- All filtering and sorting is client-side (data is already in-memory from `useK8sWatchResource`)
- Use `React.useMemo` for sorted/filtered plan list — recompute only when plans, sort, or filters change
- Debounce text search (300ms) to avoid re-rendering on every keystroke
- The composable PatternFly Table renders standard DOM `<table>` — 500 rows of 6 columns is well within browser capability without virtualization
- If performance profiling reveals issues, virtualization can be added later — do NOT prematurely optimize with windowing libraries

### Color Semantics — PatternFly CSS Custom Properties

All colors reference PatternFly tokens. Never hardcode hex values:

| Semantic | Token | Usage |
|---|---|---|
| Healthy / Success | `--pf-v5-global--success-color--100` | Green badges, healthy replication |
| Degraded / Warning | `--pf-v5-global--warning-color--100` | Yellow badges, degraded replication |
| Error / Danger | `--pf-v5-global--danger-color--100` | Red badges, broken replication |
| Unknown / Disabled | `--pf-v5-global--disabled-color--100` | Gray badges, unknown state |
| In-Progress / Info | `--pf-v5-global--info-color--100` | Blue badges, transient phases |

Apply via inline `style={{ color: 'var(--pf-v5-global--success-color--100)' }}` or PatternFly component `status` / `color` props where available.

### Responsive Behavior

PatternFly Table with 6 columns fits comfortably at all target widths:
- **1920px+:** Full width, generous column spacing
- **1440px:** Full width, standard spacing — 6 compact columns easily fit
- **1024px:** Compact layout, all columns visible — RPO text may truncate to "12s"

No responsive hiding logic needed for 6 columns. PatternFly's `Table` with `variant="compact"` handles density. If the Protected column is too wide at 1024px, truncate RPO display (icon + label is sufficient, tooltip shows full detail).

### Non-Negotiable Constraints

- **PatternFly 5 ONLY** — `Table`, `Thead`, `Tr`, `Th`, `Tbody`, `Td` from `@patternfly/react-table`; `Toolbar`, `ToolbarFilter`, `Select`, `Label`, `Alert` from `@patternfly/react-core`. No other UI libraries.
- **CSS custom properties only** — `--pf-v5-global--*` tokens for all colors, spacing, typography. No hardcoded values. Dark mode must work automatically.
- **Console SDK hooks only** — `useK8sWatchResource()` for all data. No direct API calls, no `fetch()`.
- **No external state libraries** — no Redux, Zustand, or MobX. Module-level state (from Story 6.2's `useDashboardState`) or React `useState`/`useMemo` only.
- **Imports from `react-router`** — NOT `react-router-dom` (React Router v7 consolidation).
- **No external utility libraries** — no lodash, no date-fns. Write simple utility functions.
- **No chart libraries** — the dashboard is a table, not a chart.

### What NOT to Do

- **Do NOT build alert banners above the table** — Story 6.4 handles the "N plans UNPROTECTED" danger/warning banners. Leave a placeholder `<div>` or simply omit — 6.4 adds them above the table.
- **Do NOT build the Plan Detail page content** — Story 6.5 builds the tabbed detail view. The Name column link navigates there; the detail page is a placeholder from Story 6.2.
- **Do NOT implement the pre-flight confirmation modal** — Story 7.1 builds the modal. Kebab actions are stubs that log to console.
- **Do NOT implement table row virtualization** — 500 rows of 6 columns renders fine without virtualization. Only add if profiling shows a concrete problem.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT add pagination** — the table shows all plans. `useK8sWatchResource` returns all resources; client-side filtering handles subset display.
- **Do NOT create separate CSS files** — use PatternFly tokens via inline styles or PatternFly component props.

### Testing Approach

**Component tests:** React Testing Library with mock data. Mock `useK8sWatchResource` to return controlled plan/execution data:

```typescript
jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  useK8sWatchResource: jest.fn(() => [mockPlans, true, null]),
}));
```

Mock `react-router` for Link and navigation:
```typescript
jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useLocation: () => ({ search: '', pathname: '/disaster-recovery' }),
  useNavigate: () => jest.fn(),
  Link: ({ to, children }: any) => <a href={to}>{children}</a>,
}));
```

**Utility tests:** Pure function tests for `getEffectivePhase`, `getReplicationHealth`, `getLastExecution`, `getValidActions`, `formatRPO`. Table-driven with edge cases.

**Accessibility:** `jest-axe` via `toHaveNoViolations` on rendered DRDashboard, ReplicationHealthIndicator, PhaseBadge components.

**Build verification:** `yarn build` must succeed with all new components and exports.

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   ├── DRDashboard.tsx               # REWRITTEN — full table with sorting/filtering
│   │   ├── DRDashboardPage.tsx           # (from 6.2)
│   │   ├── DRDashboardToolbar.tsx        # NEW — toolbar with filters
│   │   └── DRPlanActions.tsx             # NEW — kebab menu per row
│   ├── DRPlanDetail/
│   │   └── DRPlanDetailPage.tsx          # (from 6.2)
│   ├── ExecutionDetail/
│   │   └── ExecutionDetailPage.tsx       # (from 6.2)
│   └── shared/
│       ├── DRBreadcrumb.tsx              # (from 6.2)
│       ├── ReplicationHealthIndicator.tsx # NEW — compact variant
│       ├── PhaseBadge.tsx                # NEW — phase status badge
│       └── ExecutionResultBadge.tsx      # NEW — execution result badge
├── hooks/
│   ├── useDRResources.ts                # (from 6.1)
│   ├── useDashboardState.ts             # (from 6.2)
│   └── useFilterParams.ts              # NEW — URL ↔ filter sync
├── models/
│   └── types.ts                         # (from 6.1)
└── utils/
    ├── formatters.ts                    # NEW — RPO, duration, time formatters
    └── drPlanUtils.ts                   # NEW — getEffectivePhase, getReplicationHealth, getValidActions
```

**New test files:**
```
console-plugin/tests/
├── components/
│   ├── DRDashboard.test.tsx              # NEW
│   ├── ReplicationHealthIndicator.test.tsx # NEW
│   ├── PhaseBadge.test.tsx               # NEW
│   ├── DRDashboardToolbar.test.tsx       # NEW
│   └── DRPlanActions.test.tsx            # NEW
└── utils/
    └── drPlanUtils.test.ts              # NEW
```

### Project Structure Notes

- All new files under `console-plugin/src/` — fully independent from Go project
- Shared UI components in `src/components/shared/` — ReplicationHealthIndicator, PhaseBadge, and ExecutionResultBadge are reused by Stories 6.4, 6.5, 6.6
- Utilities in `src/utils/` — `getEffectivePhase`, `getReplicationHealth`, `getValidActions` are reused across dashboard, detail page, and action flows
- `useFilterParams` hook is dashboard-specific but pattern can be reused if other views need URL-synced filters

### References

- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 5, webpack module federation
- [Source: _bmad-output/planning-artifacts/architecture.md § Project Directory Structure] — `console-plugin/` file structure, `DRDashboard/` directory
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Dashboard Table Design] — Column definitions, sort order, toolbar features, alert banners, compact variant
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Search & Filtering Patterns] — Filter types, AND logic, chips, URL persistence, result count
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § ReplicationHealthIndicator] — Compact/expanded variants, states, icons, accessibility
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § DR-Specific Semantic Color Mapping] — Phase, replication, execution color tokens
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Action Hierarchy] — Valid transitions per phase, danger variant for failover
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Responsive Strategy] — 1920px/1440px/1024px breakpoints, desktop-only
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Accessibility Considerations] — Color-independent status, keyboard navigation, screen reader
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Typography System] — Font sizes for dashboard plan name (lg), status text (sm)
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Component Strategy] — DRPlan Table, Table Toolbar, Status Badge, implementation roadmap
- [Source: _bmad-output/planning-artifacts/epics.md § Story 6.3] — Acceptance criteria and BDD scenarios
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK hooks, PatternFly-only, no state libraries, no direct API calls
- [Source: _bmad-output/project-context.md § DRPlan 8-phase lifecycle] — EffectivePhase derivation, rest-state-only phase, ActiveExecution pointer
- [Source: _bmad-output/implementation-artifacts/6-1-console-plugin-project-initialization.md] — CRD TypeScript interfaces, useK8sWatchResource wrappers, template source
- [Source: _bmad-output/implementation-artifacts/6-2-console-plugin-navigation-routing.md] — Page components, DRDashboardPage, useDashboardState, routing, breadcrumb

### Previous Story Intelligence

**Story 6.2 (Console Plugin Navigation & Routing) established:**
- `DRDashboardPage.tsx` as the page wrapper for `/disaster-recovery` — it renders `<DRDashboard />` inside `Page > PageSection`
- `useDashboardState.ts` — module-level state holder for scroll position and filters. This story populates and uses that state
- React Router v7 import pattern: `import { useParams, useNavigate, Link, useLocation } from 'react-router'` (NOT `react-router-dom`)
- Default exports for page components (required by `$codeRef`)
- PatternFly `Breadcrumb` pattern for sub-pages
- Console extension points in `console-extensions.json` for all three routes

**Story 6.1 (Console Plugin Project Initialization) established:**
- `types.ts` with DRPlan, DRExecution, DRGroupStatus interfaces using `K8sResourceCommon` as base
- `useDRResources.ts` with `useDRPlans()`, `useDRPlan(name)`, `useDRExecutions(planName?)`, `useDRExecution(name)` wrappers
- GVK definitions: `{ group: 'soteria.io', version: 'v1alpha1', kind: 'DRPlan' }`
- Jest + RTL + jest-axe configured and passing
- `@openshift-console/dynamic-plugin-sdk` as the sole data-fetching layer

**Epic 5 Retrospective key takeaways for Epic 6:**
- All API dependencies from Epic 5 are satisfied — DRPlan status phase, EffectivePhase, ActiveExecution, ReplicationHealth, unprotected VM count, DRExecution immutable audit records
- The Go backend is stable — no backend changes needed for Epic 6
- 10-AC cap enforced — this story has 9 ACs
- Task checkbox maintenance required during implementation

### Git Intelligence

Recent commits (last 5):
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings
- `c7916df` — Mark Story 5.7 as done in sprint status
- `d494cef` — Fix ScyllaDB write contention in DRExecution reconciler
- `f127e6f` — Implement Story 5.7: driver interface simplification & workflow symmetry
- `15e0ab8` — Add Soteria overview presentation

All recent work is Go backend. Stories 6.1 and 6.2 are ready-for-dev but not yet implemented. This story builds on top of them.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
