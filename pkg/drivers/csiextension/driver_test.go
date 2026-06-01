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
	"time"

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

func primaryLabels() map[string]string {
	return map[string]string{
		VolumeReplicationClassLabel:      testVRClass,
		VolumeGroupReplicationClassLabel: testVRClass + "-group",
		SiteRoleLabel:                    SiteRolePrimary,
		LabelDRPlan:                      "test-plan",
	}
}

func secondaryLabels() map[string]string {
	m := primaryLabels()
	m[SiteRoleLabel] = SiteRoleSecondary
	return m
}

const (
	testVGNameVM  = "vm-default-web01"
	testVGNameNS  = "ns-erp-db"
	testNamespace = "erp-db"
	testPVCLogs   = "logs"
	testPVCData   = "data"
	testVRClass   = "ceph-rbd"
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
		PVCNames:  []string{testPVCData},
		Labels:    primaryLabels(),
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
	if len(info.PVCNames) != 1 || info.PVCNames[0] != testPVCData {
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
	if vr.Spec.VolumeReplicationClass != testVRClass {
		t.Errorf("VolumeReplicationClass = %q, want %q", vr.Spec.VolumeReplicationClass, testVRClass)
	}
	if vr.Spec.ReplicationState != replicationv1alpha1.Primary {
		t.Errorf("ReplicationState = %q, want primary", vr.Spec.ReplicationState)
	}
	if vr.Spec.DataSource.Name != testPVCData {
		t.Errorf("DataSource.Name = %q, want %q", vr.Spec.DataSource.Name, testPVCData)
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
		PVCNames:  []string{testPVCData, "logs", "config"},
		Labels:    primaryLabels(),
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
	for _, name := range []string{testPVCData, "logs", "config"} {
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
		PVCNames:  []string{testPVCData},
		Labels:    secondaryLabels(),
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
		Labels:    primaryLabels(),
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
	if vgr.Spec.VolumeReplicationClassName != testVRClass {
		t.Errorf("VolumeReplicationClassName = %q, want %q",
			vgr.Spec.VolumeReplicationClassName, testVRClass)
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
		Labels:    secondaryLabels(),
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
		PVCNames:  []string{testPVCData, "logs"},
		Labels:    primaryLabels(),
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
		Labels:    primaryLabels(),
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
// CreateOrUpdate — state update tests (Story 13.2, AC5)
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_CreateOrUpdate_VR_UpdatesState(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{testPVCData},
		Labels:    primaryLabels(),
	}

	_, err := drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}

	// Verify initially primary.
	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if vrList.Items[0].Spec.ReplicationState != ReplicationStatePrimary {
		t.Fatalf("initial state = %q, want primary", vrList.Items[0].Spec.ReplicationState)
	}

	// Call again with secondary — should update state, not create duplicate.
	spec.Labels = secondaryLabels()
	_, err = drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("second CreateVolumeGroup with secondary: %v", err)
	}

	var vrList2 replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList2); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vrList2.Items) != 1 {
		t.Fatalf("expected 1 VR (no duplicate), got %d", len(vrList2.Items))
	}
	if vrList2.Items[0].Spec.ReplicationState != ReplicationStateSecondary {
		t.Errorf("updated state = %q, want secondary", vrList2.Items[0].Spec.ReplicationState)
	}
	// Immutable fields should remain unchanged.
	if vrList2.Items[0].Spec.VolumeReplicationClass != testVRClass {
		t.Errorf("VolumeReplicationClass changed: %q", vrList2.Items[0].Spec.VolumeReplicationClass)
	}
	if vrList2.Items[0].Spec.DataSource.Name != testPVCData {
		t.Errorf("DataSource.Name changed: %q", vrList2.Items[0].Spec.DataSource.Name)
	}
}

