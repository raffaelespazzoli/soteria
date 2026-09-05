# Helm Installation

This guide walks through installing Soteria on a two-cluster environment
using the Helm chart. It covers all deployment modes — managed ScyllaDB
(seed and joining sites), BYO (bring-your-own) ScyllaDB, and the
automated install script.

!!! info "Prerequisites First"
    Complete every item in the [Prerequisites](prerequisites.md) checklist
    before proceeding. In particular, verify that **cert-manager**,
    **scylla-operator** (if using managed mode), and your chosen
    **cross-site networking** (Submariner or Cilium Cluster Mesh) are
    operational on both clusters.

## Chart Location

The Soteria Helm chart lives in the repository under `charts/soteria/`.
Install from a local clone or from an OCI registry if published:

=== "Local Chart"

    ```bash
    git clone https://github.com/soteria-project/soteria.git
    cd soteria
    # The chart path is charts/soteria/
    ```

=== "OCI Registry"

    ```bash
    # When the chart is published to an OCI registry:
    helm pull oci://ghcr.io/soteria-project/charts/soteria --version 0.1.0
    ```

## Deployment Concepts

Soteria deploys as a **single binary** that runs both the aggregated API
server (serving `DRPlan` and `DRExecution` resources) and the DR workflow
controller. Every installation requires:

- A **site name** — a unique identifier for this cluster (e.g., `east`,
  `west`).
- A **site role** — `seed` for the first cluster (bootstraps the ScyllaDB
  cluster) or `joining` for subsequent clusters.
- A **ScyllaDB backend** — either chart-managed via scylla-operator
  (`scylladb.mode=managed`) or an external cluster you operate yourself
  (`scylladb.mode=external`).
- A **cert-manager Issuer** — referenced by all TLS `Certificate` resources
  the chart creates.

### Two-Cluster Topology

A standard Soteria deployment spans two clusters:

| Cluster | Site Role | Description |
|---------|-----------|-------------|
| **East** (primary) | `seed` | Bootstraps ScyllaDB, hosts the initial CA |
| **West** (secondary) | `joining` | Joins the existing ScyllaDB cluster via `externalSeeds` |

The seed site must be installed **first** because the joining site needs:

1. The cert-manager CA Secret copied from the seed (for cross-site mTLS
   trust).
2. The ScyllaDB seed address for multi-DC cluster joining.

---

## Install Script (Recommended)

The repository includes an automated install script that handles the full
two-cluster deployment sequence — including CA propagation and ScyllaDB
seed wiring:

```bash
scripts/install-soteria.sh \
  --east-context east \
  --west-context west \
  --chart ./charts/soteria
```

### Script Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--east-context` | Yes | — | `kubectl` context for the seed (east) cluster |
| `--west-context` | Yes | — | `kubectl` context for the joining (west) cluster |
| `--chart` | Yes (install) | — | Helm chart reference — local path or OCI URL |
| `--networking` | No | `submariner` | Cross-cluster networking: `submariner` or `cilium` |
| `--ui-mode` | No | `console-plugin` | UI mode: `console-plugin`, `standalone`, or `none` |
| `--values-file` | No | — | Path to a custom Helm values file (passed as `-f`) |
| `--namespace` | No | `soteria` | Target Kubernetes namespace |
| `--uninstall` | No | — | Uninstall from both clusters (reverse order) |
| `--dry-run` | No | — | Print commands without executing them |

### What the Script Does

1. **Validates prerequisites** — checks for `helm`, `kubectl`, `jq`;
   verifies both clusters are reachable; confirms cert-manager, scylla-operator,
   and networking CRDs are present.
2. **Installs on the seed cluster** — runs `helm upgrade --install` with
   `site.role=seed` and waits for the controller rollout.
3. **Propagates the CA** — waits for the cert-manager CA Certificate to
   become Ready, then copies the CA Secret to the joining cluster.
4. **Installs on the joining cluster** — runs `helm upgrade --install` with
   `site.role=joining` and `scylladb.managed.externalSeeds` pointing to the
   seed ScyllaDB service.
5. **Verifies deployment** — confirms the controller Deployment is running
   on both clusters.

### Script Examples

