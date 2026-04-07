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
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gocql/gocql"
	scyllacdc "github.com/scylladb/scylla-cdc-go"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"
)

const defaultWatchBufferSize = 100

// ---------- watcher ----------

// watcher implements watch.Interface using a buffered event channel.
type watcher struct {
	resultChan chan watch.Event
	ctx        context.Context
	cancel     context.CancelFunc
	once       sync.Once
}

var _ watch.Interface = (*watcher)(nil)

func newWatcher(ctx context.Context, bufferSize int) *watcher {
	ctx, cancel := context.WithCancel(ctx)
	return &watcher{
		resultChan: make(chan watch.Event, bufferSize),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (w *watcher) ResultChan() <-chan watch.Event {
	return w.resultChan
}

func (w *watcher) Stop() {
	w.once.Do(func() {
		w.cancel()
		go func() {
			// Drain remaining events so producers don't block.
			for range w.resultChan {
			}
		}()
	})
}

// sendEvent sends an event to the watcher's channel, respecting context cancellation.
func (w *watcher) sendEvent(evt watch.Event) bool {
	select {
	case w.resultChan <- evt:
		return true
	case <-w.ctx.Done():
		return false
	}
}

// ---------- objectCache ----------

// objectCache maintains the last-known state of watched objects so that
// DELETE events can include the full object (CDC deletes only carry PK columns).
type objectCache struct {
	mu      sync.RWMutex
	objects map[string]runtime.Object
}

func newObjectCache() *objectCache {
	return &objectCache{objects: make(map[string]runtime.Object)}
}

func (c *objectCache) set(namespace, name string, obj runtime.Object) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.objects[namespace+"/"+name] = obj
}

func (c *objectCache) get(namespace, name string) (runtime.Object, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	obj, ok := c.objects[namespace+"/"+name]
	return obj, ok
}

func (c *objectCache) getAndDelete(namespace, name string) (runtime.Object, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := namespace + "/" + name
	obj, ok := c.objects[key]
	if ok {
		delete(c.objects, key)
	}
	return obj, ok
}

// ---------- dedupSet ----------

// dedupSet tracks objects seen during the snapshot phase so that CDC events
// for the same objects during the overlap window are not delivered twice.
type dedupSet struct {
	mu      sync.RWMutex
	entries map[string]uint64
	active  bool
}

func newDedupSet() *dedupSet {
	return &dedupSet{
		entries: make(map[string]uint64),
		active:  true,
	}
}

func (d *dedupSet) add(namespace, name string, rv uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[namespace+"/"+name] = rv
}

// shouldSkip returns true if the CDC event is a duplicate already seen
// in the snapshot. If the event is newer, remove the entry from the dedup
// set and return false.
func (d *dedupSet) shouldSkip(namespace, name string, rv uint64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.active {
		return false
	}
	key := namespace + "/" + name
	snapshotRV, exists := d.entries[key]
	if !exists {
		return false
	}
	if rv <= snapshotRV {
		return true
	}
	delete(d.entries, key)
	return false
}

func (d *dedupSet) clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = nil
	d.active = false
}

// ---------- keyFilter ----------

// keyFilter restricts which CDC events are forwarded to the watcher based
// on the watched key prefix.
type keyFilter struct {
	apiGroup     string
	resourceType string
	namespace    string
	name         string
}

func (f keyFilter) matches(apiGroup, resourceType, namespace string) bool {
	if f.apiGroup != apiGroup || f.resourceType != resourceType {
		return false
	}
	if f.namespace != "" && f.namespace != namespace {
		return false
	}
	return true
}

// ---------- CDC log adapter ----------

// cdcLogAdapter bridges scylla-cdc-go's Logger interface to klog.
type cdcLogAdapter struct{}

func (cdcLogAdapter) Printf(format string, v ...any) {
	klog.V(2).InfoS(fmt.Sprintf(format, v...))
}

// ---------- watchConsumer ----------

// watchConsumer implements scyllacdc.ChangeConsumer. One instance is created
// per CDC stream within a generation.
type watchConsumer struct {
	w         *watcher
	codec     runtime.Codec
	versioner storage.Versioner
	filter    keyFilter
	dedup     *dedupSet
	cache     *objectCache
	predicate storage.SelectionPredicate
}

