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

package engine

import (
	"fmt"
	"testing"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func makeVMGroups(namespace string, count int) []soteriav1alpha1.VolumeGroupInfo {
	groups := make([]soteriav1alpha1.VolumeGroupInfo, count)
	for i := range count {
		name := fmt.Sprintf("vm-%s-%d", namespace, i)
		groups[i] = soteriav1alpha1.VolumeGroupInfo{
			Name:             fmt.Sprintf("vm-%s-%s", namespace, name),
			Namespace:        namespace,
			ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM,
			VMNames:          []string{name},
		}
	}
	return groups
}

func makeNSGroup(namespace string, vmCount int) soteriav1alpha1.VolumeGroupInfo {
	vmNames := make([]string, vmCount)
	for i := range vmCount {
		vmNames[i] = fmt.Sprintf("vm-%d", i)
	}
	return soteriav1alpha1.VolumeGroupInfo{
		Name:             fmt.Sprintf("ns-%s", namespace),
		Namespace:        namespace,
		ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace,
		VMNames:          vmNames,
	}
}

func totalVMs(chunks []DRGroupChunk) int {
	total := 0
	for _, c := range chunks {
		total += len(c.VMs)
	}
	return total
}

func TestChunkWaves(t *testing.T) {
	tests := []struct {
		name            string
		input           ChunkInput
		maxConcurrent   int
		wantChunkCounts []int // per-wave chunk counts
		wantErrorCount  int
		wantChunkSizes  [][]int // per-wave, per-chunk VM counts
	}{
		{
			name: "all VM-level, maxConcurrent=4, 10 VMs — 3 chunks (4, 4, 2)",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: makeVMGroups("default", 10)},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{3},
			wantErrorCount:  0,
			wantChunkSizes:  [][]int{{4, 4, 2}},
		},
		{
			name: "single namespace group of 3, maxConcurrent=4 — 1 chunk with 3 VMs",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						makeNSGroup("erp-db", 3),
					}},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{1},
			wantErrorCount:  0,
			wantChunkSizes:  [][]int{{3}},
		},
		{
			name: "namespace group (3) + VM-level VMs (5), maxConcurrent=4",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: append(
						[]soteriav1alpha1.VolumeGroupInfo{makeNSGroup("erp-db", 3)},
						makeVMGroups("default", 5)...,
					)},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{2},
			wantErrorCount:  0,
			wantChunkSizes:  [][]int{{4, 4}},
		},
		{
			name: "namespace group (3) cannot fit into remaining (1 slot), maxConcurrent=4 — new chunk",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: append(
						[]soteriav1alpha1.VolumeGroupInfo{
							makeNSGroup("erp-db", 3),
							makeNSGroup("erp-app", 3),
						},
						makeVMGroups("default", 0)...,
					)},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{2},
			wantErrorCount:  0,
			wantChunkSizes:  [][]int{{3, 3}},
		},
		{
			name: "namespace group (5) exceeds maxConcurrent (4) — ChunkError",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						makeNSGroup("huge-ns", 5),
					}},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{0},
			wantErrorCount:  1,
		},
		{
			name: "multiple waves — each wave chunked independently",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: makeVMGroups("wave-one", 5)},
					{WaveKey: "2", VolumeGroups: makeVMGroups("wave-two", 3)},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{2, 1},
			wantErrorCount:  0,
			wantChunkSizes:  [][]int{{4, 1}, {3}},
		},
		{
			name: "namespace group exactly equals maxConcurrent — fits in one chunk",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						makeNSGroup("exact", 4),
					}},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{1},
			wantErrorCount:  0,
			wantChunkSizes:  [][]int{{4}},
		},
		{
			name: "empty wave — no chunks",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: nil},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{0},
			wantErrorCount:  0,
		},
		{
			name: "two namespace groups (3 each), maxConcurrent=4 — 2 chunks",
			input: ChunkInput{
				WaveGroups: []WaveGroupWithVolumes{
					{WaveKey: "1", VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						makeNSGroup("ns-a", 3),
						makeNSGroup("ns-b", 3),
					}},
				},
			},
			maxConcurrent:   4,
			wantChunkCounts: []int{2},
			wantErrorCount:  0,
			wantChunkSizes:  [][]int{{3, 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ChunkWaves(tt.input, tt.maxConcurrent)

			if len(result.Errors) != tt.wantErrorCount {
				t.Errorf("Errors count = %d, want %d: %+v", len(result.Errors), tt.wantErrorCount, result.Errors)
			}

			if len(result.Waves) != len(tt.wantChunkCounts) {
				t.Fatalf("Waves count = %d, want %d", len(result.Waves), len(tt.wantChunkCounts))
			}

			for wi, wc := range result.Waves {
				if len(wc.Chunks) != tt.wantChunkCounts[wi] {
					t.Errorf("Wave[%d] chunk count = %d, want %d", wi, len(wc.Chunks), tt.wantChunkCounts[wi])
				}

				if tt.wantChunkSizes != nil && wi < len(tt.wantChunkSizes) {
					for ci, chunk := range wc.Chunks {
						if ci < len(tt.wantChunkSizes[wi]) {
							wantSize := tt.wantChunkSizes[wi][ci]
							if wantSize > 0 && len(chunk.VMs) != wantSize {
								t.Errorf("Wave[%d].Chunk[%d] VM count = %d, want %d",
									wi, ci, len(chunk.VMs), wantSize)
							}
						}
					}
				}
			}
		})
	}
}

func TestChunkWaves_NamingConvention(t *testing.T) {
	input := ChunkInput{
		WaveGroups: []WaveGroupWithVolumes{
			{WaveKey: "alpha", VolumeGroups: makeVMGroups("default", 7)},
		},
	}
	result := ChunkWaves(input, 3)

	if len(result.Waves) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(result.Waves))
	}

	expectedNames := []string{
		"wave-alpha-group-0",
		"wave-alpha-group-1",
		"wave-alpha-group-2",
	}

	for i, chunk := range result.Waves[0].Chunks {
		if i >= len(expectedNames) {
			break
		}
		if chunk.Name != expectedNames[i] {
			t.Errorf("Chunk[%d].Name = %q, want %q", i, chunk.Name, expectedNames[i])
		}
	}

	if totalVMs(result.Waves[0].Chunks) != 7 {
		t.Errorf("total VMs across chunks = %d, want 7", totalVMs(result.Waves[0].Chunks))
	}
}
