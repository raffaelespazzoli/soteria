# Story 10.3: Derived Active Execution for Reconciler Gates & VM Watch

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want the DRPlan reconciler's replication health gate and the DRExecution reconciler's VM watch routing to derive active execution state from DRExecution resources,
So that no runtime code reads `plan.Status.ActiveExecution`.

## Background

After Stories 10.1 and 10.2, the DRExecution reconciler no longer writes `ActiveExecution` / `ActiveExecutionMode` on the DRPlan, and the table convertor / preflight composition already derive active execution from DRExecution resources. Two consumers still read `plan.Status.ActiveExecution`:

1. **DRPlan reconciler health polling gate** (`pkg/controller/drplan/reconciler.go:390`) — checks `plan.Status.ActiveExecution == ""` to decide whether to poll replication health. Polling is skipped during an active execution because the engine owns driver interactions.
2. **`mapVMToDRExecution` handler** (`pkg/controller/drexecution/reconciler.go:1554-1576`) — reads `plan.Status.ActiveExecution` to route VM change events to the active DRExecution for wave gate processing.

Both consumers must be migrated to derive active execution from DRExecution resources via label-filtered LIST (`soteria.io/plan-name`).

**Scope boundary:** The `reconcileSetup` ActiveExecution write removal was completed in Story 10.1. The `ActiveExecution`/`ActiveExecutionMode` fields remain on the `DRPlanStatus` struct (removal is Story 10.4). This story removes the last two **read** sites in Go production code.

**After 10.3, zero Go production code reads or writes `plan.Status.ActiveExecution`** — the field becomes dead on the struct, ready for Story 10.4's removal.

## Acceptance Criteria

1. **AC1 — Health polling gate derives from DRExecution query:** The DRPlan reconciler's replication health polling gate queries DRExecutions for the plan via `client.MatchingLabels{soteriav1alpha1.PlanNameLabel: plan.Name}`, finds any non-terminal execution (`status.result == ""`), and skips polling if one exists. The `plan.Status.ActiveExecution` read is removed.

2. **AC2 — VM watch routing derives from DRExecution query:** `mapVMToDRExecution` determines the active execution for the VM's plan by listing DRExecutions with `client.MatchingLabels{soteriav1alpha1.PlanNameLabel: planName}`, finding the non-terminal execution, and enqueuing it. The `plan.Status.ActiveExecution` read and the DRPlan GET are removed.

3. **AC3 — SetupWithManager comment updated:** The comment on the VM watch registration (`reconciler.go:1523`) is updated to reflect that VM events are routed via DRExecution label query, not via `DRPlan.ActiveExecution`.

