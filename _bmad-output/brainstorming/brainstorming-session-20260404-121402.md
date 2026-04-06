---
stepsCompleted: [1, 2, 3, 4]
inputDocuments:
  - docs/Openshift Virtualization DR -- State of the Union (1).pdf
  - docs/OpenShift Virtualization DR Battlecard.pdf
session_topic: 'Storage-agnostic DR orchestrator for OpenShift Virtualization (Active/Passive Regional DR)'
session_goals: 'Design storage abstraction layer, DR workflow engine, two-datacenter orchestrator resilience, admin UI, SRM-parity growth path'
selected_approach: 'ai-recommended'
techniques_used: ['First Principles Thinking', 'Analogical Thinking (SRM parallels)']
ideas_generated: 51
session_active: false
workflow_completed: true
---

# Brainstorming Session Results

**Facilitator:** Raffa
**Date:** 2026-04-04
**Topic:** Storage-Agnostic DR Orchestrator for OpenShift Virtualization

## Session Overview

**Topic:** Design a storage-agnostic DR orchestrator for OpenShift Virtualization targeting Active/Passive (Regional DR) scenarios, with the long-term goal of achieving VMware SRM parity in a Kubernetes-native form.

**Goals:**
- Design a common storage abstraction over heterogeneous vendor APIs
- DR workflow engine with VM prioritization, throttling, and flexible granularity
- Two-datacenter orchestrator resilience
- Intuitive admin UI as an OCP Console plugin
- Golang + Kubernetes-native architecture

