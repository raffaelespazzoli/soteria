# Story 14.5: KubeVirt Deployment

Status: ready-for-dev

## Story

As a platform engineer,
I want KubeVirt deployed on both Kind clusters with Rook-Ceph as the VM storage backend,
so that virtual machines can be created with PVC-backed disks on real replicated storage.

## Acceptance Criteria

**AC1: KubeVirt operator deployment**
Given both Kind clusters with Rook-Ceph running
When the KubeVirt setup script is executed
Then the KubeVirt operator and CR are deployed on both clusters following the KubeVirt Kind quickstart
And the operator version is fetched dynamically from the stable.txt endpoint

**AC2: Emulation fallback**
Given KubeVirt is deployed
When `/dev/kvm` is not available on the host
Then KubeVirt emulation mode is enabled (`useEmulation: true`)
And VMs can still run (with reduced performance)

**AC3: KubeVirt health**
Given KubeVirt is deployed
When checking KubeVirt status
Then KubeVirt phase is `Deployed` on both clusters
And all KubeVirt pods are running (virt-operator, virt-api, virt-controller, virt-handler)

**AC4: Container disk smoke test**
Given KubeVirt is deployed
When a minimal test VMI with a container disk (e.g., `quay.io/kubevirt/cirros-container-disk-demo`) is created
Then the VMI starts and reaches `Running` state

**AC5: PVC-backed disk smoke test**
Given KubeVirt and Rook-Ceph are running
When a test VM with a Rook-Ceph PVC-backed disk is created
Then the PVC binds on the Rook-Ceph StorageClass
And the VM starts and reaches `Running` state

**AC6: virtctl installation**
Given KubeVirt is deployed
When the setup script completes
Then `virtctl` is available for VM console access and lifecycle operations

## Tasks / Subtasks

- [ ] Task 1: Create `hack/multisite/setup-kubevirt.sh` (AC: 1, 2, 3, 6)
  - [ ] 1.1: Env-var configuration block (KUBEVIRT_VERSION, cluster contexts, namespace, consistent with other scripts)
  - [ ] 1.2: Prerequisite checks (kubectl, kind clusters running, Rook-Ceph StorageClass `rook-ceph-block` available)
  - [ ] 1.3: Fetch latest stable KubeVirt version via `curl -s https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt` (or use pinned version from env var)
  - [ ] 1.4: Deploy KubeVirt operator on both clusters (`kubectl create -f kubevirt-operator.yaml`)
  - [ ] 1.5: Deploy KubeVirt CR on both clusters (`kubectl create -f kubevirt-cr.yaml`)
  - [ ] 1.6: Detect `/dev/kvm` availability on Kind worker nodes (`docker exec` check)
  - [ ] 1.7: If `/dev/kvm` not available, patch KubeVirt CR with `useEmulation: true` on both clusters
  - [ ] 1.8: Wait for KubeVirt phase to reach `Deployed` on both clusters (`kubectl -n kubevirt get kubevirt kubevirt -o jsonpath='{.status.phase}'`)
  - [ ] 1.9: Wait for all KubeVirt pods to be Running (virt-operator, virt-api, virt-controller, virt-handler)

- [ ] Task 2: Install virtctl (AC: 6)
  - [ ] 2.1: Download virtctl binary matching the deployed KubeVirt version from GitHub releases
  - [ ] 2.2: Install to a local bin directory (e.g., `./hack/multisite/bin/virtctl`)
  - [ ] 2.3: Verify `virtctl version` succeeds against both clusters

- [ ] Task 3: Container disk smoke test (AC: 4)
  - [ ] 3.1: Create a test VMI on east using `quay.io/kubevirt/cirros-container-disk-demo` container disk
  - [ ] 3.2: Wait for VMI to reach `Running` phase (poll `kubectl get vmi`)
  - [ ] 3.3: Delete the test VMI after verification
  - [ ] 3.4: Repeat on west cluster

- [ ] Task 4: PVC-backed disk smoke test (AC: 5)
  - [ ] 4.1: Create a test PVC on east with StorageClass `rook-ceph-block` (small: 1Gi)
  - [ ] 4.2: Create a test VM on east with the PVC-backed disk + container disk for boot
  - [ ] 4.3: Wait for PVC to bind and VM to reach `Running` state
  - [ ] 4.4: Delete test VM and PVC after verification
  - [ ] 4.5: Repeat on west cluster