func (c *watchConsumer) Consume(ctx context.Context, change scyllacdc.Change) error {
	for _, delta := range change.Delta {
		op := delta.GetOperation()

		apiGroup := derefString(delta, "api_group")
		resourceType := derefString(delta, "resource_type")
		namespace := derefString(delta, "namespace")
		name := derefString(delta, "name")

		if !c.filter.matches(apiGroup, resourceType, namespace) {
			continue
		}
		if c.filter.name != "" && c.filter.name != name {
			continue
		}

		rv := extractResourceVersion(delta, change.Time)

		if c.dedup != nil && c.dedup.shouldSkip(namespace, name, rv) {
			continue
		}

		switch op {
		case scyllacdc.Insert, scyllacdc.Update:
			if err := c.consumeUpsert(ctx, delta, namespace, name, rv); err != nil {
				return err
			}
		case scyllacdc.RowDelete, scyllacdc.PartitionDelete:
			if err := c.consumeDelete(ctx, namespace, name, rv); err != nil {
				return err
			}
		}
	}
	return nil
}

// consumeUpsert handles Insert/Update CDC operations. It determines the
// correct Kubernetes event type by comparing the new and old (cached)
// object against the watch predicate, supporting synthetic ADDED/DELETED
// events when an object transitions in or out of a selector scope.
func (c *watchConsumer) consumeUpsert(
	ctx context.Context, delta *scyllacdc.ChangeRow,
	namespace, name string, rv uint64,
) error {
	rawValue, ok := delta.GetValue("value")
	if !ok || rawValue == nil {
		return nil
	}
	valueBytes, ok := rawValue.([]byte)
	if !ok || len(valueBytes) == 0 {
		return nil
	}

	obj, _, err := c.codec.Decode(valueBytes, nil, nil)
	if err != nil {
		klog.V(1).ErrorS(err, "Failed to decode CDC event value",
			"namespace", namespace, "name", name)
		return nil
	}

	if err := c.versioner.UpdateObject(obj, rv); err != nil {
		klog.V(1).ErrorS(err, "Failed to update object resource version",
			"namespace", namespace, "name", name)
		return nil
	}

	newMatches := predicateMatches(c.predicate, obj)
	oldObj, hasOld := c.cache.get(namespace, name)
	oldMatches := hasOld && predicateMatches(c.predicate, oldObj)

	c.cache.set(namespace, name, obj.DeepCopyObject())

	var eventType watch.EventType
	switch {
	case newMatches && oldMatches:
		eventType = watch.Modified
	case newMatches && !oldMatches:
		eventType = watch.Added
	case !newMatches && oldMatches:
		eventType = watch.Deleted
	default:
		return nil
	}

	if !c.w.sendEvent(watch.Event{Type: eventType, Object: obj}) {
		return ctx.Err()
	}
	return nil
}

// consumeDelete handles RowDelete/PartitionDelete CDC operations.
func (c *watchConsumer) consumeDelete(
	ctx context.Context, namespace, name string, rv uint64,
) error {
	obj, found := c.cache.getAndDelete(namespace, name)
	if !found {
		return nil
	}

	if !predicateMatches(c.predicate, obj) {
		return nil
	}

	if err := c.versioner.UpdateObject(obj, rv); err != nil {
		klog.V(1).ErrorS(err, "Failed to update deleted object resource version",
			"namespace", namespace, "name", name)
		return nil
	}

	if !c.w.sendEvent(watch.Event{Type: watch.Deleted, Object: obj}) {
		return ctx.Err()
	}
	return nil
}

func (c *watchConsumer) End() error {
	return nil
}

// ---------- watchConsumerFactory ----------

// watchConsumerFactory implements scyllacdc.ChangeConsumerFactory. All
// consumers share the same watcher, codec, and caches.
type watchConsumerFactory struct {
	w         *watcher
	codec     runtime.Codec
	versioner storage.Versioner
	filter    keyFilter
	dedup     *dedupSet
	cache     *objectCache
	predicate storage.SelectionPredicate
}

func (f *watchConsumerFactory) CreateChangeConsumer(
	_ context.Context,
	_ scyllacdc.CreateChangeConsumerInput,
) (scyllacdc.ChangeConsumer, error) {
	return &watchConsumer{
		w:         f.w,
		codec:     f.codec,
		versioner: f.versioner,
		filter:    f.filter,
		dedup:     f.dedup,
		cache:     f.cache,
		predicate: f.predicate,
	}, nil
}

// ---------- watchProgressManager ----------

// watchProgressManager implements scyllacdc.ProgressManagerWithStartTime.
// All persistence methods are no-ops because the apiserver cacher handles
// reconnection and progress tracking.
type watchProgressManager struct {
	startTime time.Time
}

