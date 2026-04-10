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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func createNamespaceWithAnnotations(t *testing.T, ctx context.Context, name string, annotations map[string]string) {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
	}
	if err := testClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace %s: %v", name, err)
	}
}

func createDRPlanWithThrottle(t *testing.T, ctx context.Context, name, namespace string, selector map[string]string, waveLabel string, maxConcurrent int) *soteriav1alpha1.DRPlan {
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
			MaxConcurrentFailovers: maxConcurrent,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create DRPlan %s/%s: %v", namespace, name, err)
	}
	return plan
}

func TestDRPlanReconciler_NamespaceConsistency_VolumeGroupsFormed(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns-consistency"
	createNamespaceWithAnnotations(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	appLabels := map[string]string{"app.kubernetes.io/part-of": "erp-nsconsist"}

	createVM(t, ctx, "vm-ns-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-ns-b", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-ns-c", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))

	createDRPlan(t, ctx, "plan-ns-consist", ns, appLabels, "soteria.io/wave")

	plan, err := waitForWaveGroups(ctx, "plan-ns-consist", ns, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(plan.Status.Waves))
	}
	wave := plan.Status.Waves[0]
	if len(wave.Groups) != 1 {
		t.Fatalf("Wave groups = %d, want 1 (single namespace-level group)", len(wave.Groups))
	}
	g := wave.Groups[0]
	if g.ConsistencyLevel != soteriav1alpha1.ConsistencyLevelNamespace {
		t.Errorf("Group level = %q, want namespace", g.ConsistencyLevel)
	}
	if len(g.VMNames) != 3 {
		t.Errorf("Group VMNames count = %d, want 3", len(g.VMNames))
	}
}

func TestDRPlanReconciler_VMConsistency_IndividualVolumeGroups(t *testing.T) {
	ctx := context.Background()
	ns := "test-vm-consistency"
	createNamespace(t, ctx, ns)

	appLabels := map[string]string{"app.kubernetes.io/part-of": "erp-vmconsist"}

	createVM(t, ctx, "vm-ind-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-ind-b", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))

	createDRPlan(t, ctx, "plan-vm-consist", ns, appLabels, "soteria.io/wave")

	plan, err := waitForWaveGroups(ctx, "plan-vm-consist", ns, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(plan.Status.Waves))
	}
	wave := plan.Status.Waves[0]
	if len(wave.Groups) != 2 {
		t.Fatalf("Wave groups = %d, want 2 (individual VM groups)", len(wave.Groups))
	}
	for _, g := range wave.Groups {
		if g.ConsistencyLevel != soteriav1alpha1.ConsistencyLevelVM {
			t.Errorf("Group %q level = %q, want vm", g.Name, g.ConsistencyLevel)
		}
		if len(g.VMNames) != 1 {
			t.Errorf("Group %q VMNames = %d, want 1", g.Name, len(g.VMNames))
		}
	}
}

