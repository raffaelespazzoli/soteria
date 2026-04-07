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

package scylladb

import (
	"context"
	"sync"
	"testing"
	"time"

	scyllacdc "github.com/scylladb/scylla-cdc-go"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
)

// ---------- watcher tests ----------

func TestWatcher_ResultChan_ReceivesEvents(t *testing.T) {
	ctx := context.Background()
	w := newWatcher(ctx, 10)
	defer w.Stop()

	obj := &fakeObject{}
	obj.Name = "test-obj"

	w.sendEvent(watch.Event{Type: watch.Added, Object: obj})

	select {
	case evt := <-w.ResultChan():
		if evt.Type != watch.Added {
			t.Fatalf("expected Added event, got %v", evt.Type)
		}
		fo := evt.Object.(*fakeObject)
		if fo.Name != "test-obj" {
			t.Fatalf("expected name 'test-obj', got %q", fo.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestWatcher_Stop_ClosesChannel(t *testing.T) {
	ctx := context.Background()
	w := newWatcher(ctx, 10)

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(w.resultChan)
	}()
	w.Stop()

	// After stop and channel close, ResultChan should be drained.
	select {
	case _, ok := <-w.ResultChan():
		if ok {
			t.Fatal("expected channel to be closed or drained")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestWatcher_Stop_DoubleCallSafe(t *testing.T) {
	ctx := context.Background()
	w := newWatcher(ctx, 10)

	// Should not panic on double-stop.
	w.Stop()
	w.Stop()
}

func TestWatcher_SendEvent_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := newWatcher(ctx, 0) // buffer size 0 — sends will block

	cancel()

	ok := w.sendEvent(watch.Event{Type: watch.Added, Object: &fakeObject{}})
	if ok {
		t.Fatal("expected sendEvent to return false after context cancellation")
	}
}

// ---------- dedupSet tests ----------

func TestDedupSet_Add_And_ShouldSkip(t *testing.T) {
	d := newDedupSet()
	d.add("default", "plan-a", 100)

	if !d.shouldSkip("default", "plan-a", 100) {
		t.Fatal("expected duplicate to be skipped (same RV)")
	}
	if !d.shouldSkip("default", "plan-a", 50) {
		t.Fatal("expected older event to be skipped")
	}
}

func TestDedupSet_ShouldSkip_NewerEventPassesThrough(t *testing.T) {
	d := newDedupSet()
	d.add("default", "plan-a", 100)

	if d.shouldSkip("default", "plan-a", 200) {
		t.Fatal("expected newer event to pass through")
	}
	// After passing through, the entry should be removed.
	if d.shouldSkip("default", "plan-a", 300) {
		t.Fatal("expected subsequent event to pass through after removal")
	}
}

func TestDedupSet_ShouldSkip_UnknownKey(t *testing.T) {
	d := newDedupSet()
	d.add("default", "plan-a", 100)

	if d.shouldSkip("default", "plan-b", 50) {
		t.Fatal("expected unknown key to pass through")
	}
}

func TestDedupSet_Clear_DisablesFiltering(t *testing.T) {
	d := newDedupSet()
	d.add("default", "plan-a", 100)
	d.clear()

	if d.shouldSkip("default", "plan-a", 50) {
		t.Fatal("expected no filtering after clear")
	}
}

func TestDedupSet_ConcurrentAccess(t *testing.T) {
	d := newDedupSet()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Go(func() {
			d.add("ns", "name", uint64(i))
			d.shouldSkip("ns", "name", uint64(i+1))
		})
	}
	wg.Wait()
}

// ---------- objectCache tests ----------

const testObjName = "plan-a"

func TestObjectCache_Set_And_GetAndDelete(t *testing.T) {
	c := newObjectCache()
	obj := &fakeObject{}
	obj.Name = testObjName

	c.set("default", testObjName, obj)

	got, ok := c.getAndDelete("default", testObjName)
	if !ok {
		t.Fatal("expected object to be found")
	}
	fo := got.(*fakeObject)
	if fo.Name != testObjName {
		t.Fatalf("expected name %q, got %q", testObjName, fo.Name)
	}

	_, ok = c.getAndDelete("default", testObjName)
	if ok {
		t.Fatal("expected object to be deleted after getAndDelete")
	}
}

func TestObjectCache_Get(t *testing.T) {
	c := newObjectCache()
	obj := &fakeObject{}
	obj.Name = testObjName

	c.set("default", testObjName, obj)

	got, ok := c.get("default", testObjName)
	if !ok {
		t.Fatal("expected object to be found")
	}
	fo := got.(*fakeObject)
	if fo.Name != testObjName {
		t.Fatalf("expected name %q, got %q", testObjName, fo.Name)
	}

	// get should not remove the object.
	_, ok = c.get("default", testObjName)
	if !ok {
		t.Fatal("expected object to still be present after get")
	}
}

func TestObjectCache_Get_MissingKey(t *testing.T) {
	c := newObjectCache()
	_, ok := c.get("default", "nonexistent")
	if ok {
		t.Fatal("expected false for missing key")
	}
}

func TestObjectCache_GetAndDelete_MissingKey(t *testing.T) {
	c := newObjectCache()
	_, ok := c.getAndDelete("default", "nonexistent")
	if ok {
		t.Fatal("expected false for missing key")
	}
}

func TestObjectCache_ConcurrentAccess(t *testing.T) {
	c := newObjectCache()
	var wg sync.WaitGroup

	for range 100 {
		wg.Go(func() {
			obj := &fakeObject{}
			obj.Name = "obj"
			c.set("ns", "obj", obj)
			c.getAndDelete("ns", "obj")
		})
	}
	wg.Wait()
}

// ---------- keyFilter tests ----------

const testAPIGroup = "soteria.io"
const testResourceType = "drplans"

func TestKeyFilter_Matches_ResourceType(t *testing.T) {
	f := keyFilter{apiGroup: testAPIGroup, resourceType: testResourceType}

	if !f.matches(testAPIGroup, testResourceType, "default") {
		t.Fatal("expected match for matching resource type")
	}
	if !f.matches(testAPIGroup, testResourceType, "other-ns") {
		t.Fatal("expected match for any namespace when filter namespace is empty")
	}
}

func TestKeyFilter_Matches_Namespace(t *testing.T) {
	f := keyFilter{apiGroup: testAPIGroup, resourceType: testResourceType, namespace: "default"}

	if !f.matches(testAPIGroup, testResourceType, "default") {
		t.Fatal("expected match for matching namespace")
	}
	if f.matches(testAPIGroup, testResourceType, "other-ns") {
		t.Fatal("expected no match for different namespace")
	}
}

func TestKeyFilter_NoMatch_DifferentResourceType(t *testing.T) {
	f := keyFilter{apiGroup: testAPIGroup, resourceType: testResourceType}

	if f.matches(testAPIGroup, "drexecutions", "default") {
		t.Fatal("expected no match for different resource type")
	}
}

func TestKeyFilter_NoMatch_DifferentAPIGroup(t *testing.T) {
	f := keyFilter{apiGroup: testAPIGroup, resourceType: testResourceType}

	if f.matches("other.io", testResourceType, "default") {
		t.Fatal("expected no match for different API group")
	}
}

// ---------- buildKeyFilter tests ----------

func TestBuildKeyFilter_Recursive(t *testing.T) {
	f, err := buildKeyFilter("/soteria.io/drplans/", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.apiGroup != testAPIGroup || f.resourceType != testResourceType {
		t.Fatalf("unexpected filter: %+v", f)
	}
	if f.namespace != "" {
		t.Fatalf("expected empty namespace for recursive list, got %q", f.namespace)
	}
	if f.name != "" {
		t.Fatalf("expected empty name for recursive list, got %q", f.name)
	}
}

func TestBuildKeyFilter_RecursiveWithNamespace(t *testing.T) {
	f, err := buildKeyFilter("/soteria.io/drplans/default/", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.namespace != "default" {
		t.Fatalf("expected namespace 'default', got %q", f.namespace)
	}
}

func TestBuildKeyFilter_NonRecursive(t *testing.T) {
	f, err := buildKeyFilter("/soteria.io/drplans/default/my-plan", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.apiGroup != testAPIGroup || f.resourceType != testResourceType {
		t.Fatalf("unexpected filter: %+v", f)
	}
	if f.namespace != "default" {
		t.Fatalf("expected namespace 'default', got %q", f.namespace)
	}
	if f.name != "my-plan" {
		t.Fatalf("expected name 'my-plan', got %q", f.name)
	}
}

// ---------- parseWatchResourceVersion tests ----------

func TestParseWatchResourceVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    uint64
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"12345", 12345, false},
		{"not-a-number", 0, true},
	}

	for _, tt := range tests {
		t.Run("input="+tt.input, func(t *testing.T) {
			got, err := parseWatchResourceVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

// ---------- watchProgressManager tests ----------

func TestWatchProgressManager_GetApplicationReadStartTime(t *testing.T) {
	start := time.Now()
	m := &watchProgressManager{startTime: start}

	got, err := m.GetApplicationReadStartTime(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(start) {
		t.Fatalf("expected %v, got %v", start, got)
	}
}

func TestWatchProgressManager_AllMethodsNoOp(t *testing.T) {
	m := &watchProgressManager{startTime: time.Now()}
	ctx := context.Background()

	if err := m.SaveApplicationReadStartTime(ctx, time.Now()); err != nil {
		t.Fatalf("SaveApplicationReadStartTime: %v", err)
	}
	if gen, err := m.GetCurrentGeneration(ctx); err != nil || !gen.IsZero() {
		t.Fatalf("GetCurrentGeneration: gen=%v err=%v", gen, err)
	}
	if err := m.StartGeneration(ctx, time.Now()); err != nil {
		t.Fatalf("StartGeneration: %v", err)
	}
	if _, err := m.GetProgress(ctx, time.Now(), "t", nil); err != nil {
		t.Fatalf("GetProgress: %v", err)
	}
	if err := m.SaveProgress(ctx, time.Now(), "t", nil, scyllacdc.Progress{}); err != nil {
		t.Fatalf("SaveProgress: %v", err)
	}
}

// ---------- predicateMatches tests ----------

func fakeGetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	fo := obj.(*fakeObject)
	return labels.Set(fo.Labels), fields.Set{"metadata.name": fo.Name}, nil
}

func TestPredicateMatches_NilGetAttrs(t *testing.T) {
	p := storage.SelectionPredicate{}
	if !predicateMatches(p, &fakeObject{}) {
		t.Fatal("expected true when GetAttrs is nil")
	}
}

func TestPredicateMatches_EmptySelector(t *testing.T) {
	p := storage.SelectionPredicate{
		Label:    labels.Everything(),
		Field:    fields.Everything(),
		GetAttrs: fakeGetAttrs,
	}
	if !predicateMatches(p, &fakeObject{}) {
		t.Fatal("expected true for empty selector")
	}
}

func TestPredicateMatches_MatchingLabel(t *testing.T) {
	sel, _ := labels.Parse("app=web")
	p := storage.SelectionPredicate{
		Label:    sel,
		Field:    fields.Everything(),
		GetAttrs: fakeGetAttrs,
	}
	obj := &fakeObject{}
	obj.Labels = map[string]string{"app": "web"}
	if !predicateMatches(p, obj) {
		t.Fatal("expected match")
	}
}

func TestPredicateMatches_NonMatchingLabel(t *testing.T) {
	sel, _ := labels.Parse("app=web")
	p := storage.SelectionPredicate{
		Label:    sel,
		Field:    fields.Everything(),
		GetAttrs: fakeGetAttrs,
	}
	obj := &fakeObject{}
	obj.Labels = map[string]string{"app": "api"}
	if predicateMatches(p, obj) {
		t.Fatal("expected no match")
	}
}

// ---------- initialEventsRequired tests ----------

func TestInitialEventsRequired_LegacyRV0(t *testing.T) {
	opts := storage.ListOptions{}
	if !initialEventsRequired(0, opts) {
		t.Fatal("expected true for legacy RV=0")
	}
}

func TestInitialEventsRequired_LegacyNonZeroRV(t *testing.T) {
	opts := storage.ListOptions{}
	if initialEventsRequired(100, opts) {
		t.Fatal("expected false for non-zero RV without SendInitialEvents")
	}
}

func TestInitialEventsRequired_ExplicitTrue(t *testing.T) {
	trueVal := true
	opts := storage.ListOptions{SendInitialEvents: &trueVal}
	if !initialEventsRequired(100, opts) {
		t.Fatal("expected true when SendInitialEvents=true")
	}
}

func TestInitialEventsRequired_ExplicitFalse(t *testing.T) {
	falseVal := false
	opts := storage.ListOptions{SendInitialEvents: &falseVal}
	if initialEventsRequired(0, opts) {
		t.Fatal("expected false when SendInitialEvents=false even with RV=0")
	}
}

// ---------- initialEventsEndBookmarkRequired tests ----------

func TestInitialEventsEndBookmarkRequired_True(t *testing.T) {
	trueVal := true
	opts := storage.ListOptions{
		SendInitialEvents: &trueVal,
		Predicate: storage.SelectionPredicate{
			AllowWatchBookmarks: true,
		},
	}
	if !initialEventsEndBookmarkRequired(opts) {
		t.Fatal("expected true")
	}
}

func TestInitialEventsEndBookmarkRequired_NoBookmarks(t *testing.T) {
	trueVal := true
	opts := storage.ListOptions{
		SendInitialEvents: &trueVal,
		Predicate:         storage.SelectionPredicate{},
	}
	if initialEventsEndBookmarkRequired(opts) {
		t.Fatal("expected false when AllowWatchBookmarks is false")
	}
}

func TestInitialEventsEndBookmarkRequired_NilSendInitial(t *testing.T) {
	opts := storage.ListOptions{
		Predicate: storage.SelectionPredicate{
			AllowWatchBookmarks: true,
		},
	}
	if initialEventsEndBookmarkRequired(opts) {
		t.Fatal("expected false when SendInitialEvents is nil")
	}
}

func TestInitialEventsEndBookmarkRequired_FalseSendInitial(t *testing.T) {
	falseVal := false
	opts := storage.ListOptions{
		SendInitialEvents: &falseVal,
		Predicate: storage.SelectionPredicate{
			AllowWatchBookmarks: true,
		},
	}
	if initialEventsEndBookmarkRequired(opts) {
		t.Fatal("expected false when SendInitialEvents=false")
	}
}
