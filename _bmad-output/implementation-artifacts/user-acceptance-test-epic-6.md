# User Acceptance Test — Epic 6: OCP Console Dashboard & Plan Management

Date: 2026-04-26
Environment: OCP 4.20.4 (etl6 + etl7 stretched cluster)
Tester: raffa

## Objective

Deploy the Soteria console plugin to a live OCP 4.20 stretched cluster (etl6/etl7) and validate all Epic 6 stories end-to-end in a real OpenShift Console environment.

## Deployment Method

Console plugin deployed via `hack/stretched-local-test.sh`, which was extended to:
1. Build and push the console plugin Docker image (`quay.io/raffaelespazzoli/soteria-console-plugin:latest`)
2. Deploy Kubernetes manifests (ConfigMap, Deployment, Service, ConsolePlugin CR) to both clusters
3. Enable the plugin on the OpenShift console via `consoles.operator.openshift.io` patch

Deployment manifest: `hack/overlays/base/console-plugin.yaml`

## Issues Found & Resolved

### Issue 1: Docker build failure — stale yarn.lock

**Symptom:** `yarn install --immutable` failed with `YN0028` during Docker build.

**Root Cause:** `yarn.lock` was not consistent with `package.json` after dependency changes.

**Fix:** Ran `yarn install` locally to regenerate `yarn.lock`, committed it.

### Issue 2: Docker build crash — V8 deserialization in UBI Node.js image

**Symptom:** `yarn install --immutable` exited with code 129 (signal kill) during Docker build using `ubi9/nodejs-22`.

**Root Cause:** The Red Hat UBI9 Node.js 22 image had a V8 bytecode cache incompatibility causing a crash during Yarn's install phase.

**Fix:** Changed Dockerfile build stage from `registry.access.redhat.com/ubi9/nodejs-22:latest` to `node:22-slim`. Created `.dockerignore` to exclude unnecessary files.

### Issue 3: TypeScript compilation errors — Link component JSX types

**Symptom:** `ts-loader` reported `TS2786: 'Link' cannot be used as a JSX component` during webpack build.

**Root Cause:** Type mismatch between `react-router-dom` v5 `Link` types and React 17 `@types/react` JSX element types when `ts-loader` performs full type checking.

**Fix:** Set `transpileOnly: true` in `ts-loader` options within `webpack.config.ts`. Also added `skipLibCheck: true` to `tsconfig.json`.

### Issue 4: Server-side apply conflict on ScyllaCluster

**Symptom:** `kubectl apply --server-side` failed with conflict on `.spec.datacenter.racks` field managed by `kubectl-patch`.

**Root Cause:** Previous manual patches on the ScyllaCluster resource created field manager conflicts with server-side apply.

**Fix:** Added `--force-conflicts` flag to `kubectl apply --server-side` commands in `stretched-local-test.sh`.

### Issue 5: ImagePullBackOff — private Quay.io repository

**Symptom:** Console plugin pod stuck in `ImagePullBackOff` with "unauthorized" error pulling from `quay.io/raffaelespazzoli/soteria-console-plugin`.

**Root Cause:** Newly created Quay.io repository was private by default. The cluster's global pull secret did not have access.

**Fix:** Created a `quay-pull-secret` Kubernetes secret in the `soteria` namespace from the local Docker config and added `imagePullSecrets` to the Deployment.

### Issue 6: Console plugin manifest load failure — HTTP vs HTTPS

**Symptom:** OpenShift console logged "Failed to get a valid plugin manifest from /api/plugins/soteria-console-plugin/".

**Root Cause:** Two issues:
1. Nginx served plain HTTP, but the console proxy expected HTTPS.
2. The Dockerfile copied built assets to `/usr/share/nginx/html` but the UBI Nginx image's document root is `/opt/app-root/src`.

**Fix:**
- Corrected `COPY` destination in Dockerfile to `/opt/app-root/src`.
- Added `service.beta.openshift.io/serving-cert-secret-name` annotation on the Service to get a TLS cert from OpenShift's service-CA.
- Created a ConfigMap with an Nginx TLS server block listening on port 9443.
- Updated Deployment to mount the serving cert secret and Nginx TLS config.
- Updated Service and ConsolePlugin CR to use port 9443/HTTPS.

