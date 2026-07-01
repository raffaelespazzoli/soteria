# Story 15.6: ShadowPV Consumer Controller (PV Creation with Pool-ID Rewrite)

Status: ready-for-dev

## Story

As a developer,
I want a controller that watches ShadowPV resources and creates local PVs for entries from remote clusters,
so that mirrored Ceph RBD images have corresponding PVs on the target cluster ready for PVC binding.

## Acceptance Criteria

**AC1: Watch ShadowPV resources**
Given the ShadowPV consumer controller
When a ShadowPV is created/updated
Then the controller is triggered

**AC2: Remote entry detection**
Given a ShadowPV with entries from multiple clusters
When the consumer processes it
Then it identifies entries where `clusterName != localSite`
And processes only those remote entries

**AC3: PV creation from remote entry**
Given a remote ShadowPV entry
When no local PV with the same name exists
Then a PV is created with the spec from the ShadowPV entry
And the PV's `spec.csi.volumeHandle` has its pool-ID segment rewritten for the local Ceph cluster

**AC4: Pool-ID rewrite for Ceph volume handles**
Given a CSI volume handle in format `<ver>-<clusterID-len-hex>-<clusterID>-<poolID-hex-16>-<uuid>`
When the consumer creates the local PV
Then the `<poolID-hex-16>` segment is replaced with the local Ceph pool's ID (in 16-char hex)
And the local pool ID is resolved from the CephBlockPool CR's `.status.poolID` field

**AC5: PV already exists (idempotent and conflict handling)**
Given a PV already exists locally with the same name
When the consumer runs
And the existing PV was created by the ShadowPV controller (has expected label)
Then no update or recreation is attempted and no error is raised
When the existing PV was NOT created by the ShadowPV controller (conflict)
Then the controller emits a warning event on the ShadowPV resource
And records the conflict in the ShadowPV status (`conditions[PVConflict]=True`)
And does not overwrite or delete the conflicting PV

**AC6: Non-Ceph volume handles**
Given a ShadowPV entry with a volume handle that does not match the Ceph format
When the consumer processes it
Then the PV is created with the volume handle as-is (no rewrite)
And a warning event is emitted indicating no pool-ID rewrite was performed

**AC7: Local pool ID resolution via CephBlockPool**
Given the consumer needs the local Ceph pool ID
When it resolves the pool
Then it reads the CephBlockPool CR's `.status.poolID` field (canonical source)
And the pool name is derived from the PV's CSI volumeAttributes `pool` field
And the resolution is cached for the lifetime of the reconcile
And the controller has RBAC for `ceph.rook.io` CephBlockPool read access

## Tasks / Subtasks

- [ ] Task 1: Create volume handle parser (AC: 4, 6)
  - [ ] 1.1: Create `pkg/drivers/csiextension/volumehandle.go` with `ParseVolumeHandle` and `RewritePoolID` functions
  - [ ] 1.2: Implement `ParseVolumeHandle` — parse format `<ver>-<clusterIDLen>-<clusterID>-<poolIDHex16>-<uuid>`, return structured `VolumeHandle` or error
  - [ ] 1.3: Implement `RewritePoolID` — given a volume handle string and new pool ID (int), return rewritten handle string
  - [ ] 1.4: Handle non-Ceph format gracefully — `ParseVolumeHandle` returns error for unrecognized formats
  - [ ] 1.5: Create `pkg/drivers/csiextension/volumehandle_test.go` — table-driven tests for parse, rewrite, and non-Ceph handles

- [ ] Task 2: Create ShadowPV consumer controller file (AC: 1, 2, 3, 5)
  - [ ] 2.1: Create `pkg/controller/shadowpv/consumer.go` with `ShadowPVConsumerReconciler` struct (fields: `client.Client`, `Scheme`, `LocalSite string`, `APIReader client.Reader`, `EventRecorder record.EventRecorder`)
  - [ ] 2.2: Implement `Reconcile` — get ShadowPV, filter entries where `clusterName != localSite`, for each remote entry call `reconcilePV`
  - [ ] 2.3: Implement `reconcilePV` — check if PV exists, if not create with pool-ID rewrite, if exists check ownership
  - [ ] 2.4: Implement ownership detection — check for `soteria.io/shadowpv-consumer` label on existing PV
  - [ ] 2.5: Implement conflict handling — emit event on ShadowPV, set `PVConflict` condition on ShadowPV status

