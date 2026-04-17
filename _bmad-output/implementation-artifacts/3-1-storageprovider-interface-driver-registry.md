# Story 3.1: StorageProvider Interface & Driver Registry

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a storage vendor engineer,
I want a clearly defined 7-method Go interface with typed errors and an automatic driver registry,
So that I know exactly what to implement and how drivers are discovered at runtime.

## Acceptance Criteria

1. **AC1 — StorageProvider interface:** `pkg/drivers/interface.go` declares the `StorageProvider` interface with exactly 7 methods: `CreateVolumeGroup`, `DeleteVolumeGroup`, `GetVolumeGroup`, `SetSource`, `SetTarget`, `StopReplication`, `GetReplicationStatus` (FR20). The replication model uses three volume roles (NonReplicated, Source, Target) with all transitions routed through NonReplicated. Every method accepts `context.Context` as its first parameter. Method signatures use domain types (not raw strings) for volume group IDs, replication roles, etc. The interface has godoc comments explaining each method's contract, idempotency guarantee, and expected error conditions.

2. **AC2 — Typed errors:** `pkg/drivers/errors.go` defines sentinel errors: `ErrVolumeNotFound`, `ErrVolumeGroupNotFound`, `ErrReplicationNotReady`, `ErrInvalidTransition`, `ErrDriverNotFound`. All use the `Err` prefix per Go convention. Driver implementations must return these typed errors — never raw errors.

3. **AC3 — Driver registry:** `pkg/drivers/registry.go` implements a global registry keyed by storage class provisioner name. `RegisterDriver(provisionerName string, factory DriverFactory)` is the registration API. Drivers register via `init()` functions.

4. **AC4 — Driver selection from PVC storage class:** Given a VM with PVCs using a specific storage class, the registry inspects the PVC's storage class provisioner field and returns the registered driver for that provisioner. Returns `ErrDriverNotFound` if no driver is registered for the provisioner (FR21).

5. **AC5 — Heterogeneous storage:** Given a DRPlan with VMs using different storage backends, each VM is handled by the appropriate driver selected from the registry (FR25).

6. **AC6 — Unit tests:** All new exported functions and types have `_test.go` coverage — registry operations, error type assertions, driver selection logic, thread safety.

## Tasks / Subtasks