func TestCreateVolumeGroup_CreateOrUpdate_VGR_UpdatesState(t *testing.T) {
	drv := testDriver(t, makePVC("pvc-1"))
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameNS,
		Namespace: testNamespace,
		PVCNames:  []string{"pvc-1"},
		Labels:    primaryLabels(),
	}

	_, err := drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}

	// Verify initially primary.
	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := drv.client.List(ctx, &vgrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if vgrList.Items[0].Spec.ReplicationState != ReplicationStatePrimary {
		t.Fatalf("initial state = %q, want primary", vgrList.Items[0].Spec.ReplicationState)
	}

	// Call again with secondary — should update state, not create duplicate.
	spec.Labels = secondaryLabels()
	_, err = drv.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("second CreateVolumeGroup with secondary: %v", err)
	}

	var vgrList2 replicationv1alpha1.VolumeGroupReplicationList
	if err := drv.client.List(ctx, &vgrList2); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vgrList2.Items) != 1 {
		t.Fatalf("expected 1 VGR (no duplicate), got %d", len(vgrList2.Items))
	}
	if vgrList2.Items[0].Spec.ReplicationState != ReplicationStateSecondary {
		t.Errorf("updated state = %q, want secondary", vgrList2.Items[0].Spec.ReplicationState)
	}
	// Immutable fields should remain unchanged.
	if vgrList2.Items[0].Spec.VolumeGroupReplicationClassName != "ceph-rbd-group" {
		t.Errorf("VolumeGroupReplicationClassName changed: %q", vgrList2.Items[0].Spec.VolumeGroupReplicationClassName)
	}
	if vgrList2.Items[0].Spec.VolumeReplicationClassName != testVRClass {
		t.Errorf("VolumeReplicationClassName changed: %q", vgrList2.Items[0].Spec.VolumeReplicationClassName)
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
		PVCNames:  []string{testPVCData, "logs"},
		Labels:    primaryLabels(),
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
		Labels:    primaryLabels(),
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
		PVCNames:  []string{testPVCData, "logs"},
		Labels:    primaryLabels(),
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
		Labels:    primaryLabels(),
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
		PVCNames:  []string{testPVCData},
		Labels:    primaryLabels(),
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
		Labels:    primaryLabels(),
	})
	if err == nil {
		t.Fatal("expected error for empty PVCNames")
	}
}

// ---------------------------------------------------------------------------
// Create-or-update (AlreadyExists) test
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// StopReplication & SetSource — Table-driven state transition tests (12.4)
// ---------------------------------------------------------------------------

func TestStopReplication_StateTransitions(t *testing.T) {
	tests := []struct {
		name      string
		initial   replicationv1alpha1.ReplicationState
		wantState replicationv1alpha1.ReplicationState
	}{
		{"primary to secondary", ReplicationStatePrimary, ReplicationStateSecondary},
		{"secondary stays secondary", ReplicationStateSecondary, ReplicationStateSecondary},
		{"resync to secondary", ReplicationStateResync, ReplicationStateSecondary},
		{"unknown to secondary", replicationv1alpha1.ReplicationState("unknown"), ReplicationStateSecondary},
		{"empty to secondary", replicationv1alpha1.ReplicationState(""), ReplicationStateSecondary},
	}

	for _, tt := range tests {
		t.Run("VR/"+tt.name, func(t *testing.T) {
			vr := &replicationv1alpha1.VolumeReplication{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vgIDPrefix + testVGNameVM + "-data",
					Namespace: "default",
					Labels:    map[string]string{LabelVolumeGroup: testVGNameVM},
				},
				Spec: replicationv1alpha1.VolumeReplicationSpec{
					VolumeReplicationClass: testVRClass,
					ReplicationState:       tt.initial,
					DataSource: corev1.TypedLocalObjectReference{
						Kind: "PersistentVolumeClaim", Name: testPVCData,
					},
				},
			}

			drv := testDriver(t, vr)
			ctx := context.Background()

			if err := drv.StopReplication(ctx, vgIDFromNamespace("default", testVGNameVM)); err != nil {
				t.Fatalf("StopReplication: %v", err)
			}

			var got replicationv1alpha1.VolumeReplication
			if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &got); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Spec.ReplicationState != tt.wantState {
				t.Errorf("ReplicationState = %q, want %q", got.Spec.ReplicationState, tt.wantState)
			}
		})

		t.Run("VGR/"+tt.name, func(t *testing.T) {
			vgr := &replicationv1alpha1.VolumeGroupReplication{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vgIDPrefix + testVGNameNS,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelVolumeGroup: testVGNameNS},
				},
				Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
					VolumeGroupReplicationClassName: "ceph-rbd-group",
					VolumeReplicationClassName:      testVRClass,
					ReplicationState:                tt.initial,
				},
			}

			drv := testDriver(t, vgr)
			ctx := context.Background()

			if err := drv.StopReplication(ctx, vgIDFromNamespace(testNamespace, testVGNameNS)); err != nil {
				t.Fatalf("StopReplication: %v", err)
			}

			var got replicationv1alpha1.VolumeGroupReplication
			if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vgr), &got); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Spec.ReplicationState != tt.wantState {
				t.Errorf("ReplicationState = %q, want %q", got.Spec.ReplicationState, tt.wantState)
			}
		})
	}
}

