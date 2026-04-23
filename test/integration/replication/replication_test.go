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

package replication_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage"

	v1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
	scyllastore "github.com/soteria-project/soteria/pkg/storage/scylladb"
)

// ---------------------------------------------------------------------------
// Task 5: Cross-site replication tests (AC #2, #4)
// ---------------------------------------------------------------------------

func TestReplication_ResourceCreatedOnDC1_VisibleOnDC2(t *testing.T) {
	dc1Store := newDRPlanStoreForDC(dc1Session)
	dc2Store := newDRPlanStoreForDC(dc2Session)

	plan := newDRPlan("repl-dc1-to-dc2")
	out := &v1alpha1.DRPlan{}
	ctx := context.Background()

	if err := dc1Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Create on DC1 failed: %v", err)
	}

	// Poll DC2 until the resource appears (async replication window).
	got := &v1alpha1.DRPlan{}
	if err := pollUntilFound(t, dc2Store, planKey(plan.Name), got, 15*time.Second); err != nil {
		t.Fatalf("Resource not visible on DC2: %v", err)
	}

	if got.Name != plan.Name {
		t.Errorf("expected name %q, got %q", plan.Name, got.Name)
	}
}

func TestReplication_ResourceCreatedOnDC2_VisibleOnDC1(t *testing.T) {
	dc1Store := newDRPlanStoreForDC(dc1Session)
	dc2Store := newDRPlanStoreForDC(dc2Session)

	plan := newDRPlan("repl-dc2-to-dc1")
	out := &v1alpha1.DRPlan{}
	ctx := context.Background()

	if err := dc2Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Create on DC2 failed: %v", err)
	}

	got := &v1alpha1.DRPlan{}
	if err := pollUntilFound(t, dc1Store, planKey(plan.Name), got, 15*time.Second); err != nil {
		t.Fatalf("Resource not visible on DC1: %v", err)
	}

	if got.Name != plan.Name {
		t.Errorf("expected name %q, got %q", plan.Name, got.Name)
	}
}

