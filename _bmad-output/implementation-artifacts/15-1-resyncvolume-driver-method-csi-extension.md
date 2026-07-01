# Story 15.1: ResyncVolume Driver Method & CSI Extension Implementation

Status: done

## Story

As a developer,
I want a `ResyncVolume` method on the StorageProvider interface that sets a VR/VGR to resync state,
so that the orchestrator can request data synchronization from the current primary before a planned failover promotion.

## Acceptance Criteria

**AC1: StorageProvider interface extension**
Given the current 6-method StorageProvider interface (`pkg/drivers/interface.go`)
When the `ResyncVolume` method is added
Then the interface has 7 methods
And the signature is `ResyncVolume(ctx context.Context, id VolumeGroupID) error`
And the method sets `spec.replicationState = resync` on the target VR/VGR

**AC2: CSI Extension implementation**
Given the CSI Extension driver in `pkg/drivers/csiextension/driver.go`
When `ResyncVolume` is called with a valid VolumeGroupID
Then the corresponding VR or VGR CR has its `spec.replicationState` patched to `resync`
And the operation is idempotent (no error if already in resync state)

**AC3: Noop driver passthrough**
Given the noop driver in `pkg/drivers/noop/driver.go`
When `ResyncVolume` is called
Then it checks context cancellation and volume group existence (returns `ErrVolumeGroupNotFound` if absent, consistent with `SetSource`/`StopReplication`)
And returns nil for existing volume groups (no state mutation — resync has no semantic meaning in noop)

**AC4: Fake driver support**
Given the fake driver in `pkg/drivers/fake/driver.go`
When `ResyncVolume` is called
Then it supports the same `On*/Return` programmable API as other methods (`OnResyncVolume(vgID ...VolumeGroupID) *CallStub`)
And call recording captures the invocation (`Call{Method: "ResyncVolume", Args: []any{id}}`)

**AC5: Conformance test**
Given the conformance test suite in `pkg/drivers/conformance/suite.go`
When `RunConformance` is executed
Then a `ResyncVolume` lifecycle test is included (create VG → resync → verify no error)
And idempotency test (resync twice → no error on second call)
And not-found test (resync non-existent VG → `ErrVolumeGroupNotFound`)
And context cancellation test (cancelled ctx → error)

**AC6: Context cancellation**
Given a `ResyncVolume` call in progress
When the context is cancelled
Then the operation respects `ctx.Err()` and returns the cancellation error

## Tasks / Subtasks

- [x] Task 1: Add `ResyncVolume` to StorageProvider interface (AC: 1, 6)
  - [x] 1.1: Add method signature to `pkg/drivers/interface.go` with godoc following existing style
  - [x] 1.2: Update interface doc comment to mention 7 methods and add `resync` transition description
  - [x] 1.3: Add `ResyncVolume` stub to `mockProvider` in `pkg/drivers/interface_test.go` (compile-time check — returns nil)

- [x] Task 2: Implement CSI Extension `ResyncVolume` (AC: 2, 6)
  - [x] 2.1: Add `ResyncVolume` method to `pkg/drivers/csiextension/driver.go` following `SetSource`/`StopReplication` pattern
  - [x] 2.2: Add `ResyncVolume` passthrough to `conformanceAdapter` in `pkg/drivers/csiextension/conformance_test.go` (delegate to `a.driver.ResyncVolume`)
  - [x] 2.3: Add unit test in `pkg/drivers/csiextension/driver_test.go` for VR and VGR paths

- [x] Task 3: Implement noop `ResyncVolume` (AC: 3, 6)
  - [x] 3.1: Add `ResyncVolume` method to `pkg/drivers/noop/driver.go`
  - [x] 3.2: Add unit test in `pkg/drivers/noop/driver_test.go`

- [x] Task 4: Implement fake `ResyncVolume` (AC: 4)
  - [x] 4.1: Add `OnResyncVolume` stub method to `pkg/drivers/fake/driver.go`
  - [x] 4.2: Add `ResyncVolume` StorageProvider implementation to `pkg/drivers/fake/driver.go`
  - [x] 4.3: Add unit test in `pkg/drivers/fake/driver_test.go`

- [x] Task 5: Add conformance tests (AC: 5)
  - [x] 5.1: Add `ResyncVolume` steps to `runLifecycleTest` in `pkg/drivers/conformance/suite.go`
  - [x] 5.2: Add `ResyncVolume` idempotency test to `runIdempotencyTest`
  - [x] 5.3: Add `ResyncVolume` context cancellation test to `runContextCancellationTest`
  - [x] 5.4: Add `ResyncVolume_NotFound` test to `runErrorConditionsTest`

