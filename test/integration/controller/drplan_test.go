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
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

const (
	testTimeout = 30 * time.Second
)

func createNamespace(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := testClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace %s: %v", name, err)
	}
}

func createVM(t *testing.T, ctx context.Context, name, namespace string, labels map[string]string) *kubevirtv1.VirtualMachine {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM %s/%s: %v", namespace, name, err)
	}
	return vm
}

func createDRPlan(t *testing.T, ctx context.Context, name, namespace string, selector map[string]string, waveLabel string) *soteriav1alpha1.DRPlan {
	t.Helper()
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: soteriav1alpha1.DRPlanSpec{
			VMSelector: metav1.LabelSelector{
				MatchLabels: selector,
			},
			WaveLabel:              waveLabel,
			MaxConcurrentFailovers: 5,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create DRPlan %s/%s: %v", namespace, name, err)
	}
	return plan
}

func TestDRPlanReconciler_DiscoverVMs_WavesPopulated(t *testing.T) {
	ctx := context.Background()
	ns := "test-discover"
	createNamespace(t, ctx, ns)

	appLabels := map[string]string{
		"app.kubernetes.io/part-of": "erp-discover",
	}

	createVM(t, ctx, "vm-w1-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-w1-b", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-w2-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "2"}))

	createDRPlan(t, ctx, "plan-discover", ns, appLabels, "soteria.io/wave")

	plan, err := waitForCondition(ctx, "plan-discover", ns, "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Status.DiscoveredVMCount != 3 {
		t.Errorf("DiscoveredVMCount = %d, want 3", plan.Status.DiscoveredVMCount)
	}
	if len(plan.Status.Waves) != 2 {
		t.Fatalf("len(Waves) = %d, want 2", len(plan.Status.Waves))
	}
	if plan.Status.Waves[0].WaveKey != "1" || plan.Status.Waves[1].WaveKey != "2" {
		t.Errorf("WaveKeys = [%q, %q], want [\"1\", \"2\"]",
			plan.Status.Waves[0].WaveKey, plan.Status.Waves[1].WaveKey)
	}
}

func TestDRPlanReconciler_NewVMAdded_WatchTriggersReconcile(t *testing.T) {
	ctx := context.Background()
	ns := "test-vm-add"
	createNamespace(t, ctx, ns)

	appLabels := map[string]string{
		"app.kubernetes.io/part-of": "erp-add",
	}

	createVM(t, ctx, "vm-initial", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createDRPlan(t, ctx, "plan-add", ns, appLabels, "soteria.io/wave")

	_, err := waitForVMCount(ctx, "plan-add", ns, 1, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	createVM(t, ctx, "vm-new", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))

	plan, err := waitForVMCount(ctx, "plan-add", ns, 2, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.Waves) != 1 {
		t.Errorf("len(Waves) = %d, want 1", len(plan.Status.Waves))
	}
}

func TestDRPlanReconciler_WaveLabelChanged_WatchTriggersReconcile(t *testing.T) {
	ctx := context.Background()
	ns := "test-wave-change"
	createNamespace(t, ctx, ns)

	appLabels := map[string]string{
		"app.kubernetes.io/part-of": "erp-wave",
	}

	vm := createVM(t, ctx, "vm-move", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-stay", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createDRPlan(t, ctx, "plan-wave", ns, appLabels, "soteria.io/wave")

	plan, err := waitForCondition(ctx, "plan-wave", ns, "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Status.Waves) != 1 {
		t.Fatalf("Initial waves count = %d, want 1", len(plan.Status.Waves))
	}

	vm.Labels["soteria.io/wave"] = "2"
	if err := testClient.Update(ctx, vm); err != nil {
		t.Fatalf("Failed to update VM labels: %v", err)
	}

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		var updated soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: "plan-wave", Namespace: ns}, &updated); err == nil {
			if len(updated.Status.Waves) == 2 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Timed out waiting for VM to move to wave 2")
}

func TestDRPlanReconciler_VMDeleted_WatchTriggersReconcile(t *testing.T) {
	ctx := context.Background()
	ns := "test-vm-delete"
	createNamespace(t, ctx, ns)

	appLabels := map[string]string{
		"app.kubernetes.io/part-of": "erp-del",
	}

	vm := createVM(t, ctx, "vm-delete-me", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-keep", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createDRPlan(t, ctx, "plan-delete", ns, appLabels, "soteria.io/wave")

	_, err := waitForVMCount(ctx, "plan-delete", ns, 2, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if err := testClient.Delete(ctx, vm); err != nil {
		t.Fatalf("Failed to delete VM: %v", err)
	}

	_, err = waitForVMCount(ctx, "plan-delete", ns, 1, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDRPlanReconciler_ReadyCondition_ReflectsDiscovery(t *testing.T) {
	ctx := context.Background()
	ns := "test-ready-cond"
	createNamespace(t, ctx, ns)

	createDRPlan(t, ctx, "plan-empty", ns,
		map[string]string{"app": "nonexistent"}, "soteria.io/wave")

	plan, err := waitForCondition(ctx, "plan-empty", ns, "Ready", metav1.ConditionFalse, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range plan.Status.Conditions {
		if c.Type == "Ready" {
			if c.Reason != "NoVMsDiscovered" {
				t.Errorf("Ready.Reason = %q, want NoVMsDiscovered", c.Reason)
			}
			return
		}
	}
	t.Error("Ready condition not found")
}

func TestDRPlanReconciler_50VMs_CompletesWithin10s(t *testing.T) {
	ctx := context.Background()
	ns := "test-perf"
	createNamespace(t, ctx, ns)

	appLabels := map[string]string{
		"app.kubernetes.io/part-of": "erp-perf",
	}

	for i := range 50 {
		wave := fmt.Sprintf("%d", (i%5)+1)
		createVM(t, ctx, fmt.Sprintf("vm-perf-%03d", i), ns,
			merge(appLabels, map[string]string{"soteria.io/wave": wave}))
	}

	start := time.Now()
	createDRPlan(t, ctx, "plan-perf", ns, appLabels, "soteria.io/wave")

	plan, err := waitForVMCount(ctx, "plan-perf", ns, 50, testTimeout)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}

	if elapsed > 10*time.Second {
		t.Errorf("Discovery took %v, exceeds NFR10 bound of 10s", elapsed)
	}
	t.Logf("50 VM discovery completed in %v", elapsed)

	if len(plan.Status.Waves) != 5 {
		t.Errorf("len(Waves) = %d, want 5", len(plan.Status.Waves))
	}
}

func merge(base, overlay map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}
