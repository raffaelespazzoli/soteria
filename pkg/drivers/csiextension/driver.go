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
	"context"
	"fmt"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// DriverName is the plan-level name used in DRPlanSpec.VolumeReplicationDriver.
const DriverName = "csi-extension"

var _ drivers.StorageProvider = (*Driver)(nil)

// Driver is a StorageProvider that manages volume replication through CSI
// Addons VolumeReplication and VolumeGroupReplication CRDs. Fields (client,
// config, etc.) are added in Story 12.2.
type Driver struct{}

// New creates a new csi-extension Driver.
func New() *Driver {
	return &Driver{}
}

func (d *Driver) CreateVolumeGroup(_ context.Context, _ drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) {
	return drivers.VolumeGroupInfo{}, fmt.Errorf("csi-extension: CreateVolumeGroup not yet implemented")
}

func (d *Driver) DeleteVolumeGroup(_ context.Context, _ drivers.VolumeGroupID) error {
	return fmt.Errorf("csi-extension: DeleteVolumeGroup not yet implemented")
}

func (d *Driver) GetVolumeGroup(_ context.Context, _ drivers.VolumeGroupID) (drivers.VolumeGroupInfo, error) {
	return drivers.VolumeGroupInfo{}, fmt.Errorf("csi-extension: GetVolumeGroup not yet implemented")
}

func (d *Driver) SetSource(_ context.Context, _ drivers.VolumeGroupID) error {
	return fmt.Errorf("csi-extension: SetSource not yet implemented")
}

func (d *Driver) StopReplication(_ context.Context, _ drivers.VolumeGroupID) error {
	return fmt.Errorf("csi-extension: StopReplication not yet implemented")
}

func (d *Driver) GetReplicationStatus(_ context.Context, _ drivers.VolumeGroupID) (drivers.ReplicationStatus, error) {
	return drivers.ReplicationStatus{}, fmt.Errorf("csi-extension: GetReplicationStatus not yet implemented")
}

func init() {
	drivers.RegisterDriver(DriverName, func() drivers.StorageProvider {
		return New()
	})
}
