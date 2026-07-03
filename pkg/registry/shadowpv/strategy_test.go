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
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

// stubPlanGetter implements rest.Getter for testing OwnerReference logic.
type stubPlanGetter struct {
	plan *soteriav1alpha1.DRPlan
	err  error
}

func (s *stubPlanGetter) Get(_ context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.plan != nil && s.plan.Name == name {
		return s.plan, nil
	}
	return nil, fmt.Errorf("drplans %q not found", name)
}

const (
	testPlanName     = "erp-full-stack"
	unknownTimestamp = "<unknown>"
)

func newTestShadowPV(name, planLabel string) *soteriav1alpha1.ShadowPV {
	spv := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{
				{
					ClusterName: "east",
					PVName:      "pv-data-0",
					PV: corev1.PersistentVolumeSpec{
						Capacity: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						PersistentVolumeSource: corev1.PersistentVolumeSource{
							CSI: &corev1.CSIPersistentVolumeSource{
								Driver:       "rook-ceph.rbd.csi.ceph.com",
								VolumeHandle: "pool-east/pv-data-0",
							},
						},
					},
				},
			},
		},
	}
	if planLabel != "" {
		spv.Labels = map[string]string{drivers.LabelDRPlan: planLabel}
	}
	return spv
}

func TestStrategy_NamespaceScoped_ReturnsFalse(t *testing.T) {
	if Strategy.NamespaceScoped() {
		t.Error("ShadowPV strategy must be cluster-scoped (NamespaceScoped() == false)")
	}
}

func TestPrepareForCreate_InitializesStatus(t *testing.T) {
	spv := newTestShadowPV("spv-init", "plan-a")
	spv.Status = soteriav1alpha1.ShadowPVStatus{
		Conditions: []metav1.Condition{{Type: "Stale", Status: metav1.ConditionTrue}},
	}

	Strategy.PrepareForCreate(context.Background(), spv)

	if len(spv.Status.Conditions) != 0 {
		t.Errorf("expected status zeroed, got %d conditions", len(spv.Status.Conditions))
	}
	if spv.Generation != 1 {
		t.Errorf("expected Generation=1, got %d", spv.Generation)
	}
}

func TestPrepareForCreate_SetsOwnerReference(t *testing.T) {
	planUID := types.UID("plan-uid-shadow-123")
	Strategy.SetPlanStorage(&stubPlanGetter{
		plan: &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{
				Name: testPlanName,
				UID:  planUID,
			},
		},
	})
	t.Cleanup(func() { Strategy.SetPlanStorage(nil) })

	spv := newTestShadowPV("spv-owner", testPlanName)

	Strategy.PrepareForCreate(context.Background(), spv)

	if len(spv.OwnerReferences) != 1 {
		t.Fatalf("expected 1 OwnerReference, got %d", len(spv.OwnerReferences))
	}

	ref := spv.OwnerReferences[0]
	if ref.APIVersion != soteriav1alpha1.SchemeGroupVersion.String() {
		t.Errorf("expected APIVersion %q, got %q", soteriav1alpha1.SchemeGroupVersion.String(), ref.APIVersion)
	}
	if ref.Kind != "DRPlan" {
		t.Errorf("expected Kind %q, got %q", "DRPlan", ref.Kind)
	}
	if ref.Name != testPlanName {
		t.Errorf("expected Name %q, got %q", testPlanName, ref.Name)
	}
	if ref.UID != planUID {
		t.Errorf("expected UID %q, got %q", planUID, ref.UID)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Error("expected Controller=true on OwnerReference")
	}
	if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
		t.Error("expected BlockOwnerDeletion=true on OwnerReference")
	}
}

func TestPrepareForCreate_NilPlanGetter_NoOwnerReference(t *testing.T) {
	Strategy.SetPlanStorage(nil)

	spv := newTestShadowPV("spv-nil-getter", "plan-a")

	Strategy.PrepareForCreate(context.Background(), spv)

	if len(spv.OwnerReferences) != 0 {
		t.Errorf("expected no OwnerReferences when planGetter is nil, got %d", len(spv.OwnerReferences))
	}
	if spv.Generation != 1 {
		t.Errorf("expected Generation=1 despite nil getter, got %d", spv.Generation)
	}
}

func TestPrepareForCreate_PlanNotFound_NoOwnerReference(t *testing.T) {
	Strategy.SetPlanStorage(&stubPlanGetter{
		err: fmt.Errorf("drplans %q not found", "missing-plan"),
	})
	t.Cleanup(func() { Strategy.SetPlanStorage(nil) })

	spv := newTestShadowPV("spv-not-found", "missing-plan")

	Strategy.PrepareForCreate(context.Background(), spv)

	if len(spv.OwnerReferences) != 0 {
		t.Errorf("expected no OwnerReferences when plan not found, got %d", len(spv.OwnerReferences))
	}
}

