# Story 14.3: KubeVirt Deployment with Nested Virtualization

Status: done

## Story

As a platform engineer,
I want KubeVirt deployed on both Minikube KVM2 clusters with nested virtualization and Rook-Ceph as the VM storage backend,
so that virtual machines can run with hardware acceleration and PVC-backed disks on real replicated storage.

## Acceptance Criteria

**AC1: Nested virtualization verification**
Given both Minikube KVM2 clusters from Story 14.1
When the KubeVirt setup script is executed
Then the script verifies `/dev/kvm` is accessible inside each Minikube node (nested virt)
And the script **fails hard** if nested virtualization is not available (no emulation fallback)

**AC2: KubeVirt operator deployment**
Given nested virtualization is confirmed
When KubeVirt operator and CR are deployed
Then the operator version is fetched dynamically from the stable.txt endpoint
And the operator is deployed on both clusters
And `useEmulation` is NOT set (hardware KVM acceleration is required)

**AC3: KubeVirt Rook-Ceph integration configuration**
Given KubeVirt is deployed and Rook-Ceph is running
When KubeVirt configuration is applied
Then a CDI (Containerized Data Importer) instance is deployed for DataVolume support
And the default StorageProfile for `rook-ceph-block` is verified functional
And KubeVirt can attach RBD-backed PVCs to VMs

**AC4: KubeVirt health**
Given KubeVirt is deployed
When checking KubeVirt status
Then KubeVirt phase is `Deployed` on both clusters
And all KubeVirt pods are running (virt-operator, virt-api, virt-controller, virt-handler)
And virt-handler confirms `/dev/kvm` access on each node

**AC5: Container disk smoke test**
Given KubeVirt is deployed with hardware acceleration
When a minimal test VMI with a container disk (cirros) is created
Then the VMI starts and reaches `Running` state using KVM (not QEMU TCG emulation)
And the VMI is cleaned up after verification

**AC6: PVC-backed disk smoke test**
Given KubeVirt and Rook-Ceph are running
When a test VM with a Rook-Ceph PVC-backed disk is created
Then the PVC binds on the `rook-ceph-block` StorageClass
And the VM starts and reaches `Running` state
And the VM is cleaned up after verification

**AC7: virtctl installation**
Given KubeVirt is deployed
When the setup script completes
Then `virtctl` is available for VM console access and lifecycle operations

## Tasks / Subtasks

- [x] Task 1: Create `hack/multisite/setup-kubevirt.sh` (AC: 1, 2, 3, 4, 7)
  - [x] 1.1: Env-var configuration block (KUBEVIRT_VERSION, CDI_VERSION, cluster profiles, consistent with other scripts)
  - [x] 1.2: Prerequisite checks (kubectl, minikube clusters running, Rook-Ceph StorageClass `rook-ceph-block` available)
  - [x] 1.3: **Nested virtualization check:** `minikube ssh -p <profile> -- test -c /dev/kvm` on each node — **hard fail** if unavailable
  - [x] 1.4: Fetch latest stable KubeVirt version via `curl -s https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt`
  - [x] 1.5: Deploy KubeVirt operator on both clusters (`kubectl apply --server-side -f kubevirt-operator.yaml`)
  - [x] 1.6: Deploy KubeVirt CR on both clusters — NO `useEmulation` (hardware KVM required)
  - [x] 1.7: Wait for KubeVirt phase to reach `Deployed` on both clusters
  - [x] 1.8: Verify virt-handler pods can access `/dev/kvm` (check pod logs or node capabilities)
  - [x] 1.9: Wait for all KubeVirt pods to be Running (virt-operator, virt-api, virt-controller, virt-handler)

- [x] Task 2: Deploy CDI for DataVolume support (AC: 3)
  - [x] 2.1: Fetch latest stable CDI version from `https://github.com/kubevirt/containerized-data-importer/releases`
  - [x] 2.2: Deploy CDI operator and CR on both clusters
  - [x] 2.3: Wait for CDI to reach `Deployed` phase
  - [x] 2.4: Verify StorageProfile for `rook-ceph-block` is populated (CDI auto-detects SC capabilities)

- [x] Task 3: Install virtctl (AC: 7)
  - [x] 3.1: Download virtctl binary matching deployed KubeVirt version from GitHub releases
  - [x] 3.2: Install to `./hack/multisite/.bin/virtctl`
  - [x] 3.3: Verify `virtctl version` succeeds against both clusters

- [x] Task 4: Container disk smoke test (AC: 5)
  - [x] 4.1: Create a test VMI on east using `quay.io/kubevirt/cirros-container-disk-demo` container disk
  - [x] 4.2: Wait for VMI to reach `Running` phase
  - [x] 4.3: Verify KVM acceleration (check QEMU command line or VMI annotations for `kvm` hint)
  - [x] 4.4: Delete the test VMI after verification
  - [x] 4.5: Repeat on west cluster

