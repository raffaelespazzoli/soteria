# Story 14.5: ScyllaDB Cross-DC Deployment on Rook-Ceph

Status: done

## Story

As a platform engineer,
I want ScyllaDB deployed as a cross-DC cluster on both Minikube KVM2 clusters using Rook-Ceph storage and Cilium Cluster Mesh for inter-node communication,
so that Soteria has its shared state store for the integration test environment.

## Acceptance Criteria

**AC1: cert-manager deployment**
Given both Minikube KVM2 clusters
When the ScyllaDB setup script is executed
Then cert-manager is deployed on both clusters
And a self-signed CA issuer is created for Soteria TLS certificates

**AC2: scylla-operator deployment**
Given cert-manager is running
When the scylla-operator is deployed
Then the ScyllaCluster CRD is available on both clusters
And scylla-operator pods are running

**AC3: ScyllaCluster creation with Rook-Ceph storage**
Given the scylla-operator and Rook-Ceph are running
When ScyllaCluster CRs are applied
Then ScyllaDB is deployed on both Minikube clusters with datacenter names `east` and `west`
And ScyllaDB PVCs use the XFS-formatted Rook-Ceph StorageClass (`rook-ceph-block-xfs`)
And developer-mode resource requests are used (reduced from production sizing)

**AC4: Cross-DC discovery via Cilium Cluster Mesh**
Given Cilium Cluster Mesh is connected between east and west
When the ScyllaDB headless service on east is annotated with `service.cilium.io/global: "true"`
Then west's ScyllaDB can discover east's seed nodes via the normal service FQDN
And `externalSeeds` in west's ScyllaCluster uses the standard service FQDN (not `clusterset.local`)

**AC5: mTLS configuration**
Given cert-manager is running on both clusters
When TLS certificates are created following the `stretched-local-test.sh` pattern
Then ScyllaDB inter-node and client communication uses mTLS
And a combined CA trust bundle is created (same pattern as `create_combined_ca` in existing script)

**AC6: Kustomize overlays**
Given the existing overlay structure in `hack/overlays/`
When new overlays are created
Then `hack/multisite/overlays/{base,east,west}/` mirror the `hack/overlays/{base,etl6,etl7}/` structure
And Cilium global service annotation replaces Submariner ServiceExport
And a Rook-Ceph XFS StorageClass (`rook-ceph-block-xfs`) replaces `ontap-san-xfs` for ScyllaDB volumes

**Note:** Minikube KVM2 VMs typically include `xfsprogs`. If PVCs fail to mount with an XFS error, install it via `minikube ssh -p <profile> -- "sudo dnf install -y xfsprogs"` on each node.

**AC7: Multi-DC convergence smoke test**
Given ScyllaDB is deployed on both clusters
When the seed cluster (east) is deployed first and west joins
Then `nodetool status` shows all nodes in UN (Up Normal) state across both DCs
And a CQL write on east is readable on west (cross-DC replication verified)

## Tasks / Subtasks

- [x] Task 0: Create XFS-formatted StorageClass for ScyllaDB (AC: 6)
  - [x] 0.1: Create `hack/multisite/manifests/storage-class-xfs.yaml` — StorageClass `rook-ceph-block-xfs` (clone of `rook-ceph-block` from Story 14.2 with `csi.storage.k8s.io/fstype: xfs` added to parameters)
  - [x] 0.2: Apply `rook-ceph-block-xfs` StorageClass on both clusters
  - [x] 0.3: Verify PVC creation with XFS format (optional: create a test PVC, attach to pod, check `df -T` shows `xfs`)

- [x] Task 1: Deploy cert-manager on both clusters (AC: 1)
  - [x] 1.1: Add cert-manager Helm install function to `setup-scylladb.sh` — install via OCI registry `oci://quay.io/jetstack/charts/cert-manager` v1.20.2 with `--set crds.enabled=true`
  - [x] 1.2: Wait for cert-manager pods (cert-manager, cainjector, webhook) to reach Running state on both clusters
  - [x] 1.3: Create self-signed ClusterIssuer `soteria-selfsigned` and CA Issuer `soteria-internal` on both clusters (same pattern as `hack/overlays/base/apiserver-cert.yaml`)

