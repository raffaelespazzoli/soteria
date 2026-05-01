# Story 8.3: Cross-Site VM Agreement & Plan Readiness Gating

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want plan validation and wave formation to require both sites to agree on the discovered VM set,
So that executions never proceed against inconsistent infrastructure where VMs are missing from one site.

## Acceptance Criteria

1. **AC1 — VM set comparison:** The active-site reconciler compares `primarySiteDiscovery.vms` and `secondarySiteDiscovery.vms` by `{name, namespace}` tuples (order-independent). If identical, wave formation proceeds as before.

2. **AC2 — SitesInSync condition True:** When VM sets agree, a condition `SitesInSync` is set to `True` with reason `VMsAgreed`. The `Ready` condition depends on `SitesInSync` among other checks.

3. **AC3 — SitesInSync condition False (mismatch):** When VM sets differ, `SitesInSync` is `False` with reason `VMsMismatch`. The condition message lists the delta: "VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]". `status.waves` is cleared, and `Ready` transitions to `False` with message "Plan blocked: sites do not agree on VM inventory".

4. **AC4 — Empty discovery on one site:** If one site's `SiteDiscovery` has zero VMs while the other has VMs, `SitesInSync` is `False`. The message indicates the empty-discovery site and suggests checking VM labels.

5. **AC5 — Waiting for discovery:** If one site's `SiteDiscovery` is nil (not yet populated, `lastDiscoveryTime` is zero), `SitesInSync` is `False` with reason `WaitingForDiscovery`. The message indicates which site has not yet reported. This is the expected initial state.

6. **AC6 — Admission webhook rejects executions when out of sync:** The DRExecution admission webhook rejects CREATE when `SitesInSync` is `False` with error: "Cannot start execution: sites do not agree on VM inventory. Resolve VM differences first." Applies to all execution modes.

7. **AC7 — Preflight report enrichment:** The preflight report includes a new `sitesInSync` boolean field and a `siteDiscoveryDelta` string field. The preflight warnings section lists any VM mismatches.

