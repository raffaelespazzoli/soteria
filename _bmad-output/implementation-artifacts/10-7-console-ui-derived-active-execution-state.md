# Story 10.7: Console UI — Derived Active Execution State

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the Console plugin to derive active execution state from DRExecution resources instead of reading DRPlan status fields, and to display the new `phase` and `isActive` fields introduced in Stories 10.5–10.6,
So that the UI accurately reflects execution state from the source of truth with richer status information.

## Background

Stories 10.1–10.4 systematically removed `ActiveExecution` and `ActiveExecutionMode` from the Go-side `DRPlanStatus` struct and migrated all Go consumers to derive active execution from DRExecution resources via `soteria.io/plan-name` label-filtered queries.

**After 10.4:** The `activeExecution` and `activeExecutionMode` JSON keys no longer appear in the DRPlan API response. Any Console code that reads `plan.status?.activeExecution` or `plan.status?.activeExecutionMode` will see `undefined`.

**After 10.5–10.6:** `DRExecutionStatus` now carries explicit `phase` (Pending → Executing → Succeeded/PartiallySucceeded/Failed) and `isActive` (boolean, always serialized — no `omitempty`) fields. The `isActive` flag is the canonical way to identify in-flight executions, replacing the fragile `!exec.status?.result` pattern used in the original 10.5 design. The `phase` field provides human-readable lifecycle state for display in execution tables and detail pages.

This story migrates the Console plugin to:
1. Remove `activeExecution` / `activeExecutionMode` from the `DRPlanStatus` TypeScript interface
2. Add `phase` and `isActive` to the `DRExecutionStatus` TypeScript interface
3. Derive active execution by matching plans to their DRExecution with `isActive === true` from the existing `useDRExecutions` watch
4. Display execution `phase` in the ExecutionHistoryTable and ExecutionDetailPage

**Optimistic execution state** (Story 8.5) is already local React state — it does not read `plan.status.activeExecution` and continues to work unchanged. The only interaction point is the "real execution replaces optimistic" transition in `DRPlanDetailPage`, which currently checks `realActiveExec = plan?.status?.activeExecution` — this must be migrated to check the matched DRExecution from the watch instead.

## Acceptance Criteria

1. **AC1 — `DRPlanStatus` interface updated:** `activeExecution` and `activeExecutionMode` fields are removed from the `DRPlanStatus` TypeScript interface in `console-plugin/src/models/types.ts`. The `PreflightReport.activeExecution` field is NOT removed (different struct, report snapshot).

2. **AC2 — `DRExecutionStatus` interface updated:** `phase` (type `DRExecutionPhase`, a string union of `'Pending' | 'Executing' | 'Succeeded' | 'PartiallySucceeded' | 'Failed'`) and `isActive` (type `boolean`) are added to the `DRExecutionStatus` TypeScript interface in `console-plugin/src/models/types.ts`. Field `isActive` is non-optional (always present in API responses since it has no `omitempty` on the Go side).

3. **AC3 — `getEffectivePhase` accepts DRExecution parameter:** The utility function in `console-plugin/src/utils/drPlanUtils.ts` accepts an optional `DRExecution` (or `undefined`) as a second parameter instead of reading `plan.status.activeExecution` and `plan.status.activeExecutionMode`. When an active DRExecution is provided, transient phase derivation uses `exec.spec.mode`. When `undefined`, the rest phase is returned directly.

4. **AC4 — Active execution detection uses `isActive`:** All active-execution detection uses `e.status?.isActive === true` (not `!e.status?.result`). This applies to `findActiveExecution`, `buildActiveExecMap`, and inline `.find()` calls in `DRPlanDetailPage`.

5. **AC5 — DRPlan detail page derives from matched DRExecution:** `DRPlanDetailPage` derives `isInTransition`, the execution link, and the `realActiveExec` (for optimistic state clearance) from the DRExecution matched via `useDRExecutions(planName)` watch + `.find(e => e.status?.isActive === true)` — not from `plan.status.activeExecution`.

