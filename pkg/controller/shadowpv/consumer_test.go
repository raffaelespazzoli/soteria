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
	"testing"

	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

func newConsumerReconciler(objs ...client.Object) (*ShadowPVConsumerReconciler, *record.FakeRecorder) {
	scheme := newTestScheme()
	cb := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(&soteriav1alpha1.ShadowPV{})
	cl := cb.Build()
	recorder := record.NewFakeRecorder(10)
	return &ShadowPVConsumerReconciler{
		Client:        cl,
		Scheme:        scheme,
		LocalSite:     testLocalSite,
		APIReader:     cl,
		EventRecorder: recorder,
	}, recorder
}

func testCephVolumeHandle(poolIDHex string) string {
	return "0001-0009-rook-ceph-" + poolIDHex + "-7f3da9a2-abcd-1234-ef56-789012345678"
}

func testShadowPVWithRemoteEntry(
	name, planName string, entries []soteriav1alpha1.ShadowPVEntry,
) *soteriav1alpha1.ShadowPV {
	return &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{drivers.LabelDRPlan: planName},
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: entries,
		},
	}
}

func testCephPVSpec(volumeHandle, poolName string) corev1.PersistentVolumeSpec {
	return corev1.PersistentVolumeSpec{
		Capacity: corev1.ResourceList{
			corev1.ResourceStorage: resource.MustParse("10Gi"),
		},
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		PersistentVolumeSource: corev1.PersistentVolumeSource{
			CSI: &corev1.CSIPersistentVolumeSource{
				Driver:       "rook-ceph.rbd.csi.ceph.com",
				VolumeHandle: volumeHandle,
				VolumeAttributes: map[string]string{
					"pool": poolName,
				},
			},
		},
	}
}

func testCephBlockPool(name string, poolNumber int) *unstructured.Unstructured {
	cbp := &unstructured.Unstructured{}
	cbp.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "ceph.rook.io", Version: "v1", Kind: "CephBlockPool",
	})
	cbp.SetName(name)
	cbp.SetNamespace("rook-ceph")
	_ = unstructured.SetNestedField(cbp.Object, map[string]any{
		"poolNumber": fmt.Sprintf("%d", poolNumber),
	}, "status", "info")
	return cbp
}

func testCephBlockPoolWithStatusPoolID(name string, poolID int64) *unstructured.Unstructured {
	cbp := &unstructured.Unstructured{}
	cbp.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "ceph.rook.io", Version: "v1", Kind: "CephBlockPool",
	})
	cbp.SetName(name)
	cbp.SetNamespace("rook-ceph")
	_ = unstructured.SetNestedField(cbp.Object, poolID, "status", "poolID")
	return cbp
}

func TestConsumer_RemoteEntry_CreatesPV(t *testing.T) {
	sourcePoolID := "0000000000000001"
	volumeHandle := testCephVolumeHandle(sourcePoolID)
	cbp := testCephBlockPool("mirrored-pool", 3)

	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-remote-data-1",
			PV:          testCephPVSpec(volumeHandle, "mirrored-pool"),
		},
	})

	r, _ := newConsumerReconciler(spv, cbp)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if result.Requeue {
		t.Fatal("Unexpected requeue")
	}

	var pv corev1.PersistentVolume
	if err := r.Get(context.Background(), client.ObjectKey{Name: "pv-remote-data-1"}, &pv); err != nil {
		t.Fatalf("PV not created: %v", err)
	}
	if pv.Labels[consumerLabel] != "plan1-vg1" {
		t.Errorf("Consumer label = %q, want %q", pv.Labels[consumerLabel], "plan1-vg1")
	}
	if pv.Labels[drivers.LabelDRPlan] != testPlanName {
		t.Errorf("DRPlan label = %q, want %q", pv.Labels[drivers.LabelDRPlan], testPlanName)
	}

	wantHandle := testCephVolumeHandle("0000000000000003")
	if pv.Spec.CSI.VolumeHandle != wantHandle {
		t.Errorf("VolumeHandle = %q, want %q", pv.Spec.CSI.VolumeHandle, wantHandle)
	}
}

func TestConsumer_LocalEntry_Skipped(t *testing.T) {
	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testLocalSite,
			PVName:      "pv-local-1",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
	})

	r, _ := newConsumerReconciler(spv)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var pv corev1.PersistentVolume
	err = r.Get(context.Background(), client.ObjectKey{Name: "pv-local-1"}, &pv)
	if err == nil {
		t.Fatal("PV should not have been created for local entry")
	}
}

