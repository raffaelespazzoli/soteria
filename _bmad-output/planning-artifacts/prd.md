---
stepsCompleted: [1, 2, 2b, 2c, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]
workflowCompleted: true
completedAt: '2026-04-04'
inputDocuments:
  - _bmad-output/planning-artifacts/product-brief-soteria.md
  - _bmad-output/planning-artifacts/product-brief-soteria-distillate.md
  - _bmad-output/brainstorming/brainstorming-session-20260404-121402.md
workflowType: 'prd'
classification:
  projectType: "Kubernetes Operator / Infrastructure Orchestrator (developer_tool + api_backend)"
  domain: "Infrastructure — Disaster Recovery"
  complexity: high
  projectContext: greenfield
---

# Product Requirements Document - Soteria

**Author:** Raffa
**Date:** 2026-04-04

## Executive Summary

Soteria is an open-source, Kubernetes-native disaster recovery orchestrator for OpenShift Virtualization. It provides storage-agnostic DR across heterogeneous storage backends — ODF, Dell, Pure Storage, NetApp — through a single, consistent workflow engine. Platform engineers define DR plans using standard Kubernetes labels and CRDs. The orchestrator handles volume promotion, VM startup sequencing, wave-based throttling, and full audit trails. VMs already exist on both clusters with correct PVC bindings; the orchestrator's role is to promote replicated volumes to read-write and start VMs in the right order at the right time. Both datacenters see the same DR state through a Kubernetes Aggregated API Server backed by ScyllaDB with async cross-site replication.

The project exists because thousands of organizations migrating from VMware vSphere to OpenShift Virtualization are discovering there is no equivalent to VMware Site Recovery Manager. DR is the gating blocker for many of these migrations. Each storage vendor offers its own replication tooling, but no unified orchestration layer exists. Soteria fills that gap — starting lean with a pluggable driver framework and no-op driver for development, growing toward SRM-class maturity, and doing it in the open under Apache 2.0.

The target users are platform engineers and infrastructure architects responsible for RTO/RPO commitments, compliance audits, and DR testing across OpenShift Virtualization deployments. Secondary users are storage vendors who want first-class participation in the DR story through a bounded driver interface.

### What Makes This Special

No existing project combines storage-agnostic DR, VM-aware orchestration, Kubernetes-native operation, and open-source availability. Soteria delivers all four. The pluggable driver model means investment survives storage vendor churn — the orchestrator never changes when a new storage backend is added. Label-driven wave auto-formation means adding a VM to DR protection is two labels, not a plan rewrite.

The core design philosophy separates careful planning from simple execution. DRPlans are designed deliberately — waves, throttling, hooks, consistency levels — and validated through test executions. When the moment arrives, whether a planned migration or a 3 AM disaster, the operator presses one button. The orchestrator executes the plan exactly as designed, with wave-by-wave progress, partial success reporting, and an immutable audit trail. The product delivers confidence: not "I hope this works" but "I know this works because I've tested it and the orchestrator does exactly what it showed me it would do."

The vendor-specific surface is narrow — four replication operations (SetSource, SetTarget, StopReplication, GetReplicationStatus) plus volume group CRUD, modeled on CSI-Addons semantics. The real value is in the orchestration layer above: waves, throttling, fail-forward error handling, cross-site state, and the audit trail that proves compliance.

## Project Classification

- **Project Type:** Kubernetes Operator / Infrastructure Orchestrator (Go, CRDs, Aggregated API Server, kubectl-native)
- **Domain:** Infrastructure — Disaster Recovery
- **Complexity:** High — cross-site state coordination, multi-vendor storage abstraction, safety-critical failure scenarios, complex state machine (4 rest states, 8 phases with 4 transitions, 3 execution modes: planned_migration, disaster, reprotect)
- **Project Context:** Greenfield

## Success Criteria

### User Success

- **DR confidence through visibility:** Platform engineers can see replication health for every protected volume group in real time through the OCP Console plugin. Degraded or broken replication is surfaced immediately — no guessing.
- **DR confidence through testing (post-v1):** Administrators run non-disruptive DR tests against production plans on a regular cadence (target: bi-annually or more frequently). Successful test executions prove the plan works before a real disaster. Until Test mode is available, confidence is built through planned migration dry-runs during maintenance windows.
- **One-action failover:** When a failover is needed — planned or disaster — the operator triggers it with a single action. The orchestrator executes the full wave sequence without manual intervention. The operator monitors progress, not individual steps.
- **Honest status reporting:** Partial failures are reported as `PartiallySucceeded` with clear identification of what failed and what succeeded. The operator is never left guessing.

### Technical Success

- **Failover success rate:** Target 99% successful failover executions, measured across plan types (planned migration, disaster recovery). Partial failures attributable to orchestrator bugs (not external storage/infrastructure issues) should be exceptional.
- **Full DR lifecycle:** The orchestrator executes the complete 4-state cycle — failover, re-protect, failback, restore — against real OpenShift Virtualization clusters with real storage replication.
- **Storage-agnostic validation:** StorageProvider interface validated via no-op driver and conformance test suite. Production driver implementations (ODF, Dell, Pure, or NetApp) validated end-to-end as they are contributed.
- **Cross-site state consistency:** Both clusters serve identical DR state via the Aggregated API Server. State survives single-datacenter failure and auto-reconciles on recovery.

### Measurable Outcomes

