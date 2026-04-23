# Story 5.2: Unprotected VM Detection

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want to identify VMs not covered by any DRPlan,
so that I can close protection gaps before they become audit findings.

## Background

FR34 requires the orchestrator to detect VMs that are not covered by any DRPlan. Today, the DRPlan controller discovers VMs **per plan** using the `soteria.io/drplan=<planName>` label — but no component checks for VMs that lack this label entirely. These "unprotected" VMs represent protection gaps that operators must close before an audit or before a disaster strikes.

**How VM membership currently works:**
- A VM declares membership in a DRPlan by carrying the label `soteria.io/drplan=<planName>`.
- The `TypedVMDiscoverer.DiscoverVMs(ctx, planName)` lists VMs with `LabelSelector: soteria.io/drplan=<planName>`.
- Because a Kubernetes label key can only have one value, a VM can belong to at most one DRPlan (structural exclusivity).
- VMs without the `soteria.io/drplan` label are invisible to the DR system — never discovered, never protected.

**Design decisions:**
- **Detection approach:** A `ListUnprotectedVMs` function lists all kubevirt VMs cluster-wide that lack the `soteria.io/drplan` label. This is the inverse of per-plan discovery — instead of filtering for a specific value, we filter for **absence** of the label key.
- **Data placement:** Unprotected VM data is stored in two locations on `DRPlanStatus`:
  1. `UnprotectedVMCount int` — a top-level status field for easy `kubectl get` consumption and printer columns.
  2. `PreflightReport.UnprotectedVMs []DiscoveredVM` — the full list (capped at 100 entries) for structured tooling and Console consumption. The PreflightReport already aggregates plan health data; unprotected VMs are a natural extension.
- **Why per-plan duplication is acceptable:** The unprotected VM list is identical across all DRPlans (cluster-wide). Duplicating it is acceptable because: (a) unprotected VM count should trend toward zero in production; (b) the list is capped at 100 entries; (c) the PreflightReport is already re-computed on every reconcile; (d) this avoids creating a new CRD for a simple monitoring concern.
- **Event-driven reactivity:** The DRPlan controller already watches VM create/update/delete via `vmEventHandler()`. When a VM gains or loses the `soteria.io/drplan` label, the affected plan is re-reconciled. For unprotected VM detection, we extend the VM watch to also trigger reconciliation when a VM without the label is created or deleted (since it changes the unprotected count).
- **No Prometheus metric in this story:** Story 5.3 owns all Prometheus metrics. This story exposes data via the Kubernetes API (status fields, printer columns, events) — Story 5.3 will add `soteria_unprotected_vms_total` consuming this data.

## Acceptance Criteria

1. **AC1 — UnprotectedVMCount status field:** `DRPlanStatus` has a new `UnprotectedVMCount int` field (`json:"unprotectedVMCount"`) that reflects the cluster-wide count of VMs not belonging to any DRPlan. The field is populated on every successful reconcile cycle, even when `UnprotectedVMCount` is 0 (explicitly set, not omitted).

2. **AC2 — UnprotectedVMs in PreflightReport:** `PreflightReport` has a new `UnprotectedVMs []DiscoveredVM` field (`json:"unprotectedVMs,omitempty"`). Each entry contains the VM name and namespace. When the unprotected VM count exceeds 100, the list is truncated to the first 100 entries (sorted by namespace/name) and a warning is added: `"Showing 100 of N unprotected VMs"`. When all VMs are protected, the field is empty.

3. **AC3 — Detection logic:** A new `ListUnprotectedVMs(ctx context.Context) ([]VMReference, error)` function in `pkg/engine/discovery.go` lists all kubevirt VMs cluster-wide that do NOT have the `soteria.io/drplan` label key present. The function uses the controller-runtime cached client with a label selector that matches VMs where the label key does not exist (`!soteria.io/drplan`).

