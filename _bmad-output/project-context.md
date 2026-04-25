---
project_name: 'dr-orchestrator'
user_name: 'Raffa'
date: '2026-04-25'
sections_completed: ['technology_stack', 'language_rules', 'framework_rules', 'testing_rules', 'code_quality', 'workflow_rules', 'critical_rules']
status: 'complete'
rule_count: 95
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
| **PatternFly 6** | UI components | Mandated by NFR17 — no other UI libraries. Upstream console-plugin-template pins PF 6; Console 4.22+ drops PF 5 support entirely |
| **Webpack** (module federation) | Console plugin architecture | Standard OCP dynamic plugin pattern |
| **Ginkgo/Gomega** | Go integration + E2E testing | envtest for integration tests |
| **Jest** | TypeScript component testing | Console plugin template default |
| **golangci-lint** | Go linting | Includes K8s logging linter from kubebuilder |
| **cert-manager** | TLS certificate management | mTLS for all ScyllaDB traffic |
| **operator-sdk CLI** | OLM bundle generation only | Standalone tool, not scaffolding — `generate bundle` + `bundle validate` |
| **GitHub Actions** | CI/CD | Reuse redhat-cop/github-workflows-operators |

**External Runtime Dependencies:**
- `kubevirt.io/api` — typed `kubevirtv1.VirtualMachine` access. This is a DR orchestrator for VMs — the kubevirt dependency is fundamental. Register in scheme: `kubevirtv1.AddToScheme(scheme)`. Used for VM discovery (controller-runtime cached client), VM watches (secondary watch in DRPlan controller), volume extraction (typed struct traversal), and admission webhooks (typed decoding).

**Architectural References (not runtime dependencies):**
- `kubernetes/sample-apiserver` — patterns for storage.Interface, API registration, codegen
- `apiserver-builder-alpha` — design inspiration for storage strategy wiring and API registration patterns

## Critical Implementation Rules

### Language-Specific Rules

**Go:**

- Error wrapping: lowercase, no punctuation, wrap with `%w` — `fmt.Errorf("setting volume %s to source: %w", name, err)`
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
- No external UI libraries — PatternFly 6 only, no Material UI, no Chakra
- State management: Console SDK watch hooks — no Redux, no Zustand, no custom state libraries

### Framework-Specific Rules

**Kubernetes Operator / API Server:**

- API group: `soteria.io/v1alpha1` — resources: `drplans`, `drexecutions`, `drgroupstatuses`
- Single binary: API server + controller in one process. Leader election (`ctrl.Options{LeaderElection: true}`) controls workflow engine only — all replicas serve API
- Controller communicates via standard client-go through kube-apiserver proxy — never touches ScyllaDB directly
- **Aggregated API Server admission flow:** kube-apiserver owns webhook admission — it reads the VWC, calls the Soteria webhook service (port 443 → pod 9443), and only proxies to the aggregated API server (port 6443) if validation passes. The aggregated API server stores to ScyllaDB without further validation. Disable `ValidatingAdmissionWebhook` and `MutatingAdmissionWebhook` on the aggregated API server (`--disable-admission-plugins`) to prevent it from re-invoking external webhooks. The in-process controller-runtime webhook handler serves both the VWC calls from kube-apiserver and the aggregated API server's own admission chain
- Reconcile returns: success `ctrl.Result{}, nil` | poll `ctrl.Result{RequeueAfter: d}, nil` | error `ctrl.Result{}, err` | yield after setup writes `ctrl.Result{RequeueAfter: 1*time.Millisecond}, nil`
- No in-memory state across reconcile calls — use CRD status or ScyllaDB
- **ScyllaDB retry policy:** All `RetryOnConflict` calls against ScyllaDB-backed resources must use `engine.ScyllaRetry` (200ms base, factor 2.0, jitter 0.3, 8 steps) — never `retry.DefaultRetry` (10ms/5 steps is too fast for cross-DC propagation)
- **Strategic merge patches:** Prefer `client.MergeFrom(obj.DeepCopy())` + `r.Patch()` over `r.Update()` for metadata/label updates to reduce resourceVersion conflict surface in eventual-consistency environments
- **Site-aware reconciliation:** Controller instances are configured with `--site-name` (required); reconcile ownership is derived from `engine.ReconcileRole(phase, mode, localSite, primary, secondary)` returning Owner/Step0Only/None. Source site runs Step 0 in planned migration; target site runs per-group waves
- **VM readiness gate:** After StartVM, the controller yields and waits for VM watch events. Wave N+1 starts only after all wave N VMs reach Running state (configurable timeout via `DRPlanSpec.VMReadyTimeout`, default 5m)
- CRD status conditions: always `metav1.Condition` — no custom condition types
- CRD JSON fields: camelCase — `waveLabel`, `maxConcurrentFailovers`
- CRD field markers — defaulting, validation, and optionality:
  - Zero-value defaults (empty string, 0, false): no `+kubebuilder:default`, use `omitempty` — Go zero value is the default
  - Non-zero defaults: use `+kubebuilder:default=<value>`, no `omitempty` — field always appears in serialized output
  - Enumerated string fields: add `+kubebuilder:validation:Enum` listing all valid values
  - Numeric ranges / string lengths: use `+kubebuilder:validation:Minimum`/`Maximum` or `MinLength`/`MaxLength`
  - Regex-constrained strings: use `+kubebuilder:validation:Pattern`
  - Mandatory fields without defaults: use `+kubebuilder:validation:Required` — exception: Spec struct fields are implicitly required (no `omitempty`, programmatic validation in `validation.go`)
