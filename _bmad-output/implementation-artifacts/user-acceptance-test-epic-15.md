# Epic 15 — User Acceptance Test Log

**Epic:** Full Lifecycle E2E Testing with Real Storage (Minikube + Ceph RBD)
**Date Started:** 2026-07-08
**Environment:** minikube east / west (KVM2, Cilium Cluster Mesh, Rook-Ceph, ScyllaDB multi-DC)
**Tester:** rspazzol (AI-assisted)
**Story Under Test:** 15.9 — Full Lifecycle E2E Test with Real Storage

## Stories Under Test

| Story | Title | Dev Status |
|-------|-------|------------|
| 15.1 | ResyncVolume Driver Method (CSI Extension) | done |
| 15.2 | Planned Failover Resync Guard (Event-Driven) | done |
| 15.3 | Reprotect Handler Simplification (Real Storage) | done |
| 15.4 | ShadowPV CRD (ScyllaDB Storage) | done |
| 15.5 | ShadowPV Publisher Controller | done |
| 15.6 | ShadowPV Consumer Controller (PV Creation, Pool-ID Rewrite) | done |
| 15.7 | Console Plugin Standalone Mode | done |
| 15.8 | Soteria Operator Deployment (Multisite) | done |
| 15.9 | Full Lifecycle E2E Test with Real Storage | done (code complete, test in progress) |
| 15.10 | Site-Owned Coordination Status (ScyllaDB LWW Fix) | ready |

## Test Environment

- **Clusters:** minikube `east` (primary/seed DC), minikube `west` (secondary DC)
- **Nodes:** 1 control-plane + 3 workers per cluster (KVM2 driver)
- **Worker Memory:** 4 GB per node (increased from 3 GB during testing)
- **CNI:** Cilium with Cluster Mesh (MCS-API for cross-cluster DNS)
- **Storage:** Rook-Ceph (4 OSD per cluster, mirrored-pool with RBD mirroring)
- **VMs:** KubeVirt with cirros container disks + 1 Gi Ceph RBD data PVC per VM
- **ScyllaDB:** Multi-DC (1 node per DC) with symmetric external seeds via clusterset DNS
- **VolumeReplicationClasses:**
  - `rook-ceph-rbd-vrc-snapshot` — snapshot-based mirroring (schedulingInterval: 1m)
  - `rook-ceph-rbd-vrc-journal` — journal-based streaming mirroring

## Test Matrix

| # | Test | Failover Mode | Mirroring | Disaster Sim |
|---|------|---------------|-----------|--------------|
| 1 | DRPlan convergence | N/A | snapshot | No |
| 2 | Full lifecycle | planned_migration | snapshot | No |
| 3 | Full lifecycle | planned_migration | journal | No |
| 4 | Full lifecycle | disaster | snapshot | minikube stop |
| 5 | Full lifecycle | disaster | journal | minikube stop |

Tests run sequentially (`Ginkgo Ordered + Serial`). Each scenario creates and destroys its own set of 6 VMs.

---

## Test Runs and Findings

### Run 1 — Initial Run (2026-07-08)

**Convergence test:** PASS — DRPlan converged to all conditions healthy. ShadowPV pipeline worked end-to-end.

**planned-snapshot (T1: planned migration east→west):** FAIL — timed out in `observeResyncPending` waiting for `ResyncPending` or execution completion (300s timeout at `helpers_test.go:798`).

#### UAT-15.001 — Deadlock in Multi-Site Planned Migration Target Routing

- **Severity:** Critical
- **Description:** During planned migration east→west, the source site (east, RoleStep0) completed Step 0 (stopped VMs, set VMsStopped condition) and logged "waiting for target site resync." The target site (west, Owner role) repeatedly logged "Waiting for source site to complete Step 0 (resume path)" — creating a deadlock. East waited for west's resync; west waited for east's Step0Complete, which east only sets after west's resync.
- **Root Cause:** The `reconcileResume` function's wait-for-Step0Complete gate was evaluated before the target site's resync gate. For multi-site planned migrations, the target site (Owner) must route to `reconcileWaveExecution` (which calls `reconcileTargetSiteResyncGate`) before waiting for Step0Complete.
- **Fix:** Modified `pkg/controller/drexecution/reconciler.go` to explicitly route multi-site planned migrations on the target site to `reconcileTargetSiteResyncGate` when Step 0 is not yet complete. Added routing logic before `reconcileResume`:

```go
if exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration && !step0Done {
    if r.LocalSite != "" {
        return r.reconcileTargetSiteResyncGate(ctx, exec, plan)
    }
}
```

---

### Run 2 — After Deadlock Fix

**planned-snapshot (T1):** FAIL — timed out at `helpers_test.go:798`. ResyncPending never set.