8. **AC8 — All tests pass:** Table-driven tests cover: both agree, primary-only VMs, secondary-only VMs, both-side extra VMs, one side empty, one side not yet discovered. Admission webhook tests verify rejection when `SitesInSync` is `False`. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Add `SitesInSync` condition constants and comparison logic (AC: #1, #2, #3, #4, #5)
  - [ ] 1.1 In `pkg/controller/drplan/reconciler.go`, add condition type and reason constants:
    - `conditionTypeSitesInSync = "SitesInSync"`
    - `reasonVMsAgreed = "VMsAgreed"`
    - `reasonVMsMismatch = "VMsMismatch"`
    - `reasonWaitingForDiscovery = "WaitingForDiscovery"`
  - [ ] 1.2 Create a new function `compareSiteDiscovery(primary, secondary *soteriav1alpha1.SiteDiscovery) (inSync bool, condition metav1.Condition)` that:
    - Returns `WaitingForDiscovery` if either pointer is nil or `LastDiscoveryTime.IsZero()`
    - Builds `map[string]struct{}` from `{namespace}/{name}` for each side
    - Returns `VMsAgreed` if sets are identical
    - Returns `VMsMismatch` with delta message listing "VMs on primary but not secondary: [...]; VMs on secondary but not primary: [...]"
    - Returns `VMsMismatch` with specific message if one side has zero VMs ("Site <name> has no discovered VMs; check VM labels")
  - [ ] 1.3 Add `reasonSitesOutOfSync = "SitesOutOfSync"` for the Ready condition message when blocked

- [ ] Task 2: Integrate agreement check into reconcile flow (AC: #1, #2, #3, #4, #5)
  - [ ] 2.1 In the `Reconcile` method, after the site-aware gate (line ~143) and before VM discovery (line ~147), add the agreement check:
    - Only evaluate when `r.LocalSite != ""` (site-aware mode)
    - Call `compareSiteDiscovery(plan.Status.PrimarySiteDiscovery, plan.Status.SecondarySiteDiscovery)`
    - Set the `SitesInSync` condition on the plan status via `meta.SetStatusCondition`
    - If `inSync == false` AND reason is `VMsMismatch`: clear `status.waves`, set `Ready=False` with message "Plan blocked: sites do not agree on VM inventory", emit warning event, return with requeue
    - If `inSync == false` AND reason is `WaitingForDiscovery`: proceed with discovery as normal (don't block — the active site populates its own SiteDiscovery during reconcile; blocking before that would deadlock on first deploy)
    - If `inSync == true`: proceed with normal wave formation
  - [ ] 2.2 When `SitesInSync` transitions from `True` to `False`, emit event: `"Warning", "SitesOutOfSync", <delta message>`
  - [ ] 2.3 When `SitesInSync` transitions from `False` to `True`, emit event: `"Normal", "SitesInSync", "Both sites agree on VM inventory"`
  - [ ] 2.4 Pass the `SitesInSync` condition through to `updateStatus` alongside the `Ready` condition. Modify `updateStatus` signature or call `meta.SetStatusCondition` before the existing patch logic

- [ ] Task 3: Update `updateStatus` to handle `SitesInSync` condition (AC: #2, #3)
  - [ ] 3.1 The `SitesInSync` condition must be set in the same status patch as the `Ready` condition. Add a parameter or variadic condition list to `updateStatus`, OR set it before calling `updateStatus` on the plan object that will be patched
  - [ ] 3.2 Add change detection for `SitesInSync` to the `anyChanged` logic so that condition transitions trigger a patch

- [ ] Task 4: Admission webhook — reject when SitesInSync is False (AC: #6)
  - [ ] 4.1 In `pkg/admission/drexecution_validator.go`, after the phase transition check (~line 108), add:
    - Find `SitesInSync` condition in `plan.Status.Conditions` via `meta.FindStatusCondition`
    - If found AND `Status == metav1.ConditionFalse`: return `admission.Denied("Cannot start execution: sites do not agree on VM inventory. Resolve VM differences first.")`
  - [ ] 4.2 Import `"k8s.io/apimachinery/pkg/api/meta"` (or use loop-based find since `api/meta` has the helper)

- [ ] Task 5: Preflight report enrichment (AC: #7)
  - [ ] 5.1 Add `SitesInSync bool` field to `PreflightReport` in `pkg/apis/soteria.io/v1alpha1/types.go`
  - [ ] 5.2 Add `SiteDiscoveryDelta string` field to `PreflightReport` (omitempty)
  - [ ] 5.3 In `composePreflightReport`, populate `SitesInSync` from the condition and `SiteDiscoveryDelta` from the condition message when `SitesInSync` is False
  - [ ] 5.4 Add a preflight warning when sites are out of sync
  - [ ] 5.5 Run `make generate` after type changes

- [ ] Task 6: Backward compatibility (AC: #1, #8)
  - [ ] 6.1 When `r.LocalSite == ""` (no `--site-name`), skip the entire agreement check — SitesInSync condition is not set, wave formation proceeds as before
  - [ ] 6.2 When `SiteDiscovery` fields are both nil (legacy plan without site-aware discovery), skip agreement check — existing plans without discovery data are not blocked

- [ ] Task 7: Unit tests (AC: #8)
  - [ ] 7.1 `TestCompareSiteDiscovery_BothAgree` — identical VM sets → `VMsAgreed`, inSync=true
  - [ ] 7.2 `TestCompareSiteDiscovery_PrimaryOnlyVMs` — extra VMs on primary → `VMsMismatch` with delta message listing primary-only
  - [ ] 7.3 `TestCompareSiteDiscovery_SecondaryOnlyVMs` — extra VMs on secondary → `VMsMismatch` with delta message
  - [ ] 7.4 `TestCompareSiteDiscovery_BothSideExtras` — extra VMs on both → message lists both directions
  - [ ] 7.5 `TestCompareSiteDiscovery_OneSideEmpty` — one side zero VMs → specific empty-site message
  - [ ] 7.6 `TestCompareSiteDiscovery_OneSideNil` — nil SiteDiscovery → `WaitingForDiscovery`
  - [ ] 7.7 `TestCompareSiteDiscovery_BothNil` — both nil → `WaitingForDiscovery`
  - [ ] 7.8 `TestReconcile_SitesInSync_WaveFormationProceeds` — agreement passes, waves formed normally
  - [ ] 7.9 `TestReconcile_SitesOutOfSync_WavesCleared` — mismatch blocks wave formation, Ready=False
  - [ ] 7.10 `TestReconcile_WaitingForDiscovery_ProceedsNormally` — nil discovery does not block
  - [ ] 7.11 `TestReconcile_NoLocalSite_SkipsAgreementCheck` — backward compat, no SitesInSync condition set
  - [ ] 7.12 `TestDRExecutionValidator_RejectWhenSitesOutOfSync` — admission returns denied
  - [ ] 7.13 `TestDRExecutionValidator_AllowWhenSitesInSync` — admission allows normally

- [ ] Task 8: Integration tests (AC: #8)
  - [ ] 8.1 `TestDRPlanReconciler_CrossSiteAgreement_BlocksOnMismatch`:
    - Create plan with pre-populated `PrimarySiteDiscovery` (3 VMs) and `SecondarySiteDiscovery` (2 VMs — missing one)
    - Reconcile as active site
    - Assert `SitesInSync=False`, `Ready=False`, waves empty
  - [ ] 8.2 `TestDRPlanReconciler_CrossSiteAgreement_ProceedsOnMatch`:
    - Create plan with matching SiteDiscovery on both sides
    - Reconcile as active site
    - Assert `SitesInSync=True`, `Ready=True`, waves populated

- [ ] Task 9: Verify build and lint (AC: #8)
  - [ ] 9.1 Run `make generate` — deepcopy + OpenAPI regenerated
  - [ ] 9.2 Run `make lint` — zero new lint errors
  - [ ] 9.3 Run `make test` — all unit tests pass
  - [ ] 9.4 Run `make integration` — all integration tests pass

## Dev Notes

### Scope & Approach

This story adds the cross-site VM agreement gate. After Story 8.2, both sites independently report their discovered VMs. This story makes the **active-site reconciler** compare both reports and block wave formation when they disagree. This is the critical safety mechanism that prevents executions against inconsistent infrastructure.

**Change pattern:** Add comparison function → integrate into reconcile flow → gate admission → enrich preflight → backward compat → tests.

### Critical: Agreement Check Placement in Reconcile Flow

The agreement check must happen **after** the site-aware gate (which determines active/passive) but **before** wave formation. However, be careful of the chicken-and-egg problem:

- On **first deploy** with site-aware mode, the active site has not yet populated its own `SiteDiscovery` (that happens during this same reconcile cycle in the `updateStatus` call from Story 8.2).
- Therefore, when `SitesInSync` is `False` with reason `WaitingForDiscovery`, the reconciler must **proceed** with discovery and wave formation. The `SitesInSync` condition is informational in this case.
- Only `VMsMismatch` (both sides have data but they disagree) should **block** wave formation.

**Sequence in reconcile:**
1. Site-aware gate (active/passive) — existing
2. Agreement check — NEW: `compareSiteDiscovery()`
3. If `VMsMismatch` → block (clear waves, Ready=False, return early)
4. If `WaitingForDiscovery` → continue (log, set condition, proceed)
5. If `VMsAgreed` → continue (set condition, proceed)
6. VM discovery, wave formation, health polling — existing
7. Status patch includes SitesInSync condition — existing `updateStatus` flow

### Critical: Condition Persistence on Block

When agreement blocks wave formation (step 3 above), you must:
1. Re-fetch the plan (`r.Get(ctx, req.NamespacedName, &plan)`) for fresh resourceVersion
2. Set `SitesInSync=False` AND `Ready=False` on the same patch
3. Clear `plan.Status.Waves = nil` and `plan.Status.DiscoveredVMCount = 0`
4. Do NOT clear `plan.Status.PrimarySiteDiscovery` or `SecondarySiteDiscovery` — those are owned by site-aware discovery logic
5. Preserve `plan.Status.ReplicationHealth` if present (out of sync != replication broken)

### Critical: Delta Message Format

The delta message in the condition must be structured for both machine-readability (UI Story 8.4 will parse it) and human-readability:
- Format: `"VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]"`
- Sort VM references alphabetically for deterministic output
- Cap the list at ~20 entries per side to avoid excessively long condition messages; if exceeded, append `"... and N more"`
- For empty-discovery case: `"Site <siteName> discovered 0 VMs; verify VMs have label soteria.io/drplan=<planName>"`

### Critical: Admission Webhook Placement

The `SitesInSync` check in the admission webhook must happen **after** the phase transition check (line ~108) but before the final `admission.Allowed("")`. This ensures:
- Plan existence is verified first
- Concurrency gate (`ActiveExecution != ""`) is checked first
- Phase validity is checked first
- Then site agreement is checked last

This ordering means a user gets the most specific error message for their situation.

### Dependency on Story 8.2

Story 8.2 adds the `SiteDiscovery` types and fields. This story assumes:
- `PrimarySiteDiscovery *SiteDiscovery` and `SecondarySiteDiscovery *SiteDiscovery` exist on `DRPlanStatus`
- `SiteDiscovery` has `VMs []DiscoveredVM`, `DiscoveredVMCount int`, `LastDiscoveryTime metav1.Time`
- The passive site reconciler populates its SiteDiscovery field
- The active site reconciler populates its SiteDiscovery field in `updateStatus`

If Story 8.2 has NOT been implemented when you start this story, you must implement it first or the types won't exist.

### Dependency on Story 8.1

Story 8.1 removes `WaveLabel` from `DRPlanSpec` and replaces it with a `WaveLabel` constant. If implemented, `GroupByWave` and `ResolveVolumeGroups` no longer accept a `waveLabel` parameter. Check the current code state.

### VM Comparison Algorithm

```go
func compareSiteDiscovery(primary, secondary *SiteDiscovery) (bool, metav1.Condition) {
    // 1. Nil check → WaitingForDiscovery
    // 2. Build set from primary VMs: map[string]struct{} keyed by "namespace/name"
    // 3. Build set from secondary VMs
    // 4. Compute primaryOnly = primary - secondary
    // 5. Compute secondaryOnly = secondary - primary
    // 6. If both empty → VMsAgreed
    // 7. If one side has zero total VMs → specific empty-site message
    // 8. Otherwise → VMsMismatch with delta message
}
```

Use `{Namespace}/{Name}` as the comparison key (same as `DiscoveredVM` struct fields). Order-independent — builds sets, not sorted slices.

### updateStatus Modification Strategy

The current `updateStatus` takes a single `condition metav1.Condition` parameter (the Ready condition). To support `SitesInSync`:

**Option A (recommended):** Set `SitesInSync` on `plan.Status.Conditions` directly before calling `updateStatus`. The `patch := client.MergeFrom(plan.DeepCopy())` call inside `updateStatus` will capture any condition changes made to the plan between the DeepCopy base and the final patch. This requires that `updateStatus` re-fetches the plan (it already does at line 546).

**Wait — the re-fetch at line 546 (`r.Get(ctx, req.NamespacedName, plan)`) will overwrite any conditions you set before the call.** So you must set `SitesInSync` AFTER the re-fetch inside `updateStatus`.

**Option B (cleaner):** Add a `conditions []metav1.Condition` parameter (or variadic) to `updateStatus` and set all conditions inside it. This requires updating all call sites.

**Option C (minimal diff):** Set `SitesInSync` right after the `meta.SetStatusCondition(&plan.Status.Conditions, condition)` call (line 584) but before the `r.Status().Patch()` call (line 596). Add a new parameter `sitesInSyncCondition *metav1.Condition` (pointer, nil when not applicable).

**Recommend Option C** — minimal signature change, nil-safe for backward compat callers.

### Architecture Compliance

- **Condition type:** `metav1.Condition` — standard (project-context mandates no custom condition types)
- **Condition naming:** `SitesInSync` — PascalCase type, `VMsAgreed`/`VMsMismatch`/`WaitingForDiscovery` — PascalCase reasons
- **Merge patch:** `client.MergeFrom` for status updates (existing pattern)
- **ScyllaDB safety:** No new retry logic needed — existing updateStatus pattern handles conflicts
- **Logging:** `log.FromContext(ctx)`, Info(0) for state transitions, V(1) for normal ops
- **Events:** PascalCase past-tense reasons: `SitesOutOfSync`, `SitesInSync`
- **Aggregated API:** DRPlan is aggregated — `make generate` for deepcopy+OpenAPI, no CRD YAML
- **Metrics:** No new Prometheus metrics in this story

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/controller/drplan/reconciler.go` | Add constants, `compareSiteDiscovery`, integrate into reconcile + updateStatus | Core behavior change |
| `pkg/admission/drexecution_validator.go` | Add SitesInSync check after phase validation | Admission gate |
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `SitesInSync` and `SiteDiscoveryDelta` to `PreflightReport` | Schema addition |
| `pkg/controller/drplan/reconciler_test.go` | ~11 new test functions | Test addition |
| `pkg/admission/drexecution_validator_test.go` | ~2 new test cases | Test addition |
| `test/integration/controller/drplan_test.go` | 2 new integration tests | Test addition |
| `internal/preflight/compose.go` (or equivalent) | Populate new preflight fields | Minor enhancement |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-regenerated | DO NOT EDIT |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` | Auto-regenerated | DO NOT EDIT |

### Testing Strategy

- **Unit tests:** `compareSiteDiscovery` is a pure function — table-driven tests cover all edge cases. Reconciler tests use `fake.NewClientBuilder` with pre-populated SiteDiscovery fields on the plan status. Admission tests mock the reader to return plans with specific conditions
- **Integration tests:** envtest round-trip with pre-set SiteDiscovery status fields
- **Regression:** All existing tests must pass unchanged — the agreement check only activates in site-aware mode (`LocalSite != ""`), and the admission check only fires when `SitesInSync` condition exists on the plan
- **No mock changes needed:** Existing `mockVMDiscoverer` suffices; SiteDiscovery is pre-populated on plan status in test fixtures

### Execution Order

1. Task 5 (types) — add preflight fields, run `make generate`
2. Task 1 (constants + comparison function) — pure logic, testable in isolation
3. Task 2 (reconcile integration) — wire comparison into flow
4. Task 3 (updateStatus) — handle new condition
5. Task 4 (admission webhook) — gate execution
6. Task 6 (backward compat) — safety
7. Tasks 7-8 (tests) — verification
8. Task 9 (build + lint) — final validation

### Previous Story Learnings (from 8.2 story file)

- **Peer SiteDiscovery preservation:** The active site must NOT nil-out or overwrite the peer site's `*SiteDiscovery` pointer. When comparing, read from plan status directly — don't rely on local variables that might have stale values
- **Always patch SiteDiscovery (no skip logic):** `LastDiscoveryTime` always advances — downstream consumers use it for staleness detection
- **`client.MergeFrom` for all patches:** Strategic merge patch reduces conflict surface
- **Fake client for unit tests:** Existing `reconciler_test.go` uses `fake.NewClientBuilder` with `WithStatusSubresource` — keep this pattern

### Project Structure Notes

- All changes within `pkg/controller/drplan/`, `pkg/admission/`, `pkg/apis/`, `internal/preflight/`, and `test/integration/` — standard layout
- Console plugin (`console-plugin/`) is unaffected — it consumes `SitesInSync` condition in Story 8.4
- `config/samples/` unchanged — `SitesInSync` is a status condition, not spec

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L93-130] — DRPlanStatus struct (SiteDiscovery fields from Story 8.2)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L132-155] — PreflightReport (add fields here)
- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L233-239] — DiscoveredVM type (comparison key)
- [Source: pkg/controller/drplan/reconciler.go#L67-76] — Existing condition constants
- [Source: pkg/controller/drplan/reconciler.go#L109-143] — Reconcile start + site-aware gate
- [Source: pkg/controller/drplan/reconciler.go#L534-625] — updateStatus with merge patch
- [Source: pkg/admission/drexecution_validator.go#L53-112] — Admission webhook Handle method
- [Source: pkg/controller/drplan/reconciler_test.go#L44-93] — Test fixtures and mock patterns
- [Source: _bmad-output/project-context.md] — ScyllaRetry, MergeFrom, condition rules
- [Source: _bmad-output/planning-artifacts/epics.md#Story-8.3] — Epic requirements
- [Source: _bmad-output/implementation-artifacts/8-2-per-site-vm-discovery-reporting.md] — Previous story context

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
