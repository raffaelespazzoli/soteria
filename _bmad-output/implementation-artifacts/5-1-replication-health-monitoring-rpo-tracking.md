# Story 5.1: Replication Health Monitoring & RPO Tracking

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want to see replication health status and estimated RPO for every protected volume group,
so that I know whether my DR plans are actually protected before a disaster strikes.

## Background

FR31 and FR32 require the DRPlan controller to actively poll storage drivers for replication health per volume group and surface that information on `DRPlan.Status`. Today the DRPlan reconciler discovers VMs, resolves volume groups, chunks waves, and composes a preflight report — but **never contacts storage drivers** to check replication health. The `GetReplicationStatus` driver method exists and is used only during re-protect health monitoring (Story 4.8). This story extends the DRPlan controller to call `GetReplicationStatus` for every resolved volume group on each reconcile, persist the results on the plan status, and emit events/conditions when health degrades.

**Key design constraints:**
- The DRPlan reconciler currently has no `drivers.Registry` dependency — only a `preflight.StorageBackendResolver` for display names. This story must wire the reconciler with the full driver resolution chain.
- `GetReplicationStatus` requires a `drivers.VolumeGroupID` (opaque driver handle), not the logical `VolumeGroupInfo.Name`. Resolving the ID requires `CreateVolumeGroup` (idempotent) — the same pattern used by `FailoverHandler.resolveVolumeGroupID`.
- The driver enum defines `Healthy`, `Degraded`, `Syncing`, `Unknown` — there is no `Error` constant. The PRD mentions `Error` as a health state (FR31). The mapping is: driver error from `GetReplicationStatus` → API `Error` status; `HealthUnknown` → API `Unknown`; all others map 1:1. The `Syncing` driver state maps to API `Syncing`.
- Polling frequency is not specified in the PRD. Use the existing `requeueInterval` (10 minutes) for steady-state re-polling. When any VG reports non-Healthy, requeue at a shorter interval (30 seconds) to detect recovery faster.
- NFR7 requires updates visible within 5 seconds — this is satisfied by the existing k8s cacher/CDC pipeline, not by polling frequency.

## Acceptance Criteria

1. **AC1 — ReplicationHealth status field:** `DRPlanStatus` has a new `ReplicationHealth []VolumeGroupHealth` field (`json:"replicationHealth,omitempty"`). Each entry contains: volume group name, namespace, health status (`Healthy`, `Degraded`, `Syncing`, `Error`, `Unknown`), last sync time (`*metav1.Time`), estimated RPO duration string, last checked timestamp (`metav1.Time`), and an optional error message. The field is populated on every successful reconcile cycle after volume groups are resolved.

2. **AC2 — Per-VG health polling:** The DRPlan controller resolves a `drivers.StorageProvider` and `drivers.VolumeGroupID` for each volume group, calls `GetReplicationStatus`, and maps the result into `VolumeGroupHealth`. Driver errors produce `Error` health with the error message. Driver `HealthUnknown` maps to `Unknown`. When no driver can be resolved for a VG (unknown provisioner), health is `Unknown` with a descriptive message.

3. **AC3 — RPO calculation:** Each `VolumeGroupHealth` entry includes `EstimatedRPO` as a human-readable duration string (e.g., `"47s"`, `"2m30s"`, `"unknown"`). When the driver provides `EstimatedRPO`, use it directly. When the driver provides only `LastSyncTime`, compute RPO as `time.Since(lastSyncTime)`. When neither is available, set RPO to `"unknown"`.

4. **AC4 — Degraded condition:** When any volume group reports non-Healthy status (Degraded, Error, or Unknown while role is Source/Target), `DRPlan.Status.Conditions` includes a `ReplicationHealthy` condition with `Status: False`, `Reason: Degraded`, and a message listing the affected volume groups. When all VGs are Healthy, the condition is `Status: True`, `Reason: AllHealthy`. When no VGs exist or discovery failed, the condition is omitted.