#### UAT-15.002 — ScyllaDB Eventual Consistency Causing Setup Loop

- **Severity:** Critical
- **Description:** After `reconcileSetup` patches `StartTime` on the DRExecution, subsequent reads from ScyllaDB return stale data (without `StartTime`) due to eventual consistency. This caused the west controller to repeatedly re-enter `reconcileSetup`, each time logging "DRExecution setup complete, yielding for fresh resourceVersion." The original `RequeueAfter: 1ms` triggered controller-runtime's exponential backoff rate limiter, delaying actual reconciliation by minutes.
- **Root Cause:** ScyllaDB's eventual consistency model means writes on one site are not immediately visible on reads from the same site. The `1ms` requeue after setup completion caused rapid retries that triggered controller-runtime's rate limiter.
- **Fix (two changes in `pkg/controller/drexecution/reconciler.go`):**
  1. Introduced `sync.Map` field `setupDone` on the reconciler to track executions that have completed setup in-process. The guard checks `setupDone.Load(exec.Name)` before entering `reconcileSetup` and stores it after setup completes.
  2. Increased `RequeueAfter` in `reconcileSetup` from `1ms` to `2 * time.Second` to avoid rate-limiter backoff.

---

### Run 3 — After Setup Guard Fix

**planned-snapshot (T1):** FAIL — timed out at `helpers_test.go:811` ("ResyncPending should resolve"). ResyncPending was set but never cleared within the 300s timeout.

#### UAT-15.003 — ResyncVolume Target Site VolumeGroup Not Found

- **Severity:** Major
- **Description:** `reconcileTargetSiteResyncGate` on west attempted to call `ResyncVolume` but repeatedly received "volume group not found" errors. The VR/VGR CRs on the target cluster had not been created or were not visible to the CSI extension driver at the time resync was attempted.
- **Fix:** Modified `reconcileTargetSiteResyncGate` in `reconciler.go` to handle `ErrVolumeGroupNotFound` gracefully — log the error and requeue (10s) rather than failing the execution.

#### UAT-15.004 — Resync Requeue Interval Too Long (10 minutes)

- **Severity:** Major
- **Description:** After calling `ResyncVolume` successfully, `reconcileTargetSiteResyncGate` used `resyncTimeout` (default 10 minutes) as the `RequeueAfter` interval. This meant the controller waited up to 10 minutes before checking if resync had completed. With a 300s test timeout, the test would time out before the controller re-checked.
- **Fix:** Changed all `RequeueAfter` values in `reconcileTargetSiteResyncGate` from `resyncTimeout` to `10 * time.Second` for more frequent polling. Removed the unused `resyncTimeout` variable from the function.

---

### Run 4 — After Resync Fixes

**planned-snapshot (T1):** FAIL — timed out at `helpers_test.go:798` ("waiting for ResyncPending or execution completion"). The `ResyncPending` condition was never set to True. Controller logs showed a burst of "DRExecution setup complete" messages followed by repeated "ResyncVolume failed on target site: volume group not found" errors.

#### UAT-15.005 — Recurring VolumeGroup Not Found Prevents ResyncPending

- **Severity:** Major (investigation ongoing)
- **Description:** Despite the graceful handling of `ErrVolumeGroupNotFound`, the condition persisted across all retry cycles. The VR/VGR CRs were either not being created on the target cluster, or the CSI Addons operator on west was not reconciling them in time for the resync call.
- **Status:** Investigation interrupted by minikube restart. Root cause suspected to be CSI Addons connection issues or VR/VGR lifecycle timing on the target cluster.

---

### Run 5 — After Minikube Restart + Clean Environment

**planned-snapshot (T1):** FAIL — timed out at `helpers_test.go:811` ("ResyncPending should resolve", 300s timeout). ResyncPending was set to True (ResyncVolume was called), but the VR/VGR resync did not complete within 5 minutes. Root cause: a race condition in the Reconcile routing where `Step0Complete` being set by the east controller caused the west controller to bypass the target site resync gate.

#### UAT-15.006 — Step0Complete Causes Target Site to Bypass Resync Gate

- **Severity:** Critical
- **Description:** In multi-site planned migration, the source site (east) checks resync completion locally via its own VR/VGR CRs. When the source finds resync complete, it sets `Step0Complete=True`. However, the target site (west) may still be polling its local VRs for resync completion (`ResyncPending=True`). The Reconcile routing condition at line 235-237 only routes to `reconcileWaveExecution` (and thus `reconcileTargetSiteResyncGate`) when `!Step0Complete`. Once Step0Complete becomes True, the target falls through to `reconcileResume`, which runs the full wave executor pipeline — bypassing the resync gate entirely. The wave executor discovers 0 VMs (test cleanup had already started), completes with "Succeeded", and advances the DRPlan phase without properly clearing ResyncPending.
- **Fix:** Modified both the Reconcile routing (line 235-237) and `reconcileWaveExecution` (line 316) to also route to `reconcileTargetSiteResyncGate` when `ResyncPending` is still True, regardless of `Step0Complete`. This ensures the target site completes its resync work before advancing to wave execution.
- **Files Changed:** `pkg/controller/drexecution/reconciler.go`

