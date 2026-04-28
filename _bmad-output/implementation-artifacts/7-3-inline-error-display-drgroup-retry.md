# Story 7.3: Inline Error Display & DRGroup Retry

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want failed DRGroups highlighted with the error message and a Retry button inline in the execution monitor,
So that I can recover from failures without leaving the view.

## Acceptance Criteria

1. **AC1 — Failed DRGroup error display:** When a DRGroup has failed during execution and the execution monitor renders, the failed DRGroup shows: red text/icon (`ExclamationCircleIcon` with `--pf-t--global--icon--color--status--danger--default` / `--pf-v5-global--danger-color--100`), error message inline, affected VM names, and the step where failure occurred. (FR39)

2. **AC2 — Retry button on failed DRGroup:** A "Retry" button (PatternFly `Button`, `variant="primary"`) appears inline next to the failed DRGroup in the execution monitor. The button is only shown when the DRExecution has `result === 'PartiallySucceeded'` (execution must be complete — not during active execution).

3. **AC3 — Retry triggers annotation patch:** When the operator clicks Retry, the Console plugin patches the DRExecution resource to add the `soteria.io/retry-groups` annotation with the failed group name as the value. This triggers the backend reconciler to validate preconditions and re-execute the group.

4. **AC4 — Retry in-progress state:** After clicking Retry, the DRGroup status transitions to InProgress (blue `Spinner` indicator) as observed through the existing `useK8sWatchResource` watch on the DRExecution. No separate polling is needed — the watch stream delivers updates.

5. **AC5 — Retry precondition failure:** If the backend rejects the retry (sets a `RetryRejected` condition on the DRExecution), an inline error message appears near the Retry button: the condition's `message` field content (e.g., "VM erp-db-1 is in an unpredictable state — manual intervention required"). The Retry button remains visible.

6. **AC6 — Successful retry updates result:** When a retried DRGroup completes successfully, its status changes from Failed → InProgress → Completed (green `CheckCircleIcon`). If all DRGroups are now Completed, the DRExecution result updates from PartiallySucceeded to Succeeded (observed via watch).

7. **AC7 — Retry All Failed button:** When multiple DRGroups are failed, a "Retry All Failed" button appears in the `ExecutionHeader` area (above the ProgressStepper). This patches the annotation with `"all-failed"` as the value.

8. **AC8 — Keyboard accessibility:** The Retry button is focusable via Tab from the failed DRGroup context. Screen readers announce "Retry {groupName}" for each Retry button. jest-axe zero violations for all states (failed with retry, retrying, retry rejected).

9. **AC9 — Error detail expandable section:** The error display within a failed DRGroup is rendered in a PatternFly `ExpandableSection` that shows: the error message (from `DRGroupExecutionStatus.error`), the failed step name (from last `StepStatus` entry), and the `retryCount` if > 0 (showing "Previously retried N times"). Auto-expanded when the group first fails.

10. **AC10 — Retry button disabled during active retry:** While any DRGroup is InProgress (retry in progress), all Retry buttons are disabled with a tooltip "Retry in progress — wait for current retry to complete". This prevents concurrent retry annotations.

## Tasks / Subtasks

