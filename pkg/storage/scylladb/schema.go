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
	"strings"

	"github.com/gocql/gocql"
)

// SchemaConfig holds configuration for keyspace and table creation.
type SchemaConfig struct {
	// Keyspace is the keyspace name (e.g., "soteria").
	Keyspace string
	// Strategy is the replication strategy class.
	// Use "SimpleStrategy" for testing, "NetworkTopologyStrategy" for production.
	Strategy string
	// ReplicationFactor is used with SimpleStrategy (e.g., 1 for test).
	ReplicationFactor int
	// DCReplication maps datacenter names to replication factors.
	// Used with NetworkTopologyStrategy (e.g., {"dc1": 2, "dc2": 2}).
	DCReplication map[string]int
	// DisableTablets appends AND TABLETS = {'enabled': false} to the
	// CREATE KEYSPACE statement. ScyllaDB tablets must be disabled for
	// CDC to work.
	DisableTablets bool
}

// EnsureKeyspace creates the keyspace if it does not already exist, using the
// replication strategy specified in cfg.
func EnsureKeyspace(session *gocql.Session, cfg SchemaConfig) error {
	if cfg.Keyspace == "" {
		return fmt.Errorf("keyspace name is required")
	}
	if cfg.Strategy == "" {
		return fmt.Errorf("replication strategy is required")
	}

	var replicationMap string
	switch cfg.Strategy {
	case "SimpleStrategy":
		if cfg.ReplicationFactor < 1 {
			return fmt.Errorf("replication factor must be >= 1 for SimpleStrategy")
		}
		replicationMap = fmt.Sprintf("{'class': 'SimpleStrategy', 'replication_factor': %d}", cfg.ReplicationFactor)
	case "NetworkTopologyStrategy":
		if len(cfg.DCReplication) == 0 {
			return fmt.Errorf("at least one datacenter replication factor is required for NetworkTopologyStrategy")
		}
		parts := make([]string, 0, len(cfg.DCReplication)+1)
		parts = append(parts, "'class': 'NetworkTopologyStrategy'")
		for dc, rf := range cfg.DCReplication {
			parts = append(parts, fmt.Sprintf("'%s': %d", dc, rf))
		}
		replicationMap = "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Errorf("unsupported replication strategy: %s", cfg.Strategy)
	}

	cql := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = %s",
		cfg.Keyspace, replicationMap,
	)
	if cfg.DisableTablets {
		cql += " AND TABLETS = {'enabled': false}"
	}
	return session.Query(cql).Exec()
}

// KeyspaceExists returns true if the named keyspace already exists in
// ScyllaDB's system schema.
func KeyspaceExists(session *gocql.Session, keyspace string) (bool, error) {
	var count int
	err := session.Query(
		`SELECT count(*) FROM system_schema.keyspaces WHERE keyspace_name = ?`,
		keyspace,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("querying system_schema.keyspaces: %w", err)
	}
	return count > 0, nil
}

// EnsureTable creates the kv_store table with CDC enabled if it does not
// already exist. The caller must have already created the keyspace.
func EnsureTable(session *gocql.Session, keyspace string) error {
	if keyspace == "" {
		return fmt.Errorf("keyspace name is required")
	}

	cql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.kv_store (
    api_group text,
    resource_type text,
    namespace text,
    name text,
    value blob,
    resource_version timeuuid,
    PRIMARY KEY ((api_group, resource_type), namespace, name)
) WITH cdc = {'enabled': true}`, keyspace)

	return session.Query(cql).Exec()
}

// EnsureLabelsTable creates the kv_store_labels index table if it does not
// already exist. This normalized table enables server-side label filtering
// for GetList queries, working around ScyllaDB's lack of SAI/ENTRIES indexes
// on map columns.
func EnsureLabelsTable(session *gocql.Session, keyspace string) error {
	if keyspace == "" {
		return fmt.Errorf("keyspace name is required")
	}

	cql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.kv_store_labels (
    api_group text,
    resource_type text,
    label_key text,
    label_value text,
    namespace text,
    name text,
    PRIMARY KEY ((api_group, resource_type, label_key), label_value, namespace, name)
)`, keyspace)

	return session.Query(cql).Exec()
}

// ValidateKeyspaceTopology checks that the keyspace already exists and uses
// NetworkTopologyStrategy with replication configured for localDC. This is
// called on startup in multi-DC mode to catch misconfigured deployments
// early instead of silently running against a SimpleStrategy keyspace.
func ValidateKeyspaceTopology(session *gocql.Session, keyspace, localDC string) error {
	if keyspace == "" {
		return fmt.Errorf("keyspace name is required")
	}
	if localDC == "" {
		return fmt.Errorf("local datacenter name is required")
	}

	var strategy string
	var replication map[string]string
	err := session.Query(
		`SELECT replication FROM system_schema.keyspaces WHERE keyspace_name = ?`,
		keyspace,
	).Scan(&replication)
	if err != nil {
		return fmt.Errorf("querying keyspace %q: %w", keyspace, err)
	}

	strategy = replication["class"]
	if !strings.HasSuffix(strategy, "NetworkTopologyStrategy") {
		return fmt.Errorf(
			"keyspace %q uses %q but multi-DC mode requires NetworkTopologyStrategy",
			keyspace, strategy,
		)
	}

	rf, ok := replication[localDC]
	if !ok || rf == "" || rf == "0" {
		return fmt.Errorf(
			"keyspace %q has no replication configured for local datacenter %q (replication: %v)",
			keyspace, localDC, replication,
		)
	}

	return nil
}

// EnsureSchema orchestrates idempotent keyspace, kv_store table, and
// kv_store_labels index table creation.
func EnsureSchema(session *gocql.Session, cfg SchemaConfig) error {
	if err := EnsureKeyspace(session, cfg); err != nil {
		return fmt.Errorf("ensuring keyspace: %w", err)
	}
	if err := EnsureTable(session, cfg.Keyspace); err != nil {
		return fmt.Errorf("ensuring kv_store table: %w", err)
	}
	if err := EnsureLabelsTable(session, cfg.Keyspace); err != nil {
		return fmt.Errorf("ensuring kv_store_labels table: %w", err)
	}
	return nil
}
