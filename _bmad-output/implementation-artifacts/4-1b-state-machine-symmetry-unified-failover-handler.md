# Story 4.1b: State Machine Symmetry & Unified Failover Handler

Status: done

## Story

As an operator,
I want the DR lifecycle to have a symmetric 8-phase state machine and a unified failover handler,
So that failover and failback are the same operation, reprotect and restore are the same operation, and the FailedBack state correctly represents the unprotected period after failback.

## Background — Design Correction

This story is a correction that addresses three design insights identified during Epic 4 implementation:

1. **Planned migration and disaster failover are the same core workflow** — planned migration adds a pre-execution phase (Step 0: stop VMs, stop replication, sync wait). Disaster skips Step 0 and uses force=true. A single `FailoverHandler` with configuration replaces two separate handlers.

2. **Failover and failback are the same operation** — just swapping active and passive cluster. The naming is a distinction useful to humans and audit trails, not to code. The handler does not distinguish direction.

3. **The state machine was asymmetric (6 phases)** — it skipped `FailedBack` and the "restore" operation, jumping directly from `FailingBack` to `SteadyState`. The PRD says "4-state cycle — failover, re-protect, failback, restore" (4 operations, 4 rest states). The 6-phase model was missing the 4th rest state and the 4th operation.

## The 8-Phase Model

**Rest states (4):**
| Phase | Active Site | Replication | Meaning |
|-------|-------------|-------------|---------|
| `SteadyState` | Site A | A → B | Original config, fully protected |
| `FailedOver` | Site B | None | Failed over, unprotected |
| `DRedSteadyState` | Site B | B → A | DR site active, fully protected |
| `FailedBack` | Site A | None | Failed back, unprotected |

**Transition states (4):**
| Phase | Operation | Handler |
|-------|-----------|---------|
| `FailingOver` | Failover in progress | FailoverHandler |
| `Reprotecting` | Reprotect in progress | ReprotectHandler |
| `FailingBack` | Failback in progress | FailoverHandler |
| `ReprotectingBack` | Restore in progress | ReprotectHandler |

**State machine transitions (start):**
- SteadyState + (planned_migration\|disaster) → FailingOver
- FailedOver + reprotect → Reprotecting
- DRedSteadyState + (planned_migration\|disaster) → FailingBack
- FailedBack + reprotect → ReprotectingBack

**State machine completions:**
- FailingOver → FailedOver
- Reprotecting → DRedSteadyState
- FailingBack → FailedBack
- ReprotectingBack → SteadyState

## Acceptance Criteria

1. **AC1 — Two new phase constants:** `pkg/apis/soteria.io/v1alpha1/types.go` adds `PhaseFailedBack = "FailedBack"` and `PhaseReprotectingBack = "ReprotectingBack"`. `DRPlanStatus.Phase` comment updated to list all 8 phases.

2. **AC2 — State machine 8-phase symmetry:** `pkg/engine/statemachine.go` updated: `validTransitions` adds `FailedBack + reprotect → ReprotectingBack`. `completionTransitions` changes `FailingBack → FailedBack` (was `SteadyState`) and adds `ReprotectingBack → SteadyState`. `terminalPhases` adds `FailedBack`. `ValidStartingPhases("reprotect")` returns `["FailedOver", "FailedBack"]`.

3. **AC3 — Unified FailoverHandler:** `pkg/engine/failover.go` replaces `pkg/engine/planned.go`. A single `FailoverHandler` struct accepts `FailoverConfig{GracefulShutdown bool, Force bool, RecordRPO bool}`. When `GracefulShutdown=true`: `PreExecute` runs Step 0 (stop VMs, StopReplication, sync wait), and per-DRGroup calls `StopReplication(force=false)` + `SetSource(force=false)` + `StartVM`. When `GracefulShutdown=false`: `PreExecute` is a no-op, per-DRGroup calls `SetSource(force=true)` + `StartVM`, and RPO is recorded from `GetReplicationStatus.LastSyncTime`.