- [x] Task 6: Update project documentation
  - [x] 6.1: Update `_bmad-output/project-context.md` line 118 — change "6-method interface" to "7-method interface" and add `ResyncVolume` to the method list

- [x] Task 7: Verify and finalize
  - [x] 7.1: Run `make lint-fix` — no new issues (pre-existing dupl/errcheck warnings only)
  - [x] 7.2: Run `make test` — all unit + envtest tests pass
  - [x] 7.3: Verify Tier 1/2/3 doc compliance (doc.go unaffected, method godoc added)

## Dev Notes

### Implementation Pattern — CSI Extension

`ResyncVolume` follows the **exact** pattern of `SetSource` and `StopReplication`. All three methods are one-liners that delegate to shared helpers:

```go
// SetSource (line 382-392 of driver.go) — the template:
func (d *Driver) SetSource(ctx context.Context, id drivers.VolumeGroupID) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    set, err := d.listCRsForVG(ctx, id)
    if err != nil {
        return err
    }
    return d.updateReplicationState(ctx, set, ReplicationStatePrimary)
}

// ResyncVolume follows this exactly, substituting ReplicationStateResync:
func (d *Driver) ResyncVolume(ctx context.Context, id drivers.VolumeGroupID) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    set, err := d.listCRsForVG(ctx, id)
    if err != nil {
        return err
    }
    return d.updateReplicationState(ctx, set, ReplicationStateResync)
}
```

**Critical: `ReplicationStateResync` already exists** in `pkg/drivers/csiextension/constants.go` line 30:

```go
ReplicationStateResync = replicationv1alpha1.Resync
```

No new constant is needed. The `updateReplicationState` helper in `helpers.go` already handles both VR and VGR CRs with retry-on-conflict — no changes to helpers.go required.

### Implementation Pattern — Noop Driver

Follow `StopReplication` pattern (lines 154-177 of `noop/driver.go`):

```go
func (d *Driver) ResyncVolume(ctx context.Context, id drivers.VolumeGroupID) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    d.mu.RLock()
    _, ok := d.volumeGroups[id]
    d.mu.RUnlock()
    if !ok {
        log.FromContext(ctx).V(1).Info("No-op: Volume group not found for ResyncVolume", "volumeGroupID", id)
        return drivers.ErrVolumeGroupNotFound
    }
    log.FromContext(ctx).V(1).Info("No-op: ResyncVolume (no-op)", "volumeGroupID", id)
    return nil
}
```

Use `RLock` (read lock) since no state mutation occurs — resync has no semantic meaning in the noop driver. The noop driver tracks `VolumeRole` (`Source`, `NonReplicated`, `Target`) but resync is a temporary CSI state, not a role transition.

### Implementation Pattern — Fake Driver

Follow `StopReplication` pattern (lines 169-286 of `fake/driver.go`). Two additions:

1. **Stub method** (after `OnStopReplication`):

```go
func (d *Driver) OnResyncVolume(vgID ...drivers.VolumeGroupID) *CallStub {
    return d.onMethod("ResyncVolume", optionalVgID(vgID))
}
```

2. **StorageProvider method** (after `StopReplication`):

```go
func (d *Driver) ResyncVolume(_ context.Context, id drivers.VolumeGroupID) error {
    d.mu.Lock()
    d.calls = append(d.calls, Call{Method: "ResyncVolume", Args: []any{id}})
    r := d.findReaction("ResyncVolume", id)
    d.mu.Unlock()
    if r != nil {
        return r.resp.Err
    }
    return nil
}
```

### Implementation Pattern — CSI Extension Conformance Adapter