- Every DRPlan execution produces an immutable DRExecution audit record with per-wave, per-group, per-step status and timestamps
- No-op driver enables full development, testing, and CI without storage infrastructure from Day 1
- *(Post-v1)* DR test mode validates plans non-disruptively against real storage (volume clones, isolated execution, automatic teardown)
- OCP Console plugin provides dashboard, plan management, replication health monitoring, and live execution progress

## User Journeys

### Journey 1: Maya — Platform Engineer, First DR Plan Setup

**Persona:** Maya is a senior platform engineer at a logistics company that migrated 200 VMs from vSphere to OpenShift Virtualization six months ago. She inherited DR responsibility when the VMware SRM licenses expired. Her manager keeps asking "are we protected?" and she keeps answering "not yet." The compliance team has flagged an open audit finding.

**Opening Scene:** Maya has three storage backends in her environment — ODF for most workloads, Dell PowerStore for the ERP database tier. She's been manually documenting a DR runbook in Confluence, but she knows it's untested and probably wrong. She installs Soteria on both clusters.

**Rising Action:** Maya starts with the ERP system. She labels the 12 ERP VMs with `app.kubernetes.io/part-of: erp-system` and assigns wave labels: `soteria.io/wave: "1"` for the database VMs, `"2"` for the application servers, `"3"` for the web frontends. She creates a DRPlan CRD:

```yaml
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: erp-full-stack
spec:
  waveLabel: soteria.io/wave
  maxConcurrentFailovers: 4
# VMs are associated to this plan via the label:
#   soteria.io/drplan: erp-full-stack
```

She applies it and opens the OCP Console. The DR Dashboard shows `erp-full-stack` with replication status for every volume group. Two volumes show `Degraded` — a replication link she didn't know was broken. She fixes it before it matters.

**Climax:** Maya schedules a planned migration to validate the DR plan end-to-end during a maintenance window. She watches the live execution monitor in the Console — wave 1 (databases) promotes volumes on the DR site, starts VMs. Wave 2 follows. Wave 3 completes. All 12 VMs are running on the DR site. She verifies the ERP login page loads. She fails back. The DRExecution record shows every step, every timestamp, every volume group promoted. *(Post-v1: non-disruptive Test mode will allow validation without production impact.)*

**Resolution:** Maya walks into the next compliance review with a DRExecution audit trail showing successful planned migration results. The audit finding is closed. Her manager stops asking. She moves on to protecting the CRM system — it takes 20 minutes because it's just labels and a new DRPlan.

**Capabilities revealed:** DRPlan CRD, label-driven wave formation, OCP Console dashboard, replication health monitoring, planned migration as DR validation, DRExecution audit trail, multi-storage-backend support.

---

### Journey 2: Carlos — DR Operator, 3 AM Disaster Failover

**Persona:** Carlos is a senior infrastructure operator on the overnight rotation. He's been trained on the DR plans Maya set up, and he's run two DR tests successfully in the past six months. He knows the plans work.

**Opening Scene:** At 3:14 AM, Carlos's pager fires. DC1 monitoring is unreachable. Network confirms the site is dark — power failure, no ETA for recovery. Carlos connects to DC2's OpenShift Console.

**Rising Action:** Carlos opens the DR Dashboard. All plans with DC1 as primary show replication status as `Unknown` — expected, since DC1 is unreachable. He selects `erp-full-stack` and clicks **Failover**. The pre-flight check shows: 12 VMs, 3 waves, last replication sync 47 seconds before the outage (RPO: ~47s), DR site has sufficient compute capacity. He types "FAILOVER" in the confirmation dialog.

**Climax:** The live execution monitor shows wave 1 starting — 3 database VMs promoting volumes with `force:true`. Two complete in 90 seconds. The third fails — the Dell PowerStore array reports a volume promotion error. The group is marked `Failed`. The engine continues to wave 1's remaining DRGroups, then moves to waves 2 and 3. The execution completes as `PartiallySucceeded`. Carlos can see exactly which VM failed and why. He retries the failed DRGroup — this time the storage array responds and the database VM starts.

**Resolution:** At 3:31 AM, all 12 ERP VMs are running on DC2. Carlos reports to the bridge call: "ERP is up on DR site, RPO was 47 seconds, one volume needed a retry on promotion." The DRExecution record captures everything. When DC1 recovers two days later, Carlos triggers re-protect from the Console to establish replication back to DC1, monitors until replication health shows `Healthy`, then schedules the failback for the next maintenance window.

**Capabilities revealed:** Cross-site shared state (plans available on DC2 during DC1 outage), disaster failover with force:true, pre-flight checks with RPO estimate, live execution monitor, partial failure handling, manual retry of failed DRGroups, re-protect workflow, failback scheduling.

---

### Journey 3: Maya — Planned Migration, Maintenance Window

**Persona:** Same Maya from Journey 1. Six months later, DC1 is due for a hardware refresh. All workloads need to move to DC2 gracefully during a weekend maintenance window.

**Opening Scene:** Maya has been running DR tests monthly. All plans show `Protected` with healthy replication. She schedules the migration for Saturday 2 AM with a 4-hour window.

**Rising Action:** Maya selects the first plan — `erp-full-stack` — and chooses **Planned Migration**. The pre-flight check confirms both DCs are up, replication is healthy, and estimated migration time (based on last test execution) is 18 minutes. She confirms. The orchestrator executes Step 0: stops the ERP VMs on DC1, demotes the volumes, waits for the final replication sync to complete — zero data loss guaranteed. Then it promotes on DC2 and starts VMs wave by wave.

