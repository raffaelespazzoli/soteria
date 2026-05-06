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

package drplan

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// mockDiskEnricher returns pre-configured disks per VM.
type mockDiskEnricher struct {
	disksByVM map[string][]soteriav1alpha1.DiscoveredDisk
	errByVM   map[string]error
}

func (m *mockDiskEnricher) EnrichDisks(_ context.Context, vmName, _ string) ([]soteriav1alpha1.DiscoveredDisk, error) {
	if m.errByVM != nil {
		if err, ok := m.errByVM[vmName]; ok {
			return nil, err
		}
	}
	if m.disksByVM != nil {
		return m.disksByVM[vmName], nil
	}
	return nil, nil
}

func newReconcilerWithDiskEnricher(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
	enricher engine.DiskEnricher,
) (*DRPlanReconciler, client.Client) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.DRPlan{}).
		Build()

	return &DRPlanReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		VMDiscoverer:    discoverer,
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		DiskEnricher:    enricher,
	}, fakeClient
}

func newReconcilerWithDiskEnricherAndSite(
	objs []client.Object,
	discoverer engine.VMDiscoverer,
	enricher engine.DiskEnricher,
	localSite string,
) (*DRPlanReconciler, client.Client) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.DRPlan{}).
		Build()

	return &DRPlanReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		VMDiscoverer:    discoverer,
		NamespaceLookup: &mockNamespaceLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
		Recorder:        events.NewFakeRecorder(10),
		DiskEnricher:    enricher,
		LocalSite:       localSite,
	}, fakeClient
}

func TestReconcile_ActiveSite_DisksInWaves(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-2", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	enricher := &mockDiskEnricher{
		disksByVM: map[string][]soteriav1alpha1.DiscoveredDisk{
			"vm-1": {
				{Name: "root-disk", PVCName: "pvc-root", StorageClass: "ocs-storagecluster-ceph-rbd"},
				{Name: "data-disk", PVCName: "pvc-data", StorageClass: "ocs-storagecluster-ceph-rbd"},
			},
			"vm-2": {
				{Name: "os-disk", PVCName: "pvc-os", StorageClass: "dell-powerstore"},
			},
		},
	}

	r, c := newReconcilerWithDiskEnricher([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, enricher)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if len(updated.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(updated.Status.Waves))
	}
	wave := updated.Status.Waves[0]
	if len(wave.VMs) != 2 {
		t.Fatalf("Wave VMs = %d, want 2", len(wave.VMs))
	}

	vm1 := findVM(wave.VMs, "vm-1")
	if vm1 == nil {
		t.Fatal("vm-1 not found in wave")
	}
	if len(vm1.Disks) != 2 {
		t.Errorf("vm-1 disks = %d, want 2", len(vm1.Disks))
	}
	if vm1.Disks[0].Name != "root-disk" || vm1.Disks[0].PVCName != "pvc-root" {
		t.Errorf("vm-1 disk[0] = %+v, want root-disk/pvc-root", vm1.Disks[0])
	}

	vm2 := findVM(wave.VMs, "vm-2")
	if vm2 == nil {
		t.Fatal("vm-2 not found in wave")
	}
	if len(vm2.Disks) != 1 {
		t.Errorf("vm-2 disks = %d, want 1", len(vm2.Disks))
	}
}

func TestReconcile_SiteDiscovery_DisksEnriched(t *testing.T) {
	tests := []struct {
		name         string
		localSite    string
		vmName       string
		disk         soteriav1alpha1.DiscoveredDisk
		checkPrimary bool
	}{
		{
			name:         "active site propagates disks to PrimarySiteDiscovery",
			localSite:    testPrimarySite,
			vmName:       "vm-1",
			disk:         soteriav1alpha1.DiscoveredDisk{Name: "root-disk", PVCName: "pvc-root", StorageClass: "sc-a"},
			checkPrimary: true,
		},
		{
			name:         "passive site propagates disks to SecondarySiteDiscovery",
			localSite:    testSecondarySite,
			vmName:       "vm-peer-1",
			disk:         soteriav1alpha1.DiscoveredDisk{Name: "data-disk", PVCName: "pvc-data", StorageClass: "sc-b"},
			checkPrimary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := newTestPlan()
			vms := []engine.VMReference{
				{Name: tt.vmName, Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
			}
			enricher := &mockDiskEnricher{
				disksByVM: map[string][]soteriav1alpha1.DiscoveredDisk{
					tt.vmName: {tt.disk},
				},
			}

			r, c := newReconcilerWithDiskEnricherAndSite(
				[]client.Object{plan}, &mockVMDiscoverer{vms: vms}, enricher, tt.localSite)

			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
			if err != nil {
				t.Fatalf("Reconcile() error: %v", err)
			}

			var updated soteriav1alpha1.DRPlan
			if err := c.Get(context.Background(), planKey, &updated); err != nil {
				t.Fatalf("Failed to get plan: %v", err)
			}

			var discovery *soteriav1alpha1.SiteDiscovery
			if tt.checkPrimary {
				discovery = updated.Status.PrimarySiteDiscovery
			} else {
				discovery = updated.Status.SecondarySiteDiscovery
			}
			if discovery == nil {
				t.Fatal("SiteDiscovery should be populated")
			}
			if len(discovery.VMs) != 1 {
				t.Fatalf("SiteDiscovery VMs = %d, want 1", len(discovery.VMs))
			}
			vm := discovery.VMs[0]
			if len(vm.Disks) != 1 {
				t.Errorf("SiteDiscovery VM disks = %d, want 1", len(vm.Disks))
			}
			if vm.Disks[0].PVCName != tt.disk.PVCName {
				t.Errorf("SiteDiscovery VM disk PVCName = %q, want %q",
					vm.Disks[0].PVCName, tt.disk.PVCName)
			}
			if vm.Disks[0].StorageClass != tt.disk.StorageClass {
				t.Errorf("SiteDiscovery VM disk StorageClass = %q, want %q",
					vm.Disks[0].StorageClass, tt.disk.StorageClass)
			}
		})
	}
}

