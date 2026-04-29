# User Acceptance Test — Epic 7: Toast Notifications & Transition Progress

Date: 2026-04-29
Environment: OCP 4.20.4 (etl6 + etl7 stretched cluster)
Tester: raffa

## Objective

Validate Sprint 7 features (toast notifications, transition progress, execution detail page) end-to-end on the live stretched cluster after deploying the updated Soteria console plugin.

## Deployment Method

Console plugin built and deployed independently of the full `stretched-local-test.sh` flow:
1. `podman build -t quay.io/raffaelespazzoli/soteria-console-plugin:latest console-plugin`
2. `podman push quay.io/raffaelespazzoli/soteria-console-plugin:latest`
3. Applied `hack/overlays/base/console-plugin.yaml` with `sed` image substitution to both etl6 and etl7
4. `kubectl rollout restart deployment/soteria-console-plugin` on both clusters
5. Verified plugin enabled on `consoles.operator.openshift.io cluster` (already active from Epic 6)

## Issues Found & Resolved

### Issue 1: TransitionProgressBanner rendered as Alert instead of progress bar

**Symptom:** During a planned failover, a PatternFly `Alert` with an "actionLinks" link appeared above the state diagram instead of a visual progress bar.

**Root Cause:** The `TransitionProgressBanner` component used a PatternFly `<Alert variant="info">` with wave info as body text and a `<Link>` in `actionLinks`. The user expected an inline progress bar showing percentage completion.

**Fix:** Replaced the `Alert` with a PatternFly `<Progress>` component:
- `value` = `(completedWaves / totalWaves) * 100`, with `ProgressMeasureLocation.top` and `ProgressVariant.info`
- Below the bar: wave count, elapsed time, estimated remaining time
- "View execution details" rendered as a `<Button variant="link">` using `history.push()` (consistent with toast and history table navigation patterns)

**Files changed:**

| File | Change |
|------|--------|
| `console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx` | Replaced `Alert` + `Link` with `Progress` + `Button` + `useHistory` |

### Issue 2: Execution detail page never loads

**Symptom:** Clicking "View execution details" from the progress bar or toast notification navigated to `/disaster-recovery/executions/<name>` but the page showed loading skeletons indefinitely.

**Root Cause:** The `useDRExecution` hook used `useK8sWatchResource` with `isList: false` (single-resource watch) for `DRExecution` resources served by the Soteria aggregated API server. The OpenShift Console SDK's WebSocket watch mechanism does not reliably establish individual resource watches against aggregated API servers — the watch hangs and `loaded` never becomes `true`.

List-based watches (`isList: true`) to the same aggregated API work correctly, as demonstrated by `useDRExecutions()` which powers the dashboard and execution history table.

**Investigation iterations:**

1. **Attempt 1 — modify `useDRExecution` to use list-based watch with nullable resource:** Changed from `isList: false` to `isList: true` with `WatchK8sResource | null` type, filtering by name client-side. Result: page still hung, possibly due to the nullable resource spec causing the SDK to use a different internal code path.

2. **Attempt 2 — make resource spec always non-nullable:** Removed the null branch so the list watch is always active. Result: broke the module — page rendered completely blank (no component loaded at all), likely due to the always-active watch interfering with the DRPlanDetailPage which also calls `useDRExecution`.

3. **Attempt 3 — revert hook, change only the page:** Reverted `useDRExecution` to the original single-resource implementation (which works on the plan detail page where `activeExecName` starts empty). Changed `ExecutionDetailPage` to use `useDRExecutions()` directly (the known-working list hook) and find the execution by name with `.find()`. Also added a "not found" guard to show a clear warning when the list is loaded but the execution isn't in it.

**Fix (Attempt 3):**

| File | Change |
|------|--------|
| `console-plugin/src/hooks/useDRResources.ts` | Reverted `useDRExecution` to original (single-resource, `isList: false`) |
| `console-plugin/src/components/ExecutionDetail/ExecutionDetailPage.tsx` | Replaced `useDRExecution(name)` with `useDRExecutions()` + `.find(e => e.metadata?.name === name)`. Added explicit "not found" state (warning alert) separate from loading state (skeletons) and error state (danger alert). |

**Technical notes:**

