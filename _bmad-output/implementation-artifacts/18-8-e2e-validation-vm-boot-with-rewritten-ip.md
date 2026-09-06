# Story 18.8: E2E Validation — VM Boot with Rewritten IP

Status: ready-for-dev

## Story

As a platform engineer,
I want end-to-end validation that a VM annotated with a new IP actually boots with that IP on a real OpenShift Virtualization cluster,
so that I can trust the feature works in production conditions.

## Acceptance Criteria

### AC1: RHEL 9 VM boots with rewritten IP
Given an OCP Virt cluster with the IP rewrite webhook deployed
And a RHEL 9 VM with static IP `10.0.1.50/24`
When the VM is annotated with `soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"` and labeled `soteria.io/ip-rewrite: "true"`
And the VM is stopped and started (not restarted — ensures a new virt-launcher pod is created)
Then the virt-launcher pod has an `ip-rewrite` init container that completed successfully
And the VM boots and the QEMU guest agent reports IP `10.0.2.100`

### AC2: Windows Server 2022 VM boots with rewritten IP
Given an OCP Virt cluster with the IP rewrite webhook deployed
And a Windows Server 2022 VM with static IP `10.0.1.60/24`
When the VM is annotated with `soteria.io/eth0-ip: "10.0.2.110/24;10.0.2.1"` and labeled `soteria.io/ip-rewrite: "true"`
And the VM is stopped and started
Then the init container completed successfully
And the VM boots and the QEMU guest agent (or `ipconfig` via console) reports IP `10.0.2.110`

### AC3: Live migration does not trigger IP rewrite
Given a running VM with the `soteria.io/ip-rewrite: "true"` label and IP annotations
When a live migration is triggered (via `VirtualMachineInstanceMigration` CR)
Then the migration target virt-launcher pod does NOT have an `ip-rewrite` init container
And the migration completes successfully

### AC4: VM without label is unaffected
Given a VM without the `soteria.io/ip-rewrite` label
When the VM is started
Then the virt-launcher pod has no `ip-rewrite` init container
And startup time is unaffected (webhook not invoked due to objectSelector)

### AC5: Webhook unavailability does not block VM starts
Given the IP rewrite webhook deployment is scaled to 0
When a VM with `soteria.io/ip-rewrite: "true"` label is started
Then the VM starts successfully (failurePolicy: Ignore)
And the IP is NOT rewritten (expected degraded behavior)

## Tasks / Subtasks

- [ ] Task 1: Create RHEL 9 test VM manifest with static IP and QEMU guest agent (AC: 1)
  - [ ] 1.1: Create `test/ip-rewrite/e2e/manifests/rhel9-test-vm.yaml` — VirtualMachine resource with a RHEL 9 boot disk PVC, static IP `10.0.1.50/24`, gateway `10.0.1.1`, QEMU guest agent pre-installed
  - [ ] 1.2: Document the prerequisite: a RHEL 9 VM template/image with QEMU guest agent and static networking must exist on the cluster (either as a DataVolume source URL or a pre-created PVC)
  - [ ] 1.3: Add `cloud-init` or pre-baked image configuration for the initial static IP
- [ ] Task 2: Create Windows Server 2022 test VM manifest with static IP and QEMU guest agent (AC: 2)
  - [ ] 2.1: Create `test/ip-rewrite/e2e/manifests/win2022-test-vm.yaml` — VirtualMachine with Windows Server 2022 boot disk, static IP `10.0.1.60/24`, QEMU guest agent + VirtIO drivers pre-installed
  - [ ] 2.2: Document the prerequisite: a Windows Server 2022 image with QEMU guest agent and VirtIO drivers must be pre-created (licensing precludes automated provisioning)
- [ ] Task 3: Write E2E test script — RHEL IP rewrite and verify via guest agent (AC: 1)
  - [ ] 3.1: Create `test/ip-rewrite/e2e/test-rhel-ip-rewrite.sh`
  - [ ] 3.2: Apply RHEL 9 VM manifest, wait for VM running state
  - [ ] 3.3: Verify initial IP via `virtctl guestosinfo`
  - [ ] 3.4: Stop the VM (`virtctl stop`), wait for VMI deletion
  - [ ] 3.5: Annotate the VM with `soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"` and label `soteria.io/ip-rewrite: "true"`
  - [ ] 3.6: Start the VM (`virtctl start`), wait for Running
  - [ ] 3.7: Verify `ip-rewrite` init container completed (check virt-launcher pod init container statuses)
  - [ ] 3.8: Wait for QEMU guest agent to report, verify IP `10.0.2.100` via `virtctl guestosinfo`
  - [ ] 3.9: Clean up: remove annotations/labels, stop VM
- [ ] Task 4: Write E2E test script — Windows IP rewrite and verify via guest agent (AC: 2)
  - [ ] 4.1: Create `test/ip-rewrite/e2e/test-windows-ip-rewrite.sh`
  - [ ] 4.2: Apply Windows VM manifest, wait for VM running state
  - [ ] 4.3: Verify initial IP via `virtctl guestosinfo`
  - [ ] 4.4: Stop the VM, wait for VMI deletion
  - [ ] 4.5: Annotate and label the VM for IP rewrite
  - [ ] 4.6: Start the VM, wait for Running (Windows boot timeout: 5 minutes)
  - [ ] 4.7: Verify init container completed and IP `10.0.2.110` reported via guest agent
  - [ ] 4.8: Clean up
