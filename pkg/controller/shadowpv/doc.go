/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package shadowpv implements the ShadowPV publisher controller.
//
// The publisher watches VolumeReplication (VR) and VolumeGroupReplication (VGR)
// CRs that carry a soteria.io/drplan label. For each such CR the controller
// resolves the PVC→PV chain and publishes the backing PV spec into a
// cluster-scoped ShadowPV resource named <planName>-<vgName>.
//
// Watch triggers:
//   - VolumeReplication with soteria.io/drplan label present
//   - VolumeGroupReplication with soteria.io/drplan label present
//
// PV discovery:
//   - VR path: spec.dataSource.Name → PVC → pvc.Spec.VolumeName → PV
//   - VGR path: list PVCs by soteria.io/volume-group label → each PVC → PV
//
// ShadowPV lifecycle:
//   - Create: first VR/VGR reconcile creates ShadowPV with local-site entries
//   - Update: subsequent reconciles merge local entries, preserving remote-site entries
//   - Delete: VR/VGR deletion (via finalizer) removes local-site entries; if
//     no entries remain the ShadowPV is deleted
//
// The publisher runs on every site. Each site independently publishes its PVs
// into the shared ShadowPV (stored in ScyllaDB, visible cross-site via CDC
// replication). The consumer controller (package shadowpv, Story 15.6) reads
// remote entries and creates local PVs with pool-ID rewrite.
package shadowpv
