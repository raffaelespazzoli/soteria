# Story 1.6: Cross-Site State Replication & Resilience

Status: ready-for-dev

## Story

As a platform engineer,
I want both clusters to see identical DR state that survives a single-datacenter failure and auto-reconciles on recovery,
so that DR operations are available from either cluster at all times.

## Acceptance Criteria

1. **Given** ScyllaDB deployed on two OpenShift clusters with `config/scylladb/` reference manifests, **When** the ScyllaCluster CR is configured with NetworkTopologyStrategy, **Then** replication factor is DC1:2, DC2:2 (RF=2 per DC, 4 nodes total), **And** the scylla-operator manages the ScyllaDB lifecycle.

2. **Given** the two-DC ScyllaDB cluster, **When** a DRPlan is created via kubectl on Cluster 1, **Then** `kubectl get drplan` on Cluster 2 returns the same resource within the async replication window, **And** both clusters serve the resource via LOCAL_ONE consistency with no cross-DC latency on reads or writes (FR27).

3. **Given** a healthy two-DC deployment, **When** DC1 becomes completely unreachable (simulated network partition), **Then** DC2's Aggregated API Server continues to serve all DR resources normally (FR28, NFR4), **And** new DRExecution records can be created on DC2 with no dependency on DC1 connectivity (FR28, NFR5), **And** no errors or degraded behavior occurs on the surviving cluster.

4. **Given** DC1 has been down and DC2 has received new writes, **When** DC1 recovers and ScyllaDB nodes rejoin the cluster, **Then** ScyllaDB's anti-entropy repair automatically synchronizes state (FR29), **And** after reconciliation, `kubectl get drplans` on both clusters returns identical results, **And** no manual intervention is required for state reconciliation.

5. **Given** concurrent writes to the same resource from both DCs, **When** a conflict occurs on a non-critical field, **Then** last-write-wins resolution applies (FR30). **Given** a critical state transition (e.g., DRPlan phase change), **When** concurrent writes to the same state field occur, **Then** lightweight transactions (LWT/CAS) prevent conflicting state transitions (FR30).

6. **Given** cert-manager deployed on both clusters, **When** TLS certificates are configured in `config/certmanager/`, **Then** all ScyllaDB internode replication traffic is encrypted via TLS (NFR12), **And** all API server to ScyllaDB communication is encrypted via mTLS (NFR13), **And** certificates share a common CA across both DCs.

## Tasks / Subtasks

