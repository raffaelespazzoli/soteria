---
title: "Product Brief Distillate: Soteria"
type: llm-distillate
source: "product-brief-soteria.md"
created: "2026-04-04"
purpose: "Token-efficient context for downstream PRD creation"
---

# Product Brief Distillate: Soteria

## Rejected Ideas (do not re-propose)

- **Pure csi-addons-only orchestrator (no shims)**: Rejected because NetApp is not interested in csi-addons adoption. Dell and Pure are in talks but not there yet. Shim/translation layers are required for real-world multi-vendor support.
- **Lowest-common-denominator abstraction**: Rejected because it ignores vendor-specific capabilities. The chosen approach uses csi-addons-inspired semantics with per-vendor drivers that can expose full power.
- **DAG-based workflow model**: Rejected in favor of wave-based, label-driven model. Waves are simpler, match real DR plans, and are Kubernetes-native via labels.
- **Automatic failure detection / auto-failover**: Rejected. Human-triggered only to eliminate split-brain risk. Matches SRM model and industry practice that DR must be triggered externally.
- **Third site / quorum for orchestrator resilience**: Rejected. Explicit constraint: strictly two datacenters. ScyllaDB async replication solves cross-site state without a third site.
- **Storage Product API only (skip CSI)**: Rejected. Couples orchestrator to product versions. Hybrid approach chosen instead.
- **Dynamic throttling**: Rejected. Storage API endpoints and LUN discovery bottlenecks lack metrics for reliable dynamic throttling. Static per-plan `maxConcurrentFailovers` chosen.
- **Admin-defined DRGroups**: Rejected. DRGroups are execution-time batches created by the engine from wave contents + throttle parameter. Not user-facing.
- **Migration orchestration scope**: Explicitly rejected. Soteria is DR-only focused. Despite architectural overlap with lift-and-shift wave sequencing, migration is not a target use case.
- **Network orchestration**: Explicitly out of scope. No IP reassignment, DNS updates, or load balancer reconfiguration. Soteria owns storage + VM sequencing; network is handled externally via hooks.

## Requirements Hints

