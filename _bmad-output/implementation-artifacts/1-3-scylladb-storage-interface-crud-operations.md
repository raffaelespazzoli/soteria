# Story 1.3: ScyllaDB storage.Interface — CRUD Operations

Status: done

## Story

As a platform engineer,
I want to create, read, update, and delete DR resources in ScyllaDB via the Kubernetes API storage interface,
so that DR state is persisted reliably with conflict detection and proper resource versioning.

## Acceptance Criteria

1. **Given** the generic KV schema from Story 1.2, **When** `storage.Interface.Create()` is called in `pkg/storage/scylladb/store.go` with a DRPlan object, **Then** the object is serialized via `pkg/storage/scylladb/codec.go` and stored in the `kv_store` table, **And** a new Timeuuid is assigned as the resource version, **And** the returned object includes the assigned `resourceVersion`, **And** creating a resource with an existing key returns an `errors.NewAlreadyExists` error.

2. **Given** a stored DRPlan resource, **When** `storage.Interface.Get()` is called with the resource key, **Then** the object is deserialized from the blob and returned with the correct `resourceVersion`, **And** requesting a non-existent key returns an `errors.NewNotFound` error.

3. **Given** stored resources across multiple API groups and resource types, **When** `storage.Interface.GetList()` is called with a resource type prefix, **Then** all matching resources are returned as a list with a collective `resourceVersion`, **And** label selectors filter results correctly, **And** pagination via `continue` tokens works correctly.

4. **Given** a stored resource with a known `resourceVersion`, **When** `storage.Interface.GuaranteedUpdate()` is called with the matching `resourceVersion`, **Then** the resource is updated and a new `resourceVersion` (Timeuuid) is assigned, **And** if the provided `resourceVersion` does not match the stored version, a `errors.NewConflict` error is returned.

5. **Given** a stored resource, **When** `storage.Interface.Delete()` is called, **Then** the resource is removed from the `kv_store` table, **And** deleting a non-existent resource returns an `errors.NewNotFound` error, **And** precondition checks (UID, resourceVersion) are enforced.

6. **Given** the versioner in `pkg/storage/scylladb/versioner.go`, **When** a Timeuuid is converted to a resourceVersion string, **Then** the conversion produces Unix microseconds as an int64 formatted as a string, **And** the conversion is reversible (string → int64 → Timeuuid range), **And** resource versions are monotonically increasing within a single DC.

7. **Given** CRUD operations, **When** integration tests run against ScyllaDB (testcontainers), **Then** all operations pass for DRPlan, DRExecution, and DRGroupStatus resource types.

## Tasks / Subtasks

