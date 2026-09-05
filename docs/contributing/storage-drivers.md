# Writing a Storage Driver

This guide walks you through implementing, registering, and validating a new
Soteria storage driver. By the end you will have a working
`StorageProvider` implementation that passes the conformance suite and is ready
for integration into a Soteria deployment.

## Prerequisites

| Requirement | Version |
|-------------|---------|
| Go | 1.22+ |
| Soteria source tree | latest `main` |
| Familiarity with Go interfaces and Kubernetes concepts (PVCs, namespaces) | — |

## Architecture overview

Soteria decouples DR orchestration from vendor-specific storage operations
through the **StorageProvider** interface defined in `pkg/drivers/interface.go`.
Every storage backend — Ceph, Dell PowerStore, Pure Storage, or your own —
implements this single interface and registers with the driver registry at
startup. The orchestrator discovers the correct driver via the PVC's
StorageClass provisioner name and delegates all replication operations to it.

```text
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│  DR Engine   │────▶│   Registry   │────▶│ StorageProvider   │
│ (controller) │     │ (provisioner │     │ (your driver)     │
│              │     │  → factory)  │     │                   │
└──────────────┘     └──────────────┘     └──────────────────┘
```

## Step 1: Understand the interface

The `StorageProvider` interface has **seven methods** using a role-based
replication model:

```go
type StorageProvider interface {
    CreateVolumeGroup(ctx context.Context, spec VolumeGroupSpec) (VolumeGroupInfo, error)
    DeleteVolumeGroup(ctx context.Context, id VolumeGroupID) error
    GetVolumeGroup(ctx context.Context, id VolumeGroupID) (VolumeGroupInfo, error)
    SetSource(ctx context.Context, id VolumeGroupID) error
    StopReplication(ctx context.Context, id VolumeGroupID) error
    ResyncVolume(ctx context.Context, id VolumeGroupID) error
    GetReplicationStatus(ctx context.Context, id VolumeGroupID) (ReplicationStatus, error)
}
```

### Role model

Soteria uses two engine-driven transitions, always routed through
`NonReplicated`:

```text
NonReplicated ──SetSource──▶ Source
Source ──StopReplication──▶ NonReplicated
```

The **Target** role exists in `ReplicationStatus` — the paired site's driver
reports its volumes as Target via `GetReplicationStatus`. However, the engine
never explicitly sets a volume to Target; when one site calls `SetSource`, the
paired site implicitly becomes the target as an admin precondition.

`ResyncVolume` requests storage-layer data synchronisation on the secondary
site before a planned failover promotion. It does **not** change the engine's
role model.

### Key types

| Type | Description |
|------|-------------|
| `VolumeGroupID` | Opaque driver-assigned identifier (e.g. CSI volume group handle) |
| `VolumeGroupSpec` | Desired volume group: `Name`, `Namespace`, `PVCNames`, `Labels` |
| `VolumeGroupInfo` | Existing volume group metadata: `ID`, `Name`, `PVCNames` |
| `VolumeRole` | `NonReplicated`, `Source`, or `Target` |
| `ReplicationHealth` | `Healthy`, `Degraded`, `Syncing`, `NotReplicating`, `Unknown` |
| `ReplicationStatus` | Current `Role`, `Health`, and `LastSyncTime` |

### Method-by-method contract

#### `CreateVolumeGroup`

Creates a new volume group containing the specified PVCs.

- **Idempotency:** If a volume group with the same `Name` and `Namespace` already
  exists, return its info without error.
- **Returns:** `VolumeGroupInfo` with a driver-assigned `ID`.

#### `DeleteVolumeGroup`

Removes a volume group and releases its resources.

- **Idempotency:** Returns `nil` if the volume group does not exist.
- The underlying PVCs are **not** deleted — only the grouping is removed.

#### `GetVolumeGroup`

Retrieves metadata for an existing volume group.

- **Error:** Returns `ErrVolumeGroupNotFound` if the group does not exist.

#### `SetSource`

Transitions a volume group to the Source role (replication origin, read-write).

- **Valid from:** `NonReplicated` only.
- **Idempotency:** Returns `nil` if already Source.
- **Errors:** `ErrInvalidTransition` if current role is Target;
  `ErrVolumeGroupNotFound` if the group does not exist.
- The driver must handle unreachable peers internally.

#### `StopReplication`

Transitions a volume group from Source or Target back to NonReplicated.

- **Idempotency:** Returns `nil` if already NonReplicated.
- **Error:** `ErrVolumeGroupNotFound` if the group does not exist.
- The driver must handle unreachable peers and outstanding writes internally.

#### `ResyncVolume`

Requests data resynchronisation for a volume group on the current secondary
site.

- **Idempotency:** Returns `nil` if already in resync state.
- **Error:** `ErrVolumeGroupNotFound` if the group does not exist.