- [x] Task 2: Deploy scylla-operator on both clusters (AC: 2)
  - [x] 2.1: Add Helm repo `scylla https://scylla-operator-charts.storage.googleapis.com/stable`
  - [x] 2.2: `helm install scylla-operator scylla/scylla-operator --create-namespace --namespace scylla-operator` on both clusters
  - [x] 2.3: Wait for scylla-operator Deployment and webhook-server Deployment rollout on both clusters
  - [x] 2.4: Verify ScyllaCluster CRD is available (`kubectl get crd scyllaclusters.scylla.scylladb.com`)

- [x] Task 3: Create Kustomize overlays for Minikube environment (AC: 6)
  - [x] 3.1: Create `hack/multisite/overlays/base/kustomization.yaml` — references ScyllaCluster CR, TLS config, global service annotation, manager patches (mirrors `hack/overlays/base/kustomization.yaml`)
  - [x] 3.2: Create `hack/multisite/overlays/base/scylladb-tls-config.yaml` — cert-manager Certificate + Secret for ScyllaDB serving TLS (mirrors `hack/overlays/base/scylladb-tls-config.yaml`)
  - [x] 3.3: Create `hack/multisite/overlays/base/scylladb-tls-patch.yaml` — ScyllaCluster strategic merge patch adding TLS options (mirrors `hack/overlays/base/scylladb-tls-patch.yaml`)
  - [x] 3.4: Create `hack/multisite/overlays/base/cilium-global-service.yaml` — Cilium `service.cilium.io/global: "true"` annotation patch for ScyllaDB client headless service (replaces Submariner ServiceExport from `hack/overlays/base/serviceexport.yaml`)
  - [x] 3.5: Create `hack/multisite/overlays/east/kustomization.yaml` — patches ScyllaCluster with `datacenter.name: east`, no `externalSeeds` (seed cluster)
  - [x] 3.6: Create `hack/multisite/overlays/east/scyllacluster-patch.yaml` — datacenter name, developer mode resources, StorageClass `rook-ceph-block-xfs`
  - [x] 3.7: Create `hack/multisite/overlays/west/kustomization.yaml` — patches ScyllaCluster with `datacenter.name: west`, adds `externalSeeds` referencing east
  - [x] 3.8: Create `hack/multisite/overlays/west/scyllacluster-patch.yaml` — datacenter name, `externalSeeds` with east's service FQDN, developer mode resources, StorageClass `rook-ceph-block-xfs`

- [x] Task 4: Create `hack/multisite/setup-scylladb.sh` (AC: 1, 2, 3, 4, 5, 6, 7)
  - [x] 4.1: Env-var configuration block (namespaces, versions, cluster contexts, consistent with `setup-clusters.sh`)
  - [x] 4.2: Prerequisite checks (helm, kubectl, Rook-Ceph StorageClass `rook-ceph-block` probe, Cilium Cluster Mesh status)
  - [x] 4.2a: Create `rook-ceph-block-xfs` StorageClass on both clusters (Task 0)
  - [x] 4.3: cert-manager deploy function (Task 1)
  - [x] 4.4: Self-signed CA Issuer creation
  - [x] 4.5: scylla-operator deploy function (Task 2)
  - [x] 4.6: Apply east overlay first (`kustomize build` east → `kubectl apply --server-side`)
  - [x] 4.7: Annotate east ScyllaDB headless service with `service.cilium.io/global: "true"`
  - [x] 4.8: `create_combined_ca` helper (mirrors `hack/stretched-local-test.sh` pattern — waits for cert-manager and operator secrets, creates combined ConfigMap)
  - [x] 4.9: `wait_scylladb_ready` helper (polls `scyllaclusters.scylla.scylladb.com` status for readyMembers)
  - [x] 4.10: Wait for east ScyllaDB readiness, then apply west overlay
  - [x] 4.11: Annotate west ScyllaDB headless service with `service.cilium.io/global: "true"`
  - [x] 4.12: Wait for west ScyllaDB readiness

- [x] Task 5: mTLS configuration (AC: 5)
  - [x] 5.1: Post-deploy: patch ScyllaDB StatefulSets with cert-manager TLS volumes (mirrors STS patching from `hack/stretched-local-test.sh` — certmanager-serving secret, certmanager-ca, combined-ca ConfigMap)
  - [x] 5.2: Rolling restart ScyllaDB pods to pick up TLS volumes
  - [x] 5.3: Wait for ScyllaDB pods to restart and reach ready state

- [x] Task 6: Multi-DC convergence smoke test (AC: 7)
  - [x] 6.1: Wait for `nodetool status` to show all nodes UN across both DCs
  - [x] 6.2: Create a test keyspace with NTS replication, insert a row on east, read on west
  - [x] 6.3: Clean up test keyspace

