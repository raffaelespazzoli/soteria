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
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

type warningCapture struct {
	mu       sync.Mutex
	warnings []string
}

func (w *warningCapture) HandleWarningHeader(_ int, _ string, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.warnings = append(w.warnings, text)
}

func newWarningClient(t *testing.T) (client.Client, *warningCapture) {
	t.Helper()
	capture := &warningCapture{}
	cfgCopy := rest.CopyConfig(testCfg)
	cfgCopy.WarningHandler = capture
	c, err := client.New(cfgCopy, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatalf("Failed to create warning-capturing client: %v", err)
	}
	return c, capture
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

func cleanupVM(t *testing.T, ctx context.Context, name, namespace string) {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	_ = testClient.Delete(ctx, vm)
}

func TestVMWebhook_PlanExists_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-exists-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	planName := fmt.Sprintf("plan-vm-exists-%d", uniqueCounter())
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, planName)

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-exists",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: planName,
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Expected VM creation to succeed (plan exists), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-exists", ns)
}

func TestVMWebhook_PlanNotFound_AllowedWithWarning(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-no-plan-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	warnClient, capture := newWarningClient(t)

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-no-plan",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-does-not-exist",
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := warnClient.Create(ctx, vm); err != nil {
		t.Fatalf("Expected VM creation to succeed with warning (plan not found), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-no-plan", ns)

	capture.mu.Lock()
	defer capture.mu.Unlock()
	found := false
	for _, w := range capture.warnings {
		if strings.Contains(w, "plan-does-not-exist") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected warning about missing plan 'plan-does-not-exist', got warnings: %v", capture.warnings)
	}
}

func TestVMWebhook_NoLabel_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-nolabel-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-nolabel",
			Namespace: ns,
			Labels:    map[string]string{"app": "frontend"},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Expected VM creation to succeed (no drplan label), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-nolabel", ns)
}

func TestVMWebhook_WaveConflict_Rejected(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-wave-cr-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	planName := fmt.Sprintf("plan-wave-%d", uniqueCounter())
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, planName)

	vm1 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-wave-1",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: planName,
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm1); err != nil {
		t.Fatalf("Failed to create first VM: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-wave-1", ns)

	vm2 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-wave-2",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: planName,
				"soteria.io/wave":           "2",
			},
		},
	}
	err := testClient.Create(ctx, vm2)
	if err == nil {
		defer cleanupVM(t, ctx, "vm-wave-2", ns)
		t.Fatal("Expected VM creation to be denied (wave conflict), but it succeeded")
	}
	if !strings.Contains(err.Error(), "wave label") {
		t.Errorf("Expected wave conflict error, got: %v", err)
	}
}

func TestVMWebhook_WaveConflict_SameWave_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-wave-ok-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	planName := fmt.Sprintf("plan-wave-ok-%d", uniqueCounter())
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, planName)

	vm1 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-a",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: planName,
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm1); err != nil {
		t.Fatalf("Failed to create vm-a: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-a", ns)

	vm2 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-b",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: planName,
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm2); err != nil {
		t.Fatalf("Expected vm-b creation to succeed (same wave), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-b", ns)
}

func TestVMWebhook_DELETE_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-del-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	planName := fmt.Sprintf("plan-del-vm-%d", uniqueCounter())
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, planName)

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-del",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: planName,
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}

	var existing kubevirtv1.VirtualMachine
	if err := waitForObject(ctx, client.ObjectKey{Name: "vm-del", Namespace: ns}, &existing); err != nil {
		t.Fatalf("Failed to get VM: %v", err)
	}
	if err := testClient.Delete(ctx, &existing); err != nil {
		t.Fatalf("Expected VM DELETE to succeed, but failed: %v", err)
	}
}
