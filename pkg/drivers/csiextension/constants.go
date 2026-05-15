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

package csiextension

import (
	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
)

// ReplicationState constants for CSI Addons VolumeReplication spec.replicationState.
// These are re-exported from the csi-addons API types for convenience.
const (
	ReplicationStatePrimary   = replicationv1alpha1.Primary
	ReplicationStateSecondary = replicationv1alpha1.Secondary
	ReplicationStateResync    = replicationv1alpha1.Resync
)

// VolumeReplicationClassLabel is the key in VolumeGroupSpec.Labels through
// which the executor passes the VolumeReplicationClass name from
// DRPlanSpec.VolumeReplicationDriver.VolumeReplicationClass to the driver.
const VolumeReplicationClassLabel = "soteria.io/volume-replication-class"

// VolumeGroupReplicationClassLabel is the key in VolumeGroupSpec.Labels
// through which the executor passes the VolumeGroupReplicationClass name
// to the driver. By convention, the executor derives this from the plan's
// VolumeReplicationClass value when no explicit override is configured.
const VolumeGroupReplicationClassLabel = "soteria.io/volume-group-replication-class"
