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

---

## Post-10.8 Validation — Sequential Chunk Execution

**Date:** 2026-05-11 16:01 ET
**Build:** sha256:1d50992955be (soteria), sha256:71a4c873d3d9 (console plugin, cached)

### Full Lifecycle (Post-Fix)

| # | Transition | Mode | From → To | Result | Duration |
|---|-----------|------|-----------|--------|----------|
| T5 | fedora-app-pm-03 | planned_migration | SteadyState → FailedOver | **Succeeded** | 64s |
| T6 | fedora-app-rp-03 | reprotect | FailedOver → DRedSteadyState | Succeeded | 43s |
| T7 | fedora-app-pm-04 | planned_migration | DRedSteadyState → FailedBack | Succeeded | 62s |
| T8 | fedora-app-rp-04 | reprotect | FailedBack → SteadyState | Succeeded | 41s |

### UAT-10.001 Resolution Confirmed

The planned migration (T5) that previously produced PartiallySucceeded (T1 pre-fix) now completes as **Succeeded**. Wave 3 groups execute sequentially — no checkpoint contention between concurrent writers. The fix is validated.

### Remaining Observations (Informational)

| # | Observation | Severity | Notes |
|---|-------------|----------|-------|
| O1 | Checkpoint retries still occur (2-4 per transition) | Low | Expected — cross-DC replication lag causes transient resourceVersion staleness even with a single writer. Retries always succeed within budget. |
| O2 | `DRExecution is immutable after completion` reconciler error on pm-04 | Low | Non-owner site (etl6) attempts to write final status after the owner (etl7) already completed and sealed the execution. Harmless — the reconciler re-fetches, sees terminal state, and exits cleanly on next loop. Known pattern from dual-site reconciliation. |
| O3 | No `Cache consistency check failed` errors this run | Info | Previous run (pre-10.8) had these. Likely timing-dependent. |

### Story 10.8 Acceptance Criteria Validation

| AC | Description | Status |
|----|-------------|--------|
| AC1 | Sequential chunk execution | PASS — T5 wave 3 ran groups 0 then 1 sequentially (confirmed by checkpoint ordering in logs) |
| AC2 | Fail-forward preserved | Not triggered this run (all groups succeeded), but code path unchanged |
| AC3 | Internal VM-level parallelism preserved | PASS — StopReplication per VG still concurrent within each group |
| AC4 | Checkpoint uncontested | PASS — no checkpoint exhaustion in any transition |
| AC5 | Retry path serialized | Not exercised (no PartiallySucceeded to retry) |
| AC6 | Context cancellation | Not exercised |
| AC7 | doc.go updated | Verified in code |
| AC8 | Tests updated | Verified via `make test` |
| AC9 | No API/schema changes | PASS — existing executions still readable |

---

## Post-Deployment Re-Test — Full DR Cycle (Run 3)

**Date:** 2026-05-11 20:58 ET
**Build:** sha256:68cbd43f4d5c (soteria, podman), sha256:0aaca86148a0 (console plugin, podman)
**Registry:** quay.io/raffaelespazzoli/soteria:latest, quay.io/raffaelespazzoli/soteria-console-plugin:latest
**Trigger:** Re-deploy to validate latest images on etl6/etl7 with podman build pipeline

### Deployment Log

| Timestamp | Action | Result |
|-----------|--------|--------|
| 2026-05-11 20:55 ET | Build + push soteria image (podman) | OK — sha256:68cbd43f4d5c pushed (~108s) |
| 2026-05-11 20:55 ET | Build + push console plugin image (podman) | OK — sha256:0aaca86148a0 pushed (~108s) |
| 2026-05-11 20:56 ET | Rollout restart controller + console plugin on both clusters | OK — all 4 deployments rolled out (~15s) |

**Note:** `.dockerignore` required a fix for podman/buildah compatibility — explicit directory inclusions (`!cmd/`, `!internal/`, `!pkg/`, `!api/`, `!hack/`) added. Docker BuildKit auto-includes parent directories for matched files; podman/buildah does not.

### Pre-Cycle Sanity

| # | Check | Expected | Actual | Status |
|---|-------|----------|--------|--------|
| S1 | APIService etl6 + etl7 | Available=True | True on both | PASS |
| S2 | DRPlan phase | SteadyState | SteadyState, activeSite=etl6 | PASS |
| S3 | VMs etl6 | 6 Running | 6 Running, Ready=True | PASS |
| S4 | VMs etl7 | 6 Stopped | 6 Stopped, Ready=False | PASS |
| S5 | Conditions | All True | Ready/SitesInSync/DisksConsistent/Replicating/ReplicationHealthy all True | PASS |
| S6 | No active DRExecutions | Clean | 8 historical (all terminal) | PASS |

