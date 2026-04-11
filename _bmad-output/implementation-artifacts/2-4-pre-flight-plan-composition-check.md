# Story 2.4: Pre-flight Plan Composition Check

Status: done

## Story

As a platform engineer,
I want to view the full composition of my DRPlan before execution,
So that I can verify the plan matches my expectations and throttling constraints are valid.

## Acceptance Criteria

1. **Given** a valid DRPlan with discovered VMs and waves, **When** the DRPlan controller reconciles, **Then** `.status.preflight` is populated with a structured report showing: total VM count per wave, wave ordering and VM membership, volume groups per VM (VM-level or namespace-level consistency), storage backend per VM (derived from PVC storage class), and DRGroup chunking preview based on `maxConcurrentFailovers` (FR8).

2. **Given** a pre-flight report in `.status.preflight`, **When** `maxConcurrentFailovers` is sufficient for all namespace+wave groups, **Then** the DRGroup chunking preview shows how VMs would be partitioned into chunks within each wave, **And** namespace-consistent VMs are shown as indivisible units within their chunk.

3. **Given** a pre-flight report, **When** any validation issue exists (e.g., VMs with unknown storage backend, namespace group exceeding throttle, VMs without PVCs), **Then** the report includes warnings with specific details and affected resource names.

4. **Given** the pre-flight report, **When** accessed via `kubectl get drplan <name> -o jsonpath='{.status.preflight}'`, **Then** the composition data is available without triggering execution.

5. **Given** the pre-flight module in `internal/preflight/checks.go`, **When** unit tests run, **Then** composition report assembly is verified with table-driven tests: single wave, multiple waves, mixed consistency levels, storage backend detection, chunking preview with namespace groups, and warning generation.

6. **Given** the storage backend resolver, **When** a VM has PVCs with a recognized storage class, **Then** the report shows the storage driver name (e.g., `odf`, `dell-powerstore`), **And** when the storage class is unknown, the report shows `unknown` with a warning.

## Tasks / Subtasks

