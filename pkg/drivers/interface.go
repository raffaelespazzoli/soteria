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
// storage backends (FR20). Every method must be idempotent — safe to retry after
// a crash or restart without side effects. All methods accept context.Context for
// cancellation and timeout propagation from the workflow engine. Implementations
// must return typed errors from pkg/drivers/errors.go, never raw error strings.
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

	// EnableReplication starts asynchronous replication for a volume group to
	// its configured peer. Idempotency: returns nil if replication is already
	// active or resyncing. Returns ErrVolumeGroupNotFound if the group does
	// not exist.
	EnableReplication(ctx context.Context, id VolumeGroupID) error

	// DisableReplication stops replication for a volume group. Idempotency:
	// returns nil if replication is already stopped or was never enabled.
	// Returns ErrVolumeGroupNotFound if the group does not exist.
	DisableReplication(ctx context.Context, id VolumeGroupID) error

	// PromoteVolume promotes a secondary volume group to primary, making it
	// writable. When opts.Force is true the driver skips waiting for a
	// graceful peer demote — required for disaster failover when the source
	// site is unreachable. Idempotency: returns nil if the volume group is
	// already promoted. Returns ErrPromotionFailed on unrecoverable errors.
	PromoteVolume(ctx context.Context, id VolumeGroupID, opts PromoteOptions) error

	// DemoteVolume demotes a primary volume group to secondary, making it
	// read-only and eligible for replication. When opts.Force is true the
	// driver forces demotion even with outstanding writes. Idempotency:
	// returns nil if the volume group is already demoted. Returns
	// ErrDemotionFailed on unrecoverable errors.
	DemoteVolume(ctx context.Context, id VolumeGroupID, opts DemoteOptions) error

	// ResyncVolume triggers a resynchronisation of a demoted volume group from
	// its promoted peer. Used during re-protect after failover to re-establish
	// replication in the reverse direction. Idempotency: returns nil if a
	// resync is already in progress. Returns ErrResyncFailed on unrecoverable
	// errors, ErrVolumeGroupNotFound if the group does not exist.
	ResyncVolume(ctx context.Context, id VolumeGroupID) error

	// GetReplicationInfo returns the current replication state and estimated
	// RPO for a volume group. The workflow engine polls this method to assess
	// readiness before failover. Returns ErrVolumeGroupNotFound if the group
	// does not exist, ErrReplicationNotReady if replication was never enabled.
	GetReplicationInfo(ctx context.Context, id VolumeGroupID) (ReplicationInfo, error)
}
