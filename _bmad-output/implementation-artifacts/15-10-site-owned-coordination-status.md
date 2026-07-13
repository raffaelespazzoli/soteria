# Story 15.10: Site-Owned Coordination Status (ScyllaDB LWW Fix)

Status: done

## Context

During multi-site planned migration, the source site (Step0 role) and the target
site (Owner role) both write coordination signals to `DRExecution.Status.Conditions`.
Because the aggregated API server stores DRExecution status in ScyllaDB with
eventual consistency (Last-Write-Wins), concurrent condition patches from different
sites can silently overwrite each other. When the source site sets `Step0Complete`
at the same wall-clock second that the target site sets `ResyncComplete`, LWW picks
the later ScyllaDB timestamp and discards the other write entirely — the `Conditions`
array is replaced wholesale by a JSON Merge Patch, not merged per-element.

This was observed in UAT-15.011: T1 planned migration timed out because east's
`Step0Complete` patch was overwritten by west's `ResyncComplete` patch (1-second
LWW race). The execution deadlocked — west waited for `Step0Complete` that was
silently lost.

## Story

As a DR operator,
I want multi-site coordination signals to be stored in site-owned status sections,
so that concurrent status updates from different sites never conflict via ScyllaDB LWW.

## Design

### New Types

```go
// SiteCoordinationStatus holds coordination signals written exclusively
// by one site's controller. Each site writes only to its own entry in
// the DRExecution's SiteStatuses map.
type SiteCoordinationStatus struct {
    // VMsStopped is set by the source site (Step0) after stopping all VMs
    // during planned migration. Signals the target site to begin resync.
    VMsStopped bool `json:"vmsStopped,omitempty"`
    // Step0Complete is set by the source site (Step0) after resync completes
    // and StopReplication has demoted local primary VRs. Signals the target
    // site to proceed with wave execution (SetSource + StartVM).
    Step0Complete bool `json:"step0Complete,omitempty"`
    // ResyncPending is set by the target site (Owner) after calling
    // ResyncVolume on its local secondary VR/VGR CRs. Indicates the
    // target is waiting for VR status watches to confirm resync completion.
    ResyncPending bool `json:"resyncPending,omitempty"`
    // ResyncComplete is set by the target site (Owner) after all local
    // VRs have completed resync. Signals the source site to proceed
    // with StopReplication and Step0Complete.
    ResyncComplete bool `json:"resyncComplete,omitempty"`
    // LastUpdated records when this site last wrote to its coordination status.
    // +optional
    LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}
```

### Schema Change

Add to `DRExecutionStatus`:

```go
// SiteStatuses holds per-site coordination signals, keyed by site name
// (e.g., "east", "west"). Each controller writes ONLY to its own site
// entry and reads from the other site's entry. This avoids ScyllaDB LWW
// conflicts from concurrent cross-site status patches.
// +optional
SiteStatuses map[string]SiteCoordinationStatus `json:"siteStatuses,omitempty"`
```

### Ownership Rules

| Controller Role | Writes to | Reads from |
|-----------------|-----------|------------|
| Step0 (source site) | `siteStatuses[localSite].vmsStopped`, `.step0Complete` | `siteStatuses[otherSite].resyncComplete` |
| Owner (target site) | `siteStatuses[localSite].resyncPending`, `.resyncComplete` | `siteStatuses[otherSite].vmsStopped`, `.step0Complete` |
| ReprotectPassive | `siteStatuses[localSite]` (future use) | — |

Each controller patches only its own `siteStatuses[r.LocalSite]` sub-object.
JSON Merge Patch on `siteStatuses.east` does not touch `siteStatuses.west`,
eliminating the LWW conflict.

### Conditions Array

The `Conditions` array is retained for Owner-only lifecycle signals:
- `Progressing` — set/cleared by the Owner during wave execution
- `RetryRejected` — set by the webhook when a retry is rejected

No coordination signals (VMsStopped, ResyncPending, ResyncComplete, Step0Complete)
are stored in `Conditions`. They move entirely to `SiteStatuses`.

### Single-Site Path

For single-site executions (no `r.LocalSite`), the existing `Conditions`-based
resync gate (`reconcileResyncGate`) continues to use `Conditions` because there
is no cross-site conflict. This path is unaffected by this change.

## Acceptance Criteria

**AC1: SiteCoordinationStatus type and SiteStatuses field**
Given the DRExecution API types
When the schema is updated
Then `DRExecutionStatus` has a `SiteStatuses map[string]SiteCoordinationStatus` field
And `SiteCoordinationStatus` has boolean fields for `VMsStopped`, `Step0Complete`, `ResyncPending`, `ResyncComplete`, and a `LastUpdated` timestamp
And `make manifests generate` succeeds

**AC2: Source site (Step0) writes only to its own site entry**
Given a multi-site planned migration
When the source site stops VMs
Then it sets `siteStatuses[localSite].vmsStopped = true`
And when Step0 completes (resync + StopReplication)
Then it sets `siteStatuses[localSite].step0Complete = true`
And it never writes to `siteStatuses[otherSite]`
And it never writes VMsStopped or Step0Complete to `Conditions`

