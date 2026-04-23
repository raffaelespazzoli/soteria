# Story 5.0: Rest-State-Only DRPlan & Active Execution Pointer

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want the DRPlan to store only rest-state phases and reference the active DRExecution by name,
so that a failed execution never leaves the plan stuck in a transient phase and I can always determine the effective DR state from the API alone.

## Background / ADR

Manual E2E testing on etl6/etl7 (Epic 4 retrospective) revealed that `failExecution()` in the DRExecution reconciler marks the execution as Failed but does **not** roll back `DRPlan.Status.Phase`. If the plan was already transitioned to a transient phase (e.g., `FailingBack`) before the failure, the plan remains stuck there with no automatic recovery. Operators must manually patch the status subresource via `kubectl replace --raw`.

**Root cause:** Transient phases should not be persisted on the DRPlan. They are a derived state — computable from the rest state plus the active execution mode.

**Design Decision (Option C from retrospective):**
- `DRPlan.Status.Phase` holds **only** rest states: `SteadyState`, `FailedOver`, `DRedSteadyState`, `FailedBack`
- New `DRPlan.Status.ActiveExecution` field: name reference to the in-progress DRExecution (empty string when idle)
- `EffectivePhase(restPhase, activeExecMode)` helper derives the transient phase from rest state + execution mode
- Reconciler sets `ActiveExecution` on execution start, clears it on completion or failure
- Phase advances to the **next** rest state on `Succeeded` or `PartiallySucceeded`; no advance on total `Failed` (plan stays at current rest state)
- Admission webhook rejects new DRExecution if `ActiveExecution != ""`
- Printer column shows the computed effective phase for `kubectl get drplans`
- `v1alpha1` is pre-GA — no backward compatibility; existing DRPlans must be deleted and recreated

## Acceptance Criteria

1. **AC1 — Rest-state-only Phase:** `DRPlan.Status.Phase` only holds rest-state values (`SteadyState`, `FailedOver`, `DRedSteadyState`, `FailedBack`). The DRExecution reconciler and WaveExecutor no longer write transient phases (`FailingOver`, `Reprotecting`, `FailingBack`, `ReprotectingBack`) to `DRPlan.Status.Phase`. On execution start, `Phase` is unchanged; on successful completion, `Phase` advances to the next rest state via `CompleteTransition`. On total failure, `Phase` stays at the current rest state.

2. **AC2 — ActiveExecution field:** `DRPlanStatus` has a new string field `ActiveExecution` (`json:"activeExecution,omitempty"`). The DRExecution reconciler sets it to `exec.Name` when starting a new execution (gated on `StartTime == nil`), and clears it (sets to `""`) on completion or failure. The registry strategy initializes it to `""` on `PrepareForCreate`.

3. **AC3 — EffectivePhase helper:** A new `EffectivePhase(restPhase string, activeExecMode ExecutionMode) string` function in `pkg/engine/statemachine.go` derives the transient phase from a rest state and execution mode. When `activeExecMode` is empty, it returns `restPhase` (idle). When mode is non-empty, it uses the existing `validTransitions` map to return the corresponding transient phase (e.g., `SteadyState` + `planned_migration` → `FailingOver`). Returns `restPhase` if the combination is unknown.

4. **AC4 — Admission concurrency gate:** `pkg/admission/drexecution_validator.go` rejects `CREATE` when `plan.Status.ActiveExecution != ""` with a message identifying the conflicting execution name. The existing `engine.Transition(plan.Status.Phase, mode)` validation continues to enforce valid rest-state-to-mode mappings.

5. **AC5 — Critical fields detection:** `pkg/apiserver/critical_fields.go` detects `ActiveExecution` changes in addition to `Phase` and `ActiveSite` changes, triggering SERIAL Paxos for cross-DC safety.

