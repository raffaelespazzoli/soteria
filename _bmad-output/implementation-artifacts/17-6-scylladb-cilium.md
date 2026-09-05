# Story 17.6: ScyllaDB with Cilium Cluster Mesh

Status: ready-for-dev

## Story

As a platform engineer deploying Soteria with Cilium Cluster Mesh networking,
I want a step-by-step guide for deploying ScyllaDB cross-DC with Cilium,
so that I can set up the shared state layer using my existing Cilium infrastructure.

## Acceptance Criteria

**AC1: End-to-end steps are complete**
Given the Cilium guide
When a reader follows the steps from start to finish
Then they can deploy ScyllaDB cross-DC with Cilium Cluster Mesh without referring to external documentation for critical steps

**AC2: Cilium Cluster Mesh setup is covered**
Given the guide's networking section
When a reader follows the Cilium setup steps
Then they have Cilium installed and Cluster Mesh enabled between clusters

**AC3: Global service annotations are documented**
Given the guide's ScyllaDB deployment section
When a reader applies the ScyllaCluster manifest
Then it includes the global service annotation for Cilium Cluster Mesh discovery

**AC4: Steps match actual scripts and overlays**
Given the documented steps
When compared against `hack/multisite/setup-scylladb.sh`, `hack/multisite/README.md`, and overlays in `hack/multisite/overlays/`
Then the guide accurately reflects the actual deployment procedure

**AC5: Seed discovery pattern is documented**
Given the guide
When a reader reviews the ScyllaDB seed configuration
Then they understand how cross-DC seed discovery works with Cilium global services

**AC6: Convergence verification is included**
Given the guide
When a reader completes the deployment
Then there are verification steps to confirm ScyllaDB nodes have discovered each other across clusters

## Tasks / Subtasks

- [ ] Task 1: Extract deployment procedure from scripts (AC: 1, 4)
  - [ ] 1.1: Read `hack/multisite/setup-scylladb.sh` and `hack/multisite/README.md` for the actual deployment procedure
  - [ ] 1.2: Examine overlays in `hack/multisite/overlays/` for Cilium-specific configuration
- [ ] Task 2: Document networking and TLS (AC: 2, 3, 5)
  - [ ] 2.1: Walk the Cilium Cluster Mesh setup, global service annotations, and pod-to-pod routing
  - [ ] 2.2: Walk the cert-manager, shared CA, and mTLS setup used in the multisite scripts
  - [ ] 2.3: Document cross-DC seed discovery via Cilium global services (externalSeeds DNS)
- [ ] Task 3: Write the deployment guide (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 3.1: Write `docs/installation/scylladb/cilium.md` covering: Cilium install, Cluster Mesh setup, cert-manager, shared CA, scylla-operator, ScyllaCluster with global service annotation, mTLS, seed discovery, convergence verification
  - [ ] 3.2: Add verification commands (cilium clustermesh status, nodetool status, CQL connectivity checks)
  - [ ] 3.3: Verify global service annotations match what the actual overlays specify

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: hack/multisite/setup-scylladb.sh — primary source of truth for Cilium-based ScyllaDB deployment]
- [Source: hack/multisite/README.md — comprehensive environment documentation including architecture, prerequisites, file structure, troubleshooting]

### Code to Verify Against

- [Source: hack/multisite/setup-scylladb.sh — deployment sequence: cert-manager (Helm OCI, v1.20.3), shared CA (east generates, copied to west), scylla-operator (Helm, v1.21.0), XFS StorageClass, ScyllaCluster with broadcastOptions.nodes.type=PodIP, Cilium global service annotation, mTLS via STS patching, combined CA trust bundle, convergence verification (nodetool + CQL)]
- [Source: hack/multisite/overlays/base/kustomization.yaml — base ScyllaDB resources]
- [Source: hack/multisite/overlays/base/scyllacluster.yaml — base ScyllaCluster CR]
- [Source: hack/multisite/overlays/base/scylladb-tls-config.yaml — ScyllaDB mTLS scylla.yaml ConfigMap]
- [Source: hack/multisite/overlays/base/scylladb-tls-patch.yaml — ScyllaCluster TLS volumes patch]
- [Source: hack/multisite/overlays/east/kustomization.yaml — east DC overlay (seed)]
- [Source: hack/multisite/overlays/east/scyllacluster-patch.yaml — datacenter.name: east]
- [Source: hack/multisite/overlays/east/serviceexport.yaml — Cilium global service annotation for east]
- [Source: hack/multisite/overlays/west/kustomization.yaml — west DC overlay (joining)]
- [Source: hack/multisite/overlays/west/scyllacluster-patch.yaml — datacenter.name: west + externalSeeds]
- [Source: hack/multisite/overlays/west/serviceexport.yaml — Cilium global service annotation for west]

### Implementation Pattern

- Cilium uses global service annotation (`service.cilium.io/global: "true"`) on headless services for cross-cluster discovery
- ScyllaDB uses `broadcastOptions.nodes.type: PodIP` so internode addresses are routable via Cilium pod-to-pod routing
- Two clusters: east (seed DC) and west (joining DC) — west discovers east via externalSeeds DNS resolved through Cilium Cluster Mesh
- Shared CA: east generates the self-signed CA key-pair; it is copied to west (no cross-CA trust)
- Combined CA trust bundle: cert-manager CA + operator internal client CA merged into ConfigMap
- mTLS via cert-manager certificates + StatefulSet volume patching workaround (scylla-operator reconciles away STS patches)
- XFS-formatted StorageClass (`rook-ceph-block-xfs`) required for ScyllaDB PVCs
- Key environment variables: EAST_CLUSTER_NAME, WEST_CLUSTER_NAME, NAMESPACE (soteria), CERT_MANAGER_VERSION, SCYLLA_OPERATOR_VERSION, MEMBERS_PER_RACK

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/installation/scylladb/cilium.md` | NEW | Step-by-step ScyllaDB deployment guide with Cilium Cluster Mesh networking |

### Key Constraints

- `hack/multisite/setup-scylladb.sh` is the authoritative source — documentation must match this script's behavior
- Cilium Cluster Mesh must be connected between clusters before ScyllaDB deployment
- ScyllaDB must use PodIP broadcast for cross-cluster internode routing via Cilium
- Shared CA is mandatory — east generates CA, copied to west
- XFS filesystem required for ScyllaDB storage (xfsprogs must be available)
- STS TLS volume patching may be reverted by scylla-operator reconciliation — script handles re-application
- Depends on: 17.1

### Project Structure Notes

- Cilium multisite overlays: `hack/multisite/overlays/` with `base/`, `east/`, `west/` subdirectories
- Soteria-specific overlays: `hack/multisite/overlays/soteria/` with `base/`, `east/`, `west/`
- Full environment documentation: `hack/multisite/README.md` (700+ lines)

### References

- [Source: hack/multisite/setup-scylladb.sh — lines 17-48, script header with design decisions and prerequisites]
- [Source: hack/multisite/README.md — lines 477-590, ScyllaDB cross-DC deployment section with verification and troubleshooting]
- [Source: hack/multisite/overlays/ — 19 files total across base/, east/, west/, soteria/ directories]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