4. **AC4 — Controller dispatch uses unified handler:** The DRExecution controller creates `FailoverHandler` with config based on execution mode: `planned_migration → {GracefulShutdown: true, Force: false, RecordRPO: false}`, `disaster → {GracefulShutdown: false, Force: true, RecordRPO: true}`. The handler is used for both failover (from SteadyState) and failback (from DRedSteadyState) — no directional logic.

5. **AC5 — Reprotect mode accepted in validation:** `pkg/apis/soteria.io/v1alpha1/validation.go` and `pkg/admission/drexecution_validator.go` accept `reprotect` as a valid execution mode. The admission webhook calls `engine.Transition(plan.Status.Phase, exec.Spec.Mode)` which validates `FailedOver + reprotect` and `FailedBack + reprotect`. The registry strategy in `pkg/registry/drexecution/strategy.go` also accepts `reprotect`.

6. **AC6 — Old files removed:** `pkg/engine/planned.go` and `pkg/engine/planned_test.go` are deleted. Their logic is merged into `pkg/engine/failover.go` and `pkg/engine/failover_test.go`. Step constants (`StepStopReplication`, `StepSetSource`, `StepStartVM`) move to `pkg/engine/failover.go` or a shared location.

7. **AC7 — Existing tests updated:** State machine tests updated for 8 phases (4 transitions, 4 completions, `FailedBack` as terminal, `ReprotectingBack` lifecycle). Planned migration handler tests become FailoverHandler tests with `FailoverConfig{GracefulShutdown: true}`. Controller dispatch tests verify handler selection for both failover modes from both starting states (SteadyState and DRedSteadyState).

8. **AC8 — `make manifests` and `make generate` clean:** Adding new phase constants may not require deepcopy regeneration (string constants), but `make manifests` must be run to verify. No CRD structural changes (phase is a string field).

9. **AC9 — All existing tests pass:** No regressions in Stories 4.1, 4.2, 4.3 tests. Coverage for the engine package remains at or above 82%.

## Tasks / Subtasks

