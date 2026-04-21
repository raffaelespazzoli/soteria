# Story 4.8: Re-protect & Failback Workflows

Status: ready-for-dev

## Story

As an operator,
I want to re-establish replication after failover via re-protect, fail back to the original site, then restore replication to return to steady state,
So that the system returns to full DR protection after the complete 8-phase lifecycle.

## Acceptance Criteria

1. **AC1 — ExecutionModeReprotect:** `ExecutionModeReprotect` and the state machine transitions are already implemented by Story 4.1b. This story implements the `ReprotectHandler` that executes the storage-only workflow. Failback does NOT require a new mode — it reuses `planned_migration` or `disaster` from `DRedSteadyState` (the state machine maps `DRedSteadyState + planned_migration → FailingBack` and `DRedSteadyState + disaster → FailingBack`).

2. **AC2 — State machine extension for re-protect:** State machine transitions are already implemented: `FailedOver + reprotect → Reprotecting` and `FailedBack + reprotect → ReprotectingBack` (Story 4.1b). This story verifies them. `CompleteTransition("Reprotecting")` returns `DRedSteadyState` and `CompleteTransition("ReprotectingBack")` returns `SteadyState` (Story 4.1b). Failback transitions `DRedSteadyState + planned_migration/disaster → FailingBack` are already defined.

3. **AC3 — Re-protect handler:** `pkg/engine/reprotect.go` implements a `ReprotectHandler` used for BOTH re-protect (`FailedOver` → `Reprotecting` → `DRedSteadyState`) and restore (`FailedBack` → `ReprotectingBack` → `SteadyState`). The handler does not distinguish direction — it receives volume groups and establishes replication. Workflow: storage-only (no waves, no VM start/stop). For each volume group in the plan: (a) call `StopReplication(force=true)` — tolerate failure if old active site is unreachable (FR16); (b) call `SetSource(force=false)` to establish the current site's volumes as replication source. The storage backend configures the remote site as Target implicitly via the replication pair.

4. **AC4 — Origin unreachable tolerance during re-protect:** When `StopReplication` fails for a volume group (e.g., old active site unreachable), the handler logs the error at V(1) and proceeds with `SetSource` for that volume group. The re-protect continues for all VGs — a `StopReplication` failure does not fail the execution.

5. **AC5 — Replication health monitoring:** After all VGs have been set to Source, the handler enters a polling loop calling `GetReplicationStatus` for each VG at a configurable interval (default 30s). The loop continues until all VGs report `HealthHealthy` or `HealthSyncing` that progresses to `HealthHealthy`, or until the configurable timeout is reached (default 24h). DRExecution status conditions are updated with resync progress: `type=Replicating, status=True, reason=SyncInProgress, message="4/6 volume groups healthy"`. DRPlan status conditions are also updated with `type=Replicating, status=True` to reflect ongoing resync.

6. **AC6 — Re-protect health monitoring timeout:** If the health monitoring timeout expires before all VGs report Healthy, the execution is marked `PartiallySucceeded` (not Failed) — replication may still be syncing and will eventually catch up. The DRPlan phase still advances past the transition phase because role setup succeeded and replication is in progress: to `DRedSteadyState` when re-protecting from `FailedOver`, or to `SteadyState` when restoring from `FailedBack`. The operator can monitor replication health via the DRPlan status conditions.

7. **AC7 — Re-protect step recording:** Each operation is recorded as a `StepStatus` entry in the DRExecution status. Step names: `"StopReplication"` (per VG), `"SetSource"` (per VG), `"HealthMonitoring"` (one summary step with final health snapshot). This provides visibility into exactly which VG had issues during re-protect.

8. **AC8 — Re-protect checkpoint integration:** Re-protect uses the `Checkpointer` from Story 4.7 to persist progress. After each VG's role setup completes (StopReplication + SetSource), a checkpoint is written. After each health poll iteration, a checkpoint is written with the current health snapshot. On pod restart, the handler resumes from the checkpoint: if role setup is done, it skips to health monitoring.

9. **AC9 — Failback reuses FailoverHandler:** Failback is triggered by creating a DRExecution with `mode=planned_migration` or `mode=disaster` when the DRPlan is in `DRedSteadyState`. The same unified `FailoverHandler` from Story 4.1b handles it. On completion, the DRPlan transitions from `FailingBack` to `FailedBack` (not `SteadyState`). The operator must then trigger restore (reprotect from `FailedBack`) to return to `SteadyState`.

10. **AC10 — Controller integration:** The DRExecution reconciler dispatches to `ReprotectHandler` when `exec.Spec.Mode == "reprotect"`. For re-protect mode from `FailedOver`: (a) transition DRPlan phase from `FailedOver` → `Reprotecting`; (b) discover all volume groups from the plan (same discovery as failover); (c) resolve drivers per VG via `PVCResolver`; (d) dispatch `ReprotectHandler.Execute()` — not via `WaveExecutor` (re-protect is not wave-based); (e) on completion, call `CompleteTransition` to advance DRPlan from `Reprotecting` → `DRedSteadyState`. For restore (reprotect from `FailedBack`): transition `FailedBack` → `ReprotectingBack`, then on completion `ReprotectingBack` → `SteadyState` via `CompleteTransition` (same handler, same discovery path). For failback (mode=`planned_migration`/`disaster` from `DRedSteadyState`): dispatch uses the unified `FailoverHandler` from Story 4.1b; on completion the plan is `FailedBack` until restore runs.

11. **AC11 — Re-protect event emission:** The controller emits events: `ReprotectStarted` when a `reprotect` execution begins (from `FailedOver` or `FailedBack`), `ReprotectRoleSetupComplete` when all VGs have been set to Source, `ReprotectHealthy` when all VGs report Healthy, `ReprotectTimeout` if health monitoring times out. Failback uses the existing `FailoverStarted`/`FailoverCompleted` events (same engine).

12. **AC12 — Full DR lifecycle validation:** Unit tests validate the complete 8-phase cycle: `SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState`. Each transition produces a DRExecution record where applicable. The cycle can be repeated.

13. **AC13 — Re-protect Prometheus metrics:** Metrics are emitted: `soteria_reprotect_duration_seconds` (histogram — total re-protect time including health monitoring), `soteria_reprotect_vg_setup_duration_seconds` (histogram — role setup phase only), `soteria_reprotect_health_polls_total` (counter — number of health poll iterations). These use the `soteria_` prefix per project convention.