func TestPrepareForCreate_NoDRPlanLabel_NoOwnerReference(t *testing.T) {
	Strategy.SetPlanStorage(&stubPlanGetter{
		plan: &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-a", UID: "uid-a"},
		},
	})
	t.Cleanup(func() { Strategy.SetPlanStorage(nil) })

	spv := newTestShadowPV("spv-no-label", "")

	Strategy.PrepareForCreate(context.Background(), spv)

	if len(spv.OwnerReferences) != 0 {
		t.Errorf("expected no OwnerReferences without drplan label, got %d", len(spv.OwnerReferences))
	}
}

func TestPrepareForUpdate_PreservesStatus(t *testing.T) {
	oldSPV := newTestShadowPV("spv-update", "plan-a")
	oldSPV.Status = soteriav1alpha1.ShadowPVStatus{
		Conditions: []metav1.Condition{
			{Type: "PVConflict", Status: metav1.ConditionTrue, Reason: "Duplicate"},
		},
	}

	newSPV := oldSPV.DeepCopy()
	newSPV.Status = soteriav1alpha1.ShadowPVStatus{}
	newSPV.Spec.PVs = append(newSPV.Spec.PVs, soteriav1alpha1.ShadowPVEntry{
		ClusterName: "west", PVName: "pv-data-1",
	})

	Strategy.PrepareForUpdate(context.Background(), newSPV, oldSPV)

	if len(newSPV.Status.Conditions) != 1 {
		t.Errorf("expected status preserved with 1 condition, got %d", len(newSPV.Status.Conditions))
	}
	if len(newSPV.Spec.PVs) != 2 {
		t.Errorf("expected spec to retain new entries, got %d PVs", len(newSPV.Spec.PVs))
	}
}

func TestPrepareForUpdate_PreservesOwnerReferences(t *testing.T) {
	planUID := types.UID("plan-uid-immutable-456")
	oldSPV := newTestShadowPV("spv-ownerref", "plan-a")
	oldSPV.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: soteriav1alpha1.SchemeGroupVersion.String(),
			Kind:       "DRPlan",
			Name:       "plan-a",
			UID:        planUID,
		},
	}

	newSPV := oldSPV.DeepCopy()
	newSPV.OwnerReferences = nil

	Strategy.PrepareForUpdate(context.Background(), newSPV, oldSPV)

	if len(newSPV.OwnerReferences) != 1 {
		t.Fatalf("expected OwnerReferences preserved, got %d", len(newSPV.OwnerReferences))
	}
	if newSPV.OwnerReferences[0].UID != planUID {
		t.Errorf("expected UID %q preserved, got %q", planUID, newSPV.OwnerReferences[0].UID)
	}
}

func TestGetAttrs_ReturnsLabels(t *testing.T) {
	spv := newTestShadowPV("spv-attrs", "erp-plan")

	lbls, flds, err := GetAttrs(spv)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if lbls[drivers.LabelDRPlan] != "erp-plan" {
		t.Errorf("expected label %s=erp-plan, got %q", drivers.LabelDRPlan, lbls[drivers.LabelDRPlan])
	}
	if flds["metadata.name"] != "spv-attrs" {
		t.Errorf("expected metadata.name=spv-attrs, got %q", flds["metadata.name"])
	}
}

func TestGetAttrs_WrongType_ReturnsError(t *testing.T) {
	wrong := &soteriav1alpha1.DRPlan{}
	_, _, err := GetAttrs(wrong)
	if err == nil {
		t.Error("GetAttrs should return an error for non-ShadowPV objects")
	}
}

func TestMatchShadowPV_UsesGetAttrs(t *testing.T) {
	pred := MatchShadowPV(nil, nil)
	if pred.GetAttrs == nil {
		t.Error("MatchShadowPV predicate must have GetAttrs set")
	}
}

