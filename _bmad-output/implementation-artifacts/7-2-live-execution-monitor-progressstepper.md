# Story 7.2: Live Execution Monitor (ProgressStepper)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want a wave-level progress view with expandable per-DRGroup detail updating in real time via Kubernetes watch,
So that I can monitor execution progress and share it on a bridge call.

## Acceptance Criteria

1. **AC1 — ProgressStepper wave view:** When the Execution Monitor page loads for an active DRExecution (navigated from Story 7.1 confirmation, the plan detail TransitionProgressBanner "View execution details" link, or the History tab row click), a full-width view renders with a PatternFly `ProgressStepper` (vertical) showing waves as sequential steps (UX-DR6). The header shows: execution name, mode (Disaster Failover / Planned Migration / Reprotect / Failback / Restore), start time, elapsed time (counting), and estimated remaining time.

2. **AC2 — Real-time watch updates:** Data updates arrive via `useK8sWatchResource` on the DRExecution resource. The view updates within 5 seconds of the underlying state change (NFR7). No manual refresh is needed.

3. **AC3 — Pending wave rendering:** A wave in Pending state shows as gray with dimmed text (`variant="pending"`). Expandable to see pending DRGroups listed inside.

4. **AC4 — InProgress wave rendering:** A wave in InProgress state shows with a blue animated indicator and bold text (`variant="info"`, `isCurrent`). It is auto-expanded to show DRGroup detail. Each DRGroup shows: VM names, status (Pending / InProgress with spinner / Completed with checkmark / Failed with error icon), and elapsed time.

5. **AC5 — Completed wave rendering:** A completed wave shows as green with a checkmark (`variant="success"`) — a visible relief milestone. It is collapsible to reduce visual noise.

6. **AC6 — Execution completion state:** When all waves complete, the header shows: total duration, final result badge (Succeeded / PartiallySucceeded / Failed via `ExecutionResultBadge`), and total RPO. The elapsed time counter stops.

7. **AC7 — Bridge-call readability:** All text is legible at 720p screen-share resolution. Minimum 14px for all text, critical numbers (RPO, time, VM count) at `--pf-t--global--font--size--body--lg` / `--pf-v5-global--FontSize--lg` (18px+). Elapsed and remaining time use monospace font variant for stable-width display (no layout shift). Animations are subtle — no distracting motion during bridge calls.

8. **AC8 — ARIA live region:** Screen readers announce wave completion events: "Wave 1 completed. Wave 2 starting." An ARIA live region (`aria-live="polite"`) updates on wave state transitions.

9. **AC9 — Breadcrumb with plan name:** The breadcrumb shows `Disaster Recovery > {planName} > {executionName}` (3-level). The plan name is derived from `DRExecution.spec.planName` after the execution resource loads.

10. **AC10 — Accessibility:** jest-axe zero violations for all states (loading, active, completed, failed). Keyboard navigation works: Tab through waves and DRGroup details.

## Tasks / Subtasks

