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

package csiextension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(replication): %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(corev1): %v", err)
	}
	return scheme
}

func testDriver(t *testing.T, objs ...client.Object) *Driver {
	t.Helper()
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		Build()
	return New(c)
}

func primaryLabels(vrClass string) map[string]string {
	return map[string]string{
		VolumeReplicationClassLabel:      vrClass,
		VolumeGroupReplicationClassLabel: vrClass + "-group",
		SiteRoleLabel:                    SiteRolePrimary,
		LabelDRPlan:                      "test-plan",
	}
}

func secondaryLabels(vrClass string) map[string]string {
	m := primaryLabels(vrClass)
	m[SiteRoleLabel] = SiteRoleSecondary
	return m
}

const (
	testVGNameVM  = "vm-default-web01"
	testVGNameNS  = "ns-erp-db"
	testNamespace = "erp-db"
)

func makePVC(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
	}
}

// ---------------------------------------------------------------------------
// Construction tests (from Story 12.2 — kept for continuity)
// ---------------------------------------------------------------------------

func TestNew_WithClient(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	drv := New(c)
	if drv == nil {
		t.Fatal("New() returned nil")
	}
	if drv.client == nil {
		t.Fatal("New() did not set client field")
	}
}

func TestNew_NilClient(t *testing.T) {
	drv := New(nil)
	if drv == nil {
		t.Fatal("New(nil) returned nil — should return a Driver even with nil client")
	}
}

// ---------------------------------------------------------------------------
// Constants tests (from Story 12.2)
// ---------------------------------------------------------------------------

func TestReplicationStateConstants(t *testing.T) {
	tests := []struct {
		name    string
		got     replicationv1alpha1.ReplicationState
		wantVal string
	}{
		{"Primary", ReplicationStatePrimary, "primary"},
		{"Secondary", ReplicationStateSecondary, "secondary"},
		{"Resync", ReplicationStateResync, "resync"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.wantVal {
				t.Errorf("ReplicationState%s = %q, want %q", tt.name, tt.got, tt.wantVal)
			}
		})
	}
}

func TestCRDTypes_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name          string
		obj           runtime.Object
		wantName      string
		wantState     replicationv1alpha1.ReplicationState
		classJSONKey  string
		wantClassName string
	}{
		{
			name: "VolumeReplication",
			obj: &replicationv1alpha1.VolumeReplication{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "replication.storage.openshift.io/v1alpha1",
					Kind:       "VolumeReplication",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vr",
					Namespace: "default",
				},
				Spec: replicationv1alpha1.VolumeReplicationSpec{
					VolumeReplicationClass: "ceph-rbd-replication",
					ReplicationState:       replicationv1alpha1.Primary,
				},
			},
			wantName:      "test-vr",
			wantState:     replicationv1alpha1.Primary,
			classJSONKey:  "volumeReplicationClass",
			wantClassName: "ceph-rbd-replication",
		},
		{
			name: "VolumeGroupReplication",
			obj: &replicationv1alpha1.VolumeGroupReplication{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "replication.storage.openshift.io/v1alpha1",
					Kind:       "VolumeGroupReplication",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vgr",
					Namespace: "default",
				},
				Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
					VolumeGroupReplicationClassName: "ceph-rbd-group-replication",
					ReplicationState:                replicationv1alpha1.Secondary,
				},
			},
			wantName:      "test-vgr",
			wantState:     replicationv1alpha1.Secondary,
			classJSONKey:  "volumeGroupReplicationClassName",
			wantClassName: "ceph-rbd-group-replication",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.obj)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map: %v", err)
			}

			meta, _ := raw["metadata"].(map[string]any)
			if got := meta["name"]; got != tt.wantName {
				t.Errorf("metadata.name = %v, want %q", got, tt.wantName)
			}

			spec, _ := raw["spec"].(map[string]any)
			if got := spec["replicationState"]; got != string(tt.wantState) {
				t.Errorf("spec.replicationState = %v, want %q", got, tt.wantState)
			}
			if got := spec[tt.classJSONKey]; got != tt.wantClassName {
				t.Errorf("spec.%s = %v, want %q", tt.classJSONKey, got, tt.wantClassName)
			}
		})
	}
}

func TestSchemeRegistration_VolumeReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}
	gvks, _, err := scheme.ObjectKinds(&replicationv1alpha1.VolumeReplication{})
	if err != nil {
		t.Fatalf("ObjectKinds(VolumeReplication) error: %v", err)
	}
	found := false
	for _, gvk := range gvks {
		if gvk.Kind == "VolumeReplication" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected kind VolumeReplication in scheme, got %v", gvks)
	}
}

