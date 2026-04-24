# Story 5.5: Site-Aware Reconcile Ownership

Status: ready-for-dev

## Story

As a DR operator,
I want each site's reconciler to know which cluster it runs on and only perform the work assigned to that site for each transition phase,
So that cross-site contention is eliminated and the DR workflow is correct even when both sites are healthy and reconciling simultaneously.

## Background

During E2E testing on etl6/etl7, we observed that both sites' reconcilers race to reconcile the same DRExecution, causing "object has been modified" contention storms. Today neither `DRExecutionReconciler` nor `DRPlanReconciler` has a site identity — both controllers on both clusters do identical work on every reconcile. This causes:

1. **Contention**: Concurrent writes to the same DRExecution status from two controllers
2. **Incorrect semantics**: During failover from east to west, the east reconciler shouldn't be starting VMs — only the west (target) site should
3. **Planned migration race**: Both sites try to run Step 0 and wave execution simultaneously

The fix is explicit **site-aware reconcile ownership**: each controller instance knows its site name and computes its role (Owner, Step0Only, or None) based on the current transition phase.

## Ownership Model

The **target site** (the site becoming active) owns the transition. The source site's only job during planned migration is Step 0 (stop VMs, stop replication, sync wait). During disaster mode, the source site does nothing.

| Phase | Source site (currently active) | Target site (becoming active) |
|---|---|---|
| **FailingOver** (east→west) | Planned: Run Step 0, update status, exit. Disaster: Exit immediately. | Wait for Step 0 status (planned) or start immediately (disaster). Run SetSource + StartVM waves. |
| **Reprotecting** | Exit immediately | Owner: establish replication |
| **FailingBack** (west→east) | Planned: Run Step 0, update status, exit. Disaster: Exit immediately. | Wait for Step 0 status (planned) or start immediately (disaster). Run SetSource + StartVM waves. |
| **ReprotectingBack** | Exit immediately | Owner: establish replication |

**Planned migration handoff sequence:**

```
Source site:  StopVM → WaitVMStopped → StopReplication → WaitForSync → Update DRExecution status (Step0Complete)
                  ↓ (status propagates via ScyllaDB cross-DC replication)
Target site:  Sees Step0Complete → SetSource wave 1 → StartVM wave 1 → WaitVMReady → wave 2 → ...
```

This is a **serialized handoff through DRExecution status**. No concurrent writes — the source completes Step 0 and stops touching the object; the target picks up from there. No status write partitioning needed.

**Disaster failover:**

```
Source site:  Exit immediately (site may be down)
Target site:  SetSource wave 1 → StartVM wave 1 → WaitVMReady → wave 2 → ...
```

## Acceptance Criteria

1. **AC1 — Site identity flag:** The controller manager accepts a `--site-name` flag (required). The value must match either `DRPlan.Spec.PrimarySite` or `DRPlan.Spec.SecondarySite` for any plan the controller reconciles. Stored as `LocalSite string` on both `DRExecutionReconciler` and `DRPlanReconciler`.

2. **AC2 — ReconcileRole function:** A pure function `ReconcileRole(phase, mode, localSite, primarySite, secondarySite) Role` in `pkg/engine/roles.go` computes the reconciler's role. Returns `RoleOwner`, `RoleStep0`, or `RoleNone`. Logic:
   - `FailingOver`: target is `secondarySite`. If local == target → `RoleOwner`. If local == source AND mode == `planned_migration` → `RoleStep0`. Else → `RoleNone`.
   - `Reprotecting`: target is `secondarySite`. If local == target → `RoleOwner`. Else → `RoleNone`.
   - `FailingBack`: target is `primarySite`. If local == target → `RoleOwner`. If local == source AND mode == `planned_migration` → `RoleStep0`. Else → `RoleNone`.
   - `ReprotectingBack`: target is `primarySite`. If local == target → `RoleOwner`. Else → `RoleNone`.
   - Rest states: `RoleNone` for DRExecution reconciler (no active execution to process).

3. **AC3 — DRExecution reconciler gating:** At the top of `Reconcile`, after fetching the DRPlan, the reconciler computes its role. If `RoleNone`: log and return immediately. If `RoleStep0`: dispatch to `reconcileStep0` (Task 5). If `RoleOwner`: proceed with existing execution logic (setup, waves, completion).

4. **AC4 — RoleOwner waits for Step 0 in planned migration:** When the target-site reconciler is `RoleOwner` for a planned migration, it gates wave execution on `Step0Complete` condition in `DRExecution.Status.Conditions`. If not present, return `RequeueAfter(5s)` — the source site is still running Step 0. If present, proceed with waves.

5. **AC5 — RoleStep0 reconcile path:** The source-site reconciler in `RoleStep0` runs the existing Step 0 logic (stop VMs, stop replication, sync wait) and updates the DRExecution status with `Step0Complete` condition. After Step 0 completes, it returns and does not touch the DRExecution again. The VM stop must use the event-driven watch-and-wait model (Story 5.5 dependency).

6. **AC6 — Disaster mode source site exits immediately:** When mode is `disaster` and the reconciler computes `RoleNone` (source site), it returns without any action. The target site is the sole owner.

