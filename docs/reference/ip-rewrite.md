# IP Rewrite Reference

Quick-lookup reference for the IP rewrite feature — annotations, labels,
supported guest operating systems, SecurityContextConstraints, and Helm chart
configuration.

For architecture details, see the
[IP Rewrite Architecture](../architecture/ip-rewrite.md) page. For
step-by-step usage instructions, see the
[IP Rewrite Usage Guide](../usage/ip-rewrite.md).

---

## Annotations and Labels

### Label

The opt-in label activates webhook interception for a VM's virt-launcher pod.

| Key | Value | Applies To | Description |
|-----|-------|-----------|-------------|
| `soteria.io/ip-rewrite` | `"true"` | VirtualMachine `spec.template.metadata.labels` | Opt-in label. Enables webhook interception via `objectSelector` on the `MutatingWebhookConfiguration`. KubeVirt propagates this label from the VM pod template to the virt-launcher pod. |

### IP Annotations

One annotation per network interface, placed on the VM's **pod template** at
`spec.template.metadata.annotations`. KubeVirt copies
`spec.template.metadata` onto the VMI → virt-launcher pod; the webhook reads
these annotations from `pod.Annotations`.

| Key | Type | Required | Format | Example | Description |
|-----|------|----------|--------|---------|-------------|
| `soteria.io/<interface>-ip` | Annotation | **Yes** (at least one) | `<address>/<prefix>;<gateway>` | `soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"` | Per-interface IP configuration. On RHEL, `<interface>` is the guest NIC name (e.g., `eth0`, `eth1`). On Windows the name is a label only; adapters are matched by existing static IP or subnet. The webhook transforms this into `SOTERIA_<INTERFACE>_IP`. Must be placed on `spec.template.metadata.annotations`. |

### DNS Annotation

| Key | Type | Required | Format | Example | Description |
|-----|------|----------|--------|---------|-------------|
| `soteria.io/dns` | Annotation | No | Comma-separated IPs | `soteria.io/dns: "10.0.2.10,10.0.2.11"` | Global DNS servers applied to all interfaces. The webhook transforms this into `SOTERIA_DNS`. When absent, the guest's existing DNS configuration is left untouched. Must be placed on `spec.template.metadata.annotations` alongside the IP annotations. |

### Migration Skip Label

| Key | Value | Description |
|-----|-------|-------------|
| `kubevirt.io/migrationJobUID` | *(set by KubeVirt)* | When this label is present on a pod, the webhook skips init container injection. Migration pods must not have their disks modified — the VM is already running with the correct IP. |

!!! info "Full YAML Example"

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

---

## Supported Guest Operating Systems

| OS | Versions | Architecture | Config Method | Config Path |
|----|----------|-------------|---------------|-------------|
| RHEL 7 | 7.x | x86_64 | `ifcfg-*` via Augeas | `/etc/sysconfig/network-scripts/ifcfg-<iface>` |
| RHEL 8 | 8.x | x86_64 | `ifcfg-*` or NM keyfile via Augeas | Auto-detected: NM keyfile preferred, `ifcfg` fallback |
| RHEL 9 | 9.x | x86_64 | NM keyfile via Augeas | `/etc/NetworkManager/system-connections/<conn>.nmconnection` |
| RHEL 10 | 10.x | x86_64 | NM keyfile via Augeas | `/etc/NetworkManager/system-connections/<conn>.nmconnection` |
| Windows Server 2016 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2019 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2022 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2025 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows 11 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |

!!! warning "Unsupported operating systems"
    Non-RHEL Linux distributions (Ubuntu, Fedora, SUSE, etc.) are not
    supported. RHEL is version-gated to major versions 7–10; other RHEL
    versions are rejected. Windows is dispatched by OS family with no
    version gate — Server 2016–2025 and Windows 11 are the tested and
    supported matrix. The init container exits with a non-zero
    code if it detects an unsupported OS, which prevents the VM from
    booting. Remove the `soteria.io/ip-rewrite` label to allow the VM to
    start without IP rewriting.

---

## SecurityContextConstraints

The IP rewrite init container runs inside virt-launcher pods with a hardened
security context — no elevated capabilities are required.

### Security Context

The init container runs as the `qemu` user (UID 107) with all capabilities
dropped:

```yaml
securityContext:
  runAsUser: 107
  runAsNonRoot: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
```

