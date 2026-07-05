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
	"testing"
)

func TestParseVolumeHandle(t *testing.T) {
	tests := []struct {
		name    string
		handle  string
		wantErr bool
		wantVH  VolumeHandle
	}{
		{
			name:   "standard rook-ceph handle",
			handle: "0001-0009-rook-ceph-0000000000000001-7f3da9a2-abcd-1234-ef56-789012345678",
			wantVH: VolumeHandle{
				Version:   "0001",
				ClusterID: "rook-ceph",
				PoolIDHex: "0000000000000001",
				ImageUUID: "7f3da9a2-abcd-1234-ef56-789012345678",
			},
		},
		{
			name:   "different cluster ID length",
			handle: "0001-000d-my-cluster-id-000000000000000a-aabbccdd-1122-3344-5566-778899001122",
			wantVH: VolumeHandle{
				Version:   "0001",
				ClusterID: "my-cluster-id",
				PoolIDHex: "000000000000000a",
				ImageUUID: "aabbccdd-1122-3344-5566-778899001122",
			},
		},
		{
			name:   "pool ID = 255 (0xff)",
			handle: "0001-0009-rook-ceph-00000000000000ff-11111111-2222-3333-4444-555555555555",
			wantVH: VolumeHandle{
				Version:   "0001",
				ClusterID: "rook-ceph",
				PoolIDHex: "00000000000000ff",
				ImageUUID: "11111111-2222-3333-4444-555555555555",
			},
		},
		{
			name:    "too short",
			handle:  "0001",
			wantErr: true,
		},
		{
			name:    "invalid format",
			handle:  "not-a-volume-handle",
			wantErr: true,
		},
		{
			name:    "empty string",
			handle:  "",
			wantErr: true,
		},
		{
			name:    "bad cluster ID length hex",
			handle:  "0001-ZZZZ-rook-ceph-0000000000000001-uuid",
			wantErr: true,
		},
		{
			name:    "cluster ID length exceeds remainder",
			handle:  "0001-ffff-short-0000000000000001-uuid",
			wantErr: true,
		},
		{
			name:    "missing pool-ID segment",
			handle:  "0001-0004-test",
			wantErr: true,
		},
		{
			name:    "pool-ID too short",
			handle:  "0001-0004-test-00000001-uuid",
			wantErr: true,
		},
		{
			name:    "non-hex pool-ID segment",
			handle:  "0001-0009-rook-ceph-zzzzzzzzzzzzzzzz-7f3da9a2-abcd-1234-ef56-789012345678",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vh, err := ParseVolumeHandle(tt.handle)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got VolumeHandle: %+v", vh)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if vh.Version != tt.wantVH.Version {
				t.Errorf("Version = %q, want %q", vh.Version, tt.wantVH.Version)
			}
			if vh.ClusterID != tt.wantVH.ClusterID {
				t.Errorf("ClusterID = %q, want %q", vh.ClusterID, tt.wantVH.ClusterID)
			}
			if vh.PoolIDHex != tt.wantVH.PoolIDHex {
				t.Errorf("PoolIDHex = %q, want %q", vh.PoolIDHex, tt.wantVH.PoolIDHex)
			}
			if vh.ImageUUID != tt.wantVH.ImageUUID {
				t.Errorf("ImageUUID = %q, want %q", vh.ImageUUID, tt.wantVH.ImageUUID)
			}
		})
	}
}

func TestRewritePoolID(t *testing.T) {
	tests := []struct {
		name      string
		handle    string
		newPoolID int
		wantErr   bool
		want      string
	}{
		{
			name:      "basic rewrite pool 1 to 2",
			handle:    "0001-0009-rook-ceph-0000000000000001-7f3da9a2-abcd-1234-ef56-789012345678",
			newPoolID: 2,
			want:      "0001-0009-rook-ceph-0000000000000002-7f3da9a2-abcd-1234-ef56-789012345678",
		},
		{
			name:      "large pool ID = 255",
			handle:    "0001-0009-rook-ceph-0000000000000001-11111111-2222-3333-4444-555555555555",
			newPoolID: 255,
			want:      "0001-0009-rook-ceph-00000000000000ff-11111111-2222-3333-4444-555555555555",
		},
		{
			name:      "pool ID = 0",
			handle:    "0001-0009-rook-ceph-00000000000000ff-aabbccdd-1122-3344-5566-778899001122",
			newPoolID: 0,
			want:      "0001-0009-rook-ceph-0000000000000000-aabbccdd-1122-3344-5566-778899001122",
		},
		{
			name:      "different cluster ID",
			handle:    "0001-000d-my-cluster-id-000000000000000a-aabbccdd-1122-3344-5566-778899001122",
			newPoolID: 3,
			want:      "0001-000d-my-cluster-id-0000000000000003-aabbccdd-1122-3344-5566-778899001122",
		},
		{
			name:      "non-Ceph handle returns error",
			handle:    "invalid-handle",
			newPoolID: 1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RewritePoolID(tt.handle, tt.newPoolID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("RewritePoolID() = %q, want %q", got, tt.want)
			}
		})
	}
}