5. **AC5 — Kubernetes events on health transitions:** When a VG transitions health state (e.g., Healthy → Degraded, or Degraded → Healthy), the controller emits a Kubernetes event: `ReplicationDegraded` (Warning) when any VG degrades, `ReplicationHealthy` (Normal) when all VGs return to Healthy. Events include the VG name and health state in the message. Events are only emitted on actual transitions — not on every reconcile cycle.

6. **AC6 — Shorter requeue on degraded health:** When any VG reports non-Healthy status, the reconciler returns `ctrl.Result{RequeueAfter: 30 * time.Second}` instead of the default 10-minute interval. This ensures faster detection of recovery. The 30-second interval matches the existing error-requeue pattern.

7. **AC7 — Graceful degradation without driver:** Volume groups whose storage class resolves to no registered driver (or where VG ID resolution fails) are reported with `Unknown` health and a descriptive message rather than failing the entire reconcile. The `Ready` condition remains based on discovery/chunking status — replication health issues do not block the Ready condition.

8. **AC8 — Reconciler wiring:** `DRPlanReconciler` gains new fields: `Registry *drivers.Registry`, `SCLister drivers.StorageClassLister`, `PVCResolver engine.PVCResolver`. These are injected in `cmd/soteria/main.go` and integration test setup. When `Registry` is nil (backward compat), the replication health section is skipped.

9. **AC9 — Test coverage:** Unit tests covering: (a) `VolumeGroupHealth` populated correctly for Healthy, Degraded, Syncing, Unknown, and Error (driver error) cases; (b) RPO calculated from driver `EstimatedRPO`, from `LastSyncTime`, and `"unknown"` when neither available; (c) `ReplicationHealthy` condition set/cleared correctly; (d) events emitted only on health transitions; (e) shorter requeue when degraded; (f) graceful handling when driver not found; (g) integration test with noop driver showing Healthy replication for Source/Target VGs.

## Tasks / Subtasks

