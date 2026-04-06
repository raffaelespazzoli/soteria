---
title: "Product Brief: Soteria"
status: "complete"
created: "2026-04-04"
updated: "2026-04-04"
inputs:
  - _bmad-output/brainstorming/brainstorming-session-20260404-121402.md
  - "Web research: Kubernetes DR competitive landscape, VMware migration trends"
---

# Product Brief: Soteria

*Soteria (Σωτηρία) — Greek goddess of safety, preservation, and deliverance from harm.*

## Executive Summary

Every enterprise running VMs on OpenShift Virtualization today faces the same question: *what happens when a datacenter goes dark?* The answer, depending on which storage vendor they chose, ranges from "we have a partial solution" to "you're on your own."

**Soteria** is an open-source, Kubernetes-native disaster recovery orchestrator for OpenShift Virtualization. It provides storage-agnostic DR across heterogeneous storage backends — ODF, Dell, Pure Storage, NetApp — through a single, consistent workflow engine. Administrators define DR plans using standard Kubernetes labels. The orchestrator handles the rest: volume promotion, PVC rebinding, VM startup sequencing, throttling, and full audit trails. Both datacenters see the same DR state through a shared data layer that survives the very disaster it's designed to recover from.

The project exists because VMware SRM set the standard for VM disaster recovery, and the thousands of organizations migrating from vSphere to OpenShift Virtualization are discovering there is no equivalent. Soteria fills that gap — starting lean, growing toward SRM-class maturity, and doing it in the open so the entire Kubernetes ecosystem can benefit.

## The Problem

Organizations migrating virtualized workloads from VMware vSphere to OpenShift Virtualization lose access to VMware Site Recovery Manager — the industry's de facto DR orchestration standard. For many, DR is the gating item that blocks or delays the entire migration program. What they find on the other side:

- **Storage-specific DR, not storage-agnostic DR.** Red Hat's OpenShift DR (ODF + Ramen + ACM) provides solid DR capabilities for ODF/Ceph environments. But enterprises with heterogeneous storage — Dell, Pure, NetApp alongside ODF — have no unified orchestration layer. Each vendor's replication tooling operates independently, forcing separate DR runbooks per storage backend.

- **Backup tools where orchestration is needed.** Velero, OADP, Kasten, and Trilio solve backup and restore. They do not solve orchestrated failover with wave-based sequencing, pre-flight validation, concurrent throttling, and tested recovery plans. Operators who need SRM-class discipline are left assembling custom scripts.

- **VM workloads are harder than containers.** Stateful VMs with multiple persistent volumes, boot-order dependencies, and application-level consistency requirements cannot be protected with the same patterns used for stateless microservices. The Kubernetes DR ecosystem has largely focused on container workloads.

The cost of the status quo is measured in RTO/RPO commitments that cannot be tested, audit findings that cannot be closed, and migration projects that stall because "we can't move until DR is solved."

## The Solution

Soteria is a Kubernetes-native DR orchestrator that separates **what** to protect from **how** storage replicates. It consists of three layers:

**Storage Abstraction.** A Go interface modeled on CSI-Addons semantics (volume groups, replication enable/disable, promote/demote, resync) with per-vendor driver implementations. ODF passes through natively. Dell and Pure use transitional shims that become thinner as those vendors adopt CSI-Addons. NetApp uses a permanent translation layer to Trident Protect. The orchestrator itself never changes when a new storage vendor is added — only a new driver is written.

**Workflow Engine.** A purpose-built executor that operates on DRPlan custom resources. VMs self-organize into plans and waves via Kubernetes labels. The engine handles the full 4-state DR cycle (steady state → failover → re-protect → failback) with three execution modes from the same plan definition: planned migration, disaster recovery, and non-disruptive testing. Waves execute sequentially; operations within a wave execute concurrently with configurable throttling. Errors are handled fail-forward — partial success is reported, failed groups are retried manually.

**Shared State.** DR metadata must survive the disaster it's designed to recover from. Both datacenter instances share plans, executions, and status through a Kubernetes Aggregated API Server backed by ScyllaDB with async cross-site replication. Each instance operates independently during a partition — `kubectl get drplan` returns the same data on both clusters, and continues working when one cluster is down. When the failed datacenter recovers, ScyllaDB's anti-entropy repair synchronizes automatically. The database's built-in reconciliation replaces what would otherwise be a complex, bespoke CRD-sync protocol.

## What Makes This Different

