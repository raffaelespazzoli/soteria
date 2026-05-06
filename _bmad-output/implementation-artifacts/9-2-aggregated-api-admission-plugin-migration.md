# Story 9.2: Aggregated API Admission Plugin Migration

Status: ready-for-dev

## Story

As a platform engineer,
I want DRExecution and DRPlan admission validation to run in-process within the aggregated API server,
So that cross-object checks (concurrency gate, phase transition, SitesInSync) are enforced reliably without depending on kube-apiserver ValidatingWebhookConfiguration — which does not fire for aggregated API resources.

## Acceptance Criteria

1. **AC1 — In-process admission plugin:** A new `pkg/admission/plugin.go` implements `k8s.io/apiserver/pkg/admission.ValidationInterface` with the same validation logic currently in `DRExecutionValidator.Handle` and `DRPlanValidator.Handle`. The plugin is registered with the aggregated API server's `RecommendedConfig` admission chain and runs in-process during the request lifecycle — no HTTP roundtrip to a webhook server.

2. **AC2 — DRExecution CREATE validation:** The admission plugin performs all cross-object checks currently in `DRExecutionValidator.Handle`: `planName` required, `mode` valid, DRPlan exists, `ActiveExecution != ""` concurrency gate, phase transition valid (`engine.Transition`), `SitesInSync=False` blocks. It uses the ScyllaDB-backed storage (via REST storage `Get`) to read fresh DRPlan state — not the controller-runtime cached reader.

3. **AC3 — DRPlan CREATE/UPDATE validation:** The admission plugin performs field validation by calling `soteriav1alpha1.ValidateDRPlan`/`ValidateDRPlanUpdate`. This is defense-in-depth alongside the existing registry strategy validation.

4. **AC4 — VWC cleanup:** The `+kubebuilder:webhook` markers on `DRExecutionValidator` and `DRPlanValidator` are removed (or the structs are refactored). `make manifests` no longer regenerates the dead `vdrexecution.kb.io` and `vdrplan.kb.io` entries in `config/webhook/manifests.yaml`. The `vvm.kb.io` entry for VirtualMachines is retained.

5. **AC5 — Controller-runtime webhook cleanup:** `SetupDRPlanWebhook` and `SetupDRExecutionWebhook` are removed from `pkg/admission/setup.go`. `SetupVMWebhook` is retained. `cmd/soteria/main.go` no longer calls the removed setup functions.

6. **AC6 — Webhook infrastructure retained:** The webhook Service, cert-manager Certificate, CA injection, and `config/default/kustomization.yaml` patches remain because the VM webhook (`vvm.kb.io`) still requires the controller-runtime webhook server.