func TestSetSource_StateTransitions(t *testing.T) {
	tests := []struct {
		name    string
		initial replicationv1alpha1.ReplicationState
	}{
		{"secondary to primary", ReplicationStateSecondary},
		{"resync to primary", ReplicationStateResync},
		{"already primary (idempotent)", ReplicationStatePrimary},
	}

	for _, tt := range tests {
		t.Run("VR/"+tt.name, func(t *testing.T) {
			vr := &replicationv1alpha1.VolumeReplication{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vgIDPrefix + testVGNameVM + "-data",
					Namespace: "default",
					Labels:    map[string]string{LabelVolumeGroup: testVGNameVM},
				},
				Spec: replicationv1alpha1.VolumeReplicationSpec{
					VolumeReplicationClass: testVRClass,
					ReplicationState:       tt.initial,
					DataSource: corev1.TypedLocalObjectReference{
						Kind: "PersistentVolumeClaim", Name: testPVCData,
					},
				},
			}

			drv := testDriver(t, vr)
			ctx := context.Background()

			if err := drv.SetSource(ctx, vgIDFromNamespace("default", testVGNameVM)); err != nil {
				t.Fatalf("SetSource: %v", err)
			}

			var got replicationv1alpha1.VolumeReplication
			if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &got); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Spec.ReplicationState != ReplicationStatePrimary {
				t.Errorf("ReplicationState = %q, want primary", got.Spec.ReplicationState)
			}
		})

		t.Run("VGR/"+tt.name, func(t *testing.T) {
			vgr := &replicationv1alpha1.VolumeGroupReplication{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vgIDPrefix + testVGNameNS,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelVolumeGroup: testVGNameNS},
				},
				Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
					VolumeGroupReplicationClassName: "ceph-rbd-group",
					VolumeReplicationClassName:      testVRClass,
					ReplicationState:                tt.initial,
				},
			}

			drv := testDriver(t, vgr)
			ctx := context.Background()

			if err := drv.SetSource(ctx, vgIDFromNamespace(testNamespace, testVGNameNS)); err != nil {
				t.Fatalf("SetSource: %v", err)
			}

			var got replicationv1alpha1.VolumeGroupReplication
			if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vgr), &got); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Spec.ReplicationState != ReplicationStatePrimary {
				t.Errorf("ReplicationState = %q, want primary", got.Spec.ReplicationState)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StopReplication — multi-PVC atomicity & idempotency (Story 12.4)
// ---------------------------------------------------------------------------

func TestStopReplication_MultiplePVCs_AllFlipped(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{testPVCData, "logs", "config"},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	vgID := vgIDFromNamespace("default", testVGNameVM)
	if err := drv.StopReplication(ctx, vgID); err != nil {
		t.Fatalf("StopReplication: %v", err)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vrList.Items) != 3 {
		t.Fatalf("expected 3 VRs, got %d", len(vrList.Items))
	}
	for _, vr := range vrList.Items {
		if vr.Spec.ReplicationState != ReplicationStateSecondary {
			t.Errorf("VR %s: state = %q, want secondary", vr.Name, vr.Spec.ReplicationState)
		}
	}
}

func TestStopReplication_DoubleCall_Idempotent(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{testPVCData},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	vgID := vgIDFromNamespace("default", testVGNameVM)
	if err := drv.StopReplication(ctx, vgID); err != nil {
		t.Fatalf("first StopReplication: %v", err)
	}
	if err := drv.StopReplication(ctx, vgID); err != nil {
		t.Fatalf("second StopReplication: %v", err)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, vr := range vrList.Items {
		if vr.Spec.ReplicationState != ReplicationStateSecondary {
			t.Errorf("VR %s: state = %q, want secondary (idempotent)", vr.Name, vr.Spec.ReplicationState)
		}
	}
}

// ---------------------------------------------------------------------------
// Error & edge-case tests (Story 12.4)
// ---------------------------------------------------------------------------

func TestStopReplication_NotFound(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	err := drv.StopReplication(ctx, "csi-ext-default/nonexistent")
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("StopReplication for nonexistent: got %v, want ErrVolumeGroupNotFound", err)
	}
}

func TestSetSource_NotFound(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	err := drv.SetSource(ctx, "csi-ext-default/nonexistent")
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("SetSource for nonexistent: got %v, want ErrVolumeGroupNotFound", err)
	}
}