The `conformanceAdapter` in `pkg/drivers/csiextension/conformance_test.go` wraps all StorageProvider methods. Add `ResyncVolume` as a simple delegation — no status simulation needed (the lifecycle test doesn't check status between ResyncVolume and StopReplication):

```go
func (a *conformanceAdapter) ResyncVolume(ctx context.Context, id drivers.VolumeGroupID) error {
    return a.driver.ResyncVolume(ctx, id)
}
```

Also add `ResyncVolume` to `mockProvider` in `pkg/drivers/interface_test.go`:

```go
func (m *mockProvider) ResyncVolume(_ context.Context, _ VolumeGroupID) error {
    return nil
}
```

### Implementation Pattern — Conformance Suite

Add `ResyncVolume` to all four test categories in `pkg/drivers/conformance/suite.go`:

1. **Lifecycle** (`runLifecycleTest`): Insert after `SetSource` step (line 90-97) — call `ResyncVolume(vgID)`, assert no error. The lifecycle becomes: Create → SetSource → **ResyncVolume** → StopReplication → Delete → Get(deleted).

2. **Idempotency** (`runIdempotencyTest`): Add after `SetSource` idempotency (line 210-217):
   ```go
   t.Run("ResyncVolume", func(t *testing.T) {
       // ResyncVolume after SetSource — should succeed
       // Call twice — second call is idempotent
   })
   ```

3. **ContextCancellation** (`runContextCancellationTest`): Add `ResyncVolume` with cancelled context (between SetSource and StopReplication tests).

4. **ErrorConditions** (`runErrorConditionsTest`): Add `ResyncVolume_NotFound` — call with `nonexistentID`, assert `ErrVolumeGroupNotFound`.

### File Inventory

| File | Action | Lines Changed (est.) |
|------|--------|---------------------|
| `pkg/drivers/interface.go` | Add method + update doc | ~10 |
| `pkg/drivers/interface_test.go` | Add `ResyncVolume` to `mockProvider` | ~4 |
| `pkg/drivers/csiextension/driver.go` | Add `ResyncVolume` method | ~10 |
| `pkg/drivers/csiextension/conformance_test.go` | Add `ResyncVolume` to `conformanceAdapter` | ~4 |
| `pkg/drivers/csiextension/driver_test.go` | Add ResyncVolume unit tests | ~40 |
| `pkg/drivers/noop/driver.go` | Add `ResyncVolume` method | ~15 |
| `pkg/drivers/noop/driver_test.go` | Add ResyncVolume unit test | ~20 |
| `pkg/drivers/fake/driver.go` | Add `OnResyncVolume` + `ResyncVolume` | ~15 |
| `pkg/drivers/fake/driver_test.go` | Add ResyncVolume unit test | ~20 |
| `pkg/drivers/conformance/suite.go` | Add to 4 test categories | ~40 |
| `_bmad-output/project-context.md` | Update method count 6→7 | ~2 |

**Total: ~11 modified files, ~180 lines added**

### Compilation Guardrails — StorageProvider Implementations

Adding `ResyncVolume` to the interface will immediately break compilation in **all** files that implement `StorageProvider`. The dev agent must update these files in the **same commit**:

1. `pkg/drivers/interface_test.go` — `mockProvider` (compile-time check only, returns nil)
2. `pkg/drivers/csiextension/driver.go` — real implementation
3. `pkg/drivers/csiextension/conformance_test.go` — `conformanceAdapter` (delegates to `a.driver.ResyncVolume`)
4. `pkg/drivers/noop/driver.go` — noop implementation
5. `pkg/drivers/fake/driver.go` — fake implementation

Each file has `var _ drivers.StorageProvider = (*T)(nil)` that enforces the contract at compile time. Update ALL five before running `make test`.

### Domain Context — What `resync` Does in Ceph

`resync` is an existing CSI Addons VolumeReplication spec state. When a **secondary** VR is set to `spec.replicationState = resync`:

1. The CSI Addons sidecar instructs Ceph to pull any un-replicated data from the peer primary
2. The VR transitions through `status.state=Secondary` with `conditions[Completed]=False` (resync in progress)
3. When fully synced: `status.state=Secondary, conditions[Completed].status=True`
4. This guarantees zero data loss before a planned failover promotion

Story 15.2 will use this method in the DRExecution controller's Step 0 to implement the resync guard.

### Key Constraints

- **Do NOT modify `pkg/drivers/csiextension/helpers.go`** — `listCRsForVG` and `updateReplicationState` are already generic and handle any `ReplicationState` value
- **Do NOT modify `pkg/drivers/csiextension/constants.go`** — `ReplicationStateResync` already exists at line 30
- **Do NOT add new sentinel errors** — `ResyncVolume` uses the same error types as `SetSource`/`StopReplication` (`ErrVolumeGroupNotFound`, context errors)
- **Interface doc comment update** — change "6-method" to "7-method" and add `Resync` transition to the role model description. The resync transition is `Secondary → Resync → Secondary(Completed)` — it's a CSI-level concept, not a role transition in the engine's model. Document it as a storage-layer synchronization request
- **Compile-time interface check** — `var _ drivers.StorageProvider = (*Driver)(nil)` in each driver file will catch missing implementations immediately

### Previous Story Intelligence

This is the first story in Epic 15. Epic 14 was 100% infrastructure (shell scripts, Kustomize overlays) — no Go code. The last Go code was in Epic 13 (VolumeReplication lifecycle redesign). Key patterns from Epic 13 that still apply:

- CSI Extension driver follows the same `listCRsForVG` → `updateReplicationState` pattern introduced in Epic 12 and refined in Epic 13
- All driver methods must be idempotent — the `updateReplicationState` helper already skips CRs that are already in the target state
- Unit tests for CSI Extension use envtest (real etcd + API server) — not fake client
- Conformance suite uses standard `testing` (not Ginkgo)

### Git Intelligence

Recent commits (last 5) were all Epic 14 infrastructure work — no Go files changed. The driver framework has been stable since Epic 13. No API changes or dependency updates affect this story.

### Project Structure Notes

- All files are in existing packages — no new packages or directories needed
- Naming follows established conventions: method name `ResyncVolume` matches `SetSource` / `StopReplication` (verb+noun)
- Conformance suite changes in `pkg/drivers/conformance/suite.go` are automatically picked up by all driver conformance tests (`csiextension/conformance_test.go`, `noop/noop_test.go`)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.1]
- [Source: pkg/drivers/interface.go — StorageProvider interface]
- [Source: pkg/drivers/csiextension/driver.go — SetSource/StopReplication pattern]
- [Source: pkg/drivers/csiextension/helpers.go — listCRsForVG + updateReplicationState]
- [Source: pkg/drivers/csiextension/constants.go — ReplicationStateResync]
- [Source: pkg/drivers/noop/driver.go — StopReplication pattern]
- [Source: pkg/drivers/fake/driver.go — On*/Return/Call pattern]
- [Source: pkg/drivers/conformance/suite.go — test categories]
- [Source: _bmad-output/project-context.md — StorageProvider Driver Framework rules]
- [Source: _bmad-output/implementation-artifacts/epic-14-retro-2026-06-30.md — retrospective carry-forward]

