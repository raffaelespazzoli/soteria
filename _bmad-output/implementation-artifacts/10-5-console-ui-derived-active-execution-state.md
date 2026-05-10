# Story 10.5: Console UI — Derived Active Execution State

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the Console plugin to derive active execution state from DRExecution resources instead of reading DRPlan status fields,
So that the UI accurately reflects execution state from the source of truth.

## Background

Stories 10.1–10.4 systematically removed `ActiveExecution` and `ActiveExecutionMode` from the Go-side `DRPlanStatus` struct and migrated all Go consumers to derive active execution from DRExecution resources via `soteria.io/plan-name` label-filtered queries.

**After 10.4:** The `activeExecution` and `activeExecutionMode` JSON keys no longer appear in the DRPlan API response. Any Console code that reads `plan.status?.activeExecution` or `plan.status?.activeExecutionMode` will see `undefined`.

This story migrates the Console plugin to derive active execution state by matching plans to their non-terminal DRExecution from the existing `useDRExecutions` watch. The pattern mirrors the Go-side changes: a single LIST of DRExecutions is indexed client-side by `spec.planName` to find the active (non-terminal) execution for each plan.

**Optimistic execution state** (Story 8.5) is already local React state — it does not read `plan.status.activeExecution` and continues to work unchanged. The only interaction point is the "real execution replaces optimistic" transition in `DRPlanDetailPage`, which currently checks `realActiveExec = plan?.status?.activeExecution` — this must be migrated to check the matched DRExecution from the watch instead.

## Acceptance Criteria

1. **AC1 — `DRPlanStatus` interface updated:** `activeExecution` and `activeExecutionMode` fields are removed from the `DRPlanStatus` TypeScript interface in `console-plugin/src/models/types.ts`. The `PreflightReport.activeExecution` field is NOT removed (different struct, report snapshot).

2. **AC2 — `getEffectivePhase` accepts DRExecution parameter:** The utility function in `console-plugin/src/utils/drPlanUtils.ts` accepts an optional `DRExecution` (or `undefined`) as a second parameter instead of reading `plan.status.activeExecution` and `plan.status.activeExecutionMode`. When a non-terminal DRExecution is provided, transient phase derivation uses `exec.spec.mode`. When `undefined`, the rest phase is returned directly.

3. **AC3 — DRPlan detail page derives from matched DRExecution:** `DRPlanDetailPage` derives `isInTransition`, the execution link, and the `realActiveExec` (for optimistic state clearance) from the DRExecution matched via `useDRExecutions(planName)` watch + `.find(e => !e.status?.result)` — not from `plan.status.activeExecution`.

4. **AC4 — Dashboard derives effective phase per row from DRExecution index:** `DRDashboard` uses the existing `useDRExecutions()` full-cluster list watch. The `enrichPlans` function builds a `planName → DRExecution` index of non-terminal executions and passes each plan's matched execution to `getEffectivePhase(plan, activeExec)`. Performance is O(plans + executions) via a single client-side index.

5. **AC5 — TransitionProgressBanner uses execution prop:** The banner derives `isRealTransition` from the `execution` prop and plan's rest phase (via the updated `getEffectivePhase`). The "View execution details" link uses `execution.metadata.name` instead of `plan.status.activeExecution`.

6. **AC6 — Optimistic state transition intact:** The optimistic execution flow in `DRPlanDetailPage` continues to work. The `effectiveOptimisticExec` derivation checks whether the matched DRExecution (from the watch) exists instead of checking `plan?.status?.activeExecution`. Once a real non-terminal DRExecution appears in the watch, the optimistic state is cleared.

