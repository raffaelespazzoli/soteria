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

package apiserver_test

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func newDynamicClientForAdmission(t *testing.T) dynamic.Interface {
	t.Helper()
	cfg := rest.CopyConfig(restConfig)
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("creating dynamic client: %v", err)
	}
	return client
}

func createDRPlan(t *testing.T, ctx context.Context, client dynamic.Interface, name, phase string, conditions []map[string]any) {
	t.Helper()

	plan := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata":   map[string]any{"name": name},
			"spec": map[string]any{
				"volumeReplicationDriver": map[string]any{"type": "noop"},
				"maxConcurrentFailovers":  int64(2),
				"primarySite":             "dc-west",
				"secondarySite":           "dc-east",
			},
		},
	}

	created, err := client.Resource(drplanGVR()).Create(ctx, plan, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRPlan %s failed: %v", name, err)
	}

	if phase != "" || len(conditions) > 0 {
		status := map[string]any{}
		if phase != "" {
			status["phase"] = phase
		}
		if len(conditions) > 0 {
			status["conditions"] = conditions
		}
		created.Object["status"] = status
		if _, err := client.Resource(drplanGVR()).UpdateStatus(ctx, created, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("UpdateStatus DRPlan %s failed: %v", name, err)
		}
	}
}

func deleteDRPlan(t *testing.T, ctx context.Context, client dynamic.Interface, name string) {
	t.Helper()
	_ = client.Resource(drplanGVR()).Delete(ctx, name, metav1.DeleteOptions{})
}

func TestAdmission_DRExecution_ValidCreate_Allowed(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	planName := "admission-valid-plan"
	createDRPlan(t, ctx, client, planName, soteriav1alpha1.PhaseSteadyState, nil)
	defer deleteDRPlan(t, ctx, client, planName)

	exec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "admission-valid-exec"},
			"spec": map[string]any{
				"planName": planName,
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}

	_, err := client.Resource(drexecutionGVR()).Create(ctx, exec, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Expected valid DRExecution CREATE to be allowed, got: %v", err)
	}
	defer func() {
		_ = client.Resource(drexecutionGVR()).Delete(ctx, "admission-valid-exec", metav1.DeleteOptions{})
	}()
}

func TestAdmission_DRExecution_PlanNotFound_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	exec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "exec-no-plan"},
			"spec": map[string]any{
				"planName": "nonexistent-plan",
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}

	_, err := client.Resource(drexecutionGVR()).Create(ctx, exec, metav1.CreateOptions{})
	if err == nil {
		defer func() {
			_ = client.Resource(drexecutionGVR()).Delete(ctx, "exec-no-plan", metav1.DeleteOptions{})
		}()
		t.Fatal("Expected DRExecution to be rejected when plan not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' in error, got: %v", err)
	}
}

func TestAdmission_DRExecution_ConcurrencyGate_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	planName := "admission-concurrency-plan"
	createDRPlan(t, ctx, client, planName, soteriav1alpha1.PhaseSteadyState, nil)
	defer deleteDRPlan(t, ctx, client, planName)

	// Create a first non-terminal DRExecution for this plan.
	firstExec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "existing-exec"},
			"spec": map[string]any{
				"planName": planName,
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}
	_, err := client.Resource(drexecutionGVR()).Create(ctx, firstExec, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Creating first DRExecution: %v", err)
	}
	defer func() {
		_ = client.Resource(drexecutionGVR()).Delete(ctx, "existing-exec", metav1.DeleteOptions{})
	}()

	// Second CREATE for the same plan should be rejected.
	secondExec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "exec-concurrent"},
			"spec": map[string]any{
				"planName": planName,
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}

	_, err = client.Resource(drexecutionGVR()).Create(ctx, secondExec, metav1.CreateOptions{})
	if err == nil {
		defer func() {
			_ = client.Resource(drexecutionGVR()).Delete(ctx, "exec-concurrent", metav1.DeleteOptions{})
		}()
		t.Fatal("Expected DRExecution to be rejected when active execution exists")
	}
	if !strings.Contains(err.Error(), "concurrent") {
		t.Errorf("Expected 'concurrent' in error, got: %v", err)
	}
}

func TestAdmission_DRExecution_ConcurrencyGate_AllowedAfterCompletion(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	planName := "admission-concurrency-done-plan"
	createDRPlan(t, ctx, client, planName, soteriav1alpha1.PhaseSteadyState, nil)
	defer deleteDRPlan(t, ctx, client, planName)

	// Create and complete a DRExecution.
	firstExec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "exec-completed"},
			"spec": map[string]any{
				"planName": planName,
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}
	created, err := client.Resource(drexecutionGVR()).Create(ctx, firstExec, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Creating first DRExecution: %v", err)
	}
	defer func() {
		_ = client.Resource(drexecutionGVR()).Delete(ctx, "exec-completed", metav1.DeleteOptions{})
	}()

	// Mark it as completed via status update.
	created.Object["status"] = map[string]any{
		"result": "Succeeded",
	}
	_, err = client.Resource(drexecutionGVR()).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Updating DRExecution status: %v", err)
	}

	// A new CREATE should be allowed since the existing execution is terminal.
	secondExec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "exec-after-completion"},
			"spec": map[string]any{
				"planName": planName,
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}
	_, err = client.Resource(drexecutionGVR()).Create(ctx, secondExec, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Expected DRExecution CREATE allowed after prior completed, got: %v", err)
	}
	defer func() {
		_ = client.Resource(drexecutionGVR()).Delete(ctx, "exec-after-completion", metav1.DeleteOptions{})
	}()
}