func TestStopReplication_ContextCancelled(t *testing.T) {
	drv := testDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := drv.StopReplication(ctx, "csi-ext-default/test"); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestSetSource_ContextCancelled(t *testing.T) {
	drv := testDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := drv.SetSource(ctx, "csi-ext-default/test"); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Helper function tests (Story 12.4)
// ---------------------------------------------------------------------------

func TestCrSet_CurrentState(t *testing.T) {
	tests := []struct {
		name string
		set  crSet
		want replicationv1alpha1.ReplicationState
	}{
		{
			name: "VGR takes precedence",
			set: crSet{
				vgrs: []replicationv1alpha1.VolumeGroupReplication{
					{Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
						ReplicationState: ReplicationStateSecondary,
					}},
				},
			},
			want: ReplicationStateSecondary,
		},
		{
			name: "VR when no VGR",
			set: crSet{
				vrs: []replicationv1alpha1.VolumeReplication{
					{Spec: replicationv1alpha1.VolumeReplicationSpec{
						ReplicationState: ReplicationStatePrimary,
					}},
				},
			},
			want: ReplicationStatePrimary,
		},
		{
			name: "empty set",
			set:  crSet{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.set.currentState(); got != tt.want {
				t.Errorf("currentState() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Create-or-update (AlreadyExists) test
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// GetReplicationStatus — Role mapping tests (Story 12.5, AC1)
// ---------------------------------------------------------------------------

func TestGetReplicationStatus_RoleMapping(t *testing.T) {
	tests := []struct {
		name     string
		state    replicationv1alpha1.State
		wantRole drivers.VolumeRole
	}{
		{"Primary→Source", replicationv1alpha1.PrimaryState, drivers.RoleSource},
		{"Secondary→Target", replicationv1alpha1.SecondaryState, drivers.RoleTarget},
		{"Unknown→NonReplicated", replicationv1alpha1.UnknownState, drivers.RoleNonReplicated},
		{"empty→NonReplicated", "", drivers.RoleNonReplicated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapRole(tt.state); got != tt.wantRole {
				t.Errorf("mapRole(%q) = %q, want %q", tt.state, got, tt.wantRole)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — Health mapping tests (Story 12.5, AC2, AC7)
// ---------------------------------------------------------------------------

func conditionTrue(typ string) metav1.Condition {
	return metav1.Condition{Type: typ, Status: metav1.ConditionTrue}
}

func conditionFalse(typ string) metav1.Condition {
	return metav1.Condition{Type: typ, Status: metav1.ConditionFalse}
}

func TestGetReplicationStatus_HealthMapping(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		wantHealth drivers.ReplicationHealth
	}{
		{
			"Completed=True→Healthy",
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
			drivers.HealthHealthy,
		},
		{
			"Degraded=True→Degraded",
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionDegraded)},
			drivers.HealthDegraded,
		},
		{
			"Resyncing=True→Syncing",
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionResyncing)},
			drivers.HealthSyncing,
		},
		{
			"Degraded=True takes precedence over Completed=True",
			[]metav1.Condition{
				conditionTrue(replicationv1alpha1.ConditionCompleted),
				conditionTrue(replicationv1alpha1.ConditionDegraded),
			},
			drivers.HealthDegraded,
		},
		{
			"Resyncing=True takes precedence over Completed=True",
			[]metav1.Condition{
				conditionTrue(replicationv1alpha1.ConditionCompleted),
				conditionTrue(replicationv1alpha1.ConditionResyncing),
			},
			drivers.HealthSyncing,
		},
		{
			"Degraded=True takes precedence over Resyncing=True",
			[]metav1.Condition{
				conditionTrue(replicationv1alpha1.ConditionDegraded),
				conditionTrue(replicationv1alpha1.ConditionResyncing),
			},
			drivers.HealthDegraded,
		},
		{
			"all False→Unknown",
			[]metav1.Condition{
				conditionFalse(replicationv1alpha1.ConditionCompleted),
				conditionFalse(replicationv1alpha1.ConditionDegraded),
				conditionFalse(replicationv1alpha1.ConditionResyncing),
			},
			drivers.HealthUnknown,
		},
		{
			"empty conditions→Unknown",
			nil,
			drivers.HealthUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapHealth(tt.conditions); got != tt.wantHealth {
				t.Errorf("mapHealth() = %q, want %q", got, tt.wantHealth)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — Worst health aggregation (Story 12.5, AC4)
// ---------------------------------------------------------------------------

func TestWorstHealth(t *testing.T) {
	tests := []struct {
		name    string
		healths []drivers.ReplicationHealth
		want    drivers.ReplicationHealth
	}{
		{"all healthy", []drivers.ReplicationHealth{drivers.HealthHealthy, drivers.HealthHealthy}, drivers.HealthHealthy},
		{
			"healthy+degraded",
			[]drivers.ReplicationHealth{drivers.HealthHealthy, drivers.HealthDegraded},
			drivers.HealthDegraded,
		},
		{"healthy+syncing", []drivers.ReplicationHealth{drivers.HealthHealthy, drivers.HealthSyncing}, drivers.HealthSyncing},
		{
			"syncing+degraded",
			[]drivers.ReplicationHealth{drivers.HealthSyncing, drivers.HealthDegraded},
			drivers.HealthDegraded,
		},
		{"unknown worst", []drivers.ReplicationHealth{drivers.HealthHealthy, drivers.HealthUnknown}, drivers.HealthUnknown},
		{"single healthy", []drivers.ReplicationHealth{drivers.HealthHealthy}, drivers.HealthHealthy},
		{"empty defaults to healthy", []drivers.ReplicationHealth{}, drivers.HealthHealthy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := worstHealth(tt.healths); got != tt.want {
				t.Errorf("worstHealth() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — VR single-CR (Story 12.5, AC1-AC3)
// ---------------------------------------------------------------------------

func makeVRWithStatus(
	name, namespace, vgName string,
	state replicationv1alpha1.State,
	conditions []metav1.Condition,
	syncTime *metav1.Time,
) *replicationv1alpha1.VolumeReplication {
	return &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{LabelVolumeGroup: vgName},
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: testVRClass,
			ReplicationState:       ReplicationStatePrimary,
			DataSource: corev1.TypedLocalObjectReference{
				Kind: "PersistentVolumeClaim",
				Name: testPVCData,
			},
		},
		Status: replicationv1alpha1.VolumeReplicationStatus{
			State:        state,
			Conditions:   conditions,
			LastSyncTime: syncTime,
		},
	}
}

func TestGetReplicationStatus_VR_Healthy(t *testing.T) {
	syncTime := metav1.Now()
	vr := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-data", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		&syncTime,
	)

	drv := testDriver(t, vr)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace("default", testVGNameVM))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Errorf("Role = %q, want Source", status.Role)
	}
	if status.Health != drivers.HealthHealthy {
		t.Errorf("Health = %q, want Healthy", status.Health)
	}
	if status.LastSyncTime == nil {
		t.Fatal("LastSyncTime should not be nil")
	}
}

func TestGetReplicationStatus_VR_Degraded(t *testing.T) {
	const vgName = "vm-erp-db-app01"
	vr := makeVRWithStatus(
		vgIDPrefix+vgName+"-data", testNamespace, vgName,
		replicationv1alpha1.SecondaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionDegraded)},
		nil,
	)

	drv := testDriver(t, vr)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace(testNamespace, vgName))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleTarget {
		t.Errorf("Role = %q, want Target", status.Role)
	}
	if status.Health != drivers.HealthDegraded {
		t.Errorf("Health = %q, want Degraded", status.Health)
	}
	if status.LastSyncTime != nil {
		t.Errorf("LastSyncTime = %v, want nil", status.LastSyncTime)
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — VR aggregation (Story 12.5, AC4)
// ---------------------------------------------------------------------------

func TestGetReplicationStatus_VR_Aggregation_WorstHealthWins(t *testing.T) {
	now := metav1.Now()
	earlier := metav1.NewTime(now.Add(-10 * time.Minute))

	vr1 := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-data", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		&now,
	)
	vr2 := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-logs", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionDegraded)},
		&earlier,
	)
	vr2.Spec.DataSource.Name = testPVCLogs

	drv := testDriver(t, vr1, vr2)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace("default", testVGNameVM))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}

	if status.Role != drivers.RoleSource {
		t.Errorf("Role = %q, want Source (from first CR)", status.Role)
	}
	if status.Health != drivers.HealthDegraded {
		t.Errorf("Health = %q, want Degraded (worst wins)", status.Health)
	}
	if status.LastSyncTime == nil {
		t.Fatal("LastSyncTime should not be nil")
	}
	if status.LastSyncTime.Truncate(time.Second) != earlier.Truncate(time.Second) {
		t.Errorf("LastSyncTime = %v, want %v (oldest)", status.LastSyncTime, earlier.Time)
	}
}

func TestGetReplicationStatus_VR_Aggregation_ThreePVCs_MixedHealth(t *testing.T) {
	now := metav1.Now()
	earlier := metav1.NewTime(now.Add(-5 * time.Minute))
	earliest := metav1.NewTime(now.Add(-20 * time.Minute))

	vr1 := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-data", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		&now,
	)
	vr2 := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-logs", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionResyncing)},
		&earlier,
	)
	vr2.Spec.DataSource.Name = testPVCLogs
	vr3 := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-config", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		&earliest,
	)
	vr3.Spec.DataSource.Name = "config"

	drv := testDriver(t, vr1, vr2, vr3)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace("default", testVGNameVM))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Health != drivers.HealthSyncing {
		t.Errorf("Health = %q, want Syncing (worst of Healthy+Syncing+Healthy)", status.Health)
	}
	if status.LastSyncTime == nil || status.LastSyncTime.Truncate(time.Second) != earliest.Truncate(time.Second) {
		t.Errorf("LastSyncTime = %v, want %v (oldest)", status.LastSyncTime, earliest.Time)
	}
}

func TestGetReplicationStatus_VR_Aggregation_NilSyncTimes(t *testing.T) {
	vr1 := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-data", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		nil,
	)
	vr2 := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-logs", "default", testVGNameVM,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		nil,
	)
	vr2.Spec.DataSource.Name = testPVCLogs

	drv := testDriver(t, vr1, vr2)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace("default", testVGNameVM))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.LastSyncTime != nil {
		t.Errorf("LastSyncTime = %v, want nil (all sync times nil)", status.LastSyncTime)
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — VGR direct mapping (Story 12.5, AC5)
// ---------------------------------------------------------------------------

func makeVGRWithStatus(
	name, namespace, vgName string,
	state replicationv1alpha1.State,
	conditions []metav1.Condition,
	syncTime *metav1.Time,
) *replicationv1alpha1.VolumeGroupReplication {
	return &replicationv1alpha1.VolumeGroupReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{LabelVolumeGroup: vgName},
		},
		Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
			VolumeGroupReplicationClassName: "ceph-rbd-group",
			VolumeReplicationClassName:      testVRClass,
			ReplicationState:                ReplicationStatePrimary,
		},
		Status: replicationv1alpha1.VolumeGroupReplicationStatus{
			VolumeReplicationStatus: replicationv1alpha1.VolumeReplicationStatus{
				State:        state,
				Conditions:   conditions,
				LastSyncTime: syncTime,
			},
		},
	}
}