func TestSchemeRegistration_VolumeGroupReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}
	gvks, _, err := scheme.ObjectKinds(&replicationv1alpha1.VolumeGroupReplication{})
	if err != nil {
		t.Fatalf("ObjectKinds(VolumeGroupReplication) error: %v", err)
	}
	found := false
	for _, gvk := range gvks {
		if gvk.Kind == "VolumeGroupReplication" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected kind VolumeGroupReplication in scheme, got %v", gvks)
	}
}

func TestFakeClient_CRUD_VolumeReplication(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{Name: "crud-vr", Namespace: "default"},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: "test-class",
			ReplicationState:       replicationv1alpha1.Primary,
		},
	}

	if err := drv.client.Create(ctx, vr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var got replicationv1alpha1.VolumeReplication
	if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.ReplicationState != replicationv1alpha1.Primary {
		t.Errorf("Get replicationState = %q, want %q", got.Spec.ReplicationState, replicationv1alpha1.Primary)
	}

	got.Spec.ReplicationState = replicationv1alpha1.Secondary
	if err := drv.client.Update(ctx, &got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var updated replicationv1alpha1.VolumeReplication
	if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &updated); err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if updated.Spec.ReplicationState != replicationv1alpha1.Secondary {
		t.Errorf("Updated replicationState = %q, want %q", updated.Spec.ReplicationState, replicationv1alpha1.Secondary)
	}

	if err := drv.client.Delete(ctx, &updated); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var deleted replicationv1alpha1.VolumeReplication
	if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &deleted); err == nil {
		t.Fatal("Get after Delete: expected NotFound error, got nil")
	}
}

func TestVolumeReplicationClassLabel(t *testing.T) {
	if VolumeReplicationClassLabel == "" {
		t.Fatal("VolumeReplicationClassLabel must not be empty")
	}
}