6. **AC6 — Preflight report:** `PreflightReport` includes a new `ActiveExecution string` field. `ComposeReport` populates it from `plan.Status.ActiveExecution`. When non-empty, preflight adds a warning: `"execution <name> is active; new execution blocked"`.

7. **AC7 — OpenAPI additionalPrinterColumns:** The DRPlan OpenAPI schema in `pkg/registry/drplan/strategy.go` (or `pkg/apiserver/` table conversion) provides an `EFFECTIVE PHASE` printer column that computes the transient phase for display. `kubectl get drplans` shows the effective phase (transient when active, rest when idle) alongside the raw `PHASE` column.

8. **AC8 — Checkpoint/resume compatibility:** The checkpoint writer (`pkg/engine/checkpoint.go`) and resume analyzer (`pkg/engine/resume.go`) work correctly with the new model. Resume identifies the active execution via `plan.Status.ActiveExecution` instead of inferring from transient phase. Checkpoint writes do not modify `DRPlan.Status.Phase`.

9. **AC9 — Test coverage:** Unit tests covering: (a) `EffectivePhase` returns correct transient for all 4 rest-state + mode combinations and returns rest when idle; (b) DRPlan.Status.Phase never holds a transient value after any reconciler or executor path; (c) `ActiveExecution` is set on start, cleared on completion, cleared on failure; (d) admission rejects create when `ActiveExecution != ""`; (e) critical fields detects `ActiveExecution` change; (f) failed execution does NOT advance phase (plan stays at current rest state); (g) all existing tests pass with updated fixtures — plans never hold transient phases in test setup.

## Tasks / Subtasks

