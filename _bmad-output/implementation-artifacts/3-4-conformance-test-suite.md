# Story 3.4: Conformance Test Suite

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a storage vendor engineer,
I want a conformance test suite that validates the full DR lifecycle against any driver,
So that I can prove my driver implementation is correct before submitting it.

## Acceptance Criteria

1. **AC1 — Full DR lifecycle validation:** `pkg/drivers/conformance/suite.go` exercises the complete DR lifecycle in sequence against any `StorageProvider` implementation: CreateVolumeGroup → EnableReplication → GetReplicationInfo (verify active) → PromoteVolume (planned) → DemoteVolume → ResyncVolume → GetReplicationInfo (verify re-established) → PromoteVolume (disaster, force) → ResyncVolume → DisableReplication → DeleteVolumeGroup (FR24).

2. **AC2 — Clear failure messages:** When any method returns an unexpected error, the test fails with a clear message identifying which lifecycle step failed, the method name, the volume group ID, and the error returned.

3. **AC3 — No-op driver passes:** Running the conformance suite against the no-op driver (Story 3.2) results in all tests passing — confirming the no-op driver is a valid reference implementation.

4. **AC4 — Idempotency verification:** Each of the 9 methods is called twice in succession during the test and the second call succeeds without error, verifying all methods are idempotent as architecturally required.

5. **AC5 — Context cancellation:** Each method respects `context.Context` cancellation and returns promptly (or with a context error) when the context is cancelled before invocation.

6. **AC6 — Vendor-friendly execution:** The suite is designed so that a vendor engineer can wire their driver and run it with a clear, documented invocation pattern. The suite exposes a `RunConformance(t *testing.T, provider drivers.StorageProvider)` function that any `_test.go` file can call with any driver instance.

7. **AC7 — Package documentation:** `pkg/drivers/conformance/doc.go` has comprehensive godoc explaining the suite's purpose, what it validates, how vendors wire their driver, and example usage.

## Tasks / Subtasks