func TestGetReplicationStatus_VGR_Healthy(t *testing.T) {
	syncTime := metav1.Now()
	vgr := makeVGRWithStatus(
		vgIDPrefix+testVGNameNS, testNamespace, testVGNameNS,
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		&syncTime,
	)

	drv := testDriver(t, vgr)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace(testNamespace, testVGNameNS))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Errorf("Role = %q, want Source", status.Role)
	}
	if status.Health != drivers.HealthHealthy {
		t.Errorf("Health = %q, want Healthy", status.Health)
	}
	if status.LastSyncTime == nil {
		t.Fatal("LastSyncTime should not be nil")
	}
}

func TestGetReplicationStatus_VGR_Secondary_Syncing(t *testing.T) {
	vgr := makeVGRWithStatus(
		vgIDPrefix+testVGNameNS, testNamespace, testVGNameNS,
		replicationv1alpha1.SecondaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionResyncing)},
		nil,
	)

	drv := testDriver(t, vgr)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace(testNamespace, testVGNameNS))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleTarget {
		t.Errorf("Role = %q, want Target", status.Role)
	}
	if status.Health != drivers.HealthSyncing {
		t.Errorf("Health = %q, want Syncing", status.Health)
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — Not found (Story 12.5, AC6)
// ---------------------------------------------------------------------------

func TestGetReplicationStatus_VR_NotFound(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace("default", testVGNameVM))
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("GetReplicationStatus for nonexistent VR: got %v, want ErrVolumeGroupNotFound", err)
	}
}