6. **AC6 — Dashboard derives effective phase per row from DRExecution index:** `DRDashboard` uses the existing `useDRExecutions()` full-cluster list watch. The `enrichPlans` function builds a `planName → DRExecution` index of active executions (`isActive === true`) and passes each plan's matched execution to `getEffectivePhase(plan, activeExec)`. Performance is O(plans + executions) via a single client-side index.

7. **AC7 — TransitionProgressBanner uses execution prop:** The banner derives `isRealTransition` from the `execution` prop and plan's rest phase (via the updated `getEffectivePhase`). The "View execution details" link uses `execution.metadata.name` instead of `plan.status.activeExecution`.

8. **AC8 — ExecutionHistoryTable shows Phase column:** The `ExecutionHistoryTable` component adds a Phase column (between Mode and Result, matching the Go table convertor column order). The phase value comes from `exec.status?.phase`. No additional formatting needed — raw string values match the Go `ExecutionPhase` constants.

9. **AC9 — ExecutionDetailPage uses Phase for header display:** The `ExecutionHeader` component displays the execution's `phase` (from `exec.status?.phase`) as a status indicator. This supplements the existing `result` display — when `result` is empty (in-flight), `phase` shows whether the execution is `Pending` or `Executing`.

10. **AC10 — Optimistic state transition intact:** The optimistic execution flow in `DRPlanDetailPage` continues to work. The `effectiveOptimisticExec` derivation checks whether a DRExecution with `isActive === true` exists (from the watch) instead of checking `plan?.status?.activeExecution`. Once a real active DRExecution appears in the watch, the optimistic state is cleared.

