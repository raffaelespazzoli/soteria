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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func drplanGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    soteriav1alpha1.GroupName,
		Version:  "v1alpha1",
		Resource: "drplans",
	}
}

func drexecutionGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    soteriav1alpha1.GroupName,
		Version:  "v1alpha1",
		Resource: "drexecutions",
	}
}

func drgroupstatusGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    soteriav1alpha1.GroupName,
		Version:  "v1alpha1",
		Resource: "drgroupstatuses",
	}
}

func newDynamicClient(t *testing.T) dynamic.Interface {
	t.Helper()
	cfg := rest.CopyConfig(restConfig)
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("creating dynamic client: %v", err)
	}
	return client
}

func TestAPIServer_Discovery_SoteriaGroupRegistered(t *testing.T) {
	cfg := rest.CopyConfig(restConfig)
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		t.Fatalf("creating discovery client: %v", err)
	}

	resources, err := disco.ServerResourcesForGroupVersion("soteria.io/v1alpha1")
	if err != nil {
		t.Fatalf("API group discovery failed: %v", err)
	}

	wantResources := map[string]bool{
		"drplans":        false,
		"drexecutions":   false,
		"drgroupstatuses": false,
	}
	for _, r := range resources.APIResources {
		if _, ok := wantResources[r.Name]; ok {
			wantResources[r.Name] = true
		}
	}
	for name, found := range wantResources {
		if !found {
			t.Errorf("expected resource %q in soteria.io/v1alpha1, not found", name)
		}
	}
}

func TestAPIServer_DRPlan_CRUD(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	plan := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata": map[string]any{
				"name": "test-plan",
			},
			"spec": map[string]any{
				"waveLabel":              "wave",
				"maxConcurrentFailovers": int64(2),
				"primarySite":            "dc-west",
				"secondarySite":          "dc-east",
			},
		},
	}

	// Create
	created, err := client.Resource(drplanGVR()).Create(ctx, plan, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRPlan failed: %v", err)
	}
	if created.GetName() != "test-plan" {
		t.Errorf("expected name test-plan, got %s", created.GetName())
	}
	if created.GetResourceVersion() == "" {
		t.Error("expected non-empty resource version after create")
	}

	// Get
	got, err := client.Resource(drplanGVR()).Get(ctx, "test-plan", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get DRPlan failed: %v", err)
	}
	if got.GetName() != "test-plan" {
		t.Errorf("expected name test-plan, got %s", got.GetName())
	}
	// Verify status phase was set to SteadyState by PrepareForCreate
	status, _, _ := unstructured.NestedString(got.Object, "status", "phase")
	if status != soteriav1alpha1.PhaseSteadyState {
		t.Errorf("expected status phase %s, got %s", soteriav1alpha1.PhaseSteadyState, status)
	}
	// Verify activeSite was set to primarySite by PrepareForCreate
	activeSite, _, _ := unstructured.NestedString(got.Object, "status", "activeSite")
	if activeSite != "dc-west" {
		t.Errorf("expected status activeSite dc-west, got %s", activeSite)
	}

	// List
	list, err := client.Resource(drplanGVR()).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List DRPlan failed: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(list.Items))
	}

	// Update
	got.Object["spec"].(map[string]any)["maxConcurrentFailovers"] = int64(5)
	updated, err := client.Resource(drplanGVR()).Update(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Update DRPlan failed: %v", err)
	}
	if updated.GetResourceVersion() == got.GetResourceVersion() {
		t.Error("expected resource version to change after update")
	}

	// Delete
	err = client.Resource(drplanGVR()).Delete(ctx, "test-plan", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Delete DRPlan failed: %v", err)
	}

	// Verify deleted
	_, err = client.Resource(drplanGVR()).Get(ctx, "test-plan", metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected NotFound error after delete")
	}
}

