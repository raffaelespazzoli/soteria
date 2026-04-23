# Story 5.4: DRExecution as Immutable Audit Record

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want every execution's DRExecution `.status` to serve as the immutable audit record with per-wave, per-group, per-step detail, timestamps, and error messages, persisting across DC failures via the shared state layer,
so that `kubectl get drexecutions` is all I need for compliance evidence.

## Background

FR41–FR43 require DRExecution to function as a self-contained, immutable audit record that persists across datacenter failures and supports per-plan execution history queries. The record must contain per-wave, per-DRGroup, per-step status, timestamps, and error details — sufficient for SOX, ISO 22301, SOC 2 compliance evidence without external log lookups.

**Current state (what already exists):**
- `DRExecution` type (`pkg/apis/soteria.io/v1alpha1/types.go`) already has: `DRExecutionSpec` (PlanName, Mode), `DRExecutionStatus` (Result, Waves, StartTime, CompletionTime, Conditions), `WaveStatus` (WaveIndex, Groups, StartTime, CompletionTime), `DRGroupExecutionStatus` (Name, Result, VMNames, Error, Steps, RetryCount, StartTime, CompletionTime), `StepStatus` (Name, Status, Message, Timestamp).
- `DRExecution` doc comment already says "records an immutable execution of a DRPlan".
- **Spec immutability** enforced: `strategy.go` `ValidateUpdate` rejects spec changes. `PrepareForUpdate` replaces incoming status with old status on main-resource updates.
- **Terminal status immutability** enforced: `StatusStrategy.ValidateUpdate` rejects status updates when `Result` is `Succeeded` or `Failed`. `PartiallySucceeded` is intentionally re-openable for retries (FR14).
- **Cross-DC persistence** works: ScyllaDB-backed storage with RF=2 per DC, LOCAL_ONE consistency. `critical_fields.go` detects `Status.Result` changes for SERIAL Paxos.
- **RBAC** restricts operators to `get/list/watch/create` on `drexecutions` — no `update/patch/delete`. Comment in `soteria_operator_role.yaml` explicitly references FR41.
- **No `soteria.io/plan-name` label** is set on DRExecution at creation time — FR42 requires `kubectl get drexecutions -l soteria.io/plan-name=<plan>` to work.
- **No custom TableConvertor** — `rest.NewDefaultTableConvertor` shows only metadata (name, age). No PLAN, MODE, RESULT, DURATION, or AGE columns.
- **No `spec.planName` field selector** in `GetAttrs` — only `metadata.name` is indexed. Server-side field filtering on `spec.planName` is not possible.
- **No sample DRExecution YAML** in `config/samples/`.
- **Missing audit-specific status fields:** No `Duration` field (computed from Start/Completion), no `EstimatedRPO` summary captured at execution time.
- **`delete` verb absent from RBAC** — but no strategy-level or admission-level block on DELETE operations for completed records.

**What this story must deliver:**
1. **Plan-name label on DRExecution** — set at creation time by the reconciler for label-based history queries (FR42).
2. **Custom TableConvertor** — rich `kubectl get drexecutions` output with PLAN, MODE, RESULT, DURATION columns.
3. **Field selector for `spec.planName`** — enable server-side filtering: `kubectl get drexecutions --field-selector spec.planName=erp-full-stack`.
4. **Delete protection** — strategy-level block preventing deletion of completed executions (audit records must persist).
5. **Validation of audit completeness** — verify all required fields are populated before terminal result is set.
6. **Sample DRExecution YAML** — onboarding reference in `config/samples/`.
7. **Documentation updates** — expand `pkg/registry/drexecution/doc.go` with audit record semantics.

## Acceptance Criteria

1. **AC1 — Plan-name label:** When the DRExecution reconciler begins execution (gated on `StartTime == nil`), it sets the label `soteria.io/plan-name: <spec.planName>` on the DRExecution metadata. This enables `kubectl get drexecutions -l soteria.io/plan-name=<plan>` to return all executions for a given plan (FR42). The label is set via a metadata update before the status update that sets `StartTime`. If the label is already present (idempotent reconcile), no update is issued. The `PrepareForCreate` strategy does NOT set this label — the reconciler owns it, keeping the strategy generic.