7. **AC7 — Tests:** New unit tests verify the admission plugin rejects/allows the same scenarios as the current webhook tests (table-driven, same test cases). Integration tests verify the admission plugin runs in-process within the aggregated API server. The `test/integration/admission/suite_test.go` DRExecution-related assertions (if any) are updated. All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Create in-process admission plugin (AC: #1, #2, #3)
  - [ ] 1.1 Create `pkg/admission/plugin.go` — implement `admission.ValidationInterface` with `Validate(ctx, a, o) error`
  - [ ] 1.2 Implement DRExecution CREATE validation — extract plan via REST storage `Get`, perform concurrency gate, phase transition, SitesInSync checks
  - [ ] 1.3 Implement DRPlan CREATE/UPDATE validation — call `ValidateDRPlan`/`ValidateDRPlanUpdate`
  - [ ] 1.4 Register plugin with `admission.Plugins.Register` and add to admission chain

- [ ] Task 2: Wire plugin into aggregated API server (AC: #1)
  - [ ] 2.1 In `pkg/apiserver/options.go`, register the Soteria admission plugin before `RecommendedOptions.ApplyTo`
  - [ ] 2.2 Add the plugin name to the `RecommendedOptions.Admission.RecommendedPluginOrder` and `EnablePlugins`
  - [ ] 2.3 Provide a plugin initializer that injects REST storage for DRPlan lookups

- [ ] Task 3: Remove DRPlan and DRExecution webhook markers and setup (AC: #4, #5)
  - [ ] 3.1 Remove `+kubebuilder:webhook` marker from `DRExecutionValidator` in `drexecution_validator.go`
  - [ ] 3.2 Remove `+kubebuilder:webhook` marker from `DRPlanValidator` in `drplan_validator.go`
  - [ ] 3.3 Remove `SetupDRPlanWebhook` and `SetupDRExecutionWebhook` from `setup.go`; remove `ValidateDRPlanPath` and `ValidateDRExecutionPath` constants
  - [ ] 3.4 Remove `admission.SetupDRPlanWebhook(mgr)` and `admission.SetupDRExecutionWebhook(mgr)` calls from `cmd/soteria/main.go`
  - [ ] 3.5 Run `make manifests` — verify only `vvm.kb.io` remains in `config/webhook/manifests.yaml`

- [ ] Task 4: Verify webhook infrastructure retained for VM webhook (AC: #6)
  - [ ] 4.1 Confirm `config/default/kustomization.yaml` still includes webhook/certmanager resources
  - [ ] 4.2 Confirm `manager_webhook_patch.yaml` still adds port 9443 and cert volume mount

- [ ] Task 5: Update doc.go (AC: #1)
  - [ ] 5.1 Update `pkg/admission/doc.go` to reflect migration — DRPlan and DRExecution validation moved to in-process admission plugin; VM webhook remains on controller-runtime path

- [ ] Task 6: Unit tests for admission plugin (AC: #7)
  - [ ] 6.1 Create `pkg/admission/plugin_test.go` with table-driven tests covering the same scenarios as `drexecution_validator_test.go` (planName missing, invalid mode, plan not found, active execution, invalid transition, SitesInSync false, allowed)
  - [ ] 6.2 Add DRPlan validation tests matching `drplan_validator_test.go` scenarios (create valid, update immutable sites, invalid maxConcurrentFailovers)
  - [ ] 6.3 Verify the old webhook test files still compile (they test the webhook structs directly — retain as legacy validation if structs remain, or remove if structs are deleted)

- [ ] Task 7: Integration tests (AC: #7)
  - [ ] 7.1 Update `test/integration/admission/suite_test.go` — the DRExecution webhook was never installed in integration tests (only DRPlan + VM), so minimal changes expected
  - [ ] 7.2 Add integration test verifying DRExecution admission runs in-process via the aggregated API server path (may require `test/integration/apiserver/` suite)

- [ ] Task 8: Run make manifests generate, lint, test (AC: #7)
  - [ ] 8.1 `make manifests generate` — zero errors
  - [ ] 8.2 `make lint-fix` — zero new lint errors
  - [ ] 8.3 `make test` — all tests pass, zero regressions

## Dev Notes

### Scope & Approach

This is a Go backend story. Changes span `pkg/admission/`, `pkg/apiserver/`, `cmd/soteria/`, `config/webhook/`, and tests. No console-plugin changes. The core challenge is implementing a `k8s.io/apiserver/pkg/admission.ValidationInterface` plugin that replaces the controller-runtime webhook handlers for DRExecution and DRPlan, registering it with the aggregated API server's admission chain, and cleanly removing the dead webhook infrastructure.

**Change pattern:** Create admission plugin → register with API server → remove webhook markers/setup → update manifests → tests.

### Critical: k8s.io/apiserver Admission Plugin Interface

The plugin must implement `admission.ValidationInterface` from `k8s.io/apiserver/pkg/admission` (version v0.35.0 in go.mod):

```go
import (
    "k8s.io/apiserver/pkg/admission"
)

type ValidationInterface interface {
    Interface
    Validate(ctx context.Context, a Attributes, o ObjectInterfaces) error
}

type Interface interface {
    Handles(operation Operation) bool
}
```

The plugin should embed `*admission.Handler` (via `admission.NewHandler(ops...)`) for the `Handles()` implementation.

### Critical: Plugin Registration Pattern

k8s.io/apiserver uses a plugin registry pattern. The plugin must:

1. **Define a plugin name** — e.g., `const PluginName = "SoteriaValidation"`
2. **Register with the global registry** — `admission.Plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {...})`
3. **Be added to the admission chain** — via `RecommendedOptions.Admission.RecommendedPluginOrder` and `RecommendedOptions.Admission.EnablePlugins`

The registration must happen before `RecommendedOptions.ApplyTo(serverConfig)` in `options.go`.

### Critical: DRPlan Lookup — REST Storage, Not client.Reader

The current `DRExecutionValidator` uses `mgr.GetAPIReader()` (uncached controller-runtime reader that goes through kube-apiserver). The in-process admission plugin runs INSIDE the aggregated API server, so it cannot use controller-runtime clients.

**Two options for DRPlan lookups:**

**Option A — Plugin Initializer with REST storage:** Use a `WantsRESTStorageProvider` plugin initializer that provides the DRPlan REST storage. The plugin calls `storage.Get()` directly to read the DRPlan from ScyllaDB. This avoids any network hop.

**Option B — Plugin Initializer with informer/lister:** Use a `WantsExternalKubeInformerFactory` or custom initializer. However, since the admission plugin needs the absolute latest DRPlan state (concurrency gate!), a cached informer is insufficient — the stale-cache race that the webhook's `GetAPIReader()` avoids would reappear.

**Recommended: Option A** — direct REST storage `Get`. The plugin needs a `WantsInternalRESTStorageProvider` interface (custom `admission.InitializationValidator`) and a plugin initializer that injects the DRPlan `rest.Storage` at server startup. The `Validate` method calls `drplanStorage.Get(ctx, planName, &metav1.GetOptions{})` to read fresh state.

Implementation sketch:

```go
type SoteriaAdmissionPlugin struct {
    *admission.Handler
    drplanStorage rest.Getter
}

func (p *SoteriaAdmissionPlugin) SetDRPlanStorage(s rest.Getter) {
    p.drplanStorage = s
}

func (p *SoteriaAdmissionPlugin) ValidateInitialization() error {
    if p.drplanStorage == nil {
        return fmt.Errorf("drplan storage not initialized")
    }
    return nil
}
```

### Critical: Plugin Initializer Pattern

The aggregated API server needs to inject the DRPlan REST storage into the plugin at startup. Use a custom `admission.PluginInitializer`:

```go
type soteriaPluginInitializer struct {
    drplanStorage rest.Getter
}

func (i *soteriaPluginInitializer) Initialize(plugin admission.Interface) {
    if wants, ok := plugin.(WantsDRPlanStorage); ok {
        wants.SetDRPlanStorage(i.drplanStorage)
    }
}
```

The initializer is created in `apiserver.go` (or `options.go`) after `NewREST` returns the DRPlan storage, and added to `serverConfig.AdmissionControl` chain.

**Timing in `options.go` → `Config()` → `New()`:**

- `Config()` registers the plugin name and applies `RecommendedOptions`
- `New()` creates REST storage via `NewREST`, then creates the initializer and initializes the admission chain

Check the `k8s.io/apiserver` `server.Config.AdmissionControl` field and `AdmissionControl` admission chain setup in `GenericConfig.New()` for the injection point. The `RecommendedConfig` has `AdmissionControl admission.Interface` which is the configured chain after `ApplyTo`.

### Critical: Admission Attributes Object Extraction

Inside `Validate(ctx, a Attributes, o ObjectInterfaces)`:

```go
func (p *SoteriaAdmissionPlugin) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
    if a.GetResource().GroupResource() == soteriav1alpha1.Resource("drexecutions") {
        return p.validateDRExecution(ctx, a)
    }
    if a.GetResource().GroupResource() == soteriav1alpha1.Resource("drplans") {
        return p.validateDRPlan(ctx, a)
    }
    return nil
}
```

Extract the object from attributes:

```go
exec, ok := a.GetObject().(*soteriav1alpha1.DRExecution)
// For updates, old object:
oldPlan, ok := a.GetOldObject().(*soteriav1alpha1.DRPlan)
```

**Operation type:** `a.GetOperation()` returns `admission.Operation` (Create, Update, Delete, Connect).

### Critical: What NOT to Change — VM Webhook Stays

The `VMValidator` webhook MUST remain on the controller-runtime webhook path because:
- VMs are standard KubeVirt CRDs served by the kube-apiserver (not the aggregated API)
- `ValidatingWebhookConfiguration` works correctly for standard CRD resources
- The VM webhook has different dependencies (VMDiscoverer, NamespaceLookup) that don't exist inside the aggregated API server

**Retain:**
- `VMValidator` struct and `Handle()` method
- `SetupVMWebhook()` function
- `ValidateVMPath` constant
- `vvm.kb.io` entry in `config/webhook/manifests.yaml`
- `admission.SetupVMWebhook(mgr, nsLookup, vmDiscoverer)` in `main.go`
- All VM webhook tests (unit + integration)

### Critical: DRExecution Validation Logic to Port

Port the exact logic from `drexecution_validator.go` lines 55-121:

1. **Operation filter:** Only validate `CREATE` (`admission.Create` in apiserver terms)
2. **Field validation:** `spec.planName` non-empty; `spec.mode` ∈ {planned_migration, disaster, reprotect}
3. **DRPlan lookup:** `drplanStorage.Get(ctx, exec.Spec.PlanName, &metav1.GetOptions{})` — return `admission.NewForbidden` if NotFound
4. **Concurrency gate:** `plan.Status.ActiveExecution != ""` → reject
5. **Phase transition:** `engine.Transition(plan.Status.Phase, exec.Spec.Mode)` → reject with `engine.ValidStartingPhases`
6. **SitesInSync:** `meta.FindStatusCondition(plan.Status.Conditions, "SitesInSync")` → reject if `Status == False`

### Critical: DRPlan Validation Logic to Port

Port from `drplan_validator.go` lines 55-85:

1. **Operation filter:** Only validate `CREATE` and `UPDATE`
2. **Create:** `soteriav1alpha1.ValidateDRPlan(plan)` → return aggregated errors
3. **Update:** `soteriav1alpha1.ValidateDRPlanUpdate(plan, oldPlan)` → return aggregated errors

### Critical: Error Return Convention

The webhook used `admission.Denied(msg)` / `admission.Errored(statusCode, err)`. The apiserver plugin uses:
- **Denied → `admission.NewForbidden(a, fmt.Errorf(msg))`** — returns 403 Forbidden
- **Server error → `apierrors.NewInternalError(err)`** — returns 500

### Critical: Existing Registry Strategy Validation is NOT Replaced

The DRExecution registry strategy (`pkg/registry/drexecution/strategy.go`) already validates field-level constraints (`planName` required, `mode` valid) via its own `Validate()` method. The admission plugin adds **cross-object checks** (DRPlan existence, concurrency gate, phase transition, SitesInSync) that the strategy cannot perform because it has no access to other resources. Both layers run — the admission plugin runs first (before storage), then the strategy's `Validate` runs during the storage `Create` call.

### Critical: Keeping Old Validator Structs vs Deleting

**Recommended: Keep the old validator structs but remove the webhook markers.** The validator structs (`DRExecutionValidator`, `DRPlanValidator`) are still useful as reusable validation logic that can be called from the admission plugin. The admission plugin can delegate to them, or the logic can be extracted into shared helper functions.

Alternatively, if the structs are deleted, move the cross-object validation logic into the plugin directly and update imports.

### Critical: `make manifests` Regeneration

After removing `+kubebuilder:webhook` markers from `drexecution_validator.go` and `drplan_validator.go`, running `make manifests` will regenerate `config/webhook/manifests.yaml` with **only** the `vvm.kb.io` webhook entry. Verify:
- `vdrexecution.kb.io` entry is GONE
- `vdrplan.kb.io` entry is GONE
- `vvm.kb.io` entry is RETAINED

### Critical: Integration Test Impact

The existing integration test suite (`test/integration/admission/suite_test.go`) only installs DRPlan and VM webhooks — it **never installed the DRExecution webhook** (lines 86-143 show only `vdrplan.kb.io` and `vvm.kb.io` in `ValidatingWebhooks`). So DRExecution admission was never tested via integration tests against the webhook path.

For in-process admission plugin integration tests, you would need to boot the aggregated API server with the admission plugin enabled. This can be done in the existing `test/integration/apiserver/` suite which already starts the aggregated API server with Scylla (testcontainers).

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `admission.ValidationInterface` | `k8s.io/apiserver/pkg/admission` | Implement for plugin |
| `admission.NewHandler(ops...)` | `k8s.io/apiserver/pkg/admission` | Embed in plugin struct |
| `admission.Plugins.Register` | `k8s.io/apiserver/pkg/admission` | Register plugin |
| `admission.PluginInitializer` | `k8s.io/apiserver/pkg/admission` | Inject REST storage |
| `rest.Getter` interface | `k8s.io/apiserver/pkg/registry/rest` | DRPlan storage injection |
| `admission.NewForbidden` | `k8s.io/apiserver/pkg/admission` | Error return for denied requests |
| `ValidateDRPlan`/`ValidateDRPlanUpdate` | `pkg/apis/soteria.io/v1alpha1/validation.go` | Reuse for DRPlan field validation |
| `engine.Transition`/`engine.ValidStartingPhases` | `pkg/engine/statemachine.go` | Reuse for phase transition checks |
| `DRExecutionValidator.Handle` logic | `pkg/admission/drexecution_validator.go` | Port cross-object checks |
| `SoteriaServerOptions.Config()` | `pkg/apiserver/options.go` | Extend with plugin registration |
| `CompletedConfig.New()` | `pkg/apiserver/apiserver.go` | Inject plugin initializer |
| `drplanregistry.NewREST` | `pkg/registry/drplan/rest.go` | Get DRPlan REST storage for plugin |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/admission/plugin.go` | New file — admission plugin implementing `ValidationInterface` | Core migration (~120 lines) |
| `pkg/admission/initializer.go` | New file — plugin initializer for REST storage injection | Dependency injection (~30 lines) |
| `pkg/admission/doc.go` | Update — reflect DRPlan/DRExecution migration to in-process plugin | ~5 lines changed |
| `pkg/admission/drexecution_validator.go` | Remove `+kubebuilder:webhook` marker; optionally keep struct as helper | ~1 line removed |
| `pkg/admission/drplan_validator.go` | Remove `+kubebuilder:webhook` marker; optionally keep struct as helper | ~1 line removed |
| `pkg/admission/setup.go` | Remove `SetupDRPlanWebhook`, `SetupDRExecutionWebhook`, their constants | ~20 lines removed |
| `pkg/apiserver/options.go` | Register plugin, add to admission chain order | ~15 lines |
| `pkg/apiserver/apiserver.go` | Create plugin initializer with DRPlan REST storage, inject into admission chain | ~20 lines |
| `cmd/soteria/main.go` | Remove `SetupDRPlanWebhook` and `SetupDRExecutionWebhook` calls | ~8 lines removed |
| `config/webhook/manifests.yaml` | Auto-generated — loses `vdrexecution.kb.io` and `vdrplan.kb.io` | `make manifests` |
| `pkg/admission/plugin_test.go` | New file — unit tests for admission plugin | ~200 lines |
| `test/integration/admission/suite_test.go` | Minor update — DRPlan VWC entry may need removal if DRPlan validation moves fully in-process | ~10 lines changed |

### Execution Order

1. Task 1 (admission plugin + initializer) — core migration logic
2. Task 2 (wire into aggregated API server) — registration and initialization
3. Task 6 (unit tests for plugin) — validate behavior before wiring
4. Task 3 (remove webhook markers and setup)
5. Task 5 (update doc.go)
6. Task 4 (verify webhook infrastructure intact for VM)
7. Task 7 (integration tests)
8. Task 8 (manifests, lint, full test suite)

### Decision Point: DRPlan Webhook Removal Scope

**Two approaches for DRPlan admission:**

**Option A — Move both to in-process plugin (recommended):** Both DRExecution and DRPlan validation move to the admission plugin. The registry strategy already has `ValidateDRPlan`/`ValidateDRPlanUpdate` — the plugin adds defense-in-depth. Remove `DRPlanValidator` webhook entirely.

**Option B — Keep DRPlan webhook as defense-in-depth:** DRPlan webhook could stay since it works (DRPlans ARE stored via the aggregated API but the webhook works because kube-apiserver calls it before proxying). However, the epic explicitly says "webhook entries for soteria.io resources are removed" and the `DRPlanValidator` marker is to be removed.

**Epic requirement is clear: remove BOTH soteria.io webhook entries.** Go with Option A.

### Decision Point: Integration Test Approach for In-Process Admission

The `test/integration/admission/suite_test.go` tests use envtest with controller-runtime webhooks. Since the admission plugin runs inside the aggregated API server, integration testing requires the `test/integration/apiserver/` suite (which boots the full aggregated API server with ScyllaDB testcontainers). Consider:

- Adding DRExecution CREATE admission tests to `test/integration/apiserver/`
- The DRPlan integration tests in `test/integration/admission/` that test field validation may need updating since the DRPlan VWC entry will be removed from `make manifests`

### Previous Story Learnings (from 9.1)

- **Self-healing via reconcile loop** — partial/missing data fills in on subsequent cycles
- **`collectVMsFromWaves` propagates all VM data** — enriching VMs in waves automatically enriches SiteDiscovery
- **Use cached `client.Reader` for PVC resolution** — but for admission, use direct storage access to avoid stale-cache races
- **API types in `pkg/apis/soteria.io/v1alpha1/types.go`** — not kubebuilder `api/` convention

### Project Structure Notes

- API types live in `pkg/apis/soteria.io/v1alpha1/types.go` (sample-apiserver pattern)
- Admission webhooks in `pkg/admission/` — this story adds an in-process admission plugin here
- Aggregated API server setup in `pkg/apiserver/` — plugin registration goes here
- Registry strategies in `pkg/registry/` — already validate field constraints (strategy validation runs alongside plugin)
- Generated files: `config/webhook/manifests.yaml`, `config/rbac/role.yaml` — regenerated via `make manifests`
- Run `make manifests generate` after removing webhook markers

### References

- [Source: pkg/admission/drexecution_validator.go#L47-L121] — Current DRExecution webhook validation logic (port to plugin)
- [Source: pkg/admission/drplan_validator.go#L47-L85] — Current DRPlan webhook validation logic (port to plugin)
- [Source: pkg/admission/setup.go#L27-L79] — Webhook setup functions (remove DRPlan + DRExecution)
- [Source: pkg/apiserver/options.go#L129-L162] — API server Config() — register plugin here
- [Source: pkg/apiserver/apiserver.go] — API server New() — inject plugin initializer here
- [Source: pkg/registry/drexecution/strategy.go#L70-L92] — Strategy field validation (runs alongside plugin)
- [Source: pkg/registry/drplan/strategy.go] — Strategy field validation (calls ValidateDRPlan/ValidateDRPlanUpdate)
- [Source: cmd/soteria/main.go#L357-L372] — Webhook wiring (remove DRPlan + DRExecution calls)
- [Source: test/integration/admission/suite_test.go#L76-L144] — Integration test setup (no DRExecution webhook installed)
- [Source: pkg/apis/soteria.io/v1alpha1/validation.go] — ValidateDRPlan/ValidateDRPlanUpdate functions
- [Source: pkg/engine/statemachine.go] — Transition/ValidStartingPhases functions
- [Source: _bmad-output/planning-artifacts/epics.md#Story-9.2] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, platform constraints

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
