# Story 17.4: ScyllaDB Architecture Overview

Status: done

## Story

As a platform engineer evaluating Soteria's architecture,
I want to understand why ScyllaDB was chosen and how the cross-DC topology works,
so that I can assess the operational implications and plan my deployment accordingly.

## Acceptance Criteria

**AC1: Cross-DC topology is explained**
Given the ScyllaDB overview page
When a reader reviews the topology section
Then they understand the cross-DC ScyllaDB deployment model with datacenter-aware replication

**AC2: Design rationale is documented**
Given the ScyllaDB overview page
When a reader asks "why ScyllaDB and not etcd?"
Then the page explains the shared-state architecture choice, availability characteristics, and why etcd was not suitable

**AC3: Replication strategy is accurate**
Given the NetworkTopologyStrategy and LOCAL_ONE consistency model described
When compared against the actual ScyllaDB schema and connection configuration in code
Then the documented strategy matches the implementation

**AC4: Connection configuration is documented**
Given the ScyllaDB overview page
When a reader wants to understand how Soteria connects to ScyllaDB
Then they find the connection model, including relevant flags and configuration from the API server options

## Tasks / Subtasks

- [x] Task 1: Research ScyllaDB architecture in code (AC: 1, 3, 4)
  - [x] 1.1: Read architecture doc data architecture section for conceptual framing
  - [x] 1.2: Walk `pkg/storage/scylladb/` implementation to understand actual schema, connection handling, and replication configuration
  - [x] 1.3: Examine connection flags in `pkg/apiserver/options.go`
- [x] Task 2: Write ScyllaDB overview page (AC: 1, 2, 3, 4)
  - [x] 2.1: Write `docs/installation/scylladb/overview.md` covering: cross-DC topology, design rationale (why ScyllaDB), shared-state architecture, NetworkTopologyStrategy, LOCAL_ONE consistency model, connection configuration
  - [x] 2.2: Add topology diagram (Mermaid or image)
  - [x] 2.3: Verify all technical claims against actual code behavior

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/architecture.md — data architecture section: ScyllaDB rationale, shared-state model, cross-site replication]
- [Source: _bmad-output/planning-artifacts/prd.md — FR26-FR30 cross-site shared state requirements]

### Code to Verify Against

- [Source: pkg/storage/scylladb/schema.go — SchemaConfig struct with Strategy (SimpleStrategy vs NetworkTopologyStrategy), DCReplication map, DisableTablets, EnsureKeyspace function]
- [Source: pkg/storage/scylladb/client.go — ClientConfig struct: ContactPoints, Port, Keyspace, Datacenter (local DC for DC-aware routing), TLS cert/key/CA paths, Consistency level (gocql.Consistency)]
- [Source: pkg/storage/scylladb/store.go — storage.Interface implementation backed by ScyllaDB]
- [Source: pkg/storage/scylladb/watch.go — watch implementation using ScyllaDB CDC]
- [Source: pkg/apiserver/options.go — connection flags: `--scylladb-contact-points` (default localhost:9042), `--scylladb-keyspace` (default soteria), `--scylladb-local-dc`, `--scylladb-dc-replication` (comma-separated dc:rf pairs for auto-create with NetworkTopologyStrategy)]
- [Source: pkg/storage/scylladb/versioner.go — resource versioning backed by ScyllaDB]

### Implementation Pattern

- Cross-DC topology: each cluster runs its own ScyllaDB DC; inter-DC replication uses NetworkTopologyStrategy
- Connection: `gocql.ClusterConfig` with DC-aware token-aware policy, TLS via cert-manager certificates
- Schema: keyspace auto-created with `--scylladb-dc-replication` flag (e.g., `etl6:2,etl7:2`)
- Consistency: LOCAL_ONE for reads/writes (each site reads from its local DC)
- Etcd is disabled: `o.RecommendedOptions.Etcd = nil` — ScyllaDB replaces etcd entirely
- Tablets disabled for CDC compatibility: `DisableTablets: true`
- Use Mermaid diagram showing two DCs with ScyllaDB nodes and async replication arrows

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/installation/scylladb/overview.md` | NEW | ScyllaDB architecture overview: topology, rationale, replication, connection config |

### Key Constraints

- ScyllaDB replaces etcd entirely (not a sidecar — it IS the storage backend for the aggregated API server)
- NetworkTopologyStrategy with per-DC replication factors is the production configuration
- LOCAL_ONE consistency means each site reads/writes locally — eventual consistency across DCs
- Tablets must be disabled for CDC (change data capture) to work
- TLS is mandatory in production (cert-manager mTLS)
- Depends on: 17.1

### Project Structure Notes

- ScyllaDB storage implementation: `pkg/storage/scylladb/` (14 files: client, schema, store, watch, versioner, selector, labelsync, keyutil, doc + tests)
- Connection wiring: `pkg/apiserver/options.go` → `cmd/soteria/main.go` → `pkg/storage/scylladb/client.go`

### References

- [Source: pkg/storage/scylladb/schema.go — lines 27-42, SchemaConfig struct]
- [Source: pkg/storage/scylladb/client.go — lines 30-50, ClientConfig struct]
- [Source: pkg/apiserver/options.go — lines 39-49, SoteriaServerOptions ScyllaDB fields]
- [Source: pkg/apiserver/options.go — lines 69-86, CLI flag definitions]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor)

### Debug Log References

- No debug issues encountered — documentation-only story

### Completion Notes List

- ✅ Task 1: Read architecture doc (ScyllaDB rationale, shared-state model, cross-site replication, FR26-FR30), walked all source files in `pkg/storage/scylladb/` (schema.go, client.go, store.go, watch.go, versioner.go) and `pkg/apiserver/options.go` to understand actual implementation
- ✅ Task 2: Wrote comprehensive `docs/installation/scylladb/overview.md` covering:
  - Why ScyllaDB instead of etcd (3-point rationale: cross-DC replication, DC failure availability, automatic reconciliation)
  - Cross-DC topology with Mermaid diagram showing two DCs, 4 ScyllaDB nodes, LOCAL_ONE connections, async replication
  - NetworkTopologyStrategy with per-DC replication factors, TABLETS disabled for CDC
  - LOCAL_ONE consistency model with explanation of LWT for critical state transitions
  - Complete CLI flag reference table (8 flags with defaults and descriptions)
  - DC-aware routing via DCAwareRoundRobinPolicy
  - TLS/mTLS configuration requirements
  - Connection resilience parameters (connect timeout, exponential backoff retry, reconnect interval)
  - Generic KV schema overview (kv_store + kv_store_labels tables)
  - etcd disabled explanation with code reference
- ✅ All technical claims verified against actual code: consistency levels, flag defaults, retry policy params, schema structure, CDC enablement, tablets disabled, topology validation
- ✅ mkdocs build passes with `--strict` flag

### Change Log

- 2026-09-05: Implemented story 17.4 — wrote ScyllaDB architecture overview documentation page

### File List

| File | Action |
|------|--------|
| `docs/installation/scylladb/overview.md` | MODIFIED (replaced placeholder with full content) |
| `_bmad-output/implementation-artifacts/17-4-scylladb-overview.md` | MODIFIED (task checkboxes, status, dev agent record) |
