# Story 12.2: VolumeReplication / VolumeGroupReplication CRD Types & Client

Status: done

## Story

As a CSI extension driver developer,
I want Go types for VolumeReplication and VolumeGroupReplication CRDs and a configured Kubernetes client to manage them,
So that subsequent stories can create, read, update, and delete these CRDs declaratively.

## Background

### CSI Addons CRD Model

The [csi-addons/kubernetes-csi-addons](https://github.com/csi-addons/kubernetes-csi-addons) project defines Kubernetes CRDs that wrap the CSI Addons gRPC RPCs:

- **VolumeReplication** — per-PVC replication management. `spec.replicationState` drives state transitions (`primary`, `secondary`, `resync`). The csi-addons sidecar reconciles this into PromoteVolume/DemoteVolume/ResyncVolume RPCs.
- **VolumeGroupReplication** — multi-PVC group replication. Same `spec.replicationState` model but applied to a group of volumes atomically.
- **VolumeReplicationClass** / **VolumeGroupReplicationClass** — define replication parameters (schedule, backend config).

### Design Decision

Soteria needs Go types for these CRDs to create/manage them as Kubernetes resources. Options:

1. **Import from csi-addons module** — pull `github.com/csi-addons/kubernetes-csi-addons/api/...` as a Go dependency.
2. **Define local types** — define minimal types in Soteria matching the CRD schema, using `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured` or typed structs.

**Decision: Option 1 (import)** if the module is stable and the types are well-defined. **Fallback to Option 2** if the dependency tree is too heavy or the API is unstable. The developer should evaluate both during implementation.

### Client Strategy

The driver uses `controller-runtime`'s `client.Client` (already available in the reconciler) or the dynamic client to manage VR/VGR resources. The client is injected into the `Driver` struct via the factory or a config struct.

## Acceptance Criteria

1. **AC1 — Go types available:** VolumeReplication and VolumeGroupReplication Go types are available in the codebase (either imported from csi-addons or locally defined). Types include `Spec` (with `replicationState`, `dataSource`, `volumeReplicationClassName`) and `Status` (with `state`, `conditions`, `message`).

2. **AC2 — ReplicationState constants:** String constants defined for `primary`, `secondary`, `resync` replication states, usable by the driver for spec updates.

3. **AC3 — VolumeReplicationClass reference:** A mechanism to specify which `VolumeReplicationClass` / `VolumeGroupReplicationClass` the driver should use when creating CRs. This could be a field on the DRPlan spec, a driver config, or a convention-based lookup.

4. **AC4 — Client injection:** The `Driver` struct gains a Kubernetes client field (e.g., `client.Client`) that can create, read, update, and delete VR/VGR resources. The factory in `init()` is updated to accept or obtain the client.

5. **AC5 — Factory wiring:** The driver factory in `main.go` or the driver registration path is updated so the `csi-extension` driver receives a configured client at construction time. This may require extending the `DriverFactory` signature or using a post-registration configuration step.

6. **AC6 — Scheme registration:** VR/VGR types are registered in the controller-runtime scheme so that `client.Client` can serialize/deserialize them.

7. **AC7 — Unit tests:** Tests verify that the types can be serialized to/from JSON, that the scheme registration works, and that the client can be injected into the driver.

## Tasks / Subtasks

- [x] Task 1: Evaluate dependency approach (AC: #1)
  - [x] 1.1 Check if `github.com/csi-addons/kubernetes-csi-addons/api` is importable and stable
  - [x] 1.2 If yes, `go get` the module; if no, define local types in `pkg/drivers/csiextension/types.go`

- [x] Task 2: Define or import types (AC: #1, #2)
  - [x] 2.1 Ensure VolumeReplication, VolumeGroupReplication, and their List types are available
  - [x] 2.2 Define `ReplicationStatePrimary`, `ReplicationStateSecondary`, `ReplicationStateResync` constants

- [x] Task 3: VolumeReplicationClass reference (AC: #3)
  - [x] 3.1 Decide on the mechanism (driver config, DRPlan annotation, convention-based)
  - [x] 3.2 Implement the reference mechanism

- [x] Task 4: Client injection (AC: #4, #5)
  - [x] 4.1 Add `client.Client` field to `Driver` struct
  - [x] 4.2 Update `New()` constructor to accept a client
  - [x] 4.3 Update the driver factory/registration to provide the client
  - [x] 4.4 Consider whether `DriverFactory` needs to become `DriverFactory func(config) StorageProvider` or use a post-registration config

- [x] Task 5: Scheme registration (AC: #6)
  - [x] 5.1 Register VR/VGR types in the scheme used by the manager
  - [x] 5.2 Verify `client.Client.Get/Create/Update/Delete` works with VR/VGR types

- [x] Task 6: Tests (AC: #7)
  - [x] 6.1 Test type serialization round-trip
  - [x] 6.2 Test driver construction with injected client
  - [x] 6.3 Run `make test` — all tests pass
  - [x] 6.4 Run `make lint-fix && make lint` — zero lint issues

## Dev Notes

### Key Locations

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/types.go` | New — local CRD types (if not importing) |
| `pkg/drivers/csiextension/driver.go` | Modified — add client field |
| `pkg/drivers/csiextension/constants.go` | New — replication state constants |
| `cmd/soteria/main.go` | Modified — wire client into driver factory |
| `go.mod` / `go.sum` | Modified — if importing csi-addons module |

### Driver Factory Extension

The current `DriverFactory` is `func() StorageProvider`. The csi-extension driver needs a client at construction time. Options:

1. **Post-registration injection:** After `NewRegistry()`, call a `Configure(driverName, config)` method
2. **Closure capture:** The factory closure captures the client from the main setup
3. **Interface extension:** Add an optional `Configurable` interface with `Configure(client.Client)`

Option 2 (closure) is simplest and matches how the noop driver captures nothing:
```go
drivers.RegisterDriver("csi-extension", func() drivers.StorageProvider {
    return csiextension.New(mgr.GetClient())
})
```

But this requires the registration to happen AFTER the manager is created, not in `init()`. The developer should evaluate whether to move registration out of `init()` for this driver, or use a deferred configuration pattern.

### What NOT to Change

- `StorageProvider` interface — no method signature changes
- `Registry` internals — no changes to the registry itself
- Noop driver — no modifications
- Handler/executor — no changes

### Dependency

- **Depends on Story 12.1** — driver skeleton must exist.
- **Soft dependency on csi-addons module** — if unavailable, local types are used.

### Previous Story Intelligence

- **Story 3.1 (Interface & Registry):** DriverFactory pattern and registration model.
- **Story 1.5 (Aggregated API Server):** Shows how external types are registered in the scheme.

### Build Commands

```bash
go get github.com/csi-addons/kubernetes-csi-addons/api/...  # If importing
make test
make lint-fix && make lint
```

## Dev Agent Record

### Implementation Plan

- **Option 1 (import) confirmed**: csi-addons v0.14.0 already in go.mod (from Story 12.0). VR/VGR types are well-defined and stable. No new go.mod changes needed.
- **ReplicationState constants**: Created `constants.go` re-exporting `Primary`/`Secondary`/`Resync` from csi-addons `replicationv1alpha1` package as `ReplicationStatePrimary`/`ReplicationStateSecondary`/`ReplicationStateResync`.
- **VolumeReplicationClass mechanism**: Already exists on `DRPlanSpec.VolumeReplicationDriver.VolumeReplicationClass` (from Story 12.0). Added `VolumeReplicationClassLabel` constant (`soteria.io/volume-replication-class`) for passing the class name through `VolumeGroupSpec.Labels` to the driver without changing the `StorageProvider` interface.
- **Client injection (closure pattern)**: Chose Option 2 (closure capture). Removed `init()` from `driver.go`, removed csiextension from `all.go` (no more side-effect import since it needs a client). `main.go` registers the driver after manager creation: `drivers.RegisterDriver(csiextension.DriverName, func() drivers.StorageProvider { return csiextension.New(mgr.GetClient()) })`.
- **Scheme registration**: Already done in `main.go init()` since Story 12.0: `utilruntime.Must(replicationv1alpha1.AddToScheme(scheme))`. Verified via TestSchemeRegistration_* tests.
- **No changes to**: StorageProvider interface, Registry internals, noop driver, handler/executor.

### Completion Notes

All 6 tasks and subtasks completed. 11 tests (with subtests) covering: driver construction with client, nil client safety, ReplicationState constant values, VR/VGR JSON round-trip serialization, scheme registration for VR and VGR, fake client CRUD verification, VolumeReplicationClassLabel non-empty, DriverName correctness, registry-based driver retrieval. All unit tests pass, all integration tests pass, 0 lint issues.

### Debug Log

No debug issues encountered. Clean implementation.

## File List

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/constants.go` | New — ReplicationState constants, VolumeReplicationClassLabel |
| `pkg/drivers/csiextension/driver.go` | Modified — added client.Client field, updated New() signature, removed init() |
| `pkg/drivers/csiextension/doc.go` | Modified — updated docs for new construction pattern |
| `pkg/drivers/csiextension/driver_test.go` | New — 11 tests for types, scheme, client injection, constants |
| `pkg/drivers/csiextension/registration_test.go` | Modified — updated to manual registry (no init()) |
| `pkg/drivers/all/all.go` | Modified — removed csiextension import (no more init() side-effect) |
| `cmd/soteria/main.go` | Modified — explicit csi-extension driver registration after manager creation |

## Change Log

- 2026-05-14: Story 12.2 implemented — csi-addons VR/VGR types imported (Option 1), ReplicationState constants defined, client.Client injected into Driver via closure-based factory registration in main.go, VolumeReplicationClassLabel for per-plan VRC passing, 11 tests added, 0 lint issues, all unit/integration tests pass

### Review Findings

- [x] [Review][Patch] Derive `VolumeGroupReplicationClass` by convention from `volumeReplicationClass` — added `VolumeGroupReplicationClassLabel` constant, updated doc.go
- [x] [Review][Patch] Assert replication class fields in the JSON round-trip tests — added `classJSONKey` table field and assertion
- [x] [Review][Patch] Exercise real fake-client CRUD instead of only constructor wiring — full Create/Get/Update/Delete cycle on VolumeReplication
