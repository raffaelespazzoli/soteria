# Story 17.1: Documentation Site Setup

Status: ready-for-dev

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

- [ ] Task 1: Create mkdocs configuration (AC: 1, 2)
  - [ ] 1.1: Create `mkdocs.yml` with material theme config (palette with dark/light toggle, search plugin, code highlighting with `pymdownx` extensions, admonitions, tabs, Mermaid diagrams)
  - [ ] 1.2: Add docs dependency manifest (`requirements-docs.txt` or `pyproject.toml` section) for mkdocs-material, pymdownx extras, mkdocs-mermaid2-plugin
- [ ] Task 2: Create docs directory structure (AC: 3)
  - [ ] 2.1: Create `docs/` directory with `index.md` placeholder
  - [ ] 2.2: Create placeholder subdirectories: `installation/scylladb/`, `architecture/`, `usage/ui-guide/`, `reference/api/`, `contributing/`
  - [ ] 2.3: Define the full `nav` structure in `mkdocs.yml` covering all 21 remaining stories
- [ ] Task 3: Create CI/CD workflow (AC: 4)
  - [ ] 3.1: Create `.github/workflows/docs.yml` — trigger on push to main, install mkdocs-material, build, deploy to GitHub Pages
- [ ] Task 4: Verify local and CI build (AC: 1, 5)
  - [ ] 4.1: Verify `mkdocs build` completes successfully
  - [ ] 4.2: Verify `mkdocs serve` renders the site locally

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

### Debug Log References

### Completion Notes List

### File List