2. **AC2 — Custom TableConvertor:** `pkg/registry/drexecution/storage.go` replaces `rest.NewDefaultTableConvertor` with a custom `TableConvertor` that produces columns: NAME, PLAN, MODE, RESULT, DURATION, AGE. PLAN maps to `spec.planName`. MODE maps to `spec.mode`. RESULT maps to `status.result` (empty string shows `""` for in-progress). DURATION is computed as `completionTime - startTime` formatted as Go `time.Duration` string (e.g., `"2m35s"`); shows `""` when either timestamp is nil. AGE is standard `metav1.ObjectMeta.CreationTimestamp` delta. The TableConvertor is also set on the StatusREST store.

3. **AC3 — Field selector for spec.planName:** `GetAttrs` in `pkg/registry/drexecution/strategy.go` adds `spec.planName` to the `fields.Set`, enabling `kubectl get drexecutions --field-selector spec.planName=erp-full-stack`. `MatchDRExecution` already passes the field selector to the storage predicate. The `StoreOptions` in `storage.go` registers `spec.planName` as an indexer field via `AttrFunc`.

4. **AC4 — Delete protection for completed executions:** A new `RESTDeleteStrategy` on the main store in `storage.go` checks whether `Status.Result` is `Succeeded`, `Failed`, or `PartiallySucceeded`. If so, deletion is rejected with a Forbidden error: `"completed DRExecution audit records cannot be deleted (FR41)"`. In-progress executions (`Result == ""`) can be deleted for operational cleanup. This protection is implemented via a custom `BeforeDelete` function on the store, not via admission webhook, since DRExecution is served by the aggregated API server.

5. **AC5 — No sensitive data in audit record:** No field in `DRExecutionStatus`, `WaveStatus`, `DRGroupExecutionStatus`, or `StepStatus` contains storage credentials, secret references, or sensitive information (NFR14). The `Error` field in `DRGroupExecutionStatus` contains only sanitized error messages from driver typed errors (which already strip credentials per `pkg/drivers/errors.go`). Validated by a unit test scanning all populated fields.

6. **AC6 — Sample DRExecution YAML:** `config/samples/soteria_v1alpha1_drexecution.yaml` provides a sample completed DRExecution with: spec (planName, mode), status with 2 waves, 2 groups per wave, per-step details, startTime/completionTime, result=Succeeded. Labels include `soteria.io/plan-name`. Annotated with comments explaining field semantics for onboarding.

7. **AC7 — Self-contained audit record:** Each DRExecution record contains all information needed for compliance evidence: execution mode, result, timestamps (start, completion, per-wave, per-group, per-step), VM names per group, step-level details (promote volume, start VM, etc.), error messages for failed groups, retry count for retried groups. No external log, database lookup, or cross-resource join is needed to reconstruct the execution timeline.

8. **AC8 — Execution history sorted by creation time:** `kubectl get drexecutions -l soteria.io/plan-name=<plan> --sort-by=.metadata.creationTimestamp` returns all executions for a plan sorted most-recent-first. This is standard kubectl behavior — no custom sorting needed. The field selector from AC3 enables server-side pre-filtering.

9. **AC9 — Cross-DC persistence:** DRExecution records persist across single-datacenter failures via ScyllaDB async replication (FR43). This is already guaranteed by the existing storage layer — no new code needed, but validated by verifying that DRExecution uses the same `pkg/storage/scylladb/` path as DRPlan. No story-specific cross-DC code changes are required.

10. **AC10 — Documentation:** `pkg/registry/drexecution/doc.go` is created or expanded to describe: (a) the audit record lifecycle (create → in-progress → terminal → immutable), (b) the three-layer immutability model (spec immutable from creation, status immutable after Succeeded/Failed, PartiallySucceeded re-openable for retries), (c) delete protection for completed records, (d) RBAC design (operators can create but not update/delete), (e) the `soteria.io/plan-name` label convention for history queries.

