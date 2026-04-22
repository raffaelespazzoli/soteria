# Story 4.9: Site Topology Fields for DRPlan

Status: done

## Story

As a platform engineer,
I want the DRPlan to declare which cluster is the primary site and which is the secondary site, and track which site currently owns the active workloads,
So that the 8-phase lifecycle state is unambiguous and I can determine failover direction from the API alone.

## Background / ADR

The DRPlan's 8-phase state machine uses directional names (`SteadyState` vs `DRedSteadyState`, `FailingOver` vs `FailingBack`) that are meaningful only relative to an original primary/secondary cluster assignment. Today, no field records this topology. An observer reading `phase: SteadyState` cannot determine whether VMs are on the original primary or have been failed back — the phase name is ambiguous without a directional anchor.

**Decision:** Add `primarySite` and `secondarySite` to `DRPlanSpec` (user-declared, immutable), and `activeSite` to `DRPlanStatus` (controller-managed, flipped on failover/failback completion). No backward compatibility — `v1alpha1` is pre-GA, existing DRPlans must be deleted and recreated.

**Invariants:**

| Phase | ActiveSite |
|-------|-----------|
| `SteadyState` | == `primarySite` |
| `FailingOver` | == `primarySite` (in-flight) |
| `FailedOver` | == `secondarySite` |
| `Reprotecting` | == `secondarySite` (in-flight) |
| `DRedSteadyState` | == `secondarySite` |
| `FailingBack` | == `secondarySite` (in-flight) |
| `FailedBack` | == `primarySite` |
| `ReprotectingBack` | == `primarySite` (in-flight) |

`ActiveSite` flips at exactly two completion points:
1. `CompleteTransition(FailingOver)` -> `FailedOver`: flip primary -> secondary
2. `CompleteTransition(FailingBack)` -> `FailedBack`: flip secondary -> primary

## Acceptance Criteria

1. **AC1 — Spec fields:** `DRPlanSpec` has two new required string fields: `primarySite` and `secondarySite`. Both are validated as non-empty and not equal to each other. Both are immutable after creation (rejected on update if changed).

2. **AC2 — Status field:** `DRPlanStatus` has a new string field `activeSite`. On creation, the registry strategy sets `activeSite = primarySite`. The field is controller-managed — users cannot set it via the spec subresource.

3. **AC3 — Validation:** `ValidateDRPlan` rejects: (a) empty `primarySite`, (b) empty `secondarySite`, (c) `primarySite == secondarySite`. `ValidateDRPlanUpdate` additionally rejects changes to `primarySite` or `secondarySite` (immutable fields).

4. **AC4 — Admission webhook:** The admission webhook in `pkg/admission/drplan_validator.go` passes through to `ValidateDRPlan`/`ValidateDRPlanUpdate` — no additional webhook logic needed beyond what the validation functions provide.

5. **AC5 — ActiveSite flip on failover completion:** When the execution engine calls `CompleteTransition` and the transition is `FailingOver -> FailedOver`, the DRPlan's `status.activeSite` is set to `secondarySite`. When the transition is `FailingBack -> FailedBack`, `status.activeSite` is set to `primarySite`. These are the ONLY two points where `activeSite` changes. Reprotect transitions (`Reprotecting -> DRedSteadyState`, `ReprotectingBack -> SteadyState`) do NOT change `activeSite`.

6. **AC6 — Critical field detection:** `detectDRPlanCriticalFields` in `pkg/apiserver/critical_fields.go` detects `activeSite` changes in addition to `phase` changes, triggering SERIAL Paxos for cross-DC safety.

7. **AC7 — Preflight report:** The `PreflightReport` includes `primarySite`, `secondarySite`, and `activeSite` so operators see the failover direction before triggering execution.

8. **AC8 — State machine helper:** A new `ActiveSiteForPhase(phase, primarySite, secondarySite) string` function in `pkg/engine/statemachine.go` returns the expected `activeSite` for any given phase. Used for invariant assertions in tests.