- Labels/annotations: `soteria.io/<key>` kebab-case — `soteria.io/drplan`, `soteria.io/wave`
- Event reasons: PascalCase past-tense — `FailoverStarted`, `WaveCompleted`, `GroupFailed`
- RBAC: Kubernetes-native only — no custom authorization logic
- DRPlan 8-phase lifecycle: `DRPlan.Status.Phase` holds **only** rest states (`SteadyState`, `FailedOver`, `DRedSteadyState`, `FailedBack`). Transient phases (`FailingOver`, `Reprotecting`, `FailingBack`, `ReprotectingBack`) are derived via `EffectivePhase(restPhase, activeExecMode)`. `DRPlan.Status.ActiveExecution` references the in-progress DRExecution by name (empty when idle). Phase advances to next rest state on successful completion; stays unchanged on failure (self-healing)
- Unified handler model: `FailoverHandler` (config: `GracefulShutdown bool`) implements both failover and failback. Per-group path is always `StopReplication → StartVM`. `ReprotectHandler` implements both reprotect and restore. Direction is encoded in state machine phases, not handler logic

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

- 6-method interface: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, SetSource, StopReplication, GetReplicationStatus
- Replication model uses two engine-driven transitions (NonReplicated → Source, Source → NonReplicated) while the Target role is observable via GetReplicationStatus but not explicitly set by the engine
- Drivers act as reconcilers — check actual storage state before applying changes
- All 6 methods must be idempotent — safe to retry after crash/restart
- Driver selection is implicit from PVC storage class — no StorageProviderConfig CRD
- Registration: `init()` + registry pattern, discovered at startup
- Timeouts: accept `context.Context`, respect cancellation
- Driver packages: `pkg/drivers/<vendor>/` — `noop/`, `fake/`
- All drivers must pass conformance suite at `pkg/drivers/conformance/suite.go`

**React / Console Plugin:**

- Console SDK hooks (`useK8sWatchResource()`) for all data — real-time via K8s watch events
- PatternFly 6 design tokens for DR status colors: success (green), warning (yellow), danger (red), disabled (gray), info (blue)
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
- All drivers must pass conformance suite covering full DR lifecycle (create volume groups, set source/target roles, stop replication, re-protect, disaster failover with force, failback)
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
| CRD JSON fields | camelCase | `waveLabel`, `maxConcurrentFailovers` |
| Labels/annotations | `soteria.io/<kebab-case>` | `soteria.io/drplan`, `soteria.io/wave` |
| ScyllaDB tables/columns | snake_case | `kv_store`, `api_group` |
| Prometheus metrics | `soteria_` prefix, snake_case with unit | `soteria_drplan_vms_total`, `soteria_failover_duration_seconds` |
| Event reasons | PascalCase past-tense | `FailoverStarted`, `WaveCompleted` |

