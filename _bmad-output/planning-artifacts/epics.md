---
stepsCompleted: ['step-01', 'step-02', 'step-03', 'step-04']
workflowCompleted: true
completedAt: '2026-04-06'
project_name: 'dr-orchestrator'
user_name: 'Raffa'
totalEpics: 16
totalStories: 101
totalFRsCovered: 44
totalNFRsAddressed: 19
totalUXDRsCovered: 20
totalDeferredItems: 21
inputDocuments:
  - _bmad-output/planning-artifacts/prd.md
  - _bmad-output/planning-artifacts/architecture.md
  - _bmad-output/planning-artifacts/ux-design-specification.md
  - _bmad-output/planning-artifacts/product-brief-soteria.md
---

# Soteria - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for Soteria, decomposing the requirements from the PRD, UX Design, Architecture, and Product Brief into implementable stories.

## Requirements Inventory

### Functional Requirements

**DR Plan Management:**
FR1: Platform engineer can create a DRPlan by defining a VM label selector, a wave label key, and a max concurrent failovers parameter
FR2: Platform engineer can view all DRPlans and their current state via kubectl or the OCP Console
FR3: Orchestrator automatically discovers VMs matching a DRPlan's label selector and groups them into waves based on the wave label value
FR4: Orchestrator enforces VM exclusivity — a VM can belong to at most one DRPlan (validated by admission webhook)
FR5: Platform engineer can add a VM to an existing DRPlan by adding the appropriate labels to the VM — no plan editing required
FR6: Platform engineer can configure namespace-level volume consistency for a namespace via annotation, causing all VM disks in that namespace to form a single VolumeGroup
FR7: Orchestrator enforces that all VMs belonging to a DRPlan in a namespace with namespace-level consistency are in the same wave (validated by admission webhook)
FR8: Platform engineer can view the composition of a DRPlan (VMs, waves, volume groups) before execution via pre-flight check

**DR Execution & Workflow:**
FR9: Operator can trigger a planned migration execution for a DRPlan when both datacenters are available — orchestrator gracefully stops origin VMs, waits for final replication sync, promotes target volumes, and starts target VMs wave by wave
FR10: Operator can trigger a disaster recovery execution for a DRPlan — orchestrator force-promotes target volumes and starts target VMs wave by wave, ignoring errors from the origin site
FR11: Orchestrator executes waves sequentially and operations within a wave concurrently, respecting maxConcurrentFailovers by chunking waves into DRGroups
FR12: maxConcurrentFailovers always counts individual VMs regardless of consistency level. When namespace-level consistency is configured, the orchestrator creates DRGroup chunks such that all VMs in the same namespace and same wave are always fully contained in a single chunk. If remaining chunk capacity cannot fit the next namespace group, a new chunk is created. A pre-flight check validates that maxConcurrentFailovers >= largest namespace+wave group
FR13: Orchestrator uses fail-forward error handling — if a DRGroup fails, it is marked Failed, the engine continues with remaining groups, and the execution is reported as PartiallySucceeded
FR14: Operator can manually retry a failed DRGroup if the VM is still in a healthy, known state on the original site
FR15: Orchestrator rejects retry attempts when the starting state is non-standard or unpredictable, requiring manual intervention
FR16: Operator can trigger re-protect after a failover — orchestrator demotes volumes on the old active site (if reachable), initiates resync, and monitors until replication is healthy
FR17: Operator can trigger failback — orchestrator executes the reverse of failover using the same wave-based engine
FR18: All failover operations require explicit human initiation — no automatic failure detection or auto-failover
FR19: Execution mode (planned_migration or disaster) is specified at execution time, not on the DRPlan definition

**Storage Abstraction:**
FR20: Orchestrator interacts with storage backends exclusively through a StorageProvider Go interface with 7 methods: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, SetSource, SetTarget, StopReplication, GetReplicationStatus. The replication model uses three volume roles (NonReplicated, Source, Target) with all transitions routed through NonReplicated
FR21: Orchestrator determines which StorageProvider driver to use by inspecting the storage class of the VMs' PVCs — no explicit storage configuration resource required
FR23: No-op driver implements the full StorageProvider interface but performs no actual storage operations, enabling development, testing, and CI without storage infrastructure
FR24: Storage vendor engineer can implement a new StorageProvider driver by implementing the 7-method Go interface and running the conformance test suite
FR25: Orchestrator supports heterogeneous storage within a single DRPlan — different VMs can use different storage backends, each handled by the appropriate driver

**Cross-Site Shared State:**
FR26: DR resources (DRPlan, DRExecution, DRGroupStatus) are accessible via kubectl on both clusters and return the same data under normal operation
FR27: Each cluster can read and write DR resources locally without cross-datacenter latency
FR28: When one datacenter fails, the surviving cluster continues to operate normally — reading existing plans and writing new execution records
FR29: When a failed datacenter recovers, DR state automatically reconciles without manual intervention
FR30: Concurrent writes from both sites are resolved via last-write-wins, with lightweight transactions for critical state transitions

**Monitoring & Observability:**
FR31: Platform engineer can view replication health status (Healthy/Degraded/Error) for every volume group protected by a DRPlan
FR32: Platform engineer can view estimated RPO (time since last successful replication sync) for each protected volume group
FR33: Orchestrator exposes Prometheus metrics: VMs under DR plan (gauge), failover execution duration (histogram), RPO/replication lag per volume group (gauge), execution success/failure counts (counter)
FR34: Platform engineer can identify unprotected VMs — VMs not covered by any DRPlan

**OCP Console Plugin (v1 Scope):**
FR35: Platform engineer can view a DR Dashboard showing all DRPlans with their cross-cluster status and last execution result, with alert banners for broken/degraded replication
FR36: Platform engineer can view a DRPlan detail view showing wave composition, VM membership, and context-aware action buttons (only valid state transitions enabled)
FR37: Operator can trigger failover from the Console with a pre-flight confirmation dialog showing VM count, estimated RPO, estimated RTO, DR site capacity, and summary of actions
FR38: Operator must type a confirmation keyword (e.g., "FAILOVER") to prevent accidental execution
FR39: Operator can monitor a live execution via a Gantt chart-style progress view showing per-wave, per-DRGroup timeline, elapsed time, estimated remaining time, and inline error display with retry action
FR40: Console shows cross-cluster awareness — a table lists every DRPlan with columns representing the two clusters involved, showing active/passive status and protection status per site

**Audit & Compliance:**
FR41: Every DRPlan execution creates an immutable DRExecution record with per-wave, per-DRGroup, per-step status, timestamps, and error details
FR42: Platform engineer can view the execution history for any DRPlan, including all past executions and their outcomes
FR43: DRExecution records persist across datacenter failures and are available on both clusters via the shared state layer

**Access Control & Security:**
FR44: All authorization uses Kubernetes-native RBAC applied to Soteria's CRDs — separate permissions for plan viewing, plan management, and failover execution
FR45: Storage credentials are referenced from Kubernetes Secrets or HashiCorp Vault — the orchestrator never stores credentials directly

### NonFunctional Requirements

**Reliability:**
NFR1: The orchestrator must resume an in-progress execution from the last known checkpoint after a pod restart — no manual re-trigger required. DRExecution state must be persisted frequently enough that a restart loses at most one in-flight DRGroup operation
NFR2: The orchestrator must run as multiple instances in an active/passive configuration, with the active instance elected via Kubernetes Leases. If the active instance fails, a standby instance acquires the lease and resumes operations
NFR3: Target 99% failover execution success rate across all execution modes. Failures attributable to orchestrator bugs must be exceptional
NFR4: The ScyllaDB-backed Aggregated API Server must remain available on the surviving cluster during a single-datacenter failure with no manual intervention required
NFR5: DRExecution writes during a disaster (when the other DC is down) must succeed locally with no dependency on cross-site connectivity

**Performance:**
NFR6: API response time for kubectl get drplan and Console dashboard queries must be under 2 seconds under normal operation
NFR7: Live execution monitor updates must be visible in the Console within 5 seconds of the underlying state change

**Scalability:**
NFR8: The orchestrator must support clusters with up to 5,000 VMs total
NFR9: The orchestrator must support up to 100 DRPlans per cluster with an average of 50 VMs per plan (5,000 VMs under DR protection)
NFR10: Wave discovery and DRGroup chunking must complete within 10 seconds for a 50-VM plan
NFR11: Multiple DRPlan executions can run concurrently without interference (separate plans operating on disjoint VM sets)

**Security:**
NFR12: All cross-site ScyllaDB replication traffic must be encrypted via TLS. TLS certificates are generated and managed by the cert-manager operator
NFR13: All communication between the extension API server and ScyllaDB must be encrypted via TLS
NFR14: The orchestrator must not log or expose storage credentials in any output — logs, events, metrics, or DRExecution records
NFR15: Admission webhooks must validate all DRPlan mutations to prevent misconfiguration (VM exclusivity violations, namespace-level consistency conflicts, invalid label selectors)

**Integration:**
NFR16: The orchestrator must be compatible with OpenShift 4.x and integrate with OLM for lifecycle management (install, upgrade, uninstall)
NFR17: The OCP Console plugin must use PatternFly components and follow Red Hat Console UI guidelines for consistent user experience
NFR18: Prometheus metrics must follow OpenShift monitoring conventions and be scrapeable by the in-cluster Prometheus stack without additional configuration
NFR19: The StorageProvider interface must be stable enough for external driver development — breaking changes require a new API version with a deprecation period

### Additional Requirements

**Project Initialization (from Architecture):**
- Starter template: kubebuilder v4.13.1 for Go project skeleton (Makefile, controller-runtime, testing, linting, multigroup)
- Aggregated API Server hand-built following kubernetes/sample-apiserver patterns with apiserver-builder-alpha design inspiration
- Console plugin from openshift/console-plugin-template
- API group: `soteria.io/v1alpha1` with resources: drplans, drexecutions, drgroupstatuses
- Single binary: API server + controller in one process; leader election controls workflow engine only, all replicas serve API

**ScyllaDB Storage Backend (from Architecture — highest risk, prototype first):**
- Custom `storage.Interface` implementation for k8s.io/apiserver against ScyllaDB
- CDC-based Watch implementation with initial SELECT snapshot and scylla-cdc-go for stream tracking
- ResourceVersion mapping: CDC Timeuuid → Unix microseconds (int64 → string), monotonic within single DC
- Generic KV schema: `(api_group, resource_type, namespace, name)` → serialized blob — no CQL migrations on field changes
- k8s.io/apiserver cacher wraps storage.Interface for in-memory fan-out to client watches
- In-memory PK deduplication set for snapshot-to-CDC transition overlap window
- ScyllaDB topology: NetworkTopologyStrategy DC1:2, DC2:2 (RF=2 per DC, 4 nodes total)
- Consistency: LOCAL_ONE reads/writes; LWT for critical state transitions only
- ScyllaDB deployment via scylla-operator (OperatorHub-certified)
- mTLS only for ScyllaDB authentication via cert-manager with shared CA across DCs

**Driver Framework (from Architecture):**
- Driver registration via init() + registry pattern, discovered at startup, selected at runtime by PVC storage class
- Typed errors from pkg/drivers/errors.go (ErrVolumeNotFound, ErrReplicationNotReady, ErrInvalidTransition)
- All 7 methods must be idempotent — safe to retry after crash/restart
- Conformance test suite at pkg/drivers/conformance/suite.go — all drivers must pass full DR lifecycle battery
- Fake driver at pkg/drivers/fake/ for unit testing (k8s <package>fake convention)

**Workflow Engine (from Architecture):**
- Per-DRGroup checkpoint: DRExecution status updated after each DRGroup completes
- Pod restart resumes from last checkpoint — at most one in-flight DRGroup lost
- State machine: 4-state DR cycle with validated transitions (SteadyState, FailingOver, FailedOver, Reprotecting, DRedSteadyState, FailingBack)
- Controller communicates via standard client-go through kube-apiserver proxy — never touches ScyllaDB directly
- Re-protect workflow: StopReplication on old active → SetTarget on old active / SetSource on new active → monitor until healthy (storage-only, no waves)

**CI/CD & Packaging (from Architecture):**
- GitHub Actions reusing redhat-cop/github-workflows-operators
- OLM bundle generation via operator-sdk CLI (standalone, not scaffolding)
- Multi-arch container images: linux/amd64, linux/arm64, linux/ppc64le
- Makefile targets: test, integration, helmchart-test for pipeline compatibility
- Single Go binary + separate Console plugin image (nginx) + OLM bundle image
- golangci-lint with K8s logging linter (kubebuilder default)
- codegen: hack/update-codegen.sh (deepcopy-gen, conversion-gen, openapi-gen) and hack/verify-codegen.sh in CI

**Admission Webhooks (from Architecture):**
- DRPlan validator: VM exclusivity, namespace consistency, label validation
- DRExecution validator: state transition validation, pre-flight checks

**Pre-flight Checks (from Architecture):**
- DR site capacity validation
- Replication health verification
- RPO estimate based on last replication sync time
- maxConcurrentFailovers vs largest namespace+wave group validation

### UX Design Requirements

UX-DR1: DR Dashboard implemented as PatternFly Table (composable, compact) with sortable/filterable columns — must scale to 500 DRPlans with default sort by protection status (Error first)
UX-DR2: Persistent Alert Banner system above dashboard table — danger: "N DR Plans running UNPROTECTED" (not dismissible), warning: degraded replication and stale test warnings (not dismissible), info: execution notifications (dismissible)
UX-DR3: Cross-Cluster Status Columns integrated into dashboard table — active/passive indicators per cluster with filled circle (Active + VM count) and open circle (Passive), question mark for Unknown
UX-DR4: Plan Detail Page with four horizontal tabs — Overview (metadata, health, context-aware action buttons), Waves (WaveCompositionTree), History (execution table), Configuration (YAML view)
UX-DR5: Pre-flight Confirmation Modal (PatternFly Modal, large variant ~800px) — structured summary with VM count, estimated RPO (2xl bold), estimated RTO, DR site capacity, duration estimate from history, and confirmation keyword TextInput ("FAILOVER" / "MIGRATE" / "REPROTECT" / "FAILBACK")
UX-DR6: Execution Monitor Phase 1 — PatternFly ProgressStepper for wave-level sequential progress with expandable per-wave DRGroup detail, supporting full execution lifecycle
UX-DR7: ExecutionGanttChart (custom component, Phase 1b) — horizontal Gantt visualization with waves as rows, DRGroups as blocks, real-time progress via useK8sWatchResource, inline error display + retry action, bridge-call readable at 720p screen-share. Uses PatternFly color tokens only
UX-DR8: ReplicationHealthIndicator (custom component) — compact variant for dashboard table cell (icon + label + RPO + freshness in one line) and expanded variant for plan detail (per-volume-group breakdown with health/RPO/freshness)
UX-DR9: WaveCompositionTree (custom component) — PatternFly TreeView with custom node renderers showing waves → DRGroup chunks → VMs with per-VM columns: name, storage backend, consistency level, replication health, RPO
UX-DR10: Status Badge system — PatternFly Label (colored, with icon) for all inline status: phase badges (SteadyState=green, FailedOver=blue, FailingOver=blue+spinner), execution result badges (Succeeded=green, PartiallySucceeded=yellow, Failed=red), replication health indicators
UX-DR11: Execution History Table — PatternFly Table (compact) in History tab with columns: Date, Mode (Planned/Disaster), Result badge, Duration, RPO, Triggered By. Row click navigates to execution detail
UX-DR12: Empty State patterns — "No DR Plans configured" EmptyState with setup guidance and documentation link; "No executions yet" compact EmptyState with planned migration suggestion
UX-DR13: Toast Notification system — AlertGroup with context-appropriate persistence: execution started (info, 8s auto-dismiss), succeeded (success, 15s), partial success (warning, persistent), all with link to plan detail
UX-DR14: Navigation structure — Console left nav "Disaster Recovery" entry → DR Dashboard (default) → Plan Detail (via row click) → Execution Detail (via History tab or active execution). Breadcrumbs on all sub-pages. URL-based routing with preserved table scroll/filter state
UX-DR15: Dashboard Toolbar — text search for plan name, dropdown multi-select filters for Phase, Active On, Protected, Last Execution. Additive AND logic, filter chips, "Clear all", result count display, URL-reflected filters
UX-DR16: Accessibility requirements — color-independent status (icon + text label alongside color on all indicators), keyboard-accessible entire failover flow, ARIA live regions for execution status changes, screen reader announcements for confirmation input, axe-core in Jest for every PR
UX-DR17: Screen-share optimization for execution monitor — minimum 14px font for all text, 18px+ for critical numbers (RPO, time, VM count), high contrast between states, no hover-only information, monospace for elapsed/remaining time, subtle animation only
UX-DR18: DR-specific semantic color mapping — all using PatternFly CSS custom properties exclusively for automatic dark mode support: SteadyState/DRedSteadyState=success-green, FailedOver=info-blue, in-progress states=info-blue+spinner, Healthy=success-green, Degraded=warning-yellow, Error=danger-red, Unknown=disabled-gray
UX-DR19: Context-aware action buttons — only valid state transitions shown (not disabled, hidden entirely): SteadyState→Failover(danger)/PlannedMigration(primary), FailedOver→Reprotect(primary), DRedSteadyState→Failback(primary). Danger variant reserved exclusively for disaster failover
UX-DR20: Responsive design — desktop-only, optimized for 1920px+ (primary: NOC stations), functional at 1440px (laptops), minimum supported 1024px (some columns hidden). No mobile/tablet support

### Deferred Requirements (Post-v1 / Phase 2+)

**From PRD Phase 2:**
DEF-1: Test execution mode — non-disruptive DR testing with volume cloning, isolated network, automatic teardown. Enables validation without production impact
DEF-2: OCP Console health monitoring — dedicated replication health view with RPO time-series graphs and alert thresholds
DEF-3: OCP Console plan creation wizard — step-by-step: select VMs → review waves → configure throttling → review & create
DEF-4: Additional storage drivers — Dell PowerStore, Pure Storage, NetApp (Trident Protect)
DEF-5: Hook framework — pre/post hooks at plan, wave, and VM level (Kubernetes Jobs, QEMU guest agent commands, webhooks)
DEF-6: Post-startup health gate — optional readiness check after VM startup before proceeding to next wave

**From PRD Phase 3 / Vision:**
DEF-7: Automated DR test scheduling with compliance reporting
DEF-8: Multi-application orchestration with cross-plan dependencies
DEF-9: SRM feature parity — full protection group management
DEF-10: Community-contributed storage driver ecosystem
DEF-11: Extension to non-VM workloads (container/pod DR)
DEF-12: Broader KubeVirt community targeting beyond OpenShift

**From Product Brief Vision:**
DEF-13: Production deployment case studies and public adopters list
DEF-14: Storage vendor co-maintainers program

**From UX Spec Phase 2:**
DEF-15: RPO Time-Series Chart — replication health monitoring with historical RPO graph
DEF-16: Unprotected VM List — dedicated view for VMs not covered by any DRPlan
DEF-17: Dashboard bulk operations (multi-plan actions)

**From Architecture Deferred:**
DEF-18: Helm chart packaging (in addition to OLM)
DEF-19: ScyllaDB backup/restore strategy
DEF-20: CDC-to-polling fallback path (if CDC proves problematic)
DEF-21: Hook extension points — empty hook interfaces in executor (preWave, postWave, preVM, postVM callbacks)

### FR Coverage Map

FR1: Epic 1 — Create DRPlan CRD with label selector, wave label, maxConcurrentFailovers
FR2: Epic 1 — View all DRPlans and their state via kubectl or Console
FR3: Epic 2 — Auto-discover VMs matching label selector and group into waves
FR4: Epic 2 — VM exclusivity enforcement via admission webhook
FR5: Epic 2 — Add VM to existing plan by adding labels (no plan editing)
FR6: Epic 2 — Namespace-level volume consistency via annotation
FR7: Epic 2 — Namespace consistency + same-wave enforcement via admission webhook
FR8: Epic 2 — Pre-flight check showing plan composition (VMs, waves, volume groups)
FR9: Epic 4 — Planned migration execution (graceful stop → final sync → promote → start)
FR10: Epic 4 — Disaster recovery execution (force-promote → start, ignore origin errors)
FR11: Epic 4 — Wave-sequential, intra-wave-concurrent execution with DRGroup chunking
FR12: Epic 4 — maxConcurrentFailovers VM counting with namespace group constraints
FR13: Epic 4 — Fail-forward error handling with PartiallySucceeded reporting
FR14: Epic 4 — Manual retry of failed DRGroup when preconditions met
FR15: Epic 4 — Reject retry when starting state is non-standard
FR16: Epic 4 — Re-protect workflow (demote → resync → monitor until healthy)
FR17: Epic 4 — Failback as reverse failover using same wave-based engine
FR18: Epic 4 — Human-triggered only — no auto-failover
FR19: Epic 4 — Execution mode specified at runtime, not on DRPlan
FR20: Epic 3 — StorageProvider Go interface with 7 methods (role-based replication model)
FR21: Epic 3 — Implicit driver selection from PVC storage class
FR23: Epic 3 — No-op driver for dev/test/CI
FR24: Epic 3 — Driver contribution path via interface + conformance suite
FR25: Epic 3 — Heterogeneous storage within single DRPlan
FR26: Epic 1 — DR resources accessible via kubectl on both clusters
FR27: Epic 1 — Local read/write without cross-datacenter latency
FR28: Epic 1 — Surviving cluster operates normally during DC failure
FR29: Epic 1 — Automatic state reconciliation on DC recovery
FR30: Epic 1 — Last-write-wins with LWT for critical state transitions
FR31: Epic 5 — Replication health status per volume group (Healthy/Degraded/Error)
FR32: Epic 5 — Estimated RPO per protected volume group
FR33: Epic 5 — Prometheus metrics (VMs gauge, failover histogram, RPO gauge, counters)
FR34: Epic 5 — Identify unprotected VMs
FR35: Epic 6 — DR Dashboard table with cross-cluster status and alert banners
FR36: Epic 6 — DRPlan detail view with waves, VMs, context-aware actions
FR37: Epic 7 — Failover trigger with pre-flight confirmation dialog
FR38: Epic 7 — Confirmation keyword to prevent accidental execution
FR39: Epic 7 — Live execution Gantt-style progress with inline retry
FR40: Epic 6 — Cross-cluster awareness table (active/passive per cluster, protection status)
FR41: Epic 5 — Immutable DRExecution audit record per execution
FR42: Epic 5 — Execution history view per DRPlan
FR43: Epic 5 — DRExecution records persist across DC failures via shared state
FR44: Epic 2 — Kubernetes-native RBAC on Soteria CRDs
FR45: Epic 2 — Storage credentials from K8s Secrets or Vault (never stored directly)

## Epic List

### Epic 1: Project Foundation & Cross-Site DR State
Platform engineers can create, view, and manage DRPlan resources via kubectl on both OpenShift clusters. Both clusters see identical DR state via the ScyllaDB-backed Aggregated API Server. State survives single-datacenter failure and auto-reconciles on recovery. The project is installable via OLM.
**FRs covered:** FR1, FR2, FR26, FR27, FR28, FR29, FR30

### Epic 2: DR Plan Discovery, Validation & Access Control
VMs self-organize into DR plans via Kubernetes labels, waves auto-form from wave label values, namespace-level consistency is enforced, admission webhooks prevent misconfiguration, pre-flight checks validate plan composition, RBAC controls access, and storage credentials are handled securely.
**FRs covered:** FR3, FR4, FR5, FR6, FR7, FR8, FR44, FR45

### Epic 2b: VM-to-DRPlan Label Convention Refactoring
Replace the `vmSelector` label-selector approach with a convention-based `soteria.io/drplan: <planName>` label on VMs. VMs declare their plan membership explicitly via a single label, structurally enforcing one-VM-one-plan exclusivity through Kubernetes label semantics. Admission webhooks are simplified — the O(plans x VMs) cross-check is eliminated. Discovery becomes an exact-match label query.
**FRs refined:** FR1, FR3, FR4, FR5

### Epic 3: Storage Driver Framework & Reference Implementations
Storage vendor engineers can implement and validate new drivers using the 7-method Go interface and conformance test suite. The replication model uses three volume roles (NonReplicated, Source, Target) with all transitions routed through NonReplicated. The no-op driver enables full dev/CI without real storage. Driver selection is automatic from PVC storage classes.
**FRs covered:** FR20, FR21, FR23, FR24, FR25

### Epic 4: DR Workflow Engine — Full Lifecycle
Operators can execute the complete DR lifecycle through 4 rest states and 8 phases: failover (planned migration RPO=0 or disaster RPO>0), re-protect, failback, and restore. Failover and failback share a single FailoverHandler. Re-protect and restore share a single ReprotectHandler.
**FRs covered:** FR9, FR10, FR11, FR12, FR13, FR14, FR15, FR16, FR17, FR18, FR19

### Epic 5: Monitoring, Observability & Audit Trail
Platform engineers can view replication health and RPO per volume group, identify unprotected VMs, access Prometheus metrics, and use immutable DRExecution audit records for compliance evidence.
**FRs covered:** FR31, FR32, FR33, FR34, FR41, FR42, FR43

### Epic 6: OCP Console — Dashboard & Plan Management
The OCP Console provides a sortable/filterable DR Dashboard table (500+ plans), persistent alert banners for protection status, plan detail pages with a DR lifecycle state machine overview, wave composition trees, execution history, and configuration views.
**FRs covered:** FR35, FR36, FR40

### Epic 7: OCP Console — Execution & DR Operations
Operators can trigger failover, planned migration, re-protect, and failback from the Console with pre-flight confirmation dialogs, safety keyword input, live execution monitoring, inline retry of failed groups, and bridge-call-ready summaries.
**FRs covered:** FR37, FR38, FR39

---

## Epic 1: Project Foundation & Cross-Site DR State

Platform engineers can create, view, and manage DRPlan resources via kubectl on both OpenShift clusters. Both clusters see identical DR state via the ScyllaDB-backed Aggregated API Server. State survives single-datacenter failure and auto-reconciles on recovery. The project is installable via OLM.

### Story 1.1: Project Initialization & API Type Definitions

As a developer,
I want the project scaffolded with kubebuilder and core API types defined,
So that all subsequent development has a consistent foundation with build tooling, linting, and codegen.

**Acceptance Criteria:**

**Given** an empty repository
**When** the project is initialized with `kubebuilder init --domain soteria.io --repo github.com/soteria-project/soteria --plugins go/v4`
**Then** the project compiles with `make build`, the Makefile includes targets for build, test, lint, manifests, and docker-build
**And** `.golangci.yml` is configured with the K8s logging linter

**Given** the initialized project
**When** API types are defined in `pkg/apis/soteria.io/v1alpha1/types.go`
**Then** DRPlan, DRExecution, and DRGroupStatus structs exist with spec and status substructures
**And** DRPlan.Spec includes `vmSelector` (LabelSelector), `waveLabel` (string), and `maxConcurrentFailovers` (int)
**And** DRExecution.Spec includes `planName` (string) and `mode` (enum: planned_migration, disaster)
**And** DRExecution.Status includes `result` (enum: Succeeded, PartiallySucceeded, Failed), `waves[]` with per-group status, `startTime`, and `completionTime`
**And** DRGroupStatus includes per-group state tracking fields
**And** all status conditions use `metav1.Condition`
**And** CRD JSON tags use camelCase per Kubernetes convention

**Given** the type definitions
**When** `hack/update-codegen.sh` is run (deepcopy-gen, openapi-gen)
**Then** `zz_generated_deepcopy.go` files are generated without errors
**And** `hack/verify-codegen.sh` passes confirming generated code is up to date

**Given** the project structure
**When** reviewing the directory layout
**Then** it follows the architecture: `cmd/soteria/`, `pkg/apis/`, `pkg/apiserver/`, `pkg/registry/`, `pkg/storage/`, `pkg/drivers/`, `pkg/engine/`, `pkg/controller/`, `pkg/admission/`, `pkg/metrics/`, `internal/`, `console-plugin/`, `config/`, `hack/`, `test/`, `bundle/`
**And** a multi-stage Dockerfile exists for the single Go binary
**And** `bundle.Dockerfile` exists for the OLM bundle image

### Story 1.2: ScyllaDB Connection & Generic KV Schema

