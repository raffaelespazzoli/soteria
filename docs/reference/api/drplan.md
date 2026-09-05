# DRPlan API Reference

**API Group:** `soteria.io/v1alpha1`
**Resource:** `drplans`
**Scope:** Namespaced
**Served by:** Aggregated API Server (ScyllaDB-backed)

The `DRPlan` resource defines a disaster recovery plan for a set of VMs
selected by the `soteria.io/drplan` label. It describes which storage
driver handles volume replication, which clusters serve as primary and
secondary sites, and how VMs are grouped and executed in waves.

!!! info "Not a standard CRD"
    DRPlan is served by the Soteria aggregated API server, not a
    conventional CRD. There is no manifest in `config/crd/bases/`.
    Validation is two-layered: the aggregated API server's strategy
    pipeline runs field-level checks, and a validating admission webhook
    provides defense-in-depth.

---

## Resource Structure

```yaml
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: <string>
  namespace: <string>
spec:   # DRPlanSpec
  ...
status: # DRPlanStatus (read-only, set by controller)
  ...
```

---

## Spec Fields (`DRPlanSpec`)

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `volumeReplicationDriver` | [`VolumeReplicationDriverConfig`](#volumereplicationdriverconfig) | **Yes** | — | See nested table | Configures the volume replication driver. **Immutable after creation.** |
| `maxConcurrentFailovers` | `int` | **Yes** | — | Minimum: 1 | Maximum number of concurrent VM failovers per wave chunk. |
| `primarySite` | `string` | **Yes** | — | Must not be empty; must differ from `secondarySite` | Cluster name that originally owns the active workloads. **Immutable after creation.** |
| `secondarySite` | `string` | **Yes** | — | Must not be empty; must differ from `primarySite` | Cluster name that serves as the DR target. **Immutable after creation.** |
| `vmReadyTimeout` | `Duration` | No | `5m` | Must be a positive duration | Maximum wait time for all VMs in a wave to reach Running state after StartVM. Per-wave timeout. |
| `resyncTimeout` | `Duration` | No | `10m` | — | Maximum wait time for VR/VGR resync completion during planned failover Step 0. Only applies to `planned_migration` mode. |

### `VolumeReplicationDriverConfig`

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `type` | `string` | **Yes** | — | Enum: `noop`, `csi-extension` | Registered driver that handles volume replication operations. **Immutable after creation.** |
| `volumeReplicationClass` | `string` | No | — | Required when `type` is `csi-extension`; forbidden when `type` is `noop` | VolumeReplicationClass name stamped on VolumeReplication and VolumeGroupReplication CRs. Only applicable for the CSI Extension driver. **Immutable after creation** (covered by struct-level immutability). |

---

## Status Fields (`DRPlanStatus`)

Status is read-only and populated by the DRPlan controller during reconciliation.

| Field | Type | Description |
|-------|------|-------------|
| `phase` | `string` | Current DR lifecycle rest state. Valid values: `SteadyState`, `FailedOver`, `DRedSteadyState`, `FailedBack`. Transient phases (`FailingOver`, `Reprotecting`, `FailingBack`, `ReprotectingBack`) are derived at runtime from the active DRExecution. |
| `activeSite` | `string` | Cluster currently owning the active workloads. Set to `primarySite` on creation; flipped on failover/failback completion. |
| `conditions` | [`[]metav1.Condition`](#conditions) | Standard Kubernetes conditions representing the latest observations of the plan's state. |
| `observedGeneration` | `int64` | Most recent `.metadata.generation` observed by the controller. |
| `waves` | [`[]WaveInfo`](#waveinfo) | Discovered VMs grouped by wave label value. |
| `discoveredVMCount` | `int` | Total number of VMs discovered for this plan. |
| `preflight` | [`*PreflightReport`](#preflightreport) | Pre-flight plan composition report, populated on every reconcile to give visibility into plan structure before execution. |
| `replicationHealth` | [`[]VolumeGroupHealth`](#volumegrouphealth) | Per-volume-group replication status, populated by polling storage drivers on each reconcile cycle. |
| `primarySiteDiscovery` | [`*SiteDiscovery`](#sitediscovery) | VMs discovered on the primary site cluster. Written exclusively by the primary site's Soteria instance. |
| `secondarySiteDiscovery` | [`*SiteDiscovery`](#sitediscovery) | VMs discovered on the secondary site cluster. Written exclusively by the secondary site's Soteria instance. |

---

## Nested Types

### `WaveInfo`

Groups discovered VMs into a single execution wave. Only created when at least one VM belongs to the wave.

| Field | Type | Required | Validation | Description |
|-------|------|----------|------------|-------------|
| `waveKey` | `string` | **Yes** | — | Value of the `soteria.io/wave` label that groups these VMs. |
| `vms` | [`[]DiscoveredVM`](#discoveredvm) | **Yes** | MinItems: 1 | Discovered VMs in this wave. |
| `groups` | [`[]VolumeGroupInfo`](#volumegroupinfo) | No | — | Volume groups formed from VMs in this wave. Populated after consistency resolution. |

### `VolumeGroupInfo`

Describes a group of VM disks that are snapshotted atomically. Namespace-level groups ensure crash-consistent snapshots across all VMs sharing a namespace; VM-level groups scope consistency to a single VM's disks.

| Field | Type | Required | Validation | Description |
|-------|------|----------|------------|-------------|
| `name` | `string` | **Yes** | Required | Group identifier (e.g., `ns-erp-database` or `vm-default-web01`). |
| `namespace` | `string` | **Yes** | — | Kubernetes namespace for VMs in this group. |
| `consistencyLevel` | `string` | **Yes** | Enum: `namespace`, `vm` | Whether this is a namespace-level or VM-level group. |
| `vmNames` | `[]string` | **Yes** | MinItems: 1 | VMs belonging to this volume group. |

### `DiscoveredVM`

Identifies a VM discovered by a DRPlan's label selector.

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | VM resource name. |
| `namespace` | `string` | VM's namespace. |
| `disks` | [`[]DiscoveredDisk`](#discovereddisk) | Per-disk PVC topology. Only disks backed by PersistentVolumeClaim or DataVolume volumes are included; other volume types (containerDisk, cloudInit, etc.) are silently omitted. |

### `DiscoveredDisk`

Describes a single disk attached to a VM and its backing PVC.

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Disk name from the VM's `domain.devices.disks` spec (the guest-visible identifier). |
| `pvcName` | `string` | PersistentVolumeClaim backing this disk. Empty when the PVC has not yet been created (e.g., DataVolume provisioning). |
| `storageClass` | `string` | Storage class of the backing PVC. Empty when the PVC does not exist or has no `storageClassName`. |

### `PreflightReport`

Pre-flight composition summary assembled from discovery, consistency, chunking, and storage backend data. Shows exactly how the plan would execute.

| Field | Type | Description |
|-------|------|-------------|
| `primarySite` | `string` | Declared primary cluster for this plan. |
| `secondarySite` | `string` | Declared secondary (DR target) cluster. |
| `activeSite` | `string` | Cluster currently owning the active workloads. |
| `activeExecution` | `string` | Name of the in-progress DRExecution, or empty when idle. When non-empty, a warning is added. |
| `waves` | [`[]PreflightWave`](#preflightwave) | Per-wave composition summaries. |
| `totalVMs` | `int` | Total number of VMs in the plan. |
| `warnings` | `[]string` | Non-blocking validation issues (e.g., unknown storage backend). |
| `sitesInSync` | `bool` | Whether primary and secondary sites agree on the discovered VM set. Only meaningful in site-aware mode. |
| `siteDiscoveryDelta` | `string` | VM differences between sites when `sitesInSync` is false. Omitted when sites agree. |
| `disksConsistent` | `bool` | Whether all VMs have matching disk topology across sites. Only meaningful in site-aware mode with disk discovery on both sites. |
| `diskDiscoveryDelta` | `string` | Per-VM disk topology differences when `disksConsistent` is false. Omitted when disks agree. |
| `generatedAt` | `Time` | When this report was last computed. |

### `PreflightWave`

Summarises a single execution wave in the pre-flight report.

| Field | Type | Description |
|-------|------|-------------|
| `waveKey` | `string` | Wave label value. |
| `vmCount` | `int` | Total VMs in this wave. |
| `vms` | [`[]PreflightVM`](#preflightvm) | Per-VM composition details. |
| `chunks` | [`[]PreflightChunk`](#preflightchunk) | DRGroup chunking preview for this wave. |

### `PreflightVM`

Single VM's composition attributes in the pre-flight report.

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | VM resource name. |
| `namespace` | `string` | VM's namespace. |
| `storageBackend` | `string` | Plan's declared VolumeReplicationDriver stamped on every VM. |
| `consistencyLevel` | `string` | Consistency level: `namespace` or `vm`. |
| `volumeGroupName` | `string` | Volume group this VM belongs to. |

### `PreflightChunk`

DRGroup chunk in the pre-flight chunking preview.

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | DRGroup chunk name (e.g., `wave-1-group-0`). |
| `vmCount` | `int` | Number of VMs in this chunk. |
| `vmNames` | `[]string` | VM names in this chunk. |
| `volumeGroups` | [`[]PreflightVolumeGroup`](#preflightvolumegroup) | Volume groups in this chunk, enriched with per-disk PVC topology. |

### `PreflightVolumeGroup`

Volume group enriched with per-disk PVC topology.

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Volume group identifier (e.g., `ns-erp-database` or `vm-default-web01`). |
| `disks` | [`[]VolumeGroupDisk`](#volumegroupdisk) | Disks and their per-site PVC mappings. Sorted by VM name then disk name. |

### `VolumeGroupDisk`

Single disk within a volume group with per-site PVC mappings.

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Disk name from the VM's `domain.devices.disks` spec. |
| `sites` | [`[]DiskSiteMapping`](#disksitemapping) | Per-site PVC mappings. Typically two entries (one per site). |

### `DiskSiteMapping`

Maps a disk to its PVC on a specific site.

| Field | Type | Description |
|-------|------|-------------|
| `site` | `string` | Cluster site name (e.g., `dc1`). |
| `pvcName` | `string` | PersistentVolumeClaim backing this disk on the site. |
| `pvcNamespace` | `string` | Namespace of the backing PVC on the site. |

### `VolumeGroupHealth`

Replication health for a single volume group, populated by polling storage drivers on each reconcile cycle.

| Field | Type | Validation | Description |
|-------|------|------------|-------------|
| `name` | `string` | — | Volume group identifier (e.g., `ns-erp-database`). |
| `namespace` | `string` | — | Kubernetes namespace for VMs in this group. |
| `health` | `string` | Enum: `Healthy`, `Degraded`, `Syncing`, `NotReplicating`, `Error`, `Unknown` | Replication health status. |
| `lastSyncTime` | `Time` | — | When data was last successfully synchronized. |
| `lastChecked` | `Time` | — | When this health status was last polled. |
| `message` | `string` | — | Optional error or informational message. |

### `SiteDiscovery`

VM discovery results from a single site's perspective. Each Soteria instance writes exclusively to the `SiteDiscovery` field matching its own site role.

| Field | Type | Description |
|-------|------|-------------|
| `vms` | [`[]DiscoveredVM`](#discoveredvm) | VMs discovered on this site. |
| `discoveredVMCount` | `int` | Number of VMs discovered on this site. |
| `lastDiscoveryTime` | `Time` | When the last discovery cycle completed on this site. |

### Conditions

DRPlan uses standard Kubernetes `metav1.Condition` objects. Each condition has:

| Field | Type | Description |
|-------|------|-------------|
| `type` | `string` | Condition type (e.g., `Ready`). |
| `status` | `string` | `True`, `False`, or `Unknown`. |
| `observedGeneration` | `int64` | Generation observed when the condition was set. |
| `lastTransitionTime` | `Time` | Last time the condition transitioned. |
| `reason` | `string` | Machine-readable reason (PascalCase). |
| `message` | `string` | Human-readable description. |

---

## Validation Rules

### Create Validation

These rules are enforced on every `CREATE` operation by the aggregated API server strategy and the validating admission webhook:

| Field | Rule | Error |
|-------|------|-------|
| `spec.volumeReplicationDriver.type` | Must be one of `noop`, `csi-extension` | `NotSupported` — unsupported value |
| `spec.volumeReplicationDriver.volumeReplicationClass` | Required when `type` is `csi-extension`; forbidden when `type` is `noop` | `Required` / `Forbidden` |
| `spec.maxConcurrentFailovers` | Must be ≥ 1 | `Invalid` — must be greater than 0 |
| `spec.primarySite` | Must not be empty | `Required` |
| `spec.secondarySite` | Must not be empty; must differ from `primarySite` | `Required` / `Invalid` — must differ from primarySite |
| `spec.vmReadyTimeout` | If set, must be a positive duration | `Invalid` — must be a positive duration |

### Update Validation (Immutability)

These fields are **immutable after creation**. Any change is rejected with a `Forbidden` error:

| Field | Constraint |
|-------|------------|
| `spec.volumeReplicationDriver` | Entire struct is immutable (type, volumeReplicationClass) |
| `spec.primarySite` | Immutable |
| `spec.secondarySite` | Immutable |

All create-time validations also run on updates.

### Driver-Specific Validation

=== "noop"

    The `noop` driver requires no additional configuration:

    - `volumeReplicationClass`: **Must not be set** (forbidden for noop driver).

=== "csi-extension"

    The CSI Extension driver requires:

    - `volumeReplicationClass`: **Required** — the VolumeReplicationClass name stamped on VR/VGR CRs.

---

## Label & Annotation Conventions

| Key | Applies To | Description |
|-----|-----------|-------------|
| `soteria.io/drplan` | VirtualMachine | Declares membership in a DRPlan. Label keys can only have one value per resource, structurally enforcing one-plan-per-VM exclusivity. |
| `soteria.io/wave` | VirtualMachine | Assigns VMs to execution waves. The value is the wave key (e.g., `"1"`, `"2"`). |
| `soteria.io/consistency-level` | Namespace (annotation) | Controls consistency-level grouping. Set to `namespace` to group all VMs in that namespace into a single VolumeGroup. Defaults to `vm` (per-VM grouping). |

---

## Phase Lifecycle

DRPlan follows an 8-phase symmetric lifecycle. Only **rest states** are persisted in `status.phase`:

| Persisted Rest State | Description |
|---------------------|-------------|
| `SteadyState` | Normal operation. Primary site is active, replication flows A→B. |
| `FailedOver` | Workloads are running on the secondary site. No replication. |
| `DRedSteadyState` | Post-reprotect. Secondary site is active, replication flows B→A. |
| `FailedBack` | Workloads returned to primary site. No replication. |

**Transient phases** are derived at runtime from the active DRExecution's mode — they are never stored:

| Transient Phase | Derived When |
|----------------|--------------|
| `FailingOver` | Active execution in `planned_migration` or `disaster` mode from `SteadyState` |
| `Reprotecting` | Active execution in `reprotect` mode from `FailedOver` |
| `FailingBack` | Active execution in `planned_migration` or `disaster` mode from `DRedSteadyState` |
| `ReprotectingBack` | Active execution in `reprotect` mode from `FailedBack` |

---

## Examples

### Minimal DRPlan (noop driver)

```yaml
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: my-dr-plan
  namespace: dr-plans
spec:
  volumeReplicationDriver:
    type: noop
  maxConcurrentFailovers: 5
  primarySite: dc1
  secondarySite: dc2
```

### Full DRPlan (CSI Extension driver)

```yaml
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: erp-full-stack
  namespace: dr-plans
spec:
  # Volume replication driver configuration (immutable after creation)
  volumeReplicationDriver:
    type: csi-extension
    # VolumeReplicationClass for VR/VGR CRs (required for csi-extension)
    volumeReplicationClass: odf-rbd-volumereplicationclass
  # Maximum concurrent VM failovers per wave chunk (minimum: 1)
  maxConcurrentFailovers: 3
  # Site definitions (both immutable after creation)
  primarySite: east
  secondarySite: west
  # Maximum time to wait for VMs to reach Running state per wave (default: 5m)
  vmReadyTimeout: 10m
  # Maximum time to wait for VR/VGR resync during planned migration Step 0 (default: 10m)
  resyncTimeout: 15m
```

### VM Labels for Plan Membership

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: erp-app-01
  namespace: erp
  labels:
    # Associates this VM with the "erp-full-stack" DRPlan
    soteria.io/drplan: erp-full-stack
    # Places this VM in wave "1" for ordered execution
    soteria.io/wave: "1"
```

### Namespace Annotation for Consistency Grouping

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: erp
  annotations:
    # Groups all VMs in this namespace into a single VolumeGroup
    # for crash-consistent snapshots (default is "vm" — per-VM grouping)
    soteria.io/consistency-level: namespace
```

---

## Source Files

| File | Purpose |
|------|---------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | DRPlan struct definitions and kubebuilder markers |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | `ValidateDRPlan` and `ValidateDRPlanUpdate` field-level validation |
| `pkg/apis/soteria.io/v1alpha1/defaults.go` | Defaulting logic (kubebuilder markers handle defaults for `vmReadyTimeout`, `resyncTimeout`) |
| `pkg/admission/drplan_validator.go` | Validating admission webhook (defense-in-depth) |