The libguestfs `direct` backend launches a user-mode QEMU appliance; all
filesystem operations (mount, augeas, etc.) happen **inside the QEMU VM**,
not in the container. CRI-O's RuntimeDefault seccomp profile on OpenShift
allows the `unshare`/`mount`/`pivot_root` syscalls that the QEMU appliance
needs, so `CAP_SYS_ADMIN` is not required.

### SCC Allowed Capabilities

| Capability | Reason |
|------------|--------|
| `NET_BIND_SERVICE` | Inherited from the base virt-launcher SCC — allows binding to privileged ports. |
| `SYS_NICE` | Inherited from the base virt-launcher SCC — allows adjusting process scheduling priority. |

The chart-managed SCC grants the same capabilities virt-launcher already
needs (`NET_BIND_SERVICE`, `SYS_NICE`). It does not enable host networking.

### SCC Volume Types

The chart-managed SCC sets `volumes: ['*']` so virt-launcher can keep using
every volume type it already needs (PVC disks, `hostPath`, secrets, projected
tokens, and so on).

### Host Access Flags

| Flag | Value | Reason |
|------|-------|--------|
| `allowHostDirVolumePlugin` | `true` | Permits `hostPath` volumes required by virt-launcher for device and node-level access. |
| `allowHostNetwork` | `false` | Host networking is not required for the IP rewrite init container. |
| `allowHostPorts` | `false` | Not required. |
| `allowHostPID` | `false` | Not required. |
| `allowHostIPC` | `false` | Not required. |

### Binding Mechanism

On OpenShift, the chart creates:

1. A **SecurityContextConstraints** resource that lists those virt-launcher
   ServiceAccounts in its `users` field.
2. A **ClusterRole** granting the `use` verb on the SCC.
3. A **ClusterRoleBinding** that binds the ClusterRole to the same
   ServiceAccount subjects in the configured namespaces.

By default, the binding covers the `default` ServiceAccount in the release
namespace. Use `scc.namespaces` to extend coverage to all namespaces where
VMs with `soteria.io/ip-rewrite: "true"` run, and `scc.serviceAccountNames`
to specify the virt-launcher ServiceAccount names.

!!! tip "OpenShift vs. Vanilla Kubernetes"
    OpenShift clusters require `scc.enabled: true` so the chart creates the
    SCC resource and RBAC bindings. Vanilla Kubernetes can leave the default
    (`scc.enabled: false`) — there is no SCC API. Virt-launcher pods already
    use the same host access KubeVirt requires; the injected init container
    does not add privileged capabilities.

---

## Helm Values Reference

Complete reference for every configurable parameter in the IP rewrite Helm
chart (`charts/soteria-ip-rewrite/values.yaml`).

For the main Soteria chart values, see
[Soteria Helm Values](helm-values.md).