As a developer,
I want a ScyllaDB client with mTLS support and the generic KV store table with CDC enabled,
So that the storage backend is ready for storage.Interface implementation.

**Acceptance Criteria:**

**Given** a running ScyllaDB cluster (or testcontainers instance)
**When** the ScyllaDB client in `pkg/storage/scylladb/client.go` is initialized with mTLS certificates
**Then** the client establishes a connection authenticated via client certificates from cert-manager
**And** the connection is encrypted with TLS
**And** no password-based authentication is used

**Given** an established ScyllaDB connection
**When** the schema initializer in `pkg/storage/scylladb/schema.go` runs
**Then** a `kv_store` table is created with columns: `api_group` (text), `resource_type` (text), `namespace` (text), `name` (text), `value` (blob), `resource_version` (timeuuid)
**And** the primary key is `(api_group, resource_type, namespace, name)`
**And** CDC is enabled on the `kv_store` table for change stream consumption
**And** table and column names use snake_case

**Given** a ScyllaDB client
**When** the connection is lost and re-established
**Then** the client reconnects automatically with exponential backoff
**And** connection health is reportable via a health check method

**Given** the schema module
**When** integration tests run against a real ScyllaDB instance (testcontainers)
**Then** all tests pass confirming table creation, CDC enablement, and connection lifecycle

### Story 1.3: ScyllaDB storage.Interface — CRUD Operations

As a platform engineer,
I want to create, read, update, and delete DR resources in ScyllaDB via the Kubernetes API storage interface,
So that DR state is persisted reliably with conflict detection and proper resource versioning.

**Acceptance Criteria:**

**Given** the generic KV schema from Story 1.2
**When** `storage.Interface.Create()` is called in `pkg/storage/scylladb/store.go` with a DRPlan object
**Then** the object is serialized via `pkg/storage/scylladb/codec.go` and stored in the `kv_store` table
**And** a new Timeuuid is assigned as the resource version
**And** the returned object includes the assigned `resourceVersion`
**And** creating a resource with an existing key returns an `AlreadyExists` error

**Given** a stored DRPlan resource
**When** `storage.Interface.Get()` is called with the resource key
**Then** the object is deserialized from the blob and returned with the correct `resourceVersion`
**And** requesting a non-existent key returns a `NotFound` error

**Given** stored resources across multiple API groups and resource types
**When** `storage.Interface.GetList()` is called with a resource type prefix
**Then** all matching resources are returned as a list with a collective `resourceVersion`
**And** label selectors filter results correctly
**And** pagination via `continue` tokens works correctly

**Given** a stored resource with a known `resourceVersion`
**When** `storage.Interface.Update()` is called with the matching `resourceVersion`
**Then** the resource is updated and a new `resourceVersion` (Timeuuid) is assigned
**And** if the provided `resourceVersion` does not match the stored version, a `Conflict` error is returned

**Given** a stored resource
**When** `storage.Interface.Delete()` is called
**Then** the resource is removed from the `kv_store` table
**And** deleting a non-existent resource returns a `NotFound` error

**Given** the versioner in `pkg/storage/scylladb/versioner.go`
**When** a Timeuuid is converted to a resourceVersion string
**Then** the conversion produces Unix microseconds as an int64 formatted as a string
**And** the conversion is reversible (string → int64 → Timeuuid range)
**And** resource versions are monotonically increasing within a single DC

**Given** CRUD operations
**When** integration tests run against ScyllaDB (testcontainers)
**Then** all operations pass for DRPlan, DRExecution, and DRGroupStatus resource types

### Story 1.4: ScyllaDB storage.Interface — Watch via CDC

As a platform engineer,
I want real-time notifications when DR resources change,
So that controllers and Console clients receive updates within seconds via standard Kubernetes watch semantics.

**Acceptance Criteria:**

**Given** stored resources in the KV table
**When** `storage.Interface.Watch()` is called in `pkg/storage/scylladb/watch.go` with `resourceVersion=0`
**Then** an initial snapshot is delivered via SELECT of all matching resources
**And** subsequent changes are delivered via the ScyllaDB CDC stream using `scylla-cdc-go`
**And** the transition from snapshot to CDC is handled via an in-memory primary key deduplication set that filters duplicates during the overlap window

**Given** an active watch
**When** a resource is created, updated, or deleted
**Then** the watch receives an ADDED, MODIFIED, or DELETED event respectively
**And** each event includes the full resource object with the current `resourceVersion`
**And** events are delivered within 5 seconds of the underlying change (NFR7)

**Given** a watch with a specific `resourceVersion` (resume from checkpoint)
**When** the watch is established
**Then** only changes after that `resourceVersion` are delivered (no initial snapshot)
**And** the CDC stream is consumed from the Timeuuid corresponding to the given resourceVersion

**Given** the watch implementation
**When** integrated with `k8s.io/apiserver`'s cacher layer
**Then** the cacher wraps the storage.Interface watch as a single CDC consumer
**And** the cacher provides in-memory fan-out to multiple client watches
**And** API response times for list operations are under 2 seconds (NFR6) served from the cache

**Given** the watch implementation
**When** integration tests run against ScyllaDB (testcontainers)
**Then** watch events are received for all CRUD operations
**And** snapshot-to-CDC deduplication produces no duplicate events
**And** watch resume from a specific resourceVersion delivers only subsequent changes

### Story 1.5: Aggregated API Server & API Registration

As a platform engineer,
I want to interact with DRPlan, DRExecution, and DRGroupStatus resources via kubectl through the kube-apiserver aggregation layer,
So that DR resources feel like native Kubernetes resources with standard CRUD, watch, and API discovery.

**Acceptance Criteria:**

**Given** the storage.Interface implementation from Stories 1.3–1.4
**When** the extension API server is configured in `pkg/apiserver/apiserver.go`
**Then** the `soteria.io/v1alpha1` API group is registered with the kube-apiserver aggregation layer
**And** `kubectl api-resources` lists `drplans.soteria.io`, `drexecutions.soteria.io`, and `drgroupstatuses.soteria.io`
**And** `kubectl explain drplan` returns the OpenAPI schema

**Given** the registered API group
**When** registry wiring in `pkg/registry/` connects resource types to storage
**Then** each resource type (DRPlan, DRExecution, DRGroupStatus) has a strategy file defining create/update/delete validation
**And** DRExecution enforces append-only semantics (immutable after completion)
**And** status and spec subresources are served separately

**Given** a running extension API server with kube-apiserver proxy
**When** `kubectl create -f drplan.yaml` is executed
**Then** the DRPlan is created in ScyllaDB via the storage.Interface
**And** `kubectl get drplans` returns the created plan with correct status
**And** `kubectl get drplan <name> -o yaml` returns the full resource with metadata, spec, and status
**And** `kubectl delete drplan <name>` removes the resource

**Given** the aggregated API server
**When** the server starts as part of the single binary (`cmd/soteria/main.go`)
**Then** the binary runs both the API server and controller-runtime manager in one process
**And** leader election is configured via `ctrl.Options{LeaderElection: true}` controlling the workflow engine only
**And** all replicas serve API requests (active/active for reads)

**Given** the APIService configuration in `config/apiservice/`
**When** the APIService resource is applied to the cluster
**Then** kube-apiserver proxies all `soteria.io` requests to the extension API server
**And** Kubernetes RBAC is enforced on all proxied requests

### Story 1.6: Cross-Site State Replication & Resilience

As a platform engineer,
I want both clusters to see identical DR state that survives a single-datacenter failure and auto-reconciles on recovery,
So that DR operations are available from either cluster at all times.

**Acceptance Criteria:**

**Given** ScyllaDB deployed on two OpenShift clusters with `config/scylladb/` reference manifests
**When** the ScyllaCluster CR is configured with NetworkTopologyStrategy
**Then** replication factor is DC1:2, DC2:2 (RF=2 per DC, 4 nodes total)
**And** the scylla-operator manages the ScyllaDB lifecycle

**Given** the two-DC ScyllaDB cluster
**When** a DRPlan is created via kubectl on Cluster 1
**Then** `kubectl get drplan` on Cluster 2 returns the same resource within the async replication window
**And** both clusters serve the resource via LOCAL_ONE consistency with no cross-DC latency on reads or writes (FR27)

**Given** a healthy two-DC deployment
**When** DC1 becomes completely unreachable (simulated network partition)
**Then** DC2's Aggregated API Server continues to serve all DR resources normally (FR28, NFR4)
**And** new DRExecution records can be created on DC2 with no dependency on DC1 connectivity (FR28, NFR5)
**And** no errors or degraded behavior occurs on the surviving cluster

**Given** DC1 has been down and DC2 has received new writes
**When** DC1 recovers and ScyllaDB nodes rejoin the cluster
**Then** ScyllaDB's anti-entropy repair automatically synchronizes state (FR29)
**And** after reconciliation, `kubectl get drplans` on both clusters returns identical results
**And** no manual intervention is required for state reconciliation

**Given** concurrent writes to the same resource from both DCs
**When** a conflict occurs on a non-critical field
**Then** last-write-wins resolution applies (FR30)

**Given** a critical state transition (e.g., DRPlan phase change)
**When** concurrent writes to the same state field occur
**Then** lightweight transactions (LWT/CAS) prevent conflicting state transitions (FR30)

**Given** cert-manager deployed on both clusters
**When** TLS certificates are configured in `config/certmanager/`
**Then** all ScyllaDB internode replication traffic is encrypted via TLS (NFR12)
**And** all API server to ScyllaDB communication is encrypted via mTLS (NFR13)
**And** certificates share a common CA across both DCs

### Story 1.7: CI Pipeline & OLM Packaging

As a platform engineer,
I want to install Soteria from OperatorHub via OLM, and as a contributor, I want automated CI to validate my changes,
So that installation is standard and contributions are verified automatically.

**Acceptance Criteria:**

**Given** the project repository
**When** a pull request is opened
**Then** GitHub Actions runs the PR pipeline reusing `redhat-cop/github-workflows-operators`
**And** the pipeline executes `make test` (unit + envtest), `make lint` (golangci-lint), and `hack/verify-codegen.sh`
**And** the pipeline builds multi-arch container images for `linux/amd64`, `linux/arm64`, `linux/ppc64le`
**And** pipeline failures block merge

**Given** the operator binary and Console plugin images
**When** `operator-sdk generate bundle` is run (standalone CLI, not scaffolding)
**Then** an OLM bundle is generated in `bundle/` with a valid ClusterServiceVersion
**And** `operator-sdk bundle validate` passes with no errors
**And** the CSV declares ScyllaDB and cert-manager as prerequisites
**And** the APIService registration is included in bundle manifests

**Given** a generated OLM bundle
**When** the bundle is published to an OperatorHub catalog
**Then** a platform engineer can install Soteria from the OCP OperatorHub UI
**And** OLM manages the operator lifecycle (install, upgrade, uninstall) per NFR16

**Given** the Makefile
**When** reviewing available targets
**Then** `make test`, `make integration`, `make helmchart-test`, `make lint`, `make manifests`, `make run`, and `make dev-cluster` targets exist
**And** `make integration` runs ScyllaDB integration tests via testcontainers
**And** `make helmchart-test` is compatible with the redhat-cop pipeline

**Given** the release pipeline
**When** a release tag is pushed
**Then** multi-arch container images are built and published
**And** the OLM bundle image is built from `bundle.Dockerfile` and published

---

## Epic 2: DR Plan Discovery, Validation & Access Control

VMs self-organize into DR plans via Kubernetes labels, waves auto-form from wave label values, namespace-level consistency is enforced, admission webhooks prevent misconfiguration, pre-flight checks validate plan composition, RBAC controls access, and storage credentials are handled securely.

### Story 2.1: DRPlan Controller & VM Auto-Discovery

As a platform engineer,
I want the orchestrator to automatically discover VMs matching my DRPlan's label selector and organize them into waves,
So that adding VMs to DR protection requires only Kubernetes labels — no plan editing.

**Acceptance Criteria:**

**Given** a DRPlan with `vmSelector.matchLabels: {app.kubernetes.io/part-of: erp-system}` and `waveLabel: soteria.io/wave`
**When** the DRPlan controller in `pkg/controller/drplan/reconciler.go` reconciles
**Then** the controller discovers all VMs with the matching label using client-go via kube-apiserver (never direct ScyllaDB)
**And** VMs are grouped into waves based on their `soteria.io/wave` label value (e.g., "1", "2", "3")
**And** DRPlan `.status.waves[]` is updated with discovered VM names, namespaces, and wave membership
**And** DRPlan `.status.conditions` includes a `Ready` condition reflecting discovery success

**Given** a DRPlan with discovered VMs
**When** a platform engineer adds a new VM with matching labels (FR5)
**Then** the controller re-discovers on the next reconcile cycle and updates `.status.waves[]` to include the new VM
**And** no manual DRPlan editing is required

**Given** a DRPlan with discovered VMs
**When** a VM's wave label is changed (e.g., from "1" to "2")
**Then** the controller moves the VM to the new wave in `.status.waves[]` on the next reconcile

**Given** a DRPlan with `vmSelector` matching 50 VMs
**When** VM discovery and wave grouping executes
**Then** the operation completes within 10 seconds (NFR10)

**Given** the discovery engine in `pkg/engine/discovery.go`
**When** unit tests run
**Then** wave grouping is verified with table-driven tests covering: single wave, multiple waves, VMs without wave labels, empty selector results

### Story 2.2: Namespace-Level Volume Consistency

As a platform engineer,
I want to configure namespace-level volume consistency so that all VM disks in a namespace form a single VolumeGroup,
So that I can ensure crash-consistent snapshots across related VMs sharing a namespace.

**Acceptance Criteria:**

**Given** a namespace annotated with `soteria.io/consistency-level: namespace`
**When** VMs in that namespace are discovered by a DRPlan controller
**Then** all VM disks in that namespace are grouped into a single VolumeGroup (FR6)
**And** the VolumeGroup is tracked in `.status.waves[].groups[]` with consistency level indicated

**Given** a namespace with namespace-level consistency and VMs belonging to a DRPlan
**When** VMs in that namespace have different wave labels
**Then** the controller detects the conflict and sets a `Ready=False` condition with a message identifying the mismatched VMs
**And** the DRPlan is not considered valid for execution until the conflict is resolved (FR7)

**Given** a valid DRPlan with namespace-consistent VMs all in the same wave
**When** DRGroup chunking is previewed
**Then** all VMs in the same namespace+wave are always contained in a single DRGroup chunk — never split across chunks (FR12 partial)
**And** if the namespace group size exceeds `maxConcurrentFailovers`, the plan reports a validation error (FR12 partial)

**Given** VMs in a namespace without the consistency annotation
**When** discovered by a DRPlan controller
**Then** each VM's disks form an individual VolumeGroup (VM-level consistency is the default)

### Story 2.3: Admission Webhooks — DRPlan Validation

As a platform engineer,
I want the orchestrator to reject misconfigured DRPlan mutations at admission time,
So that VM exclusivity violations, namespace consistency conflicts, and invalid label selectors are caught before they cause problems.

**Acceptance Criteria:**

**Given** an existing DRPlan selecting VMs with label `app=erp`
**When** a second DRPlan is created with a `vmSelector` that would also match any of the same VMs
**Then** the admission webhook in `pkg/admission/drplan_validator.go` rejects the creation with a clear error: "VM <name> already belongs to DRPlan <existing-plan>" (FR4)

**Given** a DRPlan being created or updated
**When** the `vmSelector` contains an invalid label selector expression
**Then** the admission webhook rejects the mutation with a descriptive validation error (NFR15)

**Given** a namespace with `soteria.io/consistency-level: namespace` annotation
**When** a DRPlan is created or updated that would place VMs from that namespace in different waves
**Then** the admission webhook rejects the mutation with an error identifying the conflicting VMs and waves (FR7)

**Given** a DRPlan with `maxConcurrentFailovers: 4`
**When** a namespace+wave group contains 6 VMs (exceeding maxConcurrentFailovers)
**Then** the admission webhook rejects the mutation with an error: "maxConcurrentFailovers (4) is less than namespace+wave group size (6) for namespace <ns> wave <w>" (FR12 partial)

**Given** valid DRPlan creation or update
**When** no exclusivity, consistency, or selector violations exist
**Then** the admission webhook allows the mutation

**Given** the webhook configuration
**When** deployed via `config/webhook/`
**Then** the webhook intercepts CREATE and UPDATE operations on DRPlan resources
**And** webhook TLS certificates are managed by cert-manager

### Story 2.4: Pre-flight Plan Composition Check

As a platform engineer,
I want to view the full composition of my DRPlan before execution,
So that I can verify the plan matches my expectations and throttling constraints are valid.

**Acceptance Criteria:**

**Given** a valid DRPlan with discovered VMs and waves
**When** a pre-flight check is requested (via kubectl subresource or internal API in `internal/preflight/checks.go`)
**Then** the check returns a structured report showing:
- Total VM count per wave
- Wave ordering and VM membership
- Volume groups per VM (VM-level or namespace-level consistency)
- Storage backend per VM (derived from PVC storage class)
- DRGroup chunking preview based on `maxConcurrentFailovers`
**And** the report matches FR8 requirements

**Given** a pre-flight check report
**When** `maxConcurrentFailovers` is sufficient for all namespace+wave groups
**Then** the DRGroup chunking preview shows how VMs would be partitioned into chunks within each wave
**And** namespace-consistent VMs are shown as indivisible units within their chunk

**Given** a pre-flight check report
**When** any validation issue exists (e.g., VMs with broken replication, namespace group exceeding throttle)
**Then** the report includes warnings with specific details and affected resource names

**Given** the pre-flight check
**When** invoked via `kubectl get drplan <name> -o jsonpath='{.status.preflight}'` or a dedicated subresource
**Then** the composition data is accessible without triggering execution

**Given** the pre-flight module
**When** unit tests run
**Then** chunking preview is verified with table-driven tests: single chunk, multiple chunks, namespace groups that force new chunks, edge case where namespace group exactly equals maxConcurrentFailovers

### Story 2.5: RBAC & Secure Credential Handling

As a platform administrator,
I want Kubernetes-native RBAC on all Soteria CRDs with granular permissions and secure credential handling,
So that access is properly controlled and no credentials are stored or exposed by the orchestrator.

**Acceptance Criteria:**

**Given** the RBAC manifests in `config/rbac/`
**When** ClusterRoles are defined for Soteria resources
**Then** a `soteria-viewer` role grants `get`, `list`, `watch` on DRPlan, DRExecution, and DRGroupStatus (read-only users)
**And** a `soteria-editor` role adds `create`, `update`, `patch` on DRPlan (plan authors)
**And** a `soteria-operator` role adds `create` on DRExecution (failover operators)
**And** role granularity follows CRD verb semantics per FR44

**Given** the RBAC configuration
**When** a user without `soteria-operator` role attempts to create a DRExecution
**Then** the request is rejected by Kubernetes RBAC with a `403 Forbidden` response
**And** no custom authorization logic exists — Kubernetes RBAC is the only access control mechanism (FR44)

**Given** the storage driver framework
**When** a driver needs storage credentials (e.g., for a CSI-Addons endpoint)
**Then** credentials are read from Kubernetes Secrets referenced by the driver configuration or discovered via PVC storage class annotations
**And** the orchestrator never stores credentials in its own resources, config maps, or local state (FR45)

**Given** any Soteria operation
**When** logs, events, metrics, or DRExecution records are written
**Then** no storage credentials appear in any output (NFR14)
**And** the credential sanitization is verified by unit tests that assert no secret values in formatted log/event/metric strings

**Given** HashiCorp Vault as an alternative credential source
**When** a driver is configured to use Vault
**Then** credentials are fetched via the Vault Kubernetes auth method at runtime
**And** credentials are not cached beyond the operation lifetime

---

## Epic 2b: VM-to-DRPlan Label Convention Refactoring

Replace the `vmSelector` label-selector approach with a convention-based `soteria.io/drplan: <planName>` label on VMs. VMs declare their plan membership explicitly via a single label, structurally enforcing one-VM-one-plan exclusivity through Kubernetes label semantics. Admission webhooks are simplified — the O(plans × VMs) cross-check is eliminated. Discovery becomes an exact-match label query.

**Motivation:**
- **Structural exclusivity:** A Kubernetes label key can only have one value per resource, so a VM with `soteria.io/drplan: plan-a` physically cannot also belong to `plan-b`. This eliminates FR4 as a code concern.
- **Performance:** The current `ExclusivityChecker.FindMatchingPlans` lists every DRPlan, parses each `vmSelector`, and tests `sel.Matches(vmLabels)` for every VM — O(plans × VMs) on every admission. The new model requires no cross-resource check.
- **Simplicity:** The `DRPlanSpec.VMSelector` field, `exclusivity.go`, and the VM discovery call in the DRPlan webhook are all removed. The controller's `mapVMToDRPlans` becomes O(1).

**FRs refined:** FR1, FR3, FR4, FR5

### Story 2b.1: Label Convention — API, Discovery & Controller Refactoring

As a platform engineer,
I want VMs to declare their DRPlan membership via the `soteria.io/drplan` label,
So that plan membership is explicit, unambiguous, and structurally limited to one plan per VM.

**Acceptance Criteria:**

**Given** the `DRPlanSpec` in `pkg/apis/soteria.io/v1alpha1/types.go`
**When** the label convention is adopted
**Then** the `VMSelector metav1.LabelSelector` field is removed from `DRPlanSpec`
**And** a new exported constant `DRPlanLabel = "soteria.io/drplan"` is added to the API package
**And** `make manifests` and `make generate` succeed with the updated types

**Given** the `TypedVMDiscoverer` in `pkg/engine/discovery.go`
**When** `DiscoverVMs` is called
**Then** the method signature changes from `DiscoverVMs(ctx, metav1.LabelSelector)` to `DiscoverVMs(ctx, planName string)`
**And** VMs are listed using an exact label selector: `soteria.io/drplan=<planName>`
**And** the `VMDiscoverer` interface is updated to match the new signature
**And** `GroupByWave` remains unchanged — waves are still determined by the separate wave label

**Given** the `DRPlanReconciler` in `pkg/controller/drplan/reconciler.go`
**When** a DRPlan is reconciled
**Then** `DiscoverVMs` is called with `plan.Name` instead of `plan.Spec.VMSelector`
**And** all downstream logic (wave grouping, volume group resolution, chunking, preflight) operates identically

**Given** the `mapVMToDRPlans` function in the reconciler
**When** a VM label changes
**Then** the function reads the VM's `soteria.io/drplan` label and enqueues the single named plan — O(1) instead of O(N) DRPlan scanning
**And** if the label is absent or empty, no reconcile requests are enqueued

**Given** the `vmRelevantChangePredicate`
**When** the `soteria.io/drplan` label is added, removed, or changed on a VM
**Then** the predicate fires and the relevant DRPlan(s) are reconciled (both old and new plan if the value changed)

**Given** the validation in `pkg/apis/soteria.io/v1alpha1/validation.go`
**When** `ValidateDRPlan` is called
**Then** the `validateVMSelector` function is removed
**And** validation of `waveLabel` and `maxConcurrentFailovers` remains unchanged

**Given** the updated discovery engine
**When** unit tests in `pkg/engine/discovery_test.go` run
**Then** tests verify: VMs with matching `soteria.io/drplan` label are discovered, VMs without the label are not discovered, VMs with a different plan name are not discovered, wave grouping still works correctly

### Story 2b.2: Webhook Simplification

As a platform engineer,
I want admission webhooks to be simpler and faster after the label convention change,
So that DRPlan and VM mutations are validated without expensive cross-resource queries.

**Acceptance Criteria:**

**Given** the `ExclusivityChecker` in `pkg/admission/exclusivity.go`
**When** the label convention is adopted
**Then** the entire `exclusivity.go` file is deleted — `FindMatchingPlans`, `CheckVMExclusivity`, and `CheckDRPlanExclusivity` are no longer needed
**And** `exclusivity_test.go` is deleted

**Given** the `DRPlanValidator` in `pkg/admission/drplan_validator.go`
**When** a DRPlan CREATE or UPDATE is admitted
**Then** the webhook no longer calls `DiscoverVMs` or `CheckDRPlanExclusivity`
**And** the `ExclusivityChecker` dependency is removed from `DRPlanValidator`
**And** validation of `waveLabel`, `maxConcurrentFailovers` (field-level) remains
**And** namespace consistency and throttle capacity checks remain — but are now performed by the controller during reconciliation (eventual consistency), not at admission time
**And** the webhook becomes a lightweight field validator only

**Given** the `VMValidator` in `pkg/admission/vm_validator.go`
**When** a VirtualMachine CREATE or UPDATE is admitted with a `soteria.io/drplan` label
**Then** the webhook validates that the referenced DRPlan exists (by name lookup) — if not, it issues a warning (not a rejection, to avoid ordering issues during GitOps apply)
**And** the `CheckVMExclusivity` call is removed entirely — exclusivity is guaranteed by label semantics
**And** wave conflict checking for namespace-level consistency is simplified: the webhook reads the plan name from the label, fetches that single plan, and checks wave consistency only within that plan's VMs

**Given** the webhook configuration markers
**When** `make manifests` is run
**Then** RBAC markers are updated to reflect reduced permissions (no longer needs to list all DRPlans for exclusivity scanning)

**Given** the updated webhooks
**When** integration tests in `test/integration/admission/` run
**Then** DRPlan webhook tests verify field validation without VM discovery
**And** VM webhook tests verify: VM with valid `soteria.io/drplan` label is accepted, VM with wave conflict in namespace-level namespace is rejected, VM without the label is always accepted

### Story 2b.3: Test Suite, Documentation & Requirement Updates

As a team member,
I want all tests, documentation, and requirements to reflect the label convention change,
So that the codebase is consistent and future contributors understand the new design.

**Acceptance Criteria:**

**Given** the existing integration tests
**When** the test suite runs after the refactoring
**Then** all tests in `test/integration/admission/` are updated to use the `soteria.io/drplan` label instead of `vmSelector` overlap scenarios
**And** all tests in `pkg/engine/discovery_test.go` use the new `DiscoverVMs(ctx, planName)` signature
**And** `make test` passes with 100% of existing test scenarios adapted to the new convention
**And** no test files reference `vmSelector` or `ExclusivityChecker`

**Given** the PRD at `_bmad-output/planning-artifacts/prd.md`
**When** updated for the label convention
**Then** FR1 reads: "Platform engineer can create a DRPlan by defining a wave label key and a max concurrent failovers parameter. VMs are associated to the plan by setting the `soteria.io/drplan: <planName>` label"
**And** FR3 reads: "Orchestrator automatically discovers VMs with the `soteria.io/drplan` label matching the plan name and groups them into waves based on the wave label value"
**And** FR4 reads: "VM exclusivity is structurally enforced — a Kubernetes label key can have only one value, so a VM can belong to at most one DRPlan"
**And** FR5 reads: "Platform engineer can add a VM to an existing DRPlan by setting `soteria.io/drplan: <planName>` on the VM — no plan editing required"

**Given** the architecture doc at `_bmad-output/planning-artifacts/architecture.md`
**When** updated
**Then** the admission webhook section reflects the simplified design
**And** the data flow diagram no longer shows `vmSelector` parsing

**Given** the project context at `_bmad-output/project-context.md`
**When** updated
**Then** the CRD JSON fields section removes `vmSelector` and documents `soteria.io/drplan` as the VM association label
**And** the Labels/annotations table includes `soteria.io/drplan`

**Given** sample CRs in `config/samples/`
**When** updated
**Then** DRPlan samples no longer contain `vmSelector`
**And** VM samples include `soteria.io/drplan: <sample-plan-name>` in their labels

### Story 2b.4: ScyllaDB Label-Indexed Pagination — Scan Cap Integration Test

As a developer,
I want the scan cap behavior of the label-indexed pagination re-fetch loop to be verified end-to-end against real ScyllaDB,
So that I am confident the bounded scan and partial-list continue-token logic works correctly under realistic data volumes.

**Background:** Story 1.3.1 implemented label-indexed pagination with a bounded re-fetch loop in `pkg/storage/scylladb/store.go`. The loop has a scan cap (`maxScanRows = limit * 10`) that returns a partial list with a continue token when reached. Task 6.9 — the integration test verifying this behavior — was deferred because it requires creating enough objects with low-selectivity labels to trigger the scan cap, which is expensive in testcontainers. This debt has been carried since Epic 1.

