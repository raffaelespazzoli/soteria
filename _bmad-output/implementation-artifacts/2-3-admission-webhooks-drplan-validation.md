# Story 2.3: Admission Webhooks — DRPlan Validation

Status: ready-for-dev

## Story

As a platform engineer,
I want the orchestrator to reject misconfigured DRPlan mutations at admission time,
So that VM exclusivity violations, namespace consistency conflicts, and invalid label selectors are caught before they cause problems.

## Acceptance Criteria

1. **Given** an existing DRPlan selecting VMs with label `app=erp`, **When** a second DRPlan is created with a `vmSelector` that would also match any of the same VMs, **Then** the admission webhook in `pkg/admission/drplan_validator.go` rejects the creation with a clear error: "VM <namespace>/<name> already belongs to DRPlan <namespace>/<existing-plan>" (FR4), **And** the error lists ALL conflicting VMs, not just the first.

2. **Given** a DRPlan being created or updated, **When** the `vmSelector` contains an invalid label selector expression, **Then** the admission webhook rejects the mutation with a descriptive validation error on the `spec.vmSelector` field path (NFR15), **And** the strategy-level validation in the aggregated API server also rejects as defense-in-depth.

3. **Given** a namespace with `soteria.io/consistency-level: namespace` annotation, **When** a DRPlan is created or updated that would place VMs from that namespace in different waves, **Then** the admission webhook rejects the mutation with an error identifying the conflicting VMs and their wave values (FR7).

4. **Given** a DRPlan with `maxConcurrentFailovers: 4`, **When** a namespace+wave group contains 6 VMs (exceeding maxConcurrentFailovers), **Then** the admission webhook rejects the mutation with an error: "maxConcurrentFailovers (4) is less than namespace+wave group size (6) for namespace <ns> wave <w>" (FR12 partial).

5. **Given** valid DRPlan creation or update, **When** no exclusivity, consistency, or selector violations exist, **Then** the admission webhook allows the mutation.

6. **Given** the webhook configuration, **When** deployed via `config/webhook/`, **Then** the webhook intercepts CREATE and UPDATE operations on DRPlan resources, **And** webhook TLS certificates are managed by cert-manager.

## Tasks / Subtasks

