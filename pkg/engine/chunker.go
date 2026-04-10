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

// chunker.go partitions VolumeGroups within each wave into DRGroup chunks that
// respect maxConcurrentFailovers.
//
// Architecture: After consistency resolution produces VolumeGroups per wave,
// ChunkWaves bins them into DRGroup chunks. Each chunk holds at most
// maxConcurrent VMs (counted individually, not by VolumeGroup count).
// Namespace-level VolumeGroups are indivisible: all VMs in the group must
// land in the same chunk. VM-level VolumeGroups are single-VM and fill
// remaining capacity freely. Namespace groups are placed first (largest-first)
// because they are harder to fit; VM-level groups fill the gaps.

package engine

import (
	"fmt"
	"sort"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// ChunkInput holds the per-wave VolumeGroups to be chunked.
type ChunkInput struct {
	WaveGroups []WaveGroupWithVolumes
}

// WaveGroupWithVolumes pairs a wave key with its resolved VolumeGroups.
type WaveGroupWithVolumes struct {
	WaveKey      string
	VolumeGroups []soteriav1alpha1.VolumeGroupInfo
}

// DRGroupChunk is a set of VMs and VolumeGroups that will be failed over
// concurrently as one DRGroup.
type DRGroupChunk struct {
	Name         string
	VMs          []VMReference
	VolumeGroups []soteriav1alpha1.VolumeGroupInfo
}

// ChunkResult is the output of ChunkWaves.
type ChunkResult struct {
	Waves  []WaveChunks
	Errors []ChunkError
}

// WaveChunks holds the DRGroup chunks for a single wave.
type WaveChunks struct {
	WaveKey string
	Chunks  []DRGroupChunk
}

// ChunkError records a namespace group that cannot fit in any single chunk
// because it exceeds maxConcurrentFailovers.
type ChunkError struct {
	WaveKey       string
	Namespace     string
	GroupSize     int
	MaxConcurrent int
}

// ChunkWaves partitions VolumeGroups within each wave into DRGroup chunks.
//
// Crash-consistent snapshots require all VMs in a namespace-level VolumeGroup
// to have their volumes promoted atomically in the same DRGroup; splitting them
// across chunks would break consistency guarantees. Therefore namespace groups
// are indivisible: if a group has 3 VMs and maxConcurrent is 4, the group
// occupies 3 of the 4 slots in a single chunk. If a namespace group exceeds
// maxConcurrent entirely, it is recorded as a ChunkError because it can never
// be placed.
func ChunkWaves(input ChunkInput, maxConcurrent int) ChunkResult {
	var result ChunkResult

	for _, wg := range input.WaveGroups {
		wc, errs := chunkSingleWave(wg.WaveKey, wg.VolumeGroups, maxConcurrent)
		result.Waves = append(result.Waves, wc)
		result.Errors = append(result.Errors, errs...)
	}

	return result
}

func chunkSingleWave(
	waveKey string, groups []soteriav1alpha1.VolumeGroupInfo, maxConcurrent int,
) (WaveChunks, []ChunkError) {
	if len(groups) == 0 {
		return WaveChunks{WaveKey: waveKey}, nil
	}

	nsGroups, vmGroups := partitionGroups(groups)

	sort.Slice(nsGroups, func(i, j int) bool {
		return len(nsGroups[i].VMNames) > len(nsGroups[j].VMNames)
	})

	var chunks []DRGroupChunk
	var errors []ChunkError
	chunkIdx := 0
	remaining := 0 // no capacity until startNewChunk is called

	startNewChunk := func() {
		chunks = append(chunks, DRGroupChunk{
			Name: fmt.Sprintf("wave-%s-group-%d", waveKey, chunkIdx),
		})
		chunkIdx++
		remaining = maxConcurrent
	}

	addToCurrentChunk := func(vg soteriav1alpha1.VolumeGroupInfo) {
		c := &chunks[len(chunks)-1]
		c.VolumeGroups = append(c.VolumeGroups, vg)
		for _, vmName := range vg.VMNames {
			c.VMs = append(c.VMs, VMReference{
				Name:      vmName,
				Namespace: vg.Namespace,
			})
		}
		remaining -= len(vg.VMNames)
	}

	// Namespace groups are placed first (largest-first) because they are
	// indivisible: splitting a namespace group across chunks would cause its
	// VMs to be promoted at different times, breaking crash-consistency.
	for _, nsGroup := range nsGroups {
		size := len(nsGroup.VMNames)
		if size > maxConcurrent {
			errors = append(errors, ChunkError{
				WaveKey:       waveKey,
				Namespace:     nsGroup.Namespace,
				GroupSize:     size,
				MaxConcurrent: maxConcurrent,
			})
			continue
		}

		if len(chunks) == 0 || remaining < size {
			startNewChunk()
		}
		addToCurrentChunk(nsGroup)
	}

	for _, vmGroup := range vmGroups {
		if len(chunks) == 0 || remaining < 1 {
			startNewChunk()
		}
		addToCurrentChunk(vmGroup)
	}

	return WaveChunks{WaveKey: waveKey, Chunks: chunks}, errors
}

func partitionGroups(groups []soteriav1alpha1.VolumeGroupInfo) (ns, vm []soteriav1alpha1.VolumeGroupInfo) {
	for _, g := range groups {
		if g.ConsistencyLevel == soteriav1alpha1.ConsistencyLevelNamespace {
			ns = append(ns, g)
		} else {
			vm = append(vm, g)
		}
	}
	return
}