- [ ] Task 3: Implement pool-ID rewrite logic (AC: 4, 6, 7)
  - [ ] 3.1: Implement `resolveLocalPoolID` — read CephBlockPool CR by name from `rook-ceph` namespace, return `.status.poolID`
  - [ ] 3.2: Implement `poolNameFromPV` — extract pool name from `spec.csi.volumeAttributes["pool"]`
  - [ ] 3.3: Implement `rewriteVolumeHandle` — parse handle, rewrite pool-ID segment, return modified PV spec
  - [ ] 3.4: Handle non-Ceph handles — skip rewrite, emit warning event, create PV as-is

- [ ] Task 4: Implement `SetupWithManager` (AC: 1)
  - [ ] 4.1: Watch ShadowPV with `For(&soteriav1alpha1.ShadowPV{})`
  - [ ] 4.2: Use `.Named("shadowpv-consumer")` for controller name
  - [ ] 4.3: Add RBAC markers for ShadowPV (get, list, watch, update, patch), ShadowPV/status (get, update, patch), PV (get, list, watch, create), CephBlockPool (get, list, watch), events (create, patch)

- [ ] Task 5: Create `pkg/controller/shadowpv/consumer_doc.go` or extend existing `doc.go` (Tier 1 docs)
  - [ ] 5.1: Document consumer controller purpose, pool-ID rewrite model, and relationship to publisher

