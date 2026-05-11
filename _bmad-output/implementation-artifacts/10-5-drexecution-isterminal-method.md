# Story 10.5: DRExecution IsTerminal Method

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a maintainer of the DR orchestrator codebase,
I want a single `IsTerminal()` method on `DRExecutionStatus` and all duplicated `Result == ""` / `Result != ""` checks refactored to use it,
So that "active" (in-progress) vs terminal execution semantics stay consistent across admission, controllers, registry, engine, and API server logic.

## Background

`DRExecutionStatus` carries an `ExecutionResult` in `Status.Result`. Today, seven or more production call sites infer "non-terminal / still active" by testing `Status.Result == ""`, and symmetrically infer "terminal" by `Status.Result != ""`. The allowed non-empty values are:

- `ExecutionResultSucceeded`
- `ExecutionResultPartiallySucceeded`
- `ExecutionResultFailed`

**Convention for this story:** an execution is **in progress** while `Result` is empty (`""`). Any non-empty `Result` means the reconcile/storage/admission layers should treat the object as having **finished reconciliation with a persisted outcome** for purposes of concurrency, audits, indexing, resume gates, etc. **`PartiallySucceeded` counts as terminal for `IsTerminal()`** — it is a non-empty result.

Duplicating raw string comparisons spreads subtle drift risk if new helpers or conditions are added inconsistently. Centralizing the rule in one method documents the contract and gives tests a single hook.

**Related work:** Epic 10 stories 10.1–10.4 migrated plan-side `ActiveExecution` fields; execution liveness is now derived from DRExecution resources. This story tightens the **status-level** expression of "is there a result yet?" without changing API shape or stored data.

**Not in scope:** comparing two different `Result` values (e.g. critical field detection) is unrelated to terminality and stays as direct field comparison.

## Acceptance Criteria

1. **AC1 — Method on `DRExecutionStatus`:** In `pkg/apis/soteria.io/v1alpha1/types.go`, add `func (s DRExecutionStatus) IsTerminal() bool` returning `s.Result != ""`. Document in a short comment that empty `Result` means in-progress; any non-empty value (including `PartiallySucceeded`) is terminal for this helper.

2. **AC2 — No API or serialization change:** No new CRD/OpenAPI fields; no JSON tag changes; behavior of storage and controllers remains equivalent to pre-refactor for all execution states.

3. **AC3 — Production call sites refactored:** Replace inline `Status.Result == ""` / `Status.Result != ""` checks at the locations below with `!status.IsTerminal()` / `status.IsTerminal()` (using the appropriate receiver — often `exec.Status`, `other.Status`, `e.Status`). Apply consistently to list iterations and guards.

   | Package / file | Function / area | Current pattern |
   |----------------|-----------------|-----------------|
   | `pkg/admission/plugin.go` | `checkNoConcurrentExecution` | `e.Status.Result == ""` |
   | `pkg/controller/drplan/reconciler.go` | `hasActiveExecution` | `execList.Items[i].Status.Result == ""` |
   | `pkg/registry/drplan/strategy.go` | `buildActiveExecIndex` | `exec.Status.Result == ""` |
   | `pkg/controller/drexecution/reconciler.go` | `mapVMToDRExecution` | `execList.Items[i].Status.Result == ""` |
   | `pkg/controller/drexecution/reconciler.go` | `verifyExclusiveExecution` | `other.Status.Result == ""` |
   | `pkg/controller/drexecution/reconciler.go` | `Reconcile` — resume path gate | `exec.Status.Result == ""` (with `StartTime != nil`) |
   | `pkg/controller/drexecution/reconciler.go` | `Reconcile` — idempotency guard | See **AC4** (not a blind `IsTerminal()` swap) |
   | `pkg/engine/resume.go` | `AnalyzeExecution` | `exec.Status.Result != ""` |
   | `pkg/controller/drexecution/reconciler.go` | `recordExecutionMetrics` | `exec.Status.Result == ""` |
   | `pkg/registry/drexecution/storage.go` | `validateAuditDelete` | `exec.Status.Result != ""` |
   | `pkg/controller/drexecution/reconciler.go` | `reconcileWaveExecution` | `exec.Status.Result != ""` |

4. **AC4 — `Reconcile` idempotency guard preserves `PartiallySucceeded`:** The block that currently checks `Result == Succeeded || Result == Failed` before the early return **must not** become a bare `IsTerminal()` check. Bare `IsTerminal()` would also match `PartiallySucceeded` and would skip the retry path later in `Reconcile`. Refactor to an equivalent condition that still excludes `PartiallySucceeded` from the early-exit, for example:
   - keep `exec.Status.Result == ExecutionResultSucceeded || exec.Status.Result == ExecutionResultFailed`, or
   - use `exec.Status.IsTerminal() && exec.Status.Result != ExecutionResultPartiallySucceeded` (verify equivalence for all defined `ExecutionResult` constants), or
   - add a second small helper (only if the team prefers naming over inline logic) — **do not** change runtime ordering of PartiallySucceeded vs terminal cleanup.