- [ ] Task 1: Implement Versioner (AC: #6)
  - [ ] 1.1 Create `pkg/storage/scylladb/versioner.go` implementing `storage.Versioner` interface
  - [ ] 1.2 Implement `UpdateObject(obj runtime.Object, resourceVersion uint64) error` — sets resourceVersion on ObjectMeta
  - [ ] 1.3 Implement `UpdateList(listObj runtime.Object, resourceVersion uint64, nextKey string, count *int64) error` — sets list resourceVersion and continue token
  - [ ] 1.4 Implement `PrepareObjectForStorage(obj runtime.Object) error` — clears resourceVersion before write
  - [ ] 1.5 Implement `ObjectResourceVersion(obj runtime.Object) (uint64, error)` — extracts resourceVersion
  - [ ] 1.6 Implement `ParseResourceVersion(resourceVersion string) (uint64, error)` — string → uint64
  - [ ] 1.7 Implement helper `TimeuuidToResourceVersion(timeuuid gocql.UUID) uint64` — Timeuuid → Unix microseconds
  - [ ] 1.8 Implement helper `ResourceVersionToTimeuuid(rv uint64) gocql.UUID` — Unix microseconds → Timeuuid (for range queries)
  - [ ] 1.9 Write unit tests in `pkg/storage/scylladb/versioner_test.go` — roundtrip conversion, monotonicity, edge cases

- [ ] Task 2: Implement Codec (AC: #1, #2)
  - [ ] 2.1 Create `pkg/storage/scylladb/codec.go` with `Codec` struct wrapping `runtime.Codec` from the apiserver serializer chain
  - [ ] 2.2 Implement `Encode(obj runtime.Object) ([]byte, error)` — serialize runtime.Object to protobuf or JSON bytes for blob storage
  - [ ] 2.3 Implement `Decode(data []byte, into runtime.Object) (runtime.Object, error)` — deserialize blob back to runtime.Object
  - [ ] 2.4 Implement `DecodeList(data []byte, into runtime.Object) error` — deserialize for list operations
  - [ ] 2.5 Write unit tests in `pkg/storage/scylladb/codec_test.go` — roundtrip encoding for DRPlan, DRExecution, DRGroupStatus

- [ ] Task 3: Implement key encoding helpers (AC: #1, #2, #3)
  - [ ] 3.1 Create `pkg/storage/scylladb/keyutil.go` with functions to parse storage keys into `(api_group, resource_type, namespace, name)` tuples
  - [ ] 3.2 Implement `KeyToComponents(key string) (apiGroup, resourceType, namespace, name string, err error)` — parses `/soteria.io/drplans/default/my-plan` format
  - [ ] 3.3 Implement `ComponentsToKey(apiGroup, resourceType, namespace, name string) string` — reverse of parse
  - [ ] 3.4 Implement `KeyPrefix(key string) (apiGroup, resourceType, namespace string, err error)` — extracts prefix for list operations
  - [ ] 3.5 Write unit tests in `pkg/storage/scylladb/keyutil_test.go`

- [ ] Task 4: Implement storage.Interface CRUD (AC: #1, #2, #3, #4, #5)
  - [ ] 4.1 Create `pkg/storage/scylladb/store.go` with `Store` struct holding gocql session, codec, versioner, and keyspace
  - [ ] 4.2 Implement `NewStore(session *gocql.Session, codec runtime.Codec, keyspace string) *Store`
  - [ ] 4.3 Implement `Versioner() storage.Versioner` — return the versioner instance
  - [ ] 4.4 Implement `Create(ctx, key, obj, out, ttl)` — INSERT with IF NOT EXISTS (LWT for AlreadyExists detection), assign Timeuuid, encode obj, write to kv_store, decode into out
  - [ ] 4.5 Implement `Get(ctx, key, opts, objPtr)` — SELECT by full primary key, decode blob, populate objPtr, handle NotFound
  - [ ] 4.6 Implement `GetList(ctx, key, opts, listObj)` — SELECT by partition key prefix, apply label selector filtering in-memory, implement continue token pagination, set collective resourceVersion
  - [ ] 4.7 Implement `GuaranteedUpdate(ctx, key, destination, ignoreNotFound, preconditions, tryUpdate, cachedExistingObject)` — read-modify-write loop with CAS on resource_version column for conflict detection, call tryUpdate repeatedly
  - [ ] 4.8 Implement `Delete(ctx, key, out, preconditions, validateDeletion, cachedExistingObject)` — read current, validate preconditions (UID, resourceVersion), run validateDeletion, DELETE from kv_store
  - [ ] 4.9 Implement `Count(key)` — SELECT count(*) for the partition
  - [ ] 4.10 Implement `Watch()` as a stub returning `storage.ErrResourceVersionNotSupported` — full Watch is Story 1.4
  - [ ] 4.11 Implement `RequestWatchProgress()` as a no-op stub — etcd-specific feature, not applicable to ScyllaDB

- [ ] Task 5: Implement continue token encoding for pagination (AC: #3)
  - [ ] 5.1 Create `pkg/storage/scylladb/pager.go` with continue token encode/decode — base64-encoded `(namespace, name)` pair
  - [ ] 5.2 Implement `EncodeContinue(namespace, name string) string`
  - [ ] 5.3 Implement `DecodeContinue(token string) (namespace, name string, err error)`
  - [ ] 5.4 Implement list pagination logic: use clustering column comparison `(namespace, name) > (?, ?)` in CQL for efficient server-side paging
  - [ ] 5.5 Write unit tests in `pkg/storage/scylladb/pager_test.go`

- [ ] Task 6: Integration tests with testcontainers (AC: #7)
  - [ ] 6.1 Create `test/integration/storage/store_test.go` — reuse testcontainers ScyllaDB lifecycle from Story 1.2's `suite_test.go`
  - [ ] 6.2 Test Create — new object, duplicate key (AlreadyExists), resourceVersion assigned
  - [ ] 6.3 Test Get — existing object, non-existent key (NotFound), correct deserialization
  - [ ] 6.4 Test GetList — multiple resources, namespace-scoped, cluster-wide, label selector filtering, pagination with continue tokens
  - [ ] 6.5 Test GuaranteedUpdate — successful update, stale resourceVersion (Conflict), ignoreNotFound
  - [ ] 6.6 Test Delete — existing object, non-existent key (NotFound), precondition checks (UID, resourceVersion)
  - [ ] 6.7 Test Count — correct count for partition
  - [ ] 6.8 Test Versioner — resourceVersion monotonically increases across Create/Update operations
  - [ ] 6.9 Test all three resource types: DRPlan, DRExecution, DRGroupStatus

- [ ] Task 7: Final validation
  - [ ] 7.1 `make build` passes
  - [ ] 7.2 `make test` passes (unit tests only)
  - [ ] 7.3 `make lint` passes
  - [ ] 7.4 `make integration` passes (testcontainers)

## Dev Notes

### storage.Interface — Complete Method Contract

The `k8s.io/apiserver/pkg/storage` interface defines 8 methods. This story implements all except `Watch` (Story 1.4). The store lives in `pkg/storage/scylladb/store.go`.

**Method signatures (from `k8s.io/apiserver/pkg/storage/interfaces.go`):**

```go
type Interface interface {
    Versioner() Versioner

    Create(ctx context.Context, key string, obj, out runtime.Object, ttl uint64) error

    Delete(ctx context.Context, key string, out runtime.Object, preconditions *Preconditions,
        validateDeletion ValidateObjectFunc, cachedExistingObject runtime.Object) error

    Watch(ctx context.Context, key string, opts ListOptions) (watch.Interface, error)

    Get(ctx context.Context, key string, opts GetOptions, objPtr runtime.Object) error

    GetList(ctx context.Context, key string, opts ListOptions, listObj runtime.Object) error

    GuaranteedUpdate(ctx context.Context, key string, destination runtime.Object,
        ignoreNotFound bool, preconditions *Preconditions,
        tryUpdate UpdateFunc, cachedExistingObject runtime.Object) error

    Count(key string) (int64, error)
}
```

**Watch stub:** Return `storage.NewInternalError("watch not yet implemented")` or similar. The cacher layer from `k8s.io/apiserver` will wrap the storage.Interface, so Watch must exist. Story 1.4 replaces the stub with the CDC-based implementation.

**RequestWatchProgress:** This method may exist on the interface in newer k8s versions. If present, implement as a no-op — it's an etcd-specific optimization not applicable to ScyllaDB.

### Versioner — Timeuuid ↔ ResourceVersion Mapping

The `storage.Versioner` interface is:

```go
type Versioner interface {
    UpdateObject(obj runtime.Object, resourceVersion uint64) error
    UpdateList(obj runtime.Object, resourceVersion uint64, nextKey string, count *int64) error
    PrepareObjectForStorage(obj runtime.Object) error
    ObjectResourceVersion(obj runtime.Object) (uint64, error)
    ParseResourceVersion(resourceVersion string) (uint64, error)
}
```

**Timeuuid → Unix microseconds mapping:**

ScyllaDB Timeuuid contains an embedded timestamp. Extract it and convert to Unix microseconds (int64):

```go
func TimeuuidToResourceVersion(id gocql.UUID) uint64 {
    // gocql.UUID is a [16]byte Timeuuid (version 1 UUID)
    // Extract the 60-bit timestamp from the UUID fields
    // Convert from UUID epoch (October 15, 1582) to Unix epoch
    // Return as microseconds
    t := id.Time()  // gocql provides .Time() → time.Time
    return uint64(t.UnixMicro())
}
```

**ResourceVersion → Timeuuid (for range queries):**

For Watch resume and GetList with resourceVersion, convert back to a Timeuuid lower bound:

```go
func ResourceVersionToMinTimeuuid(rv uint64) time.Time {
    return time.UnixMicro(int64(rv))
}
```

Use `gocql.MinTimeUUID(t)` or `gocql.MaxTimeUUID(t)` for CQL `WHERE resource_version > minTimeuuid(?)` range queries.

**Monotonicity guarantee:** Within a single DC, Timeuuids are monotonically increasing because they embed the ScyllaDB coordinator node's timestamp. Cross-DC ordering is not guaranteed — this is acceptable because each DC serves its own clients via `LOCAL_ONE`.

### Codec — Object Serialization

The codec wraps `runtime.Codec` from the apiserver serializer chain. Use the same codec pattern as etcd3's store:

```go
type codec struct {
    runtime.Codec
}

func newCodec(c runtime.Codec) *codec {
    return &codec{Codec: c}
}
```

The codec is passed into the Store constructor from the apiserver wiring layer. Use `runtime.Encode(codec, obj)` to serialize and `runtime.Decode(codec, data)` to deserialize. The codec handles:
- Protobuf or JSON encoding (depending on the configured serializer)
- GVK (GroupVersionKind) embedding in the serialized bytes
- Schema version negotiation

**Do NOT implement custom serialization** — use the standard `runtime.Codec` passed from the apiserver configuration. The etcd3 store in `k8s.io/apiserver/pkg/storage/etcd3/store.go` is the reference for how codec is used.

### Key Encoding

The `k8s.io/apiserver` registry layer constructs storage keys in the format:

```
/<prefix>/<resource>/<namespace>/<name>
```

For Soteria's resources, keys look like:
- `/soteria.io/drplans/default/erp-full-stack`
- `/soteria.io/drexecutions/default/exec-001`
- `/soteria.io/drgroupstatuses/default/exec-001-wave0-group0`

The store must parse these keys into `(api_group, resource_type, namespace, name)` tuples for the ScyllaDB primary key:

```go
func KeyToComponents(key string) (apiGroup, resourceType, namespace, name string, err error) {
    // Strip leading slash
    // Split by "/"
    // key = "/soteria.io/drplans/default/my-plan"
    // → apiGroup="soteria.io", resourceType="drplans", namespace="default", name="my-plan"
    //
    // For cluster-scoped resources (not applicable here, all Soteria resources are namespaced):
    // key = "/soteria.io/drplans/my-plan"
    // → apiGroup="soteria.io", resourceType="drplans", namespace="", name="my-plan"
}
```

For GetList, the key is a prefix:
- `/soteria.io/drplans` → all DRPlans across namespaces
- `/soteria.io/drplans/default` → all DRPlans in namespace "default"

### Create Implementation

```go
func (s *Store) Create(ctx context.Context, key string, obj, out runtime.Object, ttl uint64) error {
    // 1. Parse key into (apiGroup, resourceType, namespace, name)
    // 2. Prepare object for storage: s.versioner.PrepareObjectForStorage(obj)
    // 3. Encode object: data, err := runtime.Encode(s.codec, obj)
    // 4. Generate new Timeuuid: gocql.TimeUUID()
    // 5. INSERT with IF NOT EXISTS:
    //    INSERT INTO kv_store (api_group, resource_type, namespace, name, value, resource_version)
    //    VALUES (?, ?, ?, ?, ?, ?) IF NOT EXISTS
    // 6. Check applied flag from CAS result — if not applied, return errors.NewAlreadyExists
    // 7. Set resourceVersion on out object via versioner.UpdateObject
    // 8. Decode stored data into out
}
```

**IF NOT EXISTS (LWT):** ScyllaDB supports lightweight transactions (Paxos-based CAS). The `IF NOT EXISTS` clause on INSERT returns an `[applied]` column. If false, the row already exists → return `AlreadyExists`. This is the ONLY place Create uses LWT. Normal reads/writes use `LOCAL_ONE`.

**TTL:** Soteria DR resources do not use TTL. Accept the parameter for interface compliance but ignore it (pass 0 or omit in CQL). Log a warning at V(2) if ttl > 0.

### Get Implementation

```go
func (s *Store) Get(ctx context.Context, key string, opts storage.GetOptions, objPtr runtime.Object) error {
    // 1. Parse key into (apiGroup, resourceType, namespace, name)
    // 2. SELECT value, resource_version FROM kv_store
    //    WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?
    // 3. If no rows → return errors.NewNotFound(gr, name)
    // 4. Decode blob into objPtr
    // 5. Set resourceVersion on objPtr via versioner.UpdateObject
    //
    // opts.IgnoreNotFound: if true, return empty object instead of NotFound error
    // opts.ResourceVersion: handle "0" (any version OK), "" (most recent), specific version
}
```

**GetOptions.ResourceVersion handling:**
- `""` (empty) — return the current value (default, what we do with LOCAL_ONE)
- `"0"` — return any version, served from cache (same as empty for our implementation since we don't have a local cache at the storage layer; the apiserver cacher handles this)
- Specific version — the cacher layer typically handles this; at the storage layer, return current and let cacher decide

### GetList Implementation

```go
func (s *Store) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
    // 1. Parse key prefix → (apiGroup, resourceType, optionalNamespace)
    // 2. Build CQL query:
    //    - Cluster-wide: WHERE api_group = ? AND resource_type = ?
    //    - Namespace-scoped: WHERE api_group = ? AND resource_type = ? AND namespace = ?
    // 3. Apply pagination via continue token:
    //    - Decode continue token → (lastNamespace, lastName)
    //    - Add WHERE (namespace, name) > (?, ?) for efficient server-side paging
    //    - LIMIT opts.Predicate.Limit + 1 (extra to detect hasMore)
    // 4. Execute query, iterate rows
    // 5. For each row: decode blob → runtime.Object
    // 6. Apply label selector filtering in-memory (opts.Predicate.Label)
    //    - ScyllaDB cannot filter by label natively (labels are inside the blob)
    //    - Decode each object, check labels against selector, include if match
    // 7. If more rows exist → encode continue token for next page
    // 8. Set collective resourceVersion on list (max resourceVersion seen)
    // 9. Populate listObj items
}
```

**Label selector filtering:** Labels are stored inside the serialized blob, not as CQL columns. Filter in-memory after deserialization. This is the same approach etcd uses — the storage layer returns all objects and the `SelectionPredicate` filters them. For Soteria's scale (max 100 DRPlans, ~5000 VMs), in-memory filtering is fine.

**Pagination:** Use CQL clustering column comparison for efficient server-side paging. The continue token encodes the last `(namespace, name)` seen. The next page query adds `AND (namespace, name) > (?, ?)`.

**Field selectors:** Story 1.3 should support `opts.Predicate.Field` for basic field matching. The standard apiserver field selector for `metadata.name` and `metadata.namespace` can be handled by adjusting the CQL WHERE clause. Other field selectors are filtered in-memory.

### GuaranteedUpdate Implementation

```go
func (s *Store) GuaranteedUpdate(ctx context.Context, key string, destination runtime.Object,
    ignoreNotFound bool, preconditions *storage.Preconditions,
    tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
    // Retry loop:
    // 1. Get current object (or use cachedExistingObject if provided)
    //    - If not found and ignoreNotFound → create default object
    //    - If not found and !ignoreNotFound → return NotFound
    // 2. Check preconditions (UID, resourceVersion) if set
    // 3. Call tryUpdate(existingObj, ResponseMeta{resourceVersion})
    //    - tryUpdate returns (newObj, ttl, err)
    //    - If err is a NoUpdateNeeded sentinel → return without write
    // 4. Encode newObj
    // 5. CAS update:
    //    UPDATE kv_store SET value = ?, resource_version = ?
    //    WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?
    //    IF resource_version = ?
    //    (compare against current resource_version Timeuuid)
    // 6. If CAS fails (resource changed between read and write) → retry from step 1
    // 7. If CAS succeeds → decode into destination, set resourceVersion
}
```

**CAS for conflict detection:** Use ScyllaDB's LWT `IF resource_version = <old_timeuuid>` to detect concurrent modifications. The retry loop handles conflicts by re-reading, re-calling tryUpdate, and re-attempting the CAS write. Limit retries (e.g., 5 attempts) and return `Conflict` if exhausted.

**ResponseMeta:** Pass the current resourceVersion (as uint64) so tryUpdate can make decisions based on version.

### Delete Implementation

```go
func (s *Store) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions,
    validateDeletion storage.ValidateObjectFunc, cachedExistingObject runtime.Object) error {
    // 1. Get current object
    // 2. If not found → return NotFound
    // 3. Check preconditions (UID, resourceVersion) if set
    // 4. Run validateDeletion(ctx, existingObj) if provided
    // 5. DELETE FROM kv_store WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?
    //    IF resource_version = ? (CAS to prevent deleting a concurrently-modified object)
    // 6. If CAS fails → re-read and retry
    // 7. Decode existing object into out (return the object as it was before deletion)
}
```

### Count Implementation

```go
func (s *Store) Count(key string) (int64, error) {
    // 1. Parse key prefix → (apiGroup, resourceType)
    // 2. SELECT count(*) FROM kv_store WHERE api_group = ? AND resource_type = ?
    // 3. Return count
}
```

### Continue Token Encoding

Continue tokens encode the pagination cursor as base64-encoded `namespace + "\x00" + name`:

```go
func EncodeContinue(namespace, name string) string {
    raw := namespace + "\x00" + name
    return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeContinue(token string) (namespace, name string, err error) {
    raw, err := base64.RawURLEncoding.DecodeString(token)
    if err != nil {
        return "", "", fmt.Errorf("decoding continue token: %w", err)
    }
    parts := strings.SplitN(string(raw), "\x00", 2)
    if len(parts) != 2 {
        return "", "", fmt.Errorf("invalid continue token format")
    }
    return parts[0], parts[1], nil
}
```

### Error Mapping

Map ScyllaDB and storage errors to Kubernetes API errors using `k8s.io/apimachinery/pkg/api/errors`:

| Condition | K8s Error |
|---|---|
| INSERT IF NOT EXISTS → not applied | `errors.NewAlreadyExists(gr, name)` |
| SELECT returns no rows | `errors.NewNotFound(gr, name)` |
| CAS UPDATE fails (version mismatch) | `errors.NewConflict(gr, name, err)` |
| Precondition UID mismatch | `errors.NewConflict(gr, name, err)` |
| Precondition resourceVersion mismatch | `errors.NewConflict(gr, name, err)` |
| Context cancelled/timeout | `errors.NewTimeoutError(msg, retryAfterSeconds)` |
| ScyllaDB connection error | `errors.NewInternalError(err)` |

### CQL Queries Reference

All queries use `LOCAL_ONE` consistency (the default from Story 1.2's client), except LWT operations which use `SERIAL`/`LOCAL_SERIAL` consistency for the Paxos round.

```sql
-- Create (LWT)
INSERT INTO kv_store (api_group, resource_type, namespace, name, value, resource_version)
VALUES (?, ?, ?, ?, ?, ?) IF NOT EXISTS

-- Get
SELECT value, resource_version FROM kv_store
WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?

-- List (cluster-wide)
SELECT namespace, name, value, resource_version FROM kv_store
WHERE api_group = ? AND resource_type = ?

-- List (namespace-scoped)
SELECT namespace, name, value, resource_version FROM kv_store
WHERE api_group = ? AND resource_type = ? AND namespace = ?

-- List (with continue token)
SELECT namespace, name, value, resource_version FROM kv_store
WHERE api_group = ? AND resource_type = ? AND (namespace, name) > (?, ?)
LIMIT ?

-- Update (CAS)
UPDATE kv_store SET value = ?, resource_version = ?
WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?
IF resource_version = ?

-- Delete (CAS)
DELETE FROM kv_store
WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ?
IF resource_version = ?

-- Count
SELECT count(*) FROM kv_store WHERE api_group = ? AND resource_type = ?
```

### LWT Consistency Levels

When using `IF NOT EXISTS`, `IF resource_version = ?`, or any conditional operation:
- The write itself uses `LOCAL_ONE` (gocql default)
- The serial consistency for the Paxos round uses `LOCAL_SERIAL` (local DC only)

Configure on the query:

```go
query := session.Query(cql, args...).
    WithContext(ctx).
    SerialConsistency(gocql.LocalSerial)
```

`LOCAL_SERIAL` ensures the Paxos agreement only involves nodes in the local DC — no cross-DC latency for CAS operations. This is critical for DR: when one DC is down, CAS operations must still succeed on the surviving DC.

### File Organization

After this story, `pkg/storage/scylladb/` contains:

```
pkg/storage/scylladb/
├── doc.go           # from Story 1.1 (stub)
├── client.go        # from Story 1.2 — connection management
├── schema.go        # from Story 1.2 — keyspace and table DDL
├── store.go         # NEW — storage.Interface: Create, Get, GetList, GuaranteedUpdate, Delete, Count
├── versioner.go     # NEW — Timeuuid ↔ resourceVersion mapping
├── codec.go         # NEW — runtime.Object serialization wrapper
├── keyutil.go       # NEW — storage key parsing
├── pager.go         # NEW — continue token encoding
├── versioner_test.go  # NEW — unit tests
├── codec_test.go      # NEW — unit tests
├── keyutil_test.go    # NEW — unit tests
└── pager_test.go      # NEW — unit tests
```

Integration tests:

```
test/integration/storage/
├── suite_test.go    # from Story 1.2 — testcontainers lifecycle
├── client_test.go   # from Story 1.2
├── schema_test.go   # from Story 1.2
└── store_test.go    # NEW — CRUD integration tests
```

### Dependencies

This story uses dependencies already added in Stories 1.1 and 1.2:
- `k8s.io/apiserver` — `storage.Interface`, `storage.Versioner`, `storage.Preconditions`, `storage.UpdateFunc`
- `k8s.io/apimachinery` — `runtime.Object`, `runtime.Codec`, `api/errors`, `metav1`, `labels`, `fields`
- `github.com/gocql/gocql` — ScyllaDB queries, Timeuuid generation
- `github.com/testcontainers/testcontainers-go` — integration test infrastructure

**No new dependencies required.**

### Testing Strategy

**Unit tests** (co-located `_test.go` files):
- `versioner_test.go` — Timeuuid ↔ uint64 ↔ string roundtrip, monotonicity, zero value, max value
- `codec_test.go` — roundtrip encoding for all three resource types using a real `runtime.Codec` from the scheme
- `keyutil_test.go` — key parsing for all resource types, namespace-scoped and cluster-scoped, edge cases (empty namespace, special characters)
- `pager_test.go` — continue token encode/decode roundtrip, invalid token handling

**Integration tests** (`test/integration/storage/store_test.go`):
- Reuse the testcontainers ScyllaDB lifecycle from Story 1.2's `suite_test.go`
- Test naming: `TestStore_Create_NewObject`, `TestStore_Create_DuplicateKey_ReturnsAlreadyExists`, etc.
- Use the API types from `pkg/apis/soteria.io/v1alpha1/` (DRPlan, DRExecution, DRGroupStatus)
- Create a real `runtime.Codec` via the scheme registration from Story 1.1
- All integration test files use `//go:build integration` tag

### Reference Implementation

Study `k8s.io/apiserver/pkg/storage/etcd3/store.go` as the canonical reference for how `storage.Interface` is implemented against etcd. Key patterns to follow:

1. **Error handling:** etcd3 store converts etcd-specific errors to k8s API errors — do the same for gocql errors
2. **Versioner usage:** etcd3's versioner uses etcd's ModRevision as resourceVersion — we use Timeuuid-derived microseconds
3. **Codec usage:** `runtime.Encode(s.codec, obj)` and `runtime.Decode(s.codec, data)` — same pattern
4. **GuaranteedUpdate loop:** etcd3 uses optimistic concurrency with retry — we use CAS with retry
5. **Precondition checks:** etcd3 checks UID and resourceVersion before write — same pattern
6. **List implementation:** etcd3 uses prefix-based key range — we use partition key query

### Project Structure Notes

- `pkg/storage/scylladb/` is the ONLY package that touches ScyllaDB — this boundary is absolute
- The store receives a `runtime.Codec` from the apiserver wiring layer — it does NOT create its own codec
- The store receives a `*gocql.Session` from the client (Story 1.2) — it does NOT manage connections
- Key format is determined by the apiserver's registry layer (Story 1.5) — the store only parses keys

### Architecture Compliance

- **Storage boundary:** Only `pkg/storage/scylladb/` touches ScyllaDB — enforced
- **Table naming:** snake_case — `kv_store`, `api_group`, `resource_type`
- **Consistency:** `LOCAL_ONE` for reads/writes, `LOCAL_SERIAL` for CAS Paxos round
- **Error wrapping:** lowercase, no punctuation, wrap with `%w`
- **Structured logging:** `log.FromContext(ctx).WithValues(...)` — no fmt.Println
- **Context propagation:** All methods accept and propagate `ctx` — never `context.Background()`
- **Test naming:** `TestFunction_Scenario_Expected`
- **Integration test tag:** `//go:build integration`

### Critical Warnings

1. **Do NOT implement Watch in this story** — stub it. Watch via CDC is Story 1.4. The stub must compile and satisfy the interface, but should return an error indicating it's not yet implemented.

2. **LWT performance:** Lightweight transactions are ~4x slower than normal writes in ScyllaDB. Use LWT only for Create (IF NOT EXISTS), GuaranteedUpdate (IF resource_version), and Delete (IF resource_version). Normal Get and GetList use regular LOCAL_ONE queries.

3. **Label selector filtering is in-memory** — labels are inside the blob, not CQL columns. This is by design (generic KV schema = no per-field columns). At Soteria's scale (100 plans, 5000 VMs), this is fine.

4. **Timeuuid vs UUID:** gocql.TimeUUID() generates a version 1 UUID with embedded timestamp. Regular gocql.RandomUUID() is version 4 (no timestamp). Always use `gocql.TimeUUID()` for resource_version.

5. **CAS retry limit:** GuaranteedUpdate and Delete must have a retry limit (e.g., 5 attempts). If CAS keeps failing, return a Conflict error rather than looping forever.

6. **Do NOT use ALLOW FILTERING** in CQL queries — it causes full table scans. All queries must use the primary key structure (partition key + clustering columns).

7. **gocql MapScan for LWT results:** When using `IF NOT EXISTS` or `IF resource_version = ?`, use `MapScan` to read the `[applied]` boolean from the result row. If `[applied]` is false, the CAS failed.

8. **Cluster-scoped vs namespace-scoped resources:** All Soteria resources (DRPlan, DRExecution, DRGroupStatus) are namespaced. The key always includes a namespace component. However, the key parser should handle the edge case of empty namespace for future-proofing.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 1.3 (lines 380-425)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Data Architecture (lines 178-189)]
- [Source: _bmad-output/planning-artifacts/architecture.md — storage.Interface files (lines 412-419)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Implementation Sequence (lines 229-242)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Project Structure — pkg/storage/scylladb/ (lines 412-419)]
- [Source: _bmad-output/project-context.md — ScyllaDB Storage Layer rules (lines 80-89)]
- [Source: _bmad-output/project-context.md — Testing Rules (lines 109-131)]
- [Source: _bmad-output/implementation-artifacts/1-2-scylladb-connection-generic-kv-schema.md — KV schema, client, testcontainers setup]
- [Source: _bmad-output/implementation-artifacts/1-1-project-initialization-api-type-definitions.md — API types, codegen, scheme registration]
- [External: k8s.io/apiserver/pkg/storage/interfaces.go — storage.Interface definition]
- [External: k8s.io/apiserver/pkg/storage/etcd3/store.go — Reference storage.Interface implementation]
- [External: Daniel Mangum — K8s ASA: The Storage Interface — https://danielmangum.com/posts/k8s-asa-the-storage-interface/]
- [External: gocql TimeUUID — https://pkg.go.dev/github.com/gocql/gocql#TimeUUID]
- [External: ScyllaDB Lightweight Transactions — https://docs.scylladb.com/stable/using-scylla/lwt.html]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

- Implemented against k8s.io/apiserver v0.35.0 `storage.Interface` which differs from the story spec:
  no `Count()` method (replaced by `Stats()`), `Delete` has `DeleteOptions` param, additional methods
  `ReadinessCheck`, `GetCurrentResourceVersion`, `EnableResourceSizeEstimation`, `CompactRevision`.
- Reused `storage.APIObjectVersioner` from k8s.io/apiserver instead of a custom Versioner — it handles
  uint64 ↔ string on ObjectMeta identically. Added Timeuuid ↔ uint64 helpers as separate functions.
- Skipped `codec.go` — standard `runtime.Encode`/`runtime.DecodeInto` used directly (per story dev notes).
- Skipped `pager.go` — continue tokens use standard k8s `storage.EncodeContinue`/`DecodeContinue` format
  from `k8s.io/apiserver/pkg/storage/continue.go` for compatibility with the apiserver cacher layer.
- All CQL queries use `LOCAL_ONE` for reads/writes, `LOCAL_SERIAL` for CAS (LWT) Paxos rounds.
- `Watch` returns `storage.NewInternalError` stub; full CDC-based Watch is Story 1.4.
- 18 unit tests + 26 integration tests (+ 12 pre-existing from Story 1.2 = 38 total integration).

### File List

New source files:
- `pkg/storage/scylladb/versioner.go` — Timeuuid ↔ uint64 helpers, NewVersioner()
- `pkg/storage/scylladb/keyutil.go` — Storage key parsing (KeyToComponents, KeyPrefixToComponents, ComponentsToKey)
- `pkg/storage/scylladb/store.go` — storage.Interface implementation (Create, Get, GetList, GuaranteedUpdate, Delete, Watch stub, Stats, ReadinessCheck, etc.)

New test files:
- `pkg/storage/scylladb/versioner_test.go` — Unit tests for Timeuuid conversion, monotonicity, versioner methods
- `pkg/storage/scylladb/keyutil_test.go` — Unit tests for key parsing roundtrip, edge cases
- `test/integration/storage/store_test.go` — Integration tests against testcontainers ScyllaDB for all CRUD ops, all 3 resource types
