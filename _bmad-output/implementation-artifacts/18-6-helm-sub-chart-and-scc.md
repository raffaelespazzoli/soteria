# Story 18.6: Helm Sub-Chart & SCC

Status: ready-for-dev

## Story

As a platform engineer,
I want a standalone Helm chart for the IP rewrite webhook that includes the necessary SecurityContextConstraints,
so that I can deploy the IP rewrite feature independently of the main Soteria chart.

## Acceptance Criteria

### AC1: Chart structure
Given the chart at `charts/soteria-ip-rewrite/`
When I inspect the directory
Then it contains `Chart.yaml`, `values.yaml`, `templates/_helpers.tpl`, and template files for all resources

### AC2: Webhook deployment
Given the chart is installed with default values
When the Deployment is rendered
Then it creates a webhook server with configurable replicas (default 2), resources, and image reference

### AC3: SecurityContextConstraints for init containers
Given the chart is installed on OpenShift
When the SCC template is rendered
Then it creates an SCC named `soteria-ip-rewrite` that allows `SYS_ADMIN` capability
And binds to a dedicated ServiceAccount
And the SCC is the minimum privilege needed for guestfish to operate

### AC4: MutatingWebhookConfiguration
Given the chart is installed
When the MWC template is rendered
Then it targets pod `CREATE` operations with `objectSelector.matchLabels: {"soteria.io/ip-rewrite": "true"}`
And `failurePolicy: Ignore` (fail-open)
And `caBundle` is injected via cert-manager annotation

### AC5: cert-manager integration
Given the chart is installed with cert-manager available
When cert-manager templates are rendered
Then it creates a self-signed `Issuer` and a `Certificate` for the webhook server
And the certificate Secret is mounted into the webhook Deployment

### AC6: Namespace isolation
Given the chart is installed into a namespace (e.g., `soteria-ip-rewrite`)
When the webhook runs
Then it only processes pods in namespaces with the label `soteria.io/ip-rewrite-enabled: "true"` (namespace selector on MWC) or cluster-wide (configurable via `values.yaml`)

### AC7: helm lint passes
Given the chart directory
When I run `helm lint charts/soteria-ip-rewrite/`
Then no errors are reported

### AC8: Optional integration with main Soteria chart
Given the main Soteria chart at `charts/soteria/`
When `ipRewrite.enabled: true` is set in the main chart's `values.yaml`
Then the main chart includes the IP rewrite sub-chart as a dependency
And shared cert-manager issuer is used

## Tasks / Subtasks

- [ ] Task 1: Create `charts/soteria-ip-rewrite/Chart.yaml` and `charts/soteria-ip-rewrite/values.yaml` (AC: 1)
  - [ ] 1.1: `Chart.yaml` — `apiVersion: v2`, `name: soteria-ip-rewrite`, `type: application`, version/appVersion matching root chart `0.1.0`
  - [ ] 1.2: `values.yaml` — structured with all configurable defaults (see Dev Notes for schema)
- [ ] Task 2: Create `charts/soteria-ip-rewrite/templates/_helpers.tpl` (AC: 1)
  - [ ] 2.1: `soteria-ip-rewrite.name`, `soteria-ip-rewrite.fullname`, `soteria-ip-rewrite.chart`, `soteria-ip-rewrite.labels`, `soteria-ip-rewrite.selectorLabels`, `soteria-ip-rewrite.serviceAccountName`, `soteria-ip-rewrite.imageTag` — mirroring the Soteria parent chart's helpers pattern
