# Story 2b.4: ScyllaDB Label-Indexed Pagination — Scan Cap Integration Test

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want the scan cap behavior of the label-indexed pagination re-fetch loop to be verified end-to-end against real ScyllaDB,
So that I am confident the bounded scan and partial-list continue-token logic works correctly under realistic data volumes.

## Background

Story 1.3.1 implemented label-indexed pagination with a bounded re-fetch loop in `pkg/storage/scylladb/store.go`. The loop uses a scan cap (`maxScanRows = limit * 10`) that returns a partial list with a continue token when reached. Task 6.9 — the integration test verifying this behavior — was deferred because it requires creating enough objects with low-selectivity labels to trigger the scan cap, which is expensive in testcontainers. This debt has been carried since Epic 1.

## Acceptance Criteria

1. **AC1 — Scan cap returns partial list:** Given the bounded re-fetch loop in `GetList` (both path B: base-table with negative-only selectors, and label-index path with residual filters), when the label selector has very low selectivity (fewer than `limit` matches within the scan cap window), then the loop stops at `maxScanRows` and returns a partial list with a valid continue token.

2. **AC2 — Partial list size and continue token:** Given a ScyllaDB testcontainers environment with 100+ objects where only a small fraction (e.g., 5 out of 100) match a given label selector, when `GetList` is called with `limit=10` and the matching label selector, then the scan cap (`limit * 10 = 100`) is reached before 10 matches are found, the returned list contains fewer than `limit` items (the matches found within the scan window), and `list.Continue` is non-empty.

3. **AC3 — Continue token resumes correctly:** Given the partial list with a continue token from the scan cap scenario, when the client issues a follow-up `GetList` with the continue token, then the follow-up request resumes scanning from where the previous request stopped and any additional matches beyond the scan cap window are returned.

4. **AC4 — Coexistence with existing tests:** Given the scan cap integration test, when `make integration` runs, then the test passes alongside all existing label-indexed pagination tests (Story 1.3.1 Tasks 6.1–6.8, 6.10).

## Tasks / Subtasks

