# Story 8.5: Optimistic DRExecution Detection in Console

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want immediate visual feedback on the plan detail page when I trigger a DR execution,
So that the UI feels responsive and I know my action was registered without waiting for the controller.

## Acceptance Criteria

1. **AC1 — Immediate optimistic banner on create success:** When the operator confirms an action in the pre-flight modal and `k8sCreate` succeeds, the transition progress banner appears immediately (within the same render cycle) without waiting for `plan.status.activeExecution` to update. The banner shows: "Starting \<mode\>..." with the mode label and a spinning indicator. This optimistic state is driven by local React state set in the `k8sCreate` success handler.

2. **AC2 — Smooth transition from optimistic to real state:** When the real `plan.status.activeExecution` watch update arrives (typically 1-3 seconds later), the optimistic state is replaced by the real execution data. The banner transitions smoothly to show actual wave progress. There is no visual flash or double-render between optimistic and real states.

3. **AC3 — No persistence across navigations:** If `k8sCreate` succeeds but the controller has not yet updated the plan, and the user navigates away and returns, the plan detail page uses the standard watch-driven mechanism (no optimistic state persisted). Once the controller updates `activeExecution`, the transition banner appears normally.

4. **AC4 — No optimistic banner on create failure:** When `k8sCreate` fails, no optimistic banner is shown. The error is displayed in the pre-flight modal as before (existing behavior).

5. **AC5 — Real execution data takeover:** When the real execution data arrives and the execution is already showing wave progress, `useDRExecution(activeExecName)` provides live data, and the elapsed time counter starts from the execution's actual `startTime`.

6. **AC6 — Tests:** Tests verify the immediate banner render after mock `k8sCreate` resolves, the transition from optimistic to real state, no optimistic state on `k8sCreate` failure, and jest-axe accessibility audit passes on the optimistic banner state.

## Tasks / Subtasks