- [ ] Task 3: Create `charts/soteria-ip-rewrite/templates/deployment.yaml` (AC: 2)
  - [ ] 3.1: Webhook server Deployment with configurable replicas, image, resources, liveness/readiness probes on `:8081`
  - [ ] 3.2: TLS cert volume mount from cert-manager Secret
  - [ ] 3.3: `--init-container-image` flag wired from `values.yaml`
  - [ ] 3.4: Security context: `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `seccompProfile: RuntimeDefault`
- [ ] Task 4: Create `charts/soteria-ip-rewrite/templates/service.yaml` (AC: 2)
  - [ ] 4.1: Service port 443 → targetPort 9443 (webhook convention)
- [ ] Task 5: Create `charts/soteria-ip-rewrite/templates/mutatingwebhookconfiguration.yaml` (AC: 4, 6)
  - [ ] 5.1: `objectSelector.matchLabels: {"soteria.io/ip-rewrite": "true"}`
  - [ ] 5.2: `failurePolicy: Ignore`, `sideEffects: None`
  - [ ] 5.3: `cert-manager.io/inject-ca-from` annotation referencing the Certificate
  - [ ] 5.4: Configurable `namespaceSelector` from `values.yaml` (empty = all namespaces)
- [ ] Task 6: Create `charts/soteria-ip-rewrite/templates/certificate.yaml` and `charts/soteria-ip-rewrite/templates/issuer.yaml` (AC: 5)
  - [ ] 6.1: Self-signed `Issuer` for standalone deployment
  - [ ] 6.2: `Certificate` for webhook TLS with DNS names matching Service FQDN
  - [ ] 6.3: Issuer configurable via `values.yaml` (for parent chart override)
- [ ] Task 7: Create `charts/soteria-ip-rewrite/templates/scc.yaml` (AC: 3)
  - [ ] 7.1: SCC with `allowedCapabilities: [SYS_ADMIN]`, restricted volumes list, non-host access
  - [ ] 7.2: `users:` field binding to the chart's ServiceAccount
- [ ] Task 8: Create `charts/soteria-ip-rewrite/templates/serviceaccount.yaml` and `charts/soteria-ip-rewrite/templates/clusterrolebinding.yaml` (AC: 3)
  - [ ] 8.1: ServiceAccount for webhook server + init containers
  - [ ] 8.2: ClusterRoleBinding granting the SCC to the ServiceAccount
- [ ] Task 9: Add sub-chart dependency to main Soteria chart (AC: 8)
  - [ ] 9.1: Add `ipRewrite.enabled: false` to `charts/soteria/values.yaml`
  - [ ] 9.2: Add `dependencies:` entry in `charts/soteria/Chart.yaml` pointing to `file://../soteria-ip-rewrite` with `condition: ipRewrite.enabled`
- [ ] Task 10: Validate with `helm lint` (AC: 7)
  - [ ] 10.1: Run `helm lint charts/soteria-ip-rewrite/` with minimal required values
  - [ ] 10.2: Run `helm template` and verify all resource names, labels, selectors are correct

## Dev Notes

### Story Intelligence Chain

#### Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9

Story 18.1 created the container image used by the init container that the webhook injects. Key context:

- **Init container image**: `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION` — this is the image referenced in `values.yaml` as `initContainer.image` and passed to the webhook server via `--init-container-image` flag
- **Runtime requirements**: The init container needs `SYS_ADMIN` capability for the guestfish appliance. This is **why the SCC exists** in this chart — the init container runs with `SYS_ADMIN` inside virt-launcher pods, and OpenShift requires an SCC to permit that
- **`LIBGUESTFS_BACKEND=direct`** is baked into the image — no chart-level env var needed for the init container

#### Predecessor: Story 18.5 — Mutating Webhook: virt-launcher Init Container Injection

Story 18.5 creates the Go webhook server binary that this chart deploys. Key context:

- **Binary location**: `cmd/ip-rewrite-webhook/main.go` — a **standalone binary**, NOT part of the main Soteria manager. The Deployment in this chart runs this binary
- **Webhook image**: Needs its own Dockerfile for the webhook binary (multi-stage Go build → distroless). This is SEPARATE from the init container image (18.1). The chart's `webhook.image` references this webhook server image
- **Server flags**: `--cert-dir` (default `/tmp/k8s-webhook-server/serving-certs`), `--port` (default `9443`), `--init-container-image` (default `quay.io/raffaelespazzoli/soteria-ip-rewrite:latest`)
- **Health probes**: `/healthz` and `/readyz` on port `8081`
- **Webhook path**: `/mutate-v1-pod`
- **Handler package**: `internal/webhook/iprewrite/handler.go` — uses `sigs.k8s.io/controller-runtime/pkg/webhook/admission`
- **MWC reference manifest**: Story 18.5 was told to create `config/ip-rewrite-webhook/mutatingwebhookconfiguration.yaml` as a reference. The Helm chart templates this manifest with proper Helm variables

