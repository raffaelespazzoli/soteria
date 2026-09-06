# IP Rewrite Usage Guide

This guide walks you through configuring and using the IP rewrite feature to
reconfigure VM network settings during disaster recovery, planned migration,
or subnet changes. The IP rewrite webhook modifies guest network configuration
offline — before the VM boots — so the VM comes up with the correct IP address
on the target network.

---

## Prerequisites

!!! info "Before you begin"
    Ensure the following components are installed and healthy on your cluster:

    - **cert-manager** — Required for TLS certificate provisioning on the
      webhook server. Verify with:
      ```bash
      kubectl get pods -n cert-manager
      ```
    - **OpenShift Virtualization or KubeVirt** — The IP rewrite feature
      targets virt-launcher pods created by KubeVirt.
    - **PVC-backed VM disks** — The init container modifies guest filesystems
      on PVC volumes. Container disks are not supported.

---

## Installation

Install the IP rewrite webhook using the standalone Helm chart:

```bash
helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ \
  --namespace soteria \
  --create-namespace
```

!!! tip "Custom image tag"
    To specify a particular init container image version:
    ```bash
    helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ \
      --namespace soteria \
      --create-namespace \
      --set webhook.initContainerImage=quay.io/raffaelespazzoli/soteria-ip-rewrite:v0.1.0
    ```

For general Helm installation guidance, see
[Helm Installation](../installation/helm.md). The IP rewrite chart is
independent of the main Soteria chart and can be installed standalone.

---

## Configuring IP Rewrite

### Single-NIC Rewrite

Follow these steps to rewrite the IP address on a VM with a single network
interface.

**Step 1 — Add the opt-in label:**

```bash
kubectl label vm <vm-name> soteria.io/ip-rewrite=true
```

**Step 2 — Add the IP annotation:**

```bash
kubectl annotate vm <vm-name> \
  soteria.io/eth0-ip="10.0.2.100/24;10.0.2.1"
```

The annotation format is `<address>/<prefix>;<gateway>`:

| Part | Example | Description |
|------|---------|-------------|
| `address` | `10.0.2.100` | Desired static IP address |
| `prefix` | `24` | CIDR prefix length (subnet mask) |
| `gateway` | `10.0.2.1` | Default gateway |

**Step 3 — Stop and start the VM:**

```bash
virtctl stop <vm-name>
virtctl start <vm-name>
```

!!! note "Restart required"
    The IP rewrite happens during pod creation. A running VM must be stopped
    and started (not just rebooted from inside the guest) so that a new
    virt-launcher pod is created and the webhook can inject the init container.

**Step 4 — Verify the rewrite:**