- [x] Task 7: README and finalization (AC: 6)
  - [x] 7.1: Update `hack/multisite/README.md` with ScyllaDB section (prerequisites, usage, troubleshooting, access)
  - [x] 7.2: Add idempotency checks throughout script

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — a shell script, Kustomize overlays, and a README update. No Go code. The outputs are:
- `hack/multisite/setup-scylladb.sh` — main setup script
- `hack/multisite/overlays/{base,east,west}/` — Kustomize overlays for ScyllaDB
- `hack/multisite/README.md` — updated with ScyllaDB section

This story deploys the cross-DC state store that Soteria depends on. ScyllaDB is **not** a storage-replicated workload — it manages its own cross-DC replication via Cassandra gossip protocol. Rook-Ceph storage is used only for ScyllaDB's PVCs (durable data), not for replication.

### Critical: Cilium Global Service Annotation Change

The epics reference `io.cilium/global-service: "true"` — this is the **old** annotation format. The current Cilium docs (v1.19+) use:

```
service.cilium.io/global: "true"
```

This annotation replaces Submariner's `ServiceExport` CRD. When applied to the ScyllaDB headless service, it makes the service endpoints discoverable across clusters via the normal service FQDN (e.g., `soteria-scylladb-client.soteria.svc.cluster.local`), enabling cross-DC gossip.

For headless services, Cilium automatically syncs EndpointSlices across clusters when `service.cilium.io/global: "true"` is set (no additional `global-sync-endpoint-slices` annotation needed for headless).

**Key difference from Submariner:** With Cilium global services, the FQDN is the normal `svc.cluster.local` — NOT `svc.clusterset.local`. The `externalSeeds` in west's ScyllaCluster should use `soteria-scylladb-client.soteria.svc.cluster.local`.

### Critical: ScyllaDB Requires XFS Filesystem

ScyllaDB requires storage formatted with **XFS** (not ext4). The default `rook-ceph-block` StorageClass from Story 14.2 does not specify a filesystem type, which causes Rook-Ceph CSI to default to **ext4**. ScyllaDB will run on ext4 in developer mode but XFS is the supported and recommended filesystem.

Create a separate `rook-ceph-block-xfs` StorageClass that clones the existing `rook-ceph-block` but adds the `csi.storage.k8s.io/fstype: xfs` parameter:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: rook-ceph-block-xfs
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
  csi.storage.k8s.io/fstype: xfs
reclaimPolicy: Delete
allowVolumeExpansion: true
```

This keeps the original `rook-ceph-block` (ext4) for other workloads (KubeVirt VMs in Story 14.3) while providing XFS for ScyllaDB. The existing `hack/overlays/base/storageclass-xfs.yaml` in the production overlays follows the same pattern — a dedicated XFS StorageClass for ScyllaDB.

**Note:** Minikube KVM2 VMs typically include `xfsprogs` in their base image. If PVCs fail to mount with an XFS error, install it via `minikube ssh -p <profile> -- "sudo dnf install -y xfsprogs"` on each node.

### Critical: scylla-operator Helm Limitations for Multi-DC

The scylla-operator Helm chart docs state: "The Helm installation path supports single-datacenter deployments only." However, this refers to the `scylla/scylla` chart for deploying ScyllaCluster — not the operator itself. Our approach deploys one ScyllaCluster per Minikube cluster (each is a single DC) and connects them via `externalSeeds`. The operator is installed via Helm; the ScyllaCluster CRs are applied via Kustomize overlays.

### Critical: ScyllaDB Version and Developer Mode

Use `developerMode: true` on the ScyllaCluster spec. This:
- Lowers resource requirements (critical for Minikube)
- Relaxes tuning checks (required since Minikube nodes lack real hardware tuning)
- Is NOT for production — acceptable for integration testing

ScyllaDB version: `2026.1.3` (latest stable per operator docs). scylla-operator v1.21 (latest stable, released 2026-05-20).

### Dependencies

| Dependency | Story | What's Needed |
|------------|-------|---------------|
| Minikube KVM2 clusters | 14.1 | Both `east` and `west` clusters running with Cilium Cluster Mesh |
| Rook-Ceph | 14.2 | StorageClass `rook-ceph-block` available on both clusters (used as base for `rook-ceph-block-xfs`) |
| Cilium Cluster Mesh | 14.1 | Cross-cluster pod-to-pod connectivity for ScyllaDB gossip |

This story does NOT depend on 14.4 (Fedora VM Validation). It depends on 14.3 (KubeVirt) only because KubeVirt is deployed first in the sequence, but has no functional dependency on it.

### Existing Deployment Pattern: `hack/stretched-local-test.sh`

The existing script deploys ScyllaDB on real OpenShift clusters (etl6/etl7) using Submariner MCS. This story adapts the same pattern for Minikube KVM2 + Cilium + Rook-Ceph.

| Aspect | Existing (etl6/etl7) | This Story (east/west) |
|--------|----------------------|------------------------|
| Clusters | Real OpenShift | Minikube KVM2 clusters from 14.1 |
| Cross-DC gossip | Submariner MCS (ServiceExport → `clusterset.local`) | Cilium global service (`service.cilium.io/global` → `cluster.local`) |
| Storage | `ontap-san-xfs` (NetApp Trident, XFS) | `rook-ceph-block-xfs` (Rook-Ceph from 14.2, XFS-formatted) |
| Datacenter names | `etl6` / `etl7` | `east` / `west` |
| Members per rack | 2 | 1 (developer mode, resource-conscious for Minikube) |
| cert-manager | Pre-installed (prerequisite) | Installed by this script |
| scylla-operator | Pre-installed (OperatorHub) | Installed by this script (Helm) |
| TLS | mTLS with STS patching workaround | Same mTLS + STS patching pattern |
| Namespace | `soteria` | `soteria` (same) |

### cert-manager Installation

Install cert-manager via Helm OCI registry (source of truth, no repo-add needed):

```bash
helm install cert-manager oci://quay.io/jetstack/charts/cert-manager \
  --version v1.20.2 \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true
