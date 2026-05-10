# Story 10.4: Remove ActiveExecution Fields from DRPlanStatus

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want the `ActiveExecution` and `ActiveExecutionMode` fields removed from the `DRPlanStatus` type,
So that the DRPlan API clearly communicates that plans are static configuration and discovery artifacts.

## Background

Stories 10.1–10.3 systematically migrated every Go production code consumer of `plan.Status.ActiveExecution` and `plan.Status.ActiveExecutionMode`:

- **10.1** — Admission concurrency guard now queries DRExecution storage directly; reconciler no longer writes `ActiveExecution`/`ActiveExecutionMode` on the plan (reconcileSetup, finishExecution, failExecution, reprotect completion all migrated); `fetchPlanWithActiveExecCheck` replaced by `verifyExclusiveExecution`; critical field detector no longer compares `ActiveExecution`.
- **10.2** — Table convertor derives effective phase and active execution from `DRPlanTableConvertor.drexecutionLister`; preflight composition reads `CompositionInput.ActiveExecution` instead of plan status.
- **10.3** — Health polling gate uses `hasActiveExecution` helper (DRExecution label query); `mapVMToDRExecution` queries DRExecutions directly instead of reading `plan.Status.ActiveExecution`.

**After 10.3, zero Go production code reads or writes `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode`.** The fields are dead on the struct. This story removes them, regenerates codegen, and sweeps all remaining references (test fixtures, legacy webhook, documentation).

**Backward compatibility:** Existing DRPlan resources stored in ScyllaDB that contain `activeExecution`/`activeExecutionMode` JSON keys will have them silently ignored on deserialization — standard Kubernetes behavior for removed status fields. No data migration is required.

## Acceptance Criteria

1. **AC1 — Fields removed from DRPlanStatus:** `ActiveExecution string` and `ActiveExecutionMode ExecutionMode` are removed from the `DRPlanStatus` struct in `pkg/apis/soteria.io/v1alpha1/types.go`. The `Phase` field doc comment is updated to reflect the new derivation pattern (DRExecution query, not ActiveExecution mode).

2. **AC2 — Codegen regenerated:** `make manifests generate` regenerates DeepCopy, OpenAPI, and CRD/RBAC manifests. The updated OpenAPI spec no longer includes `activeExecution` or `activeExecutionMode` on `DRPlanStatus`.

