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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

type mockVMDiscoverer struct {
	vms map[string][]engine.VMReference
	err error
}

func (m *mockVMDiscoverer) DiscoverVMs(_ context.Context, selector metav1.LabelSelector) ([]engine.VMReference, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := selectorKey(selector)
	return m.vms[key], nil
}

func selectorKey(sel metav1.LabelSelector) string {
	b, _ := json.Marshal(sel)
	return string(b)
}

type mockNSLookup struct {
	levels map[string]soteriav1alpha1.ConsistencyLevel
	err    error
}

func (m *mockNSLookup) GetConsistencyLevel(_ context.Context, ns string) (soteriav1alpha1.ConsistencyLevel, error) {
	if m.err != nil {
		return "", m.err
	}
	if level, ok := m.levels[ns]; ok {
		return level, nil
	}
	return soteriav1alpha1.ConsistencyLevelVM, nil
}

func buildScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	return s
}

func makeRequest(plan *soteriav1alpha1.DRPlan, op admissionv1.Operation) admission.Request {
	raw, _ := json.Marshal(plan)
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: op,
			Name:      plan.Name,
			Namespace: plan.Namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

func TestDRPlanValidator_VMExclusivity(t *testing.T) {
	erpSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}}
	crmSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app": "crm"}}

	erpVMs := []engine.VMReference{
		{Name: "erp-db", Namespace: "default", Labels: map[string]string{"app": "erp", "wave": "1"}},
		{Name: "erp-app", Namespace: "default", Labels: map[string]string{"app": "erp", "wave": "1"}},
	}
	crmVMs := []engine.VMReference{
		{Name: "crm-db", Namespace: "default", Labels: map[string]string{"app": "crm", "wave": "1"}},
	}

	tests := []struct {
		name          string
		plan          *soteriav1alpha1.DRPlan
		existingPlans []*soteriav1alpha1.DRPlan
		op            admissionv1.Operation
		wantAllowed   bool
		wantMessage   string
	}{
		{
			name: "no existing plans — allowed",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			op:          admissionv1.Create,
			wantAllowed: true,
		},
		{
			name: "non-overlapping selector — allowed",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-b", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: crmSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{
				{ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"}, Spec: soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4}},
			},
			op:          admissionv1.Create,
			wantAllowed: true,
		},
		{
			name: "overlapping selector — denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-b", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{
				{ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"}, Spec: soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4}},
			},
			op:          admissionv1.Create,
			wantAllowed: false,
			wantMessage: "already belongs to DRPlan",
		},
		{
			name: "update same plan (self) — allowed",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{
				{ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"}, Spec: soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4}},
			},
			op:          admissionv1.Update,
			wantAllowed: true,
		},
		{
			name: "cluster-wide exclusivity — different namespace overlaps denied",
			plan: &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "plan-b", Namespace: "other-ns"},
				Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4},
			},
			existingPlans: []*soteriav1alpha1.DRPlan{
				{ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"}, Spec: soteriav1alpha1.DRPlanSpec{VMSelector: erpSelector, WaveLabel: "wave", MaxConcurrentFailovers: 4}},
			},
			op:          admissionv1.Create,
			wantAllowed: false,
			wantMessage: "already belongs to DRPlan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildScheme()
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, p := range tt.existingPlans {
				builder.WithObjects(p.DeepCopy())
			}
			fakeClient := builder.Build()

			discoverer := &mockVMDiscoverer{vms: map[string][]engine.VMReference{
				selectorKey(erpSelector): erpVMs,
				selectorKey(crmSelector): crmVMs,
			}}

			v := &DRPlanValidator{
				Client:       fakeClient,
				VMDiscoverer: discoverer,
				NSLookup:     &mockNSLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
				decoder:      admission.NewDecoder(scheme),
			}

			resp := v.Handle(context.Background(), makeRequest(tt.plan, tt.op))
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v; result: %s", resp.Allowed, tt.wantAllowed, resp.Result)
			}
			if tt.wantMessage != "" && (resp.Result == nil || !strings.Contains(resp.Result.Message, tt.wantMessage)) {
				msg := ""
				if resp.Result != nil {
					msg = resp.Result.Message
				}
				t.Errorf("expected message containing %q, got %q", tt.wantMessage, msg)
			}
		})
	}
}

