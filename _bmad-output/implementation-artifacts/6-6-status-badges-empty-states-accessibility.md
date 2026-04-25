# Story 6.6: Status Badges, Empty States & Accessibility

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want consistent status badges, helpful empty states, and full accessibility support across all DR views,
So that the Console is usable by all operators including those with assistive technology.

## Acceptance Criteria

1. **AC1 — Status badge color semantics (audit & harden):** All DR status badges across dashboard, plan detail, and execution history use PatternFly `Label` components with the correct DR-specific color semantics (UX-DR10, UX-DR18):
   - Phase: SteadyState/DRedSteadyState = green (solid), FailedOver/FailedBack = blue (solid), FailingOver/Reprotecting/FailingBack/Restoring = blue (outlined) + spinner icon
   - Execution result: Succeeded = green, PartiallySucceeded = yellow, Failed = red
   - Replication: Healthy = green, Degraded = yellow, Error = red, Unknown = gray
   All colors use PatternFly CSS custom properties exclusively — no hardcoded hex/rgb values anywhere in badge/indicator components. Dark mode support is automatic via tokens.

2. **AC2 — Dashboard empty state:** When no DRPlans exist on the cluster, the dashboard renders a PatternFly `EmptyState` displaying: icon + "No DR Plans configured" + body text "Create your first DR plan by labeling VMs with `app.kubernetes.io/part-of=<app-name>` and `soteria.io/wave=<number>`" + link to documentation. (UX-DR12)

3. **AC3 — Color-independent status:** Every status indicator across all Console views communicates status via icon + text label in addition to color — never color alone. Screen readers can access the full status as a single readable string (e.g., "erp-full-stack: SteadyState, replication healthy, RPO 12 seconds"). (UX-DR16)

4. **AC4 — Keyboard-accessible failover flow:** The full failover flow is operable entirely via keyboard: Tab to plan row → Enter to open detail → Tab to Failover button → Enter to open modal → Tab to confirmation input → type keyword → Tab to Confirm → Enter. (UX-DR16)

5. **AC5 — jest-axe zero violations on all custom components:** DRLifecycleDiagram, ReplicationHealthIndicator, WaveCompositionTree, PhaseBadge, ExecutionResultBadge, AlertBannerSystem, ExecutionHistoryTable, PlanConfiguration, DashboardEmptyState — all pass `jest-axe` with zero accessibility violations. Keyboard navigation tests confirm arrow key and Tab behavior per component. (UX-DR16)

6. **AC6 — DRLifecycleDiagram accessibility (audit):** Action buttons are reachable via Tab. Phase nodes are readable by screen reader (ARIA role and labels). During transitions, ARIA live region announces progress updates (e.g., "Failover in progress, wave 2 of 3"). (UX-DR16)

7. **AC7 — Screen-share readability:** All text in status indicators and key data elements across all Console views is legible at 720p screen-share resolution with minimum `--pf-v5-global--FontSize--md` (14px). Critical numbers (RPO, elapsed time, VM count) use `--pf-v5-global--FontSize--lg` or larger. (UX-DR17)

## Tasks / Subtasks

