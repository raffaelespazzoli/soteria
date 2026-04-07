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
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/gocql/gocql"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"
)

const maxCASRetries = 5

// Store implements storage.Interface backed by ScyllaDB's kv_store table.
type Store struct {
	session        *gocql.Session
	codec          runtime.Codec
	versioner      storage.Versioner
	keyspace       string
	groupResource  schema.GroupResource
	resourcePrefix string
	newFunc        func() runtime.Object
	newListFunc    func() runtime.Object
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
}

var _ storage.Interface = (*Store)(nil)

// NewStore creates a new ScyllaDB-backed storage.Interface.
func NewStore(cfg StoreConfig) *Store {
	return &Store{
		session:        cfg.Session,
		codec:          cfg.Codec,
		versioner:      NewVersioner(),
		keyspace:       cfg.Keyspace,
		groupResource:  cfg.GroupResource,
		resourcePrefix: cfg.ResourcePrefix,
		newFunc:        cfg.NewFunc,
		newListFunc:    cfg.NewListFunc,
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

// Watch implements storage.Interface. Watch via CDC is implemented in Story 1.4.
func (s *Store) Watch(_ context.Context, _ string, _ storage.ListOptions) (watch.Interface, error) {
	return nil, storage.NewInternalError(fmt.Errorf("watch not yet implemented"))
}

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

	var continueNS, continueName string
	if continueKey != "" {
		continueNS, continueName, err = parseContinueKey(continueKey, preparedKey)
		if err != nil {
			return err
		}
	}

	limit := opts.Predicate.Limit
	paging := limit > 0

	// Fetch one extra row to detect whether more items exist beyond this page.
	var fetchLimit int64
	if paging {
		fetchLimit = limit + 1
	}

	rows, err := s.queryList(ctx, apiGroup, resourceType, namespace, continueNS, continueName, fetchLimit)
	if err != nil {
		return storage.NewInternalError(err)
	}

	newItemFunc := getNewItemFunc(v)
	var maxRV uint64
	var lastItemKey string
	var hasMore bool

	for _, row := range rows {
		if paging && int64(v.Len()) >= limit {
			hasMore = true
			break
		}

		obj := newItemFunc()
		itemKey := ComponentsToKey(apiGroup, resourceType, row.namespace, row.name)
		if err := decode(s.codec, row.value, obj); err != nil {
			return storage.NewCorruptObjError(itemKey, err)
		}
		rv := TimeuuidToResourceVersion(row.resourceVersion)
		if err := s.versioner.UpdateObject(obj, rv); err != nil {
			return err
		}
		if rv > maxRV {
			maxRV = rv
		}

		if opts.Predicate.GetAttrs != nil {
			if matched, err := opts.Predicate.Matches(obj); err != nil {
				return err
			} else if !matched {
				continue
			}
		}

		v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
		lastItemKey = itemKey
	}

	if v.IsNil() {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}

	// Establish a stable list resourceVersion:
	// - If withRev > 0 (from continue token or explicit RV), preserve it
	//   across pages so paginated lists see a consistent version.
	// - Otherwise use the max RV observed in this page.
	// - For empty results fall back to the current time.
	var listRV uint64
	switch {
	case withRev > 0:
		listRV = uint64(withRev)
	case maxRV > 0:
		listRV = maxRV
	default:
		listRV = TimeuuidToResourceVersion(gocql.TimeUUID())
	}

	var continueValue string
	if hasMore && lastItemKey != "" {
		continueValue, err = storage.EncodeContinue(
			lastItemKey+"\x00", preparedKey, int64(listRV))
		if err != nil {
			return err
		}
	}

	return s.versioner.UpdateList(listObj, listRV, continueValue, nil)
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
		} else {
			applied, casErr := s.casUpdate(ctx, kc, newData, newRV, existingRV)
			if casErr != nil {
				return storage.NewInternalError(casErr)
			}
			if !applied {
				klog.V(4).InfoS("GuaranteedUpdate CAS failed, retrying",
					"key", preparedKey)
				continue
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

func (s *Store) casUpdate(
	ctx context.Context, kc KeyComponents,
	data []byte, newRV, oldRV gocql.UUID,
) (bool, error) {
	cql := fmt.Sprintf(
		`UPDATE %s.kv_store SET value = ?, resource_version = ?`+
			` WHERE api_group = ? AND resource_type = ?`+
			` AND namespace = ? AND name = ?`+
			` IF resource_version = ?`,
		s.keyspace,
	)
	result := make(map[string]any)
	applied, err := s.session.Query(cql, data, newRV, kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name, oldRV).
		WithContext(ctx).
		SerialConsistency(gocql.LocalSerial).
		MapScanCAS(result)
	if err != nil {
		return false, err
	}
	return applied, nil
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

// parseContinueKey extracts namespace and name from a decoded continue key.
// The continue key includes a trailing \x00 appended by the standard k8s
// continue token encoding; we strip it here.
func parseContinueKey(continueKey, keyPrefix string) (namespace, name string, err error) {
	relative := strings.TrimPrefix(continueKey, keyPrefix)
	relative = strings.TrimSuffix(relative, "\x00")
	relative = strings.TrimPrefix(relative, "/")
	relative = strings.TrimSuffix(relative, "/")

	parts := strings.SplitN(relative, "/", 2)
	switch len(parts) {
	case 2:
		return parts[0], parts[1], nil
	case 1:
		return "", parts[0], nil
	default:
		return "", "", fmt.Errorf("invalid continue key %q", continueKey)
	}
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
