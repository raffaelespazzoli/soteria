# Story 12.3: CreateVolumeGroup / DeleteVolumeGroup / GetVolumeGroup

Status: done

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

- [x] Task 1: Implement VR rendering (AC: #1, #3, #4, #8)
  - [x] 1.1 Create VolumeReplication CRs for each PVC when VG has 1 VM's PVCs
  - [x] 1.2 Set `spec.replicationState` based on site role
  - [x] 1.3 Apply labels for identification
  - [x] 1.4 Return VolumeGroupInfo with deterministic ID

- [x] Task 2: Implement VGR rendering (AC: #2, #3, #4, #8)
  - [x] 2.1 Create VolumeGroupReplication CR with all PVC volume IDs
  - [x] 2.2 Set `spec.replicationState` based on site role
  - [x] 2.3 Apply labels for identification

- [x] Task 3: Implement idempotency (AC: #5)
  - [x] 3.1 Before creating, check if CRs with matching labels already exist
  - [x] 3.2 If found, return existing VolumeGroupInfo

- [x] Task 4: Implement DeleteVolumeGroup (AC: #6)
  - [x] 4.1 List CRs by label selector
  - [x] 4.2 Delete all matching CRs
  - [x] 4.3 Return nil if no CRs found

- [x] Task 5: Implement GetVolumeGroup (AC: #7)
  - [x] 5.1 List CRs by label selector
  - [x] 5.2 Build VolumeGroupInfo from found CRs
  - [x] 5.3 Return ErrVolumeGroupNotFound if no CRs

- [x] Task 6: Site role determination (AC: #3)
  - [x] 6.1 Implement mechanism for the driver to know its site role (primary or secondary)
  - [x] 6.2 Pass via VolumeGroupSpec.Labels[SiteRoleLabel]

- [x] Task 7: Unit tests (AC: #9)
  - [x] 7.1 Test single-VM VR creation (1 PVC, 3 PVCs)
  - [x] 7.2 Test multi-VM VGR creation
  - [x] 7.3 Test idempotent re-creation returns existing
  - [x] 7.4 Test DeleteVolumeGroup removes CRs
  - [x] 7.5 Test GetVolumeGroup reads CRs
  - [x] 7.6 Test GetVolumeGroup returns ErrVolumeGroupNotFound
  - [x] 7.7 Run `make test` and `make lint`

### Review Findings

- [x] [Review][Decision] **Namespace scoping for GetVolumeGroup/DeleteVolumeGroup** — Resolved: Option A — encode namespace in VolumeGroupID (`csi-ext-<ns>/<name>`), scope Get/Delete to namespace via `parseVGID`.
- [x] [Review][Decision] **Partial failure in createVRs leaves orphans and breaks idempotent retry** — Resolved: create-or-update semantics — `createVRs`/`createVGR` skip `AlreadyExists`, removed up-front `getByName` check.
- [x] [Review][Patch] **DeleteVolumeGroup does not ignore NotFound on individual CR deletion** — Fixed: `apierrors.IsNotFound` check added.
- [x] [Review][Patch] **getByName swallows pvcNamesFromSelector error, returns nil PVCNames** — Fixed: error propagated.
- [x] [Review][Patch] **Empty PVCNames in createVRs reports success without creating any CRs** — Fixed: validation guard at entry.
- [x] [Review][Defer] **PVC labels not cleaned up on DeleteVolumeGroup** — `createVGR` patches PVCs with `soteria.io/volume-group` but `DeleteVolumeGroup` only removes VR/VGR CRs. Labels remain on PVCs. Not in AC scope; cosmetic until a PVC reassignment story. [driver.go:216-247]
- [x] [Review][Defer] **createVGR overwrites existing LabelVolumeGroup without conflict detection** — If a PVC already has `soteria.io/volume-group` set to a different group, the code patches it to the new group without error. Orchestration layer prevents double-enrollment. [driver.go:168-183]
- [x] [Review][Defer] **Same PVC can be enrolled under multiple VGs on the VR path** — Different VG names produce different VR CR names while `DataSource` points at the same PVC. Orchestration layer assigns PVCs to exactly one VG. [driver.go:129-148]
- [x] [Review][Defer] **Duplicate pvcName entries in spec.PVCNames produce AlreadyExists on second VR** — Two identical PVCNames generate the same CR name. Executor provides deduplicated lists. [driver.go:129-148]
- [x] [Review][Defer] **VGR create failure after PVC labeling leaves orphan labels** — On retry the idempotent label check is a no-op (labels already correct), so retry works; orphan labels are cosmetically wrong but harmless. [driver.go:167-205]
- [x] [Review][Defer] **Concurrent creates can race past getByName check** — Kubernetes reconcilers are single-threaded per object; executor is synchronized per DRExecution. Not actionable in this story. [driver.go:97-118]
- [x] [Review][Defer] **VR object naming may exceed K8s 253-char name limit** — In practice, VG names are well under the limit. Defensive truncation/hashing is a future hardening item. [driver.go:131]

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

## Dev Agent Record

### Implementation Plan

- **Rendering rule:** VG name prefix determines CR type — `vm-*` → individual VolumeReplication CRs (one per PVC), `ns-*` → single VolumeGroupReplication CR
- **Site role:** Passed via `VolumeGroupSpec.Labels[SiteRoleLabel]`; defaults to primary when absent
- **Idempotency:** Before creating, `getByName` lists VR/VGR CRs by `soteria.io/volume-group` label; returns existing info if found
- **VolumeGroupID:** Deterministic `"csi-ext-" + vgName` format; reversible via prefix strip
- **VGR PVC selection:** Driver labels PVCs with `soteria.io/volume-group` then creates VGR with source selector matching that label
- **DeleteVolumeGroup:** Lists+deletes both VR and VGR CRs by label; nil for non-existent (idempotent)
- **GetVolumeGroup:** Lists CRs by label; VR case extracts PVCNames from DataSource; VGR case resolves PVCNames from source selector

### Debug Log

No debug issues encountered.

### Completion Notes

All 7 tasks complete. 30 unit tests pass (including 12 from Story 12.2 retained for continuity). Coverage 83.8%. 0 lint issues. All unit and integration tests pass with zero regressions.

## File List

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/driver.go` | Modified — implemented CreateVolumeGroup (VR+VGR rendering), DeleteVolumeGroup, GetVolumeGroup with idempotency and label-based lookup |
| `pkg/drivers/csiextension/driver_test.go` | Modified — 30 tests covering single-VM VR (1+3 PVCs), multi-VM VGR, secondary state, idempotency, delete, get, not-found, context cancellation |
| `pkg/drivers/csiextension/constants.go` | Modified — added LabelVolumeGroup, LabelDRPlan, SiteRoleLabel, SiteRolePrimary, SiteRoleSecondary constants |
| `pkg/drivers/csiextension/doc.go` | Modified — updated package doc with Story 12.3 rendering rule and label conventions |

## Change Log

- **2026-05-14:** Story 12.3 implemented — CreateVolumeGroup/DeleteVolumeGroup/GetVolumeGroup with VR/VGR rendering rule, site role determination via labels, idempotent create/delete, label-based CR identification, 30 tests at 83.8% coverage, 0 lint issues