**Climax:** Maya repeats for each plan — CRM, web tier, analytics. She monitors four plan executions in parallel on the Dashboard, each progressing through their waves independently. One plan's wave 2 takes longer than expected because `maxConcurrentFailovers` is throttling to 3 and there are 15 VMs in the wave. She watches the DRGroups process sequentially.

**Resolution:** By 3:40 AM — well within the 4-hour window — all workloads are running on DC2. Zero data loss. Maya triggers re-protect on all plans to establish replication from DC2 back to DC1 (which will become the new DR site after its hardware refresh). Monday morning, no one notices the migration happened.

**Capabilities revealed:** Planned migration mode (graceful stop + final sync), zero-data-loss guarantee, concurrent plan execution monitoring, throttling via maxConcurrentFailovers, re-protect to establish reverse replication.

---

### Journey 4: Priya — Storage Vendor Engineer, Driver Contribution

**Persona:** Priya is a software engineer at Dell working on CSI drivers for PowerStore. Her team has been asked to make PowerStore a first-class citizen in Soteria's DR story. She's an experienced Go developer familiar with CSI but new to the Soteria project.

**Opening Scene:** Priya clones the Soteria repo and runs `make dev-cluster`. A local OpenShift dev environment spins up with the orchestrator running against the no-op driver. She creates a test DRPlan, triggers a failover, and watches it succeed — all without any real storage. She reads the no-op driver source to understand the StorageProvider interface contract.

**Rising Action:** Priya creates a new `dell-powerstore` driver package. The interface is 6 methods — `CreateVolumeGroup`, `DeleteVolumeGroup`, `GetVolumeGroup`, `SetSource`, `StopReplication`, `GetReplicationStatus`. The replication model uses two engine-driven transitions (NonReplicated → Source, Source → NonReplicated) and the driver acts as a reconciler — checking actual storage state before flipping roles. Drivers handle unreachable peers internally — no force flags from the orchestrator. She maps each to Dell CSM's `repctl` commands and the `DellCSIReplicationGroup` CRD. The translation layer is straightforward because Soteria's interface mirrors CSI-Addons semantics, which Dell is already moving toward.

**Climax:** Priya runs the conformance test suite against a real two-cluster environment with PowerStore arrays. The suite exercises the full DR lifecycle — create volume groups, enable replication, planned failover, re-protect, disaster failover with force, failback. Three tests fail because Dell's volume promotion is asynchronous where Soteria expects synchronous completion. She adds a polling loop with timeout. All tests pass.

**Resolution:** Priya submits a PR with the driver, conformance test results, and a failure-mode document describing known edge cases (async promotion, replication group size limits). The driver is reviewed by Soteria maintainers and merged. PowerStore appears as a supported storage backend in the next release. Her team anticipates the driver will thin out significantly once Dell's native CSI-Addons support ships.

**Capabilities revealed:** No-op driver as development reference, `make dev-cluster` contributor onboarding, StorageProvider interface (6 methods), conformance test suite, driver contribution workflow, transitional shim architecture.

---

### Journey Requirements Summary

| Capability | Journey 1 (Setup) | Journey 2 (Disaster) | Journey 3 (Migration) | Journey 4 (Contributor) |
|---|---|---|---|---|
| DRPlan CRD with label-driven waves | Primary | Used | Used | Used |
| OCP Console Dashboard | Primary | Primary | Primary | — |
| Replication health monitoring | Primary | Context | Context | — |
| DR test mode *(post-v1)* | Primary | — | — | — |
| DRExecution audit trail | Primary | Primary | Context | Context |
| Disaster failover (force:true) | — | Primary | — | Tested |
| Pre-flight checks with RPO estimate | — | Primary | Primary | — |
| Live execution monitor | Context | Primary | Primary | — |
| Partial failure + manual retry | — | Primary | — | — |
| Re-protect workflow | — | Primary | Primary | — |
| Planned migration (graceful) | — | — | Primary | — |
| Concurrent plan monitoring | — | — | Primary | — |
| Throttling (maxConcurrentFailovers) | Context | Context | Primary | — |
| No-op driver | — | — | — | Primary |
| StorageProvider interface | Context | — | — | Primary |
| Conformance test suite | — | — | — | Primary |
| Cross-site shared state (ScyllaDB) | Context | Primary | Context | — |

## Domain-Specific Requirements

### Operational Safety

- **Failover retry preconditions:** A failover can only be retried if the VM is still in a healthy, known state on the original site. If the starting state is non-standard (e.g., VM partially failed over, volumes in inconsistent state), the orchestrator must reject the retry and require manual intervention. The system must never attempt a failover from an unpredictable starting point.
- **Human-triggered only:** All failover operations require explicit human initiation. No automatic failure detection or auto-failover. This eliminates split-brain risk by design.
- **Fail-forward execution:** Rollback is often impossible during DR (the active site may be down). Failed DRGroups are marked `Failed`, the engine continues with remaining groups, and the execution reports `PartiallySucceeded`. Manual retry of individual failed groups is supported when preconditions are met.

### RPO & Data Loss

- **RPO is storage-determined:** Recovery Point Objective depends entirely on the volume replication configuration and the underlying storage capability. RPO > 0 is acceptable for all use cases (planned migration, disaster recovery, and test). The orchestrator reports estimated RPO based on last known replication sync time but does not enforce RPO targets — that responsibility belongs to the storage replication layer.

### Access Control

- **Kubernetes-native RBAC:** All authorization uses standard Kubernetes RBAC applied to Soteria's CRDs. No custom authorization layer. Role granularity follows CRD verb semantics — e.g., `get`/`list` on DRPlan for read-only users, `create`/`update` for plan authors, `create` on DRExecution for failover operators.