11. **AC11 — Tests updated:** All test files no longer set `activeExecution` / `activeExecutionMode` on plan status mocks. DRExecution test fixtures include `phase` and `isActive` fields. Tests provide DRExecution fixtures for the derived pattern. jest-axe accessibility audits pass. All tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Update TypeScript interfaces (AC: #1, #2)
  - [ ] 1.1 In `console-plugin/src/models/types.ts`, remove `activeExecution?: string` and `activeExecutionMode?: DRExecutionMode` from the `DRPlanStatus` interface
  - [ ] 1.2 Verify `PreflightReport.activeExecution` is NOT removed — it is a separate report snapshot field
  - [ ] 1.3 Add `DRExecutionPhase` type: `type DRExecutionPhase = 'Pending' | 'Executing' | 'Succeeded' | 'PartiallySucceeded' | 'Failed'`
  - [ ] 1.4 Add `phase?: DRExecutionPhase` and `isActive: boolean` to `DRExecutionStatus` interface (phase is optional for backward compat with old resources; isActive is non-optional as the Go side always serializes it)
  - [ ] 1.5 Verify `DRExecution` interface and `DRExecutionSpec` interface are unchanged — `spec.mode` and `spec.planName` remain

- [ ] Task 2: Rewrite `getEffectivePhase` to accept optional DRExecution (AC: #3)
  - [ ] 2.1 In `console-plugin/src/utils/drPlanUtils.ts`, change function signature from `getEffectivePhase(plan: DRPlan): EffectivePhase` to `getEffectivePhase(plan: DRPlan, activeExec?: DRExecution): EffectivePhase`
  - [ ] 2.2 Replace `if (!plan.status?.activeExecution)` with `if (!activeExec)`
  - [ ] 2.3 Replace `const mode = plan.status.activeExecutionMode` with `const mode = activeExec.spec.mode`
  - [ ] 2.4 Update the JSDoc comment: remove "derived from activeExecution + activeExecutionMode", add "derived from the active DRExecution resource (if any) matched by isActive flag"
  - [ ] 2.5 Add import for `DRExecution` type if not already imported

- [ ] Task 3: Add `findActiveExecution` and `buildActiveExecMap` helpers (AC: #4)
  - [ ] 3.1 In `console-plugin/src/utils/drPlanUtils.ts`, add `findActiveExecution(executions: DRExecution[]): DRExecution | undefined` — returns the first execution with `status?.isActive === true`
  - [ ] 3.2 Add `buildActiveExecMap(executions: DRExecution[]): Map<string, DRExecution>` — builds a `planName → DRExecution` map from active executions keyed by `exec.spec.planName`. Used by the dashboard for O(1) lookup per plan row

- [ ] Task 4: Update `DRPlanDetailPage` to derive from matched DRExecution (AC: #5, #10)
  - [ ] 4.1 Replace `const activeExecName = plan?.status?.activeExecution ?? ''` with: derive the active execution from the executions list: `const activeExec = executions.find(e => e.status?.isActive === true) ?? null`
  - [ ] 4.2 Remove the stale `activeExecName` → `.find(e => e.metadata?.name === activeExecName)` lookup — replace with `activeExec` directly
  - [ ] 4.3 Update `effectivePhase` computation: change `getEffectivePhase(plan)` to `getEffectivePhase(plan, activeExec ?? undefined)`
  - [ ] 4.4 Replace `const realActiveExec = plan?.status?.activeExecution` with `const realActiveExec = activeExec?.metadata?.name ?? ''` — this drives the optimistic state clearance
  - [ ] 4.5 Verify `effectiveOptimisticExec = realActiveExec ? null : optimisticExec` still works — when `realActiveExec` is truthy (non-empty string or non-null), optimistic is cleared
  - [ ] 4.6 Verify `isInTransition` logic remains correct: `(effectivePhase !== restPhase) || effectiveOptimisticExec !== null`
  - [ ] 4.7 Pass `activeExec` as `execution` prop to `TransitionProgressBanner` (replaces the old `execution` which was found by name match)

- [ ] Task 5: Update `TransitionProgressBanner` to use execution prop (AC: #7)
  - [ ] 5.1 Replace `getEffectivePhase(plan)` with `getEffectivePhase(plan, execution ?? undefined)` — pass the execution prop through
  - [ ] 5.2 Replace `plan.status?.activeExecution` in the "View execution details" link with `execution?.metadata?.name`
  - [ ] 5.3 Verify `isRealTransition = effectivePhase !== restPhase` still works correctly
  - [ ] 5.4 Verify `showOptimistic = !execution && !!optimisticExec` still works — when no real execution object exists but optimistic is set

- [ ] Task 6: Update `DRDashboard` to use DRExecution index for effective phase (AC: #6)
  - [ ] 6.1 In `enrichPlans` function, add a `buildActiveExecMap(executions)` call to build the active execution index
  - [ ] 6.2 Change `effectivePhase: getEffectivePhase(plan)` to `effectivePhase: getEffectivePhase(plan, activeExecMap.get(plan.metadata?.name ?? '') ?? undefined)` — passes the matched DRExecution for each plan
  - [ ] 6.3 Update `enrichPlans` signature to accept `executions: DRExecution[]` if not already
  - [ ] 6.4 Verify `enrichPlans` is called with the executions list at the call site

- [ ] Task 7: Update other callers of `getEffectivePhase` (AC: #3)
  - [ ] 7.1 `PlanHeader.tsx`: calls `getEffectivePhase(plan)`. Pass `effectivePhase` as a prop from `DRPlanDetailPage` (cleaner — avoids threading `activeExec` through)
  - [ ] 7.2 `DRLifecycleDiagram.tsx`: calls `getEffectivePhase(plan)`. Same approach — pass `effectivePhase` as a prop from `DRPlanDetailPage` (already computed there)
  - [ ] 7.3 `drPlanActions.ts` `getValidActions`: calls `getEffectivePhase(plan)`. Update to accept `effectivePhase` directly to decouple from DRExecution — preferred since callers already have the effective phase
  - [ ] 7.4 Compile-check: search for all `getEffectivePhase(` calls in `console-plugin/src/` and verify each is updated

- [ ] Task 8: Add Phase column to `ExecutionHistoryTable` (AC: #8)
  - [ ] 8.1 In `console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx` (or wherever the execution history columns are defined), add a "Phase" column after Mode and before Result
  - [ ] 8.2 Render `exec.status?.phase ?? ''` in the Phase cell
  - [ ] 8.3 Verify column order matches Go table convertor: Name, Plan, Mode, Phase, (Active — optional, boolean columns are less useful in UI tables), Result, Duration, Age

- [ ] Task 9: Update `ExecutionDetailPage` header with Phase (AC: #9)
  - [ ] 9.1 In `ExecutionHeader` component, display execution phase alongside existing result display
  - [ ] 9.2 When `result` is empty (in-flight execution), show `phase` as the primary status indicator (e.g., "Pending" or "Executing")
  - [ ] 9.3 When `result` is set, the existing result display takes precedence; phase is supplementary

- [ ] Task 10: Update `getEffectivePhase` tests (AC: #11)
  - [ ] 10.1 In `console-plugin/tests/utils/drPlanUtils.test.ts`, update all `getEffectivePhase` test cases:
    - Remove `activeExecution` and `activeExecutionMode` from `makePlan` overrides
    - Create DRExecution fixtures with `status: { phase: 'Executing', isActive: true }` for active cases
    - Pass the execution as the second argument: `getEffectivePhase(plan, exec)`
    - For idle/no-execution cases, call `getEffectivePhase(plan)` or `getEffectivePhase(plan, undefined)`
  - [ ] 10.2 Add tests for `findActiveExecution` and `buildActiveExecMap` — verify `isActive === true` filtering

- [ ] Task 11: Update `DRPlanDetailPage` tests (AC: #11)
  - [ ] 11.1 Update the optimistic execution test suite:
    - Remove `activeExecution` from updated plan fixtures
    - Add DRExecution fixtures with `isActive: true` and `phase: 'Executing'` to the `useDRExecutions` mock return value when testing "real execution replaces optimistic"
    - For "optimistic renders when no real execution", ensure `useDRExecutions` returns an empty list or only terminal executions (with `isActive: false`)
  - [ ] 11.2 Update any plan fixtures that set `activeExecution`/`activeExecutionMode` on `plan.status`

- [ ] Task 12: Update `TransitionProgressBanner` tests (AC: #11)
  - [ ] 12.1 Update all plan fixtures:
    - Remove `activeExecution` and `activeExecutionMode` from plan status mocks
    - Provide DRExecution fixtures with `phase` and `isActive` as the `execution` prop when testing active transition state
    - For idle/optimistic-only tests, pass `execution={null}`
  - [ ] 12.2 Verify the `getEffectivePhase` call inside the component uses the execution prop

- [ ] Task 13: Update `DRLifecycleDiagram` and `DRPlanActions` tests (AC: #11)
  - [ ] 13.1 In `DRLifecycleDiagram` tests: update plan fixtures, pass `effectivePhase` prop if component now receives it
  - [ ] 13.2 In `DRPlanActions` tests: update `makePlan` helper, pass effective phase directly

- [ ] Task 14: Update `ExecutionHistoryTable` and `ExecutionDetailPage` tests (AC: #8, #9, #11)
  - [ ] 14.1 Add Phase column assertions to `ExecutionHistoryTable` tests
  - [ ] 14.2 Add phase display assertions to `ExecutionHeader` / `ExecutionDetailPage` tests
  - [ ] 14.3 Test in-flight execution showing "Executing" phase when result is empty

- [ ] Task 15: Update remaining test files and final sweep (AC: #11)
  - [ ] 15.1 `Accessibility.test.tsx`: remove `activeExecution`/`activeExecutionMode` from plan status mocks
  - [ ] 15.2 `KeyboardAccessibility.test.tsx`: remove `activeExecution`/`activeExecutionMode` from plan status mocks
  - [ ] 15.3 Update all DRExecution test fixtures to include `phase` and `isActive` fields
  - [ ] 15.4 Run `rg 'activeExecution|activeExecutionMode' console-plugin/` to verify zero remaining references on plan status (expect only `PreflightReport.activeExecution` and `DRExecution.spec.mode`)

- [ ] Task 16: Run full test suite (AC: #11)
  - [ ] 16.1 Run `cd console-plugin && yarn test` — all tests pass with zero regressions
  - [ ] 16.2 Run `cd console-plugin && yarn lint` — zero lint errors
  - [ ] 16.3 Verify jest-axe accessibility audits pass

## Dev Notes

### Scope & Architecture

This story touches the Console plugin only — no Go code changes. After Stories 10.1–10.6, the Go API:
- No longer includes `activeExecution`/`activeExecutionMode` on DRPlan status responses (10.4)
- Now includes `phase` (string) and `isActive` (boolean, always serialized) on DRExecution status responses (10.6)
- Has `IsTerminal()` method on `DRExecutionStatus` for Go callers (10.5)

**Key design change from the original 10.5:** The `isActive` field replaces the fragile `!exec.status?.result` pattern for active-execution detection. Since `isActive` is always serialized (no `omitempty` on Go side), it's safe to check `e.status?.isActive === true` — it will be `true` for in-flight and explicitly `false` for terminal executions.

**NOT in scope:**
- Any Go code changes (completed in 10.1–10.6)
- `PreflightReport.activeExecution` — this is a snapshot field populated server-side. It stays on the TypeScript interface
- DRExecution spec interface changes — the execution model is unchanged

### Critical: Active Execution Detection with `isActive`

**Old pattern (fragile):**
```typescript
const activeExec = executions.find(e => !e.status?.result) ?? null;
```

**New pattern (explicit):**
```typescript
const activeExec = executions.find(e => e.status?.isActive === true) ?? null;
```

The `isActive` field is always present in the JSON response (Go serializes `false` because there's no `omitempty`). Using strict `=== true` is a defensive guard against `undefined` from very old resources that predate Story 10.6 (if any survive without reconciler refresh).

### Critical: `getEffectivePhase` Signature Change

**Current (`console-plugin/src/utils/drPlanUtils.ts:13-36`):**

```typescript
export function getEffectivePhase(plan: DRPlan): EffectivePhase {
  const phase = plan.status?.phase;
  if (!plan.status?.activeExecution) return (phase as RestPhase) ?? 'SteadyState';
  const mode = plan.status.activeExecutionMode;
  switch (phase) {
    case 'SteadyState':
      return mode === 'planned_migration' || mode === 'disaster' ? 'FailingOver' : 'SteadyState';
    // ...
  }
}
```

**New:**

```typescript
export function getEffectivePhase(plan: DRPlan, activeExec?: DRExecution): EffectivePhase {
  const phase = plan.status?.phase;
  if (!activeExec) return (phase as RestPhase) ?? 'SteadyState';
  const mode = activeExec.spec.mode;
  switch (phase) {
    case 'SteadyState':
      return mode === 'planned_migration' || mode === 'disaster' ? 'FailingOver' : 'SteadyState';
    // ... identical switch cases
  }
}
```

The switch logic is identical — only the data source for `mode` changes from `plan.status.activeExecutionMode` to `activeExec.spec.mode`.

### Critical: DRPlanDetailPage Execution Matching

**Current pattern (lines 37–55):**

```typescript
const activeExecName = plan?.status?.activeExecution ?? '';
const execution = activeExecName
  ? executions.find(e => e.metadata?.name === activeExecName) ?? null
  : null;
const realActiveExec = plan?.status?.activeExecution;
```

**New pattern:**

```typescript
const activeExec = executions.find(e => e.status?.isActive === true) ?? null;
const effectivePhase = plan ? getEffectivePhase(plan, activeExec ?? undefined) : null;
const realActiveExec = activeExec?.metadata?.name ?? '';
const effectiveOptimisticExec = realActiveExec ? null : optimisticExec;
```

### Critical: Execution Phase Display

**ExecutionHistoryTable** currently shows: Date, Mode, Result, Duration, RPO, Triggered By. Add Phase column between Mode and Result to mirror the Go table convertor column order.

**ExecutionHeader** in `ExecutionDetailPage` currently shows result via `ExecutionResultBadge`. For in-flight executions (no result), display the `phase` instead (e.g., a Label with "Pending" or "Executing").

### Critical: Dashboard `enrichPlans` with Execution Index

**New:**

```typescript
function enrichPlans(plans: DRPlan[], executions: DRExecution[]): EnrichedPlan[] {
  const latestByPlan = buildLatestExecutionMap(executions);
  const activeByPlan = buildActiveExecMap(executions);
  return plans.map((plan) => ({
    plan,
    effectivePhase: getEffectivePhase(plan, activeByPlan.get(plan.metadata?.name ?? '')),
    // ...
  }));
}
```

The `buildActiveExecMap` filters by `exec.status?.isActive === true` and builds a `Map<string, DRExecution>` indexed by `exec.spec.planName`.

### Critical: Callers of `getEffectivePhase` — Full List

| File | Usage | Migration |
|------|-------|-----------|
| `drPlanUtils.ts` | Definition | Signature change |
| `DRPlanDetailPage.tsx` | `getEffectivePhase(plan)` | Pass `activeExec` |
| `TransitionProgressBanner.tsx` | `getEffectivePhase(plan)` | Pass `execution` prop |
| `DRDashboard.tsx` (enrichPlans) | `getEffectivePhase(plan)` | Pass from `activeByPlan` map |
| `PlanHeader.tsx` | `getEffectivePhase(plan)` | Receive `effectivePhase` prop from parent |
| `DRLifecycleDiagram.tsx` | `getEffectivePhase(plan)` | Receive `effectivePhase` prop from parent |
| `drPlanActions.ts` (getValidActions) | `getEffectivePhase(plan)` | Accept `effectivePhase` param |

### Critical: Optimistic Execution State

The optimistic flow (Story 8.5) is local React state in `DRPlanDetailPage`. After migration, the "real execution detected" check changes from `plan?.status?.activeExecution` to `activeExec?.metadata?.name`. The watch should detect the newly created DRExecution even faster because:
- The execution is created immediately via `k8sCreate`
- `useDRExecutions(planName)` watches DRExecutions with `soteria.io/plan-name` label selector
- The label is stamped server-side in `PrepareForCreate` (Story 10.1)
- `isActive: true` is set in `PrepareForCreate` (Story 10.6)

### TypeScript Type Additions

```typescript
export type DRExecutionPhase = 'Pending' | 'Executing' | 'Succeeded' | 'PartiallySucceeded' | 'Failed';

export interface DRExecutionStatus {
  phase?: DRExecutionPhase;
  isActive: boolean;
  result?: DRExecutionResult;
  waves?: WaveStatus[];
  startTime?: string;
  completionTime?: string;
  conditions?: Condition[];
}
```

Note: `phase` is optional (`?`) because old DRExecution resources without the field deserialize to empty/undefined. `isActive` is non-optional because the Go side always serializes it (no `omitempty`), but for very old resources a defensive `=== true` check handles the `undefined` case.

### Critical: `useDRExecution` (Singular) — Dead Code

`useDRExecution(name)` is defined in `useDRResources.ts` but is not imported anywhere. `ExecutionDetailPage` uses `useDRExecutions()` with `.find()` instead (due to OCP Console SDK platform constraints — single-resource watches fail against aggregated APIs). Do NOT introduce new usages.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `useDRExecutions(planName?)` label-filtered list watch | `console-plugin/src/hooks/useDRResources.ts:57-64` | Already used in detail page and dashboard |
| `buildLatestExecutionMap` client-side index | `console-plugin/src/utils/drPlanUtils.ts:70-92` | Follow for `buildActiveExecMap` |
| Active execution check via `isActive` | Go Stories 10.5-10.6 | Mirror as `e.status?.isActive === true` |
| `effectiveOptimisticExec` clearance pattern | `DRPlanDetailPage.tsx:55` | Keep pattern, change data source |
| Props-down pattern for computed values | PatternFly convention, `PhaseBadge` | Follow for `effectivePhase` prop drilling |
| PatternFly Label for status display | `ExecutionResultBadge`, `PhaseBadge` | Follow for execution Phase display |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `console-plugin/src/models/types.ts` | Remove 2 DRPlanStatus fields, add `DRExecutionPhase` type + 2 DRExecutionStatus fields | ~5 lines changed |
| `console-plugin/src/utils/drPlanUtils.ts` | Rewrite `getEffectivePhase` signature, add `findActiveExecution` + `buildActiveExecMap` | ~25 lines changed/added |
| `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` | Derive from matched DRExecution via `isActive`, pass `effectivePhase` down | ~15 lines changed |
| `console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx` | Use execution prop for effective phase and link | ~10 lines changed |
| `console-plugin/src/components/DRPlanDetail/PlanHeader.tsx` | Accept `effectivePhase` prop | ~5 lines changed |
| `console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx` | Accept `effectivePhase` prop | ~5 lines changed |
| `console-plugin/src/components/DRDashboard/DRDashboard.tsx` | Build active exec map, pass to `getEffectivePhase` | ~5 lines changed |
| `console-plugin/src/utils/drPlanActions.ts` | Accept `effectivePhase` param in `getValidActions` | ~5 lines changed |
| `console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx` | Add Phase column | ~5 lines changed |
| `console-plugin/src/components/ExecutionDetail/ExecutionHeader.tsx` | Display execution phase | ~10 lines changed |
| `tests/utils/drPlanUtils.test.ts` | Rewrite `getEffectivePhase` tests, add `findActiveExecution`/`buildActiveExecMap` tests | ~45 lines changed |
| `tests/components/DRPlanDetailPage.test.tsx` | Update optimistic tests, remove plan status mocks, add isActive fixtures | ~35 lines changed |
| `tests/components/TransitionProgressBanner.test.tsx` | Remove plan status mocks, use execution fixtures with phase/isActive | ~30 lines changed |
| `tests/components/DRLifecycleDiagram.test.tsx` | Update plan mocks, pass `effectivePhase` prop | ~20 lines changed |
| `tests/components/DRPlanActions.test.tsx` | Update `makePlan` helper, pass effective phase | ~15 lines changed |
| `tests/components/ExecutionHistoryTable.test.tsx` | Phase column assertions | ~10 lines changed |
| `tests/components/ExecutionDetailPage.test.tsx` | Phase display assertions | ~10 lines changed |
| `tests/components/Accessibility.test.tsx` | Remove plan status fields, add DRExecution status fields | ~5 lines changed |
| `tests/components/KeyboardAccessibility.test.tsx` | Remove plan status fields | ~5 lines changed |

### Execution Order

1. Task 1 (update TypeScript interfaces — both DRPlanStatus removals and DRExecutionStatus additions) — root cause of all type changes
2. Task 2 (rewrite `getEffectivePhase`) — core derivation change
3. Task 3 (add `findActiveExecution` + `buildActiveExecMap` with `isActive` filter) — shared utilities
4. Task 7 (update downstream callers: `PlanHeader`, `DRLifecycleDiagram`, `drPlanActions`) — fix compile errors
5. Task 4 (update `DRPlanDetailPage`) — primary consumer
6. Task 5 (update `TransitionProgressBanner`) — depends on Task 4
7. Task 6 (update `DRDashboard` with execution index) — independent of detail page
8. Task 8 (add Phase column to ExecutionHistoryTable) — new feature
9. Task 9 (update ExecutionDetailPage header with Phase) — new feature
10. Tasks 10–15 (update all tests) — after production code compiles
11. Task 16 (run full test suite) — final gate

### Previous Story Learnings (from 10.1–10.6)

- **Story 10.1** stamps `soteria.io/plan-name` label server-side in `PrepareForCreate`
- **Story 10.2** established the `buildActiveExecIndex` pattern in Go — mirror as `buildActiveExecMap` in TypeScript
- **Story 10.4** removed the Go struct fields — API response no longer includes `activeExecution`/`activeExecutionMode` on DRPlan status
- **Story 10.5** added `IsTerminal()` method on `DRExecutionStatus` — centralized terminal check
- **Story 10.6** added `phase` and `isActive` fields — `isActive` is always serialized (no `omitempty`), `phase` is `omitempty`. `PrepareForCreate` sets `Pending`/`true`, reconciler sets `Executing`, terminal paths set the terminal phase + `false`
- **Story 8.5** established the optimistic execution pattern — local component state, 30s timeout
- **OCP Console SDK constraint:** Single-resource watches fail against aggregated APIs. Always use list watch + `.find()`
- **React Router v5** on OCP 4.20: Use `useHistory`, `useParams` from `react-router-dom`
- **PatternFly 6 CSS tokens:** Use `--pf-t--global--*` with `--pf-v5-global--*` fallback

### Git Intelligence (Recent Patterns)

Recent commits follow a single-commit-per-story pattern with the story number as prefix. Console plugin stories typically modify `~8-12 src files` and `~6-8 test files`. jest-axe accessibility is mandatory.

### Project Structure Notes

- TypeScript types: `console-plugin/src/models/types.ts`
- Plan utilities: `console-plugin/src/utils/drPlanUtils.ts`
- Plan actions: `console-plugin/src/utils/drPlanActions.ts`
- Plan detail page: `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx`
- Transition banner: `console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx`
- Plan header: `console-plugin/src/components/DRPlanDetail/PlanHeader.tsx`
- Lifecycle diagram: `console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx`
- Dashboard: `console-plugin/src/components/DRDashboard/DRDashboard.tsx`
- Execution history: `console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx`
- Execution detail: `console-plugin/src/components/ExecutionDetail/ExecutionDetailPage.tsx`
- Execution header: `console-plugin/src/components/ExecutionDetail/ExecutionHeader.tsx`
- DR hooks: `console-plugin/src/hooks/useDRResources.ts`
- Tests: `console-plugin/tests/` — component tests in `tests/components/`, utility tests in `tests/utils/`

### References

- [Source: console-plugin/src/models/types.ts] — DRPlanStatus and DRExecutionStatus interfaces
- [Source: console-plugin/src/utils/drPlanUtils.ts] — getEffectivePhase, buildLatestExecutionMap
- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx] — Execution matching and optimistic state
- [Source: console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx] — Transition detection
- [Source: console-plugin/src/components/DRDashboard/DRDashboard.tsx] — enrichPlans with getEffectivePhase per plan
- [Source: console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx] — Execution history table columns
- [Source: console-plugin/src/components/ExecutionDetail/ExecutionHeader.tsx] — Execution header display
- [Source: _bmad-output/implementation-artifacts/10-5-drexecution-isterminal-method.md] — Story 10.5: IsTerminal method
- [Source: _bmad-output/implementation-artifacts/10-6-drexecution-phase-isactive-fields.md] — Story 10.6: Phase and IsActive fields
- [Source: _bmad-output/implementation-artifacts/10-4-remove-activeexecution-fields-from-drplanstatus.md] — Story 10.4: Go struct field removal
- [Source: _bmad-output/project-context.md] — Critical rules, Console SDK constraints, PatternFly 6

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