**Acceptance Criteria:**

**Given** the bounded re-fetch loop in `GetList` (path B: negative-only selectors, or label-index path with residual filters)
**When** the label selector has very low selectivity (e.g., fewer than `limit` matches exist within the scan cap window)
**Then** the loop stops at `maxScanRows` and returns a partial list
**And** the response includes a valid continue token
**And** the client can resume from the continue token to retrieve remaining matching objects

**Given** a ScyllaDB testcontainers environment with 100+ objects where only a small fraction (e.g., 5 out of 100) match a given label selector
**When** `GetList` is called with `limit=10` and the matching label selector
**Then** the scan cap is reached before 10 matches are found
**And** the returned list contains fewer than `limit` items (the 5 matches found within the scan window)
**And** `list.Continue` is non-empty (indicating more rows exist beyond the scan cap)

**Given** the partial list with a continue token from the scan cap scenario
**When** the client issues a follow-up `GetList` with the continue token
**Then** the follow-up request resumes scanning from where the previous request stopped
**And** any additional matches beyond the scan cap window are returned

**Given** the scan cap integration test
**When** `make integration` runs
**Then** the test passes alongside all existing label-indexed pagination tests (Story 1.3.1 Tasks 6.1–6.8, 6.10)

---

## Epic 3: Storage Driver Framework & Reference Implementations

Storage vendor engineers can implement and validate new drivers using the 7-method Go interface and conformance test suite. The replication model uses three volume roles (NonReplicated, Source, Target) with all transitions routed through NonReplicated. The no-op driver enables full dev/CI without real storage. Driver selection is automatic from PVC storage classes.

### Story 3.1: StorageProvider Interface & Driver Registry

As a storage vendor engineer,
I want a clearly defined 7-method Go interface with typed errors and an automatic driver registry,
So that I know exactly what to implement and how drivers are discovered at runtime.

**Acceptance Criteria:**

**Given** the file `pkg/drivers/interface.go`
**When** the StorageProvider interface is defined
**Then** it declares exactly 7 methods: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, SetSource, SetTarget, StopReplication, GetReplicationStatus (FR20)
**And** every method accepts `context.Context` as its first parameter for timeout and cancellation support
**And** method signatures use domain types (not raw strings) for volume group IDs, replication roles, etc.
**And** the interface is documented with godoc comments explaining each method's contract, idempotency guarantee, and expected error conditions

**Given** the file `pkg/drivers/errors.go`
**When** typed error variables are defined
**Then** sentinel errors exist for: `ErrVolumeNotFound`, `ErrVolumeGroupNotFound`, `ErrReplicationNotReady`, `ErrInvalidTransition`, `ErrDriverNotFound`
**And** all error variables use the `Err` prefix per Go convention
**And** driver implementations return these typed errors — never raw errors

**Given** the file `pkg/drivers/registry.go`
**When** a driver registers itself via `init()` function
**Then** the driver is added to a global registry keyed by storage class provisioner name
**And** `RegisterDriver(provisionerName string, factory DriverFactory)` is the registration API

**Given** a VM with PVCs using a specific storage class
**When** the orchestrator needs to select a driver (FR21)
**Then** the registry inspects the PVC's storage class provisioner field
**And** returns the registered driver for that provisioner
**And** returns `ErrDriverNotFound` if no driver is registered for the provisioner

**Given** a DRPlan with VMs using different storage backends
**When** the orchestrator processes different VMs
**Then** each VM is handled by the appropriate driver selected from the registry (FR25)

### Story 3.2: No-Op Driver

As a developer,
I want a no-op driver that implements the full StorageProvider interface without performing actual storage operations,
So that I can develop, test, and run CI without storage infrastructure from Day 1.

**Acceptance Criteria:**

**Given** the no-op driver in `pkg/drivers/noop/driver.go`
**When** any of the 7 StorageProvider methods is called
**Then** the method returns success without performing any actual storage operations (FR23)
**And** the driver logs the operation at V(1) level with structured logging: `log.FromContext(ctx).V(1).Info("No-op: Set volume group to Source", "volumeGroupID", vgID)`

**Given** the no-op driver
**When** `CreateVolumeGroup` is called
**Then** a synthetic volume group ID is generated and returned
**And** subsequent `GetVolumeGroup` calls with that ID return the synthetic group

**Given** the no-op driver
**When** `GetReplicationStatus` is called
**Then** it returns the current role and health with a synthetic RPO (e.g., last sync = now)
**And** the role reflects the last operation (e.g., after SetSource, role is Source with HealthHealthy)

**Given** the no-op driver
**When** all 7 methods are called repeatedly
**Then** every method is idempotent — calling the same operation twice produces the same result without error

**Given** the no-op driver
**When** it registers via `init()`
**Then** it registers under a known provisioner name (e.g., `noop.soteria.io`)
**And** `make dev-cluster` uses this driver for local development

### Story 3.3: Fake Driver for Unit Testing

As a developer,
I want a programmable fake driver for unit tests,
So that I can test workflow engine behavior with controlled storage responses including error injection.

**Acceptance Criteria:**

**Given** the fake driver in `pkg/drivers/fake/driver.go`
**When** instantiated in a test
**Then** the caller can program responses for each method: `fake.OnSetSource(vgID).Return(nil)` or `fake.OnSetSource(vgID).Return(drivers.ErrInvalidTransition)`
**And** the fake records all method calls with arguments for assertion

**Given** a programmed fake driver
**When** a method is called that has a programmed error response
**Then** the configured error is returned
**And** the call is recorded in the call history

**Given** a programmed fake driver
**When** a method is called with no programmed response
**Then** a sensible default is returned (success with empty/zero-value result)

**Given** the fake driver
**When** used in concurrent test scenarios
**Then** call recording and response programming are thread-safe

**Given** the fake driver package name
**When** reviewing the package
**Then** it follows the k8s `<package>fake` convention at `pkg/drivers/fake/`

### Story 3.4: Conformance Test Suite

As a storage vendor engineer,
I want a conformance test suite that validates the full DR lifecycle against any driver,
So that I can prove my driver implementation is correct before submitting it.

**Acceptance Criteria:**

**Given** the conformance suite in `pkg/drivers/conformance/suite.go`
**When** run against any StorageProvider implementation
**Then** it exercises the full DR lifecycle in sequence:
1. CreateVolumeGroup — creates a volume group
2. SetSource — transitions to Source role (replication origin)
3. GetReplicationStatus — verifies role is Source, health is Healthy
4. StopReplication — transitions back to NonReplicated
5. SetTarget — transitions to Target role (replication destination)
6. GetReplicationStatus — verifies role is Target, health is Healthy
7. StopReplication — transitions back to NonReplicated
8. DeleteVolumeGroup — cleans up (FR24)

**Given** the conformance suite
**When** any method returns an unexpected error
**Then** the test fails with a clear message identifying which lifecycle step failed and the error returned

**Given** the conformance suite
**When** run against the no-op driver
**Then** all tests pass — confirming the no-op driver is a valid reference implementation

**Given** the conformance suite
**When** testing idempotency
**Then** each method is called twice in succession and the second call succeeds without error
**And** this verifies all 7 methods are idempotent as required

**Given** the conformance suite
**When** testing context cancellation
**Then** each method respects `context.Context` cancellation and returns promptly when the context is cancelled

**Given** the conformance suite documentation
**When** a vendor engineer reads it
**Then** clear instructions explain how to wire their driver into the suite and run it: `go test ./pkg/drivers/conformance/ -run TestConformance -driver=<name>`

---

## Epic 4: DR Workflow Engine — Full Lifecycle

Operators can execute the complete DR lifecycle through 4 rest states (SteadyState, FailedOver, DRedSteadyState, FailedBack) and 8 phases (including 4 transition states: FailingOver, Reprotecting, FailingBack, ReprotectingBack). Four operations drive the cycle: failover (planned migration RPO=0 or disaster RPO>0), re-protect, failback, and restore. Failover and failback share a single FailoverHandler. Re-protect and restore share a single ReprotectHandler. Execution respects wave ordering, DRGroup chunking with throttling, fail-forward error handling, checkpoint-based pod restart resumption, and manual retry of failed groups.

### Story 4.1: DR State Machine & Execution Controller

As an operator,
I want the orchestrator to enforce valid state transitions for the DR lifecycle,
So that plans progress through well-defined states and invalid operations are rejected.

**Acceptance Criteria:**

**Given** the state machine in `pkg/engine/statemachine.go`
**When** DRPlan phase transitions are defined
**Then** the following 8 phases exist: 4 rest states (`SteadyState`, `FailedOver`, `DRedSteadyState`, `FailedBack`) and 4 transition states (`FailingOver`, `Reprotecting`, `FailingBack`, `ReprotectingBack`)
**And** valid transitions are enforced: SteadyState→FailingOver, FailingOver→FailedOver, FailedOver→Reprotecting, Reprotecting→DRedSteadyState, DRedSteadyState→FailingBack, FailingBack→FailedBack, FailedBack→ReprotectingBack, ReprotectingBack→SteadyState
**And** invalid transitions return a typed error with current and requested states

**Given** the DRExecution controller in `pkg/controller/drexecution/reconciler.go`
**When** a DRExecution resource is created
**Then** the controller validates the execution mode is `planned_migration`, `disaster`, or `reprotect` (FR19)
**And** the controller validates the referenced DRPlan exists and is in a valid starting state
**And** the DRPlan phase is transitioned to the appropriate transition state (`FailingOver`, `Reprotecting`, `FailingBack`, or `ReprotectingBack`)
**And** the controller triggers the workflow engine

**Given** a DRExecution request
**When** the DRPlan is not in a valid state for the requested operation
**Then** the admission webhook in `pkg/admission/drexecution_validator.go` rejects the DRExecution creation
**And** the error message identifies the current plan state and the valid transitions

**Given** any failover operation
**When** it is triggered
**Then** it requires explicit human initiation via DRExecution creation — no automatic failure detection or auto-failover exists (FR18)

**Given** the state machine
**When** unit tests run
**Then** all valid transitions succeed, all invalid transitions are rejected, and edge cases (concurrent transitions, re-entrant calls) are covered

### Story 4.2: DRGroup Chunking & Wave Executor

As an operator,
I want waves executed sequentially with operations within a wave chunked into DRGroups respecting maxConcurrentFailovers,
So that failover is throttled to prevent resource exhaustion.

**Acceptance Criteria:**

**Given** a DRPlan with 3 waves and `maxConcurrentFailovers: 4`
**When** the wave executor in `pkg/engine/executor.go` processes the plan
**Then** waves are executed strictly in sequence: Wave 1 completes before Wave 2 starts (FR11)
**And** within each wave, VMs are partitioned into DRGroup chunks of at most 4 VMs
**And** DRGroup chunks within a wave execute concurrently (FR11)

**Given** the chunker in `pkg/engine/chunker.go`
**When** `maxConcurrentFailovers` is 4 and a wave has 10 VMs (no namespace constraints)
**Then** 3 DRGroup chunks are created: [4 VMs], [4 VMs], [2 VMs]
**And** chunks process concurrently up to the wave's chunk set

**Given** namespace-level consistency with 3 VMs in namespace "erp-db" in the same wave
**When** `maxConcurrentFailovers` is 4 and the wave also has 2 individual VMs
**Then** the namespace group of 3 VMs is placed in one chunk (never split)
**And** the 2 individual VMs fill remaining capacity: chunk 1 = [3 namespace VMs + 1 individual], chunk 2 = [1 individual] (FR12)

**Given** `maxConcurrentFailovers` counts individual VMs regardless of consistency level
**When** a namespace group has 3 VMs and maxConcurrent is 4
**Then** the namespace group consumes 3 of the 4 slots in the chunk (FR12)

**Given** remaining chunk capacity cannot fit the next namespace group
**When** the chunker encounters this situation
**Then** a new chunk is created even if the current chunk has unused capacity (FR12)

**Given** the chunker
**When** unit tests run with table-driven scenarios
**Then** all chunking scenarios are verified: single chunk, multiple chunks, namespace groups, namespace group exactly equals max, namespace group forces underfilled chunk, mixed VM-level and namespace-level

### Story 4.3: Planned Migration Workflow

As an operator,
I want to execute a planned migration that gracefully stops origin VMs, waits for final replication sync, then promotes and starts VMs wave by wave with zero data loss,
So that I can migrate workloads during maintenance windows.

**Acceptance Criteria:**

**Given** a DRExecution with mode `planned_migration` and both DCs available
**When** the unified failover workflow in `pkg/engine/failover.go` executes with GracefulShutdown=true
**Then** Step 0 executes first: origin VMs are gracefully stopped, StopReplication is called on origin volumes, and the workflow waits for the final replication sync to complete — guaranteeing RPO=0 (FR9)
**And** after sync completes, SetSource (force=false) is called on the target site volumes
**And** target VMs are started wave by wave in sequence

**Given** a wave with multiple DRGroups
**When** the planned migration processes the wave
**Then** for each DRGroup: SetSource is called for all volume groups in the group, then VMs are started
**And** DRGroups within the wave execute concurrently per Story 4.2

**Given** a planned migration execution
**When** all waves complete successfully
**Then** DRExecution `.status.result` is set to `Succeeded`
**And** DRPlan phase transitions to `FailedOver`
**And** per-wave and per-DRGroup status, timing, and step details are recorded in DRExecution `.status`

**Given** a planned migration
**When** the origin site becomes unreachable during Step 0 (before sync completes)
**Then** the execution fails with a clear error indicating the origin is unreachable
**And** the operator can choose to re-attempt as planned migration or switch to disaster mode

### Story 4.4: Disaster Failover Workflow

As an operator,
I want to execute a disaster failover that force-promotes target volumes and starts VMs wave by wave while ignoring origin errors,
So that workloads recover quickly when the primary DC is down.

**Acceptance Criteria:**

**Given** a DRExecution with mode `disaster`
**When** the unified failover workflow in `pkg/engine/failover.go` executes with GracefulShutdown=false, Force=true
**Then** no Step 0 occurs — the origin site is assumed unreachable (FR10)
**And** SetSource is called with `force: true` on the target site for each DRGroup
**And** errors from the origin site are logged but do not block execution (FR10)
**And** target VMs are started wave by wave in sequence

**Given** a disaster failover with force-promote
**When** a volume promotion succeeds
**Then** the VM is started on the target site
**And** RPO is recorded based on the last known replication sync time (RPO > 0)

**Given** a disaster failover
**When** all waves complete successfully
**Then** DRExecution `.status.result` is set to `Succeeded`
**And** DRPlan phase transitions to `FailedOver`
**And** total RPO is reported as the maximum RPO across all volume groups

**Given** disaster and planned migration modes
**When** comparing the two workflows
**Then** execution mode is determined solely by the DRExecution `.spec.mode` field — the DRPlan has no `type` field (FR19)

### Story 4.5: Fail-Forward Error Handling & Partial Success

As an operator,
I want failed DRGroups to be marked Failed while the engine continues with remaining groups and reports PartiallySucceeded,
So that partial recovery is better than no recovery during a disaster.

**Acceptance Criteria:**

**Given** a wave with 3 DRGroup chunks executing concurrently
**When** DRGroup-2 fails (e.g., SetSource returns ErrInvalidTransition)
**Then** DRGroup-2 is marked `Failed` with the error message recorded in DRExecution status (FR13)
**And** DRGroup-1 and DRGroup-3 continue executing unaffected
**And** the wave completes when all non-failed DRGroups finish

**Given** a failed DRGroup in Wave 1
**When** Wave 1 completes with partial failure
**Then** the engine proceeds to Wave 2 (FR13)
**And** Wave 2 DRGroups execute normally

**Given** an execution where some DRGroups failed
**When** the final wave completes
**Then** DRExecution `.status.result` is set to `PartiallySucceeded` (not `Failed`)
**And** `.status.waves[].groups[]` shows exactly which groups Completed and which Failed
**And** each failed group includes the error message, the affected VM names, and the step where failure occurred

**Given** an execution where all DRGroups succeed
**When** the final wave completes
**Then** DRExecution `.status.result` is `Succeeded`

**Given** an execution where a critical pre-condition failure occurs (e.g., plan not found)
**When** the engine cannot proceed at all
**Then** DRExecution `.status.result` is `Failed` with a top-level error

### Story 4.6: Failed DRGroup Retry

As an operator,
I want to manually retry a failed DRGroup when the VM is in a healthy state, and have the orchestrator reject retries when the state is unpredictable,
So that I can recover from transient failures safely.

**Acceptance Criteria:**

**Given** a DRExecution with a DRGroup in `Failed` state
**When** the operator requests a retry of the failed DRGroup (FR14)
**Then** the orchestrator validates that the VMs in the DRGroup are still in a healthy, known state on the original site
**And** if preconditions are met, the DRGroup is re-executed using the same workflow (planned or disaster)
**And** the DRGroup status transitions from `Failed` to `InProgress` to `Completed`

**Given** a failed DRGroup where the VM state is non-standard
**When** the operator requests a retry
**Then** the orchestrator rejects the retry with a clear error: "VM <name> is in an unpredictable state — manual intervention required" (FR15)
**And** the DRGroup remains in `Failed` state

**Given** a successful retry
**When** all DRGroups in the execution are now Completed
**Then** DRExecution `.status.result` is updated from `PartiallySucceeded` to `Succeeded`

**Given** a failed DRGroup
**When** the operator decides not to retry
**Then** the DRGroup remains `Failed` and the DRExecution result stays `PartiallySucceeded`
**And** no further action is taken automatically

**Given** the retry mechanism
**When** the retry itself fails
**Then** the DRGroup returns to `Failed` with the new error message
**And** the operator can attempt another retry if preconditions still hold

### Story 4.7: Checkpoint, Resume & High Availability

As an operator,
I want in-progress executions to resume from the last checkpoint after a pod restart with at most one in-flight DRGroup lost,
So that DR operations survive orchestrator failures.

**Acceptance Criteria:**

**Given** an in-progress DRExecution with Wave 1 completed and Wave 2 in progress
**When** the orchestrator pod restarts
**Then** the new pod acquires the leader lease (NFR2)
**And** the DRExecution controller loads the execution state from the DRExecution `.status`
**And** execution resumes from the last checkpointed state — Wave 2 DRGroups that were completed remain completed
**And** at most one in-flight DRGroup operation is lost and retried (NFR1)

**Given** the checkpoint mechanism in `pkg/engine/checkpoint.go`
**When** a DRGroup completes (success or failure)
**Then** the DRExecution `.status` is updated immediately via the Kubernetes API (through kube-apiserver, not direct ScyllaDB)
**And** the checkpoint includes per-DRGroup state, timestamps, and any error details

**Given** the leader election configuration
**When** the active instance fails
**Then** a standby instance acquires the Kubernetes Lease within the configured lease duration
**And** the standby resumes all in-progress executions from their last checkpoints (NFR2)
**And** leader election controls the workflow engine only — all replicas continue serving API requests

**Given** checkpoint writes
**When** a checkpoint write fails (e.g., API server temporarily unreachable)
**Then** the engine retries the checkpoint write with exponential backoff
**And** the engine does not proceed to the next DRGroup until the checkpoint is persisted

**Given** multiple concurrent DRPlan executions (separate plans, disjoint VM sets)
**When** the orchestrator processes them
**Then** executions run independently without interference (NFR11)
**And** checkpointing for one execution does not block another

### Story 4.8: Re-protect & Failback Workflows

As an operator,
I want to re-establish replication after failover or failback via re-protect, and fail back to the original site,
So that the system completes the full 8-phase DR lifecycle.

**Acceptance Criteria:**

**Given** a DRPlan in `FailedOver` phase after a successful failover
**When** the operator triggers re-protect by creating a DRExecution with mode `reprotect`
**Then** the orchestrator calls StopReplication on the old active site (if reachable) for each volume group (FR16)
**And** transitions old active volumes to Target and new active volumes to Source to establish replication in the new direction
**And** monitors replication health via GetReplicationStatus until all volume groups report Healthy
**And** DRPlan phase transitions from `FailedOver` → `Reprotecting` → `DRedSteadyState`

**Given** re-protect when the old active site is unreachable
**When** StopReplication fails for the origin
**Then** the orchestrator logs the error and proceeds with role transitions
**And** replication may take longer to establish but the workflow continues

**Given** a DRPlan in `DRedSteadyState` phase with healthy replication
**When** the operator triggers failback using the same FailoverHandler (planned_migration or disaster mode)
**Then** the orchestrator executes the same workflow as failover (FR17)
**And** DRPlan phase transitions from `DRedSteadyState` → `FailingBack` → `FailedBack`
**And** `FailedBack` indicates the system is unprotected — restore is required

**Given** a DRPlan in `FailedBack` phase after a successful failback
**When** the operator triggers restore by creating a DRExecution with mode `reprotect`
**Then** the same ReprotectHandler establishes replication in the original direction (A→B)
**And** DRPlan phase transitions from `FailedBack` → `ReprotectingBack` → `SteadyState`

**Given** the re-protect or restore workflow
**When** replication health monitoring is in progress
**Then** DRPlan status conditions report the resync progress (percentage or state)
**And** the controller polls GetReplicationStatus at regular intervals until healthy

**Given** the full DR lifecycle
**When** executed end-to-end: SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState
**Then** the system returns to the original configuration with healthy replication in the original direction
**And** DRExecution records exist for all four operations (failover, re-protect, failback, restore)
**And** the cycle can be repeated

---

## Epic 5: Monitoring, Observability & Audit Trail

Platform engineers can view replication health and RPO per volume group, identify unprotected VMs, access Prometheus metrics, and use immutable DRExecution audit records for compliance evidence.

### Story 5.1: Replication Health Monitoring & RPO Tracking

As a platform engineer,
I want to see replication health status and estimated RPO for every protected volume group,
So that I know whether my DR plans are actually protected.

**Acceptance Criteria:**

**Given** a DRPlan with discovered VMs and active volume groups
**When** the DRPlan controller polls `GetReplicationStatus` from the appropriate StorageProvider driver for each volume group
**Then** DRPlan `.status.replicationHealth` is updated with per-volume-group health: Healthy, Degraded, or Error (FR31)
**And** each volume group entry includes the last successful replication sync timestamp
**And** estimated RPO is calculated as time-since-last-sync for each volume group (FR32)

**Given** a volume group with healthy replication syncing every 10 seconds
**When** the controller polls replication health
**Then** the status shows Healthy with RPO approximately equal to the sync interval

**Given** a volume group where replication has fallen behind or is intermittent
**When** the controller polls replication health
**Then** the status shows Degraded with an elevated RPO reflecting the actual lag

**Given** a volume group where replication has broken completely
**When** the controller polls replication health
**Then** the status shows Error with RPO reported as unknown
**And** DRPlan `.status.conditions` includes a `Degraded` condition identifying the affected volume groups

**Given** the remote DC is unreachable
**When** the controller attempts to poll replication health
**Then** the status shows Unknown (not Error — we cannot confirm whether replication is broken or just unobservable)
**And** the last-checked timestamp is updated to reflect when the check was attempted

**Given** replication health updates
**When** the status changes (e.g., Healthy → Degraded)
**Then** the update is visible to Console watch clients within 5 seconds (NFR7)
**And** a Kubernetes event is emitted: e.g., `ReplicationDegraded` with a human-readable message

### Story 5.2: Unprotected VM Detection

As a platform engineer,
I want to identify VMs not covered by any DRPlan,
So that I can close protection gaps before they become audit findings.

**Acceptance Criteria:**

**Given** a cluster with VMs, some covered by DRPlans and some not
**When** the orchestrator evaluates VM coverage (FR34)
**Then** VMs not matched by any DRPlan's `vmSelector` are identifiable
**And** the count of unprotected VMs is available via the API

**Given** a DRPlan's label selector
**When** VMs exist that match no DRPlan's selector
**Then** these VMs are reported as unprotected with their names and namespaces

**Given** unprotected VM data
**When** queried via kubectl (e.g., `kubectl get` on a status field or a dedicated subresource)
**Then** the list of unprotected VMs is returned in a structured format suitable for tooling and Console consumption

**Given** a previously unprotected VM
**When** labels are added that match an existing DRPlan's selector
**Then** the VM is no longer reported as unprotected on the next reconcile cycle

### Story 5.3: Prometheus Metrics

As a platform engineer,
I want Prometheus metrics for VM counts, failover duration, RPO/replication lag, and execution outcomes,
So that I can integrate DR monitoring into my existing OpenShift observability stack.

**Acceptance Criteria:**

**Given** the metrics module in `pkg/metrics/metrics.go`
**When** the orchestrator is running
**Then** the following metrics are exposed on the `/metrics` endpoint (FR33):
- `soteria_drplan_vms_total` (gauge, per plan) — count of VMs under each DRPlan
- `soteria_failover_duration_seconds` (histogram, per plan type and mode) — execution duration
- `soteria_replication_lag_seconds` (gauge, per volume group) — RPO / time since last replication sync
- `soteria_execution_total` (counter, per plan type, mode, and result) — execution success/failure counts

**Given** the metrics naming
**When** reviewing metric names and labels
**Then** all metrics use the `soteria_` prefix with snake_case and unit suffixes per OpenShift conventions (NFR18)
**And** metrics are scrapeable by the in-cluster Prometheus stack without additional ServiceMonitor configuration beyond what OLM provides

**Given** a completed DRExecution
**When** the execution finishes
**Then** `soteria_failover_duration_seconds` records the total duration
**And** `soteria_execution_total` increments with labels for mode (planned_migration/disaster) and result (Succeeded/PartiallySucceeded/Failed)

**Given** the replication health polling loop from Story 5.1
**When** RPO is updated for a volume group
**Then** `soteria_replication_lag_seconds` is updated for that volume group's metric label set

**Given** any metric output
**When** inspecting metric labels and values
**Then** no storage credentials or sensitive information appear in any metric name, label, or help text (NFR14)

### Story 5.4: DRExecution as Immutable Audit Record

As a platform engineer,
I want every execution's DRExecution `.status` to serve as the immutable audit record with per-wave, per-group, per-step detail, timestamps, and error messages, persisting across DC failures via the shared state layer,
So that `kubectl get drexecutions` is all I need for compliance evidence.

**Acceptance Criteria:**

**Given** a completed DRExecution (any mode, any result)
**When** the `.status` section is inspected
**Then** it contains: execution mode, result (Succeeded/PartiallySucceeded/Failed), `startTime`, `completionTime`, total duration, and estimated RPO (FR41)
**And** `.status.waves[]` contains per-wave entries with: wave number, start time, completion time, and aggregate status
**And** `.status.waves[].groups[]` contains per-DRGroup entries with: VM names, status (Completed/Failed), start time, completion time, error message (if failed), and per-step details (promote, start VM, etc.)
**And** timestamps use `metav1.Time` (ISO 8601)

**Given** a DRExecution record
**When** the execution has completed
**Then** the record is immutable — no further updates to `.status` are permitted by the API strategy (append-only semantics from Story 1.5)
**And** the `spec` is immutable from creation

**Given** DRExecution records stored in ScyllaDB
**When** one datacenter fails
**Then** all DRExecution records remain accessible on the surviving cluster via the shared state layer (FR43)
**And** when the failed DC recovers, execution records are automatically reconciled

**Given** a DRPlan with multiple past executions
**When** a platform engineer queries execution history (FR42)
**Then** `kubectl get drexecutions -l soteria.io/plan-name=<plan>` returns all executions for that plan
**And** results are sorted by creation time (most recent first)
**And** each record is self-contained — no external log or database lookup needed for audit evidence

**Given** the audit record content
**When** reviewed for compliance purposes
**Then** the structured data satisfies general compliance needs (SOX, ISO 22301, SOC 2) without manual assembly
**And** no credentials or sensitive information appear in any field (NFR14)

### Story 5.7: Driver Interface Simplification & Workflow Symmetry

As a storage vendor engineer and DR operator,
I want the StorageProvider interface to contain only the methods the orchestrator actually uses, with no force-flag leaking implementation concerns into the engine,
So that driver implementations are simpler, the failover/failback code paths are truly symmetric, and the system is easier to reason about and test.

**Acceptance Criteria:**

