---
project_name: 'dr-orchestrator'
user_name: 'Raffa'
date: '2026-04-06'
sections_completed: ['technology_stack', 'language_rules', 'framework_rules', 'testing_rules', 'code_quality', 'workflow_rules', 'critical_rules']
status: 'complete'
rule_count: 78
optimized_for_llm: true
---

# Project Context for AI Agents

_This file contains critical rules and patterns that AI agents must follow when implementing code in this project. Focus on unobvious details that agents might otherwise miss._

---

## Technology Stack & Versions

Use latest stable versions for all dependencies unless a specific constraint is noted.

| Technology | Role | Notes |
|---|---|---|
| **Go** (latest stable) | Operator, API server, controllers, drivers | controller-runtime, k8s.io/apiserver, client-go |
| **kubebuilder** v4.13.1+ | Project scaffolding | Makefile, controller-runtime integration, multigroup layout |
| **k8s.io/apiserver** | Aggregated API Server | Custom `storage.Interface` backed by ScyllaDB |
| **ScyllaDB** (scylla-operator via OperatorHub) | Cross-site shared state | gocql driver, scylla-cdc-go for CDC streams |
| **TypeScript / React** | OCP Console plugin | openshift/console-plugin-template starter |
| **PatternFly 5** | UI components | Mandated by NFR17 — no other UI libraries |
| **Webpack** (module federation) | Console plugin architecture | Standard OCP dynamic plugin pattern |
| **Ginkgo/Gomega** | Go integration + E2E testing | envtest for integration tests |
| **Jest** | TypeScript component testing | Console plugin template default |
| **golangci-lint** | Go linting | Includes K8s logging linter from kubebuilder |
| **cert-manager** | TLS certificate management | mTLS for all ScyllaDB traffic |
| **operator-sdk CLI** | OLM bundle generation only | Standalone tool, not scaffolding — `generate bundle` + `bundle validate` |
| **GitHub Actions** | CI/CD | Reuse redhat-cop/github-workflows-operators |

**Architectural References (not runtime dependencies):**
- `kubernetes/sample-apiserver` — patterns for storage.Interface, API registration, codegen
- `apiserver-builder-alpha` — design inspiration for storage strategy wiring and API registration patterns

## Critical Implementation Rules

### Language-Specific Rules

**Go:**

- Error wrapping: lowercase, no punctuation, wrap with `%w` — `fmt.Errorf("promoting volume %s: %w", name, err)`
- Sentinel errors use `Err` prefix — `ErrPlanNotFound`, `ErrInvalidState`
- Driver implementations return typed errors from `pkg/drivers/errors.go` — never raw errors
- Always pass `ctx` from reconcile handler — never create `context.Background()` inside reconcile or library code
- Structured logging only: `log.FromContext(ctx).WithValues("plan", plan.Name)` — no `fmt.Println`, no `log.Printf`
- Log levels: Info(0) = state transitions, V(1) = normal ops, V(2) = debug
- Interfaces defined where consumed — `StorageProvider` in `pkg/drivers/interface.go`
- `zz_generated_*.go` files are codegen output — never hand-edit; regenerate via `hack/update-codegen.sh`
- `log.Fatal` and `os.Exit` only in `cmd/` entry points — never in `pkg/` or `internal/`
- Package naming: lowercase single word, no underscores — `engine`, `drivers`, `storage`

**TypeScript (Console Plugin):**

- Data fetching: Console SDK hooks exclusively (`useK8sWatchResource`, `useK8sModel`) — no direct API calls
- Styling: PatternFly CSS custom properties only — no hardcoded colors, spacing, or font sizes (ensures dark mode works)
- No external UI libraries — PatternFly 5 only, no Material UI, no Chakra
- State management: Console SDK watch hooks — no Redux, no Zustand, no custom state libraries

### Framework-Specific Rules

**Kubernetes Operator / API Server:**

