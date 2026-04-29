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

package noop

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/soteria-project/soteria/pkg/drivers"
)

const ProvisionerName = "noop.soteria.io"

var _ drivers.StorageProvider = (*Driver)(nil)

type volumeGroupState struct {
	info      drivers.VolumeGroupInfo
	namespace string // stored to match idempotent re-create by Name+Namespace
	role      drivers.VolumeRole
	createdAt time.Time
}

// Driver is a no-op StorageProvider that tracks volume groups and replication
// roles in memory without performing actual storage operations. It is safe for
// concurrent use by multiple goroutines.
type Driver struct {
	mu           sync.RWMutex
	volumeGroups map[drivers.VolumeGroupID]*volumeGroupState
}

func New() *Driver {
	return &Driver{
		volumeGroups: make(map[drivers.VolumeGroupID]*volumeGroupState),
	}
}

func (d *Driver) CreateVolumeGroup(ctx context.Context, spec drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) {
	if err := ctx.Err(); err != nil {
		return drivers.VolumeGroupInfo{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Idempotency: Name+Namespace together form the logical identity of a volume
	// group. Same name in different namespaces must produce distinct groups.
	for _, state := range d.volumeGroups {
		if state.info.Name == spec.Name && state.namespace == spec.Namespace {
			log.FromContext(ctx).V(1).Info("No-op: Returning existing volume group", "volumeGroupID", state.info.ID)
			return copyInfo(state.info), nil
		}
	}

	vgID := drivers.VolumeGroupID("noop-" + uuid.NewString())
	info := drivers.VolumeGroupInfo{
		ID:       vgID,
		Name:     spec.Name,
		PVCNames: append([]string(nil), spec.PVCNames...), // copy so callers cannot mutate driver state
	}

	d.volumeGroups[vgID] = &volumeGroupState{
		info:      info,
		namespace: spec.Namespace,
		role:      drivers.RoleNonReplicated,
		createdAt: time.Now(),
	}

	log.FromContext(ctx).V(1).Info("No-op: Created volume group", "volumeGroupID", vgID)
	return copyInfo(info), nil
}

func (d *Driver) DeleteVolumeGroup(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	d.mu.Lock()
	delete(d.volumeGroups, id)
	d.mu.Unlock()

	log.FromContext(ctx).V(1).Info("No-op: Deleted volume group", "volumeGroupID", id)
	return nil
}

func (d *Driver) GetVolumeGroup(ctx context.Context, id drivers.VolumeGroupID) (drivers.VolumeGroupInfo, error) {
	if err := ctx.Err(); err != nil {
		return drivers.VolumeGroupInfo{}, err
	}

	d.mu.RLock()
	state, ok := d.volumeGroups[id]
	d.mu.RUnlock()

	if !ok {
		log.FromContext(ctx).V(1).Info("No-op: Volume group not found", "volumeGroupID", id)
		return drivers.VolumeGroupInfo{}, drivers.ErrVolumeGroupNotFound
	}

	log.FromContext(ctx).V(1).Info("No-op: Got volume group", "volumeGroupID", id)
	return copyInfo(state.info), nil
}

func (d *Driver) SetSource(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	state, ok := d.volumeGroups[id]
	if !ok {
		log.FromContext(ctx).V(1).Info("No-op: Volume group not found for SetSource", "volumeGroupID", id)
		return drivers.ErrVolumeGroupNotFound
	}

	if state.role == drivers.RoleSource {
		log.FromContext(ctx).V(1).Info("No-op: Already Source, no-op", "volumeGroupID", id)
		return nil
	}
	if state.role != drivers.RoleNonReplicated {
		log.FromContext(ctx).V(1).Info("No-op: Invalid transition for SetSource",
			"volumeGroupID", id, "currentRole", state.role)
		return drivers.ErrInvalidTransition
	}

	state.role = drivers.RoleSource

	log.FromContext(ctx).V(1).Info("No-op: Set volume group to Source", "volumeGroupID", id)
	return nil
}

func (d *Driver) StopReplication(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	state, ok := d.volumeGroups[id]
	if !ok {
		log.FromContext(ctx).V(1).Info("No-op: Volume group not found for StopReplication", "volumeGroupID", id)
		return drivers.ErrVolumeGroupNotFound
	}

	if state.role == drivers.RoleNonReplicated {
		log.FromContext(ctx).V(1).Info("No-op: Already NonReplicated, no-op", "volumeGroupID", id)
		return nil
	}

	state.role = drivers.RoleNonReplicated

	log.FromContext(ctx).V(1).Info("No-op: Stopped replication", "volumeGroupID", id)
	return nil
}

func (d *Driver) GetReplicationStatus(
	ctx context.Context, id drivers.VolumeGroupID,
) (drivers.ReplicationStatus, error) {
	if err := ctx.Err(); err != nil {
		return drivers.ReplicationStatus{}, err
	}

	d.mu.RLock()
	state, ok := d.volumeGroups[id]
	if !ok {
		d.mu.RUnlock()
		log.FromContext(ctx).V(1).Info("No-op: Volume group not found for GetReplicationStatus", "volumeGroupID", id)
		return drivers.ReplicationStatus{}, drivers.ErrVolumeGroupNotFound
	}
	role := state.role
	d.mu.RUnlock()

	if role == drivers.RoleNonReplicated {
		log.FromContext(ctx).V(1).Info("No-op: Got replication status", "volumeGroupID", id, "role", role)
		return drivers.ReplicationStatus{
			Role:   role,
			Health: drivers.HealthNotReplicating,
		}, nil
	}

	now := time.Now()

	log.FromContext(ctx).V(1).Info("No-op: Got replication status", "volumeGroupID", id, "role", role)
	return drivers.ReplicationStatus{
		Role:         role,
		Health:       drivers.HealthHealthy,
		LastSyncTime: &now,
	}, nil
}

// copyInfo returns a shallow copy of VolumeGroupInfo with a freshly allocated
// PVCNames slice so callers cannot mutate driver-internal state through aliasing.
func copyInfo(src drivers.VolumeGroupInfo) drivers.VolumeGroupInfo {
	dst := src
	dst.PVCNames = append([]string(nil), src.PVCNames...)
	return dst
}

func init() {
	drivers.RegisterDriver(ProvisionerName, func() drivers.StorageProvider {
		return New()
	})
	// The noop driver is the catch-all: any CSI provisioner not claimed by a
	// real DR storage driver falls through to noop (no storage-level
	// replication actions). This removes the need for an explicit flag.
	drivers.SetFallbackDriver(func() drivers.StorageProvider {
		return New()
	})
}
