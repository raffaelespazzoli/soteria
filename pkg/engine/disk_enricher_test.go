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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func diskEnricherTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	_ = kubevirtv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

func strPtr(s string) *string { return &s }

func TestKubeVirtDiskEnricher_PVCBackedDisks(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-db01", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{Name: "root-disk"},
								{Name: "data-disk"},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "root-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "pvc-root",
									},
								},
							},
						},
						{
							Name: "data-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "pvc-data",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	pvcRoot := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-root", Namespace: "ns1"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("ocs-storagecluster-ceph-rbd")},
	}
	pvcData := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-data", Namespace: "ns1"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("ocs-storagecluster-ceph-rbd")},
	}

	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).
		WithObjects(vm, pvcRoot, pvcData).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	disks, err := enricher.EnrichDisks(context.Background(), "vm-db01", "ns1")
	if err != nil {
		t.Fatalf("EnrichDisks() error: %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("Expected 2 disks, got %d", len(disks))
	}
	assertDisk(t, disks[0], "root-disk", "pvc-root", "ocs-storagecluster-ceph-rbd")
	assertDisk(t, disks[1], "data-disk", "pvc-data", "ocs-storagecluster-ceph-rbd")
}

func TestKubeVirtDiskEnricher_DataVolumeBackedDisk(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-dv01", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{Name: "dv-disk"},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "dv-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								DataVolume: &kubevirtv1.DataVolumeSource{
									Name: "dv-data",
								},
							},
						},
					},
				},
			},
		},
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "dv-data", Namespace: "ns1"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("dell-powerstore")},
	}

	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).
		WithObjects(vm, pvc).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	disks, err := enricher.EnrichDisks(context.Background(), "vm-dv01", "ns1")
	if err != nil {
		t.Fatalf("EnrichDisks() error: %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("Expected 1 disk, got %d", len(disks))
	}
	assertDisk(t, disks[0], "dv-disk", "dv-data", "dell-powerstore")
}

func TestKubeVirtDiskEnricher_MixedVolumes(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-mixed", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{Name: "pvc-disk"},
								{Name: "dv-disk"},
								{Name: "container-disk"},
								{Name: "cloudinit-disk"},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "pvc-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "pvc-root",
									},
								},
							},
						},
						{
							Name: "dv-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								DataVolume: &kubevirtv1.DataVolumeSource{
									Name: "dv-data",
								},
							},
						},
						{
							Name: "container-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								ContainerDisk: &kubevirtv1.ContainerDiskSource{
									Image: "registry.io/os:latest",
								},
							},
						},
						{
							Name: "cloudinit-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
									UserData: "#cloud-config",
								},
							},
						},
					},
				},
			},
		},
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-root", Namespace: "ns1"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("sc-a")},
	}
	dvPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "dv-data", Namespace: "ns1"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("sc-b")},
	}

	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).
		WithObjects(vm, pvc, dvPVC).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	disks, err := enricher.EnrichDisks(context.Background(), "vm-mixed", "ns1")
	if err != nil {
		t.Fatalf("EnrichDisks() error: %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("Expected 2 disks (PVC+DV only), got %d", len(disks))
	}
	assertDisk(t, disks[0], "pvc-disk", "pvc-root", "sc-a")
	assertDisk(t, disks[1], "dv-disk", "dv-data", "sc-b")
}

func TestKubeVirtDiskEnricher_NoPVCVolumes(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-stateless", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{Name: "container-disk"},
								{Name: "cloudinit-disk"},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "container-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								ContainerDisk: &kubevirtv1.ContainerDiskSource{
									Image: "registry.io/os:latest",
								},
							},
						},
						{
							Name: "cloudinit-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
									UserData: "#cloud-config",
								},
							},
						},
					},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).
		WithObjects(vm).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	disks, err := enricher.EnrichDisks(context.Background(), "vm-stateless", "ns1")
	if err != nil {
		t.Fatalf("EnrichDisks() error: %v", err)
	}
	if len(disks) != 0 {
		t.Errorf("Expected 0 disks for stateless VM, got %d", len(disks))
	}
}

