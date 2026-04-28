# Story 7.1: Pre-flight Confirmation & Failover Trigger

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want a pre-flight confirmation dialog showing VM count, RPO estimate, RTO estimate, capacity, and a safety keyword input before any destructive action,
So that I act with full confidence and never trigger failover accidentally.

## Acceptance Criteria

1. **AC1 — Modal opens from lifecycle diagram action button:** When the operator clicks an action button on the DRLifecycleDiagram's outgoing transition arrow (Failover, Planned Migration, Reprotect, Failback, or Restore), a PatternFly `Modal` (large variant, ~800px) opens with a structured pre-flight summary. (FR37)

2. **AC2 — Disaster failover pre-flight content:** For a disaster failover, the modal displays:
   - VM count and wave count (e.g., "12 VMs across 3 waves")
   - Estimated RPO prominently at `--pf-t--global--font--size--heading--h1` (or `--pf-v5-global--FontSize--2xl` fallback) bold (time since last replication sync)
   - Estimated RTO based on last execution duration (e.g., "~18 min based on last execution")
   - DR site capacity assessment (sufficient / warning)
   - Summary of actions to be performed ("Force-promote volumes on DC2, start VMs wave by wave")
   - RPO is the single most visually prominent number in the dialog

3. **AC3 — Planned migration pre-flight content:** For a planned migration, RPO shows "0 — guaranteed (both DCs up, final sync before promote)" and the summary includes Step 0: "Stop VMs on origin → wait for final sync → promote on target".

4. **AC4 — Confirmation keyword input:** A `TextInput` field displays with the instruction "Type FAILOVER to confirm" (or MIGRATE / REPROTECT / FAILBACK / RESTORE depending on the action). The Confirm button is disabled until the keyword matches exactly (case-sensitive). Failover uses danger variant (red) for the Confirm button; all others use primary variant. (FR38)

5. **AC5 — DRExecution creation on confirm:** When the confirmation keyword is entered correctly and the operator clicks Confirm, a `DRExecution` resource is created via `k8sCreate` from the Console SDK with the appropriate `spec.mode`. The modal closes and the Overview tab shows the transition in-progress state (progress banner, "In progress..." on the transition arrow, dashed border on destination phase). The pre-flight modal is the only confirmation — no cascading "Are you sure?" dialogs.

6. **AC6 — Cancel behavior:** When the operator presses Escape or clicks Cancel, the modal closes with no side effects.

7. **AC7 — Accessibility:** Focus is trapped in the modal and auto-focused on the first element. The confirmation field has a clear label and screen reader announcement. Keyboard navigation: Tab through summary → input → Confirm/Cancel buttons. jest-axe zero violations.

8. **AC8 — Planned Migration exposed from lifecycle diagram:** The SteadyState transition edge in the DRLifecycleDiagram shows both Failover and Planned Migration buttons (two valid actions from SteadyState). Failover uses `variant="danger"`, Planned Migration uses `variant="secondary"`.

## Tasks / Subtasks