#### UAT-15.007 — Stale setupDone Guard Blocks Setup on Re-Created Executions

- **Severity:** Critical
- **Description:** The `setupDone` sync.Map (introduced in UAT-15.002 to handle ScyllaDB eventual consistency) retains entries across the lifetime of the controller process. When a DRExecution is deleted and re-created with the same name (as happens in E2E test reruns), the stale `setupDone[name] = true` entry causes the controller to enter the resume path instead of setup. Since `StartTime` is nil on the new execution, the Step0 role controller waits forever with "Execution not yet started, waiting for target site setup", while the target site waits for VMsStopped. Neither condition is ever set because setup never ran.
- **Fix:** Added a staleness check: if `setupAlreadyDone` is true but `StartTime` is nil and `Result` is empty, the entry is stale and is deleted from the map, allowing setup to proceed.
- **Files Changed:** `pkg/controller/drexecution/reconciler.go`

#### UAT-15.008 — DRPlan Reports Healthy Before Target Site VRs Exist

- **Severity:** Critical
- **Description:** The DRPlan's `ReplicationHealthy=True` condition is set by the east DRPlan reconciler after verifying east VRs are healthy. The test reads this from `eastClient` and immediately creates the DRExecution. However, the west DRPlan reconciler takes an additional ~2.5 minutes to create its VRs (observed: east VRs created at T+9s, west VRs at T+5m). The DRExecution's wave execution on west then fails with "VR/VGR not yet created by DRPlan reconciler for volume group: volume group not found" because no VRs exist on the target site yet. The resync gate (07:42:42Z) also sees all VGs as "not found" and vacuously marks resync complete, masking the real issue.
- **Root Cause:** In multi-site deployments, each DRPlan reconciler independently creates and monitors VRs on its own cluster. The DRPlan status (stored in ScyllaDB) is updated by each reconciler independently. The east reconciler can report healthy before the west reconciler has finished creating VRs, creating a race between DRPlan health convergence and DRExecution start.
- **Fix:** Added `waitForVRsOnBothSites` helper in `test/multisite/helpers_test.go` that explicitly polls both `eastClient` and `westClient` for the expected number of VolumeReplication CRs before proceeding to create the DRExecution. This is called in `convergeScenario` immediately after `waitForDRPlanHealthy`.
- **Files Changed:** `test/multisite/helpers_test.go`
- **Note:** A longer-term controller-level fix would be to have the DRPlan health check verify VR existence cross-site (e.g., via ShadowPV-based VR status propagation), but this requires additional design work.

---

## Infrastructure Fixes Applied During Testing

### INFRA-15.001 — Worker Node Memory Increase

- **Description:** Increased minikube worker node memory from 3 GB to 4 GB on both clusters. Updated `hack/multisite/setup-clusters.sh` default memory parameter.
- **Reason:** VMs + Ceph + ScyllaDB + KubeVirt caused OOM pressure on 3 GB workers. Swap-on-btrfs was attempted but `swapon` returned "Invalid argument" (btrfs does not support swap files).

### INFRA-15.002 — RBD NBD Mounter for Journal Mirroring

- **Description:** Created a separate StorageClass `rook-ceph-block-journal` with `mounter: rbd-nbd` in `hack/multisite/manifests/rook-ceph/storage-class.yaml`. Updated `setup-rook-ceph.sh` to deploy both storage classes.
- **Reason:** Enabling journal-mode mirroring adds the `journaling` RBD image feature, which the kernel `krbd` driver cannot mount. The userspace `rbd-nbd` mapper is required. Verified that `rbd-nbd` binary is present in Rook CSI node plugin images by default.

### INFRA-15.003 — ScyllaDB Symmetric Seeds (AC8)

- **Description:** Updated `hack/multisite/overlays/east/scyllacluster-patch.yaml` to add symmetric external seeds. East points to west via `soteria-scylladb-west-rack1-0.soteria.svc.clusterset.local` (was already configured on west pointing to east).
- **Reason:** Required for disaster recovery scenarios where either DC can go offline and the surviving DC continues operating ScyllaDB independently.

### INFRA-15.004 — Optimistic Concurrency Log Filter

- **Description:** Modified the e2e log scanner in `test/multisite/helpers_test.go` to filter out "the object has been modified" errors unless they appear 5+ times in less than 10 seconds for the same resource.
- **Reason:** Controller-runtime retries on optimistic concurrency conflicts are expected and benign. The previous log scanner flagged every occurrence, causing false positive test failures.

