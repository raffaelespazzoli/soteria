# Story 16.8: Release Pipeline & GitHub Pages

Status: ready-for-dev

## Story

As a project maintainer,
I want a GitHub Actions release workflow and Helm chart hosting on GitHub Pages,
so that tagged releases automatically build and publish container images and the Helm chart.

## Acceptance Criteria

**AC1: Workflow triggers on version tags**
Given a tag matching `v*` is pushed
When GitHub Actions processes the event
Then the `.github/workflows/release.yml` workflow is triggered

**AC2: Controller image is built and pushed**
Given the release workflow runs
When the controller build step completes
Then the controller image is pushed to `quay.io/soteria-project/soteria` with the tag from the git tag

**AC3: Console plugin image is built and pushed**
Given the release workflow runs
When the console-plugin build step completes
Then the image is pushed to `quay.io/soteria-project/soteria-console-plugin` with the tag from the git tag

**AC4: Standalone UI image is built and pushed**
Given the release workflow runs
When the standalone-ui build step completes
Then the image is pushed to `quay.io/soteria-project/soteria-standalone-ui` with the tag from the git tag

**AC5: Helm chart is published to GitHub Pages**
Given the release workflow runs
When the chart publishing step completes
Then the Helm chart is published via `helm/chart-releaser-action` to the `gh-pages` branch

**AC6: Chart version derived from git tag**
Given a tag `v1.2.3` is pushed
When the release workflow runs
Then `charts/soteria/Chart.yaml` version is set to `1.2.3` before packaging

**AC7: gh-pages branch exists with README**
Given the repository is set up
When I inspect the `gh-pages` branch
Then it exists as an orphan branch with a README documenting the Helm repository URL

## Tasks / Subtasks

- [ ] Task 1: Create release workflow (AC: 1)
  - [ ] 1.1: Create `.github/workflows/release.yml` triggered on `push.tags: ["v*"]`
  - [ ] 1.2: Define jobs: `build-images`, `publish-chart`
- [ ] Task 2: Implement controller image build and push (AC: 2)
  - [ ] 2.1: Add step to checkout code and set up Docker buildx
  - [ ] 2.2: Add step to login to `quay.io` using `REGISTRY_USERNAME` / `REGISTRY_PASSWORD` secrets (matching existing `release-operator.yml`)
  - [ ] 2.3: Build controller image using `Dockerfile` (multi-arch: `linux/amd64,linux/arm64,linux/ppc64le` matching existing CI)
  - [ ] 2.4: Push to `quay.io/soteria-project/soteria:${VERSION}` (strip `v` prefix from tag)
- [ ] Task 3: Implement console plugin image build and push (AC: 3)
  - [ ] 3.1: Build console-plugin image using `console-plugin/Dockerfile` (context: `console-plugin/`)
  - [ ] 3.2: Push to `quay.io/soteria-project/soteria-console-plugin:${VERSION}`
- [ ] Task 4: Implement standalone UI image build and push (AC: 4)
  - [ ] 4.1: Build standalone-ui image using `console-plugin/Dockerfile.standalone` (context: repo root)
  - [ ] 4.2: Push to `quay.io/soteria-project/soteria-standalone-ui:${VERSION}`
- [ ] Task 5: Implement chart version update (AC: 6)
  - [ ] 5.1: Add step to derive version from git tag: `VERSION=${GITHUB_REF_NAME#v}`
  - [ ] 5.2: Update `charts/soteria/Chart.yaml` version and appVersion to `${VERSION}` using `sed` or `yq`
- [ ] Task 6: Implement Helm chart publishing (AC: 5)
  - [ ] 6.1: Add `helm/chart-releaser-action@v1` step to package and publish chart to `gh-pages` branch
  - [ ] 6.2: Configure `charts_dir: charts` input for chart-releaser-action
- [ ] Task 7: Create gh-pages branch (AC: 7)
  - [ ] 7.1: Create orphan branch `gh-pages` with a `README.md` documenting:
    - Helm repo URL: `https://soteria-project.github.io/soteria`
    - `helm repo add soteria https://soteria-project.github.io/soteria`
    - `helm install soteria soteria/soteria -n soteria --create-namespace`
