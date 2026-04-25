---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
lastStep: 8
status: 'complete'
completedAt: '2026-04-05'
inputDocuments:
  - _bmad-output/planning-artifacts/prd.md
  - _bmad-output/planning-artifacts/product-brief-soteria.md
  - _bmad-output/planning-artifacts/product-brief-soteria-distillate.md
  - _bmad-output/brainstorming/brainstorming-session-20260404-121402.md
workflowType: 'architecture'
project_name: 'dr-orchestrator'
user_name: 'Raffa'
date: '2026-04-05'
---

# Architecture Decision Document

_This document builds collaboratively through step-by-step discovery. Sections are appended as we work through each architectural decision together._

## Project Context Analysis

### Requirements Overview

**Functional Requirements (45 FRs across 8 categories):**

| Category | FRs | Architectural Implication |
|---|---|---|
| DR Plan Management | FR1–FR8 | CRD design, label-driven discovery, admission webhooks for field validation and wave consistency |
| DR Execution & Workflow | FR9–FR19 | Purpose-built wave executor, DRGroup chunking engine, 4-state machine with 3 execution modes, fail-forward error model |
| Storage Abstraction | FR20–FR25 | Pluggable driver interface (6 methods), role-based replication model (NonReplicated/Source; Target observable but not engine-set), implicit driver selection from PVC storage classes, heterogeneous storage within a single plan |
| Cross-Site Shared State | FR26–FR30 | ScyllaDB-backed Aggregated API Server, LOCAL_ONE consistency, LWW conflict resolution, lightweight transactions for state transitions |
| Monitoring & Observability | FR31–FR34 | Prometheus metrics endpoint, replication health polling via GetReplicationStatus, unprotected VM detection |
| OCP Console Plugin | FR35–FR40 | PatternFly plugin, dual-mode UX (planning/disaster), cross-cluster awareness via shared ScyllaDB, live Gantt-chart execution monitor |
| Audit & Compliance | FR41–FR43 | Immutable DRExecution records, cross-site persistence, execution history |
| Access Control & Security | FR44–FR45 | Kubernetes-native RBAC on CRDs, external secrets references (K8s Secrets / Vault) |

**Non-Functional Requirements (19 NFRs across 4 categories):**

| Category | NFRs | Architectural Driver |
|---|---|---|
| Reliability | NFR1–NFR5 | Checkpoint-based execution resume after pod restart, active/passive via Kubernetes Leases, ScyllaDB survives single-DC failure |
| Performance | NFR6–NFR7 | API response < 2s, live execution updates visible within 5s |
| Scalability | NFR8–NFR11 | Up to 5,000 VMs, 100 DRPlans, 50 VMs/plan avg, concurrent plan execution, wave discovery < 10s |
| Security | NFR12–NFR15 | TLS on all ScyllaDB traffic (cert-manager), no credential leakage, admission webhook validation |
| Integration | NFR16–NFR19 | OLM lifecycle, PatternFly Console UX, OpenShift Prometheus conventions, stable StorageProvider interface for external drivers |

**Scale & Complexity:**

- Primary domain: Kubernetes Operator / Infrastructure Orchestrator (Go)
- Complexity level: Enterprise/High
- Estimated architectural components: ~10 major components (Aggregated API Server, ScyllaDB storage backend, workflow engine, storage driver registry, admission webhooks, leader election, OCP Console plugin, Prometheus exporter, CLI, conformance test framework)

### Technical Constraints & Dependencies

| Constraint | Source | Impact |
|---|---|---|
| Two datacenters only | Product Brief (explicit exclusion) | No quorum-based consensus; must use eventual consistency |
| Human-triggered failover only | Product Brief, PRD (FR18) | No failure detection subsystem; eliminates split-brain by design |
| VMs pre-exist on both clusters | PRD (Architectural Constraints) | Orchestrator does not create VMs or manage PVC bindings — only transitions volumes to Source role and starts VMs |
| Homogeneous storage replication | PRD (Architectural Constraints) | Dell-to-Dell, ODF-to-ODF only; cross-vendor replication not supported |
| Driver selection is implicit | PRD (Architectural Constraints) | No StorageProviderConfig CRD; driver determined from PVC storage class |
| Golang | PRD, Brainstorming | Kubernetes API ecosystem alignment |
| OLM deployment | PRD (NFR16) | Operator packaging, CSV, OperatorHub catalog |
| ScyllaDB prerequisite | PRD (Technical Architecture) | scylla-operator from OperatorHub (certified March 2026) |
| Apache 2.0 license | Product Brief | Open-source governance implications |
| API versioning starts at v1alpha1 | PRD | Conversion webhooks needed for future version graduation |

### Cross-Cutting Concerns Identified

| Concern | Affected Components | Notes |
|---|---|---|
| Cross-site state consistency | Aggregated API Server, ScyllaDB, all CRDs | The foundational architectural challenge; custom storage.Interface is highest risk |
| TLS encryption | ScyllaDB replication, API Server ↔ ScyllaDB, cross-site traffic | cert-manager dependency |
| Kubernetes RBAC | All CRDs, Console plugin, CLI | No custom auth — standard verb-based RBAC |
| Fail-forward error handling | Workflow engine, DRExecution, Console | Partial success is a first-class state; rollback not supported |
| Audit trail persistence | DRExecution, ScyllaDB, cross-site replication | Immutable records must survive DC failure |
| Admission validation | DRPlan field constraints, VM plan existence warning, wave consistency | Webhooks prevent misconfiguration before execution |
| Leader election | DR Orchestrator Controller | Active/passive via Kubernetes Leases (NFR2) |
| Observability | All runtime components | Prometheus metrics, /metrics endpoint, OpenShift monitoring conventions |
| Checkpoint & resume | Workflow engine, DRExecution | Pod restart must resume from last checkpoint (NFR1) |

