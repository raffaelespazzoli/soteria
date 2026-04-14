# Story 2b.1: Label Convention — API, Discovery & Controller Refactoring

Status: done

## Story

As a platform engineer,
I want VMs to declare their DRPlan membership via the `soteria.io/drplan` label,
So that plan membership is explicit, unambiguous, and structurally limited to one plan per VM.

## Acceptance Criteria

1. **AC1 — Remove `VMSelector` from `DRPlanSpec`:** The `VMSelector metav1.LabelSelector` field is removed from `DRPlanSpec` in `pkg/apis/soteria.io/v1alpha1/types.go`. A new exported constant `DRPlanLabel = "soteria.io/drplan"` is added. `make manifests` and `make generate` succeed.

2. **AC2 — Discovery signature change:** `VMDiscoverer.DiscoverVMs` signature changes from `(ctx, metav1.LabelSelector)` to `(ctx, planName string)`. `TypedVMDiscoverer` lists VMs using exact label selector `soteria.io/drplan=<planName>`. `GroupByWave` remains unchanged.

3. **AC3 — Reconciler uses plan name:** `DRPlanReconciler.Reconcile` calls `DiscoverVMs(ctx, plan.Name)` instead of `DiscoverVMs(ctx, plan.Spec.VMSelector)`. All downstream logic (wave grouping, volume groups, chunking, preflight) operates identically.

4. **AC4 — `mapVMToDRPlans` becomes O(1):** The function reads the VM's `soteria.io/drplan` label and enqueues the single named plan. If the label is absent or empty, no reconcile requests are enqueued.

5. **AC5 — `vmRelevantChangePredicate` handles label changes:** When the `soteria.io/drplan` label is added, removed, or changed, the predicate fires and the relevant DRPlan(s) are reconciled (both old and new plan if the value changed).

6. **AC6 — Remove `validateVMSelector`:** In `pkg/apis/soteria.io/v1alpha1/validation.go`, the `validateVMSelector` function is removed. `ValidateDRPlan` no longer validates `vmSelector`. Validation of `waveLabel` and `maxConcurrentFailovers` remains unchanged.

7. **AC7 — Discovery unit tests pass:** Tests in `pkg/engine/discovery_test.go` verify: VMs with matching `soteria.io/drplan` label are discovered, VMs without the label are excluded, VMs with a different plan name are excluded, wave grouping still works correctly.

## Tasks / Subtasks

