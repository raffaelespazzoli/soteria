# Story 10.6: DRExecution Phase and IsActive Status Fields

Status: ready-for-dev

## Story

As a platform engineer and console operator,
I want `DRExecution` status to expose an explicit lifecycle `Phase` and an `IsActive` flag alongside the existing `Result`,
So that execution state is self-describing in the API, in `kubectl get` table output, and for future consumers without re-deriving semantics from `Result` and timestamps alone.

## Background

Epic 10 moves active-execution discovery off `DRPlanStatus` and onto `DRExecution` resources. Story 10.5 adds `IsTerminal()` on `DRExecution` (or equivalent) so callers can treat **terminal state** as `Result != ""` as the source of truth.

This story adds **first-class lifecycle fields** on `DRExecutionStatus`:

- **`Phase`** — human- and machine-readable lifecycle phase (`Pending` → `Executing` → terminal phases aligned with `ExecutionResult` where applicable).
- **`IsActive`** — `true` while the execution is in-flight (`Pending` or `Executing`); `false` once a terminal `Result` is written (and in failure paths that set `Result` without going through the happy-path executor finish).

Together, these make status self-describing while preserving backward compatibility: older stored objects without JSON keys deserialize to zero values, and the reconciler/engine refresh status on the next write.

**Architecture context:** Single aggregated API server; API types live under `pkg/apis/soteria.io/v1alpha1/`. Table output for `drexecutions` uses a custom table convertor in `pkg/registry/drexecution/storage.go`.

## Acceptance Criteria

1. **AC1 — API types:** `ExecutionPhase` type and constants (`Pending`, `Executing`, `Succeeded`, `PartiallySucceeded`, `Failed`) are added in `pkg/apis/soteria.io/v1alpha1/types.go`. `DRExecutionStatus` includes:
   - `Phase` with `+kubebuilder:validation:Enum=Pending;Executing;Succeeded;PartiallySucceeded;Failed` and `json:"phase,omitempty"`.
   - `IsActive` with `json:"isActive"` **without** `omitempty` so `false` is always serialized.
   - Field order: `Phase`, `IsActive`, then existing fields starting with `Result` (or as agreed in implementation, but `Phase`/`IsActive` must appear before `Result` in the struct to match the provided design).
2. **AC2 — `resultToPhase` helper:** A function maps `ExecutionResult` → `ExecutionPhase` for terminal results: `Succeeded` → `Succeeded`, `PartiallySucceeded` → `PartiallySucceeded`, `Failed` → `Failed`. Preferred location: `types.go` adjacent to `ExecutionPhase` / `ExecutionResult` (type-level helper).
3. **AC3 — Write path: create:** `PrepareForCreate` in `pkg/registry/drexecution/strategy.go` initializes `exec.Status` with `Phase: ExecutionPhasePending` and `IsActive: true` (replacing the empty status literal).
4. **AC4 — Write path: executing:** `reconcileSetup` in `pkg/controller/drexecution/reconciler.go` sets `exec.Status.Phase = ExecutionPhaseExecuting` when execution begins (StartTime path); `IsActive` remains `true`.
5. **AC5 — Write path: engine completion:** `finishExecution` in `pkg/engine/executor.go` sets `Phase` via `resultToPhase(result)`, `IsActive = false`, in addition to existing `Result` and `CompletionTime` updates.
6. **AC6 — Write path: reconciler failure:** `failExecution` in `pkg/controller/drexecution/reconciler.go` sets `Phase = ExecutionPhaseFailed` and `IsActive = false` alongside `Result = Failed`.
7. **AC7 — Write path: reprotect completion:** `reconcileReprotect` in `pkg/controller/drexecution/reconciler.go` sets terminal `Phase` from the outcome (`resultToPhase` or equivalent), `IsActive = false`, with `Result` / `CompletionTime` as today.
8. **AC8 — Write path: retry completion:** `ExecuteRetry` in `pkg/engine/executor.go` recomputes `Result`; also sets terminal `Phase` from that result and `IsActive = false`.
9. **AC9 — Table convertor:** `pkg/registry/drexecution/storage.go` extends `execTableColumns` and `execToRow` so column order is: Name, Plan, Mode, **Phase**, **Active**, Result, Duration, Age. Types: Phase `string`, Active `boolean`. Update the godoc comment on `DRExecutionTableConvertor` to list the new columns.
10. **AC10 — Codegen:** After `types.go` changes, `make manifests` and `make generate` (or `make manifests generate` per Makefile) regenerate DeepCopy, OpenAPI (`zz_generated.openapi.go`), and CRD manifests as required by this repo.
11. **AC11 — Tests:** Unit and integration tests updated or added so all write paths and table rows are covered where practical. All of `make test`, `make integration`, and `make lint` pass.
12. **AC12 — No regression on immutability:** Status subresource `ValidateUpdate` (`drexecutionStatusStrategy` in `pkg/registry/drexecution/strategy.go`) continues to gate terminal results; Phase and IsActive are written together with Result on terminal transitions, so behavior remains consistent — no unintended broadening of who can mutate status after terminality.