#### `GetReplicationStatus`

Returns the current replication role and health for a volume group.

- The workflow engine polls this method to assess readiness before failover.
- **Error:** `ErrVolumeGroupNotFound` if the group does not exist.
- Return `HealthNotReplicating` when `Role` is `NonReplicated`.

### Error handling

Driver implementations **must** return typed sentinel errors from
`pkg/drivers/errors.go` (or wrap them with `fmt.Errorf` and `%w`) so the
workflow engine can make branching decisions via `errors.Is`:

```go
var (
    ErrVolumeGroupNotFound = errors.New("volume group not found")
    ErrInvalidTransition   = errors.New("invalid replication state transition")
    ErrDriverNotFound      = errors.New("storage driver not found for provisioner")
)
```

Never return raw error strings — always wrap with the appropriate sentinel:

```go
return fmt.Errorf("mydriver: volume group %s: %w", id, drivers.ErrVolumeGroupNotFound)
```

### Idempotency rules

Every method must be idempotent — safe to retry after a crash or restart
without side effects. Drivers act as reconcilers: check the actual storage
state before applying changes, flipping roles only if necessary. All methods
accept `context.Context` for cancellation and timeout propagation.

## Step 2: Scaffold your driver package

Create a new package under `pkg/drivers/<vendor>/`:

```text
pkg/drivers/myvendor/
├── driver.go           # StorageProvider implementation
├── driver_test.go      # Unit tests
├── doc.go              # Package documentation
└── conformance_test.go # Conformance suite invocation
```

Start with the implementation skeleton:

```go
package myvendor

import (
    "context"
    "sync"

    "github.com/soteria-project/soteria/pkg/drivers"
)

// compile-time interface check
var _ drivers.StorageProvider = (*Driver)(nil)

type Driver struct {
    mu sync.RWMutex
    // Add vendor-specific fields: API clients, connection pools, etc.
}

func New( /* vendor-specific params */ ) *Driver {
    return &Driver{
        // initialise fields
    }
}
```

!!! tip "Compile-time check"
    The `var _ drivers.StorageProvider = (*Driver)(nil)` line ensures a
    compilation error if your `Driver` does not implement all seven methods.
    Always include this in your driver file.

## Step 3: Implement the interface

