# Story 15.7: Console Plugin Standalone Mode

Status: ready-for-dev

## Story

As a developer,
I want the OCP console plugin to also run as a standalone web application with direct K8s API access,
so that the DR management UI can be used in Minikube and non-OCP environments.

## Acceptance Criteria

**AC1: Provider abstraction**
Given the existing console plugin hooks in `console-plugin/src/hooks/`
When the code is refactored
Then a provider interface abstracts the data access layer
And `providers/ocp.ts` wraps the existing `@openshift-console/dynamic-plugin-sdk` calls
And `providers/standalone.ts` implements the same interface using raw `fetch()` against the K8s API

**AC2: Build-time configuration**
Given the console plugin build system
When `make console-standalone` is run
Then a standalone SPA is built using a separate webpack config (`webpack.standalone.ts`)
And the OCP SDK dependencies are not bundled (they don't exist in standalone mode)
And the standalone build produces a self-contained `dist-standalone/` directory

**AC3: Standalone entry point**
Given the standalone build
When the application starts
Then `standalone/index.html` loads `main.tsx` with React + React Router (BrowserRouter)
And all existing routes (Dashboard, PlanDetail, ExecutionDetail) are accessible
And PatternFly styles are loaded directly (not via OCP Console host)

**AC4: K8s API authentication**
Given the standalone application deployed in-cluster
When it connects to the K8s API
Then it uses a mounted ServiceAccount token for authentication
And API requests are proxied through a lightweight Go reverse proxy
And watch/list/get operations function identically to the OCP plugin

**AC5: Minikube deployment**
Given the standalone console build
When deployed in the Minikube test cluster
Then a Deployment + Service + ServiceAccount are created in a `soteria-console` namespace
And the UI is accessible via MetalLB LoadBalancer IP or `minikube service`
And RBAC grants read access to soteria.io resources and KubeVirt VMs

**AC6: Runtime detection**
Given the console plugin code
When running in OCP Console host
Then the OCP provider is used automatically
When running standalone
Then the standalone provider is used
And detection is via build-time `__STANDALONE__` flag (DefinePlugin)

**AC7: Existing tests pass**
Given the provider refactoring
When existing Jest tests are run
Then all existing tests continue to pass
And the mock infrastructure targets the provider interface (not SDK directly)

## Tasks / Subtasks

- [ ] Task 1: Create provider abstraction layer (AC: 1, 6)
  - [ ] 1.1: Create `console-plugin/src/providers/types.ts` — define `K8sProvider` interface with methods: `watchResource<T>(gvk, opts) → [T[], boolean, unknown]`, `createResource<T>(model, data) → Promise<T>`, `patchResource<T>(model, resource, patches) → Promise<T>`, `getDocumentTitle() → React.FC<{children: string}>`
  - [ ] 1.2: Create `console-plugin/src/providers/ocp.ts` — implement `K8sProvider` wrapping `useK8sWatchResource`, `k8sCreate`, `k8sPatch`, `DocumentTitle` from `@openshift-console/dynamic-plugin-sdk`
  - [ ] 1.3: Create `console-plugin/src/providers/standalone.ts` — implement `K8sProvider` using raw `fetch()` against `/api/k8s/` proxy path with watch, list, get, create, patch operations
  - [ ] 1.4: Create `console-plugin/src/providers/context.tsx` — React context providing the active `K8sProvider`, auto-selects OCP or standalone based on `__STANDALONE__` build flag
  - [ ] 1.5: Create `console-plugin/src/providers/index.ts` — re-exports context, types, and provider hooks

- [ ] Task 2: Refactor existing hooks to use provider (AC: 1, 7)
  - [ ] 2.1: Refactor `useDRResources.ts` — replace direct `useK8sWatchResource` calls with provider's `watchResource`, keep identical return types
  - [ ] 2.2: Refactor `useCreateDRExecution.ts` — replace `k8sCreate` with provider's `createResource`
  - [ ] 2.3: Refactor `useRetryDRGroup.ts` — replace `k8sPatch` with provider's `patchResource`
  - [ ] 2.4: Refactor page components — replace `DocumentTitle` import from SDK with provider's `DocumentTitle` component (`DRDashboardPage.tsx`, `DRPlanDetailPage.tsx`, `ExecutionDetailPage.tsx`)
  - [ ] 2.5: Update all test mocks — mock the provider context instead of SDK directly; existing assertions remain unchanged

- [ ] Task 3: Create standalone webpack config (AC: 2)
  - [ ] 3.1: Create `console-plugin/webpack.standalone.ts` — standard webpack config (no `ConsoleRemotePlugin`), entry point `standalone/main.tsx`, output to `dist-standalone/`, resolve same extensions
  - [ ] 3.2: Configure `DefinePlugin` — set `__STANDALONE__: true` in standalone config, `__STANDALONE__: false` in existing OCP config
  - [ ] 3.3: Configure `HtmlWebpackPlugin` — generate `index.html` from `standalone/index.html` template
  - [ ] 3.4: Include PatternFly CSS — import `@patternfly/react-core/dist/styles/base.css` and `@patternfly/patternfly/patternfly.min.css` in standalone entry
  - [ ] 3.5: Add `externals` for `@openshift-console/dynamic-plugin-sdk` — empty module in standalone build (tree-shaking removes it since provider routes around it)
  - [ ] 3.6: Add `console-standalone` script to `package.json` and `Makefile` target

- [ ] Task 4: Create standalone entry point (AC: 3)
  - [ ] 4.1: Create `console-plugin/standalone/index.html` — minimal HTML with `<div id="root">`, PatternFly CSS links, viewport meta
  - [ ] 4.2: Create `console-plugin/standalone/main.tsx` — render `<BrowserRouter>` with `<Switch>` routing to DRDashboardPage, DRPlanDetailPage, ExecutionDetailPage, wrapped in provider context
  - [ ] 4.3: Create `console-plugin/standalone/StandaloneApp.tsx` — app shell with PatternFly `Page` layout, sidebar navigation matching OCP routes (`/disaster-recovery`, `/disaster-recovery/plans/:name`, `/disaster-recovery/executions/:name`)
  - [ ] 4.4: Add PatternFly `@patternfly/patternfly` as a dependency (dev peer — OCP Console provides it for plugin mode; standalone needs it bundled)

- [ ] Task 5: Create Go reverse proxy (AC: 4)
  - [ ] 5.1: Create `cmd/console-proxy/main.go` — HTTP server that serves static SPA from `/` and proxies `/api/k8s/` to the K8s API server
  - [ ] 5.2: Implement K8s API proxy — read ServiceAccount token from `/var/run/secrets/kubernetes.io/serviceaccount/token`, inject `Authorization: Bearer <token>` header, proxy to `KUBERNETES_SERVICE_HOST:KUBERNETES_SERVICE_PORT`
  - [ ] 5.3: Implement SPA fallback — all non-`/api/k8s/` GET requests serve `index.html` (client-side routing support)
  - [ ] 5.4: Configure TLS — proxy connects to K8s API using the CA cert from `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`
  - [ ] 5.5: Add health endpoint — `GET /healthz` returns 200

- [ ] Task 6: Create Dockerfile and deployment manifests (AC: 5)
  - [ ] 6.1: Create `console-plugin/Dockerfile.standalone` — multi-stage: Node build stage produces `dist-standalone/`, Go build stage compiles `cmd/console-proxy`, final stage copies both into a `scratch`/`distroless` image
  - [ ] 6.2: Create `hack/overlays/base/console-standalone.yaml` — Deployment + Service + ServiceAccount + ClusterRole + ClusterRoleBinding for Minikube deployment
  - [ ] 6.3: RBAC in ClusterRole — `get`, `list`, `watch` on `soteria.io` DRPlan and DRExecution resources; `get`, `list`, `watch` on `kubevirt.io` VirtualMachine resources
  - [ ] 6.4: Service type `LoadBalancer` for MetalLB access; port 8080

- [ ] Task 7: Update existing OCP webpack config (AC: 6)
  - [ ] 7.1: Add `DefinePlugin` to existing `webpack.config.ts` — set `__STANDALONE__: false`
  - [ ] 7.2: Add `declare const __STANDALONE__: boolean;` to a global types file (`src/typings/globals.d.ts`)

- [ ] Task 8: Tests (AC: 7)
  - [ ] 8.1: Run existing test suite — verify all pass with refactored provider abstraction
  - [ ] 8.2: Add provider abstraction tests — test OCP provider delegates to SDK mocks, test standalone provider issues correct fetch calls
  - [ ] 8.3: Add standalone webpack build smoke test — `yarn console-standalone` completes without errors, `dist-standalone/index.html` exists
  - [ ] 8.4: Add Go proxy unit tests — test static file serving, API proxy path rewriting, token injection, SPA fallback

## Dev Notes

### OCP SDK Dependency Surface (8 files import from SDK)

Files that import from `@openshift-console/dynamic-plugin-sdk`:

| File | Imports Used |
|------|-------------|
| `src/models/types.ts` | `K8sResourceCommon` (type only) |
| `src/models/k8sModels.ts` | `K8sModel` (type only) |
| `src/hooks/useDRResources.ts` | `K8sGroupVersionKind`, `useK8sWatchResource`, `WatchK8sResource` |
| `src/hooks/useCreateDRExecution.ts` | `k8sCreate` |
| `src/hooks/useRetryDRGroup.ts` | `k8sPatch` |
| `src/components/DRDashboard/DRDashboardPage.tsx` | `DocumentTitle` |
| `src/components/DRPlanDetail/DRPlanDetailPage.tsx` | `DocumentTitle` |
| `src/components/ExecutionDetail/ExecutionDetailPage.tsx` | `DocumentTitle` |

Type-only imports (`K8sResourceCommon`, `K8sModel`, `K8sGroupVersionKind`, `WatchK8sResource`) can remain as-is since TypeScript strips them at build time. The refactoring targets the 5 runtime imports: `useK8sWatchResource`, `k8sCreate`, `k8sPatch`, and 3x `DocumentTitle`.

### Provider Interface Design

```typescript
interface K8sProvider {
  useWatchResource<T>(gvk: K8sGroupVersionKind, opts?: WatchOpts): [T[], boolean, unknown];
  createResource<T>(model: K8sModel, data: object): Promise<T>;
  patchResource<T>(model: K8sModel, resource: { metadata: { name: string } }, patches: object[]): Promise<T>;
  DocumentTitle: React.FC<{ children: string }>;
}

interface WatchOpts {
  name?: string;
  isList?: boolean;
  selector?: { matchLabels?: Record<string, string> };
}
```

The OCP provider wraps existing SDK calls. The standalone provider uses `fetch()` with:
- List: `GET /api/k8s/apis/{group}/{version}/{plural}`
- Watch: `GET /api/k8s/apis/{group}/{version}/{plural}?watch=true` (EventSource or WebSocket)
- Get: `GET /api/k8s/apis/{group}/{version}/{plural}/{name}`
- Create: `POST /api/k8s/apis/{group}/{version}/{plural}`
- Patch: `PATCH /api/k8s/apis/{group}/{version}/{plural}/{name}` with `Content-Type: application/json-patch+json`

### Standalone Provider Watch Implementation

Use the Kubernetes watch API with `fetch()` streaming:

```typescript
const response = await fetch(`/api/k8s/apis/${group}/${version}/${plural}?watch=true`);
const reader = response.body!.getReader();
const decoder = new TextDecoder();
```

Parse newline-delimited JSON (NDJSON) watch events (`ADDED`, `MODIFIED`, `DELETED`) and update a local cache. Expose via React state so `useWatchResource` returns `[items, loaded, error]` matching the OCP SDK signature.

### Go Reverse Proxy Architecture

```
Browser ──► console-proxy (port 8080)
              ├── GET /api/k8s/* ──► K8s API (in-cluster, with SA token)
              ├── GET /healthz   ──► 200 OK
              └── GET /*         ──► dist-standalone/ static files (SPA fallback to index.html)
```

Use `net/http/httputil.ReverseProxy` for the K8s API proxying. The proxy:
- Strips the `/api/k8s` prefix before forwarding
- Injects `Authorization: Bearer <token>` from mounted ServiceAccount
- Uses in-cluster CA cert for TLS to K8s API
- Handles WebSocket upgrade for watch connections

### PatternFly CSS Handling

OCP Console automatically injects PatternFly CSS for all plugins. Standalone mode must import it explicitly:

```typescript
// standalone/main.tsx
import '@patternfly/patternfly/patternfly.min.css';
import '@patternfly/react-core/dist/styles/base.css';
```

The OCP plugin build must NOT include these imports (double-loading causes CSS conflicts). Use the `__STANDALONE__` build flag to conditionally import.

### Routing Compatibility

Current routes in `console-extensions.json`:
- `/disaster-recovery` → `DRDashboardPage`
- `/disaster-recovery/plans/:name` → `DRPlanDetailPage`
- `/disaster-recovery/executions/:name` → `ExecutionDetailPage`

Standalone mode uses `react-router-dom` v5 (already a devDependency at `5.3.x`), same version as OCP Console. The `<BrowserRouter>` in `standalone/main.tsx` registers identical route paths. The existing `useRouteParamName` hook already handles multiple param extraction strategies (match props, useParams, pathname parsing) — it will work in both modes without changes.

### Existing Test Mock Pattern

Tests currently mock the SDK globally:
```typescript
jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }) => <title>{children}</title>,
  useK8sWatchResource: jest.fn(() => [[], true, null]),
}));
```

After refactoring, tests should mock the provider context:
```typescript
jest.mock('../../src/providers', () => ({
  useProvider: () => mockProvider,
}));
```

This is a mechanical change — find-and-replace the SDK mock with provider mock in all test files.

### File Structure

```
console-plugin/
├── src/
│   ├── providers/               ← NEW
│   │   ├── types.ts             ← K8sProvider interface
│   │   ├── ocp.ts               ← OCP SDK wrapper
│   │   ├── standalone.ts        ← fetch()-based implementation
│   │   ├── context.tsx           ← React context + auto-detection
│   │   └── index.ts             ← re-exports
│   ├── hooks/                   ← MODIFIED (use provider instead of SDK)
│   ├── components/              ← MODIFIED (DocumentTitle from provider)
│   ├── models/                  ← UNCHANGED (type-only SDK imports stay)
│   └── typings/
│       └── globals.d.ts         ← NEW (__STANDALONE__ declaration)
├── standalone/                  ← NEW
│   ├── index.html               ← HTML template
│   ├── main.tsx                 ← React entry + BrowserRouter
│   └── StandaloneApp.tsx        ← App shell with PatternFly Page layout
├── webpack.config.ts            ← MODIFIED (add DefinePlugin)
├── webpack.standalone.ts        ← NEW
├── Dockerfile                   ← UNCHANGED (OCP plugin build)
├── Dockerfile.standalone        ← NEW
└── package.json                 ← MODIFIED (add scripts)

cmd/
└── console-proxy/               ← NEW
    └── main.go                  ← Go reverse proxy

hack/overlays/base/
└── console-standalone.yaml      ← NEW (Minikube deployment manifests)
```

### Critical Constraints

1. **Do NOT modify the existing OCP plugin build path.** The `ConsoleRemotePlugin` webpack plugin, `console-extensions.json`, and `Dockerfile` must remain untouched. The standalone mode is an additive, parallel build target.
2. **Type-only imports from SDK stay.** `K8sResourceCommon`, `K8sModel`, etc. are TypeScript types stripped at compile time. They cause no runtime dependency.
3. **React 17 + react-router v5 — do NOT upgrade.** The OCP Console SDK pins React 17 and react-router 5. Standalone mode must use the same versions for component compatibility.
4. **The Go proxy is minimal.** It serves static files and proxies K8s API requests. No business logic, no caching, no transformation. ~150 lines of Go.
5. **No WebSocket in MVP.** For the standalone watch implementation, start with periodic polling (list with `resourceVersion`) as a simpler alternative to streaming. If performance is a concern, add WebSocket/streaming in a follow-up.

### Project Structure Notes

- The `cmd/console-proxy/` package follows the existing `cmd/soteria/` pattern for Go binaries
- The `console-plugin/standalone/` directory is scoped under `console-plugin/` to share `src/` imports
- Deployment manifests follow the existing pattern in `hack/overlays/base/console-plugin.yaml`
- The `Dockerfile.standalone` is separate from `Dockerfile` (OCP plugin) to avoid cross-contamination

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.7] — Story definition, BDD acceptance criteria, and technical notes
- [Source: _bmad-output/planning-artifacts/architecture.md] — Console plugin architecture, PatternFly 6, OCP Dynamic Plugin SDK
- [Source: console-plugin/webpack.config.ts] — Current OCP webpack config with `ConsoleRemotePlugin`
- [Source: console-plugin/src/hooks/useDRResources.ts] — Primary data access hook using `useK8sWatchResource`
- [Source: console-plugin/src/hooks/useCreateDRExecution.ts] — `k8sCreate` usage for DRExecution creation
- [Source: console-plugin/src/hooks/useRetryDRGroup.ts] — `k8sPatch` usage for retry annotation
- [Source: console-plugin/console-extensions.json] — Route definitions for OCP Console
- [Source: console-plugin/package.json] — Dependencies: PatternFly 6.2.2, React 17, react-router 5.3.x
- [Source: console-plugin/Dockerfile] — Current OCP build: Node 22 + nginx serving
- [Source: hack/overlays/base/console-plugin.yaml] — OCP deployment manifest pattern
- [Source: console-plugin/tests/components/DRDashboardPage.test.tsx] — Current SDK mock pattern in tests
- [Source: console-plugin/src/hooks/useRouteParamName.ts] — Multi-strategy route param extraction (already standalone-compatible)
- [Source: _bmad-output/planning-artifacts/prd.md] — OCP Console plugin requirements: dashboard, plan management, replication health, live execution progress

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