### Secrets Management

- **Storage credentials:** The orchestrator discovers storage drivers from PVC storage classes and retrieves associated credentials from Kubernetes Secrets (local cluster) or HashiCorp Vault. The orchestrator never stores credentials directly — it references external secret sources.

### Audit & Compliance

- **DRExecution as audit record:** Every plan execution produces an immutable DRExecution record with per-wave, per-DRGroup, per-step status, timestamps, and error details. No specific compliance framework is targeted — the audit trail is designed to satisfy general compliance needs (SOX, ISO 22301, SOC 2, or whatever framework the customer operates under).
- **DR test frequency:** The orchestrator supports running DR tests at any frequency. Test scheduling cadence is company-specific — not product-enforced.

### Architecture Deferrals

- **ScyllaDB operational model:** Deployment, sizing, upgrades, backup, and availability of ScyllaDB are architecture-level concerns addressed in the architecture document, not the PRD.

## Innovation & Novel Patterns

### Detected Innovation Areas

**ScyllaDB + Kubernetes API Aggregation Layer for cross-site DR state.** Rather than building a bespoke CRD-sync protocol between two Kubernetes clusters, Soteria stores DR resources (DRPlan, DRExecution, DRGroupStatus) in ScyllaDB via a Kubernetes Aggregated API Server. ScyllaDB's `NetworkTopologyStrategy` with async replication handles cross-site data distribution. `LOCAL_ONE` consistency means all operations are served locally. When a datacenter fails, the surviving instance operates normally against its local replica. When the failed site recovers, ScyllaDB's anti-entropy repair auto-synchronizes — no custom reconciliation logic needed. The database's built-in consistency mechanisms replace what would otherwise be a complex distributed systems problem.

**CSI-Addons-inspired internal interface, decoupled from the wire protocol.** Soteria defines its own Go interface modeled on CSI-Addons semantics (volume groups, role-based replication — SetSource/SetTarget/StopReplication) but not bound to CSI-Addons' gRPC transport. This is a pragmatic bet on an emerging standard: ODF already implements CSI-Addons natively, Dell and Pure are moving toward adoption, and NetApp is not interested. The interface design means that as vendors adopt CSI-Addons, their driver implementations collapse to thin pass-throughs. NetApp's permanent shim is isolated behind the same interface. The orchestrator core never changes when storage vendors are added or when vendors' CSI-Addons maturity evolves.

**Label-driven wave auto-formation.** DR plans and wave membership are determined entirely by Kubernetes labels on VMs. Adding a VM to DR protection requires adding two labels — `soteria.io/drplan: <planName>` for plan membership and a wave label for execution ordering. No plan editing, no protection group management UI, no manual wave construction. VMs self-organize. This is a departure from SRM's explicit protection group model and leverages Kubernetes' native label semantics as the DR organization primitive.

### Validation Approach

- **ScyllaDB + Aggregated API:** Prototype with two OpenShift clusters, validate that `kubectl get drplan` returns identical data on both sides, test failure scenarios (single DC loss, network partition, recovery reconciliation), measure replication lag under load.
- **Storage interface:** Implement no-op driver first. Validate that the interface contract is sufficient for the full DR lifecycle via the conformance test suite. Use the first production driver implementation as the real test of the shim model.
- **Label-driven waves:** Validate with real-world plan structures (mixed wave sizes, throttling, namespace-level consistency). Test edge cases: VMs added/removed between plan creation and execution, label changes during execution.

### Risk Mitigation

- **ScyllaDB operational complexity:** ScyllaDB is niche in the Kubernetes ecosystem. Risk: operational burden deters adoption. Mitigation: provide reference architecture, Helm charts, and sizing guidance. Architecture document must justify the choice explicitly (why not etcd-only, tradeoffs, migration path).
- **CSI-Addons adoption pace:** Risk: Dell and Pure CSI-Addons adoption stalls, leaving Soteria dependent on shim drivers indefinitely. Mitigation: shim architecture is designed to be permanent if needed (NetApp proves this works). The interface is not coupled to vendor timelines.
- **Label discipline:** Risk: operators misconfigure labels, leading to VMs in wrong waves or missing from plans. Mitigation: admission webhooks validate label consistency, pre-flight checks report plan composition before execution, dashboard shows unprotected VMs.

## Kubernetes Operator Specific Requirements

### Project-Type Overview

Soteria is a Kubernetes operator / infrastructure orchestrator deployed via OLM (Operator Lifecycle Manager) on OpenShift. The primary API surface is Kubernetes CRDs served through a dedicated Aggregated API Server backed by ScyllaDB. The OCP Console plugin communicates exclusively through the Kubernetes API — no separate REST API. All user interaction flows through `kubectl`, CRDs, or the Console plugin.

### Architectural Constraints

**Homogeneous storage replication:** Volume replication only works between homogeneous storage backends (Dell-to-Dell, ODF-to-ODF, NetApp-to-NetApp) because replication is a storage vendor feature. A Dell array in DC1 replicates to a Dell array in DC2. Cross-vendor replication is not supported and not a goal.

**Driver selection is implicit:** The orchestrator determines which StorageProvider driver to use by inspecting the storage class of the VMs' PVCs. If a VM's volumes use a Dell CSI storage class, the orchestrator uses the Dell driver. No explicit storage provider configuration resource is needed — the mapping is derived from existing cluster state.

**VM pre-existence:** VMs already exist on both clusters with correct PVC bindings. The target cluster's VMs are in a stopped state with `runStrategy: Manual` or equivalent. The orchestrator's role is to transition replicated volumes to the Source role (read-write) and start VMs — not to create VMs or rebind PVCs.

