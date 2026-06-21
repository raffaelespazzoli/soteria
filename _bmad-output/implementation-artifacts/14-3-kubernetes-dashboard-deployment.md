# Story 14.3: Kubernetes Dashboard Deployment

Status: ready-for-dev

## Story

As a platform engineer,
I want the Kubernetes Dashboard deployed on both Kind clusters,
so that I can visually troubleshoot pods, services, PVCs, and other resources during integration testing.

## Acceptance Criteria

**AC1: Dashboard deployment**
Given both Kind clusters from Story 14.1
When the dashboard setup script is executed
Then the Kubernetes Dashboard is deployed on both east and west clusters via Helm
And all Dashboard pods (api, auth, web, kong, metrics-scraper) are running

**AC2: Admin access configuration**
Given the Dashboard is deployed
When an admin ServiceAccount and ClusterRoleBinding are created
Then the Dashboard is accessible with full cluster-admin privileges for development use
And an access token is generated for login

**AC3: Access documentation**
Given the Dashboard is running
When the user follows the documented access pattern
Then the Dashboard UI is accessible via `kubectl port-forward` to the Kong proxy service
And the setup script prints access instructions (URL, port-forward command, and token retrieval command)

**AC4: Smoke test**
Given the Dashboard is deployed and accessible
When the setup script verifies readiness
Then all Dashboard pods are in Running state on both clusters

## Tasks / Subtasks

- [ ] Task 1: Create `hack/multisite/setup-dashboard.sh` (AC: 1, 2, 3, 4)
  - [ ] 1.1: Env-var configuration block (`DASH_NS`, `DASH_HELM_VERSION`, cluster names/kubeconfig paths consistent with `setup-clusters.sh`)
  - [ ] 1.2: Prerequisite check (`helm` CLI available, both Kind clusters reachable)
  - [ ] 1.3: Add Helm repo `kubernetes-retired.github.io/dashboard/` and `helm repo update`
  - [ ] 1.4: `helm upgrade --install` on east cluster with Kong proxy as ClusterIP
  - [ ] 1.5: `helm upgrade --install` on west cluster (same)
  - [ ] 1.6: Wait for all Dashboard pods to be Running on both clusters
  - [ ] 1.7: Create admin ServiceAccount + ClusterRoleBinding on both clusters
  - [ ] 1.8: Print access instructions (port-forward command, token retrieval command, Dashboard URL)
- [ ] Task 2: Update `hack/multisite/README.md` (AC: 3)
  - [ ] 2.1: Add Dashboard section with prerequisites, usage, access instructions, troubleshooting

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — a single shell script and a README update, no Go code. The output is `hack/multisite/setup-dashboard.sh` (~1 file + README update). This is a **troubleshooting aid** for the developer — no other stories depend on it.

### Critical: Helm Chart Repository URL Change (January 2026)

The `kubernetes/dashboard` GitHub repository was **archived in January 2026**. The Helm chart URL has moved:

- **OLD (broken, returns 404):** `https://kubernetes.github.io/dashboard/`
- **NEW (correct):** `https://kubernetes-retired.github.io/dashboard/`

Many online guides still reference the old URL. Use the new `kubernetes-retired` URL. The latest stable chart version is **7.14.0** (released October 2025).

```bash
helm repo add kubernetes-dashboard https://kubernetes-retired.github.io/dashboard/
helm repo update
```

### Dashboard 7.x Architecture

Dashboard v7.x has a multi-container architecture fronted by a **Kong API gateway**. A `helm upgrade --install` creates five pods in the `kubernetes-dashboard` namespace:

| Pod | Purpose |
|-----|---------|
| `kubernetes-dashboard-api` | REST API backend |
| `kubernetes-dashboard-auth` | Authentication handler |
| `kubernetes-dashboard-web` | Web UI frontend |
| `kubernetes-dashboard-kong` | Kong API gateway (proxy) |
| `kubernetes-dashboard-metrics-scraper` | Resource usage metrics |

All access goes through the Kong proxy service. Port-forward to the `kubernetes-dashboard-kong-proxy` Service on port `443`:

```bash
kubectl port-forward svc/kubernetes-dashboard-kong-proxy 8443:443 -n kubernetes-dashboard
```

Then navigate to `https://localhost:8443`.

### Helm Installation Command

```bash
DASH_NS="${DASH_NS:-kubernetes-dashboard}"

helm upgrade --install kubernetes-dashboard \
  kubernetes-dashboard/kubernetes-dashboard \
  --create-namespace \
  --namespace "${DASH_NS}" \
  --set kong.proxy.type=ClusterIP \
  --set metricsScraper.enabled=true \
  --set metrics-server.enabled=false
```