```

**Important:** `--set crds.enabled=true` is the new flag (replaced `--set installCRDs=true` starting v1.15). The OCI registry is canonical; the legacy `charts.jetstack.io` HTTP repo is a delayed mirror.

After installation, create a self-signed ClusterIssuer and a CA Issuer:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: soteria-selfsigned
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: soteria-ca
  namespace: soteria
spec:
  isCA: true
  commonName: soteria-ca
  secretName: soteria-ca-key-pair
  issuerRef:
    name: soteria-selfsigned
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-internal
  namespace: soteria
spec:
  ca:
    secretName: soteria-ca-key-pair
```

### scylla-operator Installation

```bash
helm repo add scylla https://scylla-operator-charts.storage.googleapis.com/stable
helm repo update
helm install scylla-operator scylla/scylla-operator \
  --create-namespace \
  --namespace scylla-operator
```

Wait for both deployments:

```bash
kubectl -n scylla-operator rollout status --timeout=5m deployment.apps/scylla-operator
kubectl -n scylla-operator rollout status --timeout=5m deployment.apps/webhook-server
```

### ScyllaCluster CR for Minikube (Developer Mode)

The base ScyllaCluster CR structure (before per-DC patches):

```yaml
apiVersion: scylla.scylladb.com/v1
kind: ScyllaCluster
metadata:
  name: soteria-scylladb
  namespace: soteria
spec:
  version: "2026.1.3"
  developerMode: true
  datacenter:
    name: REPLACED_BY_OVERLAY
    racks:
      - name: rack1
        members: 1
        storage:
          capacity: 5Gi
          storageClassName: rook-ceph-block-xfs
        resources:
          requests:
            cpu: 100m
            memory: 512Mi
          limits:
            cpu: "1"
            memory: 1Gi
```

Key settings:
- `developerMode: true` — required for Minikube (relaxes hardware tuning requirements)
- `members: 1` — single member per rack, conserves resources
- `storageClassName: rook-ceph-block-xfs` — XFS-formatted Rook-Ceph volume (ScyllaDB requires XFS)
- Reduced resource requests — Minikube nodes are resource-conscious
- Cluster name `soteria-scylladb` — must be identical on both clusters for multi-DC gossip

### Per-DC Overlay Patches

**East (seed cluster, no externalSeeds):**

```yaml
apiVersion: scylla.scylladb.com/v1
kind: ScyllaCluster
metadata:
  name: soteria-scylladb
  namespace: soteria
spec:
  datacenter:
    name: east
```

**West (joining cluster, references east via service FQDN):**

```yaml
apiVersion: scylla.scylladb.com/v1
kind: ScyllaCluster
metadata:
  name: soteria-scylladb
  namespace: soteria
spec:
  datacenter:
    name: west
  externalSeeds:
    - soteria-scylladb-client.soteria.svc.cluster.local
```