- [x] Task 1: Add optimistic execution state to DRPlanDetailPage (AC: #1, #3, #4)
  - [x] 1.1 In `DRPlanDetailPage.tsx`, add local state:
    ```ts
    const [optimisticExec, setOptimisticExec] = useState<{ name: string; mode: string } | null>(null);
    ```
  - [x] 1.2 In `handleConfirm`, capture the return value from `create()`:
    ```ts
    const result = await create(plan.metadata!.name!, pendingAction);
    setOptimisticExec({ name: result.metadata!.name!, mode: result.spec.mode });
    setPendingAction(null);
    ```
  - [x] 1.3 Clear optimistic state when real watch data arrives. Compute:
    ```ts
    const realActiveExec = plan?.status?.activeExecution;
    useEffect(() => {
      if (optimisticExec && realActiveExec) {
        setOptimisticExec(null);
      }
    }, [realActiveExec, optimisticExec]);
    ```
  - [x] 1.4 Compute combined `isInTransition` that includes optimistic:
    ```ts
    const isInTransition = effectivePhase !== restPhase || optimisticExec !== null;
    ```
  - [x] 1.5 When `optimisticExec` is set but `activeExecName` is empty (real data hasn't arrived), pass `optimisticExec` to `TransitionProgressBanner` as a new prop

- [x] Task 2: Extend TransitionProgressBanner to support optimistic state (AC: #1, #2, #5)
  - [x] 2.1 Add an `optimisticExec?: { name: string; mode: string } | null` prop to `TransitionProgressBannerProps`
  - [x] 2.2 When `optimisticExec` is provided and `execution` is null (real data not yet available), render the optimistic variant:
    - Show mode label from `optimisticExec.mode` (use `TRANSITIONS` or `ACTION_CONFIG` to map mode → human-readable label)
    - Show "Starting \<label\>..." text
    - Show `Spinner` (size="md") instead of `Progress` bar
    - No "View execution details" link (no real execution to navigate to yet)
  - [x] 2.3 When real `execution` prop becomes non-null, render the normal banner (real data takes over). The optimistic UI disappears naturally since parent clears `optimisticExec` state via the `useEffect` in Task 1.3
  - [x] 2.4 Ensure no flash: the banner stays rendered continuously — it transitions from optimistic content to real content without unmounting/remounting (same `<div>` container, different inner content)

- [x] Task 3: Mode-to-label mapping for optimistic banner (AC: #1)
  - [x] 3.1 In `TransitionProgressBanner.tsx` or in a utility, map `ExecutionMode` values to user-facing labels:
    ```ts
    const MODE_LABELS: Record<string, string> = {
      planned_migration: 'Planned Migration',
      disaster_failover: 'Failover',
      reprotect: 'Re-protect',
      failback: 'Failback',
      restore: 'Restore',
    };
    ```
    Check if `ACTION_CONFIG` or `TRANSITIONS` already provides this — reuse if so. The existing `TRANSITIONS` map in `DRLifecycleDiagram` maps by transition name, not by mode string. `ACTION_CONFIG` has `mode` → config with a label. Use `ACTION_CONFIG[resolveActionKey(mode)]?.keyword` or define a simple `MODE_LABELS` map (prefer the simpler approach)

- [x] Task 4: Handle edge case — stale optimistic state timeout (AC: #2)
  - [x] 4.1 Add a safety timeout (e.g., 30 seconds) that clears `optimisticExec` if the real watch update never arrives (guards against controller failure/extreme delay):
    ```ts
    useEffect(() => {
      if (!optimisticExec) return;
      const timer = setTimeout(() => setOptimisticExec(null), 30_000);
      return () => clearTimeout(timer);
    }, [optimisticExec]);
    ```
    After timeout, the optimistic banner disappears. The user can see execution status through the History tab or the real banner will appear when the controller eventually updates

- [x] Task 5: Unit tests (AC: #6)
  - [x] 5.1 Update `console-plugin/tests/components/DRPlanDetailPage.test.tsx`:
    - Test: after successful create, optimistic banner renders immediately with "Starting Failover..." (or appropriate mode)
    - Test: when plan watch updates with `activeExecution`, optimistic banner is replaced by real banner
    - Test: on create failure (mock `k8sCreate` rejects), no optimistic banner shown
    - Test: after navigation reset (unmount/remount), optimistic state is gone
  - [x] 5.2 Update `console-plugin/tests/components/TransitionProgressBanner.test.tsx` (or create if not exists):
    - Test: when `optimisticExec` provided and `execution` is null, renders "Starting \<mode\>..." with spinner
    - Test: when `optimisticExec` provided but `execution` is also provided, renders real execution data (real takes precedence)
    - Test: when `optimisticExec` is null and `execution` is null, nothing renders (no banner)
    - Test: banner remains mounted across transition from optimistic to real (no unmount flash)
    - jest-axe passes on optimistic banner state (spinner + text)
  - [x] 5.3 Add test for safety timeout:
    - Test: after 30s without watch update, optimistic state clears and banner disappears
    - Use `jest.useFakeTimers()` + `jest.advanceTimersByTime(30000)` for deterministic timing

- [x] Task 6: Verify build and lint (AC: #6)
  - [x] 6.1 Run `cd console-plugin && yarn build` — zero errors
  - [x] 6.2 Run `cd console-plugin && yarn test` — all tests pass
  - [x] 6.3 Run `cd console-plugin && yarn lint` — zero new lint errors (if lint target exists)

## Dev Notes

### Scope & Approach

This is a small, purely frontend story. All changes are within `console-plugin/`. The implementation adds **optimistic UI** — a common React pattern where local state provides immediate feedback before server-confirmed data arrives via the watch.

**Change pattern:** Extend state in DRPlanDetailPage → pass new prop to TransitionProgressBanner → add optimistic render path → clear on real data arrival → tests.

**Key insight:** The `useCreateDRExecution` hook already returns the created `DRExecution` object from `k8sCreate`. Currently `handleConfirm` does `await create(...)` but **discards the return value**. This story captures it for optimistic display.

### Critical: The Gap This Story Fills

Current flow:
1. User clicks Confirm → `k8sCreate` succeeds → modal closes
2. **GAP** — 1-3 seconds where no banner is visible
3. Controller updates `plan.status.activeExecution` + `plan.status.activeExecutionMode`
4. Watch delivers updated plan → `getEffectivePhase` diverges from `restPhase` → banner appears

With optimistic UI:
1. User clicks Confirm → `k8sCreate` succeeds → modal closes → **banner appears immediately via local state**
2. Controller updates plan
3. Watch delivers real data → local optimistic state cleared → banner continues with real data

### Critical: No Flash Between States

The transition from optimistic to real must be seamless. The banner component stays mounted the entire time. When `optimisticExec` is set, it renders optimistic content. When `execution` prop arrives (parent clears `optimisticExec`), it renders real content. Since the parent renders `<TransitionProgressBanner>` in both cases (controlled by `isInTransition` which is true in both states), there is no unmount/remount.

Implementation approach:
```tsx
// In DRPlanDetailPage
{isInTransition && (
  <TransitionProgressBanner 
    plan={plan} 
    execution={execution ?? null} 
    optimisticExec={optimisticExec}
  />
)}
```

Inside `TransitionProgressBanner`:
```tsx
if (!execution && optimisticExec) {
  // Render optimistic: "Starting <mode>..." + Spinner
} else if (execution) {
  // Render real data (existing logic)
} else {
  return null; // Neither — shouldn't happen given isInTransition guard
}
```

### Critical: isInTransition Must Include Optimistic State

Current `isInTransition`:
```ts
const isInTransition = effectivePhase !== null && effectivePhase !== restPhase;
```

This is false immediately after create because the plan hasn't been updated yet. Must expand:
```ts
const isInTransition = (effectivePhase !== null && effectivePhase !== restPhase) || optimisticExec !== null;
```

### Critical: Clearing Optimistic State

The `useEffect` that clears `optimisticExec` when `realActiveExec` appears must:
- Only clear when `realActiveExec` is truthy (non-empty string)
- Not cause infinite re-render (dependency array is `[realActiveExec, optimisticExec]`)
- Guard against stale closures — the effect captures the right state

```ts
useEffect(() => {
  if (optimisticExec && realActiveExec) {
    setOptimisticExec(null);
  }
}, [realActiveExec, optimisticExec]);
```

### Critical: Safety Timeout

If the controller is slow or fails, the optimistic banner shouldn't persist forever. A 30-second timeout clears it. This is a UX safety net — if the controller truly created the execution, the History tab or a page refresh will show it. The timeout prevents a stale "Starting..." state from confusing the user indefinitely.

### Critical: useCreateDRExecution Return Value

The hook already returns the created `DRExecution` object:
```ts
const result = await k8sCreate({ ... });
return result as DRExecution;
```

And `handleConfirm` calls `await create(...)`. Currently it ignores the return. Capture it:
```ts
const result = await create(plan.metadata!.name!, pendingAction);
setOptimisticExec({ name: result.metadata!.name!, mode: result.spec.mode });
```

### Critical: Spinner Import

The spinner for the optimistic banner should be PatternFly's `Spinner` component:
```ts
import { Spinner } from '@patternfly/react-core';
```
Use `size="md"` for inline visibility without dominating the banner.

### Critical: Accessibility

- The optimistic banner uses the same `aria-live` region as the existing `TransitionProgressBanner` (if it has one) — screen readers announce "Starting Failover..."
- The `Spinner` component has a default `aria-label="Loading"` — override with `aria-label="Execution starting"`
- jest-axe on the optimistic state verifies no violations

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `handleConfirm` with `create()` | `DRPlanDetailPage.tsx:61-70` | Extend to capture return value |
| `TransitionProgressBanner` rendering | `DRPlanDetailPage.tsx:101-103` | Add `optimisticExec` prop |
| `useEffect` for clearing state | Common React pattern | New effect in DRPlanDetailPage |
| `Spinner` in PatternFly | `@patternfly/react-core` | Import, use inline |
| `isInTransition` guard | `DRPlanDetailPage.tsx:50` | Extend condition |
| Test fixture (`makePlan`) | `AlertBannerSystem.test.tsx:8-36` | Same approach |
| Mock `k8sCreate` | `DRPlanDetailPage.test.tsx` (existing) | Already mocked for modal tests |
| `jest.useFakeTimers` | Common Jest pattern | For timeout test |
| jest-axe pattern | All component tests | Same `axe(container)` + `toHaveNoViolations` |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` | Add optimistic state, capture create result, new useEffect, extend isInTransition, pass prop | Core integration (6 lines changed, ~15 lines added) |
| `console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx` | Add optimisticExec prop, conditional render for optimistic vs real | Behavior enhancement (~20 lines added) |
| `console-plugin/tests/components/DRPlanDetailPage.test.tsx` | Add 4 tests for optimistic behavior | Test addition |
| `console-plugin/tests/components/TransitionProgressBanner.test.tsx` | Add/create 5 tests for optimistic prop handling | Test addition |

### Execution Order

1. Task 3 (mode labels) — utility needed by banner
2. Task 1 (DRPlanDetailPage state changes) — core optimistic wiring
3. Task 2 (TransitionProgressBanner enhancement) — render optimistic state
4. Task 4 (safety timeout) — edge case handling
5. Task 5 (tests) — verify all behavior
6. Task 6 (build + lint) — final validation

### Previous Story Learnings (from 8.4)

- **Watch-driven reactivity works seamlessly** — once `plan.status.conditions` or `plan.status.activeExecution` updates, React re-renders automatically. No polling or setTimeout needed for the "real data arrives" path
- **PatternFly CSS token fallback** — always use `var(--pf-t--global--*, var(--pf-v5-global--*))` dual-token pattern
- **Test fixture pattern** — `makePlan` / `makePlanWithSiteDiscovery` helpers work well; extend with `status.activeExecution` field for new tests
- **jest-axe** on every new state — include the optimistic banner state in the accessibility audit
- **No single-resource watch for aggregated APIs** — per project-context, `useDRExecution(name)` with `isList: false` may hang. The current code already has this in `useDRResources.ts` (line 62) using `isList: false`, which was noted as problematic. **However**, story 7.2/7.3/7.4 shipped with it working (the workaround in project-context notes `useDRExecutions()` list + `.find()` as the safe pattern). If `useDRExecution` is still used as-is, the optimistic state helps bridge the gap where the single-resource watch is slow to activate

### Project Structure Notes

- All changes within `console-plugin/` — separate TypeScript project
- No Go changes — this story is purely frontend
- No `make generate` or `make manifests` needed
- Follow existing component directory structure
- Import paths use relative `../../` (no aliases configured)

### References

- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx#L36-140] — Page shell (add optimistic state here)
- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx#L61-70] — handleConfirm (capture create return value)
- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx#L49-50] — isInTransition (extend with optimistic)
- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx#L100-103] — Banner render (pass new prop)
- [Source: console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx#L1-118] — Banner component (add optimistic path)
- [Source: console-plugin/src/hooks/useCreateDRExecution.ts#L1-53] — Hook returns DRExecution on success (capture it)
- [Source: console-plugin/src/hooks/useDRResources.ts#L57-89] — useDRExecution/useDRExecutions hooks
- [Source: console-plugin/src/models/types.ts] — DRPlan/DRExecution types
- [Source: _bmad-output/project-context.md] — PF6 token fallback, single-resource watch caveat, Console SDK constraints
- [Source: _bmad-output/planning-artifacts/epics.md#Story-8.5] — Epic requirements

## Dev Agent Record

### Agent Model Used
Opus 4.6 (Cursor)

### Debug Log References
- Initial `react-hooks/set-state-in-effect` lint error from `useEffect` clearing optimistic state — refactored to derived state (`effectiveOptimisticExec = realActiveExec ? null : optimisticExec`) instead of synchronous setState in effect body. Timeout effect kept since it uses `setTimeout` (async, not synchronous).

### Completion Notes List
- AC1: Optimistic banner appears immediately after `k8sCreate` succeeds via local `optimisticExec` state; renders "Starting \<mode\>..." with Spinner
- AC2: Smooth transition — `effectiveOptimisticExec` is derived as `null` when `realActiveExec` arrives, banner switches to real data without unmount; 30s safety timeout clears stale state
- AC3: Optimistic state is component-local `useState` — not persisted across navigations (unmount resets)
- AC4: On `k8sCreate` failure, `setOptimisticExec` is never called (catch block skips it)
- AC5: When real execution arrives, `useDRExecution(activeExecName)` provides live data with real `startTime`
- AC6: 14 new tests (6 DRPlanDetailPage + 8 TransitionProgressBanner) covering optimistic render, real data takeover, create failure, timeout, navigation reset, mode labels, accessibility; jest-axe passes; 561 total tests, 0 regressions
- Refactored Task 1.3 to use derived state pattern instead of `useEffect` + `setState` to avoid `react-hooks/set-state-in-effect` lint violation
- MODE_LABELS map added to TransitionProgressBanner.tsx (5 mode → label mappings)
- `data-testid="transition-progress-banner"` added to both optimistic and real banner for stable test selectors

### File List
- console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx (modified)
- console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx (modified)
- console-plugin/tests/components/DRPlanDetailPage.test.tsx (modified)
- console-plugin/tests/components/TransitionProgressBanner.test.tsx (modified)
- _bmad-output/implementation-artifacts/sprint-status.yaml (modified)
- _bmad-output/implementation-artifacts/8-5-optimistic-drexecution-detection-in-console.md (modified)

### Change Log
- 2026-05-02: Story 8.5 implemented — optimistic DRExecution detection in console. Added optimistic local state to DRPlanDetailPage, extended TransitionProgressBanner with optimistic/spinner render path, MODE_LABELS map, 30s safety timeout, derived state pattern for real-data takeover, 14 new tests with jest-axe accessibility. 561 tests pass (36 suites), 0 regressions, zero new lint errors, webpack production build clean.
- 2026-05-02: Code review fixes applied — (1) High: replaced useDRExecution single-resource watch with useDRExecutions list + .find() for aggregated API safety (removes isList:false hang risk); (2) Medium: replaced MODE_LABELS map with ACTION_CONFIG lookup, storing pendingAction instead of spec.mode so Failback/Restore show correct labels instead of their collapsed mode equivalents; added 2 new test cases for failback/restore actions. 563 total tests, 0 regressions.