func TestGetReplicationStatus_VGR_NotFound(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	_, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace(testNamespace, testVGNameNS))
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("GetReplicationStatus for nonexistent VGR: got %v, want ErrVolumeGroupNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — Empty status (Story 12.5, AC7)
// ---------------------------------------------------------------------------

func TestGetReplicationStatus_VR_EmptyStatus(t *testing.T) {
	vr := makeVRWithStatus(
		vgIDPrefix+testVGNameVM+"-data", "default", testVGNameVM,
		"", nil, nil,
	)

	drv := testDriver(t, vr)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace("default", testVGNameVM))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleNonReplicated {
		t.Errorf("Role = %q, want NonReplicated (empty state)", status.Role)
	}
	if status.Health != drivers.HealthUnknown {
		t.Errorf("Health = %q, want Unknown (no conditions)", status.Health)
	}
	if status.LastSyncTime != nil {
		t.Errorf("LastSyncTime = %v, want nil", status.LastSyncTime)
	}
}

func TestGetReplicationStatus_VGR_EmptyStatus(t *testing.T) {
	vgr := makeVGRWithStatus(
		vgIDPrefix+testVGNameNS, testNamespace, testVGNameNS,
		"", nil, nil,
	)

	drv := testDriver(t, vgr)
	ctx := context.Background()

	status, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace(testNamespace, testVGNameNS))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleNonReplicated {
		t.Errorf("Role = %q, want NonReplicated (empty state)", status.Role)
	}
	if status.Health != drivers.HealthUnknown {
		t.Errorf("Health = %q, want Unknown (no conditions)", status.Health)
	}
}

