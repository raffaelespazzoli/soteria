# Story 10.2: Derived Active Execution for Table Convertor & Preflight

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want `kubectl get drplans` and the preflight report to derive active execution state from DRExecution resources,
So that the DRPlan status no longer needs `ActiveExecution` or `ActiveExecutionMode` fields for these consumers.

## Background

After Story 10.1, the DRExecution reconciler no longer writes `ActiveExecution` / `ActiveExecutionMode` on the DRPlan. These fields remain on the `DRPlanStatus` struct (removal is Story 10.4) but are always empty for new executions. Two consumers still read them:

1. **DRPlan table convertor** (`pkg/registry/drplan/strategy.go`) — reads `plan.Status.ActiveExecutionMode` to compute `EffectivePhase` and `plan.Status.ActiveExecution` for the "Active Execution" column.
2. **Preflight composition** (`internal/preflight/checks.go`) — reads `plan.Status.ActiveExecution` to populate `report.ActiveExecution` and emit an "execution is active" warning.

Both consumers must be migrated to derive active execution from DRExecution resources. The table convertor runs inside the aggregated API server (has access to REST storage) and the preflight composition runs inside the DRPlan reconciler (has access to `client.Client`).

**Scope boundary:** The replication health polling gate (`plan.Status.ActiveExecution == ""` at `reconciler.go:390`) and VM watch routing (`mapVMToDRExecution`) are Story 10.3 scope — do NOT touch them here.

## Acceptance Criteria

1. **AC1 — Table convertor derives effective phase from DRExecution query:** The DRPlan table convertor queries the DRExecution cacher for a non-terminal execution with `spec.planName == plan.Name`. If one exists, `EffectivePhase(plan.Status.Phase, exec.Spec.Mode)` is computed. If none exists, the rest phase is shown directly. The "Active Execution" column shows the execution name from the query (or empty).

2. **AC2 — Bulk LIST performance for table convertor:** For `kubectl get drplans` (LIST of N plans), a single LIST of all non-terminal DRExecutions is performed and indexed by `spec.planName` — not N individual queries. Performance is O(plans + executions), not O(plans * executions).

3. **AC3 — Preflight report derives active execution from DRExecution query:** `ComposeReport` populates `report.ActiveExecution` from `CompositionInput.ActiveExecution` (a new field) instead of from `plan.Status.ActiveExecution`. The "execution is active" warning is generated from this field.

4. **AC4 — DRPlan reconciler queries DRExecutions for preflight:** `composePreflightReport` queries DRExecutions for the plan via `client.MatchingLabels{soteriav1alpha1.PlanNameLabel: plan.Name}`, finds the non-terminal execution (if any), and passes its name to `CompositionInput.ActiveExecution`.

5. **AC5 — PreflightReport.ActiveExecution field unchanged:** The `ActiveExecution` field continues to exist on `PreflightReport` — it's a report snapshot, not a plan status field. Only the data source changes.

