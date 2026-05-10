# Story 10.1: DRExecution Concurrency Guard Without ActiveExecution

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want DRExecution creation to be rejected when another active execution exists for the same plan, without relying on a field stored on the DRPlan,
So that the DRPlan status is never mutated by execution lifecycle and the concurrency invariant is maintained at the DRExecution layer.

## Background

The current concurrency guard stores an `ActiveExecution` pointer on `DRPlanStatus` — the admission plugin reads it to reject concurrent creates, the reconciler sets/clears it on start/finish, and `fetchPlanWithActiveExecCheck` retries reads until the write propagates. This couples plan status to execution lifecycle and requires cross-DC SERIAL CAS on every plan status patch that touches `ActiveExecution`. Story 10.1 moves the guard to the DRExecution layer: admission queries DRExecution resources directly, the reconciler stops writing `ActiveExecution`/`ActiveExecutionMode` on the plan, and a reconciler-side exclusivity check replaces `fetchPlanWithActiveExecCheck`. The `ActiveExecution`/`ActiveExecutionMode` fields remain on the `DRPlanStatus` struct (removal is Story 10.4) — they are simply no longer read or written by the code paths modified here.

**Known intermediate state:** Between 10.1 and 10.2/10.3, consumers that still read `ActiveExecution` from the plan (table convertor, preflight, health polling gate, VM watch routing) will see empty values. This is acceptable because stories are developed and tested sequentially before deployment; 10.2/10.3 migrate these consumers.

## Acceptance Criteria

1. **AC1 — Admission concurrency guard queries DRExecutions:** The aggregated API admission plugin rejects DRExecution CREATE when any non-terminal execution (empty `status.result`) exists for the same `spec.planName`. The error message includes the blocking execution's name: "DRPlan \<name\> has active execution \<exec-name\>; concurrent executions not permitted".

2. **AC2 — Cross-DC race prevention via SERIAL INSERT:** DRExecution storage creates use SERIAL consistency (`gocql.Serial`) on the `INSERT IF NOT EXISTS` CAS operation, ensuring Paxos-level ordering across DCs. Combined with the admission gate and reconciler exclusivity check, at most one execution proceeds.

3. **AC3 — Reconciler no longer sets ActiveExecution:** `reconcileSetup` no longer patches `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode` on the DRPlan. The plan status patch in `reconcileSetup` is removed entirely (it contained only these two field writes).

4. **AC4 — Reconciler no longer clears ActiveExecution:** `finishExecution` (executor.go), `failExecution`, and the reprotect completion path no longer clear `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode`. Phase advancement and ActiveSite writes continue unchanged.

5. **AC5 — Reconciler exclusivity check replaces fetchPlanWithActiveExecCheck:** A new `verifyExclusiveExecution(ctx, planName, execName)` function lists DRExecutions for the plan (using `ScyllaRetry` backoff for eventual consistency), verifies that `execName` is the only non-terminal execution, and fails the execution if a competing one exists. Replaces all call sites of `fetchPlanWithActiveExecCheck`.

6. **AC6 — Critical field detector updated:** `detectDRPlanCriticalFields` no longer compares `ActiveExecution` — only `Phase` and `ActiveSite` trigger cross-DC SERIAL on DRPlan updates. DRExecution creation uses SERIAL consistency via the storage layer (AC2).

