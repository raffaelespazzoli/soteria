# Story 14.2: Rook-Ceph Deployment with RBD Volume Replication

Status: ready-for-dev

## Story

As a platform engineer,
I want Rook-Ceph deployed on both Kind clusters with RBD mirroring and VolumeReplication CRDs,
so that real storage replication is available for Soteria's CSI Extension driver.

## Acceptance Criteria

**AC1: Rook operator deployment**
Given both Kind clusters from Story 14.1
When `setup-rook-ceph.sh` is executed
Then the Rook operator is deployed on both clusters
And operator pods are running and healthy

**AC2: CephCluster creation**
Given the Rook operator is running
When CephCluster CRs are applied
Then Ceph clusters are created on both Kind clusters using loopback/raw-file OSDs (Kind-compatible)
And Ceph health is OK on both clusters (`ceph status` via toolbox)

**AC3: RBD mirroring configuration**
Given Ceph clusters are healthy
When CephBlockPool is configured with `mirroring.enabled: true, mode: image`
Then RBD mirroring is enabled on both clusters
And CephRBDMirror daemons are deployed on both clusters
And bootstrap peer tokens are exchanged between east and west
And `rbd mirror pool status` shows healthy peering

**AC4: CSI Addons deployment**
Given Rook-Ceph is running with RBD mirroring
When CSI Addons (Volume Replication Operator) is deployed
Then VolumeReplication and VolumeGroupReplication CRDs are available on both clusters
And CSI Addons controller pods are running

**AC5: VolumeReplicationClass creation**
Given CSI Addons is deployed
When VolumeReplicationClass is created with provisioner `rook-ceph.rbd.csi.ceph.com`
Then the VolumeReplicationClass is available on both clusters
And a StorageClass `rook-ceph-block` is created for RBD volumes

**AC6: Replication smoke test**
Given the full Rook-Ceph + mirroring stack is deployed
When a test PVC is created on east with a VolumeReplication CR attached
Then the VolumeReplication status shows replication is active (state `replaying` or `syncing`)
And the PVC binds successfully on the Rook-Ceph StorageClass

## Tasks / Subtasks

- [ ] Task 1: Create loop device setup for Kind workers (AC: 2)
  - [ ] 1.1: Add loop device creation function to `setup-rook-ceph.sh` — for each worker node in both clusters, `docker exec` into the container and create a 5GB sparse file (`truncate -s 5G /tmp/osd.img`) then attach as loop device (`losetup /dev/loop0 /tmp/osd.img`)
  - [ ] 1.2: Verify loop devices are visible inside each Kind worker container

- [ ] Task 2: Deploy Rook operator on both clusters (AC: 1)
  - [ ] 2.1: Add Helm repo `rook-release https://charts.rook.io/release`
  - [ ] 2.2: Install `rook-ceph` operator chart on east (`helm install --create-namespace --namespace rook-ceph rook-ceph rook-release/rook-ceph --set csi.csiAddons.enabled=true`)
  - [ ] 2.3: Install `rook-ceph` operator chart on west (same)
  - [ ] 2.4: Wait for operator pods to be running on both clusters

- [ ] Task 3: Create CephCluster CRs (AC: 2)
  - [ ] 3.1: Create `hack/multisite/manifests/ceph-cluster.yaml` — CephCluster CR with Kind-friendly settings (1 mon allowMultiplePerNode, 1 mgr, dashboard disabled, monitoring disabled, `storage.useAllNodes: true`, `storage.useAllDevices: false`, `storage.deviceFilter: "^loop"`, reduced resource requests, `allowUnsupported: true`, ceph image `quay.io/ceph/ceph:v18`)
  - [ ] 3.2: Apply CephCluster on east and west
  - [ ] 3.3: Wait for CephCluster to reach `Ready` phase (or verify via toolbox `ceph status`)

- [ ] Task 4: Configure CephBlockPool with mirroring (AC: 3)
  - [ ] 4.1: Create `hack/multisite/manifests/ceph-blockpool.yaml` — CephBlockPool CR (`mirrored-pool`) with `replicated.size: 1`, `mirroring.enabled: true`, `mirroring.mode: image`
  - [ ] 4.2: Apply CephBlockPool on both clusters
  - [ ] 4.3: Deploy CephRBDMirror CR on both clusters (`count: 1`)