- [x] Task 1: Add new phase constants (AC: #1)
  - [x] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `PhaseFailedBack = "FailedBack"` and `PhaseReprotectingBack = "ReprotectingBack"` after `PhaseFailingBack`
  - [x] 1.2 Update the `DRPlanStatus.Phase` comment to list all 8 phases
  - [x] 1.3 Run `make generate` to verify (likely no deepcopy changes needed for string constants)

- [x] Task 2: Update state machine for 8-phase symmetry (AC: #2)
  - [x] 2.1 In `pkg/engine/statemachine.go`, add `FailedBack` entry to `validTransitions` map: `FailedBack: {ExecutionModeReprotect: PhaseReprotectingBack}` — requires adding `ExecutionModeReprotect` constant first (see Task 3)
  - [x] 2.2 Fix `completionTransitions`: change `PhaseFailingBack: PhaseSteadyState` to `PhaseFailingBack: PhaseFailedBack`
  - [x] 2.3 Add `PhaseReprotectingBack: PhaseSteadyState` to `completionTransitions`
  - [x] 2.4 Add `PhaseFailedBack: true` to `terminalPhases`
  - [x] 2.5 Update `FailedOver` entry in `validTransitions` to add reprotect: `FailedOver: {ExecutionModeReprotect: PhaseReprotecting}`
  - [x] 2.6 Update the Tier 2 architecture block comment at the top of `statemachine.go` to reflect the full 8-phase diagram

- [x] Task 3: Add ExecutionModeReprotect constant (AC: #5)
  - [x] 3.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `ExecutionModeReprotect ExecutionMode = "reprotect"` after `ExecutionModeDisaster`

- [x] Task 4: Update validation to accept reprotect mode (AC: #5)
  - [x] 4.1 In `pkg/registry/drexecution/strategy.go`, update `Validate` to accept `ExecutionModeReprotect`
  - [x] 4.2 In `pkg/apis/soteria.io/v1alpha1/validation.go`, update `ValidateDRExecution` to accept `reprotect`
  - [x] 4.3 In `pkg/admission/drexecution_validator.go`, update mode validation to accept `reprotect`
  - [x] 4.4 Add unit tests for reprotect mode validation

- [x] Task 5: Refactor PlannedMigrationHandler → FailoverHandler (AC: #3, #6)
  - [x] 5.1 Create `pkg/engine/failover.go` — copy content from `planned.go` as starting point
  - [x] 5.2 Define `FailoverConfig` struct: `GracefulShutdown bool`, `Force bool`, `RecordRPO bool`
  - [x] 5.3 Rename `PlannedMigrationHandler` → `FailoverHandler`, add `Config FailoverConfig` field
  - [x] 5.4 Update `PreExecute`: if `!Config.GracefulShutdown`, return nil immediately (no Step 0)
  - [x] 5.5 Update `ExecuteGroup`: use `Config.Force` in `SetSourceOptions{Force: h.Config.Force}`. Skip `StopReplication` when `!Config.GracefulShutdown`. Add RPO recording from `GetReplicationStatus.LastSyncTime` when `Config.RecordRPO` (port from Story 4.4 design)
  - [x] 5.6 Delete `pkg/engine/planned.go`
  - [x] 5.7 Move step constants to top of `failover.go` (or `pkg/engine/steps.go` if shared with reprotect)

- [x] Task 6: Update controller dispatch (AC: #4)
  - [x] 6.1 In `pkg/controller/drexecution/reconciler.go`, update `resolveHandler` to create `FailoverHandler` with appropriate config based on mode
  - [x] 6.2 For `planned_migration`: `FailoverConfig{GracefulShutdown: true, Force: false, RecordRPO: false}`
  - [x] 6.3 For `disaster`: `FailoverConfig{GracefulShutdown: false, Force: true, RecordRPO: true}`
  - [x] 6.4 For `reprotect`: leave as NoOpHandler placeholder (Story 4.8 implements ReprotectHandler)
  - [x] 6.5 Both failover (from SteadyState) and failback (from DRedSteadyState) use the same handler — no directional logic needed

- [x] Task 7: Update tests (AC: #7, #9)
  - [x] 7.1 Create `pkg/engine/failover_test.go` — port tests from `planned_test.go`, rename to test `FailoverHandler`
  - [x] 7.2 Add test: `TestFailoverHandler_DisasterConfig_NoStep0` — verify PreExecute is no-op with GracefulShutdown=false
  - [x] 7.3 Add test: `TestFailoverHandler_DisasterConfig_ForceTrue` — verify SetSourceOptions{Force: true}
  - [x] 7.4 Add test: `TestFailoverHandler_DisasterConfig_RPORecorded` — verify RPO from GetReplicationStatus
  - [x] 7.5 Add test: `TestFailoverHandler_DisasterConfig_NoStopReplication` — verify StopReplication not called
  - [x] 7.6 Add test: `TestFailoverHandler_DisasterConfig_GetReplicationStatusFails` — RPO "unknown", group succeeds
  - [x] 7.7 Delete `pkg/engine/planned_test.go`
  - [x] 7.8 Update state machine tests for 8 phases: `TestTransition_Reprotect_FromFailedOver`, `TestTransition_Reprotect_FromFailedBack`, `TestCompleteTransition_FailingBack_ToFailedBack`, `TestCompleteTransition_ReprotectingBack_ToSteadyState`, `TestIsTerminalPhase_FailedBack`
  - [x] 7.9 Update controller integration tests: verify handler dispatch for both modes from both starting states
  - [x] 7.10 Update admission webhook tests: verify `reprotect` mode accepted from `FailedOver` and `FailedBack`
  - [x] 7.11 Add lifecycle test: `TestFullLifecycle_8Phases` — SteadyState → FailedOver → DRedSteadyState → FailedBack → SteadyState using state machine functions

- [x] Task 8: Update wiring and documentation (AC: #8)
  - [x] 8.1 Update `cmd/soteria/main.go` if handler creation changes (FailoverHandler replaces PlannedMigrationHandler)
  - [x] 8.2 Update `pkg/engine/doc.go` — document the 8-phase model, unified FailoverHandler, and FailoverConfig
  - [x] 8.3 Run `make manifests` to regenerate RBAC/webhook configs
  - [x] 8.4 Run `make generate` to verify deepcopy
  - [x] 8.5 Run `make test` — all tests pass
  - [ ] 8.6 Run `make lint-fix` followed by `make lint` (blocked by pre-existing unrelated `goconst` in `internal/preflight/storage_test.go`)
  - [x] 8.7 Run `make build` — compiles cleanly

### Review Findings

- [x] [Review][Patch] Gate Step 0 bookkeeping to graceful mode only [`pkg/controller/drexecution/reconciler.go:180`] — disaster executions now go through the generic `PreExecute` path, so the controller still sets `Step0Complete` and emits `PlannedMigrationStarted` / `Step0Failed` events even though disaster mode intentionally skips Step 0.
- [x] [Review][Patch] Treat nil `LastSyncTime` as `RPO: unknown` [`pkg/engine/failover.go:329`] — `ReplicationStatus.LastSyncTime` is documented as nil when the driver cannot report sync time, but both RPO-recording paths currently omit any unknown marker instead of recording that the RPO is unavailable.
- [x] [Review][Patch] Add reprotect validation and webhook tests [`pkg/apis/soteria.io/v1alpha1/validation_test.go:116`] — the validation code now accepts `reprotect`, but the unit and admission test suites still cover only `planned_migration` and `disaster`, so the new mode is unprotected by tests.
- [x] [Review][Patch] Add failback controller dispatch coverage [`test/integration/controller/drexecution_test.go:34`] — the controller integration suite exercises only `SteadyState -> FailedOver` paths and does not verify handler dispatch from `DRedSteadyState` for either failback mode, which was part of the story's acceptance coverage.

## Dev Notes

### Architecture Context

This is a correction story inserted between Story 4.1 (done) and Story 4.4 (ready-for-dev). It applies three design insights that emerged during the 4.3 review cycle. Story 4.3's planned migration handler is absorbed into this story as the `FailoverHandler` refactor. Story 4.4 simplifies dramatically — it only needs to add disaster-specific tests and RPO recording to the already-unified handler.

### Epic 4 Story Chain (Updated)

| Story | Deliverable | Status |
|-------|-------------|--------|
| 4.05 | Registry fallback + preflight convergence | done |
| 4.1 | State machine (6-phase) + execution controller + admission webhook | done |
| **4.1b** | **8-phase state machine + unified FailoverHandler** | **done (this story)** |
| 4.2 | Wave executor framework + controller dispatch | done |
| 4.3 | Planned migration workflow + VMManager | review → absorbed into 4.1b |
| 4.4 | Disaster failover — add disaster config + RPO to FailoverHandler | ready-for-dev (simplified) |
| 4.5 | Fail-forward error handling & partial success | ready-for-dev |
| 4.6 | Failed DRGroup retry | ready-for-dev |
| 4.7 | Checkpoint, resume & HA | ready-for-dev |
| 4.8 | Re-protect & failback workflows | ready-for-dev (simplified — FailedBack exists) |

### Existing Code to Modify

| File | Current State | Changes |
|------|--------------|---------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | 6 phases, 2 execution modes | Add `PhaseFailedBack`, `PhaseReprotectingBack`, `ExecutionModeReprotect` |
| `pkg/engine/statemachine.go` | 6-phase model | Full 8-phase transitions and completions |
| `pkg/engine/planned.go` | PlannedMigrationHandler | Rename to `failover.go`, refactor to `FailoverHandler` with `FailoverConfig` |
| `pkg/engine/planned_test.go` | Planned migration tests | Rename to `failover_test.go`, add disaster config tests |
| `pkg/controller/drexecution/reconciler.go` | Creates PlannedMigrationHandler for planned_migration | Create FailoverHandler with mode-based config |
| `pkg/admission/drexecution_validator.go` | Accepts planned_migration, disaster | Add reprotect |
| `pkg/registry/drexecution/strategy.go` | Validates mode is planned_migration or disaster | Add reprotect |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Validates mode | Add reprotect |
| `cmd/soteria/main.go` | Wires PlannedMigrationHandler | Wire FailoverHandler |
| `pkg/engine/doc.go` | Documents 6-phase model | Document 8-phase model + unified handler |

### New Files

| File | Purpose |
|------|---------|
| `pkg/engine/failover.go` | Unified FailoverHandler (replaces `planned.go`) |
| `pkg/engine/failover_test.go` | FailoverHandler tests for both configs (replaces `planned_test.go`) |

### Files to Delete

| File | Reason |
|------|--------|
| `pkg/engine/planned.go` | Replaced by `failover.go` |
| `pkg/engine/planned_test.go` | Replaced by `failover_test.go` |

### Key Design Decisions

**1. FailoverConfig drives behavior, not the mode string.**

The handler receives a config struct, not the execution mode. This makes testing trivial (test each config field independently) and avoids string-switching inside the handler. The controller maps mode → config at dispatch time.

**2. RPO recording is part of the FailoverHandler, gated by config.**

When `RecordRPO=true`, after each successful `SetSource`, the handler calls `GetReplicationStatus` to read `LastSyncTime` and includes RPO in the step message. If `GetReplicationStatus` fails, RPO is recorded as "unknown" and the group succeeds. This logic is ported from the Story 4.4 design.

**3. Step 0 deduplication for the unified handler.**

`PreExecute` checks `Config.GracefulShutdown`. If false, returns nil immediately. If true, runs the full Step 0 sequence (stop VMs → stop replication → sync wait). The `Step0Complete` condition guard from Story 4.3 review is preserved for idempotent reconciliation.

**4. StopReplication is skipped in disaster mode.**

When `Config.GracefulShutdown=false`, the per-DRGroup `ExecuteGroup` skips `StopReplication` entirely. The `SetSource(force=true)` call handles promotion without prior replication stop.

**5. State machine completion for FailingBack → FailedBack is a correctness fix.**

The original `FailingBack → SteadyState` implied replication magically re-established after failback. In reality, after failback the system is unprotected (like FailedOver but reversed). `FailedBack` makes this risk visible. The operator must trigger restore (reprotect from FailedBack) to return to SteadyState.

### Build Commands

```bash
make manifests    # Regenerate RBAC/webhook after new mode
make generate     # Verify deepcopy (new constants are strings, likely no changes)
make test         # All tests (new + existing)
make lint-fix     # Auto-fix
make lint         # Verify
make build        # Compile
```

### References

- [Source: _bmad-output/planning-artifacts/prd.md#Project Classification] — "4 rest states, 8 phases"
- [Source: _bmad-output/planning-artifacts/prd.md#Execution Mode Model] — Three execution modes, unified handler description
- [Source: _bmad-output/planning-artifacts/prd.md#FR16] — Re-protect from FailedOver and FailedBack
- [Source: _bmad-output/planning-artifacts/prd.md#FR17] — Failback lands at FailedBack, restore required
- [Source: _bmad-output/planning-artifacts/architecture.md#CRD Status Patterns] — 8 phases listed
- [Source: _bmad-output/planning-artifacts/architecture.md#API & Communication] — Unified handler model row
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — failover.go, reprotect.go
- [Source: _bmad-output/project-context.md#Framework-Specific Rules] — 8-phase lifecycle, unified handler model
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 4] — Updated epic description and story BDD criteria
- [Source: pkg/engine/statemachine.go] — Current 6-phase state machine to extend
- [Source: pkg/engine/planned.go] — Current PlannedMigrationHandler to refactor
- [Source: pkg/engine/planned_test.go] — Tests to port to FailoverHandler
- [Source: pkg/controller/drexecution/reconciler.go] — Controller dispatch to update
- [Source: _bmad-output/implementation-artifacts/4-3-planned-migration-workflow.md] — Story 4.3 review findings to incorporate
- [Source: _bmad-output/implementation-artifacts/4-4-disaster-failover-workflow.md] — RPO recording design to port
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — Original state machine design

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
