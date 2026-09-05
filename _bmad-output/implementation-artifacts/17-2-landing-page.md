# Story 17.2: Landing Page & Index

Status: ready-for-dev

## Story

As a prospective user or evaluator,
I want a compelling landing page that explains what Soteria is, why it exists, and what it can do,
so that I can quickly determine whether Soteria meets my disaster recovery needs.

## Acceptance Criteria

**AC1: Index page exists and renders**
Given `docs/index.md` exists
When the docs site is built
Then the landing page renders as the home page without errors

**AC2: Content covers what and why**
Given the landing page content
When a reader visits the page
Then they find: a concise description of what Soteria is, the problem it solves, and the key capabilities (VM-level DR, wave-based failover, storage driver abstraction, cross-cluster coordination)

**AC3: Architecture summary is accurate**
Given the architecture summary on the landing page
When compared against the actual running codebase
Then the described components (controller, aggregated API server, ScyllaDB, console plugin) match what is actually implemented

**AC4: Link map to all sections**
Given the landing page
When a reader wants to navigate deeper
Then there is a clear link map or navigation guide pointing to Installation, Architecture, Usage, Reference, and Contributing sections

**AC5: Feature list reflects implementation reality**
Given the features listed on the landing page
When compared against the implemented codebase (not the PRD aspirations)
Then only features that are actually implemented and functional are listed as capabilities

## Tasks / Subtasks

- [ ] Task 1: Research actual implemented features (AC: 2, 5)
  - [ ] 1.1: Read PRD executive summary for conceptual framing
  - [ ] 1.2: Walk the actual codebase to identify which features are fully implemented
- [ ] Task 2: Write landing page content (AC: 1, 2, 3, 5)
  - [ ] 2.1: Write `docs/index.md` with: project tagline, what is Soteria, why it exists, key capabilities (verified against code)
  - [ ] 2.2: Add architecture summary diagram or description (cross-cluster topology, major components)
- [ ] Task 3: Add navigation aids (AC: 4)
  - [ ] 3.1: Add link map section pointing to all major doc sections
  - [ ] 3.2: Verify all internal doc links resolve correctly

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — Executive Summary section for conceptual framing ("open-source, Kubernetes-native disaster recovery orchestrator for OpenShift Virtualization")]
- [Source: _bmad-output/project-context.md — project description, technology stack, implementation rules]
- [Source: _bmad-output/planning-artifacts/architecture.md — component architecture for summary diagram]

### Code to Verify Against

- [Source: cmd/soteria/main.go — component wiring: controller-manager + aggregated API server + ScyllaDB + admission webhooks + driver registration]
- [Source: pkg/controller/ — implemented controllers: drplan, drexecution, shadowpv, volumereplication]
- [Source: pkg/engine/ — engine subsystems: failover, reprotect, discovery, executor, state machine, chunker]
- [Source: pkg/drivers/ — implemented drivers: noop (reference), csiextension (production)]
- [Source: pkg/apiserver/ — aggregated API server with ScyllaDB backend]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — CRD types: DRPlan, DRExecution]

### Implementation Pattern

- Landing page should lead with the PRD's elevator pitch: "storage-agnostic DR across heterogeneous storage backends through a single, consistent workflow engine"
- Feature list must be verified against actual code — only list features that exist in `pkg/engine/`, `pkg/drivers/`, `pkg/controller/`
- Architecture summary: two-cluster topology with Soteria controller + aggregated API server + ScyllaDB on each cluster
- Use mkdocs-material admonitions for key callouts
- Mermaid diagram for high-level topology

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/index.md` | MODIFY | Replace placeholder with full landing page content |

### Key Constraints

- Only list features that are actually implemented — not PRD aspirations
- Architecture summary must match `cmd/soteria/main.go` component wiring
- Depends on: 17.1 (docs site must exist)

### Project Structure Notes

- Key components to reference: controller-manager, aggregated API server, ScyllaDB storage, engine (failover + reprotect), storage drivers (noop + csi-extension), admission webhooks, console plugin

### References

- [Source: _bmad-output/planning-artifacts/prd.md — lines 24-30, executive summary]
- [Source: cmd/soteria/main.go — lines 37-65, imports showing all components]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlan and DRExecution CRD definitions]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
