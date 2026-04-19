# Story 4.8: Re-protect & Failback Workflows

Status: ready-for-dev

## Story

As an operator,
I want to re-establish replication after failover via re-protect and eventually fail back to the original site,
So that the system returns to full DR protection.

## Acceptance Criteria

1. **AC1 — New execution mode for re-protect:** `ExecutionModeReprotect ExecutionMode = "reprotect"` is added to `pkg/apis/soteria.io/v1alpha1/types.go`. The registry strategy in `pkg/registry/drexecution/strategy.go` and the admission webhook in `pkg/admission/drexecution_validator.go` accept `reprotect` as a valid mode. Failback does NOT require a new mode — it reuses `planned_migration` or `disaster` from `DRedSteadyState` (the state machine already maps `DRedSteadyState + planned_migration → FailingBack` and `DRedSteadyState + disaster → FailingBack` per Story 4.1).

2. **AC2 — State machine extension for re-protect:** The state machine in `pkg/engine/statemachine.go` adds a new valid transition: `FailedOver + reprotect → Reprotecting`. The `ValidStartingPhases("reprotect")` function returns `["FailedOver"]`. The `CompleteTransition("Reprotecting")` already returns `DRedSteadyState` (Story 4.1). No state machine changes are needed for failback — `Transition("DRedSteadyState", "planned_migration") → "FailingBack"` and `Transition("DRedSteadyState", "disaster") → "FailingBack"` are already defined.

3. **AC3 — Re-protect handler:** `pkg/engine/reprotect.go` implements a `ReprotectHandler` that executes the re-protect workflow: a storage-only operation (no waves, no VM start/stop). For each volume group in the plan: (a) call `StopReplication(force=true)` — tolerate failure if old active site is unreachable (FR16); (b) call `SetSource(force=false)` to establish the current site's volumes as replication source. The storage backend configures the remote site as Target implicitly via the replication pair.

4. **AC4 — Origin unreachable tolerance during re-protect:** When `StopReplication` fails for a volume group (e.g., old active site unreachable), the handler logs the error at V(1) and proceeds with `SetSource` for that volume group. The re-protect continues for all VGs — a `StopReplication` failure does not fail the execution.

5. **AC5 — Replication health monitoring:** After all VGs have been set to Source, the handler enters a polling loop calling `GetReplicationStatus` for each VG at a configurable interval (default 30s). The loop continues until all VGs report `HealthHealthy` or `HealthSyncing` that progresses to `HealthHealthy`, or until the configurable timeout is reached (default 24h). DRExecution status conditions are updated with resync progress: `type=Replicating, status=True, reason=SyncInProgress, message="4/6 volume groups healthy"`. DRPlan status conditions are also updated with `type=Replicating, status=True` to reflect ongoing resync.

6. **AC6 — Re-protect health monitoring timeout:** If the health monitoring timeout expires before all VGs report Healthy, the execution is marked `PartiallySucceeded` (not Failed) — replication may still be syncing and will eventually catch up. The DRPlan phase still transitions to `DRedSteadyState` because the role setup succeeded and replication is in progress. The operator can monitor replication health via the DRPlan status conditions.

7. **AC7 — Re-protect step recording:** Each operation is recorded as a `StepStatus` entry in the DRExecution status. Step names: `"StopReplication"` (per VG), `"SetSource"` (per VG), `"HealthMonitoring"` (one summary step with final health snapshot). This provides visibility into exactly which VG had issues during re-protect.

8. **AC8 — Re-protect checkpoint integration:** Re-protect uses the `Checkpointer` from Story 4.7 to persist progress. After each VG's role setup completes (StopReplication + SetSource), a checkpoint is written. After each health poll iteration, a checkpoint is written with the current health snapshot. On pod restart, the handler resumes from the checkpoint: if role setup is done, it skips to health monitoring.

9. **AC9 — Failback reuses existing handlers:** Failback is triggered by creating a DRExecution with `mode=planned_migration` or `mode=disaster` when the DRPlan is in `DRedSteadyState`. The state machine transitions `DRedSteadyState → FailingBack`. The same `PlannedMigrationHandler` (for graceful failback) or `DisasterFailoverHandler` (for forced failback) is used — no new handler code. On completion, `CompleteTransition("FailingBack")` returns `SteadyState`.

