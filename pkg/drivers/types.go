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

// VolumeRole describes the replication role of a volume group. The role
// determines how the storage backend treats the volumes: NonReplicated means
// no replication is configured, Source means the volumes are the replication
// origin (read-write), and Target means the volumes are the replication
// destination (read-only). Transitions between roles always pass through
// NonReplicated — there is no direct Source-to-Target or Target-to-Source path.
type VolumeRole string

const (
	// RoleNonReplicated indicates the volume group has no active replication.
	// This is the initial state after creation and the intermediate state
	// during role changes (e.g., failover goes Source → NonReplicated via
	// StopReplication, then re-protect calls SetSource on the other site).
	RoleNonReplicated VolumeRole = "NonReplicated"

	// RoleSource indicates the volume group is the replication source (primary).
	// Volumes are read-write and data is replicated to the paired target.
	RoleSource VolumeRole = "Source"

	// RoleTarget indicates the volume group is the replication target (secondary).
	// Volumes are read-only and receive replicated data from the paired source.
	RoleTarget VolumeRole = "Target"
)

// ReplicationHealth qualifies the health of an active replication link.
// Health is orthogonal to VolumeRole — a Source volume can be Healthy,
// Degraded, or Syncing depending on the state of the replication link.
type ReplicationHealth string

const (
	// HealthHealthy indicates replication is running and within target RPO.
	HealthHealthy ReplicationHealth = "Healthy"

	// HealthDegraded indicates replication is running but behind target RPO.
	HealthDegraded ReplicationHealth = "Degraded"

	// HealthSyncing indicates a sync operation is in progress (initial sync
	// after SetSource, or catch-up after a temporary disruption).
	HealthSyncing ReplicationHealth = "Syncing"

	// HealthUnknown indicates the driver cannot determine replication health.
	// Returned when the volume is NonReplicated or when the storage backend
	// is unreachable.
	HealthUnknown ReplicationHealth = "Unknown"
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

// ReplicationStatus reports the current replication role and health for a
// volume group. The workflow engine reads these fields to assess readiness
// before failover and to report estimated RPO in DRExecution status.
type ReplicationStatus struct {
	// Role is the current replication role of the volume group.
	Role VolumeRole

	// Health qualifies the replication link health. Meaningful only when
	// Role is Source or Target; set to HealthUnknown for NonReplicated.
	Health ReplicationHealth

	// LastSyncTime is the timestamp of the most recent successful data sync.
	// Nil if the driver has never completed a sync or cannot report it.
	LastSyncTime *time.Time

	// EstimatedRPO is the driver's estimate of data loss if failover happened now.
	// Nil if the driver cannot calculate RPO (e.g., replication not yet established).
	EstimatedRPO *time.Duration
}