### Technical Architecture Considerations

**Deployment model:** OLM-based installation. Soteria is packaged as an OLM operator with a ClusterServiceVersion (CSV), installed from the OperatorHub catalog. ScyllaDB is deployed as a prerequisite via the scylla-operator (OpenShift-certified, available from OperatorHub since March 2026).

**Cross-site shared state (confirmed — ScyllaDB):** The middleware analysis evaluated six alternatives — etcd (Raft quorum problem with 2 nodes, cannot survive single-node loss), PostgreSQL (single-writer), CockroachDB/FoundationDB/rqlite (all Raft/Paxos-based, same quorum problem), CouchDB (no mature K8s operator), NATS JetStream (no multi-writer cross-DC), and etcd-per-DC with custom reconciler (DIY sync logic). Any consensus-based database (Raft, Paxos) fundamentally cannot operate in a 2-DC topology without a third site. Only databases designed for eventual consistency with multi-master replication survive this constraint. ScyllaDB was confirmed: purpose-built for multi-DC eventual consistency, built-in anti-entropy repair, `LOCAL_ONE` consistency for local operation during partitions, and an OpenShift-certified Kubernetes operator (March 2026). The custom `storage.Interface` implementation for `k8s.io/apiserver` against ScyllaDB is the primary engineering risk and should be prototyped early.

**Shared resources in ScyllaDB (three):** DRPlan, DRExecution, DRGroupStatus. StorageProviderConfig was removed — driver selection is implicit from PVC storage classes.

**API surface:** All DR resources are served as Kubernetes API resources via the Aggregated API Server. The OCP Console plugin reads and writes exclusively through the Kubernetes API. No separate REST endpoints.

**API versioning:** Start with `v1alpha1` for all custom API groups. Graduate to `v1beta1` and `v1` as the API stabilizes through real-world usage. Follow standard Kubernetes API versioning conventions with conversion webhooks for version transitions.

### Observability

**Prometheus metrics (v1):**
- VMs under DR plan (gauge per plan, total)
- Failover execution duration (histogram per plan type)
- RPO / replication lag per volume group (gauge, sourced from `GetReplicationStatus`)
- Execution success/failure counts (counter per plan type and outcome)

Metrics are exposed via standard `/metrics` endpoint and scraped by Prometheus. No OpenTelemetry tracing in v1.

### Documentation (v1)

- **API reference:** Auto-generated from CRD schemas (OpenAPI / `kubectl explain` compatible)
- **Installation guide:** OLM-based installation, ScyllaDB prerequisite setup, cross-cluster connectivity
- **Storage driver development guide:** StorageProvider interface contract, no-op driver as reference, conformance test suite, contribution workflow
- **Architecture overview:** Layer-cake architecture, ScyllaDB rationale, cross-site state model, workflow engine design
- **Presentation deck:** Project overview deck for conferences, partner conversations, and internal advocacy

## Project Scoping & Phased Development

### MVP Strategy & Philosophy

**MVP approach:** Platform MVP — prove the core architecture works end-to-end (ScyllaDB shared state + storage abstraction + workflow engine + Console) against real OpenShift Virtualization clusters. The custom `storage.Interface` for ScyllaDB is the highest-risk component and must be prototyped first to validate the architecture before investing in workflow logic.

### MVP Feature Set (Phase 1 — v1)

**Core User Journeys Supported:**
- Journey 1 (Setup) — partial: DRPlan creation, Console dashboard, replication health (deferred: non-disruptive DR test)
- Journey 2 (Disaster Failover) — full
- Journey 3 (Planned Migration) — full
- Journey 4 (Driver Contribution) — full

**Must-Have Capabilities:**

| Component | Description |
|---|---|
| StorageProvider Go interface | 6-method interface with role-based replication model (NonReplicated/Source; Target observable but not engine-set) |
| No-op driver | Full interface implementation for dev/test/CI |
| DRPlan CRD | Label-driven wave formation, waveLabel, maxConcurrentFailovers. VMs associated via `soteria.io/drplan` label. No `type` field — execution mode chosen at runtime |
| DRExecution CRD | Immutable audit record. Execution mode (planned_migration, disaster, reprotect) is a field on DRExecution, not DRPlan |
| Planned migration workflow | Graceful stop → StopReplication on source → SetSource on target site → start VMs wave by wave. RPO=0 |
| Disaster recovery workflow | SetSource(force=true) on target site → start VMs wave by wave. RPO>0. Origin errors ignored |
| Re-protect workflow | StopReplication on old active → SetSource on new active → monitor until healthy. Used after failover (FailedOver→DRedSteadyState) and after failback as "restore" (FailedBack→SteadyState) |
| Failback | Same handler as failover (FailoverHandler with same planned_migration/disaster config). Lands at FailedBack state (unprotected) — restore required |
| ScyllaDB + Aggregated API Server | Custom `storage.Interface` for k8s.io/apiserver. Shared resources: DRPlan, DRExecution, DRGroupStatus |
| OCP Console plugin (core) | DR Dashboard, plan detail views, failover trigger with pre-flight checks, live execution monitor |
| Prometheus metrics | VMs under plan, failover duration, RPO/replication lag, execution success/failure |
| Kubernetes-native RBAC | Standard RBAC on CRDs |
| CLI / kubectl | Comes free with CRD design |

### Post-MVP Features (Phase 2)