10. **AC10 — Controller integration:** The DRExecution reconciler dispatches to `ReprotectHandler` when `exec.Spec.Mode == "reprotect"`. For re-protect mode: (a) transition DRPlan phase from `FailedOver` → `Reprotecting`; (b) discover all volume groups from the plan (same discovery as failover); (c) resolve drivers per VG via `PVCResolver`; (d) dispatch `ReprotectHandler.Execute()` — not via `WaveExecutor` (re-protect is not wave-based); (e) on completion, call `CompleteTransition` to advance DRPlan from `Reprotecting` → `DRedSteadyState`. For failback (mode=`planned_migration`/`disaster` from `DRedSteadyState`): the existing handler dispatch from Stories 4.3/4.4 applies unchanged.

11. **AC11 — Re-protect event emission:** The controller emits events: `ReprotectStarted` when re-protect begins, `ReprotectRoleSetupComplete` when all VGs have been set to Source, `ReprotectHealthy` when all VGs report Healthy, `ReprotectTimeout` if health monitoring times out. Failback uses the existing `FailoverStarted`/`FailoverCompleted` events (same engine).

12. **AC12 — Full DR lifecycle validation:** Unit tests validate the complete 4-state cycle: `SteadyState → (failover) → FailedOver → (reprotect) → DRedSteadyState → (failback) → SteadyState`. Each transition produces a DRExecution record. The cycle can be repeated.

13. **AC13 — Re-protect Prometheus metrics:** Metrics are emitted: `soteria_reprotect_duration_seconds` (histogram — total re-protect time including health monitoring), `soteria_reprotect_vg_setup_duration_seconds` (histogram — role setup phase only), `soteria_reprotect_health_polls_total` (counter — number of health poll iterations). These use the `soteria_` prefix per project convention.

14. **AC14 — Unit tests:** Comprehensive tests covering: (a) successful re-protect — StopReplication + SetSource for all VGs, health monitoring completes; (b) StopReplication failure tolerated — handler proceeds, logs warning; (c) SetSource failure — VG marked failed, execution fails; (d) health monitoring timeout — execution marked PartiallySucceeded; (e) health monitoring completes — all VGs Healthy; (f) resume after restart — role setup already done, resumes monitoring; (g) checkpoint writes during re-protect; (h) failback via planned_migration from DRedSteadyState — standard wave execution; (i) failback via disaster from DRedSteadyState — force-promote wave execution; (j) state machine: FailedOver + reprotect → Reprotecting; (k) state machine: DRedSteadyState + planned_migration → FailingBack; (l) full lifecycle: SteadyState → FailedOver → DRedSteadyState → SteadyState; (m) admission webhook accepts reprotect mode; (n) strategy validation accepts reprotect mode.

## Tasks / Subtasks