func TestDRPlanValidator_NamespaceConsistency(t *testing.T) {
	sel := metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}}

	tests := []struct {
		name        string
		vms         []engine.VMReference
		nsLevels    map[string]soteriav1alpha1.ConsistencyLevel
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "no namespace-level annotation — allowed",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "2"}},
			},
			nsLevels:    map[string]soteriav1alpha1.ConsistencyLevel{},
			wantAllowed: true,
		},
		{
			name: "namespace-level, all same wave — allowed",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels:    map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			wantAllowed: true,
		},
		{
			name: "namespace-level, different wave — denied",
			vms: []engine.VMReference{
				{Name: "erp-db-1", Namespace: "erp-database", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "erp-db-2", Namespace: "erp-database", Labels: map[string]string{"app": "erp", "wave": "2"}},
			},
			nsLevels:    map[string]soteriav1alpha1.ConsistencyLevel{"erp-database": soteriav1alpha1.ConsistencyLevelNamespace},
			wantAllowed: false,
			wantMessage: "conflicting wave labels",
		},
		{
			name: "mixed: namespace-level conflict + VM-level no conflict — denied",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "ns-level", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "ns-level", Labels: map[string]string{"app": "erp", "wave": "2"}},
				{Name: "vm-3", Namespace: "vm-level", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels:    map[string]soteriav1alpha1.ConsistencyLevel{"ns-level": soteriav1alpha1.ConsistencyLevelNamespace},
			wantAllowed: false,
			wantMessage: "conflicting wave labels",
		},
		{
			name: "single VM in namespace-level namespace — allowed",
			vms: []engine.VMReference{
				{Name: "vm-solo", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels:    map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			discoverer := &mockVMDiscoverer{vms: map[string][]engine.VMReference{
				selectorKey(sel): tt.vms,
			}}

			v := &DRPlanValidator{
				Client:       fakeClient,
				VMDiscoverer: discoverer,
				NSLookup:     &mockNSLookup{levels: tt.nsLevels},
				decoder:      admission.NewDecoder(scheme),
			}

			plan := &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "test-plan", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 10},
			}

			resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Create))
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v; result: %v", resp.Allowed, tt.wantAllowed, resp.Result)
			}
			if tt.wantMessage != "" && (resp.Result == nil || !strings.Contains(resp.Result.Message, tt.wantMessage)) {
				msg := ""
				if resp.Result != nil {
					msg = resp.Result.Message
				}
				t.Errorf("expected message containing %q, got %q", tt.wantMessage, msg)
			}
		})
	}
}

func TestDRPlanValidator_MaxConcurrentCapacity(t *testing.T) {
	sel := metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}}

	tests := []struct {
		name               string
		vms                []engine.VMReference
		nsLevels           map[string]soteriav1alpha1.ConsistencyLevel
		maxConcurrent      int
		wantAllowed        bool
		wantMessage        string
	}{
		{
			name: "group size under limit — allowed",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-3", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels:      map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			maxConcurrent: 4,
			wantAllowed:   true,
		},
		{
			name: "group size exceeds limit — denied",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-3", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-4", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-5", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-6", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels:      map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			maxConcurrent: 4,
			wantAllowed:   false,
			wantMessage:   "maxConcurrentFailovers (4) is less than namespace+wave group size (6)",
		},
		{
			name: "group size exactly equals limit — allowed (boundary)",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-3", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-4", Namespace: "erp-db", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels:      map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			maxConcurrent: 4,
			wantAllowed:   true,
		},
		{
			name: "multiple groups, one exceeds — denied",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-3", Namespace: "ns-b", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-4", Namespace: "ns-b", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-5", Namespace: "ns-b", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{
				"ns-a": soteriav1alpha1.ConsistencyLevelNamespace,
				"ns-b": soteriav1alpha1.ConsistencyLevelNamespace,
			},
			maxConcurrent: 2,
			wantAllowed:   false,
			wantMessage:   "namespace+wave group size (3) for namespace ns-b",
		},
		{
			name: "all VM-level — allowed (individual VMs always fit)",
			vms: []engine.VMReference{
				{Name: "vm-1", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-2", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "1"}},
				{Name: "vm-3", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "1"}},
			},
			nsLevels:      map[string]soteriav1alpha1.ConsistencyLevel{},
			maxConcurrent: 1,
			wantAllowed:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			discoverer := &mockVMDiscoverer{vms: map[string][]engine.VMReference{
				selectorKey(sel): tt.vms,
			}}

			v := &DRPlanValidator{
				Client:       fakeClient,
				VMDiscoverer: discoverer,
				NSLookup:     &mockNSLookup{levels: tt.nsLevels},
				decoder:      admission.NewDecoder(scheme),
			}

			plan := &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{Name: "test-plan", Namespace: "default"},
				Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: tt.maxConcurrent},
			}

			resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Create))
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v; result: %v", resp.Allowed, tt.wantAllowed, resp.Result)
			}
			if tt.wantMessage != "" && (resp.Result == nil || !strings.Contains(resp.Result.Message, tt.wantMessage)) {
				msg := ""
				if resp.Result != nil {
					msg = resp.Result.Message
				}
				t.Errorf("expected message containing %q, got %q", tt.wantMessage, msg)
			}
		})
	}
}