- [x] Task 1: Add API types (AC: #1)
  - [x] 1.1 Add `VolumeGroupHealthStatus` string type with constants: `HealthStatusHealthy`, `HealthStatusDegraded`, `HealthStatusSyncing`, `HealthStatusError`, `HealthStatusUnknown` to `types.go`
  - [x] 1.2 Add `VolumeGroupHealth` struct to `types.go`: `Name string`, `Namespace string`, `Health VolumeGroupHealthStatus`, `LastSyncTime *metav1.Time`, `EstimatedRPO string`, `LastChecked metav1.Time`, `Message string` (optional error/info message)
  - [x] 1.3 Add `ReplicationHealth []VolumeGroupHealth` field to `DRPlanStatus` with `json:"replicationHealth,omitempty"` tag
  - [x] 1.4 Run `make generate` to regenerate deepcopy + openapi

- [x] Task 2: Wire reconciler with driver infrastructure (AC: #8)
  - [x] 2.1 Add `Registry *drivers.Registry`, `SCLister drivers.StorageClassLister`, `PVCResolver engine.PVCResolver` fields to `DRPlanReconciler` struct
  - [x] 2.2 Update `cmd/soteria/main.go` to inject these dependencies (use the same instances wired for `WaveExecutor`)
  - [x] 2.3 Update integration test `suite_test.go` to inject a noop-backed registry and PVC resolver

- [x] Task 3: Implement replication health polling (AC: #2, #3)
  - [x] 3.1 Add a `pollReplicationHealth` method on `DRPlanReconciler` that accepts the plan and resolved waves, iterates volume groups, resolves driver + VG ID, calls `GetReplicationStatus`, and returns `[]VolumeGroupHealth`
  - [x] 3.2 For driver resolution per VG: use the same pattern as `WaveExecutor.resolveVGStorageClass` — iterate VM PVCs in the VG, find the storage class, call `Registry.GetDriverForPVC`, then `CreateVolumeGroup` (idempotent) for the VG ID
  - [x] 3.3 Map driver `ReplicationStatus` → `VolumeGroupHealth`: health enum mapping, RPO computation (driver `EstimatedRPO` → format as duration string; fallback to `time.Since(LastSyncTime)`; else `"unknown"`), `LastSyncTime` conversion to `*metav1.Time`
  - [x] 3.4 Handle errors gracefully: driver not found → `HealthStatusUnknown` with message; `GetReplicationStatus` error → `HealthStatusError` with error message; VG ID resolution failure → `HealthStatusUnknown` with message
  - [x] 3.5 Guard: skip the entire polling path if `r.Registry == nil` (backward compatibility)

- [x] Task 4: Integrate health polling into reconcile loop (AC: #2, #6)
  - [x] 4.1 Call `pollReplicationHealth` after volume groups are resolved and before `updateStatus` (after line 231 in current `reconciler.go`)
  - [x] 4.2 Pass `replicationHealth` into `updateStatus` and persist on `plan.Status.ReplicationHealth`
  - [x] 4.3 Add change detection for replication health in `updateStatus` (compare old vs new, similar to waves/condition/preflight)
  - [x] 4.4 Return shorter requeue interval (30s) when any VG is not Healthy; otherwise use standard 10-minute requeue

- [x] Task 5: Add ReplicationHealthy condition (AC: #4)
  - [x] 5.1 Define `conditionTypeReplicationHealthy = "ReplicationHealthy"` constant
  - [x] 5.2 After polling, compute the aggregate condition: all Healthy → `True`/`AllHealthy`; any non-Healthy → `False`/`Degraded` with message listing affected VGs
  - [x] 5.3 Use `meta.SetStatusCondition` to merge into `plan.Status.Conditions` alongside existing `Ready` condition
  - [x] 5.4 Only set condition when replication health was actually polled (skip when Registry is nil or no VGs resolved)

- [x] Task 6: Emit events on health transitions (AC: #5)
  - [x] 6.1 Compare previous `plan.Status.ReplicationHealth` with new results to detect per-VG health transitions
  - [x] 6.2 Emit `ReplicationDegraded` (Warning) event when any VG transitions from Healthy to non-Healthy, including VG name and new status in message
  - [x] 6.3 Emit `ReplicationHealthy` (Normal) event when all VGs return to Healthy after previously being degraded
  - [x] 6.4 No event emission on first reconcile (no previous state to compare against) or when health is unchanged

- [x] Task 7: Unit tests (AC: #9)
  - [x] 7.1 Test `pollReplicationHealth`: mock/fake driver returning various `ReplicationStatus` combinations → verify correct `VolumeGroupHealth` output for each health state
  - [x] 7.2 Test RPO calculation: driver provides `EstimatedRPO` → formatted string; provides `LastSyncTime` only → computed duration; provides neither → `"unknown"`
  - [x] 7.3 Test `ReplicationHealthy` condition: all Healthy → True; mixed → False with affected VGs listed; no VGs → condition not set
  - [x] 7.4 Test event emission: health transition → event emitted; no transition → no event; first reconcile → no event
  - [x] 7.5 Test requeue interval: non-Healthy VG → 30s requeue; all Healthy → 10min requeue
  - [x] 7.6 Test graceful degradation: unknown provisioner → Unknown health with message; driver error → Error health with message
  - [x] 7.7 Test backward compat: Registry nil → no replication health fields set, no crash

- [x] Task 8: Integration test (AC: #9g)
  - [x] 8.1 Add integration test with noop-driver-backed registry: create DRPlan, create VMs with PVCs and storage classes, reconcile → verify `plan.Status.ReplicationHealth` populated with noop's Healthy status
  - [x] 8.2 Verify `ReplicationHealthy` condition is `True` when noop driver reports Healthy
  - [x] 8.3 Verify `waitForCondition("ReplicationHealthy", metav1.ConditionTrue)` works in the existing test framework

- [x] Task 9: Run full test suite
  - [x] 9.1 `make generate` — regenerate deepcopy + openapi
  - [x] 9.2 `make manifests` — regenerate CRDs if markers changed
  - [x] 9.3 `make lint-fix` — auto-fix style
  - [x] 9.4 `make test` — all unit + integration tests pass

### Review Findings

- [x] [Review][Patch] Match the executor's storage-class resolution logic for replication health instead of using only the first VM/PVC [pkg/controller/drplan/health.go:136]
- [x] [Review][Patch] Clear stale `ReplicationHealth` and remove `ReplicationHealthy` when polling is skipped or yields no volume groups [pkg/controller/drplan/reconciler.go:555]
- [x] [Review][Patch] Report unregistered provisioners explicitly instead of silently falling back through `drivers.DefaultRegistry` [pkg/controller/drplan/health.go:169]
- [x] [Review][Patch] Harden AC9 coverage: the integration test currently accepts `Unknown` instead of proving the noop Source/Target happy path [test/integration/controller/drplan_health_test.go:85]
- [x] [Review][Patch] Declare list semantics for `DRPlanStatus.ReplicationHealth` instead of adding a new `list_type_missing` violation [pkg/apis/soteria.io/v1alpha1/types.go:112]

## Dev Notes

- **No `Error` in driver enum:** `pkg/drivers/types.go` defines `Healthy`, `Degraded`, `Syncing`, `Unknown`. The API-level `HealthStatusError` is a story-specific mapping for when `GetReplicationStatus` returns a Go error. Do NOT add `Error` to the driver enum — it stays as a higher-level abstraction.
- **VolumeGroupID resolution requires `CreateVolumeGroup`:** The DRPlan only stores logical `VolumeGroupInfo` (name, namespace, VM names). To call `GetReplicationStatus` you need a `VolumeGroupID`. Use the idempotent `CreateVolumeGroup` pattern from `FailoverHandler.resolveVolumeGroupID` (lines 105–147 of `pkg/engine/failover.go`). This is safe because `CreateVolumeGroup` returns the existing VG if it already exists.
- **Driver resolution pattern:** Follow `WaveExecutor.resolveVGStorageClass` (lines 765–847 of `pkg/engine/executor.go`): for each VG, iterate its VM names → `PVCResolver.ResolvePVCNames` → pick the first PVC → `SCLister` to get storage class → `Registry.GetDriverForPVC`. All VMs in a VG share the same storage class (validated by `ResolveVolumeGroups`).
- **Requeue interval strategy:** Use the existing `requeueInterval` (10 minutes) as base. When health is degraded, use the existing `30 * time.Second` (same as discovery error requeue). This avoids over-polling production storage systems while still providing timely recovery detection.
- **Condition naming:** Use `ReplicationHealthy` (not `Replicating`) to avoid collision with the `Replicating` condition set by `ReprotectHandler.updateHealthConditions` during active re-protect execution. The `ReplicationHealthy` condition is set by the DRPlan controller during steady-state monitoring; `Replicating` is set by the engine during re-protect.
- **Event spam prevention:** Compare old `plan.Status.ReplicationHealth` (from re-fetched plan in `updateStatus`) with new results. Use a map of `{vgName: health}` for O(1) lookups. Only emit events when a VG's health actually changes.
- **updateStatus refactoring:** The existing `updateStatus` takes a single `metav1.Condition` parameter. Extend it to also accept `replicationHealth []VolumeGroupHealth` and the replication condition. The method already handles change detection — add `replicationHealthChanged` comparison alongside `conditionChanged`, `wavesChanged`, etc.
- **Preflight report integration:** The existing `PreflightReport` already has per-VG storage backend info. The replication health data on `DRPlanStatus` complements this — preflight shows structure, replication health shows runtime state. Do NOT put health data inside `PreflightReport`.
- **Phases when polling should occur:** Poll replication health only when the plan has resolved volume groups (waves with groups populated). Skip polling when discovery failed or plan has no VMs. Also skip when `plan.Status.ActiveExecution != ""` (Story 5.0) — during active execution, the engine owns the driver interactions.

### Existing code patterns to follow

- **Status patches:** `client.MergeFrom(plan.DeepCopy())` pattern in `updateStatus` (line 520 of `reconciler.go`).
- **Condition management:** `meta.SetStatusCondition(&plan.Status.Conditions, condition)` (line 526).
- **Event emission:** `r.Recorder.Eventf(plan, nil, eventType, reason, "Reconcile", msg)` (line 248).
- **Change detection:** Compare old vs new before patching to avoid infinite requeue loops (lines 505–518).
- **Structured logging:** `log.FromContext(ctx).WithValues("drplan", req.NamespacedName)` + `logger.Info/Error/V(1).Info`.
- **Test patterns:** `fake.NewClientBuilder().WithStatusSubresource(&DRPlan{}).Build()`, `events.NewFakeRecorder(10)`, table-driven tests.
- **Health polling precedent:** `ReprotectHandler.countHealthy` (lines 341–356 of `reprotect.go`) — iterate VGs, call `GetReplicationStatus`, check health, log errors at V(1).

### Critical files to modify

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `VolumeGroupHealthStatus` type, `VolumeGroupHealth` struct, `ReplicationHealth` field on `DRPlanStatus` |
| `pkg/controller/drplan/reconciler.go` | Add Registry/SCLister/PVCResolver fields, `pollReplicationHealth` method, integrate into reconcile loop, extend `updateStatus`, add condition/event logic |
| `cmd/soteria/main.go` | Wire `Registry`, `SCLister`, `PVCResolver` into DRPlanReconciler |
| `pkg/controller/drplan/reconciler_test.go` | Unit tests for health polling, RPO calculation, conditions, events, requeue intervals, error handling |
| `test/integration/controller/suite_test.go` | Wire registry/resolver for DRPlan reconciler in integration test setup |
| `test/integration/controller/drplan_test.go` or `drplan_health_test.go` | Integration test with noop driver showing Healthy replication |

### Previous story intelligence (Story 5.0)

- **Story 5.0 adds `ActiveExecution` field:** When `plan.Status.ActiveExecution != ""`, an execution is in progress. The replication health polling should skip when an execution is active — the engine owns driver interactions during execution, and polling would add unnecessary load.
- **Story 5.0 may not be implemented yet:** If 5.0 is still `ready-for-dev` when 5.1 starts, check whether `ActiveExecution` exists. If not, skip the active-execution guard and add a TODO for when 5.0 lands.
- **Fixture churn from 5.0:** Story 5.0 changes `DRPlanStatus` to add `ActiveExecution`. Ensure your test fixtures are compatible with both pre-5.0 and post-5.0 status shapes.
- **`requeueInterval` (10 min) is in `reconciler.go`:** Reuse this constant for Healthy requeue. For degraded, reuse the 30-second pattern from the error branch.

### References

- [Source: _bmad-output/planning-artifacts/prd.md#FR31–FR32] — Replication health status and estimated RPO per volume group
- [Source: _bmad-output/planning-artifacts/prd.md#FR35] — Console dashboard degraded/unprotected banner (consumers of this data)
- [Source: _bmad-output/planning-artifacts/prd.md#Observability] — RPO/replication lag gauge metric (Story 5.3 will consume this data)
- [Source: pkg/drivers/interface.go:83–87] — `GetReplicationStatus` signature
- [Source: pkg/drivers/types.go:49–118] — `ReplicationHealth` enum, `ReplicationStatus` struct
- [Source: pkg/controller/drplan/reconciler.go:86–96] — Current `DRPlanReconciler` struct (no driver dependencies yet)
- [Source: pkg/controller/drplan/reconciler.go:488–539] — `updateStatus` with change detection pattern
- [Source: pkg/engine/reprotect.go:287–356] — `monitorHealth` + `countHealthy` — precedent for polling `GetReplicationStatus`
- [Source: pkg/engine/failover.go:105–147] — `resolveVolumeGroupID` via idempotent `CreateVolumeGroup`
- [Source: pkg/engine/executor.go:765–847] — `resolveVGStorageClass` — per-VG driver resolution pattern
- [Source: pkg/drivers/noop/driver.go:216–250] — NoOp driver returns Healthy with zero RPO for Source/Target
- [Source: pkg/drivers/fake/driver.go:310–324] — Fake driver with programmable reactions for testing
- [Source: pkg/metrics/metrics.go] — Existing Prometheus metrics registration pattern (Story 5.3 will add replication lag gauge)
- [Source: _bmad-output/project-context.md] — Project conventions, anti-patterns, testing rules

## Estimated Effort

- Production code: ~200 lines across ~4 files (types, reconciler, main.go wiring, integration setup)
- Test code: ~350 lines across ~3 test files (unit tests, integration test, test helpers)
- Total: ~550 net new/modified lines

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor Agent)

### Debug Log References

None — all tests passed on first implementation (after fixing lint issues and one test expectation for noop driver NonReplicated state).

### Completion Notes List

- Added `VolumeGroupHealthStatus` type with 5 constants and `VolumeGroupHealth` struct to API types
- Added `ReplicationHealth []VolumeGroupHealth` field to `DRPlanStatus`
- Wired `Registry`, `SCLister`, `PVCResolver` into `DRPlanReconciler` struct, `main.go`, and integration test setup
- Implemented `pollReplicationHealth` with per-VG driver resolution via `resolveDriverForVG` (PVC → StorageClass → Registry) and `resolveVolumeGroupID` (idempotent CreateVolumeGroup)
- Mapped driver `ReplicationStatus` → API `VolumeGroupHealth` with health enum mapping and RPO computation (EstimatedRPO → LastSyncTime → "unknown")
- Graceful degradation: driver not found → Unknown, GetReplicationStatus error → Error, VG ID resolution failure → Unknown
- Skips polling when `Registry == nil` (backward compat) or `ActiveExecution != ""` (engine owns drivers during execution)
- `ReplicationHealthy` condition: True/AllHealthy when all VGs healthy, False/Degraded with affected VG list when any non-healthy
- Health transition events: `ReplicationDegraded` (Warning) on degrade, `ReplicationHealthy` (Normal) on recovery; no events on first reconcile
- Shorter requeue (30s) when degraded, standard 10-minute when healthy
- Change detection for replication health ignores LastChecked timestamps to prevent infinite requeue loops
- Moved `pvcResolver` declaration before DRPlan reconciler setup in main.go to resolve dependency ordering
- 22 new unit tests covering all health states, RPO calculation, conditions, transitions, requeue intervals, backward compat
- 2 new integration tests with noop driver verifying ReplicationHealth population and ReplicationHealthy condition
- All 53 unit tests pass (31 existing + 22 new), 0 lint issues, all integration tests pass

### Change Log

- 2026-04-23: Story 5.1 implementation complete — replication health monitoring & RPO tracking

### File List

**New files:**
- `pkg/controller/drplan/health.go` — health polling, RPO computation, condition/event logic
- `pkg/controller/drplan/health_test.go` — 22 unit tests
- `test/integration/controller/drplan_health_test.go` — 2 integration tests

**Modified files:**
- `pkg/apis/soteria.io/v1alpha1/types.go` — VolumeGroupHealthStatus, VolumeGroupHealth, ReplicationHealth field
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — auto-generated
- `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` — auto-generated
- `pkg/controller/drplan/reconciler.go` — Registry/SCLister/PVCResolver fields, updateStatus replication health param, health polling integration
- `cmd/soteria/main.go` — wired Registry/SCLister/PVCResolver, moved pvcResolver before DRPlan reconciler
- `test/integration/controller/suite_test.go` — wired testPVCResolver and registry into DRPlan reconciler
