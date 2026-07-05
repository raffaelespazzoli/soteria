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
	"fmt"
	"strconv"
	"strings"
)

// VolumeHandle represents a parsed Rook-Ceph CSI volume handle.
//
// Format: <ver>-<clusterIDLenHex4>-<clusterID>-<poolIDHex16>-<imageUUID>
// Example: 0001-0009-rook-ceph-0000000000000001-7f3da9a2-abcd-1234-ef56-789012345678
type VolumeHandle struct {
	Version   string // e.g. "0001"
	ClusterID string // e.g. "rook-ceph" (variable length, may contain dashes)
	PoolIDHex string // 16-char zero-padded hex pool ID
	ImageUUID string // RBD image UUID (36 chars with dashes)
}

// ParseVolumeHandle splits a Rook-Ceph CSI volume handle into its components.
// Returns an error for handles that don't match the expected format.
func ParseVolumeHandle(handle string) (VolumeHandle, error) {
	parts := strings.SplitN(handle, "-", 3)
	if len(parts) < 3 {
		return VolumeHandle{}, fmt.Errorf("invalid volume handle format: %s", handle)
	}

	version := parts[0]
	clusterIDLenHex := parts[1]

	clusterIDLen, err := strconv.ParseInt(clusterIDLenHex, 16, 32)
	if err != nil {
		return VolumeHandle{}, fmt.Errorf("parsing cluster ID length %q: %w", clusterIDLenHex, err)
	}

	remainder := parts[2]
	if int(clusterIDLen) > len(remainder) {
		return VolumeHandle{}, fmt.Errorf("cluster ID length %d exceeds remainder %q", clusterIDLen, remainder)
	}
	clusterID := remainder[:clusterIDLen]
	afterCluster := remainder[clusterIDLen:]

	// After cluster ID: -<poolIDHex16>-<uuid>
	if len(afterCluster) < 18 || afterCluster[0] != '-' {
		return VolumeHandle{}, fmt.Errorf("missing pool-ID segment after cluster ID in %q", handle)
	}
	afterCluster = afterCluster[1:] // strip leading dash

	if len(afterCluster) < 17 {
		return VolumeHandle{}, fmt.Errorf("pool-ID segment too short in %q", handle)
	}
	poolIDHex := afterCluster[:16]
	if _, err := strconv.ParseUint(poolIDHex, 16, 64); err != nil {
		return VolumeHandle{}, fmt.Errorf("pool-ID segment %q is not valid hex in %q", poolIDHex, handle)
	}
	if afterCluster[16] != '-' {
		return VolumeHandle{}, fmt.Errorf("expected dash after pool-ID in %q", handle)
	}
	imageUUID := afterCluster[17:]

	return VolumeHandle{
		Version:   version,
		ClusterID: clusterID,
		PoolIDHex: poolIDHex,
		ImageUUID: imageUUID,
	}, nil
}

// RewritePoolID replaces the pool-ID segment in a volume handle with the given pool ID.
func RewritePoolID(handle string, newPoolID int) (string, error) {
	vh, err := ParseVolumeHandle(handle)
	if err != nil {
		return "", err
	}
	newPoolHex := fmt.Sprintf("%016x", newPoolID)
	clusterIDLenHex := fmt.Sprintf("%04x", len(vh.ClusterID))
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		vh.Version, clusterIDLenHex, vh.ClusterID, newPoolHex, vh.ImageUUID), nil
}