7. **AC7 — Misconfiguration guard:** If `localSite` does not match either `primarySite` or `secondarySite` of a DRPlan, the reconciler logs an error and returns without processing. This prevents silent misrouting.

8. **AC8 — DRPlan reconciler site awareness:** The DRPlan reconciler uses `LocalSite` to optimize VM discovery and health polling — it only discovers/polls VMs on the local site. Both sites continue reconciling DRPlan status (preflight, wave grouping) since this is read-heavy and safe to run on both.

9. **AC9 — Reprotect ownership:** During `Reprotecting` and `ReprotectingBack`, only the target site runs the `ReprotectHandler`. The source site exits immediately (reprotect is storage-only, no VM operations on the source).

10. **AC10 — Unit tests:** Tests covering: (a) `ReconcileRole` returns correct role for all 8 phases x 2 sites x 3 modes; (b) reconciler exits immediately for `RoleNone`; (c) reconciler dispatches Step 0 for `RoleStep0`; (d) reconciler proceeds with waves for `RoleOwner`; (e) misconfiguration guard fires when `localSite` doesn't match; (f) planned migration target waits for `Step0Complete`; (g) disaster mode source exits immediately.

11. **AC11 — Integration tests:** At least one integration test with two reconciler instances (different `LocalSite` values) verifying that only one performs wave execution for a given DRExecution.

## Tasks / Subtasks