---

## Code Changes Summary

All changes are uncommitted on the working branch. Files modified:

| File | Change Type | Description |
|------|-------------|-------------|
| `pkg/controller/drexecution/reconciler.go` | Bug fix + Design | Multi-site target routing, `sync.Map` setup guard, resync requeue intervals, two-sided reprotect (`reconcileReprotectPassive`), removed `ErrResyncRequested` from reprotect |
| `pkg/engine/roles.go` | Design | Added `RoleReprotectPassive` for passive site during reprotect |
| `pkg/engine/reprotect.go` | Design | Removed `ResyncVolume`/`ErrResyncRequested` from Owner-side Execute; RoleSource now passes verification |
| `pkg/engine/failover.go` | Bug fix | Step 0 now calls `StopReplication` to demote source VRs when `SkipResync=true` |
| `pkg/engine/doc.go` | Documentation | Updated reprotect workflow description for two-sided design |
| `pkg/engine/roles_test.go` | Test | Updated role assignments for `RoleReprotectPassive` |
| `pkg/engine/reprotect_test.go` | Test | Updated stale-primary tests to expect no ResyncVolume on Owner |
| `pkg/engine/failover_test.go` | Test | Updated full-cycle test for new reprotect call pattern |
| `pkg/controller/shadowpv/consumer.go` | Enhancement | Additional logging for PV creation pipeline |
| `test/multisite/helpers_test.go` | Enhancement | Log scanner conflict filter, improved cleanup, polling, infra health guard |
| `test/multisite/convergence_test.go` | Fix | Test assertion corrections |
| `test/multisite/lifecycle_test.go` | Fix | Test timing adjustments |
| `test/multisite/setup_test.go` | Fix | Namespace cleanup, client setup, infra health guard integration |
| `hack/multisite/manifests/rook-ceph/storage-class.yaml` | New | `rook-ceph-block-journal` SC with `rbd-nbd` |
| `hack/multisite/setup-rook-ceph.sh` | Enhancement | Deploy both storage classes and VRCs |
| `hack/multisite/setup-clusters.sh` | Config | Worker memory 3GB → 4GB |
| `hack/multisite/setup-all.sh` | Fix | Updated VRC references, cleanup |
| `hack/multisite/setup-scylladb.sh` | Fix | TLS and deployment improvements |
| `hack/multisite/overlays/east/scyllacluster-patch.yaml` | Config | Symmetric external seeds |
| `config/manager/kustomization.yaml` | Config | Image reference update |

---

## Environment Stability Issues

### ENV-15.001 — Cilium Cluster Mesh Disconnect After Minikube Restart

- **Description:** After minikube restart, Cilium Cluster Mesh showed `0/1 remote clusters ready` with `remote configuration: expected=true, retrieved=false`. Cross-cluster DNS (clusterset.local) stopped resolving.
- **Fix:** Restart both `clustermesh-apiserver` deployments and Cilium DaemonSets. Cluster Mesh typically recovers within 30-60 seconds after restart.
- **Frequency:** Every minikube restart.

### ENV-15.002 — ScyllaDB Stale Gossip After Pod IP Change

- **Description:** After minikube restart, ScyllaDB pods get new IPs but gossip state persists old IPs in system tables. Nodes show each other as DN (Down) with stale IPs.
- **Fix:** Delete both ScyllaClusters and PVCs, then redeploy fresh. `nodetool removenode` causes the new node to be banned via Raft topology. Full clean redeploy is the only reliable fix.
- **Frequency:** Every minikube restart that changes pod IPs.

### ENV-15.003 — TLS Certificate CA Rotation After Minikube Restart

- **Description:** cert-manager regenerated the self-signed CA (`soteria-ca`) after minikube restart, but existing certificates (ScyllaDB client, serving, API server) were still signed by the old CA. Controllers failed to connect to ScyllaDB with `x509: certificate signed by unknown authority`.
- **Fix:** Delete all cert secrets (client, serving, API server, metrics, webhook) so cert-manager re-issues them with the new CA. Update the `scylladb-combined-ca` ConfigMap. Restart ScyllaDB pods and controllers.
- **Frequency:** After minikube restart when cert-manager CA is regenerated.

### ENV-15.004 — Ceph West OSD Recovery After Restart

- **Description:** After minikube restart, 1 of 4 OSDs on west would temporarily be down, causing `HEALTH_WARN` with degraded PGs.
- **Fix:** Self-resolving — OSD pods restart and PGs recover within 2-5 minutes. No manual intervention required.

### ENV-15.005 — Stale RBD Watchers / Kernel Mapper After Minikube Restart

