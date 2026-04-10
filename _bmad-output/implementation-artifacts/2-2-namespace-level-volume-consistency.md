# Story 2.2: Namespace-Level Volume Consistency

Status: done

## Story

As a platform engineer,
I want to configure namespace-level volume consistency so that all VM disks in a namespace form a single VolumeGroup,
So that I can ensure crash-consistent snapshots across related VMs sharing a namespace.

## Acceptance Criteria

1. **Given** a namespace annotated with `soteria.io/consistency-level: namespace`, **When** VMs in that namespace are discovered by the DRPlan controller, **Then** all VM disks in that namespace are grouped into a single VolumeGroup (FR6), **And** the VolumeGroup is tracked in `.status.waves[].groups[]` with consistency level indicated.

2. **Given** a namespace with namespace-level consistency and VMs belonging to a DRPlan, **When** VMs in that namespace have different wave labels, **Then** the controller detects the conflict and sets a `Ready=False` condition with reason `WaveConflict` and a message identifying the mismatched VMs (FR7), **And** the DRPlan is not considered valid for execution until the conflict is resolved.

3. **Given** a valid DRPlan with namespace-consistent VMs all in the same wave, **When** DRGroup chunking is previewed, **Then** all VMs in the same namespace+wave are always contained in a single DRGroup chunk — never split across chunks (FR12 partial), **And** if the namespace group size exceeds `maxConcurrentFailovers`, the plan reports a validation error via `Ready=False` with reason `NamespaceGroupExceedsThrottle` (FR12 partial).

4. **Given** VMs in a namespace without the `soteria.io/consistency-level` annotation, **When** discovered by the DRPlan controller, **Then** each VM's disks form an individual VolumeGroup (VM-level consistency is the default).

5. **Given** the consistency and chunking logic in `pkg/engine/consistency.go` and `pkg/engine/chunker.go`, **When** unit tests run, **Then** all scenarios are covered with table-driven tests: namespace-level grouping, VM-level default, wave conflict detection, chunking with namespace groups as indivisible units, maxConcurrentFailovers validation.

## Tasks / Subtasks