**Given** the current `StorageProvider` interface with `SetTarget`
**When** reviewing engine handler usage
**Then** `SetTarget` is never called — remove it from the interface, fake driver, no-op driver, and conformance suite

**Given** the `Force` field on `SetSourceOptions`, `SetTargetOptions`, and `StopReplicationOptions`
**When** reviewing the orchestrator's responsibilities
**Then** peer-unreachable handling is the driver's internal concern — remove `Force` from all Options structs (or the structs entirely if empty)

**Given** the current `FailoverHandler.ExecuteGroup` with branching on `GracefulShutdown`
**When** `StopReplication` is idempotent (no-op when already NonReplicated)
**Then** both planned and disaster per-group paths use the same code: `StopReplication → StartVM`

**Given** the current `PreExecute` with StopVM + StopReplication + sync wait
**When** per-group execution always calls `StopReplication`
**Then** Step 0 is reduced to `StopVM` only — replication stop and sync wait are removed

**Given** `FailoverConfig{GracefulShutdown, Force}`
**When** `Force` is removed from the driver interface
**Then** `FailoverConfig` simplifies to `{GracefulShutdown bool}` — the controller maps `planned_migration → true`, `disaster → false`

---

## Epic 6: OCP Console — Dashboard & Plan Management

The OCP Console provides a sortable/filterable DR Dashboard table (500+ plans), persistent alert banners for protection status, plan detail pages with a DR lifecycle state machine overview, wave composition trees, execution history, and configuration views.

### Story 6.1: Console Plugin Project Initialization

As a developer,
I want the `console-plugin/` directory scaffolded from the openshift/console-plugin-template with TypeScript, React, PatternFly 6, webpack module federation, Jest, and axe-core configured,
So that all subsequent Console development has a working build, dev server, and test harness.

**Acceptance Criteria:**

**Given** the repository root
**When** the `console-plugin/` directory is initialized from `openshift/console-plugin-template`
**Then** `package.json` exists with dependencies for React, PatternFly 6, and the Console SDK (`@openshift-console/dynamic-plugin-sdk`)
**And** `tsconfig.json` is configured for strict TypeScript compilation
**And** `webpack.config.ts` is configured for module federation as a dynamic OCP Console plugin
**And** `console-extensions.json` defines the plugin's extension points

**Given** the scaffolded project
**When** `yarn install && yarn build` is run
**Then** the project compiles without errors
**And** a production build is output suitable for the nginx-based Console plugin image

**Given** the scaffolded project
**When** `yarn start` or `yarn start-console` is run
**Then** the Console plugin dev server starts and is accessible for local development

**Given** the testing configuration
**When** Jest is configured
**Then** `yarn test` runs and passes with zero tests (baseline)
**And** axe-core is integrated for automated accessibility audits (`jest-axe` or equivalent)
**And** React Testing Library is available for component tests

**Given** the project structure
**When** reviewing the directory layout
**Then** it includes: `src/components/`, `src/hooks/`, `src/models/`, `src/utils/`, `tests/components/`
**And** `src/models/types.ts` defines TypeScript interfaces matching CRD schemas (DRPlan, DRExecution, DRGroupStatus)
**And** `src/hooks/useDRResources.ts` provides `useK8sWatchResource` wrappers for Soteria resources

**Given** the Console plugin image
**When** `console-plugin/Dockerfile` is built
**Then** it produces an nginx image serving the compiled plugin assets
**And** the image is separate from the Go operator binary image

### Story 6.2: Console Plugin Navigation & Routing

As a platform engineer,
I want a "Disaster Recovery" entry in the OCP Console left navigation with URL-based routing to Dashboard, Plan Detail, and Execution Detail views,
So that DR management is a native part of my Console experience.

**Acceptance Criteria:**

**Given** the Console plugin is installed on an OpenShift cluster
**When** the plugin loads
**Then** a "Disaster Recovery" navigation item appears in the Console's left navigation sidebar (UX-DR14)
**And** clicking it navigates to the DR Dashboard as the default landing page

**Given** the navigation structure
**When** URL-based routing is configured
**Then** the following routes are defined:
- `/disaster-recovery` → DR Dashboard
- `/disaster-recovery/plans/:name` → Plan Detail
- `/disaster-recovery/executions/:name` → Execution Detail
**And** browser back/forward navigation works correctly at every level

**Given** the Plan Detail or Execution Detail page
**When** the page loads
**Then** a Breadcrumb component shows the navigation path: DR Dashboard > Plan Name > [context]
**And** each breadcrumb segment is a clickable link

**Given** the Dashboard table
**When** a user navigates to Plan Detail and then returns to the Dashboard
**Then** table scroll position and active filters are preserved (UX-DR14)

### Story 6.3: DR Dashboard Table & Toolbar

As a platform engineer,
I want a sortable, filterable dashboard table showing all DRPlans with status, replication health, and last execution,
So that I can assess DR posture for 500+ plans at a glance.

**Acceptance Criteria:**

**Given** the DR Dashboard page
**When** it loads with DRPlan data via `useK8sWatchResource`
**Then** a PatternFly Table (composable, compact variant) displays with columns: Name (link), Phase (status badge), Active On (cluster name), Protected (ReplicationHealthIndicator compact — icon + health label + RPO), Last Execution (date + result badge), Actions (kebab menu) (UX-DR1, FR35)

**Given** the dashboard table with data
**When** the user clicks a column header
**Then** the table sorts by that column
**And** default sort is by Protected column: Error first, then Degraded, then Unknown, then Healthy — problems surface to the top (UX-DR1)

**Given** the dashboard toolbar (UX-DR15)
**When** the user interacts with filter controls
**Then** a text search filters by plan name (debounced)
**And** dropdown multi-select filters are available for Phase, Active On, Protected, Last Execution
**And** filters use additive AND logic
**And** active filter chips display below the toolbar with individual clear buttons and "Clear all"
**And** result count shows: "Showing N of M plans"
**And** active filters are reflected in the URL (shareable filtered views)

**Given** the Protected column (UX-DR8 compact variant)
**When** rendering replication health
**Then** each cell shows: icon + health label + "RPO Ns" in a single line
**And** Healthy = green checkmark, Degraded = yellow warning, Error = red error circle, Unknown = gray question mark

**Given** the Actions kebab menu per row
**When** the user opens it
**Then** only valid state transitions appear as menu items (e.g., SteadyState shows Failover and Planned Migration; FailedOver shows Reprotect) — invalid actions are hidden, not disabled (UX-DR19)

**Given** the table with 500 plans
**When** rendering and interacting
**Then** performance is acceptable with no visible lag on sort, filter, or scroll operations (UX-DR1)

**Given** the dashboard at different screen sizes (UX-DR20)
**When** viewed at 1920px+
**Then** all columns are visible with generous spacing
**When** viewed at 1440px
**Then** the table fits without horizontal scroll (6 columns)
**When** viewed at 1024px (minimum supported)
**Then** all columns remain visible in compact layout

### Story 6.4: Alert Banner System

As a platform engineer,
I want persistent alert banners above the dashboard for unprotected plans and degraded replication,
So that critical protection gaps are impossible to miss.

**Acceptance Criteria:**

**Given** one or more DRPlans with broken replication (Error state)
**When** the dashboard loads
**Then** a danger Alert banner (PatternFly Alert, variant="danger", not dismissible) displays above the table: "N DR Plans running UNPROTECTED — replication broken" with a direct action link (UX-DR2)

**Given** one or more DRPlans with degraded replication
**When** the dashboard loads
**Then** a warning Alert banner (variant="warning", not dismissible) displays: "N plans with degraded replication" (UX-DR2)

**Given** no plans with broken or degraded replication
**When** the dashboard loads
**Then** no alert banners appear — the absence of banners IS the positive "all healthy" signal

**Given** alert banners are displayed
**When** the underlying condition is resolved (e.g., re-protect completes and replication becomes healthy)
**Then** the banner disappears automatically on the next data refresh via watch update
**And** no manual dismissal is needed for condition-based banners

**Given** the banner action link
**When** the user clicks it
**Then** the dashboard table filters to show only the affected plans

**Given** the alert system
**When** tested with axe-core
**Then** all alert banners pass accessibility audits including screen reader announcement of alert content

### Story 6.5: Plan Detail Shell & Overview Tab (DRLifecycleDiagram)

As a platform engineer,
I want a plan detail page with an Overview tab showing the 4-phase DR lifecycle as an interactive state machine,
So that I can see my plan's lifecycle state, take context-aware actions, and monitor transition progress.

**Acceptance Criteria:**

**Given** a DRPlan selected from the dashboard table (row click)
**When** the Plan Detail page loads
**Then** a full-page detail view renders with four horizontal tabs: Overview, Waves, History, Configuration (UX-DR4)
**And** the Waves, History, and Configuration tab panels render placeholder content (implemented in Story 6.5b)

**Given** the Overview tab
**When** it renders
**Then** a plan header shows: plan name, VM count, wave count, and active cluster
**And** a DRLifecycleDiagram custom component renders the 4-phase DR lifecycle as a visual cycle: SteadyState → FailedOver → DRedSteadyState → FailedBack
**And** only the current rest phase is highlighted (accent-filled border); all other phases are faded to ~35% opacity — exactly one phase highlighted at a time
**And** each phase node shows: phase label, description, VM location (DC1/DC2), datacenter roles, and replication direction
**And** the outgoing transition arrow from the current rest phase shows an enabled action button; all other arrows show faded action name text
**And** Failover uses danger variant (red); Reprotect, Failback, and Restore use secondary variant (FR36, UX-DR19)
**And** clicking an action button calls an `onAction` callback prop (pre-flight modal wired in Story 7.1) — no inline keyword input

**Given** the Overview tab during an active transition (FailingOver, Reprotecting, FailingBack, or Restoring)
**When** it renders
**Then** a transition progress banner (PatternFly Callout, info variant) appears above the diagram showing: action name, wave progress (e.g., "Wave 2 of 3"), elapsed time, estimated remaining time, and a link to the execution detail view
**And** the outgoing transition arrow shows "In progress..." with a blue indicator instead of a button
**And** the destination phase node shows a dashed accent border (visual "arriving here")
**And** all action buttons across the diagram are hidden — no transitions can be triggered during an active execution

### Story 6.5b: Waves, History & Configuration Tabs

As a platform engineer,
I want the Plan Detail's Waves, History, and Configuration tabs populated with wave composition, execution history, and plan metadata,
So that I can drill into plan structure, review past executions, and inspect configuration details.

**Acceptance Criteria:**

**Given** the Waves tab
**When** it renders
**Then** a WaveCompositionTree (PatternFly TreeView with custom renderers) displays the wave hierarchy (UX-DR9):
- Wave N (label, VM count, aggregate health) — expandable
  - DRGroup chunk visualization based on maxConcurrentFailovers
    - Per-VM: name, namespace, storage backend, consistency level, replication health, RPO
**And** namespace-consistent VMs are visually grouped
**And** waves default to collapsed; expand on click

**Given** the History tab
**When** it renders
**Then** a PatternFly Table (compact) lists all DRExecution records for this plan (UX-DR11, FR42)
**And** columns: Date, Mode (Planned/Disaster), Result (status badge), Duration, RPO, Triggered By
**And** row click navigates to the Execution Detail view

**Given** the Configuration tab
**When** it renders
**Then** a DescriptionList shows plan metadata: name, label selector, wave label, maxConcurrentFailovers, creation date
**And** a ReplicationHealthIndicator (expanded variant) shows per-volume-group health, RPO, and freshness (UX-DR8)
**And** a PatternFly CodeBlock shows the DRPlan CRD spec in YAML (read-only)
**And** labels and annotations are visible

**Given** a plan with no execution history
**When** the History tab renders
**Then** a compact EmptyState displays: "No executions yet — trigger a planned migration to validate your DR plan" (UX-DR12)

### Story 6.6: Status Badges, Empty States & Accessibility

As a platform engineer,
I want consistent status badges, helpful empty states, and full accessibility support across all DR views,
So that the Console is usable by all operators including those with assistive technology.

**Acceptance Criteria:**

**Given** the status badge system (UX-DR10, UX-DR18)
**When** any DR status is rendered
**Then** PatternFly Label components are used with the correct DR-specific color semantics:
- Phase: SteadyState/DRedSteadyState = green (solid), FailedOver/FailedBack = blue (solid), FailingOver/Reprotecting/FailingBack/Restoring = blue (outlined) + spinner icon
- Execution result: Succeeded = green, PartiallySucceeded = yellow, Failed = red
- Replication: Healthy = green, Degraded = yellow, Error = red, Unknown = gray
**And** all colors use PatternFly CSS custom properties exclusively — no hardcoded values (automatic dark mode support)

**Given** no DRPlans exist on the cluster
**When** the dashboard loads
**Then** a PatternFly EmptyState displays: icon + "No DR Plans configured" + "Create your first DR plan by labeling VMs with..." + link to documentation (UX-DR12)

**Given** any status indicator in the Console
**When** rendered
**Then** status is communicated via icon + text label in addition to color — never color alone (UX-DR16)
**And** screen readers can access the full status as a single readable string (e.g., "erp-full-stack: SteadyState, replication healthy, RPO 12 seconds")

**Given** the failover flow
**When** navigated entirely via keyboard
**Then** the full flow is operable: Tab to plan row → Enter to open detail → Tab to Failover button → Enter to open modal → Tab to confirmation input → type keyword → Tab to Confirm → Enter (UX-DR16)

**Given** any custom component (DRLifecycleDiagram, ReplicationHealthIndicator, WaveCompositionTree)
**When** tested with axe-core in Jest
**Then** zero accessibility violations are reported (UX-DR16)
**And** keyboard navigation tests confirm arrow key and Tab behavior per component
**And** DRLifecycleDiagram: action button reachable via Tab, phase nodes readable by screen reader, ARIA live region announces transition progress

**Given** all Console views
**When** rendered at 720p screen-share resolution
**Then** all text in status indicators and key data elements is legible at minimum `--pf-v5-global--FontSize--md` (14px) (UX-DR17)

---

## Epic 7: OCP Console — Execution & DR Operations

Operators can trigger failover, planned migration, re-protect, and failback from the Console with pre-flight confirmation dialogs, safety keyword input, live execution monitoring, inline retry of failed groups, and bridge-call-ready summaries.

### Story 7.1: Pre-flight Confirmation & Failover Trigger

As an operator,
I want a pre-flight confirmation dialog showing VM count, RPO estimate, RTO estimate, capacity, and a safety keyword input before any destructive action,
So that I act with full confidence and never trigger failover accidentally.

**Acceptance Criteria:**

**Given** a DRPlan detail page with a valid transition action on the DRLifecycleDiagram (Failover, Planned Migration, Reprotect, Failback, or Restore)
**When** the operator clicks the action button on the lifecycle diagram's outgoing transition arrow
**Then** a PatternFly Modal (large variant, ~800px) opens with a structured pre-flight summary (FR37, UX-DR5)

**Given** the pre-flight modal for a disaster failover
**When** it renders
**Then** it displays:
- VM count and wave count (e.g., "12 VMs across 3 waves")
- Estimated RPO prominently at `--pf-v5-global--FontSize--2xl` bold (time since last replication sync)
- Estimated RTO based on last execution duration (e.g., "~18 min based on last execution")
- DR site capacity assessment (sufficient / warning)
- Summary of actions to be performed ("Force-promote volumes on DC2, start VMs wave by wave")
**And** RPO is the single most visually prominent number in the dialog (UX-DR5)

**Given** the pre-flight modal for a planned migration
**When** it renders
**Then** RPO shows "0 — guaranteed (both DCs up, final sync before promote)"
**And** the summary includes Step 0: "Stop VMs on origin → wait for final sync → promote on target"

**Given** the pre-flight modal
**When** it renders the confirmation input
**Then** a TextInput field displays with the instruction: "Type FAILOVER to confirm" (or MIGRATE / REPROTECT / FAILBACK depending on the action) (FR38, UX-DR5)
**And** the Confirm button is disabled until the keyword matches exactly (case-sensitive)
**And** the keyword matches the action name per UX-DR19: danger variant (red) for FAILOVER, primary variant for all others

**Given** the confirmation keyword is entered correctly
**When** the operator clicks Confirm
**Then** a DRExecution resource is created via the Kubernetes API with the appropriate mode
**And** the modal closes and the Overview tab shows the transition in-progress state (progress banner, "In progress..." on the transition arrow, dashed border on destination phase)
**And** the pre-flight modal is the only confirmation — no cascading "Are you sure?" dialogs

**Given** the modal
**When** the operator presses Escape or clicks Cancel
**Then** the modal closes with no side effects

**Given** the pre-flight modal
**When** tested for accessibility
**Then** focus is trapped in the modal and auto-focused on the first element
**And** the confirmation field has a clear label and screen reader announcement
**And** keyboard navigation: Tab through summary → input → Confirm/Cancel buttons

### Story 7.2: Live Execution Monitor (ProgressStepper)

As an operator,
I want a wave-level progress view with expandable per-DRGroup detail updating in real time via Kubernetes watch,
So that I can monitor execution progress and share it on a bridge call.

**Acceptance Criteria:**

**Given** an active DRExecution (triggered from Story 7.1 or via History tab)
**When** the Execution Monitor page loads
**Then** a full-width view renders with a PatternFly ProgressStepper showing waves as sequential steps (UX-DR6)
**And** the header shows: execution name, mode (Disaster Failover / Planned Migration), start time, elapsed time (counting), and estimated remaining time

**Given** the execution monitor during an active execution
**When** data updates arrive via `useK8sWatchResource` on the DRExecution resource
**Then** the view updates within 5 seconds of the underlying state change (NFR7)
**And** no manual refresh is needed

**Given** a wave in Pending state
**When** rendered
**Then** it shows as gray with dimmed text, expandable to see pending DRGroups

**Given** a wave in InProgress state
**When** rendered
**Then** it shows with a blue animated indicator and bold text
**And** it is auto-expanded to show DRGroup detail
**And** each DRGroup shows: VM names, status (Pending / InProgress with spinner / Completed with checkmark / Failed with error icon), and elapsed time

**Given** a wave that has Completed
**When** rendered
**Then** it shows as green with a checkmark — a visible relief milestone
**And** it is collapsible to reduce visual noise

**Given** all waves completed
**When** the execution finishes
**Then** the header shows: total duration, final result badge (Succeeded / PartiallySucceeded / Failed), and total RPO
**And** the elapsed time counter stops

**Given** the execution monitor
**When** viewed at 720p screen-share resolution (UX-DR17)
**Then** all text is legible at minimum 14px, critical numbers (RPO, time, VM count) at 18px+
**And** elapsed and remaining time use a monospace font variant for stable-width display (no layout shift)
**And** animations are subtle — no distracting motion during bridge calls

**Given** the execution monitor
**When** an ARIA live region is configured
**Then** screen readers announce wave completion events: "Wave 1 completed. Wave 2 starting." (UX-DR16)

### Story 7.3: Inline Error Display & DRGroup Retry

As an operator,
I want failed DRGroups highlighted with the error message and a Retry button inline in the execution monitor,
So that I can recover from failures without leaving the view.

**Acceptance Criteria:**

**Given** a DRGroup that has failed during execution
**When** the execution monitor renders
**Then** the failed DRGroup shows: red text/icon, error message inline, affected VM names, and the step where failure occurred
**And** a "Retry" button (PatternFly Button, variant="primary") appears inline next to the failed group (FR39)

