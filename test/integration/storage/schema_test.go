//go:build integration

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

package storage_test

import (
	"testing"

	"github.com/soteria-project/soteria/pkg/storage/scylladb"
)

const testKeyspace = "soteria_test"

func TestEnsureKeyspace_SimpleStrategy(t *testing.T) {
	cfg := scylladb.SchemaConfig{
		Keyspace:          testKeyspace,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}

	if err := scylladb.EnsureKeyspace(testSession, cfg); err != nil {
		t.Fatalf("EnsureKeyspace failed: %v", err)
	}

	// Idempotent — running again must succeed
	if err := scylladb.EnsureKeyspace(testSession, cfg); err != nil {
		t.Fatalf("idempotent EnsureKeyspace failed: %v", err)
	}

	var ksName string
	err := testSession.Query(
		"SELECT keyspace_name FROM system_schema.keyspaces WHERE keyspace_name = ?",
		testKeyspace,
	).Scan(&ksName)
	if err != nil {
		t.Fatalf("querying keyspace: %v", err)
	}
	if ksName != testKeyspace {
		t.Fatalf("expected keyspace %q, got %q", testKeyspace, ksName)
	}
}

func TestEnsureKeyspace_Validation(t *testing.T) {
	tests := []struct {
		name string
		cfg  scylladb.SchemaConfig
	}{
		{
			name: "empty keyspace",
			cfg:  scylladb.SchemaConfig{Strategy: "SimpleStrategy", ReplicationFactor: 1},
		},
		{
			name: "empty strategy",
			cfg:  scylladb.SchemaConfig{Keyspace: "test"},
		},
		{
			name: "unsupported strategy",
			cfg:  scylladb.SchemaConfig{Keyspace: "test", Strategy: "Invalid"},
		},
		{
			name: "SimpleStrategy zero RF",
			cfg:  scylladb.SchemaConfig{Keyspace: "test", Strategy: "SimpleStrategy", ReplicationFactor: 0},
		},
		{
			name: "NetworkTopologyStrategy no DCs",
			cfg:  scylladb.SchemaConfig{Keyspace: "test", Strategy: "NetworkTopologyStrategy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := scylladb.EnsureKeyspace(testSession, tt.cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEnsureTable_CreatesKVStore(t *testing.T) {
	cfg := scylladb.SchemaConfig{
		Keyspace:          testKeyspace,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}
	if err := scylladb.EnsureKeyspace(testSession, cfg); err != nil {
		t.Fatalf("EnsureKeyspace failed: %v", err)
	}

	if err := scylladb.EnsureTable(testSession, testKeyspace); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}

	// Idempotent
	if err := scylladb.EnsureTable(testSession, testKeyspace); err != nil {
		t.Fatalf("idempotent EnsureTable failed: %v", err)
	}
}

func TestEnsureTable_EmptyKeyspace(t *testing.T) {
	if err := scylladb.EnsureTable(testSession, ""); err == nil {
		t.Fatal("expected error for empty keyspace")
	}
}

func TestEnsureTable_SchemaStructure(t *testing.T) {
	cfg := scylladb.SchemaConfig{
		Keyspace:          testKeyspace,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}
	if err := scylladb.EnsureSchema(testSession, cfg); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	expectedColumns := map[string]string{
		"api_group":        "text",
		"resource_type":    "text",
		"namespace":        "text",
		"name":             "text",
		"value":            "blob",
		"resource_version": "timeuuid",
	}

	iter := testSession.Query(
		"SELECT column_name, type FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?",
		testKeyspace, "kv_store",
	).Iter()

	foundColumns := make(map[string]string)
	var colName, colType string
	for iter.Scan(&colName, &colType) {
		foundColumns[colName] = colType
	}
	if err := iter.Close(); err != nil {
		t.Fatalf("iterating columns: %v", err)
	}

	for name, expectedType := range expectedColumns {
		actualType, ok := foundColumns[name]
		if !ok {
			t.Errorf("expected column %q not found", name)
			continue
		}
		if actualType != expectedType {
			t.Errorf("column %q: expected type %q, got %q", name, expectedType, actualType)
		}
	}

	if len(foundColumns) != len(expectedColumns) {
		t.Errorf("expected %d columns, found %d: %v", len(expectedColumns), len(foundColumns), foundColumns)
	}
}

func TestEnsureTable_CDCEnabled(t *testing.T) {
	cfg := scylladb.SchemaConfig{
		Keyspace:          testKeyspace,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}
	if err := scylladb.EnsureSchema(testSession, cfg); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	var count int
	err := testSession.Query(
		"SELECT count(*) FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?",
		testKeyspace, "kv_store_scylla_cdc_log",
	).Scan(&count)
	if err != nil {
		t.Fatalf("querying CDC log table: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected CDC log table to exist (count=1), got count=%d", count)
	}
}

func TestEnsureSchema_Orchestration(t *testing.T) {
	ks := "soteria_schema_test"
	cfg := scylladb.SchemaConfig{
		Keyspace:          ks,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}

	if err := scylladb.EnsureSchema(testSession, cfg); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	// Verify keyspace
	var ksName string
	err := testSession.Query(
		"SELECT keyspace_name FROM system_schema.keyspaces WHERE keyspace_name = ?",
		ks,
	).Scan(&ksName)
	if err != nil {
		t.Fatalf("querying keyspace: %v", err)
	}

	// Verify table
	var tableName string
	err = testSession.Query(
		"SELECT table_name FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?",
		ks, "kv_store",
	).Scan(&tableName)
	if err != nil {
		t.Fatalf("querying table: %v", err)
	}
	if tableName != "kv_store" {
		t.Fatalf("expected table kv_store, got %q", tableName)
	}

	// Idempotent
	if err := scylladb.EnsureSchema(testSession, cfg); err != nil {
		t.Fatalf("idempotent EnsureSchema failed: %v", err)
	}
}