- [ ] Task 1: Create `FailedGroupDetail` component (AC: #1, #9)
  - [ ] 1.1 Create `src/components/ExecutionDetail/FailedGroupDetail.tsx` — renders error info for a failed DRGroup within the `WaveProgressStep` expanded section
  - [ ] 1.2 Display: `ExclamationCircleIcon` (red), group name, error message from `group.error`, affected VM names from `group.vmNames`
  - [ ] 1.3 Show failed step: derive from last entry in `group.steps[]` where `status !== 'Completed'`
  - [ ] 1.4 Show retry count: "Previously retried {retryCount} time(s)" when `retryCount > 0`
  - [ ] 1.5 Use PatternFly `ExpandableSection` — auto-expanded when `group.result === 'Failed'`

- [ ] Task 2: Create `useRetryDRGroup` hook (AC: #3, #5, #10)
  - [ ] 2.1 Create `src/hooks/useRetryDRGroup.ts` — wraps `k8sPatch` from Console SDK to add `soteria.io/retry-groups` annotation
  - [ ] 2.2 Accept execution name + group name (or `'all-failed'`); build JSON merge patch: `{ metadata: { annotations: { 'soteria.io/retry-groups': groupName } } }`
  - [ ] 2.3 Return `{ retry, retryAll, isRetrying, retryError }` tuple
  - [ ] 2.4 Track `isRetrying` state (set true on patch, cleared when watch shows group InProgress or RetryRejected condition appears)
  - [ ] 2.5 Extract `RetryRejected` condition message from `execution.status.conditions[]` for error display

- [ ] Task 3: Add Retry button to `WaveProgressStep` (AC: #2, #4, #8, #10)
  - [ ] 3.1 Modify `src/components/ExecutionDetail/WaveProgressStep.tsx` — add Retry button inline for failed DRGroups when execution `result === 'PartiallySucceeded'`
  - [ ] 3.2 Render `<Button variant="primary" size="sm" onClick={() => retry(groupName)}>Retry</Button>` next to `FailedGroupDetail`
  - [ ] 3.3 Disable all Retry buttons when any group has `result === 'InProgress'` (retry in progress) with `Tooltip` message
  - [ ] 3.4 Add `aria-label="Retry {groupName}"` for screen reader

- [ ] Task 4: Add "Retry All Failed" to ExecutionHeader (AC: #7)
  - [ ] 4.1 Modify `src/components/ExecutionDetail/ExecutionHeader.tsx` — add "Retry All Failed" button when `result === 'PartiallySucceeded'` and multiple groups failed
  - [ ] 4.2 Button calls `retryAll()` from `useRetryDRGroup` hook (patches with `'all-failed'`)
  - [ ] 4.3 Disable when any group is InProgress
  - [ ] 4.4 Hide when only one group failed (single Retry button suffices)

- [ ] Task 5: Wire retry into ExecutionDetailPage (AC: #3, #5)
  - [ ] 5.1 Modify `src/components/ExecutionDetail/ExecutionDetailPage.tsx` — instantiate `useRetryDRGroup(executionName)` hook
  - [ ] 5.2 Pass `retry`, `retryAll`, `isRetrying`, `retryError` down to `WaveProgressStep` and `ExecutionHeader`
  - [ ] 5.3 Display `retryError` (from RetryRejected condition) inline near the failed group that was retried

- [ ] Task 6: Enhance error display in existing DRGroup rendering (AC: #1)
  - [ ] 6.1 In `WaveProgressStep`, replace plain error text with `FailedGroupDetail` component for failed groups
  - [ ] 6.2 Ensure existing DRGroup status icon rendering (Pending/InProgress/Completed/Failed) is preserved
  - [ ] 6.3 Add `FailedGroupDetail` below the status icon + name for failed groups only

- [ ] Task 7: Write tests (AC: #8)
  - [ ] 7.1 Create `tests/components/FailedGroupDetail.test.tsx`:
    - Renders error message from group.error
    - Shows affected VM names
    - Shows failed step name
    - Shows retry count when > 0
    - Does not show retry count when 0 or undefined
    - ExpandableSection auto-expanded for failed groups
    - jest-axe zero violations
  - [ ] 7.2 Create `tests/hooks/useRetryDRGroup.test.ts`:
    - Calls k8sPatch with correct annotation for single group
    - Calls k8sPatch with 'all-failed' for retry all
    - Returns isRetrying true while patch in flight
    - Extracts RetryRejected condition message
    - Clears retry error on new retry attempt
  - [ ] 7.3 Update `tests/components/WaveProgressStep.test.tsx`:
    - Retry button visible for failed groups when result is PartiallySucceeded
    - Retry button NOT visible during active execution (no result yet)
    - Retry button disabled when a group is InProgress (concurrent retry prevention)
    - Tooltip shown on disabled retry button
    - Retry button has correct aria-label
    - jest-axe zero violations for retry states
  - [ ] 7.4 Update `tests/components/ExecutionHeader.test.tsx`:
    - "Retry All Failed" button shown when multiple groups failed and result is PartiallySucceeded
    - "Retry All Failed" button hidden when only one group failed
    - "Retry All Failed" disabled when retry in progress
  - [ ] 7.5 Update `tests/components/ExecutionDetailPage.test.tsx`:
    - PartiallySucceeded execution renders retry buttons
    - Retry rejected condition displays inline error
  - [ ] 7.6 Verify all existing tests still pass (`yarn test`)
  - [ ] 7.7 Verify build succeeds (`yarn build`)

## Dev Notes

### This Story Adds Write Operations to the Execution Monitor

Story 7.2 built a read-only execution monitor. Story 7.3 adds the second write operation to the Console plugin (after 7.1's `k8sCreate`): patching the `soteria.io/retry-groups` annotation on DRExecution resources via `k8sPatch`.

### Backend Retry Mechanism — Annotation-Driven

The backend retry mechanism is **annotation-driven**, not a separate API endpoint. The DRExecution reconciler in `pkg/controller/drexecution/reconciler.go` works as follows:

1. When a DRExecution has `result === 'PartiallySucceeded'`, the reconciler checks for the `soteria.io/retry-groups` annotation
2. The annotation value is either a comma-separated list of group names (e.g., `"drgroup-3,drgroup-5"`) or the sentinel `"all-failed"` to retry every failed group
3. The reconciler validates: execution must be PartiallySucceeded, no groups already InProgress, all VMs in retry groups pass health checks
4. On successful validation: groups are re-executed wave-ordered, annotation is removed automatically
5. On validation failure: a `RetryRejected` condition is set on the DRExecution status with a descriptive message, annotation is removed

The Console plugin's role is simply to **set the annotation** — the reconciler handles all validation and execution.

```typescript
const RetryGroupsAnnotation = 'soteria.io/retry-groups';
const RetryAllFailed = 'all-failed';
```

### Console SDK `k8sPatch` for Annotation Patching

The Console SDK `@openshift-console/dynamic-plugin-sdk` exports `k8sPatch` for patching Kubernetes resources. This is the first PATCH operation in the Console plugin.

```typescript
import { k8sPatch, K8sModel } from '@openshift-console/dynamic-plugin-sdk';

const drExecutionModel: K8sModel = {
  apiGroup: 'soteria.io',
  apiVersion: 'v1alpha1',
  kind: 'DRExecution',
  abbr: 'DRE',
  label: 'DRExecution',
  labelPlural: 'DRExecutions',
  plural: 'drexecutions',
  namespaced: false,
};

await k8sPatch({
  model: drExecutionModel,
  resource: { metadata: { name: executionName } },
  data: [
    {
      op: 'add',
      path: '/metadata/annotations/soteria.io~1retry-groups',
      value: groupName,  // or 'all-failed'
    },
  ],
});
```

**Important:** The `k8sPatch` uses JSON Patch (RFC 6902) format. The annotation key `soteria.io/retry-groups` must be escaped as `soteria.io~1retry-groups` in the JSON Patch path (forward slash is encoded as `~1`).

If the execution has no `annotations` map yet, the patch needs to handle the case where `/metadata/annotations` doesn't exist. Use a two-step approach: first add the annotations object if missing, then add the key. Or use a strategic merge patch approach if the SDK supports it.

**Alternative approach using strategic merge patch:**

```typescript
await k8sPatch({
  model: drExecutionModel,
  resource: { metadata: { name: executionName } },
  data: [
    {
      op: 'add',
      path: '/metadata/annotations',
      value: { 'soteria.io/retry-groups': groupName },
    },
  ],
});
```

Test both approaches — the exact API depends on the Console SDK version. The `K8sModel` for DRExecution was already defined in Story 7.1's `useCreateDRExecution.ts` — **reuse it**.

### `useRetryDRGroup` Hook Design

```typescript
interface UseRetryDRGroupResult {
  retry: (groupName: string) => Promise<void>;
  retryAll: () => Promise<void>;
  isRetrying: boolean;
  retryError: string | null;
}

function useRetryDRGroup(
  executionName: string,
  execution: DRExecution | null,
): UseRetryDRGroupResult
```

The hook:
1. Patches the `soteria.io/retry-groups` annotation via `k8sPatch`
2. Tracks `isRetrying` state (true from patch call until watch delivers InProgress group or RetryRejected condition)
3. Monitors `execution.status.conditions[]` for a `RetryRejected` condition — extracts the `message` field as `retryError`
4. Clears `retryError` on new retry attempt

### Determining When to Show Retry Buttons

Retry buttons appear **only** when:
- `execution.status.result === 'PartiallySucceeded'` — execution has completed with some failures
- The specific DRGroup has `result === 'Failed'`

Retry buttons are **NOT shown** when:
- Execution is still active (no `result` yet) — groups may still be processing
- Execution `result === 'Succeeded'` — nothing to retry
- Execution `result === 'Failed'` — terminal failure, retry not supported (reconciler will reject)

### Detecting RetryRejected Condition

The backend sets a `RetryRejected` condition when retry preconditions fail:

```typescript
function getRetryRejectedMessage(execution: DRExecution): string | null {
  const condition = execution.status?.conditions?.find(
    c => c.type === 'RetryRejected' && c.status === 'True'
  );
  return condition?.message ?? null;
}
```

Display this message inline near the failed group that was retried. The condition includes details like "VM erp-db-1 is in an unpredictable state — manual intervention required."

### Concurrent Retry Prevention

The backend already guards against concurrent retries (rejects if any group is InProgress). The frontend should additionally:
1. Disable all Retry buttons when `isRetrying` is true
2. Disable all Retry buttons when any `group.result === 'InProgress'` (detected via watch)
3. Show a Tooltip on disabled buttons: "Retry in progress — wait for current retry to complete"

### FailedGroupDetail Component Design

```typescript
interface FailedGroupDetailProps {
  group: DRGroupExecutionStatus;
  onRetry?: () => void;
  isRetryDisabled: boolean;
  retryTooltip?: string;
  retryError?: string | null;
  showRetryButton: boolean;
}
```

Layout within the `WaveProgressStep` expanded section:

```
❌ drgroup-3 (erp-app-1, erp-app-2, erp-app-3)  Failed  2m 15s
  ┌──────────────────────────────────────────────────┐
  │ Error: storage driver timeout on SetSource       │
  │ Failed step: StopReplication                     │
  │ Previously retried 1 time                        │
  │                                                  │
  │ [Retry]  ← primary button, inline                │
  │                                                  │
  │ ⚠ Cannot retry — VM erp-app-1 is in an          │  ← only if retryError
  │   unpredictable state. Manual intervention       │
  │   required.                                      │
  └──────────────────────────────────────────────────┘
```

Use PatternFly components:
- `ExpandableSection` wrapping the error detail
- `Alert` (variant="danger", isInline, isPlain) for the retry rejected error
- `Button` (variant="primary", size="sm") for Retry
- `Tooltip` wrapping the button when disabled

### Reuse from Existing Codebase

**From Story 7.1 (`src/hooks/useCreateDRExecution.ts`):**
- `K8sModel` definition for DRExecution (apiGroup, apiVersion, kind, plural, namespaced: false) — reuse, do not duplicate
- Extract the `drExecutionModel` constant to a shared location (e.g., `src/models/k8sModels.ts`) or import from the existing hook file

**From Story 7.2 (`src/components/ExecutionDetail/WaveProgressStep.tsx`):**
- DRGroup rendering with status icons (Pending/InProgress/Completed/Failed)
- `ExpandableSection` pattern for DRGroup detail within waves
- DRGroup status icon mapping (color tokens + icons)
- `getWaveState()` wave state derivation function

**From Story 7.2 (`src/components/ExecutionDetail/ExecutionHeader.tsx`):**
- Execution metadata header — extend with "Retry All Failed" button
- `ExecutionResultBadge` reuse for result display

**From Story 7.2 (`src/components/ExecutionDetail/ExecutionDetailPage.tsx`):**
- `useDRExecution(name)` watch — already provides real-time updates
- Page shell with breadcrumb and loading/error states

**From Story 6.6 (accessibility patterns):**
- jest-axe pattern: `const { container } = render(<Component />); expect(await axe(container)).toHaveNoViolations()`
- Keyboard testing with `userEvent.setup()` + `userEvent.tab()`
- react-router mock: `jest.mock('react-router', ...)` with `jest.requireActual`

**From Story 6.3 (`src/utils/formatters.ts`):**
- `formatDuration(start, end)` — elapsed time display for failed groups

**From `src/models/types.ts`:**
- `DRExecution`, `DRExecutionStatus`, `DRGroupExecutionStatus`, `StepStatus`, `DRGroupResult`, `DRGroupResultValue`, `ExecutionResult`, `Condition`
- `DRGroupExecutionStatus.error`, `DRGroupExecutionStatus.steps`, `DRGroupExecutionStatus.retryCount` — all fields needed for error display already in the type definitions

### Deriving the Failed Step

Extract the failed step from `DRGroupExecutionStatus.steps[]`:

```typescript
function getFailedStep(group: DRGroupExecutionStatus): string | null {
  if (!group.steps?.length) return null;
  const failedStep = group.steps.find(s => s.status === 'Failed');
  if (failedStep) return failedStep.name;
  const lastStep = group.steps[group.steps.length - 1];
  return lastStep.status !== 'Completed' ? lastStep.name : null;
}
```

The backend step names are human-readable: `"StopReplication"`, `"StartVM"`, `"WaitVMReady"`, etc. Display them directly.

### Non-Negotiable Constraints

- **PatternFly 6 ONLY** — `Button`, `ExpandableSection`, `Alert`, `Tooltip`, `Spinner` from `@patternfly/react-core`. Icons from `@patternfly/react-icons`. No other UI libraries.
- **CSS custom properties only** — PF6 `--pf-t--global--*` tokens preferred; `--pf-v5-global--*` tokens still resolve as fallbacks. No hardcoded colors, spacing, or font sizes.
- **Console SDK hooks only** — `useK8sWatchResource` for reads, `k8sPatch` for writes. No direct API calls, no polling.
- **Imports from `react-router-dom`** — `Link`, `useHistory`, `useParams` from `react-router-dom` (React Router v5 on OCP 4.20). Test mocks use `jest.mock('react-router', ...)` at the mock layer.
- **No external state libraries** — `useState` / `useCallback` for retry state. No Redux, Zustand, MobX.
- **No separate CSS files** — inline styles with PatternFly tokens.
- **Default export** on `ExecutionDetailPage` — required by Console SDK `$codeRef`.
- **No separate confirmation dialog for retry** — retry is a secondary action, not destructive. One click triggers the annotation patch.

### What NOT to Do

- **Do NOT implement the ExecutionGanttChart** — that is Phase 1b. Continue using the ProgressStepper approach from Story 7.2.
- **Do NOT implement toast notifications** — that is Story 7.4.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`. The backend retry mechanism already exists (Story 4.6).
- **Do NOT implement a retry confirmation dialog** — the UX spec explicitly states retry is a secondary action with no cascading confirmation.
- **Do NOT implement per-step drill-down** — show the failed step name, but no expandable step-by-step detail within a group. Keep it: Wave → DRGroup → error summary.
- **Do NOT implement client-side retry precondition validation** — the backend handles all validation via the reconciler. The Console just sets the annotation and observes results.
- **Do NOT use `useDRGroupStatuses` for retry state** — `DRExecution.status.waves[].groups[]` contains the embedded group status with retry state. The watch on DRExecution already delivers updates.
- **Do NOT implement "Retry All" with individual patches** — use the `"all-failed"` sentinel value in a single annotation patch. The backend resolves it to all failed groups.

### Testing Approach

**FailedGroupDetail tests:**

```typescript
import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import FailedGroupDetail from '../../src/components/ExecutionDetail/FailedGroupDetail';

expect.extend(toHaveNoViolations);

const mockFailedGroup: DRGroupExecutionStatus = {
  name: 'drgroup-3',
  result: 'Failed',
  vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'],
  error: 'storage driver timeout on SetSource for volume-group vg-erp-app',
  steps: [
    { name: 'StopReplication', status: 'Completed', timestamp: '...' },
    { name: 'StartVM', status: 'Failed', message: 'timeout', timestamp: '...' },
  ],
  retryCount: 1,
  startTime: '...',
  completionTime: '...',
};

describe('FailedGroupDetail', () => {
  it('renders error message from group.error', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/storage driver timeout/)).toBeInTheDocument();
  });

  it('shows affected VM names', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/erp-app-1/)).toBeInTheDocument();
    expect(screen.getByText(/erp-app-2/)).toBeInTheDocument();
  });

  it('shows failed step name', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/StartVM/)).toBeInTheDocument();
  });

  it('shows retry count when > 0', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/previously retried 1 time/i)).toBeInTheDocument();
  });

  it('does not show retry count when 0', () => {
    render(<FailedGroupDetail group={{ ...mockFailedGroup, retryCount: 0 }} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.queryByText(/previously retried/i)).not.toBeInTheDocument();
  });

  it('renders retry button when showRetryButton is true', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton onRetry={jest.fn()} isRetryDisabled={false} />);
    expect(screen.getByRole('button', { name: /retry drgroup-3/i })).toBeInTheDocument();
  });

  it('disables retry button when isRetryDisabled is true', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton onRetry={jest.fn()} isRetryDisabled retryTooltip="Retry in progress" />);
    expect(screen.getByRole('button', { name: /retry drgroup-3/i })).toBeDisabled();
  });

  it('shows retry error when provided', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton isRetryDisabled={false} retryError="VM erp-app-1 is in an unpredictable state" />);
    expect(screen.getByText(/unpredictable state/)).toBeInTheDocument();
  });

  it('passes jest-axe', async () => {
    const { container } = render(<FailedGroupDetail group={mockFailedGroup} showRetryButton onRetry={jest.fn()} isRetryDisabled={false} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
```

**useRetryDRGroup tests:**

```typescript
import { k8sPatch } from '@openshift-console/dynamic-plugin-sdk';

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  k8sPatch: jest.fn(),
}));

