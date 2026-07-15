# Story 15.11: Remove ResyncVolume from Planned Migration Step 0

Status: done

## Context

UAT for Epic 15 revealed that T3 (planned migration west→east, the symmetric reverse of T1 east→west) consistently fails during Step 0 with `no snapshot details: last sync time not found` on the target's VolumeReplication CRs. Investigation traced the root cause to an incorrect ordering of operations in the multi-site planned migration Step 0.

### Root Cause Analysis

**The multi-site Step 0 flow demotes the source BEFORE calling ResyncVolume on the target — the opposite of the single-site flow and the Ceph mirroring model.**

| Path | Order | Status |
|------|-------|--------|
| Single-site (`LocalSite == ""`) | ResyncVolume on target → wait → StopReplication on source | Works (resync pulls from active primary) |
| Multi-site (`LocalSite != ""`) | StopReplication on source → signal VMsStopped → ResyncVolume on target | **Broken** (no primary to pull from) |
| Ceph planned failover model | Demote primary → rbd-mirror daemon auto-syncs demotion snapshot → promote secondary | No explicit ResyncVolume needed |
| Ramen (OCM-DR) relocate | Set source VRG to Secondary → wait for VR healthy → promote target VRG to Primary | No explicit ResyncVolume on target |

After StopReplication (demote) on the source, both sides become non-primary. The rbd-mirror daemon reports "remote image is not primary" and the CSI Addons `ResyncVolume` RPC on the target fails because `GetLastSyncInfo` can't find valid snapshot metadata in the mixed snapshot chain.

**Why T1 works but T3 fails**: In T1, the target (west) was the original secondary with a clean snapshot chain — `ResyncVolume` finds valid metadata even though the source has been demoted. In T3, the target (east) was a former primary (demoted during T1 + reprotected during T2) with a mixed snapshot chain (old `.mirror.primary` snaps + received `.mirror.non_primary` snaps), and `GetLastSyncInfo` chokes with "no snapshot details."

### Evidence

RBD snapshot analysis confirmed the demotion snapshot IS delivered to the target automatically:
- West (source, T3) snap 6992: `.mirror.primary` — demoted snapshot at 20:24:32
- East (target, T3) snap 8447: `.mirror.non_primary` — received copy at 20:24:33, marked `copied`

The data is already consistent. The target just needs to be promoted — no ResyncVolume needed.

## Story

As a DR operator,
I want planned migration to align with the Ceph snapshot-based mirroring model (demote → auto-sync → promote),
so that planned migrations work symmetrically in both directions without ResyncVolume failures.

## Design

### New Multi-Site Step 0 Flow

Replace the 3-signal protocol (`VMsStopped` → `ResyncComplete` → `Step0Complete`) with a 2-signal protocol (`DemotionComplete` → `Step0Complete`):

| Step | Source site | Signal | Target site |
|------|------------|--------|-------------|
| 1 | Stop VMs | | |
| 2 | StopReplication (demote local VRs) | | |
| 3 | Wait for local VRs: `state=Secondary`, `Completed=True`, `Degraded=False` | | |
| 4 | | → `DemotionComplete` | |
| 5 | | | See `DemotionComplete` |
| 6 | | | Promote (SetSource on local VRs) |
| 7 | | ← `Step0Complete` | |
| 8 | (done) | | Proceed to wave execution |

### SiteCoordinationStatus Changes

Replace `VMsStopped` + `ResyncPending` + `ResyncComplete` with `DemotionComplete`:

```go
type SiteCoordinationStatus struct {
    // DemotionComplete is set by the source site (Step0) after all local
    // primary VRs have been demoted to secondary AND the demotion snapshot
    // has been confirmed synced (Completed=True, Degraded=False).
    // Signals the target site to promote its VRs to primary.
    DemotionComplete bool `json:"demotionComplete,omitempty"`
    // Step0Complete is set by the target site after promoting its VRs to
    // primary. Signals the source site that Step 0 is done and waves can
    // proceed.
    Step0Complete bool `json:"step0Complete,omitempty"`
    // LastUpdated records when this site last wrote to its coordination status.
    // +optional
    LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}
```

