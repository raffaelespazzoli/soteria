# Story 17.5: ScyllaDB with Submariner

Status: review

## Story

As a platform engineer deploying Soteria with Submariner networking,
I want a step-by-step guide for deploying ScyllaDB cross-DC with Submariner MCS,
so that I can set up the shared state layer without guesswork.

## Acceptance Criteria

**AC1: End-to-end steps are complete**
Given the Submariner guide
When a reader follows the steps from start to finish
Then they can deploy ScyllaDB cross-DC with Submariner MCS networking without referring to external documentation for critical steps

**AC2: cert-manager and TLS setup is covered**
Given the guide's TLS section
When a reader follows the cert-manager setup steps
Then they have a working shared CA and mTLS configuration for ScyllaDB inter-node communication

**AC3: ScyllaCluster with ServiceExport is documented**
Given the guide's ScyllaDB deployment section
When a reader applies the ScyllaCluster manifest
Then it includes ServiceExport configuration for Submariner MCS discovery

**AC4: Steps match actual scripts and overlays**
Given the documented steps
When compared against `hack/stretched-local-test.sh` and kustomize overlays in `hack/overlays/`
Then the guide accurately reflects the actual deployment procedure

**AC5: Convergence verification is included**
Given the guide
When a reader completes the deployment
Then there are verification steps to confirm ScyllaDB nodes have discovered each other and the cluster is healthy

## Tasks / Subtasks

- [x] Task 1: Extract deployment procedure from scripts (AC: 1, 4)
  - [x] 1.1: Read `hack/stretched-local-test.sh` end-to-end to extract the actual deployment sequence
  - [x] 1.2: Examine kustomize overlays in `hack/overlays/` for Submariner-specific configuration
- [x] Task 2: Document TLS and certificate setup (AC: 2)
  - [x] 2.1: Walk the cert-manager and TLS setup used in the script
  - [x] 2.2: Document shared CA setup, Issuer "soteria-internal", and mTLS configuration
- [x] Task 3: Write the deployment guide (AC: 1, 2, 3, 4, 5)
  - [x] 3.1: Write `docs/installation/scylladb/submariner.md` covering: cert-manager setup, shared CA, scylla-operator install, ScyllaCluster with ServiceExport, mTLS configuration, convergence verification
  - [x] 3.2: Add verification commands (nodetool status, CQL connectivity checks)
  - [x] 3.3: Verify that documented commands match actual script behavior

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: hack/stretched-local-test.sh — THE source of truth for Submariner-based ScyllaDB deployment]
- [Source: hack/overlays/ — kustomize overlays for Submariner deployment]

### Code to Verify Against

- [Source: hack/stretched-local-test.sh — deployment sequence: uses kubectl contexts "etl6" and "etl7", kustomize overlays, cert-manager + Issuer "soteria-internal", scylla-operator, Submariner MCS API for cross-DC seed discovery]
- [Source: hack/overlays/base/kustomization.yaml — base overlay resources]
- [Source: hack/overlays/base/serviceexport.yaml — ServiceExport for Submariner MCS discovery]
- [Source: hack/overlays/base/scylladb-tls-patch.yaml — ScyllaCluster TLS volumes patch]
- [Source: hack/overlays/base/scylladb-tls-config.yaml — ScyllaDB mTLS scylla.yaml ConfigMap]
- [Source: hack/overlays/base/manager-scylladb-patch.yaml — Soteria TLS volume mounts]
- [Source: hack/overlays/base/manager-args-patch.yaml — Soteria ScyllaDB/APIServer args]
- [Source: hack/overlays/base/apiserver-cert.yaml — APIServer TLS Certificate]
- [Source: hack/overlays/base/apiserver-rbac.yaml — auth-delegation RBAC]
- [Source: hack/overlays/etl6/kustomization.yaml — etl6 DC-specific overlay (seed)]
- [Source: hack/overlays/etl6/scyllacluster-patch.yaml — datacenter.name: etl6]
- [Source: hack/overlays/etl7/kustomization.yaml — etl7 DC-specific overlay (joining)]
- [Source: hack/overlays/etl7/scyllacluster-patch.yaml — datacenter.name: etl7 + externalSeeds]

### Implementation Pattern

- Submariner uses MCS (Multicluster Services API) for cross-DC seed discovery via ServiceExport
- Two clusters: etl6 (seed DC) and etl7 (joining DC) — etl7 discovers etl6 via Submariner MCS DNS
- Prerequisites: cert-manager, scylla-operator, Submariner with MCS API active, Issuer "soteria-internal"
- Shared CA pattern: both clusters must trust the same CA for internode mTLS
- Kustomize overlays layer: base → DC-specific (etl6/etl7)
- Convergence verification: `nodetool status` should show nodes from both DCs as UN (Up/Normal)

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/installation/scylladb/submariner.md` | NEW | Step-by-step ScyllaDB deployment guide with Submariner MCS networking |

### Key Constraints

- `hack/stretched-local-test.sh` is the authoritative source — documentation must match this script's behavior
- Submariner with MCS API must be active between clusters before ScyllaDB deployment
- Shared CA is required — cross-CA trust is not supported
- Depends on: 17.1

### Project Structure Notes

- Submariner overlays: `hack/overlays/` with `base/`, `etl6/`, `etl7/` subdirectories
- Base overlay contains shared resources (TLS patches, configs, RBAC)
- DC overlays contain datacenter-specific patches (ScyllaCluster DC name, external seeds)

### References

- [Source: hack/stretched-local-test.sh — lines 17-37, script header with architecture and prerequisites]
- [Source: hack/overlays/ — 18 files total across base/, etl6/, etl7/ directories]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor)

### Debug Log References

- Read all 18 kustomize overlay files from hack/overlays/{base,etl6,etl7}/
- Read hack/stretched-local-test.sh end-to-end (572 lines)
- Validated mkdocs build with --strict; fixed 2 broken anchor links (#step-N-- → #step-N-)
- All Go tests pass (make test), no regressions from documentation changes

### Completion Notes List

- ✅ Task 1: Extracted full deployment sequence from stretched-local-test.sh and all 18 overlay files
- ✅ Task 2: Documented cert-manager TLS setup including shared CA requirement, soteria-internal Issuer, all 5 Certificate resources, mTLS scylla.yaml ConfigMap, combined CA bundle, and the operator volume propagation workaround
- ✅ Task 3: Wrote comprehensive guide covering architecture overview, prerequisites table, cert-manager setup, ScyllaDB mTLS config, Submariner MCS ServiceExport, kustomize overlay structure, step-by-step deploy for etl6 (seed) and etl7 (joining), post-deploy STS TLS volume patch, convergence verification (nodetool status, CQL connectivity, keyspace validation, APIService check), manager flag reference table, tear-down instructions, and troubleshooting section
- mkdocs build passes with --strict, no broken links

### File List

| File | Action |
|------|--------|
| `docs/installation/scylladb/submariner.md` | MODIFIED (replaced placeholder with full guide) |
| `_bmad-output/implementation-artifacts/17-5-scylladb-submariner.md` | MODIFIED (status + task checkboxes + dev record) |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | MODIFIED (17-5 status: ready-for-dev → review) |

### Change Log

- 2026-09-05: Implemented story 17.5 — wrote comprehensive ScyllaDB with Submariner deployment guide based on hack/stretched-local-test.sh and hack/overlays/