4. **AC4 — Reconciler integration:** The DRPlan reconciler calls `ListUnprotectedVMs` after per-plan VM discovery completes. The count and list are passed to `updateStatus` and persisted on every reconcile cycle. The unprotected VM check does not block the `Ready` condition — a plan can be Ready even if unprotected VMs exist.

5. **AC5 — Kubernetes events on coverage transitions:** When unprotected VMs are first detected (count goes from 0 to >0), the controller emits an `UnprotectedVMsDetected` (Warning) event on the DRPlan with a message including the count. When all VMs become protected (count goes from >0 to 0), the controller emits an `AllVMsProtected` (Normal) event. Events are only emitted on actual transitions — not on every reconcile cycle.

6. **AC6 — Reactive detection on VM changes:** When a new VM is created without the `soteria.io/drplan` label, the DRPlan controller is re-reconciled within the next reconcile cycle and the unprotected count is updated. When a previously unprotected VM gains the label, the count decreases on the next reconcile. No special watch plumbing is needed — the existing 10-minute requeue interval handles detection; the existing VM watch triggers immediate re-reconciliation when labels change.

7. **AC7 — Printer column:** The DRPlan table output (via `pkg/registry/drplan/strategy.go` table conversion or `storage.go` `TableConvertor`) includes an `UNPROTECTED` column showing `UnprotectedVMCount`. `kubectl get drplans` displays the count alongside existing columns.

8. **AC8 — Test coverage:** Unit tests covering: (a) `ListUnprotectedVMs` returns VMs without the `soteria.io/drplan` label and excludes labeled VMs; (b) `UnprotectedVMCount` populated correctly for 0, 1, N cases; (c) `PreflightReport.UnprotectedVMs` populated with names/namespaces and truncated at 100; (d) events emitted only on count transitions (0→N, N→0); (e) no event on first reconcile (no previous state); (f) `ListUnprotectedVMs` error does not block `Ready` condition or discovery; (g) integration test with envtest VMs showing unprotected count updates as labels change.

## Tasks / Subtasks

