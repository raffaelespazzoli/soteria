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
// by controller-runtime's cached client. Discovery uses the soteria.io/drplan label
// to find VMs belonging to a specific plan by name, replacing the previous arbitrary
// label-selector approach.

package engine

import (
	"context"
	"maps"
	"sort"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
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

// VMDiscoverer abstracts the Kubernetes API call that lists VMs belonging to a
// named DRPlan (via the soteria.io/drplan label). Implementations:
// TypedVMDiscoverer (production, uses controller-runtime client) and test mocks.
type VMDiscoverer interface {
	DiscoverVMs(ctx context.Context, planName string) ([]VMReference, error)
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

func (d *TypedVMDiscoverer) DiscoverVMs(ctx context.Context, planName string) ([]VMReference, error) {
	sel := labels.SelectorFromSet(labels.Set{soteriav1alpha1.DRPlanLabel: planName})

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

// UnprotectedVMDiscoverer lists VMs cluster-wide that are not covered by any
// DRPlan (i.e. lack the soteria.io/drplan label key entirely). Kept separate
// from VMDiscoverer so mock injection is independent.
type UnprotectedVMDiscoverer interface {
	ListUnprotectedVMs(ctx context.Context) ([]VMReference, error)
}

// ListUnprotectedVMs returns all kubevirt VMs not covered by any DRPlan.
// A VM is unprotected when the soteria.io/drplan label key is absent OR
// present with an empty value (the admission webhook and enqueueForVM both
// treat empty values as "no plan"). Two selector passes are needed because
// Kubernetes ANDs requirements within a single selector.
func (d *TypedVMDiscoverer) ListUnprotectedVMs(ctx context.Context) ([]VMReference, error) {
	absentReq, err := labels.NewRequirement(soteriav1alpha1.DRPlanLabel, selection.DoesNotExist, nil)
	if err != nil {
		return nil, err
	}
	emptyReq, err := labels.NewRequirement(soteriav1alpha1.DRPlanLabel, selection.In, []string{""})
	if err != nil {
		return nil, err
	}

	var refs []VMReference
	for _, sel := range []labels.Selector{
		labels.NewSelector().Add(*absentReq),
		labels.NewSelector().Add(*emptyReq),
	} {
		var vmList kubevirtv1.VirtualMachineList
		if err := d.Reader.List(ctx, &vmList, &client.ListOptions{
			LabelSelector: sel,
		}); err != nil {
			return nil, err
		}
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
	}

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Namespace != refs[j].Namespace {
			return refs[i].Namespace < refs[j].Namespace
		}
		return refs[i].Name < refs[j].Name
	})

	return refs, nil
}

// Compile-time interface checks.
var _ VMDiscoverer = (*TypedVMDiscoverer)(nil)
var _ UnprotectedVMDiscoverer = (*TypedVMDiscoverer)(nil)

// NewTypedVMDiscoverer returns a VMDiscoverer backed by a controller-runtime Reader.
func NewTypedVMDiscoverer(reader client.Reader) *TypedVMDiscoverer {
	return &TypedVMDiscoverer{Reader: reader}
}
