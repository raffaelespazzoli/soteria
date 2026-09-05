# Helm Values Reference

Complete reference for every configurable parameter in the Soteria Helm chart
(`charts/soteria/values.yaml`).

For step-by-step installation instructions, see the
[Helm Installation](../installation/helm.md) guide.

---

## Global

Top-level overrides that control Kubernetes resource naming.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `nameOverride` | string | `""` | Override the chart name used in resource names and labels. Truncated to 63 characters. |
| `fullnameOverride` | string | `""` | Override the full release name (`<release>-<chart>`) used in resource names. When set, replaces the computed name entirely. Truncated to 63 characters. |

---

## Site

Each Soteria instance belongs to a named site with a role in the DR topology.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `site.name` | string | `""` | **Required.** Unique identifier for this site. Passed as `--site-name` to the controller. Must be set per-cluster; the controller uses it to determine reconcile ownership. |
| `site.role` | string | `"seed"` | This site's initial role in the ScyllaDB cluster. `"seed"` bootstraps a new ScyllaDB cluster; `"joining"` connects to an existing seed. |

!!! info "Validation"
    `site.name` is required — the chart fails to render if it is empty.
    `site.role` must be `"seed"` or `"joining"`.

---

## Controller

Governs the single-binary deployment that runs both the aggregated API server
(serving DRPlan / DRExecution) and the workflow-engine controller.

### Image

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controller.image.repository` | string | `"quay.io/raffaelespazzoli/soteria"` | Container image for the Soteria controller. |
| `controller.image.tag` | string | `""` | Image tag. When empty, defaults to the chart's `appVersion`. |
| `controller.image.pullPolicy` | string | `"IfNotPresent"` | Kubernetes [image pull policy](https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy). |

### Replicas & Resources

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controller.replicas` | integer | `1` | Number of controller pods. All replicas serve the aggregated API; leader election controls which replica runs the workflow engine. |
| `controller.resources.requests.cpu` | string | `"10m"` | CPU request for the controller container. |
| `controller.resources.requests.memory` | string | `"64Mi"` | Memory request for the controller container. |
| `controller.resources.limits.cpu` | string | `"500m"` | CPU limit for the controller container. |
| `controller.resources.limits.memory` | string | `"128Mi"` | Memory limit for the controller container. |

### Leader Election

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controller.leaderElection.enabled` | boolean | `true` | Enable leader election for the workflow engine. When `true`, only one replica runs reconciliation loops; all replicas serve the aggregated API. |

### API Server

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controller.apiserver.port` | integer | `6443` | Port for the aggregated API server that serves `DRPlan` and `DRExecution` resources. |