- [x] Task 1: Define domain types for interface method parameters and return values (AC: #1)
  - [x] 1.1 In `pkg/drivers/types.go`, define `VolumeGroupID` (string alias or newtype), `VolumeGroupInfo`, `VolumeRole` enum (string constants: `RoleNonReplicated`, `RoleSource`, `RoleTarget`), `ReplicationHealth` enum (string constants: `HealthHealthy`, `HealthDegraded`, `HealthSyncing`, `HealthUnknown`), and `ReplicationStatus` struct (Role, Health, LastSyncTime, EstimatedRPO)
  - [x] 1.2 Define `SetSourceOptions`, `SetTargetOptions`, and `StopReplicationOptions` structs each with a `Force bool` field (needed for disaster failover vs planned migration)
  - [x] 1.3 Define `VolumeGroupSpec` struct for `CreateVolumeGroup` input (PVC references, namespace, labels)
  - [x] 1.4 Add godoc on every exported type explaining its purpose and relationship to the DR lifecycle

- [x] Task 2: Define the StorageProvider interface (AC: #1)
  - [x] 2.1 In `pkg/drivers/interface.go`, define the `StorageProvider` interface with the 7 methods using the domain types from Task 1
  - [x] 2.2 Each method signature: `MethodName(ctx context.Context, <domain-typed params>) (<return types>, error)`
  - [x] 2.3 Add comprehensive godoc on the interface itself explaining the contract: all methods must be idempotent, all accept context for cancellation/timeout, all return typed errors from `errors.go`
  - [x] 2.4 Add godoc on each method explaining: purpose, idempotency guarantee, expected error conditions, relationship to DR lifecycle

- [x] Task 3: Define typed errors (AC: #2)
  - [x] 3.1 In `pkg/drivers/errors.go`, add new sentinel errors: `ErrVolumeNotFound`, `ErrVolumeGroupNotFound`, `ErrReplicationNotReady`, `ErrInvalidTransition`, `ErrDriverNotFound`
  - [x] 3.2 Use `errors.New()` for sentinel errors, following the existing pattern in `credentials_secret.go`
  - [x] 3.3 Do NOT remove or modify existing credential-related errors (`ErrVaultNotImplemented`, `ErrNoCredentialSource`, `ErrAmbiguousSource`, `ErrSecretNotFound`, `ErrSecretKeyNotFound`) — they remain in `credentials_secret.go`

- [x] Task 4: Define DriverFactory type and implement the registry (AC: #3, #4, #5)
  - [x] 4.1 In `pkg/drivers/registry.go`, define `DriverFactory func() StorageProvider`
  - [x] 4.2 Implement a package-level `Registry` struct with a `sync.RWMutex`-protected `map[string]DriverFactory` (keyed by provisioner name)
  - [x] 4.3 Implement `RegisterDriver(provisionerName string, factory DriverFactory)` — panics on duplicate registration (fail-fast at startup, same pattern as `prometheus.MustRegister`)
  - [x] 4.4 Implement `GetDriver(provisionerName string) (StorageProvider, error)` — returns `ErrDriverNotFound` if not registered
  - [x] 4.5 Implement `GetDriverForPVC(ctx context.Context, storageClassName string, scLister StorageClassLister) (StorageProvider, error)` — resolves PVC storage class → provisioner → driver. `StorageClassLister` is a small interface: `GetProvisioner(ctx context.Context, scName string) (string, error)` to keep the registry testable without a real k8s client
  - [x] 4.6 Implement `ListRegistered() []string` — returns sorted list of registered provisioner names (for diagnostics/logging)
  - [x] 4.7 Expose a package-level `DefaultRegistry` instance plus `RegisterDriver`, `GetDriver`, `GetDriverForPVC`, `ListRegistered` functions that delegate to `DefaultRegistry` (mirrors `http.DefaultServeMux` pattern)
  - [x] 4.8 Add a `ResetForTesting()` function (test-only) that clears the registry — enables test isolation

- [x] Task 5: Unit tests (AC: #6)
  - [x] 5.1 In `pkg/drivers/interface_test.go`, add compile-time interface check pattern (`var _ StorageProvider = (*mockProvider)(nil)`) with a minimal mock that satisfies the interface
  - [x] 5.2 In `pkg/drivers/errors_test.go`, add `errors.Is` assertions for all sentinel errors
  - [x] 5.3 In `pkg/drivers/registry_test.go`:
    - [x] 5.3.1 `TestRegistry_RegisterAndGet` — register a driver, retrieve it by provisioner name, verify it returns the correct driver
    - [x] 5.3.2 `TestRegistry_GetDriver_NotFound` — verify `ErrDriverNotFound` returned for unregistered provisioner
    - [x] 5.3.3 `TestRegistry_RegisterDriver_Duplicate_Panics` — verify duplicate registration panics
    - [x] 5.3.4 `TestRegistry_GetDriverForPVC` — mock `StorageClassLister`, verify PVC → provisioner → driver resolution
    - [x] 5.3.5 `TestRegistry_GetDriverForPVC_UnknownStorageClass` — verify error on unknown SC
    - [x] 5.3.6 `TestRegistry_ListRegistered` — register multiple drivers, verify sorted list
    - [x] 5.3.7 `TestRegistry_ConcurrentAccess` — concurrent register + get from multiple goroutines with `sync.WaitGroup`
    - [x] 5.3.8 `TestRegistry_ResetForTesting` — register, reset, verify empty
  - [x] 5.4 In `pkg/drivers/types_test.go`, verify `VolumeRole` and `ReplicationHealth` string constants have expected values

- [x] Task 6: Update `pkg/drivers/doc.go` (AC: #1)
  - [x] 6.1 Update the package doc comment to describe: StorageProvider interface (7 methods), driver registry (init-based registration), typed errors, credential resolution, and how external vendors import `pkg/drivers/`

- [x] Task 7: Verify build and tests (AC: #6)
  - [x] 7.1 Run `make test` — all unit tests pass (new + existing)
  - [x] 7.2 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [x] 7.3 Run `make build` — compiles cleanly

### Review Findings

- [x] [Review][Patch] Missing package-level `ResetForTesting` helper [`pkg/drivers/registry.go:111`] — added
- [x] [Review][Patch] Package-level registry entry points are untested [`pkg/drivers/registry.go:123`] — `TestDefaultRegistry_PackageLevelFunctions` added
- [x] [Review][Patch] `GetDriverForPVC` panics on nil `StorageClassLister` [`pkg/drivers/registry.go:85`] — nil guard added
- [x] [Review][Patch] `RegisterDriver` accepts invalid registrations (`""` provisioner or nil factory) [`pkg/drivers/registry.go:58`] — panic guards added

## Dev Notes

### Architecture Context

This is Story 1 of Epic 3 (Storage Driver Framework & Reference Implementations). It establishes the foundational types, interface, and registry that all subsequent stories (3.2 no-op driver, 3.3 fake driver, 3.4 conformance suite) build upon. The `StorageProvider` interface is one of the most architecturally significant contracts in the project — it's the boundary between the orchestrator engine and vendor-specific storage backends (FR20). The replication model uses three volume roles (NonReplicated, Source, Target) with all transitions routed through NonReplicated. External storage vendor engineers will import `pkg/drivers/` to implement this interface (NFR19: interface stability).

### Existing Code to Preserve

The `pkg/drivers/` package already contains production code that MUST NOT be modified:

| File | Contents | Action |
|------|----------|--------|
| `credentials.go` | `CredentialSource`, `SecretRef`, `VaultRef`, `CredentialResolver` interface | Preserve — complements StorageProvider |
| `credentials_secret.go` | `SecretCredentialResolver`, credential-related errors (`ErrVaultNotImplemented`, `ErrNoCredentialSource`, `ErrAmbiguousSource`, `ErrSecretNotFound`, `ErrSecretKeyNotFound`) | Preserve — do not move these errors |
| `credentials_test.go` | Credential resolver unit tests | Preserve |
| `noop/doc.go` | Package stub — Story 3.2 target | Preserve — do not add code here |
| `fake/doc.go` | Package stub — Story 3.3 target | Preserve — do not add code here |
| `conformance/doc.go` | Package stub — Story 3.4 target | Preserve — do not add code here |

### Relationship to Preflight's StorageBackendResolver

`internal/preflight/storage.go` already implements `StorageBackendResolver` / `TypedStorageBackendResolver` which resolves PVC storage class → driver name string for preflight reporting. This story's registry is the runtime complement: it resolves provisioner name → actual `StorageProvider` instance for execution. The preflight resolver and the driver registry will eventually share the same PVC → provisioner lookup, but for now they remain separate — preflight uses a static `StorageClassDriverMap` config, while the registry discovers drivers via `init()` registration. Do NOT refactor preflight in this story.

### Interface Design Constraints

- **7 methods exactly** — defined in FR20, epics, and architecture. Do not add extra methods.
- **All methods idempotent** — safe to retry after crash/restart. Document idempotency in godoc.
- **`context.Context` first parameter** — enables timeout/cancellation from the workflow engine.
- **Domain types, not raw strings** — use `VolumeGroupID`, `ReplicationStatus`, etc.
- **Return typed errors** — from `pkg/drivers/errors.go`, never raw error strings.
- **Force flag** for `SetSource`, `SetTarget`, `StopReplication` — disaster failover proceeds even if the peer is unreachable; planned migration does not.
- **NFR19** — this interface must be stable enough for external driver development. Breaking changes require a new API version with a deprecation period.

### Registry Design Constraints

- **`init()` + factory pattern** — drivers register at import time, instantiated on first use.
- **Keyed by provisioner name** — the CSI driver name from the StorageClass `.provisioner` field (e.g., `rook-ceph.rbd.csi.ceph.com`, `csi-powerstore.dellemc.com`).
- **No StorageProviderConfig CRD** — driver selection is implicit from PVC storage class (architectural constraint from PRD).
- **Thread-safe** — multiple goroutines may read the registry concurrently during reconciliation.
- **Panic on duplicate** — fail-fast at startup rather than silent override. Same pattern as `prometheus.MustRegister`.

### Code Patterns to Follow

**Interface definition pattern** (from existing codebase):

```go
// In pkg/drivers/interface.go
type StorageProvider interface {
    // Method documentation with idempotency guarantee
    MethodName(ctx context.Context, params...) (result, error)
}
```

**Compile-time interface check** (from `internal/preflight/storage.go`):

```go
var _ StorageProvider = (*ConcreteDriver)(nil)
```

**Error definition pattern** (from `pkg/drivers/credentials_secret.go`):

```go
var (
    ErrVolumeNotFound = errors.New("volume not found")
)
```

**Error wrapping** (project convention):

```go
fmt.Errorf("creating volume group %s: %w", vgID, err)
```

**Logging** (from `pkg/controller/drplan/reconciler.go`):

```go
log := log.FromContext(ctx)
log.Info("Registered storage driver", "provisioner", name)
log.V(1).Info("Looking up driver for provisioner", "provisioner", name)
```

### File Structure

| File | Purpose | New/Modified |
|------|---------|-------------|
| `pkg/drivers/types.go` | Domain types: `VolumeGroupID`, `VolumeGroupInfo`, `VolumeRole`, `ReplicationHealth`, `ReplicationStatus`, `SetSourceOptions`, `SetTargetOptions`, `StopReplicationOptions`, `VolumeGroupSpec` | **New** |
| `pkg/drivers/interface.go` | `StorageProvider` interface (7 methods) | **New** (replaces stub doc) |
| `pkg/drivers/errors.go` | Storage driver sentinel errors | **New** |
| `pkg/drivers/registry.go` | `Registry` struct, `DefaultRegistry`, `RegisterDriver`, `GetDriver`, `GetDriverForPVC`, `StorageClassLister` interface | **New** |
| `pkg/drivers/doc.go` | Updated package documentation | **Modified** |
| `pkg/drivers/types_test.go` | Domain type tests | **New** |
| `pkg/drivers/interface_test.go` | Interface compile-time check | **New** |
| `pkg/drivers/errors_test.go` | Error sentinel tests | **New** |
| `pkg/drivers/registry_test.go` | Registry unit tests (register, get, PVC resolution, concurrency, reset) | **New** |

### Files NOT to Modify

- `pkg/drivers/credentials.go` — existing credential types and interface
- `pkg/drivers/credentials_secret.go` — existing errors and `SecretCredentialResolver`
- `pkg/drivers/credentials_test.go` — existing credential tests
- `pkg/drivers/noop/doc.go` — Story 3.2 target
- `pkg/drivers/fake/doc.go` — Story 3.3 target
- `pkg/drivers/conformance/doc.go` — Story 3.4 target
- `internal/preflight/storage.go` — do not refactor preflight in this story
- `cmd/soteria/main.go` — no wiring changes needed yet (no real drivers to register)
- Any controller, engine, or admission code

### CRITICAL — errors.go Separation

The existing `pkg/drivers/credentials_secret.go` already contains credential-related sentinel errors (`ErrVaultNotImplemented`, `ErrNoCredentialSource`, `ErrAmbiguousSource`, `ErrSecretNotFound`, `ErrSecretKeyNotFound`). The new storage driver errors (`ErrVolumeNotFound`, `ErrVolumeGroupNotFound`, etc.) go in a NEW file `pkg/drivers/errors.go` — do NOT add them to `credentials_secret.go` and do NOT move existing errors out of `credentials_secret.go`.

### Build Commands

```bash
make test                   # All unit tests (new + existing)
make lint-fix               # Auto-fix code style
make lint                   # Verify lint passes
make build                  # Verify compilation
go test ./pkg/drivers/...   # Driver package tests only
```

### Project Structure Notes

- Aligned with architecture: `pkg/drivers/interface.go`, `pkg/drivers/errors.go`, `pkg/drivers/registry.go` exactly as specified in the project directory structure
- External driver authors import `pkg/drivers/` — types and interface must be in `pkg/`, not `internal/`
- `StorageClassLister` interface keeps registry testable without k8s client dependency in unit tests
- The `DefaultRegistry` + package-level functions pattern matches Go stdlib conventions (`http.DefaultServeMux`, `prometheus.DefaultRegisterer`)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.1] — Full acceptance criteria and BDD scenarios
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 3] — Epic overview and FR coverage (FR20, FR21, FR23–FR25)
- [Source: _bmad-output/planning-artifacts/epics.md#Driver Framework (from Architecture)] — Registry pattern, typed errors, idempotency, conformance requirements
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — `init()` + registry, typed errors, context, idempotency
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/drivers/` directory layout with `interface.go`, `errors.go`, `registry.go`
- [Source: _bmad-output/planning-artifacts/architecture.md#Architectural Boundaries] — Driver boundary: `interface.go` = contract, above is driver-agnostic, below is vendor-specific
- [Source: _bmad-output/planning-artifacts/architecture.md#Naming Patterns] — Interface naming, error naming, package naming
- [Source: _bmad-output/planning-artifacts/prd.md#Storage Abstraction FR20-FR25] — Functional requirements for 7-method interface, implicit selection, conformance
- [Source: _bmad-output/planning-artifacts/prd.md#NFR19] — Interface stability for external driver development
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — 7 methods, idempotency, context, registry, conformance
- [Source: _bmad-output/project-context.md#Critical Don't-Miss Rules] — Anti-patterns and architectural boundaries
- [Source: pkg/drivers/credentials.go] — Existing credential types and `CredentialResolver` interface (pattern reference)
- [Source: pkg/drivers/credentials_secret.go] — Existing `SecretCredentialResolver` and credential error sentinels (pattern reference)
- [Source: internal/preflight/storage.go] — `StorageBackendResolver` interface and `StorageClassDriverMap` (related but separate from registry)
- [Source: pkg/engine/discovery.go] — `VMDiscoverer` interface pattern (small interfaces, compile-time check)

## Dev Agent Record

### Agent Model Used

Claude Opus 4 (claude-sonnet-4-20250514)

### Debug Log References

None — implementation completed without errors or retries.

### Completion Notes List

- Implemented domain types in `types.go`: `VolumeGroupID`, `VolumeRole` (3 constants), `ReplicationHealth` (4 constants), `VolumeGroupSpec`, `VolumeGroupInfo`, `ReplicationStatus`, `SetSourceOptions`, `SetTargetOptions`, `StopReplicationOptions`
- Defined 7-method `StorageProvider` interface in `interface.go` with role-based replication model (NonReplicated/Source/Target), comprehensive godoc covering idempotency guarantees, error conditions, and DR lifecycle context per method
- Created 5 sentinel errors in `errors.go` — kept separate from existing credential errors in `credentials_secret.go`
- Implemented thread-safe `Registry` with `sync.RWMutex`, panic-on-duplicate registration, `GetDriverForPVC` resolution chain, `StorageClassLister` abstraction, `DefaultRegistry` singleton with package-level convenience functions
- Fixed 2 lint issues (line length >120 chars on `GetDriverForPVC` signatures) by wrapping parameters
- 17 new tests across 4 test files; existing 10 credential tests unaffected
- Driver package coverage: 91.8%
- All existing tests pass (no regressions), `make lint` clean, `make build` clean

### Change Log

- 2026-04-16: Implemented Story 3.1 — StorageProvider interface, domain types, typed errors, driver registry, and unit tests
- 2026-04-16: Reworked in place — 9-method interface redesigned to 7-method role-based model (NonReplicated/Source/Target), replaced ReplicationState/PromoteOptions/DemoteOptions with VolumeRole/ReplicationHealth/ReplicationStatus/SetSourceOptions/SetTargetOptions/StopReplicationOptions, replaced ErrPromotionFailed/ErrDemotionFailed/ErrResyncFailed with ErrInvalidTransition

### File List

| File | Action |
|------|--------|
| `pkg/drivers/types.go` | **New** — Domain types (VolumeGroupID, VolumeRole, ReplicationHealth, VolumeGroupSpec, VolumeGroupInfo, ReplicationStatus, SetSourceOptions, SetTargetOptions, StopReplicationOptions) |
| `pkg/drivers/interface.go` | **New** — StorageProvider 7-method interface with role-based replication model |
| `pkg/drivers/errors.go` | **New** — 5 sentinel errors for storage driver operations |
| `pkg/drivers/registry.go` | **New** — Registry, DriverFactory, StorageClassLister, DefaultRegistry, package-level functions |
| `pkg/drivers/doc.go` | **Modified** — Updated package godoc to cover interface, registry, errors, credentials |
| `pkg/drivers/types_test.go` | **New** — VolumeRole and ReplicationHealth string constant tests |
| `pkg/drivers/interface_test.go` | **New** — Compile-time interface check with mockProvider |
| `pkg/drivers/errors_test.go` | **New** — Sentinel error distinctness and errors.Is wrapping tests |
| `pkg/drivers/registry_test.go` | **New** — Registry tests (register/get, not-found, duplicate panic, PVC resolution, concurrent access, reset) |