## Starter Template Evaluation

### Primary Technology Domain

**Kubernetes Operator / Infrastructure Orchestrator (Go)** with an **OCP Console Plugin (TypeScript/React)** — identified from PRD technical constraints and brainstorming decisions. This is a multi-component project requiring distinct starter foundations for the Go backend and TypeScript frontend.

### Starter Options Considered

| Starter | Version | Evaluation | Verdict |
|---|---|---|---|
| kubebuilder | v4.13.1 (March 2026) | Active, latest scaffolding, excellent testing/CI/linting, controller-runtime, multigroup layout support. CRD-focused but provides the best project skeleton for Go. | **Selected** — project skeleton + controller |
| operator-sdk | v1.42.2 (March 2026) | Builds on kubebuilder, adds OLM bundle generation. Adds opinions and indirection that aren't needed — OLM bundling can be added independently. | Rejected — unnecessary layer over kubebuilder |
| kubernetes/sample-apiserver | Tracks k8s releases | Official reference for Aggregated API Server with custom storage. Fork-and-modify pattern, not scaffolding. Demonstrates `storage.Interface`, API registration, codegen. | **Selected** — architectural reference + code patterns |
| apiserver-builder-alpha | v1.23.0 (April 2022) | Designed for aggregated APIs with custom storage. Valuable design patterns for storage backend wiring and API registration. Last release 2022; stale. | **Inspiration only** — patterns, not dependency |
| openshift/console-plugin-template | Active (March 2026) | Official OCP Console plugin template. TypeScript, React, PatternFly 5, webpack module federation. | **Selected** — Console plugin |

### Selected Approach: Hybrid Multi-Component

**Rationale:** No single starter addresses Soteria's architecture. Kubebuilder provides the best Go project skeleton — Makefile, controller-runtime, testing harness, linting, multigroup support — without the indirection of operator-sdk. The Aggregated API Server with a custom ScyllaDB `storage.Interface` is hand-built following patterns from `kubernetes/sample-apiserver` and design inspiration from `apiserver-builder-alpha` (API registration, storage wiring, codegen patterns). OLM bundle generation is added as a standalone concern, not via operator-sdk. The Console plugin uses its own official template.

**Initialization Commands:**

```bash
# Go project skeleton (kubebuilder)
kubebuilder init --domain dr.orchestrator --repo github.com/soteria-project/soteria --plugins go/v4

# Console plugin (separate directory)
# Clone from openshift/console-plugin-template into console-plugin/
```

**Architectural Decisions Provided by Starters:**

**Language & Runtime:**
- Go (controller-runtime, k8s.io/apiserver, k8s.io/client-go)
- TypeScript (Console plugin — React, PatternFly 5)

**Build Tooling:**
- Makefile (kubebuilder generated — build, test, lint, manifests, docker-build)
- Dockerfile (multi-stage Go build)
- OLM bundle generation added independently (not via operator-sdk)
- Webpack module federation (Console plugin)

**Testing Framework:**
- Go: Ginkgo/Gomega (kubebuilder default), envtest for integration tests
- TypeScript: Jest (Console plugin template default)

**Code Organization:**
- `cmd/` — entry points (apiserver, controller)
- `pkg/apis/` — API type definitions for Aggregated API Server (pattern from sample-apiserver)
- `pkg/registry/` — API resource registry and storage wiring (pattern from sample-apiserver + apiserver-builder-alpha inspiration)
- `pkg/storage/` — custom ScyllaDB `storage.Interface` implementation
- `pkg/drivers/` — StorageProvider driver implementations
- `pkg/engine/` — workflow engine
- `internal/` — internal packages
- `console-plugin/` — OCP Console plugin (TypeScript)
- `config/` — kubebuilder-generated Kustomize manifests
- `hack/` — codegen scripts (inspired by sample-apiserver `hack/update-codegen.sh`)

**Development Experience:**
- `make run` — local development with controller-runtime
- `make test` — unit tests + envtest integration tests
- `make lint` — golangci-lint (kubebuilder v4.13.1 includes custom K8s logging linter)
- `make manifests` — generate RBAC, webhook configs
- `yarn start` / `yarn start-console` — Console plugin dev server

**Inspiration from apiserver-builder-alpha (patterns, not code):**
- API resource registration pattern (how types map to storage)
- Storage strategy wiring (how `storage.Interface` implementations are connected to API groups)
- Codegen integration patterns (deepcopy-gen, conversion-gen, openapi-gen)

**Note:** Project initialization should be an early implementation story. The ScyllaDB `storage.Interface` prototype should follow immediately as the highest-risk validation.

## Core Architectural Decisions

### Decision Priority Analysis

**Critical Decisions (Block Implementation):**
- ScyllaDB `storage.Interface` with CDC-based Watch (highest risk, prototype first)
- ResourceVersion mapping: CDC Timeuuid → Unix microseconds (int64)
- Generic KV schema for ScyllaDB (resource-agnostic storage)
- API group: `soteria.io/v1alpha1`
- Single binary: API server + controller in one process

**Important Decisions (Shape Architecture):**
- ScyllaDB RF=2 per DC (4 nodes total)
- mTLS-only for ScyllaDB authentication
- Standard client-go for controller ↔ API server communication
- Per-DRGroup execution checkpointing
- Console SDK watch hooks for real-time updates

**Deferred Decisions (Post-MVP):**
- Helm chart packaging (in addition to OLM)
- ScyllaDB backup/restore strategy
- CDC-to-polling fallback path (if CDC proves problematic)

### Data Architecture

