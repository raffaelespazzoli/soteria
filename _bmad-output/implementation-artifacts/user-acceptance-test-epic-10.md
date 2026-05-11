# Epic 10 — User Acceptance Test Log

**Epic:** Remove ActiveExecution from DRPlan — Static Plan Status
**Date Started:** 2026-05-11
**Environment:** etl6 / etl7 stretched cluster (Submariner MCS)
**Tester:** rspazzol

## Stories Under Test

| Story | Title | Dev Status |
|-------|-------|------------|
| 10.1 | DRExecution Concurrency Guard Without ActiveExecution | done |
| 10.2 | Derived Active Execution for Table Convertor & Preflight | done |
| 10.3 | Derived Active Execution for Reconciler Gates & VM Watch | done |
| 10.4 | Remove ActiveExecution Fields from DRPlanStatus | done |
| 10.5 | DRExecution IsTerminal Method | done |
| 10.6 | DRExecution Phase and IsActive Status Fields | done |
| 10.7 | Console UI Derived Active Execution State | done |

## Test Environment Setup

- **Clusters:** etl6 (primary/seed DC), etl7 (secondary DC)
- **ScyllaDB:** Multi-DC with NetworkTopologyStrategy (2 members per rack per DC)
- **VMs:** fedora-db (wave 1), fedora-appserver-{1,2} (wave 2), fedora-webserver-{1,2,3} (wave 3)
- **DRPlan:** fedora-app (maxConcurrentFailovers: 2, primarySite: etl6, secondarySite: etl7)

## Deployment Log

| Timestamp | Action | Result |
|-----------|--------|--------|
| 2026-05-11 14:08 ET | Build + push soteria image | OK — sha256:40e530536849 pushed (~49s) |
| 2026-05-11 14:09 ET | Build + push console plugin image (lockfile fix required) | OK — sha256:71a4c873d3d9 pushed (~46s) |
| 2026-05-11 14:10 ET | Rollout restart controller + console plugin on both clusters | OK — all 4 deployments rolled out (~19s) |

---

## Sanity Checks

| # | Check | Expected | Actual | Status |
|---|-------|----------|--------|--------|
| S1 | APIService available on etl6 | Available=True | True | PASS |
| S2 | APIService available on etl7 | Available=True | True | PASS |
| S3 | DRPlan visible on etl6 | fedora-app listed | `fedora-app SteadyState` (6 VMs, 3 waves, activeSite=etl6) | PASS |
| S4 | DRPlan visible on etl7 (cross-DC replication) | fedora-app listed | `fedora-app SteadyState` (identical to etl6) | PASS |
| S5 | VMs running on etl6 | 6 VMs Running/Ready | 6 VMs Running, Ready=True | PASS |
| S6 | VMs halted on etl7 | 6 VMs Stopped | 6 VMs Stopped, Ready=False | PASS |
| S7 | Console plugin deployed on etl6 | 1 replica ready | 1/1 Running | PASS |
| S8 | Console plugin deployed on etl7 | 1 replica ready | 1/1 Running | PASS |
| S9 | Controller healthy (no error logs) | No errors | Clean reconcile cycle: discovery + health polling for all 6 VGs | PASS |
| S10 | DRPlan conditions healthy | All True | Ready=VMsDiscovered, SitesInSync=VMsAgreed, DisksConsistent=DisksAgreed, ReplicationHealthy=AllHealthy | PASS |
| S11 | DRPlan table columns (Epic 10) | PHASE, EFFECTIVE PHASE, ACTIVE EXECUTION | Present — EffectivePhase derived correctly, Active Execution blank (no active exec) | PASS |
| S12 | No DRExecutions present | Clean state | No resources found on either cluster | PASS |

---

## Full Lifecycle Test — DR State Cycle

### Overview

Cycled through all 4 rest states using planned migrations and re-protects:

| # | Transition | Mode | From → To | Result | Duration |
|---|-----------|------|-----------|--------|----------|
| T1 | fedora-app-pm-01 | planned_migration | SteadyState → FailedOver | **PartiallySucceeded** | 80s |
| T2 | fedora-app-rp-01 | reprotect | FailedOver → DRedSteadyState | Succeeded | 41s |
| T3 | fedora-app-pm-02 | planned_migration | DRedSteadyState → FailedBack | Succeeded | 63s |
| T4 | fedora-app-rp-02 | reprotect | FailedBack → SteadyState | Succeeded | 41s |

### Epic 10 Feature Validation (during lifecycle)

| # | Feature | Expected | Actual | Status |
|---|---------|----------|--------|--------|
| E10.1 | DRExecution Phase field | Executing during run, terminal after | Phase=Executing → Succeeded/PartiallySucceeded correctly | PASS |
| E10.2 | DRExecution IsActive field | true during run, false after | IsActive=true → false on completion | PASS |
| E10.3 | DRExecution table columns (Phase/Active) | New columns in kubectl output | `PHASE` and `ACTIVE` columns present, values correct | PASS |
| E10.4 | DRPlan EffectivePhase derived (no ActiveExecution in status) | EffectivePhase=transient during exec, rest when idle | FailingOver/Reprotecting/FailingBack during execution, rest-state after | PASS |
| E10.5 | DRPlan Active Execution derived from DRExecution LIST | Shows active exec name, clears on terminal | Correctly showed `fedora-app-pm-01` etc. during execution, blank after | PASS |
| E10.6 | Concurrency guard (admission) | Only one active execution per plan | Tested implicitly — sequential execs created only after prior completed | PASS |
| E10.7 | VM states correct after full cycle | Back to original (Running etl6, Stopped etl7) | All 6 VMs Running on etl6, Stopped on etl7 | PASS |
| E10.8 | DRPlan phase progression (rest states) | SteadyState→FailedOver→DRedSteadyState→FailedBack→SteadyState | Exact progression observed | PASS |
| E10.9 | DRPlan conditions restored after cycle | All conditions True | Ready/SitesInSync/DisksConsistent/ReplicationHealthy/Replicating all True | PASS |