- [ ] Task 5: Write E2E test script — migration skip verification (AC: 3)
  - [ ] 5.1: Create `test/ip-rewrite/e2e/test-migration-skip.sh`
  - [ ] 5.2: Start a VM with IP rewrite label and annotations
  - [ ] 5.3: Trigger live migration via `VirtualMachineInstanceMigration` CR
  - [ ] 5.4: Verify the migration target virt-launcher pod has NO `ip-rewrite` init container
  - [ ] 5.5: Verify migration completes successfully
- [ ] Task 6: Write E2E test script — webhook fail-open (AC: 5)
  - [ ] 6.1: Create `test/ip-rewrite/e2e/test-webhook-failopen.sh`
  - [ ] 6.2: Scale the webhook Deployment to 0 replicas
  - [ ] 6.3: Start a VM with `soteria.io/ip-rewrite: "true"` label
  - [ ] 6.4: Verify the VM starts successfully (pod created without error)
  - [ ] 6.5: Verify no `ip-rewrite` init container was injected
  - [ ] 6.6: Scale webhook back to original replica count
- [ ] Task 7: Create E2E test runner and README (AC: all)
  - [ ] 7.1: Create `test/ip-rewrite/e2e/run-e2e.sh` — orchestrates all tests with pass/fail summary
  - [ ] 7.2: Create `test/ip-rewrite/e2e/README.md` — documents prerequisites, cluster requirements, manual execution procedure
  - [ ] 7.3: Create `test/ip-rewrite/e2e/lib/helpers.sh` — shared functions for wait loops, verification, logging

## Dev Notes

### Story Intelligence Chain

This is Story 18.8, the **final validation story** in Epic 18. It depends on ALL seven predecessor stories and validates that the entire IP rewrite pipeline works end-to-end on a real OCP Virtualization cluster. Below is how each predecessor contributes to what E2E tests validate.

#### Story 18.1 — Init Container Image: guestfs-tools on UBI9
**Contribution to E2E:** The container image (`quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION`) that runs as the init container in virt-launcher pods. E2E tests validate that this image starts, runs guestfish inside a real pod with `SYS_ADMIN` capability, and exits successfully.
- Containerfile at `build/ip-rewrite/Containerfile`, UBI9 base with `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport`
- `LIBGUESTFS_BACKEND=direct` baked in
- Image must be accessible from the cluster (pre-pushed to registry or loaded into cluster)

#### Story 18.2 — IP Rewrite Entrypoint Script: OS Detection & Dispatch
**Contribution to E2E:** The entrypoint script (`/scripts/entrypoint.sh`) that parses `SOTERIA_*_IP` env vars, scans disks with `virt-inspector`, detects the OS, and dispatches to the correct handler. E2E tests validate the full parsing → detection → dispatch chain runs on real VM disks.
- Environment variable convention: `SOTERIA_ETH0_IP="10.0.2.100/24;10.0.2.1"`, `SOTERIA_DNS="..."`
- Boot disk discovery under `/disks/*/`
- OS detection via `virt-inspector --xml` with `xmllint --xpath` parsing
- Dispatch to `rhel-handler.sh` or `windows-handler.sh` via `source`

#### Story 18.3 — RHEL IP Rewrite Handler: Augeas-Based
**Contribution to E2E:** The RHEL handler (`/scripts/rhel-handler.sh`) that rewrites static IP config using guestfish + Augeas. AC1 validates this handler operates on a real RHEL 9 guest disk with NM keyfile format.
- Two-phase guestfish approach: read-only detection, read-write rewrite
- NM keyfile rewrite for RHEL 9: `aug-set .../ipv4/address1 <ip>/<prefix>,<gateway>`, `aug-set .../ipv4/method manual`
- Handler reads `REWRITE_*` env vars set by entrypoint

#### Story 18.4 — Windows IP Rewrite Handler: hivex-Based
**Contribution to E2E:** The Windows handler (`/scripts/windows-handler.sh`) that rewrites static IP in the Windows registry hive offline. AC2 validates this handler operates on a real Windows Server 2022 guest disk.
- Download → modify → upload pattern for SYSTEM hive
- Adapter GUID matching under `<ControlSet>\Services\Tcpip\Parameters\Interfaces\{GUID}`
- `hivexregedit --merge` for safe value updates (IPAddress, SubnetMask, DefaultGateway, NameServer)
- `prefix_to_mask()` conversion (e.g., 24 → `255.255.255.0`)

#### Story 18.5 — Mutating Webhook: virt-launcher Init Container Injection
**Contribution to E2E:** The Go webhook server at `cmd/ip-rewrite-webhook/main.go` with handler at `internal/webhook/iprewrite/handler.go`. Every E2E test validates that the webhook correctly intercepts virt-launcher pod CREATE, injects the init container, and handles edge cases (migration skip, fail-open).
- Annotation-to-env-var transformation: `soteria.io/eth0-ip` → `SOTERIA_ETH0_IP`
- PVC volume mount injection at `/disks/<volumeName>`
- Init container prepended to `pod.Spec.InitContainers` with `SYS_ADMIN` capability
- Migration detection via `kubevirt.io/migrationJobLabel` label
- `failurePolicy: Ignore` on the MutatingWebhookConfiguration

