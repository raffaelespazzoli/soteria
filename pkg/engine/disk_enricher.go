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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// DiskEnricher resolves per-disk PVC topology for a VM.
// KubeVirtDiskEnricher reads the VM spec and resolves PVCs;
// NoOpDiskEnricher returns nil for environments without real KubeVirt VMs.
type DiskEnricher interface {
	EnrichDisks(ctx context.Context, vmName, namespace string) ([]soteriav1alpha1.DiscoveredDisk, error)
}

// KubeVirtDiskEnricher resolves per-disk PVC topology by reading a KubeVirt
// VirtualMachine's disks and volumes, joining them by name, then fetching
// each backing PVC to extract the storage class.
type KubeVirtDiskEnricher struct {
	Reader client.Reader
}

func (e *KubeVirtDiskEnricher) EnrichDisks(
	ctx context.Context, vmName, namespace string,
) ([]soteriav1alpha1.DiscoveredDisk, error) {
	var vm kubevirtv1.VirtualMachine
	if err := e.Reader.Get(ctx, types.NamespacedName{Name: vmName, Namespace: namespace}, &vm); err != nil {
		return nil, fmt.Errorf("fetching VM %s/%s: %w", namespace, vmName, err)
	}

	if vm.Spec.Template == nil {
		return nil, nil
	}

	volMap := make(map[string]kubevirtv1.Volume, len(vm.Spec.Template.Spec.Volumes))
	for _, vol := range vm.Spec.Template.Spec.Volumes {
		volMap[vol.Name] = vol
	}

	var disks []soteriav1alpha1.DiscoveredDisk
	for _, disk := range vm.Spec.Template.Spec.Domain.Devices.Disks {
		vol, ok := volMap[disk.Name]
		if !ok {
			continue
		}
		var pvcName string
		if vol.PersistentVolumeClaim != nil {
			pvcName = vol.PersistentVolumeClaim.ClaimName
		} else if vol.DataVolume != nil {
			pvcName = vol.DataVolume.Name
		} else {
			continue
		}

		var pvc corev1.PersistentVolumeClaim
		err := e.Reader.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: namespace}, &pvc)
		if apierrors.IsNotFound(err) {
			// PVC doesn't exist yet (e.g. passive/secondary site before failover).
			// Try to resolve the storage class from a pre-provisioned PV whose
			// ClaimRef targets this PVC (created by the ShadowPV consumer).
			sc := e.storageClassFromPV(ctx, pvcName, namespace)
			disks = append(disks, soteriav1alpha1.DiscoveredDisk{
				Name:         disk.Name,
				PVCName:      pvcName,
				StorageClass: sc,
			})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("fetching PVC %s/%s: %w", namespace, pvcName, err)
		}

		sc := ""
		if pvc.Spec.StorageClassName != nil {
			sc = *pvc.Spec.StorageClassName
		}
		disks = append(disks, soteriav1alpha1.DiscoveredDisk{
			Name:         disk.Name,
			PVCName:      pvcName,
			StorageClass: sc,
		})
	}
	return disks, nil
}

// storageClassFromPV searches for a PV whose ClaimRef matches the given
// PVC name/namespace and returns its StorageClassName. This covers the
// passive-site case where ShadowPV consumer has pre-created PVs but
// PVCs don't exist yet.
func (e *KubeVirtDiskEnricher) storageClassFromPV(
	ctx context.Context, pvcName, namespace string,
) string {
	var pvList corev1.PersistentVolumeList
	if err := e.Reader.List(ctx, &pvList); err != nil {
		return ""
	}
	for i := range pvList.Items {
		ref := pvList.Items[i].Spec.ClaimRef
		if ref != nil && ref.Name == pvcName && ref.Namespace == namespace {
			return pvList.Items[i].Spec.StorageClassName
		}
	}
	return ""
}

// NoOpDiskEnricher returns nil disks. Used with noop/fake drivers
// in dev/CI where no real KubeVirt VMs exist.
type NoOpDiskEnricher struct{}

func (NoOpDiskEnricher) EnrichDisks(context.Context, string, string) ([]soteriav1alpha1.DiscoveredDisk, error) {
	return nil, nil
}
