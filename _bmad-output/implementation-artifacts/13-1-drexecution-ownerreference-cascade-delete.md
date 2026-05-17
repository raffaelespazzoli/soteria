# Story 13.1: DRExecution OwnerReference for Cascade Delete

Status: ready-for-dev

## Story

As a platform engineer,
I want DRExecution objects to have an OwnerReference pointing to their DRPlan,
So that deleting a DRPlan automatically cascade-deletes all its DRExecutions.

## Background

Both DRPlan and DRExecution are cluster-scoped custom resources stored in the aggregated API server backed by ScyllaDB. Today, DRExecution references its DRPlan via `spec.planName` (string) and carries a `soteria.io/plan-name` label stamped in `PrepareForCreate`. However, there is no Kubernetes OwnerReference, meaning deleting a DRPlan leaves orphaned DRExecution audit records that require manual cleanup.

This story adds a controller OwnerReference on DRExecution at creation time, enabling Kubernetes garbage collection to cascade-delete all DRExecutions when their owning DRPlan is deleted. This is safe for cluster-scoped → cluster-scoped ownership (both resources live in the same scope).

**Important context:** The aggregated API server stores resources in ScyllaDB, but the Kubernetes garbage collector (GC) works through the standard API — it watches resources, detects OwnerReferences, and issues DELETE calls through the kube-apiserver proxy. The GC does not need to understand the storage backend. As long as OwnerReferences are set correctly in the object metadata, cascade delete works identically to etcd-backed resources.

## Acceptance Criteria

1. **AC1: OwnerReference set on creation**
   - **Given** a DRExecution is created via the aggregated API server
   - **When** `PrepareForCreate` in `pkg/registry/drexecution/strategy.go` processes the resource
   - **Then** it sets a controller OwnerReference on the DRExecution with the DRPlan as owner (using `metav1.OwnerReference` with `Controller: true`, `BlockOwnerDeletion: true`)
   - **And** the DRPlan is fetched by `spec.planName` to resolve the UID

2. **AC2: Cascade delete behavior**
   - **Given** a DRPlan with one or more DRExecutions
   - **When** the DRPlan is deleted
   - **Then** Kubernetes garbage collection cascade-deletes all owned DRExecutions
   - **And** no manual cleanup of DRExecution resources is required

3. **AC3: Backward compatibility**
   - **Given** existing DRExecution resources without an OwnerReference
   - **When** the updated reconciler processes them
   - **Then** they continue to function normally (OwnerReference is NOT retroactively added — only new executions get it)

4. **AC4: Tests**
   - **Given** the DRExecution strategy and reconciler
   - **When** unit and integration tests run
   - **Then** PrepareForCreate tests verify OwnerReference is set with correct plan UID
   - **And** integration tests verify cascade delete behavior
   - **And** all existing tests pass with zero regressions

## Tasks / Subtasks

