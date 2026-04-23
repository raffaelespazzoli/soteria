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

package controller_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestIntegration_UnprotectedVMs_DetectedOnReconcile(t *testing.T) {
	ctx := context.Background()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unprotected-test"}}
	if err := testClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, ns)
	})

	plan := newTestDRPlan("unprotected-plan", "dc-west", "dc-east")
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create DRPlan: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, plan)
	})

	protectedVM := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-protected",
			Namespace: "unprotected-test",
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "unprotected-plan",
				"soteria.io/wave":          "1",
			},
		},
	}
	if err := testClient.Create(ctx, protectedVM); err != nil {
		t.Fatalf("Failed to create protected VM: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, protectedVM)
	})

	unprotectedVM := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-unprotected",
			Namespace: "unprotected-test",
			Labels:    map[string]string{"app": "standalone"},
		},
	}
	if err := testClient.Create(ctx, unprotectedVM); err != nil {
		t.Fatalf("Failed to create unprotected VM: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, unprotectedVM)
	})

	// Wait for at least 1 unprotected VM to be detected. The exact count
	// depends on other test VMs that may exist cluster-wide, so we check
	// that the count is >= 1 and that the protected VM is discovered.
	_, err := waitForCondition(ctx, "unprotected-plan", "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatalf("DRPlan did not become Ready: %v", err)
	}

	var updated soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: "unprotected-plan"}, &updated); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if updated.Status.DiscoveredVMCount != 1 {
		t.Errorf("DiscoveredVMCount = %d, want 1 (only protected VM)", updated.Status.DiscoveredVMCount)
	}

	if updated.Status.UnprotectedVMCount < 1 {
		t.Errorf("UnprotectedVMCount = %d, want >= 1", updated.Status.UnprotectedVMCount)
	}
}

func TestIntegration_UnprotectedVMs_LabelAddedDecreases(t *testing.T) {
	ctx := context.Background()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unprotected-label-test"}}
	if err := testClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, ns)
	})

	plan := newTestDRPlan("label-change-plan", "dc-west", "dc-east")
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create DRPlan: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, plan)
	})

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-will-be-protected",
			Namespace: "unprotected-label-test",
			Labels:    map[string]string{"app": "test"},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, vm)
	})

	// Wait for initial reconcile — VM is unprotected.
	_, err := waitForCondition(ctx, "label-change-plan", "", "Ready", metav1.ConditionFalse, testTimeout)
	if err != nil {
		t.Fatalf("DRPlan should be Ready=False (no protected VMs): %v", err)
	}

	var before soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: "label-change-plan"}, &before); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}
	initialUnprotected := before.Status.UnprotectedVMCount

	// Add the DRPlan label to the VM → it becomes protected.
	if err := testClient.Get(ctx, client.ObjectKey{Name: "vm-will-be-protected", Namespace: "unprotected-label-test"}, vm); err != nil {
		t.Fatalf("Failed to re-fetch VM: %v", err)
	}
	vm.Labels[soteriav1alpha1.DRPlanLabel] = "label-change-plan"
	vm.Labels["soteria.io/wave"] = "1"
	if err := testClient.Update(ctx, vm); err != nil {
		t.Fatalf("Failed to add label to VM: %v", err)
	}

	// Wait for reconcile — VM is now protected, count should decrease.
	_, err = waitForVMCount(ctx, "label-change-plan", "", 1, testTimeout)
	if err != nil {
		t.Fatalf("DRPlan did not discover the labeled VM: %v", err)
	}

	var after soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: "label-change-plan"}, &after); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if after.Status.UnprotectedVMCount >= initialUnprotected && initialUnprotected > 0 {
		t.Errorf("UnprotectedVMCount should have decreased: before=%d, after=%d",
			initialUnprotected, after.Status.UnprotectedVMCount)
	}
}

func TestIntegration_UnprotectedVMs_LabelRemovedIncreases(t *testing.T) {
	ctx := context.Background()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unprotected-remove-test"}}
	if err := testClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, ns)
	})

	plan := newTestDRPlan("remove-label-plan", "dc-west", "dc-east")
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create DRPlan: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, plan)
	})

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-will-lose-protection",
			Namespace: "unprotected-remove-test",
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "remove-label-plan",
				"soteria.io/wave":          "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}
	t.Cleanup(func() {
		_ = testClient.Delete(ctx, vm)
	})

	// Wait for Ready=True with 1 discovered VM.
	_, err := waitForVMCount(ctx, "remove-label-plan", "", 1, testTimeout)
	if err != nil {
		t.Fatalf("DRPlan did not discover the VM: %v", err)
	}

	var before soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: "remove-label-plan"}, &before); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}
	initialUnprotected := before.Status.UnprotectedVMCount

	// Remove the DRPlan label → VM becomes unprotected.
	if err := testClient.Get(ctx, client.ObjectKey{Name: "vm-will-lose-protection", Namespace: "unprotected-remove-test"}, vm); err != nil {
		t.Fatalf("Failed to re-fetch VM: %v", err)
	}
	delete(vm.Labels, soteriav1alpha1.DRPlanLabel)
	if err := testClient.Update(ctx, vm); err != nil {
		t.Fatalf("Failed to remove label from VM: %v", err)
	}

	// Wait for the discovered VM count to drop to 0 (label removed).
	_, err = waitForVMCount(ctx, "remove-label-plan", "", 0, testTimeout)
	if err != nil {
		t.Fatalf("DRPlan did not lose the VM: %v", err)
	}

	var after soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: "remove-label-plan"}, &after); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	if after.Status.UnprotectedVMCount <= initialUnprotected {
		t.Errorf("UnprotectedVMCount should have increased: before=%d, after=%d",
			initialUnprotected, after.Status.UnprotectedVMCount)
	}
}