- [x] Task 1: Extend API types with PreflightReport (AC: #1, #4)
  - [x] 1.1 Add `PreflightReport` struct to `pkg/apis/soteria.io/v1alpha1/types.go`:
    - `Waves []PreflightWave` — per-wave composition summary
    - `TotalVMs int` — total VMs in plan
    - `Warnings []string` — validation warnings (non-blocking issues)
    - `GeneratedAt *metav1.Time` — when the report was last computed
  - [x] 1.2 Add `PreflightWave` struct:
    - `WaveKey string` — wave label value
    - `VMCount int` — total VMs in this wave
    - `VMs []PreflightVM` — per-VM details
    - `Chunks []PreflightChunk` — DRGroup chunking preview for this wave
  - [x] 1.3 Add `PreflightVM` struct:
    - `Name string`
    - `Namespace string`
    - `StorageBackend string` — driver name from PVC storage class (or `"unknown"`)
    - `ConsistencyLevel string` — `"namespace"` or `"vm"`
    - `VolumeGroupName string` — which volume group this VM belongs to
  - [x] 1.4 Add `PreflightChunk` struct:
    - `Name string` — DRGroup chunk name (e.g., `wave-1-group-0`)
    - `VMCount int` — VMs in this chunk
    - `VMNames []string` — VM names in this chunk
    - `VolumeGroups []string` — volume group names in this chunk
  - [x] 1.5 Add `Preflight *PreflightReport` field to `DRPlanStatus`
  - [x] 1.6 Run `hack/update-codegen.sh` to regenerate deepcopy + OpenAPI; verify `hack/verify-codegen.sh` passes

- [x] Task 2: Implement storage backend resolver (AC: #6)
  - [x] 2.1 Create `internal/preflight/storage.go` with Tier 2 architecture block comment explaining: this module resolves storage backends for VMs by inspecting their PVC references and mapping PVC storage classes to known driver names; it's used during preflight composition to show which storage driver handles each VM's volumes
  - [x] 2.2 Define `StorageBackendResolver` interface — `ResolveBackends(ctx context.Context, vms []engine.VMReference) (map[string]string, error)` — returns a map of `"namespace/vmName"` → storage backend name
  - [x] 2.3 Define `StorageClassDriverMap` type — `map[string]string` — maps storage class name → driver name (e.g., `"ocs-storagecluster-ceph-rbd"` → `"odf"`)
  - [x] 2.4 Implement `TypedStorageBackendResolver` struct — fields:
    - `Client client.Reader` — for reading typed `kubevirtv1.VirtualMachine` objects (kubevirt.io/api dependency from Story 2.1)
    - `CoreClient corev1client.CoreV1Interface` — for reading PVCs and StorageClasses
    - `DriverMap StorageClassDriverMap` — static mapping from storage class to driver name (loaded from driver registry)
  - [x] 2.5 Implement `ResolveBackends`:
    - For each VM: fetch the typed `kubevirtv1.VirtualMachine` via `Client.Get()`, read `vm.Spec.Template.Spec.Volumes` directly from the typed struct to extract `PersistentVolumeClaim.ClaimName` and `DataVolume.Name`
    - For each PVC claim name: get PVC via `CoreClient.PersistentVolumeClaims(namespace).Get()`, read `.spec.storageClassName`
    - For DataVolume-backed VMs: the PVC name typically matches the DataVolume name — look up PVC with same name
    - Map storage class name to driver name via `DriverMap`; if not found, use `"unknown"`
    - Return the first resolved backend per VM (all PVCs of a single VM are expected to use the same storage class within a consistency group)
  - [x] 2.6 Tier 3 domain 'why' comment: storage backend is resolved from PVC storage classes because driver selection is implicit in the architecture (FR21) — there is no StorageProviderConfig CRD; the orchestrator discovers which driver handles each VM's volumes by inspecting existing cluster state
  - [x] 2.7 Handle edge cases:
    - VM with no volumes → `"none"` with warning
    - VM with mixed storage classes across PVCs → use first, add warning: `"VM <ns>/<name> has PVCs across multiple storage classes; using <class>"`
    - PVC not found → `"unknown"` with warning
    - Storage class not in driver map → `"unknown"` with warning

- [x] Task 3: Implement preflight composition engine (AC: #1, #2, #3)
  - [x] 3.1 Implement the main composition function in `internal/preflight/checks.go` with Tier 2 architecture block comment explaining: this module assembles a preflight composition report from discovery, consistency, chunking, and storage backend data; it's called by the DRPlan reconciler on every reconcile to populate `.status.preflight`; the report gives platform engineers full visibility into plan structure before execution
  - [x] 3.2 Define `CompositionInput` type — aggregates inputs from earlier pipeline stages:
    - `Plan *soteriav1alpha1.DRPlan`
    - `DiscoveryResult *engine.DiscoveryResult` (from Story 2.1)
    - `ConsistencyResult *engine.ConsistencyResult` (from Story 2.2)
    - `ChunkResult *engine.ChunkResult` (from Story 2.2)
    - `StorageBackends map[string]string` (from Task 2, key = `namespace/vmName`)
  - [x] 3.3 Implement `ComposeReport(input CompositionInput) *soteriav1alpha1.PreflightReport`:
    - For each wave in `DiscoveryResult.Waves`:
      - Build `PreflightWave` with VM count and per-VM details
      - For each VM: populate `StorageBackend` from `StorageBackends` map, `ConsistencyLevel` from `ConsistencyResult`, `VolumeGroupName` from the VM's resolved volume group
      - Build `PreflightChunk` entries from `ChunkResult.Waves[].Chunks[]`
    - Set `TotalVMs` from `DiscoveryResult.TotalVMs`
    - Set `GeneratedAt` to current time
    - Collect all warnings (storage issues, validation issues)
  - [x] 3.4 Implement `collectWarnings(input CompositionInput, storageBackends map[string]string) []string`:
    - For each VM with `StorageBackend == "unknown"`: `"VM <ns>/<name>: could not determine storage backend from PVC storage class"`
    - For each VM with `StorageBackend == "none"`: `"VM <ns>/<name>: no PVC volumes found"`
    - For each `ChunkError` in `ChunkResult.Errors`: `"Wave <key>: namespace group <ns> (<N> VMs) exceeds maxConcurrentFailovers (<M>)"`
    - For each `WaveConflict` in `ConsistencyResult.WaveConflicts`: `"Wave conflict in namespace <ns>: VMs have conflicting wave labels"`
  - [x] 3.5 Structured logging: `log.Info("Composed preflight report", "totalVMs", report.TotalVMs, "waves", len(report.Waves), "warnings", len(report.Warnings))`

- [x] Task 4: Extend DRPlan reconciler with preflight composition (AC: #1, #4)
  - [x] 4.1 Extend `DRPlanReconciler` struct (from Stories 2.1/2.2) with `StorageResolver preflight.StorageBackendResolver` field
  - [x] 4.2 Update `Reconcile` method — after existing discovery (2.1) + consistency/chunking (2.2) pipeline:
    - Call `StorageResolver.ResolveBackends(ctx, discoveredVMs)` to get storage backends
    - Build `preflight.CompositionInput` from the existing pipeline outputs
    - Call `preflight.ComposeReport(input)` to generate the report
    - Set `plan.Status.Preflight = report`
  - [x] 4.3 The status update already happens in the reconcile loop from Story 2.1 — preflight is populated before the existing status write; no additional status update call needed
  - [x] 4.4 Handle `StorageResolver` errors gracefully: if resolution fails, still populate the report with available data and add a warning: `"Storage backend resolution failed: <error>"`; do NOT fail the reconcile — preflight is informational
  - [x] 4.5 Add RBAC markers for PVC and StorageClass access:
    - `+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch`
  - [x] 4.6 Structured logging: `log.Info("Preflight report generated", "totalVMs", report.TotalVMs, "warnings", len(report.Warnings))`

- [x] Task 5: Wire storage resolver into cmd/soteria/main.go (AC: #1)
  - [x] 5.1 Import `"github.com/soteria-project/soteria/internal/preflight"`
  - [x] 5.2 After manager creation, build `StorageClassDriverMap` from the driver registry (or initialize with known mappings — ODF, no-op) — the driver registry from Epic 3 doesn't exist yet, so use a static map for now with a TODO comment
  - [x] 5.3 Create `TypedStorageBackendResolver` using manager's client, core client from rest config, and the driver map
  - [x] 5.4 Pass `StorageResolver` to `DRPlanReconciler` during construction
  - [x] 5.5 Verify `go build ./cmd/soteria/` succeeds
  - [x] 5.6 Run `make manifests` to regenerate RBAC with PVC and StorageClass read permissions

- [x] Task 6: Unit tests for storage backend resolver (AC: #6)
  - [x] 6.1 Create `internal/preflight/storage_test.go`
  - [x] 6.2 Table-driven `TestResolveBackends` covering:
    - VM with PVC using known storage class → correct driver name returned
    - VM with PVC using unknown storage class → `"unknown"` returned
    - VM with no volumes → `"none"` returned
    - VM with PVC that doesn't exist → `"unknown"` returned
    - VM with DataVolume-backed volumes → PVC resolved by DataVolume name
    - VM with mixed storage classes → first used, warning noted
    - Multiple VMs with different storage classes → each resolved independently
  - [x] 6.3 Use controller-runtime `fake.NewClientBuilder().WithObjects(...)` with kubevirt scheme for VM fetching, and `k8s.io/client-go/kubernetes/fake` for PVC/StorageClass lookups

- [x] Task 7: Unit tests for preflight composition engine (AC: #1, #2, #3, #5)
  - [x] 7.1 Create `internal/preflight/checks_test.go`
  - [x] 7.2 Table-driven `TestComposeReport` covering:
    - Single wave, all VM-level consistency, known storage → correct report structure
    - Multiple waves → waves sorted by key, correct VM counts
    - Namespace-level VMs → correct consistency level and volume group names in report
    - Mixed consistency → VM-level and namespace-level correctly reflected
    - Chunking preview → chunks match `ChunkResult` output, correct VM membership
    - Namespace group as indivisible unit → chunk contains all namespace VMs
    - Unknown storage backend → warning generated
    - No-volume VMs → warning generated
    - Chunk errors (namespace group exceeds throttle) → warning in report
    - Wave conflicts → warning in report
    - Empty plan (no VMs) → empty waves, TotalVMs=0, no warnings
  - [x] 7.3 Table-driven `TestCollectWarnings` — isolated tests for each warning condition

- [x] Task 8: Unit tests for reconciler preflight integration (AC: #1, #4)
  - [x] 8.1 Extend `pkg/controller/drplan/reconciler_test.go` (from Stories 2.1/2.2) with mock `StorageBackendResolver`
  - [x] 8.2 Create `MockStorageBackendResolver` — returns configurable backend maps
  - [x] 8.3 Test: reconcile with valid plan + storage resolution → `.status.preflight` populated with correct wave structure, VM details, chunking preview, and `GeneratedAt` set
  - [x] 8.4 Test: reconcile with storage resolution failure → `.status.preflight` still populated with available data, warning added, reconcile succeeds (no error)
  - [x] 8.5 Test: reconcile with unknown storage backends → preflight populated with `"unknown"` backends and warnings
  - [x] 8.6 Test: reconcile updates preflight on every cycle → preflight reflects latest discovery state

- [x] Task 9: Integration tests (AC: #1, #2, #3, #4, #6)
  - [x] 9.1 Extend `test/integration/controller/suite_test.go` (from Stories 2.1/2.2) — ensure PVC and StorageClass resources are available in envtest
  - [x] 9.2 Create `test/integration/controller/drplan_preflight_test.go` with `//go:build integration` tag
  - [x] 9.3 `TestDRPlanReconciler_Preflight_BasicComposition` — create DRPlan + VMs with matching labels + PVCs with known storage class, reconcile, verify `.status.preflight.waves` has correct VM details, storage backends, and chunking preview
  - [x] 9.4 `TestDRPlanReconciler_Preflight_NamespaceConsistency` — create namespace-level namespace + VMs + PVCs, reconcile, verify preflight shows namespace-level volume groups as indivisible chunks
  - [x] 9.5 `TestDRPlanReconciler_Preflight_StorageBackendUnknown` — create VM with PVC using unlisted storage class, reconcile, verify preflight shows `"unknown"` backend with warning
  - [x] 9.6 `TestDRPlanReconciler_Preflight_KubectlAccess` — create and reconcile a DRPlan, then verify the preflight report is accessible via `kubectl get drplan <name> -o json` (parse JSON, check `.status.preflight` structure)
  - [x] 9.7 `TestDRPlanReconciler_Preflight_MultiWaveChunking` — 3 waves with different VM counts, verify each wave's chunking preview is correct and independent
  - [x] 9.8 `TestDRPlanReconciler_Preflight_WarningsPopulated` — create conditions that generate warnings (missing PVC, unknown storage class), verify warnings array is populated

- [x] Task 10: Verify and finalize
  - [x] 10.1 Run `make lint-fix` to auto-fix code style
  - [x] 10.2 Run `make test` — all unit tests pass
  - [x] 10.3 Run `make integration` — all integration tests pass
  - [x] 10.4 Run `make manifests` — verify RBAC regenerated with PVC and StorageClass read permissions
  - [x] 10.5 Verify Tier 1/2/3 documentation standards met (retro action item #2)

## Dev Notes

### Architecture Context

This story adds the pre-flight plan composition check — FR8's core capability. Stories 2.1 and 2.2 build the discovery, consistency, and chunking pipeline. This story assembles those outputs into a structured report that shows the platform engineer exactly what their plan contains and how it would execute, without triggering execution.

The preflight report is **informational, not gating**. It populates `.status.preflight` on every reconcile. If the plan has issues (wave conflicts, namespace group exceeding throttle), those are still caught by the `Ready=False` condition from Stories 2.1/2.2. The preflight adds **visibility** — detailed composition breakdown, DRGroup chunking preview, and storage backend identification.

**Relationship to Story 7.1 (Console pre-flight confirmation):** Story 7.1 adds the Console pre-flight dialog with RPO estimates, RTO estimates, and capacity checks. That builds ON TOP of this story's composition data. The Console reads `.status.preflight` to show wave/VM/chunk structure, then adds runtime data (replication sync time, last execution duration, DR site compute capacity) that requires live queries.

### Preflight Report Location in API

The report lives at `.status.preflight` on the DRPlan resource. This means:

- **No separate subresource** — the composition data is part of the standard status, populated during reconcile
- **Accessible via kubectl:** `kubectl get drplan erp-full-stack -o jsonpath='{.status.preflight}'`
- **Accessible via Console:** `useK8sWatchResource()` automatically includes status
- **Updated on every reconcile** — always reflects the latest discovery state

This is simpler than a dedicated subresource and aligns with the Kubernetes pattern of status reflecting observed state. The `GeneratedAt` timestamp lets consumers know when the report was last computed.

### Pipeline Integration

The reconcile loop, after this story, follows this pipeline:

```
DRPlan reconcile:
  1. Discover VMs          → DiscoveryResult  (Story 2.1)
  2. Resolve consistency   → ConsistencyResult (Story 2.2)
  3. Chunk waves           → ChunkResult       (Story 2.2)
  4. Resolve storage       → map[vm]backend    (Story 2.4 — NEW)
  5. Compose preflight     → PreflightReport   (Story 2.4 — NEW)
  6. Set status.waves                           (Story 2.1)
  7. Set status.preflight                       (Story 2.4 — NEW)
  8. Set Ready condition                        (Stories 2.1/2.2)
  9. Update status subresource
```

Steps 4-5 and 7 are the additions from this story. The preflight composition is a pure assembly step — it doesn't make new decisions, it formats existing pipeline outputs into a user-facing report.

### Storage Backend Resolution

The architecture says driver selection is implicit from PVC storage classes (FR21). This story implements the first half of that — resolving which driver handles each VM's volumes. The resolution chain:

```
VirtualMachine (typed kubevirtv1.VirtualMachine)
  → vm.Spec.Template.Spec.Volumes[].PersistentVolumeClaim.ClaimName
  → PVC.spec.storageClassName
  → StorageClassDriverMap[storageClassName]
  → driver name (e.g., "odf", "noop")
```

**Static driver map:** Since Epic 3 (StorageProvider interface + driver registry) doesn't exist yet, the storage class → driver mapping is a static map initialized in `cmd/soteria/main.go`. Add a TODO comment for Epic 3 to populate this from the driver registry dynamically. **For Story 2.4, only the `noop` storage driver is recognized in the production binary** (`"noop-storage"` → `"noop"`). Other storage classes (including ODF) will resolve as `"unknown"` with a warning until Epic 3 implements the driver registry and populates the map dynamically. The integration tests use a broader map (including `"test-odf"`) to validate the resolution logic itself, but the production configuration is intentionally minimal.

**DataVolumes:** OpenShift Virtualization VMs can use DataVolumes (CDI) instead of direct PVC claims. The PVC created by CDI has the same name as the DataVolume. The resolver handles both patterns: `persistentVolumeClaim.claimName` and `dataVolume.name`.

**Typed VM access:** VM specs are accessed as typed `kubevirtv1.VirtualMachine` using the controller-runtime cached client (`kubevirt.io/api` is a project dependency since Story 2.1). PVC and StorageClass access uses typed `corev1client`.

### Error Resilience

The preflight report is informational. The reconciler must NOT fail if preflight composition encounters errors:

- Storage resolution failure → populate report with available data, add warning, continue
- Missing PVCs → show `"unknown"` backend, add warning
- Unknown storage class → show `"unknown"` backend, add warning
- ChunkResult errors → include in warnings (already caught by `Ready=False` in Story 2.2)

The reconciler sets `Ready=False` based on critical issues (wave conflicts, throttle violations) in Stories 2.1/2.2. The preflight warnings are supplementary — they surface non-critical issues that don't prevent plan execution but inform the engineer.

### RBAC Additions

This story adds read access for PVCs and StorageClasses:

| Resource | Verbs | Reason |
|----------|-------|--------|
| `""/persistentvolumeclaims` | `get`, `list`, `watch` | Read VM PVCs to determine storage backend |
| `storage.k8s.io/storageclasses` | `get`, `list`, `watch` | Read PVC storage class names |

These are in addition to the RBAC from Stories 2.1 (VMs, DRPlans) and 2.2 (namespaces).

### VM Volume Extraction

Extracting PVC references from typed `kubevirtv1.VirtualMachine` objects. The relevant paths:

```yaml
spec:
  template:
    spec:
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: vm-rootdisk-pvc
        - name: datadisk
          dataVolume:
            name: vm-datadisk-dv
```

Access pattern in Go with typed structs:

```go
for _, vol := range vm.Spec.Template.Spec.Volumes {
    if vol.PersistentVolumeClaim != nil {
        claimName := vol.PersistentVolumeClaim.ClaimName
    }
    if vol.DataVolume != nil {
        dvName := vol.DataVolume.Name
    }
}
```

### Dependency on Stories 2.1 and 2.2

This story assumes Stories 2.1 and 2.2 have been completed and the following exist:

- **From Story 2.1:** `engine.VMDiscoverer`, `engine.VMReference`, `engine.DiscoveryResult`, `engine.GroupByWave`, `DRPlanReconciler` with VM watch and event-driven reconciliation, `kubevirt.io/api` dependency with typed `kubevirtv1.VirtualMachine`, `DRPlanStatus.Waves`, `DRPlanStatus.DiscoveredVMCount`
- **From Story 2.2:** `engine.NamespaceLookup`, `engine.ConsistencyResult`, `engine.VolumeGroupInfo`, `engine.WaveConflict`, `engine.ResolveVolumeGroups`, `engine.ChunkWaves`, `engine.ChunkResult`, `engine.DRGroupChunk`, `engine.ChunkError`, reconciler consistency + chunking integration

**If Stories 2.1/2.2 aren't complete when development starts:** the preflight composition cannot be fully implemented because it depends on the discovery, consistency, and chunking pipeline outputs. Implement the preflight types (Task 1) and the storage backend resolver (Task 2) first — these are independent. Then integrate once the pipeline exists.

### Chunking Preview Representation

The `PreflightChunk` type mirrors the chunking result from `engine.ChunkWaves()` but in a user-facing format. It shows:

- Chunk name (e.g., `wave-1-group-0`) — matches the DRGroup naming from Story 2.2
- VM count and names in each chunk
- Volume group names in each chunk (shows namespace groups as single entities)

This gives the platform engineer a preview of exactly how the wave executor (Epic 4) will partition VMs during execution.

### Integration Test Setup

Integration tests extend the Story 2.1/2.2 test suite:

- **envtest** — already configured for VM CRDs and namespaces
- **New:** create PVC and StorageClass resources in envtest for storage backend tests
- **ScyllaDB testcontainer** — existing setup for DRPlan resources

The test creates a complete scenario: DRPlan + VMs + PVCs + StorageClasses → reconcile → verify `.status.preflight` structure.

### Retro Action Items (Epic 1)

- **Action #2:** All new code must meet Tier 1-3 documentation standards
  - Tier 1: Update `internal/preflight/doc.go` with 3-5 sentence godoc covering the preflight composition module's purpose, inputs, and relationship to the reconcile pipeline
  - Tier 2: Architecture block comments on `checks.go` (composition pipeline) and `storage.go` (backend resolution)
  - Tier 3: Domain 'why' comments on storage backend resolution (implicit driver selection), and on why preflight is informational not gating

### Project Structure Notes

- `internal/preflight/checks.go` — composition engine (pure logic)
- `internal/preflight/storage.go` — storage backend resolver (requires K8s API access)
- `internal/preflight/doc.go` — already exists as placeholder; update with Tier 1 godoc
- New types in `pkg/apis/soteria.io/v1alpha1/types.go` — `PreflightReport`, `PreflightWave`, `PreflightVM`, `PreflightChunk`

The `internal/` placement means the preflight module is not importable by external driver authors — correct, since it's internal orchestration logic.

### References

- Architecture: `_bmad-output/planning-artifacts/architecture.md` — "Project Structure" (`internal/preflight/checks.go`), "Data Flow" section, FR→structure mapping for `internal/preflight/`
- PRD: FR8 (plan composition preview), FR12 (chunking with namespace groups), FR21 (implicit driver selection from PVC storage class)
- Story 2.1: DRPlan Controller & VM Auto-Discovery (prerequisite — provides discovery pipeline)
- Story 2.2: Namespace-Level Volume Consistency (prerequisite — provides consistency + chunking pipeline)
- Story 2.3: Admission Webhooks (parallel — validation patterns reusable for warning generation)
- Story 7.1: Pre-flight Confirmation & Failover Trigger (future consumer — Console reads `.status.preflight`)
- Existing patterns: `pkg/registry/drplan/strategy.go` (status subresource), `pkg/registry/drplan/storage.go` (StatusREST)

## File List

### New Files
- `internal/preflight/checks.go` — preflight composition engine
- `internal/preflight/storage.go` — storage backend resolver
- `internal/preflight/checks_test.go` — unit tests for composition engine
- `internal/preflight/storage_test.go` — unit tests for storage resolver
- `test/integration/controller/drplan_preflight_test.go` — integration tests for preflight

### Modified Files
- `pkg/apis/soteria.io/v1alpha1/types.go` — added PreflightReport, PreflightWave, PreflightVM, PreflightChunk types + Preflight field on DRPlanStatus
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — regenerated (auto)
- `pkg/controller/drplan/reconciler.go` — added StorageResolver field, RBAC markers, preflight composition in reconcile loop
- `pkg/controller/drplan/reconciler_test.go` — added mockStorageBackendResolver and 4 preflight tests
- `cmd/soteria/main.go` — wired StorageClassDriverMap and TypedStorageBackendResolver
- `internal/preflight/doc.go` — updated with Tier 1 godoc
- `config/rbac/role.yaml` — regenerated with PVC + StorageClass read permissions (auto)
- `test/integration/controller/suite_test.go` — added StorageResolver to test manager setup + waitForPreflight helper

## Change Log

- 2026-04-11: Implemented pre-flight plan composition check (Story 2.4). Added PreflightReport API types, storage backend resolver, composition engine, reconciler integration, and comprehensive unit + integration tests. All existing tests pass without regressions.
- 2026-04-11: Code review fixes — (1) Always-populate `status.preflight` on every exit path including discovery errors, zero VMs, wave conflicts, and chunking failures; removed stale `setDiscoveryErrorCondition` and unified all status updates through a single `updateStatus` path. (2) Added `+listType=atomic` markers to all Preflight slice fields, removing 6 entries from `hack/api-violations.list`. (3) Documented that only the `noop` storage driver is recognized in the production binary for Story 2.4; ODF and other drivers pending Epic 3.

## Dev Agent Record

| Field | Value |
|-------|-------|
| Story file created | 2026-04-09 |
| Implementation started | 2026-04-11 |
| Implementation completed | 2026-04-11 |
| Code review requested | — |
| Code review completed | — |
| Status | done |

### Completion Notes

Implemented the full pre-flight plan composition check pipeline:

1. **API Types**: Added `PreflightReport`, `PreflightWave`, `PreflightVM`, `PreflightChunk` structs to the v1alpha1 API. The report is populated at `.status.preflight` on every reconcile cycle.

2. **Storage Backend Resolver**: `TypedStorageBackendResolver` reads kubevirt VM specs to extract PVC claim names, then maps PVC storage classes to driver names via a static `StorageClassDriverMap`. Handles edge cases: missing PVCs, unknown storage classes, mixed classes, DataVolume-backed volumes, and VMs with no volumes.

3. **Composition Engine**: Pure function `ComposeReport` assembles discovery, consistency, chunking, and storage backend data into a user-facing report with per-wave VM details and DRGroup chunking preview. Collects non-blocking warnings for informational issues.

4. **Reconciler Integration**: Preflight composition runs after the success path (discovery + consistency + chunking), right before the final status update. Storage resolution errors are gracefully handled — the report is still populated with available data and a warning is added. The reconcile never fails due to preflight issues.

5. **RBAC**: Added `get;list;watch` for PVCs and StorageClasses.

6. **Wiring**: `cmd/soteria/main.go` creates a static `StorageClassDriverMap` (TODO for Epic 3 to populate dynamically from driver registry) and passes `TypedStorageBackendResolver` to the reconciler.

**Test Coverage**:
- `internal/preflight`: 93.8% coverage — 7 storage resolver tests + 8 composition tests + 6 warning tests
- `pkg/controller/drplan`: 89.2% coverage — 4 new reconciler preflight tests
- 6 integration tests covering basic composition, namespace consistency, unknown storage backends, kubectl JSON access, multi-wave chunking, and warning population

**Documentation**: Tier 1 (doc.go updated), Tier 2 (architecture block comments on checks.go and storage.go), Tier 3 (FR21 domain comment on storage resolution rationale).