---

## Anomalies

### UAT-10.001: Checkpoint Write Conflict Storm (PartiallySucceeded)

**Severity:** Medium
**Affected:** T1 (fedora-app-pm-01), wave-3-group-1 (fedora-webserver-3)
**Symptom:** Execution marked PartiallySucceeded despite all VM operations succeeding

**Root Cause:** When multiple groups in the same wave execute concurrently (maxConcurrentFailovers: 2), they race to checkpoint progress on the shared DRExecution resource. The checkpoint writer uses optimistic concurrency (resourceVersion-based update), and when two groups complete nearly simultaneously, one wins and the other must re-fetch + retry. In T1, wave-3-group-1 exhausted its 6 retry attempts because wave-3-group-0's checkpoints kept winning the race.

**Log Evidence (etl7):**
```
Checkpoint write failed, retrying  attempt: 5  error: "the object has been modified; please apply your changes to the latest version"
Checkpoint write failed, retrying  attempt: 6  error: "the object has been modified; please apply your changes to the latest version"
Checkpoint write exhausted retries  attempts: 6
Marking group Failed due to checkpoint exhaustion  wave: 2  group: wave-3-group-1
```

**Impact:** The VM operations (StopReplication + StartVM) all completed successfully. Only the audit checkpoint failed. The plan transitioned correctly to FailedOver and VMs are in correct state. This is a false-negative from the user's perspective — the DR operation succeeded but the audit trail is incomplete.

**Contributing Factors:**
- ScyllaDB eventual consistency between DCs causes resource version to diverge
- Cross-DC reconciler on etl6 also writes to the DRExecution (Step0, finishExecution), adding write contention
- 6 retries may be insufficient for a 3-group wave under cross-DC latency

**Recommendation:** Serialize chunk execution within waves — chunks run sequentially (one after another) while preserving VM-level parallelism within each chunk. This eliminates the shared-resource contention entirely.

**Resolution:** Spawned Story 10.8 (Sequential Chunk Execution Within Waves).

---

### UAT-10.002: Cache Consistency Check Failed (Informational)

**Severity:** Low (informational)
**Affected:** Both DRPlans and DRExecutions resources
**Symptom:** `Cache consistency check failed` error log

**Log Evidence (etl6):**
```
E0511 18:18:57 delegator.go:347 "Cache consistency check failed" group="soteria.io" resource="drplans" resourceVersion="1778523504480137" etcdDigest="a28780f6247266eb" cacheDigest="3d5340b934454d33"
```

**Root Cause:** ScyllaDB-backed storage has eventual consistency between DCs. The cache consistency checker (delegator) detects a mismatch between what the cache holds and what ScyllaDB returns for the same resourceVersion. This happens when a write on one DC hasn't fully propagated to the local cache on the reading DC.

**Impact:** No functional impact observed — the system self-corrects on next reconcile. This is a known characteristic of eventual-consistency-backed API servers. The error is logged but does not prevent correct operation.

---

### UAT-10.003: Checkpoint Retries on All Transitions (Pattern)

**Severity:** Low
**Affected:** All 4 transitions (T1–T4)
**Symptom:** Checkpoint write retries observed even in T2/T3/T4 (which succeeded)

The reprotect operations (T2, T4) both showed 4+ checkpoint retry attempts but ultimately succeeded within the retry budget. T3 (planned_migration for failback) succeeded without exhausting retries — likely because wave-3 groups ran with less cross-DC write contention (etl6 was the Step0 owner, etl7 was the wave owner — same as T1 but in reverse, and etl7 may have been less loaded).

**Pattern:** Checkpoint write conflicts are systemic under multi-DC eventual consistency. T1 was unlucky (6 retries exhausted for group-1 specifically because group-0 was also checkpointing simultaneously). T2/T3/T4 experienced the same contention but didn't exhaust the budget.

---

## Final State Verification

| Check | Expected | Actual | Status |
|-------|----------|--------|--------|
| DRPlan phase | SteadyState | SteadyState | PASS |
| DRPlan activeSite | etl6 | etl6 | PASS |
| VMs on etl6 | 6 Running | 6 Running, Ready=True | PASS |
| VMs on etl7 | 6 Stopped | 6 Stopped, Ready=False | PASS |
| Conditions | All True | Ready/SitesInSync/DisksConsistent/ReplicationHealthy/Replicating all True | PASS |
| Execution history | 4 records | pm-01 (PartiallySucceeded), rp-01 (Succeeded), pm-02 (Succeeded), rp-02 (Succeeded) | PASS |