- [ ] Task 6: Write unit tests (AC: 1, 2, 3, 4, 5, 6, 7)
  - [ ] 6.1: Create `pkg/controller/shadowpv/consumer_test.go`
  - [ ] 6.2: Test ShadowPV with remote entry → PV created with rewritten pool-ID
  - [ ] 6.3: Test ShadowPV with local-only entries → no PV created
  - [ ] 6.4: Test PV already exists with consumer label → no-op (idempotent)
  - [ ] 6.5: Test PV already exists without consumer label → PVConflict condition set, event emitted
  - [ ] 6.6: Test non-Ceph volume handle → PV created as-is, warning event
  - [ ] 6.7: Test CephBlockPool not found → error returned, requeue
  - [ ] 6.8: Test multiple remote entries → multiple PVs created
  - [ ] 6.9: Test pool-ID correctly rewritten (16-char hex, different source/target pool IDs)
  - [ ] 6.10: Test ShadowPV entry removed → PV remains (GC is NOT the consumer's job)

- [ ] Task 7: Register controller in `cmd/soteria/main.go` (AC: 1)
  - [ ] 7.1: Add `ShadowPVConsumerReconciler` instantiation after existing controller registrations
  - [ ] 7.2: Inject `Client`, `Scheme`, `LocalSite: siteName`, `APIReader`, `EventRecorder`
  - [ ] 7.3: Call `SetupWithManager(mgr)` with error handling

- [ ] Task 8: Verify and finalize
  - [ ] 8.1: Run `make manifests` — regenerate RBAC from markers
  - [ ] 8.2: Run `make lint-fix` — fix any issues
  - [ ] 8.3: Run `make test` — all unit + envtest tests pass
  - [ ] 8.4: Verify Tier 1/2/3 doc compliance

## Dev Notes

### Controller Architecture

```
ShadowPV (cluster-scoped, stored in ScyllaDB)
    │
    ├─ spec.pvs[]:
    │     - clusterName: "east"  (remote → consumer creates PV)
    │     - clusterName: "west"  (local → skip)
    │
    ▼
Consumer (runs on "west"):
    1. Filter: entries where clusterName != "west"
    2. For each remote entry:
       a. Parse volume handle → extract pool-ID segment
       b. Resolve local CephBlockPool → get .status.poolID
       c. Rewrite pool-ID segment → new volume handle
       d. Create PV with rewritten spec + consumer label
    3. If PV exists with consumer label → skip (idempotent)
    4. If PV exists without consumer label → conflict → event + condition
```

### Reconciler Struct

```go
type ShadowPVConsumerReconciler struct {
    client.Client
    Scheme        *runtime.Scheme
    LocalSite     string
    APIReader     client.Reader
    EventRecorder record.EventRecorder
}
```

- `Client` — standard controller-runtime cached client for reads/writes
- `LocalSite` — from `--site-name` flag; entries matching this site are skipped (they are "ours")
- `APIReader` — uncached API reader for PV existence checks (avoids cache lag for freshly-created PVs)
- `EventRecorder` — for emitting warning events on ShadowPV for conflicts and non-Ceph handles

### Volume Handle Format and Parsing

Rook-Ceph CSI volume handle format:
```
0001-0009-rook-ceph-<poolIDHex16>-<imageUUID>
│    │    │         │              └─ RBD image UUID (36 chars, with dashes)
│    │    │         └─ Pool ID as 16-char zero-padded hex
│    │    └─ Cluster ID (length specified by field 2)
│    └─ Cluster ID length as 4-char hex (0009 = 9 bytes for "rook-ceph")
└─ Version field (always "0001" for current Rook)
```

Reference implementation from `hack/multisite/setup-rook-ceph.sh` line 656:
```bash
sed -E "s/^(0001-0009-rook-ceph-)[0-9a-f]{16}(-.*)/\1${west_pool_hex}\2/"
```

### Volume Handle Parser (`pkg/drivers/csiextension/volumehandle.go`)

```go
// VolumeHandle represents a parsed Rook-Ceph CSI volume handle.
type VolumeHandle struct {
    Version    string // "0001"
    ClusterID  string // "rook-ceph" (variable length)
    PoolIDHex  string // 16-char zero-padded hex pool ID
    ImageUUID  string // RBD image UUID
}

// ParseVolumeHandle splits a Rook-Ceph CSI volume handle into its components.
// Returns an error for handles that don't match the expected format.
func ParseVolumeHandle(handle string) (VolumeHandle, error) {
    // Format: <ver>-<clusterIDLenHex4>-<clusterID>-<poolIDHex16>-<rest>
    // Example: 0001-0009-rook-ceph-0000000000000001-7f3da9a2-...
    parts := strings.SplitN(handle, "-", 3)
    if len(parts) < 3 {
        return VolumeHandle{}, fmt.Errorf("invalid volume handle format: %s", handle)
    }

    version := parts[0]
    clusterIDLenHex := parts[1]

    clusterIDLen, err := strconv.ParseInt(clusterIDLenHex, 16, 32)
    if err != nil {
        return VolumeHandle{}, fmt.Errorf("parsing cluster ID length %q: %w", clusterIDLenHex, err)
    }

    remainder := parts[2]
    // clusterID is the next clusterIDLen bytes (which may contain dashes)
    if int(clusterIDLen) > len(remainder) {
        return VolumeHandle{}, fmt.Errorf("cluster ID length %d exceeds remainder %q", clusterIDLen, remainder)
    }
    clusterID := remainder[:clusterIDLen]
    afterCluster := remainder[clusterIDLen:]

    // After cluster ID: -<poolIDHex16>-<uuid>
    if len(afterCluster) < 18 || afterCluster[0] != '-' {
        return VolumeHandle{}, fmt.Errorf("missing pool-ID segment after cluster ID in %q", handle)
    }
    afterCluster = afterCluster[1:] // strip leading dash

    if len(afterCluster) < 17 {
        return VolumeHandle{}, fmt.Errorf("pool-ID segment too short in %q", handle)
    }
    poolIDHex := afterCluster[:16]
    if afterCluster[16] != '-' {
        return VolumeHandle{}, fmt.Errorf("expected dash after pool-ID in %q", handle)
    }
    imageUUID := afterCluster[17:]

    return VolumeHandle{
        Version:   version,
        ClusterID: clusterID,
        PoolIDHex: poolIDHex,
        ImageUUID: imageUUID,
    }, nil
}

// RewritePoolID replaces the pool-ID segment in a volume handle with the given pool ID.
func RewritePoolID(handle string, newPoolID int) (string, error) {
    vh, err := ParseVolumeHandle(handle)
    if err != nil {
        return "", err
    }
    newPoolHex := fmt.Sprintf("%016x", newPoolID)
    clusterIDLenHex := fmt.Sprintf("%04x", len(vh.ClusterID))
    return fmt.Sprintf("%s-%s-%s-%s-%s",
        vh.Version, clusterIDLenHex, vh.ClusterID, newPoolHex, vh.ImageUUID), nil
}
```

### CephBlockPool Pool-ID Resolution

The **canonical source** for pool ID is `CephBlockPool.status.poolID` (not a toolbox query). The pool name comes from the PV's `spec.csi.volumeAttributes["pool"]` field.

```go
import (
    cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
)

func (r *ShadowPVConsumerReconciler) resolveLocalPoolID(
    ctx context.Context, poolName string,
) (int, error) {
    var cbp cephv1.CephBlockPool
    if err := r.APIReader.Get(ctx, client.ObjectKey{
        Namespace: "rook-ceph",
        Name:      poolName,
    }, &cbp); err != nil {
        return 0, fmt.Errorf("getting CephBlockPool %s: %w", poolName, err)
    }

    if cbp.Status.PoolID == 0 {
        return 0, fmt.Errorf("CephBlockPool %s has no poolID in status", poolName)
    }

    return cbp.Status.PoolID, nil
}
```

**Pool name derivation:** Extract from `pvSpec.CSI.VolumeAttributes["pool"]`. Example: the PV has `volumeAttributes: {pool: "mirrored-pool"}` → look up `CephBlockPool` named `mirrored-pool` in `rook-ceph` namespace.

### PV Creation

```go
const consumerLabel = "soteria.io/shadowpv-consumer"

func (r *ShadowPVConsumerReconciler) reconcilePV(
    ctx context.Context, spv *soteriav1alpha1.ShadowPV,
    entry soteriav1alpha1.ShadowPVEntry,
) error {
    logger := log.FromContext(ctx).WithValues("pv", entry.PVName)

    // Check if PV already exists
    var existingPV corev1.PersistentVolume
    err := r.APIReader.Get(ctx, client.ObjectKey{Name: entry.PVName}, &existingPV)
    if err == nil {
        // PV exists — check ownership
        if existingPV.Labels[consumerLabel] == spv.Name {
            return nil // Idempotent — we created it
        }
        // Conflict — PV exists but not created by us
        r.EventRecorder.Eventf(spv, corev1.EventTypeWarning, "PVConflict",
            "PV %s exists but was not created by ShadowPV consumer (missing label %s=%s)",
            entry.PVName, consumerLabel, spv.Name)
        return r.setConflictCondition(ctx, spv, entry.PVName)
    }
    if !apierrors.IsNotFound(err) {
        return fmt.Errorf("checking PV %s existence: %w", entry.PVName, err)
    }

    // PV does not exist — create with pool-ID rewrite
    pvSpec := entry.PV.DeepCopy()
    if pvSpec.CSI != nil && pvSpec.CSI.VolumeHandle != "" {
        rewritten, rewriteErr := r.rewriteVolumeHandle(ctx, spv, pvSpec.CSI.VolumeHandle, pvSpec.CSI.VolumeAttributes)
        if rewriteErr != nil {
            // Non-Ceph handle or pool resolution failure — create as-is with warning
            r.EventRecorder.Eventf(spv, corev1.EventTypeWarning, "PoolIDRewriteSkipped",
                "Could not rewrite pool-ID for PV %s: %v — creating with original handle",
                entry.PVName, rewriteErr)
        } else {
            pvSpec.CSI.VolumeHandle = rewritten
        }
    }

    pv := &corev1.PersistentVolume{
        ObjectMeta: metav1.ObjectMeta{
            Name: entry.PVName,
            Labels: map[string]string{
                consumerLabel:   spv.Name,
                drivers.LabelDRPlan: spv.Labels[drivers.LabelDRPlan],
            },
        },
        Spec: *pvSpec,
    }
    if err := r.Create(ctx, pv); err != nil {
        if apierrors.IsAlreadyExists(err) {
            return nil // Race condition — another reconcile created it
        }
        return fmt.Errorf("creating PV %s: %w", entry.PVName, err)
    }

    logger.Info("Created PV from ShadowPV entry",
        "shadowpv", spv.Name, "sourceCluster", entry.ClusterName)
    return nil
}
```

### Conflict Condition on ShadowPV Status

```go
func (r *ShadowPVConsumerReconciler) setConflictCondition(
    ctx context.Context, spv *soteriav1alpha1.ShadowPV, pvName string,
) error {
    patch := client.MergeFrom(spv.DeepCopy())
    meta.SetStatusCondition(&spv.Status.Conditions, metav1.Condition{
        Type:               "PVConflict",
        Status:             metav1.ConditionTrue,
        Reason:             "ExistingPVNotOwnedByConsumer",
        Message:            fmt.Sprintf("PV %s exists but was not created by the ShadowPV consumer controller", pvName),
        ObservedGeneration: spv.Generation,
    })
    return r.Status().Patch(ctx, spv, patch)
}
```

**Use `r.Status().Patch()` with `client.MergeFrom`** — ShadowPV status is updated via the `/status` subresource (Story 15.4 registered `shadowpvs/status`). Strategic merge patch reduces conflict with publisher updating spec.

### SetupWithManager

```go
func (r *ShadowPVConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        Named("shadowpv-consumer").
        For(&soteriav1alpha1.ShadowPV{}).
        Complete(r)
}
```

**Uses `.For()`** — unlike the publisher (which watches two external types), the consumer watches a single primary type (ShadowPV). The ShadowPV is the owned resource type, so `.For()` is appropriate and provides the controller name automatically. `.Named()` overrides for clarity.

### RBAC Markers

```go
// +kubebuilder:rbac:groups=soteria.io,resources=shadowpvs,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=shadowpvs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
```

- ShadowPV: read-only for spec (publisher owns spec), write for status (consumer sets conditions)
- PV: read + create only (consumer never updates or deletes PVs it created — PV lifecycle is Kubernetes-native)
- CephBlockPool: read-only (pool-ID resolution)
- Events: emit warnings for conflicts and non-Ceph handles

### Reconcile Flow

```
1. Get ShadowPV by name (from ctrl.Request)
2. If not found → return (deleted)
3. Filter entries: find all where entry.ClusterName != r.LocalSite
4. For each remote entry:
   a. Check if PV already exists (APIReader.Get)
   b. If exists with consumer label → skip (idempotent)
   c. If exists without consumer label → setConflictCondition + event
   d. If not exists → resolve pool-ID, rewrite handle, create PV
5. Return success
```

### Pool-ID Rewrite Details

The volume handle format for Rook-Ceph RBD CSI:
```
0001-0009-rook-ceph-0000000000000001-7f3da9a2-abcd-1234-ef56-789012345678
│    │    │         │                └─ Image UUID (RBD image)
│    │    │         └─ Pool ID = 1 (as 16-char hex)
│    │    └─ Cluster ID = "rook-ceph" (9 chars)
│    └─ Cluster ID length = 9 (as 4-char hex = "0009")
└─ Format version = "0001"
```

When the same pool name (e.g., `mirrored-pool`) exists on both clusters, the pool IDs differ:
- East: pool ID = 1 → `0000000000000001`
- West: pool ID = 2 → `0000000000000002`

The consumer rewrites: `0001-0009-rook-ceph-0000000000000001-<uuid>` → `0001-0009-rook-ceph-0000000000000002-<uuid>`

### CephBlockPool Dependency

The consumer controller requires the `ceph.rook.io/v1` API types. Add the Rook dependency:

```go
import cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
```

Register in scheme (in main.go or suite_test.go):
```go
cephv1.AddToScheme(scheme)
```

The `CephBlockPool.Status` has a `PoolID` field (int type in Rook's API types). If the Rook Go API is too heavy a dependency, the alternative is an unstructured client get:

```go
var cbp unstructured.Unstructured
cbp.SetGroupVersionKind(schema.GroupVersionKind{
    Group: "ceph.rook.io", Version: "v1", Kind: "CephBlockPool",
})
err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: "rook-ceph", Name: poolName}, &cbp)
poolID, _, _ := unstructured.NestedInt64(cbp.Object, "status", "info", "poolNumber")
```

**Recommended approach:** Use unstructured client to avoid importing the entire Rook module. The pool ID is at `.status.info["poolNumber"]` (string representation) in some Rook versions, or `.status.poolID` (int) in newer versions. Check the actual Rook version deployed on the Minikube clusters (Rook v1.16+ uses `.status.info["poolNumber"]` as a string). Parse defensively:

```go
func (r *ShadowPVConsumerReconciler) resolveLocalPoolID(
    ctx context.Context, poolName string,
) (int, error) {
    var cbp unstructured.Unstructured
    cbp.SetGroupVersionKind(schema.GroupVersionKind{
        Group: "ceph.rook.io", Version: "v1", Kind: "CephBlockPool",
    })
    if err := r.APIReader.Get(ctx, client.ObjectKey{
        Namespace: "rook-ceph", Name: poolName,
    }, &cbp); err != nil {
        return 0, fmt.Errorf("getting CephBlockPool %s: %w", poolName, err)
    }

    // Try .status.info.poolNumber (string in Rook v1.14+)
    info, _, _ := unstructured.NestedStringMap(cbp.Object, "status", "info")
    if poolNumStr, ok := info["poolNumber"]; ok {
        poolID, err := strconv.Atoi(poolNumStr)
        if err == nil {
            return poolID, nil
        }
    }

    return 0, fmt.Errorf("CephBlockPool %s has no pool ID in status", poolName)
}
```

### Retry on ResourceVersion Conflict

Wrap status patch with `retry.RetryOnConflict` using `engine.ScyllaRetry`:

```go
err := retry.RetryOnConflict(engine.ScyllaRetry, func() error {
    var fresh soteriav1alpha1.ShadowPV
    if err := r.Get(ctx, client.ObjectKeyFromObject(spv), &fresh); err != nil {
        return err
    }
    return r.setConflictCondition(ctx, &fresh, pvName)
})
```

### File Inventory

| File | Action | Lines Changed (est.) |
|------|--------|---------------------|
| `pkg/drivers/csiextension/volumehandle.go` | NEW — ParseVolumeHandle, RewritePoolID, VolumeHandle type | ~80 |
| `pkg/drivers/csiextension/volumehandle_test.go` | NEW — table-driven tests for volume handle parsing | ~120 |
| `pkg/controller/shadowpv/consumer.go` | NEW — ShadowPVConsumerReconciler, Reconcile, reconcilePV, resolveLocalPoolID, SetupWithManager | ~250 |
| `pkg/controller/shadowpv/consumer_test.go` | NEW — unit tests with envtest | ~350 |
| `pkg/controller/shadowpv/doc.go` | MODIFY — add consumer controller documentation | ~10 |
| `cmd/soteria/main.go` | MODIFY — register ShadowPVConsumerReconciler | ~12 |

**Total: ~4 new files, ~2 modified files, ~820 lines**

### Key Constraints

- **Do NOT delete PVs** — the consumer creates PVs but never deletes them. PV lifecycle is Kubernetes-native (ReclaimPolicy governs deletion). If a ShadowPV entry disappears, the PV remains
- **Do NOT update existing PVs** — once created, PVs are immutable from the consumer's perspective. If the source PV spec changes, the consumer does not patch the existing PV (PV specs are largely immutable in Kubernetes anyway)
- **Do NOT import the full Rook module** — use `unstructured.Unstructured` client for CephBlockPool access to avoid pulling in the entire Rook dependency tree
- **Do NOT access ScyllaDB directly** — ShadowPV is accessed through the kube-apiserver aggregated API. The controller-runtime client goes through the proxy (per project context rule)
- **Do NOT implement error condition bubbling to DRPlan** — ShadowPV status conditions are local to ShadowPV. DRPlan integration is deferred to a future epic
- **Do NOT watch PVs** — the consumer is triggered by ShadowPV changes only. PV creation is a one-shot operation during reconcile
- **Do NOT handle PV deletion on ShadowPV entry removal** — the consumer is additive only. Entry removal handling (if needed) belongs in a future epic
- **Use `engine.ScyllaRetry` for conflict retries** — ShadowPV is stored in ScyllaDB
- **Use `client.MergeFrom` strategic merge patch for status updates** — reduces conflict surface with the publisher which updates spec
- **Consumer label format:** `soteria.io/shadowpv-consumer=<shadowpv-name>` on created PVs — enables idempotency check and conflict detection
- **PV is cluster-scoped** — use `client.ObjectKey{Name: pvName}` (no namespace)
- **ShadowPV is cluster-scoped** — same as publisher (no namespace in ObjectKey)
- **CephBlockPool is namespace-scoped** — always in `rook-ceph` namespace (hardcoded for now, could be configurable in future)
- **The publisher stores raw PV specs** — the consumer is responsible for pool-ID rewrite. The publisher's output is the consumer's input
- **`+listType=atomic` on ShadowPV.spec.pvs** — the consumer reads the full list, it does not merge individual entries

### Naming Convention

PVs created by the consumer keep the original `pvName` from the ShadowPV entry (same name as the source PV). This enables PVC binding on the target cluster using `volumeName: <pvName>`.

Labels on created PVs:
- `soteria.io/shadowpv-consumer=<shadowpv-name>` — ownership marker for idempotency
- `soteria.io/drplan=<plan-name>` — carried from ShadowPV's label for plan-level queries

### Testing Strategy

**Volume handle tests (`volumehandle_test.go`):**

| Test | Input | Expected |
|------|-------|----------|
| `TestParseVolumeHandle_Valid` | `0001-0009-rook-ceph-0000000000000001-uuid` | Parsed VolumeHandle with poolID="0000000000000001" |
| `TestParseVolumeHandle_DifferentClusterID` | `0001-000c-my-cluster-id-000000000000000a-uuid` | clusterID="my-cluster-id", poolID hex a |
| `TestParseVolumeHandle_InvalidFormat` | `not-a-volume-handle` | Error returned |
| `TestParseVolumeHandle_TooShort` | `0001` | Error returned |
| `TestRewritePoolID_Basic` | handle with pool 1, newPoolID=2 | Pool segment becomes `0000000000000002` |
| `TestRewritePoolID_LargePoolID` | newPoolID=255 | Pool segment becomes `00000000000000ff` |
| `TestRewritePoolID_NonCephHandle` | invalid handle string | Error returned |

**Consumer controller tests (`consumer_test.go`) use envtest:**

| Test | Setup | Expected |
|------|-------|----------|
| `TestConsumer_RemoteEntry_CreatesPV` | ShadowPV with remote entry + CephBlockPool with poolID | PV created with rewritten handle, consumer label set |
| `TestConsumer_LocalEntry_Skipped` | ShadowPV with entry matching localSite | No PV created |
| `TestConsumer_PVExists_WithLabel_NoOp` | PV already exists with consumer label | No error, no update |
| `TestConsumer_PVExists_NoLabel_Conflict` | PV exists without consumer label | PVConflict condition set, warning event emitted |
| `TestConsumer_NonCephHandle_CreatesAsIs` | Entry with non-Ceph volume handle | PV created with original handle, warning event |
| `TestConsumer_CephBlockPoolNotFound` | No CephBlockPool in cluster | Error returned (requeue) |
| `TestConsumer_MultipleRemoteEntries` | ShadowPV with 3 remote entries | 3 PVs created |
| `TestConsumer_PoolIDRewrite_Correct` | Source pool=1, target pool=3 | Handle has `0000000000000003` |
| `TestConsumer_EntryRemoved_PVRemains` | PV was previously created, entry now missing | PV not deleted |
| `TestConsumer_MixedEntries` | 2 remote + 1 local entry | 2 PVs created, local skipped |
| `TestConsumer_AlreadyExists_Race` | Create returns AlreadyExists | No error (handles race) |

### Previous Story Intelligence

**Story 15.5 (ShadowPV Publisher):**
- Creates ShadowPV resources named `<planName>-<vgName>` with `soteria.io/drplan` label
- Each entry has `ClusterName` (the site that published it), `PVName`, and full `corev1.PersistentVolumeSpec`
- Uses `client.MergeFrom` patch for updates (reduces conflict with consumer's status patches)
- The publisher stores raw PV specs as-is — no pool-ID manipulation
- `mergeEntries` preserves remote-site entries and replaces local-site entries
- `entriesEqual` uses `apiequality.Semantic.DeepEqual` for PV spec comparison

**Story 15.4 (ShadowPV CRD):**
- `ShadowPVStatus.Conditions []metav1.Condition` — consumer uses this for PVConflict
- `shadowpvs/status` subresource registered — consumer updates status independently from spec
- `+listType=atomic` on PVs — full list replacement on each update
- Validation: non-empty clusterName/pvName, no duplicate pairs
- OwnerReference to DRPlan set by PrepareForCreate in strategy (not by consumer)

**Story 13.4 (DRPlan VR/VGR watches):**
- Pattern for `.Watches()` with predicates — consumer uses simpler `.For()` since ShadowPV is the primary type

**Epic 14 infrastructure:**
- `hack/multisite/setup-rook-ceph.sh` lines 646-689 — reference implementation of pool-ID rewrite and PV creation
- CephBlockPool `mirrored-pool` with mirroring enabled
- Volume handle format confirmed: `0001-0009-rook-ceph-<16hex>-<uuid>`
- Pool name from PV's `spec.csi.volumeAttributes["pool"]` = `"mirrored-pool"`

### Git Intelligence

Recent commits are all Epic 14 infrastructure (shell scripts, no Go code). The controller framework and driver patterns are stable since Epic 13. No dependency updates or API changes affect this story. The Rook-Ceph manifests in `hack/multisite/manifests/rook-ceph/` confirm the CephBlockPool naming and structure.

### Downstream Impact

This is the last story in the ShadowPV chain (15-4 → 15-5 → 15-6). After this:
- Story 15-8 (Soteria Operator Deployment) deploys the operator with all controllers including publisher + consumer
- Story 15-9 (Full Lifecycle E2E Test) validates the complete ShadowPV flow end-to-end

The E2E test will verify:
1. Publisher discovers PVs from VR/VGR CRs → creates ShadowPV entries
2. Consumer reads remote entries → creates PVs with pool-ID rewrite
3. PVC on target cluster binds to pre-provisioned PV → VM can start

### Project Structure Notes

- `pkg/drivers/csiextension/volumehandle.go` — lives in the CSI extension package because volume handle format is CSI-driver-specific. Other drivers (Dell, Pure) would have different handle formats
- `pkg/controller/shadowpv/consumer.go` — same package as publisher (both share ShadowPV domain). The package `doc.go` should document both controllers
- No new `internal/` packages needed
- No `config/crd/bases/` changes — ShadowPV is aggregated API
- `go.mod` does NOT need Rook dependency — using unstructured client

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.6 — acceptance criteria and technical notes]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.5 — upstream publisher ShadowPV creation pattern]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.4 — ShadowPV type definition, status conditions]
- [Source: _bmad-output/implementation-artifacts/15-5-shadowpv-publisher-controller.md — publisher reconcile flow, ShadowPV naming, label constants]
- [Source: _bmad-output/implementation-artifacts/15-4-shadowpv-crd-scylladb-storage.md — ShadowPV types, StatusREST, validation, strategy]
- [Source: hack/multisite/setup-rook-ceph.sh#lines 646-689 — reference pool-ID rewrite implementation and PV creation]
- [Source: hack/multisite/manifests/rook-ceph/ceph-blockpool.yaml — CephBlockPool manifest (pool name: mirrored-pool)]
- [Source: pkg/drivers/csiextension/constants.go — LabelVolumeGroup, LabelDRPlan re-exports]
- [Source: pkg/drivers/types.go — LabelDRPlan constant definition]
- [Source: pkg/engine/executor.go — ScyllaRetry retry policy for ScyllaDB-backed resources]
- [Source: pkg/controller/drexecution/reconciler.go — LocalSite field pattern, APIReader pattern, SetupWithManager pattern]
- [Source: pkg/controller/volumereplication/reconciler.go — controller setup pattern]
- [Source: _bmad-output/project-context.md — ScyllaDB storage boundary, MergeFrom patch rule, testing rules, Tier 1/2/3 docs]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
