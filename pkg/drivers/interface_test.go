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

func (m *mockProvider) SetSource(_ context.Context, _ VolumeGroupID) error {
	return nil
}

func (m *mockProvider) StopReplication(_ context.Context, _ VolumeGroupID) error {
	return nil
}

func (m *mockProvider) GetReplicationStatus(_ context.Context, _ VolumeGroupID) (ReplicationStatus, error) {
	return ReplicationStatus{}, nil
}

// Compile-time interface satisfaction check.
var _ StorageProvider = (*mockProvider)(nil)
