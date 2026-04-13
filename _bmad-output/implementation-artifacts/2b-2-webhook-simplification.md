# Story 2b.2: Webhook Simplification

Status: ready-for-dev

## Story

As a platform engineer,
I want admission webhooks to be simpler and faster after the label convention change,
So that DRPlan and VM mutations are validated without expensive cross-resource queries.

## Acceptance Criteria

1. **AC1 — Delete `ExclusivityChecker`:** The entire `pkg/admission/exclusivity.go` file is deleted — `FindMatchingPlans`, `CheckVMExclusivity`, and `CheckDRPlanExclusivity` are no longer needed. `exclusivity_test.go` is also deleted.

2. **AC2 — DRPlan webhook becomes field-only validator:** The `DRPlanValidator` no longer calls `DiscoverVMs` or `CheckDRPlanExclusivity`. The `ExclusivityChecker` dependency is removed from `DRPlanValidator`. Validation of `waveLabel` and `maxConcurrentFailovers` (field-level) remains. Namespace consistency and throttle capacity checks are removed from the webhook — the controller already enforces these via `ResolveVolumeGroups` + `ChunkWaves` with `Ready=False` conditions. The webhook becomes a lightweight field validator only.

3. **AC3 — VM webhook simplified:** When a VirtualMachine CREATE or UPDATE is admitted with a `soteria.io/drplan` label, the webhook validates that the referenced DRPlan exists by name lookup. If the DRPlan does not exist, the webhook issues a **warning** (not a rejection) to avoid ordering issues during GitOps apply. The `CheckVMExclusivity` call is removed entirely — exclusivity is guaranteed by Kubernetes label semantics (a label key can have only one value). Wave conflict checking for namespace-level consistency is simplified: the webhook reads the plan name from the `soteria.io/drplan` label, fetches that single plan, and checks wave consistency only within that plan's VMs.

4. **AC4 — RBAC markers updated:** RBAC markers on `drplan_validator.go` are reduced — the DRPlan webhook no longer needs to list VirtualMachines or read namespaces. The VM webhook retains `drplans` get (for existence check) and `virtualmachines` list (for wave conflict siblings). `make manifests` regenerates `config/rbac/role.yaml` cleanly.

5. **AC5 — Setup wiring updated:** `setup.go` no longer passes `ExclusivityChecker` to either webhook. `SetupDRPlanWebhook` takes only `ctrl.Manager`. `SetupVMWebhook` takes `ctrl.Manager`, `engine.NamespaceLookup`, and `engine.VMDiscoverer`. All callers in `cmd/` are updated.

6. **AC6 — Unit tests updated:** `drplan_validator_test.go` tests verify field validation without VM discovery. `vm_validator_test.go` tests verify: VM with valid `soteria.io/drplan` label is accepted, VM with nonexistent plan gets a warning (not rejection), VM with wave conflict in namespace-level namespace is rejected, VM without the label is always accepted.

7. **AC7 — Integration tests updated:** Integration tests in `test/integration/admission/` are updated: DRPlan webhook tests verify field-only validation (no exclusivity scenarios). VM webhook tests verify plan existence warning, wave conflict rejection, and label-absent acceptance. No test file references `vmSelector` or `ExclusivityChecker`. `make test` passes.

## Tasks / Subtasks

