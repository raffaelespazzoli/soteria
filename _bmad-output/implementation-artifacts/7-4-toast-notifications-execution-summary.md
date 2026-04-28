# Story 7.4: Toast Notifications & Execution Summary

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want toast notifications for execution lifecycle events and a bridge-call-ready completion summary,
So that I stay informed and can report precise results to stakeholders.

## Acceptance Criteria

1. **AC1 — Execution started toast:** When an execution starts (detected via `useK8sWatchResource` watch on DRExecution resources), an info toast appears: "Failover started for {planName}" (auto-dismiss after 8 seconds). The toast includes a link to the execution monitor at `/disaster-recovery/executions/{name}`. (UX-DR13)

2. **AC2 — Execution succeeded toast:** When an execution completes with `result === 'Succeeded'`, a success toast appears: "Failover completed: {vmCount} VMs recovered in {duration}" (auto-dismiss after 15 seconds).

3. **AC3 — Execution partially succeeded toast:** When an execution completes with `result === 'PartiallySucceeded'`, a warning toast appears: "Failover partially succeeded: {failedCount} DRGroup failed — [View Details]" (persistent until dismissed). The "[View Details]" link navigates to the execution monitor. (UX-DR13)

4. **AC4 — Re-protect complete toast:** When a re-protect execution completes with `result === 'Succeeded'`, a success toast appears: "Re-protect complete: replication healthy" (auto-dismiss after 8 seconds).

5. **AC5 — Bridge-call-ready completion summary:** The execution monitor (`ExecutionDetailPage`) displays a completion summary section when the execution is finished. The summary uses `--pf-t--global--font--size--heading--h3` / `--pf-v5-global--FontSize--xl` with plain language: "{vmCount} VMs recovered in {duration}", "RPO: {rpoSeconds} seconds", "Result: Succeeded" (or "{successCount} of {totalCount} VMs recovered — {failedCount} DRGroup failed"). Designed to be read aloud on a bridge call. (UX-DR17)

6. **AC6 — Toast links:** Every toast includes a link to the relevant execution monitor page. Links use `react-router-dom` `Link` component.

7. **AC7 — Toast stacking:** Multiple simultaneous toasts stack correctly using PatternFly `AlertGroup` (toast variant). Maximum 4 visible toasts; oldest auto-dismiss first.

8. **AC8 — ARIA live regions:** Screen readers announce toast content via `AlertGroup`'s built-in ARIA live region support. jest-axe zero violations for all toast states and the completion summary.

9. **AC9 — Mode-aware messaging:** Toast messages use human-readable mode labels: "Failover" for `disaster`, "Planned Migration" for `planned_migration`, "Re-protect" for `reprotect`. Not raw mode strings.

10. **AC10 — Execution failed toast:** When an execution completes with `result === 'Failed'`, a danger toast appears: "{modeLabel} failed for {planName}" (persistent until dismissed).

## Tasks / Subtasks

