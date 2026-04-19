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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

const execTestTimeout = 15 * time.Second

func TestDRExecutionReconciler_SuccessfulSetup(t *testing.T) {
	ctx := context.Background()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-success-plan"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("creating DRPlan: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, plan) })

	if err := setPlanPhase(ctx, plan.Name, soteriav1alpha1.PhaseSteadyState); err != nil {
		t.Fatalf("setting DRPlan phase: %v", err)
	}

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-success-test"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: plan.Name,
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
	if err := testClient.Create(ctx, exec); err != nil {
		t.Fatalf("creating DRExecution: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, exec) })

	// Wait for execution to be processed.
	got, err := waitForExecStartTime(ctx, exec.Name, execTestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.StartTime == nil {
		t.Error("expected startTime to be set")
	}

	// Verify plan transitioned to FailingOver.
	updatedPlan, err := waitForPlanPhase(ctx, plan.Name, soteriav1alpha1.PhaseFailingOver, execTestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseFailingOver {
		t.Errorf("expected plan phase %q, got %q",
			soteriav1alpha1.PhaseFailingOver, updatedPlan.Status.Phase)
	}

	// Verify Progressing condition.
	var foundProgressing bool
	for _, c := range got.Status.Conditions {
		if c.Type == "Progressing" && c.Status == metav1.ConditionTrue {
			foundProgressing = true
			break
		}
	}
	if !foundProgressing {
		t.Error("expected Progressing=True condition on DRExecution")
	}
}

func TestDRExecutionReconciler_PlanNotFound(t *testing.T) {
	ctx := context.Background()

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-no-plan-test"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "nonexistent-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
	if err := testClient.Create(ctx, exec); err != nil {
		t.Fatalf("creating DRExecution: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, exec) })

	got, err := waitForExecResult(ctx, exec.Name, soteriav1alpha1.ExecutionResultFailed, execTestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.Result != soteriav1alpha1.ExecutionResultFailed {
		t.Errorf("expected result %q, got %q",
			soteriav1alpha1.ExecutionResultFailed, got.Status.Result)
	}

	var foundReady bool
	for _, c := range got.Status.Conditions {
		if c.Type == "Ready" && c.Reason == "PlanNotFound" {
			foundReady = true
			break
		}
	}
	if !foundReady {
		t.Error("expected Ready condition with reason PlanNotFound")
	}
}

func TestDRExecutionReconciler_InvalidPhase(t *testing.T) {
	ctx := context.Background()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-invalid-phase-plan"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("creating DRPlan: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, plan) })

	if err := setPlanPhase(ctx, plan.Name, soteriav1alpha1.PhaseFailedOver); err != nil {
		t.Fatalf("setting DRPlan phase: %v", err)
	}

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-invalid-phase-test"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: plan.Name,
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
	if err := testClient.Create(ctx, exec); err != nil {
		t.Fatalf("creating DRExecution: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, exec) })

	got, err := waitForExecResult(ctx, exec.Name, soteriav1alpha1.ExecutionResultFailed, execTestTimeout)
	if err != nil {
		t.Fatal(err)
	}

	var foundReady bool
	for _, c := range got.Status.Conditions {
		if c.Type == "Ready" && c.Reason == "InvalidPhaseTransition" {
			foundReady = true
			break
		}
	}
	if !foundReady {
		t.Error("expected Ready condition with reason InvalidPhaseTransition")
	}

	// Verify plan phase is unchanged.
	var updatedPlan soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: plan.Name}, &updatedPlan); err != nil {
		t.Fatalf("getting DRPlan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseFailedOver {
		t.Errorf("expected plan phase unchanged %q, got %q",
			soteriav1alpha1.PhaseFailedOver, updatedPlan.Status.Phase)
	}
}

func TestDRExecutionReconciler_IdempotentRereconcile(t *testing.T) {
	ctx := context.Background()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-idempotent-plan"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("creating DRPlan: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, plan) })

	if err := setPlanPhase(ctx, plan.Name, soteriav1alpha1.PhaseSteadyState); err != nil {
		t.Fatalf("setting DRPlan phase: %v", err)
	}

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-idempotent-test"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: plan.Name,
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
	}
	if err := testClient.Create(ctx, exec); err != nil {
		t.Fatalf("creating DRExecution: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, exec) })

	// Wait for first reconcile.
	got, err := waitForExecStartTime(ctx, exec.Name, execTestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	firstStartTime := got.Status.StartTime.Time

	// Wait briefly to let any re-reconcile happen.
	time.Sleep(2 * time.Second)

	// Verify start time hasn't changed (idempotent).
	var recheck soteriav1alpha1.DRExecution
	if err := testClient.Get(ctx, client.ObjectKey{Name: exec.Name}, &recheck); err != nil {
		t.Fatalf("getting DRExecution: %v", err)
	}
	if !recheck.Status.StartTime.Time.Equal(firstStartTime) {
		t.Errorf("startTime changed after re-reconcile: first=%v, now=%v",
			firstStartTime, recheck.Status.StartTime.Time)
	}

	// Verify plan is still FailingOver (not double-transitioned).
	var updatedPlan soteriav1alpha1.DRPlan
	if err := testClient.Get(ctx, client.ObjectKey{Name: plan.Name}, &updatedPlan); err != nil {
		t.Fatalf("getting DRPlan: %v", err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseFailingOver {
		t.Errorf("expected plan phase %q after re-reconcile, got %q",
			soteriav1alpha1.PhaseFailingOver, updatedPlan.Status.Phase)
	}
}

func TestDRExecutionReconciler_DisasterMode(t *testing.T) {
	ctx := context.Background()

	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-disaster-plan"},
		Spec: soteriav1alpha1.DRPlanSpec{
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 4,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("creating DRPlan: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, plan) })

	if err := setPlanPhase(ctx, plan.Name, soteriav1alpha1.PhaseSteadyState); err != nil {
		t.Fatalf("setting DRPlan phase: %v", err)
	}

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-disaster-test"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: plan.Name,
			Mode:     soteriav1alpha1.ExecutionModeDisaster,
		},
	}
	if err := testClient.Create(ctx, exec); err != nil {
		t.Fatalf("creating DRExecution: %v", err)
	}
	t.Cleanup(func() { _ = testClient.Delete(ctx, exec) })

	got, err := waitForExecStartTime(ctx, exec.Name, execTestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.StartTime == nil {
		t.Error("expected startTime to be set")
	}

	// Verify plan transitioned to FailingOver.
	updatedPlan, err := waitForPlanPhase(ctx, plan.Name, soteriav1alpha1.PhaseFailingOver, execTestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if updatedPlan.Status.Phase != soteriav1alpha1.PhaseFailingOver {
		t.Errorf("expected plan phase %q, got %q",
			soteriav1alpha1.PhaseFailingOver, updatedPlan.Status.Phase)
	}
}
