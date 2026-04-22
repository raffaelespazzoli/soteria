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

// checks.go assembles a preflight composition report from discovery,
// consistency, chunking, and storage backend data. The DRPlan reconciler calls
// ComposeReport on every reconcile to populate .status.preflight. The report
// gives platform engineers full visibility into plan structure — wave
// membership, VM details, storage backends, and DRGroup chunking preview —
// before any execution is triggered.

package preflight

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

const (
	backendUnknown = "unknown"
	backendNone    = "none"
)

// CompositionInput aggregates outputs from earlier pipeline stages into a single
// struct for preflight report assembly.
type CompositionInput struct {
	Plan              *soteriav1alpha1.DRPlan
	DiscoveryResult   *engine.DiscoveryResult
	ConsistencyResult *engine.ConsistencyResult
	ChunkResult       *engine.ChunkResult
	StorageBackends   map[string]string
}

// ComposeReport builds a PreflightReport from the combined pipeline outputs.
// The report is informational — it does not gate execution. Non-critical issues
// are collected as warnings rather than errors.
func ComposeReport(input CompositionInput, now metav1.Time) *soteriav1alpha1.PreflightReport {
	report := &soteriav1alpha1.PreflightReport{
		GeneratedAt: ptr.To(now),
	}

	if input.Plan != nil {
		report.PrimarySite = input.Plan.Spec.PrimarySite
		report.SecondarySite = input.Plan.Spec.SecondarySite
		report.ActiveSite = input.Plan.Status.ActiveSite
	}

	if input.DiscoveryResult != nil {
		report.TotalVMs = input.DiscoveryResult.TotalVMs

		vmGroupIndex := buildVMGroupIndex(input.ConsistencyResult)
		chunkIndex := buildChunkIndex(input.ChunkResult)

		for _, wg := range input.DiscoveryResult.Waves {
			pw := soteriav1alpha1.PreflightWave{
				WaveKey: wg.WaveKey,
				VMCount: len(wg.VMs),
			}

			for _, vm := range wg.VMs {
				key := vm.Namespace + "/" + vm.Name
				backend := input.StorageBackends[key]
				if backend == "" {
					backend = backendUnknown
				}

				gi := vmGroupIndex[key]
				pw.VMs = append(pw.VMs, soteriav1alpha1.PreflightVM{
					Name:             vm.Name,
					Namespace:        vm.Namespace,
					StorageBackend:   backend,
					ConsistencyLevel: gi.consistencyLevel,
					VolumeGroupName:  gi.groupName,
				})
			}

			if chunks, ok := chunkIndex[wg.WaveKey]; ok {
				for _, chunk := range chunks {
					pc := soteriav1alpha1.PreflightChunk{
						Name:    chunk.Name,
						VMCount: len(chunk.VMs),
					}
					for _, vm := range chunk.VMs {
						pc.VMNames = append(pc.VMNames, vm.Name)
					}
					for _, vg := range chunk.VolumeGroups {
						pc.VolumeGroups = append(pc.VolumeGroups, vg.Name)
					}
					pw.Chunks = append(pw.Chunks, pc)
				}
			}

			report.Waves = append(report.Waves, pw)
		}
	}

	report.Warnings = collectWarnings(input)

	return report
}

type vmGroupInfo struct {
	groupName        string
	consistencyLevel string
}

func buildVMGroupIndex(cr *engine.ConsistencyResult) map[string]vmGroupInfo {
	index := make(map[string]vmGroupInfo)
	if cr == nil {
		return index
	}
	for _, vg := range cr.VolumeGroups {
		for _, vmName := range vg.VMNames {
			key := vg.Namespace + "/" + vmName
			index[key] = vmGroupInfo{
				groupName:        vg.Name,
				consistencyLevel: string(vg.ConsistencyLevel),
			}
		}
	}
	return index
}

func buildChunkIndex(cr *engine.ChunkResult) map[string][]engine.DRGroupChunk {
	index := make(map[string][]engine.DRGroupChunk)
	if cr == nil {
		return index
	}
	for _, wc := range cr.Waves {
		index[wc.WaveKey] = wc.Chunks
	}
	return index
}

func collectWarnings(input CompositionInput) []string {
	var warnings []string

	for key, backend := range input.StorageBackends {
		switch backend {
		case backendUnknown:
			warnings = append(warnings, fmt.Sprintf(
				"VM %s: could not determine storage backend from PVC storage class", key))
		case backendNone:
			warnings = append(warnings, fmt.Sprintf(
				"VM %s: no PVC volumes found", key))
		}
	}

	if input.ChunkResult != nil {
		for _, ce := range input.ChunkResult.Errors {
			warnings = append(warnings, fmt.Sprintf(
				"Wave %s: namespace group %s (%d VMs) exceeds maxConcurrentFailovers (%d)",
				ce.WaveKey, ce.Namespace, ce.GroupSize, ce.MaxConcurrent))
		}
	}

	if input.ConsistencyResult != nil {
		for _, wc := range input.ConsistencyResult.WaveConflicts {
			warnings = append(warnings, fmt.Sprintf(
				"Wave conflict in namespace %s: VMs have conflicting wave labels", wc.Namespace))
		}
	}

	return warnings
}