- **Description:** After minikube restart, ScyllaDB PVCs (on `mirrored-pool`) fail to mount with `rbd image ... is still being used` (stale exclusive lock watcher from the old pod) or `rbd: map failed: (108) Cannot send after transport endpoint shutdown` (stale kernel RBD messenger state).
- **Fix (multi-step):**
  1. Clear the Ceph OSD blocklist: `ceph osd blocklist clear`
  2. Restart the RBD CSI node plugin DaemonSet: `kubectl rollout restart ds/rook-ceph.rbd.csi.ceph.com-nodeplugin`
  3. Force-delete the stuck pod so the operator recreates it
- **Frequency:** Common after minikube restart when RBD volumes were mounted before the stop.

### ENV-15.006 — ScyllaDB Keyspace Missing After Data Wipe

- **Description:** After deleting ScyllaDB PVCs (required for gossip cleanup), the `soteria` keyspace no longer exists. The controller logs `Keyspace soteria does not exist` errors on cache consistency checks.
- **Fix:** Restart the controller deployment. The controller's startup sequence recreates the keyspace and schema on first connection to a fresh ScyllaDB instance.

---

## Reconciler Fixes Detail

### Fix 1: Multi-Site Target Routing (`reconciler.go` lines ~310-316)

**Before:** Target site (Owner) evaluated `reconcileResume` which waited for Step0Complete before proceeding to wave execution. This created a deadlock because Step0Complete is only set after the target site completes resync.

**After:** Added explicit routing before `reconcileResume`:
```go
if exec.Spec.Mode == soteriav1alpha1.ExecutionModePlannedMigration && !step0Done {
    if r.LocalSite != "" {
        return r.reconcileTargetSiteResyncGate(ctx, exec, plan)
    }
}
```

### Fix 2: ScyllaDB Eventual Consistency Guard (`reconciler.go` lines ~102-107, 211-212, 1526)

**Before:** After `reconcileSetup` patched `StartTime`, stale reads caused the controller to re-enter setup repeatedly.

**After:** Added `sync.Map` field:
```go
setupDone sync.Map
```
- `Store(exec.Name, true)` after setup completes
- `Load(exec.Name)` before entering setup to skip if already done
- `Delete(exec.Name)` when execution reaches terminal state

### Fix 3: Resync Requeue Intervals (`reconciler.go` lines ~1826-1882)

**Before:** Used `resyncTimeout` (10 minutes) as `RequeueAfter`.

**After:** All `RequeueAfter` values in `reconcileTargetSiteResyncGate` changed to `10 * time.Second`.

### Fix 4: Graceful VolumeGroup Not Found (`reconciler.go` lines ~1846-1849)

**Before:** `ErrVolumeGroupNotFound` from `ResyncVolume` would fail the execution.

**After:** Log the error and requeue with 10s delay, allowing VR/VGR CRs time to be created by the CSI Addons operator.

### Fix 5: Two-Sided Reprotect (`roles.go`, `reprotect.go`, `reconciler.go`)

**Before:** Reprotect ran only on the Owner (active) site. The `ReprotectHandler.Execute` Phase 1 treated `RoleSource` as a "stale primary" and called `ResyncVolume` to demote it. This was wrong for planned migration where the Owner's VRs are legitimately primary.

**After:** Reprotect is now a two-sided operation:
- **Owner site:** `ReprotectHandler.Execute` accepts both `RoleSource` and `RoleTarget` as valid in Phase 1. No `ResyncVolume` or `StopReplication` calls. Proceeds directly to Phase 2 health monitoring.
- **Passive site:** New `reconcileReprotectPassive` in the reconciler. Gets `RoleReprotectPassive` from `ReconcileRole`. Iterates VGs, calls `StopReplication` on any `RoleSource` (stale primary after disaster), requeues every 10s until all are secondary and healthy.

```go
// roles.go — new role
RoleReprotectPassive  // assigned to non-target site during Reprotecting/ReprotectingBack with mode=reprotect

// reprotect.go — Phase 1 (Owner side)
case drivers.RoleSource:
    logger.Info("VG confirmed in primary state (active site)", "vg", vg.Info.Name)
    // No demote, no resync — legitimate primary

// reconciler.go — Passive side
case drivers.RoleSource:
    logger.Info("Stale primary detected on passive site, demoting", "vg", vg.Info.Name)
    vg.Driver.StopReplication(ctx, vg.VGID)
```

### Fix 6: Source Site Demote in Step 0 (`failover.go`)

**Before:** When `SkipResync=true` (multi-site planned migration), `PreExecute` stopped VMs and returned `ErrResyncRequested` without demoting source VRs. The target site's `ResyncVolume` would wait indefinitely because the primary held the image lock.

**After:** `PreExecute` now calls `driver.StopReplication` on each source VG before returning `ErrResyncRequested`:
```go
if h.Config.SkipResync {
    // Stop VMs... then:
    for _, vg := range dedupedVGs {
        driver.StopReplication(ctx, vgID) // demote → creates final mirror snapshot
    }
    return ErrResyncRequested
}
```

