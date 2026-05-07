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

var counter int

func uniqueCounter() int {
	counter++
	return counter
}
