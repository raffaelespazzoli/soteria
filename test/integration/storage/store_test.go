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
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/storage"

	v1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	scyllastore "github.com/soteria-project/soteria/pkg/storage/scylladb"
)

const storeTestKeyspace = "soteria_store_test"

func setupStoreTest(t *testing.T) *scyllastore.Store {
	t.Helper()

	cfg := scyllastore.SchemaConfig{
		Keyspace:          storeTestKeyspace,
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
		Keyspace:       storeTestKeyspace,
		GroupResource:  schema.GroupResource{Group: "soteria.io", Resource: "drplans"},
		ResourcePrefix: "/soteria.io/drplans",
		NewFunc:        func() runtime.Object { return &v1alpha1.DRPlan{} },
		NewListFunc:    func() runtime.Object { return &v1alpha1.DRPlanList{} },
	})
}

func setupStoreForResource(t *testing.T, resource string, newFunc, newListFunc func() runtime.Object) *scyllastore.Store {
	t.Helper()

	cfg := scyllastore.SchemaConfig{
		Keyspace:          storeTestKeyspace,
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
		Keyspace:       storeTestKeyspace,
		GroupResource:  schema.GroupResource{Group: "soteria.io", Resource: resource},
		ResourcePrefix: "/soteria.io/" + resource,
		NewFunc:        newFunc,
		NewListFunc:    newListFunc,
	})
}

func cleanupKey(t *testing.T, key string) {
	t.Helper()
	kc, err := scyllastore.KeyToComponents(key)
	if err != nil {
		return
	}
	cql := fmt.Sprintf(
		`DELETE FROM %s.kv_store WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?`,
		storeTestKeyspace,
	)
	_ = testSession.Query(cql, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name).Exec()
}

func cleanupLabelRow(t *testing.T, apiGroup, resourceType, labelKey, labelValue, namespace, name string) {
	t.Helper()
	cql := fmt.Sprintf(
		`DELETE FROM %s.kv_store_labels WHERE api_group = ? AND resource_type = ?`+
			` AND label_key = ? AND label_value = ? AND namespace = ? AND name = ?`,
		storeTestKeyspace,
	)
	_ = testSession.Query(cql, apiGroup, resourceType, labelKey, labelValue, namespace, name).Exec()
}

func cleanupObjectLabels(t *testing.T, key string, lbls map[string]string) {
	t.Helper()
	kc, err := scyllastore.KeyToComponents(key)
	if err != nil {
		return
	}
	for k, v := range lbls {
		cleanupLabelRow(t, kc.APIGroup, kc.ResourceType, k, v, kc.Namespace, kc.Name)
	}
}

func newDRPlan(namespace, name string) *v1alpha1.DRPlan {
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
			WaveLabel:              "wave",
			MaxConcurrentFailovers: 2,
		},
	}
}

// ---- Create tests ----

