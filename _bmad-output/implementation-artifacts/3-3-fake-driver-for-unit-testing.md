# Story 3.3: Fake Driver for Unit Testing

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want a programmable fake driver for unit tests,
So that I can test workflow engine behavior with controlled storage responses including error injection.

## Acceptance Criteria

1. **AC1 — Programmable responses:** `pkg/drivers/fake/driver.go` implements all 7 `StorageProvider` methods. Each method can have its response pre-programmed: `fake.OnSetSource(vgID).Return(nil)` or `fake.OnSetSource(vgID).Return(drivers.ErrInvalidTransition)`. Compile-time interface check: `var _ drivers.StorageProvider = (*Driver)(nil)`.

2. **AC2 — Call recording:** Every method invocation is recorded with the method name and arguments. Callers can assert on recorded calls: `fake.Calls()` returns all calls, `fake.CallsTo("SetSource")` returns calls to a specific method, `fake.CallCount("SetSource")` returns the count.

3. **AC3 — Error injection:** When a method is called that has a programmed error response, the configured error is returned and the call is still recorded in the call history.

4. **AC4 — Sensible defaults:** When a method is called with no programmed response, it returns success with zero-value results: `CreateVolumeGroup` returns a `VolumeGroupInfo` whose `ID` is auto-generated (`"fake-<uuid>"`), `GetVolumeGroup` returns a zero-value `VolumeGroupInfo`, `GetReplicationStatus` returns a zero-value `ReplicationStatus`, all other methods return `nil` error.

5. **AC5 — Thread safety:** Call recording, response programming, and method invocation are all protected by `sync.Mutex`. Concurrent calls from multiple goroutines are safe.

6. **AC6 — k8s `<package>fake` convention:** The package lives at `pkg/drivers/fake/` following the Kubernetes `<package>fake` naming convention (e.g., `k8s.io/client-go/kubernetes/fake`).

7. **AC7 — Unit tests:** All functionality has `_test.go` coverage — response programming (success + error), call recording, default behavior, thread safety, reset, and multiple reactions consumed in order.

## Tasks / Subtasks