**AC3: Target site (Owner) writes only to its own site entry**
Given a multi-site planned migration
When the target site initiates resync
Then it sets `siteStatuses[localSite].resyncPending = true`
And when resync completes
Then it sets `siteStatuses[localSite].resyncComplete = true`
And it never writes to `siteStatuses[otherSite]`
And it never writes ResyncPending or ResyncComplete to `Conditions`

**AC4: Cross-site reads use SiteStatuses**
Given a multi-site planned migration
When the source site checks for resync completion
Then it reads `siteStatuses[otherSite].resyncComplete`
And when the target site checks for Step0 completion
Then it reads `siteStatuses[otherSite].step0Complete`
And when the target site checks for VMs stopped
Then it reads `siteStatuses[otherSite].vmsStopped`

**AC5: No LWW conflict under concurrent writes**
Given both sites update their SiteStatuses within the same second
When ScyllaDB applies LWW conflict resolution
Then both sites' coordination signals are preserved
Because each site writes to a different JSON path

**AC6: Single-site path unchanged**
Given a single-site execution (no LocalSite configured)
When the reconciler runs
Then it uses the existing `Conditions`-based resync gate (`reconcileResyncGate`)
And `SiteStatuses` is not used

**AC7: E2E test observeResyncPending updated**
Given the e2e test helper `observeResyncPending`
When polling for execution progress
Then it reads coordination status from `exec.Status.SiteStatuses`
And diagnostic output includes site-specific coordination signals

**AC8: Console UI unaffected**
Given the console-plugin UI
When rendering DRExecution status
Then it continues to use `status.phase`, `status.result`, `status.waves`
And the coordination conditions removed from `Conditions` were never rendered by the UI

## Tasks / Subtasks

- [x] Task 1: Add `SiteCoordinationStatus` type and `SiteStatuses` field to `DRExecutionStatus` in `pkg/apis/soteria.io/v1alpha1/types.go` (AC: 1)
- [x] Task 2: Run `make manifests generate` to regenerate CRDs and DeepCopy (AC: 1)
- [x] Task 3: Add helper functions for reading/writing site coordination status (e.g., `getSiteStatus`, `setSiteStatus`, `getOtherSiteStatus`) in reconciler (AC: 2, 3, 4)
- [x] Task 4: Update `reconcileStep0` to write `VMsStopped` and `Step0Complete` to `siteStatuses[localSite]` instead of `Conditions` (AC: 2)
- [x] Task 5: Update `reconcileStep0ResyncGate` to read `ResyncComplete` from `siteStatuses[otherSite]` and write `Step0Complete` to `siteStatuses[localSite]` (AC: 2, 4)
- [x] Task 6: Update `reconcileTargetSiteResyncGate` to read `VMsStopped` and `Step0Complete` from `siteStatuses[otherSite]`, write `ResyncPending` and `ResyncComplete` to `siteStatuses[localSite]` (AC: 3, 4)
- [x] Task 7: Update `Reconcile` and `reconcileWaveExecution` routing logic to read from `SiteStatuses` instead of `Conditions` for multi-site coordination checks (AC: 4)
- [x] Task 8: Update `reconcileResume` to read `Step0Complete` from `SiteStatuses` for multi-site path (AC: 4)
- [x] Task 9: Verify single-site `reconcileResyncGate` is unchanged (AC: 6)
- [x] Task 10: Update `observeResyncPending` test helper to read from `SiteStatuses` (AC: 7)
- [x] Task 11: Update unit tests in `reconciler_test.go` for SiteStatuses assertions (AC: 2, 3, 4, 5)
- [x] Task 12: Run `make lint-fix && make test` — all unit tests pass (AC: 1-6)
- [x] Task 13: Build, deploy, and run planned-snapshot e2e test (AC: 5, 7)

### Review Findings

- [x] [Review][Dismiss] Single-site planned migration deadlocks when `LocalSite` is set — dismissed: `dispatchByRole` prevents single-site plans from reaching the multi-site resync gate
- [x] [Review][Patch] Upgrade path breaks in-flight multi-site executions that still store Step 0 state only in `Conditions` — fixed: added legacy Conditions fallback at five gate locations
- [x] [Review][Patch] Source Step 0 can bypass the target-owned `ResyncComplete` handshake — fixed: removed local `checkResyncComplete` fallback from `reconcileStep0ResyncGate`

## Technical Notes

- The `map[string]SiteCoordinationStatus` keys are stable site names (e.g., `"east"`, `"west"`) from the DRPlan spec. These never change regardless of which direction the migration goes.
- During T1 (east→west): east writes to `siteStatuses["east"]`, west writes to `siteStatuses["west"]`. During T3 (west→east): the same — each site always writes to its own key.
- The `Conditions` array retains `Progressing` (set by the executor), `RetryRejected` (set by the webhook), and any future Owner-only lifecycle conditions.
- `ResyncPending` in `SiteStatuses` replaces the `ResyncPending` condition for multi-site. The single-site path continues to use the `ResyncPending` condition in `reconcileResyncGate`.
- Lazy initialization: the reconciler creates the map and site entry on first write. No pre-initialization needed.

## References

- UAT-15.011: ScyllaDB LWW race condition in `user-acceptance-test-epic-15.md`
- Story 15.2: Planned Failover Resync Guard — original conditions-based design
- Story 15.9: Full Lifecycle E2E Test — test infrastructure
