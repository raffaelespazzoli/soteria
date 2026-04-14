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
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

func buildVMScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	_ = kubevirtv1.AddToScheme(s)
	return s
}

func makeVMRequest(vm *kubevirtv1.VirtualMachine, op admissionv1.Operation) admission.Request {
	raw, _ := json.Marshal(vm)
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: op,
			Name:      vm.Name,
			Namespace: vm.Namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

func TestVMValidator_Exclusivity(t *testing.T) {
	planERP := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}
	planDB := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-db"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}

	tests := []struct {
		name        string
		vm          *kubevirtv1.VirtualMachine
		plans       []*soteriav1alpha1.DRPlan
		op          admissionv1.Operation
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "VM CREATE matching 0 DRPlans — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-1", Namespace: "default",
					Labels: map[string]string{"app": "crm"},
				},
			},
			plans:       []*soteriav1alpha1.DRPlan{planERP, planDB},
			op:          admissionv1.Create,
			wantAllowed: true,
		},
		{
			name: "VM CREATE matching 1 DRPlan — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-1", Namespace: "default",
					Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp"},
				},
			},
			plans:       []*soteriav1alpha1.DRPlan{planERP, planDB},
			op:          admissionv1.Create,
			wantAllowed: true,
		},
		{
			name: "VM UPDATE removing labels — matches 0 plans — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-1", Namespace: "default",
					Labels: map[string]string{"unrelated": "true"},
				},
			},
			plans:       []*soteriav1alpha1.DRPlan{planERP, planDB},
			op:          admissionv1.Update,
			wantAllowed: true,
		},
		{
			name: "VM with no labels — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-1", Namespace: "default",
				},
			},
			plans:       []*soteriav1alpha1.DRPlan{planERP},
			op:          admissionv1.Create,
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildVMScheme()
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, p := range tt.plans {
				builder.WithObjects(p.DeepCopy())
			}
			fakeClient := builder.Build()

			checker := &ExclusivityChecker{Client: fakeClient}
			v := &VMValidator{
				ExclusivityChecker: checker,
				NSLookup:           &mockNSLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{}},
				Client:             fakeClient,
				VMDiscoverer:       &mockVMDiscoverer{},
				decoder:            admission.NewDecoder(scheme),
			}

			resp := v.Handle(context.Background(), makeVMRequest(tt.vm, tt.op))
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

