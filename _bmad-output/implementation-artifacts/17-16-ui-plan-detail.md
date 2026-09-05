# Story 17.16: UI Guide: Plan Detail

Status: backlog

## Story

As a DR operator using the Soteria console,
I want a guide for the plan detail view with annotated screenshots,
so that I can understand plan composition, review history, and take actions from this view.

## Acceptance Criteria

**AC1: Plan detail view layout is documented**
Given the plan detail guide
When a reader opens the page
Then they find a description of the plan detail view's layout and navigation

**AC2: Tabs are documented**
Given the plan detail guide
When a reader reviews the tabs section
Then they understand the purpose of each tab: overview, waves, history, configuration

**AC3: Wave composition tree is explained**
Given the plan detail guide
When a reader reviews the waves tab description
Then they understand the wave composition tree and VM membership list

**AC4: Action buttons are documented**
Given the plan detail guide
When a reader reviews the actions section
Then they understand the context-aware action buttons and when each is available

**AC5: Execution history is documented**
Given the plan detail guide
When a reader reviews the history section
Then they understand the execution history table and how to navigate to execution details

**AC6: Screenshots are planned (or included)**
Given the plan detail guide
When screenshots are available
Then annotated screenshots illustrate each tab and major UI element

## Tasks / Subtasks

- [ ] Task 1: Research plan detail components (AC: 1, 2, 3, 4, 5)
  - [ ] 1.1: Read UX spec Plan Detail View and Journeys 2–4
  - [ ] 1.2: Walk `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` for page layout and tab structure
  - [ ] 1.3: Walk `console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx` for wave tree component
  - [ ] 1.4: Walk `console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx` for history table
  - [ ] 1.5: Walk `console-plugin/src/components/DRPlanDetail/PlanHeader.tsx` for header and action buttons
  - [ ] 1.6: Walk `console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx` for configuration tab
  - [ ] 1.7: Walk `console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx` for lifecycle state visualization
- [ ] Task 2: Write the documentation page (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 2.1: Write `docs/usage/ui-guide/plan-detail.md` covering view layout, tabs, wave tree, action buttons, history table
  - [ ] 2.2: Add placeholder image references for annotated screenshots
- [ ] Task 3: Verify component coverage (AC: 1, 2, 3, 4, 5)
  - [ ] 3.1: Verify described UI elements exist in actual component code

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/ux-design-specification.md — Plan Detail View, Journeys 2–4, context-aware state actions design principle]
- [Source: _bmad-output/planning-artifacts/prd.md — FR8 (pre-flight check visibility)]

### Code to Verify Against

- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx — plan detail page layout and tab routing]
- [Source: console-plugin/src/components/DRPlanDetail/WaveCompositionTree.tsx — wave → DRGroup → VM tree display]
- [Source: console-plugin/src/components/DRPlanDetail/ExecutionHistoryTable.tsx — execution history with result badges]
- [Source: console-plugin/src/components/DRPlanDetail/PlanHeader.tsx — plan header with context-aware action buttons]
- [Source: console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx — plan configuration display]
- [Source: console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx — 8-phase lifecycle state machine diagram with highlighted current phase]
- [Source: console-plugin/src/components/DRPlanDetail/PreflightConfirmationModal.tsx — pre-flight confirmation dialog]
- [Source: console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx — in-progress transition banner]
- [Source: console-plugin/src/components/DRPlanDetail/ReplicationHealthExpanded.tsx — per-VG replication health detail]
- [Source: console-plugin/src/components/DRPlanDetail/SiteDiscoverySection.tsx — cross-site VM discovery comparison]
- [Source: console-plugin/src/components/DRPlanDetail/SiteDisagreementAlert.tsx — site discovery mismatch alert]
- [Source: console-plugin/src/components/DRPlanDetail/DiskDisagreementAlert.tsx — disk topology mismatch alert]
- [Source: console-plugin/src/components/shared/ExecutionResultBadge.tsx — execution result badge]

### Implementation Pattern

- Document context-aware actions: only valid state transitions appear as buttons (UX principle #4)
- Explain 8-phase lifecycle diagram: SteadyState → FailingOver → FailedOver → Reprotecting → DRedSteadyState → FailingBack → FailedBack → ReprotectingBack → SteadyState
- Document site discovery section: primary vs secondary VM lists, disagreement alerts
- Use placeholder screenshots with alt text describing expected content

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/usage/ui-guide/plan-detail.md | NEW | Plan detail view: tabs, wave tree, lifecycle diagram, actions, history |

### Key Constraints

- **Blocked on:** Raffa provides access to running Soteria UI instance for screenshots
- Write prose and structure first; insert annotated screenshots when available
- Action buttons are context-aware based on DRPlan.Status.Phase and state machine transitions

### Project Structure Notes

- Plan detail components: `console-plugin/src/components/DRPlanDetail/`
- 13 component files in this directory covering all plan detail functionality

### References

- [Source: console-plugin/src/components/DRPlanDetail/ — all plan detail components]
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md — Plan Detail design]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