- [ ] Task 5: Exchange RBD mirroring bootstrap peer secrets (AC: 3)
  - [ ] 5.1: Extract bootstrap peer secret from east cluster
  - [ ] 5.2: Import east's secret into west cluster and patch CephBlockPool with peer secret reference
  - [ ] 5.3: Extract bootstrap peer secret from west cluster
  - [ ] 5.4: Import west's secret into east cluster and patch CephBlockPool with peer secret reference
  - [ ] 5.5: Wait for `rbd mirror pool status` to show healthy on both clusters

- [ ] Task 6: Enable CSI Addons sidecar and deploy CSI Addons controller (AC: 4)
  - [ ] 6.1: Patch `rook-ceph-operator-config` ConfigMap on both clusters to set `CSI_ENABLE_CSIADDONS: "true"` and `CSI_ENABLE_OMAP_GENERATOR: "true"`
  - [ ] 6.2: Wait for CSI RBD provisioner pods to restart with the `csi-addons` sidecar container
  - [ ] 6.3: Deploy CSI Addons controller manifests (CRDs, RBAC, controller Deployment) from `https://raw.githubusercontent.com/csi-addons/kubernetes-csi-addons/main/deploy/controller/` on both clusters
  - [ ] 6.4: Verify VolumeReplication and VolumeGroupReplication CRDs are registered

- [ ] Task 7: Create VolumeReplicationClass and StorageClass (AC: 5)
  - [ ] 7.1: Create `hack/multisite/manifests/volume-replication-class.yaml` — VolumeReplicationClass with provisioner `rook-ceph.rbd.csi.ceph.com`, `mirroringMode: snapshot`, replication secret references
  - [ ] 7.2: Create `hack/multisite/manifests/storage-class.yaml` — StorageClass `rook-ceph-block` using provisioner `rook-ceph.rbd.csi.ceph.com`, pool `mirrored-pool`, `imageFeatures: layering,exclusive-lock`
  - [ ] 7.3: Apply both on east and west

- [ ] Task 8: Replication smoke test (AC: 6)
  - [ ] 8.1: Create a test PVC on east using StorageClass `rook-ceph-block`
  - [ ] 8.2: Create a VolumeReplication CR referencing the test PVC with `replicationState: primary`
  - [ ] 8.3: Verify VolumeReplication status shows active replication
  - [ ] 8.4: Clean up test resources

- [ ] Task 9: Script finalization and README update (AC: 1, 5, 6)
  - [ ] 9.1: Add idempotency checks throughout `setup-rook-ceph.sh`
  - [ ] 9.2: Add cleanup function for teardown
  - [ ] 9.3: Update `hack/multisite/README.md` with Rook-Ceph section

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — shell scripts and YAML manifests only, no Go code. The output is `hack/multisite/setup-rook-ceph.sh` plus YAML manifests in `hack/multisite/manifests/`. This story deploys the real storage replication layer that replaces the noop controller used in Epics 12-13.

### Critical Dependency: Story 14.1

This story requires the Kind clusters from Story 14.1 to be running. Specifically:
- Both `east` and `west` Kind clusters must exist
- Each cluster must have 3 worker nodes with `extraMounts` pointing to `/var/lib/rook-{east,west}/osd{0,1,2}`
- Cilium must be installed as CNI (Rook-Ceph needs a working pod network)

The `extraMounts` from Story 14.1 provide host directories that map into worker containers. However, for Rook-Ceph OSDs on Kind, we need actual **loop devices** inside the worker containers — the `extraMounts` alone are host directories, not block devices. The recommended approach is to `docker exec` into each worker container and create loop devices from sparse files.

### Rook-Ceph on Kind — OSD Strategy

Kind containers do not have real block devices. Two approaches exist:

1. **Loop devices inside containers (recommended):** `docker exec` into each worker node, create a sparse file (`truncate -s 5G /tmp/osd.img`), and attach it as a loop device (`losetup /dev/loop0 /tmp/osd.img`). Configure CephCluster with `storage.deviceFilter: "^loop"`. This gives Ceph real block devices to work with.

2. **Directory-backed OSDs (fallback):** Use `dataDirHostPath` with `useAllDevices: false` and configure `directories` in the CephCluster storage spec. Simpler but may have limitations with mirroring features.

Use approach 1 (loop devices) since RBD mirroring requires proper block device semantics.

**Important:** Loop devices created with `losetup` inside Kind containers do not persist across container restarts. The setup script must recreate them if clusters are restarted. Consider `allowUnsupported: true` on the CephCluster to suppress health warnings about non-production devices.

### Helm-Based Rook Operator Installation

Install the Rook operator via Helm chart (not raw manifests) for reproducibility:

```bash
helm repo add rook-release https://charts.rook.io/release
helm install --create-namespace --namespace rook-ceph rook-ceph \
  rook-release/rook-ceph \
  --set csi.csiAddons.enabled=true
```

The `csi.csiAddons.enabled=true` flag enables the CSI Addons sidecar at operator install time, avoiding a post-install ConfigMap patch.

Do NOT use the `rook-ceph-cluster` chart — apply CephCluster/CephBlockPool/CephRBDMirror as separate manifests for fine-grained control over the mirroring bootstrap sequence.

### CephCluster Configuration for Kind

```yaml
apiVersion: ceph.rook.io/v1
kind: CephCluster
metadata:
  name: rook-ceph
  namespace: rook-ceph
spec:
  cephVersion:
    image: quay.io/ceph/ceph:v18
    allowUnsupported: true
  dataDirHostPath: /var/lib/rook
  mon:
    count: 1
    allowMultiplePerNode: true
  mgr:
    count: 1
    allowMultiplePerNode: true
    modules:
      - name: pg_autoscaler
        enabled: true
  dashboard:
    enabled: false
  monitoring:
    enabled: false
  storage:
    useAllNodes: true
    useAllDevices: false
    deviceFilter: "^loop"
  resources:
    mon:
      requests:
        cpu: "100m"
        memory: "256Mi"
      limits:
        memory: "512Mi"
    osd:
      requests:
        cpu: "100m"
        memory: "512Mi"
      limits:
        memory: "1Gi"
    mgr:
      requests:
        cpu: "100m"
        memory: "256Mi"
      limits:
        memory: "512Mi"
```

Key tuning for Kind:
- `mon.count: 1` + `allowMultiplePerNode: true` — Kind workers share resources
- `dashboard.enabled: false` / `monitoring.enabled: false` — saves ~200MB RAM
- `deviceFilter: "^loop"` — matches loop devices created by the setup script
- Reduced resource requests/limits — Kind clusters are resource-constrained
- `replicated.size: 1` on the CephBlockPool — single-node failure domain

### CephBlockPool with Mirroring

```yaml
apiVersion: ceph.rook.io/v1
kind: CephBlockPool
metadata:
  name: mirrored-pool
  namespace: rook-ceph
spec:
  replicated:
    size: 1
  mirroring:
    enabled: true
    mode: image
```

`replicated.size: 1` because each Kind cluster has only 3 small OSDs — size 2+ may cause PG warnings. `mirroring.mode: image` enables per-image mirroring (as opposed to pool-level), which is what VolumeReplication CRs control.

### RBD Mirroring Bootstrap Peer Exchange

This is the most operationally complex part of the story. The sequence is:

1. **Apply CephBlockPool on both clusters** — Rook creates a bootstrap peer secret automatically when mirroring is enabled
2. **Wait for bootstrap secret creation** — The secret name appears in `CephBlockPool.status.info["rbdMirrorBootstrapPeerSecretName"]`
3. **Export east's secret:** `kubectl get secret -n rook-ceph <name> -o jsonpath='{.data.token}' | base64 -d`
4. **Create import secret on west:** Create a Secret with the token, then patch west's CephBlockPool to add `mirroring.peers.secretNames`
5. **Repeat in reverse** for bidirectional mirroring (east imports west's token)
6. **Deploy CephRBDMirror CR on both clusters** — one daemon per cluster is sufficient

```yaml
apiVersion: ceph.rook.io/v1
kind: CephRBDMirror
metadata:
  name: rbd-mirror
  namespace: rook-ceph
spec:
  count: 1
```

**Verification:** After peer exchange, use the Rook toolbox to verify:
```bash
kubectl -n rook-ceph exec deploy/rook-ceph-tools -- rbd mirror pool status mirrored-pool
```
Expected: `health: OK` with peer info visible.

### CSI Addons Deployment

The CSI Addons controller is deployed separately from the Rook operator. It provides the VolumeReplication CRD reconciliation that translates VR/VGR objects into actual Ceph RBD mirror operations.

Two configuration points:
1. **Operator ConfigMap** (if not using Helm flag): Patch `rook-ceph-operator-config` with `CSI_ENABLE_CSIADDONS: "true"` and `CSI_ENABLE_OMAP_GENERATOR: "true"` — this adds the `csi-addons` sidecar to CSI provisioner pods
2. **CSI Addons controller deployment:** Apply manifests from the `kubernetes-csi-addons` repo:
   - `deploy/controller/crds.yaml` — VolumeReplication, VolumeGroupReplication, ReclaimSpaceJob CRDs
   - `deploy/controller/rbac.yaml` — ServiceAccount, ClusterRole, ClusterRoleBinding
   - `deploy/controller/setup-controller.yaml` — Controller Deployment

**After deployment, verify:**
- `kubectl get crd volumereplicationclasses.replication.storage.openshift.io` exists
- `kubectl get crd volumereplications.replication.storage.openshift.io` exists
- `kubectl get crd volumegroupreplications.replication.storage.openshift.io` exists
- CSI Addons controller pod is running

### VolumeReplicationClass

This is the VRC that Soteria's `DRPlan.spec.volumeReplicationDriver.volumeReplicationClass` will reference in Story 14.6:

```yaml
apiVersion: replication.storage.openshift.io/v1alpha1
kind: VolumeReplicationClass
metadata:
  name: rook-ceph-rbd-vrc
spec:
  provisioner: rook-ceph.rbd.csi.ceph.com
  parameters:
    mirroringMode: snapshot
    schedulingInterval: "1m"
    replication.storage.openshift.io/replication-secret-name: rook-csi-rbd-provisioner
    replication.storage.openshift.io/replication-secret-namespace: rook-ceph
```

Key parameters:
- `mirroringMode: snapshot` — uses snapshot-based mirroring (does NOT require `journaling` image feature, only `exclusive-lock`)
- `schedulingInterval: "1m"` — 1-minute snapshot schedule for fast RPO during testing (production would use longer intervals)
- The replication secret references are the CSI provisioner secrets created by Rook

### StorageClass

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: rook-ceph-block
provisioner: rook-ceph.rbd.csi.ceph.com
parameters:
  clusterID: rook-ceph
  pool: mirrored-pool
  imageFormat: "2"
  imageFeatures: layering,exclusive-lock
  csi.storage.k8s.io/provisioner-secret-name: rook-csi-rbd-provisioner
  csi.storage.k8s.io/provisioner-secret-namespace: rook-ceph
  csi.storage.k8s.io/controller-expand-secret-name: rook-csi-rbd-provisioner
  csi.storage.k8s.io/controller-expand-secret-namespace: rook-ceph
  csi.storage.k8s.io/node-stage-secret-name: rook-csi-rbd-node
  csi.storage.k8s.io/node-stage-secret-namespace: rook-ceph
reclaimPolicy: Delete
allowVolumeExpansion: true
```

`imageFeatures: layering,exclusive-lock` — `exclusive-lock` is required for RBD mirroring. Do NOT add `journaling` when using `mirroringMode: snapshot`.

### Downstream Impact on Soteria

The VolumeReplicationClass name (`rook-ceph-rbd-vrc`) will be used in Story 14.6 when creating DRPlans with `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}`. This replaces the noop controller from Epics 12-13 with real CSI Addons sidecar handling VR/VGR reconciliation.

The StorageClass name (`rook-ceph-block`) replaces `ontap-san` / `ontap-san-xfs` from the production `hack/stretched-local-test.sh`.

### Existing Patterns to Follow

The script follows the same conventions as `hack/stretched-local-test.sh`:
- Env-var-driven configuration at top of script
- `kctl()` helper wrapping kubectl with explicit kubeconfig/context
- Preflight checks
- Idempotent operations (check before create, tolerate NotFound on delete)
- `set -euo pipefail` for strict error handling
- Status messages for each step

### Idempotency

- Helm install uses `helm upgrade --install` for idempotency
- `kubectl apply` is naturally idempotent for manifests
- Loop device creation checks `losetup -a | grep osd.img` before creating
- Bootstrap peer exchange checks if peer secret already exists before importing
- CSI Addons controller checks if CRDs exist before applying

### File Structure

```
hack/multisite/
├── setup-rook-ceph.sh                  # Main setup script
├── manifests/
│   ├── ceph-cluster.yaml               # CephCluster CR (Kind-tuned)
│   ├── ceph-blockpool.yaml             # CephBlockPool with mirroring enabled
│   ├── ceph-rbd-mirror.yaml            # CephRBDMirror daemon CR
│   ├── volume-replication-class.yaml   # VolumeReplicationClass for Soteria
│   └── storage-class.yaml              # StorageClass rook-ceph-block
└── README.md                           # (updated with Rook-Ceph section)
```

### Timing and Resource Expectations

- Rook operator Helm install: ~2 min per cluster
- CephCluster bootstrapping (mons, mgr, OSDs): ~3-5 min per cluster
- RBD mirroring peer exchange + daemon readiness: ~2-3 min
- CSI Addons deployment: ~1 min per cluster
- Total: ~15-20 min for the full Rook-Ceph stack on both clusters

### Potential Failure Modes

1. **Loop devices not found by Ceph** — `deviceFilter: "^loop"` may not match if `losetup` path differs. Verify with `docker exec <node> ls /dev/loop*`
2. **OSD pods in CrashLoopBackOff** — Usually insufficient memory. Reduce `osd.limits.memory` or increase Kind node resources
3. **Bootstrap peer secret never created** — Check CephBlockPool status conditions and Rook operator logs. May indicate mirroring not fully initialized
4. **CSI Addons sidecar not starting** — Verify `rook-ceph-operator-config` ConfigMap has `CSI_ENABLE_CSIADDONS: "true"` and operator pods have restarted
5. **VolumeReplication stuck in `Unknown`** — Check CSI Addons controller logs for connection errors to the sidecar

### Testing Standards

No Go tests for this story — validation is via the replication smoke test in AC6. The setup script includes verification steps (Ceph health check, mirror pool status, VR status).

### Project Structure Notes

- All files go in `hack/multisite/` — follows the convention from Story 14.1
- YAML manifests live in `hack/multisite/manifests/` — a new subdirectory to keep manifests organized separately from scripts
- No Makefile targets needed in this story

### References

- [Source: epics.md#Story 14.2] — acceptance criteria and technical notes
- [Source: hack/stretched-local-test.sh] — existing multi-site deployment pattern (StorageClass `ontap-san` → `rook-ceph-block`)
- [Source: project-context.md#StorageProvider Driver Framework] — 6-method driver interface, CSI Extension driver
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — `VolumeReplicationDriverConfig` struct with `VolumeReplicationClass` field
- [Source: pkg/drivers/csiextension/] — CSI Extension driver that creates/manages VR/VGR CRs
- [Source: Rook RBD Mirroring docs] — https://rook.io/docs/rook/latest/Storage-Configuration/Block-Storage-RBD/rbd-mirroring/
- [Source: Rook CSI Drivers docs] — https://rook.io/docs/rook/latest/Storage-Configuration/Ceph-CSI/ceph-csi-drivers/
- [Source: CSI Addons deploy] — https://github.com/csi-addons/kubernetes-csi-addons/tree/main/deploy/controller
- [Source: Story 14.1] — Kind cluster provisioning with extraMounts for OSD paths

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