func TestAdmission_DRExecution_InvalidPhase_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	planName := "admission-phase-plan"
	createDRPlan(t, ctx, client, planName, soteriav1alpha1.PhaseFailedOver, nil)
	defer deleteDRPlan(t, ctx, client, planName)

	exec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "exec-bad-phase"},
			"spec": map[string]any{
				"planName": planName,
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}

	_, err := client.Resource(drexecutionGVR()).Create(ctx, exec, metav1.CreateOptions{})
	if err == nil {
		defer func() {
			_ = client.Resource(drexecutionGVR()).Delete(ctx, "exec-bad-phase", metav1.DeleteOptions{})
		}()
		t.Fatal("Expected DRExecution to be rejected for invalid phase transition")
	}
	if !strings.Contains(err.Error(), soteriav1alpha1.PhaseFailedOver) {
		t.Errorf("Expected current phase in error, got: %v", err)
	}
}

func TestAdmission_DRExecution_SitesOutOfSync_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	planName := "admission-sis-plan"
	conditions := []map[string]any{{
		"type":               "SitesInSync",
		"status":             "False",
		"reason":             "VMsMismatch",
		"message":            "VMs differ between sites",
		"lastTransitionTime": "2026-01-01T00:00:00Z",
	}}
	createDRPlan(t, ctx, client, planName, soteriav1alpha1.PhaseSteadyState, conditions)
	defer deleteDRPlan(t, ctx, client, planName)

	exec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": "exec-sis-false"},
			"spec": map[string]any{
				"planName": planName,
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}

	_, err := client.Resource(drexecutionGVR()).Create(ctx, exec, metav1.CreateOptions{})
	if err == nil {
		defer func() {
			_ = client.Resource(drexecutionGVR()).Delete(ctx, "exec-sis-false", metav1.DeleteOptions{})
		}()
		t.Fatal("Expected DRExecution to be rejected when SitesInSync=False")
	}
	if !strings.Contains(err.Error(), "sites do not agree") {
		t.Errorf("Expected 'sites do not agree' in error, got: %v", err)
	}
}

func TestAdmission_DRPlan_InvalidCreate_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	plan := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata":   map[string]any{"name": "admission-invalid-plan"},
			"spec": map[string]any{
				"volumeReplicationDriver": map[string]any{"type": "noop"},
				"maxConcurrentFailovers":  int64(0),
				"primarySite":             "dc-west",
				"secondarySite":           "dc-east",
			},
		},
	}

	_, err := client.Resource(drplanGVR()).Create(ctx, plan, metav1.CreateOptions{})
	if err == nil {
		defer deleteDRPlan(t, ctx, client, "admission-invalid-plan")
		t.Fatal("Expected DRPlan to be rejected for invalid maxConcurrentFailovers")
	}
	if !strings.Contains(err.Error(), "maxConcurrentFailovers") {
		t.Errorf("Expected 'maxConcurrentFailovers' in error, got: %v", err)
	}
}

func TestAdmission_DRPlan_ImmutableSiteUpdate_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	planName := "admission-immut-site"
	createDRPlan(t, ctx, client, planName,
		soteriav1alpha1.PhaseSteadyState, nil)
	defer deleteDRPlan(t, ctx, client, planName)

	got, err := client.Resource(drplanGVR()).Get(
		ctx, planName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get DRPlan failed: %v", err)
	}

	spec := got.Object["spec"].(map[string]any)
	spec["primarySite"] = "dc-north"

	_, err = client.Resource(drplanGVR()).Update(
		ctx, got, metav1.UpdateOptions{})
	if err == nil {
		t.Fatal("Expected update to be rejected when primarySite changes")
	}
	if !strings.Contains(err.Error(), "primarySite") {
		t.Errorf("Expected 'primarySite' in error, got: %v", err)
	}
}

func TestAdmission_DRPlan_ImmutableDriverUpdate_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	planName := "admission-immut-driver"
	createDRPlan(t, ctx, client, planName,
		soteriav1alpha1.PhaseSteadyState, nil)
	defer deleteDRPlan(t, ctx, client, planName)

	got, err := client.Resource(drplanGVR()).Get(
		ctx, planName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get DRPlan failed: %v", err)
	}

	spec := got.Object["spec"].(map[string]any)
	spec["volumeReplicationDriver"] = map[string]any{"type": "other"}

	_, err = client.Resource(drplanGVR()).Update(
		ctx, got, metav1.UpdateOptions{})
	if err == nil {
		t.Fatal("Expected update to be rejected when volumeReplicationDriver changes")
	}
	if !strings.Contains(err.Error(), "volumeReplicationDriver") {
		t.Errorf("Expected 'volumeReplicationDriver' in error, got: %v", err)
	}
}

func TestAdmission_DRPlan_ScalarDriverCreate_Rejected(t *testing.T) {
	client := newDynamicClientForAdmission(t)
	ctx := context.Background()

	plan := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata":   map[string]any{"name": "admission-scalar-driver"},
			"spec": map[string]any{
				"volumeReplicationDriver": "noop",
				"maxConcurrentFailovers":  int64(2),
				"primarySite":             "dc-west",
				"secondarySite":           "dc-east",
			},
		},
	}

	_, err := client.Resource(drplanGVR()).Create(ctx, plan, metav1.CreateOptions{})
	if err == nil {
		defer deleteDRPlan(t, ctx, client, "admission-scalar-driver")
		t.Fatal("Expected DRPlan to be rejected when volumeReplicationDriver is a scalar string")
	}
}
