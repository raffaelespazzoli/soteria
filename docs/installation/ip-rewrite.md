# IP Rewrite Installation

The IP rewrite webhook is distributed as a **standalone Helm chart** that can
be installed independently of the main Soteria chart. This means you can use
the IP rewrite feature with **any DR orchestrator or workflow** — you do not
need the full Soteria platform.

---

## Prerequisites

- **cert-manager** — Required for TLS certificate provisioning on the webhook
  server.
- **KubeVirt or OpenShift Virtualization** — The webhook intercepts
  virt-launcher pods created by KubeVirt.
- **PVC-backed VM disks** — The init container modifies guest filesystems on
  PVC volumes. Container disks are not supported.

---

## Standalone Installation

Use this method if you run a different DR orchestrator (e.g., Red Hat DRM,
Kasten, Zerto, or custom automation) and only need the VM IP rewrite
capability.

### From the Helm Repository

```bash
# Add the Helm repository
helm repo add soteria https://raffaelespazzoli.github.io/soteria
helm repo update
```

**Vanilla Kubernetes:**

```bash
helm install soteria-ip-rewrite soteria/soteria-ip-rewrite \
  --namespace soteria-ip-rewrite \
  --create-namespace
```

**OpenShift** (with SecurityContextConstraints):

```bash
helm install soteria-ip-rewrite soteria/soteria-ip-rewrite \
  --namespace soteria-ip-rewrite \
  --create-namespace \
  --set scc.enabled=true \
  --set "scc.namespaces={vm-namespace-1,vm-namespace-2}"
```

Replace `vm-namespace-1,vm-namespace-2` with the namespaces where your VMs
run.

### From Source (Air-Gapped / Development)

```bash
helm install soteria-ip-rewrite charts/soteria-ip-rewrite/ \
  --namespace soteria-ip-rewrite \
  --create-namespace
```

---

## What Gets Deployed

The standalone chart deploys:

| Resource | Description |
|----------|-------------|
| **Deployment** | Go-based mutating admission webhook server (2 replicas by default) |
| **Service** | ClusterIP service for the webhook endpoint |
| **MutatingWebhookConfiguration** | Intercepts virt-launcher pod creation for labeled VMs |
| **Certificate + Issuer** | TLS certificates via cert-manager (self-signed by default) |
| **SCC + RBAC** | *(OpenShift only, when `scc.enabled: true`)* SecurityContextConstraints and bindings for virt-launcher SAs |

It does **not** deploy the Soteria controller, ScyllaDB, the UI console
plugin, or any other Soteria component.

---

## As a Sub-Chart of Soteria

If you already use the Soteria DR orchestrator, enable the IP rewrite feature
in the parent chart values:

```yaml
# In your Soteria values.yaml
soteria-ip-rewrite:
  enabled: true
  scc:
    enabled: true        # OpenShift only
    namespaces:
      - my-vm-namespace
```

Or via the command line:

```bash
helm upgrade soteria charts/soteria \
  --set soteria-ip-rewrite.enabled=true \
  --set soteria-ip-rewrite.scc.enabled=true
```

The parent chart wires the TLS issuer so the sub-chart shares the same CA.

---

## Next Steps

- [IP Rewrite Usage Guide](../usage/ip-rewrite.md) — Configure VMs for IP
  rewriting, verify results, troubleshoot issues
- [IP Rewrite Reference](../reference/ip-rewrite.md) — Full Helm values
  reference, annotation/label tables, supported OS matrix
- [IP Rewrite Architecture](../architecture/ip-rewrite.md) — How the webhook,
  init container, and guest handlers work together