```bash
# Install with Cilium networking and a custom values file
scripts/install-soteria.sh \
  --east-context east \
  --west-context west \
  --chart ./charts/soteria \
  --networking cilium \
  --values-file my-values.yaml

# Dry run — see what would be executed without making changes
scripts/install-soteria.sh \
  --east-context east \
  --west-context west \
  --chart ./charts/soteria \
  --dry-run

# Uninstall from both clusters
scripts/install-soteria.sh \
  --east-context east \
  --west-context west \
  --uninstall
```

---

## Manual Installation

If you prefer full control over each step, follow the manual procedure
below. This is equivalent to what the install script automates.

### Step 1: Create the cert-manager Issuer

Before installing the chart, ensure a cert-manager `Issuer` or
`ClusterIssuer` exists in the target namespace. The chart references it
via `tls.issuerRef`:

```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-ca
  namespace: soteria
spec:
  selfSigned: {}
```

!!! warning "Production Issuers"
    The self-signed example above is for development only. In production,
    use a CA-backed Issuer with your organization's PKI. Both clusters
    should trust the same root CA for ScyllaDB cross-site mTLS.

### Step 2: Install on the Seed Cluster

=== "Managed ScyllaDB (Seed)"

    Install Soteria with `site.role=seed`. The chart deploys a
    `ScyllaCluster` CR that the scylla-operator manages:

    ```bash
    helm upgrade --install soteria ./charts/soteria \
      --namespace soteria \
      --create-namespace \
      --set-string site.name=east \
      --set-string site.role=seed \
      --set-string scylladb.localDC=east \
      --set-string scylladb.dcReplication="east:2,west:2" \
      --set-string networking.mode=submariner \
      --wait \
      --timeout 10m
    ```

    This creates:

    - A **controller Deployment** (aggregated API server + workflow controller)
    - A **ScyllaCluster** CR with datacenter name `east`
    - **cert-manager Certificates** for the API server, webhook, metrics, and
      ScyllaDB mTLS
    - A **ServiceExport** for ScyllaDB cross-cluster service discovery
      (Submariner mode)
    - **RBAC resources** (ClusterRole, ClusterRoleBinding, leader-election
      Role/RoleBinding)

=== "BYO ScyllaDB"

    If you operate your own ScyllaDB cluster, use `scylladb.mode=external`:

    ```bash
    helm upgrade --install soteria ./charts/soteria \
      --namespace soteria \
      --create-namespace \
      --set-string site.name=east \
      --set-string site.role=seed \
      --set-string scylladb.mode=external \
      --set-string scylladb.external.contactPoints="scylla-east.example.com:9142" \
      --set-string scylladb.keyspace=soteria \
      --set-string scylladb.localDC=dc-east \
      --set scylladb.external.tls.enabled=true \
      --set-string scylladb.external.tls.secretName=my-scylladb-tls \
      --set-string networking.mode=submariner \
      --wait \
      --timeout 10m
    ```

    In external mode, **no ScyllaDB resources are rendered** — no
    ScyllaCluster, no ScyllaDB Certificate, no ServiceExport. The
    controller connects to your external cluster using the provided contact
    points and TLS Secret.

    !!! note "TLS Secret Format"
        The TLS Secret must contain `tls.crt`, `tls.key`, and `ca.crt`
        keys. Create it from your ScyllaDB client certificates:

        ```bash
        kubectl create secret generic my-scylladb-tls \
          --namespace soteria \
          --from-file=tls.crt=client.crt \
          --from-file=tls.key=client.key \
          --from-file=ca.crt=ca.crt
        ```

### Step 3: Wait for the Seed to Become Ready

Confirm the controller is running and the CA Certificate is ready:

```bash
# Watch controller pods
kubectl --context east -n soteria get pods -w

# Verify the controller Deployment is available
kubectl --context east -n soteria rollout status \
  deployment/soteria-controller-manager --timeout=300s

# Check the CA Certificate (managed mode)
kubectl --context east -n soteria get certificate
```

### Step 4: Copy the CA Secret (Managed Mode Only)

When using managed ScyllaDB, the seed cluster's cert-manager CA must be
copied to the joining cluster so both sites share the same trust root:

```bash
# Create the target namespace
kubectl --context west create namespace soteria --dry-run=client -o yaml \
  | kubectl --context west apply --server-side -f -

# Copy the CA Secret (strip metadata that shouldn't transfer)
kubectl --context east -n soteria get secret soteria-ca-key-pair -o json \
  | jq 'del(.metadata.resourceVersion, .metadata.uid,
            .metadata.creationTimestamp, .metadata.managedFields,
            .metadata.ownerReferences,
            .metadata.annotations["cert-manager.io/certificate-name"])' \
  | kubectl --context west -n soteria apply --server-side --force-conflicts -f -
```

!!! tip "BYO ScyllaDB"
    If you are using `scylladb.mode=external`, you manage your own CA and
    certificates. Skip this step.

### Step 5: Install on the Joining Cluster

=== "Managed ScyllaDB (Joining)"

    Install with `site.role=joining` and provide the seed ScyllaDB address
    in `externalSeeds`:

    ```bash
    # Resolve the seed address based on networking mode
    # Submariner: *.svc.clusterset.local
    # Cilium:     *.svc.cluster.local (global service annotation)

    helm upgrade --install soteria ./charts/soteria \
      --namespace soteria \
      --create-namespace \
      --set-string site.name=west \
      --set-string site.role=joining \
      --set-string scylladb.localDC=west \
      --set-string scylladb.dcReplication="east:2,west:2" \
      --set-string networking.mode=submariner \
      --set-string scylladb.managed.externalSeeds[0]=soteria-scylladb-client.soteria.svc.clusterset.local \
      --wait \
      --timeout 10m
    ```

    !!! warning "External Seeds Are Required"
        The chart validates that `scylladb.managed.externalSeeds` is
        non-empty when `site.role=joining`. Omitting it causes a template
        rendering error.

    **Seed DNS by networking mode:**

    | Networking | External Seeds DNS |
    |------------|--------------------|
    | Submariner | `soteria-scylladb-client.soteria.svc.clusterset.local` |
    | Cilium | `soteria-scylladb-client.soteria.svc.cluster.local` |

=== "BYO ScyllaDB (Joining)"

    ```bash
    helm upgrade --install soteria ./charts/soteria \
      --namespace soteria \
      --create-namespace \
      --set-string site.name=west \
      --set-string site.role=joining \
      --set-string scylladb.mode=external \
      --set-string scylladb.external.contactPoints="scylla-west.example.com:9142" \
      --set-string scylladb.keyspace=soteria \
      --set-string scylladb.localDC=dc-west \
      --set scylladb.external.tls.enabled=true \
      --set-string scylladb.external.tls.secretName=my-scylladb-tls \
      --set-string networking.mode=submariner \
      --wait \
      --timeout 10m
    ```

### Step 6: Verify the Installation

Run these checks on **both** clusters:

```bash
# Controller pods are running
kubectl --context east -n soteria get pods
kubectl --context west -n soteria get pods

# APIService is Available
kubectl --context east get apiservice v1alpha1.soteria.io
kubectl --context west get apiservice v1alpha1.soteria.io

# DRPlan API is reachable
kubectl --context east get drplans -A
kubectl --context west get drplans -A

# ScyllaDB is healthy (managed mode only)
kubectl --context east -n soteria get scyllacluster
kubectl --context west -n soteria get scyllacluster
```

---

## Key Values Walkthrough

This section explains the most important configuration values. For a
complete flat reference of every field, see the
[Helm Values Reference](../reference/helm-values.md).

### Site Configuration

```yaml
site:
  name: "east"        # Unique per cluster — passed as --site-name
  role: seed           # "seed" or "joining"
```

`site.name` identifies this Soteria instance in the DR topology.
The controller uses it to determine reconcile ownership (which site runs
which DR execution phases). `site.role` controls ScyllaDB bootstrap
behavior — `seed` creates a new cluster, `joining` connects to an
existing one.

!!! warning "Required Values"
    Both `site.name` and `site.role` are **required**. The chart fails
    to render if `site.name` is empty.

### ScyllaDB Mode

```yaml
scylladb:
  mode: managed    # "managed" or "external"
  keyspace: soteria
  localDC: ""      # Required for managed mode
  dcReplication: "" # e.g., "east:2,west:2" — auto-creates keyspace
```

- **`managed`** — The chart deploys a `ScyllaCluster` CR. The
  scylla-operator handles provisioning, scaling, and upgrades.
