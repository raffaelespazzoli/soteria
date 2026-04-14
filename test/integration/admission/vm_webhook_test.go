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

func cleanupVM(t *testing.T, ctx context.Context, name, namespace string) {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	_ = testClient.Delete(ctx, vm)
}

func TestVMWebhook_Exclusivity_CreateMatchingTwoPlans_Rejected(t *testing.T) {
	ctx := context.Background()
	id := uniqueCounter()
	nsA := fmt.Sprintf("ns-a-%d", id)
	createTestNamespace(t, ctx, nsA, nil)

	planA := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-shared"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	planB := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-shared"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, planA); err != nil {
		t.Fatalf("Failed to create plan-shared in ns-a: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-shared")
	if err := testClient.Create(ctx, planB); err == nil {
		t.Fatal("Expected duplicate DRPlan name to be rejected, but it succeeded")
	} else if !errors.IsAlreadyExists(err) {
		t.Fatalf("Expected AlreadyExists for duplicate plan name, got: %v", err)
	}

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-both",
			Namespace: nsA,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-shared",
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Expected VM creation to succeed (single cluster-scoped plan), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-both", nsA)
}

func TestVMWebhook_Exclusivity_CreateMatchingOnePlan_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-excl-1plan-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-a"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan-a: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-a")

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-ok",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-a",
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Expected VM creation to succeed (matches one plan), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-ok", ns)
}

func TestVMWebhook_Exclusivity_UpdateAddsPlanLabel_OneClusterPlan_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-excl-upd-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-shared"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan-shared: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-shared")

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-update",
			Namespace: ns,
			Labels:    map[string]string{"unrelated": "true"},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-update", ns)

	var existing kubevirtv1.VirtualMachine
	if err := waitForObject(ctx, client.ObjectKey{Name: "vm-update", Namespace: ns}, &existing); err != nil {
		t.Fatalf("Failed to get VM: %v", err)
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[soteriav1alpha1.DRPlanLabel] = "plan-shared"
	existing.Labels["soteria.io/wave"] = "1"
	if err := testClient.Update(ctx, &existing); err != nil {
		t.Fatalf("Expected VM update to succeed (single cluster-scoped DRPlan), but failed: %v", err)
	}
}

func TestVMWebhook_Exclusivity_UpdateRemovesLabels_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-excl-rem-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-a"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-a")

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-remove",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-a",
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-remove", ns)

	var existing kubevirtv1.VirtualMachine
	if err := waitForObject(ctx, client.ObjectKey{Name: "vm-remove", Namespace: ns}, &existing); err != nil {
		t.Fatalf("Failed to get VM: %v", err)
	}
	existing.Labels = map[string]string{"unrelated": "true"}
	if err := testClient.Update(ctx, &existing); err != nil {
		t.Fatalf("Expected VM update to succeed (removed matching labels), but failed: %v", err)
	}
}

func TestVMWebhook_WaveConflict_CreateConflicting_Rejected(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-wave-cr-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-wave"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-wave")

	vm1 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-wave-1",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-wave",
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
				soteriav1alpha1.DRPlanLabel: "plan-wave",
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

func TestVMWebhook_WaveConflict_UpdateChangesWave_Rejected(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-wave-upd-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-wave"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-wave")

	vm1 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-a",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-wave",
				"soteria.io/wave":           "1",
			},
		},
	}
	vm2 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-b",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-wave",
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm1); err != nil {
		t.Fatalf("Failed to create vm-a: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-a", ns)
	if err := testClient.Create(ctx, vm2); err != nil {
		t.Fatalf("Failed to create vm-b: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-b", ns)

	var existing kubevirtv1.VirtualMachine
	if err := waitForObject(ctx, client.ObjectKey{Name: "vm-a", Namespace: ns}, &existing); err != nil {
		t.Fatalf("Failed to get vm-a: %v", err)
	}
	existing.Labels["soteria.io/wave"] = "2"
	err := testClient.Update(ctx, &existing)
	if err == nil {
		t.Fatal("Expected VM update to be denied (wave conflict), but it succeeded")
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

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-wave"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-wave")

	vm1 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-a",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-wave",
				"soteria.io/wave":           "1",
			},
		},
	}
	vm2 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-b",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-wave",
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm1); err != nil {
		t.Fatalf("Failed to create vm-a: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-a", ns)
	if err := testClient.Create(ctx, vm2); err != nil {
		t.Fatalf("Expected vm-b creation to succeed (same wave), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-b", ns)
}

func TestVMWebhook_NoViolations_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("vm-ok-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-single"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, "plan-single")

	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-ok",
			Namespace: ns,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-single",
				"soteria.io/wave":           "1",
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Expected VM creation to succeed (no violations), but failed: %v", err)
	}
	defer cleanupVM(t, ctx, "vm-ok", ns)
}

// createTestNamespaceWithAnnotations creates a namespace with annotations for
// wave conflict integration tests. Uses an existing helper but adds the
// consistency-level annotation.
func ensureNamespaceAnnotation(t *testing.T, ctx context.Context, name string, annotations map[string]string) {
	t.Helper()
	var ns corev1.Namespace
	if err := testClient.Get(ctx, client.ObjectKey{Name: name}, &ns); err == nil {
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		for k, v := range annotations {
			ns.Annotations[k] = v
		}
		if err := testClient.Update(ctx, &ns); err != nil {
			t.Fatalf("Failed to update namespace annotations for %s: %v", name, err)
		}
		return
	}
	createTestNamespace(t, ctx, name, annotations)
}