- [ ] Task 1: Create ScyllaDB reference deployment manifests (AC: #1)
  - [ ] 1.1 Create `config/scylladb/scyllacluster.yaml` — ScyllaCluster CR for two-DC deployment
  - [ ] 1.2 Configure `NetworkTopologyStrategy` with RF=2 per DC (DC1:2, DC2:2, 4 nodes total)
  - [ ] 1.3 Configure CDC enablement on the `kv_store` table via ScyllaCluster `extensions` or table properties
  - [ ] 1.4 Configure node affinity/topology constraints for DC placement via `datacenter` specs in ScyllaCluster
  - [ ] 1.5 Create `config/scylladb/kustomization.yaml` referencing the ScyllaCluster CR

- [ ] Task 2: Create cert-manager TLS certificate manifests (AC: #6)
  - [ ] 2.1 Create `config/certmanager/ca-issuer.yaml` — self-signed root CA Issuer shared across both DCs
  - [ ] 2.2 Create `config/certmanager/ca-certificate.yaml` — root CA Certificate resource
  - [ ] 2.3 Create `config/certmanager/scylladb-serving-cert.yaml` — Certificate for ScyllaDB internode and client-facing TLS
  - [ ] 2.4 Create `config/certmanager/scylladb-client-cert.yaml` — client Certificate for API server → ScyllaDB mTLS authentication
  - [ ] 2.5 Create `config/certmanager/kustomization.yaml` referencing all cert-manager resources
  - [ ] 2.6 Document the CA sharing strategy: both DCs use the same root CA so cross-DC internode TLS is mutually trusted

- [ ] Task 3: Update ScyllaDB client for multi-DC awareness (AC: #2)
  - [ ] 3.1 Update `pkg/storage/scylladb/client.go` — add `LocalDC` field to `ClientConfig`
  - [ ] 3.2 Configure `gocql.ClusterConfig` with `DCAwareRoundRobinPolicy` preferring the local DC
  - [ ] 3.3 Add `--scylladb-local-dc` flag to `pkg/apiserver/options.go`
  - [ ] 3.4 Verify `LOCAL_ONE` consistency is set on the session for all standard reads and writes
  - [ ] 3.5 Unit test: `TestClientConfig_DCAwarePolicy_LocalDCPreferred`

- [ ] Task 4: Implement LWT for critical state transitions (AC: #5)
  - [ ] 4.1 Add `CompareAndSetPhase()` method to `pkg/storage/scylladb/store.go` — CQL `UPDATE ... IF phase = ?` for DRPlan phase transitions
  - [ ] 4.2 Define which fields are "critical" and require LWT: DRPlan `.status.phase`, DRExecution `.status.result`
  - [ ] 4.3 Return `errors.NewConflict` when the CAS condition fails (applied=false)
  - [ ] 4.4 Integrate LWT into `GuaranteedUpdate()` path — when the update touches a critical field, use `IF` clause; otherwise standard LOCAL_ONE write
  - [ ] 4.5 Add a `CriticalFieldDetector` interface or helper that inspects old vs new object to determine if LWT is needed
  - [ ] 4.6 Unit tests for LWT: `TestGuaranteedUpdate_CriticalField_UsesLWT`, `TestGuaranteedUpdate_NonCriticalField_UsesStandardWrite`, `TestGuaranteedUpdate_CASConflict_ReturnsConflictError`

- [ ] Task 5: Integration tests — cross-site replication (AC: #2, #4)
  - [ ] 5.1 Create `test/integration/replication/suite_test.go` — setup multi-DC ScyllaDB via testcontainers with two logical datacenters
  - [ ] 5.2 Create `test/integration/replication/replication_test.go` with `//go:build integration` tag
  - [ ] 5.3 `TestReplication_ResourceCreatedOnDC1_VisibleOnDC2` — create a DRPlan via DC1 session, read via DC2 session, verify identical
  - [ ] 5.4 `TestReplication_ResourceCreatedOnDC2_VisibleOnDC1` — reverse direction
  - [ ] 5.5 `TestReplication_MultipleResources_AllReplicateWithinWindow` — create 10 resources on DC1, verify all appear on DC2 within a bounded time window

- [ ] Task 6: Integration tests — DC failure resilience (AC: #3)
  - [ ] 6.1 `TestResilience_DC1Down_DC2ContinuesReads` — stop DC1 container(s), verify DC2 session can still list all resources
  - [ ] 6.2 `TestResilience_DC1Down_DC2ContinuesWrites` — stop DC1 container(s), create new DRExecution on DC2, verify success
  - [ ] 6.3 `TestResilience_DC1Down_DC2NoErrors` — verify no gocql errors or timeouts on DC2 during DC1 outage
  - [ ] 6.4 `TestResilience_DC2Down_DC1ContinuesOperations` — reverse: stop DC2, verify DC1 operates normally

- [ ] Task 7: Integration tests — recovery & reconciliation (AC: #4)
  - [ ] 7.1 `TestRecovery_DC1Recovers_StateReconciles` — stop DC1, write resources on DC2, restart DC1, run `nodetool repair`, verify DC1 sees all resources
  - [ ] 7.2 `TestRecovery_BothDCsIdenticalAfterReconciliation` — after repair, verify list from both DCs returns the same set and same resourceVersions
  - [ ] 7.3 `TestRecovery_NoManualIntervention` — verify the reconciliation requires only ScyllaDB's built-in repair (no custom code)

- [ ] Task 8: Integration tests — conflict resolution (AC: #5)
  - [ ] 8.1 `TestConflict_ConcurrentNonCriticalWrite_LastWriteWins` — write to the same resource from DC1 and DC2 simultaneously on a non-critical field, verify one value wins
  - [ ] 8.2 `TestConflict_ConcurrentPhaseTransition_LWTPreventsDuplicate` — attempt the same phase transition from DC1 and DC2, verify exactly one succeeds and the other gets a conflict error
  - [ ] 8.3 `TestConflict_LWT_StalePhaseRejected` — attempt a phase transition with stale phase value, verify CAS failure
  - [ ] 8.4 Table-driven tests covering LWT conflict scenarios: `(currentPhase, attemptedPhase, expectedApplied)`

- [ ] Task 9: Final validation
  - [ ] 9.1 `make build` passes
  - [ ] 9.2 `make test` passes (unit tests)
  - [ ] 9.3 `make lint` passes
  - [ ] 9.4 `make integration` passes (testcontainers with multi-DC ScyllaDB)

## Dev Notes

### Architecture Overview

Story 1.6 validates the cross-site resilience properties that make the entire Soteria architecture viable. Stories 1.2–1.5 built the storage layer and API server against a single ScyllaDB instance. This story proves the same code works correctly when ScyllaDB is deployed across two datacenters with asynchronous replication — the fundamental architectural bet of the project.

The story has three distinct dimensions:
1. **Configuration artifacts** — ScyllaDB and cert-manager manifests that define the reference two-DC deployment
2. **Code changes** — DC-aware client configuration and LWT support for critical state transitions
3. **Validation tests** — integration tests proving replication, failure resilience, recovery, and conflict resolution

```
DC1 (OpenShift Cluster 1)              DC2 (OpenShift Cluster 2)
┌─────────────────────────┐            ┌─────────────────────────┐
│  Soteria API Server     │            │  Soteria API Server     │
│  (LOCAL_ONE reads/writes)│           │  (LOCAL_ONE reads/writes)│
│         │               │            │         │               │
│         ▼               │            │         ▼               │
│  ScyllaDB Node 1        │◄──────────►│  ScyllaDB Node 3        │
│  ScyllaDB Node 2        │  async     │  ScyllaDB Node 4        │
│  (RF=2)                 │  repl.     │  (RF=2)                 │
└─────────────────────────┘            └─────────────────────────┘
```

Each API server talks only to its local ScyllaDB nodes via `LOCAL_ONE` consistency. Cross-DC replication is handled entirely by ScyllaDB — the application is unaware of it. When DC1 goes down, DC2 operates independently. When DC1 recovers, ScyllaDB's anti-entropy repair synchronizes state automatically.

### ScyllaDB ScyllaCluster CR — Reference Deployment

The scylla-operator manages the ScyllaDB lifecycle on OpenShift. The `ScyllaCluster` CR defines the multi-DC topology:

```yaml
# config/scylladb/scyllacluster.yaml
apiVersion: scylla.scylladb.com/v1
kind: ScyllaCluster
metadata:
  name: soteria-scylladb
  namespace: soteria-system
spec:
  version: "6.2"
  agentVersion: "3.3.3"
  developerMode: false
  sysctls:
    - "fs.aio-max-nr=2097152"

  datacenter:
    name: dc1
    racks:
      - name: rack1
        members: 2
        storage:
          capacity: 100Gi
          storageClassName: gp3-csi
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
          limits:
            cpu: "4"
            memory: "8Gi"
        placement:
          nodeAffinity:
            requiredDuringSchedulingIgnoredDuringExecution:
              nodeSelectorTerms:
                - matchExpressions:
                    - key: topology.kubernetes.io/zone
                      operator: In
                      values:
                        - dc1-zone1

  # Note: ScyllaCluster CR is per-cluster (one per OpenShift cluster).
  # The second DC is a separate ScyllaCluster CR on the second OpenShift cluster
  # with datacenter.name: dc2. ScyllaDB multi-DC is formed by connecting
  # the clusters via seeds/externalSeeds.
```

**Critical design point:** The scylla-operator deploys a `ScyllaCluster` per Kubernetes cluster. Multi-DC is formed by configuring `externalSeeds` on each cluster to point to the other cluster's ScyllaDB nodes. The operator handles this via the `ScyllaCluster` CR's `externalSeeds` field or manual seed configuration. Both ScyllaDB clusters must use the same `clusterName` for multi-DC to work.

The keyspace must be created with `NetworkTopologyStrategy`:

```cql
CREATE KEYSPACE soteria WITH replication = {
  'class': 'NetworkTopologyStrategy',
  'dc1': 2,
  'dc2': 2
};
```

This needs to be handled in `pkg/storage/scylladb/schema.go`'s `EnsureSchema()` function (from Story 1.2). If the keyspace already exists with `SimpleStrategy` (from single-node dev), the function should check and log a warning but not alter it — topology changes require operator intervention.

**Update to `EnsureSchema()`:**

```go
type SchemaConfig struct {
    Keyspace           string
    ReplicationClass   string            // "SimpleStrategy" or "NetworkTopologyStrategy"
    ReplicationOptions map[string]int    // e.g. {"dc1": 2, "dc2": 2} or {"replication_factor": 1}
}
```

Story 1.2 created `EnsureSchema()` with `SimpleStrategy` for dev/test. This story adds the ability to configure `NetworkTopologyStrategy` via the `SchemaConfig`. In production, the deployment manifests set `ReplicationClass: NetworkTopologyStrategy` with DC-specific RF values. In integration tests, `SimpleStrategy` with RF=1 per DC can be used if the testcontainers ScyllaDB doesn't support true multi-DC.

### DC-Aware Client Configuration

The `gocql` driver must be configured with DC awareness so that reads and writes go to the local DC:

```go
// pkg/storage/scylladb/client.go — additions to existing ClientConfig

type ClientConfig struct {
    ContactPoints string
    Keyspace      string
    TLSCert       string
    TLSKey        string
    TLSCA         string
    LocalDC       string // NEW: e.g. "dc1" or "dc2"
}

func NewClient(cfg ClientConfig) (*Client, error) {
    cluster := gocql.NewCluster(strings.Split(cfg.ContactPoints, ",")...)
    cluster.Keyspace = cfg.Keyspace
    cluster.Consistency = gocql.LocalOne

    // DC-aware routing policy
    if cfg.LocalDC != "" {
        cluster.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy(cfg.LocalDC)
    }

    // mTLS configuration (from Story 1.2)
    if cfg.TLSCert != "" {
        tlsConfig, err := buildTLSConfig(cfg.TLSCert, cfg.TLSKey, cfg.TLSCA)
        if err != nil {
            return nil, fmt.Errorf("building tls config: %w", err)
        }
        cluster.SslOpts = &gocql.SslOptions{Config: tlsConfig}
    }

    session, err := cluster.CreateSession()
    if err != nil {
        return nil, fmt.Errorf("creating scylladb session: %w", err)
    }

    return &Client{session: session}, nil
}
```

The `--scylladb-local-dc` flag in `pkg/apiserver/options.go` tells the API server which DC it's in. This must be set correctly on each cluster:

- Cluster 1: `--scylladb-local-dc=dc1`
- Cluster 2: `--scylladb-local-dc=dc2`

With `DCAwareRoundRobinPolicy`, the driver sends all reads and writes to the local DC. Combined with `LOCAL_ONE` consistency, this means zero cross-DC latency for normal operations.

### Lightweight Transactions (LWT) for Critical State Transitions

Standard writes use `LOCAL_ONE` consistency — fast, local, no cross-DC coordination. But certain state transitions require stronger guarantees to prevent conflicting concurrent writes from both DCs. ScyllaDB's lightweight transactions (LWT) provide compare-and-set (CAS) semantics.

**Which fields are critical (require LWT):**
- `DRPlan.status.phase` — state machine transitions must not conflict (e.g., two DCs both trying to transition from `SteadyState` to `FailingOver`)
- `DRExecution.status.result` — terminal result must be set exactly once

**Which fields are non-critical (standard LWW):**
- Metadata updates (labels, annotations)
- Non-state-machine status fields (conditions, wave status details)
- DRGroupStatus updates (owned by a single controller leader)

**Implementation approach:**

The LWT integration hooks into `GuaranteedUpdate()` from Story 1.3. When the update function modifies a critical field, the CQL write includes an `IF` clause:

```go
// pkg/storage/scylladb/store.go — extension to GuaranteedUpdate

func (s *Store) GuaranteedUpdate(
    ctx context.Context,
    key string,
    destination runtime.Object,
    ignoreNotFound bool,
    preconditions *storage.Preconditions,
    tryUpdate storage.UpdateFunc,
    cachedExistingObject runtime.Object,
) error {
    // ... existing logic from Story 1.3: read current, apply tryUpdate ...

    if s.requiresLWT(existing, updated) {
        return s.casUpdate(ctx, key, existing, updated)
    }
    return s.standardUpdate(ctx, key, updated)
}

func (s *Store) requiresLWT(old, new runtime.Object) bool {
    // Check if critical fields changed between old and new
    // This uses type assertions to check specific fields
    if oldPlan, ok := old.(*soteriav1alpha1.DRPlan); ok {
        newPlan := new.(*soteriav1alpha1.DRPlan)
        if oldPlan.Status.Phase != newPlan.Status.Phase {
            return true
        }
    }
    if oldExec, ok := old.(*soteriav1alpha1.DRExecution); ok {
        newExec := new.(*soteriav1alpha1.DRExecution)
        if oldExec.Status.Result != newExec.Status.Result {
            return true
        }
    }
    return false
}

func (s *Store) casUpdate(ctx context.Context, key string, old, new runtime.Object) error {
    // Serialize the new object
    data, err := s.codec.Encode(new)
    if err != nil {
        return fmt.Errorf("encoding object for cas update: %w", err)
    }

    // CQL with IF condition on resource_version (optimistic lock via CAS)
    // The IF clause makes this a lightweight transaction
    applied, err := s.session.Query(
        `UPDATE kv_store SET value = ?, resource_version = ? WHERE api_group = ? AND resource_type = ? AND namespace = ? AND name = ? IF resource_version = ?`,
        data, newVersion, apiGroup, resourceType, namespace, name, oldVersion,
    ).WithContext(ctx).ScanCAS(/* result columns if not applied */)

    if err != nil {
        return fmt.Errorf("cas update for key %s: %w", key, err)
    }
    if !applied {
        return storage.NewKeyExistsError(key, 0)
    }
    return nil
}
```

**Important:** LWT in ScyllaDB uses Paxos under the hood. In a two-DC deployment, LWT requires a quorum across all DCs — this means LWT will fail if one DC is down. This is acceptable because:
- During a DC failure, the surviving DC is the only one making state transitions — no conflict possible
- LWT is only needed when both DCs are up and could conflict
- If LWT fails due to DC loss, the write should fall back to standard LOCAL_ONE (the caller knows only one DC is active)

**Fallback strategy:**

```go
func (s *Store) casUpdate(ctx context.Context, key string, old, new runtime.Object) error {
    // ... attempt CAS ...
    if err != nil {
        // If CAS fails due to unavailability (DC down), fall back to standard write
        // This is safe because DC failure means no concurrent writer
        if isUnavailableError(err) {
            log.FromContext(ctx).V(1).Info("LWT unavailable, falling back to standard write",
                "key", key, "error", err)
            return s.standardUpdate(ctx, key, new)
        }
        return fmt.Errorf("cas update for key %s: %w", key, err)
    }
    // ...
}
```

This fallback is architecturally sound: LWT prevents conflicting writes when both DCs are active. When one DC is down, there's only one writer, so LOCAL_ONE is sufficient.

### cert-manager TLS Configuration

All ScyllaDB communication must be encrypted:

1. **Internode traffic** — ScyllaDB nodes in DC1 replicate to DC2 over TLS
2. **Client traffic** — API server connects to ScyllaDB via mTLS (mutual TLS)

Both require certificates from a common CA so that cross-DC connections are trusted.

```yaml
# config/certmanager/ca-issuer.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: soteria-ca-issuer
spec:
  selfSigned: {}
---
# config/certmanager/ca-certificate.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: soteria-ca
  namespace: soteria-system
spec:
  isCA: true
  commonName: soteria-ca
  secretName: soteria-ca-secret
  duration: 87600h  # 10 years
  issuerRef:
    name: soteria-ca-issuer
    kind: ClusterIssuer
---
# Issuer that uses the CA certificate to sign all other certs
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-ca
  namespace: soteria-system
spec:
  ca:
    secretName: soteria-ca-secret
```

```yaml
# config/certmanager/scylladb-serving-cert.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: scylladb-serving
  namespace: soteria-system
spec:
  secretName: scylladb-serving-tls
  duration: 8760h  # 1 year
  renewBefore: 720h  # 30 days
  issuerRef:
    name: soteria-ca
    kind: Issuer
  commonName: soteria-scylladb
  dnsNames:
    - "soteria-scylladb-client.soteria-system.svc"
    - "*.soteria-scylladb.soteria-system.svc.cluster.local"
    - "*.soteria-scylladb-client.soteria-system.svc"
  usages:
    - server auth
    - client auth  # Internode communication requires both
```

```yaml
# config/certmanager/scylladb-client-cert.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: soteria-scylladb-client
  namespace: soteria-system
spec:
  secretName: soteria-scylladb-client-tls
  duration: 8760h
  renewBefore: 720h
  issuerRef:
    name: soteria-ca
    kind: Issuer
  commonName: soteria-apiserver
  usages:
    - client auth
```

**Cross-DC CA sharing:** Both DCs must use the same root CA certificate. In practice, the `soteria-ca-secret` containing the CA key pair is created on one cluster and replicated (manually or via a secret sync mechanism) to the other. The cert-manager Issuer on each cluster then issues certs signed by the same CA. This is a manual operations step documented in the deployment guide — the manifests in `config/certmanager/` are per-cluster but depend on the shared CA secret.

**scylla-operator TLS integration:** The scylla-operator supports TLS configuration via the ScyllaCluster CR. The `spec.network` section references the serving cert secret, and the operator configures ScyllaDB's `client_encryption_options` and `server_encryption_options` accordingly.

### Multi-DC Testcontainers Setup

Integration tests use testcontainers to create a multi-DC ScyllaDB cluster. ScyllaDB supports multi-DC in a single cluster by configuring the `GossipingPropertyFileSnitch` and assigning nodes to datacenters.

```go
// test/integration/replication/suite_test.go

func setupMultiDCScyllaDB(t *testing.T) (dc1Session, dc2Session *gocql.Session, cleanup func()) {
    // Start ScyllaDB node 1 (DC1 seed)
    dc1Node1 := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image: "scylladb/scylla:6.2",
            Cmd:   []string{
                "--smp", "1",
                "--memory", "512M",
                "--overprovisioned", "1",
                "--seeds", "<self-ip>",
            },
            Env: map[string]string{
                "SCYLLA_DC": "dc1",
                "SCYLLA_RACK": "rack1",
            },
            ExposedPorts: []string{"9042/tcp"},
        },
    })

    // Start ScyllaDB node 2 (DC2, seeds point to DC1)
    dc2Node1 := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image: "scylladb/scylla:6.2",
            Cmd:   []string{
                "--smp", "1",
                "--memory", "512M",
                "--overprovisioned", "1",
                "--seeds", dc1Node1IP,
            },
            Env: map[string]string{
                "SCYLLA_DC": "dc2",
                "SCYLLA_RACK": "rack1",
            },
            ExposedPorts: []string{"9042/tcp"},
        },
    })

    // Wait for both nodes to be healthy and see each other
    // Create keyspace with NetworkTopologyStrategy
    // Create sessions — one targeting each DC

    dc1Session = createSession(dc1Node1IP, "dc1")
    dc2Session = createSession(dc2Node1IP, "dc2")

    return dc1Session, dc2Session, cleanup
}
```

**Simplification for testcontainers:** A real production deployment has 2 nodes per DC (4 total). In testcontainers, 1 node per DC (2 total) is sufficient to validate replication behavior. The key is that they are assigned to different DCs via `SCYLLA_DC` environment variable and the keyspace uses `NetworkTopologyStrategy` with RF=1 per DC.

**DC failure simulation:** To simulate a DC failure, stop one of the ScyllaDB containers. The surviving container's session should continue operating normally with `LOCAL_ONE` consistency. After restarting the stopped container, run `nodetool repair` to trigger anti-entropy reconciliation.

### LWW Conflict Resolution

ScyllaDB uses cell-level last-write-wins (LWW) by default. In the generic KV store model, each row is a single blob (`value` column), so LWW applies to the entire resource. When two DCs write to the same resource concurrently, the write with the later timestamp wins.

For the generic KV store, this means:
- Two concurrent updates to the same DRPlan → the later timestamp's full blob wins
- This is acceptable for most updates because the Kubernetes API layer enforces resourceVersion-based optimistic concurrency per DC
- True concurrent writes from two DCs (same resource, different fields) cannot be field-merged — the entire blob wins or loses

This is a known tradeoff of the generic KV design (same as etcd — full-object replacement). Field-level merging would require breaking the blob into columns, which contradicts the generic KV architecture. The mitigation is:
1. Controller leader election ensures only one DC actively writes execution state
2. LWT guards state machine transitions (the critical path)
3. Non-critical concurrent writes (metadata) are rare and LWW is acceptable

### Update to `pkg/apiserver/options.go`

Add the new `--scylladb-local-dc` flag alongside the existing ScyllaDB flags from Story 1.5:

```go
func (o *SoteriaServerOptions) AddFlags(fs *pflag.FlagSet) {
    o.RecommendedOptions.AddFlags(fs)
    // ... existing flags from Story 1.5 ...
    fs.StringVar(&o.ScyllaDBLocalDC, "scylladb-local-dc", "",
        "Local ScyllaDB datacenter name for DC-aware routing (e.g. 'dc1')")
}
```

### File Organization

After this story, the following files are created or updated:

```
config/scylladb/
├── scyllacluster.yaml           # NEW — ScyllaCluster CR for two-DC deployment
└── kustomization.yaml           # NEW — Kustomize references

config/certmanager/
├── ca-issuer.yaml               # NEW — Self-signed root CA ClusterIssuer
├── ca-certificate.yaml          # NEW — Root CA Certificate
├── scylladb-serving-cert.yaml   # NEW — ScyllaDB serving/internode TLS cert
├── scylladb-client-cert.yaml    # NEW — API server mTLS client cert
└── kustomization.yaml           # NEW — Kustomize references

pkg/storage/scylladb/
├── client.go                    # UPDATED — LocalDC field, DCAwareRoundRobinPolicy
├── store.go                     # UPDATED — LWT support in GuaranteedUpdate, casUpdate, requiresLWT
└── schema.go                    # UPDATED — SchemaConfig supports NetworkTopologyStrategy

pkg/apiserver/
└── options.go                   # UPDATED — --scylladb-local-dc flag
```

Integration tests:

```
test/integration/replication/
├── suite_test.go                # NEW — multi-DC testcontainers setup
└── replication_test.go          # NEW — replication, resilience, recovery, conflict tests
```

### Dependencies

This story uses dependencies already added in Stories 1.1 and 1.2:
- `github.com/gocql/gocql` — ScyllaDB driver (already present)
- `github.com/testcontainers/testcontainers-go` — integration test containers (already present)

**No new Go dependencies required.** The `DCAwareRoundRobinPolicy` is part of `gocql`. LWT is a CQL feature exposed through standard `gocql` query execution with `ScanCAS()`.

**External operator dependencies (not Go modules):**
- `scylla-operator` — manages ScyllaDB on OpenShift (OperatorHub, certified March 2026)
- `cert-manager` — manages TLS certificates (OperatorHub)

### Testing Strategy

**Integration tests** (`test/integration/replication/`):

Tests use a multi-DC ScyllaDB cluster via testcontainers. Two ScyllaDB nodes are started with different `SCYLLA_DC` values, forming a two-DC cluster. Tests exercise the full chain: storage.Interface → ScyllaDB with cross-DC replication.

Test categories:

1. **Replication** (AC #2): Write on DC1, read on DC2. Verify eventual consistency with bounded delay.
2. **Resilience** (AC #3): Stop one DC's container, verify the other continues read/write operations without errors.
3. **Recovery** (AC #4): Stop DC1, write on DC2, restart DC1, repair, verify convergence.
4. **Conflict** (AC #5): Concurrent writes to the same key from both DCs — verify LWW for regular writes and CAS for critical fields.

All tests use `//go:build integration` tag and generous timeouts. Cross-DC replication in testcontainers is fast (sub-second) but DC failure/recovery tests may take 10-30 seconds for container stop/start cycles.

**Test timeouts:** DC failure simulation (container stop + restart + repair) can take 30-60 seconds. Set test timeout to 120 seconds for resilience and recovery tests.

### Critical Warnings

1. **Do NOT modify the kv_store table schema.** The generic KV schema from Story 1.2 is unchanged. Cross-site replication works at the ScyllaDB level on the same table — no schema changes needed for multi-DC.

2. **LWT has performance implications.** LWT uses Paxos and requires multi-DC quorum when both DCs are up. Only use LWT for state machine transitions (phase, result), never for routine status updates. Every non-LWT write uses LOCAL_ONE — fast and local.

3. **LWT fails when quorum is unavailable.** If one DC is down, LWT cannot achieve quorum. The fallback to standard LOCAL_ONE writes is safe because a single DC cannot conflict with itself. The `isUnavailableError()` check must correctly identify `gocql.ErrUnavailable` and similar errors.

4. **`DCAwareRoundRobinPolicy` requires the correct local DC name.** If `--scylladb-local-dc` is set incorrectly, all queries route to the wrong DC, adding cross-DC latency to every operation. Validate this flag early in startup.

5. **ScyllaDB `nodetool repair` is async.** After a DC recovers, anti-entropy repair synchronizes data. In production, the scylla-operator handles repair scheduling. In tests, explicitly call `nodetool repair` via container exec and wait for completion before asserting convergence.

6. **The CA secret must be shared across clusters manually.** cert-manager generates the CA on one cluster. The CA secret (`soteria-ca-secret`) must be copied to the second cluster before cert-manager on that cluster can issue certificates signed by the same CA. This is a documented operational step, not automated by the manifests.

7. **testcontainers multi-DC setup may need Docker network configuration.** Both ScyllaDB containers must be on the same Docker network to form a cluster. Use `testcontainers.NetworkRequest` to create a shared network.

8. **This story does NOT implement actual cross-cluster kubectl validation.** The acceptance criteria mention `kubectl get drplan` on both clusters, but that requires two running OpenShift clusters with the full API server deployed. Integration tests validate the underlying ScyllaDB replication behavior using direct sessions. End-to-end `kubectl` validation is an E2E concern for `test/e2e/`.

9. **`SchemaConfig` changes must be backward-compatible.** The `EnsureSchema()` function from Story 1.2 uses `SimpleStrategy` with RF=1 for development. The new `NetworkTopologyStrategy` configuration is an alternative, not a replacement. Both must work — `SimpleStrategy` for dev/CI, `NetworkTopologyStrategy` for production.

10. **Integration tests are expensive.** Multi-DC testcontainers startup takes 30-60 seconds. Keep the number of test functions reasonable and use subtests (`t.Run()`) to share the expensive setup across related assertions.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 1.6 (lines 501-544)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Data Architecture: ScyllaDB topology, consistency, deployment (lines 178-189)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Authentication & Security: mTLS, cert-manager (lines 191-198)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Project Structure: config/scylladb/, config/certmanager/ (lines 465-466)]
- [Source: _bmad-output/project-context.md — ScyllaDB Storage Layer rules (lines 80-89)]
- [Source: _bmad-output/project-context.md — Testing rules (lines 108-131)]
- [Source: _bmad-output/planning-artifacts/prd.md — Cross-Site Shared State requirements FR26-FR30 (lines 376-382)]
- [Source: _bmad-output/planning-artifacts/prd.md — NFR4, NFR5: ScyllaDB DC failure resilience (lines 420-422)]
- [Source: _bmad-output/planning-artifacts/prd.md — NFR12-NFR13: TLS encryption (lines 437-438)]
- [Source: _bmad-output/implementation-artifacts/1-2-scylladb-connection-generic-kv-schema.md — ScyllaDB client, schema, mTLS]
- [Source: _bmad-output/implementation-artifacts/1-3-scylladb-storage-interface-crud-operations.md — storage.Interface CRUD, GuaranteedUpdate]
- [Source: _bmad-output/implementation-artifacts/1-5-aggregated-api-server-api-registration.md — API server options, ScyllaDB flags]
- [External: ScyllaDB Multi-Datacenter Documentation — https://docs.scylladb.com/stable/operating-scylla/procedures/cluster-management/create-cluster-multidc.html]
- [External: ScyllaDB Lightweight Transactions — https://docs.scylladb.com/stable/using-scylla/lwt.html]
- [External: scylla-operator Documentation — https://operator.docs.scylladb.com/stable/]
- [External: cert-manager Documentation — https://cert-manager.io/docs/]
- [External: gocql DCAwareRoundRobinPolicy — https://pkg.go.dev/github.com/gocql/gocql#DCAwareRoundRobinPolicy]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
