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

package preflight

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubevirtv1 "kubevirt.io/api/core/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/soteria-project/soteria/pkg/engine"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = kubevirtv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

func strPtr(s string) *string { return &s }

func TestResolveBackends(t *testing.T) {
	tests := []struct {
		name         string
		vms          []engine.VMReference
		vmObjects    []crclient.Object
		pvcs         []runtime.Object
		driverMap    StorageClassDriverMap
		wantBackends map[string]string
		wantWarnings int
	}{
		{
			name: "VM with PVC using known storage class",
			vms:  []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
			vmObjects: []crclient.Object{
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{{
									Name: "rootdisk",
									VolumeSource: kubevirtv1.VolumeSource{
										PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
											PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
												ClaimName: "vm-1-rootdisk",
											},
										},
									},
								}},
							},
						},
					},
				},
			},
			pvcs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1-rootdisk", Namespace: "ns1"},
					Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("ocs-rbd")},
				},
			},
			driverMap:    StorageClassDriverMap{"ocs-rbd": "odf"},
			wantBackends: map[string]string{"ns1/vm-1": "odf"},
			wantWarnings: 0,
		},
		{
			name: "VM with PVC using unknown storage class",
			vms:  []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
			vmObjects: []crclient.Object{
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{{
									Name: "rootdisk",
									VolumeSource: kubevirtv1.VolumeSource{
										PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
											PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
												ClaimName: "vm-1-rootdisk",
											},
										},
									},
								}},
							},
						},
					},
				},
			},
			pvcs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1-rootdisk", Namespace: "ns1"},
					Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("some-unknown-class")},
				},
			},
			driverMap:    StorageClassDriverMap{"ocs-rbd": "odf"},
			wantBackends: map[string]string{"ns1/vm-1": "unknown"},
			wantWarnings: 1,
		},
		{
			name: "VM with no volumes",
			vms:  []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
			vmObjects: []crclient.Object{
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{},
						},
					},
				},
			},
			driverMap:    StorageClassDriverMap{},
			wantBackends: map[string]string{"ns1/vm-1": "none"},
			wantWarnings: 1,
		},
		{
			name: "VM with PVC not found",
			vms:  []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
			vmObjects: []crclient.Object{
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{{
									Name: "rootdisk",
									VolumeSource: kubevirtv1.VolumeSource{
										PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
											PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
												ClaimName: "nonexistent-pvc",
											},
										},
									},
								}},
							},
						},
					},
				},
			},
			driverMap:    StorageClassDriverMap{},
			wantBackends: map[string]string{"ns1/vm-1": "unknown"},
			wantWarnings: 1,
		},
		{
			name: "VM with DataVolume-backed volumes",
			vms:  []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
			vmObjects: []crclient.Object{
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{{
									Name: "rootdisk",
									VolumeSource: kubevirtv1.VolumeSource{
										DataVolume: &kubevirtv1.DataVolumeSource{
											Name: "vm-1-dv",
										},
									},
								}},
							},
						},
					},
				},
			},
			pvcs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1-dv", Namespace: "ns1"},
					Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("ocs-rbd")},
				},
			},
			driverMap:    StorageClassDriverMap{"ocs-rbd": "odf"},
			wantBackends: map[string]string{"ns1/vm-1": "odf"},
			wantWarnings: 0,
		},
		{
			name: "VM with mixed storage classes",
			vms:  []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
			vmObjects: []crclient.Object{
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "rootdisk",
										VolumeSource: kubevirtv1.VolumeSource{
											PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
												PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
													ClaimName: "pvc-root",
												},
											},
										},
									},
									{
										Name: "datadisk",
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
				},
			},
			pvcs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-root", Namespace: "ns1"},
					Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("ocs-rbd")},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data", Namespace: "ns1"},
					Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("other-class")},
				},
			},
			driverMap:    StorageClassDriverMap{"ocs-rbd": "odf", "other-class": "dell"},
			wantBackends: map[string]string{"ns1/vm-1": "odf"},
			wantWarnings: 1,
		},
		{
			name: "Multiple VMs with different storage classes",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "ns1"},
				{Name: "vm-2", Namespace: "ns1"},
			},
			vmObjects: []crclient.Object{
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-1", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{{
									Name: "rootdisk",
									VolumeSource: kubevirtv1.VolumeSource{
										PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
											PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
												ClaimName: "pvc-1",
											},
										},
									},
								}},
							},
						},
					},
				},
				&kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-2", Namespace: "ns1"},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{{
									Name: "rootdisk",
									VolumeSource: kubevirtv1.VolumeSource{
										PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
											PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
												ClaimName: "pvc-2",
											},
										},
									},
								}},
							},
						},
					},
				},
			},
			pvcs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-1", Namespace: "ns1"},
					Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("ocs-rbd")},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-2", Namespace: "ns1"},
					Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("dell-pstore")},
				},
			},
			driverMap:    StorageClassDriverMap{"ocs-rbd": "odf", "dell-pstore": "dell-powerstore"},
			wantBackends: map[string]string{"ns1/vm-1": "odf", "ns1/vm-2": "dell-powerstore"},
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := testScheme()
			crClient := crfake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.vmObjects...).
				Build()

			k8sClient := k8sfake.NewSimpleClientset(tt.pvcs...) //nolint:staticcheck // NewClientset requires apply configurations not available for this project

			resolver := &TypedStorageBackendResolver{
				Client:     crClient,
				CoreClient: k8sClient.CoreV1(),
				DriverMap:  tt.driverMap,
			}

			backends, warnings, err := resolver.ResolveBackends(context.Background(), tt.vms)
			if err != nil {
				t.Fatalf("ResolveBackends() error: %v", err)
			}

			for key, want := range tt.wantBackends {
				got := backends[key]
				if got != want {
					t.Errorf("Backend[%q] = %q, want %q", key, got, want)
				}
			}

			if len(warnings) != tt.wantWarnings {
				t.Errorf("len(warnings) = %d, want %d; warnings: %v",
					len(warnings), tt.wantWarnings, warnings)
			}
		})
	}
}