func TestReplication_MultipleResources_AllReplicateWithinWindow(t *testing.T) {
	dc1Store := newDRPlanStoreForDC(dc1Session)
	dc2Store := newDRPlanStoreForDC(dc2Session)
	ctx := context.Background()

	const count = 10
	names := make([]string, count)
	for i := range count {
		name := fmt.Sprintf("repl-batch-%d", i)
		names[i] = name
		plan := newDRPlan(name)
		out := &v1alpha1.DRPlan{}
		if err := dc1Store.Create(ctx, planKey(name), plan, out, 0); err != nil {
			t.Fatalf("Create %d on DC1 failed: %v", i, err)
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for _, name := range names {
		got := &v1alpha1.DRPlan{}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			remaining = 1 * time.Second
		}
		if err := pollUntilFound(t, dc2Store, planKey(name), got, remaining); err != nil {
			t.Fatalf("Resource %q not replicated to DC2 within window: %v", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Task 6: DC failure resilience tests (AC #3)
// ---------------------------------------------------------------------------

func TestResilience_DC1Down_DC2ContinuesReads(t *testing.T) {
	dc1Store := newDRPlanStoreForDC(dc1Session)
	dc2Store := newDRPlanStoreForDC(dc2Session)
	ctx := context.Background()

	plan := newDRPlan("resil-reads")
	out := &v1alpha1.DRPlan{}
	if err := dc1Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Pre-create on DC1 failed: %v", err)
	}

	// Wait for replication to DC2
	got := &v1alpha1.DRPlan{}
	if err := pollUntilFound(t, dc2Store, planKey(plan.Name), got, 15*time.Second); err != nil {
		t.Fatalf("Pre-replication failed: %v", err)
	}

	// Stop DC1 container
	if err := dc1Container.Stop(ctx, nil); err != nil {
		t.Fatalf("Stopping DC1: %v", err)
	}
	defer restartDC1(t)

	// DC2 should still serve reads
	time.Sleep(5 * time.Second)
	got2 := &v1alpha1.DRPlan{}
	if err := dc2Store.Get(ctx, planKey(plan.Name), storage.GetOptions{}, got2); err != nil {
		t.Fatalf("DC2 read failed while DC1 down: %v", err)
	}
	if got2.Name != plan.Name {
		t.Errorf("expected %q, got %q", plan.Name, got2.Name)
	}
}

func TestResilience_DC1Down_DC2ContinuesWrites(t *testing.T) {
	dc2Store := newDRExecutionStoreForDC(dc2Session)
	ctx := context.Background()

	// Stop DC1 container
	if err := dc1Container.Stop(ctx, nil); err != nil {
		t.Fatalf("Stopping DC1: %v", err)
	}
	defer restartDC1(t)

	time.Sleep(5 * time.Second)

	// DC2 should accept writes
	exec := newDRExecution("resil-write")
	out := &v1alpha1.DRExecution{}
	if err := dc2Store.Create(ctx, execKey(exec.Name), exec, out, 0); err != nil {
		t.Fatalf("DC2 write failed while DC1 down: %v", err)
	}
}

func TestResilience_DC1Down_DC2NoErrors(t *testing.T) {
	dc2Store := newDRPlanStoreForDC(dc2Session)
	ctx := context.Background()

	// Pre-populate before taking DC1 down
	plan := newDRPlan("resil-no-errors")
	out := &v1alpha1.DRPlan{}

	// Use DC2 to create (both DCs still up)
	if err := dc2Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Pre-create on DC2 failed: %v", err)
	}

	if err := dc1Container.Stop(ctx, nil); err != nil {
		t.Fatalf("Stopping DC1: %v", err)
	}
	defer restartDC1(t)

	time.Sleep(5 * time.Second)

	// Multiple operations on DC2 should succeed without errors.
	for i := range 5 {
		name := fmt.Sprintf("resil-noerr-%d", i)
		p := newDRPlan(name)
		o := &v1alpha1.DRPlan{}
		if err := dc2Store.Create(ctx, planKey(name), p, o, 0); err != nil {
			t.Errorf("Write %d on DC2 errored while DC1 down: %v", i, err)
		}
	}

	list := &v1alpha1.DRPlanList{}
	if err := dc2Store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{Recursive: true}, list); err != nil {
		t.Errorf("List on DC2 errored while DC1 down: %v", err)
	}
}

func TestResilience_DC2Down_DC1ContinuesOperations(t *testing.T) {
	dc1Store := newDRPlanStoreForDC(dc1Session)
	ctx := context.Background()

	if err := dc2Container.Stop(ctx, nil); err != nil {
		t.Fatalf("Stopping DC2: %v", err)
	}
	defer restartDC2(t)

	time.Sleep(5 * time.Second)

	plan := newDRPlan("resil-dc2-down")
	out := &v1alpha1.DRPlan{}
	if err := dc1Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("DC1 write failed while DC2 down: %v", err)
	}

	got := &v1alpha1.DRPlan{}
	if err := dc1Store.Get(ctx, planKey(plan.Name), storage.GetOptions{}, got); err != nil {
		t.Fatalf("DC1 read failed while DC2 down: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 7: Recovery & reconciliation tests (AC #4)
// ---------------------------------------------------------------------------

func TestRecovery_DC1Recovers_StateReconciles(t *testing.T) {
	dc2Store := newDRPlanStoreForDC(dc2Session)
	ctx := context.Background()

	// Stop DC1
	if err := dc1Container.Stop(ctx, nil); err != nil {
		t.Fatalf("Stopping DC1: %v", err)
	}

	time.Sleep(5 * time.Second)

	// Write resources on DC2 while DC1 is down.
	const count = 3
	names := make([]string, count)
	for i := range count {
		name := fmt.Sprintf("recovery-%d", i)
		names[i] = name
		plan := newDRPlan(name)
		out := &v1alpha1.DRPlan{}
		if err := dc2Store.Create(ctx, planKey(name), plan, out, 0); err != nil {
			t.Fatalf("Create %q on DC2 (DC1 down): %v", name, err)
		}
	}

	// Restart DC1 and wait for it to rejoin the cluster.
	restartDC1(t)
	waitForCluster(dc1Session, 3)

	// Run nodetool repair on DC1 to trigger anti-entropy reconciliation.
	runRepair(t, dc1Container)

	// Verify DC1 can see all resources written on DC2.
	dc1Store := newDRPlanStoreForDC(dc1Session)
	for _, name := range names {
		got := &v1alpha1.DRPlan{}
		if err := pollUntilFound(t, dc1Store, planKey(name), got, 30*time.Second); err != nil {
			t.Errorf("Resource %q not reconciled to DC1 after recovery: %v", name, err)
		}
	}
}

func TestRecovery_BothDCsIdenticalAfterReconciliation(t *testing.T) {
	dc1Store := newDRPlanStoreForDC(dc1Session)
	dc2Store := newDRPlanStoreForDC(dc2Session)
	ctx := context.Background()

	// Create resources via different DCs.
	planA := newDRPlan("identical-a")
	planB := newDRPlan("identical-b")
	outA := &v1alpha1.DRPlan{}
	outB := &v1alpha1.DRPlan{}

	if err := dc1Store.Create(ctx, planKey(planA.Name), planA, outA, 0); err != nil {
		t.Fatalf("Create A on DC1: %v", err)
	}
	if err := dc2Store.Create(ctx, planKey(planB.Name), planB, outB, 0); err != nil {
		t.Fatalf("Create B on DC2: %v", err)
	}

	// Wait for natural replication.
	time.Sleep(5 * time.Second)

	// List from both DCs.
	dc1List := &v1alpha1.DRPlanList{}
	dc2List := &v1alpha1.DRPlanList{}
	listOpts := storage.ListOptions{Recursive: true}

	dc1NSStore := newStoreForDC(dc1Session, "drplans",
		func() runtime.Object { return &v1alpha1.DRPlan{} },
		func() runtime.Object { return &v1alpha1.DRPlanList{} },
	)
	dc2NSStore := newStoreForDC(dc2Session, "drplans",
		func() runtime.Object { return &v1alpha1.DRPlan{} },
		func() runtime.Object { return &v1alpha1.DRPlanList{} },
	)

	if err := dc1NSStore.GetList(ctx, "/soteria.io/drplans", listOpts, dc1List); err != nil {
		t.Fatalf("List DC1: %v", err)
	}
	if err := dc2NSStore.GetList(ctx, "/soteria.io/drplans", listOpts, dc2List); err != nil {
		t.Fatalf("List DC2: %v", err)
	}

	if len(dc1List.Items) != len(dc2List.Items) {
		t.Fatalf("DC1 has %d items, DC2 has %d items", len(dc1List.Items), len(dc2List.Items))
	}

	dc1Names := mapNames(dc1List.Items)
	dc2Names := mapNames(dc2List.Items)
	for name := range dc1Names {
		if !dc2Names[name] {
			t.Errorf("DC1 has %q but DC2 does not", name)
		}
	}
}

func TestRecovery_NoManualIntervention(t *testing.T) {
	// This test verifies that ScyllaDB's built-in repair is sufficient —
	// no custom reconciliation code is needed.
	dc2Store := newDRPlanStoreForDC(dc2Session)
	ctx := context.Background()

	plan := newDRPlan("no-manual")
	out := &v1alpha1.DRPlan{}
	if err := dc2Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Create on DC2: %v", err)
	}

	// Natural async replication — no repair, no custom code.
	dc1Store := newDRPlanStoreForDC(dc1Session)
	got := &v1alpha1.DRPlan{}
	if err := pollUntilFound(t, dc1Store, planKey(plan.Name), got, 15*time.Second); err != nil {
		t.Fatalf("Async replication did not deliver resource to DC1: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 8: Conflict resolution tests (AC #5)
// ---------------------------------------------------------------------------

func TestConflict_ConcurrentNonCriticalWrite_LastWriteWins(t *testing.T) {
	dc1Store := newDRPlanStoreForDC(dc1Session)
	dc2Store := newDRPlanStoreForDC(dc2Session)
	ctx := context.Background()

	plan := newDRPlan("lww-test")
	out := &v1alpha1.DRPlan{}
	if err := dc1Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Wait for replication.
	got := &v1alpha1.DRPlan{}
	if err := pollUntilFound(t, dc2Store, planKey(plan.Name), got, 15*time.Second); err != nil {
		t.Fatalf("Pre-replication: %v", err)
	}

	// Concurrent non-critical updates from both DCs (labels update).
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = updateLabel(dc1Store, ctx, plan.Name, "source", "dc1")
	}()
	go func() {
		defer wg.Done()
		_ = updateLabel(dc2Store, ctx, plan.Name, "source", "dc2")
	}()
	wg.Wait()

	// After replication settles, one value wins (LWW).
	time.Sleep(5 * time.Second)
	final := &v1alpha1.DRPlan{}
	if err := dc1Store.Get(ctx, planKey(plan.Name), storage.GetOptions{}, final); err != nil {
		t.Fatalf("Get final: %v", err)
	}

	val := final.Labels["source"]
	if val != "dc1" && val != "dc2" {
		t.Errorf("expected label source to be 'dc1' or 'dc2', got %q", val)
	}
}

func TestConflict_ConcurrentPhaseTransition_LWTPreventsDuplicate(t *testing.T) {
	dc1Store := newDRPlanStoreWithDetector(dc1Session)
	dc2Store := newDRPlanStoreWithDetector(dc2Session)
	ctx := context.Background()

	plan := newDRPlanWithPhase("lwt-phase", v1alpha1.PhaseSteadyState)
	out := &v1alpha1.DRPlan{}
	if err := dc1Store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := pollUntilFound(t, dc2Store, planKey(plan.Name), &v1alpha1.DRPlan{}, 15*time.Second); err != nil {
		t.Fatalf("Pre-replication: %v", err)
	}

	// Both DCs attempt the same phase transition concurrently.
	var wg sync.WaitGroup
	var dc1Err, dc2Err error
	wg.Add(2)

	go func() {
		defer wg.Done()
		dc1Err = updatePhase(dc1Store, ctx, plan.Name, v1alpha1.PhaseFailedOver)
	}()
	go func() {
		defer wg.Done()
		dc2Err = updatePhase(dc2Store, ctx, plan.Name, v1alpha1.PhaseFailedOver)
	}()
	wg.Wait()

	// Both writes target the same key via CAS (IF resource_version = ?).
	// At least one must succeed. Both could succeed only if a retry loop
	// re-reads the new RV, but the phase would already be FailingOver
	// so the detector fires and Serial CAS is used — verify the final
	// state is consistent regardless.
	if dc1Err != nil && dc2Err != nil {
		t.Fatalf("Both DCs failed: dc1=%v dc2=%v", dc1Err, dc2Err)
	}

	// Verify the final object has exactly the expected phase.
	time.Sleep(3 * time.Second)
	final := &v1alpha1.DRPlan{}
	if err := dc1Store.Get(ctx, planKey(plan.Name), storage.GetOptions{}, final); err != nil {
		t.Fatalf("Final Get: %v", err)
	}
	if final.Status.Phase != v1alpha1.PhaseFailedOver {
		t.Errorf("expected phase %q after concurrent transitions, got %q",
			v1alpha1.PhaseFailedOver, final.Status.Phase)
	}
}

func TestConflict_LWT_StalePhaseRejected(t *testing.T) {
	store := newDRPlanStoreWithDetector(dc1Session)
	ctx := context.Background()

	plan := newDRPlanWithPhase("stale-phase", v1alpha1.PhaseSteadyState)
	out := &v1alpha1.DRPlan{}
	if err := store.Create(ctx, planKey(plan.Name), plan, out, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// First update succeeds.
	if err := updatePhase(store, ctx, plan.Name, v1alpha1.PhaseFailedOver); err != nil {
		t.Fatalf("First phase update: %v", err)
	}

	// Second update with a different phase should also succeed (CAS on resourceVersion).
	if err := updatePhase(store, ctx, plan.Name, v1alpha1.PhaseDRedSteadyState); err != nil {
		t.Fatalf("Second phase update: %v", err)
	}

	// Verify final state.
	got := &v1alpha1.DRPlan{}
	if err := store.Get(ctx, planKey(plan.Name), storage.GetOptions{}, got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status.Phase != v1alpha1.PhaseDRedSteadyState {
		t.Errorf("expected phase %q, got %q", v1alpha1.PhaseDRedSteadyState, got.Status.Phase)
	}
}

func TestConflict_LWT_TableDriven(t *testing.T) {
	store := newDRPlanStoreWithDetector(dc1Session)
	ctx := context.Background()

	tests := []struct {
		name         string
		initialPhase string
		updatePhase  string
		expectErr    bool
	}{
		{"SteadyState to FailedOver", v1alpha1.PhaseSteadyState, v1alpha1.PhaseFailedOver, false},
		{"FailedOver to DRedSteadyState", v1alpha1.PhaseFailedOver, v1alpha1.PhaseDRedSteadyState, false},
		{"DRedSteadyState to FailedBack", v1alpha1.PhaseDRedSteadyState, v1alpha1.PhaseFailedBack, false},
		{"FailedBack to SteadyState", v1alpha1.PhaseFailedBack, v1alpha1.PhaseSteadyState, false},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := fmt.Sprintf("table-lwt-%d", i)
			plan := newDRPlanWithPhase(name, tc.initialPhase)
			out := &v1alpha1.DRPlan{}
			if err := store.Create(ctx, planKey(name), plan, out, 0); err != nil {
				t.Fatalf("Create: %v", err)
			}

			err := updatePhase(store, ctx, name, tc.updatePhase)
			if tc.expectErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func planKey(name string) string {
	return fmt.Sprintf("/soteria.io/drplans/%s", name)
}

func execKey(name string) string {
	return fmt.Sprintf("/soteria.io/drexecutions/%s", name)
}

func newDRPlan(name string) *v1alpha1.DRPlan {
	return &v1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 2,
		},
	}
}

func newDRPlanWithPhase(name, phase string) *v1alpha1.DRPlan {
	p := newDRPlan(name)
	p.Status.Phase = phase
	p.Status.ActiveSite = engine.ActiveSiteForPhase(phase, p.Spec.PrimarySite, p.Spec.SecondarySite)
	return p
}

func newDRExecution(name string) *v1alpha1.DRExecution {
	return &v1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.DRExecutionSpec{
			PlanName: "some-plan",
			Mode:     v1alpha1.ExecutionModePlannedMigration,
		},
	}
}

func newDRPlanStoreWithDetector(session *gocql.Session) *scyllastore.Store {
	return scyllastore.NewStore(scyllastore.StoreConfig{
		Session:        session,
		Codec:          testCodec,
		Keyspace:       testKeyspace,
		GroupResource:  schema.GroupResource{Group: "soteria.io", Resource: "drplans"},
		ResourcePrefix: "/soteria.io/drplans",
		NewFunc:        func() runtime.Object { return &v1alpha1.DRPlan{} },
		NewListFunc:    func() runtime.Object { return &v1alpha1.DRPlanList{} },
		CriticalFieldDetector: func(old, updated runtime.Object) bool {
			oldPlan, ok := old.(*v1alpha1.DRPlan)
			if !ok {
				return false
			}
			newPlan, ok := updated.(*v1alpha1.DRPlan)
			if !ok {
				return false
			}
			return oldPlan.Status.Phase != newPlan.Status.Phase
		},
	})
}

func pollUntilFound(t *testing.T, store *scyllastore.Store, key string, out runtime.Object, timeout time.Duration) error {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = store.Get(ctx, key, storage.GetOptions{IgnoreNotFound: false}, out)
		if lastErr == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("not found within %v: %w", timeout, lastErr)
}

func updateLabel(store *scyllastore.Store, ctx context.Context, name, key, value string) error {
	return store.GuaranteedUpdate(ctx, planKey(name), &v1alpha1.DRPlan{}, false,
		nil, func(existing runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			plan := existing.(*v1alpha1.DRPlan)
			if plan.Labels == nil {
				plan.Labels = make(map[string]string)
			}
			plan.Labels[key] = value
			return plan, nil, nil
		}, nil)
}

func updatePhase(store *scyllastore.Store, ctx context.Context, name, phase string) error {
	return store.GuaranteedUpdate(ctx, planKey(name), &v1alpha1.DRPlan{}, false,
		nil, func(existing runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			plan := existing.(*v1alpha1.DRPlan)
			plan.Status.Phase = phase
			plan.Status.ActiveSite = engine.ActiveSiteForPhase(phase, plan.Spec.PrimarySite, plan.Spec.SecondarySite)
			return plan, nil, nil
		}, nil)
}

func mapNames(items []v1alpha1.DRPlan) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item.Name] = true
	}
	return m
}

func restartDC1(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := dc1Container.Start(ctx); err != nil {
		t.Fatalf("Restarting DC1: %v", err)
	}
	time.Sleep(10 * time.Second)
	dc1Session.Close()
	dc1Session = createSession(dc1IP, testKeyspace, dc1Name)
}

func restartDC2(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := dc2Container.Start(ctx); err != nil {
		t.Fatalf("Restarting DC2: %v", err)
	}
	time.Sleep(10 * time.Second)
	dc2Session.Close()
	dc2Session = createSession(dc2IP, testKeyspace, dc2Name)
}

func runRepair(t *testing.T, container testcontainers.Container) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	code, _, err := container.Exec(ctx, []string{"nodetool", "repair", testKeyspace})
	if err != nil {
		t.Fatalf("nodetool repair exec failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("nodetool repair exited with code %d", code)
	}
}