- [ ] Task 1: Extend API types with volume group and consistency tracking (AC: #1, #4)
  - [ ] 1.1 Add `ConsistencyLevel` string type and constants to `pkg/apis/soteria.io/v1alpha1/types.go`: `ConsistencyLevelNamespace = "namespace"`, `ConsistencyLevelVM = "vm"`
  - [ ] 1.2 Add `VolumeGroupInfo` struct to `types.go` — fields: `Name string` (group identifier), `Namespace string`, `ConsistencyLevel ConsistencyLevel`, `VMNames []string` (VMs in this group)
  - [ ] 1.3 Add `Groups []VolumeGroupInfo` field to the `WaveInfo` struct (created in Story 2.1) — tracks volume groups per wave
  - [ ] 1.4 Add `ConsistencyAnnotation` constant: `soteria.io/consistency-level`
  - [ ] 1.5 Add OpenAPI validation markers: `VolumeGroupInfo.Name` required, `VolumeGroupInfo.VMNames` minItems=1
  - [ ] 1.6 Run `hack/update-codegen.sh` to regenerate deepcopy + OpenAPI; verify `hack/verify-codegen.sh` passes

- [ ] Task 2: Implement namespace consistency resolution engine (AC: #1, #2, #4)
  - [ ] 2.1 Create `pkg/engine/consistency.go` with Tier 2 architecture block comment explaining: how namespace annotations drive consistency level, how VMs are grouped into VolumeGroups, and how wave conflicts are detected
  - [ ] 2.2 Define `NamespaceConsistency` type — `Namespace string`, `Level ConsistencyLevel` (resolved from namespace annotation)
  - [ ] 2.3 Define `NamespaceLookup` interface — `GetConsistencyLevel(ctx context.Context, namespace string) (ConsistencyLevel, error)` — allows mock injection; real implementation reads namespace annotations via client-go
  - [ ] 2.4 Implement `DefaultNamespaceLookup` struct — uses `corev1client.NamespacesGetter` to fetch namespace, reads `soteria.io/consistency-level` annotation, returns `ConsistencyLevelVM` if annotation is missing or has any value other than `"namespace"`
  - [ ] 2.5 Implement `ResolveVolumeGroups(vms []VMReference, waveLabel string, nsLookup NamespaceLookup, ctx context.Context) (*ConsistencyResult, error)` — pure orchestration function:
    - Groups VMs by namespace
    - For each namespace, queries consistency level via `nsLookup`
    - Namespace-level: all VMs in that namespace form one VolumeGroup named `ns-<namespace>`
    - VM-level (default): each VM forms its own VolumeGroup named `vm-<namespace>-<name>`
    - Returns `ConsistencyResult` with `VolumeGroups []VolumeGroupInfo`, `WaveConflicts []WaveConflict`
  - [ ] 2.6 Define `WaveConflict` type — `Namespace string`, `VMNames []string`, `WaveKeys []string` (distinct wave values found)
  - [ ] 2.7 Implement wave conflict detection within `ResolveVolumeGroups`: for namespace-level namespaces, verify all VMs have the same wave label value; if not, record a `WaveConflict`
  - [ ] 2.8 Add Tier 3 domain 'why' comments explaining: why namespace-level consistency exists (crash-consistent snapshots require all disks in a namespace to be part of a single VolumeGroup for atomic storage operations), why wave conflict detection is needed (namespace-level VMs must move together, which is impossible if they're in different waves)

- [ ] Task 3: Implement DRGroup chunking with namespace-aware grouping (AC: #3)
  - [ ] 3.1 Create `pkg/engine/chunker.go` with Tier 2 architecture block comment explaining the chunking algorithm: how VMs are partitioned into DRGroups respecting `maxConcurrentFailovers`, and how namespace groups act as indivisible units
  - [ ] 3.2 Define `ChunkInput` type — `WaveGroups []WaveGroupWithVolumes` (wave groups with resolved volume group info)
  - [ ] 3.3 Define `WaveGroupWithVolumes` type — `WaveKey string`, `VolumeGroups []VolumeGroupInfo`
  - [ ] 3.4 Define `DRGroupChunk` type — `Name string`, `VMs []VMReference`, `VolumeGroups []VolumeGroupInfo`
  - [ ] 3.5 Define `ChunkResult` type — `Waves []WaveChunks`, `Errors []ChunkError`
  - [ ] 3.6 Define `WaveChunks` type — `WaveKey string`, `Chunks []DRGroupChunk`
  - [ ] 3.7 Implement `ChunkWaves(input ChunkInput, maxConcurrent int) ChunkResult` — pure function:
    - For each wave, partition VolumeGroups into DRGroup chunks
    - Namespace-level VolumeGroups are indivisible: all VMs in the group go into the same chunk
    - VM-level VolumeGroups are individual VMs that can fill remaining capacity
    - If a namespace group exceeds `maxConcurrent`, record a `ChunkError` (cannot fit)
    - If remaining chunk capacity cannot fit the next namespace group, start a new chunk (current chunk may be underutilized)
    - DRGroup chunks are named `wave-<key>-group-<index>` (0-indexed)
  - [ ] 3.8 Define `ChunkError` type — `WaveKey string`, `Namespace string`, `GroupSize int`, `MaxConcurrent int`
  - [ ] 3.9 Add Tier 3 domain 'why' comment on the indivisible namespace group constraint: crash-consistent snapshots require all VMs in a namespace-level VolumeGroup to have their volumes promoted atomically in the same DRGroup; splitting them across chunks would break consistency guarantees

- [ ] Task 4: Unit tests for consistency engine (AC: #5)
  - [ ] 4.1 Create `pkg/engine/consistency_test.go`
  - [ ] 4.2 Create `MockNamespaceLookup` for tests — configurable per-namespace consistency levels
  - [ ] 4.3 Table-driven `TestResolveVolumeGroups` covering:
    - All VMs in namespace without annotation → individual VolumeGroups per VM (VM-level default)
    - All VMs in namespace with `soteria.io/consistency-level: namespace` → single VolumeGroup per namespace
    - Mixed: VMs across 3 namespaces, 1 namespace-level, 2 VM-level → correct grouping
    - Namespace-level with VMs in same wave → no wave conflicts
    - Namespace-level with VMs in different waves → `WaveConflict` returned with correct details
    - Multiple namespace-level namespaces with different waves → multiple independent conflicts
    - Empty VM list → empty result
    - Single VM in namespace-level namespace → single VolumeGroup with 1 VM (edge case)
  - [ ] 4.4 Test `DefaultNamespaceLookup` with fake client — verify annotation reading, missing annotation defaults to VM-level, invalid annotation value defaults to VM-level

- [ ] Task 5: Unit tests for chunking engine (AC: #5)
  - [ ] 5.1 Create `pkg/engine/chunker_test.go`
  - [ ] 5.2 Table-driven `TestChunkWaves` covering:
    - All VM-level VMs, maxConcurrent=4, 10 VMs → 3 chunks (4, 4, 2)
    - Single namespace group of 3 VMs, maxConcurrent=4 → 1 chunk with 3 VMs (room for 1 more)
    - Namespace group (3) + VM-level VMs (5), maxConcurrent=4 → chunk1: namespace group (3) + 1 VM, chunk2: 4 VMs
    - Namespace group (3) cannot fit into remaining capacity (1 slot left), maxConcurrent=4 → new chunk started
    - Namespace group (5) exceeds maxConcurrent (4) → `ChunkError` recorded
    - Multiple waves → each wave chunked independently
    - Namespace group exactly equals maxConcurrent → fits in one chunk
    - Empty wave → no chunks
    - Two namespace groups (3 each), maxConcurrent=4 → 2 chunks (one group per chunk, second group cannot fit in first)
  - [ ] 5.3 Verify DRGroup chunk naming convention: `wave-<key>-group-<index>`

- [ ] Task 6: Extend DRPlan reconciler with consistency logic (AC: #1, #2, #3, #4)
  - [ ] 6.1 Extend `DRPlanReconciler` struct (from Story 2.1) with `NamespaceLookup engine.NamespaceLookup` field
  - [ ] 6.2 Update `Reconcile` method to call `engine.ResolveVolumeGroups()` after VM discovery and wave grouping — passes discovered VMs and namespace lookup
  - [ ] 6.3 Handle wave conflicts: if `ConsistencyResult.WaveConflicts` is non-empty, set `Ready=False` condition with reason `WaveConflict`, message listing each namespace and its conflicting VMs/waves; do NOT populate `.status.waves[].groups[]` (plan is invalid)
  - [ ] 6.4 Handle successful resolution: populate `.status.waves[].groups[]` with `VolumeGroupInfo` from `ConsistencyResult`
  - [ ] 6.5 Call `engine.ChunkWaves()` with resolved volume groups and plan's `maxConcurrentFailovers`
  - [ ] 6.6 Handle chunk errors: if `ChunkResult.Errors` is non-empty, set `Ready=False` condition with reason `NamespaceGroupExceedsThrottle`, message: `"maxConcurrentFailovers (<N>) is less than namespace+wave group size (<M>) for namespace <ns> wave <w>"`
  - [ ] 6.7 Add RBAC marker: `+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch` (needed for namespace annotation lookup)
  - [ ] 6.8 Emit events: `ConsistencyResolved` (Info) when volume groups are successfully formed, `WaveConflictDetected` (Warning) on wave conflicts, `ChunkingFailed` (Warning) when namespace group exceeds throttle
  - [ ] 6.9 Structured logging: `log.Info("Resolved volume groups", "namespaceLevel", count, "vmLevel", count)`, `log.Info("Detected wave conflict", "namespace", ns, "waves", waveKeys)`

- [ ] Task 7: Wire namespace lookup into `cmd/soteria/main.go` (AC: #1)
  - [ ] 7.1 After manager creation, create `DefaultNamespaceLookup` using the manager's client (or a typed `corev1client` from rest config)
  - [ ] 7.2 Pass `NamespaceLookup` to `DRPlanReconciler` during construction
  - [ ] 7.3 Verify `go build ./cmd/soteria/` succeeds
  - [ ] 7.4 Run `make manifests` to regenerate RBAC with new namespace read permission

- [ ] Task 8: Unit tests for reconciler consistency integration (AC: #1, #2, #3, #4)
  - [ ] 8.1 Extend `pkg/controller/drplan/reconciler_test.go` (from Story 2.1) with mock `NamespaceLookup`
  - [ ] 8.2 Test: reconcile with all VM-level namespaces → `.status.waves[].groups[]` has individual VolumeGroups, Ready=True
  - [ ] 8.3 Test: reconcile with namespace-level namespace → `.status.waves[].groups[]` has single VolumeGroup per namespace, Ready=True
  - [ ] 8.4 Test: reconcile with wave conflict → Ready=False, reason=WaveConflict, no groups populated
  - [ ] 8.5 Test: reconcile with namespace group exceeding maxConcurrentFailovers → Ready=False, reason=NamespaceGroupExceedsThrottle
  - [ ] 8.6 Test: reconcile after wave conflict resolved (VMs moved to same wave) → Ready=True, groups populated
  - [ ] 8.7 Test: reconcile with mixed consistency (some namespace-level, some VM-level) → correct grouping for both

- [ ] Task 9: Integration tests (AC: #1, #2, #3, #4)
  - [ ] 9.1 Extend `test/integration/controller/suite_test.go` (from Story 2.1) — ensure namespaces with annotations are supported in envtest
  - [ ] 9.2 Create `test/integration/controller/drplan_consistency_test.go` with `//go:build integration` tag
  - [ ] 9.3 `TestDRPlanReconciler_NamespaceConsistency_VolumeGroupsFormed` — create namespace with annotation, DRPlan + VMs with matching labels in same wave, verify `.status.waves[].groups[]` has single namespace-level VolumeGroup
  - [ ] 9.4 `TestDRPlanReconciler_VMConsistency_IndividualVolumeGroups` — VMs in namespace without annotation, verify individual VolumeGroups
  - [ ] 9.5 `TestDRPlanReconciler_WaveConflict_ReadyFalse` — VMs in namespace-level namespace with different wave labels, verify Ready=False with WaveConflict reason
  - [ ] 9.6 `TestDRPlanReconciler_WaveConflictResolved_ReadyTrue` — fix wave labels after conflict, verify Ready transitions to True
  - [ ] 9.7 `TestDRPlanReconciler_NamespaceGroupExceedsThrottle_ReadyFalse` — namespace group size > maxConcurrentFailovers, verify Ready=False with correct reason
  - [ ] 9.8 `TestDRPlanReconciler_MixedConsistency_CorrectGrouping` — namespaces with and without annotation in same plan, verify correct grouping for both
  - [ ] 9.9 `TestDRPlanReconciler_ChunkingPreview_NamespaceGroupIndivisible` — verify DRGroup chunks never split a namespace group across chunks

- [ ] Task 10: Verify and finalize
  - [ ] 10.1 Run `make lint-fix` to auto-fix code style
  - [ ] 10.2 Run `make test` — all unit tests pass
  - [ ] 10.3 Run `make integration` — all integration tests pass
  - [ ] 10.4 Run `make manifests` — verify RBAC regenerated with namespace read permission
  - [ ] 10.5 Verify Tier 1/2/3 documentation standards met (retro action item #2)

## Dev Notes

### Architecture Context

This story builds directly on Story 2.1 (DRPlan Controller & VM Auto-Discovery). Story 2.1 establishes the controller skeleton, VM discovery engine, and wave grouping. This story adds the consistency layer on top: namespace-level volume groups, wave conflict detection, and DRGroup chunking.

**Key concept:** "Consistency level" determines how VM disks are grouped into VolumeGroups for atomic storage operations. VM-level (default) = each VM's disks are an independent VolumeGroup. Namespace-level = all VMs in a namespace share one VolumeGroup, ensuring crash-consistent snapshots across related applications.

### Namespace Annotation Convention

The consistency level is configured via a **namespace annotation**, not on the DRPlan or VM:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: erp-database
  annotations:
    soteria.io/consistency-level: "namespace"
```

This follows the architecture's convention: `soteria.io/<key>` with kebab-case keys. The annotation is on the namespace because consistency is a namespace-level property — it applies to all VMs in that namespace regardless of which DRPlan selects them.

If the annotation is missing or has any value other than `"namespace"`, the default is VM-level consistency.

### Wave Conflict Rule

FR7 requires that VMs in a namespace with namespace-level consistency must all be in the same wave. This is enforced at two levels:
1. **Controller** (this story): detects conflicts during reconciliation and sets `Ready=False`
2. **Admission webhook** (Story 2.3): rejects mutations that would create conflicts

The controller detection is the authoritative check — it runs on every reconcile and catches conflicts regardless of how they were introduced (direct API edits, label changes, etc.).

### DRGroup Chunking Algorithm

The chunking algorithm in `pkg/engine/chunker.go` partitions VMs within a wave into DRGroup chunks, respecting `maxConcurrentFailovers`:

1. Collect all VolumeGroups in the wave
2. Sort namespace-level groups first (larger, harder to place), then VM-level groups
3. For each VolumeGroup:
   - If namespace-level: check if it fits in the current chunk (remaining capacity >= group size)
     - If yes: add to current chunk
     - If no: start a new chunk (current chunk may be underutilized)
     - If group size > maxConcurrent: record error (group can never fit)
   - If VM-level: add to current chunk; if full, start a new chunk

`maxConcurrentFailovers` always counts individual VMs (FR12), not VolumeGroups. A namespace group of 3 VMs consumes 3 slots in the chunk capacity.

### Dependency on Story 2.1

This story assumes Story 2.1 has been completed and the following exist:
- `DRPlanStatus.Waves []WaveInfo` with `DiscoveredVM` structs
- `pkg/engine/discovery.go` with `VMReference`, `GroupByWave`, `VMDiscoverer` interface, `TypedVMDiscoverer`
- `kubevirt.io/api` dependency and `kubevirtv1.VirtualMachine` registered in the scheme
- `pkg/controller/drplan/reconciler.go` with `DRPlanReconciler`, VM watch (secondary), and event-driven reconcile loop
- Integration test infrastructure in `test/integration/controller/`

### Type Changes

`WaveInfo` (from Story 2.1) gains a `Groups []VolumeGroupInfo` field. `DRPlanStatus` does not change beyond what 2.1 introduced — the volume group data lives inside the existing wave structure. After editing `types.go`, run:

```bash
hack/update-codegen.sh   # regenerate deepcopy + openapi
hack/verify-codegen.sh   # CI verification
```

### Namespace Client Access

The reconciler needs to read namespace annotations. This requires:
- RBAC marker: `+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch`
- A `corev1client.NamespacesGetter` or similar client injected into `DefaultNamespaceLookup`
- The manager's cached client can be used since namespace data is relatively stable

### Retro Action Items (Epic 1)

- **Action #2:** All new code must meet Tier 1-3 documentation standards
  - Tier 1: Add `pkg/engine/doc.go` update to cover consistency and chunking
  - Tier 2: Architecture block comments on `consistency.go` and `chunker.go`
  - Tier 3: Domain 'why' comments on namespace grouping rationale and indivisible chunk constraint

### References

- Architecture: `_bmad-output/planning-artifacts/architecture.md` — "Data Flow" section, naming conventions for annotations (`soteria.io/consistency-level`)
- PRD: FR6 (namespace-level volume consistency), FR7 (same-wave enforcement), FR12 (chunking respects namespace groups)
- NFR15: Admission webhooks validate namespace consistency conflicts (Story 2.3 builds on this)
- Story 2.1: DRPlan Controller & VM Auto-Discovery (prerequisite)
- Story 2.3: Admission Webhooks — will add webhook-level enforcement for wave conflicts detected here
- Story 2.4: Pre-flight Plan Composition Check — will use chunking preview data from this story
- Existing patterns: `pkg/engine/discovery.go` (from Story 2.1), `pkg/controller/drplan/reconciler.go` with VM watch (from Story 2.1)
- controller-runtime FAQ: https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md

## Dev Agent Record

| Field | Value |
|-------|-------|
| Story file created | 2026-04-09 |
| Implementation started | 2026-04-10 |
| Implementation completed | 2026-04-10 |
| Code review requested | 2026-04-10 |
| Code review completed | 2026-04-10 |
| Status | done |