Key Helm values:
- `kong.proxy.type=ClusterIP` — Kind has no cloud LB; access via `kubectl port-forward`
- `metricsScraper.enabled=true` — provides resource usage graphs in the UI
- `metrics-server.enabled=false` — do not install bundled metrics-server (it may conflict with one installed later, and Kind clusters may already have one)

### Admin RBAC for Development

Create a ServiceAccount with `cluster-admin` privileges (acceptable for local Kind development only):

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: dashboard-admin
  namespace: kubernetes-dashboard
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: dashboard-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: dashboard-admin
    namespace: kubernetes-dashboard
```

Generate the login token:

```bash
kubectl create token dashboard-admin -n kubernetes-dashboard --duration=24h
```

### Existing Script Conventions to Follow

Follow the same conventions established by `hack/stretched-local-test.sh` and planned for `setup-clusters.sh` (Story 14.1) and `setup-rook-ceph.sh` (Story 14.2):

- `set -euo pipefail` at top
- Env-var-driven configuration block at top of script
- `kctl()` helper wrapping `kubectl` with explicit `--kubeconfig` or `--context` for each cluster
- Idempotent operations (`helm upgrade --install` is naturally idempotent; check before create for RBAC)
- Status messages for each step
- Prerequisite checks (helm, kubectl, cluster reachability)
- Consistent cluster naming: `east` and `west` (context names `kind-east` and `kind-west`)

### Kubeconfig / Context Pattern

The script must use the same kubeconfig/context pattern as `setup-clusters.sh`:

```bash
EAST_CONTEXT="${EAST_CONTEXT:-kind-east}"
WEST_CONTEXT="${WEST_CONTEXT:-kind-west}"

kctl_east() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kctl_west() { kubectl --context "${WEST_CONTEXT}" "$@"; }
```

### Idempotency

- `helm upgrade --install` is naturally idempotent (upgrades if already installed)
- ServiceAccount/ClusterRoleBinding: use `kubectl apply` (idempotent) rather than `kubectl create`
- Script is safe to re-run without side effects

### Dependency

This story depends only on **Story 14.1** (Kind clusters must be running). It does NOT require Rook-Ceph (14.2), ScyllaDB (14.4), or KubeVirt (14.5). No other stories depend on this — it is a standalone troubleshooting utility.

### File Structure

```
hack/multisite/
├── setup-dashboard.sh    # Dashboard setup script (NEW — this story)
└── README.md             # Updated with Dashboard section (MODIFY)
```

### Testing Standards

No Go tests for this story — validation is via AC4 (pod readiness check in the setup script itself). The script verifies that all Dashboard pods reach Running state on both clusters.

### Potential Failure Modes

1. **Helm repo add fails with 404** — Using the old `kubernetes.github.io/dashboard/` URL. Must use `kubernetes-retired.github.io/dashboard/`
2. **Kong pod OOMKilled** — Kong has non-trivial memory requirements (~256Mi). If Kind nodes are severely resource-constrained, reduce Kong resource limits via Helm values
3. **Port-forward connection refused** — The Kong proxy service takes ~30s after pod Ready to accept connections. Retry with backoff
4. **Token command fails** — `kubectl create token` requires Kubernetes 1.24+. Kind clusters should always meet this requirement

### Project Structure Notes

- Script goes in `hack/multisite/` — follows the convention from Stories 14.1 and 14.2
- Standalone script (not part of `setup-clusters.sh` or `setup-rook-ceph.sh`) since the Dashboard is optional and independent
- README update adds a "Kubernetes Dashboard" section with access instructions

### References

- [Source: epics.md#Story 14.3] — acceptance criteria and technical notes
- [Source: hack/stretched-local-test.sh] — existing multi-site deployment pattern (script conventions)
- [Source: Story 14.1] — Kind cluster provisioning (cluster names, contexts, kubeconfig pattern)
- [Source: Story 14.2] — Rook-Ceph setup script conventions (same env-var and helper patterns)
- [Source: kubernetes-retired/dashboard GitHub] — https://github.com/kubernetes/dashboard (archived Jan 2026)
- [Source: Helm chart ArtifactHub] — https://artifacthub.io/packages/helm/k8s-dashboard/kubernetes-dashboard (v7.14.0)
- [Source: Retired chart repo] — https://kubernetes-retired.github.io/dashboard/

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
