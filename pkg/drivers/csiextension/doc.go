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

// Package csiextension implements a StorageProvider that manages volume
// replication through CSI Addons VolumeReplication and VolumeGroupReplication
// Kubernetes CRDs. The csi-addons sidecar container reconciles these CRDs into
// actual storage-level replication operations.
//
// This driver is progressively implemented across Stories 12.1–12.5.
// Story 12.3 implements CreateVolumeGroup, DeleteVolumeGroup, and
// GetVolumeGroup using a rendering rule based on VG name prefix:
//
//   - "vm-*" (single-VM) → one VolumeReplication CR per PVC
//   - "ns-*" (namespace-level, multi-VM) → one VolumeGroupReplication CR
//
// Created CRs are labeled with soteria.io/volume-group and
// soteria.io/drplan for identification and idempotent re-lookup. The
// initial spec.replicationState is determined by the site role passed in
// VolumeGroupSpec.Labels[SiteRoleLabel].
//
// Story 12.4 implements StopReplication and SetSource as declarative
// state transitions on the VR/VGR CRs' spec.replicationState field.
// StopReplication reads the current state and flips the replication
// direction (primary→secondary, secondary/resync→primary). SetSource
// unconditionally sets the state to primary (idempotent). Both methods
// locate CRs via the soteria.io/volume-group label and skip updates
// when the CR is already in the target state.
//
// Unlike the noop driver, csi-extension requires a Kubernetes client at
// construction time and cannot use init() for self-registration. Instead,
// main.go registers it after the controller-runtime manager is created:
//
//	drivers.RegisterDriver(csiextension.DriverName, func() drivers.StorageProvider {
//	    return csiextension.New(mgr.GetClient())
//	})
//
// The VolumeReplicationClass for VR CRs is specified per-DRPlan in
// DRPlanSpec.VolumeReplicationDriver.VolumeReplicationClass and passed to
// the driver through VolumeGroupSpec.Labels[VolumeReplicationClassLabel].
// The VolumeGroupReplicationClass for VGR CRs is derived by convention
// from the same value and passed via Labels[VolumeGroupReplicationClassLabel].
package csiextension