func TestKubeVirtDiskEnricher_MissingPVC_SelfHealing(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-pending", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{Name: "dv-disk"},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "dv-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								DataVolume: &kubevirtv1.DataVolumeSource{
									Name: "dv-provisioning",
								},
							},
						},
					},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).
		WithObjects(vm).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	// Phase 1: PVC does not exist yet — disk recorded with empty pvcName/storageClass.
	disks, err := enricher.EnrichDisks(context.Background(), "vm-pending", "ns1")
	if err != nil {
		t.Fatalf("Phase 1 EnrichDisks() error: %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("Phase 1: expected 1 disk entry for missing PVC, got %d", len(disks))
	}
	if disks[0].Name != "dv-disk" {
		t.Errorf("Phase 1: disk name = %q, want dv-disk", disks[0].Name)
	}
	if disks[0].PVCName != "" {
		t.Errorf("Phase 1: PVCName should be empty for missing PVC, got %q", disks[0].PVCName)
	}
	if disks[0].StorageClass != "" {
		t.Errorf("Phase 1: StorageClass should be empty for missing PVC, got %q", disks[0].StorageClass)
	}

	// Phase 2: PVC appears (DataVolume finished provisioning) — next enrichment populates fields.
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "dv-provisioning", Namespace: "ns1"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("ocs-storagecluster-ceph-rbd")},
	}
	if err := cl.Create(context.Background(), pvc); err != nil {
		t.Fatalf("Failed to create PVC for phase 2: %v", err)
	}

	disks2, err := enricher.EnrichDisks(context.Background(), "vm-pending", "ns1")
	if err != nil {
		t.Fatalf("Phase 2 EnrichDisks() error: %v", err)
	}
	if len(disks2) != 1 {
		t.Fatalf("Phase 2: expected 1 disk, got %d", len(disks2))
	}
	assertDisk(t, disks2[0], "dv-disk", "dv-provisioning", "ocs-storagecluster-ceph-rbd")
}

func TestKubeVirtDiskEnricher_NilStorageClassName(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-nosc", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Domain: kubevirtv1.DomainSpec{
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{Name: "root-disk"},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "root-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "pvc-no-sc",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-no-sc", Namespace: "ns1"},
		Spec:       corev1.PersistentVolumeClaimSpec{},
	}

	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).
		WithObjects(vm, pvc).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	disks, err := enricher.EnrichDisks(context.Background(), "vm-nosc", "ns1")
	if err != nil {
		t.Fatalf("EnrichDisks() error: %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("Expected 1 disk, got %d", len(disks))
	}
	assertDisk(t, disks[0], "root-disk", "pvc-no-sc", "")
}

func TestKubeVirtDiskEnricher_VMNotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	_, err := enricher.EnrichDisks(context.Background(), "nonexistent", "ns1")
	if err == nil {
		t.Fatal("Expected error for nonexistent VM")
	}
}

func TestKubeVirtDiskEnricher_NilTemplate(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-notemplate", Namespace: "ns1"},
		Spec:       kubevirtv1.VirtualMachineSpec{},
	}

	cl := fake.NewClientBuilder().WithScheme(diskEnricherTestScheme()).
		WithObjects(vm).Build()
	enricher := &KubeVirtDiskEnricher{Reader: cl}

	disks, err := enricher.EnrichDisks(context.Background(), "vm-notemplate", "ns1")
	if err != nil {
		t.Fatalf("EnrichDisks() error: %v", err)
	}
	if disks != nil {
		t.Errorf("Expected nil disks for nil template, got %v", disks)
	}
}

func TestNoOpDiskEnricher_ReturnsNil(t *testing.T) {
	enricher := NoOpDiskEnricher{}
	disks, err := enricher.EnrichDisks(context.Background(), "any-vm", "any-ns")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if disks != nil {
		t.Errorf("Expected nil, got: %v", disks)
	}
}

// Compile-time interface checks.
var (
	_ DiskEnricher = (*KubeVirtDiskEnricher)(nil)
	_ DiskEnricher = NoOpDiskEnricher{}
)

func assertDisk(t *testing.T, d soteriav1alpha1.DiscoveredDisk, name, pvcName, storageClass string) {
	t.Helper()
	if d.Name != name {
		t.Errorf("Disk.Name = %q, want %q", d.Name, name)
	}
	if d.PVCName != pvcName {
		t.Errorf("Disk.PVCName = %q, want %q", d.PVCName, pvcName)
	}
	if d.StorageClass != storageClass {
		t.Errorf("Disk.StorageClass = %q, want %q", d.StorageClass, storageClass)
	}
}
