# Story 12.5: GetReplicationStatus — Health Monitoring

Status: done

## Story

As a DRPlan controller polling replication health,
I want `GetReplicationStatus` to read VolumeReplication/VolumeGroupReplication CRD status and map it to Soteria's ReplicationStatus,
So that the preflight report, replication health conditions, and reprotect health monitoring reflect actual storage replication state.

## Background

### CSI Addons Status Model

VolumeReplication and VolumeGroupReplication CRDs report status through:
- `status.state` — current observed state (`Primary`, `Secondary`, `Resync`, `Unknown`)
- `status.conditions` — Kubernetes conditions with `Completed`, `Degraded`, `Resyncing` types
- `status.message` — human-readable status message
- `GetVolumeReplicationInfo` response: `last_sync_time`, `last_sync_duration`, `last_sync_bytes`, `status` (HEALTHY/DEGRADED/ERROR/UNKNOWN)

### Health Mapping

| CSI Status | Soteria ReplicationHealth |
|---|---|
| `HEALTHY` / `Completed=True` | `Healthy` |
| `DEGRADED` / `Degraded=True` | `Degraded` |
| `Resyncing=True` | `Syncing` |
| `ERROR` / all conditions False | `Unknown` |
| `UNKNOWN` | `Unknown` |
| VR/VGR not found | `NotReplicating` |

### Role Mapping

| CSI `status.state` | Soteria VolumeRole |
|---|---|
| `Primary` | `Source` |
| `Secondary` | `Target` |
| `Resync` | `Target` |
| `Unknown` | `NonReplicated` |

### VR Aggregation (Single-VM Path)

For single-VM volume groups with multiple VR CRs (one per PVC), health is aggregated using worst-health-wins: if any PVC reports `Degraded`, the group is `Degraded`. The role comes from the first CR (all PVCs in a VG share the same role). `LastSyncTime` is the oldest (minimum) across all PVCs.

## Acceptance Criteria

1. **AC1 — GetReplicationStatus returns role:** The driver reads VR/VGR status and maps `status.state` to Soteria `VolumeRole` (Source/Target/NonReplicated).

2. **AC2 — GetReplicationStatus returns health:** The driver reads VR/VGR status/conditions and maps to Soteria `ReplicationHealth` (Healthy/Degraded/Syncing/NotReplicating/Unknown).

3. **AC3 — GetReplicationStatus returns LastSyncTime:** The driver reads `last_sync_time` from VR/VGR status and returns it as `ReplicationStatus.LastSyncTime`.

4. **AC4 — VR aggregation:** For single-VM VGs with multiple VR CRs, health is aggregated (worst health wins), role comes from first CR, LastSyncTime is the oldest.

5. **AC5 — VGR direct mapping:** For multi-VM VGs, the single VGR CR's status is mapped directly.

6. **AC6 — Not found returns NotReplicating:** If no VR/VGR CRs exist for the VolumeGroupID, returns `ReplicationStatus{Role: NonReplicated, Health: NotReplicating}`.

7. **AC7 — Graceful degradation:** If VR/VGR status fields are empty or conditions are absent, returns `Health: Unknown` rather than erroring.

8. **AC8 — Unit tests:** Tests covering all health/role mappings, VR aggregation, VGR direct mapping, not-found, and empty-status cases.

## Tasks / Subtasks

