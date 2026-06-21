# Story 14.1: Kind Cluster Provisioning with Cilium Cluster Mesh

Status: done

## Story

As a platform engineer,
I want two Kind clusters (east and west) with Cilium Cluster Mesh providing cross-cluster networking,
so that I have the foundational multi-site topology for integration testing.

## Acceptance Criteria

**AC1: Kind cluster creation**
Given the `hack/multisite/` directory with Kind cluster configs
When `setup-clusters.sh` is executed
Then two Kind clusters named `east` and `west` are created with `disableDefaultCNI: true`
And each cluster has at least 3 worker nodes (for Rook-Ceph OSD placement)
And worker nodes have `extraMounts` for Rook-Ceph raw block OSD paths

**AC2: Cilium deployment**
Given both Kind clusters are running without a CNI
When the setup script installs Cilium
Then Cilium is deployed as the CNI on both clusters
And Cilium agents are healthy on all nodes

**AC3: Cluster Mesh enablement**
Given Cilium is running on both clusters
When the setup script enables Cluster Mesh
Then Cluster Mesh is connected between east and west
And `cilium clustermesh status` shows healthy on both clusters

**AC4: Cross-cluster connectivity smoke test**
Given Cluster Mesh is connected
When a test pod on east curls/pings a test pod on west (and vice versa)
Then cross-cluster pod-to-pod connectivity succeeds

**AC5: Idempotent setup and teardown**
Given the setup and teardown scripts
When `setup-clusters.sh` is run multiple times
Then it is idempotent (safe to re-run)
And `teardown.sh` cleanly removes both Kind clusters

**AC6: Script structure**
Given the `hack/multisite/` directory
When scripts are created
Then cluster configs live in `hack/multisite/kind-east.yaml` and `hack/multisite/kind-west.yaml`
And all scripts are CI-friendly (no hardcoded paths, configurable via env vars)

## Tasks / Subtasks

- [x] Task 1: Create `hack/multisite/` directory structure (AC: 6)
  - [x] 1.1: Create `hack/multisite/kind-east.yaml` — Kind config with `disableDefaultCNI: true`, 1 control-plane + 3 workers, `extraMounts` for Rook OSD raw block paths, unique pod/service CIDRs
  - [x] 1.2: Create `hack/multisite/kind-west.yaml` — Same structure as east with non-overlapping pod/service CIDRs
- [x] Task 2: Create `hack/multisite/setup-clusters.sh` (AC: 1, 2, 3, 4, 5)
  - [x] 2.1: Env var configuration block (cluster names, kubeconfig paths, Cilium version)
  - [x] 2.2: Prerequisite check (kind, cilium CLI, docker/podman)
  - [x] 2.3: Idempotent Kind cluster creation (check if exists before create)
  - [x] 2.4: Cilium install on both clusters with unique `cluster.id` and `cluster.name`
  - [x] 2.5: Cluster Mesh enable with `--service-type NodePort` (required for Kind)
  - [x] 2.6: Cluster Mesh connect between east and west
  - [x] 2.7: Wait for Cluster Mesh health on both clusters
  - [x] 2.8: Cross-cluster connectivity smoke test (deploy test pods, verify bidirectional)
  - [x] 2.9: Print kubeconfig access instructions on completion
- [x] Task 3: Create `hack/multisite/teardown.sh` (AC: 5)
  - [x] 3.1: Delete both Kind clusters (tolerant of missing clusters)
  - [x] 3.2: Clean up any generated kubeconfig files
- [x] Task 4: Create `hack/multisite/README.md` (AC: 6)
  - [x] 4.1: Prerequisites, usage, env var reference, troubleshooting

### Review Findings

- [x] [Review][Patch] Guard `KUBECONFIG_DIR` teardown so it only removes generated kubeconfig files instead of recursively deleting an arbitrary env-supplied directory [`hack/multisite/teardown.sh:67`]
- [x] [Review][Patch] Validate `EAST_CLUSTER_NAME` and `WEST_CLUSTER_NAME` are distinct before provisioning or meshing clusters [`hack/multisite/setup-clusters.sh:38`]
- [x] [Review][Patch] Make Podman support real instead of advisory-only, since preflight currently accepts Podman but cluster creation still assumes Kind's default provider behavior [`hack/multisite/setup-clusters.sh:73`]
- [x] [Review][Patch] Stop masking probe creation failures with `|| true`, which currently hides real `kubectl run` errors during the connectivity smoke test [`hack/multisite/setup-clusters.sh:177`]
- [x] [Review][Patch] Ensure the connectivity smoke test cleans up its temporary namespace on failure/interruption, not only on the success path [`hack/multisite/setup-clusters.sh:167`]
- [x] [Review][Patch] Add the missing disk-space preflight check required by the story's prerequisite constraints [`hack/multisite/setup-clusters.sh:66`]
- [x] [Review][Patch] Exercise the Cilium global-service path in the smoke test, not just direct pod-IP reachability, to match the story's specified downstream networking validation [`hack/multisite/setup-clusters.sh:189`]

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — shell scripts and YAML configs only, no Go code. All output goes in `hack/multisite/` (~5-6 files). This lays the foundation for the entire Epic 14 multi-site integration test stack.