- [ ] Task 1: Create DashboardEmptyState component (AC: #2)
  - [ ] 1.1 Create `src/components/DRDashboard/DashboardEmptyState.tsx` — PatternFly `EmptyState` with `CubesIcon` (or `AddCircleOIcon`), title "No DR Plans configured", body "Create your first DR plan by labeling VMs..." + `Button` variant="link" linking to documentation
  - [ ] 1.2 Wire into `DRDashboard.tsx` — when `useDRPlans()` returns an empty array (loaded, no error), render `DashboardEmptyState` instead of the table
  - [ ] 1.3 Ensure empty state is accessible: `EmptyState` provides native screen reader support, title uses heading level `h4`

- [ ] Task 2: Audit PhaseBadge for color semantics compliance (AC: #1, #3)
  - [ ] 2.1 Open `src/components/shared/PhaseBadge.tsx` from Story 6.3
  - [ ] 2.2 Verify/fix color mapping: SteadyState/DRedSteadyState → `color="green"` (Label prop), FailedOver/FailedBack → `color="blue"`, transient phases → `color="blue"` + `variant="outline"` + spinner icon via `icon={<Spinner size="sm" />}`
  - [ ] 2.3 Verify/fix every badge renders icon + text (never color alone): add `CheckCircleIcon` for green rest phases, `InfoCircleIcon` for blue rest phases, `Spinner` for transient
  - [ ] 2.4 Verify no hardcoded color values — all colors from PatternFly Label `color` prop or `--pf-v5-global--*` CSS tokens
  - [ ] 2.5 Verify `aria-label` or visible text provides full phase name for screen readers

- [ ] Task 3: Audit ExecutionResultBadge for color semantics compliance (AC: #1, #3)
  - [ ] 3.1 Open `src/components/shared/ExecutionResultBadge.tsx` from Story 6.3
  - [ ] 3.2 Verify/fix color mapping: Succeeded → `color="green"` + `CheckCircleIcon`, PartiallySucceeded → `color="gold"` + `ExclamationTriangleIcon`, Failed → `color="red"` + `ExclamationCircleIcon`
  - [ ] 3.3 Verify icon + text on all variants — never color alone
  - [ ] 3.4 Verify no hardcoded color values

- [ ] Task 4: Audit ReplicationHealthIndicator for color and accessibility (AC: #1, #3)
  - [ ] 4.1 Open `src/components/shared/ReplicationHealthIndicator.tsx` from Story 6.3
  - [ ] 4.2 Verify/fix compact variant: each health state uses icon + text label + RPO — never color alone
  - [ ] 4.3 Verify/fix screen reader string: `aria-label="Replication healthy, RPO 12 seconds"` (single readable string per AC3)
  - [ ] 4.4 Verify no hardcoded color values — all via `--pf-v5-global--success-color--100`, `--pf-v5-global--warning-color--100`, `--pf-v5-global--danger-color--100`, `--pf-v5-global--disabled-color--100`
  - [ ] 4.5 Open `src/components/DRPlanDetail/ReplicationHealthExpanded.tsx` from Story 6.5b — verify expanded variant also has icon + text + screen reader strings per row

- [ ] Task 5: Audit DRLifecycleDiagram accessibility (AC: #4, #6)
  - [ ] 5.1 Open `src/components/DRPlanDetail/DRLifecycleDiagram.tsx` from Story 6.5
  - [ ] 5.2 Verify/fix diagram container: `role="img"`, `aria-label="DR lifecycle state machine diagram"`
  - [ ] 5.3 Verify/fix each PhaseNode: `role="group"`, `aria-label` reads full state (e.g., "Steady State, current phase, VMs on DC1, replication DC1 to DC2")
  - [ ] 5.4 Verify/fix action buttons: standard PatternFly `Button` — focusable via Tab, labelled with action name
  - [ ] 5.5 Verify/fix `aria-live="polite"` region for transition progress: announces "{action} in progress, wave {n} of {total}"
  - [ ] 5.6 Verify/fix keyboard flow: Tab moves through action buttons in order; Enter activates the focused button
  - [ ] 5.7 Verify/fix PhaseNode opacity changes are purely decorative — screen reader state comes from aria-label

- [ ] Task 6: Audit AlertBannerSystem accessibility (AC: #3, #5)
  - [ ] 6.1 Open `src/components/DRDashboard/AlertBannerSystem.tsx` from Story 6.4
  - [ ] 6.2 Verify/fix alert banners are announced by screen readers (PatternFly `Alert` with `isInline` handles this natively)
  - [ ] 6.3 Verify/fix `AlertActionLink` is keyboard accessible (focusable via Tab, activatable via Enter)

- [ ] Task 7: Audit WaveCompositionTree accessibility (AC: #5)
  - [ ] 7.1 Open `src/components/DRPlanDetail/WaveCompositionTree.tsx` from Story 6.5b
  - [ ] 7.2 Verify/fix TreeView has `aria-label="Wave composition"`
  - [ ] 7.3 Verify/fix each VM leaf node is readable as a single string: "erp-db-1, odf-storage, VM-level consistency, replication healthy, RPO 8 seconds"
  - [ ] 7.4 Verify/fix keyboard navigation: Arrow Up/Down between nodes, Right to expand, Left to collapse

- [ ] Task 8: Audit screen-share readability (AC: #7)
  - [ ] 8.1 Review all custom components for font size compliance: minimum `--pf-v5-global--FontSize--md` (14px) for all text in status indicators
  - [ ] 8.2 Verify RPO, elapsed time, and VM count use `--pf-v5-global--FontSize--lg` or larger where they appear as primary data points (plan header, transition progress banner, execution summary)
  - [ ] 8.3 Fix any components using `--pf-v5-global--FontSize--sm` (12px) for critical status text — upgrade to `--pf-v5-global--FontSize--md`
  - [ ] 8.4 Verify all font sizes use PatternFly tokens — no hardcoded px values

- [ ] Task 9: Full keyboard flow integration test (AC: #4)
  - [ ] 9.1 Create `tests/components/KeyboardAccessibility.test.tsx` — integration test that simulates the full failover keyboard flow across components:
    - Render DRDashboard with a mock plan in SteadyState
    - Tab to the plan row, press Enter → verify navigation intent (Link href)
    - Render DRPlanDetailPage with mock SteadyState plan
    - Tab to Failover button → verify focus on the button
    - Press Enter → verify `onAction` callback called (modal trigger)
    Note: The actual pre-flight modal is Story 7.1. This test validates up to the action trigger.
  - [ ] 9.2 Test keyboard navigation within DRLifecycleDiagram: Tab through action buttons, verify only the available action receives focus
  - [ ] 9.3 Test keyboard navigation within WaveCompositionTree: Arrow keys expand/collapse nodes

- [ ] Task 10: Comprehensive jest-axe test suite (AC: #5, #6)
  - [ ] 10.1 Create `tests/components/Accessibility.test.tsx` — centralized accessibility audit:
    - PhaseBadge: render all 8 phases, `toHaveNoViolations`
    - ExecutionResultBadge: render all 3 results, `toHaveNoViolations`
    - ReplicationHealthIndicator compact: render all 4 health states, `toHaveNoViolations`
    - DashboardEmptyState: render, `toHaveNoViolations`
    - DRDashboard with data: render with mock plans, `toHaveNoViolations`
    - DRDashboard empty: render with no plans, `toHaveNoViolations`
    - AlertBannerSystem with danger banner: render, `toHaveNoViolations`
    - AlertBannerSystem with warning banner: render, `toHaveNoViolations`
    - DRLifecycleDiagram in rest state: render SteadyState, `toHaveNoViolations`
    - DRLifecycleDiagram in transient state: render FailingOver, `toHaveNoViolations`
    - WaveCompositionTree: render with mock waves, `toHaveNoViolations`
    - ExecutionHistoryTable with data: render, `toHaveNoViolations`
    - ExecutionHistoryTable empty: render, `toHaveNoViolations`
    - PlanConfiguration: render, `toHaveNoViolations`
    - ReplicationHealthExpanded: render, `toHaveNoViolations`
  - [ ] 10.2 Verify all components pass — fix any violations found
  - [ ] 10.3 Verify `yarn build` succeeds after all changes

## Dev Notes

### This Is an Audit & Polish Story

Story 6.6 is NOT building new features from scratch. Stories 6.1–6.5b create all the components. Story 6.6 performs a systematic accessibility and consistency audit across every component, creating only the `DashboardEmptyState` as new, and then writing a comprehensive test suite that catches any gaps.

**Workflow:**
1. Create the single new component (`DashboardEmptyState`)
2. Systematically audit each existing component for color compliance, screen reader support, keyboard navigation, and font sizing
3. Fix any issues found during the audit (in-place edits to existing files)
4. Write comprehensive accessibility and keyboard tests
5. Run full `jest-axe` suite and fix any violations

### Dependency on Stories 6.1–6.5b

**All six prior stories must be implemented before this story.** Story 6.6 audits and hardens every component they created.

**From Story 6.1:**
- `src/models/types.ts` — DRPlan, DRExecution TypeScript interfaces
- `src/hooks/useDRResources.ts` — `useDRPlans()`, `useDRPlan(name)`, `useDRExecution(name)`, `useDRExecutions(planName?)` hooks
- Jest + jest-axe + RTL configured

**From Story 6.2:**
- React Router v7 import: `import { useParams, Link } from 'react-router'` (NOT `react-router-dom`)
- `DRBreadcrumb` shared component
- Default exports on page components

**From Story 6.3:**
- `src/components/shared/PhaseBadge.tsx` — phase status badge. **Audit target.**
- `src/components/shared/ExecutionResultBadge.tsx` — execution result badge. **Audit target.**
- `src/components/shared/ReplicationHealthIndicator.tsx` — compact health indicator. **Audit target.**
- `src/utils/drPlanUtils.ts` — `getEffectivePhase(plan)`, `getReplicationHealth(plan)`, `getValidActions(plan)`
- `src/utils/formatters.ts` — `formatRPO(seconds)`, `formatDuration(start, end)`, `formatRelativeTime(date)`
- `src/components/DRDashboard/DRDashboard.tsx` — dashboard table. **Audit target + empty state integration point.**
- `src/components/DRDashboard/DRDashboardToolbar.tsx` — toolbar with filters
- `src/components/DRDashboard/DRPlanActions.tsx` — kebab menu

**From Story 6.4:**
- `src/components/DRDashboard/AlertBannerSystem.tsx` — danger/warning banners. **Audit target.**

**From Story 6.5:**
- `src/components/DRPlanDetail/DRPlanDetailPage.tsx` — tab shell + Overview content
- `src/components/DRPlanDetail/DRLifecycleDiagram.tsx` — lifecycle state machine. **Audit target (most complex accessibility).**
- `src/components/DRPlanDetail/PlanHeader.tsx` — plan name, VM/wave count, active cluster
- `src/components/DRPlanDetail/TransitionProgressBanner.tsx` — in-progress alert with ARIA live region

**From Story 6.5b:**
- `src/components/DRPlanDetail/WaveCompositionTree.tsx` — TreeView hierarchy. **Audit target.**
- `src/components/DRPlanDetail/ExecutionHistoryTable.tsx` — execution history table. **Audit target.**
- `src/components/DRPlanDetail/PlanConfiguration.tsx` — plan metadata. **Audit target.**
- `src/components/DRPlanDetail/ReplicationHealthExpanded.tsx` — per-VG health table. **Audit target.**

### DashboardEmptyState — New Component

The only new component in this story. Rendered by `DRDashboard.tsx` when no DRPlans are loaded.

```typescript
import {
  EmptyState, EmptyStateBody, EmptyStateHeader,
  EmptyStateIcon, Button,
} from '@patternfly/react-core';
import { CubesIcon } from '@patternfly/react-icons';

export function DashboardEmptyState() {
  return (
    <EmptyState>
      <EmptyStateHeader
        titleText="No DR Plans configured"
        icon={<EmptyStateIcon icon={CubesIcon} />}
        headingLevel="h4"
      />
      <EmptyStateBody>
        Create your first DR plan by labeling VMs with{' '}
        <code>app.kubernetes.io/part-of=&lt;app-name&gt;</code> and{' '}
        <code>soteria.io/wave=&lt;number&gt;</code>.
        The orchestrator auto-discovers labeled VMs and forms protection waves.
      </EmptyStateBody>
      <Button
        variant="link"
        component="a"
        href="https://soteria.io/docs/getting-started"
        target="_blank"
        rel="noopener noreferrer"
      >
        View documentation
      </Button>
    </EmptyState>
  );
}
```

**Integration into DRDashboard.tsx:**

```typescript
export function DRDashboard() {
  const [plans, loaded, error] = useDRPlans();

  if (!loaded) return <Skeleton />;
  if (error) return <Alert variant="danger" title="Failed to load DR plans">{error.message}</Alert>;
  if (plans.length === 0) return <DashboardEmptyState />;

  return (
    <>
      <AlertBannerSystem plans={plans} />
      <DRDashboardToolbar /* ... */ />
      {/* Table */}
    </>
  );
}
```

### PatternFly Label Color Props

PatternFly `Label` component accepts a `color` prop with semantic values that map to the design system tokens. This is the correct way to set badge colors (NOT inline styles):

```typescript
// PhaseBadge color mapping
const PHASE_LABEL_CONFIG: Record<string, { color: LabelProps['color']; icon: ReactElement; variant?: 'outline' }> = {
  SteadyState:      { color: 'green', icon: <CheckCircleIcon /> },
  DRedSteadyState:  { color: 'green', icon: <CheckCircleIcon /> },
  FailedOver:       { color: 'blue',  icon: <InfoCircleIcon /> },
  FailedBack:       { color: 'blue',  icon: <InfoCircleIcon /> },
  FailingOver:      { color: 'blue',  icon: <Spinner size="sm" />, variant: 'outline' },
  Reprotecting:     { color: 'blue',  icon: <Spinner size="sm" />, variant: 'outline' },
  FailingBack:      { color: 'blue',  icon: <Spinner size="sm" />, variant: 'outline' },
  Restoring:        { color: 'blue',  icon: <Spinner size="sm" />, variant: 'outline' },
};

// ExecutionResultBadge color mapping
const RESULT_LABEL_CONFIG: Record<string, { color: LabelProps['color']; icon: ReactElement }> = {
  Succeeded:            { color: 'green', icon: <CheckCircleIcon /> },
  PartiallySucceeded:   { color: 'gold',  icon: <ExclamationTriangleIcon /> },
  Failed:               { color: 'red',   icon: <ExclamationCircleIcon /> },
};

// ReplicationHealthIndicator color mapping (for inline styles using tokens)
const HEALTH_COLOR_MAP: Record<string, string> = {
  Healthy:  'var(--pf-v5-global--success-color--100)',
  Degraded: 'var(--pf-v5-global--warning-color--100)',
  Error:    'var(--pf-v5-global--danger-color--100)',
  Unknown:  'var(--pf-v5-global--disabled-color--100)',
};
```

**Key rule:** PatternFly `Label`'s `color` prop handles dark mode automatically. For inline-styled components (like `ReplicationHealthIndicator`), use `--pf-v5-global--*` CSS custom properties — never hardcoded hex values.

### ARIA Patterns for Custom Components

**DRLifecycleDiagram:**
```typescript
// Container
<div role="img" aria-label="DR lifecycle state machine diagram">
  {/* Phase nodes */}
  <div role="group" aria-label="Steady State, current phase, VMs on DC1, replication DC1 to DC2">
    {/* node content */}
  </div>
  {/* ARIA live region for transitions */}
  <div aria-live="polite" aria-atomic="true">
    {isInTransition && `${transitionAction} in progress, wave ${completedWaves} of ${totalWaves}`}
  </div>
</div>
```

**WaveCompositionTree:**
PatternFly `TreeView` provides `role="tree"` and `role="treeitem"` automatically. Each VM leaf needs an accessible name:
```typescript
// TreeViewDataItem for a VM
{
  name: 'erp-db-1, odf-storage, VM-level consistency, replication healthy, RPO 8 seconds',
  title: <VMNodeContent vm={vm} namespace={group.namespace} />,
  id: `vm-${vm.name}`,
}
```
The `name` property is used by the TreeView for the accessible text. Set it to the full status string even though `title` renders the visual component.

**ReplicationHealthIndicator:**
```typescript
<span aria-label={`Replication ${health}, RPO ${formatRPO(rpo)}`}>
  <StatusIcon health={health} />
  <span>{healthLabel}</span>
  <span>{formatRPO(rpo)}</span>
</span>
```

### Keyboard Flow Validation

The failover keyboard flow (AC4) traverses multiple components:

| Step | Component | Key | Expected |
|------|-----------|-----|----------|
| 1 | DRDashboard table | Tab | Focus moves to first plan row link |
| 2 | DRDashboard table | Enter | Navigation to plan detail (`/disaster-recovery/plans/:name`) |
| 3 | DRPlanDetailPage | Tab | Focus moves through tabs, then to diagram action buttons |
| 4 | DRLifecycleDiagram | Tab | Focus on the available action button (e.g., Failover) |
| 5 | DRLifecycleDiagram | Enter | `onAction('Failover', plan)` called — triggers pre-flight modal (Story 7.1) |

Testing note: Since components live on separate pages, the keyboard flow test validates each component's keyboard behavior independently:
- DRDashboard: table row focusable, Enter triggers navigation
- DRLifecycleDiagram: action buttons focusable via Tab, Enter triggers callback
- WaveCompositionTree: arrow keys navigate tree nodes

### Screen-Share Readability Checklist

| Component | Element | Minimum Font | Fix If... |
|-----------|---------|-------------|-----------|
| PlanHeader | Plan name | `--pf-v5-global--FontSize--lg` | Using `--sm` or hardcoded |
| PlanHeader | VM count, wave count | `--pf-v5-global--FontSize--md` | Using `--sm` |
| TransitionProgressBanner | Wave progress, elapsed time | `--pf-v5-global--FontSize--md` | Using `--sm` |
| DRLifecycleDiagram | Phase labels | `--pf-v5-global--FontSize--md` | Using `--sm` |
| DRLifecycleDiagram | Action button text | `--pf-v5-global--FontSize--md` | Button handles this natively |
| ReplicationHealthIndicator | Health label, RPO | `--pf-v5-global--FontSize--md` | Using `--sm` for RPO |
| ExecutionHistoryTable | Date, Mode, Duration | `--pf-v5-global--FontSize--md` | Table compact uses `--sm` by default — acceptable for data tables |
| WaveCompositionTree | VM name, storage, RPO | `--pf-v5-global--FontSize--md` | Using `--sm` |
| DashboardEmptyState | Title, body | `--pf-v5-global--FontSize--lg` / `--md` | EmptyState handles this natively |

PatternFly compact `Table` uses `--sm` (12px) for data — this is acceptable for dense data tables at 720p. The audit focuses on status indicators and key data elements, not table body text.

### Non-Negotiable Constraints

- **PatternFly 5 ONLY** — all components use `@patternfly/react-core`, `@patternfly/react-table`, `@patternfly/react-icons`. No other UI libraries.
- **CSS custom properties only** — `--pf-v5-global--*` tokens. No hardcoded colors, spacing, or font sizes.
- **No separate CSS files** — inline styles with PatternFly tokens. Use PatternFly `Label` `color` prop where available.
- **Console SDK hooks only** — `useDRPlans()`, `useDRPlan(name)`, `useDRExecution(name)` for data. No direct API calls.
- **Imports from `react-router`** — NOT `react-router-dom`.
- **Do NOT duplicate existing components** — this story audits and fixes existing components, it does not recreate them.
- **No external state libraries** — no Redux, Zustand, or MobX.

### What NOT to Do

- **Do NOT rewrite PhaseBadge, ExecutionResultBadge, or ReplicationHealthIndicator from scratch** — audit and fix in place. They exist from Story 6.3.
- **Do NOT rewrite DRLifecycleDiagram or WaveCompositionTree** — audit and fix accessibility issues in place. They exist from Stories 6.5 and 6.5b.
- **Do NOT implement the pre-flight confirmation modal** — Story 7.1 handles that. The keyboard flow test validates up to the action trigger.
- **Do NOT implement the ExecutionGanttChart** — that's Epic 7 (Story 7.2).
- **Do NOT implement toast notifications** — that's Story 7.4.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT add any new data hooks or API calls** — this story uses existing hooks from Story 6.1.
- **Do NOT modify the DRDashboardToolbar or filter logic** — focus on empty state, badges, and accessibility.
- **Do NOT add `jest-axe` as a dependency** — it's already configured from Story 6.1.

### Testing Approach

**DashboardEmptyState tests:**

```typescript
import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { DashboardEmptyState } from '../../src/components/DRDashboard/DashboardEmptyState';

expect.extend(toHaveNoViolations);

describe('DashboardEmptyState', () => {
  it('renders empty state with title and guidance', () => {
    render(<DashboardEmptyState />);
    expect(screen.getByText('No DR Plans configured')).toBeInTheDocument();
    expect(screen.getByText(/Create your first DR plan/)).toBeInTheDocument();
    expect(screen.getByText('View documentation')).toBeInTheDocument();
  });

  it('documentation link opens in new tab', () => {
    render(<DashboardEmptyState />);
    const link = screen.getByText('View documentation');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  it('passes jest-axe', async () => {
    const { container } = render(<DashboardEmptyState />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
```

**DRDashboard empty state integration test:**

```typescript
jest.mock('../../src/hooks/useDRResources', () => ({
  useDRPlans: jest.fn(() => [[], true, null]),
}));

it('renders DashboardEmptyState when no plans exist', () => {
  render(<DRDashboard />);
  expect(screen.getByText('No DR Plans configured')).toBeInTheDocument();
  expect(screen.queryByRole('table')).not.toBeInTheDocument();
});
```

**PhaseBadge color compliance test:**

```typescript
describe('PhaseBadge color compliance', () => {
  const greenPhases = ['SteadyState', 'DRedSteadyState'];
  const blueRestPhases = ['FailedOver', 'FailedBack'];
  const transientPhases = ['FailingOver', 'Reprotecting', 'FailingBack', 'Restoring'];

  greenPhases.forEach(phase => {
    it(`${phase} renders green solid Label with icon`, () => {
      const { container } = render(<PhaseBadge phase={phase} />);
      const label = container.querySelector('.pf-v5-c-label');
      expect(label).toHaveClass('pf-m-green');
      expect(label).not.toHaveClass('pf-m-outline');
      expect(screen.getByText(phase.replace(/([A-Z])/g, ' $1').trim())).toBeInTheDocument();
    });
  });

  blueRestPhases.forEach(phase => {
    it(`${phase} renders blue solid Label with icon`, () => {
      const { container } = render(<PhaseBadge phase={phase} />);
      const label = container.querySelector('.pf-v5-c-label');
      expect(label).toHaveClass('pf-m-blue');
      expect(label).not.toHaveClass('pf-m-outline');
    });
  });

  transientPhases.forEach(phase => {
    it(`${phase} renders blue outlined Label with spinner`, () => {
      const { container } = render(<PhaseBadge phase={phase} />);
      const label = container.querySelector('.pf-v5-c-label');
      expect(label).toHaveClass('pf-m-blue');
      expect(label).toHaveClass('pf-m-outline');
      expect(container.querySelector('.pf-v5-c-spinner')).toBeInTheDocument();
    });
  });
});
```

**Centralized accessibility audit:**

```typescript
describe('Accessibility audit — all custom components', () => {
  it('PhaseBadge passes jest-axe for all phases', async () => {
    for (const phase of ALL_PHASES) {
      const { container } = render(<PhaseBadge phase={phase} />);
      expect(await axe(container)).toHaveNoViolations();
    }
  });

  it('ExecutionResultBadge passes jest-axe for all results', async () => {
    for (const result of ['Succeeded', 'PartiallySucceeded', 'Failed']) {
      const { container } = render(<ExecutionResultBadge result={result} />);
      expect(await axe(container)).toHaveNoViolations();
    }
  });

  it('ReplicationHealthIndicator passes jest-axe for all health states', async () => {
    for (const health of ['Healthy', 'Degraded', 'Error', 'Unknown']) {
      const { container } = render(<ReplicationHealthIndicator health={health} rpo={12} compact />);
      expect(await axe(container)).toHaveNoViolations();
    }
  });

  it('DRDashboard with plans passes jest-axe', async () => {
    // Mock useDRPlans to return mock data
    const { container } = render(<DRDashboard />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('DRDashboard empty passes jest-axe', async () => {
    // Mock useDRPlans to return empty
    const { container } = render(<DRDashboard />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('DRLifecycleDiagram rest state passes jest-axe', async () => {
    const { container } = render(
      <DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={jest.fn()} />
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('DRLifecycleDiagram transient state passes jest-axe', async () => {
    const { container } = render(
      <DRLifecycleDiagram plan={mockFailingOverPlan} onAction={jest.fn()} />
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('WaveCompositionTree passes jest-axe', async () => {
    const { container } = render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('ExecutionHistoryTable with data passes jest-axe', async () => {
    const { container } = render(
      <ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('ExecutionHistoryTable empty passes jest-axe', async () => {
    const { container } = render(
      <ExecutionHistoryTable executions={[]} planName="erp-full-stack" />
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('PlanConfiguration passes jest-axe', async () => {
    const { container } = render(<PlanConfiguration plan={mockPlanWithWaves} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('AlertBannerSystem with danger banner passes jest-axe', async () => {
    const { container } = render(<AlertBannerSystem plans={[mockBrokenPlan]} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
```

**Keyboard navigation tests:**

```typescript
describe('Keyboard navigation', () => {
  it('DRLifecycleDiagram: Tab reaches Failover button from SteadyState', () => {
    render(<DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={jest.fn()} />);
    const failoverButton = screen.getByRole('button', { name: /failover/i });
    failoverButton.focus();
    expect(failoverButton).toHaveFocus();
  });

  it('DRLifecycleDiagram: Enter on action button triggers onAction', async () => {
    const onAction = jest.fn();
    render(<DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={onAction} />);
    const failoverButton = screen.getByRole('button', { name: /failover/i });
    fireEvent.keyDown(failoverButton, { key: 'Enter' });
    expect(onAction).toHaveBeenCalledWith('Failover', expect.objectContaining({ metadata: expect.objectContaining({ name: 'erp-full-stack' }) }));
  });

  it('DRLifecycleDiagram: no action buttons during transient phase', () => {
    render(<DRLifecycleDiagram plan={mockFailingOverPlan} onAction={jest.fn()} />);
    expect(screen.queryByRole('button', { name: /failover|reprotect|failback|restore/i })).not.toBeInTheDocument();
  });
});
```

**Mock data:** Reuse mock patterns from Stories 6.3, 6.5, and 6.5b. Key mocks needed:

```typescript
const mockSteadyStatePlan: DRPlan = {
  metadata: { name: 'erp-full-stack', uid: '1' },
  spec: { labelSelector: 'app.kubernetes.io/part-of=erp-system', waveLabel: 'soteria.io/wave', maxConcurrentFailovers: 4 },
  status: { phase: 'SteadyState', activeSite: 'dc1-prod', vmCount: 12, waveCount: 3,
    conditions: [{ type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s', lastTransitionTime: new Date().toISOString() }] },
};

const mockFailingOverPlan: DRPlan = {
  ...mockSteadyStatePlan,
  status: { ...mockSteadyStatePlan.status, activeExecution: 'erp-full-stack-failover-001', activeExecutionMode: 'disaster' },
};

const mockBrokenPlan: DRPlan = {
  ...mockSteadyStatePlan,
  status: { ...mockSteadyStatePlan.status,
    conditions: [{ type: 'ReplicationHealthy', status: 'False', reason: 'Error', message: 'Replication broken', lastTransitionTime: new Date().toISOString() }] },
};
```

**Build verification:** `yarn build` must succeed after all changes.

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   ├── AlertBannerSystem.tsx       # (from 6.4) — AUDITED for accessibility
│   │   ├── DashboardEmptyState.tsx     # NEW — empty state for no plans
│   │   ├── DRDashboard.tsx             # (from 6.3) — MODIFIED to render empty state
│   │   ├── DRDashboardPage.tsx         # (from 6.2) — unchanged
│   │   ├── DRDashboardToolbar.tsx      # (from 6.3) — unchanged
│   │   └── DRPlanActions.tsx           # (from 6.3) — unchanged
│   ├── DRPlanDetail/
│   │   ├── DRPlanDetailPage.tsx        # (from 6.5/6.5b) — unchanged
│   │   ├── DRLifecycleDiagram.tsx      # (from 6.5) — AUDITED for ARIA, keyboard, readability
│   │   ├── PlanHeader.tsx              # (from 6.5) — AUDITED for font sizes
│   │   ├── TransitionProgressBanner.tsx # (from 6.5) — AUDITED for ARIA live region
│   │   ├── WaveCompositionTree.tsx     # (from 6.5b) — AUDITED for accessible names, keyboard nav
│   │   ├── ExecutionHistoryTable.tsx   # (from 6.5b) — AUDITED for accessibility
│   │   ├── PlanConfiguration.tsx       # (from 6.5b) — AUDITED for accessibility
│   │   └── ReplicationHealthExpanded.tsx # (from 6.5b) — AUDITED for icon+text, screen reader
│   ├── ExecutionDetail/
│   │   └── ExecutionDetailPage.tsx     # (from 6.2) — unchanged
│   └── shared/
│       ├── DRBreadcrumb.tsx            # (from 6.2) — unchanged
│       ├── ReplicationHealthIndicator.tsx # (from 6.3) — AUDITED for color tokens, aria-label
│       ├── PhaseBadge.tsx              # (from 6.3) — AUDITED for color props, icon+text
│       └── ExecutionResultBadge.tsx    # (from 6.3) — AUDITED for color props, icon+text
├── hooks/
│   ├── useDRResources.ts              # (from 6.1) — unchanged
│   ├── useDashboardState.ts           # (from 6.2) — unchanged
│   └── useFilterParams.ts             # (from 6.3) — unchanged
├── models/
│   └── types.ts                       # (from 6.1) — unchanged
└── utils/
    ├── formatters.ts                  # (from 6.3) — unchanged
    └── drPlanUtils.ts                 # (from 6.3) — unchanged
```

**New test files:**
```
console-plugin/tests/
└── components/
    ├── DashboardEmptyState.test.tsx     # NEW — empty state component tests
    ├── Accessibility.test.tsx           # NEW — centralized jest-axe audit (all components)
    └── KeyboardAccessibility.test.tsx   # NEW — keyboard navigation flow tests
```

### Project Structure Notes

- `DashboardEmptyState.tsx` is the only new source file — placed in `DRDashboard/` alongside the dashboard
- All other changes are in-place audits of existing files created by Stories 6.1–6.5b
- Three new test files consolidate the accessibility and keyboard audits
- No new hooks, utilities, or shared components needed
- The audit may produce small fixes (e.g., adding missing `aria-label`, changing `color` prop, upgrading font size tokens) across many files — these are minor edits, not rewrites

### References

- [Source: _bmad-output/planning-artifacts/epics.md § Story 6.6] — Acceptance criteria
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § DR-Specific Semantic Color Mapping] — Phase, replication, execution color tokens
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Accessibility Considerations] — Keyboard navigation, screen reader, axe-core, ARIA live regions
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Typography System] — Font sizes, screen-share readability rule, minimum 14px for status indicators
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Loading & Empty State Patterns] — "No DR Plans configured" empty state, guidance action
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Color System] — PatternFly token usage, dark mode via tokens, no hardcoded colors
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Responsive Strategy] — 720p screen-share readability, minimum 14px
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § ReplicationHealthIndicator] — Compact/expanded variants, screen reader strings, color-independent status
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § DRLifecycleDiagram] — ARIA roles, live regions, keyboard navigation, phase node accessibility
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § WaveCompositionTree] — ARIA tree role, keyboard expand/collapse, per-VM readable strings
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — PatternFly 5, Console SDK hooks, dark mode via tokens
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK hooks, PatternFly-only, no state libraries, CSS custom properties only
- [Source: _bmad-output/implementation-artifacts/6-3-dr-dashboard-table-toolbar.md] — PhaseBadge, ExecutionResultBadge, ReplicationHealthIndicator creation, mock patterns
- [Source: _bmad-output/implementation-artifacts/6-4-alert-banner-system.md] — AlertBannerSystem, PatternFly Alert accessibility
- [Source: _bmad-output/implementation-artifacts/6-5-plan-detail-shell-overview-tab.md] — DRLifecycleDiagram, TransitionProgressBanner, ARIA live region, keyboard flow, mock data
- [Source: _bmad-output/implementation-artifacts/6-5b-waves-history-configuration-tabs.md] — WaveCompositionTree, ExecutionHistoryTable, PlanConfiguration, jest-axe patterns, mock data
- [Source: _bmad-output/implementation-artifacts/6-1-console-plugin-project-initialization.md] — jest-axe setup, CRD interfaces, useDRResources hooks

### Previous Story Intelligence

**Story 6.5b (Waves, History & Configuration Tabs) established:**
- TreeView accessible name via `TreeViewDataItem.name` — set to full status string for screen readers
- `jest-axe` pattern: `const { container } = render(<Component />); expect(await axe(container)).toHaveNoViolations()`
- Mock data patterns for DRPlan with waves, DRExecution records
- PatternFly `EmptyState` pattern for empty history tab

**Story 6.5 (Plan Detail Shell & Overview Tab) established:**
- DRLifecycleDiagram with `role="img"`, `aria-label`, `aria-live="polite"` region
- Action buttons as standard PatternFly `Button` — Tab-focusable by default
- PhaseNode opacity for visual-only distinction — screen reader uses aria-label
- `onAction` callback pattern for action triggers

**Story 6.4 (Alert Banner System) established:**
- PatternFly `Alert` with `isInline` — screen reader announces automatically
- `AlertActionLink` is keyboard accessible by default (PatternFly handles this)

**Story 6.3 (DR Dashboard Table & Toolbar) established:**
- PhaseBadge, ExecutionResultBadge, ReplicationHealthIndicator created
- Color mapping: Label `color` prop for badges, inline CSS tokens for indicators
- Mock patterns for `useK8sWatchResource` and `react-router`

### Git Intelligence

Recent commits (last 5):
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings
- `c7916df` — Mark Story 5.7 as done in sprint status
- `d494cef` — Fix ScyllaDB write contention in DRExecution reconciler
- `f127e6f` — Implement Story 5.7: driver interface simplification & workflow symmetry
- `15e0ab8` — Add Soteria overview presentation

All recent work is Go backend. Stories 6.1–6.5b are ready-for-dev but not yet implemented. This story is the final Epic 6 story and depends on all six prior stories.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
