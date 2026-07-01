# Story 15.5: ShadowPV Publisher Controller

Status: ready-for-dev

## Story

As a developer,
I want a controller that watches VolumeReplication/VolumeGroupReplication CRs and publishes the backing PV information to ShadowPV resources,
so that other clusters can discover and create the corresponding PVs.

## Acceptance Criteria

**AC1: Watch VR/VGR CRs**
Given the ShadowPV publisher controller
When a VolumeReplication or VolumeGroupReplication CR is created/updated
And it has a `soteria.io/drplan` label
Then the controller is triggered

**AC2: PV discovery**
Given a VR/VGR CR is observed
When the controller processes it
Then it resolves the dataSource PVC → PV
And reads the full PV spec (capacity, accessModes, CSI volumeHandle, volumeAttributes, etc.)

**AC3: ShadowPV entry creation**
Given a PV is discovered for a VR/VGR
When the controller updates the ShadowPV
Then it creates or updates the ShadowPV named `<plan-name>-<vg-name>`
And adds/updates an entry with `clusterName=<localSite>`, `pvName=<pv.Name>`, `pv=<pv.Spec>`
And sets the `soteria.io/drplan` label on the ShadowPV
And sets the OwnerReference to the DRPlan

**AC4: Idempotent updates**
Given a ShadowPV entry already exists for this cluster+PV
When the publisher runs again (reconcile retry)
Then no update is performed if the PV spec is unchanged
And resourceVersion conflicts are handled via retry

**AC5: VGR multi-PVC handling**
Given a VolumeGroupReplication CR with a PVC selector matching multiple PVCs
When the publisher processes it
Then all backing PVs are added as entries in the same ShadowPV
And each entry has its own `pvName` and `pv` spec

**AC6: PV deletion handling**
Given a VR/VGR CR is deleted (or the backing PV is deleted)
When the publisher detects the deletion
Then the corresponding ShadowPV entry for this cluster is removed
And if no entries remain in the ShadowPV, the entire ShadowPV resource is deleted

## Tasks / Subtasks

- [ ] Task 1: Create ShadowPV publisher controller file (AC: 1, 2, 3, 4, 5)
  - [ ] 1.1: Create `pkg/controller/shadowpv/publisher.go` with `ShadowPVPublisherReconciler` struct (fields: `client.Client`, `Scheme`, `LocalSite string`, `APIReader client.Reader`)
  - [ ] 1.2: Implement `Reconcile` — read VR/VGR, extract `soteria.io/drplan` and `soteria.io/volume-group` labels, resolve PVC→PV chain, build ShadowPV entries
  - [ ] 1.3: Implement `reconcileShadowPV` — create-or-update ShadowPV named `<planName>-<vgName>`, upsert local-site entries, prune stale entries for local site
  - [ ] 1.4: Implement `resolvePVsForVR` — read `spec.dataSource.Name` PVC, follow `pvc.Spec.VolumeName` to PV, return `[]ShadowPVEntry`
  - [ ] 1.5: Implement `resolvePVsForVGR` — list PVCs matching `soteria.io/volume-group=<vgName>` label in VGR namespace, follow each PVC→PV, return `[]ShadowPVEntry`
  - [ ] 1.6: Implement idempotency check — compare existing entries for `clusterName==localSite` with computed entries, skip update if equal

- [ ] Task 2: Implement ShadowPV deletion handling (AC: 6)
  - [ ] 2.1: Add finalizer `soteria.io/shadowpv-publisher` to VR/VGR CRs on first reconcile (only if `soteria.io/drplan` label is present)
  - [ ] 2.2: On VR/VGR deletion (finalizer present): remove local-site entries from the ShadowPV, delete ShadowPV if no entries remain, remove finalizer
  - [ ] 2.3: Handle missing PV/PVC gracefully during deletion — remove entries by stored pvName, not by re-resolving PVC→PV chain