func TestConsumer_PVExists_WithLabel_NoOp(t *testing.T) {
	existingPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-existing-1",
			Labels: map[string]string{
				consumerLabel: "plan1-vg1",
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "rook-ceph.rbd.csi.ceph.com",
					VolumeHandle: testCephVolumeHandle("0000000000000002"),
				},
			},
		},
	}

	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-existing-1",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
	})

	r, recorder := newConsumerReconciler(spv, existingPV)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	select {
	case event := <-recorder.Events:
		t.Fatalf("Unexpected event emitted for idempotent PV: %s", event)
	default:
	}
}

func TestConsumer_PVExists_NoLabel_Conflict(t *testing.T) {
	existingPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-conflict-1",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "rook-ceph.rbd.csi.ceph.com",
					VolumeHandle: "some-other-handle",
				},
			},
		},
	}

	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-conflict-1",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
	})

	r, recorder := newConsumerReconciler(spv, existingPV)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	select {
	case event := <-recorder.Events:
		if !containsSubstring(event, "PVConflict") {
			t.Errorf("Expected PVConflict event, got: %s", event)
		}
	default:
		t.Error("Expected PVConflict event, got none")
	}

	var updatedSPV soteriav1alpha1.ShadowPV
	if err := r.Get(context.Background(), client.ObjectKey{Name: "plan1-vg1"}, &updatedSPV); err != nil {
		t.Fatalf("Could not get updated ShadowPV: %v", err)
	}
	cond := meta.FindStatusCondition(updatedSPV.Status.Conditions, "PVConflict")
	if cond == nil {
		t.Fatal("PVConflict condition not set on ShadowPV status")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("PVConflict status = %s, want True", cond.Status)
	}
	if cond.Reason != "ExistingPVNotOwnedByConsumer" {
		t.Errorf("PVConflict reason = %q, want ExistingPVNotOwnedByConsumer", cond.Reason)
	}
}

func TestConsumer_NonCephHandle_CreatesAsIs(t *testing.T) {
	nonCephSpec := corev1.PersistentVolumeSpec{
		Capacity: corev1.ResourceList{
			corev1.ResourceStorage: resource.MustParse("10Gi"),
		},
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		PersistentVolumeSource: corev1.PersistentVolumeSource{
			CSI: &corev1.CSIPersistentVolumeSource{
				Driver:       "other-csi-driver",
				VolumeHandle: "non-ceph-handle-format",
			},
		},
	}

	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-non-ceph-1",
			PV:          nonCephSpec,
		},
	})

	r, recorder := newConsumerReconciler(spv)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	var pv corev1.PersistentVolume
	if err := r.Get(context.Background(), client.ObjectKey{Name: "pv-non-ceph-1"}, &pv); err != nil {
		t.Fatalf("PV not created: %v", err)
	}
	if pv.Spec.CSI.VolumeHandle != "non-ceph-handle-format" {
		t.Errorf("VolumeHandle should be unchanged, got %q", pv.Spec.CSI.VolumeHandle)
	}

	select {
	case event := <-recorder.Events:
		if !containsSubstring(event, "PoolIDRewriteSkipped") {
			t.Errorf("Expected PoolIDRewriteSkipped event, got: %s", event)
		}
	default:
		t.Error("Expected PoolIDRewriteSkipped warning event, got none")
	}
}

func TestConsumer_CephBlockPoolNotFound_Error(t *testing.T) {
	volumeHandle := testCephVolumeHandle("0000000000000001")

	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-no-pool-1",
			PV:          testCephPVSpec(volumeHandle, "nonexistent-pool"),
		},
	})

	r, _ := newConsumerReconciler(spv)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})

	// When CephBlockPool is not found, pool-ID resolution fails and the
	// reconciler returns an error (requeue) without creating the PV.
	if err == nil {
		t.Fatal("Expected error when CephBlockPool is not found")
	}

	var pv corev1.PersistentVolume
	if getErr := r.Get(context.Background(), client.ObjectKey{Name: "pv-no-pool-1"}, &pv); getErr == nil {
		t.Fatal("PV should not be created when pool-ID resolution fails")
	}
}

func TestConsumer_MultipleRemoteEntries(t *testing.T) {
	cbp := testCephBlockPool("mirrored-pool", 5)

	entries := []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-multi-1",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-multi-2",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-multi-3",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
	}

	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, entries)
	r, _ := newConsumerReconciler(spv, cbp)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	expectedHandle := testCephVolumeHandle("0000000000000005")
	for _, pvName := range []string{"pv-multi-1", "pv-multi-2", "pv-multi-3"} {
		var pv corev1.PersistentVolume
		if err := r.Get(context.Background(), client.ObjectKey{Name: pvName}, &pv); err != nil {
			t.Errorf("PV %s not created: %v", pvName, err)
			continue
		}
		if pv.Spec.CSI.VolumeHandle != expectedHandle {
			t.Errorf("PV %s VolumeHandle = %q, want %q", pvName, pv.Spec.CSI.VolumeHandle, expectedHandle)
		}
	}
}

