# Story 17.1: Documentation Site Setup

Status: done

## Story

As a platform engineer,
I want a fully configured mkdocs-material documentation site with CI/CD deployment,
so that all Soteria documentation is published automatically to GitHub Pages on every merge to main.

## Acceptance Criteria

**AC1: mkdocs.yml is valid and configured**
Given the `mkdocs.yml` file exists at the repository root
When I run `mkdocs build`
Then the site builds successfully with no errors

**AC2: Material theme with dark mode**
Given `mkdocs.yml` specifies the `material` theme
When I view the built site
Then it has a dark mode toggle, search enabled, and code highlighting configured

**AC3: Navigation structure covers all sections**
Given the `docs/` directory exists
When I inspect `mkdocs.yml` navigation
Then it contains sections for: Home, Installation (prerequisites, ScyllaDB, Helm), Architecture (overview, DR lifecycle, storage drivers), Usage (DRPlan, waves, volumes, failover, UI guides), Reference (API, Helm values), and Contributing (dev setup, storage drivers)

**AC4: GitHub Actions workflow deploys to GitHub Pages**
Given a `.github/workflows/docs.yml` workflow exists
When a commit is pushed to the `main` branch
Then the workflow builds the docs and deploys them to GitHub Pages

**AC5: Local preview works**
Given the docs site is configured
When I run `mkdocs serve`
Then a local development server starts and renders the documentation correctly

## Tasks / Subtasks

- [x] Task 1: Create mkdocs configuration (AC: 1, 2)
  - [x] 1.1: Create `mkdocs.yml` with material theme config (palette with dark/light toggle, search plugin, code highlighting with `pymdownx` extensions, admonitions, tabs, Mermaid diagrams)
  - [x] 1.2: Add docs dependency manifest (`requirements-docs.txt` or `pyproject.toml` section) for mkdocs-material, pymdownx extras, mkdocs-mermaid2-plugin
- [x] Task 2: Create docs directory structure (AC: 3)
  - [x] 2.1: Create `docs/` directory with `index.md` placeholder
  - [x] 2.2: Create placeholder subdirectories: `installation/scylladb/`, `architecture/`, `usage/ui-guide/`, `reference/api/`, `contributing/`
  - [x] 2.3: Define the full `nav` structure in `mkdocs.yml` covering all 21 remaining stories
- [x] Task 3: Create CI/CD workflow (AC: 4)
  - [x] 3.1: Create `.github/workflows/docs.yml` — trigger on push to main, install mkdocs-material, build, deploy to GitHub Pages
- [x] Task 4: Verify local and CI build (AC: 1, 5)
  - [x] 4.1: Verify `mkdocs build` completes successfully
  - [x] 4.2: Verify `mkdocs serve` renders the site locally

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — overall project scope informing nav structure]
- [Source: _bmad-output/planning-artifacts/architecture.md — section organization for Architecture nav]

### Code to Verify Against

- [Source: project root — no existing `mkdocs.yml` or `docs/` directory currently exists]
- [Source: .github/workflows/ — existing CI workflows (`pr-operator.yml`, `lint.yml`, `test.yml`, `test-e2e.yml`, `release-operator.yml`) for CI pattern reference]

### Implementation Pattern

- Use mkdocs-material theme with: palette toggle (dark/light), search plugin, pymdownx extensions (highlight, superfences, tabbed, arithmatex, details), admonitions, Mermaid diagram support via `pymdownx.superfences` custom fences
- Nav structure should mirror the Epic 17 story breakdown: Home → Installation (prerequisites, ScyllaDB overview/submariner/cilium, Helm) → Architecture (overview, DR lifecycle, storage drivers) → Usage (DRPlan, waves, volumes, failover, UI) → Reference (API DRPlan/DRExecution, Helm values) → Contributing (dev setup, storage drivers)
- GitHub Actions workflow should use `peaceiris/actions-gh-pages` or `mkdocs gh-deploy` pattern

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `mkdocs.yml` | NEW | mkdocs-material site configuration with full nav tree |
| `docs/index.md` | NEW | Landing page placeholder (populated by Story 17.2) |
| `requirements-docs.txt` | NEW | Python dependencies for mkdocs build |
| `.github/workflows/docs.yml` | NEW | GitHub Actions workflow for GitHub Pages deployment |
| `docs/installation/` | NEW | Directory for installation guide pages |
| `docs/installation/scylladb/` | NEW | Directory for ScyllaDB deployment guides |
| `docs/architecture/` | NEW | Directory for architecture documentation |
| `docs/usage/` | NEW | Directory for usage guides |
| `docs/usage/ui-guide/` | NEW | Directory for UI guide pages |
| `docs/reference/` | NEW | Directory for API and config references |
| `docs/reference/api/` | NEW | Directory for API reference pages |
| `docs/contributing/` | NEW | Directory for contributor documentation |

