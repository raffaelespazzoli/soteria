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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubevirtv1 "kubevirt.io/api/core/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/noop"
	"github.com/soteria-project/soteria/pkg/engine"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = kubevirtv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

func strPtr(s string) *string { return &s }

// fakeSCLister implements drivers.StorageClassLister for unit tests.
type fakeSCLister struct {
	provisioners map[string]string // storageClassName → provisioner
}

func (f *fakeSCLister) GetProvisioner(_ context.Context, scName string) (string, error) {
	p, ok := f.provisioners[scName]
	if !ok {
		return "", fmt.Errorf("storage class %q not found", scName)
	}
	return p, nil
}

// newTestRegistry creates a fresh Registry with the given provisioner→factory
// mappings and optional fallback.
func newTestRegistry(provisioners map[string]drivers.DriverFactory, fallback drivers.DriverFactory) *drivers.Registry {
	reg := drivers.NewRegistry()
	for name, factory := range provisioners {
		reg.RegisterDriver(name, factory)
	}
	if fallback != nil {
		reg.SetFallbackDriver(fallback)
	}
	return reg
}

func noopFactory() drivers.StorageProvider { return noop.New() }

func TestResolveBackends(t *testing.T) {
	tests := []struct {
		name         string
		vms          []engine.VMReference
		vmObjects    []crclient.Object
		pvcs         []runtime.Object
		registry     *drivers.Registry
		scLister     drivers.StorageClassLister
		wantBackends map[string]string
		wantWarnings int
	}{
		{
			name: "VM with PVC using registered provisioner",
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
			registry: newTestRegistry(map[string]drivers.DriverFactory{
				noop.ProvisionerName: noopFactory,
			}, nil),
			scLister:     &fakeSCLister{provisioners: map[string]string{"ocs-rbd": noop.ProvisionerName}},
			wantBackends: map[string]string{"ns1/vm-1": noop.ProvisionerName},
			wantWarnings: 0,
		},
		{
			name: "VM with PVC using unregistered provisioner",
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
			registry:     newTestRegistry(nil, nil),
			scLister:     &fakeSCLister{provisioners: map[string]string{"some-unknown-class": "unregistered.csi.com"}},
			wantBackends: map[string]string{"ns1/vm-1": backendUnknown},
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
			registry:     newTestRegistry(nil, nil),
			scLister:     &fakeSCLister{},
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
			registry:     newTestRegistry(nil, nil),
			scLister:     &fakeSCLister{},
			wantBackends: map[string]string{"ns1/vm-1": backendUnknown},
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
			registry: newTestRegistry(map[string]drivers.DriverFactory{
				noop.ProvisionerName: noopFactory,
			}, nil),
			scLister:     &fakeSCLister{provisioners: map[string]string{"ocs-rbd": noop.ProvisionerName}},
			wantBackends: map[string]string{"ns1/vm-1": noop.ProvisionerName},
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
			registry: newTestRegistry(map[string]drivers.DriverFactory{
				noop.ProvisionerName:           noopFactory,
				"dell-powerstore.csi.dell.com": noopFactory,
			}, nil),
			scLister: &fakeSCLister{provisioners: map[string]string{
				"ocs-rbd":     noop.ProvisionerName,
				"other-class": "dell-powerstore.csi.dell.com",
			}},
			wantBackends: map[string]string{"ns1/vm-1": noop.ProvisionerName},
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
			registry: newTestRegistry(map[string]drivers.DriverFactory{
				noop.ProvisionerName:           noopFactory,
				"dell-powerstore.csi.dell.com": noopFactory,
			}, nil),
			scLister: &fakeSCLister{provisioners: map[string]string{
				"ocs-rbd":     noop.ProvisionerName,
				"dell-pstore": "dell-powerstore.csi.dell.com",
			}},
			wantBackends: map[string]string{"ns1/vm-1": noop.ProvisionerName, "ns1/vm-2": "dell-powerstore.csi.dell.com"},
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
				Registry:   tt.registry,
				SCLister:   tt.scLister,
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

func TestResolveBackends_FallbackEnabled(t *testing.T) {
	scheme := testScheme()
	crClient := crfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-fb", Namespace: "ns1"},
			Spec: kubevirtv1.VirtualMachineSpec{
				Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
					Spec: kubevirtv1.VirtualMachineInstanceSpec{
						Volumes: []kubevirtv1.Volume{{
							Name: "rootdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "vm-fb-root",
									},
								},
							},
						}},
					},
				},
			},
		}).
		Build()

	k8sClient := k8sfake.NewSimpleClientset( //nolint:staticcheck // NewClientset requires apply configurations not available for this project
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-fb-root", Namespace: "ns1"},
			Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("fancy-sc")},
		},
	)

	registry := newTestRegistry(nil, noopFactory)
	scLister := &fakeSCLister{provisioners: map[string]string{"fancy-sc": "unregistered.csi.com"}}

	resolver := &TypedStorageBackendResolver{
		Client:     crClient,
		CoreClient: k8sClient.CoreV1(),
		Registry:   registry,
		SCLister:   scLister,
	}

	backends, warnings, err := resolver.ResolveBackends(context.Background(), []engine.VMReference{{Name: "vm-fb", Namespace: "ns1"}})
	if err != nil {
		t.Fatalf("ResolveBackends() error: %v", err)
	}

	if got := backends["ns1/vm-fb"]; got != "unregistered.csi.com" {
		t.Errorf("Backend = %q, want %q (provisioner name from fallback)", got, "unregistered.csi.com")
	}
	if len(warnings) != 0 {
		t.Errorf("Expected no warnings with fallback enabled, got %d: %v", len(warnings), warnings)
	}
}

