---
stepsCompleted: ['step-01', 'step-02', 'step-03', 'step-04']
workflowCompleted: true
completedAt: '2026-04-06'
project_name: 'dr-orchestrator'
user_name: 'Raffa'
totalEpics: 8
totalStories: 41
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
The OCP Console provides a sortable/filterable DR Dashboard table (500+ plans), persistent alert banners for protection status, cross-cluster awareness columns, plan detail pages with wave composition trees, execution history, and configuration views.
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

---

## Epic 6: OCP Console — Dashboard & Plan Management

The OCP Console provides a sortable/filterable DR Dashboard table (500+ plans), persistent alert banners for protection status, cross-cluster awareness columns, plan detail pages with wave composition trees, execution history, and configuration views.

### Story 6.1: Console Plugin Project Initialization

As a developer,
I want the `console-plugin/` directory scaffolded from the openshift/console-plugin-template with TypeScript, React, PatternFly 5, webpack module federation, Jest, and axe-core configured,
So that all subsequent Console development has a working build, dev server, and test harness.

**Acceptance Criteria:**

**Given** the repository root
**When** the `console-plugin/` directory is initialized from `openshift/console-plugin-template`
**Then** `package.json` exists with dependencies for React, PatternFly 5, and the Console SDK (`@openshift-console/dynamic-plugin-sdk`)
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
I want a sortable, filterable dashboard table showing all DRPlans with status, cross-cluster awareness, replication health, and RPO,
So that I can assess DR posture for 500+ plans at a glance.

**Acceptance Criteria:**

**Given** the DR Dashboard page
**When** it loads with DRPlan data via `useK8sWatchResource`
**Then** a PatternFly Table (composable, compact variant) displays with columns: Name (link), Phase (status badge), Active On (cluster name), DC1 status, DC2 status, Protected (ReplicationHealthIndicator), VMs (count), Last Execution (date + result badge), RPO, Actions (kebab menu) (UX-DR1, FR35)

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

**Given** the cross-cluster status columns (UX-DR3, FR40)
**When** the table renders
**Then** each cluster column shows: filled circle (●) + "Active (N VMs)" for the active site, open circle (○) + "Passive" for the passive site, question mark (?) + "Unknown" when a site is unreachable
**And** both clusters display identical data because both read from the same ScyllaDB state

**Given** the Protected column (UX-DR8 compact variant)
**When** rendering replication health
**Then** each cell shows: icon + health label + "RPO Ns" + "checked Ns ago" in a single line
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
**Then** the table may require horizontal scroll for all columns
**When** viewed at 1024px (minimum supported)
**Then** lower-priority columns (RPO, Last Execution) may be hidden by default

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

### Story 6.5: Plan Detail Page with Tabs

As a platform engineer,
I want a plan detail page with Overview, Waves, History, and Configuration tabs,
So that I can drill into any plan's full state and take context-aware actions.

**Acceptance Criteria:**

**Given** a DRPlan selected from the dashboard table (row click)
**When** the Plan Detail page loads
**Then** a full-page detail view renders with four horizontal tabs: Overview, Waves, History, Configuration (UX-DR4)

**Given** the Overview tab
**When** it renders
**Then** a DescriptionList shows plan metadata: name, label selector, wave label, maxConcurrentFailovers, creation date
**And** current phase is displayed with the appropriate status badge (UX-DR10)
**And** a ReplicationHealthIndicator (expanded variant) shows per-volume-group health, RPO, and freshness (UX-DR8)
**And** context-aware action buttons appear: only valid state transitions are shown — SteadyState shows Failover (danger) and Planned Migration (primary); FailedOver shows Reprotect (primary); DRedSteadyState shows Failback (primary) (FR36, UX-DR19)

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
**Then** a PatternFly CodeBlock shows the DRPlan CRD spec in YAML (read-only)
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
- Phase: SteadyState/DRedSteadyState = green (solid), FailedOver = blue (solid), FailingOver/Reprotecting/FailingBack = blue (outlined) + spinner icon
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

**Given** any custom component (ReplicationHealthIndicator, WaveCompositionTree, CrossClusterStatusColumns)
**When** tested with axe-core in Jest
**Then** zero accessibility violations are reported (UX-DR16)
**And** keyboard navigation tests confirm arrow key and Tab behavior per component

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

**Given** a DRPlan detail page with a valid action button (Failover, Planned Migration, Reprotect, or Failback)
**When** the operator clicks the action button
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
**And** the modal closes and the view transitions to the Execution Monitor
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
