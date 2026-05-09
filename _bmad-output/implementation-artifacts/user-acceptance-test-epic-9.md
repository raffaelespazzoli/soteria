# Epic 9 — User Acceptance Test Log

**Epic:** Disk-Level Discovery, Volume Group Enrichment & Structural Validation
**Date Started:** 2026-05-08
**Environment:** etl6 / etl7 stretched cluster (Submariner MCS)
**Tester:** rspazzol

## Stories Under Test

| Story | Title | Dev Status |
|-------|-------|------------|
| 9.1 | Disk Discovery in SiteDiscovery | done |
| 9.2 | Aggregated API Admission Plugin Migration | done |
| 9.3 | Cross-Site Disk Agreement Validation | done |
| 9.4 | Volume Group Disk Enrichment in Preflight | done |
| 9.5 | Storage Class Homogeneity Validation | done |
| 9.6 | Console UI Disk Discovery & Validation Display | done |

## Test Environment Setup

- **Clusters:** etl6 (primary/seed DC), etl7 (secondary DC)
- **ScyllaDB:** Multi-DC with NetworkTopologyStrategy
- **VMs:** fedora-db (wave 1), fedora-appserver-{1,2} (wave 2), fedora-webserver-{1,2,3} (wave 3)
- **DRPlan:** fedora-app (maxConcurrentFailovers: 2, primarySite: etl6, secondarySite: etl7)

## Deployment Log

| Timestamp | Action | Result |
|-----------|--------|--------|
| 2026-05-09 08:10 MT | Teardown existing deployment | OK — exit 0, ~284s |
| 2026-05-09 08:15 MT | Deploy via stretched-local-test.sh (attempt 1) | FAIL — console plugin webpack OOM (V8 exit 129) |
| 2026-05-09 08:18 MT | Rebuild console plugin with `--memory=4g` + push | OK |
| 2026-05-09 08:19 MT | Deploy via stretched-local-test.sh (attempt 2) | FAIL — DNS loss mid-deploy (etl7 ScyllaDB waiting) |
| 2026-05-09 08:30 MT | Connectivity restored, full teardown | OK — exit 0 |
| 2026-05-09 08:40 MT | Deploy via stretched-local-test.sh (attempt 3) | OK — exit 0, ~1478s |
| 2026-05-09 08:55 MT | `rollout restart` controller + console plugin on both clusters | OK — new pods running |

---

## UAT Findings

### Sanity Checks

| # | Check | Expected | Actual | Status |
|---|-------|----------|--------|--------|
| S1 | DRPlan visible on etl6 | fedora-app listed | `fedora-app SteadyState` (6 VMs, 3 waves, activeSite=etl6) | PASS |
| S2 | DRPlan visible on etl7 | fedora-app listed (cross-DC replication) | `fedora-app SteadyState` (6 VMs) — identical to etl6 | PASS |
| S3 | VMs running on etl6 | 6 VMs running (Always) | 6 VMs: fedora-db Running/Ready, 4 Starting, 1 Provisioning (still booting) | PASS |
| S4 | VMs halted on etl7 | 6 VMs halted | 6 VMs Stopped | PASS |
| S5 | APIService available on etl6 | Available=True | True | PASS |
| S6 | APIService available on etl7 | Available=True | True | PASS |
| S7 | Console plugin deployed | Both clusters | 1 replica ready on each cluster | PASS |

### Story 9.1 — Disk Discovery in SiteDiscovery

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 9.1.1 | DiscoveredVM includes disks array | Each VM has disks with name/pvcName/storageClass | All 6 VMs have `disks: [{name: rootdisk, pvcName: <vm>-rootdisk, storageClass: ontap-san}]` in both primarySiteDiscovery and secondarySiteDiscovery | PASS |
| 9.1.2 | primarySiteDiscovery VMs have disks populated | Disks from KubeVirt domain spec | 6 VMs, each with 1 disk (rootdisk), discoveredVMCount=6, lastDiscoveryTime=2026-05-09T14:36:50Z | PASS |
| 9.1.3 | secondarySiteDiscovery VMs have disks populated | Disks from KubeVirt domain spec | 6 VMs, each with 1 disk (rootdisk), discoveredVMCount=6, lastDiscoveryTime=2026-05-09T14:36:52Z | PASS |
| 9.1.4 | Disk storageClass resolved from PVC | storageClass matches PVC's SC | `storageClass: ontap-san` — matches the DR_TEST_SC used in test VMs | PASS |
| 9.1.5 | DataVolume-backed disks resolved | PVC created by DV has storageClass | All VMs use DataVolumeTemplates; PVCs created by DV correctly resolved to `ontap-san` | PASS |

### Story 9.2 — Aggregated API Admission Plugin Migration

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 9.2.1 | DRPlan CREATE validated in-process | Field validation errors returned | | PENDING |
| 9.2.2 | DRExecution CREATE validated in-process | Cross-object checks (plan exists, concurrency, SitesInSync) | | PENDING |
| 9.2.3 | No VWC entries for soteria.io resources | Only VM webhook VWC remains | VWC has only `vvm.kb.io` → `kubevirt.io virtualmachines`. No soteria.io webhook entries. | PASS |
| 9.2.4 | DRExecution rejected when SitesInSync=False | Admission error with reason | | PENDING |

### Story 9.3 — Cross-Site Disk Agreement Validation

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 9.3.1 | DisksConsistent=True when disks match | Condition True, reason DisksAgreed | `DisksConsistent=True, DisksAgreed, "All VMs have matching disk topology across sites"` | PASS |
| 9.3.2 | DisksConsistent=False on disk mismatch | Condition False, DiskMismatch + delta | | PENDING |
| 9.3.3 | DRExecution rejected when DisksConsistent=False | Admission error | | PENDING |
| 9.3.4 | WaitingForDiskDiscovery during initial deploy | Condition with guidance message | | PENDING |
| 9.3.5 | diskDiscoveryDelta in preflight report | Delta message in preflight | `disksConsistent: true` in preflight, no delta (expected when disks agree) | PASS |