func TestConsumer_PoolIDRewrite_Correct(t *testing.T) {
	tests := []struct {
		name       string
		cbp        client.Object
		poolName   string
		wantPoolID string
	}{
		{
			name:       "status.info.poolNumber string field",
			cbp:        testCephBlockPool("data-pool", 42),
			poolName:   "data-pool",
			wantPoolID: "000000000000002a",
		},
		{
			name:       "status.poolID int field preferred",
			cbp:        testCephBlockPoolWithStatusPoolID("typed-pool", 9),
			poolName:   "typed-pool",
			wantPoolID: "0000000000000009",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volumeHandle := testCephVolumeHandle("0000000000000001")
			pvName := "pv-rewrite-" + tt.poolName

			spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, []soteriav1alpha1.ShadowPVEntry{
				{
					ClusterName: testRemoteSite,
					PVName:      pvName,
					PV:          testCephPVSpec(volumeHandle, tt.poolName),
				},
			})

			r, _ := newConsumerReconciler(spv, tt.cbp)

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
			})
			if err != nil {
				t.Fatalf("Reconcile failed: %v", err)
			}

			var pv corev1.PersistentVolume
			if err := r.Get(context.Background(), client.ObjectKey{Name: pvName}, &pv); err != nil {
				t.Fatalf("PV not created: %v", err)
			}

			wantHandle := testCephVolumeHandle(tt.wantPoolID)
			if pv.Spec.CSI.VolumeHandle != wantHandle {
				t.Errorf("VolumeHandle = %q, want %q", pv.Spec.CSI.VolumeHandle, wantHandle)
			}
		})
	}
}

func TestConsumer_EntryRemoved_PVRemains(t *testing.T) {
	cbp := testCephBlockPool("mirrored-pool", 2)
	rewrittenHandle := testCephVolumeHandle("0000000000000002")

	existingPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-was-created-1",
			Labels: map[string]string{
				consumerLabel: "plan2-vg2",
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "rook-ceph.rbd.csi.ceph.com",
					VolumeHandle: rewrittenHandle,
				},
			},
		},
	}

	// ShadowPV no longer has the entry for pv-was-created-1
	spv := testShadowPVWithRemoteEntry("plan2-vg2", "backup-plan", []soteriav1alpha1.ShadowPVEntry{})

	r, _ := newConsumerReconciler(spv, existingPV, cbp)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan2-vg2"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// PV should still exist — consumer never deletes PVs
	var pv corev1.PersistentVolume
	if err := r.Get(context.Background(), client.ObjectKey{Name: "pv-was-created-1"}, &pv); err != nil {
		t.Fatalf("PV should not have been deleted when entry is removed: %v", err)
	}
}

func TestConsumer_MixedEntries(t *testing.T) {
	cbp := testCephBlockPool("mirrored-pool", 7)

	entries := []soteriav1alpha1.ShadowPVEntry{
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-remote-1",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
		{
			ClusterName: testLocalSite,
			PVName:      "pv-local-1",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000003"), "mirrored-pool"),
		},
		{
			ClusterName: testRemoteSite,
			PVName:      "pv-remote-2",
			PV:          testCephPVSpec(testCephVolumeHandle("0000000000000001"), "mirrored-pool"),
		},
	}

	spv := testShadowPVWithRemoteEntry("plan1-vg1", testPlanName, entries)
	r, _ := newConsumerReconciler(spv, cbp)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plan1-vg1"},
	})
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Remote entries should have PVs created
	for _, pvName := range []string{"pv-remote-1", "pv-remote-2"} {
		var pv corev1.PersistentVolume
		if err := r.Get(context.Background(), client.ObjectKey{Name: pvName}, &pv); err != nil {
			t.Errorf("Remote PV %s not created: %v", pvName, err)
		}
	}

	// Local entry should NOT have a PV created
	var localPV corev1.PersistentVolume
	err = r.Get(context.Background(), client.ObjectKey{Name: "pv-local-1"}, &localPV)
	if err == nil {
		t.Error("PV should not be created for local entry")
	}
}

func TestConsumer_ShadowPVDeleted_NoError(t *testing.T) {
	r, _ := newConsumerReconciler()

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent-spv"},
	})
	if err != nil {
		t.Fatalf("Reconcile should succeed for deleted ShadowPV: %v", err)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
