# Story 1.7: CI Pipeline & OLM Packaging

Status: ready-for-dev

## Story

As a platform engineer,
I want to install Soteria from OperatorHub via OLM, and as a contributor, I want automated CI to validate my changes,
so that installation is standard and contributions are verified automatically.

## Acceptance Criteria

1. **Given** the project repository, **When** a pull request is opened, **Then** GitHub Actions runs the PR pipeline reusing `redhat-cop/github-workflows-operators`, **And** the pipeline executes `make test` (unit + envtest), `make lint` (golangci-lint), and `hack/verify-codegen.sh`, **And** the pipeline builds multi-arch container images for `linux/amd64`, `linux/arm64`, `linux/ppc64le`, **And** pipeline failures block merge.

2. **Given** the operator binary and Console plugin images, **When** `operator-sdk generate bundle` is run (standalone CLI, not scaffolding), **Then** an OLM bundle is generated in `bundle/` with a valid ClusterServiceVersion, **And** `operator-sdk bundle validate` passes with no errors, **And** the CSV declares ScyllaDB and cert-manager as prerequisites, **And** the APIService registration is included in bundle manifests.

3. **Given** a generated OLM bundle, **When** the bundle is published to an OperatorHub catalog, **Then** a platform engineer can install Soteria from the OCP OperatorHub UI, **And** OLM manages the operator lifecycle (install, upgrade, uninstall) per NFR16.

4. **Given** the Makefile, **When** reviewing available targets, **Then** `make test`, `make integration`, `make helmchart-test`, `make lint`, `make manifests`, `make run`, and `make dev-cluster` targets exist, **And** `make integration` runs ScyllaDB integration tests via testcontainers, **And** `make helmchart-test` is compatible with the redhat-cop pipeline.

5. **Given** the release pipeline, **When** a release tag is pushed, **Then** multi-arch container images are built and published, **And** the OLM bundle image is built from `bundle.Dockerfile` and published.

## Tasks / Subtasks

