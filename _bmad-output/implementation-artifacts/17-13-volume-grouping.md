# Story 17.13: Volume Grouping

Status: done

## Story

As a DR operator,
I want to understand the volume grouping options and their trade-offs,
so that I can choose the right grouping strategy for my application's consistency requirements.

## Acceptance Criteria

**AC1: Namespace-level grouping is documented**
Given the volume grouping page
When a reader reviews namespace-level grouping
Then they understand how the `soteria.io/namespace-consistency` annotation works and when to use it

**AC2: VM-level grouping is documented**
Given the volume grouping page
When a reader reviews VM-level grouping
Then they understand how individual VM disks are grouped into DRGroups by default

**AC3: Trade-offs are explained**
Given the page
When a reader compares the two grouping strategies
Then they understand when to use each approach and the consistency implications

**AC4: Wave constraint for namespace grouping is documented**
Given the page
When a reader configures namespace-level grouping
Then they understand the constraint that namespace-grouped VMs must be in the same wave

**AC5: Label-to-DRGroup mapping is documented**
Given the page
When a reader reviews how labels map to DRGroups
Then the label → DRGroup formation logic is clearly explained

**AC6: Documentation matches implementation**
Given the documented behavior
When compared against DRGroup formation code, namespace consistency logic, and webhook validation
Then all described behavior matches the actual code

## Tasks / Subtasks

- [x] Task 1: Research volume grouping implementation (AC: 1, 2, 5, 6)
  - [x] 1.1: Read PRD FR2–FR4, FR6–FR7 for the conceptual volume grouping design
  - [x] 1.2: Walk `pkg/engine/consistency.go` for namespace annotation reading and VolumeGroup formation
  - [x] 1.3: Walk `pkg/engine/discovery.go` for label-to-wave mapping and VM discovery
  - [x] 1.4: Walk `pkg/engine/chunker.go` for how VolumeGroups map into DRGroup chunks
- [x] Task 2: Research validation and constraints (AC: 4, 6)
  - [x] 2.1: Walk `pkg/admission/drplan_validator.go` for field-level validation
  - [x] 2.2: Walk `pkg/controller/drplan/reconciler.go` for runtime enforcement (WaveConflict, NamespaceGroupExceedsThrottle conditions)
- [x] Task 3: Write the documentation page (AC: 1, 2, 3, 4, 5)
  - [x] 3.1: Write `docs/usage/volumes.md` covering namespace-level vs VM-level grouping, annotation usage, label-to-DRGroup mapping, wave constraint, trade-offs
  - [x] 3.2: Add annotated YAML examples showing both grouping strategies
- [x] Task 4: Verify accuracy (AC: 6)
  - [x] 4.1: Verify all grouping rules match the actual code

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR2 (label-driven DRPlan), FR3 (wave label), FR4 (VM exclusivity via label key), FR6 (namespace-level annotation), FR7 (same-wave enforcement)]
- [Source: _bmad-output/planning-artifacts/architecture.md — consistency resolution, VolumeGroup formation]

### Code to Verify Against

- [Source: pkg/engine/consistency.go — reads `soteria.io/consistency-level` namespace annotation; namespace-level groups all VMs in a namespace into single VolumeGroup; VM-level (default) creates per-VM VolumeGroups; detects wave conflicts when namespace-level VMs span multiple waves]
- [Source: pkg/engine/discovery.go — VMDiscoverer partitions VMs into ordered waves by `soteria.io/wave` label]
- [Source: pkg/engine/chunker.go — namespace-level VolumeGroups are indivisible units that cannot split across DRGroup chunks]
- [Source: pkg/admission/drplan_validator.go — field-level validation (maxConcurrentFailovers, site names); cross-resource constraints delegated to controller reconciliation]
- [Source: pkg/controller/drplan/reconciler.go — runtime enforcement of WaveConflict and NamespaceGroupExceedsThrottle via Ready=False status conditions]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — ConsistencyLevel type (`namespace` | `vm`), ConsistencyAnnotation constant (`soteria.io/consistency-level`), VolumeGroupInfo struct]

