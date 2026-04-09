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

// Package scylladb implements k8s.io/apiserver/pkg/storage.Interface backed
// by ScyllaDB's generic key-value store table (kv_store). It provides CRUD
// operations using gocql with LOCAL_ONE consistency for reads/writes and
// optional cross-DC lightweight transactions (LWT) for critical state-machine
// fields. Watch is implemented via ScyllaDB CDC (Change Data Capture) using
// scylla-cdc-go, with an initial SELECT snapshot followed by real-time CDC
// stream consumption. The package also manages label-indexed pagination via
// a secondary kv_store_labels table, ResourceVersion mapping from CDC
// Timeuuid to monotonic uint64, and an in-memory object cache for
// reconstructing full objects on DELETE events.
package scylladb
