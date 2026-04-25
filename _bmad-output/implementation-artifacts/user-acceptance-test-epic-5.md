# User Acceptance Test — Epic 5: Multi-Site DR Lifecycle

**Date:** 2026-04-25
**Clusters:** etl6 (primarySite), etl7 (secondarySite)
**Environment:** Stretched-cluster deployment via `hack/stretched-local-test.sh`
**DRPlan under test:** `fedora-app` (namespace: `soteria-dr-test`)

## Environment Setup

Deployed Soteria using `hack/stretched-local-test.sh`:

- **ScyllaDB:** 4 UN nodes across 2 DCs (etl6: 2, etl7: 2), NetworkTopologyStrategy
- **Soteria APIService:** `v1alpha1.soteria.io` Available=True on both clusters
- **Submariner MCS:** Active for cross-DC ScyllaDB gossip
- **cert-manager & scylla-operator:** Running on both clusters

### DRPlan: fedora-app

| Field | Value |
|-------|-------|
| Namespace | `soteria-dr-test` |
| waveLabel | `soteria.io/wave` |
| maxConcurrentFailovers | 2 |
| primarySite | etl6 |
| secondarySite | etl7 |

### VMs (5 total, 3 waves)

| Wave | VMs | etl6 (initial) | etl7 (initial) |
|------|-----|-----------------|-----------------|
| 1 | fedora-db | Running | Halted |
| 2 | fedora-appserver-1, fedora-appserver-2 | Running | Halted |
| 3 | fedora-webserver-1, fedora-webserver-2 | Running | Halted |

## Pre-test Note

The DRPlan started in `FailedOver` phase (activeSite: etl7) from previous testing. The full cycle was executed from that starting point.

## Test Execution: Full Planned Migration Cycle (4 Steps)

### Step 1 — Reprotect (FailedOver → DRedSteadyState)

| Field | Value |
|-------|-------|
| DRExecution | `cycle-reprotect-001` |
| Mode | `reprotect` |
| Result | **Succeeded** |
| Duration | ~35s |
| Plan phase after | `DRedSteadyState` |

### Step 2 — Planned Migration / Failback (DRedSteadyState → FailedBack)

| Field | Value |
|-------|-------|
| DRExecution | `cycle-failback-001` |
| Mode | `planned_migration` |
| Result | **Succeeded** |
| Duration | ~48s |
| Plan phase after | `FailedBack` |

Step 0 executed on etl7 (source site for failback). Waves executed on etl6 (target/owner). All 3 waves completed: StopReplication → SetSource → StartVM for each group.

### Step 3 — Reprotect Back (FailedBack → SteadyState)

| Field | Value |
|-------|-------|
| DRExecution | `cycle-reprotect-002` |
| Mode | `reprotect` |
| Result | **Succeeded** |
| Duration | ~35s |
| Plan phase after | `SteadyState` |

### Step 4 — Planned Migration / Failover (SteadyState → FailedOver)

| Field | Value |
|-------|-------|
| DRExecution | `cycle-failover-001` |
| Mode | `planned_migration` |
| Result | **Succeeded** (after bug fix) |
| Duration | ~13 min (stuck ~12 min before manual intervention) |
| Plan phase after | `FailedOver` |

#### Bug discovered: Step0Complete condition lost by persistStatus

Step 4 got stuck because the etl7 (Owner) controller could not see the `Step0Complete` condition set by etl6 (Step0 source).

**Root cause:** Two interrelated bugs in the multi-site planned migration path.

**Bug A — `WaveExecutor.persistStatus` overwrites cross-site conditions:**
The `persistStatus` method (`pkg/engine/executor.go`) takes a snapshot of the in-memory `exec.Status`, re-fetches the object, then replaces the entire status with the snapshot via `Status().Update()`. If another controller (e.g., the source-site Step0 reconciler) added a condition between snapshot and update, the condition is lost because the snapshot doesn't contain it.

```go
// BEFORE (bug):
statusCopy := exec.Status.DeepCopy()
e.Client.Get(ctx, ..., exec)   // fresh object may have Step0Complete
exec.Status = *statusCopy      // overwrites with stale snapshot
e.Client.Status().Update(...)   // Step0Complete gone
```

**Bug B — `reconcileResume` bypasses the Step0Complete gate:**
When the Owner site enters the resume path (StartTime set, no waves initialized), `reconcileResume` dispatches directly to the WaveExecutor without checking `Step0Complete`. This allows waves to run prematurely, and the `persistStatus` calls during wave execution overwrite the `Step0Complete` condition. Subsequent reconciles then hit the `Step0Complete` check in `reconcileWaveExecution` and wait forever.