func TestDRPlanReconciler_WaveConflict_ReadyFalse(t *testing.T) {
	ctx := context.Background()
	ns := "test-wave-conflict"
	createNamespaceWithAnnotations(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	appLabels := map[string]string{"app.kubernetes.io/part-of": "erp-conflict"}

	createVM(t, ctx, "vm-conflict-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-conflict-b", ns, merge(appLabels, map[string]string{"soteria.io/wave": "2"}))

	createDRPlan(t, ctx, "plan-conflict", ns, appLabels, "soteria.io/wave")

	plan, err := waitForConditionReason(ctx, "plan-conflict", ns, "Ready", "WaveConflict", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range plan.Status.Conditions {
		if c.Type == "Ready" {
			if c.Status != metav1.ConditionFalse {
				t.Errorf("Ready.Status = %v, want False", c.Status)
			}
			return
		}
	}
	t.Error("Ready condition not found")
}

func TestDRPlanReconciler_WaveConflictResolved_ReadyTrue(t *testing.T) {
	ctx := context.Background()
	ns := "test-conflict-resolved"
	createNamespaceWithAnnotations(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	appLabels := map[string]string{"app.kubernetes.io/part-of": "erp-resolve"}

	createVM(t, ctx, "vm-resolve-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	vm := createVM(t, ctx, "vm-resolve-b", ns, merge(appLabels, map[string]string{"soteria.io/wave": "2"}))

	createDRPlan(t, ctx, "plan-resolve", ns, appLabels, "soteria.io/wave")

	_, err := waitForConditionReason(ctx, "plan-resolve", ns, "Ready", "WaveConflict", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	vm.Labels["soteria.io/wave"] = "1"
	if err := testClient.Update(ctx, vm); err != nil {
		t.Fatalf("Failed to update VM labels: %v", err)
	}

	plan, err := waitForCondition(ctx, "plan-resolve", ns, "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.Waves) == 0 {
		t.Fatal("Expected at least one wave after resolution")
	}
	if len(plan.Status.Waves[0].Groups) == 0 {
		t.Error("Expected groups populated after conflict resolved")
	}
}

func TestDRPlanReconciler_NamespaceGroupExceedsThrottle_ReadyFalse(t *testing.T) {
	ctx := context.Background()
	ns := "test-throttle-exceed"
	createNamespaceWithAnnotations(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	appLabels := map[string]string{"app.kubernetes.io/part-of": "erp-throttle"}

	createVM(t, ctx, "vm-throttle-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-throttle-b", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-throttle-c", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))

	createDRPlanWithThrottle(t, ctx, "plan-throttle", ns, appLabels, "soteria.io/wave", 2)

	plan, err := waitForConditionReason(ctx, "plan-throttle", ns, "Ready", "NamespaceGroupExceedsThrottle", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range plan.Status.Conditions {
		if c.Type == "Ready" && c.Reason == "NamespaceGroupExceedsThrottle" {
			if c.Status != metav1.ConditionFalse {
				t.Errorf("Ready.Status = %v, want False", c.Status)
			}
			return
		}
	}
	t.Error("Expected NamespaceGroupExceedsThrottle condition")
}

func TestDRPlanReconciler_MixedConsistency_CorrectGrouping(t *testing.T) {
	ctx := context.Background()
	nsLevel := "test-mixed-ns"
	vmLevel := "test-mixed-vm"
	createNamespaceWithAnnotations(t, ctx, nsLevel, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})
	createNamespace(t, ctx, vmLevel)

	appLabels := map[string]string{"app.kubernetes.io/part-of": "erp-mixed"}

	createVM(t, ctx, "vm-mixed-ns-a", nsLevel, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-mixed-ns-b", nsLevel, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-mixed-vm-a", vmLevel, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-mixed-vm-b", vmLevel, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))

	createDRPlan(t, ctx, "plan-mixed", nsLevel, appLabels, "soteria.io/wave")

	plan, err := waitForWaveGroups(ctx, "plan-mixed", nsLevel, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(plan.Status.Waves))
	}
	wave := plan.Status.Waves[0]

	nsCount := 0
	vmCount := 0
	for _, g := range wave.Groups {
		if g.ConsistencyLevel == soteriav1alpha1.ConsistencyLevelNamespace {
			nsCount++
			if len(g.VMNames) != 2 {
				t.Errorf("Namespace group VMNames = %d, want 2", len(g.VMNames))
			}
		} else {
			vmCount++
		}
	}

	if nsCount != 1 {
		t.Errorf("Namespace-level groups = %d, want 1", nsCount)
	}
	if vmCount != 2 {
		t.Errorf("VM-level groups = %d, want 2", vmCount)
	}
}

func TestDRPlanReconciler_ChunkingPreview_NamespaceGroupIndivisible(t *testing.T) {
	ctx := context.Background()
	ns := "test-chunk-indivisible"
	createNamespaceWithAnnotations(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	appLabels := map[string]string{"app.kubernetes.io/part-of": "erp-chunk"}

	createVM(t, ctx, "vm-chunk-a", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-chunk-b", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))
	createVM(t, ctx, "vm-chunk-c", ns, merge(appLabels, map[string]string{"soteria.io/wave": "1"}))

	createDRPlanWithThrottle(t, ctx, "plan-chunk", ns, appLabels, "soteria.io/wave", 4)

	plan, err := waitForWaveGroups(ctx, "plan-chunk", ns, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1", len(plan.Status.Waves))
	}

	wave := plan.Status.Waves[0]
	if len(wave.Groups) != 1 {
		t.Fatalf("Wave groups = %d, want 1", len(wave.Groups))
	}
	g := wave.Groups[0]
	if len(g.VMNames) != 3 {
		t.Errorf("Namespace group has %d VMs, want 3 (indivisible)", len(g.VMNames))
	}

	// Verify Ready=True (plan is valid — group fits in maxConcurrent=4)
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		var updated soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: "plan-chunk", Namespace: ns}, &updated); err == nil {
			for _, c := range updated.Status.Conditions {
				if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
					return
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Error("Expected Ready=True for valid chunking")
}