func TestStore_Create_NewObject(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/create-new"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "create-new")
	out := &v1alpha1.DRPlan{}

	if err := store.Create(ctx, key, plan, out, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if out.Name != "create-new" {
		t.Fatalf("expected name 'create-new', got %q", out.Name)
	}
	if out.ResourceVersion == "" {
		t.Fatal("expected non-empty resourceVersion")
	}
}

func TestStore_Create_DuplicateKey_ReturnsAlreadyExists(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/create-dup"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "create-dup")
	if err := store.Create(ctx, key, plan, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	plan2 := newDRPlan("default", "create-dup")
	err := store.Create(ctx, key, plan2, &v1alpha1.DRPlan{}, 0)
	if !storage.IsExist(err) {
		t.Fatalf("expected KeyExists error, got %v", err)
	}
}

func TestStore_Create_ResourceVersionAssigned(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/create-rv"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "create-rv")
	out := &v1alpha1.DRPlan{}
	if err := store.Create(ctx, key, plan, out, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	rv, err := store.Versioner().ObjectResourceVersion(out)
	if err != nil {
		t.Fatalf("ObjectResourceVersion failed: %v", err)
	}
	if rv == 0 {
		t.Fatal("expected non-zero resourceVersion")
	}
}

func TestStore_Create_ResourceVersionSetOnCreate_Fails(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/create-rv-set"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "create-rv-set")
	plan.ResourceVersion = "12345"

	err := store.Create(ctx, key, plan, &v1alpha1.DRPlan{}, 0)
	if err != storage.ErrResourceVersionSetOnCreate {
		t.Fatalf("expected ErrResourceVersionSetOnCreate, got %v", err)
	}
}

// ---- Get tests ----

func TestStore_Get_ExistingObject(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/get-existing"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "get-existing")
	if err := store.Create(ctx, key, plan, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	out := &v1alpha1.DRPlan{}
	if err := store.Get(ctx, key, storage.GetOptions{}, out); err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if out.Name != "get-existing" {
		t.Fatalf("expected name 'get-existing', got %q", out.Name)
	}
	if out.Spec.WaveLabel != "wave" {
		t.Fatalf("expected waveLabel 'wave', got %q", out.Spec.WaveLabel)
	}
	if out.ResourceVersion == "" {
		t.Fatal("expected non-empty resourceVersion")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/get-missing"

	err := store.Get(ctx, key, storage.GetOptions{}, &v1alpha1.DRPlan{})
	if !storage.IsNotFound(err) {
		t.Fatalf("expected NotFound error, got %v", err)
	}
}

func TestStore_Get_IgnoreNotFound(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/get-ignore-missing"

	out := &v1alpha1.DRPlan{}
	err := store.Get(ctx, key, storage.GetOptions{IgnoreNotFound: true}, out)
	if err != nil {
		t.Fatalf("expected no error with IgnoreNotFound, got %v", err)
	}
	if out.Name != "" {
		t.Fatalf("expected zero-value object, got name %q", out.Name)
	}
}

// ---- GetList tests ----

func TestStore_GetList_MultipleResources(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	names := []string{"list-a", "list-b", "list-c"}
	for _, name := range names {
		key := "/soteria.io/drplans/default/" + name
		t.Cleanup(func() { cleanupKey(t, key) })
		if err := store.Create(ctx, key, newDRPlan("default", name), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", name, err)
		}
	}

	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label: labels.Everything(),
			Field: fields.Everything(),
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList failed: %v", err)
	}

	if len(list.Items) < len(names) {
		t.Fatalf("expected at least %d items, got %d", len(names), len(list.Items))
	}
	if list.ResourceVersion == "" {
		t.Fatal("expected non-empty list resourceVersion")
	}
}

func TestStore_GetList_NamespaceScoped(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	key1 := "/soteria.io/drplans/ns-a/plan1"
	key2 := "/soteria.io/drplans/ns-b/plan2"
	t.Cleanup(func() { cleanupKey(t, key1); cleanupKey(t, key2) })

	if err := store.Create(ctx, key1, newDRPlan("ns-a", "plan1"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create plan1 failed: %v", err)
	}
	if err := store.Create(ctx, key2, newDRPlan("ns-b", "plan2"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create plan2 failed: %v", err)
	}

	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans/ns-a", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label: labels.Everything(),
			Field: fields.Everything(),
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList ns-a failed: %v", err)
	}

	for _, item := range list.Items {
		if item.Namespace != "ns-a" {
			t.Fatalf("expected all items in ns-a, found item in %q", item.Namespace)
		}
	}
}

func TestStore_GetList_LabelSelector(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	plan1 := newDRPlan("default", "label-a")
	plan1.Labels = map[string]string{"tier": "frontend"}
	key1 := "/soteria.io/drplans/default/label-a"

	plan2 := newDRPlan("default", "label-b")
	plan2.Labels = map[string]string{"tier": "backend"}
	key2 := "/soteria.io/drplans/default/label-b"

	t.Cleanup(func() { cleanupKey(t, key1); cleanupKey(t, key2) })

	if err := store.Create(ctx, key1, plan1, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create plan1 failed: %v", err)
	}
	if err := store.Create(ctx, key2, plan2, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create plan2 failed: %v", err)
	}

	selector, _ := labels.Parse("tier=frontend")
	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList with label selector failed: %v", err)
	}

	for _, item := range list.Items {
		if item.Labels["tier"] != "frontend" {
			t.Fatalf("expected only 'frontend' items, got labels %v", item.Labels)
		}
	}
}

func TestStore_GetList_Pagination(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("page-%02d", i)
		key := "/soteria.io/drplans/default/" + name
		t.Cleanup(func() { cleanupKey(t, key) })
		if err := store.Create(ctx, key, newDRPlan("default", name), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", name, err)
		}
	}

	// First page: limit 2
	list1 := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label: labels.Everything(),
			Field: fields.Everything(),
			Limit: 2,
		},
	}, list1)
	if err != nil {
		t.Fatalf("GetList page 1 failed: %v", err)
	}
	if len(list1.Items) != 2 {
		t.Fatalf("expected 2 items on page 1, got %d", len(list1.Items))
	}
	if list1.Continue == "" {
		t.Fatal("expected continue token for page 1")
	}

	// Second page using continue token
	list2 := &v1alpha1.DRPlanList{}
	err = store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    labels.Everything(),
			Field:    fields.Everything(),
			Limit:    2,
			Continue: list1.Continue,
		},
	}, list2)
	if err != nil {
		t.Fatalf("GetList page 2 failed: %v", err)
	}
	if len(list2.Items) != 2 {
		t.Fatalf("expected 2 items on page 2, got %d", len(list2.Items))
	}

	// Verify no overlap between pages
	page1Names := make(map[string]bool)
	for _, item := range list1.Items {
		page1Names[item.Name] = true
	}
	for _, item := range list2.Items {
		if page1Names[item.Name] {
			t.Fatalf("item %q appears on both pages", item.Name)
		}
	}
}