**Manual workaround applied during test:**
Injected `Step0Complete` condition via `kubectl replace --subresource=status` to unblock the execution. The execution then completed immediately with result `Succeeded`.

**Fixes applied (same session):**

1. **`pkg/engine/executor.go` — `persistStatus` + `mergeConditions`:**
   After re-fetching the object, conditions present in the fetched object but absent from the in-memory copy are preserved via a new `mergeConditions` helper.

2. **`pkg/controller/drexecution/reconciler.go` — Step0Complete gate in resume path:**
   Added the same `Step0Complete` check that exists in `reconcileWaveExecution` to the top of `reconcileResume`, so the Owner waits for the source site before dispatching waves.

3. **Tests added:**
   - `TestPersistStatus_PreservesExternalConditions` (pkg/engine)
   - `TestDRExecutionReconciler_ResumePath_WaitsForStep0Complete` (pkg/controller/drexecution)
   - `TestDRExecutionReconciler_ResumePath_ProceedsAfterStep0Complete` (pkg/controller/drexecution)

**Verification:** `make test` — all unit tests pass, 0 lint issues, 0 regressions.

## Final State After Full Cycle

| Resource | Value |
|----------|-------|
| DRPlan phase | `FailedOver` |
| activeSite | `etl7` |
| etl6 VMs | All 5 Stopped |
| etl7 VMs | All 5 Running |

## State Machine Transitions Validated

```
FailedOver ──(reprotect)──► DRedSteadyState ──(planned_migration)──► FailedBack
     ▲                                                                    │
     │                                                                    │
     └──(planned_migration)── SteadyState ◄──(reprotect)─────────────────┘
```

All four transitions of the symmetric 8-phase lifecycle were exercised via the aggregated Soteria API on a real multi-DC ScyllaDB-backed deployment with KubeVirt VMs.

## Run 1 Summary

| Metric | Value |
|--------|-------|
| Steps executed | 4/4 |
| Steps passed (clean) | 3/4 |
| Steps passed (with fix) | 4/4 |
| Bugs found | 2 (related) |
| Bugs fixed | 2 |
| Tests added | 3 |
| Regressions | 0 |

---

## Run 2: Full Planned Migration Cycle with Metrics Validation

**Date:** 2026-04-25
**Image:** `quay.io/raffaelespazzoli/soteria:latest` (post-bugfix build)
**Starting state:** Phase=`SteadyState`, activeSite=`etl6`

### Objective

Execute a full 4-step planned migration cycle while scraping Prometheus metrics from both Soteria instances (etl6 and etl7) after each step. Validate that all registered `soteria_*` metrics are exposed, monotonically correct, and consistent across sites.

### Metrics Access Setup

Soteria exposes metrics on `https://:8443/metrics` via the controller-runtime metrics server, secured with TLS (cert-manager) and Kubernetes token authentication.

- Created `ClusterRoleBinding` for `soteria-metrics-reader` on both clusters to allow SA token access
- Scraped via `kubectl port-forward` + `curl -sk` with a short-lived SA bearer token

### Baseline Metrics (before cycle)

| Metric | etl6 | etl7 |
|--------|------|------|
| `execution_total{planned_migration,Succeeded}` | 1 | 0 |
| `execution_total{reprotect,Succeeded}` | 1 | 1 |
| `failover_duration_seconds_sum{planned_migration}` | 48s | -- |
| `failover_duration_seconds_sum{reprotect}` | 35s | 34s |
| `drplan_vms_total{fedora-app}` | 5 | 5 |
| `unprotected_vms_total` | 18 | 0 |
| `replication_lag_seconds` | (absent) | (absent) |

### Step 1 — Planned Migration (SteadyState -> FailedOver)

| Field | Value |
|-------|-------|
| DRExecution | `uat2-failover` |
| Mode | `planned_migration` |
| Result | **Succeeded** |
| Plan phase after | `FailedOver` |

**Metrics delta (etl7 was owner/target):**
- etl7: `execution_total{planned_migration,Succeeded}` 0 -> 1
- etl7: `failover_duration_seconds_sum{planned_migration}` 0 -> 43s
- etl7: `checkpoint_writes_total{uat2-failover,success}` = 6
- etl6: No change (Step 0 only, no execution completion recorded)

### Step 2 — Reprotect (FailedOver -> DRedSteadyState)

