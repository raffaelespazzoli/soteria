# Story 2.1: DRPlan Controller & VM Auto-Discovery

Status: ready-for-dev

## Story

As a platform engineer,
I want the orchestrator to automatically discover VMs matching my DRPlan's label selector and organize them into waves,
So that adding VMs to DR protection requires only Kubernetes labels — no plan editing.

## Acceptance Criteria

1. **Given** a DRPlan with `vmSelector.matchLabels: {app.kubernetes.io/part-of: erp-system}` and `waveLabel: soteria.io/wave`, **When** the DRPlan controller in `pkg/controller/drplan/reconciler.go` reconciles, **Then** the controller discovers all VMs with the matching label using client-go via kube-apiserver (never direct ScyllaDB), **And** VMs are grouped into waves based on their `soteria.io/wave` label value (e.g., "1", "2", "3"), **And** DRPlan `.status.waves[]` is updated with discovered VM names, namespaces, and wave membership, **And** DRPlan `.status.conditions` includes a `Ready` condition reflecting discovery success.

2. **Given** a DRPlan with discovered VMs, **When** a platform engineer adds a new VM with matching labels (FR5), **Then** the VM watch triggers an immediate DRPlan reconcile, the controller re-discovers and updates `.status.waves[]` to include the new VM, **And** no manual DRPlan editing is required.

3. **Given** a DRPlan with discovered VMs, **When** a VM's wave label is changed (e.g., from "1" to "2"), **Then** the label change predicate triggers a DRPlan reconcile and the controller moves the VM to the new wave in `.status.waves[]`.

4. **Given** a DRPlan with `vmSelector` matching 50 VMs, **When** VM discovery and wave grouping executes, **Then** the operation completes within 10 seconds (NFR10).

5. **Given** the discovery engine in `pkg/engine/discovery.go`, **When** unit tests run, **Then** wave grouping is verified with table-driven tests covering: single wave, multiple waves, VMs without wave labels, empty selector results.

## Tasks / Subtasks