### Implementation Pattern

- Document both strategies with side-by-side comparison table
- Show annotated YAML: (1) namespace with `soteria.io/consistency-level: namespace` annotation, (2) VMs with `soteria.io/drplan` and `soteria.io/wave` labels
- Explain naming convention: `ns-<namespace>` for namespace-level groups, `vm-<namespace>-<vmname>` for VM-level groups
- Use mkdocs admonition for the wave constraint: namespace-grouped VMs must all be in the same wave or the plan will report WaveConflict
- Explain the pre-flight check: maxConcurrentFailovers must be ≥ largest namespace+wave group

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/usage/volumes.md | MODIFIED | Namespace-level vs VM-level grouping strategies, annotations, constraints, trade-offs (replaced placeholder) |

### Key Constraints

- `soteria.io/consistency-level: namespace` annotation on a Namespace triggers namespace-level grouping
- All VMs in a namespace with namespace-level consistency MUST be in the same wave (validated at reconcile time, reported as WaveConflict condition)
- Namespace-level VolumeGroups are indivisible in chunking — if they exceed maxConcurrentFailovers, execution is rejected (NamespaceGroupExceedsThrottle)
- VM exclusivity is structurally enforced: `soteria.io/drplan` label key can have only one value

### Project Structure Notes

- Consistency resolution: `pkg/engine/consistency.go`
- Admission validation: `pkg/admission/drplan_validator.go` (field-level) + `pkg/controller/drplan/reconciler.go` (cross-resource)
- Types: `pkg/apis/soteria.io/v1alpha1/types.go` — `ConsistencyLevel`, `VolumeGroupInfo`

### References

- [Source: pkg/engine/consistency.go — namespace annotation, wave conflict detection]
- [Source: pkg/engine/chunker.go — indivisible namespace VolumeGroups]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — ConsistencyLevel, ConsistencyAnnotation, VolumeGroupInfo]
- [Source: _bmad-output/planning-artifacts/prd.md — FR6, FR7]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

None — documentation-only story, no code changes.

### Completion Notes List

- ✅ Researched all source files: consistency.go, discovery.go, chunker.go, drplan_validator.go, reconciler.go, types.go
- ✅ Read PRD FR2–FR4, FR6–FR7 for conceptual design context
- ✅ Wrote comprehensive docs/usage/volumes.md replacing the placeholder
- ✅ Documented VM-level grouping (default) with naming convention `vm-<namespace>-<vmname>`
- ✅ Documented namespace-level grouping with annotation `soteria.io/consistency-level: namespace` and naming convention `ns-<namespace>`
- ✅ Added annotated YAML examples for both strategies with mkdocs code annotations
- ✅ Documented label-to-DRGroup formation pipeline with Mermaid flowchart (discovery → wave grouping → consistency resolution → chunking)
- ✅ Documented WaveConflict constraint with admonition and example error output
- ✅ Documented NamespaceGroupExceedsThrottle constraint with admonition and example error output
- ✅ Added comparison table for choosing between strategies
- ✅ Documented VM exclusivity via `soteria.io/drplan` label key (structural enforcement)
- ✅ Added recommended patterns: independent web servers, database cluster, mixed application
- ✅ Documented pre-flight verification with kubectl command
- ✅ Verified all 12 key facts against source code — all match
- ✅ Note: story file inventory said `docs/usage/volume-grouping.md` (NEW) but mkdocs.yml already had `usage/volumes.md` mapped to "Volume Grouping"; wrote to existing file to maintain nav consistency

### Change Log

- 2026-09-05: Initial implementation — replaced placeholder in docs/usage/volumes.md with comprehensive volume grouping documentation

### File List

| File | Action |
|------|--------|
| docs/usage/volumes.md | MODIFIED |
| _bmad-output/implementation-artifacts/17-13-volume-grouping.md | MODIFIED |
| _bmad-output/implementation-artifacts/sprint-status.yaml | MODIFIED |