## Tasks / Subtasks

- [ ] **Task 1: Add `ExecutionPhase`, fields, and `resultToPhase`** (AC: #1, #2, #10)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `ExecutionPhase` string type and typed constants (`Pending`, `Executing`, `Succeeded`, `PartiallySucceeded`, `Failed`).
  - [ ] 1.2 Extend `DRExecutionStatus` with `Phase` and `IsActive` exactly as specified: `Phase` omitempty + kubebuilder Enum; **`IsActive` MUST use `json:"isActive"` with NO omitempty**.
  - [ ] 1.3 Add `func resultToPhase(r ExecutionResult) ExecutionPhase` next to phase/result definitions (handle only terminal result values used in production paths; document or panic-free map empty/unknown — align with existing `ExecutionResult` usage).
  - [ ] 1.4 Run `make manifests generate`; commit regenerated files (`zz_generated.*`, CRDs) with application code.

- [ ] **Task 2: `PrepareForCreate` initial status** (AC: #3)
  - [ ] 2.1 Replace `exec.Status = soteriav1alpha1.DRExecutionStatus{}` with `DRExecutionStatus{Phase: ExecutionPhasePending, IsActive: true}` in `pkg/registry/drexecution/strategy.go`.
  - [ ] 2.2 Extend `TestPrepareForCreate_*` tests in `pkg/registry/drexecution/strategy_test.go` to assert `Phase` and `IsActive`.

- [ ] **Task 3: Reconciler — `reconcileSetup`, `failExecution`, `reconcileReprotect`** (AC: #4, #6, #7)
  - [ ] 3.1 In `reconcileSetup`, when setting `StartTime`, set `exec.Status.Phase = ExecutionPhaseExecuting` (leave `IsActive` true).
  - [ ] 3.2 In `failExecution`, set `Phase = ExecutionPhaseFailed` and `IsActive = false`.
  - [ ] 3.3 In `reconcileReprotect`, when writing terminal status, set `Phase` from result and `IsActive = false`.

- [ ] **Task 4: Engine — `finishExecution`, `ExecuteRetry`** (AC: #5, #8)
  - [ ] 4.1 In `finishExecution`, set `exec.Status.Phase = resultToPhase(result)` and `exec.Status.IsActive = false`.
  - [ ] 4.2 In `ExecuteRetry`, after recomputing `Result`, set `Phase` and `IsActive = false`.

- [ ] **Task 5: Table convertor** (AC: #9)
  - [ ] 5.1 Update `execTableColumns` with `Phase` and `Active` after `Mode`, before `Result`.
  - [ ] 5.2 Update `execToRow` cells: include `string(exec.Status.Phase)` and `exec.Status.IsActive`.
  - [ ] 5.3 Update `pkg/registry/drexecution/storage_test.go` expectations for column count, names, and cell values.

- [ ] **Task 6: Sweep tests and critical fields** (AC: #11, #12)
  - [ ] 6.1 Search for `DRExecutionStatus{` / status fixtures in tests; add or adjust `Phase` / `IsActive` where assertions depend on table output or JSON shape.
  - [ ] 6.2 Verify `drexecutionStatusStrategy.ValidateUpdate` tests in `strategy_test.go` still pass; add cases if Phase/IsActive-only updates need explicit coverage (should follow Result gates).
  - [ ] 6.3 Run `make test`, `make integration`, `make lint`; fix failures.

- [ ] **Task 7: Commit** (convention)
  - [ ] 7.1 Commit with message: `Story 10.6: DRExecution Phase and IsActive status fields`

## Dev Notes

### Scope

**In scope:** API fields, all listed write paths, table convertor, codegen, tests, lint.

**Out of scope:** Changing `IsTerminal()` semantics (Story 10.5); DRPlan or console TypeScript types (unless a separate story requires column docs only).

### Critical: `IsActive` JSON tag

The user explicitly required:

```go
IsActive bool `json:"isActive"`  // NO omitempty
```

**Do not** add `omitempty` to `isActive`. Kubernetes clients and stored JSON must always see `"isActive": false` when false.

### `resultToPhase` mapping

Implement a small helper mapping **terminal** execution results:

| `ExecutionResult`      | `ExecutionPhase`      |
|------------------------|-----------------------|
| `Succeeded`            | `Succeeded`           |
| `PartiallySucceeded`   | `PartiallySucceeded`  |
| `Failed`               | `Failed`              |

Non-terminal / empty `Result` should not be passed to `resultToPhase` from `finishExecution` / retry completion paths — those transitions should already set explicit `Result`. If the helper receives `""`, return a deterministic value or document callers’ obligation (prefer aligning with existing engine invariants).

**Phase transitions summary:**

| Location           | Phase              | IsActive |
|--------------------|--------------------|----------|
| `PrepareForCreate` | `Pending`          | `true`   |
| `reconcileSetup`   | `Executing`        | `true`   |
| `finishExecution`  | from `resultToPhase` | `false` |
| `failExecution`    | `Failed`           | `false`  |
| `reconcileReprotect` | from result      | `false`  |
| `ExecuteRetry`     | from result        | `false`  |

### Backward compatibility (ScyllaDB / old JSON)

Older `DRExecution` documents without `phase` or `isActive` deserialize to:

- `Phase: ""` (zero value)
- `IsActive: false`

This is acceptable because:

- **`IsTerminal()` uses `Result != ""`** (Story 10.5), not `IsActive`.
- Table output may show blank Phase until the next reconciler/controller status write refreshes fields.
- No one-shot data migration required.

### ValidateUpdate / strategy split

Main resource updates go through `drexecutionStrategy.ValidateUpdate` (spec immutability). **Status mutations** use `drexecutionStatusStrategy.ValidateUpdate` in the same file — it rejects status changes once `Result` is `Succeeded` or `Failed` (see `pkg/registry/drexecution/doc.go` for intended semantics).

Phase and IsActive are updated in the same patches as Result on terminal transitions, so **no change is required** to the gate logic unless a review discovers a path that sets Phase/IsActive without setting Result first (avoid that).

### Codegen and verification commands

After editing `*_types.go`:

```bash
make manifests generate
make test
make integration
make lint
```

### Project conventions (recap)

- Aggregated API server; types in `pkg/apis/soteria.io/v1alpha1/`.
- Tests use the standard Go `testing` package.
- Commit subject: `Story 10.6: …` as in Task 7.

### Reference snippets (target end state)

**`types.go` (illustrative — match repo style and markers):**

```go
// ExecutionPhase is the lifecycle phase of a DRExecution.
type ExecutionPhase string

const (
	ExecutionPhasePending            ExecutionPhase = "Pending"
	ExecutionPhaseExecuting          ExecutionPhase = "Executing"
	ExecutionPhaseSucceeded          ExecutionPhase = "Succeeded"
	ExecutionPhasePartiallySucceeded ExecutionPhase = "PartiallySucceeded"
	ExecutionPhaseFailed             ExecutionPhase = "Failed"
)

type DRExecutionStatus struct {
	// Phase is the lifecycle phase of the execution.
	// +kubebuilder:validation:Enum=Pending;Executing;Succeeded;PartiallySucceeded;Failed
	Phase ExecutionPhase `json:"phase,omitempty"`
	// IsActive indicates whether the execution is in-flight (Pending or Executing).
	// Set to true on creation, false when a terminal Result is written.
	IsActive bool `json:"isActive"`
	// Result is the overall execution outcome.
	// +kubebuilder:validation:Enum=Succeeded;PartiallySucceeded;Failed
	Result ExecutionResult `json:"result,omitempty"`
	// ... remainder unchanged (Waves, StartTime, CompletionTime, Conditions)
}
```

**`storage.go` table:**

```go
var execTableColumns = []metav1.TableColumnDefinition{
	{Name: "Name", Type: "string", Format: "name"},
	{Name: "Plan", Type: "string"},
	{Name: "Mode", Type: "string"},
	{Name: "Phase", Type: "string"},
	{Name: "Active", Type: "boolean"},
	{Name: "Result", Type: "string"},
	{Name: "Duration", Type: "string"},
	{Name: "Age", Type: "string"},
}

func execToRow(exec *soteriav1alpha1.DRExecution) metav1.TableRow {
	return metav1.TableRow{
		Object: runtime.RawExtension{Object: exec},
		Cells: []any{
			exec.Name,
			exec.Spec.PlanName,
			string(exec.Spec.Mode),
			string(exec.Status.Phase),
			exec.Status.IsActive,
			string(exec.Status.Result),
			execDuration(exec),
			translateTimestampSince(exec.CreationTimestamp),
		},
	}
}
```

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List

### Change Log
