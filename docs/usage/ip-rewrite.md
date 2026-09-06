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
    - **SecurityContextConstraints (OpenShift)** — The init container runs as
      root with `SYS_ADMIN` capability. On OpenShift, an appropriate SCC must
      be bound to the virt-launcher service account. The Helm chart includes
      the required SCC.

---

## Installation

Install the IP rewrite webhook using the standalone Helm chart:

```bash
helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ \
  --namespace soteria \
  --create-namespace
```

!!! tip "Custom values"
    Override chart defaults with `--set` or a values file:
    ```bash
    helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ \
      --namespace soteria \
      --create-namespace \
      -f my-values.yaml
    ```

For general Helm installation guidance, see
[Helm Installation](../installation/helm.md). The IP rewrite chart is
independent of the main Soteria chart and can be installed standalone.

---

## Configuring IP Rewrite

### Single-NIC Rewrite

Follow these steps to rewrite the IP address on a VM with a single network
interface.

**Step 1 — Add the opt-in label to the pod template:**

```bash
kubectl patch vm <vm-name> --type merge -p '
spec:
  template:
    metadata:
      labels:
        soteria.io/ip-rewrite: "true"
'
```

!!! note "Label placement"
    The `soteria.io/ip-rewrite` label must be on the VM's
    `spec.template.metadata.labels` so that KubeVirt stamps it onto the
    virt-launcher pod. The webhook's `objectSelector` matches this label on
    the pod.

**Step 2 — Add the IP annotation to the VM's pod template:**

Annotations must be placed on `spec.template.metadata.annotations` so that
KubeVirt copies them to the virt-launcher pod. Use a JSON patch:

```bash
kubectl patch vm <vm-name> --type merge -p '
spec:
  template:
    metadata:
      annotations:
        soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
'
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

For VMs with multiple network interfaces, add one annotation per interface
on the pod template:

```bash
kubectl patch vm <vm-name> --type merge -p '
spec:
  template:
    metadata:
      annotations:
        soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
        soteria.io/eth1-ip: "192.168.1.50/16;192.168.1.1"
'
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
spec:
  running: true
  template:
    metadata:
      labels:
        soteria.io/ip-rewrite: "true"
      annotations:
        soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
        soteria.io/eth1-ip: "192.168.1.50/16;192.168.1.1"
        soteria.io/dns: "10.0.2.10,10.0.2.11"
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

!!! note "Annotation and label placement"
    Both **IP annotations** (`soteria.io/*-ip`, `soteria.io/dns`) and the
    **opt-in label** (`soteria.io/ip-rewrite`) go on
    `spec.template.metadata`. KubeVirt copies `spec.template.metadata` onto
    the VMI → virt-launcher pod. The webhook reads annotations and the
    `objectSelector` matches labels on the pod.

### DNS Configuration

DNS server configuration is optional. When provided, DNS settings are applied
to all interfaces on the guest:

```bash
kubectl patch vm <vm-name> --type merge -p '
spec:
  template:
    metadata:
      annotations:
        soteria.io/dns: "10.0.2.10,10.0.2.11"
'
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
[INFO]  2026-09-06T12:00:00Z IP rewrite entrypoint starting
[INFO]  2026-09-06T12:00:00Z Found 1 IP configuration variable(s)
[INFO]  2026-09-06T12:00:00Z Parsed interface eth0: ip=10.0.2.100 prefix=24 gateway=10.0.2.1
[INFO]  2026-09-06T12:00:00Z No DNS configuration provided
[INFO]  2026-09-06T12:00:00Z Scanning disks for operating system...
[INFO]  2026-09-06T12:00:01Z Found 1 disk candidate(s)
[INFO]  2026-09-06T12:00:02Z Detected OS: family=linux distro=rhel version=9.4
[INFO]  2026-09-06T12:00:02Z Product name: Red Hat Enterprise Linux release 9.4 (Plow)
[INFO]  2026-09-06T12:00:02Z Dispatching to RHEL handler
[INFO]  2026-09-06T12:00:03Z RHEL handler completed successfully
[INFO]  2026-09-06T12:00:03Z IP rewrite entrypoint completed successfully
```

### Checking VM Interface IPs

After the VM boots, verify the IP address from outside the VM using the
VirtualMachineInstance status:

```bash
kubectl get vmi <vm-name> -o jsonpath='{.status.interfaces}' | jq .
```

The output lists the guest's network interfaces and their configured IP
addresses. Confirm the IP matches the value you set in the annotation.

---

## Troubleshooting

### IP Rewrite Failure Blocks VM Boot

The init container runs with `set -euo pipefail`. Any error — unsupported OS,
malformed annotation value, disk inspection failure — causes the init
container to exit with a non-zero code. Since Kubernetes does not start
subsequent containers (including the virt-launcher) until all init containers
succeed, **a failed IP rewrite prevents the VM from booting**.

To diagnose:

```bash
# Check init container status
kubectl describe pod <virt-launcher-pod> | grep -A5 "ip-rewrite"

# View init container logs for the error
kubectl logs <virt-launcher-pod> -c ip-rewrite
```

!!! tip "Removing the rewrite to unblock a VM"
    If the IP rewrite is failing and you need the VM to start immediately,
    remove the opt-in label from the pod template:
    ```bash
    kubectl patch vm <vm-name> --type json \
      -p '[{"op":"remove","path":"/spec/template/metadata/labels/soteria.io~1ip-rewrite"}]'
    virtctl stop <vm-name> && virtctl start <vm-name>
    ```

### SCC Issues (OpenShift)

!!! warning "Privileged security context required"
    The init container runs as root (`runAsUser: 0`) with `SYS_ADMIN`
    capability and `allowPrivilegeEscalation: true`. On OpenShift, this
    requires an appropriate SecurityContextConstraints (SCC) binding.

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
| `No disk images or block devices found under /disks/` | No PVC volumes mounted | Verify the VM uses PVC-backed disks, not container disks |
| `No operating system detected on any disk` | Disk does not contain a recognizable OS | Verify the PVC contains a boot disk with a supported OS, not a data disk |

### Unsupported Guest OS

If the init container logs show:

```
[ERROR] 2026-09-06T12:00:02Z Unsupported operating system: family=<name> distro=<distro>
[ERROR] 2026-09-06T12:00:02Z Supported operating systems: RHEL 7/8/9/10, Windows Server 2016/2019/2022/2025, Windows 10/11
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
migration pods by the `kubevirt.io/migrationJobUID` label and deliberately
skips init container injection.

Migration pods run a target virt-launcher that takes over the running VM from
the source — modifying the disk during migration would corrupt the VM's
filesystem. Only pods created for initial VM boot receive the init container.

---

## Known Limitations

- **IPv6** — Not supported. Only IPv4 addresses are handled.
- **DHCP-to-static** — Not supported. The source VM must already have a
  static IP configuration.
- **Hostname rewrite** — Not supported. Only IP, gateway, and DNS are
  modified.
- **ARM64 guests** — Not supported. The init container image is
  single-architecture `linux/amd64`.
- **Non-RHEL Linux** — Only RHEL 7–10 is supported. Ubuntu, Fedora, SUSE,
  and other distributions are not handled.

For the full supported OS matrix, see the
[architecture page](../architecture/ip-rewrite.md#supported-guest-operating-systems).
