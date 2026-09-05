# Story 16.7: Multi-Cluster Install Script

Status: done

## Story

As a platform engineer,
I want an install script that orchestrates a two-cluster Soteria deployment end-to-end,
so that I can set up a complete multi-site DR environment with a single command.

## Acceptance Criteria

**AC1: Script accepts required flags**
Given the script `scripts/install-soteria.sh`
When I invoke it with `--help`
Then it displays usage for `--east-context`, `--west-context`, `--networking` (cilium|submariner), `--ui-mode` (console-plugin|standalone|none), `--values-file`, and `--chart` (path or repo URL)

**AC2: Install sequence is correct**
Given valid contexts, chart, and values
When I run the install script
Then it performs: helm install on seed cluster → wait for CA cert to be ready → copy CA Secret to joining cluster → helm install on joining cluster with externalSeeds pointing to seed

**AC3: Uninstall teardown in reverse order**
Given Soteria is installed on both clusters
When I run the script with `--uninstall`
Then it performs helm uninstall on the joining cluster first, then the seed cluster

**AC4: Script is idempotent**
Given Soteria is already installed on both clusters
When I re-run the install script
Then it completes successfully without errors (helm upgrade or equivalent)

**AC5: Prerequisite validation**
Given the script is invoked
When prerequisites are missing (helm, kubectl, cert-manager CRDs, or scylla-operator CRDs when in managed mode)
Then the script exits with a clear error message listing missing prerequisites

**AC6: Script is executable and self-documented**
Given the script exists
When I inspect it
Then it has a shebang line, is executable, and contains usage documentation

## Tasks / Subtasks

- [x] Task 1: Create script skeleton (AC: 1, 6)
  - [x] 1.1: Create `scripts/install-soteria.sh` with `#!/usr/bin/env bash`, `set -euo pipefail`
  - [x] 1.2: Implement `usage()` function displaying all flags and descriptions
  - [x] 1.3: Implement flag parsing for `--east-context`, `--west-context`, `--networking`, `--ui-mode`, `--values-file`, `--chart`, `--uninstall`, `--namespace`
  - [x] 1.4: Make script executable (`chmod +x`)
- [x] Task 2: Implement prerequisite validation (AC: 5)
  - [x] 2.1: Check for `helm` and `kubectl` commands
  - [x] 2.2: Verify cluster reachability via `kubectl cluster-info` for both contexts
  - [x] 2.3: Check `cert-manager.io` CRDs on both clusters (`certificates.cert-manager.io`)
  - [x] 2.4: Check `scylla-operator` CRDs when `scylladb.mode=managed` (`scyllaclusters.scylla.scylladb.com`)
  - [x] 2.5: Check Submariner MCS CRDs when `--networking=submariner` (`serviceexports.multicluster.x-k8s.io`)
- [x] Task 3: Implement seed cluster install (AC: 2, 4)
  - [x] 3.1: Run `helm upgrade --install` on east context (seed cluster) with values file
  - [x] 3.2: Set `--set site.name=<east-context>,site.role=seed`
  - [x] 3.3: Wait for controller Deployment rollout (`kubectl rollout status --timeout=300s`)
- [x] Task 4: Implement CA Secret copy (AC: 2)
  - [x] 4.1: Wait for CA cert Secret to be ready on seed cluster (poll `soteria-ca-secret` up to 5 minutes)
  - [x] 4.2: Export Secret JSON, strip metadata (resourceVersion, uid, creationTimestamp, managedFields)
  - [x] 4.3: Apply Secret to joining cluster namespace with `--server-side --force-conflicts`
- [x] Task 5: Implement joining cluster install (AC: 2, 4)
  - [x] 5.1: Run `helm upgrade --install` on west context (joining cluster)
  - [x] 5.2: Set `--set site.name=<west-context>,site.role=joining,scylladb.managed.externalSeeds[0]=<seed-scylladb-service-dns>`
  - [x] 5.3: Wait for controller Deployment rollout
- [x] Task 6: Implement uninstall (AC: 3)
  - [x] 6.1: Run `helm uninstall` on joining cluster first
  - [x] 6.2: Run `helm uninstall` on seed cluster second
  - [x] 6.3: Optionally clean up CA Secret and namespace
- [x] Task 7: Implement idempotency (AC: 4)
  - [x] 7.1: Use `helm upgrade --install` (not plain `helm install`) for all install commands
  - [x] 7.2: Use `kubectl apply --server-side --force-conflicts` for Secret copy
- [x] Task 8: Add integration/dry-run mode (AC: 4)
  - [x] 8.1: Add `--dry-run` flag that prints all helm/kubectl commands without executing

## Dev Notes

### Implementation Pattern

Follow the deployment patterns from `hack/stretched-local-test.sh` and `hack/multisite/deploy-soteria.sh`.