#### Story 18.6 — Helm Sub-Chart & SCC
**Contribution to E2E:** The Helm chart (`charts/soteria-ip-rewrite/`) deploys the webhook server, creates the MutatingWebhookConfiguration, SecurityContextConstraints, cert-manager Certificate/Issuer, and RBAC. E2E tests require the chart to be installed as a prerequisite.
- Chart deploys webhook Deployment + Service + ServiceAccount
- SCC allows `SYS_ADMIN` capability for init containers in virt-launcher pods
- MWC with `objectSelector: {"soteria.io/ip-rewrite": "true"}` and `failurePolicy: Ignore`
- cert-manager self-signed Issuer + Certificate for TLS

#### Story 18.7 — Unit & Integration Tests: Disk Image Fixtures
**Contribution to E2E:** Story 18.7 tested the handlers on synthetic disk images in CI. This story (18.8) validates the same pipeline on **real VM disks** with **real OS boot** on a production-like cluster. The two test levels are complementary: 18.7 catches handler regressions in CI; 18.8 catches integration issues (SCC, disk access, boot sequence, guest agent).
- Go unit tests at `internal/webhook/iprewrite/handler_test.go`
- Shell integration tests at `test/ip-rewrite/run-tests.sh` with fixtures in `test/ip-rewrite/fixtures/`
- Makefile target: `make test-ip-rewrite`

### Critical Technical Details

#### E2E Tests Are NOT CI — Dedicated Environment Only

These E2E tests **cannot run in GitHub Actions CI**. They require:

1. **An OpenShift Virtualization cluster** (or KubeVirt on vanilla Kubernetes with SCC-equivalent pod security)
2. **Pre-created RHEL 9 VM images** with QEMU guest agent and static IP configured
3. **Pre-created Windows Server 2022 VM images** with QEMU guest agent and VirtIO drivers (requires Microsoft licensing)
4. **The IP rewrite webhook chart installed** (`helm install` from Story 18.6)
5. **cert-manager deployed** (for webhook TLS)
6. **Sufficient node resources** to run VMs (CPU, memory, storage)

**Execution model:** Manual or dedicated test environment triggered by a human operator. Test scripts are designed to be run from a workstation with `kubectl`/`oc` and `virtctl` access to the target cluster.

#### Verification via QEMU Guest Agent

The primary verification method is `virtctl guestosinfo <vm-name>`, which queries the QEMU guest agent running inside the VM to report network interface information including IP addresses.

**Command:**
```bash
virtctl guestosinfo <vm-name> -n <namespace>
```

**Output structure (JSON):**
```json
{
  "guestAgentVersion": "...",
  "hostname": "...",
  "os": { "name": "Red Hat Enterprise Linux", "version": "9.4" },
  "interfaces": [
    {
      "name": "eth0",
      "mac": "52:54:00:xx:xx:xx",
      "ipAddresses": ["10.0.2.100"]
    }
  ]
}
```

**Alternative verification (fallback):**
- RHEL: `virtctl ssh <vm>` then `ip addr show eth0` (requires SSH access configured)
- Windows: `virtctl console <vm>` then `ipconfig /all` (requires manual interaction or serial console scripting)

**QEMU guest agent readiness:** The guest agent takes time to start after VM boot (especially Windows). Poll `virtctl guestosinfo` with retries:
```bash
wait_for_guest_agent() {
    local vm="$1" ns="$2" timeout="${3:-300}" interval="${4:-10}"
    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        if virtctl guestosinfo "$vm" -n "$ns" -o json 2>/dev/null | jq -e '.interfaces' >/dev/null 2>&1; then
            return 0
        fi
        sleep "$interval"
    done
    return 1
}
```

#### VM Lifecycle for IP Rewrite Testing

The test sequence for AC1/AC2 must follow this precise order:

1. **VM exists with initial static IP** — either pre-created or applied from manifest
2. **VM is Running** — verify initial IP via guest agent
3. **Stop the VM** — `virtctl stop <vm>` → wait until VMI is deleted and virt-launcher pod is gone
4. **Annotate and label the VM** — `kubectl annotate vm <vm> soteria.io/eth0-ip="..."` and `kubectl label vm <vm> soteria.io/ip-rewrite=true`
5. **Start the VM** — `virtctl start <vm>` → KubeVirt creates a new VMI → new virt-launcher pod → webhook intercepts CREATE → init container injected
6. **Verify init container completed** — check virt-launcher pod's init container statuses
7. **Wait for guest agent** — VM boots, OS starts, guest agent reports
8. **Verify new IP** — parse `virtctl guestosinfo` output for the expected IP

**CRITICAL — stop-then-start, NOT restart:** `virtctl restart` may reuse the same virt-launcher pod (depending on KubeVirt version and restart policy). Use `virtctl stop` followed by `virtctl start` to guarantee a new pod creation, which triggers the webhook.

#### Annotation and Label Application

```bash
# Label the VM for webhook targeting
kubectl label vm "${VM_NAME}" -n "${NAMESPACE}" soteria.io/ip-rewrite=true --overwrite

# Annotate with target IP configuration
kubectl annotate vm "${VM_NAME}" -n "${NAMESPACE}" \
    soteria.io/eth0-ip="10.0.2.100/24;10.0.2.1" --overwrite
```

