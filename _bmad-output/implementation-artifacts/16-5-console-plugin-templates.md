# Story 16.5: Console Plugin Templates

Status: ready-for-dev

## Story

As a platform engineer deploying Soteria on OpenShift,
I want Helm templates for the OCP console plugin that are only rendered when `ui.mode=console-plugin`,
so that the Soteria UI integrates natively into the OpenShift console.

## Acceptance Criteria

**AC1: Console plugin Deployment renders in console-plugin mode**
Given `ui.mode=console-plugin`
When I run `helm template`
Then a Deployment running the nginx-based console plugin image is rendered with the image from `ui.consolePlugin.image`

**AC2: Console plugin Service exposes HTTPS on 9443**
Given `ui.mode=console-plugin`
When I render the chart
Then a Service is rendered with HTTPS on port 9443

**AC3: ConsolePlugin CR is rendered**
Given `ui.mode=console-plugin`
When I render the chart
Then a ConsolePlugin CR (`console.openshift.io/v1`) is rendered referencing the console plugin Service

**AC4: cert-manager Certificate for console plugin**
Given `ui.mode=console-plugin`
When I render the chart
Then a cert-manager Certificate for the console plugin's serving cert is rendered referencing `tls.issuerRef`

**AC5: No console plugin resources in other modes**
Given `ui.mode=standalone` or `ui.mode=none`
When I render the chart
Then no console plugin Deployment, Service, ConsolePlugin CR, or Certificate is rendered

## Tasks / Subtasks

- [ ] Task 1: Create console plugin Deployment template (AC: 1, 5)
  - [ ] 1.1: Create `templates/console-plugin/deployment.yaml` guarded by `{{- if eq .Values.ui.mode "console-plugin" }}`
  - [ ] 1.2: Wire image from `ui.consolePlugin.image.repository` and `ui.consolePlugin.image.tag`
  - [ ] 1.3: Set container port 9443 (HTTPS), mount serving cert volume and nginx TLS config volume
  - [ ] 1.4: Add liveness/readiness probes against `/plugin-manifest.json` on HTTPS port 9443
  - [ ] 1.5: Set resource limits (requests: cpu 10m, mem 50Mi; limits: mem 100Mi — matching existing manifest)
- [ ] Task 2: Create nginx TLS ConfigMap template (AC: 1)
  - [ ] 2.1: Create `templates/console-plugin/configmap-nginx.yaml` with nginx `tls.conf` listening on 9443 with cert paths (mirror `hack/overlays/base/console-plugin.yaml` ConfigMap)
- [ ] Task 3: Create console plugin Service template (AC: 2, 5)
  - [ ] 3.1: Create `templates/console-plugin/service.yaml` with HTTPS on port 9443, selector matching console plugin pods
- [ ] Task 4: Create ConsolePlugin CR template (AC: 3, 5)
  - [ ] 4.1: Create `templates/console-plugin/consoleplugin.yaml` with `console.openshift.io/v1` API, referencing the console plugin Service at port 9443
  - [ ] 4.2: Set `displayName: "Soteria DR Management"` (matching `console-plugin/package.json` consolePlugin.displayName)
- [ ] Task 5: Create console plugin Certificate template (AC: 4, 5)
  - [ ] 5.1: Create `templates/console-plugin/certificate.yaml` referencing `tls.issuerRef`, with DNS names for the console plugin Service
- [ ] Task 6: Verify rendering (AC: 1–5)
  - [ ] 6.1: Run `helm template` with `ui.mode=console-plugin` — verify all 5 resources render
  - [ ] 6.2: Run `helm template` with `ui.mode=standalone` — verify no console plugin resources
  - [ ] 6.3: Run `helm template` with `ui.mode=none` — verify no console plugin resources

## Dev Notes

### Implementation Pattern

The existing manifest at `hack/overlays/base/console-plugin.yaml` contains the complete reference implementation with four resources:

1. **ConfigMap** (`soteria-console-plugin-nginx`) — nginx TLS config:
```yaml
data:
  tls.conf: |
    server {
        listen 9443 ssl;
        ssl_certificate /var/serving-cert/tls.crt;
        ssl_certificate_key /var/serving-cert/tls.key;
        root /opt/app-root/src;
        location / {
            try_files $uri $uri/ =404;
        }
    }
```

2. **Deployment** — nginx-based container with two volume mounts:
   - `serving-cert` → Secret with TLS cert at `/var/serving-cert`
   - `nginx-tls-conf` → ConfigMap at `/opt/app-root/etc/nginx.d`

3. **Service** — HTTPS on port 9443

4. **ConsolePlugin CR** (`console.openshift.io/v1`):
```yaml
spec:
  displayName: "Soteria DR Management"
  backend:
    type: Service
    service:
      name: soteria-console-plugin
      namespace: soteria
      port: 9443
```

The `console-plugin/Dockerfile` builds from `node:22-slim` → `registry.access.redhat.com/ubi9/nginx-120:latest`, serving built static assets.

The `console-plugin/package.json` `consolePlugin` section defines the plugin metadata:
- name: `soteria-console-plugin`
- displayName: `Soteria DR Management`
- exposedModules: `DRDashboardPage`, `DRPlanDetailPage`, `ExecutionDetailPage`

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `charts/soteria/templates/console-plugin/deployment.yaml` | NEW | Console plugin Deployment (nginx), conditional on `ui.mode=console-plugin` |
| `charts/soteria/templates/console-plugin/configmap-nginx.yaml` | NEW | Nginx TLS configuration ConfigMap |
| `charts/soteria/templates/console-plugin/service.yaml` | NEW | Service: HTTPS 9443 |
| `charts/soteria/templates/console-plugin/consoleplugin.yaml` | NEW | ConsolePlugin CR (`console.openshift.io/v1`) |
| `charts/soteria/templates/console-plugin/certificate.yaml` | NEW | cert-manager Certificate for console plugin TLS |

### Key Constraints

- All templates must be guarded by `{{- if eq .Values.ui.mode "console-plugin" }}`
- The ConsolePlugin CR name must be exactly `soteria-console-plugin` (not prefixed by release name) since OpenShift references it by name
- The nginx container uses `registry.access.redhat.com/ubi9/nginx-120` as base image, runs as UID 1001
- The existing manifest uses OpenShift service-CA annotation (`service.beta.openshift.io/serving-cert-secret-name`) for cert; the Helm chart should use cert-manager instead for consistency
- Depends on: Story 16.1 (chart skeleton and values.yaml)

### Project Structure Notes

- All console plugin templates go under `charts/soteria/templates/console-plugin/` subdirectory
- The console plugin Dockerfile is at `console-plugin/Dockerfile` (separate from the controller image)

### References

- [Source: `hack/overlays/base/console-plugin.yaml` — complete console plugin manifest with ConfigMap, Deployment, Service, ConsolePlugin CR]
- [Source: `console-plugin/Dockerfile` — nginx-based image build from Node.js SPA]
- [Source: `console-plugin/package.json` — consolePlugin metadata: name, displayName, exposedModules]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
