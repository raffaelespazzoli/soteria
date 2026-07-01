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

import "context"

// StorageProvider is the contract between the DR orchestrator and vendor-specific
// storage backends (FR20). The 7-method interface uses a role-based replication
// model with two engine-driven transitions routed through the NonReplicated state:
//
//	NonReplicated → Source        (SetSource)
//	Source        → NonReplicated (StopReplication)
//
// Additionally, ResyncVolume requests storage-layer data synchronization on the
// secondary site before a planned failover promotion. This is a CSI-level concept
// (Secondary → Resync → Secondary(Completed)) that does not change the engine's
// role model — it ensures zero data loss before the promotion step.
//
// The Target role still exists in [ReplicationStatus] — the paired site's driver
// may report its volumes as Target via [GetReplicationStatus]. However, the engine
// never explicitly sets a volume to Target; when one site calls SetSource, the
// paired site implicitly becomes the target as an admin precondition.
//
// Volume pairing is an admin precondition — the driver assumes that paired
// volumes are correctly configured on both storage instances before any
// replication method is called.
//
// Every method must be idempotent — safe to retry after a crash or restart
// without side effects. Drivers act as reconcilers: they check the actual
// storage state before applying changes, flipping roles only if necessary.
// All methods accept context.Context for cancellation and timeout propagation
// from the workflow engine. Implementations must return typed errors from
// pkg/drivers/errors.go, never raw error strings.
//
// External storage vendor engineers implement this interface in their own driver
// packages under pkg/drivers/<vendor>/ and register via init() + RegisterDriver.
// All implementations must pass the conformance test suite in pkg/drivers/conformance/.
type StorageProvider interface {
	// CreateVolumeGroup creates a new volume group containing the specified PVCs.
	// Idempotency: if a volume group with the same spec already exists, returns
	// its info without error. Returns the created (or existing) VolumeGroupInfo.
	CreateVolumeGroup(ctx context.Context, spec VolumeGroupSpec) (VolumeGroupInfo, error)

	// DeleteVolumeGroup removes a volume group and releases its resources.
	// Idempotency: returns nil if the volume group does not exist.
	// The underlying PVCs are not deleted — only the grouping is removed.
	DeleteVolumeGroup(ctx context.Context, id VolumeGroupID) error

	// GetVolumeGroup retrieves metadata for an existing volume group.
	// Returns ErrVolumeGroupNotFound if the group does not exist.
	GetVolumeGroup(ctx context.Context, id VolumeGroupID) (VolumeGroupInfo, error)

	// SetSource transitions a volume group to the Source role (replication
	// origin, read-write). Valid from NonReplicated; returns ErrInvalidTransition
	// if the current role is Target. The driver must handle unreachable peers
	// internally — resilience to network partitions is the driver's responsibility,
	// not the orchestrator's. Idempotency: returns nil if the volume group is
	// already Source. Returns ErrVolumeGroupNotFound if the group does not exist.
	SetSource(ctx context.Context, id VolumeGroupID) error

	// StopReplication transitions a volume group from Source or Target back to
	// NonReplicated. The driver must handle unreachable peers and outstanding
	// writes internally. Idempotency: returns nil if the volume group is already
	// NonReplicated. Returns ErrVolumeGroupNotFound if the group does not exist.
	StopReplication(ctx context.Context, id VolumeGroupID) error

	// ResyncVolume requests data resynchronization for a volume group on the
	// current secondary site. The driver sets spec.replicationState to resync
	// on the target VR/VGR CRs, instructing the storage backend to pull any
	// un-replicated data from the peer primary before a planned failover
	// promotion. This is a storage-layer synchronization request — it does not
	// change the engine's role model. Idempotency: returns nil if the volume
	// group is already in resync state. Returns ErrVolumeGroupNotFound if the
	// group does not exist.
	ResyncVolume(ctx context.Context, id VolumeGroupID) error

	// GetReplicationStatus returns the current replication role and health for
	// a volume group. The workflow engine polls this method to assess readiness
	// before failover. Returns ErrVolumeGroupNotFound if the group does not
	// exist.
	GetReplicationStatus(ctx context.Context, id VolumeGroupID) (ReplicationStatus, error)
}
