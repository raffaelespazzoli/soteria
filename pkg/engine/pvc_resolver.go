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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PVCResolver abstracts PVC claim name resolution from VM specs.
// KubeVirtPVCResolver reads VM volumes; NoOpPVCResolver returns empty
// for environments without real KubeVirt VMs (dev/CI).
type PVCResolver interface {
	ResolvePVCNames(ctx context.Context, vmName, namespace string) ([]string, error)
}

// KubeVirtPVCResolver resolves PVC names by reading a KubeVirt VirtualMachine's
// Spec.Template.Spec.Volumes and extracting PersistentVolumeClaim.ClaimName
// references. Non-PVC volumes (containerDisk, cloudInitNoCloud, etc.) are
// silently ignored.
type KubeVirtPVCResolver struct {
	Client client.Client
}

func (r *KubeVirtPVCResolver) ResolvePVCNames(ctx context.Context, vmName, namespace string) ([]string, error) {
	var vm kubevirtv1.VirtualMachine
	if err := r.Client.Get(ctx, types.NamespacedName{Name: vmName, Namespace: namespace}, &vm); err != nil {
		return nil, fmt.Errorf("fetching VM %s/%s: %w", namespace, vmName, err)
	}

	if vm.Spec.Template == nil {
		return nil, nil
	}

	var pvcNames []string
	for _, vol := range vm.Spec.Template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			pvcNames = append(pvcNames, vol.PersistentVolumeClaim.ClaimName)
		}
	}
	return pvcNames, nil
}

// NoOpPVCResolver returns empty PVC names. Used with noop/fake drivers
// in dev/CI where no real KubeVirt VMs exist.
type NoOpPVCResolver struct{}

func (NoOpPVCResolver) ResolvePVCNames(context.Context, string, string) ([]string, error) {
	return nil, nil
}