**KubeVirt annotation propagation:** Annotations set on the `VirtualMachine` resource are propagated to the `VirtualMachineInstance` (VMI) and then to the `virt-launcher` pod. This is handled by KubeVirt's control plane — no action needed from the webhook or test scripts.

**Label propagation for webhook `objectSelector`:** The `soteria.io/ip-rewrite: "true"` label must also propagate from VM to pod. KubeVirt propagates all VM labels to VMI and virt-launcher pod by default. Verify this works on the target cluster version.

#### Init Container Completion Verification

After the VM starts, verify the `ip-rewrite` init container ran and completed successfully:

```bash
verify_init_container() {
    local vm="$1" ns="$2"
    # Find the virt-launcher pod for this VM
    local pod
    pod=$(kubectl get pods -n "$ns" \
        -l "kubevirt.io/vm=${vm}" \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    [ -n "$pod" ] || { echo "FAIL: no virt-launcher pod found"; return 1; }

    # Check init container status
    local status
    status=$(kubectl get pod "$pod" -n "$ns" \
        -o jsonpath='{.status.initContainerStatuses[?(@.name=="ip-rewrite")].state.terminated.reason}')
    [ "$status" = "Completed" ] || { echo "FAIL: ip-rewrite init container status: $status"; return 1; }

    # Check exit code
    local exit_code
    exit_code=$(kubectl get pod "$pod" -n "$ns" \
        -o jsonpath='{.status.initContainerStatuses[?(@.name=="ip-rewrite")].state.terminated.exitCode}')
    [ "$exit_code" = "0" ] || { echo "FAIL: ip-rewrite exit code: $exit_code"; return 1; }

    echo "PASS: ip-rewrite init container completed successfully"
    return 0
}
```

#### Migration Skip Verification (AC3)

Create a `VirtualMachineInstanceMigration` to trigger live migration:

```bash
# Trigger live migration
cat <<EOF | kubectl apply -f -
apiVersion: kubevirt.io/v1
kind: VirtualMachineInstanceMigration
metadata:
  name: test-migration
  namespace: ${NAMESPACE}
spec:
  vmiName: ${VMI_NAME}
EOF

# Wait for migration to complete
kubectl wait vmim test-migration -n "${NAMESPACE}" \
    --for=jsonpath='{.status.phase}'=Succeeded --timeout=300s
```

Then verify the migration target pod does NOT have an `ip-rewrite` init container:

```bash
# Get the target pod (the newly created one during migration)
target_pod=$(kubectl get pods -n "$ns" \
    -l "kubevirt.io/vm=${vm},kubevirt.io/migrationJobLabel" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

# Verify no ip-rewrite init container
init_names=$(kubectl get pod "$target_pod" -n "$ns" \
    -o jsonpath='{.spec.initContainers[*].name}')
if echo "$init_names" | grep -q "ip-rewrite"; then
    echo "FAIL: ip-rewrite init container found on migration target pod"
    return 1
fi
echo "PASS: migration target pod has no ip-rewrite init container"
```

**Note:** The migration target pod has the `kubevirt.io/migrationJobLabel` label set by KubeVirt's `RenderMigrationManifest`. The webhook checks for this label and skips injection. The `objectSelector` on the MWC only checks for `soteria.io/ip-rewrite: "true"` — it does NOT filter by migration label. The webhook handler itself performs the migration skip.

#### Webhook Fail-Open Verification (AC5)

```bash
# Scale webhook to 0
WEBHOOK_NS="soteria-ip-rewrite"  # or wherever the chart was installed
kubectl scale deployment -n "${WEBHOOK_NS}" \
    -l app.kubernetes.io/name=soteria-ip-rewrite --replicas=0

# Wait for rollout (pods terminated)
kubectl rollout status deployment -n "${WEBHOOK_NS}" \
    -l app.kubernetes.io/name=soteria-ip-rewrite --timeout=60s

# Start a VM with the IP rewrite label
virtctl start "${VM_NAME}" -n "${NAMESPACE}"

# VM should start (failurePolicy: Ignore means webhook timeout → admit unmodified)
kubectl wait vmi "${VM_NAME}" -n "${NAMESPACE}" \
    --for=jsonpath='{.status.phase}'=Running --timeout=300s

# Verify NO ip-rewrite init container was injected
pod=$(kubectl get pods -n "${NAMESPACE}" \
    -l "kubevirt.io/vm=${VM_NAME}" \
    -o jsonpath='{.items[0].metadata.name}')
init_names=$(kubectl get pod "$pod" -n "${NAMESPACE}" \
    -o jsonpath='{.spec.initContainers[*].name}')
if echo "$init_names" | grep -q "ip-rewrite"; then
    echo "FAIL: ip-rewrite init container was injected despite webhook being down"
else
    echo "PASS: VM started without ip-rewrite init container (fail-open working)"
fi

# Restore webhook
kubectl scale deployment -n "${WEBHOOK_NS}" \
    -l app.kubernetes.io/name=soteria-ip-rewrite --replicas=2
```

#### Timeouts and Timing

| Operation | RHEL 9 | Windows Server 2022 |
|-----------|--------|---------------------|
| VM boot to Running phase | ~30s | ~60-120s |
| Guest agent reporting after boot | ~15-30s | ~60-180s |
| Total wait for guest agent IP | ~60s | ~300s |
| Live migration | ~30-60s | ~60-120s |
| Init container (guestfish appliance boot + disk rewrite) | ~15-30s | ~15-30s |

