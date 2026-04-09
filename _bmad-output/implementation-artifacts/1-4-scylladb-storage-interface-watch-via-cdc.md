# Story 1.4: ScyllaDB storage.Interface — Watch via CDC

Status: done

## Story

As a platform engineer,
I want real-time notifications when DR resources change,
so that controllers and Console clients receive updates within seconds via standard Kubernetes watch semantics.

## Acceptance Criteria

1. **Given** stored resources in the KV table, **When** `storage.Interface.Watch()` is called in `pkg/storage/scylladb/watch.go` with `resourceVersion=0`, **Then** an initial snapshot is delivered via SELECT of all matching resources, **And** subsequent changes are delivered via the ScyllaDB CDC stream using `scylla-cdc-go`, **And** the transition from snapshot to CDC is handled via an in-memory primary key deduplication set that filters duplicates during the overlap window.

2. **Given** an active watch, **When** a resource is created, updated, or deleted, **Then** the watch receives an ADDED, MODIFIED, or DELETED event respectively, **And** each event includes the full resource object with the current `resourceVersion`, **And** events are delivered within 5 seconds of the underlying change (NFR7).

3. **Given** a watch with a specific `resourceVersion` (resume from checkpoint), **When** the watch is established, **Then** only changes after that `resourceVersion` are delivered (no initial snapshot), **And** the CDC stream is consumed from the Timeuuid corresponding to the given resourceVersion.

4. **Given** the watch implementation, **When** integrated with `k8s.io/apiserver`'s cacher layer, **Then** the cacher wraps the storage.Interface watch as a single CDC consumer, **And** the cacher provides in-memory fan-out to multiple client watches, **And** API response times for list operations are under 2 seconds (NFR6) served from the cache.

5. **Given** the watch implementation, **When** integration tests run against ScyllaDB (testcontainers), **Then** watch events are received for all CRUD operations, **And** snapshot-to-CDC deduplication produces no duplicate events, **And** watch resume from a specific resourceVersion delivers only subsequent changes.

## Tasks / Subtasks

