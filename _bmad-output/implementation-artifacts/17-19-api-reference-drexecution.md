# Story 17.19: API Reference: DRExecution

Status: ready-for-dev

## Story

As a platform engineer or automation developer,
I want a complete API reference for the DRExecution CRD,
so that I can programmatically trigger and monitor DR operations with full knowledge of status semantics.

## Acceptance Criteria

**AC1: All spec fields are documented**
Given the DRExecution API reference
When a reader reviews spec fields
Then every field in DRExecutionSpec is documented with: name, type, required/optional, default value, and description

**AC2: All status fields are documented**
Given the DRExecution API reference
When a reader reviews status fields
Then every field in DRExecutionStatus is documented including conditions, phase, and DRGroupStatus sub-resources

**AC3: Status conditions are documented**
Given the DRExecution API reference
When a reader reviews status conditions
Then every condition type is documented with: type name, meaning, and when it transitions

**AC4: Phase semantics are documented**
Given the DRExecution API reference
When a reader reviews phase values
Then each phase value is documented with its meaning and the conditions that trigger transitions

**AC5: DRGroupStatus is documented**
Given the DRExecution API reference
When a reader reviews DRGroupStatus
Then the sub-resource fields are fully documented

**AC6: Reference matches actual CRD schema**
Given the documented API reference
When compared against the generated CRD schema and types.go
Then every field, type, and semantic is accurate

## Tasks / Subtasks

- [ ] Task 1: Extract spec field definitions (AC: 1, 6)
  - [ ] 1.1: Walk `pkg/apis/soteria.io/v1alpha1/types.go` for DRExecution, DRExecutionSpec — fields and kubebuilder markers
  - [ ] 1.2: Walk `pkg/apis/soteria.io/v1alpha1/validation.go` for DRExecution validation rules
- [ ] Task 2: Extract status field definitions (AC: 2, 3, 4, 5, 6)
  - [ ] 2.1: Walk `pkg/apis/soteria.io/v1alpha1/types.go` for DRExecutionStatus, WaveStatus, DRGroupExecutionStatus, StepStatus, SiteCoordinationStatus
  - [ ] 2.2: Walk `pkg/controller/drexecution/reconciler.go` for condition types set during reconciliation (Progressing, Step0Started, Step0Complete, Ready, ResyncPending, ReprotectPhase, RetryRejected)
  - [ ] 2.3: Walk `pkg/controller/drexecution/reconciler.go` for phase transition logic and result computation
- [ ] Task 3: Write the reference page (AC: 1, 2, 3, 4, 5)
  - [ ] 3.1: Write `docs/reference/api/drexecution.md` with full field reference tables (spec + status)
  - [ ] 3.2: Document all status conditions with lifecycle (when set, when cleared, meanings)
  - [ ] 3.3: Document phase semantics and result computation (Succeeded, PartiallySucceeded, Failed)
  - [ ] 3.4: Add DRExecution YAML examples for planned migration and disaster failover
  - [ ] 3.5: Document status condition lifecycle and DRGroupResult state transitions
- [ ] Task 4: Verify accuracy (AC: 6)
  - [ ] 4.1: Cross-reference every documented field against the actual types.go and reconciler

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR9 (planned migration), FR10 (disaster), FR18 (explicit initiation), FR19 (mode at execution time)]
- [Source: _bmad-output/planning-artifacts/architecture.md — execution lifecycle, state machine]

### Code to Verify Against

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRExecutionSpec: PlanName (string), Mode (enum: planned_migration|disaster|reprotect)]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRExecutionStatus: Phase (enum: Pending|Executing|Succeeded|PartiallySucceeded|Failed), IsActive (bool), Result (enum: Succeeded|PartiallySucceeded|Failed), Waves ([]WaveStatus), StartTime, CompletionTime, Duration, Conditions, SiteStatuses (map)]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — WaveStatus: WaveIndex (int), Groups ([]DRGroupExecutionStatus), StartTime, CompletionTime, VMReadyStartTime]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRGroupExecutionStatus: Name, Result (enum: Pending|InProgress|Completed|Failed|WaitingForVMReady), VMNames, Error, Steps ([]StepStatus), RetryCount, StartTime, CompletionTime]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — StepStatus: Name (e.g., "SetSource", "StartVM", "WaitVMReady"), Status, Message, Timestamp]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — SiteCoordinationStatus: DemotionComplete, Step0Complete, LastUpdated]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — IsTerminal() method: Result != "" means terminal]
- [Source: pkg/controller/drexecution/reconciler.go — Conditions set: "Progressing" (ExecutionStarted), "Step0Started" (PreExecuteCompleted), "Step0Complete" (DemotionAndPromotionCompleted), "Ready" (ReprotectSucceeded/Failed), "ResyncPending", "ReprotectPhase", "RetryRejected"]
- [Source: pkg/controller/drexecution/reconciler.go — Result computation: all Completed → Succeeded; mixed Completed+Failed → PartiallySucceeded; no Completed → Failed]
- [Source: pkg/controller/drexecution/reconciler.go — Events emitted: FailoverStarted, Step0Failed, Step0Completed, ExecutionCompleted, ExecutionResumed, RetryStarted, RetryRejected, GroupRetrySucceeded, GroupRetryFailed, ReprotectStarted, ReprotectRoleSetupComplete, etc.]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — Constants: PlanNameLabel ("soteria.io/plan-name"), TriggeredByAnnotation ("soteria.io/triggered-by")]

### Implementation Pattern

- Structure as field reference tables: Spec fields, Status fields, WaveStatus, DRGroupExecutionStatus, StepStatus, SiteCoordinationStatus
- Include a DRGroupResult state machine diagram: Pending → InProgress → Completed/Failed, Completed → WaitingForVMReady → Completed/Failed
- Document the condition lifecycle in a dedicated table: Condition Type, When Set, Reason, Meaning
- Include annotated YAML examples showing status at different lifecycle points (in-progress, partial success, completed)
- Document the retry annotation: `soteria.io/retry-groups` with values "all-failed" or comma-separated group names

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/reference/api/drexecution.md | NEW | Exhaustive DRExecution field reference with phases, conditions, status semantics |

### Key Constraints

- DRExecution is immutable after creation (spec cannot be updated)
- Only Succeeded and Failed are fully terminal — PartiallySucceeded is re-openable via retry annotation
- Result computation depends on DRGroupResults across all waves
- SiteStatuses map uses write-isolation: each site writes only to its own entry

### Project Structure Notes

- API types: `pkg/apis/soteria.io/v1alpha1/types.go`
- Execution controller: `pkg/controller/drexecution/reconciler.go`
- Engine step constants: `pkg/engine/failover.go` — StepSetSource, StepStartVM, StepWaitVMReady

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRExecution, DRExecutionSpec, DRExecutionStatus, all nested types]
- [Source: pkg/controller/drexecution/reconciler.go — reconciliation loop, conditions, events, result computation]
- [Source: pkg/engine/failover.go — step constants: SetSource, StartVM, WaitVMReady]
- [Source: pkg/engine/doc.go — retry annotation, checkpoint, resume documentation]
- [Source: _bmad-output/planning-artifacts/prd.md — FR9, FR10, FR18, FR19]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