- [ ] Task 1: Add PlanGetter to strategy (AC: #1)
  - [ ] 1.1 Add `rest.Getter` field to `drexecutionStrategy` struct
  - [ ] 1.2 Create exported setter `SetPlanStorage(rest.Getter)` on `Strategy`
  - [ ] 1.3 Wire `SetPlanStorage` in `pkg/apiserver/apiserver.go` after `drplanStore` is created

- [ ] Task 2: Set OwnerReference in PrepareForCreate (AC: #1)
  - [ ] 2.1 In `PrepareForCreate`, fetch DRPlan via `planGetter.Get(ctx, exec.Spec.PlanName, &metav1.GetOptions{})`
  - [ ] 2.2 Build `metav1.OwnerReference` with plan Name, UID, APIVersion, Kind, Controller=true, BlockOwnerDeletion=true
  - [ ] 2.3 Set `exec.OwnerReferences = []metav1.OwnerReference{ownerRef}`
  - [ ] 2.4 If plan fetch fails (nil getter or Get error), log warning and proceed without OwnerReference (graceful degradation)

- [ ] Task 3: Unit tests for strategy (AC: #4)
  - [ ] 3.1 Test OwnerReference is set with correct UID when plan exists
  - [ ] 3.2 Test OwnerReference Controller=true, BlockOwnerDeletion=true
  - [ ] 3.3 Test graceful degradation when plan getter is nil
  - [ ] 3.4 Test graceful degradation when plan is not found
  - [ ] 3.5 Verify all existing PrepareForCreate tests still pass (no regressions)

- [ ] Task 4: Integration tests for cascade delete (AC: #2, #4)
  - [ ] 4.1 Create DRPlan, create DRExecution, verify OwnerReference is present
  - [ ] 4.2 Delete DRPlan, verify DRExecution is cascade-deleted by GC

- [ ] Task 5: Backward compatibility verification (AC: #3)
  - [ ] 5.1 Verify existing DRExecution without OwnerReference continues to function
  - [ ] 5.2 No retroactive OwnerReference addition in reconciler

- [ ] Task 6: Verify (AC: all)
  - [ ] 6.1 Run `make test` — all tests pass
  - [ ] 6.2 Run `make lint-fix && make lint` — zero lint issues
  - [ ] 6.3 Run `make manifests generate` — codegen clean (no diff)

## Dev Notes

### Key Source Files

| File | Action | Purpose |
|------|--------|---------|
| `pkg/registry/drexecution/strategy.go` | **Modified** | Add `planGetter` field, `SetPlanStorage`, OwnerReference in `PrepareForCreate` |
| `pkg/registry/drexecution/strategy_test.go` | **Modified** | Add OwnerReference unit tests |
| `pkg/apiserver/apiserver.go` | **Modified** | Wire `Strategy.SetPlanStorage(drplanStore)` |
| Integration test file (existing or new) | **Modified** | Cascade delete integration test |

### Architecture & Design Decisions

**Strategy struct needs state:** The current `drexecutionStrategy` is a stateless struct with embedded `ObjectTyper` and `NameGenerator`. Adding a `planGetter rest.Getter` field makes it stateful. This follows the same pattern as `DRPlanTableConvertor` which has a `drexecutionLister` field injected via `SetDRExecutionStorage`. The `Strategy` package-level var remains the singleton; `SetPlanStorage` is called once during API server initialization.

**OwnerReference fields for cluster-scoped resources:**

```go
metav1.OwnerReference{
    APIVersion:         soteriav1alpha1.SchemeGroupVersion.String(), // "soteria.io/v1alpha1"
    Kind:               "DRPlan",
    Name:               plan.Name,
    UID:                 plan.UID,
    Controller:         pointer.Bool(true),
    BlockOwnerDeletion: pointer.Bool(true),
}
```

- `Controller: true` — marks this as the managing controller (only one per object)
- `BlockOwnerDeletion: true` — foreground cascade deletion waits for dependents
- APIVersion must be the full group/version string, not just the group

**Graceful degradation is critical:** `PrepareForCreate` runs in the hot path of every DRExecution creation. If the plan getter is nil (e.g., during tests or initialization race) or the DRPlan GET fails, the strategy must log a warning and proceed without setting the OwnerReference. The DRExecution is still valid without it — the `soteria.io/plan-name` label and `spec.planName` field provide the logical association. OwnerReference is an optimization for cleanup, not a correctness requirement.

**Why not set OwnerReference in the admission plugin?** The admission plugin (`pkg/admission/plugin.go`) already fetches the DRPlan for validation. However, admission plugins are for validation, not mutation. The `PrepareForCreate` strategy is the correct Kubernetes pattern for server-side mutation before storage. The admission plugin's plan GET provides validation (plan exists, correct phase, no concurrent execution); the strategy's plan GET provides mutation (OwnerReference UID). These are separate concerns running in the same request pipeline. The strategy's GET is a second round-trip, but it's a local in-process call to the same storage backend.

**Pointer helper:** Use `k8s.io/utils/ptr` package (`ptr.To(true)`) for the `*bool` fields on `OwnerReference`. This package is already a transitive dependency via controller-runtime. Alternatively, use `&[]bool{true}[0]` or a local helper if the import is not already present — check `go.mod` first.

### What NOT to Change

- **DRExecution reconciler** — no retroactive OwnerReference patching on existing executions (AC3)
- **Admission plugin** — it already validates plan existence; don't duplicate OwnerReference logic there
- **DRPlan controller** — it does not need to know about this; GC handles cascade
- **DRExecution types** — OwnerReferences are part of `metav1.ObjectMeta`, no schema changes needed
- **No `make manifests`-visible changes** — OwnerReference is set at runtime, not in CRD schema

### Testing Patterns

**Unit tests (strategy_test.go):** Follow the existing table-driven pattern in `strategy_test.go`. The existing tests use `context.Background()` and construct `DRExecution` objects directly. For the new tests:

- Create a mock `rest.Getter` that returns a DRPlan with a known UID
- Call `Strategy.PrepareForCreate(ctx, exec)` and assert `exec.OwnerReferences`
- Reset `Strategy`'s planGetter to nil between tests to avoid state leakage

**Mock rest.Getter pattern:** Implement a minimal `rest.Getter` interface:

```go
type stubPlanGetter struct {
    plan *soteriav1alpha1.DRPlan
    err  error
}

func (s *stubPlanGetter) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
    if s.err != nil {
        return nil, s.err
    }
    if s.plan != nil && s.plan.Name == name {
        return s.plan, nil
    }
    return nil, apierrors.NewNotFound(soteriav1alpha1.Resource("drplans"), name)
}
```

**Integration test for cascade delete:** The aggregated API server integration tests in this project use envtest. Create a DRPlan, create a DRExecution, verify the OwnerReference, then delete the DRPlan and wait for the GC to delete the DRExecution. Note: envtest may or may not run the garbage collector — if GC is not available in the test environment, verify the OwnerReference is correctly set and document that cascade behavior is verified by Kubernetes GC contract (standard K8s behavior, not custom logic).

### Previous Story Intelligence

**Story 12.6 (Conformance Suite & Integration Testing)** — the last completed story in Epic 12. Key patterns:
- Tests use envtest with a fake client for CRD types (VR/VGR). Same pattern applies here for the `rest.Getter` mock
- `conformanceAdapter` bridges model deviations — similar adapter thinking applies if GC isn't available in envtest
- Coverage was tracked (90.9%). Maintain or improve coverage in modified files

**Epic 12 overall** — established the CSI Extension driver. No OwnerReference patterns were introduced. This is the first story using OwnerReference in the Soteria codebase.

**Sprint status context** — Epic 12 is done. Epic 13 is the new epic starting with this story. All 6 stories in Epic 13 relate to VolumeReplication lifecycle management. Story 13.1 is the simplest — it only touches the DRExecution creation path and doesn't involve VR/VGR at all.

### Dependency Injection Wiring

The wiring in `pkg/apiserver/apiserver.go` follows the established pattern. Currently:

1. `drplanStore` is created via `drplanregistry.NewREST()`
2. `drplanStore` is injected into `SoteriaPlugin.SetDRPlanStorage(drplanStore)`
3. `drexecStore` is created via `drexecutionregistry.NewREST()`
4. `drexecStore` is injected into `SoteriaPlugin.SetDRExecutionStorage(drexecStore)`
5. `drexecStore` is injected into `drplanTC.SetDRExecutionStorage(drexecStore)`

Add between steps 1 and 3:
```
drexecutionregistry.Strategy.SetPlanStorage(drplanStore)
```

This must happen before `drexecutionregistry.NewREST()` because `NewREST` references `Strategy` as the `CreateStrategy`. The `Strategy` singleton is initialized at package init time with nil planGetter; the setter fills it before any requests arrive.

### Build Commands

```bash
make test                    # All unit + integration tests
make lint-fix && make lint   # Lint
make manifests generate      # Codegen (should produce no diff)
```

### Project Structure Notes

- `pkg/registry/drexecution/strategy.go` — strategy mutation logic (PrepareForCreate, PrepareForUpdate)
- `pkg/registry/drexecution/storage.go` — REST storage, audit protection, table convertor
- `pkg/apiserver/apiserver.go` — API server initialization, storage wiring, admission chain
- `pkg/admission/plugin.go` — in-process admission plugin (validation only)
- `pkg/apis/soteria.io/v1alpha1/types.go` — CRD type definitions (no changes needed)

### References

- [Source: pkg/registry/drexecution/strategy.go] — current PrepareForCreate implementation (lines 45-64)
- [Source: pkg/registry/drexecution/strategy_test.go] — existing unit tests (20 tests)
- [Source: pkg/apiserver/apiserver.go] — API server wiring (lines 119-154)
- [Source: pkg/admission/plugin.go] — admission plugin DRPlan fetch pattern (lines 117-135)
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 13, Story 13.1] — acceptance criteria
- [Source: _bmad-output/project-context.md] — project rules and patterns

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