- [ ] Task 8: Configure repository secrets (AC: 2, 3, 4)
  - [ ] 8.1: Document required secrets: `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` for quay.io
- [ ] Task 9: Test workflow (AC: 1–7)
  - [ ] 9.1: Push a pre-release tag (e.g. `v0.1.0-rc1`) to validate the full pipeline

## Dev Notes

### Implementation Pattern

The existing CI already has two workflows:

**PR workflow** (`.github/workflows/pr-operator.yml`):
```yaml
on:
  pull_request:
    branches: [main]
jobs:
  lint: ...
  pr:
    uses: redhat-cop/github-workflows-operators/.github/workflows/pr-operator.yml@v1
    with:
      GO_VERSION: "1.25"
      BUILD_PLATFORMS: "linux/amd64,linux/arm64,linux/ppc64le"
```

**Release workflow** (`.github/workflows/release-operator.yml`):
```yaml
on:
  push:
    tags: ["v*"]
jobs:
  release:
    uses: redhat-cop/github-workflows-operators/.github/workflows/release-operator.yml@v1
    with:
      GO_VERSION: "1.25"
      BUILD_PLATFORMS: "linux/amd64,linux/arm64,linux/ppc64le"
    secrets:
      REGISTRY_USERNAME: ${{ secrets.REGISTRY_USERNAME }}
      REGISTRY_PASSWORD: ${{ secrets.REGISTRY_PASSWORD }}
```

The new `release.yml` workflow should complement `release-operator.yml` by adding:
1. Console plugin and standalone UI image builds (these are NOT Go binaries, so the reusable operator workflow doesn't cover them)
2. Helm chart packaging and publishing via `helm/chart-releaser-action`

The controller `Dockerfile` builds a Go binary:
```dockerfile
FROM golang:1.25 AS builder
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd/soteria/
FROM gcr.io/distroless/static:nonroot
```

The console-plugin `Dockerfile` builds an nginx image:
```dockerfile
FROM node:22-slim AS build
RUN yarn install --immutable && yarn build
FROM registry.access.redhat.com/ubi9/nginx-120:latest
```

The standalone `Dockerfile.standalone` is a two-stage build:
```dockerfile
FROM node:22-slim AS node-build     # SPA assets
FROM golang:1.25 AS go-build        # console-proxy binary
FROM gcr.io/distroless/static:nonroot
```

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `.github/workflows/release.yml` | NEW | Release workflow: build/push 3 images + publish Helm chart |
| `charts/soteria/Chart.yaml` | UPDATE (by CI) | Version/appVersion updated from git tag at release time |

### Key Constraints

- Container registry: `quay.io/soteria-project/` with three images: `soteria`, `soteria-console-plugin`, `soteria-standalone-ui`
- The existing `release-operator.yml` already builds and pushes the controller image via the reusable workflow — the new workflow should avoid duplicating this; alternatively, consolidate into a single workflow
- Multi-arch builds: `linux/amd64,linux/arm64,linux/ppc64le` (matching existing CI config)
- `helm/chart-releaser-action` requires the chart to be in `charts/` directory and the `gh-pages` branch to exist
- GitHub Pages must be enabled on the repository for the Helm repo to be accessible
- Depends on: Story 16.1 (chart directory must exist)

### Project Structure Notes

- The new workflow file goes in `.github/workflows/` alongside existing CI files
- Consider whether to keep `release-operator.yml` as-is and add `release.yml` as a companion, or consolidate them

### References

- [Source: `.github/workflows/pr-operator.yml` — PR CI with lint, unit tests, integration tests, Helm chart test]
- [Source: `.github/workflows/release-operator.yml` — existing release workflow using reusable operator workflow]
- [Source: `Dockerfile` — controller image build (Go 1.25, multi-stage, distroless)]
- [Source: `console-plugin/Dockerfile` — console plugin image build (Node.js + nginx)]
- [Source: `console-plugin/Dockerfile.standalone` — standalone UI image build (Node.js + Go, two-stage)]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