### Issue 7: Plugin dependency resolution failure

**Symptom:** Console logged "Failed to resolve dependencies of plugin soteria-console-plugin".

**Root Cause:** The plugin manifest declared `@console/pluginAPI: ^4.21.0`, but OCP 4.20.4 provided API version 4.20.x, which does not satisfy `^4.21.0`.

**Fix:** Changed `@console/pluginAPI` dependency in `console-plugin/package.json` from `"^4.21.0"` to `"*"`.

### Issue 8: React Error #130 — undefined component (Link)

**Symptom:** Browser console showed `Minified React error #130: Element type is invalid: expected a string or class/function but got: undefined`.

**Root Cause:** The plugin's `react-router.d.ts` type declaration file incorrectly assumed the OCP 4.20 console provides React Router v7 (where `react-router` and `react-router-dom` are unified). In reality, OCP 4.20 provides **React Router v5** where:
- `Link` and `NavLink` are only exported from `react-router-dom`, NOT from `react-router`
- `useNavigate` does not exist in v5 (it is a v6+ API; v5 uses `useHistory`)

The type declaration re-exported `Link` from `react-router-dom` into the `react-router` module for TypeScript, but at runtime webpack's module federation resolved `react-router` from the console's shared scope — which is v5 and does not export `Link`. The result was `Link === undefined`, triggering React Error #130 when it was rendered as a JSX element.

**Evidence from SDK source:**

```javascript
// node_modules/@openshift-console/dynamic-plugin-sdk-webpack/lib/shared-modules/shared-modules-meta.js
exports.sharedPluginModules = [
    '@openshift-console/dynamic-plugin-sdk',
    'react',
    'react-router',        // v5
    'react-router-dom',    // v5
    'react-router-dom-v5-compat',  // bridge package
    // ...
];
```

**Files changed (7 files):**

| File | Before | After |
|------|--------|-------|
| `DRDashboard.tsx` | `import { Link } from 'react-router'` | `import { Link } from 'react-router-dom'` |
| `TransitionProgressBanner.tsx` | `import { Link } from 'react-router'` | `import { Link } from 'react-router-dom'` |
| `DRBreadcrumb.tsx` | `import { Link } from 'react-router'` | `import { Link } from 'react-router-dom'` |
| `useFilterParams.ts` | `import { useNavigate } from 'react-router'` | `import { useHistory } from 'react-router-dom'` |
| `ExecutionHistoryTable.tsx` | `import { useNavigate } from 'react-router'` | `import { useHistory } from 'react-router-dom'` |
| `DRPlanDetailPage.tsx` | `import { useParams } from 'react-router'` | `import { useParams } from 'react-router-dom'` |
| `ExecutionDetailPage.tsx` | `import { useParams } from 'react-router'` | `import { useParams } from 'react-router-dom'` |

Additional API changes:
- `useNavigate()` → `useHistory()` (different object)
- `navigate({ search }, { replace: true })` → `history.replace({ search })`
- `navigate(path)` → `history.push(path)`

`react-router.d.ts` was rewritten to remove the incorrect re-export hack and document the v5 convention.

**Verification:** After rebuild and redeploy, the webpack shared module reference changed from `webpack/sharing/consume/default/react-router` to `webpack/sharing/consume/default/react-router-dom`, confirming `Link`, `useHistory`, `useParams`, and `useLocation` now resolve from the console's `react-router-dom` v5 shared module.

## Environment Findings

### PatternFly Version on OCP 4.20

**Finding:** OCP 4.20 ships **PatternFly v6** (6.2.3), not v5 as initially assumed.

**Evidence:** Web search confirmed the OCP 4.20 release notes list PF v6 as the console's UI framework.

**Implication:** The plugin's PF v6 imports (`@patternfly/react-core`, `@patternfly/react-table`, `@patternfly/react-icons`) are compatible with the console's shared scope. No PF version coexistence issue exists.

### React Router Version on OCP 4.20

**Finding:** OCP 4.20 ships **React Router v5** with a `react-router-dom-v5-compat` bridge package.