func TestVMValidator_WaveConflict(t *testing.T) {
	planERP := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}

	erpW1Labels := map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp", "wave": "1"}
	erpW2Labels := map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp", "wave": "2"}

	tests := []struct {
		name        string
		vm          *kubevirtv1.VirtualMachine
		plans       []*soteriav1alpha1.DRPlan
		nsLevels    map[string]soteriav1alpha1.ConsistencyLevel
		siblingVMs  map[string][]engine.VMReference
		op          admissionv1.Operation
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "VM in namespace without consistency annotation — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-1", Namespace: "default",
					Labels: map[string]string{
						soteriav1alpha1.DRPlanLabel: "plan-erp",
						"wave":                      "2",
					},
				},
			},
			plans:       []*soteriav1alpha1.DRPlan{planERP},
			nsLevels:    map[string]soteriav1alpha1.ConsistencyLevel{},
			wantAllowed: true,
		},
		{
			name: "VM in namespace-level namespace, wave matches siblings — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-new", Namespace: "erp-db",
					Labels: map[string]string{
						soteriav1alpha1.DRPlanLabel: "plan-erp",
						"wave":                      "1",
					},
				},
			},
			plans:    []*soteriav1alpha1.DRPlan{planERP},
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			siblingVMs: map[string][]engine.VMReference{
				"plan-erp": {
					{Name: "vm-existing", Namespace: "erp-db", Labels: erpW1Labels},
					{Name: "vm-new", Namespace: "erp-db", Labels: erpW1Labels},
				},
			},
			wantAllowed: true,
		},
		{
			name: "VM CREATE with conflicting wave in namespace-level namespace — denied",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-new", Namespace: "erp-db",
					Labels: map[string]string{
						soteriav1alpha1.DRPlanLabel: "plan-erp",
						"wave":                      "2",
					},
				},
			},
			plans:    []*soteriav1alpha1.DRPlan{planERP},
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			siblingVMs: map[string][]engine.VMReference{
				"plan-erp": {
					{Name: "vm-existing", Namespace: "erp-db", Labels: erpW1Labels},
					{Name: "vm-new", Namespace: "erp-db", Labels: erpW2Labels},
				},
			},
			wantAllowed: false,
			wantMessage: "wave label '2' conflicts",
		},
		{
			name: "VM UPDATE changing wave to conflict — denied",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-existing", Namespace: "erp-db",
					Labels: map[string]string{
						soteriav1alpha1.DRPlanLabel: "plan-erp",
						"wave":                      "2",
					},
				},
			},
			plans:    []*soteriav1alpha1.DRPlan{planERP},
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			siblingVMs: map[string][]engine.VMReference{
				"plan-erp": {
					{Name: "vm-existing", Namespace: "erp-db", Labels: erpW2Labels},
					{Name: "vm-other", Namespace: "erp-db", Labels: erpW1Labels},
				},
			},
			op:          admissionv1.Update,
			wantAllowed: false,
			wantMessage: "wave label '2' conflicts",
		},
		{
			name: "VM in namespace-level namespace not matching any DRPlan — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-unrelated", Namespace: "erp-db",
					Labels: map[string]string{"app": "crm", "wave": "2"},
				},
			},
			plans:       []*soteriav1alpha1.DRPlan{planERP},
			nsLevels:    map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			wantAllowed: true,
		},
		{
			name: "only VM in namespace-level namespace under a plan — allowed",
			vm: &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vm-solo", Namespace: "erp-db",
					Labels: map[string]string{
						soteriav1alpha1.DRPlanLabel: "plan-erp",
						"wave":                      "1",
					},
				},
			},
			plans:    []*soteriav1alpha1.DRPlan{planERP},
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{"erp-db": soteriav1alpha1.ConsistencyLevelNamespace},
			siblingVMs: map[string][]engine.VMReference{
				"plan-erp": {
					{Name: "vm-solo", Namespace: "erp-db", Labels: erpW1Labels},
				},
			},
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := buildVMScheme()
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, p := range tt.plans {
				builder.WithObjects(p.DeepCopy())
			}
			fakeClient := builder.Build()

			discoverer := &mockVMDiscoverer{vms: tt.siblingVMs}
			checker := &ExclusivityChecker{Client: fakeClient}

			op := tt.op
			if op == "" {
				op = admissionv1.Create
			}

			v := &VMValidator{
				ExclusivityChecker: checker,
				NSLookup:           &mockNSLookup{levels: tt.nsLevels},
				Client:             fakeClient,
				VMDiscoverer:       discoverer,
				decoder:            admission.NewDecoder(scheme),
			}

			resp := v.Handle(context.Background(), makeVMRequest(tt.vm, op))
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

func TestVMValidator_DeleteAllowed(t *testing.T) {
	scheme := buildVMScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	v := &VMValidator{
		ExclusivityChecker: &ExclusivityChecker{Client: fakeClient},
		NSLookup:           &mockNSLookup{},
		Client:             fakeClient,
		VMDiscoverer:       &mockVMDiscoverer{},
		decoder:            admission.NewDecoder(scheme),
	}

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vm-1", Namespace: "default",
			Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp"},
		},
	}

	resp := v.Handle(context.Background(), makeVMRequest(vm, admissionv1.Delete))
	if !resp.Allowed {
		t.Errorf("expected DELETE to be allowed, got denied: %v", resp.Result)
	}
}

func TestVMValidator_WaveConflict_OnlyViolation(t *testing.T) {
	erpW1Labels := map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp", "wave": "1"}
	erpW2Labels := map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp", "wave": "2"}

	planERP := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 4,
		},
	}

	scheme := buildVMScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(planERP.DeepCopy()).Build()

	discoverer := &mockVMDiscoverer{vms: map[string][]engine.VMReference{
		"plan-erp": {
			{Name: "vm-conflict", Namespace: "erp-db", Labels: erpW2Labels},
			{Name: "vm-sibling", Namespace: "erp-db", Labels: erpW1Labels},
		},
	}}

	v := &VMValidator{
		ExclusivityChecker: &ExclusivityChecker{Client: fakeClient},
		NSLookup: &mockNSLookup{levels: map[string]soteriav1alpha1.ConsistencyLevel{
			"erp-db": soteriav1alpha1.ConsistencyLevelNamespace,
		}},
		Client:       fakeClient,
		VMDiscoverer: discoverer,
		decoder:      admission.NewDecoder(scheme),
	}

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vm-conflict", Namespace: "erp-db",
			Labels: map[string]string{soteriav1alpha1.DRPlanLabel: "plan-erp", "wave": "2"},
		},
	}

	resp := v.Handle(context.Background(), makeVMRequest(vm, admissionv1.Create))
	if resp.Allowed {
		t.Fatal("expected denied for wave conflict")
	}
	msg := resp.Result.Message
	if !strings.Contains(msg, "wave label") {
		t.Errorf("expected wave conflict error in message, got: %s", msg)
	}
}