7. **AC7 — Tests updated:** All test files no longer set `activeExecution` / `activeExecutionMode` on plan status mocks. Tests provide DRExecution fixtures for the derived pattern. jest-axe accessibility audits pass. All tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Remove `activeExecution` and `activeExecutionMode` from `DRPlanStatus` interface (AC: #1)
  - [ ] 1.1 In `console-plugin/src/models/types.ts`, remove `activeExecution?: string` (line ~89) and `activeExecutionMode?: DRExecutionMode` (line ~90) from the `DRPlanStatus` interface
  - [ ] 1.2 Verify `PreflightReport.activeExecution` (line ~140) is NOT removed — it is a separate report snapshot field
  - [ ] 1.3 Verify `DRExecution` interface and `DRExecutionSpec` interface are unchanged — `spec.mode` and `spec.planName` remain

- [ ] Task 2: Rewrite `getEffectivePhase` to accept optional DRExecution (AC: #2)
  - [ ] 2.1 In `console-plugin/src/utils/drPlanUtils.ts`, change function signature from `getEffectivePhase(plan: DRPlan): EffectivePhase` to `getEffectivePhase(plan: DRPlan, activeExec?: DRExecution): EffectivePhase`
  - [ ] 2.2 Replace `if (!plan.status?.activeExecution)` with `if (!activeExec)`
  - [ ] 2.3 Replace `const mode = plan.status.activeExecutionMode` with `const mode = activeExec.spec.mode`
  - [ ] 2.4 Update the JSDoc comment: remove "derived from activeExecution + activeExecutionMode", add "derived from the active DRExecution resource (if any) matched by soteria.io/plan-name label"
  - [ ] 2.5 Add import for `DRExecution` type if not already imported

- [ ] Task 3: Add `findActiveExecution` and `buildActiveExecMap` helpers (AC: #3, #4)
  - [ ] 3.1 In `console-plugin/src/utils/drPlanUtils.ts`, add `findActiveExecution(executions: DRExecution[]): DRExecution | undefined` — returns the first execution with no `status.result` (non-terminal)
  - [ ] 3.2 Add `buildActiveExecMap(executions: DRExecution[]): Map<string, DRExecution>` — builds a `planName → DRExecution` map from non-terminal executions keyed by `exec.spec.planName`. Used by the dashboard for O(1) lookup per plan row

- [ ] Task 4: Update `DRPlanDetailPage` to derive from matched DRExecution (AC: #3, #6)
  - [ ] 4.1 Replace `const activeExecName = plan?.status?.activeExecution ?? ''` (line ~39) with: derive the active execution from the executions list: `const activeExec = executions.find(e => !e.status?.result) ?? null`
  - [ ] 4.2 Remove the stale `activeExecName` → `.find(e => e.metadata?.name === activeExecName)` lookup — replace with `activeExec` directly
  - [ ] 4.3 Update `effectivePhase` computation: change `getEffectivePhase(plan)` (line ~49) to `getEffectivePhase(plan, activeExec ?? undefined)`
  - [ ] 4.4 Replace `const realActiveExec = plan?.status?.activeExecution` (line ~53) with `const realActiveExec = activeExec?.metadata?.name ?? ''` — this drives the optimistic state clearance
  - [ ] 4.5 Verify `effectiveOptimisticExec = realActiveExec ? null : optimisticExec` still works — when `realActiveExec` is truthy (non-empty string or non-null), optimistic is cleared
  - [ ] 4.6 Verify `isInTransition` logic remains correct: `(effectivePhase !== restPhase) || effectiveOptimisticExec !== null`
  - [ ] 4.7 Pass `activeExec` as `execution` prop to `TransitionProgressBanner` (replaces the old `execution` which was found by name match)

- [ ] Task 5: Update `TransitionProgressBanner` to use execution prop (AC: #5)
  - [ ] 5.1 Replace `getEffectivePhase(plan)` (line ~29) with `getEffectivePhase(plan, execution ?? undefined)` — pass the execution prop through
  - [ ] 5.2 Replace `plan.status?.activeExecution` in the "View execution details" link (lines ~102, ~134–141) with `execution?.metadata?.name`
  - [ ] 5.3 Verify `isRealTransition = effectivePhase !== restPhase` still works correctly
  - [ ] 5.4 Verify `showOptimistic = !execution && !!optimisticExec` still works — when no real execution object exists but optimistic is set

- [ ] Task 6: Update `DRDashboard` to use DRExecution index for effective phase (AC: #4)
  - [ ] 6.1 In `enrichPlans` function (`console-plugin/src/components/DRDashboard/DRDashboard.tsx`, lines ~53–62), add a `buildActiveExecMap(executions)` call to build the non-terminal execution index
  - [ ] 6.2 Change `effectivePhase: getEffectivePhase(plan)` to `effectivePhase: getEffectivePhase(plan, activeExecMap.get(plan.metadata?.name ?? '') ?? undefined)` — passes the matched DRExecution for each plan
  - [ ] 6.3 Update `enrichPlans` signature to accept `executions: DRExecution[]` (it may already — verify; the `useDRExecutions()` call at line ~142 provides the list)
  - [ ] 6.4 Verify `enrichPlans` is called with the executions list at the call site (~line 148 or wherever `enrichPlans(plans, executions)` is called)

- [ ] Task 7: Update other callers of `getEffectivePhase` (AC: #2)
  - [ ] 7.1 `PlanHeader.tsx` (line ~21, 45): calls `getEffectivePhase(plan)`. This is inside `DRPlanDetailPage` render tree where `activeExec` is available. Either: (a) pass `effectivePhase` as a prop from `DRPlanDetailPage` (cleaner), or (b) pass `activeExec` to `PlanHeader` and call `getEffectivePhase(plan, activeExec)` there. Option (a) preferred — `PlanHeader` already receives `plan`, add an `effectivePhase` prop
  - [ ] 7.2 `DRLifecycleDiagram.tsx` (lines ~225–228): calls `getEffectivePhase(plan)`. Same approach — pass `effectivePhase` as a prop from `DRPlanDetailPage` (already computed there). Verify the component already receives `plan` as a prop; add `effectivePhase` prop
  - [ ] 7.3 `drPlanActions.ts` `getValidActions` (line ~48–50): calls `getEffectivePhase(plan)`. This is used by: (a) `DRPlanActions` component and (b) `DRLifecycleDiagram`. Update `getValidActions` signature to accept an optional `activeExec?: DRExecution` parameter and pass it through to `getEffectivePhase`. Alternatively, accept `effectivePhase` directly to decouple from DRExecution — preferred since callers already have the effective phase
  - [ ] 7.4 Compile-check: search for all `getEffectivePhase(` calls in `console-plugin/src/` and verify each is updated

- [ ] Task 8: Update `getEffectivePhase` tests (AC: #7)
  - [ ] 8.1 In `console-plugin/tests/utils/drPlanUtils.test.ts`, update all `getEffectivePhase` test cases:
    - Remove `activeExecution` and `activeExecutionMode` from `makePlan` overrides
    - Instead, create a `DRExecution` fixture: `{ spec: { mode: '<mode>', planName: '<plan>' }, status: {} }` for active execution cases
    - Pass the execution as the second argument: `getEffectivePhase(plan, exec)`
    - For idle/no-execution cases, call `getEffectivePhase(plan)` or `getEffectivePhase(plan, undefined)`
  - [ ] 8.2 Update the `makePlan` helper if it accepts `activeExecution`/`activeExecutionMode` overrides — remove these parameters. If it's used by other test files, update all call sites

- [ ] Task 9: Update `DRPlanDetailPage` tests (AC: #7)
  - [ ] 9.1 In `console-plugin/tests/components/DRPlanDetailPage.test.tsx`, update the optimistic execution test suite (~287–419):
    - Remove `activeExecution` from updated plan fixtures (line ~326–331)
    - Instead, add a DRExecution fixture to the `useDRExecutions` mock return value when testing "real execution replaces optimistic"
    - For "optimistic renders when no real execution", ensure `useDRExecutions` returns an empty list or only terminal executions
  - [ ] 9.2 Update any plan fixtures that set `activeExecution`/`activeExecutionMode` on `plan.status`

- [ ] Task 10: Update `TransitionProgressBanner` tests (AC: #7)
  - [ ] 10.1 In `console-plugin/tests/components/TransitionProgressBanner.test.tsx`, update all plan fixtures (~63–150, ~233–234, ~281–282):
    - Remove `activeExecution` and `activeExecutionMode` from plan status mocks
    - Provide a DRExecution fixture as the `execution` prop when testing active transition state
    - For idle/optimistic-only tests, pass `execution={null}`
  - [ ] 10.2 Verify the `getEffectivePhase` call inside the component uses the execution prop

- [ ] Task 11: Update `DRLifecycleDiagram` tests (AC: #7)
  - [ ] 11.1 In `console-plugin/tests/components/DRLifecycleDiagram.test.tsx`, update plan fixtures (~99–241):
    - Remove `activeExecution` and `activeExecutionMode` from plan status
    - If the component now receives `effectivePhase` as a prop, pass it directly in tests
    - If it still calls `getEffectivePhase`, provide an execution prop or mock

- [ ] Task 12: Update `DRPlanActions` tests (AC: #7)
  - [ ] 12.1 In `console-plugin/tests/components/DRPlanActions.test.tsx`, update `makePlan` helper (lines ~8–21):
    - Remove `activeExecution?` and `activeExecutionMode?` parameters
    - Transient-phase tests (~60–71) should pass the effective phase directly if `getValidActions` is updated to accept it
  - [ ] 12.2 If `getValidActions` now accepts `effectivePhase` instead of computing it, update test calls accordingly

- [ ] Task 13: Update remaining test files (AC: #7)
  - [ ] 13.1 `Accessibility.test.tsx` (line ~71–72): remove `activeExecution`/`activeExecutionMode` from plan status mocks
  - [ ] 13.2 `KeyboardAccessibility.test.tsx` (line ~42–43): remove `activeExecution`/`activeExecutionMode` from plan status mocks
  - [ ] 13.3 Run `rg 'activeExecution|activeExecutionMode' console-plugin/` to verify zero remaining references to these fields on plan status (expect only `PreflightReport.activeExecution` and `DRExecution.spec.mode` related hits)

- [ ] Task 14: Run full test suite (AC: #7)
  - [ ] 14.1 Run `cd console-plugin && yarn test` — all tests pass with zero regressions
  - [ ] 14.2 Run `cd console-plugin && yarn lint` — zero lint errors
  - [ ] 14.3 Verify jest-axe accessibility audits pass (they run as part of the test suite)

## Dev Notes

### Scope & Architecture

This story touches the Console plugin only — no Go code changes. After Stories 10.1–10.4, the Go API no longer includes `activeExecution` / `activeExecutionMode` in DRPlan status responses. The Console must derive this state from DRExecution resources.

**Key insight:** The Console already watches DRExecutions via `useDRExecutions()` — both on the dashboard (full-cluster LIST) and detail page (plan-filtered LIST). The migration connects these existing watches to the phase derivation and transition detection logic.

**NOT in scope:**
- Any Go code changes (completed in 10.1–10.4)
- `PreflightReport.activeExecution` — this is a snapshot field populated server-side from `CompositionInput.ActiveExecution` (Story 10.2). It stays on the TypeScript interface
- DRExecution spec/status interface changes — the execution model is unchanged

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
const [plan, planLoaded, planError] = useDRPlan(name!);
const [executions, executionsLoaded] = useDRExecutions(name!);
const activeExecName = plan?.status?.activeExecution ?? '';
const execution = activeExecName
  ? executions.find(e => e.metadata?.name === activeExecName) ?? null
  : null;
// ...
const realActiveExec = plan?.status?.activeExecution;
const effectiveOptimisticExec = realActiveExec ? null : optimisticExec;
```

**New pattern:**

```typescript
const [plan, planLoaded, planError] = useDRPlan(name!);
const [executions, executionsLoaded] = useDRExecutions(name!);
const activeExec = executions.find(e => !e.status?.result) ?? null;
// ...
const effectivePhase = plan ? getEffectivePhase(plan, activeExec ?? undefined) : null;
const realActiveExec = activeExec?.metadata?.name ?? '';
const effectiveOptimisticExec = realActiveExec ? null : optimisticExec;
```

**Key changes:**
- `activeExec` is found by checking for non-terminal status (`!e.status?.result`) instead of name-matching from `plan.status.activeExecution`
- `realActiveExec` is derived from the found execution object, not plan status
- `useDRExecutions(name!)` with plan name already filters by `soteria.io/plan-name` label — the filter is set server-side in `PrepareForCreate` (Story 10.1)

### Critical: Dashboard `enrichPlans` with Execution Index

**Current (`DRDashboard.tsx:53-62`):**

```typescript
function enrichPlans(plans: DRPlan[], executions: DRExecution[]): EnrichedPlan[] {
  const latestByPlan = buildLatestExecutionMap(executions);
  return plans.map((plan) => ({
    plan,
    effectivePhase: getEffectivePhase(plan),
    // ...
  }));
}
```

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

The `buildActiveExecMap` builds a `Map<string, DRExecution>` indexed by `exec.spec.planName` — O(executions) build, O(1) per-plan lookup.

### Critical: Callers of `getEffectivePhase` — Full List

All `getEffectivePhase` call sites in `console-plugin/src/`:

| File | Usage | Migration |
|------|-------|-----------|
| `drPlanUtils.ts` | Definition | Signature change |
| `DRPlanDetailPage.tsx` | `getEffectivePhase(plan)` | Pass `activeExec` |
| `TransitionProgressBanner.tsx` | `getEffectivePhase(plan)` | Pass `execution` prop |
| `DRDashboard.tsx` (enrichPlans) | `getEffectivePhase(plan)` | Pass from `activeByPlan` map |
| `PlanHeader.tsx` | `getEffectivePhase(plan)` | Receive `effectivePhase` prop from parent |
| `DRLifecycleDiagram.tsx` | `getEffectivePhase(plan)` | Receive `effectivePhase` prop from parent |
| `drPlanActions.ts` (getValidActions) | `getEffectivePhase(plan)` | Accept `effectivePhase` param or `activeExec` param |

**Recommended approach for `PlanHeader` and `DRLifecycleDiagram`:** Since both are rendered inside `DRPlanDetailPage`, which already computes `effectivePhase`, pass it as a prop rather than threading `activeExec` through. This reduces the number of components that need to know about `DRExecution`.

**Recommended approach for `getValidActions`:** Change the signature to accept `effectivePhase: EffectivePhase` directly (instead of computing it internally), since all callers already have the effective phase computed. This decouples the action validation from execution state derivation.

### Critical: Optimistic Execution State

The optimistic flow (Story 8.5) is local React state in `DRPlanDetailPage`:
1. User clicks action → `useCreateDRExecution().create()` → stores `optimisticExec = { name, action }` in component state
2. `effectiveOptimisticExec` = `realActiveExec ? null : optimisticExec` — once a real execution is detected, optimistic is cleared
3. 30s safety timeout auto-clears stale optimistic state

**After migration:** The "real execution detected" check changes from `plan?.status?.activeExecution` (which was written by the reconciler within ~1–5 seconds) to `activeExec?.metadata?.name` (from the `useDRExecutions` watch). The watch should detect the newly created DRExecution even faster than the plan status update path because:
- The execution is created immediately via `k8sCreate`
- `useDRExecutions(planName)` watches DRExecutions with `soteria.io/plan-name` label selector
- The label is stamped server-side in `PrepareForCreate` (Story 10.1)

### Critical: `useDRExecution` (Singular) — Dead Code

`useDRExecution(name)` is defined in `useDRResources.ts` (lines 66–89) but is **not imported anywhere** in the codebase. `ExecutionDetailPage` uses `useDRExecutions()` with `.find()` instead (due to OCP Console SDK platform constraints — single-resource watches fail against aggregated APIs; see project-context.md). This singular hook is already dead code — do NOT introduce new usages of it.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `useDRExecutions(planName?)` label-filtered list watch | `console-plugin/src/hooks/useDRResources.ts:57-64` | Already used in detail page and dashboard |
| `buildLatestExecutionMap` client-side index | `console-plugin/src/utils/drPlanUtils.ts:70-92` | Follow for `buildActiveExecMap` |
| `findActiveExecution` non-terminal check | Go `Status.Result == ""` pattern (Stories 10.1-10.3) | Mirror as `!exec.status?.result` |
| `effectiveOptimisticExec` clearance pattern | `DRPlanDetailPage.tsx:55` | Keep pattern, change data source |
| `ACTION_CONFIG` action-to-mode mapping | `useCreateDRExecution.ts:7-25` | Already used, no changes needed |
| Props-down pattern for computed values | PatternFly convention, `PhaseBadge` | Follow for `effectivePhase` prop drilling |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `console-plugin/src/models/types.ts` | Remove 2 fields from `DRPlanStatus` | ~2 lines removed |
| `console-plugin/src/utils/drPlanUtils.ts` | Rewrite `getEffectivePhase` signature, add `findActiveExecution` + `buildActiveExecMap` | ~25 lines changed/added |
| `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` | Derive from matched DRExecution, pass `effectivePhase` down | ~15 lines changed |
| `console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx` | Use execution prop for effective phase and link | ~10 lines changed |
| `console-plugin/src/components/DRPlanDetail/PlanHeader.tsx` | Accept `effectivePhase` prop | ~5 lines changed |
| `console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx` | Accept `effectivePhase` prop | ~5 lines changed |
| `console-plugin/src/components/DRDashboard/DRDashboard.tsx` | Build active exec map, pass to `getEffectivePhase` | ~5 lines changed |
| `console-plugin/src/utils/drPlanActions.ts` | Accept `effectivePhase` param in `getValidActions` | ~5 lines changed |
| `tests/utils/drPlanUtils.test.ts` | Rewrite `getEffectivePhase` tests with DRExecution fixtures | ~40 lines changed |
| `tests/components/DRPlanDetailPage.test.tsx` | Update optimistic tests, remove plan status mocks | ~30 lines changed |
| `tests/components/TransitionProgressBanner.test.tsx` | Remove plan status mocks, use execution fixtures | ~30 lines changed |
| `tests/components/DRLifecycleDiagram.test.tsx` | Update plan mocks, pass `effectivePhase` prop | ~20 lines changed |
| `tests/components/DRPlanActions.test.tsx` | Update `makePlan` helper, pass effective phase | ~15 lines changed |
| `tests/components/Accessibility.test.tsx` | Remove plan status fields | ~2 lines changed |
| `tests/components/KeyboardAccessibility.test.tsx` | Remove plan status fields | ~2 lines changed |

### Execution Order

1. Task 1 (remove fields from TypeScript interface) — the root cause of all type errors
2. Task 2 (rewrite `getEffectivePhase`) — core derivation change
3. Task 3 (add `findActiveExecution` + `buildActiveExecMap` helpers) — shared utilities
4. Task 7 (update downstream callers: `PlanHeader`, `DRLifecycleDiagram`, `drPlanActions`) — fix compile errors from signature change
5. Task 4 (update `DRPlanDetailPage`) — primary consumer
6. Task 5 (update `TransitionProgressBanner`) — depends on Task 4 prop changes
7. Task 6 (update `DRDashboard` with execution index) — independent of detail page
8. Tasks 8–13 (update all tests) — after production code compiles
9. Task 14 (run full test suite) — final gate

### Previous Story Learnings (from 10.1–10.4)

- **Story 10.1 stamps `soteria.io/plan-name` label server-side** in `PrepareForCreate` — the `useDRExecutions(planName)` hook's `matchLabels` selector relies on this label being present on all DRExecutions (both new and backfilled via `ensurePlanNameLabel`)
- **Story 10.2 established the `buildActiveExecIndex` pattern** in Go (bulk LIST + client-side map, O(plans+executions)) — mirror this as `buildActiveExecMap` in TypeScript for the dashboard
- **Story 10.2/10.3 defined "non-terminal" as `Status.Result == ""`** — mirror in TypeScript as `!exec.status?.result`
- **Story 10.4 removed the Go struct fields** — the API response no longer includes `activeExecution`/`activeExecutionMode` on DRPlan status. TypeScript reads will return `undefined`
- **Story 8.5 established the optimistic execution pattern** — local component state, 30s timeout, cleared when real execution appears. This pattern is execution-object-driven, not plan-status-driven — the migration changes only the "real execution detected" check
- **OCP Console SDK constraint:** `useDRExecution(name)` (singular watch) does not work against aggregated APIs. Always use `useDRExecutions()` (list watch) + `.find()`. See project-context.md
- **React Router v5 on OCP 4.20:** Use `useHistory`, `useParams` from `react-router-dom`, NOT `useNavigate` (v7+). See project-context.md
- **PatternFly 6 CSS tokens:** Use `--pf-t--global--*` with `--pf-v5-global--*` fallback. No hardcoded colors. See project-context.md

### Git Intelligence (Recent Patterns)

Recent commits follow a single-commit-per-story pattern with the story number as prefix (e.g., "Story 9.7: cross-site volume group disk mapping"). Console plugin stories typically modify `~6-10 src files` and `~4-6 test files`. jest-axe accessibility is mandatory on all test suites.

### Project Structure Notes

- TypeScript types: `console-plugin/src/models/types.ts` (DRPlanStatus at lines ~88-100, PreflightReport at lines ~136-149)
- Plan utilities: `console-plugin/src/utils/drPlanUtils.ts` (getEffectivePhase at lines ~13-36, buildLatestExecutionMap at lines ~70-92)
- Plan actions: `console-plugin/src/utils/drPlanActions.ts` (getValidActions at lines ~48-50)
- Plan detail page: `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` (execution matching at lines ~37-55)
- Transition banner: `console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx` (effective phase at lines ~27-49)
- Plan header: `console-plugin/src/components/DRPlanDetail/PlanHeader.tsx` (getEffectivePhase at line ~21)
- Lifecycle diagram: `console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx` (getEffectivePhase at lines ~225-228)
- Dashboard: `console-plugin/src/components/DRDashboard/DRDashboard.tsx` (enrichPlans at lines ~53-62)
- DR hooks: `console-plugin/src/hooks/useDRResources.ts` (useDRExecutions at lines ~57-64)
- Create hook: `console-plugin/src/hooks/useCreateDRExecution.ts` (soteria.io/plan-name label at line ~38)
- Tests: `console-plugin/tests/` — component tests in `tests/components/`, utility tests in `tests/utils/`
- Auto-generated files (DO NOT EDIT): None in console-plugin

### References

- [Source: console-plugin/src/models/types.ts#L88-L100] — DRPlanStatus interface with activeExecution fields (to be removed)
- [Source: console-plugin/src/models/types.ts#L136-L149] — PreflightReport interface with activeExecution (to be KEPT)
- [Source: console-plugin/src/utils/drPlanUtils.ts#L13-L36] — getEffectivePhase reading plan.status.activeExecution (to be rewritten)
- [Source: console-plugin/src/utils/drPlanUtils.ts#L70-L92] — buildLatestExecutionMap (template for buildActiveExecMap)
- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx#L37-L55] — Execution matching and optimistic state
- [Source: console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx#L27-L49] — Transition detection using getEffectivePhase
- [Source: console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx#L102] — View execution details link using plan.status.activeExecution
- [Source: console-plugin/src/components/DRDashboard/DRDashboard.tsx#L53-L62] — enrichPlans with getEffectivePhase per plan
- [Source: console-plugin/src/components/DRDashboard/DRDashboard.tsx#L139-L143] — useDRExecutions() full cluster list
- [Source: console-plugin/src/hooks/useDRResources.ts#L57-L64] — useDRExecutions with optional planName label selector
- [Source: console-plugin/src/hooks/useCreateDRExecution.ts#L38] — soteria.io/plan-name label client-side stamping
- [Source: console-plugin/src/utils/drPlanActions.ts#L48-L50] — getValidActions calling getEffectivePhase
- [Source: console-plugin/src/components/DRPlanDetail/PlanHeader.tsx#L21] — getEffectivePhase in PlanHeader
- [Source: console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx#L225-L228] — getEffectivePhase in DRLifecycleDiagram
- [Source: console-plugin/tests/utils/drPlanUtils.test.ts] — getEffectivePhase tests with activeExecution plan mocks
- [Source: console-plugin/tests/components/DRPlanDetailPage.test.tsx] — Optimistic execution tests
- [Source: console-plugin/tests/components/TransitionProgressBanner.test.tsx] — Banner tests with activeExecution mocks
- [Source: console-plugin/tests/components/DRLifecycleDiagram.test.tsx] — Diagram tests with activeExecution mocks
- [Source: console-plugin/tests/components/DRPlanActions.test.tsx] — Action tests with activeExecution mocks
- [Source: _bmad-output/implementation-artifacts/10-4-remove-activeexecution-fields-from-drplanstatus.md] — Story 10.4: Go struct field removal
- [Source: _bmad-output/implementation-artifacts/10-2-derived-active-execution-for-table-convertor-preflight.md] — Story 10.2: buildActiveExecIndex pattern
- [Source: _bmad-output/implementation-artifacts/10-1-drexecution-concurrency-guard-without-activeexecution.md] — Story 10.1: PlanNameLabel, PrepareForCreate stamping
- [Source: _bmad-output/project-context.md] — Critical rules, Console SDK constraints, PatternFly 6

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
