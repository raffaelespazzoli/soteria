# Story 18.1: Init Container Image — guestfs-tools on UBI9

Status: ready-for-dev

## Story

As a developer,
I want a container image based on UBI9 that contains guestfs-tools (guestfish, virt-inspector), Augeas, hivex, and NTFS support,
so that the init container has all the tooling needed to inspect and modify guest filesystems for both RHEL and Windows VMs.

## Acceptance Criteria

### AC1: Containerfile builds successfully
Given a `Containerfile` in `build/ip-rewrite/`
When built with `podman build`
Then the image builds without errors on x86_64
And the image is based on `registry.access.redhat.com/ubi9/ubi`

### AC2: Required tools are present
Given the built container image
When I run `guestfish --version`, `virt-inspector --version`, `augtool --version`, and `hivexregedit --version`
Then all commands succeed with version output

### AC3: guestfish can launch its appliance
Given the built container image running with `SYS_ADMIN` capability
When I run `guestfish --ro -a /dev/null run`
Then the libguestfs appliance launches successfully (validates kernel + initrd are present)

### AC4: Image builds via unified CI pipeline
Given the CI pipeline (`.github/workflows/ci.yml`) and release pipeline (`.github/workflows/release.yml`)
When Story 18.9 integrates the `build-ip-rewrite` job
Then the image is built on every PR/merge (CI) and pushed to `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION` on tag (release)
And no standalone workflow is needed — the Containerfile is the deliverable of this story

### AC5: Image size is reasonable
Given the built container image
When I inspect its size
Then it is under 800MB (guestfs-tools + kernel + appliance are heavy but bounded)

## Tasks / Subtasks

- [ ] Task 1: Create `build/ip-rewrite/Containerfile` (AC: 1, 2, 3, 5)
  - [ ] 1.1: Base on `registry.access.redhat.com/ubi9/ubi`
  - [ ] 1.2: Install `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport`
  - [ ] 1.3: Set `LIBGUESTFS_BACKEND=direct` environment variable
  - [ ] 1.4: Clean dnf cache to minimize image size
  - [ ] 1.5: Create placeholder entrypoint directory at `/scripts/`
- [ ] Task 2: Verify all tools are functional inside the container (AC: 2, 3)
  - [ ] 2.1: Run `podman build` and confirm success
  - [ ] 2.2: Run tool version commands inside the image
  - [ ] 2.3: Test `guestfish --ro -a /dev/null run` with `SYS_ADMIN` capability
  - [ ] 2.4: Verify image size is under 800MB
- [ ] Task 3: Create `build/ip-rewrite/README.md` (documentation)
  - [ ] 3.1: Document the image purpose, contents, and build instructions
  - [ ] 3.2: Document the `LIBGUESTFS_BACKEND=direct` requirement
  - [ ] 3.3: Document `SYS_ADMIN` capability requirement for runtime

## Dev Notes

### Story Intelligence Chain

This is the **first story in Epic 18** (Guest Filesystem IP Rewrite — Standalone). There are no predecessor stories.

**Epic 18 context:** This epic creates a standalone component that enables VMs running on OpenShift Virtualization to boot with reconfigured static IPs. It works by injecting an init container into virt-launcher pods that edits guest filesystem network configuration offline before the VM starts. The component is fully independent of the Soteria DR orchestrator.

**What this story establishes for the epic:**
- The base container image that ALL subsequent stories depend on (18.2 entrypoint, 18.3 RHEL handler, 18.4 Windows handler all run inside this image)
- The `build/ip-rewrite/` directory structure that becomes the build context
- The `LIBGUESTFS_BACKEND=direct` pattern required for guestfs in containers
- Proof that guestfish appliance works within a container (kernel + initrd validation)

**What this story defers to later stories:**
- Entrypoint script and handler scripts → Story 18.2, 18.3, 18.4 (scripts are `COPY`ed into the image in 18.2)
- CI/release pipeline integration → Story 18.9 (adds the `build-ip-rewrite` job to existing workflows)
- Helm chart and SCC → Story 18.6

**Dependencies downstream:** Stories 18.2, 18.3, 18.4, 18.5, 18.9 all depend on this image.

### Critical Technical Details

#### Package Selection — CRITICAL CORRECTION from Epic Spec

The epic spec mentions `ntfs-3g` as a required package. **DO NOT install `ntfs-3g`**. On RHEL 9 / UBI9, `ntfs-3g` is only available from EPEL (third-party repository), which is not available in UBI containers without adding external repos.

**Use `libguestfs-winsupport` instead.** This is the official Red Hat package (available in RHEL 9 AppStream) that injects the NTFS driver into the libguestfs appliance via supermin. It provides the same NTFS filesystem access capability that `ntfs-3g` would, but through the official channel. [Source: migtools/oadp-vm-file-restore RHEL 9 compatibility fix]