| Decision | Choice | Rationale |
|---|---|---|
| Watch implementation | ScyllaDB CDC with initial SELECT snapshot | CDC provides native change stream; initial SELECT fills the k8s Watch(rev=0) contract; `scylla-cdc-go` v1.2.1 handles stream tracking |
| ResourceVersion | CDC Timeuuid → Unix microseconds (int64 → string) | Unifies resourceVersion with CDC stream position; monotonic within single DC; clock skew irrelevant (each DC serves own clients) |
| Caching layer | k8s.io/apiserver cacher wraps storage.Interface | Single CDC consumer, in-memory fan-out to all client watches; meets NFR6 (< 2s) and NFR7 (< 5s) trivially |
| Snapshot-to-CDC transition | In-memory PK deduplication set during overlap window | Handles writes between SELECT completion and CDC consumption start |
| Schema design | Generic KV store: `(api_group, resource_type, namespace, name)` → serialized blob | Mirrors etcd model; no CQL migrations when API fields change; resource-agnostic |
| ScyllaDB topology | `NetworkTopologyStrategy` DC1:2, DC2:2 (RF=2 per DC) | Survives single-node failure within a DC; 4 nodes total |
| ScyllaDB deployment | scylla-operator via OperatorHub | OpenShift-certified (March 2026); cert-manager for TLS |
| Consistency | LOCAL_ONE reads/writes; LWT for critical state transitions | Local operation during partitions; CAS for DR state machine transitions |

### Authentication & Security

| Decision | Choice | Rationale |
|---|---|---|
| ScyllaDB auth | mTLS only (cert-manager, shared CA across DCs) | No password rotation; extension API server authenticates via client certificates |
| RBAC | Kubernetes-native on CRDs (pre-decided in PRD) | Standard verb-based permissions on `soteria.io` resources |
| Secrets | External references — K8s Secrets or Vault (pre-decided in PRD) | Orchestrator never stores credentials directly |
| Transport encryption | TLS everywhere — ScyllaDB internode, client-to-ScyllaDB, cross-site (pre-decided in PRD) | cert-manager manages all certificates |

### API & Communication Patterns

| Decision | Choice | Rationale |
|---|---|---|
| API group | `soteria.io/v1alpha1` | Project-branded, not tied to OpenShift; resources: `drplans.soteria.io`, `drexecutions.soteria.io`, `drgroupstatuses.soteria.io` |
| Controller ↔ API server | Standard client-go via kube-apiserver proxy | Decoupled from ScyllaDB; RBAC/audit/admission enforced uniformly; extra hop negligible at our write rates |
| Checkpointing | Per-DRGroup — DRExecution status updated after each DRGroup completes | Natural execution boundary; concurrent operations within a DRGroup retry together; storage operations are idempotent |
| Error model | Fail-forward with PartiallySucceeded (pre-decided in PRD) | Rollback impossible when active DC is down |
| Unified handler model | Single FailoverHandler (parameterized by GracefulShutdown), single ReprotectHandler | Failover == failback, reprotect == restore at the code level. Direction encoded in state machine phases, not handler logic. Per-group path is always `StopReplication → StartVM` regardless of mode |
| Site-aware reconciliation | `--site-name` flag, `ReconcileRole(phase, mode, localSite, primary, secondary)` → Owner/Step0Only/None | Eliminates cross-site write contention. Source site runs Step 0 in planned migration; target site runs per-group waves. Disaster source exits immediately |
| VM readiness gate | Wave N+1 starts only after all wave N VMs reach Running state (event-driven via VM watch + timeout) | Enforces application dependency ordering defined by waves at runtime. Configurable timeout with mode-dependent policy |

### Frontend Architecture

| Decision | Choice | Rationale |
|---|---|---|
| State management | Console SDK `useK8sWatchResource()` hooks | Native k8s watch via aggregated API; no custom state library; real-time updates via watch events |
| UI framework | PatternFly 5 (pre-decided in PRD) | OpenShift Console consistency |
| Plugin architecture | Webpack module federation (from console-plugin-template) | Standard OCP dynamic plugin pattern |

### Infrastructure & Deployment

