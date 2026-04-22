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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func cleanupDRPlan(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	_ = testClient.Delete(ctx, plan)
}

// waitForObject polls the cache until the object is found or the timeout is reached.
func waitForObject(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	timeout := 5 * time.Second
	tick := 50 * time.Millisecond
	deadline := time.After(timeout)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			return testClient.Get(ctx, key, obj)
		case <-ticker.C:
			if err := testClient.Get(ctx, key, obj); err == nil {
				return nil
			}
		}
	}
}

func TestDRPlanWebhook_ValidPlan_Allowed(t *testing.T) {
	ctx := context.Background()
	ns := fmt.Sprintf("valid-plan-%d", uniqueCounter())
	createTestNamespace(t, ctx, ns, nil)

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("valid-plan-%d", uniqueCounter())},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Expected valid plan creation to succeed, but failed: %v", err)
	}
	defer cleanupDRPlan(t, ctx, plan.Name)
}

func TestDRPlanWebhook_InvalidWaveLabel_Rejected(t *testing.T) {
	ctx := context.Background()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("no-wave-%d", uniqueCounter())},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			WaveLabel:              "",
			MaxConcurrentFailovers: 10,
		},
	}
	err := testClient.Create(ctx, plan)
	if err == nil {
		defer cleanupDRPlan(t, ctx, plan.Name)
		t.Fatal("Expected creation to be denied for missing waveLabel, but it succeeded")
	}
	if !strings.Contains(err.Error(), "waveLabel") {
		t.Errorf("Expected waveLabel error, got: %v", err)
	}
}

func TestDRPlanWebhook_InvalidMaxConcurrent_Rejected(t *testing.T) {
	ctx := context.Background()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("bad-max-%d", uniqueCounter())},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 0,
		},
	}
	err := testClient.Create(ctx, plan)
	if err == nil {
		defer cleanupDRPlan(t, ctx, plan.Name)
		t.Fatal("Expected creation to be denied for invalid maxConcurrentFailovers, but it succeeded")
	}
	if !strings.Contains(err.Error(), "maxConcurrentFailovers") {
		t.Errorf("Expected maxConcurrentFailovers error, got: %v", err)
	}
}

func TestDRPlanWebhook_DELETE_Allowed(t *testing.T) {
	ctx := context.Background()

	planName := fmt.Sprintf("plan-del-%d", uniqueCounter())
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan for deletion test: %v", err)
	}

	if err := testClient.Delete(ctx, plan); err != nil {
		t.Fatalf("Expected DELETE to succeed, but failed: %v", err)
	}
}

func TestDRPlanWebhook_UPDATE_Validation(t *testing.T) {
	ctx := context.Background()

	planName := fmt.Sprintf("plan-upd-%d", uniqueCounter())
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, planName)

	var existing soteriav1alpha1.DRPlan
	if err := waitForObject(ctx, client.ObjectKey{Name: planName}, &existing); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	existing.Spec.MaxConcurrentFailovers = 8
	if err := testClient.Update(ctx, &existing); err != nil {
		t.Fatalf("Expected valid update to succeed, but failed: %v", err)
	}
}

func TestDRPlanWebhook_MissingSites_Rejected(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		primary     string
		secondary   string
		wantMessage string
	}{
		{"missing primarySite", "", "dc-east", "primarySite"},
		{"missing secondarySite", "dc-west", "", "secondarySite"},
		{"equal sites", "dc-west", "dc-west", "secondarySite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &soteriav1alpha1.DRPlan{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("site-%d", uniqueCounter()),
				},
				Spec: soteriav1alpha1.DRPlanSpec{
					PrimarySite:            tt.primary,
					SecondarySite:          tt.secondary,
					WaveLabel:              "soteria.io/wave",
					MaxConcurrentFailovers: 10,
				},
			}
			err := testClient.Create(ctx, plan)
			if err == nil {
				defer cleanupDRPlan(t, ctx, plan.Name)
				t.Fatalf("expected creation denied for %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("expected error containing %q, got: %v",
					tt.wantMessage, err)
			}
		})
	}
}

func TestDRPlanWebhook_SiteImmutability_Rejected(t *testing.T) {
	ctx := context.Background()

	planName := fmt.Sprintf("immut-site-%d", uniqueCounter())
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 10,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create plan: %v", err)
	}
	defer cleanupDRPlan(t, ctx, planName)

	var existing soteriav1alpha1.DRPlan
	if err := waitForObject(ctx, client.ObjectKey{Name: planName}, &existing); err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}

	existing.Spec.PrimarySite = "dc-north"
	err := testClient.Update(ctx, &existing)
	if err == nil {
		t.Fatal("expected update denied when primarySite changes")
	}
	if !strings.Contains(err.Error(), "primarySite") {
		t.Errorf("expected error containing 'primarySite', got: %v", err)
	}
}

var counter int

func uniqueCounter() int {
	counter++
	return counter
}
