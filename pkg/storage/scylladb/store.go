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

// Store architecture:
//
// Store implements k8s.io/apiserver/pkg/storage.Interface using a generic
// key-value schema in ScyllaDB. Resources are stored as opaque blobs in the
// kv_store table with composite primary key (api_group, resource_type,
// namespace, name). This avoids CQL schema changes when API types evolve.
//
// Write path: All mutations use lightweight transactions (CAS) to prevent
// lost updates. Create uses IF NOT EXISTS; Delete and GuaranteedUpdate use
// IF resource_version = <expected> with up to maxCASRetries on contention.
// When a CriticalFieldDetector signals that an update touches a state-machine
// field (e.g., DRPlan phase transition), the CAS consistency is upgraded from
// LOCAL_SERIAL to SERIAL for cross-DC linearizability.
//
// Read path: Get and GetList use LOCAL_ONE consistency. GetList supports
// label-selector filtering via a secondary kv_store_labels index table and
// continuation-token pagination.
//
// Watch is implemented in watch.go via ScyllaDB CDC.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/gocql/gocql"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"
)

// maxCASRetries bounds the number of CAS retry loops for Delete and
// GuaranteedUpdate. In practice, contention is low (single writer per
// resource via the cacher), so retries indicate either a bug or extreme
// clock skew. Five retries is generous while preventing infinite loops.
const maxCASRetries = 5

// CriticalFieldDetector inspects old and new versions of an object to
// determine whether the update touches a "critical" field that requires
// cross-DC lightweight transactions (LWT with Serial consistency) instead
// of local-only CAS (LocalSerial). Returning true causes the CAS to use
// Paxos quorum across all datacenters, preventing conflicting state
// transitions from concurrent writers on different DCs.
type CriticalFieldDetector func(old, updated runtime.Object) bool

// Store implements storage.Interface backed by ScyllaDB's kv_store table.
type Store struct {
	session               *gocql.Session
	codec                 runtime.Codec
	versioner             storage.Versioner
	keyspace              string
	groupResource         schema.GroupResource
	resourcePrefix        string
	newFunc               func() runtime.Object
	newListFunc           func() runtime.Object
	criticalFieldDetector CriticalFieldDetector
}

// StoreConfig holds the parameters for constructing a new Store.
type StoreConfig struct {
	Session        *gocql.Session
	Codec          runtime.Codec
	Keyspace       string
	GroupResource  schema.GroupResource
	ResourcePrefix string
	NewFunc        func() runtime.Object
	NewListFunc    func() runtime.Object
	// CriticalFieldDetector, when set, enables cross-DC LWT (Serial
	// consistency) for updates that touch critical state-machine fields.
	// When nil, all CAS operations use LocalSerial (single-DC).
	CriticalFieldDetector CriticalFieldDetector
}

var _ storage.Interface = (*Store)(nil)

// NewStore creates a new ScyllaDB-backed storage.Interface.
func NewStore(cfg StoreConfig) *Store {
	return &Store{
		session:               cfg.Session,
		codec:                 cfg.Codec,
		versioner:             NewVersioner(),
		keyspace:              cfg.Keyspace,
		groupResource:         cfg.GroupResource,
		resourcePrefix:        cfg.ResourcePrefix,
		newFunc:               cfg.NewFunc,
		newListFunc:           cfg.NewListFunc,
		criticalFieldDetector: cfg.CriticalFieldDetector,
	}
}

func (s *Store) Versioner() storage.Versioner {
	return s.versioner
}