var _ scyllacdc.ProgressManagerWithStartTime = (*watchProgressManager)(nil)

func (m *watchProgressManager) GetApplicationReadStartTime(_ context.Context) (time.Time, error) {
	return m.startTime, nil
}

func (m *watchProgressManager) SaveApplicationReadStartTime(_ context.Context, _ time.Time) error {
	return nil
}

func (m *watchProgressManager) GetCurrentGeneration(_ context.Context) (time.Time, error) {
	return time.Time{}, nil
}

func (m *watchProgressManager) StartGeneration(_ context.Context, _ time.Time) error {
	return nil
}

func (m *watchProgressManager) GetProgress(
	_ context.Context, _ time.Time, _ string, _ scyllacdc.StreamID,
) (scyllacdc.Progress, error) {
	return scyllacdc.Progress{}, nil
}

func (m *watchProgressManager) SaveProgress(
	_ context.Context, _ time.Time, _ string, _ scyllacdc.StreamID, _ scyllacdc.Progress,
) error {
	return nil
}

// ---------- Store.Watch ----------

// Watch implements storage.Interface via ScyllaDB CDC.
func (s *Store) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	preparedKey, err := s.prepareKey(key, opts.Recursive)
	if err != nil {
		return nil, err
	}

	filter, err := buildKeyFilter(preparedKey, opts.Recursive)
	if err != nil {
		return nil, storage.NewInternalError(err)
	}

	startRV, err := parseWatchResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}

	w := newWatcher(ctx, defaultWatchBufferSize)

	go func() {
		defer close(w.resultChan)
		if err := s.watchLoop(w, preparedKey, opts, filter, startRV); err != nil {
			if w.ctx.Err() == nil {
				klog.ErrorS(err, "Watch loop terminated with error",
					"key", preparedKey)
				errStatus := apierrors.NewInternalError(err)
				w.sendEvent(watch.Event{
					Type:   watch.Error,
					Object: &errStatus.ErrStatus,
				})
			}
		}
	}()

	return w, nil
}

func (s *Store) watchLoop(
	w *watcher, key string, opts storage.ListOptions,
	filter keyFilter, startRV uint64,
) error {
	var dedup *dedupSet
	cache := newObjectCache()
	var cdcStartTime time.Time

	snapshotNeeded := initialEventsRequired(startRV, opts)

	if snapshotNeeded {
		dedup = newDedupSet()
		preSnapshotTime := time.Now()

		snapshotRV, err := s.runSnapshot(w, key, opts, dedup, cache)
		if err != nil {
			return fmt.Errorf("snapshot phase: %w", err)
		}

		go func() {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
				dedup.clear()
			case <-w.ctx.Done():
			}
		}()

		cdcStartTime = preSnapshotTime.Add(-30 * time.Second)
		_ = snapshotRV
	} else {
		// Populate cache silently so that delete events can include
		// the full object body even without a snapshot phase.
		if err := s.populateCache(
			w.ctx, key, opts.Recursive, cache,
		); err != nil {
			return fmt.Errorf("populating object cache: %w", err)
		}
		if startRV > 0 {
			cdcStartTime = ResourceVersionToMinTimeuuid(startRV)
		} else {
			cdcStartTime = time.Now().Add(-5 * time.Second)
		}
	}

	return s.runCDCReader(
		w, cdcStartTime, filter, dedup, cache,
		opts.Predicate, snapshotNeeded,
	)
}

