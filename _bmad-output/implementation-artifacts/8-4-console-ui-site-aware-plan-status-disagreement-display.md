# Story 8.4: Console UI — Site-Aware Plan Status & Disagreement Display

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the Console to show per-site VM inventory and clearly indicate when sites disagree,
So that I can identify and resolve VM provisioning gaps before attempting a DR operation.

## Acceptance Criteria

1. **AC1 — Site Discovery section on Configuration tab:** When both `primarySiteDiscovery` and `secondarySiteDiscovery` are populated, a "Site Discovery" section appears on the Configuration tab with two columns: primary site VMs and secondary site VMs. Each column shows site name, VM count, and last discovery timestamp. Matching VMs render in default style; VMs present on only one site are highlighted with a warning indicator.

2. **AC2 — Danger alert when SitesInSync=False:** When the `SitesInSync` condition is `False`, a PatternFly `Alert` (variant=danger, inline) appears prominently above the lifecycle diagram on the Overview tab. Title: "Sites do not agree on VM inventory — DR operations are blocked". The alert includes an `AlertActionLink` that switches to the Configuration tab and scrolls to the site discovery section. The alert body summarizes the delta: e.g., "2 VMs on primary not found on secondary".

3. **AC3 — Lifecycle diagram action buttons disabled when blocked:** When `SitesInSync` condition is `False`, all transition action buttons in the `DRLifecycleDiagram` are disabled. Each disabled button shows a tooltip: "Blocked: sites do not agree on VM inventory".

4. **AC4 — Dashboard table warning indicator:** When a plan has `SitesInSync: False`, the plan row in the DR Dashboard table shows a warning icon in the status/health column (alongside or replacing the `ReplicationHealthIndicator`). The kebab menu actions are disabled with a tooltip indicating the plan is blocked.

5. **AC5 — Live watch resolution:** When `SitesInSync` transitions from `False` to `True` via a watch update, the danger alert disappears, action buttons become enabled, and the site discovery section shows all VMs as matching.

6. **AC6 — Stale discovery warning:** When `lastDiscoveryTime` for either site is older than 5 minutes from now, a subtle warning shows beneath the site column: "Discovery data from <site> is stale (last updated <relative time>)".

7. **AC7 — Accessibility:** The danger alert uses ARIA live region to announce blocking state changes. The per-site VM comparison table is keyboard navigable. Warning indicators include screen reader text explaining the mismatch. jest-axe passes on all new component states.

## Tasks / Subtasks