describe('useRetryDRGroup', () => {
  it('patches annotation with group name for single retry', async () => {
    // ... renderHook, call retry('drgroup-3'), verify k8sPatch called with correct path/value
  });

  it('patches annotation with all-failed for retry all', async () => {
    // ... renderHook, call retryAll(), verify k8sPatch called with 'all-failed'
  });

  it('returns isRetrying true while patch is in flight', async () => { ... });

  it('extracts RetryRejected condition message', () => { ... });

  it('clears retryError on new retry attempt', async () => { ... });
});
```

**Mock data patterns — extend existing:**

```typescript
const mockPartialExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'erp-failover-1714327200000', uid: '1', labels: { 'soteria.io/plan-name': 'erp-full-stack' } },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    result: 'PartiallySucceeded',
    startTime: new Date(Date.now() - 17 * 60 * 1000).toISOString(),
    completionTime: new Date().toISOString(),
    rpoSeconds: 47,
    waves: [
      {
        waveIndex: 0,
        startTime: '...',
        completionTime: '...',
        groups: [
          { name: 'drgroup-1', result: 'Completed', vmNames: ['erp-db-1', 'erp-db-2'], startTime: '...', completionTime: '...' },
          { name: 'drgroup-2', result: 'Completed', vmNames: ['erp-db-3'], startTime: '...', completionTime: '...' },
        ],
      },
      {
        waveIndex: 1,
        startTime: '...',
        completionTime: '...',
        groups: [
          {
            name: 'drgroup-3',
            result: 'Failed',
            vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'],
            error: 'storage driver timeout on SetSource for volume-group vg-erp-app',
            steps: [
              { name: 'StopReplication', status: 'Completed', timestamp: '...' },
              { name: 'StartVM', status: 'Failed', message: 'timeout after 5m', timestamp: '...' },
            ],
            retryCount: 0,
            startTime: '...',
            completionTime: '...',
          },
          { name: 'drgroup-4', result: 'Completed', vmNames: ['erp-app-4', 'erp-app-5'], startTime: '...', completionTime: '...' },
        ],
      },
      {
        waveIndex: 2,
        startTime: '...',
        completionTime: '...',
        groups: [
          { name: 'drgroup-5', result: 'Completed', vmNames: ['erp-web-1', 'erp-web-2', 'erp-web-3'], startTime: '...', completionTime: '...' },
          { name: 'drgroup-6', result: 'Completed', vmNames: ['erp-web-4'], startTime: '...', completionTime: '...' },
        ],
      },
    ],
  },
};
```

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── ExecutionDetail/
│   │   ├── ExecutionDetailPage.tsx       # MODIFIED — wire useRetryDRGroup hook
│   │   ├── ExecutionHeader.tsx           # MODIFIED — add "Retry All Failed" button
│   │   ├── WaveProgressStep.tsx          # MODIFIED — add Retry button + FailedGroupDetail
│   │   └── FailedGroupDetail.tsx         # NEW — error detail + retry button for failed groups
│   └── ... (unchanged)
├── hooks/
│   ├── useRetryDRGroup.ts               # NEW — retry via k8sPatch annotation
│   └── ... (unchanged)
└── ... (unchanged)
```