// ---- GuaranteedUpdate tests ----

func TestStore_GuaranteedUpdate_Success(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/update-ok"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "update-ok")
	created := &v1alpha1.DRPlan{}
	if err := store.Create(ctx, key, plan, created, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	originalRV := created.ResourceVersion

	dest := &v1alpha1.DRPlan{}
	err := store.GuaranteedUpdate(ctx, key, dest, false, nil,
		func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			p := input.(*v1alpha1.DRPlan)
			p.Spec.MaxConcurrentFailovers = 10
			return p, nil, nil
		}, nil)
	if err != nil {
		t.Fatalf("GuaranteedUpdate failed: %v", err)
	}

	if dest.Spec.MaxConcurrentFailovers != 10 {
		t.Fatalf("expected MaxConcurrentFailovers=10, got %d", dest.Spec.MaxConcurrentFailovers)
	}
	if dest.ResourceVersion == originalRV {
		t.Fatal("expected resourceVersion to change after update")
	}
}

func TestStore_GuaranteedUpdate_NotFound(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/update-missing"

	err := store.GuaranteedUpdate(ctx, key, &v1alpha1.DRPlan{}, false, nil,
		func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			return input, nil, nil
		}, nil)
	if !storage.IsNotFound(err) {
		t.Fatalf("expected NotFound error, got %v", err)
	}
}

func TestStore_GuaranteedUpdate_IgnoreNotFound_Creates(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/update-create"
	t.Cleanup(func() { cleanupKey(t, key) })

	dest := &v1alpha1.DRPlan{}
	err := store.GuaranteedUpdate(ctx, key, dest, true, nil,
		func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			p := input.(*v1alpha1.DRPlan)
			p.Name = "update-create"
			p.Namespace = "default"
			p.Spec.WaveLabel = "wave"
			return p, nil, nil
		}, nil)
	if err != nil {
		t.Fatalf("GuaranteedUpdate with ignoreNotFound failed: %v", err)
	}

	// Verify the object was created by reading it back
	got := &v1alpha1.DRPlan{}
	if err := store.Get(ctx, key, storage.GetOptions{}, got); err != nil {
		t.Fatalf("Get after GuaranteedUpdate failed: %v", err)
	}
	if got.Spec.WaveLabel != "wave" {
		t.Fatalf("expected WaveLabel 'wave', got %q", got.Spec.WaveLabel)
	}
}

func TestStore_GuaranteedUpdate_NoChange_ShortCircuits(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/update-noop"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "update-noop")
	created := &v1alpha1.DRPlan{}
	if err := store.Create(ctx, key, plan, created, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	dest := &v1alpha1.DRPlan{}
	err := store.GuaranteedUpdate(ctx, key, dest, false, nil,
		func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			return input, nil, nil
		}, nil)
	if err != nil {
		t.Fatalf("GuaranteedUpdate no-op failed: %v", err)
	}

	if dest.ResourceVersion != created.ResourceVersion {
		t.Fatalf("expected unchanged resourceVersion %s, got %s", created.ResourceVersion, dest.ResourceVersion)
	}
}

// ---- Delete tests ----

