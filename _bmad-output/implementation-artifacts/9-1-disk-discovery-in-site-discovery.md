# Story 9.1: Disk Discovery in SiteDiscovery

Status: done

## Story

As a platform engineer,
I want each site's VM discovery to include per-disk PVC topology (disk name, PVC name, storage class),
So that the system has visibility into the storage layout of each VM for cross-site validation.

## Acceptance Criteria

1. **AC1 — DiscoveredDisk type and DiscoveredVM enrichment:** A new `DiscoveredDisk` type is added to `pkg/apis/soteria.io/v1alpha1/types.go` with fields: `name` (string, from `domain.devices.disks[*].name`), `pvcName` (string, resolved PVC claim name), and `storageClass` (string, from `PVC.spec.storageClassName`). `DiscoveredVM` gains a `disks []DiscoveredDisk` field (`omitempty`, `+listType=atomic`). `make manifests generate` regenerates deepcopy and OpenAPI without errors.

2. **AC2 — Active-site disk enrichment in reconciler:** When the active-site DRPlan reconciler discovers VMs, for each VM it joins `spec.template.spec.domain.devices.disks[*]` with `spec.template.spec.volumes[*]` by name, filters for volumes backed by `persistentVolumeClaim` or `dataVolume` (all other volume types silently ignored), resolves the PVC via the cached `client.Reader` and reads `storageClassName`, and writes the resolved disks to both `WaveInfo.VMs[*].Disks` and `SiteDiscovery.VMs[*].Disks`.

3. **AC3 — Passive-site disk enrichment:** The passive-site reconciler (`reconcilePassiveSite`) performs the same disk enrichment when building its `SiteDiscovery`, so both sites report identical disk topology structures.

4. **AC4 — No-PVC VMs handled gracefully:** A VM with only non-PVC volumes (containerDisk, cloudInitNoCloud, etc.) has its `disks` field empty (`[]` or nil) — this is valid and does not produce an error.

5. **AC5 — Missing PVC self-healing:** When a DataVolume has not yet created its PVC, the disk entry is recorded with empty `pvcName` and empty `storageClass` (PVC GET returns NotFound). On the next reconcile cycle when the PVC exists, the disk entry is populated — self-healing via the reconcile loop.

6. **AC6 — Cached client.Reader for PVC resolution:** PVC reads during discovery use the controller-runtime cached `client.Reader` (informer-backed) instead of direct API calls, eliminating per-PVC API server round-trips.