### Service Account

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controller.serviceAccount.name` | string | `""` | Override the default service account name. When empty, the chart generates a name using the fullname template (`<fullname>`). |

---

## TLS

References a [cert-manager](https://cert-manager.io/) Issuer or ClusterIssuer
used for all TLS certificates: the aggregated API server serving cert, ScyllaDB
client mTLS, metrics, and webhook certs.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `tls.issuerRef.name` | string | `"soteria-ca"` | **Required.** Name of the cert-manager Issuer or ClusterIssuer. |
| `tls.issuerRef.kind` | string | `"Issuer"` | Kind of the cert-manager issuer — `"Issuer"` (namespace-scoped) or `"ClusterIssuer"` (cluster-scoped). |

!!! info "Validation"
    `tls.issuerRef.name` is required — the chart fails to render if it is empty.

---

## ScyllaDB

Controls how the controller connects to its ScyllaDB backend. The `mode` field
selects between a chart-managed [ScyllaCluster](https://operator.docs.scylladb.com/)
CR or an external (pre-existing) ScyllaDB cluster.

### Mode

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scylladb.mode` | string | `"managed"` | `"managed"` deploys a ScyllaCluster CR via the scylla-operator. `"external"` connects to a pre-existing ScyllaDB cluster. **Controls which template set is rendered** — see [Conditional behavior](#scylladb-conditional-behavior) below. |

### Common Fields

These apply regardless of whether `scylladb.mode` is `"managed"` or `"external"`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scylladb.keyspace` | string | `"soteria"` | ScyllaDB keyspace for Soteria's KV store. Passed as `--scylladb-keyspace` to the controller. |
| `scylladb.localDC` | string | `""` | Local datacenter name for DC-aware routing (e.g. `"dc1"`). **Required when `scylladb.mode=managed`** (validated by the ScyllaCluster template). When set, passed as `--scylladb-local-dc` to the controller. |
| `scylladb.dcReplication` | string | `""` | Auto-create the keyspace with `NetworkTopologyStrategy`. Comma-separated `dc:rf` pairs (e.g. `"etl6:2,etl7:2"`). When empty, the keyspace must already exist. Passed as `--scylladb-dc-replication` to the controller when set. |

### Managed Mode

Active only when `scylladb.mode: managed`. These fields configure the
ScyllaCluster CR deployed by the chart.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scylladb.managed.version` | string | `"2026.1.3"` | ScyllaDB version for the managed cluster. |
| `scylladb.managed.rack.name` | string | `"rack1"` | Name of the default rack. |
| `scylladb.managed.members` | integer | `1` | Number of ScyllaDB nodes per rack. |
| `scylladb.managed.storage.capacity` | string | `"5Gi"` | Persistent volume size for each ScyllaDB node. |
| `scylladb.managed.storage.storageClassName` | string | `""` | StorageClass for ScyllaDB PVs. When empty, the cluster default StorageClass is used. |
| `scylladb.managed.developerMode` | boolean | `true` | Relax production constraints (single-node, lower resources). Suitable for development and CI environments only. |
| `scylladb.managed.resources.requests.cpu` | string | `"100m"` | CPU request for each ScyllaDB pod. |
| `scylladb.managed.resources.requests.memory` | string | `"512Mi"` | Memory request for each ScyllaDB pod. |
| `scylladb.managed.resources.limits.cpu` | string | `"1"` | CPU limit for each ScyllaDB pod. |
| `scylladb.managed.resources.limits.memory` | string | `"1Gi"` | Memory limit for each ScyllaDB pod. |
| `scylladb.managed.externalSeeds` | list | `[]` | Seed addresses for multi-DC cluster joining. Must not be empty when `site.role=joining` (validated at render time). |

### External Mode

Active only when `scylladb.mode: external`. These fields configure the
connection to a pre-existing ScyllaDB cluster.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scylladb.external.contactPoints` | string | `""` | **Required when `scylladb.mode=external`.** Comma-separated `host:port` pairs for the external cluster. Passed as `--scylladb-contact-points` to the controller. |
| `scylladb.external.tls.enabled` | boolean | `false` | Enable TLS for connections to the external cluster. When `true`, mounts the TLS Secret as a volume and passes cert/key/CA flags to the controller. |
| `scylladb.external.tls.secretName` | string | `""` | **Required when `scylladb.external.tls.enabled=true`.** Name of the Kubernetes Secret containing `tls.crt`, `tls.key`, and `ca.crt`. |
| `scylladb.external.tls.serverName` | string | `""` | TLS server name for certificate verification. Overrides the hostname derived from `contactPoints`. Passed as `--scylladb-tls-server-name` when set. |

### ScyllaDB Conditional Behavior

The `scylladb.mode` field controls which template set is rendered:

| Condition | Effect |
|-----------|--------|
| `scylladb.mode: managed` | Deploys a `ScyllaCluster` CR, ScyllaDB TLS `Certificate`, ScyllaDB config `ConfigMap`, and (when `networking.mode=submariner`) a `ServiceExport`. Mounts the managed ScyllaDB client-TLS volume in the controller. |
| `scylladb.mode: external` | Skips all managed ScyllaDB resources. The controller connects using `scylladb.external.contactPoints`. When `scylladb.external.tls.enabled=true`, mounts the user-provided TLS Secret in the controller. |
| `scylladb.mode: managed` + `site.role: joining` | The chart **fails** if `scylladb.managed.externalSeeds` is empty — joining sites must know at least one seed address. |
| `scylladb.mode: external` + (empty `contactPoints`) | The chart **fails** — `scylladb.external.contactPoints` is required. |
| `scylladb.mode: external` + `tls.enabled: true` + (empty `secretName`) | The chart **fails** — `scylladb.external.tls.secretName` is required when TLS is enabled. |

---

## Networking

Selects the cross-cluster networking layer for multi-site DR.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `networking.mode` | string | `"submariner"` | Cross-cluster networking backend. `"submariner"` uses Submariner `ServiceExport`/`Import` for service discovery. `"cilium"` uses Cilium ClusterMesh for direct pod-to-pod connectivity. |

!!! info "Template Effects"
    `networking.mode` influences the ScyllaCluster's `exposeOptions` (managed mode):

    - **`submariner`** — node broadcast type `ServiceClusterIP`, node service type `ClusterIP`, and a `ServiceExport` is created for the ScyllaDB client service.
    - **`cilium`** — node broadcast type `PodIP`, node service type `Headless`. No `ServiceExport` is created.

    Both Submariner and Cilium require their own operator installs but have no
    additional chart-level parameters today.

---

## UI

Controls which UI frontend is deployed alongside the controller.

### Mode

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ui.mode` | string | `"console-plugin"` | UI deployment strategy. `"console-plugin"` registers as an OCP dynamic console plugin. `"standalone"` deploys a standalone web UI behind a Gateway API `HTTPRoute`. `"none"` disables the UI entirely. |

!!! info "Validation"
    `ui.mode` must be `"console-plugin"`, `"standalone"`, or `"none"`.

### Console Plugin

Rendered only when `ui.mode: console-plugin`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ui.consolePlugin.image.repository` | string | `"quay.io/raffaelespazzoli/soteria-console-plugin"` | Container image for the console plugin nginx proxy. |
| `ui.consolePlugin.image.tag` | string | `""` | Image tag for the console plugin. When empty, defaults to the chart's `appVersion`. |

### Standalone UI

Rendered only when `ui.mode: standalone`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ui.standalone.image.repository` | string | `"quay.io/raffaelespazzoli/soteria-standalone-ui"` | Container image for the standalone UI. |
| `ui.standalone.image.tag` | string | `""` | Image tag for the standalone UI. When empty, defaults to the chart's `appVersion`. |
| `ui.standalone.gateway.name` | string | `""` | Name of the Gateway resource to attach the `HTTPRoute` to. **When empty, no `HTTPRoute` is created.** |
| `ui.standalone.gateway.namespace` | string | `""` | Namespace of the Gateway resource. Defaults to the release namespace when empty. Set explicitly for cross-namespace Gateway references. |
| `ui.standalone.gateway.hostname` | string | `""` | Optional hostname for the `HTTPRoute`. When set, adds a `hostnames` filter to prevent catch-all routing on shared Gateways. |

### UI Conditional Behavior

| Condition | Effect |
|-----------|--------|
| `ui.mode: console-plugin` | Deploys the console plugin Deployment, Service, `ConsolePlugin` registration, TLS Certificate, and nginx ConfigMap. |
| `ui.mode: standalone` | Deploys the standalone UI Deployment, Service, ServiceAccount, ClusterRole, and ClusterRoleBinding. |
| `ui.mode: standalone` + `gateway.name` set | Additionally creates an `HTTPRoute` attached to the named Gateway. |
| `ui.mode: none` | No UI resources are created. |