func TestAPIServer_DRPlan_StatusSubresource(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	plan := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata": map[string]any{
				"name": "plan-status-test",
			},
			"spec": map[string]any{
				"waveLabel":              "wave",
				"maxConcurrentFailovers": int64(1),
				"primarySite":            "dc-west",
				"secondarySite":          "dc-east",
			},
		},
	}

	created, err := client.Resource(drplanGVR()).Create(ctx, plan, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRPlan failed: %v", err)
	}

	// Update status subresource
	created.Object["status"] = map[string]any{
		"phase":      soteriav1alpha1.PhaseFailedOver,
		"activeSite": "dc-east",
	}
	statusUpdated, err := client.Resource(drplanGVR()).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	phase, _, _ := unstructured.NestedString(statusUpdated.Object, "status", "phase")
	if phase != soteriav1alpha1.PhaseFailedOver {
		t.Errorf("expected phase %s, got %s", soteriav1alpha1.PhaseFailedOver, phase)
	}

	// Verify spec was preserved during status update
	waveLabel, _, _ := unstructured.NestedString(statusUpdated.Object, "spec", "waveLabel")
	if waveLabel != "wave" {
		t.Errorf("expected spec.waveLabel to be preserved as 'wave', got %q", waveLabel)
	}
}

func TestAPIServer_DRPlan_Validation_MissingWaveLabel(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	invalidPlan := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata": map[string]any{
				"name": "invalid-plan",
			},
			"spec": map[string]any{
				"waveLabel":              "",
				"maxConcurrentFailovers": int64(1),
			},
		},
	}

	_, err := client.Resource(drplanGVR()).Create(ctx, invalidPlan, metav1.CreateOptions{})
	if err == nil {
		t.Fatal("expected validation error for missing waveLabel")
	}
}

func TestAPIServer_DRExecution_CRUD(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	exec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata": map[string]any{
				"name": "test-exec",
			},
			"spec": map[string]any{
				"planName": "my-plan",
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}

	created, err := client.Resource(drexecutionGVR()).Create(ctx, exec, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRExecution failed: %v", err)
	}
	if created.GetName() != "test-exec" {
		t.Errorf("expected name test-exec, got %s", created.GetName())
	}

	// Get
	got, err := client.Resource(drexecutionGVR()).Get(ctx, "test-exec", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get DRExecution failed: %v", err)
	}

	// Verify spec immutability on update: changing spec should be rejected
	got.Object["spec"].(map[string]any)["planName"] = "changed-plan"
	_, err = client.Resource(drexecutionGVR()).Update(ctx, got, metav1.UpdateOptions{})
	if err == nil {
		t.Fatal("expected error when changing immutable DRExecution spec")
	}

	// Delete
	err = client.Resource(drexecutionGVR()).Delete(ctx, "test-exec", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Delete DRExecution failed: %v", err)
	}
}

func TestAPIServer_DRExecution_AppendOnly(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	exec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata": map[string]any{
				"name": "completed-exec",
			},
			"spec": map[string]any{
				"planName": "my-plan",
				"mode":     string(soteriav1alpha1.ExecutionModePlannedMigration),
			},
		},
	}

	created, err := client.Resource(drexecutionGVR()).Create(ctx, exec, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRExecution failed: %v", err)
	}

	// Set status to completed via status subresource
	created.Object["status"] = map[string]any{
		"result": string(soteriav1alpha1.ExecutionResultSucceeded),
	}
	completed, err := client.Resource(drexecutionGVR()).UpdateStatus(ctx, created, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateStatus to completed failed: %v", err)
	}

	// Attempt to update status after completion — should be rejected
	completed.Object["status"] = map[string]any{
		"result": string(soteriav1alpha1.ExecutionResultFailed),
	}
	_, err = client.Resource(drexecutionGVR()).UpdateStatus(ctx, completed, metav1.UpdateOptions{})
	if err == nil {
		t.Fatal("expected error when updating completed DRExecution status (append-only)")
	}
}

func TestAPIServer_DRExecution_Validation_InvalidMode(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	invalidExec := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata": map[string]any{
				"name": "invalid-exec",
			},
			"spec": map[string]any{
				"planName": "my-plan",
				"mode":     "invalid_mode",
			},
		},
	}

	_, err := client.Resource(drexecutionGVR()).Create(ctx, invalidExec, metav1.CreateOptions{})
	if err == nil {
		t.Fatal("expected validation error for invalid mode")
	}
}