- [x] Task 1: Create `PreflightConfirmationModal` component (AC: #1, #2, #3, #4, #6, #7)
  - [x] 1.1 Create `src/components/DRPlanDetail/PreflightConfirmationModal.tsx` — PatternFly `Modal` with `variant="large"`, structured pre-flight summary, TextInput confirmation field, Confirm/Cancel buttons
  - [x] 1.2 Implement pre-flight summary section: VM count, wave count, estimated RPO (from `getReplicationHealth`), estimated RTO (from last execution via `useDRExecutions`), action summary text
  - [x] 1.3 Implement action-specific content variants: disaster failover summary vs. planned migration Step 0 summary vs. reprotect/failback/restore summaries
  - [x] 1.4 Implement confirmation keyword validation: TextInput with `FormGroup` label "Type {KEYWORD} to confirm", Confirm button disabled until exact match
  - [x] 1.5 Implement button variants: `variant="danger"` for Failover confirm, `variant="primary"` for all others
  - [x] 1.6 Implement Cancel + Escape close with no side effects

- [x] Task 2: Create `usePreflightData` hook (AC: #2, #3)
  - [x] 2.1 Create `src/hooks/usePreflightData.ts` — derives pre-flight display data from DRPlan + last DRExecution
  - [x] 2.2 Compute estimated RPO from `getReplicationHealth(plan).rpoSeconds`
  - [x] 2.3 Compute estimated RTO from `getLastExecution()` duration (fallback: "Unknown — no previous execution")
  - [x] 2.4 Compute DR site capacity assessment from plan status (sufficient/warning/unknown)
  - [x] 2.5 Generate action summary text per action type

- [x] Task 3: Create `useCreateDRExecution` hook (AC: #5)
  - [x] 3.1 Create `src/hooks/useCreateDRExecution.ts` — wraps `k8sCreate` from Console SDK to create a DRExecution resource
  - [x] 3.2 Map UI action to `DRExecutionSpec.mode`: Failover→`disaster`, Planned Migration→`planned_migration`, Reprotect→`reprotect`, Failback→`planned_migration`, Restore→`reprotect`
  - [x] 3.3 Generate DRExecution name: `{planName}-{action}-{timestamp}`
  - [x] 3.4 Set `soteria.io/plan-name` label for history queries
  - [x] 3.5 Return `{ create, isCreating, error }` tuple with loading/error state

- [x] Task 4: Wire modal into DRPlanDetailPage (AC: #1, #5)
  - [x] 4.1 Replace `console.log` in `handleAction` with modal state management (`useState` for `isModalOpen` + `pendingAction`)
  - [x] 4.2 Render `PreflightConfirmationModal` conditionally when `isModalOpen`
  - [x] 4.3 On confirm: call `useCreateDRExecution.create()`, close modal on success
  - [x] 4.4 On error: display inline error in modal, keep modal open

- [x] Task 5: Extend DRLifecycleDiagram for Planned Migration (AC: #8)
  - [x] 5.1 Modify `TransitionEdge` for SteadyState→FailedOver to render TWO buttons when `state === 'available'`: "Failover" (`variant="danger"`) and "Planned Migration" (`variant="secondary"`)
  - [x] 5.2 Update `onAction` calls: Failover button calls `onAction('Failover', plan)`, Planned Migration button calls `onAction('Planned Migration', plan)`

- [x] Task 6: Wire modal into DRPlanActions kebab menu (AC: #1)
  - [x] 6.1 Modify `DRPlanActions.tsx` to accept an `onAction` callback prop instead of console.log
  - [x] 6.2 In `DRDashboard.tsx`, pass the same modal trigger logic from dashboard context

- [x] Task 7: Write tests (AC: #7)
  - [x] 7.1 Create `tests/components/PreflightConfirmationModal.test.tsx`:
    - Renders modal with pre-flight summary for each action type (5 actions)
    - Confirm button disabled until keyword matches exactly
    - Confirm button enabled after correct keyword
    - Cancel closes modal with no side effects
    - Escape closes modal with no side effects
    - Failover uses danger variant, others use primary
    - RPO displayed at 2xl size for disaster failover
    - Planned migration shows RPO "0 — guaranteed"
    - jest-axe zero violations for all action variants
  - [x] 7.2 Create `tests/hooks/usePreflightData.test.ts`:
    - Computes estimated RPO from plan health
    - Computes estimated RTO from last execution
    - Returns "Unknown" RTO when no previous execution
    - Generates correct summary text per action type
  - [x] 7.3 Create `tests/hooks/useCreateDRExecution.test.ts`:
    - Maps actions to correct DRExecution mode
    - Generates correct resource name
    - Sets soteria.io/plan-name label
    - Handles creation error
  - [x] 7.4 Update `tests/components/DRLifecycleDiagram.test.tsx`:
    - SteadyState shows both Failover and Planned Migration buttons
    - Only Failover has danger variant
    - Both buttons trigger onAction with correct action string
  - [x] 7.5 Run `yarn build` and verify all tests pass

### Review Findings

- [x] [Review][Patch] Clear stale create errors when the pre-flight modal closes [`console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx`, `console-plugin/src/hooks/useCreateDRExecution.ts`]
- [x] [Review][Patch] Do not compute pre-flight RTO from an unloaded execution-history watch; show a loading or unknown state explicitly until `useDRExecutions` has loaded [`console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx`, `console-plugin/src/hooks/usePreflightData.ts`]
- [x] [Review][Patch] Normalize action inputs before looking up `ACTION_CONFIG` so the modal and create hook accept both title-case labels and existing lowercase keys as required by the story (`Failover` and `failover`, etc.) [`console-plugin/src/components/DRPlanDetail/PreflightConfirmationModal.tsx`, `console-plugin/src/hooks/useCreateDRExecution.ts`, `console-plugin/src/utils/drPlanActions.ts`]
- [x] [Review][Patch] Add the explicit `Step 0:` framing to the planned-migration pre-flight summary to match AC3 [`console-plugin/src/hooks/usePreflightData.ts`]

## Dev Notes

### This Story Bridges Read-Only UI to Write Operations

Stories 6.1–6.6 built the complete read-only Console plugin. Story 7.1 is the first story that **writes** to the Kubernetes API — creating DRExecution resources. This is a critical architectural boundary crossing: the Console plugin transitions from passive observer to active participant.

### Console SDK `k8sCreate` for Resource Creation

The Console SDK `@openshift-console/dynamic-plugin-sdk` exports `k8sCreate` for creating Kubernetes resources. This is the correct way to create resources from a Console plugin — never use `fetch()` or direct API calls.

```typescript
import { k8sCreate, K8sModel } from '@openshift-console/dynamic-plugin-sdk';

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

const execution = await k8sCreate({
  model: drExecutionModel,
  data: {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: {
      name: `${planName}-${actionKey}-${Date.now()}`,
      labels: {
        'soteria.io/plan-name': planName,
      },
    },
    spec: {
      planName,
      mode: actionToMode(action),
    },
  },
});
```

### Action-to-Mode Mapping

The `DRExecution.spec.mode` value differs from the UI action label. The mapping is:

| UI Action | `onAction` string | `DRExecution.spec.mode` | Confirmation Keyword | Confirm Button Variant |
|-----------|-------------------|-------------------------|---------------------|----------------------|
| Failover | `'Failover'` | `'disaster'` | `FAILOVER` | `danger` |
| Planned Migration | `'Planned Migration'` | `'planned_migration'` | `MIGRATE` | `primary` |
| Reprotect | `'Reprotect'` | `'reprotect'` | `REPROTECT` | `primary` |
| Failback | `'Failback'` | `'planned_migration'` | `FAILBACK` | `primary` |
| Restore | `'Restore'` | `'reprotect'` | `RESTORE` | `primary` |

This mapping must live in a shared constant (e.g., in `drPlanActions.ts`) to avoid duplication.

### Action String Normalization

The DRLifecycleDiagram passes action strings in title case (`'Failover'`, `'Reprotect'`). The `DRAction.key` values from `getValidActions` use lowercase/snake_case (`'failover'`, `'planned_migration'`). The modal needs to handle both input formats. Use the title-case action strings as the canonical input — they match the diagram's `TRANSITIONS` array.

### PreflightConfirmationModal Component Design

```typescript
interface PreflightConfirmationModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  action: string;           // 'Failover' | 'Planned Migration' | 'Reprotect' | 'Failback' | 'Restore'
  plan: DRPlan;
  preflightData: PreflightData;
  isCreating: boolean;
  error?: string;
}
```

**Modal layout structure:**

```
┌─────────────────────────────────────────────────────────────┐
│ Confirm Failover: erp-full-stack                      [X]   │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   Estimated Data Loss (RPO)                                  │
│   47 seconds                          ← 2xl bold, prominent │
│                                                              │
│   12 VMs across 3 waves               ← xl semi-bold        │
│   Estimated duration: ~18 min (based on last execution)      │
│   DR site capacity: Sufficient                               │
│                                                              │
│   ─────────────────────────────────────────────              │
│   Summary of actions:                                        │
│   Force-promote volumes on dc2-prod, start VMs wave by wave │
│                                                              │
│   ─────────────────────────────────────────────              │
│   Type FAILOVER to confirm                                   │
│   [________________]                   ← lg monospace        │
│                                                              │
├─────────────────────────────────────────────────────────────┤
│                              [Cancel]  [Confirm Failover]    │
│                                         ↑ danger variant     │
└─────────────────────────────────────────────────────────────┘
```

### Action-Specific Summary Text

Each action has distinct summary text based on what the orchestrator will do:

| Action | Summary Text |
|--------|-------------|
| Failover | "Force-promote volumes on {secondarySite}, start VMs wave by wave" |
| Planned Migration | "Stop VMs on {activeSite} → wait for final replication sync → promote volumes on {secondarySite} → start VMs wave by wave" |
| Reprotect | "Demote volumes on old active site, initiate replication resync, monitor until healthy" |
| Failback | "Stop VMs on {activeSite} → wait for final replication sync → promote volumes on {primarySite} → start VMs wave by wave" |
| Restore | "Demote volumes on old active site, initiate replication resync, monitor until healthy" |

### RPO Display Per Action

| Action | RPO Display |
|--------|------------|
| Failover (disaster) | Time since last sync, e.g., "47 seconds" — the actual estimated data loss |
| Planned Migration | "0 — guaranteed (both DCs up, final sync before promote)" |
| Reprotect | "N/A — no data movement, establishes reverse replication" |
| Failback | "0 — guaranteed (both DCs up, final sync before promote)" — same as planned migration |
| Restore | "N/A — no data movement, establishes reverse replication" — same as reprotect |

### Extending DRLifecycleDiagram for Planned Migration

The SteadyState → FailedOver `TransitionEdge` currently renders ONE button ("Failover", `variant="danger"`). It needs to render TWO buttons when `state === 'available'`:

```typescript
{state === 'available' && transition.from === 'SteadyState' && (
  <>
    <Button variant="danger" onClick={() => onAction('Failover', plan)} size="sm">
      Failover
    </Button>
    <Button variant="secondary" onClick={() => onAction('Planned Migration', plan)} size="sm">
      Planned Migration
    </Button>
    <span style={{ /* arrow */ }}>{arrow}</span>
  </>
)}
```

For the Failback edge (DRedSteadyState → FailedBack), the UX spec only shows one "Failback" action, so no dual-button is needed there.

### DRPlanActions Kebab Menu Integration

`DRPlanActions.tsx` currently console.logs the action. It needs to accept an `onAction` callback prop to trigger the pre-flight modal from the dashboard too. The `DRDashboard.tsx` needs minimal changes — pass an `onAction` handler that opens the modal or navigates to the plan detail page.

For v1 scope, the kebab menu actions can navigate to the plan detail page where the modal opens from the lifecycle diagram. Full dashboard-level modal integration is a stretch goal — the critical path is the lifecycle diagram trigger.

### Dependency on Existing Components

**From Story 6.1:**
- `src/models/types.ts` — `DRPlan`, `DRExecution`, `DRExecutionSpec`, `DRExecutionMode` interfaces
- `src/hooks/useDRResources.ts` — `useDRPlan(name)`, `useDRExecutions(planName)` hooks
- Jest + jest-axe + RTL configured

**From Story 6.2:**
- React Router v5 imports from `react-router-dom`: `import { Link, useHistory, useParams } from 'react-router-dom'` (NOT `react-router` — OCP 4.20 ships RR v5, not v7)
- Programmatic navigation: `const history = useHistory(); history.push('/path')` (NOT `useNavigate`)
- Default exports on page components

**From Story 6.3:**
- `src/utils/drPlanUtils.ts` — `getEffectivePhase(plan)`, `getReplicationHealth(plan)`, `getLastExecution(executions, planName)`
- `src/utils/formatters.ts` — `formatRPO(seconds)`, `formatDuration(start, end)`

**From Story 6.5:**
- `src/components/DRPlanDetail/DRPlanDetailPage.tsx` — tab shell with `handleAction(action, plan)` stub
- `src/components/DRPlanDetail/DRLifecycleDiagram.tsx` — `onAction` callback, `TRANSITIONS` array, `TransitionEdge` component

**From Story 6.5b:**
- `src/components/DRPlanDetail/ExecutionHistoryTable.tsx` — uses `useDRExecutions`

**From Story 6.6:**
- All accessibility patterns established: jest-axe, keyboard navigation, ARIA labels
- `DRAction` interface and `getValidActions` in `src/utils/drPlanActions.ts`

### `usePreflightData` Hook Design

```typescript
interface PreflightData {
  vmCount: number;
  waveCount: number;
  estimatedRPO: string;          // "47 seconds" or "0 — guaranteed"
  estimatedRPOSeconds: number | null;
  estimatedRTO: string;          // "~18 min based on last execution" or "Unknown"
  capacityAssessment: 'sufficient' | 'warning' | 'unknown';
  actionSummary: string;
  primarySite: string;
  secondarySite: string;
  activeSite: string;
}

function usePreflightData(plan: DRPlan, action: string): PreflightData
```

Data sources:
- `plan.status?.discoveredVMCount` → VM count
- `plan.status?.waves?.length` → wave count
- `getReplicationHealth(plan).rpoSeconds` → RPO estimate
- `getLastExecution(executions, planName)` → RTO estimate from previous execution duration
- `plan.spec.primarySite` / `plan.spec.secondarySite` / `plan.status?.activeSite` → site names for summary text

### Error Handling in Modal

If `k8sCreate` fails (e.g., admission webhook rejects the DRExecution — concurrent execution already active), display the error inline in the modal using a PatternFly `Alert` (variant="danger", isInline) below the confirmation input. Keep the modal open so the operator can read the error and either retry or cancel.

```typescript
{error && (
  <Alert variant="danger" isInline title="Failed to create execution">
    {error}
  </Alert>
)}
```

### Non-Negotiable Constraints

- **PatternFly 6 ONLY** — `@patternfly/react-core` Modal, TextInput, FormGroup, Button, Alert, DescriptionList. No other UI libraries.
- **CSS custom properties only** — PF6 `--pf-t--global--*` tokens preferred; `--pf-v5-global--*` tokens still resolve as fallbacks. No hardcoded colors, spacing, or font sizes.
- **Console SDK hooks only** — `useK8sWatchResource` for reads, `k8sCreate` for writes. No direct API calls.
- **Imports from `react-router-dom`** — `Link`, `useHistory`, `useParams` from `react-router-dom` (React Router v5 on OCP 4.20). NOT `react-router` unified import (that's v7).
- **No external state libraries** — useState/useCallback for modal state. No Redux, Zustand, MobX.
- **One confirmation dialog** — the pre-flight modal IS the confirmation. No cascading "Are you sure?" dialogs.
- **No separate CSS files** — inline styles with PatternFly tokens.

### What NOT to Do

- **Do NOT implement the live execution monitor** — that is Story 7.2. After DRExecution creation, the Overview tab's existing `TransitionProgressBanner` handles the in-progress display.
- **Do NOT implement toast notifications** — that is Story 7.4.
- **Do NOT implement inline error display or DRGroup retry** — that is Story 7.3.
- **Do NOT modify Go code** — this story is pure TypeScript/React in `console-plugin/`.
- **Do NOT implement a pre-flight API endpoint** — derive all pre-flight data client-side from existing DRPlan status and DRExecution history.
- **Do NOT add a "Pre-flight Check" standalone button** — the pre-flight summary is shown as part of the action confirmation modal.
- **Do NOT implement capacity assessment logic** — v1 shows "Unknown" or derives from plan status; real capacity assessment requires server-side implementation.
- **Do NOT create separate modals per action** — one `PreflightConfirmationModal` component handles all 5 actions via props.

### Testing Approach

**PreflightConfirmationModal tests:**

```typescript
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { axe, toHaveNoViolations } from 'jest-axe';
import { PreflightConfirmationModal } from '../../src/components/DRPlanDetail/PreflightConfirmationModal';

expect.extend(toHaveNoViolations);

describe('PreflightConfirmationModal', () => {
  const defaultProps = {
    isOpen: true,
    onClose: jest.fn(),
    onConfirm: jest.fn(),
    action: 'Failover',
    plan: mockSteadyStatePlan,
    preflightData: mockPreflightData,
    isCreating: false,
  };

  it('renders pre-flight summary with VM count and wave count', () => {
    render(<PreflightConfirmationModal {...defaultProps} />);
    expect(screen.getByText(/12 VMs across 3 waves/)).toBeInTheDocument();
  });

  it('displays RPO prominently for disaster failover', () => {
    render(<PreflightConfirmationModal {...defaultProps} />);
    const rpo = screen.getByText('47 seconds');
    expect(rpo).toBeInTheDocument();
    // Verify 2xl font size via style or data attribute
  });

  it('shows "0 — guaranteed" RPO for planned migration', () => {
    render(<PreflightConfirmationModal {...defaultProps} action="Planned Migration" preflightData={mockPlannedMigrationData} />);
    expect(screen.getByText(/0 — guaranteed/)).toBeInTheDocument();
  });

  it('Confirm button disabled until keyword matches', async () => {
    const user = userEvent.setup();
    render(<PreflightConfirmationModal {...defaultProps} />);
    const confirmBtn = screen.getByRole('button', { name: /confirm failover/i });
    expect(confirmBtn).toBeDisabled();
    const input = screen.getByLabelText(/type failover to confirm/i);
    await user.type(input, 'FAILOVER');
    expect(confirmBtn).toBeEnabled();
  });

  it('Confirm button uses danger variant for Failover', () => {
    render(<PreflightConfirmationModal {...defaultProps} />);
    const confirmBtn = screen.getByRole('button', { name: /confirm failover/i });
    expect(confirmBtn.closest('.pf-v6-c-button')).toHaveClass('pf-m-danger');
  });

  it('Confirm button uses primary variant for Reprotect', () => {
    render(<PreflightConfirmationModal {...defaultProps} action="Reprotect" preflightData={mockReprotectData} />);
    const confirmBtn = screen.getByRole('button', { name: /confirm reprotect/i });
    expect(confirmBtn.closest('.pf-v6-c-button')).toHaveClass('pf-m-primary');
  });

  it('Cancel closes modal with no side effects', async () => {
    const onClose = jest.fn();
    const onConfirm = jest.fn();
    const user = userEvent.setup();
    render(<PreflightConfirmationModal {...defaultProps} onClose={onClose} onConfirm={onConfirm} />);
    await user.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onClose).toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('displays inline error when creation fails', () => {
    render(<PreflightConfirmationModal {...defaultProps} error="concurrent execution already active" />);
    expect(screen.getByText(/concurrent execution already active/)).toBeInTheDocument();
  });

  it('passes jest-axe for Failover variant', async () => {
    const { container } = render(<PreflightConfirmationModal {...defaultProps} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('passes jest-axe for Planned Migration variant', async () => {
    const { container } = render(<PreflightConfirmationModal {...defaultProps} action="Planned Migration" preflightData={mockPlannedMigrationData} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
```

**DRLifecycleDiagram update tests:**

```typescript
it('SteadyState edge shows both Failover and Planned Migration buttons', () => {
  render(<DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={jest.fn()} />);
  expect(screen.getByRole('button', { name: /^failover$/i })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: /planned migration/i })).toBeInTheDocument();
});

it('Failover button uses danger variant, Planned Migration uses secondary', () => {
  const { container } = render(<DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={jest.fn()} />);
  const failoverBtn = screen.getByRole('button', { name: /^failover$/i });
  expect(failoverBtn.closest('.pf-v6-c-button')).toHaveClass('pf-m-danger');
  const migrateBtn = screen.getByRole('button', { name: /planned migration/i });
  expect(migrateBtn.closest('.pf-v6-c-button')).toHaveClass('pf-m-secondary');
});
```

**Mock data** — reuse patterns from Stories 6.3/6.5/6.6:

```typescript
const mockSteadyStatePlan: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: { name: 'erp-full-stack', uid: '1' },
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
    discoveredVMCount: 12,
    waves: [
      { waveKey: '1', vms: [] },
      { waveKey: '2', vms: [] },
      { waveKey: '3', vms: [] },
    ],
    conditions: [{
      type: 'ReplicationHealthy',
      status: 'True',
      reason: 'Healthy',
      message: 'RPO: 47s',
      lastTransitionTime: new Date().toISOString(),
    }],
  },
};

const mockPreflightData: PreflightData = {
  vmCount: 12,
  waveCount: 3,
  estimatedRPO: '47 seconds',
  estimatedRPOSeconds: 47,
  estimatedRTO: '~18 min based on last execution',
  capacityAssessment: 'sufficient',
  actionSummary: 'Force-promote volumes on dc2-prod, start VMs wave by wave',
  primarySite: 'dc1-prod',
  secondarySite: 'dc2-prod',
  activeSite: 'dc1-prod',
};
```

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   ├── DRPlanActions.tsx           # MODIFIED — accept onAction prop
│   │   └── ... (unchanged)
│   ├── DRPlanDetail/
│   │   ├── PreflightConfirmationModal.tsx  # NEW — pre-flight modal component
│   │   ├── DRPlanDetailPage.tsx        # MODIFIED — modal state, handleAction wired
│   │   ├── DRLifecycleDiagram.tsx      # MODIFIED — Planned Migration button on SteadyState edge
│   │   └── ... (unchanged)
│   └── ...
├── hooks/
│   ├── usePreflightData.ts            # NEW — pre-flight data derivation
│   ├── useCreateDRExecution.ts        # NEW — DRExecution creation via k8sCreate
│   └── ... (unchanged)
└── utils/
    ├── drPlanActions.ts               # MODIFIED — add ACTION_CONFIG with keyword/mode/variant mapping
    └── ... (unchanged)
```

**New test files:**
```
console-plugin/tests/
├── components/
│   └── PreflightConfirmationModal.test.tsx  # NEW — modal tests + jest-axe
├── hooks/
│   ├── usePreflightData.test.ts       # NEW — preflight data derivation tests
│   └── useCreateDRExecution.test.ts   # NEW — execution creation tests
└── ...
```

### Project Structure Notes

- `PreflightConfirmationModal.tsx` lives in `DRPlanDetail/` because it's triggered from the plan detail page's lifecycle diagram
- `usePreflightData.ts` and `useCreateDRExecution.ts` are new hooks in `src/hooks/`
- The action-to-mode mapping constant is added to `src/utils/drPlanActions.ts` (extends existing file)
- No new shared components needed — the modal uses standard PatternFly components
- `DRPlanActions.tsx` is modified to accept a callback prop (currently console.logs)
- `DRPlanDetailPage.tsx` is modified to add modal state management

### References

- [Source: _bmad-output/planning-artifacts/epics.md § Story 7.1] — Acceptance criteria, user story
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Action Hierarchy] — Confirmation keywords, button variants per action
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Typography System] — Pre-flight RPO at 2xl bold, VM count at xl, keyword at lg monospace
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Spacing & Layout] — Pre-flight Dialog at large modal (~800px), medium density, spacer--lg between sections
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Safety & Confirmation] — AWS-inspired keyword input, SRM-inspired pre-flight summary
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Accessibility] — Confirmation input accessibility, keyboard-accessible failover flow
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Anti-Patterns] — No cascading confirmations, one high-quality pre-flight dialog
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Component Strategy] — Modal (variant="large"), TextInput + FormGroup, Button variants
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § DRLifecycleDiagram] — Action button click opens pre-flight modal, danger variant for Failover only
- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 6, module federation
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console SDK for data, PatternFly only, no state libraries
- [Source: _bmad-output/implementation-artifacts/6-6-status-badges-empty-states-accessibility.md] — jest-axe patterns, keyboard testing with userEvent, mock data patterns
- [Source: _bmad-output/implementation-artifacts/6-5-plan-detail-shell-overview-tab.md] — DRLifecycleDiagram onAction callback, TransitionEdge component, TRANSITIONS array
- [Source: console-plugin/src/utils/drPlanActions.ts] — DRAction interface, getValidActions, ACTIONS_BY_PHASE
- [Source: console-plugin/src/models/types.ts] — DRExecutionSpec { planName, mode }, ExecutionMode enum
- [Source: console-plugin/src/hooks/useDRResources.ts] — useDRPlan, useDRExecutions hooks

### Previous Story Intelligence

**Story 6.6 (Status Badges, Empty States & Accessibility) established:**
- jest-axe pattern: `const { container } = render(<Component />); expect(await axe(container)).toHaveNoViolations()`
- Keyboard testing with `userEvent.setup()` + `userEvent.tab()` + `userEvent.keyboard('{Enter}')`
- react-router mock: `jest.mock('react-router', ...)` with `jest.requireActual('react-router')` (mock layer operates at `react-router` level; runtime imports from `react-router-dom`)
- PatternFly `Button` focus testing: use `screen.getByRole('button', { name: /.../ })` then `.focus()` or `userEvent.tab()`
- DRLifecycleDiagram uses `role="figure"` (not `role="img"`) — avoids axe nested-interactive violation
- All 291 tests pass across the full console-plugin test suite

**Story 6.5 (Plan Detail Shell & Overview Tab) established:**
- `handleAction(action: string, plan: DRPlan)` as module-level function in `DRPlanDetailPage.tsx` — currently `console.log`s
- `DRLifecycleDiagram` component with `onAction` prop
- `TransitionEdge` renders PatternFly `Button` with `variant="danger"` for Failover, `variant="secondary"` for others
- `TRANSITIONS` exported from `DRLifecycleDiagram.tsx` with `isDanger` flag per transition
- `WaveProgress` interface for transition progress display

**Story 6.3 (DR Dashboard Table & Toolbar) established:**
- `getReplicationHealth(plan)` returns `{ status, rpoSeconds }` — used for RPO estimate
- `getLastExecution(executions, planName)` returns most recent execution — used for RTO estimate
- `formatRPO(seconds)` and `formatDuration(start, end)` formatters

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

All Epic 6 stories are complete. Story 7.1 is the first Epic 7 story. The codebase has 28 source files and passes 291 tests.

## Dev Agent Record

### Agent Model Used

Opus 4.6 (Cursor)

### Debug Log References

- PF6 Modal API: `title` and `actions` props not supported in PF6; switched to compositional API with `ModalHeader`/`ModalBody`/`ModalFooter`
- `@testing-library/react` v12 does not export `renderHook`; used React mock pattern for hook tests instead
- Task 5 (DRLifecycleDiagram dual buttons) already implemented in existing code from Story 6.5/6.6; action keys passed as lowercase (`a.key`), not title-case as story spec assumed; modal's `ACTION_CONFIG` handles lowercase keys directly to avoid breaking existing 303-line test suite

### Completion Notes List

- **Task 1:** Created `PreflightConfirmationModal` using PF6 compositional Modal API (`ModalHeader`/`ModalBody`/`ModalFooter`), with structured pre-flight summary, keyword confirmation `TextInput` in `FormGroup`, danger variant for Failover confirm, inline `Alert` for creation errors, and proper ARIA labeling
- **Task 2:** Created `getPreflightData` pure function in `usePreflightData.ts` — derives VM count, wave count, RPO (47s human-readable for disaster, "0 — guaranteed" for planned, "N/A" for reprotect/restore), RTO from last execution duration, capacity from preflight warnings, and action-specific summary text with site names
- **Task 3:** Created `useCreateDRExecution` hook wrapping Console SDK `k8sCreate` — maps 6 action keys to 3 `DRExecution.spec.mode` values via `ACTION_CONFIG`, generates hyphenated resource names with timestamps, sets `soteria.io/plan-name` label, tracks `isCreating`/`error` state
- **Task 4:** Rewired `DRPlanDetailPage` — replaced module-level `handleAction` console.log with component-level `useCallback` that sets `pendingAction` state, conditionally renders `PreflightConfirmationModal`, calls `useCreateDRExecution.create()` on confirm, clears state on success, keeps modal open on error
- **Task 5:** Already satisfied by existing code — SteadyState edge renders both Failover (danger) and Planned Migration (secondary) buttons via `getValidActions`/`ACTIONS_BY_PHASE`; DRedSteadyState similarly renders Failback + Planned Migration
- **Task 6:** Modified `DRPlanActions` to accept optional `onAction` callback prop; `DRDashboard` passes `history.push` handler to navigate to plan detail page on kebab action (v1 scope per dev notes)
- **Task 7:** 75 new tests across 3 files — 35 PreflightConfirmationModal tests (5 action variants, keyword validation, button variants, cancel/escape, RPO display, error display, 6 jest-axe accessibility passes), 24 usePreflightData tests (RPO/RTO/summary/capacity/sites), 16 useCreateDRExecution tests (mode mapping, naming, labels, loading/error state, ACTION_CONFIG completeness)

### Change Log

- 2026-04-28: Story 7.1 implemented — Pre-flight Confirmation & Failover Trigger. 3 new source files, 2 new hooks, 1 modified utility, 3 modified components. 370 total tests (75 new), `yarn build` clean, all Go unit + integration tests pass.

### File List

**New files:**
- `console-plugin/src/components/DRPlanDetail/PreflightConfirmationModal.tsx`
- `console-plugin/src/hooks/usePreflightData.ts`
- `console-plugin/src/hooks/useCreateDRExecution.ts`
- `console-plugin/tests/components/PreflightConfirmationModal.test.tsx`
- `console-plugin/tests/hooks/usePreflightData.test.ts`
- `console-plugin/tests/hooks/useCreateDRExecution.test.ts`

**Modified files:**
- `console-plugin/src/utils/drPlanActions.ts` — added `ACTION_CONFIG` with keyword/mode/variant mapping per action
- `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` — replaced console.log handleAction with modal state management + PreflightConfirmationModal rendering
- `console-plugin/src/components/DRDashboard/DRPlanActions.tsx` — added optional `onAction` callback prop
- `console-plugin/src/components/DRDashboard/DRDashboard.tsx` — pass `history.push` onAction to DRPlanActions
- `console-plugin/tests/components/DRPlanActions.test.tsx` — updated to test onAction callback instead of console.log