- [ ] Task 1: Extend DRPlanStatus with wave discovery fields (AC: #1)
  - [ ] 1.1 Add `DiscoveredVM` struct to `pkg/apis/soteria.io/v1alpha1/types.go` — fields: `Name string`, `Namespace string`
  - [ ] 1.2 Add `WaveInfo` struct to `types.go` — fields: `WaveKey string` (wave label value), `VMs []DiscoveredVM`
  - [ ] 1.3 Add `Waves []WaveInfo` field to `DRPlanStatus`
  - [ ] 1.4 Add `DiscoveredVMCount int` field to `DRPlanStatus` — total VMs matching selector
  - [ ] 1.5 Add OpenAPI validation markers: `WaveKey` required, `VMs` minItems=1
  - [ ] 1.6 Run `hack/update-codegen.sh` to regenerate deepcopy + OpenAPI; verify `hack/verify-codegen.sh` passes

- [ ] Task 2: Add kubevirt dependency and implement VM discovery engine (AC: #1, #4, #5)
  - [ ] 2.1 Add `kubevirt.io/api` as a Go module dependency: `go get kubevirt.io/api`
  - [ ] 2.2 Register kubevirt types in the manager's scheme in `cmd/soteria/main.go`: `kubevirtv1.AddToScheme(scheme)` (import `kubevirt.io/api/core/v1`)
  - [ ] 2.3 Create `pkg/engine/discovery.go` with Tier 1 package doc update + Tier 2 file-level architecture comment
  - [ ] 2.4 Define `VMReference` type — `Name`, `Namespace`, `Labels map[string]string` (lightweight projection of kubevirt VM metadata for pipeline stages that don't need the full object)
  - [ ] 2.5 Define `WaveGroup` type — `WaveKey string`, `VMs []VMReference`
  - [ ] 2.6 Define `DiscoveryResult` type — `Waves []WaveGroup`, `TotalVMs int`
  - [ ] 2.7 Implement `GroupByWave(vms []VMReference, waveLabel string) DiscoveryResult` — pure function, no K8s dependency; VMs without the wave label go to a `""` (empty string) wave key; waves sorted by key for deterministic output
  - [ ] 2.8 Define `VMDiscoverer` interface — `DiscoverVMs(ctx context.Context, selector metav1.LabelSelector) ([]VMReference, error)` — allows mock injection for tests
  - [ ] 2.9 Implement `TypedVMDiscoverer` struct — uses controller-runtime `client.Reader` to list `kubevirtv1.VirtualMachineList` with parsed label selector; extracts Name/Namespace/Labels into `VMReference`

- [ ] Task 3: Unit tests for discovery engine (AC: #5)
  - [ ] 3.1 Create `pkg/engine/discovery_test.go`
  - [ ] 3.2 Table-driven `TestGroupByWave` covering:
    - Single wave: 5 VMs all with `soteria.io/wave: "1"` → 1 wave group
    - Multiple waves: 10 VMs across waves "1", "2", "3" → 3 wave groups, correct membership
    - VMs without wave labels: 3 VMs with no wave label → 1 group with empty wave key
    - Empty input: 0 VMs → empty result
    - Mixed: some VMs with wave label, some without → correct split
    - Deterministic ordering: verify wave groups are sorted by key
  - [ ] 3.3 Unit test for `TypedVMDiscoverer` using controller-runtime `fake.NewClientBuilder().WithObjects(...)` with kubevirt scheme — verify label selector is passed correctly, verify VMReference extraction from typed VirtualMachine objects

- [ ] Task 4: Implement DRPlan reconciler (AC: #1, #2, #3)
  - [ ] 4.1 Create `pkg/controller/drplan/reconciler.go` with Tier 2 architecture comment explaining the reconcile flow: fetch DRPlan → discover VMs → group waves → update status
  - [ ] 4.2 Define `DRPlanReconciler` struct — fields: `client.Client`, `Scheme *runtime.Scheme`, `VMDiscoverer engine.VMDiscoverer`
  - [ ] 4.3 Add RBAC markers:
    - `+kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch`
    - `+kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch`
  - [ ] 4.4 Implement `Reconcile` method:
    - Fetch DRPlan by NamespacedName; if not found, return (deleted)
    - Call `VMDiscoverer.DiscoverVMs()` with plan's `vmSelector`
    - Call `engine.GroupByWave()` with discovered VMs and plan's `waveLabel`
    - Map `DiscoveryResult` to `DRPlanStatus.Waves` and `DiscoveredVMCount`
    - Set `Ready` condition: `True` if VMs found, `False` with reason `NoVMsDiscovered` if empty
    - Set `ObservedGeneration` to `plan.Generation`
    - Update DRPlan status via status subresource client
    - Re-fetch DRPlan before status update to avoid resourceVersion conflicts
    - Structured logging: `log.FromContext(ctx).WithValues("drplan", req.NamespacedName)`
  - [ ] 4.5 Implement `SetupWithManager` — event-driven reconciliation:
    - `.For(&soteriav1alpha1.DRPlan{})` — primary watch on DRPlan resources
    - `.Watches(&kubevirtv1.VirtualMachine{}, handler.EnqueueRequestsFromMapFunc(r.mapVMToDRPlans), builder.WithPredicates(vmRelevantChangePredicate()))` — secondary watch on VMs; enqueues affected DRPlans when VMs are created, deleted, or have label changes
    - Return `RequeueAfter: 10 * time.Minute` from `Reconcile` as a safety-net fallback (catches any missed events, e.g., after controller restart)
  - [ ] 4.6 Implement `mapVMToDRPlans(ctx context.Context, obj client.Object) []reconcile.Request`:
    - Cast `obj` to `*kubevirtv1.VirtualMachine`, extract labels
    - List all DRPlans via `r.Client.List(ctx, &soteriav1alpha1.DRPlanList{})`
    - For each DRPlan: parse `spec.vmSelector` with `metav1.LabelSelectorAsSelector()`, check if VM labels match
    - Return `reconcile.Request` for each matching DRPlan (namespace + name)
    - Structured logging: `log.V(2).Info("VM change mapped to DRPlans", "vm", obj.GetName(), "matchedPlans", len(requests))`
  - [ ] 4.7 Implement `vmRelevantChangePredicate() predicate.Predicate`:
    - CREATE: always enqueue (new VM may match a plan)
    - DELETE: always enqueue (VM removed from a plan)
    - UPDATE: enqueue only if `oldObj.GetLabels()` differs from `newObj.GetLabels()` (label changes affect plan membership and wave assignment)
    - GENERIC: enqueue (safety net)
  - [ ] 4.8 Emit events: `DiscoveryCompleted` (Info) when waves change, `DiscoveryFailed` (Warning) on errors

- [ ] Task 5: Wire controller into `cmd/soteria/main.go` (AC: #1)
  - [ ] 5.1 Import `pkg/controller/drplan`, `pkg/engine`, and `kubevirt.io/api/core/v1`
  - [ ] 5.2 Register kubevirt types in the scheme: `kubevirtv1.AddToScheme(scheme)`
  - [ ] 5.3 After manager creation, instantiate `TypedVMDiscoverer` using `mgr.GetClient()`
  - [ ] 5.4 Create `DRPlanReconciler` and call `SetupWithManager(mgr)`
  - [ ] 5.5 Verify `go build ./cmd/soteria/` succeeds
  - [ ] 5.6 Run `make manifests` to regenerate RBAC from markers

- [ ] Task 6: Unit tests for reconciler and VM-to-DRPlan mapping (AC: #1, #2, #3)
  - [ ] 6.1 Create `pkg/controller/drplan/reconciler_test.go`
  - [ ] 6.2 Create a mock `VMDiscoverer` that returns configurable VM lists
  - [ ] 6.3 Test: reconcile with VMs discovered → status.waves populated, Ready=True
  - [ ] 6.4 Test: reconcile with no VMs found → status.waves empty, Ready=False, reason=NoVMsDiscovered
  - [ ] 6.5 Test: reconcile after VM added → status.waves updated with new VM
  - [ ] 6.6 Test: reconcile after wave label change → VM moved to new wave
  - [ ] 6.7 Test: reconcile with DRPlan not found → no error, no requeue
  - [ ] 6.8 Test: reconcile with discovery error → Ready=False with error message, requeue with backoff
  - [ ] 6.9 Test `mapVMToDRPlans`: VM labels match one DRPlan → returns one reconcile.Request
  - [ ] 6.10 Test `mapVMToDRPlans`: VM labels match two DRPlans → returns two reconcile.Requests
  - [ ] 6.11 Test `mapVMToDRPlans`: VM labels match no DRPlans → returns empty slice
  - [ ] 6.12 Test `vmRelevantChangePredicate`: CREATE event → returns true
  - [ ] 6.13 Test `vmRelevantChangePredicate`: DELETE event → returns true
  - [ ] 6.14 Test `vmRelevantChangePredicate`: UPDATE with label change → returns true
  - [ ] 6.15 Test `vmRelevantChangePredicate`: UPDATE with no label change (e.g., status update) → returns false

- [ ] Task 7: Integration tests (AC: #1, #2, #3, #4)
  - [ ] 7.1 Create `test/integration/controller/suite_test.go` — setup envtest Environment for VM CRDs + aggregated API server with testcontainers ScyllaDB for DRPlan resources
  - [ ] 7.2 Create `test/integration/controller/drplan_test.go` with `//go:build integration` tag
  - [ ] 7.3 `TestDRPlanReconciler_DiscoverVMs_WavesPopulated` — create DRPlan + VMs with matching labels, verify `.status.waves` is populated correctly
  - [ ] 7.4 `TestDRPlanReconciler_NewVMAdded_WatchTriggersReconcile` — add a VM after initial reconcile, verify the VM watch triggers re-reconciliation and status updates with the new VM
  - [ ] 7.5 `TestDRPlanReconciler_WaveLabelChanged_WatchTriggersReconcile` — change VM wave label, verify the label-change predicate triggers reconcile and VM moves between waves in status
  - [ ] 7.6 `TestDRPlanReconciler_VMDeleted_WatchTriggersReconcile` — delete a VM that was part of a plan, verify the watch triggers reconcile and the VM is removed from status
  - [ ] 7.6 `TestDRPlanReconciler_ReadyCondition_ReflectsDiscovery` — verify Ready condition True/False based on discovery result
  - [ ] 7.7 `TestDRPlanReconciler_50VMs_CompletesWithin10s` — performance: 50 VMs discovered within NFR10 bound

- [ ] Task 8: Verify and finalize
  - [ ] 8.1 Run `make lint-fix` to auto-fix code style
  - [ ] 8.2 Run `make test` — all unit tests pass
  - [ ] 8.3 Run `make integration` — all integration tests pass (including new controller tests)
  - [ ] 8.4 Run `make manifests` — verify RBAC regenerated with new VM watch permission
  - [ ] 8.5 Verify Tier 1/2/3 documentation standards met (retro action item #2)

## Dev Notes

### Architecture Context

This is the first controller in the project. Epic 1 built the API types, ScyllaDB storage layer, aggregated API server, and CI pipeline. This story introduces the first reconciliation loop.

**Key boundary:** The controller talks to the aggregated API server through the standard kube-apiserver proxy (DRPlan CRUD) and to the native kube-apiserver for VM resources (kubevirt.io). It never touches ScyllaDB directly.

### DRPlan Resource Flow

```
DRPlan create (kubectl/Console)
  → kube-apiserver proxy → Aggregated API Server → ScyllaDB
  → CDC → cacher → controller-runtime informer
  → DRPlan reconciler: discover VMs → group waves → update status
  → status update → Aggregated API Server → ScyllaDB
```

### Type Changes

`DRPlanStatus` currently has: `Phase`, `Conditions`, `ObservedGeneration`. This story adds `Waves []WaveInfo` and `DiscoveredVMCount int`. After editing `types.go`, run:

```bash
hack/update-codegen.sh   # regenerate deepcopy + openapi
hack/verify-codegen.sh   # CI verification
```

### Typed kubevirt Dependency

VMs are OpenShift Virtualization `VirtualMachine` resources (`kubevirt.io/v1`). The project takes a **direct compile-time dependency** on `kubevirt.io/api` — this is a DR orchestrator for VMs, so the dependency is natural and gives type-safe access to VM specs (volumes, PVC references, etc.).

Add to `go.mod` via `go get kubevirt.io/api`. Register in scheme: `kubevirtv1.AddToScheme(scheme)` in `cmd/soteria/main.go`.

The `VMDiscoverer` interface still allows test mocks, but the production implementation (`TypedVMDiscoverer`) uses the controller-runtime cached `client.Reader` to list `kubevirtv1.VirtualMachineList` with label selectors.

### Controller-Runtime + Aggregated API

The controller-runtime manager's client works with aggregated API resources because:
1. The soteria types are registered in the scheme via `soteriainstall.Install(scheme)`
2. The kube-apiserver proxies `soteria.io/v1alpha1` requests to the aggregated API server
3. Informers and watches work through the standard discovery + watch mechanisms

### Event-Driven VM Watch

The DRPlan controller uses a **secondary watch on VirtualMachine resources** to trigger reconciliation when VMs change. This replaces polling-based re-discovery with event-driven reconciliation:

```go
ctrl.NewControllerManagedBy(mgr).
    For(&soteriav1alpha1.DRPlan{}).
    Watches(
        &kubevirtv1.VirtualMachine{},
        handler.EnqueueRequestsFromMapFunc(r.mapVMToDRPlans),
        builder.WithPredicates(vmRelevantChangePredicate()),
    ).
    Complete(r)
```

**mapVMToDRPlans:** When a VM event fires, this function lists all DRPlans, parses each plan's `vmSelector`, and returns reconcile requests for plans whose selector matches the VM's labels. This is O(N) where N = number of DRPlans (capped at 100 by NFR9). The DRPlan list is served from the controller-runtime informer cache.

**vmRelevantChangePredicate:** Filters VM events to only those that affect plan composition:
- CREATE: new VM may match a plan → enqueue
- DELETE: VM removed from a plan → enqueue
- UPDATE: only if labels changed (label changes affect plan membership and wave assignment); ignores status-only updates to avoid unnecessary reconciliation
- GENERIC: enqueue (safety net for informer resyncs)

**Fallback requeue:** `RequeueAfter: 10 * time.Minute` returned from every successful reconcile as a safety net. Catches edge cases: events missed during controller restart, VMs created while the controller was down, namespace annotation changes (not watched). The manager's built-in 10-hour cache resync provides an additional backstop.

**Why event-driven over polling:** Immediate response to VM changes (sub-second vs. 30s polling), no wasted reconciliation cycles when nothing changes, and follows controller-runtime best practices (event-driven first, `RequeueAfter` only as fallback).

### Wave Sorting

Waves in `.status.waves[]` must be sorted by `waveKey` for deterministic output. The wave key is the string value of the wave label (e.g., "1", "2", "3"). Use lexicographic sorting — this works for numeric strings if they're zero-padded, and is deterministic regardless.

### Integration Test Setup

Integration tests need both:
- **envtest** — for VM CRDs (install kubevirt VirtualMachine CRD from `kubevirt.io/api`; typed client handles the rest)
- **testcontainers ScyllaDB** + aggregated API server — for DRPlan resources

Reference `test/integration/apiserver/suite_test.go` for the ScyllaDB + API server setup pattern. The controller test suite combines this with envtest for VM resources. The VM watch integration tests verify that creating/deleting/relabeling VMs triggers DRPlan reconciliation automatically.

### Retro Action Items (Epic 1)

- **Action #2:** All new code must meet Tier 1-3 documentation standards
  - Tier 1: Update `pkg/engine/doc.go` with 3-5 sentence godoc
  - Tier 2: Architecture block comments on `discovery.go` and `reconciler.go`
  - Tier 3: Domain 'why' comments on wave grouping logic (why empty wave key, why sorted)

### References

- Architecture: `_bmad-output/planning-artifacts/architecture.md` — "Data Flow" section, "Requirements to Structure Mapping" table
- PRD: FR1 (plan creation), FR3 (auto-discovery), FR5 (label-based addition)
- NFR10: wave discovery < 10s for up to 50 VMs/plan
- Existing patterns: `pkg/registry/drplan/strategy.go` (status strategy), `pkg/apiserver/apiserver.go` (API registration)
- controller-runtime FAQ: https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md

## Dev Agent Record

| Field | Value |
|-------|-------|
| Story file created | 2026-04-09 |
| Implementation started | — |
| Implementation completed | — |
| Code review requested | — |
| Code review completed | — |
| Status | ready-for-dev |