- [ ] Task 5: README and finalization
  - [ ] 5.1: Update `hack/multisite/README.md` with KubeVirt section (prerequisites, usage, troubleshooting)
  - [ ] 5.2: Add idempotency checks throughout script (`kubectl get kubevirt -n kubevirt` before create)

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — a shell script and a README update. No Go code. The outputs are:
- `hack/multisite/setup-kubevirt.sh` — main setup script
- `hack/multisite/README.md` — updated with KubeVirt section

KubeVirt provides VM execution on Kubernetes. In this project, VMs are the workloads protected by Soteria's DR orchestration. The PVC-backed disks attached to KubeVirt VMs are what get volume-replicated via Rook-Ceph RBD mirroring.

### Critical: KubeVirt Version

Latest stable release: **v1.8.4** (released 2026-06-16). The script should dynamically fetch the latest stable version:

```bash
KUBEVIRT_VERSION="${KUBEVIRT_VERSION:-$(curl -s https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)}"
```

Operator and CR manifests:
```bash
kubectl create -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml"
kubectl create -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-cr.yaml"
```

For idempotency, use `kubectl apply --server-side` instead of `kubectl create` (create fails if resources exist).

### Critical: Emulation Mode for Kind

Kind clusters typically run inside Docker/Podman containers. The host's `/dev/kvm` is only available if:
1. The host has hardware virtualization (VT-x/AMD-V) enabled
2. Kind is configured to pass through `/dev/kvm` to containers

Detection logic:
```bash
check_kvm_available() {
  local worker_node="$1"
  docker exec "${worker_node}" test -c /dev/kvm 2>/dev/null
}
```

If KVM is not available on any worker node, enable emulation:
```bash
kubectl -n kubevirt patch kubevirt kubevirt --type=merge \
  --patch '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'
```

**Important:** The emulation patch must be applied AFTER the KubeVirt CR is created but BEFORE waiting for Deployed phase. KubeVirt will not proceed to Deployed if virt-handler can't find `/dev/kvm` unless emulation is enabled.

**Note on `useEmulation: true` behavior (v1.8+):** When enabled, software emulation via QEMU is used only as a *fallback* when hardware `/dev/kvm` is not available. Hardware acceleration is always attempted first regardless of the setting. This means the patch is safe to apply unconditionally in Kind environments.

### Critical: Wait for KubeVirt Deployed Phase

The KubeVirt CR reports its lifecycle phase in `.status.phase`. Wait for `Deployed`:

```bash
wait_kubevirt_deployed() {
  local ctx="$1"
  echo "  Waiting for KubeVirt to reach Deployed phase on ${ctx}..."
  for i in $(seq 1 60); do
    phase=$(kubectl --context="${ctx}" -n kubevirt get kubevirt kubevirt \
      -o jsonpath='{.status.phase}' 2>/dev/null) || true
    if [[ "${phase}" == "Deployed" ]]; then
      echo "  KubeVirt Deployed on ${ctx}"
      return 0
    fi
    sleep 5
  done
  echo "ERROR: KubeVirt did not reach Deployed phase on ${ctx} within 5 minutes"
  return 1
}
```

### Critical: Container Disk Smoke Test VMI

Container disks are ephemeral — they don't require PVCs. Used for quick health verification:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachineInstance
metadata:
  name: smoke-test-vmi
  namespace: default
spec:
  domain:
    resources:
      requests:
        memory: 128Mi
    devices:
      disks:
        - name: containerdisk
          disk:
            bus: virtio
  volumes:
    - name: containerdisk
      containerDisk:
        image: quay.io/kubevirt/cirros-container-disk-demo
```

Wait for Running state:
```bash
kubectl --context="${ctx}" wait vmi smoke-test-vmi \
  --for=jsonpath='{.status.phase}'=Running --timeout=120s
```

### Critical: PVC-Backed Disk Smoke Test VM

This validates the Rook-Ceph + KubeVirt integration (the critical path for DR):

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: smoke-test-pvc
  namespace: default
spec:
  accessModes: ["ReadWriteOnce"]
  storageClassName: rook-ceph-block
  resources:
    requests:
      storage: 1Gi
---
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: smoke-test-vm
  namespace: default
spec:
  runStrategy: Always
  template:
    spec:
      domain:
        resources:
          requests:
            memory: 128Mi
        devices:
          disks:
            - name: containerdisk
              disk:
                bus: virtio
            - name: datadisk
              disk:
                bus: virtio
      volumes:
        - name: containerdisk
          containerDisk:
            image: quay.io/kubevirt/cirros-container-disk-demo
        - name: datadisk
          persistentVolumeClaim:
            claimName: smoke-test-pvc
```

The VM uses a container disk for booting and a Rook-Ceph PVC for data. This mirrors the pattern in Story 14.7 where test VMs have container disk boot + Rook-Ceph PVC data disk.