**Test files:**
```
console-plugin/tests/
├── components/
│   ├── FailedGroupDetail.test.tsx        # NEW — error display + retry tests
│   ├── WaveProgressStep.test.tsx         # MODIFIED — add retry button tests
│   ├── ExecutionHeader.test.tsx          # MODIFIED — add "Retry All Failed" tests
│   └── ExecutionDetailPage.test.tsx      # MODIFIED — add retry integration tests
├── hooks/
│   └── useRetryDRGroup.test.ts          # NEW — retry hook tests
└── ... (unchanged)
```

### Project Structure Notes

- `FailedGroupDetail.tsx` lives in `ExecutionDetail/` — it's specific to the execution monitor, not a shared component
- `useRetryDRGroup.ts` is a new hook in `src/hooks/` — follows the same pattern as `useCreateDRExecution.ts` from Story 7.1
- The `K8sModel` for DRExecution should be extracted to a shared location if not already done (check `useCreateDRExecution.ts`)
- No new PatternFly dependencies — `ExpandableSection`, `Button`, `Alert`, `Tooltip` are all in `@patternfly/react-core` ^6.2.2
- No changes to `console-extensions.json` — all changes are within existing routes
- No new CRD types — all data comes from existing `DRExecution.status.waves[].groups[]` structure
- The annotation constant `'soteria.io/retry-groups'` should be defined once (e.g., in `src/models/constants.ts` or alongside the hook)