Note: `Step0Complete` moves from source-written to target-written. This is a semantic change — the target is now the authority on when Step 0 is done (it's the one that promotes).

### Single-Site Path Alignment

The single-site path (`reconcileResyncGate`) should also be aligned:
1. `PreExecute`: Stop VMs (no ResyncVolume, no SkipResync branching)
2. Reconciler: StopReplication on source → wait for VRs healthy → promote target → done

Remove `SkipResync` flag, `ErrResyncRequested`, and the ResyncVolume call from `PreExecute` entirely.

## Acceptance Criteria

**AC1: No ResyncVolume in PreExecute**
Given a planned migration (GracefulShutdown=true)
When PreExecute runs Step 0
Then it stops all origin VMs and calls StopReplication on all source VGs (demote)
And it does NOT call ResyncVolume on any VG
And the `SkipResync` config field is removed

**AC2: Source site waits for demotion health**
Given the source site has demoted all local VRs
When the reconciler checks VR status
Then it waits until all source VRs show `state=Secondary`, `Completed=True`, `Degraded=False`
And it sets `siteStatuses[localSite].demotionComplete = true`

**AC3: Target site promotes after DemotionComplete**
Given the target site sees `siteStatuses[otherSite].demotionComplete = true`
When it reconciles
Then it calls SetSource (promote) on all local VRs
And after promotion succeeds, it sets `siteStatuses[localSite].step0Complete = true`
And it does NOT call ResyncVolume

**AC4: SiteCoordinationStatus simplified**
Given the DRExecution API types
When the schema is updated
Then `SiteCoordinationStatus` has `DemotionComplete`, `Step0Complete`, and `LastUpdated` fields
And `VMsStopped`, `ResyncPending`, `ResyncComplete` are removed
And `make manifests generate` succeeds

**AC5: Single-site path aligned**
Given a single-site planned migration (`LocalSite == ""`)
When Step 0 runs
Then PreExecute stops VMs and demotes source VRs (StopReplication)
And the reconciler waits for VR health (`Completed=True`, `Degraded=False`)
And then promotes target VRs (SetSource)
And the `ResyncPending` condition is no longer used
And `ErrResyncRequested` is removed

**AC6: T1 and T3 both succeed**
Given the full lifecycle e2e test (planned-snapshot scenario)
When T1 (east→west) and T3 (west→east) are executed
Then both transitions complete successfully
And the resync timeout is not triggered

**AC7: Console UI panel updated**
Given the console UI `SiteCoordinationPanel` and TypeScript types
When the schema changes
Then the TypeScript `SiteCoordinationStatus` interface reflects the new fields
And the panel displays "Demoting Volumes" and "Demotion Synced" for the source
And the panel displays "Promoting Volumes" for the target

## Tasks / Subtasks

- [x] Task 1: Update `SiteCoordinationStatus` in `pkg/apis/soteria.io/v1alpha1/types.go` — replace `VMsStopped`/`ResyncPending`/`ResyncComplete` with `DemotionComplete`, move `Step0Complete` semantics to target-written (AC: 4)
- [x] Task 2: Run `make manifests generate` (AC: 4)
- [x] Task 3: Rewrite `PreExecute` in `pkg/engine/failover.go` — remove `SkipResync` flag, always do StopVMs + StopReplication, remove ResyncVolume call and `ErrResyncRequested` (AC: 1, 5)
- [x] Task 4: Rewrite `reconcileStep0` in `reconciler.go` — after PreExecute, wait for local VR health (`Completed=True`, `Degraded=False`), then set `demotionComplete` (AC: 2)
- [x] Task 5: Rewrite `reconcileTargetSiteResyncGate` → rename to `reconcileTargetStep0` — wait for `demotionComplete`, call SetSource (promote), set `step0Complete` (AC: 3)
- [x] Task 6: Rewrite `reconcileStep0ResyncGate` → rename to `reconcileSourceStep0Wait` — wait for `step0Complete` from target (AC: 2)
- [x] Task 7: Rewrite single-site `reconcileResyncGate` — align with new ordering: wait for VR health after demote, then promote, remove `ConditionResyncPending` (AC: 5)
- [x] Task 8: Remove `checkSourceVRsHealthy` health gate — the demotion health wait (Task 4) subsumes it (AC: 2)
- [x] Task 9: Update routing logic in `Reconcile()` and `reconcileWaveExecution()` — use new signal names (AC: 2, 3)
- [x] Task 10: Update `reconcileResume` for new Step0 signals (AC: 5)
- [x] Task 11: Update unit tests in `reconciler_test.go` (AC: 1-5)
- [x] Task 12: Update `failover_test.go` / engine tests (AC: 1)
- [x] Task 13: Update e2e test helpers — `observeResyncPending` → adapt to new signals (AC: 6)
- [x] Task 14: Update console UI TypeScript types + `SiteCoordinationPanel` (AC: 7)
- [x] Task 15: Run `make lint-fix && make test` — all unit tests pass (AC: 1-5)
- [ ] Task 16: Build, deploy, and run planned-snapshot T1→T2→T3→T4 full lifecycle (AC: 6)

### Review Findings

- [x] [Review][Patch] Resuming a legacy single-site `ResyncPending` execution can bypass the Step 0 gate and jump straight into wave execution [`pkg/controller/drexecution/reconciler.go:218`]
- [x] [Review][Patch] Target Step 0 marks `Step0Complete` even after skipping `SetSource` for a missing local volume group [`pkg/controller/drexecution/reconciler.go:1801`]
- [x] [Review][Patch] Single-site Step 0 timeout starts from execution start instead of the post-demotion health-wait window [`pkg/controller/drexecution/reconciler.go:1857`]
- [x] [Review][Patch] The console maps `Demotion Synced` to `step0Complete` instead of the source's `demotionComplete` signal [`console-plugin/src/components/ExecutionDetail/SiteCoordinationPanel.tsx:23`]

## Dev Notes

### Key Files to Modify

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | `SiteCoordinationStatus` struct |
| `pkg/engine/failover.go` | `PreExecute`, remove `SkipResync`, `ErrResyncRequested` |
| `pkg/controller/drexecution/reconciler.go` | Step 0 source/target/single-site paths, routing |
| `pkg/controller/drexecution/reconciler_test.go` | Unit tests |
| `test/multisite/helpers_test.go` | E2E helpers |
| `test/multisite/lifecycle_test.go` | E2E test assertions |
| `console-plugin/src/models/types.ts` | TypeScript interface |
| `console-plugin/src/components/ExecutionDetail/SiteCoordinationPanel.tsx` | UI panel |

### Architecture Constraints

- **Idempotent reconciliation**: All new logic must be safe to re-execute on every reconcile loop
- **Site ownership rule**: Each controller writes ONLY to `siteStatuses[r.LocalSite]`
- **Single-site compatibility**: `LocalSite == ""` must still work without `SiteStatuses`
- **Timeout safety net**: Keep `resyncTimeout` (now `demotionTimeout`) for the source-side wait
- **Structured logging**: Follow K8s logging conventions (capital first letter, no trailing period, past tense)
- **After editing types**: Run `make manifests generate`
- **After editing Go**: Run `make lint-fix && make test`

### What NOT to Change

- Wave execution pipeline (SetSource + StartVM per group)
- DRPlan reconciler (VR/VGR creation)
- Reprotect handler (`reconcileReprotectPassive`)
- Disaster failover path (no Step 0)
- ScyllaDB storage layer

### Previous Story Intelligence

Story 15.10 introduced `SiteStatuses` and the site-owned coordination model. The signal names (`VMsStopped`, `ResyncPending`, `ResyncComplete`, `Step0Complete`) are being replaced in this story. The ownership pattern (each site writes to its own entry) is preserved.

Story 15.2 introduced the original resync guard with `ErrResyncRequested` and `ConditionResyncPending`. This story removes that mechanism entirely.

### Project Structure Notes

- Go types: `pkg/apis/soteria.io/v1alpha1/types.go`
- Engine: `pkg/engine/failover.go`
- Reconciler: `pkg/controller/drexecution/reconciler.go`
- Console types: `console-plugin/src/models/types.ts`
- E2E tests: `test/multisite/` (build tag `multisite`)

### References

- [Source: pkg/engine/failover.go] — `PreExecute`, `SkipResync`, `ErrResyncRequested`
- [Source: pkg/controller/drexecution/reconciler.go] — `reconcileStep0`, `reconcileTargetSiteResyncGate`, `reconcileStep0ResyncGate`, `reconcileResyncGate`
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — `SiteCoordinationStatus`
- [Source: _bmad-output/implementation-artifacts/15-10-site-owned-coordination-status.md] — Previous story context
- [Source: _bmad-output/implementation-artifacts/user-acceptance-test-epic-15.md] — T3 failure findings
- Ceph RBD snapshot-based mirroring planned failover model: demote → auto-sync → promote
- Ramen (OCM-DR) VRG relocate flow: source Secondary → wait healthy → target Primary

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Pre-existing flaky integration test (`TestRecovery_DC1Recovers_StateReconciles`) accepted by user — ScyllaDB connectivity/read-repair issue unrelated to story scope
- Reconciler tasks 4-10 implemented as a single unit due to high interdependency
- Lint dupl issue in reconciler_test.go resolved by converting two near-identical tests into a table-driven test

### Completion Notes List

- ✅ `SiteCoordinationStatus` simplified from 5 fields (`VMsStopped`, `ResyncPending`, `ResyncComplete`, `Step0Complete`, `LastUpdated`) to 3 (`DemotionComplete`, `Step0Complete`, `LastUpdated`). `Step0Complete` semantics changed from source-written to target-written.
- ✅ `PreExecute` rewritten: always does StopVMs + StopReplication (demote), returns `nil`. Removed `SkipResync` config field, `ErrResyncRequested` sentinel error, and all `ResyncVolume` calls.
- ✅ Multi-site source path: `reconcileStep0` runs PreExecute → `checkVRsHealthy` waits for VR health (Completed=True, Degraded=False) → sets `DemotionComplete`. `reconcileSourceStep0Wait` waits for target's `Step0Complete`.
- ✅ Multi-site target path: `reconcileTargetStep0` waits for source's `DemotionComplete` → calls `SetSource` (promote) → sets `Step0Complete`.
- ✅ Single-site path: `reconcileResyncGate` aligned — checks VR health → calls SetSource → sets Step0Complete condition. `ConditionResyncPending` removed.
- ✅ `checkSourceVRsHealthy` removed — subsumed by demotion health wait in `checkVRsHealthy`.
- ✅ All unit tests updated and passing (Go: `make test`, Console: `npm test` — 627/627 pass).
- ✅ E2E helpers updated: `observeResyncPending` → `observeDemotionComplete`, `observeStep0Complete`.
- ✅ Console UI updated: TypeScript types + SiteCoordinationPanel displays "Demoting Volumes" / "Demotion Synced" / "Promoting Volumes".
- ⏳ Task 16 (deploy and run planned-snapshot lifecycle) pending — requires cluster deployment.

### File List

- `pkg/apis/soteria.io/v1alpha1/types.go` (modified)
- `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` (regenerated)
- `hack/api-violations.list` (regenerated)
- `pkg/engine/failover.go` (modified)
- `pkg/engine/doc.go` (modified)
- `pkg/engine/failover_test.go` (modified)
- `pkg/controller/drexecution/reconciler.go` (modified)
- `pkg/controller/drexecution/doc.go` (modified)
- `pkg/controller/drexecution/reconciler_test.go` (modified)
- `test/multisite/helpers_test.go` (modified)
- `test/multisite/lifecycle_test.go` (modified)
- `console-plugin/src/models/types.ts` (modified)
- `console-plugin/src/components/ExecutionDetail/SiteCoordinationPanel.tsx` (modified)
- `console-plugin/tests/components/SiteCoordinationPanel.test.tsx` (modified)

### Change Log

- 2025-07-15: Implemented story 15.11 — removed ResyncVolume from planned migration Step 0, simplified SiteCoordinationStatus to 2-signal protocol (DemotionComplete → Step0Complete), aligned single-site path, updated all tests and console UI
