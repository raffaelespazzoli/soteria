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

package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gocql/gocql"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"

	v1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	scyllastore "github.com/soteria-project/soteria/pkg/storage/scylladb"
)

const watchTestKeyspace = "soteria_watch_test"

func setupWatchTest(t *testing.T) *scyllastore.Store {
	t.Helper()

	cfg := scyllastore.SchemaConfig{
		Keyspace:          watchTestKeyspace,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}
	if err := scyllastore.EnsureSchema(testSession, cfg); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}
	codecs := serializer.NewCodecFactory(scheme)
	codec := codecs.LegacyCodec(v1alpha1.SchemeGroupVersion)

	return scyllastore.NewStore(scyllastore.StoreConfig{
		Session:        testSession,
		Codec:          codec,
		Keyspace:       watchTestKeyspace,
		GroupResource:  schema.GroupResource{Group: "soteria.io", Resource: "drplans"},
		ResourcePrefix: "/soteria.io/drplans",
		NewFunc:        func() runtime.Object { return &v1alpha1.DRPlan{} },
		NewListFunc:    func() runtime.Object { return &v1alpha1.DRPlanList{} },
	})
}

func setupWatchTestForResource(t *testing.T, resource string, newFunc, newListFunc func() runtime.Object) *scyllastore.Store {
	t.Helper()

	cfg := scyllastore.SchemaConfig{
		Keyspace:          watchTestKeyspace,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}
	if err := scyllastore.EnsureSchema(testSession, cfg); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}
	codecs := serializer.NewCodecFactory(scheme)
	codec := codecs.LegacyCodec(v1alpha1.SchemeGroupVersion)

	return scyllastore.NewStore(scyllastore.StoreConfig{
		Session:        testSession,
		Codec:          codec,
		Keyspace:       watchTestKeyspace,
		GroupResource:  schema.GroupResource{Group: "soteria.io", Resource: resource},
		ResourcePrefix: "/soteria.io/" + resource,
		NewFunc:        newFunc,
		NewListFunc:    newListFunc,
	})
}

func watchCleanupKey(t *testing.T, key string) {
	t.Helper()
	kc, err := scyllastore.KeyToComponents(key)
	if err != nil {
		return
	}
	cql := fmt.Sprintf(
		`DELETE FROM %s.kv_store WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?`,
		watchTestKeyspace,
	)
	_ = testSession.Query(cql, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name).Exec()
}

func newWatchDRPlan(namespace, name string) *v1alpha1.DRPlan {
	return &v1alpha1.DRPlan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "soteria.io/v1alpha1",
			Kind:       "DRPlan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID(gocql.TimeUUID().String()),
		},
		Spec: v1alpha1.DRPlanSpec{
			VMSelector:             metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 2,
		},
	}
}

// expectEvent waits for an event on the watch channel with a generous timeout.
func expectEvent(t *testing.T, w watch.Interface, expectedType watch.EventType, timeout time.Duration) *watch.Event {
	t.Helper()
	select {
	case evt, ok := <-w.ResultChan():
		if !ok {
			t.Fatal("watch channel closed unexpectedly")
		}
		if evt.Type != expectedType {
			t.Fatalf("expected %v event, got %v (object: %T)", expectedType, evt.Type, evt.Object)
		}
		return &evt
	case <-time.After(timeout):
		t.Fatalf("timed out after %v waiting for %v event", timeout, expectedType)
		return nil
	}
}

// expectNoEvent verifies no event is received within the given duration.
func expectNoEvent(t *testing.T, w watch.Interface, dur time.Duration) {
	t.Helper()
	select {
	case evt, ok := <-w.ResultChan():
		if ok {
			t.Fatalf("unexpected event: type=%v object=%T", evt.Type, evt.Object)
		}
	case <-time.After(dur):
	}
}

const watchTimeout = 30 * time.Second