- [x] Task 5: PVC-backed disk smoke test (AC: 6)
  - [x] 5.1: Create a test PVC on east with StorageClass `rook-ceph-block` (1Gi)
  - [x] 5.2: Create a test VM on east with PVC-backed disk + container disk for boot
  - [x] 5.3: Wait for PVC to bind and VM to reach `Running` state
  - [x] 5.4: Delete test VM and PVC after verification
  - [x] 5.5: Repeat on west cluster

- [x] Task 6: README and finalization
  - [x] 6.1: Update `hack/multisite/README.md` with KubeVirt section (prerequisites, nested virt requirements, usage, troubleshooting)
  - [x] 6.2: Add idempotency checks throughout script

### Review Findings

- [x] [Review][Patch] Skip `minikube node list` headers and fail cleanly when no nodes are returned [hack/multisite/setup-kubevirt.sh:105]
- [x] [Review][Patch] Wait for KubeVirt and CDI CRDs to be established before applying their custom resources [hack/multisite/setup-kubevirt.sh:138]
- [x] [Review][Patch] Prevent `wait_kubevirt_pods()` from exiting under `set -euo pipefail` when all pods are already ready [hack/multisite/setup-kubevirt.sh:207]
- [x] [Review][Patch] Positively verify `/dev/kvm` access for each `virt-handler` pod instead of only grepping logs [hack/multisite/setup-kubevirt.sh:178]
- [x] [Review][Patch] Fail if the `rook-ceph-block` `StorageProfile` never populates instead of warning and continuing [hack/multisite/setup-kubevirt.sh:278]
- [x] [Review][Patch] Verify `virtctl` against both clusters instead of only running `version --client` [hack/multisite/setup-kubevirt.sh:334]
- [x] [Review][Patch] Make the container-disk smoke test fail when KVM acceleration cannot be proven [hack/multisite/setup-kubevirt.sh:398]
- [x] [Review][Patch] Wait for smoke-test VMI/VM/PVC cleanup to complete before reporting success [hack/multisite/setup-kubevirt.sh:413]
- [x] [Review][Patch] Update the README troubleshooting command to avoid assuming Docker is the Minikube node runtime [hack/multisite/README.md:323]

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — a shell script and a README update. No Go code. The outputs are:
- `hack/multisite/setup-kubevirt.sh` — main setup script
- `hack/multisite/README.md` — updated with KubeVirt section

KubeVirt provides VM execution on Kubernetes. In this project, VMs are the workloads protected by Soteria's DR orchestration. The PVC-backed disks attached to KubeVirt VMs are what get volume-replicated via Rook-Ceph RBD mirroring.

### Critical: Nested Virtualization Architecture (KVM2 Driver)

With Minikube `--driver=kvm2`, the virtualization stack is:

```
Host (bare metal) → KVM L0
  └── Minikube VM (L1 guest) → KVM L1 (nested virt)
        └── KubeVirt VM (L2 guest) — workload VMs live here
```

For this to work:
1. **Host** must have nested virtualization enabled:
   - Intel: `cat /sys/module/kvm_intel/parameters/nested` = `Y`
   - AMD: `cat /sys/module/kvm_amd/parameters/nested` = `1`
2. **Minikube's libvirt domain** passes through CPU features by default with `--driver=kvm2`, exposing `vmx`/`svm` to the guest
3. **Inside Minikube nodes**, `/dev/kvm` is available for KubeVirt's virt-handler

**No emulation fallback:** Unlike the old Kind-based approach (which used `useEmulation: true` as fallback), this story **requires** hardware-accelerated KVM. If nested virtualization is unavailable, the script must fail with a clear error message explaining how to enable it.

### Critical: Verifying Nested Virt Inside Minikube Nodes

```bash
verify_nested_virt() {
  local profile="$1"
  info "Checking nested virtualization on Minikube profile '${profile}'..."

  # Check each node
  local nodes
  nodes=$(minikube node list -p "${profile}" 2>/dev/null | awk '{print $1}')
  for node in ${nodes}; do
    if ! minikube ssh -p "${profile}" -n "${node}" -- "test -c /dev/kvm" 2>/dev/null; then
      fatal "Nested virtualization not available on node '${node}' in profile '${profile}'.
  Enable nested virt on host:
    Intel: echo 'options kvm_intel nested=1' | sudo tee /etc/modprobe.d/kvm.conf && sudo modprobe -r kvm_intel && sudo modprobe kvm_intel
    AMD:   echo 'options kvm_amd nested=1' | sudo tee /etc/modprobe.d/kvm.conf && sudo modprobe -r kvm_amd && sudo modprobe kvm_amd"
    fi
  done
  info "Nested virtualization confirmed on all nodes in '${profile}'"
}
```