7. **AC7 — Tests:** New tests verify disk enrichment for PVC volumes, DataVolume volumes, mixed volumes (PVC + DataVolume + containerDisk), no-PVC-only VMs, and missing-PVC (NotFound) self-healing. All existing unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [x] Task 1: Add DiscoveredDisk type and enrich DiscoveredVM (AC: #1)
  - [x] 1.1 Add `DiscoveredDisk` struct to `pkg/apis/soteria.io/v1alpha1/types.go` immediately before `DiscoveredVM`
  - [x] 1.2 Add `Disks []DiscoveredDisk` field to `DiscoveredVM` with `json:"disks,omitempty"` and `+listType=atomic`
  - [x] 1.3 Run `make manifests generate` to regenerate deepcopy and OpenAPI
  - [x] 1.4 Verify no compilation errors

- [x] Task 2: Create disk enrichment function (AC: #2, #4, #5, #6)
  - [x] 2.1 Add `DiskEnricher` interface and `KubeVirtDiskEnricher` implementation to `pkg/engine/disk_enricher.go`
  - [x] 2.2 Implement `EnrichDisks(ctx, vmName, namespace string) ([]DiscoveredDisk, error)` — joins disks with volumes, resolves PVCs, reads storageClassName
  - [x] 2.3 Add `NoOpDiskEnricher` for dev/CI environments
  - [x] 2.4 Write `doc.go`-level comment if new package file

- [x] Task 3: Wire disk enrichment into active-site reconciler (AC: #2)
  - [x] 3.1 Add `DiskEnricher` field to `DRPlanReconciler` struct
  - [x] 3.2 In the wave-building loop (lines ~186-199), call `DiskEnricher.EnrichDisks` per VM and set `discoveredVMs[j].Disks`
  - [x] 3.3 Since `collectVMsFromWaves` is used for active-site SiteDiscovery, the disk data propagates automatically

- [x] Task 4: Wire disk enrichment into passive-site reconciler (AC: #3)
  - [x] 4.1 In `reconcilePassiveSite` (lines ~341-395), after building `discoveredVMs`, call `DiskEnricher.EnrichDisks` per VM

- [x] Task 5: Wire DiskEnricher in main.go setup (AC: #2, #3)
  - [x] 5.1 In `main.go`, construct `KubeVirtDiskEnricher` (using the cached reader) and pass to `DRPlanReconciler`
  - [x] 5.2 Construct `NoOpDiskEnricher` when no-op mode is active

- [x] Task 6: Unit tests for DiskEnricher (AC: #7)
  - [x] 6.1 Create `pkg/engine/disk_enricher_test.go` with table-driven tests:
    - VM with 2 PVC-backed disks → 2 DiscoveredDisk entries with pvcName + storageClass
    - VM with DataVolume-backed disk → DiscoveredDisk with DV name as pvcName + storageClass
    - VM with mixed volumes (PVC + DataVolume + containerDisk + cloudInit) → only PVC/DV disks
    - VM with no PVC/DV volumes → empty disks slice
    - VM with PVC that doesn't exist yet (NotFound) → disk entry with empty pvcName + empty storageClass
    - VM with PVC that has nil storageClassName → disk entry with empty storageClass
  - [x] 6.2 Create `pkg/controller/drplan/reconciler_disk_test.go` for reconciler integration tests verifying disks appear in waves and SiteDiscovery

- [x] Task 7: Run make manifests generate, lint, test (AC: #7)
  - [x] 7.1 `make manifests generate` — zero errors
  - [x] 7.2 `make lint-fix` — zero new lint errors (1 pre-existing goconst in executor_test.go)
  - [x] 7.3 `make test` — all tests pass, zero regressions

### Review Findings

- [x] [Review][Patch] Replace fake-client reconciler disk tests with envtest coverage that exercises the real DRPlan controller path and real disk enrichment wiring [pkg/controller/drplan/reconciler_disk_test.go:28] — Fixed: switched integration suite from NoOpDiskEnricher to real KubeVirtDiskEnricher{Reader: mgr.GetClient()} so envtest exercises the real enrichment path
- [x] [Review][Patch] Extend the missing-PVC self-healing test to create the PVC and verify a second reconcile/enrichment populates `pvcName` and `storageClass` [pkg/engine/disk_enricher_test.go:282] — Fixed: added Phase 2 that creates the PVC and re-enriches, asserting pvcName and storageClass are populated

## Dev Notes

### Scope & Approach

This is a Go backend story. All changes are in `pkg/apis/`, `pkg/engine/`, `pkg/controller/drplan/`, and `cmd/`. No console-plugin changes. The story enriches the existing VM discovery pipeline with per-disk PVC topology data.

**Change pattern:** Add API type → create enricher function → wire into both reconciler paths → tests → regenerate manifests.

### Critical: KubeVirt VM Disk-to-Volume Join

KubeVirt VMs have two separate arrays that must be joined by name:

1. **Disks** at `vm.Spec.Template.Spec.Domain.Devices.Disks[]` — each has a `Name` field (disk identifier visible to the guest OS)
2. **Volumes** at `vm.Spec.Template.Spec.Volumes[]` — each has a `Name` field matching a disk name, plus a volume source

The join logic:
```go
// Build volume lookup map
volMap := make(map[string]kubevirtv1.Volume, len(vm.Spec.Template.Spec.Volumes))
for _, vol := range vm.Spec.Template.Spec.Volumes {
    volMap[vol.Name] = vol
}

// Iterate disks, resolve PVC for each
for _, disk := range vm.Spec.Template.Spec.Domain.Devices.Disks {
    vol, ok := volMap[disk.Name]
    if !ok {
        continue // orphan disk — no volume source
    }
    var pvcName string
    if vol.PersistentVolumeClaim != nil {
        pvcName = vol.PersistentVolumeClaim.ClaimName
    } else if vol.DataVolume != nil {
        pvcName = vol.DataVolume.Name // DV creates PVC with same name
    } else {
        continue // containerDisk, cloudInitNoCloud, etc. — skip
    }
    // Resolve PVC → storageClass...
}
```

### Critical: DataVolume PVC Name Convention

When a KubeVirt VM uses a `DataVolume` volume source, CDI creates a PVC with the **same name** as the DataVolume. So `vol.DataVolume.Name` is the PVC name to look up. This is a CDI convention, not a coincidence.

### Critical: PVC Resolution for StorageClass

After determining the PVC name, resolve it to get storageClassName:
```go
var pvc corev1.PersistentVolumeClaim
err := reader.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: namespace}, &pvc)
if apierrors.IsNotFound(err) {
    // PVC not yet created (DataVolume still provisioning)
    // Record disk with empty pvcName and storageClass — self-heals next reconcile
    disks = append(disks, DiscoveredDisk{Name: disk.Name})
    continue
}
if err != nil {
    return nil, fmt.Errorf("fetching PVC %s/%s: %w", namespace, pvcName, err)
}
sc := ""
if pvc.Spec.StorageClassName != nil {
    sc = *pvc.Spec.StorageClassName
}
disks = append(disks, DiscoveredDisk{
    Name:         disk.Name,
    PVCName:      pvcName,
    StorageClass: sc,
})
```

### Critical: Existing PVCResolver vs New DiskEnricher

The existing `KubeVirtPVCResolver` in `pkg/engine/pvc_resolver.go` resolves PVC **names** from VM volumes, but:
- It does NOT iterate disks (only volumes)
- It does NOT resolve storageClass
- It does NOT join disks with volumes by name
- It does NOT handle DataVolume volume sources

**DO NOT modify `PVCResolver`** — it is used by the volume group health polling path which only needs PVC names. Create a separate `DiskEnricher` that handles the full disk-to-PVC-to-storageClass pipeline. The two concerns are distinct and have different consumers.

### Critical: Use Cached client.Reader, Not Direct API Calls

The reconciler's `client.Client` includes a cached reader backed by informers. Use `r.Client.Get()` (or pass the client as a `client.Reader` to the enricher) for PVC lookups. This means:
- **First read after controller startup** may miss recently created PVCs (informer hasn't synced)
- This is acceptable — self-heals on next reconcile
- **No direct `CoreV1Interface` calls** — would bypass the cache and create per-PVC API server round-trips

### Critical: RBAC Already Covers PVCs and StorageClasses

The DRPlan reconciler already has RBAC for PVCs and StorageClasses (lines 92-93 in reconciler.go):
```go
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
```
No new RBAC markers needed.

### Critical: DiscoveredDisk Type Design

```go
// DiscoveredDisk describes a single disk attached to a VM and its backing PVC topology.
type DiscoveredDisk struct {
    // Name is the disk name from the VM's domain.devices.disks spec
    // (the identifier visible to the guest OS).
    Name string `json:"name"`
    // PVCName is the PersistentVolumeClaim backing this disk.
    // Empty when the PVC has not yet been created (e.g., DataVolume provisioning).
    PVCName string `json:"pvcName,omitempty"`
    // StorageClass is the storage class of the backing PVC.
    // Empty when the PVC does not exist yet or has no storageClassName.
    StorageClass string `json:"storageClass,omitempty"`
}
```

Place this immediately before `DiscoveredVM` in `types.go` (line ~250). Add `+listType=atomic` on the `Disks` field in `DiscoveredVM`.

### Critical: DiskEnricher Interface and Placement

Place in `pkg/engine/disk_enricher.go` alongside the existing `PVCResolver`:

```go
// DiskEnricher resolves per-disk PVC topology for a VM.
type DiskEnricher interface {
    EnrichDisks(ctx context.Context, vmName, namespace string) ([]soteriav1alpha1.DiscoveredDisk, error)
}
```

`KubeVirtDiskEnricher` needs:
- `client.Reader` (for PVC GET — uses the cached informer-backed reader)
- Fetches the full VM to access `domain.devices.disks` and `volumes`

`NoOpDiskEnricher` returns `nil, nil` (same pattern as `NoOpPVCResolver`).

### Critical: Passive-Site Discovery Enhancement

`reconcilePassiveSite` currently builds `DiscoveredVM` with only Name/Namespace (line ~362-366). Must be enhanced to call `DiskEnricher.EnrichDisks` for each VM:

```go
discoveredVMs := make([]soteriav1alpha1.DiscoveredVM, len(vms))
for i, vm := range vms {
    disks, err := r.DiskEnricher.EnrichDisks(ctx, vm.Name, vm.Namespace)
    if err != nil {
        logger.V(1).Info("Disk enrichment failed for VM, skipping disks", "vm", vm.Name, "error", err)
        // Proceed without disks — self-heals next reconcile
    }
    discoveredVMs[i] = soteriav1alpha1.DiscoveredVM{
        Name:      vm.Name,
        Namespace: vm.Namespace,
        Disks:     disks,
    }
}
```

### Critical: Active-Site Wave Building Enhancement

The active-site wave-building loop (lines ~186-199) converts `VMReference` → `DiscoveredVM`. Enrich with disks:

```go
for j, vm := range wg.VMs {
    disks, err := r.DiskEnricher.EnrichDisks(ctx, vm.Name, vm.Namespace)
    if err != nil {
        logger.V(1).Info("Disk enrichment failed for VM, skipping disks", "vm", vm.Name, "error", err)
    }
    discoveredVMs[j] = soteriav1alpha1.DiscoveredVM{
        Name:      vm.Name,
        Namespace: vm.Namespace,
        Disks:     disks,
    }
}
```

Since `collectVMsFromWaves` gathers all VMs from waves for the active-site SiteDiscovery, the disk data propagates automatically without additional code.

### Critical: Error Handling Strategy for Disk Enrichment

Disk enrichment failures should NOT fail the reconcile loop. Log at V(1) and continue with empty disks. Reasons:
- A single VM's PVC resolution failure should not block the entire plan's discovery
- Self-heals on next reconcile (30s)
- The DRPlan reconciler already tolerates partial data (e.g., SiteDiscovery waiting)

### Critical: Test Strategy

**Unit tests** (`pkg/engine/disk_enricher_test.go`):
- Use controller-runtime's `fake.NewClientBuilder()` for PVC/VM fixtures — acceptable here because the enricher only does simple GET operations (not reconciler-level operations that need envtest)
- Table-driven tests covering all AC scenarios

**Reconciler integration tests** (`pkg/controller/drplan/reconciler_disk_test.go`):
- Use envtest (existing `suite_test.go` setup)
- Create VMs with disk specs, PVCs, and StorageClasses in the test environment
- Verify reconciled DRPlan status has disks in both `Waves[*].VMs[*].Disks` and `SiteDiscovery.VMs[*].Disks`

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| `KubeVirtPVCResolver` (interface + real + noop) | `pkg/engine/pvc_resolver.go` | Same pattern for `DiskEnricher` |
| `DRPlanReconciler` struct injection | `pkg/controller/drplan/reconciler.go:100-116` | Add `DiskEnricher` field |
| `DiscoveredVM` type with deepcopy | `pkg/apis/soteria.io/v1alpha1/types.go:250-256` | Extend with `Disks` field |
| `reconcilePassiveSite` VM building | `pkg/controller/drplan/reconciler.go:362-366` | Enrich with disks |
| Active-site wave building | `pkg/controller/drplan/reconciler.go:186-199` | Enrich with disks |
| `+listType=atomic` marker | `SiteDiscovery.VMs`, `WaveInfo.VMs` | Same for `DiscoveredVM.Disks` |
| Error-tolerant enrichment | `composePreflightReport` (logs + continues) | Same log-and-continue for disk errors |
| `NoOpPVCResolver` returns nil | `pkg/engine/pvc_resolver.go:62-68` | Same for `NoOpDiskEnricher` |
| `main.go` wiring | Existing `PVCResolver` construction | Same pattern for `DiskEnricher` |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `DiscoveredDisk` type, add `Disks` field to `DiscoveredVM` | API schema change (~10 lines) |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-generated | `make generate` |
| `pkg/engine/disk_enricher.go` | New file — `DiskEnricher` interface, `KubeVirtDiskEnricher`, `NoOpDiskEnricher` | Core enrichment logic (~80 lines) |
| `pkg/controller/drplan/reconciler.go` | Add `DiskEnricher` field, enrich disks in wave building + passive site | ~15 lines changed |
| `cmd/main.go` | Wire `DiskEnricher` into reconciler setup | ~5 lines |
| `pkg/engine/disk_enricher_test.go` | New file — unit tests | ~150 lines |
| `pkg/controller/drplan/reconciler_disk_test.go` | New file — integration tests | ~100 lines |
| `config/crd/bases/soteria.io_drplans.yaml` | Auto-generated | `make manifests` |

### Execution Order

1. Task 1 (API types) — foundation for everything else
2. Task 2 (DiskEnricher) — core logic, independently testable
3. Task 6.1 (unit tests for enricher) — validate enricher before wiring
4. Task 3 (active-site wiring) + Task 4 (passive-site wiring)
5. Task 5 (main.go setup)
6. Task 6.2 (integration tests)
7. Task 7 (manifests, lint, full test suite)

### Previous Story Learnings (from 8.5)

- **Watch-driven reactivity works seamlessly** — once SiteDiscovery is updated, cross-site comparison picks it up automatically on the next reconcile
- **Strategic merge patch preserves concurrent writes** — use `client.MergeFrom` for all status patches (already in place for SiteDiscovery)
- **Self-healing via reconcile loop** — partial/missing data fills in on subsequent cycles; no need for explicit retry logic
- **30s passive-site requeue** is the standard polling interval; no change needed
- **`collectVMsFromWaves` propagates all VM data to active-site SiteDiscovery** — enriching VMs in waves automatically enriches SiteDiscovery

### Project Structure Notes

- API types live in `pkg/apis/soteria.io/v1alpha1/types.go` (sample-apiserver pattern, not kubebuilder `api/` convention)
- Controllers live in `pkg/controller/drplan/` (not `internal/controller/`)
- Engine utilities in `pkg/engine/` — public API for driver authors
- Generated files: `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`, `config/rbac/role.yaml` — never hand-edit
- Run `make manifests generate` after any types.go change

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L250-L269] — DiscoveredVM and SiteDiscovery types (enrich here)
- [Source: pkg/engine/pvc_resolver.go#L28-L68] — PVCResolver interface pattern (follow for DiskEnricher)
- [Source: pkg/controller/drplan/reconciler.go#L100-L116] — DRPlanReconciler struct (add DiskEnricher field)
- [Source: pkg/controller/drplan/reconciler.go#L186-L199] — Active-site wave building loop (enrich with disks)
- [Source: pkg/controller/drplan/reconciler.go#L341-L395] — reconcilePassiveSite (enrich with disks)
- [Source: pkg/controller/drplan/reconciler.go#L597-L608] — collectVMsFromWaves (propagates disk data automatically)
- [Source: pkg/controller/drplan/reconciler.go#L92-L93] — RBAC for PVCs and StorageClasses (already present)
- [Source: pkg/engine/discovery.go#L98-L128] — TypedVMDiscoverer (reads KubeVirt VMs)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-9.1] — Epic requirements
- [Source: _bmad-output/project-context.md] — Critical rules, naming conventions, testing pyramid

## Dev Agent Record

### Agent Model Used

Opus 4.6

### Debug Log References

- Pre-existing goconst lint (executor_test.go "group-3") — not from this story, existed before
- DiskEnricher tests required separate scheme with corev1 registered (existing newTestScheme only had soteria+kubevirt)
- findCondition helper already existed in health_test.go — removed duplicate from reconciler_disk_test.go
- dupl lint resolved by consolidating active/passive site discovery tests into table-driven test

### Completion Notes List

- DiscoveredDisk type added to types.go with name/pvcName/storageClass fields
- DiskEnricher interface follows same pattern as PVCResolver (KubeVirt + NoOp implementations)
- KubeVirtDiskEnricher joins disks→volumes by name, filters for PVC/DataVolume sources, resolves PVC storageClass
- Missing PVC (DataVolume still provisioning) records disk with empty pvcName/storageClass — self-heals next reconcile
- Enrichment errors are non-fatal: logged at V(1), VM continues with empty disks
- Nil DiskEnricher (backward compat) skips enrichment entirely
- Active-site: disks enriched in wave-building loop, propagate to SiteDiscovery via collectVMsFromWaves
- Passive-site: disks enriched in reconcilePassiveSite before SiteDiscovery patch
- main.go: KubeVirtDiskEnricher (using mgr.GetClient() as cached Reader) or NoOpDiskEnricher based on --noop-fallback
- Integration test suite: KubeVirtDiskEnricher wired in suite_test.go (real enrichment path)
- 9 DiskEnricher unit tests + 6 reconciler disk tests = 15 new tests
- drplan coverage: 85.4% → 85.8%, engine coverage: 80.4% → 80.7%
- Zero regressions across all packages

### File List

- `pkg/apis/soteria.io/v1alpha1/types.go` — Added DiscoveredDisk type, added Disks field to DiscoveredVM
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — Auto-generated (make generate)
- `pkg/engine/disk_enricher.go` — New: DiskEnricher interface, KubeVirtDiskEnricher, NoOpDiskEnricher
- `pkg/engine/disk_enricher_test.go` — New: 9 unit tests for DiskEnricher
- `pkg/controller/drplan/reconciler.go` — Added DiskEnricher field, enrichment in active + passive paths
- `pkg/controller/drplan/reconciler_disk_test.go` — New: 6 reconciler disk enrichment tests
- `cmd/soteria/main.go` — Wired DiskEnricher construction and injection
- `test/integration/controller/suite_test.go` — Added NoOpDiskEnricher to integration test setup
- `config/crd/bases/soteria.io_drplans.yaml` — Auto-generated (make manifests)
