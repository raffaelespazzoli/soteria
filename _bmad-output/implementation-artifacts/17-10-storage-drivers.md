# Story 17.10: Storage Drivers Architecture

Status: ready-for-dev

## Story

As a storage vendor engineer or contributor,
I want to understand the storage driver architecture and extension points,
so that I can evaluate integration options or plan a new driver implementation.

## Acceptance Criteria

**AC1: StorageProvider interface is documented**
Given the storage drivers page
When a reader reviews the interface section
Then they find the complete 7-method StorageProvider interface with method signatures, contracts, and idempotency guarantees

**AC2: Replication model is explained**
Given the storage drivers page
When a reader reviews the replication model
Then they understand the three volume roles (NonReplicated, Source, Target) and the transition rules between them

**AC3: Driver lifecycle is documented**
Given the storage drivers page
When a reader reviews the driver lifecycle
Then they understand: registration via init(), factory pattern, selection from PVC storage class provisioner

**AC4: Reference implementations are described**
Given the storage drivers page
When a reader wants to see examples
Then the no-op driver is described as a reference implementation, and the CSI extension driver as the production implementation

**AC5: Conformance suite is mentioned**
Given the storage drivers page
When a reader wants to validate a driver
Then the conformance suite is described with its purpose and how to run it

**AC6: Documentation matches actual interface**
Given the documented interface
When compared against `pkg/drivers/interface.go`
Then method signatures, types, and error contracts match exactly

## Tasks / Subtasks

- [ ] Task 1: Extract interface and driver patterns from code (AC: 1, 2, 3, 6)
  - [ ] 1.1: Read architecture doc driver patterns section
  - [ ] 1.2: Walk `pkg/drivers/interface.go` for the actual StorageProvider interface (7 methods)
  - [ ] 1.3: Examine driver registration pattern (`init()` + `RegisterDriver`)
- [ ] Task 2: Document reference implementations (AC: 4, 5)
  - [ ] 2.1: Walk `pkg/drivers/noop/driver.go` for the no-op reference implementation
  - [ ] 2.2: Walk `pkg/drivers/csiextension/driver.go` for the CSI extension driver
  - [ ] 2.3: Walk `pkg/drivers/conformance/suite.go` for the conformance suite
- [ ] Task 3: Write the documentation (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 3.1: Write `docs/architecture/storage-drivers.md` covering: StorageProvider 7-method interface, replication model, driver lifecycle, driver selection, no-op driver, CSI extension driver, conformance suite
  - [ ] 3.2: Add interface method table with signatures, return types, and error conditions
  - [ ] 3.3: Verify all method signatures match the actual code

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/architecture.md — driver patterns section, storage abstraction design]
- [Source: _bmad-output/planning-artifacts/prd.md — FR20-FR25 storage abstraction requirements]

### Code to Verify Against

- [Source: pkg/drivers/interface.go — StorageProvider interface: 7 methods (CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, SetSource, StopReplication, ResyncVolume, GetReplicationStatus). Role-based replication model: NonReplicated → Source (SetSource), Source → NonReplicated (StopReplication). Target role is observable but never explicitly set by the engine. Every method must be idempotent.]
- [Source: pkg/drivers/noop/driver.go — no-op reference implementation (4 files: driver.go, driver_test.go, registration_test.go, doc.go)]
- [Source: pkg/drivers/csiextension/driver.go — CSI extension production driver (11 files: driver.go, driver_test.go, status.go, volumehandle.go, helpers.go, constants.go, conformance_test.go, integration_test.go, registration_test.go, doc.go, volumehandle_test.go)]
- [Source: pkg/drivers/conformance/suite.go — conformance test suite: RunConformance(t, provider) runs Lifecycle, Idempotency, ContextCancellation, ErrorConditions subtests]
- [Source: cmd/soteria/main.go — driver registration via blank import: `_ "github.com/soteria-project/soteria/pkg/drivers/all"`]

### Implementation Pattern

- **7-method interface**: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, SetSource, StopReplication, ResyncVolume, GetReplicationStatus
- **Role-based replication model**: two engine-driven transitions routed through NonReplicated: `NonReplicated → Source (SetSource)`, `Source → NonReplicated (StopReplication)`. Target role exists in ReplicationStatus but is never explicitly set by engine.
- **ResyncVolume**: requests storage-layer data sync on secondary site before planned failover promotion (CSI-level concept, doesn't change engine role model)
- **Idempotency**: all methods must be safe to retry after crash/restart without side effects. Drivers act as reconcilers — check actual state before applying changes.
- **Registration**: drivers register via `init()` + `RegisterDriver` pattern, discovered via blank import `_ "pkg/drivers/all"`
- **Driver selection**: executor resolves driver via `Registry.GetDriver(plan.Spec.VolumeReplicationDriver.Type)` — plan-level declared driver, not runtime PVC inspection
- **Error contracts**: drivers must return typed errors from `pkg/drivers/errors.go`, never raw strings
- **Conformance**: any driver must pass `conformance.RunConformance(t, driver)` which tests Lifecycle, Idempotency, ContextCancellation, ErrorConditions

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/architecture/storage-drivers.md` | NEW | Storage driver architecture: interface, replication model, lifecycle, implementations, conformance |

### Key Constraints

- Interface has exactly 7 methods — document all 7 with exact signatures
- Target role is NEVER set by the engine — only observable via GetReplicationStatus
- Volume pairing is an admin precondition — drivers assume paired volumes are pre-configured
- Driver selection is plan-level (VolumeReplicationDriver.Type), not PVC-level
- Conformance suite is mandatory for all driver implementations
- This is the architecture overview; the step-by-step implementation guide is Story 17.22
- Depends on: 17.1

### Project Structure Notes

- `pkg/drivers/interface.go` — StorageProvider interface + types (VolumeGroupSpec, VolumeGroupID, VolumeGroupInfo, ReplicationStatus)
- `pkg/drivers/noop/` — 4 files: no-op reference driver
- `pkg/drivers/csiextension/` — 11 files: CSI extension production driver (uses VolumeReplication/VolumeGroupReplication CRDs)
- `pkg/drivers/conformance/suite.go` — conformance test suite
- `pkg/drivers/all/` — blank import package for registration
- `pkg/drivers/errors.go` — typed error definitions (ErrVolumeGroupNotFound, ErrInvalidTransition, etc.)

### References

- [Source: pkg/drivers/interface.go — lines 22-96, complete StorageProvider interface with doc comments]
- [Source: pkg/drivers/conformance/suite.go — lines 28-35, RunConformance function and test categories]
- [Source: cmd/soteria/main.go — line 37, driver registration import]
- [Source: pkg/engine/doc.go — lines 57-62, driver resolution pattern]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