- [ ] Task 1: Add `--site-name` flag and `LocalSite` field (AC: #1)
  - [ ] 1.1 Add `--site-name` string flag to `cmd/soteria/main.go` (required, no default)
  - [ ] 1.2 Add `LocalSite string` field to `DRExecutionReconciler` struct in `pkg/controller/drexecution/reconciler.go`
  - [ ] 1.3 Add `LocalSite string` field to `DRPlanReconciler` struct in `pkg/controller/drplan/reconciler.go`
  - [ ] 1.4 Wire the flag value into both reconcilers in `main.go`
  - [ ] 1.5 Validate flag is non-empty at startup; fail fast if missing
  - [ ] 1.6 Update `hack/stretched-local-test.sh` to pass `--site-name` matching each cluster's context name

- [ ] Task 2: Implement `ReconcileRole` function (AC: #2)
  - [ ] 2.1 Create `pkg/engine/roles.go` with `Role` type (`RoleOwner`, `RoleStep0`, `RoleNone`)
  - [ ] 2.2 Implement `ReconcileRole(phase, mode, localSite, primarySite, secondarySite) Role`
  - [ ] 2.3 Implement `TargetSiteForPhase(phase, primarySite, secondarySite) string` helper — returns which site becomes active for a given transition phase
  - [ ] 2.4 Comprehensive unit tests: all phase x site x mode combinations

- [ ] Task 3: Gate DRExecution reconciler on role (AC: #3, #6, #7)
  - [ ] 3.1 After fetching DRPlan in `Reconcile`, compute `role := engine.ReconcileRole(...)`
  - [ ] 3.2 If `RoleNone`: log `"Skipping reconcile, not the owning site"` at V(1) and return
  - [ ] 3.3 If `RoleStep0`: dispatch to `reconcileStep0(ctx, &exec, &plan)` (Task 5)
  - [ ] 3.4 If `RoleOwner`: continue with existing logic
  - [ ] 3.5 Add misconfiguration guard: if `localSite` matches neither `primarySite` nor `secondarySite`, log error and return
  - [ ] 3.6 Unit tests for each dispatch path

- [ ] Task 4: Gate target-site on Step0Complete for planned migration (AC: #4)
  - [ ] 4.1 In the `RoleOwner` path for `planned_migration` mode, before calling `WaveExecutor.Execute`, check for `Step0Complete` condition
  - [ ] 4.2 If `Step0Complete` not present: return `RequeueAfter(5s)` — source site is still running Step 0
  - [ ] 4.3 If `Step0Complete` present: proceed with wave execution
  - [ ] 4.4 For disaster mode: skip Step0Complete check entirely (no Step 0)
  - [ ] 4.5 Unit tests: target waits when Step0Complete absent, proceeds when present

- [ ] Task 5: Implement `reconcileStep0` for source site (AC: #5)
  - [ ] 5.1 Create `reconcileStep0(ctx, exec, plan)` method on `DRExecutionReconciler`
  - [ ] 5.2 If `Step0Complete` condition already set: return (idempotent — source already finished its job)
  - [ ] 5.3 Else: run Step 0 logic — stop VMs (using event-driven watch from Story 5.5), stop replication, wait for sync
  - [ ] 5.4 On completion: patch DRExecution status with `Step0Complete` condition
  - [ ] 5.5 Return — do not proceed to wave execution
  - [ ] 5.6 Unit tests for Step 0 execution and idempotent re-entry

- [ ] Task 6: Update DRPlan reconciler with site awareness (AC: #8)
  - [ ] 6.1 Add `LocalSite` to DRPlanReconciler struct (already done in Task 1.3)
  - [ ] 6.2 Use `LocalSite` to scope VM discovery to the local site (optimization, not correctness — both sites can still discover all VMs)
  - [ ] 6.3 No gating on DRPlan reconciler — both sites continue reconciling DRPlan status

- [ ] Task 7: Update tests (AC: #10, #11)
  - [ ] 7.1 Unit tests for `ReconcileRole` — all combinations
  - [ ] 7.2 Unit tests for reconciler gating — RoleNone, RoleStep0, RoleOwner dispatch
  - [ ] 7.3 Unit tests for misconfiguration guard
  - [ ] 7.4 Unit tests for Step0Complete gating on target site
  - [ ] 7.5 Unit tests for `reconcileStep0` idempotency
  - [ ] 7.6 Integration test: two reconcilers with different `LocalSite` values
  - [ ] 7.7 Update all existing DRExecution reconciler tests to set `LocalSite` (use the target site so tests exercise the Owner path)

## Dev Notes

### Architecture: Serialized Handoff via Status

The key insight is that during planned migration, the source and target sites never write to the same DRExecution concurrently. The workflow is serialized:

1. **Source site** owns Step 0: stop VMs → wait → stop replication → sync → write `Step0Complete` condition
2. **Target site** sees `Step0Complete` → runs waves (SetSource → StartVM → WaitVMReady per wave)

The `Step0Complete` condition in `DRExecution.Status.Conditions` is the handoff signal. It propagates from source to target via ScyllaDB cross-DC replication. The target site polls for it with `RequeueAfter(5s)`.

During disaster mode, there's no handoff — the target site starts immediately and the source site does nothing.

### Prerequisite for Story 5.6

Story 5.6 (Event-Driven Wave Gate) depends on this story for:
- `LocalSite` field on reconcilers
- Role-based dispatch in `Reconcile`
- The understanding that only the `RoleOwner` site runs waves

Story 5.6 adds the VM readiness gate between waves. This story adds the site ownership gate that determines which site runs waves at all.

### Key Existing Patterns to Reuse

- **`ActiveSiteForPhase` in `pkg/engine/statemachine.go`**: Already computes which site is active after a phase completes. `TargetSiteForPhase` is the inverse — which site is *becoming* active during a transition.
- **`Step0Complete` condition**: Already exists in the reconciler (Story 4.1b/4.3). The target-site gating just needs to check for it before proceeding.
- **`specOrAnnotationChanged` predicate**: Already on the DRExecution `.For()` watch. The `Step0Complete` condition update from the source site arrives via informer event, not via the predicate (status writes don't bump generation). The target site is woken by `RequeueAfter(5s)` safety net.

### Files to Touch

| File | Changes |
|------|---------|
| `cmd/soteria/main.go` | Add `--site-name` flag, wire to reconcilers |
| `pkg/engine/roles.go` | New file: `ReconcileRole`, `TargetSiteForPhase`, Role type |
| `pkg/controller/drexecution/reconciler.go` | Add `LocalSite`, role gating, `reconcileStep0`, Step0Complete wait |
| `pkg/controller/drplan/reconciler.go` | Add `LocalSite`, optional site-scoped VM discovery |
| `hack/stretched-local-test.sh` | Pass `--site-name` per cluster |
| `pkg/engine/roles_test.go` | New file: comprehensive role tests |
| Test files | Update existing tests to set `LocalSite` |

### Critical Constraints

- **`--site-name` is required** — the controller must fail to start without it. No auto-detection magic.
- **`ReconcileRole` must be a pure function** — no side effects, no API calls. Fully testable with table-driven tests.
- **Existing tests must set `LocalSite`** — use the target site value so they exercise the `RoleOwner` path unchanged. This is a backward-compatible change.
- **Do NOT gate the DRPlan reconciler** — both sites should continue discovering VMs and polling health. Only the DRExecution reconciler gates on site ownership.
- **`Step0Complete` is already wired** — the existing planned migration path sets this condition. This story just moves Step 0 execution to the source site's reconciler and makes the target site wait for it.

### Testing Standards

- Ginkgo + Gomega BDD style for new tests
- `ReconcileRole` tested with table-driven tests covering all 8 phases x 2 sites x 3 modes
- Integration tests use envtest with two reconciler instances
- All existing tests must continue to pass (with `LocalSite` set)
- Minimum 90% coverage for `pkg/engine/roles.go`

### References

- [Source: pkg/controller/drexecution/reconciler.go — DRExecutionReconciler struct, Reconcile, SetupWithManager]
- [Source: pkg/controller/drplan/reconciler.go — DRPlanReconciler struct]
- [Source: pkg/engine/statemachine.go — ActiveSiteForPhase, validTransitions, completionTransitions]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlanSpec.PrimarySite, SecondarySite, DRPlanStatus.ActiveSite]
- [Source: _bmad-output/implementation-artifacts/4-1b-state-machine-symmetry-unified-failover-handler.md — 8-phase model, transition states]
- [Source: _bmad-output/implementation-artifacts/5-6-event-driven-wave-gate-vm-readiness.md — VM readiness gate, event-driven model]
- [Source: cmd/soteria/main.go — controller manager setup, flag parsing]
- [Source: hack/stretched-local-test.sh — deployment script for etl6/etl7]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
