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

package shadowpv

import (
	"context"
	"slices"
	"testing"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/csiextension"
)

const (
	testLocalSite  = "dc-east"
	testRemoteSite = "dc-west"
	testNamespace  = "test-ns"
	testPlanName   = "erp-full-stack"
	testVGName     = "vm-web01"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	_ = replicationv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

func newReconciler(objs ...client.Object) *ShadowPVPublisherReconciler {
	scheme := newTestScheme()
	cb := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.ShadowPV{})
	cl := cb.Build()
	return &ShadowPVPublisherReconciler{
		Client:    cl,
		Scheme:    scheme,
		LocalSite: testLocalSite,
		APIReader: cl,
	}
}

func testPV(name string) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "rook-ceph.rbd.csi.ceph.com",
					VolumeHandle: "0001-0024-abc123-0000000000000001-def456",
				},
			},
		},
	}
}

func testPVC(name, pvName string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: pvName,
		},
	}
}

func testVR(name, pvcName string) *replicationv1alpha1.VolumeReplication {
	return &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				drivers.LabelDRPlan:           testPlanName,
				csiextension.LabelVolumeGroup: testVGName,
			},
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			DataSource: corev1.TypedLocalObjectReference{
				Kind: "PersistentVolumeClaim",
				Name: pvcName,
			},
		},
	}
}

func testVGR(name, vgName string) *replicationv1alpha1.VolumeGroupReplication {
	return &replicationv1alpha1.VolumeGroupReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				drivers.LabelDRPlan:           testPlanName,
				csiextension.LabelVolumeGroup: vgName,
			},
		},
	}
}

func TestPublisher_VR_CreatesShadowPV(t *testing.T) {
	pv := testPV("pv-data-1")
	pvc := testPVC("pvc-data-1", "pv-data-1")
	vr := testVR("vr-1", "pvc-data-1")

	r := newReconciler(vr, pvc, pv)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatal("Unexpected requeue")
	}

	var spv soteriav1alpha1.ShadowPV
	shadowPVName := testPlanName + "-" + testVGName
	if err := r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv); err != nil {
		t.Fatalf("ShadowPV not created: %v", err)
	}
	if len(spv.Spec.PVs) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(spv.Spec.PVs))
	}
	if spv.Spec.PVs[0].ClusterName != testLocalSite {
		t.Errorf("ClusterName = %q, want %q", spv.Spec.PVs[0].ClusterName, testLocalSite)
	}
	if spv.Spec.PVs[0].PVName != "pv-data-1" {
		t.Errorf("PVName = %q, want %q", spv.Spec.PVs[0].PVName, "pv-data-1")
	}
	if spv.Labels[drivers.LabelDRPlan] != testPlanName {
		t.Errorf("Label %s = %q, want %q", drivers.LabelDRPlan, spv.Labels[drivers.LabelDRPlan], testPlanName)
	}
}

func TestPublisher_VGR_MultiplePVCs(t *testing.T) {
	vgName := "ns-erp-database"
	pv1 := testPV("pv-db-1")
	pv2 := testPV("pv-db-2")
	pv3 := testPV("pv-db-3")

	pvc1 := testPVC("pvc-db-1", "pv-db-1")
	pvc1.Labels = map[string]string{csiextension.LabelVolumeGroup: vgName}
	pvc2 := testPVC("pvc-db-2", "pv-db-2")
	pvc2.Labels = map[string]string{csiextension.LabelVolumeGroup: vgName}
	pvc3 := testPVC("pvc-db-3", "pv-db-3")
	pvc3.Labels = map[string]string{csiextension.LabelVolumeGroup: vgName}

	vgr := testVGR("vgr-1", vgName)

	r := newReconciler(vgr, pvc1, pvc2, pvc3, pv1, pv2, pv3)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vgr-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatal("Unexpected requeue")
	}

	shadowPVName := testPlanName + "-" + vgName
	var spv soteriav1alpha1.ShadowPV
	if err := r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv); err != nil {
		t.Fatalf("ShadowPV not created: %v", err)
	}
	if len(spv.Spec.PVs) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(spv.Spec.PVs))
	}
	for _, entry := range spv.Spec.PVs {
		if entry.ClusterName != testLocalSite {
			t.Errorf("ClusterName = %q, want %q", entry.ClusterName, testLocalSite)
		}
	}
}

func TestPublisher_Idempotent_NoUpdate(t *testing.T) {
	pv := testPV("pv-data-1")
	pvc := testPVC("pvc-data-1", "pv-data-1")
	vr := testVR("vr-1", "pvc-data-1")

	shadowPVName := testPlanName + "-" + testVGName
	existingSPV := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:            shadowPVName,
			Labels:          map[string]string{drivers.LabelDRPlan: testPlanName},
			ResourceVersion: "100",
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{{
				ClusterName: testLocalSite,
				PVName:      "pv-data-1",
				PV:          pv.Spec,
			}},
		},
	}

	vr.Finalizers = []string{publisherFinalizer}

	r := newReconciler(vr, pvc, pv, existingSPV)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var spv soteriav1alpha1.ShadowPV
	if err := r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv); err != nil {
		t.Fatalf("ShadowPV disappeared: %v", err)
	}
	if spv.ResourceVersion != "100" {
		t.Errorf("ResourceVersion changed from 100 to %s — expected no update", spv.ResourceVersion)
	}
}

