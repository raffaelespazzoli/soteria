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

package drivers

import "time"

// VolumeGroupID uniquely identifies a volume group managed by a storage driver.
// The value is opaque to the orchestrator — its format is driver-specific
// (e.g., a CSI volume group handle, a Dell PowerStore volume group UUID).
type VolumeGroupID string

// ReplicationState describes the current replication status of a volume group.
// Drivers report this state through GetReplicationInfo; the workflow engine
// uses it to decide which DR operations are valid at each lifecycle stage.
type ReplicationState string

const (
	// ReplicationActive indicates replication is healthy and synchronising.
	ReplicationActive ReplicationState = "Active"

	// ReplicationDegraded indicates replication is running but behind target RPO.
	ReplicationDegraded ReplicationState = "Degraded"

	// ReplicationStopped indicates replication has been explicitly disabled.
	ReplicationStopped ReplicationState = "Stopped"

	// ReplicationPromoted indicates the volume group has been promoted to primary.
	ReplicationPromoted ReplicationState = "Promoted"

	// ReplicationDemoted indicates the volume group has been demoted to secondary.
	ReplicationDemoted ReplicationState = "Demoted"

	// ReplicationResyncing indicates a resync operation is in progress.
	ReplicationResyncing ReplicationState = "Resyncing"
)

// VolumeGroupSpec describes the desired volume group to create. The orchestrator
// populates this from PVC references discovered during DRPlan reconciliation.
type VolumeGroupSpec struct {
	// Name is a human-readable identifier for the volume group.
	Name string

	// Namespace is the Kubernetes namespace containing the source PVCs.
	Namespace string

	// PVCNames lists the PersistentVolumeClaim names to include in the group.
	PVCNames []string

	// Labels are propagated to the underlying storage volume group for
	// identification and filtering by the driver.
	Labels map[string]string
}

// VolumeGroupInfo describes an existing volume group as returned by the driver.
type VolumeGroupInfo struct {
	// ID is the driver-assigned unique identifier for this volume group.
	ID VolumeGroupID

	// Name is the human-readable name assigned at creation.
	Name string

	// PVCNames lists the PVCs currently in the group.
	PVCNames []string
}

// ReplicationInfo reports the current replication state for a volume group.
// The workflow engine reads these fields to assess readiness before failover
// and to report estimated RPO in DRExecution status.
type ReplicationInfo struct {
	// State is the current replication lifecycle state.
	State ReplicationState

	// LastSyncTime is the timestamp of the most recent successful data sync.
	// Nil if the driver has never completed a sync or cannot report it.
	LastSyncTime *time.Time

	// EstimatedRPO is the driver's estimate of data loss if failover happened now.
	// Nil if the driver cannot calculate RPO (e.g., replication not yet established).
	EstimatedRPO *time.Duration
}

// PromoteOptions configures a volume promotion operation.
type PromoteOptions struct {
	// Force skips graceful demote of the peer and forces promotion. Required
	// for disaster failover when the source site is unreachable; must not be
	// set for planned migration where both sites are healthy.
	Force bool
}

// DemoteOptions configures a volume demotion operation.
type DemoteOptions struct {
	// Force forces demotion even if the volume group has outstanding writes.
	// Used during re-protect when the previously-promoted site must become
	// secondary regardless of in-flight I/O.
	Force bool
}
