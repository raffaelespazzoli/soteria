# Story 17.14: Executing Failover

Status: review

## Story

As a DR operator,
I want a clear guide for triggering and monitoring failover operations,
so that I can execute disaster recovery confidently and handle partial failures.

## Acceptance Criteria

**AC1: Planned migration trigger is documented**
Given the failover guide
When a reader wants to trigger a planned migration
Then they find instructions for creating a DRExecution CR (via kubectl or UI) with the planned migration configuration

**AC2: Disaster failover trigger is documented**
Given the failover guide
When a reader needs to trigger an emergency disaster failover
Then they find instructions for creating a DRExecution CR with disaster mode enabled

**AC3: Monitoring progress is documented**
Given the failover guide
When a failover is in progress
Then the reader knows how to monitor progress via kubectl (status conditions, events) and the UI

**AC4: Partial success handling is documented**
Given the failover guide
When some DRGroups fail during execution
Then the reader understands partial success semantics and how to identify which groups failed

**AC5: Retry procedures are documented**
Given the failover guide
When a reader needs to retry failed DRGroups
Then they find clear instructions for retrying

**AC6: Execution steps are accurate**
Given the documented execution steps
When compared against `pkg/engine/failover.go` and DRExecution reconcile behavior
Then each documented step matches the actual execution sequence

## Tasks / Subtasks

- [x] Task 1: Research failover execution flow (AC: 1, 2, 6)
  - [x] 1.1: Read PRD FR9–FR12, FR18–FR19 for conceptual failover design
  - [x] 1.2: Walk `pkg/engine/failover.go` for FailoverHandler — PreExecute (Step 0) and ExecuteGroup path
  - [x] 1.3: Walk `pkg/controller/drexecution/reconciler.go` for reconcileSetup, reconcileWaveExecution, reconcileResyncGate
  - [x] 1.4: Walk `pkg/engine/executor.go` for wave-by-wave execution dispatch
- [x] Task 2: Research status and monitoring (AC: 3, 4)
  - [x] 2.1: Walk `pkg/apis/soteria.io/v1alpha1/types.go` for DRExecution status types (phases, conditions, DRGroupExecutionStatus)
  - [x] 2.2: Walk `pkg/controller/drexecution/reconciler.go` for event emission (FailoverStarted, GroupFailed, ExecutionCompleted, etc.)
  - [x] 2.3: Identify kubectl commands for monitoring (`kubectl get drexecutions`, events, conditions)
- [x] Task 3: Research retry mechanism (AC: 5)
  - [x] 3.1: Walk `pkg/engine/executor.go` for ExecuteRetry and retry annotation handling (`soteria.io/retry-groups`)
  - [x] 3.2: Walk `pkg/controller/drexecution/reconciler.go` reconcileRetry for VM health validation preconditions
- [x] Task 4: Write the documentation page (AC: 1, 2, 3, 4, 5)
  - [x] 4.1: Write `docs/usage/failover.md` covering planned migration trigger, disaster failover trigger, monitoring, partial success, retry
  - [x] 4.2: Add annotated DRExecution YAML examples for planned and disaster modes
  - [x] 4.3: Add kubectl commands for monitoring status and events
- [x] Task 5: Verify accuracy (AC: 6)
  - [x] 5.1: Verify documented execution steps match actual code flow

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR9 (planned migration), FR10 (disaster failover), FR11 (wave-by-wave), FR12 (maxConcurrentFailovers), FR18 (explicit human initiation), FR19 (mode at execution time)]
- [Source: _bmad-output/planning-artifacts/architecture.md — execution lifecycle, state machine]

### Code to Verify Against

- [Source: pkg/engine/failover.go — FailoverHandler.PreExecute (Step 0: stop VMs in reverse wave order + StopReplication) and ExecuteGroup (unified path: SetSource → StartVM for both planned and disaster)]
- [Source: pkg/engine/failover.go — FailoverConfig: {GracefulShutdown: true} for planned migration, {GracefulShutdown: false} for disaster]
- [Source: pkg/controller/drexecution/reconciler.go — reconcileSetup (validates mode, transitions DRPlan phase, sets startTime), reconcileWaveExecution (initializes waves, dispatches handler, converts to WaitingForVMReady), reconcileRetry (PartiallySucceeded + annotation)]
- [Source: pkg/controller/drexecution/reconciler.go — reconcileResyncGate (single-site Step 0: checks VRs → SetSource → Step0Complete), reconcileStep0/reconcileTargetStep0 (multi-site Step 0 coordination)]
- [Source: pkg/engine/executor.go — WaveExecutor: discover → group → chunk → execute; fail-forward semantics; ComputeResult (all Completed → Succeeded; mixed → PartiallySucceeded; no Completed → Failed)]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRExecutionSpec (planName, mode), DRExecutionStatus (phase, result, waves, conditions, siteStatuses), ExecutionMode enum (planned_migration, disaster, reprotect)]
- [Source: pkg/engine/doc.go — retry annotation: `soteria.io/retry-groups` with comma-separated group names or "all-failed"; VM health validation before retry; RetryCount audit trail]

