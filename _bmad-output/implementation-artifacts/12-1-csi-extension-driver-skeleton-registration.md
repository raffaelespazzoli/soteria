# Story 12.1: CSI Extension Driver Skeleton & Registration

Status: ready-for-dev

## Story

As a storage driver framework maintainer,
I want a registered `csi-extension` driver package with a stub StorageProvider implementation,
So that the driver can be selected via `volumeReplicationDriver: csi-extension` and progressively filled in by subsequent stories.

## Background

### Context

Epic 11 introduced the `volumeReplicationDriver` field on DRPlanSpec with `noop` as the only admissible value. This story extends the enum to include `csi-extension` and creates the driver package skeleton. The `csi-extension` driver manages volume replication through CSI Addons VolumeReplication and VolumeGroupReplication Kubernetes CRDs, reconciled by the csi-addons sidecar.

### Design

The driver package follows the established pattern from `pkg/drivers/noop/`:
- Package at `pkg/drivers/csiextension/`
- `init()` registration under `"csi-extension"`
- Import in `pkg/drivers/all/all.go`
- Compile-time `StorageProvider` interface check

All 6 StorageProvider methods initially return `drivers.ErrDriverNotFound` (or a new `ErrNotImplemented` sentinel) — they are progressively implemented in Stories 12.3–12.5.

## Acceptance Criteria

1. **AC1 — Package created:** `pkg/drivers/csiextension/driver.go` exists with a `Driver` struct that implements all 6 `StorageProvider` methods. Each method returns an appropriate error indicating it is not yet implemented.

2. **AC2 — Compile-time interface check:** `var _ drivers.StorageProvider = (*Driver)(nil)` compiles.

3. **AC3 — Registration:** `init()` in `driver.go` calls `drivers.RegisterDriver("csi-extension", factory)`. `drivers.GetDriver("csi-extension")` returns a non-nil `StorageProvider`.

4. **AC4 — Import in all.go:** `pkg/drivers/all/all.go` imports `_ "github.com/soteria-project/soteria/pkg/drivers/csiextension"`.

5. **AC5 — Enum extended:** `DRPlanSpec.VolumeReplicationDriver` kubebuilder marker updated to `+kubebuilder:validation:Enum=noop;csi-extension`. `make manifests generate` regenerates CRD/OpenAPI.

6. **AC6 — Package documentation:** `pkg/drivers/csiextension/doc.go` describes the driver's purpose: managing volume replication through CSI Addons VolumeReplication/VolumeGroupReplication CRDs.

7. **AC7 — Registration test:** `pkg/drivers/csiextension/registration_test.go` verifies `drivers.GetDriver("csi-extension")` returns a non-nil provider.

8. **AC8 — Sample CRD updated:** `config/samples/soteria_v1alpha1_drplan.yaml` comment mentions `csi-extension` as an available driver value.

## Tasks / Subtasks

- [ ] Task 1: Create driver package (AC: #1, #2, #6)
  - [ ] 1.1 Create `pkg/drivers/csiextension/doc.go` with package documentation
  - [ ] 1.2 Create `pkg/drivers/csiextension/driver.go` with `Driver` struct, `New()` constructor, all 6 `StorageProvider` method stubs, compile-time check, `init()` registration

- [ ] Task 2: Import in all.go (AC: #4)
  - [ ] 2.1 Add `_ "github.com/soteria-project/soteria/pkg/drivers/csiextension"` import to `pkg/drivers/all/all.go`

- [ ] Task 3: Extend enum (AC: #5)
  - [ ] 3.1 Update `+kubebuilder:validation:Enum=noop;csi-extension` marker on `VolumeReplicationDriver` in `types.go`
  - [ ] 3.2 Run `make manifests generate`

- [ ] Task 4: Update sample (AC: #8)
  - [ ] 4.1 Add comment to `config/samples/soteria_v1alpha1_drplan.yaml`

- [ ] Task 5: Tests (AC: #3, #7)
  - [ ] 5.1 Create `pkg/drivers/csiextension/registration_test.go`
  - [ ] 5.2 Run `make test` — all tests pass
  - [ ] 5.3 Run `make lint-fix && make lint` — zero lint issues

## Dev Notes

### Key Locations

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/doc.go` | New — package documentation |
| `pkg/drivers/csiextension/driver.go` | New — Driver struct, stubs, init() |
| `pkg/drivers/csiextension/registration_test.go` | New — registration test |
| `pkg/drivers/all/all.go` | Modified — add import |
| `pkg/apis/soteria.io/v1alpha1/types.go` | Modified — extend enum |

### Driver Stub Pattern

```go
const DriverName = "csi-extension"

type Driver struct {
    // Fields added in Story 12.2 (client, config, etc.)
}

func New() *Driver {
    return &Driver{}
}

func (d *Driver) CreateVolumeGroup(ctx context.Context, spec drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) {
    return drivers.VolumeGroupInfo{}, fmt.Errorf("csi-extension: CreateVolumeGroup not yet implemented")
}
// ... remaining 5 methods follow the same pattern

func init() {
    drivers.RegisterDriver(DriverName, func() drivers.StorageProvider {
        return New()
    })
}
```

### What NOT to Change

- `pkg/drivers/noop/` — no modifications
- `pkg/drivers/fake/` — no modifications
- `pkg/drivers/registry.go` — no changes needed (registration is via init())
- No handler or executor changes

### Dependency

- **Depends on Epic 11 (Story 11.1)** — `VolumeReplicationDriver` field must exist on `DRPlanSpec`.

### Previous Story Intelligence

- **Story 3.1 (StorageProvider Interface & Registry):** Established the registration pattern. Follow exactly.
- **Story 3.2 (No-Op Driver):** Reference implementation for driver package structure.

### Build Commands

```bash
make manifests generate   # Regenerate CRDs/OpenAPI after enum change
make test                 # All unit tests
make lint-fix && make lint
```
