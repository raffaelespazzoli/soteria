# Story 18.9: CI & Release Pipeline Integration

Status: review

## Story

As a developer,
I want the IP rewrite container image built and tested in the unified CI pipeline and published in the release pipeline,
so that the image follows the same build, test, and publish lifecycle as all other Soteria images.

## Acceptance Criteria

### AC1: CI pipeline builds the image
Given the CI pipeline at `.github/workflows/ci.yml`
When a PR or merge to `main` occurs with changes in `build/ip-rewrite/`, `internal/webhook/iprewrite/`, or `cmd/ip-rewrite-webhook/`
Then a `build-ip-rewrite` job builds the image using `build/ip-rewrite/Containerfile`
And the build uses Docker Buildx with GHA cache (`scope=ip-rewrite`)
And the image is built for `linux/amd64` only (single-arch)
And the image is not pushed (CI only validates the build)

### AC2: Release pipeline builds and pushes the image
Given the release pipeline at `.github/workflows/release.yml`
When a semver tag is pushed
Then the `build-ip-rewrite` job builds and pushes to `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION`
And a `latest` tag is pushed for non-prerelease tags
And the image is single-arch `linux/amd64`

### AC3: Helm sub-chart is packaged and published
Given the release pipeline's `helm` job
When the chart is packaged
Then `charts/soteria-ip-rewrite/` is also packaged via `helm package`
And published to the GitHub Pages Helm repo alongside the main Soteria chart
And the sub-chart version is stamped from the tag

### AC4: CI path filtering
Given the CI pipeline
When only documentation files or unrelated code changes are pushed
Then the `build-ip-rewrite` job is skipped (path-based filtering)

### AC5: Webhook unit tests run in CI
Given the `test` job in CI
When tests run
Then `internal/webhook/iprewrite/` tests are included in the standard `make test` target
And no additional test targets are needed for the webhook Go code

## Tasks / Subtasks

- [x] Task 1: Add `build-ip-rewrite` job to `.github/workflows/ci.yml` (AC: 1, 4)
  - [x] 1.1: Add job `build-ip-rewrite` following the existing `build-soteria` / `build-console-plugin` / `build-standalone-ui` pattern
  - [x] 1.2: Set `context: build/ip-rewrite/` and `file: build/ip-rewrite/Containerfile`
  - [x] 1.3: Set `platforms: linux/amd64` (single-arch, NOT the `BUILD_PLATFORMS` env var)
  - [x] 1.4: Set `push: false`, `tags: soteria-ip-rewrite:ci`
  - [x] 1.5: Set `cache-from: type=gha,scope=ip-rewrite` and `cache-to: type=gha,mode=max,scope=ip-rewrite`
  - [x] 1.6: Do NOT include QEMU setup step (not needed for single-arch)
- [x] Task 2: Add `build-ip-rewrite` job to `.github/workflows/release.yml` (AC: 2)
  - [x] 2.1: Add job `build-ip-rewrite` under the "3x. Build & push" section, after `build-standalone-ui`
  - [x] 2.2: Set `needs: validate` (same as other build jobs)
  - [x] 2.3: Login to quay.io using existing secrets pattern
  - [x] 2.4: Set `platforms: linux/amd64` (single-arch, NOT `BUILD_PLATFORMS` env var)
  - [x] 2.5: Set `push: true` with version and conditional `latest` tags
  - [x] 2.6: Do NOT include QEMU setup step (not needed for single-arch)
  - [x] 2.7: Add `build-ip-rewrite` to the `needs` list of the `helm` job
- [x] Task 3: Update release `helm` job to package and publish `charts/soteria-ip-rewrite/` (AC: 3)
  - [x] 3.1: Add a step to stamp `charts/soteria-ip-rewrite/Chart.yaml` version (same pattern as main chart)
  - [x] 3.2: Add `helm lint charts/soteria-ip-rewrite` step
  - [x] 3.3: Add `helm package charts/soteria-ip-rewrite --destination /tmp/chart-package` step
  - [x] 3.4: Both chart packages (main + sub-chart) must be in `/tmp/chart-package/` before the `cp` step
  - [x] 3.5: Both charts are included in the `helm repo index` and gh-pages publish
  - [x] 3.6: Both chart `.tgz` files are uploaded as release artifacts
