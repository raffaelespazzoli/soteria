# Story 12.6: Conformance Suite & Integration Testing

Status: done

## Story

As a quality engineer,
I want the `csi-extension` driver to pass the conformance suite and have integration tests covering the full DR lifecycle,
So that the driver is verified against the same contract as the noop driver and lifecycle transitions are proven correct.

## Background

### Conformance Suite

Story 3.4 established a `pkg/drivers/conformance/` test suite (`RunConformance`) that exercises the full StorageProvider contract: lifecycle transitions, idempotency, context cancellation, and error paths. The noop driver passes this suite. The csi-extension driver must also pass it.

However, the csi-extension driver has external dependencies (Kubernetes API server for VR/VGR CRDs). The conformance suite needs either:
1. A real or fake API server (envtest) for VR/VGR CRDs
2. A mock Kubernetes client injected into the driver

### Integration Tests

Beyond conformance, integration tests should exercise the full Soteria DR lifecycle with the csi-extension driver:
1. Create DRPlan with `volumeReplicationDriver: csi-extension`
2. Verify VR/VGR CRs created on reconcile
3. Execute planned failover → verify VR/VGR state transitions
4. Execute reprotect → verify VR/VGR state transitions
5. Verify health monitoring reads VR/VGR status correctly

## Acceptance Criteria

1. **AC1 — Conformance suite passes:** `RunConformance` from `pkg/drivers/conformance/` passes against the `csi-extension` driver with a fake/envtest Kubernetes client.

2. **AC2 — Lifecycle test:** An integration test creates a DRPlan, runs discovery/consistency/chunking, creates VGs via the driver, then exercises: StopReplication → SetSource → GetReplicationStatus across the full 8-phase lifecycle.

3. **AC3 — VR/VGR verification:** Tests verify that the correct VR or VGR CRs exist after each operation, with the expected `spec.replicationState` values.

4. **AC4 — Health mapping test:** Tests verify that GetReplicationStatus correctly maps VR/VGR status to Soteria ReplicationStatus for all health/role combinations.

5. **AC5 — Error handling:** Tests verify driver behavior when VR/VGR CRs cannot be created (API errors), when CRs are deleted externally (not found), and when status is empty.

6. **AC6 — Concurrent access:** Tests verify the driver is safe for concurrent use (the reconciler calls driver methods from multiple goroutines).

## Tasks / Subtasks

- [x] Task 1: Conformance suite setup (AC: #1)
  - [x] 1.1 Set up envtest or fake client for VR/VGR CRDs
  - [x] 1.2 Create csi-extension driver instance with test client
  - [x] 1.3 Run `RunConformance` and fix any failures
  - [x] 1.4 Document any conformance deviations (e.g., CSI model has no NonReplicated)

- [x] Task 2: Lifecycle integration test (AC: #2, #3)
  - [x] 2.1 Create DRPlan with `csi-extension` driver
  - [x] 2.2 Exercise: Create → StopReplication → SetSource → GetReplicationStatus → Delete
  - [x] 2.3 Verify VR/VGR CRs at each step

- [x] Task 3: Health mapping tests (AC: #4)
  - [x] 3.1 Create VR/VGR CRs with various status values
  - [x] 3.2 Verify GetReplicationStatus returns correct Soteria mappings

- [x] Task 4: Error handling tests (AC: #5)
  - [x] 4.1 Test API error on CreateVolumeGroup
  - [x] 4.2 Test VR/VGR deleted externally → StopReplication returns ErrVolumeGroupNotFound
  - [x] 4.3 Test empty status → Health=Unknown

- [x] Task 5: Concurrent access test (AC: #6)
  - [x] 5.1 Run multiple goroutines calling driver methods concurrently with `-race`

- [x] Task 6: Verify (AC: all)
  - [x] 6.1 Run `make test` — all tests pass
  - [x] 6.2 Run `make lint-fix && make lint` — zero lint issues

## Dev Notes

### Key Locations

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/conformance_test.go` | New — conformance suite runner |
| `pkg/drivers/csiextension/integration_test.go` | New — lifecycle integration tests |
| `pkg/drivers/csiextension/driver_test.go` | Modified — add error/concurrent tests |

### Conformance Deviations

The CSI model doesn't have a `NonReplicated` state. The conformance suite tests role transitions through NonReplicated. The csi-extension driver may need to report `NonReplicated` as `secondary` (or the conformance suite may need adapter logic). Document any deviations in the test file.

### What NOT to Change

- `pkg/drivers/conformance/` — the suite itself should not change for a new driver
- Noop driver tests
- Integration test infrastructure (envtest setup)

### Dependency

- **Depends on Stories 12.3, 12.4, 12.5** — all StorageProvider methods must be implemented.

### Previous Story Intelligence

- **Story 3.4 (Conformance Test Suite):** Established RunConformance. 32 subtests against noop.

### Build Commands

```bash
make test
make lint-fix && make lint
```

## Dev Agent Record

### Implementation Plan

- Created a `conformanceAdapter` that wraps the csi-extension driver to bridge three deviations between the CSI replication model and the conformance suite's abstract StorageProvider contract: (1) inject synthetic PVCNames, (2) simulate controller status reconciliation, (3) clear status after StopReplication to map to NonReplicated.
- Lifecycle integration tests exercise the full Create → StopReplication → SetSource → GetReplicationStatus → Delete cycle for both single-VM (VR) and multi-VM (VGR) paths, verifying VR/VGR spec.replicationState at each step.
- Health mapping tests cover all 10 state×condition combinations for both VR and VGR paths, including condition precedence (Degraded > Resyncing > Completed).
- Error handling tests use controller-runtime interceptor.Funcs to inject API errors on Create, and simulate external CR deletion to verify ErrVolumeGroupNotFound.
- Concurrent access tests run 10-20 goroutines with `-race` flag exercising Create+Get, Stop+SetSource, and GetReplicationStatus in parallel.

### Debug Log

No issues encountered.

### Completion Notes

- All 27 conformance subtests pass (Lifecycle: 7, Idempotency: 6, ContextCancellation: 6, ErrorConditions: 4)
- All integration tests pass with `-race` flag (no data races detected)
- `make test` passes — csiextension coverage increased from 89.4% to 90.1%
- `make lint-fix && make lint` — zero lint issues
- No changes to `pkg/drivers/conformance/`, noop driver, or other packages

## File List

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/conformance_test.go` | New — conformance adapter + RunConformance runner |
| `pkg/drivers/csiextension/integration_test.go` | New — lifecycle, health mapping, error handling, concurrent access tests |
| `_bmad-output/implementation-artifacts/12-6-conformance-suite-integration-testing.md` | Modified — story status and completion |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | Modified — story status updated |

### Review Findings

- [x] [Review][Patch] MultiVM lifecycle missing post-delete `GetVolumeGroup` assertion — parity with single-VM test [integration_test.go:182-190]
- [x] [Review][Patch] MultiVM lifecycle missing VGR count guards after StopReplication/SetSource — potential index out of range [integration_test.go:148-163]
- [x] [Review][Patch] SingleVM lifecycle missing VR count assertion after StopReplication/SetSource — assertAllVRState vacuous pass if list is empty [integration_test.go:63-77]
- [x] [Review][Patch] TestSetSource_ExternallyDeleted missing VGR/multi-VM path — AC5 symmetry with StopReplication tests [integration_test.go:479-507]

## Change Log

- **2026-05-15:** Implemented conformance suite adapter and integration tests for csi-extension driver (Story 12.6)