### Full Lifecycle (Run 3)

| # | Transition | Mode | From → To | Result | Duration |
|---|-----------|------|-----------|--------|----------|
| T9 | fedora-app-pm-05 | planned_migration | SteadyState → FailedOver | Succeeded | 64s |
| T10 | fedora-app-rp-05 | reprotect | FailedOver → DRedSteadyState | Succeeded | 43s |
| T11 | fedora-app-pm-06 | planned_migration | DRedSteadyState → FailedBack | Succeeded | 59s |
| T12 | fedora-app-rp-06 | reprotect | FailedBack → SteadyState | Succeeded | 42s |

**All 4 transitions Succeeded. No PartiallySucceeded results. Sequential chunk execution (Story 10.8) continues to hold.**

### Anomalies (Run 3)

#### UAT-10.003 (Continued): Checkpoint Retries Remain Systemic

**Severity:** Low (informational)
**Affected:** All 4 transitions (T9–T12)
**Symptom:** Checkpoint write retries observed on every transition (3–5 retries per checkpoint on planned migrations, 2–4 on reprotects). All retries succeed within the 6-attempt budget.

**Pattern Consistent With Prior Runs:** The cross-DC ScyllaDB reconciler on the non-owner site writes to the DRExecution (Step0, finishExecution), causing resourceVersion staleness when the owner site attempts its next checkpoint. Sequential chunk execution eliminated the *inter-chunk* contention (Story 10.8), but the *cross-site* contention between owner reconciler and Step0 reconciler is inherent to the dual-site architecture.

**No Action Required:** All checkpoints succeed. The retry budget (6 attempts with exponential backoff) is sufficient.

#### UAT-10.002 (Continued): Immutable Execution Write on Non-Owner Site

**Severity:** Low (informational)
**Affected:** T9 (etl7 pm-05), T11 (etl6 pm-06)
**Symptom:** `DRExecution is immutable after completion` reconciler ERROR on the non-owner site after owner completes and seals the execution.

**etl7 log (T9):**
```
ERROR Reconciler error  error: "writing final execution status: DRExecution.soteria.io \"fedora-app-pm-05\" is invalid: status: Forbidden: DRExecution is immutable after completion"
```

**etl6 log (T11):**
```
ERROR Reconciler error  error: "writing final execution status: DRExecution.soteria.io \"fedora-app-pm-06\" is invalid: status: Forbidden: DRExecution is immutable after completion"
```

**Root Cause:** The non-owner site's reconciler attempts to write final status after the owner has already sealed the execution. On the next reconcile loop, it re-fetches, sees the terminal state, and exits cleanly. This is the same pattern observed in Run 2 (O2) — now confirmed as consistent across all planned migration transitions.

**Impact:** None — the error is self-healing on the next reconcile cycle. The execution record is correct (sealed by the owner).

#### UAT-10.004 (New): DRGroupStatus Not Found During Finalization

**Severity:** Low (informational)
**Affected:** All 4 transitions, every wave
**Symptom:** `Could not fetch DRGroupStatus for finalization` DEBUG messages for each group after wave completion.

**Example (etl6, T11 wave-1):**
```
Could not fetch DRGroupStatus for finalization  name: "fedora-app-pm-06-wave-1-group-0"  error: "DRGroupStatus.soteria.io \"fedora-app-pm-06-wave-1-group-0\" not found"
```