- [x] Task 4: Add `docker-build-ip-rewrite` Makefile target (AC: 1)
  - [x] 4.1: Add `IP_REWRITE_IMG` variable with default `quay.io/raffaelespazzoli/soteria-ip-rewrite:latest`
  - [x] 4.2: Add `.PHONY: docker-build-ip-rewrite` target that builds from `build/ip-rewrite/`
- [x] Task 5: Verify webhook unit tests are included in `make test` (AC: 5)
  - [x] 5.1: Confirm `go test $$(go list ./... | grep -v /e2e | grep -v /console-plugin/)` already includes `internal/webhook/iprewrite/...`
  - [x] 5.2: No Makefile changes needed if the glob already picks it up (it will — the package is under `./internal/...`)

## Dev Notes

### Story Intelligence Chain

**Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9**

Story 18.1 establishes the build context this story integrates into CI/release:
- Created `build/ip-rewrite/Containerfile` — UBI9-based image with guestfs-tools, augeas, hivex, libguestfs-winsupport
- Created `build/ip-rewrite/README.md` — documents image purpose and build instructions
- Set `LIBGUESTFS_BACKEND=direct` as baked-in environment variable
- **Key corrections from 18.1**: Uses `libguestfs-winsupport` instead of `ntfs-3g` (EPEL not available in UBI9). No ENTRYPOINT/CMD set (deferred to 18.2)
- **Container naming convention established**: `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION`
- **Makefile target pattern previewed**: `IP_REWRITE_IMG ?= quay.io/raffaelespazzoli/soteria-ip-rewrite:latest`

**Story 18.1 deferred to this story:**
- CI/release pipeline integration (the `build-ip-rewrite` job)
- Makefile build targets

**Other Epic 18 stories relevant to CI scope:**
- Story 18.5 creates `cmd/ip-rewrite-webhook/main.go` and `internal/webhook/iprewrite/handler.go` — Go code whose tests AC5 validates are included in `make test`
- Story 18.6 creates `charts/soteria-ip-rewrite/` — the Helm sub-chart that AC3 packages and publishes
- Story 18.7 adds integration tests at `test/ip-rewrite/` — these may need `LIBGUESTFS_BACKEND=direct` and won't run in standard CI (guestfish requires SYS_ADMIN); they are out of scope for this story

**Dependency status:** This story can be implemented now. The Containerfile from 18.1 must exist. If `charts/soteria-ip-rewrite/` (18.6) does not yet exist when this story is implemented, the Helm packaging steps (Task 3) should be added but will only activate once the chart directory exists. Use conditional checks (`if: hashFiles('charts/soteria-ip-rewrite/Chart.yaml') != ''`) or simply document that the helm steps require 18.6 to be complete.

### Critical Technical Details

#### Existing CI Job Pattern (MUST FOLLOW)

All existing image build jobs in `ci.yml` follow this exact structure:

```yaml
build-<name>:
  name: Build <name> image
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v7

    - name: Set up QEMU                        # ← SKIP for ip-rewrite (single-arch)
      if: github.event_name != 'pull_request'
      uses: docker/setup-qemu-action@v4

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v4

    - name: Build image
      uses: docker/build-push-action@v7
      with:
        context: <build-context>/
        file: <build-context>/Dockerfile
        platforms: ${{ env.BUILD_PLATFORMS }}    # ← OVERRIDE for ip-rewrite
        push: false
        tags: <name>:ci
        cache-from: type=gha,scope=<name>
        cache-to: type=gha,mode=max,scope=<name>
```