- [ ] Task 1: Update API types (AC: #1, #2)
  - [ ] 1.1 Update `Phase` doc comment in `DRPlanStatus` to list only rest states as valid values
  - [ ] 1.2 Add `ActiveExecution string` field to `DRPlanStatus` with `json:"activeExecution,omitempty"` tag and doc comment explaining it's a name reference to the in-progress DRExecution
  - [ ] 1.3 Run `make generate` to regenerate `zz_generated.deepcopy.go` and `zz_generated.openapi.go`

- [ ] Task 2: Add EffectivePhase helper (AC: #3)
  - [ ] 2.1 Add `EffectivePhase(restPhase string, activeExecMode ExecutionMode) string` to `pkg/engine/statemachine.go` — when mode is empty return restPhase; otherwise look up `validTransitions[restPhase][mode]` and return the transient phase (or restPhase if lookup fails)
  - [ ] 2.2 Add unit tests in `statemachine_test.go`: all 4 rest states × relevant modes, idle mode returns rest, invalid combination returns rest

- [ ] Task 3: Refactor DRExecution reconciler (AC: #1, #2)
  - [ ] 3.1 In the setup phase (gated on `StartTime == nil`): **remove** the `plan.Status.Phase = targetPhase` patch. Instead, patch `plan.Status.ActiveExecution = exec.Name`
  - [ ] 3.2 Compute `targetPhase` via `engine.Transition()` still (for validation), but do not write it to the plan. Use it only for event messages and logging
  - [ ] 3.3 In `failExecution`: if `plan` is available and `plan.Status.ActiveExecution == exec.Name`, clear `ActiveExecution` (set to `""`) via status patch. Phase stays unchanged — this is the self-healing property
  - [ ] 3.4 In `reconcileReprotect` completion path: change `plan.Status.Phase = newPhase` to also set `plan.Status.ActiveExecution = ""` in the same patch. Keep `CompleteTransition` + `ActiveSiteForPhase` logic as-is
  - [ ] 3.5 Add/update unit tests: verify Phase unchanged on start, ActiveExecution set on start, ActiveExecution cleared on completion, ActiveExecution cleared on failure, Phase unchanged on failure

- [ ] Task 4: Refactor WaveExecutor (AC: #1, #2)
  - [ ] 4.1 In `finishExecution`: when result is `Succeeded` or `PartiallySucceeded`, call `CompleteTransition(plan.Status.Phase)` — but note `plan.Status.Phase` is already a rest state (no transient stored), so this call must be adapted. Use `EffectivePhase` to get the transient phase, then `CompleteTransition` on that to get the next rest state
  - [ ] 4.2 Alternative (simpler): add a `RestStateAfterCompletion(currentRestPhase string, mode ExecutionMode) (string, error)` helper to statemachine.go that chains `validTransitions[rest][mode]` → `completionTransitions[transient]` to go directly rest→rest. Use this in both `finishExecution` and `reconcileReprotect`
  - [ ] 4.3 In `finishExecution`: set `plan.Status.ActiveExecution = ""` alongside the phase advance
  - [ ] 4.4 On failure path in `finishExecution`: do NOT advance phase; set `plan.Status.ActiveExecution = ""`
  - [ ] 4.5 Update `executor_test.go`: verify plan.Status.Phase is always a rest state after execution

- [ ] Task 5: Update admission webhook (AC: #4)
  - [ ] 5.1 In `drexecution_validator.go` `CREATE` path: add check before Transition validation — if `plan.Status.ActiveExecution != ""`, deny with message `"DRPlan <name> has active execution <activeExec>; concurrent executions not permitted"`
  - [ ] 5.2 Add unit test in `drexecution_validator_test.go`: create rejected when ActiveExecution is set; create allowed when ActiveExecution is empty and phase is valid

- [ ] Task 6: Update critical fields detection (AC: #5)
  - [ ] 6.1 In `detectDRPlanCriticalFields`: add `|| oldPlan.Status.ActiveExecution != newPlan.Status.ActiveExecution`
  - [ ] 6.2 Add unit test in `critical_fields_test.go`: ActiveExecution change triggers critical field detection

- [ ] Task 7: Update preflight report (AC: #6)
  - [ ] 7.1 Add `ActiveExecution string` field to `PreflightReport` in `types.go`
  - [ ] 7.2 Populate in `ComposeReport` from `plan.Status.ActiveExecution`; add warning when non-empty
  - [ ] 7.3 Add unit test in `checks_test.go`: ActiveExecution appears in report and warning is emitted

- [ ] Task 8: Add printer column for effective phase (AC: #7)
  - [ ] 8.1 Determine the aggregated API server's table conversion mechanism (likely `pkg/registry/drplan/strategy.go` `TableConvertor` or `pkg/apiserver/` table handler). Add an `EFFECTIVE PHASE` column that computes `EffectivePhase(plan.Status.Phase, activeExecMode)` — where `activeExecMode` is resolved by looking up the active DRExecution's mode (or empty if `ActiveExecution == ""`)
  - [ ] 8.2 If table conversion requires fetching the DRExecution, consider caching or computing from known state machine mappings instead. Alternative: store `ActiveExecutionMode` alongside `ActiveExecution` in status to avoid an extra GET
  - [ ] 8.3 Verify `kubectl get drplans` displays effective phase correctly

- [ ] Task 9: Update checkpoint/resume (AC: #8)
  - [ ] 9.1 In `pkg/engine/resume.go` `ResumeAnalyzer`: identify active execution from `plan.Status.ActiveExecution` instead of inferring from transient phase
  - [ ] 9.2 In `pkg/engine/checkpoint.go`: verify checkpoint writes do not set `plan.Status.Phase` to transient values
  - [ ] 9.3 Add/update tests in `resume_test.go` and `checkpoint_test.go`

- [ ] Task 10: Update all test fixtures (AC: #9)
  - [ ] 10.1 Search all `_test.go` files for `plan.Status.Phase =` assignments using transient phase constants and replace with rest-state + `ActiveExecution` setup as appropriate
  - [ ] 10.2 Files likely needing updates (from grep): `executor_test.go`, `statemachine_test.go`, `critical_fields_test.go`, `drexecution_validator_test.go`, `reconciler_test.go` (drexecution), `reprotect_test.go`, `replication_test.go`, `apiserver_test.go`
  - [ ] 10.3 Update integration test helpers: `suite_test.go` `setPlanPhase` and `waitForPlanPhase` — these must only set/expect rest states. Add `setPlanActiveExecution` helper
  - [ ] 10.4 Update `hack/stretched-local-test.sh` if it checks for transient phases in `kubectl get` output
  - [ ] 10.5 Update `config/samples/` if any sample references transient phases

- [ ] Task 11: Update registry strategy (AC: #2)
  - [ ] 11.1 In `PrepareForCreate`: initialize `plan.Status.ActiveExecution = ""` (already empty by zero-value, but explicit for clarity)
  - [ ] 11.2 In `PrepareForUpdate`: ensure `ActiveExecution` is preserved from old object on spec-only updates (status subresource should handle this, verify)
  - [ ] 11.3 Add unit test in `strategy_test.go`: ActiveExecution initialized correctly on create

- [ ] Task 12: Run full test suite
  - [ ] 12.1 `make generate` — regenerate deepcopy + openapi
  - [ ] 12.2 `make manifests` — regenerate CRDs if markers changed
  - [ ] 12.3 `make lint-fix` — auto-fix style
  - [ ] 12.4 `make test` — all unit + integration tests pass

## Dev Notes

- **No backward compat**: `v1alpha1` is pre-GA. Existing DRPlans must be deleted and recreated. No migration script needed.
- **Key insight — self-healing on failure**: When execution fails, `failExecution` now clears `ActiveExecution` without touching `Phase`. The plan returns to its rest state naturally. This eliminates the "stuck transient phase" bug class entirely.
- **`CompleteTransition` refactoring**: Currently `CompleteTransition(transientPhase)` expects a transient phase. Since DRPlan.Status.Phase will only hold rest states, the callers must derive the transient phase first. The cleanest approach is a new `RestStateAfterCompletion(restPhase, mode)` helper that chains `Transition` + `CompleteTransition` internally — avoids exposing transient phases outside the state machine.
- **ActiveExecution as concurrency guard**: Replaces the implicit concurrency guard where only rest states allowed new `Transition()` calls. Now the guard is explicit: `ActiveExecution != ""` blocks new executions, even if the rest state would theoretically allow it.
- **Printer column complexity**: The aggregated API server uses `pkg/registry/drplan/strategy.go` for table conversion (not kubebuilder CRD printer columns). To avoid an extra DRExecution GET in the table handler, consider storing `ActiveExecutionMode ExecutionMode` alongside `ActiveExecution` in status. This makes `EffectivePhase` computable without a second API call.
- **10-AC cap**: This story has 9 ACs, within the new limit from the Epic 4 retrospective.
- **`failExecution` signature change**: Currently `failExecution(ctx, exec, reason, message)` has no `plan` parameter. Post-setup failures (e.g., `HandlerResolutionFailed`, `PreExecutionFailed`) occur AFTER `ActiveExecution` was set on the plan. The cleanest fix: add an optional `*DRPlan` parameter to `failExecution`. When non-nil and `plan.Status.ActiveExecution == exec.Name`, clear it via status patch. Pre-setup failures (PlanNotFound, InvalidMode, InvalidPhaseTransition) pass `nil` — ActiveExecution was never set.
- **Fixture churn is significant but mechanical**: ~8 test files reference transient phases in plan objects. Pattern: replace `plan.Status.Phase = PhaseFailingOver` with `plan.Status.Phase = PhaseSteadyState` + `plan.Status.ActiveExecution = "exec-name"`. Use search-and-replace systematically.
- **State machine maps stay the same**: `validTransitions` and `completionTransitions` remain valuable as lookup tables. They now serve `EffectivePhase` and `RestStateAfterCompletion` rather than direct plan-phase writes.

### Existing code patterns to follow

- **Phase writes**: Currently in 4 locations: `reconciler.go:164` (start), `reconciler.go:378` (reprotect complete), `executor.go:940` (wave complete), `strategy.go:47` (create). All must be updated.
- **Status patches**: Use `client.MergeFrom(plan.DeepCopy())` pattern established in all existing status writes.
- **Event emission**: Keep existing event reasons (`FailoverStarted`, `WaveCompleted`, etc.) — they use the target transient phase for messaging, but this is purely descriptive (no plan-phase dependency).
- **Structured logging**: `log.FromContext(ctx).WithValues("plan", plan.Name)` pattern per controller conventions.
- **Test helper**: `newTestDRPlan(name, primary, secondary)` from Story 4.9 in `test/integration/controller/suite_test.go:388`.

### Critical files to modify

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `ActiveExecution` to `DRPlanStatus`, add `ActiveExecution` to `PreflightReport`, optionally add `ActiveExecutionMode` |
| `pkg/engine/statemachine.go` | Add `EffectivePhase()`, add `RestStateAfterCompletion()` |
| `pkg/controller/drexecution/reconciler.go` | Stop writing transient phase; set/clear `ActiveExecution`; update `failExecution` to clear pointer |
| `pkg/engine/executor.go` | Update `finishExecution` to use rest-state-only model |
| `pkg/admission/drexecution_validator.go` | Add `ActiveExecution != ""` concurrency gate |
| `pkg/apiserver/critical_fields.go` | Add `ActiveExecution` change detection |
| `internal/preflight/checks.go` | Populate `ActiveExecution` in report + warning |
| `pkg/registry/drplan/strategy.go` | Initialize `ActiveExecution` on create; add table column |
| `pkg/engine/checkpoint.go` | Verify no transient phase writes |
| `pkg/engine/resume.go` | Use `ActiveExecution` instead of transient phase inference |

### Previous story intelligence (Story 4.9)

- **Fixture churn was ~23 test files** for site topology. This story's fixture scope is smaller (~8 test files with transient phases) but the pattern is the same: systematic search-and-replace.
- **Search pattern**: `rg 'PhaseFailingOver|PhaseReprotecting|PhaseFailingBack|PhaseReprotectingBack' --glob '*_test.go'` to find all test files needing updates.
- **`make generate` is essential** after types.go changes — deepcopy methods must be regenerated.
- **Zero lint issues expected** if following existing patterns.

### References

- [Source: _bmad-output/implementation-artifacts/epic-4-retro-2026-04-21.md#Epic 5 Preparation] — Option C design decision and rationale
- [Source: pkg/engine/statemachine.go] — Current 8-phase state machine, `Transition`, `CompleteTransition`, `IsTerminalPhase`, `ActiveSiteForPhase`
- [Source: pkg/controller/drexecution/reconciler.go:127-188] — Setup phase that writes transient to DRPlan (must change)
- [Source: pkg/controller/drexecution/reconciler.go:369-388] — Reprotect completion path
- [Source: pkg/controller/drexecution/reconciler.go:609-637] — `failExecution` does not roll back phase (the bug)
- [Source: pkg/engine/executor.go:880-951] — `finishExecution` with `CompleteTransition` + `ActiveSiteForPhase`
- [Source: pkg/apis/soteria.io/v1alpha1/types.go:81-101] — Current `DRPlanStatus` struct
- [Source: pkg/apiserver/critical_fields.go:43-57] — Critical field detection for SERIAL Paxos
- [Source: pkg/admission/drexecution_validator.go:91-101] — Current phase-based admission validation
- [Source: _bmad-output/project-context.md] — Project conventions, anti-patterns, testing rules

## Estimated Effort

- Production code: ~150 lines across ~10 files (types, statemachine, reconciler, executor, admission, critical_fields, preflight, strategy, checkpoint, resume)
- Test code: ~300 lines across ~10 test files (new tests + fixture updates)
- Total: ~450 net new/modified lines

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