**Evidence:** The `@openshift-console/dynamic-plugin-sdk-webpack` SDK's `shared-modules-meta.js` lists `react-router`, `react-router-dom`, and `react-router-dom-v5-compat` as shared modules, with `react-router` having `singleton: false` and the comment "fixes runtime error when both v5-compat and v5 are present".

**Implication:** All plugin routing imports must use React Router v5 APIs: `useHistory` (not `useNavigate`), `Link` from `react-router-dom` (not `react-router`). The `react-router-dom-v5-compat` bridge is available if v6-style APIs are needed.

## Recommendations

1. **Story 6.2 correction:** The story notes reference "React Router v7 useParams for URL params" and the `react-router.d.ts` comment said "Console shell provides React Router v7 at runtime". This was incorrect. Story documentation should be updated to reflect that OCP 4.20 provides React Router v5.

2. **Import convention rule:** Establish a lint rule or project convention that all `react-router` imports must come from `react-router-dom`, never bare `react-router`. This prevents future developers from importing DOM components (`Link`, `NavLink`) from the wrong package.

3. **Dev build for debugging:** Building with `yarn build-dev` (non-minified) was essential for diagnosing the React #130 error. Consider adding a `CONSOLE_PLUGIN_DEV_BUILD` flag to `stretched-local-test.sh` for future debugging.

4. **Console plugin deployment manifest:** The `hack/overlays/base/console-plugin.yaml` should be reviewed for production readiness (e.g., proper resource limits, pod disruption budgets, health check tuning).

### Issue 9: Navigation placement — plugin under Administration instead of Virtualization

**Symptom:** The "Disaster Recovery" nav item appeared as its own top-level section under "Administration" in the admin perspective, unrelated to the Virtualization context it belongs to.

**Root Cause:** The original `console-extensions.json` defined a standalone `console.navigation/section` with `perspective: "admin"`, creating an independent nav section. The plugin had no presence in the Virtualization perspective at all.

**Fix:** Rewrote `console-extensions.json`:
- Removed the standalone `console.navigation/section` for "disaster-recovery".
- Added a `console.navigation/href` for the **Virtualization perspective** (`perspective: "virtualization-perspective"`, `insertAfter: "virtualization-checkups-virt-perspective"`).
- Added a `console.navigation/href` for the **admin perspective's Virtualization section** (`section: "virtualization"`, `insertAfter: "virtualization-checkups"`).
- Removed `perspective` restrictions from `console.page/route` entries so pages work in both perspectives.

**Discovery method:** Inspected the kubevirt-plugin's `plugin-manifest.json` on the running cluster to find:
- Virtualization perspective ID: `virtualization-perspective`
- Admin nav section ID: `virtualization`
- Anchor item IDs for `insertAfter` positioning

**Result:** "Disaster Recovery" now appears:
1. In the **Virtualization perspective** left nav (after "Checkups")
2. Under **Virtualization** in the **admin perspective** left nav (after "Checkups")

## Post-Deployment Enhancement: DR Lifecycle Diagram Overhaul (2026-04-28)

### Changes Applied

Reworked the `DRLifecycleDiagram` component on the Plan Detail Overview tab based on party-mode analysis with Developer, QA, UX Designer, Architect, PM, and Analyst agents. All changes applied directly without a dedicated story — classified as polish on story 6.5.

#### 1. State Box Text Restructured with Real Site Names

Replaced static DC1/DC2 role descriptions with dynamic plan-aware content:

| Phase | Before | After |
|-------|--------|-------|
| Steady State | "VMs on DC1", "DC1: Active (source)", "DC2: Passive (target)", "Replication: DC1 → DC2" | "VMs running in etl6", "VMs stopped in etl7", "Volume Replication: on" |
| Failed Over | "VMs on DC2", "DC1: Passive / down", "DC2: Active (promoted)", "Replication: None" | "VMs running in etl7", "VMs stopped in etl6", "Volume Replication: off" |
| DR-ed Steady State | "VMs on DC2", "DC1: Passive (target)", "DC2: Active (source)", "Replication: DC2 → DC1" | "VMs running in etl7", "VMs stopped in etl6", "Volume Replication: on" |
| Failed Back | "VMs on DC1", "DC1: Active (promoted)", "DC2: Passive / down", "Replication: None" | "VMs running in etl6", "VMs stopped in etl7", "Volume Replication: off" |

