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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestDRPlanReconciler_ReplicationHealth_Populated(t *testing.T) {
	ctx := context.Background()
	ns := "test-repl-health"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-health-1", ns, map[string]string{
		soteriav1alpha1.DRPlanLabel: "plan-repl-health",
		"soteria.io/wave":          "1",
	})

	createDRPlan(t, ctx, "plan-repl-health")

	plan, err := waitForCondition(ctx, "plan-repl-health", "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	plan, err = waitForReplicationHealth(ctx, "plan-repl-health", 1, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.ReplicationHealth) < 1 {
		t.Fatalf("ReplicationHealth entries = %d, want >= 1", len(plan.Status.ReplicationHealth))
	}

	h := plan.Status.ReplicationHealth[0]
	if h.LastChecked.IsZero() {
		t.Error("LastChecked should be populated")
	}
	if h.Name == "" {
		t.Error("VolumeGroupHealth.Name should be populated")
	}

	replCond := findTestCondition(plan.Status.Conditions, "ReplicationHealthy")
	if replCond == nil {
		t.Fatal("ReplicationHealthy condition not found")
	}
}

func TestDRPlanReconciler_ReplicationHealthy_NotReplicatingIsNeutral(t *testing.T) {
	ctx := context.Background()
	ns := "test-repl-cond"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-cond-1", ns, map[string]string{
		soteriav1alpha1.DRPlanLabel: "plan-repl-cond",
		"soteria.io/wave":          "1",
	})

	createDRPlan(t, ctx, "plan-repl-cond")

	// The noop driver returns HealthNotReplicating for NonReplicated VGs
	// (no SetSource/SetTarget has been called). NotReplicating is treated as
	// a neutral status — the aggregate condition is True/AllHealthy because
	// the absence of replication is not a degradation. Source/Target happy
	// path is covered by unit tests with the programmable fake driver.
	plan, err := waitForReplicationHealth(ctx, "plan-repl-cond", 1, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	h := plan.Status.ReplicationHealth[0]
	if h.Health != soteriav1alpha1.HealthStatusNotReplicating {
		t.Errorf("Health = %q, want NotReplicating (noop NonReplicated VG)", h.Health)
	}

	replCond := findTestCondition(plan.Status.Conditions, "ReplicationHealthy")
	if replCond == nil {
		t.Fatal("ReplicationHealthy condition not found")
	}
	if replCond.Status != metav1.ConditionTrue {
		t.Errorf("ReplicationHealthy.Status = %v, want True (NotReplicating is neutral)", replCond.Status)
	}
	if replCond.Reason != "AllHealthy" {
		t.Errorf("ReplicationHealthy.Reason = %q, want AllHealthy", replCond.Reason)
	}
}

// waitForReplicationHealth polls until the DRPlan has at least minEntries
// in ReplicationHealth.
func waitForReplicationHealth(ctx context.Context, name string, minEntries int, timeout time.Duration) (*soteriav1alpha1.DRPlan, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if len(plan.Status.ReplicationHealth) >= minEntries {
			return &plan, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for ReplicationHealth (minEntries=%d) on DRPlan %q", minEntries, name)
}

func findTestCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