- `useDRExecution` still works correctly on the `DRPlanDetailPage` because `activeExecName` starts as `''` (triggering the null/short-circuit path), and the single-resource watch only activates during an active transition when the SDK already has API discovery cached.
- `useDRExecutions()` (list) works on every page because list watches to aggregated APIs are handled differently by the SDK — they use standard HTTP long-polling rather than per-resource WebSocket watches.
- The `ExecutionDetailPage` now shares the same data-fetching approach as the dashboard, ensuring consistent behavior.

**Status:** Pending final verification. The progress bar fix is confirmed working. The execution detail page fix (attempt 3) is deployed and awaiting user re-test.

### Issue 3: DRPlan overview page flickers on periodic status updates

**Symptom:** The DRPlan detail overview page periodically flashed default/zeroed values (e.g. 0 VMs, SteadyState badge) for a brief moment before re-rendering with correct data. This happened approximately every 30 seconds.

**Root Cause (controller):** The DRPlan status was being patched every 30 seconds even when no business-relevant data changed. The `UnprotectedVMs` field was populated on the preflight report *after* the change-detection comparison in `updateStatus`, so every reconcile saw a diff between the stored preflight (which had 18 unprotected VMs from the previous patch) and the newly computed preflight (which had none yet). Additionally, all 5 volume groups reported `Unknown` health (not `Healthy`), triggering the 30-second degraded requeue interval instead of the normal 10-minute interval.

**Root Cause (UI):** When the DRPlan resource version changed, the `useK8sWatchResource` SDK hook could briefly transition through a loading/error state. The `useDRPlan` hook returned `undefined` during these transient states, causing the detail page to unmount the tabs and show skeletons momentarily.

**Fix (controller):** Removed the `UnprotectedVMs` and `UnprotectedVMCount` fields from the DRPlan status entirely. Cluster-wide unprotected VM data does not belong on a per-plan resource — every DRPlan carried the same global list, and it was the root cause of the infinite status patch loop. The `UnprotectedVMDiscoverer` interface, `ListUnprotectedVMs` method, related metrics (`soteria_unprotected_vms_total`), events (`UnprotectedVMsDetected`, `AllVMsProtected`), and all associated tests were removed.

**Fix (UI):** Added a "stale-while-revalidate" pattern to `useDRPlan` and `useDRExecution` hooks — a `useRef` caches the last successfully loaded resource, and after the initial load the hook always returns cached data during transient loading/error states, preventing the skeleton flash.

**Files changed:**

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Removed `UnprotectedVMCount` from `DRPlanStatus`, `UnprotectedVMs` from `PreflightReport` |
| `pkg/controller/drplan/reconciler.go` | Removed `UnprotectedVMDiscoverer` field, `detectUnprotectedVMs`, `emitUnprotectedVMEvents`, `unprotectedVMResult`, `maxUnprotectedVMs`; removed `unprotected` parameter from `updateStatus` |
| `pkg/engine/discovery.go` | Removed `UnprotectedVMDiscoverer` interface and `ListUnprotectedVMs` method |
| `pkg/metrics/metrics.go` | Removed `UnprotectedVMsTotal` gauge and `RecordUnprotectedVMs` function |
| `pkg/registry/drplan/strategy.go` | Removed "Unprotected" column from table convertor |
| `cmd/soteria/main.go` | Removed `UnprotectedVMDiscoverer` wiring |
| `console-plugin/src/hooks/useDRResources.ts` | Added `useRef`-based caching to `useDRPlan` and `useDRExecution` |
| `console-plugin/src/models/types.ts` | Removed `unprotectedVMCount` and `unprotectedVMs` fields |

**TODO:** Add an "Unprotected VMs" tab to the Disaster Recovery dashboard overview page as a pure UI feature. This would query unprotected VMs on-demand (using the existing `ListUnprotectedVMs` engine capability) rather than embedding the data in each DRPlan's status. This is a UI/UX enhancement and does not require any controller changes.

## Summary

Sprint 7 UAT on the live OCP 4.20 stretched cluster uncovered 3 issues. The most significant finding was the OpenShift Console SDK's `useK8sWatchResource` not supporting reliable single-resource WebSocket watches against aggregated API servers, requiring a list-based fallback on the execution detail page. The transition progress banner was corrected from an Alert to a proper Progress bar, the execution detail page loading issue was resolved by switching to a list-based data fetch, and a 30-second status patch loop causing UI flicker was eliminated by removing misplaced cluster-wide data from the per-plan status and adding stale-while-revalidate caching to the UI hooks.
