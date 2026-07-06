# Multisite Minikube KVM2 Cluster Environment

Two-cluster Minikube KVM2 environment with Cilium Cluster Mesh for cross-cluster
networking. Provides the foundational multi-site topology for Soteria
integration testing (Epic 14).

## Architecture

```
┌─────────────────────┐         ┌─────────────────────┐
│   Minikube: east    │         │   Minikube: west    │
│   cluster.id=1      │◄───────►│   cluster.id=2      │
│                     │  Cilium  │                     │
│  1 control-plane    │  Cluster │  1 control-plane    │
│  3 workers (Rook)   │   Mesh   │  3 workers (Rook)   │
│                     │ NodePort │                     │
│  Pod: 10.10.0.0/16  │         │  Pod: 10.20.0.0/16  │
│  Svc: 10.96.0.0/16  │         │  Svc: 10.97.0.0/16  │
└─────────────────────┘         └─────────────────────┘

Each worker node has an extra raw block disk (/dev/vdb) for Rook-Ceph OSDs.
```

## Prerequisites

| Tool | Minimum Version | Install |
|------|----------------|---------|
| `minikube` | v1.32+ | Auto-downloaded if missing (or install: https://minikube.sigs.k8s.io/) |
| `helm` | v3.12+ | https://helm.sh/docs/intro/install/ |
| `cilium` CLI | v0.15.0+ | Auto-downloaded if missing (or: https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/#install-the-cilium-cli) |
| `kubectl` | v1.28+ | https://kubernetes.io/docs/tasks/tools/ |
| `libvirt` + KVM | — | `sudo dnf install libvirt qemu-kvm virt-install` |

**Auto-downloaded binaries:** If `minikube` or `cilium` CLI is not on your
`$PATH`, the setup script downloads them into `hack/multisite/.bin/` (gitignored).

**Resource requirements:** Running 8 KVM VMs (2 control-planes + 6 workers)
requires approximately:
- **vCPU**: 16 cores (2 per VM)
- **RAM**: 32 GB (4 GB per VM)
- **Disk**: ~340 GB (mostly thin-provisioned by libvirt)

## Usage

### Create clusters

```bash
./hack/multisite/setup-clusters.sh
```

The script is idempotent — safe to re-run if interrupted.

### Tear down

```bash
./hack/multisite/teardown.sh
```

Tolerant of missing clusters — safe to run even if setup was incomplete.

### Access clusters

After setup, use standard kubectl contexts:

```bash
kubectl --context east get nodes
kubectl --context west get nodes
```

Or use the exported kubeconfigs:

```bash
KUBECONFIG=./hack/multisite/.kubeconfigs/east.kubeconfig kubectl get nodes
KUBECONFIG=./hack/multisite/.kubeconfigs/west.kubeconfig kubectl get nodes
```

### Verify Cluster Mesh

```bash
cilium clustermesh status --context east
cilium clustermesh status --context west
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EAST_CLUSTER_NAME` | `east` | Name of the east Minikube profile |
| `WEST_CLUSTER_NAME` | `west` | Name of the west Minikube profile |
| `CILIUM_VERSION` | *(latest)* | Cilium Helm chart version to install |
| `NODE_CPUS` | `2` | vCPUs per node |
| `NODE_MEMORY` | `4096` | Memory per node in MB |
| `NODE_DISK_SIZE` | `20g` | System disk size per node |
| `OSD_DISK_SIZE` | `30g` | Extra block disk size for Rook OSDs |
| `KUBECONFIG_DIR` | `hack/multisite/.kubeconfigs` | Directory for exported kubeconfigs |
| `CONNECTIVITY_TEST` | `1` | Set to `0` to skip the cross-cluster connectivity smoke test |

## File Structure

```
hack/multisite/
├── setup-clusters.sh                   # Creates Minikube VMs + Cilium (Helm) + Cluster Mesh
├── setup-rook-ceph.sh                  # Deploys Rook-Ceph + RBD mirroring + CSI Addons
├── setup-kubevirt.sh                   # Deploys KubeVirt + CDI + virtctl
├── validate-fedora-vm.sh              # Fedora VM validation + node sizing report
├── setup-scylladb.sh                   # Deploys ScyllaDB cross-DC + cert-manager + mTLS
├── deploy-soteria.sh                   # Builds + deploys Soteria operator (Story 15.8)
├── fix-ceph-osd-auth.sh               # Recovers Ceph OSDs after mon database reset
├── teardown.sh                         # Deletes clusters + cleans kubeconfigs
├── manifests/
│   ├── cilium/
│   │   └── values.yaml                 # Cilium Helm chart values
│   ├── rook-ceph/
│   │   ├── operator-values.yaml        # Rook operator Helm values
│   │   ├── ceph-cluster.yaml           # CephCluster CR (deviceFilter: ^vdb$)
│   │   ├── ceph-blockpool.yaml         # CephBlockPool with image-mode mirroring
│   │   ├── ceph-rbd-mirror.yaml        # CephRBDMirror daemon CR
│   │   ├── peer-secret.yaml            # Template: bootstrap peer secret
│   │   ├── volume-replication-class.yaml  # VolumeReplicationClass for Soteria
│   │   ├── storage-class.yaml          # StorageClass rook-ceph-block
│   │   ├── test-pvc.yaml               # Template: smoke test PVC
│   │   └── test-volume-replication.yaml # Template: smoke test VolumeReplication
│   └── storage-class-xfs.yaml          # XFS StorageClass for ScyllaDB (rook-ceph-block-xfs)
├── overlays/
│   ├── base/                           # ScyllaDB shared resources (used by setup-scylladb.sh)
│   │   ├── kustomization.yaml          # ScyllaDB resources + TLS patches
│   │   ├── scyllacluster.yaml          # Base ScyllaCluster CR
│   │   ├── scylladb-tls-config.yaml    # ScyllaDB mTLS scylla.yaml ConfigMap
│   │   ├── scylladb-tls-patch.yaml     # ScyllaCluster TLS volumes patch
│   │   ├── manager-args-patch.yaml     # Soteria: ScyllaDB/APIServer args
│   │   ├── manager-scylladb-patch.yaml # Soteria: TLS volume mounts
│   │   ├── apiserver-rbac.yaml         # Soteria: auth-delegation RBAC
│   │   └── apiserver-cert.yaml         # Soteria: APIServer TLS Certificate
│   ├── east/
│   │   ├── kustomization.yaml          # ScyllaDB: DC = east (seed)
│   │   ├── scyllacluster-patch.yaml    # datacenter.name: east
│   │   └── manager-dc-patch.yaml       # Soteria: --site-name=east
│   ├── west/
│   │   ├── kustomization.yaml          # ScyllaDB: DC = west (joining)
│   │   ├── scyllacluster-patch.yaml    # datacenter.name: west + externalSeeds
│   │   └── manager-dc-patch.yaml       # Soteria: --site-name=west
│   └── soteria/                        # Soteria operator overlays (used by deploy-soteria.sh)
│       ├── base/
│       │   └── kustomization.yaml      # config/default + patches + RBAC + cert
│       ├── east/
│       │   └── kustomization.yaml      # Inherits soteria/base, adds east DC args
│       └── west/
│           └── kustomization.yaml      # Inherits soteria/base, adds west DC args
├── .bin/                               # Auto-downloaded tool binaries (gitignored)
├── .kubeconfigs/                       # Exported kubeconfigs (gitignored)
└── README.md                           # This file
```

## Troubleshooting

### Cluster Mesh fails to connect

Cluster Mesh uses NodePort. Both Minikube profiles share the same libvirt
network, so node IPs should be routable:

```bash
# Check NodePort services
kubectl --context east -n kube-system get svc clustermesh-apiserver
kubectl --context west -n kube-system get svc clustermesh-apiserver
```

### Cilium agents not ready

If Cilium pods are stuck in CrashLoopBackOff, verify CNI is disabled in Minikube:

```bash
minikube ssh -p east -- ls /etc/cni/net.d/
# Should only show Cilium-managed configs
```

### Ceph OSDs down after cluster restart

If Ceph OSDs are "down" after a `minikube stop/start` cycle, it may indicate
the mon database was reset and OSD auth keys no longer match. Run:

```bash
./hack/multisite/fix-ceph-osd-auth.sh east
./hack/multisite/fix-ceph-osd-auth.sh west
```

The `dataDirHostPath` is set to `/data/rook` (backed by persistent `/dev/vda1`)
to prevent this. If you see this issue, check that `/data/rook` still exists:

```bash
minikube ssh -p east -- ls -la /data/rook/
```

### Extra disk not visible

Verify the extra disk appears on worker nodes:

```bash
minikube ssh -p east -n east-m02 -- lsblk
# Should show vdb (30G disk)
```

### Connectivity test fails

Cross-cluster ping requires Cilium Cluster Mesh to be fully converged.
If the smoke test fails immediately after setup, wait 30 seconds and re-run:

```bash
CONNECTIVITY_TEST=1 ./hack/multisite/setup-clusters.sh
```

## Rook-Ceph with RBD Mirroring (Story 14.2)

After clusters are created, deploy Rook-Ceph with RBD volume replication:

```bash
./hack/multisite/setup-rook-ceph.sh
```

This deploys:
- Rook operator (Helm) on both clusters
- CephCluster with direct device discovery (`/dev/vdb` on each worker)
- CephBlockPool with image-level RBD mirroring
- Bidirectional bootstrap peer exchange between east and west
- CephRBDMirror daemons for asynchronous replication
- CSI Addons controller (VolumeReplication/VolumeGroupReplication CRDs)
- VolumeReplicationClasses `rook-ceph-rbd-vrc-snapshot` (snapshot-mode, 1m schedule) and `rook-ceph-rbd-vrc-journal` (journal-mode)
- StorageClass `rook-ceph-block` with `exclusive-lock` feature

### Rook-Ceph Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EAST_CLUSTER_NAME` | `east` | Name of the east Minikube profile |
| `WEST_CLUSTER_NAME` | `west` | Name of the west Minikube profile |
| `ROOK_CHART_VERSION` | *(latest)* | Rook Helm chart version |
| `SMOKE_TEST` | `1` | Set to `0` to skip the replication smoke test |
| `CSI_ADDONS_TAG` | `v0.14.0` | CSI Addons release tag for reproducible installs |

### Verify Rook-Ceph

```bash
# Ceph cluster health
kubectl --context east -n rook-ceph get cephcluster
kubectl --context west -n rook-ceph get cephcluster

# RBD mirror status
kubectl --context east -n rook-ceph get cephblockpool mirrored-pool -o yaml

# VolumeReplicationClasses
kubectl --context east get volumereplicationclass rook-ceph-rbd-vrc-snapshot rook-ceph-rbd-vrc-journal

# StorageClass
kubectl --context east get storageclass rook-ceph-block
```

### Teardown Rook-Ceph

```bash
./hack/multisite/setup-rook-ceph.sh teardown
```

### Rook-Ceph Troubleshooting

**OSD pods in CrashLoopBackOff:** Verify the extra disk is available inside the VM:
```bash
minikube ssh -p east -n east-m02 -- lsblk /dev/vdb
```

**Bootstrap peer secret not created:** Check CephBlockPool status:
```bash
kubectl --context east -n rook-ceph get cephblockpool mirrored-pool -o yaml
```

**VolumeReplication stuck in Unknown:** Check CSI Addons controller logs:
```bash
kubectl --context east -n csi-addons-system logs deployment/csi-addons-controller-manager --tail=50
```

## KubeVirt with CDI (Story 14.3)

After clusters and Rook-Ceph are deployed, install KubeVirt with hardware-accelerated
KVM and CDI (Containerized Data Importer):

```bash
./hack/multisite/setup-kubevirt.sh
```

This deploys:
- KubeVirt operator and CR with hardware KVM acceleration (no emulation fallback)
- CDI operator and CR for DataVolume and StorageProfile support
- `virtctl` binary in `hack/multisite/.bin/`
- Validates via container disk and PVC-backed disk smoke tests

### Prerequisites

- Minikube KVM2 clusters running (Story 14.1)
- Rook-Ceph deployed with `rook-ceph-block` StorageClass (Story 14.2)
- **Nested virtualization** enabled on the host:
  - Intel: `cat /sys/module/kvm_intel/parameters/nested` → `Y`
  - AMD: `cat /sys/module/kvm_amd/parameters/nested` → `1`

### KubeVirt Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EAST_CLUSTER_NAME` | `east` | Name of the east Minikube profile |
| `WEST_CLUSTER_NAME` | `west` | Name of the west Minikube profile |
| `KUBEVIRT_VERSION` | *(latest stable)* | KubeVirt release to deploy |
| `CDI_VERSION` | *(latest stable)* | CDI release to deploy |
| `SMOKE_TEST` | `1` | Set to `0` to skip smoke tests |

### Verify KubeVirt

```bash
# KubeVirt status
kubectl --context east -n kubevirt get kubevirt
kubectl --context west -n kubevirt get kubevirt

# KubeVirt pods
kubectl --context east -n kubevirt get pods
kubectl --context west -n kubevirt get pods

# CDI status
kubectl --context east -n cdi get cdi
kubectl --context west -n cdi get cdi

# StorageProfile
kubectl --context east get storageprofile rook-ceph-block

# virtctl
./hack/multisite/.bin/virtctl version --client
```

### KubeVirt Troubleshooting

**Nested virtualization not available:**
```bash
# Verify on the host
cat /sys/module/kvm_intel/parameters/nested  # Should be Y (Intel)
cat /sys/module/kvm_amd/parameters/nested    # Should be 1 (AMD)

# Enable it:
# Intel:
echo 'options kvm_intel nested=1' | sudo tee /etc/modprobe.d/kvm.conf
sudo modprobe -r kvm_intel && sudo modprobe kvm_intel
# AMD:
echo 'options kvm_amd nested=1' | sudo tee /etc/modprobe.d/kvm.conf
sudo modprobe -r kvm_amd && sudo modprobe kvm_amd
```

**KubeVirt stuck in Deploying phase:**
virt-handler cannot access `/dev/kvm`. Since emulation is disabled, this surfaces
as a hard failure. Verify nested virt inside the Minikube node:
```bash
minikube ssh -p east -- test -c /dev/kvm && echo "OK"
```

**virt-handler CrashLoopBackOff:**
```bash
kubectl --context east -n kubevirt logs -l kubevirt.io=virt-handler --tail=30
minikube ssh -p east -- lsmod | grep kvm
```

**CDI StorageProfile empty:**
CDI could not detect `rook-ceph-block` capabilities. Check CSI driver:
```bash
kubectl --context east get storageprofile rook-ceph-block -o yaml
```

**Container disk image pull failure:**
Verify `quay.io/kubevirt/cirros-container-disk-demo` is pullable from inside nodes:
```bash
minikube ssh -p east -- sudo crictl pull quay.io/kubevirt/cirros-container-disk-demo
```

## Fedora VM Validation & Node Sizing (Story 14.4)

After KubeVirt and CDI are deployed, validate that a Fedora VM boots successfully
with Rook-Ceph storage and calculate node sizing for the full 6-VM integration test:

```bash
./hack/multisite/validate-fedora-vm.sh
```

This validates:
- Fedora container disk image (`quay.io/containerdisks/fedora:latest`) pre-cached on all nodes
- Single Fedora VM boots with container disk + Rook-Ceph PVC data disk
- VM runs stably at 256Mi memory (integration test resource profile)
- Guest OS responsiveness (via guest agent or Running state confirmation)
- Node capacity sufficient for full 6-VM integration test
- Node sizing recommendations for `setup-clusters.sh`

### Fedora VM Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EAST_CLUSTER_NAME` | `east` | Name of the east Minikube profile |
| `WEST_CLUSTER_NAME` | `west` | Name of the west Minikube profile |
| `FEDORA_IMAGE` | `quay.io/containerdisks/fedora:latest` | Fedora container disk image |
| `CIRROS_IMAGE` | `quay.io/kubevirt/cirros-container-disk-demo` | Cirros image (also pre-cached) |
| `VM_MEMORY` | `256Mi` | Memory for the test VM |
| `VM_BOOT_TIMEOUT` | `300` | Timeout in seconds for VM to reach Running |
| `GUEST_AGENT_TIMEOUT` | `300` | Timeout in seconds for guest agent check |
| `SKIP_CLEANUP` | `0` | Set to `1` to keep test resources after validation |

### Node Sizing Recommendations

The full 6-VM integration test (Story 14.7) requires sufficient resources for
VMs, KubeVirt, Rook-Ceph, ScyllaDB, Cilium, and the Soteria operator.

**Total per-cluster requirements (3 workers, 6 VMs):**

| Category | Memory | CPU |
|----------|--------|-----|
| Node overhead (3 x 1536Mi) | 4608Mi | 900m |
| VM workloads (6 x 384Mi) | 2304Mi | 1200m |
| Shared components | ~3200Mi | ~1350m |
| **Total** | **~10.1 GiB** | **~4.5 cores** |

**Recommended node sizing:**

```bash
NODE_CPUS=4          # 4 vCPUs per node
WORKER_MEMORY=6144   # 6 GiB per worker node
MASTER_MEMORY=7168   # 7 GiB for control-plane node
```

This provides ~50% headroom over calculated minimums.

**To resize clusters:**

```bash
# 1. Tear down existing clusters
./hack/multisite/teardown.sh

# 2. Recreate with recommended sizing
NODE_CPUS=4 WORKER_MEMORY=6144 MASTER_MEMORY=7168 ./hack/multisite/setup-clusters.sh
```

### Verify Fedora VM Validation

```bash
# Check if test resources were cleaned up
kubectl --context east get vm,pvc -n default | grep fedora-validation
kubectl --context west get vm,pvc -n default | grep fedora-validation
# Should return nothing (resources cleaned up)
```

### Fedora VM Troubleshooting

**Fedora image pull timeout:**
The image is ~700MB. Pre-pulling via `crictl pull` on all nodes mitigates this:
```bash
minikube ssh -p east -- sudo crictl pull quay.io/containerdisks/fedora:latest
```

**VM OOMKilled with 256Mi:**
Fedora cloud image may need more RAM for initial boot. Try increasing:
```bash
VM_MEMORY=512Mi ./hack/multisite/validate-fedora-vm.sh
```

**PVC not binding:**
Check Rook-Ceph health:
```bash
kubectl --context east -n rook-ceph exec deploy/rook-ceph-tools -- ceph status
```

**Scheduling failure (insufficient resources):**
The capacity check will report this. Resize clusters with recommended sizing above.

**Guest agent not detected:**
Container disk images may not have `qemu-guest-agent` pre-installed.
Reaching Running state is sufficient validation that the boot + storage path works.

## ScyllaDB Cross-DC Deployment (Story 14.5)

After KubeVirt and Rook-Ceph are deployed, install ScyllaDB as a cross-DC cluster
using Rook-Ceph storage and Cilium Cluster Mesh for inter-node gossip:

```bash
./hack/multisite/setup-scylladb.sh
```

This deploys:
- cert-manager (Helm OCI, v1.20.2) on both clusters
- **Shared CA**: east generates the self-signed CA; its key-pair is copied to west
  so both clusters issue certificates from the same root (avoids cross-CA trust)
- scylla-operator (Helm, v1.21) on both clusters
- XFS-formatted StorageClass `rook-ceph-block-xfs` for ScyllaDB PVCs
- ScyllaCluster `soteria-scylladb` on east (seed DC) and west (joining DC) with
  `broadcastOptions.nodes.type: PodIP` for cross-cluster internode routing
- Cilium `service.cilium.io/global: "true"` annotation on headless services
  for cross-cluster seed discovery via normal `svc.cluster.local` FQDN
- mTLS via cert-manager certificates + StatefulSet volume patching workaround
- Combined CA trust bundle (cert-manager CA + operator internal client CA)
- Multi-DC convergence verification (nodetool status + CQL cross-DC read)

### Prerequisites

- Minikube KVM2 clusters running (Story 14.1)
- Rook-Ceph deployed with `rook-ceph-block` StorageClass (Story 14.2)
- Cilium Cluster Mesh connected between east and west (Story 14.1)

### ScyllaDB Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EAST_CLUSTER_NAME` | `east` | Name of the east Minikube profile |
| `WEST_CLUSTER_NAME` | `west` | Name of the west Minikube profile |
| `NAMESPACE` | `soteria` | Namespace for ScyllaDB deployment |
| `CERT_MANAGER_VERSION` | `v1.20.2` | cert-manager Helm chart version |
| `SCYLLA_OPERATOR_NS` | `scylla-operator` | scylla-operator namespace |
| `SMOKE_TEST` | `1` | Set to `0` to skip convergence smoke test |
| `MEMBERS_PER_RACK` | `1` | ScyllaDB members per rack (1 for dev mode) |

### Verify ScyllaDB

```bash
# ScyllaCluster status
kubectl --context east -n soteria get scyllaclusters
kubectl --context west -n soteria get scyllaclusters

# Multi-DC cluster status
kubectl --context east -n soteria exec -it <pod> -c scylla -- nodetool status

# CQL access
kubectl --context east -n soteria exec -it <pod> -c scylla -- cqlsh

# Combined CA trust bundle
kubectl --context east -n soteria get configmap scylladb-combined-ca
```

### ScyllaDB Troubleshooting

**ScyllaDB pods OOMKilled:**
Developer mode reduces but doesn't eliminate memory needs. If Minikube nodes
are resource-constrained, increase `NODE_MEMORY` and recreate clusters:
```bash
./hack/multisite/teardown.sh
NODE_CPUS=4 WORKER_MEMORY=6144 MASTER_MEMORY=7168 ./hack/multisite/setup-clusters.sh
```

**West can't discover east seeds (externalSeeds resolution fails):**
Cilium global service annotation must be applied to east's headless service
before west starts. Verify:
```bash
kubectl --context east -n soteria get svc soteria-scylladb-client -o yaml | grep cilium
cilium clustermesh status --context east
```

**Cross-DC TLS "error while extracting certificate DN strings":**
Both clusters must share the same CA for internode mTLS. The script copies east's
CA to west automatically. If you see this error, verify CAs match:
```bash
# Both fingerprints must be identical
kubectl --context east -n soteria get secret soteria-ca-key-pair -o jsonpath='{.data.ca\.crt}' | base64 -d | openssl x509 -fingerprint -noout
kubectl --context west -n soteria get secret soteria-ca-key-pair -o jsonpath='{.data.ca\.crt}' | base64 -d | openssl x509 -fingerprint -noout
```

**West gossiper stuck "not ready" (internode addresses unreachable):**
ScyllaDB must use PodIP broadcast (`exposeOptions.broadcastOptions.nodes.type: PodIP`)
so advertised addresses are routable cross-cluster via Cilium. The base overlay
includes this. If you changed it to ServiceClusterIP, per-pod ClusterIPs are
cluster-local and won't work cross-DC.

**cert-manager webhook not ready:**
cert-manager webhook takes ~30s to become ready. The script waits for the
webhook deployment rollout before creating Certificate resources.

**scylla-operator webhook not ready:**
Same timing issue. The script waits for `webhook-server` deployment rollout
before applying ScyllaCluster CRs.

**STS TLS volumes reverted by operator:**
scylla-operator may reconcile the STS and remove patches. Re-run the script
to re-apply. The patching is idempotent (checks before patching).

**PVC binding timeout (XFS):**
ScyllaDB requires XFS. If PVCs fail to mount, `xfsprogs` may be missing:
```bash
minikube ssh -p east -- "sudo dnf install -y xfsprogs"
minikube ssh -p west -- "sudo dnf install -y xfsprogs"
```

**CQL smoke test fails with "No hosts available":**
ScyllaDB may need additional time after nodetool shows UN. The script retries
CQL reads with 5s intervals.

## Soteria Operator Deployment (Story 15.8)

After all infrastructure components are deployed, install the Soteria DR orchestration
operator on both clusters:

```bash
./hack/multisite/deploy-soteria.sh
```

This deploys:
- Soteria operator image (built locally, loaded into Minikube via `minikube image load`)
- Controller-manager with aggregated API server (port 6443, TLS)
- ScyllaDB connection with mTLS (contact-points, keyspace, DC replication)
- Per-site configuration (`--site-name=east/west`, `--scylladb-local-dc=east/west`)
- APIService `v1alpha1.soteria.io` registration with cert-manager CA injection
- Cross-DC replication smoke test (DRPlan create on east → verify on west)

### Prerequisites

- Minikube KVM2 clusters running (Story 14.1)
- Rook-Ceph deployed with `rook-ceph-block` StorageClass (Story 14.2)
- KubeVirt deployed with VirtualMachine CRDs (Story 14.3)
- ScyllaDB cross-DC cluster running with mTLS + cert-manager (Story 14.5)
- `podman` (or `docker`) for image build

### Soteria Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EAST_CLUSTER_NAME` | `east` | Name of the east Minikube profile |
| `WEST_CLUSTER_NAME` | `west` | Name of the west Minikube profile |
| `NAMESPACE` | `soteria` | Namespace for Soteria deployment |
| `IMG` | `localhost/soteria:dev` | Soteria container image name:tag |
| `CONTAINER_TOOL` | `podman` | Container build tool (podman or docker) |
| `SKIP_BUILD` | `0` | Set to `1` to skip image build |
| `SKIP_LOAD` | `0` | Set to `1` to skip loading image into Minikube |
| `SMOKE_TEST` | `1` | Set to `0` to skip cross-DC replication smoke test |
| `SMOKE_TEST_PLAN_NAME` | `smoke-test-plan-<timestamp>-<pid>` | Name for the temporary DRPlan used by the smoke test |

### Verify Soteria

```bash
# APIService status
kubectl --context east get apiservice v1alpha1.soteria.io
kubectl --context west get apiservice v1alpha1.soteria.io

# Controller-manager pods
kubectl --context east -n soteria get pods -l control-plane=controller-manager
kubectl --context west -n soteria get pods -l control-plane=controller-manager

# Controller logs
kubectl --context east -n soteria logs deploy/soteria-controller-manager -c manager --tail=30
kubectl --context west -n soteria logs deploy/soteria-controller-manager -c manager --tail=30

# Create a Ceph-backed DRPlan via the Soteria API
kubectl --context east apply -f - <<'EOF'
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: verify-ceph-plan
spec:
  maxConcurrentFailovers: 2
  primarySite: east
  secondarySite: west
  volumeReplicationDriver:
    type: csi-extension
    volumeReplicationClass: rook-ceph-rbd-vrc-snapshot
EOF
kubectl --context east get drplans
```

### Soteria Troubleshooting

**Image not found (ImagePullBackOff):**
The image must be loaded into Minikube before deployment. Re-run without skipping:
```bash
SKIP_BUILD=0 SKIP_LOAD=0 ./hack/multisite/deploy-soteria.sh
```

**Controller-manager CrashLoopBackOff:**
Usually indicates ScyllaDB connection failure. Check:
```bash
kubectl --context east -n soteria logs deploy/soteria-controller-manager -c manager --tail=50
# Verify ScyllaDB is reachable
kubectl --context east -n soteria get scyllacluster soteria-scylladb
```

**APIService stays Unavailable:**
The pod isn't passing readiness probes (`/readyz` on port 8081). Check:
```bash
kubectl --context east -n soteria describe pod -l control-plane=controller-manager
kubectl --context east get apiservice v1alpha1.soteria.io -o yaml
```

**TLS certificate not issued:**
cert-manager must have the `soteria-internal` Issuer in the `soteria` namespace:
```bash
kubectl --context east -n soteria get issuer soteria-internal
kubectl --context east -n soteria get certificate
```

**Cross-DC replication timeout:**
ScyllaDB replication may be slow in developer mode. Verify multi-DC connectivity:
```bash
kubectl --context east -n soteria exec -it pod/soteria-scylladb-east-rack1-0 -c scylla -- nodetool status
```

**Kustomize build failure:**
Ensure `--load-restrictor LoadRestrictionsNone` is used (the script handles this).
Verify `config/manager/kustomization.yaml` has the correct image after `kustomize edit set image`.

### Idempotency

The deploy script is idempotent — safe to re-run at any time:
- `kubectl apply --server-side --force-conflicts` handles existing resources
- Image build and load are no-ops if unchanged
- `kustomize edit set image` overwrites the previous value
- Use `SKIP_BUILD=1 SKIP_LOAD=1` for faster re-deploys after code changes

## Downstream Dependencies

This environment is the foundation for the full Epic 14 stack:

1. **Story 14.2** — Rook-Ceph uses extra VM disks (`/dev/vdb`) for OSD storage
2. **Story 14.3** — KubeVirt deployment with hardware KVM acceleration
3. **Story 14.4** — Fedora VM validation (depends on KubeVirt + CDI)
4. **Story 14.5** — ScyllaDB cross-DC deployment
5. **Story 15.8** — Soteria operator deployment
6. **Story 15.9** — Full lifecycle integration test
