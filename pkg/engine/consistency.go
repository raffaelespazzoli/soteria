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

// consistency.go resolves volume-group membership from namespace annotations and
// detects wave-placement conflicts for namespace-level consistency.
//
// Architecture: The DRPlan controller calls ResolveVolumeGroups after VM
// discovery and wave grouping. The function queries each namespace's consistency
// level via a NamespaceLookup (abstracting the Kubernetes API), groups VMs into
// VolumeGroups accordingly, and checks for wave conflicts. Namespace-level
// consistency means all VMs in a namespace form one VolumeGroup — those VMs must
// share a single wave, because the VolumeGroup is an indivisible unit during
// failover. If VMs in a namespace-level namespace carry different wave labels,
// a WaveConflict is reported so the reconciler can set Ready=False.

package engine

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// WaveConflict records VMs in a namespace-level namespace that have mismatched
// wave labels, which is invalid because the entire namespace must move as one
// indivisible VolumeGroup in a single wave.
type WaveConflict struct {
	Namespace string
	VMNames   []string
	WaveKeys  []string
}

// ConsistencyResult is the output of ResolveVolumeGroups.
type ConsistencyResult struct {
	VolumeGroups  []soteriav1alpha1.VolumeGroupInfo
	WaveConflicts []WaveConflict
}

// NamespaceLookup abstracts namespace annotation reads so the consistency
// engine can be tested with mocks instead of a real Kubernetes client.
type NamespaceLookup interface {
	GetConsistencyLevel(ctx context.Context, namespace string) (soteriav1alpha1.ConsistencyLevel, error)
}

// DefaultNamespaceLookup reads namespace annotations via the Kubernetes API.
//
// TODO(perf): This uses a direct typed client (uncached). For high-frequency
// reconciles, consider switching to the manager's cached client or adding a
// short-lived per-reconcile cache to avoid repeated API calls for the same
// namespace within a single Reconcile invocation.
type DefaultNamespaceLookup struct {
	Client corev1client.NamespacesGetter
}

func (d *DefaultNamespaceLookup) GetConsistencyLevel(
	ctx context.Context, namespace string,
) (soteriav1alpha1.ConsistencyLevel, error) {
	ns, err := d.Client.Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return soteriav1alpha1.ConsistencyLevelVM, err
	}
	if ns.Annotations[soteriav1alpha1.ConsistencyAnnotation] == string(soteriav1alpha1.ConsistencyLevelNamespace) {
		return soteriav1alpha1.ConsistencyLevelNamespace, nil
	}
	return soteriav1alpha1.ConsistencyLevelVM, nil
}

// Compile-time interface check.
var _ NamespaceLookup = (*DefaultNamespaceLookup)(nil)

// ResolveVolumeGroups partitions VMs into VolumeGroups based on namespace
// consistency annotations and detects wave conflicts.
//
// For namespace-level namespaces, all VMs form a single VolumeGroup named
// "ns-<namespace>". For VM-level namespaces (the default), each VM forms its
// own VolumeGroup named "vm-<namespace>-<name>".
//
// Wave conflict detection: namespace-level consistency requires crash-consistent
// snapshots across all VMs in the namespace, which is only possible when they
// all belong to the same wave. VMs in different waves would be failed over at
// different times, breaking atomicity.
func ResolveVolumeGroups(
	ctx context.Context,
	vms []VMReference,
	waveLabel string,
	nsLookup NamespaceLookup,
) (*ConsistencyResult, error) {
	if len(vms) == 0 {
		return &ConsistencyResult{}, nil
	}

	type nsGroup struct {
		vms []VMReference
	}
	byNS := make(map[string]*nsGroup)
	for _, vm := range vms {
		g, ok := byNS[vm.Namespace]
		if !ok {
			g = &nsGroup{}
			byNS[vm.Namespace] = g
		}
		g.vms = append(g.vms, vm)
	}

	nsLevels := make(map[string]soteriav1alpha1.ConsistencyLevel, len(byNS))
	for ns := range byNS {
		level, err := nsLookup.GetConsistencyLevel(ctx, ns)
		if err != nil {
			return nil, fmt.Errorf("looking up consistency level for namespace %q: %w", ns, err)
		}
		nsLevels[ns] = level
	}

	var volumeGroups []soteriav1alpha1.VolumeGroupInfo
	var conflicts []WaveConflict

	namespaces := make([]string, 0, len(byNS))
	for ns := range byNS {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	for _, ns := range namespaces {
		g := byNS[ns]
		level := nsLevels[ns]

		if level == soteriav1alpha1.ConsistencyLevelNamespace {
			// Namespace-level: all VMs share one VolumeGroup so their disks can be
			// snapshotted atomically — required for crash-consistent recovery of
			// tightly coupled services (e.g. DB primary + replicas in one namespace).
			vmNames := make([]string, len(g.vms))
			for i, vm := range g.vms {
				vmNames[i] = vm.Name
			}
			sort.Strings(vmNames)

			volumeGroups = append(volumeGroups, soteriav1alpha1.VolumeGroupInfo{
				Name:             fmt.Sprintf("ns-%s", ns),
				Namespace:        ns,
				ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace,
				VMNames:          vmNames,
			})

			// Namespace-level VMs must move together during failover, which
			// requires them to be in the same wave. Different waves execute at
			// different times, breaking the atomicity guarantee.
			waveSet := make(map[string]bool)
			for _, vm := range g.vms {
				waveSet[vm.Labels[waveLabel]] = true
			}
			if len(waveSet) > 1 {
				waveKeys := make([]string, 0, len(waveSet))
				for k := range waveSet {
					waveKeys = append(waveKeys, k)
				}
				sort.Strings(waveKeys)
				conflicts = append(conflicts, WaveConflict{
					Namespace: ns,
					VMNames:   vmNames,
					WaveKeys:  waveKeys,
				})
			}
		} else {
			sort.Slice(g.vms, func(i, j int) bool {
				return g.vms[i].Name < g.vms[j].Name
			})
			for _, vm := range g.vms {
				volumeGroups = append(volumeGroups, soteriav1alpha1.VolumeGroupInfo{
					Name:             fmt.Sprintf("vm-%s-%s", ns, vm.Name),
					Namespace:        ns,
					ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM,
					VMNames:          []string{vm.Name},
				})
			}
		}
	}

	return &ConsistencyResult{
		VolumeGroups:  volumeGroups,
		WaveConflicts: conflicts,
	}, nil
}