func TestDriverName(t *testing.T) {
	if DriverName != "csi-extension" {
		t.Errorf("DriverName = %q, want %q", DriverName, "csi-extension")
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestVgIDFromNamespace(t *testing.T) {
	tests := []struct {
		namespace string
		name      string
		want      drivers.VolumeGroupID
	}{
		{"default", "vm-default-web01", "csi-ext-default/vm-default-web01"},
		{"erp-db", "ns-erp-db", "csi-ext-erp-db/ns-erp-db"},
	}
	for _, tt := range tests {
		if got := vgIDFromNamespace(tt.namespace, tt.name); got != tt.want {
			t.Errorf("vgIDFromNamespace(%q, %q) = %q, want %q", tt.namespace, tt.name, got, tt.want)
		}
	}
}

func TestParseVGID(t *testing.T) {
	tests := []struct {
		id            drivers.VolumeGroupID
		wantNamespace string
		wantName      string
	}{
		{"csi-ext-default/vm-default-web01", "default", "vm-default-web01"},
		{"csi-ext-erp-db/ns-erp-db", "erp-db", "ns-erp-db"},
		{"csi-ext-vm-legacy", "", "vm-legacy"},
	}
	for _, tt := range tests {
		ns, name := parseVGID(tt.id)
		if ns != tt.wantNamespace || name != tt.wantName {
			t.Errorf("parseVGID(%q) = (%q, %q), want (%q, %q)",
				tt.id, ns, name, tt.wantNamespace, tt.wantName)
		}
	}
}

func TestIsMultiVM(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"ns-erp-db", true},
		{"ns-web", true},
		{"vm-default-web01", false},
		{"vm-ns-something", false},
		{"other", false},
	}
	for _, tt := range tests {
		if got := isMultiVM(tt.name); got != tt.want {
			t.Errorf("isMultiVM(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestReplicationStateFromLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   replicationv1alpha1.ReplicationState
	}{
		{"primary explicit", map[string]string{SiteRoleLabel: SiteRolePrimary}, ReplicationStatePrimary},
		{"secondary", map[string]string{SiteRoleLabel: SiteRoleSecondary}, ReplicationStateSecondary},
		{"missing label defaults to primary", map[string]string{}, ReplicationStatePrimary},
		{"nil map defaults to primary", nil, ReplicationStatePrimary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replicationStateFromLabels(tt.labels); got != tt.want {
				t.Errorf("replicationStateFromLabels() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CreateVolumeGroup — Single-VM (VR) tests
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_SingleVM_OnePVC(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	info, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	wantID := drivers.VolumeGroupID("csi-ext-default/" + testVGNameVM)
	if info.ID != wantID {
		t.Errorf("ID = %q, want %q", info.ID, wantID)
	}
	if info.Name != testVGNameVM {
		t.Errorf("Name = %q, want %q", info.Name, testVGNameVM)
	}
	if len(info.PVCNames) != 1 || info.PVCNames[0] != "data" {
		t.Errorf("PVCNames = %v, want [data]", info.PVCNames)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List VRs: %v", err)
	}
	if len(vrList.Items) != 1 {
		t.Fatalf("expected 1 VR CR, got %d", len(vrList.Items))
	}

	vr := vrList.Items[0]
	if vr.Spec.VolumeReplicationClass != "ceph-rbd" {
		t.Errorf("VolumeReplicationClass = %q, want %q", vr.Spec.VolumeReplicationClass, "ceph-rbd")
	}
	if vr.Spec.ReplicationState != replicationv1alpha1.Primary {
		t.Errorf("ReplicationState = %q, want primary", vr.Spec.ReplicationState)
	}
	if vr.Spec.DataSource.Name != "data" {
		t.Errorf("DataSource.Name = %q, want %q", vr.Spec.DataSource.Name, "data")
	}
	if vr.Spec.DataSource.Kind != "PersistentVolumeClaim" {
		t.Errorf("DataSource.Kind = %q, want PersistentVolumeClaim", vr.Spec.DataSource.Kind)
	}
	if vr.Labels[LabelVolumeGroup] != testVGNameVM {
		t.Errorf("label %s = %q, want %q", LabelVolumeGroup, vr.Labels[LabelVolumeGroup], testVGNameVM)
	}
	if vr.Labels[LabelDRPlan] != "test-plan" {
		t.Errorf("label %s = %q, want %q", LabelDRPlan, vr.Labels[LabelDRPlan], "test-plan")
	}
}

func TestCreateVolumeGroup_SingleVM_ThreePVCs(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	info, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data", "logs", "config"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}
	if len(info.PVCNames) != 3 {
		t.Fatalf("PVCNames length = %d, want 3", len(info.PVCNames))
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List VRs: %v", err)
	}
	if len(vrList.Items) != 3 {
		t.Fatalf("expected 3 VR CRs, got %d", len(vrList.Items))
	}

	pvcSet := map[string]bool{}
	for _, vr := range vrList.Items {
		pvcSet[vr.Spec.DataSource.Name] = true
		if vr.Labels[LabelVolumeGroup] != testVGNameVM {
			t.Errorf("VR %s: label %s = %q", vr.Name, LabelVolumeGroup, vr.Labels[LabelVolumeGroup])
		}
	}
	for _, name := range []string{"data", "logs", "config"} {
		if !pvcSet[name] {
			t.Errorf("no VR found for PVC %q", name)
		}
	}
}

func TestCreateVolumeGroup_SingleVM_SecondaryState(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data"},
		Labels:    secondaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vrList.Items) != 1 {
		t.Fatalf("expected 1 VR, got %d", len(vrList.Items))
	}
	if vrList.Items[0].Spec.ReplicationState != replicationv1alpha1.Secondary {
		t.Errorf("ReplicationState = %q, want secondary", vrList.Items[0].Spec.ReplicationState)
	}
}

// ---------------------------------------------------------------------------
// CreateVolumeGroup — Multi-VM (VGR) tests
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_MultiVM_CreatesVGR(t *testing.T) {
	drv := testDriver(t,
		makePVC("pvc-1"),
		makePVC("pvc-2"),
		makePVC("pvc-3"),
	)
	ctx := context.Background()

	info, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameNS,
		Namespace: testNamespace,
		PVCNames:  []string{"pvc-1", "pvc-2", "pvc-3"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	wantID := drivers.VolumeGroupID("csi-ext-" + testNamespace + "/" + testVGNameNS)
	if info.ID != wantID {
		t.Errorf("ID = %q, want %q", info.ID, wantID)
	}
	if len(info.PVCNames) != 3 {
		t.Errorf("PVCNames length = %d, want 3", len(info.PVCNames))
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := drv.client.List(ctx, &vgrList); err != nil {
		t.Fatalf("List VGRs: %v", err)
	}
	if len(vgrList.Items) != 1 {
		t.Fatalf("expected 1 VGR, got %d", len(vgrList.Items))
	}

	vgr := vgrList.Items[0]
	if vgr.Spec.VolumeGroupReplicationClassName != "ceph-rbd-group" {
		t.Errorf("VolumeGroupReplicationClassName = %q, want %q",
			vgr.Spec.VolumeGroupReplicationClassName, "ceph-rbd-group")
	}
	if vgr.Spec.VolumeReplicationClassName != "ceph-rbd" {
		t.Errorf("VolumeReplicationClassName = %q, want %q",
			vgr.Spec.VolumeReplicationClassName, "ceph-rbd")
	}
	if vgr.Spec.ReplicationState != replicationv1alpha1.Primary {
		t.Errorf("ReplicationState = %q, want primary", vgr.Spec.ReplicationState)
	}
	if vgr.Labels[LabelVolumeGroup] != testVGNameNS {
		t.Errorf("label %s = %q, want %q", LabelVolumeGroup, vgr.Labels[LabelVolumeGroup], testVGNameNS)
	}
	if vgr.Labels[LabelDRPlan] != "test-plan" {
		t.Errorf("label %s = %q, want %q", LabelDRPlan, vgr.Labels[LabelDRPlan], "test-plan")
	}

	// No VR CRs should exist.
	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List VRs: %v", err)
	}
	if len(vrList.Items) != 0 {
		t.Errorf("expected 0 VR CRs for multi-VM group, got %d", len(vrList.Items))
	}

	// PVCs should be labeled for VGR source selector.
	for _, pvcName := range []string{"pvc-1", "pvc-2", "pvc-3"} {
		var pvc corev1.PersistentVolumeClaim
		if err := drv.client.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: pvcName}, &pvc); err != nil {
			t.Fatalf("Get PVC %s: %v", pvcName, err)
		}
		if pvc.Labels[LabelVolumeGroup] != testVGNameNS {
			t.Errorf("PVC %s label %s = %q, want %q",
				pvcName, LabelVolumeGroup, pvc.Labels[LabelVolumeGroup], testVGNameNS)
		}
	}
}

func TestCreateVolumeGroup_MultiVM_SecondaryState(t *testing.T) {
	drv := testDriver(t, makePVC("pvc-1"))
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameNS,
		Namespace: testNamespace,
		PVCNames:  []string{"pvc-1"},
		Labels:    secondaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := drv.client.List(ctx, &vgrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vgrList.Items) != 1 {
		t.Fatalf("expected 1 VGR, got %d", len(vgrList.Items))
	}
	if vgrList.Items[0].Spec.ReplicationState != replicationv1alpha1.Secondary {
		t.Errorf("ReplicationState = %q, want secondary", vgrList.Items[0].Spec.ReplicationState)
	}
}

// ---------------------------------------------------------------------------
// Idempotency tests
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_Idempotent_VR(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data", "logs"},
		Labels:    primaryLabels("ceph-rbd"),
	}

	info1, err := drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}

	info2, err := drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("second CreateVolumeGroup: %v", err)
	}

	if info1.ID != info2.ID {
		t.Errorf("IDs differ: %q vs %q", info1.ID, info2.ID)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List VRs: %v", err)
	}
	if len(vrList.Items) != 2 {
		t.Errorf("expected 2 VR CRs (unchanged after idempotent call), got %d", len(vrList.Items))
	}
}

func TestCreateVolumeGroup_Idempotent_VGR(t *testing.T) {
	drv := testDriver(t,
		makePVC("pvc-1"),
		makePVC("pvc-2"),
	)
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameNS,
		Namespace: testNamespace,
		PVCNames:  []string{"pvc-1", "pvc-2"},
		Labels:    primaryLabels("ceph-rbd"),
	}

	info1, err := drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}

	info2, err := drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("second CreateVolumeGroup: %v", err)
	}

	if info1.ID != info2.ID {
		t.Errorf("IDs differ: %q vs %q", info1.ID, info2.ID)
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := drv.client.List(ctx, &vgrList); err != nil {
		t.Fatalf("List VGRs: %v", err)
	}
	if len(vgrList.Items) != 1 {
		t.Errorf("expected 1 VGR CR (unchanged after idempotent call), got %d", len(vgrList.Items))
	}
}