- [ ] Task 1: Create toast notification store (AC: #1, #7)
  - [ ] 1.1 Create `src/notifications/toastStore.ts` — module-level singleton store with `addToast`, `removeToast`, `getToasts`, `subscribe` API
  - [ ] 1.2 Define `Toast` interface: `{ id: string, variant: AlertVariant, title: string, description?: string, linkText?: string, linkTo?: string, persistent: boolean, timeout: number }`
  - [ ] 1.3 Auto-dismiss logic: `setTimeout` per non-persistent toast, clear on remove
  - [ ] 1.4 Max 8 toasts in store; evict oldest non-persistent when exceeded

- [ ] Task 2: Create `useToastNotifications` hook (AC: #1, #7)
  - [ ] 2.1 Create `src/hooks/useToastNotifications.ts` — subscribes to `toastStore` via `useSyncExternalStore`
  - [ ] 2.2 Return `{ toasts: Toast[], removeToast: (id: string) => void }`
  - [ ] 2.3 Cleanup: unsubscribe on unmount

- [ ] Task 3: Create `ToastContainer` component (AC: #7, #8)
  - [ ] 3.1 Create `src/components/shared/ToastContainer.tsx` — renders PatternFly `AlertGroup` with `isToast` and `isLiveRegion` props
  - [ ] 3.2 Map each `Toast` to an `Alert` with correct variant, `actionClose` for dismiss, and optional `actionLinks` for navigation
  - [ ] 3.3 Use `Link` from `react-router-dom` for toast action links (wrapping in `AlertActionLink`); for programmatic navigation use `useHistory().push()` (RR v5)

- [ ] Task 4: Create `useExecutionNotifications` hook (AC: #1, #2, #3, #4, #9, #10)
  - [ ] 4.1 Create `src/hooks/useExecutionNotifications.ts` — watches DRExecution list via `useDRExecutions()`, detects lifecycle transitions, dispatches toasts
  - [ ] 4.2 Track previous execution states in a `useRef<Map<string, {result, startTime}>>` to detect transitions (new startTime = started, new result = completed)
  - [ ] 4.3 Execution started detection: execution appears with `startTime` but no `result`
  - [ ] 4.4 Execution completed detection: execution gains a `result` value (Succeeded / PartiallySucceeded / Failed)
  - [ ] 4.5 Re-protect detection: `spec.mode === 'reprotect'` + `result === 'Succeeded'` → special message
  - [ ] 4.6 Mode label mapping: `disaster` → "Failover", `planned_migration` → "Planned Migration", `reprotect` → "Re-protect"
  - [ ] 4.7 VM count derivation: count all `vmNames` across all waves/groups from `execution.status.waves[]`
  - [ ] 4.8 Failed group count: count groups with `result === 'Failed'` across all waves
  - [ ] 4.9 Duration formatting: reuse `formatDuration(startTime, completionTime)` from `src/utils/formatters.ts`
  - [ ] 4.10 Skip initial load: do not fire toasts for existing executions on first render (use a `useRef<boolean>` initialized flag)

- [ ] Task 5: Create `ExecutionSummary` component (AC: #5, #8)
  - [ ] 5.1 Create `src/components/ExecutionDetail/ExecutionSummary.tsx` — bridge-call-ready completion summary
  - [ ] 5.2 Render at `--pf-t--global--font--size--heading--h3` (or `--pf-v5-global--FontSize--xl` fallback) in plain language:
    - "{vmCount} VMs recovered in {duration}"
    - "RPO: {rpoSeconds} seconds"
    - "Result: {result}" or "{successCount} of {totalCount} VMs recovered — {failedCount} DRGroup failed"
  - [ ] 5.3 Use PatternFly `Card` with `isCompact` for visual grouping
  - [ ] 5.4 Only render when execution has `completionTime` (finished)

- [ ] Task 6: Integrate ToastContainer into all three pages (AC: #1, #7)
  - [ ] 6.1 Modify `src/components/DRDashboard/DRDashboardPage.tsx` — add `<ToastContainer />` and `useExecutionNotifications()`
  - [ ] 6.2 Modify `src/components/DRPlanDetail/DRPlanDetailPage.tsx` — add `<ToastContainer />` and `useExecutionNotifications()`
  - [ ] 6.3 Modify `src/components/ExecutionDetail/ExecutionDetailPage.tsx` — add `<ToastContainer />`, `useExecutionNotifications()`, and `<ExecutionSummary />` in the completed state

- [ ] Task 7: Write tests (AC: #8)
  - [ ] 7.1 Create `tests/notifications/toastStore.test.ts`:
    - addToast adds to store and notifies subscribers
    - removeToast removes and notifies
    - Auto-dismiss fires after timeout
    - Max toast eviction works
    - Subscribe/unsubscribe lifecycle
  - [ ] 7.2 Create `tests/hooks/useToastNotifications.test.ts`:
    - Returns current toasts from store
    - Updates when store changes
    - Cleanup on unmount
  - [ ] 7.3 Create `tests/hooks/useExecutionNotifications.test.ts`:
    - Fires info toast when execution starts
    - Fires success toast when execution succeeds
    - Fires warning toast (persistent) when partially succeeded
    - Fires danger toast (persistent) when failed
    - Fires re-protect-specific message for reprotect mode
    - Uses correct mode labels (Failover, Planned Migration, Re-protect)
    - Includes VM count and duration in success message
    - Includes failed group count in partial success message
    - Does NOT fire toasts on initial load
    - Includes link to execution monitor in all toasts
  - [ ] 7.4 Create `tests/components/ToastContainer.test.tsx`:
    - Renders AlertGroup with isToast
    - Renders correct Alert variants
    - Dismiss calls removeToast
    - Action link navigates correctly
    - jest-axe zero violations
  - [ ] 7.5 Create `tests/components/ExecutionSummary.test.tsx`:
    - Renders VM count and duration
    - Renders RPO in seconds
    - Renders "Succeeded" result
    - Renders partial failure count
    - Does not render when execution is active (no completionTime)
    - Uses xl font size token
    - jest-axe zero violations
  - [ ] 7.6 Update `tests/components/DRDashboardPage.test.tsx` — ToastContainer renders
  - [ ] 7.7 Verify all existing tests still pass (`yarn test`)
  - [ ] 7.8 Verify build succeeds (`yarn build`)

## Dev Notes

### Architectural Challenge — Cross-Page Toast Notifications in OCP Console Plugins

OCP Console plugins expose pages as independent webpack module federation entries (`console.page/route` + `$codeRef`). There is **no shared layout wrapper** that wraps all plugin pages. Each page is independently mounted/unmounted as the user navigates.

**Solution: Module-level singleton store.**

A module-level store (plain TypeScript, no React) persists across page navigations because webpack module federation keeps the module instance alive in memory. Each page mounts a `<ToastContainer />` that subscribes to the same store. When a toast is added on one page and the user navigates to another, the new page's `ToastContainer` immediately renders any active toasts.

```
Module-level (persists across navigation):
  toastStore.ts — { toasts[], addToast(), removeToast(), subscribe() }

Per-page (mounted/unmounted on navigation):
  <ToastContainer /> — subscribes to toastStore, renders AlertGroup
  useExecutionNotifications() — watches executions, calls addToast()
```

### Toast Store Design

Use `useSyncExternalStore` (React 18) for the hook binding. The store itself is plain TypeScript:

```typescript
interface Toast {
  id: string;
  variant: 'info' | 'success' | 'warning' | 'danger';
  title: string;
  description?: string;
  linkText?: string;
  linkTo?: string;
  persistent: boolean;
  timeout: number;
}

let toasts: Toast[] = [];
const listeners = new Set<() => void>();

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot(): Toast[] { return toasts; }

function addToast(toast: Omit<Toast, 'id'>): void {
  const id = `toast-${Date.now()}-${Math.random()}`;
  const entry = { ...toast, id };
  toasts = [...toasts, entry];
  if (toasts.length > 8) toasts = toasts.slice(-8);
  notify();
  if (!toast.persistent) {
    setTimeout(() => removeToast(id), toast.timeout);
  }
}

function removeToast(id: string): void {
  toasts = toasts.filter(t => t.id !== id);
  notify();
}

function notify() { listeners.forEach(l => l()); }
```

The hook:

```typescript
import { useSyncExternalStore } from 'react';

export function useToastNotifications() {
  const toasts = useSyncExternalStore(subscribe, getSnapshot);
  return { toasts, removeToast };
}
```

**Check React version:** The console-plugin template uses React 17 (per architecture.md). `useSyncExternalStore` requires React 18. If React 17, use the `use-sync-external-store` shim package OR implement with `useEffect` + `useState` + subscribe pattern:

```typescript
function useToastNotifications() {
  const [, forceUpdate] = useReducer(x => x + 1, 0);
  useEffect(() => {
    const unsub = subscribe(forceUpdate);
    return unsub;
  }, []);
  return { toasts: getSnapshot(), removeToast };
}
```

**Prefer the `useReducer` + subscribe approach** for React 17 compatibility. Check `package.json` dependencies for the actual React version.

### useExecutionNotifications — Transition Detection

The hook must distinguish between **initial load** (existing executions already in various states) and **runtime transitions** (an execution just started or just completed).

```typescript
function useExecutionNotifications(): void {
  const [executions, loaded] = useDRExecutions();
  const prevStates = useRef<Map<string, { result?: string; startTime?: string }>>(new Map());
  const initialized = useRef(false);

  useEffect(() => {
    if (!loaded || !executions) return;

    if (!initialized.current) {
      // Snapshot initial state — do NOT fire toasts
      executions.forEach(exec => {
        prevStates.current.set(exec.metadata?.name ?? '', {
          result: exec.status?.result,
          startTime: exec.status?.startTime,
        });
      });
      initialized.current = true;
      return;
    }

    executions.forEach(exec => {
      const name = exec.metadata?.name ?? '';
      const prev = prevStates.current.get(name);
      const curr = { result: exec.status?.result, startTime: exec.status?.startTime };

      if (!prev && curr.startTime && !curr.result) {
        // New execution detected — just started
        fireStartedToast(exec);
      } else if (prev && !prev.result && curr.result) {
        // Execution just completed
        fireCompletedToast(exec);
      }

      prevStates.current.set(name, curr);
    });
  }, [executions, loaded]);
}
```

### Mode Label Mapping

Reuse from Story 7.1's `drPlanActions.ts` if already implemented, otherwise define locally:

```typescript
const MODE_LABELS: Record<string, string> = {
  disaster: 'Failover',
  planned_migration: 'Planned Migration',
  reprotect: 'Re-protect',
};

function getModeLabel(mode: string): string {
  return MODE_LABELS[mode] ?? mode;
}
```

### VM Count Derivation from Execution Status

```typescript
function getVMCount(execution: DRExecution): number {
  return (execution.status?.waves ?? []).reduce(
    (total, wave) => total + wave.groups.reduce(
      (waveTotal, group) => waveTotal + (group.vmNames?.length ?? 0), 0
    ), 0
  );
}

function getFailedGroupCount(execution: DRExecution): number {
  return (execution.status?.waves ?? []).reduce(
    (total, wave) => total + wave.groups.filter(g => g.result === 'Failed').length, 0
  );
}
```

### Toast Message Templates

| Event | Variant | Title | Persistence | Timeout |
|-------|---------|-------|-------------|---------|
| Execution started | `info` | "{modeLabel} started for {planName}" | auto-dismiss | 8s |
| Succeeded | `success` | "{modeLabel} completed: {vmCount} VMs recovered in {duration}" | auto-dismiss | 15s |
| PartiallySucceeded | `warning` | "{modeLabel} partially succeeded: {failedCount} DRGroup failed" | persistent | — |
| Failed | `danger` | "{modeLabel} failed for {planName}" | persistent | — |
| Re-protect Succeeded | `success` | "Re-protect complete: replication healthy" | auto-dismiss | 8s |

All toasts include `linkText: "View execution"` and `linkTo: "/disaster-recovery/executions/{name}"`.

### ToastContainer — PatternFly AlertGroup (Toast)

```typescript
import { Alert, AlertActionCloseButton, AlertActionLink, AlertGroup } from '@patternfly/react-core';
import { Link } from 'react-router-dom';

const ToastContainer: React.FC = () => {
  const { toasts, removeToast } = useToastNotifications();

  return (
    <AlertGroup isToast isLiveRegion>
      {toasts.slice(0, 4).map(toast => (
        <Alert
          key={toast.id}
          variant={toast.variant}
          title={toast.title}
          actionClose={<AlertActionCloseButton onClose={() => removeToast(toast.id)} />}
          actionLinks={toast.linkTo ? (
            <AlertActionLink component={(props) => <Link {...props} to={toast.linkTo!} />}>
              {toast.linkText ?? 'View details'}
            </AlertActionLink>
          ) : undefined}
        >
          {toast.description}
        </Alert>
      ))}
    </AlertGroup>
  );
};
```

**Note on AlertActionLink:** In PatternFly 6, `AlertActionLink` accepts a `component` prop for custom link rendering. If this doesn't work in v6.2.2, use `onClick` with `useHistory().push()` from `react-router-dom` as a fallback (React Router v5 on OCP 4.20 — no `useNavigate`). Test the actual API.

### ExecutionSummary — Bridge-Call-Ready Component

```typescript
interface ExecutionSummaryProps {
  execution: DRExecution;
}

const ExecutionSummary: React.FC<ExecutionSummaryProps> = ({ execution }) => {
  if (!execution.status?.completionTime) return null;

  const vmCount = getVMCount(execution);
  const failedCount = getFailedGroupCount(execution);
  const duration = formatDuration(execution.status.startTime, execution.status.completionTime);
  const rpo = execution.status.rpoSeconds;
  const result = execution.status.result;

  const summaryStyle: React.CSSProperties = {
    fontSize: 'var(--pf-t--global--font--size--heading--h3, var(--pf-v5-global--FontSize--xl))',
    lineHeight: 1.5,
  };

  return (
    <Card isCompact>
      <CardBody>
        <div style={summaryStyle} role="region" aria-label="Execution summary">
          <div>{result === 'Succeeded'
            ? `${vmCount} VMs recovered in ${duration}`
            : `${vmCount - failedCount} of ${vmCount} VMs recovered — ${failedCount} DRGroup failed`
          }</div>
          {rpo != null && <div>RPO: {rpo} seconds</div>}
          <div>Result: <ExecutionResultBadge result={result!} /></div>
        </div>
      </CardBody>
    </Card>
  );
};
```

### Integration Points

**DRDashboardPage.tsx** — add toast infrastructure:

```typescript
import ToastContainer from '../shared/ToastContainer';
import { useExecutionNotifications } from '../../hooks/useExecutionNotifications';

function DRDashboardPage() {
  useExecutionNotifications();
  // ... existing code ...
  return (
    <>
      <ToastContainer />
      {/* ... existing JSX ... */}
    </>
  );
}
```

**DRPlanDetailPage.tsx** — same pattern.

**ExecutionDetailPage.tsx** — add toast + summary:

```typescript
import ToastContainer from '../shared/ToastContainer';
import ExecutionSummary from './ExecutionSummary';
import { useExecutionNotifications } from '../../hooks/useExecutionNotifications';

// After Story 7.2's implementation, add:
useExecutionNotifications();
// In the completed execution section:
{execution.status?.completionTime && <ExecutionSummary execution={execution} />}
// At the component root:
<ToastContainer />
```

### Preventing Duplicate Toasts on Page Navigation

When the user navigates from Dashboard to Plan Detail, both pages call `useExecutionNotifications()`. The old hook unmounts (clearing its `prevStates` ref) and the new hook mounts (initializing from scratch). The `initialized.current = false` guard ensures the new hook's first render snapshots existing state without firing toasts.

However, if an execution transitions **between** unmount and mount (unlikely but possible), the toast is missed. This is acceptable for v1.

If duplicate toasts are observed (same execution fires on both old and new page), add a **dedup check** in `addToast()`:

```typescript
function addToast(toast: Omit<Toast, 'id'>) {
  // Dedup: skip if a toast with same title exists and was added < 2s ago
  const duplicate = toasts.find(t => t.title === toast.title);
  if (duplicate) return;
  // ... rest of addToast
}
```

### Reuse from Existing Codebase

**From `src/utils/formatters.ts`:**
- `formatDuration(start, end)` — for duration display in toasts and summary
- `formatRPO(seconds)` — for RPO formatting (though the summary uses raw seconds per UX spec)

**From `src/hooks/useDRResources.ts`:**
- `useDRExecutions()` — watch all DRExecution resources for transition detection
- `useDRExecution(name)` — single execution watch for ExecutionDetailPage

**From `src/models/types.ts`:**
- `DRExecution`, `DRExecutionStatus`, `DRExecutionResult`, `ExecutionResult`, `WaveStatus`, `DRGroupExecutionStatus`
- All types needed already defined — no additions required

**From `src/components/shared/ExecutionResultBadge.tsx`:**
- Result badge rendering for the completion summary

**From Story 7.1 (when implemented):**
- Mode label mapping in `drPlanActions.ts` — reuse or import
- `K8sModel` for DRExecution — not needed for 7.4 (no writes)

**From Story 7.2 (when implemented):**
- `ExecutionHeader.tsx` — the summary component renders below the header
- `WaveProgressStep.tsx` — the summary needs completed wave/group data

### Non-Negotiable Constraints

- **PatternFly 6 ONLY** — `AlertGroup`, `Alert`, `AlertActionCloseButton`, `AlertActionLink`, `Card`, `CardBody` from `@patternfly/react-core`. No other UI libraries.
- **CSS custom properties only** — PF6 `--pf-t--global--*` tokens preferred (e.g. `--pf-t--global--font--size--heading--h3` for summary text); `--pf-v5-global--*` tokens still resolve as fallbacks. No hardcoded values.
- **Console SDK hooks only** — `useK8sWatchResource` (via `useDRExecutions`) for data. No direct API calls, no polling.
- **Imports from `react-router-dom`** — `Link`, `useHistory` from `react-router-dom` (React Router v5 on OCP 4.20). Test mocks use `jest.mock('react-router', ...)` at the mock layer.
- **No external state libraries** — module-level singleton store. No Redux, Zustand, MobX.
- **No separate CSS files** — inline styles with PatternFly tokens.
- **Default exports** on page components — required by Console SDK `$codeRef`.
- **React 17 compatibility** — do NOT use `useSyncExternalStore` unless you verify React 18. Use `useReducer` + `subscribe` pattern.

### What NOT to Do

- **Do NOT use the Console SDK `useNotify`** — it doesn't exist in the current SDK. Build the toast system with PatternFly `AlertGroup`.
- **Do NOT create a React Context provider** wrapping routes — Console plugins don't have a shared layout wrapper. Use the module-level singleton pattern.
- **Do NOT poll for execution updates** — use the existing `useDRExecutions()` watch hook. It delivers real-time updates.
- **Do NOT fire toasts for existing executions on initial page load** — only fire for runtime transitions detected via state comparison.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT implement toast persistence across browser refreshes** — toasts are ephemeral and live in memory only.
- **Do NOT use `window.alert` or `console.log` for notifications** — only PatternFly `AlertGroup` toasts.
- **Do NOT implement a notification drawer or history** — only real-time toasts. A notification history feature would be a separate story.
- **Do NOT re-implement the execution header** — `ExecutionSummary` is a new component rendered below the header, not a replacement.
- **Do NOT add new PatternFly dependencies** — `AlertGroup`, `Alert`, `Card`, `CardBody` are all in `@patternfly/react-core` ^6.2.2. `AlertActionCloseButton` and `AlertActionLink` are also in `@patternfly/react-core`.

### Testing Approach

**toastStore tests** (pure unit, no React):

```typescript
import { addToast, removeToast, getSnapshot, subscribe, resetForTesting } from '../../src/notifications/toastStore';

beforeEach(() => resetForTesting());

describe('toastStore', () => {
  it('adds a toast and notifies subscribers', () => {
    const listener = jest.fn();
    subscribe(listener);
    addToast({ variant: 'info', title: 'Test', persistent: false, timeout: 8000 });
    expect(listener).toHaveBeenCalled();
    expect(getSnapshot()).toHaveLength(1);
  });

  it('removes a toast', () => { ... });

  it('auto-dismisses non-persistent toast after timeout', () => {
    jest.useFakeTimers();
    addToast({ variant: 'info', title: 'Test', persistent: false, timeout: 8000 });
    expect(getSnapshot()).toHaveLength(1);
    jest.advanceTimersByTime(8000);
    expect(getSnapshot()).toHaveLength(0);
  });

  it('does not auto-dismiss persistent toast', () => {
    jest.useFakeTimers();
    addToast({ variant: 'warning', title: 'Persist', persistent: true, timeout: 0 });
    jest.advanceTimersByTime(60000);
    expect(getSnapshot()).toHaveLength(1);
  });

  it('evicts oldest when exceeding max', () => { ... });
});
```

**useExecutionNotifications tests:**

```typescript
jest.mock('../../src/hooks/useDRResources', () => ({
  useDRExecutions: jest.fn(),
}));

import { useDRExecutions } from '../../src/hooks/useDRResources';
import * as toastStore from '../../src/notifications/toastStore';

describe('useExecutionNotifications', () => {
  it('does NOT fire toasts on initial load', () => {
    (useDRExecutions as jest.Mock).mockReturnValue([[mockActiveExecution], true]);
    const spy = jest.spyOn(toastStore, 'addToast');
    renderHook(() => useExecutionNotifications());
    expect(spy).not.toHaveBeenCalled();
  });

  it('fires info toast when new execution starts', () => {
    const { rerender } = renderHook(() => useExecutionNotifications());
    // First render — initial snapshot (empty)
    (useDRExecutions as jest.Mock).mockReturnValue([[], true]);
    rerender();
    // Second render — new execution appears
    (useDRExecutions as jest.Mock).mockReturnValue([[mockStartedExecution], true]);
    rerender();
    expect(toastStore.addToast).toHaveBeenCalledWith(expect.objectContaining({ variant: 'info' }));
  });

  it('fires success toast when execution succeeds', () => { ... });
  it('fires warning toast (persistent) when partially succeeded', () => { ... });
  it('fires danger toast (persistent) when failed', () => { ... });
  it('fires re-protect-specific message', () => { ... });
  it('uses correct mode labels', () => { ... });
  it('includes VM count and duration in success message', () => { ... });
  it('includes link to execution monitor', () => { ... });
});
```

**ToastContainer tests:**

```typescript
import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import ToastContainer from '../../src/components/shared/ToastContainer';

expect.extend(toHaveNoViolations);

// Mock the toast store or inject test toasts
jest.mock('../../src/hooks/useToastNotifications', () => ({
  useToastNotifications: () => ({
    toasts: [
      { id: '1', variant: 'success', title: 'Failover completed: 12 VMs in 17m', persistent: false, timeout: 15000, linkTo: '/disaster-recovery/executions/test-1', linkText: 'View execution' },
    ],
    removeToast: jest.fn(),
  }),
}));

describe('ToastContainer', () => {
  it('renders AlertGroup with isToast', () => {
    render(<MemoryRouter><ToastContainer /></MemoryRouter>);
    expect(screen.getByText(/Failover completed/)).toBeInTheDocument();
  });

  it('dismiss calls removeToast', async () => { ... });
  it('action link navigates correctly', () => { ... });

  it('passes jest-axe', async () => {
    const { container } = render(<MemoryRouter><ToastContainer /></MemoryRouter>);
    expect(await axe(container)).toHaveNoViolations();
  });
});
```

**ExecutionSummary tests:**

```typescript
import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ExecutionSummary from '../../src/components/ExecutionDetail/ExecutionSummary';

expect.extend(toHaveNoViolations);

const mockSucceeded: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution',
  metadata: { name: 'test-1', uid: '1' },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    result: 'Succeeded',
    startTime: new Date(Date.now() - 17 * 60000).toISOString(),
    completionTime: new Date().toISOString(),
    rpoSeconds: 47,
    waves: [
      { waveIndex: 0, groups: [
        { name: 'g1', result: 'Completed', vmNames: ['vm1', 'vm2'] },
        { name: 'g2', result: 'Completed', vmNames: ['vm3'] },
      ]},
      { waveIndex: 1, groups: [
        { name: 'g3', result: 'Completed', vmNames: ['vm4', 'vm5', 'vm6'] },
      ]},
    ],
  },
};

describe('ExecutionSummary', () => {
  it('renders VM count and duration', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByText(/6 VMs recovered/)).toBeInTheDocument();
  });

  it('renders RPO in seconds', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByText(/RPO: 47 seconds/)).toBeInTheDocument();
  });

  it('does not render when execution is active', () => {
    const active = { ...mockSucceeded, status: { ...mockSucceeded.status, completionTime: undefined, result: undefined } };
    const { container } = render(<ExecutionSummary execution={active as DRExecution} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders partial failure count', () => {
    const partial = {
      ...mockSucceeded,
      status: {
        ...mockSucceeded.status,
        result: 'PartiallySucceeded',
        waves: [
          { waveIndex: 0, groups: [
            { name: 'g1', result: 'Completed', vmNames: ['vm1', 'vm2'] },
            { name: 'g2', result: 'Failed', vmNames: ['vm3'], error: 'timeout' },
          ]},
        ],
      },
    };
    render(<ExecutionSummary execution={partial as DRExecution} />);
    expect(screen.getByText(/2 of 3 VMs recovered/)).toBeInTheDocument();
    expect(screen.getByText(/1 DRGroup failed/)).toBeInTheDocument();
  });

  it('passes jest-axe', async () => {
    const { container } = render(<ExecutionSummary execution={mockSucceeded} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
```

### Mock Data Patterns

Extend existing mock patterns from Story 7.2/7.3 test files:

```typescript
const mockStartedExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution',
  metadata: { name: 'erp-failover-1714327200000', uid: '1', labels: { 'soteria.io/plan-name': 'erp-full-stack' } },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    startTime: new Date().toISOString(),
    waves: [],
  },
};

const mockCompletedExecution: DRExecution = {
  ...mockStartedExecution,
  status: {
    result: 'Succeeded',
    startTime: new Date(Date.now() - 17 * 60000).toISOString(),
    completionTime: new Date().toISOString(),
    rpoSeconds: 47,
    waves: [
      { waveIndex: 0, groups: [
        { name: 'drgroup-1', result: 'Completed', vmNames: ['erp-db-1', 'erp-db-2'], startTime: '...', completionTime: '...' },
      ]},
      { waveIndex: 1, groups: [
        { name: 'drgroup-2', result: 'Completed', vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'], startTime: '...', completionTime: '...' },
      ]},
    ],
  },
};

const mockReprotectExecution: DRExecution = {
  ...mockStartedExecution,
  spec: { planName: 'erp-full-stack', mode: 'reprotect' },
  status: {
    result: 'Succeeded',
    startTime: new Date(Date.now() - 5 * 60000).toISOString(),
    completionTime: new Date().toISOString(),
    rpoSeconds: 0,
    waves: [],
  },
};
```

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   └── DRDashboardPage.tsx           # MODIFIED — add ToastContainer + useExecutionNotifications
│   ├── DRPlanDetail/
│   │   └── DRPlanDetailPage.tsx          # MODIFIED — add ToastContainer + useExecutionNotifications
│   ├── ExecutionDetail/
│   │   ├── ExecutionDetailPage.tsx       # MODIFIED — add ToastContainer + useExecutionNotifications + ExecutionSummary
│   │   └── ExecutionSummary.tsx          # NEW — bridge-call-ready completion summary
│   └── shared/
│       └── ToastContainer.tsx            # NEW — PatternFly AlertGroup toast renderer
├── hooks/
│   ├── useToastNotifications.ts          # NEW — subscribe to toast store
│   ├── useExecutionNotifications.ts      # NEW — execution lifecycle transition detection
│   └── ... (unchanged)
├── notifications/
│   └── toastStore.ts                     # NEW — module-level singleton toast store
└── ... (unchanged)
```

**Test files:**
```
console-plugin/tests/
├── notifications/
│   └── toastStore.test.ts                # NEW — store unit tests
├── hooks/
│   ├── useToastNotifications.test.ts     # NEW — hook subscription tests
│   └── useExecutionNotifications.test.ts # NEW — transition detection tests
├── components/
│   ├── ToastContainer.test.tsx           # NEW — AlertGroup rendering tests
│   ├── ExecutionSummary.test.tsx         # NEW — bridge-call summary tests
│   └── DRDashboardPage.test.tsx          # MODIFIED — verify ToastContainer renders
└── ... (unchanged)
```

### Project Structure Notes

- `src/notifications/toastStore.ts` is a new directory — plain TypeScript module, no React dependency. This separation is intentional: the store must work as a module-level singleton independent of React lifecycle.
- `ToastContainer.tsx` lives in `shared/` — it's used by all three page components.
- `ExecutionSummary.tsx` lives in `ExecutionDetail/` — it's specific to the execution monitor page.
- `useExecutionNotifications.ts` goes in `hooks/` — follows the existing hook pattern.
- No changes to `console-extensions.json` — no new routes or extensions.
- No new CRD types — all data comes from existing `DRExecution.status` structure.
- No new PatternFly dependencies — `AlertGroup`, `Alert`, `AlertActionCloseButton`, `AlertActionLink`, `Card`, `CardBody` are all in `@patternfly/react-core` ^6.2.2.

### References

- [Source: _bmad-output/planning-artifacts/epics.md § Story 7.4] — Acceptance criteria, user story, UX-DR13, UX-DR17 references
- [Source: _bmad-output/planning-artifacts/epics.md § UX-DR13] — Toast notification system: AlertGroup with context-appropriate persistence
- [Source: _bmad-output/planning-artifacts/epics.md § UX-DR17] — Screen-share optimization: min 14px, 18px+ critical, monospace times
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Feedback Patterns] — Status change notification table (event, feedback type, persistence, content)
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Implementation Approach] — PatternFly component mapping: "Notification toasts → AlertGroup (toast)"
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Alert Banner Color Semantics] — Info (dismissible), warning/danger (persistent) pattern
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Bridge-Call Ready principle] — Plain language, visual progress, not technical codes
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Typography] — Bridge-call summary at `--pf-v5-global--FontSize--xl`, monospace for times
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Screen-Share Optimization] — 14px min, 18px+ critical, high contrast, no hover-only
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 6, webpack module federation
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK for data, PatternFly only, no state libraries
- [Source: console-plugin/src/models/types.ts] — DRExecution, DRExecutionStatus, DRExecutionResult, WaveStatus, Condition
- [Source: console-plugin/src/hooks/useDRResources.ts] — useDRExecutions, useDRExecution watch hooks
- [Source: console-plugin/src/utils/formatters.ts] — formatDuration, formatRPO, formatRelativeTime
- [Source: console-plugin/src/components/shared/ExecutionResultBadge.tsx] — Result badge for summary
- [Source: console-plugin/console-extensions.json] — 3 page routes, no layout wrapper
- [Source: console-plugin/package.json § exposedModules] — DRDashboardPage, DRPlanDetailPage, ExecutionDetailPage

### Previous Story Intelligence

**Story 7.3 (Inline Error Display & DRGroup Retry) established:**
- `FailedGroupDetail.tsx` — error detail with `ExpandableSection` in `WaveProgressStep`
- `useRetryDRGroup.ts` — `k8sPatch` annotation pattern for DRExecution writes
- `RetryRejected` condition detection from `execution.status.conditions[]`
- Failed group count derivation from waves/groups — **reuse this pattern for toast messages**
- `ExclamationCircleIcon` red danger styling for failed states
- DRExecution model reuse from Story 7.1's `useCreateDRExecution.ts`
- Concurrent retry prevention pattern (disable buttons during in-progress)

**Story 7.2 (Live Execution Monitor — ProgressStepper) established:**
- `ExecutionDetailPage.tsx` rewritten as full execution monitor with vertical `ProgressStepper`
- `ExecutionHeader.tsx` — execution name, mode, elapsed time, ETA, result badge
- `WaveProgressStep.tsx` — per-wave `ProgressStep` with expandable DRGroup detail
- `useElapsedTime.ts` hook — 1s tick counter
- Wave state derivation: `getWaveState()` from start/completion/failed groups
- Mode labels: `disaster` → "Disaster Failover", `planned_migration` → "Planned Migration", etc.
- `useDRExecution(name)` for single execution watch
- `DRBreadcrumb` with planName from `spec.planName`
- Monospace time display: `fontFamily: 'var(--pf-t--global--font--family--mono)'` (or `--pf-v5-global--FontFamily--monospace` fallback)
- `aria-live="polite"` for wave transition announcements

**Story 7.1 (Pre-flight Confirmation & Failover Trigger) established:**
- `k8sCreate` DRExecution pattern — first write operation in Console plugin
- `K8sModel` for DRExecution defined in `useCreateDRExecution.ts`
- `usePreflightData` hook for plan metadata derivation
- `drPlanActions.ts` with action-to-mode mapping and mode labels
- Inline `Alert` error display pattern within modal
- Console SDK write operations: loading/error state management with `useState`

**Story 6.6 (Status Badges, Empty States & Accessibility) established:**
- jest-axe pattern: `expect(await axe(container)).toHaveNoViolations()`
- Keyboard testing: `userEvent.setup()` + `userEvent.tab()`
- `react-router` mock: `jest.mock('react-router', ...)` with `jest.requireActual`

**Story 6.4 (Alert Banner System) established:**
- `AlertBannerSystem` component — PatternFly `Alert` (inline, persistent)
- Pattern for persistent vs dismissible alerts
- `AlertActionLink` with filter wiring — **similar pattern for toast action links**

### Git Intelligence

Recent commits (last 10):
- `31cc201` — Implement Story 6.6: Status badges, empty states & accessibility
- `624d650` — Implement Story 6.5b: Waves, History & Configuration tabs
- `4eb1b98` — Implement Story 6.5: Plan Detail Shell & Overview Tab
- `09bf6d9` — Implement Story 6.4: Alert Banner System
- `a0e873a` — Implement Story 6.3: DR Dashboard Table & Toolbar
- `826fae3` — Implement Story 6.2: Console plugin navigation & routing
- `1cc02b1` — Implement Story 6.1: Console plugin project initialization
- `9824b1f` — Fix DRExecution integration test failures
- `09a0674` — Add Epic 6 story files, wireframes, and planning updates
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings

All Epic 6 stories complete. Stories 7.1, 7.2, 7.3 are ready-for-dev. This story (7.4) is the final story in Epic 7. It adds ~2 new source components, ~1 new hook, ~1 new store module, modifies ~3 existing page files, and creates ~5 new test files + modifies ~1 existing test file.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