### Reference Documents
- OpenShift Virtualization DR -- State of the Union (Spazzoli, Jackson)
- OpenShift Virtualization DR Battlecard (vendor-by-vendor DR capability analysis)
- VMware SRM documentation (north star reference)
- CSI-Addons spec (https://github.com/csi-addons/spec) -- replication and volumegroup specs
- Dell CSM Replication docs (https://dell.github.io/csm-docs/docs/concepts/replication/)
- NetApp Trident Protect docs

---

## Theme 1: Storage Abstraction Layer

### The Core Problem

Every storage vendor exposes DR capabilities differently. There is no storage-agnostic way of writing DR orchestration today.

| Vendor | CSI-Addons Status | Current Interface | Shim Required? |
|---|---|---|---|
| **ODF** | Implemented (native) | VolumeReplication addon | No |
| **Dell** | In talks, Red Hat influencing | CSM/repctl + DellCSIReplicationGroup | Likely temporary → eventual native |
| **Pure** | In talks, Red Hat influencing | Nothing at CSI level | Shim until native |
| **NetApp** | Not interested | Trident Protect (AppMirrorRelationship, SnapMirror) | **Permanent shim** |

### The Fundamental DR Primitives

Distilled from first principles analysis of the 4-state DR cycle:

| Sub-operation | Layer | Vendor-specific? |
|---|---|---|
| Define consistency group (volume group) | Storage | YES |
| Stop/start replication | Storage | YES |
| Promote/demote volume (flip R/W) | Storage | YES (often entangled with replication) |
| Ensure correct PVC/PV binding | Storage/Compute bridge | Partially |
| Start/stop VM | Compute (KubeVirt) | NO -- standard API |
| Handle IP/network changes | Network | NO -- but environment-specific |

### Ideas Explored

1. **Lowest Common Denominator** -- Abstraction at lowest level all vendors support. Maximally portable but ignores vendor-specific capabilities.
2. **Tiered Plugin Interface** -- Mandatory low-level ops + optional higher-level ops. Vendors like NetApp expose full power; bare-bones vendors fall back to primitives.
3. **Storage Product API** -- Skip CSI extensions, talk to product APIs directly (like SRM's SRAs). Cleaner ops but coupled to product versions.
4. **Hybrid CSI/Product** -- CSI where mature, product API where not.
5. **CSI-Addons as Canonical Interface** -- Orchestrator speaks pure csi-addons. Vendors provide compliant drivers.
6. **CSI-Addons + Shim** -- Same as #5 with shim adapters for non-compliant vendors.
7. **Dual-Mode Driver** -- Single driver binary with native csi-addons mode + translated mode. Config flag to switch as vendors adopt.

### SELECTED: Internal Interface Inspired by csi-addons

The orchestrator defines its own **internal Go interface** modeled after csi-addons semantics:

```go
type StorageProvider interface {
    // Volume Group management
    CreateVolumeGroup(ctx, name, volumeIDs) (VolumeGroup, error)
    DeleteVolumeGroup(ctx, groupID) error
    GetVolumeGroup(ctx, groupID) (VolumeGroup, error)

    // Replication control
    EnableReplication(ctx, source ReplicationSource) error
    DisableReplication(ctx, source ReplicationSource) error
    PromoteVolume(ctx, source ReplicationSource, force bool) error
    DemoteVolume(ctx, source ReplicationSource, force bool) error
    ResyncVolume(ctx, source ReplicationSource) error
    GetReplicationInfo(ctx, source ReplicationSource) (ReplicationInfo, error)
}
```

- **ODF driver**: Thin pass-through to csi-addons gRPC
- **Dell driver**: Translates to CSM/repctl today, migrates to csi-addons pass-through later
- **Pure driver**: Translates to array-level API (shim until CSI support exists)
- **NetApp driver**: Permanent translation to Trident Protect CRs (AppMirrorRelationship)

**Why this works:** Semantics match csi-addons so when Dell/Pure adopt it natively, their drivers become trivial. NetApp's permanent shim is isolated. The orchestrator itself never changes.

### CSI-Addons to State Machine Mapping

| State Transition | Operations (passive side) | Operations (active side) |
|---|---|---|
| **Failover** | `PromoteVolume` (group becomes R/W) | `DemoteVolume` or nothing (DC may be down) |
| **Re-protect** | — (already promoted) | `DemoteVolume` then `ResyncVolume` |
| **Failback** | Mirror of failover | Mirror of failover |
| **Restore** | Mirror of re-protect | Mirror of re-protect |

---

## Theme 2: Workflow Engine

### The 4-State DR Cycle

```
Steady State ──failover──► Failed-over ──re-protect──► DR-ed Steady State
     ▲                                                         │
     └──────restore──── Failed-back ◄──failback────────────────┘
```

State transitions apply to individual VMs or groups of VMs. This supports:
1. Individual application failover
2. Tier-level failover
3. Full cluster failover with throttling and precedences

### DRPlan Model

**Multiple DRPlans coexist** with varying scope (one app, a group of apps, full cluster).

**VM Exclusivity Rule:** A VM belongs to at most one DRPlan per plan type (`planned`, `disaster`, `test`). A VM can be in both a planned migration plan and a disaster plan.

**Waves are auto-formed** from a well-known VM label (e.g., `dr.orchestrator/wave`), not manually defined. The orchestrator discovers VMs via label selector, groups by wave label, sorts waves lexicographically.

**Volume Consistency Levels (per-plan, mixed):**
- **VM-level (default):** Each VM's disks form their own VolumeGroup
- **Namespace-level (opt-in via namespace annotation):** All VM disks in a namespace form one VolumeGroup. All VMs in that namespace must be in the same wave (enforced by admission control)

**Static Throttling:** Per-plan `maxConcurrentFailovers` parameter. Bottleneck is storage API endpoints and LUN discovery -- neither has easily available metrics for dynamic throttling.

**Conceptual DRPlan CRD:**

```yaml
apiVersion: dr.orchestrator/v1
kind: DRPlan
metadata:
  name: erp-full-stack
spec:
  type: disaster  # or: planned, test
  vmSelector:
    matchLabels:
      app.kubernetes.io/part-of: erp-system
  waveLabel: dr.orchestrator/wave
  maxConcurrentFailovers: 3
  hooks:
    preFailover: ...
    postFailover: ...
```

### DRGroups: Execution-Time Chunking

DRGroups are NOT admin-defined. They are execution-time batches created by the engine to enforce throttling:

- Wave has 12 VMs, `maxConcurrentFailovers` = 4
- Engine creates: DRGroup-1 (4 VMs) → DRGroup-2 (4 VMs) → DRGroup-3 (4 VMs)
- DRGroups execute **sequentially** within a wave (batch model)
- Operations within a DRGroup execute **concurrently**

With namespace-level consistency, `maxConcurrentFailovers` counts namespaces (the atomic unit), not individual VMs.

### Failover Execution Sequence

```
DRPlan triggered (mode: disaster)
  ├─ Create DRExecution CRD (audit record)
  ├─ Discover VMs via vmSelector
  ├─ Group VMs into waves via waveLabel
  ├─ Determine consistency groups per VM/namespace
  ├─ Chunk waves into DRGroups
  │
  ├─ WAVE 1: For each DRGroup (sequential):
  │    For each consistency unit in DRGroup (concurrent):
  │      1. PromoteVolume(volumeGroup, force:true)
  │      2. EnsurePVCBinding(VMs)
  │      3. StartVMs(VMs)
  │      4. UpdateNetworking(VMs)
  │      5. WaitForHealthGate(optional)
  │    DRGroup complete → next DRGroup
  │  Wave complete → next wave
  │
  ├─ WAVE 2, 3, ...
  │
  └─ Update DRPlan status, DRExecution result
```

### Workflow Variants

| Mode | Pre-condition | Differences |
|---|---|---|
| **Planned migration** | Both DCs up | Step 0: StopVMs on active + DemoteVolume + wait for final sync |
| **Disaster recovery** | Active DC down | Skip step 0, use `force:true` on PromoteVolume, accept RPO > 0 |
| **Test** | Both DCs up | Clone volumes (snapshot), run against clones in isolated network, tear down after |

One DRPlan definition, three execution modes.

### DRExecution CRD (Audit Trail)

Every plan trigger creates an immutable `DRExecution` record tracking per-wave, per-DRGroup, per-step status with timestamps and errors. Enables compliance reporting and post-mortem analysis.

### Error Handling: Fail-Forward

Rollback is often impossible in DR (active site may be gone). If a DRGroup fails mid-failover, mark it `Failed`, log the error, continue with remaining groups. Report `PartiallySucceeded`. Admin retries failed groups manually.

### Re-Protect

Triggered separately after failover (not automatic). Purely a storage operation:
- `DemoteVolume` on old active (if reachable)
- `ResyncVolume` to establish replication new-active → old-active
- Monitor via `GetReplicationInfo` until HEALTHY

No waves or throttling needed -- VMs are already running.

### Hooks

Pre/post hooks at plan, wave, and VM level. Types:
- Kubernetes Job reference
- Command inside VM (via QEMU guest agent)
- HTTP webhook (external notification)

---

## Theme 3: Two-Datacenter Orchestrator Resilience

### The Paradox

The DR orchestrator must survive the disaster it's designed to recover from. In a two-DC setup with no third site, the orchestrator needs two instances that stay in sync.

### Key Design Decisions

1. **Human-triggered failover only** -- no automatic detection. Eliminates split-brain. Matches SRM model and the reality that "DR must be triggered externally, typically by a human."
2. **No third site** -- strictly two DCs.
3. **Each instance can operate locally** during disaster without contacting the other.

### SELECTED: ScyllaDB-Backed Kubernetes Aggregated API

**Architecture:**

```
DC1 (Active)                              DC2 (Passive)
┌─────────────────────────┐              ┌─────────────────────────┐
│  OpenShift Cluster 1    │              │  OpenShift Cluster 2    │
│                         │              │                         │
│  DR Extension API Server│◄── async ──►│  DR Extension API Server│
│  (K8s Aggregation Layer)│  ScyllaDB   │  (K8s Aggregation Layer)│
│         │               │   repl.     │         │               │
│  ScyllaDB Node(s)       │              │  ScyllaDB Node(s)       │
│         │               │              │         │               │
│  DR Orchestrator        │              │  DR Orchestrator        │
│  Controller             │              │  Controller             │
│                         │              │                         │
│  VMs (active)           │              │  VMs (standby)          │
└─────────────────────────┘              └─────────────────────────┘
```

**How it works:**

- DR-specific resources (`DRPlan`, `DRExecution`, `DRGroupStatus`, `StorageProviderConfig`) are registered via the [Kubernetes API Aggregation Layer](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/)
- An extension API server on each cluster stores these resources in **ScyllaDB** (Cassandra-compatible, lighter resource footprint) instead of native etcd
- ScyllaDB runs with `NetworkTopologyStrategy` (DC1:1, DC2:1) -- one replica per DC, async replication
- Write/read consistency: `LOCAL_ONE` -- all operations served locally, replicated async
- `kubectl get drplan` works on BOTH clusters and returns the same data

**What lives in ScyllaDB (shared):**
- DRPlan definitions, DRExecution records, DRGroupStatus, StorageProviderConfig

**What stays in native cluster etcd (local):**
- VirtualMachine (KubeVirt), PVC/PV, VolumeReplication (csi-addons), Network configs

**Disaster scenario:**
1. DC1 goes down → DC2's ScyllaDB has full replica, operates normally
2. Admin triggers failover on DC2 → reads plans from local ScyllaDB, executes workflow
3. Writes (execution records, status updates) succeed locally
4. When DC1 recovers → ScyllaDB anti-entropy repair auto-syncs all missed writes
5. No custom reconciliation protocol needed -- the database IS the reconciliation

**Conflict resolution:** Last-write-wins (LWW) sufficient for most cases. Optimistic locking via lightweight transactions for critical state transitions (`IF status = 'SteadyState' THEN SET status = 'FailingOver'`).

---

## Theme 4: UI/UX (OCP Console Plugin)

### Design Principles

Two user modes that must be served equally well:
- **Planning Mode (calm):** Setting up plans, configuring protection, running tests. Wants clarity, completeness, confidence.
- **Disaster Mode (panic):** 3 AM, primary DC is dark. Wants speed, simplicity, zero ambiguity.

### Key Views

#### 1. DR Dashboard

Top-level "Disaster Recovery" navigation item. At-a-glance view of all DR plans:

| Name | Primary | Secondary | Protected | Last Test |
|---|---|---|---|---|
| erp-full | dc1-prod | dc2-prod | ✅ Yes | 3d ago |
| crm-app | dc1-prod | dc2-prod | ✅ Yes | 7d ago |
| web-tier | dc1-prod | dc2-prod | ⚠️ Degraded | 31d ago |
| analytics | dc2-prod | dc1-prod | ✅ Yes | 1d ago |
| payments | dc2-prod | — | ❌ No | 14d ago |

- **Primary**: Cluster where VMs are currently running
- **Secondary**: Cluster where volumes are replicated to
- **Protected**: Is replication active and healthy? (Yes / Degraded / No)
- Prominent red banner for unprotected plans: "1 DR Plan running UNPROTECTED: payments → [Reprotect Now]"
- Unprotected VM counter: VMs not covered by any plan

#### 2. DR Plan Detail View

Tabbed view (Overview, History, Configuration):
- Wave visualization showing VMs, their consistency level, replication health
- Context-aware action buttons: only valid state transitions are enabled
  - SteadyState → [Failover] [Test] enabled
  - FailedOver → [Reprotect] enabled
  - DRedSteadyState → [Failback] [Test] enabled

#### 3. Failover Confirmation (Pre-Flight Check)

Not just "Are you sure?" -- a full pre-flight:
- VM count, storage provider reachability, compute capacity on DR site
- Replication lag → estimated RPO (data loss)
- Estimated RTO based on last test execution time
- Summary of actions to be taken
- Type "FAILOVER" to confirm (prevents accidental clicks)

#### 4. Live Execution Monitor (Gantt Chart)

Real-time progress during failover:
- Per-wave progress bars
- Per-DRGroup / per-VM step-by-step timeline
- Elapsed time and estimated remaining
- Inline error display with retry action
- The view to share on the bridge call with management

#### 5. Replication Health Monitoring

- Per-volume-group replication status (Healthy/Degraded/Error)
- RPO tracking with time-series graph
- Configurable alert thresholds (warn if RPO > 1m, critical if RPO > 5m)
- Answers: "If disaster hit RIGHT NOW, how much data would I lose?"

#### 6. Plan Creation Wizard

Step-by-step: Select VMs → Review auto-formed waves → Configure throttling → Add hooks → Review & create. Validates as you go (missing wave labels, exclusivity violations, missing replication).

#### 7. Cross-Cluster Awareness

Since both consoles read from ScyllaDB, the plugin shows the other cluster's perspective:
- Active side: "Failover TO dc2-prod" with capacity info
- Passive side: "Failover HERE" with local readiness

---

## Key Architectural Decisions Summary

| Decision | Choice | Rationale |
|---|---|---|
| Storage abstraction | csi-addons-inspired Go interface | Matches csi-addons semantics for future vendor adoption; decoupled from gRPC transport |
| NetApp support | Permanent shim driver | NetApp not interested in csi-addons; translate to Trident Protect CRs |
| Workflow model | Wave-based, label-driven, purpose-built | Simpler than DAG, matches real DR plans, Kubernetes-native via labels |
| Volume consistency | VM-level (default), namespace-level (opt-in annotation) | Mixed per plan; covers independent VMs and transactionally-coupled apps |
| DRGroups | Execution-time chunking | Not admin-defined; engine creates batches from wave contents + throttle |
| Throttling | Static per-plan parameter | Storage API and LUN discovery bottlenecks lack metrics for dynamic approach |
| VM exclusivity | Per plan type | A VM can be in one planned + one disaster + one test plan |
| Two-DC sync | ScyllaDB + K8s API Aggregation Layer | Eventually consistent, survives single-DC loss, Kubernetes-native UX |
| Failover trigger | Human-only | Eliminates split-brain; matches SRM model and industry practice |
| Conflict resolution | Last-write-wins + lightweight transactions | LWW sufficient for low-conflict DR metadata; LWT for critical state transitions |
| UI | OCP Console plugin | Zero context-switch, inherits auth/RBAC, PatternFly |
| Language | Golang | Kubernetes/OpenShift API ecosystem |

---

## Breakthrough Concepts

1. **ScyllaDB + API Aggregation Layer**: Eliminates the entire CRD-sync-between-clusters problem. DR resources are shared by construction, not by replication logic. The database's anti-entropy repair IS the reconciliation protocol.

2. **csi-addons-inspired interface (not bound to it)**: Gets the semantic benefits of the emerging standard without coupling to its gRPC transport or requiring vendor adoption. Creates a smooth migration path as vendors adopt csi-addons natively.

3. **Label-driven wave auto-formation**: VMs self-organize into DR plans and waves via standard Kubernetes labels. Adding a VM to DR protection = adding two labels. No plan editing needed.

4. **Mixed consistency levels**: VM-level for independent workloads, namespace-level for transactionally-coupled apps, configurable per-plan. Covers the full spectrum of real-world scenarios.

5. **Context-aware UI with pre-flight checks**: Action buttons that prevent invalid state transitions + a pre-flight dialog showing estimated RPO/RTO before failover. Designed for the 3 AM operator.

---

## Next Steps

1. **Architecture document**: Formalize the layer-cake architecture with component responsibilities and interfaces
2. **CRD design**: Define the DRPlan, DRExecution, DRGroupStatus, StorageProviderConfig API schemas
3. **Storage provider interface**: Define the Go interface and implement the ODF driver (first, since csi-addons is already supported)
4. **ScyllaDB + Extension API Server**: Prototype the aggregated API backed by ScyllaDB across two clusters
5. **Workflow engine**: Implement the purpose-built wave executor with DRGroup chunking
6. **OCP Console plugin**: Build the dashboard and plan management views
7. **Vendor drivers**: Dell shim → Pure shim → NetApp shim (in order of expected csi-addons adoption timeline)

---

## Session Insights

**Techniques Used:** First Principles Thinking (primary), Analogical Thinking (SRM parallels, Crossplane patterns), woven throughout with Role Playing perspectives from storage admins, app owners, and DR operators.

**Key Insight:** The storage abstraction problem is NOT as hard as it appears. When reduced to first principles, the vendor-specific surface is narrow (6 operations). The real complexity is in the workflow orchestration and the two-DC resilience -- and both have clean solutions (wave-based engine + ScyllaDB shared store).

**Total Ideas Generated:** 51 across 5 themes
**Session Duration:** ~90 minutes
