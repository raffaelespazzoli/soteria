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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func createPVC(t *testing.T, ctx context.Context, name, namespace, storageClass string) {
	t.Helper()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	if err := testClient.Create(ctx, pvc); err != nil {
		t.Fatalf("Failed to create PVC %s/%s: %v", namespace, name, err)
	}
}

func createVMWithPVC(t *testing.T, ctx context.Context, name, namespace string, labels map[string]string, pvcClaimName string) *kubevirtv1.VirtualMachine {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Volumes: []kubevirtv1.Volume{{
						Name: "rootdisk",
						VolumeSource: kubevirtv1.VolumeSource{
							PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
								PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcClaimName,
								},
							},
						},
					}},
				},
			},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM %s/%s: %v", namespace, name, err)
	}
	return vm
}

func TestDRPlanReconciler_Preflight_BasicComposition(t *testing.T) {
	ctx := context.Background()
	ns := "test-pf-basic"
	createNamespace(t, ctx, ns)

	createPVC(t, ctx, "vm-pf-1-root", ns, "test-odf")
	createPVC(t, ctx, "vm-pf-2-root", ns, "test-odf")
	createVMWithPVC(t, ctx, "vm-pf-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-basic", "soteria.io/wave": "1"}, "vm-pf-1-root")
	createVMWithPVC(t, ctx, "vm-pf-2", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-basic", "soteria.io/wave": "1"}, "vm-pf-2-root")

	createDRPlan(t, ctx, "plan-pf-basic", "soteria.io/wave")

	plan, err := waitForPreflight(ctx, "plan-pf-basic", "", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	pf := plan.Status.Preflight
	if pf.TotalVMs != 2 {
		t.Errorf("Preflight.TotalVMs = %d, want 2", pf.TotalVMs)
	}
	if len(pf.Waves) != 1 {
		t.Fatalf("Preflight.Waves = %d, want 1", len(pf.Waves))
	}
	if pf.Waves[0].VMCount != 2 {
		t.Errorf("Wave VMCount = %d, want 2", pf.Waves[0].VMCount)
	}
	for _, vm := range pf.Waves[0].VMs {
		if vm.StorageBackend != "odf" {
			t.Errorf("VM %s StorageBackend = %q, want odf", vm.Name, vm.StorageBackend)
		}
	}
	if len(pf.Waves[0].Chunks) == 0 {
		t.Error("Expected at least one chunk in wave")
	}
	if pf.GeneratedAt == nil {
		t.Error("GeneratedAt should not be nil")
	}
}

func TestDRPlanReconciler_Preflight_NamespaceConsistency(t *testing.T) {
	ctx := context.Background()
	ns := "test-pf-nscons"
	createNamespaceWithAnnotations(t, ctx, ns, map[string]string{
		soteriav1alpha1.ConsistencyAnnotation: "namespace",
	})

	createPVC(t, ctx, "vm-pfns-1-root", ns, "test-odf")
	createPVC(t, ctx, "vm-pfns-2-root", ns, "test-odf")
	createVMWithPVC(t, ctx, "vm-pfns-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-nscons", "soteria.io/wave": "1"}, "vm-pfns-1-root")
	createVMWithPVC(t, ctx, "vm-pfns-2", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-nscons", "soteria.io/wave": "1"}, "vm-pfns-2-root")

	createDRPlan(t, ctx, "plan-pf-nscons", "soteria.io/wave")

	plan, err := waitForPreflight(ctx, "plan-pf-nscons", "", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	pf := plan.Status.Preflight
	for _, vm := range pf.Waves[0].VMs {
		if vm.ConsistencyLevel != "namespace" {
			t.Errorf("VM %s ConsistencyLevel = %q, want namespace", vm.Name, vm.ConsistencyLevel)
		}
	}

	if len(pf.Waves[0].Chunks) > 0 {
		chunk := pf.Waves[0].Chunks[0]
		if chunk.VMCount != 2 {
			t.Errorf("Chunk VMCount = %d, want 2 (indivisible namespace group)", chunk.VMCount)
		}
	}
}

func TestDRPlanReconciler_Preflight_StorageBackendUnknown(t *testing.T) {
	ctx := context.Background()
	ns := "test-pf-unknown-sc"
	createNamespace(t, ctx, ns)

	createPVC(t, ctx, "vm-pfu-root", ns, "unlisted-storage-class")
	createVMWithPVC(t, ctx, "vm-pfu-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-unknown", "soteria.io/wave": "1"}, "vm-pfu-root")

	createDRPlan(t, ctx, "plan-pf-unknown", "soteria.io/wave")

	plan, err := waitForPreflight(ctx, "plan-pf-unknown", "", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	pf := plan.Status.Preflight
	if len(pf.Waves[0].VMs) != 1 {
		t.Fatalf("Expected 1 VM, got %d", len(pf.Waves[0].VMs))
	}
	if pf.Waves[0].VMs[0].StorageBackend != "unknown" {
		t.Errorf("StorageBackend = %q, want unknown", pf.Waves[0].VMs[0].StorageBackend)
	}
	if len(pf.Warnings) == 0 {
		t.Error("Expected warnings for unknown storage class")
	}
}

func TestDRPlanReconciler_Preflight_KubectlAccess(t *testing.T) {
	ctx := context.Background()
	ns := "test-pf-kubectl"
	createNamespace(t, ctx, ns)

	createPVC(t, ctx, "vm-pfk-root", ns, "test-odf")
	createVMWithPVC(t, ctx, "vm-pfk-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-kubectl", "soteria.io/wave": "1"}, "vm-pfk-root")

	createDRPlan(t, ctx, "plan-pf-kubectl", "soteria.io/wave")

	plan, err := waitForPreflight(ctx, "plan-pf-kubectl", "", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate kubectl -o json access: marshal status and verify preflight is present
	statusBytes, err := json.Marshal(plan.Status)
	if err != nil {
		t.Fatalf("Failed to marshal status: %v", err)
	}

	var statusMap map[string]interface{}
	if err := json.Unmarshal(statusBytes, &statusMap); err != nil {
		t.Fatalf("Failed to unmarshal status: %v", err)
	}

	pf, ok := statusMap["preflight"]
	if !ok || pf == nil {
		t.Fatal("Preflight not present in serialized status")
	}
	pfMap, ok := pf.(map[string]interface{})
	if !ok {
		t.Fatal("Preflight is not an object in serialized status")
	}
	totalVMs, ok := pfMap["totalVMs"]
	if !ok {
		t.Fatal("totalVMs not present in preflight")
	}
	if int(totalVMs.(float64)) != 1 {
		t.Errorf("totalVMs = %v, want 1", totalVMs)
	}
}

func TestDRPlanReconciler_Preflight_MultiWaveChunking(t *testing.T) {
	ctx := context.Background()
	ns := "test-pf-multiwave"
	createNamespace(t, ctx, ns)

	for i := range 6 {
		wave := fmt.Sprintf("%d", (i%3)+1)
		vmName := fmt.Sprintf("vm-pfmw-%d", i)
		pvcName := fmt.Sprintf("vm-pfmw-%d-root", i)
		createPVC(t, ctx, pvcName, ns, "test-odf")
		createVMWithPVC(t, ctx, vmName, ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-multiwave", "soteria.io/wave": wave}, pvcName)
	}

	createDRPlan(t, ctx, "plan-pf-multiwave", "soteria.io/wave")

	plan, err := waitForPreflight(ctx, "plan-pf-multiwave", "", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	pf := plan.Status.Preflight
	if len(pf.Waves) != 3 {
		t.Fatalf("Preflight.Waves = %d, want 3", len(pf.Waves))
	}
	if pf.TotalVMs != 6 {
		t.Errorf("TotalVMs = %d, want 6", pf.TotalVMs)
	}

	for _, w := range pf.Waves {
		if w.VMCount != 2 {
			t.Errorf("Wave %q VMCount = %d, want 2", w.WaveKey, w.VMCount)
		}
		if len(w.Chunks) == 0 {
			t.Errorf("Wave %q has no chunks", w.WaveKey)
		}
	}
}

func TestDRPlanReconciler_Preflight_WarningsPopulated(t *testing.T) {
	ctx := context.Background()
	ns := "test-pf-warnings"
	createNamespace(t, ctx, ns)

	// VM with unlisted storage class — should generate warning
	createPVC(t, ctx, "vm-pfw-root", ns, "mystery-class")
	createVMWithPVC(t, ctx, "vm-pfw-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-pf-warnings", "soteria.io/wave": "1"}, "vm-pfw-root")

	createDRPlan(t, ctx, "plan-pf-warnings", "soteria.io/wave")

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: "plan-pf-warnings"}, &plan); err == nil {
			if plan.Status.Preflight != nil && len(plan.Status.Preflight.Warnings) > 0 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Timed out waiting for preflight warnings to be populated")
}
