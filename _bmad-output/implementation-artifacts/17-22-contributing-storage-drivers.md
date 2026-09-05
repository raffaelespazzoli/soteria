# Story 17.22: Contributing: Writing a Storage Driver

Status: review

## Story

As a storage vendor engineer building a Soteria driver,
I want a step-by-step guide for implementing and validating a new storage driver,
so that I can integrate my storage platform with Soteria without deep knowledge of the orchestrator internals.

## Acceptance Criteria

**AC1: Interface contract is documented**
Given the storage driver guide
When a developer reads the interface section
Then they find the complete StorageProvider interface contract with method-by-method explanation of expected behavior, error handling, and idempotency requirements

**AC2: Step-by-step implementation guide is provided**
Given the storage driver guide
When a developer follows the steps
Then they can scaffold a new driver package, implement the interface, register it, and validate it — in order

**AC3: Registration pattern is documented**
Given the storage driver guide
When a developer wants to register their driver
Then they find the `init()` + registry pattern with a concrete code example

**AC4: Conformance suite is documented**
Given the storage driver guide
When a developer wants to validate their implementation
Then they find a walkthrough of the conformance suite: what it tests, how to run it, how to interpret results

**AC5: Packaging guidance is provided**
Given the storage driver guide
When a developer wants to distribute their driver
Then they find guidance on packaging as a separate Go module

**AC6: Reference implementations are pointed to**
Given the storage driver guide
When a developer wants to see examples
Then they are directed to the fake driver, no-op driver, and CSI extension driver with explanation of what each demonstrates

## Tasks / Subtasks

- [x] Task 1: Research the StorageProvider interface (AC: 1)
  - [x] 1.1: Walk `pkg/drivers/interface.go` for the canonical 7-method interface definition and doc comments
  - [x] 1.2: Walk `pkg/drivers/errors.go` for typed error sentinels (ErrVolumeGroupNotFound, ErrInvalidTransition)
  - [x] 1.3: Walk `pkg/drivers/types.go` (or equivalent) for VolumeGroupSpec, VolumeGroupInfo, VolumeGroupID, ReplicationStatus, VolumeRole, ReplicationHealth
- [x] Task 2: Research the registration and driver framework (AC: 2, 3)
  - [x] 2.1: Walk `pkg/drivers/registry.go` (or equivalent) for RegisterDriver and GetDriver functions
  - [x] 2.2: Walk `pkg/drivers/noop/driver.go` for the init() registration pattern (registers ProvisionerName, PlanDriverName, and fallback)
- [x] Task 3: Research the conformance suite (AC: 4)
  - [x] 3.1: Walk `pkg/drivers/conformance/suite.go` for RunConformance — 4 test categories: Lifecycle, Idempotency, ContextCancellation, ErrorConditions
  - [x] 3.2: Document what each category validates and expected behavior
- [x] Task 4: Research reference implementations (AC: 6)
  - [x] 4.1: Walk `pkg/drivers/noop/driver.go` — minimal implementation: in-memory state, role transitions, fallback driver registration
  - [x] 4.2: Walk `pkg/drivers/fake/driver.go` — programmable test double: On*/Return fluent API, call recording, FIFO reaction matching
  - [x] 4.3: Walk `pkg/drivers/csiextension/` for CSI extension driver (production implementation pattern)
- [x] Task 5: Write the documentation page (AC: 1, 2, 3, 4, 5, 6)
  - [x] 5.1: Write `docs/contributing/storage-drivers.md` covering interface contract, step-by-step guide, registration, conformance suite, packaging, reference implementations
  - [x] 5.2: Add code snippets: interface implementation skeleton, init() registration, conformance suite invocation
  - [x] 5.3: Verify all code snippets compile against the actual interface

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR20 (StorageProvider interface: 7 methods, role-based model), FR23 (no-op driver), NFR19 (interface stability)]
- [Source: _bmad-output/planning-artifacts/architecture.md — driver patterns section]
- [Source: _bmad-output/project-context.md — StorageProvider driver framework, registration, conformance, error patterns]

### Code to Verify Against

- [Source: pkg/drivers/interface.go — StorageProvider interface with 7 methods: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, SetSource, StopReplication, ResyncVolume, GetReplicationStatus. Role model: NonReplicated → Source (SetSource), Source → NonReplicated (StopReplication). Target role observable but never explicitly set. All methods must be idempotent. Drivers return typed errors from pkg/drivers/errors.go.]
- [Source: pkg/drivers/conformance/suite.go — RunConformance(t, provider) runs 4 categories: (1) Lifecycle (Create → SetSource → Resync → GetStatus(Source) → StopReplication → GetStatus(NonReplicated) → Delete → Get(deleted)), (2) Idempotency (every method safe to call twice), (3) ContextCancellation (pre-cancelled context returns error), (4) ErrorConditions (nonexistent ID returns ErrVolumeGroupNotFound)]
- [Source: pkg/drivers/noop/driver.go — No-op reference implementation: in-memory map, sync.RWMutex for thread safety, role state tracking (Source/NonReplicated), init() registers ProvisionerName ("noop.soteria.io"), PlanDriverName ("noop"), and sets fallback driver. Creates volume groups with "noop-<ns>/<name>" ID pattern.]
- [Source: pkg/drivers/fake/driver.go — Programmable fake for unit tests: On*(vgID...).Return(err) / ReturnResult(Response) fluent API, FIFO reaction consumption, call recording (Calls, CallsTo, CallCount, Called), Reset for reuse across sub-tests. Follows k8s <package>fake naming convention.]
- [Source: pkg/drivers/noop/driver.go — Registration pattern: `drivers.RegisterDriver(ProvisionerName, func() drivers.StorageProvider { return shared })` and `drivers.SetFallbackDriver(func() drivers.StorageProvider { return shared })`]