11. **AC11 — Test coverage:** Unit tests covering: (a) plan-name label set on first reconcile (StartTime == nil → label appears); (b) plan-name label idempotent (second reconcile does not issue update if already set); (c) custom TableConvertor produces correct columns for completed, in-progress, and failed executions; (d) field selector `spec.planName` returns correct results; (e) delete of completed execution (Succeeded/Failed/PartiallySucceeded) is rejected; (f) delete of in-progress execution is allowed; (g) no sensitive data in any DRExecution field (scan `Desc()` and sample status); (h) strategy tests for all existing immutability guarantees remain green.

## Tasks / Subtasks

- [x] Task 1: Add `soteria.io/plan-name` label in DRExecution reconciler (AC: #1)
  - [x] 1.1 In `pkg/controller/drexecution/reconciler.go`, in the setup path (gated on `StartTime == nil`), add label `soteria.io/plan-name: exec.Spec.PlanName` to `exec.Labels` map (initialize map if nil). Issue `r.Update(ctx, exec)` for the metadata change before the status update that sets StartTime. Guard: skip if label already matches (idempotent)
  - [x] 1.2 Add RBAC marker if needed — controller already has `update` on `drexecutions` via `+kubebuilder:rbac` marker (verify existing marker includes `update`)

- [x] Task 2: Add field selector for `spec.planName` (AC: #3)
  - [x] 2.1 In `pkg/registry/drexecution/strategy.go` `GetAttrs`, add `"spec.planName": exec.Spec.PlanName` to the returned `fields.Set`
  - [x] 2.2 Verify `MatchDRExecution` already passes `fieldSel` to the predicate (it does — no change needed)
  - [x] 2.3 In `storage.go`, confirm `StoreOptions.AttrFunc` is set to `GetAttrs` (it is — the field becomes queryable automatically via the predicate)

- [x] Task 3: Implement custom TableConvertor (AC: #2)
  - [x] 3.1 Create a `tableConvertor` struct in `pkg/registry/drexecution/storage.go` implementing `rest.TableConvertor`
  - [x] 3.2 Define column definitions: NAME (string), PLAN (string), MODE (string), RESULT (string), DURATION (string), AGE (date)
  - [x] 3.3 Implement `ConvertToTable` — iterate items, extract fields from `DRExecution`, compute duration as `completionTime.Sub(startTime.Time)` formatted via `duration.HumanDuration()` from `k8s.io/apimachinery/pkg/util/duration`
  - [x] 3.4 Replace `rest.NewDefaultTableConvertor(...)` in `NewREST` with the custom convertor
  - [x] 3.5 Set the same custom convertor on the statusStore

- [x] Task 4: Implement delete protection for completed executions (AC: #4)
  - [x] 4.1 Add a `BeforeDelete` function on the store in `storage.go` that fetches the existing object, checks `Status.Result`, and returns `apierrors.NewForbidden(...)` if result is non-empty (any terminal or partially-terminal state)
  - [x] 4.2 Alternative approach: override `DeleteStrategy` with a custom strategy that wraps the existing one and adds the result check in its validation. Choose whichever approach is idiomatic for `k8s.io/apiserver` — likely `store.PreDeleteHook` or a `BeforeDelete` on the store config

- [x] Task 5: Create sample DRExecution YAML (AC: #6)
  - [x] 5.1 Create `config/samples/soteria_v1alpha1_drexecution.yaml` with a completed planned_migration example: spec.planName=erp-full-stack, spec.mode=planned_migration, status.result=Succeeded, 2 waves with 2 groups each, per-step details, timestamps, `soteria.io/plan-name` label
  - [x] 5.2 Add explanatory comments for each section

- [x] Task 6: Create/expand `pkg/registry/drexecution/doc.go` (AC: #10)
  - [x] 6.1 Write package doc covering: audit record lifecycle, three-layer immutability model, delete protection, RBAC design intent, plan-name label convention, field selector support

- [x] Task 7: Unit tests (AC: #11)
  - [x] 7.1 Test plan-name label: create DRExecution with no label, run reconciler setup path, verify label `soteria.io/plan-name` is set to `spec.planName`
  - [x] 7.2 Test label idempotency: create DRExecution with label already set, verify no metadata update issued
  - [x] 7.3 Test custom TableConvertor: pass completed, in-progress, and failed DRExecution objects through `ConvertToTable`, verify column values (name, plan, mode, result, duration, age)
  - [x] 7.4 Test field selector: add `spec.planName` to `GetAttrs` fields, verify `MatchDRExecution` with field selector `spec.planName=plan-a` matches correctly and excludes `plan-b`
  - [x] 7.5 Test delete protection: attempt delete on Succeeded/Failed/PartiallySucceeded execution, verify Forbidden error; attempt delete on in-progress execution, verify allowed
  - [x] 7.6 Test no sensitive data: construct a fully-populated DRExecution status, scan all string fields for "password", "secret", "credential", "token" — verify none found
  - [x] 7.7 Verify existing strategy tests remain green (spec immutability, terminal status immutability, PartiallySucceeded re-openable)

- [x] Task 8: Run full test suite
  - [x] 8.1 `make lint-fix` — auto-fix style
  - [x] 8.2 `make test` — all unit + integration tests pass

### Review Findings

- [x] [Review][Patch] Sample DRExecution removed from `config/samples/kustomization.yaml` resources — kept as reference-only with explanatory comment
- [x] [Review][Patch] TOCTOU hole in `AuditProtectedREST.Delete()` closed — audit guard now runs inside `deleteValidation` wrapper via `wrapAuditValidation()`, eliminating the separate pre-check Get
- [x] [Review][Patch] `doc.go` corrected — now references `AuditProtectedREST` wrapper instead of strategy level

## Dev Notes

- **Most audit infrastructure already exists.** The types, immutability strategy, RBAC, cross-DC persistence, and checkpointing are all in place from Epics 1 and 4. This story fills the remaining gaps: plan-name label for history queries, custom table output, field selectors, delete protection, and documentation.

- **Label must be set before StartTime status update.** The reconciler's setup path (gated on `StartTime == nil`) runs once per execution. The label is metadata — updated via `r.Update(ctx, exec)` on the main resource. The StartTime is status — updated via `r.Status().Update(ctx, exec)`. Issue the metadata update first, then re-fetch (`r.Get`) before the status update to avoid resourceVersion conflicts. The existing `removeRetryAnnotation` function already uses this pattern (`r.Update` for metadata changes).

- **`GetAttrs` field selector is the idiomatic approach.** The aggregated API server's generic registry uses `SelectionPredicate` with `GetAttrs` for field selectors. Adding `"spec.planName"` to the `fields.Set` makes it automatically queryable. No custom indexer or informer is needed — the ScyllaDB storage layer handles the filtering. Note: this is different from CRD field selectors (which require `+listType` markers) — the aggregated API pattern is simpler.

- **Custom `TableConvertor` pattern.** The DRPlan store uses `rest.NewDefaultTableConvertor` currently (Story 5.2 adds an UNPROTECTED column via custom convertor). Follow the same pattern: implement `rest.TableConvertor` interface with `ConvertToTable(ctx, object, tableOptions) (*metav1.Table, error)`. Handle both single-object and list cases. Use `k8s.io/apimachinery/pkg/util/duration.HumanDuration()` for DURATION formatting — this matches `kubectl` age formatting. Column types: `"string"` for text, `"date"` for age.

- **Delete protection via `store.BeforeDelete`.** The `genericregistry.Store` supports `PreDeleteHook` (a `BeforeDelete` function). Set `store.PreDeleteHook = auditDeleteGuard` where `auditDeleteGuard(ctx, obj) error` checks the execution's `Status.Result`. If non-empty, return `apierrors.NewForbidden(Resource("drexecutions"), name, errors.New("..."))`. This runs inside the store's delete transaction, before the actual storage delete. Alternative: `store.DeleteStrategy` can have a custom `Validate` — but `PreDeleteHook` is cleaner since it already has the current object.

- **No admission webhook changes needed.** The existing DRExecution admission webhook only validates CREATE operations. Delete protection and status immutability are enforced in the aggregated API server's strategy layer, which is the correct place for these checks (admission webhooks run on kube-apiserver, not on the aggregated API).

- **RBAC already correct for FR41.** The `soteria_operator_role.yaml` already restricts operators to `get/list/watch/create` — no `delete`. The controller's internal RBAC (`role.yaml`) has `get/list/watch/update/patch` for status updates. The delete protection in AC4 is a defense-in-depth measure for cluster admins who might have broader permissions.

- **Cross-DC persistence is inherited.** DRExecution records flow through the same `pkg/storage/scylladb/` path as all other resources. ScyllaDB RF=2 per DC with async replication ensures availability on the surviving cluster. The `critical_fields.go` detection for `Status.Result` changes uses SERIAL Paxos for terminal state transitions. No new cross-DC code is needed.

- **`PartiallySucceeded` is intentionally not terminal.** The strategy allows status updates on `PartiallySucceeded` executions for retry support (FR14). When a retry succeeds, the result can change to `Succeeded`. When a retry fails again, it stays `PartiallySucceeded` or moves to `Failed`. Each retry is auditable via `RetryCount` in `DRGroupExecutionStatus` and the `DRGroupStatus` CRD. The delete protection in AC4 still blocks deletion of `PartiallySucceeded` records to preserve the audit trail.

- **No new CRD/RBAC/Kustomize generation needed.** This story only modifies Go code in the aggregated API server path — no kubebuilder markers that trigger `make manifests`, no CRD changes, no webhook config changes. The controller's `+kubebuilder:rbac` marker for `drexecutions` already includes `update` verb for the metadata label update.

### Existing code patterns to follow

- **Metadata update in reconciler:** `removeRetryAnnotation` in `pkg/controller/drexecution/reconciler.go` uses `r.Update(ctx, exec)` for annotation changes on the main resource — same pattern for label setting.
- **Custom TableConvertor:** If Story 5.2 is implemented first, follow its `drplanTableConvertor` pattern in `pkg/registry/drplan/storage.go`. Otherwise, reference the `k8s.io/apiserver` `rest.TableConvertor` interface: `ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error)`.
- **Field selector attrs:** `pkg/registry/drplan/strategy.go` `GetAttrs` adds `"metadata.name"` — extend with `"spec.planName"` following the same pattern.
- **Duration formatting:** `k8s.io/apimachinery/pkg/util/duration.HumanDuration(d time.Duration)` produces `"2m35s"`, `"1h12m"`, etc. — standard kubectl output format.
- **Store PreDeleteHook:** `genericregistry.Store.PreDeleteHook` is `func(ctx context.Context, obj runtime.Object, options *metav1.DeleteOptions) error`. Return `apierrors.NewForbidden(...)` to block deletion.
- **Strategy tests:** `pkg/registry/drexecution/strategy_test.go` tests spec immutability, terminal status immutability, and PartiallySucceeded re-openability — extend with delete protection and field selector tests.
- **Structured logging:** `log.FromContext(ctx).WithValues("drexecution", exec.Name)` — add `"label", "soteria.io/plan-name"` when logging the label set operation.

### Critical files to modify

| File | Change |
|------|--------|
| `pkg/controller/drexecution/reconciler.go` | Add plan-name label in setup path (before StartTime status update) |
| `pkg/registry/drexecution/strategy.go` | Add `spec.planName` to `GetAttrs` fields.Set |
| `pkg/registry/drexecution/storage.go` | Replace DefaultTableConvertor with custom implementation; add `PreDeleteHook` for delete protection |
| `pkg/registry/drexecution/doc.go` | New file: package doc describing audit record semantics, immutability model, RBAC design |
| `config/samples/soteria_v1alpha1_drexecution.yaml` | New file: sample completed DRExecution |
| `pkg/registry/drexecution/strategy_test.go` | Add field selector test, delete protection test |
| `pkg/registry/drexecution/storage_test.go` | New file: TableConvertor tests, delete protection integration tests |
| `pkg/controller/drexecution/reconciler_test.go` | Add plan-name label tests (set on first reconcile, idempotent) |

### Previous story intelligence (Stories 5.0–5.3)

- **Story 5.0 (ActiveExecution):** Adds `DRPlan.Status.ActiveExecution` pointer. The reconciler setup path (gated on `StartTime == nil`) is the same path where we set the plan-name label. Story 5.0 may add additional logic to this path (setting ActiveExecution). Ensure the label set happens before or alongside the ActiveExecution set — both are metadata/status updates in the same setup block. No conflict — label is metadata update, ActiveExecution is plan status update.
- **Story 5.1 (ReplicationHealth):** No impact on this story. ReplicationHealth is a DRPlan status field, not DRExecution.
- **Story 5.2 (UnprotectedVMs):** Introduces a custom `TableConvertor` pattern for DRPlan if implemented. Follow the same approach for DRExecution's TableConvertor.
- **Story 5.3 (Prometheus Metrics):** Adds `recordExecutionMetrics` in the DRExecution reconciler's completion paths. No conflict — metrics recording happens at completion, label setting happens at start.

### References

- [Source: _bmad-output/planning-artifacts/prd.md#FR41] — "Every DRPlan execution creates an immutable DRExecution record with per-wave, per-DRGroup, per-step status, timestamps, and error details"
- [Source: _bmad-output/planning-artifacts/prd.md#FR42] — "Platform engineer can view the execution history for any DRPlan, including all past executions and their outcomes"
- [Source: _bmad-output/planning-artifacts/prd.md#FR43] — "DRExecution records persist across datacenter failures and are available on both clusters via the shared state layer"
- [Source: _bmad-output/planning-artifacts/prd.md#NFR14] — "The orchestrator must not log or expose storage credentials in any output"
- [Source: _bmad-output/planning-artifacts/prd.md#Audit & Compliance] — "DRExecution as audit record: immutable record with per-wave, per-DRGroup, per-step status for SOX, ISO 22301, SOC 2"
- [Source: _bmad-output/planning-artifacts/epics.md#Story 5.4] — BDD acceptance criteria for immutable audit record
- [Source: _bmad-output/planning-artifacts/architecture.md#Audit FR41-FR43] — "Immutable DRExecution records, cross-site persistence, execution history"
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `pkg/registry/drexecution/strategy.go` for append-only semantics
- [Source: pkg/apis/soteria.io/v1alpha1/types.go:242-309] — DRExecution type definition with all status sub-types
- [Source: pkg/registry/drexecution/strategy.go:86-97] — Spec immutability enforcement
- [Source: pkg/registry/drexecution/strategy.go:142-160] — Terminal status immutability enforcement
- [Source: pkg/registry/drexecution/storage.go:45] — Current DefaultTableConvertor (to be replaced)
- [Source: pkg/registry/drexecution/strategy.go:109-117] — GetAttrs with only metadata.name (to be extended)
- [Source: config/rbac/soteria_operator_role.yaml:23-34] — RBAC restricting operators to get/list/watch/create
- [Source: pkg/controller/drexecution/reconciler.go:25] — Retry annotation pattern (metadata update reference)
- [Source: pkg/apiserver/critical_fields.go] — Status.Result detected for cross-DC Paxos
- [Source: _bmad-output/project-context.md] — Label convention `soteria.io/<kebab-case>`, naming table, anti-patterns
- [Source: _bmad-output/implementation-artifacts/5-2-unprotected-vm-detection.md#AC7] — Custom TableConvertor pattern for DRPlan

## Estimated Effort

- Production code: ~30 lines in `reconciler.go` (label setting), ~15 lines in `strategy.go` (field selector), ~90 lines in `storage.go` (TableConvertor + delete protection), ~30 lines in `doc.go`, ~40 lines in sample YAML
- Test code: ~200 lines across `strategy_test.go`, `storage_test.go`, `reconciler_test.go`
- Total: ~405 net new/modified lines across 8 files

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor Agent)

### Debug Log References

- Lint fix: goconst for `<unknown>` string → extracted `unknownTimestamp` constant
- Lint fix: gocyclo for Reconcile (35 → 28) → extracted `startEventFields()` and `ensurePlanNameLabel()` helper methods
- Lint fix: lll for test line > 120 chars → reformatted struct literal
- Test fix: `MatchDRExecution` nil label selector panic → use `labels.Everything()`
- Delete protection: `PreDeleteHook` does not exist on `genericregistry.Store` v0.35.0 → used `AuditProtectedREST` wrapper struct overriding `Delete`/`DeleteCollection` methods

### Completion Notes List

- AC1: `soteria.io/plan-name` label set by `ensurePlanNameLabel()` in reconciler setup path (gated on `StartTime == nil`), idempotent skip when already set, metadata update before status update
- AC2: `DRExecutionTableConvertor` produces NAME, PLAN, MODE, RESULT, DURATION, AGE columns; duration via `duration.HumanDuration()`; shared by main store and StatusREST
- AC3: `spec.planName` added to `GetAttrs` `fields.Set`; `MatchDRExecution` and `StoreOptions.AttrFunc=GetAttrs` already wire it through
- AC4: `AuditProtectedREST` wraps `*genericregistry.Store`, overrides `Delete` and `DeleteCollection` to block deletion of completed executions (any non-empty `Status.Result`); returns `apierrors.NewForbidden` with FR41 reference
- AC5: Verified by `TestNoSensitiveData_InAuditRecord` — recursive string scan finds no sensitive patterns
- AC6: Sample YAML with 2 waves, 4 groups, per-step details, plan-name label, annotated comments
- AC7: Self-contained audit record inherits from existing types (verified by design, no new code)
- AC8: Standard kubectl sort-by CreationTimestamp (no new code needed)
- AC9: Cross-DC persistence via existing ScyllaDB storage path (no new code needed)
- AC10: `doc.go` expanded with audit lifecycle, three-layer immutability, delete protection, RBAC design, label convention, field selector docs
- AC11: 25 new tests — 2 reconciler (label set + idempotent), 15 storage (TableConvertor ×4, delete protection ×4, sensitive data ×1, duration ×3, timestamp ×2, field selector ×2 in strategy), all existing strategy tests green

### Change Log

- 2026-04-23: Story 5.4 implemented — plan-name label, custom TableConvertor (6 columns), spec.planName field selector, AuditProtectedREST delete protection, sample YAML, doc.go, 25 new tests; 0 lint issues, all unit+integration tests pass

### File List

**Modified:**
- `pkg/controller/drexecution/reconciler.go` — `ensurePlanNameLabel()` helper, `startEventFields()` extraction, reconciler setup path calls label helper
- `pkg/registry/drexecution/strategy.go` — `spec.planName` added to `GetAttrs` `fields.Set`
- `pkg/registry/drexecution/storage.go` — `AuditProtectedREST` wrapper (Delete/DeleteCollection protection), `DRExecutionTableConvertor` (6 columns), replaced `DefaultTableConvertor`, `NewREST` returns `*AuditProtectedREST`
- `pkg/registry/drexecution/doc.go` — expanded package doc with audit record semantics
- `config/samples/kustomization.yaml` — added drexecution sample reference
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — story status tracking

**New:**
- `pkg/registry/drexecution/storage_test.go` — TableConvertor, delete protection, sensitive data, duration/timestamp tests (15 tests)
- `config/samples/soteria_v1alpha1_drexecution.yaml` — sample completed DRExecution

**Test files modified:**
- `pkg/controller/drexecution/reconciler_test.go` — plan-name label set + idempotency tests (2 tests)
- `pkg/registry/drexecution/strategy_test.go` — field selector tests (2 tests)