- API group: `soteria.io/v1alpha1` — resources: `drplans`, `drexecutions`, `drgroupstatuses`
- Single binary: API server + controller in one process. Leader election (`ctrl.Options{LeaderElection: true}`) controls workflow engine only — all replicas serve API
- Controller communicates via standard client-go through kube-apiserver proxy — never touches ScyllaDB directly
- Reconcile returns: success `ctrl.Result{}, nil` | poll `ctrl.Result{RequeueAfter: d}, nil` | error `ctrl.Result{}, err`
- No in-memory state across reconcile calls — use CRD status or ScyllaDB
- CRD status conditions: always `metav1.Condition` — no custom condition types
- CRD JSON fields: camelCase — `vmSelector`, `waveLabel`, `maxConcurrentFailovers`
- Labels/annotations: `soteria.io/<key>` kebab-case — `soteria.io/wave`, `soteria.io/plan-name`
- Event reasons: PascalCase past-tense — `FailoverStarted`, `WaveCompleted`, `GroupFailed`
- RBAC: Kubernetes-native only — no custom authorization logic

**ScyllaDB Storage Layer:**

- Only `pkg/storage/scylladb/` touches ScyllaDB — everything else goes through Kubernetes API
- Watch: CDC with initial SELECT snapshot; scylla-cdc-go handles stream tracking
- ResourceVersion: CDC Timeuuid → Unix microseconds (int64 → string), monotonic within single DC
- Schema: generic KV store `(api_group, resource_type, namespace, name)` → blob — no CQL migrations on field changes
- Table/column naming: snake_case — `kv_store`, `api_group`, `resource_type`
- Consistency: LOCAL_ONE reads/writes; LWT for critical state transitions only
- Auth: mTLS only via cert-manager — no passwords
- Topology: NetworkTopologyStrategy DC1:2, DC2:2 (RF=2 per DC, 4 nodes total)

**StorageProvider Driver Framework:**

- 9-method interface: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, EnableReplication, DisableReplication, PromoteVolume, DemoteVolume, ResyncVolume, GetReplicationInfo
- All 9 methods must be idempotent — safe to retry after crash/restart
- Driver selection is implicit from PVC storage class — no StorageProviderConfig CRD
- Registration: `init()` + registry pattern, discovered at startup
- Timeouts: accept `context.Context`, respect cancellation
- Driver packages: `pkg/drivers/<vendor>/` — `noop/`, `odf/`, `fake/`
- All drivers must pass conformance suite at `pkg/drivers/conformance/suite.go`

**React / Console Plugin:**

- Console SDK hooks (`useK8sWatchResource()`) for all data — real-time via K8s watch events
- PatternFly 5 tokens for DR status colors: success (green), warning (yellow), danger (red), disabled (gray), info (blue)
- Navigation: OCP Console list → detail → action pattern — never deviate
- Accessibility: color-independent status (icon + text label alongside color) on all indicators

### Testing Rules

**Testing Pyramid:**

| Layer | Tool | Scope |
|---|---|---|
| Pure logic unit tests | Standard `testing`, table-driven | State machine, chunking math, wave formation — no K8s API needed |
| Controller/reconciler tests | **envtest** (real local etcd + API server) | Reconcile loops, RBAC, webhooks, status updates, owner references |
| ScyllaDB storage tests | envtest + testcontainers (real ScyllaDB) | storage.Interface, CDC Watch, ResourceVersion mapping, KV operations |
| Integration tests | envtest + mock drivers | Workflow engine end-to-end with fake storage |
| E2E tests | Ginkgo/Gomega on real cluster | Full operator against real OpenShift + real storage |
| Driver conformance | Ginkgo/Gomega | Full DR lifecycle per driver — `pkg/drivers/conformance/suite.go` |

**Critical Rules:**

