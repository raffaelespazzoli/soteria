# Story 17.8: Architecture Overview

Status: ready-for-dev

## Story

As a platform engineer or contributor,
I want a clear architecture overview with component diagrams and data flow,
so that I understand how Soteria's components fit together across clusters.

## Acceptance Criteria

**AC1: Two-cluster topology diagram is included**
Given the architecture overview page
When a reader views the topology section
Then they see a diagram showing: Soteria controller + aggregated API server + ScyllaDB on each cluster, with cross-cluster ScyllaDB replication and console plugin connection

**AC2: Component diagram is accurate**
Given the component diagram
When compared against `cmd/soteria/main.go` startup and actual component wiring
Then every depicted component exists in the codebase and connections match reality

**AC3: Data flow is documented**
Given the architecture overview
When a reader wants to understand how data flows during a DR operation
Then the data flow between components (controller → engine → driver → storage, controller → ScyllaDB, UI → API server) is clearly described

**AC4: Component responsibilities are described**
Given the architecture overview
When a reader wants to understand what each component does
Then there is a brief description of each component's role: controller, aggregated API server, engine, storage drivers, ScyllaDB, console plugin

**AC5: Diagram matches current project structure**
Given the architecture diagrams
When compared against the actual project directory structure
Then the depicted components correspond to actual packages and directories

## Tasks / Subtasks

- [ ] Task 1: Map codebase to architectural components (AC: 2, 5)
  - [ ] 1.1: Read architecture doc sections 1-4 for the conceptual architecture
  - [ ] 1.2: Walk `cmd/soteria/main.go` to identify actual component startup and wiring
  - [ ] 1.3: Map the current project structure (`pkg/`, `cmd/`) to components
- [ ] Task 2: Write architecture overview (AC: 1, 3, 4)
  - [ ] 2.1: Write `docs/architecture/overview.md` covering: two-cluster topology, component diagram, component responsibilities, data flow
  - [ ] 2.2: Create Mermaid diagrams for: topology (two-cluster view), component diagram, data flow
- [ ] Task 3: Verify accuracy (AC: 2, 5)
  - [ ] 3.1: Verify every component in diagrams exists in the codebase
  - [ ] 3.2: Verify connections between components match actual code paths

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/architecture.md — sections 1-4: project context, component architecture, data flow, cross-site state]
- [Source: _bmad-output/planning-artifacts/prd.md — FR26-FR30 shared state, FR35-FR40 console plugin]

### Code to Verify Against

- [Source: cmd/soteria/main.go — component startup and wiring: scheme registration (kubevirtv1, replicationv1alpha1, soteriav1alpha1), controller-manager setup, aggregated API server, ScyllaDB client, driver registry, admission plugin, controllers (drplan, drexecution, shadowpv, volumereplication)]
- [Source: cmd/console-proxy/main.go — console proxy component]
- [Source: pkg/controller/drplan/ — DRPlan controller (reconciler, health monitoring, disk management)]
- [Source: pkg/controller/drexecution/ — DRExecution controller (reconciler)]
- [Source: pkg/controller/shadowpv/ — ShadowPV controller (publisher + consumer)]
- [Source: pkg/controller/volumereplication/ — VolumeReplication controller]
- [Source: pkg/engine/ — engine subsystems: failover, reprotect, discovery, executor, state machine, chunker, consistency, resume, checkpoint, vm, pvc_resolver]
- [Source: pkg/drivers/ — storage drivers: interface, noop, csiextension, conformance suite, registry]
- [Source: pkg/apiserver/ — aggregated API server with ScyllaDB-backed storage.Interface]
- [Source: pkg/storage/scylladb/ — ScyllaDB storage implementation (14 files)]
- [Source: pkg/admission/ — admission webhooks: drplan_validator, drexecution_validator, vm_validator, plugin]

### Implementation Pattern

- Two-cluster topology: each cluster runs independently with controller-manager + aggregated API server + ScyllaDB DC
- Cross-cluster coordination: ScyllaDB async replication (NetworkTopologyStrategy, LOCAL_ONE) — no direct controller-to-controller communication
- Component wiring in `cmd/soteria/main.go`: ScyllaDB client → aggregated API server → controller-manager → engine → drivers
- Four controllers: DRPlan (label-based VM discovery, health), DRExecution (workflow orchestration), ShadowPV (cross-site PV mirroring), VolumeReplication (VR/VGR lifecycle)
- Engine pipeline: discover VMs → group by wave → chunk by maxConcurrentFailovers → execute via handler (failover or reprotect)
- Console plugin connects to aggregated API server for cross-cluster visibility

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/architecture/overview.md` | NEW | Architecture overview with topology, component, and data flow diagrams |

### Key Constraints

- Every component depicted must map to an actual package in `cmd/` or `pkg/`
- Connections between components must match actual code paths (imports, function calls)
- Aggregated API server is NOT a standard kube-apiserver — it implements `storage.Interface` backed by ScyllaDB
- Depends on: 17.1

### Project Structure Notes

- `cmd/soteria/main.go` — single binary with controller-manager + aggregated API server
- `cmd/console-proxy/main.go` — separate binary for console proxy
- `pkg/controller/` — 4 controllers: drplan, drexecution, shadowpv, volumereplication
- `pkg/engine/` — workflow execution engine (16+ files)
- `pkg/drivers/` — storage driver framework: interface, noop, csiextension, conformance
- `pkg/apiserver/` — aggregated API server options, config, REST storage
- `pkg/storage/scylladb/` — ScyllaDB storage implementation (14 files)
- `pkg/admission/` — admission webhooks (10 files)
- `pkg/apis/soteria.io/` — API type definitions

### References

- [Source: cmd/soteria/main.go — lines 19-66, imports showing all component packages]
- [Source: _bmad-output/planning-artifacts/architecture.md — sections 1-4, conceptual architecture]
- [Source: pkg/engine/doc.go — comprehensive engine documentation (210 lines)]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