### Critical: KubeVirt CR — No Emulation

The KubeVirt CR should NOT contain any emulation configuration:

```yaml
apiVersion: kubevirt.io/v1
kind: KubeVirt
metadata:
  name: kubevirt
  namespace: kubevirt
spec:
  configuration:
    developerConfiguration:
      featureGates:
        - LiveMigration
        - Snapshot
```

If `useEmulation: true` were set, VMs would silently fall back to QEMU TCG (10-100x slower) without any warning. By omitting it, virt-handler will fail loudly if `/dev/kvm` is not accessible.

### Critical: CDI (Containerized Data Importer) for Rook-Ceph Integration

CDI provides DataVolume support — the standard KubeVirt mechanism for creating VM disks from container images, URLs, or blank disks. It integrates with StorageClasses and is the preferred way to manage VM storage.

Why CDI is needed:
- Provides `StorageProfile` CRDs that auto-detect StorageClass capabilities (access modes, volume modes)
- Enables `DataVolume` templating for VM disk creation in Story 14.4 and 14.7
- Handles PVC population workflows (import, clone, upload)

CDI deployment follows the same pattern as KubeVirt:
```bash
CDI_VERSION="${CDI_VERSION:-$(curl -s https://github.com/kubevirt/containerized-data-importer/releases/latest/download/VERSION)}"
kubectl apply --server-side -f "https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-operator.yaml"
kubectl apply --server-side -f "https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-cr.yaml"
```

After deployment, verify the StorageProfile:
```bash
kubectl get storageprofile rook-ceph-block -o jsonpath='{.status.claimPropertySets}'
```

### Critical: KubeVirt Version

Latest stable release: **v1.8.4** (released 2026-06-16). The script should dynamically fetch the latest stable version:

```bash
KUBEVIRT_VERSION="${KUBEVIRT_VERSION:-$(curl -s https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)}"
```

Operator and CR manifests:
```bash
kubectl apply --server-side -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml"
kubectl apply --server-side -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-cr.yaml"
```

### Critical: Container Disk Smoke Test VMI

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

### Critical: PVC-Backed Disk Smoke Test VM

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

### Dependencies

| Dependency | Story | What's Needed |
|------------|-------|---------------|
| Minikube KVM2 clusters | 14.1 | Both `east` and `west` clusters running with nested virt |
| Rook-Ceph | 14.2 | StorageClass `rook-ceph-block` available (for PVC smoke test) |

This story does NOT depend on ScyllaDB (14.5) or Soteria (14.6).

### Deployment Sequence

1. Verify prerequisites (Minikube clusters running, nested virt available, Rook-Ceph SC available)
2. Deploy KubeVirt operator on both clusters (parallel or sequential)
3. Deploy KubeVirt CR on both clusters (NO useEmulation)
4. Wait for KubeVirt `Deployed` phase on both clusters
5. Verify virt-handler has `/dev/kvm` access
6. Verify all KubeVirt pods Running
7. Deploy CDI operator + CR on both clusters
8. Wait for CDI `Deployed` phase
9. Verify StorageProfile for `rook-ceph-block`
10. Install virtctl binary
11. Run container disk smoke test on both clusters
12. Run PVC-backed disk smoke test on both clusters
13. Clean up smoke test resources

### Script Conventions to Follow

Follow the same conventions from Stories 14.1 and 14.2:

- `set -euo pipefail` at top
- Env-var-driven configuration block at top of script
- `keast()` / `kwest()` helpers with explicit `--context`
- Cluster profiles: `east` and `west` (contexts match profile names)
- Idempotent operations (`kubectl apply --server-side`, check-before-create)
- Status messages via `info()`, `warn()`, `error()`, `fatal()` helpers
- Prerequisite checks at script start

### Context Pattern (from setup-clusters.sh / setup-rook-ceph.sh)

```bash
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }
```

### Idempotency

- `kubectl apply --server-side --force-conflicts` for operator and CR YAMLs
- Smoke tests: create → verify → delete (clean up after themselves)
- virtctl download: skip if already exists with correct version
- Script is safe to re-run without side effects

### Downstream Impact

Story 14.4 (Fedora VM Validation) depends on KubeVirt + CDI being deployed because:
- Uses CDI DataVolume or direct PVC + container disk pattern for Fedora VMs
- Validates the Rook-Ceph + KubeVirt end-to-end path with a Fedora image

Story 14.7 (Lifecycle Test) creates VMs with the same validated pattern:
- Container disk for boot (Fedora OS image)
- Rook-Ceph PVC for data (volume-replicated across sites)
- Labels: `soteria.io/drplan` and `soteria.io/wave`

### Potential Failure Modes