#### Required Packages

| Package | Repository | Purpose |
|---------|-----------|---------|
| `guestfs-tools` | AppStream | Provides `guestfish`, `virt-inspector`, and other libguestfs CLI tools (v1.52.2 on el9). Also pulls in the libguestfs appliance (kernel + initrd via supermin) |
| `augeas` | AppStream | Provides `augtool` CLI. The Augeas *library* is already a dependency of libguestfs for in-guestfish `aug-*` commands, but the standalone `augtool` binary requires this package |
| `hivex` | AppStream | Provides `hivexregedit`, `hivexget`, `hivexml` CLIs. The hivex *library* is already built into libguestfs for in-guestfish `hivex-*` commands (since libguestfs 1.19.35), but Story 18.4 may use standalone hivex tools |
| `libguestfs-winsupport` | AppStream | Injects NTFS driver into the libguestfs supermin appliance. Required for mounting Windows NTFS filesystems with guestfish |

#### LIBGUESTFS_BACKEND=direct

When running guestfish inside a container, `LIBGUESTFS_BACKEND` **must** be set to `direct`. This tells libguestfs to use the kernel directly (not libvirt/QEMU) for the appliance. Without this, guestfish will try to connect to libvirtd, which is not available in the container. [Source: openshift/appliance Dockerfile pattern]

#### SYS_ADMIN Capability

The libguestfs appliance requires `SYS_ADMIN` capability (or privileged mode) to launch its internal QEMU/KVM instance that boots the appliance kernel. This is a runtime requirement — not needed at build time but critical for AC3 validation.

#### Image Size Considerations

The guestfs-tools package plus its dependencies (kernel, initrd, supermin appliance, QEMU) will make this a large image (~500-800MB). This is expected and acceptable. To minimize bloat:
- Run `dnf clean all` after installation
- Remove `/var/cache/dnf` and `/var/log/dnf*`
- Do NOT install documentation (`--nodocs` flag)
- Do NOT install weak dependencies (`--setopt=install_weak_deps=False`)

### Containerfile Structure

```dockerfile
FROM registry.access.redhat.com/ubi9/ubi

# Install guestfs-tools and all required dependencies for guest filesystem
# manipulation. libguestfs-winsupport replaces ntfs-3g (EPEL-only on RHEL 9)
# and provides NTFS support via the libguestfs supermin appliance.
RUN dnf install -y --nodocs --setopt=install_weak_deps=False \
        guestfs-tools \
        augeas \
        hivex \
        libguestfs-winsupport \
    && dnf clean all \
    && rm -rf /var/cache/dnf /var/log/dnf*

# Required for running libguestfs inside a container — uses kernel directly
# instead of connecting to libvirtd.
ENV LIBGUESTFS_BACKEND=direct

# Placeholder for scripts added by Story 18.2
RUN mkdir -p /scripts

# Entrypoint will be set by Story 18.2 when scripts are COPY'd in
```

**Key decisions embedded in the Containerfile:**
- `--nodocs` saves ~50-100MB by skipping man pages and documentation
- `--setopt=install_weak_deps=False` prevents pulling in optional packages that inflate the image
- The `LIBGUESTFS_BACKEND=direct` env var is baked into the image so consumers never need to set it
- `/scripts/` directory is created as a placeholder — Story 18.2 will `COPY` entrypoint.sh and handler scripts here

### Project Structure Notes

This story creates a **new** directory in the project:

```
build/
└── ip-rewrite/
    ├── Containerfile          ← This story
    └── README.md              ← This story
    # Future (Story 18.2+):
    # └── scripts/
    #     ├── entrypoint.sh
    #     ├── rhel-handler.sh
    #     └── windows-handler.sh
```

**Alignment with existing project patterns:**
- The project uses `Dockerfile` (not `Containerfile`) for the main controller image at the repo root. However, this is a separate image with different tooling. Using `Containerfile` is the Red Hat/Podman convention and matches the UBI9 ecosystem. Either naming is acceptable — `podman build` and `docker build` both recognize both names.
- Existing CI uses `docker/build-push-action@v7` which accepts a `file:` parameter — it works with any filename.
- The project already has separate build contexts: root `Dockerfile` for controller, `console-plugin/Dockerfile` for the UI. Adding `build/ip-rewrite/Containerfile` follows this pattern of per-image build contexts.

**Container image naming convention:**
- Controller: `quay.io/raffaelespazzoli/soteria:$VERSION`
- Console plugin: `quay.io/raffaelespazzoli/soteria-console-plugin:$VERSION`
- Standalone UI: `quay.io/raffaelespazzoli/soteria-standalone-ui:$VERSION`
- IP rewrite (this image): `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION`

