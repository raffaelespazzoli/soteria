# Story 6.4: Alert Banner System

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want persistent alert banners above the dashboard for unprotected plans and degraded replication,
So that critical protection gaps are impossible to miss.

## Acceptance Criteria

1. **AC1 — Danger banner for broken replication:** When one or more DRPlans have broken replication (Error state in the `ReplicationHealthy` condition), a danger Alert banner (PatternFly `Alert`, `variant="danger"`, not dismissible) displays above the dashboard table: "N DR Plans running UNPROTECTED — replication broken" with a direct action link. (UX-DR2)

2. **AC2 — Warning banner for degraded replication:** When one or more DRPlans have degraded replication, a warning Alert banner (`variant="warning"`, not dismissible) displays: "N plans with degraded replication". (UX-DR2)

3. **AC3 — No banners when healthy:** When no plans have broken or degraded replication, no alert banners appear. The absence of banners IS the positive "all healthy" signal.

4. **AC4 — Automatic banner resolution:** When the underlying condition is resolved (e.g., re-protect completes and replication becomes healthy), the banner disappears automatically on the next data refresh via watch update. No manual dismissal is needed for condition-based banners.

5. **AC5 — Banner action link filters table:** Clicking the banner's action link filters the dashboard table to show only the affected plans (Error for danger banner, Degraded for warning banner).

6. **AC6 — Accessibility:** All alert banners pass `jest-axe` accessibility audits including screen reader announcement of alert content. (UX-DR16)

## Tasks / Subtasks