**What 18.5 deferred to this story:**
- All deployment manifests (Deployment, Service, ServiceAccount)
- cert-manager Certificate and Issuer
- SecurityContextConstraints
- MutatingWebhookConfiguration (the Helm-templated production version)
- Dockerfile for the webhook binary
- Integration with main Soteria chart

#### Story interdependencies within Epic 18

- Stories 18.2–18.4 (scripts) are NOT dependencies — scripts are inside the init container image, not the webhook server
- Story 18.7 (tests) comes after this story — tests need the chart to exist for Helm lint verification
- Story 18.9 (CI) adds build targets — the webhook Dockerfile created here will be built by CI later

### Critical Technical Details

#### Two Separate Images — DO NOT Confuse

| Image | Purpose | Deployed By | Source |
|-------|---------|-------------|--------|
| `quay.io/raffaelespazzoli/soteria-ip-rewrite-webhook` | Webhook server binary (Go) | This chart's Deployment | `cmd/ip-rewrite-webhook/main.go` built via `build/ip-rewrite-webhook/Dockerfile` |
| `quay.io/raffaelespazzoli/soteria-ip-rewrite` | Init container with guestfs-tools | Injected by webhook into virt-launcher pods | `build/ip-rewrite/Containerfile` (Story 18.1) |

The chart must configure BOTH images in `values.yaml`:
- `webhook.image.*` — the webhook server (deployed as a Deployment in this chart)
- `initContainer.image.*` — passed to the webhook server via `--init-container-image` flag, which then injects it into virt-launcher pods

#### Webhook Server Dockerfile

Create `build/ip-rewrite-webhook/Dockerfile` following the exact pattern of the root `Dockerfile`:

```dockerfile
FROM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o ip-rewrite-webhook ./cmd/ip-rewrite-webhook/
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/ip-rewrite-webhook .
USER 65532:65532
ENTRYPOINT ["/ip-rewrite-webhook"]
```

Key differences from root Dockerfile:
- Builds `./cmd/ip-rewrite-webhook/` instead of `./cmd/soteria/`
- Output binary named `ip-rewrite-webhook` instead of `manager`
- Same Go version (`1.26`), same distroless base, same user
- Build context is the repo root (needs `go.mod`/`go.sum` + full source tree)

#### SecurityContextConstraints (SCC) — OpenShift-Specific

The SCC is required because the ip-rewrite init container runs with `SYS_ADMIN` capability inside virt-launcher pods. OpenShift blocks capabilities not permitted by an SCC.

**SCC design — minimum privilege:**

```yaml
apiVersion: security.openshift.io/v1
kind: SecurityContextConstraints
metadata:
  name: <fullname>-ip-rewrite
allowedCapabilities:
  - SYS_ADMIN
# Required volume types for virt-launcher pod context
volumes:
  - persistentVolumeClaim
  - secret
  - configMap
  - projected
  - emptyDir
# Deny host-level access
allowHostDirVolumePlugin: false
allowHostNetwork: false
allowHostPorts: false
allowHostPID: false
allowHostIPC: false
allowPrivilegedContainer: false
# RunAsUser: must allow the same UID range as the namespace
runAsUser:
  type: RunAsAny
seLinuxContext:
  type: MustRunAs
fsGroup:
  type: RunAsAny
supplementalGroups:
  type: RunAsAny
# Bind to ServiceAccount
users:
  - system:serviceaccount:{{ .Release.Namespace }}:{{ serviceAccountName }}
```

**SCC binding**: On OpenShift, SCCs are bound via the `users:` field directly referencing ServiceAccount names in `system:serviceaccount:<namespace>:<name>` format. No separate ClusterRole/ClusterRoleBinding is needed for SCC binding itself. However, the virt-launcher pods use their OWN ServiceAccount (managed by KubeVirt), not the chart's ServiceAccount. The SCC `users:` field must reference the ServiceAccount that the virt-launcher pods run under.

**CRITICAL INSIGHT**: The SCC needs to cover the init container running inside the **virt-launcher pod**, not the webhook Deployment. The virt-launcher pod's ServiceAccount is managed by KubeVirt (typically `default` or a per-VM SA in the VM's namespace). The chart should provide a configurable `scc.serviceAccountNames` list or bind using a group-based approach. The pragmatic approach: use `allowedCapabilities` scoped to `SYS_ADMIN` and bind via a ClusterRole + ClusterRoleBinding with `use` verb on the SCC, allowing specific ServiceAccounts or groups to use it.