**Given** the Retry button on a failed DRGroup
**When** the operator clicks it
**Then** the orchestrator validates retry preconditions (VM in healthy state — from Story 4.6)
**And** if preconditions pass, the DRGroup status transitions to InProgress with a pulsing blue indicator
**And** no separate confirmation dialog is needed for retry (it's a secondary action, not destructive)

**Given** the Retry button on a failed DRGroup where preconditions fail
**When** the operator clicks it
**Then** an inline error message appears: "Cannot retry — VM <name> is in an unpredictable state. Manual intervention required."
**And** the Retry button remains visible but the error guides the operator

**Given** a successful retry
**When** the retried DRGroup completes
**Then** its status changes from Failed → InProgress → Completed (green)
**And** if all DRGroups are now Completed, the DRExecution result updates from PartiallySucceeded to Succeeded

**Given** the Retry button
**When** navigating via keyboard
**Then** the button is focusable via Tab from the failed DRGroup context
**And** focus moves to the Retry button automatically when a DRGroup fails (UX-DR16)

### Story 7.4: Toast Notifications & Execution Summary

As an operator,
I want toast notifications for execution lifecycle events and a bridge-call-ready completion summary,
So that I stay informed and can report precise results to stakeholders.

**Acceptance Criteria:**

**Given** the notification system using PatternFly AlertGroup (toast variant) (UX-DR13)
**When** an execution starts
**Then** an info toast appears: "Failover started for erp-full-stack" (auto-dismiss after 8 seconds)
**And** the toast includes a link to the execution monitor

**Given** an execution that completes successfully
**When** the result is Succeeded
**Then** a success toast appears: "Failover completed: 12 VMs recovered in 17 min" (auto-dismiss after 15 seconds)

**Given** an execution that completes with partial failure
**When** the result is PartiallySucceeded
**Then** a warning toast appears: "Failover partially succeeded: 1 DRGroup failed — [View Details]" (persistent until dismissed) (UX-DR13)
**And** the "[View Details]" link navigates to the execution monitor

**Given** a re-protect that completes
**When** replication returns to healthy
**Then** a success toast appears: "Re-protect complete: replication healthy" (auto-dismiss after 8 seconds)

**Given** the execution monitor after completion
**When** the summary section renders
**Then** a bridge-call-ready summary is displayed at `--pf-v5-global--FontSize--xl` using plain language (UX-DR17):
- "12 VMs recovered in 17 minutes"
- "RPO: 47 seconds"
- "Result: Succeeded" (or "11 of 12 VMs recovered — 1 DRGroup failed")
**And** the summary is designed to be read aloud on a bridge call

**Given** all toast notifications
**When** rendered
**Then** each toast includes a link to the relevant plan detail or execution monitor
**And** toasts stack correctly when multiple appear simultaneously
**And** screen readers announce toast content via ARIA live regions

## Epic 8: Cross-Site Discovery Agreement, API Simplification & UI Responsiveness

Both Soteria instances independently discover VMs on their local cluster and report to dedicated per-site status fields. Plans are validated and waves formed only when both sites agree on the VM inventory; otherwise the plan is blocked with a clear delta report. The `waveLabel` spec field is removed (wave label is always `soteria.io/wave` by convention). The Console plugin provides immediate visual feedback when an execution is triggered.

### Story 8.1: Remove `waveLabel` from `DRPlanSpec`

As a platform engineer,
I want the wave label key to be a fixed convention (`soteria.io/wave`) rather than a configurable spec field,
So that the API is simpler and there is no ambiguity about which label assigns VMs to waves.

**Acceptance Criteria:**

**Given** the `DRPlanSpec` type in `pkg/apis/soteria.io/v1alpha1/types.go`
**When** the `WaveLabel` field is removed
**Then** a new exported constant `WaveLabel = "soteria.io/wave"` is added alongside the existing `DRPlanLabel` constant
**And** `WaveLabel` follows the same naming convention as `DRPlanLabel` (e.g., `soteria.io/<kebab-case>`)

**Given** the `GroupByWave` function in `pkg/engine/discovery.go`
**When** it partitions VMs into waves
**Then** it uses the `WaveLabel` constant directly instead of accepting a `waveLabel` parameter
**And** all callers (reconciler, consistency resolver) are updated to drop the parameter

**Given** the admission webhook in `pkg/admission/vm_validator.go`
**When** it validates wave consistency for sibling VMs
**Then** it uses the `WaveLabel` constant instead of reading `plan.Spec.WaveLabel`

**Given** an existing DRPlan resource that has `waveLabel` set in its spec
**When** it is updated via the API
**Then** `PrepareForUpdate` silently strips the `waveLabel` field (backward compatibility)
**And** `PrepareForCreate` also strips it on new resources that include it

**Given** the DRPlan sample YAML in `config/samples/`
**When** updated
**Then** the `waveLabel` field is removed from the example
**And** a comment explains that the wave label is always `soteria.io/wave`

**Given** all existing tests that reference `spec.waveLabel` or pass a wave label parameter
**When** updated
**Then** they use the `WaveLabel` constant
**And** all unit and integration tests pass with zero regressions

### Story 8.2: Per-Site VM Discovery Reporting

As a platform engineer,
I want each Soteria instance to discover VMs on its local cluster and report them to a dedicated per-site status section,
So that the system has visibility into which VMs exist on each site independently.

**Acceptance Criteria:**

**Given** the `DRPlanStatus` type
**When** per-site discovery fields are added
**Then** two new explicit fields are added: `primarySiteDiscovery` and `secondarySiteDiscovery`, each of type `SiteDiscovery`
**And** `SiteDiscovery` contains: `vms []DiscoveredVM`, `discoveredVMCount int`, and `lastDiscoveryTime metav1.Time`

**Given** the DRPlan reconciler on the **active** site (where `LocalSite == activeSite`)
**When** it discovers VMs
**Then** it writes the discovered VMs to the `SiteDiscovery` field matching its site role (primary or secondary based on `spec.primarySite`/`spec.secondarySite` vs `LocalSite`)
**And** it continues to form waves and update `status.waves` as before (wave formation behavior is unchanged in this story)

**Given** the DRPlan reconciler on the **passive** site (where `LocalSite != activeSite`)
**When** it runs
**Then** it discovers VMs labeled `soteria.io/drplan=<planName>` on its local cluster
**And** it writes the discovered VMs to its corresponding `SiteDiscovery` field
**And** it does **not** modify `status.waves`, `status.discoveredVMCount`, `status.replicationHealth`, or any other status fields owned by the active site
**And** it continues to requeue at 30-second intervals

**Given** the status subresource strategy for DRPlan
**When** both controllers write their per-site discovery concurrently
**Then** each controller uses a strategic merge patch scoped to its own `SiteDiscovery` field
**And** writes do not clobber each other even with ScyllaDB's eventual consistency

**Given** the `SiteDiscovery.lastDiscoveryTime` field
**When** a controller completes a discovery cycle
**Then** it updates `lastDiscoveryTime` to the current time
**And** this timestamp is visible in the plan status for debugging staleness

**Given** all existing tests for the DRPlan reconciler
**When** updated
**Then** site-aware tests verify both active and passive controllers write to their respective `SiteDiscovery` fields
**And** tests verify the passive controller does not modify wave, health, or preflight status
**And** all unit and integration tests pass

### Story 8.3: Cross-Site VM Agreement & Plan Readiness Gating

As a platform engineer,
I want plan validation and wave formation to require both sites to agree on the discovered VM set,
So that executions never proceed against inconsistent infrastructure where VMs are missing from one site.

**Acceptance Criteria:**

**Given** the DRPlan reconciler on the active site
**When** it has both `primarySiteDiscovery` and `secondarySiteDiscovery` populated
**Then** it compares the VM sets by `{name, namespace}` tuples (order-independent)
**And** if both sets are identical, it proceeds with wave formation as before

**Given** that both site discovery VM sets are identical
**When** the reconciler forms waves
**Then** a condition `SitesInSync` is set to `True` with reason `VMsAgreed`
**And** the `Ready` condition is evaluated as before (depends on `SitesInSync` among other checks)

**Given** that the site discovery VM sets differ
**When** the reconciler compares them
**Then** a condition `SitesInSync` is set to `False` with reason `VMsMismatch`
**And** the condition message lists the delta: "VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]"
**And** `status.waves` is cleared (no valid wave formation with inconsistent VMs)
**And** the `Ready` condition transitions to `False` with message "Plan blocked: sites do not agree on VM inventory"

**Given** a DRPlan where `SitesInSync` is `False`
**When** a user attempts to create a DRExecution for that plan
**Then** the admission webhook rejects the creation with a clear error: "Cannot start execution: sites do not agree on VM inventory. Resolve VM differences first."
**And** this applies to **all** execution modes (planned migration, disaster, and reprotect) — no exceptions

**Given** that one site's `SiteDiscovery` has an empty VM list (discovery returned zero VMs) while the other has VMs
**When** the reconciler evaluates agreement
**Then** `SitesInSync` is set to `False`
**And** the message indicates the empty-discovery site and suggests checking VM labels

**Given** that one site's `SiteDiscovery` is not yet populated (`lastDiscoveryTime` is zero)
**When** the reconciler evaluates agreement
**Then** it treats this as "waiting for discovery" — `SitesInSync` is `False` with reason `WaitingForDiscovery`
**And** the message indicates which site has not yet reported
**And** this is the expected initial state on first deployment

**Given** the preflight report
**When** generated with `SitesInSync` information
**Then** it includes a new `sitesInSync` boolean field and a `siteDiscoveryDelta` summary
**And** the preflight warning section lists any VM mismatches

**Given** all existing tests
**When** updated for the agreement logic
**Then** table-driven tests cover: both agree, primary-only VMs, secondary-only VMs, both-side extra VMs, one side empty, one side not yet discovered
**And** admission webhook tests verify rejection when `SitesInSync` is `False`

### Story 8.4: Console UI — Site-Aware Plan Status & Disagreement Display

As an operator,
I want the Console to show per-site VM inventory and clearly indicate when sites disagree,
So that I can identify and resolve VM provisioning gaps before attempting a DR operation.

**Acceptance Criteria:**

**Given** the DRPlan detail page Configuration tab
**When** `primarySiteDiscovery` and `secondarySiteDiscovery` are both populated
**Then** a "Site Discovery" section shows two columns: primary site VMs and secondary site VMs
**And** each column shows the site name, VM count, and last discovery timestamp
**And** matching VMs are shown in default style; VMs present on only one site are highlighted with a warning indicator

**Given** the `SitesInSync` condition is `False`
**When** the plan detail page renders
**Then** a PatternFly Alert (variant=danger, inline) appears prominently above the lifecycle diagram: "Sites do not agree on VM inventory — DR operations are blocked"
**And** the alert includes an `AlertActionLink` that scrolls to or switches to the Configuration tab's site discovery section
**And** the delta is summarized in the alert: e.g., "2 VMs on primary not found on secondary"

**Given** the `SitesInSync` condition is `False`
**When** the lifecycle diagram renders action buttons
**Then** all transition action buttons are disabled
**And** each disabled button shows a tooltip: "Blocked: sites do not agree on VM inventory"

**Given** the DR Dashboard table
**When** a plan has `SitesInSync: False`
**Then** the plan row shows a warning icon in the health/status column
**And** the kebab menu actions are disabled with a tooltip indicating the plan is blocked

**Given** the `SitesInSync` condition transitions from `False` to `True`
**When** the watch update arrives
**Then** the danger alert disappears
**And** action buttons become enabled
**And** the site discovery section shows all VMs as matching

**Given** the site discovery section
**When** `lastDiscoveryTime` for one site is older than 5 minutes
**Then** a subtle warning shows: "Discovery data from <site> is stale (last updated <time>)"

**Given** the site-aware UI
**When** tested for accessibility
**Then** the danger alert uses ARIA live region to announce blocking state changes
**And** the per-site VM comparison table is keyboard navigable
**And** warning indicators have screen reader text explaining the mismatch

### Story 8.5: Optimistic DRExecution Detection in Console

As an operator,
I want immediate visual feedback on the plan detail page when I trigger a DR execution,
So that the UI feels responsive and I know my action was registered without waiting for the controller.

**Acceptance Criteria:**

**Given** the DRPlan detail page
**When** the operator confirms an action in the pre-flight modal and `k8sCreate` succeeds
**Then** the transition progress banner appears immediately (within the same render cycle) without waiting for `plan.status.activeExecution` to update
**And** the banner shows: "Starting <mode>..." with the mode label and a spinning indicator
**And** this optimistic state is driven by local React state set in the `k8sCreate` success handler

**Given** the optimistic "Starting..." banner is displayed
**When** the real `plan.status.activeExecution` watch update arrives (typically 1-3 seconds later)
**Then** the optimistic state is replaced by the real execution data
**And** the banner transitions smoothly to show actual wave progress
**And** there is no visual flash or double-render between optimistic and real states

**Given** `k8sCreate` succeeds but the controller has not yet updated the plan
**When** the user navigates away from the plan detail page and returns
**Then** the plan detail page uses the standard watch-driven mechanism (no optimistic state is persisted across navigations)
**And** once the controller updates `activeExecution`, the transition banner appears normally

**Given** `k8sCreate` fails
**When** the error is returned
**Then** no optimistic banner is shown
**And** the error is displayed in the pre-flight modal as before (existing behavior)

**Given** the optimistic state
**When** the real execution data arrives and the execution is already showing wave progress
**Then** the `useDRExecution(activeExecName)` hook provides live data
**And** the elapsed time counter starts from the execution's actual `startTime`

**Given** the optimistic execution detection
**When** tested
**Then** tests verify the immediate banner render after mock `k8sCreate` resolves
**And** tests verify the transition from optimistic to real state
**And** tests verify no optimistic state on `k8sCreate` failure
**And** jest-axe accessibility audit passes on the optimistic banner state

## Epic 9: Disk-Level Discovery, Volume Group Enrichment & Structural Validation

VM discovery is enriched with per-disk PVC topology (disk name, PVC name, storage class) extracted from VM specs. Each site reports disk details in its `SiteDiscovery` section, enabling cross-site structural validation: VMs must have matching disk count, disk names, and storage classes on both sites (PVC names may differ). Volume groups in the preflight report are enriched with disk-to-PVC mappings per site. A new `DisksConsistent` condition (separate from `SitesInSync`) gates execution when disk topology disagrees or when a volume group contains disks from mixed storage classes. DRExecution and DRPlan admission validation is migrated from controller-runtime webhooks (which do not fire for aggregated API resources) to an in-process admission plugin within the aggregated API server, and unused `ValidatingWebhookConfiguration` entries for `soteria.io` resources are removed. The Console plugin surfaces disk details in site discovery comparison, wave composition, and validation alerts.

### Story 9.1: Disk Discovery in SiteDiscovery

As a platform engineer,
I want each site's VM discovery to include per-disk PVC topology (disk name, PVC name, storage class),
So that the system has visibility into the storage layout of each VM for cross-site validation.

**Acceptance Criteria:**

**Given** the `DiscoveredVM` type in `pkg/apis/soteria.io/v1alpha1/types.go`
**When** enriched with disk information
**Then** a new `DiscoveredDisk` type is added with fields: `name` (from `domain.devices.disks[*].name`), `pvcName` (resolved PVC claim name), and `storageClass` (from `PVC.spec.storageClassName`)
**And** `DiscoveredVM` gains a `disks []DiscoveredDisk` field (omitempty)
**And** `make manifests generate` regenerates deepcopy and OpenAPI

**Given** the DRPlan reconciler on either site (active or passive)
**When** it discovers VMs
**Then** for each VM, it joins `spec.template.spec.domain.devices.disks[*]` with `spec.template.spec.volumes[*]` by name
**And** it filters for volumes backed by `persistentVolumeClaim` or `dataVolume` (all other volume types are silently ignored)
**And** for each matched pair, it resolves the PVC (using `vol.PersistentVolumeClaim.ClaimName` or `vol.DataVolume.Name` as the PVC name) via the cached `client.Reader` and reads `storageClassName`
**And** the resolved disks are written to `SiteDiscovery.VMs[*].disks`

**Given** a VM with only non-PVC volumes (containerDisk, cloudInitNoCloud, etc.)
**When** disk enrichment runs
**Then** the VM's `disks` field is empty (`[]` or nil) — this is valid and does not produce an error

**Given** a VM whose DataVolume has not yet created its PVC
**When** disk enrichment runs
**Then** the disk entry is recorded with an empty `pvcName` and empty `storageClass` (the PVC GET returns NotFound)
**And** on the next reconcile cycle when the PVC exists, the disk entry is populated — self-healing via the reconcile loop

**Given** the PVC resolution during discovery
**When** reading PVCs
**Then** the reconciler uses the controller-runtime cached `client.Reader` (informer-backed) instead of direct `CoreV1Interface` API calls
**And** this eliminates per-PVC API server round-trips during reconcile

**Given** `WaveInfo.VMs` in `DRPlanStatus`
**When** populated during active-site reconcile
**Then** the `DiscoveredVM` entries in waves also carry the enriched `disks` field (same type, same data as SiteDiscovery)

**Given** all existing tests
**When** updated
**Then** new tests verify disk enrichment for PVC volumes, DataVolume volumes, mixed volumes, and no-PVC VMs
**And** all unit and integration tests pass with zero regressions

### Story 9.2: Aggregated API Admission Plugin Migration

As a platform engineer,
I want DRExecution and DRPlan admission validation to run in-process within the aggregated API server,
So that cross-object checks (concurrency gate, phase transition, SitesInSync, DisksConsistent) are enforced reliably without depending on kube-apiserver ValidatingWebhookConfiguration — which does not fire for aggregated API resources.

**Acceptance Criteria:**

**Given** the Soteria aggregated API server in `pkg/apiserver/`
**When** a custom admission plugin is registered
**Then** a new `pkg/admission/plugin.go` (or equivalent) implements `k8s.io/apiserver/pkg/admission.ValidationInterface` with the same validation logic currently in `DRExecutionValidator.Handle` and `DRPlanValidator.Handle`
**And** the plugin is registered with the `genericapiserver.RecommendedConfig` admission chain via `admission.NewFromPlugins` or `Plugins.Register`
**And** the plugin runs in-process during the request lifecycle — no HTTP roundtrip to a webhook server

**Given** a DRExecution CREATE request through the aggregated API
**When** the admission plugin runs
**Then** it performs all cross-object checks currently in `DRExecutionValidator`: planName required, mode valid, DRPlan exists, `ActiveExecution != ""` concurrency gate, phase transition valid (`engine.Transition`), `SitesInSync=False` blocks
**And** it uses an uncached reader (or direct storage lookup) to ensure fresh DRPlan state
**And** all existing admission test scenarios pass against the new in-process path

**Given** a DRPlan CREATE or UPDATE request through the aggregated API
**When** the admission plugin runs
**Then** it performs the same field validation as `DRPlanValidator` (calling `ValidateDRPlan`/`ValidateDRPlanUpdate`)
**And** this is defense-in-depth alongside the existing registry strategy validation

**Given** the `ValidatingWebhookConfiguration` in `config/webhook/manifests.yaml`
**When** updated
**Then** the webhook entries for `soteria.io` resources (`drplans` and `drexecutions`) are removed — they were never effective for aggregated API traffic
**And** the `kubevirt.io/virtualmachines` webhook entry is retained — VMs are standard CRDs and the kube-apiserver VWC path works correctly for them
**And** the `+kubebuilder:webhook` markers on `DRPlanValidator` and `DRExecutionValidator` are removed (or the structs are refactored) so `make manifests` no longer regenerates the dead entries

**Given** the controller-runtime webhook server setup in `pkg/admission/setup.go`
**When** updated
**Then** `SetupDRPlanWebhook` and `SetupDRExecutionWebhook` are removed (no longer needed — validation runs in the aggregated API server)
**And** `SetupVMWebhook` is retained (VMs still use the controller-runtime webhook path)
**And** `main.go` is updated to no longer call the removed setup functions

**Given** the webhook TLS and cert-manager configuration
**When** the soteria.io webhook entries are removed
**Then** the webhook Service, cert-manager Certificate, and CA injection for the `ValidatingWebhookConfiguration` are reviewed — if only the VM webhook remains, the webhook infrastructure is still needed for that single webhook
**And** `config/default/kustomization.yaml` patches are updated if any references change

**Given** all existing tests
**When** updated
**Then** new unit tests verify the admission plugin rejects/allows the same scenarios as the current webhook tests (table-driven, same test cases)
**And** integration tests verify the admission plugin runs in-process within the aggregated API server (envtest or equivalent)
**And** the `test/integration/admission/suite_test.go` DRPlan webhook install is updated or removed (DRPlan validation now runs in the aggregated API server, not via VWC)
**And** all unit and integration tests pass with zero regressions

### Story 9.3: Cross-Site Disk Agreement Validation

As a platform engineer,
I want the system to validate that each VM has the same disk topology (count, names, storage classes) across both sites,
So that executions never proceed when storage layout is inconsistent between sites.

**Acceptance Criteria:**

**Given** both `primarySiteDiscovery` and `secondarySiteDiscovery` populated with disk data
**When** the active-site reconciler evaluates cross-site agreement
**Then** for each VM present on both sites, it compares: disk count, sorted disk names, and per-disk `storageClass`
**And** the `pvcName` field is explicitly excluded from the cross-site comparison (PVC names may differ across sites due to CDI cloning, DataVolume imports, etc.)

**Given** all VMs have matching disk topology across both sites
**When** the reconciler evaluates disk agreement
**Then** a new condition `DisksConsistent` is set to `True` with reason `DisksAgreed`

**Given** one or more VMs have mismatched disk topology (different count, missing disks, or different storage classes)
**When** the reconciler evaluates disk agreement
**Then** `DisksConsistent` is set to `False` with reason `DiskMismatch`
**And** the condition message lists the delta per VM: e.g., "VM default/web01: primary disks [rootdisk(ceph-rbd), datadisk(ceph-rbd)] vs secondary disks [rootdisk(ceph-rbd)] — count mismatch"
**And** messages are capped (reusing `writeCappedList` pattern)
**And** `Ready` transitions to `False`

**Given** that disk data is not yet available from one or both sites (disks nil/empty on `DiscoveredVM`)
**When** the reconciler evaluates disk agreement
**Then** `DisksConsistent` is `False` with reason `WaitingForDiskDiscovery`
**And** the reconciler proceeds with wave formation (same pattern as `WaitingForDiscovery` for VMs — no blocking on first deploy)

**Given** a DRPlan where `DisksConsistent` is `False`
**When** a user attempts to create a DRExecution
**Then** the admission webhook rejects the creation: "Cannot start execution: disk topology does not match across sites. Resolve disk differences first."
**And** this applies to all execution modes

**Given** all existing tests
**When** updated
**Then** table-driven tests cover: disks match, disk count mismatch, disk name mismatch, storage class mismatch, mixed mismatches, one side no disks yet
**And** admission webhook tests verify rejection when `DisksConsistent` is `False`
**And** all unit and integration tests pass

### Story 9.4: Volume Group Disk Enrichment in Preflight

As a platform engineer,
I want the preflight report's volume group entries to show which disks and PVCs belong to each group,
So that I can verify the storage composition of each volume group before execution.

**Acceptance Criteria:**

**Given** the `PreflightChunk` type
**When** the volume group representation is enriched
**Then** `PreflightChunk.VolumeGroups` changes from `[]string` to `[]PreflightVolumeGroup` (v1alpha1 breaking change)
**And** `PreflightVolumeGroup` has fields: `name` (string), `site` (string — which site this view is from), `disks []VolumeGroupDisk`
**And** `VolumeGroupDisk` has fields: `name` (disk name), `pvcName` (PVC name), `pvcNamespace` (PVC namespace)

**Given** a volume group with VM-level consistency
**When** the preflight report is composed
**Then** the `PreflightVolumeGroup.disks` contains all disks from that single VM (sourced from `DiscoveredVM.disks`)
**And** `site` is populated from `r.LocalSite`

**Given** a volume group with namespace-level consistency
**When** the preflight report is composed
**Then** the `PreflightVolumeGroup.disks` contains all disks from all member VMs in that namespace
**And** disks are sorted by VM name then disk name for deterministic output

**Given** the preflight composition pipeline
**When** building `PreflightVolumeGroup` entries
**Then** VG-to-disk mapping is derived from `DiscoveredVM.disks` + VG membership (`VMNames` on `VolumeGroupInfo`)
**And** no additional PVC GETs are needed (all data comes from the already-enriched discovery)

**Given** the Console plugin TypeScript interfaces
**When** the `PreflightChunk` type changes
**Then** the Console plugin TypeScript types are updated to reflect the new `PreflightVolumeGroup` shape

**Given** all existing tests
**When** updated
**Then** tests verify VG disk enrichment for VM-level and namespace-level groups
**And** all unit and integration tests pass

### Story 9.5: Storage Class Homogeneity Validation

As a platform engineer,
I want the system to validate that all disks within a volume group use the same storage class,
So that volume group operations are guaranteed to target a single storage driver.

**Acceptance Criteria:**

**Given** a volume group where all member disks have the same `storageClass`
**When** the reconciler validates VG composition
**Then** validation passes and the `DisksConsistent` condition is not degraded by this check

**Given** a volume group where member disks have different storage classes
**When** the reconciler validates VG composition
**Then** `DisksConsistent` is set to `False` with reason `StorageClassMixed`
**And** the condition message identifies the VG and the conflicting classes: e.g., "Volume group ns-erp-database: mixed storage classes [ceph-rbd, local-path] — all disks must use the same storage class"
**And** `Ready` transitions to `False`
**And** wave formation is blocked

**Given** the validation runs after VG formation
**When** VGs are built from `ResolveVolumeGroups`
**Then** the storage class check iterates all VGs, collects distinct storage classes from `DiscoveredVM.disks` for member VMs, and fails if any VG has > 1 distinct class

**Given** a VG where some member VMs have no disks (stateless VMs)
**When** the storage class check runs
**Then** VMs with no disks are skipped — they do not contribute to storage class counting
**And** if the VG has only stateless VMs (all disks empty), validation passes trivially

**Given** all existing tests
**When** updated
**Then** tests cover: homogeneous VG passes, heterogeneous VG fails, mixed with stateless VMs, namespace-level multi-VM VG
**And** all unit and integration tests pass

### Story 9.6: Console UI — Disk Discovery & Validation Display

As an operator,
I want the Console to show per-VM disk details, volume group disk composition, and disk validation alerts,
So that I can understand and troubleshoot disk-level DR protection.

**Acceptance Criteria:**

**Given** the DRPlan detail page Configuration tab's Site Discovery section
**When** per-VM disk data is available in `SiteDiscovery`
**Then** each VM row in the two-column comparison expands to show its disks (disk name, PVC name, storage class)
**And** disk mismatches between sites are highlighted (different count, missing disk, different storage class)
**And** matching disks are shown in default style

**Given** the Waves tab's WaveCompositionTree
**When** volume group nodes are rendered
**Then** each volume group node shows its `PreflightVolumeGroup.disks` (disk name, PVC name, PVC namespace)
**And** the site label is shown on the volume group

**Given** the `DisksConsistent` condition is `False`
**When** the plan detail page renders
**Then** a PatternFly Alert (variant=danger, inline) appears alongside the existing `SitesInSync` alert: "Disk topology does not match across sites — DR operations are blocked"
**And** the alert message includes the condition's delta details
**And** the alert includes an `AlertActionLink` to the Configuration tab's site discovery section

**Given** the `DisksConsistent` condition is `False` with reason `StorageClassMixed`
**When** the plan detail page renders
**Then** the alert message indicates the mixed storage class issue: "Volume group <name> has mixed storage classes — all disks must use the same storage class"

**Given** the DRLifecycleDiagram
**When** `DisksConsistent` is `False`
**Then** all transition action buttons are disabled with tooltip: "Blocked: disk topology inconsistent across sites"
**And** `isBlocked` check is extended to include `DisksConsistent` alongside `SitesInSync`

**Given** the DR Dashboard table
**When** a plan has `DisksConsistent: False`
**Then** the plan row shows a warning icon
**And** kebab menu actions are disabled with tooltip indicating disk inconsistency

**Given** all new UI components
**When** tested for accessibility
**Then** jest-axe audit passes on all states (disks match, disk mismatch, storage class mixed)
**And** disk comparison tables are keyboard navigable
**And** mismatch indicators have screen reader text

### Story 9.7: Cross-Site Volume Group Disk Mapping (spawned from UAT-9.001)

As a platform engineer,
I want each disk in a PreflightVolumeGroup to show its PVC mapping on both sites,
So that I can verify cross-site disk correspondence before triggering a DR operation.

**Background:** Story 9.4 introduced per-disk PVC enrichment in the preflight report, but only from the active site's perspective. UAT-9.001 identified that users need to see which PVC on the partner site corresponds to each local disk.

#### Acceptance Criteria

**AC1: DiskSiteMapping API type**
**Given** the v1alpha1 API types
**When** a `PreflightVolumeGroup` is serialized
**Then** each `VolumeGroupDisk` contains a `sites` array of `DiskSiteMapping` entries (site, pvcName, pvcNamespace)
**And** the top-level `Site` field is removed from `PreflightVolumeGroup`

**AC2: Cross-site enrichment in preflight composition**
**Given** a DRPlan with both primarySiteDiscovery and secondarySiteDiscovery populated
**When** the preflight report is composed
**Then** each disk in a VolumeGroup has entries for both sites (matched by disk name)
**And** a site entry is omitted only when that site's SiteDiscovery is nil (e.g., first reconcile)

**AC3: Console WaveCompositionTree cross-site rendering**
**Given** the WaveCompositionTree component receives enriched VolumeGroups
**When** expanding a VG disk node
**Then** each disk shows per-site PVC mapping (site label + PVC name + namespace)

**AC4: Backward compatibility**
**Given** a DRPlan created before this change (single-site `Site` + flat `PVCName/PVCNamespace`)
**When** the console plugin reads the preflight data
**Then** the old format is rendered gracefully (fallback to single-site view)

**AC5: Tests**
**Given** the preflight enrichment logic and console components
**When** unit tests run
**Then** enrichVolumeGroup tests validate cross-site output
**And** WaveCompositionTree tests validate per-site disk rendering
**And** all existing tests pass with zero regressions

#### Technical Notes

- `CompositionInput` gains access to both SiteDiscovery objects (already on `plan.Status`)
- `enrichVolumeGroup` builds disk index from both sites, matches disks by name, populates `Sites` slice
- Console `buildVGDiskNodes` renders per-site rows instead of flat `disk.pvcName`
- Scope: ~3 modified Go files, ~3 modified TS files, ~2 test files

## Epic 10: Remove ActiveExecution from DRPlan — Static Plan Status

The `ActiveExecution` and `ActiveExecutionMode` fields are removed from `DRPlanStatus`, making the DRPlan a static configuration and discovery artifact that is never mutated by execution lifecycle events. Active execution state is derived at runtime by querying DRExecution resources filtered by `spec.planName`. Concurrency control moves to the DRExecution creation path (aggregated API admission plugin or registry strategy) using a storage-level guard. The table convertor, replication health polling gate, VM watch routing, preflight warnings, and Console plugin are all migrated to the derived pattern. Cross-DC SERIAL consistency triggers move from DRPlan status writes to DRExecution creation/completion writes.

### Story 10.1: DRExecution Concurrency Guard Without ActiveExecution

As a platform engineer,
I want DRExecution creation to be rejected when another active execution exists for the same plan, without relying on a field stored on the DRPlan,
So that the DRPlan status is never mutated by execution lifecycle and the concurrency invariant is maintained at the DRExecution layer.

**Acceptance Criteria:**

**Given** a DRExecution CREATE request for plan `my-plan`
**When** the aggregated API admission plugin (from Story 9.2) processes the request
**Then** it queries DRExecution storage for resources where `spec.planName == my-plan` and `status.result` is empty (no terminal result)
**And** if any non-terminal DRExecution exists, the CREATE is rejected with: "DRPlan <name> has active execution <exec-name>; concurrent executions not permitted"
**And** if no non-terminal DRExecution exists, the CREATE proceeds

**Given** two concurrent DRExecution CREATE requests for the same plan
**When** both arrive simultaneously
**Then** at most one succeeds — the guard uses SERIAL consistency (LWT) on the DRExecution write or an equivalent atomic check-and-create pattern to prevent races
**And** the rejected request receives a clear concurrency error

**Given** the DRExecution reconciler
**When** it starts processing a new execution
**Then** it no longer sets `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode` on the DRPlan
**And** execution ownership is established by the DRExecution resource's existence and non-terminal status alone

**Given** the DRExecution reconciler
**When** an execution completes (success, partial, or failure)
**Then** it no longer clears `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode` on the DRPlan
**And** the DRPlan status patch for phase advancement (`plan.Status.Phase`) and active site flip (`plan.Status.ActiveSite`) continues to work as before — only `ActiveExecution`/`ActiveExecutionMode` writes are removed

**Given** the `fetchPlanWithActiveExecCheck` function in the DRExecution reconciler
**When** it validates execution ownership
**Then** it is replaced by a pattern that verifies the current execution is the only non-terminal execution for the plan (query DRExecutions, not the plan's ActiveExecution field)

**Given** the ScyllaDB critical field detector for DRPlan
**When** `ActiveExecution` is no longer a field
**Then** the detector is updated to remove `ActiveExecution` from the comparison — phase and activeSite changes remain SERIAL
**And** DRExecution creation/completion writes use SERIAL consistency to ensure cross-DC safety on the concurrency guard

**Given** all existing tests
**When** updated
**Then** concurrency tests verify rejection of duplicate active executions via the new guard
**And** race condition tests verify at most one execution succeeds under concurrent creation
**And** all unit and integration tests pass with zero regressions

### Story 10.2: Derived Active Execution for Table Convertor & Preflight

As a platform engineer,
I want `kubectl get drplans` and the preflight report to derive active execution state from DRExecution resources,
So that the DRPlan status no longer needs `ActiveExecution` or `ActiveExecutionMode` fields.

**Acceptance Criteria:**

**Given** the DRPlan table convertor in `pkg/registry/drplan/strategy.go`
**When** rendering the "Effective Phase" column
**Then** it derives the effective phase by querying the DRExecution cache/informer for a non-terminal execution with `spec.planName == plan.Name`
**And** if one exists, `EffectivePhase(plan.Status.Phase, exec.Spec.Mode)` is computed
**And** if none exists, the rest phase is shown directly
**And** the "Active Execution" column shows the execution name from the query (or empty)

**Given** a LIST of N DRPlans via `kubectl get drplans`
**When** the table convertor renders rows
**Then** a single LIST of all non-terminal DRExecutions is performed and indexed by `spec.planName` — not N individual GETs
**And** performance is O(plans + executions), not O(plans * executions)

**Given** the preflight report composition in `internal/preflight/checks.go`
**When** building the report
**Then** `report.ActiveExecution` is populated by querying DRExecutions for the plan, not from `plan.Status.ActiveExecution`
**And** the "execution is active" warning is generated from the query result

**Given** the `PreflightReport` type
**When** `ActiveExecution` is populated from the derived query
**Then** the field continues to exist on `PreflightReport` (it's a report snapshot, not a plan status field) — only the data source changes

**Given** all existing tests
**When** updated
**Then** table convertor tests verify effective phase derivation from DRExecution fixtures
**And** preflight tests verify active execution warning from DRExecution fixtures
**And** all unit and integration tests pass

### Story 10.3: Derived Active Execution for Reconciler Gates & VM Watch Routing

As a platform engineer,
I want the DRPlan reconciler's replication health gate and the DRExecution reconciler's VM watch routing to derive active execution state from DRExecution resources,
So that no runtime code reads `plan.Status.ActiveExecution`.

**Acceptance Criteria:**

**Given** the DRPlan reconciler's replication health polling gate
**When** deciding whether to skip polling
**Then** it queries the DRExecution cache/informer for a non-terminal execution with `spec.planName == plan.Name`
**And** if one exists, polling is skipped (execution owns driver interactions)
**And** if none exists, polling proceeds as before

**Given** the `mapVMToDRExecution` handler in the DRExecution reconciler
**When** a VM change event fires
**Then** it determines the active execution for the VM's plan by querying DRExecutions (via label index `soteria.io/plan-name` or cache lookup), not from `plan.Status.ActiveExecution`
**And** if a non-terminal execution exists, it enqueues that execution for reconcile

**Given** the `reconcileSetup` function in the DRExecution reconciler
**When** a new execution begins
**Then** it no longer patches `plan.Status.ActiveExecution` or `plan.Status.ActiveExecutionMode`
**And** the plan's phase advancement and active site flip on completion continue to be patched (those are plan lifecycle state, not execution pointers)

**Given** all existing tests
**When** updated
**Then** health polling tests verify skip/proceed based on DRExecution query
**And** VM watch routing tests verify correct execution enqueue from DRExecution query
**And** reconciler setup tests no longer assert ActiveExecution/Mode writes
**And** all unit and integration tests pass

### Story 10.4: Remove ActiveExecution Fields from DRPlanStatus

As a platform engineer,
I want the `ActiveExecution` and `ActiveExecutionMode` fields removed from the `DRPlanStatus` type,
So that the DRPlan API clearly communicates that plans are static configuration and discovery artifacts.

**Acceptance Criteria:**

**Given** the `DRPlanStatus` type in `pkg/apis/soteria.io/v1alpha1/types.go`
**When** updated
**Then** the `ActiveExecution string` and `ActiveExecutionMode ExecutionMode` fields are removed
**And** `make manifests generate` regenerates deepcopy and OpenAPI
**And** the updated OpenAPI spec no longer includes these fields

**Given** the DRPlan registry strategy in `pkg/registry/drplan/strategy.go`
**When** `PrepareForCreate` initializes status
**Then** it no longer sets `ActiveExecution` or `ActiveExecutionMode` (the fields don't exist)

**Given** existing DRPlan resources stored in ScyllaDB that have `activeExecution` / `activeExecutionMode` JSON keys
**When** deserialized by the updated API server
**Then** unknown JSON fields are silently ignored (standard Kubernetes behavior for removed status fields)
**And** no migration is needed — the fields simply disappear from the schema

**Given** all production code and tests
**When** compiled
**Then** zero references to `DRPlanStatus.ActiveExecution` or `DRPlanStatus.ActiveExecutionMode` remain (stories 10.1–10.3 have already migrated all consumers)
**And** all unit and integration tests pass with zero regressions
**And** `make lint` passes

### Story 10.5: DRExecution IsTerminal Method

As a maintainer of the DR orchestrator codebase,
I want a single `IsTerminal()` method on `DRExecutionStatus` and all duplicated `Result == ""` / `Result != ""` checks refactored to use it,
So that "active" vs terminal execution semantics stay consistent across admission, controllers, registry, engine, and API server logic.

**Acceptance Criteria:**

**Given** `DRExecutionStatus` in `pkg/apis/soteria.io/v1alpha1/types.go`
**When** `IsTerminal()` is added as a value-receiver method returning `s.Result != ""`
**Then** all production call sites that check `Status.Result == ""` or `Status.Result != ""` for terminal semantics are refactored to use `!Status.IsTerminal()` or `Status.IsTerminal()`

**Given** the `Reconcile` idempotency guard that checks `Result == Succeeded || Result == Failed`
**When** refactored
**Then** `PartiallySucceeded` still reaches the retry path (not early-exited)

**Given** `detectDRExecutionCriticalFields` which compares `oldExec.Status.Result != newExec.Status.Result`
**When** reviewed
**Then** it is left untouched — pairwise comparison is not a terminality check

**Given** all unit and integration tests
**When** run after refactoring
**Then** all pass with zero regressions
**And** `make lint` passes

### Story 10.6: DRExecution Phase and IsActive Status Fields

As a platform engineer and console operator,
I want `DRExecution` status to expose an explicit lifecycle `Phase` and an `IsActive` flag alongside the existing `Result`,
So that execution state is self-describing in the API, in `kubectl get` table output, and for future consumers without re-deriving semantics from `Result` and timestamps alone.

**Acceptance Criteria:**

**Given** `DRExecutionStatus` in `pkg/apis/soteria.io/v1alpha1/types.go`
**When** updated
**Then** `Phase` (type `ExecutionPhase`, enum: Pending/Executing/Succeeded/PartiallySucceeded/Failed) is added with `json:"phase,omitempty"` and kubebuilder Enum validation
**And** `IsActive` (type `bool`) is added with `json:"isActive"` **without** `omitempty` so `false` is always serialized

**Given** all write paths (`PrepareForCreate`, `reconcileSetup`, `finishExecution`, `failExecution`, `reconcileReprotect`, `ExecuteRetry`)
**When** setting execution status
**Then** `Phase` and `IsActive` are set alongside existing `Result` and `CompletionTime` writes
**And** `PrepareForCreate` sets `Pending`/`true`, `reconcileSetup` sets `Executing`/`true`, terminal paths set terminal phase/`false`

**Given** the DRExecution table convertor in `pkg/registry/drexecution/storage.go`
**When** rendering `kubectl get drexecutions`
**Then** Phase and Active columns appear between Mode and Result (column order: Name, Plan, Mode, Phase, Active, Result, Duration, Age)

**Given** all unit and integration tests
**When** run after changes
**Then** all pass with zero regressions
**And** `make lint` passes

### Story 10.7: Console UI — Derived Active Execution State

As an operator,
I want the Console plugin to derive active execution state from DRExecution resources using the new `isActive` flag and to display execution `phase`,
So that the UI accurately reflects execution state from the source of truth with richer status information.

**Acceptance Criteria:**

**Given** the `DRPlanStatus` TypeScript interface in `console-plugin/src/models/types.ts`
**When** updated
**Then** the `activeExecution` and `activeExecutionMode` fields are removed from the interface

**Given** the `DRExecutionStatus` TypeScript interface
**When** updated
**Then** `phase` (string union) and `isActive` (boolean, non-optional) fields are added

**Given** the `getEffectivePhase` utility in `console-plugin/src/utils/drPlanUtils.ts`
**When** computing effective phase for a plan
**Then** it accepts the active DRExecution (or undefined) as a parameter instead of reading `plan.status.activeExecution` and `plan.status.activeExecutionMode`
**And** active execution detection uses `e.status?.isActive === true` (not `!e.status?.result`)

**Given** the DRPlan detail page
**When** rendering the transition progress banner
**Then** it derives `isInTransition` and the execution link from the DRExecution matched via `isActive === true`, not from `plan.status.activeExecution`

**Given** the DR Dashboard table
**When** rendering effective phase per plan row
**Then** it builds a client-side `planName → DRExecution` index of active executions (filtered by `isActive === true`)

**Given** the ExecutionHistoryTable
**When** rendering execution history
**Then** a Phase column is displayed between Mode and Result

**Given** the ExecutionDetailPage header
**When** displaying an in-flight execution (no result)
**Then** the execution's `phase` (Pending or Executing) is shown as the primary status indicator

**Given** the optimistic execution detection (Story 8.5)
**When** the user triggers an execution
**Then** the optimistic state continues to work — once a real DRExecution with `isActive === true` appears in the watch, the optimistic state is replaced

**Given** all existing console tests
**When** updated
**Then** tests no longer set `activeExecution` / `activeExecutionMode` on plan status mocks
**And** DRExecution test fixtures include `phase` and `isActive` fields
**And** jest-axe accessibility audit passes
**And** all tests pass with zero regressions

## Epic 13: VolumeReplication Lifecycle Management & Cascade Ownership

DRPlan assumes full lifecycle ownership of VolumeReplication (VR) and VolumeGroupReplication (VGR) objects. Each Soteria controller instance creates VR/VGR on its local cluster via `createOrUpdate` with `spec.replicationState` reflecting the site's current role: `primary` (writable) where VMs are running, `secondary` (read-only) where they are not. DRPlan deletes VR/VGR objects via dual finalizers (one per site), ensuring both clusters clean up before the object is garbage collected. DRPlan watches VR/VGR status to derive replication health (replacing poll-only). DRExecution mutates `spec.replicationState` during transitions: the failover handler calls `SetSource` on the target site (promoting to `primary`/writable before VMs start) and Step0 calls `StopReplication` on the source site for planned migration (demoting to `secondary`/read-only). DRPlan owns DRExecution via OwnerReference for cascade delete. The `resync` replication state is not used — only `primary`/`secondary`.

### Story 13.1: DRExecution OwnerReference for Cascade Delete

As a platform engineer,
I want DRExecution objects to have an OwnerReference pointing to their DRPlan,
So that deleting a DRPlan automatically cascade-deletes all its DRExecutions.

#### Acceptance Criteria

**AC1: OwnerReference set on creation**
**Given** a DRExecution is created via the aggregated API server
**When** `PrepareForCreate` in `pkg/registry/drexecution/strategy.go` processes the resource
**Then** it sets a controller OwnerReference on the DRExecution with the DRPlan as owner (using `metav1.OwnerReference` with `Controller: true`, `BlockOwnerDeletion: true`)
**And** the DRPlan is fetched by `spec.planName` to resolve the UID

**AC2: Cascade delete behavior**
**Given** a DRPlan with one or more DRExecutions
**When** the DRPlan is deleted
**Then** Kubernetes garbage collection cascade-deletes all owned DRExecutions
**And** no manual cleanup of DRExecution resources is required

**AC3: Backward compatibility**
**Given** existing DRExecution resources without an OwnerReference
**When** the updated reconciler processes them
**Then** they continue to function normally (OwnerReference is not retroactively added — only new executions get it)

**AC4: Tests**
**Given** the DRExecution strategy and reconciler
**When** unit and integration tests run
**Then** PrepareForCreate tests verify OwnerReference is set with correct plan UID
**And** integration tests verify cascade delete behavior
**And** all existing tests pass with zero regressions

#### Technical Notes

- Both DRPlan and DRExecution are cluster-scoped — `metav1.OwnerReference` works for cluster-scoped → cluster-scoped ownership
- `PrepareForCreate` already stamps `soteria.io/plan-name` label; OwnerReference is set in the same method
- The strategy needs a `PlanGetter` (rest.Getter or client.Reader) to fetch the DRPlan for UID resolution
- Scope: ~2 modified prod files, ~2 modified test files

### Story 13.2: DRPlan CreateOrUpdate VR/VGR with Site-Aware State

As a platform engineer,
I want the DRPlan controller to create or update VolumeReplication and VolumeGroupReplication objects on each site with the correct replication state,
So that VR/VGR lifecycle is managed by the DRPlan and the replication state always reflects the site's current role.

#### Acceptance Criteria

**AC1: CreateOrUpdate in DRPlan reconciler**
**Given** a DRPlan with discovered VMs and resolved volume groups
**When** the DRPlan controller reconciles (active or passive site)
**Then** it calls `createOrUpdate` (via the CSI Extension driver) for each volume group's VR/VGR on the local cluster
**And** the `createOrUpdate` uses `controllerutil.CreateOrUpdate` semantics (create if absent, update spec if changed)

**AC2: Site-aware replicationState derivation**
**Given** the DRPlan controller with `LocalSite` configured
**When** creating or updating VR/VGR on the local cluster
**Then** `spec.replicationState` is set to `primary` if `LocalSite == plan.Status.ActiveSite` (or `plan.Spec.PrimarySite` when ActiveSite is empty)
**And** `spec.replicationState` is set to `secondary` if `LocalSite != ActiveSite`

**AC3: Each site creates independently**
**Given** two Soteria controller instances (one per site)
**When** both reconcile the same DRPlan
**Then** each creates VR/VGR only on its own cluster (local Kubernetes API)
**And** the primary site creates with `replicationState: primary`
**And** the secondary site creates with `replicationState: secondary`

**AC4: No createOrUpdate during active execution**
**Given** a DRPlan with an active (non-terminal) DRExecution
**When** the DRPlan controller reconciles
**Then** it skips the createOrUpdate of VR/VGR (the DRExecution controller owns replication state changes during execution)

**AC5: CSI Extension driver CreateVolumeGroup uses createOrUpdate**
**Given** the CSI Extension driver
**When** `CreateVolumeGroup` is called
**Then** it uses `createOrUpdate` semantics (not create-only with AlreadyExists skip)
**And** on update, only `spec.replicationState` is mutated (DataSource and class are immutable)

**AC6: StopReplication always sets secondary**
**Given** the CSI Extension driver's `StopReplication` method
**When** called on a VR/VGR with any `spec.replicationState`
**Then** it sets `spec.replicationState` to `secondary` (not a flip)
**And** the `flipReplicationState` helper is replaced with unconditional `secondary` assignment

**AC7: Noop controller drops resync case**
**Given** the noop VolumeReplication controller's `stateForReplicationState` function
**When** mapping `spec.replicationState` to status
**Then** only `primary` and `secondary` cases are handled (the `Resync` case is removed from the switch)
**And** `statusUpToDate` is simplified accordingly

**AC8: Tests**
**Given** the DRPlan reconciler, CSI Extension driver, and noop controller
**When** tests run
**Then** reconciler tests verify VR/VGR creation with correct replicationState per site role
**And** driver tests verify createOrUpdate idempotency (create + update path)
**And** StopReplication tests verify it always sets `secondary` regardless of input state
**And** resync test cases in the noop controller are removed or replaced
**And** tests verify no createOrUpdate during active execution
**And** all existing tests pass with zero regressions

#### Technical Notes

- Soteria never sets `spec.replicationState = resync` today — the `Resync` handling in `flipReplicationState` and `stateForReplicationState` is dead-code defense. This story removes it for clarity
- `ConditionResyncing` in VR/VGR *status* is unrelated — it's a CSI Addons status condition reported by real storage drivers and is still read by `mapHealth` in `status.go` (unchanged)
- **IMPORTANT:** After this change, `StopReplication` always sets `secondary` (read-only). The failover handler currently calls `StopReplication` on the Owner (target) to promote it — Story 13.6 MUST restructure the failover handler to call `SetSource → StartVM` (not `StopReplication → StartVM`) and add `StopReplication` to Step0 for planned migration
- DRPlan reconciler needs RBAC for `replication.storage.openshift.io` VR/VGR resources (get/list/watch/create/update/patch)
- `VolumeGroupSpec.Labels` already carries `soteria.io/site-role` — reuse for state derivation
- Scope: ~5 modified prod files, ~5 modified test files

### Story 13.3: Dual Finalizers on VR/VGR Objects

As a platform engineer,
I want VR/VGR objects to carry two finalizers (one per site),
So that both Soteria controller instances clean up their local resources before the object is garbage collected, and DRPlan deletion triggers VR/VGR cleanup.

#### Acceptance Criteria

**AC1: Finalizer constants**
**Given** the CSI Extension constants
**When** updated
**Then** two finalizer constants are defined: `soteria.io/site-primary` and `soteria.io/site-secondary`

**AC2: Finalizer added on creation**
**Given** the DRPlan controller creating a VR/VGR on the local cluster
**When** the object is created or updated
**Then** the controller adds its site-specific finalizer (e.g., primary site adds `soteria.io/site-primary`)
**And** the finalizer is added only if not already present

**AC3: Finalizer removed on DRPlan deletion**
**Given** a DRPlan with a deletion timestamp (being deleted)
**When** the DRPlan controller reconciles
**Then** it deletes all VR/VGR objects owned by this plan (by label `soteria.io/drplan`)
**And** removes its site-specific finalizer from each VR/VGR before deletion
**And** removes the DRPlan's own finalizer only after all VR/VGR finalizers are cleared

**AC4: DRPlan finalizer**
**Given** a DRPlan
**When** first reconciled
**Then** the DRPlan controller adds a finalizer `soteria.io/volume-replication` to the DRPlan
**And** this finalizer prevents DRPlan deletion until VR/VGR cleanup is complete

**AC5: Cross-site cleanup**
**Given** a VR/VGR with both `soteria.io/site-primary` and `soteria.io/site-secondary` finalizers
**When** the primary site controller removes `soteria.io/site-primary`
**Then** the object remains (secondary finalizer still present)
**When** the secondary site controller removes `soteria.io/site-secondary`
**Then** the object is garbage collected

**AC6: Degraded mode — one site down**
**Given** a DRPlan being deleted while one site is unreachable
**When** the reachable site's controller removes its finalizer
**Then** the VR/VGR object remains in Terminating state until the other site recovers
**And** when the other site recovers, its controller removes its finalizer and the object is GC'd

**AC7: Tests**
**Given** the finalizer logic
**When** tests run
**Then** tests verify finalizer addition on create
**And** tests verify finalizer removal on DRPlan deletion
**And** tests verify GC only after both finalizers removed
**And** all existing tests pass with zero regressions

#### Technical Notes

- DRPlan reconciler needs `update` permission on VR/VGR for finalizer manipulation
- The DRPlan finalizer follows the same pattern as standard Kubernetes operator finalizers
- Finalizers are set on VR/VGR objects (not on the DRPlan itself — the DRPlan gets its own separate finalizer)
- Scope: ~3 modified prod files, ~3 modified test files

### Story 13.4: DRPlan Watches VR/VGR for Replication Health

As a platform engineer,
I want the DRPlan controller to watch VR/VGR status changes and update replication health reactively,
So that health updates are timely and event-driven rather than purely poll-based.

#### Acceptance Criteria

**AC1: Secondary watch on VR/VGR**
**Given** the DRPlan controller's `SetupWithManager`
**When** setting up watches
**Then** it watches VolumeReplication and VolumeGroupReplication resources
**And** uses a predicate that fires only on `status.state` or `status.conditions` changes

**AC2: Event-to-DRPlan mapping**
**Given** a VR/VGR status change event
**When** the watch handler processes it
**Then** it reads the `soteria.io/drplan` label to determine the owning DRPlan
**And** enqueues a reconcile request for that DRPlan

**AC3: Health derived from VR/VGR status.state**
**Given** the DRPlan controller reconciling after a VR/VGR status change
**When** updating `status.replicationHealth`
**Then** it reads `status.state` from VR/VGR objects (via the existing `GetReplicationStatus` driver method)
**And** maps to the existing `VolumeGroupHealth` model

**AC4: Poll-based health retained as fallback**
**Given** the existing poll-based health monitoring
**When** the watch-based path is added
**Then** the periodic requeue (10m normal / 30s degraded) is retained as a safety net
**And** the watch provides faster reaction to state changes between poll intervals

**AC5: Tests**
**Given** the watch setup and event handler
**When** tests run
**Then** watch predicate tests verify filtering on status changes only
**And** event handler tests verify correct DRPlan enqueue from label
**And** all existing health monitoring tests pass with zero regressions

#### Technical Notes

- The predicate should ignore `metadata` and `spec` changes (only `status` is relevant)
- Similar pattern to the existing VM watch predicate (`vmRelevantChangePredicate`)
- RBAC markers for VR/VGR watch already exist from Story 12.0
- Scope: ~2 modified prod files, ~2 modified test files

### Story 13.5: Remove CreateVolumeGroup from Engine, Reprotect & Health Paths

As a developer,
I want the failover engine, reprotect handler, and health polling to use `GetVolumeGroup` instead of `CreateVolumeGroup` for VG ID resolution,
So that VR/VGR creation responsibility is exclusively owned by the DRPlan reconciler (Story 13.2), and execution/monitoring paths only read.

**Background:** Currently `CreateVolumeGroup` is used as an idempotent "get-or-create" in three locations: `resolveVolumeGroupID` in `pkg/engine/failover.go`, `resolveVGID` in `pkg/controller/drexecution/reconciler.go`, and `resolveVolumeGroupID` in `pkg/controller/drplan/health.go`. With Story 13.2 moving VR/VGR creation to the DRPlan reconciler, these paths should use the read-only `GetVolumeGroup` method instead.

#### Acceptance Criteria

**AC1: Engine failover path uses GetVolumeGroup**
**Given** the `resolveVolumeGroupID` function in `pkg/engine/failover.go`
**When** resolving a VolumeGroupInfo to a driver-level VolumeGroupID
**Then** it calls `driver.GetVolumeGroup` instead of `driver.CreateVolumeGroup`
**And** if the VG is not found (`ErrVolumeGroupNotFound`), the error is propagated with a clear message indicating the DRPlan reconciler has not yet created the VR/VGR

**AC2: DRExecution reprotect path uses GetVolumeGroup**
**Given** the `resolveVGID` function in `pkg/controller/drexecution/reconciler.go`
**When** `buildVolumeGroupEntries` resolves VG IDs for reprotect
**Then** it calls `drv.GetVolumeGroup` instead of `drv.CreateVolumeGroup`
**And** the function signature changes to accept a `VolumeGroupID` (the ID format is deterministic from name/namespace) or uses `GetVolumeGroup` by name

**AC3: DRPlan health path uses GetVolumeGroup**
**Given** the `resolveVolumeGroupID` function in `pkg/controller/drplan/health.go`
**When** resolving VG IDs for health polling
**Then** it calls `drv.GetVolumeGroup` instead of `drv.CreateVolumeGroup`

**AC4: PVC resolution no longer needed in resolve paths**
**Given** that VR/VGR objects already exist (created by DRPlan)
**When** resolving a VG ID
**Then** the PVCResolver calls are removed from the resolve functions (PVC information was only needed for `CreateVolumeGroup`)
**And** the VG ID is derived deterministically from the VG name/namespace (matching the ID format established in Story 12.3)

**AC5: Tests updated**
**Given** all tests that stub `OnCreateVolumeGroup` for resolve purposes
**When** updated
**Then** they stub `OnGetVolumeGroup` instead
**And** new tests verify that `ErrVolumeGroupNotFound` is handled gracefully with a descriptive error
**And** all existing tests pass with zero regressions, 0 lint issues

#### Technical Notes

- The VolumeGroupID format is `csi-ext-<namespace>/<name>` (from Story 12.3) — this can be computed deterministically without calling `GetVolumeGroup` at all, but using `GetVolumeGroup` validates the VR/VGR exists
- The `PVCResolver` dependency in the resolve functions can be dropped since PVC names are only needed during creation
- This story depends on 13.3 (DRPlan must create VR/VGR before execution can run)
- Scope: ~3 modified prod files, ~5 modified test files

### Story 13.6: DRExecution Mutates VR/VGR ReplicationState During Transitions

As a platform engineer,
I want the DRExecution controller to change `spec.replicationState` on VR/VGR objects during DR transitions,
So that the replication direction follows the VM migration: `primary` on the site becoming active, `secondary` on the site becoming passive.

#### Acceptance Criteria

**AC1: Failover handler restructure — SetSource replaces StopReplication on Owner**
**Given** the FailoverHandler's per-group path currently calls `StopReplication → StartVM` on the Owner (target) site
**When** Story 13.2 changes StopReplication to always set `secondary`
**Then** the per-group path is changed to `SetSource → StartVM` (SetSource sets `primary`, making the target volume writable before VMs start)
**And** `StopReplication` is no longer called in the per-group handler on the Owner site

**AC2: Step0 adds StopReplication for planned migration**
**Given** a planned_migration failover with GracefulShutdown=true
**When** Step0 (PreExecute) runs on the source/active site
**Then** it calls `StopReplication` on the source site's local VR/VGR (setting `secondary` — demoting the source volume to read-only)
**And** this happens after StopVM (VMs are already stopped, volume is safe to demote)
**And** disaster failover (GracefulShutdown=false) does NOT call StopReplication in Step0 (source may be unreachable)

**AC3: Failback — symmetric reverse**
**Given** a failback execution transitioning from DRedSteadyState
**When** the Owner (target/original primary site) processes each DRGroup
**Then** `SetSource` sets VR/VGR `spec.replicationState = primary` on the target site (promoting it writable)
**And** Step0 on the source/current active site calls `StopReplication` → `secondary` (planned) or is skipped (disaster)

**AC4: Reprotect — role confirmation**
**Given** a reprotect execution transitioning from FailedOver
**When** the ReprotectHandler processes volume groups
**Then** `StopReplication` sets `spec.replicationState = secondary` (demoting the old active, tolerant of failure)
**And** `SetSource` sets `spec.replicationState = primary` (confirming the new active)
**And** the result matches the rest-state table: active site has `primary`, passive has `secondary`

**AC5: State table invariant**
**Given** any rest state (SteadyState, FailedOver, DRedSteadyState, FailedBack)
**When** the transition completes and the system reaches the next rest state
**Then** the site where VMs are running has VR/VGR with `spec.replicationState = primary`
**And** the other site has VR/VGR with `spec.replicationState = secondary`

**AC6: Tests**
**Given** the failover, failback, and reprotect handlers
**When** integration tests run
**Then** each transition is verified to produce the correct replicationState on both sites
**And** the state table invariant is verified at every rest state
**And** existing tests that assert "SetSource should not be called during failover" are updated to assert `SetSource` IS called
**And** all existing tests pass with zero regressions

#### Technical Notes

- This story restructures the failover handler from `StopReplication → StartVM` to `SetSource → StartVM` — a real handler change, not just tests
- The change is required because 13.2 makes StopReplication always set `secondary`, which would leave the target volume read-only (VMs can't write)
- `SetSource` (always sets `primary`) promotes the target volume to writable before VMs start
- Step0 adds StopReplication back for planned migration (reversed from the 5.7 simplification that removed it)
- For disaster failover, Step0 remains a no-op (source unreachable) — the source site's VR/VGR stays stale until recovery, then DRPlan reconciler fixes it
- Reprotect handler is structurally unchanged (StopReplication + SetSource), but semantics are now clearer with 13.2's simplification
- Story 13.5 must land first so resolve paths use `GetVolumeGroup` (read-only)
- A table-driven integration test covering the full 8-phase cycle validates the invariant
- Scope: ~3 modified prod files (`failover.go`, `failover_test.go`, `doc.go`), ~4 modified test files

## Epic 14: Multi-Site Integration Testing with Real Storage

Validates Soteria's full DR lifecycle against real Ceph RBD volume replication on two Kind clusters connected via Cilium Cluster Mesh. Uses the same ScyllaDB cross-DC deployment pattern from `hack/stretched-local-test.sh`, adapted for Kind + Cilium (replacing Submariner MCS) + Rook-Ceph (replacing ontap-san). Two Kind clusters (east and west) simulate the multi-site topology. Cilium Cluster Mesh provides cross-cluster pod networking (replacing Submariner). Rook-Ceph provides RBD storage with image-level mirroring and VolumeReplication CRDs via CSI Addons. ScyllaDB uses Rook-Ceph for its PVCs and Cilium for cross-DC gossip. KubeVirt runs VMs with emulation fallback. The lifecycle test mirrors the UAT-13 4-transition cycle (planned migration → reprotect → failback → reprotect) as the first test in a multi-site integration test suite. CI workflow deferred to a future epic.

Dependency graph:
```
14.1 (Kind+Cilium) → 14.2 (Rook-Ceph) → 14.4 (ScyllaDB on Rook+Cilium)  → 14.6 (Soteria) → 14.7 (Test)
       ↓                    ↓                                                   ↑
       → 14.3 (Dashboard)  → 14.5 (KubeVirt on Rook)  ─────────────────────────┘
```

### Story 14.1: Kind Cluster Provisioning with Cilium Cluster Mesh

As a platform engineer,
I want two Kind clusters (east and west) with Cilium Cluster Mesh providing cross-cluster networking,
So that I have the foundational multi-site topology for integration testing.

#### Acceptance Criteria

**AC1: Kind cluster creation**
**Given** the `hack/multisite/` directory with Kind cluster configs
**When** `setup-clusters.sh` is executed
**Then** two Kind clusters named `east` and `west` are created with `disableDefaultCNI: true`
**And** each cluster has at least 3 worker nodes (for Rook-Ceph OSD placement)
**And** worker nodes have `extraMounts` for Rook-Ceph raw block OSD paths

**AC2: Cilium deployment**
**Given** both Kind clusters are running without a CNI
**When** the setup script installs Cilium
**Then** Cilium is deployed as the CNI on both clusters
**And** Cilium agents are healthy on all nodes

**AC3: Cluster Mesh enablement**
**Given** Cilium is running on both clusters
**When** the setup script enables Cluster Mesh
**Then** Cluster Mesh is connected between east and west
**And** `cilium clustermesh status` shows healthy on both clusters

**AC4: Cross-cluster connectivity smoke test**
**Given** Cluster Mesh is connected
**When** a test pod on east curls/pings a test pod on west (and vice versa)
**Then** cross-cluster pod-to-pod connectivity succeeds

**AC5: Idempotent setup and teardown**
**Given** the setup and teardown scripts
**When** `setup-clusters.sh` is run multiple times
**Then** it is idempotent (safe to re-run)
**And** `teardown.sh` cleanly removes both Kind clusters

**AC6: Script structure**
**Given** the `hack/multisite/` directory
**When** scripts are created
**Then** cluster configs live in `hack/multisite/kind-east.yaml` and `hack/multisite/kind-west.yaml`
**And** all scripts are CI-friendly (no hardcoded paths, configurable via env vars)

#### Technical Notes

- Cilium's official docs have a Kind-specific guide for Cluster Mesh setup
- The `cilium` CLI handles Cluster Mesh enablement between clusters
- Each cluster needs `cluster.id` and `cluster.name` set to unique values for Cluster Mesh
- Kind config needs `kubeadmConfigPatches` to disable default CNI
- Minimum 1 control-plane + 3 worker nodes per cluster for Rook-Ceph OSD anti-affinity
- Scope: `hack/multisite/` directory (~5 files: 2 Kind configs, setup script, teardown script, README)

### Story 14.2: Rook-Ceph Deployment with RBD Volume Replication

As a platform engineer,
I want Rook-Ceph deployed on both Kind clusters with RBD mirroring and VolumeReplication CRDs,
So that real storage replication is available for Soteria's CSI Extension driver.

#### Acceptance Criteria

**AC1: Rook operator deployment**
**Given** both Kind clusters from Story 14.1
**When** `setup-rook-ceph.sh` is executed
**Then** the Rook operator is deployed on both clusters
**And** operator pods are running and healthy

**AC2: CephCluster creation**
**Given** the Rook operator is running
**When** CephCluster CRs are applied
**Then** Ceph clusters are created on both Kind clusters using loopback/raw-file OSDs (Kind-compatible)
**And** Ceph health is OK on both clusters (`ceph status` via toolbox)

**AC3: RBD mirroring configuration**
**Given** Ceph clusters are healthy
**When** CephBlockPool is configured with `mirroring.enabled: true, mode: image`
**Then** RBD mirroring is enabled on both clusters
**And** CephRBDMirror daemons are deployed on both clusters
**And** bootstrap peer tokens are exchanged between east and west
**And** `rbd mirror pool status` shows healthy peering

**AC4: CSI Addons deployment**
**Given** Rook-Ceph is running with RBD mirroring
**When** CSI Addons (Volume Replication Operator) is deployed
**Then** VolumeReplication and VolumeGroupReplication CRDs are available on both clusters
**And** CSI Addons controller pods are running

**AC5: VolumeReplicationClass creation**
**Given** CSI Addons is deployed
**When** VolumeReplicationClass is created with provisioner `rook-ceph.rbd.csi.ceph.com`
**Then** the VolumeReplicationClass is available on both clusters
**And** a StorageClass `rook-ceph-block` is created for RBD volumes

**AC6: Replication smoke test**
**Given** the full Rook-Ceph + mirroring stack is deployed
**When** a test PVC is created on east with a VolumeReplication CR attached
**Then** the VolumeReplication status shows replication is active (state `replaying` or `syncing`)
**And** the PVC binds successfully on the Rook-Ceph StorageClass

#### Technical Notes

- Rook-Ceph on Kind requires raw block files or loopback devices for OSDs — use `extraMounts` in Kind config (from 14.1) with raw files in `/var/lib/rook/`
- Each cluster needs minimum 3 OSDs for Ceph health (one per worker node)
- RBD mirroring bootstrap: generate token on east, import on west (and vice versa for bidirectional)
- CSI Addons is the component that reconciles VolumeReplication CRs into actual Ceph RBD mirror operations — this replaces the noop controller from Epics 12-13
- The VolumeReplicationClass name is what gets referenced in the DRPlan's `volumeReplicationDriver.volumeReplicationClass` field
- Scope: `hack/multisite/setup-rook-ceph.sh` + Rook manifests/values

### Story 14.3: Kubernetes Dashboard Deployment

As a platform engineer,
I want the Kubernetes Dashboard deployed on both Kind clusters,
So that I can visually troubleshoot pods, services, PVCs, and other resources during integration testing.

#### Acceptance Criteria

**AC1: Dashboard deployment**
**Given** both Kind clusters from Story 14.1
**When** the dashboard setup script or section is executed
**Then** the Kubernetes Dashboard is deployed on both east and west clusters

**AC2: Admin access configuration**
**Given** the Dashboard is deployed
**When** an admin ServiceAccount and ClusterRoleBinding are created
**Then** the Dashboard is accessible with full cluster-admin privileges for development use
**And** an access token is generated or skip-login is configured

**AC3: Access documentation**
**Given** the Dashboard is running
**When** the user follows the documented access pattern
**Then** the Dashboard UI is accessible via `kubectl proxy` or `kubectl port-forward`
**And** the setup script prints access instructions (URL and token)

**AC4: Smoke test**
**Given** the Dashboard is deployed and accessible
**When** navigating to the Dashboard UI
**Then** pods, services, and PVCs are visible across namespaces

#### Technical Notes

- The standard Kubernetes Dashboard (kubernetes/dashboard) deploys via Helm or static manifests
- For Kind local development, `kubectl port-forward` to the dashboard service is the simplest access method
- Admin RBAC is acceptable for local development — not for production
- This is a troubleshooting aid, not a testing dependency — other stories do not depend on this
- Scope: small addition to setup scripts or a standalone `setup-dashboard.sh`

### Story 14.4: ScyllaDB Cross-DC Deployment on Rook-Ceph

As a platform engineer,
I want ScyllaDB deployed as a cross-DC cluster on both Kind clusters using Rook-Ceph storage and Cilium Cluster Mesh for inter-node communication,
So that Soteria has its shared state store for the integration test environment.

#### Acceptance Criteria

**AC1: cert-manager deployment**
**Given** both Kind clusters
**When** the ScyllaDB setup script is executed
**Then** cert-manager is deployed on both clusters
**And** a self-signed CA issuer is created for Soteria TLS certificates

**AC2: scylla-operator deployment**
**Given** cert-manager is running
**When** the scylla-operator is deployed
**Then** the ScyllaCluster CRD is available on both clusters
**And** scylla-operator pods are running

**AC3: ScyllaCluster creation with Rook-Ceph storage**
**Given** the scylla-operator and Rook-Ceph are running
**When** ScyllaCluster CRs are applied
**Then** ScyllaDB is deployed on both clusters with datacenter names `east` and `west`
**And** ScyllaDB PVCs use the Rook-Ceph block StorageClass
**And** developer-mode resource requests are used (reduced from production sizing)

**AC4: Cross-DC discovery via Cilium Cluster Mesh**
**Given** Cilium Cluster Mesh is connected between east and west
**When** the ScyllaDB headless service on east is annotated with `io.cilium/global-service: "true"`
**Then** west's ScyllaDB can discover east's seed nodes via the normal service FQDN
**And** `externalSeeds` in west's ScyllaCluster uses the standard service FQDN (not `clusterset.local`)

**AC5: mTLS configuration**
**Given** cert-manager is running on both clusters
**When** TLS certificates are created following the `stretched-local-test.sh` pattern
**Then** ScyllaDB inter-node and client communication uses mTLS
**And** a combined CA trust bundle is created (same pattern as `create_combined_ca` in existing script)

**AC6: Kustomize overlays**
**Given** the existing overlay structure in `hack/overlays/`
**When** new overlays are created
**Then** `hack/multisite/overlays/{base,east,west}/` mirror the `hack/overlays/{base,etl6,etl7}/` structure
**And** Cilium global service annotation replaces Submariner ServiceExport
**And** Rook-Ceph StorageClass replaces `ontap-san-xfs`

**AC7: Multi-DC convergence smoke test**
**Given** ScyllaDB is deployed on both clusters
**When** the seed cluster (east) is deployed first and west joins
**Then** `nodetool status` shows all nodes in UN (Up Normal) state across both DCs
**And** a CQL write on east is readable on west (cross-DC replication verified)

#### Technical Notes

- Follows the deployment pattern from `hack/stretched-local-test.sh` — same ScyllaCluster CR structure, cert-manager TLS, combined-CA approach
- Key difference: Cilium global service replaces Submariner MCS ServiceExport for cross-cluster ScyllaDB gossip
- Key difference: Rook-Ceph block StorageClass replaces `ontap-san-xfs` for ScyllaDB PVCs
- ScyllaDB developer mode: single member per rack, reduced CPU/memory requests for Kind
- East is the seed cluster (no externalSeeds); west joins via externalSeeds referencing east's service
- Highest-risk story in the epic — scylla-operator's topology assumptions may require workarounds in Kind
- Scope: `hack/multisite/setup-scylladb.sh` + `hack/multisite/overlays/` directory

### Story 14.5: KubeVirt Deployment

As a platform engineer,
I want KubeVirt deployed on both Kind clusters with Rook-Ceph as the VM storage backend,
So that virtual machines can be created with PVC-backed disks on real replicated storage.

#### Acceptance Criteria

**AC1: KubeVirt operator deployment**
**Given** both Kind clusters with Rook-Ceph running
**When** the KubeVirt setup script is executed
**Then** the KubeVirt operator and CR are deployed on both clusters following the [KubeVirt Kind quickstart](https://kubevirt.io/quickstart_kind/)

**AC2: Emulation fallback**
**Given** KubeVirt is deployed
**When** `/dev/kvm` is not available on the host
**Then** KubeVirt emulation mode is enabled (`useEmulation: true`)
**And** VMs can still run (with reduced performance)

**AC3: KubeVirt health**
**Given** KubeVirt is deployed
**When** checking KubeVirt status
**Then** KubeVirt phase is `Deployed` on both clusters
**And** all KubeVirt pods are running (virt-operator, virt-api, virt-controller, virt-handler)

**AC4: Container disk smoke test**
**Given** KubeVirt is deployed
**When** a minimal test VMI with a container disk (e.g., `quay.io/kubevirt/cirros-container-disk-demo`) is created
**Then** the VMI starts and reaches `Running` state

**AC5: PVC-backed disk smoke test**
**Given** KubeVirt and Rook-Ceph are running
**When** a test VM with a Rook-Ceph PVC-backed disk is created
**Then** the PVC binds on the Rook-Ceph StorageClass
**And** the VM starts and reaches `Running` state

**AC6: virtctl installation**
**Given** KubeVirt is deployed
**When** the setup script completes
**Then** `virtctl` is available for VM console access and lifecycle operations

#### Technical Notes

- KubeVirt Kind quickstart: https://kubevirt.io/quickstart_kind/
- Emulation mode patch: `kubectl -n kubevirt patch kubevirt kubevirt --type=merge --patch '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'`
- KubeVirt needs `/dev/kvm` for hardware acceleration — Linux hosts with KVM support get native performance; other hosts fall back to emulation
- PVC-backed VMs use the Rook-Ceph StorageClass — this is the disk that gets volume-replicated
- Container disks (for boot images) don't use PVCs — they are pulled as container images
- Scope: `hack/multisite/setup-kubevirt.sh`

### Story 14.6: Soteria Operator Deployment

As a platform engineer,
I want Soteria deployed on both Kind clusters with the real Ceph VolumeReplicationClass,
So that the operator manages DR plans against real storage replication.

#### Acceptance Criteria

**AC1: Soteria operator deployment**
**Given** ScyllaDB and KubeVirt are running on both clusters
**When** the Soteria deployment script is executed
**Then** Soteria operator (API server + controller) is deployed on both clusters
**And** console plugin is excluded (Kind has no OCP Console)

**AC2: Site-name configuration**
**Given** the Soteria deployment
**When** the controller manager starts
**Then** east cluster runs with `--site-name=east` and `--scylladb-local-dc=east`
**And** west cluster runs with `--site-name=west` and `--scylladb-local-dc=west`

**AC3: VolumeReplicationClass reference**
**Given** the Rook-Ceph VolumeReplicationClass from Story 14.2
**When** DRPlans are created
**Then** they reference `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: <rook-ceph-vrc-name>}`

**AC4: APIService availability smoke test**
**Given** Soteria is deployed
**When** checking APIService status
**Then** `v1alpha1.soteria.io` is Available on both clusters

**AC5: Cross-DC replication smoke test**
**Given** Soteria is running with ScyllaDB cross-DC
**When** a test resource is created via the Soteria API on east
**Then** it is visible on west after ScyllaDB replication delay

#### Technical Notes

- Deployment follows the `hack/stretched-local-test.sh` pattern minus console plugin
- Uses the `hack/multisite/overlays/` created in Story 14.4 (ScyllaDB overlays) with Soteria additions
- The noop VR controller from Epic 12 is NOT deployed — real CSI Addons sidecar handles VR reconciliation
- Soteria image must be built and loaded into Kind clusters (either via registry or `kind load docker-image`)
- Scope: `hack/multisite/deploy-soteria.sh` + overlay additions

### Story 14.7: Test Scenario Setup + Full Lifecycle Integration Test

As a platform engineer,
I want the same test scenario from `hack/stretched-local-test.sh` deployed in the Kind environment and an automated full lifecycle test validating 4 DR transitions with real storage,
So that the platform is proven to work with real Ceph RBD volume replication.

#### Acceptance Criteria

**AC1: Test namespace and VM creation**
**Given** Soteria and KubeVirt are running on both clusters
**When** the test setup runs (either via script or Go test `BeforeSuite`)
**Then** namespace `soteria-dr-test` is created on both clusters
**And** 6 test VMs are created following the `stretched-local-test.sh` wave structure:
  - Wave 1: `fedora-db`
  - Wave 2: `fedora-appserver-1`, `fedora-appserver-2`
  - Wave 3: `fedora-webserver-1`, `fedora-webserver-2`, `fedora-webserver-3`
**And** east VMs have `runStrategy: Always`, west VMs have `runStrategy: Halted`
**And** VMs use container disk for boot + Rook-Ceph PVC for data disk (PVC is what gets volume-replicated)
**And** VMs are labeled with `soteria.io/drplan: fedora-app` and `soteria.io/wave: "<N>"`

**AC2: DRPlan creation**
**Given** VMs are deployed
**When** the DRPlan `fedora-app` is created on both clusters
**Then** `primarySite: east, secondarySite: west, maxConcurrentFailovers: 2`
**And** `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: <rook-ceph-vrc-name>}`

**AC3: Pre-test sanity verification**
**Given** the test scenario is deployed
**When** the test verifies initial state (mirrors UAT-13 sanity checks)
**Then** DRPlan phase = SteadyState, activeSite = east
**And** Conditions: Ready=True, SitesInSync=True, DisksConsistent=True, ReplicationHealthy=True
**And** VR CRs on east = primary, on west = secondary
**And** East VMs running, west VMs stopped

**AC4: Planned migration east → west (T1: SteadyState → FailedOver)**
**Given** the system is in SteadyState
**When** a planned_migration DRExecution is created
**Then** the execution completes with result = Succeeded
**And** DRPlan phase = FailedOver, activeSite = west
**And** VR CRs on west = primary, on east = secondary
**And** West VMs running, east VMs stopped

**AC5: Reprotect (T2: FailedOver → DRedSteadyState)**
**Given** the system is in FailedOver
**When** a reprotect DRExecution is created
**Then** the execution completes with result = Succeeded
**And** DRPlan phase = DRedSteadyState

**AC6: Planned migration west → east (T3: DRedSteadyState → FailedBack)**
**Given** the system is in DRedSteadyState
**When** a planned_migration DRExecution is created
**Then** the execution completes with result = Succeeded
**And** DRPlan phase = FailedBack, activeSite = east
**And** VR CRs on east = primary, on west = secondary
**And** East VMs running, west VMs stopped

**AC7: Reprotect (T4: FailedBack → SteadyState)**
**Given** the system is in FailedBack
**When** a reprotect DRExecution is created
**Then** the execution completes with result = Succeeded
**And** DRPlan phase = SteadyState, activeSite = east

**AC8: Per-transition assertions**
**Given** each transition completes
**When** the test validates post-transition state
**Then** DRExecution Duration field is populated
**And** all conditions remain healthy (Ready, SitesInSync, DisksConsistent, ReplicationHealthy)
**And** no checkpoint conflicts in controller logs
**And** no immutability violations in controller logs

**AC9: Real-storage assertions**
**Given** real Ceph RBD mirroring is in use (not noop)
**When** the test checks replication state
**Then** VolumeReplication status shows non-zero `lastSyncTime` delta between primary and secondary
**And** measurable RPO from actual Ceph mirroring lag (not noop-instant)

**AC10: Post-lifecycle verification**
**Given** the full 4-transition lifecycle is complete
**When** the test verifies final state
**Then** the system is back to its initial state (SteadyState, activeSite=east, all conditions healthy)

**AC11: Test infrastructure**
**Given** the test suite
**When** implemented
**Then** Go test code lives in `test/multisite/` using Ginkgo/Gomega
**And** `suite_test.go` bootstraps the Ginkgo suite
**And** `setup_test.go` handles `BeforeSuite` (scenario setup) and `AfterSuite` (cleanup)
**And** `lifecycle_test.go` contains the 4-transition lifecycle test
**And** kubeconfigs are configurable via env vars (`EAST_KUBECONFIG`, `WEST_KUBECONFIG`)
**And** timeouts are configurable for real-storage timing variability

#### Technical Notes

- Test scenario mirrors `hack/stretched-local-test.sh` lines 420-514 (VM creation, wave structure, DRPlan)
- VM images: container disk for boot (no CDI dependency), Rook-Ceph PVC for data disk (this is what gets volume-replicated)
- `BeforeSuite` deploys the scenario; `AfterSuite` cleans up — test is self-contained
- Per-transition polling needs realistic timeouts: UAT-13 took 40-70s against noop; real Ceph may be longer
- Log scanning: capture controller logs during lifecycle, assert no unexpected ERROR entries
- This is the first test of a multi-site integration test suite — structured for extensibility (future tests: disaster failover, partial failure, concurrent plans)
- CI-friendly structure (env var config, no hardcoded paths) but CI workflow deferred to future epic
- Scope: `test/multisite/` directory (~3 Go files), test scenario setup (Go or shell)

### Story 14.8: Full Environment Orchestration Script

As a platform engineer,
I want a single script that provisions the entire multi-site test infrastructure in sequence,
So that the complete environment can be set up with one command.

#### Acceptance Criteria

**AC1: Sequential execution**
**Given** the `hack/multisite/` directory with individual setup scripts
**When** `setup-all.sh` is executed
**Then** it calls the following scripts in order:
  1. `setup-clusters.sh` — Minikube KVM2 clusters + Cilium Cluster Mesh
  2. `setup-rook-ceph.sh` — Rook-Ceph + RBD mirroring
  3. `setup-kubevirt.sh` — KubeVirt + CDI
  4. `validate-fedora-vm.sh` — Fedora VM validation + node sizing
  5. `setup-scylladb.sh` — ScyllaDB cross-DC deployment

**AC2: Fail-fast behavior**
**Given** the orchestration script is running
**When** any individual setup script exits with a non-zero code
**Then** execution halts immediately with a clear error message indicating which step failed
**And** the exit code is propagated

**AC3: Skip support**
**Given** the orchestration script
**When** invoked with `--skip <script-name>` flags (e.g., `--skip clusters --skip rook-ceph`)
**Then** the specified steps are skipped
**And** remaining steps execute in order

**AC4: Teardown support**
**Given** the orchestration script
**When** invoked with `teardown` subcommand
**Then** teardown is performed in reverse order (ScyllaDB → KubeVirt → Rook-Ceph → clusters)
**And** each teardown step tolerates partial state

**AC5: Timing and summary**
**Given** the orchestration script completes
**When** all steps succeed
**Then** a summary is printed showing each step name and elapsed time
**And** total elapsed time is displayed

**AC6: Idempotent**
**Given** the orchestration script
**When** run multiple times
**Then** it is idempotent (each underlying script is already idempotent)

#### Technical Notes

- Does NOT include `deploy-soteria.sh` — Soteria deployment belongs in the e2e test `BeforeSuite` because the operator image may be recompiled last-minute
- All underlying scripts are already idempotent from Stories 14.1–14.5
- `set -euo pipefail` for fail-fast
- Scope: `hack/multisite/setup-all.sh` (~80-120 lines)

---

## Epic 15: Real-Storage Orchestration Correctness & Cross-Cluster PV Management

Addresses three categories of insights discovered during Epic 14 integration testing with real Ceph RBD mirroring:

1. **Resync guard for planned failover**: Asynchronous replication has a lag window. Before promoting the target to primary, the orchestrator must request a resync and wait for completion (state=Secondary, Completed=True) to guarantee zero data loss. This only applies to planned migrations where the source is healthy and reachable. The implementation uses an event-driven watch (no polling) — the DRExecution controller watches VR/VGR status changes and proceeds when the resync completes, with a `RequeueAfter` timeout as safety net.

2. **ShadowPV CRD for cross-cluster PV provisioning**: Ceph RBD mirroring replicates the image but not the Kubernetes PV metadata. The target cluster needs a pre-provisioned PV referencing the mirrored image (with pool-ID adjustment for the local Ceph cluster). ShadowPV is a cluster-scoped resource (stored in ScyllaDB like DRPlan) that shares PV manifests between clusters. A publisher controller discovers PVs backing replicated volumes; a consumer controller creates local PVs from remote entries.

3. **Console plugin standalone mode**: The OCP console plugin cannot run in Minikube (no OCP Console host). A standalone SPA wrapper with direct K8s API access enables the same UI for the multi-site dev/test environment.

4. **Soteria operator deployment** (moved from Epic 14 Story 14-6): Deploys Soteria on both Minikube clusters with Kustomize overlays adapted for Cilium + Rook-Ceph. May become a test fixture in BeforeSuite.

5. **Full lifecycle E2E test** (moved from Epic 14 Story 14-7): Validates all orchestration logic from Epics 13–15 against real Ceph RBD replication.

**Deferred to future epic:** Error condition bubbling from ShadowPV status and VR/VGR failures into DRPlan conditions, enabling DRPlan to gate operations based on infrastructure health. In Epic 15, ShadowPV records issues in its own status and emits events; DRPlan integration is separate.

Dependency graph:
```
15-1 (ResyncVolume method) → 15-2 (Planned Failover Resync Guard) → 15-3 (Reprotect Simplification)
15-4 (ShadowPV CRD) → 15-5 (Publisher Controller) → 15-6 (Consumer Controller)
15-7 (Console Standalone) — independent
15-8 (Soteria Deploy ex-14-6) — depends on 14.1-14.5 infra + all code stories
15-9 (E2E Test) — depends on 15-8
```

Depends on Epic 13 (VolumeReplication lifecycle) and Epic 14 (multi-site infrastructure).

### Story 15.1: ResyncVolume Driver Method & CSI Extension Implementation

As a developer,
I want a `ResyncVolume` method on the StorageProvider interface that sets a VR/VGR to resync state,
So that the orchestrator can request data synchronization from the current primary before a planned failover promotion.

#### Acceptance Criteria

**AC1: StorageProvider interface extension**
**Given** the current 6-method StorageProvider interface
**When** the `ResyncVolume` method is added
**Then** the interface has 7 methods
**And** the signature is `ResyncVolume(ctx context.Context, id VolumeGroupID) error`
**And** the method sets `spec.replicationState = resync` on the target VR/VGR

**AC2: CSI Extension implementation**
**Given** the CSI Extension driver in `pkg/drivers/csiextension/`
**When** `ResyncVolume` is called with a valid VolumeGroupID
**Then** the corresponding VR or VGR CR has its `spec.replicationState` patched to `resync`
**And** the operation is idempotent (no error if already in resync state)

**AC3: Noop driver passthrough**
**Given** the noop driver
**When** `ResyncVolume` is called
**Then** it returns nil immediately (no-op behavior, consistent with other noop methods)

**AC4: Fake driver support**
**Given** the fake driver for unit testing
**When** `ResyncVolume` is called
**Then** it supports the same `On*/Return` programmable API as other methods
**And** call recording captures the invocation

**AC5: Conformance test**
**Given** the conformance test suite in `pkg/drivers/conformance/`
**When** `RunConformance` is executed
**Then** a `ResyncVolume` lifecycle test is included (create VG → resync → verify state)
**And** idempotency test (resync when already resyncing → no error)
**And** not-found test (resync non-existent VG → ErrVolumeGroupNotFound)

**AC6: Context cancellation**
**Given** a `ResyncVolume` call in progress
**When** the context is cancelled
**Then** the operation respects `ctx.Err()` and returns the cancellation error

#### Technical Notes

- `resync` is an existing VolumeReplication spec state in the CSI Addons specification
- The CSI Extension implementation follows the same pattern as `StopReplication` (patch `spec.replicationState`) and `SetSource` (patch `spec.replicationState`)
- The Ceph behavior: when a secondary VR is set to `resync`, it pulls any un-replicated data from the peer primary and transitions to `status.state=Secondary, Completed=True` when fully synced
- Scope: ~1 new method per driver file, ~1 conformance test, ~1 fake method (~5 modified files, ~1 new test file section)

### Story 15.2: Planned Failover Resync Guard (Event-Driven)

As a developer,
I want the planned failover Step 0 to request a resync on the target VR/VGR and wait (event-driven) for completion before proceeding,
So that zero data loss is guaranteed during planned migrations with asynchronous replication.

#### Acceptance Criteria

**AC1: ResyncVolume call in Step 0**
**Given** a planned failover (GracefulShutdown=true)
**When** PreExecute runs Step 0
**Then** after stopping all origin VMs, `ResyncVolume` is called on each target VG
**And** `StopReplication` (demote source) is NOT called until resync completes

**AC2: Event-driven resync wait**
**Given** ResyncVolume has been called on target VGs
**When** the DRExecution reconciler returns
**Then** the DRExecution has a `ResyncPending=True` condition
**And** no `RequeueAfter` polling loop is used for checking resync state
**And** the DRExecution controller has a `.Watches()` for VolumeReplication/VolumeGroupReplication with a status predicate

**AC3: VR/VGR status watch on DRExecution controller**
**Given** the DRExecution controller's `SetupWithManager`
**When** the controller is initialized
**Then** it watches VR and VGR resources with a predicate filtering on `status.state` and `status.conditions[Completed]` changes
**And** the event handler maps VR/VGR → DRExecution via the `soteria.io/drplan` label (find active DRExecution for that plan)

**AC4: Reconciler gate on resync completion**
**Given** the DRExecution has `ResyncPending=True` and reconciliation is triggered by a VR status change
**When** all target VRs have `status.state=Secondary && conditions[Completed].status=True`
**Then** the `ResyncPending` condition is removed
**And** `StopReplication` is called on each source VG (demote to secondary)
**And** wave execution proceeds (SetSource + StartVM on target)

**AC5: Timeout safety net**
**Given** the DRExecution has `ResyncPending=True`
**When** `RequeueAfter(resyncTimeout)` fires (configurable, default 10m)
**And** the target VRs have NOT completed resync
**Then** the execution fails with a clear error message indicating resync timeout
**And** no partial promotion occurs (AC5 rest-state invariant preserved)

**AC6: Disaster failover unchanged**
**Given** a disaster failover (GracefulShutdown=false)
**When** PreExecute runs
**Then** no ResyncVolume call is made (source unreachable)
**And** per-group execution proceeds immediately (SetSource + StartVM)

**AC7: Checkpoint compatibility**
**Given** the DRExecution with ResyncPending state
**When** a checkpoint is written
**Then** the resync-wait state is captured
**And** on resume after crash, the reconciler re-evaluates VR status (does not re-call ResyncVolume if already in resync)

#### Technical Notes

- Pattern mirrors Story 5.6 (Event-Driven Wave Gate with VM Readiness Verification): set condition → watch fires → reconciler checks → proceed or wait
- The VR/VGR → DRExecution mapping uses the same `soteria.io/drplan` label lookup as `vrEventHandler` in Story 13.4, extended to find the active DRExecution for that plan
- `ResyncPending` condition goes on DRExecution (not DRPlan) — it's execution-scoped
- The resync timeout should be configurable via DRPlanSpec (or a default constant) since Ceph resync time depends on data volume
- Scope: ~3 modified prod files (`failover.go`, `drexecution/reconciler.go`, `drexecution/setup.go`), ~3 modified test files

### Story 15.3: Reprotect Handler Simplification for Real Storage

As a developer,
I want the reprotect handler to skip the `SetSource` call (which incorrectly promotes to primary) and instead just verify the secondary state and perform health monitoring,
So that reprotect works correctly with real Ceph where mirroring is automatic once roles are set.

#### Acceptance Criteria

**AC1: Remove SetSource from reprotect Phase 1**
**Given** the reprotect handler in `pkg/engine/reprotect.go`
**When** Phase 1 executes
**Then** `SetSource` is NOT called (the old primary is already secondary after failover Step 0)
**And** Phase 1 is replaced with a state verification: confirm each VG's VR is in `secondary` state

**AC2: State verification (idempotent)**
**Given** Phase 1 runs state verification
**When** the VR is already in `secondary` state (expected after failover)
**Then** verification passes and health monitoring begins
**And** no state mutations are performed

**AC3: Post-disaster reprotect handles stale primary**
**Given** a disaster failover occurred (source was unreachable)
**When** the old primary comes back online with its VR still in `primary` state
**And** reprotect is initiated
**Then** Phase 1 detects the stale `primary` state
**And** calls `ResyncVolume` on the old primary to transition it to resyncing from the new primary
**And** does NOT wait for resync completion (reprotect is fire-and-forget for sync)
**And** health monitoring begins immediately

**AC4: Health monitoring unchanged**
**Given** Phase 1 completes (verification or resync kick-off)
**When** Phase 2 executes
**Then** health monitoring behavior is unchanged (poll GetReplicationStatus, report Replicating condition)
**And** execution succeeds once health monitoring is kicked off (does not wait for full sync)

**AC5: Backward compatibility with noop driver**
**Given** the noop driver
**When** reprotect runs
**Then** state verification passes (noop always reports correct state)
**And** health monitoring completes immediately (noop reports HealthHealthy)
**And** existing unit tests continue to pass

#### Technical Notes

- The current `SetSource` call (line 173 of `reprotect.go`) promotes VR to primary — this creates dual-primary with real Ceph and must be removed
- After planned failover: east VR is already secondary (set in Step 0) → reprotect = pure health monitoring
- After disaster failover: east VR may still think it's primary (never received demotion) → reprotect = ResyncVolume(east) + health monitoring
- The distinction is detectable by checking VR `status.state`: if Secondary → just monitor; if Primary → needs resync
- Scope: ~1 modified prod file (`reprotect.go`), ~1 modified test file, ~1 modified doc file

### Story 15.4: ShadowPV CRD Definition & ScyllaDB Storage

As a developer,
I want a cluster-scoped ShadowPV CRD stored in ScyllaDB (same backend as DRPlan/DRExecution),
So that PV manifests can be shared between clusters for cross-site volume provisioning.

#### Acceptance Criteria

**AC1: ShadowPV API type**
**Given** the `pkg/apis/soteria.io/v1alpha1/` package
**When** the ShadowPV type is defined
**Then** it is cluster-scoped (`+kubebuilder:resource:scope=Cluster`)
**And** the spec contains:
```go
type ShadowPVSpec struct {
    PVs []ShadowPVEntry `json:"pvs"`
}

type ShadowPVEntry struct {
    ClusterName string                    `json:"clusterName"`
    PV          corev1.PersistentVolumeSpec `json:"pv"`
    PVName      string                    `json:"pvName"`
}
```

**AC2: DRPlan OwnerReference**
**Given** a ShadowPV is created
**When** it is associated with a DRPlan
**Then** it has an OwnerReference to the DRPlan (Controller=true, BlockOwnerDeletion=true)
**And** deleting the DRPlan cascades to delete the ShadowPV

**AC3: ScyllaDB storage registration**
**Given** the aggregated API server
**When** ShadowPV storage is registered
**Then** it uses the same ScyllaDB-backed REST storage as DRPlan and DRExecution
**And** CRUD operations, watch, and list work correctly
**And** cross-site replication via ScyllaDB CDC is automatic

**AC4: Label indexing**
**Given** ShadowPV resources
**When** queried by label
**Then** the `soteria.io/drplan` label is indexed for efficient lookup
**And** ShadowPVs for a given DRPlan can be listed via label selector

**AC5: Printer columns**
**Given** the ShadowPV custom TableConvertor
**When** `kubectl get shadowpvs` is run
**Then** columns show: NAME, PLAN, PV-COUNT, AGE

**AC6: Validation**
**Given** a ShadowPV create/update request
**When** validation runs
**Then** `spec.pvs[].clusterName` is required and non-empty
**And** `spec.pvs[].pvName` is required and non-empty
**And** no duplicate `(clusterName, pvName)` pairs within a single ShadowPV

#### Technical Notes

- ShadowPV is named after the DRPlan + VolumeGroup it represents (e.g., `<plan-name>-<vg-name>`)
- Uses `corev1.PersistentVolumeSpec` (not full PV) to avoid storing unnecessary metadata
- The PVName field stores the desired PV name for creation on remote clusters
- Cluster-scoped because PVs are cluster-scoped
- ScyllaDB storage follows the same pattern as `pkg/registry/drplan/` and `pkg/registry/drexecution/`
- **Garbage collection semantics:** When a PV is removed on one side, the ShadowPV publisher removes its entry from the corresponding ShadowPV. If that entry is the last one, the entire ShadowPV resource is deleted. This is entry-level GC, not resource-level — individual PV entries are added/removed independently
- **Error condition bubbling to DRPlan is out of scope for Epic 15.** ShadowPV records issues in its own status and emits events. A future epic will address how ShadowPV conditions (and VR/VGR failure conditions) bubble up to DRPlan conditions and gate operations
- Scope: ~2 new type files, ~1 new registry package, ~3 modified apiserver files, ~2 test files

### Story 15.5: ShadowPV Publisher Controller

As a developer,
I want a controller that watches VolumeReplication/VolumeGroupReplication CRs and publishes the backing PV information to ShadowPV resources,
So that other clusters can discover and create the corresponding PVs.

#### Acceptance Criteria

**AC1: Watch VR/VGR CRs**
**Given** the ShadowPV publisher controller
**When** a VolumeReplication or VolumeGroupReplication CR is created/updated
**And** it has a `soteria.io/drplan` label
**Then** the controller is triggered

**AC2: PV discovery**
**Given** a VR/VGR CR is observed
**When** the controller processes it
**Then** it resolves the dataSource PVC → PV
**And** reads the full PV spec (capacity, accessModes, CSI volumeHandle, volumeAttributes, etc.)

**AC3: ShadowPV entry creation**
**Given** a PV is discovered for a VR/VGR
**When** the controller updates the ShadowPV
**Then** it creates or updates the ShadowPV named `<plan-name>-<vg-name>`
**And** adds/updates an entry with `clusterName=<localSite>`, `pvName=<pv.Name>`, `pv=<pv.Spec>`
**And** sets the `soteria.io/drplan` label on the ShadowPV
**And** sets the OwnerReference to the DRPlan

**AC4: Idempotent updates**
**Given** a ShadowPV entry already exists for this cluster+PV
**When** the publisher runs again (reconcile retry)
**Then** no update is performed if the PV spec is unchanged
**And** resourceVersion conflicts are handled via retry

**AC5: VGR multi-PVC handling**
**Given** a VolumeGroupReplication CR with a PVC selector matching multiple PVCs
**When** the publisher processes it
**Then** all backing PVs are added as entries in the same ShadowPV
**And** each entry has its own `pvName` and `pv` spec

**AC6: PV deletion handling**
**Given** a VR/VGR CR is deleted (or the backing PV is deleted)
**When** the publisher detects the deletion
**Then** the corresponding ShadowPV entry for this cluster is removed
**And** if no entries remain in the ShadowPV, the entire ShadowPV resource is deleted

#### Technical Notes

- **Discovery model:** If a PVC is selected by a VolumeReplication or VolumeGroupReplication, we assume it is being replicated. The publisher creates a ShadowPV entry for it — no additional detection heuristic needed
- The publisher runs on every site (each site publishes its own PVs)
- The `soteria.io/drplan` label on VR/VGR (set by Story 13.2) is the trigger for publishing
- The controller needs RBAC for PV read access (cluster-scoped) and PVC read access (namespace-scoped)
- Site identity comes from `--site-name` flag (same as DRPlan controller)
- Scope: ~1 new controller file, ~1 new test file, ~1 modified setup.go

### Story 15.6: ShadowPV Consumer Controller (PV Creation with Pool-ID Rewrite)

As a developer,
I want a controller that watches ShadowPV resources and creates local PVs for entries from remote clusters,
So that mirrored Ceph RBD images have corresponding PVs on the target cluster ready for PVC binding.

#### Acceptance Criteria

**AC1: Watch ShadowPV resources**
**Given** the ShadowPV consumer controller
**When** a ShadowPV is created/updated
**Then** the controller is triggered

**AC2: Remote entry detection**
**Given** a ShadowPV with entries from multiple clusters
**When** the consumer processes it
**Then** it identifies entries where `clusterName != localSite`
**And** processes only those remote entries

**AC3: PV creation from remote entry**
**Given** a remote ShadowPV entry
**When** no local PV with the same name exists
**Then** a PV is created with the spec from the ShadowPV entry
**And** the PV's `spec.csi.volumeHandle` has its pool-ID segment rewritten for the local Ceph cluster

**AC4: Pool-ID rewrite for Ceph volume handles**
**Given** a CSI volume handle in format `<ver>-<clusterID-len-hex>-<clusterID>-<poolID-hex-16>-<uuid>`
**When** the consumer creates the local PV
**Then** the `<poolID-hex-16>` segment is replaced with the local Ceph pool's ID (in 16-char hex)
**And** the local pool ID is resolved from the CephBlockPool CR's status or via pool name lookup

**AC5: PV already exists (idempotent and conflict handling)**
**Given** a PV already exists locally with the same name
**When** the consumer runs
**And** the existing PV was created by the ShadowPV controller (matches expected spec)
**Then** no update or recreation is attempted and no error is raised
**When** the existing PV was NOT created by the ShadowPV controller (conflict)
**Then** the controller emits a warning event on the ShadowPV resource
**And** records the conflict in the ShadowPV status (e.g., `conditions[PVConflict]=True`)
**And** does not overwrite or delete the conflicting PV

**AC6: Non-Ceph volume handles**
**Given** a ShadowPV entry with a volume handle that does not match the Ceph format
**When** the consumer processes it
**Then** the PV is created with the volume handle as-is (no rewrite)
**And** a warning event is emitted indicating no pool-ID rewrite was performed

**AC7: Local pool ID resolution via CephBlockPool**
**Given** the consumer needs the local Ceph pool ID
**When** it resolves the pool
**Then** it reads the CephBlockPool CR's `.status.poolID` field (canonical source)
**And** the pool name is derived from the VolumeReplicationClass or StorageClass parameters
**And** the resolution is cached for the lifetime of the reconcile
**And** the controller has RBAC for `ceph.rook.io` CephBlockPool read access

#### Technical Notes

- Volume handle format (Rook-Ceph): `0001-0009-rook-ceph-<poolID-hex-16>-<image-uuid>`
- The pool-ID differs between clusters even for the same pool name (Ceph assigns sequential IDs)
- **Pool-ID source:** `CephBlockPool.status.poolID` is the canonical source — no toolbox query needed. The consumer controller needs RBAC to read `ceph.rook.io/v1` CephBlockPool resources
- The reference implementation is in `hack/multisite/setup-rook-ceph.sh` lines 648-656 (replication_smoke_test)
- The consumer should NOT create a PV if the local cluster already has a VR in primary state for that image (avoid creating PVs for volumes we own)
- **Conflict handling:** If a PV already exists but was not created by this controller, emit an event on the ShadowPV and record the issue in ShadowPV status. Do not overwrite. Error bubbling to DRPlan is deferred to a future epic
- Parser for volume handle format: `pkg/drivers/csiextension/volumehandle.go`
- Scope: ~1 new controller file, ~1 new volumehandle parser, ~2 test files

### Story 15.7: Console Plugin Standalone Mode

As a developer,
I want the OCP console plugin to also run as a standalone web application with direct K8s API access,
So that the DR management UI can be used in Minikube and non-OCP environments.

#### Acceptance Criteria

**AC1: Provider abstraction**
**Given** the existing console plugin hooks in `console-plugin/src/hooks/`
**When** the code is refactored
**Then** a provider interface abstracts the data access layer
**And** `providers/ocp.ts` wraps the existing `@openshift-console/dynamic-plugin-sdk` calls
**And** `providers/standalone.ts` implements the same interface using raw `fetch()` against the K8s API

**AC2: Build-time configuration**
**Given** the console plugin build system
**When** `make console-standalone` is run
**Then** a standalone SPA is built using a separate webpack config (`webpack.standalone.js`)
**And** the OCP SDK dependencies are not bundled (they don't exist in standalone mode)
**And** the standalone build produces a self-contained `dist/standalone/` directory

**AC3: Standalone entry point**
**Given** the standalone build
**When** the application starts
**Then** `standalone/index.html` loads `main.tsx` with React + React Router (BrowserRouter)
**And** all existing routes (Dashboard, PlanDetail, ExecutionDetail) are accessible
**And** PatternFly styles are loaded directly (not via OCP Console host)

**AC4: K8s API authentication**
**Given** the standalone application deployed in-cluster
**When** it connects to the K8s API
**Then** it uses a mounted ServiceAccount token for authentication
**And** API requests are proxied through a lightweight reverse proxy (or direct via CORS)
**And** watch/list/get operations function identically to the OCP plugin

**AC5: Minikube deployment**
**Given** the standalone console build
**When** deployed in the Minikube test cluster
**Then** a Deployment + Service + ServiceAccount are created in a `soteria-console` namespace
**And** the UI is accessible via MetalLB LoadBalancer IP or `minikube service`
**And** RBAC grants read access to soteria.io resources and KubeVirt VMs

**AC6: Runtime detection**
**Given** the console plugin code
**When** running in OCP Console host
**Then** the OCP provider is used automatically
**When** running standalone
**Then** the standalone provider is used
**And** detection is via build-time flag or `window.__OPENSHIFT_CONSOLE__` presence

**AC7: Existing tests pass**
**Given** the provider refactoring
**When** existing Jest tests are run
**Then** all 533+ tests continue to pass
**And** the mock infrastructure targets the provider interface (not SDK directly)

#### Technical Notes

- The existing hooks (`useDRPlans`, `useDRExecutions`, etc.) already abstract the SDK — this is a layer below
- For Minikube auth, the simplest approach is a Go reverse proxy (`cmd/console-proxy/main.go`) that adds the ServiceAccount token to API requests and serves the static SPA
- The standalone webpack config excludes `@openshift-console/dynamic-plugin-sdk` (external/empty module)
- PatternFly CSS must be imported directly in standalone mode (OCP Console injects it for plugins)
- Consider `react-router-dom` v5 (same as OCP Console uses) for route compatibility
- Scope: ~1 new providers directory, ~1 standalone directory, ~1 webpack config, ~1 proxy cmd, ~1 deployment manifest, ~5 modified hook files

### Story 15.8: Soteria Operator Deployment (Moved from 14-6)

As a platform engineer,
I want Soteria deployed on both Minikube KVM2 clusters with the real Ceph VolumeReplicationClass,
So that the operator manages DR plans against real storage replication.

#### Acceptance Criteria

**AC1: Soteria operator deployment**
**Given** ScyllaDB and KubeVirt are running on both Minikube clusters
**When** the Soteria deployment script is executed
**Then** Soteria operator (API server + controller) is deployed on both clusters
**And** OCP console plugin is excluded (Minikube has no OCP Console)
**And** standalone console UI is deployed on both clusters

**AC2: Site-name configuration**
**Given** the Soteria deployment
**When** the controller manager starts
**Then** east cluster runs with `--site-name=east` and `--scylladb-local-dc=east`
**And** west cluster runs with `--site-name=west` and `--scylladb-local-dc=west`

**AC3: VolumeReplicationClass reference**
**Given** the Rook-Ceph VolumeReplicationClass from Story 14.2
**When** DRPlans are created
**Then** they reference `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}`

**AC4: APIService availability smoke test**
**Given** Soteria is deployed
**When** checking APIService status
**Then** `v1alpha1.soteria.io` is Available on both clusters

**AC5: Cross-DC replication smoke test**
**Given** Soteria is running with ScyllaDB cross-DC
**When** a test resource is created via the Soteria API on east
**Then** it is visible on west after ScyllaDB replication delay

**AC6: Standalone console availability**
**Given** the standalone console deployment
**When** the deploy script completes
**Then** the `soteria-console-standalone` pod is Ready on both clusters
**And** `curl http://<service-ip>:8080/healthz` returns 200
**And** the UI serves the SPA at the root path

#### Technical Notes

- Image built locally and loaded into Minikube via `minikube image load` (not pulled from registry)
- Kustomize overlays in `hack/multisite/overlays/{base,east,west}/` extend the ScyllaDB overlays from Story 14.5 with Soteria-specific patches (manager args, TLS volumes, RBAC, per-DC site-name)
- Adapted from `hack/overlays/{base,etl6,etl7}/` pattern: `--scylladb-dc-replication=east:1,west:1` (1 member/rack in developer mode), `imagePullPolicy: IfNotPresent`, no console plugin, no Submariner ServiceExport
- `deploy-soteria.sh` or `BeforeSuite` fixture — deployment approach TBD (may be integrated into E2E test setup)
- No noop VR controller deployed — real CSI Addons sidecar from Story 14.2 handles VR/VGR reconciliation
- Standalone console image built via `docker build -f console-plugin/Dockerfile.standalone .` (repo root context)
- Standalone image loaded into both Minikube clusters via `minikube image load` (same workflow as operator image)
- `hack/overlays/base/console-standalone.yaml` applied with `CONSOLE_STANDALONE_IMG_PLACEHOLDER` replaced by the local tag
- Deploy order: operator deployed first → wait for APIService Available (AC4) → then deploy standalone console (the UI queries `soteria.io` APIs via the APIService)
- Standalone console is stateless and per-cluster (each instance proxies to its local K8s API); deployed on both sites so console access survives a site failure
- Scope: `hack/multisite/deploy-soteria.sh` + overlay additions + ~1 standalone image build step + ~1 standalone manifest apply + README update

### Story 15.9: Full Lifecycle E2E Test (Moved from 14-7)

As a platform engineer,
I want the same test scenario from `hack/stretched-local-test.sh` deployed in the Minikube environment and an automated full lifecycle test validating 4 DR transitions with real storage,
So that the platform is proven to work with real Ceph RBD volume replication including the Epic 15 orchestration improvements.

#### Acceptance Criteria

**AC1–AC11**: Same as original Story 14.7 acceptance criteria (see Epic 14 Story 14.7).

**AC12: Resync guard validation (Epic 15 specific)**
**Given** a planned migration transition
**When** the test observes the DRExecution during Step 0
**Then** a `ResyncPending=True` condition appears on the DRExecution
**And** after VR status shows `Secondary+Completed=True`, the condition is cleared
**And** the transition proceeds to wave execution

**AC13: ShadowPV validation (Epic 15 specific)**
**Given** DRPlan with VolumeReplication active
**When** the test checks ShadowPV resources
**Then** ShadowPV entries exist for each replicated PV
**And** PVs exist on both clusters (created by ShadowPV consumer controller)
**And** pool-IDs in volume handles differ between clusters

**AC14: BeforeSuite includes Soteria deployment**
**Given** the e2e test setup
**When** `BeforeSuite` runs
**Then** Soteria is built from the current source (`make docker-build`)
**And** the image is loaded into both Minikube clusters
**And** `deploy-soteria.sh` is called to deploy the freshly-built operator
**And** this ensures the test always runs against the latest code

#### Technical Notes

- Moved from Epic 14 Story 14-7 because Epic 15 changes (resync guard, ShadowPV) must be validated
- The e2e test compiles and deploys Soteria as part of BeforeSuite — this is why `setup-all.sh` excludes Soteria
- Additional assertions for Epic 15 features (AC12, AC13) supplement the original AC1-AC11
- Depends on all other Epic 15 stories being complete
- Scope: `test/multisite/` directory (~3-4 Go files)

---

## Epic 16: Helm Chart & Release Pipeline

**Goal:** Package Soteria as a Helm chart that lets anyone deploy the operator on two Kubernetes clusters with a single `helm install` per cluster. Provide a release pipeline that builds container images, pushes them to Quay, and publishes the chart to GitHub Pages.

**Scope:** Chart skeleton, controller templates, ScyllaDB managed/external templates, console plugin templates, standalone UI templates, multi-cluster install script, and release pipeline.

**Dependencies:** Existing kustomize manifests, console-plugin code, standalone UI code.

### Stories

| ID | Title | Depends On | Status |
|----|-------|-----------|--------|
| 16.1 | Chart Skeleton & Values | — | draft |
| 16.2 | Controller Templates | 16.1 | draft |
| 16.3 | ScyllaDB Managed Templates | 16.1 | draft |
| 16.4 | ScyllaDB External (BYO) Wiring | 16.2 | draft |
| 16.5 | Console Plugin Templates | 16.1 | draft |
| 16.6 | Standalone UI Templates | 16.1 | draft |
| 16.7 | Multi-Cluster Install Script | 16.2, 16.3 | draft |
| 16.8 | Release Pipeline & GitHub Pages | 16.1 | draft |

### Story Details

**Story 16.1 — Chart Skeleton & Values**
Create `charts/soteria/Chart.yaml`, `values.yaml`, and `_helpers.tpl`. The values file implements the full schema: site (name, role), controller (image, replicas, resources, leaderElection), tls (issuerRef), scylladb (mode: managed|external, keyspace, localDC, managed/external configs), networking (mode: submariner|cilium), ui (mode: console-plugin|standalone|none, per-mode settings).

**Story 16.2 — Controller Templates**
Create Helm templates for the Soteria controller: Deployment, ServiceAccount, ClusterRole/ClusterRoleBinding, APIService with cert-manager CA injection, ValidatingWebhookConfiguration, Services (apiserver, webhook, metrics), cert-manager Certificates. Conditional ScyllaDB connection wiring based on `scylladb.mode`.

**Story 16.3 — ScyllaDB Managed Templates**
Templates for managed ScyllaDB (rendered when `scylladb.mode=managed`): ScyllaCluster CR, mTLS ConfigMap, cert-manager Certificate, conditional ServiceExport (Submariner) or global-service annotation (Cilium).

**Story 16.4 — ScyllaDB External (BYO) Wiring**
When `scylladb.mode=external`: no ScyllaDB resources rendered, controller uses external.contactPoints, conditional TLS secret mounting.

**Story 16.5 — Console Plugin Templates**
Templates for OCP console plugin (rendered when `ui.mode=console-plugin`): Deployment, Service, ConsolePlugin CR, cert-manager Certificate.

**Story 16.6 — Standalone UI Templates**
Templates for standalone UI (rendered when `ui.mode=standalone`): Deployment, Service, ServiceAccount, ClusterRole/Binding, optional Gateway API HTTPRoute.

**Story 16.7 — Multi-Cluster Install Script**
`scripts/install-soteria.sh` orchestrating two-cluster deployment: seed install → CA propagation → joining install. Supports `--networking`, `--ui-mode`, `--uninstall`. Validates prerequisites.

**Story 16.8 — Release Pipeline & GitHub Pages**
GitHub Actions workflow: on tag push, build+push 3 images to quay.io, publish chart via chart-releaser-action, create orphan gh-pages branch.

---

## Epic 17: Documentation Site

**Goal:** Create a comprehensive documentation site (mkdocs-material, GitHub Pages) covering installation, architecture, usage, API reference, and contributor guides. Documentation follows a code-driven methodology: start from the PRD/architecture/UX spec as the conceptual base, then verify against actual code to document current behavior.

**Scope:** 22 documentation pages spanning installation guides (prerequisites, ScyllaDB networking variants, Helm), architecture explanations (topology, DR lifecycle, storage drivers), usage guides (DRPlan creation, waves, volume grouping, failover execution, UI screens), reference (CRD API, Helm values), and contributing guides (dev setup, writing a driver).

**Dependencies:** Epic 16 (Helm chart) for stories 17.7 and 17.20. UI instance access for screenshot-dependent stories 17.15–17.17.

**Documentation Methodology:** Start from the PRD, architecture doc, or UX spec as the conceptual base. Then read the related implemented user stories and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Stories

| ID | Title | Depends On | Status |
|----|-------|-----------|--------|
| 17.1 | Docs Site Setup | — | draft |
| 17.2 | Landing Page & Index | 17.1 | draft |
| 17.3 | Prerequisites Guide | 17.1 | draft |
| 17.4 | ScyllaDB Architecture Overview | 17.1 | draft |
| 17.5 | ScyllaDB with Submariner | 17.1 | draft |
| 17.6 | ScyllaDB with Cilium Cluster Mesh | 17.1 | draft |
| 17.7 | Helm Chart Installation Guide | 17.1, Epic 16 | draft |
| 17.8 | Architecture Overview | 17.1 | draft |
| 17.9 | DR Lifecycle & State Machine | 17.1 | draft |
| 17.10 | Storage Drivers Architecture | 17.1 | draft |
| 17.11 | Creating a DRPlan | 17.1 | draft |
| 17.12 | Waves & Throttling | 17.1 | draft |
| 17.13 | Volume Grouping | 17.1 | draft |
| 17.14 | Executing Failover | 17.1 | draft |
| 17.15 | UI Guide — Dashboard | 17.1 (blocked: screenshots) | draft |
| 17.16 | UI Guide — Plan Detail | 17.1 (blocked: screenshots) | draft |
| 17.17 | UI Guide — Execution Monitor | 17.1 (blocked: screenshots) | draft |
| 17.18 | API Reference — DRPlan | 17.1 | draft |
| 17.19 | API Reference — DRExecution | 17.1 | draft |
| 17.20 | Helm Values Reference | 17.1, Epic 16 | draft |
| 17.21 | Contributing — Dev Setup | 17.1 | draft |
| 17.22 | Contributing — Writing a Storage Driver | 17.1 | draft |

### Story Details

**Story 17.1 — Docs Site Setup**
Initialize mkdocs-material site: `mkdocs.yml`, `docs/` directory, nav structure, GitHub Actions deploy workflow.

**Story 17.2 — Landing Page & Index**
What is Soteria, key capabilities, architecture summary, link map to all sections.

**Story 17.3 — Prerequisites Guide**
CSI Addons storage, VolumeReplicationClass/VolumeGroupReplicationClass, cert-manager, scylla-operator.

**Story 17.4 — ScyllaDB Architecture Overview**
Cross-DC topology, why ScyllaDB, NetworkTopologyStrategy, LOCAL_ONE consistency.

**Story 17.5 — ScyllaDB with Submariner**
Step-by-step deployment with ServiceExport, shared CA, mTLS. Source: `hack/stretched-local-test.sh`.

**Story 17.6 — ScyllaDB with Cilium Cluster Mesh**
Step-by-step with global-service annotation. Source: `hack/multisite/setup-scylladb.sh`.

**Story 17.7 — Helm Chart Installation Guide**
Helm repo setup, managed + BYO ScyllaDB install, install script, upgrade/uninstall. Blocked until Epic 16 complete.

**Story 17.8 — Architecture Overview**
Two-cluster topology diagram, component diagram, data flow.

**Story 17.9 — DR Lifecycle & State Machine**
4 rest states, 4 transitions, planned vs disaster, Mermaid state diagram. Verify against `pkg/engine/failover.go`.

**Story 17.10 — Storage Drivers Architecture**
StorageProvider interface, replication model, driver lifecycle, conformance suite.

**Story 17.11 — Creating a DRPlan**
DRPlan CRD walkthrough, label selector, wave label, maxConcurrentFailovers, how to assign a VM.

**Story 17.12 — Waves & Throttling**
Wave formation, sequential execution, DRGroup chunking, startup ordering.

**Story 17.13 — Volume Grouping**
Namespace-level vs VM-level, consistency annotation, constraints.

**Story 17.14 — Executing Failover**
Planned migration, disaster recovery, monitoring, partial success, retry.

**Story 17.15 — UI Guide: Dashboard** (blocked: screenshots)
Dashboard overview, status indicators, alert banners, cross-cluster table.

**Story 17.16 — UI Guide: Plan Detail** (blocked: screenshots)
Tabs, wave tree, VM list, action buttons, execution history.

**Story 17.17 — UI Guide: Execution Monitor** (blocked: screenshots)
Gantt chart, progress bars, DRGroup status, inline errors, retry action.

**Story 17.18 — API Reference: DRPlan**
Full CRD field reference from types.go + kubebuilder markers.

**Story 17.19 — API Reference: DRExecution**
Full CRD field reference, status conditions, DRGroupStatus, phase semantics.

**Story 17.20 — Helm Values Reference**
Annotated parameter reference for every `values.yaml` field. Blocked until Epic 16 complete.

**Story 17.21 — Contributing: Dev Setup**
Clone, prerequisites, Makefile targets, testing pyramid, CI, local dev.

**Story 17.22 — Contributing: Writing a Storage Driver**
Interface contract, step-by-step driver implementation, conformance suite walkthrough.
