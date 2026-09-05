# Story 16.6: Standalone UI Templates

Status: review

## Story

As a platform engineer deploying Soteria on vanilla Kubernetes (non-OpenShift),
I want Helm templates for the standalone UI that are only rendered when `ui.mode=standalone`,
so that I can access the Soteria dashboard without requiring the OpenShift console.

## Acceptance Criteria

**AC1: Standalone UI Deployment renders in standalone mode**
Given `ui.mode=standalone`
When I run `helm template`
Then a Deployment running the console-proxy Go binary image is rendered with the image from `ui.standalone.image`

**AC2: Standalone UI Service exposes HTTP on 8080**
Given `ui.mode=standalone`
When I render the chart
Then a Service is rendered with HTTP on port 8080

**AC3: RBAC for K8s API access**
Given `ui.mode=standalone`
When I render the chart
Then a ServiceAccount, ClusterRole, and ClusterRoleBinding are rendered granting the standalone UI access to the Kubernetes API

**AC4: Gateway API HTTPRoute is conditional**
Given `ui.mode=standalone` and `ui.standalone.gateway.name` is set
When I render the chart
Then a Gateway API HTTPRoute resource is rendered referencing the specified gateway
When `ui.standalone.gateway.name` is not set
Then no HTTPRoute resource is rendered

**AC5: No standalone UI resources in other modes**
Given `ui.mode=console-plugin` or `ui.mode=none`
When I render the chart
Then no standalone UI Deployment, Service, RBAC, or HTTPRoute is rendered

## Tasks / Subtasks

- [x] Task 1: Create standalone UI Deployment template (AC: 1, 5)
  - [x] 1.1: Create `templates/standalone-ui/deployment.yaml` guarded by `{{- if eq .Values.ui.mode "standalone" }}`
  - [x] 1.2: Wire image from `ui.standalone.image.repository` and `ui.standalone.image.tag`
  - [x] 1.3: Set container port 8080 (HTTP) with `--addr=:8080` and `--static-dir=/opt/app-root/src` args
  - [x] 1.4: Set securityContext matching existing manifest: `runAsNonRoot`, `readOnlyRootFilesystem`, drop ALL capabilities
  - [x] 1.5: Add liveness/readiness probes against `/healthz` on port 8080
  - [x] 1.6: Set resources (requests: cpu 10m, mem 32Mi; limits: mem 64Mi)
- [x] Task 2: Create standalone UI Service template (AC: 2, 5)
  - [x] 2.1: Create `templates/standalone-ui/service.yaml` with HTTP on port 8080
- [x] Task 3: Create RBAC templates (AC: 3, 5)
  - [x] 3.1: Create `templates/standalone-ui/serviceaccount.yaml`
  - [x] 3.2: Create `templates/standalone-ui/clusterrole.yaml` mirroring rules from `hack/overlays/base/console-standalone.yaml` — read access to drplans, drexecutions, virtualmachines; create/patch on drexecutions
  - [x] 3.3: Create `templates/standalone-ui/clusterrolebinding.yaml`
- [x] Task 4: Create HTTPRoute template (AC: 4, 5)
  - [x] 4.1: Create `templates/standalone-ui/httproute.yaml` guarded by `ui.mode=standalone AND ui.standalone.gateway.name` being non-empty
  - [x] 4.2: Wire parentRef gateway name from `ui.standalone.gateway.name` and gatewayClassName from `ui.standalone.gateway.className`
  - [x] 4.3: Route all traffic (`path: /`, type: PathPrefix) to the standalone Service on port 8080
- [x] Task 5: Verify rendering (AC: 1–5)
  - [x] 5.1: Run `helm template` with `ui.mode=standalone, ui.standalone.gateway.name=my-gw` — verify all resources including HTTPRoute
  - [x] 5.2: Run `helm template` with `ui.mode=standalone, ui.standalone.gateway.name=""` — verify no HTTPRoute
  - [x] 5.3: Run `helm template` with `ui.mode=console-plugin` — verify no standalone resources
  - [x] 5.4: Run `helm template` with `ui.mode=none` — verify no standalone resources

## Dev Notes

### Implementation Pattern

The existing manifest at `hack/overlays/base/console-standalone.yaml` contains the complete reference implementation:

**RBAC (ClusterRole `soteria-console-reader`):**
```yaml
rules:
  - apiGroups: ["soteria.io"]
    resources: ["drplans", "drexecutions"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["soteria.io"]
    resources: ["drexecutions"]
    verbs: ["create", "patch"]
  - apiGroups: ["kubevirt.io"]
    resources: ["virtualmachines"]
    verbs: ["get", "list", "watch"]
```