9. **AC9 — Sample manifests and test script:** `config/samples/soteria_v1alpha1_drplan.yaml` and `hack/stretched-local-test.sh` are updated to include `primarySite`/`secondarySite` in all DRPlan definitions.

10. **AC10 — Test fixture updates:** All existing test files that create `DRPlan` objects are updated to include `primarySite` and `secondarySite` fields. A test helper `newTestDRPlan(name, primary, secondary string) *v1alpha1.DRPlan` is added to reduce boilerplate.

11. **AC11 — New unit tests:** Tests covering: (a) validation rejects missing/equal sites; (b) immutability on update; (c) `activeSite` defaults to `primarySite` on create; (d) `activeSite` flips to `secondarySite` after failover completion; (e) `activeSite` flips to `primarySite` after failback completion; (f) `activeSite` does NOT change on reprotect completion; (g) `ActiveSiteForPhase` returns correct site for all 8 phases; (h) critical field detector triggers on `activeSite` change.

## Tasks / Subtasks

- [ ] Task 1: Update API types (AC: #1, #2)
  - [ ] 1.1 Add `PrimarySite string` and `SecondarySite string` to `DRPlanSpec` in `pkg/apis/soteria.io/v1alpha1/types.go` with `json:"primarySite"` and `json:"secondarySite"` tags
  - [ ] 1.2 Add `ActiveSite string` to `DRPlanStatus` with `json:"activeSite,omitempty"` tag and doc comment
  - [ ] 1.3 Run `make generate` to regenerate `zz_generated.deepcopy.go` and `zz_generated.openapi.go`

- [ ] Task 2: Update validation (AC: #3)
  - [ ] 2.1 In `ValidateDRPlan`: validate `primarySite` required, `secondarySite` required, `primarySite != secondarySite`
  - [ ] 2.2 In `ValidateDRPlanUpdate`: validate `primarySite` and `secondarySite` are immutable (compare old vs new)
  - [ ] 2.3 Add unit tests in `validation_test.go`: missing primary, missing secondary, equal sites, immutable on update

- [ ] Task 3: Update registry strategy (AC: #2)
  - [ ] 3.1 In `PrepareForCreate`: set `plan.Status.ActiveSite = plan.Spec.PrimarySite`
  - [ ] 3.2 Add unit tests in `strategy_test.go`: verify `activeSite` is set on create, verify spec preserved on status update

- [ ] Task 4: Add state machine helper (AC: #8)
  - [ ] 4.1 Add `ActiveSiteForPhase(phase, primarySite, secondarySite string) string` to `pkg/engine/statemachine.go`
  - [ ] 4.2 Add unit tests in `statemachine_test.go`: all 8 phases return correct site

- [ ] Task 5: Wire activeSite flip in execution engine (AC: #5)
  - [ ] 5.1 In `pkg/engine/executor.go` (or `pkg/controller/drexecution/reconciler.go`): after `CompleteTransition` for `FailingOver -> FailedOver`, set `plan.Status.ActiveSite = plan.Spec.SecondarySite`
  - [ ] 5.2 After `CompleteTransition` for `FailingBack -> FailedBack`, set `plan.Status.ActiveSite = plan.Spec.PrimarySite`
  - [ ] 5.3 Verify reprotect transitions do NOT flip `activeSite`
  - [ ] 5.4 Add unit tests in `executor_test.go`: activeSite after failover, failback, and reprotect (no change)

- [ ] Task 6: Update critical field detection (AC: #6)
  - [ ] 6.1 In `detectDRPlanCriticalFields`: add `|| oldPlan.Status.ActiveSite != newPlan.Status.ActiveSite`
  - [ ] 6.2 Add unit test: activeSite change triggers critical field detection

- [ ] Task 7: Update preflight report (AC: #7)
  - [ ] 7.1 Add `PrimarySite`, `SecondarySite`, `ActiveSite` string fields to `PreflightReport` in `types.go`
  - [ ] 7.2 Populate them in `internal/preflight/checks.go` `ComposeReport`
  - [ ] 7.3 Add unit test in `checks_test.go`: verify sites appear in report

- [ ] Task 8: Update admission webhook (AC: #4)
  - [ ] 8.1 Verify `pkg/admission/drplan_validator.go` delegates to `ValidateDRPlan`/`ValidateDRPlanUpdate` — no changes needed if so
  - [ ] 8.2 Add integration test in `drplan_webhook_test.go`: create with missing sites rejected, update with changed sites rejected

- [ ] Task 9: Update all test fixtures (AC: #10)
  - [ ] 9.1 Add `newTestDRPlan` helper to `test/integration/controller/suite_test.go` (or shared test util)
  - [ ] 9.2 Update all DRPlan literals in: `validation_test.go`, `strategy_test.go`, `drplan_validator_test.go`, `drexecution_validator_test.go`, `vm_validator_test.go`, `reconciler_test.go` (drplan), `reconciler_test.go` (drexecution), `executor_test.go`, `failover_test.go`, `reprotect_test.go`, `statemachine_test.go`, `chunker_test.go`, `checkpoint_test.go`, `consistency_test.go`, `discovery_test.go`, `resume_test.go`, `checks_test.go`, `storage_test.go`
  - [ ] 9.3 Update integration tests: `drplan_test.go`, `drplan_consistency_test.go`, `drplan_preflight_test.go`, `drexecution_test.go`, `drplan_webhook_test.go`, `vm_webhook_test.go`, `apiserver_test.go`, `replication_test.go`, `store_test.go`, `watch_test.go`

- [ ] Task 10: Update samples and scripts (AC: #9)
  - [ ] 10.1 Update `config/samples/soteria_v1alpha1_drplan.yaml` with `primarySite`/`secondarySite`
  - [ ] 10.2 Update `config/samples/vm_with_drplan_label.yaml` comment if needed
  - [ ] 10.3 Update `hack/stretched-local-test.sh`: all DRPlan heredocs (`finance-dr`, `payments-dr`, `fedora-app`) get `primarySite`/`secondarySite`

- [ ] Task 11: Run full test suite (AC: #11)
  - [ ] 11.1 `make generate` — regenerate deepcopy + openapi
  - [ ] 11.2 `make manifests` — regenerate CRDs if markers changed
  - [ ] 11.3 `make lint-fix` — auto-fix style
  - [ ] 11.4 `make test` — all unit + integration tests pass

### Review Findings

- [x] [Review][Patch] Add the missing shared `newTestDRPlan` helper required by AC10 [`test/integration/controller/suite_test.go:388`]
- [x] [Review][Patch] Add webhook integration coverage for missing/equal site validation and site immutability [`test/integration/admission/drplan_webhook_test.go:76`]
- [x] [Review][Patch] Add coverage proving reprotect completion preserves `activeSite` as required by AC11(f) [`pkg/engine/statemachine_test.go`]

## Dev Notes

- **No backward compat**: `v1alpha1` is pre-GA. Existing DRPlans must be deleted and recreated with the new fields. No migration script.
- **No defaulting webhook**: Defaults are applied in registry strategy (`PrepareForCreate`), not a mutating webhook. This is the existing pattern.
- **`activeSite` is SERIAL-guarded**: Changes to `activeSite` coincide with `phase` transitions which are already SERIAL Paxos. Adding explicit detection in `critical_fields.go` is defense-in-depth.
- **Fixture churn is the bulk of the work**: ~23 test files need `PrimarySite`/`SecondarySite` added to every DRPlan literal. Use search-and-replace patterns.
- **Site values are opaque strings**: No validation against known DCs. The system does not currently have a cluster registry. Values like `"etl6"`, `"etl7"`, `"dc-west"` are all valid as long as primary != secondary.

## Estimated Effort

- Production code: ~120 lines across 14 files
- Test code: ~515 lines across 23+ files
- Total: ~635 net new lines