func TestDRPlanValidator_AllowedAndEdgeCases(t *testing.T) {
	sel := metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}}

	t.Run("valid plan, no existing plans, no namespace-level — allowed", func(t *testing.T) {
		scheme := buildScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		vms := []engine.VMReference{
			{Name: "vm-1", Namespace: "default", Labels: map[string]string{"app": "erp", "wave": "1"}},
		}
		discoverer := &mockVMDiscoverer{vms: map[string][]engine.VMReference{
			selectorKey(sel): vms,
		}}

		v := &DRPlanValidator{
			Client:       fakeClient,
			VMDiscoverer: discoverer,
			NSLookup:     &mockNSLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
			decoder:      admission.NewDecoder(scheme),
		}

		plan := &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"},
			Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 4},
		}

		resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Create))
		if !resp.Allowed {
			t.Errorf("expected allowed, got denied: %v", resp.Result)
		}
	})

	t.Run("DELETE operation — allowed", func(t *testing.T) {
		scheme := buildScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		v := &DRPlanValidator{
			Client:       fakeClient,
			VMDiscoverer: &mockVMDiscoverer{},
			NSLookup:     &mockNSLookup{},
			decoder:      admission.NewDecoder(scheme),
		}

		plan := &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"},
			Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 4},
		}

		resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Delete))
		if !resp.Allowed {
			t.Errorf("expected allowed for DELETE, got denied: %v", resp.Result)
		}
	})

	t.Run("no discovered VMs — allowed", func(t *testing.T) {
		scheme := buildScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		discoverer := &mockVMDiscoverer{vms: map[string][]engine.VMReference{
			selectorKey(sel): {},
		}}

		v := &DRPlanValidator{
			Client:       fakeClient,
			VMDiscoverer: discoverer,
			NSLookup:     &mockNSLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
			decoder:      admission.NewDecoder(scheme),
		}

		plan := &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"},
			Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 4},
		}

		resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Create))
		if !resp.Allowed {
			t.Errorf("expected allowed for empty VM discovery, got denied: %v", resp.Result)
		}
	})
}

func TestDRPlanValidator_ErrorPaths(t *testing.T) {
	sel := metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}}
	erpVMs := []engine.VMReference{
		{Name: "erp-db", Namespace: "ns-a", Labels: map[string]string{"app": "erp", "wave": "1"}},
	}

	t.Run("VM discovery error returns 500", func(t *testing.T) {
		scheme := buildScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		v := &DRPlanValidator{
			Client:       fakeClient,
			VMDiscoverer: &mockVMDiscoverer{err: fmt.Errorf("connection refused")},
			NSLookup:     &mockNSLookup{},
			decoder:      admission.NewDecoder(scheme),
		}

		plan := &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"},
			Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 4},
		}

		resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Create))
		if resp.Allowed {
			t.Fatal("expected denied on VM discovery error")
		}
		if resp.Result == nil || resp.Result.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %v", resp.Result)
		}
	})

	t.Run("exclusivity VM discovery error for existing plan returns 500", func(t *testing.T) {
		scheme := buildScheme()
		existingPlan := &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-existing", Namespace: "default"},
			Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 4},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingPlan).Build()

		callCount := 0
		discoverer := &errorOnSecondCallDiscoverer{
			first: erpVMs,
			err:   fmt.Errorf("timeout"),
			count: &callCount,
		}

		v := &DRPlanValidator{
			Client:       fakeClient,
			VMDiscoverer: discoverer,
			NSLookup:     &mockNSLookup{},
			decoder:      admission.NewDecoder(scheme),
		}

		plan := &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-new", Namespace: "default"},
			Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 4},
		}

		resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Create))
		if resp.Allowed {
			t.Fatal("expected denied when exclusivity check hits a VM discovery error")
		}
		if resp.Result == nil || resp.Result.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %v", resp.Result)
		}
	})

	t.Run("namespace lookup error returns 500", func(t *testing.T) {
		scheme := buildScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		discoverer := &mockVMDiscoverer{vms: map[string][]engine.VMReference{
			selectorKey(sel): erpVMs,
		}}

		v := &DRPlanValidator{
			Client:       fakeClient,
			VMDiscoverer: discoverer,
			NSLookup:     &mockNSLookup{err: fmt.Errorf("namespace not found")},
			decoder:      admission.NewDecoder(scheme),
		}

		plan := &soteriav1alpha1.DRPlan{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: "default"},
			Spec:       soteriav1alpha1.DRPlanSpec{VMSelector: sel, WaveLabel: "wave", MaxConcurrentFailovers: 4},
		}

		resp := v.Handle(context.Background(), makeRequest(plan, admissionv1.Create))
		if resp.Allowed {
			t.Fatal("expected denied on namespace lookup error")
		}
		if resp.Result == nil || resp.Result.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %v", resp.Result)
		}
	})
}

// errorOnSecondCallDiscoverer returns VMs on the first call (for the candidate
// plan) and an error on subsequent calls (simulating failure when discovering
// VMs for an existing plan during the exclusivity check).
type errorOnSecondCallDiscoverer struct {
	first []engine.VMReference
	err   error
	count *int
}

func (d *errorOnSecondCallDiscoverer) DiscoverVMs(_ context.Context, _ metav1.LabelSelector) ([]engine.VMReference, error) {
	*d.count++
	if *d.count > 1 {
		return nil, d.err
	}
	return d.first, nil
}