- [ ] Task 1: Define the conformance suite runner function (AC: #1, #2, #6)
  - [ ] 1.1 In `pkg/drivers/conformance/suite.go`, define `RunConformance(t *testing.T, provider drivers.StorageProvider)` as the entry point
  - [ ] 1.2 The function accepts any `StorageProvider` implementation — no dependency on the registry or `init()` registration
  - [ ] 1.3 Each lifecycle step is a named `t.Run` subtest for clear failure reporting
  - [ ] 1.4 The suite shares state (volume group ID) across subtests via closure variables — subtests run sequentially, not in parallel

- [ ] Task 2: Implement full DR lifecycle test (AC: #1, #2)
  - [ ] 2.1 **Step 1 — CreateVolumeGroup:** Call `CreateVolumeGroup(ctx, spec)` with a test `VolumeGroupSpec`. Verify a non-empty `VolumeGroupID` is returned. Store the ID for subsequent steps
  - [ ] 2.2 **Step 2 — EnableReplication:** Call `EnableReplication(ctx, vgID)`. Verify nil error
  - [ ] 2.3 **Step 3 — GetReplicationInfo (active):** Call `GetReplicationInfo(ctx, vgID)`. Verify nil error and `State == ReplicationActive`
  - [ ] 2.4 **Step 4 — PromoteVolume (planned):** Call `PromoteVolume(ctx, vgID, PromoteOptions{Force: false})`. Verify nil error
  - [ ] 2.5 **Step 5 — DemoteVolume:** Call `DemoteVolume(ctx, vgID, DemoteOptions{Force: false})`. Verify nil error
  - [ ] 2.6 **Step 6 — ResyncVolume:** Call `ResyncVolume(ctx, vgID)`. Verify nil error
  - [ ] 2.7 **Step 7 — GetReplicationInfo (re-established):** Call `GetReplicationInfo(ctx, vgID)`. Verify nil error (state depends on driver implementation — no-op returns `ReplicationActive` immediately, real drivers may return `ReplicationResyncing`)
  - [ ] 2.8 **Step 8 — PromoteVolume (disaster, force):** Call `PromoteVolume(ctx, vgID, PromoteOptions{Force: true})`. Verify nil error
  - [ ] 2.9 **Step 9 — ResyncVolume (after disaster):** Call `ResyncVolume(ctx, vgID)`. Verify nil error
  - [ ] 2.10 **Step 10 — DisableReplication:** Call `DisableReplication(ctx, vgID)`. Verify nil error
  - [ ] 2.11 **Step 11 — DeleteVolumeGroup:** Call `DeleteVolumeGroup(ctx, vgID)`. Verify nil error
  - [ ] 2.12 **Step 12 — GetVolumeGroup (deleted):** Call `GetVolumeGroup(ctx, vgID)`. Verify `drivers.ErrVolumeGroupNotFound` is returned (confirms cleanup)

- [ ] Task 3: Implement idempotency test (AC: #4)
  - [ ] 3.1 In `suite.go`, define a subtest `Idempotency` within `RunConformance` (runs after the lifecycle test)
  - [ ] 3.2 Create a fresh volume group for idempotency testing
  - [ ] 3.3 For each of the 9 methods, call it twice in succession with the same arguments:
    - `CreateVolumeGroup` — two calls, both succeed (may return different IDs)
    - `GetVolumeGroup` — two calls with the same ID, both return same result
    - `EnableReplication` — two calls, second is a no-op
    - `GetReplicationInfo` — two calls, both succeed
    - `PromoteVolume` — two calls (Force: false), second is a no-op
    - `DemoteVolume` — two calls, second is a no-op
    - `ResyncVolume` — two calls, second is a no-op
    - `DisableReplication` — two calls, second is a no-op
    - `DeleteVolumeGroup` — two calls, second returns nil (idempotent delete)
  - [ ] 3.4 Each double-call is a named `t.Run` subtest: `"Idempotency/CreateVolumeGroup"`, etc.

- [ ] Task 4: Implement context cancellation test (AC: #5)
  - [ ] 4.1 In `suite.go`, define a subtest `ContextCancellation` within `RunConformance`
  - [ ] 4.2 For each of the 9 methods, create a pre-cancelled context (`context.WithCancel` + immediate `cancel()`) and call the method
  - [ ] 4.3 Verify the method returns an error (either `context.Canceled` or a wrapped context error) — the method must not hang or succeed when the context is already cancelled
  - [ ] 4.4 Each method test is a named `t.Run` subtest: `"ContextCancellation/CreateVolumeGroup"`, etc.
  - [ ] 4.5 Use a separate volume group (created with a valid context before the cancellation subtests) for methods that require an existing volume group

- [ ] Task 5: Implement error condition tests (AC: #2)
  - [ ] 5.1 In `suite.go`, define a subtest `ErrorConditions` within `RunConformance`
  - [ ] 5.2 Test `GetVolumeGroup` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [ ] 5.3 Test `EnableReplication` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [ ] 5.4 Test `DisableReplication` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [ ] 5.5 Test `PromoteVolume` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [ ] 5.6 Test `DemoteVolume` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [ ] 5.7 Test `ResyncVolume` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`
  - [ ] 5.8 Test `GetReplicationInfo` with a nonexistent ID — verify `drivers.ErrVolumeGroupNotFound`

- [ ] Task 6: Create no-op driver conformance test (AC: #3)
  - [ ] 6.1 In `pkg/drivers/conformance/noop_test.go`, create `TestConformance_NoopDriver`
  - [ ] 6.2 Import `pkg/drivers/noop` (side-effect import not needed — instantiate directly with `noop.New()`)
  - [ ] 6.3 Call `RunConformance(t, noop.New())`
  - [ ] 6.4 This test file serves as the example for vendor engineers wiring their own driver

- [ ] Task 7: Update package documentation (AC: #7)
  - [ ] 7.1 Update `pkg/drivers/conformance/doc.go` with comprehensive godoc: purpose, what the suite validates (lifecycle, idempotency, context cancellation, error conditions), how to wire a driver, example test file, run command

- [ ] Task 8: Verify build and tests (AC: #3)
  - [ ] 8.1 Run `go test ./pkg/drivers/conformance/...` — all conformance tests pass against no-op driver
  - [ ] 8.2 Run `make test` — all unit tests pass (new + existing)
  - [ ] 8.3 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 8.4 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4 of Epic 3 (Storage Driver Framework & Reference Implementations). It is the capstone story that validates the entire driver framework built in Stories 3.1–3.3. The conformance suite lives at `pkg/drivers/conformance/suite.go` as specified in the architecture document and is the primary validation tool for FR24: "Storage vendor engineer can implement a new StorageProvider driver by implementing the 9-method Go interface and running the conformance test suite."

**Epic 3 story chain:**

| Story | Deliverable | Relationship |
|---|---|---|
| 3.1 | Interface, types, errors, registry | Foundation — defines the 9-method contract |
| 3.2 | No-op driver | Reference implementation — passes conformance |
| 3.3 | Fake driver | Unit test utility — does NOT pass conformance (programmable, not simulating real behavior) |
| **3.4** | **Conformance suite** | **Validates drivers implement the contract correctly** |

### Dependencies on Stories 3.1–3.2

The conformance suite depends on the StorageProvider interface and no-op driver from previous stories:

| From Story | File | Used By |
|---|---|---|
| 3.1 | `pkg/drivers/interface.go` — `StorageProvider` interface | `RunConformance` accepts any `StorageProvider` |
| 3.1 | `pkg/drivers/types.go` — `VolumeGroupID`, `VolumeGroupSpec`, `VolumeGroupInfo`, `ReplicationInfo`, `ReplicationState`, `PromoteOptions`, `DemoteOptions` | Test assertions on return values and state |
| 3.1 | `pkg/drivers/errors.go` — `ErrVolumeGroupNotFound` | Error condition tests |
| 3.2 | `pkg/drivers/noop/driver.go` — `New() *Driver` | `noop_test.go` instantiates no-op driver for conformance validation |

### Suite Design — Why `RunConformance(t, provider)` Not Ginkgo

The conformance suite uses standard `testing.T` (not Ginkgo/Gomega) because:
- **Vendor accessibility:** External driver engineers should not be forced to learn Ginkgo. Standard `testing.T` is universally understood.
- **Minimal dependencies:** The conformance package should have no dependencies beyond `testing`, `context`, `errors`, and `pkg/drivers/`. No Ginkgo/Gomega import in the conformance package itself.
- **Composability:** Vendor test files call `RunConformance(t, myDriver)` — works with any Go test runner, CI system, or IDE.
- **Subtests for structure:** Use `t.Run("Lifecycle/CreateVolumeGroup", ...)` for structured output without Ginkgo `Describe`/`It`.

The no-op conformance test file (`noop_test.go`) also uses standard `testing.T` for consistency with the suite.

### Lifecycle Test State Management

The lifecycle test creates ONE volume group and threads its ID through all 11 steps. Steps are sequential subtests sharing state via closure variables. This is intentional:
- The DR lifecycle is inherently sequential (you can't promote before enabling replication).
- Each step depends on the state left by the previous step.
- If any step fails, subsequent steps are skipped via `t.FailNow()` (no point testing promote if create failed).

The idempotency test creates a SEPARATE volume group to avoid interference with the lifecycle test.

### Context Cancellation Test Strategy

The context cancellation test verifies that driver methods respect `context.Context` semantics:
- Create a pre-cancelled context via `ctx, cancel := context.WithCancel(context.Background()); cancel()`
- Call each method with this cancelled context
- The method MUST return an error — it should not succeed or block indefinitely
- We verify `err != nil` but do NOT mandate a specific error type (drivers may return `context.Canceled`, a wrapped context error, or their own timeout error)

For methods that need an existing volume group (all except `CreateVolumeGroup`), create the volume group with a valid context first, then test cancellation separately.

### Error Condition Test Strategy

Tests that a driver correctly returns `drivers.ErrVolumeGroupNotFound` for operations on nonexistent volume group IDs. Uses `errors.Is()` for assertion, following the established pattern from `pkg/drivers/credentials_test.go`. The nonexistent ID uses a synthetic value: `drivers.VolumeGroupID("conformance-nonexistent-vgid")`.

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
t.Run("Idempotency/EnableReplication", func(t *testing.T) {
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

The fake driver (Story 3.3) is intentionally excluded from conformance testing. It is a programmable test utility that returns pre-configured responses — it does not simulate real storage behavior. The conformance suite validates that a driver correctly implements the full DR lifecycle with state tracking, which the fake driver does not do (it's stateless by design).

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
- Error returns use sentinel errors from `pkg/drivers/errors.go` — test with `errors.Is`
- `ReplicationState` constants: `ReplicationActive`, `ReplicationDegraded`, `ReplicationStopped`, `ReplicationPromoted`, `ReplicationDemoted`, `ReplicationResyncing`
- `PromoteOptions` and `DemoteOptions` both have a `Force bool` field
- `VolumeGroupSpec` struct for `CreateVolumeGroup` input

**From Story 3.2 (No-Op Driver):**
- The no-op driver is STATEFUL (tracks volume groups, replication states in-memory)
- `New() *Driver` constructor creates a fresh instance — use this in `noop_test.go`
- The no-op driver returns `drivers.ErrVolumeGroupNotFound` for unknown volume group IDs
- After `ResyncVolume`, the no-op driver sets state to `ReplicationActive` immediately (no async)
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

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