**Script configuration pattern** (from `hack/stretched-local-test.sh`):
```bash
set -euo pipefail
IMG="${IMG:-quay.io/soteria-project/soteria:latest}"
NAMESPACE="soteria"
CTX_EAST="${CTX_EAST:-east}"
CTX_WEST="${CTX_WEST:-west}"
```

**Prerequisite validation pattern** (from `hack/stretched-local-test.sh`):
```bash
for ctx in "${CTX_EAST}" "${CTX_WEST}"; do
  if ! kubectl --context="${ctx}" cluster-info &>/dev/null; then
    echo "Error: cannot reach cluster via context '${ctx}'" >&2; exit 1
  fi
  if ! kubectl --context="${ctx}" get crd certificates.cert-manager.io &>/dev/null; then
    echo "Error: cert-manager CRD not found on ${ctx}" >&2; exit 1
  fi
done
```

**CA Secret copy pattern** (from `hack/multisite/setup-scylladb.sh`):
```bash
kubectl --context "${EAST_CONTEXT}" -n "${NAMESPACE}" get secret soteria-ca-key-pair -o json \
  | jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp,
            .metadata.managedFields, .metadata.annotations["cert-manager.io/certificate-name"])' \
  | kubectl --context "${WEST_CONTEXT}" -n "${NAMESPACE}" apply --server-side --force-conflicts -f -
```

**Wait-for-CA pattern** (from `hack/multisite/setup-scylladb.sh`):
```bash
for _ in $(seq 1 60); do
  ready=$(kubectl --context "${ctx}" -n "${NAMESPACE}" \
    get certificate soteria-ca -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
  if [[ "${ready}" == "True" ]]; then break; fi
  sleep 2
done
```

**Install sequence** (Helm-based, replacing kustomize):
1. `helm upgrade --install soteria <chart> --kube-context <east> -f values.yaml --set site.name=east,site.role=seed -n soteria --create-namespace --wait --timeout 10m`
2. Wait for CA Secret
3. Copy CA Secret to west
4. `helm upgrade --install soteria <chart> --kube-context <west> -f values.yaml --set site.name=west,site.role=joining --set scylladb.managed.externalSeeds[0]=soteria-scylladb-client.soteria.svc.clusterset.local -n soteria --create-namespace --wait --timeout 10m`

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `scripts/install-soteria.sh` | NEW | Multi-cluster Helm install/uninstall script |

### Key Constraints

- The script must work with both local chart paths and remote Helm repository URLs
- Use `helm upgrade --install` for idempotency (not `helm install`)
- CA Secret copy requires `jq` — add to prerequisite checks
- The seed-to-joining externalSeeds DNS varies by networking mode: Submariner uses `*.svc.clusterset.local`, Cilium uses direct PodIP
- Uninstall must be reverse order: joining cluster first, then seed
- Depends on: Story 16.2 (controller templates), Story 16.3 (ScyllaDB managed templates)

### Project Structure Notes

- Script goes in `scripts/` directory (not `hack/` which is for development tooling)
- This script replaces the deployment logic in `hack/stretched-local-test.sh` and `hack/multisite/deploy-soteria.sh` with a Helm-based approach

### References

- [Source: `hack/stretched-local-test.sh` — complete two-cluster deployment with Submariner, CA copy, ScyllaDB convergence, console plugin setup]
- [Source: `hack/multisite/deploy-soteria.sh` — Minikube two-cluster deployment with Cilium, image loading, smoke test]
- [Source: `hack/multisite/setup-scylladb.sh` — ScyllaDB cross-DC setup with shared CA creation, Secret copy, operator install, convergence test]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6

### Debug Log References

### Completion Notes List
- Created `scripts/install-soteria.sh` — 350+ line multi-cluster install/uninstall script
- Implements full two-cluster Helm deployment: seed install → CA wait → CA copy → joining install
- Follows patterns from `hack/stretched-local-test.sh` and `hack/multisite/setup-scylladb.sh`
- Prerequisite checks: helm, kubectl, jq, cluster reachability, cert-manager CRDs, scylla-operator CRDs, Submariner MCS CRDs
- Idempotent via `helm upgrade --install` and `kubectl apply --server-side --force-conflicts`
- Uninstall in reverse order: joining cluster → seed cluster → CA cleanup
- `--dry-run` flag prints all commands without executing
- All acceptance criteria satisfied (AC1-AC6)
- Validated: bash syntax check, --help output, --dry-run install/uninstall, missing flags error, invalid enum error
- All existing Go tests pass (no regressions)

### File List
| File | Action |
|------|--------|
| `scripts/install-soteria.sh` | NEW |
| `_bmad-output/implementation-artifacts/16-7-install-script.md` | MODIFIED |
