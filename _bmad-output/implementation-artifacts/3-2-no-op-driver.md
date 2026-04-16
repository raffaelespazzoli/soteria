# Story 3.2: No-Op Driver

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want a no-op driver that implements the full StorageProvider interface without performing actual storage operations,
So that I can develop, test, and run CI without storage infrastructure from Day 1.

## Acceptance Criteria

1. **AC1 — Full interface implementation:** `pkg/drivers/noop/driver.go` implements all 9 `StorageProvider` methods. Every method returns success without performing actual storage operations (FR23). Compile-time interface check: `var _ drivers.StorageProvider = (*Driver)(nil)`.

2. **AC2 — Stateful volume group tracking:** `CreateVolumeGroup` generates a synthetic `VolumeGroupID` (e.g., `"noop-<uuid>"`) and stores it in an in-memory map. Subsequent `GetVolumeGroup` calls with that ID return the synthetic group. `DeleteVolumeGroup` removes it from the map. `GetVolumeGroup` for an unknown ID returns `drivers.ErrVolumeGroupNotFound`.

3. **AC3 — Replication state machine:** The driver tracks per-volume-group replication state. `EnableReplication` sets state to `ReplicationActive`. `DisableReplication` sets state to `ReplicationStopped`. `PromoteVolume` sets state to `ReplicationPromoted`. `DemoteVolume` sets state to `ReplicationDemoted`. `ResyncVolume` sets state to `ReplicationResyncing` (then `ReplicationActive`). `GetReplicationInfo` returns the current state plus a synthetic RPO (`LastSyncTime = time.Now()`, `EstimatedRPO = 0`).

4. **AC4 — Structured logging:** Every method logs at `V(1)` level using `log.FromContext(ctx)` with the operation name and relevant parameters (e.g., `log.FromContext(ctx).V(1).Info("No-op: created volume group", "volumeGroupID", vgID)`).

5. **AC5 — Idempotency:** All 9 methods are idempotent — calling the same operation twice with the same arguments produces the same result without error. `CreateVolumeGroup` with an already-existing group returns the existing ID without error. `DeleteVolumeGroup` on a missing group returns nil. `EnableReplication` on an already-active group is a no-op.

6. **AC6 — init() registration:** The driver registers itself with the global registry via an `init()` function under provisioner name `noop.soteria.io`. Importing `pkg/drivers/noop` is sufficient to register the driver.

7. **AC7 — Thread safety:** All internal state (volume group map, replication states) is protected by `sync.RWMutex`. Concurrent calls from multiple goroutines are safe.

8. **AC8 — Unit tests:** All methods have `_test.go` coverage — lifecycle (create → get → delete), replication state transitions, idempotency, unknown volume group errors, concurrent access, and init registration.

## Tasks / Subtasks