func TestResolveBackends_NilRegistry(t *testing.T) {
	scheme := testScheme()
	crClient := crfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-noreg", Namespace: "ns1"},
			Spec: kubevirtv1.VirtualMachineSpec{
				Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
					Spec: kubevirtv1.VirtualMachineInstanceSpec{
						Volumes: []kubevirtv1.Volume{{
							Name: "rootdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "vm-noreg-root",
									},
								},
							},
						}},
					},
				},
			},
		}).
		Build()

	k8sClient := k8sfake.NewSimpleClientset( //nolint:staticcheck // NewClientset requires apply configurations not available for this project
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-noreg-root", Namespace: "ns1"},
			Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("some-sc")},
		},
	)

	resolver := &TypedStorageBackendResolver{
		Client:     crClient,
		CoreClient: k8sClient.CoreV1(),
		Registry:   nil,
		SCLister:   &fakeSCLister{provisioners: map[string]string{"some-sc": "csi.example.com"}},
	}

	backends, warnings, err := resolver.ResolveBackends(context.Background(), []engine.VMReference{{Name: "vm-noreg", Namespace: "ns1"}})
	if err != nil {
		t.Fatalf("ResolveBackends() error: %v", err)
	}

	if got := backends["ns1/vm-noreg"]; got != backendUnknown {
		t.Errorf("Backend = %q, want %q", got, backendUnknown)
	}
	if len(warnings) != 1 {
		t.Errorf("Expected 1 warning for nil Registry, got %d: %v", len(warnings), warnings)
	}
}

func TestResolveBackends_EmptyProvisioner(t *testing.T) {
	scheme := testScheme()
	crClient := crfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-empty", Namespace: "ns1"},
			Spec: kubevirtv1.VirtualMachineSpec{
				Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
					Spec: kubevirtv1.VirtualMachineInstanceSpec{
						Volumes: []kubevirtv1.Volume{{
							Name: "rootdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "vm-empty-root",
									},
								},
							},
						}},
					},
				},
			},
		}).
		Build()

	k8sClient := k8sfake.NewSimpleClientset( //nolint:staticcheck // NewClientset requires apply configurations not available for this project
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-empty-root", Namespace: "ns1"},
			Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("misconfigured-sc")},
		},
	)

	resolver := &TypedStorageBackendResolver{
		Client:     crClient,
		CoreClient: k8sClient.CoreV1(),
		Registry:   newTestRegistry(nil, nil),
		SCLister:   &fakeSCLister{provisioners: map[string]string{"misconfigured-sc": ""}},
	}

	backends, warnings, err := resolver.ResolveBackends(context.Background(), []engine.VMReference{{Name: "vm-empty", Namespace: "ns1"}})
	if err != nil {
		t.Fatalf("ResolveBackends() error: %v", err)
	}

	if got := backends["ns1/vm-empty"]; got != backendUnknown {
		t.Errorf("Backend = %q, want %q", got, backendUnknown)
	}
	if len(warnings) != 1 {
		t.Errorf("Expected 1 warning for empty provisioner, got %d: %v", len(warnings), warnings)
	}
}

func TestResolveBackends_NilSCLister(t *testing.T) {
	scheme := testScheme()
	crClient := crfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-nil", Namespace: "ns1"},
			Spec: kubevirtv1.VirtualMachineSpec{
				Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
					Spec: kubevirtv1.VirtualMachineInstanceSpec{
						Volumes: []kubevirtv1.Volume{{
							Name: "rootdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "vm-nil-root",
									},
								},
							},
						}},
					},
				},
			},
		}).
		Build()

	k8sClient := k8sfake.NewSimpleClientset( //nolint:staticcheck // NewClientset requires apply configurations not available for this project
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-nil-root", Namespace: "ns1"},
			Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("some-sc")},
		},
	)

	resolver := &TypedStorageBackendResolver{
		Client:     crClient,
		CoreClient: k8sClient.CoreV1(),
		Registry:   drivers.NewRegistry(),
		SCLister:   nil,
	}

	backends, warnings, err := resolver.ResolveBackends(context.Background(), []engine.VMReference{{Name: "vm-nil", Namespace: "ns1"}})
	if err != nil {
		t.Fatalf("ResolveBackends() error: %v", err)
	}

	if got := backends["ns1/vm-nil"]; got != backendUnknown {
		t.Errorf("Backend = %q, want %q", got, backendUnknown)
	}
	if len(warnings) != 1 {
		t.Errorf("Expected 1 warning for nil SCLister, got %d: %v", len(warnings), warnings)
	}
}