| Field | Value |
|-------|-------|
| DRExecution | `uat2-reprotect1` |
| Mode | `reprotect` |
| Result | **Succeeded** |
| Plan phase after | `DRedSteadyState` |

**Metrics delta (etl7 was owner):**
- etl7: `execution_total{reprotect,Succeeded}` 1 -> 2
- etl7: `failover_duration_seconds_sum{reprotect}` 34s -> 69s (+35s)
- etl7: `checkpoint_writes_total{uat2-reprotect1,success}` = 5, `failure` = 1
- etl7: `reprotect_duration_seconds_count` 1 -> 2

### Step 3 — Failback / Planned Migration (DRedSteadyState -> FailedBack)

| Field | Value |
|-------|-------|
| DRExecution | `uat2-failback` |
| Mode | `planned_migration` |
| Result | **Succeeded** |
| Plan phase after | `FailedBack` |

**Metrics delta (etl6 was owner/target):**
- etl6: `execution_total{planned_migration,Succeeded}` 1 -> 2
- etl6: `failover_duration_seconds_sum{planned_migration}` 48s -> 90s (+42s)
- etl6: `checkpoint_writes_total{uat2-failback,success}` = 6

### Step 4 — Reprotect Back (FailedBack -> SteadyState)

| Field | Value |
|-------|-------|
| DRExecution | `uat2-reprotect2` |
| Mode | `reprotect` |
| Result | **Succeeded** |
| Plan phase after | `SteadyState` |

**Metrics delta (etl6 was owner):**
- etl6: `execution_total{reprotect,Succeeded}` 1 -> 2
- etl6: `failover_duration_seconds_sum{reprotect}` 35s -> 69s (+34s)
- etl6: `checkpoint_writes_total{uat2-reprotect2,success}` = 6
- etl6: `reprotect_duration_seconds_count` 1 -> 2

### Final State

| Resource | Value |
|----------|-------|
| DRPlan phase | `SteadyState` |
| activeSite | `etl6` |
| Total cycle time | ~3.7 minutes |

### Post-Cycle Metrics Summary

#### `soteria_execution_total` (final values)

| Cluster | `planned_migration,Succeeded` | `reprotect,Succeeded` |
|---------|-------------------------------|----------------------|
| etl6 | 2 | 2 |
| etl7 | 1 | 2 |

Each site records completions for executions where it was the **owner** (target site). Total across both clusters: 3 planned_migrations + 4 reprotects = 7, matching the 4 steps of this cycle + 3 from Run 1. **Correct.**

#### `soteria_failover_duration_seconds` (final values)

| Cluster | Mode | Count | Sum | Avg |
|---------|------|-------|-----|-----|
| etl6 | planned_migration | 2 | 90s | 45s |
| etl6 | reprotect | 2 | 69s | 34.5s |
| etl7 | planned_migration | 1 | 43s | 43s |
| etl7 | reprotect | 2 | 69s | 34.5s |

Planned migrations average ~43s. Reprotects average ~34.5s. **Consistent and correct.**

#### `soteria_checkpoint_writes_total` (Run 2 executions only)

| Execution | Cluster | Success | Failure |
|-----------|---------|---------|---------|
| `uat2-failover` | etl7 | 6 | 0 |
| `uat2-reprotect1` | etl7 | 5 | 1 |
| `uat2-failback` | etl6 | 6 | 0 |
| `uat2-reprotect2` | etl6 | 6 | 0 |

`uat2-reprotect1` had 1 transient checkpoint write failure. Execution still succeeded.

#### `soteria_checkpoint_retries_total` (cumulative)

| Cluster | Value |
|---------|-------|
| etl6 | 77 |
| etl7 | 43 |

~3 retries/write on etl6, ~2.4 on etl7. Elevated but functional.

#### `controller_runtime_reconcile_total` (final values)

| Cluster | Controller | Total | Errors | Requeue After | Success |
|---------|-----------|-------|--------|---------------|---------|
| etl6 | drexecution | 57 | 16 | 5 | 36 |
| etl6 | drplan | 64 | 0 | 64 | 0 |
| etl7 | drexecution | 65 | 16 | 4 | 45 |
| etl7 | drplan | 8 | 0 | 8 | 0 |

The 16 reconcile errors on each cluster are cumulative from the pod lifetime (including Run 1). No new errors were introduced during Run 2.

### Anomalies Found