### Existing Patterns to Follow

The project already has a multi-site deployment script at `hack/stretched-local-test.sh` that deploys to real OpenShift clusters (etl6/etl7) using Submariner MCS. This story creates the Kind-based equivalent with Cilium replacing Submariner.

**Key pattern translations from `hack/stretched-local-test.sh`:**

| Aspect | Existing (etl6/etl7) | This Story (east/west) |
|--------|----------------------|------------------------|
| Clusters | Real OpenShift via kubeconfig contexts | Kind clusters created by script |
| CNI/Networking | Submariner MCS | Cilium Cluster Mesh |
| Cross-cluster DNS | `*.svc.clusterset.local` | Cilium global services (normal FQDN) |
| Storage | `ontap-san-xfs` (NetApp Trident) | Rook-Ceph RBD (Story 14.2) |
| Site identity | `--site-name=etl6/etl7` | `--site-name=east/west` |

**Script conventions from `hack/stretched-local-test.sh`:**
- Env-var-driven configuration at top of script (e.g., `IMG`, `NAMESPACE`)
- `kctl()` helper wrapping kubectl with explicit `--kubeconfig`
- Preflight checks verifying cluster reachability and required CRDs
- Colored output for status messages (optional but consistent)
- Idempotent operations (check before create, tolerate NotFound on delete)

### Kind Cluster Configuration

Each cluster config YAML needs:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: true
  podSubnet: "<unique-per-cluster>"
  serviceSubnet: "<unique-per-cluster>"
nodes:
  - role: control-plane
  - role: worker
    extraMounts:
      - hostPath: /var/lib/rook-east/osd0   # or rook-west
        containerPath: /var/lib/rook/osd0
  - role: worker
    extraMounts:
      - hostPath: /var/lib/rook-east/osd1
        containerPath: /var/lib/rook/osd1
  - role: worker
    extraMounts:
      - hostPath: /var/lib/rook-east/osd2
        containerPath: /var/lib/rook/osd2
```

**Non-overlapping CIDRs (mandatory for Cluster Mesh):**
- East: `podSubnet: "10.1.0.0/16"`, `serviceSubnet: "10.11.0.0/16"`
- West: `podSubnet: "10.2.0.0/16"`, `serviceSubnet: "10.12.0.0/16"`

**Worker nodes:** 3 workers per cluster are required for Rook-Ceph OSD anti-affinity in Story 14.2. Each worker gets one `extraMounts` entry pointing to a host-local directory that will be used as a raw block device path for Rook OSD placement. Use cluster-prefixed host paths (`/var/lib/rook-east/`, `/var/lib/rook-west/`) to avoid collisions when both Kind clusters run on the same Docker host.

### Cilium Installation and Cluster Mesh

**Installation sequence (per Cilium docs for Kind):**

```bash
# Install Cilium on east (cluster.id=1)
cilium install --context kind-east \
  --set cluster.name=east \
  --set cluster.id=1

# Install Cilium on west (cluster.id=2)
cilium install --context kind-west \
  --set cluster.name=west \
  --set cluster.id=2

# Wait for Cilium readiness
cilium status --wait --context kind-east
cilium status --wait --context kind-west

# Enable Cluster Mesh (NodePort required for Kind — no cloud LB)
cilium clustermesh enable --context kind-east --service-type NodePort
cilium clustermesh enable --context kind-west --service-type NodePort

# Connect the clusters (bidirectional)
cilium clustermesh connect --context kind-east --destination-context kind-west

