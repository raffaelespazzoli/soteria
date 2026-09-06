# ip-rewrite Init Container Image

Init container image for rewriting static IP configuration on guest VM filesystems before boot. Part of [Epic 18 — Guest Filesystem IP Rewrite (Standalone)](../../_bmad-output/planning-artifacts/epics.md).

## Purpose

When VMs running on OpenShift Virtualization are failed over to a DR site, their guest OS network configuration still references the original site's IP addresses. This init container is injected into `virt-launcher` pods by a mutating webhook and edits guest filesystem network configuration offline (via libguestfs) before the VM starts.

## Image Contents

| Tool | Package | Purpose |
|------|---------|---------|
| `guestfish` | `guestfs-tools` | Mount and modify guest VM disk images |
| `virt-inspector` | `guestfs-tools` | Detect guest OS type and filesystem layout |
| `augtool` | `augeas` | Structured editing of Linux config files (ifcfg, NM keyfiles) |
| `hivexget`, `hivexml`, `hivexsh` | `hivex` | Read/inspect Windows registry hives |
| `hivexregedit` | `perl-hivex` | Merge/export Windows registry hive entries |
| NTFS support | `libguestfs-winsupport` | Mount Windows NTFS filesystems via the libguestfs appliance |

### Why CentOS Stream 9 (not UBI9)?

The required packages (`guestfs-tools`, `libguestfs-winsupport`) are in the full RHEL 9 AppStream repository, which is **not** available in UBI9 repos (`ubi-9-baseos-rpms`, `ubi-9-appstream-rpms`) without RHEL entitlements. CentOS Stream 9 provides the same packages from its freely-available repos and works in CI (GitHub Actions) without entitlements.

## Build

```bash
podman build -t soteria-ip-rewrite:dev build/ip-rewrite/
```

Or using the project convention:

```bash
docker build -t soteria-ip-rewrite:dev build/ip-rewrite/
```

## Runtime Requirements

### `LIBGUESTFS_BACKEND=direct`

This environment variable is baked into the image. It tells libguestfs to use the kernel directly (not libvirt/QEMU) for the appliance, which is required when running inside a container where libvirtd is not available.

### `SYS_ADMIN` Capability

The libguestfs appliance launches an internal QEMU/KVM instance to boot its kernel and initrd. This requires the `SYS_ADMIN` capability (or privileged mode). The mutating webhook (Story 18.5) and SCC (Story 18.6) handle granting this capability in OpenShift.

For local testing:

```bash
podman run --rm --cap-add SYS_ADMIN soteria-ip-rewrite:dev guestfish --version
```

### `/dev/kvm` Access

For optimal performance, the container should have access to `/dev/kvm`. Without it, libguestfs falls back to software emulation (TCG), which is slower but functional.

## Verification

```bash
# Tool versions
podman run --rm soteria-ip-rewrite:dev guestfish --version
podman run --rm soteria-ip-rewrite:dev virt-inspector --version
podman run --rm soteria-ip-rewrite:dev augtool --version
podman run --rm soteria-ip-rewrite:dev command -v hivexregedit

# Appliance launch (requires SYS_ADMIN and /dev/kvm)
podman run --rm --cap-add SYS_ADMIN soteria-ip-rewrite:dev \
    guestfish --ro -a /dev/null run

# Image size (should be under 800 MB)
podman image inspect soteria-ip-rewrite:dev --format '{{.Size}}' | \
    awk '{printf "%.0f MB\n", $1/1024/1024}'
```

> **Note:** `hivexregedit` does not support `--version`. Use `command -v hivexregedit` or `hivexregedit --help` to verify presence.

## Image Registry

| Environment | Image |
|-------------|-------|
| CI | `soteria-ip-rewrite:ci` (built on every PR, not pushed) |
| Release | `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION` (pushed on tag) |

## Architecture

- **x86_64 only** — all OCP Virtualization certified Windows guests are x86_64.
- Based on `quay.io/centos/centos:stream9` (CentOS Stream 9).
- Image size: ~500–800 MB (guestfs-tools + kernel + supermin appliance + QEMU are inherently large).

## Related Stories

| Story | Description |
|-------|-------------|
| 18.1 (this) | Base container image with guestfs-tools |
| 18.2 | Entrypoint script — OS detection and dispatch |
| 18.3 | RHEL IP rewrite handler (Augeas-based) |
| 18.4 | Windows IP rewrite handler (hivex-based) |
| 18.5 | Mutating webhook — init container injection |
| 18.6 | Helm sub-chart and SCC |
| 18.9 | CI/CD pipeline integration |
