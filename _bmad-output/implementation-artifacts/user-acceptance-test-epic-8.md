# Epic 8 — User Acceptance Test Log

**Epic:** Cross-Site Discovery Agreement, API Simplification & UI Responsiveness
**Date Started:** 2026-05-02
**Environment:** etl6 / etl7 stretched cluster (Submariner MCS)
**Tester:** rspazzol

## Stories Under Test

| Story | Title | Dev Status |
|-------|-------|------------|
| 8.1 | Remove waveLabel from DRPlanSpec | done |
| 8.2 | Per-Site VM Discovery Reporting | done |
| 8.3 | Cross-Site VM Agreement & Plan Readiness Gating | done |
| 8.4 | Console UI — Site-Aware Plan Status & Disagreement Display | done |
| 8.5 | Optimistic DRExecution Detection in Console | done |

## Test Environment Setup

- **Clusters:** etl6 (primary/seed DC), etl7 (secondary DC)
- **ScyllaDB:** Multi-DC with NetworkTopologyStrategy
- **VMs:** fedora-db (wave 1), fedora-appserver-{1,2} (wave 2), fedora-webserver-{1,2,3} (wave 3)
- **DRPlan:** fedora-app (maxConcurrentFailovers: 2, primarySite: etl6, secondarySite: etl7)

## Deployment Log

| Timestamp | Action | Result |
|-----------|--------|--------|
| 2026-05-02 10:57 ET | Teardown existing deployment | OK |
| 2026-05-02 10:57 ET | Initial deploy via stretched-local-test.sh (with fedora-webserver-3) | OK — exit 0, 123s |
| 2026-05-02 11:04 ET | Noticed controller pod 43h old (stale image) | **Finding**: deploy script doesn't force pod restart on same `:latest` tag |
| 2026-05-02 11:04 ET | `rollout restart` controller + console plugin on both clusters | OK — new pods running |
| 2026-05-02 11:10 ET | Restart etl6 controller pod to fix stuck SitesInSync | OK — condition flipped to VMsAgreed |

---

## UAT Findings

### Sanity Checks

| # | Check | Expected | Actual | Status |
|---|-------|----------|--------|--------|
| S1 | DRPlan visible on etl6 | fedora-app listed | `fedora-app SteadyState` (6 VMs, 3 waves) | PASS |
| S2 | DRPlan visible on etl7 | fedora-app listed (cross-DC replication) | `fedora-app SteadyState` (6 VMs) — same as etl6 | PASS |
| S3 | VMs running on etl6 | 6 VMs running (Always) | 6 VMs Running/Ready=True (incl. fedora-webserver-3) | PASS |
| S4 | VMs halted on etl7 | 6 VMs halted | 6 VMs Stopped (incl. fedora-webserver-3) | PASS |
| S5 | APIService available on etl6 | Available=True | True | PASS |
| S6 | APIService available on etl7 | Available=True | True | PASS |
| S7 | Console plugin deployed | Both clusters | 1 replica ready on each cluster | PASS |

### Story 8.1 — Remove waveLabel from DRPlanSpec

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 8.1.1 | DRPlan spec has no waveLabel field | Field absent from spec | After controller restart: spec shows only `maxConcurrentFailovers`, `primarySite`, `secondarySite`. Field correctly removed from Go struct. | PASS |
| 8.1.2 | VMs grouped by soteria.io/wave label | 3 waves: db(1), appserver(2), webserver(3) | 3 waves, 6 VMs — webserver-3 grouped into wave 3 | PASS |
| 8.1.3 | Backward compat — old YAML with waveLabel accepted | No error on apply | server-side apply succeeds (with non-fatal internal-version warning); waveLabel silently dropped from Go struct round-trip | PASS |

### Story 8.2 — Per-Site VM Discovery Reporting

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 8.2.1 | primarySiteDiscovery populated on plan | VMs listed with count and timestamp | 6 VMs (all 6 names), discoveredVMCount=6, lastDiscoveryTime=2026-05-02T15:10:27Z | PASS |
| 8.2.2 | secondarySiteDiscovery populated on plan | VMs listed with count and timestamp | 6 VMs (same 6 names), discoveredVMCount=6, lastDiscoveryTime=2026-05-02T15:10:36Z | PASS |
| 8.2.3 | lastDiscoveryTime advances on reconcile | Timestamp updates | Not yet verified — controller requeue not firing (see UAT-8.002) | BLOCKED |

### Story 8.3 — Cross-Site VM Agreement & Plan Readiness Gating

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 8.3.1 | SitesInSync=True when VMs match | Condition True, reason VMsAgreed | `SitesInSync=True, VMsAgreed, "Both sites agree on VM inventory"` | PASS |
| 8.3.2 | SitesInSync=False on VM mismatch | Condition False, VMsMismatch + delta | Not yet tested (need to create asymmetric VM setup) | PENDING |
| 8.3.3 | DRExecution rejected when SitesInSync=False | Admission error | Not yet tested | PENDING |
| 8.3.4 | WaitingForDiscovery during initial deploy | Condition with guidance message | Observed: `WaitingForDiscovery, "Waiting for VM discovery from etl6"` — see UAT-8.001 | PASS (with caveat) |