**Makefile target (Story 18.9 will add this, but document the expected interface):**
```makefile
IP_REWRITE_IMG ?= quay.io/raffaelespazzoli/soteria-ip-rewrite:latest

.PHONY: docker-build-ip-rewrite
docker-build-ip-rewrite:
	$(CONTAINER_TOOL) build -t $(IP_REWRITE_IMG) build/ip-rewrite/
```

### CI Pipeline Context (for Story 18.9 — DO NOT modify CI in this story)

The existing CI pipeline (`.github/workflows/ci.yml`) has parallel build jobs:
- `build-soteria` — controller image from root `Dockerfile`
- `build-console-plugin` — UI image from `console-plugin/Dockerfile`
- `build-standalone-ui` — standalone UI from `console-plugin/Dockerfile.standalone`

Story 18.9 will add a `build-ip-rewrite` job following the same pattern:
```yaml
build-ip-rewrite:
  name: Build ip-rewrite image
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v7
    - uses: docker/setup-buildx-action@v4
    - uses: docker/build-push-action@v7
      with:
        context: build/ip-rewrite/
        file: build/ip-rewrite/Containerfile
        platforms: linux/amd64  # x86_64 only for v1
        push: false
        tags: soteria-ip-rewrite:ci
```

**This story's scope:** Create the Containerfile and README only. Do NOT modify `.github/workflows/ci.yml` or `release.yml` — that is Story 18.9.

### Anti-Patterns / DO NOT

- **DO NOT install `ntfs-3g`** — it requires EPEL, which is not available in UBI9 containers without adding external repos. Use `libguestfs-winsupport` instead.
- **DO NOT add EPEL or any third-party repositories** — this image must only use official UBI9/RHEL 9 repos (ubi-9-baseos-rpms, ubi-9-appstream-rpms).
- **DO NOT create entrypoint scripts** — that is Story 18.2's responsibility. This story only creates the base image with tools.
- **DO NOT set an ENTRYPOINT or CMD in the Containerfile** — Story 18.2 will add this when the entrypoint script is created.
- **DO NOT modify `.github/workflows/ci.yml` or `release.yml`** — CI integration is Story 18.9.
- **DO NOT modify the `Makefile`** — build targets are Story 18.9.
- **DO NOT use `ubi-minimal`** — `ubi-minimal` uses `microdnf` which may not resolve all guestfs-tools dependencies correctly. Use the full `ubi9/ubi` base.
- **DO NOT add multi-arch support** — x86_64 only for v1 (all OCP Virt certified Windows guests are x86_64).
- **DO NOT install `python3-hivex`** — the Python bindings are not needed. The guestfish built-in `hivex-*` commands (since libguestfs 1.19.35) and the standalone `hivexregedit` CLI are sufficient.

### Verification Commands

After building, run these to validate all ACs:

```bash
# Build (AC1)
podman build -t soteria-ip-rewrite:dev build/ip-rewrite/

# Tool versions (AC2)
podman run --rm soteria-ip-rewrite:dev guestfish --version
podman run --rm soteria-ip-rewrite:dev virt-inspector --version
podman run --rm soteria-ip-rewrite:dev augtool --version
podman run --rm soteria-ip-rewrite:dev hivexregedit --version

# Appliance launch (AC3) — requires SYS_ADMIN
podman run --rm --cap-add SYS_ADMIN soteria-ip-rewrite:dev \
    guestfish --ro -a /dev/null run

# Image size (AC5)
podman image inspect soteria-ip-rewrite:dev --format '{{.Size}}' | \
    awk '{printf "%.0f MB\n", $1/1024/1024}'
```

**Note on AC3 verification:** The `guestfish --ro -a /dev/null run` command validates that the supermin appliance can boot its kernel + initrd. This requires `/dev/kvm` to be available. In CI (GitHub Actions), hardware KVM may not be available on all runner types. If AC3 cannot be validated in CI, document this limitation and validate manually. Story 18.9 will determine the CI runner configuration.

### References

- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18"]
- [Existing controller Dockerfile: `Dockerfile` at repo root]
- [CI pipeline: `.github/workflows/ci.yml`]
- [Release pipeline: `.github/workflows/release.yml`]
- [UBI9 package installation docs: Red Hat RHEL 9 container docs]
- [libguestfs backend=direct pattern: openshift/appliance Dockerfile]
- [libguestfs-winsupport replaces ntfs-3g: migtools/oadp-vm-file-restore RHEL 9 fix]
- [guestfs-tools v1.52.2 on RHEL 9 AppStream: pkgdex.org]
- [Project container tool convention: `Makefile` — `CONTAINER_TOOL ?= podman`]

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
