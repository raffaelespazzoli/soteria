# Story 16.1: Chart Skeleton & Values

Status: review

## Story

As a platform engineer,
I want a well-structured Helm chart with a comprehensive `values.yaml` schema,
so that all Soteria deployment parameters are centrally configured and validated.

## Acceptance Criteria

**AC1: Chart metadata is valid**
Given the `charts/soteria/` directory exists
When I run `helm lint charts/soteria/`
Then the chart passes linting with no errors

**AC2: values.yaml contains the full agreed schema**
Given the `values.yaml` file exists
When I inspect its top-level keys
Then it contains `site`, `controller`, `tls`, `scylladb`, `networking`, and `ui` sections with all sub-fields documented

**AC3: Site configuration**
Given `values.yaml` has a `site` section
When I set `site.name` and `site.role`
Then the chart templates can reference those values

**AC4: Controller configuration**
Given `values.yaml` has a `controller` section
When I set `controller.image`, `controller.replicas`, `controller.resources`, `controller.leaderElection`, and `controller.apiserver.port`
Then the chart templates can reference those values for the controller Deployment

**AC5: TLS issuer configuration**
Given `values.yaml` has a `tls` section
When I set `tls.issuerRef.name` and `tls.issuerRef.kind`
Then all cert-manager Certificate resources reference the configured issuer

**AC6: ScyllaDB mode selection**
Given `values.yaml` has a `scylladb` section
When I set `scylladb.mode` to `managed` or `external`
Then common fields (`keyspace`, `localDC`, `dcReplication`) are available and mode-specific sub-sections (`managed.*` or `external.*` with `contactPoints` and `tls`) are present

**AC7: Networking mode selection**
Given `values.yaml` has a `networking` section
When I set `networking.mode` to `submariner` or `cilium`
Then per-mode settings are available and a `cUDN` placeholder is present as a TODO comment

**AC8: UI mode selection**
Given `values.yaml` has a `ui` section
When I set `ui.mode` to `console-plugin`, `standalone`, or `none`
Then per-mode image and settings are available

**AC9: _helpers.tpl standard helpers**
Given `charts/soteria/templates/_helpers.tpl` exists
When I render the chart
Then the helpers `soteria.name`, `soteria.fullname`, `soteria.labels`, `soteria.selectorLabels`, `soteria.chart`, and `soteria.serviceAccountName` are available

## Tasks / Subtasks

- [x] Task 1: Create chart directory and metadata (AC: 1)
  - [x] 1.1: Create `charts/soteria/Chart.yaml` with name, version `0.1.0`, appVersion, description, type `application`, and `kubeVersion: ">=1.28.0"` constraint (mirror `config/helmchart/Chart.yaml.tpl` structure)
  - [x] 1.2: Create empty `charts/soteria/templates/` directory with `.gitkeep`
- [x] Task 2: Create values.yaml — site & controller sections (AC: 2, 3, 4)
  - [x] 2.1: Add `site` section with `name: ""` and `role: seed|joining`
  - [x] 2.2: Add `controller` section with `image.repository`, `image.tag`, `image.pullPolicy`, `replicas: 1`, `resources` (matching `config/manager/manager.yaml` defaults: cpu 10m–500m, mem 64–128Mi), `leaderElection.enabled: true`, `apiserver.port: 6443`
- [x] Task 3: Create values.yaml — TLS section (AC: 2, 5)
  - [x] 3.1: Add `tls` section with `issuerRef.name: "soteria-ca"` and `issuerRef.kind: "Issuer"` (matching `config/certmanager/` resources)
- [x] Task 4: Create values.yaml — ScyllaDB section (AC: 2, 6)
  - [x] 4.1: Add `scylladb.mode: managed` (toggle), common fields `keyspace: soteria`, `localDC: ""`, `dcReplication: ""`
  - [x] 4.2: Add `managed` sub-section with `version`, `rack`, `members`, `storage`, `developerMode`, `resources`, `externalSeeds`
  - [x] 4.3: Add `external` sub-section with `contactPoints: ""`, `tls.enabled: false`, `tls.secretName: ""`
- [x] Task 5: Create values.yaml — networking section (AC: 2, 7)
  - [x] 5.1: Add `networking.mode: submariner` (toggle) with per-mode settings
  - [x] 5.2: Add `# TODO: cUDN networking mode` comment placeholder
- [x] Task 6: Create values.yaml — UI section (AC: 2, 8)
  - [x] 6.1: Add `ui.mode: console-plugin` (toggle)
  - [x] 6.2: Add `consolePlugin` sub-section with `image.repository`, `image.tag`
  - [x] 6.3: Add `standalone` sub-section with `image.repository`, `image.tag`, `gateway.name`, `gateway.className`
- [x] Task 7: Create _helpers.tpl (AC: 9)
  - [x] 7.1: Implement `soteria.name`, `soteria.fullname`, `soteria.labels`, `soteria.selectorLabels`, `soteria.chart`, `soteria.serviceAccountName` helpers