**Deployment** — runs the `console-proxy` Go binary (from `Dockerfile.standalone`):
- Two-stage build: Node.js for SPA assets → Go for the proxy binary
- Go binary: `/console-proxy` with `--addr=:8080` and `--static-dir=/opt/app-root/src`
- Serves static SPA files and reverse-proxies `/api/k8s/` to the in-cluster Kubernetes API using SA token

The console-proxy (`cmd/console-proxy/main.go`) accepts flags:
- `--addr` (default `:8080`) — listen address
- `--static-dir` (default `/opt/app-root/src`) — SPA static files directory

The Gateway API manifest at `hack/multisite/manifests/console-gateway.yaml`:
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: soteria-console-gateway
spec:
  gatewayClassName: cilium
  listeners:
    - name: http
      protocol: HTTP
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: soteria-console-route
spec:
  parentRefs:
    - name: soteria-console-gateway
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: soteria-console-standalone
          port: 8080
```

Note: The Helm chart should only render the HTTPRoute, not the Gateway itself (the Gateway is infrastructure-level and managed separately). The Gateway name is provided via `ui.standalone.gateway.name`.

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `charts/soteria/templates/standalone-ui/deployment.yaml` | NEW | Console-proxy Deployment, conditional on `ui.mode=standalone` |
| `charts/soteria/templates/standalone-ui/service.yaml` | NEW | Service: HTTP 8080 |
| `charts/soteria/templates/standalone-ui/serviceaccount.yaml` | NEW | ServiceAccount for console-proxy |
| `charts/soteria/templates/standalone-ui/clusterrole.yaml` | NEW | ClusterRole: read drplans/drexecutions/VMs, create/patch drexecutions |
| `charts/soteria/templates/standalone-ui/clusterrolebinding.yaml` | NEW | ClusterRoleBinding |
| `charts/soteria/templates/standalone-ui/httproute.yaml` | NEW | Gateway API HTTPRoute, conditional on gateway.name being set |

### Key Constraints

- All templates must be guarded by `{{- if eq .Values.ui.mode "standalone" }}`
- The HTTPRoute has a double guard: standalone mode AND `ui.standalone.gateway.name` is non-empty
- The standalone UI deploys to its own namespace (`soteria-console`) in the existing manifests, but the Helm chart should deploy to the release namespace for simplicity
- The console-proxy container uses `gcr.io/distroless/static:nonroot` as base image, runs as UID 65534
- The SA token is mounted automatically at `/var/run/secrets/kubernetes.io/serviceaccount/token` by Kubernetes
- Depends on: Story 16.1 (chart skeleton and values.yaml)

### Project Structure Notes

- All standalone UI templates go under `charts/soteria/templates/standalone-ui/` subdirectory
- The standalone Dockerfile is at `console-plugin/Dockerfile.standalone` (two-stage: Node.js + Go)
- The console-proxy Go binary source is at `cmd/console-proxy/main.go`

### References

- [Source: `hack/overlays/base/console-standalone.yaml` — complete standalone UI manifest with Namespace, RBAC, Deployment, Service]
- [Source: `hack/multisite/manifests/console-gateway.yaml` — Gateway + HTTPRoute for Cilium ingress]
- [Source: `console-plugin/Dockerfile.standalone` — two-stage build: Node.js SPA + Go proxy binary]
- [Source: `cmd/console-proxy/main.go` — Go reverse proxy with SA token auth, SPA handler, healthz endpoint]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6

### Debug Log References
None — clean implementation, all helm template verifications passed first try.

### Completion Notes List
- Created 6 Helm templates under `charts/soteria/templates/standalone-ui/`
- All templates guarded by `{{- if eq .Values.ui.mode "standalone" }}`
- HTTPRoute additionally guarded by `.Values.ui.standalone.gateway.name` being non-empty
- Deployment mirrors `hack/overlays/base/console-standalone.yaml`: securityContext, probes, resources
- RBAC rules exactly match `soteria-console-reader` ClusterRole from reference manifest
- Image tag defaults to `.Chart.AppVersion` when `ui.standalone.image.tag` is empty
- All resources use standard `soteria.labels`, `soteria.selectorLabels`, and `soteria.fullname` helpers
- Verified all 5 ACs via 4 `helm template` invocations

### File List
- `charts/soteria/templates/standalone-ui/deployment.yaml` (NEW)
- `charts/soteria/templates/standalone-ui/service.yaml` (NEW)
- `charts/soteria/templates/standalone-ui/serviceaccount.yaml` (NEW)
- `charts/soteria/templates/standalone-ui/clusterrole.yaml` (NEW)
- `charts/soteria/templates/standalone-ui/clusterrolebinding.yaml` (NEW)
- `charts/soteria/templates/standalone-ui/httproute.yaml` (NEW)
- `_bmad-output/implementation-artifacts/16-6-standalone-ui-templates.md` (MODIFIED)
