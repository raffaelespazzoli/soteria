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

package admission

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

func TestFindMatchingPlans(t *testing.T) {
	planERP := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: "default"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}
	planDB := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-db", Namespace: "default"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}
	planERPOtherNS := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: "other-ns"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}

	tests := []struct {
		name      string
		plans     []*soteriav1alpha1.DRPlan
		vmLabels  labels.Set
		exclude   *types.NamespacedName
		wantCount int
		wantNames []string
	}{
		{
			name:      "VM labels match no DRPlans",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planDB},
			vmLabels:  labels.Set{"app": "crm"},
			wantCount: 0,
		},
		{
			name:      "VM drplan label matches exactly one DRPlan",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planDB},
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-erp"},
			wantCount: 1,
			wantNames: []string{"plan-erp"},
		},
		{
			name:      "VM drplan label matches two DRPlans with the same name in different namespaces",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planERPOtherNS},
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-erp"},
			wantCount: 2,
		},
		{
			name:      "VM drplan label matches a DRPlan but it is excluded",
			plans:     []*soteriav1alpha1.DRPlan{planERP},
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-erp"},
			exclude:   &types.NamespacedName{Namespace: "default", Name: "plan-erp"},
			wantCount: 0,
		},
		{
			name:      "only plan whose name equals drplan label value matches",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planDB},
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-db"},
			wantCount: 1,
			wantNames: []string{"plan-db"},
		},
		{
			name:      "no DRPlans exist",
			plans:     nil,
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-erp"},
			wantCount: 0,
		},
		{
			name:      "VM with empty labels matches no plans",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planDB},
			vmLabels:  labels.Set{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildScheme()
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, p := range tt.plans {
				builder.WithObjects(p.DeepCopy())
			}

			checker := &ExclusivityChecker{Client: builder.Build()}
			result, err := checker.FindMatchingPlans(context.Background(), tt.vmLabels, tt.exclude)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.wantCount {
				t.Errorf("got %d matching plans, want %d: %v", len(result), tt.wantCount, result)
			}
			for _, wantName := range tt.wantNames {
				found := false
				for _, r := range result {
					if r.Name == wantName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected plan %q in results, got %v", wantName, result)
				}
			}
		})
	}
}

func TestCheckVMExclusivity(t *testing.T) {
	planERP := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: "default"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}
	planDB := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-db", Namespace: "default"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}
	planERPOtherNS := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: "other-ns"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}
	planERPThirdNS := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: "third-ns"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}

	tests := []struct {
		name      string
		plans     []*soteriav1alpha1.DRPlan
		vmLabels  labels.Set
		wantCount int
		wantMsg   string
	}{
		{
			name:      "VM matches 0 plans — no errors",
			plans:     []*soteriav1alpha1.DRPlan{planERP},
			vmLabels:  labels.Set{"app": "crm"},
			wantCount: 0,
		},
		{
			name:      "VM matches 1 plan — no errors",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planDB},
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-erp"},
			wantCount: 0,
		},
		{
			name:      "VM matches 2 plans — error listing both",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planERPOtherNS},
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-erp"},
			wantCount: 1,
			wantMsg:   "would belong to multiple DRPlans",
		},
		{
			name:      "VM matches 3 plans — error listing all three",
			plans:     []*soteriav1alpha1.DRPlan{planERP, planERPOtherNS, planERPThirdNS},
			vmLabels:  labels.Set{soteriav1alpha1.DRPlanLabel: "plan-erp"},
			wantCount: 1,
			wantMsg:   "would belong to multiple DRPlans",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildScheme()
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, p := range tt.plans {
				builder.WithObjects(p.DeepCopy())
			}

			checker := &ExclusivityChecker{Client: builder.Build()}
			errors, err := checker.CheckVMExclusivity(context.Background(), "test-vm", "default", tt.vmLabels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(errors) != tt.wantCount {
				t.Errorf("got %d errors, want %d: %v", len(errors), tt.wantCount, errors)
			}
			if tt.wantMsg != "" {
				found := false
				for _, e := range errors {
					if strings.Contains(e, tt.wantMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got %v", tt.wantMsg, errors)
				}
			}
		})
	}
}

func TestCheckDRPlanExclusivity(t *testing.T) {
	planERP := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: "default"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}
	planCRM := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-crm", Namespace: "default"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}

	tests := []struct {
		name          string
		candidatePlan *soteriav1alpha1.DRPlan
		discoveredVMs []engine.VMReference
		existingPlans []*soteriav1alpha1.DRPlan
		wantCount     int
		wantMsg       string
	}{
		{
			name: "all discovered VMs unique to this plan — no errors",
			candidatePlan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-new", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			discoveredVMs: []engine.VMReference{
				{Name: "crm-db", Namespace: "default", Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-new"}},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{planERP},
			wantCount:     0,
		},
		{
			name: "one discovered VM also matches another plan — one error",
			candidatePlan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-new", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			discoveredVMs: []engine.VMReference{
				{Name: "erp-db", Namespace: "default", Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp"}},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{planERP},
			wantCount:     1,
			wantMsg:       "already belongs to DRPlan",
		},
		{
			name: "multiple VMs each match different other plans — multiple errors",
			candidatePlan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-new", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			discoveredVMs: []engine.VMReference{
				{Name: "erp-db", Namespace: "default", Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp"}},
				{Name: "crm-db", Namespace: "default", Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-crm"}},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{planERP, planCRM},
			wantCount:     2,
		},
		{
			name: "discovered VMs match the plan being validated (self) — excluded, no errors",
			candidatePlan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			discoveredVMs: []engine.VMReference{
				{Name: "erp-db", Namespace: "default", Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp"}},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{planERP},
			wantCount:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildScheme()
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, p := range tt.existingPlans {
				builder.WithObjects(p.DeepCopy())
			}

			checker := &ExclusivityChecker{Client: builder.Build()}
			conflicts, err := checker.CheckDRPlanExclusivity(
				context.Background(), tt.candidatePlan, tt.discoveredVMs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(conflicts) != tt.wantCount {
				t.Errorf("got %d conflicts, want %d: %v", len(conflicts), tt.wantCount, conflicts)
			}
			if tt.wantMsg != "" {
				found := false
				for _, c := range conflicts {
					if strings.Contains(c, tt.wantMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected conflict containing %q, got %v", tt.wantMsg, conflicts)
				}
			}
		})
	}
}
