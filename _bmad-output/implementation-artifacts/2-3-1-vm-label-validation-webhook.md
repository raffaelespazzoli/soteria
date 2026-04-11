# Story 2.3.1: VM Label Validation Webhook

Status: done

## Story

As a platform engineer,
I want the orchestrator to reject VM label changes that would violate DRPlan constraints,
So that VM exclusivity and namespace wave consistency are enforced regardless of whether the DRPlan or the VM is mutated.

## Acceptance Criteria

1. **Given** two existing DRPlans — DRPlan A selecting `app=erp` and DRPlan B selecting `tier=db`, **When** a VirtualMachine is created with labels `app=erp` and `tier=db` (matching both plans), **Then** the admission webhook in `pkg/admission/vm_validator.go` rejects the creation with a clear error: "VM <namespace>/<name> would belong to multiple DRPlans: <namespace>/<plan-A>, <namespace>/<plan-B>" (FR4).

2. **Given** a VirtualMachine that currently matches only DRPlan A (`app=erp`), **When** the VM is updated to add label `tier=db` which also matches DRPlan B, **Then** the admission webhook rejects the update with the same multi-plan exclusivity error (FR4).

3. **Given** a namespace with `soteria.io/consistency-level: namespace` annotation and a DRPlan selecting VMs in that namespace where all current VMs have wave label `soteria.io/wave: "1"`, **When** a VM in that namespace is created or updated with wave label `soteria.io/wave: "2"`, **Then** the admission webhook rejects the mutation with an error: "VM <namespace>/<name> wave label '2' conflicts with existing VMs in namespace-level namespace <ns> under DRPlan <plan> (expected wave '1')" (FR7).

4. **Given** a VirtualMachine being created or updated, **When** no exclusivity violations or wave conflicts exist, **Then** the admission webhook allows the mutation.

5. **Given** the webhook configuration, **When** deployed via `config/webhook/`, **Then** the webhook intercepts CREATE and UPDATE operations on `kubevirt.io/v1` VirtualMachine resources, **And** webhook TLS certificates are shared with the Story 2.3 DRPlan webhook (same cert-manager Certificate and webhook server).

6. **Given** a VirtualMachine that currently matches a DRPlan, **When** the VM's labels are updated to remove the matching labels so it no longer matches any DRPlan, **Then** the admission webhook allows the mutation (the controller handles removal gracefully on next reconcile).

## Tasks / Subtasks