- [x] Task 1: Create AlertBannerSystem component (AC: #1, #2, #3, #4)
  - [x] 1.1 Create `src/components/DRDashboard/AlertBannerSystem.tsx` — accepts DRPlan list, computes alert state, renders 0–2 PatternFly `Alert` components
  - [x] 1.2 Compute broken replication count: plans where `getReplicationHealth(plan).status === 'Error'`
  - [x] 1.3 Compute degraded replication count: plans where `getReplicationHealth(plan).status === 'Degraded'`
  - [x] 1.4 Render danger alert when `errorCount > 0`: `variant="danger"`, `isInline`, no `actionClose` (not dismissible)
  - [x] 1.5 Render warning alert when `degradedCount > 0`: `variant="warning"`, `isInline`, no `actionClose` (not dismissible)
  - [x] 1.6 Render nothing when both counts are zero
  - [x] 1.7 Danger banner appears above warning banner (danger first = highest severity on top)

- [x] Task 2: Implement banner action links (AC: #5)
  - [x] 2.1 Add `actionLinks` prop to danger alert with a link: "View affected plans" that applies `protected=Error` filter
  - [x] 2.2 Add `actionLinks` prop to warning alert with a link: "View affected plans" that applies `protected=Degraded` filter
  - [x] 2.3 Action links call a callback prop `onFilterByHealth(status: string)` that the parent wires to the existing `useFilterParams` from Story 6.3

- [x] Task 3: Integrate into DRDashboardPage (AC: #1, #2, #3, #4)
  - [x] 3.1 Import `AlertBannerSystem` into `DRDashboardPage.tsx`
  - [x] 3.2 Render `<AlertBannerSystem>` above the `<DRDashboard>` table component within the same `PageSection`
  - [x] 3.3 Pass the full plans array from `useDRPlans()` to `AlertBannerSystem`
  - [x] 3.4 Wire `onFilterByHealth` to the dashboard's filter state (via `useFilterParams` or direct state setter from Story 6.3)

- [x] Task 4: Tests (AC: #1–#6)
  - [x] 4.1 Create `tests/components/AlertBannerSystem.test.tsx` — danger banner renders when Error plans exist, count is correct
  - [x] 4.2 Test warning banner renders when Degraded plans exist, count is correct
  - [x] 4.3 Test no banners render when all plans are Healthy or Unknown
  - [x] 4.4 Test both banners render simultaneously: danger above warning
  - [x] 4.5 Test danger banner disappears when Error condition resolves (re-render with updated props)
  - [x] 4.6 Test action link click calls `onFilterByHealth` with correct status
  - [x] 4.7 Run `jest-axe` on `AlertBannerSystem` with danger, warning, and both-visible scenarios — zero violations
  - [x] 4.8 Verify `yarn build` succeeds with all new components

### Review Findings

- [x] [Review][Decision] Banner action resets all active filters — AC5 requires showing the full affected set. Changed `handleFilterByHealth` to use `{ ...EMPTY_FILTERS, protected: [healthStatus] }` instead of spreading existing filters. Decision: reset all filters on banner click.
- [x] [Review][Patch] Added page-level test proving banner click navigates with `?protected=Error` only, clearing other filters [console-plugin/tests/components/DRDashboardPage.test.tsx]

## Dev Notes

### Dependency on Stories 6.1, 6.2, and 6.3

This story depends on all three previous stories being complete:

**From Story 6.1:**
- `src/models/types.ts` — DRPlan TypeScript interface with `status.conditions` array
- `src/hooks/useDRResources.ts` — `useDRPlans()` hook wrapping `useK8sWatchResource`
- PatternFly 5 + Console SDK + Jest + jest-axe + RTL configured

**From Story 6.2:**
- `src/components/DRDashboard/DRDashboardPage.tsx` — page wrapper where alert banners are inserted
- React Router v7 import pattern: `import { ... } from 'react-router'` (NOT `react-router-dom`)

**From Story 6.3:**
- `src/utils/drPlanUtils.ts` — `getReplicationHealth(plan)` utility already implemented — **reuse it, do not reimplement**
- `src/hooks/useFilterParams.ts` — URL filter synchronization hook — **reuse for banner action links**
- `src/components/DRDashboard/DRDashboard.tsx` — the table component this banner system sits above
- `src/components/DRDashboard/DRDashboardToolbar.tsx` — toolbar with filter state that banner links interact with

### PatternFly Alert — Inline Variant API

Use PatternFly's `Alert` component with `isInline` for banner-style alerts embedded in page content. The `Alert` is imported from `@patternfly/react-core`:

```typescript
import { Alert, AlertActionLink } from '@patternfly/react-core';

<Alert
  variant="danger"
  isInline
  title={`${errorCount} DR Plans running UNPROTECTED — replication broken`}
  actionLinks={
    <AlertActionLink onClick={() => onFilterByHealth('Error')}>
      View affected plans
    </AlertActionLink>
  }
/>
```

**Key API details:**
- `variant`: `"danger"` | `"warning"` | `"info"` | `"success"` | `"custom"` — controls color and icon
- `isInline`: renders the alert embedded in content (not as a floating toast)
- `actionClose`: omit entirely to make the alert non-dismissible — do NOT pass `actionClose={undefined}` or `actionClose={null}`
- `actionLinks`: renders action links below the alert message text
- `AlertActionLink`: clickable action rendered below the title — use `onClick` handler, not `href`
- `component`: set to `"h4"` or `"div"` as appropriate — since there's no `children` description, use `"div"` for the title element
- The component includes built-in screen reader support via `aria-live="polite"` on the alert region

### Alert State Computation

The component receives the full plans array and computes banner visibility:

```typescript
interface AlertBannerSystemProps {
  plans: DRPlan[];
  onFilterByHealth: (healthStatus: string) => void;
}

function AlertBannerSystem({ plans, onFilterByHealth }: AlertBannerSystemProps) {
  const errorCount = React.useMemo(
    () => plans.filter(p => getReplicationHealth(p).status === 'Error').length,
    [plans]
  );
  const degradedCount = React.useMemo(
    () => plans.filter(p => getReplicationHealth(p).status === 'Degraded').length,
    [plans]
  );

  return (
    <>
      {errorCount > 0 && (
        <Alert variant="danger" isInline title={...} actionLinks={...} />
      )}
      {degradedCount > 0 && (
        <Alert variant="warning" isInline title={...} actionLinks={...} />
      )}
    </>
  );
}
```

**Reactivity is automatic:** `useDRPlans()` returns a live-watched array. When a plan's `ReplicationHealthy` condition changes (e.g., from Error to True after re-protect), the plans array updates, `useMemo` recomputes, and the banner disappears — no manual refresh or dismissal needed.

### Replication Health Extraction — Reuse from Story 6.3

`getReplicationHealth(plan)` is already implemented in `src/utils/drPlanUtils.ts` from Story 6.3. It extracts health from the `ReplicationHealthy` condition in `status.conditions`:

```typescript
function getReplicationHealth(plan: DRPlan): {
  status: 'Healthy' | 'Degraded' | 'Error' | 'Unknown';
  rpoSeconds: number | null;
}
```

Import and use directly — do NOT duplicate this logic.

### Integration Point — DRDashboardPage Layout

The `AlertBannerSystem` renders between the page header and the table. The `DRDashboardPage.tsx` from Story 6.2 wraps content in `Page` > `PageSection`. Insert the alert system above the `DRDashboard` table:

```typescript
export default function DRDashboardPage() {
  const [plans, loaded, error] = useDRPlans();

  const handleFilterByHealth = (healthStatus: string) => {
    // Wire to useFilterParams or direct filter state
    // Set protected filter to [healthStatus] and clear other protected filters
  };

  return (
    <Page>
      <PageSection>
        <Title headingLevel="h1">Disaster Recovery</Title>
      </PageSection>
      <PageSection>
        <AlertBannerSystem plans={plans ?? []} onFilterByHealth={handleFilterByHealth} />
        <DRDashboard />
      </PageSection>
    </Page>
  );
}
```

**Important:** The `DRDashboardPage` already receives plans from `useDRPlans()`. Story 6.3 may have passed them to `DRDashboard` as props or used the hook inside `DRDashboard` directly. Examine the actual implementation to determine the correct data flow — the alert system needs the same plan data.

### Banner-to-Filter Wiring

When the user clicks "View affected plans" on a banner, the dashboard table should filter to show only affected plans. This connects the `AlertBannerSystem` to Story 6.3's filter infrastructure:

1. The `onFilterByHealth` callback receives the health status string (`'Error'` or `'Degraded'`)
2. It updates the `protected` filter parameter via `useFilterParams` from Story 6.3
3. The URL updates to `?protected=Error` or `?protected=Degraded`
4. The table toolbar reflects the active filter chip
5. The table shows only matching plans

The exact wiring depends on how Story 6.3 exposes filter state. Two likely patterns:

**Pattern A — useFilterParams is called in DRDashboardPage:**
```typescript
const { setFilters, filters } = useFilterParams();
const handleFilterByHealth = (status: string) => {
  setFilters({ ...filters, protected: [status] });
};
```

**Pattern B — Filter state is in DRDashboard component:**
Pass `onFilterByHealth` as a prop to `DRDashboard` which internally applies the filter. In this case, `AlertBannerSystem` needs a ref or callback chain.

Check Story 6.3's implementation to determine which pattern applies.

### Banner Message Format

Follow the UX spec exactly:

| Banner | Variant | Message | Action |
|---|---|---|---|
| Broken replication | `danger` | "N DR Plans running UNPROTECTED — replication broken" | "View affected plans" → filter `protected=Error` |
| Degraded replication | `warning` | "N plans with degraded replication" | "View affected plans" → filter `protected=Degraded` |

Use singular/plural grammar:
- `errorCount === 1`: "1 DR Plan running UNPROTECTED — replication broken"
- `errorCount > 1`: "N DR Plans running UNPROTECTED — replication broken"
- Same pattern for degraded count

### Non-Negotiable Constraints

- **PatternFly 5 ONLY** — `Alert`, `AlertActionLink` from `@patternfly/react-core`. No other UI libraries.
- **CSS custom properties only** — `--pf-v5-global--*` tokens. No hardcoded colors/spacing. Alert colors are handled automatically by the `variant` prop.
- **Console SDK hooks only** — `useK8sWatchResource()` via `useDRPlans()` for data. No direct API calls.
- **No external state libraries** — no Redux, Zustand, or MobX.
- **Imports from `react-router`** — NOT `react-router-dom`.
- **Reuse `getReplicationHealth`** from `src/utils/drPlanUtils.ts` — do NOT duplicate.
- **Reuse `useFilterParams`** from `src/hooks/useFilterParams.ts` — do NOT duplicate.
- **No `actionClose`** on condition-based banners — they are not dismissible.

### What NOT to Do

- **Do NOT create a new utility for health extraction** — `getReplicationHealth` exists in `src/utils/drPlanUtils.ts` from Story 6.3. Import it.
- **Do NOT create a custom filter mechanism** — use Story 6.3's `useFilterParams` hook to set URL-synced filters.
- **Do NOT add toast notifications** — Story 7.4 handles toast notifications for execution events. This story is only persistent inline banners.
- **Do NOT add stale test warnings** — UX-DR2 mentions "N plans not tested in 30+ days" as a future warning. DRExecution history queries are not wired for this yet. Defer to a later story.
- **Do NOT add execution notification banners** — UX-DR2 mentions info-variant dismissible banners for execution events. These are toast notifications (Story 7.4), not inline banners.
- **Do NOT modify the DRDashboard table component** — this story only adds banners above the table. Do not change table columns, sorting, or kebab menu.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT create separate CSS files** — PatternFly handles Alert styling via variant prop and CSS custom properties.

### Testing Approach

**Component tests:** React Testing Library with mock plan data. Create mock plans with different `ReplicationHealthy` conditions:

```typescript
const mockErrorPlan: DRPlan = {
  metadata: { name: 'plan-broken', uid: '1' },
  status: {
    phase: 'SteadyState',
    conditions: [{
      type: 'ReplicationHealthy',
      status: 'False',
      reason: 'Error',
      message: 'Replication broken',
      lastTransitionTime: new Date().toISOString(),
    }],
  },
};

const mockDegradedPlan: DRPlan = {
  metadata: { name: 'plan-degraded', uid: '2' },
  status: {
    phase: 'SteadyState',
    conditions: [{
      type: 'ReplicationHealthy',
      status: 'False',
      reason: 'Degraded',
      message: 'RPO: 120s',
      lastTransitionTime: new Date().toISOString(),
    }],
  },
};

const mockHealthyPlan: DRPlan = {
  metadata: { name: 'plan-healthy', uid: '3' },
  status: {
    phase: 'SteadyState',
    conditions: [{
      type: 'ReplicationHealthy',
      status: 'True',
      reason: 'Healthy',
      message: 'RPO: 12s',
      lastTransitionTime: new Date().toISOString(),
    }],
  },
};
```

**Test scenarios:**
- Danger banner appears with 2 Error plans → title shows "2 DR Plans running UNPROTECTED..."
- Warning banner appears with 1 Degraded plan → title shows "1 plan with degraded replication"
- No banners with all Healthy plans
- Both banners with mix of Error + Degraded plans, danger above warning
- Re-render with resolved conditions → banners disappear
- Action link click calls `onFilterByHealth('Error')` / `onFilterByHealth('Degraded')`

**Accessibility:** `jest-axe` via `toHaveNoViolations` on rendered `AlertBannerSystem` with danger-only, warning-only, and both-visible scenarios.

**Build verification:** `yarn build` must succeed with all new components.

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   ├── AlertBannerSystem.tsx       # NEW — alert banners above table
│   │   ├── DRDashboard.tsx             # (from 6.3) — unchanged
│   │   ├── DRDashboardPage.tsx         # (from 6.2) — MODIFIED to render AlertBannerSystem
│   │   ├── DRDashboardToolbar.tsx      # (from 6.3) — unchanged
│   │   └── DRPlanActions.tsx           # (from 6.3) — unchanged
│   ├── DRPlanDetail/
│   │   └── DRPlanDetailPage.tsx        # (from 6.2) — unchanged
│   ├── ExecutionDetail/
│   │   └── ExecutionDetailPage.tsx     # (from 6.2) — unchanged
│   └── shared/
│       ├── DRBreadcrumb.tsx            # (from 6.2) — unchanged
│       ├── ReplicationHealthIndicator.tsx # (from 6.3) — unchanged
│       ├── PhaseBadge.tsx              # (from 6.3) — unchanged
│       └── ExecutionResultBadge.tsx    # (from 6.3) — unchanged
├── hooks/
│   ├── useDRResources.ts              # (from 6.1) — unchanged
│   ├── useDashboardState.ts           # (from 6.2) — unchanged
│   └── useFilterParams.ts             # (from 6.3) — unchanged
├── models/
│   └── types.ts                       # (from 6.1) — unchanged
└── utils/
    ├── formatters.ts                  # (from 6.3) — unchanged
    └── drPlanUtils.ts                 # (from 6.3) — unchanged (getReplicationHealth reused)
```

**New test files:**
```
console-plugin/tests/
└── components/
    └── AlertBannerSystem.test.tsx      # NEW
```

### Project Structure Notes

- `AlertBannerSystem.tsx` placed in `src/components/DRDashboard/` — it is dashboard-specific, not a shared component
- Only `DRDashboardPage.tsx` is modified (to render the alert system above the table) — minimal surface area
- No new utilities or hooks — reuses `getReplicationHealth` from `drPlanUtils.ts` and `useFilterParams` from Story 6.3
- This is a focused, additive story: 1 new component, 1 modified file, 1 new test file

### References

- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Alert Banner Color Semantics] — danger/warning/info variant mapping, dismissibility rules
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Dashboard Table Design] — "Alert Banners (Above Table)" section, message format
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Feedback Patterns] — persistent alerts for broken/degraded replication, auto-dismiss toasts for info events
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § User Journey 1: Daily Health Check] — "No banners = healthy" design decision
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § User Journey 4: Re-protect After Failover] — danger banner drives re-protect action, banner clears automatically
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Emotional Design Principles] — "Anxiety is a Feature" for unprotected banners
- [Source: _bmad-output/planning-artifacts/epics.md § Story 6.4] — Acceptance criteria and BDD scenarios
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 5
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK hooks, PatternFly-only, no state libraries
- [Source: _bmad-output/project-context.md § DRPlan 8-phase lifecycle] — ReplicationHealthy condition in status.conditions
- [Source: _bmad-output/implementation-artifacts/6-3-dr-dashboard-table-toolbar.md] — getReplicationHealth utility, useFilterParams hook, DRDashboard table component
- [Source: _bmad-output/implementation-artifacts/6-2-console-plugin-navigation-routing.md] — DRDashboardPage layout, Page/PageSection pattern
- [Source: _bmad-output/implementation-artifacts/6-1-console-plugin-project-initialization.md] — CRD TypeScript interfaces, useDRPlans hook
- [Source: PatternFly 5 Alert docs] — isInline, variant, actionLinks, AlertActionLink API

### Previous Story Intelligence

**Story 6.3 (DR Dashboard Table & Toolbar) established:**
- `getReplicationHealth(plan)` in `src/utils/drPlanUtils.ts` — extracts health from `ReplicationHealthy` condition. Returns `{ status: 'Healthy' | 'Degraded' | 'Error' | 'Unknown', rpoSeconds: number | null }`. **Reuse directly.**
- `useFilterParams` in `src/hooks/useFilterParams.ts` — syncs filter state to/from URL search params. Provides `setFilters` to programmatically apply filters. **Reuse for banner action links.**
- PatternFly composable Table API pattern — `Table`, `Thead`, `Tr`, `Th`, `Tbody`, `Td` from `@patternfly/react-table`
- `DRDashboardToolbar.tsx` manages filter state with chips, clear all, and result count
- `DRDashboard.tsx` renders the full table with sorting and filtering
- Mock pattern for `useK8sWatchResource`: `jest.fn(() => [mockPlans, true, null])`
- Mock pattern for `react-router`: `jest.mock('react-router', () => (...))`
- Story 6.3 explicitly noted: "Do NOT build alert banners above the table — Story 6.4 handles the 'N plans UNPROTECTED' danger/warning banners"

**Story 6.2 (Console Plugin Navigation & Routing) established:**
- `DRDashboardPage.tsx` — page wrapper using `Page` > `PageSection` > `Title` from PatternFly
- Default exports for page components (required by `$codeRef` / webpack module federation)
- `useDashboardState.ts` — module-level state for scroll/filter preservation

**Story 6.1 (Console Plugin Project Initialization) established:**
- `types.ts` with DRPlan interface using `K8sResourceCommon` as base, including `status.conditions` array
- `useDRPlans()` hook wrapping `useK8sWatchResource` with `{ group: 'soteria.io', version: 'v1alpha1', kind: 'DRPlan' }` GVK
- Jest + RTL + jest-axe configured

### Git Intelligence

Recent commits (last 5):
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings
- `c7916df` — Mark Story 5.7 as done in sprint status
- `d494cef` — Fix ScyllaDB write contention in DRExecution reconciler
- `f127e6f` — Implement Story 5.7: driver interface simplification & workflow symmetry
- `15e0ab8` — Add Soteria overview presentation

All recent work is Go backend. Stories 6.1–6.3 are ready-for-dev but not yet implemented. This story builds on top of all three.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor Agent)

### Debug Log References

None — clean implementation with no debugging required.

### Completion Notes List

- Created `AlertBannerSystem` component: accepts DRPlan array, uses `useMemo` with `getReplicationHealth()` (reused from Story 6.3) to compute error/degraded counts, renders 0–2 PatternFly `Alert` components (danger above warning, `isInline`, not dismissible)
- Implemented singular/plural grammar for banner messages: "1 DR Plan" vs "N DR Plans" for danger, "1 plan" vs "N plans" for warning
- Added `AlertActionLink` "View affected plans" on each banner, calling `onFilterByHealth` callback with `'Error'` or `'Degraded'`
- Integrated into `DRDashboardPage`: page now calls `useDRPlans()` and `useFilterParams()` to wire alert banners above the dashboard table; `handleFilterByHealth` sets `protected` filter via `setFilters`
- Refactored `DRDashboardPage` from `React.FC` arrow function to regular function declaration (fixes `no-undef` lint for `React` since jsx-runtime transform is used)
- Updated `DRDashboardPage` test with `react-router` and `useK8sWatchResource` mocks needed by new dependencies
- 23 new tests covering: danger banner rendering/count/grammar (4), warning banner rendering/count/grammar (4), no banners when healthy/unknown/empty (4), automatic resolution via rerender (2), both banners simultaneously with ordering (2), action link click callbacks (3), jest-axe accessibility for all 4 scenarios (4)
- All 158 tests pass (135 existing + 23 new), 0 regressions
- Production webpack build succeeds

### Change Log

- 2026-04-25: Implemented Story 6.4 — AlertBannerSystem component, DRDashboardPage integration, 23 tests

### File List

New files:
- console-plugin/src/components/DRDashboard/AlertBannerSystem.tsx
- console-plugin/tests/components/AlertBannerSystem.test.tsx

Modified files:
- console-plugin/src/components/DRDashboard/DRDashboardPage.tsx
- console-plugin/tests/components/DRDashboardPage.test.tsx