## Dev Agent Record

### Agent Model Used

Opus 4.6 (Cursor)

### Debug Log References

- Compilation break in `pkg/drivers/registry_test.go` — `stubProvider` also implemented StorageProvider and needed the `ResyncVolume` method added. Fixed immediately.

### Completion Notes List

- Added `ResyncVolume(ctx context.Context, id VolumeGroupID) error` to `StorageProvider` interface, extending it from 6 to 7 methods. Updated interface doc comment with resync transition description.
- CSI Extension implementation follows `SetSource`/`StopReplication` pattern exactly — delegates to `listCRsForVG` + `updateReplicationState(ctx, set, ReplicationStateResync)`. No changes to helpers.go or constants.go needed.
- Noop implementation uses `RLock` (read lock) since resync has no semantic meaning — checks context cancellation and volume group existence, returns nil for existing groups.
- Fake implementation adds `OnResyncVolume` stub and `ResyncVolume` method following existing `On*/Return` programmable API pattern with call recording.
- Conformance suite adds ResyncVolume to all 4 test categories: lifecycle (Create → SetSource → ResyncVolume → StopReplication → Delete), idempotency (double-call), context cancellation, and not-found error condition.
- All 6 AC satisfied: interface extension (AC1), CSI Extension implementation (AC2), noop passthrough (AC3), fake driver support (AC4), conformance tests (AC5), context cancellation (AC6).
- All tests pass — 0 regressions, ~180 lines added across 12 files.
- Pre-existing lint warnings (dupl in helpers.go/reconciler_test.go, errcheck in health_test.go) not introduced by this story.

### File List

- `pkg/drivers/interface.go` — Added ResyncVolume method + updated doc comment (6→7 methods)
- `pkg/drivers/interface_test.go` — Added ResyncVolume to mockProvider
- `pkg/drivers/registry_test.go` — Added ResyncVolume to stubProvider
- `pkg/drivers/csiextension/driver.go` — Added ResyncVolume method
- `pkg/drivers/csiextension/driver_test.go` — Added ResyncVolume state transition, not-found, context-cancelled tests
- `pkg/drivers/csiextension/conformance_test.go` — Added ResyncVolume to conformanceAdapter
- `pkg/drivers/noop/driver.go` — Added ResyncVolume method
- `pkg/drivers/noop/driver_test.go` — Added ResyncVolume, ResyncVolume_NotFound, ResyncVolume_ContextCancelled tests + updated UnknownVolumeGroup table
- `pkg/drivers/fake/driver.go` — Added OnResyncVolume + ResyncVolume methods
- `pkg/drivers/fake/driver_test.go` — Added ResyncVolume default, error injection, call recording tests
- `pkg/drivers/conformance/suite.go` — Added ResyncVolume to lifecycle, idempotency, context-cancellation, error-conditions tests
- `_bmad-output/project-context.md` — Updated 6-method → 7-method interface description

### Change Log

- 2026-07-01: Implemented Story 15.1 — ResyncVolume Driver Method & CSI Extension. Extended StorageProvider interface with ResyncVolume method across all drivers (csi-extension, noop, fake) with full conformance test coverage. All ACs satisfied, all tests pass.
