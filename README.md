# Soteria

Soteria is a Kubernetes operator that orchestrates disaster recovery for KubeVirt virtual machines across two OpenShift clusters. It coordinates storage-level volume replication through a pluggable driver interface, enabling planned migrations, disaster failovers, and reprotect operations — all triggered by a human operator, never automatically.

## Description

Soteria manages the full DR lifecycle for KubeVirt workloads. A `DRPlan` selects VMs by label, groups their persistent volumes for atomic replication, and organizes failover into ordered waves with configurable concurrency. When a DR event occurs, the operator creates a `DRExecution` that drives a state machine through the appropriate transitions — stopping VMs on the source site, flipping volume replication roles via the storage driver, and starting VMs on the target site.

The project is built as a Kubernetes Aggregated API Server backed by ScyllaDB instead of etcd. This allows both OpenShift clusters to share the same DR state through ScyllaDB's multi-datacenter replication, without requiring a third coordination cluster. The controller-runtime reconciliation loop runs alongside the API server in a single binary.

## Capabilities

### DR State Machine

Soteria implements an 8-phase symmetric lifecycle with 4 rest states and 4 transition states:

```
SteadyState ──(planned_migration|disaster)──► FailingOver ──(complete)──► FailedOver
FailedOver  ──(reprotect)──────────────────► Reprotecting ──(complete)──► DRedSteadyState
DRedSteadyState ──(planned_migration|disaster)──► FailingBack ──(complete)──► FailedBack
FailedBack  ──(reprotect)──────────────────► ReprotectingBack ──(complete)──► SteadyState
```

Only the 4 rest states (`SteadyState`, `FailedOver`, `DRedSteadyState`, `FailedBack`) are persisted in `DRPlan.status.phase`. Transition states are derived at runtime from the rest phase and the active execution mode, avoiding stale in-progress phases if the controller restarts mid-operation.

Three execution modes drive transitions:
- **Planned migration** — graceful shutdown on source, wait for replication quiesce, promote on target. Used for scheduled maintenance or site evacuation.
- **Disaster** — force-promote on target without contacting the source site. Used when the primary datacenter is unreachable.
- **Reprotect** — reverse replication direction after a failover so the original site becomes the new target. Prepares the system for a future failback.

The state machine enforces that only valid transitions are allowed (e.g., you cannot reprotect from `SteadyState`) and that only one execution can be active per plan at any time.

### Volume Grouping

VMs under a DRPlan may need their volumes failed over atomically — for example, a database VM and an application VM in the same namespace that share a consistency domain. Soteria supports two consistency levels controlled by a namespace annotation (`soteria.io/consistency-level`):

- **VM-level** (default) — each VM's PVCs form their own volume group, failed over independently.
- **Namespace-level** — all VMs in the namespace that belong to the same DRPlan are grouped into a single volume group, ensuring their storage transitions happen as one atomic unit.

Volume groups are the unit of work for the `StorageProvider` driver interface. Each group maps to storage-level constructs (e.g., Dell PowerFlex consistency groups or ODF mirroring groups) through the pluggable driver.

### Chunking and Waves

DRPlans organize VMs into **waves** using a label key (`spec.waveLabel`). VMs sharing the same wave label value are failed over together before the next wave begins. Within each wave, Soteria **chunks** volume groups into **DRGroups** that respect the plan's `maxConcurrentFailovers` throttle:

1. Volume groups are sorted largest-first (namespace-level groups, being indivisible, are placed first).
2. Groups are packed into chunks until adding another would exceed the VM concurrency limit.
3. All chunks within a wave execute concurrently; the next wave starts only after the current wave completes.

This allows operators to control blast radius (waves) and infrastructure load (concurrency cap) independently. If a namespace-level group contains more VMs than `maxConcurrentFailovers`, a preflight warning is raised — the group is still processed as a single indivisible unit since splitting it would break consistency.

### Pluggable Storage Drivers

The `StorageProvider` interface defines 7 methods using a role-based replication model with three volume roles (`NonReplicated`, `Source`, `Target`) and four valid transitions routed through the `NonReplicated` state:

```
NonReplicated → Source   (SetSource)
NonReplicated → Target   (SetTarget)
Source        → NonReplicated (StopReplication)
Target        → NonReplicated (StopReplication)
```

Drivers register themselves at compile time via `init()` and are selected implicitly from PVC storage classes — no configuration CRD is needed. A conformance test suite in `pkg/drivers/conformance/` validates that any implementation honors the 7-method contract, idempotency requirements, and typed error conventions.

### Preflight Checks

Before any execution can start, Soteria's DRPlan controller runs continuous preflight validation:
- Discovers VMs matching the plan's label selector.
- Resolves volume groups and validates storage class to driver mappings.
- Detects wave conflicts (e.g., namespace-level groups spanning multiple waves).
- Reports chunk errors when namespace groups exceed the concurrency throttle.
- Sets a `Ready` condition on the DRPlan with detailed warnings.

### Fail-Forward Error Model

Rollback is impossible when the source datacenter is down. Soteria uses a fail-forward model where `PartiallySucceeded` is a first-class execution result. Failed DRGroups are recorded with error details, and operators can retry specific groups via the `soteria.io/retry-groups` annotation without re-executing the entire plan.

## Architecture

### Aggregated API Server over ScyllaDB

Soteria runs as a Kubernetes Aggregated API Server — it registers itself with the kube-apiserver via an `APIService` resource, so clients interact with `soteria.io/v1alpha1` resources (`DRPlan`, `DRExecution`, `DRGroupStatus`) through the standard Kubernetes API, with full support for RBAC, admission, audit logging, and `kubectl`.

