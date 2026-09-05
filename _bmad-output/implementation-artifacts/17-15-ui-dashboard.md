# Story 17.15: UI Guide: Dashboard

Status: done

## Story

As a DR operator using the Soteria console,
I want a guide for the dashboard view with annotated screenshots,
so that I can understand the information displayed and take action from the dashboard.

## Acceptance Criteria

**AC1: Dashboard overview is written**
Given the dashboard guide
When a reader opens the page
Then they find a description of the dashboard's purpose and layout

**AC2: DR plan cards are explained**
Given the dashboard guide
When a reader reviews the plan cards section
Then they understand what each card shows and what the status indicators mean (SteadyState, FailedOver, etc.)

**AC3: Replication health badges are explained**
Given the dashboard guide
When a reader sees replication health badges
Then they understand the health states and what each badge color means

**AC4: Alert banners are documented**
Given the dashboard guide
When a reader reviews the alerts section
Then they understand what conditions trigger alert banners

**AC5: Cross-cluster awareness is documented**
Given the dashboard guide
When a reader reviews the cross-cluster section
Then they understand how the dashboard shows awareness of both clusters

**AC6: Screenshots are planned (or included)**
Given the dashboard guide
When screenshots are available
Then annotated screenshots illustrate each major UI element

## Tasks / Subtasks

- [ ] Task 1: Research dashboard components (AC: 1, 2, 3, 4, 5)
  - [ ] 1.1: Read UX spec Dashboard Table Design and Journey 1
  - [ ] 1.2: Walk `console-plugin/src/components/DRDashboard/DRDashboardPage.tsx` for page structure
  - [ ] 1.3: Walk `console-plugin/src/components/DRDashboard/DRDashboard.tsx` for main dashboard component
  - [ ] 1.4: Walk `console-plugin/src/components/DRDashboard/AlertBannerSystem.tsx` for alert banner logic
  - [ ] 1.5: Walk `console-plugin/src/components/DRDashboard/DRPlanActions.tsx` for action buttons
  - [ ] 1.6: Walk `console-plugin/src/components/shared/ReplicationHealthIndicator.tsx` for health badges
- [ ] Task 2: Write the documentation page (AC: 1, 2, 3, 4, 5, 6)
  - [ ] 2.1: Write `docs/usage/ui-guide/dashboard.md` covering dashboard overview, plan cards, status indicators, replication health badges, alert banners, cross-cluster awareness
  - [ ] 2.2: Add placeholder image references for annotated screenshots
- [ ] Task 3: Verify component coverage (AC: 1, 2, 3, 4, 5)
  - [ ] 3.1: Verify described UI elements exist in actual component code

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/ux-design-specification.md — Dashboard Table Design, Journey 1 ("First Green Dashboard"), 5-second status check design goal]
- [Source: _bmad-output/planning-artifacts/prd.md — NFR6 (dashboard query < 2s)]

### Code to Verify Against

- [Source: console-plugin/src/components/DRDashboard/DRDashboardPage.tsx — main dashboard page]
- [Source: console-plugin/src/components/DRDashboard/DRDashboard.tsx — dashboard content component]
- [Source: console-plugin/src/components/DRDashboard/DRDashboardToolbar.tsx — toolbar/filters]
- [Source: console-plugin/src/components/DRDashboard/AlertBannerSystem.tsx — alert banner display logic]
- [Source: console-plugin/src/components/DRDashboard/DashboardEmptyState.tsx — empty state handling]
- [Source: console-plugin/src/components/DRDashboard/DRPlanActions.tsx — plan action buttons]
- [Source: console-plugin/src/components/shared/ReplicationHealthIndicator.tsx — health badges: Healthy=green, Degraded=yellow, Error=red, Unknown=gray]
- [Source: console-plugin/src/components/shared/PhaseBadge.tsx — phase status badges]

### Implementation Pattern

- Write prose and structure first; insert screenshots when available
- Use placeholder image references: `![Dashboard overview](../images/dashboard-overview.png)`
- Document color semantics from UX spec: SteadyState=green, FailedOver=blue, Degraded=yellow, Failed=red

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/usage/ui-guide/dashboard.md | NEW | Dashboard overview, plan cards, health badges, alert banners |

### Key Constraints

- **Blocked on:** Raffa provides access to running Soteria UI instance for screenshots
- Write prose and structure first; insert annotated screenshots when available
- Dashboard must answer "Am I protected?" in 5 seconds (UX design goal)

### Project Structure Notes

- Console plugin components: `console-plugin/src/components/DRDashboard/`
- Shared components: `console-plugin/src/components/shared/`

### References

- [Source: console-plugin/src/components/DRDashboard/ — all dashboard components]
- [Source: console-plugin/src/components/shared/ReplicationHealthIndicator.tsx — health badge]
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md — Dashboard design]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