- **envtest over fake client:** Use `envtest.Environment` for all controller tests — never use controller-runtime's fake client for reconciler testing. Fake client doesn't handle resourceVersion, UIDs, timestamps, webhooks, or status subresources correctly
- Test naming: `TestFunction_Scenario_Expected` — e.g. `TestWaveExecutor_PartialFailure_ReportsPartiallySucceeded`
- Unit tests: `_test.go` co-located in same package
- Integration tests: `test/integration/` with envtest — isolated from unit tests
- E2E tests: `test/e2e/` using Ginkgo/Gomega — full cluster
- Mock drivers: `pkg/drivers/fake/` follows k8s `<package>fake` convention
- All drivers must pass conformance suite covering full DR lifecycle (create volume groups, enable replication, failover, re-protect, disaster failover with force, failback)
- Write `_test.go` for all new exported functions — no untested public API
- No-op driver enables full dev/test/CI without storage infrastructure from Day 1

**TypeScript Testing (Console Plugin):**

- Jest + React Testing Library (`console-plugin/tests/components/`)
- axe-core in Jest for automated accessibility audits on every PR
- Keyboard navigation: manual test checklist per new component

### Code Quality & Style Rules

**Go Code Organization:**

- By layer (not by feature) — matches kubebuilder/sample-apiserver convention
- `cmd/` — entry points only; `pkg/` — public API for external driver authors; `internal/` — non-importable
- `console-plugin/` — fully separate TypeScript project at repo root
- Driver packages: `pkg/drivers/<vendor>/` — external authors import `pkg/`, never `internal/`

**Naming Conventions:**

| Area | Convention | Example |
|---|---|---|
| Go packages | lowercase, single word | `engine`, `drivers`, `storage` |
| Exported types | PascalCase | `DRPlan`, `StorageProvider` |
| Unexported | camelCase | `waveExecutor`, `groupChunker` |
| Interfaces | `-er` suffix where natural | `StorageProvider`, `WaveExecutor` |
| CRD JSON fields | camelCase | `vmSelector`, `maxConcurrentFailovers` |
| Labels/annotations | `soteria.io/<kebab-case>` | `soteria.io/wave`, `soteria.io/plan-name` |
| ScyllaDB tables/columns | snake_case | `kv_store`, `api_group` |
| Prometheus metrics | `soteria_` prefix, snake_case with unit | `soteria_drplan_vms_total`, `soteria_failover_duration_seconds` |
| Event reasons | PascalCase past-tense | `FailoverStarted`, `WaveCompleted` |

**Linting & Formatting:**

- golangci-lint with K8s logging linter (kubebuilder default)
- `make lint` must pass before merge
- `make manifests` to regenerate RBAC/webhook configs after changes
- `hack/verify-codegen.sh` in CI — ensures generated code is up to date

**Documentation:**

- Code comments explain *why*, not *what*
- Event messages: human-readable sentences — `"Failover started for plan erp-full-stack in disaster mode"`
- Error messages: lowercase, no punctuation, descriptive context

### Development Workflow Rules

**Project Initialization:**

- Go skeleton: `kubebuilder init --domain soteria.io --repo github.com/soteria-project/soteria --plugins go/v4`
- Console plugin: clone `openshift/console-plugin-template` into `console-plugin/`
- First priority: ScyllaDB `storage.Interface` prototype — highest risk, gates all other work

**Build & Development Commands:**

- `make run` — local dev with controller-runtime
- `make test` — unit + envtest integration tests
- `make lint` — golangci-lint
- `make manifests` — regenerate RBAC, webhook configs
- `make integration` — integration tests (ScyllaDB testcontainers)
- `make helmchart-test` — redhat-cop pipeline compatibility
- `make dev-cluster` — local OpenShift dev with no-op driver (contributor onboarding)
- `yarn start` / `yarn start-console` — Console plugin dev server

**CI/CD:**