- [x] Task 1: Extend TypeScript types for SiteDiscovery and PreflightReport (AC: #1, #2)
  - [x] 1.1 In `console-plugin/src/models/types.ts`, add `SiteDiscovery` interface:
    ```ts
    export interface SiteDiscovery {
      vms?: DiscoveredVM[];
      discoveredVMCount?: number;
      lastDiscoveryTime?: string;
    }
    ```
  - [x] 1.2 Add `primarySiteDiscovery` and `secondarySiteDiscovery` fields (both `SiteDiscovery | undefined`) to the `DRPlanStatus` interface
  - [x] 1.3 Add `sitesInSync` and `siteDiscoveryDelta` fields to `PreflightReport` (from Story 8.3's backend enrichment)

- [x] Task 2: Add `getSitesInSync` helper to drPlanUtils (AC: #2, #3, #4)
  - [x] 2.1 In `console-plugin/src/utils/drPlanUtils.ts`, add:
    ```ts
    export interface SitesInSyncStatus {
      inSync: boolean;
      reason?: string;   // VMsAgreed | VMsMismatch | WaitingForDiscovery
      message?: string;  // delta description from condition
    }
    export function getSitesInSync(plan: DRPlan): SitesInSyncStatus
    ```
    Implementation: find `SitesInSync` condition in `plan.status.conditions`; if not found return `{ inSync: true }` (backward compat — plans without the condition are not blocked); if `status === 'True'` return `inSync: true`; otherwise return `{ inSync: false, reason, message }`
  - [x] 2.2 Add `parseSiteDiscoveryDelta(message: string): { primaryOnly: string[]; secondaryOnly: string[] }` pure function that parses the structured delta message from the `SitesInSync` condition (format: `"VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]"`)

- [x] Task 3: Create `SiteDiscoverySection` component (AC: #1, #6)
  - [x] 3.1 Create `console-plugin/src/components/DRPlanDetail/SiteDiscoverySection.tsx`
  - [x] 3.2 Props: `plan: DRPlan`
  - [x] 3.3 Render two-column layout (CSS Grid `1fr 1fr`) with:
    - Column header: site name (from `plan.spec.primarySite` / `plan.spec.secondarySite`)
    - VM count badge: `"N VMs discovered"`
    - Last discovery timestamp (via `formatRelativeTime` from `utils/formatters.ts`)
    - VM list: `Table` (compact) with `Name` and `Namespace` columns
    - Matching VMs: default style
    - VMs present on only one site: row highlighted with `--pf-t--global--color--status--warning--default` background and `ExclamationTriangleIcon` in an extra "Status" column cell
  - [x] 3.4 When either `SiteDiscovery` is nil/undefined, show informational text: "Waiting for <site> to report discovery data"
  - [x] 3.5 When both are nil, show: "Site discovery not yet available. Ensure both Soteria instances are running with --site-name."
  - [x] 3.6 Staleness check: if `lastDiscoveryTime` is > 5 minutes old, show `Alert` (variant=warning, isInline, isPlain) beneath that column: "Discovery data from <site> is stale (last updated <relative time>)"
  - [x] 3.7 Add `id="site-discovery-section"` on the root element for scroll-to anchor from the danger alert
  - [x] 3.8 Use PatternFly `Content` (h3) header: "Site Discovery"

- [x] Task 4: Create `SiteDisagreementAlert` component (AC: #2, #5)
  - [x] 4.1 Create `console-plugin/src/components/DRPlanDetail/SiteDisagreementAlert.tsx`
  - [x] 4.2 Props: `plan: DRPlan`, `onSwitchToConfig: () => void`
  - [x] 4.3 Render `Alert` (variant=danger, isInline) with title: "Sites do not agree on VM inventory — DR operations are blocked"
  - [x] 4.4 Alert body summarizes the delta from condition message. Parse the `SitesInSync` condition message to extract counts: "N VMs on primary not found on secondary, M VMs on secondary not found on primary"
  - [x] 4.5 `AlertActionLink` with text "View site differences" that calls `onSwitchToConfig()` — parent handler switches to Configuration tab (eventKey=3) and scrolls to `#site-discovery-section`
  - [x] 4.6 Wrap in `div` with `aria-live="assertive"` so screen readers announce when the blocking alert appears/disappears

- [x] Task 5: Integrate SiteDisagreementAlert into DRPlanDetailPage (AC: #2, #5)
  - [x] 5.1 In `DRPlanDetailPage.tsx`, import `SiteDisagreementAlert` and `getSitesInSync`
  - [x] 5.2 Compute `sitesInSync = getSitesInSync(plan)` below `effectivePhase`
  - [x] 5.3 If `!sitesInSync.inSync`, render `<SiteDisagreementAlert>` in the Overview tab, above `PlanHeader` (or between `PlanHeader` and `TransitionProgressBanner`)
  - [x] 5.4 The `onSwitchToConfig` handler: `setActiveTab(3)` then `setTimeout(() => document.getElementById('site-discovery-section')?.scrollIntoView({ behavior: 'smooth' }), 100)`
  - [x] 5.5 When `SitesInSync` transitions True→False→True via watch, the alert appears/disappears reactively (no extra logic needed — `useDRPlan` watch provides updated conditions)

- [x] Task 6: Integrate SiteDiscoverySection into PlanConfiguration (AC: #1)
  - [x] 6.1 In `PlanConfiguration.tsx`, import `SiteDiscoverySection`
  - [x] 6.2 Add `SiteDiscoverySection` as a full-width row ABOVE the existing two-column grid (Plan Information + Replication Health). Use a wrapper `div` so Site Discovery spans the full width, then the existing two-pane layout sits below it
  - [x] 6.3 Only render `SiteDiscoverySection` when site-aware mode is active: check `plan.spec?.primarySite && plan.spec?.secondarySite` — if neither is set, skip the section entirely (backward compat for plans without site topology)

- [x] Task 7: Disable lifecycle diagram actions when blocked (AC: #3)
  - [x] 7.1 In `DRLifecycleDiagram.tsx`, add a `isBlocked?: boolean` prop and `blockedTooltip?: string` prop
  - [x] 7.2 When `isBlocked` is true, all `TransitionEdge` buttons render as `isDisabled` with a `Tooltip` wrapping: "Blocked: sites do not agree on VM inventory"
  - [x] 7.3 In `TransitionEdge`, when in `available` state but parent passes `isBlocked`, render buttons as disabled with tooltip instead of clickable
  - [x] 7.4 In `DRPlanDetailPage.tsx`, pass `isBlocked={!sitesInSync.inSync}` and `blockedTooltip="Blocked: sites do not agree on VM inventory"` to `DRLifecycleDiagram`

- [x] Task 8: Dashboard table warning indicator for SitesInSync=False (AC: #4)
  - [x] 8.1 In `DRDashboard.tsx`, import `getSitesInSync` and add `sitesInSync: SitesInSyncStatus` to `EnrichedPlan`
  - [x] 8.2 In `enrichPlans()`, add `sitesInSync: getSitesInSync(plan)` to each enriched plan
  - [x] 8.3 In the `Protected` column (`Td` at index 3), when `ep.sitesInSync.inSync === false`, render an `ExclamationTriangleIcon` (color: `--pf-t--global--icon--color--status--warning--default`) with a `Tooltip`: "Sites do not agree on VM inventory" — render this BEFORE the `ReplicationHealthIndicator` so it's visible at a glance
  - [x] 8.4 In `DRPlanActions.tsx`, add an `isDisabled?: boolean` and `disabledTooltip?: string` prop. When `isDisabled`, render the kebab `MenuToggle` inside a `Tooltip` with the tooltip text, and set `isDisabled` on the `MenuToggle`
  - [x] 8.5 In `DRDashboard.tsx`, pass `isDisabled={!ep.sitesInSync.inSync}` and `disabledTooltip="Plan blocked: sites do not agree on VM inventory"` to `DRPlanActions`

- [x] Task 9: Unit tests (AC: #7)
  - [x] 9.1 Create `console-plugin/tests/components/SiteDiscoverySection.test.tsx`:
    - Both sites populated with matching VMs — all rows default style, no warning icons
    - Both populated with mismatched VMs — extra VMs highlighted, warning icons present
    - One site nil — informational waiting text displayed
    - Both nil — "not yet available" message
    - Stale discovery time (> 5 min) — stale warning rendered
    - Fresh discovery time — no stale warning
    - jest-axe passes on all states
  - [x] 9.2 Create `console-plugin/tests/components/SiteDisagreementAlert.test.tsx`:
    - SitesInSync=False — danger alert rendered with correct title and delta summary
    - SitesInSync=True — no alert rendered
    - No SitesInSync condition — no alert rendered (backward compat)
    - AlertActionLink click calls `onSwitchToConfig`
    - Alert disappears on rerender with SitesInSync=True
    - jest-axe passes on alert-visible and alert-absent states
  - [x] 9.3 Update `console-plugin/tests/components/DRPlanDetailPage.test.tsx`:
    - Add test: plan with SitesInSync=False renders danger alert above overview
    - Add test: clicking "View site differences" switches to Configuration tab
    - Add test: plan with SitesInSync=True (or no condition) renders no alert
  - [x] 9.4 Update `console-plugin/tests/components/DRLifecycleDiagram.test.tsx`:
    - Add test: `isBlocked=true` disables all action buttons
    - Add test: `isBlocked=false` actions work normally (regression)
  - [x] 9.5 Update `console-plugin/tests/components/DRDashboard.test.tsx`:
    - Add test: plan with SitesInSync=False shows warning icon in table row
    - Add test: plan with SitesInSync=False has disabled kebab menu
  - [x] 9.6 Update `console-plugin/tests/components/DRPlanActions.test.tsx`:
    - Add test: `isDisabled=true` renders disabled kebab with tooltip
  - [x] 9.7 Update `console-plugin/tests/components/PlanConfiguration.test.tsx`:
    - Add test: plan with site discovery renders SiteDiscoverySection
    - Add test: plan without primarySite/secondarySite skips SiteDiscoverySection
  - [x] 9.8 Create or update `console-plugin/tests/utils/drPlanUtils.test.ts`:
    - `getSitesInSync` with True condition → `{ inSync: true }`
    - `getSitesInSync` with False/VMsMismatch → `{ inSync: false, reason, message }`
    - `getSitesInSync` with no condition → `{ inSync: true }` (backward compat)
    - `parseSiteDiscoveryDelta` parses structured message correctly
    - `parseSiteDiscoveryDelta` with empty/malformed message returns empty arrays

- [x] Task 10: Verify build and lint (AC: #7)
  - [x] 10.1 Run `cd console-plugin && yarn build` — zero errors
  - [x] 10.2 Run `cd console-plugin && yarn test` — all tests pass
  - [x] 10.3 Run `cd console-plugin && yarn lint` — zero new lint errors (if lint target exists)

### Review Findings

- [x] [Review][Patch] Disabled dashboard kebab tooltip is attached to a disabled control, so the blocked-plan explanation may never appear [`console-plugin/src/components/DRDashboard/DRPlanActions.tsx:46`] — **Fixed**: wrapped disabled Dropdown in `<span>` so Tooltip receives pointer events
- [x] [Review][Patch] `SiteDisagreementAlert` undercounts mismatches when the backend message is capped with `... and N more` [`console-plugin/src/utils/drPlanUtils.ts:135`] — **Fixed**: `parseSiteDiscoveryDelta` now extracts `primaryMoreCount`/`secondaryMoreCount` from "and N more" suffix; alert sums named VMs + extra count
- [x] [Review][Patch] The blocking-state live region is unmounted as soon as plans return in sync, so the unblock transition may not be announced [`console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx:109`] — **Fixed**: moved `aria-live="assertive"` wrapper to always-rendered parent div in DRPlanDetailPage; alert content renders conditionally inside it
- [x] [Review][Patch] `jest-axe` coverage is missing for several new states required by AC7, including stale and one-site-missing discovery states plus the dashboard blocked state [`console-plugin/tests/components/SiteDiscoverySection.test.tsx:115`] — **Fixed**: added axe tests for one-site-nil and stale-discovery states in SiteDiscoverySection, plus dashboard blocked-row state in DRDashboard
- [x] [Review][Patch] The dashboard Protected column introduces hardcoded spacing instead of PatternFly tokens [`console-plugin/src/components/DRDashboard/DRDashboard.tsx:271`] — **Fixed**: replaced `gap: '0.5rem'` with `var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))` in both Protected and Last Execution columns

## Dev Notes

### Scope & Approach

This is a purely frontend story — all changes are within `console-plugin/`. The backend data (SiteDiscovery fields, SitesInSync condition, preflight enrichment) is provided by Stories 8.2 and 8.3. This story reads those fields from the existing `useDRPlan` watch hook and renders them in the UI.

**Change pattern:** Extend types → add utility helpers → create new components → integrate into existing pages → add blocking behavior → tests.

### Critical: No SiteDiscovery TypeScript Interface Exists Yet

The plugin's `types.ts` has no `SiteDiscovery` interface. You must add it (Task 1). The Go type added in Story 8.2 has these fields:
- `vms []DiscoveredVM` (same `DiscoveredVM` type already in types.ts — `{ name, namespace }`)
- `discoveredVMCount int`
- `lastDiscoveryTime metav1.Time` (serializes as ISO 8601 string)

The `DRPlanStatus` interface needs `primarySiteDiscovery?: SiteDiscovery` and `secondarySiteDiscovery?: SiteDiscovery` added.

### Critical: SitesInSync Condition Is a Standard Condition

The `SitesInSync` condition is a standard `metav1.Condition` on `plan.status.conditions[]`. The existing `Condition` interface in `types.ts` (lines 58-65) already supports it. Read it with:
```ts
const cond = plan.status?.conditions?.find(c => c.type === 'SitesInSync');
```
This follows the exact same pattern as `getReplicationHealth` in `drPlanUtils.ts` (line 42).

### Critical: Backward Compatibility

Plans created before Epic 8 will NOT have `SitesInSync` condition or SiteDiscovery fields. The `getSitesInSync` helper MUST return `{ inSync: true }` when the condition is absent — otherwise every pre-existing plan would appear blocked. The `SiteDiscoverySection` should not render at all when `plan.spec.primarySite` is empty (no site topology configured).

### Critical: Delta Message Parsing

The `SitesInSync` condition message from Story 8.3 follows this format:
- `"VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]"`
- `"Site dc-east discovered 0 VMs; verify VMs have label soteria.io/drplan=<planName>"`
- Cap at ~20 per side with `"... and N more"`

Parse with regex or string split. The `parseSiteDiscoveryDelta` function should handle all variants gracefully and return empty arrays for unparseable messages.

### Critical: Watch-Driven Reactivity

All UI updates are driven by the `useDRPlan` watch hook. When the backend reconciler updates the `SitesInSync` condition or `SiteDiscovery` fields, the watch delivers the updated plan object. React re-renders naturally — no polling, no setTimeout, no manual refresh needed. The alert appears/disappears, buttons enable/disable, and the site discovery section updates automatically.

### Critical: PlanConfiguration Layout Change

The current `PlanConfiguration` uses a two-column CSS Grid (`1fr 1fr`) for Plan Information (left) and Replication Health (right). The new `SiteDiscoverySection` should sit ABOVE this grid as a full-width element. Structure:

```tsx
<div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--pf-t--global--spacer--xl, ...)' }}>
  {hasSiteTopology && <SiteDiscoverySection plan={plan} />}
  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', ... }}>
    {/* existing Plan Information + Replication Health */}
  </div>
</div>
```

### Critical: PatternFly Component Selection

- **Alert (danger, inline):** For the blocking banner — same pattern as `AlertBannerSystem.tsx`
- **Alert (warning, inline, isPlain):** For the stale discovery warning — subtle, not alarming
- **AlertActionLink:** For the "View site differences" link in the danger alert
- **Table (compact):** For the VM comparison list in each site column
- **Tooltip:** For disabled action button explanations
- **ExclamationTriangleIcon:** For warning indicators (from `@patternfly/react-icons`)
- **Content (h3):** For section headers — same pattern as `PlanConfiguration.tsx`

### Critical: CSS Token Usage

Per project-context, use PF6 tokens with PF5 fallback:
```css
var(--pf-t--global--color--status--warning--default, var(--pf-v5-global--warning-color--100))
var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))
```
No hardcoded colors, spacing, or font sizes.

### Critical: Accessibility Requirements

1. **ARIA live region** on `SiteDisagreementAlert` container — `aria-live="assertive"` announces when blocking state appears
2. **Keyboard navigation** — VM comparison table must be navigable; PatternFly Table handles this by default
3. **Screen reader text** — warning icons on mismatched VMs need `aria-label` or visually-hidden text: "VM present on primary site only" / "VM present on secondary site only"
4. **Color independence** — warning state must use icon+text, not color alone (consistent with project rule: "color-independent status on all indicators")
5. **jest-axe** on all new component states

### Dependency on Stories 8.2 and 8.3

This story assumes the backend has been implemented:
- **Story 8.2:** `primarySiteDiscovery` and `secondarySiteDiscovery` fields exist on `DRPlanStatus` in the Go types and are populated by the reconciler
- **Story 8.3:** `SitesInSync` condition exists on `plan.status.conditions[]` with reasons `VMsAgreed`, `VMsMismatch`, `WaitingForDiscovery`; `sitesInSync` and `siteDiscoveryDelta` fields exist on `PreflightReport`; admission webhook rejects executions when `SitesInSync=False`

If those stories have NOT been implemented, you can still build the UI — it will gracefully degrade (no condition = not blocked, no SiteDiscovery = section hidden). But you should implement them first for full testability.

### VM Comparison Algorithm for Display

To identify which VMs are mismatched for visual highlighting:
1. Build a Set of `"namespace/name"` keys from primary VMs
2. Build a Set from secondary VMs
3. VMs in primary but not secondary → highlight in primary column
4. VMs in secondary but not primary → highlight in secondary column
5. VMs in both → default style

This mirrors the Go `compareSiteDiscovery` function from Story 8.3 but runs client-side for display purposes.

### Existing Patterns to Follow

| Pattern | Source | Reuse |
|---------|--------|-------|
| Condition reading | `drPlanUtils.ts:42` (`getReplicationHealth`) | Same `.find(c => c.type === ...)` pattern |
| Danger alert banner | `AlertBannerSystem.tsx` | Same Alert variant=danger isInline pattern |
| AlertActionLink wiring | `AlertBannerSystem.tsx:30` | Same callback pattern |
| Two-column layout | `PlanConfiguration.tsx:47` | Same CSS Grid pattern |
| Relative time formatting | `formatters.ts:15` (`formatRelativeTime`) | Reuse for lastDiscoveryTime display |
| Table (compact) | `DRDashboard.tsx:241` | Same PatternFly Table pattern |
| Tooltip on disabled elements | N/A (new pattern) | Import `Tooltip` from `@patternfly/react-core` |
| Warning icon | `ReplicationHealthIndicator` uses icons | Same `@patternfly/react-icons` imports |
| Test fixture pattern | `AlertBannerSystem.test.tsx:8-36` | Same `makePlan` helper pattern |
| jest-axe pattern | `AlertBannerSystem.test.tsx:255-289` | Same `axe(container)` + `toHaveNoViolations` |

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `console-plugin/src/models/types.ts` | Add `SiteDiscovery` interface + extend `DRPlanStatus` + extend `PreflightReport` | Type additions |
| `console-plugin/src/utils/drPlanUtils.ts` | Add `getSitesInSync` + `parseSiteDiscoveryDelta` | Utility additions |
| `console-plugin/src/components/DRPlanDetail/SiteDiscoverySection.tsx` | **NEW** — site-by-site VM comparison | New component |
| `console-plugin/src/components/DRPlanDetail/SiteDisagreementAlert.tsx` | **NEW** — danger alert for SitesInSync=False | New component |
| `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` | Add SiteDisagreementAlert + pass isBlocked to diagram + onSwitchToConfig | Integration |
| `console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx` | Add SiteDiscoverySection above existing layout | Layout enhancement |
| `console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx` | Add `isBlocked`/`blockedTooltip` props, disable buttons | Behavior change |
| `console-plugin/src/components/DRDashboard/DRDashboard.tsx` | Add sitesInSync to EnrichedPlan, warning icon in table | Table enhancement |
| `console-plugin/src/components/DRDashboard/DRPlanActions.tsx` | Add `isDisabled`/`disabledTooltip` props | Behavior change |
| `console-plugin/tests/components/SiteDiscoverySection.test.tsx` | **NEW** | Test addition |
| `console-plugin/tests/components/SiteDisagreementAlert.test.tsx` | **NEW** | Test addition |
| `console-plugin/tests/components/DRPlanDetailPage.test.tsx` | Add 3 tests | Test update |
| `console-plugin/tests/components/DRLifecycleDiagram.test.tsx` | Add 2 tests | Test update |
| `console-plugin/tests/components/DRDashboard.test.tsx` | Add 2 tests | Test update |
| `console-plugin/tests/components/DRPlanActions.test.tsx` | Add 1 test | Test update |
| `console-plugin/tests/components/PlanConfiguration.test.tsx` | Add 2 tests | Test update |
| `console-plugin/tests/utils/drPlanUtils.test.ts` | **NEW or update** | Test addition |

### Testing Strategy

- **Unit tests:** Component-level RTL tests with mocked DRPlan fixtures containing various SiteDiscovery/condition states. Pure function tests for `getSitesInSync` and `parseSiteDiscoveryDelta`
- **jest-axe:** On every new component state (alert visible, alert absent, stale warning, matching VMs, mismatched VMs, nil discovery)
- **Regression:** All existing tests must pass unchanged — new features are additive. Existing plan fixtures lack SiteDiscovery/SitesInSync data, which triggers backward compat path (no alert, no section)
- **Mock pattern:** Existing tests mock `useDRResources` hooks. New tests create plan fixtures with `SitesInSync` condition in `status.conditions[]`
- **No SDK mocking changes needed:** `useDRPlan` already returns the full plan object including conditions and status — just add the right fixture data

### Test Fixture Pattern

Follow the established `makePlan` pattern from `AlertBannerSystem.test.tsx`. Add SiteDiscovery and condition fields:

```ts
function makePlanWithSiteDiscovery(
  name: string,
  opts: {
    sitesInSync?: 'True' | 'False';
    sitesReason?: string;
    sitesMessage?: string;
    primaryVMs?: DiscoveredVM[];
    secondaryVMs?: DiscoveredVM[];
    primaryLastDiscovery?: string;
    secondaryLastDiscovery?: string;
  } = {},
): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name, uid: name, creationTimestamp: '' },
    spec: {
      maxConcurrentFailovers: 1,
      primarySite: 'dc-west',
      secondarySite: 'dc-east',
    },
    status: {
      phase: 'SteadyState',
      conditions: opts.sitesInSync ? [{
        type: 'SitesInSync',
        status: opts.sitesInSync,
        reason: opts.sitesReason,
        message: opts.sitesMessage,
      }] : [],
      primarySiteDiscovery: opts.primaryVMs ? {
        vms: opts.primaryVMs,
        discoveredVMCount: opts.primaryVMs.length,
        lastDiscoveryTime: opts.primaryLastDiscovery ?? new Date().toISOString(),
      } : undefined,
      secondarySiteDiscovery: opts.secondaryVMs ? {
        vms: opts.secondaryVMs,
        discoveredVMCount: opts.secondaryVMs.length,
        lastDiscoveryTime: opts.secondaryLastDiscovery ?? new Date().toISOString(),
      } : undefined,
    },
  };
}
```

### Execution Order

1. Task 1 (types) — foundation for everything
2. Task 2 (utils) — `getSitesInSync` + parser needed by all components
3. Task 3 (SiteDiscoverySection) — the VM comparison display
4. Task 4 (SiteDisagreementAlert) — the blocking banner
5. Task 6 (PlanConfiguration integration) — wire in the section
6. Task 5 (DRPlanDetailPage integration) — wire in the alert + tab switch
7. Task 7 (lifecycle diagram) — disable buttons when blocked
8. Task 8 (dashboard) — warning icon + disabled kebab
9. Task 9 (tests) — verify all behavior
10. Task 10 (build + lint) — final validation

### Previous Story Learnings (from 8.3 story file)

- **Delta message is structured for parsing:** Format is predictable: `"VMs on primary but not secondary: [ns/vm-a, ...]; VMs on secondary but not primary: [ns/vm-c, ...]"` — build the parser against this format
- **updateStatus writes SitesInSync alongside Ready:** The condition is always present in site-aware mode after first reconcile — you won't see it flip from "no condition" to "has condition" during normal operation. On first deploy, `WaitingForDiscovery` appears quickly
- **Cap at ~20 VMs per side in message:** If the delta exceeds 20, the message ends with `"... and N more"` — parser must handle this
- **Admission webhook already blocks execution:** The UI disabling buttons is a UX convenience — the backend would reject the request anyway. But it prevents confusing error messages

### Project Structure Notes

- All changes within `console-plugin/` — separate TypeScript project
- No Go changes — this story is purely frontend
- No `make generate` or `make manifests` needed
- Follow existing component directory structure: new components in `DRPlanDetail/`, tests in `tests/components/`
- Import paths use `../../` relative (no aliases configured)

### References

- [Source: console-plugin/src/models/types.ts#L58-94] — Condition interface, DRPlanStatus (extend here)
- [Source: console-plugin/src/models/types.ts#L102-105] — DiscoveredVM (reuse for SiteDiscovery.vms)
- [Source: console-plugin/src/models/types.ts#L123-132] — PreflightReport (add sitesInSync fields)
- [Source: console-plugin/src/utils/drPlanUtils.ts#L41-53] — getReplicationHealth (same condition reading pattern)
- [Source: console-plugin/src/utils/formatters.ts#L15-29] — formatRelativeTime (reuse for lastDiscoveryTime)
- [Source: console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx#L1-141] — Page shell (integration point)
- [Source: console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx#L39-121] — Configuration tab (add section here)
- [Source: console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx#L130-198] — TransitionEdge (add disabled state)
- [Source: console-plugin/src/components/DRDashboard/DRDashboard.tsx#L38-53] — EnrichedPlan + enrichPlans (add sitesInSync)
- [Source: console-plugin/src/components/DRDashboard/DRPlanActions.tsx#L12-61] — Kebab menu (add isDisabled)
- [Source: console-plugin/src/components/DRDashboard/AlertBannerSystem.tsx#L1-50] — Alert pattern reference
- [Source: console-plugin/tests/components/AlertBannerSystem.test.tsx#L1-290] — Test pattern reference
- [Source: _bmad-output/project-context.md] — PF6 token fallback, Console SDK constraints, accessibility rules
- [Source: _bmad-output/planning-artifacts/epics.md#Story-8.4] — Epic requirements
- [Source: _bmad-output/implementation-artifacts/8-3-cross-site-vm-agreement-plan-readiness-gating.md] — Backend context, delta format

## Dev Agent Record

### Agent Model Used
Opus 4.6 (Cursor Agent)

### Debug Log References
- All 547 tests pass (40 new tests added from 507 baseline, including 3 post-review axe coverage additions)
- Webpack production build succeeds (zero errors, pre-existing asset size warning only)
- Go unit and integration tests pass (no regressions)

### Completion Notes List
- Task 1: Added `SiteDiscovery` interface and extended `DRPlanStatus` + `PreflightReport` types
- Task 2: Added `getSitesInSync` and `parseSiteDiscoveryDelta` utilities following existing condition-reading pattern
- Task 3: Created `SiteDiscoverySection` with two-column VM comparison, staleness warning, and mismatch highlighting
- Task 4: Created `SiteDisagreementAlert` with danger alert, delta summary parsing, and aria-live region
- Task 5: Integrated alert into DRPlanDetailPage Overview tab with tab-switch and scroll-to-anchor handler
- Task 6: Integrated SiteDiscoverySection into PlanConfiguration above existing grid, guarded by site topology check
- Task 7: Added `isBlocked`/`blockedTooltip` props to DRLifecycleDiagram, disabling action buttons with tooltip
- Task 8: Added warning icon + disabled kebab in DRDashboard for plans with SitesInSync=False
- Task 9: Created comprehensive unit tests (SiteDiscoverySection, SiteDisagreementAlert, drPlanUtils) and updated 5 existing test files
- Task 10: Verified build, lint, and full regression suite green

### File List
- console-plugin/src/models/types.ts (modified — added SiteDiscovery interface, extended DRPlanStatus + PreflightReport)
- console-plugin/src/utils/drPlanUtils.ts (modified — added getSitesInSync, SitesInSyncStatus, parseSiteDiscoveryDelta, SiteDiscoveryDelta)
- console-plugin/src/components/DRPlanDetail/SiteDiscoverySection.tsx (new — site VM comparison component)
- console-plugin/src/components/DRPlanDetail/SiteDisagreementAlert.tsx (new — danger alert for blocked plans)
- console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx (modified — integrated alert + isBlocked + onSwitchToConfig)
- console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx (modified — added SiteDiscoverySection above grid)
- console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx (modified — added isBlocked/blockedTooltip props)
- console-plugin/src/components/DRDashboard/DRDashboard.tsx (modified — added warning icon + disabled kebab + sitesInSync enrichment)
- console-plugin/src/components/DRDashboard/DRPlanActions.tsx (modified — added isDisabled/disabledTooltip props)
- console-plugin/tests/components/SiteDiscoverySection.test.tsx (new)
- console-plugin/tests/components/SiteDisagreementAlert.test.tsx (new)
- console-plugin/tests/components/DRPlanDetailPage.test.tsx (modified — 3 new tests)
- console-plugin/tests/components/DRLifecycleDiagram.test.tsx (modified — 2 new tests)
- console-plugin/tests/components/DRDashboard.test.tsx (modified — 2 new tests)
- console-plugin/tests/components/DRPlanActions.test.tsx (modified — 1 new test)
- console-plugin/tests/components/PlanConfiguration.test.tsx (modified — 2 new tests)
- console-plugin/tests/utils/drPlanUtils.test.ts (modified — 11 new tests for getSitesInSync + parseSiteDiscoveryDelta)