### References

- [Source: _bmad-output/planning-artifacts/epics.md § Story 7.3] — Acceptance criteria, user story, FR39 reference
- [Source: _bmad-output/planning-artifacts/epics.md § Story 4.6] — Backend retry mechanism: preconditions, validation, re-execution
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Inline Error Resolution] — "Error appears inline with Retry action button"
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Control over Helplessness] — "Every error state has a next action. Failed DRGroups show Retry inline"
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § ExecutionGanttChart] — "inline error/retry" as component purpose
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Execution Monitor Color Language] — DRGroup Failed=red danger, Retrying=blue pulsing
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Common Error Pattern] — "Error occurs → Inline error display → Actionable next step → Resolution → Status updates"
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § GitHub Actions re-run] — "Retry failed DRGroup inline without re-running the entire execution"
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Novel UX Patterns] — "Inline execution retry — pre-flight validation, familiar from GitHub Actions"
- [Source: _bmad-output/planning-artifacts/architecture.md § API & Communication Patterns] — `soteria.io/retry-groups` annotation, error model, fail-forward
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 6
- [Source: _bmad-output/planning-artifacts/architecture.md § CRD Status Patterns] — Per-DRGroup status fields (Failed, error, steps, retryCount)
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK for data, PatternFly only, no state libraries
- [Source: pkg/engine/executor.go § RetryGroupsAnnotation] — `"soteria.io/retry-groups"` annotation key, `"all-failed"` sentinel
- [Source: pkg/controller/drexecution/reconciler.go § reconcileRetry] — Reconciler retry path: validates preconditions, re-executes, removes annotation
- [Source: console-plugin/src/models/types.ts] — DRGroupExecutionStatus.error, .steps, .retryCount, Condition interface
- [Source: console-plugin/src/hooks/useDRResources.ts] — useDRExecution watch hook