func TestTableConvertor_SingleResource(t *testing.T) {
	spv := newTestShadowPV(testPlanName+"-vm-billing", testPlanName)
	spv.CreationTimestamp = metav1.NewTime(time.Now().Add(-2 * time.Hour))
	spv.Spec.PVs = append(spv.Spec.PVs, soteriav1alpha1.ShadowPVEntry{
		ClusterName: "west",
		PVName:      "pv-data-1",
	})

	tc := ShadowPVTableConvertor{}
	table, err := tc.ConvertToTable(context.Background(), spv, nil)
	if err != nil {
		t.Fatalf("ConvertToTable error: %v", err)
	}

	if len(table.ColumnDefinitions) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(table.ColumnDefinitions))
	}
	if len(table.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(table.Rows))
	}

	row := table.Rows[0]
	if row.Cells[0] != testPlanName+"-vm-billing" {
		t.Errorf("Name cell = %v, want %q", row.Cells[0], testPlanName+"-vm-billing")
	}
	if row.Cells[1] != testPlanName {
		t.Errorf("Plan cell = %v, want %q", row.Cells[1], testPlanName)
	}
	if row.Cells[2] != 2 {
		t.Errorf("PV Count cell = %v, want 2", row.Cells[2])
	}
	if row.Cells[3] == unknownTimestamp {
		t.Error("Age cell should not be <unknown> for a non-zero timestamp")
	}
}

func TestTableConvertor_List(t *testing.T) {
	list := &soteriav1alpha1.ShadowPVList{
		Items: []soteriav1alpha1.ShadowPV{
			*newTestShadowPV("spv-a", "plan-a"),
			*newTestShadowPV("spv-b", "plan-b"),
			*newTestShadowPV("spv-c", "plan-c"),
		},
	}

	tc := ShadowPVTableConvertor{}
	table, err := tc.ConvertToTable(context.Background(), list, nil)
	if err != nil {
		t.Fatalf("ConvertToTable error: %v", err)
	}

	if len(table.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(table.Rows))
	}

	for i, wantName := range []string{"spv-a", "spv-b", "spv-c"} {
		if table.Rows[i].Cells[0] != wantName {
			t.Errorf("row[%d] Name = %v, want %q", i, table.Rows[i].Cells[0], wantName)
		}
	}
}

func TestTableConvertor_ZeroTimestamp(t *testing.T) {
	spv := newTestShadowPV("spv-zero-ts", "plan-a")

	tc := ShadowPVTableConvertor{}
	table, err := tc.ConvertToTable(context.Background(), spv, nil)
	if err != nil {
		t.Fatalf("ConvertToTable error: %v", err)
	}

	if table.Rows[0].Cells[3] != unknownTimestamp {
		t.Errorf("expected %s for zero timestamp, got %v", unknownTimestamp, table.Rows[0].Cells[3])
	}
}

func TestStatusStrategy_PreservesSpec(t *testing.T) {
	oldSPV := newTestShadowPV("spv-status", "plan-a")
	newSPV := oldSPV.DeepCopy()
	newSPV.Spec.PVs = nil
	newSPV.Status.Conditions = []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue},
	}

	StatusStrategy.PrepareForUpdate(context.Background(), newSPV, oldSPV)

	if len(newSPV.Spec.PVs) != 1 {
		t.Errorf("expected spec preserved with 1 PV entry, got %d", len(newSPV.Spec.PVs))
	}
	if len(newSPV.Status.Conditions) != 1 {
		t.Errorf("expected status conditions retained, got %d", len(newSPV.Status.Conditions))
	}
}

func TestStatusStrategy_PreservesLabelsAndOwnerReferences(t *testing.T) {
	planUID := types.UID("plan-uid-status-789")
	oldSPV := newTestShadowPV("spv-status-linkage", "plan-a")
	oldSPV.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: soteriav1alpha1.SchemeGroupVersion.String(),
			Kind:       "DRPlan",
			Name:       "plan-a",
			UID:        planUID,
		},
	}

	newSPV := oldSPV.DeepCopy()
	newSPV.Labels = map[string]string{drivers.LabelDRPlan: "tampered-plan"}
	newSPV.OwnerReferences = nil
	newSPV.Status.Conditions = []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue},
	}

	StatusStrategy.PrepareForUpdate(context.Background(), newSPV, oldSPV)

	if newSPV.Labels[drivers.LabelDRPlan] != "plan-a" {
		t.Errorf("expected drplan label preserved as %q, got %q", "plan-a", newSPV.Labels[drivers.LabelDRPlan])
	}
	if len(newSPV.OwnerReferences) != 1 {
		t.Fatalf("expected OwnerReferences preserved, got %d", len(newSPV.OwnerReferences))
	}
	if newSPV.OwnerReferences[0].UID != planUID {
		t.Errorf("expected UID %q preserved, got %q", planUID, newSPV.OwnerReferences[0].UID)
	}
}

func TestValidateUpdate_RejectsDRPlanLabelChange(t *testing.T) {
	oldSPV := newTestShadowPV("spv-label-change", "plan-a")
	newSPV := oldSPV.DeepCopy()
	newSPV.Labels[drivers.LabelDRPlan] = "plan-b"

	errs := Strategy.ValidateUpdate(context.Background(), newSPV, oldSPV)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for drplan label change, got %d: %v", len(errs), errs)
	}
}
