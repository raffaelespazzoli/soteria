# Creating a DRPlan

This guide walks you through creating and configuring a **DRPlan** — the central
resource that defines which VMs are protected and how they fail over between
sites.

## Overview

A DRPlan declares:

- **Which VMs** are protected (discovered via label selectors).
- **How they fail over** (concurrency throttling, wave ordering).
- **Where they fail over** (primary and secondary site names).
- **Which storage driver** handles volume replication.

Soteria discovers VMs dynamically at runtime — you never list VMs in the DRPlan
spec itself. Instead, VMs declare their membership by carrying two labels.

---

## Annotated DRPlan YAML

Below is a complete DRPlan with every field explained:

```yaml
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: erp-full-stack                # (1)!
spec:
  volumeReplicationDriver:            # (2)!
    type: noop                         # (3)!
    # volumeReplicationClass: ""       # (4)!
  maxConcurrentFailovers: 4            # (5)!
  primarySite: dc-west                 # (6)!
  secondarySite: dc-east               # (7)!
  vmReadyTimeout: 5m                   # (8)!
  resyncTimeout: 10m                   # (9)!
```

1. **name** — A unique identifier for the plan. VMs reference this value in their
   `soteria.io/drplan` label.
2. **volumeReplicationDriver** — Configures the storage replication backend.
   **Immutable after creation.**
