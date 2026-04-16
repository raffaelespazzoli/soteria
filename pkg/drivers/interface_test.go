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

// mockProvider is a minimal implementation that satisfies the StorageProvider
// interface. It exists only for the compile-time check below — no behaviour
// is exercised here. Real test mocks live in pkg/drivers/fake/.
type mockProvider struct{}

func (m *mockProvider) CreateVolumeGroup(_ context.Context, _ VolumeGroupSpec) (VolumeGroupInfo, error) {
	return VolumeGroupInfo{}, nil
}

func (m *mockProvider) DeleteVolumeGroup(_ context.Context, _ VolumeGroupID) error {
	return nil
}

func (m *mockProvider) GetVolumeGroup(_ context.Context, _ VolumeGroupID) (VolumeGroupInfo, error) {
	return VolumeGroupInfo{}, nil
}

func (m *mockProvider) EnableReplication(_ context.Context, _ VolumeGroupID) error {
	return nil
}

func (m *mockProvider) DisableReplication(_ context.Context, _ VolumeGroupID) error {
	return nil
}

func (m *mockProvider) PromoteVolume(_ context.Context, _ VolumeGroupID, _ PromoteOptions) error {
	return nil
}

func (m *mockProvider) DemoteVolume(_ context.Context, _ VolumeGroupID, _ DemoteOptions) error {
	return nil
}

func (m *mockProvider) ResyncVolume(_ context.Context, _ VolumeGroupID) error {
	return nil
}

func (m *mockProvider) GetReplicationInfo(_ context.Context, _ VolumeGroupID) (ReplicationInfo, error) {
	return ReplicationInfo{}, nil
}

// Compile-time interface satisfaction check.
var _ StorageProvider = (*mockProvider)(nil)