| Component | Description |
|---|---|
| Test execution mode | Non-disruptive DR testing: volume cloning, isolated network, cleanup. Enables bi-annual testing without production impact |
| OCP Console — health monitoring | Dedicated replication health view with RPO time-series, alert thresholds |
| OCP Console — plan creation wizard | Step-by-step: select VMs → review waves → configure throttling → review & create |
| Additional storage drivers | Dell PowerStore, Pure Storage, NetApp (Trident Protect) |
| Hook framework | Pre/post hooks at plan, wave, VM level (K8s Jobs, QEMU guest agent, webhooks) |
| Post-startup health gate | Optional readiness check after VM startup — wait for application-level health before proceeding to next wave |

### Vision (Phase 3)

- Automated DR test scheduling with compliance reporting
- Multi-application orchestration with cross-plan dependencies
- SRM feature parity — full protection group management
- Community-contributed storage driver ecosystem
- Extension to non-VM workloads
- Broader KubeVirt community targeting beyond OpenShift

### Risk Mitigation Strategy

**Technical risks:**
- **ScyllaDB `storage.Interface` (highest risk):** Prototype first. Build the extension API server with ScyllaDB backend before any workflow logic. Validate watch semantics, resourceVersion tracking, pagination, and conflict resolution. If this fails, fall back to etcd-per-DC + reconciler (architecture preserves this escape hatch).
- **OCP Console plugin scope:** Reduced to core views for v1 (dashboard, plan detail, failover trigger, live execution). Health monitoring and plan wizard deferred to post-v1.

**Resource risks:**
- If resources are constrained, the minimum viable v1 is: StorageProvider interface + no-op driver + DRPlan CRD + disaster failover workflow + ScyllaDB shared state + CLI only (no Console). This proves the architecture without the UI.

### Execution Mode Model (Design Decision)

**DRPlan has no `type` field.** A DRPlan describes what to protect and how — wave grouping and throttling. VMs declare plan membership via the `soteria.io/drplan` label. Execution mode is chosen at runtime and recorded on DRExecution.

**DRPlan phases track the DR lifecycle.** The plan progresses through 8 phases — 4 rest states (SteadyState, FailedOver, DRedSteadyState, FailedBack) and 4 transition states (FailingOver, Reprotecting, FailingBack, ReprotectingBack). Rest states indicate where the system is idle; transition states indicate an operation is in progress. The phase advances to a transition state when an execution starts, and to the next rest state when it completes. The full cycle is: SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState.

**Three execution modes (v1 ships all three):**

Failover and failback use the same handler — the execution mode (planned_migration or disaster) determines behavior, not the direction. Direction is encoded in the state machine phases for human visibility and audit compliance.

| Mode | Origin Site | Target Site | RPO | v1 | Lasting Effects |
|---|---|---|---|---|---|
| **Planned Migration** | Both DCs up. Step 0: graceful VM shutdown only (StopReplication deferred to per-group) | Per-group: StopReplication (idempotent no-op after Step 0) → StartVM wave by wave, gated on VM readiness | 0 | Yes | Yes |
| **Disaster Recovery** | May be down. Errors ignored | Per-group: StopReplication → SetSource → StartVM wave by wave, gated on VM readiness | >0 | Yes | Yes |
| **Reprotect** | N/A (storage-only, no VM changes) | StopReplication → SetSource → monitor replication health | N/A | Yes | Yes |
| **Test** *(post-v1)* | VMs keep running, untouched | Clone/snapshot volumes → start VMs in isolated network → validate → cleanup | N/A | No | None |

Planned migration and disaster recovery are the same core workflow with different configuration: planned migration adds a pre-execution phase (Step 0: stop origin VMs). The per-group path is always unified: `StopReplication → StartVM`, with each wave gated on VM readiness (VMs must reach Running state before the next wave starts). In the planned case, Step 0 has already moved volumes to NonReplicated, so the per-group StopReplication is an idempotent no-op. A single `FailoverHandler` implements both modes, parameterized by `GracefulShutdown` (whether Step 0 runs). Similarly, failover (from SteadyState) and failback (from DRedSteadyState) are the same operation — the handler does not distinguish direction. Reprotect (from FailedOver) and restore (from FailedBack) are also the same operation — a single `ReprotectHandler` implements both. Each controller instance is configured with a `--site-name` flag and only reconciles DRExecution steps for which it is the designated owner based on the current DR phase — the source site runs Step 0 in planned migration, the target site runs the per-group waves.

## Functional Requirements

### DR Plan Management

- **FR1:** Platform engineer can create a DRPlan by defining a wave label key and a max concurrent failovers parameter. VMs are associated to the plan by setting the `soteria.io/drplan: <planName>` label
- **FR2:** Platform engineer can view all DRPlans and their current state via `kubectl` or the OCP Console
- **FR3:** Orchestrator automatically discovers VMs with the `soteria.io/drplan` label matching the plan name and groups them into waves based on the wave label value
- **FR4:** VM exclusivity is structurally enforced — a Kubernetes label key can have only one value, so a VM can belong to at most one DRPlan
- **FR5:** Platform engineer can add a VM to an existing DRPlan by setting `soteria.io/drplan: <planName>` on the VM — no plan editing required
- **FR6:** Platform engineer can configure namespace-level volume consistency for a namespace via annotation, causing all VM disks in that namespace to form a single VolumeGroup
- **FR7:** Orchestrator enforces that all VMs belonging to a DRPlan in a namespace with namespace-level consistency are in the same wave (validated by admission webhook)
- **FR8:** Platform engineer can view the composition of a DRPlan (VMs, waves, volume groups) before execution via pre-flight check