// runSnapshot executes the snapshot phase: list/get current objects and
// send them as ADDED events. Returns the collective snapshot resourceVersion.
func (s *Store) runSnapshot(
	w *watcher, key string, opts storage.ListOptions,
	dedup *dedupSet, cache *objectCache,
) (uint64, error) {
	if !opts.Recursive {
		return s.snapshotSingleObject(w, key, opts, dedup, cache)
	}

	listObj := s.newListFunc()
	listOpts := storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{},
	}

	if err := s.GetList(w.ctx, key, listOpts, listObj); err != nil {
		return 0, fmt.Errorf("listing objects for snapshot: %w", err)
	}

	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return 0, fmt.Errorf("getting items pointer: %w", err)
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil {
		return 0, fmt.Errorf("enforcing pointer: %w", err)
	}

	var maxRV uint64
	for i := 0; i < v.Len(); i++ {
		item := v.Index(i).Addr().Interface().(runtime.Object)
		rv, rvErr := s.versioner.ObjectResourceVersion(item)
		if rvErr != nil {
			return 0, fmt.Errorf("getting object resource version: %w", rvErr)
		}

		accessor, accErr := meta.Accessor(item)
		if accErr != nil {
			return 0, fmt.Errorf("accessing object metadata: %w", accErr)
		}
		ns := accessor.GetNamespace()
		name := accessor.GetName()

		dedup.add(ns, name, rv)
		cache.set(ns, name, item.DeepCopyObject())

		if predicateMatches(opts.Predicate, item) {
			if !w.sendEvent(watch.Event{
				Type: watch.Added, Object: item.DeepCopyObject(),
			}) {
				return 0, w.ctx.Err()
			}
		}

		if rv > maxRV {
			maxRV = rv
		}
	}

	listAccessor, err := meta.ListAccessor(listObj)
	if err == nil && listAccessor != nil {
		if rvStr := listAccessor.GetResourceVersion(); rvStr != "" {
			if parsed, parseErr := strconv.ParseUint(
				rvStr, 10, 64,
			); parseErr == nil && parsed > maxRV {
				maxRV = parsed
			}
		}
	}

	s.maybeSendInitialEventsBookmark(w, opts, maxRV)
	return maxRV, nil
}

// snapshotSingleObject handles the snapshot for a non-recursive
// (single-object) watch.
func (s *Store) snapshotSingleObject(
	w *watcher, key string, opts storage.ListOptions,
	dedup *dedupSet, cache *objectCache,
) (uint64, error) {
	obj := s.newFunc()
	err := s.Get(
		w.ctx, key, storage.GetOptions{IgnoreNotFound: true}, obj,
	)
	if err != nil {
		return 0, fmt.Errorf("getting object for snapshot: %w", err)
	}

	rv, rvErr := s.versioner.ObjectResourceVersion(obj)
	if rvErr != nil || rv == 0 {
		s.maybeSendInitialEventsBookmark(w, opts, 0)
		return 0, nil
	}

	accessor, accErr := meta.Accessor(obj)
	if accErr != nil {
		return 0, fmt.Errorf("accessing object metadata: %w", accErr)
	}
	ns := accessor.GetNamespace()
	name := accessor.GetName()

	dedup.add(ns, name, rv)
	cache.set(ns, name, obj.DeepCopyObject())

	if predicateMatches(opts.Predicate, obj) {
		if !w.sendEvent(watch.Event{
			Type: watch.Added, Object: obj.DeepCopyObject(),
		}) {
			return 0, w.ctx.Err()
		}
	}

	s.maybeSendInitialEventsBookmark(w, opts, rv)
	return rv, nil
}

func (s *Store) maybeSendInitialEventsBookmark(
	w *watcher, opts storage.ListOptions, maxRV uint64,
) {
	if !initialEventsEndBookmarkRequired(opts) {
		return
	}
	bookmarkObj := s.newFunc()
	bookmarkAccessor, accErr := meta.Accessor(bookmarkObj)
	if accErr != nil {
		return
	}
	bookmarkAccessor.SetResourceVersion(strconv.FormatUint(maxRV, 10))
	bookmarkAccessor.SetAnnotations(map[string]string{
		metav1.InitialEventsAnnotationKey: "true",
	})
	w.sendEvent(watch.Event{Type: watch.Bookmark, Object: bookmarkObj})
}

// runCDCReader configures and runs the scylla-cdc-go reader until the watch
// context is cancelled.
func (s *Store) runCDCReader(
	w *watcher, startTime time.Time, filter keyFilter,
	dedup *dedupSet, cache *objectCache,
	predicate storage.SelectionPredicate,
	useDedupChangeAgeLimit bool,
) error {
	factory := &watchConsumerFactory{
		w:         w,
		codec:     s.codec,
		versioner: s.versioner,
		filter:    filter,
		dedup:     dedup,
		cache:     cache,
		predicate: predicate,
	}

	cfg := &scyllacdc.ReaderConfig{
		Session:               s.session,
		TableNames:            []string{s.keyspace + ".kv_store"},
		ChangeConsumerFactory: factory,
		Consistency:           gocql.LocalOne,
		Logger:                cdcLogAdapter{},
		Advanced: scyllacdc.AdvancedReaderConfig{
			ConfidenceWindowSize:   2 * time.Second,
			PostNonEmptyQueryDelay: 500 * time.Millisecond,
			PostEmptyQueryDelay:    1 * time.Second,
		},
	}

	if useDedupChangeAgeLimit {
		cfg.Advanced.ChangeAgeLimit = 30 * time.Second
	} else {
		cfg.ProgressManager = &watchProgressManager{startTime: startTime}
	}

	reader, err := scyllacdc.NewReader(w.ctx, cfg)
	if err != nil {
		return fmt.Errorf("creating CDC reader: %w", err)
	}

	err = reader.Run(w.ctx)
	if err != nil && w.ctx.Err() != nil {
		return nil
	}
	return err
}

