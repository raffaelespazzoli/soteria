# IP Rewrite E2E Tests

End-to-end validation that the IP rewrite feature works on real VMs running on an OpenShift Virtualization (or KubeVirt) cluster.

These tests **cannot run in CI** — they require a dedicated OCP Virt environment with pre-created VM images and licensed Windows Server software.

## Test Matrix

| Test Script | AC | Description |
|---|---|---|
| `test-rhel-ip-rewrite.sh` | AC1 | RHEL 9 VM boots with rewritten IP |
| `test-windows-ip-rewrite.sh` | AC2 | Windows Server 2022 VM boots with rewritten IP |
| `test-migration-skip.sh` | AC3 | Live migration does not trigger IP rewrite |
| `test-webhook-failopen.sh` | AC5 | Webhook unavailability does not block VM starts |

AC4 (VM without label is unaffected) is validated implicitly during the initial boot phase of AC1 and AC2 tests — the VMs first start without the `soteria.io/ip-rewrite` label and verify no init container is injected.

## Prerequisites

### Cluster Requirements

1. **OpenShift Virtualization** (or upstream KubeVirt) installed and operational
2. **cert-manager** deployed and running:
   ```bash
   kubectl get pods -n cert-manager
   ```
3. **IP rewrite webhook chart** installed:
   ```bash
   # Standard Kubernetes
   helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ \
       -n soteria-ip-rewrite --create-namespace

   # OpenShift — enable SCC and include the E2E namespace
   helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ \
       -n soteria-ip-rewrite --create-namespace \
       --set scc.enabled=true \
       --set "scc.namespaces={soteria-ip-rewrite,ip-rewrite-e2e}"
   ```
4. **IP rewrite init container image** accessible from the cluster — either pushed to a registry the cluster can pull from or loaded into the cluster's internal registry
5. **At least 2 schedulable nodes** for live migration tests (AC3)

### VM Image Requirements

#### RHEL 9

The test uses a RHEL 9 VM with:
- `qemu-guest-agent` package installed and enabled (`systemctl enable --now qemu-guest-agent`)
- Static IP `10.0.1.50/24` configured (via NM keyfile or cloud-init)
- Gateway `10.0.1.1`

The default manifest (`manifests/rhel9-test-vm.yaml`) uses cloud-init to configure networking and install the guest agent. Edit the `dataVolumeTemplates` section to point to your RHEL 9 image source.

> **Cloud-init caveat:** Cloud-init may re-apply its network configuration on subsequent boots, overriding the IP rewrite handler's changes. For reliable production validation, use a **pre-baked VM image** where the static IP is configured directly in the guest OS (not via cloud-init) and remove the `cloudInitNoCloud` volume from the manifest.

#### Windows Server 2022

