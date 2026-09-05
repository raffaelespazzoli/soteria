# Story 17.17: UI Guide: Execution Monitor

Status: done

## Story

As a DR operator monitoring a live failover,
I want a guide for the execution monitor view with annotated screenshots,
so that I can track progress, identify failures, and take corrective action during execution.

## Acceptance Criteria

**AC1: Execution monitor layout is documented**
Given the execution monitor guide
When a reader opens the page
Then they find a description of the live execution view layout

**AC2: Gantt chart is explained**
Given the guide
When a reader reviews the Gantt chart section
Then they understand the live execution Gantt chart showing wave and DRGroup progress over time

**AC3: Wave progress bars are documented**
Given the guide
When a reader reviews wave progress
Then they understand how wave progress bars show completion percentage and status

**AC4: DRGroup status cards are documented**
Given the guide
When a reader reviews DRGroup status
Then they understand the DRGroup status cards including elapsed/remaining time

**AC5: Error display and retry are documented**
Given the guide
When a reader sees inline error display
Then they understand how errors are shown and how to trigger retry actions

**AC6: Partial success visualization is documented**
Given the guide
When an execution completes with partial success
Then the reader understands how partial success is visualized in the execution monitor

## Tasks / Subtasks

- [ ] Task 1: Research execution monitor components (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 1.1: Read UX spec ExecutionGanttChart component section and execution visualization patterns
  - [ ] 1.2: Walk `console-plugin/src/components/ExecutionDetail/ExecutionDetailPage.tsx` for page structure
  - [ ] 1.3: Walk `console-plugin/src/components/ExecutionDetail/WaveProgressStep.tsx` for wave progress rendering
  - [ ] 1.4: Walk `console-plugin/src/components/ExecutionDetail/ExecutionSummary.tsx` for summary panel
  - [ ] 1.5: Walk `console-plugin/src/components/ExecutionDetail/FailedGroupDetail.tsx` for error display and retry
  - [ ] 1.6: Walk `console-plugin/src/components/ExecutionDetail/ExecutionHeader.tsx` for header and status
  - [ ] 1.7: Walk `console-plugin/src/components/ExecutionDetail/SiteCoordinationPanel.tsx` for multi-site coordination
- [ ] Task 2: Write the documentation page (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 2.1: Write `docs/usage/ui-guide/execution-monitor.md` covering Gantt chart, wave progress, DRGroup cards, error/retry, partial success
  - [ ] 2.2: Add placeholder image references for annotated screenshots
- [ ] Task 3: Verify component coverage (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 3.1: Verify described UI elements exist in actual component code

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/ux-design-specification.md — ExecutionGanttChart component, execution visualization patterns, "relief through progress" design principle]
- [Source: _bmad-output/planning-artifacts/prd.md — NFR7 (execution updates visible < 5s)]

### Code to Verify Against

- [Source: console-plugin/src/components/ExecutionDetail/ExecutionDetailPage.tsx — execution detail page layout and data fetching]
- [Source: console-plugin/src/components/ExecutionDetail/WaveProgressStep.tsx — wave-by-wave progress rendering with DRGroup blocks]
- [Source: console-plugin/src/components/ExecutionDetail/ExecutionSummary.tsx — summary panel with overall result, duration, VM count]
- [Source: console-plugin/src/components/ExecutionDetail/FailedGroupDetail.tsx — error display with step-level detail and retry action]
- [Source: console-plugin/src/components/ExecutionDetail/ExecutionHeader.tsx — execution header with phase and mode badges]
- [Source: console-plugin/src/components/ExecutionDetail/SiteCoordinationPanel.tsx — multi-site Step 0 coordination status]
- [Source: console-plugin/src/components/shared/ExecutionResultBadge.tsx — Succeeded=green, PartiallySucceeded=yellow, Failed=red badges]

### Implementation Pattern

- Document the color language from UX spec: Succeeded=green, PartiallySucceeded=yellow, Failed=red, InProgress=blue, Pending=gray
- Explain the Gantt-style visualization: waves as rows, DRGroups as blocks within rows, real-time progress fill
- Document retry UX: inline "Retry" button on failed groups; `soteria.io/retry-groups` annotation applied via UI
- Explain bridge-call readability: summary is designed to be shareable during incident calls
- Document multi-site coordination panel: source site Step 0, target site promotion status

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/usage/ui-guide/execution-monitor.md | NEW | Live Gantt chart, wave progress, DRGroup status, error/retry, partial success |

### Key Constraints

- **Blocked on:** Raffa provides access to running Soteria UI instance for screenshots
- Write prose and structure first; insert annotated screenshots when available
- Updates must be visible within 5 seconds of state change (NFR7)

### Project Structure Notes

- Execution detail components: `console-plugin/src/components/ExecutionDetail/`
- 6 component files covering execution monitoring functionality

### References

- [Source: console-plugin/src/components/ExecutionDetail/ — all execution monitor components]
- [Source: console-plugin/src/components/shared/ExecutionResultBadge.tsx — result badge]
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md — ExecutionGanttChart, execution visualization]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
