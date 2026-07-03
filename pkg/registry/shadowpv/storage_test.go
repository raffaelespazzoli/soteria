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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

func TestStatusREST_New_ReturnsShadowPV(t *testing.T) {
	statusStore := &StatusREST{}
	obj := statusStore.New()

	if _, ok := obj.(*soteriav1alpha1.ShadowPV); !ok {
		t.Errorf("StatusREST.New() returned %T, want *ShadowPV", obj)
	}
}

func TestStatusREST_Destroy_DoesNotPanic(t *testing.T) {
	statusStore := &StatusREST{}
	statusStore.Destroy()
}

func TestShadowPV_LabelSelector_Matching(t *testing.T) {
	spvA := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "erp-full-stack-vm-billing",
			Labels: map[string]string{drivers.LabelDRPlan: "erp-full-stack"},
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{
				{ClusterName: "east", PVName: "pv-data-0"},
			},
		},
	}
	spvB := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "hr-portal-vm-hr",
			Labels: map[string]string{drivers.LabelDRPlan: "hr-portal"},
		},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{
				{ClusterName: "west", PVName: "pv-data-1"},
			},
		},
	}

	sel, err := labels.Parse(drivers.LabelDRPlan + "=erp-full-stack")
	if err != nil {
		t.Fatalf("parsing label selector: %v", err)
	}

	pred := MatchShadowPV(sel, fields.Everything())

	matchA, err := pred.Matches(spvA)
	if err != nil {
		t.Fatalf("matching spvA: %v", err)
	}
	if !matchA {
		t.Error("spvA should match label selector for erp-full-stack")
	}

	matchB, err := pred.Matches(spvB)
	if err != nil {
		t.Fatalf("matching spvB: %v", err)
	}
	if matchB {
		t.Error("spvB should not match label selector for erp-full-stack")
	}
}

func TestShadowPV_ValidationReject_EmptyPVs(t *testing.T) {
	spv := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{Name: "spv-invalid"},
		Spec:       soteriav1alpha1.ShadowPVSpec{PVs: []soteriav1alpha1.ShadowPVEntry{}},
	}

	errs := Strategy.Validate(context.Background(), spv)
	if len(errs) == 0 {
		t.Error("expected validation error for empty PVs, got none")
	}
}

func TestShadowPV_OwnerReference_CorrectUID(t *testing.T) {
	planUID := types.UID("plan-uid-storage-test-456")
	Strategy.SetPlanStorage(&stubPlanGetter{
		plan: &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{
				Name: "erp-full-stack",
				UID:  planUID,
			},
		},
	})
	t.Cleanup(func() { Strategy.SetPlanStorage(nil) })

	spv := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "erp-full-stack-vm-billing",
			Labels: map[string]string{drivers.LabelDRPlan: "erp-full-stack"},
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
					},
				},
			},
		},
	}

	Strategy.PrepareForCreate(context.Background(), spv)

	if len(spv.OwnerReferences) != 1 {
		t.Fatalf("expected 1 OwnerReference, got %d", len(spv.OwnerReferences))
	}
	ref := spv.OwnerReferences[0]
	if ref.UID != planUID {
		t.Errorf("expected UID %q, got %q", planUID, ref.UID)
	}
	if ref.Kind != "DRPlan" {
		t.Errorf("expected Kind DRPlan, got %q", ref.Kind)
	}
	if ref.Name != "erp-full-stack" {
		t.Errorf("expected Name erp-full-stack, got %q", ref.Name)
	}
}

func TestShadowPV_StatusSubresource_SpecPreserved(t *testing.T) {
	oldSPV := &soteriav1alpha1.ShadowPV{
		ObjectMeta: metav1.ObjectMeta{Name: "spv-status-sub"},
		Spec: soteriav1alpha1.ShadowPVSpec{
			PVs: []soteriav1alpha1.ShadowPVEntry{
				{ClusterName: "east", PVName: "pv-data-0"},
				{ClusterName: "west", PVName: "pv-data-1"},
			},
		},
	}

	newSPV := oldSPV.DeepCopy()
	newSPV.Spec.PVs = nil
	newSPV.Status.Conditions = []metav1.Condition{
		{
			Type:               "PVConflict",
			Status:             metav1.ConditionTrue,
			Reason:             "DuplicatePV",
			Message:            "PV pv-data-0 already exists on cluster west",
			LastTransitionTime: metav1.Now(),
		},
	}

	StatusStrategy.PrepareForUpdate(context.Background(), newSPV, oldSPV)

	if len(newSPV.Spec.PVs) != 2 {
		t.Errorf("expected spec preserved with 2 PV entries, got %d", len(newSPV.Spec.PVs))
	}
	if len(newSPV.Status.Conditions) != 1 {
		t.Errorf("expected 1 status condition, got %d", len(newSPV.Status.Conditions))
	}
}