// ---------------------------------------------------------------------------
// GetReplicationStatus — Context cancellation (Story 12.5)
// ---------------------------------------------------------------------------

func TestGetReplicationStatus_ContextCancelled(t *testing.T) {
	drv := testDriver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := drv.GetReplicationStatus(ctx, vgIDFromNamespace("default", testVGNameVM))
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Create-or-update (AlreadyExists) test
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Finalizer tests (Story 13.3)
// ---------------------------------------------------------------------------

func TestFinalizerForSiteRole(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"primary explicit", map[string]string{SiteRoleLabel: SiteRolePrimary}, FinalizerSitePrimary},
		{"secondary", map[string]string{SiteRoleLabel: SiteRoleSecondary}, FinalizerSiteSecondary},
		{"missing label defaults to primary", map[string]string{}, FinalizerSitePrimary},
		{"nil map defaults to primary", nil, FinalizerSitePrimary},
		{"identity overrides role after failover", map[string]string{
			SiteRoleLabel:     SiteRoleSecondary,
			SiteIdentityLabel: SiteRolePrimary,
		}, FinalizerSitePrimary},
		{"identity secondary with role primary", map[string]string{
			SiteRoleLabel:     SiteRolePrimary,
			SiteIdentityLabel: SiteRoleSecondary,
		}, FinalizerSiteSecondary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := finalizerForSiteRole(tt.labels); got != tt.want {
				t.Errorf("finalizerForSiteRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func postFailoverPrimaryLabels() map[string]string {
	m := primaryLabels()
	m[SiteRoleLabel] = SiteRoleSecondary
	m[SiteIdentityLabel] = SiteRolePrimary
	return m
}

func TestCreateVolumeGroup_SiteFinalizer(t *testing.T) {
	tests := []struct {
		name          string
		multiVM       bool
		labels        map[string]string
		wantFinalizer string
	}{
		{"VR/primary", false, primaryLabels(), FinalizerSitePrimary},
		{"VR/secondary", false, secondaryLabels(), FinalizerSiteSecondary},
		{"VGR/primary", true, primaryLabels(), FinalizerSitePrimary},
		{"VGR/secondary", true, secondaryLabels(), FinalizerSiteSecondary},
		{"VR/post-failover primary identity", false, postFailoverPrimaryLabels(), FinalizerSitePrimary},
		{"VGR/post-failover primary identity", true, postFailoverPrimaryLabels(), FinalizerSitePrimary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			vgName := testVGNameVM
			ns := "default"
			pvcs := []string{testPVCData}
			if tt.multiVM {
				vgName = testVGNameNS
				ns = testNamespace
				pvcs = []string{"pvc-1"}
				objs = append(objs, makePVC("pvc-1"))
			}

			drv := testDriver(t, objs...)
			ctx := context.Background()

			_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
				Name: vgName, Namespace: ns, PVCNames: pvcs, Labels: tt.labels,
			})
			if err != nil {
				t.Fatalf("CreateVolumeGroup: %v", err)
			}

			var finalizers []string
			if tt.multiVM {
				var list replicationv1alpha1.VolumeGroupReplicationList
				if err := drv.client.List(ctx, &list); err != nil {
					t.Fatalf("List: %v", err)
				}
				if len(list.Items) != 1 {
					t.Fatalf("expected 1 VGR, got %d", len(list.Items))
				}
				finalizers = list.Items[0].Finalizers
			} else {
				var list replicationv1alpha1.VolumeReplicationList
				if err := drv.client.List(ctx, &list); err != nil {
					t.Fatalf("List: %v", err)
				}
				if len(list.Items) != 1 {
					t.Fatalf("expected 1 VR, got %d", len(list.Items))
				}
				finalizers = list.Items[0].Finalizers
			}

			found := false
			for _, f := range finalizers {
				if f == tt.wantFinalizer {
					found = true
				}
			}
			if !found {
				t.Errorf("missing %s finalizer; finalizers = %v", tt.wantFinalizer, finalizers)
			}
		})
	}
}