| # | Severity | Finding |
|---|----------|---------|
| A1 | **Medium** | `soteria_replication_lag_seconds` is **never emitted**. The metric is registered in `pkg/metrics/metrics.go` and `RecordPlanReplicationHealth()` exists, but it is never called. RPO visibility is completely absent from metrics. |
| A2 | **Low** | `uat2-reprotect1` had 1 checkpoint write failure on etl7. The execution still succeeded, indicating the retry logic worked, but transient failures suggest ScyllaDB write contention. |
| A3 | **Low** | `checkpoint_retries_total` averages ~3 retries per write on etl6. Functional but indicates frequent contention in the ScyllaDB checkpoint path. |
| A4 | **Info** | `finance-dr` and `payments-dr` on etl7 report `drplan_vms_total=0`. These are sample plans with no matching VMs — expected for test data, but creates empty gauge series. |
| A5 | **Info** | `drplan` reconciler always returns `requeue_after`, never `success`. This is by design (polling loop) but may confuse dashboard consumers. |

### Run 2 Summary

| Metric | Value |
|--------|-------|
| Steps executed | 4/4 |
| Steps passed (clean) | 4/4 |
| Bugs found in this run | 0 |
| Metrics exposed | 10/11 registered `soteria_*` metrics |
| Metrics missing | 1 (`replication_lag_seconds` — never populated) |
| Metrics correctness | All emitted metrics are monotonically correct and consistent across sites |
| Anomalies | 1 medium, 2 low, 2 informational |

---

## Run 3: Disaster Failover Workflow — Bug Discovery and Fix

**Date:** 2026-04-25
**Image:** `quay.io/raffaelespazzoli/soteria:latest` (post-bugfix build)
**Starting state:** Phase=`SteadyState`, activeSite=`etl6`

### Objective

Test the disaster failover workflow by simulating a complete etl6 site failure (ScyllaDB + Soteria scaled to 0) and triggering a `disaster` mode DRExecution from etl7.

### Bug Found: Failover handler calls SetSource instead of StopReplication

During the first disaster test run (pre-fix), the execution recorded per-group steps as `SetSource` + `StartVM`. This is architecturally incorrect.

**Root cause analysis:**

The `StorageProvider` interface defines a strict role-based state machine:

```
Target → NonReplicated   (StopReplication)
NonReplicated → Source   (SetSource)
```

`SetSource` is only valid from `NonReplicated` and returns `ErrInvalidTransition` if the volume is in the `Target` role. During failover, the surviving site's disks are in the `Target` role (read-only replicas). They must first be moved to `NonReplicated` via `StopReplication(force=true)` to become writable. Only then can VMs boot and write to them.

`SetSource` re-establishes the replication relationship (making the volume group the replication origin). This belongs exclusively in the **reprotect** phase, not in failover. The reprotect handler (`pkg/engine/reprotect.go`) already does this correctly: `StopReplication(force=true)` → `SetSource(force=false)` → health monitoring.

**The bug affected both failover paths:**

| Path | Before (buggy) | After (fixed) |
|------|----------------|---------------|
| Planned migration per-group | `StopReplication(false)` → `SetSource(false)` → `StartVM` | `StartVM` only (Step 0 already did `StopReplication`) |
| Disaster per-group | `SetSource(force=true)` → `StartVM` | `StopReplication(force=true)` → `StartVM` |

The bug was masked by the noop/fake storage driver which does not enforce the state machine transitions.

### Fix Applied

**File: `pkg/engine/failover.go`**

1. Removed `SetSource` calls from both `ExecuteGroup` and `ExecuteGroupWithSteps`
2. Removed `StepSetSource` constant (only the reprotect handler needs it, via `StepReprotectSetSource`)
3. Added `StopReplication(force=true)` to the disaster path (`!GracefulShutdown`) in both methods
4. Graceful path now only calls `StartVM` per-group (volumes are already `NonReplicated` from Step 0)
5. Updated `FailoverConfig` documentation to reflect the correct behavior

**File: `pkg/engine/failover_test.go`**

Updated all disaster tests to expect `StopReplication` instead of `SetSource`:
- `TestFailoverHandler_DisasterConfig_StopReplicationForce` (was `ForceTrue`)
- `TestFailoverHandler_DisasterConfig_NoSetSource` (was `NoStopReplication`)
- `TestFailover_Disaster_FullSuccess` — expects 2 `StopReplication` + 2 `StartVM`
- `TestFailover_Disaster_StopReplicationFails` (was `SetSourceFails`)
- `TestFailover_Disaster_StartVMFails` — first step now `StopReplication`
- `TestFailover_Disaster_StepStatusRecorded` — expects `StopReplication` step names
- `TestFailover_Disaster_ForceFlag` — checks `StopReplicationOptions.Force=true`
- `TestFailover_Disaster_NoSetSource` (was `NoStopReplication`)
- `TestFailover_Disaster_MultipleVolumeGroups` — 3 `StopReplication` + 2 `StartVM`

