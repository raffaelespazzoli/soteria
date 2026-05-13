# Story 12.3: CreateVolumeGroup / DeleteVolumeGroup / GetVolumeGroup

Status: ready-for-dev

## Story

As a CSI extension driver,
I want to create, delete, and retrieve VolumeReplication or VolumeGroupReplication CRDs based on Soteria volume groups,
So that the csi-addons sidecar manages the actual storage replication for protected VMs.

## Background

### Rendering Rule

Soteria VolumeGroups map to CSI Addons CRDs based on VM count:

| Soteria VolumeGroup | # VMs | CSI CRD Created |
|---|---|---|
| VM-level group (1 VM) | 1 | One `VolumeReplication` CR per PVC |
| Namespace-level group (N VMs) | 2+ | One `VolumeGroupReplication` CR for the group |

**Rationale:** VolumeReplication is simpler and doesn't require VolumeGroup capability from the storage backend. Multi-VM groups need crash-consistent grouping via VolumeGroupReplication.

### Initial Replication State

When creating CRs, the initial `spec.replicationState` depends on which site the driver is running on:
- **Primary site** → `primary` (this site is the replication source)
- **Secondary site** → `secondary` (this site is the replication target)

The driver needs to know its site role. This is determined from the plan's `primarySite`/`secondarySite` fields and the operator's `--site-name` flag.

### Idempotency

`CreateVolumeGroup` is called on every reconcile cycle (via `resolveVolumeGroupID` in the handler). The driver must be idempotent: if VR/VGR CRs already exist with matching spec, return the existing resources without modification.

## Acceptance Criteria

1. **AC1 — CreateVolumeGroup with 1 VM:** When `VolumeGroupSpec` contains PVCs from a single VM (inferred from PVCNames count or VG name prefix), the driver creates individual `VolumeReplication` CRs — one per PVC in the spec. Each VR CR references the PVC as `spec.dataSource` and the configured `VolumeReplicationClass`.

2. **AC2 — CreateVolumeGroup with N VMs:** When the VolumeGroup contains PVCs from multiple VMs, the driver creates a single `VolumeGroupReplication` CR containing all PVC volume IDs and referencing the configured `VolumeGroupReplicationClass`.

3. **AC3 — Initial replication state:** Created CRs have `spec.replicationState` set to `primary` on the primary site and `secondary` on the secondary site.

4. **AC4 — VolumeGroupID mapping:** The returned `VolumeGroupInfo.ID` is a deterministic identifier derived from the VG name (e.g., `"csi-ext-<vg-name>"`) that can be used to locate the CRs in subsequent calls.

5. **AC5 — Idempotency:** If CRs already exist for this VolumeGroup (matched by name/labels), `CreateVolumeGroup` returns the existing `VolumeGroupInfo` without recreating or modifying the CRs.

6. **AC6 — DeleteVolumeGroup:** Deletes all VR CRs (single-VM case) or the VGR CR (multi-VM case) associated with the given `VolumeGroupID`. Returns nil if CRs don't exist (idempotent).

7. **AC7 — GetVolumeGroup:** Reads the VR/VGR CRs and returns `VolumeGroupInfo` with the current PVC list. Returns `ErrVolumeGroupNotFound` if no CRs exist.

8. **AC8 — Labels for identification:** Created CRs are labeled with `soteria.io/volume-group: <vg-name>` and `soteria.io/drplan: <plan-name>` to enable lookup by the driver.

9. **AC9 — Unit tests:** Tests with a fake Kubernetes client verifying single-VM VR creation, multi-VM VGR creation, idempotency, deletion, and retrieval.

## Tasks / Subtasks

- [ ] Task 1: Implement VR rendering (AC: #1, #3, #4, #8)
  - [ ] 1.1 Create VolumeReplication CRs for each PVC when VG has 1 VM's PVCs
  - [ ] 1.2 Set `spec.replicationState` based on site role
  - [ ] 1.3 Apply labels for identification
  - [ ] 1.4 Return VolumeGroupInfo with deterministic ID

- [ ] Task 2: Implement VGR rendering (AC: #2, #3, #4, #8)
  - [ ] 2.1 Create VolumeGroupReplication CR with all PVC volume IDs
  - [ ] 2.2 Set `spec.replicationState` based on site role
  - [ ] 2.3 Apply labels for identification

- [ ] Task 3: Implement idempotency (AC: #5)
  - [ ] 3.1 Before creating, check if CRs with matching labels already exist
  - [ ] 3.2 If found, return existing VolumeGroupInfo

- [ ] Task 4: Implement DeleteVolumeGroup (AC: #6)
  - [ ] 4.1 List CRs by label selector
  - [ ] 4.2 Delete all matching CRs
  - [ ] 4.3 Return nil if no CRs found

- [ ] Task 5: Implement GetVolumeGroup (AC: #7)
  - [ ] 5.1 List CRs by label selector
  - [ ] 5.2 Build VolumeGroupInfo from found CRs
  - [ ] 5.3 Return ErrVolumeGroupNotFound if no CRs

- [ ] Task 6: Site role determination (AC: #3)
  - [ ] 6.1 Implement mechanism for the driver to know its site role (primary or secondary)
  - [ ] 6.2 Pass via VolumeGroupSpec extension, driver config, or labels

- [ ] Task 7: Unit tests (AC: #9)
  - [ ] 7.1 Test single-VM VR creation (1 PVC, 3 PVCs)
  - [ ] 7.2 Test multi-VM VGR creation
  - [ ] 7.3 Test idempotent re-creation returns existing
  - [ ] 7.4 Test DeleteVolumeGroup removes CRs
  - [ ] 7.5 Test GetVolumeGroup reads CRs
  - [ ] 7.6 Test GetVolumeGroup returns ErrVolumeGroupNotFound
  - [ ] 7.7 Run `make test` and `make lint`

## Dev Notes

### Key Locations

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/driver.go` | Modified — implement CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup |
| `pkg/drivers/csiextension/driver_test.go` | New — unit tests with fake client |

### VolumeGroupID Design

The `VolumeGroupID` is a deterministic string derived from the VG name:
```go
func vgIDFromName(name string) drivers.VolumeGroupID {
    return drivers.VolumeGroupID("csi-ext-" + name)
}
```

This enables the driver to reconstruct which CRs belong to a VG from just the ID — by stripping the prefix and using the label selector.

### VR vs VGR Decision Logic

```go
func (d *Driver) CreateVolumeGroup(ctx context.Context, spec drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) {
    // Multi-VM VGs: spec is populated by the handler with PVCs from all VMs.
    // The VG name prefix distinguishes: "ns-*" = namespace-level (multi-VM), "vm-*" = VM-level (single).
    // Alternatively, count unique VMs by looking at the VG's VMNames metadata.
    // For now: if spec.Labels["consistency-level"] == "namespace" → VGR, else → VR
}
```

The exact heuristic will be refined during implementation. The VolumeGroupSpec includes the VG name which encodes the consistency level (`ns-` prefix = namespace-level = multi-VM, `vm-` prefix = VM-level = single-VM).

### What NOT to Change

- StorageProvider interface
- Handler/executor logic
- Noop driver
- Preflight/health paths (they use the driver through the same interface)

### Dependency

- **Depends on Story 12.2** — CRD types and client must be available.

### Build Commands

```bash
make test
make lint-fix && make lint
```