- [x] Task 1: Add scylla-cdc-go dependency (AC: #1, #2)
  - [x] 1.1 Add `github.com/scylladb/scylla-cdc-go` v1.2.1 to go.mod
  - [x] 1.2 Run `go mod tidy` to resolve dependency graph

- [x] Task 2: Implement Watcher struct (AC: #1, #2, #3)
  - [x] 2.1 Create `pkg/storage/scylladb/watch.go` with `watcher` struct implementing `watch.Interface`
  - [x] 2.2 Implement `watcher.ResultChan() <-chan watch.Event` — return the buffered event channel
  - [x] 2.3 Implement `watcher.Stop()` — cancel context, drain channel, close channel (safe for double-call)
  - [x] 2.4 Implement `newWatcher(ctx, cancel, bufferSize)` constructor — initialize channel, stopped flag, sync.Once for stop safety

- [x] Task 3: Implement CDC consumer (AC: #1, #2)
  - [x] 3.1 Create CDC `ChangeConsumer` implementation in `pkg/storage/scylladb/watch.go` that implements `scyllacdc.ChangeConsumer` interface (`Consume(ctx, change)` and `End()`)
  - [x] 3.2 Implement `ChangeConsumerFactory` that creates consumers bound to a specific watcher, key prefix filter, codec, and object cache
  - [x] 3.3 Implement CDC `OperationType` → `watch.EventType` mapping: Insert→Added, Update→Modified, RowDelete/PartitionDelete→Deleted
  - [x] 3.4 Extract primary key columns `(api_group, resource_type, namespace, name)` from CDC `ChangeRow`
  - [x] 3.5 For Insert/Update: extract `value` blob and `resource_version` Timeuuid from CDC Delta, decode to runtime.Object via codec, set resourceVersion, send event
  - [x] 3.6 For Delete: look up last-known object from in-memory object cache, send Deleted event with the cached object
  - [x] 3.7 Filter CDC events by key prefix — only forward events matching the watched `(api_group, resource_type, optional namespace)` tuple

- [x] Task 4: Implement in-memory object cache for DELETE events (AC: #2)
  - [x] 4.1 Create `objectCache` struct — `sync.RWMutex`-protected `map[string]runtime.Object` keyed by `namespace/name`
  - [x] 4.2 Implement `set(namespace, name string, obj runtime.Object)` — called on every ADDED/MODIFIED event
  - [x] 4.3 Implement `getAndDelete(namespace, name string) (runtime.Object, bool)` — called on DELETE events
  - [x] 4.4 Populate cache during initial snapshot (all snapshot objects are added)
  - [x] 4.5 Update cache on every ADDED/MODIFIED CDC event

- [x] Task 5: Implement snapshot-to-CDC deduplication (AC: #1)
  - [x] 5.1 Create `dedupSet` struct — `sync.RWMutex`-protected `map[string]uint64` keyed by `namespace/name`, value is resourceVersion
  - [x] 5.2 During snapshot: populate dedup set with `(namespace, name) → resourceVersion` for every object returned by SELECT
  - [x] 5.3 During CDC consumption: for each event, check dedup set — if PK exists with RV >= event's RV, skip the event (duplicate)
  - [x] 5.4 If PK exists with RV < event's RV, the event is newer — process normally, remove PK from dedup set
  - [x] 5.5 After the dedup window expires (confidence window + margin), clear remaining dedup entries to free memory

- [x] Task 6: Implement Watch() on Store (AC: #1, #2, #3)
  - [x] 6.1 Replace Watch() stub in `pkg/storage/scylladb/store.go` with real implementation
  - [x] 6.2 Parse key into `(apiGroup, resourceType, optionalNamespace, optionalName)` for filtering
  - [x] 6.3 Parse `opts.ResourceVersion` — `"0"` or empty triggers snapshot+CDC, specific version triggers CDC-only
  - [x] 6.4 Handle `opts.SendInitialEvents` — when set, send snapshot then BOOKMARK event
  - [x] 6.5 Create watcher, launch goroutine calling `watchLoop()`
  - [x] 6.6 Implement `watchLoop()`: orchestrate snapshot phase → CDC phase
  - [x] 6.7 Snapshot phase: call `GetList()` to fetch current objects, send as ADDED events, populate dedup set and object cache
  - [x] 6.8 Record snapshot completion timestamp for CDC reader start position
  - [x] 6.9 CDC phase: configure and start `scyllacdc.Reader` from the appropriate start time
  - [x] 6.10 Handle context cancellation — stop CDC reader and close watcher cleanly

- [x] Task 7: Implement CDC reader configuration (AC: #2, #3)
  - [x] 7.1 Build `scyllacdc.ReaderConfig` with the gocql session, table name `keyspace.kv_store`, and the consumer factory
  - [x] 7.2 Set `Consistency` to `gocql.LocalOne` for CDC log reads
  - [x] 7.3 Configure `AdvancedReaderConfig.ConfidenceWindowSize` to 2 seconds (CDC eventual consistency window)
  - [x] 7.4 Configure `AdvancedReaderConfig.PostNonEmptyQueryDelay` to 500ms for responsive event delivery
  - [x] 7.5 Configure `AdvancedReaderConfig.PostEmptyQueryDelay` to 1 second for polling idle streams
  - [x] 7.6 For resourceVersion=0 (snapshot+CDC): set `ChangeAgeLimit` to cover the snapshot overlap window
  - [x] 7.7 For specific resourceVersion (resume): implement custom `ProgressManager` that positions the CDC reader at the Timeuuid corresponding to the given resourceVersion

- [x] Task 8: Implement custom ProgressManager for watch resume (AC: #3)
  - [x] 8.1 Create `watchProgressManager` struct implementing `scyllacdc.ProgressManagerWithStartTime`
  - [x] 8.2 Implement `GetApplicationReadStartTime()` — return time corresponding to the watch's starting resourceVersion
  - [x] 8.3 Implement `SaveApplicationReadStartTime()` — no-op (watch doesn't persist progress)
  - [x] 8.4 Implement `GetCurrentGeneration()` — return zero (let library determine)
  - [x] 8.5 Implement `StartGeneration()` — no-op
  - [x] 8.6 Implement `GetProgress()` — return zero (no saved progress, start from generation beginning)
  - [x] 8.7 Implement `SaveProgress()` — no-op (watch doesn't persist progress across restarts; the cacher handles that)

- [x] Task 9: Unit tests (AC: #1, #2, #3)
  - [x] 9.1 Write unit tests in `pkg/storage/scylladb/watch_test.go`
  - [x] 9.2 Test watcher lifecycle: create, receive events, stop, double-stop safety
  - [x] 9.3 Test dedup set: duplicate filtering, newer event pass-through, expiry
  - [x] 9.4 Test object cache: set, get-and-delete, concurrent access
  - [x] 9.5 Test CDC operation type → watch event type mapping
  - [x] 9.6 Test key prefix filtering logic

- [x] Task 10: Integration tests with testcontainers (AC: #5)
  - [x] 10.1 Create `test/integration/storage/watch_test.go` — reuse testcontainers ScyllaDB lifecycle from Story 1.2's `suite_test.go`
  - [x] 10.2 Test Watch with resourceVersion=0 — create objects, start watch, verify ADDED events for all existing objects
  - [x] 10.3 Test Watch receives ADDED event for new Create after watch starts
  - [x] 10.4 Test Watch receives MODIFIED event for GuaranteedUpdate
  - [x] 10.5 Test Watch receives DELETED event for Delete (with correct object in event)
  - [x] 10.6 Test Watch with specific resourceVersion — create objects, note RV, create more objects, start watch from first RV, verify only subsequent events
  - [x] 10.7 Test snapshot-to-CDC deduplication — create objects, start watch at RV=0, create more objects during snapshot, verify no duplicates
  - [x] 10.8 Test Watch stop — start watch, stop it, verify channel is closed
  - [x] 10.9 Test Watch key prefix filtering — create objects of different resource types, verify only matching events received
  - [x] 10.10 Test all three resource types: DRPlan, DRExecution, DRGroupStatus

- [x] Task 11: Final validation
  - [x] 11.1 `make build` passes
  - [x] 11.2 `make test` passes (unit tests only)
  - [x] 11.3 `make lint` passes
  - [x] 11.4 `make integration` passes (testcontainers)

## Dev Notes

### Watch Architecture Overview

The `storage.Interface.Watch()` method is the most complex part of the ScyllaDB storage backend. It bridges ScyllaDB's CDC (Change Data Capture) with Kubernetes' watch semantics. The architecture has three layers:

```
Client watches (kubectl, Console, controllers)
         ↓
k8s.io/apiserver cacher (in-memory fan-out, event buffering)
         ↓
storage.Interface.Watch() — ONE underlying watch per resource type
         ↓
ScyllaDB CDC stream (scylla-cdc-go Reader)
```

The cacher layer creates exactly one Watch per resource type. The cacher then fans out events to all client watches in-memory. This means our Watch implementation only runs 3 CDC consumers total (one for drplans, one for drexecutions, one for drgroupstatuses).

### watch.Interface Contract

From `k8s.io/apimachinery/pkg/watch`:

```go
type Interface interface {
    Stop()
    ResultChan() <-chan Event
}

type Event struct {
    Type   EventType
    Object runtime.Object
}

// EventType values: Added, Modified, Deleted, Bookmark, Error
```

The Watch method signature from `k8s.io/apiserver/pkg/storage`:

```go
Watch(ctx context.Context, key string, opts ListOptions) (watch.Interface, error)
```

`ListOptions` includes:
- `ResourceVersion` — `"0"` for initial snapshot + stream, specific value for resume
- `SendInitialEvents` — when non-nil and true, send synthetic ADDED events for all current objects followed by a BOOKMARK
- `Predicate` — `SelectionPredicate` with label/field selectors (filtering done by cacher, not here)
- `Recursive` — true for prefix watch (e.g., all DRPlans), false for single object

### Watcher Implementation

```go
type watcher struct {
    resultChan chan watch.Event
    ctx        context.Context
    cancel     context.CancelFunc
    once       sync.Once
}

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
        // Drain and close in a goroutine to avoid blocking
        // if the producer is still writing
    })
}
```

**Buffer size:** Use 100 as the default buffer size. The cacher consumes events quickly, so the buffer rarely fills. If it does fill, the CDC consumer blocks until space is available (backpressure).

### Watch() Implementation Flow

```go
func (s *Store) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
    // 1. Parse key into (apiGroup, resourceType, namespace) for CDC event filtering
    // 2. Parse opts.ResourceVersion
    // 3. Create watcher
    // 4. Launch goroutine:
    //    a. If resourceVersion == "0" or SendInitialEvents:
    //       - Snapshot phase: GetList, send ADDED events, build dedup set + object cache
    //       - If SendInitialEvents: send BOOKMARK event with snapshot RV
    //       - CDC phase: start CDC reader from just before snapshot time
    //    b. If resourceVersion > 0:
    //       - No snapshot
    //       - CDC phase: start CDC reader from Timeuuid(resourceVersion)
    // 5. Return watcher
}
```

### Snapshot Phase

The snapshot provides the initial state when `resourceVersion=0`:

```go
func (s *Store) runSnapshot(ctx context.Context, w *watcher, key string, opts storage.ListOptions,
    dedup *dedupSet, cache *objectCache) (uint64, error) {
    // 1. Record pre-snapshot time: preSnapshotTime = time.Now()
    // 2. Call s.GetList(ctx, key, listOpts, listObj) to get all current objects
    // 3. For each item in the list:
    //    a. Get resourceVersion via s.versioner.ObjectResourceVersion(obj)
    //    b. Add to dedup set: dedup.add(namespace, name, resourceVersion)
    //    c. Add to object cache: cache.set(namespace, name, obj.DeepCopy())
    //    d. Send watch.Event{Type: watch.Added, Object: obj} to w.resultChan
    // 4. Get the list's collective resourceVersion as snapshotRV
    // 5. If opts.SendInitialEvents != nil && *opts.SendInitialEvents:
    //    Send watch.Event{Type: watch.Bookmark, Object: bookmarkObj} with snapshotRV
    // 6. Return snapshotRV (used to calculate CDC start position)
}
```

**Important:** The snapshot uses `GetList()` from Story 1.3, which returns a collective resourceVersion (the max RV across all returned objects). This RV is used to seed the BOOKMARK event.

### CDC Phase — scylla-cdc-go Integration

The CDC phase uses `scylla-cdc-go` v1.2.1 to consume the CDC log:

```go
func (s *Store) runCDCReader(ctx context.Context, w *watcher, startTime time.Time,
    keyFilter keyFilter, dedup *dedupSet, cache *objectCache) error {

    factory := &watchConsumerFactory{
        watcher:   w,
        codec:     s.codec,
        versioner: s.versioner,
        keyFilter: keyFilter,
        dedup:     dedup,
        cache:     cache,
    }

    cfg := &scyllacdc.ReaderConfig{
        Session:               s.session,
        TableNames:            []string{s.keyspace + ".kv_store"},
        ChangeConsumerFactory: factory,
        Consistency:           gocql.LocalOne,
        Logger:                /* structured logger adapter */,
        Advanced: scyllacdc.AdvancedReaderConfig{
            ConfidenceWindowSize:    2 * time.Second,
            PostNonEmptyQueryDelay:  500 * time.Millisecond,
            PostEmptyQueryDelay:     1 * time.Second,
            ChangeAgeLimit:          30 * time.Second,
        },
    }

    // For resume from specific RV: use custom ProgressManager
    if startRV > 0 {
        cfg.ProgressManager = &watchProgressManager{startTime: startTime}
    }

    reader, err := scyllacdc.NewReader(ctx, cfg)
    if err != nil {
        return fmt.Errorf("creating CDC reader: %w", err)
    }

    // reader.Run blocks until ctx is cancelled or error
    return reader.Run(ctx)
}
```

### CDC ChangeConsumer Implementation

```go
type watchConsumer struct {
    watcher   *watcher
    codec     *codec
    versioner *versioner
    keyFilter keyFilter
    dedup     *dedupSet
    cache     *objectCache
}

func (c *watchConsumer) Consume(ctx context.Context, change scyllacdc.Change) error {
    for _, delta := range change.Delta {
        op := delta.GetOperation()

        // Extract primary key columns from the CDC change
        rawApiGroup, _ := delta.GetValue("api_group")
        rawResourceType, _ := delta.GetValue("resource_type")
        rawNamespace, _ := delta.GetValue("namespace")
        rawName, _ := delta.GetValue("name")

        apiGroup := derefString(rawApiGroup)
        resourceType := derefString(rawResourceType)
        namespace := derefString(rawNamespace)
        name := derefString(rawName)

        // Filter: only process events matching the watched key prefix
        if !c.keyFilter.matches(apiGroup, resourceType, namespace) {
            continue
        }

        // Extract resourceVersion from cdc$time (Timeuuid)
        rv := TimeuuidToResourceVersion(change.Time)

        // Dedup check: skip events already seen in the snapshot
        if c.dedup != nil && c.dedup.shouldSkip(namespace, name, rv) {
            continue
        }

        switch {
        case op == scyllacdc.Insert || op == scyllacdc.Update:
            // Extract the value blob from the delta
            rawValue, ok := delta.GetValue("value")
            if !ok || rawValue == nil {
                continue
            }
            valueBytes := rawValue.([]byte)

            // Decode the blob into a runtime.Object
            obj, err := runtime.Decode(c.codec, valueBytes)
            if err != nil {
                // Log error but don't stop the watch
                continue
            }

            // Set resourceVersion on the object
            c.versioner.UpdateObject(obj, rv)

            // Determine event type
            eventType := watch.Added
            if op == scyllacdc.Update {
                eventType = watch.Modified
            }

            // Update object cache (for future DELETE lookups)
            c.cache.set(namespace, name, obj.DeepCopyObject())

            // Send event
            select {
            case c.watcher.resultChan <- watch.Event{Type: eventType, Object: obj}:
            case <-ctx.Done():
                return ctx.Err()
            }

        case op == scyllacdc.RowDelete || op == scyllacdc.PartitionDelete:
            // Look up the deleted object from the in-memory cache
            obj, found := c.cache.getAndDelete(namespace, name)
            if !found {
                // Object not in cache — construct a minimal "tombstone" object
                // with just the key metadata. The cacher layer tolerates this.
                continue
            }

            // Set resourceVersion on the deleted object
            c.versioner.UpdateObject(obj, rv)

            select {
            case c.watcher.resultChan <- watch.Event{Type: watch.Deleted, Object: obj}:
            case <-ctx.Done():
                return ctx.Err()
            }
        }
    }
    return nil
}

func (c *watchConsumer) End() error {
    return nil
}
```

### CDC OperationType → watch.EventType Mapping

| ScyllaDB CDC OperationType | Value | watch.EventType | Notes |
|---|---|---|---|
| `scyllacdc.Insert` | 2 | `watch.Added` | New row created via INSERT |
| `scyllacdc.Update` | 1 | `watch.Modified` | Existing row updated via UPDATE (CAS) |
| `scyllacdc.RowDelete` | 3 | `watch.Deleted` | Single row deleted |
| `scyllacdc.PartitionDelete` | 4 | `watch.Deleted` | Entire partition deleted (unlikely for our use, but handle it) |
| `scyllacdc.PreImage` | 0 | — | Ignored (pre-image not enabled) |
| `scyllacdc.PostImage` | 9 | — | Ignored (post-image not enabled) |
| Range deletes (5–8) | 5–8 | — | Ignored (not applicable to our key structure) |

**Critical:** Our CRUD operations in Story 1.3 use INSERT for Create and UPDATE for GuaranteedUpdate. The Delete method uses DELETE. This maps cleanly to CDC operation types.

### In-Memory Object Cache for DELETE Events

CDC DELETE changes don't include the row's `value` blob — they only contain the primary key columns. The Kubernetes watch contract requires DELETE events to include the full deleted object. Solution: maintain an in-memory cache of the last-known state of all watched objects.

```go
type objectCache struct {
    mu      sync.RWMutex
    objects map[string]runtime.Object // key: "namespace/name"
}

func (c *objectCache) set(namespace, name string, obj runtime.Object) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.objects[namespace+"/"+name] = obj
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
```

The cache is populated during the snapshot phase and updated on every ADDED/MODIFIED event. At Soteria's scale (max 100 DRPlans + bounded DRExecutions + DRGroupStatuses), the memory footprint is negligible.

**Why not CDC pre-image?** Enabling `preimage: true` on the table doubles write amplification (every write generates an additional pre-image row in the CDC log). The in-memory cache approach avoids this overhead. The cache is always consistent because the cacher creates exactly one Watch per resource type, so there's exactly one cache instance that sees all events.

### Snapshot-to-CDC Deduplication

When starting a watch with `resourceVersion=0`, there's an overlap window between the snapshot (SELECT) completion and the CDC reader's starting position. Writes during this window could appear in both the snapshot result and the CDC stream.

```go
type dedupSet struct {
    mu      sync.RWMutex
    entries map[string]uint64 // key: "namespace/name", value: resourceVersion
    active  bool
}

func (d *dedupSet) add(namespace, name string, rv uint64) {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.entries[namespace+"/"+name] = rv
}

func (d *dedupSet) shouldSkip(namespace, name string, rv uint64) bool {
    d.mu.RLock()
    defer d.mu.RUnlock()
    if !d.active {
        return false
    }
    snapshotRV, exists := d.entries[namespace+"/"+name]
    if !exists {
        return false
    }
    return rv <= snapshotRV // skip if CDC event is not newer than snapshot
}

func (d *dedupSet) remove(namespace, name string) {
    d.mu.Lock()
    defer d.mu.Unlock()
    delete(d.entries, namespace+"/"+name)
}

func (d *dedupSet) clear() {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.entries = nil
    d.active = false
}
```

**Dedup lifecycle:**
1. Before snapshot: create empty dedup set, mark active
2. During snapshot: add each object's `(namespace, name) → resourceVersion`
3. During CDC: for each event, call `shouldSkip()` — if true, skip; if false and PK was in dedup set, remove it
4. After the confidence window (ConfidenceWindowSize + margin ≈ 5 seconds post-snapshot): call `clear()` to free memory

### CDC Reader Start Position

**For `resourceVersion=0` (snapshot + CDC):**
- Record `preSnapshotTime = time.Now()` before executing the snapshot SELECT
- Start the CDC reader with `ChangeAgeLimit` set to cover the overlap: `time.Since(preSnapshotTime) + 30*time.Second` (generous margin)
- The dedup set handles any duplicate events from the overlap

**For specific `resourceVersion` (resume):**
- Convert the resourceVersion (uint64 microseconds) back to `time.Time` using `time.UnixMicro(int64(rv))`
- Create a `watchProgressManager` with `GetApplicationReadStartTime()` returning this time
- The CDC reader starts from this position
- No snapshot phase, no dedup set needed

### Custom ProgressManager for Watch Resume

```go
type watchProgressManager struct {
    startTime time.Time
}

func (m *watchProgressManager) GetApplicationReadStartTime(_ context.Context) (time.Time, error) {
    return m.startTime, nil
}

func (m *watchProgressManager) SaveApplicationReadStartTime(_ context.Context, _ time.Time) error {
    return nil // no-op — watch doesn't persist state
}

func (m *watchProgressManager) GetCurrentGeneration(_ context.Context) (time.Time, error) {
    return time.Time{}, nil // zero → library determines generation
}

func (m *watchProgressManager) StartGeneration(_ context.Context, _ time.Time) error {
    return nil
}

func (m *watchProgressManager) GetProgress(_ context.Context, _ time.Time, _ string, _ scyllacdc.StreamID) (scyllacdc.Progress, error) {
    return scyllacdc.Progress{}, nil // no saved progress
}

func (m *watchProgressManager) SaveProgress(_ context.Context, _ time.Time, _ string, _ scyllacdc.StreamID, _ scyllacdc.Progress) error {
    return nil // no-op
}
```

The `watchProgressManager` implements `scyllacdc.ProgressManagerWithStartTime`. The `GetApplicationReadStartTime()` method tells the CDC reader where to start consuming. All save/load methods are no-ops because the watch doesn't persist progress — the k8s.io/apiserver cacher handles reconnection and progress tracking.

### Key Prefix Filtering

The CDC reader receives changes for the entire `kv_store` table (all resource types). Each Watch call is scoped to a specific resource type prefix. Filter events to only forward matching ones:

```go
type keyFilter struct {
    apiGroup     string
    resourceType string
    namespace    string // empty for cluster-wide watches
    name         string // empty for list watches
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
```

The cacher calls Watch with keys like:
- `/soteria.io/drplans/` → apiGroup=`soteria.io`, resourceType=`drplans`, recursive=true
- `/soteria.io/drplans/default/` → apiGroup=`soteria.io`, resourceType=`drplans`, namespace=`default`
- `/soteria.io/drplans/default/my-plan` → specific object

### CDC Logger Adapter

`scylla-cdc-go` expects a `Logger` interface with `Printf(format string, v ...interface{})`. Adapt to structured logging:

```go
type cdcLogAdapter struct {
    logger logr.Logger
}

func (a *cdcLogAdapter) Printf(format string, v ...interface{}) {
    a.logger.V(2).Info(fmt.Sprintf(format, v...))
}
```

Route all CDC library log output through V(2) (debug level) to avoid cluttering operator logs during normal operation.

### Extracting Values from CDC ChangeRow

ScyllaDB CDC represents column values as pointers for scalar types. The `value` column (blob) is represented as `[]byte`. The primary key columns (text) are represented as `*string`:

```go
func derefString(v interface{}) string {
    if v == nil {
        return ""
    }
    if s, ok := v.(*string); ok && s != nil {
        return *s
    }
    return ""
}
```

The `resource_version` column is a Timeuuid. In CDC changes, it appears as a `*gocql.UUID`. However, for the watch, we use `change.Time` (the `cdc$time` column) as the event's resourceVersion, since `cdc$time` is the authoritative timestamp for the CDC event.

**Important nuance:** The `resource_version` column in the kv_store table is the Timeuuid assigned by our Create/Update operations. The `cdc$time` column is the Timeuuid assigned by ScyllaDB when the CDC entry was written. These are typically very close but not identical. Use the `resource_version` column value from the Delta when available (INSERT/UPDATE), falling back to `change.Time` only if needed.

### BOOKMARK Events

When `opts.SendInitialEvents` is true, after sending all snapshot ADDED events, send a BOOKMARK event:

```go
// Construct a minimal object with just TypeMeta and ObjectMeta
bookmarkObj := /* create empty object of the watched type */
accessor, _ := meta.Accessor(bookmarkObj)
accessor.SetResourceVersion(fmt.Sprintf("%d", snapshotRV))

w.resultChan <- watch.Event{
    Type:   watch.Bookmark,
    Object: bookmarkObj,
}
```

The BOOKMARK tells the cacher "the initial list is complete, and subsequent events have resourceVersion > snapshotRV." The cacher uses this to switch from list mode to watch mode.

**Note:** Creating the bookmark object requires knowing the watched type. The Store receives a `runtime.NewObjectFunc` or similar from the apiserver wiring layer. If not available, use the predicate's object type. The exact mechanism follows the etcd3 watcher pattern.

### Integration with k8s.io/apiserver Cacher

The cacher layer (`k8s.io/apiserver/pkg/storage/cacher`) is configured in Story 1.5 when wiring the API server. The cacher:
1. Calls `storage.Interface.Watch()` once per resource type at startup
2. Uses `SendInitialEvents: true` to bootstrap its in-memory cache
3. Maintains a ring buffer of recent watch events
4. Fans out events to all client watches with their individual predicates
5. Handles resourceVersion bookkeeping for clients

**Story 1.4 does NOT wire the cacher** — that's Story 1.5's responsibility. Story 1.4 implements the underlying `Watch()` that the cacher calls. However, the implementation must satisfy the cacher's expectations:
- Deliver all events for the watched prefix (recursive)
- Include the full object in every event (including DELETE)
- Respect `SendInitialEvents` for bootstrap
- Deliver events with monotonically increasing resourceVersions within a DC

### File Organization

After this story, `pkg/storage/scylladb/` contains:

```
pkg/storage/scylladb/
├── doc.go           # from Story 1.1 (stub)
├── client.go        # from Story 1.2 — connection management
├── schema.go        # from Story 1.2 — keyspace and table DDL
├── store.go         # from Story 1.3 — storage.Interface: Create, Get, GetList, GuaranteedUpdate, Delete, Count
├── versioner.go     # from Story 1.3 — Timeuuid ↔ resourceVersion mapping
├── codec.go         # from Story 1.3 — runtime.Object serialization wrapper
├── keyutil.go       # from Story 1.3 — storage key parsing
├── pager.go         # from Story 1.3 — continue token encoding
├── watch.go         # NEW — Watch(), watcher, CDC consumer, dedup, object cache
├── versioner_test.go  # from Story 1.3
├── codec_test.go      # from Story 1.3
├── keyutil_test.go    # from Story 1.3
├── pager_test.go      # from Story 1.3
└── watch_test.go      # NEW — unit tests
```

Integration tests:

```
test/integration/storage/
├── suite_test.go    # from Story 1.2 — testcontainers lifecycle
├── client_test.go   # from Story 1.2
├── schema_test.go   # from Story 1.2
├── store_test.go    # from Story 1.3 — CRUD integration tests
└── watch_test.go    # NEW — Watch integration tests
```

### Dependencies

**New dependency added in this story:**
- `github.com/scylladb/scylla-cdc-go` v1.2.1 — CDC stream consumption library

**Existing dependencies from Stories 1.1–1.3:**
- `k8s.io/apiserver` — `storage.Interface`, `watch.Interface`
- `k8s.io/apimachinery` — `runtime.Object`, `watch.Event`, `watch.EventType`
- `github.com/gocql/gocql` — ScyllaDB driver, gocql.UUID (Timeuuid)
- `github.com/testcontainers/testcontainers-go` — integration test infrastructure

### Testing Strategy

**Unit tests** (`pkg/storage/scylladb/watch_test.go`):
- `TestWatcher_ResultChan_ReceivesEvents` — send events, verify received
- `TestWatcher_Stop_ClosesChannel` — stop watcher, verify channel closed
- `TestWatcher_Stop_DoubleCallSafe` — call Stop() twice, no panic
- `TestDedupSet_Add_And_ShouldSkip` — add entry, verify duplicate detected
- `TestDedupSet_ShouldSkip_NewerEventPassesThrough` — CDC event with higher RV not skipped
- `TestDedupSet_Clear_DisablesFiltering` — after clear, nothing is filtered
- `TestObjectCache_Set_And_GetAndDelete` — set object, get it on delete
- `TestObjectCache_GetAndDelete_MissingKey` — returns false for unknown key
- `TestObjectCache_ConcurrentAccess` — parallel set/get operations
- `TestOperationTypeMapping` — verify all CDC op types map correctly
- `TestKeyFilter_Matches_ResourceType` — filter by api_group + resource_type
- `TestKeyFilter_Matches_Namespace` — filter by namespace
- `TestKeyFilter_NoMatch_DifferentResourceType` — verify non-matching events filtered

**Integration tests** (`test/integration/storage/watch_test.go`):
- Use `//go:build integration` tag
- Reuse testcontainers ScyllaDB lifecycle from Story 1.2's `suite_test.go`
- Test naming: `TestWatch_ResourceVersion0_DeliversSnapshot`, `TestWatch_Create_DeliversAddedEvent`, etc.
- Use real CRUD operations (Create, GuaranteedUpdate, Delete from Story 1.3) to generate events
- Verify events arrive within a reasonable timeout (10 seconds for integration tests to account for CDC polling delays)
- Use the API types from `pkg/apis/soteria.io/v1alpha1/` (DRPlan, DRExecution, DRGroupStatus)

**Integration test timing considerations:** CDC has eventual consistency — changes may take 1-3 seconds to appear in the CDC log (ConfidenceWindowSize + polling delay). Integration tests must use generous timeouts and retry/poll patterns rather than expecting immediate delivery. Use `time.After` with 10-second timeout and `select` on the watch channel.

### CDC Polling Latency vs NFR7

NFR7 requires updates visible within 5 seconds. The CDC polling configuration achieves this:
- `ConfidenceWindowSize`: 2 seconds (wait for eventual consistency)
- `PostNonEmptyQueryDelay`: 500ms (poll quickly when changes are arriving)
- `PostEmptyQueryDelay`: 1 second (poll interval when idle)

Worst case: 2s confidence + 1s poll = 3s. Typical case: 2s confidence + near-immediate delivery when changes are flowing. This meets NFR7 comfortably.

### Error Handling

- **CDC reader error:** If the CDC reader encounters a fatal error (connection loss, table missing), log the error and send a `watch.Error` event to the watcher channel. The cacher will handle reconnection.
- **Codec decode error:** If a CDC event contains a malformed blob that can't be decoded, log the error at V(1) and skip the event. Do not crash the watch.
- **Channel full:** If `w.resultChan` is full (producer faster than consumer), the CDC consumer blocks until space is available. This provides backpressure. If the context is cancelled while blocked, the consumer returns `ctx.Err()`.
- **Context cancellation:** When the watch context is cancelled (by Stop() or parent cancellation), the CDC reader is stopped via context propagation. The `reader.Run(ctx)` call returns, and the watch goroutine exits cleanly.

### Architecture Compliance

- **Storage boundary:** Only `pkg/storage/scylladb/` touches ScyllaDB — enforced
- **CDC library:** `scylla-cdc-go` v1.2.1 — as specified in architecture
- **Consistency:** CDC log reads use `LOCAL_ONE` — local DC only, no cross-DC dependency
- **Error wrapping:** lowercase, no punctuation, wrap with `%w`
- **Structured logging:** `log.FromContext(ctx).WithValues(...)` — no fmt.Println; CDC library output via V(2) adapter
- **Context propagation:** All methods accept and propagate `ctx` — never `context.Background()`
- **Test naming:** `TestFunction_Scenario_Expected`
- **Integration test tag:** `//go:build integration`

### Critical Warnings

1. **CDC eventual consistency:** CDC changes are not immediately visible. The `ConfidenceWindowSize` (default: 10 seconds in the library, we set 2 seconds) defines how old a change must be before it's considered safe to read. Setting this too low can cause missed events. 2 seconds is aggressive but acceptable for our use case — the dedup set handles any edge cases during the snapshot transition.

2. **CDC reader is blocking:** `reader.Run(ctx)` blocks until the context is cancelled or an error occurs. Always run it in a goroutine. Always pass a cancellable context.

3. **CDC stream IDs change on topology changes:** When ScyllaDB's topology changes (node added/removed), CDC stream IDs change (new "generation"). The `scylla-cdc-go` library handles this transparently — it detects new generations and creates new ChangeConsumers via the factory. Our factory must be stateless enough to handle this.

4. **Do NOT use `postimage: true` on the table.** Post-image doubles write amplification and is not needed — the `value` blob in INSERT/UPDATE deltas already contains the full serialized object.

5. **Do NOT use `preimage: true` on the table either.** Pre-image also doubles write amplification. Instead, maintain an in-memory object cache for DELETE event construction. The cache is consistent because the cacher creates exactly one Watch per resource type.

6. **Thread safety:** The CDC reader calls `Consume()` from multiple goroutines (one per stream). The watcher's `resultChan` is safe for concurrent sends (Go channels are goroutine-safe). The `dedupSet` and `objectCache` use `sync.RWMutex` for concurrent access.

7. **Watch channel must be closed after all events are sent.** When the watch goroutine exits (context cancelled, CDC reader stopped), close the `resultChan`. The cacher relies on channel closure to detect watch termination.

8. **gocql.UUID zero value:** Be careful with zero-value `gocql.UUID` when checking CDC timestamps. A zero UUID has timestamp 0 (Unix epoch). Always check for nil pointers when extracting values from CDC ChangeRow.

9. **Do NOT persist CDC progress.** The watch is ephemeral — when the pod restarts, the cacher re-creates the watch from the last known resourceVersion. The `watchProgressManager` is intentionally stateless. Using `TableBackedProgressManager` would create unnecessary ScyllaDB writes and complicate cleanup.

10. **scylla-cdc-go Consistency level:** The library defaults to `LOCAL_QUORUM` for CDC log reads. Override to `LOCAL_ONE` via `ReaderConfig.Consistency` to match our architecture's consistency model and avoid unnecessary quorum reads.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 1.4 (lines 426-461)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Data Architecture (lines 178-189)]
- [Source: _bmad-output/planning-artifacts/architecture.md — storage.Interface files (lines 412-419)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Implementation Sequence (lines 229-242)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Operational Flow (lines 559-576)]
- [Source: _bmad-output/project-context.md — ScyllaDB Storage Layer rules (lines 80-89)]
- [Source: _bmad-output/project-context.md — Testing Rules (lines 109-131)]
- [Source: _bmad-output/implementation-artifacts/1-2-scylladb-connection-generic-kv-schema.md — CDC enablement, testcontainers setup]
- [Source: _bmad-output/implementation-artifacts/1-3-scylladb-storage-interface-crud-operations.md — storage.Interface CRUD, versioner, codec]
- [External: k8s.io/apiserver/pkg/storage/interfaces.go — storage.Interface definition, Watch() contract]
- [External: k8s.io/apimachinery/pkg/watch — watch.Interface, Event, EventType]
- [External: k8s.io/apiserver/pkg/storage/cacher — cacher layer that wraps storage.Interface]
- [External: k8s.io/apiserver/pkg/storage/etcd3/watcher.go — Reference Watch implementation]
- [External: scylla-cdc-go v1.2.1 — https://pkg.go.dev/github.com/scylladb/scylla-cdc-go]
- [External: scylla-cdc-go examples — https://github.com/scylladb/scylla-cdc-go/tree/master/examples]
- [External: ScyllaDB CDC documentation — https://docs.scylladb.com/using-scylla/cdc/]
- [External: Daniel Mangum — K8s ASA: The Storage Interface — https://danielmangum.com/posts/k8s-asa-the-storage-interface/]

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (retroactively documented — story file was not synced during original dev session)

### Debug Log References

(Not captured during original dev session)

### Completion Notes List

- Implemented full CDC-based `Watch()` for `storage.Interface` using `scylla-cdc-go` v1.2.0
- `watcher` struct: buffered `resultChan`, context-cancelable, `sync.Once` on `Stop()` for double-call safety
- `objectCache`: `sync.RWMutex`-protected map for last-seen objects so DELETE CDC rows emit full objects
- `dedupSet`: snapshot-to-CDC deduplication — skips CDC events with RV <= snapshot RV, clears after confidence window
- `keyFilter`: filters CDC events by `(api_group, resource_type, optional namespace, optional name)`
- `watchConsumer` implements `scyllacdc.ChangeConsumer`: decodes value bytes, applies versioner, maps operation types to watch event types
- `watchConsumerFactory` implements `ChangeConsumerFactory`: one consumer per CDC stream
- `watchProgressManager` implements `ProgressManagerWithStartTime`: stateless (cacher handles reconnection)
- `cdcLogAdapter`: routes scylla-cdc-go logging to `klog` at V(2)
- `Watch()` method: parses key/RV, starts `watchLoop` goroutine, handles snapshot+CDC or CDC-only paths
- `watchLoop`: optional snapshot via `runSnapshot`/`snapshotSingleObject` + dedup window, then `runCDCReader` with `scyllacdc.Reader`
- CDC reader configured with `LocalOne` consistency, 2s confidence window, 500ms/1s polling delays
- Unit tests (531 lines): watcher lifecycle, dedupSet, objectCache, keyFilter, parseWatchResourceVersion, predicateMatches
- Integration tests (506 lines): RV=0 snapshot, create/update/delete events, stop behavior, key prefix filtering, multi-resource CRUD

### File List

- `pkg/storage/scylladb/watch.go` (new, ~838 lines)
- `pkg/storage/scylladb/watch_test.go` (new, ~531 lines)
- `test/integration/storage/watch_test.go` (new, ~506 lines)
- `go.mod` (modified — added `github.com/scylladb/scylla-cdc-go v1.2.0`)
- `go.sum` (modified)

## Change Log

- **2026-04-09**: Story file retroactively synced during Epic 1 retrospective — status, tasks, Dev Agent Record, File List backfilled from actual implementation