- [ ] Task 1: Define the Driver struct and supporting types (AC: #1, #2, #5)
  - [ ] 1.1 In `pkg/drivers/fake/driver.go`, define `Driver` struct with `sync.Mutex`, `calls []Call`, `reactions map[string][]*Reaction`
  - [ ] 1.2 Define `Call` struct: `Method string`, `Args []interface{}`
  - [ ] 1.3 Define unexported `Reaction` struct: `method string`, `vgID *drivers.VolumeGroupID` (nil = match any), `resp Response`, `consumed bool`
  - [ ] 1.4 Define `Response` struct (exported, test code may construct directly): `VolumeGroupID drivers.VolumeGroupID`, `VolumeGroupInfo *drivers.VolumeGroupInfo`, `ReplicationStatus *drivers.ReplicationStatus`, `Err error`
  - [ ] 1.5 Add `New() *Driver` constructor that initializes the map and slice
  - [ ] 1.6 Add compile-time interface check: `var _ drivers.StorageProvider = (*Driver)(nil)`

- [ ] Task 2: Implement response programming API (AC: #1, #3)
  - [ ] 2.1 Define `CallStub` struct: unexported, holds `*Driver` and `*Reaction` pointer
  - [ ] 2.2 Implement typed `On<Method>` methods on `Driver` for all 7 StorageProvider methods. Each accepts an optional `vgID drivers.VolumeGroupID` argument (variadic) for argument matching. Returns `*CallStub`. Methods: `OnCreateVolumeGroup()`, `OnDeleteVolumeGroup(vgID ...drivers.VolumeGroupID)`, `OnGetVolumeGroup(vgID ...drivers.VolumeGroupID)`, `OnSetSource(vgID ...drivers.VolumeGroupID)`, `OnSetTarget(vgID ...drivers.VolumeGroupID)`, `OnStopReplication(vgID ...drivers.VolumeGroupID)`, `OnGetReplicationStatus(vgID ...drivers.VolumeGroupID)`
  - [ ] 2.3 Implement `CallStub.Return(err error) *Driver` — sets `Reaction.resp.Err` and returns `Driver` for chaining
  - [ ] 2.4 Implement `CallStub.ReturnResult(resp Response) *Driver` — sets full `Reaction.resp` and returns `Driver` for chaining (for methods that return values + error)
  - [ ] 2.5 Reaction matching: when a method is called, find the first unconsumed reaction for that method name where either `vgID` is nil (match any) or `vgID` matches the called argument. Mark the reaction as consumed. If no unconsumed reaction matches, return sensible default.

- [ ] Task 3: Implement all 7 StorageProvider methods (AC: #1, #2, #3, #4)
  - [ ] 3.1 Each method implementation follows this pattern: lock mutex → record the call → find matching reaction → unlock → return reaction response (or default)
  - [ ] 3.2 `CreateVolumeGroup(ctx, spec)` — record call, return programmed `VolumeGroupInfo` + error, or default `VolumeGroupInfo` with ID `"fake-<uuid>"` + nil
  - [ ] 3.3 `DeleteVolumeGroup(ctx, vgID)` — record call, return programmed error or nil
  - [ ] 3.4 `GetVolumeGroup(ctx, vgID)` — record call, return programmed `VolumeGroupInfo` + error, or zero-value `VolumeGroupInfo` + nil
  - [ ] 3.5 `SetSource(ctx, vgID, opts)` — record call (include opts in args), return programmed error or nil
  - [ ] 3.6 `SetTarget(ctx, vgID, opts)` — record call (include opts in args), return programmed error or nil
  - [ ] 3.7 `StopReplication(ctx, vgID, opts)` — record call (include opts in args), return programmed error or nil
  - [ ] 3.8 `GetReplicationStatus(ctx, vgID)` — record call, return programmed `ReplicationStatus` + error, or zero-value `ReplicationStatus` + nil

- [ ] Task 4: Implement call recording and assertion helpers (AC: #2)
  - [ ] 4.1 `Calls() []Call` — returns a copy of all recorded calls (thread-safe snapshot)
  - [ ] 4.2 `CallsTo(method string) []Call` — returns calls filtered by method name
  - [ ] 4.3 `CallCount(method string) int` — returns count of calls to a specific method
  - [ ] 4.4 `Called(method string) bool` — returns true if method was called at least once
  - [ ] 4.5 `Reset()` — clears all recorded calls and all reactions (enables test reuse)

- [ ] Task 5: Update package documentation (AC: #6)
  - [ ] 5.1 Update `pkg/drivers/fake/doc.go` with a comprehensive package doc comment: purpose (programmable fake for unit tests), API overview (On*.Return pattern, call recording), k8s `<package>fake` convention, thread safety, contrast with no-op driver (programmable vs. stateful simulation)

- [ ] Task 6: Unit tests (AC: #7)
  - [ ] 6.1 In `pkg/drivers/fake/driver_test.go`:
    - [ ] 6.1.1 `TestDriver_CompileTimeInterfaceCheck` — `var _ drivers.StorageProvider = (*Driver)(nil)` (explicit in test)
    - [ ] 6.1.2 `TestDriver_DefaultBehavior_ReturnsSuccess` — call all 7 methods with no programmed responses, verify success with zero-value/default results
    - [ ] 6.1.3 `TestDriver_CreateVolumeGroup_DefaultReturnsFakeID` — verify default returns a `"fake-"` prefixed ID
    - [ ] 6.1.4 `TestDriver_OnSetSource_ReturnError` — program `OnSetSource(vgID).Return(drivers.ErrInvalidTransition)`, call SetSource, verify error returned and call recorded
    - [ ] 6.1.5 `TestDriver_OnSetSource_ReturnNil` — program success, verify nil error returned
    - [ ] 6.1.6 `TestDriver_OnGetReplicationStatus_ReturnResult` — program with `ReturnResult(Response{ReplicationStatus: &status, Err: nil})`, verify status returned
    - [ ] 6.1.7 `TestDriver_OnGetVolumeGroup_ReturnError` — program `ErrVolumeGroupNotFound`, verify error
    - [ ] 6.1.8 `TestDriver_MultipleReactions_ConsumedInOrder` — program two reactions for same method (first returns nil, second returns error), verify first call gets nil, second gets error, third gets default
    - [ ] 6.1.9 `TestDriver_ArgMatching_SpecificVgID` — program reaction for specific vgID, call with matching and non-matching vgIDs, verify only matching call gets programmed response
    - [ ] 6.1.10 `TestDriver_CallRecording` — make several calls, verify `Calls()` returns all, `CallsTo("SetSource")` returns filtered, `CallCount` is correct, `Called` returns true/false
    - [ ] 6.1.11 `TestDriver_Reset` — program reactions, make calls, reset, verify calls and reactions cleared
    - [ ] 6.1.12 `TestDriver_ConcurrentAccess` — concurrent program + call + read from multiple goroutines with `sync.WaitGroup`, verify no races (run with `-race`)
    - [ ] 6.1.13 `TestDriver_ErrorInjection_AllMethods` — program each of the 7 methods with a typed error from `pkg/drivers/errors.go`, verify the correct error is returned via `errors.Is`
    - [ ] 6.1.14 `TestDriver_CallArgs_Recorded` — call SetSource with specific vgID and opts, verify Call.Args contains the exact arguments

- [ ] Task 7: Verify build and tests (AC: #7)
  - [ ] 7.1 Run `go test -race ./pkg/drivers/fake/...` — all tests pass with race detector
  - [ ] 7.2 Run `make test` — all unit tests pass (new + existing)
  - [ ] 7.3 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [ ] 7.4 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 3 of Epic 3 (Storage Driver Framework & Reference Implementations). It builds on Story 3.1's interface, types, and errors. The fake driver is the primary testing primitive for Epic 4 (DR Workflow Engine) — the workflow engine executor, wave chunker, and state machine will all use the fake driver to test error handling, partial failures, and retry logic without any real or simulated storage.

**Key distinction from the no-op driver (Story 3.2):**

| Aspect | No-Op Driver | Fake Driver |
|---|---|---|
| Purpose | Dev/test/CI without storage infra | Unit test error injection and verification |
| State | Stateful in-memory (tracks volume groups, replication roles) | Stateless (returns programmed responses) |
| Registration | `init()` + registry under `noop.soteria.io` | Direct instantiation in tests — NOT registered in the global registry |
| Conformance | Must pass conformance suite (Story 3.4) | Does NOT pass conformance suite (programmable, not simulating real behavior) |
| Consumers | `make dev-cluster`, integration tests, CI | Unit tests for workflow engine (Epic 4), controller tests |

### Dependency on Story 3.1

This story depends entirely on types, interface, and errors from Story 3.1. The following must exist before implementation:

| From Story 3.1 | File | Used By |
|---|---|---|
| `StorageProvider` interface (7 methods) | `pkg/drivers/interface.go` | Compile-time check, all method implementations |
| `VolumeGroupID`, `VolumeGroupInfo`, `VolumeGroupSpec` | `pkg/drivers/types.go` | Method parameters, return types, Call.Args |
| `ReplicationStatus`, `VolumeRole`, `ReplicationHealth` constants | `pkg/drivers/types.go` | GetReplicationStatus return type, status fields |
| `SetSourceOptions`, `SetTargetOptions`, `StopReplicationOptions` | `pkg/drivers/types.go` | SetSource/SetTarget/StopReplication parameters, Call.Args recording |
| `ErrVolumeNotFound`, `ErrVolumeGroupNotFound`, `ErrReplicationNotReady`, `ErrInvalidTransition`, etc. | `pkg/drivers/errors.go` | Error injection in tests |

### API Design — Fluent Response Programming

The fake driver uses a fluent builder pattern inspired by testify/mock and k8s client-go fake reactors, but domain-typed for the StorageProvider interface:

```go
fake := fake.New()

// Program error response for specific volume group
fake.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)

// Program success
fake.OnSetSource("vg-2").Return(nil)

// Program result + error for value-returning methods
fake.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
    ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleSource, Health: drivers.HealthHealthy},
})

// Match any argument (omit vgID)
fake.OnDeleteVolumeGroup().Return(nil)

// Call the method
err := fake.SetSource(ctx, "vg-1", drivers.SetSourceOptions{Force: true})
// err == drivers.ErrInvalidTransition

// Assert on recorded calls
calls := fake.CallsTo("SetSource")
// calls[0].Args == []interface{}{"vg-1", drivers.SetSourceOptions{Force: true}}
```

### Reaction Matching Algorithm

When a StorageProvider method is called:
1. Lock mutex
2. Record the call (method name + args)
3. Scan the reactions list for this method name in order:
   a. Skip consumed reactions
   b. If reaction has a specific `vgID` matcher, check if the called vgID matches
   c. If reaction has no matcher (nil vgID), it matches any call
   d. First match wins — mark it consumed, use its Response
4. If no unconsumed reaction matches, use the sensible default
5. Unlock mutex
6. Return the response

Reactions are consumed in FIFO order. This enables programming sequences:

```go
fake.OnSetSource("vg-1").Return(nil)                        // first call succeeds
fake.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition) // second call fails
```

### No init() Registration

The fake driver is NOT registered in the global driver registry. It is instantiated directly in test code via `fake.New()`. This is intentional:
- The fake is a test utility, not a real driver
- Test code creates and configures the fake inline, then passes it to the code under test
- No import side effects, no global state pollution between tests

### UUID Generation for Default CreateVolumeGroup

Use `github.com/google/uuid` (already in go.mod as a transitive dependency) for generating default volume group IDs when no response is programmed. Prefix with `"fake-"` to distinguish from no-op driver's `"noop-"` prefix.

### Thread Safety Implementation

All public methods (On*, Return, StorageProvider methods, Calls, Reset) must acquire `sync.Mutex`. Use a single mutex on the Driver struct — the fake driver is not performance-sensitive, so a single lock is simpler than `sync.RWMutex`.

### Existing Code to Preserve

| File | Contents | Action |
|------|----------|--------|
| `pkg/drivers/fake/doc.go` | Package stub with license header | **Modify** — update doc comment only |
| `pkg/drivers/credentials.go` | CredentialSource, CredentialResolver interface | Preserve — do not modify |
| `pkg/drivers/credentials_secret.go` | SecretCredentialResolver, credential errors | Preserve — do not modify |
| `pkg/drivers/credentials_test.go` | Credential resolver tests | Preserve — do not modify |
| `pkg/drivers/noop/` | No-op driver (Story 3.2 target) | Preserve — do not modify |
| `pkg/drivers/conformance/doc.go` | Package stub — Story 3.4 target | Preserve — do not add code |
| Story 3.1 files (interface.go, types.go, errors.go, registry.go) | Story 3.1 deliverables | Preserve — do not modify |

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
- `pkg/drivers/conformance/doc.go` — Story 3.4 target
- `cmd/soteria/main.go` — no wiring changes
- Any controller, engine, admission, or storage code

### File Structure

| File | Purpose | New/Modified |
|------|---------|-------------|
| `pkg/drivers/fake/driver.go` | `Driver` struct, `Call`/`Response`/`CallStub` types, all 7 `StorageProvider` method implementations, `On*` programming API, call recording helpers | **New** |
| `pkg/drivers/fake/doc.go` | Updated package documentation | **Modified** |
| `pkg/drivers/fake/driver_test.go` | Unit tests: default behavior, response programming, error injection, call recording, arg matching, concurrency, reset | **New** |

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

**Error assertion** (from `pkg/drivers/credentials_test.go`):

```go
if !errors.Is(err, tt.wantErr) {
    t.Fatalf("expected error wrapping %v, got: %v", tt.wantErr, err)
}
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
go test -race ./pkg/drivers/fake/...   # Fake driver tests with race detector
make test                               # All unit tests (new + existing)
make lint-fix                           # Auto-fix code style
make lint                               # Verify lint passes
make build                              # Verify compilation
```

### Previous Story Intelligence

**From Story 3.1 (Interface & Registry):**
- Domain types use strong typing (`drivers.VolumeGroupID`, not `string`) — the fake's `On*` methods must accept these types
- Error returns use sentinel errors from `pkg/drivers/errors.go` — tests should verify error injection with `errors.Is`
- The `StorageClassLister` interface in the registry is not relevant to the fake driver
- `ResetForTesting()` in the registry shows the pattern for test-friendly reset methods

**From Story 3.2 (No-Op Driver):**
- The no-op driver is STATEFUL (tracks volume groups, replication roles) — the fake driver is STATELESS (returns programmed responses)
- The no-op driver registers via `init()` — the fake driver does NOT register
- The no-op driver's `V(1)` structured logging is not needed in the fake driver (test utility, not a real driver)
- The no-op driver uses `uuid.NewString()` for synthetic IDs — the fake driver should use the same library for default `CreateVolumeGroup` IDs

### Project Structure Notes

- Aligned with architecture: `pkg/drivers/fake/driver.go` exactly as specified in the project directory structure
- The fake driver is in `pkg/` (not `internal/`) because it will be used by tests across multiple packages (`pkg/engine/`, `internal/controller/`)
- Keep all driver logic in a single `driver.go` file — the fake driver is simple enough that splitting adds unnecessary complexity
- The `Call` and `Response` types are exported because test code constructs and inspects them directly
- The `Reaction` and `CallStub` types: `Reaction` is unexported (internal bookkeeping), `CallStub` is exported (returned by `On*` methods for fluent chaining)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.3] — Full acceptance criteria and BDD scenarios
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 3] — Epic overview: FR20, FR21, FR23–FR25
- [Source: _bmad-output/planning-artifacts/epics.md#Driver Framework (from Architecture)] — Fake driver at `pkg/drivers/fake/` for unit testing (k8s `<package>fake` convention)
- [Source: _bmad-output/planning-artifacts/architecture.md#Driver Implementation Patterns] — Typed errors, context, idempotency
- [Source: _bmad-output/planning-artifacts/architecture.md#Structure Patterns] — Driver mocks at `pkg/drivers/fake/`, k8s `<package>fake` convention
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/drivers/fake/driver.go` = Mock driver for unit tests
- [Source: _bmad-output/planning-artifacts/architecture.md#Testing Patterns] — Driver conformance, test naming convention
- [Source: _bmad-output/project-context.md#StorageProvider Driver Framework] — 7 methods, idempotency, driver packages at `pkg/drivers/<vendor>/`
- [Source: _bmad-output/project-context.md#Testing Rules] — Mock drivers: `pkg/drivers/fake/` follows k8s `<package>fake` convention
- [Source: _bmad-output/project-context.md#Critical Don't-Miss Rules] — Anti-patterns and architectural boundaries
- [Source: _bmad-output/implementation-artifacts/3-1-storageprovider-interface-driver-registry.md] — Story 3.1 types, interface, errors, registry definitions
- [Source: _bmad-output/implementation-artifacts/3-2-no-op-driver.md] — Story 3.2 no-op driver patterns, stateful design contrast
- [Source: pkg/drivers/credentials_test.go] — Test pattern reference: compile-time check, table-driven tests, errors.Is, k8s fake client usage
- [Source: pkg/drivers/fake/doc.go] — Existing package stub to update

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