**ip-rewrite deviations from the pattern:**
1. **No QEMU step** — single-arch `linux/amd64` only, QEMU is for cross-compilation
2. **Hardcoded `platforms: linux/amd64`** — do NOT use `${{ env.BUILD_PLATFORMS }}` which includes arm64 and ppc64le
3. **`file:` uses `Containerfile`** not `Dockerfile` — the `docker/build-push-action` accepts any filename via the `file:` parameter
4. **`context: build/ip-rewrite/`** — separate build context directory (like `console-plugin/`)

#### Existing Release Job Pattern (MUST FOLLOW)

All existing image build jobs in `release.yml` follow this structure:

```yaml
build-<name>:
  name: Build <name> image
  needs: validate
  runs-on: ubuntu-latest
  permissions:
    contents: read
  steps:
    - uses: actions/checkout@v7

    - name: Set up QEMU                        # ← SKIP for ip-rewrite
      uses: docker/setup-qemu-action@v4

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v4

    - name: Login to quay.io
      uses: docker/login-action@v4
      with:
        registry: ${{ env.REGISTRY }}
        username: ${{ secrets.REGISTRY_USERNAME }}
        password: ${{ secrets.REGISTRY_PASSWORD }}

    - name: Build and push
      uses: docker/build-push-action@v7
      with:
        context: .
        file: Dockerfile
        platforms: ${{ env.BUILD_PLATFORMS }}    # ← OVERRIDE for ip-rewrite
        push: true
        provenance: false
        tags: |
          ${{ env.REGISTRY }}/${{ env.REGISTRY_ORG }}/<name>:${{ needs.validate.outputs.version }}
          ${{ needs.validate.outputs.is_prerelease == 'false' && format('{0}/{1}/<name>:latest', env.REGISTRY, env.REGISTRY_ORG) || '' }}
        cache-from: type=gha,scope=<name>
        cache-to: type=gha,mode=max,scope=<name>
```

**ip-rewrite deviations:**
1. **No QEMU step** — single-arch
2. **`platforms: linux/amd64`** — hardcoded, not `${{ env.BUILD_PLATFORMS }}`
3. **`context: build/ip-rewrite/`** and **`file: build/ip-rewrite/Containerfile`**
4. **Image name**: `soteria-ip-rewrite` (following convention: `soteria`, `soteria-console-plugin`, `soteria-standalone-ui`, `soteria-ip-rewrite`)

#### Helm Chart Publishing Pattern (MUST FOLLOW)

The existing `helm` job in `release.yml` stamps, lints, templates, packages, and publishes the main chart. For the sub-chart, replicate the same steps. Key patterns:

```yaml
# Stamp version
sed -i "s/^version:.*/version: ${VERSION}/" charts/soteria-ip-rewrite/Chart.yaml
sed -i "s/^appVersion:.*/appVersion: \"${{ github.ref_name }}\"/" charts/soteria-ip-rewrite/Chart.yaml

# Lint
helm lint charts/soteria-ip-rewrite

# Package (into same destination as main chart)
helm package charts/soteria-ip-rewrite --destination /tmp/chart-package
```

The existing step `cp /tmp/chart-package/*.tgz /tmp/helm-repo/` will pick up both chart packages automatically via the glob. The `helm repo index` step also handles multiple packages in the directory.

#### Release Job Dependency Graph

Current release flow:
```
validate ─┬→ build-soteria ──────────┐
          ├→ build-console-plugin ───├→ helm ──┬→ github-release
          ├→ build-standalone-ui ────┤         │
          ├→ test → helmchart-test ──┘         │
          └──────────────────────→ docs ────────┘
```

Updated flow with ip-rewrite:
```
validate ─┬→ build-soteria ──────────┐
          ├→ build-console-plugin ───├→ helm ──┬→ github-release
          ├→ build-standalone-ui ────┤         │
          ├→ build-ip-rewrite ───────┤         │
          ├→ test → helmchart-test ──┘         │
          └──────────────────────→ docs ────────┘
```