### Previous Story Intelligence

**Story 7.2 (Live Execution Monitor — ProgressStepper) established:**
- `ExecutionDetailPage.tsx` rewritten as full execution monitor
- `WaveProgressStep.tsx` with `ExpandableSection` for DRGroup detail within waves
- DRGroup status icons: Pending (gray PendingIcon), InProgress (Spinner), Completed (CheckCircleIcon), Failed (ExclamationCircleIcon)
- `getWaveState()` wave state derivation function
- `ExecutionHeader.tsx` with execution metadata, elapsed time, result badge
- `useElapsedTime.ts` hook — reusable for group-level elapsed time
- Error state already displayed for failed groups (red icon + error text) — Story 7.3 enhances this with detailed error view and retry action
- `useDRExecution(name)` provides real-time updates via watch
- `DRBreadcrumb` with planName + executionName 3-level hierarchy
- Monospace time display pattern: `fontFamily: 'var(--pf-t--global--font--family--mono)'` (or `--pf-v5-global--FontFamily--monospace` fallback)

**Story 7.1 (Pre-flight Confirmation & Failover Trigger) established:**
- `k8sCreate` usage pattern for DRExecution creation
- `K8sModel` definition for DRExecution resource — reuse for `k8sPatch`
- Action-to-mode mapping in `drPlanActions.ts`
- Inline `Alert` error display within modal on creation failure
- Console SDK write operations pattern (loading/error state management)

**Story 6.6 (Status Badges, Empty States & Accessibility) established:**
- jest-axe pattern with `toHaveNoViolations`
- Keyboard testing with `userEvent.setup()` + `userEvent.tab()`
- react-router mock: `jest.mock('react-router', ...)` with `jest.requireActual`
- 291+ tests pass across the full console-plugin test suite

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

All Epic 6 stories complete. Stories 7.1 and 7.2 are ready-for-dev. This story adds ~1 new source file, ~1 new hook, modifies ~3 existing files, and creates ~2 new test files + modifies ~3 existing test files.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