| | Soteria | ODF/Ramen | Velero/OADP | SRM |
|---|---|---|---|---|
| Storage-agnostic | Yes — pluggable drivers | ODF/Ceph only | CSI snapshots (no replication orchestration) | vSphere SRAs |
| VM-aware orchestration | Waves, throttling, consistency groups | Basic relocate/failover | Backup/restore | Full |
| Kubernetes-native | CRDs, labels, kubectl, RBAC | Yes | Yes | No |
| Open source | Yes | Partially | Yes | No |
| Multi-vendor storage | ODF, Dell, Pure, NetApp | No | N/A | Via SRAs |

**The core differentiator is the combination**: storage-agnostic + VM-aware orchestration + Kubernetes-native + open source. Each individual capability exists somewhere; no existing project combines them. Critically, investment in Soteria is not invalidated when storage contracts or arrays change — the orchestrator is a long-term hedge against vendor churn.

**Design choices that build trust:** Human-triggered failover eliminates split-brain risk. Fail-forward execution with partial success reporting gives operators honest status during incidents rather than brittle all-or-nothing automation. Plans and waves are declarative Kubernetes resources compatible with GitOps review workflows.

**Why open source:** This is a Red Hat project — open source is how we build. More importantly, a storage-agnostic orchestrator cannot be credibly built by a single vendor. The pluggable driver model invites storage vendors and the broader Kubernetes community to contribute, validate, and adopt.

**Relationship to existing projects:** Soteria is complementary to the ODF/Ramen/ACM stack, not a replacement. For ODF-only environments, Ramen remains the integrated path. Soteria addresses the multi-vendor gap — orchestrating DR across heterogeneous storage backends where no single-vendor solution can reach. The ODF driver is Soteria's first implementation precisely because of that alignment.

**License:** Apache 2.0.

## Who This Serves

**Primary: Platform Engineers and Infrastructure Architects** managing OpenShift Virtualization deployments with enterprise DR requirements. They're responsible for RTO/RPO commitments, compliance audits, and DR testing. Today they're assembling DR from vendor-specific tooling, custom scripts, and manual runbooks. Soteria gives them a single control plane for all of it.

**Secondary: Storage Vendors** (Dell, Pure, NetApp, and others) who want their storage platforms to be first-class citizens in OpenShift Virtualization DR without building their own orchestration layer. Soteria's driver interface lets them participate in the DR story with a bounded, well-defined integration surface.

## Success Criteria

- **Functional orchestrator** capable of executing the full DR lifecycle (failover, re-protect, failback) against real OpenShift Virtualization clusters
- **Three or more storage vendor drivers** implemented and validated (ODF + at least two of Dell, Pure, NetApp)
- **No-op driver** available from Day 1 for development, testing, and CI without requiring real storage replication infrastructure
- **Community adoption signals**: external contributors, storage vendor co-maintainers, public adopters list, production deployment case studies

## Scope

**In scope for v1:**
- StorageProvider Go interface with ODF driver and no-op/dummy driver
- DRPlan CRD with label-driven wave formation and execution
- Failover, re-protect, and failback workflows (planned + disaster modes)
- DRExecution audit trail
- ScyllaDB-backed Aggregated API Server for cross-cluster shared state
- CLI and kubectl-native interaction

**Explicitly out of scope:**
- Network orchestration (IP reassignment, DNS updates, load balancer reconfiguration) — Soteria owns storage and VM sequencing; network cutover is handled by external tooling (ACM, Ansible, DNS providers) via the hook framework
- OCP Console plugin UI (planned for post-v1)
- Container/pod workload DR (VM-first by design; extension points preserved for future scope)
- More than two datacenters
- Automatic failure detection or auto-failover (human-triggered by design, eliminating split-brain risk)
- DR test mode with volume cloning (planned for post-v1)

## Vision

If Soteria succeeds, it becomes the **SRM of the Kubernetes era** — the standard way enterprises protect virtualized workloads running on OpenShift and, eventually, any KubeVirt-based platform.

**Near-term (v1-v2):** Functional orchestrator with ODF + no-op drivers. Prove the architecture works end-to-end. Attract early adopters and storage vendor partners.

**Mid-term (v2-v3):** Dell, Pure, and NetApp drivers. OCP Console plugin with dashboard, pre-flight checks, live execution monitoring, and replication health tracking. DR test mode with volume cloning. Hook framework (Kubernetes Jobs, guest agent commands, webhooks).

**Long-term:** SRM feature parity — full protection group management, automated DR testing schedules, compliance reporting, multi-application orchestration with cross-plan dependencies, and a vibrant ecosystem of community-contributed storage drivers extending beyond the initial four vendors.