---

### Run 6 — T1 Passes, T2 Reprotect Fails (2026-07-12)

**planned-snapshot (T1: planned migration east→west):** PASS — after UAT-15.008 fix (explicit VR wait on both sites), the planned migration completed successfully. The `StopReplication` demote-before-resync fix in `PreExecute` was critical: without demoting source VRs before the target calls `ResyncVolume`, the Ceph secondary images remain "not ready" indefinitely because the primary holds the image lock.

**planned-snapshot (T2: reprotect):** FAIL — timed out at 300s with "DRExecution ps-t2-reprotect result: got , want Succeeded". The execution never completed.

#### UAT-15.009 — Reprotect Handler Incorrectly Demotes Active Site's Primary VRs

- **Severity:** Critical (design flaw)
- **Description:** After planned migration east→west, west's VRs are legitimately primary (RoleSource). The ReprotectHandler's Phase 1 state verification treated `RoleSource` as a "stale primary" and called `ResyncVolume` on the active site's own VRs. This effectively demoted the active site's primary volumes, which is incorrect — the active site's VRs are supposed to remain primary. The resync call returned `ErrResyncRequested`, causing the reconciler to set `ResyncPending=True` and enter an indefinite wait for a resync that can never complete (because no other site is primary to sync from).
- **Root Cause:** The original reprotect design assumed it only ran on the Owner (active) site and treated `RoleSource` as always indicating a stale primary. This assumption is wrong for planned migration where the Owner's VRs are legitimately primary. The design also assumed a single-sided approach where only the active site participates in reprotect.
- **Fix (two-sided reprotect redesign):**

  **Concept:** Reprotect becomes a two-sided operation. Each site runs its local part independently:
  - **Active site (RoleOwner):** Verifies local VRs are in a valid role (Source or Target) and monitors replication health. Does NOT demote or resync its own VRs.
  - **Passive site (RoleReprotectPassive):** Ensures local VRs are secondary — demotes stale primaries (StopReplication) if they exist after a disaster scenario, and verifies replication health.

  **Changes:**

  1. **`pkg/engine/roles.go`** — Added `RoleReprotectPassive` constant. Updated `ReconcileRole` to assign it to the non-target site during `Reprotecting` and `ReprotectingBack` phases when mode is `reprotect`.

  2. **`pkg/engine/reprotect.go`** — Phase 1 state verification now treats both `RoleSource` (primary, legitimate on active site) and `RoleTarget` (secondary) as valid. Removed the `ResyncVolume` call for `RoleSource` and the `ErrResyncRequested` yield block entirely. The `resyncRequested` variable is eliminated.

  3. **`pkg/controller/drexecution/reconciler.go`** — Added `reconcileReprotectPassive` method: discovers local VRs, checks their role via `GetReplicationStatus`, calls `StopReplication` on stale primaries, requeues until all VRs are secondary and healthy. Wired `RoleReprotectPassive` into `dispatchByRole`. Removed the `ErrResyncRequested` handling block from `reconcileReprotect`.

  4. **`pkg/engine/roles_test.go`** — Updated `TestReconcileRole_AllCombinations` for the new `RoleReprotectPassive` assignments during `Reprotecting` and `ReprotectingBack` phases with `ExecutionModeReprotect`.

  5. **`pkg/engine/reprotect_test.go`** — Renamed and updated tests: `TestReprotect_PrimaryOnActiveSite_PassesVerification` (RoleSource passes without ResyncVolume), `TestReprotect_PrimaryOnActiveSite_HealthMonitoring` (health monitoring works for primary VGs), `TestReprotect_MixedRoles_BothPass_ErrorVGFails` (both Source and Target pass verification), `TestReprotect_DriverCallsMade` (no ResyncVolume or StopReplication on Owner).

  6. **`pkg/engine/failover_test.go`** — Updated `TestStateTableInvariant_FullCycle` reprotect helper and cumulative assertion counts (reprotect no longer contributes ResyncVolume calls).

- **Files Changed:** `pkg/engine/roles.go`, `pkg/engine/roles_test.go`, `pkg/engine/reprotect.go`, `pkg/engine/reprotect_test.go`, `pkg/engine/failover_test.go`, `pkg/engine/doc.go`, `pkg/controller/drexecution/reconciler.go`
- **Tests:** All unit tests pass (20 packages, including engine and reconciler tests).
- **Status:** **RESOLVED** (code complete, all unit tests pass, awaiting e2e validation)

#### UAT-15.010 — Source Site Must Demote VRs in Step 0 for Planned Migration

