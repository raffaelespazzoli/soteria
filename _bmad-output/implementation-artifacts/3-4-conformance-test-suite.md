# Story 3.4: Conformance Test Suite

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a storage vendor engineer,
I want a conformance test suite that validates the full DR lifecycle against any driver,
So that I can prove my driver implementation is correct before submitting it.

## Acceptance Criteria

1. **AC1 — Full DR lifecycle validation:** `pkg/drivers/conformance/suite.go` exercises the complete DR lifecycle in sequence against any `StorageProvider` implementation: CreateVolumeGroup → SetSource → GetReplicationStatus → StopReplication → SetTarget → GetReplicationStatus → StopReplication → DeleteVolumeGroup (FR24).

2. **AC2 — Clear failure messages:** When any method returns an unexpected error, the test fails with a clear message identifying which lifecycle step failed, the method name, the volume group ID, and the error returned.

3. **AC3 — No-op driver passes:** Running the conformance suite against the no-op driver (Story 3.2) results in all tests passing — confirming the no-op driver is a valid reference implementation.

4. **AC4 — Idempotency verification:** Each of the 7 methods is called twice in succession during the test and the second call succeeds without error, verifying all methods are idempotent as architecturally required.

5. **AC5 — Context cancellation:** Each method respects `context.Context` cancellation and returns promptly (or with a context error) when the context is cancelled before invocation.

6. **AC6 — Vendor-friendly execution:** The suite is designed so that a vendor engineer can wire their driver and run it with a clear, documented invocation pattern. The suite exposes a `RunConformance(t *testing.T, provider drivers.StorageProvider)` function that any `_test.go` file can call with any driver instance.

7. **AC7 — Package documentation:** `pkg/drivers/conformance/doc.go` has comprehensive godoc explaining the suite's purpose, what it validates, how vendors wire their driver, and example usage.

## Tasks / Subtasks

