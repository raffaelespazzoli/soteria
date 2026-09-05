# Developer Setup

This guide walks you through cloning, building, testing, and running Soteria
locally. By the end you will have a working development environment with all
prerequisites installed and a local Kind cluster running the controller with the
no-op storage driver.

---

## Prerequisites

Install the following tools before you begin. The table lists the minimum
versions verified in CI.

| Tool | Minimum Version | Purpose |
|------|----------------|---------|
| **Go** | 1.25 (see `go.mod`) | Compile the operator and run tests |
| **Git** | 2.x | Clone the repository |
| **Make** | GNU Make 4.x | Build system — all commands go through the Makefile |
| **kubectl** | 1.28+ | Interact with Kubernetes clusters |
| **Kind** | latest | Local Kubernetes clusters for e2e and dev |
| **podman** (or Docker) | latest | Build container images (`CONTAINER_TOOL=podman` by default) |

!!! tip "Optional but recommended"
    - **golangci-lint** — downloaded automatically by `make lint`, but a global
      install gives faster IDE feedback.
    - **kubebuilder** 4.13.1+ — only needed if you scaffold new APIs or
      webhooks.
    - **operator-sdk** v1.42.2 — only needed for OLM bundle generation.
    - **Helm** 3.x — only needed for Helm chart development.
    - **Python** 3.12 + **pip** — only needed to build the documentation site
      locally (`pip install -r requirements-docs.txt && mkdocs serve`).

### Verify your setup

```bash
go version          # go1.25.x or later
make --version      # GNU Make 4.x
kubectl version --client
kind version
podman --version    # or: docker --version
```

---

## Clone and Build

### 1. Clone the repository

```bash
git clone https://github.com/soteria-project/soteria.git
cd soteria
```

### 2. Download Go dependencies

```bash
go mod download
```

### 3. Generate code and build

```bash
make manifests generate   # Regenerate RBAC, deepcopy, and OpenAPI
make build                # Compile bin/manager
```

`make build` runs `manifests`, `generate`, `fmt`, and `vet` automatically, so
a single `make build` is enough for most workflows.

---

## Makefile Targets Reference

The Makefile is the single entry point for all build, test, and deploy
operations. Run `make help` to see every target with a short description.

### Development

| Target | Description | When to Use |
|--------|-------------|-------------|
| `make manifests` | Regenerate RBAC ClusterRole and webhook configs from kubebuilder markers | After editing `*_types.go`, RBAC markers, or webhook markers |
| `make generate` | Regenerate deepcopy helpers and OpenAPI definitions via `hack/update-codegen.sh` | After editing `*_types.go` fields |
| `make fmt` | Run `go fmt` on all packages | Before committing — also run by `make build` |
| `make vet` | Run `go vet` on all packages | Before committing — also run by `make build` |
| `make lint` | Run golangci-lint (downloads binary if needed) | Before opening a PR |
| `make lint-fix` | Run golangci-lint with `--fix` | Fix auto-fixable lint issues |
| `make lint-config` | Verify golangci-lint configuration is valid | After editing `.golangci.yml` |

### Build

| Target | Description | When to Use |
|--------|-------------|-------------|
| `make build` | Compile the manager binary to `bin/manager` | Local development |
| `make run` | Run the controller from your host against the current kubeconfig | Local testing (requires `SITE_NAME` env var) |
| `make docker-build` | Build the container image (`IMG` variable, default `quay.io/raffaelespazzoli/soteria:latest`) | Before deploying or pushing |
| `make docker-push` | Push the container image to the registry | After building, before remote deploy |
| `make docker-buildx` | Multi-arch build and push (`linux/amd64,arm64,s390x,ppc64le`) | Release builds |
| `make build-installer` | Generate `dist/install.yaml` from Kustomize manifests | Distribution as a single YAML |

### Testing

