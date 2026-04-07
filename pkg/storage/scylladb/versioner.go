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
	"time"

	"github.com/gocql/gocql"
	"k8s.io/apiserver/pkg/storage"
)

// NewVersioner returns the standard Kubernetes APIObjectVersioner.
// The Timeuuid-to-uint64 mapping happens in the store layer via the helper
// functions below; the Versioner only handles uint64 ↔ string on ObjectMeta.
func NewVersioner() storage.Versioner {
	return storage.APIObjectVersioner{}
}

// TimeuuidToResourceVersion extracts the embedded timestamp from a version-1
// UUID (Timeuuid) and returns it as Unix microseconds. The result is
// monotonically increasing within a single ScyllaDB datacenter.
func TimeuuidToResourceVersion(id gocql.UUID) uint64 {
	return uint64(id.Time().UnixMicro())
}

// ResourceVersionToMinTimeuuid converts a resourceVersion (Unix microseconds)
// to the corresponding time.Time. Use with gocql.MinTimeUUID(t) or
// gocql.MaxTimeUUID(t) for CQL range queries on the resource_version column.
func ResourceVersionToMinTimeuuid(rv uint64) time.Time {
	return time.UnixMicro(int64(rv))
}