### Key Constraints

- No existing `docs/` directory or `mkdocs.yml` — this is a greenfield docs setup
- No dependencies — this is the foundation for all other Epic 17 stories
- All subsequent Epic 17 stories depend on this one for site structure

### Project Structure Notes

- Docs infrastructure lives at project root (`mkdocs.yml`, `docs/`)
- CI workflow follows existing patterns in `.github/workflows/`
- Python docs dependencies are isolated from Go project

### References

- [Source: .github/workflows/pr-operator.yml — existing CI workflow pattern reference]
- [Source: .github/workflows/lint.yml — existing CI workflow pattern reference]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Integration tests could not run (fs.inotify.max_user_instances limit) — environment constraint, not code issue; irrelevant to docs-only story
- mkdocs-mermaid2-plugin not needed: Mermaid support is provided natively by pymdownx.superfences custom fences in mkdocs-material ≥9.x

### Completion Notes List

- Created mkdocs-material site with dark/light palette toggle, search, code highlighting (pymdownx.highlight, pymdownx.superfences, pymdownx.inlinehilite), admonitions, tabs, Mermaid diagram support via pymdownx.superfences custom fences
- Created requirements-docs.txt with pinned version ranges (mkdocs>=1.6,<2, mkdocs-material>=9.6,<10, pymdown-extensions>=10.14,<11)
- Created full docs/ directory structure with 18 placeholder pages covering all nav sections
- Nav structure covers: Home, Installation (prerequisites, ScyllaDB/Submariner/Cilium, Helm), Architecture (overview, DR lifecycle, storage drivers), Usage (DRPlan, waves, volumes, failover, UI guides), Reference (API DRPlan/DRExecution, Helm values), Contributing (dev setup, storage drivers)
- Created .github/workflows/docs.yml with build-on-PR validation and deploy-on-push-to-main using peaceiris/actions-gh-pages@v4 with keep_files:true and destination_dir:docs to coexist with Helm chart hosting on gh-pages
- mkdocs build --strict passes with zero errors
- mkdocs serve starts successfully on localhost

### Change Log

- 2026-09-05: Story 17.1 implementation complete — docs site skeleton with mkdocs-material, full nav structure, CI/CD workflow

### File List

| File | Action |
|------|--------|
| `mkdocs.yml` | NEW |
| `requirements-docs.txt` | NEW |
| `.github/workflows/docs.yml` | NEW |
| `docs/index.md` | NEW |
| `docs/installation/prerequisites.md` | NEW |
| `docs/installation/helm.md` | NEW |
| `docs/installation/scylladb/overview.md` | NEW |
| `docs/installation/scylladb/submariner.md` | NEW |
| `docs/installation/scylladb/cilium.md` | NEW |
| `docs/architecture/overview.md` | NEW |
| `docs/architecture/dr-lifecycle.md` | NEW |
| `docs/architecture/storage-drivers.md` | NEW |
| `docs/usage/drplan.md` | NEW |
| `docs/usage/waves.md` | NEW |
| `docs/usage/volumes.md` | NEW |
| `docs/usage/failover.md` | NEW |
| `docs/usage/ui-guide/dashboard.md` | NEW |
| `docs/usage/ui-guide/plan-detail.md` | NEW |
| `docs/usage/ui-guide/execution-monitor.md` | NEW |
| `docs/reference/api/drplan.md` | NEW |
| `docs/reference/api/drexecution.md` | NEW |
| `docs/reference/helm-values.md` | NEW |
| `docs/contributing/dev-setup.md` | NEW |
| `docs/contributing/storage-drivers.md` | NEW |
| `.gitignore` | MODIFIED |
| `_bmad-output/implementation-artifacts/17-1-docs-site-setup.md` | MODIFIED |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | MODIFIED |