// Create implements storage.Interface.
func (s *Store) Create(ctx context.Context, key string, obj, out runtime.Object, ttl uint64) error {
	preparedKey, err := s.prepareKey(key, false)
	if err != nil {
		return err
	}

	if version, err := s.versioner.ObjectResourceVersion(obj); err == nil && version != 0 {
		return storage.ErrResourceVersionSetOnCreate
	}
	if err := s.versioner.PrepareObjectForStorage(obj); err != nil {
		return fmt.Errorf("PrepareObjectForStorage failed: %w", err)
	}

	data, err := runtime.Encode(s.codec, obj)
	if err != nil {
		return err
	}

	if ttl > 0 {
		klog.V(2).InfoS("TTL not supported for ScyllaDB storage, ignoring",
			"ttl", ttl)
	}

	kc, err := KeyToComponents(preparedKey)
	if err != nil {
		return storage.NewInternalError(err)
	}

	newRV := gocql.TimeUUID()

	applied, err := s.casInsert(ctx, kc, data, newRV)
	if err != nil {
		return storage.NewInternalError(err)
	}
	if !applied {
		return storage.NewKeyExistsError(preparedKey, 0)
	}

	// Sync label index (best-effort: kv_store blob is authoritative)
	newLabels := extractLabels(obj)
	if len(newLabels) > 0 {
		if serr := syncLabels(s.session, s.keyspace, kc, nil, newLabels); serr != nil {
			klog.ErrorS(serr, "Failed to sync label index on create", "key", preparedKey)
		}
	}

	if out != nil {
		if err := decode(s.codec, data, out); err != nil {
			return storage.NewCorruptObjError(preparedKey, err)
		}
		if err := s.versioner.UpdateObject(out, TimeuuidToResourceVersion(newRV)); err != nil {
			return err
		}
	}
	return nil
}

// Delete implements storage.Interface.
func (s *Store) Delete(
	ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions,
	validateDeletion storage.ValidateObjectFunc, cachedExistingObject runtime.Object, opts storage.DeleteOptions) error {

	preparedKey, err := s.prepareKey(key, false)
	if err != nil {
		return err
	}

	kc, err := KeyToComponents(preparedKey)
	if err != nil {
		return storage.NewInternalError(err)
	}

	for range maxCASRetries {
		data, existingRV, err := s.getRow(ctx, kc)
		if err != nil {
			return storage.NewInternalError(err)
		}
		if data == nil {
			return storage.NewKeyNotFoundError(preparedKey, 0)
		}

		existing := s.newFunc()
		if err := decode(s.codec, data, existing); err != nil {
			if opts.IgnoreStoreReadError {
				if applied, casErr := s.casDeleteRow(ctx, kc, existingRV); casErr != nil {
					return storage.NewInternalError(casErr)
				} else if !applied {
					continue
				}
				return nil
			}
			return storage.NewCorruptObjError(preparedKey, err)
		}
		rv := TimeuuidToResourceVersion(existingRV)
		if err := s.versioner.UpdateObject(existing, rv); err != nil {
			return err
		}

		if err := preconditions.Check(preparedKey, existing); err != nil {
			return err
		}
		if validateDeletion != nil {
			if err := validateDeletion(ctx, existing); err != nil {
				return err
			}
		}

		applied, casErr := s.casDeleteRow(ctx, kc, existingRV)
		if casErr != nil {
			return storage.NewInternalError(casErr)
		}
		if !applied {
			klog.V(4).InfoS("Delete CAS failed, retrying",
				"key", preparedKey)
			continue
		}

		// Clean up label index rows (best-effort)
		if serr := deleteAllLabels(s.session, s.keyspace, kc, extractLabels(existing)); serr != nil {
			klog.ErrorS(serr, "Failed to delete label index on delete", "key", preparedKey)
		}

		if out != nil {
			if err := decode(s.codec, data, out); err != nil {
				return storage.NewCorruptObjError(preparedKey, err)
			}
			if err := s.versioner.UpdateObject(out, rv); err != nil {
				return err
			}
		}
		return nil
	}
	return storage.NewResourceVersionConflictsError(preparedKey, 0)
}

// Watch is implemented in watch.go.

// Get implements storage.Interface.
func (s *Store) Get(ctx context.Context, key string, opts storage.GetOptions, objPtr runtime.Object) error {
	preparedKey, err := s.prepareKey(key, false)
	if err != nil {
		return err
	}

	kc, err := KeyToComponents(preparedKey)
	if err != nil {
		return storage.NewInternalError(err)
	}

	data, rv, err := s.getRow(ctx, kc)
	if err != nil {
		return storage.NewInternalError(err)
	}
	if data == nil {
		if opts.IgnoreNotFound {
			return runtime.SetZeroValue(objPtr)
		}
		return storage.NewKeyNotFoundError(preparedKey, 0)
	}

	if err := decode(s.codec, data, objPtr); err != nil {
		return storage.NewCorruptObjError(preparedKey, err)
	}
	return s.versioner.UpdateObject(objPtr, TimeuuidToResourceVersion(rv))
}

// defaultOverscanFactor controls how many extra rows to fetch per re-fetch
// iteration relative to the remaining items needed.
const defaultOverscanFactor = 3