- [ ] Task 1: Implement the no-op driver struct and constructor (AC: #1, #2, #7)
  - [ ] 1.1 In `pkg/drivers/noop/driver.go`, define `Driver` struct with `sync.RWMutex`, `volumeGroups map[drivers.VolumeGroupID]*volumeGroupState` (stores VolumeGroupInfo + ReplicationState)
  - [ ] 1.2 Define unexported `volumeGroupState` struct holding `drivers.VolumeGroupInfo`, `drivers.ReplicationState`, and `createdAt time.Time`
  - [ ] 1.3 Add `New() *Driver` constructor that initializes the map
  - [ ] 1.4 Add compile-time interface check: `var _ drivers.StorageProvider = (*Driver)(nil)`

- [ ] Task 2: Implement volume group lifecycle methods (AC: #1, #2, #4, #5)
  - [ ] 2.1 `CreateVolumeGroup(ctx, spec)` — generate UUID-based `VolumeGroupID` (`"noop-" + uuid.NewString()`), store in map, log at V(1), return ID. For idempotency: if a second create arrives with the same spec, generate a new ID (the noop driver does not need persistent deduplication — idempotency means repeated calls succeed without error, not that they return the same ID)
  - [ ] 2.2 `DeleteVolumeGroup(ctx, vgID)` — remove from map, log at V(1). Return nil if not found (idempotent)
  - [ ] 2.3 `GetVolumeGroup(ctx, vgID)` — look up in map, return info. Return `drivers.ErrVolumeGroupNotFound` if missing

- [ ] Task 3: Implement replication methods (AC: #1, #3, #4, #5)
  - [ ] 3.1 `EnableReplication(ctx, vgID)` — set state to `ReplicationActive`, log at V(1). Return `drivers.ErrVolumeGroupNotFound` if vgID unknown. Idempotent if already active
  - [ ] 3.2 `DisableReplication(ctx, vgID)` — set state to `ReplicationStopped`, log at V(1). Return `drivers.ErrVolumeGroupNotFound` if vgID unknown. Idempotent if already stopped
  - [ ] 3.3 `GetReplicationInfo(ctx, vgID)` — return current `ReplicationInfo` with state from map, `LastSyncTime = now`, `EstimatedRPO = 0`. Return `drivers.ErrVolumeGroupNotFound` if vgID unknown

- [ ] Task 4: Implement failover/failback methods (AC: #1, #3, #4, #5)
  - [ ] 4.1 `PromoteVolume(ctx, vgID, opts)` — set state to `ReplicationPromoted`, log at V(1) including `opts.Force`. Return `drivers.ErrVolumeGroupNotFound` if vgID unknown. Idempotent if already promoted
  - [ ] 4.2 `DemoteVolume(ctx, vgID, opts)` — set state to `ReplicationDemoted`, log at V(1). Return `drivers.ErrVolumeGroupNotFound` if vgID unknown. Idempotent if already demoted
  - [ ] 4.3 `ResyncVolume(ctx, vgID)` — set state to `ReplicationActive` (noop immediately completes resync), log at V(1). Return `drivers.ErrVolumeGroupNotFound` if vgID unknown

- [ ] Task 5: Implement init() registration (AC: #6)
  - [ ] 5.1 Add `init()` function that calls `drivers.RegisterDriver("noop.soteria.io", func() drivers.StorageProvider { return New() })`
  - [ ] 5.2 Export the provisioner name as a constant: `const ProvisionerName = "noop.soteria.io"`

- [ ] Task 6: Update package documentation (AC: #1)
  - [ ] 6.1 Update `pkg/drivers/noop/doc.go` with a comprehensive package doc comment: purpose (dev/test/CI without storage), stateful behavior (tracks volume groups and replication states in-memory), registration provisioner name, reference to StorageProvider contract

- [ ] Task 7: Unit tests (AC: #8)
  - [ ] 7.1 In `pkg/drivers/noop/driver_test.go`:
    - [ ] 7.1.1 `TestDriver_CreateAndGetVolumeGroup` — create a group, verify GetVolumeGroup returns it with correct info
    - [ ] 7.1.2 `TestDriver_DeleteVolumeGroup` — create, delete, verify GetVolumeGroup returns `ErrVolumeGroupNotFound`
    - [ ] 7.1.3 `TestDriver_DeleteVolumeGroup_NotFound` — delete nonexistent, verify no error (idempotent)
    - [ ] 7.1.4 `TestDriver_GetVolumeGroup_NotFound` — verify `ErrVolumeGroupNotFound` for unknown ID
    - [ ] 7.1.5 `TestDriver_ReplicationLifecycle` — create → enable → get (active) → promote → get (promoted) → demote → get (demoted) → resync → get (active) → disable → get (stopped)
    - [ ] 7.1.6 `TestDriver_PromoteVolume_Force` — verify force promote sets promoted state and logs force flag
    - [ ] 7.1.7 `TestDriver_Idempotency_Create` — create two groups, verify both succeed without error
    - [ ] 7.1.8 `TestDriver_Idempotency_Enable` — enable replication twice, no error
    - [ ] 7.1.9 `TestDriver_Idempotency_Promote` — promote twice, no error
    - [ ] 7.1.10 `TestDriver_UnknownVolumeGroup_ReplicationMethods` — verify ErrVolumeGroupNotFound from Enable/Disable/Promote/Demote/Resync/GetReplicationInfo on unknown vgID
    - [ ] 7.1.11 `TestDriver_ConcurrentAccess` — concurrent create + get + delete from multiple goroutines with `sync.WaitGroup`, verify no races (run with `-race`)
    - [ ] 7.1.12 `TestDriver_CompileTimeInterfaceCheck` — `var _ drivers.StorageProvider = (*Driver)(nil)` (compile-time only, but good to be explicit in test file)
  - [ ] 7.2 In `pkg/drivers/noop/registration_test.go`:
    - [ ] 7.2.1 `TestNoopDriver_Registration` — import the noop package side-effect, verify `drivers.GetDriver("noop.soteria.io")` returns a valid StorageProvider

- [ ] Task 8: Verify build and tests (AC: #8)
  - [ ] 8.1 Run `go test -race ./pkg/drivers/noop/...` — all tests pass with race detector
  - [ ] 8.2 Run `make test` — all unit tests pass (new + existing)
  - [ ] 8.3 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 8.4 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 2 of Epic 3 (Storage Driver Framework & Reference Implementations). It builds directly on Story 3.1's interface, types, errors, and registry. The no-op driver serves three critical purposes:

1. **Development enablement** — developers can run the full orchestrator locally without any storage infrastructure. `make dev-cluster` uses the no-op driver (FR23).
2. **CI pipeline** — all integration and E2E tests can run without storage arrays. The no-op driver enables testing workflow engine logic in isolation from storage.
3. **Reference implementation** — storage vendor engineers (Journey 4: Priya) read the no-op driver source to understand the StorageProvider interface contract before writing their own driver.

### Dependency on Story 3.1

This story depends entirely on types, interface, errors, and registry from Story 3.1. The following must exist before implementation:

| From Story 3.1 | File | Used By |
|---|---|---|
| `StorageProvider` interface (9 methods) | `pkg/drivers/interface.go` | Compile-time check, all method implementations |
| `VolumeGroupID`, `VolumeGroupInfo`, `VolumeGroupSpec` | `pkg/drivers/types.go` | Method parameters and return types |
| `ReplicationInfo`, `ReplicationState` constants | `pkg/drivers/types.go` | GetReplicationInfo return, state tracking |
| `PromoteOptions`, `DemoteOptions` | `pkg/drivers/types.go` | PromoteVolume/DemoteVolume parameters |
| `ErrVolumeGroupNotFound`, `ErrVolumeNotFound` | `pkg/drivers/errors.go` | Error returns for unknown volume groups |
| `RegisterDriver`, `GetDriver` | `pkg/drivers/registry.go` | init() registration, test verification |
| `DriverFactory` type | `pkg/drivers/registry.go` | Factory function in init() |

### Stateful Design

The no-op driver is intentionally stateful (in-memory) rather than purely returning empty values. This is critical because:

- The conformance test suite (Story 3.4) will verify that `CreateVolumeGroup` → `GetVolumeGroup` returns the created group. A stateless driver would fail conformance.
- Replication state transitions must be tracked so `GetReplicationInfo` reflects operations performed. The workflow engine and Console dashboard read replication state to determine available actions.
- The no-op driver proves the interface contract is implementable end-to-end, including state management.

State is stored in a `map[VolumeGroupID]*volumeGroupState` protected by `sync.RWMutex`. State is lost on process restart — this is acceptable because the no-op driver has no persistent backend by design.

### Idempotency Contract

All 9 methods must be idempotent per the architecture. Specific no-op behavior:

| Method | Idempotent Behavior |
|---|---|
| `CreateVolumeGroup` | Always succeeds — repeated calls create new groups (no deduplication needed since noop has no real storage) |
| `DeleteVolumeGroup` | Missing ID → return nil (not an error) |
| `GetVolumeGroup` | Pure read — naturally idempotent |
| `EnableReplication` | Already active → no-op, no error |
| `DisableReplication` | Already stopped → no-op, no error |
| `PromoteVolume` | Already promoted → no-op, no error |
| `DemoteVolume` | Already demoted → no-op, no error |
| `ResyncVolume` | Already active → no-op, no error |
| `GetReplicationInfo` | Pure read — naturally idempotent |

### UUID Generation

Use `github.com/google/uuid` (already in go.mod as a transitive dependency) for generating synthetic volume group IDs. Prefix with `"noop-"` to make the source immediately identifiable in logs and debugging.

### Logging Pattern

Follow the project's structured logging convention from `project-context.md`:

```go
log := log.FromContext(ctx)
log.V(1).Info("No-op: created volume group", "volumeGroupID", vgID)
log.V(1).Info("No-op: promoting volume", "volumeGroupID", vgID, "force", opts.Force)
```

- Use `V(1)` for all no-op operations (normal ops level, not state transitions)
- Start log messages with `"No-op: "` prefix for easy filtering
- Always include `"volumeGroupID"` key for traceability

### Registration Pattern

The no-op driver registers via `init()` which runs when the package is imported. In production, `cmd/soteria/main.go` will add a blank import `_ "pkg/drivers/noop"` to activate registration. This is the standard Go driver registration pattern (mirrors `database/sql` drivers, image codecs, etc.).

```go
const ProvisionerName = "noop.soteria.io"

func init() {
    drivers.RegisterDriver(ProvisionerName, func() drivers.StorageProvider {
        return New()
    })
}
```

### Existing Code to Preserve

| File | Contents | Action |
|------|----------|--------|
| `pkg/drivers/noop/doc.go` | Package stub with license header | **Modify** — update doc comment only |
| `pkg/drivers/credentials.go` | CredentialSource, CredentialResolver interface | Preserve — do not modify |
| `pkg/drivers/credentials_secret.go` | SecretCredentialResolver, credential errors | Preserve — do not modify |
| `pkg/drivers/credentials_test.go` | Credential resolver tests | Preserve — do not modify |
| `pkg/drivers/fake/doc.go` | Package stub — Story 3.3 target | Preserve — do not add code |
| `pkg/drivers/conformance/doc.go` | Package stub — Story 3.4 target | Preserve — do not add code |
| Story 3.1 files (interface.go, types.go, errors.go, registry.go) | Story 3.1 deliverables | Preserve — do not modify |

### Files NOT to Modify

- `pkg/drivers/interface.go` — Story 3.1 deliverable
- `pkg/drivers/types.go` — Story 3.1 deliverable
- `pkg/drivers/errors.go` — Story 3.1 deliverable
- `pkg/drivers/registry.go` — Story 3.1 deliverable
- `pkg/drivers/credentials.go` — existing credential types
- `pkg/drivers/credentials_secret.go` — existing credential resolver
- `pkg/drivers/credentials_test.go` — existing tests
- `pkg/drivers/fake/doc.go` — Story 3.3 target
- `pkg/drivers/conformance/doc.go` — Story 3.4 target
- `cmd/soteria/main.go` — no wiring changes needed in this story (blank import can be added later)
- Any controller, engine, admission, or storage code

### File Structure

| File | Purpose | New/Modified |
|------|---------|-------------|
| `pkg/drivers/noop/driver.go` | `Driver` struct, constructor, all 9 `StorageProvider` method implementations, `init()` registration | **New** |
| `pkg/drivers/noop/doc.go` | Updated package documentation | **Modified** |
| `pkg/drivers/noop/driver_test.go` | Unit tests: lifecycle, replication state, idempotency, concurrency | **New** |
| `pkg/drivers/noop/registration_test.go` | Registration verification test | **New** |

### Code Patterns to Follow

**Compile-time interface check** (from `pkg/drivers/credentials_test.go`):

```go
var _ drivers.StorageProvider = (*Driver)(nil)
```

**Table-driven tests** (from `pkg/drivers/credentials_test.go`):

```go
tests := []struct {
    name    string
    // ...
    wantErr error
}{
    // ...
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

**Error wrapping** (project convention):

```go
fmt.Errorf("getting volume group %s: %w", vgID, drivers.ErrVolumeGroupNotFound)
```

**License header** (from existing files):

```go
/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 ...
*/
```

### Build Commands

```bash
go test -race ./pkg/drivers/noop/...   # No-op driver tests with race detector
make test                               # All unit tests (new + existing)
make lint-fix                           # Auto-fix code style
make lint                               # Verify lint passes
make build                              # Verify compilation
```

### Project Structure Notes

- Aligned with architecture: `pkg/drivers/noop/driver.go` exactly as specified in the project directory structure
- The no-op driver is in `pkg/` (not `internal/`) because external driver authors may reference it as a pattern
- Keep all driver logic in a single `driver.go` file — the no-op driver is simple enough that splitting into multiple files adds unnecessary complexity
- The `init()` registration function lives in `driver.go` alongside the implementation

### Previous Story Intelligence

Story 3.1 establishes the foundational types and patterns that this story must follow:
- Domain types use strong typing (not raw strings) — use `drivers.VolumeGroupID`, not `string`
- Error returns use sentinel errors from `pkg/drivers/errors.go` — never return raw error strings
- The registry uses `DriverFactory` function type — the no-op driver provides a factory that calls `New()`
- Test patterns include compile-time interface checks, table-driven tests, and `errors.Is` assertions
- The `StorageClassLister` interface in the registry is not needed by the no-op driver — it only needs to call `RegisterDriver`

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.2] — Full acceptance criteria and BDD scenarios
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 3] — Epic overview: FR20, FR21, FR23–FR25
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — init() + registry, typed errors, context, idempotency
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/drivers/noop/driver.go`
- [Source: _bmad-output/planning-artifacts/architecture.md#Structure Patterns] — Driver packages at `pkg/drivers/<vendor>/`, mocks at `pkg/drivers/fake/`
- [Source: _bmad-output/planning-artifacts/prd.md#FR23] — No-op driver: full interface, no actual storage operations, dev/test/CI
- [Source: _bmad-output/planning-artifacts/prd.md#Journey 4] — Priya reads no-op source to understand the interface contract
- [Source: _bmad-output/planning-artifacts/prd.md#NFR19] — Interface stability for external driver development
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — 9 methods, idempotency, context, init() registration
- [Source: _bmad-output/project-context.md#Testing Rules] — Driver conformance, envtest over fake client, test naming
- [Source: _bmad-output/implementation-artifacts/3-1-storageprovider-interface-driver-registry.md] — Story 3.1 types, interface, errors, registry definitions
- [Source: pkg/drivers/credentials_test.go] — Test pattern reference: compile-time check, table-driven tests, errors.Is
- [Source: pkg/drivers/noop/doc.go] — Existing package stub to update

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