!!! info "Sub-chart integration"
    The IP rewrite chart can be installed **standalone** or as a sub-chart of the
    main Soteria chart.

    **Standalone** (for customers using a different DR orchestrator):
    ```bash
    helm install soteria-ip-rewrite soteria/soteria-ip-rewrite \
      --namespace soteria-ip-rewrite --create-namespace \
      --set scc.enabled=true  # OpenShift only
    ```

    **Sub-chart** (for Soteria users): enable in the parent chart with
    `soteria-ip-rewrite.enabled: true` in `charts/soteria/values.yaml`.
    The parent chart overrides `tls.issuerRef.name` with its own CA issuer.

    See the [Installation section](../usage/ip-rewrite.md#installation) in the
    usage guide for full details.

### Global

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `nameOverride` | string | `""` | Override the chart name used in resource names and labels. Truncated to 63 characters. |
| `fullnameOverride` | string | `""` | Override the full release name used in resource names. |

---

### Webhook

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `webhook.image.repository` | string | `"quay.io/raffaelespazzoli/soteria-ip-rewrite-webhook"` | Container image for the IP rewrite webhook server (Go binary). |
| `webhook.image.tag` | string | `""` | Image tag. When empty, defaults to the chart's `appVersion`. |
| `webhook.image.pullPolicy` | string | `"IfNotPresent"` | Kubernetes image pull policy. |
| `webhook.replicas` | integer | `2` | Number of webhook server pods. |
| `webhook.resources.requests.cpu` | string | `"100m"` | CPU request for each webhook pod. |
| `webhook.resources.requests.memory` | string | `"128Mi"` | Memory request for each webhook pod. |
| `webhook.resources.limits.cpu` | string | `"500m"` | CPU limit for each webhook pod. |
| `webhook.resources.limits.memory` | string | `"256Mi"` | Memory limit for each webhook pod. |

---

### Init Container

The webhook injects this image as an init container into virt-launcher pods.
This is the guestfs-tools image built on CentOS Stream 9.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `initContainer.image.repository` | string | `"quay.io/raffaelespazzoli/soteria-ip-rewrite"` | Container image for the IP rewrite init container. Contains `guestfs-tools`, `augeas`, `hivex`, `python3-libguestfs`, `perl-hivex`, and `libguestfs-winsupport`. |
| `initContainer.image.tag` | string | `""` | Image tag. When empty, defaults to the chart's `appVersion`. |
| `initContainer.image.pullPolicy` | string | `"IfNotPresent"` | Image pull policy for the injected init container. |
| `initContainer.requestKVMDevice` | boolean | `true` | When `true`, the injected init container requests `devices.kubevirt.io/kvm: 1` for hardware-accelerated libguestfs. Set to `false` on nodes without `/dev/kvm` (TCG fallback). |

---

### TLS

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `tls.issuerRef.name` | string | `""` | cert-manager Issuer or ClusterIssuer name for webhook TLS. When empty, the chart creates a self-signed Issuer (if `tls.createSelfSigned` is `true`). |
| `tls.issuerRef.kind` | string | `"Issuer"` | Kind of the cert-manager issuer — `"Issuer"` (namespace-scoped) or `"ClusterIssuer"` (cluster-scoped). |
| `tls.createSelfSigned` | boolean | `true` | When `true` and `tls.issuerRef.name` is empty, the chart creates a self-signed Issuer. When `false`, `tls.issuerRef.name` must be set. |

---

### SecurityContextConstraints

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scc.enabled` | boolean | `false` | Enable SCC resource creation. Set to `true` on OpenShift; set to `false` on vanilla Kubernetes (no SCC API). |
| `scc.create` | boolean | `true` | When `true` and `scc.enabled` is `true`, the chart creates a dedicated SCC. Set to `false` if the virt-launcher SA already has access to the `kubevirt-controller` SCC. |
| `scc.serviceAccountNames` | list | `["default"]` | virt-launcher ServiceAccount names to bind the SCC to. Override with your actual virt-launcher SA names. |
| `scc.namespaces` | list | `[]` | Namespaces where VMs run. The SCC ClusterRoleBinding grants `use` to the `serviceAccountNames` in each of these namespaces. Empty list = release namespace only. |
| `scc.additionalSubjects` | list | `[]` | Extra RBAC subjects granted SCC `use` permission. Each entry is a complete RBAC subject object (`kind`, `name`, `namespace`, `apiGroup`). |

---

### Webhook Configuration

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `webhookConfig.failurePolicy` | string | `"Ignore"` | MutatingWebhookConfiguration failure policy. `"Ignore"` (fail-open) means VMs start without IP rewriting if the webhook is unavailable. `"Fail"` (fail-closed) blocks pod creation when the webhook is down. |
| `webhookConfig.namespaceSelector` | object | `{}` | Label selector for namespaces the webhook applies to. When empty, the webhook intercepts pods in all namespaces (filtered by the `objectSelector` label). |

---

## Known Limitations

- **IPv6** — Not supported. Only IPv4 addresses are handled.
- **DHCP-to-static conversion (RHEL)** — Not supported when an `ifcfg`
  interface is already DHCP. RHEL 8+ guests with no on-disk network config
  get a new NetworkManager keyfile instead.
- **DHCP-to-static conversion (Windows)** — A single-NIC guest with only a
  DHCP adapter is converted to static. Multi-NIC matching prefers an
  existing static adapter or a subnet match.
- **Guest hostname rewrite** — Not supported. Only IP address, gateway, and
  DNS servers are modified.
- **BitLocker / encrypted disks** — Encrypted Windows volumes cannot be
  inspected or rewritten. Decrypt the OS disk in the guest before failover.
- **Guest architecture** — The supported guest OS matrix is x86_64. The
  webhook image is `linux/amd64`, `linux/arm64`, and `linux/ppc64le`. The
  init container is `linux/amd64` and `linux/arm64` (no guestfs-tools for
  ppc64le). ARM Windows guests are not certified by OpenShift
  Virtualization and are untested.
- **Non-RHEL Linux** — Distributions such as Ubuntu, Fedora, or SUSE are not
  supported. Only RHEL 7–10 is handled on the Linux side.