Set timeouts generously — E2E tests run on real infrastructure with variable performance:
```bash
RHEL_BOOT_TIMEOUT=120       # seconds
WINDOWS_BOOT_TIMEOUT=360    # seconds
GUEST_AGENT_TIMEOUT=300     # seconds for guest agent to report
MIGRATION_TIMEOUT=300       # seconds
VM_STOP_TIMEOUT=120         # seconds for VM to fully stop
```

#### Cluster Prerequisites Checklist

The `README.md` must document these prerequisites:

1. **OpenShift Virtualization** (or upstream KubeVirt) installed and operational
2. **cert-manager** installed (`kubectl get pods -n cert-manager`)
3. **IP rewrite webhook chart installed** (`helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ -n soteria-ip-rewrite --create-namespace`)
4. **IP rewrite init container image available** (either pushed to a registry the cluster can pull from, or loaded into the cluster's internal registry)
5. **RHEL 9 VM template/image** with:
   - QEMU guest agent (`qemu-guest-agent` package) installed and enabled
   - Static IP `10.0.1.50/24` configured (via NM keyfile)
   - Gateway `10.0.1.1`
   - Network accessible from the test runner (for `virtctl` commands)
6. **Windows Server 2022 VM template/image** with:
   - QEMU guest agent installed (from [virtio-win](https://github.com/virtio-win/virtio-win-pkg-scripts))
   - VirtIO network and storage drivers installed
   - Static IP `10.0.1.60/24` configured
   - Gateway `10.0.1.1`
7. **`virtctl` CLI** installed on the test runner machine
8. **`kubectl`/`oc` CLI** with cluster-admin access
9. **`jq`** installed for JSON parsing of guest agent output
10. **Sufficient node resources** — at least 4 CPU, 8GB RAM available for test VMs

#### E2E Test Runner Script Structure

```
test/ip-rewrite/e2e/
├── run-e2e.sh                               # Top-level orchestrator
├── README.md                                # Prerequisites and manual execution guide
├── lib/
│   └── helpers.sh                           # Shared functions (wait, verify, log)
├── manifests/
│   ├── rhel9-test-vm.yaml                   # RHEL 9 VM resource manifest
│   ├── win2022-test-vm.yaml                 # Windows Server 2022 VM manifest
│   └── migration-cr.yaml                    # VirtualMachineInstanceMigration template
├── test-rhel-ip-rewrite.sh                  # AC1: RHEL IP rewrite
├── test-windows-ip-rewrite.sh               # AC2: Windows IP rewrite
├── test-migration-skip.sh                   # AC3: Migration skip
└── test-webhook-failopen.sh                 # AC5: Webhook fail-open
```

**Note:** AC4 (VM without label is unaffected) is implicitly tested during the initial VM start in AC1/AC2 — the VMs first boot without the label and verify no `ip-rewrite` init container exists. A dedicated test is unnecessary but can be added as a quick assertion in the runner.

#### Helper Functions Library

`test/ip-rewrite/e2e/lib/helpers.sh` should provide:

```bash
#!/usr/bin/env bash
# Shared helpers for IP rewrite E2E tests

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $(date -u '+%H:%M:%S') $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date -u '+%H:%M:%S') $*" >&2; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date -u '+%H:%M:%S') $*" >&2; }
log_pass()  { echo -e "${GREEN}[PASS]${NC}  $*"; }
log_fail()  { echo -e "${RED}[FAIL]${NC}  $*"; }

# Default configuration — override via environment variables
: "${E2E_NAMESPACE:=ip-rewrite-e2e}"
: "${WEBHOOK_NAMESPACE:=soteria-ip-rewrite}"
: "${RHEL_BOOT_TIMEOUT:=120}"
: "${WINDOWS_BOOT_TIMEOUT:=360}"
: "${GUEST_AGENT_TIMEOUT:=300}"
: "${GUEST_AGENT_POLL_INTERVAL:=10}"
: "${MIGRATION_TIMEOUT:=300}"
: "${VM_STOP_TIMEOUT:=120}"

# Wait for VMI to reach Running phase
wait_for_vmi_running() {
    local vm="$1" ns="$2" timeout="${3:-$RHEL_BOOT_TIMEOUT}"
    log_info "Waiting for VMI ${vm} to reach Running (timeout: ${timeout}s)"
    kubectl wait vmi "${vm}" -n "${ns}" \
        --for=jsonpath='{.status.phase}'=Running --timeout="${timeout}s"
}

# Wait for VMI to be fully deleted
wait_for_vmi_deleted() {
    local vm="$1" ns="$2" timeout="${3:-$VM_STOP_TIMEOUT}"
    log_info "Waiting for VMI ${vm} to be deleted (timeout: ${timeout}s)"
    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        if ! kubectl get vmi "${vm}" -n "${ns}" &>/dev/null; then
            return 0
        fi
        sleep 5
    done
    return 1
}

# Wait for QEMU guest agent to report interfaces with IP addresses
wait_for_guest_agent_ip() {
    local vm="$1" ns="$2" expected_ip="$3" timeout="${4:-$GUEST_AGENT_TIMEOUT}"
    log_info "Waiting for guest agent to report IP ${expected_ip} on ${vm} (timeout: ${timeout}s)"
    local end=$((SECONDS + timeout))
    while [ $SECONDS -lt $end ]; do
        local ip
        ip=$(virtctl guestosinfo "${vm}" -n "${ns}" -o json 2>/dev/null \
            | jq -r '.interfaces[]?.ipAddresses[]? // empty' 2>/dev/null)
        if echo "$ip" | grep -qF "${expected_ip}"; then
            log_info "Guest agent reports IP: ${expected_ip}"
            return 0
        fi
        sleep "${GUEST_AGENT_POLL_INTERVAL}"
    done
    log_error "Guest agent did not report IP ${expected_ip} within ${timeout}s"
    # Dump debug info
    log_error "Last guest agent output:"
    virtctl guestosinfo "${vm}" -n "${ns}" -o json 2>&1 || true
    return 1
}

# Verify ip-rewrite init container exists and completed
verify_init_container_completed() {
    local vm="$1" ns="$2"
    local pod
    pod=$(kubectl get pods -n "$ns" \
        -l "kubevirt.io/vm=${vm}" \
        --field-selector=status.phase!=Succeeded \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    [ -n "$pod" ] || { log_fail "No virt-launcher pod found for VM ${vm}"; return 1; }

    local exit_code
    exit_code=$(kubectl get pod "$pod" -n "$ns" \
        -o jsonpath='{.status.initContainerStatuses[?(@.name=="ip-rewrite")].state.terminated.exitCode}' 2>/dev/null)
    [ "$exit_code" = "0" ] || { log_fail "ip-rewrite init container exit code: ${exit_code:-not found}"; return 1; }

    log_pass "ip-rewrite init container completed (exit code 0)"
    return 0
}

# Verify ip-rewrite init container does NOT exist on a pod
verify_no_init_container() {
    local vm="$1" ns="$2"
    local pod
    pod=$(kubectl get pods -n "$ns" \
        -l "kubevirt.io/vm=${vm}" \
        --field-selector=status.phase!=Succeeded \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    [ -n "$pod" ] || { log_fail "No virt-launcher pod found for VM ${vm}"; return 1; }

    local init_names
    init_names=$(kubectl get pod "$pod" -n "$ns" \
        -o jsonpath='{.spec.initContainers[*].name}' 2>/dev/null)
    if echo "$init_names" | grep -q "ip-rewrite"; then
        log_fail "ip-rewrite init container found on pod ${pod} (should not be present)"
        return 1
    fi

    log_pass "No ip-rewrite init container on pod ${pod}"
    return 0
}
```

#### VM Manifest Design — RHEL 9

The RHEL 9 test VM manifest should use a pre-existing PVC or DataVolume as the boot disk. Cloud-init can configure the initial static IP:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: rhel9-ip-rewrite-test
  namespace: ip-rewrite-e2e
spec:
  running: false
  template:
    metadata:
      labels: {}  # No ip-rewrite label initially
    spec:
      domain:
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
            - name: cloudinitdisk
              disk:
                bus: virtio
          interfaces:
            - name: default
              masquerade: {}
        resources:
          requests:
            memory: 2Gi
      networks:
        - name: default
          pod: {}
      volumes:
        - name: rootdisk
          dataVolume:
            name: rhel9-ip-rewrite-test-dv
        - name: cloudinitdisk
          cloudInitNoCloud:
            networkData: |
              version: 2
              ethernets:
                eth0:
                  addresses:
                    - 10.0.1.50/24
                  gateway4: 10.0.1.1
                  nameservers:
                    addresses:
                      - 8.8.8.8
            userData: |
              #cloud-config
              packages:
                - qemu-guest-agent
              runcmd:
                - systemctl enable --now qemu-guest-agent
  dataVolumeTemplates:
    - metadata:
        name: rhel9-ip-rewrite-test-dv
      spec:
        source:
          registry:
            url: "docker://registry.redhat.io/rhel9/rhel-guest-image:latest"
        pvc:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 30Gi
```

**Important:** The actual image source depends on the cluster's registry configuration. The manifest should use a configurable image source. Cloud-init configures the initial static IP and installs the QEMU guest agent.

**Cloud-init networking:** Cloud-init sets the initial IP (`10.0.1.50/24`). After the IP rewrite test, the guest disk's NM keyfile will have been modified by the handler (Story 18.3's Augeas rewrite). On next boot, NetworkManager reads the rewritten config.

**Potential issue:** Cloud-init may re-apply its network configuration on subsequent boots, overriding the handler's rewrite. To prevent this, the cloud-init `network-data` should configure a **one-time** network setup, or the cloud-init disk should be excluded from the DataVolume approach. Alternatively, use a pre-baked VM image where the static IP is configured in the guest OS directly (not via cloud-init). **Document this consideration in the README.**

#### VM Manifest Design — Windows Server 2022

Windows VMs cannot use cloud-init for networking. The VM must be created from a pre-baked image:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: win2022-ip-rewrite-test
  namespace: ip-rewrite-e2e
spec:
  running: false
  template:
    metadata:
      labels: {}
    spec:
      domain:
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
          interfaces:
            - name: default
              masquerade: {}
          tpm: {}
        clock:
          utc: {}
          timer:
            hpet:
              present: false
            pit:
              tickPolicy: delay
            rtc:
              tickPolicy: catchup
            hyperv: {}
        features:
          acpi: {}
          apic: {}
          hyperv:
            relaxed: {}
            vapic: {}
            spinlocks:
              spinlocks: 8191
        resources:
          requests:
            memory: 4Gi
      networks:
        - name: default
          pod: {}
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: win2022-ip-rewrite-test-pvc
```

**Prerequisites for Windows VM image:**
- VirtIO drivers installed (storage and network)
- QEMU guest agent for Windows installed and running as a service
- Static IP `10.0.1.60/24`, gateway `10.0.1.1` pre-configured
- The PVC must be pre-created from a golden image (licensing prevents automated download)

#### Environment Variables for Test Configuration

All test scripts should support configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `E2E_NAMESPACE` | `ip-rewrite-e2e` | Namespace for test VMs |
| `WEBHOOK_NAMESPACE` | `soteria-ip-rewrite` | Namespace where webhook chart is installed |
| `RHEL_VM_NAME` | `rhel9-ip-rewrite-test` | Name of the RHEL 9 test VM |
| `WINDOWS_VM_NAME` | `win2022-ip-rewrite-test` | Name of the Windows test VM |
| `RHEL_INITIAL_IP` | `10.0.1.50` | Initial static IP on RHEL VM |
| `RHEL_TARGET_IP` | `10.0.2.100` | Target IP after rewrite |
| `RHEL_TARGET_ANNOTATION` | `10.0.2.100/24;10.0.2.1` | Full annotation value |
| `WINDOWS_INITIAL_IP` | `10.0.1.60` | Initial static IP on Windows VM |
| `WINDOWS_TARGET_IP` | `10.0.2.110` | Target IP after rewrite |
| `WINDOWS_TARGET_ANNOTATION` | `10.0.2.110/24;10.0.2.1` | Full annotation value |
| `RHEL_BOOT_TIMEOUT` | `120` | Seconds to wait for RHEL VM boot |
| `WINDOWS_BOOT_TIMEOUT` | `360` | Seconds to wait for Windows VM boot |
| `GUEST_AGENT_TIMEOUT` | `300` | Seconds to wait for guest agent |
| `SKIP_CLEANUP` | `false` | If true, do not delete test resources |
| `INIT_CONTAINER_IMAGE` | *(from chart)* | Override init container image for testing |

#### Existing E2E Patterns in This Project

The project has two existing E2E test suites:

1. **`test/e2e/`** — Kubebuilder-scaffolded Kind-based E2E (Ginkgo/Gomega, `//go:build e2e`). Tests deploy the controller via `make docker-build` + `kind load`, install cert-manager, and validate the manager. Uses `test/utils/` helpers.

2. **`test/multisite/`** — Full lifecycle integration tests (Ginkgo/Gomega, `//go:build multisite`). Two-client architecture with `EAST_KUBECONFIG`/`WEST_KUBECONFIG`, VM deployment with DRPlan lifecycle. Uses cleanup guards, convergence helpers, and scenario-driven table tests.

**This story's E2E tests differ from both:**
- Shell scripts (not Go/Ginkgo) — appropriate because the test logic is primarily `kubectl`/`virtctl` commands with simple assertions
- Manual execution (not CI) — requires OCP Virt with licensed Windows images
- Tests are under `test/ip-rewrite/e2e/` (sibling to the `test/ip-rewrite/` integration test directory from Story 18.7)

**Why shell scripts, not Ginkgo?**
- The IP rewrite E2E tests are simpler than multisite lifecycle tests
- They require `virtctl` CLI (not Go client) for guest agent interaction
- They are run manually, not in automated CI
- Shell scripts are more accessible to platform engineers who may run these tests
- Reduces Go dependency surface (no KubeVirt Go client imports needed)

### Project Structure Notes

After this story, new files under `test/ip-rewrite/e2e/`:

```
test/
└── ip-rewrite/
    ├── run-tests.sh                      ← From Story 18.7 (integration tests)
    ├── fixtures/                          ← From Story 18.7 (synthetic disk images)
    └── e2e/                              ← NEW (this story)
        ├── run-e2e.sh                    ← Top-level E2E test runner
        ├── README.md                     ← Prerequisites + manual execution guide
        ├── lib/
        │   └── helpers.sh                ← Shared functions
        ├── manifests/
        │   ├── rhel9-test-vm.yaml        ← RHEL 9 VM manifest
        │   ├── win2022-test-vm.yaml      ← Windows Server 2022 VM manifest
        │   └── migration-cr.yaml         ← VirtualMachineInstanceMigration template
        ├── test-rhel-ip-rewrite.sh       ← AC1: RHEL IP rewrite
        ├── test-windows-ip-rewrite.sh    ← AC2: Windows IP rewrite
        ├── test-migration-skip.sh        ← AC3: migration skip
        └── test-webhook-failopen.sh      ← AC5: webhook fail-open
```

**No existing files are modified.** This story is purely additive.

**Alignment with existing project patterns:**
- `test/ip-rewrite/e2e/` sits alongside `test/ip-rewrite/` (Story 18.7's integration tests) — clear separation between CI-runnable integration tests and manual E2E tests
- Shell script test convention matches `hack/multisite/*.sh` scripts used in the multisite infrastructure setup (Epic 14)
- `lib/helpers.sh` follows the helper-library pattern from `test/utils/` (Go helpers for Kind-based E2E)
- All scripts use `#!/usr/bin/env bash` and `set -euo pipefail` per Epic 18 convention

### Anti-Patterns / DO NOT

- **DO NOT add these tests to CI** — E2E tests require an OCP Virt cluster with licensed Windows images. They cannot run in GitHub Actions. Do not modify `.github/workflows/ci.yml` or `release.yml`.
- **DO NOT modify the Makefile** — no Makefile target for E2E tests. They are run manually via `test/ip-rewrite/e2e/run-e2e.sh`.
- **DO NOT write Go/Ginkgo tests** — use shell scripts. The tests are CLI-driven (`kubectl`, `virtctl`, `jq`) and intended for manual execution by platform engineers.
- **DO NOT create VM images programmatically** — the RHEL and Windows VM images must be pre-created on the target cluster. Document requirements in the README, do not automate image creation.
- **DO NOT hardcode cluster-specific values** — use environment variables for all configurable values (namespace, VM names, IPs, timeouts, image references).
- **DO NOT modify handler scripts** (`entrypoint.sh`, `rhel-handler.sh`, `windows-handler.sh`) — this is a test-only story. If tests reveal bugs, document them.
- **DO NOT modify webhook code** (`handler.go`, `main.go`) — same as above.
- **DO NOT modify the Helm chart** — chart deployment is a prerequisite, not part of the test.
- **DO NOT use `virtctl restart`** for IP rewrite tests — use `virtctl stop` + `virtctl start` to guarantee a new virt-launcher pod (new webhook interception).
- **DO NOT assume cloud-init network config persists across reboots** — cloud-init may re-apply its network config on subsequent boots. Document this consideration and recommend pre-baked images.
- **DO NOT install `python3-hivex` or any additional packages** — E2E tests do not require guest filesystem tools on the test runner (all disk manipulation happens inside the init container on the cluster).
- **DO NOT test DNS rewrite in E2E** — DNS verification is complex (requires in-guest DNS resolution testing). DNS rewrite is validated in Story 18.7's integration tests. E2E focuses on IP address verification only.
- **DO NOT add IPv6 test cases** — deferred per Epic 18 scope.
- **DO NOT use `#!/bin/bash`** — use `#!/usr/bin/env bash` per Epic 18 convention.

### Verification Commands

```bash
# Run the full E2E test suite
bash test/ip-rewrite/e2e/run-e2e.sh

# Run individual tests
bash test/ip-rewrite/e2e/test-rhel-ip-rewrite.sh
bash test/ip-rewrite/e2e/test-windows-ip-rewrite.sh
bash test/ip-rewrite/e2e/test-migration-skip.sh
bash test/ip-rewrite/e2e/test-webhook-failopen.sh

# Run with custom configuration
E2E_NAMESPACE=my-test-ns \
RHEL_VM_NAME=my-rhel-vm \
GUEST_AGENT_TIMEOUT=600 \
    bash test/ip-rewrite/e2e/run-e2e.sh

# Verify prerequisites
kubectl get pods -n cert-manager              # cert-manager running
kubectl get pods -n soteria-ip-rewrite        # webhook running
virtctl version                                # virtctl available
kubectl get vm -n ip-rewrite-e2e              # test VMs exist
```

### References

- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Story 18.2 spec: `_bmad-output/implementation-artifacts/18-2-ip-rewrite-entrypoint-script-os-detection-dispatch.md`]
- [Story 18.3 spec: `_bmad-output/implementation-artifacts/18-3-rhel-ip-rewrite-handler-augeas-based.md`]
- [Story 18.4 spec: `_bmad-output/implementation-artifacts/18-4-windows-ip-rewrite-handler-hivex-based.md`]
- [Story 18.5 spec: `_bmad-output/implementation-artifacts/18-5-mutating-webhook-virt-launcher-init-container-injection.md`]
- [Story 18.6 spec: `_bmad-output/implementation-artifacts/18-6-helm-sub-chart-and-scc.md`]
- [Story 18.7 spec: `_bmad-output/implementation-artifacts/18-7-unit-and-integration-tests-disk-image-fixtures.md`]
- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18", Story 18.8 at ~line 4754]
- [Existing E2E test suite: `test/e2e/e2e_test.go` — Kind-based Ginkgo E2E pattern]
- [Existing multisite lifecycle tests: `test/multisite/lifecycle_test.go` — multi-cluster Ginkgo pattern]
- [virtctl guestosinfo documentation: https://kubevirt.io/user-guide/virtual_machines/accessing_virtual_machines/#retrieving-guest-os-information]
- [KubeVirt VirtualMachineInstanceMigration API: https://kubevirt.io/api-reference/main/definitions.html#_v1_virtualmachineinstancemigration]
- [KubeVirt annotation propagation: VM annotations propagated to VMI and virt-launcher pod]
- [cloud-init network config v2: https://cloudinit.readthedocs.io/en/latest/reference/network-config-format-v2.html]

## Code Review Record

### Review Model Used
*(To be filled during code review — must differ from dev model)*

### Review Findings
*(To be filled during code review)*

### Decisions Needed / Decisions Taken
*(To be filled during code review)*

### Fixes Applied
*(To be filled during code review)*

## Dev Agent Record

### Agent Model Used

*(To be filled by dev agent)*

### Debug Log References

### Completion Notes List

### File List