func TestReconcile_DiskEnrichmentError_ContinuesWithEmptyDisks(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-ok", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
		{Name: "vm-fail", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	enricher := &mockDiskEnricher{
		disksByVM: map[string][]soteriav1alpha1.DiscoveredDisk{
			"vm-ok": {
				{Name: "root-disk", PVCName: "pvc-root", StorageClass: "sc-a"},
			},
		},
		errByVM: map[string]error{
			"vm-fail": fmt.Errorf("connection refused"),
		},
	}

	r, c := newReconcilerWithDiskEnricher([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, enricher)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if len(updated.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(updated.Status.Waves))
	}
	wave := updated.Status.Waves[0]

	vmOK := findVM(wave.VMs, "vm-ok")
	if vmOK == nil {
		t.Fatal("vm-ok not found in wave")
	}
	if len(vmOK.Disks) != 1 {
		t.Errorf("vm-ok disks = %d, want 1", len(vmOK.Disks))
	}

	vmFail := findVM(wave.VMs, "vm-fail")
	if vmFail == nil {
		t.Fatal("vm-fail not found in wave")
	}
	if len(vmFail.Disks) != 0 {
		t.Errorf("vm-fail disks = %d, want 0 (enrichment failed)", len(vmFail.Disks))
	}

	readyCond := findReadyCondition(updated.Status.Conditions)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Expected Ready=True even when disk enrichment fails for one VM")
	}
}

func TestReconcile_NilDiskEnricher_NoDisks(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	r, c := newReconcilerWithDiskEnricher([]client.Object{plan}, &mockVMDiscoverer{vms: vms}, nil)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if len(updated.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(updated.Status.Waves))
	}
	vm := updated.Status.Waves[0].VMs[0]
	if len(vm.Disks) != 0 {
		t.Errorf("VM disks should be empty when DiskEnricher is nil, got %d", len(vm.Disks))
	}
}

func TestReconcile_PassiveSite_DiskEnrichmentError_ContinuesWithEmptyDisks(t *testing.T) {
	plan := newTestPlan()
	vms := []engine.VMReference{
		{Name: "vm-peer-1", Namespace: "default", Labels: map[string]string{"soteria.io/wave": "1"}},
	}

	enricher := &mockDiskEnricher{
		errByVM: map[string]error{
			"vm-peer-1": fmt.Errorf("timeout"),
		},
	}

	r, c := newReconcilerWithDiskEnricherAndSite(
		[]client.Object{plan}, &mockVMDiscoverer{vms: vms}, enricher, testSecondarySite)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: planKey})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := c.Get(context.Background(), planKey, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.SecondarySiteDiscovery == nil {
		t.Fatal("SecondarySiteDiscovery should be populated even on enrichment failure")
	}
	if len(updated.Status.SecondarySiteDiscovery.VMs) != 1 {
		t.Fatalf("SecondarySiteDiscovery VMs = %d, want 1", len(updated.Status.SecondarySiteDiscovery.VMs))
	}
	vm := updated.Status.SecondarySiteDiscovery.VMs[0]
	if len(vm.Disks) != 0 {
		t.Errorf("VM disks should be empty on enrichment failure, got %d", len(vm.Disks))
	}
}

func findVM(vms []soteriav1alpha1.DiscoveredVM, name string) *soteriav1alpha1.DiscoveredVM {
	for i := range vms {
		if vms[i].Name == name {
			return &vms[i]
		}
	}
	return nil
}

// Compile-time check.
var _ engine.DiskEnricher = (*mockDiskEnricher)(nil)