func TestAPIServer_DRGroupStatus_CRUD(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	gs := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRGroupStatus",
			"metadata": map[string]any{
				"name": "test-gs",
			},
			"spec": map[string]any{
				"executionName": "my-exec",
				"waveIndex":     int64(0),
				"groupName":     "group-1",
				"vmNames":       []any{"vm-1", "vm-2"},
			},
		},
	}

	created, err := client.Resource(drgroupstatusGVR()).Create(ctx, gs, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRGroupStatus failed: %v", err)
	}
	if created.GetName() != "test-gs" {
		t.Errorf("expected name test-gs, got %s", created.GetName())
	}

	// Get
	got, err := client.Resource(drgroupstatusGVR()).Get(ctx, "test-gs", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get DRGroupStatus failed: %v", err)
	}

	// Status subresource update
	got.Object["status"] = map[string]any{
		"phase": string(soteriav1alpha1.DRGroupResultInProgress),
	}
	statusUpdated, err := client.Resource(drgroupstatusGVR()).UpdateStatus(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateStatus DRGroupStatus failed: %v", err)
	}
	phase, _, _ := unstructured.NestedString(statusUpdated.Object, "status", "phase")
	if phase != string(soteriav1alpha1.DRGroupResultInProgress) {
		t.Errorf("expected phase %s, got %s", soteriav1alpha1.DRGroupResultInProgress, phase)
	}

	// Delete
	err = client.Resource(drgroupstatusGVR()).Delete(ctx, "test-gs", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Delete DRGroupStatus failed: %v", err)
	}
}

func TestAPIServer_DRGroupStatus_SpecImmutable(t *testing.T) {
	client := newDynamicClient(t)
	ctx := context.Background()

	gs := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRGroupStatus",
			"metadata": map[string]any{
				"name": "immutable-gs",
			},
			"spec": map[string]any{
				"executionName": "my-exec",
				"waveIndex":     int64(0),
				"groupName":     "group-1",
				"vmNames":       []any{"vm-1"},
			},
		},
	}

	_, err := client.Resource(drgroupstatusGVR()).Create(ctx, gs, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRGroupStatus failed: %v", err)
	}

	got, err := client.Resource(drgroupstatusGVR()).Get(ctx, "immutable-gs", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get DRGroupStatus failed: %v", err)
	}

	// Attempt to change spec.groupName — should be rejected
	got.Object["spec"].(map[string]any)["groupName"] = "changed-group"
	_, err = client.Resource(drgroupstatusGVR()).Update(ctx, got, metav1.UpdateOptions{})
	if err == nil {
		t.Fatal("expected error when changing immutable DRGroupStatus spec")
	}
}

func TestAPIServer_OpenAPI_SoteriaTypesPresent(t *testing.T) {
	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		t.Fatalf("creating HTTP client: %v", err)
	}

	url := fmt.Sprintf("%s/openapi/v3/apis/soteria.io/v1alpha1", restConfig.Host)
	resp, err := httpClient.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for OpenAPI endpoint, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parsing OpenAPI JSON: %v", err)
	}

	schemas, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI doc missing components.schemas")
	}

	for _, kind := range []string{"DRPlan", "DRExecution", "DRGroupStatus"} {
		found := false
		for key := range schemas {
			if len(key) >= len(kind) && key[len(key)-len(kind):] == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no OpenAPI schema ending with %q found in %d schemas", kind, len(schemas))
		}
	}
}

func TestAPIServer_DRPlan_Watch(t *testing.T) {
	client := newDynamicClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a plan first so the watch snapshot includes it
	plan := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata": map[string]any{
				"name": "watched-plan",
			},
			"spec": map[string]any{
				"waveLabel":              "wave",
				"maxConcurrentFailovers": int64(1),
				"primarySite":            "dc-west",
				"secondarySite":          "dc-east",
			},
		},
	}

	_, err := client.Resource(drplanGVR()).Create(ctx, plan, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create DRPlan for watch failed: %v", err)
	}

	// Start watch — the initial snapshot should emit an ADDED event for
	// the plan we just created.
	watcher, err := client.Resource(drplanGVR()).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Watch DRPlan failed: %v", err)
	}
	defer watcher.Stop()

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	receivedAdd := false
	for !receivedAdd {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				t.Fatal("watch channel closed unexpectedly")
			}
			if event.Type == watch.Added {
				receivedAdd = true
			}
		case <-timer.C:
			t.Fatal("timed out waiting for ADDED event")
		}
	}
}