// ---------- helpers ----------

func buildKeyFilter(key string, recursive bool) (keyFilter, error) {
	if recursive {
		apiGroup, resourceType, namespace, err := KeyPrefixToComponents(key)
		if err != nil {
			return keyFilter{}, err
		}
		return keyFilter{
			apiGroup:     apiGroup,
			resourceType: resourceType,
			namespace:    namespace,
		}, nil
	}

	kc, err := KeyToComponents(key)
	if err != nil {
		return keyFilter{}, err
	}
	return keyFilter{
		apiGroup:     kc.APIGroup,
		resourceType: kc.ResourceType,
		namespace:    kc.Namespace,
		name:         kc.Name,
	}, nil
}

func parseWatchResourceVersion(rv string) (uint64, error) {
	if rv == "" || rv == "0" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(rv, 10, 64)
	if err != nil {
		return 0, storage.NewInternalError(fmt.Errorf("invalid resource version %q: %w", rv, err))
	}
	return parsed, nil
}

// derefString extracts a text column value from a CDC ChangeRow delta.
func derefString(delta *scyllacdc.ChangeRow, column string) string {
	v, ok := delta.GetValue(column)
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(*string); ok && s != nil {
		return *s
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// extractResourceVersion returns the resource version from a CDC delta,
// preferring the resource_version column (our assigned Timeuuid) and falling
// back to the change's cdc$time.
func extractResourceVersion(delta *scyllacdc.ChangeRow, changeTime gocql.UUID) uint64 {
	rv, ok := delta.GetValue("resource_version")
	if ok && rv != nil {
		switch v := rv.(type) {
		case *gocql.UUID:
			if v != nil {
				return TimeuuidToResourceVersion(*v)
			}
		case gocql.UUID:
			return TimeuuidToResourceVersion(v)
		}
	}
	return TimeuuidToResourceVersion(changeTime)
}

// predicateMatches returns true if obj satisfies the predicate, or if the
// predicate has no GetAttrs function and therefore cannot be evaluated.
func predicateMatches(
	p storage.SelectionPredicate, obj runtime.Object,
) bool {
	if p.GetAttrs == nil {
		return true
	}
	matched, err := p.Matches(obj)
	if err != nil {
		return false
	}
	return matched
}

func initialEventsRequired(
	startRV uint64, opts storage.ListOptions,
) bool {
	if opts.SendInitialEvents != nil {
		return *opts.SendInitialEvents
	}
	return startRV == 0
}

func initialEventsEndBookmarkRequired(opts storage.ListOptions) bool {
	return opts.SendInitialEvents != nil &&
		*opts.SendInitialEvents &&
		opts.Predicate.AllowWatchBookmarks
}

// populateCache loads current objects into the cache without sending events.
// This ensures delete events can include the full object body even when
// the watch starts without a snapshot phase.
func (s *Store) populateCache(
	ctx context.Context, key string, recursive bool, cache *objectCache,
) error {
	if !recursive {
		obj := s.newFunc()
		if err := s.Get(
			ctx, key, storage.GetOptions{IgnoreNotFound: true}, obj,
		); err != nil {
			return err
		}
		rv, rvErr := s.versioner.ObjectResourceVersion(obj)
		if rvErr != nil || rv == 0 {
			return nil
		}
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return err
		}
		cache.set(accessor.GetNamespace(), accessor.GetName(), obj)
		return nil
	}

	listObj := s.newListFunc()
	listOpts := storage.ListOptions{
		Recursive: true,
		Predicate: storage.SelectionPredicate{},
	}
	if err := s.GetList(ctx, key, listOpts, listObj); err != nil {
		return err
	}

	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil {
		return err
	}

	for i := 0; i < v.Len(); i++ {
		item := v.Index(i).Addr().Interface().(runtime.Object)
		accessor, accErr := meta.Accessor(item)
		if accErr != nil {
			continue
		}
		cache.set(
			accessor.GetNamespace(), accessor.GetName(),
			item.DeepCopyObject(),
		)
	}
	return nil
}
