# Story 6.5: Plan Detail Shell & Overview Tab (DRLifecycleDiagram)

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want a plan detail page with an Overview tab showing the 4-phase DR lifecycle as an interactive state machine,
So that I can see my plan's lifecycle state, take context-aware actions, and monitor transition progress.

## Acceptance Criteria

1. **AC1 — Plan Detail page shell:** When a DRPlan is selected from the dashboard table (row click navigating to `/disaster-recovery/plans/:name`), a full-page detail view renders with four horizontal PatternFly `Tabs`: Overview, Waves, History, Configuration. The Waves, History, and Configuration tab panels render placeholder content ("Coming soon" or empty `TabContent`). (UX-DR4)

2. **AC2 — Plan header:** The Overview tab displays a plan header showing: plan name, VM count (from `status.vmCount` or computed), wave count (from `status.waveCount` or computed), and active cluster (from `status.activeSite`).

3. **AC3 — DRLifecycleDiagram (rest state):** A `DRLifecycleDiagram` custom component renders the 4-phase DR lifecycle as a visual cycle: SteadyState -> FailedOver -> DRedSteadyState -> FailedBack. Only the current rest phase is highlighted (accent-filled border); all other phases are faded to ~35% opacity. Each phase node shows: phase label, description, VM location (DC1/DC2), datacenter roles, and replication direction. The outgoing transition arrow from the current rest phase shows an enabled action button; all other arrows show faded action name text. Failover uses danger variant (red); Reprotect, Failback, and Restore use secondary variant. (FR36, UX-DR19)

4. **AC4 — DRLifecycleDiagram (transient state):** During an active transition (FailingOver, Reprotecting, FailingBack, or Restoring), a transition progress banner (PatternFly Alert, info variant, inline) appears above the diagram showing: action name, wave progress (e.g., "Wave 2 of 3"), elapsed time, estimated remaining time, and a `Link` to the execution detail view. The outgoing transition arrow shows "In progress..." with a blue indicator instead of a button. The destination phase node shows a dashed accent border ("arriving here"). All action buttons across the diagram are hidden. (UX-DR4)

5. **AC5 — Action button callback:** Clicking an action button on the lifecycle diagram calls an `onAction(action: string, plan: DRPlan)` callback prop. For now, this logs to console: `console.log('Trigger:', action, plan.metadata.name)`. Story 7.1 wires this to the pre-flight confirmation modal.

6. **AC6 — Accessibility:** DRLifecycleDiagram passes `jest-axe` with zero violations. Action buttons are reachable via Tab key. Phase nodes are readable by screen reader (ARIA role and labels). During transitions, an ARIA live region announces progress updates. (UX-DR16)

## Tasks / Subtasks