func TestCreateVolumeGroup_VR_Idempotent_FinalizerNotDuplicated(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{testPVCData},
		Labels:    primaryLabels(),
	}

	if _, err := drv.CreateVolumeGroup(ctx, spec); err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}
	if _, err := drv.CreateVolumeGroup(ctx, spec); err != nil {
		t.Fatalf("second CreateVolumeGroup: %v", err)
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := drv.client.List(ctx, &vrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	vr := vrList.Items[0]
	count := 0
	for _, f := range vr.Finalizers {
		if f == FinalizerSitePrimary {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 %s finalizer, got %d; finalizers = %v",
			FinalizerSitePrimary, count, vr.Finalizers)
	}
}

func TestCreateVolumeGroup_VGR_Idempotent_FinalizerNotDuplicated(t *testing.T) {
	drv := testDriver(t, makePVC("pvc-1"))
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameNS,
		Namespace: testNamespace,
		PVCNames:  []string{"pvc-1"},
		Labels:    primaryLabels(),
	}

	if _, err := drv.CreateVolumeGroup(ctx, spec); err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}
	if _, err := drv.CreateVolumeGroup(ctx, spec); err != nil {
		t.Fatalf("second CreateVolumeGroup: %v", err)
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := drv.client.List(ctx, &vgrList); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vgrList.Items) != 1 {
		t.Fatalf("expected 1 VGR, got %d", len(vgrList.Items))
	}
	vgr := vgrList.Items[0]
	count := 0
	for _, f := range vgr.Finalizers {
		if f == FinalizerSitePrimary {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 %s finalizer, got %d; finalizers = %v",
			FinalizerSitePrimary, count, vgr.Finalizers)
	}
}

func TestCreateVolumeGroup_PartialRetry_SkipsExisting(t *testing.T) {
	drv := testDriver(t)
	ctx := context.Background()

	spec := drivers.VolumeGroupSpec{
		Name:      testVGNameVM,
		Namespace: "default",
		PVCNames:  []string{testPVCData, "logs"},
		Labels:    primaryLabels(),
	}

	// Create one VR manually to simulate partial prior run.
	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vgIDPrefix + testVGNameVM + "-data",
			Namespace: "default",
			Labels:    map[string]string{LabelVolumeGroup: testVGNameVM},
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: testVRClass,
			ReplicationState:       replicationv1alpha1.Primary,
			DataSource:             corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: testPVCData},
		},
	}
	if err := drv.client.Create(ctx, vr); err != nil {
		t.Fatalf("pre-create VR: %v", err)
	}

	// CreateVolumeGroup should succeed (skip existing data VR, create logs VR).
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