// TestWatch_ResourceVersion0_DeliversSnapshot verifies that a watch started
// with resourceVersion="0" delivers ADDED events for all existing objects.
func TestWatch_ResourceVersion0_DeliversSnapshot(t *testing.T) {
	store := setupWatchTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	key1 := "/soteria.io/drplans/default/watch-snap-1"
	key2 := "/soteria.io/drplans/default/watch-snap-2"
	t.Cleanup(func() { watchCleanupKey(t, key1); watchCleanupKey(t, key2) })

	if err := store.Create(ctx, key1, newWatchDRPlan("default", "watch-snap-1"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create 1 failed: %v", err)
	}
	if err := store.Create(ctx, key2, newWatchDRPlan("default", "watch-snap-2"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create 2 failed: %v", err)
	}

	w, err := store.Watch(ctx, "/soteria.io/drplans", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Stop()

	seen := make(map[string]bool)
	for i := 0; i < 2; i++ {
		evt := expectEvent(t, w, watch.Added, watchTimeout)
		plan := evt.Object.(*v1alpha1.DRPlan)
		seen[plan.Name] = true
	}

	if !seen["watch-snap-1"] || !seen["watch-snap-2"] {
		t.Fatalf("expected both snapshot objects, got %v", seen)
	}
}

// TestWatch_Create_DeliversAddedEvent verifies that creating an object
// after a watch starts delivers an ADDED event via CDC.
func TestWatch_Create_DeliversAddedEvent(t *testing.T) {
	store := setupWatchTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := store.Watch(ctx, "/soteria.io/drplans", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Stop()

	// Give the CDC reader a moment to start before creating.
	time.Sleep(2 * time.Second)

	key := "/soteria.io/drplans/default/watch-create"
	t.Cleanup(func() { watchCleanupKey(t, key) })

	if err := store.Create(ctx, key, newWatchDRPlan("default", "watch-create"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	evt := expectEvent(t, w, watch.Added, watchTimeout)
	plan := evt.Object.(*v1alpha1.DRPlan)
	if plan.Name != "watch-create" {
		t.Fatalf("expected name 'watch-create', got %q", plan.Name)
	}
}

// TestWatch_Update_DeliversModifiedEvent verifies that updating an object
// delivers a MODIFIED event.
func TestWatch_Update_DeliversModifiedEvent(t *testing.T) {
	store := setupWatchTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	key := "/soteria.io/drplans/default/watch-update"
	t.Cleanup(func() { watchCleanupKey(t, key) })

	if err := store.Create(ctx, key, newWatchDRPlan("default", "watch-update"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	w, err := store.Watch(ctx, "/soteria.io/drplans", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Stop()

	// Drain the snapshot ADDED event.
	expectEvent(t, w, watch.Added, watchTimeout)

	// Give the CDC reader a moment to start.
	time.Sleep(2 * time.Second)

	dest := &v1alpha1.DRPlan{}
	err = store.GuaranteedUpdate(ctx, key, dest, false, nil,
		func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			p := input.(*v1alpha1.DRPlan)
			p.Spec.MaxConcurrentFailovers = 99
			return p, nil, nil
		}, nil)
	if err != nil {
		t.Fatalf("GuaranteedUpdate failed: %v", err)
	}

	evt := expectEvent(t, w, watch.Modified, watchTimeout)
	plan := evt.Object.(*v1alpha1.DRPlan)
	if plan.Spec.MaxConcurrentFailovers != 99 {
		t.Fatalf("expected MaxConcurrentFailovers=99, got %d", plan.Spec.MaxConcurrentFailovers)
	}
}

// TestWatch_Delete_DeliversDeletedEvent verifies that deleting an object
// delivers a DELETED event with the full deleted object.
func TestWatch_Delete_DeliversDeletedEvent(t *testing.T) {
	store := setupWatchTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	key := "/soteria.io/drplans/default/watch-delete"
	t.Cleanup(func() { watchCleanupKey(t, key) })

	if err := store.Create(ctx, key, newWatchDRPlan("default", "watch-delete"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	w, err := store.Watch(ctx, "/soteria.io/drplans", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Stop()

	// Drain snapshot ADDED event (populates the object cache).
	expectEvent(t, w, watch.Added, watchTimeout)

	// Give CDC reader time to start.
	time.Sleep(2 * time.Second)

	out := &v1alpha1.DRPlan{}
	if err := store.Delete(ctx, key, out, nil, storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{}); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	evt := expectEvent(t, w, watch.Deleted, watchTimeout)
	plan := evt.Object.(*v1alpha1.DRPlan)
	if plan.Name != "watch-delete" {
		t.Fatalf("expected name 'watch-delete', got %q", plan.Name)
	}
}

// TestWatch_Stop_ClosesChannel verifies that stopping a watch closes its
// event channel.
func TestWatch_Stop_ClosesChannel(t *testing.T) {
	store := setupWatchTest(t)
	ctx := context.Background()

	w, err := store.Watch(ctx, "/soteria.io/drplans", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	w.Stop()

	select {
	case _, ok := <-w.ResultChan():
		if ok {
			// Might still get buffered events; drain them.
			for range w.ResultChan() {
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for channel closure after Stop()")
	}
}

// TestWatch_KeyPrefixFiltering verifies that a watch scoped to drplans
// does not receive events for drexecutions.
func TestWatch_KeyPrefixFiltering(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	planStore := setupWatchTest(t)
	execStore := setupWatchTestForResource(t, "drexecutions",
		func() runtime.Object { return &v1alpha1.DRExecution{} },
		func() runtime.Object { return &v1alpha1.DRExecutionList{} },
	)

	w, err := planStore.Watch(ctx, "/soteria.io/drplans", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Stop()

	// Give CDC reader time to start.
	time.Sleep(2 * time.Second)

	// Create an execution (different resource type).
	execKey := "/soteria.io/drexecutions/default/watch-exec"
	t.Cleanup(func() { watchCleanupKey(t, execKey) })

	exec := &v1alpha1.DRExecution{
		TypeMeta: metav1.TypeMeta{APIVersion: "soteria.io/v1alpha1", Kind: "DRExecution"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "watch-exec",
			UID:       types.UID(gocql.TimeUUID().String()),
		},
		Spec: v1alpha1.DRExecutionSpec{
			PlanName: "test-plan",
			Mode:     v1alpha1.ExecutionModePlannedMigration,
		},
	}
	if err := execStore.Create(ctx, execKey, exec, &v1alpha1.DRExecution{}, 0); err != nil {
		t.Fatalf("Create DRExecution failed: %v", err)
	}

	// Create a plan (matching resource type) — this should generate an event.
	planKey := "/soteria.io/drplans/default/watch-filter-plan"
	t.Cleanup(func() { watchCleanupKey(t, planKey) })
	if err := planStore.Create(ctx, planKey, newWatchDRPlan("default", "watch-filter-plan"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create DRPlan failed: %v", err)
	}

	// We should receive the DRPlan event, not the DRExecution.
	evt := expectEvent(t, w, watch.Added, watchTimeout)
	plan := evt.Object.(*v1alpha1.DRPlan)
	if plan.Name != "watch-filter-plan" {
		t.Fatalf("expected 'watch-filter-plan', got %q", plan.Name)
	}
}

// TestWatch_DRExecution_CRUD verifies watch events for DRExecution resources.
func TestWatch_DRExecution_CRUD(t *testing.T) {
	store := setupWatchTestForResource(t, "drexecutions",
		func() runtime.Object { return &v1alpha1.DRExecution{} },
		func() runtime.Object { return &v1alpha1.DRExecutionList{} },
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := store.Watch(ctx, "/soteria.io/drexecutions", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Stop()

	time.Sleep(2 * time.Second)

	key := "/soteria.io/drexecutions/default/watch-exec-crud"
	t.Cleanup(func() { watchCleanupKey(t, key) })

	exec := &v1alpha1.DRExecution{
		TypeMeta: metav1.TypeMeta{APIVersion: "soteria.io/v1alpha1", Kind: "DRExecution"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "watch-exec-crud",
			UID:       types.UID(gocql.TimeUUID().String()),
		},
		Spec: v1alpha1.DRExecutionSpec{
			PlanName: "test-plan",
			Mode:     v1alpha1.ExecutionModePlannedMigration,
		},
	}
	if err := store.Create(ctx, key, exec, &v1alpha1.DRExecution{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	evt := expectEvent(t, w, watch.Added, watchTimeout)
	got := evt.Object.(*v1alpha1.DRExecution)
	if got.Spec.PlanName != "test-plan" {
		t.Fatalf("expected PlanName 'test-plan', got %q", got.Spec.PlanName)
	}
}

// TestWatch_DRGroupStatus_CRUD verifies watch events for DRGroupStatus resources.
func TestWatch_DRGroupStatus_CRUD(t *testing.T) {
	store := setupWatchTestForResource(t, "drgroupstatuses",
		func() runtime.Object { return &v1alpha1.DRGroupStatus{} },
		func() runtime.Object { return &v1alpha1.DRGroupStatusList{} },
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := store.Watch(ctx, "/soteria.io/drgroupstatuses", storage.ListOptions{
		ResourceVersion: "0",
		Recursive:       true,
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Stop()

	time.Sleep(2 * time.Second)

	key := "/soteria.io/drgroupstatuses/default/watch-gs-crud"
	t.Cleanup(func() { watchCleanupKey(t, key) })

	gs := &v1alpha1.DRGroupStatus{
		TypeMeta: metav1.TypeMeta{APIVersion: "soteria.io/v1alpha1", Kind: "DRGroupStatus"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "watch-gs-crud",
			UID:       types.UID(gocql.TimeUUID().String()),
		},
		Spec: v1alpha1.DRGroupStatusSpec{
			ExecutionName: "exec-001",
			WaveIndex:     0,
			GroupName:     "group0",
			VMNames:       []string{"vm-1"},
		},
	}
	if err := store.Create(ctx, key, gs, &v1alpha1.DRGroupStatus{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	evt := expectEvent(t, w, watch.Added, watchTimeout)
	got := evt.Object.(*v1alpha1.DRGroupStatus)
	if got.Spec.GroupName != "group0" {
		t.Fatalf("expected GroupName 'group0', got %q", got.Spec.GroupName)
	}
}