3. **AC3 — PrepareForCreate cleaned up:** `PrepareForCreate` in `pkg/registry/drplan/strategy.go` no longer sets `ActiveExecution` or `ActiveExecutionMode` (the fields don't exist).

4. **AC4 — Legacy webhook validator compiles:** The `ActiveExecution` concurrency check in `pkg/admission/drexecution_validator.go` is removed (the webhook is unused for soteria.io resources — VWC entries were removed in Story 9.2; the real concurrency guard lives in the admission plugin).

5. **AC5 — All Go code compiles with zero references:** Zero references to `DRPlanStatus.ActiveExecution` or `DRPlanStatus.ActiveExecutionMode` remain in Go source files (production or test). Auto-generated files (`zz_generated.deepcopy.go`, `zz_generated.openapi.go`) are freshly regenerated.

6. **AC6 — Documentation updated:** `project-context.md` is updated to reflect the derived active execution pattern. The `DRPlanStatus.Phase` field comment in `types.go` is updated.

7. **AC7 — All tests pass:** All unit and integration tests pass with zero regressions. `make lint` passes.

## Tasks / Subtasks

- [ ] Task 1: Remove ActiveExecution and ActiveExecutionMode from DRPlanStatus (AC: #1)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, remove the `ActiveExecution string` field and its doc comment (lines 103–107)
  - [ ] 1.2 Remove the `ActiveExecutionMode ExecutionMode` field and its doc comment (lines 108–112)
  - [ ] 1.3 Update the `Phase` field doc comment (line 99): change `engine.EffectivePhase(Phase, ActiveExecution mode)` to `engine.EffectivePhase(Phase, executionMode)` where `executionMode` is derived from the active DRExecution resource

- [ ] Task 2: Remove PrepareForCreate zeroing (AC: #3)
  - [ ] 2.1 In `pkg/registry/drplan/strategy.go`, remove `plan.Status.ActiveExecution = ""` and `plan.Status.ActiveExecutionMode = ""` from `PrepareForCreate` (lines 52–53)

- [ ] Task 3: Remove legacy webhook ActiveExecution check (AC: #4)
  - [ ] 3.1 In `pkg/admission/drexecution_validator.go`, remove the concurrency gate block (lines 93–98) that reads `plan.Status.ActiveExecution`. Add a comment: "Concurrency gate is enforced by SoteriaAdmissionPlugin via DRExecution storage query (Story 10.1)"
  - [ ] 3.2 Update the function's doc comment if it mentions ActiveExecution

- [ ] Task 4: Regenerate codegen (AC: #2)
  - [ ] 4.1 Run `make manifests generate`
  - [ ] 4.2 Verify `zz_generated.deepcopy.go` no longer contains `ActiveExecution` or `ActiveExecutionMode` field-level handling (the struct-level `*out = *in` copy still copies remaining fields)
  - [ ] 4.3 Verify `zz_generated.openapi.go` no longer defines `activeExecution` or `activeExecutionMode` properties on `DRPlanStatus`

- [ ] Task 5: Sweep and fix test compilation errors (AC: #5)
  - [ ] 5.1 Run `rg 'ActiveExecution|ActiveExecutionMode' --type go` across the entire repo and identify every remaining reference. Categorize each as: (a) auto-generated (will be fixed by codegen), (b) test fixture assignment on DRPlan status, (c) test assertion on DRPlan status, (d) `PreflightReport.ActiveExecution` (KEEP — different struct)
  - [ ] 5.2 In `pkg/registry/drplan/strategy_test.go`, remove `TestPrepareForCreate_InitializesActiveExecution` entirely (the fields no longer exist; nothing to test)
  - [ ] 5.3 In `pkg/admission/drexecution_validator_test.go`, remove or update any test fixtures that set `plan.Status.ActiveExecution` on DRPlan objects. If the test exercised the concurrency gate that was removed in Task 3, remove the test case entirely. If the test exercises other validation (phase transition, SitesInSync), keep it but remove the `ActiveExecution` field assignment
  - [ ] 5.4 In `pkg/admission/plugin_test.go`, sweep for any remaining `plan.Status.ActiveExecution` assignments in test fixtures and remove them. After 10.1, these should already be migrated — verify no stale fixtures remain
  - [ ] 5.5 In `pkg/controller/drexecution/reconciler_test.go`, sweep for any `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode` assignments in DRPlan fixtures. Remove them — after 10.1, tests should use DRExecution fixtures for concurrency/ownership semantics
  - [ ] 5.6 In `pkg/engine/executor_test.go`, sweep for `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode` assignments. Remove them — after 10.1, finishExecution no longer touches these fields
  - [ ] 5.7 In `pkg/engine/reprotect_test.go`, sweep for `plan.Status.ActiveExecution` assignments. Remove them
  - [ ] 5.8 In `pkg/controller/drplan/health_test.go`, sweep for `plan.Status.ActiveExecution` assignments. Remove them — after 10.3, health tests use DRExecution fixtures
  - [ ] 5.9 In `pkg/apiserver/critical_fields_test.go`, sweep for test cases that set `ActiveExecution` on old/new DRPlan status. Remove the field from test fixtures — after 10.1 the detector ignores ActiveExecution changes
  - [ ] 5.10 In `test/integration/apiserver/admission_test.go`, sweep for `status["activeExecution"]` or `ActiveExecution` references. After 10.1, the integration test creates real DRExecutions for the concurrency gate — verify no stale plan status references remain
  - [ ] 5.11 In `internal/preflight/checks_test.go`, sweep for `plan.Status.ActiveExecution` assignments. After 10.2, these read from `CompositionInput.ActiveExecution` — verify no stale plan status references

- [ ] Task 6: Update documentation (AC: #6)
  - [ ] 6.1 In `_bmad-output/project-context.md`, update the DRPlan 8-phase lifecycle bullet (line 102): remove the sentence "`DRPlan.Status.ActiveExecution` references the in-progress DRExecution by name (empty when idle)" and replace with "Active execution state is derived at runtime by querying DRExecution resources filtered by `soteria.io/plan-name` label"
  - [ ] 6.2 Verify no other project-context.md references to `ActiveExecution` on DRPlan status

- [ ] Task 7: Final verification sweep (AC: #5, #7)
  - [ ] 7.1 Run `rg 'ActiveExecution|ActiveExecutionMode' --type go` — verify every remaining reference is either: (a) `PreflightReport.ActiveExecution` (keep), (b) `CompositionInput.ActiveExecution` (keep, Story 10.2 addition), (c) `EffectivePhase` function parameters (keep, parameterized), (d) auto-generated files just regenerated, (e) `PlanNameLabel` or DRExecution-side code (keep)
  - [ ] 7.2 Run `make test` — all unit and integration tests pass with zero regressions
  - [ ] 7.3 Run `make lint` — zero lint errors

## Dev Notes

### Scope & Architecture

This is a deletion/cleanup story — the simplest in Epic 10. Stories 10.1–10.3 already migrated every production code consumer. This story removes the dead fields, fixes the compilation cascade, and updates documentation.

**What is removed:**
- `DRPlanStatus.ActiveExecution string` field and its 4-line doc comment
- `DRPlanStatus.ActiveExecutionMode ExecutionMode` field and its 4-line doc comment plus kubebuilder validation marker
- `PrepareForCreate` explicit zeroing of these fields
- Legacy webhook validator's `plan.Status.ActiveExecution` concurrency check (dead code since Story 9.2)
- Test fixtures that set these fields on DRPlan status objects
- `TestPrepareForCreate_InitializesActiveExecution` test

**What is NOT removed (different structs/contexts):**
- `PreflightReport.ActiveExecution` — a report snapshot field, not a plan status pointer. Populated from `CompositionInput.ActiveExecution` (Story 10.2)
- `CompositionInput.ActiveExecution` — the derived-from-DRExecution-query input (Story 10.2)
- `engine.EffectivePhase(restPhase, activeExecMode)` — parameterized function, does not read plan status. Callers pass mode from DRExecution resources (Stories 10.2/10.3)
- Console TypeScript `activeExecution`/`activeExecutionMode` on `DRPlanStatus` interface — migrated in Story 10.5

### Critical: Types.go Field Removal

**Current `DRPlanStatus` (`pkg/apis/soteria.io/v1alpha1/types.go:96-141`):**

```go
type DRPlanStatus struct {
	// Phase represents the current DR lifecycle rest state. Only rest-state
	// values are persisted; transient phases are derived at runtime via
	// engine.EffectivePhase(Phase, ActiveExecution mode).
	Phase string `json:"phase,omitempty"`
	// ActiveExecution is the name of the in-progress DRExecution...
	ActiveExecution string `json:"activeExecution,omitempty"`
	// ActiveExecutionMode is the mode of the active execution...
	ActiveExecutionMode ExecutionMode `json:"activeExecutionMode,omitempty"`
	// ActiveSite tracks which cluster currently owns the active workloads.
	ActiveSite string `json:"activeSite,omitempty"`
	// ...remaining fields
}
```

**After removal:**

```go
type DRPlanStatus struct {
	// Phase represents the current DR lifecycle rest state. Only rest-state
	// values are persisted; transient phases are derived at runtime via
	// engine.EffectivePhase(Phase, executionMode) where executionMode is
	// obtained from the active DRExecution resource (if any).
	Phase string `json:"phase,omitempty"`
	// ActiveSite tracks which cluster currently owns the active workloads.
	ActiveSite string `json:"activeSite,omitempty"`
	// ...remaining fields unchanged
}
```

### Critical: Legacy Webhook Validator

`pkg/admission/drexecution_validator.go` still reads `plan.Status.ActiveExecution` at line 94. This webhook is **dead code for soteria.io resources** — VWC entries for `drexecutions.soteria.io` were removed in Story 9.2 (aggregated API admission plugin migration). The real concurrency guard now lives in `SoteriaAdmissionPlugin` (Story 10.1).

**The code block to remove (lines 93–98):**

```go
// Concurrency gate: reject if another execution is already active.
if plan.Status.ActiveExecution != "" {
    return admission.Denied(fmt.Sprintf(
        "DRPlan %q has active execution %q; concurrent executions not permitted",
        exec.Spec.PlanName, plan.Status.ActiveExecution))
}
```

**Replace with a comment:**

```go
// Concurrency gate is enforced by SoteriaAdmissionPlugin via DRExecution
// storage query (Story 10.1). This webhook no longer enforces concurrency
// for soteria.io resources (VWC entries removed in Story 9.2).
```

The remaining validation in the function (phase transition check via `engine.Transition`, SitesInSync/DisksConsistent gates) still references `plan.Status.Phase` and conditions — those stay.

### Critical: Test Fixture Sweep Strategy

After 10.1–10.3, most test **assertions** on `ActiveExecution`/`ActiveExecutionMode` have been removed. However, test **fixtures** that create DRPlan objects may still set these fields as part of struct literals. When the fields are removed from the struct, these assignments will fail to compile.

**Sweep approach:**

```bash
rg '\.ActiveExecution|\.ActiveExecutionMode' --type go -g '*_test.go'
```

For each hit, determine if it's:
1. **Assignment in DRPlan status fixture** → Remove the line
2. **Assignment on PreflightReport** → KEEP (different struct)
3. **Assignment on CompositionInput** → KEEP (Story 10.2)
4. **Assertion/comparison** → Remove (should be gone after 10.1–10.3, but sweep as safety)

**Expected compilation errors** (assuming 10.1–10.3 are complete):
- `strategy_test.go`: `TestPrepareForCreate_InitializesActiveExecution` — remove entire test
- `drexecution_validator_test.go`: test fixtures setting `plan.Status.ActiveExecution` for the now-removed concurrency gate — remove the field from fixtures or remove concurrency-gate-specific test cases entirely
- Possible stale fixtures in `reconciler_test.go`, `executor_test.go`, `reprotect_test.go`, `health_test.go` — any struct literal `DRPlanStatus{..., ActiveExecution: "exec-1", ...}` will fail

**Safe removal pattern:** For each test file, remove the `ActiveExecution:` and `ActiveExecutionMode:` field assignments from DRPlan status struct literals. If a test case exists solely to test the old ActiveExecution concurrency gate on the legacy webhook, remove the entire test case.

### Critical: PreflightReport.ActiveExecution Is NOT Removed

`PreflightReport.ActiveExecution` (`types.go:153–155`) is a **report snapshot field** on a separate struct. It was migrated in Story 10.2 to be populated from `CompositionInput.ActiveExecution` (derived from DRExecution query). It stays in the API — the preflight report still communicates "an execution is active for this plan" to consumers.

When sweeping with `rg 'ActiveExecution'`, you WILL see hits for `PreflightReport.ActiveExecution` and `CompositionInput.ActiveExecution`. These are correct references that must NOT be removed.

### Critical: EffectivePhase Is NOT Changed

`engine.EffectivePhase(restPhase string, activeExecMode ExecutionMode) string` in `pkg/engine/statemachine.go` is a **parameterized function** that takes the execution mode as an argument. It does not read `plan.Status`. After 10.1–10.3, all callers pass the mode from DRExecution resources:
- Table convertor (10.2): `engine.EffectivePhase(plan.Status.Phase, exec.Spec.Mode)`
- Console (10.5): will derive similarly

The function signature and implementation stay unchanged.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| Field removal from API type | Standard Kubernetes pattern | Remove field, run `make manifests generate` |
| Unknown JSON field handling | Kubernetes JSON deserializer | Silently ignores unknown fields in stored objects |
| Dead webhook code removal | Story 9.2 (removed VWC entries) | Legacy validator code is now dead for soteria.io resources |
| `rg` sweep for stale references | Stories 10.1–10.3 (Task 6 in 10.3) | Use same grep sweep pattern |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Remove 2 fields + comments, update Phase comment | ~12 lines removed, ~2 lines changed |
| `pkg/registry/drplan/strategy.go` | Remove 2 lines from PrepareForCreate | ~2 lines removed |
| `pkg/admission/drexecution_validator.go` | Remove concurrency gate block, add comment | ~6 lines removed, ~3 lines added |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-regenerated | N/A |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` | Auto-regenerated | N/A |
| `config/crd/bases/*.yaml` | Auto-regenerated | N/A |
| `pkg/registry/drplan/strategy_test.go` | Remove `TestPrepareForCreate_InitializesActiveExecution` | ~18 lines removed |
| `pkg/admission/drexecution_validator_test.go` | Remove/update fixtures and concurrency gate tests | ~20–40 lines changed |
| `pkg/admission/plugin_test.go` | Sweep for stale fixtures | ~5–10 lines removed |
| `pkg/controller/drexecution/reconciler_test.go` | Sweep for stale fixtures | ~10–30 lines removed |
| `pkg/engine/executor_test.go` | Sweep for stale fixtures | ~5–10 lines removed |
| `pkg/engine/reprotect_test.go` | Sweep for stale fixtures | ~2–5 lines removed |
| `pkg/controller/drplan/health_test.go` | Sweep for stale fixtures | ~2–5 lines removed |
| `pkg/apiserver/critical_fields_test.go` | Sweep for stale fixtures | ~2–5 lines removed |
| `test/integration/apiserver/admission_test.go` | Sweep for stale fixtures | ~2–5 lines removed |
| `internal/preflight/checks_test.go` | Sweep for stale fixtures | ~2–5 lines removed |
| `_bmad-output/project-context.md` | Update lifecycle description | ~2 lines changed |

### Execution Order

1. Task 1 (remove fields from types.go) — the root cause of all compilation errors
2. Task 2 (remove PrepareForCreate zeroing) — immediate compilation fix
3. Task 3 (legacy webhook fix) — immediate compilation fix
4. Task 4 (regenerate codegen) — must run before test compilation
5. Task 5 (sweep all test files) — fix remaining compilation errors
6. Task 6 (documentation) — no compilation impact
7. Task 7 (final verification) — gate

### Previous Story Learnings (from 10.1, 10.2, 10.3)

- **Story 10.1 established the PlanNameLabel constant** in types.go and moved the concurrency guard to the admission plugin. `PrepareForCreate` now stamps `soteria.io/plan-name` server-side. The `ensurePlanNameLabel` fallback in `reconcileSetup` handles pre-10.1 executions.
- **Story 10.1 documented the legacy webhook as unused** for soteria.io resources (VWC entries removed in 9.2). Story 10.4 completes the cleanup by removing the stale `ActiveExecution` check.
- **Story 10.2 changed `NewREST` signature** to return `*DRPlanTableConvertor` as third value. The table convertor derives effective phase from `drexecutionLister` via `buildActiveExecIndex`.
- **Story 10.2 added `ActiveExecution` to `CompositionInput`** — this field replaces `plan.Status.ActiveExecution` reads in preflight composition. Do NOT remove it during the sweep.
- **Story 10.3 added `hasActiveExecution` helper** to the DRPlan reconciler for the health polling gate. Confirmed zero production code reads `plan.Status.ActiveExecution` after 10.3.
- **`make manifests generate` IS needed** for this story — types.go struct changes require OpenAPI and DeepCopy regeneration.
- **Controller-runtime fake client** supports struct-level equality even with removed fields — test fixtures just need the field references removed from struct literals.

### Git Intelligence (Recent Patterns)

Recent commits follow a single-commit-per-story pattern with the story number as prefix (e.g., "Story 9.7: cross-site volume group disk mapping"). Epic 10 stories 10.1–10.3 are in ready-for-dev status and must be implemented before 10.4. The removal story is intentionally sequenced last in the Go changes (10.5 is Console-only) to ensure all consumers are migrated before the struct fields are deleted.

### Project Structure Notes

- Types: `pkg/apis/soteria.io/v1alpha1/types.go` — DRPlanStatus at lines 96–141, PreflightReport at lines 143–175
- Registry strategy: `pkg/registry/drplan/strategy.go` — PrepareForCreate at lines 47–55
- Legacy webhook: `pkg/admission/drexecution_validator.go` — concurrency gate at lines 93–98
- State machine: `pkg/engine/statemachine.go` — EffectivePhase at lines 118–135 (NOT changed)
- Project context: `_bmad-output/project-context.md` — lifecycle bullet at line 102
- Auto-generated (DO NOT hand-edit): `zz_generated.deepcopy.go`, `zz_generated.openapi.go`, `config/crd/bases/*.yaml`, `config/rbac/role.yaml`

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L96-L141] — DRPlanStatus struct with ActiveExecution fields (to be removed)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L143-L175] — PreflightReport struct with ActiveExecution (to be KEPT)
- [Source: pkg/registry/drplan/strategy.go#L47-L55] — PrepareForCreate with ActiveExecution zeroing
- [Source: pkg/registry/drplan/strategy_test.go#L108-L125] — TestPrepareForCreate_InitializesActiveExecution (to be removed)
- [Source: pkg/admission/drexecution_validator.go#L93-L98] — Legacy webhook ActiveExecution concurrency gate (to be removed)
- [Source: pkg/engine/statemachine.go#L118-L135] — EffectivePhase parameterized function (NOT changed)
- [Source: _bmad-output/project-context.md#L102] — DRPlan lifecycle description referencing ActiveExecution
- [Source: _bmad-output/implementation-artifacts/10-1-drexecution-concurrency-guard-without-activeexecution.md] — Story 10.1: concurrency guard migration, PlanNameLabel, reconciler write removal
- [Source: _bmad-output/implementation-artifacts/10-2-derived-active-execution-for-table-convertor-preflight.md] — Story 10.2: table convertor derived pattern, CompositionInput.ActiveExecution
- [Source: _bmad-output/implementation-artifacts/10-3-derived-active-execution-for-reconciler-gates-vm-watch.md] — Story 10.3: hasActiveExecution helper, mapVMToDRExecution rewrite
- [Source: _bmad-output/planning-artifacts/epics.md#Story-10.4] — Epic acceptance criteria
- [Source: _bmad-output/project-context.md] — Critical rules, ScyllaRetry, tiered comments

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