- **`external`** — No ScyllaDB resources are rendered. You provide
  connection details via `scylladb.external.*`.

`localDC` is **required** in managed mode (used as the datacenter name in
the ScyllaCluster spec). `dcReplication` sets up the keyspace with
`NetworkTopologyStrategy` — when empty, the keyspace must already exist.

### Managed ScyllaDB Settings

```yaml
scylladb:
  managed:
    version: "2026.1.3"
    rack:
      name: rack1
    members: 1
    storage:
      capacity: 5Gi
      storageClassName: ""
    developerMode: true
    resources:
      requests:
        cpu: 100m
        memory: 512Mi
      limits:
        cpu: "1"
        memory: 1Gi
    externalSeeds: []    # Required when site.role=joining
```

`developerMode: true` relaxes production constraints (suitable for
development and CI). Set to `false` for production with appropriate
resource limits and multi-node racks.

### External ScyllaDB Settings

```yaml
scylladb:
  external:
    contactPoints: "scylla.example.com:9142"
    tls:
      enabled: false
      secretName: ""
      serverName: ""
```

`contactPoints` is **required** when `scylladb.mode=external` (the chart
validates this). When `tls.enabled=true`, the `secretName` Secret is
mounted into the controller pod and used for mTLS authentication.

### TLS Configuration

```yaml
tls:
  issuerRef:
    name: soteria-ca
    kind: Issuer      # "Issuer" or "ClusterIssuer"
```

All cert-manager `Certificate` resources in the chart reference this
issuer — including the API server serving cert, webhook cert, metrics
cert, and ScyllaDB client cert (managed mode). The `tls.issuerRef.name`
is **required**.

### Controller Settings

```yaml
controller:
  image:
    repository: quay.io/raffaelespazzoli/soteria
    tag: ""               # Defaults to Chart.appVersion
    pullPolicy: IfNotPresent
  replicas: 1
  resources:
    requests:
      cpu: 10m
      memory: 64Mi
    limits:
      cpu: 500m
      memory: 128Mi
  leaderElection:
    enabled: true
  apiserver:
    port: 6443
  serviceAccount:
    name: ""              # Auto-generated when empty
```

All replicas serve the aggregated API. Leader election controls which
replica runs the workflow engine reconciliation loops.

### Networking Mode

```yaml
networking:
  mode: submariner    # "submariner" or "cilium"
```

This controls how ScyllaDB cross-cluster communication is configured:

- **`submariner`** — Uses `ServiceExport`/`ServiceImport` (MCS API) for
  service discovery. The chart renders a `ServiceExport` for the ScyllaDB
  client service. ScyllaCluster `broadcastOptions` use `ServiceClusterIP`.
- **`cilium`** — Uses Cilium ClusterMesh global services. No
  `ServiceExport` is rendered. ScyllaCluster `broadcastOptions` use
  `PodIP`. The ScyllaDB headless service requires the annotation
  `service.cilium.io/global: "true"` for cross-DC pod routing — this
  must be applied to the Service created by the scylla-operator (not the
  ScyllaCluster CR itself) after the operator provisions the cluster.

### UI Mode

```yaml
ui:
  mode: console-plugin    # "console-plugin" | "standalone" | "none"
```

- **`console-plugin`** — Registers as an OpenShift dynamic console plugin.
  Requires OpenShift with the `ConsolePlugin` CRD.
- **`standalone`** — Deploys a standalone web UI with a Gateway API
  `HTTPRoute`. Suitable for non-OpenShift clusters.
- **`none`** — No UI is deployed. API-only installation.

---

## Upgrading

To upgrade Soteria, re-run `helm upgrade` on each cluster. Upgrade the
**seed** cluster first, then the **joining** cluster:

=== "Using the Install Script"

    The install script uses `helm upgrade --install`, so re-running it
    with updated values or a newer chart version performs an upgrade:

    ```bash
    scripts/install-soteria.sh \
      --east-context east \
      --west-context west \
      --chart ./charts/soteria
    ```

=== "Manual Upgrade"

    ```bash
    # Upgrade seed first
    helm upgrade soteria ./charts/soteria \
      --namespace soteria \
      --reuse-values \
      --set controller.image.tag=0.2.0 \
      --wait \
      --timeout 10m \
      --kube-context east

    # Then upgrade joining
    helm upgrade soteria ./charts/soteria \
      --namespace soteria \
      --reuse-values \
      --set controller.image.tag=0.2.0 \
      --wait \
      --timeout 10m \
      --kube-context west
    ```