- GitHub Actions reusing `redhat-cop/github-workflows-operators`
- PR gates: `test`, `integration`, `helmchart-test` Makefile targets
- Multi-arch: `linux/amd64,linux/arm64,linux/ppc64le`
- OLM: `operator-sdk generate bundle` + `operator-sdk bundle validate`

**Container Images:**

- Single Go binary (API server + controller) — multi-stage Dockerfile
- Separate Console plugin image (nginx) — `console-plugin/Dockerfile`
- OLM bundle image — `bundle.Dockerfile`

**Implementation Sequence (architecture-driven):**

1. ScyllaDB `storage.Interface` prototype (CDC Watch + generic KV) — gates everything
2. Aggregated API Server with DRPlan/DRExecution/DRGroupStatus types
3. Controller skeleton with leader election + client-go informers
4. StorageProvider interface + no-op driver
5. Workflow engine (wave executor, DRGroup chunking, checkpointing)
6. OLM bundle + CI pipeline
7. Console plugin

**License:** Apache 2.0

### Critical Don't-Miss Rules

**Explicitly Forbidden Anti-Patterns:**

1. **No custom authorization** — Kubernetes RBAC exclusively on `soteria.io` CRDs
2. **No in-memory state across reconcile calls** — use CRD status or ScyllaDB
3. **No direct ScyllaDB from controller** — always through kube-apiserver; storage boundary at `pkg/storage/scylladb/` is absolute
4. **No `context.Background()` in reconcile/library code** — propagate `ctx` from reconcile method
5. **No `log.Fatal` / `os.Exit` in library code** — only in `cmd/` entry points
6. **No hand-editing `zz_generated_*.go`** — regenerate via `hack/update-codegen.sh`; CI verifies with `hack/verify-codegen.sh`
7. **No storing credentials directly** — reference K8s Secrets or Vault; never log/expose credentials in logs, events, metrics, or DRExecution records

**Domain-Specific Safety Rules:**

- Human-triggered only: all failover requires explicit human initiation — no auto-failover, no failure detection (eliminates split-brain)
- Fail-forward: rollback impossible when active DC is down. Failed DRGroups marked `Failed`, engine continues, execution reports `PartiallySucceeded`
- Reject retry if VM is in non-standard state — never attempt failover from unpredictable starting point
- RPO is storage-determined: orchestrator reports estimated RPO but does not enforce targets
- VM pre-existence: VMs exist on both clusters with PVC bindings. Orchestrator promotes volumes and starts VMs — does not create VMs or rebind PVCs
- Homogeneous storage only: Dell-to-Dell, ODF-to-ODF — no cross-vendor replication

**Architectural Boundaries:**

- **Storage:** Only `pkg/storage/scylladb/` talks to ScyllaDB
- **Driver:** `pkg/drivers/interface.go` = contract. Above is driver-agnostic, below is vendor-specific
- **Engine:** `pkg/engine/` knows plans and drivers — does not know ScyllaDB, CDC, or API server internals
- **Console:** `console-plugin/` communicates via `useK8sWatchResource()` only — no Go imports, no shared code

**Checkpointing & Resilience:**

- Per-DRGroup checkpoint: DRExecution status updated after each DRGroup completes
- Pod restart resumes from last checkpoint — at most one in-flight DRGroup lost
- All driver methods must be idempotent — safe to retry after crash

---

## Usage Guidelines

**For AI Agents:**

- Read this file before implementing any code in this project
- Follow ALL rules exactly as documented — when in doubt, prefer the more restrictive option
- Refer to `_bmad-output/planning-artifacts/architecture.md` for detailed architectural decisions and project structure
- Refer to `_bmad-output/planning-artifacts/prd.md` for functional requirements and domain context
- Refer to `_bmad-output/planning-artifacts/ux-design-specification.md` for Console plugin UX patterns

**For Humans:**

- Keep this file lean and focused on agent needs
- Update when technology stack or patterns change
- Review periodically for outdated rules
- Remove rules that become obvious over time

Last Updated: 2026-04-06