7. **AC7 — Tests:** Admission concurrency tests verify rejection via DRExecution query (not plan status). Reconciler tests verify no `ActiveExecution`/`ActiveExecutionMode` writes. Exclusivity check tests verify reconciler dedup. Critical field detector tests updated. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Add SERIAL create support to ScyllaDB storage layer (AC: #2)
  - [ ] 1.1 Add `SerialCreate bool` field to `StoreConfig` in `pkg/storage/scylladb/store.go`
  - [ ] 1.2 Store the flag on the `Store` struct: `serialCreate bool`
  - [ ] 1.3 In `casInsert`, use `gocql.Serial` when `s.serialCreate` is true, otherwise `gocql.LocalSerial`
  - [ ] 1.4 Add `SerialCreateResources map[schema.GroupResource]bool` to `ScyllaStoreFactory` in `pkg/apiserver/apiserver.go`
  - [ ] 1.5 In `soteriaRESTOptionsGetter.GetRESTOptions`, set `cfg.SerialCreate = true` when resource is in the set
  - [ ] 1.6 Add `DefaultSerialCreateResources()` in `pkg/apiserver/critical_fields.go` returning `drexecutions: true`
  - [ ] 1.7 Wire `DefaultSerialCreateResources()` into `ScyllaStoreFactory` construction in `pkg/apiserver/options.go`

- [ ] Task 2: Inject DRExecution storage into admission plugin (AC: #1)
  - [ ] 2.1 Add `drexecutionStorage rest.Lister` field to `SoteriaAdmissionPlugin` struct
  - [ ] 2.2 Add `SetDRExecutionStorage(s rest.Lister)` method
  - [ ] 2.3 In `apiserver.go`, after DRExecution `NewREST`, call `c.SoteriaPlugin.SetDRExecutionStorage(drexecStore)`
  - [ ] 2.4 Update `NewSoteriaAdmissionPlugin` godoc to mention both storage injections

- [ ] Task 3: Rewrite admission concurrency guard (AC: #1)
  - [ ] 3.1 In `validateDRExecution`, replace the `plan.Status.ActiveExecution != ""` check with a DRExecution LIST query
  - [ ] 3.2 Call `p.drexecutionStorage.List(ctx, &metainternalversion.ListOptions{})` to get all DRExecutions
  - [ ] 3.3 Filter results: find any DRExecution where `spec.planName == exec.Spec.PlanName` and `status.result == ""` (non-terminal)
  - [ ] 3.4 If a non-terminal execution exists, reject with the same error message format (preserving the existing message for backward compat)
  - [ ] 3.5 Add nil guard: if `p.drexecutionStorage == nil`, skip the concurrency check with a log warning (plugin may not be fully initialized)

- [ ] Task 4: Remove ActiveExecution writes from reconcileSetup (AC: #3)
  - [ ] 4.1 In `reconcileSetup` (~1318-1331), remove the `planPatch`/`ActiveExecution`/`ActiveExecutionMode` patch block and its error handling + log lines. **KEEP `ensurePlanNameLabel` call (~1333)** — it is a separate operation below the patch block
  - [ ] 4.2 Remove the `logger.Info("Set ActiveExecution on DRPlan", ...)` log line
  - [ ] 4.3 Verify the function still: calls `ensurePlanNameLabel`, initializes execution StartTime and conditions, yields with `RequeueAfter`

- [ ] Task 5: Remove ActiveExecution clearing from completion paths (AC: #4)
  - [ ] 5.1 In `finishExecution` (executor.go ~1032-1065): On success/partial, keep Phase and ActiveSite patches but remove `ActiveExecution = ""` and `ActiveExecutionMode = ""` writes. On failure path (~1055-1064), remove the entire "clear ActiveExecution" block (the `if plan.Status.ActiveExecution != ""` guard and its patch)
  - [ ] 5.2 In `failExecution` (reconciler.go ~979-989): Remove the entire block that clears `ActiveExecution`/`ActiveExecutionMode` on the plan (the `if len(plan) > 0 && plan[0] != nil && plan[0].Status.ActiveExecution == exec.Name` guard and its patch)
  - [ ] 5.3 In reprotect completion (reconciler.go ~687-722): On success/partial, keep Phase and ActiveSite patches but remove `ActiveExecution`/`ActiveExecutionMode` writes. On failure path, remove the "clear ActiveExecution" block
  - [ ] 5.4 Update log messages: remove "Cleared ActiveExecution on DRPlan" log lines

- [ ] Task 6: Replace fetchPlanWithActiveExecCheck with verifyExclusiveExecution (AC: #5)
  - [ ] 6.1 Add `verifyExclusiveExecution(ctx context.Context, planName, execName string) error` method on `DRExecutionReconciler`
  - [ ] 6.2 Implementation: LIST DRExecutions via `r.List(ctx, &execList, client.MatchingLabels{"soteria.io/plan-name": planName})` (or use the new `PlanNameLabel` constant after Task 12.2), use `ScyllaRetry` backoff to retry until a consistent view is obtained
  - [ ] 6.3 Filter for non-terminal: iterate list, find any execution with `Status.Result == ""` and `Name != execName`
  - [ ] 6.4 If a competing execution exists, return an error: "DRPlan %q has competing execution %q; this execution %q cannot proceed"
  - [ ] 6.5 Replace call site at ~783 (`reconcileReprotectResume`) — change `fetchPlanWithActiveExecCheck(ctx, planName, execName)` to `verifyExclusiveExecution(ctx, planName, execName)`. Adjust error handling: on error, call `failExecution` with reason `StaleExecution`
  - [ ] 6.6 Replace call site at ~877 (`reconcileResume`) — same pattern as 6.5
  - [ ] 6.7 Delete `fetchPlanWithActiveExecCheck` function (~1222-1251)

- [ ] Task 7: Update critical field detector (AC: #6)
  - [ ] 7.1 In `detectDRPlanCriticalFields` (critical_fields.go ~46-58), remove the `oldPlan.Status.ActiveExecution != newPlan.Status.ActiveExecution` comparison — keep only `Phase` and `ActiveSite`
  - [ ] 7.2 Update `TestDetectDRPlanCriticalFields_ActiveExecutionChange` test to expect `false` (ActiveExecution changes no longer trigger SERIAL)
  - [ ] 7.3 Rename the test to `TestDetectDRPlanCriticalFields_ActiveExecutionChange_NoLongerCritical` for clarity

- [ ] Task 8: Update legacy webhook validator (AC: #1, #7)
  - [ ] 8.1 In `pkg/admission/drexecution_validator.go`, update the `ActiveExecution` check to match the new query-based pattern, or add a comment noting the webhook is unused for soteria.io resources (VWC entries removed in 9.2) and leave it as-is
  - [ ] 8.2 If updating: the legacy validator doesn't have storage injection, so it cannot query DRExecutions — add a comment documenting this limitation

- [ ] Task 9: Update admission plugin unit tests (AC: #7)
  - [ ] 9.1 Add `stubLister` test helper implementing `rest.Lister` — returns configured `DRExecutionList`
  - [ ] 9.2 Update `TestPlugin_DRExecution_ActiveExecution_Denied` → `TestPlugin_DRExecution_ConcurrencyGuard_Denied`: set up `stubLister` returning a non-terminal DRExecution for the plan (instead of plan with ActiveExecution set). Plan fixture should have empty `ActiveExecution`
  - [ ] 9.3 Add `TestPlugin_DRExecution_ConcurrencyGuard_Allowed_NoActive`: `stubLister` returns empty list — CREATE proceeds
  - [ ] 9.4 Add `TestPlugin_DRExecution_ConcurrencyGuard_Allowed_TerminalOnly`: `stubLister` returns a DRExecution with `Result: Succeeded` — CREATE proceeds (terminal executions don't block)
  - [ ] 9.5 Update all existing admission tests to call `p.SetDRExecutionStorage(...)` with appropriate fixtures
  - [ ] 9.6 Add `TestPlugin_DRExecution_NilExecutionStorage_Proceeds`: verify CREATE proceeds when execution storage is nil (backward compat/initialization edge case)

- [ ] Task 10: Update reconciler and executor unit tests (AC: #7)
  - [ ] 10.1 Remove test assertions that check `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode` after reconcileSetup
  - [ ] 10.2 Remove test assertions that check clearing of `ActiveExecution`/`ActiveExecutionMode` after finishExecution/failExecution/reprotect completion
  - [ ] 10.3 Add tests for `verifyExclusiveExecution`: exclusive (single non-terminal) → success; competing execution → error; terminal-only siblings → success; empty list → success
  - [ ] 10.4 Update reconcileSetup tests to verify no plan status patch is made (only execution status is updated)

- [ ] Task 11: Update integration tests (AC: #7)
  - [ ] 11.1 Update `TestAdmission_DRExecution_ConcurrencyGate_Rejected` in `test/integration/apiserver/admission_test.go`: instead of setting `ActiveExecution` on the plan via status update, create a real non-terminal DRExecution first, then verify the second CREATE is rejected
  - [ ] 11.2 Add `TestAdmission_DRExecution_ConcurrencyGate_AllowedAfterCompletion`: create and complete an execution (set `Result: Succeeded`), then verify a new CREATE succeeds
  - [ ] 11.3 Verify all existing integration tests pass with zero regressions

- [ ] Task 12: Update registry strategies (AC: #1, #3)
  - [ ] 12.1 In DRPlan `PrepareForCreate` (`pkg/registry/drplan/strategy.go` ~47-54), remove `ActiveExecution = ""` and `ActiveExecutionMode = ""` initialization — these fields will still exist on the struct (removed in 10.4) but no longer need explicit zeroing since Go zero values are already empty strings
  - [ ] 12.2 Add `PlanNameLabel` constant to `pkg/apis/soteria.io/v1alpha1/types.go`: `const PlanNameLabel = "soteria.io/plan-name"`
  - [ ] 12.3 In DRExecution `PrepareForCreate` (`pkg/registry/drexecution/strategy.go` ~45-56), add server-side `soteria.io/plan-name` label stamping: `exec.Labels[soteriav1alpha1.PlanNameLabel] = exec.Spec.PlanName` (with nil map guard)
  - [ ] 12.4 Replace all Go string literal `"soteria.io/plan-name"` usages with `soteriav1alpha1.PlanNameLabel` constant
  - [ ] 12.5 Run `make manifests generate` after types.go constant addition (no struct changes, but ensures codegen is clean)

- [ ] Task 13: Documentation and tiered comments (AC: all)
  - [ ] 13.1 Update `pkg/admission/doc.go` — document the DRExecution-based concurrency guard pattern
  - [ ] 13.2 Add Tier 2 block comment on `verifyExclusiveExecution` explaining the admission + SERIAL INSERT + reconciler dedup three-layer safety model
  - [ ] 13.3 Update `pkg/apiserver/critical_fields.go` doc comment to reflect that DRExecution SERIAL creates replace the former ActiveExecution SERIAL on DRPlan
  - [ ] 13.4 Add Tier 3 domain-why comment on `SerialCreate` in store.go explaining why DRExecution needs cross-DC Paxos on insert

## Dev Notes

### Scope & Architecture

This story touches three layers: storage (SERIAL creates), admission (concurrency guard), and reconciler (ActiveExecution write removal + exclusivity check). The change fundamentally restructures how the concurrency invariant is maintained — from a DRPlan-stored pointer to a DRExecution-derived query.

**Three-layer concurrency model (replaces the single-pointer model):**

1. **Admission gate** (best-effort, catches 99.9% of cases): LIST DRExecutions for the plan, reject if any non-terminal one exists. Uses the cacher (watch cache) — may have brief eventual consistency lag.
2. **SERIAL INSERT** (cross-DC serialization): DRExecution `INSERT IF NOT EXISTS` uses `gocql.Serial` instead of `gocql.LocalSerial`, ensuring Paxos-level ordering across DCs. If two concurrent creates from different DCs both pass admission, both inserts are serialized at the Paxos leader.
3. **Reconciler exclusivity check** (true safety net): After an execution is created, the reconciler lists DRExecutions for the plan and verifies exclusivity. If a competing non-terminal execution exists (rare cross-DC race), the newer one self-fails. Uses `ScyllaRetry` backoff to wait for eventual consistency.

### Critical: ScyllaDB Storage Layer Changes

**Current `casInsert` (store.go ~874-891):**

```go
func (s *Store) casInsert(
    ctx context.Context, kc KeyComponents, data []byte, rv gocql.UUID,
) (bool, error) {
    // ...
    applied, err := s.session.Query(cql, ...).
        WithContext(ctx).
        SerialConsistency(gocql.LocalSerial).  // ← always LocalSerial
        MapScanCAS(result)
```

**New pattern:**

```go
func (s *Store) casInsert(
    ctx context.Context, kc KeyComponents, data []byte, rv gocql.UUID,
) (bool, error) {
    serialCL := gocql.LocalSerial
    if s.serialCreate {
        serialCL = gocql.Serial
    }
    // ...
    applied, err := s.session.Query(cql, ...).
        WithContext(ctx).
        SerialConsistency(serialCL).
        MapScanCAS(result)
```

The `serialCreate` flag is set per-resource via `StoreConfig` ← `ScyllaStoreFactory.SerialCreateResources` ← `DefaultSerialCreateResources()`.

**Why SERIAL on INSERT matters:** Even though `IF NOT EXISTS` is per-key (each DRExecution has a unique name), SERIAL ensures the insert participates in the Paxos quorum across all DCs. When combined with the reconciler exclusivity check (which reads with eventual consistency + retry), this guarantees that concurrent creates from different DCs are visible to each other within the `ScyllaRetry` window.

### Critical: Admission Plugin Changes

**Current flow (plugin.go ~127-131):**

```go
if plan.Status.ActiveExecution != "" {
    return admission.NewForbidden(a, fmt.Errorf(
        "DRPlan %q has active execution %q; concurrent executions not permitted",
        exec.Spec.PlanName, plan.Status.ActiveExecution))
}
```

**New flow:**

```go
if p.drexecutionStorage != nil {
    execList, err := p.drexecutionStorage.List(ctx, &metainternalversion.ListOptions{})
    if err != nil {
        return fmt.Errorf("listing DRExecutions for concurrency check: %w", err)
    }
    if list, ok := execList.(*soteriav1alpha1.DRExecutionList); ok {
        for i := range list.Items {
            if list.Items[i].Spec.PlanName == exec.Spec.PlanName &&
                list.Items[i].Status.Result == "" {
                return admission.NewForbidden(a, fmt.Errorf(
                    "DRPlan %q has active execution %q; concurrent executions not permitted",
                    exec.Spec.PlanName, list.Items[i].Name))
            }
        }
    }
}
```

**Why LIST all DRExecutions instead of filtering by label?** The `rest.Lister` interface accepts `metainternalversion.ListOptions` which supports label/field selectors. Using `soteria.io/plan-name=<plan>` label selector would be more efficient. However, the admission plugin receives `DRExecutionList` from the cacher which handles the selector. Use a label selector if the cacher supports it (it does — `genericregistry.Store` delegates to the cacher which maintains indexes). If performance is a concern with many executions, add the label selector.

**Recommended approach — use label selector:**

```go
opts := &metainternalversion.ListOptions{}
labelReq, _ := labels.NewRequirement(
    soteriav1alpha1.PlanNameLabel, selection.Equals, []string{exec.Spec.PlanName})
opts.LabelSelector = labels.NewSelector().Add(*labelReq)
execList, err := p.drexecutionStorage.List(ctx, opts)
```

The `soteria.io/plan-name` label is set by `PrepareForCreate` in the DRExecution registry strategy (via `TriggeredByAnnotation` stamping — verify this; if the label is set elsewhere, use that mechanism). Actually, the label is set by the Console UI when creating via `k8sCreate` (Story 7.1). Verify it's also set server-side in `PrepareForCreate` — if not, add it there (Task 2 subtask).

**IMPORTANT:** Check whether `soteria.io/plan-name` label is stamped server-side in `PrepareForCreate`. If it's only client-side (Console), the label selector won't work for API-created executions (e.g., `kubectl`). If missing, add label stamping to `PrepareForCreate`:

```go
exec.Labels[soteriav1alpha1.PlanNameLabel] = exec.Spec.PlanName
```

### Critical: Reconciler Changes — reconcileSetup

**Current `reconcileSetup` (~1288-1360):**

The function does:
1. Validates mode transition
2. Creates `planPatch` and sets `plan.Status.ActiveExecution` + `ActiveExecutionMode`
3. Patches plan status
4. Initializes execution's StartTime, conditions
5. Patches execution status
6. Yields with `RequeueAfter: 1ms`

After 10.1, steps 2-3 are removed entirely. The function simplifies to:
1. Validates mode transition
2. Initializes execution's StartTime, conditions
3. Patches execution status
4. Yields with `RequeueAfter: 1ms`

**Phase transition in `reconcileSetup`:** The current code does NOT change `plan.Status.Phase` in `reconcileSetup` — the phase only advances on execution completion. The `engine.Transition(plan.Status.Phase, exec.Spec.Mode)` call validates the transition is valid but doesn't persist the transient phase. This stays the same.

### Critical: Reconciler Changes — finishExecution (executor.go)

**Current success/partial path (~1034-1054):**

```go
planPatch := client.MergeFrom(plan.DeepCopy())
plan.Status.Phase = newPhase
plan.Status.ActiveExecution = ""          // REMOVE
plan.Status.ActiveExecutionMode = ""      // REMOVE
plan.Status.ActiveSite = ActiveSiteForPhase(...)
r.Client.Status().Patch(ctx, plan, planPatch)
```

After 10.1, remove the two `Active*` lines. The patch still applies Phase and ActiveSite.

**Current failure path (~1055-1065):**

```go
if plan.Status.ActiveExecution != "" {
    planPatch := client.MergeFrom(plan.DeepCopy())
    plan.Status.ActiveExecution = ""
    plan.Status.ActiveExecutionMode = ""
    r.Client.Status().Patch(ctx, plan, planPatch)
}
```

After 10.1, remove this entire block. On failure, the plan status is not patched (phase stays at current rest state — this is the self-healing property, now simplified).

### Critical: Reconciler Changes — failExecution

**Current plan clearing (~979-989):**

```go
if len(plan) > 0 && plan[0] != nil && plan[0].Status.ActiveExecution == exec.Name {
    planPatch := client.MergeFrom(plan[0].DeepCopy())
    plan[0].Status.ActiveExecution = ""
    plan[0].Status.ActiveExecutionMode = ""
    r.Status().Patch(ctx, plan[0], planPatch)
}
```

Remove this entire block. `failExecution` sets execution result and conditions — the plan is no longer touched.

### Critical: Reconciler Changes — reprotect completion

**Reprotect path (~687-722) follows the same pattern as `finishExecution`.** On success/partial: keep Phase + ActiveSite writes, remove `ActiveExecution`/`ActiveExecutionMode` writes. On failure: remove the "clear ActiveExecution" block entirely.

### Critical: verifyExclusiveExecution

Replaces `fetchPlanWithActiveExecCheck`. The old function retried `Get` on the DRPlan until `ActiveExecution == execName`. The new function lists DRExecutions for the plan and verifies exclusivity.

```go
func (r *DRExecutionReconciler) verifyExclusiveExecution(
    ctx context.Context, planName, execName string,
) error {
    logger := log.FromContext(ctx)
    var lastErr error
    err := retry.RetryOnConflict(engine.ScyllaRetry, func() error {
        var execList soteriav1alpha1.DRExecutionList
        if err := r.List(ctx, &execList,
            client.MatchingLabels{soteriav1alpha1.PlanNameLabel: planName}); err != nil {
            return err
        }
        for i := range execList.Items {
            e := &execList.Items[i]
            if e.Name != execName && e.Status.Result == "" {
                lastErr = fmt.Errorf(
                    "DRPlan %q has competing execution %q; execution %q cannot proceed",
                    planName, e.Name, execName)
                return nil  // stop retrying — we found a competitor
            }
        }
        // Verify our own execution is in the list (consistency check)
        for i := range execList.Items {
            if execList.Items[i].Name == execName {
                return nil  // exclusive — we are the only non-terminal execution
            }
        }
        // Our execution not yet visible — retry for consistency
        logger.V(1).Info("Execution not yet visible in list, retrying",
            "execution", execName)
        return &errors.StatusError{ErrStatus: metav1.Status{
            Reason:  metav1.StatusReasonConflict,
            Message: "execution not yet visible in list",
        }}
    })
    if lastErr != nil {
        return lastErr
    }
    if err != nil {
        return fmt.Errorf("execution %q for plan %q not visible after retries: %w",
            execName, planName, err)
    }
    return nil
}
```

**Call sites:** Both `reconcileReprotectResume` (~783) and `reconcileResume` (~877) replace `fetchPlanWithActiveExecCheck` with `verifyExclusiveExecution`. On error, call `failExecution` with reason `StaleExecution` (same as current behavior).

**Important difference:** `fetchPlanWithActiveExecCheck` returned the plan object (used by callers). `verifyExclusiveExecution` does not. The callers at ~783 and ~877 must continue to fetch the plan separately (they already do `r.Get` for the plan before calling — verify this at each call site).

### Critical: PlanNameLabel Stamping

**There is no `PlanNameLabel` Go constant.** The label `"soteria.io/plan-name"` is used as a string literal across Go and TypeScript. Define a new constant in `types.go`:

```go
const PlanNameLabel = "soteria.io/plan-name"
```

**Current stamping is controller-side, not server-side.** The label is currently set by:
1. `ensurePlanNameLabel` in `reconcileSetup` (~1254-1284 in reconciler.go) — patches the label during first reconcile
2. Console `useCreateDRExecution.ts` — sets it client-side on create

It is NOT set in `PrepareForCreate` (registry strategy). This means a kubectl-created DRExecution won't have the label until after its first reconcile cycle. For the admission concurrency guard to catch ALL active executions via label selector, the label MUST be stamped server-side in `PrepareForCreate`:

```go
func (drexecutionStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
    exec := obj.(*soteriav1alpha1.DRExecution)
    exec.Status = soteriav1alpha1.DRExecutionStatus{}
    exec.Generation = 1
    if exec.Labels == nil {
        exec.Labels = make(map[string]string)
    }
    exec.Labels[soteriav1alpha1.PlanNameLabel] = exec.Spec.PlanName
    // ... existing triggered-by annotation logic
}
```

**Keep `ensurePlanNameLabel` in `reconcileSetup` as a fallback** for executions created before this change. It's called at line ~1333 — do NOT remove it when removing the ActiveExecution patch block (lines ~1322-1331). These are separate operations within the function.

**Update all string literal `"soteria.io/plan-name"` usages in Go** to use the new `PlanNameLabel` constant — search with `rg '"soteria.io/plan-name"'` across Go files. Console TypeScript usages remain as string literals (no shared TS constants in this project).

### Critical: SERIAL Fallback

The existing `casUpdateWithConsistency` has a fallback to `LOCAL_ONE` when SERIAL fails (e.g., Paxos quorum unavailable). Apply the same fallback pattern to `casInsert` when `serialCreate` is true:

```go
if err != nil && s.serialCreate && shouldFallbackToLocal(err) {
    // Log warning and retry with LocalSerial
}
```

This ensures the system degrades gracefully during DC outages rather than blocking all DRExecution creates.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `CriticalFieldDetector` per-resource wiring | `pkg/apiserver/critical_fields.go:36-41` | Follow for `SerialCreateResources` |
| `SetDRPlanStorage` injection pattern | `pkg/admission/plugin.go:53-56` | Follow for `SetDRExecutionStorage` |
| `stubGetter` test helper | `pkg/admission/plugin_test.go:33-43` | Follow for `stubLister` |
| `ScyllaRetry` backoff pattern | `pkg/engine/executor.go:55-65` | Reuse in `verifyExclusiveExecution` |
| `fetchPlanWithActiveExecCheck` retry logic | `pkg/controller/drexecution/reconciler.go:1222-1251` | Follow retry pattern for `verifyExclusiveExecution` |
| `client.MatchingLabels` list filtering | Used across controller code | Reuse for label-filtered LIST |
| `casUpdateWithConsistency` SERIAL/fallback | `pkg/storage/scylladb/store.go:902-941` | Follow for SERIAL INSERT fallback |
| `DefaultCriticalFieldDetectors` registration | `pkg/apiserver/critical_fields.go:27-41` | Follow for `DefaultSerialCreateResources` |
| Integration admission test setup | `test/integration/apiserver/admission_test.go:145-175` | Follow for updated concurrency test |
| `admission.NewForbidden` error format | `pkg/admission/plugin.go:128-130` | Preserve error message format |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `PlanNameLabel` constant | ~2 lines |
| `pkg/storage/scylladb/store.go` | Add `serialCreate` field + conditional SERIAL in `casInsert` | ~15 lines |
| `pkg/apiserver/critical_fields.go` | Add `DefaultSerialCreateResources()`, update `detectDRPlanCriticalFields` | ~15 lines |
| `pkg/apiserver/critical_fields_test.go` | Update ActiveExecution test → expect false | ~5 lines |
| `pkg/apiserver/apiserver.go` | Add `SerialCreateResources` to factory, inject DRExecution storage into plugin | ~10 lines |
| `pkg/apiserver/options.go` | Wire `DefaultSerialCreateResources` | ~3 lines |
| `pkg/admission/plugin.go` | Add `drexecutionStorage`, rewrite concurrency guard | ~30 lines changed |
| `pkg/admission/plugin_test.go` | Add `stubLister`, rewrite concurrency tests | ~80 lines |
| `pkg/controller/drexecution/reconciler.go` | Remove ActiveExecution writes from `reconcileSetup`/`failExecution`/reprotect, add `verifyExclusiveExecution`, delete `fetchPlanWithActiveExecCheck` | ~100 lines changed |
| `pkg/engine/executor.go` | Remove ActiveExecution clearing from `finishExecution` | ~20 lines removed |
| `pkg/registry/drplan/strategy.go` | Remove ActiveExecution zeroing from `PrepareForCreate` | ~2 lines |
| `pkg/registry/drexecution/strategy.go` | Add `soteria.io/plan-name` label stamping in `PrepareForCreate` | ~5 lines |
| `pkg/admission/doc.go` | Update documentation | ~5 lines |
| `test/integration/apiserver/admission_test.go` | Update concurrency gate tests | ~30 lines |

### Execution Order

1. Task 1 (SERIAL storage) — foundation; needed by all layers
2. Task 7 (critical field detector) — remove ActiveExecution from DRPlan detector
3. Task 2 (inject DRExecution storage) — wiring for admission
4. Task 3 (rewrite admission guard) — depends on Task 2
5. Task 4 (remove reconcileSetup writes) — independent of admission changes
6. Task 5 (remove clearing from completion paths) — independent
7. Task 6 (verifyExclusiveExecution) — depends on Tasks 4-5 being clear about what's removed
8. Task 12 (DRPlan PrepareForCreate) — simple cleanup
9. Task 8 (legacy webhook) — minor
10. Tasks 9-11 (tests) — after all production changes compile
11. Task 13 (documentation) — final pass

### Previous Story Learnings (from 9.7 and 9.2)

- **Story 9.2 established the admission plugin pattern** — `SoteriaAdmissionPlugin` with injected `rest.Getter`, in-process admission chain via `NewChainHandler`. Story 10.1 extends this by injecting a `rest.Lister` for DRExecution cross-object queries.
- **Story 9.2 integration tests** use a full aggregated API server test harness (`test/integration/apiserver/suite_test.go`). Follow the same setup for updated concurrency tests.
- **`make manifests generate` is NOT needed** for this story — no types.go changes (field removal is Story 10.4).
- **PlanNameLabel constant:** Verify the constant exists in types.go. From Story 5.4, the label `soteria.io/plan-name` is used for DRExecution field selectors and history queries. The Console sets it client-side. Ensure it's also set server-side in `PrepareForCreate`.
- **ScyllaRetry backoff** (200ms base, 8 steps, ~50s window) is the standard for all eventual consistency handling — use it in `verifyExclusiveExecution`.
- **Error message backward compat:** Keep the admission error message "DRPlan \<name\> has active execution \<exec-name\>; concurrent executions not permitted" — external tooling or scripts may parse this format.

### Project Structure Notes

- Admission plugin: `pkg/admission/plugin.go` + `plugin_test.go`
- Legacy webhook (unused for soteria.io): `pkg/admission/drexecution_validator.go`
- API server wiring: `pkg/apiserver/apiserver.go`, `options.go`
- Critical field detectors: `pkg/apiserver/critical_fields.go` + `critical_fields_test.go`
- ScyllaDB storage: `pkg/storage/scylladb/store.go`
- DRExecution reconciler: `pkg/controller/drexecution/reconciler.go`
- Wave executor: `pkg/engine/executor.go`
- DRPlan registry strategy: `pkg/registry/drplan/strategy.go`
- DRExecution registry strategy: `pkg/registry/drexecution/strategy.go`
- Integration tests: `test/integration/apiserver/admission_test.go`
- Auto-generated files (DO NOT EDIT): `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`

### References

- [Source: pkg/admission/plugin.go] — Current admission plugin with ActiveExecution concurrency guard
- [Source: pkg/admission/plugin_test.go] — Admission concurrency tests with stubGetter pattern
- [Source: pkg/apiserver/apiserver.go#L84-L159] — API server wiring, DRPlan storage injection into plugin
- [Source: pkg/apiserver/critical_fields.go#L36-58] — CriticalFieldDetectors including ActiveExecution
- [Source: pkg/apiserver/critical_fields_test.go] — Critical field detector tests
- [Source: pkg/apiserver/options.go] — ScyllaStoreFactory construction
- [Source: pkg/storage/scylladb/store.go#L64-70] — CriticalFieldDetector type definition
- [Source: pkg/storage/scylladb/store.go#L874-891] — casInsert with LocalSerial
- [Source: pkg/storage/scylladb/store.go#L902-941] — casUpdateWithConsistency with SERIAL fallback
- [Source: pkg/controller/drexecution/reconciler.go#L1222-1251] — fetchPlanWithActiveExecCheck
- [Source: pkg/controller/drexecution/reconciler.go#L1288-1360] — reconcileSetup with ActiveExecution write
- [Source: pkg/controller/drexecution/reconciler.go#L979-989] — failExecution ActiveExecution clearing
- [Source: pkg/controller/drexecution/reconciler.go#L687-722] — reprotect completion ActiveExecution clearing
- [Source: pkg/engine/executor.go#L1032-1065] — finishExecution ActiveExecution clearing
- [Source: pkg/engine/executor.go#L55-65] — ScyllaRetry backoff definition
- [Source: pkg/registry/drplan/strategy.go#L47-54] — PrepareForCreate ActiveExecution zeroing
- [Source: pkg/registry/drexecution/strategy.go#L45-56] — DRExecution PrepareForCreate
- [Source: pkg/registry/drexecution/storage.go#L36-64] — AuditProtectedREST wrapping genericregistry.Store
- [Source: test/integration/apiserver/admission_test.go#L145-175] — Integration concurrency gate test
- [Source: _bmad-output/planning-artifacts/epics.md#Story-10.1] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, ScyllaRetry, SERIAL patterns

### Review Findings

- [x] [Review][Decision] Label-only concurrency lookup can miss active executions — Both the admission gate and `verifyExclusiveExecution` rely on `soteria.io/plan-name` label queries, but this story only guarantees server-side label stamping for newly created executions. Pre-existing non-terminal executions created before this change remain invisible unless they re-enter `reconcileSetup`, because `ensurePlanNameLabel()` is only called there. In addition, Scylla label-index sync on create is still best-effort (`pkg/storage/scylladb/store.go`), so an execution can exist in `kv_store` without becoming selector-visible in `kv_store_labels`. That leaves both layer 1 and layer 3 blind to some active executions. Evidence: `pkg/admission/plugin.go`, `pkg/controller/drexecution/reconciler.go`, `pkg/storage/scylladb/store.go`. **Resolved (Option 1)**: Moved `ensurePlanNameLabel()` from `reconcileSetup` to the top of the main `Reconcile()` loop so it backfills the label on every reconcile cycle for pre-existing executions. New executions get the label via `PrepareForCreate`. The informer/cacher paths used by admission and reconciler queries filter by object metadata labels (not the ScyllaDB label index), so best-effort `syncLabels` is not a concern for these paths.
- [x] [Review][Patch] `verifyExclusiveExecution` treats a stale empty LIST as exclusive instead of retrying — **Fixed**: Added self-visibility check; returns `Conflict` error (triggering `ScyllaRetry` backoff) when the calling execution is not visible in the label-filtered list.
- [x] [Review][Patch] Nil `drexecutionStorage` silently disables the admission concurrency gate — **Fixed**: Added `klog.Warningf` when `drexecutionStorage` is nil.

## Dev Agent Record

### Agent Model Used

Claude Opus 4 (claude-opus-4-20250514)

### Debug Log References

- [Story 10.1 dev session](70e7605b-4efa-4a59-963a-2f684f517a74) — full implementation session

### Completion Notes List

- AC1: Admission concurrency guard now lists DRExecutions via `rest.Lister` with `soteria.io/plan-name` label selector; rejects CREATE when any non-terminal execution exists. Error message preserves backward-compatible format.
- AC2: `casInsert` in `store.go` uses `gocql.Serial` when `serialCreate` is true (configured per-resource via `SerialCreateResources`). Includes fallback to `LocalSerial` on infrastructure errors via `shouldFallbackToLocal`.
- AC3: `reconcileSetup` no longer patches `plan.Status.ActiveExecution` or `ActiveExecutionMode`. The plan status patch block was removed entirely.
- AC4: `finishExecution` (executor.go), `failExecution`, and reprotect completion path no longer clear `ActiveExecution`/`ActiveExecutionMode`. Phase/ActiveSite writes continue unchanged.
- AC5: `verifyExclusiveExecution` lists DRExecutions with `ScyllaRetry` backoff, self-fails if competing non-terminal execution found. Replaces all `fetchPlanWithActiveExecCheck` call sites.
- AC6: `detectDRPlanCriticalFields` only compares `Phase` and `ActiveSite`. `DefaultSerialCreateResources` configures DRExecution for SERIAL creates.
- AC7: All unit and integration tests pass with zero regressions. 4 new admission plugin tests (concurrency guard denied, allowed no active, allowed terminal only, nil storage proceeds). Legacy webhook test updated. Reconciler/executor tests updated to no longer assert `ActiveExecution` writes. Integration test rewritten to create real DRExecution instead of setting plan status.
- `mapVMToDRExecution` updated to list DRExecutions by plan-name label instead of reading `plan.Status.ActiveExecution`.
- `ensurePlanNameLabel` updated to use `PlanNameLabel` constant.
- `PlanNameLabel` stamped server-side in `PrepareForCreate` (was controller-side only).
- Legacy `DRExecutionValidator` webhook concurrency check removed with comment noting guard moved to in-process plugin.
- Known intermediate state: consumers in Stories 10.2/10.3 (table convertor, preflight, health polling gate, DRPlan reconciler) still read `ActiveExecution` and will see empty values until those stories migrate them.

### File List

- `cmd/soteria/main.go` — Wired `DefaultSerialCreateResources()` into `ScyllaStoreFactory`
- `pkg/admission/doc.go` — Documented three-layer concurrency model
- `pkg/admission/drexecution_validator.go` — Removed `ActiveExecution` check from legacy webhook
- `pkg/admission/drexecution_validator_test.go` — Updated legacy test to expect allowed
- `pkg/admission/plugin.go` — Added `drexecutionStorage rest.Lister`, `SetDRExecutionStorage`, `checkNoConcurrentExecution`
- `pkg/admission/plugin_test.go` — Added `stubLister`, rewrote concurrency test, added 3 new tests, injected lister into all DRExecution CREATE tests
- `pkg/apis/soteria.io/v1alpha1/types.go` — Added `PlanNameLabel` constant
- `pkg/apiserver/apiserver.go` — Added `SerialCreateResources` to factory, injected DRExecution storage into admission plugin
- `pkg/apiserver/critical_fields.go` — Added `DefaultSerialCreateResources()`, removed `ActiveExecution` from `detectDRPlanCriticalFields`
- `pkg/apiserver/critical_fields_test.go` — Updated `ActiveExecution` test expectation to `false`
- `pkg/controller/drexecution/reconciler.go` — Removed `ActiveExecution` writes from `reconcileSetup`/`failExecution`/reprotect, replaced `fetchPlanWithActiveExecCheck` with `verifyExclusiveExecution` + `fetchPlan`, updated `mapVMToDRExecution` and `ensurePlanNameLabel`
- `pkg/controller/drexecution/reconciler_test.go` — Updated `ActiveExecution` assertions, rewrote `mapVMToDRExecution` test
- `pkg/engine/executor.go` — Removed `ActiveExecution` clearing from `finishExecution`
- `pkg/engine/executor_test.go` — Removed `ActiveExecution` assertions
- `pkg/registry/drexecution/strategy.go` — Server-side `soteria.io/plan-name` label stamping in `PrepareForCreate`
- `pkg/storage/scylladb/store.go` — Added `serialCreate` flag, conditional `gocql.Serial` in `casInsert` with fallback
- `test/integration/apiserver/admission_test.go` — Rewrote concurrency gate test, added `AllowedAfterCompletion` test
- `test/integration/apiserver/suite_test.go` — Wired `SerialCreateResources` into test harness
