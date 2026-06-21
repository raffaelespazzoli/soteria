# Multisite Kind Cluster Environment

Two-cluster Kind environment with Cilium Cluster Mesh for cross-cluster
networking. Provides the foundational multi-site topology for Soteria
integration testing (Epic 14).

## Architecture

```
┌─────────────────────┐         ┌─────────────────────┐
│   Kind: east        │         │   Kind: west        │
│   cluster.id=1      │◄───────►│   cluster.id=2      │
│                     │  Cilium  │                     │
│  1 control-plane    │  Cluster │  1 control-plane    │
│  3 workers (Rook)   │   Mesh   │  3 workers (Rook)   │
│                     │ NodePort │                     │
│  Pod: 10.1.0.0/16   │         │  Pod: 10.2.0.0/16   │
│  Svc: 10.11.0.0/16  │         │  Svc: 10.12.0.0/16  │
└─────────────────────┘         └─────────────────────┘
```

## Prerequisites

| Tool | Minimum Version | Install |
|------|----------------|---------|
| `kind` | v0.20+ | https://kind.sigs.k8s.io/docs/user/quick-start/#installation |
| `cilium` CLI | v0.15.0+ | Auto-downloaded if missing (or install manually: https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/#install-the-cilium-cli) |
| `kubectl` | v1.28+ | https://kubernetes.io/docs/tasks/tools/ |
| `podman` or `docker` | — | Container runtime must be running (podman preferred) |

**Auto-downloaded binaries:** If the `cilium` CLI is not on your `$PATH`, the
setup script downloads the latest release into `hack/multisite/.bin/` (gitignored).
The directory is reused across runs.

**Container runtime preference:** When both podman and docker are available, the
script prefers podman and exports `KIND_EXPERIMENTAL_PROVIDER=podman` automatically.

**Resource requirements:** Running 8 containers (2 control-planes + 6 workers)
requires approximately 8 GB of available RAM and 20 GB of disk space.

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
kubectl --context kind-east get nodes
kubectl --context kind-west get nodes
```

Or use the exported kubeconfigs:

```bash
KUBECONFIG=./hack/multisite/.kubeconfigs/east.kubeconfig kubectl get nodes
KUBECONFIG=./hack/multisite/.kubeconfigs/west.kubeconfig kubectl get nodes
```

### Verify Cluster Mesh

```bash
cilium clustermesh status --context kind-east
cilium clustermesh status --context kind-west
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EAST_CLUSTER_NAME` | `east` | Name of the east Kind cluster |
| `WEST_CLUSTER_NAME` | `west` | Name of the west Kind cluster |
| `CILIUM_VERSION` | *(auto)* | Cilium version to install (empty = CLI default) |
| `KUBECONFIG_DIR` | `hack/multisite/.kubeconfigs` | Directory for exported kubeconfigs |
| `CONNECTIVITY_TEST` | `1` | Set to `0` to skip the cross-cluster connectivity smoke test |

## File Structure

```
hack/multisite/
├── kind-east.yaml      # Kind cluster config (cluster.id=1, east CIDRs)
├── kind-west.yaml      # Kind cluster config (cluster.id=2, west CIDRs)
├── setup-clusters.sh   # Creates clusters + Cilium + Cluster Mesh
├── teardown.sh         # Deletes clusters + cleans kubeconfigs
├── .bin/               # Auto-downloaded tool binaries (gitignored)
├── .kubeconfigs/       # Exported kubeconfigs (gitignored)
└── README.md           # This file
```

## Troubleshooting

### Cluster Mesh fails to connect

Cluster Mesh uses NodePort on Kind. Ensure no firewall rules block
communication between the Kind Docker networks:

```bash
# Check NodePort services
kubectl --context kind-east -n kube-system get svc clustermesh-apiserver
kubectl --context kind-west -n kube-system get svc clustermesh-apiserver
```

### Cilium agents not ready

If Cilium pods are stuck in CrashLoopBackOff, verify that `disableDefaultCNI`
is set in the Kind configs:

```bash
docker exec east-control-plane cat /etc/cni/net.d/*.conflist
# Should be empty — Cilium manages CNI
```

### Rook OSD extraMounts missing

Worker nodes have `extraMounts` for Rook-Ceph OSD directories. Verify:

```bash
docker exec east-worker ls /var/lib/rook/osd0
docker exec east-worker2 ls /var/lib/rook/osd1
docker exec east-worker3 ls /var/lib/rook/osd2
```

### Connectivity test fails

Cross-cluster ping requires Cilium Cluster Mesh to be fully converged.
If the smoke test fails immediately after setup, wait 30 seconds and re-run:

```bash
CONNECTIVITY_TEST=1 ./hack/multisite/setup-clusters.sh
```

### Podman users

The setup script automatically sets `KIND_EXPERIMENTAL_PROVIDER=podman` when
podman is detected. If you need to override this (e.g., to force docker), set
the variable explicitly before running:

```bash
export KIND_EXPERIMENTAL_PROVIDER=docker
```

For rootless Podman, refer to the
[Kind documentation](https://kind.sigs.k8s.io/docs/user/rootless/).

## Downstream Dependencies

This environment is the foundation for the full Epic 14 stack:

1. **Story 14.2** — Rook-Ceph uses `extraMounts` on workers for OSD block devices
2. **Story 14.3** — Kubernetes Dashboard deployment
3. **Story 14.4** — ScyllaDB uses Cilium global services for cross-DC gossip
4. **Story 14.5** — KubeVirt deployment with emulation fallback
5. **Story 14.6** — Soteria operator deployment
6. **Story 14.7** — Full lifecycle integration test
