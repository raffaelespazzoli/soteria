# Prerequisites

Before installing Soteria, verify that your environment meets the following
requirements. Soteria orchestrates disaster recovery for OpenShift
Virtualization workloads across two Kubernetes clusters, so both the
**primary** and **secondary** clusters must satisfy every prerequisite listed
below.

## Kubernetes Cluster

Soteria requires **two** Kubernetes clusters connected by a cross-site network
fabric. Each cluster must run:

| Requirement | Minimum Version | Notes |
|-------------|-----------------|-------|
| OpenShift Container Platform | 4.x | Required for OLM lifecycle management and Console plugin integration |
| Kubernetes API server | 1.27+ | Aggregated API server support is required |

!!! tip "Verification"
    ```bash
    # Check Kubernetes version on each cluster
    kubectl version

    # Confirm the cluster supports aggregated API services
    kubectl api-resources | grep apiregistration
    ```

## Storage

Soteria manages volume replication through the
[CSI-Addons](https://github.com/csi-addons/kubernetes-csi-addons)
`VolumeReplication` and `VolumeGroupReplication` APIs. Your storage subsystem
must provide a CSI driver that implements these CRDs.

### Required CRDs

The following CRDs must be installed on **both** clusters:

| CRD | API Group | Provided By |
|-----|-----------|-------------|
| `VolumeReplication` | `replication.storage.openshift.io` | CSI-Addons operator or storage vendor CSI driver |
| `VolumeGroupReplication` | `replication.storage.openshift.io` | CSI-Addons operator or storage vendor CSI driver |
| `VolumeReplicationClass` | `replication.storage.openshift.io` | CSI-Addons operator or storage vendor CSI driver |
| `VolumeGroupReplicationClass` | `replication.storage.openshift.io` | CSI-Addons operator or storage vendor CSI driver |

### Pre-Configuration

Before installing Soteria you must:

1. **Create `VolumeReplicationClass`** resources matching your storage backend
   (e.g., ODF/Ceph RBD, Dell PowerStore).
2. **Create `VolumeGroupReplicationClass`** resources for volume group
   operations.
3. **Verify that replication is active** between the primary and secondary
   clusters at the storage layer.

!!! warning "Homogeneous Storage Only"
    Soteria requires the same storage vendor on both clusters. Cross-vendor
    replication (e.g., Dell-to-ODF) is **not** supported. Supported
    configurations include ODF-to-ODF, Dell-to-Dell, and similar
    same-vendor pairings.

!!! tip "Verification"
    ```bash
    # Confirm VolumeReplication CRDs are installed
    kubectl get crd volumereplications.replication.storage.openshift.io
    kubectl get crd volumegroupreplications.replication.storage.openshift.io
    kubectl get crd volumereplicationclasses.replication.storage.openshift.io
    kubectl get crd volumegroupreplicationclasses.replication.storage.openshift.io

    # List configured replication classes
    kubectl get volumereplicationclass
    kubectl get volumegroupreplicationclass
    ```

### StorageClass

A `StorageClass` backed by a CSI driver with replication support must be
available. Soteria determines the storage driver automatically from the
PVC's `StorageClass` — no additional driver-selection configuration is
required.

!!! tip "Verification"
    ```bash
    kubectl get storageclass
    ```

## KubeVirt (OpenShift Virtualization)

Soteria orchestrates disaster recovery for virtual machines managed by
[KubeVirt](https://kubevirt.io/). KubeVirt must be installed and operational
on **both** clusters.

| Requirement | Minimum Version | Notes |
|-------------|-----------------|-------|
| KubeVirt | 1.8+ | Provides the `VirtualMachine` CRD (`kubevirt.io/v1`) |

### Required CRDs

| CRD | API Group | Provided By |
|-----|-----------|-------------|
| `VirtualMachine` | `kubevirt.io` | KubeVirt / OpenShift Virtualization operator |

!!! info "VM Pre-existence"
    Soteria does **not** create VMs or manage PVC bindings. Virtual machines
    must already exist on both clusters with their PVC attachments configured.
    Soteria transitions volumes to the `Source` role and starts VMs during
    failover — it does not provision infrastructure.

!!! tip "Verification"
    ```bash
    # Confirm KubeVirt is installed
    kubectl get crd virtualmachines.kubevirt.io

    # Check that VMs are present
    kubectl get vm -A
    ```

## cert-manager

[cert-manager](https://cert-manager.io/) provides TLS certificate lifecycle
management. Soteria requires it for:

- **ScyllaDB mTLS** — mutual TLS authentication between the Soteria API
  server and the ScyllaDB cluster
- **Aggregated API server serving certificate** — TLS for the Soteria
  extension API endpoint
- **Webhook certificates** — TLS for the admission webhook server

| Requirement | Minimum Version | Notes |
|-------------|-----------------|-------|
| cert-manager | 1.20+ | Tested with v1.20.3 |

### Pre-Configuration

After installing cert-manager, create an `Issuer` or `ClusterIssuer` that
Soteria will reference for certificate generation:

```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-internal
  namespace: soteria
spec:
  selfSigned: {}
```

!!! warning "Production Issuers"
    The self-signed example above is suitable for development. For production,
    use a CA-backed `Issuer` or `ClusterIssuer` with your organization's PKI.
    Both clusters should trust the same root CA if ScyllaDB spans sites.

!!! tip "Verification"
    ```bash
    # Confirm cert-manager is running
    kubectl get pods -n cert-manager

    # Check that CRDs are installed
    kubectl get crd certificates.cert-manager.io
    kubectl get crd issuers.cert-manager.io

    # Verify your Issuer is ready
    kubectl get issuer -n soteria
    ```

## ScyllaDB

Soteria uses [ScyllaDB](https://www.scylladb.com/) as the cross-site shared
state store backing the aggregated API server. You have two options:

=== "Managed (scylla-operator)"

    Install the **scylla-operator** to manage ScyllaDB clusters declaratively.

    | Requirement | Minimum Version | Notes |
    |-------------|-----------------|-------|
    | scylla-operator | 1.21+ | Tested with v1.21.0; available via OperatorHub |

    The scylla-operator manages the ScyllaDB lifecycle including deployment,
    scaling, and upgrades. Soteria can auto-create the keyspace when
    `--scylladb-dc-replication` is set.

    **Verification:**

    ```bash
    # Confirm scylla-operator is running
    kubectl get pods -n scylla-operator

    # Check that ScyllaCluster CRD is available
    kubectl get crd scyllaclusters.scylla.scylladb.com
    ```

=== "Bring Your Own (BYO)"

    Operate your own ScyllaDB cluster and provide Soteria with connection
    details. The keyspace must already exist before Soteria starts.

    **Requirements:**

    - ScyllaDB cluster accessible from both Kubernetes clusters
    - `NetworkTopologyStrategy` with replication factor ≥ 2 per datacenter
    - The keyspace must already exist (Soteria creates the required tables
      automatically on startup)

### ScyllaDB Connection Parameters

Soteria connects to ScyllaDB via the following flags on the `soteria`
binary:

| Flag | Default | Description |
|------|---------|-------------|
| `--scylladb-contact-points` | `localhost:9042` | Comma-separated ScyllaDB contact points |
| `--scylladb-keyspace` | `soteria` | Keyspace name |
| `--scylladb-local-dc` | _(empty)_ | Local datacenter name for DC-aware routing (e.g., `dc1`) |
| `--scylladb-dc-replication` | _(empty)_ | Auto-create keyspace with `NetworkTopologyStrategy`. Comma-separated `dc:rf` pairs (e.g., `dc1:2,dc2:2`). When empty, the keyspace must already exist |

### ScyllaDB TLS (Required for Production)

All Soteria-to-ScyllaDB communication must be encrypted via mTLS. The
following flags configure TLS:

| Flag | Description |
|------|-------------|
| `--scylladb-tls-cert` | Path to the TLS client certificate |
| `--scylladb-tls-key` | Path to the TLS client key |
| `--scylladb-tls-ca` | Path to the TLS CA certificate |
| `--scylladb-tls-server-name` | TLS server name for certificate verification (overrides hostname from contact points) |

!!! warning "TLS is Mandatory"
    In production deployments, all four TLS flags must be provided. ScyllaDB
    connections switch to TLS port `9142` when TLS is configured. Certificates
    should be managed by cert-manager with a shared CA across both
    datacenters.

## Cross-Site Networking

The two Kubernetes clusters must be connected by a network fabric that allows:

- **Pod-to-pod communication** across clusters (for ScyllaDB gossip and data
  replication)
- **Service discovery** across clusters (for ScyllaDB seed node resolution)

Soteria supports two networking options:

=== "Submariner"

    [Submariner](https://submariner.io/) provides cross-cluster connectivity
    with the Multicluster Services (MCS) API.

    **Requirements:**

    - Submariner installed and connected between both clusters
    - MCS API active (`ServiceExport` / `ServiceImport` CRDs available)
    - The Soteria namespace `ServiceExport` configured for ScyllaDB service
      discovery

    **Verification:**

    ```bash
    # Check Submariner status
    kubectl get pods -n submariner-operator

    # Verify MCS API is available
    kubectl get crd serviceexports.multicluster.x-k8s.io
    kubectl get crd serviceimports.multicluster.x-k8s.io
    ```

=== "Cilium Cluster Mesh"

    [Cilium Cluster Mesh](https://docs.cilium.io/en/stable/network/clustermesh/)
    provides cross-cluster networking with global service annotations.

    **Requirements:**

    - Cilium installed as the CNI on both clusters
    - Cluster Mesh enabled and connected between both clusters
    - ScyllaDB services annotated with `service.cilium.io/global: "true"` for
      cross-DC pod routing

    **Verification:**

    ```bash
    # Check Cilium Cluster Mesh status
    cilium clustermesh status

    # Verify connectivity
    cilium connectivity test
    ```

## Version Compatibility Matrix

The following versions have been validated together in the Soteria test
infrastructure:

| Component | Tested Version | go.mod Dependency |
|-----------|---------------|-------------------|
| KubeVirt API | v1.8.1 | `kubevirt.io/api v1.8.1` |
| CSI-Addons | v0.14.0 | `github.com/csi-addons/kubernetes-csi-addons v0.14.0` |
| cert-manager | v1.20.3 | — (cluster prerequisite) |
| scylla-operator | v1.21.0 | — (cluster prerequisite) |
| ScyllaDB Go driver | v1.7.0 | `github.com/gocql/gocql v1.7.0` |
| ScyllaDB CDC library | v1.2.0 | `github.com/scylladb/scylla-cdc-go v1.2.0` |

## External API Dependencies Summary

The table below lists every external API group that the Soteria controller
requires RBAC access to, derived from the controller RBAC markers and the
generated `ClusterRole`:

| API Group | Resources | Used By |
|-----------|-----------|---------|
| `kubevirt.io` | `virtualmachines` | DRPlan controller (VM discovery), DRExecution controller (VM lifecycle) |
| `replication.storage.openshift.io` | `volumereplications`, `volumegroupreplications` | DRPlan controller (replication management), DRExecution controller (status monitoring), ShadowPV publisher, VolumeReplication controller |
| `ceph.rook.io` | `cephblockpools` | ShadowPV consumer controller (pool discovery for PV creation) |
| `storage.k8s.io` | `storageclasses` | DRExecution controller (driver selection) |
| `soteria.io` | `drplans`, `drexecutions`, `shadowpvs` | Soteria's own CRDs (installed automatically) |
| _(core)_ | `persistentvolumeclaims`, `persistentvolumes`, `namespaces`, `secrets`, `events` | Various controllers (volume tracking, namespace lookup, event recording) |

!!! note "Rook-Ceph Dependency"
    The `ceph.rook.io/cephblockpools` dependency is used by the ShadowPV
    controller for cross-site PV publishing. If you are not using Rook-Ceph
    storage, this CRD may not be present — the ShadowPV feature requires it
    only for Ceph-backed deployments.

## CLI Tools (for Setup)

The following tools are needed during initial deployment and cluster
configuration. They are not required at runtime.

| Tool | Purpose |
|------|---------|
| `kubectl` | Kubernetes cluster management |
| `helm` | Installing cert-manager, scylla-operator, and Soteria |
| `kustomize` | Applying Soteria overlays (alternative to Helm) |
| `jq` | Used by setup scripts for JSON processing |

## Checklist

Use this checklist to confirm your environment is ready:

- [ ] Two Kubernetes/OpenShift clusters running and accessible
- [ ] Cross-site networking operational (Submariner or Cilium Cluster Mesh)
- [ ] CSI-Addons `VolumeReplication` and `VolumeGroupReplication` CRDs installed
- [ ] `VolumeReplicationClass` and `VolumeGroupReplicationClass` resources created
- [ ] Storage replication active between clusters
- [ ] KubeVirt / OpenShift Virtualization installed and VMs present on both clusters
- [ ] cert-manager installed with an `Issuer` or `ClusterIssuer` configured
- [ ] ScyllaDB available (via scylla-operator or BYO) with mTLS configured
- [ ] `helm` and `kubectl` available on your workstation
