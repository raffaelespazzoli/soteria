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

package rbac_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func ensureNamespace(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := adminClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("creating namespace %s: %v", name, err)
	}
}

func newDRPlan(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRPlan",
			"metadata":   map[string]any{"name": name},
			"spec": map[string]any{
				"maxConcurrentFailovers": int64(2),
			},
		},
	}
}

func newDRExecution(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "soteria.io/v1alpha1",
			"kind":       "DRExecution",
			"metadata":   map[string]any{"name": name},
			"spec": map[string]any{
				"planName": "my-plan",
				"mode":     "PlannedMigration",
			},
		},
	}
}

func TestRBAC_ViewerCanReadDRPlan(t *testing.T) {
	ctx := context.Background()
	ns := "rbac-viewer-read"
	ensureNamespace(t, ctx, ns)

	if err := bindRole(ctx, "viewer-read-binding", "soteria-viewer", "test-viewer"); err != nil {
		t.Fatalf("binding viewer role: %v", err)
	}

	// Pre-create a DRPlan as admin
	plan := newDRPlan("viewer-plan")
	if err := adminClient.Create(ctx, plan); err != nil {
		t.Fatalf("admin creating DRPlan: %v", err)
	}

	viewerClient, err := impersonatedClient("test-viewer", nil)
	if err != nil {
		t.Fatalf("creating viewer client: %v", err)
	}

	// Viewer can GET DRPlan
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(plan.GroupVersionKind())
	if err := viewerClient.Get(ctx, types.NamespacedName{Name: "viewer-plan"}, got); err != nil {
		t.Fatalf("viewer GET DRPlan should succeed: %v", err)
	}

	// Viewer cannot CREATE DRExecution
	exec := newDRExecution("viewer-exec")
	err = viewerClient.Create(ctx, exec)
	if err == nil {
		t.Fatal("viewer CREATE DRExecution should be forbidden")
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden, got: %v", err)
	}

	// Viewer cannot CREATE DRPlan
	extraPlan := newDRPlan("viewer-extra-plan")
	err = viewerClient.Create(ctx, extraPlan)
	if err == nil {
		t.Fatal("viewer CREATE DRPlan should be forbidden")
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden, got: %v", err)
	}
}

func TestRBAC_EditorCanCreateDRPlan(t *testing.T) {
	ctx := context.Background()
	ns := "rbac-editor-create"
	ensureNamespace(t, ctx, ns)

	if err := bindRole(ctx, "editor-create-binding", "soteria-editor", "test-editor"); err != nil {
		t.Fatalf("binding editor role: %v", err)
	}

	editorClient, err := impersonatedClient("test-editor", nil)
	if err != nil {
		t.Fatalf("creating editor client: %v", err)
	}

	// Editor can CREATE DRPlan
	plan := newDRPlan("editor-plan")
	if err := editorClient.Create(ctx, plan); err != nil {
		t.Fatalf("editor CREATE DRPlan should succeed: %v", err)
	}

	// Editor cannot CREATE DRExecution
	exec := newDRExecution("editor-exec")
	err = editorClient.Create(ctx, exec)
	if err == nil {
		t.Fatal("editor CREATE DRExecution should be forbidden")
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden, got: %v", err)
	}
}

func TestRBAC_OperatorCanCreateDRExecution(t *testing.T) {
	ctx := context.Background()
	ns := "rbac-operator-create"
	ensureNamespace(t, ctx, ns)

	if err := bindRole(ctx, "operator-create-binding", "soteria-operator", "test-operator"); err != nil {
		t.Fatalf("binding operator role: %v", err)
	}

	operatorClient, err := impersonatedClient("test-operator", nil)
	if err != nil {
		t.Fatalf("creating operator client: %v", err)
	}

	// Operator can CREATE DRExecution
	exec := newDRExecution("operator-exec")
	if err := operatorClient.Create(ctx, exec); err != nil {
		t.Fatalf("operator CREATE DRExecution should succeed: %v", err)
	}

	// Operator can also CREATE DRPlan
	plan := newDRPlan("operator-plan")
	if err := operatorClient.Create(ctx, plan); err != nil {
		t.Fatalf("operator CREATE DRPlan should succeed: %v", err)
	}
}

func TestRBAC_OperatorCannotDeleteDRExecution(t *testing.T) {
	ctx := context.Background()
	ns := "rbac-operator-delete"
	ensureNamespace(t, ctx, ns)

	if err := bindRole(ctx, "operator-delete-binding", "soteria-operator", "test-operator-del"); err != nil {
		t.Fatalf("binding operator role: %v", err)
	}

	// Pre-create a DRExecution as admin
	exec := newDRExecution("nodelete-exec")
	if err := adminClient.Create(ctx, exec); err != nil {
		t.Fatalf("admin creating DRExecution: %v", err)
	}

	operatorClient, err := impersonatedClient("test-operator-del", nil)
	if err != nil {
		t.Fatalf("creating operator client: %v", err)
	}

	// Operator cannot DELETE DRExecution (immutability enforcement)
	toDelete := &unstructured.Unstructured{}
	toDelete.SetGroupVersionKind(exec.GroupVersionKind())
	toDelete.SetName("nodelete-exec")
	err = operatorClient.Delete(ctx, toDelete)
	if err == nil {
		t.Fatal("operator DELETE DRExecution should be forbidden")
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden, got: %v", err)
	}
}

func TestRBAC_UnboundUserRejected(t *testing.T) {
	ctx := context.Background()
	ns := "rbac-unbound"
	ensureNamespace(t, ctx, ns)

	noBindingClient, err := impersonatedClient("nobody-user", nil)
	if err != nil {
		t.Fatalf("creating unbound-user client: %v", err)
	}

	plan := newDRPlan("noauth-plan")
	err = noBindingClient.Create(ctx, plan)
	if err == nil {
		t.Fatal("user with no bindings should be rejected")
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden, got: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(plan.GroupVersionKind())
	err = noBindingClient.Get(ctx, types.NamespacedName{Name: "any-plan"}, got)
	if err == nil {
		t.Fatal("user with no bindings GET should be rejected")
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("expected Forbidden, got: %v", err)
	}
}
