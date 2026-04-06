# Story 1.2: ScyllaDB Connection & Generic KV Schema

Status: ready-for-dev

## Story

As a developer,
I want a ScyllaDB client with mTLS support and the generic KV store table with CDC enabled,
so that the storage backend is ready for storage.Interface implementation.

## Acceptance Criteria

1. **Given** a running ScyllaDB cluster (or testcontainers instance), **When** the ScyllaDB client in `pkg/storage/scylladb/client.go` is initialized with mTLS certificates, **Then** the client establishes a connection authenticated via client certificates from cert-manager, **And** the connection is encrypted with TLS, **And** no password-based authentication is used.

2. **Given** an established ScyllaDB connection, **When** the schema initializer in `pkg/storage/scylladb/schema.go` runs, **Then** a `kv_store` table is created with columns: `api_group` (text), `resource_type` (text), `namespace` (text), `name` (text), `value` (blob), `resource_version` (timeuuid), **And** the primary key is `((api_group, resource_type), namespace, name)` where `(api_group, resource_type)` is the partition key and `(namespace, name)` are clustering columns, **And** CDC is enabled on the `kv_store` table for change stream consumption, **And** table and column names use snake_case.

3. **Given** a ScyllaDB client, **When** the connection is lost and re-established, **Then** the client reconnects automatically with exponential backoff, **And** connection health is reportable via a health check method.

4. **Given** the schema module, **When** integration tests run against a real ScyllaDB instance (testcontainers), **Then** all tests pass confirming table creation, CDC enablement, and connection lifecycle.

## Tasks / Subtasks