- [ ] Task 1: Implement ValidateDRPlan per-object field validation (AC: #2)
  - [ ] 1.1 Implement `ValidateDRPlan` in `pkg/apis/soteria.io/v1alpha1/validation.go`:
    - Validate `spec.vmSelector`: parse with `metav1.LabelSelectorAsSelector()`, reject if parsing fails with descriptive error on `field.NewPath("spec", "vmSelector")`
    - Validate `spec.vmSelector.matchLabels` / `matchExpressions`: reject empty selector (at least one matchLabels entry or matchExpressions entry required) — prevents accidental "select all VMs" plans
    - Validate `spec.waveLabel`: required, non-empty
    - Validate `spec.maxConcurrentFailovers`: must be > 0
  - [ ] 1.2 Implement `ValidateDRPlanUpdate(new, old *DRPlan)` in `validation.go`:
    - Call `ValidateDRPlan(new)` for all field validation
    - No immutability checks for now — add when DRExecution lifecycle exists (document as future work)
  - [ ] 1.3 Run `hack/verify-codegen.sh` to confirm no generated file changes needed (validation.go is hand-written)

- [ ] Task 2: Consolidate strategy validation with ValidateDRPlan (AC: #2)
  - [ ] 2.1 Update `pkg/registry/drplan/strategy.go` `Validate` method to call `soteriav1alpha1.ValidateDRPlan(plan)` and return its result, replacing the inline `waveLabel` / `maxConcurrentFailovers` checks
  - [ ] 2.2 Update `ValidateUpdate` to call `soteriav1alpha1.ValidateDRPlanUpdate(newPlan, oldPlan)`, passing both objects
  - [ ] 2.3 Verify existing unit and integration tests still pass after consolidation

- [ ] Task 3: Implement DRPlanValidator admission webhook handler (AC: #1, #3, #4, #5)
  - [ ] 3.1 Create `pkg/admission/drplan_validator.go` with Tier 2 architecture block comment explaining: the webhook validates cross-resource constraints (VM exclusivity, namespace consistency, throttle capacity) that require external state beyond the single object being admitted; per-object field validation is handled by the aggregated API server's strategy and is also checked here as defense-in-depth
  - [ ] 3.2 Define `DRPlanValidator` struct — fields:
    - `Client client.Reader` — for listing existing DRPlans
    - `VMDiscoverer engine.VMDiscoverer` — for discovering VMs matching a selector (from Story 2.1)
    - `NSLookup engine.NamespaceLookup` — for checking namespace consistency annotations (from Story 2.2)
    - `decoder admission.Decoder`
  - [ ] 3.3 Implement `Handle(ctx context.Context, req admission.Request) admission.Response`:
    - Only handle CREATE and UPDATE operations; allow all others
    - Decode the DRPlan from `req.Object.Raw`
    - Parse `spec.vmSelector` with `metav1.LabelSelectorAsSelector()`; if parse fails, deny with field validation error (defense-in-depth)
    - Call `VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)` to get the list of VMs matching the plan's selector
    - If no VMs discovered, allow (empty result is valid; controller will set Ready=False)
    - Run VM exclusivity check (3.4)
    - Run namespace consistency wave conflict check (3.5)
    - Run maxConcurrentFailovers capacity check (3.6)
    - Collect all denial reasons; if any exist, deny with combined message
    - If all checks pass, return `admission.Allowed("")`
  - [ ] 3.4 Implement VM exclusivity check using shared `ExclusivityChecker` (extracted in Story 2.3.1 Task 1; if 2.3.1 isn't started yet, implement inline and refactor later):
    - Call `ExclusivityChecker.CheckDRPlanExclusivity(ctx, plan, discoveredVMs)` — lists all DRPlans, parses their selectors, checks each discovered VM against other plans (excluding self)
    - **Shared helper location:** `pkg/admission/exclusivity.go` — provides `FindMatchingPlans`, `CheckVMExclusivity`, and `CheckDRPlanExclusivity` used by both the DRPlan webhook (this story) and the VM webhook (Story 2.3.1)
    - If implementing inline (2.3.1 not started): list all DRPlans across all namespaces via `Client.List(ctx, &DRPlanList{})`, for each existing DRPlan (exclude self by namespace+name), parse its `vmSelector`, call `VMDiscoverer.DiscoverVMs()`, build VM key sets (`namespace/name`), intersect with new plan's VMs
    - For each conflicting VM, record: `"VM <namespace>/<name> already belongs to DRPlan <namespace>/<plan-name>"`
    - Return all conflict messages
  - [ ] 3.5 Implement `checkNamespaceConsistency(ctx, plan *DRPlan, discoveredVMs []engine.VMReference) []string`:
    - Group discovered VMs by namespace
    - For each namespace containing VMs:
      - Call `NSLookup.GetConsistencyLevel(ctx, namespace)` to check annotation
      - If consistency level is `"namespace"`: collect distinct wave label values from VMs in this namespace
      - If more than one distinct wave value exists, record: `"namespace <ns> has consistency-level 'namespace' but VMs have conflicting wave labels: <vm1>=<wave1>, <vm2>=<wave2>"`
    - Return all conflict messages
  - [ ] 3.6 Implement `checkMaxConcurrentCapacity(plan *DRPlan, discoveredVMs []engine.VMReference, nsConsistency map[string]string) []string`:
    - For each namespace with consistency level `"namespace"`, group VMs by wave label to form namespace+wave groups
    - For each namespace+wave group, count VMs
    - If any group size exceeds `plan.Spec.MaxConcurrentFailovers`, record: `"maxConcurrentFailovers (<N>) is less than namespace+wave group size (<M>) for namespace <ns> wave <w>"`
    - Return all capacity error messages
  - [ ] 3.7 Add Tier 3 domain 'why' comments:
    - On VM exclusivity: a VM can only belong to one DRPlan because two plans trying to promote/demote the same VM's storage would cause data corruption or conflicting operations
    - On wave conflict: namespace-level VMs must share a wave because crash-consistent snapshots require atomic storage operations across all VMs in the namespace — different waves execute at different times, breaking consistency
    - On maxConcurrent admission check: validating at admission time prevents plans that appear valid but fail at execution time when they can't be chunked into DRGroups
  - [ ] 3.8 Structured logging:
    - `log.Info("Validating DRPlan admission", "name", req.Name, "namespace", req.Namespace, "operation", req.Operation)`
    - `log.Info("VM exclusivity check completed", "conflictCount", len(conflicts))`
    - `log.Info("Admission denied", "reasons", len(allDenials))` or `log.Info("Admission allowed")`

- [ ] Task 4: Create webhook registration and configuration (AC: #6)
  - [ ] 4.1 Create `pkg/admission/setup.go`:
    - Define webhook path constant: `ValidateDRPlanPath = "/validate-soteria-io-v1alpha1-drplan"`
    - Implement `SetupDRPlanWebhook(mgr ctrl.Manager, vmDiscoverer engine.VMDiscoverer, nsLookup engine.NamespaceLookup) error`:
      - Create `DRPlanValidator` with `mgr.GetClient()`, vmDiscoverer, nsLookup, and `admission.NewDecoder(mgr.GetScheme())`
      - Register: `mgr.GetWebhookServer().Register(ValidateDRPlanPath, &webhook.Admission{Handler: validator})`
  - [ ] 4.2 Create `config/webhook/manifests.yaml` — `ValidatingWebhookConfiguration`:
    - Name: `soteria-validating-webhook-configuration`
    - Webhook name: `vdrplan.kb.io`
    - Rules: apiGroups `["soteria.io"]`, apiVersions `["v1alpha1"]`, resources `["drplans"]`, operations `["CREATE", "UPDATE"]`
    - `failurePolicy: Fail` (fail-closed)
    - `sideEffects: None`
    - `admissionReviewVersions: ["v1"]`
    - `matchPolicy: Equivalent`
    - `clientConfig.service`: name `webhook-service`, namespace `system`, path `/validate-soteria-io-v1alpha1-drplan`
  - [ ] 4.3 Create `config/webhook/kustomization.yaml` — resources: `manifests.yaml`, `service.yaml`
  - [ ] 4.4 Create `config/webhook/service.yaml` — Service targeting controller-manager pods on port 443 → targetPort 9443 (webhook-server)
  - [ ] 4.5 Create `config/default/manager_webhook_patch.yaml` — strategic merge patch for the manager Deployment:
    - Add container port 9443 named `webhook-server`
    - Add volumeMount for `/tmp/k8s-webhook-server/serving-certs` (default cert-manager secret mount path)
    - Add volume projecting the `webhook-server-cert` Secret
  - [ ] 4.6 Update `config/default/kustomization.yaml`:
    - Uncomment `- ../webhook` resource
    - Uncomment `- ../certmanager` resource
    - Uncomment `manager_webhook_patch.yaml` patch
    - Uncomment `ValidatingWebhookConfiguration` cert-manager CA injection replacements
  - [ ] 4.7 Create `config/certmanager/webhook-serving-cert.yaml` — cert-manager Certificate:
    - Name: `serving-cert`
    - Secret name: `webhook-server-cert`
    - Issuer: reference existing `ca-issuer` from `config/certmanager/ca-issuer.yaml`
    - DNS names: webhook service FQDN (injected by kustomize replacements)
  - [ ] 4.8 Update `config/certmanager/kustomization.yaml` to include `webhook-serving-cert.yaml`
  - [ ] 4.9 Add RBAC markers to `pkg/admission/drplan_validator.go`:
    - `+kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch`

- [ ] Task 5: Wire webhook into cmd/soteria/main.go (AC: #6)
  - [ ] 5.1 Import `"github.com/soteria-project/soteria/pkg/admission"` and `"github.com/soteria-project/soteria/pkg/engine"` (if not already imported)
  - [ ] 5.2 After manager creation, instantiate `TypedVMDiscoverer` (from Story 2.1) using manager's client
  - [ ] 5.3 Instantiate `DefaultNamespaceLookup` (from Story 2.2) using manager's client
  - [ ] 5.4 Call `admission.SetupDRPlanWebhook(mgr, vmDiscoverer, nsLookup)` with error handling
  - [ ] 5.5 Verify `go build ./cmd/soteria/` succeeds
  - [ ] 5.6 Run `make manifests` to regenerate RBAC and webhook configuration

- [ ] Task 6: Unit tests for ValidateDRPlan (AC: #2)
  - [ ] 6.1 Create `pkg/apis/soteria.io/v1alpha1/validation_test.go`
  - [ ] 6.2 Table-driven `TestValidateDRPlan` covering:
    - Valid plan: valid selector `{matchLabels: {app: erp}}`, non-empty waveLabel, maxConcurrent=4 → no errors
    - Invalid vmSelector matchExpressions: operator `"InvalidOp"` → error on `spec.vmSelector`
    - Empty vmSelector: no matchLabels, no matchExpressions → error on `spec.vmSelector`
    - Empty waveLabel → error on `spec.waveLabel`
    - maxConcurrentFailovers = 0 → error on `spec.maxConcurrentFailovers`
    - maxConcurrentFailovers negative → error on `spec.maxConcurrentFailovers`
    - Multiple errors: empty waveLabel + maxConcurrent=0 → two errors returned
    - Valid matchExpressions: `{key: "env", operator: "In", values: ["prod"]}` → no errors
  - [ ] 6.3 Table-driven `TestValidateDRPlanUpdate` — verifies `ValidateDRPlan` is called for the new object

- [ ] Task 7: Unit tests for DRPlanValidator webhook handler (AC: #1, #3, #4, #5)
  - [ ] 7.1 Create `pkg/admission/drplan_validator_test.go`
  - [ ] 7.2 Create test helpers:
    - `MockVMDiscoverer` returning configurable VM lists per selector (or reuse mock from Story 2.1 tests)
    - `MockNamespaceLookup` returning configurable consistency levels per namespace (or reuse mock from Story 2.2 tests)
    - Use controller-runtime `fake.NewClientBuilder()` with `soteriav1alpha1` scheme for DRPlan listing
    - Helper to build `admission.Request` from a DRPlan object for CREATE/UPDATE operations
  - [ ] 7.3 Table-driven tests for VM exclusivity (AC: #1):
    - No existing plans → allowed
    - Existing plan with non-overlapping selector (different VMs) → allowed
    - Existing plan with overlapping selector (same VMs matched) → denied with exclusivity error listing all conflicting VMs
    - UPDATE existing plan (same namespace+name) → exclusivity check excludes self → allowed
    - Two existing plans, one overlaps → denied; non-overlapping plan ignored
    - Existing plan in different namespace with overlapping VMs → denied (VM exclusivity is cluster-wide)
  - [ ] 7.4 Table-driven tests for namespace consistency wave conflict (AC: #3):
    - VMs in namespace without consistency annotation → allowed
    - VMs in namespace-level namespace, all same wave label value → allowed
    - VMs in namespace-level namespace, different wave label values → denied with wave conflict error listing conflicting VMs and their waves
    - Mixed namespaces: one namespace-level (with conflict), one VM-level → denied (namespace-level conflict reported)
    - Single VM in namespace-level namespace → allowed (one VM cannot conflict)
  - [ ] 7.5 Table-driven tests for maxConcurrentFailovers capacity (AC: #4):
    - Namespace group (3 VMs) with maxConcurrent=4 → allowed
    - Namespace group (6 VMs) with maxConcurrent=4 → denied with capacity error message
    - Namespace group exactly equals maxConcurrent → allowed (boundary)
    - Multiple namespace groups, one exceeds → denied (reports the exceeding group)
    - All VM-level namespaces (no namespace groups) → allowed (individual VMs always fit)
  - [ ] 7.6 Test: valid plan, no existing plans, no namespace-level namespaces → allowed
  - [ ] 7.7 Test: DELETE operation → allowed (webhook only validates CREATE/UPDATE)
  - [ ] 7.8 Test: plan with no discovered VMs → allowed (controller handles empty discovery)

- [ ] Task 8: Integration tests (AC: #1, #2, #3, #4, #5, #6)
  - [ ] 8.1 Create `test/integration/admission/suite_test.go` with `//go:build integration` tag:
    - Start ScyllaDB testcontainer (reuse pattern from `test/integration/apiserver/suite_test.go`)
    - Start aggregated API server with ScyllaDB store factory
    - Configure envtest `Environment` with `WebhookInstallOptions` for webhook server
    - Register kubevirt VirtualMachine CRD in envtest for VM resources
    - Apply `ValidatingWebhookConfiguration` to envtest kube-apiserver
    - Create rest.Config clients for DRPlan (via aggregated API) and VMs/namespaces (via envtest kube-apiserver)
  - [ ] 8.2 Create `test/integration/admission/drplan_webhook_test.go` with `//go:build integration` tag
  - [ ] 8.3 `TestDRPlanWebhook_VMExclusivity_Rejected` — create 3 VMs with `app=erp`, create DRPlan A selecting `app=erp`, create DRPlan B also selecting `app=erp` → expect admission denied with exclusivity error
  - [ ] 8.4 `TestDRPlanWebhook_VMExclusivity_NonOverlapping_Allowed` — create VMs with `app=erp` and `app=crm`, create DRPlan A selecting `app=erp`, create DRPlan B selecting `app=crm` → both succeed
  - [ ] 8.5 `TestDRPlanWebhook_InvalidSelector_Rejected` — create DRPlan with malformed vmSelector matchExpressions → expect validation error
  - [ ] 8.6 `TestDRPlanWebhook_WaveConflict_Rejected` — create namespace with `soteria.io/consistency-level: namespace`, create VMs in that namespace with wave labels "1" and "2", create DRPlan → expect admission denied with wave conflict error
  - [ ] 8.7 `TestDRPlanWebhook_WaveConflict_SameWave_Allowed` — create namespace-level namespace, VMs all with wave label "1", create DRPlan → allowed
  - [ ] 8.8 `TestDRPlanWebhook_MaxConcurrentExceeded_Rejected` — create namespace-level namespace, 6 VMs same wave, create DRPlan with maxConcurrentFailovers=4 → expect admission denied with capacity error
  - [ ] 8.9 `TestDRPlanWebhook_ValidPlan_Allowed` — valid plan, non-overlapping VMs, no namespace-level consistency → allowed
  - [ ] 8.10 `TestDRPlanWebhook_Update_ExclusivityExcludesSelf` — create DRPlan A, update DRPlan A with same selector → allowed (self-exclusion)

- [ ] Task 9: Verify and finalize
  - [ ] 9.1 Run `make lint-fix` to auto-fix code style
  - [ ] 9.2 Run `make test` — all unit tests pass
  - [ ] 9.3 Run `make integration` — all integration tests pass (including new admission tests)
  - [ ] 9.4 Run `make manifests` — verify webhook configuration and RBAC regenerated
  - [ ] 9.5 Verify `config/default/kustomization.yaml` webhook and certmanager sections are uncommented
  - [ ] 9.6 Verify Tier 1/2/3 documentation standards met (retro action item #2)

## Dev Notes

### Architecture Context

This story adds the admission validation layer for DRPlan mutations. Stories 2.1 and 2.2 implement controller-side validation (setting `Ready=False` conditions on reconcile). This story prevents misconfigured plans from being created in the first place — catching errors at admission time rather than asynchronously during reconciliation.

**Companion story:** Story 2.3.1 (VM Label Validation Webhook) covers the same constraints from the VM mutation side — rejecting VM CREATE/UPDATE operations that would violate exclusivity or wave consistency. Together, Stories 2.3 and 2.3.1 enforce FR4 and FR7 regardless of which resource is mutated. The VM exclusivity check logic is shared via `pkg/admission/exclusivity.go`.

**Two validation layers (defense-in-depth):**
1. **Admission webhook** (in controller-runtime manager): cross-resource validation requiring external state — VM exclusivity, namespace consistency, throttle capacity. Runs before the request reaches the aggregated API server.
2. **Strategy-level** (in aggregated API server): per-object field validation via `ValidateDRPlan` — runs synchronously inside the storage pipeline. Catches basic errors even if the webhook is temporarily unavailable.

### Webhook Architecture

The webhook handler runs on the controller-runtime manager's webhook server (port 9443), registered as a raw `admission.Handler`. The kube-apiserver calls this webhook for DRPlan CREATE/UPDATE operations via a `ValidatingWebhookConfiguration`.

```
Client (kubectl/Console)
  → kube-apiserver
  → ValidatingWebhookConfiguration match (matchPolicy: Equivalent)
  → webhook call → controller-runtime webhook server (port 9443)
  → DRPlanValidator.Handle()
    ├── VM exclusivity: list DRPlans + discover VMs for each
    ├── Namespace consistency: check annotations + wave labels
    └── Throttle capacity: namespace group size vs maxConcurrentFailovers
  → admission.Allowed / admission.Denied
  → kube-apiserver proxy → Aggregated API Server
  → Strategy.Validate() (field validation)
  → ScyllaDB
```

**Aggregated API and webhooks:** DRPlan resources are served by the aggregated API server, not CRDs. The kube-apiserver supports calling `ValidatingWebhookConfiguration` webhooks for aggregated API resources when `matchPolicy: Equivalent` is set (Kubernetes 1.28+, stable by the target version). If this mechanism doesn't work in the deployment environment, the fallback is to register an in-process admission plugin within the aggregated API server's admission chain — the `DRPlanValidator` logic is the same; only the wiring changes (see "Fallback" section below).

### VM Exclusivity Algorithm

The exclusivity check lists all existing DRPlans and discovers VMs for each plan's selector. For a cluster with N plans and M VMs:

1. List all DRPlans: `Client.List()` — cached by controller-runtime informer, O(N)
2. For each existing DRPlan (excluding self): `VMDiscoverer.DiscoverVMs()` — one dynamic list per plan
3. Build VM key sets (`namespace/name`) and intersect

Performance is acceptable because:
- NFR9 limits plans to 100 — N ≤ 100
- VM discovery per plan is bounded by the plan's selector scope
- The controller-runtime client caches DRPlan and namespace lists via informers
- The dynamic client list for VMs is a direct kube-apiserver call (not cached), but each call is scoped by label selector

For UPDATE operations, the plan being updated is excluded from the exclusivity check by matching `namespace/name`.

**Optimization note:** If N grows large, the exclusivity check could be optimized by maintaining an in-memory VM-to-DRPlan index. This is deferred — the current approach is correct and performant at target scale.

### Namespace Consistency at Admission Time

The wave conflict detection at admission time mirrors the controller-side detection (Story 2.2) but runs proactively:

1. Discover VMs matching the plan's selector
2. Group VMs by namespace
3. For each namespace, query consistency level via `NSLookup.GetConsistencyLevel()`
4. For namespaces with `"namespace"` level: verify all VMs have the same wave label value
5. Also check that each namespace+wave group size ≤ `maxConcurrentFailovers`

This catches conflicts BEFORE they cause the controller to set `Ready=False`, providing immediate feedback to the user during `kubectl apply`.

### Webhook TLS and cert-manager

The webhook server requires TLS. Certificates are managed by cert-manager:

- `config/certmanager/webhook-serving-cert.yaml` — cert-manager `Certificate` for the webhook Service DNS name
- cert-manager's CA injector patches the `ValidatingWebhookConfiguration` with the CA bundle (via `cert-manager.io/inject-ca-from` annotation)
- The controller manager Deployment mounts the TLS cert/key from the Secret created by cert-manager
- The existing `config/certmanager/ca-issuer.yaml` serves as the issuer for webhook certs (self-signed CA, same as ScyllaDB certs)

The webhook cert is separate from the ScyllaDB client certs (`scylladb-serving-cert.yaml`, `scylladb-client-cert.yaml`) — they serve different purposes and may have different lifetimes.

### Dependency on Stories 2.1 and 2.2

This story reuses interfaces and implementations from Stories 2.1 and 2.2:

- **From Story 2.1:** `engine.VMDiscoverer` interface, `engine.VMReference` type, `engine.TypedVMDiscoverer` struct, `kubevirt.io/api` dependency with typed `kubevirtv1.VirtualMachine`
- **From Story 2.2:** `engine.NamespaceLookup` interface, `engine.DefaultNamespaceLookup` struct, `ConsistencyLevelNamespace` constant

The `SetupDRPlanWebhook` function accepts these as constructor parameters, enabling mock injection for tests. In `cmd/soteria/main.go`, the same `TypedVMDiscoverer` and `DefaultNamespaceLookup` instances can be shared between the DRPlan controller (Stories 2.1/2.2) and the webhook.

**If Stories 2.1/2.2 aren't complete when development starts:** implement the VM listing and namespace annotation checks inline using the controller-runtime client with typed `kubevirtv1.VirtualMachineList`. Refactor to use the engine interfaces when Stories 2.1/2.2 land. The `DRPlanValidator` struct fields should still use the interfaces for testability.

### Error Message Format

Admission rejection messages follow Kubernetes conventions — concise, complete, and actionable:

- **VM exclusivity:** `"VM default/erp-db already belongs to DRPlan default/erp-plan-primary"`
- **Wave conflict:** `"namespace erp-database has consistency-level 'namespace' but VMs have conflicting wave labels: erp-db-1=1, erp-db-2=2"`
- **Throttle capacity:** `"maxConcurrentFailovers (4) is less than namespace+wave group size (6) for namespace erp-database wave 1"`

Multiple violations are collected and returned together in a single denial response. The webhook never short-circuits after the first error — all applicable checks run and all violations are reported.

### Fallback: In-Process Admission Plugin

If `ValidatingWebhookConfiguration` with `matchPolicy: Equivalent` does not intercept aggregated API requests in the deployment's Kubernetes version, the fallback is to register `DRPlanValidator` as a `k8s.io/apiserver` admission plugin within the aggregated API server:

1. Register the plugin via `admission.RegisterPlugin("DRPlanValidation", ...)` in `pkg/admission/register.go`
2. Add `"DRPlanValidation"` to `RecommendedOptions.Admission.RecommendedPluginOrder` in `pkg/apiserver/options.go`
3. Inject dependencies (clients) via `WantsExternalKubeClientSet` / custom initialization
4. Remove `config/webhook/` resources (not needed for in-process admission)

The `DRPlanValidator.Handle()` logic is identical in both approaches — only the admission request/response envelope changes (`admission.Request` for controller-runtime webhook vs `admission.Attributes` for k8s.io/apiserver plugin).

### RBAC Requirements

The webhook handler needs to read resources from multiple API groups:

| Resource | Verbs | Reason |
|----------|-------|--------|
| `soteria.io/drplans` | `get`, `list`, `watch` | List existing DRPlans for exclusivity checks |
| `kubevirt.io/virtualmachines` | `get`, `list`, `watch` | Discover VMs matching plan selectors |
| `""/namespaces` | `get`, `list`, `watch` | Read namespace annotations for consistency checks |

The `+kubebuilder:rbac` markers on `drplan_validator.go` generate ClusterRole rules via `make manifests`. These are in addition to the RBAC already generated for the DRPlan controller (Stories 2.1/2.2).

### Integration Test Setup

Integration tests combine the envtest webhook infrastructure with the existing ScyllaDB testcontainer pattern:

1. **envtest `Environment`** — provides kube-apiserver + etcd for namespaces, VMs (via kubevirt CRD), and webhook interception
2. **`WebhookInstallOptions`** — starts the webhook server, installs `ValidatingWebhookConfiguration`, handles TLS for the test
3. **ScyllaDB testcontainer** — backing store for the aggregated API server (DRPlan resources)
4. **Aggregated API server** — in-process, registered as APIService in envtest's kube-apiserver

Reference: `test/integration/apiserver/suite_test.go` for ScyllaDB + API server setup. The admission test suite extends this with envtest's webhook support.

### Retro Action Items (Epic 1)

- **Action #2:** All new code must meet Tier 1-3 documentation standards
  - Tier 1: Update `pkg/admission/doc.go` with 3-5 sentence godoc covering webhook validation purpose, what it validates (exclusivity, consistency, capacity), and relationship to strategy-level validation
  - Tier 2: Architecture block comment on `drplan_validator.go` explaining the validation flow, external state dependencies, and defense-in-depth strategy
  - Tier 3: Domain 'why' comments on VM exclusivity (data corruption risk), wave conflict (consistency guarantee), and throttle validation (fail-fast over fail-at-execution)

### References

- Architecture: `_bmad-output/planning-artifacts/architecture.md` — "Cross-Cutting Concerns: Admission validation", "Requirements to Structure Mapping" for `pkg/admission/`
- PRD: FR4 (VM exclusivity), FR7 (same-wave enforcement), FR12 (chunking respects namespace groups), NFR15 (admission webhook validation)
- Story 2.1: DRPlan Controller & VM Auto-Discovery (prerequisite — provides `engine.VMDiscoverer`, `engine.VMReference`, `engine.TypedVMDiscoverer`, `kubevirt.io/api` dependency)
- Story 2.2: Namespace-Level Volume Consistency (prerequisite — provides `engine.NamespaceLookup`, `engine.DefaultNamespaceLookup`, `ConsistencyLevelNamespace`)
- Story 2.3.1: VM Label Validation Webhook (companion — covers VM-side admission validation, shares `pkg/admission/exclusivity.go`)
- Story 2.4: Pre-flight Plan Composition Check — will reuse validation patterns from this story
- Existing patterns: `pkg/registry/drplan/strategy.go` (strategy validation), `pkg/apiserver/options.go` (aggregated API server admission chain)
- controller-runtime webhook docs: https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html
- Kubernetes admission webhooks: https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/

## Dev Agent Record

| Field | Value |
|-------|-------|
| Story file created | 2026-04-09 |
| Implementation started | 2026-04-10 |
| Implementation completed | 2026-04-10 |
| Code review requested | 2026-04-10 |
| Code review completed | 2026-04-10 |
| Status | done |