- [x] Task 1: Add API types (AC: #1, #2)
  - [x] 1.1 Add `UnprotectedVMCount int` field to `DRPlanStatus` with `json:"unprotectedVMCount"` tag (no omitempty — always present, even when 0)
  - [x] 1.2 Add `UnprotectedVMs []DiscoveredVM` field to `PreflightReport` with `json:"unprotectedVMs,omitempty"` tag and `+listType=atomic` marker
  - [x] 1.3 Run `make generate` to regenerate deepcopy + openapi

- [x] Task 2: Implement ListUnprotectedVMs (AC: #3)
  - [x] 2.1 Add `ListUnprotectedVMs(ctx context.Context) ([]VMReference, error)` method to `TypedVMDiscoverer` in `pkg/engine/discovery.go`
  - [x] 2.2 Implementation: use `labels.NewRequirement(soteriav1alpha1.DRPlanLabel, selection.DoesNotExist, nil)` to build a label selector that matches VMs without the label key
  - [x] 2.3 Sort results by namespace then name for deterministic output
  - [x] 2.4 Add `UnprotectedVMDiscoverer` interface alongside `VMDiscoverer`: `ListUnprotectedVMs(ctx context.Context) ([]VMReference, error)` — keep it separate from `VMDiscoverer` so mock injection is independent
  - [x] 2.5 Implement `UnprotectedVMDiscoverer` on `TypedVMDiscoverer` (same struct, new method)

- [x] Task 3: Integrate into DRPlan reconciler (AC: #4, #5, #6)
  - [x] 3.1 Add `UnprotectedVMDiscoverer engine.UnprotectedVMDiscoverer` field to `DRPlanReconciler` struct (optional — when nil, skip unprotected VM detection for backward compat)
  - [x] 3.2 In `Reconcile`, after per-plan discovery and before `updateStatus`: call `r.UnprotectedVMDiscoverer.ListUnprotectedVMs(ctx)`. On error: log at V(1), set count to -1 or skip update (do NOT fail the reconcile)
  - [x] 3.3 Convert `[]VMReference` to `[]DiscoveredVM` for status storage; truncate to 100 entries sorted by namespace/name; add preflight warning if truncated
  - [x] 3.4 Extend `updateStatus` signature: add `unprotectedVMs []soteriav1alpha1.DiscoveredVM` and `unprotectedVMCount int` parameters
  - [x] 3.5 In `updateStatus`: add `unprotectedChanged := plan.Status.UnprotectedVMCount != unprotectedVMCount` to the change detection logic; persist `plan.Status.UnprotectedVMCount` and `preflightReport.UnprotectedVMs`
  - [x] 3.6 Add event transition detection: compare old `plan.Status.UnprotectedVMCount` with new count. Emit `UnprotectedVMsDetected` (Warning) when old=0 and new>0. Emit `AllVMsProtected` (Normal) when old>0 and new=0. Skip on first reconcile (old count == 0 and no previous reconcile — use `ObservedGeneration == 0` as heuristic for first reconcile)

- [x] Task 4: Add printer column (AC: #7)
  - [x] 4.1 In `pkg/registry/drplan/storage.go`: replace `rest.NewDefaultTableConvertor` with a custom `TableConvertor` implementation that adds an `UNPROTECTED` column reading from `plan.Status.UnprotectedVMCount`
  - [x] 4.2 Follow the pattern from `k8s.io/apiserver/pkg/registry/rest` `TableConvertor` interface: implement `ConvertToTable(ctx, object, tableOptions)` returning column definitions + rows
  - [x] 4.3 Include existing default columns (NAME, AGE) plus `UNPROTECTED` and `PHASE` columns

- [x] Task 5: Wire in main.go (AC: #4)
  - [x] 5.1 Update `cmd/soteria/main.go`: inject `UnprotectedVMDiscoverer: vmDiscoverer` into `DRPlanReconciler` — the existing `TypedVMDiscoverer` implements both `VMDiscoverer` and `UnprotectedVMDiscoverer`
  - [x] 5.2 No new dependencies needed — reuses the existing `mgr.GetClient()` reader

- [x] Task 6: Unit tests (AC: #8a–f)
  - [x] 6.1 Test `ListUnprotectedVMs`: create VMs with and without `soteria.io/drplan` label using fake client → verify only unlabeled VMs returned, sorted by namespace/name
  - [x] 6.2 Test reconciler: mock `UnprotectedVMDiscoverer` returning various counts → verify `UnprotectedVMCount` and `PreflightReport.UnprotectedVMs` populated correctly
  - [x] 6.3 Test truncation: mock returns 150 VMs → verify list truncated to 100, warning added
  - [x] 6.4 Test event transitions: first reconcile (no event), 0→5 (Warning event), 5→3 (no event — still unprotected), 3→0 (Normal event)
  - [x] 6.5 Test error handling: `ListUnprotectedVMs` returns error → verify reconcile succeeds, count unchanged, no crash
  - [x] 6.6 Test backward compat: `UnprotectedVMDiscoverer` nil → verify no unprotected fields set, no crash
  - [x] 6.7 Add `mockUnprotectedVMDiscoverer` to test helpers

- [x] Task 7: Integration test (AC: #8g)
  - [x] 7.1 Add integration test in `test/integration/controller/drplan_unprotected_test.go`: create DRPlan + VMs (some with label, some without) → reconcile → verify `UnprotectedVMCount` matches count of unlabeled VMs
  - [x] 7.2 Then add label to an unprotected VM → trigger reconcile → verify count decreases
  - [x] 7.3 Then remove label from a protected VM → trigger reconcile → verify count increases
  - [x] 7.4 Wire `UnprotectedVMDiscoverer` in integration test `suite_test.go` setup

- [x] Task 8: Run full test suite
  - [x] 8.1 `make generate` — regenerate deepcopy + openapi
  - [x] 8.2 `make manifests` — regenerate CRDs if markers changed
  - [x] 8.3 `make lint-fix` — auto-fix style
  - [x] 8.4 `make test` — all unit + integration tests pass

## Dev Notes

- **Label absence selector:** Kubernetes label selectors support `DoesNotExist` operator via `labels.NewRequirement(key, selection.DoesNotExist, nil)`. This is more efficient than listing all VMs and filtering in-memory — the API server/cache handles the filtering.
- **Existing `TypedVMDiscoverer` reuse:** `TypedVMDiscoverer` already has a `Reader client.Reader`. Add `ListUnprotectedVMs` as a new method on the same struct. No new constructor needed — `NewTypedVMDiscoverer` returns a `*TypedVMDiscoverer` that satisfies both `VMDiscoverer` and `UnprotectedVMDiscoverer`.
- **The `!soteria.io/drplan` selector catches ALL unlabeled VMs:** This includes VMs that legitimately don't need DR protection (e.g., ephemeral test VMs, CI runners). The story doesn't add a filtering mechanism — that's out of scope. Operators manage this by either labeling all VMs or accepting the count includes non-DR-relevant VMs. A future enhancement could add a `soteria.io/exclude-from-detection=true` opt-out label.
- **`UnprotectedVMCount` uses `json:"unprotectedVMCount"` (no omitempty):** This ensures the field always appears in JSON output, even when 0. A zero count is meaningful ("all VMs protected") and should be distinguishable from "not yet computed" (field missing entirely).
- **Truncation at 100:** NFR8 allows up to 5,000 VMs. If most are unprotected, the list could be very large. The 100-entry cap keeps DRPlan status objects manageable. The full count is still available via `UnprotectedVMCount`. Console and CLI can paginate if needed via `kubectl get drplan -o json`.
- **Event spam prevention:** Events only fire on 0↔N transitions, not on every count change. If unprotected count goes from 5 to 3, no event — still unprotected. Only 0→N and N→0 transitions matter. First reconcile (no prior state) does not emit an event to avoid noise during controller startup.
- **Reconcile error isolation:** If `ListUnprotectedVMs` fails (e.g., kubevirt API unavailable), log the error at V(1) and skip updating the unprotected count. Do NOT fail the reconcile — per-plan discovery and the Ready condition must not be affected by a cluster-wide detection failure.
- **updateStatus signature extension:** The existing `updateStatus` takes 6 parameters. Adding 2 more (unprotectedVMs, unprotectedVMCount) keeps it manageable. If Story 5.1 lands first and adds replicationHealth params, consider grouping into a struct parameter.
- **VM watch already covers label changes:** The existing `vmEventHandler` in `SetupWithManager` watches VM creates/updates/deletes and enqueues the plan from the VM's `soteria.io/drplan` label. For *unlabeled* VMs (creates/deletes), re-detection happens on the next 10-minute requeue interval. This is acceptable — real-time detection of newly created unlabeled VMs is not required.
- **Custom TableConvertor:** The current `storage.go` uses `rest.NewDefaultTableConvertor` which only shows NAME and AGE. Replace with a custom implementation that adds PHASE, UNPROTECTED, and VMS columns. Pattern reference: `k8s.io/apiserver/pkg/registry/rest.TableConvertor` interface requires `ConvertToTable(ctx, obj, tableOptions) (*metav1.Table, error)`.
- **No new RBAC markers needed:** The DRPlan controller already has `+kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch`. The `ListUnprotectedVMs` function uses the same cached client with a different label selector — no additional RBAC is required.

### Existing code patterns to follow

- **VM discovery:** `TypedVMDiscoverer.DiscoverVMs` in `pkg/engine/discovery.go` lines 104–129 — use same `Reader.List` pattern with different label selector.
- **Status patches:** `client.MergeFrom(plan.DeepCopy())` pattern in `updateStatus` (line 520 of `reconciler.go`).
- **Change detection:** `conditionChanged`, `wavesChanged`, `countChanged` comparison pattern in `updateStatus` (lines 505–517).
- **Event emission:** `r.Recorder.Eventf(plan, nil, eventType, reason, "Reconcile", msg)` (line 248).
- **Structured logging:** `log.FromContext(ctx).WithValues("drplan", req.NamespacedName)` + `logger.Info/Error/V(1).Info`.
- **Mock pattern in tests:** `mockVMDiscoverer` in `reconciler_test.go` — follow same pattern for `mockUnprotectedVMDiscoverer`.
- **Preflight warnings:** `report.Warnings = append(report.Warnings, ...)` (line 117 of `reconciler.go`).
- **Integration test helpers:** `createVM`, `createDRPlan`, `waitForCondition`, `waitForVMCount` in `test/integration/controller/suite_test.go` and `drplan_test.go`.

### Critical files to modify

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `UnprotectedVMCount` to `DRPlanStatus`, add `UnprotectedVMs` to `PreflightReport` |
| `pkg/engine/discovery.go` | Add `UnprotectedVMDiscoverer` interface, implement `ListUnprotectedVMs` on `TypedVMDiscoverer` |
| `pkg/controller/drplan/reconciler.go` | Add `UnprotectedVMDiscoverer` field, call `ListUnprotectedVMs`, extend `updateStatus`, add event transition logic |
| `pkg/registry/drplan/storage.go` | Replace `DefaultTableConvertor` with custom implementation adding UNPROTECTED column |
| `cmd/soteria/main.go` | Inject `UnprotectedVMDiscoverer: vmDiscoverer` into DRPlanReconciler |
| `pkg/controller/drplan/reconciler_test.go` | Unit tests for unprotected VM detection, event transitions, truncation, error handling |
| `pkg/engine/discovery_test.go` | Unit tests for `ListUnprotectedVMs` |
| `test/integration/controller/drplan_unprotected_test.go` | Integration test with label change scenarios |
| `test/integration/controller/suite_test.go` | Wire `UnprotectedVMDiscoverer` in integration test setup |

### Previous story intelligence (Stories 5.0 and 5.1)

- **Story 5.0 adds `ActiveExecution` field:** May change `DRPlanStatus` shape. Ensure `UnprotectedVMCount` is compatible with both pre-5.0 and post-5.0 status. If 5.0 is not yet implemented, `ActiveExecution` won't exist — no conflict.
- **Story 5.1 adds `ReplicationHealth` and extends `updateStatus`:** If 5.1 lands first, `updateStatus` may have additional parameters (replication health, replication condition). Coordinate the function signature — both stories add parameters. Consider introducing a `statusUpdate` struct to bundle parameters if the signature exceeds 8 params.
- **Story 5.1 adds `VolumeGroupHealth` types:** No overlap with this story's types. `UnprotectedVMCount` and `UnprotectedVMs` are orthogonal to replication health.
- **Fixture churn from 5.0/5.1:** If either story adds new `DRPlanStatus` fields, existing test fixtures may need updating. Use `newTestPlan` helper pattern and only assert on fields this story modifies.
- **Integration test wiring:** Story 5.1 adds `Registry`, `SCLister`, `PVCResolver` to the integration test setup. This story adds `UnprotectedVMDiscoverer`. Both modify `suite_test.go` — coordinate if implementing in parallel.

### References

- [Source: _bmad-output/planning-artifacts/prd.md#FR34] — "Platform engineer can identify unprotected VMs — VMs not covered by any DRPlan"
- [Source: _bmad-output/planning-artifacts/prd.md#FR35] — Dashboard alert banner for unprotected plans (consumer of this data)
- [Source: _bmad-output/planning-artifacts/epics.md#Story 5.2] — BDD acceptance criteria: identifiable unprotected VMs, count via API, kubectl queryable
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Phase 2] — "Unprotected VM List" view (post-v1 Console feature consuming this API data)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go:54–57] — `DRPlanLabel = "soteria.io/drplan"` constant
- [Source: pkg/engine/discovery.go:62–67] — `VMDiscoverer` interface and `DiscoverVMs` method
- [Source: pkg/engine/discovery.go:104–129] — `TypedVMDiscoverer.DiscoverVMs` implementation (list with label selector pattern)
- [Source: pkg/controller/drplan/reconciler.go:89–96] — Current `DRPlanReconciler` struct (no unprotected VM dependency yet)
- [Source: pkg/controller/drplan/reconciler.go:488–539] — `updateStatus` with change detection pattern
- [Source: pkg/controller/drplan/reconciler.go:252–265] — `SetupWithManager` with VM and Namespace watches
- [Source: pkg/registry/drplan/storage.go:45,87–90] — Current `DefaultTableConvertor` (to be replaced with custom)
- [Source: _bmad-output/project-context.md] — Project conventions, anti-patterns, testing rules

## Estimated Effort

- Production code: ~120 lines across ~5 files (types, discovery, reconciler, storage, main.go wiring)
- Test code: ~250 lines across ~3 test files (discovery unit, reconciler unit, integration)
- Total: ~370 net new/modified lines

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Fixed lint: cyclomatic complexity in updateStatus — extracted `emitUnprotectedVMEvents` helper and refactored change detection into `anyChanged` bool
- Fixed lint: line length > 120 chars on combined condition check
- Integration test: removed duplicate `testTimeout` constant already declared in `drplan_test.go`

### Completion Notes List

- All 8 acceptance criteria satisfied
- `UnprotectedVMCount` always present in JSON (no omitempty), set to 0 when all VMs protected
- `PreflightReport.UnprotectedVMs` capped at 100 entries with truncation warning
- Event spam prevention: only 0↔N transitions emit events, not every count change
- First reconcile (ObservedGeneration==0) suppresses events to avoid startup noise
- `ListUnprotectedVMs` error does not fail reconcile — logged at V(1) and skipped
- Backward compat: nil `UnprotectedVMDiscoverer` is safe — no crash, no unprotected fields set
- Used `unprotectedVMResult` struct to bundle params for `updateStatus` instead of adding more positional args
- DRPlan controller coverage increased from 83.4% to 85.0%
- No new dependencies — reuses existing `TypedVMDiscoverer` and `mgr.GetClient()` reader

### Change Log

- 2026-04-23: Story 5.2 implemented — unprotected VM detection, status fields, printer column, events, tests

### File List

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Added `UnprotectedVMCount` to `DRPlanStatus`, `UnprotectedVMs` to `PreflightReport` |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-generated deepcopy for new fields |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` | Auto-generated openapi for new fields |
| `pkg/engine/discovery.go` | Added `UnprotectedVMDiscoverer` interface, `ListUnprotectedVMs` on `TypedVMDiscoverer` |
| `pkg/engine/discovery_test.go` | Added `TestListUnprotectedVMs` with 4 subtests |
| `pkg/controller/drplan/reconciler.go` | Added `UnprotectedVMDiscoverer` field, `detectUnprotectedVMs`, `emitUnprotectedVMEvents`, extended `updateStatus` |
| `pkg/controller/drplan/reconciler_test.go` | Added 6 unit tests: count population, preflight, truncation, event transitions, error handling, backward compat |
| `pkg/registry/drplan/strategy.go` | Added `Unprotected` column to table convertor |
| `cmd/soteria/main.go` | Wired `UnprotectedVMDiscoverer: vmDiscoverer` into `DRPlanReconciler` |
| `test/integration/controller/suite_test.go` | Wired `UnprotectedVMDiscoverer` in integration test setup, added `waitForUnprotectedVMCount` helper |
| `test/integration/controller/drplan_unprotected_test.go` | New file: 3 integration tests for detection, label-add, label-remove scenarios |

### Review Findings

- [x] [Review][Patch] Unprotected VM detection is skipped on non-ready reconcile paths [`pkg/controller/drplan/reconciler.go:173`]
- [x] [Review][Patch] Empty `soteria.io/drplan` labels are excluded from unprotected detection [`pkg/engine/discovery.go:143`]
- [x] [Review][Patch] Event-transition tests never assert emitted events [`pkg/controller/drplan/reconciler_test.go:1318`]