- [ ] Task 1: Base-table path (Path B) scan cap test (AC: #1, #2, #3, #4)
  - [ ] 1.1 In `test/integration/storage/store_test.go`, add `TestStore_ScanCap_BaseTable_PartialList`
  - [ ] 1.2 Create 110 DRPlan objects in namespace `"default"` with names `sc-base-NNN` (NNN = 000–109, zero-padded 3-digit). Give 105 objects label `status=active`. Give 5 objects (at indices 020, 040, 060, 080, 100) label `status=standby` instead
  - [ ] 1.3 Call `GetList` with `limit=10` and a **negative-only selector** `status!=active`. The `!=` operator (`selection.NotEquals`) is non-pushable (`pushablePriority` returns -1), so `classifySelector` returns `hasPushable: false`, routing through `getListBaseTable` (Path B). The predicate filters each scanned row in-memory. With `maxScanMultiplier=10`, the scan cap = `10 * 10 = 100` rows. Only 4 of the 5 `status=standby` objects fall within the first 100 rows scanned (NNN 020, 040, 060, 080) — NNN 100 is beyond row 100
  - [ ] 1.4 Assert `len(list.Items) < 10` (exactly 4 matches found before scan cap)
  - [ ] 1.5 Assert `list.Continue != ""` (scan cap reached, more rows exist)
  - [ ] 1.6 Assert all returned items have `status=standby` (not `status=active`)
  - [ ] 1.7 Issue a follow-up `GetList` with the continue token and the same selector
  - [ ] 1.8 Assert the follow-up returns the remaining match(es) (NNN 100 and any beyond)
  - [ ] 1.9 Assert combined results from both pages cover all 5 `status=standby` objects
  - [ ] 1.10 Register `t.Cleanup` for all 110 keys and their label rows

- [ ] Task 2: Label-index path (Path A) scan cap test (AC: #1, #2, #3, #4)
  - [ ] 2.1 Add `TestStore_ScanCap_LabelIndex_PartialList`
  - [ ] 2.2 Create 110 DRPlan objects in namespace `"default"` with names `sc-idx-NNN` (NNN = 000–109, zero-padded 3-digit). Give all 110 objects `group=alpha` (pushable primary label). Give 5 objects (at indices 020, 040, 060, 080, 100) an additional `env=staging` label; give the remaining 105 objects `env=prod`
  - [ ] 2.3 Call `GetList` with `limit=10` and selector `group=alpha,env=staging`. Both are equality (`=`) selectors; `classifySelector` picks the first as primary (both have priority 3 — `Equals`). The primary requirement (`group=alpha` or `env=staging`, depending on iteration order) drives the label-index query; the other is the residual. Either way, all 110 objects match the primary, and only 5 pass the residual. Scan cap = `10 * 10 = 100` index rows scanned
  - [ ] 2.4 Assert `len(list.Items) < 10` (only the matches found within the scan window)
  - [ ] 2.5 Assert `list.Continue != ""` (scan cap reached, more index candidates exist)
  - [ ] 2.6 Assert all returned items have both `group=alpha` and `env=staging`
  - [ ] 2.7 Issue a follow-up `GetList` with continue token and same selector
  - [ ] 2.8 Assert combined results cover all 5 `env=staging` objects
  - [ ] 2.9 Register `t.Cleanup` for all 110 keys and their label rows

- [ ] Task 3: Verify coexistence and run (AC: #4)
  - [ ] 3.1 Run `go test -tags=integration ./test/integration/storage/ -v -count=1 -run TestStore_ScanCap` — new tests pass
  - [ ] 3.2 Run `make integration` — all integration tests pass (including existing Story 1.3.1 label-indexed pagination tests)
  - [ ] 3.3 Run `make test` — no regressions in unit tests
  - [ ] 3.4 Run `make lint-fix` followed by `make lint`

## Dev Notes

### Previous Story Learnings (from Story 1.3.1)

- Task 6.9 was explicitly deferred: "requires creating enough objects with low-selectivity labels to trigger the scan cap, which is expensive in testcontainers"
- The deferred task targets the bounded re-fetch loop's scan cap behavior — the loop in both `getListBaseTable` and `getListViaLabelIndex`
- Story 1.3.1 debug log: ScyllaDB CQL limitation — multi-column tuple comparisons must include clustering columns in order. Fixed by using full `(label_value, namespace, name)` tuple for pagination
- Story 1.3.1 debug log: `getListViaLabelIndex` `hasMore` detection — `exhaustedPage` only tracks whether the index query returned all rows, not whether all fetched candidates were consumed. Fixed by also checking if unprocessed candidates remain
- Story 1.3.1 debug log: Overflow fix — unlimited queries passed `int64(^uint(0)>>1)` as CQL LIMIT, exceeding ScyllaDB's int range. Fixed by passing 0 (no LIMIT clause)

### Architecture Context

This is Story 4 (final) of the 4-story Epic 2b refactoring sprint. It closes the last piece of technical debt from Epic 1 — the deferred Task 6.9 integration test for the scan cap behavior. This story is self-contained: **no production code changes**, only a new integration test in `test/integration/storage/store_test.go`.

The scan cap is a safety mechanism that prevents unbounded table scans when label selectors have low selectivity. Without this test, we have no end-to-end proof that the partial-list + continue-token contract works correctly when the scan cap fires.

### Critical Implementation Details

**Scan cap constants (in `pkg/storage/scylladb/store.go`):**

```go
const defaultOverscanFactor = 3
const maxScanMultiplier = 10
```

The scan cap = `limit * maxScanMultiplier`. For `limit=10`, the scan cap is 100 rows. After scanning 100 rows, if fewer than `limit` matches are found, the loop returns a partial list with `hasMore = true`, which causes a continue token to be encoded.

**Two code paths to test:**

1. **`getListBaseTable` (Path B):** Used when the label selector has only negative requirements (or no pushable requirement). The loop scans the `kv_store` base table directly and applies predicates in-memory.

2. **`getListViaLabelIndex` (Path A with residual):** Used when the selector has at least one pushable positive requirement. The loop queries the `kv_store_labels` index for candidates, fetches full objects from `kv_store`, and applies residual predicates. The scan cap applies to the number of *index candidates* scanned, not just matches.

**How to trigger the scan cap:**

Create N objects (N > `limit * maxScanMultiplier`) where only a small fraction match the full selector. The scan cap will be hit before all matches are found.

Example for `limit=10`, `maxScanMultiplier=10` → scan cap = 100:
- Create 110 objects
- Only 5 match the full selector (distributed across the range)
- Some matches fall within the first 100 rows scanned (partial list)
- Remaining matches fall after row 100 (continue token resumes)

**Important: ScyllaDB row ordering:**

Rows in the `kv_store` base table are ordered by `(api_group, resource_type, namespace, name)` — the primary key. Within a partition (same api_group + resource_type), clustering is by `(namespace, name)`. The test objects must be named so that the 5 matching objects are spread across the full range, with at least one past the 100th row in scan order.

For the label index path, candidates from `kv_store_labels` are ordered by `(label_value, namespace, name)` within the partition `(api_group, resource_type, label_key)`. All 110 objects share the same primary label value (`group=alpha`), so they sort by `(namespace, name)`.

**Object creation cost in testcontainers:**

Creating 110 objects in a single ScyllaDB testcontainer is feasible but slower than typical tests. Each `Create()` call involves:
1. INSERT into `kv_store` (CAS/LWT)
2. Batch INSERT into `kv_store_labels` (one row per label)

Expect ~200ms per object. The full test may take 20–30 seconds for object creation. This is acceptable for integration tests but confirms why 1.3.1 deferred it.

**Cleanup:**

Every created object needs `t.Cleanup` for both the `kv_store` row and all `kv_store_labels` rows. Use the existing `cleanupKey` and `cleanupObjectLabels` helpers. Be careful with closures in loops — capture the key and labels in a local variable before passing to `t.Cleanup`.

**Test assertions — scan cap math:**

For the base-table path test:
- 110 objects named `sc-base-000` through `sc-base-109` in namespace `"default"`
- ScyllaDB orders by `(namespace, name)` → alphabetical by name within `"default"`
- Objects with `status=standby`: indices 020, 040, 060, 080, 100
- Selector: `status!=active` (non-pushable `NotEquals` → base table scan)
- Scan cap = 100 rows → first 100 rows are `sc-base-000` through `sc-base-099`
- Matches in first 100 rows: 020, 040, 060, 080 → 4 matches
- `list.Items` should have 4 items, `list.Continue` non-empty
- Follow-up: scans from row 100+ → finds `sc-base-100` (last match)

For the label-index path test:
- 110 objects named `sc-idx-000` through `sc-idx-109`, all with `group=alpha`
- The label index partition for `(soteria.io, drplans, group)` with `label_value=alpha` orders by `(namespace, name)`
- Same scan order as base table within the partition → same math applies
- Objects with `env=staging`: indices 020, 040, 060, 080, 100
- Selector: `group=alpha,env=staging` (primary pushable `group=alpha` drives index, `env=staging` is residual)
- Scan cap = 100 index candidates → matches found: 4 within first 100
- Follow-up picks up the 5th match at index 100

**CRITICAL — Path B selector design:**

To route through `getListBaseTable` (Path B), the selector MUST have zero pushable requirements. The `classifySelector` function assigns `hasPushable: false` only when all requirements have `pushablePriority == -1`. Pushable operators are: `Equals`, `DoubleEquals`, `In`, `Exists`. Non-pushable operators are: `NotEquals`, `NotIn`, `DoesNotExist`. Use `status!=active` (a single `NotEquals` requirement) — this is guaranteed to route through the base-table path. Do NOT combine it with any positive equality (`=`) or `in` requirement, as that would make the selector pushable and route through the label-index path instead.

**Note on naming:** Use zero-padded 3-digit names (e.g., `sc-base-000`) to guarantee alphabetical sort order matches numeric order in ScyllaDB's UTF-8 byte-order clustering.

### Existing Code Patterns to Follow

- **Build tag:** `//go:build integration` at top of file (already present in `store_test.go`)
- **Test naming:** `TestStore_ScanCap_BaseTable_PartialList`, `TestStore_ScanCap_LabelIndex_PartialList`
- **Setup:** Use `setupStoreTest(t)` which returns a `*scyllastore.Store` backed by the shared ScyllaDB testcontainer
- **Object creation:** Use `newDRPlanWithLabels(namespace, name, lbls)` helper
- **Key format:** `/soteria.io/drplans/default/<name>`
- **Cleanup pattern:** `t.Cleanup(func() { cleanupKey(t, key); cleanupObjectLabels(t, key, lbls) })` — capture `key` and `lbls` in local variables inside the loop
- **GetList options:** `storage.ListOptions{Recursive: true, Predicate: storage.SelectionPredicate{Label: selector, Field: fields.Everything(), GetAttrs: storage.DefaultNamespaceScopedAttr, Limit: N}}`
- **Continue token follow-up:** Set `Continue: list.Continue` in the `SelectionPredicate` for the next page
- **Import alias:** `v1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"`, `scyllastore "github.com/soteria-project/soteria/pkg/storage/scylladb"`
- **Selector parsing:** `labels.Parse("app=web,tier=frontend")` — returns `(labels.Selector, error)`

### Files to Modify

| File | Change |
|------|--------|
| `test/integration/storage/store_test.go` | Add `TestStore_ScanCap_BaseTable_PartialList` and `TestStore_ScanCap_LabelIndex_PartialList` test functions |

### Files NOT to Modify

- `pkg/storage/scylladb/store.go` — no production code changes, test-only story
- `pkg/storage/scylladb/labelsync.go` — unchanged
- `pkg/storage/scylladb/selector.go` — unchanged
- `pkg/storage/scylladb/schema.go` — unchanged
- `test/integration/storage/suite_test.go` — testcontainer setup already sufficient
- Any files modified by Stories 2b.1, 2b.2, or 2b.3

### Build Commands

```bash
make integration                                                    # Full integration test suite
go test -tags=integration ./test/integration/storage/ -v -count=1   # Storage integration tests only
go test -tags=integration ./test/integration/storage/ -v -count=1 -run TestStore_ScanCap  # New tests only
make test                                                           # Unit tests (no regressions)
make lint-fix                                                       # Auto-fix code style
make lint                                                           # Verify lint passes
```

### Project Structure Notes

- `test/integration/storage/store_test.go` — existing integration tests; append new scan cap tests after the Story 1.3.1 label-indexed pagination tests (after line ~1572)
- `test/integration/storage/suite_test.go` — `TestMain` manages the shared ScyllaDB testcontainer lifecycle (start, session, teardown)
- Build tag `//go:build integration` gates these tests — they only run with `-tags=integration` or via `make integration`
- The testcontainer uses ScyllaDB image `scylladb/scylla:2025.4` with minimal resources (`--smp 1 --memory 256M`)
- No production Go code changes in this story — test file only

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2b.4] — Full acceptance criteria
- [Source: _bmad-output/implementation-artifacts/1-3-1-label-indexed-pagination.md#Task 6.9] — Deferred scan cap test (the debt this story closes)
- [Source: _bmad-output/implementation-artifacts/1-3-1-label-indexed-pagination.md#Dev Notes] — Bounded re-fetch loop design, scan cap constants, debug learnings
- [Source: pkg/storage/scylladb/store.go#L288-293] — `defaultOverscanFactor=3`, `maxScanMultiplier=10` constants
- [Source: pkg/storage/scylladb/store.go#L396-514] — `getListBaseTable` bounded re-fetch loop implementation
- [Source: pkg/storage/scylladb/store.go#L516-620] — `getListViaLabelIndex` bounded re-fetch loop implementation
- [Source: test/integration/storage/store_test.go#L967-1572] — Existing label-indexed pagination tests (patterns to follow)
- [Source: test/integration/storage/suite_test.go] — TestMain with ScyllaDB testcontainer setup
- [Source: _bmad-output/project-context.md#ScyllaDB Storage Layer] — Storage boundary, testcontainers pattern, `make integration` command

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