# Verify
cilium clustermesh status --wait --context kind-east
cilium clustermesh status --wait --context kind-west
```

**Critical: `--service-type NodePort`** — Kind clusters have no cloud load balancer, so Cluster Mesh must use NodePort for the clustermesh-apiserver. Without this, Cluster Mesh enablement will hang waiting for an external IP.

### Cross-Cluster Connectivity Smoke Test

Deploy a simple test pod (e.g., nginx or busybox) on each cluster in a known namespace with a Cilium global service annotation. Verify bidirectional pod-to-pod connectivity via `kubectl exec` curl/wget. Clean up test resources after verification.

The Cilium global service annotation is `io.cilium/global-service: "true"` (replaces Submariner's ServiceExport). This is the mechanism Story 14.4 will use for ScyllaDB cross-DC gossip.

### Idempotency Requirements

- `setup-clusters.sh` must check if clusters already exist (`kind get clusters | grep -q east`) before creating
- Cilium install should use `cilium install` which is naturally idempotent (upgrades if already installed)
- Cluster Mesh connect should tolerate already-connected state
- `teardown.sh` must tolerate missing clusters (`kind delete cluster --name east 2>/dev/null || true`)

### Prerequisite Checks

The setup script must verify at minimum:
- `kind` CLI available
- `cilium` CLI available (v0.15.0+)
- `kubectl` available
- `docker` (or `podman`) running (Kind backend)
- Sufficient disk space for 8 containers (2 control-planes + 6 workers)

### Downstream Dependencies

This story is the foundation for the entire epic. Other stories depend on it:
- **14.2** (Rook-Ceph) needs the `extraMounts` on workers for OSD block devices
- **14.3** (Dashboard) needs clusters running
- **14.4** (ScyllaDB) needs Cilium Cluster Mesh for cross-DC gossip via global services
- **14.5** (KubeVirt) needs clusters running with storage
- **14.6** (Soteria) needs all of the above
- **14.7** (Lifecycle test) needs everything

### File Structure

```
hack/multisite/
├── kind-east.yaml          # Kind cluster config for east (cluster.id=1)
├── kind-west.yaml          # Kind cluster config for west (cluster.id=2)
├── setup-clusters.sh       # Creates Kind clusters + installs Cilium + Cluster Mesh
├── teardown.sh             # Deletes both Kind clusters
└── README.md               # Prerequisites, usage, env vars, troubleshooting
```

### Project Structure Notes

- All files go in `hack/multisite/` — this is a new directory, no conflicts with existing structure
- Follows the `hack/` convention established by `hack/stretched-local-test.sh` and `hack/overlays/`
- Scripts should be executable (`chmod +x`) with bash shebangs
- No Makefile targets needed in this story (future stories may add `make multisite-up`/`make multisite-down`)

### Testing Standards

No Go tests for this story — validation is via the smoke test in AC4 (cross-cluster connectivity). The setup script itself includes verification steps (`cilium status --wait`, `cilium clustermesh status --wait`, connectivity test).

### References

- [Source: epics.md#Story 14.1] — acceptance criteria and technical notes
- [Source: hack/stretched-local-test.sh] — existing multi-site deployment pattern (Submariner-based)
- [Source: hack/overlays/] — existing Kustomize overlay structure for site-specific patches
- [Source: Cilium Kind docs] — https://docs.cilium.io/en/stable/installation/kind/#cluster-mesh
- [Source: project-context.md] — container tool is `podman` (but Kind uses Docker by default — document both)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Fixed pre-existing integration test failure (TestDRExecutionReconciler_SiteAware_OnlyTargetOwns) before starting story — test was missing WaveExecutor on its targetReconciler, causing infinite loop in resume path with 0-VM plan

### Completion Notes List

- Created `hack/multisite/` directory with 5 files (2 Kind configs, 2 scripts, 1 README)
- Kind configs use non-overlapping CIDRs (east: 10.1.0.0/16 + 10.11.0.0/16, west: 10.2.0.0/16 + 10.12.0.0/16)
- Each cluster has 1 control-plane + 3 workers with `extraMounts` for Rook-Ceph OSDs (cluster-prefixed host paths)
- Setup script follows `hack/stretched-local-test.sh` conventions (env-var config, colored output, idempotent operations)
- Cilium installed with unique `cluster.id`/`cluster.name` per cluster
- Cluster Mesh uses `--service-type NodePort` (required for Kind — no cloud LB)
- Connectivity smoke test deploys busybox probes and verifies bidirectional pod-to-pod ping
- Teardown tolerates missing clusters (`kind get clusters | grep -qx` before delete)
- Kubeconfigs exported to `.kubeconfigs/` directory (gitignored by convention)
- README documents prerequisites, usage, env vars, troubleshooting, and downstream dependencies
- No Go tests (as specified in Dev Notes — validation is via AC4 smoke test built into setup script)

### File List

- hack/multisite/kind-east.yaml (new)
- hack/multisite/kind-west.yaml (new)
- hack/multisite/setup-clusters.sh (new)
- hack/multisite/teardown.sh (new)
- hack/multisite/README.md (new)

### Change Log

- 2026-06-21: Story implemented — all 4 tasks complete, 5 new files in hack/multisite/
