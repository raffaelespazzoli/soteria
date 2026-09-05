# Story 17.9: DR Lifecycle & State Machine

Status: ready-for-dev

## Story

As a platform engineer or DR operator,
I want to understand the full DR lifecycle and state machine with all transitions,
so that I can confidently plan and execute disaster recovery operations.

## Acceptance Criteria

**AC1: Four rest states are documented**
Given the DR lifecycle page
When a reader reviews the state descriptions
Then they find clear definitions of: SteadyState, FailedOver, DRedSteadyState, and FailedBack

**AC2: Four transitions are documented**
Given the DR lifecycle page
When a reader reviews the transition descriptions
Then they find: failover, reprotect, failback, and restore — with trigger conditions and outcomes

**AC3: Planned vs disaster mode differences are explained**
Given the DR lifecycle page
When a reader compares planned migration and disaster failover
Then the differences (Step 0, resync behavior, force flags) are clearly documented

**AC4: Mermaid state diagram is included**
Given the DR lifecycle page
When a reader views the state diagram
Then a Mermaid state machine diagram shows all 4 states and 4 transitions with correct arrows

**AC5: State machine matches code implementation**
Given the documented state machine
When compared against `pkg/engine/failover.go`, `EffectivePhase()`, and handler implementations
Then the states, transitions, and conditions match the actual code behavior

**AC6: Fail-forward semantics are documented**
Given the DR lifecycle page
When a reader reviews error handling during transitions
Then fail-forward semantics are explained — how partial failures are handled during execution

## Tasks / Subtasks

- [ ] Task 1: Extract state machine from code (AC: 1, 2, 5)
  - [ ] 1.1: Read PRD FR9-FR19 for the conceptual state machine
  - [ ] 1.2: Read architecture state machine section
  - [ ] 1.3: Walk `pkg/engine/failover.go` to extract actual state transition logic
  - [ ] 1.4: Examine phase constants in `pkg/apis/soteria.io/v1alpha1/types.go`
- [ ] Task 2: Document handler behavior (AC: 3, 6)
  - [ ] 2.1: Walk FailoverHandler (failover.go) for planned migration vs disaster behavior
  - [ ] 2.2: Walk ReprotectHandler (reprotect.go) for re-protection workflow
  - [ ] 2.3: Review test cases in `pkg/engine/failover_test.go` for edge cases and scenarios
- [ ] Task 3: Write the lifecycle documentation (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 3.1: Write `docs/architecture/dr-lifecycle.md` covering: 4 rest states, 4 transitions, planned vs disaster differences, Step 0, fail-forward semantics
  - [ ] 3.2: Create Mermaid state diagram with all states and transitions
  - [ ] 3.3: Verify state names and transition conditions match code exactly

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR9-FR19 DR execution and workflow requirements]
- [Source: _bmad-output/planning-artifacts/architecture.md — state machine section, execution modes]

### Code to Verify Against

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — 8-phase symmetric lifecycle constants: SteadyState, FailingOver, FailedOver, Reprotecting, DRedSteadyState, FailingBack, FailedBack, ReprotectingBack]
- [Source: pkg/engine/doc.go — comprehensive engine documentation including: state machine (statemachine.go), Transition() and CompleteTransition() functions, full cycle description, failover handler, reprotect handler, fail-forward error model, execution resume]
- [Source: pkg/engine/failover.go — FailoverHandler: FailoverConfig{GracefulShutdown bool}, planned_migration → GracefulShutdown=true (Step 0: stop VMs reverse wave order → StopReplication → wait for demotion), disaster → GracefulShutdown=false (PreExecute no-op). Per-group: SetSource → StartVM]
- [Source: pkg/engine/failover_test.go — test cases covering planned migration Step 0, disaster failover, edge cases]

### Implementation Pattern

- **8 phases** (4 rest + 4 transition): SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState
- **Full cycle**: the state machine is symmetric — failback uses the same FailoverHandler as failover (direction is in the phase, not the handler)
- **FailoverConfig** drives behavior: `{GracefulShutdown: true}` for planned_migration, `{GracefulShutdown: false}` for disaster
- **Step 0** (planned migration only): (1) stop origin VMs in reverse wave order (dependants before dependencies), (2) StopReplication on source VGs (demote primary to secondary), (3) wait for VRs to confirm role=Target before promotion
- **Per-group execution** (both modes): SetSource (promote target VR to primary/writable) → StartVM → WaitVMReady
- **Fail-forward**: failed DRGroup does not block subsequent chunks or waves; GroupError provides structured error propagation; result computation: all Completed → Succeeded, mixed → PartiallySucceeded, none Completed → Failed
- **Reprotect**: storage-only (no VM changes) — owner site verifies roles + health monitors; passive site demotes stale primaries
- **Failed DRGroup retry**: annotate DRExecution with `soteria.io/retry-groups` (comma-separated group names or "all-failed")
- **Checkpoint resume**: on crash, in-progress groups are reset to Pending and retried (driver ops are idempotent)
- Use Mermaid stateDiagram-v2 for the lifecycle diagram

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/architecture/dr-lifecycle.md` | NEW | DR lifecycle and state machine documentation with Mermaid diagrams |

### Key Constraints

- Phase names must exactly match the constants in `pkg/apis/soteria.io/v1alpha1/types.go`
- FailoverHandler is reused for failback — do not document separate failback handler
- ReprotectHandler is two-sided (owner + passive) — document both roles
- Fail-forward semantics are critical to document — partial success is an expected outcome
- CompleteTransition only called for Succeeded or PartiallySucceeded — Failed leaves plan in transition phase
- Depends on: 17.1

### Project Structure Notes

- State machine: `pkg/engine/statemachine.go` (Transition, CompleteTransition)
- Failover handler: `pkg/engine/failover.go` (FailoverConfig, PreExecute, ExecuteGroup)
- Reprotect handler: `pkg/engine/reprotect.go` (ReprotectHandler, owner + passive)
- Executor: `pkg/engine/executor.go` (wave orchestration, fail-forward, checkpointing)
- Discovery: `pkg/engine/discovery.go` (VM discovery, wave grouping)
- Resume: `pkg/engine/resume.go` (execution state reconstruction)
- VM manager: `pkg/engine/vm.go` (KubeVirt VM lifecycle)

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — lines 24-35, phase constants]
- [Source: pkg/engine/doc.go — full engine documentation, lines 18-210]
- [Source: pkg/engine/failover.go — lines 17-49, FailoverHandler documentation]
- [Source: pkg/engine/failover.go — lines 71-80, FailoverConfig struct]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
