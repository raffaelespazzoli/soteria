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
├── teardown.sh                         # Deletes clusters + cleans kubeconfigs
├── manifests/
│   ├── cilium/
│   │   └── values.yaml                 # Cilium Helm chart values
│   └── rook-ceph/
│       ├── operator-values.yaml        # Rook operator Helm values
│       ├── ceph-cluster.yaml           # CephCluster CR (deviceFilter: ^vdb$)
│       ├── ceph-blockpool.yaml         # CephBlockPool with image-mode mirroring
│       ├── ceph-rbd-mirror.yaml        # CephRBDMirror daemon CR
│       ├── peer-secret.yaml            # Template: bootstrap peer secret
│       ├── volume-replication-class.yaml  # VolumeReplicationClass for Soteria
│       ├── storage-class.yaml          # StorageClass rook-ceph-block
│       ├── test-pvc.yaml               # Template: smoke test PVC
│       └── test-volume-replication.yaml # Template: smoke test VolumeReplication
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
- VolumeReplicationClass `rook-ceph-rbd-vrc` (snapshot-mode, 1m schedule)
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

# VolumeReplicationClass
kubectl --context east get volumereplicationclass rook-ceph-rbd-vrc

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

## Downstream Dependencies

This environment is the foundation for the full Epic 14 stack:

1. **Story 14.2** — Rook-Ceph uses extra VM disks (`/dev/vdb`) for OSD storage
2. **Story 14.3** — Kubernetes Dashboard deployment
3. **Story 14.4** — ScyllaDB uses Cilium global services for cross-DC gossip
4. **Story 14.5** — KubeVirt deployment with emulation fallback
5. **Story 14.6** — Soteria operator deployment
6. **Story 14.7** — Full lifecycle integration test