func TestStore_Delete_ExistingObject(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/delete-ok"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "delete-ok")
	if err := store.Create(ctx, key, plan, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	out := &v1alpha1.DRPlan{}
	if err := store.Delete(ctx, key, out, nil, storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{}); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if out.Name != "delete-ok" {
		t.Fatalf("expected deleted object returned, got name %q", out.Name)
	}

	// Verify object is gone
	err := store.Get(ctx, key, storage.GetOptions{}, &v1alpha1.DRPlan{})
	if !storage.IsNotFound(err) {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestStore_Delete_NotFound(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/delete-missing"

	err := store.Delete(ctx, key, &v1alpha1.DRPlan{}, nil, storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{})
	if !storage.IsNotFound(err) {
		t.Fatalf("expected NotFound error, got %v", err)
	}
}

func TestStore_Delete_PreconditionUID(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/delete-uid"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "delete-uid")
	created := &v1alpha1.DRPlan{}
	if err := store.Create(ctx, key, plan, created, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	wrongUID := types.UID("wrong-uid")
	err := store.Delete(ctx, key, &v1alpha1.DRPlan{},
		&storage.Preconditions{UID: &wrongUID},
		storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{})
	if err == nil {
		t.Fatal("expected error for wrong UID precondition")
	}
}

func TestStore_Delete_PreconditionResourceVersion(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/delete-rv"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "delete-rv")
	if err := store.Create(ctx, key, plan, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	wrongRV := "999"
	err := store.Delete(ctx, key, &v1alpha1.DRPlan{},
		&storage.Preconditions{ResourceVersion: &wrongRV},
		storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{})
	if err == nil {
		t.Fatal("expected error for wrong resourceVersion precondition")
	}
}

// ---- Stats tests ----

func TestStore_Stats(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	key := "/soteria.io/drplans/default/stats-test"
	t.Cleanup(func() { cleanupKey(t, key) })

	if err := store.Create(ctx, key, newDRPlan("default", "stats-test"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.ObjectCount < 1 {
		t.Fatalf("expected at least 1 object, got %d", stats.ObjectCount)
	}
}

// ---- ReadinessCheck test ----

func TestStore_ReadinessCheck(t *testing.T) {
	store := setupStoreTest(t)
	if err := store.ReadinessCheck(); err != nil {
		t.Fatalf("ReadinessCheck failed: %v", err)
	}
}

// ---- Versioner monotonicity across Create/Update ----

func TestStore_ResourceVersion_Monotonically_Increases(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/rv-mono"
	t.Cleanup(func() { cleanupKey(t, key) })

	plan := newDRPlan("default", "rv-mono")
	created := &v1alpha1.DRPlan{}
	if err := store.Create(ctx, key, plan, created, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	createRV, _ := store.Versioner().ObjectResourceVersion(created)

	time.Sleep(1 * time.Millisecond)

	updated := &v1alpha1.DRPlan{}
	err := store.GuaranteedUpdate(ctx, key, updated, false, nil,
		func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			p := input.(*v1alpha1.DRPlan)
			p.Spec.MaxConcurrentFailovers = 99
			return p, nil, nil
		}, nil)
	if err != nil {
		t.Fatalf("GuaranteedUpdate failed: %v", err)
	}
	updateRV, _ := store.Versioner().ObjectResourceVersion(updated)

	if updateRV <= createRV {
		t.Fatalf("expected update RV (%d) > create RV (%d)", updateRV, createRV)
	}
}

// ---- Multi-resource-type tests ----

func TestStore_DRExecution_CRUD(t *testing.T) {
	store := setupStoreForResource(t, "drexecutions",
		func() runtime.Object { return &v1alpha1.DRExecution{} },
		func() runtime.Object { return &v1alpha1.DRExecutionList{} },
	)
	ctx := context.Background()
	key := "/soteria.io/drexecutions/default/exec-001"
	t.Cleanup(func() { cleanupKey(t, key) })

	exec := &v1alpha1.DRExecution{
		TypeMeta: metav1.TypeMeta{APIVersion: "soteria.io/v1alpha1", Kind: "DRExecution"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "exec-001",
			UID:       types.UID(gocql.TimeUUID().String()),
		},
		Spec: v1alpha1.DRExecutionSpec{
			PlanName: "erp-full-stack",
			Mode:     v1alpha1.ExecutionModePlannedMigration,
		},
	}

	out := &v1alpha1.DRExecution{}
	if err := store.Create(ctx, key, exec, out, 0); err != nil {
		t.Fatalf("Create DRExecution failed: %v", err)
	}
	if out.Spec.PlanName != "erp-full-stack" {
		t.Fatalf("expected PlanName 'erp-full-stack', got %q", out.Spec.PlanName)
	}

	got := &v1alpha1.DRExecution{}
	if err := store.Get(ctx, key, storage.GetOptions{}, got); err != nil {
		t.Fatalf("Get DRExecution failed: %v", err)
	}

	deleted := &v1alpha1.DRExecution{}
	if err := store.Delete(ctx, key, deleted, nil, storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{}); err != nil {
		t.Fatalf("Delete DRExecution failed: %v", err)
	}
}

func TestStore_DRGroupStatus_CRUD(t *testing.T) {
	store := setupStoreForResource(t, "drgroupstatuses",
		func() runtime.Object { return &v1alpha1.DRGroupStatus{} },
		func() runtime.Object { return &v1alpha1.DRGroupStatusList{} },
	)
	ctx := context.Background()
	key := "/soteria.io/drgroupstatuses/default/exec-001-wave0-group0"
	t.Cleanup(func() { cleanupKey(t, key) })

	gs := &v1alpha1.DRGroupStatus{
		TypeMeta: metav1.TypeMeta{APIVersion: "soteria.io/v1alpha1", Kind: "DRGroupStatus"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "exec-001-wave0-group0",
			UID:       types.UID(gocql.TimeUUID().String()),
		},
		Spec: v1alpha1.DRGroupStatusSpec{
			ExecutionName: "exec-001",
			WaveIndex:     0,
			GroupName:     "group0",
			VMNames:       []string{"vm-1", "vm-2"},
		},
	}

	out := &v1alpha1.DRGroupStatus{}
	if err := store.Create(ctx, key, gs, out, 0); err != nil {
		t.Fatalf("Create DRGroupStatus failed: %v", err)
	}
	if len(out.Spec.VMNames) != 2 {
		t.Fatalf("expected 2 VMNames, got %d", len(out.Spec.VMNames))
	}

	got := &v1alpha1.DRGroupStatus{}
	if err := store.Get(ctx, key, storage.GetOptions{}, got); err != nil {
		t.Fatalf("Get DRGroupStatus failed: %v", err)
	}

	deleted := &v1alpha1.DRGroupStatus{}
	if err := store.Delete(ctx, key, deleted, nil, storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{}); err != nil {
		t.Fatalf("Delete DRGroupStatus failed: %v", err)
	}
}

// Watch is no longer a stub — see watch_test.go for Watch integration tests.

// ---- Pagination resourceVersion stability (issue 2 regression) ----

func TestStore_GetList_Pagination_StableResourceVersion(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("rv-stable-%02d", i)
		key := "/soteria.io/drplans/default/" + name
		t.Cleanup(func() { cleanupKey(t, key) })
		if err := store.Create(ctx, key, newDRPlan("default", name), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", name, err)
		}
	}

	list1 := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label: labels.Everything(),
			Field: fields.Everything(),
			Limit: 2,
		},
	}, list1)
	if err != nil {
		t.Fatalf("GetList page 1 failed: %v", err)
	}
	if list1.Continue == "" {
		t.Fatal("expected continue token on page 1")
	}
	page1RV := list1.ResourceVersion

	list2 := &v1alpha1.DRPlanList{}
	err = store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    labels.Everything(),
			Field:    fields.Everything(),
			Limit:    2,
			Continue: list1.Continue,
		},
	}, list2)
	if err != nil {
		t.Fatalf("GetList page 2 failed: %v", err)
	}
	page2RV := list2.ResourceVersion

	if page1RV != page2RV {
		t.Fatalf(
			"expected stable resourceVersion across pages, "+
				"page1=%s page2=%s", page1RV, page2RV)
	}

	if list2.Continue != "" {
		list3 := &v1alpha1.DRPlanList{}
		err = store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
			Recursive: true,
			Predicate: storage.SelectionPredicate{
				Label:    labels.Everything(),
				Field:    fields.Everything(),
				Limit:    2,
				Continue: list2.Continue,
			},
		}, list3)
		if err != nil {
			t.Fatalf("GetList page 3 failed: %v", err)
		}
		if list3.ResourceVersion != page1RV {
			t.Fatalf(
				"expected stable resourceVersion on page 3, "+
					"page1=%s page3=%s", page1RV, list3.ResourceVersion)
		}
	}
}

