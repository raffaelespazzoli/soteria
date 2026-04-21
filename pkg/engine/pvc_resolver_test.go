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
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKubeVirtPVCResolver_ResolvePVCNames(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-db01", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
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
						{
							Name: "container-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								ContainerDisk: &kubevirtv1.ContainerDiskSource{
									Image: "registry.io/os:latest",
								},
							},
						},
					},
				},
			},
		},
	}

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vm).Build()
	resolver := &KubeVirtPVCResolver{Client: cl}

	names, err := resolver.ResolvePVCNames(context.Background(), "vm-db01", "ns1")
	if err != nil {
		t.Fatalf("ResolvePVCNames failed: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("Expected 2 PVC names, got %d: %v", len(names), names)
	}
	if names[0] != "pvc-root" || names[1] != "pvc-data" {
		t.Errorf("Expected [pvc-root, pvc-data], got %v", names)
	}
}

func TestKubeVirtPVCResolver_VMNotFound(t *testing.T) {
	scheme := newTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	resolver := &KubeVirtPVCResolver{Client: cl}

	_, err := resolver.ResolvePVCNames(context.Background(), "nonexistent", "ns1")
	if err == nil {
		t.Fatal("Expected error for nonexistent VM")
	}
}

func TestKubeVirtPVCResolver_NoPVCs(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-thin", Namespace: "ns1"},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Volumes: []kubevirtv1.Volume{
						{
							Name: "container-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								ContainerDisk: &kubevirtv1.ContainerDiskSource{
									Image: "registry.io/os:latest",
								},
							},
						},
					},
				},
			},
		},
	}

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vm).Build()
	resolver := &KubeVirtPVCResolver{Client: cl}

	names, err := resolver.ResolvePVCNames(context.Background(), "vm-thin", "ns1")
	if err != nil {
		t.Fatalf("ResolvePVCNames failed: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("Expected empty PVC names, got %v", names)
	}
}

func TestNoOpPVCResolver_ReturnsEmpty(t *testing.T) {
	resolver := NoOpPVCResolver{}
	names, err := resolver.ResolvePVCNames(context.Background(), "any-vm", "any-ns")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if names != nil {
		t.Errorf("Expected nil slice, got: %v", names)
	}
}