- [ ] Task 1: Create Plan Detail page shell with tabs (AC: #1)
  - [ ] 1.1 Modify `src/components/DRPlanDetail/DRPlanDetailPage.tsx` (from Story 6.2 placeholder) — add `useParams` to extract plan name from URL, `useDRPlan(name)` to fetch plan data, and PatternFly `Tabs`/`Tab`/`TabTitleText` for the four-tab layout
  - [ ] 1.2 Render Overview tab content (plan header + DRLifecycleDiagram); Waves, History, Configuration tabs render placeholder `TabContent`
  - [ ] 1.3 Wire breadcrumb: "DR Dashboard / {plan.metadata.name}" using `DRBreadcrumb` from Story 6.2
  - [ ] 1.4 Handle loading state (PatternFly `Skeleton`) and error state (PatternFly `Alert`)

- [ ] Task 2: Create plan header component (AC: #2)
  - [ ] 2.1 Create `src/components/DRPlanDetail/PlanHeader.tsx` — displays plan name (bold, `--pf-v5-global--FontSize--lg`), VM count, wave count, and active cluster
  - [ ] 2.2 Show effective phase as a `PhaseBadge` (from Story 6.3's shared component) next to the plan name
  - [ ] 2.3 Active cluster displayed as: "Active on: {activeSite}"

- [ ] Task 3: Create DRLifecycleDiagram component (AC: #3, #5)
  - [ ] 3.1 Create `src/components/DRPlanDetail/DRLifecycleDiagram.tsx` — accepts `plan: DRPlan`, `onAction: (action: string, plan: DRPlan) => void`
  - [ ] 3.2 Define the 4 rest phases as a constant array: `{ id, label, description, vmLocation, dc1Role, dc2Role, replication }`
  - [ ] 3.3 Define the 4 transitions: `{ from, to, action, transientPhase, isDanger }`
  - [ ] 3.4 Render 4 `PhaseNode` sub-components in a 2x2 grid layout (SteadyState top-left, FailedOver top-right, FailedBack bottom-left, DRedSteadyState bottom-right) with `TransitionEdge` sub-components connecting them
  - [ ] 3.5 Compute effective phase using `getEffectivePhase(plan)` from `src/utils/drPlanUtils.ts` (Story 6.3)
  - [ ] 3.6 Current rest phase: accent-filled border + background, full opacity. Other phases: 35% opacity, border only
  - [ ] 3.7 Active transition edge: render `Button` (danger for Failover, secondary for others) with `onClick={() => onAction(action, plan)}`
  - [ ] 3.8 Idle transition edges: faded action name text only
  - [ ] 3.9 Use PatternFly CSS custom properties for all colors: `--pf-v5-global--active-color--100` for accent, `--pf-v5-global--danger-color--100` for Failover

- [ ] Task 4: Implement transient state rendering (AC: #4)
  - [ ] 4.1 Create `src/components/DRPlanDetail/TransitionProgressBanner.tsx` — PatternFly `Alert` (variant="info", isInline) showing: action name, wave progress, elapsed time, estimated remaining time, Link to execution detail
  - [ ] 4.2 Extract active execution data: `plan.status.activeExecution` (name), `plan.status.activeExecutionMode` (mode)
  - [ ] 4.3 Use `useDRExecution(activeExecutionName)` to get wave progress from the active DRExecution resource
  - [ ] 4.4 Compute elapsed time from `execution.status.startTime` to now (using `Date.now()` with `useEffect` interval for live clock)
  - [ ] 4.5 Compute estimated remaining from execution duration history or wave progress ratio
  - [ ] 4.6 In DRLifecycleDiagram: when `isTransient(effectivePhase)`, render "In progress..." pill on the active transition edge instead of a button, dashed accent border on destination node, hide all action buttons

- [ ] Task 5: Accessibility (AC: #6)
  - [ ] 5.1 Add `role="img"` and `aria-label="DR lifecycle state machine diagram"` to the diagram container
  - [ ] 5.2 Each phase node: `role="group"`, `aria-label="{label}, {isActive ? 'current phase' : ''}, VMs on {location}, replication: {replication}"`
  - [ ] 5.3 Action buttons: standard `Button` component — inherently focusable via Tab
  - [ ] 5.4 Add `aria-live="polite"` region for transition progress announcements: "{action} in progress, wave {n} of {total}"
  - [ ] 5.5 Phase node opacity changes are decorative — screen reader gets state from aria-label, not visual opacity

- [ ] Task 6: Tests (AC: #1–#6)
  - [ ] 6.1 Create `tests/components/DRPlanDetailPage.test.tsx` — page renders with tabs, breadcrumb shows plan name, Overview tab active by default, other tabs render placeholders
  - [ ] 6.2 Create `tests/components/DRLifecycleDiagram.test.tsx`:
    - 4 phase nodes render with correct labels
    - Current phase (SteadyState) is highlighted, others faded
    - Failover button renders with danger variant from SteadyState
    - Reprotect button renders with secondary variant from FailedOver
    - No buttons render during transient phase (FailingOver)
    - Destination node gets dashed border during transition
    - Action button click calls onAction with correct args
  - [ ] 6.3 Create `tests/components/TransitionProgressBanner.test.tsx` — banner renders during transition with action name and wave progress, Link points to execution detail, banner absent during rest state
  - [ ] 6.4 Run `jest-axe` on DRLifecycleDiagram in rest state and transient state — zero violations
  - [ ] 6.5 Run `jest-axe` on DRPlanDetailPage — zero violations
  - [ ] 6.6 Verify `yarn build` succeeds with all new components

## Dev Notes

### Dependency on Stories 6.1, 6.2, and 6.3

**From Story 6.1:**
- `src/models/types.ts` — DRPlan, DRExecution TypeScript interfaces with `status.conditions`, `status.phase`, `status.activeExecution`, `status.activeExecutionMode`
- `src/hooks/useDRResources.ts` — `useDRPlan(name)`, `useDRExecution(name)` hooks wrapping `useK8sWatchResource`
- PatternFly 5 + Console SDK + Jest + jest-axe + RTL configured

**From Story 6.2:**
- `src/components/DRPlanDetail/DRPlanDetailPage.tsx` — placeholder page component for `/disaster-recovery/plans/:name` route
- `src/components/shared/DRBreadcrumb.tsx` — reusable breadcrumb component
- React Router v7 import pattern: `import { useParams, Link } from 'react-router'` (NOT `react-router-dom`)
- Default exports for page components (required by `$codeRef` / webpack module federation)

**From Story 6.3:**
- `src/utils/drPlanUtils.ts` — `getEffectivePhase(plan)` derives transient phase from rest phase + activeExecution. **Reuse directly.**
- `src/components/shared/PhaseBadge.tsx` — phase status badge component. **Reuse in plan header.**
- Mock pattern for `useK8sWatchResource`: `jest.fn(() => [mockPlan, true, null])`
- Mock pattern for `react-router`: `jest.mock('react-router', () => (...))`

### PatternFly Tabs API

Use PatternFly's `Tabs`, `Tab`, `TabTitleText`, `TabContent` from `@patternfly/react-core`:

```typescript
import { Tabs, Tab, TabTitleText, TabContent } from '@patternfly/react-core';

const [activeTab, setActiveTab] = React.useState<string | number>(0);

<Tabs activeKey={activeTab} onSelect={(_e, key) => setActiveTab(key)}>
  <Tab eventKey={0} title={<TabTitleText>Overview</TabTitleText>}>
    <PlanHeader plan={plan} />
    <DRLifecycleDiagram plan={plan} onAction={handleAction} />
  </Tab>
  <Tab eventKey={1} title={<TabTitleText>Waves</TabTitleText>}>
    <TabContent>Placeholder — implemented in Story 6.5b</TabContent>
  </Tab>
  <Tab eventKey={2} title={<TabTitleText>History</TabTitleText>}>
    <TabContent>Placeholder — implemented in Story 6.5b</TabContent>
  </Tab>
  <Tab eventKey={3} title={<TabTitleText>Configuration</TabTitleText>}>
    <TabContent>Placeholder — implemented in Story 6.5b</TabContent>
  </Tab>
</Tabs>
```

**Key API details:**
- `activeKey` + `onSelect` for controlled tab state
- `TabTitleText` wraps the tab label text
- `TabContent` renders the tab panel content
- `eventKey` is a unique identifier per tab (number or string)
- Keyboard navigation (Arrow keys between tabs, Tab into content) is built-in

### DRLifecycleDiagram — Component Architecture

This is the most complex custom component in Epic 6. It renders a 2x2 grid of phase nodes connected by transition arrows:

```
┌──────────────────┐              ┌──────────────────┐
│  ★ Steady State  │── Failover →│    Failed Over    │
│  VMs on DC1      │              │    VMs on DC2     │
│  DC1: Active     │              │    DC2: Active    │
│  Repl: DC1→DC2   │              │    No replication │
└──────────────────┘              └──────────────────┘
        ↑                                  ↓
     Restore                          Reprotect
        ↑                                  ↓
┌──────────────────┐              ┌──────────────────┐
│   Failed Back    │← Failback ──│ DR-ed Steady State│
│   VMs on DC1     │              │    VMs on DC2     │
│   DC1: Active    │              │    DC2: Active    │
│   No replication │              │    Repl: DC2→DC1  │
└──────────────────┘              └──────────────────┘
```

**Sub-components:**

1. **PhaseNode** — renders a single phase box with: label, description, VM location, DC roles, replication direction. Styling controlled by `isActive` (accent fill) and `isTransitioning` (dashed border) props. Faded at 35% opacity when not active/transitioning.

2. **TransitionEdge** — renders the connection between phases with: action name text, directional arrow indicator. Three states: `idle` (faded text), `available` (Button), `in-progress` (blue pill).

3. **DRLifecycleDiagram** — orchestrates the layout, computes state from plan data, and renders PhaseNodes + TransitionEdges.

**Layout approach:** CSS Grid or Flexbox. The wireframe canvas (lines 174-200 of `epic-6-wireframes.canvas.tsx`) uses nested `Row` components. For production, use a CSS Grid:

```css
.lifecycle-grid {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  grid-template-rows: auto auto auto;
  gap: var(--pf-v5-global--spacer--lg);
  align-items: center;
  justify-items: center;
}
```

**Important:** Use inline styles with PatternFly CSS custom properties only. No separate CSS files. No CSS modules.

### Phase-Transition State Machine Data

```typescript
const REST_PHASES = [
  { id: 'SteadyState', label: 'Steady State', description: 'Normal operations',
    vm: 'VMs on DC1', dc1: 'Active (source)', dc2: 'Passive (target)', replication: 'DC1 → DC2' },
  { id: 'FailedOver', label: 'Failed Over', description: 'Running on DR site',
    vm: 'VMs on DC2', dc1: 'Passive / down', dc2: 'Active (promoted)', replication: 'None' },
  { id: 'DRedSteadyState', label: 'DR-ed Steady State', description: 'Protected on DR site',
    vm: 'VMs on DC2', dc1: 'Passive (target)', dc2: 'Active (source)', replication: 'DC2 → DC1' },
  { id: 'FailedBack', label: 'Failed Back', description: 'Returned to origin',
    vm: 'VMs on DC1', dc1: 'Active (promoted)', dc2: 'Passive / down', replication: 'None' },
] as const;

const TRANSITIONS = [
  { from: 'SteadyState', to: 'FailedOver', action: 'Failover', transient: 'FailingOver', isDanger: true },
  { from: 'FailedOver', to: 'DRedSteadyState', action: 'Reprotect', transient: 'Reprotecting', isDanger: false },
  { from: 'DRedSteadyState', to: 'FailedBack', action: 'Failback', transient: 'FailingBack', isDanger: false },
  { from: 'FailedBack', to: 'SteadyState', action: 'Restore', transient: 'Restoring', isDanger: false },
] as const;
```

This data is derived directly from the backend's 8-phase state machine (project-context.md) and the wireframe canvas (lines 54-70).

### EffectivePhase — Reuse from Story 6.3

`getEffectivePhase(plan)` is already implemented in `src/utils/drPlanUtils.ts` from Story 6.3. It derives the transient phase from `plan.status.phase` (rest state only) + `plan.status.activeExecution` + `plan.status.activeExecutionMode`. Import and use directly — do NOT reimplement.

The diagram needs both:
- **Rest phase:** Which phase node to highlight (always a rest phase — the actual `.status.phase` value)
- **Effective phase:** Whether we're in a transient state (determines if buttons show or "In progress..." shows)

```typescript
const restPhase = plan.status?.phase ?? 'SteadyState';
const effectivePhase = getEffectivePhase(plan);
const isInTransition = effectivePhase !== restPhase;
```

### Transition Progress Banner

The banner appears above the diagram only during active transitions. It uses PatternFly `Alert` (info, inline):

```typescript
import { Alert, AlertActionLink } from '@patternfly/react-core';
import { Link } from 'react-router';

interface TransitionProgressBannerProps {
  plan: DRPlan;
  execution: DRExecution | null;
}

function TransitionProgressBanner({ plan, execution }: TransitionProgressBannerProps) {
  const effectivePhase = getEffectivePhase(plan);
  const restPhase = plan.status?.phase;
  if (effectivePhase === restPhase) return null;

  const transition = TRANSITIONS.find(t => t.transient === effectivePhase);
  if (!transition) return null;

  const waveProgress = execution?.status?.waves
    ? `Wave ${execution.status.waves.filter(w => w.result).length} of ${execution.status.waves.length}`
    : 'Starting...';

  return (
    <Alert
      variant="info"
      isInline
      title={`${transition.action} in progress`}
      actionLinks={
        <AlertActionLink>
          <Link to={`/disaster-recovery/executions/${plan.status?.activeExecution}`}>
            View execution details
          </Link>
        </AlertActionLink>
      }
    >
      {waveProgress} — Elapsed: {elapsed} · Est. remaining: ~{remaining}
    </Alert>
  );
}
```

**Elapsed time:** Compute from `execution.status.startTime` using a `useEffect` interval that updates every second during active transitions. Use `formatDuration` from `src/utils/formatters.ts` (Story 6.3).

**Estimated remaining:** Simple heuristic: `(totalWaves - completedWaves) * avgWaveDuration`. If no history is available, show "calculating...".

### Active Execution Data Flow

The page fetches two resources:
1. `useDRPlan(name)` — the DRPlan resource (includes `status.activeExecution`)
2. `useDRExecution(plan.status.activeExecution)` — the active DRExecution (only when `activeExecution` is non-empty)

```typescript
export default function DRPlanDetailPage() {
  const { name } = useParams<{ name: string }>();
  const [plan, planLoaded, planError] = useDRPlan(name!);
  const activeExecName = plan?.status?.activeExecution ?? '';
  const [execution, execLoaded] = useDRExecution(activeExecName);

  // ...
}
```

The `useDRExecution` hook should gracefully handle an empty name (return `[null, true, null]`).

### PhaseNode Visual Styling

Phase node appearance is controlled by two boolean props:

| State | `isActive` | `isTransitioning` | Border | Background | Opacity |
|---|---|---|---|---|---|
| Current rest phase | true | false | 2px solid accent | accent fill | 1.0 |
| Transition destination | false | true | 2px dashed accent | transparent | 1.0 |
| Other phases | false | false | 2px solid border-color | transparent | 0.35 |

```typescript
const nodeStyle: CSSProperties = {
  border: `2px ${isTransitioning ? 'dashed' : 'solid'} ${isActive ? 'var(--pf-v5-global--active-color--100)' : 'var(--pf-v5-global--BorderColor--100)'}`,
  borderRadius: 'var(--pf-v5-global--BorderRadius--sm)',
  padding: 'var(--pf-v5-global--spacer--md)',
  background: isActive ? 'var(--pf-v5-global--active-color--100)' : 'transparent',
  opacity: (isActive || isTransitioning) ? 1 : 0.35,
  color: isActive ? '#fff' : 'var(--pf-v5-global--Color--100)',
};
```

**Note on dark mode:** Using PatternFly CSS custom properties ensures dark mode works automatically. The only hardcoded value is `#fff` for text on the accent-filled active node — use `--pf-v5-global--Color--light-100` instead for dark mode safety.

### Action Button Variant Mapping

| Action | Button Variant | Rationale |
|---|---|---|
| Failover | `danger` | Most consequential action — red draws deliberate attention (UX-DR19) |
| Reprotect | `secondary` | Standard transition |
| Failback | `secondary` | Standard transition |
| Restore | `secondary` | Standard transition |

```typescript
<Button
  variant={transition.isDanger ? 'danger' : 'secondary'}
  onClick={() => onAction(transition.action, plan)}
>
  {transition.action}
</Button>
```

### Non-Negotiable Constraints

- **PatternFly 5 ONLY** — `Tabs`, `Tab`, `TabTitleText`, `Alert`, `AlertActionLink`, `Button`, `Skeleton` from `@patternfly/react-core`. No other UI libraries.
- **CSS custom properties only** — `--pf-v5-global--*` tokens. No hardcoded colors/spacing. Accent and border colors handled via tokens.
- **Console SDK hooks only** — `useDRPlan(name)`, `useDRExecution(name)` for data. No direct API calls.
- **No external state libraries** — no Redux, Zustand, or MobX.
- **Imports from `react-router`** — NOT `react-router-dom`.
- **Reuse `getEffectivePhase`** from `src/utils/drPlanUtils.ts` — do NOT duplicate.
- **Reuse `PhaseBadge`** from `src/components/shared/PhaseBadge.tsx` — do NOT duplicate.
- **Reuse `formatDuration`** from `src/utils/formatters.ts` — do NOT duplicate.
- **No separate CSS files** — inline styles with PatternFly tokens.
- **Default export** for `DRPlanDetailPage` — required by webpack module federation `$codeRef`.

### What NOT to Do

- **Do NOT implement Waves, History, or Configuration tab content** — Story 6.5b handles those. Render placeholder content in those tabs.
- **Do NOT implement the pre-flight confirmation modal** — Story 7.1 handles the modal. Action buttons call a callback prop that logs to console.
- **Do NOT implement WaveCompositionTree** — that's Story 6.5b.
- **Do NOT implement ReplicationHealthIndicator expanded variant** — that's Story 6.5b (Configuration tab).
- **Do NOT implement ExecutionHistoryTable** — that's Story 6.5b (History tab).
- **Do NOT use SVG for the lifecycle diagram** — use HTML elements with CSS for the phase nodes and arrows. PatternFly tokens for all styling. SVG adds unnecessary complexity.
- **Do NOT use a charting library** — the diagram is 4 boxes + 4 arrows. CSS Grid handles this.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT modify DRDashboard or toolbar components** — this story only touches `DRPlanDetail/`.

### Testing Approach

**Component tests:** React Testing Library with mock plan data. Mock `useDRPlan` to return controlled plan data with different phases:

```typescript
const mockSteadyStatePlan: DRPlan = {
  metadata: { name: 'erp-full-stack', uid: '1' },
  spec: { labelSelector: 'app.kubernetes.io/part-of=erp-system', waveLabel: 'soteria.io/wave', maxConcurrentFailovers: 4 },
  status: {
    phase: 'SteadyState',
    activeSite: 'dc1-prod',
    vmCount: 12,
    waveCount: 3,
    conditions: [{ type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s', lastTransitionTime: new Date().toISOString() }],
  },
};

const mockFailingOverPlan: DRPlan = {
  ...mockSteadyStatePlan,
  status: {
    ...mockSteadyStatePlan.status,
    activeExecution: 'erp-full-stack-failover-001',
    activeExecutionMode: 'disaster',
  },
};
```

**DRLifecycleDiagram tests:**
- 4 phase nodes render with labels (SteadyState, FailedOver, DRedSteadyState, FailedBack)
- SteadyState phase highlighted when `status.phase === 'SteadyState'`
- Failover button present with danger variant from SteadyState
- Reprotect button present from FailedOver (render with `status.phase: 'FailedOver'`)
- No buttons during transient phase (FailingOver) — "In progress..." text visible
- Destination node (FailedOver) gets dashed styling during FailingOver
- `onAction` callback called with `('Failover', plan)` when Failover button clicked
- `onAction` callback called with `('Reprotect', plan)` when Reprotect button clicked

**TransitionProgressBanner tests:**
- Banner absent when plan is in rest state (no `activeExecution`)
- Banner renders with "Failover in progress" during FailingOver
- Banner contains link to execution detail view
- Banner absent after transition completes (re-render with rest state)

**Accessibility:** `jest-axe` via `toHaveNoViolations` on:
- DRLifecycleDiagram in SteadyState (buttons present)
- DRLifecycleDiagram in FailingOver (no buttons, progress banner)
- DRPlanDetailPage with tabs

**Build verification:** `yarn build` must succeed.

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   ├── AlertBannerSystem.tsx       # (from 6.4)
│   │   ├── DRDashboard.tsx             # (from 6.3)
│   │   ├── DRDashboardPage.tsx         # (from 6.2)
│   │   ├── DRDashboardToolbar.tsx      # (from 6.3)
│   │   └── DRPlanActions.tsx           # (from 6.3)
│   ├── DRPlanDetail/
│   │   ├── DRPlanDetailPage.tsx        # REWRITTEN — tabs + Overview content
│   │   ├── DRLifecycleDiagram.tsx      # NEW — state machine visual
│   │   ├── PlanHeader.tsx              # NEW — plan name, VM/wave count, active cluster
│   │   └── TransitionProgressBanner.tsx # NEW — in-progress banner
│   ├── ExecutionDetail/
│   │   └── ExecutionDetailPage.tsx     # (from 6.2) — unchanged
│   └── shared/
│       ├── DRBreadcrumb.tsx            # (from 6.2) — unchanged
│       ├── ReplicationHealthIndicator.tsx # (from 6.3) — unchanged
│       ├── PhaseBadge.tsx              # (from 6.3) — reused in PlanHeader
│       └── ExecutionResultBadge.tsx    # (from 6.3) — unchanged
├── hooks/
│   ├── useDRResources.ts              # (from 6.1) — unchanged
│   ├── useDashboardState.ts           # (from 6.2) — unchanged
│   └── useFilterParams.ts             # (from 6.3) — unchanged
├── models/
│   └── types.ts                       # (from 6.1) — unchanged
└── utils/
    ├── formatters.ts                  # (from 6.3) — unchanged (formatDuration reused)
    └── drPlanUtils.ts                 # (from 6.3) — unchanged (getEffectivePhase reused)
```

**New test files:**
```
console-plugin/tests/
└── components/
    ├── DRPlanDetailPage.test.tsx        # NEW
    ├── DRLifecycleDiagram.test.tsx      # NEW
    └── TransitionProgressBanner.test.tsx # NEW
```

### Project Structure Notes

- `DRLifecycleDiagram.tsx`, `PlanHeader.tsx`, and `TransitionProgressBanner.tsx` placed in `src/components/DRPlanDetail/` — they are plan-detail-specific, not shared
- `DRPlanDetailPage.tsx` is the only file modified from previous stories (rewriting the placeholder from Story 6.2)
- No new utilities or hooks — reuses `getEffectivePhase` and `formatDuration` from Story 6.3
- Story 6.5b will import and render content inside the Waves, History, and Configuration tab panels — no changes to this story's page shell will be needed

### References

- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Plan Detail View] — Tab layout, Overview tab lifecycle diagram, transition progress banner
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § DRLifecycleDiagram] — Phase node states, transition edge states, transition-phase mapping, accessibility
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Action Hierarchy] — Failover = danger, others = secondary; valid transitions per phase
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Accessibility Considerations] — Keyboard navigation, screen reader, ARIA live regions
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Typography System] — Font sizes for plan header, pre-flight RPO
- [Source: _bmad-output/planning-artifacts/epic-6-wireframes.canvas.tsx § PlanDetailWireframe] — Interactive wireframe with state cycling, phase nodes, transition edges, progress banner
- [Source: _bmad-output/planning-artifacts/epic-6-wireframes.canvas.tsx § StateMachineDiagram] — Phase node rendering, edge state logic, layout structure
- [Source: _bmad-output/planning-artifacts/epics.md § Story 6.5] — Acceptance criteria (split version)
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 5
- [Source: _bmad-output/planning-artifacts/architecture.md § API & Communication Patterns] — 8-phase lifecycle, EffectivePhase, ActiveExecution
- [Source: _bmad-output/project-context.md § DRPlan 8-phase lifecycle] — Rest-state-only phase model, EffectivePhase derivation, ActiveExecution pointer
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK hooks, PatternFly-only, no state libraries
- [Source: _bmad-output/implementation-artifacts/6-3-dr-dashboard-table-toolbar.md] — getEffectivePhase utility, PhaseBadge component, mock patterns, formatDuration
- [Source: _bmad-output/implementation-artifacts/6-2-console-plugin-navigation-routing.md] — DRPlanDetailPage placeholder, DRBreadcrumb, route structure, default exports
- [Source: _bmad-output/implementation-artifacts/6-1-console-plugin-project-initialization.md] — CRD TypeScript interfaces, useDRPlan/useDRExecution hooks, Jest/RTL/jest-axe config
- [Source: _bmad-output/implementation-artifacts/6-4-alert-banner-system.md] — PatternFly Alert API (isInline, variant, actionLinks, AlertActionLink)

### Previous Story Intelligence

**Story 6.4 (Alert Banner System) established:**
- PatternFly `Alert` with `isInline` for embedded banners — same pattern needed for TransitionProgressBanner
- `AlertActionLink` for action links within alerts
- Omit `actionClose` for non-dismissible alerts
- `React.useMemo` for derived state from plan data

**Story 6.3 (DR Dashboard Table & Toolbar) established:**
- `getEffectivePhase(plan)` in `src/utils/drPlanUtils.ts` — derives transient phase from rest phase + activeExecution. **Reuse directly.**
- `PhaseBadge` in `src/components/shared/PhaseBadge.tsx` — PatternFly Label with DR-specific color mapping. **Reuse in plan header.**
- `formatDuration(start, end)` in `src/utils/formatters.ts` — **Reuse for elapsed time in progress banner.**
- Mock pattern: `jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({ useK8sWatchResource: jest.fn(() => [...]) }))`
- Mock pattern: `jest.mock('react-router', () => ({ ...jest.requireActual('react-router'), useParams: () => ({ name: 'erp-full-stack' }), Link: ({ to, children }: any) => <a href={to}>{children}</a> }))`

**Story 6.2 (Console Plugin Navigation & Routing) established:**
- `DRPlanDetailPage.tsx` as a placeholder page for `/disaster-recovery/plans/:name`
- `DRBreadcrumb.tsx` — reusable breadcrumb with `items` prop
- Default exports for page components (required by `$codeRef`)
- `useParams` from `react-router` for extracting URL params

**Story 6.1 (Console Plugin Project Initialization) established:**
- `types.ts` with DRPlan interface — `status.phase`, `status.activeExecution`, `status.activeExecutionMode`, `status.activeSite`, `status.vmCount`, `status.waveCount`
- `useDRPlan(name)` and `useDRExecution(name)` hooks
- Jest + RTL + jest-axe configured

### Git Intelligence

Recent commits (last 5):
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings
- `c7916df` — Mark Story 5.7 as done in sprint status
- `d494cef` — Fix ScyllaDB write contention in DRExecution reconciler
- `f127e6f` — Implement Story 5.7: driver interface simplification & workflow symmetry
- `15e0ab8` — Add Soteria overview presentation

All recent work is Go backend. Stories 6.1–6.4 are ready-for-dev but not yet implemented. This story builds on top of all four.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