### Critical: virtctl Installation

Download the matching binary:

```bash
install_virtctl() {
  local version="$1"
  local arch
  arch="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/')"
  local url="https://github.com/kubevirt/kubevirt/releases/download/${version}/virtctl-${version}-${arch}"
  local target="./hack/multisite/bin/virtctl"
  
  mkdir -p "$(dirname "${target}")"
  curl -L -o "${target}" "${url}"
  chmod +x "${target}"
  echo "  virtctl ${version} installed to ${target}"
}
```

Alternatively, if `krew` is available: `kubectl krew install virt` (creates `kubectl virt` alias).

The script should prefer the binary download (no krew dependency) and add it to PATH or print instructions.

### Dependencies

| Dependency | Story | What's Needed |
|------------|-------|---------------|
| Kind clusters | 14.1 | Both `east` and `west` clusters running |
| Rook-Ceph | 14.2 | StorageClass `rook-ceph-block` available (for AC5 smoke test) |

This story does NOT depend on 14.3 (Dashboard) or 14.4 (ScyllaDB).

### Deployment Sequence

1. Verify prerequisites (Kind clusters running, Rook-Ceph StorageClass available)
2. Deploy KubeVirt operator on both clusters (parallel or sequential)
3. Deploy KubeVirt CR on both clusters
4. Detect KVM availability on Kind worker nodes
5. If KVM unavailable, patch KubeVirt with `useEmulation: true` on both clusters
6. Wait for KubeVirt `Deployed` phase on both clusters
7. Verify all KubeVirt pods Running (virt-operator, virt-api, virt-controller, virt-handler)
8. Install virtctl binary
9. Run container disk smoke test on both clusters
10. Run PVC-backed disk smoke test on both clusters
11. Clean up smoke test resources

### Script Conventions to Follow

Follow the same conventions from Stories 14.1/14.2/14.3/14.4:

- `set -euo pipefail` at top
- Env-var-driven configuration block at top of script
- `kctl_east()` / `kctl_west()` helpers with explicit `--context`
- Consistent cluster naming: `east` and `west` (contexts `kind-east` and `kind-west`)
- Idempotent operations (`kubectl apply --server-side`, check-before-create)
- Status messages for each step
- Prerequisite checks at script start

### Kubeconfig / Context Pattern

```bash
EAST_CONTEXT="${EAST_CONTEXT:-kind-east}"
WEST_CONTEXT="${WEST_CONTEXT:-kind-west}"

kctl_east() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kctl_west() { kubectl --context "${WEST_CONTEXT}" "$@"; }
```

### Idempotency

- `kubectl apply --server-side --force-conflicts` for operator and CR YAMLs
- Emulation patch: `--type=merge` is naturally idempotent
- Smoke tests: create → verify → delete (clean up after themselves)
- virtctl download: skip if already exists with correct version
- Script is safe to re-run without side effects

### Downstream Impact

Story 14.6 (Soteria Operator Deployment) depends on KubeVirt being deployed because:
- Soteria watches `kubevirt.io/v1` VirtualMachine resources for DR plan composition
- The VM webhook validates VM label changes against DRPlan

Story 14.7 (Lifecycle Test) creates VMs with the same pattern validated here:
- Container disk for boot (lightweight OS image)
- Rook-Ceph PVC for data (volume-replicated across sites)
- Labels: `soteria.io/drplan` and `soteria.io/wave`

The VM pattern in `hack/stretched-local-test.sh` uses DataVolumes with `sourceRef` pointing to `openshift-virtualization-os-images` — this won't be available in Kind. Story 14.7 must use container disks for boot instead.

### Potential Failure Modes

1. **KubeVirt stuck in `Deploying` phase** — Usually means virt-handler can't start because `/dev/kvm` is missing and emulation is not enabled. Apply the emulation patch before waiting.
2. **virt-handler pods CrashLoopBackOff** — Check node capabilities. In Kind, worker containers may not have the kernel modules loaded. Emulation mode bypasses this.
3. **Container disk pull failure** — `quay.io/kubevirt/cirros-container-disk-demo` needs to be pullable from inside Kind nodes. Kind clusters have internet access by default, but proxy configurations may block quay.io.
4. **PVC not binding** — Rook-Ceph StorageClass `rook-ceph-block` must exist and Ceph health must be OK. Verify via `kubectl get sc` and `kubectl -n rook-ceph get cephcluster`.
5. **VMI stays in Scheduling/Pending** — Resource constraints. KubeVirt pods + smoke test VMs may exceed Kind worker capacity. Reduce VM memory requests for smoke tests (128Mi is minimal for cirros).
6. **virtctl download fails** — GitHub release asset URL depends on architecture detection. Test with `uname -m` returning `x86_64` (mapped to `amd64`) or `aarch64` (mapped to `arm64`).