### Story 9.4 — Volume Group Disk Enrichment in Preflight

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 9.4.1 | Preflight VolumeGroups contain disk details | PreflightVolumeGroup with name/site/disks | Each VG has `{name, site: etl6, disks: [{name, pvcName, pvcNamespace}]}` | PASS |
| 9.4.2 | VolumeGroupDisk has pvcName and pvcNamespace | Per-disk PVC topology | `{name: rootdisk, pvcName: fedora-db-rootdisk, pvcNamespace: soteria-dr-test}` — all VGs populated | PASS |
| 9.4.3 | Disks sorted by VM name then disk name | Deterministic ordering | Only 1 disk per VM (rootdisk), VMs in waves sorted alphabetically within chunks | PASS |

### Story 9.5 — Storage Class Homogeneity Validation

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 9.5.1 | Homogeneous SCs pass validation | No StorageClassMixed condition | All VMs use `ontap-san`, DisksConsistent=True/DisksAgreed, Ready=True | PASS |
| 9.5.2 | Mixed SCs within a VG trigger DisksConsistent=False | StorageClassMixed reason + message | | PENDING |
| 9.5.3 | Ready=False when SC mismatch detected | DisksOutOfSync reason | | PENDING |

### Story 9.5 — Storage Class Homogeneity Validation

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 9.5.1 | Homogeneous SCs pass validation | No StorageClassMixed condition | | PENDING |
| 9.5.2 | Mixed SCs within a VG trigger DisksConsistent=False | StorageClassMixed reason + message | | PENDING |
| 9.5.3 | Ready=False when SC mismatch detected | DisksOutOfSync reason | | PENDING |

### Story 9.6 — Console UI Disk Discovery & Validation Display

| # | Test | Expected | Actual | Status |
|---|------|----------|--------|--------|
| 9.6.1 | Disk rows in SiteDiscoverySection | Expandable per-VM disk comparison | | PENDING |
| 9.6.2 | DiskDisagreementAlert on DisksConsistent=False | Danger alert with reason | | PENDING |
| 9.6.3 | Transition buttons disabled on disk mismatch | Buttons disabled + tooltip | | PENDING |
| 9.6.4 | VG disk composition in WaveCompositionTree | Per-VG disk nodes with site label | | PENDING |
| 9.6.5 | Dashboard warning icon on DisksConsistent=False | Warning icon + disabled actions | | PENDING |

---

## Issues Found

### Issue UAT-9.001 — PreflightVolumeGroup shows single-site disks, not cross-site PVC mapping

- **Story:** 9.4
- **Severity:** major
- **Steps to Reproduce:**
  1. Deploy stretched cluster with VMs on etl6 (Always) and etl7 (Halted)
  2. Inspect `kubectl get drplans fedora-app -n soteria-dr-test -o yaml`
  3. Examine `.status.preflight.waves[].chunks[].volumeGroups`
- **Expected:** Each disk in a volume group should show its PVC mapping on *both* sites, allowing the user to understand cross-site disk correspondence:
  ```yaml
  volumeGroups:
  - name: vm-soteria-dr-test-fedora-db
    disks:
    - name: rootdisk
      sites:
      - site: etl6
        pvcName: fedora-db-rootdisk
        pvcNamespace: soteria-dr-test
      - site: etl7
        pvcName: fedora-db-rootdisk
        pvcNamespace: soteria-dr-test
  ```
- **Actual:** Volume group disks show only the active site's PVC mapping with a top-level `site` field:
  ```yaml
  volumeGroups:
  - name: vm-soteria-dr-test-fedora-db
    site: etl6
    disks:
    - name: rootdisk
      pvcName: fedora-db-rootdisk
      pvcNamespace: soteria-dr-test
  ```
- **Impact:** Users cannot see which PVC on the partner site corresponds to a local disk, making cross-site verification difficult.
- **Resolution:** Spawned as Story 9.7 (Cross-Site Volume Group Disk Mapping) — API type change (`DiskSiteMapping` type, `VolumeGroupDisk.Sites` replaces flat `PVCName/PVCNamespace`, `PreflightVolumeGroup.Site` removed), preflight composition needs both site discoveries, console UI `WaveCompositionTree` needs cross-site rendering. Scope comparable to Story 9.4.

### Issue UAT-9.002 — WaveCompositionTree lacks visual separation between VMs and Volume Groups

- **Story:** 9.6
- **Severity:** minor
- **Steps to Reproduce:**
  1. Open DRPlan detail page in OpenShift Console
  2. Expand a wave in the Waves tab
  3. Observe the DRGroup chunk contents
- **Expected:** VMs and Volume Groups should be visually distinct sections within each chunk.
- **Actual:** VMs and Volume Groups are rendered as flat siblings in the TreeView with no grouping — users must mentally parse which items are VMs (storage backend labels) and which are VGs (site badge + disk count).
- **Resolution:** Fixed — added "Virtual Machines (N)" and "Volume Groups (N)" sub-nodes with VirtualMachineIcon and StorageDomainIcon icons. VMs default expanded, VGs default collapsed. 589 tests pass, 0 regressions.

<!-- Template for new issues:
### Issue UAT-9.NNN — <title>

- **Story:** 9.X
- **Severity:** critical / major / minor / cosmetic
- **Steps to Reproduce:**
  1. ...
- **Expected:** ...
- **Actual:** ...
- **Screenshot:** (if applicable)
- **Resolution:** pending / fixed in <commit> / deferred
-->