14. **AC14 — Unit tests:** Comprehensive tests covering: (a) successful re-protect — StopReplication + SetSource for all VGs, health monitoring completes; (b) StopReplication failure tolerated — handler proceeds, logs warning; (c) SetSource failure — VG marked failed, execution fails; (d) health monitoring timeout — execution marked PartiallySucceeded; (e) health monitoring completes — all VGs Healthy; (f) resume after restart — role setup already done, resumes monitoring; (g) checkpoint writes during re-protect; (h) failback via planned_migration from DRedSteadyState — unified FailoverHandler wave execution; (i) failback via disaster from DRedSteadyState — unified FailoverHandler force-promote wave execution; (j) state machine: FailedOver + reprotect → Reprotecting; (k) state machine: FailedBack + reprotect → ReprotectingBack; (l) state machine: DRedSteadyState + planned_migration → FailingBack; (m) full 8-phase lifecycle: SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState; (n) restore: reprotect from FailedBack completes to SteadyState; (o) admission/strategy: reprotect mode already accepted (4.1b) — verify only.

## Tasks / Subtasks

- [ ] Task 1: Add ExecutionModeReprotect constant (AC: #1) — **Already done in 4.1b; verify only**
  - [ ] 1.1 Confirm `ExecutionModeReprotect` exists in `pkg/apis/soteria.io/v1alpha1/types.go` (Story 4.1b)
  - [ ] 1.2 Run `make generate` / `make manifests` if types change — otherwise verify no drift
  - [ ] 1.3 Add or extend verification tests only if gaps exist vs Story 4.1b

- [ ] Task 2: Update validation to accept reprotect mode (AC: #1) — **Already done in 4.1b; verify only**
  - [ ] 2.1 Confirm `pkg/registry/drexecution/strategy.go` accepts `ExecutionModeReprotect`
  - [ ] 2.2 Confirm `pkg/apis/soteria.io/v1alpha1/validation.go` (if applicable) accepts `reprotect`
  - [ ] 2.3 Confirm `pkg/admission/drexecution_validator.go` accepts `reprotect` and uses `engine.Transition` for phase validation
  - [ ] 2.4 Verify unit tests in `strategy_test.go` / `drexecution_validator_test.go` from 4.1b still pass; add only if coverage is missing

- [ ] Task 3: Extend state machine for re-protect transition (AC: #2) — **Already done in 4.1b; verify only**
  - [ ] 3.1 Confirm `FailedOver + reprotect → Reprotecting` and `FailedBack + reprotect → ReprotectingBack` in `pkg/engine/statemachine.go`
  - [ ] 3.2 Confirm `ValidStartingPhases("reprotect")` includes both `FailedOver` and `FailedBack` (Story 4.1b)
  - [ ] 3.3 Verify `CompleteTransition("Reprotecting") → "DRedSteadyState"` and `CompleteTransition("ReprotectingBack") → "SteadyState"`
  - [ ] 3.4 Add verification tests or extend existing state machine tests: reprotect from `FailedOver` and from `FailedBack`, invalid phases rejected

- [ ] Task 4: Implement ReprotectHandler (AC: #3, #4, #7)
  - [ ] 4.1 Create `pkg/engine/reprotect.go` with copyright header and Tier 2 architecture block comment explaining the storage-only workflow (re-protect from `FailedOver` and restore from `FailedBack` — same handler): StopReplication + SetSource + health monitoring
  - [ ] 4.2 Define `ReprotectHandler` struct with fields: `DriverResolver` (function that returns `StorageProvider` per VG), `Checkpointer engine.Checkpointer`, `HealthPollInterval time.Duration` (default 30s), `HealthTimeout time.Duration` (default 24h)
  - [ ] 4.3 Define `ReprotectInput` struct: `Execution *v1alpha1.DRExecution`, `Plan *v1alpha1.DRPlan`, `VolumeGroups []VolumeGroupEntry` where `VolumeGroupEntry` bundles a `VolumeGroupInfo` (from plan discovery) with its resolved `drivers.StorageProvider` and `drivers.VolumeGroupID`
  - [ ] 4.4 Implement `Execute(ctx context.Context, input ReprotectInput) (*ReprotectResult, error)` — the main entry point
  - [ ] 4.5 Phase 1 — Role setup: iterate all VGs. For each: (a) call `driver.StopReplication(ctx, vgID, StopReplicationOptions{Force: true})` — on error, log at V(1) `"StopReplication failed for volume group, proceeding"` and continue; (b) call `driver.SetSource(ctx, vgID, SetSourceOptions{Force: false})` — on error, mark VG as failed and record StepStatus with error. If ALL SetSource calls fail, return error (execution fails). If some fail, proceed to health monitoring for the successful ones
  - [ ] 4.6 Phase 2 — Record step status for each VG operation using existing step recording patterns from `failover.go` (unified FailoverHandler; same patterns as prior planned/disaster handlers)
  - [ ] 4.7 Define step name constants: `StepReprotectStopReplication = "StopReplication"`, `StepReprotectSetSource = "SetSource"`, `StepReprotectHealthMonitoring = "HealthMonitoring"`

- [ ] Task 5: Implement health monitoring loop (AC: #5, #6, #8)
  - [ ] 5.1 In `pkg/engine/reprotect.go`, implement `monitorHealth(ctx context.Context, input ReprotectInput, successfulVGs []VolumeGroupEntry) error`
  - [ ] 5.2 Use `time.NewTicker(r.HealthPollInterval)` and `ctx.Done()` for cancellation-aware polling. Also create a `time.After(r.HealthTimeout)` for the overall timeout
  - [ ] 5.3 On each tick: call `driver.GetReplicationStatus(ctx, vgID)` for each successful VG. Count how many report `HealthHealthy`. Log at V(1): `"Replication health check"` with `healthy`/`total` counts
  - [ ] 5.4 If all VGs report Healthy: return nil (success)
  - [ ] 5.5 If timeout expires: return a typed error `ErrReprotectHealthTimeout` with the count of VGs not yet healthy
  - [ ] 5.6 Write checkpoint via `Checkpointer.WriteCheckpoint` after each poll iteration (status includes current health snapshot)
  - [ ] 5.7 Update DRExecution status conditions with progress: `type=Replicating, reason=SyncInProgress, message="N/M volume groups healthy"`

- [ ] Task 6: Define ReprotectResult (AC: #6, #7)
  - [ ] 6.1 Define `ReprotectResult` struct: `SetupSucceeded int`, `SetupFailed int`, `HealthyVGs int`, `TotalVGs int`, `TimedOut bool`, `FailedVGs []string` (names of VGs that failed SetSource)
  - [ ] 6.2 Return `ExecutionResultSucceeded` if all VGs are healthy after monitoring
  - [ ] 6.3 Return `ExecutionResultPartiallySucceeded` if health monitoring timed out but role setup succeeded
  - [ ] 6.4 Return `ExecutionResultFailed` if all SetSource calls failed (role setup failed completely)

- [ ] Task 7: Wire re-protect in DRExecution controller (AC: #10, #11)
  - [ ] 7.1 In `pkg/controller/drexecution/reconciler.go`, add `ReprotectHandler *engine.ReprotectHandler` field to `DRExecutionReconciler`
  - [ ] 7.2 In the reconcile dispatch, add a new branch: when `exec.Spec.Mode == v1alpha1.ExecutionModeReprotect`
  - [ ] 7.3 Validate plan phase via state machine `Transition` — reprotect is valid from `FailedOver` or `FailedBack` (Story 4.1b)
  - [ ] 7.4 After `Transition` succeeds, ensure DRPlan is in `Reprotecting` (from `FailedOver`) or `ReprotectingBack` (from `FailedBack`) per Story 4.1b; discover all volume groups from the plan using the existing discovery + consistency pipeline (reuse from failover path)
  - [ ] 7.5 Resolve drivers per VG using `PVCResolver` (from Story 4.5)
  - [ ] 7.6 Resolve VolumeGroupIDs via `CreateVolumeGroup` (idempotent, reuse `resolveVolumeGroupID` helper from `helpers.go`)
  - [ ] 7.7 Build `ReprotectInput` and dispatch `ReprotectHandler.Execute()` — NOT via WaveExecutor (re-protect is not wave-based)
  - [ ] 7.8 On success: set `DRExecution.Status.Result`, call `CompleteTransition` — from `Reprotecting` → `DRedSteadyState` when started from `FailedOver`, or from `ReprotectingBack` → `SteadyState` when started from `FailedBack`
  - [ ] 7.9 Emit events: `ReprotectStarted`, `ReprotectRoleSetupComplete`, `ReprotectHealthy` or `ReprotectTimeout`
  - [ ] 7.10 For failback dispatch (mode=`planned_migration`/`disaster` from `DRedSteadyState`): unified `FailoverHandler` from Story 4.1b runs the wave workflow; on completion `CompleteTransition("FailingBack")` lands the DRPlan in `FailedBack` (not `SteadyState`). Restore is a separate `reprotect` execution from `FailedBack`

- [ ] Task 8: Resume support for re-protect (AC: #8)
  - [ ] 8.1 Define re-protect execution phases in DRExecution status: use a condition `type=ReprotectPhase` with reasons: `RoleSetup`, `HealthMonitoring`, `Complete`
  - [ ] 8.2 In the reconciler's resume path (Story 4.7), add re-protect resume logic: if `exec.Spec.Mode == "reprotect"` AND `StartTime != nil` AND `Result == ""`, check the `ReprotectPhase` condition to determine resume point
  - [ ] 8.3 If `ReprotectPhase == RoleSetup`: re-execute role setup (idempotent — StopReplication and SetSource are safe to retry)
  - [ ] 8.4 If `ReprotectPhase == HealthMonitoring`: skip role setup, resume health monitoring loop

- [ ] Task 9: Update main.go wiring (AC: #10)
  - [ ] 9.1 Create `ReprotectHandler` with configurable poll interval and timeout, `Checkpointer` from existing wiring
  - [ ] 9.2 Pass `ReprotectHandler` to `DRExecutionReconciler`
  - [ ] 9.3 No VMManager needed for re-protect (storage-only)

- [ ] Task 10: Add re-protect Prometheus metrics (AC: #13)
  - [ ] 10.1 In `pkg/metrics/metrics.go`, add: `ReprotectDuration` (Histogram), `ReprotectVGSetupDuration` (Histogram), `ReprotectHealthPollsTotal` (Counter). All with `soteria_` prefix
  - [ ] 10.2 Register with `metrics.Registry` via `init()`
  - [ ] 10.3 In `ReprotectHandler`: observe setup duration, total duration, increment poll counter per iteration

- [ ] Task 11: Unit tests for ReprotectHandler (AC: #14a-g)
  - [ ] 11.1 Create `pkg/engine/reprotect_test.go`
  - [ ] 11.2 Test: `TestReprotect_FullSuccess` — StopReplication + SetSource for all VGs, health monitoring completes (all Healthy), result Succeeded
  - [ ] 11.3 Test: `TestReprotect_StopReplicationFails_Tolerated` — StopReplication returns error for one VG, handler proceeds, SetSource succeeds, result Succeeded
  - [ ] 11.4 Test: `TestReprotect_SetSourceFails_VGMarkedFailed` — SetSource returns error for one VG of three, two VGs proceed to monitoring, result depends on monitoring
  - [ ] 11.5 Test: `TestReprotect_AllSetSourceFail_ExecutionFails` — all SetSource calls fail, execution result Failed
  - [ ] 11.6 Test: `TestReprotect_HealthMonitoringTimeout` — health never reaches Healthy within timeout, result PartiallySucceeded
  - [ ] 11.7 Test: `TestReprotect_HealthMonitoringCompletes` — VGs transition from Syncing to Healthy over multiple polls
  - [ ] 11.8 Test: `TestReprotect_ResumeFromHealthMonitoring` — role setup already done (condition present), resumes health monitoring
  - [ ] 11.9 Test: `TestReprotect_CheckpointWrittenPerPoll` — checkpoint called after each health poll iteration
  - [ ] 11.10 Test: `TestReprotect_ContextCancelled` — context cancelled mid-monitoring, returns ctx.Err()
  - [ ] 11.11 Test: `TestReprotect_StepStatusRecorded` — all steps recorded with correct names, timestamps, statuses
  - [ ] 11.12 Test: `TestReprotect_EmptyVolumeGroups` — no VGs, handler succeeds trivially with Succeeded
  - [ ] 11.13 Test: `TestReprotect_ForceFlags` — verify StopReplication uses Force=true, SetSource uses Force=false

- [ ] Task 12: Unit tests for failback path (AC: #14h-i)
  - [ ] 12.1 In `pkg/engine/statemachine_test.go`, verify: `TestTransition_PlannedMigration_FromDRedSteadyState` — returns FailingBack
  - [ ] 12.2 Verify: `TestTransition_Disaster_FromDRedSteadyState` — returns FailingBack
  - [ ] 12.3 Verify: `TestCompleteTransition_FailingBack` — returns `FailedBack` (not `SteadyState`)
  - [ ] 12.4 Add restore coverage: reprotect from `FailedBack` → `ReprotectingBack` → `SteadyState` via `CompleteTransition("ReprotectingBack")`
  - [ ] 12.5 Verify in reconciler tests: failback dispatch uses unified `FailoverHandler` for `planned_migration` and `disaster` from `DRedSteadyState` (Story 4.1b); completion phase is `FailedBack`

- [ ] Task 13: Full lifecycle integration test (AC: #12)
  - [ ] 13.1 In executor or reconciler test file, add `TestFullDRLifecycle_EightPhases` — `SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState`. Verify DRExecution records exist for failover, reprotect, failback, and restore (reprotect from `FailedBack`)
  - [ ] 13.2 Test lifecycle can be repeated: after completing the cycle, verify another failover can be triggered from SteadyState

- [ ] Task 14: Update documentation and verify (AC: all)
  - [ ] 14.1 Update `pkg/engine/doc.go` to cover: re-protect and restore (storage-only, no waves), health monitoring loop, failback via unified `FailoverHandler` to `FailedBack`, then restore via `reprotect` to `SteadyState`
  - [ ] 14.2 Add godoc on `ReprotectHandler` explaining the distributed systems rationale: storage-only because VMs don't change site during these steps — only replication roles/direction are (re)established; same handler for re-protect and restore
  - [ ] 14.3 Add godoc on `monitorHealth` explaining the polling model and timeout behavior
  - [ ] 14.4 Run `make manifests` — regenerate RBAC (no CRD structural changes expected, but verify)
  - [ ] 14.5 Run `make generate` — verify deepcopy (types from 4.1b)
  - [ ] 14.6 Run `make test` — all unit tests pass (new + existing)
  - [ ] 14.7 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 14.8 Run `make build` — compiles cleanly

### Review Findings

- [x] [Review][Patch] Re-protect can be marked terminal on `context.Canceled` instead of remaining resumable — **Fixed**: added `ctx.Err()` guard before writing terminal result in `reconcileReprotect`
- [x] [Review][Patch] Mixed `SetSource` failures are still classified as full success — **Fixed**: `Result()` now returns `PartiallySucceeded` when `SetupFailed > 0`; added test case
- [x] [Review][Patch] Re-protect uses `DRPlan.Status.Waves` instead of the live execution discovery pipeline — **Mitigated**: added empty-waves guard with operator-visible `NoVolumeGroups` warning event + comment documenting the design decision; full re-discovery deferred (needs WaveExecutor refactor)
- [x] [Review][Patch] `DRPlan` replication progress is updated only in memory and never persisted during re-protect — **Fixed**: `planPreExec` base captured before `Execute()` so `MergeFrom` patch includes condition changes alongside phase advance
- [x] [Review][Patch] `ReprotectPhase` never records `RoleSetup` / `HealthMonitoring`, so resume just re-runs the full workflow — **Documented**: updated godoc to describe idempotent-replay model; trade-off is intentional (idempotent ops vs phase-checkpoint complexity)
- [x] [Review][Patch] Re-protect step history is never surfaced on the persisted API objects — **Documented**: CRD schema gap noted in `doc.go`; adding a top-level `Steps` field requires a schema extension story

## Dev Notes

### Architecture Context

This is Story 4.8 of Epic 4 (DR Workflow Engine — Full Lifecycle). It implements re-protect/restore (storage-only replication rebuild) and completes operator-facing documentation and tests for failback plus restore. Story 4.1b already delivered the 8-phase state machine (`FailedBack`, `ReprotectingBack`), `ExecutionModeReprotect` acceptance, and unified `FailoverHandler` for planned and disaster execution including failback. Prerequisites: Epic 4 stories through 4.7 and Story 4.1b.

**Story 4.8 scope:**
1. **Re-protect / restore** — NEW `ReprotectHandler` in `pkg/engine/reprotect.go`: storage-only (no waves, no VM operations). Used from `FailedOver` (reverse replication after failover) and from `FailedBack` (restore to `SteadyState` after failback). State transitions for reprotect are implemented in Story 4.1b; this story implements handler + controller wiring + metrics + tests.
2. **Failback** — no new handler: unified `FailoverHandler` from Story 4.1b (`failover.go`). Failback completes in `FailedBack`; operator runs restore (`reprotect` from `FailedBack`) to reach `SteadyState`.

**Key architectural distinction:** Storage-only replication rebuild vs wave-based workload move:
- **Re-protect and restore** use the same `ReprotectHandler` — replication direction/roles without moving workloads for those steps; NOT wave-based. Re-protect runs from `FailedOver`; restore runs from `FailedBack` after failback.
- **Failback** moves workloads back to the original site — wave-based, same unified `FailoverHandler` as failover (Story 4.1b). Completes in `FailedBack`; restore (`reprotect`) returns the plan to `SteadyState`.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — baseline Transition/CompleteTransition and execution wiring |
| **4.1b** | **Extended state machine + FailoverHandler** | **Prerequisite — `FailedBack`, `ReprotectingBack`, `ExecutionModeReprotect`, `FailedOver`/`FailedBack` + reprotect transitions, unified `FailoverHandler` (`failover.go`), failback lands `FailedBack`** |
| 4.2 | Wave executor framework + controller dispatch | Prerequisite — provides WaveExecutor, DRGroupHandler, fail-forward |
| 4.3 | Planned migration workflow + VMManager | Prerequisite — VMManager, step recording, resolveVolumeGroupID patterns (logic consolidated under FailoverHandler in 4.1b) |
| 4.4 | Disaster failover workflow | Prerequisite — force-promote / RPO patterns (consolidated under FailoverHandler in 4.1b) |
| 4.5 | Fail-forward error handling & partial success | Prerequisite — provides GroupError, StepRecorder, PVCResolver |
| 4.6 | Failed DRGroup retry | Prerequisite — provides retry mechanism |
| 4.7 | Checkpoint, resume & HA | Prerequisite — provides Checkpointer, ResumeAnalyzer, leader election |
| **4.8** | **Re-protect & failback workflows** | **This story — `ReprotectHandler`, restore path, verification; completes 8-phase DR lifecycle** |

### Critical Design Decision: Re-protect is Storage-Only (No Waves)

| Approach | Verdict | Reason |
|----------|---------|--------|
| Wave-based re-protect / restore | Rejected | These paths do not start/stop VMs — there is nothing to "wave" through. Waves exist to control VM startup ordering; re-protect/restore only change storage roles |
| **Storage-only re-protect and restore** | **Selected** | Operates on all volume groups in parallel. No VMManager dependency. Simpler execution model. Matches architecture document: "re-protect is a storage-only operation (no waves)" |
| Re-protect / restore as DRGroupHandler | Rejected | DRGroupHandler is designed for per-DRGroup execution with VMs. Reprotect paths operate on VGs directly, not grouped into DRGroups |

### Critical Design Decision: Failback Uses Unified FailoverHandler

| Approach | Verdict | Reason |
|----------|---------|--------|
| New FailbackHandler | Rejected | Failback is structurally identical to failover — same wave-based execution, same VM stop/start, same driver calls. Creating a new handler would duplicate code |
| **Unified FailoverHandler (Story 4.1b)** | **Selected** | `DRedSteadyState + planned_migration/disaster → FailingBack`, then `CompleteTransition("FailingBack") → FailedBack`. Same `FailoverHandler` as failover; direction comes from active site / plan context. Restore to `SteadyState` is a separate `reprotect` execution from `FailedBack` |

### Critical Design Decision: ExecutionModeReprotect vs Annotation

| Approach | Verdict | Reason |
|----------|---------|--------|
| **ExecutionModeReprotect (Story 4.1b)** | **Selected** | Clean API: `mode: reprotect` in DRExecution spec. State machine validates `FailedOver + reprotect → Reprotecting` and `FailedBack + reprotect → ReprotectingBack`. The mode field already disambiguates planned_migration vs disaster |
| Annotation-triggered re-protect | Rejected | Inconsistent with the execution model. DRExecution.Spec.Mode is the established pattern for execution type. Annotations are used for retry (Story 4.6) which is a different concept |

### Critical Design Decision: Re-protect Health Monitoring Model

| Approach | Verdict | Reason |
|----------|---------|--------|
| Blocking poll in handler goroutine | Rejected | Re-protect monitoring can take hours/days for large data sets. A blocking poll ties up a goroutine and can't survive pod restarts without checkpointing. Not Kubernetes-native |
| **Polling with checkpointing** | **Selected** | The handler runs in a goroutine (matching wave execution / `FailoverHandler` timing patterns), polls health at intervals, and writes checkpoints after each poll. On pod restart, the resume path (Story 4.7) restores the handler to its monitoring phase. Checkpoints include current health snapshot |
| RequeueAfter reconcile loop | Rejected | Would require restructuring the controller dispatch model away from the goroutine pattern established in Stories 4.2-4.7. Adds complexity without benefit given checkpointing exists |

### Critical Design Decision: Health Monitoring Timeout Behavior

| Approach | Verdict | Reason |
|----------|---------|--------|
| Failed on timeout | Rejected | Replication may still be syncing — marking as Failed implies the operation was unsuccessful. In reality, role setup succeeded and data is replicating |
| **PartiallySucceeded on timeout** | **Selected** | Role setup completed successfully (volumes are in correct roles, replication is active). Only the "wait for full sync" timed out. The operator can monitor replication health via DRPlan status conditions. DRPlan still completes the transition (`Reprotecting` → `DRedSteadyState` or `ReprotectingBack` → `SteadyState`) because the system has the intended roles (just not fully synced) |

### Critical Design Decision: Re-protect StopReplication Force Flag

The re-protect handler uses `StopReplication(force=true)` because:
- After a disaster failover, the old active site may be unreachable
- Even after recovery, the old site's volumes may be in an inconsistent state
- Force=true ensures the driver cleans up regardless of the peer's state
- This aligns with `pkg/drivers/types.go` comment: "Force tells the driver to stop replication even if there are outstanding writes or the peer is unreachable. Used during re-protect when the previously active site must transition to NonReplicated regardless of in-flight I/O."

The re-protect handler uses `SetSource(force=false)` because:
- At this point, the old active site's replication has been stopped (or was unreachable)
- No force needed — the local volumes are being promoted to Source normally
- If the remote site is still unreachable, the driver will establish replication once the remote recovers

### Existing Code to Reuse (Critical — Do NOT Reinvent)

| File | What It Provides | How This Story Uses It |
|------|-----------------|----------------------|
| `pkg/engine/statemachine.go` (Stories 4.1 / 4.1b) | `Transition`, `CompleteTransition`, `ValidStartingPhases`, `validTransitions` map | Reprotect and failback/restore transitions implemented in 4.1b — verify; no new map entries required for 4.8 |
| `pkg/engine/executor.go` (Stories 4.2-4.7) | `WaveExecutor`, `DRGroupHandler`, `ExecutionGroup`, `Checkpointer` | Failback uses executor via unified `FailoverHandler`. Re-protect reuses `Checkpointer` interface only |
| `pkg/engine/failover.go` (Story 4.1b) | `FailoverHandler`, step recording patterns, `resolveVolumeGroupID` usage | Failover and failback dispatch. Re-protect reuses `resolveVolumeGroupID` and step recording patterns aligned with `failover.go` |
| `pkg/engine/vm.go` (Story 4.3) | `VMManager`, `KubeVirtVMManager` | Failback reuses VMManager. Re-protect does NOT use VMManager |
| `pkg/engine/checkpoint.go` (Story 4.7) | `Checkpointer`, `KubeCheckpointer`, `NoOpCheckpointer` | Re-protect uses Checkpointer for role setup + health monitoring checkpoints |
| `pkg/engine/resume.go` (Story 4.7) | `ResumeAnalyzer`, `ResumePoint` | Extended for re-protect resume (different resume model than wave-based) |
| `pkg/engine/helpers.go` (Story 4.4) | `resolveVolumeGroupID`, shared step constants | Re-protect reuses VG ID resolution |
| `pkg/engine/pvc_resolver.go` (Story 4.5) | `PVCResolver` — resolves driver per VM/VG from PVC storage class | Re-protect uses PVCResolver to resolve driver per VG |
| `pkg/drivers/interface.go` | `StorageProvider` — `StopReplication`, `SetSource`, `GetReplicationStatus` | Core driver calls for re-protect |
| `pkg/drivers/types.go` | `StopReplicationOptions{Force}`, `SetSourceOptions{Force}`, `ReplicationStatus`, `ReplicationHealth` | All types used by re-protect handler |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing re-protect without real storage |
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRExecution`, `ExecutionMode`, `ExecutionResult`, `StepStatus`, all phase constants | `ExecutionModeReprotect` and `FailedBack`/`ReprotectingBack` phases from 4.1b — verify; all status types reused for re-protect step recording |
| `pkg/controller/drexecution/reconciler.go` (Stories 4.1-4.7, 4.1b) | DRExecution reconciler with new-execution, resume, and retry paths | Add re-protect dispatch for `FailedOver` and `FailedBack`; wire `CompleteTransition` for both `Reprotecting` and `ReprotectingBack` outcomes |
| `pkg/admission/drexecution_validator.go` (Stories 4.1 / 4.1b) | DRExecution admission webhook | `reprotect` acceptance from 4.1b — verify only |
| `pkg/registry/drexecution/strategy.go` | Strategy validation for DRExecution | `reprotect` acceptance from 4.1b — verify only |
| `pkg/engine/discovery.go` | `VMDiscoverer`, `GroupByWave` | Re-protect reuses VM discovery to find all VGs in the plan |
| `pkg/engine/consistency.go` | `ResolveVolumeGroups` | Re-protect reuses consistency resolution to get VG list |
| `cmd/soteria/main.go` (Stories 4.1-4.7) | Manager wiring with all existing components | Wire ReprotectHandler |
| `pkg/metrics/metrics.go` (Story 4.7) | Prometheus metrics registration pattern | Add re-protect metrics following same pattern |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Includes `ExecutionModeReprotect`, `FailedBack`, `ReprotectingBack` (4.1b) | Verify only unless gaps found |
| `pkg/engine/statemachine.go` (Stories 4.1 / 4.1b) | Full 8-phase transitions including reprotect from `FailedOver` and `FailedBack` | Verify only |
| `pkg/registry/drexecution/strategy.go` | Accepts `reprotect` (4.1b) | Verify only |
| `pkg/admission/drexecution_validator.go` | Accepts `reprotect`, calls `engine.Transition` (4.1b) | Verify only |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Accepts `reprotect` if validated here (4.1b) | Verify only |
| `pkg/controller/drexecution/reconciler.go` (Stories 4.1-4.7, 4.1b) | Dispatches unified `FailoverHandler` for planned/disaster | Add re-protect dispatch: when mode is `reprotect`, build `ReprotectInput` and call `ReprotectHandler.Execute` instead of `WaveExecutor`; handle both `Reprotecting` and `ReprotectingBack` completion |
| `cmd/soteria/main.go` (Stories 4.1-4.7) | Wires VMManager, WaveExecutor, handlers | Create ReprotectHandler, pass to DRExecutionReconciler |
| `pkg/metrics/metrics.go` (Story 4.7) | Checkpoint metrics | Add re-protect metrics |
| `pkg/engine/doc.go` | Covers discovery through checkpoint/resume | Add re-protect workflow and failback documentation |

### New Files to Create

| File | Purpose |
|------|---------|
| `pkg/engine/reprotect.go` | `ReprotectHandler`, `ReprotectInput`, `ReprotectResult`, `monitorHealth`, `ErrReprotectHealthTimeout` |
| `pkg/engine/reprotect_test.go` | Comprehensive re-protect unit tests |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/engine/failover.go` | Unified FailoverHandler — unchanged by 4.8; failback and failover entry point |
| `pkg/engine/failover_test.go` | Tests — no changes unless extending failback/`FailedBack` coverage |
| `pkg/engine/executor.go` | Wave executor — re-protect does not use it; failback uses it unchanged |
| `pkg/engine/checkpoint.go` | Checkpointer — reused by re-protect, not modified |
| `pkg/engine/resume.go` | ResumeAnalyzer — may need extension for re-protect resume, but prefer adding re-protect-specific resume logic in the reconciler rather than modifying the analyzer |
| `pkg/engine/vm.go` | VMManager — re-protect does not use it; failback uses it unchanged |
| `pkg/engine/chunker.go` | Chunker — re-protect does not use it; failback uses it unchanged |
| `pkg/engine/discovery.go` | VM discovery — reuse for VG enumeration, don't modify |
| `pkg/engine/consistency.go` | Consistency — reuse for VG resolution, don't modify |
| `pkg/drivers/*` | All driver code — no changes |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes |

### Key Implementation Patterns

**1. Re-protect handler (storage-only, no waves):**

```go
type ReprotectHandler struct {
    Checkpointer Checkpointer
    HealthPollInterval time.Duration
    HealthTimeout      time.Duration
}

type ReprotectInput struct {
    Execution    *v1alpha1.DRExecution
    Plan         *v1alpha1.DRPlan
    VolumeGroups []VolumeGroupEntry
}

type VolumeGroupEntry struct {
    Info   v1alpha1.VolumeGroupInfo
    Driver drivers.StorageProvider
    VGID   drivers.VolumeGroupID
}

func (h *ReprotectHandler) Execute(ctx context.Context, input ReprotectInput) (*ReprotectResult, error) {
    logger := log.FromContext(ctx)

    // Phase 1: Role setup — StopReplication + SetSource per VG
    var successfulVGs []VolumeGroupEntry
    for _, vg := range input.VolumeGroups {
        if err := vg.Driver.StopReplication(ctx, vg.VGID, drivers.StopReplicationOptions{Force: true}); err != nil {
            logger.V(1).Info("StopReplication failed for volume group, proceeding", "vg", vg.Info.Name, "error", err)
        }
        if err := vg.Driver.SetSource(ctx, vg.VGID, drivers.SetSourceOptions{Force: false}); err != nil {
            // Record failure step, continue with other VGs
            continue
        }
        successfulVGs = append(successfulVGs, vg)
    }

    if len(successfulVGs) == 0 {
        return nil, fmt.Errorf("all volume groups failed SetSource during re-protect")
    }

    // Phase 2: Health monitoring
    return h.monitorHealth(ctx, input, successfulVGs)
}
```

**2. Health monitoring loop:**

```go
func (h *ReprotectHandler) monitorHealth(ctx context.Context, input ReprotectInput, vgs []VolumeGroupEntry) (*ReprotectResult, error) {
    logger := log.FromContext(ctx)
    ticker := time.NewTicker(h.HealthPollInterval)
    defer ticker.Stop()
    timeout := time.After(h.HealthTimeout)

    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-timeout:
            return &ReprotectResult{TimedOut: true, ...}, ErrReprotectHealthTimeout
        case <-ticker.C:
            healthy := 0
            for _, vg := range vgs {
                status, err := vg.Driver.GetReplicationStatus(ctx, vg.VGID)
                if err != nil {
                    logger.V(1).Info("Could not check replication health", "vg", vg.Info.Name, "error", err)
                    continue
                }
                if status.Health == drivers.HealthHealthy {
                    healthy++
                }
            }
            logger.V(1).Info("Replication health check", "healthy", healthy, "total", len(vgs))
            // Checkpoint progress
            if healthy == len(vgs) {
                return &ReprotectResult{HealthyVGs: healthy, TotalVGs: len(vgs)}, nil
            }
        }
    }
}
```

**3. Controller dispatch for re-protect:**

```go
case v1alpha1.ExecutionModeReprotect:
    logger.Info("Dispatching re-protect execution", "plan", plan.Name)
    r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "ReprotectStarted", "Dispatch",
        "Re-protect started for plan %s", plan.Name)

    // Discover VGs from plan (reuse existing discovery pipeline)
    vgs, err := r.discoverVolumeGroups(ctx, &plan)
    if err != nil {
        return ctrl.Result{}, fmt.Errorf("discovering volume groups for re-protect: %w", err)
    }

    input := engine.ReprotectInput{
        Execution:    &exec,
        Plan:         &plan,
        VolumeGroups: vgs,
    }

    go func() {
        result, err := r.ReprotectHandler.Execute(ctx, input)
        // Handle result, CompleteTransition, update status
    }()
```

**4. Structured logging (project convention):**

```go
logger.Info("Re-protect started", "plan", plan.Name, "volumeGroups", len(input.VolumeGroups))
logger.Info("StopReplication completed", "vg", vg.Info.Name)
logger.V(1).Info("StopReplication failed for volume group, proceeding", "vg", vg.Info.Name, "error", err)
logger.Info("SetSource completed", "vg", vg.Info.Name)
logger.V(1).Info("Replication health check", "healthy", healthy, "total", total)
logger.Info("Re-protect health monitoring complete", "plan", plan.Name, "healthy", healthy, "total", total)
logger.Info("Re-protect health monitoring timed out", "plan", plan.Name, "healthy", healthy, "total", total, "timeout", timeout)
```

**5. Error wrapping (project convention):**

```go
return fmt.Errorf("setting source for volume group %s during re-protect: %w", vg.Info.Name, err)
return fmt.Errorf("monitoring replication health during re-protect: %w", err)
return fmt.Errorf("discovering volume groups for re-protect: %w", err)
```

**6. Event emission:**

```go
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "ReprotectStarted", "Dispatch",
    "Re-protect started for plan %s with %d volume groups", plan.Name, len(vgs))
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "ReprotectRoleSetupComplete", "RoleSetup",
    "Re-protect role setup complete: %d/%d volume groups succeeded", succeeded, total)
r.Recorder.Eventf(&exec, nil, corev1.EventTypeNormal, "ReprotectHealthy", "HealthMonitoring",
    "All %d volume groups report healthy replication", total)
r.Recorder.Eventf(&exec, nil, corev1.EventTypeWarning, "ReprotectTimeout", "HealthMonitoring",
    "Re-protect health monitoring timed out: %d/%d volume groups healthy after %v", healthy, total, timeout)
```

**7. Context propagation (architecture mandate):**

All methods in `reprotect.go` MUST accept `ctx context.Context` as the first parameter. The health monitoring loop MUST respect `ctx.Done()` for cancellation on leader election loss. Never create `context.Background()`.

### Re-protect / Restore Flow Diagram

Same `ReprotectHandler` for both branches; starting phase determines transition names.

**Branch A — Re-protect after failover**

```
DRPlan: FailedOver
        │
        ▼
Operator creates DRExecution{mode: "reprotect"}
        │
        ▼
Admission / strategy: Transition validates FailedOver + reprotect → allowed
        │
        ▼
Controller: Transition(FailedOver, reprotect) → Reprotecting
        │
        ▼
ReprotectHandler — Phase 1 Role Setup (per VG): StopReplication(force=true) → SetSource(force=false)
        │
        ▼ checkpoint
        │
Phase 2 — Health Monitoring (poll interval, timeout per AC5/AC6)
        │
        ▼
Controller: CompleteTransition(Reprotecting) → DRedSteadyState
```

**Branch B — Restore after failback**

```
DRPlan: FailedBack
        │
        ▼
Operator creates DRExecution{mode: "reprotect"}
        │
        ▼
Controller: Transition(FailedBack, reprotect) → ReprotectingBack
        │
        ▼
ReprotectHandler — same storage-only steps as Branch A
        │
        ▼
Controller: CompleteTransition(ReprotectingBack) → SteadyState
```

### Failback + Restore Flow (FailoverHandler + ReprotectHandler)

```
DRPlan: DRedSteadyState
        │
        ▼
Operator creates DRExecution{mode: "planned_migration" | "disaster"}
        │
        ▼
Controller: Transition(DRedSteadyState, mode) → FailingBack
        │
        ▼
FailoverHandler (Story 4.1b) — wave-based execution (same as failover)
        │
        ▼
Controller: CompleteTransition(FailingBack) → FailedBack
        │
        ▼
DRPlan: FailedBack  (operator must run restore — Branch B above)
```

### Full DR Lifecycle State Machine (8 phases)

```
SteadyState ──[failover start]──► FailingOver ──[complete]──► FailedOver
      ▲                                                              │
      │                                                    [reprotect]
      │                                                              ▼
      │                                                        Reprotecting
      │                                                              │
      │                                                   [complete] │
      │                                                              ▼
      │                                                      DRedSteadyState
      │                                                              │
      │                                                    [failback start]
      │                                                              ▼
      │                                                        FailingBack
      │                                                              │
      │                                                   [complete] │
      │                                                              ▼
      │                                                         FailedBack
      │                                                              │
      │                                                    [reprotect restore]
      │                                                              ▼
      │                                                    ReprotectingBack
      │                                                              │
      └──────────────────────────────────────────────[complete]──────┘
```

### RBAC Requirements

No new RBAC markers needed beyond what Stories 4.1-4.7 and 4.1b established. The DRExecution controller already has permissions for:
- `soteria.io/drexecutions` — get, list, watch, update, patch
- `soteria.io/drexecutions/status` — get, update, patch
- `soteria.io/drplans` — get, list, watch (for fetching plan)
- `soteria.io/drplans/status` — update, patch (for phase transitions)
- `soteria.io/drgroupstatuses` — full CRUD (for wave-based execution)
- `events.k8s.io/events` — create, patch (for event emission)

Re-protect uses the same permissions: reads DRPlan, updates DRExecution status, updates DRPlan status phase. No additional RBAC needed.

### Test Strategy

**ReprotectHandler tests** (`pkg/engine/reprotect_test.go`): Use fake driver (`pkg/drivers/fake/`) and `NoOpCheckpointer` from Story 4.7. Configure fake driver to return specific responses per VG. Test both role setup and health monitoring phases independently and together.

**State machine tests** (`pkg/engine/statemachine_test.go`): Verify `FailedOver + reprotect`, `FailedBack + reprotect`, failback entry, and `CompleteTransition` outcomes including `FailingBack → FailedBack` (Story 4.1b).

**Strategy/admission tests**: `reprotect` acceptance is Story 4.1b — regression-verify only.

**Reconciler tests**: Re-protect dispatch from `FailedOver` and from `FailedBack`; failback from `DRedSteadyState` uses unified `FailoverHandler`; completion at `FailedBack` until restore.

**Full lifecycle test**: End-to-end test through all 8 phases (rest + transition). Uses mock drivers and VMManagers where wave paths run.

### Previous Story Intelligence

**From Story 4.7 (Checkpoint, Resume & High Availability):**
- `Checkpointer` interface and `KubeCheckpointer` — re-protect reuses for health monitoring checkpoints
- `ResumeAnalyzer` — re-protect adds a different resume model (phase-based, not wave-based): check condition `ReprotectPhase` to determine if role setup is done
- Resume ordering in reconciler: `StartTime != nil && Result == ""` identifies in-progress executions. Re-protect resume needs to check mode to distinguish from wave-based resume

**From Story 4.5 (Fail-Forward Error Handling & Partial Success):**
- `PVCResolver` — re-protect reuses for driver resolution per VG
- Step recording via `StepStatus` — re-protect follows the same pattern
- `GroupError` — may be useful for tracking per-VG failures during role setup

**From Story 4.3 (Planned Migration Workflow):**
- `resolveVolumeGroupID` helper — re-protect reuses for obtaining VolumeGroupIDs
- PreExecute / sync polling patterns now live under unified `FailoverHandler` (`failover.go`, Story 4.1b) — re-protect's health monitoring is similar (poll interval, timeout, context cancellation)

**From Story 4.4 (Disaster Failover Workflow):**
- Force-promote and RPO patterns — consolidated in `FailoverHandler` for disaster paths; re-protect does not need RPO

**From Story 4.1b (Extended state machine + FailoverHandler):**
- Eight-phase lifecycle including `FailedBack`, `ReprotectingBack`, and reprotect from both `FailedOver` and `FailedBack`
- `CompleteTransition("FailingBack") → FailedBack`; restore completes via `ReprotectingBack → SteadyState`
- Unified `FailoverHandler` for planned and disaster execution (failover and failback)

**From Story 4.1 (DR State Machine & Execution Controller):**
- Baseline execution controller wiring and admission patterns
- Controller reconcile path ordering — add re-protect dispatch branch alongside `FailoverHandler` dispatch

**From Epic 3 (Storage Driver Framework):**
- All driver methods are idempotent — `StopReplication` and `SetSource` are safe to retry during re-protect resume
- `SetTargetOptions{Force: true}` comment explicitly mentions re-protect scenario
- `StopReplicationOptions{Force: true}` comment explicitly mentions re-protect scenario
- Fake driver enables full testing of re-protect without real storage

### Build Commands

```bash
make manifests    # Regenerate RBAC (verify no unexpected changes)
make generate     # Verify deepcopy (ExecutionModeReprotect from 4.1b)
make test         # All unit tests (new + existing)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make build        # Verify compilation
```

### Project Structure Notes

All files align with the architecture document:
- `pkg/engine/reprotect.go` — new: ReprotectHandler, ReprotectInput, ReprotectResult, health monitoring
- `pkg/engine/reprotect_test.go` — new: comprehensive re-protect tests
- `pkg/apis/soteria.io/v1alpha1/types.go` — Story 4.1b: phases + `ExecutionModeReprotect` — verify only
- `pkg/engine/statemachine.go` — Story 4.1b: 8-phase transitions — verify only
- `pkg/registry/drexecution/strategy.go` — Story 4.1b: reprotect mode — verify only
- `pkg/admission/drexecution_validator.go` — Story 4.1b: reprotect mode — verify only
- `pkg/controller/drexecution/reconciler.go` — modified: add re-protect/restore dispatch path (`FailedOver` / `FailedBack`) and completion for `Reprotecting` / `ReprotectingBack`
- `cmd/soteria/main.go` — modified: wire ReprotectHandler
- `pkg/metrics/metrics.go` — modified: add re-protect metrics
- `pkg/engine/doc.go` — modified: add re-protect and failback documentation

The engine boundary is maintained: `pkg/engine/` handles re-protect workflow logic. The handler calls drivers for storage operations and uses the Kubernetes API client (via Checkpointer) for status persistence. It does NOT know about ScyllaDB, CDC, or aggregated API server internals.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.8] — Story acceptance criteria (BDD format): re-protect/restore, failback from DRedSteadyState to FailedBack, origin unreachable tolerance, health monitoring, full 8-phase lifecycle
- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.1] — Baseline state machine and execution controller
- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.1b] — Extended phases (`FailedBack`, `ReprotectingBack`), reprotect from `FailedOver`/`FailedBack`, unified FailoverHandler, failback completion at `FailedBack`
- [Source: _bmad-output/planning-artifacts/epics.md#FR16] — Re-protect: StopReplication on old active, transition roles, monitor until healthy
- [Source: _bmad-output/planning-artifacts/epics.md#FR17] — Failback: reverse of failover using same wave-based engine
- [Source: _bmad-output/planning-artifacts/prd.md#DR Execution & Workflow] — FR16 re-protect, FR17 failback, FR18 human-triggered
- [Source: _bmad-output/planning-artifacts/prd.md#MVP Feature Set] — Re-protect and failback are v1 must-haves
- [Source: _bmad-output/planning-artifacts/architecture.md#CRD Status Patterns] — DRPlan phases: include Reprotecting, DRedSteadyState, FailingBack, FailedBack, ReprotectingBack (8-phase lifecycle)
- [Source: _bmad-output/planning-artifacts/architecture.md#API & Communication Patterns] — Checkpointing per-DRGroup, error model fail-forward
- [Source: _bmad-output/planning-artifacts/architecture.md#Gap Analysis] — "Add pkg/engine/reprotect.go — re-protect is a storage-only operation (no waves)"
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — All 7 methods idempotent, accept context.Context
- [Source: _bmad-output/project-context.md#Workflow Engine] — "Re-protect workflow: StopReplication on old active → SetTarget on old active / SetSource on new active → monitor until healthy (storage-only, no waves)"
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — 7-method interface, role model, Force flags for re-protect
- [Source: pkg/drivers/interface.go] — StorageProvider: SetSource, SetTarget, StopReplication, GetReplicationStatus
- [Source: pkg/drivers/types.go] — SetSourceOptions{Force}, SetTargetOptions{Force}, StopReplicationOptions{Force}, ReplicationHealth, ReplicationStatus
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — ExecutionMode, DRPlan phases, DRExecution spec/status, StepStatus
- [Source: _bmad-output/implementation-artifacts/4-7-checkpoint-resume-high-availability.md] — Previous story: Checkpointer, ResumeAnalyzer, checkpoint granularity
- [Source: _bmad-output/implementation-artifacts/4-3-planned-migration-workflow.md] — Historical planned migration story; patterns consolidated in `failover.go` (4.1b)
- [Source: _bmad-output/implementation-artifacts/4-4-disaster-failover-workflow.md] — Historical disaster story; patterns consolidated in `failover.go` (4.1b)
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — Baseline state machine: Transition, CompleteTransition, admission webhook
- [Source: _bmad-output/implementation-artifacts/4-1b-state-machine-symmetry-unified-failover-handler.md] — Story 4.1b: 8-phase model, unified FailoverHandler

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