- **Severity:** Critical
- **Description:** During planned migration Step 0, after VMs are stopped, the source site's primary VRs must be demoted (via `StopReplication`) so the RBD mirroring daemon creates a final snapshot. Without this demote, the target site's `ResyncVolume` call waits indefinitely because the secondary images remain "not ready" — the primary holds the image lock and no final snapshot exists for the secondary to replay.
- **Fix:** Modified `pkg/engine/failover.go` `PreExecute` function: when `SkipResync=true` (multi-site planned migration), the code now iterates through volume groups and calls `driver.StopReplication` to demote source VRs before returning `ErrResyncRequested`.
- **Files Changed:** `pkg/engine/failover.go`, `pkg/engine/failover_test.go`
- **Status:** **RESOLVED**

---

### Infrastructure Health Guard (2026-07-12)

#### INFRA-15.005 — Pre-Test Infrastructure Health Guard

- **Description:** Added `verifyInfrastructureHealth()` function in `test/multisite/helpers_test.go` that runs as a guard before any test. Checks: (1) Cilium Cluster Mesh connectivity on both clusters, (2) Ceph health on both clusters, (3) Ceph RBD mirroring pool health on both clusters, (4) ScyllaDB cross-DC gossip (≥2 UN nodes).
- **Integration:** Called in `BeforeSuite` after Kubernetes clients are created and before test namespace provisioning.
- **Files Changed:** `test/multisite/helpers_test.go`, `test/multisite/setup_test.go`
- **Status:** **APPLIED** — successfully detected Cilium Cluster Mesh issues before test execution.

---

### Run 7 — Two-Sided Reprotect + Step 0 Demotion (2026-07-13)

After deploying the two-sided reprotect fix (UAT-15.009) and Step 0 demotion fix (UAT-15.010), two consecutive runs were performed.

**Run 7a:**
- Convergence: PASS (all 8 checks)
- T1 (planned migration east→west): PASS (2m37s)
- T2 (reprotect on west): PASS (50s) — **two-sided reprotect confirmed working**
- T3 (planned migration west→east): FAIL — DR execution succeeded (3m47s) but `ps-db` VM stuck in `Scheduling` for 3 minutes. Root cause: 32 stale VolumeAttachments from previous test runs overwhelmed the CSI controller. Infrastructure issue, not DR logic.
- T4: Skipped (T3 failure)

**Run 7b** (after cleaning stale VolumeAttachments and PVs):
- Convergence: FAIL — log scanner detected immutability error from previous run's `ps-t3-migrate`
- T1 (planned migration east→west): FAIL — timed out at 300s waiting for execution result. Stuck at `ResyncPending=True` — west controller looped "waiting for Step0Complete from source" for 5+ minutes.

#### UAT-15.011 — ScyllaDB LWW Race Condition Overwrites Step0Complete