Implement each method following the contract documented in [Step 1](#step-1-understand-the-interface).
Here is an annotated skeleton:

```go
func (d *Driver) CreateVolumeGroup(ctx context.Context, spec drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) {
    // 1. Respect context cancellation
    if err := ctx.Err(); err != nil {
        return drivers.VolumeGroupInfo{}, err
    }

    // 2. Check for idempotency — does this group already exist?
    //    If yes, return existing info without error.

    // 3. Call vendor API to create the volume group.

    // 4. Return VolumeGroupInfo with your driver-assigned ID.
    return drivers.VolumeGroupInfo{
        ID:       drivers.VolumeGroupID("myvendor-" + spec.Namespace + "/" + spec.Name),
        Name:     spec.Name,
        PVCNames: spec.PVCNames,
    }, nil
}

func (d *Driver) DeleteVolumeGroup(ctx context.Context, id drivers.VolumeGroupID) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    // Idempotent: return nil if not found (no error).
    return nil
}

func (d *Driver) GetVolumeGroup(ctx context.Context, id drivers.VolumeGroupID) (drivers.VolumeGroupInfo, error) {
    if err := ctx.Err(); err != nil {
        return drivers.VolumeGroupInfo{}, err
    }
    // Return ErrVolumeGroupNotFound if the group does not exist.
    return drivers.VolumeGroupInfo{}, drivers.ErrVolumeGroupNotFound
}

func (d *Driver) SetSource(ctx context.Context, id drivers.VolumeGroupID) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    // 1. Look up current role.
    // 2. If already Source → return nil (idempotent).
    // 3. If NonReplicated → transition to Source.
    // 4. If Target → return drivers.ErrInvalidTransition.
    // 5. If not found → return drivers.ErrVolumeGroupNotFound.
    return nil
}

func (d *Driver) StopReplication(ctx context.Context, id drivers.VolumeGroupID) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    // If already NonReplicated → return nil (idempotent).
    // Otherwise → transition to NonReplicated.
    return nil
}

func (d *Driver) ResyncVolume(ctx context.Context, id drivers.VolumeGroupID) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    // If already resyncing → return nil (idempotent).
    // Otherwise → request resync from storage backend.
    return nil
}

func (d *Driver) GetReplicationStatus(ctx context.Context, id drivers.VolumeGroupID) (drivers.ReplicationStatus, error) {
    if err := ctx.Err(); err != nil {
        return drivers.ReplicationStatus{}, err
    }
    // Query vendor API for current role and health.
    // Return HealthNotReplicating when role is NonReplicated.
    return drivers.ReplicationStatus{
        Role:   drivers.RoleNonReplicated,
        Health: drivers.HealthNotReplicating,
    }, nil
}
```

!!! warning "Context cancellation"
    Every method **must** check `ctx.Err()` before performing work. The
    conformance suite's ContextCancellation category verifies this.

## Step 4: Handle errors correctly

Return typed sentinels from `pkg/drivers/errors.go`. The workflow engine uses
`errors.Is` to branch on specific error conditions:

| Scenario | Error to return |
|----------|----------------|
| Volume group does not exist | `drivers.ErrVolumeGroupNotFound` |
| Invalid role transition (e.g. Target → Source) | `drivers.ErrInvalidTransition` |
| Replication link not ready | `drivers.ErrReplicationNotReady` |

Wrap errors with context using `%w`:

```go
if !found {
    return fmt.Errorf("myvendor: volume group %s in namespace %s: %w",
        name, namespace, drivers.ErrVolumeGroupNotFound)
}
```

## Step 5: Register via init() + RegisterDriver

Register your driver using the `init()` function so it is automatically
available when imported. The registry maps **CSI provisioner names** to driver
factories:

```go
package myvendor

import "github.com/soteria-project/soteria/pkg/drivers"

func init() {
    drivers.RegisterDriver("myvendor.example.com", func() drivers.StorageProvider {
        return New()
    })
}
```

### How the registry works

The `DefaultRegistry` is a process-wide singleton (similar to
`http.DefaultServeMux`). Driver packages register themselves in `init()`
functions. The orchestrator resolves drivers at runtime:

```text
PVC → StorageClass → provisioner name → Registry.GetDriver → StorageProvider
```

Key registry functions:

| Function | Purpose |
|----------|---------|
| `RegisterDriver(provisioner, factory)` | Associate a provisioner name with a driver factory |
| `GetDriver(provisioner)` | Return a `StorageProvider` for the provisioner |
| `SetFallbackDriver(factory)` | Set a catch-all driver (used by the noop driver for dev/CI) |
| `GetDriverForPVC(ctx, scName, lister)` | Resolve PVC → StorageClass → provisioner → driver |

!!! note "Fail-fast at startup"
    `RegisterDriver` **panics** if the same provisioner is registered twice.
    This is intentional — fail fast at startup rather than silent override
    (same pattern as `prometheus.MustRegister`).

### Importing your driver

Add an import to `pkg/drivers/all/all.go` so the driver is included in the
build:

```go
package all

import (
    _ "github.com/soteria-project/soteria/pkg/drivers/noop"
    _ "github.com/soteria-project/soteria/pkg/drivers/csiextension"
    _ "github.com/soteria-project/soteria/pkg/drivers/myvendor" // your driver
)
```

## Step 6: Run the conformance suite

The conformance test suite at `pkg/drivers/conformance/suite.go` validates that
your implementation correctly fulfils the `StorageProvider` contract. Create a
test file in your driver package:

```go
package myvendor_test

import (
    "testing"

    "github.com/soteria-project/soteria/pkg/drivers/conformance"
    "github.com/soteria-project/soteria/pkg/drivers/myvendor"
)

func TestConformance(t *testing.T) {
    conformance.RunConformance(t, myvendor.New())
}
```

Run it:

```bash
go test ./pkg/drivers/myvendor/ -run TestConformance -v
```

### What the conformance suite tests

The suite runs **four test categories** via `RunConformance(t, provider)`:

#### 1. Lifecycle

Exercises the complete DR lifecycle in sequence:

```text
Create → SetSource → ResyncVolume → GetStatus(Source) →
StopReplication → GetStatus(NonReplicated) → Delete → Get(deleted)
```

Each step depends on the state left by the previous step. If any step fails,
subsequent steps are skipped.

#### 2. Idempotency

Verifies that every method is safe to call twice in succession with identical
arguments:

- `CreateVolumeGroup` called twice with the same spec returns success both times
- `SetSource` called twice on the same volume group returns success both times
- `DeleteVolumeGroup` called twice on the same ID returns success both times
- All other methods follow the same pattern

#### 3. ContextCancellation

Verifies that every method returns an error when called with a pre-cancelled
context. This ensures your driver respects `context.Context` for cancellation
and timeout propagation.

#### 4. ErrorConditions

Verifies that operations on nonexistent volume group IDs return
`ErrVolumeGroupNotFound`:

- `GetVolumeGroup` with a nonexistent ID
- `SetSource` with a nonexistent ID
- `StopReplication` with a nonexistent ID
- `ResyncVolume` with a nonexistent ID
- `GetReplicationStatus` with a nonexistent ID

### Interpreting results

```text
=== RUN   TestConformance
=== RUN   TestConformance/Lifecycle
=== RUN   TestConformance/Lifecycle/CreateVolumeGroup
=== RUN   TestConformance/Lifecycle/SetSource
...
=== RUN   TestConformance/Idempotency
...
=== RUN   TestConformance/ContextCancellation
...
=== RUN   TestConformance/ErrorConditions
...
--- PASS: TestConformance (0.01s)
```

All four categories must pass. A failure in any sub-test indicates a contract
violation that must be fixed before the driver can be used in production.

## Step 7: Package as a Go module

For drivers maintained outside the Soteria repository, package your driver as a
separate Go module:

```text
github.com/myorg/soteria-driver-myvendor/
├── go.mod
├── go.sum
├── driver.go
├── driver_test.go
├── conformance_test.go
└── doc.go
```

Your `go.mod` should depend on the Soteria `pkg/drivers` package:

```text
module github.com/myorg/soteria-driver-myvendor

go 1.22

require github.com/soteria-project/soteria v0.x.x
```

!!! important "Interface stability"
    The `StorageProvider` interface is covered by NFR19 (interface stability).
    Breaking changes require a new API version with a deprecation period. You
    can depend on the 7-method contract remaining stable within a major version.

Users install your driver by adding a blank import to their operator build:

```go
import _ "github.com/myorg/soteria-driver-myvendor"
```

## Reference implementations

Soteria ships three driver implementations. Study them to understand different
implementation patterns:

### No-op driver (`pkg/drivers/noop/`)

The **minimal reference implementation**. It tracks volume groups and replication
roles entirely in memory using a `sync.RWMutex`-protected map.

**What it demonstrates:**

- Complete `StorageProvider` implementation with all 7 methods
- Thread-safe state management with `sync.RWMutex`
- Role state tracking (`NonReplicated` ↔ `Source`)
- Idempotent `CreateVolumeGroup` using `Name` + `Namespace` as the logical identity
- `init()` registration for multiple provisioner names (`noop.soteria.io`, `noop`)
- Fallback driver registration via `SetFallbackDriver`
- Volume group ID pattern: `noop-<namespace>/<name>`

**Best for:** Understanding the minimum required implementation.

### Fake driver (`pkg/drivers/fake/`)

A **programmable test double** for unit testing code that depends on
`StorageProvider`. It follows the Kubernetes `<package>fake` naming convention
(e.g. `k8s.io/client-go/kubernetes/fake`).

**What it demonstrates:**

- `On*(...).Return(err)` / `.ReturnResult(Response)` fluent API for programming responses
- FIFO reaction consumption with optional `VolumeGroupID` matchers
- Call recording: `Calls()`, `CallsTo(method)`, `CallCount(method)`, `Called(method)`
- `Reset()` for reuse across sub-tests

**Best for:** Writing unit tests for controllers and workflows that use a driver.

**Example usage:**

```go
d := fake.New()
d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)
d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
    ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleSource},
})

err := d.SetSource(ctx, "vg-1")
// err == drivers.ErrInvalidTransition

assert.True(t, d.Called("SetSource"))
assert.Equal(t, 1, d.CallCount("SetSource"))
```

### CSI extension driver (`pkg/drivers/csiextension/`)

The **production implementation** that manages volume replication through
CSI Addons `VolumeReplication` and `VolumeGroupReplication` CRDs. It uses a
`controller-runtime` client to create, read, update, and delete VR/VGR
resources in the Kubernetes cluster.

**What it demonstrates:**

- Real-world Kubernetes API interaction via `controller-runtime` client
- Single-VM groups (individual VR CRs per PVC) vs. multi-VM groups (single VGR CR)
- `CreateOrUpdate` pattern for idempotent CR management
- Dual-site finaliser management for safe deletion
- Retry-on-conflict for concurrent status updates
- Health aggregation across multiple CRs (worst-wins strategy)
- CSI Addons replication state mapping to Soteria roles

**Best for:** Understanding how a production driver integrates with Kubernetes.

## Checklist

Before submitting your driver, verify:

- [ ] All 7 `StorageProvider` methods are implemented
- [ ] Compile-time interface check (`var _ drivers.StorageProvider = (*Driver)(nil)`)
- [ ] All methods check `ctx.Err()` before performing work
- [ ] Typed errors from `pkg/drivers/errors.go` are returned (never raw strings)
- [ ] All methods are idempotent
- [ ] `init()` function registers the driver with the correct provisioner name
- [ ] Conformance suite passes all 4 categories
- [ ] Thread safety for concurrent access (the orchestrator runs parallel reconcile loops)
- [ ] Volume pairing is treated as an admin precondition (not managed by the driver)
- [ ] Unreachable peers are handled internally (no force flags from the orchestrator)