- [x] Task 8: Validate chart (AC: 1)
  - [x] 8.1: Run `helm lint charts/soteria/` and fix any issues

## Dev Notes

### Implementation Pattern

Use the existing `config/helmchart/Chart.yaml.tpl` as the starting point for `Chart.yaml`:

```yaml
apiVersion: v2
name: soteria
description: Storage-agnostic disaster recovery orchestrator for OpenShift Virtualization
type: application
version: 0.1.0
appVersion: 0.1.0
```

The ScyllaDB connection flags in `pkg/apiserver/options.go` define what `values.yaml` must expose:
- `--scylladb-contact-points` (default `localhost:9042`)
- `--scylladb-keyspace` (default `soteria`)
- `--scylladb-local-dc` (empty)
- `--scylladb-dc-replication` (empty, comma-separated `dc:rf` pairs like `etl6:2,etl7:2`)
- `--scylladb-tls-cert`, `--scylladb-tls-key`, `--scylladb-tls-ca`, `--scylladb-tls-server-name`

The existing kustomize overlay in `hack/overlays/base/manager-args-patch.yaml` shows how these flags are set:
```yaml
- --scylladb-contact-points=soteria-scylladb-client.soteria.svc:9142
- --scylladb-keyspace=soteria
- --scylladb-dc-replication=etl6:2,etl7:2
- --scylladb-tls-cert=/etc/soteria/scylladb-client-tls/tls.crt
```

The `hack/stretched-local-test.sh` shows two-cluster deployment patterns with `--site-name` flag per cluster. The `hack/multisite/deploy-soteria.sh` shows the Minikube variant with Cilium networking.

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `charts/soteria/Chart.yaml` | NEW | Chart metadata (name, version, appVersion, kubeVersion) |
| `charts/soteria/values.yaml` | NEW | Full values schema: site, controller, tls, scylladb, networking, ui |
| `charts/soteria/templates/_helpers.tpl` | NEW | Standard Helm helpers (name, fullname, labels, selectorLabels, chart, serviceAccountName) |
| `charts/soteria/templates/.gitkeep` | NEW | Placeholder to ensure templates dir is tracked |

### Key Constraints

- No chart dependencies — ScyllaDB operator and cert-manager are prerequisites, not sub-charts
- The `cUDN` networking mode is a placeholder for future work; include a TODO comment in `values.yaml`
- Use inline YAML comments in `values.yaml` to document every field
- `controller.resources` defaults must match `config/manager/manager.yaml` (cpu: 10m/500m, mem: 64Mi/128Mi)
- `scylladb.managed` defaults must match `hack/multisite/overlays/base/scyllacluster.yaml` (version 2026.1.3, 1 member, 5Gi storage)

### Project Structure Notes

- Chart lives at `charts/soteria/` (standard Helm convention)
- Chart templates are organized by component in subdirectories (e.g. `templates/controller/`, `templates/scylladb/`)

### References

- [Source: `config/helmchart/Chart.yaml.tpl` — existing chart template with REPLACE_VERSION placeholders]
- [Source: `pkg/apiserver/options.go` — all ScyllaDB connection flags and defaults]
- [Source: `hack/stretched-local-test.sh` — two-cluster Submariner deployment patterns]
- [Source: `hack/multisite/deploy-soteria.sh` — Minikube Cilium deployment patterns]
- [Source: `config/manager/manager.yaml` — controller Deployment spec with default resources]
- [Source: `hack/multisite/overlays/base/scyllacluster.yaml` — ScyllaDB defaults]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6 (via Cursor)

### Debug Log References
- `helm lint charts/soteria/` passed: 1 chart linted, 0 failed (INFO: icon recommended)

### Completion Notes List
- Created Chart.yaml with metadata mirroring config/helmchart/Chart.yaml.tpl, added kubeVersion constraint
- Created values.yaml with all 6 top-level sections (site, controller, tls, scylladb, networking, ui) fully documented
- Controller resource defaults match config/manager/manager.yaml (cpu: 10m/500m, mem: 64Mi/128Mi)
- ScyllaDB managed defaults match hack/multisite/overlays/base/scyllacluster.yaml (v2026.1.3, 1 member, 5Gi, developerMode)
- ScyllaDB external section includes contactPoints and TLS sub-section
- Networking includes submariner/cilium modes and cUDN TODO placeholder
- UI supports console-plugin, standalone, and none modes
- _helpers.tpl implements all 6 required helpers (name, fullname, chart, labels, selectorLabels, serviceAccountName)
- templates/.gitkeep placeholder ensures directory is tracked in git

### File List
- `charts/soteria/Chart.yaml` — NEW — Chart metadata
- `charts/soteria/values.yaml` — NEW — Full values schema
- `charts/soteria/templates/_helpers.tpl` — NEW — Standard Helm helpers
- `charts/soteria/templates/.gitkeep` — NEW — Directory placeholder
- `_bmad-output/implementation-artifacts/16-1-chart-skeleton-and-values.md` — MODIFIED — Task checkboxes, status, dev record