Updated graceful tests:
- `TestFailoverHandler_Graceful_PerGroup_NoDriverCalls` (was `SetSourceFails`) — verifies no driver calls per-group
- `TestFailoverHandler_Graceful_PerGroup_StepStatusRecorded` — expects only `StartVM` steps (2)
- `TestFailoverHandler_Graceful_FullSuccess` — asserts `SetSource` NOT called

**File: `pkg/engine/executor_test.go`**

Updated mock `GroupError` step names from `SetSource` to `StopReplication` in partial-failure tests.

### Verification

- `make test`: All tests pass, 0 failures
- `make lint-fix`: 0 issues

### Live Test: Disaster Failover (post-fix)

**Simulation setup:**
- Scaled `soteria-controller-manager` to 0 replicas on etl6
- Scaled `scyllacluster` to 0 members on etl6
- etl7 healthy: Soteria running, ScyllaDB 2 pods

**Execution: `uat3-disaster`**

| Field | Value |
|-------|-------|
| DRExecution | `uat3-disaster` |
| Mode | `disaster` |
| Result | **Succeeded** |
| Duration | ~35s |
| Plan phase after | `FailedOver` (activeSite: `etl7`) |

**Per-group steps (now correct):**

```
wave-1-group-0: StopReplication=Succeeded StartVM=Succeeded
wave-2-group-0: StopReplication=Succeeded StopReplication=Succeeded StartVM=Succeeded StartVM=Succeeded
wave-3-group-0: StopReplication=Succeeded StopReplication=Succeeded StartVM=Succeeded StartVM=Succeeded
```

All 5 VMs running on etl7. No `SetSource` steps in the execution status.

### Run 3 Summary

| Metric | Value |
|--------|-------|
| Bug found | 1 (architectural: failover called SetSource instead of StopReplication) |
| Bug severity | High (would cause `ErrInvalidTransition` with real storage drivers) |
| Files changed | 3 (`failover.go`, `failover_test.go`, `executor_test.go`) |
| Tests updated | 12 |
| `make test` | Pass |
| `make lint-fix` | 0 issues |
| Live disaster failover | Succeeded with correct `StopReplication` steps |

---

## Run 4: Clean Planned Migration Cycle (Post-Bugfix Validation)

**Date:** 2026-04-25
**Image:** `quay.io/raffaelespazzoli/soteria:latest` (fresh build with all Run 1–3 fixes)
**Starting state:** Phase=`SteadyState`, activeSite=`etl6`

### Objective

Validate a full 4-step planned migration cycle on a freshly deployed image with all previous bugfixes. Environment was cleaned: `finance-dr` and `payments-dr` DRPlans removed from both clusters and from `hack/stretched-local-test.sh`. Only `fedora-app` remains.

### Pre-test Cleanup

- Rebuilt and pushed `quay.io/raffaelespazzoli/soteria:latest`
- Rolling restart on both etl6 and etl7
- Deleted `finance-dr` and `payments-dr` DRPlans from both clusters (executions `finance-failover-001` and `payments-disaster-001` retained as immutable audit records per FR41)
- Reset `fedora-app` from `FailedOver` to `SteadyState` via 3-step cycle: reprotect → planned_migration → reprotect
- Removed sample data sections from `hack/stretched-local-test.sh`

### Step 1 — Planned Migration (SteadyState → FailedOver)

| Field | Value |
|-------|-------|
| DRExecution | `run4-failover` |
| Mode | `planned_migration` |
| Result | **Succeeded** |
| Duration | 41s |
| Plan phase after | `FailedOver` (activeSite: `etl7`) |

Per-group steps (unified path: StopReplication → StartVM):

```
wave-1-group-0: StopReplication=Succeeded StartVM=Succeeded
wave-2-group-0: StopReplication=Succeeded StopReplication=Succeeded StartVM=Succeeded StartVM=Succeeded
wave-3-group-0: StopReplication=Succeeded StopReplication=Succeeded StartVM=Succeeded StartVM=Succeeded
```

Step0Complete condition preserved correctly across sites.

### Step 2 — Reprotect (FailedOver → DRedSteadyState)

| Field | Value |
|-------|-------|
| DRExecution | `run4-reprotect1` |
| Mode | `reprotect` |
| Result | **Succeeded** |
| Duration | 35s |
| Plan phase after | `DRedSteadyState` |

