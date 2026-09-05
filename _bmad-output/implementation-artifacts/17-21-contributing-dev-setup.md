# Story 17.21: Contributing: Development Setup

Status: done

## Story

As a new contributor to Soteria,
I want a comprehensive development setup guide,
so that I can clone, build, test, and run Soteria locally with minimal friction.

## Acceptance Criteria

**AC1: Clone and prerequisites are documented**
Given the development setup guide
When a new contributor starts from scratch
Then they find: clone instructions, required tools (Go version, kubebuilder, other dependencies), and how to verify the setup

**AC2: Makefile targets are documented**
Given the development setup guide
When a contributor wants to know available commands
Then they find a walkthrough of key Makefile targets: `make test`, `make lint`, `make manifests`, `make run`, `make dev-cluster`, and others

**AC3: Testing pyramid is documented**
Given the development setup guide
When a contributor wants to understand the test strategy
Then they find the testing pyramid: unit tests, integration tests (envtest), multisite tests, and how to run each level

**AC4: CI structure is documented**
Given the development setup guide
When a contributor wants to understand CI
Then they find an overview of the CI pipeline structure and what checks run on PRs

**AC5: Local development with no-op driver is documented**
Given the development setup guide
When a contributor wants to run locally without real storage
Then they find instructions for local development using the no-op driver

**AC6: Guide matches actual project tooling**
Given the documented setup steps
When compared against the actual Makefile, CI configuration, and project structure
Then all commands, paths, and tool versions are accurate

## Tasks / Subtasks

- [x] Task 1: Research project tooling and conventions (AC: 1, 2, 6)
  - [x] 1.1: Read AGENTS.md for documented project conventions and CLI commands
  - [x] 1.2: Read `_bmad-output/project-context.md` for project overview, coding standards, package naming
  - [x] 1.3: Walk the Makefile for all available targets and their descriptions
  - [x] 1.4: Read `go.mod` for Go version and key dependencies
- [x] Task 2: Research testing infrastructure (AC: 3, 6)
  - [x] 2.1: Walk test infrastructure: `pkg/*/` test files for unit test patterns
  - [x] 2.2: Walk `test/multisite/` for multisite test patterns
  - [x] 2.3: Walk `test/e2e/` for end-to-end test setup
  - [x] 2.4: Walk `pkg/drivers/conformance/suite.go` for driver conformance tests
- [x] Task 3: Research CI pipeline (AC: 4, 6)
  - [x] 3.1: Walk `.github/workflows/test.yml` for unit/integration test pipeline
  - [x] 3.2: Walk `.github/workflows/lint.yml` for linting checks
  - [x] 3.3: Walk `.github/workflows/test-e2e.yml` for e2e test pipeline
  - [x] 3.4: Walk `.github/workflows/pr-operator.yml` for PR checks
- [x] Task 4: Research local dev workflow (AC: 5, 6)
  - [x] 4.1: Document `make dev-cluster` for local OpenShift dev with no-op driver
  - [x] 4.2: Document `make run` for running the controller locally against kubeconfig
- [x] Task 5: Write the documentation page (AC: 1, 2, 3, 4, 5)
  - [x] 5.1: Write `docs/contributing/dev-setup.md` covering clone, prerequisites, Makefile walkthrough, testing pyramid, CI structure, local development
  - [x] 5.2: Verify all documented commands work against the current codebase
  - [x] 5.3: Verify tool version requirements match go.mod, CI config

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: AGENTS.md — project structure, critical rules (never edit generated files), CLI commands, testing, deployment workflow, API design markers]
- [Source: _bmad-output/project-context.md — coding standards, package naming (lowercase single word), log levels (Info(0)=state transitions, V(1)=normal ops, V(2)=debug), error patterns (sentinel errors with Err prefix), test strategy]

### Code to Verify Against