### DR Execution & Workflow

- **FR9:** Operator can trigger a planned migration execution for a DRPlan when both datacenters are available — the source-site controller gracefully stops origin VMs (Step 0), then the target-site controller runs the per-group path (StopReplication → StartVM) wave by wave, with each wave gated on VM readiness (VMs must reach Running state before the next wave begins)
- **FR10:** Operator can trigger a disaster recovery execution for a DRPlan — the target-site controller runs the per-group path (StopReplication → SetSource → StartVM) wave by wave, with each wave gated on VM readiness, ignoring errors from the origin site. Drivers handle unreachable peers internally
- **FR11:** Orchestrator executes waves sequentially and operations within a wave concurrently, respecting `maxConcurrentFailovers` by chunking waves into DRGroups
- **FR12:** `maxConcurrentFailovers` always counts individual VMs regardless of consistency level. When namespace-level consistency is configured, the orchestrator creates DRGroup chunks such that all VMs in the same namespace and same wave are always fully contained in a single chunk. If remaining chunk capacity cannot fit the next namespace group, a new chunk is created (current chunk capacity may be underutilized). A pre-flight check validates that `maxConcurrentFailovers` is greater than or equal to the largest namespace+wave group — if not, execution is rejected
- **FR13:** Orchestrator uses fail-forward error handling — if a DRGroup fails, it is marked `Failed`, the engine continues with remaining groups, and the execution is reported as `PartiallySucceeded`
- **FR14:** Operator can manually retry a failed DRGroup if the VM is still in a healthy, known state on the original site
- **FR15:** Orchestrator rejects retry attempts when the starting state is non-standard or unpredictable, requiring manual intervention
- **FR16:** Operator can trigger re-protect after a failover or failback — orchestrator stops replication on the old active site (if reachable), transitions old active volumes to Target and new active volumes to Source, and monitors until replication is healthy. Re-protect from FailedOver establishes reverse replication (Reprotecting → DRedSteadyState). Re-protect from FailedBack restores original replication (ReprotectingBack → SteadyState). Both use the same ReprotectHandler.
- **FR17:** Operator can trigger failback from DRedSteadyState — orchestrator executes the same workflow as failover (planned_migration or disaster mode) using the same FailoverHandler. Failback lands at FailedBack state (unprotected). Operator must subsequently trigger restore (re-protect from FailedBack) to re-establish original replication and return to SteadyState.
- **FR18:** All failover operations require explicit human initiation — no automatic failure detection or auto-failover
- **FR19:** Execution mode (planned_migration, disaster, or reprotect) is specified at execution time, not on the DRPlan definition

### Storage Abstraction

- **FR20:** Orchestrator interacts with storage backends exclusively through a StorageProvider Go interface with 6 methods: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, SetSource, StopReplication, GetReplicationStatus. The replication model uses two engine-driven transitions (NonReplicated → Source via SetSource, Source → NonReplicated via StopReplication). The Target role is observable via GetReplicationStatus but not explicitly set by the engine — when one site calls SetSource, the paired site implicitly becomes the target. Drivers act as reconcilers — checking actual storage state before applying changes. Drivers are responsible for handling unreachable peers internally — the orchestrator does not pass force flags
- **FR21:** Orchestrator determines which StorageProvider driver to use by inspecting the storage class of the VMs' PVCs — no explicit storage configuration resource required
- **FR23:** No-op driver implements the full StorageProvider interface but performs no actual storage operations, enabling development, testing, and CI without storage infrastructure
- **FR24:** Storage vendor engineer can implement a new StorageProvider driver by implementing the 6-method Go interface and running the conformance test suite
- **FR25:** Orchestrator supports heterogeneous storage within a single DRPlan — different VMs can use different storage backends, each handled by the appropriate driver

### Cross-Site Shared State

- **FR26:** DR resources (DRPlan, DRExecution, DRGroupStatus) are accessible via `kubectl` on both clusters and return the same data under normal operation
- **FR27:** Each cluster can read and write DR resources locally without cross-datacenter latency
- **FR28:** When one datacenter fails, the surviving cluster continues to operate normally — reading existing plans and writing new execution records
- **FR29:** When a failed datacenter recovers, DR state automatically reconciles without manual intervention
- **FR30:** Concurrent writes from both sites are resolved via last-write-wins, with lightweight transactions for critical state transitions (e.g., plan state changes). Controllers use ScyllaDB-tuned retry backoff for status updates and strategic merge patches to reduce conflict surface
- **FR30a:** Each controller instance is configured with a `--site-name` flag and computes its reconcile role (Owner, Step0Only, or None) based on the current DRPlan transition phase. Only the target site runs per-group wave execution; the source site runs Step 0 in planned migration mode and exits immediately in disaster mode. This eliminates cross-site write contention on DRExecution status
- **FR30b:** After all StartVM operations in a wave complete, the controller verifies VMs have reached Running state before advancing to the next wave. A configurable per-plan timeout (default 5 minutes) controls how long to wait; timeout behavior is mode-dependent (fail-forward in disaster, fail-fast in planned migration)

### Monitoring & Observability

- **FR31:** Platform engineer can view replication health status (Healthy/Degraded/Error) for every volume group protected by a DRPlan
- **FR32:** Platform engineer can view estimated RPO (time since last successful replication sync) for each protected volume group
- **FR33:** Orchestrator exposes Prometheus metrics: VMs under DR plan (gauge), failover execution duration (histogram), RPO/replication lag per volume group (gauge), execution success/failure counts (counter)
- **FR34:** Platform engineer can identify unprotected VMs — VMs not covered by any DRPlan