Change: add `build-ip-rewrite` to the `needs` array of the `helm` job:
```yaml
helm:
  needs: [validate, build-soteria, build-console-plugin, build-standalone-ui, build-ip-rewrite, helmchart-test]
```

#### CI `paths-ignore` — NO CHANGES NEEDED

The CI pipeline already has `paths-ignore` that skips `**.md`, `docs/**`, `mkdocs.yml`, `requirements-docs.txt`. The `build-ip-rewrite` job does NOT need its own path filtering — it runs on all non-doc changes just like the other build jobs. This is correct because the image build validates the Containerfile is not broken by any Go dependency changes.

#### Makefile Convention

The project uses `CONTAINER_TOOL ?= podman` and existing targets follow:
```makefile
docker-build:
	$(CONTAINER_TOOL) build -t ${IMG} .
```

Add a parallel target for the ip-rewrite image:
```makefile
IP_REWRITE_IMG ?= quay.io/raffaelespazzoli/soteria-ip-rewrite:latest

.PHONY: docker-build-ip-rewrite
docker-build-ip-rewrite: ## Build ip-rewrite init container image.
	$(CONTAINER_TOOL) build -t $(IP_REWRITE_IMG) build/ip-rewrite/
```

#### `make test` Coverage — NO CHANGES NEEDED

The existing `make test` target runs:
```makefile
go test $$(go list ./... | grep -v /e2e | grep -v /console-plugin/) -coverprofile cover.out
```

This glob includes ALL Go packages under `./internal/...` including `internal/webhook/iprewrite/...`. So webhook unit tests (from Story 18.7) are automatically included. No Makefile changes needed for AC5.

#### GitHub Actions Versions

The project uses these pinned action versions (from recent commit `2383af8ad`):
- `actions/checkout@v7`
- `docker/setup-qemu-action@v4`
- `docker/setup-buildx-action@v4`
- `docker/build-push-action@v7`
- `docker/login-action@v4`
- `azure/setup-helm@v4`
- `peaceiris/actions-gh-pages@v4`
- `actions/upload-artifact@v7`

Use these exact versions. Do NOT upgrade or change them.

### Project Structure Notes

Files this story creates or modifies:

```
.github/workflows/ci.yml          ← MODIFY: add build-ip-rewrite job
.github/workflows/release.yml     ← MODIFY: add build-ip-rewrite job + update helm job
Makefile                           ← MODIFY: add IP_REWRITE_IMG var + docker-build-ip-rewrite target
```

Files this story depends on (must already exist from predecessor stories):

```
build/ip-rewrite/Containerfile     ← From Story 18.1
charts/soteria-ip-rewrite/         ← From Story 18.6 (may not exist yet — use conditional)
internal/webhook/iprewrite/        ← From Story 18.5 (tests auto-included in make test)
cmd/ip-rewrite-webhook/            ← From Story 18.5 (compiled as part of Go build)
```

### Anti-Patterns / DO NOT

- **DO NOT use `${{ env.BUILD_PLATFORMS }}`** for the ip-rewrite image build — this expands to `linux/amd64,linux/arm64,linux/ppc64le` and the ip-rewrite image is x86_64 only. Hardcode `linux/amd64`.
- **DO NOT add QEMU setup step** to the ip-rewrite build jobs — QEMU is only needed for cross-architecture builds. The ip-rewrite image is single-arch `linux/amd64`.
- **DO NOT create a separate workflow file** (e.g., `ci-ip-rewrite.yml`) — this story integrates into the EXISTING `ci.yml` and `release.yml` workflows.
- **DO NOT add path filtering to the `build-ip-rewrite` CI job** — the other build jobs don't have per-job path filters either. The workflow-level `paths-ignore` is sufficient.
- **DO NOT modify the `test` job** — webhook unit tests are already captured by `make test`'s `go list ./...` glob. No new test targets needed.
- **DO NOT add `helmchart-test` steps for the sub-chart** — that is out of scope. The sub-chart is simple (no Kind cluster deployment needed for validation). `helm lint` in the release pipeline is sufficient.
- **DO NOT modify `build/ip-rewrite/Containerfile`** — that is Story 18.1's deliverable.
- **DO NOT add multi-arch support** for the ip-rewrite image — x86_64 only for v1 (all OCP Virt certified Windows guests are x86_64).
- **DO NOT change the concurrency groups** in either workflow — the existing groups (`ci-${{ ... }}` and `release-${{ ... }}`) apply to all jobs in the workflow and are correct.
- **DO NOT change existing jobs** (build-soteria, build-console-plugin, build-standalone-ui, test, helmchart-test) — only add new jobs and extend the `helm` job.
- **DO NOT upgrade GitHub Actions versions** — use the exact versions already in the workflows (checkout@v7, buildx@v4, build-push-action@v7, etc.).