### Implementation Pattern

- Structure as a step-by-step guide with clear sections: "Triggering a Planned Migration", "Triggering a Disaster Failover", "Monitoring Execution", "Understanding Results", "Retrying Failed Groups"
- Include DRExecution YAML examples:
  ```yaml
  apiVersion: soteria.io/v1alpha1
  kind: DRExecution
  metadata:
    name: erp-failover-001
  spec:
    planName: erp-full-stack
    mode: planned_migration  # or: disaster
  ```
- Document the execution sequence: Setup → (planned: Step 0 → resync gate →) Wave execution → VM readiness gate → Result
- Document status conditions: Progressing, Step0Started, Step0Complete, Ready
- Document retry: `kubectl annotate drexecution <name> soteria.io/retry-groups=all-failed`

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/usage/failover.md | MODIFIED | Step-by-step failover trigger, monitoring, partial success, retry guide (replaced placeholder) |

### Key Constraints

- All failover operations require explicit human initiation — no auto-failover (FR18)
- Execution mode is specified at runtime on the DRExecution, not on the DRPlan (FR19)
- Planned migration: GracefulShutdown=true → Step 0 runs (stop VMs + StopReplication + wait for VRs secondary + SetSource)
- Disaster: GracefulShutdown=false → Step 0 skipped, per-group path identical (SetSource → StartVM)
- Retry requires: execution is PartiallySucceeded, no InProgress groups, VMs pass VMHealthValidator checks

### Project Structure Notes

- Execution controller: `pkg/controller/drexecution/reconciler.go`
- Failover handler: `pkg/engine/failover.go`
- Wave executor: `pkg/engine/executor.go`
- API types: `pkg/apis/soteria.io/v1alpha1/types.go`

### References

- [Source: pkg/engine/failover.go — FailoverHandler implementation]
- [Source: pkg/controller/drexecution/reconciler.go — full reconciliation loop]
- [Source: pkg/engine/executor.go — WaveExecutor, ExecuteRetry]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRExecution types]
- [Source: pkg/engine/doc.go — comprehensive engine documentation]
- [Source: _bmad-output/planning-artifacts/prd.md — FR9, FR10, FR18, FR19]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- ✅ Tasks 1–3 (Research): Walked all source files: `pkg/engine/failover.go`, `pkg/engine/executor.go`, `pkg/engine/doc.go`, `pkg/engine/statemachine.go`, `pkg/controller/drexecution/reconciler.go`, `pkg/apis/soteria.io/v1alpha1/types.go`. Extracted execution flow, status types, event reasons, retry mechanics.
- ✅ Task 4 (Write docs): Wrote comprehensive `docs/usage/failover.md` replacing placeholder. Covers: prerequisites, execution modes table, planned migration trigger (with YAML + kubectl), disaster failover trigger (with YAML + kubectl), execution lifecycle (Setup → Step 0 → Resync Gate → Wave Execution → VM Readiness → Result), monitoring (status fields table, conditions table, events table, kubectl commands), result computation, partial success semantics, retry mechanism (preconditions, all-failed, specific groups, rejection reasons), Mermaid lifecycle diagram, crash recovery, multi-site coordination.
- ✅ Task 5 (Verify accuracy): Cross-referenced every documented claim against source code — FailoverConfig mapping (GracefulShutdown true/false), default timeouts (VMReady 5m, Step0 10m), safety requeue (10s), fail-fast vs fail-forward, state machine transitions, event reasons, retry annotation handling, result computation logic. All accurate.
- ℹ️ File path deviation: Story specified `docs/usage/executing-failover.md` but `docs/usage/failover.md` already existed as a placeholder and was linked in `mkdocs.yml` nav. Wrote to the existing file to avoid nav breakage.
- ℹ️ Integration tests: Skipped due to host `inotify.max_user_instances` limit (128 < 1024). Unit tests (`make test`) all pass. No Go code changes in this story — documentation only.

### File List

- docs/usage/failover.md (MODIFIED — replaced placeholder with comprehensive guide)
- _bmad-output/implementation-artifacts/17-14-executing-failover.md (MODIFIED — task checkboxes, status, dev record)
