# Story 17.12: Waves & Throttling

Status: review

## Story

As a DR operator,
I want to understand how VMs are grouped into waves and how execution is throttled,
so that I can configure failover ordering and concurrency to meet my application dependencies.

## Acceptance Criteria

**AC1: Wave formation is explained**
Given the waves and throttling page
When a reader reviews wave formation
Then they understand how VMs are grouped into waves based on labels

**AC2: Sequential wave execution is documented**
Given the page
When a reader reviews execution ordering
Then they understand that waves execute sequentially — wave N+1 waits for wave N VMs to be Running

**AC3: maxConcurrentFailovers and DRGroup chunking are explained**
Given the page
When a reader reviews throttling
Then they understand how maxConcurrentFailovers limits concurrent DRGroup processing within waves

**AC4: Startup ordering is documented**
Given the page
When a reader reviews inter-wave dependencies
Then they understand the gate condition — wave N+1 starts only after wave N VMs reach Running state

**AC5: Documentation matches engine implementation**
Given the documented behavior
When compared against wave executor and chunking logic in `pkg/engine/`
Then the formation, sequencing, and throttling behavior matches the actual code

## Tasks / Subtasks

- [x] Task 1: Research wave formation and execution (AC: 1, 2, 5)
  - [x] 1.1: Read PRD FR11–FR15 for the conceptual wave design
  - [x] 1.2: Walk `pkg/engine/discovery.go` for VM discovery and wave grouping by `soteria.io/wave` label
  - [x] 1.3: Walk `pkg/engine/executor.go` for sequential wave execution and fail-forward semantics
  - [x] 1.4: Walk `pkg/engine/failover.go` for reverse-wave VM shutdown in Step 0 (planned migration)
- [x] Task 2: Research chunking and throttling (AC: 3, 5)
  - [x] 2.1: Walk `pkg/engine/chunker.go` for DRGroup chunking respecting `maxConcurrentFailovers`
  - [x] 2.2: Walk `pkg/engine/consistency.go` for namespace-level VolumeGroup indivisibility constraint
  - [x] 2.3: Review test scenarios in `pkg/engine/` for edge cases
- [x] Task 3: Write the documentation page (AC: 1, 2, 3, 4)
  - [x] 3.1: Write `docs/usage/waves.md` covering wave formation from labels, VM grouping, sequential execution, maxConcurrentFailovers, DRGroup chunking, startup ordering
  - [x] 3.2: Add diagrams showing wave execution timeline (e.g., wave → chunk → group flow)
- [x] Task 4: Verify accuracy (AC: 5)
  - [x] 4.1: Verify all behavior descriptions match actual engine code

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR11 (sequential waves + concurrent within wave), FR12 (maxConcurrentFailovers VM counting + namespace indivisibility), FR13 (fail-forward)]
- [Source: _bmad-output/planning-artifacts/architecture.md — wave executor design]

### Code to Verify Against

- [Source: pkg/engine/discovery.go — VMDiscoverer interface, partitions VMs into ordered waves by `soteria.io/wave` label value]
- [Source: pkg/engine/consistency.go — namespace-level consistency annotation (`soteria.io/consistency-level`), VolumeGroup formation, wave conflict detection]
- [Source: pkg/engine/chunker.go — DRGroup chunking respecting maxConcurrentFailovers; namespace VGs are indivisible units that cannot split across chunks]
- [Source: pkg/engine/executor.go — WaveExecutor orchestrates discover → group → chunk → execute; waves sequential, chunks within wave also sequential; fail-forward semantics (failed DRGroup does not block subsequent); status serialized via mutex]
- [Source: pkg/engine/failover.go — PreExecute stops all origin VMs in reverse wave order (dependants before dependencies), then StopReplication on each source VG; per-group path: SetSource → StartVM]
- [Source: pkg/engine/doc.go — comprehensive package-level documentation of wave executor, chunker, handler, and fail-forward model]

### Implementation Pattern

- Use mkdocs admonitions for important notes (e.g., `!!! warning` for namespace indivisibility constraint)
- Include a Mermaid or ASCII diagram showing: Wave 1 → [Chunk A (VMs 1-3)] → [Chunk B (VMs 4-5)] → Wave 2 → ...
- Reference `DRPlanSpec.maxConcurrentFailovers` field by its actual JSON path
- Explain the VM readiness gate: after handler ops (SetSource + StartVM) complete, groups enter `WaitingForVMReady` state; wave advances only when all VMs reach Running (default timeout: 5m, configurable via `vmReadyTimeout`)

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/usage/waves-and-throttling.md | NEW | Wave formation, sequential execution, throttling via maxConcurrentFailovers, DRGroup chunking |

### Key Constraints

- Namespace-level VolumeGroups are indivisible — cannot be split across chunks (FR12)
- maxConcurrentFailovers counts individual VMs regardless of consistency level
- If maxConcurrentFailovers < largest namespace+wave group, execution is rejected at pre-flight
- Waves execute sequentially; chunks within a wave also execute sequentially; VM-level parallelism within a chunk is preserved by the handler's internal goroutines

### Project Structure Notes

- Engine code lives in `pkg/engine/` — discovery, consistency, chunker, executor, failover handler
- All engine functions are pure or accept interfaces for dependency injection

### References

- [Source: pkg/engine/doc.go — package-level documentation]
- [Source: pkg/engine/executor.go — WaveExecutor implementation]
- [Source: pkg/engine/chunker.go — DRGroup chunking logic]
- [Source: pkg/engine/failover.go — FailoverHandler with PreExecute and ExecuteGroup]
- [Source: _bmad-output/planning-artifacts/prd.md — FR11, FR12, FR13]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor)

### Debug Log References

No debug issues encountered. Documentation-only story.

### Completion Notes List

- ✅ Wrote comprehensive `docs/usage/waves.md` covering all 5 acceptance criteria
- ✅ Wave formation from `soteria.io/wave` label with lexicographic sorting documented (AC1)
- ✅ Sequential wave execution with readiness gate documented (AC2, AC4)
- ✅ `maxConcurrentFailovers` VM counting, namespace indivisibility, chunking algorithm documented (AC3)
- ✅ Step 0 planned migration pre-execution (reverse wave VM stop + StopReplication) documented
- ✅ Fail-forward semantics and checkpointing documented
- ✅ 4 Mermaid diagrams added: pipeline flowchart, sequence diagram, chunk visualization, full timeline
- ✅ Configuration reference table with all relevant fields and defaults
- ✅ All 18 key claims verified against engine source code (AC5)
- ✅ mkdocs build --strict passes successfully
- Note: File written to `docs/usage/waves.md` (matching existing mkdocs.yml nav entry) rather than `docs/usage/waves-and-throttling.md` as originally specified in story

### File List

| File | Action | Description |
|------|--------|-------------|
| docs/usage/waves.md | MODIFIED | Replaced placeholder with comprehensive waves & throttling documentation |
| _bmad-output/implementation-artifacts/17-12-waves-and-throttling.md | MODIFIED | Updated task checkboxes, status, dev agent record |

### Change Log

- 2026-09-05: Implemented story 17.12 — wrote comprehensive Waves & Throttling documentation page
