# Story 17.3: Prerequisites Guide

Status: ready-for-dev

## Story

As a platform engineer preparing to deploy Soteria,
I want a clear list of all prerequisites and their configuration requirements,
so that I can ensure my environment is ready before installation.

## Acceptance Criteria

**AC1: Storage prerequisites are documented**
Given the prerequisites page
When a reader checks storage requirements
Then they find: CSI Addons compatible storage with VolumeReplication and VolumeGroupReplication support, VolumeReplicationClass and VolumeGroupReplicationClass pre-configuration, and storage configured for actual replication

**AC2: Certificate manager prerequisites are documented**
Given the prerequisites page
When a reader checks TLS/cert requirements
Then they find: cert-manager installation requirement and Issuer/ClusterIssuer configuration

**AC3: ScyllaDB prerequisites for managed mode are documented**
Given the prerequisites page
When a reader plans to use managed ScyllaDB mode
Then they find: scylla-operator installation requirement and version constraints

**AC4: Prerequisites match actual CRD dependencies**
Given the documented prerequisites
When compared against RBAC markers in controller code and CRD imports
Then every external CRD dependency (VolumeReplication, VolumeGroupReplication, VirtualMachine, etc.) is listed

**AC5: Prerequisites match integration test fixtures**
Given the documented prerequisites
When compared against test fixtures and setup scripts
Then the required external resources match what the test infrastructure provisions

## Tasks / Subtasks

- [ ] Task 1: Identify CRD and API dependencies (AC: 4)
  - [ ] 1.1: Walk RBAC markers in `pkg/controller/` to identify all external CRD dependencies
  - [ ] 1.2: Review `config/rbac/role.yaml` for complete list of API groups and resources accessed
  - [ ] 1.3: Check scheme registrations in `cmd/soteria/main.go` for external type dependencies
- [ ] Task 2: Identify infrastructure prerequisites (AC: 1, 2, 3)
  - [ ] 2.1: Read PRD storage constraints and architecture doc dependencies section
  - [ ] 2.2: Read `pkg/apiserver/options.go` for ScyllaDB connection flags and TLS requirements
  - [ ] 2.3: Read setup scripts (`hack/multisite/setup-scylladb.sh`, `hack/stretched-local-test.sh`) for prerequisite lists
- [ ] Task 3: Write prerequisites page (AC: 1, 2, 3, 4, 5)
  - [ ] 3.1: Write `docs/installation/prerequisites.md` covering: storage, cert-manager, scylla-operator, KubeVirt
  - [ ] 3.2: Add version compatibility matrix if determinable from code
  - [ ] 3.3: Verify all mentioned CRDs and tools are actually used in the codebase

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — storage constraints, NFR12-NFR15 security requirements]
- [Source: _bmad-output/planning-artifacts/architecture.md — dependencies section, technology stack]

### Code to Verify Against

- [Source: pkg/apiserver/options.go — ScyllaDB connection flags: `--scylladb-contact-points`, `--scylladb-keyspace`, `--scylladb-local-dc`, `--scylladb-dc-replication`, `--scylladb-tls-cert`, `--scylladb-tls-key`, `--scylladb-tls-ca`, `--scylladb-tls-server-name`]
- [Source: config/rbac/role.yaml — external API group dependencies: `kubevirt.io` (virtualmachines), `replication.storage.openshift.io` (volumereplications, volumegroupreplications), `ceph.rook.io` (cephblockpools), `storage.k8s.io` (storageclasses)]
- [Source: cmd/soteria/main.go — scheme registrations: `kubevirtv1.AddToScheme`, `replicationv1alpha1.AddToScheme`]
- [Source: hack/multisite/setup-scylladb.sh — prerequisites: Minikube KVM2 clusters, Rook-Ceph StorageClass, Cilium Cluster Mesh, helm, jq; cert-manager v1.20.3, scylla-operator v1.21.0]
- [Source: hack/stretched-local-test.sh — prerequisites: kubectl with contexts, kustomize, cert-manager installed, scylla-operator installed, Submariner with MCS API, Issuer "soteria-internal"]

### Implementation Pattern

- Organize prerequisites by category: Kubernetes cluster, storage (CSI Addons + VolumeReplication CRDs), networking (Submariner or Cilium), cert-manager, ScyllaDB (managed vs BYO), KubeVirt
- Each prerequisite should include: name, minimum version (if determinable), what it's needed for, verification command
- Use mkdocs-material admonitions for important notes and warnings

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/installation/prerequisites.md` | NEW | Complete prerequisites guide with storage, cert-manager, ScyllaDB, KubeVirt requirements |

### Key Constraints

- External CRD dependencies from RBAC: `kubevirt.io/virtualmachines`, `replication.storage.openshift.io/volumereplications`, `replication.storage.openshift.io/volumegroupreplications`, `ceph.rook.io/cephblockpools`, `storage.k8s.io/storageclasses`
- ScyllaDB TLS is mandatory in production (all 4 TLS flags in options.go)
- cert-manager provides TLS for both ScyllaDB mTLS and aggregated API server serving cert
- Depends on: 17.1

### Project Structure Notes

- RBAC role at `config/rbac/role.yaml` is the authoritative list of external API dependencies
- ScyllaDB connection configuration in `pkg/apiserver/options.go` defines all required ScyllaDB flags

### References

- [Source: config/rbac/role.yaml — full RBAC manifest with all external API groups]
- [Source: pkg/apiserver/options.go — lines 66-86, ScyllaDB CLI flags]
- [Source: cmd/soteria/main.go — lines 42-43, external scheme registrations]
- [Source: hack/multisite/setup-scylladb.sh — lines 33-38, prerequisites section]
- [Source: hack/stretched-local-test.sh — lines 30-37, prerequisites section]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