- [ ] Task 1: Add ExecutionModeReprotect constant (AC: #1)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go`, add `ExecutionModeReprotect ExecutionMode = "reprotect"` after the existing `ExecutionModeDisaster` constant
  - [ ] 1.2 Run `make generate` to regenerate deepcopy (the new constant itself doesn't trigger deepcopy changes, but verify)
  - [ ] 1.3 Run `make manifests` — no CRD structural changes (ExecutionMode is a string field, not an enum in CRD)

- [ ] Task 2: Update validation to accept reprotect mode (AC: #1)
  - [ ] 2.1 In `pkg/registry/drexecution/strategy.go`, update `Validate` to accept `ExecutionModeReprotect` alongside `planned_migration` and `disaster`
  - [ ] 2.2 In `pkg/apis/soteria.io/v1alpha1/validation.go`, update `ValidateDRExecution` (if it validates mode) to accept `reprotect`
  - [ ] 2.3 In `pkg/admission/drexecution_validator.go`, update the mode validation to accept `reprotect` and call `engine.Transition(plan.Status.Phase, exec.Spec.Mode)` (the state machine handles phase validation)
  - [ ] 2.4 Add unit tests for reprotect mode validation in `strategy_test.go` and `drexecution_validator_test.go`

- [ ] Task 3: Extend state machine for re-protect transition (AC: #2)
  - [ ] 3.1 In `pkg/engine/statemachine.go`, add `FailedOver + reprotect → Reprotecting` to `validTransitions` map
  - [ ] 3.2 Update `ValidStartingPhases` to return `["FailedOver"]` for `reprotect` mode
  - [ ] 3.3 Verify `CompleteTransition("Reprotecting") → "DRedSteadyState"` already exists (Story 4.1)
  - [ ] 3.4 Add unit tests: `TestTransition_Reprotect_FromFailedOver`, `TestTransition_Reprotect_FromInvalidPhase`, `TestValidStartingPhases_Reprotect`

- [ ] Task 4: Implement ReprotectHandler (AC: #3, #4, #7)
  - [ ] 4.1 Create `pkg/engine/reprotect.go` with copyright header and Tier 2 architecture block comment explaining the re-protect workflow: storage-only (no waves/VMs), StopReplication + SetSource + health monitoring
  - [ ] 4.2 Define `ReprotectHandler` struct with fields: `DriverResolver` (function that returns `StorageProvider` per VG), `Checkpointer engine.Checkpointer`, `HealthPollInterval time.Duration` (default 30s), `HealthTimeout time.Duration` (default 24h)
  - [ ] 4.3 Define `ReprotectInput` struct: `Execution *v1alpha1.DRExecution`, `Plan *v1alpha1.DRPlan`, `VolumeGroups []VolumeGroupEntry` where `VolumeGroupEntry` bundles a `VolumeGroupInfo` (from plan discovery) with its resolved `drivers.StorageProvider` and `drivers.VolumeGroupID`
  - [ ] 4.4 Implement `Execute(ctx context.Context, input ReprotectInput) (*ReprotectResult, error)` — the main entry point
  - [ ] 4.5 Phase 1 — Role setup: iterate all VGs. For each: (a) call `driver.StopReplication(ctx, vgID, StopReplicationOptions{Force: true})` — on error, log at V(1) `"StopReplication failed for volume group, proceeding"` and continue; (b) call `driver.SetSource(ctx, vgID, SetSourceOptions{Force: false})` — on error, mark VG as failed and record StepStatus with error. If ALL SetSource calls fail, return error (execution fails). If some fail, proceed to health monitoring for the successful ones
  - [ ] 4.6 Phase 2 — Record step status for each VG operation using existing step recording patterns from planned.go/disaster.go
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
  - [ ] 7.3 Validate plan is in `FailedOver` phase (state machine handles this via Transition call)
  - [ ] 7.4 Discover all volume groups from the plan using the existing discovery + consistency pipeline (reuse from failover path)
  - [ ] 7.5 Resolve drivers per VG using `PVCResolver` (from Story 4.5)
  - [ ] 7.6 Resolve VolumeGroupIDs via `CreateVolumeGroup` (idempotent, reuse `resolveVolumeGroupID` helper from `helpers.go`)
  - [ ] 7.7 Build `ReprotectInput` and dispatch `ReprotectHandler.Execute()` — NOT via WaveExecutor (re-protect is not wave-based)
  - [ ] 7.8 On success: set `DRExecution.Status.Result`, call `CompleteTransition` to advance DRPlan from `Reprotecting` → `DRedSteadyState`
  - [ ] 7.9 Emit events: `ReprotectStarted`, `ReprotectRoleSetupComplete`, `ReprotectHealthy` or `ReprotectTimeout`
  - [ ] 7.10 For failback dispatch (mode=`planned_migration`/`disaster` from `DRedSteadyState`): no changes needed — the existing state machine returns `FailingBack`, and the handler dispatch (PlannedMigrationHandler/DisasterFailoverHandler) works unchanged

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
  - [ ] 12.1 In `pkg/engine/statemachine_test.go`, add: `TestTransition_PlannedMigration_FromDRedSteadyState` — returns FailingBack
  - [ ] 12.2 Add: `TestTransition_Disaster_FromDRedSteadyState` — returns FailingBack
  - [ ] 12.3 Add: `TestCompleteTransition_FailingBack` — returns SteadyState
  - [ ] 12.4 Verify in reconciler tests: failback dispatch uses PlannedMigrationHandler for planned_migration mode, DisasterFailoverHandler for disaster mode — same as failover (no new handler code)

- [ ] Task 13: Full lifecycle integration test (AC: #12)
  - [ ] 13.1 In executor or reconciler test file, add `TestFullDRLifecycle_SteadyStateToSteadyState` — create plan in SteadyState, execute failover (SteadyState → FailingOver → FailedOver), execute re-protect (FailedOver → Reprotecting → DRedSteadyState), execute failback (DRedSteadyState → FailingBack → SteadyState). Verify DRExecution records exist for each operation
  - [ ] 13.2 Test lifecycle can be repeated: after completing the cycle, verify another failover can be triggered from SteadyState

- [ ] Task 14: Update documentation and verify (AC: all)
  - [ ] 14.1 Update `pkg/engine/doc.go` to cover: re-protect workflow (storage-only, no waves), health monitoring loop, failback as reverse failover
  - [ ] 14.2 Add godoc on `ReprotectHandler` explaining the distributed systems rationale: re-protect is storage-only because VMs don't change site — only replication direction reverses
  - [ ] 14.3 Add godoc on `monitorHealth` explaining the polling model and timeout behavior
  - [ ] 14.4 Run `make manifests` — regenerate RBAC (no CRD structural changes expected, but verify)
  - [ ] 14.5 Run `make generate` — regenerate deepcopy
  - [ ] 14.6 Run `make test` — all unit tests pass (new + existing)
  - [ ] 14.7 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 14.8 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4.8 of Epic 4 (DR Workflow Engine — Full Lifecycle). It implements the final two workflows in the 4-state DR lifecycle: re-protect (establishing reverse replication after failover) and failback (returning to the original site). All preceding Epic 4 stories (4.05 through 4.7) are prerequisites — they build the state machine, executor, handlers, checkpointing, and retry infrastructure.

**Story 4.8 scope:** Two workflows:
1. **Re-protect** — a NEW handler (`ReprotectHandler`) that is storage-only (no waves, no VM operations). Adds `ExecutionModeReprotect` constant and extends the state machine with `FailedOver + reprotect → Reprotecting`.
2. **Failback** — reuses the EXISTING `PlannedMigrationHandler`/`DisasterFailoverHandler` with no new code. The state machine already handles `DRedSteadyState + planned_migration → FailingBack` and `DRedSteadyState + disaster → FailingBack`.

**Key architectural distinction:** Re-protect and failback are fundamentally different operations:
- **Re-protect** reverses the replication direction without moving workloads — VMs stay where they are, only storage roles change. It is NOT wave-based.
- **Failback** moves workloads back to the original site — it IS wave-based and identical in structure to failover (same handlers, reversed direction).

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite |
| 4.1 | State machine + execution controller + admission webhook | Prerequisite — provides Transition, CompleteTransition, state machine with FailingBack/Reprotecting phases |
| 4.2 | Wave executor framework + controller dispatch | Prerequisite — provides WaveExecutor, DRGroupHandler, fail-forward |
| 4.3 | Planned migration workflow + VMManager | Prerequisite — provides PlannedMigrationHandler (reused for failback), VMManager, step recording, resolveVolumeGroupID |
| 4.4 | Disaster failover workflow | Prerequisite — provides DisasterFailoverHandler (reused for failback) |
| 4.5 | Fail-forward error handling & partial success | Prerequisite — provides GroupError, StepRecorder, PVCResolver |
| 4.6 | Failed DRGroup retry | Prerequisite — provides retry mechanism |
| 4.7 | Checkpoint, resume & HA | Prerequisite — provides Checkpointer, ResumeAnalyzer, leader election |
| **4.8** | **Re-protect & failback workflows** | **This story — completes the 4-state DR lifecycle** |

### Critical Design Decision: Re-protect is Storage-Only (No Waves)

| Approach | Verdict | Reason |
|----------|---------|--------|
| Wave-based re-protect | Rejected | Re-protect does not start/stop VMs — there is nothing to "wave" through. Waves exist to control VM startup ordering; re-protect only changes storage roles |
| **Storage-only re-protect** | **Selected** | Operates on all volume groups in parallel. No VMManager dependency. Simpler execution model. Matches architecture document: "re-protect is a storage-only operation (no waves)" |
| Re-protect as DRGroupHandler | Rejected | DRGroupHandler is designed for per-DRGroup execution with VMs. Re-protect operates on VGs directly, not grouped into DRGroups |

### Critical Design Decision: Failback Reuses Existing Handlers

| Approach | Verdict | Reason |
|----------|---------|--------|
| New FailbackHandler | Rejected | Failback is structurally identical to failover — same wave-based execution, same VM stop/start, same driver calls. Creating a new handler would duplicate code |
| **Reuse PlannedMigrationHandler / DisasterFailoverHandler** | **Selected** | The state machine maps `DRedSteadyState + mode → FailingBack`, and `CompleteTransition("FailingBack") → SteadyState`. The handlers don't need to know whether they're failing over or failing back — they execute the same sequence. The "direction" is determined by which site is currently active (which VMs to stop/start, which volumes to promote) |

### Critical Design Decision: ExecutionModeReprotect vs Annotation

| Approach | Verdict | Reason |
|----------|---------|--------|
| **New ExecutionMode constant** | **Selected** | Clean API: `mode: reprotect` in DRExecution spec. State machine validates `FailedOver + reprotect → Reprotecting`. The mode field is the natural place for this — it already disambiguates planned_migration vs disaster |
| Annotation-triggered re-protect | Rejected | Inconsistent with the execution model. DRExecution.Spec.Mode is the established pattern for execution type. Annotations are used for retry (Story 4.6) which is a different concept |

### Critical Design Decision: Re-protect Health Monitoring Model

| Approach | Verdict | Reason |
|----------|---------|--------|
| Blocking poll in handler goroutine | Rejected | Re-protect monitoring can take hours/days for large data sets. A blocking poll ties up a goroutine and can't survive pod restarts without checkpointing. Not Kubernetes-native |
| **Polling with checkpointing** | **Selected** | The handler runs in a goroutine (matching Stories 4.3/4.4 pattern), polls health at intervals, and writes checkpoints after each poll. On pod restart, the resume path (Story 4.7) restores the handler to its monitoring phase. Checkpoints include current health snapshot |
| RequeueAfter reconcile loop | Rejected | Would require restructuring the controller dispatch model away from the goroutine pattern established in Stories 4.2-4.7. Adds complexity without benefit given checkpointing exists |

### Critical Design Decision: Health Monitoring Timeout Behavior

| Approach | Verdict | Reason |
|----------|---------|--------|
| Failed on timeout | Rejected | Replication may still be syncing — marking as Failed implies the operation was unsuccessful. In reality, role setup succeeded and data is replicating |
| **PartiallySucceeded on timeout** | **Selected** | Role setup completed successfully (volumes are in correct roles, replication is active). Only the "wait for full sync" timed out. The operator can monitor replication health via DRPlan status conditions. DRPlan still transitions to DRedSteadyState because the system IS protected (just not fully synced) |

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
| `pkg/engine/statemachine.go` (Story 4.1) | `Transition`, `CompleteTransition`, `ValidStartingPhases`, `validTransitions` map | Add `FailedOver + reprotect → Reprotecting` entry. Failback transitions already exist |
| `pkg/engine/executor.go` (Stories 4.2-4.7) | `WaveExecutor`, `DRGroupHandler`, `ExecutionGroup`, `Checkpointer` | Failback reuses the full executor chain. Re-protect reuses `Checkpointer` interface only |
| `pkg/engine/planned.go` (Story 4.3) | `PlannedMigrationHandler`, `resolveVolumeGroupID`, step recording patterns | Failback reuses PlannedMigrationHandler directly. Re-protect reuses `resolveVolumeGroupID` and step recording patterns |
| `pkg/engine/disaster.go` (Story 4.4) | `DisasterFailoverHandler`, RPO recording pattern | Failback reuses DisasterFailoverHandler directly |
| `pkg/engine/vm.go` (Story 4.3) | `VMManager`, `KubeVirtVMManager` | Failback reuses VMManager. Re-protect does NOT use VMManager |
| `pkg/engine/checkpoint.go` (Story 4.7) | `Checkpointer`, `KubeCheckpointer`, `NoOpCheckpointer` | Re-protect uses Checkpointer for role setup + health monitoring checkpoints |
| `pkg/engine/resume.go` (Story 4.7) | `ResumeAnalyzer`, `ResumePoint` | Extended for re-protect resume (different resume model than wave-based) |
| `pkg/engine/helpers.go` (Story 4.4) | `resolveVolumeGroupID`, shared step constants | Re-protect reuses VG ID resolution |
| `pkg/engine/pvc_resolver.go` (Story 4.5) | `PVCResolver` — resolves driver per VM/VG from PVC storage class | Re-protect uses PVCResolver to resolve driver per VG |
| `pkg/drivers/interface.go` | `StorageProvider` — `StopReplication`, `SetSource`, `GetReplicationStatus` | Core driver calls for re-protect |
| `pkg/drivers/types.go` | `StopReplicationOptions{Force}`, `SetSourceOptions{Force}`, `ReplicationStatus`, `ReplicationHealth` | All types used by re-protect handler |
| `pkg/drivers/fake/driver.go` | Programmable fake `StorageProvider` | Unit testing re-protect without real storage |
| `pkg/apis/soteria.io/v1alpha1/types.go` | `DRExecution`, `ExecutionMode`, `ExecutionResult`, `StepStatus`, all phase constants | Add `ExecutionModeReprotect`. All status types reused for re-protect step recording |
| `pkg/controller/drexecution/reconciler.go` (Stories 4.1-4.7) | DRExecution reconciler with new-execution, resume, and retry paths | Add re-protect dispatch path alongside existing handler dispatch |
| `pkg/admission/drexecution_validator.go` (Story 4.1) | DRExecution admission webhook | Accept `reprotect` mode |
| `pkg/registry/drexecution/strategy.go` | Strategy validation for DRExecution | Accept `reprotect` mode |
| `pkg/engine/discovery.go` | `VMDiscoverer`, `GroupByWave` | Re-protect reuses VM discovery to find all VGs in the plan |
| `pkg/engine/consistency.go` | `ResolveVolumeGroups` | Re-protect reuses consistency resolution to get VG list |
| `cmd/soteria/main.go` (Stories 4.1-4.7) | Manager wiring with all existing components | Wire ReprotectHandler |
| `pkg/metrics/metrics.go` (Story 4.7) | Prometheus metrics registration pattern | Add re-protect metrics following same pattern |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | `ExecutionModePlannedMigration`, `ExecutionModeDisaster` | Add `ExecutionModeReprotect ExecutionMode = "reprotect"` |
| `pkg/engine/statemachine.go` (Story 4.1) | `validTransitions` has 4 entries for failover + failback | Add `FailedOver + reprotect → Reprotecting` |
| `pkg/registry/drexecution/strategy.go` | Validates mode is `planned_migration` or `disaster` | Add `reprotect` to accepted modes |
| `pkg/admission/drexecution_validator.go` (Story 4.1) | Validates mode, calls `engine.Transition` for phase check | Add `reprotect` to accepted modes (the Transition call handles phase validation automatically) |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Empty or validates mode is `planned_migration`/`disaster` | Add `reprotect` to accepted modes |
| `pkg/controller/drexecution/reconciler.go` (Stories 4.1-4.7) | Dispatches PlannedMigrationHandler or DisasterFailoverHandler based on mode | Add re-protect dispatch: when mode is `reprotect`, build ReprotectInput and call ReprotectHandler.Execute instead of WaveExecutor |
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
| `pkg/engine/planned.go` | Planned migration handler — unchanged; reused for failback |
| `pkg/engine/planned_test.go` | Tests — no changes |
| `pkg/engine/disaster.go` | Disaster handler — unchanged; reused for failback |
| `pkg/engine/disaster_test.go` | Tests — no changes |
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

### Re-protect Flow Diagram

```
DRPlan: FailedOver
        │
        ▼
Operator creates DRExecution{mode: "reprotect"}
        │
        ▼
Admission webhook validates FailedOver + reprotect → allowed
        │
        ▼
Controller: Transition(FailedOver, reprotect) → Reprotecting
        │
        ▼
Phase 1 — Role Setup (per volume group):
  VG1: StopReplication(force=true) → SetSource(force=false) → ✓
  VG2: StopReplication(force=true) [FAIL, logged] → SetSource(force=false) → ✓
  VG3: StopReplication(force=true) → SetSource(force=false) → ✓
        │
        ▼ checkpoint
        │
Phase 2 — Health Monitoring (30s poll interval):
  Poll 1: VG1=Syncing, VG2=Syncing, VG3=Syncing (0/3 healthy)
  Poll 2: VG1=Healthy, VG2=Syncing, VG3=Syncing (1/3 healthy)
  ...
  Poll N: VG1=Healthy, VG2=Healthy, VG3=Healthy (3/3 healthy)
        │
        ▼ checkpoint per poll
        │
Controller: CompleteTransition(Reprotecting) → DRedSteadyState
        │
        ▼
DRExecution.Status.Result = Succeeded
DRPlan: DRedSteadyState (ready for failback)
```

### Failback Flow (No New Code)

```
DRPlan: DRedSteadyState
        │
        ▼
Operator creates DRExecution{mode: "planned_migration"}
        │
        ▼
Controller: Transition(DRedSteadyState, planned_migration) → FailingBack
        │
        ▼
[Reuses PlannedMigrationHandler — same code as failover]
Step 0: Stop VMs, StopReplication, sync wait
Waves: SetSource + StartVM per DRGroup
        │
        ▼
Controller: CompleteTransition(FailingBack) → SteadyState
        │
        ▼
DRPlan: SteadyState (original configuration restored)
```

### Full DR Lifecycle State Machine

```
SteadyState ──[failover]──► FailingOver ──[complete]──► FailedOver
                                                              │
                                                    [reprotect]
                                                              │
                                                              ▼
SteadyState ◄──[complete]── FailingBack ◄──[failback]── DRedSteadyState
                                                              ▲
                                                    [complete]│
                                                              │
                                                        Reprotecting
```

### RBAC Requirements

No new RBAC markers needed beyond what Stories 4.1-4.7 established. The DRExecution controller already has permissions for:
- `soteria.io/drexecutions` — get, list, watch, update, patch
- `soteria.io/drexecutions/status` — get, update, patch
- `soteria.io/drplans` — get, list, watch (for fetching plan)
- `soteria.io/drplans/status` — update, patch (for phase transitions)
- `soteria.io/drgroupstatuses` — full CRUD (for wave-based execution)
- `events.k8s.io/events` — create, patch (for event emission)

Re-protect uses the same permissions: reads DRPlan, updates DRExecution status, updates DRPlan status phase. No additional RBAC needed.

### Test Strategy

**ReprotectHandler tests** (`pkg/engine/reprotect_test.go`): Use fake driver (`pkg/drivers/fake/`) and `NoOpCheckpointer` from Story 4.7. Configure fake driver to return specific responses per VG. Test both role setup and health monitoring phases independently and together.

**State machine tests** (`pkg/engine/statemachine_test.go`): Add table entries for the new `FailedOver + reprotect` transition and verify existing failback transitions.

**Strategy/admission tests**: Add test cases for `reprotect` mode validation. Verify `reprotect` is accepted and invalid modes are still rejected.

**Reconciler tests**: Test re-protect dispatch path — verify handler is called for reprotect mode, verify PlannedMigrationHandler is called for failback from DRedSteadyState.

**Full lifecycle test**: End-to-end test through all 6 phases. Uses mock drivers and VMManagers.

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
- `PlannedMigrationHandler.PreExecute` sync polling pattern — re-protect's health monitoring is similar (poll interval, timeout, context cancellation)
- Handler is reused unchanged for failback (planned_migration from DRedSteadyState)

**From Story 4.4 (Disaster Failover Workflow):**
- `DisasterFailoverHandler` — reused unchanged for failback (disaster from DRedSteadyState)
- RPO recording pattern — re-protect doesn't need RPO (no data loss during re-protect), but failback reuses the disaster handler's RPO recording

**From Story 4.1 (DR State Machine & Execution Controller):**
- State machine `validTransitions` map — extend with `FailedOver + reprotect → Reprotecting`
- `CompleteTransition` already handles `Reprotecting → DRedSteadyState` and `FailingBack → SteadyState`
- `ValidStartingPhases` — extend for `reprotect` mode
- Controller reconcile path ordering — re-protect dispatch is a new branch alongside existing mode switch

**From Epic 3 (Storage Driver Framework):**
- All driver methods are idempotent — `StopReplication` and `SetSource` are safe to retry during re-protect resume
- `SetTargetOptions{Force: true}` comment explicitly mentions re-protect scenario
- `StopReplicationOptions{Force: true}` comment explicitly mentions re-protect scenario
- Fake driver enables full testing of re-protect without real storage

### Build Commands

```bash
make manifests    # Regenerate RBAC (verify no unexpected changes)
make generate     # Regenerate deepcopy for new ExecutionModeReprotect
make test         # All unit tests (new + existing)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make build        # Verify compilation
```

### Project Structure Notes

All files align with the architecture document:
- `pkg/engine/reprotect.go` — new: ReprotectHandler, ReprotectInput, ReprotectResult, health monitoring
- `pkg/engine/reprotect_test.go` — new: comprehensive re-protect tests
- `pkg/apis/soteria.io/v1alpha1/types.go` — modified: add ExecutionModeReprotect
- `pkg/engine/statemachine.go` — modified: add FailedOver + reprotect transition
- `pkg/registry/drexecution/strategy.go` — modified: accept reprotect mode
- `pkg/admission/drexecution_validator.go` — modified: accept reprotect mode
- `pkg/controller/drexecution/reconciler.go` — modified: add re-protect dispatch path
- `cmd/soteria/main.go` — modified: wire ReprotectHandler
- `pkg/metrics/metrics.go` — modified: add re-protect metrics
- `pkg/engine/doc.go` — modified: add re-protect and failback documentation

The engine boundary is maintained: `pkg/engine/` handles re-protect workflow logic. The handler calls drivers for storage operations and uses the Kubernetes API client (via Checkpointer) for status persistence. It does NOT know about ScyllaDB, CDC, or aggregated API server internals.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.8] — Story acceptance criteria (BDD format): re-protect from FailedOver, failback from DRedSteadyState, origin unreachable tolerance, health monitoring, full lifecycle
- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.1] — State machine transitions: SteadyState→FailingOver, FailedOver→Reprotecting, DRedSteadyState→FailingBack, FailingBack→SteadyState
- [Source: _bmad-output/planning-artifacts/epics.md#FR16] — Re-protect: StopReplication on old active, transition roles, monitor until healthy
- [Source: _bmad-output/planning-artifacts/epics.md#FR17] — Failback: reverse of failover using same wave-based engine
- [Source: _bmad-output/planning-artifacts/prd.md#DR Execution & Workflow] — FR16 re-protect, FR17 failback, FR18 human-triggered
- [Source: _bmad-output/planning-artifacts/prd.md#MVP Feature Set] — Re-protect and failback are v1 must-haves
- [Source: _bmad-output/planning-artifacts/architecture.md#CRD Status Patterns] — DRPlan phase: Reprotecting, DRedSteadyState, FailingBack
- [Source: _bmad-output/planning-artifacts/architecture.md#API & Communication Patterns] — Checkpointing per-DRGroup, error model fail-forward
- [Source: _bmad-output/planning-artifacts/architecture.md#Gap Analysis] — "Add pkg/engine/reprotect.go — re-protect is a storage-only operation (no waves)"
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — All 7 methods idempotent, accept context.Context
- [Source: _bmad-output/project-context.md#Workflow Engine] — "Re-protect workflow: StopReplication on old active → SetTarget on old active / SetSource on new active → monitor until healthy (storage-only, no waves)"
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — 7-method interface, role model, Force flags for re-protect
- [Source: pkg/drivers/interface.go] — StorageProvider: SetSource, SetTarget, StopReplication, GetReplicationStatus
- [Source: pkg/drivers/types.go] — SetSourceOptions{Force}, SetTargetOptions{Force}, StopReplicationOptions{Force}, ReplicationHealth, ReplicationStatus
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — ExecutionMode, DRPlan phases, DRExecution spec/status, StepStatus
- [Source: _bmad-output/implementation-artifacts/4-7-checkpoint-resume-high-availability.md] — Previous story: Checkpointer, ResumeAnalyzer, checkpoint granularity
- [Source: _bmad-output/implementation-artifacts/4-3-planned-migration-workflow.md] — Planned migration handler: PreExecute, sync polling, resolveVolumeGroupID
- [Source: _bmad-output/implementation-artifacts/4-4-disaster-failover-workflow.md] — Disaster handler: force-promote, RPO recording, no Step 0
- [Source: _bmad-output/implementation-artifacts/4-1-dr-state-machine-execution-controller.md] — State machine: Transition, CompleteTransition, admission webhook

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
