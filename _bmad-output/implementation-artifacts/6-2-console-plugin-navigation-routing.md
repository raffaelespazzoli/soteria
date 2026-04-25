# Story 6.2: Console Plugin Navigation & Routing

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a platform engineer,
I want a "Disaster Recovery" entry in the OCP Console left navigation with URL-based routing to Dashboard, Plan Detail, and Execution Detail views,
So that DR management is a native part of my Console experience.

## Acceptance Criteria

1. **AC1 — Navigation item:** A "Disaster Recovery" navigation item appears in the Console's left navigation sidebar when the plugin loads. Clicking it navigates to the DR Dashboard as the default landing page. (UX-DR14)

2. **AC2 — URL-based routing:** The following routes are defined and render the correct page component:
   - `/disaster-recovery` → DR Dashboard
   - `/disaster-recovery/plans/:name` → Plan Detail
   - `/disaster-recovery/executions/:name` → Execution Detail

   Browser back/forward navigation works correctly at every level.

3. **AC3 — Breadcrumbs:** Plan Detail and Execution Detail pages render a PatternFly `Breadcrumb` component showing the navigation path (e.g., `Disaster Recovery > erp-full-stack > Overview`). Each breadcrumb segment is a clickable link.

4. **AC4 — Dashboard state preservation:** When a user navigates from the Dashboard to Plan Detail and then returns via breadcrumb or browser back, table scroll position and active filters are preserved. (UX-DR14)

## Tasks / Subtasks