func TestPublisher_NoDRPlanLabel_Skipped(t *testing.T) {
	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vr-no-label",
			Namespace: testNamespace,
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			DataSource: corev1.TypedLocalObjectReference{
				Kind: "PersistentVolumeClaim",
				Name: "pvc-x",
			},
		},
	}

	r := newReconciler(vr)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-no-label"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
}

func TestPublisher_VR_Deleted_EntryRemoved(t *testing.T) {
	now := metav1.Now()
	pv := testPV("pv-data-1")

	shadowPVName := testPlanName + "-" + testVGName
	existingSPV := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:   shadowPVName,
			Labels: map[string]string{drivers.LabelDRPlan: testPlanName},
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{
				{ClusterName: testLocalSite, PVName: "pv-data-1", PV: pv.Spec},
				{ClusterName: testRemoteSite, PVName: "pv-remote-1", PV: pv.Spec},
			},
		},
	}

	vr := testVR("vr-1", "pvc-data-1")
	vr.DeletionTimestamp = &now
	vr.Finalizers = []string{publisherFinalizer}

	r := newReconciler(vr, existingSPV)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var spv soteriav1alpha1.ShadowPV
	if err := r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv); err != nil {
		t.Fatalf("ShadowPV disappeared (should still have remote entry): %v", err)
	}
	if len(spv.Spec.PVs) != 1 {
		t.Fatalf("Expected 1 remaining entry, got %d", len(spv.Spec.PVs))
	}
	if spv.Spec.PVs[0].ClusterName != testRemoteSite {
		t.Errorf("Remaining entry from %q, want %q", spv.Spec.PVs[0].ClusterName, testRemoteSite)
	}

	// Fake client GC deletes the VR after finalizer removal + DeletionTimestamp,
	// so NotFound confirms the finalizer was successfully removed.
	var updatedVR replicationv1alpha1.VolumeReplication
	err = r.Get(context.Background(), client.ObjectKeyFromObject(vr), &updatedVR)
	if err != nil {
		// Object deleted by fake GC — finalizer was removed
		return
	}
	for _, f := range updatedVR.Finalizers {
		if f == publisherFinalizer {
			t.Error("Finalizer was not removed from VR")
		}
	}
}

func TestPublisher_LastEntry_ShadowPVDeleted(t *testing.T) {
	now := metav1.Now()
	pv := testPV("pv-data-1")

	shadowPVName := testPlanName + "-" + testVGName
	existingSPV := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:   shadowPVName,
			Labels: map[string]string{drivers.LabelDRPlan: testPlanName},
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{
				{ClusterName: testLocalSite, PVName: "pv-data-1", PV: pv.Spec},
			},
		},
	}

	vr := testVR("vr-1", "pvc-data-1")
	vr.DeletionTimestamp = &now
	vr.Finalizers = []string{publisherFinalizer}

	r := newReconciler(vr, existingSPV)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var spv soteriav1alpha1.ShadowPV
	err = r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv)
	if err == nil {
		t.Fatal("ShadowPV should have been deleted (last entry removed)")
	}
}

func TestPublisher_PVCNotBound_Skipped(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-unbound",
			Namespace: testNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "", // not bound
		},
	}
	vr := testVR("vr-unbound", "pvc-unbound")

	r := newReconciler(vr, pvc)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-unbound"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// ShadowPV should not be created for unbound PVC
	shadowPVName := testPlanName + "-" + testVGName
	var spv soteriav1alpha1.ShadowPV
	err = r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv)
	if err == nil {
		t.Fatal("ShadowPV should not have been created for unbound PVC")
	}
}

func TestPublisher_PVNotFound_Skipped(t *testing.T) {
	pvc := testPVC("pvc-missing-pv", "pv-does-not-exist")
	vr := testVR("vr-missing-pv", "pvc-missing-pv")

	r := newReconciler(vr, pvc)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-missing-pv"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	shadowPVName := testPlanName + "-" + testVGName
	var spv soteriav1alpha1.ShadowPV
	err = r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv)
	if err == nil {
		t.Fatal("ShadowPV should not have been created when PV is missing")
	}
}