- [x] Task 1: API type changes (AC: #1, #6)
  - [x] 1.1 Add `DRPlanLabel = "soteria.io/drplan"` constant to `pkg/apis/soteria.io/v1alpha1/types.go`
  - [x] 1.2 Remove `VMSelector metav1.LabelSelector` field from `DRPlanSpec`
  - [x] 1.3 Update `DRPlanStatus.DiscoveredVMCount` comment — remove "matching the plan's vmSelector" wording
  - [x] 1.4 Remove `validateVMSelector` function from `validation.go`
  - [x] 1.5 Update `ValidateDRPlan` — remove `validateVMSelector` call, keep `waveLabel`+`maxConcurrentFailovers` validation
  - [x] 1.6 Update `ValidateDRPlanUpdate` if it references vmSelector
  - [x] 1.7 Run `make manifests && make generate` to regenerate CRDs and DeepCopy

- [x] Task 2: Discovery engine refactoring (AC: #2)
  - [x] 2.1 Change `VMDiscoverer` interface: `DiscoverVMs(ctx context.Context, planName string) ([]VMReference, error)`
  - [x] 2.2 Rewrite `TypedVMDiscoverer.DiscoverVMs` — build `labels.SelectorFromSet(labels.Set{DRPlanLabel: planName})` and list VMs with that exact selector
  - [x] 2.3 Remove `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` import from `discovery.go` if no longer needed (keep `"k8s.io/apimachinery/pkg/labels"` for the new selector)
  - [x] 2.4 Import `soteriav1alpha1` for the `DRPlanLabel` constant, or define a local constant — prefer importing from `pkg/apis/soteria.io/v1alpha1` for single source of truth

- [x] Task 3: Reconciler refactoring (AC: #3, #4, #5)
  - [x] 3.1 Update `Reconcile` — change `r.VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)` to `r.VMDiscoverer.DiscoverVMs(ctx, plan.Name)`
  - [x] 3.2 Update "No VMs" condition message — change `"No VMs match the plan's vmSelector"` to `"No VMs have the soteria.io/drplan label for this plan"`
  - [x] 3.3 Rewrite `mapVMToDRPlans` — read `soteria.io/drplan` label from `obj.GetLabels()`, return a single reconcile request for that named plan (O(1) — no DRPlan list needed)
  - [x] 3.4 Handle the case where the label is absent/empty — return nil (no requests)
  - [x] 3.5 Handle `soteria.io/drplan` label value change — use a custom `handler.EventHandler` (see Dev Notes) so that on UpdateEvent both old and new label values are enqueued. Do NOT rely on periodic requeue — AC5 requires prompt reconciliation of both old and new plan
  - [x] 3.6 Update `vmRelevantChangePredicate` — specifically detect `soteria.io/drplan` label add/remove/change; also still trigger on wave label changes that affect wave grouping
  - [x] 3.7 Remove unused imports from reconciler (`"k8s.io/apimachinery/pkg/labels"`, `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` if no longer needed)
  - [x] 3.8 RBAC markers: the `+kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch` marker on `reconciler.go` must STAY — it is needed for the primary informer. The map function no longer lists DRPlans but the controller still watches them

- [x] Task 4: Update tests (AC: #7)
  - [x] 4.1 Rewrite `TestTypedVMDiscoverer` — update VMs to use `soteria.io/drplan: <planName>` labels, call `DiscoverVMs(ctx, "plan-a")`, assert only matching VMs returned
  - [x] 4.2 Add test: VMs without `soteria.io/drplan` label are not discovered
  - [x] 4.3 Add test: VMs with different plan name are not discovered
  - [x] 4.4 Verify `TestGroupByWave` still passes unchanged (wave grouping is plan-name agnostic)
  - [x] 4.5 Update reconciler unit tests in `pkg/controller/drplan/reconciler_test.go` — update mock `DiscoverVMs` calls to use plan name string instead of label selector
  - [x] 4.6 Add reconciler test: `mapVMToDRPlans` returns single request for plan named in label
  - [x] 4.7 Add reconciler test: `mapVMToDRPlans` returns nil when label absent
  - [x] 4.8 Update or add validation tests in `pkg/apis/soteria.io/v1alpha1/` — `ValidateDRPlan` should no longer reject plans without `vmSelector`

- [x] Task 5: Fix compilation across the codebase (AC: all)
  - [x] 5.1 Update all callers of `DiscoverVMs` in `pkg/admission/` — the DRPlan validator calls `v.ExclusivityChecker.VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)` — change to `DiscoverVMs(ctx, plan.Name)`
  - [x] 5.2 Update VM validator's `checkWaveConflictForPlan` which calls `v.VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)` — change to `DiscoverVMs(ctx, plan.Name)`
  - [x] 5.3 Update `ExclusivityChecker` methods if they reference `VMSelector` — note: `FindMatchingPlans` uses `plan.Spec.VMSelector` on line 76 of `exclusivity.go` — this needs updating to use the `DRPlanLabel` for matching
  - [x] 5.4 Update admission test files (`exclusivity_test.go`, `drplan_validator_test.go`, `vm_validator_test.go`) to use new interface signature and remove `VMSelector` references
  - [x] 5.5 Update `setup.go` if any setup function signatures reference `VMSelector`
  - [x] 5.6 Grep for any remaining `VMSelector` or `vmSelector` references across the codebase and fix — check `cmd/`, `test/integration/`, and any other callers
  - [x] 5.7 Run `make test` to verify all unit tests pass
  - [x] 5.8 Run `make lint-fix` followed by `make lint`

- [x] Task 6: Tiered documentation compliance
  - [x] 6.1 Update `discovery.go` file-level block comment (Tier 2) — describe the new plan-name-based discovery instead of label-selector
  - [x] 6.2 Update `reconciler.go` file-level block comment (Tier 2) — replace "VMs matching the plan's label selector" with "VMs carrying the `soteria.io/drplan` label"
  - [x] 6.3 Update `mapVMToDRPlans` godoc — describe the O(1) label read instead of O(N) DRPlan scanning
  - [x] 6.4 Add Tier 3 domain 'why' comment on `DRPlanLabel` constant — explain that the single-label-key-per-resource Kubernetes semantics structurally enforce one-plan-per-VM exclusivity
  - [x] 6.5 Update `exclusivity.go` Tier 2 block comment — the `FindMatchingPlans` O(plans × VMs) cross-check pattern is being removed/simplified
  - [x] 6.6 Verify all `pkg/` packages still have Tier 1 `doc.go` godoc

## Dev Notes

### Previous Story Learnings (from Epic 2)

- RBAC markers: place on the file that contains the reconciler (`reconciler.go`), not in `doc.go` — established in Story 2.5
- `make manifests` must be run after any marker change — CRD and RBAC yamls are generated, never hand-edited
- Fake client in unit tests doesn't handle resourceVersion, UIDs, or status subresources correctly — use envtest for controller tests
- Context cancellation: fake client doesn't honor `context.Canceled` — use reactors to simulate error paths in tests
- When modifying validation, also update corresponding strategy (`registry/drplan/strategy.go`) if it calls validation — check for `Validate` calls
- Code review pattern from Epic 2: `apierrors.IsNotFound()` preferred over raw error comparison for Kubernetes API errors

### Architecture Context

This story is the first of a 4-story refactoring epic (2b). It replaces the `vmSelector` label-selector approach with a convention-based `soteria.io/drplan: <planName>` label on VMs. The key insight: a Kubernetes label key can only have one value per resource, so exclusivity becomes a structural invariant instead of a code-enforced check.

**What changes:**
- `DRPlanSpec.VMSelector` field is deleted
- `VMDiscoverer` interface signature changes from `LabelSelector` to `string` (plan name)
- `mapVMToDRPlans` drops from O(N) DRPlan scanning to O(1) label read
- `validateVMSelector` is removed from API validation
- `vmRelevantChangePredicate` needs to handle old-plan/new-plan reconciliation on label change

**What does NOT change (must be preserved):**
- `GroupByWave` — waves are still determined by the separate wave label (`soteria.io/wave`)
- `ResolveVolumeGroups` — namespace-level consistency logic unchanged
- `ChunkWaves` — DRGroup chunking unchanged
- `composePreflightReport` — preflight pipeline unchanged
- `nsConsistencyAnnotationChangePredicate` — namespace annotation watch unchanged
- `mapNamespaceToDRPlans` — namespace-to-plan mapping unchanged
- `updateStatus` — status patch logic unchanged
- Secondary watch on `corev1.Namespace` — unchanged

### Critical Implementation Details

**`mapVMToDRPlans` — handling label value changes:**
The current `vmRelevantChangePredicate` already fires on any label change (line 559 of reconciler.go: `!reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())`). However, `mapVMToDRPlans` currently only sees the *new* object. When a VM's `soteria.io/drplan` label changes from `plan-a` to `plan-b`, you need to enqueue BOTH `plan-a` (so it removes the VM from its waves) and `plan-b` (so it discovers the VM). controller-runtime's `handler.EnqueueRequestsFromMapFunc` passes the *new* object by default.

**This is a HARD REQUIREMENT (AC5) — do NOT use the periodic requeue fallback.**

**Solution approach:** Change the watch to use `handler.Funcs` or create a custom `handler.EventHandler` that exposes both old and new objects. Alternatively, keep the current predicate (which gives you the new object) and enqueue the new plan name from the label — the old plan will also reconcile on its periodic requeue (10 min). If faster old-plan reconciliation is needed, use `handler.TypedEnqueueRequestsFromMapFunc` with `TypedFuncs` that handle `UpdateEvent` separately to extract both old and new label values.

Recommended approach: Override the `Watches` call with a custom event handler:

```go
Watches(
    &kubevirtv1.VirtualMachine{},
    handler.Funcs{
        CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.RateLimitingInterface) {
            r.enqueueForVM(e.Object, q)
        },
        UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
            r.enqueueForVM(e.ObjectOld, q)
            r.enqueueForVM(e.ObjectNew, q)
        },
        DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.RateLimitingInterface) {
            r.enqueueForVM(e.Object, q)
        },
    },
    builder.WithPredicates(vmRelevantChangePredicate()),
)
```

Where `enqueueForVM` reads `soteria.io/drplan` and pushes a `reconcile.Request`.

**`TypedVMDiscoverer.DiscoverVMs` — exact label selector:**
Replace the arbitrary `LabelSelector` parsing with:
```go
func (d *TypedVMDiscoverer) DiscoverVMs(ctx context.Context, planName string) ([]VMReference, error) {
    sel := labels.SelectorFromSet(labels.Set{soteriav1alpha1.DRPlanLabel: planName})
    var vmList kubevirtv1.VirtualMachineList
    if err := d.Reader.List(ctx, &vmList, &client.ListOptions{LabelSelector: sel}); err != nil {
        return nil, err
    }
    // ... same projection logic ...
}
```

**`ExclusivityChecker.FindMatchingPlans` — adaptation for Story 2b.2:**
This function currently iterates all DRPlans and tests each `vmSelector` against a VM's labels. In Story 2b.1, you need it to still compile and work. The simplest approach: change it to check whether the VM's `soteria.io/drplan` label matches the plan name. However, note that Story 2b.2 deletes `exclusivity.go` entirely. For this story, the minimal change is to update `FindMatchingPlans` to compare the VM's `soteria.io/drplan` label value against each plan's `.Name` — this preserves the existing API surface while adapting to the new model.

### CRD Schema Migration Note

Removing `VMSelector` from `DRPlanSpec` is a **breaking CRD schema change**. Existing `DRPlan` objects stored in ScyllaDB will still have a `vmSelector` JSON field in their blob. Because the storage layer uses a generic KV store (blob-based), the old field will be silently ignored during deserialization (Go ignores unknown JSON fields by default). No data migration is needed — the field simply becomes dead data in existing blobs and will disappear on next update. This story assumes a **single-step upgrade** — there is no need for a multi-version conversion webhook since the project is still in `v1alpha1`.

### Existing Code Patterns to Follow

- **Import alias:** `soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"` — used consistently across the codebase
- **Logging style:** `logger.Info("Starting reconciliation")`, `logger.V(1).Info(...)`, `logger.Error(err, "msg", "key", val)`
- **Event style:** `r.event(&plan, "Warning", "DiscoveryFailed", err.Error())`
- **Error wrapping:** lowercase, wrap with `%w` — `fmt.Errorf("discovering VMs: %w", err)`
- **Context propagation:** always use `ctx` from reconcile — never `context.Background()`
- **Status conditions:** always `metav1.Condition` with `meta.SetStatusCondition`

### Files to Modify

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `DRPlanLabel` constant, remove `VMSelector` field from `DRPlanSpec` |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Remove `validateVMSelector`, update `ValidateDRPlan` |
| `pkg/engine/discovery.go` | Change `VMDiscoverer` interface, rewrite `TypedVMDiscoverer.DiscoverVMs` |
| `pkg/engine/discovery_test.go` | Update tests to use plan name, add new test cases |
| `pkg/controller/drplan/reconciler.go` | Update `Reconcile`, rewrite `mapVMToDRPlans`, update `vmRelevantChangePredicate`, update watch setup |
| `pkg/controller/drplan/reconciler_test.go` | Update mock calls, add `mapVMToDRPlans` tests |
| `pkg/admission/exclusivity.go` | Update `FindMatchingPlans` to use `DRPlanLabel` instead of `vmSelector` |
| `pkg/admission/exclusivity_test.go` | Update tests for new matching logic |
| `pkg/admission/drplan_validator.go` | Update `DiscoverVMs` call, remove `vmSelector` parsing |
| `pkg/admission/drplan_validator_test.go` | Update tests |
| `pkg/admission/vm_validator.go` | Update `DiscoverVMs` call in `checkWaveConflictForPlan` |
| `pkg/admission/vm_validator_test.go` | Update tests |

### Files NOT to Modify (Story 2b.2 or later)

- `pkg/admission/exclusivity.go` — will be deleted entirely in Story 2b.2. In THIS story, make the MINIMUM changes needed for compilation (update `FindMatchingPlans` to compare `soteria.io/drplan` label against plan names instead of parsing `vmSelector`). Do NOT redesign the architecture — just adapt for the new interface
- `config/samples/` — will be updated in Story 2b.3
- `_bmad-output/planning-artifacts/prd.md` — will be updated in Story 2b.3
- `_bmad-output/planning-artifacts/architecture.md` — will be updated in Story 2b.3
- `_bmad-output/project-context.md` — will be updated in Story 2b.3

### Build Commands

```bash
make manifests    # Regenerate CRDs/RBAC after types.go changes
make generate     # Regenerate DeepCopy after types.go changes
make lint-fix     # Auto-fix code style
make test         # Unit + envtest tests
```

### Project Structure Notes

- `pkg/apis/soteria.io/v1alpha1/` — CRD types and validation (this story modifies types.go and validation.go)
- `pkg/engine/` — Discovery and wave grouping (this story modifies discovery.go)
- `pkg/controller/drplan/` — DRPlan reconciler (this story modifies reconciler.go)
- `pkg/admission/` — Webhooks (this story updates for compilation; Story 2b.2 rewrites)
- Multigroup layout: not used — single `soteria.io/v1alpha1` API group
- DeepCopy regeneration required after `types.go` changes: `make generate`

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2b.1] — Full acceptance criteria
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — Current DRPlanSpec with VMSelector (lines 59-66)
- [Source: pkg/engine/discovery.go] — Current VMDiscoverer interface (line 62) and TypedVMDiscoverer (lines 96-128)
- [Source: pkg/controller/drplan/reconciler.go] — Current mapVMToDRPlans O(N) scan (lines 270-304), vmRelevantChangePredicate (lines 550-565), Reconcile DiscoverVMs call (line 111)
- [Source: pkg/admission/exclusivity.go] — ExclusivityChecker.FindMatchingPlans (lines 59-90)
- [Source: pkg/apis/soteria.io/v1alpha1/validation.go] — validateVMSelector (lines 49-63)
- [Source: _bmad-output/project-context.md] — Project conventions, testing rules, documentation tiers

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List

### Review Findings

- [x] [Review][Decision] `soteria.io/drplan=<planName>` is not enough to map VM watch events back to a namespaced DRPlan when VMs can live outside the plan namespace — Deferred to **Story 2b-1.5** (Cluster-Scoped CRD Migration): making DRPlan cluster-scoped eliminates the namespace mismatch entirely.

- [x] [Review][Decision] The new label convention is not actually unambiguous across namespaces — Deferred to **Story 2b-1.5** (Cluster-Scoped CRD Migration): cluster-scoped names are globally unique, so `plan.Name == drplanLabel` becomes unambiguous.

- [x] [Review][Patch] Story tracking is out of sync with the implementation state [`_bmad-output/implementation-artifacts/2b-1-label-convention-api-discovery-controller.md:3`] — Fixed: updated Status to `review`, checked all task boxes, synced sprint-status to `review`.
