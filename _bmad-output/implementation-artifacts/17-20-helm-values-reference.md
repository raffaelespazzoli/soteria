# Story 17.20: Helm Values Reference

Status: done

## Story

As a platform engineer customizing a Soteria deployment,
I want an exhaustive annotated reference for every Helm values parameter,
so that I can understand every configuration option without reading chart templates.

## Acceptance Criteria

**AC1: Every values parameter is documented**
Given the Helm values reference
When a reader searches for any parameter in `values.yaml`
Then they find it documented with: key path, type, default value, and description

**AC2: Parameters are organized by section**
Given the Helm values reference
When a reader browses the page
Then parameters are grouped by logical section (site, controller, tls, scylladb, networking, ui)

**AC3: Defaults are accurate**
Given the documented default values
When compared against the actual `values.yaml`
Then every default matches

**AC4: Template conditionals are reflected**
Given the Helm values reference
When a parameter controls conditional behavior (e.g., scylladb.mode toggles between managed and external templates)
Then the conditional logic is noted in the description

**AC5: Reference is complete**
Given the Helm values reference
When compared against all values used in chart templates
Then no parameter is missing from the documentation

## Tasks / Subtasks

- [x] Task 1: Extract Helm chart values (AC: 1, 2, 3)
  - [x] 1.1: Walk the actual `values.yaml` from the Helm chart (Epic 16)
  - [x] 1.2: Walk chart templates to identify all `{{ .Values.* }}` references
- [x] Task 2: Write the reference page (AC: 1, 2, 4)
  - [x] 2.1: Write `docs/reference/helm-values.md` with parameter tables organized by section
  - [x] 2.2: Note conditional behavior where values toggle template rendering
- [x] Task 3: Verify completeness (AC: 3, 5)
  - [x] 3.1: Verify every `{{ .Values.* }}` reference in templates has a corresponding documentation entry
  - [x] 3.2: Cross-reference with Story 17.7 (installation guide) to avoid redundancy

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — deployment model, Helm chart requirements]
- [Source: _bmad-output/planning-artifacts/architecture.md — deployment topology, site configuration]

### Code to Verify Against

- Helm chart source will be in Epic 16 output (not yet available)
- [Source: AGENTS.md — Helm chart plugin documentation: `kubebuilder edit --plugins=helm/v2-alpha`]

### Implementation Pattern

- Structure as a flat parameter reference table with columns: Key Path, Type, Default, Description
- Group by logical section: site, controller, tls, scylladb, networking, ui
- Note conditional behavior (e.g., `scylladb.mode: managed` vs `external` toggles different template sets)
- This is the flat parameter reference; the user-friendly installation guide is Story 17.7

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/reference/helm-values.md | NEW | Exhaustive Helm values parameter reference |

### Key Constraints

- **Blocked on: Epic 16 (Helm chart must exist before this doc can be written)**
- Minimal Dev Notes since chart source is not yet available
- This is the flat parameter reference; the user-friendly installation guide is Story 17.7

### Project Structure Notes

- Helm chart expected location: `dist/chart/` or `charts/chart/` depending on Epic 16 output

### References

- [Source: AGENTS.md — Helm chart scaffolding and management commands]
- [Source: _bmad-output/planning-artifacts/prd.md — deployment requirements]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor)

### Debug Log References

None — implementation was straightforward with no errors.

### Completion Notes List

- Extracted all 40 unique `.Values.*` references from 33 chart templates in `charts/soteria/`
- Organized parameters into 7 logical sections: Global, Site, Controller, TLS, ScyllaDB, Networking, UI
- Documented every parameter with key path, type, default value, and description
- Added conditional behavior tables for `scylladb.mode`, `networking.mode`, and `ui.mode` explaining which templates are rendered and which validations fire
- Documented all validation rules from `_validation.tpl` (required fields, enum constraints, conditional requirements)
- Verified 100% completeness: every `.Values.*` reference in templates has a corresponding documentation entry
- Cross-referenced with Story 17.7 (installation guide placeholder) — helm-values.md links to it but avoids overlap since 17.7 is not yet written
- MkDocs builds successfully with `--strict` mode
- All existing unit tests pass with no regressions

### File List

| File | Action | Description |
|------|--------|-------------|
| docs/reference/helm-values.md | MODIFIED | Replaced stub with exhaustive Helm values parameter reference |
| _bmad-output/implementation-artifacts/17-20-helm-values-reference.md | MODIFIED | Updated task checkboxes, status, Dev Agent Record |

### Change Log

- 2026-09-05: Implemented story 17-20 — wrote exhaustive Helm values reference covering all 40 parameters across 7 sections with conditional behavior documentation