// maxScanMultiplier caps total rows scanned to limit * maxScanMultiplier.
const maxScanMultiplier = 10

// GetList implements storage.Interface.
func (s *Store) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	preparedKey, err := s.prepareKey(key, opts.Recursive)
	if err != nil {
		return err
	}

	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return err
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}

	if !opts.Recursive {
		return s.getSingleItemList(ctx, preparedKey, opts, listObj, v)
	}

	withRev, continueKey, err := storage.ValidateListOptions(preparedKey, s.versioner, opts)
	if err != nil {
		return err
	}

	apiGroup, resourceType, namespace, err := KeyPrefixToComponents(preparedKey)
	if err != nil {
		return storage.NewInternalError(err)
	}

	var continueNS, continueName, continueLabelValue string
	if continueKey != "" {
		continueNS, continueName, continueLabelValue, err = parseContinueKey(continueKey, preparedKey)
		if err != nil {
			return err
		}
	}

	limit := opts.Predicate.Limit
	sel := opts.Predicate.Label

	var cs classifiedSelector
	hasLabelSelector := sel != nil && !sel.Empty()
	if hasLabelSelector {
		cs = classifySelector(sel)
	}

	var maxRV uint64
	var lastItemKey string
	var lastLabelValue string
	var hasMore bool
	newItemFunc := getNewItemFunc(v)

	if hasLabelSelector && cs.hasPushable {
		maxRV, lastItemKey, lastLabelValue, hasMore, err = s.getListViaLabelIndex(
			ctx, apiGroup, resourceType, namespace,
			continueLabelValue, continueNS, continueName,
			cs, opts, limit, v, newItemFunc)
		if err != nil {
			return err
		}
	} else {
		maxRV, lastItemKey, hasMore, err = s.getListBaseTable(
			ctx, apiGroup, resourceType, namespace,
			continueNS, continueName,
			opts, limit, v, newItemFunc)
		if err != nil {
			return err
		}
	}

	if v.IsNil() {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}

	var listRV uint64
	switch {
	case withRev > 0:
		listRV = uint64(withRev)
	case maxRV > 0:
		listRV = maxRV
	default:
		listRV = TimeuuidToResourceVersion(gocql.TimeUUID())
	}

	var continueEncoded string
	if hasMore && lastItemKey != "" {
		encodeKey := lastItemKey
		if lastLabelValue != "" {
			encodeKey += "\x01" + lastLabelValue
		}
		continueEncoded, err = storage.EncodeContinue(
			encodeKey+"\x00", preparedKey, int64(listRV))
		if err != nil {
			return err
		}
	}

	return s.versioner.UpdateList(listObj, listRV, continueEncoded, nil)
}