- [ ] Task 1: Create PR workflow (AC: #1)
  - [ ] 1.1 Replace placeholder `.github/workflows/pr-operator.yml` with a caller workflow that uses `redhat-cop/github-workflows-operators/.github/workflows/pr-operator.yml@v1`
  - [ ] 1.2 Configure workflow inputs: `GO_VERSION` matching `go.mod`, `BUILD_PLATFORMS: "linux/amd64,linux/arm64,linux/ppc64le"`, `RUN_UNIT_TESTS: true`, `RUN_INTEGRATION_TESTS: true`, `RUN_HELMCHART_TEST: true`
  - [ ] 1.3 Add a pre-job step that runs `hack/verify-codegen.sh` and `make lint` as separate jobs before the reusable workflow call
  - [ ] 1.4 Configure branch protection rules documentation noting that the PR workflow must pass before merge

- [ ] Task 2: Create release workflow (AC: #5)
  - [ ] 2.1 Replace placeholder `.github/workflows/release-operator.yml` with a caller workflow that uses `redhat-cop/github-workflows-operators/.github/workflows/release-operator.yml@v1`
  - [ ] 2.2 Configure trigger on tag push matching `v*` pattern
  - [ ] 2.3 Configure workflow inputs matching the PR workflow: `GO_VERSION`, `BUILD_PLATFORMS`, `RUN_UNIT_TESTS: true`
  - [ ] 2.4 Configure required secrets: `REGISTRY_USERNAME`, `REGISTRY_PASSWORD`, `COMMUNITY_OPERATOR_PAT`
  - [ ] 2.5 Set `OPERATOR_SDK_VERSION` to `v1.42.2` (March 2026, matching architecture doc)

- [ ] Task 3: Create `ci.Dockerfile` (AC: #1, #5)
  - [ ] 3.1 Create `ci.Dockerfile` — a runtime-only Dockerfile that copies a pre-built binary (expected by redhat-cop workflow which builds the binary externally via `make` then builds the image with `ci.Dockerfile`)
  - [ ] 3.2 Base image: Red Hat UBI9 minimal (`registry.access.redhat.com/ubi9/ubi-minimal:latest`)
  - [ ] 3.3 Copy `bin/manager` as the entrypoint binary
  - [ ] 3.4 Set standard OLM labels (name, summary, description, vendor)

- [ ] Task 4: Update Makefile with all required targets (AC: #4)
  - [ ] 4.1 Verify `make test` target runs unit tests + envtest (kubebuilder default, should exist from Story 1.1)
  - [ ] 4.2 Verify `make lint` target runs golangci-lint (kubebuilder default, should exist from Story 1.1)
  - [ ] 4.3 Verify `make integration` target runs `go test ./test/integration/... -tags=integration` with testcontainers (added in Story 1.1)
  - [ ] 4.4 Verify `make helmchart-test` target exists (added in Story 1.1) — implement as `helmchart` rendering + validation with `helm lint`
  - [ ] 4.5 Verify `make manifests` target regenerates RBAC and webhook configs (kubebuilder default)
  - [ ] 4.6 Verify `make run` target runs the operator locally (kubebuilder default)
  - [ ] 4.7 Verify `make dev-cluster` target exists (added in Story 1.1) — implement as kind/microshift cluster setup with no-op driver
  - [ ] 4.8 Add `make bundle` target — runs `kustomize build config/manifests | operator-sdk generate bundle --version $(VERSION) --default-channel $(DEFAULT_CHANNEL)` followed by `operator-sdk bundle validate ./bundle`
  - [ ] 4.9 Add `make bundle-build` target — `docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .`
  - [ ] 4.10 Add `make helmchart` target — renders the Helm chart from kustomize manifests for redhat-cop pipeline compatibility
  - [ ] 4.11 Add `make generate` target — runs controller-gen and codegen if not already present
  - [ ] 4.12 Ensure the default `make` (no target) builds `bin/manager` as the binary name (expected by redhat-cop pipeline)

- [ ] Task 5: Generate OLM bundle (AC: #2, #3)
  - [ ] 5.1 Create `config/manifests/kustomization.yaml` — Kustomize overlay that combines RBAC, webhook, and deployment manifests for OLM input
  - [ ] 5.2 Run `make bundle VERSION=0.0.1 IMG=quay.io/soteria-project/soteria:latest` to generate initial bundle
  - [ ] 5.3 Edit `bundle/manifests/soteria.clusterserviceversion.yaml` — add metadata:
    - displayName: "Soteria DR Orchestrator"
    - description: storage-agnostic disaster recovery orchestrator for OpenShift Virtualization
    - maturity: alpha
    - provider: Soteria Project
    - maintainers with project contact
    - icon (placeholder base64 PNG)
    - keywords: disaster-recovery, openshift-virtualization, dr, failover
    - links: repository, documentation
  - [ ] 5.4 Add ScyllaDB prerequisite to CSV — `spec.customresourcedefinitions.required` referencing ScyllaCluster CRD from `scylla.scylladb.com`
  - [ ] 5.5 Add cert-manager prerequisite to CSV — `spec.customresourcedefinitions.required` referencing Certificate CRD from `cert-manager.io`
  - [ ] 5.6 Add APIService registration to bundle manifests — `bundle/manifests/apiservice.yaml` declaring `v1alpha1.soteria.io` as an aggregated API
  - [ ] 5.7 Configure `spec.installModes` — OwnNamespace: true, SingleNamespace: true, MultiNamespace: false, AllNamespaces: true
  - [ ] 5.8 Verify `operator-sdk bundle validate ./bundle --select-optional name=operatorhub` passes
  - [ ] 5.9 Verify `bundle/metadata/annotations.yaml` has correct channel and package metadata

- [ ] Task 6: Update `bundle.Dockerfile` (AC: #2, #5)
  - [ ] 6.1 Replace placeholder `bundle.Dockerfile` (from Story 1.1) with proper OLM bundle Dockerfile
  - [ ] 6.2 Add OLM LABEL annotations: `operators.operatorframework.io.bundle.mediatype.v1`, `operators.operatorframework.io.bundle.manifests.v1`, `operators.operatorframework.io.bundle.metadata.v1`, `operators.operatorframework.io.bundle.package.v1=soteria`, `operators.operatorframework.io.bundle.channels.v1=alpha`
  - [ ] 6.3 COPY `bundle/manifests/` and `bundle/metadata/` into the image

- [ ] Task 7: Final validation
  - [ ] 7.1 `make build` passes
  - [ ] 7.2 `make test` passes
  - [ ] 7.3 `make lint` passes
  - [ ] 7.4 `make bundle` generates valid bundle and `operator-sdk bundle validate` passes
  - [ ] 7.5 `hack/verify-codegen.sh` passes
  - [ ] 7.6 Workflow YAML is valid (no syntax errors in `.github/workflows/*.yml`)

## Dev Notes

### Architecture Overview

Story 1.7 completes Epic 1 by adding CI/CD automation and OLM packaging. This is the "productionization" story — everything built in Stories 1.1–1.6 becomes installable via OLM and automatically validated via CI.

The story has three distinct dimensions:
1. **CI workflows** — GitHub Actions caller workflows that delegate to `redhat-cop/github-workflows-operators` reusable workflows
2. **OLM bundle** — ClusterServiceVersion, APIService registration, prerequisite declarations, bundle Dockerfile
3. **Makefile targets** — Ensuring all targets required by the redhat-cop pipeline and development workflow exist and function

```
Developer                    GitHub Actions                  OperatorHub
┌──────────┐                ┌──────────────────┐            ┌──────────────┐
│ git push  │───PR──────────►│ pr-operator.yml  │            │              │
│           │                │  ├─ make test    │            │              │
│           │                │  ├─ make lint    │            │              │
│           │                │  ├─ verify-codegen│           │              │
│           │                │  ├─ build images │            │              │
│           │                │  ├─ make bundle  │            │              │
│           │                │  └─ validate     │            │              │
│           │                └──────────────────┘            │              │
│           │                                                │              │
│ git tag   │──v*───────────►┌──────────────────┐            │              │
│           │                │release-operator  │            │              │
│           │                │  ├─ build + push │──images───►│  Catalog     │
│           │                │  ├─ bundle + push│──bundle───►│  Index       │
│           │                │  └─ helm package │            │              │
└──────────┘                └──────────────────┘            └──────────────┘
```

### redhat-cop/github-workflows-operators — Caller Pattern

The `redhat-cop/github-workflows-operators` repository provides **reusable workflows** (workflow_call). Projects create thin caller workflows that invoke the reusable workflows with project-specific inputs. The caller workflow pattern:

```yaml
# .github/workflows/pr-operator.yml — caller workflow
name: pr-operator
on:
  pull_request:
    branches:
      - main
    paths-ignore:
      - "**.md"
      - "docs/**"

jobs:
  pr:
    uses: redhat-cop/github-workflows-operators/.github/workflows/pr-operator.yml@v1
    with:
      GO_VERSION: "1.24"
      BUILD_PLATFORMS: "linux/amd64,linux/arm64,linux/ppc64le"
      RUN_UNIT_TESTS: true
      RUN_INTEGRATION_TESTS: true
      RUN_HELMCHART_TEST: true
      OPERATOR_SDK_VERSION: "v1.42.2"
    secrets: inherit

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Run linter
        run: make lint
      - name: Verify codegen
        run: hack/verify-codegen.sh
```

**Critical:** The redhat-cop pipeline expects specific Makefile conventions:
- Default `make` target (no arguments) must build `bin/manager`
- `make generate` must run controller-gen and code generators
- `make fmt` and `make vet` must exist (kubebuilder defaults)
- `make bundle` must generate OLM bundle using `operator-sdk generate bundle`
- `make test` for unit tests, `make integration` for integration tests
- `make helmchart` for Helm chart rendering, `make helmchart-test` for Helm chart testing
- The pipeline builds images using `ci.Dockerfile` (NOT the main `Dockerfile`) — it expects a pre-built binary at `bin/manager`

### ci.Dockerfile

The redhat-cop pipeline builds the Go binary externally via `make`, then builds the container image using `ci.Dockerfile`. This is different from the main `Dockerfile` which is a multi-stage build. The `ci.Dockerfile` is a runtime-only image:

```dockerfile
# ci.Dockerfile — used by redhat-cop pipeline (pre-built binary)
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

LABEL name="soteria" \
      summary="Soteria DR Orchestrator" \
      description="Storage-agnostic disaster recovery orchestrator for OpenShift Virtualization" \
      vendor="Soteria Project" \
      io.k8s.display-name="Soteria DR Orchestrator" \
      io.k8s.description="Storage-agnostic disaster recovery orchestrator for OpenShift Virtualization" \
      io.openshift.tags="disaster-recovery,openshift-virtualization,dr"

COPY bin/manager /manager

USER 65532:65532

ENTRYPOINT ["/manager"]
```

The main `Dockerfile` (multi-stage, created in Story 1.1) is used for local development and direct `docker build`. The `ci.Dockerfile` is used by the CI pipeline.

### Release Workflow

```yaml
# .github/workflows/release-operator.yml — caller workflow
name: release-operator
on:
  push:
    tags:
      - "v*"

jobs:
  release:
    uses: redhat-cop/github-workflows-operators/.github/workflows/release-operator.yml@v1
    with:
      GO_VERSION: "1.24"
      BUILD_PLATFORMS: "linux/amd64,linux/arm64,linux/ppc64le"
      RUN_UNIT_TESTS: true
      OPERATOR_SDK_VERSION: "v1.42.2"
    secrets:
      REGISTRY_USERNAME: ${{ secrets.REGISTRY_USERNAME }}
      REGISTRY_PASSWORD: ${{ secrets.REGISTRY_PASSWORD }}
      COMMUNITY_OPERATOR_PAT: ${{ secrets.COMMUNITY_OPERATOR_PAT }}
```

The release workflow:
1. Builds multi-arch operator images and pushes to registry (quay.io)
2. Generates OLM bundle with the tagged version
3. Builds and pushes the OLM bundle image
4. Packages Helm chart
5. Creates GitHub release with dist artifacts
6. Opens a PR to `community-operators` for OperatorHub listing

### OLM Bundle Structure

The OLM bundle follows standard operator-sdk conventions:

```
bundle/
├── manifests/
│   ├── soteria.clusterserviceversion.yaml    # Main CSV
│   ├── soteria.io_drplans.yaml               # Not a CRD — APIService instead
│   └── v1alpha1.soteria.io-apiservice.yaml   # APIService registration
├── metadata/
│   └── annotations.yaml                       # Bundle metadata
└── tests/
    └── scorecard/
        └── config.yaml                        # Scorecard test config
```

**Important:** Soteria uses an Aggregated API Server, not CRDs. The bundle does NOT contain CRD YAML files. Instead, it includes an APIService resource that registers `v1alpha1.soteria.io` with the kube-apiserver aggregation layer. This is an unusual but valid OLM pattern — the CSV's `spec.apiservicedefinitions.owned` section declares the API services instead of `spec.customresourcedefinitions.owned`.

### ClusterServiceVersion Key Sections

```yaml
# bundle/manifests/soteria.clusterserviceversion.yaml (key sections)
apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: soteria.v0.0.1
  namespace: placeholder
  annotations:
    alm-examples: |
      [
        {
          "apiVersion": "soteria.io/v1alpha1",
          "kind": "DRPlan",
          "metadata": {"name": "example-plan"},
          "spec": {
            "vmSelector": {"matchLabels": {"app.kubernetes.io/part-of": "example"}},
            "waveLabel": "soteria.io/wave",
            "maxConcurrentFailovers": 4
          }
        }
      ]
    capabilities: "Basic Install"
    categories: "Integration & Delivery"
    containerImage: quay.io/soteria-project/soteria:latest
    repository: https://github.com/soteria-project/soteria
    support: Soteria Project
spec:
  displayName: "Soteria DR Orchestrator"
  description: |
    Soteria is a storage-agnostic disaster recovery orchestrator for OpenShift Virtualization.
    It provides unified DR across heterogeneous storage backends — ODF, Dell, Pure Storage,
    NetApp — through a single, consistent workflow engine.
  maturity: alpha
  version: 0.0.1
  minKubeVersion: "1.28.0"
  keywords:
    - disaster-recovery
    - openshift-virtualization
    - dr
    - failover
    - storage-agnostic
  provider:
    name: Soteria Project
    url: https://github.com/soteria-project/soteria
  links:
    - name: Repository
      url: https://github.com/soteria-project/soteria
    - name: Documentation
      url: https://github.com/soteria-project/soteria/docs
  maintainers:
    - name: Soteria Maintainers
      email: soteria-maintainers@googlegroups.com
  icon:
    - base64data: ""  # Placeholder — add actual icon
      mediatype: image/png

  installModes:
    - type: OwnNamespace
      supported: true
    - type: SingleNamespace
      supported: true
    - type: MultiNamespace
      supported: false
    - type: AllNamespaces
      supported: true

  # Aggregated API Server — NOT CRDs
  apiservicedefinitions:
    owned:
      - group: soteria.io
        version: v1alpha1
        kind: DRPlan
        name: drplans
        displayName: DR Plan
        description: Defines a disaster recovery plan for a set of VMs
      - group: soteria.io
        version: v1alpha1
        kind: DRExecution
        name: drexecutions
        displayName: DR Execution
        description: Records an immutable execution of a DRPlan
      - group: soteria.io
        version: v1alpha1
        kind: DRGroupStatus
        name: drgroupstatuses
        displayName: DR Group Status
        description: Tracks real-time state of a DRGroup during execution
    required:
      - group: scylla.scylladb.com
        version: v1
        kind: ScyllaCluster
        name: scyllaclusters
        displayName: ScyllaDB Cluster
        description: ScyllaDB cluster managed by the scylla-operator
      - group: cert-manager.io
        version: v1
        kind: Certificate
        name: certificates
        displayName: Certificate
        description: cert-manager Certificate for TLS management

  install:
    strategy: deployment
    spec:
      deployments:
        - name: soteria-controller-manager
          spec:
            replicas: 2
            selector:
              matchLabels:
                control-plane: controller-manager
            template:
              metadata:
                labels:
                  control-plane: controller-manager
              spec:
                serviceAccountName: soteria-controller-manager
                containers:
                  - name: manager
                    image: quay.io/soteria-project/soteria:latest
                    args:
                      - --leader-elect
                      - --scylladb-contact-points=soteria-scylladb-client.soteria-system.svc:9042
                      - --scylladb-keyspace=soteria
                      - --scylladb-tls-cert=/etc/scylladb-client-tls/tls.crt
                      - --scylladb-tls-key=/etc/scylladb-client-tls/tls.key
                      - --scylladb-tls-ca=/etc/scylladb-client-tls/ca.crt
                    ports:
                      - containerPort: 8443
                        name: https
                        protocol: TCP
                      - containerPort: 8080
                        name: metrics
                        protocol: TCP
                    resources:
                      requests:
                        cpu: 100m
                        memory: 256Mi
                      limits:
                        cpu: 500m
                        memory: 512Mi
                    volumeMounts:
                      - name: scylladb-client-tls
                        mountPath: /etc/scylladb-client-tls
                        readOnly: true
                volumes:
                  - name: scylladb-client-tls
                    secret:
                      secretName: soteria-scylladb-client-tls
      permissions:
        - serviceAccountName: soteria-controller-manager
          rules: []  # Generated from config/rbac/ via make manifests
      clusterPermissions:
        - serviceAccountName: soteria-controller-manager
          rules: []  # Generated from config/rbac/ via make manifests
```

### APIService Bundle Manifest

The APIService resource in the bundle tells OLM to register the aggregated API:

```yaml
# bundle/manifests/v1alpha1.soteria.io-apiservice.yaml
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.soteria.io
spec:
  group: soteria.io
  version: v1alpha1
  groupPriorityMinimum: 1000
  versionPriority: 15
  service:
    name: soteria-apiserver
    namespace: soteria-system
  caBundle: ""  # Populated by OLM/cert-manager at runtime
```

OLM handles injecting the CA bundle when the APIService is managed as part of the CSV's `apiservicedefinitions.owned`. This is a standard OLM feature for operators that serve aggregated APIs.

### Makefile Additions

Story 1.1 created the Makefile with kubebuilder defaults and stubs for `integration`, `helmchart-test`, and `dev-cluster`. This story adds the OLM-specific targets and ensures all targets expected by the redhat-cop pipeline work correctly.

**New/updated Makefile targets:**

```makefile
# --- OLM Bundle ---
OPERATOR_SDK ?= operator-sdk
BUNDLE_IMG ?= quay.io/soteria-project/soteria-bundle:$(VERSION)
DEFAULT_CHANNEL ?= alpha
BUNDLE_CHANNELS ?= --channels=$(DEFAULT_CHANNEL)
BUNDLE_DEFAULT_CHANNEL ?= --default-channel=$(DEFAULT_CHANNEL)

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate OLM bundle manifests
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle \
		--version $(VERSION) \
		$(BUNDLE_CHANNELS) \
		$(BUNDLE_DEFAULT_CHANNEL) \
		--overwrite
	$(OPERATOR_SDK) bundle validate ./bundle --select-optional name=operatorhub

.PHONY: bundle-build
bundle-build: ## Build the OLM bundle image
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the OLM bundle image
	docker push $(BUNDLE_IMG)

.PHONY: operator-sdk
operator-sdk: ## Download operator-sdk CLI if not present
	@if ! command -v operator-sdk &> /dev/null; then \
		echo "operator-sdk not found, downloading..."; \
		curl -LO https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_linux_amd64; \
		chmod +x operator-sdk_linux_amd64; \
		mv operator-sdk_linux_amd64 $(LOCALBIN)/operator-sdk; \
	fi

# --- Helm Chart (redhat-cop pipeline compatibility) ---
.PHONY: helmchart
helmchart: manifests kustomize ## Render Helm chart from kustomize manifests
	mkdir -p charts/$(OPERATOR_NAME)/templates
	$(KUSTOMIZE) build config/default > charts/$(OPERATOR_NAME)/templates/manifests.yaml
	cp config/helmchart/Chart.yaml.tpl charts/$(OPERATOR_NAME)/Chart.yaml
	sed -i 's/REPLACE_VERSION/$(HELM_RELEASE_VERSION)/g' charts/$(OPERATOR_NAME)/Chart.yaml
	sed -i 's|REPLACE_IMAGE|$(IMG)|g' charts/$(OPERATOR_NAME)/templates/manifests.yaml

.PHONY: helmchart-test
helmchart-test: helmchart ## Test Helm chart rendering and validation
	helm lint charts/$(OPERATOR_NAME)

# --- Integration Tests ---
.PHONY: integration
integration: ## Run integration tests (ScyllaDB testcontainers)
	go test ./test/integration/... -tags=integration -v -timeout 300s

# --- Dev Cluster ---
.PHONY: dev-cluster
dev-cluster: ## Set up local dev cluster with no-op driver
	@echo "Setting up local development cluster..."
	hack/dev-cluster.sh
```

**Ensure binary output name is `bin/manager`:** The redhat-cop pipeline expects `bin/manager` as the build output. The kubebuilder Makefile default target builds `cmd/soteria/main.go` — verify the output path is `bin/manager`:

```makefile
.PHONY: build
build: manifests generate fmt vet ## Build manager binary
	go build -o bin/manager ./cmd/soteria/
```

### bundle.Dockerfile

Replace the placeholder from Story 1.1:

```dockerfile
# bundle.Dockerfile — OLM bundle image
FROM scratch

# OLM bundle labels
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=soteria
LABEL operators.operatorframework.io.bundle.channels.v1=alpha
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.42.2
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v4

COPY bundle/manifests /manifests/
COPY bundle/metadata /metadata/
```

### bundle/metadata/annotations.yaml

```yaml
annotations:
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
  operators.operatorframework.io.bundle.manifests.v1: manifests/
  operators.operatorframework.io.bundle.metadata.v1: metadata/
  operators.operatorframework.io.bundle.package.v1: soteria
  operators.operatorframework.io.bundle.channels.v1: alpha
  operators.operatorframework.io.bundle.channel.default.v1: alpha
```

### config/manifests/kustomization.yaml

The `config/manifests/` overlay is the input to `operator-sdk generate bundle`. It combines all resources that should appear in the OLM bundle:

```yaml
# config/manifests/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../default
  - ../rbac
  - ../apiservice
  - ../certmanager
```

### Helm Chart Template (redhat-cop compatibility)

The redhat-cop pipeline expects `make helmchart` to render a Helm chart. Create a minimal template:

```yaml
# config/helmchart/Chart.yaml.tpl
apiVersion: v2
name: soteria
description: Storage-agnostic disaster recovery orchestrator for OpenShift Virtualization
type: application
version: REPLACE_VERSION
appVersion: REPLACE_VERSION
```

This is a placeholder Helm chart for pipeline compatibility. Full Helm chart is a post-v1 concern (noted as a deferred decision in the architecture).

### GO_VERSION Alignment

The `GO_VERSION` input in the caller workflows must match the version in `go.mod`. After Story 1.1's `kubebuilder init`, check `go.mod` for the Go version and use that value. The redhat-cop workflow default is `1.21` which is outdated — override with the project's actual Go version (likely `1.24` given kubebuilder v4.13.1 in March 2026).

### OPERATOR_SDK_VERSION

The architecture document specifies `operator-sdk v1.42.2 (March 2026)`. The redhat-cop workflow defaults to `v1.42.1`. Override to `v1.42.2` in the caller workflows to match the architecture spec.

### File Organization

After this story, the following files are created or updated:

```
.github/workflows/
├── pr-operator.yml           # UPDATED — caller workflow (was placeholder)
└── release-operator.yml      # UPDATED — caller workflow (was placeholder)

ci.Dockerfile                  # NEW — runtime-only Dockerfile for CI pipeline

bundle/
├── manifests/
│   ├── soteria.clusterserviceversion.yaml  # NEW — ClusterServiceVersion
│   └── v1alpha1.soteria.io-apiservice.yaml # NEW — APIService registration
├── metadata/
│   └── annotations.yaml                    # NEW — bundle metadata
└── tests/
    └── scorecard/
        └── config.yaml                     # NEW — scorecard config (optional)

bundle.Dockerfile             # UPDATED — proper OLM bundle Dockerfile (was placeholder)

config/
├── manifests/
│   └── kustomization.yaml    # NEW — input overlay for bundle generation
└── helmchart/
    └── Chart.yaml.tpl        # NEW — Helm chart template for pipeline

Makefile                       # UPDATED — bundle, helmchart, operator-sdk targets
```

### Dependencies

**No new Go dependencies.** This story adds CI/CD configuration files and OLM manifests — no changes to `go.mod`.

**External tooling (not Go modules, downloaded by CI):**
- `operator-sdk` v1.42.2 — standalone CLI for `generate bundle` + `bundle validate`
- `helm` — for Helm chart linting in `helmchart-test`
- `kustomize` — kubebuilder already downloads this via `make kustomize`

### Testing Strategy

This story is primarily configuration and manifests. Testing is validation-focused:

1. **Bundle validation:** `operator-sdk bundle validate ./bundle --select-optional name=operatorhub` — validates CSV structure, metadata, required fields
2. **Makefile targets:** Manually verify each target runs without errors (build, test, lint, bundle, helmchart)
3. **Workflow YAML:** Validate syntax by checking GitHub Actions parses the files (or use `actionlint` locally)
4. **Dockerfile build:** `docker build -f ci.Dockerfile .` after `make build` to verify the runtime image builds
5. **Bundle Dockerfile:** `docker build -f bundle.Dockerfile .` to verify the bundle image builds

No automated tests are added by this story — the CI pipeline itself IS the test infrastructure.

### Critical Warnings

1. **The redhat-cop pipeline expects `ci.Dockerfile`, not `Dockerfile`.** The main `Dockerfile` is a multi-stage build that compiles Go inside Docker. The `ci.Dockerfile` expects a pre-built `bin/manager` binary. Both must exist. The CI pipeline uses `ci.Dockerfile`; developers use `Dockerfile` for local builds.

2. **The binary MUST be output as `bin/manager`.** The redhat-cop pipeline's `build-operator` job runs `make` (default target) to build the binary, then expects it at `bin/manager` (line: `mv bin/manager dist/...`). If the binary has a different name or path, the pipeline will fail.

3. **`make bundle` must be idempotent.** The redhat-cop pipeline runs `make bundle IMG=... VERSION=... DEFAULT_CHANNEL=...` with specific values. The Makefile target must accept these variables and produce consistent output. Running `make bundle` twice with the same inputs must produce identical output.

4. **APIService in OLM bundles is a less-common pattern.** Most operators use CRDs. Soteria uses an Aggregated API Server, so the CSV uses `spec.apiservicedefinitions.owned` instead of `spec.customresourcedefinitions.owned`. OLM handles APIService lifecycle (including CA bundle injection) when the APIService is declared in the CSV. Verify this works with the target OLM version.

5. **`spec.customresourcedefinitions.required` for prerequisites.** The CSV declares ScyllaCluster and Certificate as required CRDs. This tells OLM that these CRDs must exist on the cluster before Soteria can be installed. OLM will block installation if the prerequisites are not met. This is the standard mechanism for expressing operator dependencies.

6. **The `secrets: inherit` pattern in caller workflows.** The PR workflow uses `secrets: inherit` to pass repository secrets to the reusable workflow. For the release workflow, secrets are passed explicitly because the reusable workflow declares required secrets.

7. **Do NOT use `operator-sdk init` or `operator-sdk create api`.** The operator-sdk CLI is used ONLY for `generate bundle` and `bundle validate`. Project scaffolding was done via kubebuilder in Story 1.1. operator-sdk scaffolding was explicitly rejected in the architecture (adds unnecessary indirection).

8. **Helm chart is minimal.** The Helm chart rendered by `make helmchart` is a pipeline compatibility artifact, not a production-grade Helm chart. It renders kustomize output into a Helm template directory. Full Helm chart packaging is a post-v1 concern.

9. **Branch protection configuration is external.** The acceptance criteria state "pipeline failures block merge" — this requires GitHub branch protection rules configured in the repository settings, not in the workflow YAML. Document this requirement but do not attempt to configure it programmatically.

10. **Console plugin image is NOT part of this story.** The Console plugin is scaffolded in Epic 6 (Story 6.1). The OLM bundle in this story covers only the Go operator binary. The Console plugin image will be added to the CSV deployment spec in a later story.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 1.7 (lines 545-582)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Infrastructure & Deployment (lines 217-225)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Project Structure: .github/workflows/, bundle/ (lines 374-488)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Starter Template: operator-sdk standalone (lines 88-101)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Cross-Component Dependencies: CI pipeline (lines 238-242)]
- [Source: _bmad-output/project-context.md — CI/CD rules (lines 194-206)]
- [Source: _bmad-output/project-context.md — Build & Development Commands (lines 183-192)]
- [Source: _bmad-output/planning-artifacts/prd.md — NFR16: OLM lifecycle management (line 444)]
- [Source: _bmad-output/planning-artifacts/prd.md — Technical Architecture: OLM deployment model (lines 238-239)]
- [Source: _bmad-output/implementation-artifacts/1-1-project-initialization-api-type-definitions.md — Makefile, Dockerfiles, directory stubs]
- [Source: _bmad-output/implementation-artifacts/1-5-aggregated-api-server-api-registration.md — APIService registration pattern]
- [External: redhat-cop/github-workflows-operators — https://github.com/redhat-cop/github-workflows-operators (v1.1.5, March 2026)]
- [External: operator-sdk generate bundle — https://sdk.operatorframework.io/docs/cli/operator-sdk_generate_bundle/]
- [External: operator-sdk bundle validate — https://sdk.operatorframework.io/docs/cli/operator-sdk_bundle_validate/]
- [External: OLM APIService support — https://olm.operatorframework.io/docs/advanced-tasks/serving-aggregated-apiservices/]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