// ---- Corrupt data surfacing (issue 3 regression) ----

func TestStore_GetList_CorruptRow_ReturnsError(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	name := "corrupt-row"
	key := "/soteria.io/drplans/default/" + name
	t.Cleanup(func() { cleanupKey(t, key) })

	cql := fmt.Sprintf(
		`INSERT INTO %s.kv_store`+
			` (api_group, resource_type, namespace, name, value, resource_version)`+
			` VALUES (?, ?, ?, ?, ?, ?)`,
		storeTestKeyspace,
	)
	if err := testSession.Query(cql,
		"soteria.io", "drplans", "default", name,
		[]byte("this is not valid encoded data"),
		gocql.TimeUUID(),
	).Exec(); err != nil {
		t.Fatalf("failed to insert corrupt row: %v", err)
	}

	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans/default", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label: labels.Everything(),
			Field: fields.Everything(),
		},
	}, list)
	if err == nil {
		t.Fatal("expected error from GetList with corrupt row, got nil")
	}
	if !storage.IsCorruptObject(err) {
		t.Fatalf("expected CorruptObject error, got %T: %v", err, err)
	}
}

func TestStore_Get_CorruptRow_ReturnsError(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	name := "corrupt-get"
	key := "/soteria.io/drplans/default/" + name
	t.Cleanup(func() { cleanupKey(t, key) })

	cql := fmt.Sprintf(
		`INSERT INTO %s.kv_store`+
			` (api_group, resource_type, namespace, name, value, resource_version)`+
			` VALUES (?, ?, ?, ?, ?, ?)`,
		storeTestKeyspace,
	)
	if err := testSession.Query(cql,
		"soteria.io", "drplans", "default", name,
		[]byte("garbage bytes"),
		gocql.TimeUUID(),
	).Exec(); err != nil {
		t.Fatalf("failed to insert corrupt row: %v", err)
	}

	out := &v1alpha1.DRPlan{}
	err := store.Get(ctx, key, storage.GetOptions{}, out)
	if err == nil {
		t.Fatal("expected error from Get with corrupt row, got nil")
	}
	if !storage.IsCorruptObject(err) {
		t.Fatalf("expected CorruptObject error, got %T: %v", err, err)
	}
}

// ---- GetCurrentResourceVersion test ----