1. **Nested virt not available** — Host kernel module not configured. Script provides exact fix commands for Intel/AMD.
2. **KubeVirt stuck in `Deploying` phase** — virt-handler can't access `/dev/kvm`. Since we don't use emulation, this surfaces immediately as a hard failure.
3. **virt-handler pods CrashLoopBackOff** — Kernel modules missing inside Minikube VM. Minikube KVM2 driver should have all required modules, but verify `lsmod | grep kvm` inside node.
4. **CDI StorageProfile empty** — CDI couldn't detect `rook-ceph-block` capabilities. Manually create a StorageProfile or verify CSI driver advertises capabilities correctly.
5. **Container disk pull failure** — `quay.io/kubevirt/cirros-container-disk-demo` needs to be pullable from inside Minikube nodes.
6. **PVC not binding** — Rook-Ceph StorageClass `rook-ceph-block` must exist and Ceph health must be OK.
7. **virtctl download fails** — GitHub release asset URL depends on architecture detection.

### Timing and Resource Expectations

- KubeVirt operator deploy: ~1-2 min (download + apply)
- KubeVirt CR + reach Deployed: ~2-5 min
- CDI operator + CR + Deployed: ~1-3 min
- Container disk smoke test: ~1-2 min (pull image + start VMI)
- PVC-backed disk smoke test: ~2-3 min (PVC bind + start VM)
- Total: ~10-18 min for the full KubeVirt + CDI stack on both clusters

### File Structure

```
hack/multisite/
├── setup-kubevirt.sh                    # KubeVirt + CDI setup script (NEW — this story)
├── .bin/                                # Local tool binaries (virtctl lives here)
│   └── virtctl                          # Downloaded KubeVirt CLI
├── setup-clusters.sh                    # From Story 14.1
├── setup-rook-ceph.sh                   # From Story 14.2
├── teardown.sh                          # From Story 14.1
├── manifests/                           # From Story 14.2
└── README.md                            # Updated with KubeVirt section
```

### Testing Standards

No Go tests for this story — validation is via the smoke tests in AC5 and AC6. The setup script itself includes verification steps (nested virt check, KubeVirt Deployed phase, CDI Deployed phase, pod health, VMI Running state, PVC bind + VM Running state).

### References

- [Source: epics.md#Story 14.3 (was 14.5)] — acceptance criteria and technical notes
- [Source: hack/multisite/setup-clusters.sh] — Minikube KVM2 cluster creation pattern
- [Source: hack/multisite/setup-rook-ceph.sh] — Rook-Ceph script conventions (keast/kwest, info/fatal helpers)
- [Source: hack/stretched-local-test.sh] — existing VM creation pattern (kubevirt.io/v1 API)
- [Source: KubeVirt Kind quickstart] — https://kubevirt.io/quickstart_kind/
- [Source: KubeVirt nested virtualization] — https://kubevirt.io/user-guide/compute/nested_virtualization/
- [Source: KubeVirt releases] — https://github.com/kubevirt/kubevirt/releases (v1.8.4 latest stable)
- [Source: CDI releases] — https://github.com/kubevirt/containerized-data-importer/releases
- [Source: virtctl installation] — https://kubevirt.io/user-guide/user_workloads/virtctl_client_tool/
- [Source: Story 14.1] — Minikube KVM2 cluster provisioning with Cilium Cluster Mesh
- [Source: Story 14.2] — Rook-Ceph deployment with StorageClass `rook-ceph-block`

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

None — pure infrastructure story, no debugging required.

### Completion Notes List

- Created `hack/multisite/setup-kubevirt.sh` — full KubeVirt + CDI deployment script
- Script follows established conventions from setup-clusters.sh and setup-rook-ceph.sh (keast/kwest helpers, info/fatal log functions, env-var config block, idempotent operations)
- Nested virtualization verification hard-fails with clear remediation instructions for Intel/AMD
- KubeVirt CR deployed WITHOUT `useEmulation` — enforces hardware KVM acceleration
- CDI deployed for DataVolume/StorageProfile support, with auto-detection verification
- virtctl installed to `.bin/` with version-based skip for idempotency
- Container disk smoke test creates cirros VMI, verifies Running state and KVM accel
- PVC-backed disk smoke test creates a VM with Rook-Ceph PVC, verifies binding and Running
- Both smoke tests run on east and west, clean up after themselves
- README updated with KubeVirt section: prerequisites, env vars, verification commands, troubleshooting
- File structure section updated in README
- Downstream dependencies list corrected to reflect new story ordering
- All idempotency patterns: `kubectl apply --server-side --force-conflicts`, check-before-download for virtctl, cleanup-before-create for smoke tests

### File List

- hack/multisite/setup-kubevirt.sh (NEW)
- hack/multisite/README.md (MODIFIED)