3. **type** — The driver implementation. Must be `noop` or `csi-extension`.
   See [Storage driver options](#storage-driver-options) below.
4. **volumeReplicationClass** — Only required when `type: csi-extension`.
   Specifies the `VolumeReplicationClass` stamped on VolumeReplication and
   VolumeGroupReplication CRs. Forbidden when `type: noop`.
5. **maxConcurrentFailovers** — Maximum VMs that fail over concurrently within
   a single wave chunk. Minimum value: `1`.
6. **primarySite** — The cluster that originally owns the active workloads.
   **Immutable after creation.**
7. **secondarySite** — The DR target cluster. **Immutable after creation.**
   Must differ from `primarySite`.
8. **vmReadyTimeout** — Maximum time to wait for VMs in a wave to reach
   `Running` state after `StartVM`. Per-wave timer. Default: `5m`. Must be a
   positive duration.
9. **resyncTimeout** — Maximum time to wait for VR/VGR resync completion
   during planned failover Step 0. Only applies to `planned_migration` mode.
   Default: `10m`.

---

## Assigning VMs to a DRPlan

VMs join a DRPlan through **two labels** on the `VirtualMachine` resource:

| Label | Purpose | Example |
|---|---|---|
| `soteria.io/drplan` | Declares which plan protects this VM | `soteria.io/drplan: erp-full-stack` |
| `soteria.io/wave` | Assigns the VM to an execution wave | `soteria.io/wave: "1"` |

### Example: VM with DRPlan labels

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: erp-db-01
  namespace: erp-system
  labels:
    soteria.io/drplan: erp-full-stack   # (1)!
    soteria.io/wave: "1"                # (2)!
spec:
  runStrategy: Manual
  template:
    metadata:
      labels:
        soteria.io/drplan: erp-full-stack
        soteria.io/wave: "1"
    spec:
      domain:
        resources:
          requests:
            memory: 4Gi
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: erp-db-01-rootdisk
```

1. **Plan membership** — Links this VM to the `erp-full-stack` DRPlan. A
   Kubernetes label key can only have one value per resource, so a VM
   structurally belongs to **at most one plan** — no runtime check needed.
2. **Wave assignment** — Places this VM in wave `1` (database tier, fails over
   first). The label key `soteria.io/wave` is a fixed convention and is not
   configurable.

!!! info "One plan per VM"
    Because `soteria.io/drplan` is a single label key, each VM can carry only
    one value. This enforces **one-plan-per-VM exclusivity** at the Kubernetes
    API level — no admission webhook or runtime validation is needed.

!!! tip "Wave numbering"
    Waves execute in ascending order by label value. Use simple numeric strings
    (`"1"`, `"2"`, `"3"`) to control failover ordering. Database VMs typically
    go in wave 1, application servers in wave 2, and so on.

---

## Spec Field Reference

### `volumeReplicationDriver`

Configures the storage backend that handles volume replication for VMs in this
plan. **Immutable after creation** — you cannot change the driver on an existing
DRPlan.

| Sub-field | Type | Required | Description |
|---|---|---|---|
| `type` | `string` | Yes | Driver implementation: `noop` or `csi-extension` |
| `volumeReplicationClass` | `string` | Conditional | VolumeReplicationClass name. **Required** when `type: csi-extension`, **forbidden** when `type: noop` |

#### Storage driver options

=== "noop"

    The `noop` driver skips all volume replication operations. Use it for:

    - Testing and development environments
    - Plans where storage replication is managed externally
    - Dry-run scenarios

    ```yaml
    volumeReplicationDriver:
      type: noop
    ```

=== "csi-extension"

    The `csi-extension` driver manages replication through the CSI
    VolumeReplication API. It creates VolumeReplication and
    VolumeGroupReplication CRs for each protected volume.

    ```yaml
    volumeReplicationDriver:
      type: csi-extension
      volumeReplicationClass: my-replication-class
    ```

### `maxConcurrentFailovers`

| Type | Default | Minimum | Description |
|---|---|---|---|
| `int` | — | `1` | Maximum VMs that fail over concurrently within a single wave chunk |

Controls the concurrency throttle per wave. During execution, each wave's VMs
are chunked into groups of this size. Groups execute sequentially within a wave;
VMs within a group fail over in parallel.

**Example:** With `maxConcurrentFailovers: 4` and a wave containing 10 VMs,
Soteria creates 3 chunks (4 + 4 + 2) and processes them one at a time.

### `primarySite`

| Type | Default | Immutable | Description |
|---|---|---|---|
| `string` | — | Yes | Cluster name that originally owns active workloads |

**Required.** Must be non-empty. Cannot be changed after the DRPlan is created.

### `secondarySite`

| Type | Default | Immutable | Description |
|---|---|---|---|
| `string` | — | Yes | Cluster name that serves as the DR target |

**Required.** Must be non-empty, and **must differ from `primarySite`**. Cannot
be changed after the DRPlan is created.

### `vmReadyTimeout`

| Type | Default | Description |
|---|---|---|
| `duration` | `5m` | Maximum wait for VMs in a wave to reach Running after StartVM |

Optional. Per-wave timer that starts when all `StartVM` operations in the wave
complete. If any VM has not reached `Running` state within this timeout, the
wave is marked as timed out. Must be a positive duration when specified.

Set higher values for VMs with slow boot sequences (e.g., database servers
that run recovery on startup).

### `resyncTimeout`

| Type | Default | Description |
|---|---|---|
| `duration` | `10m` | Maximum wait for VR/VGR resync during planned failover Step 0 |

Optional. Only applies to `planned_migration` execution mode. During Step 0,
Soteria waits for all VolumeReplication/VolumeGroupReplication resources to
finish resyncing before proceeding. If resync does not complete within this
timeout, the execution is marked as timed out.

---

## Validation Rules

DRPlan validation runs at multiple layers for defense-in-depth:

### Layer 1: Kubebuilder markers (schema validation)

Applied automatically by the Kubernetes API server based on the CRD schema:

| Field | Rule | Marker |
|---|---|---|
| `volumeReplicationDriver.type` | Must be `noop` or `csi-extension` | `+kubebuilder:validation:Enum=noop;csi-extension` |
| `volumeReplicationDriver.type` | Required | `+kubebuilder:validation:Required` |
| `volumeReplicationDriver` | Required | `+kubebuilder:validation:Required` |
| `maxConcurrentFailovers` | Minimum value: 1 | `+kubebuilder:validation:Minimum=1` |
| `vmReadyTimeout` | Default: `5m` | `+kubebuilder:default="5m"` |
| `resyncTimeout` | Default: `10m` | `+kubebuilder:default="10m"` |

### Layer 2: Admission validation (field-level)

The `SoteriaAdmissionPlugin` (in-process admission plugin within the aggregated
API server) validates on CREATE and UPDATE:

| Rule | Applies to | Error |
|---|---|---|
| `maxConcurrentFailovers` must be > 0 | CREATE, UPDATE | `spec.maxConcurrentFailovers: Invalid value` |
| `primarySite` must be non-empty | CREATE, UPDATE | `spec.primarySite: Required value` |
| `secondarySite` must be non-empty | CREATE, UPDATE | `spec.secondarySite: Required value` |
| `secondarySite` must differ from `primarySite` | CREATE, UPDATE | `spec.secondarySite: Invalid value: must differ from primarySite` |
| `volumeReplicationDriver.type` must be non-empty | CREATE, UPDATE | `spec.volumeReplicationDriver.type: Required value` |
| `volumeReplicationClass` forbidden for `noop` driver | CREATE, UPDATE | `spec.volumeReplicationDriver.volumeReplicationClass: Forbidden` |
| `volumeReplicationClass` required for `csi-extension` driver | CREATE, UPDATE | `spec.volumeReplicationDriver.volumeReplicationClass: Required value` |
| `vmReadyTimeout` must be a positive duration | CREATE, UPDATE | `spec.vmReadyTimeout: Invalid value` |
| `primarySite` is immutable | UPDATE | `spec.primarySite: Forbidden: field is immutable` |
| `secondarySite` is immutable | UPDATE | `spec.secondarySite: Forbidden: field is immutable` |
| `volumeReplicationDriver` is immutable | UPDATE | `spec.volumeReplicationDriver: Forbidden: field is immutable` |

### Layer 3: Controller reconciliation (cross-resource)

The DRPlan controller enforces constraints that span multiple resources. These
are reported as `Ready=False` status conditions rather than admission rejections:

| Condition | Meaning |
|---|---|
| `WaveConflict` | VMs in the same namespace have conflicting wave labels |
| `NamespaceGroupExceedsThrottle` | A namespace-level volume group contains more VMs than `maxConcurrentFailovers` |

---

## Consistency Level

By default, each VM's disks form an independent volume group (VM-level
consistency). To enable **namespace-level consistency** — where all VM disks in
a namespace are grouped into a single VolumeGroup for crash-consistent
snapshots — annotate the namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: erp-system
  annotations:
    soteria.io/consistency-level: namespace   # (1)!
```

1. Valid values: `namespace` (all VMs share one VolumeGroup) or `vm` (each VM
   gets its own VolumeGroup, this is the default).

---

## Complete Example

Here is a full example with a DRPlan and two VMs spanning two waves:

```yaml
---
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: erp-full-stack
spec:
  volumeReplicationDriver:
    type: noop
  maxConcurrentFailovers: 4
  primarySite: dc-west
  secondarySite: dc-east
  vmReadyTimeout: 5m
  resyncTimeout: 10m
---
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: erp-db-01
  namespace: erp-system
  labels:
    soteria.io/drplan: erp-full-stack
    soteria.io/wave: "1"
spec:
  runStrategy: Manual
  template:
    metadata:
      labels:
        soteria.io/drplan: erp-full-stack
        soteria.io/wave: "1"
    spec:
      domain:
        resources:
          requests:
            memory: 4Gi
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: erp-db-01-rootdisk
---
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: erp-web-01
  namespace: erp-system
  labels:
    soteria.io/drplan: erp-full-stack
    soteria.io/wave: "2"
spec:
  runStrategy: Manual
  template:
    metadata:
      labels:
        soteria.io/drplan: erp-full-stack
        soteria.io/wave: "2"
    spec:
      domain:
        resources:
          requests:
            memory: 2Gi
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: erp-web-01-rootdisk
```

With this configuration:

1. **Wave 1** — `erp-db-01` fails over first (database tier).
2. **Wave 2** — `erp-web-01` fails over second (web tier).
3. Up to **4 VMs** can fail over concurrently within each wave.

---

## What Happens Next

After applying the DRPlan, Soteria's controller:

1. **Discovers VMs** — Finds all VirtualMachines carrying the
   `soteria.io/drplan: erp-full-stack` label.
2. **Groups into waves** — Reads the `soteria.io/wave` label on each VM.
3. **Builds a preflight report** — Populates `status.preflight` with wave
   composition, VM lists, volume groups, and any warnings.
4. **Monitors replication health** — Polls the storage driver and populates
   `status.replicationHealth`.

Check plan readiness with:

```bash
kubectl get drplan erp-full-stack -o yaml
```

Look for the `Ready` condition in `status.conditions` and review the
`status.preflight` report for any warnings before triggering an execution.

---

## Next Steps

- [Waves & Throttling](waves.md) — Fine-tune wave ordering and concurrency
- [Volume Grouping](volumes.md) — Understand volume group composition
- [Executing Failover](failover.md) — Trigger a DRExecution against your plan
