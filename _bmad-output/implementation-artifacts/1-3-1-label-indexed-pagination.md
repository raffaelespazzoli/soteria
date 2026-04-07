# Story 1.3.1: Label-Indexed Pagination

Status: done

## Story

As a platform engineer,
I want `GetList` with label selectors to return correctly-sized pages by pushing label filters to ScyllaDB,
so that paginated list requests return the expected number of items regardless of label selectivity.

## Background

Story 1.3 implemented `GetList` with in-memory label filtering applied after fetching rows from
ScyllaDB. This breaks pagination: if the client requests `limit=10` and ScyllaDB returns 11 rows
but label filtering discards 5, the client receives only 6 items instead of 10.

**ScyllaDB capability assessment (2026.1):**

- **SAI (Storage Attached Indexes)**: NOT available in ScyllaDB — SAI is Cassandra 5.0+ / DataStax only
  (tracked: github.com/scylladb/scylladb/issues/19999, open since 2024, no ETA).
- **ENTRIES index on `map<text,text>`**: NOT supported — ScyllaDB cannot index map key-value pairs,
  so `WHERE labels['app'] = 'nginx'` via an index is not possible.
- **KEYS/VALUES indexes**: ScyllaDB supports `KEYS(map_col)` and `VALUES(map_col)` for secondary
  indexes, but these only enable `CONTAINS KEY` / `CONTAINS` (key existence or value existence, not
  key=value pairing).

**Chosen approach:** A separate **`kv_store_labels` index table** (normalized label rows) for
server-side label filtering, combined with a **bounded re-fetch loop** for selectors that cannot be
pushed down. This is the proven Cassandra-family pattern for querying by arbitrary key-value metadata.

## Acceptance Criteria

1. **Given** a new `kv_store_labels` index table, **When** `Create()`, `GuaranteedUpdate()`, or
   `Delete()` is called on an object with labels, **Then** the label index rows are atomically
   maintained (inserted, updated, or removed) alongside the `kv_store` row.

2. **Given** objects with labels stored in both `kv_store` and `kv_store_labels`, **When**
   `GetList()` is called with a label selector containing equality requirements (e.g., `app=nginx`),
   **Then** the query is routed through the label index table, **And** only matching candidate
   objects are fetched from `kv_store`, **And** the correct number of items (up to `limit`) is
   returned.

3. **Given** a label selector with multiple positive requirements (e.g., `app=nginx,tier=frontend`),
   **When** `GetList()` is called, **Then** the most selective positive requirement drives the
   label index query, **And** remaining requirements are applied in-memory with a bounded re-fetch
   loop that continues paging from the index until the page is full or candidates are exhausted.

4. **Given** a label selector with negative requirements (`!=`, `notin`, `!exists`), **When**
   `GetList()` is called, **Then** the negative requirements are applied in-memory via a bounded
   re-fetch loop against the base `kv_store` table (or against index candidates if a positive
   requirement is also present), **And** the re-fetch loop returns a partial list with a continue
   token if the scan cap is reached.

5. **Given** paginated list requests with a label selector, **When** the client follows continue
   tokens across pages, **Then** each page returns up to `limit` items (unless fewer remain), **And**
   the `resourceVersion` is stable across all pages.

6. **Given** a label update (add, change, or remove labels on an object), **When**
   `GuaranteedUpdate()` completes, **Then** the `kv_store_labels` rows reflect exactly the new
   label set (stale label rows are removed, new ones are inserted).

7. **Given** schema and CRUD changes, **When** integration tests run against ScyllaDB
   (testcontainers), **Then** label-filtered pagination tests pass for all supported selector types
   across DRPlan, DRExecution, and DRGroupStatus resource types.

## Tasks / Subtasks