- [x] Task 1: Implement role mapping (AC: #1)
  - [x] 1.1 Map `status.state` string to Soteria `VolumeRole`
  - [x] 1.2 Handle unknown/empty states gracefully

- [x] Task 2: Implement health mapping (AC: #2, #7)
  - [x] 2.1 Map CSI status/conditions to Soteria `ReplicationHealth`
  - [x] 2.2 Handle empty conditions gracefully

- [x] Task 3: Implement LastSyncTime extraction (AC: #3)
  - [x] 3.1 Read `last_sync_time` from VR/VGR status
  - [x] 3.2 Convert to `*time.Time`

- [x] Task 4: VR aggregation (AC: #4)
  - [x] 4.1 Read all VR CRs for the VG
  - [x] 4.2 Aggregate health (worst wins)
  - [x] 4.3 Pick role from first CR
  - [x] 4.4 Pick oldest LastSyncTime

- [x] Task 5: VGR direct mapping (AC: #5)
  - [x] 5.1 Read single VGR CR status
  - [x] 5.2 Map directly to ReplicationStatus

- [x] Task 6: Not-found handling (AC: #6)
  - [x] 6.1 Return NotReplicating when no CRs found

- [x] Task 7: Unit tests (AC: #8)
  - [x] 7.1 Test all role mappings (Primary→Source, Secondary→Target, etc.)
  - [x] 7.2 Test all health mappings
  - [x] 7.3 Test VR aggregation with mixed health
  - [x] 7.4 Test VGR direct mapping
  - [x] 7.5 Test not-found returns NotReplicating
  - [x] 7.6 Test empty-status returns Unknown
  - [x] 7.7 Run `make test` and `make lint`

## Dev Notes

### Key Locations

| File | Action |
|------|--------|
| `pkg/drivers/csiextension/driver.go` | Implement `GetReplicationStatus` |
| `pkg/drivers/csiextension/status.go` | New — health/role mapping helpers |
| `pkg/drivers/csiextension/driver_test.go` | Tests |

### Health Aggregation

```go
func worstHealth(healths []drivers.ReplicationHealth) drivers.ReplicationHealth {
    priority := map[drivers.ReplicationHealth]int{
        drivers.HealthUnknown:         0,
        drivers.HealthDegraded:        1,
        drivers.HealthSyncing:         2,
        drivers.HealthNotReplicating:  3,
        drivers.HealthHealthy:         4,
    }
    worst := drivers.HealthHealthy
    for _, h := range healths {
        if priority[h] < priority[worst] {
            worst = h
        }
    }
    return worst
}
```

### What NOT to Change

- `ReplicationStatus` type in `pkg/drivers/types.go`
- `VolumeGroupHealth` mapping in `pkg/controller/drplan/health.go`
- ReprotectHandler health monitoring — it calls GetReplicationStatus through the interface

### Dependency

- **Depends on Story 12.2** — CRD types and client
- **Depends on Story 12.3** — CRs must exist

### Build Commands

```bash
make test
make lint-fix && make lint
```

## Dev Agent Record

### Implementation Plan

Implemented GetReplicationStatus for the CSI Extension driver with:
- `status.go`: Pure mapping helpers — `mapRole` (CSI State → VolumeRole), `mapHealth` (CSI conditions → ReplicationHealth with priority-based precedence: Degraded > Resyncing > Completed), `worstHealth` (aggregation), `oldestSyncTime`, `statusFromVR`, `aggregateVRStatus`, `statusFromVGR`
- `driver.go`: `GetReplicationStatus` method — routes single-VM (vm-*) to VR aggregation path, multi-VM (ns-*) to VGR direct mapping path; returns NotReplicating for not-found; ctx.Err() guard
- `doc.go`: Updated with Story 12.5 description

### Completion Notes

All 8 ACs satisfied:
- AC1: mapRole handles Primary→Source, Secondary→Target, Unknown/empty→NonReplicated (4 test cases)
- AC2: mapHealth handles Completed→Healthy, Degraded→Degraded, Resyncing→Syncing, with precedence rules (8 test cases)
- AC3: LastSyncTime extracted from VR/VGR status, converted to *time.Time
- AC4: VR aggregation — worst-health-wins, first-CR role, oldest sync time (3 aggregation tests + 7 worstHealth unit tests)
- AC5: VGR direct mapping (2 test cases)
- AC6: Not-found returns ErrVolumeGroupNotFound (2 test cases — VR and VGR paths)
- AC7: Empty status returns {NonReplicated, Unknown} gracefully (2 test cases)
- AC8: 19 new test functions covering all mappings, aggregation, not-found, empty-status, context cancellation

Coverage: 89.4% (up from 86.7%), 0 lint issues, all unit + integration tests pass.

## File List

- `pkg/drivers/csiextension/status.go` — NEW: health/role mapping helpers, VR aggregation, VGR mapping
- `pkg/drivers/csiextension/driver.go` — MODIFIED: GetReplicationStatus implementation (replaced stub)
- `pkg/drivers/csiextension/driver_test.go` — MODIFIED: 19 new test functions
- `pkg/drivers/csiextension/doc.go` — MODIFIED: Added Story 12.5 description

## Change Log

- 2026-05-15: Implemented GetReplicationStatus with health/role mapping, VR aggregation, VGR direct mapping, not-found/empty-status graceful handling, 19 new tests, 89.4% coverage, 0 lint issues

### Review Findings

- [x] [Review][Patch] Align GetReplicationStatus not-found behavior with the shared driver contract [`pkg/drivers/csiextension/driver.go`:`GetReplicationStatus`] — Return `drivers.ErrVolumeGroupNotFound` for missing VR/VGR objects and update the new Story 12.5 tests/docs accordingly so the CSI extension driver stays consistent with the `StorageProvider` interface and conformance suite.