func TestStore_GetCurrentResourceVersion(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()
	key := "/soteria.io/drplans/default/rv-current"
	t.Cleanup(func() { cleanupKey(t, key) })

	if err := store.Create(ctx, key, newDRPlan("default", "rv-current"), &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	rv, err := store.GetCurrentResourceVersion(ctx)
	if err != nil {
		t.Fatalf("GetCurrentResourceVersion failed: %v", err)
	}
	if rv == 0 {
		t.Fatal("expected non-zero current resource version")
	}
}

// ---- Label-indexed pagination tests (Story 1.3.1) ----

func newDRPlanWithLabels(namespace, name string, lbls map[string]string) *v1alpha1.DRPlan {
	plan := newDRPlan(namespace, name)
	plan.Labels = lbls
	return plan
}

func TestStore_LabelIndex_EqualityPagination(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	// Create 20 objects: 10 with app=web, 10 with app=api
	var allKeys []string
	var allLabels []map[string]string
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("li-web-%02d", i)
		key := "/soteria.io/drplans/default/" + name
		lbls := map[string]string{"app": "web"}
		allKeys = append(allKeys, key)
		allLabels = append(allLabels, lbls)
		t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })
		if err := store.Create(ctx, key, newDRPlanWithLabels("default", name, lbls), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", name, err)
		}
	}
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("li-api-%02d", i)
		key := "/soteria.io/drplans/default/" + name
		lbls := map[string]string{"app": "api"}
		allKeys = append(allKeys, key)
		allLabels = append(allLabels, lbls)
		t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })
		if err := store.Create(ctx, key, newDRPlanWithLabels("default", name, lbls), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", name, err)
		}
	}

	selector, _ := labels.Parse("app=web")

	// Page 1: limit=3
	list1 := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
			Limit:    3,
		},
	}, list1)
	if err != nil {
		t.Fatalf("GetList page 1 failed: %v", err)
	}
	if len(list1.Items) != 3 {
		t.Fatalf("expected 3 items on page 1, got %d", len(list1.Items))
	}
	for _, item := range list1.Items {
		if item.Labels["app"] != "web" {
			t.Fatalf("expected all items to have app=web, got %v", item.Labels)
		}
	}
	if list1.Continue == "" {
		t.Fatal("expected continue token for page 1")
	}

	// Page 2
	list2 := &v1alpha1.DRPlanList{}
	err = store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
			Limit:    3,
			Continue: list1.Continue,
		},
	}, list2)
	if err != nil {
		t.Fatalf("GetList page 2 failed: %v", err)
	}
	if len(list2.Items) != 3 {
		t.Fatalf("expected 3 items on page 2, got %d", len(list2.Items))
	}

	// Verify no overlap
	page1Names := make(map[string]bool)
	for _, item := range list1.Items {
		page1Names[item.Name] = true
	}
	for _, item := range list2.Items {
		if page1Names[item.Name] {
			t.Fatalf("item %q appears on both pages", item.Name)
		}
	}
}

func TestStore_LabelIndex_MultiLabelAND(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	objects := []struct {
		name   string
		labels map[string]string
	}{
		{"ml-both-01", map[string]string{"app": "web", "tier": "frontend"}},
		{"ml-both-02", map[string]string{"app": "web", "tier": "frontend"}},
		{"ml-app-only", map[string]string{"app": "web", "tier": "backend"}},
		{"ml-tier-only", map[string]string{"app": "api", "tier": "frontend"}},
		{"ml-neither", map[string]string{"app": "api", "tier": "backend"}},
	}

	for _, obj := range objects {
		key := "/soteria.io/drplans/default/" + obj.name
		lbls := obj.labels
		t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })
		if err := store.Create(ctx, key, newDRPlanWithLabels("default", obj.name, obj.labels), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", obj.name, err)
		}
	}

	selector, _ := labels.Parse("app=web,tier=frontend")
	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList multi-label AND failed: %v", err)
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items matching app=web,tier=frontend, got %d", len(list.Items))
	}
	for _, item := range list.Items {
		if item.Labels["app"] != "web" || item.Labels["tier"] != "frontend" {
			t.Fatalf("unexpected item labels: %v", item.Labels)
		}
	}
}

func TestStore_LabelIndex_InSelector(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	objects := []struct {
		name   string
		labels map[string]string
	}{
		{"in-frontend", map[string]string{"tier": "frontend"}},
		{"in-backend", map[string]string{"tier": "backend"}},
		{"in-middleware", map[string]string{"tier": "middleware"}},
	}

	for _, obj := range objects {
		key := "/soteria.io/drplans/default/" + obj.name
		lbls := obj.labels
		t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })
		if err := store.Create(ctx, key, newDRPlanWithLabels("default", obj.name, obj.labels), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", obj.name, err)
		}
	}

	selector, _ := labels.Parse("tier in (frontend,backend)")
	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList in selector failed: %v", err)
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items matching tier in (frontend,backend), got %d", len(list.Items))
	}
	for _, item := range list.Items {
		tier := item.Labels["tier"]
		if tier != "frontend" && tier != "backend" {
			t.Fatalf("unexpected tier label: %q", tier)
		}
	}
}