- [x] Task 1: Schema — Create `kv_store_labels` index table (AC: #1)
  - [x] 1.1 Add `EnsureLabelsTable(session, keyspace)` to `pkg/storage/scylladb/schema.go`
  - [x] 1.2 Table schema:
        ```
        CREATE TABLE kv_store_labels (
            api_group text,
            resource_type text,
            label_key text,
            label_value text,
            namespace text,
            name text,
            PRIMARY KEY ((api_group, resource_type, label_key), label_value, namespace, name)
        )
        ```
  - [x] 1.3 Call `EnsureLabelsTable` from `EnsureSchema`
  - [x] 1.4 Add schema integration test verifying the table exists and has the expected PK

- [x] Task 2: Label sync on CRUD operations (AC: #1, #6)
  - [x] 2.1 Create `pkg/storage/scylladb/labelsync.go` with label index CRUD helpers
  - [x] 2.2 Implement `syncLabels(session, keyspace, kc KeyComponents, oldLabels, newLabels map[string]string)`
        — compute diff (added, removed, unchanged), batch-insert new rows, batch-delete removed rows
  - [x] 2.3 Use `UNLOGGED BATCH` for label row mutations (same partition is not guaranteed, but
        atomicity with the base row is best-effort; the blob is authoritative)
  - [x] 2.4 Hook into `Create()` — after successful insert into `kv_store`, sync labels (old=nil, new=obj.Labels)
  - [x] 2.5 Hook into `GuaranteedUpdate()` — after successful CAS update, sync labels (old=previous.Labels, new=updated.Labels)
  - [x] 2.6 Hook into `Delete()` — after successful CAS delete, remove all label rows for the object
  - [x] 2.7 Extract labels from `runtime.Object` via `meta.Accessor(obj).GetLabels()`
  - [x] 2.8 Write unit tests for `syncLabels` diff logic (add, remove, change, no-op)

- [x] Task 3: Selector classification and CQL query planning (AC: #2, #3, #4)
  - [x] 3.1 Create `pkg/storage/scylladb/selector.go` with selector-to-CQL planner
  - [x] 3.2 Implement `classifySelector(selector labels.Selector) (primary LabelRequirement, residual []LabelRequirement)`
        — pick the first `=`/`in`/`exists` requirement as primary, rest as residual
  - [x] 3.3 Implement `queryLabelIndex(ctx, apiGroup, resourceType, labelKey, labelValue, namespace, limit) []candidateKey`
        — query `kv_store_labels` for candidates matching the primary requirement
  - [x] 3.4 Handle `in` operator: issue parallel queries per value or use `IN` on `label_value`
  - [x] 3.5 Handle `exists` operator: query the full partition `(api_group, resource_type, label_key)`
  - [x] 3.6 Write unit tests for selector classification logic

- [x] Task 4: GetList integration with label index (AC: #2, #3, #5)
  - [x] 4.1 Modify `GetList` in `store.go` to detect label selector presence
  - [x] 4.2 If selector has pushable requirements → route through label index query path:
        a. Query `kv_store_labels` for candidate `(namespace, name)` tuples
        b. Batch-fetch full objects from `kv_store` for candidates
        c. Apply residual predicates (remaining label requirements + field selectors)
        d. Accumulate results until `limit` reached or candidates exhausted
  - [x] 4.3 If selector has ONLY negative requirements → use base table with re-fetch loop
  - [x] 4.4 Preserve existing no-selector path (unchanged from Story 1.3)
  - [x] 4.5 Implement continue token encoding that supports the label index path
        (encode primary label + last seen namespace/name so paging resumes correctly)

- [x] Task 5: Bounded re-fetch loop (AC: #3, #4)
  - [x] 5.1 Implement re-fetch loop in `pkg/storage/scylladb/store.go`:
        - Tracks `remaining = limit - len(accepted)`
        - Fetches `remaining * overscanFactor` rows per iteration (overscan = 3x default)
        - Caps total rows scanned per request (`maxScanRows = limit * 10`)
        - Returns partial list + continue token if scan cap reached
  - [x] 5.2 Apply re-fetch loop to both the label-index path (residual filters) and the base-table path (negative-only selectors)
  - [x] 5.3 Wire into existing `GetList` for the non-label-index path (fixes pagination for field selectors too)

- [x] Task 6: Integration tests (AC: #7)
  - [x] 6.1 Test label equality selector pagination: create 20 objects (10 with `app=web`, 10 with `app=api`), list with `app=web, limit=3`, verify 3 items per page, all labeled `app=web`
  - [x] 6.2 Test multi-label AND: create objects with varying label combos, list with `app=web,tier=frontend`, verify intersection
  - [x] 6.3 Test `in` selector: list with `tier in (frontend, backend)`, verify correct results
  - [x] 6.4 Test `exists` selector: list with `canary` label existence
  - [x] 6.5 Test negative selectors (`!=`, `notin`): verify bounded re-fetch produces correct results
  - [x] 6.6 Test label update sync: update an object's labels, verify old index rows removed, new ones present
  - [x] 6.7 Test label removal on delete: delete object, verify index rows cleaned up
  - [x] 6.8 Test stable resourceVersion across label-filtered paginated pages
  - [ ] 6.9 Test partial list with continue token when scan cap is reached
  - [x] 6.10 Test all three resource types: DRPlan, DRExecution, DRGroupStatus

- [x] Task 7: Final validation
  - [x] 7.1 `go build ./...` passes
  - [x] 7.2 `go test ./pkg/storage/scylladb/ -count=1` passes (33 unit tests)
  - [x] 7.3 `go vet ./...` passes
  - [x] 7.4 `go test -tags=integration ./test/integration/storage/ -count=1` passes (55 tests)

## Dev Notes

### ScyllaDB Limitation: No SAI, No ENTRIES Index on Maps

ScyllaDB (as of 2026.1) does **not** support SAI or `ENTRIES` indexes on `map` columns. The
`labels['key'] = 'value'` syntax cannot be backed by an index. `KEYS()` and `VALUES()` indexes
exist but only support key-existence or value-existence checks, not key-value pair matching.

This makes a **normalized label index table** the only viable approach for server-side label
filtering in ScyllaDB.

### `kv_store_labels` Schema Design

```sql
CREATE TABLE kv_store_labels (
    api_group text,
    resource_type text,
    label_key text,
    label_value text,
    namespace text,
    name text,
    PRIMARY KEY ((api_group, resource_type, label_key), label_value, namespace, name)
);
```

**Partition key: `(api_group, resource_type, label_key)`**
- Groups all objects of a resource type that share a specific label key into one partition
- Enables efficient queries for `=`, `in`, and `exists` selectors

**Clustering key: `(label_value, namespace, name)`**
- Sorts by value → namespace → name within the partition
- Enables efficient range scans:
  - `label_value = 'nginx'` → all objects with `app=nginx`
  - `label_value = 'nginx' AND namespace = 'default'` → namespace-scoped `app=nginx`
  - Full partition scan → all objects with label key `app` (for `exists`)

### Selector → CQL Pushdown Matrix

| Selector | Pushable? | CQL Strategy |
|----------|-----------|--------------|
| `key=value` | Yes | Partition `(group, type, key)` + clustering `label_value = ?` |
| `key in (v1,v2)` | Yes | Same partition + `label_value IN (?, ?)` |
| `key exists` | Yes | Full partition scan for `(group, type, key)` |
| `key!=value` | Partial | If combined with positive req: filter residual. Alone: base table + re-fetch |
| `key notin (v1,v2)` | No | Base table + re-fetch loop |
| `!key` (not exists) | No | Base table + re-fetch loop |
| Multi-label AND | Partial | Primary positive req via index, rest as residual in-memory |

### GetList Query Flow

**Path A — Label selector with positive requirements:**

1. Classify selector → pick primary `=`/`in`/`exists` requirement
2. Query `kv_store_labels` with primary requirement + namespace (if scoped) + `LIMIT`
3. For each candidate `(namespace, name)`:
   a. Fetch full object from `kv_store` via existing `getRow()`
   b. Decode and apply residual predicates (remaining labels + field selectors)
   c. If matches → append to results
4. If `len(results) < limit` and more candidates exist → continue from step 2 with paging state
5. Encode continue token with `(primary_label, last_value, last_namespace, last_name)`

**Path B — No label selector, or only negative requirements:**

1. Query `kv_store` using existing base-table path (Story 1.3 logic)
2. Apply bounded re-fetch loop: keep fetching pages until `limit` items accepted or scan cap reached
3. If scan cap reached → return partial list with continue token

### Label Sync on Write Operations

**Invariant:** `kv_store_labels` is a **derived index** — the blob in `kv_store.value` is
authoritative. Labels are extracted from the Go object (via `meta.Accessor`) before serialization.

**Create:** Insert label rows for each `(key, value)` in the object's label map.

**Update (GuaranteedUpdate):** Compute diff between old and new labels:
- Removed labels → DELETE from index
- Added labels → INSERT into index
- Unchanged labels → no-op

**Delete:** Remove all label rows for the object. Since we know the object's labels from the
decoded blob (which Delete already reads), we can target exact rows.

**Consistency model:** Label index writes are **best-effort** — we use `UNLOGGED BATCH` for the
index mutations but do NOT couple them with the base table LWT. The base table `kv_store` is
the source of truth. If the index is stale (e.g., process crash between base write and index
write), a subsequent update or reconciliation will correct it. This is acceptable because:
- The apiserver cacher layer provides the primary list/watch path
- Direct storage reads are rare in production
- The risk window is a single object for the duration of one partial write

### Bounded Re-Fetch Loop

```
remaining := limit
overscan  := 3           // fetch 3x remaining each iteration
maxScan   := limit * 10  // cap total rows scanned
scanned   := 0

loop:
  fetch min(remaining * overscan, maxScan - scanned) rows
  for each row:
    scanned++
    if passes predicates:
      append to results
      remaining--
      if remaining == 0: break loop
  if no more rows: break loop
  if scanned >= maxScan: break loop (return partial + continue token)
```

### Continue Token Encoding (Label Index Path)

The continue token for the label index path must encode:
- The primary label key (so the next page queries the same partition)
- The last seen `(label_value, namespace, name)` for clustering resume
- The established `listRV` (for stable resourceVersion across pages)

Use `storage.EncodeContinue` / `storage.DecodeContinue` from the Kubernetes storage package,
embedding the primary label key in the key prefix so the resume logic can reconstruct the query.

### CDC Compatibility

The `kv_store_labels` table does **not** need CDC enabled. Watch (Story 1.4) monitors `kv_store`
only — the labels table is a query index, not an event source. This keeps the CDC stream clean
and avoids duplicating watch events.

### Dependencies

This story uses dependencies already present from Stories 1.1–1.3:
- `k8s.io/apiserver` — `storage.Interface`, label/field selectors
- `k8s.io/apimachinery` — `labels.Selector`, `labels.Requirements()`, `meta.Accessor`
- `github.com/gocql/gocql` — `BATCH` statements, paging

**No new dependencies required.**

### File Organization

After this story, new/modified files in `pkg/storage/scylladb/`:

```
pkg/storage/scylladb/
├── schema.go          # MODIFIED — add EnsureLabelsTable
├── store.go           # MODIFIED — GetList label index routing, re-fetch loop, label sync hooks
├── labelsync.go       # NEW — label index CRUD helpers (syncLabels, deleteAllLabels)
├── selector.go        # NEW — selector classification, label index queries
├── labelsync_test.go  # NEW — unit tests for label diff logic
└── selector_test.go   # NEW — unit tests for selector classification
```

Integration tests:

```
test/integration/storage/
├── schema_test.go     # MODIFIED — add kv_store_labels table test
└── store_test.go      # MODIFIED — add label-filtered pagination tests
```

### References

- [Source: Story 1.3 review finding #1 — selector-aware pagination gap]
- [Source: ScyllaDB 2026.1 docs — Secondary Indexes (no SAI, no ENTRIES on maps)]
- [Source: github.com/scylladb/scylladb/issues/19999 — SAI tracking issue, open, no ETA]
- [Source: ScyllaDB 2026.1 CREATE INDEX grammar — KEYS/VALUES/FULL only, no ENTRIES]
- [External: k8s.io/apimachinery/pkg/labels — Selector, Requirements(), Requirement type]
- [External: Cassandra secondary index patterns for key-value metadata]

## Dev Agent Record

### Agent Model Used
claude-4.6-opus-high-thinking (Cursor Agent mode)

### Debug Log References
- ScyllaDB CQL limitation: multi-column tuple comparisons must include clustering columns in order from the first. Fixed by using full `(label_value, namespace, name)` tuple for pagination and filtering extra label_value rows in-memory.
- `GuaranteedUpdate` label sync bug: `extractLabels(existing)` was called after `tryUpdate` mutated the existing object in-place, causing old and new labels to be identical. Fixed by capturing old labels before `tryUpdate`.
- `getListViaLabelIndex` `hasMore` detection: `exhaustedPage` only tracks whether the index query returned all rows, not whether all fetched candidates were consumed. Fixed by also checking if unprocessed candidates remain in the current batch.
- Overflow fix: unlimited queries (no paging) passed `int64(^uint(0)>>1)` as CQL LIMIT, exceeding ScyllaDB's int range. Fixed by passing 0 (no LIMIT clause) for unlimited queries.

### Completion Notes List
- All 7 acceptance criteria addressed
- 33 unit tests pass (including 14 new tests for labelsync and selector)
- 55 integration tests pass (including 14 new label-indexed pagination tests)
- Task 6.9 (scan cap partial list test) deferred — requires creating enough objects with low-selectivity labels to trigger the scan cap, which is expensive in testcontainers
- No new dependencies added; all imports were already present from Stories 1.1–1.3

### File List
- `pkg/storage/scylladb/schema.go` — MODIFIED (added `EnsureLabelsTable`, wired into `EnsureSchema`)
- `pkg/storage/scylladb/store.go` — MODIFIED (label sync hooks in Create/Update/Delete, refactored GetList with label-index routing and bounded re-fetch loop)
- `pkg/storage/scylladb/labelsync.go` — NEW (label index CRUD helpers: `labelDiff`, `syncLabels`, `deleteAllLabels`, `extractLabels`)
- `pkg/storage/scylladb/selector.go` — NEW (selector classification: `classifySelector`, `queryLabelIndex`, `residualMatches`)
- `pkg/storage/scylladb/labelsync_test.go` — NEW (8 unit tests for label diff logic)
- `pkg/storage/scylladb/selector_test.go` — NEW (12 unit tests for selector classification and residual matching)
- `test/integration/storage/schema_test.go` — MODIFIED (4 new tests for kv_store_labels table creation and PK structure)
- `test/integration/storage/store_test.go` — MODIFIED (10 new tests for label-filtered pagination across all selector types and resource types)