**Linting & Formatting:**

- golangci-lint with K8s logging linter (kubebuilder default)
- `make lint` must pass before merge
- `make manifests` to regenerate RBAC/webhook configs after changes
- `hack/verify-codegen.sh` in CI — ensures generated code is up to date

**Documentation (Tiered Comment Standards — enforced on every story):**

- Code comments explain *why*, not *what* — never narrate obvious logic
- Event messages: human-readable sentences — `"Failover started for plan erp-full-stack in disaster mode"`
- Error messages: lowercase, no punctuation, descriptive context
- **Every story must verify Tier 1/2/3 compliance before marking done** — this is a finalization gate, not optional

*Tier 1 — Package doc comments (mandatory for all `pkg/` and `internal/` packages):*
- Every package under `pkg/` must have a `doc.go` with a 3-5 sentence godoc overview
- Explains the package's purpose, its primary types, and its relationship to the architecture
- Follows Go convention: first sentence is `// Package <name> ...`

*Tier 2 — Architecture block comments (mandatory for complex/non-obvious flows):*
- Files exceeding ~200 lines or implementing non-standard patterns must have a top-of-file block comment explaining the high-level flow
- Complex exported functions (e.g., `watchLoop`, `runSnapshot`, orchestration methods) must have godoc explaining the algorithm, phases, and key invariants
- Target audience: an engineer (or AI agent) encountering this code for the first time

*Tier 3 — Domain 'why' comments (mandatory for business rule enforcement):*
- Code that encodes domain decisions (e.g., which fields trigger cross-DC LWT, append-only semantics, immutability rules) must include a comment explaining the distributed systems or business rationale — not just *what* it does but *why* it matters
- These comments bridge the gap between architecture docs and code

*Leave alone — well-known Kubernetes patterns:*
- Strategy files following `k8s.io/apiserver` registry conventions, standard storage wiring (`NewREST`, `StatusREST`), `main.go` flag parsing, and other idiomatic Kubernetes plumbing do not need additional comments beyond standard godoc on exported symbols

### Development Workflow Rules

**Story Execution Discipline:**

- Task checkboxes in story files (`- [ ]` / `- [x]`) must be updated as work progresses during implementation — a story must never reach "done" status with all task checkboxes still unchecked
- Every story goes through the `bmad-dev-story` workflow — no manual sprint-status edits
- Code reviews use a different LLM than the implementing agent
- Deferred items from code reviews are tracked in the story file and reviewed at the start of the next epic

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
8. **No `retry.DefaultRetry` for ScyllaDB-backed resources** — always use `engine.ScyllaRetry`; DefaultRetry's 10ms/5-step backoff exhausts retries before ScyllaDB propagates writes across DCs
9. **No `client.Update` for metadata/label changes in multi-controller environments** — use `client.MergeFrom` strategic merge patch to reduce conflict surface

**Domain-Specific Safety Rules:**

- Human-triggered only: all failover requires explicit human initiation — no auto-failover, no failure detection (eliminates split-brain)
- Fail-forward: rollback impossible when active DC is down. Failed DRGroups marked `Failed`, engine continues, execution reports `PartiallySucceeded`
- Reject retry if VM is in non-standard state — never attempt failover from unpredictable starting point
- RPO is storage-determined: orchestrator does not track or enforce RPO targets
- VM pre-existence: VMs exist on both clusters with PVC bindings. Orchestrator transitions volumes to Source role and starts VMs — does not create VMs or rebind PVCs
- Homogeneous storage only: Dell-to-Dell, ODF-to-ODF — no cross-vendor replication
- DR phase semantics: failback completes to FailedBack (no replication); reprotect-back (restore) completes to SteadyState with A→B replication

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

Last Updated: 2026-04-25