- [ ] Task 1: Add Go dependencies (AC: #1, #2, #4)
  - [ ] 1.1 Add `github.com/gocql/gocql` (ScyllaDB/Cassandra Go driver)
  - [ ] 1.2 Add `github.com/testcontainers/testcontainers-go` (integration test infrastructure)
  - [ ] 1.3 Run `go mod tidy` to resolve dependency graph

- [ ] Task 2: ScyllaDB client connection management (AC: #1, #3)
  - [ ] 2.1 Create `pkg/storage/scylladb/client.go` with `ClientConfig` struct (contact points, port, keyspace, TLS cert/key/CA paths)
  - [ ] 2.2 Implement `NewClient(cfg ClientConfig) (*Client, error)` — builds gocql ClusterConfig with mTLS via `crypto/tls`, configures retry policy and reconnection
  - [ ] 2.3 Implement `Client.Session() *gocql.Session` accessor
  - [ ] 2.4 Implement `Client.HealthCheck(ctx context.Context) error` — executes lightweight CQL query to verify connection liveness
  - [ ] 2.5 Implement `Client.Close()` for graceful session shutdown
  - [ ] 2.6 Support non-TLS mode for local development and testcontainers (TLS fields optional)

- [ ] Task 3: Schema initializer (AC: #2)
  - [ ] 3.1 Create `pkg/storage/scylladb/schema.go` with `SchemaConfig` struct (keyspace name, replication strategy, replication factor per DC)
  - [ ] 3.2 Implement `EnsureKeyspace(session *gocql.Session, cfg SchemaConfig) error` — idempotent keyspace creation with configurable replication strategy (SimpleStrategy for test, NetworkTopologyStrategy for production)
  - [ ] 3.3 Implement `EnsureTable(session *gocql.Session, keyspace string) error` — idempotent `kv_store` table creation with CDC enabled
  - [ ] 3.4 Implement `EnsureSchema(session *gocql.Session, cfg SchemaConfig) error` — orchestrates keyspace + table creation

- [ ] Task 4: Integration tests with testcontainers (AC: #4)
  - [ ] 4.1 Create `test/integration/storage/suite_test.go` — testcontainers ScyllaDB lifecycle (start container, wait for CQL port, create client, tear down)
  - [ ] 4.2 Create `test/integration/storage/client_test.go` — test connection establishment, session access, health check pass, close and reconnect
  - [ ] 4.3 Create `test/integration/storage/schema_test.go` — test keyspace creation, kv_store table creation, CDC enabled on table, idempotent re-runs
  - [ ] 4.4 Verify table structure matches expected schema (query system_schema.columns)
  - [ ] 4.5 Verify CDC is enabled by querying table properties

- [ ] Task 5: Makefile integration (AC: #4)
  - [ ] 5.1 Ensure `make integration` target runs `go test ./test/integration/...` with appropriate build tags
  - [ ] 5.2 Add `integration` build tag to integration test files to avoid running in `make test`

- [ ] Task 6: Final validation
  - [ ] 6.1 `make build` passes
  - [ ] 6.2 `make test` passes (unit tests only, no integration tests without build tag)
  - [ ] 6.3 `make lint` passes
  - [ ] 6.4 `make integration` passes with Docker available (testcontainers requires Docker)

## Dev Notes

### ScyllaDB Client — Connection Architecture

The client wraps `gocql.ClusterConfig` and `gocql.Session`. gocql natively handles:
- **Connection pooling:** Maintains connections to all ScyllaDB nodes in the cluster
- **Topology awareness:** Discovers nodes via gossip, uses token-aware routing
- **Reconnection:** Automatic reconnection with configurable retry policy — gocql's `ExponentialBackoffRetryPolicy` provides the exponential backoff required by AC #3
- **Host filtering:** Can restrict to a specific DC via `HostFilter: gocql.DCAwareRoundRobinPolicy(dcName)`

**Do NOT implement custom reconnection logic** — gocql handles it. Configure the retry policy on `ClusterConfig`:

```go
cluster := gocql.NewCluster(contactPoints...)
cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
    NumRetries: 10,
    Min:        100 * time.Millisecond,
    Max:        10 * time.Second,
}
cluster.ReconnectInterval = 1 * time.Second
```

### mTLS Configuration

The mTLS setup uses `crypto/tls` with gocql's `SslOpts`:

```go
cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)

caCert, err := os.ReadFile(cfg.CAPath)
caCertPool := x509.NewCertPool()
caCertPool.AppendCertsFromPEM(caCert)

cluster.SslOpts = &gocql.SslOptions{
    Config: &tls.Config{
        Certificates: []tls.Certificate{cert},
        RootCAs:      caCertPool,
        MinVersion:   tls.VersionTLS12,
    },
    EnableHostVerification: true,
}
```

When `CertPath`, `KeyPath`, and `CAPath` are all empty, TLS is disabled — this allows testcontainers and local development without certificate infrastructure. This is NOT a security bypass; production deployments always use cert-manager-issued certificates.

### ClientConfig Struct

```go
type ClientConfig struct {
    // ContactPoints is a list of ScyllaDB node addresses.
    ContactPoints []string
    // Port is the CQL native transport port (default: 9042).
    Port int
    // Keyspace is the ScyllaDB keyspace to use.
    Keyspace string
    // Datacenter is the local DC name for DC-aware routing.
    Datacenter string
    // CertPath is the path to the client TLS certificate (PEM).
    CertPath string
    // KeyPath is the path to the client TLS private key (PEM).
    KeyPath string
    // CAPath is the path to the CA certificate (PEM).
    CAPath string
    // ConnectTimeout is the initial connection timeout.
    ConnectTimeout time.Duration
    // Consistency is the default consistency level.
    Consistency gocql.Consistency
}
```

Default consistency MUST be `gocql.LocalOne` per architecture decision (LOCAL_ONE reads/writes). `Datacenter` is used for `DCAwareRoundRobinPolicy` to ensure queries route to the local DC.

### Health Check Pattern

The health check executes a lightweight CQL query:

```go
func (c *Client) HealthCheck(ctx context.Context) error {
    return c.session.Query("SELECT now() FROM system.local").
        WithContext(ctx).
        Exec()
}
```

This confirms both connectivity and CQL responsiveness. The consumer (API server liveness/readiness probes) calls this periodically.

### Generic KV Table Schema

The `kv_store` table mirrors etcd's key-value model. All Kubernetes resources (DRPlan, DRExecution, DRGroupStatus) are stored as serialized blobs in the same table — no per-resource tables, no CQL migrations when API fields change.

```sql
CREATE KEYSPACE IF NOT EXISTS soteria
WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'dc1': 2,
    'dc2': 2
};

CREATE TABLE IF NOT EXISTS soteria.kv_store (
    api_group text,
    resource_type text,
    namespace text,
    name text,
    value blob,
    resource_version timeuuid,
    PRIMARY KEY ((api_group, resource_type), namespace, name)
) WITH cdc = {'enabled': true};
```

**Primary Key Design:**
- **Partition key:** `(api_group, resource_type)` — enables `SELECT * FROM kv_store WHERE api_group = ? AND resource_type = ?` for list operations (GetList in Story 1.3)
- **Clustering columns:** `(namespace, name)` — unique identification within a partition, enables range queries by namespace

This allows efficient:
- Point reads: full key `(api_group, resource_type, namespace, name)` → single row
- Namespace-scoped lists: `WHERE api_group = ? AND resource_type = ? AND namespace = ?`
- Cluster-wide lists: `WHERE api_group = ? AND resource_type = ?`

**CDC Enablement:**
The `WITH cdc = {'enabled': true}` clause enables ScyllaDB's Change Data Capture. This creates a companion CDC log table (`kv_store_scylla_cdc_log`) that records all mutations. Story 1.4 consumes this stream via `scylla-cdc-go`. CDC must be enabled at table creation time — enabling it later causes a full table rebuild.

### SchemaConfig Struct

```go
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
}
```

### Testcontainers Setup

Use `scylladb/scylla` Docker image (latest stable). ScyllaDB takes 15-30 seconds to become CQL-ready.

```go
container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
    ContainerRequest: testcontainers.ContainerRequest{
        Image:        "scylladb/scylla:latest",
        ExposedPorts: []string{"9042/tcp"},
        WaitingFor:   wait.ForLog("Starting listening for CQL clients").WithStartupTimeout(60 * time.Second),
        Cmd:          []string{"--smp", "1", "--memory", "256M", "--overprovisioned", "1"},
    },
    Started: true,
})
```

- `--smp 1` limits ScyllaDB to 1 CPU core (sufficient for tests, reduces resource usage)
- `--memory 256M` limits memory allocation
- `--overprovisioned 1` acknowledges running in a shared environment

**Test keyspace uses SimpleStrategy with RF=1** — testcontainers runs a single-node cluster, so NetworkTopologyStrategy is not appropriate. The `SchemaConfig` abstraction allows the same code to work for both test and production topologies.

### Integration Test Build Tag

All integration test files MUST include the build tag:

```go
//go:build integration

package storage_test
```

This prevents integration tests from running during `make test` (which runs `go test ./...` without the integration tag). `make integration` runs with `-tags integration`.

### Test Assertions for CDC Enablement

To verify CDC is enabled, query the ScyllaDB system schema:

```go
var extensions map[string]string
err := session.Query(
    `SELECT extensions FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?`,
    keyspace, "kv_store",
).Scan(&extensions)
// extensions should contain cdc-related entries
```

Alternatively, verify the CDC log table exists:

```go
var count int
err := session.Query(
    `SELECT count(*) FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?`,
    keyspace, "kv_store_scylla_cdc_log",
).Scan(&count)
// count should be 1
```

### Dependency Versions

After Story 1.1's `kubebuilder init`, the go.mod contains k8s dependencies. Add:

- `github.com/gocql/gocql` — latest stable (ScyllaDB-compatible Cassandra driver for Go)
- `github.com/testcontainers/testcontainers-go` — latest stable (integration test infrastructure)

Do NOT add `github.com/scylladb/scylla-cdc-go` yet — that's Story 1.4 (Watch via CDC). This story only enables CDC on the table; CDC consumption is Story 1.4's responsibility.

### File Organization

After this story, the `pkg/storage/scylladb/` directory contains:

```
pkg/storage/scylladb/
├── doc.go           # from Story 1.1 (stub)
├── client.go        # NEW — connection management, mTLS, health check
└── schema.go        # NEW — keyspace and kv_store table DDL
```

And integration tests:

```
test/integration/storage/
├── suite_test.go    # NEW — testcontainers lifecycle, shared ScyllaDB instance
├── client_test.go   # NEW — connection, health check, session tests
└── schema_test.go   # NEW — keyspace creation, table creation, CDC verification
```

### Architecture Compliance

- **Storage boundary:** Only `pkg/storage/scylladb/` touches ScyllaDB — enforced
- **Table naming:** snake_case — `kv_store`, `api_group`, `resource_type`
- **Consistency:** `LOCAL_ONE` as default — per architecture decision
- **Auth:** mTLS only — no password-based authentication in production
- **Topology:** Schema supports NetworkTopologyStrategy for production, SimpleStrategy for test
- **CDC:** Enabled at table creation — required for Story 1.4 (Watch via CDC)
- **Logging:** Structured logging via `log.FromContext(ctx)` or `slog` — no `fmt.Println`
- **Error wrapping:** lowercase, no punctuation, wrap with `%w`

### Critical Warnings

1. **gocql is the ScyllaDB-compatible Go driver** — do not use `datastax/gocql` or other forks. The `github.com/gocql/gocql` package is the canonical, ScyllaDB-tested driver. The ScyllaDB Go driver (`github.com/scylladb/gocql`) is a fork with shard-aware optimizations but `gocql/gocql` is sufficient and more broadly used.

2. **CDC must be enabled at table creation** — adding CDC to an existing table triggers a full table rebuild. Always create the table with `WITH cdc = {'enabled': true}`.

3. **Do NOT hardcode keyspace or replication** — all values come from `SchemaConfig`. Tests use SimpleStrategy RF=1, production uses NetworkTopologyStrategy DC1:2 DC2:2.

4. **Testcontainers requires Docker** — integration tests will be skipped or fail in environments without Docker. The `make integration` target should document this prerequisite.

5. **Do NOT implement CDC stream consumption in this story** — CDC is enabled on the table, but the CDC log reading, stream tracking, and watch event generation belong to Story 1.4.

6. **Connection timeout for ScyllaDB container** — ScyllaDB takes 15-30 seconds to start. Use a generous `WaitingFor` timeout (60 seconds) in testcontainers to avoid flaky tests.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 1.2 (lines 350-378)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Data Architecture (lines 178-189)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Authentication & Security (lines 191-198)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Project Structure — pkg/storage/scylladb/ (lines 412-419)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Naming Patterns — ScyllaDB (lines 269-274)]
- [Source: _bmad-output/project-context.md — ScyllaDB Storage Layer rules (lines 80-89)]
- [Source: _bmad-output/project-context.md — Testing Rules — ScyllaDB storage tests (line 116)]
- [External: gocql — https://pkg.go.dev/github.com/gocql/gocql]
- [External: ScyllaDB CDC — https://docs.scylladb.com/stable/using-scylla/cdc.html]
- [External: testcontainers-go — https://golang.testcontainers.org/]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