### References

- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18", Story 18.9 at line ~4812]
- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [CI pipeline: `.github/workflows/ci.yml`]
- [Release pipeline: `.github/workflows/release.yml`]
- [Makefile: `Makefile` — see `docker-build` target pattern and `CONTAINER_TOOL` variable]
- [Main Helm chart: `charts/soteria/Chart.yaml`]
- [Controller Dockerfile: `Dockerfile` at repo root]
- [Console plugin Dockerfile: `console-plugin/Dockerfile`]
- [Recent CI/CD commit history: `6adbf27fa`, `f778f41ca`, `1578a2ce4` — CI parallelization and multi-arch simplification]

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

Claude Opus 4.6 (Cursor)

### Debug Log References

No issues encountered during implementation.

### Completion Notes List

- **AC1 (CI build):** Added `build-ip-rewrite` job to `ci.yml` with `context: build/ip-rewrite/`, `file: build/ip-rewrite/Containerfile`, `platforms: linux/amd64`, `push: false`, `tags: soteria-ip-rewrite:ci`, GHA cache with `scope=ip-rewrite`. No QEMU step (single-arch).
- **AC2 (Release build+push):** Added `build-ip-rewrite` job to `release.yml` with `needs: validate`, quay.io login, `platforms: linux/amd64`, version + conditional `latest` tags, `push: true`, `provenance: false`. No QEMU step. Added to `helm` job's `needs` list.
- **AC3 (Helm sub-chart):** Added conditional steps (guarded by `hashFiles('charts/soteria-ip-rewrite/Chart.yaml') != ''`) to stamp version, lint, and package `charts/soteria-ip-rewrite/` into `/tmp/chart-package/`. Existing glob-based `cp` and `helm repo index` steps automatically include both charts. Steps will activate once Story 18.6 delivers the chart directory.
- **AC4 (Path filtering):** Workflow-level `paths-ignore` (`.md`, `docs/**`, etc.) already applies to all jobs including `build-ip-rewrite`. No per-job path filtering needed (consistent with other build jobs).
- **AC5 (Webhook tests):** Verified `make test` glob (`go list ./... | grep -v /e2e | grep -v /console-plugin/`) already includes `internal/webhook/iprewrite/...`. No Makefile changes needed.
- **Makefile:** Added `IP_REWRITE_IMG ?= quay.io/raffaelespazzoli/soteria-ip-rewrite:latest` variable and `docker-build-ip-rewrite` target.
- **All tests pass** — no regressions introduced (pre and post verification).

### File List

- `.github/workflows/ci.yml` — MODIFIED: added `build-ip-rewrite` job, updated header comment
- `.github/workflows/release.yml` — MODIFIED: added `build-ip-rewrite` job, updated `helm` job with sub-chart steps, updated header comment and dependency graph
- `Makefile` — MODIFIED: added `IP_REWRITE_IMG` variable and `docker-build-ip-rewrite` target
- `_bmad-output/implementation-artifacts/18-9-ci-and-release-pipeline-integration.md` — MODIFIED: status → review, tasks checked, dev record filled