### Step 3 — Failback / Planned Migration (DRedSteadyState → FailedBack)

| Field | Value |
|-------|-------|
| DRExecution | `run4-failback` |
| Mode | `planned_migration` |
| Result | **Succeeded** |
| Duration | 45s |
| Plan phase after | `FailedBack` (activeSite: `etl6`) |

Per-group steps identical to Step 1 (StopReplication → StartVM per group). Step0Complete gate worked correctly.

### Step 4 — Reprotect Back (FailedBack → SteadyState)

| Field | Value |
|-------|-------|
| DRExecution | `run4-reprotect2` |
| Mode | `reprotect` |
| Result | **Succeeded** |
| Duration | 35s |
| Plan phase after | `SteadyState` (activeSite: `etl6`) |

### Final State

| Resource | Value |
|----------|-------|
| DRPlan phase | `SteadyState` |
| activeSite | `etl6` |
| etl6 VMs | All 5 Running |
| etl7 VMs | All 5 Stopped |
| Cross-site consistency | Both sites agree on phase, activeSite, all 4 execution results |
| Total cycle time | ~2.6 minutes |

### Anomalies Found

| # | Severity | Finding |
|---|----------|---------|
| A1 | **Medium** | `persistStatus` write conflicts on every wave group across both clusters (~5–10 ERROR-level logs per execution). "The object has been modified; please apply your changes to the latest version and try again." Retries succeed but the noise is significant. Root cause: ScyllaDB eventual consistency causes stale `resourceVersion` on re-fetch inside `persistStatus`. The `mergeConditions` fix (Run 1) prevents condition loss but doesn't eliminate the underlying contention. |
| A2 | **Low** | "Full re-execution failed after resume with no waves" errors on both sites during planned migrations. The reconciler enters the resume path, hits a write conflict persisting initial wave status, logs ERROR, then retries successfully on next reconcile. Transient but noisy. |
| A3 | **Info** | Per-group steps use the unified `StopReplication → StartVM` path for both planned and disaster modes. `StopReplication` is idempotent when volumes are already NonReplicated from Step 0. This is by design per the current `failover.go` architecture. |

### Run 4 Summary

| Metric | Value |
|--------|-------|
| Steps executed | 4/4 |
| Steps passed (clean) | 4/4 |
| Bugs found | 0 |
| Functional regressions | 0 |
| Anomalies | 1 medium (write conflicts), 1 low (resume path noise), 1 informational |
| Step0Complete gate | Correct on both planned migrations |
| Cross-site consistency | Verified — both sites agree on all state |

---

## Run 5: Disaster Failover with Network Partition (Soteria Down, ScyllaDB Up)

**Date:** 2026-04-25
**Image:** `quay.io/raffaelespazzoli/soteria:latest` (same as Run 4)
**Starting state:** Phase=`SteadyState`, activeSite=`etl6`

### Objective

Test disaster failover with a more realistic partial failure simulation: Soteria controller is down on etl6 and network-isolated from etl7, but ScyllaDB cross-DC replication remains active. This differs from Run 3 (where ScyllaDB was also scaled to 0) and tests whether the surviving site can complete the failover while the shared storage layer is still functioning.

### Simulation Setup

1. **Scaled `soteria-controller-manager` to 0 replicas on etl6** — no Soteria controller running
2. **Created `NetworkPolicy` on etl7** (`block-etl6-soteria` in namespace `soteria`) targeting pods with `control-plane=controller-manager`:
   - Ingress: allow all EXCEPT from `10.128.0.0/14` (etl6 pod CIDR) and `172.30.0.0/16` (etl6 service CIDR)
   - Egress: allow all EXCEPT to `10.128.0.0/14` and `172.30.0.0/16`
3. **ScyllaDB left running** on both clusters (2 members per DC, cross-DC replication active)

### Pre-test Verification

| Check | Status |
|-------|--------|
| etl6 Soteria pods | 0 (confirmed empty) |
| etl7 Soteria pods | 1 Running |
| etl7 APIService v1alpha1.soteria.io | Available=True |
| etl7 ScyllaDB | 2 members ready |
| NetworkPolicy active | `block-etl6-soteria` targeting `control-plane=controller-manager` |
| DRPlan | `SteadyState`, activeSite=`etl6` |

### Execution: Disaster Failover