- [Source: Makefile — key targets: `manifests` (generate RBAC/webhook), `generate` (deepcopy via hack/update-codegen.sh), `fmt`, `vet`, `test` (unit + envtest, excludes e2e and console-plugin), `setup-test-e2e`, `docker-build`, `docker-push`, `deploy`, `dev-cluster`, `run`]
- [Source: Makefile — IMG=quay.io/soteria-project/soteria:latest, CONTAINER_TOOL=podman, envtest for K8s API + etcd]
- [Source: .github/workflows/test.yml — unit/integration test CI pipeline]
- [Source: .github/workflows/lint.yml — linting CI pipeline]
- [Source: .github/workflows/test-e2e.yml — e2e test CI pipeline with Kind cluster]
- [Source: .github/workflows/pr-operator.yml — PR checks pipeline]
- [Source: .github/workflows/release-operator.yml — release pipeline]
- [Source: _bmad-output/project-context.md — testing pyramid: unit tests (pkg/*_test.go), ScyllaDB storage tests (envtest + testcontainers), integration tests (envtest + mock drivers), e2e tests (Ginkgo/Gomega on real cluster), driver conformance (pkg/drivers/conformance/suite.go)]

### Implementation Pattern

- Structure as a step-by-step getting started guide: Prerequisites → Clone → Build → Test → Run locally
- Include a Makefile target reference table with columns: Target, Description, When to Use
- Document the testing pyramid as a layered diagram or table:
  - Unit tests: `make test` (envtest, excludes e2e)
  - Multisite tests: `test/multisite/` (cross-cluster coordination)
  - E2e tests: `make setup-test-e2e` + Kind cluster
  - Driver conformance: `pkg/drivers/conformance/suite.go`
- Document the no-op driver: enables full dev/test/CI without storage infrastructure (pkg/drivers/noop/)
- Include CI pipeline overview showing what runs on PRs vs. releases

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/contributing/development.md | NEW | Dev setup guide: prerequisites, Makefile, testing, CI, local development |

### Key Constraints

- Never edit generated files: `config/crd/bases/*.yaml`, `config/rbac/role.yaml`, `**/zz_generated.*.go`
- Never remove scaffold markers: `// +kubebuilder:scaffold:*`
- Always use CLI commands (`kubebuilder create api/webhook`) to scaffold — do NOT create files manually
- E2E tests require an isolated Kind cluster (not real dev/prod cluster)
- `hack/update-codegen.sh` for deepcopy and OpenAPI generation (not `make generate` via controller-gen)

### Project Structure Notes

- Single-group layout: `cmd/`, `pkg/apis/`, `pkg/controller/`, `pkg/engine/`, `pkg/drivers/`, `pkg/admission/`
- Console plugin: separate TypeScript project at `console-plugin/`
- Entry points only in `cmd/`; public API for external driver authors in `pkg/`; non-importable in `internal/`
- Driver packages: `pkg/drivers/<vendor>/` — external authors import `pkg/`, never `internal/`

### References

- [Source: AGENTS.md — comprehensive project conventions and CLI reference]
- [Source: Makefile — all build/test/deploy targets]
- [Source: _bmad-output/project-context.md — coding standards, testing strategy, project structure]
- [Source: .github/workflows/ — CI pipeline definitions (test.yml, lint.yml, test-e2e.yml, pr-operator.yml, release-operator.yml)]
- [Source: pkg/drivers/noop/driver.go — no-op driver enabling dev without storage]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- No debug issues encountered — documentation-only story.

### Completion Notes List

- ✅ Researched all source files: AGENTS.md, project-context.md, Makefile (30 targets), go.mod (Go 1.25.3), all 7 CI workflows, no-op driver, conformance suite, test directories
- ✅ Wrote comprehensive dev setup guide at `docs/contributing/dev-setup.md` (~350 lines) covering all 6 ACs
- ✅ Guide structure: Prerequisites → Clone & Build → Makefile Reference (6 tables) → Testing Pyramid (5 layers with ASCII diagram) → CI Pipeline (4 sections: push/PR, PRs to main, docs, releases) → Local Development (dev-cluster, make run, no-op driver, container builds) → Project Structure → Common Workflows
- ✅ All documented commands verified against actual Makefile targets and CI workflow files
- ✅ Go version (1.25), container tool (podman), envtest version, golangci-lint version all match go.mod and Makefile
- ✅ MkDocs build passes in `--strict` mode with no errors
- ✅ Wrote to `docs/contributing/dev-setup.md` (existing placeholder in mkdocs.yml nav) rather than `docs/contributing/development.md` (story spec) to match site navigation
- ✅ All existing tests pass (`make test` green, exit code 0)

### File List

| File | Action | Description |
|------|--------|-------------|
| docs/contributing/dev-setup.md | MODIFIED | Replaced placeholder with comprehensive dev setup guide |
| _bmad-output/implementation-artifacts/17-21-contributing-dev-setup.md | MODIFIED | Updated task checkboxes, status, Dev Agent Record |

### Change Log

- 2026-09-05: Implemented story 17.21 — wrote comprehensive contributing/dev-setup guide covering prerequisites, clone/build, Makefile reference, testing pyramid, CI pipeline, and local development with no-op driver