- **StorageProvider Go interface** must support: CreateVolumeGroup, DeleteVolumeGroup, GetVolumeGroup, EnableReplication, DisableReplication, PromoteVolume, DemoteVolume, ResyncVolume, GetReplicationInfo
- **DRPlan CRD** supports: type (disaster/planned/test), vmSelector (label-based), waveLabel, maxConcurrentFailovers, hooks (pre/post at plan/wave/VM level)
- **VM exclusivity rule**: A VM belongs to at most one DRPlan per plan type. A VM can appear in one planned + one disaster + one test plan simultaneously.
- **Volume consistency levels**: VM-level (default, each VM's disks form their own VolumeGroup) and namespace-level (opt-in via namespace annotation, all VM disks in namespace form one VolumeGroup). Mixed per plan.
- **Namespace-level consistency constraint**: All VMs in a namespace must be in the same wave. Enforced by admission control.
- **When namespace-level consistency is used**, maxConcurrentFailovers counts namespaces (the atomic unit), not individual VMs.
- **DRExecution CRD**: Immutable audit record per plan trigger. Tracks per-wave, per-DRGroup, per-step status with timestamps and errors. Enables compliance reporting and post-mortem analysis.
- **Error handling**: Fail-forward model. If a DRGroup fails mid-failover, mark it Failed, log the error, continue with remaining groups. Report PartiallySucceeded. Admin retries failed groups manually. Rollback is often impossible (active site may be down).
- **Re-protect**: Triggered separately after failover, not automatic. Purely a storage operation (DemoteVolume on old active, ResyncVolume, monitor via GetReplicationInfo until HEALTHY). No waves or throttling needed.
- **Hook types**: Kubernetes Job reference, command inside VM (via QEMU guest agent), HTTP webhook (external notification). Hooks at plan, wave, and VM level.
- **Three execution modes from one DRPlan definition**: Planned migration (both DCs up, graceful stop+sync), disaster recovery (active DC down, force:true, accept RPO>0), test (clone volumes, isolated network, tear down after — post-v1).
- **Full v1 feature set is non-negotiable**: ScyllaDB + Aggregated API Server + full DR lifecycle. These can be implemented across different epics but all ship in v1.

## Technical Context

- **Language**: Golang. Kubernetes/OpenShift API ecosystem.
- **Platform**: OpenShift Virtualization (KubeVirt). OCP Console plugin (post-v1, PatternFly, inherits auth/RBAC).
- **License**: Apache 2.0.
- **Storage abstraction**: Internal Go interface modeled on csi-addons semantics but decoupled from gRPC transport. When Dell/Pure adopt csi-addons natively, their drivers become trivial pass-throughs. NetApp's permanent shim is isolated.
- **CSI-Addons vendor status** (as of brainstorming session):
  - ODF: Implemented natively (VolumeReplication addon). No shim needed.
  - Dell: In talks with Red Hat. Currently uses CSM/repctl + DellCSIReplicationGroup. Likely temporary shim → eventual native.
  - Pure: In talks with Red Hat. Nothing at CSI level currently. Shim until native support exists.
  - NetApp: Not interested in csi-addons. Uses Trident Protect (AppMirrorRelationship, SnapMirror). Permanent shim.
- **Cross-cluster state**: Kubernetes API Aggregation Layer with extension API server storing DR resources in ScyllaDB instead of etcd.
  - ScyllaDB (Cassandra-compatible, lighter than full Cassandra) with NetworkTopologyStrategy (DC1:1, DC2:1).
  - Write/read consistency: LOCAL_ONE — all operations served locally, replicated async.
  - Conflict resolution: Last-write-wins (LWW) for most cases. Lightweight transactions for critical state transitions (e.g., IF status='SteadyState' THEN SET status='FailingOver').
  - Shared in ScyllaDB: DRPlan, DRExecution, DRGroupStatus, StorageProviderConfig.
  - Local in cluster etcd: VirtualMachine, PVC/PV, VolumeReplication, network configs.
  - Key insight: "The database's anti-entropy repair IS the reconciliation protocol."
- **Failover execution sequence** (per consistency unit within a DRGroup):
  1. PromoteVolume(volumeGroup, force:true for disaster)
  2. EnsurePVCBinding(VMs)
  3. StartVMs(VMs)
  4. WaitForHealthGate (optional)
- **Planned migration adds Step 0**: StopVMs on active + DemoteVolume + wait for final sync before proceeding.
- **4-state DR cycle**: Steady State → (failover) → Failed-over → (re-protect) → DR-ed Steady State → (failback) → Failed-back → (restore) → Steady State.
- **Driver implementation order** (by expected csi-addons adoption timeline): ODF (native) → Dell shim → Pure shim → NetApp shim (permanent).
- **No-op/dummy driver**: Implements full StorageProvider interface but performs no operations. Enables development, testing, and CI without real storage replication infrastructure.

## Detailed User Scenarios

- **Platform Engineer (planning mode)**: Sets up DRPlan via kubectl or GitOps. Adds labels to VMs (`dr.orchestrator/wave: "1"`, selector labels). Reviews auto-formed waves. Configures throttling. Runs test execution against no-op driver to validate plan structure. Runs test against real storage to validate end-to-end. Reviews DRExecution audit trail.
- **DR Operator (disaster mode, 3 AM)**: Receives alert that DC1 is down. Connects to DC2's cluster. Sees DRPlans in local ScyllaDB. Triggers disaster failover on the appropriate plan. Monitors wave-by-wave progress. Handles any partial failures by retrying failed DRGroups. After VMs are running, triggers re-protect to establish replication back to recovered DC1.
- **Storage Admin**: Deploys and configures StorageProviderConfig for their vendor (ODF, Dell, etc.). Monitors replication health via GetReplicationInfo. Responds to RPO alerts. Validates driver compatibility with new storage firmware/versions.
- **Storage Vendor Engineer (contributor)**: Implements StorageProvider interface for their platform. Uses no-op driver as reference implementation. Runs conformance tests. Submits driver as PR with documentation and e2e tests.

## Competitive Intelligence

- **Red Hat OpenShift DR (ODF + Ramen + ACM)**: Hub-managed regional DR with Ramen orchestrating relocate/failover. Strong for ODF/Ceph environments. Not designed for multi-vendor storage. Soteria is positioned as complementary — the ODF driver is the first implementation.
- **Velero / OADP**: De facto open-source backup/restore foundation. Not SRM-style orchestrated failover. Large YAML/operational burden for DR. Limited native multi-cluster failover UX.
- **Veeam Kasten (K10)**: Enterprise Kubernetes data protection with policy engine and UI. Commercial product. Backup-centric, not deep storage-agnostic regional active/passive orchestration.
- **Trilio for Kubernetes**: Application-centric backup/DR. Competes in backup/restore and app mobility more than Kubernetes-native SRM replacement.
- **Zerto for Kubernetes (Z4K)**: Continuous replication with proprietary stack. Less aligned with community-driven, storage-agnostic, CSI-addons-native open orchestrator.
- **VMware SRM**: The incumbent. Not Kubernetes-native. Customers migrating VMs to OpenShift Virtualization need a different control plane and CSI-driven data path. SRM is the north star for feature maturity, not a direct competitor on the same platform.
- **Market dynamics**: Broadcom/VMware disruption accelerates vSphere-to-OpenShift migration. Practitioners skeptical of "backup-only" solutions when leadership expects RTO/RPO testing and failover exercises. Storage heterogeneity is a common pain point — replication behavior varies by CSI/driver.

## Open Questions

- **ScyllaDB operational model**: Who deploys, upgrades, and backs up ScyllaDB? What's the minimum sizing per site? What happens if Scylla or the extension API server is unavailable during a DR event? Needs reference architecture.
- **Driver conformance / certification**: What testing and validation is required for a storage driver to be considered "supported"? Needs a conformance test suite definition.
- **Security model**: RBAC for DRPlans, encryption in transit/at rest for ScyllaDB, audit event schema for compliance, secrets management for storage API credentials. Needs explicit design.
- **Scale limits**: Maximum VMs per plan, maximum concurrent plans, maximum volume groups — needs benchmarking and documented limits.
- **Governance**: Steering model, how vendor priorities are balanced, maintainer ladder, RFC process for API changes, security embargo process. Needs separate governance document.
- **Relationship to broader KubeVirt community**: Could Soteria eventually target upstream KubeVirt beyond OpenShift? The brief positions OpenShift Virtualization as the target, with broader KubeVirt as a long-term possibility.

## Scope Signals

- **v1 must include**: StorageProvider interface, ODF driver, no-op driver, DRPlan CRD with waves, full DR lifecycle (failover/re-protect/failback), DRExecution audit, ScyllaDB + Aggregated API, CLI/kubectl. Deliverable across multiple epics.
- **Post-v1 planned**: OCP Console plugin UI, DR test mode with volume cloning, hook framework, Dell/Pure/NetApp drivers.
- **Permanent exclusions**: Network orchestration, auto-failover, more than two datacenters.
- **Deliberate boundaries**: VM-only (extension points preserved for containers), human-triggered only (by design, not limitation).

## Reviewer Insights Worth Preserving

- **Skeptic**: v1 scope is ambitious (Scylla + agg API + full lifecycle). Epic decomposition is essential. Consider what can be validated earliest to reduce risk.
- **Skeptic**: Fail-forward partial success needs concrete UX/API contracts — execution states, retry boundaries, idempotency guarantees.
- **Skeptic**: "Multi-vendor storage" claim invites proof burden. Each driver needs certification matrix and failure-mode documentation.
- **Opportunity**: Audit trails and DRExecution records are compliance assets, not just operational logs. Position for regulated industries.
- **Opportunity**: Label-driven plans are a low-friction adoption story vs. heavyweight proprietary UIs. Worth emphasizing in docs/marketing.
- **Opportunity**: Storage abstraction that survives new vendors without core changes is a long-term vendor hedge. Strong message for procurement.
- **OSS Adoption**: No-op driver + `make dev-cluster` recipe critical for contributor onboarding. Must work without real storage infrastructure.
- **OSS Adoption**: Enumerate non-storage contribution paths (CLI/UX, docs, observability, testing, API design) to broaden the contributor funnel.
- **OSS Adoption**: ScyllaDB is niche — justify the choice explicitly in project docs (why not etcd-only, tradeoffs, migration path).
- **OSS Adoption**: Pair "SRM of Kubernetes" tagline (resonates with buyers) with open-ecosystem messaging (resonates with contributors).