The test requires a **pre-created PVC** containing a Windows Server 2022 image with:
- VirtIO storage and network drivers installed
- QEMU guest agent for Windows installed and running as a service (from [virtio-win](https://github.com/virtio-win/virtio-win-pkg-scripts))
- Static IP `10.0.1.60/24`, gateway `10.0.1.1` pre-configured

Microsoft licensing precludes automated image provisioning — the PVC must be prepared manually from a licensed Windows Server 2022 ISO.

Default PVC name: `win2022-ip-rewrite-test-pvc` in namespace `ip-rewrite-e2e`.

### Tools

The following CLI tools must be installed on the machine running the tests:

| Tool | Purpose |
|---|---|
| `kubectl` or `oc` | Cluster access (cluster-admin recommended) |
| `virtctl` | VM lifecycle management and guest agent queries |
| `jq` | JSON parsing of guest agent output |

## Quick Start

```bash
# 1. Verify prerequisites
kubectl get pods -n cert-manager              # cert-manager running
kubectl get pods -n soteria-ip-rewrite        # webhook running
virtctl version                               # virtctl available

# 2. Run all tests
bash test/ip-rewrite/e2e/run-e2e.sh

# 3. Run without Windows tests (no licensed image available)
bash test/ip-rewrite/e2e/run-e2e.sh --skip-windows

# 4. Run a single test
bash test/ip-rewrite/e2e/run-e2e.sh --test rhel
bash test/ip-rewrite/e2e/run-e2e.sh --test migration
bash test/ip-rewrite/e2e/run-e2e.sh --test failopen
```

## Configuration

All test scripts support configuration via environment variables:

| Variable | Default | Description |
|---|---|---|
| `E2E_NAMESPACE` | `ip-rewrite-e2e` | Namespace for test VMs |
| `WEBHOOK_NAMESPACE` | `soteria-ip-rewrite` | Namespace where webhook chart is installed |
| `RHEL_VM_NAME` | `rhel9-ip-rewrite-test` | Name of the RHEL 9 test VM |
| `WINDOWS_VM_NAME` | `win2022-ip-rewrite-test` | Name of the Windows test VM |
| `RHEL_INITIAL_IP` | `10.0.1.50` | Initial static IP on the RHEL VM |
| `RHEL_TARGET_IP` | `10.0.2.100` | Expected IP after rewrite |
| `RHEL_TARGET_ANNOTATION` | `10.0.2.100/24;10.0.2.1` | Full annotation value for RHEL rewrite |
| `WINDOWS_INITIAL_IP` | `10.0.1.60` | Initial static IP on the Windows VM |
| `WINDOWS_TARGET_IP` | `10.0.2.110` | Expected IP after rewrite |
| `WINDOWS_TARGET_ANNOTATION` | `10.0.2.110/24;10.0.2.1` | Full annotation value for Windows rewrite |
| `RHEL_DV_ACCESS_MODE` | `ReadWriteMany` | DataVolume access mode (`ReadWriteMany` required for live migration) |
| `RHEL_BOOT_TIMEOUT` | `300` | Seconds to wait for RHEL VM boot (includes DataVolume import) |
| `WINDOWS_BOOT_TIMEOUT` | `360` | Seconds to wait for Windows VM boot |
| `GUEST_AGENT_TIMEOUT` | `300` | Seconds to wait for guest agent to report |
| `MIGRATION_TIMEOUT` | `300` | Seconds to wait for live migration |
| `VM_STOP_TIMEOUT` | `120` | Seconds to wait for VM to fully stop |
| `SKIP_CLEANUP` | `false` | Set to `true` to keep test resources after run |

### Example: Custom Configuration

```bash
E2E_NAMESPACE=my-test-ns \
RHEL_VM_NAME=my-rhel9 \
WEBHOOK_NAMESPACE=my-webhook-ns \
GUEST_AGENT_TIMEOUT=600 \
    bash test/ip-rewrite/e2e/run-e2e.sh
```

## Directory Structure

```
test/ip-rewrite/e2e/
├── run-e2e.sh                    # Top-level test runner
├── README.md                     # This file
├── lib/
│   └── helpers.sh                # Shared functions (wait, verify, log)
├── manifests/
│   ├── rhel9-test-vm.yaml        # RHEL 9 VM resource manifest
│   ├── win2022-test-vm.yaml      # Windows Server 2022 VM manifest
│   └── migration-cr.yaml         # VirtualMachineInstanceMigration template
├── test-rhel-ip-rewrite.sh       # AC1: RHEL IP rewrite
├── test-windows-ip-rewrite.sh    # AC2: Windows IP rewrite
├── test-migration-skip.sh        # AC3: Migration skip
└── test-webhook-failopen.sh      # AC5: Webhook fail-open
```

## Troubleshooting

### VM does not start

- Check that the VM image source is correct in the manifest (`dataVolumeTemplates` or PVC)
- Verify the namespace has sufficient resource quotas
- Check for node scheduling issues: `kubectl get events -n <namespace>`

### Guest agent does not report IP

- Ensure `qemu-guest-agent` is installed and running inside the VM
- Windows: check that the QEMU Guest Agent Windows service is running
- Increase `GUEST_AGENT_TIMEOUT` (Windows may need 5+ minutes)
- Verify network connectivity: `virtctl console <vm-name>`

### Init container fails

- Check init container logs: `kubectl logs <virt-launcher-pod> -c ip-rewrite -n <namespace>`
- Verify the init container image is accessible from the cluster
- Check SCC/pod security: the init container requires `SYS_ADMIN` capability

### Migration test fails

- Ensure at least 2 schedulable nodes are available
- Check if live migration is enabled on the cluster
- Review migration status: `kubectl get vmim -n <namespace> -o yaml`

### Cloud-init overrides IP rewrite

Cloud-init may re-apply its network configuration on subsequent boots, undoing the IP rewrite. The RHEL test script automatically removes the `cloudInitNoCloud` volume from the VM spec before the rewrite boot. Other solutions:
1. Use a pre-baked VM image with static IP configured in the guest OS directly
2. Remove the `cloudInitNoCloud` volume from the VM manifest before the rewrite boot
3. Configure cloud-init to apply network config only on first boot (`network: {config: disabled}` in `/etc/cloud/cloud.cfg.d/`)

### Label and annotation placement

The `soteria.io/ip-rewrite` label and `soteria.io/eth0-ip` annotation must be on `spec.template.metadata` (not the VM's top-level `metadata`). KubeVirt only copies `spec.template.metadata` onto the VMI and virt-launcher pod. The test scripts use `kubectl patch vm --type=merge` to set these correctly.

### Live migration requires ReadWriteMany storage

The AC3 migration test requires the RHEL DataVolume to use `ReadWriteMany` (RWX) access mode. Set `RHEL_DV_ACCESS_MODE=ReadWriteMany` (default). If only `ReadWriteOnce` storage is available, the migration test will fail — skip it with `--test rhel`.