### Story 8.4 — Console UI — Site-Aware Plan Status & Disagreement Display

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 8.4.1 | Site Discovery section in Configuration tab | Two-column VM comparison | Not yet tested (requires OpenShift console) | PENDING |
| 8.4.2 | Danger alert on SitesInSync=False | Alert above lifecycle diagram | Not yet tested | PENDING |
| 8.4.3 | Transition buttons disabled on mismatch | Buttons disabled + tooltip | Not yet tested | PENDING |
| 8.4.4 | Dashboard warning icon on mismatch | Warning icon + disabled kebab | Not yet tested | PENDING |
| 8.4.5 | Alert clears when condition flips True | UI updates on watch | Not yet tested | PENDING |

### Story 8.5 — Optimistic DRExecution Detection in Console

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 8.5.1 | Immediate banner on k8sCreate | "Starting \<mode\>..." + spinner | Not yet tested (requires OpenShift console) | PENDING |
| 8.5.2 | Smooth transition to real execution | No flash, real data takes over | Not yet tested | PENDING |
| 8.5.3 | No stale optimistic on page re-entry | Banner only from watch state | Not yet tested | PENDING |
| 8.5.4 | Create failure — no optimistic banner | Modal error only | Not yet tested | PENDING |

---

## Issues Found

### Issue UAT-8.001 — SitesInSync stuck as WaitingForDiscovery after fresh deploy

- **Story:** 8.3
- **Severity:** major
- **Steps to Reproduce:**
  1. Deploy fresh controllers on both clusters (or restart both controllers)
  2. Wait for initial reconcile cycle
  3. Observe SitesInSync condition
- **Expected:** After both sites write their SiteDiscovery (within ~60s), the SitesInSync condition should flip to `True/VMsAgreed`.
- **Actual:** SitesInSync remains `False/WaitingForDiscovery "Waiting for VM discovery from etl6"` even though `primarySiteDiscovery` is populated with 6 VMs. The condition and discovery are written in the SAME status patch (updateStatus), so `evaluateSiteAgreement` reads the plan BEFORE the active site's own discovery is populated. On the first reconcile, it sees primary=nil → writes WaitingForDiscovery. Then both condition and discovery are patched together. The next reconcile should correct this, but the controller doesn't requeue (see UAT-8.002).
- **Root Cause:** Race between `evaluateSiteAgreement` (reads plan early in reconcile) and `updateStatus` (writes SiteDiscovery later). The first reconcile always produces a stale WaitingForDiscovery condition.
- **Workaround:** Delete the controller pod → on restart, both discoveries are populated → SitesInSync correctly evaluates to VMsAgreed.
- **Resolution:** pending

### Issue UAT-8.002 — Controller does not requeue after successful reconcile

- **Story:** Pre-existing (not Epic 8 specific)
- **Severity:** major
- **Steps to Reproduce:**
  1. Controller completes a reconcile with `RequeueAfter: 30s`
  2. Wait 2+ minutes
  3. Check controller logs — no new reconcile
- **Expected:** Controller should re-reconcile every 30 seconds.
- **Actual:** After the initial reconcile, no further reconciles occur. The `RequeueAfter` return value appears to be lost. Annotating the DRPlan also does not trigger a reconcile via watch events.
- **Impact:** SitesInSync condition never self-corrects (UAT-8.001). Any status drift (e.g., VM count changes, health changes) won't be detected until manual pod restart.
- **Note:** This is a pre-existing issue with the ScyllaDB-backed aggregated API server's controller-runtime integration, not introduced by Epic 8.
- **Resolution:** pending — needs investigation of controller-runtime work queue / ScyllaDB CDC watch integration

### Issue UAT-8.003 — Replicating condition shows stale 5/5 VG count after adding VM

- **Story:** Pre-existing
- **Severity:** minor
- **Steps to Reproduce:**
  1. Add fedora-webserver-3 to the plan
  2. Controller discovers 6 VMs and creates a 6th volume group
  3. Check Replicating condition
- **Expected:** `6/6 volume groups healthy`
- **Actual:** `5/5 volume groups healthy` (from previous reconcile). Updates after controller restart.
- **Note:** Side effect of UAT-8.002 (no requeue).
- **Resolution:** Will resolve when UAT-8.002 is fixed

### Issue UAT-8.004 — stretched-local-test.sh does not force controller pod restart

- **Story:** Test infrastructure
- **Severity:** minor
- **Steps to Reproduce:**
  1. Make code changes, commit
  2. Run `stretched-local-test.sh`
  3. Observe controller pod age
- **Expected:** Controller pod should restart with new image.
- **Actual:** Pod continues running with stale image when deployment spec has same `:latest` tag. Deployment YAML is unchanged → `kubectl apply` is a no-op for the pod template.
- **Workaround:** Manually run `kubectl rollout restart` after the script completes.
- **Resolution:** pending — script should add `kubectl rollout restart` or use image digest

### Issue UAT-8.005 — Test script DRPlan YAML still contained waveLabel field

- **Story:** 8.1 (test artifact)
- **Severity:** cosmetic
- **Steps to Reproduce:**
  1. Inspect `hack/stretched-local-test.sh` DRPlan YAML
- **Expected:** No `waveLabel` in DRPlan spec (story 8.1 removed it).
- **Actual:** The script's inline DRPlan YAML still had `waveLabel: soteria.io/wave`.
- **Resolution:** fixed in this session (removed from script)

<!-- Template for new issues:
### Issue UAT-8.NNN — <title>

- **Story:** 8.X
- **Severity:** critical / major / minor / cosmetic
- **Steps to Reproduce:**
  1. ...
- **Expected:** ...
- **Actual:** ...
- **Screenshot:** (if applicable)
- **Resolution:** pending / fixed in <commit> / deferred
-->