func TestStore_LabelIndex_ExistsSelector(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	objects := []struct {
		name   string
		labels map[string]string
	}{
		{"exists-has", map[string]string{"canary": "true"}},
		{"exists-has2", map[string]string{"canary": "false"}},
		{"exists-no", map[string]string{"app": "web"}},
	}

	for _, obj := range objects {
		key := "/soteria.io/drplans/default/" + obj.name
		lbls := obj.labels
		t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })
		if err := store.Create(ctx, key, newDRPlanWithLabels("default", obj.name, obj.labels), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", obj.name, err)
		}
	}

	selector, _ := labels.Parse("canary")
	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList exists selector failed: %v", err)
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items with canary label, got %d", len(list.Items))
	}
	for _, item := range list.Items {
		if _, ok := item.Labels["canary"]; !ok {
			t.Fatalf("expected canary label, got %v", item.Labels)
		}
	}
}

func TestStore_LabelIndex_NegativeSelector(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	objects := []struct {
		name   string
		labels map[string]string
	}{
		{"neg-frontend", map[string]string{"tier": "frontend"}},
		{"neg-backend", map[string]string{"tier": "backend"}},
		{"neg-middle", map[string]string{"tier": "middleware"}},
	}

	for _, obj := range objects {
		key := "/soteria.io/drplans/default/" + obj.name
		lbls := obj.labels
		t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })
		if err := store.Create(ctx, key, newDRPlanWithLabels("default", obj.name, obj.labels), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", obj.name, err)
		}
	}

	selector, _ := labels.Parse("tier!=backend")
	list := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList negative selector failed: %v", err)
	}

	for _, item := range list.Items {
		if item.Labels["tier"] == "backend" {
			t.Fatalf("expected no backend items, got %v", item.Labels)
		}
	}
}

func TestStore_LabelIndex_UpdateSyncsLabels(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	key := "/soteria.io/drplans/default/label-update"
	initialLabels := map[string]string{"app": "web", "tier": "frontend"}
	updatedLabels := map[string]string{"app": "web", "tier": "backend"}
	t.Cleanup(func() {
		cleanupKey(t, key)
		cleanupObjectLabels(t, key, initialLabels)
		cleanupObjectLabels(t, key, updatedLabels)
	})

	plan := newDRPlanWithLabels("default", "label-update", initialLabels)
	if err := store.Create(ctx, key, plan, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify initial labels via label selector
	selector, _ := labels.Parse("tier=frontend")
	list := &v1alpha1.DRPlanList{}
	if err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list); err != nil {
		t.Fatalf("GetList initial failed: %v", err)
	}
	found := false
	for _, item := range list.Items {
		if item.Name == "label-update" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected label-update in tier=frontend results before update")
	}

	// Update labels: change tier from frontend to backend
	dest := &v1alpha1.DRPlan{}
	err := store.GuaranteedUpdate(ctx, key, dest, false, nil,
		func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			p := input.(*v1alpha1.DRPlan)
			p.Labels = updatedLabels
			return p, nil, nil
		}, nil)
	if err != nil {
		t.Fatalf("GuaranteedUpdate failed: %v", err)
	}

	// After update: should NOT appear in tier=frontend results
	list2 := &v1alpha1.DRPlanList{}
	if err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list2); err != nil {
		t.Fatalf("GetList after update failed: %v", err)
	}
	for _, item := range list2.Items {
		if item.Name == "label-update" {
			t.Fatal("label-update should not appear in tier=frontend after label change")
		}
	}

	// Should appear in tier=backend results
	selector2, _ := labels.Parse("tier=backend")
	list3 := &v1alpha1.DRPlanList{}
	if err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector2,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list3); err != nil {
		t.Fatalf("GetList tier=backend failed: %v", err)
	}
	found = false
	for _, item := range list3.Items {
		if item.Name == "label-update" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected label-update in tier=backend results after update")
	}
}

func TestStore_LabelIndex_DeleteCleansUpLabels(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	key := "/soteria.io/drplans/default/label-delete"
	lbls := map[string]string{"app": "web"}
	t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })

	plan := newDRPlanWithLabels("default", "label-delete", lbls)
	if err := store.Create(ctx, key, plan, &v1alpha1.DRPlan{}, 0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify it appears in label query
	selector, _ := labels.Parse("app=web")
	list := &v1alpha1.DRPlanList{}
	if err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list); err != nil {
		t.Fatalf("GetList before delete failed: %v", err)
	}
	found := false
	for _, item := range list.Items {
		if item.Name == "label-delete" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected label-delete in app=web results before delete")
	}

	// Delete the object
	out := &v1alpha1.DRPlan{}
	if err := store.Delete(ctx, key, out, nil, storage.ValidateAllObjectFunc, nil, storage.DeleteOptions{}); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify label index row is cleaned up — the object should not appear
	// in label query results even via the index
	list2 := &v1alpha1.DRPlanList{}
	if err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list2); err != nil {
		t.Fatalf("GetList after delete failed: %v", err)
	}
	for _, item := range list2.Items {
		if item.Name == "label-delete" {
			t.Fatal("label-delete should not appear after deletion")
		}
	}
}