See the [Verifying the Rewrite](#verifying-the-rewrite) section below.

### Multi-NIC Rewrite

For VMs with multiple network interfaces, add one annotation per interface:

```bash
kubectl label vm <vm-name> soteria.io/ip-rewrite=true

kubectl annotate vm <vm-name> \
  soteria.io/eth0-ip="10.0.2.100/24;10.0.2.1" \
  soteria.io/eth1-ip="192.168.1.50/16;192.168.1.1"
```

Each annotation follows the same `<address>/<prefix>;<gateway>` format. The
interface name in the annotation (e.g., `eth0`, `eth1`) maps to the
corresponding guest network interface.

### Full YAML Example

You can also set labels and annotations directly in the VM manifest:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: my-app-vm
  labels:
    soteria.io/ip-rewrite: "true"
  annotations:
    soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
    soteria.io/eth1-ip: "192.168.1.50/16;192.168.1.1"
    soteria.io/dns: "10.0.2.10,10.0.2.11"
spec:
  running: true
  template:
    metadata:
      labels:
        soteria.io/ip-rewrite: "true"
    spec:
      domain:
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: my-app-rootdisk
```

### DNS Configuration

DNS server configuration is optional. When provided, DNS settings are applied
to all interfaces on the guest:

```bash
kubectl annotate vm <vm-name> \
  soteria.io/dns="10.0.2.10,10.0.2.11"
```

Multiple DNS servers are separated by commas. If the `soteria.io/dns`
annotation is absent, the guest's existing DNS configuration is left
untouched.

---

## Verifying the Rewrite

### Checking Init Container Logs

After the VM starts, inspect the init container logs to confirm the IP rewrite
executed successfully:

```bash
# Find the virt-launcher pod
kubectl get pods -l kubevirt.io/vm=<vm-name>

# View the init container logs
kubectl logs <virt-launcher-pod> -c ip-rewrite
```

A successful run produces output similar to:

```
[INFO] Parsing IP configuration from environment variables
[INFO] Found interface eth0: 10.0.2.100/24 gateway 10.0.2.1
[INFO] Scanning disks for operating system...
[INFO] Detected OS: Red Hat Enterprise Linux release 9.4 (rhel 9.4)
[INFO] Dispatching to RHEL handler
[INFO] Rewriting network configuration for eth0
[INFO] IP rewrite completed successfully
```

### Checking Guest Agent Output

If the QEMU guest agent is installed in the VM, verify the IP address from
outside the VM:

```bash
virtctl guestosinfo <vm-name>
```

The output includes the guest's network interfaces and their configured IP
addresses. Confirm the IP matches the value you set in the annotation.

You can also check with `kubectl`:

```bash
kubectl get vmi <vm-name> -o jsonpath='{.status.interfaces}' | jq .
```

---

## Troubleshooting

### SCC Issues (OpenShift)

!!! warning "SYS_ADMIN capability required"
    The init container requires the `SYS_ADMIN` Linux capability for the
    guestfish appliance to function. On OpenShift, this requires an
    appropriate SecurityContextConstraints (SCC) binding.

If the init container fails to start with a permission error:

1. Verify the SCC is deployed (the Helm chart includes one):
   ```bash
   kubectl get scc soteria-ip-rewrite
   ```
2. Verify the service account is bound to the SCC:
   ```bash
   kubectl get clusterrolebinding | grep soteria-ip-rewrite
   ```
3. Check pod events for SCC-related errors:
   ```bash
   kubectl describe pod <virt-launcher-pod>
   ```

### Guestfish Appliance Errors

The init container uses `LIBGUESTFS_BACKEND=direct` (baked into the image).
Common guestfish issues:

| Symptom | Cause | Solution |
|---------|-------|----------|
| `libguestfs: error: /dev/kvm not found` | KVM device not available in the init container | Verify the node supports KVM; some container runtimes restrict `/dev/kvm` access |
| `libguestfs: error: supermin appliance failed` | Appliance build failure | Check disk space and memory limits on the init container |
| `virt-inspector: no operating systems found` | Disk does not contain a recognizable OS | Verify the PVC contains a boot disk with a supported OS, not a data disk |

### Unsupported Guest OS

If the init container logs show:

```
[ERROR] Unsupported operating system: <detected-os>
[ERROR] Supported: RHEL 7/8/9/10, Windows Server 2016/2019/2022/2025, Windows 10/11
```

The VM's guest OS is not in the supported list. See the
[Supported Guest Operating Systems](../architecture/ip-rewrite.md#supported-guest-operating-systems)
table in the architecture page for the full matrix.

### Webhook Unavailable

The webhook uses `failurePolicy: Ignore` (fail-open). When the webhook is
down:

- VMs start **without** IP rewriting
- No error or warning is shown on the pod (the API server simply skips the
  webhook)
- The VM boots with its existing IP configuration

To check webhook health:

```bash
# Verify the webhook deployment is running
kubectl get deployment -n soteria soteria-ip-rewrite-webhook

# Check the webhook endpoint
kubectl get endpoints -n soteria soteria-ip-rewrite-webhook

# Verify the MutatingWebhookConfiguration
kubectl get mutatingwebhookconfigurations soteria-ip-rewrite
```

### Migration Pods Skip IP Rewrite

If you notice that IP rewrite does not run on a virt-launcher pod created
during live migration, this is **expected behavior**. The webhook detects
migration pods by the `kubevirt.io/migrationJobLabel` label and deliberately
skips init container injection.

Migration pods run a target virt-launcher that takes over the running VM from
the source — modifying the disk during migration would corrupt the VM's
filesystem. Only pods created for initial VM boot receive the init container.