| Field | Value |
|-------|-------|
| DRExecution | `run5-disaster` |
| Mode | `disaster` |
| Submitted via | `etl7` |
| Result | **Succeeded** |
| Duration | 33s |
| Plan phase after | `FailedOver` (activeSite: `etl7`) |

**No Step0** — correct for disaster mode (source site assumed unreachable).

**Per-group steps:**

```
wave-1-group-0: StopReplication=Succeeded StartVM=Succeeded
wave-2-group-0: StopReplication=Succeeded StopReplication=Succeeded StartVM=Succeeded StartVM=Succeeded
wave-3-group-0: StopReplication=Succeeded StopReplication=Succeeded StartVM=Succeeded StartVM=Succeeded
```

All 5 VMs running on etl7. Wave ordering respected.

### Post-Failover State

| Resource | etl6 | etl7 |
|----------|------|------|
| VMs | **All 5 still Running** | All 5 Running |
| Soteria controller | Down (0 replicas) | Running |
| DRPlan phase | `FailedOver` | `FailedOver` |
| activeSite | `etl7` | `etl7` |

### Post-Restore Verification

After removing the NetworkPolicy and scaling etl6 Soteria back to 1 replica:

| Check | Result |
|-------|--------|
| Cross-site DRPlan agreement | Both sites: `FailedOver`, activeSite=`etl7` |
| Cross-site execution agreement | Both sites: `run5-disaster` Succeeded, 33s |
| etl6 VMs after controller restart + 30s | **Still Running** (not stopped) |

### Anomalies Found

| # | Severity | Finding |
|---|----------|---------|
| A1 | **High** | **Split-brain VMs after disaster failover.** etl6 VMs remain Running even after the etl6 controller restarted and saw the plan in `FailedOver` state (activeSite=`etl7`). There is no mechanism in the DRPlan or DRExecution reconciler to stop stale VMs on the non-active site after disaster failover. In a real total site failure this is invisible (site is down), but in a partial failure (controller down, site recovers) VMs run on both sites simultaneously until reprotect. |
| A2 | **Medium** | **Severe write contention at execution startup.** 34 ERROR-level log entries for a single 33s execution. Breakdown: 5× "Failed to set plan-name label", 2× "Wave initialization failed", 3× "Full re-execution failed after resume with no waves", 6× "Failed to persist group status update", 18× "Reconciler error" (wrappers). The label-setting step is particularly susceptible — 5 consecutive conflict-retry cycles before succeeding. |
| A3 | **Info** | ScyllaDB cross-DC replication remaining active during the disaster failover did not interfere with the execution. The surviving site's controller operated entirely on its local DC data. |

### Run 5 Summary

| Metric | Value |
|--------|-------|
| Disaster failover | Succeeded (33s) |
| Bugs found | 0 new functional bugs |
| Anomalies | 1 high (split-brain VMs), 1 medium (write contention), 1 informational |
| Wave ordering | Correct (1 → 2 → 3) |
| StopReplication steps | Correct (`force=true` for disaster) |
| Cross-site consistency after restore | Verified — both sites agree |
| Split-brain VMs | Yes — etl6 VMs still Running after controller restart |

---

## Run 6: Post Write-Contention Fixes — Full Planned Migration Cycle

**Date**: 2026-04-25
**Objective**: Validate that the write-contention mitigation changes eliminate the "thundering herd" errors observed in Runs 4 and 5.

### Code Changes Applied

Four targeted changes to address ScyllaDB eventual-consistency write contention:

1. **`ensurePlanNameLabel`: `Update` → `MergeFrom` patch** (`pkg/controller/drexecution/reconciler.go`)
   - Replaced `r.Update(ctx, exec)` with `r.Patch(ctx, exec, client.MergeFrom(...))` to send only the label delta, reducing the conflict surface.

2. **`reconcileSetup`: yield after setup** (`pkg/controller/drexecution/reconciler.go`)
   - Changed return from `ctrl.Result{}, false, nil` (continue to wave execution) to `ctrl.Result{RequeueAfter: 1ms}, nil` (yield).
   - Forces a fresh `resourceVersion` fetch before wave initialization, breaking the chain of rapid sequential writes.
   - Simplified the function signature by removing the `bool` return value (always yields).

3. **ScyllaDB-tuned retry backoff** (`pkg/engine/executor.go`)
   - Introduced `engine.ScyllaRetry`: `{Steps: 8, Duration: 200ms, Factor: 2.0, Jitter: 0.3}`.
   - Applied to `persistStatus` (replaces `retry.DefaultRetry` with its 10ms/5-step policy).
   - Applied to `ensurePlanNameLabel` retry loop.
   - Applied to `KubeCheckpointer.WriteCheckpoint` (`pkg/engine/checkpoint.go`).