The `externalSeeds` FQDN resolves across clusters because Cilium Cluster Mesh's global service annotation on the headless service synchronizes EndpointSlices.

### Cilium Global Service Annotation

Apply to the ScyllaDB client headless service on both clusters AFTER ScyllaCluster creation (the service is created by the scylla-operator):

```bash
kubectl annotate service soteria-scylladb-client \
  -n soteria \
  service.cilium.io/global="true" \
  --overwrite
```

**Timing is critical:** The service must be annotated BEFORE west's ScyllaDB starts, so west can discover east's seed nodes. Annotate east's service immediately after east's ScyllaCluster becomes ready, before applying west's overlay.

Alternatively, create a Kustomize patch for the service annotation, but since the service is operator-managed (created dynamically), runtime annotation via kubectl is more reliable.

### Combined CA Trust Bundle (mTLS)

The `create_combined_ca` helper from `hack/stretched-local-test.sh` must be adapted:

1. Wait for cert-manager to create the `scylladb-serving-tls` secret (contains TLS cert + CA)
2. Wait for scylla-operator to create `soteria-scylladb-local-client-ca` ConfigMap (operator's internal client CA)
3. Combine both CAs into a single `scylladb-combined-ca` ConfigMap

```bash
create_combined_ca() {
  local ctx="$1"
  local CM_CA="" OP_CA=""
  for _ in $(seq 1 60); do
    raw_ca=$(kubectl --context="${ctx}" -n "${NAMESPACE}" \
      get secret scylladb-serving-tls -o jsonpath='{.data.ca\.crt}' 2>/dev/null) || true
    if [[ -n "${raw_ca}" ]]; then
      CM_CA=$(echo "${raw_ca}" | base64 -d 2>/dev/null) || true
    fi
    OP_CA=$(kubectl --context="${ctx}" -n "${NAMESPACE}" \
      get configmap soteria-scylladb-local-client-ca \
      -o jsonpath='{.data.ca-bundle\.crt}' 2>/dev/null) || true
    if [[ -n "${CM_CA}" && -n "${OP_CA}" ]]; then break; fi
    sleep 5
  done
  # Create combined ConfigMap
  kubectl --context="${ctx}" -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: scylladb-combined-ca
  namespace: ${NAMESPACE}
data:
  ca-bundle.crt: |
$(echo "${CM_CA}" | sed 's/^/    /')
$(echo "${OP_CA}" | sed 's/^/    /')
EOF
}
```

### StatefulSet TLS Volume Patching Workaround

The scylla-operator (as of v1.20/v1.21) does NOT propagate rack-level volumes/volumeMounts from the ScyllaCluster CR to the StatefulSet. The existing workaround from `hack/stretched-local-test.sh` patches the STS directly:

1. Find the ScyllaDB StatefulSet: `kubectl get sts -l scylla/cluster=soteria-scylladb -n soteria`
2. Find the `scylla` container index in the STS spec
3. Add three volumes: `certmanager-serving` (Secret), `certmanager-ca` (Secret CA only), `combined-ca` (ConfigMap)
4. Add three volumeMounts to the `scylla` container
5. Delete ScyllaDB pods to trigger restart with new volumes (operator recreates them)

This is an idempotent operation — check if volumes already exist before patching.

### Deployment Sequence

The sequence is critical due to seed/join ordering:

1. Install cert-manager on both clusters (parallel)
2. Create CA Issuers and soteria namespace on both clusters (parallel)
3. Install scylla-operator on both clusters (parallel)
4. Apply east overlay first (ScyllaCluster with datacenter `east`, no externalSeeds)
5. Annotate east's headless service with Cilium global service annotation
6. Create combined CA on east + wait for east readiness
7. Patch east STS with TLS volumes + restart pods + wait for readiness
8. Apply west overlay (ScyllaCluster with datacenter `west`, externalSeeds → east)
9. Annotate west's headless service with Cilium global service annotation
10. Create combined CA on west + wait for west readiness
11. Patch west STS with TLS volumes + restart pods + wait for readiness
12. Wait for multi-DC convergence (`nodetool status` shows UN nodes from both DCs)
13. Run CQL cross-DC verification

### Kustomize Overlay Structure

```
hack/multisite/overlays/
├── base/
│   ├── kustomization.yaml            # Shared resources + patches
│   ├── scyllacluster.yaml            # Base ScyllaCluster CR
│   ├── scylladb-tls-config.yaml      # cert-manager Certificate for ScyllaDB TLS
│   └── scylladb-tls-patch.yaml       # ScyllaCluster TLS options patch
├── east/
│   ├── kustomization.yaml            # Inherits base, DC-specific patches
│   └── scyllacluster-patch.yaml      # datacenter.name: east (no externalSeeds)
└── west/
    ├── kustomization.yaml            # Inherits base, DC-specific patches
    └── scyllacluster-patch.yaml      # datacenter.name: west, externalSeeds
```

This mirrors the `hack/overlays/{base,etl6,etl7}/` structure from the existing deployment.

### Script Conventions to Follow

Follow the same conventions from Stories 14.1/14.2/14.3:

- `set -euo pipefail` at top
- Env-var-driven configuration block at top of script
- `keast()` / `kwest()` helpers with explicit `--context`
- Cluster profiles: `east` and `west` (contexts match profile names)
- Idempotent operations (`helm upgrade --install`, `kubectl apply`, check-before-create)
- Status messages via `info()`, `warn()`, `error()`, `fatal()` helpers
- Prerequisite checks
- `NAMESPACE="${NAMESPACE:-soteria}"` consistent with existing script

### Context Pattern (from setup-clusters.sh / setup-rook-ceph.sh)

```bash
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"
NAMESPACE="${NAMESPACE:-soteria}"

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }
```

### Idempotency

- `helm upgrade --install` for cert-manager and scylla-operator
- `kubectl apply --server-side --force-conflicts` for Kustomize overlays
- Service annotation: `--overwrite` flag
- Combined CA creation: `kubectl apply` (naturally idempotent)
- STS patching: check if volumes already present before patching
- Script is safe to re-run without side effects

### Downstream Impact

Story 14.6 (Soteria Operator Deployment) depends on this story (14.5):
- Uses the ScyllaDB cluster deployed here as Soteria's state store
- Soteria's `--scylladb-local-dc` flag maps to the datacenter names set here (`east`/`west`)
- Soteria's `--site-name` flag maps to the same values
- The overlays created here will be extended in 14.6 with Soteria-specific patches

Story 14.7 (Lifecycle Test) depends on ScyllaDB being operational for cross-DC state replication during DR transitions.

### Potential Failure Modes

1. **ScyllaDB pods OOMKilled** — developerMode reduces but doesn't eliminate memory needs. If Minikube nodes are resource-constrained, increase `NODE_MEMORY` and recreate clusters
2. **externalSeeds resolution fails** — Cilium global service annotation not applied to east's headless service before west starts. Ensure the annotation is applied AFTER east's service exists but BEFORE west's ScyllaCluster is created
3. **cert-manager webhook not ready** — cert-manager webhook takes ~30s to become ready. Wait for webhook pod before creating Certificate resources
4. **scylla-operator webhook not ready** — Same timing issue. Wait for `webhook-server` deployment rollout before applying ScyllaCluster CRs
5. **Gossip failure (west can't find east seeds)** — Verify Cilium Cluster Mesh is connected (`cilium clustermesh status`). Verify the global service annotation is on the correct service (`soteria-scylladb-client`, not `soteria-scylladb-client-<hash>`)
6. **STS patching reverted by operator** — scylla-operator may reconcile the STS and remove patches. If this happens, the TLS volumes need to be re-added after each operator reconciliation. The existing `hack/stretched-local-test.sh` has this same workaround and it persists in practice
7. **Rook-Ceph PVC binding timeout** — If Rook-Ceph OSDs are not ready or the StorageClass is misconfigured, ScyllaDB PVCs will remain Pending. Verify `kubectl get sc rook-ceph-block` exists and Ceph health is OK before running this script
8. **CQL smoke test fails with "No hosts available"** — ScyllaDB may need additional time after UN status for CQL to be fully operational. Add retry logic to the CQL test
9. **XFS format fails — xfsprogs not installed** — If PVC mounting fails with an XFS-related error, `xfsprogs` is missing from the Minikube node. Fix with `minikube ssh -p <profile> -- "sudo dnf install -y xfsprogs"` on each node, then delete and re-create the PVC

### Timing and Resource Expectations

- cert-manager install: ~1 min per cluster
- scylla-operator install: ~1-2 min per cluster
- ScyllaCluster bootstrapping (east): ~3-5 min
- ScyllaCluster bootstrapping (west + join): ~3-5 min
- TLS patching + pod restart: ~3-5 min per cluster
- Multi-DC convergence: ~1-3 min after west joins
- Total: ~15-25 min for the full ScyllaDB cross-DC stack

### File Structure

```
hack/multisite/
├── setup-scylladb.sh                    # ScyllaDB setup script (NEW — this story)
├── overlays/                            # Kustomize overlays (NEW — this story)
│   ├── base/
│   │   ├── kustomization.yaml
│   │   ├── scyllacluster.yaml
│   │   ├── scylladb-tls-config.yaml
│   │   └── scylladb-tls-patch.yaml
│   ├── east/
│   │   ├── kustomization.yaml
│   │   └── scyllacluster-patch.yaml
│   └── west/
│       ├── kustomization.yaml
│       └── scyllacluster-patch.yaml
├── setup-clusters.sh                    # From Story 14.1
├── setup-rook-ceph.sh                   # From Story 14.2
├── setup-kubevirt.sh                    # From Story 14.3
├── teardown.sh                          # From Story 14.1
├── manifests/                           # From Story 14.2
│   └── storage-class-xfs.yaml           # XFS StorageClass for ScyllaDB (NEW — this story)
└── README.md                            # Updated with ScyllaDB section
```

### Testing Standards

No Go tests for this story — validation is via the multi-DC convergence smoke test in AC7. The setup script itself includes verification steps (cert-manager readiness, scylla-operator readiness, ScyllaCluster readyMembers, nodetool status, CQL cross-DC read).

### Previous Story Intelligence

**From Story 14.1 (Minikube KVM2 Cluster Provisioning):**
- Cluster profiles/contexts are `east` and `west`
- Cilium Cluster Mesh enabled with MetalLB for LoadBalancer IPs
- Each node has an extra raw block disk (`/dev/vdb`) for Rook-Ceph OSDs
- `setup-clusters.sh` establishes the `keast()`/`kwest()` helper pattern
- `--driver=kvm2`, `--nodes=4`, `--extra-disks=1`

**From Story 14.2 (Rook-Ceph Deployment):**
- StorageClass `rook-ceph-block` is available on both clusters
- CephBlockPool `mirrored-pool` has `mirroring.enabled: true, mode: image`
- Rook-Ceph uses the extra block disk (`/dev/vdb`) as OSD
- `setup-rook-ceph.sh` follows the same script conventions

**From Story 14.3 (KubeVirt Deployment):**
- KubeVirt Deployed with hardware KVM acceleration (nested virt)
- CDI deployed for DataVolume support
- Container disk + PVC-backed disk patterns validated

**From existing `hack/stretched-local-test.sh`:**
- `create_combined_ca` helper pattern — critical for mTLS
- `wait_scylladb_ready` helper — polls readyMembers count
- STS volume patching workaround for cert-manager TLS
- Seed cluster (east/etl6) deploys first, joining cluster (west/etl7) uses `externalSeeds`
- Multi-DC convergence check via `nodetool status` counting UN nodes
- Namespace is `soteria`
- ScyllaCluster name is `soteria-scylladb`
- Members per rack: 2 on real clusters → 1 for Minikube (developer mode)

### Project Structure Notes

- Overlays go in `hack/multisite/overlays/` — new directory parallel to `hack/overlays/`
- Script goes in `hack/multisite/` — follows convention from Stories 14.1, 14.2, 14.3
- Overlay structure mirrors `hack/overlays/{base,etl6,etl7}/` with `{base,east,west}` naming
- No Makefile targets needed in this story

### References

- [Source: epics.md#Story 14.5 (was 14.4)] — acceptance criteria and technical notes
- [Source: hack/stretched-local-test.sh] — existing multi-site deployment pattern (ScyllaDB + cert-manager + TLS)
- [Source: hack/overlays/] — existing Kustomize overlay structure for etl6/etl7 deployments
- [Source: project-context.md#ScyllaDB Storage Layer] — ScyllaDB configuration requirements (mTLS, NTS topology)
- [Source: architecture.md#Data Architecture] — ScyllaDB deployment via scylla-operator, mTLS, NTS DC1:2 DC2:2
- [Source: Story 14.1] — Minikube KVM2 cluster provisioning with Cilium Cluster Mesh
- [Source: Story 14.2] — Rook-Ceph deployment with StorageClass `rook-ceph-block`
- [Source: Story 14.3] — KubeVirt + CDI deployment with nested virtualization
- [Source: scylla-operator docs] — https://operator.docs.scylladb.com/stable/ (v1.21, ScyllaDB 2026.1.3)
- [Source: cert-manager docs] — https://cert-manager.io/docs/installation/helm/ (v1.20.2, OCI registry)
- [Source: Cilium global services] — https://docs.cilium.io/en/stable/network/clustermesh/services/ (`service.cilium.io/global`)
- [Source: scylla-operator releases] — https://operator.docs.scylladb.com/stable/reference/releases.html (v1.20/v1.21 supported)

## Dev Agent Record

### Agent Model Used

claude-4.6-opus

### Debug Log References

No debug issues encountered.

### Completion Notes List

- Implemented complete ScyllaDB cross-DC deployment script `setup-scylladb.sh` following the conventions of Stories 14.1-14.4 (`set -euo pipefail`, `keast()`/`kwest()` helpers, `info()`/`warn()`/`error()`/`fatal()` logging, env-var-driven config)
- Created `rook-ceph-block-xfs` StorageClass manifest cloning `rook-ceph-block` with `csi.storage.k8s.io/fstype: xfs` added for ScyllaDB XFS requirement
- Created Kustomize overlays in `hack/multisite/overlays/{base,east,west}/` mirroring the `hack/overlays/{base,etl6,etl7}/` structure with Cilium global service replacing Submariner ServiceExport, `rook-ceph-block-xfs` replacing `ontap-san-xfs`, and `cluster.local` FQDN replacing `clusterset.local`
- mTLS configured via cert-manager certificates + STS volume patching workaround identical to `hack/stretched-local-test.sh` pattern (certmanager-serving, certmanager-ca, combined-ca volumes)
- Cilium global service annotation (`service.cilium.io/global: "true"`) applied at runtime via `kubectl annotate --overwrite` after operator-created headless services exist (3.4 overlay file omitted since service is operator-managed and runtime annotation is more reliable)
- ScyllaCluster uses `developerMode: true`, 1 member per rack, 512Mi memory request, `rook-ceph-block-xfs` StorageClass
- Multi-DC convergence smoke test includes `nodetool status` UN check + CQL cross-DC replication verification (NTS keyspace write on east, read on west) with retry logic + cleanup
- Script idempotency via `helm upgrade --install`, `kubectl apply`, `--overwrite` annotations, and pre-check before STS patching
- Kustomize overlays verified rendering correctly via `kubectl kustomize` for both east and west
- All existing Go unit tests pass with zero regressions
- README updated with ScyllaDB section covering prerequisites, env vars, verification, and troubleshooting

### File List

- hack/multisite/setup-scylladb.sh (NEW)
- hack/multisite/manifests/storage-class-xfs.yaml (NEW)
- hack/multisite/overlays/base/kustomization.yaml (NEW)
- hack/multisite/overlays/base/scyllacluster.yaml (NEW)
- hack/multisite/overlays/base/scylladb-tls-config.yaml (NEW)
- hack/multisite/overlays/base/scylladb-tls-patch.yaml (NEW)
- hack/multisite/overlays/east/kustomization.yaml (NEW)
- hack/multisite/overlays/east/scyllacluster-patch.yaml (NEW)
- hack/multisite/overlays/west/kustomization.yaml (NEW)
- hack/multisite/overlays/west/scyllacluster-patch.yaml (NEW)
- hack/multisite/README.md (MODIFIED)
- _bmad-output/implementation-artifacts/14-5-scylladb-cross-dc-on-rook-ceph.md (MODIFIED)
- _bmad-output/implementation-artifacts/sprint-status.yaml (MODIFIED)

### Change Log

- 2026-06-29: Implemented Story 14.5 — ScyllaDB cross-DC deployment on Rook-Ceph with Cilium global services, cert-manager mTLS, Kustomize overlays, and convergence smoke test

### Review Findings

- [x] [Review][Patch] Missing `jq` prerequisite check [`hack/multisite/setup-scylladb.sh`:84]
- [x] [Review][Patch] CA issuer readiness loop never fails when `soteria-ca` stays unready [`hack/multisite/setup-scylladb.sh`:184]
- [x] [Review][Patch] Cluster Mesh prerequisite is never validated before deployment starts [`hack/multisite/setup-scylladb.sh`:82]
- [x] [Review][Patch] StatefulSet TLS patching is not fully idempotent after partial success or drift [`hack/multisite/setup-scylladb.sh`:350]
- [x] [Review][Patch] Smoke test only warns on failed convergence or failed cross-DC replication [`hack/multisite/setup-scylladb.sh`:454]