| Decision | Choice | Rationale |
|---|---|---|
| CI/CD | GitHub Actions — reuse [redhat-cop/github-workflows-operators](https://github.com/redhat-cop/github-workflows-operators) | Standard Red Hat operator CI; compatible with kubebuilder projects using operator-sdk CLI as standalone tool |
| OLM bundling | operator-sdk CLI (standalone, not scaffolding) | `generate bundle` + `bundle validate` work on any project with Makefile targets |
| Container images | Single Go binary (API server + controller) + separate Console plugin image | All replicas serve API (active/active); leader election controls workflow engine only |
| Multi-arch | `linux/amd64,linux/arm64,linux/ppc64le` | Default from redhat-cop pipeline |
| Makefile targets | `test`, `integration`, `helmchart-test` for pipeline compatibility | Matches redhat-cop workflow expectations |

### Decision Impact Analysis

**Implementation Sequence:**
1. ScyllaDB `storage.Interface` prototype (CDC Watch + generic KV schema) — highest risk, validate first
2. Aggregated API Server with DRPlan/DRExecution/DRGroupStatus types
3. Controller skeleton with leader election + client-go informers
4. StorageProvider interface + no-op driver
5. Workflow engine (wave executor, DRGroup chunking, checkpointing)
6. OLM bundle + CI pipeline integration
7. Console plugin (dashboard, plan detail, execution monitor)

**Cross-Component Dependencies:**
- Workflow engine depends on storage.Interface being validated (step 1 gates everything)
- Console plugin depends on API types being stable (step 2)
- CI pipeline depends on Makefile targets and Dockerfiles (step 6, can be set up early)
- OLM bundling depends on APIService registration pattern being finalized (step 2)

## Implementation Patterns & Consistency Rules

### Naming Patterns

**Go Code:**

| Area | Convention | Example |
|---|---|---|
| Packages | lowercase, single word, no underscores | `engine`, `drivers`, `storage` |
| Exported types | PascalCase (language-enforced) | `DRPlan`, `StorageProvider` |
| Unexported | camelCase (language-enforced) | `waveExecutor`, `groupChunker` |
| Interfaces | `-er` suffix where natural, else descriptive | `StorageProvider`, `WaveExecutor` |
| Error variables | `Err` prefix for sentinel errors | `ErrPlanNotFound`, `ErrInvalidState` |
| Error wrapping | lowercase, no punctuation, wrap with `%w` | `fmt.Errorf("promoting volume %s: %w", name, err)` |

**Kubernetes Resources:**

| Area | Convention | Example |
|---|---|---|
| CRD JSON fields | camelCase (Kubernetes convention) | `waveLabel`, `maxConcurrentFailovers` |
| Labels | `soteria.io/<key>` with kebab-case keys | `soteria.io/drplan`, `soteria.io/wave` |
| Annotations | `soteria.io/<key>` with kebab-case keys | `soteria.io/consistency-level` |
| Event reasons | PascalCase verb-past-tense | `FailoverStarted`, `WaveCompleted`, `GroupFailed` |
| Event messages | Human-readable sentence | `"Failover started for plan erp-full-stack in disaster mode"` |

**ScyllaDB:**

| Area | Convention | Example |
|---|---|---|
| Table names | snake_case | `kv_store` |
| Column names | snake_case | `api_group`, `resource_type`, `namespace`, `name`, `value` |

**Prometheus Metrics:**

| Area | Convention | Example |
|---|---|---|
| Prefix | `soteria_` | All metrics start with `soteria_` |
| Format | snake_case with unit suffix | `soteria_drplan_vms_total`, `soteria_failover_duration_seconds` |

### Structure Patterns

| Area | Convention | Rationale |
|---|---|---|
| Package organization | By layer (not by feature) | Matches kubebuilder/sample-apiserver convention |
| Test placement | `_test.go` co-located in same package | Go convention |
| Integration tests | `test/integration/` using envtest | Isolated from unit tests |
| E2E tests | `test/e2e/` using Ginkgo/Gomega | Full cluster tests |
| Generated code | `zz_generated_*.go` (k8s codegen convention) | Never hand-edit; regenerate via `hack/update-codegen.sh` |
| Interface files | Defined in the package that uses them | `StorageProvider` in `pkg/drivers/interface.go` |
| Internal packages | `internal/` for non-importable code | Driver authors import `pkg/`, not `internal/` |
| Console plugin | `console-plugin/` at repo root | Separate build, image, and concerns |
| Driver packages | `pkg/drivers/<vendor>/` | `pkg/drivers/noop/` |
| Driver mocks | `pkg/drivers/fake/` | k8s `<package>fake` convention |
| Conformance tests | `pkg/drivers/conformance/` | All drivers must pass; validates 6-method contract |

### CRD Status Patterns

| Area | Convention | Example |
|---|---|---|
| Status conditions | Standard `metav1.Condition` type | `type: Ready`, `type: Progressing`, `type: Degraded` |
| DRPlan phase | PascalCase string enum on `.status.phase` | `SteadyState`, `FailingOver`, `FailedOver`, `Reprotecting`, `DRedSteadyState`, `FailingBack`, `FailedBack`, `ReprotectingBack` |
| DRExecution result | `.status.result` enum | `Succeeded`, `PartiallySucceeded`, `Failed` |
| Per-DRGroup status | Embedded in `.status.waves[].groups[]` | `Completed`, `Failed`, `InProgress`, `Pending` |
| Timestamps | `metav1.Time` (ISO 8601) | `startTime`, `completionTime` |

### Controller Patterns

| Area | Convention | Rationale |
|---|---|---|
| Reconcile success | `ctrl.Result{}, nil` | Standard controller-runtime |
| Reconcile poll | `ctrl.Result{RequeueAfter: d}, nil` | Periodic re-check without error backoff |
| Reconcile error | `ctrl.Result{}, err` | Requeue with exponential backoff |
| Context | Always pass `ctx` from reconcile; never create new | Enables cancellation and tracing |
| Structured logging | `log.FromContext(ctx).WithValues("plan", plan.Name)` | controller-runtime convention |
| Log levels | Info(0) = state transitions; V(1) = normal ops; V(2) = debug | Kubernetes logging convention |
| Reconcile yield after setup | `ctrl.Result{RequeueAfter: 1*time.Millisecond}, nil` | Forces fresh object fetch in next cycle after multiple writes in eventual-consistency environment |
| Status updates | Prefer `client.MergeFrom` strategic merge patch over `client.Update` | Reduces resourceVersion conflict surface with ScyllaDB eventual consistency |
| Retry policy | `engine.ScyllaRetry` (200ms/2.0/0.3/8 steps) for all `RetryOnConflict` | Standard `retry.DefaultRetry` (10ms/5 steps) is too fast for ScyllaDB cross-DC propagation |

### ScyllaDB Eventual Consistency Patterns

Hard-won operational knowledge from 6 UAT runs on stretched etl6/etl7 clusters:

| Pattern | Implementation | Rationale |
|---|---|---|
| Setup yield | `reconcileSetup` returns `RequeueAfter: 1ms` after initial writes | Multiple writes within one reconcile (label, condition, plan status) create cascading resourceVersion conflicts. Yielding after setup forces the next phase to start with a fresh object fetch |
| ScyllaDB-tuned retry | `var ScyllaRetry = wait.Backoff{Steps: 8, Duration: 200ms, Factor: 2.0, Jitter: 0.3}` | `retry.DefaultRetry` (10ms base, 5 steps) exhausts retries before ScyllaDB propagates writes across DCs. The tuned backoff provides ~50s of total retry window |
| MergeFrom patch | `client.MergeFrom(obj.DeepCopy())` + `r.Patch()` instead of `r.Update()` | Strategic merge patch applies only changed fields, avoiding full-object replacement that conflicts with concurrent status updates from other controllers |
| Consistency-safe fetch | `fetchPlanWithActiveExecCheck(ctx, planName, execName)` retries `Get` until `plan.Status.ActiveExecution` matches expected value | After `reconcileSetup` writes `ActiveExecution`, the next reconcile may read a stale plan from ScyllaDB. The helper retries with `ScyllaRetry` backoff to wait for consistency |
| Checkpoint backoff | `KubeCheckpointer.WriteCheckpoint` uses `ScyllaRetry`-aligned parameters | Checkpoint writes after handler operations need the same eventual-consistency tolerance as other status updates |

**Anti-pattern: Using `retry.DefaultRetry` for ScyllaDB-backed resources.** This is the most common source of write contention errors. Always use `engine.ScyllaRetry`.

### Driver Implementation Patterns

| Area | Convention | Rationale |
|---|---|---|
| Registration | `init()` + registry pattern | Discovered at startup, selected at runtime by storage class |
| Error types | Typed errors from `pkg/drivers/errors.go` | `ErrVolumeNotFound`, `ErrReplicationNotReady`, `ErrInvalidTransition` |
| Timeouts | Accept `context.Context`; respect cancellation | Caller controls timeout via context deadline |
| Idempotency | All 6 methods must be idempotent | Safe to retry after crash/restart |

### Testing Patterns

| Area | Convention | Example |
|---|---|---|
| Test naming | `TestFunction_Scenario_Expected` | `TestWaveExecutor_PartialFailure_ReportsPartiallySucceeded` |
| Unit tests | Table-driven where appropriate | Standard `testing` package |
| Driver conformance | Shared suite in `pkg/drivers/conformance/` | All drivers run same test battery |

### Enforcement Guidelines

**All AI agents MUST:**

- Follow Go naming conventions strictly (enforced by `golangci-lint`)
- Use `metav1.Condition` for all CRD status conditions — no custom condition types
- Return typed errors from `pkg/drivers/errors.go` for all driver implementations
- Include `context.Context` as first parameter on all methods that may block
- Write `_test.go` files for all new exported functions
- Use structured logging via `log.FromContext(ctx)` — no `fmt.Println` or `log.Printf`
- Keep driver implementations idempotent — document any non-idempotent edge cases

**Anti-Patterns (explicitly forbidden):**

- Creating custom authorization logic (use Kubernetes RBAC exclusively)
- Storing state in controller memory across reconcile calls (use CRD status or ScyllaDB)
- Direct ScyllaDB access from the controller (always go through kube-apiserver)
- Creating new `context.Background()` inside reconcile handlers
- Using `log.Fatal` or `os.Exit` in library code (only in `cmd/` entry points)
- Hand-editing `zz_generated_*.go` files
- Using `retry.DefaultRetry` for operations against ScyllaDB-backed API server — always use `engine.ScyllaRetry`
- Using `client.Update` for metadata/label changes in multi-controller environments — use `client.MergeFrom` patch instead

## Project Structure & Boundaries

### Complete Project Directory Structure

```
soteria/
├── README.md
├── LICENSE                                  # Apache 2.0
├── Makefile                                 # kubebuilder-generated + custom targets
├── Dockerfile                               # Multi-stage Go build (single binary)
├── bundle.Dockerfile                        # OLM bundle image
├── go.mod
├── go.sum
├── .golangci.yml                            # golangci-lint config
├── .gitignore
│
├── .github/
│   └── workflows/
│       ├── pr-operator.yml                  # PR gates (reuses redhat-cop/github-workflows-operators)
│       └── release-operator.yml             # Release pipeline (reuses redhat-cop/github-workflows-operators)
│
├── cmd/
│   └── soteria/
│       └── main.go                          # Single binary: API server + controller
│                                            # Leader election via ctrl.Options{LeaderElection: true}
│
├── pkg/
│   ├── apis/                                # API type definitions (sample-apiserver pattern)
│   │   └── soteria.io/
│   │       ├── install/
│   │       │   └── install.go               # Scheme registration for all versions
│   │       └── v1alpha1/
│   │           ├── types.go                 # DRPlan, DRExecution, DRGroupStatus structs
│   │           ├── defaults.go              # Defaulting functions
│   │           ├── validation.go            # Type-level validation
│   │           ├── doc.go                   # +groupName=soteria.io marker
│   │           ├── register.go              # GVR registration
│   │           └── zz_generated_deepcopy.go # Generated — never hand-edit
│   │
│   ├── apiserver/                           # Aggregated API Server setup
│   │   ├── apiserver.go                     # Server config, API group registration
│   │   └── options.go                       # Server startup options (ScyllaDB connection, TLS)
│   │
│   ├── registry/                            # API resource → storage wiring
│   │   ├── drplan/
│   │   │   ├── strategy.go                  # Create/Update/Delete strategy for DRPlan
│   │   │   └── storage.go                   # REST storage binding
│   │   ├── drexecution/
│   │   │   ├── strategy.go                  # Strategy for DRExecution (append-only)
│   │   │   └── storage.go
│   │   └── drgroupstatus/
│   │       ├── strategy.go
│   │       └── storage.go
│   │
│   ├── storage/                             # ScyllaDB storage.Interface implementation
│   │   └── scylladb/
│   │       ├── store.go                     # storage.Interface: Get, GetList, Create, Update, Delete
│   │       ├── watch.go                     # CDC-based Watch: snapshot + CDC stream + dedup
│   │       ├── versioner.go                 # Timeuuid → int64 (Unix µs) → resourceVersion string
│   │       ├── codec.go                     # Serialize/deserialize k8s objects to/from blob
│   │       ├── client.go                    # ScyllaDB connection management (mTLS, gocql)
│   │       └── schema.go                    # Generic KV table DDL and CDC enablement
│   │
│   ├── drivers/                             # StorageProvider abstraction
│   │   ├── interface.go                     # StorageProvider interface (6 methods)
│   │   ├── errors.go                        # ErrVolumeNotFound, ErrInvalidTransition, etc.
│   │   ├── registry.go                      # Driver registration + discovery from PVC storage class
│   │   ├── noop/
│   │   │   └── driver.go                    # No-op driver (dev/test/CI)
│   │   ├── fake/
│   │   │   └── driver.go                    # Mock driver for unit tests
│   │   └── conformance/
│   │       └── suite.go                     # Conformance test suite — all drivers must pass
│   │
│   ├── engine/                              # Workflow engine
│   │   ├── executor.go                      # Wave executor: sequential waves, concurrent DRGroups
│   │   ├── chunker.go                       # DRGroup chunking per maxConcurrentFailovers
│   │   ├── statemachine.go                  # 4-state DR cycle: transitions + validation
│   │   ├── checkpoint.go                    # Per-DRGroup checkpoint: write status after each group
│   │   ├── discovery.go                     # VM discovery via `soteria.io/drplan=<planName>` label + wave grouping
│   │   ├── failover.go                      # Unified failover/failback workflow (planned + disaster modes)
│   │   ├── reprotect.go                     # Re-protect / restore workflow
│   │   └── roles.go                         # Site-aware reconcile role: ReconcileRole, TargetSiteForPhase
│   │
│   ├── controller/                          # Kubernetes controllers (controller-runtime)
│   │   ├── drplan/
│   │   │   └── reconciler.go               # DRPlan reconciler: discovery, replication health
│   │   └── drexecution/
│   │       └── reconciler.go               # DRExecution reconciler: triggers engine, updates status
│   │
│   ├── admission/                           # Admission webhooks
│   │   ├── drplan_validator.go              # DRPlan field validation (waveLabel, maxConcurrentFailovers)
│   │   ├── vm_validator.go                  # VM plan existence warning, namespace-level wave consistency
│   │   └── drexecution_validator.go         # State transition validation, pre-flight checks
│   │
│   └── metrics/                             # Prometheus metrics
│       └── metrics.go                       # soteria_drplan_vms_total, soteria_failover_duration_seconds
│
├── internal/                                # Internal packages (not importable by drivers)
│   └── preflight/
│       └── checks.go                        # Pre-flight: capacity, replication health, RPO estimate
│
├── config/                                  # kubebuilder Kustomize manifests
│   ├── default/
│   ├── rbac/                                # RBAC for soteria.io resources
│   ├── webhook/                             # Admission webhook config
│   ├── apiservice/                          # APIService registration for aggregation layer
│   ├── scylladb/                            # ScyllaDB ScyllaCluster CR (reference deployment)
│   └── certmanager/                         # cert-manager Certificate CRs (mTLS)
│
├── hack/
│   ├── update-codegen.sh                    # deepcopy-gen, conversion-gen, openapi-gen
│   └── verify-codegen.sh                    # CI: ensure generated code is up to date
│
├── test/
│   ├── integration/                         # envtest-based integration tests
│   │   ├── storage/                         # storage.Interface against ScyllaDB (or testcontainers)
│   │   ├── engine/                          # Workflow engine with mock drivers
│   │   └── apiserver/                       # API server with ScyllaDB backend
│   └── e2e/                                 # Full cluster e2e (Ginkgo/Gomega)
│       ├── failover_test.go
│       ├── planned_migration_test.go
│       ├── reprotect_test.go
│       └── suite_test.go
│
├── bundle/                                  # OLM bundle
│   ├── manifests/
│   │   ├── soteria.clusterserviceversion.yaml
│   │   └── apiservice.yaml
│   └── metadata/
│       └── annotations.yaml
│
└── console-plugin/                          # OCP Console plugin (separate image)
    ├── package.json
    ├── tsconfig.json
    ├── webpack.config.ts
    ├── console-extensions.json              # Plugin extension points
    ├── Dockerfile                           # nginx serving built plugin
    ├── src/
    │   ├── index.ts
    │   ├── components/
    │   │   ├── DRDashboard/                 # FR35: Dashboard
    │   │   ├── DRPlanDetail/                # FR36: Plan detail
    │   │   ├── FailoverConfirmation/        # FR37-38: Pre-flight + confirmation
    │   │   ├── ExecutionMonitor/            # FR39: Live Gantt chart
    │   │   └── CrossClusterStatus/          # FR40: Cross-cluster awareness
    │   ├── hooks/
    │   │   └── useDRResources.ts            # useK8sWatchResource wrappers
    │   ├── models/
    │   │   └── types.ts                     # TypeScript types matching CRD schemas
    │   └── utils/
    │       └── formatters.ts                # RPO, duration, status formatters
    └── tests/
        └── components/                      # Jest component tests
```

### Architectural Boundaries

**API Boundary:**
All external access flows through the Kubernetes API. The Aggregated API Server registers `soteria.io` with the kube-apiserver's aggregation layer. No separate REST endpoints. Console plugin, kubectl, and the controller all use the same API path.

```
kubectl / Console / Controller
        │
        ▼
kube-apiserver (proxy for soteria.io)
        │
        ▼
Soteria Extension API Server
        │
        ▼
ScyllaDB (generic KV store)
```

**Storage Boundary:**
Only `pkg/storage/scylladb/` touches ScyllaDB directly. The controller and Console go through the Kubernetes API. Enforced by `internal/` convention and anti-pattern rules.

**Driver Boundary:**
`pkg/drivers/interface.go` defines the 6-method contract. Everything above (`pkg/engine/`, `pkg/controller/`) is driver-agnostic. Everything below (`pkg/drivers/noop/`) is vendor-specific. External driver authors import `pkg/drivers/`.

**Engine Boundary:**
`pkg/engine/` owns workflow execution. Receives a plan and a driver, executes waves, writes checkpoints via the Kubernetes API. Does not know about ScyllaDB, CDC, or API server internals.

**Console Boundary:**
`console-plugin/` is a fully separate TypeScript project. Communicates exclusively via `useK8sWatchResource()`. No direct Go imports, no shared code.

### Requirements to Structure Mapping

| FR Category | Primary Location | Key Files |
|---|---|---|
| DR Plan Management (FR1–FR8) | `pkg/apis/`, `pkg/controller/drplan/`, `pkg/admission/` | `types.go`, `reconciler.go`, `drplan_validator.go` |
| DR Execution & Workflow (FR9–FR19) | `pkg/engine/`, `pkg/controller/drexecution/` | `failover.go`, `reprotect.go`, `statemachine.go`, `executor.go`, `checkpoint.go` |
| Storage Abstraction (FR20, FR21, FR23–FR25) | `pkg/drivers/` | `interface.go`, `registry.go`, `noop/driver.go` |
| Cross-Site Shared State (FR26–FR30) | `pkg/storage/scylladb/`, `pkg/apiserver/` | `store.go`, `watch.go`, `versioner.go` |
| Monitoring (FR31–FR34) | `pkg/metrics/`, `pkg/controller/drplan/` | `metrics.go`, `reconciler.go` |
| Console Plugin (FR35–FR40) | `console-plugin/src/components/` | `DRDashboard/`, `ExecutionMonitor/` |
| Audit (FR41–FR43) | `pkg/apis/`, `pkg/engine/` | `types.go`, `checkpoint.go` |
| Security (FR44–FR45) | `config/rbac/`, `pkg/drivers/registry.go` | RBAC manifests, secrets reference |

### Data Flow

```
1. Admin creates DRPlan (kubectl/Console)
   → kube-apiserver → Extension API Server → ScyllaDB
   → CDC → cacher → DRPlan controller informer

2. Operator triggers failover (kubectl/Console)
   → Creates DRExecution → Extension API Server → ScyllaDB
   → CDC → cacher → DRExecution controller informer on BOTH sites
   → Each site computes ReconcileRole(phase, mode, localSite, ...)
   → Source site (RoleStep0, planned only): StopVM → write Step0Complete
   → Target site (RoleOwner): waits for Step0Complete (planned) or starts immediately (disaster)
   → Per-DRGroup: StopReplication → StartVM → WaitVMReady (event-driven)
   → Update DRExecution status (checkpoint with ScyllaRetry) → ScyllaDB
   → CDC → cacher → Console watch → live Gantt chart

3. Replication health monitoring (background)
   → DRPlan controller polls driver.GetReplicationStatus()
   → Updates DRPlan status conditions → ScyllaDB
   → CDC → cacher → Console dashboard

4. Full DR lifecycle (8-phase cycle)
   SteadyState →[failover]→ FailingOver →[complete]→ FailedOver
   →[reprotect]→ Reprotecting →[complete]→ DRedSteadyState
   →[failback]→ FailingBack →[complete]→ FailedBack
   →[restore]→ ReprotectingBack →[complete]→ SteadyState
   Failover and failback use the same FailoverHandler (parameterized by GracefulShutdown).
   Reprotect and restore use the same ReprotectHandler.
   DRPlan.Status.Phase holds only rest states; ActiveExecution points to in-progress execution.
   EffectivePhase derives transient phase for display.
```

## Architecture Validation Results

### Coherence Validation ✅

**Decision Compatibility:** All technology choices are compatible. kubebuilder v4.13.1 + `k8s.io/apiserver` + controller-runtime + ScyllaDB (`gocql` + `scylla-cdc-go` v1.2.1) + console-plugin-template form a coherent stack. Single binary with leader election via `ctrl.Options` works cleanly. CDC Timeuuid → resourceVersion integrates correctly with the k8s cacher layer. operator-sdk CLI standalone for OLM bundling is compatible with kubebuilder projects. No contradictory decisions.

**Pattern Consistency:** Go naming, k8s CRD conventions (camelCase JSON, `metav1.Condition`), `soteria.io/` labels/annotations align with the `soteria.io` API group. Driver interface patterns (idempotency, context propagation, typed errors) are consistent with the fail-forward engine design. Controller patterns follow controller-runtime standards.

**Structure Alignment:** Project structure supports all decisions. Boundaries are clean and enforced: `pkg/storage/scylladb/` for highest-risk component, `pkg/drivers/` with public API for external contributors, `internal/` for non-importable code, `console-plugin/` fully isolated.

### Requirements Coverage ✅

**Functional Requirements (45/45 covered):**

| FR | Coverage | Location |
|---|---|---|
| FR1–FR8 (Plan Management) | ✅ | `pkg/apis/`, `pkg/controller/drplan/`, `pkg/admission/`, `pkg/engine/discovery.go` |
| FR9–FR19 (Execution) | ✅ | `pkg/engine/`, `pkg/controller/drexecution/`, `internal/preflight/` |
| FR20, FR21, FR23–FR25 (Storage) | ✅ | `pkg/drivers/`, `conformance/`, `noop/` |
| FR26–FR30 (Cross-Site) | ✅ | `pkg/storage/scylladb/`, `pkg/apiserver/` |
| FR31–FR34 (Monitoring) | ✅ | `pkg/metrics/`, `pkg/controller/drplan/` |
| FR35–FR40 (Console) | ✅ | `console-plugin/src/components/` |
| FR41–FR43 (Audit) | ✅ | DRExecution type + `pkg/engine/checkpoint.go` |
| FR44–FR45 (Security) | ✅ | `config/rbac/`, `pkg/drivers/registry.go` |

**Non-Functional Requirements (19/19 covered):**

| NFR | Coverage | How |
|---|---|---|
| NFR1 (Resume) | ✅ | Per-DRGroup checkpoint in `checkpoint.go` |
| NFR2 (Leader election) | ✅ | `ctrl.Options{LeaderElection: true}` |
| NFR3 (99% success) | ✅ | Idempotent drivers, fail-forward, retry |
| NFR4 (ScyllaDB DC failure) | ✅ | RF=2 per DC, LOCAL_ONE, async replication |
| NFR5 (Writes during disaster) | ✅ | LOCAL_ONE — no cross-site dependency |
| NFR6 (API < 2s) | ✅ | k8s cacher serves from memory |
| NFR7 (Updates < 5s) | ✅ | CDC → cacher → Console watch |
| NFR8–NFR11 (Scale) | ✅ | Generic KV, concurrent plans, plan-label discovery |
| NFR12–NFR13 (TLS) | ✅ | cert-manager, `config/certmanager/` |
| NFR14 (No credential leak) | ✅ | Anti-pattern rule; mTLS |
| NFR15 (Admission) | ✅ | `pkg/admission/` |
| NFR16 (OLM) | ✅ | `bundle/`, operator-sdk CLI |
| NFR17 (PatternFly) | ✅ | console-plugin-template |
| NFR18 (Prometheus) | ✅ | `pkg/metrics/`, `soteria_` prefix |
| NFR19 (Stable interface) | ✅ | `pkg/drivers/interface.go` public API |

### Gap Analysis

**Critical Gaps:** None.

**Important Gaps (non-blocking):**

1. ~~**Unified handler model:**~~ **RESOLVED (Epic 5, Story 5.7)** — `FailoverHandler` parameterized by `GracefulShutdown` only. Per-group path unified to `StopReplication → StartVM`. `SetTarget` removed from interface. `Force` flags removed.
2. **Hook extension points:** Hooks are post-v1, but define empty hook interfaces in the executor (`preWave`, `postWave`, `preVM`, `postVM` callbacks) so v2 hooks don't require engine restructuring.
3. **ScyllaDB operational docs:** `config/scylladb/` has the ScyllaCluster CR but sizing guidance and reference architecture should be documented post-v1.
4. **Cross-cluster integration testing:** Current envtest setup tests single-controller scenarios. Dual-controller testing (two `LocalSite` values against shared state) would catch the write-contention and site-ownership bugs found during UAT. Proposed as Tier B in Epic 5 retrospective.

**Resolved since original architecture (Epic 5):**
- ScyllaDB eventual consistency patterns documented and codified (`ScyllaRetry`, `MergeFrom`, setup yield, consistency-safe fetch)
- Site-aware reconcile ownership eliminates cross-site contention (`ReconcileRole`, `--site-name`)
- VM readiness gate enforces wave ordering at runtime (event-driven via VM watch)
- Rest-state-only DRPlan model eliminates stuck-transient-phase bug class (`ActiveExecution` pointer)

**Deferred (by design):** Test mode, hook framework, Console health monitoring/wizard, Dell/Pure/NetApp drivers.

### Architecture Completeness Checklist

**✅ Requirements Analysis**
- [x] Project context analyzed (45 FRs, 19 NFRs)
- [x] Scale and complexity assessed (Enterprise/High)
- [x] Technical constraints identified (10 constraints)
- [x] Cross-cutting concerns mapped (9 concerns)

**✅ Architectural Decisions**
- [x] Critical decisions documented with versions
- [x] Technology stack fully specified
- [x] Integration patterns defined
- [x] Performance considerations addressed

**✅ Implementation Patterns**
- [x] Naming conventions established
- [x] Structure patterns defined
- [x] CRD status patterns specified
- [x] Controller and driver patterns documented
- [x] Anti-patterns explicitly forbidden

**✅ Project Structure**
- [x] Complete directory structure defined
- [x] Component boundaries established
- [x] Integration points mapped
- [x] Requirements to structure mapping complete
- [x] Data flow documented

### Architecture Readiness Assessment

**Overall Status:** READY FOR IMPLEMENTATION

**Confidence Level:** High

**Key Strengths:**
- ScyllaDB shared state eliminates CRD-sync-between-clusters problem
- CDC Timeuuid as resourceVersion unifies change detection and versioning
- k8s cacher layer absorbs performance requirements with minimal custom code
- Generic KV schema means zero CQL migrations as API evolves
- Clean boundaries enable parallel development

**Areas for Future Enhancement:**
- ScyllaDB operational documentation (sizing, backup, monitoring)
- Hook framework extension points (preserved for v2)
- CDC-to-polling fallback (if CDC proves problematic)
- Additional storage drivers (Dell, Pure, NetApp)

### Implementation Handoff

**AI Agent Guidelines:**
- Follow all architectural decisions exactly as documented
- Use implementation patterns consistently across all components
- Respect project structure and boundaries
- Refer to this document for all architectural questions

**First Implementation Priority:**
1. `kubebuilder init --domain soteria.io --repo github.com/soteria-project/soteria --plugins go/v4`
2. ScyllaDB `storage.Interface` prototype (`pkg/storage/scylladb/`) — validates the highest-risk architectural bet before any other work
