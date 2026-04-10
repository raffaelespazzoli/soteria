//go:build integration

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

package admission_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func createTestNamespace(t *testing.T, ctx context.Context, name string, annotations map[string]string) {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
	}
	if err := testClient.Create(ctx, ns); err != nil && !errors.IsAlreadyExists(err) {
		t.Fatalf("Failed to create namespace %s: %v", name, err)
	}
}

func createTestVM(t *testing.T, ctx context.Context, name, namespace string, labels map[string]string) {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM %s/%s: %v", namespace, name, err)
	}
}

func cleanupDRPlan(t *testing.T, ctx context.Context, name, namespace string) {
	t.Helper()
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	_ = testClient.Delete(ctx, plan)
}

func TestDRPlanWebhook_VMExclusivity_Rejected(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("excl-reject-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	labels := map[string]string{"app": "erp", "soteria.io/wave": "1"}
	createTestVM(t, ctx, "erp-vm-1", ns, labels)
	createTestVM(t, ctx, "erp-vm-2", ns, labels)
	createTestVM(t, ctx, "erp-vm-3", ns, labels)

	planA := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-a", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, planA); err != nil {
		t.Fatalf("Failed to create plan-a: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-a", ns)

	planB := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-b", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	err := testClient.Create(ctx, planB)
	if err == nil {
		defer cleanupDRPlan(t, ctx, "plan-b", ns)
		t.Fatal("Expected plan-b creation to be denied, but it succeeded")
	}
	if !strings.Contains(err.Error(), "already belongs to DRPlan") {
		t.Errorf("Expected exclusivity error, got: %v", err)
	}
}

func TestDRPlanWebhook_VMExclusivity_NonOverlapping_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("excl-allow-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	createTestVM(t, ctx, "erp-vm-1", ns, map[string]string{"app": "erp", "soteria.io/wave": "1"})
	createTestVM(t, ctx, "crm-vm-1", ns, map[string]string{"app": "crm", "soteria.io/wave": "1"})

	planA := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-erp", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, planA); err != nil {
		t.Fatalf("Failed to create plan-erp: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-erp", ns)

	planB := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-crm", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "crm"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, planB); err != nil {
		t.Fatalf("Expected plan-crm creation to succeed, but failed: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-crm", ns)
}

func TestDRPlanWebhook_InvalidSelector_Rejected(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("bad-sel-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-selector", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "env", Operator: "InvalidOp", Values: []string{"prod"}},
				},
			},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
		},
	}
	err := testClient.Create(ctx, plan)
	if err == nil {
		defer cleanupDRPlan(t, ctx, "bad-selector", ns)
		t.Fatal("Expected creation with invalid selector to be denied, but it succeeded")
	}
	if !strings.Contains(err.Error(), "spec.vmSelector") {
		t.Errorf("Expected vmSelector validation error, got: %v", err)
	}
}

func TestDRPlanWebhook_WaveConflict_Rejected(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("wave-reject-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	createTestVM(t, ctx, "db-1", ns, map[string]string{"app": "erp", "soteria.io/wave": "1"})
	createTestVM(t, ctx, "db-2", ns, map[string]string{"app": "erp", "soteria.io/wave": "2"})

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "wave-conflict", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	err := testClient.Create(ctx, plan)
	if err == nil {
		defer cleanupDRPlan(t, ctx, "wave-conflict", ns)
		t.Fatal("Expected creation to be denied for wave conflict, but it succeeded")
	}
	if !strings.Contains(err.Error(), "conflicting wave labels") {
		t.Errorf("Expected wave conflict error, got: %v", err)
	}
}

func TestDRPlanWebhook_WaveConflict_SameWave_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("wave-allow-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	createTestVM(t, ctx, "db-1", ns, map[string]string{"app": "erp", "soteria.io/wave": "1"})
	createTestVM(t, ctx, "db-2", ns, map[string]string{"app": "erp", "soteria.io/wave": "1"})

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "wave-ok", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Expected creation to succeed for same-wave VMs, but failed: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "wave-ok", ns)
}

func TestDRPlanWebhook_MaxConcurrentExceeded_Rejected(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("maxconc-reject-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	for i := 1; i <= 6; i++ {
		createTestVM(t, ctx, fmt.Sprintf("vm-%d", i), ns,
			map[string]string{"app": "erp", "soteria.io/wave": "1"})
	}

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "max-exceeded", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
		},
	}
	err := testClient.Create(ctx, plan)
	if err == nil {
		defer cleanupDRPlan(t, ctx, "max-exceeded", ns)
		t.Fatal("Expected creation to be denied for maxConcurrentFailovers exceeded, but it succeeded")
	}
	if !strings.Contains(err.Error(), "maxConcurrentFailovers") {
		t.Errorf("Expected maxConcurrentFailovers error, got: %v", err)
	}
}

func TestDRPlanWebhook_ValidPlan_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("valid-plan-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	createTestVM(t, ctx, "vm-1", ns, map[string]string{"app": "valid", "soteria.io/wave": "1"})
	createTestVM(t, ctx, "vm-2", ns, map[string]string{"app": "valid", "soteria.io/wave": "1"})

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "valid-plan", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "valid"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Expected valid plan creation to succeed, but failed: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "valid-plan", ns)
}

func TestDRPlanWebhook_Update_ExclusivityExcludesSelf(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("update-self-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	createTestVM(t, ctx, "vm-1", ns, map[string]string{"app": "erp", "soteria.io/wave": "1"})

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-self", Namespace: ns},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": "erp"}},
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create initial plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-self", ns)

	// Re-fetch to get resource version
	var existing soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: "plan-self", Namespace: ns}, &existing); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	existing.Spec.MaxConcurrentFailovers = 8
	if err := testClient.Update(ctx, &existing); err != nil {
		t.Fatalf("Expected self-update to succeed, but failed: %v", err)
	}
}

var counter int

func uniqueCounter() int {
	counter++
	return counter
}