Instead of etcd, the API server stores all resources in **ScyllaDB** using a generic key-value schema (`api_group`, `resource_type`, `namespace`, `name` → serialized blob). This mirrors the etcd storage model but gains ScyllaDB's built-in multi-datacenter replication with `NetworkTopologyStrategy` (RF=2 per DC, 4 nodes total).

The Kubernetes Watch API is implemented on top of **ScyllaDB CDC** (Change Data Capture). The CDC stream provides the change feed, and an initial `SELECT` fills the `Watch(resourceVersion=0)` contract. `ResourceVersion` values are derived from CDC Timeuuid timestamps converted to Unix microseconds, providing monotonic ordering within each datacenter.

A `k8s.io/apiserver` cacher wraps the ScyllaDB storage for watch fan-out — a single CDC consumer feeds an in-memory cache that serves all client watches efficiently.

### Cross-Site State Synchronization

Both OpenShift clusters point their Soteria instances at the same ScyllaDB cluster (nodes in both DCs). Each site's API server reads and writes locally with `LOCAL_ONE` consistency for normal operations. For critical state transitions — DRPlan phase changes, active execution guards, execution results — the storage layer uses **Lightweight Transactions (LWT)** with `SERIAL` consistency to prevent split-brain on the state machine.

The controller communicates with the API server through standard `client-go`, not directly with ScyllaDB. This means RBAC, admission webhooks, and audit logging are enforced uniformly regardless of which site initiates an operation.

### Single Binary, Dual Runtime

The `cmd/soteria/main.go` entry point runs two runtimes in a single process:

1. **Aggregated API Server** — serves the `soteria.io` API group, backed by ScyllaDB storage. All replicas serve API requests (active/active).
2. **controller-runtime Manager** — runs the DRPlan and DRExecution reconcilers, admission webhooks (DRPlan, DRExecution, VM), and health endpoints. Leader election (via Kubernetes Leases) ensures only one replica runs the workflow engine at a time.

### Admission Webhooks

| Webhook | Scope | Behavior |
|---------|-------|----------|
| DRPlan | Create/Update | Validates wave label, concurrency cap, site names; enforces immutability of primary/secondary sites after creation |
| DRExecution | Create | Validates plan exists, mode is valid, no other execution is active, and the state machine allows the transition |
| VM (KubeVirt) | Create/Update | Warns if referenced DRPlan doesn't exist; denies conflicting wave labels within a namespace-level consistency group |

### Checkpoint and Resume

Execution state is checkpointed per DRGroup in `DRExecution.status`. If the controller pod restarts mid-execution, the reconciler detects in-flight groups, resets them to `Pending`, and resumes from the last completed wave — skipping groups that already succeeded or failed. Storage driver methods are idempotent, making replay safe.

## Getting Started

### Prerequisites
- Go 1.25+
- Docker 17.03+
- kubectl v1.30+
- Access to a Kubernetes / OpenShift cluster (v1.30+)
- A ScyllaDB cluster (deployed via scylla-operator from OperatorHub)
- cert-manager (for TLS certificates)

### Building

```sh
make build
```

### Running Locally

```sh
make run
```

### Running Tests

```sh
make test              # Unit tests (envtest)
make integration       # Integration tests (requires ScyllaDB via testcontainers)
make lint              # Lint with golangci-lint
```

### To Deploy on the Cluster

**Build and push your image:**

```sh
make docker-build docker-push IMG=<some-registry>/soteria:tag
```

**Deploy the manager:**

```sh
make deploy IMG=<some-registry>/soteria:tag
```

> **NOTE:** If you encounter RBAC errors, you may need cluster-admin privileges.

**Apply sample resources:**

```sh
kubectl apply -k config/samples/
```

### To Uninstall

```sh
kubectl delete -k config/samples/   # Delete sample CRs
make undeploy                        # Remove the operator
```

## Project Distribution

### YAML Bundle (Kustomize)

Build the installer:

```sh
make build-installer IMG=<some-registry>/soteria:tag
```

This generates `dist/install.yaml` containing all resources (RBAC, webhooks, APIService, deployment). Users install with:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/soteria/<tag>/dist/install.yaml
```

### Helm Chart

Generate a Helm chart:

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

The chart is created under `dist/chart/`. If you modify the project after chart generation, re-run with `--force` and manually restore any custom values.

### OLM (Operator Lifecycle Manager)

```sh
make bundle            # Generate OLM bundle manifests
make bundle-build      # Build the bundle image
make bundle-push       # Push to registry
```

## Project Structure

```
cmd/soteria/           Single binary entry point (API server + controller)
pkg/apis/soteria.io/   API types (DRPlan, DRExecution, DRGroupStatus)
pkg/apiserver/         Aggregated API server, ScyllaDB storage factory
pkg/registry/          API resource storage strategies and validation
pkg/storage/scylladb/  ScyllaDB storage.Interface (KV store, CDC watch)
pkg/engine/            State machine, wave executor, chunker, reprotect
pkg/drivers/           StorageProvider interface, registry, noop/fake drivers
pkg/admission/         Validation webhooks (DRPlan, DRExecution, VM)
pkg/controller/        DRPlan and DRExecution reconcilers
internal/preflight/    Preflight checks and storage class resolution
config/                Kustomize manifests (RBAC, webhooks, APIService, ScyllaDB, certs)
test/integration/      Integration tests (envtest + ScyllaDB testcontainers)
test/e2e/              End-to-end tests (Kind cluster)
hack/                  Code generation scripts
```

## Contributing

Contributions are welcome. To get started:

1. Fork the repository and create a feature branch.
2. Run `make lint-fix` to auto-fix code style before committing.
3. Run `make test` to verify unit tests pass.
4. Run `make integration` if your change touches storage or API server paths.
5. Submit a pull request with a clear description of the change and its motivation.

Run `make help` for a full list of available targets.

## License

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