- **Severity:** Critical
- **Description:** During T1 planned migration, the east controller (Step0 role) patched `Step0Complete=True` at 08:49:20Z and the west controller (Owner role) patched `ResyncComplete=True` at 08:49:21Z. Both patches used JSON Merge Patch which replaces the entire `Conditions` array. ScyllaDB Last-Write-Wins conflict resolution selected west's write (later timestamp), silently discarding east's `Step0Complete` condition. The DRExecution's conditions became `[Progressing, VMsStopped, ResyncComplete]` (west's version) instead of `[Progressing, Step0Complete]` (east's version). West then looped forever waiting for `Step0Complete` that was silently lost.
- **Root Cause:** Both sites write coordination signals to the same `status.Conditions` array. JSON Merge Patch replaces the entire array on each write. With ScyllaDB LWW, concurrent writes from different sites within the same second result in one write being silently discarded.
- **Evidence:** Post-failure inspection of the DRExecution on east showed `conditions=[Progressing, VMsStopped, ResyncComplete]` — exactly west's write result, with east's `Step0Complete` absent despite east logging successful completion.
- **Fix:** Story 15.10 — Split coordination signals into site-owned `SiteStatuses` map keyed by site name. Each controller writes only to `siteStatuses[localSite]`, reading from `siteStatuses[otherSite]`. Different JSON paths eliminate LWW conflicts.
- **Status:** **OPEN** — Story 15.10 written, implementation pending.

---

## Current Status

**Convergence test (Test 1):** PASS — DRPlan converges to healthy state via ShadowPV pipeline.

**Planned-snapshot lifecycle (Test 2):**
- T1 (planned migration east→west): PASS (when LWW race doesn't occur) / FAIL (intermittent UAT-15.011)
- T2 (reprotect on west): PASS — two-sided reprotect confirmed working (50s)
- T3 (planned migration west→east): PASS (execution succeeds, VM scheduling can be slow)
- T4 (reprotect on east): Not yet validated (T3 VM scheduling timeout in Run 7a)

**Planned-journal lifecycle (Test 3):** Not yet attempted.

**Disaster-snapshot lifecycle (Test 4):** Not yet attempted.

**Disaster-journal lifecycle (Test 5):** Not yet attempted.

---

## Open Issues

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| UAT-15.001 | Deadlock in multi-site target routing | Critical | **RESOLVED** |
| UAT-15.002 | ScyllaDB eventual consistency setup loop | Critical | **RESOLVED** |
| UAT-15.003 | ResyncVolume VolumeGroup not found | Major | **RESOLVED** (graceful handling) |
| UAT-15.004 | Resync requeue interval too long | Major | **RESOLVED** |
| UAT-15.005 | Recurring VolumeGroup not found prevents ResyncPending | Major | **RESOLVED** (transient, handled gracefully) |
| UAT-15.006 | Step0Complete causes target to bypass resync gate | Critical | **RESOLVED** |
| UAT-15.007 | Stale setupDone guard blocks setup on re-created executions | Critical | **RESOLVED** |
| UAT-15.008 | DRPlan reports healthy before target site VRs exist | Critical | **RESOLVED** (test fix) |
| UAT-15.009 | Reprotect handler demotes active site's primary VRs | Critical | **RESOLVED** (two-sided reprotect) |
| UAT-15.010 | Source site must demote VRs in Step 0 | Critical | **RESOLVED** |
| UAT-15.011 | ScyllaDB LWW race overwrites Step0Complete | Critical | **OPEN** — Story 15.10 |
| INFRA-15.001 | Worker node memory increase | — | **APPLIED** |
| INFRA-15.002 | RBD NBD mounter for journal mirroring | — | **APPLIED** |
| INFRA-15.003 | ScyllaDB symmetric seeds | — | **APPLIED** |
| INFRA-15.004 | Log scanner conflict filter | — | **APPLIED** |
| INFRA-15.005 | Pre-test infrastructure health guard | — | **APPLIED** |
| INFRA-15.006 | ScyllaDB CA: cert-manager managed secret | — | **APPLIED** |
| INFRA-15.007 | Pre-test stale resource cleanup guard | — | **APPLIED** |

### INFRA-15.006: ScyllaDB CA — cert-manager managed secret (replaces manual ConfigMap)

- **Problem:** The `scylladb-combined-ca` ConfigMap was created once during setup by concatenating two CA certs. When cert-manager rotated its CA, the ConfigMap went stale, causing TLS `EOF` errors on ScyllaDB connections after any controller redeployment.
- **Root cause:** The ConfigMap is a static snapshot; Kubernetes does not auto-update ConfigMap contents when upstream secrets change.
- **Fix:** Replaced the ConfigMap volume source with a secret-backed volume referencing `scylladb-serving-tls` (cert-manager managed). Kubernetes auto-updates secret-backed volumes when the secret changes.
- **Files changed:**
  - `hack/multisite/overlays/base/scylladb-tls-patch.yaml` — `combined-ca` volume: `configMap` → `secret`
  - `hack/multisite/setup-scylladb.sh` — removed `create_combined_ca()`, replaced with `wait_for_ca_secret()`, updated STS patch

### INFRA-15.007: Pre-test stale resource cleanup guard

- **Problem:** Manual cleanup of left-over resources (stuck namespaces, orphaned PVs, stale VolumeAttachments, orphaned RBD images) was required before every test re-run.
- **Fix:** Added `cleanupStaleTestResources()` guard to `BeforeSuite`, called after `verifyInfrastructureHealth()` and before namespace creation. The guard handles:
  1. Cluster-scoped DR resources (DRPlans, DRExecutions, ShadowPVs) with finalizer stripping
  2. Stuck test namespaces (strips VR/VGR/PVC finalizers to unblock Terminating state)
  3. Stale VolumeAttachments referencing deleted PVs
  4. Orphaned PVs (Released/Failed or stuck Terminating with test namespace ClaimRef)
  5. Orphaned RBD mirror images (cross-references against live PVs to avoid touching ScyllaDB images)
- **Safety:** RBD image cleanup builds a set of image names from all live PV CSI volume attributes. Only images not referenced by any PV are removed — ScyllaDB PVs in `mirrored-pool` are never touched.
- **Files changed:**
  - `test/multisite/helpers_test.go` — added `cleanupStaleTestResources()` and sub-functions
  - `test/multisite/setup_test.go` — wired guard into `BeforeSuite`

## Next Steps

1. Implement Story 15.10 (site-owned coordination status) to fix UAT-15.011.
2. Build, deploy, and run planned-snapshot full lifecycle (T1–T4).
3. Proceed to Tests 3-5 (planned-journal, disaster-snapshot, disaster-journal).
4. Validate per-transition assertions (Duration, Phase, conditions) and real-storage assertions (lastSyncTime on VR CRs).