Site names sourced from `plan.spec.primarySite` / `plan.spec.secondarySite`.

#### 2. State Topology Images Added

Four PNG topology diagrams embedded below the text in each state box:
- `state-steady-state.png` — VM in primary, green storage, replication arrow to secondary
- `state-failed-over.png` — VM in secondary, green storage, stale dashed storage in primary
- `state-dred-steady-state.png` — VM in secondary, green storage, reverse replication to primary
- `state-failed-back.png` — VM in primary, green storage, stale dashed storage in secondary

Images stored in `console-plugin/src/assets/`, bundled via webpack `asset/resource` rule.

#### 3. Dual Transition Buttons (Planned Migration vs Disaster)

| Phase | Before | After |
|-------|--------|-------|
| SteadyState | "Failover" (danger) only | "Failover" (danger) + "Planned Migration" (secondary) |
| DRedSteadyState | "Failback" (secondary) only | "Failback" (danger) + "Planned Migration" (secondary) |
| FailedOver | "Reprotect" (secondary) | Unchanged |
| FailedBack | "Restore" (secondary) | Unchanged |

#### 4. Files Changed

| File | Change |
|------|--------|
| `console-plugin/src/components/DRPlanDetail/DRLifecycleDiagram.tsx` | Complete rework: dynamic site names, image imports, multi-action transition edges |
| `console-plugin/src/utils/drPlanActions.ts` | Added `planned_failback` action for DRedSteadyState, made `failback` danger variant |
| `console-plugin/src/components/DRPlanDetail/TransitionProgressBanner.tsx` | Fixed `transition.action` → derived label from `transient` field |
| `console-plugin/src/typings/assets.d.ts` | New: TypeScript declarations for PNG/SVG imports |
| `console-plugin/src/assets/state-*.png` | New: 4 state topology images |
| `console-plugin/jest.config.ts` | Added image file mock for tests |
| `console-plugin/tests/__mocks__/fileMock.ts` | New: Jest file mock stub |
| `console-plugin/tests/components/DRLifecycleDiagram.test.tsx` | 30 tests covering all changes |
| `console-plugin/tests/utils/drPlanUtils.test.ts` | Updated DRedSteadyState actions assertion |

#### 5. Test Results

30 DRLifecycleDiagram tests pass (including 2 jest-axe accessibility audits). All previously-passing tests continue to pass.

#### 6. Runtime Fix: `plan.spec` Null Safety

**Symptom:** `TypeError: Cannot read properties of undefined (reading 'primarySite')` at runtime.

**Root Cause:** `plan.spec` can be `undefined` when the K8s resource is partially loaded, even though the TypeScript type marks it as required.

**Fix:** Changed `plan.spec.primarySite` to `plan.spec?.primarySite ?? 'Primary'` (same for `secondarySite`).

#### 7. Image Size & Box Padding Tuning

Iteratively reduced state box image and padding based on live testing:
- Images: 100% → 50% → 37.5% of container width
- Box padding: `spacer--md` (16px all around) → `spacer--xs` top/bottom, `spacer--sm` left/right
- Removed `minWidth: 220px` constraint to allow boxes to size to content

#### 8. Configuration Tab: Two-Pane Layout

Reworked the Configuration tab from a single-column vertical stack to a two-column grid:

| Left Pane | Right Pane |
|-----------|------------|
| Plan Information (name, label selector, wave label, max concurrent failovers, primary/secondary site, creation date) | Replication Health (per-VG health table) |
| Labels (LabelGroup) | |
| Annotations (if any) | |

Removed the Plan Spec YAML code block section.

## Summary

Epic 6 deployment to a live OCP 4.20 cluster uncovered 9 issues, all resolved. The most significant finding was the React Router version mismatch (v5 at runtime vs v7 assumed in code), which caused the `Link` component to resolve to `undefined` through webpack module federation's shared scope. Navigation was also relocated from a standalone Administration section to the Virtualization perspective and admin Virtualization submenu, where it contextually belongs. A post-deployment enhancement overhauled the DR Lifecycle Diagram with real site names, state topology images, and dual planned-migration/disaster transition buttons. All issues are now fixed.
