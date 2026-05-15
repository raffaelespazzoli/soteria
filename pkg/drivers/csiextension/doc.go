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
// Story 12.2 adds a controller-runtime client for managing VR/VGR resources,
// ReplicationState constants, and scheme registration.
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
