# Story 17.7: Helm Chart Installation Guide

Status: done

## Story

As a platform engineer deploying Soteria,
I want a comprehensive Helm installation guide covering all deployment modes,
so that I can install Soteria using Helm with the correct configuration for my environment.

## Acceptance Criteria

**AC1: Helm repo setup is documented**
Given the installation guide
When a reader wants to add the Soteria Helm repo
Then they find clear instructions for repo setup or chart location

**AC2: Managed ScyllaDB installation is covered**
Given the installation guide
When a reader deploys with managed ScyllaDB (seed + joining modes)
Then the guide provides values examples and step-by-step instructions for both the seed site and joining site

**AC3: BYO ScyllaDB installation is covered**
Given the installation guide
When a reader has an existing ScyllaDB cluster
Then the guide provides values examples for connecting to an external ScyllaDB instance

**AC4: Values walkthrough is comprehensive**
Given the installation guide
When a reader reviews the values section
Then key values are explained with context (not just a flat reference — that's Story 17.20)

**AC5: Upgrade and uninstall procedures are documented**
Given the installation guide
When a reader needs to upgrade or remove Soteria
Then they find clear procedures for both operations

**AC6: Install script usage is documented**
Given the installation guide
When an install script exists in the chart or repository
Then its usage, flags, and behavior are documented

## Tasks / Subtasks

- [x] Task 1: Review Helm chart design (AC: 1, 2, 3, 4)
  - [x] 1.1: Read Epic 16 stories (16-1 through 16-6) for Helm chart design decisions
  - [x] 1.2: Walk the actual Helm chart (`charts/soteria/` or equivalent) to verify values and templates
- [x] Task 2: Write the installation guide (AC: 1, 2, 3, 4, 5, 6)
  - [x] 2.1: Write `docs/installation/helm.md` covering: Helm repo setup, managed ScyllaDB install (seed + joining), BYO ScyllaDB install, install script usage, key values walkthrough, upgrade procedures, uninstall procedures
  - [x] 2.2: Add example `helm install` commands for each deployment mode
- [x] Task 3: Verify documentation accuracy (AC: 4)
  - [x] 3.1: Verify all documented values exist in the actual `values.yaml`
  - [x] 3.2: Verify template behavior matches documented instructions

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/implementation-artifacts/16-1-chart-skeleton-and-values.md — Helm chart skeleton and values design]
- [Source: _bmad-output/implementation-artifacts/16-2-controller-templates.md — controller templates]
- [Source: _bmad-output/implementation-artifacts/16-3-scylladb-managed-templates.md — managed ScyllaDB templates]
- [Source: _bmad-output/implementation-artifacts/16-4-scylladb-external-wiring.md — BYO ScyllaDB wiring]
- [Source: _bmad-output/implementation-artifacts/16-5-console-plugin-templates.md — console plugin templates]
- [Source: _bmad-output/implementation-artifacts/16-6-standalone-ui-templates.md — standalone UI templates]

### Code to Verify Against

- [Source: charts/soteria/ (or equivalent) — actual Helm chart values.yaml, templates/ — will exist after Epic 16 is implemented]

### Implementation Pattern

- This story depends on Epic 16 (Helm chart) being implemented first
- Guide should cover three deployment modes: managed ScyllaDB (seed site), managed ScyllaDB (joining site), BYO ScyllaDB
- Key values walkthrough provides context beyond the flat reference in Story 17.20

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/installation/helm.md` | NEW | Comprehensive Helm installation guide with deployment mode examples |

### Key Constraints

- **Blocked on Epic 16** — Helm chart must exist before this guide can be written with code verification
- This is the user-friendly installation guide; the flat values reference is Story 17.20
- Depends on: 17.1, Epic 16

### Project Structure Notes

- Helm chart location TBD by Epic 16 (likely `charts/soteria/` or `dist/chart/`)

### References

- [Source: _bmad-output/implementation-artifacts/16-1-chart-skeleton-and-values.md — chart design decisions]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6 (via Cursor)

### Debug Log References
- Verified all documented values.yaml keys exist in actual chart at `charts/soteria/`
- Verified _validation.tpl error messages match documented troubleshooting table
- Verified install script flags match documentation (CA_SECRET_NAME=soteria-ca-key-pair, RELEASE_NAME=soteria)
- All unit tests pass (cached, no regressions from docs-only changes)

### Completion Notes List
- Created comprehensive Helm installation guide at `docs/installation/helm.md` (placeholder replaced)
- Covers chart location (local clone and OCI registry), deployment concepts, two-cluster topology
- Documents install script (`scripts/install-soteria.sh`) with full flag reference and usage examples
- Manual installation guide: 6 steps covering seed install (managed + BYO), CA propagation, joining install, verification
- Key values walkthrough: site, scylladb (managed/external), tls, controller, networking, ui — with context beyond flat reference
- Upgrade procedures: script-based and manual, with --reuse-values and CRD update notes
- Uninstall procedures: script-based and manual, including post-uninstall cleanup
- Troubleshooting section covering common issues (controller pod, validation errors, APIService)
- All documented values verified against actual `values.yaml` (30+ fields checked)
- All template validation rules verified against `_validation.tpl` and `scyllacluster.yaml`
- External seeds DNS documented per networking mode (Submariner: clusterset.local, Cilium: cluster.local)

### File List
- `docs/installation/helm.md` — MODIFIED — Comprehensive Helm installation guide (was placeholder)
- `_bmad-output/implementation-artifacts/17-7-helm-installation.md` — MODIFIED — Task checkboxes, status, dev record

### Change Log
- 2026-09-05: Implemented Story 17.7 — wrote comprehensive Helm installation guide covering all ACs
- 2026-09-05: Code review — fixed Cilium annotation claim (was incorrectly attributed to install script; now describes operator-level annotation requirement)