6. **AC6 — Tests:** Table convertor tests verify effective phase derivation from DRExecution fixtures (not plan status). Preflight tests verify active execution warning from `CompositionInput.ActiveExecution`. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Add DRExecution lister to DRPlanTableConvertor (AC: #1, #2)
  - [ ] 1.1 Change `DRPlanTableConvertor` from zero-field struct to `DRPlanTableConvertor struct { drexecutionLister rest.Lister }` in `pkg/registry/drplan/strategy.go`
  - [ ] 1.2 Add `SetDRExecutionStorage(s rest.Lister)` method on `*DRPlanTableConvertor`
  - [ ] 1.3 Change `ConvertToTable` to pointer receiver `(c *DRPlanTableConvertor)`
  - [ ] 1.4 In `NewREST` (`pkg/registry/drplan/storage.go`), create `tc := &DRPlanTableConvertor{}` and set `TableConvertor: tc`. Return `tc` as a third value from `NewREST` (update signature to `(*genericregistry.Store, *StatusREST, *DRPlanTableConvertor, error)`)
  - [ ] 1.5 Update `StatusREST` — its `store` shares the parent's `TableConvertor`, so no separate wiring needed

- [ ] Task 2: Implement derived effective phase and active execution in table convertor (AC: #1, #2)
  - [ ] 2.1 Add `activeExecIndex` type: `map[string]*soteriav1alpha1.DRExecution` (keyed by `spec.planName`)
  - [ ] 2.2 Add `(c *DRPlanTableConvertor) buildActiveExecIndex(ctx context.Context) activeExecIndex` helper: calls `c.drexecutionLister.List(ctx, &metainternalversion.ListOptions{})`, iterates results, builds index of non-terminal executions (`status.result == ""`) keyed by `spec.planName`. Returns empty map on nil lister or error (log warning, degrade gracefully)
  - [ ] 2.3 In `ConvertToTable`, call `buildActiveExecIndex` once before row iteration. Pass the index to `planToRow`
  - [ ] 2.4 Update `planToRow` signature to accept `activeExecIndex`. Look up `index[plan.Name]` — if found, use `engine.EffectivePhase(plan.Status.Phase, exec.Spec.Mode)` and `exec.Name` for the cells. If not found, use `plan.Status.Phase` (rest phase) and empty string
  - [ ] 2.5 **Graceful degradation:** If `c.drexecutionLister` is nil (e.g., during startup or tests), fall back to the current behavior reading from plan status — this ensures zero disruption if the lister isn't wired yet

- [ ] Task 3: Wire DRExecution storage into DRPlan table convertor in apiserver.go (AC: #1)
  - [ ] 3.1 Update `drplanregistry.NewREST` call site in `apiserver.go` to capture the returned `*DRPlanTableConvertor`
  - [ ] 3.2 After DRExecution `NewREST`, call `tc.SetDRExecutionStorage(drexecStore)` — the `AuditProtectedREST` wraps a `*genericregistry.Store` which implements `rest.Lister`
  - [ ] 3.3 Add import for `rest "k8s.io/apiserver/pkg/registry/rest"` if not already present

- [ ] Task 4: Add ActiveExecution field to CompositionInput (AC: #3, #5)
  - [ ] 4.1 Add `ActiveExecution string` field to `CompositionInput` struct in `internal/preflight/checks.go`
  - [ ] 4.2 In `ComposeReport`, replace `input.Plan.Status.ActiveExecution` (line 68) with `input.ActiveExecution`
  - [ ] 4.3 In `collectWarnings`, replace `input.Plan.Status.ActiveExecution` (line 269) with `input.ActiveExecution`

- [ ] Task 5: DRPlan reconciler queries DRExecutions for preflight (AC: #4)
  - [ ] 5.1 Add RBAC marker to DRPlan reconciler: `// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=list`
  - [ ] 5.2 In `composePreflightReport`, before building `CompositionInput`, query for the active execution:
    ```go
    var activeExecName string
    var execList soteriav1alpha1.DRExecutionList
    if err := r.List(ctx, &execList,
        client.MatchingLabels{soteriav1alpha1.PlanNameLabel: plan.Name}); err != nil {
        logger.Error(err, "Failed to list DRExecutions for preflight")
    } else {
        for i := range execList.Items {
            if execList.Items[i].Status.Result == "" {
                activeExecName = execList.Items[i].Name
                break
            }
        }
    }
    ```
  - [ ] 5.3 Set `ActiveExecution: activeExecName` in the `CompositionInput` struct
  - [ ] 5.4 Run `make manifests` after adding the RBAC marker (regenerates `role.yaml`)

- [ ] Task 6: Update table convertor tests (AC: #6)
  - [ ] 6.1 Add `stubExecLister` test helper implementing `rest.Lister` in `pkg/registry/drplan/strategy_test.go` — returns a configurable `DRExecutionList`. Follow the `stubLister` pattern from `pkg/admission/plugin_test.go` (added in Story 10.1)
  - [ ] 6.2 Add `TestConvertToTable_DeriveEffectivePhase_FromExecution`: set up a plan in SteadyState + a non-terminal DRExecution with mode `planned_migration` in the stub lister. Verify "Effective Phase" cell = `FailingOver` and "Active Execution" cell = execution name
  - [ ] 6.3 Add `TestConvertToTable_NoActiveExecution_ShowsRestPhase`: stub lister returns empty list. Verify "Effective Phase" = rest phase and "Active Execution" = ""
  - [ ] 6.4 Add `TestConvertToTable_TerminalExecution_Ignored`: stub lister returns a DRExecution with `Result: Succeeded`. Verify treated as idle (rest phase shown)
  - [ ] 6.5 Add `TestConvertToTable_NilLister_FallsBackToPlanStatus`: convertor with nil lister. Verify graceful degradation — uses `plan.Status.ActiveExecutionMode` for effective phase and `plan.Status.ActiveExecution` for the column (backward compat during startup)
  - [ ] 6.6 Add `TestConvertToTable_BulkList_SingleQuery`: create convertor with a counting stub lister, convert a `DRPlanList` with 3 plans. Verify the lister's `List` was called exactly once (not per plan)
  - [ ] 6.7 Update `TestPrepareForCreate_InitializesActiveExecution` — this test still passes because the fields still exist on the struct (removal is 10.4). No change needed, just verify

- [ ] Task 7: Update preflight tests (AC: #6)
  - [ ] 7.1 Update `TestComposeReport_ActiveExecution`: set `ActiveExecution: "exec-failover-1"` on `CompositionInput` instead of on `Plan.Status.ActiveExecution`. Keep plan's `ActiveExecution` empty to prove the report reads from the new field
  - [ ] 7.2 Update `TestComposeReport_NoActiveExecution_NoWarning`: ensure `CompositionInput.ActiveExecution` is empty. Plan status may have a stale value — verify it's ignored
  - [ ] 7.3 Add `TestComposeReport_PlanStatusActiveExecution_Ignored`: set `plan.Status.ActiveExecution = "stale"` but `input.ActiveExecution = ""`. Verify `report.ActiveExecution` is empty and no warning emitted — confirms plan status is no longer read

- [ ] Task 8: Update DRPlan reconciler integration tests (AC: #6)
  - [ ] 8.1 Verify existing integration tests that exercise the reconcile → preflight pipeline still pass. If any test previously set `plan.Status.ActiveExecution` to test the warning, update it to create a real non-terminal DRExecution for the plan instead
  - [ ] 8.2 Run `make test` — all unit and integration tests pass with zero regressions

- [ ] Task 9: Documentation and tiered comments (AC: all)
  - [ ] 9.1 Update `DRPlanTableConvertor` godoc to explain the DRExecution-derived pattern
  - [ ] 9.2 Add Tier 3 domain-why comment on `buildActiveExecIndex` explaining why a bulk LIST + client-side index is used instead of per-plan queries (O(plans+executions) vs O(plans*executions))
  - [ ] 9.3 Update `CompositionInput.ActiveExecution` field godoc explaining it's derived from DRExecution query, not from plan status
  - [ ] 9.4 Update `composePreflightReport` comment to note the DRExecution query for active execution derivation

## Dev Notes

### Scope & Architecture

This story touches two layers: the aggregated API server (table convertor) and the DRPlan reconciler (preflight). Both are read-side consumers of "active execution" state — they need to know if an execution is running but do not write to it.

**After Story 10.1:** `plan.Status.ActiveExecution` and `plan.Status.ActiveExecutionMode` are never written by new code paths. They remain on the struct with zero values for new executions. This story migrates the two remaining read-side consumers so they derive the information from DRExecution resources.

**NOT in scope:**
- Replication health polling gate (`reconciler.go:390`) — Story 10.3
- VM watch routing (`mapVMToDRExecution`) — Story 10.3
- Removing `ActiveExecution`/`ActiveExecutionMode` from `DRPlanStatus` struct — Story 10.4
- Console UI derivation — Story 10.5

### Critical: Table Convertor Design

**Current code (`pkg/registry/drplan/strategy.go:118-165`):**

```go
type DRPlanTableConvertor struct{}

func (DRPlanTableConvertor) ConvertToTable(
    ctx context.Context, object runtime.Object, tableOptions runtime.Object,
) (*metav1.Table, error) {
    // ...per row: plan.Status.ActiveExecutionMode, plan.Status.ActiveExecution
}
```

**New pattern:**

```go
type DRPlanTableConvertor struct {
    drexecutionLister rest.Lister
}

func (c *DRPlanTableConvertor) SetDRExecutionStorage(s rest.Lister) {
    c.drexecutionLister = s
}

func (c *DRPlanTableConvertor) ConvertToTable(
    ctx context.Context, object runtime.Object, tableOptions runtime.Object,
) (*metav1.Table, error) {
    index := c.buildActiveExecIndex(ctx)
    // ...per row: look up index[plan.Name]
}
```

**Bulk LIST + client-side index:** `ConvertToTable` is called for both single GET and LIST. For a LIST of N plans, doing N individual GETs would be O(N * executions). Instead, a single LIST of ALL DRExecutions is performed from the cacher (in-memory watch cache — fast), filtered client-side for non-terminal results, and indexed by `spec.planName`. Each row lookup is O(1).

**Why not label selector on LIST?** The `rest.Lister.List` accepts `metainternalversion.ListOptions` with label/field selectors. Since we need ALL active executions (not per-plan), an unfiltered LIST is correct. The cacher serves it from memory.

**Graceful degradation:** If `drexecutionLister` is nil (startup race or tests without wiring), fall back to reading `plan.Status` directly. This preserves backward compat during the transition:

```go
func (c *DRPlanTableConvertor) buildActiveExecIndex(ctx context.Context) activeExecIndex {
    if c == nil || c.drexecutionLister == nil {
        return nil
    }
    // ...LIST and index
}

func planToRow(plan *soteriav1alpha1.DRPlan, index activeExecIndex) metav1.TableRow {
    var effectivePhase string
    var activeExecName string
    if exec, ok := index[plan.Name]; ok {
        effectivePhase = engine.EffectivePhase(plan.Status.Phase, exec.Spec.Mode)
        activeExecName = exec.Name
    } else if index == nil {
        // Fallback: lister not wired, read from plan status
        effectivePhase = engine.EffectivePhase(plan.Status.Phase, plan.Status.ActiveExecutionMode)
        activeExecName = plan.Status.ActiveExecution
    } else {
        // Index exists but no active execution for this plan — idle
        effectivePhase = plan.Status.Phase
    }
    // ...build row
}
```

### Critical: Storage Wiring in apiserver.go

**Current wiring (`pkg/apiserver/apiserver.go:124-138`):**

```go
drplanStore, drplanStatusStore, err := drplanregistry.NewREST(soteriainstall.Scheme, optsGetter)
// ...
drexecStore, drexecStatusStore, err := drexecutionregistry.NewREST(soteriainstall.Scheme, optsGetter)
```

**New wiring (add table convertor injection):**

```go
drplanStore, drplanStatusStore, drplanTC, err := drplanregistry.NewREST(soteriainstall.Scheme, optsGetter)
// ...
drexecStore, drexecStatusStore, err := drexecutionregistry.NewREST(soteriainstall.Scheme, optsGetter)
// ...
// Inject DRExecution storage into DRPlan table convertor for derived
// effective phase and active execution columns.
if drplanTC != nil {
    drplanTC.SetDRExecutionStorage(drexecStore)
}
```

**`AuditProtectedREST` implements `rest.Lister`:** The `drexecStore` returned by `drexecutionregistry.NewREST` is `*AuditProtectedREST` which embeds `*genericregistry.Store`. The `genericregistry.Store` implements `rest.Lister` via its `List` method (which delegates to the cacher). So `drexecStore` can be passed directly — no extra unwrapping needed.

**Verify `AuditProtectedREST` satisfies `rest.Lister`:** `*genericregistry.Store` has `List(ctx, options)` which returns `runtime.Object, error`. Since `AuditProtectedREST` embeds `*genericregistry.Store`, it inherits `List`. The `rest.Lister` interface requires `List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error)` — matches.

### Critical: NewREST Signature Change

**Current (`pkg/registry/drplan/storage.go:33-35`):**

```go
func NewREST(
    scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter,
) (*genericregistry.Store, *StatusREST, error) {
```

**New:**

```go
func NewREST(
    scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter,
) (*genericregistry.Store, *StatusREST, *DRPlanTableConvertor, error) {
```

Inside:
```go
tc := &DRPlanTableConvertor{}
store := &genericregistry.Store{
    // ...
    TableConvertor: tc,
}
// ...
return store, &StatusREST{store: &statusStore}, tc, nil
```

### Critical: Preflight CompositionInput Change

**Current (`internal/preflight/checks.go:42-54`):**

```go
type CompositionInput struct {
    Plan                   *soteriav1alpha1.DRPlan
    DiscoveryResult        *engine.DiscoveryResult
    ConsistencyResult      *engine.ConsistencyResult
    ChunkResult            *engine.ChunkResult
    StorageBackends        map[string]string
    Waves                  []soteriav1alpha1.WaveInfo
    LocalSite              string
    PrimarySiteDiscovery   *soteriav1alpha1.SiteDiscovery
    SecondarySiteDiscovery *soteriav1alpha1.SiteDiscovery
}
```

**New (add one field):**

```go
type CompositionInput struct {
    Plan                   *soteriav1alpha1.DRPlan
    DiscoveryResult        *engine.DiscoveryResult
    ConsistencyResult      *engine.ConsistencyResult
    ChunkResult            *engine.ChunkResult
    StorageBackends        map[string]string
    Waves                  []soteriav1alpha1.WaveInfo
    LocalSite              string
    PrimarySiteDiscovery   *soteriav1alpha1.SiteDiscovery
    SecondarySiteDiscovery *soteriav1alpha1.SiteDiscovery
    // ActiveExecution is the name of the currently running DRExecution for
    // this plan, derived from a DRExecution list query. Empty when idle.
    ActiveExecution string
}
```

**In `ComposeReport` (line 68):** Change `report.ActiveExecution = input.Plan.Status.ActiveExecution` to `report.ActiveExecution = input.ActiveExecution`.

**In `collectWarnings` (line 269):** Change `input.Plan.Status.ActiveExecution` to `input.ActiveExecution`.

### Critical: DRPlan Reconciler Query

**In `composePreflightReport` (`pkg/controller/drplan/reconciler.go:1212-1253`):**

The reconciler has `client.Client` (embedded) and can LIST DRExecutions. The query uses `client.MatchingLabels` with the `PlanNameLabel` constant (added in Story 10.1, Task 12.2).

**Important — `PlanNameLabel` dependency on 10.1:** Story 10.1 Task 12 adds `const PlanNameLabel = "soteria.io/plan-name"` to `types.go` and stamps the label server-side in DRExecution `PrepareForCreate`. If 10.1 is not yet complete when implementing 10.2, use the string literal `"soteria.io/plan-name"` temporarily and convert to the constant once available.

**RBAC:** The DRPlan reconciler currently does not have RBAC to list DRExecutions. Add the marker:
```go
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=list
```

Then run `make manifests` to regenerate `config/rbac/role.yaml`.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `SetDRPlanStorage` injection pattern | `pkg/admission/plugin.go:53-56` | Follow for `SetDRExecutionStorage` on table convertor |
| `stubGetter` / `stubLister` test helpers | `pkg/admission/plugin_test.go` | Follow for `stubExecLister` in strategy_test.go |
| `rest.Lister` interface | `k8s.io/apiserver/pkg/registry/rest` | Type for DRExecution storage injection |
| `metainternalversion.ListOptions` | Used in admission plugin LIST | Same for table convertor LIST |
| `client.MatchingLabels` for label-filtered LIST | Used across controller code | Reuse in reconciler DRExecution query |
| `CompositionInput` extension pattern | Story 9.4 (added `Waves`/`LocalSite`), 9.7 (added SiteDiscovery) | Follow for `ActiveExecution` field |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/registry/drplan/strategy.go` | Add lister field to `DRPlanTableConvertor`, rewrite `planToRow` with index lookup | ~40 lines changed |
| `pkg/registry/drplan/storage.go` | Update `NewREST` signature to return `*DRPlanTableConvertor` | ~5 lines |
| `pkg/registry/drplan/strategy_test.go` | Add `stubExecLister`, add 5 new table convertor tests | ~120 lines |
| `pkg/apiserver/apiserver.go` | Update `NewREST` call site, inject DRExecution storage into table convertor | ~5 lines |
| `internal/preflight/checks.go` | Add `ActiveExecution` to `CompositionInput`, change 2 read sites | ~5 lines |
| `internal/preflight/checks_test.go` | Update 2 tests, add 1 new test | ~20 lines |
| `pkg/controller/drplan/reconciler.go` | Add RBAC marker, add DRExecution query in `composePreflightReport` | ~15 lines |
| `config/rbac/role.yaml` | Auto-generated after `make manifests` | N/A |

### Execution Order

1. Task 1 (table convertor struct + `NewREST` signature) — foundation
2. Task 2 (implement derived logic) — depends on Task 1
3. Task 3 (wire in apiserver.go) — depends on Tasks 1-2
4. Task 4 (CompositionInput change) — independent of Tasks 1-3
5. Task 5 (reconciler DRExecution query) — depends on Task 4
6. Tasks 6-7 (unit tests) — after production code compiles
7. Task 8 (integration tests) — after unit tests pass
8. Task 9 (documentation) — final pass

### Previous Story Learnings (from 10.1)

- **Story 10.1 adds `PlanNameLabel` constant** (`"soteria.io/plan-name"`) to `types.go` and stamps the label server-side in `PrepareForCreate`. Use this constant for the reconciler's `client.MatchingLabels` query. If 10.1 isn't merged yet, use the string literal and update later.
- **Story 10.1 adds `stubLister` to `pkg/admission/plugin_test.go`** — reuse the same pattern for `stubExecLister` in the strategy tests.
- **`ensurePlanNameLabel` in reconcileSetup** backfills the label for pre-10.1 executions — the label selector query works for both old and new executions.
- **`make manifests generate` is needed** for the RBAC marker addition. No types.go struct changes, so only `make manifests` is strictly needed, but running both is safe.
- **`AuditProtectedREST` embeds `*genericregistry.Store`** — it inherits `List()` and satisfies `rest.Lister` without any wrapper.

### Project Structure Notes

- DRPlan table convertor: `pkg/registry/drplan/strategy.go` (lines 118-165)
- DRPlan REST storage: `pkg/registry/drplan/storage.go`
- DRPlan strategy tests: `pkg/registry/drplan/strategy_test.go`
- DRExecution REST storage: `pkg/registry/drexecution/storage.go` (returns `AuditProtectedREST`)
- Preflight composition: `internal/preflight/checks.go` (lines 42-54 CompositionInput, 59-132 ComposeReport, 266-301 collectWarnings)
- Preflight tests: `internal/preflight/checks_test.go`
- DRPlan reconciler: `pkg/controller/drplan/reconciler.go` (lines 1212-1253 composePreflightReport)
- API server wiring: `pkg/apiserver/apiserver.go` (lines 120-145 storage creation)
- State machine: `pkg/engine/statemachine.go` (EffectivePhase function)
- Types: `pkg/apis/soteria.io/v1alpha1/types.go` (DRPlanStatus lines 96-141, PreflightReport lines 143-175)
- Auto-generated files (DO NOT EDIT): `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`, `config/rbac/role.yaml`

### References

- [Source: pkg/registry/drplan/strategy.go#L118-L165] — Current DRPlanTableConvertor with ActiveExecution reads
- [Source: pkg/registry/drplan/storage.go#L33-L60] — NewREST creating store with TableConvertor
- [Source: pkg/registry/drplan/strategy_test.go#L108-L125] — PrepareForCreate ActiveExecution test
- [Source: internal/preflight/checks.go#L42-L54] — CompositionInput struct
- [Source: internal/preflight/checks.go#L59-L69] — ComposeReport reading plan.Status.ActiveExecution
- [Source: internal/preflight/checks.go#L266-L272] — collectWarnings reading plan.Status.ActiveExecution
- [Source: internal/preflight/checks_test.go#L985-L1010] — ActiveExecution test reading from plan status
- [Source: pkg/controller/drplan/reconciler.go#L107-L126] — DRPlanReconciler struct with client.Client
- [Source: pkg/controller/drplan/reconciler.go#L1212-L1253] — composePreflightReport building CompositionInput
- [Source: pkg/controller/drplan/reconciler.go#L388-L394] — Health polling gate (Story 10.3 scope, DO NOT touch)
- [Source: pkg/apiserver/apiserver.go#L120-L145] — Storage creation and admission plugin injection pattern
- [Source: pkg/registry/drexecution/storage.go#L36-L65] — AuditProtectedREST embedding genericregistry.Store
- [Source: pkg/engine/statemachine.go#L118-L132] — EffectivePhase function signature
- [Source: pkg/admission/plugin.go#L53-L56] — SetDRPlanStorage injection pattern (template for SetDRExecutionStorage)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-10.2] — Epic acceptance criteria
- [Source: _bmad-output/project-context.md] — Critical rules, ScyllaRetry, tiered comments

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