4. **`fetchPlanWithActiveExecCheck`: consistency-safe plan re-fetch** (`pkg/controller/drexecution/reconciler.go`)
   - New helper that retries the DRPlan Get with `ScyllaRetry` backoff, waiting for `ActiveExecution` to converge after the setup yield.
   - Prevents false `StaleExecution` failures caused by ScyllaDB read-after-write lag.
   - Replaced raw `Get + ActiveExecution check` in both `reconcileResume` and `reconcileReprotectResume`.

### Test Results

All unit tests pass. Lint clean. Coverage increased from 45.4% to 46.2%.

### Execution Log

| Step | Execution Name | Mode | Result | Start Time | Completion Time | Duration |
|------|---------------|------|--------|------------|-----------------|----------|
| 1 | run6v2-failover | planned_migration | **Succeeded** | 15:05:01Z | 15:05:49Z | 48s |
| 2 | run6v2-reprotect1 | reprotect | **Succeeded** | 15:06:03Z | 15:06:45Z | 42s |
| 3 | run6v2-failback | planned_migration | **Succeeded** | 15:06:57Z | 15:07:49Z | 52s |
| 4 | run6v2-reprotect2 | reprotect | **Succeeded** | 15:08:07Z | 15:08:47Z | 40s |

**Total cycle time**: ~4 minutes (15:04:50Z → 15:08:54Z)

### Final State

```
NAME         PHASE         EFFECTIVE PHASE   ACTIVE SITE   VMS   UNPROTECTED   ACTIVE EXECUTION
fedora-app   SteadyState   SteadyState       etl6          5     0
```

All 4 steps returned `Succeeded`. Full round-trip SteadyState → FailedOver → DRedSteadyState → FailedBack → SteadyState completed cleanly.

### Error Log Comparison

| Metric | Run 4 (Before) | Run 5 (Before) | Run 6 (After) |
|--------|----------------|----------------|---------------|
| ERROR-level logs (total, both sites) | 34+ | 34+ | **2** |
| "Failed to set plan-name label" | 5+ per execution | 5+ per execution | **0** |
| "Failed to persist group status update" | 5-10 per execution | 5-10 per execution | **0** |
| "StaleExecution" failures | N/A | N/A | **0** |
| "checkpoint write failed after retries" | 0 | 0 | **0** |
| Checkpoint retries (DEBUG-level) | N/A (logged at ERROR) | N/A (logged at ERROR) | ~20 (at DEBUG, all resolved) |

The 2 remaining ERROR-level logs are benign "DRExecution is immutable after completion" races from the non-owner site attempting a late write on an already-completed execution. These are harmless — the admission webhook correctly rejects the stale write.

### Anomalies

#### Resolved: Write contention at startup — Severity: Fixed

The thundering herd of `resourceVersion` conflicts that generated 34+ ERROR logs in Runs 4 and 5 is completely eliminated. The combination of:
- Targeted `MergeFrom` patches (smaller conflict surface)
- Setup yield (fresh resourceVersion for wave initialization)
- ScyllaDB-tuned retry backoff (200ms base, exponential to ~50s total)
- Consistency-safe plan re-fetch (retries read-after-write lag)

reduced ERROR-level logs from 34+ to 2 (benign), and eliminated all `persistStatus`, `ensurePlanNameLabel`, and `StaleExecution` failures entirely.

#### Previously Identified: Immutable-after-completion race — Severity: Low

The non-owner site's reconciler sometimes queues a late write against a DRExecution that was just marked complete by the owner. The admission webhook correctly rejects this (`status: Forbidden: DRExecution is immutable after completion`). This is a harmless eventual-consistency artifact — the execution is already complete and the late write carries no new information. Occurs 1-2 times per cycle.

### Verification Checklist

| Check | Result |
|-------|--------|
| All 4 steps Succeeded | Yes |
| Final phase SteadyState | Yes |
| Active site etl6 (back to primary) | Yes |
| ERROR-level logs (plan-name label) | 0 (was 5+ per execution) |
| ERROR-level logs (persistStatus) | 0 (was 5-10 per execution) |
| ERROR-level logs (StaleExecution) | 0 |
| ERROR-level logs (total) | 2 (benign immutable race) |
| Checkpoint retries resolve within backoff | Yes (all at DEBUG level) |
| No PartiallySucceeded results | Yes |
| Unit tests pass | Yes |
| Lint clean | Yes |