### Timing and Resource Expectations

- KubeVirt operator deploy: ~1-2 min (download + apply)
- KubeVirt CR + reach Deployed: ~2-5 min
- Emulation patch propagation: ~30s
- Container disk smoke test: ~1-2 min (pull image + start VMI)
- PVC-backed disk smoke test: ~2-3 min (PVC bind + start VM)
- Total: ~8-15 min for the full KubeVirt stack on both clusters

### File Structure

```
hack/multisite/
├── setup-kubevirt.sh                    # KubeVirt setup script (NEW — this story)
├── bin/                                 # Local tool binaries (NEW — virtctl lives here)
│   └── virtctl                          # Downloaded KubeVirt CLI
├── setup-clusters.sh                    # From Story 14.1
├── setup-rook-ceph.sh                   # From Story 14.2
├── setup-dashboard.sh                   # From Story 14.3
├── setup-scylladb.sh                    # From Story 14.4
├── teardown.sh                          # From Story 14.1
├── manifests/                           # From Story 14.2
├── overlays/                            # From Story 14.4
└── README.md                            # Updated with KubeVirt section
```

### Testing Standards

No Go tests for this story — validation is via the smoke tests in AC4 and AC5. The setup script itself includes verification steps (KubeVirt Deployed phase, pod health, VMI Running state, PVC bind + VM Running state).

### Previous Story Intelligence

**From Story 14.1 (Kind Cluster Provisioning):**
- Cluster contexts are `kind-east` and `kind-west`
- Worker nodes are Docker containers — `/dev/kvm` may not be available
- `kctl_east()`/`kctl_west()` helper pattern established

**From Story 14.2 (Rook-Ceph Deployment):**
- StorageClass `rook-ceph-block` available on both clusters
- PVCs bind on this StorageClass
- Loop-device OSDs inside Kind worker containers

**From Story 14.3 (Dashboard Deployment):**
- `helm upgrade --install` pattern for idempotency
- Script structure conventions (set -euo pipefail, status messages, prerequisites)

**From Story 14.4 (ScyllaDB Deployment):**
- `EAST_CONTEXT`/`WEST_CONTEXT` env vars, `kctl_east()`/`kctl_west()` helpers
- Prerequisite checks pattern (StorageClass probe, Cilium status)
- Sequential operations where ordering matters (seed first, then join)
- Timeout/wait loop pattern with counter and sleep

**From existing `hack/stretched-local-test.sh`:**
- VMs use `kubevirt.io/v1` API version
- VMs have `runStrategy: Always` (active site) or `runStrategy: Halted` (passive site)
- VM labels: `soteria.io/drplan: fedora-app`, `soteria.io/wave: "<N>"`
- Disk structure: `dataVolumeTemplates` with `sourceRef` for boot + PVC storage (OpenShift-specific, won't work in Kind)
- Kind alternative: use `containerDisk` for boot + direct PVC for data

### Project Structure Notes

- Script goes in `hack/multisite/` — follows convention from Stories 14.1-14.4
- `bin/` subdirectory for downloaded tools (virtctl) — keeps tools co-located with scripts
- No Makefile targets needed in this story
- No Kustomize overlays needed (KubeVirt uses direct YAML manifests)

### References

- [Source: epics.md#Story 14.5] — acceptance criteria and technical notes
- [Source: hack/stretched-local-test.sh] — existing VM creation pattern (kubevirt.io/v1 API)
- [Source: project-context.md#External Runtime Dependencies] — kubevirt.io/api typed VirtualMachine access
- [Source: KubeVirt Kind quickstart] — https://kubevirt.io/quickstart_kind/
- [Source: KubeVirt software emulation] — https://github.com/kubevirt/kubevirt/blob/master/docs/software-emulation.md
- [Source: KubeVirt releases] — https://github.com/kubevirt/kubevirt/releases (v1.8.4 latest stable, 2026-06-16)
- [Source: virtctl installation] — https://kubevirt.io/user-guide/user_workloads/virtctl_client_tool/
- [Source: Story 14.1] — Kind cluster provisioning with Cilium Cluster Mesh
- [Source: Story 14.2] — Rook-Ceph deployment with StorageClass `rook-ceph-block`
- [Source: Story 14.4] — ScyllaDB script conventions and overlay patterns

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
