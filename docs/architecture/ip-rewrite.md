# IP Rewrite Architecture

The IP rewrite feature is a **standalone add-on** that reconfigures guest
network settings on VM disks before the VM boots. It works by intercepting
virt-launcher pod creation with a mutating admission webhook and injecting an
init container that edits the guest filesystem offline using
[libguestfs](https://libguestfs.org/) tools.

This feature has **no dependency on Soteria CRDs** (DRPlan, DRExecution). It
operates entirely through Kubernetes labels and annotations on VM resources,
making it usable with any orchestration tool — `kubectl`, ArgoCD, Ansible, or
Soteria's own DRExecution workflow.

**Use cases:**

- Disaster recovery IP reconfiguration after failover to a different subnet
- Planned migration to a new network segment
- Lab or staging environment cloning with different IPs

## Annotation and Label Contract

The IP rewrite feature is activated by a **label** and configured through
**annotations** on VirtualMachine resources. KubeVirt propagates VM metadata
labels and annotations down to the virt-launcher pod, where the webhook
intercepts them.

### Label (Opt-In and Webhook Filter)

The opt-in label must be placed on the VM's **pod template** so that KubeVirt
stamps it onto the virt-launcher pod:

```yaml
spec:
  template:
    metadata:
      labels:
        soteria.io/ip-rewrite: "true"
```

This label serves two purposes:

1. **Webhook filtering** — The `MutatingWebhookConfiguration` uses an
   `objectSelector` with this label. The Kubernetes API server only sends
   admission requests for pods that carry this label, so the webhook is never
   invoked for unlabeled pods.
2. **Opt-in signal** — Setting this label explicitly opts the VM into IP
   rewriting.

### Annotations (IP Configuration)

IP annotations go on the VM's top-level `metadata.annotations`. KubeVirt
propagates VM-level annotations to the VMI and then to the virt-launcher pod.

Each annotation configures a single network interface:

```yaml
annotations:
  soteria.io/<interface>-ip: "<address>/<prefix>;<gateway>"
```

- `<interface>` — the guest network interface name (e.g., `eth0`, `eth1`)
- `<address>/<prefix>` — the desired static IP and CIDR prefix length
- `<gateway>` — the default gateway for that interface

### DNS Annotation (Optional)

```yaml
annotations:
  soteria.io/dns: "<server1>,<server2>"
```

When present, DNS servers are applied to all interfaces. When absent, DNS
configuration is left untouched on the guest.

### Examples

=== "Single-NIC"

    ```yaml
    apiVersion: kubevirt.io/v1
    kind: VirtualMachine
    metadata:
      name: my-vm
      annotations:
        soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
    spec:
      template:
        metadata:
          labels:
            soteria.io/ip-rewrite: "true"
    ```

=== "Multi-NIC"

    ```yaml
    apiVersion: kubevirt.io/v1
    kind: VirtualMachine
    metadata:
      name: my-vm
      annotations:
        soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
        soteria.io/eth1-ip: "192.168.1.50/16;192.168.1.1"
    spec:
      template:
        metadata:
          labels:
            soteria.io/ip-rewrite: "true"
    ```

=== "With DNS"

    ```yaml
    apiVersion: kubevirt.io/v1
    kind: VirtualMachine
    metadata:
      name: my-vm
      annotations:
        soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
        soteria.io/eth1-ip: "192.168.1.50/16;192.168.1.1"
        soteria.io/dns: "10.0.2.10,10.0.2.11"
    spec:
      template:
        metadata:
          labels:
            soteria.io/ip-rewrite: "true"
    ```

## How It Works

### Sequence Diagram

The following diagram shows the full flow from VM annotation to VM boot with
the new IP address:

```mermaid
sequenceDiagram
  participant User as User / Orchestrator
  participant VM as VirtualMachine
  participant KubeAPI as kube-apiserver
  participant Webhook as IP Rewrite Webhook
  participant Pod as virt-launcher Pod
  participant Init as ip-rewrite Init Container
  participant GuestFS as guestfish / virt-inspector

  User->>VM: Annotate with soteria.io/eth0-ip<br/>and label soteria.io/ip-rewrite: "true"
  User->>VM: Stop and start VM

  Note over VM,KubeAPI: KubeVirt creates virt-launcher pod<br/>(inherits VM labels and annotations)

  KubeAPI->>KubeAPI: objectSelector matches<br/>soteria.io/ip-rewrite: "true"
  KubeAPI->>Webhook: Admission request (pod CREATE)

  Webhook->>Webhook: Check for kubevirt.io/migrationJobUID
  Webhook->>Webhook: Parse annotations → env vars
  Webhook->>Webhook: Detect PVC volumes (filesystem + block)
  Webhook->>Webhook: Build init container spec

  Webhook-->>KubeAPI: JSON patch (inject init container)
  KubeAPI->>Pod: Create mutated pod

  Note over Pod,Init: Init container runs before QEMU starts

  Init->>Init: Check for SOTERIA_*_IP env vars
  Init->>GuestFS: Scan disks with virt-inspector

  GuestFS-->>Init: OS metadata (family, distro, version)

  alt Linux (RHEL)
    Init->>GuestFS: Rewrite IP config via Augeas
  else Windows
    Init->>GuestFS: Rewrite registry hive via hivex
  end

  Init->>Init: Exit 0 (success)
  Pod->>Pod: virt-launcher starts QEMU
  Note over Pod: VM boots with new IP
```

### Webhook Interception

The IP rewrite webhook is a standalone Go binary (`cmd/ip-rewrite-webhook`)
that runs as a Deployment in the cluster, separate from the main Soteria
controller-manager.

**Admission flow:**

1. The `MutatingWebhookConfiguration` registers for `pods` `CREATE`
   operations with an `objectSelector` matching
   `soteria.io/ip-rewrite: "true"`.
2. The Kubernetes API server evaluates the label selector *before* sending
   the admission request — pods without the label never reach the webhook.
3. The webhook handler at `/mutate-v1-pod` (port 9443) processes the request.
4. The handler transforms annotations into environment variables for the init
   container: `soteria.io/eth0-ip` becomes `SOTERIA_ETH0_IP`,
   `soteria.io/dns` becomes `SOTERIA_DNS`.
5. The handler identifies all PVC-backed volumes in the pod spec and creates
   corresponding volume mounts at `/disks/<volumeName>` for filesystem-mode
   PVCs, and volume devices at `/disks/<volumeName>` for block-mode PVCs.
6. The init container is prepended before all existing init containers so it
   runs first, before QEMU starts.
7. The handler returns a JSON patch that adds the init container to the pod
   spec.

**Init container spec highlights:**

- **Image:** Configurable via the `--init-container-image` flag on the webhook
  server (default: `quay.io/raffaelespazzoli/soteria-ip-rewrite:latest`)
- **Security context:** Runs as root (`runAsUser: 0`, `runAsNonRoot: false`)
  with `allowPrivilegeEscalation: true` and the `SYS_ADMIN` capability —
  required for the guestfish appliance to function inside a container
- **Volume mounts:** Filesystem-mode PVC volumes are mounted under `/disks/`;
  block-mode PVC volumes are exposed as device nodes under `/disks/`

### Fail-Open Policy

The webhook is configured with `failurePolicy: Ignore`. If the webhook server
is unavailable (scaled to zero, crashed, network partition), the Kubernetes
API server admits the pod **unmodified** — the VM starts without IP
rewriting.

!!! warning "Fail-open means no IP rewrite on webhook failure"
    When the webhook is down, VMs start with their existing IP
    configuration. This is a deliberate design choice: VM availability is
    prioritized over IP correctness. Monitor webhook health to detect
    this condition.

### OS Detection

After the init container starts, the entrypoint performs OS detection:

1. **Environment variable check** — If no `SOTERIA_*_IP` variables are set,
   the script exits immediately with code 0 (no-op). The virt-launcher
   proceeds normally.

2. **Disk scanning** — The entrypoint iterates over disk images and block
   devices under `/disks/`, running
   `virt-inspector --xml --no-applications --no-icon -a <disk>` on each
   candidate. `virt-inspector` uses the libguestfs appliance to analyze the
   disk and returns XML metadata about the operating system.

3. **Boot disk identification** — A disk with an `<operatingsystem>` element
   in the virt-inspector output is a boot disk candidate. Data disks (no OS)
   are logged and skipped. When multiple disks contain an OS, the volume
   named `rootdisk` is preferred; otherwise the first candidate
   alphabetically is selected.

4. **OS family and version extraction** — The entrypoint extracts the OS
   family (`linux` or `windows`), distribution (`rhel`, `windows`), and
   major/minor version from the XML output using `xmllint --xpath`.

5. **Dispatch** — Based on the detected OS:
    - **RHEL** (any version 7–10) → dispatches to the RHEL handler
    - **Windows** (Server 2016–2025, Windows 10/11) → dispatches to the
      Windows handler
    - **Unsupported OS** → exits with a non-zero code listing supported
      operating systems

!!! note "LIBGUESTFS_BACKEND=direct"
    The init container image sets `LIBGUESTFS_BACKEND=direct` as a baked-in
    environment variable. This tells libguestfs to use the direct backend
    (no libvirtd) — necessary because there is no libvirtd daemon inside the
    container.

### Linux IP Rewrite (Augeas)

For RHEL guests, the handler uses [Augeas](https://augeas.net/) via
`guestfish` to edit network configuration files on the guest filesystem. The
specific file format depends on the RHEL version:

| RHEL Version | Config Format | Config Path | Augeas Lens |
|---|---|---|---|
| RHEL 7 | `ifcfg-*` | `/etc/sysconfig/network-scripts/ifcfg-<iface>` | `Shellvars.lns` |
| RHEL 8 | `ifcfg-*` or NM keyfile | Auto-detected: NM keyfile preferred, `ifcfg` fallback | `Shellvars.lns` or `NetworkManager.lns` |
| RHEL 9 | NM keyfile | `/etc/NetworkManager/system-connections/<conn>.nmconnection` | `NetworkManager.lns` |
| RHEL 10 | NM keyfile | `/etc/NetworkManager/system-connections/<conn>.nmconnection` | `NetworkManager.lns` |

**How it works:**

1. The handler mounts the guest filesystem read-only to detect which network
   configuration format is in use.
2. It remounts read-write and uses `guestfish aug-set` commands (via the
   appropriate Augeas lens) to update IP address, prefix length, gateway, and
   optionally DNS servers.
3. The handler supports multiple interfaces — each `SOTERIA_*_IP` variable
   maps to a guest interface configuration file.

### Windows IP Rewrite (hivex)

For Windows guests, the handler uses
[hivex](https://libguestfs.org/hivex.3.html) to edit the Windows registry
hive offline. Network configuration in Windows is stored in the SYSTEM
registry hive.

**Registry path:**

```
HKLM\SYSTEM\<ControlSet>\Services\Tcpip\Parameters\Interfaces\<AdapterGUID>
```

**How it works:**

1. The handler locates the SYSTEM hive at
   `<systemroot>\system32\config\system` on the guest filesystem.
2. It determines the active `ControlSet` by reading the `Select\Current`
   value in the SYSTEM hive.
3. It enumerates adapter GUIDs under
   `ControlSet<N>\Services\Tcpip\Parameters\Interfaces\`.
4. It matches the target adapter by interface index or existing IP
   configuration.
5. It writes the new IP address, subnet mask (converted from CIDR prefix),
   gateway, and DNS servers as registry values using `hivexregedit --merge`.

**Registry value types used:**

| Value | Type | Description |
|---|---|---|
| `IPAddress` | `REG_MULTI_SZ` | Static IP address(es) |
| `SubnetMask` | `REG_MULTI_SZ` | Subnet mask(s) corresponding to IP addresses |
| `DefaultGateway` | `REG_MULTI_SZ` | Default gateway address(es) |
| `NameServer` | `REG_SZ` | Comma-separated DNS server addresses |
| `EnableDHCP` | `REG_DWORD` | Set to `0` to disable DHCP (static config) |

### Migration Detection

The webhook checks for the `kubevirt.io/migrationJobUID` label on incoming
pods. This label is set by KubeVirt on pods created during live migration.

When this label is detected, the webhook returns an `Allowed` response
immediately — no init container is injected. This is critical because:

- Migration pods run a *target* virt-launcher that takes over the running VM
  from the source virt-launcher
- Modifying the disk during migration would corrupt the running VM's
  filesystem
- The IP is already correct on the running VM — only the initial boot needs
  rewriting

## Components

| Component | Location | Description |
|---|---|---|
| **Webhook Server** | `cmd/ip-rewrite-webhook` | Standalone Go binary serving the mutating admission webhook on port 9443. Configured via flags (`--init-container-image`, `--cert-dir`). |
| **Webhook Handler** | `internal/webhook/iprewrite` | Core admission logic: annotation parsing, migration detection, init container construction, JSON patch generation. |
| **Init Container Image** | `build/ip-rewrite/Containerfile` | CentOS Stream 9-based image containing `guestfs-tools`, `augeas`, `hivex`, `perl-hivex`, and `libguestfs-winsupport`. |
| **MWC Manifest** | `config/ip-rewrite-webhook` | Reference `MutatingWebhookConfiguration` with `objectSelector`, `failurePolicy: Ignore`, and cert-manager CA injection annotation. |

## Supported Guest Operating Systems

| OS | Versions | Architecture | Config Method | Config Path |
|---|---|---|---|---|
| RHEL 7 | 7.x | x86_64 | `ifcfg-*` via Augeas | `/etc/sysconfig/network-scripts/ifcfg-<iface>` |
| RHEL 8 | 8.x | x86_64 | `ifcfg-*` or NM keyfile via Augeas | Auto-detected: NM keyfile preferred, `ifcfg` fallback |
| RHEL 9 | 9.x | x86_64 | NM keyfile via Augeas | `/etc/NetworkManager/system-connections/<conn>.nmconnection` |
| RHEL 10 | 10.x | x86_64 | NM keyfile via Augeas | `/etc/NetworkManager/system-connections/<conn>.nmconnection` |
| Windows Server 2016 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2019 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2022 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2025 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows 10 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows 11 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |

## Known Limitations

- **IPv6** — Not supported. Only IPv4 addresses are handled.
- **DHCP-to-static** — Not supported. The source VM must already have a
  static IP configuration.
- **Hostname rewrite** — Not supported. Only IP, gateway, and DNS are
  modified.
- **ARM64 guests** — Not supported. The init container image is x86_64 only.
- **Non-RHEL Linux** — Distributions such as Ubuntu, Fedora, or SUSE are not
  supported. Only RHEL 7–10 is handled on the Linux side.