- [ ] Task 3: Implement `SetupWithManager` (AC: 1)
  - [ ] 3.1: Watch VolumeReplication with predicate filtering on `soteria.io/drplan` label presence
  - [ ] 3.2: Watch VolumeGroupReplication with same predicate
  - [ ] 3.3: Add RBAC markers for VR/VGR (get, list, watch, update, patch), PVC (get, list, watch), PV (get, list, watch), ShadowPV (get, list, watch, create, update, patch, delete), DRPlan (get)

- [ ] Task 4: Create `pkg/controller/shadowpv/doc.go` (Tier 1 docs)
  - [ ] 4.1: Package godoc explaining the ShadowPV publisher's purpose, watch triggers, PV discovery model, and ShadowPV lifecycle

- [ ] Task 5: Register in integration test suite (optional — only if envtest supports aggregated API)
  - [ ] 5.1: Add `ShadowPVPublisherReconciler` registration to `test/integration/controller/suite_test.go`
  - [ ] 5.2: Add ShadowPV CRD to envtest CRD list (envtest registers ShadowPV as CRD in etcd, same as DRPlan/DRExecution)

- [ ] Task 6: Write unit tests (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 6.1: Create `pkg/controller/shadowpv/publisher_test.go`
  - [ ] 6.2: Test VR with single PVC → single ShadowPV entry created
  - [ ] 6.3: Test VGR with multiple PVCs → multiple entries in same ShadowPV
  - [ ] 6.4: Test idempotent reconcile — no update when PV spec unchanged
  - [ ] 6.5: Test VR/VGR without `soteria.io/drplan` label → skipped (not reconciled)
  - [ ] 6.6: Test VR/VGR deletion → entry removed from ShadowPV
  - [ ] 6.7: Test last entry removed → ShadowPV deleted
  - [ ] 6.8: Test PVC not bound (no VolumeName) → skip entry, emit warning event
  - [ ] 6.9: Test PV not found → skip entry, emit warning event
  - [ ] 6.10: Test resourceVersion conflict on ShadowPV update → retry succeeds

- [ ] Task 7: Register controller in `cmd/soteria/main.go` (AC: 1)
  - [ ] 7.1: Add `ShadowPVPublisherReconciler` instantiation after existing controller registrations (lines ~362-373)
  - [ ] 7.2: Inject `Client: mgr.GetClient()`, `Scheme: mgr.GetScheme()`, `LocalSite: siteName`, `APIReader: mgr.GetAPIReader()`
  - [ ] 7.3: Call `SetupWithManager(mgr)` with error handling following existing pattern

- [ ] Task 8: Verify and finalize
  - [ ] 8.1: Run `make manifests` — regenerate RBAC from markers
  - [ ] 8.2: Run `make lint-fix` — fix any issues
  - [ ] 8.3: Run `make test` — all unit + envtest tests pass
  - [ ] 8.4: Verify Tier 1/2/3 doc compliance

## Dev Notes

### Discovery Model

The publisher controller does NOT detect whether a PVC is "really" being replicated by Ceph. Instead, it uses a simple heuristic: **if a PVC is selected by a VolumeReplication or VolumeGroupReplication CR that has a `soteria.io/drplan` label, it is assumed to be replicated.** The publisher creates a ShadowPV entry for each backing PV. No additional detection logic is needed.

### Controller Architecture

```
VR/VGR CR (with soteria.io/drplan label)
    │
    ├─ VR: spec.dataSource.Name → PVC → pvc.Spec.VolumeName → PV
    │
    └─ VGR: list PVCs with soteria.io/volume-group=<vgName> label → for each PVC → PV
           │
           ▼
    ShadowPV (named <planName>-<vgName>)
        spec.pvs[]:
          - clusterName: <localSite>
            pvName: <pv.Name>
            pv: <pv.Spec>
```

The publisher runs on **every** site. Each site publishes its own PVs into the shared ShadowPV resource (stored in ScyllaDB, visible cross-site via CDC replication). The consumer controller (Story 15.6) reads entries from remote clusters and creates local PVs.

### Reconciler Struct

```go
type ShadowPVPublisherReconciler struct {
    client.Client
    Scheme    *runtime.Scheme
    LocalSite string
    APIReader client.Reader
}
```

- `Client` — standard controller-runtime cached client for reads/writes
- `LocalSite` — from `--site-name` flag, same as DRPlan/DRExecution controllers. Used as `clusterName` in ShadowPV entries
- `APIReader` — uncached API reader for PV/PVC reads (avoids cache staleness for one-shot lookups during reconcile)

### VR-to-PV Resolution

For a VolumeReplication CR:

```go
func (r *ShadowPVPublisherReconciler) resolvePVsForVR(
    ctx context.Context, vr *replicationv1alpha1.VolumeReplication,
) ([]soteriav1alpha1.ShadowPVEntry, error) {
    pvcName := vr.Spec.DataSource.Name
    var pvc corev1.PersistentVolumeClaim
    if err := r.APIReader.Get(ctx, client.ObjectKey{
        Namespace: vr.Namespace, Name: pvcName,
    }, &pvc); err != nil {
        return nil, fmt.Errorf("getting PVC %s/%s: %w", vr.Namespace, pvcName, err)
    }

    pvName := pvc.Spec.VolumeName
    if pvName == "" {
        return nil, nil // PVC not bound yet
    }

    var pv corev1.PersistentVolume
    if err := r.APIReader.Get(ctx, client.ObjectKey{Name: pvName}, &pv); err != nil {
        return nil, fmt.Errorf("getting PV %s: %w", pvName, err)
    }

    return []soteriav1alpha1.ShadowPVEntry{{
        ClusterName: r.LocalSite,
        PVName:      pv.Name,
        PV:          pv.Spec,
    }}, nil
}
```

### VGR-to-PV Resolution

For a VolumeGroupReplication CR, PVCs are found via the `soteria.io/volume-group` label (set by the CSI Extension driver's `CreateVolumeGroup` in `pkg/drivers/csiextension/driver.go` lines 194-206):

```go
func (r *ShadowPVPublisherReconciler) resolvePVsForVGR(
    ctx context.Context, vgr *replicationv1alpha1.VolumeGroupReplication,
) ([]soteriav1alpha1.ShadowPVEntry, error) {
    vgName := vgr.Labels[csiextension.LabelVolumeGroup]
    if vgName == "" {
        return nil, nil
    }

    var pvcList corev1.PersistentVolumeClaimList
    if err := r.APIReader.List(ctx, &pvcList,
        client.InNamespace(vgr.Namespace),
        client.MatchingLabels{csiextension.LabelVolumeGroup: vgName},
    ); err != nil {
        return nil, fmt.Errorf("listing PVCs for VG %s: %w", vgName, err)
    }

    var entries []soteriav1alpha1.ShadowPVEntry
    for _, pvc := range pvcList.Items {
        if pvc.Spec.VolumeName == "" {
            continue // PVC not bound
        }
        var pv corev1.PersistentVolume
        if err := r.APIReader.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, &pv); err != nil {
            return nil, fmt.Errorf("getting PV %s for PVC %s: %w", pvc.Spec.VolumeName, pvc.Name, err)
        }
        entries = append(entries, soteriav1alpha1.ShadowPVEntry{
            ClusterName: r.LocalSite,
            PVName:      pv.Name,
            PV:          pv.Spec,
        })
    }
    return entries, nil
}
```

### ShadowPV Create-or-Update Pattern

```go
func (r *ShadowPVPublisherReconciler) reconcileShadowPV(
    ctx context.Context, planName, vgName string,
    entries []soteriav1alpha1.ShadowPVEntry,
) error {
    shadowPVName := planName + "-" + vgName
    logger := log.FromContext(ctx).WithValues("shadowpv", shadowPVName)

    var spv soteriav1alpha1.ShadowPV
    err := r.Get(ctx, client.ObjectKey{Name: shadowPVName}, &spv)
    if client.IgnoreNotFound(err) != nil {
        return fmt.Errorf("getting ShadowPV %s: %w", shadowPVName, err)
    }

    if errors.IsNotFound(err) {
        if len(entries) == 0 {
            return nil
        }
        spv = soteriav1alpha1.ShadowPV{
            ObjectMeta: metav1.ObjectMeta{
                Name: shadowPVName,
                Labels: map[string]string{
                    drivers.LabelDRPlan: planName,
                },
            },
            Spec: soteriav1alpha1.ShadowPVSpec{
                PVs: entries,
            },
        }
        if err := r.Create(ctx, &spv); err != nil {
            return fmt.Errorf("creating ShadowPV %s: %w", shadowPVName, err)
        }
        logger.Info("Created ShadowPV", "entryCount", len(entries))
        return nil
    }

    // Merge: keep remote entries, replace local entries
    merged := mergeEntries(spv.Spec.PVs, entries, r.LocalSite)

    if len(merged) == 0 {
        if err := r.Delete(ctx, &spv); err != nil {
            return client.IgnoreNotFound(fmt.Errorf("deleting empty ShadowPV %s: %w", shadowPVName, err))
        }
        logger.Info("Deleted ShadowPV (no entries remain)")
        return nil
    }

    if entriesEqual(spv.Spec.PVs, merged) {
        return nil // Idempotent — no change
    }

    patch := client.MergeFrom(spv.DeepCopy())
    spv.Spec.PVs = merged
    if err := r.Patch(ctx, &spv, patch); err != nil {
        return fmt.Errorf("patching ShadowPV %s: %w", shadowPVName, err)
    }
    logger.Info("Updated ShadowPV entries", "entryCount", len(merged))
    return nil
}

func mergeEntries(
    existing []soteriav1alpha1.ShadowPVEntry,
    localEntries []soteriav1alpha1.ShadowPVEntry,
    localSite string,
) []soteriav1alpha1.ShadowPVEntry {
    var merged []soteriav1alpha1.ShadowPVEntry
    for _, e := range existing {
        if e.ClusterName != localSite {
            merged = append(merged, e)
        }
    }
    merged = append(merged, localEntries...)
    return merged
}
```

**Use `client.MergeFrom` strategic merge patch** for ShadowPV updates — per project context rule (no `client.Update` for multi-controller environments, reduces resourceVersion conflict surface).

### Idempotency Check

```go
func entriesEqual(a, b []soteriav1alpha1.ShadowPVEntry) bool {
    if len(a) != len(b) {
        return false
    }
    am := make(map[string]soteriav1alpha1.ShadowPVEntry, len(a))
    for _, e := range a {
        am[e.ClusterName+"/"+e.PVName] = e
    }
    for _, e := range b {
        existing, ok := am[e.ClusterName+"/"+e.PVName]
        if !ok {
            return false
        }
        if !apiequality.Semantic.DeepEqual(existing.PV, e.PV) {
            return false
        }
    }
    return true
}
```

Use `apiequality.Semantic.DeepEqual` (from `k8s.io/apimachinery/pkg/api/equality`) for PV spec comparison. This correctly handles nil vs empty slices/maps and provides Kubernetes-aware structural equality.

### Finalizer Pattern for Deletion Handling

The publisher adds a finalizer to VR/VGR CRs it processes, so it can clean up ShadowPV entries on VR/VGR deletion. Without a finalizer, deleted VR/VGR CRs disappear before the controller can read which PVs to remove.

```go
const publisherFinalizer = "soteria.io/shadowpv-publisher"
```

**Only add the finalizer if the VR/VGR has a `soteria.io/drplan` label.** VR/VGRs without the label are not managed by this controller.

On deletion (VR/VGR has `DeletionTimestamp` set):
1. Read `soteria.io/drplan` and `soteria.io/volume-group` labels (still available on the object)
2. Get the ShadowPV by name `<planName>-<vgName>`
3. Remove all entries where `clusterName == localSite` AND `pvName` matches entries previously published
4. If no entries remain → delete the ShadowPV
5. Remove the finalizer from the VR/VGR

**Graceful degradation:** If the ShadowPV is already deleted (e.g., DRPlan deletion cascade), skip step 3-4 and just remove the finalizer.

### SetupWithManager

```go
func (r *ShadowPVPublisherReconciler) SetupWithManager(mgr ctrl.Manager) error {
    hasDRPlanLabel := predicate.NewPredicateFuncs(func(obj client.Object) bool {
        return obj.GetLabels()[drivers.LabelDRPlan] != ""
    })

    return ctrl.NewControllerManagedBy(mgr).
        Named("shadowpv-publisher").
        Watches(
            &replicationv1alpha1.VolumeReplication{},
            &handler.EnqueueRequestForObject{},
            builder.WithPredicates(hasDRPlanLabel),
        ).
        Watches(
            &replicationv1alpha1.VolumeGroupReplication{},
            &handler.EnqueueRequestForObject{},
            builder.WithPredicates(hasDRPlanLabel),
        ).
        Complete(r)
}
```

**No `.For()` call** — this controller watches two external resource types (VR and VGR), neither of which is the primary type. Using `.Watches()` with `EnqueueRequestForObject` enqueues the VR/VGR's own `NamespacedName` as the reconcile request.

**Named controller:** Use `.Named("shadowpv-publisher")` to avoid name conflicts since neither VR nor VGR is the "primary" resource (no `.For()` call to derive a name).

### RBAC Markers

```go
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumereplications,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumegroupreplications,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=shadowpvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get
```

- VR/VGR need `update;patch` for adding/removing the publisher finalizer
- PVC/PV are read-only (discovery chain)
- ShadowPV needs full CRUD (create, read, update, delete)
- DRPlan needs `get` only — the OwnerReference is set by ShadowPV's `PrepareForCreate` strategy (Story 15.4), not by this controller

### Reconcile Flow

```
1. Get VR or VGR by NamespacedName
2. If not found → return (already deleted, finalizer cleaned up or race)
3. Read labels: planName = soteria.io/drplan, vgName = soteria.io/volume-group
4. If planName or vgName is empty → return (not a managed VR/VGR)
5. If DeletionTimestamp is set:
   a. Get ShadowPV "<planName>-<vgName>"
   b. Remove entries for localSite
   c. If empty → delete ShadowPV; else → patch
   d. Remove finalizer from VR/VGR
   e. Return
6. Add finalizer if not present
7. Determine type (VR or VGR) from TypeMeta or object type assertion
8. Resolve PVs:
   - VR: spec.dataSource.Name → PVC → PV → one entry
   - VGR: list PVCs by volume-group label → each PVC → PV → multiple entries
9. Call reconcileShadowPV(planName, vgName, entries)
10. Return
```

### Determining VR vs VGR

The controller watches both types via `.Watches()`. Since `Reconcile` receives a generic `ctrl.Request` (just a NamespacedName), the controller must determine which type it is reconciling. The approach:

```go
func (r *ShadowPVPublisherReconciler) Reconcile(
    ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    // Try VR first
    var vr replicationv1alpha1.VolumeReplication
    vrErr := r.Get(ctx, req.NamespacedName, &vr)
    if vrErr == nil {
        return r.reconcileVR(ctx, &vr)
    }

    // Try VGR
    var vgr replicationv1alpha1.VolumeGroupReplication
    vgrErr := r.Get(ctx, req.NamespacedName, &vgr)
    if vgrErr == nil {
        return r.reconcileVGR(ctx, &vgr)
    }

    // Both not found — already deleted
    if errors.IsNotFound(vrErr) && errors.IsNotFound(vgrErr) {
        return ctrl.Result{}, nil
    }

    logger.Error(vrErr, "Could not get VR or VGR", "name", req.NamespacedName)
    return ctrl.Result{}, vrErr
}
```

**Note on name collision:** VR and VGR names are derived from different patterns in the CSI extension driver. VR names use `<vgID>-<pvcName>` format, VGR names use `<vgID>` format. They won't collide in the same namespace.

### Retry on ResourceVersion Conflict

Wrap the `reconcileShadowPV` call with `retry.RetryOnConflict` using `engine.ScyllaRetry` (per project context rule — ShadowPV is stored in ScyllaDB, so use 200ms base/2.0 factor/0.3 jitter/8 steps):

```go
import "github.com/soteria-project/soteria/pkg/engine"

err := retry.RetryOnConflict(engine.ScyllaRetry, func() error {
    return r.reconcileShadowPV(ctx, planName, vgName, entries)
})
```

### File Inventory

| File | Action | Lines Changed (est.) |
|------|--------|---------------------|
| `pkg/controller/shadowpv/publisher.go` | NEW — ShadowPVPublisherReconciler, Reconcile, resolvePVs, reconcileShadowPV, SetupWithManager | ~300 |
| `pkg/controller/shadowpv/doc.go` | NEW — package documentation | ~15 |
| `pkg/controller/shadowpv/publisher_test.go` | NEW — unit tests with envtest | ~350 |
| `cmd/soteria/main.go` | MODIFY — register ShadowPVPublisherReconciler with LocalSite injection | ~12 |
| `test/integration/controller/suite_test.go` | MODIFY — register ShadowPVPublisherReconciler, add ShadowPV CRD | ~15 |

**Total: ~3 new files, ~2 modified files, ~700 lines**

### Key Constraints

- **Do NOT access ScyllaDB directly** — ShadowPV is an aggregated API resource. The controller-runtime client goes through the kube-apiserver proxy to the aggregated API server. The `pkg/storage/scylladb/` boundary is absolute (per project context)
- **Do NOT create the ShadowPV OwnerReference in this controller** — the OwnerReference to DRPlan is set by `PrepareForCreate` in the ShadowPV registry strategy (Story 15.4). The publisher only needs to set the `soteria.io/drplan` label; the strategy uses that label to look up the DRPlan UID
- **Do NOT watch PVs or PVCs** — the publisher is triggered by VR/VGR changes only. PVs and PVCs are read on-demand during reconcile. If a PV changes independently, the consumer controller (Story 15.6) handles discrepancies
- **Do NOT modify VR/VGR spec or status** — the publisher only reads VR/VGR metadata and spec, and manages its finalizer. VR/VGR spec/status are owned by the CSI extension driver and the CSI Addons sidecar
- **Do NOT implement pool-ID rewrite** — that's the consumer controller's responsibility (Story 15.6). The publisher stores the raw PV spec as-is from the source cluster
- **Do NOT implement error condition bubbling** — ShadowPV status conditions are for the consumer controller (Story 15.6). Error bubbling to DRPlan is deferred to a future epic
- **Use `engine.ScyllaRetry` for conflict retries** — ShadowPV is stored in ScyllaDB, `retry.DefaultRetry` (10ms/5 steps) is too fast for cross-DC propagation
- **Use `client.MergeFrom` strategic merge patch** — reduces conflict surface when publisher and consumer update different parts of the same ShadowPV
- **`LabelVolumeGroup` is in `pkg/drivers/csiextension/constants.go`** — import from `csiextension` package, NOT from `drivers` (it's CSI-specific, not part of the generic driver interface)
- **The publisher adds a finalizer to VR/VGR** — this requires `update;patch` RBAC on VR/VGR resources, not just `get;list;watch`
- **ShadowPV is cluster-scoped** — use `client.ObjectKey{Name: shadowPVName}` (no namespace) for Get/Create/Patch/Delete
- **ShadowPV PVs list is `+listType=atomic`** — the full list is replaced on each update. Do not attempt strategic merge patch on individual entries

### Naming Convention

ShadowPV resources follow the naming convention established in Story 15.4:
- Name: `<plan-name>-<vg-name>` (e.g., `erp-full-stack-vm-billing`, `erp-full-stack-ns-accounting`)
- Label: `soteria.io/drplan=<plan-name>` (e.g., `soteria.io/drplan=erp-full-stack`)

Volume group names follow the CSI extension driver convention:
- VM-level: `vm-<namespace>-<vmName>` (e.g., `vm-default-web01`)
- Namespace-level: `ns-<namespace>` (e.g., `ns-erp-database`)

### Label Constants

| Constant | Package | Value |
|----------|---------|-------|
| `LabelDRPlan` | `pkg/drivers` | `soteria.io/drplan` |
| `LabelVolumeGroup` | `pkg/drivers/csiextension` | `soteria.io/volume-group` |

Import `drivers.LabelDRPlan` for the ShadowPV label. Import `csiextension.LabelVolumeGroup` for VR/VGR volume group resolution.

### Testing Strategy

**Unit tests (`publisher_test.go`) use envtest** — real etcd + API server. Tests need VR/VGR CRDs, PVC, PV, and ShadowPV CRD registered. Since ShadowPV is normally an aggregated API resource, register it as a CRD in envtest (same approach used for DRPlan/DRExecution in `test/integration/controller/suite_test.go`).

| Test | Setup | Expected |
|------|-------|----------|
| `TestPublisher_VR_CreatesShadowPV` | VR with drplan label + PVC bound to PV | ShadowPV created with 1 entry (localSite, pvName, pvSpec) |
| `TestPublisher_VGR_MultiplePVCs` | VGR with 3 PVCs labeled with volume-group | ShadowPV created with 3 entries |
| `TestPublisher_Idempotent_NoUpdate` | ShadowPV already exists with correct entries | No patch, resourceVersion unchanged |
| `TestPublisher_PVSpecChanged_Updates` | PV spec changed since last publish | ShadowPV patched with updated entry |
| `TestPublisher_NoDRPlanLabel_Skipped` | VR without soteria.io/drplan label | No ShadowPV created |
| `TestPublisher_VR_Deleted_EntryRemoved` | VR with finalizer deleted | Entry removed from ShadowPV, finalizer removed |
| `TestPublisher_LastEntry_ShadowPVDeleted` | VR deleted, was last entry in ShadowPV | ShadowPV deleted |
| `TestPublisher_PVCNotBound_Skipped` | PVC with empty VolumeName | No entry added, warning event |
| `TestPublisher_PVNotFound_Warning` | PVC references non-existent PV | No entry added, warning event |
| `TestPublisher_MultiSite_PreservesRemoteEntries` | ShadowPV has entries from remote site | Remote entries preserved, local entries replaced |
| `TestPublisher_Finalizer_Added` | VR with drplan label, no finalizer | Finalizer added on first reconcile |
| `TestPublisher_ShadowPV_AlreadyDeleted` | VR deleted, ShadowPV already gone (cascade) | Finalizer removed, no error |

### Previous Story Intelligence

**Story 15.4 (ShadowPV CRD):**
- `pkg/apis/soteria.io/v1alpha1/types_shadowpv.go` — ShadowPV, ShadowPVList, ShadowPVSpec, ShadowPVStatus, ShadowPVEntry types
- `pkg/registry/shadowpv/strategy.go` — PrepareForCreate sets OwnerReference via `soteria.io/drplan` label + planGetter
- `pkg/registry/shadowpv/storage.go` — NewREST with StatusREST
- Validation: non-empty clusterName/pvName, no duplicate (clusterName, pvName) pairs, at least one entry on create
- **`+listType=atomic` on PVs** — the PVs list is replaced atomically on each update

**Story 15.1-15.3 (Epic 15 context):**
- Stories 15.1-15.3 modified the engine layer and driver framework — no controller or registry changes
- CSI Extension driver's `CreateVolumeGroup` labels PVCs with `soteria.io/volume-group` and VR/VGR CRs with `soteria.io/drplan` — these labels are the publisher's trigger
- VR CRs have `spec.dataSource = corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: pvcName}` — this is the PVC reference to follow

**Story 13.4 (DRPlan VR/VGR watches):**
- `vrStatusChangePredicate()` and `vrEventHandler()` in `pkg/controller/drplan/reconciler.go` — pattern for watching VR/VGR
- The publisher uses a different predicate (label presence, not status changes) and different handler (enqueue self, not map to plan)

### Git Intelligence

Recent commits were Epic 14 infrastructure work (shell scripts, Kustomize overlays). No Go code changes since Epic 13. The controller framework and driver patterns are stable. No API changes or dependency updates affect this story.

### Downstream Impact on Story 15.6

Story 15.6 (Consumer Controller):
- Watches ShadowPV resources created by this publisher
- Filters entries where `clusterName != localSite`
- Creates local PVs from remote entries with pool-ID rewrite
- Records conflicts in ShadowPV `status.conditions`

The publisher must ensure:
- ShadowPV entries contain complete `corev1.PersistentVolumeSpec` (capacity, accessModes, CSI volumeHandle, volumeAttributes)
- The `pvName` field accurately reflects the PV name to create on remote clusters
- The `soteria.io/drplan` label is always set for label-based filtering

### Project Structure Notes

- `pkg/controller/shadowpv/` — new package, follows existing `pkg/controller/drplan/` and `pkg/controller/drexecution/` structure
- The controller imports from `pkg/apis/soteria.io/v1alpha1` (ShadowPV types), `pkg/drivers` (LabelDRPlan), and `pkg/drivers/csiextension` (LabelVolumeGroup)
- No new `internal/` packages — the publisher is a straightforward controller with no shared utilities
- No `config/crd/bases/` changes — ShadowPV is an aggregated API, not a standalone CRD
- No `make manifests generate` needed beyond RBAC — no type changes in this story

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.5 — acceptance criteria and technical notes]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.4 — ShadowPV type definition and naming convention]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.6 — downstream consumer requirements]
- [Source: _bmad-output/implementation-artifacts/15-4-shadowpv-crd-scylladb-storage.md — ShadowPV type definitions, validation, strategy, storage]
- [Source: pkg/drivers/csiextension/driver.go — CreateVolumeGroup PVC labeling (line 194-206), VR spec.dataSource (line 166-169)]
- [Source: pkg/drivers/csiextension/constants.go — LabelVolumeGroup, LabelDRPlan re-exports]
- [Source: pkg/drivers/types.go — LabelDRPlan, VolumeGroupSpec, VolumeGroupID]
- [Source: pkg/controller/drplan/reconciler.go — SetupWithManager VR/VGR watch pattern (lines 1289-1313), vrStatusChangePredicate, vrEventHandler]
- [Source: pkg/controller/drexecution/reconciler.go — LocalSite field (line 87-91), SetupWithManager VM watch (lines 1561-1575)]
- [Source: pkg/controller/volumereplication/reconciler.go — SetupVolumeReplicationController pattern (lines 223-233)]
- [Source: test/integration/controller/suite_test.go — controller registration, envtest setup, scheme registration]
- [Source: cmd/soteria/main.go — controller registration pattern (lines 285-298, 347-373), --site-name flag (line 93, 129-131)]
- [Source: pkg/engine — ScyllaRetry retry policy for ScyllaDB-backed resources]
- [Source: _bmad-output/project-context.md — aggregated API server architecture, ScyllaDB storage boundary, MergeFrom patch rule, testing rules]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