- [ ] Task 1: Update `console-extensions.json` with navigation and route declarations (AC: #1, #2)
  - [ ] 1.1 Add a `console.navigation/section` entry with `id: "disaster-recovery"`, `name: "Disaster Recovery"`, `perspective: "admin"`
  - [ ] 1.2 Add a `console.navigation/href` entry linking to `/disaster-recovery` within the `disaster-recovery` section, with `startsWith: ["/disaster-recovery"]`
  - [ ] 1.3 Add `console.page/route` entries for all three routes (`/disaster-recovery`, `/disaster-recovery/plans/:name`, `/disaster-recovery/executions/:name`) with `exact: true` and `$codeRef` pointing to lazy-loaded page components. Sort routes most-specific-first in the JSON array.

- [ ] Task 2: Create page-level components (AC: #2, #3)
  - [ ] 2.1 Create `src/components/DRDashboard/DRDashboardPage.tsx` — wraps a placeholder in a PatternFly `Page > PageSection`. Import and render a `<DRDashboard />` placeholder. This is the landing page for `/disaster-recovery`.
  - [ ] 2.2 Create `src/components/DRPlanDetail/DRPlanDetailPage.tsx` — reads `:name` from the URL using `react-router`'s `useParams()`, renders breadcrumb + placeholder plan detail content.
  - [ ] 2.3 Create `src/components/ExecutionDetail/ExecutionDetailPage.tsx` — reads `:name` from the URL using `react-router`'s `useParams()`, renders breadcrumb + placeholder execution detail content.
  - [ ] 2.4 Ensure each page component is exported as a default export (required by `$codeRef` in `console-extensions.json`).

- [ ] Task 3: Implement Breadcrumb component (AC: #3)
  - [ ] 3.1 Create `src/components/shared/DRBreadcrumb.tsx` — a reusable breadcrumb component using PatternFly `Breadcrumb` + `BreadcrumbItem`. Accepts props for the current page context (plan name, execution name).
  - [ ] 3.2 Wire breadcrumb links using `react-router`'s `Link` component (or Console's `Link` equivalent) — all segments clickable.
  - [ ] 3.3 Render breadcrumb in `DRPlanDetailPage` and `ExecutionDetailPage` only — the Dashboard is the top level and has no breadcrumb.

- [ ] Task 4: Dashboard state preservation (AC: #4)
  - [ ] 4.1 Create `src/hooks/useDashboardState.ts` — a React context or module-level state holder that stores current filter values and scroll position.
  - [ ] 4.2 In `DRDashboardPage`, save scroll position and filter state on unmount; restore on mount.
  - [ ] 4.3 Verify back/forward navigation preserves state (the Console's routing + React component lifecycle should handle this if state is held outside the component tree).

- [ ] Task 5: Tests (AC: #1, #2, #3, #4)
  - [ ] 5.1 Create `tests/components/DRDashboardPage.test.tsx` — renders without crash, contains expected placeholder content.
  - [ ] 5.2 Create `tests/components/DRPlanDetailPage.test.tsx` — renders breadcrumb, displays plan name from URL params.
  - [ ] 5.3 Create `tests/components/ExecutionDetailPage.test.tsx` — renders breadcrumb, displays execution name from URL params.
  - [ ] 5.4 Create `tests/components/DRBreadcrumb.test.tsx` — renders correct links, all segments clickable.
  - [ ] 5.5 Run `jest-axe` audit on each page component to verify accessibility baseline.
  - [ ] 5.6 Verify `yarn build` succeeds with the new routes and components.

## Dev Notes

### Dependency on Story 6.1

This story assumes Story 6.1 (Console Plugin Project Initialization) is complete. The following must exist before starting:
- `console-plugin/package.json` with Console SDK, React, PatternFly 5, and TypeScript dependencies
- `console-plugin/console-extensions.json` with at least one placeholder extension
- `console-plugin/webpack.config.ts` with `ConsoleRemotePlugin` for module federation
- `console-plugin/tsconfig.json` with strict mode
- `src/components/DRDashboard/DRDashboard.tsx` placeholder component
- `src/hooks/useDRResources.ts` with `useK8sWatchResource` wrappers
- `src/models/types.ts` with CRD TypeScript interfaces
- Jest + React Testing Library + jest-axe configured

### Console Extension Points — Exact Patterns

The OCP Console plugin SDK defines extension points in `console-extensions.json`. Here is the exact pattern for navigation and routing (verified against the SDK docs for Console 4.19+ with React Router v7):

**Navigation section:**
```json
{
  "type": "console.navigation/section",
  "properties": {
    "id": "disaster-recovery",
    "perspective": "admin",
    "name": "Disaster Recovery"
  }
}
```

**Navigation link (href):**
```json
{
  "type": "console.navigation/href",
  "properties": {
    "id": "dr-dashboard",
    "name": "Dashboard",
    "href": "/disaster-recovery",
    "section": "disaster-recovery",
    "perspective": "admin",
    "startsWith": ["/disaster-recovery"]
  }
}
```

**Page routes (sort most-specific first):**
```json
{
  "type": "console.page/route",
  "properties": {
    "path": "/disaster-recovery/executions/:name",
    "exact": true,
    "component": { "$codeRef": "ExecutionDetailPage" },
    "perspective": "admin"
  }
},
{
  "type": "console.page/route",
  "properties": {
    "path": "/disaster-recovery/plans/:name",
    "exact": true,
    "component": { "$codeRef": "DRPlanDetailPage" },
    "perspective": "admin"
  }
},
{
  "type": "console.page/route",
  "properties": {
    "path": "/disaster-recovery",
    "exact": true,
    "component": { "$codeRef": "DRDashboardPage" },
    "perspective": "admin"
  }
}
```

The `$codeRef` values must match `exposedModules` keys in `package.json`'s `consolePlugin` field:
```json
{
  "consolePlugin": {
    "name": "soteria-console-plugin",
    "version": "0.0.1",
    "displayName": "Soteria DR Management",
    "exposedModules": {
      "DRDashboardPage": "./src/components/DRDashboard/DRDashboardPage",
      "DRPlanDetailPage": "./src/components/DRPlanDetail/DRPlanDetailPage",
      "ExecutionDetailPage": "./src/components/ExecutionDetail/ExecutionDetailPage"
    }
  }
}
```

### React Router v7 — Critical Change

The OCP Console upgraded to React Router v7 (CONSOLE-4439, targeting Console 4.22+). Key implications:
- `react-router-dom` and `react-router` are consolidated into a single `react-router` package
- Remove `react-router-dom-v5-compat` if present from template
- Import `useParams`, `useNavigate`, `Link`, `useLocation` from `react-router` (NOT from `react-router-dom`)
- Paths are matched as `exact: true` by default in RR v7, but the Console retains backward compatibility — explicitly set `exact: true` in extensions for clarity
- `path-to-regexp` is no longer used; URL params use `:name` syntax directly

If the console-plugin-template still ships with `react-router-dom-v5-compat`, update to `react-router` v7.

### Page Component Pattern

Each page component must be a **default export** for `$codeRef` to resolve correctly via webpack module federation. Follow this pattern:

```typescript
import * as React from 'react';
import { Page, PageSection, Title } from '@patternfly/react-core';

const DRDashboardPage: React.FC = () => {
  return (
    <Page>
      <PageSection variant="light">
        <Title headingLevel="h1">Disaster Recovery</Title>
      </PageSection>
      <PageSection>
        {/* DRDashboard component from Story 6.3 */}
        <p>Dashboard placeholder — Story 6.3 builds the table here.</p>
      </PageSection>
    </Page>
  );
};

export default DRDashboardPage;
```

### Breadcrumb Pattern

Use PatternFly `Breadcrumb` + `BreadcrumbItem` with `react-router` `Link` for navigation:

```typescript
import * as React from 'react';
import { Breadcrumb, BreadcrumbItem } from '@patternfly/react-core';
import { Link } from 'react-router';

interface DRBreadcrumbProps {
  planName?: string;
  executionName?: string;
}

const DRBreadcrumb: React.FC<DRBreadcrumbProps> = ({ planName, executionName }) => (
  <Breadcrumb>
    <BreadcrumbItem>
      <Link to="/disaster-recovery">Disaster Recovery</Link>
    </BreadcrumbItem>
    {planName && (
      <BreadcrumbItem>
        <Link to={`/disaster-recovery/plans/${planName}`}>{planName}</Link>
      </BreadcrumbItem>
    )}
    {executionName && (
      <BreadcrumbItem isActive>{executionName}</BreadcrumbItem>
    )}
  </Breadcrumb>
);
```

### Dashboard State Preservation Strategy

AC4 requires preserving table scroll position and filters when navigating away from the dashboard and returning. Options:

**Recommended approach — Module-level state:**
Store filter/scroll state in a module-scoped variable (outside the React component tree) that survives component unmount/remount cycles during SPA navigation. This avoids Context providers and is the simplest approach that works with the Console's routing model.

```typescript
// src/hooks/useDashboardState.ts
interface DashboardState {
  scrollTop: number;
  filters: Record<string, string[]>;
  searchText: string;
}

let savedState: DashboardState | null = null;

export const saveDashboardState = (state: DashboardState) => { savedState = state; };
export const restoreDashboardState = (): DashboardState | null => savedState;
```

The Dashboard page calls `saveDashboardState` on unmount (`useEffect` cleanup) and `restoreDashboardState` on mount. URL query parameters should also reflect active filters (for shareable URLs, per UX-DR15), but that's Story 6.3's concern.

### File Structure After This Story

```
console-plugin/src/
├── components/
│   ├── DRDashboard/
│   │   ├── DRDashboard.tsx            # (from 6.1) placeholder component
│   │   └── DRDashboardPage.tsx        # NEW — page wrapper with PageSection
│   ├── DRPlanDetail/
│   │   └── DRPlanDetailPage.tsx       # NEW — reads :name, renders breadcrumb + placeholder
│   ├── ExecutionDetail/
│   │   └── ExecutionDetailPage.tsx    # NEW — reads :name, renders breadcrumb + placeholder
│   └── shared/
│       └── DRBreadcrumb.tsx           # NEW — reusable breadcrumb
├── hooks/
│   ├── useDRResources.ts             # (from 6.1)
│   └── useDashboardState.ts          # NEW — module-level state persistence
├── models/
│   └── types.ts                      # (from 6.1)
└── utils/                            # (from 6.1)
```

**New test files:**
```
console-plugin/tests/components/
├── DRDashboardPage.test.tsx           # NEW
├── DRPlanDetailPage.test.tsx          # NEW
├── ExecutionDetailPage.test.tsx       # NEW
└── DRBreadcrumb.test.tsx              # NEW
```

### Non-Negotiable Constraints

From architecture and project context:
- **PatternFly 5 ONLY** — `Breadcrumb`, `BreadcrumbItem`, `Page`, `PageSection`, `Title` from `@patternfly/react-core`. No other UI libraries.
- **CSS custom properties only** — `--pf-v5-global--*` tokens. No hardcoded colors/spacing.
- **Console SDK hooks only** — `useK8sWatchResource()` for data. No direct API calls.
- **No external state libraries** — no Redux, Zustand, or MobX. Module-level state or Console SDK hooks only.
- **Imports from `react-router`** — NOT `react-router-dom` (React Router v7 consolidation).
- **Default exports for page components** — required by `$codeRef` / webpack module federation.

### What NOT to Do

- **Do NOT build actual Dashboard table content** — Story 6.3 handles the table, toolbar, sorting, and filtering. This story only creates the page wrapper and routing.
- **Do NOT build Plan Detail tabs** — Story 6.5 builds Overview, Waves, History, Configuration tabs. This story only creates the page shell with breadcrumb and a placeholder body.
- **Do NOT build Execution Monitor** — Story 7.2 handles the live execution monitor. This story only creates the page shell.
- **Do NOT implement URL-based filter persistence** — Story 6.3 handles filter-to-URL synchronization. This story provides the module-level state hook as infrastructure.
- **Do NOT install additional routing libraries** — `react-router` (v7) is already provided by the Console. Do not add `@reach/router` or any other router.
- **Do NOT modify Go code** — this story is pure TypeScript/React.

### Testing Approach

- **Component rendering tests** using React Testing Library — verify each page renders without errors, displays expected placeholder content, and includes breadcrumbs where expected.
- **URL parameter extraction** — mock `useParams` from `react-router` to test that plan/execution name is extracted and displayed.
- **Accessibility** — run `jest-axe` (via `toHaveNoViolations` matcher) on every page component.
- **Build verification** — `yarn build` must succeed with all new routes and `exposedModules` entries.

Mock `react-router` hooks in tests:
```typescript
jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useParams: () => ({ name: 'erp-full-stack' }),
  Link: ({ to, children }: any) => <a href={to}>{children}</a>,
}));
```

### Project Structure Notes

- All new files go under `console-plugin/src/` — fully independent from Go project
- Page components in separate directories matching the architecture doc's `DRDashboard/`, `DRPlanDetail/`, `ExecutionDetail/` pattern
- Shared components in `src/components/shared/` — reusable across pages
- Hooks in `src/hooks/` — consistent with Story 6.1's pattern

### References

- [Source: _bmad-output/planning-artifacts/architecture.md § Frontend Architecture] — Console SDK hooks, PatternFly 5, webpack module federation
- [Source: _bmad-output/planning-artifacts/architecture.md § Project Directory Structure] — `console-plugin/` file structure with `DRDashboard/`, `DRPlanDetail/`, `ExecutionMonitor/` directories
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § Navigation Patterns] — Information architecture: Console left nav → DR Dashboard → Plan Detail → Execution Detail
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md § UX-DR14] — Navigation structure with breadcrumbs and URL-based routing with preserved table scroll/filter state
- [Source: _bmad-output/planning-artifacts/epics.md § Story 6.2] — Acceptance criteria and BDD scenarios
- [Source: _bmad-output/project-context.md § TypeScript rules] — Console plugin coding rules, PatternFly-only, Console SDK hooks only
- [Source: github.com/openshift/console — console-extensions.md] — Extension type specifications for `console.navigation/section`, `console.navigation/href`, `console.page/route`
- [Source: github.com/openshift/console — CONSOLE-4439] — React Router v7 upgrade: consolidated `react-router` package, import changes
- [Source: _bmad-output/implementation-artifacts/6-1-console-plugin-project-initialization.md] — Previous story context, template source, PatternFly constraints, CRD interfaces

### Previous Story Intelligence

Story 6.1 (Console Plugin Project Initialization) established:
- The `console-plugin/` directory scaffolded from `openshift/console-plugin-template`
- `package.json` with `consolePlugin.name: "soteria-console-plugin"` and `exposedModules`
- `console-extensions.json` with a placeholder `console.navigation/section` and `console.page/route`
- `src/models/types.ts` with DRPlan, DRExecution, DRGroupStatus TypeScript interfaces
- `src/hooks/useDRResources.ts` with `useK8sWatchResource` wrappers
- `src/components/DRDashboard/DRDashboard.tsx` placeholder component
- Jest + RTL + jest-axe test harness
- `@openshift-console/dynamic-plugin-sdk` v4.21.0 (or template-pinned version)

This story **extends** the 6.1 scaffold — it replaces the placeholder navigation/route entries with the full routing structure and adds page-level components for all three views.

### Git Intelligence

Recent commits (last 5):
- `8f18908` — Fix retry robustness and update docs with Epic 5 learnings
- `c7916df` — Mark Story 5.7 as done in sprint status
- `d494cef` — Fix ScyllaDB write contention in DRExecution reconciler
- `f127e6f` — Implement Story 5.7: driver interface simplification & workflow symmetry
- `15e0ab8` — Add Soteria overview presentation

All recent work is Go backend. Epic 6 is the first TypeScript/React work. No console-plugin code exists yet beyond a README placeholder.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
