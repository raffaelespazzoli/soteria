# Story 4.05: Driver Registry Fallback & Preflight Convergence

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want the preflight storage backend resolver to use the driver registry instead of a static driver map, with optional noop-driver fallback for unmapped provisioners,
So that storage backends are resolved from a single source of truth at both preflight and execution time, and dev/CI environments function without real storage infrastructure.

## Acceptance Criteria

1. **AC1 — Registry fallback configuration:** A `--noop-fallback` flag is accepted by `cmd/soteria/main.go`. When enabled, `Registry.GetDriver(provisionerName)` returns the noop driver for any provisioner that has no explicitly registered driver. When disabled (default), the existing `ErrDriverNotFound` behavior is preserved.

2. **AC2 — Preflight convergence:** `TypedStorageBackendResolver` uses `StorageClassLister` (storage class → CSI provisioner) and the driver registry. The reported backend string is the StorageClass's CSI provisioner name when `GetDriver(provisioner)` succeeds (explicit registration or noop fallback when `--noop-fallback` is enabled). When resolution fails (`ErrDriverNotFound` with fallback off, missing StorageClass, nil lister), the backend is `"unknown"` with warnings. `CoreClient` remains used for PVC reads — it is separate from `StorageClassLister`.

3. **AC3 — Static map removal:** The `StorageClassDriverMap` type in `internal/preflight/storage.go` and its empty wiring in `main.go` (`DriverMap: preflight.StorageClassDriverMap{}`) are deleted. No static storage class → driver name mapping remains.

4. **AC4 — Main.go wiring:** `cmd/soteria/main.go` passes a `drivers.StorageClassLister` (backed by a real Kubernetes StorageClass client) and the `drivers.DefaultRegistry` to `TypedStorageBackendResolver`. The `--noop-fallback` flag configures the registry fallback before controllers start.

5. **AC5 — Fallback isolation:** The noop fallback only affects driver resolution — it does not alter driver registration, conformance behavior, or driver factory semantics. `ListRegistered()` does not include the fallback driver in its output (it is not a registered provisioner).

6. **AC6 — Tests updated:** Preflight unit tests are updated to inject a `drivers.Registry` (or mock `StorageClassLister`) instead of `StorageClassDriverMap`. Registry unit tests cover: fallback enabled returns noop driver for unknown provisioner, fallback disabled returns `ErrDriverNotFound`, fallback does not affect explicitly registered drivers, `ListRegistered` excludes fallback.

7. **AC7 — Startup logging:** When `--noop-fallback` is enabled, log at Info using `setupLog` (same as other startup messages in `cmd/soteria/main.go`): `"Noop fallback enabled for unregistered provisioners"`. Registered drivers are logged at startup via `setupLog.Info("Registered storage drivers", "drivers", drivers.ListRegistered())`.

## Tasks / Subtasks

