# Deferred Work

## Deferred from: code review of 4-05-driver-registry-fallback-preflight-convergence (2026-04-19)

- CSI vs in-tree provisioner ambiguity — `KubeStorageClassLister.GetProvisioner` returns `sc.Provisioner` verbatim; legacy/migrated clusters with in-tree volume types may have provisioner strings that don't match CSI driver registry keys. Normalization or aliases may be needed for non-CSI environments.

## Deferred from: code review of 4-1-dr-state-machine-execution-controller (2026-04-19)

- FailedOver→Reprotecting transition not yet defined in state machine — Story 4.8 will design the reprotect mechanism and mode, then add this transition edge to `validTransitions`.
- Pre-existing test patterns: `StorageClass` creation in `suite_test.go` lacks AlreadyExists guard; manager Start goroutine error not propagated to test runner.

## Deferred from: code review of 7-2-live-execution-monitor-progressstepper (2026-04-28)

- Wave/group elapsed strings don't tick between K8s watch updates — `getWaveElapsed`/`getGroupElapsed` use `Date.now()` at render time but have no local interval. Values only refresh when K8s watch fires (every ~2-5s). Matches existing `TransitionProgressBanner` pattern. Adding per-wave/group tick intervals would increase complexity for marginal UX gain.

## Deferred from: code review of 10-8-sequential-chunk-execution-within-waves (2026-05-11)

- Filtered-wave execution uses slice index as status slot — `ExecuteWaveHandler`/`ExecuteFromWave` rebuild filtered chunk slices, then `executeWave` passes the filtered-loop index into `setGroupStatus`/`getGroupVMNames`; if earlier groups are skipped, updates can land in the wrong `wave.Groups` slot. This appears pre-existing rather than introduced by Story 10.8.

## Deferred from: code review of 12-3-createvolumegroup-deletevolumegroup-getvolumegroup (2026-05-14)

- PVC labels (`soteria.io/volume-group`) not cleaned up on `DeleteVolumeGroup` — only VR/VGR CRs are removed. PVC labels persist until reassignment or manual cleanup.
- `createVGR` overwrites existing `soteria.io/volume-group` label on PVCs without conflict detection — orchestration layer is expected to prevent double-enrollment.
- Same PVC can be enrolled under multiple VGs on the VR path (different VG names produce different VR CR names); orchestration layer assigns PVCs exclusively.
- Duplicate `pvcName` entries in `spec.PVCNames` produce `AlreadyExists` on the second VR — executor provides deduplicated lists.
- VGR create failure after PVC labeling leaves orphan labels on PVCs — functionally harmless on retry but cosmetically stale.
- Concurrent creates can race past `getByName` idempotency check — mitigated by single-threaded reconciler per object.
- VR object naming (`csi-ext-<vgName>-<pvcName>`) may exceed K8s 253-char name limit for unusually long names — in practice well under the limit.