- [ ] Task 1: Extract shared exclusivity helper from Story 2.3 (AC: #1, #2)
  - [ ] 1.1 Create `pkg/admission/exclusivity.go` with Tier 2 architecture block comment explaining: this module provides shared VM exclusivity checking logic used by both the DRPlan webhook (Story 2.3) and the VM webhook (Story 2.3.1); the core question is "given a VM's labels, which DRPlans select it?"
  - [ ] 1.2 Define `ExclusivityChecker` struct — fields:
    - `Client client.Reader` — for listing DRPlans
    - `VMDiscoverer engine.VMDiscoverer` — for discovering VMs matching a selector (needed by DRPlan webhook; VM webhook uses label matching directly)
  - [ ] 1.3 Implement `FindMatchingPlans(ctx context.Context, vmLabels labels.Set, excludePlan *types.NamespacedName) ([]types.NamespacedName, error)`:
    - List all DRPlans across all namespaces via `Client.List(ctx, &DRPlanList{})`
    - For each DRPlan (excluding `excludePlan` if non-nil):
      - Parse `vmSelector` with `metav1.LabelSelectorAsSelector()`; skip plans with invalid selectors
      - Check if `vmLabels` matches the parsed selector
    - Return list of matching DRPlan NamespacedNames
    - Tier 3 domain 'why' comment: VM exclusivity prevents two DRPlans from issuing conflicting storage operations (promote/demote) on the same VM's volumes, which would cause data corruption or split-brain
  - [ ] 1.4 Implement `CheckVMExclusivity(ctx context.Context, vmName, vmNamespace string, vmLabels labels.Set) ([]string, error)`:
    - Call `FindMatchingPlans(ctx, vmLabels, nil)`
    - If len(matchingPlans) > 1, build error messages: `"VM <namespace>/<name> would belong to multiple DRPlans: <plan1>, <plan2>, ..."`
    - Return list of error messages (empty if no violations)
  - [ ] 1.5 Implement `CheckDRPlanExclusivity(ctx context.Context, plan *DRPlan, discoveredVMs []engine.VMReference) ([]string, error)`:
    - For each discovered VM, call `FindMatchingPlans(ctx, vmLabels, &planNamespacedName)` — excludes the plan being validated
    - For each VM with > 0 other matching plans, build error message: `"VM <namespace>/<name> already belongs to DRPlan <namespace>/<plan-name>"`
    - Return all conflict messages
    - This replaces the inline `checkVMExclusivity` in Story 2.3's `DRPlanValidator`

- [ ] Task 2: Implement VMValidator admission webhook handler (AC: #1, #2, #3, #4, #6)
  - [ ] 2.1 Create `pkg/admission/vm_validator.go` with Tier 2 architecture block comment explaining: the webhook validates VM CREATE/UPDATE mutations against DRPlan constraints from the VM side; this complements the DRPlan webhook (Story 2.3) which validates from the DRPlan side — together they enforce VM exclusivity and namespace wave consistency regardless of which resource is mutated
  - [ ] 2.2 Define `VMValidator` struct — fields:
    - `ExclusivityChecker *ExclusivityChecker` — shared exclusivity logic (from Task 1)
    - `NSLookup engine.NamespaceLookup` — for checking namespace consistency annotations (from Story 2.2)
    - `Client client.Reader` — for listing DRPlans and VMs in the same namespace (wave conflict check)
    - `VMDiscoverer engine.VMDiscoverer` — for discovering sibling VMs in the same DRPlan (wave conflict check)
    - `decoder admission.Decoder`
  - [ ] 2.3 Implement `Handle(ctx context.Context, req admission.Request) admission.Response`:
    - Only handle CREATE and UPDATE operations; allow all others (including DELETE — AC #6 rationale)
    - Decode the VirtualMachine from `req.Object.Raw` as `kubevirtv1.VirtualMachine` (typed — `kubevirt.io/api` is a project dependency since Story 2.1)
    - Extract VM name, namespace, and labels from the typed object
    - If the VM has no labels, allow (cannot match any DRPlan selector)
    - Run VM exclusivity check (2.4)
    - Run wave conflict check (2.5)
    - Collect all denial reasons; if any exist, deny with combined message
    - If all checks pass, return `admission.Allowed("")`
  - [ ] 2.4 Implement exclusivity check within Handle:
    - Call `ExclusivityChecker.CheckVMExclusivity(ctx, vmName, vmNamespace, vmLabels)`
    - If errors returned, add to denial reasons
  - [ ] 2.5 Implement `checkWaveConflict(ctx, vmName, vmNamespace string, vmLabels labels.Set) ([]string, error)`:
    - Call `NSLookup.GetConsistencyLevel(ctx, vmNamespace)` to check namespace annotation
    - If consistency level is not `"namespace"`, return empty (no wave conflict possible at VM-level consistency)
    - Call `ExclusivityChecker.FindMatchingPlans(ctx, vmLabels, nil)` to find which DRPlan(s) select this VM
    - If no matching plans, return empty (VM not in any DRPlan — no conflict)
    - For each matching DRPlan:
      - Get the plan's `waveLabel` from spec
      - Extract this VM's wave value from `vmLabels[waveLabel]`
      - Discover other VMs in the same namespace matching the same plan's selector via `VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)`
      - Filter to VMs in the same namespace (excluding this VM)
      - Check if any sibling VM has a different wave label value
      - If conflict found, record: `"VM <ns>/<name> wave label '<wave>' conflicts with existing VMs in namespace-level namespace <ns> under DRPlan <plan-ns>/<plan-name> (expected wave '<existing-wave>')"`
    - Return all conflict messages
  - [ ] 2.6 Add Tier 3 domain 'why' comments:
    - On VM-side exclusivity: DRPlan-side validation only catches conflicts when a DRPlan is created/updated; VM label changes can bypass that check — e.g., adding a label to a VM after two non-overlapping DRPlans exist can create a new overlap
    - On wave conflict from VM side: when a VM's wave label changes in a namespace-level namespace, it can break the same-wave constraint required for crash-consistent snapshots — the DRPlan webhook only checks this when the DRPlan is mutated, not when individual VMs change their wave labels
  - [ ] 2.7 Structured logging:
    - `log.Info("Validating VM admission", "name", vmName, "namespace", vmNamespace, "operation", req.Operation)`
    - `log.Info("VM exclusivity check completed", "matchingPlans", len(matchingPlans))`
    - `log.Info("VM admission denied", "reasons", len(allDenials))` or `log.Info("VM admission allowed")`

- [ ] Task 3: Register VM webhook and extend webhook configuration (AC: #5)
  - [ ] 3.1 Update `pkg/admission/setup.go` (created in Story 2.3):
    - Add `ValidateVMPath = "/validate-kubevirt-io-v1-virtualmachine"` constant
    - Add `SetupVMWebhook(mgr ctrl.Manager, exclusivityChecker *ExclusivityChecker, nsLookup engine.NamespaceLookup, vmDiscoverer engine.VMDiscoverer) error`:
      - Create `VMValidator` with the shared `ExclusivityChecker`, `mgr.GetClient()`, nsLookup, vmDiscoverer, and `admission.NewDecoder(mgr.GetScheme())`
      - Register: `mgr.GetWebhookServer().Register(ValidateVMPath, &webhook.Admission{Handler: validator})`
    - Update `SetupDRPlanWebhook` to accept `*ExclusivityChecker` instead of raw Client — refactor `DRPlanValidator` to use the shared checker
  - [ ] 3.2 Update `config/webhook/manifests.yaml` (created in Story 2.3) — add a second webhook entry in the `ValidatingWebhookConfiguration`:
    - Webhook name: `vvm.kb.io`
    - Rules: apiGroups `["kubevirt.io"]`, apiVersions `["v1"]`, resources `["virtualmachines"]`, operations `["CREATE", "UPDATE"]`
    - `failurePolicy: Fail` (fail-closed)
    - `sideEffects: None`
    - `admissionReviewVersions: ["v1"]`
    - `clientConfig.service`: same service as DRPlan webhook, path `/validate-kubevirt-io-v1-virtualmachine`
  - [ ] 3.3 No changes needed to `config/webhook/service.yaml` or cert-manager — both webhooks share the same webhook server (port 9443) and TLS certificate
  - [ ] 3.4 Add RBAC markers to `pkg/admission/vm_validator.go`:
    - `+kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch`

- [ ] Task 4: Wire VM webhook into cmd/soteria/main.go (AC: #5)
  - [ ] 4.1 After manager creation, create `ExclusivityChecker` with the manager's client and VM discoverer
  - [ ] 4.2 Update `admission.SetupDRPlanWebhook(mgr, exclusivityChecker, nsLookup)` call to pass the shared checker
  - [ ] 4.3 Call `admission.SetupVMWebhook(mgr, exclusivityChecker, nsLookup, vmDiscoverer)` with error handling
  - [ ] 4.4 Verify `go build ./cmd/soteria/` succeeds
  - [ ] 4.5 Run `make manifests` to regenerate RBAC and webhook configuration

- [ ] Task 5: Unit tests for shared exclusivity helper (AC: #1, #2)
  - [ ] 5.1 Create `pkg/admission/exclusivity_test.go`
  - [ ] 5.2 Table-driven `TestFindMatchingPlans` covering:
    - VM labels match no DRPlans → empty result
    - VM labels match exactly one DRPlan → one result
    - VM labels match two DRPlans → two results
    - VM labels match a DRPlan but it's excluded via `excludePlan` → empty result
    - DRPlan with invalid vmSelector → skipped, no error
    - No DRPlans exist → empty result
    - VM with empty labels → matches no plans
  - [ ] 5.3 Table-driven `TestCheckVMExclusivity` covering:
    - VM matches 0 plans → no errors
    - VM matches 1 plan → no errors (single plan is fine)
    - VM matches 2 plans → error listing both plans
    - VM matches 3 plans → error listing all three plans
  - [ ] 5.4 Table-driven `TestCheckDRPlanExclusivity` covering:
    - All discovered VMs unique to this plan → no errors
    - One discovered VM also matches another plan → one error
    - Multiple VMs each match different other plans → multiple errors
    - Discovered VMs match the plan being validated (self) → excluded, no errors

- [ ] Task 6: Unit tests for VMValidator webhook handler (AC: #1, #2, #3, #4, #6)
  - [ ] 6.1 Create `pkg/admission/vm_validator_test.go`
  - [ ] 6.2 Create test helpers:
    - Helper to build `admission.Request` from a typed `kubevirtv1.VirtualMachine` for CREATE/UPDATE operations
    - Reuse `MockVMDiscoverer` and `MockNamespaceLookup` from Story 2.3 tests (or define locally if 2.3 isn't implemented yet)
    - Use controller-runtime `fake.NewClientBuilder()` with `soteriav1alpha1` scheme for DRPlan listing
  - [ ] 6.3 Table-driven tests for VM exclusivity (AC: #1, #2):
    - VM CREATE with labels matching 0 DRPlans → allowed
    - VM CREATE with labels matching 1 DRPlan → allowed
    - VM CREATE with labels matching 2 DRPlans → denied with exclusivity error listing both plans
    - VM UPDATE adding label that causes match with 2nd DRPlan → denied
    - VM UPDATE removing labels so it matches 0 plans → allowed (AC #6)
    - VM with no labels → allowed
  - [ ] 6.4 Table-driven tests for wave conflict (AC: #3):
    - VM in namespace without consistency annotation → allowed (no wave conflict check)
    - VM in namespace-level namespace, wave label matches all siblings → allowed
    - VM CREATE in namespace-level namespace with wave label differing from existing VMs → denied with wave conflict error
    - VM UPDATE changing wave label to conflict with siblings in namespace-level namespace → denied
    - VM in namespace-level namespace but not matching any DRPlan → allowed (not part of a plan)
    - VM is the only one in a namespace-level namespace under a plan → allowed (no siblings to conflict with)
  - [ ] 6.5 Test: DELETE operation → allowed (webhook only validates CREATE/UPDATE)
  - [ ] 6.6 Test: combined violations — VM matches 2 plans AND has wave conflict → both errors reported

- [ ] Task 7: Integration tests (AC: #1, #2, #3, #4, #5, #6)
  - [ ] 7.1 Extend `test/integration/admission/suite_test.go` (from Story 2.3) — ensure VM ValidatingWebhookConfiguration is applied alongside the DRPlan webhook; kubevirt VM CRD already registered from Story 2.3
  - [ ] 7.2 Create `test/integration/admission/vm_webhook_test.go` with `//go:build integration` tag
  - [ ] 7.3 `TestVMWebhook_Exclusivity_CreateMatchingTwoPlans_Rejected` — create DRPlan A (`app=erp`) and DRPlan B (`tier=db`), create VM with both labels → expect admission denied with exclusivity error
  - [ ] 7.4 `TestVMWebhook_Exclusivity_CreateMatchingOnePlan_Allowed` — create DRPlan A (`app=erp`), create VM with only `app=erp` → allowed
  - [ ] 7.5 `TestVMWebhook_Exclusivity_UpdateAddsSecondPlanMatch_Rejected` — create DRPlan A + DRPlan B, create VM matching only A, update VM to also match B → denied
  - [ ] 7.6 `TestVMWebhook_Exclusivity_UpdateRemovesLabels_Allowed` — create DRPlan A, create VM matching A, update VM to remove matching labels → allowed
  - [ ] 7.7 `TestVMWebhook_WaveConflict_CreateConflicting_Rejected` — create namespace-level namespace, DRPlan selecting VMs in namespace, create VM with wave "1", create second VM with wave "2" → second VM rejected
  - [ ] 7.8 `TestVMWebhook_WaveConflict_UpdateChangesWave_Rejected` — create namespace-level namespace, DRPlan, two VMs both wave "1", update one VM to wave "2" → rejected
  - [ ] 7.9 `TestVMWebhook_WaveConflict_SameWave_Allowed` — create namespace-level namespace, DRPlan, create two VMs both wave "1" → both allowed
  - [ ] 7.10 `TestVMWebhook_NoViolations_Allowed` — VM with labels matching one plan, no namespace-level consistency → allowed

- [ ] Task 8: Verify and finalize
  - [ ] 8.1 Run `make lint-fix` to auto-fix code style
  - [ ] 8.2 Run `make test` — all unit tests pass (including Story 2.3 tests with refactored exclusivity)
  - [ ] 8.3 Run `make integration` — all integration tests pass (including both DRPlan and VM webhook tests)
  - [ ] 8.4 Run `make manifests` — verify webhook configuration includes both DRPlan and VM webhooks, RBAC regenerated
  - [ ] 8.5 Verify Tier 1/2/3 documentation standards met (retro action item #2)

## Dev Notes

### Architecture Context

This story completes the admission validation for VM exclusivity (FR4) and namespace wave consistency (FR7) by covering the VM mutation path. Story 2.3 validates from the DRPlan side (when a DRPlan is created/updated). This story validates from the VM side (when a VM is created or its labels change).

**Why both sides are needed:**

```
Story 2.3 catches:                    Story 2.3.1 catches:
  DRPlan CREATE with overlapping        VM CREATE with labels matching
  vmSelector → denied                   2+ existing DRPlans → denied

  DRPlan UPDATE widening selector       VM UPDATE adding label that
  to overlap another plan → denied      now matches 2nd plan → denied

  DRPlan CREATE placing namespace-      VM CREATE/UPDATE changing wave
  level VMs in different waves → denied label in namespace-level NS → denied
```

Without Story 2.3.1, a user can bypass exclusivity by: (1) creating two non-overlapping DRPlans, then (2) adding a label to a VM that makes it match both. Story 2.3 never re-validates because no DRPlan was mutated. Only the controller would catch this asynchronously on the next reconcile.

### VM Webhook vs DRPlan Webhook: Technical Differences

| Aspect | DRPlan Webhook (2.3) | VM Webhook (2.3.1) |
|--------|---------------------|--------------------|
| Target resource | `soteria.io/v1alpha1/drplans` (aggregated API) | `kubevirt.io/v1/virtualmachines` (CRD) |
| Webhook interception | Requires `matchPolicy: Equivalent` for aggregated APIs | Standard CRD webhook — works on all K8s versions |
| Object decoding | Typed `DRPlan` struct | Typed `kubevirtv1.VirtualMachine` struct (`kubevirt.io/api` is a project dependency) |
| Exclusivity direction | "For this plan's VMs, do any belong to another plan?" | "For this VM's labels, do they match multiple plans?" |
| Wave conflict direction | "For this plan's VMs, do namespace-level VMs share a wave?" | "For this VM's new wave label, does it conflict with siblings?" |
| Shared webhook server | Port 9443 | Same server, different path |

### Shared Exclusivity Logic

The `ExclusivityChecker` in `pkg/admission/exclusivity.go` provides a single source of truth for "which DRPlans select a given VM?" Both webhooks use it:

- **DRPlan webhook (Story 2.3):** calls `CheckDRPlanExclusivity(ctx, plan, discoveredVMs)` — for each discovered VM, checks if it also matches any *other* DRPlan
- **VM webhook (Story 2.3.1):** calls `CheckVMExclusivity(ctx, vmName, vmNamespace, vmLabels)` — checks if the VM's labels match more than one DRPlan

The core primitive is `FindMatchingPlans(ctx, vmLabels, excludePlan)` which lists all DRPlans, parses their selectors, and returns those whose selector matches the given label set. This is O(N) where N = number of DRPlans (capped at 100 by NFR9).

### Typed VM Decoding

The VM webhook decodes VirtualMachine objects as typed `kubevirtv1.VirtualMachine` structs. The `kubevirt.io/api` module is a direct project dependency (added in Story 2.1). This gives compile-time safety and access to the full VM spec if needed in future validations. For admission validation, only standard Kubernetes metadata is used:

```go
vm.Name
vm.Namespace
vm.Labels
```

### Wave Conflict Check Algorithm

The wave conflict check for a VM mutation is:

1. Check if the VM's namespace has `soteria.io/consistency-level: namespace`
2. If not namespace-level, skip (no wave constraint)
3. Find which DRPlan(s) select this VM
4. For each matching DRPlan:
   a. Get the plan's `waveLabel` key
   b. Extract this VM's wave value from its labels
   c. Discover other VMs in the same namespace matching the same plan
   d. Check if any sibling has a different wave value
5. If conflict found, deny

This is more targeted than the DRPlan webhook's wave check: it only validates the *single VM being mutated* against its siblings, rather than checking all VMs in the plan.

### Webhook Configuration

Both the DRPlan webhook and VM webhook are entries in the same `ValidatingWebhookConfiguration` resource. They share:
- The same webhook Service (controller-manager, port 9443)
- The same TLS certificate (from cert-manager)
- The same `failurePolicy: Fail` (fail-closed)

They differ only in their `rules` (API group/resource) and `clientConfig.service.path`.

### Performance Considerations

The VM webhook runs on every VM CREATE/UPDATE. In a cluster with 5,000 VMs (NFR8), this could be frequent. Performance is bounded by:
- `FindMatchingPlans`: lists DRPlans (cached by informer) and parses selectors — O(N plans), N ≤ 100
- Wave conflict check: one VM discovery call per matching plan — typically 0-1 plans match

The selector parsing (`metav1.LabelSelectorAsSelector`) is lightweight. The DRPlan list is served from the controller-runtime cache. The overall webhook latency should be well under the default 10s webhook timeout.

**Optimization note:** If selector parsing per-request becomes a concern, the `ExclusivityChecker` could cache parsed selectors keyed by DRPlan resourceVersion. This is deferred.

### Dependency on Stories 2.1, 2.2, and 2.3

This story depends on:

- **Story 2.1:** `engine.VMDiscoverer` interface, `engine.VMReference` type, `kubevirt.io/api` dependency with typed `kubevirtv1.VirtualMachine` — for discovering sibling VMs in wave conflict checks
- **Story 2.2:** `engine.NamespaceLookup` interface, `ConsistencyLevelNamespace` constant — for namespace consistency checks
- **Story 2.3:** Webhook infrastructure (`config/webhook/`, cert-manager cert, `pkg/admission/setup.go`, manager wiring) — this story extends it

Task 1 (shared exclusivity helper) refactors Story 2.3's inline `checkVMExclusivity` into a shared module. If Story 2.3 isn't implemented yet, the shared module can be built first and Story 2.3 adopts it.

### Integration Test Setup

Integration tests extend the test suite from Story 2.3:
- The existing `test/integration/admission/suite_test.go` already configures envtest with webhook server, ScyllaDB, and kubevirt CRD
- This story adds the VM `ValidatingWebhookConfiguration` entry to the webhook install options
- VM tests create VMs via the envtest kube-apiserver and verify admission behavior

### Retro Action Items (Epic 1)

- **Action #2:** All new code must meet Tier 1-3 documentation standards
  - Tier 1: Update `pkg/admission/doc.go` to mention VM validation alongside DRPlan validation
  - Tier 2: Architecture block comments on `vm_validator.go` and `exclusivity.go`
  - Tier 3: Domain 'why' comments on why VM-side validation is needed (label change bypass scenario) and why wave conflict is checked from both sides

### References

- Architecture: `_bmad-output/planning-artifacts/architecture.md` — "Cross-Cutting Concerns: Admission validation"
- PRD: FR4 (VM exclusivity), FR5 (label-based plan membership), FR7 (same-wave enforcement), NFR15 (admission webhook validation)
- PRD Risk section: "Label discipline — admission webhooks validate label consistency"
- Story 2.1: DRPlan Controller & VM Auto-Discovery (prerequisite — provides `engine.VMDiscoverer`)
- Story 2.2: Namespace-Level Volume Consistency (prerequisite — provides `engine.NamespaceLookup`)
- Story 2.3: Admission Webhooks — DRPlan Validation (prerequisite — provides webhook infrastructure, DRPlan-side validation)
- Existing patterns: `pkg/admission/drplan_validator.go` (from Story 2.3), `pkg/engine/discovery.go` with typed VM access (from Story 2.1)

## Dev Agent Record

| Field | Value |
|-------|-------|
| Story file created | 2026-04-09 |
| Implementation started | 2026-04-10 |
| Implementation completed | 2026-04-10 |
| Code review requested | 2026-04-10 |
| Code review completed | 2026-04-11 |
| Status | done |
