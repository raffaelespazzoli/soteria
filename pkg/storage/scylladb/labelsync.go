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
	"fmt"

	"github.com/gocql/gocql"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// labelDiff computes the label rows to add and remove when transitioning
// from oldLabels to newLabels. Unchanged labels are omitted from both sets.
func labelDiff(oldLabels, newLabels map[string]string) (added, removed map[string]string) {
	added = make(map[string]string)
	removed = make(map[string]string)

	for k, v := range newLabels {
		if oldVal, ok := oldLabels[k]; !ok || oldVal != v {
			added[k] = v
		}
	}
	for k, v := range oldLabels {
		if newVal, ok := newLabels[k]; !ok || newVal != v {
			removed[k] = v
		}
	}
	return added, removed
}

// extractLabels returns the labels map from a runtime.Object using meta.Accessor.
func extractLabels(obj runtime.Object) map[string]string {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil
	}
	return accessor.GetLabels()
}

// syncLabels updates the kv_store_labels index table to reflect a label
// transition from oldLabels to newLabels for the given object key.
// Uses an UNLOGGED BATCH for atomicity within ScyllaDB's best-effort model.
func syncLabels(
	session *gocql.Session, keyspace string, kc KeyComponents,
	oldLabels, newLabels map[string]string,
) error {
	added, removed := labelDiff(oldLabels, newLabels)
	if len(added) == 0 && len(removed) == 0 {
		return nil
	}

	batch := session.NewBatch(gocql.UnloggedBatch)

	insertCQL := fmt.Sprintf(
		`INSERT INTO %s.kv_store_labels`+
			` (api_group, resource_type, label_key, label_value, namespace, name)`+
			` VALUES (?, ?, ?, ?, ?, ?)`,
		keyspace,
	)
	for k, v := range added {
		batch.Query(insertCQL, kc.APIGroup, kc.ResourceType, k, v, kc.Namespace, kc.Name)
	}

	deleteCQL := fmt.Sprintf(
		`DELETE FROM %s.kv_store_labels`+
			` WHERE api_group = ? AND resource_type = ?`+
			` AND label_key = ? AND label_value = ?`+
			` AND namespace = ? AND name = ?`,
		keyspace,
	)
	for k, v := range removed {
		batch.Query(deleteCQL, kc.APIGroup, kc.ResourceType, k, v, kc.Namespace, kc.Name)
	}

	return session.ExecuteBatch(batch)
}

// deleteAllLabels removes all label index rows for the given object.
func deleteAllLabels(session *gocql.Session, keyspace string, kc KeyComponents, labels map[string]string) error {
	if len(labels) == 0 {
		return nil
	}

	batch := session.NewBatch(gocql.UnloggedBatch)
	deleteCQL := fmt.Sprintf(
		`DELETE FROM %s.kv_store_labels`+
			` WHERE api_group = ? AND resource_type = ?`+
			` AND label_key = ? AND label_value = ?`+
			` AND namespace = ? AND name = ?`,
		keyspace,
	)
	for k, v := range labels {
		batch.Query(deleteCQL, kc.APIGroup, kc.ResourceType, k, v, kc.Namespace, kc.Name)
	}

	return session.ExecuteBatch(batch)
}