- [x] Task 1: Define the conformance suite runner function (AC: #1, #2, #6)
  - [x] 1.1 In `pkg/drivers/conformance/suite.go`, define `RunConformance(t *testing.T, provider drivers.StorageProvider)` as the entry point
  - [x] 1.2 The function accepts any `StorageProvider` implementation — no dependency on the registry or `init()` registration
  - [x] 1.3 Each lifecycle step is a named `t.Run` subtest for clear failure reporting
  - [x] 1.4 The suite shares state (volume group ID) across subtests via closure variables — subtests run sequentially, not in parallel

- [x] Task 2: Implement full DR lifecycle test (AC: #1, #2)
  - [x] 2.1 **Step 1 — CreateVolumeGroup:** Call `CreateVolumeGroup(ctx, spec)` with a test `VolumeGroupSpec`. Verify a non-empty `VolumeGroupID` is returned. Store the ID for subsequent steps
  - [x] 2.2 **Step 2 — SetSource:** Call `SetSource(ctx, vgID, SetSourceOptions{Force: false})`. Verify nil error (establishes or confirms source role for replication)
  - [x] 2.3 **Step 3 — GetReplicationStatus (source):** Call `GetReplicationStatus(ctx, vgID)`. Verify nil error and `Role == RoleSource` (and `Health == HealthHealthy` when applicable)
  - [x] 2.4 **Step 4 — StopReplication:** Call `StopReplication(ctx, vgID, StopReplicationOptions{Force: false})`. Verify nil error
  - [x] 2.5 **Step 5 — SetTarget:** Call `SetTarget(ctx, vgID, SetTargetOptions{Force: false})`. Verify nil error (demotes to target / establishes target role)
  - [x] 2.6 **Step 6 — GetReplicationStatus (target):** Call `GetReplicationStatus(ctx, vgID)`. Verify nil error and `Role == RoleTarget` (health may be `HealthDegraded` or `HealthSyncing` depending on driver)
  - [x] 2.7 **Step 7 — StopReplication (again):** Call `StopReplication(ctx, vgID, StopReplicationOptions{Force: false})`. Verify nil error (idempotent stop; replaces the old resync/disable split for conformance)
  - [x] 2.8 **Step 8 — DeleteVolumeGroup:** Call `DeleteVolumeGroup(ctx, vgID)`. Verify nil error
  - [x] 2.9 **Step 9 — GetVolumeGroup (deleted):** Call `GetVolumeGroup(ctx, vgID)`. Verify `drivers.ErrVolumeGroupNotFound` is returned (confirms cleanup)

- [x] Task 3: Implement idempotency test (AC: #4)
  - [x] 3.1 In `suite.go`, define a subtest `Idempotency` within `RunConformance` (runs after the lifecycle test)
  - [x] 3.2 Create a fresh volume group for idempotency testing
  - [x] 3.3 For each of the 7 methods, call it twice in succession with the same arguments:
    - `CreateVolumeGroup` — two calls, both succeed (may return different IDs)
    - `GetVolumeGroup` — two calls with the same ID, both return same result
    - `SetSource` — two calls (`SetSourceOptions{Force: false}`), second is a no-op
    - `SetTarget` — two calls (`SetTargetOptions{Force: false}`), second is a no-op
    - `StopReplication` — two calls (`StopReplicationOptions{Force: false}`), second is a no-op
    - `GetReplicationStatus` — two calls, both succeed
    - `DeleteVolumeGroup` — two calls, second returns nil (idempotent delete)
  - [x] 3.4 Each double-call is a named `t.Run` subtest: `"Idempotency/CreateVolumeGroup"`, etc.

- [x] Task 4: Implement context cancellation test (AC: #5)
  - [x] 4.1 In `suite.go`, define a subtest `ContextCancellation` within `RunConformance`
  - [x] 4.2 For each of the 7 methods, create a pre-cancelled context (`context.WithCancel` + immediate `cancel()`) and call the method
  - [x] 4.3 Verify the method returns an error (either `context.Canceled` or a wrapped context error) — the method must not hang or succeed when the context is already cancelled
  - [x] 4.4 Each method test is a named `t.Run` subtest: `"ContextCancellation/CreateVolumeGroup"`, etc.
  - [x] 4.5 Use a separate volume group (created with a valid context before the cancellation subtests) for methods that require an existing volume group

- [x] Task 5: Implement error condition tests (AC: #2)
  - [x] 5.1 In `suite.go`, define a subtest `ErrorConditions` within `RunConformance`
  - [x] 5.2 Test `GetVolumeGroup` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [x] 5.3 Test `SetSource` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [x] 5.4 Test `SetTarget` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [x] 5.5 Test `StopReplication` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [x] 5.6 Test `GetReplicationStatus` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`

- [x] Task 6: Create no-op driver conformance test (AC: #3)
  - [x] 6.1 In `pkg/drivers/conformance/noop_test.go`, create `TestConformance_NoopDriver`
  - [x] 6.2 Import `pkg/drivers/noop` (side-effect import not needed — instantiate directly with `noop.New()`)
  - [x] 6.3 Call `RunConformance(t, noop.New())`
  - [x] 6.4 This test file serves as the example for vendor engineers wiring their own driver

- [x] Task 7: Update package documentation (AC: #7)
  - [x] 7.1 Update `pkg/drivers/conformance/doc.go` with comprehensive godoc: purpose, what the suite validates (lifecycle, idempotency, context cancellation, error conditions), how to wire a driver, example test file, run command

- [x] Task 8: Verify build and tests (AC: #3)
  - [x] 8.1 Run `go test ./pkg/drivers/conformance/...` — all conformance tests pass against no-op driver
  - [x] 8.2 Run `make test` — all unit tests pass (new + existing)
  - [x] 8.3 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [x] 8.4 Run `make build` — compiles cleanly

### Review Findings

- [x] [Review][Patch] Stop lifecycle after the first failing step [`pkg/drivers/conformance/suite.go`]
- [x] [Review][Patch] Include method, volume group ID, and returned error in conformance failure messages [`pkg/drivers/conformance/suite.go`]
- [x] [Review][Patch] Delete the setup volume group after context-cancellation tests to avoid leaking backend state [`pkg/drivers/conformance/suite.go`]
- [x] [Review][Patch] Make the `GetVolumeGroup` idempotency check compare the full returned result, not just the ID [`pkg/drivers/conformance/suite.go`]
- [x] [Review][Patch] Assert healthy source replication status when the driver reports source status immediately after `SetSource` [`pkg/drivers/conformance/suite.go`]

## Dev Notes

### Architecture Context

This is Story 4 of Epic 3 (Storage Driver Framework & Reference Implementations). It is the capstone story that validates the entire driver framework built in Stories 3.1–3.3. The conformance suite lives at `pkg/drivers/conformance/suite.go` as specified in the architecture document and is the primary validation tool for FR24: "Storage vendor engineer can implement a new StorageProvider driver by implementing the 7-method Go interface and running the conformance test suite."

**Epic 3 story chain:**

| Story | Deliverable | Relationship |
|---|---|---|
| 3.1 | Interface, types, errors, registry | Foundation — defines the 7-method contract |
| 3.2 | No-op driver | Reference implementation — passes conformance |
| 3.3 | Fake driver | Unit test utility — does NOT pass conformance (programmable, not simulating real behavior) |
| **3.4** | **Conformance suite** | **Validates drivers implement the contract correctly** |

### Dependencies on Stories 3.1–3.2

The conformance suite depends on the StorageProvider interface and no-op driver from previous stories:

| From Story | File | Used By |
|---|---|---|
| 3.1 | `pkg/drivers/interface.go` — `StorageProvider` interface | `RunConformance` accepts any `StorageProvider` |
| 3.1 | `pkg/drivers/types.go` — `VolumeGroupID`, `VolumeGroupSpec`, `VolumeGroupInfo`, `ReplicationStatus`, `VolumeRole`, `ReplicationHealth`, `SetSourceOptions`, `SetTargetOptions`, `StopReplicationOptions` | Test assertions on return values, replication roles, and health |
| 3.1 | `pkg/drivers/errors.go` — `ErrVolumeGroupNotFound`, `ErrInvalidTransition` | Error condition tests |
| 3.2 | `pkg/drivers/noop/driver.go` — `New() *Driver` | `noop_test.go` instantiates no-op driver for conformance validation |

### Suite Design — Why `RunConformance(t, provider)` Not Ginkgo

The conformance suite uses standard `testing.T` (not Ginkgo/Gomega) because:
- **Vendor accessibility:** External driver engineers should not be forced to learn Ginkgo. Standard `testing.T` is universally understood.
- **Minimal dependencies:** The conformance package should have no dependencies beyond `testing`, `context`, `errors`, and `pkg/drivers/`. No Ginkgo/Gomega import in the conformance package itself.
- **Composability:** Vendor test files call `RunConformance(t, myDriver)` — works with any Go test runner, CI system, or IDE.
- **Subtests for structure:** Use `t.Run("Lifecycle/CreateVolumeGroup", ...)` for structured output without Ginkgo `Describe`/`It`.

The no-op conformance test file (`noop_test.go`) also uses standard `testing.T` for consistency with the suite.

### Lifecycle Test State Management

The lifecycle test creates ONE volume group and threads its ID through all lifecycle steps. Steps are sequential subtests sharing state via closure variables. This is intentional:
- The DR lifecycle is inherently sequential (for example, you cannot set target before establishing source when the contract requires that ordering).
- Each step depends on the state left by the previous step.
- If any step fails, subsequent steps are skipped via `t.FailNow()` (no point testing `SetSource` if create failed).

The idempotency test creates a SEPARATE volume group to avoid interference with the lifecycle test.

### Context Cancellation Test Strategy

The context cancellation test verifies that driver methods respect `context.Context` semantics:
- Create a pre-cancelled context via `ctx, cancel := context.WithCancel(context.Background()); cancel()`
- Call each method with this cancelled context
- The method MUST return an error — it should not succeed or block indefinitely
- We verify `err != nil` but do NOT mandate a specific error type (drivers may return `context.Canceled`, a wrapped context error, or their own timeout error)

For methods that need an existing volume group (all except `CreateVolumeGroup`), create the volume group with a valid context first, then test cancellation separately.

### Error Condition Test Strategy

Tests that a driver correctly returns `drivers.ErrVolumeGroupNotFound` for operations on nonexistent volume group IDs. Uses `errors.Is()` for assertion, following the established pattern from `pkg/drivers/credentials_test.go`. The nonexistent ID uses a synthetic value: `drivers.VolumeGroupID("conformance-nonexistent-vgid")`. Where the contract forbids a transition (for example, `SetTarget` before `SetSource`), drivers should return `drivers.ErrInvalidTransition` — assert with `errors.Is` when adding those scenarios.

`DeleteVolumeGroup` with a nonexistent ID is NOT tested in error conditions because the idempotency contract requires it to return nil (not an error) for missing IDs.

### VolumeGroupSpec for Test

Create a minimal `VolumeGroupSpec` for conformance tests. The spec contents don't matter for conformance validation — what matters is that the driver accepts it and returns a valid ID. Use descriptive labels to identify conformance test resources:

```go
spec := drivers.VolumeGroupSpec{
    Namespace: "conformance-test",
    Labels:    map[string]string{"conformance": "true"},
}
```

### Existing Code to Preserve

| File | Contents | Action |
|------|----------|--------|
| `pkg/drivers/conformance/doc.go` | Package stub with license header | **Modify** — update doc comment |
| `pkg/drivers/credentials.go` | CredentialSource, CredentialResolver interface | Preserve — do not modify |
| `pkg/drivers/credentials_secret.go` | SecretCredentialResolver, credential errors | Preserve — do not modify |
| `pkg/drivers/credentials_test.go` | Credential resolver tests | Preserve — do not modify |
| Story 3.1 files (interface.go, types.go, errors.go, registry.go) | Story 3.1 deliverables | Preserve — do not modify |
| Story 3.2 files (noop/driver.go, noop/doc.go) | Story 3.2 deliverables | Preserve — do not modify |
| Story 3.3 files (fake/driver.go, fake/doc.go) | Story 3.3 deliverables | Preserve — do not modify |

### Files NOT to Modify

- `pkg/drivers/interface.go` — Story 3.1 deliverable
- `pkg/drivers/types.go` — Story 3.1 deliverable
- `pkg/drivers/errors.go` — Story 3.1 deliverable
- `pkg/drivers/registry.go` — Story 3.1 deliverable
- `pkg/drivers/doc.go` — Package documentation
- `pkg/drivers/credentials.go` — existing credential types
- `pkg/drivers/credentials_secret.go` — existing credential resolver
- `pkg/drivers/credentials_test.go` — existing tests
- `pkg/drivers/noop/` — Story 3.2 deliverable (all files)
- `pkg/drivers/fake/` — Story 3.3 deliverable (all files)
- `cmd/soteria/main.go` — no wiring changes
- Any controller, engine, admission, or storage code

### File Structure

| File | Purpose | New/Modified |
|------|---------|-------------|
| `pkg/drivers/conformance/suite.go` | `RunConformance(t, provider)` entry point, lifecycle test, idempotency test, context cancellation test, error conditions test | **New** |
| `pkg/drivers/conformance/doc.go` | Updated package documentation with vendor instructions | **Modified** |
| `pkg/drivers/conformance/noop_test.go` | Conformance validation of no-op driver — also serves as vendor example | **New** |

### Code Patterns to Follow

**Subtest naming** (from project conventions):

```go
t.Run("Lifecycle/CreateVolumeGroup", func(t *testing.T) {
    // ...
})
t.Run("Idempotency/SetSource", func(t *testing.T) {
    // ...
})
```

**Error assertion** (from `pkg/drivers/credentials_test.go`):

```go
if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
    t.Fatalf("expected error wrapping %v, got: %v", drivers.ErrVolumeGroupNotFound, err)
}
```

**Fail-fast pattern** for sequential lifecycle:

```go
vgID, err := provider.CreateVolumeGroup(ctx, spec)
if err != nil {
    t.Fatalf("CreateVolumeGroup failed: %v", err)
}
```

Using `t.Fatalf` (not `t.Errorf`) ensures subsequent lifecycle steps are skipped when a prerequisite step fails.

**License header** (from existing files):

```go
/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 ...
*/
```

### Fake Driver Does NOT Pass Conformance

The fake driver (Story 3.3) is intentionally excluded from conformance testing. It is a programmable test utility that returns pre-configured responses — it does not simulate real storage behavior. The conformance suite validates that a driver correctly implements the full DR lifecycle with role and health tracking, which the fake driver does not do (it's stateless by design).

### Build Commands

```bash
go test ./pkg/drivers/conformance/...   # Conformance suite against no-op driver
make test                               # All unit tests (new + existing)
make lint-fix                           # Auto-fix code style
make lint                               # Verify lint passes
make build                              # Verify compilation
```

### Previous Story Intelligence

**From Story 3.1 (Interface & Registry):**
- Domain types use strong typing (`drivers.VolumeGroupID`, not `string`) — the conformance suite must use these types
- Error returns use sentinel errors from `pkg/drivers/errors.go` — test with `errors.Is` (including `ErrInvalidTransition` where invalid role transitions are expected)
- `VolumeRole` constants: `RoleNonReplicated`, `RoleSource`, `RoleTarget`
- `ReplicationHealth` constants: `HealthHealthy`, `HealthDegraded`, `HealthSyncing`, `HealthUnknown`
- `ReplicationStatus` struct holds `Role`, `Health`, `LastSyncTime`, `EstimatedRPO`
- `SetSourceOptions`, `SetTargetOptions`, and `StopReplicationOptions` each include a `Force bool` field
- `VolumeGroupSpec` struct for `CreateVolumeGroup` input

**From Story 3.2 (No-Op Driver):**
- The no-op driver is STATEFUL (tracks volume groups, replication roles and health in-memory)
- `New() *Driver` constructor creates a fresh instance — use this in `noop_test.go`
- The no-op driver returns `drivers.ErrVolumeGroupNotFound` for unknown volume group IDs
- After `StopReplication` followed by `SetTarget`, the no-op driver reflects `RoleTarget` (and may report `HealthSyncing` then settle to `HealthHealthy`) immediately (no async) — align assertions with the no-op contract
- Idempotency is built-in: repeated calls to the same operation succeed without error
- The no-op driver does NOT register via `init()` in test context — instantiate directly with `noop.New()`

**From Story 3.3 (Fake Driver):**
- The fake driver is NOT conformance-eligible — it's a programmable mock, not a real implementation
- The fake driver lives in `pkg/drivers/fake/` — do not import it in the conformance suite

### Git Intelligence

Recent commits show a mature project with well-established patterns:
- Table-driven tests with `t.Run` subtests
- `errors.Is` for sentinel error assertions
- Standard Go `testing` package for unit tests (Ginkgo/Gomega used for integration/e2e, not unit tests)
- License headers on all files
- Structured logging at V(1) for normal operations

### Project Structure Notes

- Aligned with architecture: `pkg/drivers/conformance/suite.go` exactly as specified in the project directory structure
- The conformance package is in `pkg/` (not `internal/`) because external driver authors import it to validate their drivers
- The `RunConformance` function is the single public API — vendors wire their driver and call this one function
- The `noop_test.go` file is both a real test and vendor documentation by example
- No dependencies on Ginkgo/Gomega, `controller-runtime`, or any k8s client libraries — the conformance package depends only on `testing`, `context`, `errors`, and `pkg/drivers/`

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.4] — Full acceptance criteria and BDD scenarios
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 3] — Epic overview: FR20, FR21, FR23–FR25
- [Source: _bmad-output/planning-artifacts/architecture.md#Testing Patterns] — Driver conformance in `pkg/drivers/conformance/`, all drivers must pass
- [Source: _bmad-output/planning-artifacts/architecture.md#Structure Patterns] — Conformance tests at `pkg/drivers/conformance/`
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — Typed errors, context, idempotency
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/drivers/conformance/suite.go` = Conformance test suite — all drivers must pass
- [Source: _bmad-output/planning-artifacts/prd.md#FR24] — Storage vendor engineer validates driver via conformance suite
- [Source: _bmad-output/planning-artifacts/prd.md#Journey 4 (Priya)] — Vendor runs conformance suite against real two-cluster environment
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — All drivers must pass conformance suite at `pkg/drivers/conformance/suite.go`
- [Source: _bmad-output/project-context.md#Testing Rules] — Driver conformance covers full DR lifecycle
- [Source: _bmad-output/implementation-artifacts/3-1-storageprovider-interface-driver-registry.md] — Story 3.1 types, interface, errors
- [Source: _bmad-output/implementation-artifacts/3-2-no-op-driver.md] — Story 3.2 no-op driver stateful design, idempotency
- [Source: _bmad-output/implementation-artifacts/3-3-fake-driver-for-unit-testing.md] — Story 3.3 fake driver (not conformance-eligible)
- [Source: pkg/drivers/conformance/doc.go] — Existing package stub to update
- [Source: pkg/drivers/credentials_test.go] — Test pattern reference: table-driven tests, errors.Is assertions

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor)

### Debug Log References

None — clean implementation with no debugging required.

### Completion Notes List

- Implemented `RunConformance(t *testing.T, provider drivers.StorageProvider)` in `pkg/drivers/conformance/suite.go` as the single public API entry point for vendor driver conformance validation
- **Lifecycle test** (9 subtests): Exercises the full DR lifecycle sequence — Create → SetSource → GetReplicationStatus(Source) → StopReplication → SetTarget → GetReplicationStatus(Target) → StopReplication → Delete → Get(deleted). Verifies role transitions and cleanup via `ErrVolumeGroupNotFound`
- **Idempotency test** (7 subtests): Each of the 7 StorageProvider methods called twice in succession with identical arguments; second call must succeed without error. State transitions between subtests managed to allow valid role changes
- **Context cancellation test** (7 subtests): All 7 methods called with pre-cancelled context; each must return an error (not block or succeed). Setup creates VG with valid context for methods requiring an existing volume group
- **Error conditions test** (5 subtests): Operations on nonexistent volume group ID verified to return `drivers.ErrVolumeGroupNotFound` via `errors.Is` (excluding DeleteVolumeGroup per idempotent delete contract)
- Created `noop_test.go` as both a real test and vendor documentation by example — all 32 subtests pass against the no-op driver
- Updated `doc.go` with comprehensive godoc: purpose, what the suite validates, how to wire a driver, example test file, and run command
- Suite uses only standard `testing` package — no Ginkgo, Gomega, or external test framework dependencies
- All validation gates passed: `go test ./pkg/drivers/conformance/...` (32/32 PASS), `make test` (all green, 72.4% conformance coverage), `make lint` (0 issues), `make build` (clean)

### File List

- `pkg/drivers/conformance/suite.go` — **New** — Conformance test suite: RunConformance entry point, lifecycle/idempotency/context-cancellation/error-condition tests
- `pkg/drivers/conformance/noop_test.go` — **New** — No-op driver conformance validation test (also serves as vendor example)
- `pkg/drivers/conformance/doc.go` — **Modified** — Updated package documentation with comprehensive godoc
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — **Modified** — Story status updated to review
- `_bmad-output/implementation-artifacts/3-4-conformance-test-suite.md` — **Modified** — All tasks checked, Dev Agent Record populated
