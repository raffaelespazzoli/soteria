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

// discovery.go implements VM discovery and wave-grouping for DRPlan reconciliation.
//
// Architecture: The DRPlan controller delegates VM lookup to a VMDiscoverer, then
// passes the results through GroupByWave — a pure function that partitions VMs by
// their wave-label value. This separation keeps the reconciler testable: unit tests
// inject a mock VMDiscoverer while the production path uses TypedVMDiscoverer backed
// by controller-runtime's cached client.

package engine

import (
	"context"
	"maps"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMReference is a lightweight projection of kubevirt VM metadata. It carries
// only the fields needed by downstream pipeline stages (wave grouping, status
// mapping) without pulling in the full VirtualMachine spec.
type VMReference struct {
	Name      string
	Namespace string
	Labels    map[string]string
}

// WaveGroup holds the VMs that share a single wave-label value.
type WaveGroup struct {
	WaveKey string
	VMs     []VMReference
}

// DiscoveryResult is the output of a discovery + wave-grouping pass.
type DiscoveryResult struct {
	Waves    []WaveGroup
	TotalVMs int
}

// VMDiscoverer abstracts the Kubernetes API call that lists VMs matching a
// label selector. Implementations: TypedVMDiscoverer (production, uses
// controller-runtime client) and test mocks.
type VMDiscoverer interface {
	DiscoverVMs(ctx context.Context, selector metav1.LabelSelector) ([]VMReference, error)
}

// GroupByWave partitions VMs into waves keyed by the value of waveLabel.
// VMs that lack the wave label are placed in a wave with an empty-string key
// so they are never silently dropped. Waves are sorted lexicographically by
// key for deterministic status output.
func GroupByWave(vms []VMReference, waveLabel string) DiscoveryResult {
	if len(vms) == 0 {
		return DiscoveryResult{}
	}

	groups := make(map[string][]VMReference)
	for _, vm := range vms {
		key := vm.Labels[waveLabel]
		groups[key] = append(groups[key], vm)
	}

	waves := make([]WaveGroup, 0, len(groups))
	for k, v := range groups {
		waves = append(waves, WaveGroup{WaveKey: k, VMs: v})
	}
	sort.Slice(waves, func(i, j int) bool {
		return waves[i].WaveKey < waves[j].WaveKey
	})

	return DiscoveryResult{
		Waves:    waves,
		TotalVMs: len(vms),
	}
}

// TypedVMDiscoverer lists kubevirt VirtualMachine resources via a
// controller-runtime cached client and projects them into VMReferences.
type TypedVMDiscoverer struct {
	Reader client.Reader
}

func (d *TypedVMDiscoverer) DiscoverVMs(ctx context.Context, selector metav1.LabelSelector) ([]VMReference, error) {
	sel, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return nil, err
	}

	var vmList kubevirtv1.VirtualMachineList
	if err := d.Reader.List(ctx, &vmList, &client.ListOptions{
		LabelSelector: sel,
	}); err != nil {
		return nil, err
	}

	refs := make([]VMReference, 0, len(vmList.Items))
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		ref := VMReference{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		}
		if len(vm.Labels) > 0 {
			ref.Labels = make(map[string]string, len(vm.Labels))
			maps.Copy(ref.Labels, vm.Labels)
		}
		refs = append(refs, ref)
	}

	return refs, nil
}

// Compile-time interface check.
var _ VMDiscoverer = (*TypedVMDiscoverer)(nil)

// NewTypedVMDiscoverer returns a VMDiscoverer backed by a controller-runtime Reader.
func NewTypedVMDiscoverer(reader client.Reader) *TypedVMDiscoverer {
	return &TypedVMDiscoverer{Reader: reader}
}