5. **AC5 — Explicit exclusion:** Leave `pkg/apiserver/critical_fields.go` `detectDRExecutionCriticalFields` untouched regarding `oldExec.Status.Result != newExec.Status.Result` — that is a pairwise comparison, not a terminality check.

6. **AC6 — Codegen:** After editing `types.go`, run `make manifests generate`. The new method does not appear in generated OpenAPI/deepcopy as a separate artifact field, but type changes beside the method still require regeneration per project convention.

7. **AC7 — Tests and quality gates:** Update unit/integration tests that assert `Status.Result == ""` / `!= ""` for terminal semantics to prefer `Status.IsTerminal()` / `!Status.IsTerminal()` where it improves clarity. Run `make test`, `make integration` (if applicable for touched areas), and `make lint` — zero new failures.

8. **AC8 — Grep cleanliness:** After implementation, `rg 'Status\.Result\s*[=!]='` (and similar) across refactored packages should show no leftover terminality checks that duplicates `IsTerminal()` for the same meaning — except `AC4`/`AC5` exceptions and legitimate assignments or enum comparisons.

## Tasks / Subtasks

- [ ] Task 1: Add `IsTerminal()` to `DRExecutionStatus` (AC: #1, #2, #6)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `func (s DRExecutionStatus) IsTerminal() bool { return s.Result != "" }` adjacent to `DRExecutionStatus` / `ExecutionResult` definitions with a brief godoc
  - [ ] 1.2 Run `make manifests generate` and verify no unexpected churn beyond normal regeneration

- [ ] Task 2: Refactor admission and registry (AC: #3)
  - [ ] 2.1 `pkg/admission/plugin.go` — `checkNoConcurrentExecution`: use `!e.Status.IsTerminal()` (or equivalent) instead of `Result == ""`
  - [ ] 2.2 `pkg/registry/drplan/strategy.go` — `buildActiveExecIndex`: active predicate via `!exec.Status.IsTerminal()`
  - [ ] 2.3 `pkg/registry/drexecution/storage.go` — `validateAuditDelete`: terminal predicate via `exec.Status.IsTerminal()`

- [ ] Task 3: Refactor DRPlan controller (AC: #3)
  - [ ] 3.1 `pkg/controller/drplan/reconciler.go` — `hasActiveExecution`: use `!execList.Items[i].Status.IsTerminal()`

- [ ] Task 4: Refactor DRExecution controller (AC: #3, #4)
  - [ ] 4.1 `mapVMToDRExecution`: active execution selection via `!execList.Items[i].Status.IsTerminal()`
  - [ ] 4.2 `verifyExclusiveExecution`: `other.Status.Result == ""` → `!other.Status.IsTerminal()`
  - [ ] 4.3 `recordExecutionMetrics`: in-progress check via `!exec.Status.IsTerminal()`
  - [ ] 4.4 `reconcileWaveExecution`: guard using `exec.Status.IsTerminal()` instead of `Result != ""`
  - [ ] 4.5 `Reconcile` resume branch: replace `exec.Status.Result == ""` with `!exec.Status.IsTerminal()` alongside existing `StartTime` checks
  - [ ] 4.6 `Reconcile` idempotency guard: refactor per **AC4** — verify `PartiallySucceeded` still reaches `reconcileRetry`; add a regression test if none exists

- [ ] Task 5: Refactor engine resume (AC: #3)
  - [ ] 5.1 `pkg/engine/resume.go` — `AnalyzeExecution`: use `exec.Status.IsTerminal()` instead of `Result != ""`

- [ ] Task 6: Tests and sweep (AC: #7, #8)
  - [ ] 6.1 Search `_test.go` files and relevant tests for `Result == ""` / `!= ""` patterns tied to terminality; migrate to `IsTerminal()` where it matches story semantics
  - [ ] 6.2 Add or extend a focused test in `pkg/apis/soteria.io/v1alpha1` (table-driven) for `IsTerminal()` — empty, `Succeeded`, `Failed`, `PartiallySucceeded` — if no suitable package test file exists, use the nearest conventional location per project layout
  - [ ] 6.3 Run `make test`, `make integration`, `make lint`

- [ ] Task 7: Final verification (AC: #5, #8)
  - [ ] 7.1 Confirm `detectDRExecutionCriticalFields` still uses direct `Result` inequality for change detection
  - [ ] 7.2 Grep for remaining ad-hoc terminality checks; justify any intentional leftovers in code comments if absolutely necessary

## Dev Notes

### Scope & Architecture

Pure refactor: **one behavioral method, many call sites**, no user-visible API changes. The aggregated API server keeps types in `pkg/apis/soteria.io/v1alpha1/`. `IsTerminal()` is a **value receiver** on `DRExecutionStatus` so it works on structs and copies without requiring a pointer to the whole `DRExecution`.

**Semantic split (important):**

| Concept | Meaning in this codebase |
|---------|---------------------------|
| `IsTerminal()` | `Result != ""` — outcome recorded |
| DRPlan concurrency / “active execution” | Another execution with `!IsTerminal()` for the same plan |
| `Reconcile` top-of-function “done, skip forever” | **Only** fully closed outcomes **`Succeeded`** and **`Failed`**, plus stale retry annotation cleanup — **`PartiallySucceeded` must fall through** to the retry annotation path |

Therefore **`IsTerminal()` is not interchangeable** with the Reconcile early-exit condition without composing it with `Result != PartiallySucceeded`.

### Critical sections

**`DRExecutionStatus` (`types.go` ~414–424):**

```go
type DRExecutionStatus struct {
    Result          ExecutionResult   `json:"result,omitempty"`
    Waves           []WaveStatus      `json:"waves,omitempty"`
    StartTime       *metav1.Time      `json:"startTime,omitempty"`
    CompletionTime  *metav1.Time      `json:"completionTime,omitempty"`
    Conditions      []metav1.Condition `json:"conditions,omitempty"`
}
```

**`Reconcile` ordering (`pkg/controller/drexecution/reconciler.go`):**

Current structure (abbreviated):

1. Fetch `exec`.
2. If **Succeeded or Failed**: clean stale retry annotation if needed; return (skip).
3. `ensurePlanNameLabel`, fetch `plan`, site-aware dispatch.
4. If `StartTime != nil` **and** `Result == ""`: resume / wave paths.
5. If **PartiallySucceeded**: `reconcileRetry`.
6. Setup if `StartTime == nil`; etc.

Step 2 must remain **narrower** than `IsTerminal()`. Step 4 may use **`!IsTerminal()`** because `PartiallySucceeded` has non-empty `Result` and must **not** enter the resume path (different code path — step 5).

**`detectDRExecutionCriticalFields`:** Keep `oldExec.Status.Result != newExec.Status.Result` — detects **mutation** of the result field between objects, not “is terminal”.

### Existing Patterns

| Pattern | Where | This story |
|---------|-------|------------|
| Small predicate methods on API structs | Prefer value receivers on status structs | Match surrounding `types.go` style |
| Duplicate condition consolidation | Admission + controllers + registry | Single `IsTerminal()` |
| Logging | Kubernetes conventions | Not primary scope; touch only if editing nearby lines |

### File Structure & Impact Map

| File | Change Type |
|------|-------------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `IsTerminal()` + godoc |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.*.go` | Regenerated by `make manifests generate` |
| `pkg/admission/plugin.go` | Terminality via `IsTerminal()` |
| `pkg/controller/drplan/reconciler.go` | `hasActiveExecution` |
| `pkg/registry/drplan/strategy.go` | `buildActiveExecIndex` |
| `pkg/controller/drexecution/reconciler.go` | Multiple functions + careful `Reconcile` guard |
| `pkg/engine/resume.go` | `AnalyzeExecution` |
| `pkg/registry/drexecution/storage.go` | `validateAuditDelete` |
| `pkg/apiserver/critical_fields.go` | No change (exception) |
| `*_test.go` across impacted packages | Assertions / possibly new unit test table |

### Execution Order

1. Add `IsTerminal()` + `make manifests generate`
2. Refactor leaf call sites (admission, registry, engine resume, drplan reconciler)
3. Refactor DRExecution reconciler (non-`Reconcile` helpers first, then `Reconcile` resume line, then idempotency guard with **AC4** verification)
4. Tests + grep sweep + `make test` / `make integration` / `make lint`

### Previous Story Learnings (Epic 10)

- **10.1–10.3:** Active execution is derived via DRExecution list + label index; `buildActiveExecIndex` and `hasActiveExecution` already centralize discovery — this story **only** replaces the predicate `Result == ""` with `!IsTerminal()`.
- **10.4:** Types and codegen workflow confirmed for `pkg/apis/soteria.io/v1alpha1/types.go`; same regeneration discipline applies here.
- **PartiallySucceeded** remains a first-class retry state in `Reconcile`; do not collapse it into the succeeded/failed early return.

### Git Intelligence

Recent commits use **Story X.Y: …** prefixes. Prefer one logical commit or a small sequence following that convention.

### Project Structure Notes

- API types live under **`pkg/apis/soteria.io/v1alpha1/`** (aggregated API server layout, not Kubebuilder `api/` scaffolding).
- Tests use the **standard Go `testing`** package — follow existing table/fixture patterns in each package.
- Do **not** hand-edit **`zz_generated.*`** files.

### References

- [Source: `pkg/apis/soteria.io/v1alpha1/types.go`] — `DRExecution`, `DRExecutionStatus`, `ExecutionResult` constants
- [Source: `pkg/controller/drexecution/reconciler.go`] — `Reconcile` idempotency vs resume vs retry paths
- [Source: `pkg/admission/plugin.go`] — concurrency guard for parallel executions
- [Source: `pkg/registry/drplan/strategy.go`] — `buildActiveExecIndex`
- [Source: `pkg/engine/resume.go`] — `AnalyzeExecution`
- [Source: `pkg/registry/drexecution/storage.go`] — audit delete validation
- [Source: `pkg/apiserver/critical_fields.go`] — critical field detection (comparison exception)
- [Source: Kubernetes logging guidelines](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md#message-style-guidelines) — message style when touching logs
- [Source: `_bmad-output/implementation-artifacts/10-4-remove-activeexecution-fields-from-drplanstatus.md`] — prior epic story format and codegen notes

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