4. **AC4 — Tests:** Health polling tests verify skip/proceed based on DRExecution presence (not plan status). VM watch routing tests verify correct execution enqueue from DRExecution query. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Migrate health polling gate to DRExecution query (AC: #1)
  - [ ] 1.1 In `pkg/controller/drplan/reconciler.go`, add a helper method `(r *DRPlanReconciler) hasActiveExecution(ctx context.Context, planName string) bool` that lists DRExecutions via `client.MatchingLabels{soteriav1alpha1.PlanNameLabel: planName}` and returns `true` if any non-terminal execution (`Status.Result == ""`) exists. On error, log a warning and return `false` (degrade to polling — safe direction)
  - [ ] 1.2 Replace line ~390: change `plan.Status.ActiveExecution == ""` to `!r.hasActiveExecution(ctx, plan.Name)` — the condition reads: "poll when registry is wired AND no active execution"
  - [ ] 1.3 Update the comment above (lines 387-388) to reflect the new derived pattern: "no execution is active (derived from DRExecution resources)"

- [ ] Task 2: Migrate mapVMToDRExecution to DRExecution query (AC: #2)
  - [ ] 2.1 Rewrite `mapVMToDRExecution` in `pkg/controller/drexecution/reconciler.go` (lines 1554-1576):
    - Keep: extract `planName` from VM's `soteria.io/drplan` label; return nil if empty
    - Remove: the `r.Get(ctx, ..., &plan)` call and the `plan.Status.ActiveExecution` read
    - New: LIST DRExecutions via `r.List(ctx, &execList, client.MatchingLabels{soteriav1alpha1.PlanNameLabel: planName})`; on error, return nil. Find the first non-terminal execution (`Status.Result == ""`); if found, enqueue it. If none found, return nil
  - [ ] 2.2 Update the function's godoc comment: "maps a VirtualMachine event to the active DRExecution (if any) by querying DRExecutions with the soteria.io/plan-name label"
  - [ ] 2.3 Remove the `"sigs.k8s.io/controller-runtime/pkg/client"` types import for `types.NamespacedName` if it's no longer needed (verify — it's likely still used elsewhere)

- [ ] Task 3: Update SetupWithManager comment (AC: #3)
  - [ ] 3.1 In `SetupWithManager` (~line 1523), update the comment from "The mapper routes VM events to the active DRExecution via the soteria.io/drplan label → DRPlan.ActiveExecution" to "The mapper routes VM events to the active DRExecution by querying DRExecutions with the soteria.io/plan-name label (derived from DRExecution resources, not DRPlan status)"

- [ ] Task 4: Update health polling test (AC: #4)
  - [ ] 4.1 In `pkg/controller/drplan/health_test.go`, rewrite `TestReconcile_ActiveExecution_SkipsPolling` → `TestReconcile_ActiveDRExecution_SkipsPolling`:
    - Remove `plan.Status.ActiveExecution = "exec-1"` and `plan.Status.ActiveExecutionMode = ...` from the plan fixture
    - Create a non-terminal DRExecution fixture: `&soteriav1alpha1.DRExecution{ObjectMeta: metav1.ObjectMeta{Name: "exec-1", Labels: map[string]string{soteriav1alpha1.PlanNameLabel: "plan-1"}}, Spec: soteriav1alpha1.DRExecutionSpec{PlanName: "plan-1", Mode: soteriav1alpha1.ExecutionModePlannedMigration}}`
    - Add the DRExecution to the test client objects
    - Keep the same assertion: `ReplicationHealth` should be empty during active execution
  - [ ] 4.2 Add `TestReconcile_NoDRExecution_PollsHealth`: same as the existing health polling test but without any DRExecution — verify `ReplicationHealth` is populated (polling proceeds). This may already exist as an implicit case, but make it explicit
  - [ ] 4.3 Add `TestReconcile_TerminalDRExecution_PollsHealth`: create a DRExecution with `Status.Result: "Succeeded"` — verify polling proceeds (terminal executions don't block polling)

- [ ] Task 5: Update mapVMToDRExecution test (AC: #4)
  - [ ] 5.1 Rewrite `TestMapVMToDRExecution` in `pkg/controller/drexecution/reconciler_test.go` (lines 1297-1364):
    - **Active execution case:** Remove `plan.Status.ActiveExecution = "exec-active"` from the plan fixture. Create a non-terminal DRExecution `exec-active` with `Labels: {PlanNameLabel: "plan-map"}` and `Spec.PlanName: "plan-map"`. Add it to the test client. Verify `mapVMToDRExecution` returns `[]reconcile.Request{{NamespacedName: {Name: "exec-active"}}}`
    - **No label case:** VM without `soteria.io/drplan` label → returns nil (no change)
    - **Idle plan case:** Plan with no DRExecutions → returns nil (remove `plan2` with empty ActiveExecution; instead just use a plan with no associated DRExecutions)
    - **Terminal execution case (NEW):** Plan has a DRExecution with `Status.Result: "Succeeded"` → returns nil (terminal doesn't route)

- [ ] Task 6: Verify no remaining ActiveExecution reads in Go production code (AC: #4)
  - [ ] 6.1 Run `rg 'ActiveExecution' pkg/ internal/ --type go` and verify every remaining reference is either:
    - The struct field definition in `types.go` (removed in Story 10.4)
    - The `PrepareForCreate` zeroing in `drplan/strategy.go` (removed in Story 10.4)
    - DeepCopy generated code (`zz_generated.deepcopy.go`)
    - The `EffectivePhase` function signature (operates on parameters, not plan status)
    - Test code in `_test.go` files that may still reference the field for backward-compat fixtures
  - [ ] 6.2 If any production code still reads/writes `ActiveExecution` on `plan.Status`, migrate it. This task is a safety sweep — no changes expected if Tasks 1-2 are correct

- [ ] Task 7: Run full test suite and verify (AC: #4)
  - [ ] 7.1 Run `make test` — all unit and integration tests pass with zero regressions
  - [ ] 7.2 Run `make lint` — zero lint errors

## Dev Notes

### Scope & Architecture

This story touches two reconcilers: the DRPlan reconciler (health polling gate) and the DRExecution reconciler (VM watch routing). Both are read-side consumers of "active execution" state that must be migrated from plan status to DRExecution query.

**After Story 10.1:** `plan.Status.ActiveExecution` is never written by new code paths. The fields remain on the struct with zero values for new executions.

**After Story 10.2:** Table convertor and preflight composition derive active execution from DRExecution resources.

**This story (10.3) migrates the final two read-side consumers**, making `ActiveExecution`/`ActiveExecutionMode` fully dead fields on the struct — ready for removal in Story 10.4.

**NOT in scope:**
- Removing `ActiveExecution`/`ActiveExecutionMode` from `DRPlanStatus` struct — Story 10.4
- Console UI derivation — Story 10.5
- Any write-side changes (completed in Story 10.1)

### Critical: Health Polling Gate

**Current code (`pkg/controller/drplan/reconciler.go:387-394`):**

```go
// Poll replication health when the driver infrastructure is wired and no
// execution is active (the engine owns driver interactions during execution).
var replicationHealth []soteriav1alpha1.VolumeGroupHealth
if r.Registry != nil && plan.Status.ActiveExecution == "" {
    replicationHealth = r.pollReplicationHealth(ctx, &plan, waves)
    logger.V(1).Info("Replication health polled",
        "totalVGs", len(replicationHealth))
}
```

**New pattern — add helper method + replace condition:**

```go
// hasActiveExecution checks whether any non-terminal DRExecution exists for
// the given plan by querying DRExecutions with the soteria.io/plan-name label.
func (r *DRPlanReconciler) hasActiveExecution(ctx context.Context, planName string) bool {
    var execList soteriav1alpha1.DRExecutionList
    if err := r.List(ctx, &execList,
        client.MatchingLabels{soteriav1alpha1.PlanNameLabel: planName}); err != nil {
        log.FromContext(ctx).Error(err, "Failed to list DRExecutions for health gate")
        return false
    }
    for i := range execList.Items {
        if execList.Items[i].Status.Result == "" {
            return true
        }
    }
    return false
}
```

**Gate replacement:**

```go
// Poll replication health when the driver infrastructure is wired and no
// execution is active (derived from DRExecution resources — the engine owns
// driver interactions during execution).
var replicationHealth []soteriav1alpha1.VolumeGroupHealth
if r.Registry != nil && !r.hasActiveExecution(ctx, plan.Name) {
    replicationHealth = r.pollReplicationHealth(ctx, &plan, waves)
    logger.V(1).Info("Replication health polled",
        "totalVGs", len(replicationHealth))
}
```

**Graceful degradation on error:** If the DRExecution LIST fails, `hasActiveExecution` returns `false` (no active execution detected) which means polling proceeds. This is the safe direction — polling when there's actually an active execution wastes a few driver calls but doesn't corrupt state. The alternative (returning `true` on error) would silently suppress health monitoring.

**RBAC:** The DRPlan reconciler already has RBAC to list DRExecutions (added in Story 10.2, Task 5.1: `+kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=list`). No additional RBAC markers needed.

### Critical: mapVMToDRExecution Rewrite

**Current code (`pkg/controller/drexecution/reconciler.go:1554-1576`):**

```go
func (r *DRExecutionReconciler) mapVMToDRExecution(
    ctx context.Context, obj client.Object,
) []reconcile.Request {
    planName := obj.GetLabels()[soteriav1alpha1.DRPlanLabel]
    if planName == "" {
        return nil
    }

    var plan soteriav1alpha1.DRPlan
    if err := r.Get(ctx, types.NamespacedName{Name: planName}, &plan); err != nil {
        return nil
    }

    if plan.Status.ActiveExecution == "" {
        return nil
    }

    return []reconcile.Request{{
        NamespacedName: types.NamespacedName{Name: plan.Status.ActiveExecution},
    }}
}
```

**New pattern — query DRExecutions directly:**

```go
// mapVMToDRExecution maps a VirtualMachine event to the active DRExecution
// (if any) by querying DRExecutions with the soteria.io/plan-name label.
func (r *DRExecutionReconciler) mapVMToDRExecution(
    ctx context.Context, obj client.Object,
) []reconcile.Request {
    planName := obj.GetLabels()[soteriav1alpha1.DRPlanLabel]
    if planName == "" {
        return nil
    }

    var execList soteriav1alpha1.DRExecutionList
    if err := r.List(ctx, &execList,
        client.MatchingLabels{soteriav1alpha1.PlanNameLabel: planName}); err != nil {
        return nil
    }

    for i := range execList.Items {
        if execList.Items[i].Status.Result == "" {
            return []reconcile.Request{{
                NamespacedName: types.NamespacedName{Name: execList.Items[i].Name},
            }}
        }
    }

    return nil
}
```

**Key changes:**
- Removes the DRPlan GET entirely — no need to read the plan to find the active execution
- Removes the dependency on `plan.Status.ActiveExecution`
- Uses `PlanNameLabel` (from Story 10.1) for label-filtered LIST
- Finds the first non-terminal execution and enqueues it
- `types.NamespacedName` is still used for the return value — keep the import

**Performance note:** This mapper is called per-VM event, not in bulk. The label-filtered LIST goes through the controller-runtime cacher (in-memory watch cache) and is filtered by the `soteria.io/plan-name` label index. Typically returns 0 or 1 results. This is comparable in cost to the previous DRPlan GET (also through the cacher).

### Critical: Test Pattern Changes

**Health polling test (`health_test.go`):**

The test `TestReconcile_ActiveExecution_SkipsPolling` currently creates a plan with `ActiveExecution: "exec-1"` set on the plan status, then verifies that `ReplicationHealth` is empty after reconcile. After this change, the test must:
1. Create a non-terminal DRExecution with `Labels: {PlanNameLabel: "plan-1"}` — the helper `hasActiveExecution` will find it via LIST
2. Remove `ActiveExecution` from the plan fixture
3. Keep the same assertion

**Important — `fake.NewClientBuilder()` and LIST behavior:** The controller-runtime fake client supports `client.MatchingLabels` filtering. It uses the object's labels for matching. Ensure the DRExecution fixture has the `PlanNameLabel` label set. The fake client does NOT require index registration for label selectors (unlike the real cacher which needs explicit index registration for field selectors).

**mapVMToDRExecution test (`reconciler_test.go`):**

The test `TestMapVMToDRExecution` currently creates a plan with `ActiveExecution: "exec-active"` and no DRExecution objects. After this change:
1. Create a DRExecution `exec-active` with labels `{PlanNameLabel: "plan-map"}` and empty `Status.Result`
2. Add it to the test client
3. Remove `ActiveExecution` from the plan fixture
4. The plan fixture is no longer needed (the mapper doesn't GET the plan) — but keep it for the `soteria.io/drplan` label mapping from VM to plan name

**`newTestClient` behavior:** The existing `newTestClient` helper builds a fake client from provided objects. Add the DRExecution to the objects list. Verify the helper registers `DRExecutionList` for LIST operations — it should via the scheme.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `client.MatchingLabels` for DRExecution query | Story 10.2 reconciler query (`composePreflightReport`) | Follow same label-filtered LIST pattern |
| `PlanNameLabel` constant | `pkg/apis/soteria.io/v1alpha1/types.go` (added in Story 10.1) | Use for label key |
| `Status.Result == ""` as non-terminal check | Story 10.1 `verifyExclusiveExecution`, Story 10.2 `buildActiveExecIndex` | Reuse same non-terminal detection |
| Graceful degradation on LIST error | Story 10.2 `buildActiveExecIndex` returns empty map on error | Follow — degrade safely |
| Health test reconciler setup | `newHealthTestReconciler` in `health_test.go` | Follow for DRExecution fixture injection |
| `newTestClient` for VM watch tests | `reconciler_test.go:1311` | Follow — add DRExecution to objects |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/controller/drplan/reconciler.go` | Add `hasActiveExecution` helper, replace line ~390 condition | ~15 lines |
| `pkg/controller/drexecution/reconciler.go` | Rewrite `mapVMToDRExecution` (~1554-1576), update `SetupWithManager` comment (~1523) | ~20 lines changed |
| `pkg/controller/drplan/health_test.go` | Rewrite `TestReconcile_ActiveExecution_SkipsPolling`, add 2 new tests | ~60 lines |
| `pkg/controller/drexecution/reconciler_test.go` | Rewrite `TestMapVMToDRExecution`, add terminal execution case | ~40 lines |

### Execution Order

1. Task 1 (health polling gate migration) — independent
2. Task 2 (mapVMToDRExecution rewrite) — independent of Task 1
3. Task 3 (comment update) — trivial, after Task 2
4. Tasks 4-5 (test updates) — after production code compiles
5. Task 6 (ActiveExecution sweep) — verification after all changes
6. Task 7 (full test suite) — final gate

### Previous Story Learnings (from 10.1 and 10.2)

- **Story 10.1 established `PlanNameLabel` constant** in `types.go` and stamps the label server-side in `PrepareForCreate`. Use `soteriav1alpha1.PlanNameLabel` for all `client.MatchingLabels` queries.
- **Story 10.1 `ensurePlanNameLabel`** in `reconcileSetup` backfills the label for pre-10.1 executions — label selector queries work for both old and new executions.
- **Story 10.2 added RBAC** for DRPlan reconciler to list DRExecutions (`+kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=list`). No additional RBAC marker needed in 10.3.
- **Story 10.2 `buildActiveExecIndex`** in the table convertor uses the same pattern (LIST + filter for non-terminal) — follow the same `Status.Result == ""` check.
- **controller-runtime fake client** supports `client.MatchingLabels` without explicit index registration — simpler than the real cacher.
- **`make manifests generate` is NOT needed** for this story — no types.go changes, no RBAC marker additions (already added in 10.2).

### Git Intelligence (Recent Patterns)

Recent commits follow a clear pattern: each story is a single commit with the story number as prefix. The most recent work is Epic 9 (disk discovery, admission migration, cross-site validation). Epic 10 stories 10.1 and 10.2 are in `ready-for-dev` status — this story assumes they are implemented first.

### Project Structure Notes

- DRPlan reconciler: `pkg/controller/drplan/reconciler.go` (health gate at line 390)
- DRPlan health tests: `pkg/controller/drplan/health_test.go` (ActiveExecution skip test at line 431)
- DRExecution reconciler: `pkg/controller/drexecution/reconciler.go` (mapVMToDRExecution at lines 1554-1576, SetupWithManager at lines 1516-1530)
- DRExecution reconciler tests: `pkg/controller/drexecution/reconciler_test.go` (TestMapVMToDRExecution at line 1297)
- Types: `pkg/apis/soteria.io/v1alpha1/types.go` (DRPlanStatus with ActiveExecution, PlanNameLabel constant)
- Auto-generated files (DO NOT EDIT): `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`, `config/rbac/role.yaml`

### References

- [Source: pkg/controller/drplan/reconciler.go#L387-L394] — Current health polling gate with ActiveExecution read
- [Source: pkg/controller/drplan/reconciler.go#L107-L122] — DRPlanReconciler struct with client.Client
- [Source: pkg/controller/drexecution/reconciler.go#L1554-L1576] — Current mapVMToDRExecution with ActiveExecution read
- [Source: pkg/controller/drexecution/reconciler.go#L1516-L1530] — SetupWithManager VM watch registration
- [Source: pkg/controller/drexecution/reconciler.go#L79-L93] — DRExecutionReconciler struct with client.Client
- [Source: pkg/controller/drplan/health_test.go#L431-L462] — TestReconcile_ActiveExecution_SkipsPolling
- [Source: pkg/controller/drexecution/reconciler_test.go#L1297-L1364] — TestMapVMToDRExecution
- [Source: pkg/controller/drplan/health_test.go#L57-L77] — newHealthTestReconciler helper
- [Source: _bmad-output/planning-artifacts/epics.md#Story-10.3] — Epic acceptance criteria
- [Source: _bmad-output/implementation-artifacts/10-1-drexecution-concurrency-guard-without-activeexecution.md] — Previous story: PlanNameLabel, verifyExclusiveExecution
- [Source: _bmad-output/implementation-artifacts/10-2-derived-active-execution-for-table-convertor-preflight.md] — Previous story: buildActiveExecIndex, RBAC marker
- [Source: _bmad-output/project-context.md] — Critical rules, ScyllaRetry, tiered comments

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