**Revised approach — ClusterRole + ClusterRoleBinding for SCC:**

```yaml
# ClusterRole granting "use" of the SCC
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: <fullname>-scc-use
rules:
  - apiGroups: ["security.openshift.io"]
    resources: ["securitycontextconstraints"]
    resourceNames: ["<fullname>-ip-rewrite"]
    verbs: ["use"]
---
# ClusterRoleBinding — bind to configurable subjects
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: <fullname>-scc-use
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: <fullname>-scc-use
subjects:
  # Default: all ServiceAccounts in the release namespace
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: system:serviceaccounts:{{ .Release.Namespace }}
  # Additional subjects configurable via values.yaml
```

This allows any pod in the release namespace (or configured namespaces) to use the SCC. For cross-namespace support (virt-launcher pods run in user namespaces, not the chart's namespace), the binding subjects should be configurable in `values.yaml`.

#### values.yaml Schema

```yaml
# -- Override the chart name used in resource names and labels.
nameOverride: ""
# -- Override the full release name used in resource names.
fullnameOverride: ""

webhook:
  image:
    repository: quay.io/raffaelespazzoli/soteria-ip-rewrite-webhook
    tag: ""          # defaults to Chart.appVersion
    pullPolicy: IfNotPresent
  replicas: 2
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi

initContainer:
  image:
    repository: quay.io/raffaelespazzoli/soteria-ip-rewrite
    tag: ""          # defaults to Chart.appVersion

tls:
  issuerRef:
    name: ""         # empty → use self-signed issuer created by chart
    kind: Issuer
  # createSelfSigned: When true AND issuerRef.name is empty, chart creates
  # a self-signed Issuer. When false, issuerRef.name MUST be set.
  createSelfSigned: true

scc:
  # enabled: Set to false on non-OpenShift clusters (vanilla K8s has no SCC API)
  enabled: true
  # subjects: Additional RBAC subjects granted SCC "use" permission.
  # Default binding: all SAs in namespaces listed in scc.namespaces.
  additionalSubjects: []
  # namespaces: Namespaces whose ServiceAccounts can use the SCC.
  # Must include namespaces where VMs run (virt-launcher pods need SYS_ADMIN).
  # Empty list = release namespace only.
  namespaces: []

webhookConfig:
  # failurePolicy: Ignore (fail-open) or Fail (fail-closed)
  failurePolicy: Ignore
  # namespaceSelector: Label selector for namespaces the webhook applies to.
  # Empty = all namespaces.
  namespaceSelector: {}
```

#### Existing Chart Patterns to Follow

The parent chart at `charts/soteria/` establishes these conventions — follow them exactly:

| Pattern | Soteria Chart | IP Rewrite Chart |
|---------|---------------|------------------|
| Helper prefix | `soteria.*` | `soteria-ip-rewrite.*` |
| Name template | `soteria.name` → `default .Chart.Name .Values.nameOverride` | Same pattern |
| Fullname | `soteria.fullname` → release-name logic, trunc 63 | Same pattern |
| Labels | `soteria.labels` (chart, selector, version, managed-by) | Same pattern |
| Selector labels | `soteria.selectorLabels` (name + instance) | Same pattern |
| ServiceAccount | `soteria.serviceAccountName` (configurable via values) | Same pattern |
| Image tag | `soteria.imageTag` (tag or Chart.AppVersion fallback) | Same pattern |
| Validation | `soteria.validate` (required/fail assertions in `_validation.tpl`) | Same pattern |
| Secret names | `printf "%s-suffix" (include "fullname" .) | trunc 63 | trimSuffix "-"` | Same pattern |
| cert-manager | `cert-manager.io/inject-ca-from: namespace/certname` annotation | Same pattern |
| Service ports | `port: 443, targetPort: 9443` for webhooks | Same pattern |
| Security context | `runAsNonRoot: true`, `seccompProfile: RuntimeDefault`, `readOnlyRootFilesystem: true`, drop ALL | Same pattern |
| Deployment probes | `httpGet /healthz :8081` | Same pattern |
| Volume mounts | cert secrets mounted read-only | Same pattern |
| Namespace | Always `{{ .Release.Namespace }}` on namespaced resources | Same pattern |
| Comments | `{{- /* Description */ -}}` at top of each template | Same pattern |

#### Chart Directory Structure

```
charts/soteria-ip-rewrite/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── _helpers.tpl                         # Standard helpers (name, fullname, labels, etc.)
    ├── _validation.tpl                      # Required-value guards
    ├── deployment.yaml                      # Webhook server Deployment
    ├── service.yaml                         # Service (443→9443)
    ├── serviceaccount.yaml                  # ServiceAccount for webhook server
    ├── mutatingwebhookconfiguration.yaml    # MWC with objectSelector + namespaceSelector
    ├── certificate.yaml                     # cert-manager Certificate for webhook TLS
    ├── issuer.yaml                          # Self-signed Issuer (conditional)
    ├── scc.yaml                             # OpenShift SecurityContextConstraints (conditional)
    ├── clusterrole-scc.yaml                 # ClusterRole granting SCC "use" (conditional)
    └── clusterrolebinding-scc.yaml          # ClusterRoleBinding for SCC (conditional)
```

Additionally:

```
build/ip-rewrite-webhook/
└── Dockerfile                               # Multi-stage Go build for webhook binary
```

#### MutatingWebhookConfiguration Template

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: {{ printf "%s-mutating-webhook" (include "soteria-ip-rewrite.fullname" .) | trunc 63 | trimSuffix "-" }}
  labels:
    {{- include "soteria-ip-rewrite.labels" . | nindent 4 }}
  annotations:
    cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/{{ printf "%s-webhook-serving" (include "soteria-ip-rewrite.fullname" .) | trunc 63 | trimSuffix "-" }}
webhooks:
  - name: ip-rewrite.soteria.io
    admissionReviewVersions: ["v1"]
    clientConfig:
      service:
        name: {{ printf "%s-webhook" (include "soteria-ip-rewrite.fullname" .) | trunc 63 | trimSuffix "-" }}
        namespace: {{ .Release.Namespace }}
        path: /mutate-v1-pod
    failurePolicy: {{ .Values.webhookConfig.failurePolicy }}
    sideEffects: None
    objectSelector:
      matchLabels:
        soteria.io/ip-rewrite: "true"
    {{- with .Values.webhookConfig.namespaceSelector }}
    namespaceSelector:
      matchLabels:
        {{- toYaml . | nindent 8 }}
    {{- end }}
    rules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE"]
        resources: ["pods"]
```

Key: `objectSelector` is always present (only intercept labeled pods). `namespaceSelector` is optional — when `webhookConfig.namespaceSelector` is empty, it is omitted (webhook applies to all namespaces).

#### cert-manager Certificate Template

Follow the exact pattern from `charts/soteria/templates/controller/certificate-webhook.yaml`:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: {{ printf "%s-webhook-serving" (include "soteria-ip-rewrite.fullname" .) | trunc 63 | trimSuffix "-" }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "soteria-ip-rewrite.labels" . | nindent 4 }}
spec:
  secretName: {{ printf "%s-webhook-server-cert" (include "soteria-ip-rewrite.fullname" .) | trunc 63 | trimSuffix "-" }}
  duration: 8760h
  renewBefore: 720h
  usages:
    - server auth
  issuerRef:
    name: {{ default (printf "%s-selfsigned" (include "soteria-ip-rewrite.fullname" .)) .Values.tls.issuerRef.name }}
    kind: {{ .Values.tls.issuerRef.kind }}
  dnsNames:
    - {{ printf "%s-webhook.%s.svc" (include "soteria-ip-rewrite.fullname" .) .Release.Namespace }}
    - {{ printf "%s-webhook.%s.svc.cluster.local" (include "soteria-ip-rewrite.fullname" .) .Release.Namespace }}
```

#### Self-Signed Issuer (conditional)

Only rendered when `tls.createSelfSigned: true` AND `tls.issuerRef.name` is empty:

```yaml
{{- if and .Values.tls.createSelfSigned (not .Values.tls.issuerRef.name) }}
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: {{ printf "%s-selfsigned" (include "soteria-ip-rewrite.fullname" .) | trunc 63 | trimSuffix "-" }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "soteria-ip-rewrite.labels" . | nindent 4 }}
spec:
  selfSigned: {}
{{- end }}
```

When the parent Soteria chart includes this sub-chart, it can set `tls.issuerRef.name` to its own issuer (e.g., `soteria-ca`), and the self-signed Issuer is not created.

#### Deployment Template — Webhook Server Container

```yaml
containers:
  - name: webhook
    command:
      - /ip-rewrite-webhook
    args:
      - --port=9443
      - --cert-dir=/tmp/k8s-webhook-server/serving-certs
      - --init-container-image={{ printf "%s:%s" .Values.initContainer.image.repository (include "soteria-ip-rewrite.imageTag" (dict "tag" .Values.initContainer.image.tag "ctx" .)) }}
    image: "{{ .Values.webhook.image.repository }}:{{ include "soteria-ip-rewrite.imageTag" (dict "tag" .Values.webhook.image.tag "ctx" .) }}"
    imagePullPolicy: {{ .Values.webhook.image.pullPolicy }}
    ports:
      - containerPort: 9443
        name: webhook
        protocol: TCP
    securityContext:
      readOnlyRootFilesystem: true
      allowPrivilegeEscalation: false
      runAsNonRoot: true
      capabilities:
        drop: ["ALL"]
    livenessProbe:
      httpGet:
        path: /healthz
        port: 8081
      initialDelaySeconds: 15
      periodSeconds: 20
    readinessProbe:
      httpGet:
        path: /readyz
        port: 8081
      initialDelaySeconds: 5
      periodSeconds: 10
    resources:
      {{- toYaml .Values.webhook.resources | nindent 12 }}
    volumeMounts:
      - name: webhook-server-cert
        mountPath: /tmp/k8s-webhook-server/serving-certs
        readOnly: true
```

#### Integration with Main Soteria Chart (AC8)

Add to `charts/soteria/Chart.yaml`:

```yaml
dependencies:
  - name: soteria-ip-rewrite
    version: "0.1.0"
    repository: "file://../soteria-ip-rewrite"
    condition: ipRewrite.enabled
```

Add to `charts/soteria/values.yaml`:

```yaml
# -- IP Rewrite sub-chart (standalone VM IP reconfiguration webhook)
ipRewrite:
  enabled: false
  # Sub-chart values override — when enabled, override tls.issuerRef to share
  # the parent chart's cert-manager issuer:
  soteria-ip-rewrite:
    tls:
      issuerRef:
        name: soteria-ca
        kind: Issuer
      createSelfSigned: false
```

### Project Structure Notes

**New files created by this story:**

```
charts/soteria-ip-rewrite/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── _helpers.tpl
    ├── _validation.tpl
    ├── deployment.yaml
    ├── service.yaml
    ├── serviceaccount.yaml
    ├── mutatingwebhookconfiguration.yaml
    ├── certificate.yaml
    ├── issuer.yaml
    ├── scc.yaml
    ├── clusterrole-scc.yaml
    └── clusterrolebinding-scc.yaml

build/ip-rewrite-webhook/
└── Dockerfile
```

**Existing files modified by this story:**

```
charts/soteria/Chart.yaml              ← Add dependencies section
charts/soteria/values.yaml             ← Add ipRewrite.enabled + sub-chart overrides
```

**Alignment with existing project:**
- `charts/soteria-ip-rewrite/` sits alongside `charts/soteria/` — sibling chart pattern
- Template helpers use `soteria-ip-rewrite.*` prefix to avoid conflicts when used as sub-chart
- `build/ip-rewrite-webhook/Dockerfile` follows `build/ip-rewrite/Containerfile` (18.1) and root `Dockerfile` patterns
- Go module is `github.com/soteria-project/soteria` — the webhook binary is built from the same module

### Verification Commands

```bash
# Lint the standalone chart (AC7)
helm lint charts/soteria-ip-rewrite/ \
  --set webhook.image.tag=dev

# Template rendering verification — inspect all resources
helm template test-release charts/soteria-ip-rewrite/ \
  --set webhook.image.tag=dev \
  --namespace soteria-ip-rewrite | kubectl apply --dry-run=client -f -

# Template with SCC disabled (vanilla K8s)
helm template test-release charts/soteria-ip-rewrite/ \
  --set webhook.image.tag=dev \
  --set scc.enabled=false

# Template with custom issuer (sub-chart mode)
helm template test-release charts/soteria-ip-rewrite/ \
  --set webhook.image.tag=dev \
  --set tls.issuerRef.name=soteria-ca \
  --set tls.createSelfSigned=false

# Verify parent chart with sub-chart enabled
cd charts/soteria && helm dependency update
helm lint charts/soteria/ \
  --set site.name=test \
  --set ipRewrite.enabled=true

# Build webhook Dockerfile (smoke test)
docker build -t soteria-ip-rewrite-webhook:dev -f build/ip-rewrite-webhook/Dockerfile .
```

### Anti-Patterns / DO NOT

- **DO NOT create the webhook Go code** — that is Story 18.5 (`cmd/ip-rewrite-webhook/main.go`, `internal/webhook/iprewrite/handler.go`). This story creates only the Helm chart and Dockerfile.
- **DO NOT create the init container image or Containerfile** — that is Story 18.1 (`build/ip-rewrite/Containerfile`). This story references it in `values.yaml` only.
- **DO NOT create entrypoint or handler scripts** — those are Stories 18.2, 18.3, 18.4.
- **DO NOT modify `.github/workflows/ci.yml` or `release.yml`** — CI integration is Story 18.9.
- **DO NOT modify the `Makefile`** — build targets are Story 18.9's responsibility.
- **DO NOT use a different helper prefix** — always use `soteria-ip-rewrite.*` (not `soteria.*` or `ipRewrite.*`). This avoids name collisions when the chart is included as a sub-chart of the parent Soteria chart.
- **DO NOT hardcode namespace in cluster-scoped resources** — SCC, ClusterRole, and ClusterRoleBinding are cluster-scoped. Use the fullname template for uniqueness.
- **DO NOT make the SCC unconditional** — wrap SCC templates in `{{- if .Values.scc.enabled }}` for vanilla Kubernetes compatibility (no SCC API on non-OpenShift).
- **DO NOT create a separate ClusterRole for the webhook server RBAC** — the webhook server needs no cluster-level permissions (it just handles admission requests). The ClusterRole is only for SCC `use`.
- **DO NOT set `namespaceSelector` by default** — leave it empty (all namespaces) so the webhook works out of the box. Users can restrict via `values.yaml`.
- **DO NOT write unit or integration tests** — Story 18.7 covers tests. Story 18.10 covers documentation.
- **DO NOT add Helm test hooks** — `helm lint` validation is sufficient for this story.
- **DO NOT modify any existing webhook configuration** — `config/webhook/manifests.yaml` is the Soteria VM webhook. This chart creates a separate MWC.

### References

- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18" (~line 4198)]
- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Story 18.5 spec: `_bmad-output/implementation-artifacts/18-5-mutating-webhook-virt-launcher-init-container-injection.md`]
- [Existing Soteria Helm chart: `charts/soteria/` — helpers, values, deployment, webhook, certificate patterns]
- [Existing webhook configuration: `charts/soteria/templates/controller/webhookconfiguration.yaml`]
- [Existing webhook certificate: `charts/soteria/templates/controller/certificate-webhook.yaml`]
- [Existing webhook service: `charts/soteria/templates/controller/service-webhook.yaml`]
- [Root Dockerfile: `Dockerfile` — multi-stage Go build pattern for webhook binary]
- [OpenShift SCC documentation: SecurityContextConstraints API `security.openshift.io/v1`]
- [cert-manager CA injection: `cert-manager.io/inject-ca-from` annotation]
- [Helm sub-chart dependencies: `Chart.yaml` `dependencies:` with `condition:` field]

## Code Review Record

### Review Model Used
*(To be filled during code review — must differ from dev model)*

### Review Findings
*(To be filled during code review)*

### Decisions Needed / Decisions Taken
*(To be filled during code review)*

### Fixes Applied
*(To be filled during code review)*

## Dev Agent Record

### Agent Model Used

*(To be filled by dev agent)*

### Debug Log References

### Completion Notes List

### File List