| Target | Description | When to Use |
|--------|-------------|-------------|
| `make test` | Unit + envtest tests (excludes e2e and console-plugin) | Every code change |
| `make integration` | Integration tests (envtest for controllers, ScyllaDB via testcontainers) | Before merging — needs podman running |
| `make setup-test-e2e` | Create a Kind cluster named `soteria-test-e2e` for e2e tests | One-time setup before e2e |
| `make test-e2e` | Run end-to-end tests on a Kind cluster (creates the cluster if needed) | Full validation before release |
| `make cleanup-test-e2e` | Delete the Kind e2e cluster | After e2e testing |
| `make helmchart-test` | Lint and test the Helm chart | After Helm chart changes |

### Deployment

| Target | Description | When to Use |
|--------|-------------|-------------|
| `make install` | Install CRDs into the current cluster | Setting up a dev cluster |
| `make uninstall` | Remove CRDs from the current cluster | Cleanup |
| `make deploy` | Deploy the controller to the current cluster | Deploy for testing |
| `make undeploy` | Remove the controller from the current cluster | Cleanup |
| `make dev-cluster` | Create a local Kind cluster named `soteria-dev` | Contributor onboarding / local development |

### OLM Bundle

| Target | Description | When to Use |
|--------|-------------|-------------|
| `make bundle` | Generate OLM bundle manifests | Preparing for OperatorHub |
| `make bundle-build` | Build the OLM bundle image | OLM release |
| `make bundle-push` | Push the OLM bundle image | OLM release |

### Helm Chart

| Target | Description | When to Use |
|--------|-------------|-------------|
| `make helmchart` | Render Helm chart from Kustomize manifests | After manifest changes |
| `make helmchart-test` | Lint and test Helm chart | Validate chart correctness |

---

## Testing Pyramid

Soteria uses a layered testing strategy. Each layer builds on the one below and
catches different classes of bugs.

```
┌──────────────────────────────────────────────┐
│            E2E Tests (Kind cluster)          │  Full operator on real K8s
├──────────────────────────────────────────────┤
│     Multisite Tests (envtest, 2 clusters)    │  Cross-site coordination
├──────────────────────────────────────────────┤
│  Integration Tests (envtest + testcontainers)│  Controller + ScyllaDB + webhooks
├──────────────────────────────────────────────┤
│     Driver Conformance (standard testing)    │  Full DR lifecycle per driver
├──────────────────────────────────────────────┤
│         Unit Tests (envtest or pure Go)      │  Business logic, state machine
└──────────────────────────────────────────────┘
```

### Unit tests

**Run:** `make test`

Unit tests live in `_test.go` files co-located with the code they test. Most
controller and API server tests use
[envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest) which
spins up a real local etcd and kube-apiserver — this is intentional because
the fake client does not handle `resourceVersion`, UIDs, timestamps, webhooks,
or status subresources correctly.

Pure logic tests (state machine, chunking math, wave formation) use the
standard `testing` package with table-driven patterns.

```bash
# Run all unit tests with coverage
make test

# Run tests for a single package
go test ./pkg/engine/...
```

### Driver conformance tests

**Run:** `go test ./pkg/drivers/conformance/...`

The conformance suite at `pkg/drivers/conformance/suite.go` validates any
`StorageProvider` implementation against the full DR lifecycle contract:

- **Lifecycle** — Create → SetSource → GetReplicationStatus → StopReplication →
  Delete → verify not found
- **Idempotency** — every method called twice must succeed both times
- **Context cancellation** — pre-cancelled context must return an error
  immediately
- **Error conditions** — operations on nonexistent volume groups must return
  `ErrVolumeGroupNotFound`

To wire a new driver, create a test file and call `conformance.RunConformance`:

```go
func TestConformance(t *testing.T) {
    conformance.RunConformance(t, mydriver.New())
}
```

### Integration tests

**Run:** `make integration`

Integration tests live in `test/integration/` and cover:

| Sub-directory | What it tests |
|--------------|---------------|
| `storage/` | ScyllaDB storage interface, CDC Watch, KV operations (needs testcontainers) |
| `controller/` | DRPlan and DRExecution controller reconciliation with envtest |
| `apiserver/` | Aggregated API server admission and CRUD operations |
| `admission/` | VM admission webhooks |
| `replication/` | Volume replication lifecycle |
| `rbac/` | RBAC policy enforcement |

!!! warning "Podman requirement"
    Integration tests that use testcontainers (ScyllaDB) require a running
    podman socket. Start it with:

    ```bash
    systemctl --user start podman.socket
    ```

    You may also need to increase inotify limits:

    ```bash
    sudo sysctl -w fs.inotify.max_user_instances=1024
    ```

### Multisite tests

**Run:** `go test ./test/multisite/...`

Multisite tests in `test/multisite/` simulate a two-cluster DR environment to
validate cross-site coordination, convergence, and lifecycle flows. They use
envtest with two separate API server instances.

### End-to-end tests

**Run:** `make test-e2e`

E2E tests in `test/e2e/` run the full operator on a real Kubernetes cluster
using [Kind](https://kind.sigs.k8s.io/). The Makefile creates an isolated
Kind cluster named `soteria-test-e2e`, runs the Ginkgo test suite, then tears
the cluster down.

!!! danger "Isolated cluster required"
    E2E tests **must** run against a dedicated Kind cluster — never your real
    dev or production cluster. The tests create and delete resources freely.

```bash
# Full e2e lifecycle (create cluster → test → cleanup)
make test-e2e

# Or step by step:
make setup-test-e2e    # Create Kind cluster
KIND=kind KIND_CLUSTER=soteria-test-e2e go test -tags=e2e ./test/e2e/ -v -ginkgo.v
make cleanup-test-e2e  # Delete Kind cluster
```

---

## CI Pipeline

Every push and pull request triggers several GitHub Actions workflows. Here is
what runs and when.

### On every push and PR

| Workflow | File | What it does |
|----------|------|-------------|
| **Tests** | `.github/workflows/test.yml` | `go mod tidy` → `make test` (unit + envtest) |
| **Lint** | `.github/workflows/lint.yml` | `make lint-config` → `make lint` (golangci-lint) |
| **E2E Tests** | `.github/workflows/test-e2e.yml` | Install Kind → `make test-e2e` (full cluster) |

### On PRs to `main`

| Workflow | File | What it does |
|----------|------|-------------|
| **PR Operator** | `.github/workflows/pr-operator.yml` | Linting + codegen verification (`hack/verify-codegen.sh`) + reusable `redhat-cop/github-workflows-operators` PR pipeline (unit tests, integration tests, Helm chart test, multi-arch build) |

The PR operator workflow skips `**.md` and `docs/**` paths — documentation-only
PRs are not blocked by Go build checks.

### On PRs touching `docs/`

| Workflow | File | What it does |
|----------|------|-------------|
| **Documentation** | `.github/workflows/docs.yml` | Build the MkDocs site with `--strict` mode to catch broken links and warnings |

### On tag push (`v*`)

| Workflow | File | What it does |
|----------|------|-------------|
| **Release Operator** | `.github/workflows/release-operator.yml` | Build + push controller image via `redhat-cop/github-workflows-operators`, publish to OperatorHub |
| **Release** | `.github/workflows/release.yml` | Build + push console-plugin and standalone-ui images, publish Helm chart to GitHub Pages via chart-releaser |

### What must pass before merge

At a minimum, a PR must pass:

1. **Lint** — golangci-lint with the project's configuration
2. **Unit + envtest tests** — `make test`
3. **Codegen verification** — `hack/verify-codegen.sh` confirms generated code is up to date
4. **E2E tests** — full operator lifecycle on a Kind cluster

---

## Local Development

### Quick start with `make dev-cluster`

The fastest way to get a local environment is:

```bash
make dev-cluster   # Creates a Kind cluster named "soteria-dev"
make deploy        # Deploy the controller (uses the no-op driver by default)
```

### Running the controller locally

For a faster development loop, run the controller directly on your host instead
of inside the cluster:

```bash
# Ensure your kubeconfig points to a development cluster
make run SITE_NAME=site-a
```

This runs `go run ./cmd/soteria/ --site-name=site-a` after regenerating
manifests and running `fmt` and `vet`. The controller connects to whatever
cluster your kubeconfig points to.

!!! tip "Hot reload"
    `make run` compiles and starts the controller in one step. For a faster
    feedback loop, use a file-watcher like
    [air](https://github.com/air-verse/air) or simply re-run `make run` after
    changes.

### The no-op driver

The **no-op driver** (`pkg/drivers/noop/`) implements the full 7-method
`StorageProvider` interface without performing actual storage operations. It
tracks volume groups and replication roles in memory so that the entire
DR lifecycle (create → failover → reprotect → failback) works end-to-end
without any real storage infrastructure.

The no-op driver is the default — it registers itself as both `noop.soteria.io`
and `noop`, and is also the **fallback driver**: any CSI provisioner not claimed
by a real storage driver falls through to no-op. This means you can develop and
test the full workflow engine, controllers, and UI without configuring Dell,
ODF, or any other vendor driver.

```bash
# Run locally with no-op driver (no special flags needed)
make run SITE_NAME=site-a

# Or in a Kind cluster
make dev-cluster
make deploy
```

### Container builds

Soteria uses **podman** by default. To use Docker instead, override the
`CONTAINER_TOOL` variable:

```bash
# Build with podman (default)
make docker-build

# Build with docker
make docker-build CONTAINER_TOOL=docker

# Load into Kind (no push needed)
kind load docker-image quay.io/raffaelespazzoli/soteria:latest --name soteria-dev
```

---

## Project Structure

```
cmd/soteria/              Entry point — single binary (API server + controller)
pkg/
├── apis/                 CRD types (soteria.io/v1alpha1)
├── apiserver/            Aggregated API server (ScyllaDB-backed storage)
├── controller/           Reconciliation logic (DRPlan, DRExecution, ShadowPV, VolumeReplication)
├── drivers/              StorageProvider interface + implementations
│   ├── interface.go      Driver contract (7 methods)
│   ├── conformance/      Conformance test suite for drivers
│   ├── noop/             No-op driver (development/CI)
│   ├── fake/             Fake driver (unit tests)
│   └── csiextension/     CSI extension driver
├── engine/               Workflow engine (wave executor, DRGroup chunking)
├── admission/            Webhook validation
├── metrics/              Prometheus metrics
├── registry/             API server registry strategies
├── storage/scylladb/     ScyllaDB storage backend
└── util/                 Shared utilities
internal/
└── preflight/            Preflight checks
test/
├── integration/          Integration tests (envtest + testcontainers)
├── multisite/            Cross-site coordination tests
├── e2e/                  End-to-end tests (Kind cluster)
└── utils/                Shared test helpers
config/                   Kubernetes manifests (Kustomize)
console-plugin/           OpenShift Console plugin (TypeScript/React)
charts/                   Helm chart
docs/                     Documentation site (MkDocs Material)
hack/                     Scripts (codegen, verification)
```

---

## Common Workflows

### After editing `*_types.go`

```bash
make manifests generate   # Regenerate RBAC + deepcopy + OpenAPI
make test                 # Verify nothing broke
```

### After editing any Go file

```bash
make lint-fix             # Auto-fix lint issues
make test                 # Run the test suite
```

### Before opening a PR

```bash
make lint                 # Full lint check
make test                 # Unit + envtest tests
hack/verify-codegen.sh    # Verify generated code is up to date
make test-e2e             # E2E tests on Kind (optional but recommended)
```

### Scaffolding new resources

Always use the CLI — never create API files manually:

```bash
# New API + controller
kubebuilder create api --group soteria.io --version v1alpha1 --kind MyKind

# New webhook
kubebuilder create webhook --group soteria.io --version v1alpha1 --kind MyKind \
  --defaulting --programmatic-validation
```