// getListBaseTable implements GetList using the kv_store base table with a
// bounded re-fetch loop for predicate filtering.
func (s *Store) getListBaseTable(
	ctx context.Context,
	apiGroup, resourceType, namespace string,
	continueNS, continueName string,
	opts storage.ListOptions,
	limit int64,
	v reflect.Value,
	newItemFunc func() runtime.Object,
) (maxRV uint64, lastItemKey string, hasMore bool, err error) {
	paging := limit > 0
	hasPredicate := opts.Predicate.GetAttrs != nil

	if !paging || !hasPredicate {
		// No filtering or no pagination: single fetch, original path.
		var fetchLimit int64
		if paging {
			fetchLimit = limit + 1
		}
		rows, qerr := s.queryList(ctx, apiGroup, resourceType, namespace, continueNS, continueName, fetchLimit)
		if qerr != nil {
			return 0, "", false, storage.NewInternalError(qerr)
		}
		for _, row := range rows {
			if paging && int64(v.Len()) >= limit {
				hasMore = true
				break
			}
			obj := newItemFunc()
			itemKey := ComponentsToKey(apiGroup, resourceType, row.namespace, row.name)
			if derr := decode(s.codec, row.value, obj); derr != nil {
				return 0, "", false, storage.NewCorruptObjError(itemKey, derr)
			}
			rv := TimeuuidToResourceVersion(row.resourceVersion)
			if uerr := s.versioner.UpdateObject(obj, rv); uerr != nil {
				return 0, "", false, uerr
			}
			if rv > maxRV {
				maxRV = rv
			}
			if hasPredicate {
				if matched, merr := opts.Predicate.Matches(obj); merr != nil {
					return 0, "", false, merr
				} else if !matched {
					continue
				}
			}
			v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
			lastItemKey = itemKey
		}
		return maxRV, lastItemKey, hasMore, nil
	}

	// Bounded re-fetch loop: keep fetching from the base table until
	// limit items are accepted or scan cap is reached.
	remaining := limit
	maxScan := limit * maxScanMultiplier
	var scanned int64
	curNS, curName := continueNS, continueName

	for remaining > 0 && scanned < maxScan {
		fetchSize := min(remaining*int64(defaultOverscanFactor), maxScan-scanned)
		// Fetch one extra to detect hasMore
		rows, qerr := s.queryList(ctx, apiGroup, resourceType, namespace, curNS, curName, fetchSize+1)
		if qerr != nil {
			return 0, "", false, storage.NewInternalError(qerr)
		}
		if len(rows) == 0 {
			break
		}

		exhaustedPage := len(rows) <= int(fetchSize)
		if len(rows) > int(fetchSize) {
			rows = rows[:fetchSize]
		}

		for _, row := range rows {
			scanned++
			obj := newItemFunc()
			itemKey := ComponentsToKey(apiGroup, resourceType, row.namespace, row.name)
			if derr := decode(s.codec, row.value, obj); derr != nil {
				return 0, "", false, storage.NewCorruptObjError(itemKey, derr)
			}
			rv := TimeuuidToResourceVersion(row.resourceVersion)
			if uerr := s.versioner.UpdateObject(obj, rv); uerr != nil {
				return 0, "", false, uerr
			}
			if rv > maxRV {
				maxRV = rv
			}
			curNS = row.namespace
			curName = row.name

			if matched, merr := opts.Predicate.Matches(obj); merr != nil {
				return 0, "", false, merr
			} else if !matched {
				continue
			}
			v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
			lastItemKey = itemKey
			remaining--
			if remaining == 0 {
				hasMore = !exhaustedPage || scanned < int64(len(rows))
				break
			}
		}
		if exhaustedPage {
			break
		}
	}

	// If we hit the scan cap but still have remaining items, signal hasMore
	if remaining > 0 && scanned >= maxScan {
		hasMore = true
	}

	return maxRV, lastItemKey, hasMore, nil
}

// getListViaLabelIndex implements GetList using the kv_store_labels index
// table with a bounded re-fetch loop.
func (s *Store) getListViaLabelIndex(
	ctx context.Context,
	apiGroup, resourceType, namespace string,
	continueValue, continueNS, continueName string,
	cs classifiedSelector,
	opts storage.ListOptions,
	limit int64,
	v reflect.Value,
	newItemFunc func() runtime.Object,
) (maxRV uint64, lastItemKey string, lastLabelValue string, hasMore bool, err error) {
	paging := limit > 0
	remaining := limit
	maxScan := limit * maxScanMultiplier

	var scanned int64
	curNS, curName := continueNS, continueName
	curValue := continueValue

	for !paging || (remaining > 0 && scanned < maxScan) {

		var fetchSize int64
		if paging {
			fetchSize = max(min(remaining*int64(defaultOverscanFactor), maxScan-scanned), 1)
		}

		var queryLimit int64
		if paging {
			queryLimit = fetchSize + 1
		}

		candidates, rawCount, qerr := queryLabelIndex(
			ctx, s.session, s.keyspace,
			apiGroup, resourceType, *cs.primary,
			namespace,
			curValue, curNS, curName,
			queryLimit,
		)
		if qerr != nil {
			return 0, "", "", false, storage.NewInternalError(qerr)
		}

		// Use rawCount (pre-filter) to detect true CQL exhaustion.
		// len(candidates)==0 after filtering doesn't mean no more data.
		if rawCount == 0 {
			break
		}

		var exhaustedPage bool
		if !paging {
			exhaustedPage = true
		} else {
			exhaustedPage = len(candidates) <= int(fetchSize)
			if len(candidates) > int(fetchSize) {
				candidates = candidates[:fetchSize]
			}
		}

		for i, cand := range candidates {
			scanned++
			kc := KeyComponents{
				APIGroup:     apiGroup,
				ResourceType: resourceType,
				Namespace:    cand.namespace,
				Name:         cand.name,
			}
			data, rv, gerr := s.getRow(ctx, kc)
			if gerr != nil {
				return 0, "", "", false, storage.NewInternalError(gerr)
			}
			if data == nil {
				curValue = cand.labelValue
				curNS = cand.namespace
				curName = cand.name
				continue
			}

			obj := newItemFunc()
			itemKey := ComponentsToKey(apiGroup, resourceType, cand.namespace, cand.name)
			if derr := decode(s.codec, data, obj); derr != nil {
				return 0, "", "", false, storage.NewCorruptObjError(itemKey, derr)
			}
			rvVal := TimeuuidToResourceVersion(rv)
			if uerr := s.versioner.UpdateObject(obj, rvVal); uerr != nil {
				return 0, "", "", false, uerr
			}
			if rvVal > maxRV {
				maxRV = rvVal
			}

			curValue = cand.labelValue
			curNS = cand.namespace
			curName = cand.name

			if len(cs.residual) > 0 {
				objLabels := extractLabels(obj)
				if !residualMatches(objLabels, cs.residual) {
					continue
				}
			}
			if opts.Predicate.GetAttrs != nil {
				if matched, merr := opts.Predicate.Matches(obj); merr != nil {
					return 0, "", "", false, merr
				} else if !matched {
					continue
				}
			}

			v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
			lastItemKey = itemKey
			lastLabelValue = cand.labelValue
			if paging {
				remaining--
				if remaining == 0 {
					hasMore = !exhaustedPage || i < len(candidates)-1
					break
				}
			}
		}

		if exhaustedPage {
			break
		}
	}

	if paging && remaining > 0 && scanned >= maxScan {
		hasMore = true
	}

	return maxRV, lastItemKey, lastLabelValue, hasMore, nil
}