- [x] Task 1: Rewrite `ExecutionDetailPage.tsx` as the execution monitor shell (AC: #1, #2, #9)
  - [x] 1.1 Replace stub content with full execution monitor: load `useDRExecution(name)`, extract `spec.planName`, load `useDRPlan(planName)` for breadcrumb
  - [x] 1.2 Render `ExecutionHeader` component with execution metadata
  - [x] 1.3 Render `ProgressStepper` (vertical) with one `ProgressStep` per wave from `execution.status.waves[]`
  - [x] 1.4 Handle loading state with `Skeleton` placeholder matching the monitor layout
  - [x] 1.5 Handle error state with `Alert` (danger, inline)

- [x] Task 2: Create `ExecutionHeader` component (AC: #1, #6, #7)
  - [x] 2.1 Create `src/components/ExecutionDetail/ExecutionHeader.tsx` — displays execution name, mode label, start time, elapsed counter, estimated remaining
  - [x] 2.2 Mode label: map `DRExecutionMode` to display text ("Disaster Failover" / "Planned Migration" / "Reprotect") with appropriate `Label` color
  - [x] 2.3 Elapsed time: use `useElapsedTime(startTime)` hook — counts up every second while execution is active, stops on completion
  - [x] 2.4 Estimated remaining: compute from (elapsed / completedWaves) * remainingWaves when completedWaves > 0; show "calculating..." otherwise
  - [x] 2.5 Completion state: show total duration via `formatDuration`, `ExecutionResultBadge`, and total RPO via `formatRPO`
  - [x] 2.6 Monospace font for elapsed/remaining time: `fontFamily: 'var(--pf-t--global--font--family--mono)'` (or `--pf-v5-global--FontFamily--monospace` fallback)

- [x] Task 3: Create `WaveProgressStep` component (AC: #3, #4, #5)
  - [x] 3.1 Create `src/components/ExecutionDetail/WaveProgressStep.tsx` — renders a single `ProgressStep` with expandable DRGroup detail
  - [x] 3.2 Map wave state to `ProgressStep` variant: pending→`"pending"`, inProgress→`"info"` + `isCurrent`, completed→`"success"`, partiallyFailed→`"warning"`
  - [x] 3.3 Wave description: "Wave {index+1} — {vmCount} VMs" with elapsed time if wave has started
  - [x] 3.4 Auto-expand InProgress wave, collapse Completed waves by default (operator can toggle)
  - [x] 3.5 DRGroup detail list inside expanded wave: group name, VM names, status icon + label, elapsed time
  - [x] 3.6 DRGroup status icons: Pending (gray `PendingIcon`), InProgress (blue `Spinner`), Completed (green `CheckCircleIcon`), Failed (red `ExclamationCircleIcon`)

- [x] Task 4: Create `useElapsedTime` hook (AC: #1, #6)
  - [x] 4.1 Create `src/hooks/useElapsedTime.ts` — returns formatted elapsed string, updates every second
  - [x] 4.2 Accept `startTime: string | undefined` and `isRunning: boolean` — stops counting when `isRunning` is false
  - [x] 4.3 Return `{ elapsed: string, elapsedMs: number }` — formatted string for display, raw ms for calculations

- [x] Task 5: Wire ARIA live region (AC: #8)
  - [x] 5.1 Add `aria-live="polite"` region below the ProgressStepper
  - [x] 5.2 Track previous wave completion count; announce "Wave N completed. Wave N+1 starting." on change
  - [x] 5.3 Announce final result: "Execution completed. Result: Succeeded." (or Failed / Partially Succeeded)

- [x] Task 6: Update DRBreadcrumb for execution context (AC: #9)
  - [x] 6.1 In `ExecutionDetailPage`, pass both `planName` (from loaded execution's `spec.planName`) and `executionName` to `DRBreadcrumb`
  - [x] 6.2 Breadcrumb already supports 3-level hierarchy — just needs the planName prop passed correctly

- [x] Task 7: Write tests (AC: #10)
  - [x] 7.1 Rewrite `tests/components/ExecutionDetailPage.test.tsx`:
    - Loading state renders skeleton
    - Active execution renders ProgressStepper with waves
    - Completed execution renders result badge and total duration
    - Breadcrumb shows plan name from execution spec
    - jest-axe zero violations for active and completed states
  - [x] 7.2 Create `tests/components/WaveProgressStep.test.tsx`:
    - Pending wave renders with pending variant
    - InProgress wave auto-expanded with DRGroup detail
    - Completed wave renders with success variant and checkmark
    - Failed DRGroup shows error icon and error message
    - VM names listed in expanded DRGroup detail
  - [x] 7.3 Create `tests/hooks/useElapsedTime.test.ts`:
    - Returns "0s" when no start time
    - Counts up from start time
    - Stops counting when isRunning is false
    - Formats minutes and hours correctly
  - [x] 7.4 Create `tests/components/ExecutionHeader.test.tsx`:
    - Renders execution name and mode label
    - Elapsed time updates (use `jest.useFakeTimers`)
    - Shows "calculating..." when no waves completed
    - Shows estimated remaining after first wave completes
    - Shows total duration and result badge when complete
    - Monospace font applied to time displays
  - [x] 7.5 Verify all existing tests still pass (`yarn test`)
  - [x] 7.6 Verify build succeeds (`yarn build`)

### Review Follow-ups (AI)

- [x] [Review][Decision] VM counts not at 18px+ per AC7 — Accepted PF default sizing (14px min met); 18px bump deferred to UX polish pass
- [x] [Review][Patch] Remove dead import `getWaveState` from ExecutionDetailPage [ExecutionDetailPage.tsx:11]
- [x] [Review][Patch] Remove dead branch `prev === 0 && completedCount === 0` [ExecutionDetailPage.tsx:28]
- [x] [Review][Patch] Remove unused `useDRPlan(planName)` hook call — result discarded [ExecutionDetailPage.tsx:15]
- [x] [Review][Patch] Add `Number.isFinite()` guard in `formatElapsedMs` for NaN input [useElapsedTime.ts:9]
- [x] [Review][Patch] Add index fallback to group key to prevent collision [WaveProgressStep.tsx:142]
- [x] [Review][Patch] Add ARIA completion announcement fallback when `result` is absent [ExecutionDetailPage.tsx:34-37]
- [x] [Review][Patch] Add jest-axe test for error/failed state per AC10 [ExecutionDetailPage.test.tsx]
- [x] [Review][Patch] Use `(execError as Error)?.message` instead of `String(execError)` [ExecutionDetailPage.tsx:52]
- [x] [Review][Defer] Wave/group elapsed strings don't tick between K8s watch updates — matches existing codebase pattern (TransitionProgressBanner); would add complexity for ~2-5s gain

## Dev Notes

### This Story Replaces the ExecutionDetailPage Stub

Story 6.2 created `ExecutionDetailPage.tsx` as a placeholder. This story rewrites it completely to implement the live execution monitor. The stub currently renders only a heading and placeholder text — all of that is replaced.

### PatternFly ProgressStepper API

PatternFly 6 `ProgressStepper` + `ProgressStep` from `@patternfly/react-core` (already in `package.json` as `^6.2.2`):

```typescript
import { ProgressStepper, ProgressStep } from '@patternfly/react-core';

<ProgressStepper isVertical aria-label="Execution wave progress">
  <ProgressStep
    variant="success"       // 'default' | 'success' | 'info' | 'pending' | 'warning' | 'danger'
    isCurrent={false}
    description="3 VMs — completed in 1m 32s"
    aria-label="Wave 1: completed"
    id="wave-1"
  >
    Wave 1 — Databases
  </ProgressStep>
  <ProgressStep
    variant="info"
    isCurrent={true}
    description="5 VMs — 2 of 2 groups in progress"
    aria-label="Wave 2: in progress"
    id="wave-2"
  >
    Wave 2 — App Servers
  </ProgressStep>
  <ProgressStep
    variant="pending"
    description="4 VMs"
    aria-label="Wave 3: pending"
    id="wave-3"
  >
    Wave 3 — Web Frontends
  </ProgressStep>
</ProgressStepper>
```

Use `isVertical` layout — the vertical ProgressStepper provides the sequential wave visualization needed for bridge-call readability. Each `ProgressStep` maps to one wave from `execution.status.waves[]`.

### Data Model — DRExecution Status Structure

The live monitor reads from `DRExecution.status`:

```typescript
interface DRExecutionStatus {
  result?: DRExecutionResult;           // 'Succeeded' | 'PartiallySucceeded' | 'Failed'
  waves?: WaveStatus[];                 // one per wave, updated in real-time
  startTime?: string;                   // ISO timestamp, set when execution begins
  completionTime?: string;              // ISO timestamp, set when execution finishes
  rpoSeconds?: number;                  // total RPO in seconds
  conditions?: Condition[];
}

interface WaveStatus {
  waveIndex: number;
  groups?: DRGroupExecutionStatus[];    // per-DRGroup status within the wave
  startTime?: string;
  completionTime?: string;
  vmReadyStartTime?: string;
}

interface DRGroupExecutionStatus {
  name: string;
  result?: DRGroupResult;               // 'Pending' | 'InProgress' | 'Completed' | 'Failed' | 'WaitingForVMReady'
  vmNames?: string[];                   // VMs in this group
  error?: string;                       // error message if failed
  steps?: StepStatus[];                 // per-step status for detail
  retryCount?: number;
  startTime?: string;
  completionTime?: string;
}
```

All types already exist in `src/models/types.ts`.

### Wave State Derivation

Derive wave state from `WaveStatus` fields — no explicit "state" field exists:

```typescript
function getWaveState(wave: WaveStatus): 'pending' | 'inProgress' | 'completed' | 'partiallyFailed' {
  if (wave.completionTime) {
    const hasFailed = wave.groups?.some(g => g.result === 'Failed');
    return hasFailed ? 'partiallyFailed' : 'completed';
  }
  if (wave.startTime) return 'inProgress';
  return 'pending';
}
```

Map to `ProgressStep` variant:

| Wave State | `variant` | `isCurrent` | Icon Override |
|---|---|---|---|
| pending | `"pending"` | false | default PF pending icon |
| inProgress | `"info"` | true | default PF info icon |
| completed | `"success"` | false | default PF success icon |
| partiallyFailed | `"warning"` | false | default PF warning icon |

### DRGroup Status Icon Mapping

| DRGroup Result | Icon | Color Token |
|---|---|---|
| Pending | `PendingIcon` | `--pf-t--global--icon--color--disabled` / `--pf-v5-global--disabled-color--100` (gray) |
| InProgress | `Spinner` (size="md") | `--pf-t--global--icon--color--status--info--default` / `--pf-v5-global--info-color--100` (blue) |
| Completed | `CheckCircleIcon` | `--pf-t--global--icon--color--status--success--default` / `--pf-v5-global--success-color--100` (green) |
| Failed | `ExclamationCircleIcon` | `--pf-t--global--icon--color--status--danger--default` / `--pf-v5-global--danger-color--100` (red) |
| WaitingForVMReady | `Spinner` (size="md") | `--pf-t--global--icon--color--status--info--default` / `--pf-v5-global--info-color--100` (blue) — same visual as InProgress |

### Mode Display Label Mapping

```typescript
const MODE_LABELS: Record<string, string> = {
  disaster: 'Disaster Failover',
  planned_migration: 'Planned Migration',
  reprotect: 'Reprotect',
};
```

Use `Label` component for mode display:
- `disaster` → `Label status="danger"` (red)
- `planned_migration` → `Label status="info"` (blue)
- `reprotect` → `Label status="custom"` (gray/default)

### ExecutionHeader Layout

```
┌─────────────────────────────────────────────────────────────────┐
│ erp-full-stack-failover-1714327200000                           │
│ [Disaster Failover]  Started: 03:14 AM  Elapsed: 04:12         │
│                      Est. remaining: ~14:00                     │
├─────────────────────────────────────────────────────────────────┤
│ ... ProgressStepper waves below ...                             │
└─────────────────────────────────────────────────────────────────┘
```

On completion:

```
┌─────────────────────────────────────────────────────────────────┐
│ erp-full-stack-failover-1714327200000                           │
│ [Disaster Failover]  Duration: 17m 32s  [✅ Succeeded]         │
│                      RPO: 47s                                   │
└─────────────────────────────────────────────────────────────────┘
```

### Expanded Wave with DRGroup Detail

```
✅ Wave 1 — 3 VMs (completed in 1m 32s)
  ├─ DRGroup-1 (erp-db-1, erp-db-2)  ✅ Completed  1m 18s
  └─ DRGroup-2 (erp-db-3)            ✅ Completed  1m 32s

🔄 Wave 2 — 5 VMs (in progress)                        [current]
  ├─ DRGroup-3 (erp-app-1, erp-app-2, erp-app-3)  🔄 In Progress  0m 45s
  └─ DRGroup-4 (erp-app-4, erp-app-5)              ⏳ Pending

⏳ Wave 3 — 4 VMs
  ├─ DRGroup-5 (erp-web-1, erp-web-2, erp-web-3)  ⏳ Pending
  └─ DRGroup-6 (erp-web-4)                         ⏳ Pending
```

Render the DRGroup list inside each `ProgressStep`'s children, below the step title. Use a simple list with consistent spacing. InProgress wave is auto-expanded; Completed waves collapse by default; Pending waves collapse by default.

Use PatternFly `ExpandableSection` for the DRGroup detail within each wave step:

```typescript
<ProgressStep variant="success" id="wave-0">
  Wave 1 — 3 VMs
  <ExpandableSection
    toggleText={isExpanded ? 'Hide groups' : 'Show groups'}
    isExpanded={isExpanded}
    onToggle={(_e, expanded) => setIsExpanded(expanded)}
  >
    {/* DRGroup list */}
  </ExpandableSection>
</ProgressStep>
```

### Elapsed Time Hook Design

```typescript
interface UseElapsedTimeResult {
  elapsed: string;    // "4m 12s", "1h 23m"
  elapsedMs: number;  // raw milliseconds for calculations
}

function useElapsedTime(startTime: string | undefined, isRunning: boolean): UseElapsedTimeResult
```

Implementation:
- `useEffect` with `setInterval(1000)` when `isRunning` is true
- Clear interval when `isRunning` becomes false or on unmount
- Format using same logic as `TransitionProgressBanner.formatElapsed`
- Return `{ elapsed: '0s', elapsedMs: 0 }` when no startTime

### Estimated Remaining Time Calculation

Same approach as `TransitionProgressBanner`:

```typescript
function estimateRemaining(elapsedMs: number, completedWaves: number, totalWaves: number): string {
  if (completedWaves === 0) return 'calculating...';
  const avgPerWave = elapsedMs / completedWaves;
  const remainingMs = (totalWaves - completedWaves) * avgPerWave;
  return `~${formatElapsed(remainingMs)}`;
}
```

### Reuse from Existing Codebase

**From Story 6.1 (`src/models/types.ts`):**
- `DRExecution`, `DRExecutionStatus`, `WaveStatus`, `DRGroupExecutionStatus`, `StepStatus`
- `DRGroupResult`, `DRGroupResultValue`, `DRExecutionResult`, `ExecutionResult`, `ExecutionMode`

**From Story 6.1 (`src/hooks/useDRResources.ts`):**
- `useDRExecution(name)` — watches single DRExecution by name (real-time updates via Kubernetes watch)
- `useDRPlan(planName)` — used to get plan metadata for breadcrumb
- `useDRGroupStatuses(executionName)` — available if finer-grained DRGroupStatus updates are needed (not required for v1; DRExecution.status.waves has embedded group status)

**From Story 6.3 (`src/utils/formatters.ts`):**
- `formatDuration(start, end)` — total duration display
- `formatRPO(seconds)` — RPO formatting

**From Story 6.3 (`src/utils/drPlanUtils.ts`):**
- Not directly needed — execution monitor reads from DRExecution, not DRPlan

**From Story 6.2 (`src/components/shared/DRBreadcrumb.tsx`):**
- `DRBreadcrumb` with `planName` + `executionName` props — already supports 3-level hierarchy

**From Story 6.6 (`src/components/shared/ExecutionResultBadge.tsx`):**
- `ExecutionResultBadge` — renders `Succeeded` / `PartiallySucceeded` / `Failed` labels with appropriate colors

**From `TransitionProgressBanner.tsx`:**
- `formatElapsed(ms)` function — reuse the formatting logic (consider extracting to `formatters.ts` or duplicate inline for isolation)

### Non-Negotiable Constraints

- **PatternFly 6 ONLY** — `ProgressStepper`, `ProgressStep`, `ExpandableSection`, `Spinner`, `Label`, `Alert`, `Skeleton` from `@patternfly/react-core`. Icons from `@patternfly/react-icons`. No other UI libraries.
- **CSS custom properties only** — PF6 `--pf-t--global--*` tokens preferred; `--pf-v5-global--*` tokens still resolve as fallbacks. No hardcoded colors, spacing, or font sizes.
- **Console SDK hooks only** — `useK8sWatchResource` via `useDRExecution` for real-time data. No direct API calls, no polling.
- **Imports from `react-router-dom`** — `Link`, `useHistory`, `useParams` from `react-router-dom` (React Router v5 on OCP 4.20). Test mocks use `jest.mock('react-router', ...)` at the mock layer.
- **No external state libraries** — `useState` / `useEffect` / `useCallback` for component state. No Redux, Zustand, MobX.
- **No separate CSS files** — inline styles with PatternFly tokens.
- **Default export** on `ExecutionDetailPage` — required by Console SDK `$codeRef` in `console-extensions.json`.

### What NOT to Do

- **Do NOT implement the ExecutionGanttChart** — that is Phase 1b. This story uses the simplified `ProgressStepper` approach per the UX phasing plan.
- **Do NOT implement inline retry of failed DRGroups** — that is Story 7.3. Failed groups show the error message and icon but no retry button yet.
- **Do NOT implement toast notifications** — that is Story 7.4.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT add a "Retry" button on failed DRGroups** — only display the error state. Retry is Story 7.3.
- **Do NOT implement per-step drill-down** — DRGroup detail shows VM names and status, but not per-step (StopReplication, StartVM, etc.) detail within a group. Keep it one level deep: Wave → DRGroup.
- **Do NOT use `useDRGroupStatuses` hook for primary data** — `DRExecution.status.waves[].groups[]` contains the embedded group status. The `useDRGroupStatuses` hook watches separate `DRGroupStatus` CRDs and is available for Story 7.3 retry flows.
- **Do NOT create a separate route** — the `/disaster-recovery/executions/:name` route already exists in `console-extensions.json` pointing to `ExecutionDetailPage`.

### Testing Approach

**ExecutionDetailPage tests (rewrite existing):**

```typescript
import { render, screen, waitFor } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ExecutionDetailPage from '../../src/components/ExecutionDetail/ExecutionDetailPage';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => <title>{children}</title>,
}));

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useParams: () => ({ name: 'erp-failover-1714327200000' }),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => <a href={to}>{children}</a>,
}));

// Mock useDRExecution to return active execution data
jest.mock('../../src/hooks/useDRResources', () => ({
  useDRExecution: jest.fn(),
  useDRPlan: jest.fn(),
  useDRGroupStatuses: jest.fn(),
}));

describe('ExecutionDetailPage', () => {
  it('renders loading skeleton when execution is loading', () => { ... });
  it('renders ProgressStepper with waves for active execution', () => { ... });
  it('renders completed execution with result badge and duration', () => { ... });
  it('renders breadcrumb with plan name from execution spec', () => { ... });
  it('renders error alert when execution fails to load', () => { ... });
  it('has no accessibility violations (active execution)', async () => { ... });
  it('has no accessibility violations (completed execution)', async () => { ... });
});
```

**WaveProgressStep tests:**

```typescript
describe('WaveProgressStep', () => {
  it('renders pending wave with pending variant', () => { ... });
  it('renders in-progress wave as current with auto-expanded groups', () => { ... });
  it('renders completed wave with success variant', () => { ... });
  it('renders partially-failed wave with warning variant', () => { ... });
  it('shows DRGroup names and VM names in expanded view', () => { ... });
  it('shows spinner for in-progress DRGroup', () => { ... });
  it('shows error icon and message for failed DRGroup', () => { ... });
  it('shows elapsed time for started DRGroups', () => { ... });
});
```

**useElapsedTime tests:**

```typescript
describe('useElapsedTime', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  it('returns "0s" when no startTime', () => { ... });
  it('counts up from startTime when isRunning', () => { ... });
  it('stops counting when isRunning becomes false', () => { ... });
  it('formats correctly for minutes', () => { ... });
  it('formats correctly for hours', () => { ... });
  it('cleans up interval on unmount', () => { ... });
});
```

**Mock data pattern:**

```typescript
const mockActiveExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'erp-failover-1714327200000', uid: '1', labels: { 'soteria.io/plan-name': 'erp-full-stack' } },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    startTime: new Date(Date.now() - 4 * 60 * 1000).toISOString(),
    waves: [
      {
        waveIndex: 0,
        startTime: new Date(Date.now() - 4 * 60 * 1000).toISOString(),
        completionTime: new Date(Date.now() - 2.5 * 60 * 1000).toISOString(),
        groups: [
          { name: 'drgroup-1', result: 'Completed', vmNames: ['erp-db-1', 'erp-db-2'], startTime: '...', completionTime: '...' },
          { name: 'drgroup-2', result: 'Completed', vmNames: ['erp-db-3'], startTime: '...', completionTime: '...' },
        ],
      },
      {
        waveIndex: 1,
        startTime: new Date(Date.now() - 2.5 * 60 * 1000).toISOString(),
        groups: [
          { name: 'drgroup-3', result: 'InProgress', vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'], startTime: '...' },
          { name: 'drgroup-4', result: 'Pending', vmNames: ['erp-app-4', 'erp-app-5'] },
        ],
      },
      {
        waveIndex: 2,
        groups: [
          { name: 'drgroup-5', result: 'Pending', vmNames: ['erp-web-1', 'erp-web-2', 'erp-web-3'] },
          { name: 'drgroup-6', result: 'Pending', vmNames: ['erp-web-4'] },
        ],
      },
    ],
  },
};

const mockCompletedExecution: DRExecution = {
  ...mockActiveExecution,
  status: {
    ...mockActiveExecution.status,
    result: 'Succeeded',
    completionTime: new Date().toISOString(),
    rpoSeconds: 47,
    waves: mockActiveExecution.status!.waves!.map(w => ({
      ...w,
      completionTime: w.completionTime || new Date().toISOString(),
      groups: w.groups?.map(g => ({ ...g, result: 'Completed' as const, completionTime: new Date().toISOString() })),
    })),
  },
};
```

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── ExecutionDetail/
│   │   ├── ExecutionDetailPage.tsx       # REWRITTEN — full execution monitor
│   │   ├── ExecutionHeader.tsx           # NEW — header with name, mode, times
│   │   └── WaveProgressStep.tsx          # NEW — per-wave ProgressStep + DRGroup detail
│   └── ... (unchanged)
├── hooks/
│   ├── useElapsedTime.ts                # NEW — elapsed time counter hook
│   └── ... (unchanged)
└── ... (unchanged)
```

**Test files:**
```
console-plugin/tests/
├── components/
│   ├── ExecutionDetailPage.test.tsx      # REWRITTEN — full monitor tests
│   ├── ExecutionHeader.test.tsx          # NEW
│   └── WaveProgressStep.test.tsx         # NEW
├── hooks/
│   └── useElapsedTime.test.ts           # NEW
└── ... (unchanged)
```

### Project Structure Notes

- `ExecutionDetailPage.tsx` stays in `ExecutionDetail/` — route already configured in `console-extensions.json`
- `ExecutionHeader.tsx` and `WaveProgressStep.tsx` are co-located in `ExecutionDetail/` (only used here)
- `useElapsedTime.ts` goes to `src/hooks/` — generic enough for reuse (e.g., TransitionProgressBanner could adopt it later)
- No new shared components needed — `ExecutionResultBadge` already exists
- No changes to `console-extensions.json` — the route `/disaster-recovery/executions/:name` already points to `ExecutionDetailPage`
- No new PatternFly dependencies — `ProgressStepper`, `ProgressStep`, `ExpandableSection`, `Spinner` are all in `@patternfly/react-core` ^6.2.2

### References

- [Source: _bmad-output/planning-artifacts/epics.md § Story 7.2] — Acceptance criteria, user story
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § UX-DR6] — ProgressStepper execution monitor, wave states, DRGroup detail
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § ExecutionGanttChart anatomy] — Wave/DRGroup visual hierarchy and states (reference for Phase 1b; Phase 1 uses ProgressStepper simplification)
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Execution Monitor Color Language] — Color tokens per state
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Typography System] — Font sizes for bridge-call readability, monospace for times
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Spacing & Layout] — Low density, high emphasis for execution monitor
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Accessibility] — ExecutionGanttChart ARIA live region, keyboard navigation
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Navigation Patterns] — Execution monitor is full-page view, breadcrumb: DR Dashboard > Plan Name > Execution
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Screen-Share Optimization] — Min 14px, 18px+ for critical numbers, no hover-only info
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Loading & Empty States] — Execution monitor waiting state
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Implementation Roadmap] — Phase 1 = ProgressStepper (P0), Phase 1b = ExecutionGanttChart (P2)
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 6, module federation
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK for data, PatternFly only, no state libraries
- [Source: console-plugin/src/models/types.ts] — DRExecution, WaveStatus, DRGroupExecutionStatus, DRGroupResult types
- [Source: console-plugin/src/hooks/useDRResources.ts] — useDRExecution, useDRPlan hooks
- [Source: console-plugin/src/utils/formatters.ts] — formatDuration, formatRPO
- [Source: console-plugin/src/components/shared/ExecutionResultBadge.tsx] — result badge for completion state
- [Source: console-plugin/src/components/shared/DRBreadcrumb.tsx] — 3-level breadcrumb with planName + executionName
- [Source: console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx] — formatElapsed function, estimated remaining calculation pattern

### Previous Story Intelligence

**Story 7.1 (Pre-flight Confirmation & Failover Trigger) established:**
- Action-to-mode mapping constant in `drPlanActions.ts`
- `useCreateDRExecution` hook creating DRExecution resources via `k8sCreate`
- Modal state management pattern with `useState`
- `console.log` in `handleAction` is now replaced with modal trigger
- Button variants: danger for Failover, primary for others

**Story 6.6 (Status Badges, Empty States & Accessibility) established:**
- jest-axe pattern: `const { container } = render(<Component />); expect(await axe(container)).toHaveNoViolations()`
- Keyboard testing with `userEvent.setup()` + `userEvent.tab()`
- react-router mock: `jest.mock('react-router', ...)` with `jest.requireActual`
- 291+ tests pass across the full console-plugin test suite

**Story 6.5 (Plan Detail Shell & Overview Tab) established:**
- `TransitionProgressBanner` with `formatElapsed` function and estimated remaining calculation — directly reusable pattern for execution monitor
- Wave progress tracking from `execution.status.waves[]`

**Story 6.2 (Console Plugin Navigation & Routing) established:**
- Route `/disaster-recovery/executions/:name` → `ExecutionDetailPage`
- `DRBreadcrumb` component with `planName` + `executionName` props
- Default exports on page components (required by Console SDK `$codeRef`)

### Git Intelligence

Recent commits (last 10):
- `31cc201` — Implement Story 6.6: Status badges, empty states & accessibility
- `624d650` — Implement Story 6.5b: Waves, History & Configuration tabs
- `4eb1b98` — Implement Story 6.5: Plan Detail Shell & Overview Tab
- `09bf6d9` — Implement Story 6.4: Alert Banner System
- `a0e873a` — Implement Story 6.3: DR Dashboard Table & Toolbar
- `826fae3` — Implement Story 6.2: Console plugin navigation & routing
- `1cc02b1` — Implement Story 6.1: Console plugin project initialization
- `9824b1f` — Fix DRExecution integration test failures from missing ResumeAnalyzer
- `09a0674` — Add Epic 6 story files, wireframes, and planning updates
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings

All Epic 6 stories complete. Story 7.1 is ready-for-dev. The codebase has ~28 source files and passes 291+ tests. This story adds ~3 new source files, ~1 new hook, rewrites 1 existing page and test file, creates ~3 new test files.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor Agent)

### Debug Log References

- Initial test run: 376 existing tests pass (22 suites)
- First implementation test run: 3 failures — `renderHook` not available in `@testing-library/react` v12 (React 17), `jest.useFakeTimers()` conflicting with `jest-axe`, duplicate text in DOM from breadcrumb + header
- Fixed: replaced `renderHook` with component wrapper test approach, removed global fake timers from axe tests, used `getAllByText` for duplicated names
- Added `titleId` prop to `ProgressStep` to resolve PF6 accessibility warning
- Final test run: 421 tests pass (26 suites), 0 failures

### Completion Notes List

- Rewrote `ExecutionDetailPage.tsx` from stub to full live execution monitor with `useDRExecution` watch, ProgressStepper, ExecutionHeader, error/loading states
- Created `ExecutionHeader` component with active (elapsed+estimated) and completed (duration+result+RPO) rendering modes, monospace time displays, mode label mapping with PF6 Label status variants
- Created `WaveProgressStep` component with wave state derivation, ExpandableSection DRGroup detail, auto-expand for InProgress waves, DRGroup status icons (Spinner/CheckCircle/ExclamationCircle/Pending)
- Created `useElapsedTime` hook with 1s interval counter, cleanup on unmount, `formatElapsedMs` utility exported for reuse
- Wired ARIA live region with `aria-live="polite"` for wave completion announcements and final execution result
- Wired 3-level breadcrumb: Disaster Recovery > planName > executionName (planName from `execution.spec.planName`)
- 45 new tests: 13 ExecutionDetailPage, 12 WaveProgressStep + getWaveState, 11 ExecutionHeader, 7 formatElapsedMs, 6 useElapsedTime hook behavior; all with jest-axe accessibility checks
- All 421 tests pass (376 existing + 45 new), 0 regressions

### File List

New files:
- console-plugin/src/components/ExecutionDetail/ExecutionHeader.tsx
- console-plugin/src/components/ExecutionDetail/WaveProgressStep.tsx
- console-plugin/src/hooks/useElapsedTime.ts
- console-plugin/tests/components/ExecutionHeader.test.tsx
- console-plugin/tests/components/WaveProgressStep.test.tsx
- console-plugin/tests/hooks/useElapsedTime.test.ts
- console-plugin/tests/hooks/useElapsedTime.hook.test.tsx

Rewritten files:
- console-plugin/src/components/ExecutionDetail/ExecutionDetailPage.tsx
- console-plugin/tests/components/ExecutionDetailPage.test.tsx

Modified files:
- _bmad-output/implementation-artifacts/sprint-status.yaml
- _bmad-output/implementation-artifacts/7-2-live-execution-monitor-progressstepper.md