!!! tip "Check Diff Before Upgrading"
    Use `helm diff` to preview changes before applying:

    ```bash
    helm diff upgrade soteria ./charts/soteria \
      --namespace soteria \
      --reuse-values \
      --kube-context east
    ```

    Install the plugin: `helm plugin install https://github.com/databus23/helm-diff`

### Upgrade Notes

- **Seed before joining** — always upgrade the seed cluster first to
  avoid ScyllaDB schema incompatibilities.
- **`--reuse-values`** — preserves your current configuration. Omit it
  if you want to reset to chart defaults.
- **CRD updates** — Helm does not upgrade CRDs automatically. If a new
  chart version updates CRD schemas, apply them manually before upgrading:

    ```bash
    kubectl apply --server-side -f charts/soteria/crds/  # if CRDs are shipped
    ```

---

## Uninstalling

Remove Soteria from both clusters by uninstalling the **joining** cluster
first, then the **seed**:

=== "Using the Install Script"

    ```bash
    scripts/install-soteria.sh \
      --east-context east \
      --west-context west \
      --uninstall
    ```

    The script uninstalls in reverse order (joining → seed), cleans up
    the CA Secret, and preserves the namespaces.

=== "Manual Uninstall"

    ```bash
    # Uninstall joining cluster first
    helm uninstall soteria \
      --namespace soteria \
      --kube-context west \
      --wait

    # Then uninstall seed cluster
    helm uninstall soteria \
      --namespace soteria \
      --kube-context east \
      --wait

    # Clean up CA Secrets
    kubectl --context west -n soteria delete secret soteria-ca-key-pair --ignore-not-found
    kubectl --context east -n soteria delete secret soteria-ca-key-pair --ignore-not-found
    ```

### Post-Uninstall Cleanup

Helm does not remove namespaces or CRDs. To fully clean up:

```bash
# Remove namespaces (deletes all remaining resources)
kubectl --context east delete namespace soteria
kubectl --context west delete namespace soteria

# Remove Soteria CRDs if no longer needed
kubectl delete crd drplans.soteria.io drexecutions.soteria.io shadowpvs.soteria.io
```

!!! danger "Data Loss"
    Deleting the namespace removes the ScyllaDB `PersistentVolumeClaim`
    resources and their data. Ensure you no longer need the DR state
    before proceeding.

---

## Troubleshooting

### Controller Pod Not Starting

```bash
# Check pod status and events
kubectl -n soteria describe pod -l control-plane=controller-manager

# View controller logs
kubectl -n soteria logs deployment/soteria-controller-manager -c manager --tail=50
```

Common causes:

- **Missing cert-manager Issuer** — the Certificate resources fail to
  issue, so the TLS Secrets don't exist and the pod can't mount them.
- **ScyllaDB not ready** — the controller retries ScyllaDB connections on
  startup. Check that the ScyllaCluster is healthy or that external
  contact points are reachable.

### Chart Validation Errors

The chart includes a `_validation.tpl` template that fails fast on
misconfigured values:

| Error | Cause | Fix |
|-------|-------|-----|
| `site.name is required` | `site.name` is empty | Set `--set-string site.name=<name>` |
| `site.role must be 'seed' or 'joining'` | Invalid role value | Use `seed` or `joining` |
| `tls.issuerRef.name is required` | Missing TLS issuer | Set `--set-string tls.issuerRef.name=<issuer>` |
| `scylladb.external.contactPoints is required...` | External mode without contact points | Set `--set-string scylladb.external.contactPoints=<host:port>` |
| `scylladb.managed.externalSeeds must not be empty when site.role=joining` | Joining without seeds | Set `--set-string scylladb.managed.externalSeeds[0]=<seed-dns>` |

### APIService Not Available

If `kubectl get apiservice v1alpha1.soteria.io` shows `False` for
`Available`:

```bash
# Check the APIService status
kubectl get apiservice v1alpha1.soteria.io -o yaml

# The service must be resolvable and the controller pod healthy
kubectl -n soteria get svc,endpoints
```