- [ ] Task 1: Delete exclusivity checker (AC: #1)
  - [ ] 1.1 Delete `pkg/admission/exclusivity.go`
  - [ ] 1.2 Delete `pkg/admission/exclusivity_test.go`

- [ ] Task 2: Simplify DRPlan validator (AC: #2, #4)
  - [ ] 2.1 Remove `ExclusivityChecker` field from `DRPlanValidator` struct
  - [ ] 2.2 Remove `VMSelector` parsing from `Handle` (the `metav1.LabelSelectorAsSelector` block around current lines 91–95)
  - [ ] 2.3 Remove `DiscoverVMs` call and all downstream logic (exclusivity check, namespace consistency check, throttle capacity check — current lines 97–148)
  - [ ] 2.4 Keep field validation: decode the DRPlan, call `ValidateDRPlan` / `ValidateDRPlanUpdate` from `pkg/apis/soteria.io/v1alpha1/validation.go`, return denial if errors
  - [ ] 2.5 Remove `NSLookup` field from `DRPlanValidator` — no longer needed (namespace consistency delegated to controller)
  - [ ] 2.6 Remove unused imports: `"k8s.io/apimachinery/pkg/labels"`, `metav1`, `engine`, anything no longer referenced
  - [ ] 2.7 Update RBAC markers: remove `kubevirt.io virtualmachines` and `namespaces` — the DRPlan webhook only needs `soteria.io drplans` for decoding the admission request (no list/watch needed — the object comes in the request). Evaluate whether any RBAC is still needed — if the handler only validates fields from the request object, no API calls are made and all three RBAC lines can be removed
  - [ ] 2.8 Remove `checkNamespaceConsistency` function (lines 153–213)
  - [ ] 2.9 Remove `checkMaxConcurrentCapacity` function (lines 215–260)
  - [ ] 2.10 Update the Tier 2 block comment at top of file — describe the webhook as a lightweight field validator that delegates cross-resource consistency to the controller

- [ ] Task 3: Simplify VM validator (AC: #3, #4)
  - [ ] 3.1 Remove `ExclusivityChecker` field from `VMValidator` struct
  - [ ] 3.2 Remove `CheckVMExclusivity` call from `Handle` (current line 93) — exclusivity is structurally guaranteed by label semantics
  - [ ] 3.3 Rewrite `Handle` flow: if VM has `soteria.io/drplan` label → check plan existence → check wave consistency → allow/deny. If VM has no `soteria.io/drplan` label → allow immediately (no plan membership = no constraints)
  - [ ] 3.4 Implement plan existence check: read `soteria.io/drplan` label value as plan name, `client.Get` the DRPlan by `types.NamespacedName{Namespace: vm.Namespace, Name: planName}`. If `apierrors.IsNotFound` → return `Allowed` with a **warning** (`admission.Allowed("...").WithWarnings(...)`) — do NOT reject, because during GitOps apply the DRPlan CR may not exist yet
  - [ ] 3.5 Simplify `checkWaveConflict`: remove `FindMatchingPlans` call — the VM's plan is known from the single `soteria.io/drplan` label value. Fetch the plan, call `VMDiscoverer.DiscoverVMs(ctx, planName)` to get sibling VMs, then check wave consistency for namespace-level namespaces
  - [ ] 3.6 Remove `checkWaveConflict` outer function (lines 124–157) which iterated over `FindMatchingPlans` results — replace with direct plan-name-based lookup
  - [ ] 3.7 Keep `checkWaveConflictForPlan` (lines 159–199) but update: change `v.VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)` to `v.VMDiscoverer.DiscoverVMs(ctx, plan.Name)` — align with Story 2b.1 interface change
  - [ ] 3.8 Remove `Client client.Reader` field from `VMValidator` if plan existence can be checked via the `VMDiscoverer`'s underlying reader — OR keep a separate `Client` field for the `Get` call. Evaluate: the `Get` call for plan existence requires a `client.Reader`, not a `VMDiscoverer`. Keep a `PlanReader client.Reader` or equivalent for the `Get` call
  - [ ] 3.9 Update RBAC markers: remove `soteria.io drplans list;watch` → only `get` is needed (existence check). Keep `kubevirt.io virtualmachines get;list;watch` (for wave conflict sibling discovery). Keep `namespaces get;list;watch` (for `NSLookup.GetConsistencyLevel`). Add `soteria.io drplans get` if not already present with just `get`
  - [ ] 3.10 Remove unused imports after refactoring
  - [ ] 3.11 Update the Tier 2 block comment — describe the webhook as validating plan existence (warning) and namespace-level wave consistency only, with exclusivity structurally guaranteed

- [ ] Task 4: Update setup.go wiring (AC: #5)
  - [ ] 4.1 Update `SetupDRPlanWebhook` signature: remove `exclusivityChecker` and `nsLookup` parameters. The function takes only `mgr ctrl.Manager`
  - [ ] 4.2 Update `DRPlanValidator` construction: remove `ExclusivityChecker` and `NSLookup` fields — only `decoder` remains
  - [ ] 4.3 Update `SetupVMWebhook` signature: remove `exclusivityChecker` parameter. Keep `nsLookup` and `vmDiscoverer`. Add a `planReader client.Reader` parameter (or derive from `mgr.GetClient()`) for the DRPlan existence check
  - [ ] 4.4 Update `VMValidator` construction: remove `ExclusivityChecker`, wire `PlanReader` (or `Client`) for the `Get` call
  - [ ] 4.5 Remove `engine` import from `setup.go` if `SetupDRPlanWebhook` no longer needs `engine.NamespaceLookup`

- [ ] Task 5: Update callers in cmd/ (AC: #5)
  - [ ] 5.1 Find where `SetupDRPlanWebhook` and `SetupVMWebhook` are called (likely `cmd/main.go` or a setup function)
  - [ ] 5.2 Remove `ExclusivityChecker` construction — no longer needed anywhere
  - [ ] 5.3 Update `SetupDRPlanWebhook` call to match new simplified signature
  - [ ] 5.4 Update `SetupVMWebhook` call to match new signature (pass `nsLookup`, `vmDiscoverer` but not `exclusivityChecker`)
  - [ ] 5.5 Remove any unused imports from `cmd/main.go`

- [ ] Task 6: Update unit tests (AC: #6)
  - [ ] 6.1 Rewrite `drplan_validator_test.go`: remove `mockVMDiscoverer`, `ExclusivityChecker`, and all exclusivity/namespace/throttle test cases. Test only: valid DRPlan allowed, invalid `waveLabel` rejected, invalid `maxConcurrentFailovers` rejected, UPDATE validation, DELETE always allowed
  - [ ] 6.2 Remove shared test helpers that are no longer needed (`mockVMDiscoverer` if only used by DRPlan tests — check if VM tests still need it)
  - [ ] 6.3 Rewrite `vm_validator_test.go`: remove exclusivity test cases. Add/update: VM with valid `soteria.io/drplan` label pointing to existing plan → allowed. VM with `soteria.io/drplan` pointing to nonexistent plan → allowed with warning. VM with no `soteria.io/drplan` label → allowed. VM with wave conflict in namespace-level namespace → rejected. VM DELETE → always allowed
  - [ ] 6.4 Verify `mockVMDiscoverer` is still present for VM wave conflict tests (it's needed for `DiscoverVMs` in `checkWaveConflictForPlan`)
  - [ ] 6.5 Update `mockNSLookup` usage if needed
  - [ ] 6.6 Remove `buildScheme` helper if no longer needed by DRPlan tests — or keep if VM tests still use it
  - [ ] 6.7 Run `make test` to verify all unit tests pass

- [ ] Task 7: Update integration tests (AC: #7)
  - [ ] 7.1 Update `test/integration/admission/suite_test.go`: remove `ExclusivityChecker` construction (lines 167–186). Update `SetupDRPlanWebhook` call to match new signature. Update `SetupVMWebhook` call
  - [ ] 7.2 Rewrite `test/integration/admission/drplan_webhook_test.go`: remove `VMExclusivity_Rejected`, `InvalidSelector_Rejected`, `WaveConflict_Rejected`, `MaxConcurrentExceeded_Rejected`, `Update_ExclusivityExcludesSelf` — none of these are webhook concerns anymore. Keep/add: `ValidPlan_Allowed`, `InvalidWaveLabel_Rejected`, `InvalidMaxConcurrent_Rejected`, `DELETE_Allowed`, `UPDATE_Validation`
  - [ ] 7.3 Rewrite `test/integration/admission/vm_webhook_test.go`: remove exclusivity tests. Add: VM with `soteria.io/drplan` label pointing to existing plan → allowed. VM with nonexistent plan → allowed with warning. VM with no label → allowed. VM with namespace wave conflict → rejected
  - [ ] 7.4 Remove helper functions that reference `vmSelector` or `ExclusivityChecker`
  - [ ] 7.5 Verify no test file references `vmSelector`, `ExclusivityChecker`, `FindMatchingPlans`, `CheckVMExclusivity`, or `CheckDRPlanExclusivity`

- [ ] Task 8: Update doc.go and run generators (AC: #4)
  - [ ] 8.1 Update `pkg/admission/doc.go` package comment: remove references to "VM exclusivity across plans", "ExclusivityChecker", and "vmSelector". Describe: DRPlan webhook validates field-level constraints, VM webhook validates plan existence (warning) and namespace-level wave consistency. Exclusivity is structurally guaranteed by the `soteria.io/drplan` label convention
  - [ ] 8.2 Run `make manifests` — regenerate RBAC from updated markers
  - [ ] 8.3 Run `make generate` — should be a no-op but run for safety
  - [ ] 8.4 Run `make lint-fix` followed by `make lint`
  - [ ] 8.5 Run `make test` — all unit + envtest tests must pass

- [ ] Task 9: Verify codebase is clean
  - [ ] 9.1 Grep for `ExclusivityChecker` across the entire codebase — must return zero hits
  - [ ] 9.2 Grep for `FindMatchingPlans` — zero hits
  - [ ] 9.3 Grep for `CheckVMExclusivity` — zero hits
  - [ ] 9.4 Grep for `CheckDRPlanExclusivity` — zero hits
  - [ ] 9.5 Grep for `vmSelector` in `pkg/` — zero hits (Story 2b.1 removed it from types; this story removes it from admission)
  - [ ] 9.6 Grep for `plan.Spec.VMSelector` — zero hits
  - [ ] 9.7 Verify `config/rbac/role.yaml` reflects reduced webhook permissions

## Dev Notes

### Previous Story Learnings (from Story 2b.1)

- RBAC markers: place on the file that contains the webhook handler, not in `doc.go` or `setup.go`
- `make manifests` must be run after any marker change — RBAC yaml is generated, never hand-edited
- `apierrors.IsNotFound()` is the preferred error check pattern for Kubernetes API errors
- When modifying validation, also check `pkg/registry/drplan/strategy.go` for calls to validation — strategy is the API server's validation layer, webhooks are the kube-apiserver's admission layer
- Code review pattern from Epic 2: fake client in unit tests doesn't handle resourceVersion, UIDs, or status subresources — but for simple `Get` operations (plan existence check) the fake client is sufficient
- Context propagation: always use `ctx` from the admission handler — never `context.Background()`

### Architecture Context

This is Story 2 of a 4-story refactoring epic (2b). Story 2b.1 replaced `vmSelector` with the `soteria.io/drplan` label convention, making exclusivity a structural invariant. This story capitalizes on that change by deleting the `ExclusivityChecker` entirely and simplifying both webhooks.

**The key design shift:** Namespace consistency and throttle capacity checks are removed from admission webhooks and delegated to the controller's reconciliation loop (where they already exist). This is safe because:

1. The controller already enforces wave conflicts via `ResolveVolumeGroups` with `Ready=False` + reason `WaveConflict`
2. The controller already enforces throttle limits via `ChunkWaves` with `Ready=False` + reason `NamespaceGroupExceedsThrottle`
3. A misconfigured DRPlan is never executed — the workflow engine requires `Ready=True` before starting any failover
4. This moves from "prevent all misconfiguration at admission" to "allow writes, enforce correctness via status conditions" — the standard Kubernetes eventual-consistency pattern

**What is deleted:**
- `pkg/admission/exclusivity.go` — the entire file (147 lines)
- `pkg/admission/exclusivity_test.go` — the entire file (359 lines)
- `checkNamespaceConsistency` and `checkMaxConcurrentCapacity` functions from `drplan_validator.go`
- `CheckVMExclusivity` and `FindMatchingPlans` calls from `vm_validator.go`

**What is simplified:**
- `DRPlanValidator.Handle` — shrinks from ~85 lines of logic to ~20 lines (decode + validate fields + return)
- `VMValidator.Handle` — reads plan name from label, checks existence (warning), checks wave consistency only
- `setup.go` — simpler wiring without `ExclusivityChecker`

**What does NOT change (must be preserved):**
- The DRPlan webhook path `/validate-soteria-io-v1alpha1-drplan` — do not change the URL
- The VM webhook path `/validate-kubevirt-io-v1-virtualmachine` — do not change the URL
- The `+kubebuilder:webhook` markers on both handlers — do not change `path`, `name`, `groups`, `resources`, or `versions` fields. Only RBAC markers change
- Controller-side consistency enforcement (`ResolveVolumeGroups`, `ChunkWaves`) — untouched
- `config/webhook/manifests.yaml` — generated from markers, not hand-edited

### Critical Implementation Details

**DRPlan webhook — field-only validation pattern:**

After simplification, `Handle` should be approximately:

```go
func (v *DRPlanValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
    if req.Operation == admissionv1.Delete {
        return admission.Allowed("")
    }

    plan := &soteriav1alpha1.DRPlan{}
    if err := v.decoder.Decode(req, plan); err != nil {
        return admission.Errored(http.StatusBadRequest, err)
    }

    var allErrs field.ErrorList
    if req.Operation == admissionv1.Create {
        allErrs = soteriav1alpha1.ValidateDRPlan(plan)
    } else {
        oldPlan := &soteriav1alpha1.DRPlan{}
        if err := json.Unmarshal(req.OldObject.Raw, oldPlan); err != nil {
            return admission.Errored(http.StatusBadRequest, err)
        }
        allErrs = soteriav1alpha1.ValidateDRPlanUpdate(plan, oldPlan)
    }

    if len(allErrs) > 0 {
        return admission.Denied(allErrs.ToAggregate().Error())
    }
    return admission.Allowed("")
}
```

No `client.Reader`, no `VMDiscoverer`, no namespace lookup — pure field validation on the request object.

**VM webhook — plan existence warning:**

The VM webhook must issue a **warning, not a rejection** when the referenced DRPlan doesn't exist. This is critical for GitOps workflows where VMs may be applied before their DRPlan:

```go
planName := vm.Labels[soteriav1alpha1.DRPlanLabel]
if planName == "" {
    return admission.Allowed("")
}

plan := &soteriav1alpha1.DRPlan{}
err := v.Client.Get(ctx, types.NamespacedName{Namespace: vm.Namespace, Name: planName}, plan)
if apierrors.IsNotFound(err) {
    return admission.Allowed("").WithWarnings(
        fmt.Sprintf("referenced DRPlan %q does not exist in namespace %q", planName, vm.Namespace))
}
if err != nil {
    return admission.Errored(http.StatusInternalServerError, err)
}
// proceed to wave consistency check...
```

**Wave consistency check — simplified sibling discovery:**

Instead of calling `FindMatchingPlans` to discover which plans match the VM, the webhook reads the plan name from the label directly. Then calls `VMDiscoverer.DiscoverVMs(ctx, planName)` to get sibling VMs. For namespace-level namespaces, it checks that all VMs in the same namespace have the same wave label value. This reduces the wave check from O(plans × VMs) to O(VMs-in-plan).

**Warning response in admission webhooks:**

controller-runtime's `admission.Allowed("")` returns a `Response`. Use `.WithWarnings(msgs...)` to attach warning strings. Kubernetes 1.19+ propagates these as `Warning` headers to the kubectl client. The response is still `Allowed` — the VM is admitted.

### Existing Code Patterns to Follow

- **Import alias:** `soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"` — consistent across codebase
- **Logging style:** `logger.Info("msg", "key", val)`, `logger.Error(err, "msg", "key", val)`
- **Error wrapping:** lowercase, wrap with `%w` — `fmt.Errorf("getting DRPlan: %w", err)`
- **Admission response patterns:**
  - `admission.Allowed("")` — accepted
  - `admission.Denied("reason")` — rejected with human-readable message
  - `admission.Errored(http.StatusInternalServerError, err)` — internal error
  - `admission.Allowed("").WithWarnings("msg")` — accepted with warning
- **`apierrors.IsNotFound(err)`** — preferred over raw error comparison
- **Context propagation:** use `ctx` from `Handle` — never `context.Background()`

### Files to Delete

| File | Reason |
|------|--------|
| `pkg/admission/exclusivity.go` | ExclusivityChecker no longer needed — label semantics guarantee exclusivity |
| `pkg/admission/exclusivity_test.go` | Tests for deleted code |

### Files to Modify

| File | Change |
|------|--------|
| `pkg/admission/drplan_validator.go` | Remove ExclusivityChecker/VMDiscoverer/NSLookup deps, remove discovery+consistency+throttle logic, keep field validation only, update RBAC markers, update block comment |
| `pkg/admission/drplan_validator_test.go` | Rewrite: field-only validation tests, remove exclusivity/consistency/throttle tests, remove mockVMDiscoverer if unused |
| `pkg/admission/vm_validator.go` | Remove ExclusivityChecker, add plan existence warning, simplify wave check to use plan name from label, update RBAC markers, update block comment |
| `pkg/admission/vm_validator_test.go` | Rewrite: plan existence warning tests, wave conflict tests, remove exclusivity tests |
| `pkg/admission/setup.go` | Simplify SetupDRPlanWebhook (mgr only), update SetupVMWebhook (remove exclusivityChecker param) |
| `pkg/admission/doc.go` | Update package comment for new webhook responsibilities |
| `cmd/main.go` (or equivalent caller) | Remove ExclusivityChecker construction, update webhook setup calls |
| `test/integration/admission/suite_test.go` | Remove ExclusivityChecker wiring, update webhook setup calls |
| `test/integration/admission/drplan_webhook_test.go` | Rewrite for field-only validation scenarios |
| `test/integration/admission/vm_webhook_test.go` | Rewrite for plan existence + wave conflict scenarios |

### Files NOT to Modify (Story 2b.3 or later)

- `config/samples/` — will be updated in Story 2b.3
- `_bmad-output/planning-artifacts/prd.md` — will be updated in Story 2b.3
- `_bmad-output/planning-artifacts/architecture.md` — will be updated in Story 2b.3
- `_bmad-output/project-context.md` — will be updated in Story 2b.3
- `pkg/controller/drplan/reconciler.go` — already has consistency enforcement, no changes needed
- `pkg/engine/discovery.go` — changed in Story 2b.1, no changes in this story

### Build Commands

```bash
make manifests    # Regenerate RBAC after marker changes
make generate     # No-op but run for safety
make lint-fix     # Auto-fix code style
make test         # Unit + envtest tests
```

### Project Structure Notes

- `pkg/admission/` — Webhooks (this story simplifies all files, deletes exclusivity)
- `test/integration/admission/` — Integration tests for webhooks (this story rewrites all three files)
- `cmd/main.go` — Entry point that wires webhook setup (this story updates calls)
- Multigroup layout: not used — single `soteria.io/v1alpha1` API group
- RBAC regeneration required after marker changes: `make manifests`
- `config/webhook/manifests.yaml` is generated from `+kubebuilder:webhook` markers — do NOT hand-edit
- `config/rbac/role.yaml` is generated from `+kubebuilder:rbac` markers — do NOT hand-edit

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2b.2] — Full acceptance criteria
- [Source: pkg/admission/exclusivity.go] — ExclusivityChecker to be deleted (147 lines)
- [Source: pkg/admission/drplan_validator.go] — Current DRPlanValidator with discovery+exclusivity+consistency+throttle (260 lines)
- [Source: pkg/admission/vm_validator.go] — Current VMValidator with exclusivity+wave checks (199 lines)
- [Source: pkg/admission/setup.go] — Current webhook wiring with ExclusivityChecker (70 lines)
- [Source: pkg/controller/drplan/reconciler.go] — Controller already enforces ResolveVolumeGroups (line ~159) and ChunkWaves (line ~198)
- [Source: _bmad-output/implementation-artifacts/2b-1-label-convention-api-discovery-controller.md] — Story 2b.1 context (DRPlanLabel constant, DiscoverVMs(planName) signature)
- [Source: _bmad-output/project-context.md] — Project conventions, testing rules, documentation tiers

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