func TestPublisher_MultiSite_PreservesRemoteEntries(t *testing.T) {
	pv := testPV("pv-local-1")
	pvc := testPVC("pvc-local-1", "pv-local-1")
	vr := testVR("vr-1", "pvc-local-1")

	remotePV := testPV("pv-remote-1")
	shadowPVName := testPlanName + "-" + testVGName
	existingSPV := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:   shadowPVName,
			Labels: map[string]string{drivers.LabelDRPlan: testPlanName},
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{
				{ClusterName: testRemoteSite, PVName: "pv-remote-1", PV: remotePV.Spec},
			},
		},
	}

	r := newReconciler(vr, pvc, pv, existingSPV)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var spv soteriav1alpha1.ShadowPV
	if err := r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv); err != nil {
		t.Fatalf("ShadowPV not found: %v", err)
	}
	if len(spv.Spec.PVs) != 2 {
		t.Fatalf("Expected 2 entries (remote + local), got %d", len(spv.Spec.PVs))
	}

	hasRemote, hasLocal := false, false
	for _, e := range spv.Spec.PVs {
		if e.ClusterName == testRemoteSite {
			hasRemote = true
		}
		if e.ClusterName == testLocalSite {
			hasLocal = true
		}
	}
	if !hasRemote {
		t.Error("Remote entry was not preserved")
	}
	if !hasLocal {
		t.Error("Local entry was not added")
	}
}

func TestPublisher_Finalizer_Added(t *testing.T) {
	pv := testPV("pv-data-1")
	pvc := testPVC("pvc-data-1", "pv-data-1")
	vr := testVR("vr-no-fin", "pvc-data-1")

	r := newReconciler(vr, pvc, pv)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-no-fin"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var updatedVR replicationv1alpha1.VolumeReplication
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(vr), &updatedVR); err != nil {
		t.Fatalf("Could not get updated VR: %v", err)
	}

	hasFinalizer := slices.Contains(updatedVR.Finalizers, publisherFinalizer)
	if !hasFinalizer {
		t.Error("Finalizer was not added to VR")
	}
}

func TestPublisher_MultiVR_PreservesSiblingEntries(t *testing.T) {
	pv1 := testPV("pv-disk-1")
	pv2 := testPV("pv-disk-2")
	pvc1 := testPVC("pvc-disk-1", "pv-disk-1")
	pvc2 := testPVC("pvc-disk-2", "pv-disk-2")
	vr1 := testVR("vr-disk-1", "pvc-disk-1")
	vr2 := testVR("vr-disk-2", "pvc-disk-2")

	r := newReconciler(vr1, vr2, pvc1, pvc2, pv1, pv2)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-disk-1"},
	})
	if err != nil {
		t.Fatalf("First VR reconcile failed: %v", err)
	}

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-disk-2"},
	})
	if err != nil {
		t.Fatalf("Second VR reconcile failed: %v", err)
	}

	shadowPVName := testPlanName + "-" + testVGName
	var spv soteriav1alpha1.ShadowPV
	if err := r.Get(context.Background(), client.ObjectKey{Name: shadowPVName}, &spv); err != nil {
		t.Fatalf("ShadowPV not found: %v", err)
	}
	if len(spv.Spec.PVs) != 2 {
		t.Fatalf("Expected 2 entries (one per VR), got %d", len(spv.Spec.PVs))
	}

	pvNames := make(map[string]bool)
	for _, e := range spv.Spec.PVs {
		pvNames[e.PVName] = true
	}
	if !pvNames["pv-disk-1"] {
		t.Error("Entry for pv-disk-1 (from VR-1) was lost after VR-2 reconcile")
	}
	if !pvNames["pv-disk-2"] {
		t.Error("Entry for pv-disk-2 (from VR-2) was not added")
	}
}

func TestPublisher_ShadowPV_AlreadyDeleted(t *testing.T) {
	now := metav1.Now()
	vr := testVR("vr-cascade", "pvc-x")
	vr.DeletionTimestamp = &now
	vr.Finalizers = []string{publisherFinalizer}

	// No ShadowPV exists (already deleted by cascade)
	r := newReconciler(vr)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: "vr-cascade"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Fake client GC deletes the VR after finalizer removal + DeletionTimestamp,
	// so NotFound confirms the finalizer was successfully removed.
	var updatedVR replicationv1alpha1.VolumeReplication
	err = r.Get(context.Background(), client.ObjectKeyFromObject(vr), &updatedVR)
	if err != nil {
		return
	}
	for _, f := range updatedVR.Finalizers {
		if f == publisherFinalizer {
			t.Error("Finalizer was not removed when ShadowPV already deleted")
		}
	}
}

func TestEntriesEqual(t *testing.T) {
	pvSpec := testPV("pv-1").Spec

	tests := []struct {
		name string
		a, b []soteriav1alpha1.ShadowPVEntry
		want bool
	}{
		{
			name: "identical",
			a:    []soteriav1alpha1.ShadowPVEntry{{ClusterName: "a", PVName: "pv1", PV: pvSpec}},
			b:    []soteriav1alpha1.ShadowPVEntry{{ClusterName: "a", PVName: "pv1", PV: pvSpec}},
			want: true,
		},
		{
			name: "different length",
			a:    []soteriav1alpha1.ShadowPVEntry{{ClusterName: "a", PVName: "pv1", PV: pvSpec}},
			b:    nil,
			want: false,
		},
		{
			name: "different cluster",
			a:    []soteriav1alpha1.ShadowPVEntry{{ClusterName: "a", PVName: "pv1", PV: pvSpec}},
			b:    []soteriav1alpha1.ShadowPVEntry{{ClusterName: "b", PVName: "pv1", PV: pvSpec}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entriesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("entriesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