### Implementation Pattern

- Structure as a step-by-step tutorial:
  1. Understand the interface (7 methods, role model, idempotency)
  2. Scaffold your driver package: `pkg/drivers/<vendor>/driver.go`
  3. Implement the interface (method-by-method with contract explanation)
  4. Handle errors (return typed sentinels: ErrVolumeGroupNotFound, ErrInvalidTransition)
  5. Register via init() + RegisterDriver
  6. Run the conformance suite
  7. Package as a Go module
- Include code skeleton:
  ```go
  package mydriver

  import (
      "context"
      "github.com/soteria-project/soteria/pkg/drivers"
  )

  var _ drivers.StorageProvider = (*Driver)(nil)

  type Driver struct { /* ... */ }

  func (d *Driver) CreateVolumeGroup(ctx context.Context, spec drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) { /* ... */ }
  // ... remaining 6 methods

  func init() {
      drivers.RegisterDriver("mydriver.vendor.io", func() drivers.StorageProvider {
          return &Driver{}
      })
  }
  ```
- Include conformance invocation:
  ```go
  func TestConformance(t *testing.T) {
      conformance.RunConformance(t, mydriver.New())
  }
  ```

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/contributing/storage-drivers.md | NEW | Step-by-step storage driver implementation, registration, conformance, packaging guide |

### Key Constraints

- All 7 methods MUST be idempotent — safe to retry after crash/restart
- Drivers must return typed errors from `pkg/drivers/errors.go`, never raw strings
- All drivers MUST pass the conformance suite at `pkg/drivers/conformance/suite.go`
- Volume pairing is an admin precondition — driver assumes paired volumes are pre-configured
- Drivers handle unreachable peers internally — orchestrator does not pass force flags
- Interface stability: breaking changes require a new API version with deprecation (NFR19)

### Project Structure Notes

- Interface: `pkg/drivers/interface.go`
- Conformance: `pkg/drivers/conformance/suite.go`
- No-op driver: `pkg/drivers/noop/driver.go` (minimal reference)
- Fake driver: `pkg/drivers/fake/driver.go` (test double)
- CSI extension: `pkg/drivers/csiextension/` (production reference)
- External authors import `pkg/`, never `internal/`

### References

- [Source: pkg/drivers/interface.go — StorageProvider interface definition]
- [Source: pkg/drivers/conformance/suite.go — conformance test suite (4 categories)]
- [Source: pkg/drivers/noop/driver.go — no-op reference implementation + registration pattern]
- [Source: pkg/drivers/fake/driver.go — programmable fake driver for unit tests]
- [Source: _bmad-output/project-context.md — driver framework, error patterns, conformance requirement]
- [Source: _bmad-output/planning-artifacts/prd.md — FR20, FR23, NFR19]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

None — documentation-only story with no code changes.

### Completion Notes List

- Researched all source files: interface.go, errors.go, types.go, registry.go, noop/driver.go, fake/driver.go, csiextension/ (driver.go, constants.go, status.go, helpers.go), conformance/suite.go
- Wrote comprehensive 7-step guide covering: interface contract (all 7 methods with method-by-method contract), role model, key types, error handling, scaffolding, implementation skeleton, registration pattern, conformance suite walkthrough (all 4 categories), packaging as Go module, reference implementations (noop, fake, CSI extension)
- All code snippets verified to compile against the actual interface via temporary build verification
- All existing tests pass with no regressions
- AC1 ✅: Complete StorageProvider interface contract with method-by-method explanation
- AC2 ✅: Step-by-step implementation guide (Steps 1–7)
- AC3 ✅: init() + RegisterDriver pattern with concrete code example
- AC4 ✅: Conformance suite walkthrough with 4 test categories explained
- AC5 ✅: Packaging guidance as separate Go module
- AC6 ✅: Reference implementations (noop, fake, CSI extension) with explanation

### File List

| File | Action |
|------|--------|
| docs/contributing/storage-drivers.md | MODIFIED | Replaced placeholder with comprehensive storage driver implementation guide |
| _bmad-output/implementation-artifacts/17-22-contributing-storage-drivers.md | MODIFIED | Updated task checkboxes, status, dev agent record |