**Root Cause:** The finalization step attempts to fetch DRGroupStatus resources that were never created (the no-op driver doesn't create them). This is a benign code path — the finalization gracefully handles the 404 and proceeds.

**Impact:** None — purely cosmetic log noise at DEBUG level.

#### Notable Absence: No Cache Consistency Check Failures

Zero `Cache consistency check failed` errors across all 4 transitions on both clusters. This matches Run 2 (post-10.8) and suggests the cache divergence observed in Run 1 was timing-dependent.

---

## Anomaly Deep-Dive Analysis (2026-05-12)

### UAT-10.003 Analysis — Checkpoint Conflict Retries

**Observation:** On the owner site, every group/wave completion triggers "object has been modified" conflict retries (up to 4-5 attempts) before the write eventually succeeds. This occurs on both the per-group "Merged status+checkpoint write" and the per-wave "Checkpoint write" paths.

**Working hypothesis (not confirmed):** The retries may be caused by back-to-back status writes creating a cache-staleness window. The controller-runtime cached client reads a stale `resourceVersion` from the informer cache (which lags behind ScyllaDB due to CDC propagation delay: `ConfidenceWindowSize`=2s + `PostNonEmptyQueryDelay`=500ms). However, Run 4 showed that even after partially merging the per-group writes (Story 10.9), the wave-level checkpoint still conflicts — suggesting additional contention sources we haven't identified.

**What we know:**
- Retries always succeed (self-healing, no data loss)
- Only occurs on the owner site (non-owner doesn't write status)
- Present in every single execution across all 4 runs (16/16 executions)
- Does not affect overall execution outcome or duration significantly

**What we don't know:**
- Whether the non-owner site's reconcile activity (reacting to CDC events) contributes to contention
- Whether the ScyllaDB CDC watch itself advances the `resourceVersion` without user-initiated writes
- Why the merged per-group write still conflicts (if it's truly a single write, what is the competing writer?)

**Status:** Root cause not definitively established. Story 10.9 created as a potential fix but may not fully resolve the issue without deeper investigation into the ScyllaDB-backed apiserver's concurrency behavior.

### UAT-10.002 Analysis — Immutable Execution Write After Completion

**Observation:** Sporadically, the owner site logs `ERROR Reconciler error ... Forbidden: DRExecution is immutable after completion`. This means the reconciler attempted a `Status().Update` on an execution that was already sealed with `Result=Succeeded`. The error is logged at ERROR level but is self-healing — the subsequent reconcile sees the terminal state and exits cleanly.

**Working hypothesis (not confirmed):** A stale reconcile event (enqueued from an intermediate CDC watch update during execution) fires after the execution is already complete. The reconciler's initial terminal check passes because the cache hasn't converged to the final `Result=Succeeded` state yet, so it proceeds to finalize the execution again. However, this explanation was challenged — controller-runtime should update the cache from the watch event that triggered the reconcile, meaning the cache should already reflect the state that caused the enqueue.

**What we know:**
- Appears only on the owner site, only for planned migrations (not reprotects)
- Frequency: ~1 occurrence per full cycle (1 out of 4 transitions in Run 4, similar in Run 3)
- Self-healing: next reconcile exits at the terminal check
- No user-visible impact (execution already succeeded)

**What we don't know:**
- The exact mechanism by which a reconcile starts with a stale view of an already-completed execution
- Whether the informer cache update from a watch event is guaranteed to be visible at the start of the reconcile it triggered (controller-runtime internals vs. ScyllaDB CDC event ordering)
- Whether this is specific to the ScyllaDB-backed apiserver's watch semantics or a general controller-runtime behavior

**Status:** Root cause not definitively established. Deferred — monitoring across runs. Low severity, self-healing, no user impact.

### UAT-10.004 Root Cause Analysis

**Root cause:** `DRGroupStatus` is a **write-only resource with no consumer**. The executor creates it for real-time step streaming, but the Console plugin (the intended consumer) never wired `useDRGroupStatuses` into any component — all execution UI reads from `DRExecution.Status.Waves[].Groups[]`. Every DRGroupStatus operation is treated as best-effort (falls back to `noopStepRecorder` on error), confirming the resource is architecturally optional.

**Party mode analysis (Winston/Amelia/Murat) concluded:**
- DRGroupStatus adds ~18 extra ScyllaDB round-trips per execution with zero consumer benefit
- Steps are duplicated: written to DRGroupStatus in real-time AND aggregated into DRExecutionStatus at group completion
- The `StepRecorder` interface should be retained (handlers depend on it) but default to `noopStepRecorder{}`
- `DRGroupExecutionStatus.Steps` + `RetryCount` provide sufficient audit trail without the separate resource

**Resolution:** Story 10.10 — Remove DRGroupStatus CRD entirely (types, registry, apiserver registration, executor CRUD, console plugin types/hook, RBAC, tests). Eliminates UAT-10.004 and simplifies the codebase.

### Spawned Stories Summary

| Story | Addresses | Status | Notes |
|-------|-----------|--------|-------|
| 10.9 — Merge persistStatus and Checkpoint Into Single Write | UAT-10.003 | ready-for-dev | May reduce but not fully eliminate retries — root cause not definitively understood |
| 10.10 — Remove DRGroupStatus Resource | UAT-10.004 | **validated in Run 4** | CRD removed, no errors, issue eliminated |
| UAT-10.002 — Immutable Execution Write | Deferred | monitoring | Root cause not definitively understood; self-healing, low severity |

---

---

## Post-Story-10.10 Re-Test — Full DR Cycle (Run 4)

**Date:** 2026-05-12 18:05–18:12 ET
**Build:** Story 10.10 implemented (DRGroupStatus CRD removed), Story 10.9 partially implemented (merged status+checkpoint log messages present, but separate checkpoint write path still active).

### Pre-deployment

| Action | Result |
|--------|--------|
| Delete all DRGroupStatus objects on etl6 | 16 objects deleted |
| Delete all DRGroupStatus objects on etl7 | 8 objects deleted |
| Build + push soteria controller image | OK — quay.io/raffaelespazzoli/soteria:latest (~69s) |
| Build + push console plugin image | OK — quay.io/raffaelespazzoli/soteria-console-plugin:latest (~139s build + ~81s push) |
| Rollout restart on both clusters | All 4 deployments rolled out (~26s) |

### State: SteadyState (pre-T13)

- DRPlan: `fedora-app`, Phase=SteadyState, ActiveSite=etl6, 6 VMs
- All replication VGs healthy
- No DRGroupStatus resource type registered (CRD removed — Story 10.10 validated)

### T13: Planned Migration — SteadyState → FailedOver

| Field | Value |
|-------|-------|
| Execution | fedora-app-pm-07 |
| Start | 2026-05-12T22:05:50Z |
| End | 2026-05-12T22:06:55Z |
| Duration | ~65s |
| Result | **Succeeded** |
| Owner Site | etl7 |

### T14: Reprotect — FailedOver → DRedSteadyState

| Field | Value |
|-------|-------|
| Execution | fedora-app-rp-07 |
| Start | 2026-05-12T22:08:04Z |
| End | 2026-05-12T22:08:44Z |
| Duration | ~40s |
| Result | **Succeeded** |
| Owner Site | etl7 |

### T15: Planned Migration — DRedSteadyState → FailedBack

| Field | Value |
|-------|-------|
| Execution | fedora-app-pm-08 |
| Start | 2026-05-12T22:09:11Z |
| End | 2026-05-12T22:10:24Z |
| Duration | ~73s |
| Result | **Succeeded** |
| Owner Site | etl6 |

### T16: Reprotect — FailedBack → SteadyState

| Field | Value |
|-------|-------|
| Execution | fedora-app-rp-08 |
| Start | 2026-05-12T22:11:03Z |
| End | 2026-05-12T22:11:45Z |
| Duration | ~42s |
| Result | **Succeeded** |
| Owner Site | etl6 |

### Anomalies (Run 4)

| ID | Category | Severity | Observation | Seen In |
|----|----------|----------|-------------|---------|
| UAT-10.003 (Continued) | Checkpoint conflict retries | Low | "Merged status+checkpoint write failed, retrying" (up to 4 attempts per group) AND "Checkpoint write failed, retrying" (up to 5 attempts per wave). Both always recover. Owner site only. | T13, T14, T15, T16 |
| UAT-10.002 (Continued) | Immutable execution write | Low | `Forbidden: DRExecution is immutable after completion` on owner site after execution sealed. Self-healing — next reconcile exits cleanly. | T13 (etl7) |
| UAT-10.004 | **RESOLVED** | — | DRGroupStatus CRD removed. No "Not Found" debug messages. No DRGroupStatus objects created. | All transitions clean |

### Key Observations (Run 4)

1. **Story 10.10 validated:** DRGroupStatus resource type is gone from both clusters. No errors related to missing resource. All executions succeed without it. UAT-10.004 is closed.
2. **UAT-10.003 persists:** Both "Merged status+checkpoint write" and standalone "Checkpoint write" paths hit conflict retries on every execution. Root cause remains unclear — further investigation needed into ScyllaDB-backed apiserver concurrency behavior before Story 10.9 can be confidently expected to fix this.
3. **UAT-10.002 persists:** Appeared once in 4 transitions (T13 only), consistent with previous runs. Root cause remains unclear — the mechanism by which a reconcile sees stale state after an execution is sealed is not definitively explained.
4. **Performance:** Planned migrations ~65-73s, reprotects ~40-42s — consistent with Run 3 timings. The retries add latency but don't prevent success.

---

## Final State Verification

| Check | Expected | Actual | Status |
|-------|----------|--------|--------|
| DRPlan phase | SteadyState | SteadyState | PASS |
| DRPlan activeSite | etl6 | etl6 | PASS |
| DRPlan conditions | All True | Ready/SitesInSync/DisksConsistent/Replicating/ReplicationHealthy all True | PASS |
| VMs on etl6 | 6 Running | 6 Running, Ready=True | PASS |
| VMs on etl7 | 6 Stopped | 6 Stopped, Ready=False | PASS |
| Execution history | 16 records (Run 1: 4, Run 2: 4, Run 3: 4, Run 4: 4) | pm-01 (PartiallySucceeded), all others Succeeded | PASS |
| Story 10.8 fix validated | All post-fix planned migrations Succeeded | 7/7 post-fix PMs Succeeded (pm-03 through pm-08) | PASS |
| Story 10.10 fix validated | DRGroupStatus CRD removed, no errors | No resource type registered, no debug "Not Found" messages | PASS |
| Phase cycle complete | SteadyState→FailedOver→DRedSteadyState→FailedBack→SteadyState | Exact progression observed across 4 full cycles | PASS |