func TestStore_LabelIndex_PaginationStableResourceVersion(t *testing.T) {
	store := setupStoreTest(t)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("li-rv-%02d", i)
		key := "/soteria.io/drplans/default/" + name
		lbls := map[string]string{"app": "web"}
		t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })
		if err := store.Create(ctx, key, newDRPlanWithLabels("default", name, lbls), &v1alpha1.DRPlan{}, 0); err != nil {
			t.Fatalf("Create %s failed: %v", name, err)
		}
	}

	selector, _ := labels.Parse("app=web")
	list1 := &v1alpha1.DRPlanList{}
	err := store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
			Limit:    3,
		},
	}, list1)
	if err != nil {
		t.Fatalf("GetList page 1 failed: %v", err)
	}
	if list1.Continue == "" {
		t.Fatal("expected continue token on page 1")
	}
	page1RV := list1.ResourceVersion

	list2 := &v1alpha1.DRPlanList{}
	err = store.GetList(ctx, "/soteria.io/drplans", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
			Limit:    3,
			Continue: list1.Continue,
		},
	}, list2)
	if err != nil {
		t.Fatalf("GetList page 2 failed: %v", err)
	}
	page2RV := list2.ResourceVersion

	if page1RV != page2RV {
		t.Fatalf("expected stable resourceVersion across pages, page1=%s page2=%s", page1RV, page2RV)
	}
}

func TestStore_LabelIndex_DRExecution(t *testing.T) {
	store := setupStoreForResource(t, "drexecutions",
		func() runtime.Object { return &v1alpha1.DRExecution{} },
		func() runtime.Object { return &v1alpha1.DRExecutionList{} },
	)
	ctx := context.Background()

	exec := &v1alpha1.DRExecution{
		TypeMeta: metav1.TypeMeta{APIVersion: "soteria.io/v1alpha1", Kind: "DRExecution"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "exec-label-test",
			UID:       types.UID(gocql.TimeUUID().String()),
			Labels:    map[string]string{"mode": "planned"},
		},
		Spec: v1alpha1.DRExecutionSpec{
			PlanName: "erp-full-stack",
			Mode:     v1alpha1.ExecutionModePlannedMigration,
		},
	}

	key := "/soteria.io/drexecutions/default/exec-label-test"
	t.Cleanup(func() {
		cleanupKey(t, key)
		cleanupObjectLabels(t, key, map[string]string{"mode": "planned"})
	})

	if err := store.Create(ctx, key, exec, &v1alpha1.DRExecution{}, 0); err != nil {
		t.Fatalf("Create DRExecution failed: %v", err)
	}

	selector, _ := labels.Parse("mode=planned")
	list := &v1alpha1.DRExecutionList{}
	err := store.GetList(ctx, "/soteria.io/drexecutions", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList DRExecution with label failed: %v", err)
	}

	found := false
	for _, item := range list.Items {
		if item.Name == "exec-label-test" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected exec-label-test in mode=planned results")
	}
}

func TestStore_LabelIndex_DRGroupStatus(t *testing.T) {
	store := setupStoreForResource(t, "drgroupstatuses",
		func() runtime.Object { return &v1alpha1.DRGroupStatus{} },
		func() runtime.Object { return &v1alpha1.DRGroupStatusList{} },
	)
	ctx := context.Background()

	gs := &v1alpha1.DRGroupStatus{
		TypeMeta: metav1.TypeMeta{APIVersion: "soteria.io/v1alpha1", Kind: "DRGroupStatus"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "gs-label-test",
			UID:       types.UID(gocql.TimeUUID().String()),
			Labels:    map[string]string{"wave": "0"},
		},
		Spec: v1alpha1.DRGroupStatusSpec{
			ExecutionName: "exec-001",
			WaveIndex:     0,
			GroupName:     "group0",
			VMNames:       []string{"vm-1"},
		},
	}

	key := "/soteria.io/drgroupstatuses/default/gs-label-test"
	t.Cleanup(func() {
		cleanupKey(t, key)
		cleanupObjectLabels(t, key, map[string]string{"wave": "0"})
	})

	if err := store.Create(ctx, key, gs, &v1alpha1.DRGroupStatus{}, 0); err != nil {
		t.Fatalf("Create DRGroupStatus failed: %v", err)
	}

	selector, _ := labels.Parse("wave=0")
	list := &v1alpha1.DRGroupStatusList{}
	err := store.GetList(ctx, "/soteria.io/drgroupstatuses", storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{
			Label:    selector,
			Field:    fields.Everything(),
			GetAttrs: storage.DefaultNamespaceScopedAttr,
		},
	}, list)
	if err != nil {
		t.Fatalf("GetList DRGroupStatus with label failed: %v", err)
	}

	found := false
	for _, item := range list.Items {
		if item.Name == "gs-label-test" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected gs-label-test in wave=0 results")
	}
}
