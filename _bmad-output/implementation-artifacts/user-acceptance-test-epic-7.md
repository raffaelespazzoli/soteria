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

**Status:** Verified working. The execution detail page loads correctly via the list-based fetch when navigated with a valid execution name.

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

### Issue 4: `estimatedRPO` field displayed `unknown` and had no real backing data

**Symptom:** The DRPlan status showed `estimatedRPO: unknown` for every volume group. The UI had RPO columns in multiple tables, a formatRPO utility, RPO display in the preflight modal, execution header, execution summary, and history table — all fed by data that was never populated by the controller.

**Root Cause:** The `estimatedRPO` field on `VolumeGroupHealth` was intended to report RPO from the storage driver's `ReplicationStatus.EstimatedRPO`. However, the fallback noop driver always returned `nil` for this field, and no real storage driver was integrated yet. The controller's `computeRPO`/`formatDuration`/`buildReplicationLagEntries` functions produced `"unknown"` or zero values. The UI's `rpoSeconds` parsing (regex on condition messages) was dead code — the controller never included RPO data in condition messages. The `soteria_replication_lag_seconds` Prometheus metric was similarly always zero.

**Fix:** Removed `estimatedRPO` end-to-end across the full stack:

| Layer | Files | Change |
|-------|-------|--------|
| API types (Go) | `types.go` | Removed `EstimatedRPO` from `VolumeGroupHealth` |
| Driver types (Go) | `drivers/types.go` | Removed `EstimatedRPO *time.Duration` from `ReplicationStatus` |
| Controller | `health.go`, `reconciler.go` | Removed `computeRPO`, `formatDuration`, `buildReplicationLagEntries`, RPO logging |
| Metrics | `metrics.go`, `doc.go` | Removed `ReplicationLagSeconds` GaugeVec, `RecordPlanReplicationHealth`, `ReplicationLagEntry` |
| Noop driver | `noop/driver.go` | Removed `EstimatedRPO: &zero` return |
| UI types | `models/types.ts` | Removed `estimatedRPO` from `VolumeGroupHealth`, `rpoSeconds` from `DRExecutionStatus` |
| UI hooks | `usePreflightData.ts` | Removed `estimatedRPO`, `estimatedRPOSeconds`, `formatRPOHuman`, `getEstimatedRPO` |
| UI utils | `formatters.ts`, `drPlanUtils.ts` | Removed `formatRPO`, `rpoSeconds` from `ReplicationHealth` |
| UI components | `ReplicationHealthIndicator.tsx`, `ReplicationHealthExpanded.tsx`, `WaveCompositionTree.tsx`, `PreflightConfirmationModal.tsx`, `ExecutionHeader.tsx`, `ExecutionSummary.tsx`, `ExecutionHistoryTable.tsx` | Removed RPO columns, RPO display, RPO aria-labels |
| Tests | Multiple Go + Jest test files | Removed RPO-related assertions, fixtures, mock data |

### Issue 5: Non-replicating volumes reported `Unknown` health instead of a meaningful status

**Symptom:** Volume groups backed by the fallback noop driver (no PVC storage class found) showed `health: Unknown` in the DRPlan status. The controller treated `Unknown` as degraded, triggering the 30-second requeue interval. "Unknown" implied the system couldn't determine the state, when in reality the volumes simply weren't replicated.

**Root Cause:** The noop driver's `GetReplicationStatus` returned `Health: HealthUnknown` for `RoleNonReplicated` volumes. The controller's `anyNonHealthy` and `allHealthy` helpers counted `Unknown` as a degraded state, and `computeReplicationCondition` classified it as contributing to a `Degraded` condition reason.

**Fix:** Introduced `NotReplicating` as a distinct, neutral health status:

| Layer | Files | Change |
|-------|-------|--------|
| Driver types | `drivers/types.go` | Added `HealthNotReplicating` constant |
| API types | `types.go` | Added `NotReplicating` to `VolumeGroupHealthStatus` enum and kubebuilder validation |
| Noop driver | `noop/driver.go` | Returns `HealthNotReplicating` for `RoleNonReplicated` |
| Controller | `health.go`, `reconciler.go` | `mapReplicationStatus` maps `HealthNotReplicating`; `anyNonHealthy`/`allHealthy` treat `NotReplicating` as neutral (not degraded, doesn't block "all healthy"); `computeReplicationCondition` excludes it from degradation |
| UI types | `models/types.ts` | Added `NotReplicating` to `VolumeGroupHealthStatus` |
| UI utils | `drPlanUtils.ts` | Added `NotReplicating: 4` to `HEALTH_SORT_ORDER` |
| UI components | `ReplicationHealthIndicator.tsx` | Added `NotReplicating` config with `MinusCircleIcon`, disabled color, label "Not replicating" |
| UI components | `WaveCompositionTree.tsx` | Added `NotReplicating` to aggregate health, icons, label colors |
| Tests | `noop/driver_test.go`, multiple Go/Jest tests | Updated expectations to `HealthNotReplicating` |

**Result:** Volume groups with no replication now show "Not replicating" with a neutral grey icon. The ReplicationHealthy condition is `True` with `Reason: AllHealthy` (instead of `False`/`Degraded`), and the controller uses the normal 10-minute requeue interval.

### Issue 6: "View execution details" navigated to `/disaster-recovery/executions/undefined`

**Symptom:** After triggering an execution from the DRPlan detail page, clicking "View execution details" in the `TransitionProgressBanner` navigated to a URL with the literal string `undefined` as the execution name. The execution detail page showed: "DRExecution 'undefined' was not found. It may have been deleted."

**Root Cause:** The banner's click handler captured `activeExec` from the render closure via `plan.status?.activeExecution`. A suspected timing race between the plan watch update (controller clearing `activeExecution` on completion) and the user's click caused `activeExec` to be the JS `undefined` value, which JavaScript's template literal coerced to the string `"undefined"`. The `{activeExec && ...}` render guard should have hidden the button, but the race window between React's reconciliation and the DOM event could leave a stale button interactable.

**Fix:** Two defensive changes:

| File | Change |
|------|--------|
| `TransitionProgressBanner.tsx` | Captures `execDetailPath` as a string constant at render time (primitive value, immune to object mutation). Button only rendered when `execDetailPath` is truthy. |
| `ExecutionDetailPage.tsx` | Added `Redirect` to `/disaster-recovery` when resolved name is falsy. |

**Status:** Initial fix resolved the `undefined` navigation, but revealed Issue 7 below.

### Issue 7: `useParams()` returns `undefined` for `:name` route parameter in OpenShift Console plugin pages

**Symptom:** After fixing Issue 6, clicking "View execution details" navigated to the correct URL (confirmed by `window.location.pathname`), but the `ExecutionDetailPage` immediately redirected back to `/disaster-recovery`. Debug logging showed:
```
[ExecutionDetailPage] mount/render {
  params: {…},     // object exists but name key is undefined
  name: undefined,
  pathname: '/disaster-recovery/executions/fedora-app-planned-migration-1777518537936'
}
```
The component mounted, the URL was correct, but `useParams<{ name: string }>()` returned `{ name: undefined }`.

**Root Cause:** The OpenShift Console plugin SDK renders `console.page/route` components **outside** of a React Router `<Route>` context that provides route parameters. Despite the route being declared with `:name` in `console-extensions.json`:
```json
{ "type": "console.page/route", "properties": { "path": "/disaster-recovery/executions/:name", ... } }
```
the SDK's internal routing mechanism matches the path and renders the component, but does **not** wrap it in a React Router `<Route path="/disaster-recovery/executions/:name">` that would populate the `useParams()` context. The React Router `useParams()` hook therefore reads from a parent Route context that has no `:name` parameter.

This affects **all** `console.page/route` extensions, not just the execution page. The `DRPlanDetailPage` appeared to work because `useDRPlan(undefined)` with a single-resource watch against the aggregated API happened to return the only existing DRPlan by coincidence — the `name: undefined` was silently ignored by the SDK's watch mechanism.

**Investigation iterations:**

1. **Attempt 1 — Re-read `activeExecution` at click time:** Modified the banner's click handler to re-read `plan.status?.activeExecution` fresh. Result: no change; the URL pushed was already correct. The problem was on the receiving end.

2. **Attempt 2 — Accept `match` prop:** Changed `ExecutionDetailPage` to accept a `match` prop (React Router v5 passes route props to components rendered via `<Route component={...}>`). Result: `props.match` was also `undefined`, confirming the Console doesn't use React Router's `<Route>` to render plugin components.

3. **Attempt 3 — Pathname extraction fallback:** Added `window.location.pathname.split('/').pop()` as a third fallback after `match.params.name` and `useParams().name`. Result: **success** — the execution name was correctly extracted from the URL.

**Fix:** Created a shared `useRouteParamName` hook with a three-level fallback strategy:

```typescript
export function useRouteParamName(match?: { params?: { name?: string } }): string | undefined {
  const routerParams = useParams<{ name: string }>();
  return match?.params?.name ?? routerParams?.name ?? window.location.pathname.split('/').pop() ?? undefined;
}
```

| File | Change |
|------|--------|
| `console-plugin/src/hooks/useRouteParamName.ts` | New shared hook: tries `match.params.name`, then `useParams().name`, then pathname extraction |
| `console-plugin/src/components/ExecutionDetail/ExecutionDetailPage.tsx` | Uses `useRouteParamName(props.match)` instead of `useParams()` |
| `console-plugin/src/components/DRPlanDetail/DRPlanDetailPage.tsx` | Uses `useRouteParamName(props.match)` instead of `useParams()` — fixes the latent bug where `name` was always `undefined` |

**Status:** Verified working. Both plan detail and execution detail pages correctly extract the resource name from the URL.

**Key insight for future Console plugin development:** Never rely on `useParams()` in `console.page/route` components. Use `window.location.pathname` extraction or check if the Console passes `match` as a component prop.

### Issue 8: "Triggered By" column shows "N/A" — mutating webhook does not intercept aggregated API requests

**Symptom:** After triggering a failback from the console, the History tab's "Triggered By" column showed "N/A" for all executions, including the one just created. The `soteria.io/triggered-by` annotation was absent from the `DRExecution` resource.

**Root Cause:** A `MutatingWebhookConfiguration` was created at the kube-apiserver level to stamp the annotation via `req.UserInfo.Username`. However, `DRExecution` resources are served by the Soteria **aggregated API server**, and the kube-apiserver **proxies requests to aggregated APIs without running its admission webhook chain**. The mutating webhook endpoint was never called.

Additionally, the `failurePolicy: Fail` on the non-functional `MutatingWebhookConfiguration` caused a secondary regression: the "View execution details" link stopped working, sending users back to the DRPlan list. Removing the webhook configuration resolved both issues simultaneously.

**Investigation iterations:**

1. **Attempt 1 — MutatingWebhookConfiguration:** Created a `MutatingWebhookConfiguration` with a handler that read `req.UserInfo.Username` and returned a JSON patch adding the annotation. Webhook config, cert-manager CA injection, and webhook handler all deployed correctly. Tests passed locally. Result: annotation never stamped; webhook endpoint never called by the kube-apiserver for aggregated API resources.

2. **Attempt 2 — PrepareForCreate in registry strategy:** Moved the annotation stamping into the aggregated API server's `PrepareForCreate` method using `request.UserFrom(ctx)` from `k8s.io/apiserver/pkg/endpoints/request`. This runs inside the aggregated API server's request processing pipeline and has access to the authenticated user identity for every creation path. Result: **success** — annotation stamped correctly.

**Fix:**

| File | Change |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Added `TriggeredByAnnotation` constant (`soteria.io/triggered-by`) |
| `pkg/registry/drexecution/strategy.go` | `PrepareForCreate` stamps annotation with `user.GetName()` from `request.UserFrom(ctx)` |
| `pkg/registry/drexecution/strategy_test.go` | 4 tests: stamps annotation, preserves existing annotations, overwrites client-supplied values (anti-spoofing), skips when no user in context |

**Key insight for aggregated API server development:** Kubernetes admission webhooks (`MutatingWebhookConfiguration`, `ValidatingWebhookConfiguration`) do **not** intercept requests to aggregated API servers. The kube-apiserver proxies requests directly to the extension API server without running its webhook admission chain. For server-side mutation of aggregated API resources, use the registry strategy's `PrepareForCreate`/`PrepareForUpdate` methods with `request.UserFrom(ctx)` to access the authenticated user identity. Note: the existing `ValidatingWebhookConfiguration` entries for DRExecution, DRPlan, and VirtualMachine work because they are registered on the **controller-runtime webhook server** (a separate HTTPS server in the same pod), not through the kube-apiserver's admission chain.

**Status:** Verified working. New executions show the authenticated username in the History tab's "Triggered By" column.

## Summary

Sprint 7 UAT on the live OCP 4.20 stretched cluster uncovered 8 issues across UI, controller, and API layers. The most significant findings were:

1. **OpenShift Console SDK limitation — watch** (Issue 2): `useK8sWatchResource` does not support reliable single-resource WebSocket watches against aggregated API servers, requiring a list-based fallback.
2. **Misplaced cluster-wide data** (Issue 3): Per-plan `UnprotectedVMs` caused a 30-second infinite status patch loop and UI flicker. Removed entirely — cluster-wide data belongs in a dedicated UI tab, not per-plan status.
3. **Dead RPO code** (Issue 4): The `estimatedRPO` field was never populated by any real driver, yet had extensive UI rendering code. Full end-to-end removal across Go types, controller, metrics, drivers, and 7+ UI components.
4. **Imprecise health semantics** (Issue 5): Non-replicating volumes reported `Unknown` (implying an error) instead of the intentional `NotReplicating`. New neutral status prevents false degraded conditions and unnecessary requeue pressure.
5. **Stale closure navigation bug** (Issue 6): A race between plan watch updates and user clicks caused the banner link to navigate with `undefined`. Fixed with render-time path capture and a redirect guard.
6. **OpenShift Console SDK limitation — routing** (Issue 7): `useParams()` does not work in `console.page/route` plugin components because the SDK renders them outside a React Router `<Route>` context. Fixed with pathname extraction fallback. This was a latent bug affecting all pages but only manifested on the execution detail page (where the name was essential for data lookup).
7. **Aggregated API admission limitation** (Issue 8): `MutatingWebhookConfiguration` does not intercept requests to aggregated API servers. The `triggered-by` annotation must be stamped in the registry's `PrepareForCreate` using `request.UserFrom(ctx)`, not via a webhook. This also applies to any future server-side mutation of aggregated API resources.