### OCP Console Plugin (v1 Scope)

**Design philosophy:** The Console serves two user modes equally — Planning Mode (calm: setting up plans, configuring protection, reviewing health — wants clarity, completeness, confidence) and Disaster Mode (panic: 3 AM, primary DC is dark — wants speed, simplicity, zero ambiguity). All views must be effective in both contexts.

- **FR35:** Platform engineer can view a DR Dashboard showing all DRPlans with their cross-cluster status (see FR40) and last execution result. Plans with broken or degraded replication display a prominent alert banner with a direct re-protect action (e.g., "1 DR Plan running UNPROTECTED: payments → [Reprotect Now]")
- **FR36:** Platform engineer can view a DRPlan detail view showing wave composition, VM membership, and context-aware action buttons (only valid state transitions enabled — e.g., SteadyState enables Failover; FailedOver enables Reprotect)
- **FR37:** Operator can trigger failover from the Console with a pre-flight confirmation dialog showing VM count, estimated RPO (time since last replication sync), estimated RTO (based on last execution duration), DR site capacity, and summary of actions
- **FR38:** Operator must type a confirmation keyword (e.g., "FAILOVER") to prevent accidental execution
- **FR39:** Operator can monitor a live execution via a Gantt chart-style progress view showing per-wave, per-DRGroup timeline, elapsed time, estimated remaining time, and inline error display with retry action — designed to be shareable on a bridge call
- **FR40:** Console shows cross-cluster awareness — a table lists every DRPlan with columns representing the two clusters involved in the plan. Cells show whether the application is currently active or passive on each site, and a separate column shows the protection status (i.e., whether volumes are replicating correctly). Because both clusters read from the same ScyllaDB state, this view appears identical regardless of which cluster's Console it is displayed on

### Audit & Compliance

- **FR41:** Every DRPlan execution creates an immutable DRExecution record with per-wave, per-DRGroup, per-step status, timestamps, and error details
- **FR42:** Platform engineer can view the execution history for any DRPlan, including all past executions and their outcomes
- **FR43:** DRExecution records persist across datacenter failures and are available on both clusters via the shared state layer

### Access Control & Security

- **FR44:** All authorization uses Kubernetes-native RBAC applied to Soteria's CRDs — separate permissions for plan viewing, plan management, and failover execution
- **FR45:** Storage credentials are referenced from Kubernetes Secrets or HashiCorp Vault — the orchestrator never stores credentials directly

## Non-Functional Requirements

### Reliability

- **NFR1:** The orchestrator must resume an in-progress execution from the last known checkpoint after a pod restart — no manual re-trigger required. DRExecution state must be persisted frequently enough that a restart loses at most one in-flight DRGroup operation.
- **NFR2:** The orchestrator must run as multiple instances in an active/passive configuration, with the active instance elected via Kubernetes Leases. If the active instance fails, a standby instance acquires the lease and resumes operations.
- **NFR3:** Target 99% failover execution success rate across all execution modes, measured over time. Failures attributable to orchestrator bugs (not external storage/infrastructure issues) must be exceptional.
- **NFR4:** The ScyllaDB-backed Aggregated API Server must remain available on the surviving cluster during a single-datacenter failure with no manual intervention required.
- **NFR5:** DRExecution writes during a disaster (when the other DC is down) must succeed locally with no dependency on cross-site connectivity.
- **NFR5a:** The orchestrator must handle ScyllaDB eventual consistency gracefully — status updates use tuned retry backoff (200ms base, factor 2.0, 8 steps with jitter) and strategic merge patches rather than full object replacement. The default Kubernetes retry policy (`retry.DefaultRetry`) is insufficient for ScyllaDB and must not be used.

### Performance

- **NFR6:** API response time for `kubectl get drplan` and Console dashboard queries must be under 2 seconds under normal operation.
- **NFR7:** Live execution monitor updates must be visible in the Console within 5 seconds of the underlying state change.

### Scalability

- **NFR8:** The orchestrator must support clusters with up to 5,000 VMs total.
- **NFR9:** The orchestrator must support up to 100 DRPlans per cluster with an average of 50 VMs per plan (5,000 VMs under DR protection).
- **NFR10:** Wave discovery and DRGroup chunking must complete within 10 seconds for a 50-VM plan.
- **NFR11:** Multiple DRPlan executions can run concurrently without interference (separate plans operating on disjoint VM sets).

### Security

- **NFR12:** All cross-site ScyllaDB replication traffic must be encrypted via TLS. TLS certificates are generated and managed by the cert-manager operator.
- **NFR13:** All communication between the extension API server and ScyllaDB must be encrypted via TLS.
- **NFR14:** The orchestrator must not log or expose storage credentials in any output — logs, events, metrics, or DRExecution records.
- **NFR15:** Admission webhooks validate DRPlan field constraints (waveLabel, maxConcurrentFailovers) and warn when a VM references a nonexistent DRPlan. VM exclusivity is structurally enforced by label semantics.

### Integration

- **NFR16:** The orchestrator must be compatible with OpenShift 4.x and integrate with OLM for lifecycle management (install, upgrade, uninstall).
- **NFR17:** The OCP Console plugin must use PatternFly components and follow Red Hat Console UI guidelines for consistent user experience.
- **NFR18:** Prometheus metrics must follow OpenShift monitoring conventions and be scrapeable by the in-cluster Prometheus stack without additional configuration.
- **NFR19:** The StorageProvider interface must be stable enough for external driver development — breaking changes require a new API version with a deprecation period.