// GuaranteedUpdate implements storage.Interface.
func (s *Store) GuaranteedUpdate(
	ctx context.Context, key string, destination runtime.Object, ignoreNotFound bool,
	preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {

	preparedKey, err := s.prepareKey(key, false)
	if err != nil {
		return err
	}

	v, err := conversion.EnforcePtr(destination)
	if err != nil {
		return fmt.Errorf("unable to convert output object to pointer: %w", err)
	}

	kc, err := KeyToComponents(preparedKey)
	if err != nil {
		return storage.NewInternalError(err)
	}

	for range maxCASRetries {
		existingData, existingRV, err := s.getRow(ctx, kc)
		if err != nil {
			return storage.NewInternalError(err)
		}

		var existing runtime.Object
		var currentRV uint64
		if existingData == nil {
			if !ignoreNotFound {
				return storage.NewKeyNotFoundError(preparedKey, 0)
			}
			existing = reflect.New(v.Type()).Interface().(runtime.Object)
		} else {
			existing = s.newFunc()
			if err := decode(s.codec, existingData, existing); err != nil {
				return storage.NewCorruptObjError(preparedKey, err)
			}
			currentRV = TimeuuidToResourceVersion(existingRV)
			if err := s.versioner.UpdateObject(existing, currentRV); err != nil {
				return err
			}
		}

		if err := preconditions.Check(preparedKey, existing); err != nil {
			return err
		}

		// Snapshot the pre-mutation object for label diff and critical-field
		// detection. tryUpdate mutates existing in-place (and usually returns
		// the same pointer), so comparisons must use this snapshot.
		oldLabels := extractLabels(existing)
		var existingSnapshot runtime.Object
		if s.criticalFieldDetector != nil && existingData != nil {
			existingSnapshot = s.newFunc()
			if err := decode(s.codec, existingData, existingSnapshot); err != nil {
				return storage.NewCorruptObjError(preparedKey, err)
			}
		}

		ret, _, err := tryUpdate(existing, storage.ResponseMeta{ResourceVersion: currentRV})
		if err != nil {
			return err
		}

		if err := s.versioner.PrepareObjectForStorage(ret); err != nil {
			return fmt.Errorf("PrepareObjectForStorage failed: %w", err)
		}
		newData, err := runtime.Encode(s.codec, ret)
		if err != nil {
			return err
		}

		// Short-circuit if no data change.
		if existingData != nil && bytes.Equal(newData, existingData) {
			if err := decode(s.codec, existingData, destination); err != nil {
				return storage.NewCorruptObjError(preparedKey, err)
			}
			return s.versioner.UpdateObject(destination, currentRV)
		}

		newRV := gocql.TimeUUID()
		useCrossDCSerial := existingSnapshot != nil &&
			s.criticalFieldDetector(existingSnapshot, ret)

		if existingData == nil {
			applied, casErr := s.casInsert(ctx, kc, newData, newRV)
			if casErr != nil {
				return storage.NewInternalError(casErr)
			}
			if !applied {
				klog.V(4).InfoS("GuaranteedUpdate insert CAS failed, retrying",
					"key", preparedKey)
				continue
			}
			oldLabels = nil
		} else {
			applied, casErr := s.casUpdateWithConsistency(ctx, kc, newData, newRV, existingRV, useCrossDCSerial)
			if casErr != nil {
				return storage.NewInternalError(casErr)
			}
			if !applied {
				klog.V(4).InfoS("GuaranteedUpdate CAS failed, retrying",
					"key", preparedKey)
				continue
			}
		}

		// Sync label index (best-effort)
		newLabels := extractLabels(ret)
		if len(oldLabels) > 0 || len(newLabels) > 0 {
			if serr := syncLabels(s.session, s.keyspace, kc, oldLabels, newLabels); serr != nil {
				klog.ErrorS(serr, "Failed to sync label index on update", "key", preparedKey)
			}
		}

		if err := decode(s.codec, newData, destination); err != nil {
			return storage.NewCorruptObjError(preparedKey, err)
		}
		return s.versioner.UpdateObject(destination, TimeuuidToResourceVersion(newRV))
	}
	return storage.NewResourceVersionConflictsError(preparedKey, 0)
}

// Stats implements storage.Interface.
func (s *Store) Stats(ctx context.Context) (storage.Stats, error) {
	apiGroup, resourceType, _, err := KeyPrefixToComponents(s.resourcePrefix)
	if err != nil {
		return storage.Stats{}, storage.NewInternalError(err)
	}

	cql := fmt.Sprintf(
		`SELECT count(*) FROM %s.kv_store WHERE api_group = ? AND resource_type = ?`,
		s.keyspace,
	)
	var count int64
	if err := s.session.Query(cql, apiGroup, resourceType).WithContext(ctx).Scan(&count); err != nil {
		return storage.Stats{}, storage.NewInternalError(err)
	}
	return storage.Stats{ObjectCount: count}, nil
}

// ReadinessCheck implements storage.Interface.
func (s *Store) ReadinessCheck() error {
	if s.session == nil || s.session.Closed() {
		return storage.ErrStorageNotReady
	}
	return nil
}

// RequestWatchProgress implements storage.Interface (etcd-specific, no-op for ScyllaDB).
func (s *Store) RequestWatchProgress(_ context.Context) error {
	return nil
}

// GetCurrentResourceVersion implements storage.Interface.
func (s *Store) GetCurrentResourceVersion(ctx context.Context) (uint64, error) {
	emptyList := s.newListFunc()
	err := s.GetList(ctx, s.resourcePrefix, storage.ListOptions{
		Predicate: storage.SelectionPredicate{
			Limit: 1,
		},
		Recursive: true,
	}, emptyList)
	if err != nil {
		return 0, err
	}
	listAccessor, err := meta.ListAccessor(emptyList)
	if err != nil {
		return 0, err
	}
	if listAccessor == nil {
		return 0, fmt.Errorf("unable to extract a list accessor from %T", emptyList)
	}
	rvStr := listAccessor.GetResourceVersion()
	if rvStr == "" || rvStr == "0" {
		rv := TimeuuidToResourceVersion(gocql.TimeUUID())
		return rv, nil
	}
	return strconv.ParseUint(rvStr, 10, 64)
}

// EnableResourceSizeEstimation implements storage.Interface (no-op for ScyllaDB).
func (s *Store) EnableResourceSizeEstimation(_ storage.KeysFunc) error {
	return nil
}

// CompactRevision implements storage.Interface (etcd-specific, returns 0 for ScyllaDB).
func (s *Store) CompactRevision() int64 {
	return 0
}

// ---------- internal helpers ----------

func (s *Store) prepareKey(key string, recursive bool) (string, error) {
	return storage.PrepareKey(s.resourcePrefix, key, recursive)
}

type listRow struct {
	namespace       string
	name            string
	value           []byte
	resourceVersion gocql.UUID
}

func (s *Store) getRow(
	ctx context.Context, kc KeyComponents,
) ([]byte, gocql.UUID, error) {
	cql := fmt.Sprintf(`SELECT value, resource_version FROM %s.kv_store`+
		` WHERE api_group = ? AND resource_type = ?`+
		` AND namespace = ? AND name = ?`, s.keyspace)
	var data []byte
	var rv gocql.UUID
	err := s.session.Query(cql, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name).
		WithContext(ctx).
		Scan(&data, &rv)
	if err == gocql.ErrNotFound {
		return nil, gocql.UUID{}, nil
	}
	if err != nil {
		return nil, gocql.UUID{}, err
	}
	return data, rv, nil
}

func (s *Store) casInsert(
	ctx context.Context, kc KeyComponents, data []byte, rv gocql.UUID,
) (bool, error) {
	cql := fmt.Sprintf(
		`INSERT INTO %s.kv_store`+
			` (api_group, resource_type, namespace, name, value, resource_version)`+
			` VALUES (?, ?, ?, ?, ?, ?) IF NOT EXISTS`,
		s.keyspace,
	)
	result := make(map[string]any)
	applied, err := s.session.Query(cql, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name, data, rv).
		WithContext(ctx).
		SerialConsistency(gocql.LocalSerial).
		MapScanCAS(result)
	if err != nil {
		return false, err
	}
	return applied, nil
}

// casUpdateWithConsistency performs a CAS update with configurable serial
// consistency. When crossDC is true, Serial consistency is used for
// cross-DC Paxos (LWT). If the cross-DC CAS fails for any infrastructure
// reason (DC down, timeout, connection loss), it falls back to an
// unconditional LOCAL_ONE write. This trades linearizability for
// availability: the surviving DC can always make progress, but concurrent
// writers on a partitioned remote DC may produce last-write-wins conflicts
// that are reconciled after the partition heals.
func (s *Store) casUpdateWithConsistency(
	ctx context.Context, kc KeyComponents,
	data []byte, newRV, oldRV gocql.UUID,
	crossDC bool,
) (bool, error) {
	casCQL := fmt.Sprintf(
		`UPDATE %s.kv_store SET value = ?, resource_version = ?`+
			` WHERE api_group = ? AND resource_type = ?`+
			` AND namespace = ? AND name = ?`+
			` IF resource_version = ?`,
		s.keyspace,
	)
	casArgs := []any{data, newRV, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name, oldRV}

	serialCL := gocql.LocalSerial
	if crossDC {
		serialCL = gocql.Serial
	}

	result := make(map[string]any)
	applied, err := s.session.Query(casCQL, casArgs...).
		WithContext(ctx).
		SerialConsistency(serialCL).
		MapScanCAS(result)

	if err != nil && crossDC && shouldFallbackToLocal(err) {
		klog.InfoS("Cross-DC Serial CAS unavailable, falling back to unconditional LOCAL_ONE write",
			"apiGroup", kc.APIGroup, "resourceType", kc.ResourceType,
			"namespace", kc.Namespace, "name", kc.Name, "error", err)
		err = s.unconditionalUpdate(ctx, kc, data, newRV)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	if err != nil {
		return false, err
	}
	return applied, nil
}

// unconditionalUpdate writes the row without a CAS condition using
// LOCAL_ONE consistency. Used as a degraded-mode fallback when cross-DC
// Paxos is unreachable.
func (s *Store) unconditionalUpdate(
	ctx context.Context, kc KeyComponents,
	data []byte, newRV gocql.UUID,
) error {
	cql := fmt.Sprintf(
		`UPDATE %s.kv_store SET value = ?, resource_version = ?`+
			` WHERE api_group = ? AND resource_type = ?`+
			` AND namespace = ? AND name = ?`,
		s.keyspace,
	)
	return s.session.Query(cql, data, newRV, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name).
		WithContext(ctx).
		Consistency(gocql.LocalOne).
		Exec()
}

// shouldFallbackToLocal returns true if a failed Serial CAS should be
// retried as an unconditional local write. Any infrastructure error
// (unavailable, timeout, connection loss) triggers the fallback. The only
// exception is explicit context cancellation, which means the caller
// abandoned the operation.
func shouldFallbackToLocal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

func (s *Store) casDeleteRow(
	ctx context.Context, kc KeyComponents, oldRV gocql.UUID,
) (bool, error) {
	cql := fmt.Sprintf(
		`DELETE FROM %s.kv_store`+
			` WHERE api_group = ? AND resource_type = ?`+
			` AND namespace = ? AND name = ?`+
			` IF resource_version = ?`,
		s.keyspace,
	)
	result := make(map[string]any)
	applied, err := s.session.Query(cql, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name, oldRV).
		WithContext(ctx).
		SerialConsistency(gocql.LocalSerial).
		MapScanCAS(result)
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (s *Store) queryList(
	ctx context.Context,
	apiGroup, resourceType, namespace string,
	continueNS, continueName string,
	limit int64,
) ([]listRow, error) {
	var cql string
	var args []any

	hasNamespace := namespace != ""
	hasContinue := continueName != ""

	base := fmt.Sprintf(
		`SELECT namespace, name, value, resource_version`+
			` FROM %s.kv_store`+
			` WHERE api_group = ? AND resource_type = ?`,
		s.keyspace,
	)
	args = []any{apiGroup, resourceType}

	switch {
	case hasNamespace && hasContinue:
		cql = base + ` AND namespace = ? AND name > ?`
		args = append(args, namespace, continueName)
	case hasNamespace:
		cql = base + ` AND namespace = ?`
		args = append(args, namespace)
	case hasContinue:
		cql = base + ` AND (namespace, name) > (?, ?)`
		args = append(args, continueNS, continueName)
	default:
		cql = base
	}

	if limit > 0 {
		cql += " LIMIT ?"
		args = append(args, limit)
	}

	iter := s.session.Query(cql, args...).WithContext(ctx).Iter()
	var rows []listRow
	var row listRow
	for iter.Scan(&row.namespace, &row.name, &row.value, &row.resourceVersion) {
		rows = append(rows, row)
		row = listRow{}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) getSingleItemList(
	ctx context.Context, key string, opts storage.ListOptions,
	listObj runtime.Object, v reflect.Value,
) error {
	kc, err := KeyToComponents(key)
	if err != nil {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return s.versioner.UpdateList(
			listObj, TimeuuidToResourceVersion(gocql.TimeUUID()), "", nil)
	}

	data, rv, err := s.getRow(ctx, kc)
	if err != nil {
		return storage.NewInternalError(err)
	}

	var listRV uint64
	if data != nil {
		obj := getNewItemFunc(v)()
		if err := decode(s.codec, data, obj); err != nil {
			return storage.NewCorruptObjError(key, err)
		}
		listRV = TimeuuidToResourceVersion(rv)
		if err := s.versioner.UpdateObject(obj, listRV); err != nil {
			return err
		}
		if opts.Predicate.GetAttrs != nil {
			if matched, matchErr := opts.Predicate.Matches(obj); matchErr != nil {
				return matchErr
			} else if matched {
				v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
			}
		} else {
			v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
		}
	}

	if v.IsNil() {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}
	if listRV == 0 {
		listRV = TimeuuidToResourceVersion(gocql.TimeUUID())
	}
	return s.versioner.UpdateList(listObj, listRV, "", nil)
}

// parseContinueKey extracts namespace, name, and an optional labelValue from a
// decoded continue key. The label-index path encodes the last candidate's
// label_value after the name using a \x01 separator so that in/exists
// selectors can resume from the correct (label_value, namespace, name) position.
func parseContinueKey(continueKey, keyPrefix string) (namespace, name, labelValue string, err error) {
	relative := strings.TrimPrefix(continueKey, keyPrefix)
	relative = strings.TrimSuffix(relative, "\x00")
	relative = strings.TrimPrefix(relative, "/")
	relative = strings.TrimSuffix(relative, "/")

	parts := strings.SplitN(relative, "/", 2)
	switch len(parts) {
	case 2:
		namespace = parts[0]
		name = parts[1]
	case 1:
		name = parts[0]
	default:
		return "", "", "", fmt.Errorf("invalid continue key %q", continueKey)
	}

	// Extract embedded label_value (used by label-index pagination)
	if idx := strings.IndexByte(name, '\x01'); idx >= 0 {
		labelValue = name[idx+1:]
		name = name[:idx]
	}
	return namespace, name, labelValue, nil
}

func getNewItemFunc(v reflect.Value) func() runtime.Object {
	elem := v.Type().Elem()
	return func() runtime.Object {
		return reflect.New(elem).Interface().(runtime.Object)
	}
}

func decode(codec runtime.Codec, data []byte, into runtime.Object) error {
	return runtime.DecodeInto(codec, data, into)
}