- [x] Task 1: Add fallback support to the driver registry (AC: #1, #5)
  - [x] 1.1 In `pkg/drivers/registry.go`, add a `fallbackFactory DriverFactory` field to `Registry`
  - [x] 1.2 Add `SetFallbackDriver(factory DriverFactory)` method on `Registry` — stores the factory; panics if called twice (same fail-fast pattern as `RegisterDriver`)
  - [x] 1.3 Update `GetDriver` to return `fallbackFactory()` when `!ok && fallbackFactory != nil` — preserving `ErrDriverNotFound` when fallback is nil
  - [x] 1.4 Update `GetDriverForPVC` to inherit the same fallback behavior (it delegates to `GetDriver`)
  - [x] 1.5 Ensure `ListRegistered` does NOT include the fallback driver — it is not a registered provisioner
  - [x] 1.6 Add package-level `SetFallbackDriver` convenience function delegating to `DefaultRegistry`
  - [x] 1.7 Update `ResetForTesting` to also clear the fallback factory

- [x] Task 2: Add registry fallback unit tests (AC: #1, #5, #6)
  - [x] 2.1 In `pkg/drivers/registry_test.go`, add test: `TestRegistry_GetDriver_FallbackEnabled_UnknownProvisioner` — returns noop driver, not error
  - [x] 2.2 Add test: `TestRegistry_GetDriver_FallbackDisabled_UnknownProvisioner` — returns `ErrDriverNotFound`
  - [x] 2.3 Add test: `TestRegistry_GetDriver_FallbackEnabled_RegisteredProvisioner` — returns the registered driver, not fallback
  - [x] 2.4 Add test: `TestRegistry_ListRegistered_ExcludesFallback` — fallback not in list
  - [x] 2.5 Add test: `TestRegistry_SetFallbackDriver_PanicOnDouble` — second call panics
  - [x] 2.6 Add test: `TestRegistry_ResetForTesting_ClearsFallback` — fallback cleared after reset
  - [x] 2.7 Add test for package-level `SetFallbackDriver` + `ResetForTesting` clearing fallback on `DefaultRegistry`

- [x] Task 3: Refactor `TypedStorageBackendResolver` to use the driver registry (AC: #2, #3)
  - [x] 3.1 In `internal/preflight/storage.go`, replace `DriverMap StorageClassDriverMap` field with `Registry *drivers.Registry` and `SCLister drivers.StorageClassLister`. **Preserve `Client client.Reader` and `CoreClient corev1client.CoreV1Interface`** — they are still required for VM reads and `PersistentVolumeClaims(...).Get(...)` and are independent of `StorageClassLister`
  - [x] 3.2 Delete the `StorageClassDriverMap` type entirely
  - [x] 3.3 Update `resolveVM` to: (1) read PVC via `r.CoreClient` (unchanged); (2) call `r.SCLister.GetProvisioner(ctx, storageClassName)` to obtain the CSI provisioner string; (3) call `r.Registry.GetDriver(provisionerName)` — if nil error, backend name is that provisioner string; if `errors.Is(err, drivers.ErrDriverNotFound)`, backend is `"unknown"` and emit a warning (unless noop fallback causes `GetDriver` to succeed — then use provisioner string, no warning). Other errors → `"unknown"` with a distinct warning. Do NOT use `GetDriverForPVC` here — keep provisioner resolution and driver lookup separate to get the provisioner name for display
  - [x] 3.4 Handle edge cases: `nil` SCLister (return "unknown" with warning), PVC without storage class, multiple storage classes across PVCs in the same VM

- [x] Task 4: Create a real `StorageClassLister` implementation (AC: #4)
  - [x] 4.1 In `internal/preflight/storage.go` (or a new file `internal/preflight/sc_lister.go`), implement `KubeStorageClassLister` that satisfies `drivers.StorageClassLister`
  - [x] 4.2 `GetProvisioner(ctx, storageClassName)` calls `storageClient.StorageClasses().Get(ctx, name, metav1.GetOptions{})` and returns `sc.Provisioner`
  - [x] 4.3 Return a descriptive error wrapping the storage class name if the SC is not found

- [x] Task 5: Update `main.go` wiring (AC: #4, #7)
  - [x] 5.1 Add `--noop-fallback` flag (`pflag.Bool`) with default `false` and description
  - [x] 5.2 When flag is true: call `drivers.DefaultRegistry.SetFallbackDriver(func() drivers.StorageProvider { return noop.New() })` — import `noop` explicitly for the factory
  - [x] 5.3 Log `"Noop fallback enabled for unregistered provisioners"` at Info level when flag is true
  - [x] 5.4 Log registered drivers at startup: `setupLog.Info("Registered storage drivers", "drivers", drivers.ListRegistered())`
  - [x] 5.5 Create `KubeStorageClassLister` with `clientset.StorageV1()` and pass it to `TypedStorageBackendResolver`
  - [x] 5.6 Replace `DriverMap: preflight.StorageClassDriverMap{}` with `Registry: drivers.DefaultRegistry, SCLister: scLister`
  - [x] 5.7 Remove the `preflight.StorageClassDriverMap{}` construction entirely

- [x] Task 6: Update preflight unit tests (AC: #6)
  - [x] 6.1 In `internal/preflight/storage_test.go`, replace all `StorageClassDriverMap` usage with a mock/fake `StorageClassLister` and a test `Registry` with registered drivers
  - [x] 6.2 Add test: `TestResolveBackends_DriverRegistered` — provisioner in registry → backend is provisioner name
  - [x] 6.3 Add test: `TestResolveBackends_DriverNotRegistered` — provisioner not in registry → backend is `"unknown"`, warning emitted
  - [x] 6.4 Add test: `TestResolveBackends_FallbackEnabled` — unknown provisioner with fallback → backend is provisioner name (fallback resolves), no warning
  - [x] 6.5 Add test: `TestResolveBackends_NilSCLister` — returns `"unknown"` with warning
  - [x] 6.6 Update existing test scenarios to use the new field names
  - [x] 6.7 Confirm `internal/preflight/checks_test.go` has no `StorageClassDriverMap` / resolver fixtures — no change expected unless new references appear

- [x] Task 7: Update integration tests (AC: #6)
  - [x] 7.1 Update `test/integration/controller/suite_test.go` — replace `StorageClassDriverMap` / `DriverMap` wiring with `Registry` + `SCLister`. Ensure noop driver is registered (import `_ "github.com/soteria-project/soteria/pkg/drivers/all"` or register in test setup)
  - [x] 7.2 Create `storage.k8s.io/v1` `StorageClass` objects in the envtest environment for storage class names used by PVCs in test fixtures (e.g. `test-odf` with `provisioner: noop.soteria.io`)
  - [x] 7.3 Update `test/integration/controller/drplan_preflight_test.go` — change assertions from human-friendly names (e.g. `"odf"`) to CSI provisioner strings (e.g. `"noop.soteria.io"`)
  - [x] 7.4 Create `KubeStorageClassLister` with `clientset.StorageV1()` for integration test resolver wiring

- [x] Task 8: Update package documentation (AC: #7)
  - [x] 8.1 Update `internal/preflight/doc.go` to mention registry-based resolution
  - [x] 8.2 Add/update godoc on `TypedStorageBackendResolver` explaining the registry + SCLister flow
  - [x] 8.3 Update godoc on `Registry.GetDriver` to document fallback behavior

- [x] Task 9: Verify build and tests
  - [x] 9.1 Run `make test` — all unit tests pass (new + existing)
  - [x] 9.2 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [x] 9.3 Run `make build` — compiles cleanly
  - [x] 9.4 Run `make integration` — integration tests pass

## Dev Notes

### Architecture Context

This is Story 4.05 of Epic 4 (DR Workflow Engine — Full Lifecycle). It is a convergence story that must complete before Story 4.1 (State Machine & Execution Controller) because Story 4.1 needs runtime driver resolution to work correctly. The story resolves a gap identified in the Epic 3 retrospective: the preflight system and the driver registry resolve storage drivers through two independent, disconnected paths.

**Current state (two disconnected resolution paths):**

| Path | Location | Resolution | Result |
|------|----------|-----------|--------|
| Preflight (display) | `internal/preflight/storage.go` | `StorageClassDriverMap[storageClassName]` → driver name string | Empty map in `main.go` → always `"unknown"` |
| Runtime (execution) | `pkg/drivers/registry.go` | CSI provisioner name → `DriverFactory` → `StorageProvider` | `ErrDriverNotFound` if not registered |

**Target state (single resolution path):**

| Path | Location | Resolution | Result |
|------|----------|-----------|--------|
| Both | `pkg/drivers/registry.go` | Storage class → provisioner (via `StorageClassLister`) → registry lookup → `StorageProvider` | Driver found or fallback-to-noop (if enabled) or `ErrDriverNotFound` |

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| **4.05** | **Registry fallback + preflight convergence** | **Unifies driver resolution — prerequisite for all execution stories** |
| 4.1 | State machine & execution controller | Dispatches to drivers via registry (depends on 4.05) |
| 4.2 | DRGroup chunking & wave executor | Uses driver per DRGroup |
| 4.3 | Planned migration workflow | Calls SetSource, StopReplication via driver |
| 4.4 | Disaster failover workflow | Calls SetSource(force=true) via driver |
| 4.5 | Fail-forward error handling | Handles driver errors per DRGroup |

### Dependencies on Epic 3

| Component | Status | Used By This Story |
|-----------|--------|-------------------|
| `pkg/drivers/interface.go` — StorageProvider interface (7 methods) | Done | Not modified — stable contract |
| `pkg/drivers/registry.go` — Registry, GetDriver, GetDriverForPVC | Done | **Modified** — add fallback support |
| `pkg/drivers/errors.go` — ErrDriverNotFound | Done | Used for fallback branching |
| `pkg/drivers/noop/driver.go` — noop driver + init() registration | Done | Used as fallback target |
| `pkg/drivers/all/all.go` — central driver import | Done | Not modified |
| `pkg/drivers/conformance/suite.go` — conformance tests | Done | Not modified — conformance does not test fallback |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/drivers/registry.go` | `GetDriver` returns `ErrDriverNotFound` for unknown provisioners | Add `fallbackFactory` field, `SetFallbackDriver` method, fallback logic in `GetDriver`, clear in `ResetForTesting` |
| `internal/preflight/storage.go` | `TypedStorageBackendResolver` has `DriverMap StorageClassDriverMap`; `resolveVM` looks up `r.DriverMap[scName]` | Replace `DriverMap` with `Registry` + `SCLister`; delete `StorageClassDriverMap` type; rewrite `resolveVM` to use registry |
| `cmd/soteria/main.go` | Creates `TypedStorageBackendResolver{DriverMap: preflight.StorageClassDriverMap{}}` | Add `--noop-fallback` flag; create `KubeStorageClassLister`; wire registry + SCLister to resolver; conditionally set fallback |
| `pkg/drivers/registry_test.go` | Tests for RegisterDriver, GetDriver, GetDriverForPVC, ListRegistered, ResetForTesting | Add fallback tests |
| `internal/preflight/storage_test.go` | Tests use `StorageClassDriverMap` for resolver setup | Rewrite to use mock `StorageClassLister` + test registry |
| `test/integration/controller/suite_test.go` | Wires `StorageClassDriverMap` / `DriverMap` on resolver | Wire registry + `KubeStorageClassLister`; register drivers; create StorageClass fixtures |
| `test/integration/controller/drplan_preflight_test.go` | Asserts `StorageBackend == "odf"` etc. | Assert CSI provisioner name (e.g. `"noop.soteria.io"`) |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/drivers/interface.go` | Stable StorageProvider interface — no changes |
| `pkg/drivers/types.go` | Domain types — no changes |
| `pkg/drivers/errors.go` | Sentinel errors — no changes |
| `pkg/drivers/noop/driver.go` | Reference driver — no changes |
| `pkg/drivers/noop/doc.go` | Package docs — no changes |
| `pkg/drivers/fake/driver.go` | Test utility — no changes |
| `pkg/drivers/conformance/suite.go` | Conformance tests — no changes |
| `pkg/drivers/all/all.go` | Central import — no changes |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes (uses `StorageBackendResolver` interface) |
| `pkg/controller/drexecution/reconciler.go` | DRExecution controller skeleton — no changes |
| `internal/preflight/checks.go` | Report composition — no changes (consumes `StorageBackends map[string]string` from resolver) |

### Key Implementation Decisions

**1. Fallback lives in the Registry, not in preflight.**
The fallback mechanism belongs in `pkg/drivers/registry.go` because Story 4.1's execution controller will also need fallback behavior. If fallback were only in preflight, the execution path would need its own fallback logic. Single source of truth.

**2. `StorageClassLister` interface already exists.**
`pkg/drivers/registry.go` already defines `StorageClassLister` (line 37) with `GetProvisioner(ctx, storageClassName) (string, error)`. The `GetDriverForPVC` method already uses it. Create a real Kubernetes-backed implementation in `internal/preflight/` for production use.

**3. Backend name is the CSI provisioner name.**
The preflight report `PreflightVM.StorageBackend` field currently shows human-friendly names like `"odf"`. After convergence, it will show the CSI provisioner name (e.g., `"rook-ceph.rbd.csi.ceph.com"`, `"noop.soteria.io"`). This is more accurate and matches the registry key. The noop driver's provisioner is `"noop.soteria.io"` (defined in `pkg/drivers/noop/driver.go` line 15).

**4. `SetFallbackDriver` panics on double-call.**
Follows the same fail-fast-at-startup pattern as `RegisterDriver` — silent override is worse than a crash during initialization.

**5. Preflight resolver does NOT instantiate drivers.**
The resolver only needs to know *whether* a driver exists for a provisioner, not to create one. However, since `GetDriver` returns a fresh instance each call (factory pattern), and the resolver discards it, this is wasteful. Prefer: call `SCLister.GetProvisioner()` to get the provisioner name, then check if the registry has it registered. Consider adding a `HasDriver(provisionerName) bool` method to `Registry` for efficiency — it avoids allocating a driver instance just to check existence.

**6. The `--noop-fallback` flag defaults to false.**
Production environments must explicitly opt in. Dev/CI environments set `--noop-fallback=true` to function without real storage drivers. This mirrors the architecture's "no-op driver for dev/test/CI" design.

### RBAC

Confirm existing `storageclasses` get/list/watch in `config/rbac/role.yaml` satisfies `KubeStorageClassLister`. No manifest change expected — the DRPlan controller already needs StorageClass access. If `make manifests` adds new RBAC entries, include them.

### Code Patterns to Follow

**Startup logging** (for `cmd/soteria/main.go`): Use `setupLog` (`ctrl.Log.WithName("setup")`), not `log.FromContext(ctx)`. The reconcile context is not available at startup.

**Registry pattern** (from `pkg/drivers/registry.go`):

```go
func (r *Registry) SetFallbackDriver(factory DriverFactory) {
    if factory == nil {
        panic("SetFallbackDriver called with nil factory")
    }
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.fallbackFactory != nil {
        panic("fallback driver already set")
    }
    r.fallbackFactory = factory
}
```

**Error wrapping** (project convention):

```go
return nil, fmt.Errorf("resolving provisioner for storage class %q: %w", storageClassName, err)
```

**Structured logging** (project convention):

```go
log.FromContext(ctx).Info("Noop fallback enabled for unregistered provisioners")
log.FromContext(ctx).V(1).Info("Resolved storage backend via registry",
    "storageClass", scName, "provisioner", provisioner)
```

**Test pattern** (from `pkg/drivers/registry_test.go`):

```go
func TestRegistry_GetDriver_FallbackEnabled(t *testing.T) {
    reg := drivers.NewRegistry()
    reg.SetFallbackDriver(func() drivers.StorageProvider { return noop.New() })
    provider, err := reg.GetDriver("unknown-provisioner")
    if err != nil {
        t.Fatalf("expected fallback driver, got error: %v", err)
    }
    if provider == nil {
        t.Fatal("expected non-nil provider from fallback")
    }
}
```

### `StorageClassLister` Interface (Already Defined)

```go
// pkg/drivers/registry.go lines 37-39
type StorageClassLister interface {
    GetProvisioner(ctx context.Context, storageClassName string) (string, error)
}
```

The real implementation needs `storagev1client.StorageV1Interface`:

```go
type KubeStorageClassLister struct {
    Client storagev1client.StorageV1Interface
}

func (l *KubeStorageClassLister) GetProvisioner(ctx context.Context, storageClassName string) (string, error) {
    sc, err := l.Client.StorageClasses().Get(ctx, storageClassName, metav1.GetOptions{})
    if err != nil {
        return "", fmt.Errorf("fetching storage class %q: %w", storageClassName, err)
    }
    return sc.Provisioner, nil
}
```

### `main.go` Current Wiring (To Replace)

```go
// cmd/soteria/main.go lines 226-230 — DELETE this block:
storageResolver := &preflight.TypedStorageBackendResolver{
    Client:     mgr.GetClient(),
    CoreClient: clientset.CoreV1(),
    DriverMap:  preflight.StorageClassDriverMap{},
}

// REPLACE with:
scLister := &preflight.KubeStorageClassLister{Client: clientset.StorageV1()}
storageResolver := &preflight.TypedStorageBackendResolver{
    Client:     mgr.GetClient(),
    CoreClient: clientset.CoreV1(),
    Registry:   drivers.DefaultRegistry,
    SCLister:   scLister,
}
```

### Backwards Compatibility

- `StorageBackendResolver` interface (`ResolveBackends`) is unchanged — the DRPlan controller calls it the same way
- `CompositionInput.StorageBackends` map remains `map[string]string` — `checks.go` is unaffected
- `PreflightReport.VMs[].StorageBackend` format changes from human-friendly name to CSI provisioner name — this is acceptable as the field was always `"unknown"` in production (empty map)
- Existing integration tests that pass `StorageClassDriverMap` to the resolver will break and must be updated to use the new fields

### HasDriver vs GetDriver for Existence Check

Prefer `GetProvisioner` + `GetDriver` (discard the returned driver). The noop driver is cheap to construct. An optional `HasDriver(provisionerName) bool` on `Registry` is acceptable but must also check fallback — when fallback is set, it returns `true` for any provisioner.

### Previous Story Intelligence

**From Story 3.4 (Conformance Test Suite):**
- Conformance tests validate any `StorageProvider` — they are NOT affected by this story
- The conformance suite does not test fallback behavior

**From Story 3.1 (Interface & Registry):**
- `ResetForTesting()` clears all registered drivers — must also clear fallback factory
- `RegisterDriver` panics on double-registration — `SetFallbackDriver` should follow same pattern
- `DefaultRegistry` is the process-wide singleton — fallback is set on it from `main.go`

**From Story 2.4 (Pre-flight Plan Composition Check):**
- Created `TypedStorageBackendResolver` with `StorageClassDriverMap`
- The static map was always intended as a temporary solution — Epic 3 retro item #9 explicitly marks it "Resolves via Story 4.05"

**From Epic 3 Retrospective:**
- Story 4.05 was specifically carved out to converge these paths before Story 4.1
- All prep tasks completed: `drivers/all`, `main.go` import, DRExecution skeleton
- Sequencing: 4.05 → 4.1 → 4.2 → ... (driver resolution must be settled first)

### Git Intelligence

Recent work: Epic 3 retro (`c284494` — `drivers/all` wiring, DRExecution skeleton, Story 4.05), Story 3.4 conformance (`0758797`), Story 3.1 registry (`4c78776`), Story 2.4 preflight (`96d9138` — created `TypedStorageBackendResolver` with `StorageClassDriverMap`). Commit messages follow pattern: `Implement Story X.Y: Short Description`.

### Project Structure Notes

All files align with the architecture document's directory structure:
- `pkg/drivers/registry.go` — driver registration and resolution (public API for external drivers)
- `internal/preflight/storage.go` — storage backend resolution (internal, not importable by drivers)
- `cmd/soteria/main.go` — wiring (flag parsing, registry configuration, resolver construction)

The `StorageClassLister` implementation (`KubeStorageClassLister`) belongs in `internal/preflight/` because it is a Kubernetes-specific implementation detail. External driver authors should never need it — they register via `init()` + `RegisterDriver`.

### Build Commands

```bash
make test         # All unit tests (new + existing)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make build        # Verify compilation
make integration  # Integration tests (if applicable)
```

### References

- [Source: _bmad-output/implementation-artifacts/epic-3-retro-2026-04-17.md#Story 4.05] — Story rationale, scope, sequencing, prep tasks
- [Source: _bmad-output/implementation-artifacts/epic-3-retro-2026-04-17.md#Epic 2b Follow-Through] — Item #9: static StorageClassDriverMap resolves via Story 4.05
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — Registration via init() + registry, typed errors, idempotency
- [Source: _bmad-output/planning-artifacts/architecture.md#Architectural Boundaries] — Driver boundary at `pkg/drivers/interface.go`
- [Source: _bmad-output/planning-artifacts/architecture.md#Requirements Coverage] — FR9–FR19 coverage includes `internal/preflight/`
- [Source: _bmad-output/planning-artifacts/prd.md#FR21] — Driver selection is implicit from PVC storage class
- [Source: _bmad-output/planning-artifacts/prd.md#FR23] — No-op driver for dev/test/CI
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — Registration at startup, implicit from PVC, no StorageProviderConfig CRD
- [Source: _bmad-output/project-context.md#Testing Rules] — envtest over fake client, test naming conventions
- [Source: pkg/drivers/registry.go] — Current registry implementation with GetDriver, GetDriverForPVC, StorageClassLister
- [Source: internal/preflight/storage.go] — Current TypedStorageBackendResolver with StorageClassDriverMap
- [Source: cmd/soteria/main.go#L226-230] — Current wiring: empty StorageClassDriverMap

### Review Findings

- [x] [Review][Patch] Nil `Registry` pointer dereference — added `r.Registry == nil` guard in `resolveProvisioner` + `TestResolveBackends_NilRegistry` test [internal/preflight/storage.go]
- [x] [Review][Patch] Nil `KubeStorageClassLister.Client` panic — added `l.Client == nil` guard + `TestKubeStorageClassLister_NilClient` test [internal/preflight/sc_lister.go]
- [x] [Review][Patch] Empty provisioner string from StorageClassLister — added `provisioner == ""` guard in `resolveProvisioner` + `TestResolveBackends_EmptyProvisioner` test [internal/preflight/storage.go]
- [x] [Review][Defer] CSI vs in-tree provisioner ambiguity — `sc.Provisioner` used verbatim; legacy/migrated clusters may have non-CSI provisioner strings that don't match registry keys [internal/preflight/sc_lister.go] — deferred, pre-existing
- [x] [Review][Patch] Missing empty-provisioner guard in `GetDriverForPVC` — added `provisioner == ""` guard + `TestRegistry_GetDriverForPVC_EmptyProvisioner` test [pkg/drivers/registry.go]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor Agent)

### Debug Log References

No debug issues encountered. All tests passed on first run after implementation.

### Completion Notes List

- Added `fallbackFactory` field to `Registry` struct with `SetFallbackDriver` method following fail-fast-at-startup pattern (panics on nil or double-call)
- `GetDriver` returns fallback driver for unknown provisioners when fallback is set; `GetDriverForPVC` inherits this behavior via delegation
- `ListRegistered` excludes fallback; `ResetForTesting` clears fallback
- Added 10 new registry unit tests covering all fallback scenarios (100% coverage on `pkg/drivers`)
- Deleted `StorageClassDriverMap` type entirely from `internal/preflight/storage.go`
- Refactored `TypedStorageBackendResolver` to use `*drivers.Registry` + `drivers.StorageClassLister` instead of static map
- Extracted provisioner resolution into `resolveProvisioner()` helper that maps SC → provisioner → registry lookup
- Created `KubeStorageClassLister` in `internal/preflight/sc_lister.go` backed by `StorageV1Interface`
- Added `--noop-fallback` flag to `cmd/soteria/main.go` (default false); wired `SetFallbackDriver` + startup logging
- Rewrote all preflight unit tests to use `fakeSCLister` + test `Registry`; added `TestResolveBackends_FallbackEnabled` and `TestResolveBackends_NilSCLister`
- Updated integration test suite: replaced `StorageClassDriverMap` with registry + `KubeStorageClassLister`; created `StorageClass` objects in envtest (`test-odf`, `noop-storage` → `noop.soteria.io`); updated assertions from `"odf"` to `noop.ProvisionerName`

### Change Log

- 2026-04-19: Implemented Story 4.05 — Driver Registry Fallback & Preflight Convergence

### File List

- `pkg/drivers/registry.go` — Added fallbackFactory field, SetFallbackDriver, updated GetDriver/ResetForTesting, added package-level SetFallbackDriver
- `pkg/drivers/registry_test.go` — Added 10 fallback tests (enabled/disabled, panic, reset, package-level, GetDriverForPVC)
- `internal/preflight/storage.go` — Deleted StorageClassDriverMap; replaced DriverMap with Registry+SCLister; added resolveProvisioner helper
- `internal/preflight/sc_lister.go` — New file: KubeStorageClassLister implementation
- `internal/preflight/sc_lister_test.go` — New file: nil-Client guard test for KubeStorageClassLister
- `internal/preflight/storage_test.go` — Rewrote all tests to use fakeSCLister + Registry; added fallback, nil-lister, nil-registry, and empty-provisioner tests
- `internal/preflight/doc.go` — Updated package doc to mention registry-based resolution
- `cmd/soteria/main.go` — Added --noop-fallback flag, SetFallbackDriver wiring, startup logging, KubeStorageClassLister creation
- `test/integration/controller/suite_test.go` — Replaced StorageClassDriverMap with Registry+KubeStorageClassLister; created StorageClass fixtures
- `test/integration/controller/drplan_preflight_test.go` — Updated assertions from "odf" to noop.ProvisionerName