// ---------------------------------------------------------------------------
// DeleteVolumeGroup tests
// ---------------------------------------------------------------------------

func TestDeleteVolumeGroup_VR(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data", "logs"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	if err := drv.DeleteVolumeGroup(ctx, vgIDFromNamespace("default", testVGNameVM)); err != nil {
		t.Fatalf("DeleteVolumeGroup: %v", err)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vrList.Items) != 0 {
		t.Errorf("expected 0 VR CRs after delete, got %d", len(vrList.Items))
	}
}

func TestDeleteVolumeGroup_VGR(t *testing.T) {
	drv := testDriver(t, makePVC("pvc-1"))
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameNS,
		Namespace: testNamespace,
		PVCNames:  []string{"pvc-1"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	if err := drv.DeleteVolumeGroup(ctx, vgIDFromNamespace(testNamespace, testVGNameNS)); err != nil {
		t.Fatalf("DeleteVolumeGroup: %v", err)
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := drv.client.List(ctx, &vgrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vgrList.Items) != 0 {
		t.Errorf("expected 0 VGR CRs after delete, got %d", len(vgrList.Items))
	}
}

func TestDeleteVolumeGroup_NotFound_ReturnsNil(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	if err := drv.DeleteVolumeGroup(ctx, "csi-ext-default/nonexistent"); err != nil {
		t.Errorf("DeleteVolumeGroup for nonexistent should return nil, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetVolumeGroup tests
// ---------------------------------------------------------------------------

func TestGetVolumeGroup_VR(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data", "logs"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	wantID := vgIDFromNamespace("default", testVGNameVM)
	info, err := drv.GetVolumeGroup(ctx, wantID)
	if err != nil {
		t.Fatalf("GetVolumeGroup: %v", err)
	}

	if info.ID != wantID {
		t.Errorf("ID = %q, want %q", info.ID, wantID)
	}
	if info.Name != testVGNameVM {
		t.Errorf("Name = %q, want %q", info.Name, testVGNameVM)
	}
	if len(info.PVCNames) != 2 {
		t.Errorf("PVCNames length = %d, want 2", len(info.PVCNames))
	}
}

func TestGetVolumeGroup_VGR(t *testing.T) {
	drv := testDriver(t,
		makePVC("pvc-1"),
		makePVC("pvc-2"),
	)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameNS,
		Namespace: testNamespace,
		PVCNames:  []string{"pvc-1", "pvc-2"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	wantID := vgIDFromNamespace(testNamespace, testVGNameNS)
	info, err := drv.GetVolumeGroup(ctx, wantID)
	if err != nil {
		t.Fatalf("GetVolumeGroup: %v", err)
	}

	if info.ID != wantID {
		t.Errorf("ID = %q, want %q", info.ID, wantID)
	}
	if info.Name != testVGNameNS {
		t.Errorf("Name = %q, want %q", info.Name, testVGNameNS)
	}
	if len(info.PVCNames) != 2 {
		t.Errorf("PVCNames length = %d, want 2", len(info.PVCNames))
	}
}

func TestGetVolumeGroup_NotFound(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.GetVolumeGroup(ctx, "csi-ext-default/nonexistent")
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("GetVolumeGroup for nonexistent: got %v, want ErrVolumeGroupNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation tests
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_ContextCancelled(t *testing.T) {
	drv := testDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data"},
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDeleteVolumeGroup_ContextCancelled(t *testing.T) {
	drv := testDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := drv.DeleteVolumeGroup(ctx, "csi-ext-default/test"); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestGetVolumeGroup_ContextCancelled(t *testing.T) {
	drv := testDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := drv.GetVolumeGroup(ctx, "csi-ext-default/test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Empty PVCNames validation test
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_EmptyPVCNames_ReturnsError(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  nil,
		Labels:    primaryLabels("ceph-rbd"),
	})
	if err == nil {
		t.Fatal("expected error for empty PVCNames")
	}
}

// ---------------------------------------------------------------------------
// Create-or-update (AlreadyExists) test
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_PartialRetry_SkipsExisting(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{"data", "logs"},
		Labels:    primaryLabels("ceph-rbd"),
	}

	// Create one VR manually to simulate partial prior run.
	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vgIDPrefix + testVGNameVM + "-data",
			Namespace: "default",
			Labels:    map[string]string{LabelVolumeGroup: testVGNameVM},
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: "ceph-rbd",
			ReplicationState:       replicationv1alpha1.Primary,
			DataSource:             corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "data"},
		},
	}
	if err := drv.client.Create(ctx, vr); err != nil {
		t.Fatalf("pre-create VR: %v", err)
	}

	// CreateVolumeGroup should succeed (skip existing "data" VR, create "logs" VR).
	info, err := drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("CreateVolumeGroup after partial: %v", err)
	}
	if len(info.PVCNames) != 2 {
		t.Errorf("PVCNames = %v, want 2 entries", info.PVCNames)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vrList.Items) != 2 {
		t.Errorf("expected 2 VR CRs total, got %d", len(vrList.Items))
	}
}
