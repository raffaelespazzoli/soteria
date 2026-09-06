# Story 18.2: IP Rewrite Entrypoint Script — OS Detection & Dispatch

Status: ready-for-dev

## Story

As a developer,
I want an entrypoint script that parses IP configuration from environment variables, detects the guest OS on the VM disk, and dispatches to the correct OS-specific handler,
so that the init container can automatically handle both RHEL and Windows VMs without manual configuration.

## Acceptance Criteria

### AC1: Annotation parsing from environment variables
Given environment variables set by the webhook (e.g., `SOTERIA_ETH0_IP="10.0.2.100/24;10.0.2.1"`, `SOTERIA_DNS="10.0.2.10,10.0.2.11"`)
When the entrypoint script starts
Then it parses each `SOTERIA_*_IP` variable into interface name, IP address, prefix length, and gateway
And it parses the optional `SOTERIA_DNS` variable into a list of DNS server addresses

### AC2: Boot disk identification
Given the init container has multiple PVC volumes mounted (e.g., `/disks/rootdisk`, `/disks/datadisk`)
When the script scans each disk with `virt-inspector`
Then it identifies the disk containing an operating system (the boot disk)
And proceeds with that disk for IP rewriting
And logs a warning if no OS is detected on any disk

### AC3: OS detection and version identification
Given a boot disk is identified
When `virt-inspector` returns guest OS metadata
Then the script determines the OS family (`linux` or `windows`)
And the specific distribution and version (e.g., `rhel` `9.4`, `windows` `Server 2022`)
And logs the detected OS information

### AC4: Dispatch to RHEL handler
Given the OS is detected as RHEL (any supported version 7/8/9/10)
When dispatch occurs
Then the RHEL handler script is invoked with the parsed IP configuration and disk path

### AC5: Dispatch to Windows handler
Given the OS is detected as Windows (any supported version)
When dispatch occurs
Then the Windows handler script is invoked with the parsed IP configuration and disk path

### AC6: Unsupported OS handling
Given the OS is not RHEL or a supported Windows version (e.g., Ubuntu, Fedora, or undetectable)
When dispatch is attempted
Then the script exits with a non-zero code and a clear error message listing supported OSes
And the virt-launcher pod fails to start (init container failure)

### AC7: No-op when no IP annotations present
Given the init container starts but no `SOTERIA_*_IP` environment variables are set
When the entrypoint runs
Then the script exits 0 immediately (no-op, virt-launcher proceeds)
And logs an informational message that no IP rewrite was requested

## Tasks / Subtasks

- [ ] Task 1: Create `build/ip-rewrite/scripts/entrypoint.sh` with annotation parsing logic (AC: 1, 7)
  - [ ] 1.1: Add shebang, `set -euo pipefail`, and structured logging helper functions
  - [ ] 1.2: Enumerate `SOTERIA_*_IP` env vars using `env | grep '^SOTERIA_.*_IP='` pattern
  - [ ] 1.3: Parse each var: extract interface name (lowercase, between `SOTERIA_` and `_IP`), split value on `;` for address/prefix and gateway, split address on `/` for IP and prefix length
  - [ ] 1.4: Parse optional `SOTERIA_DNS` into comma-separated list
  - [ ] 1.5: Implement no-op exit (exit 0 with log) when no `SOTERIA_*_IP` vars found
- [ ] Task 2: Implement boot disk scanning loop using `virt-inspector` (AC: 2)
  - [ ] 2.1: Iterate disk image files under `/disks/*/` mount points
  - [ ] 2.2: Run `virt-inspector --xml -a <disk>` on each disk
  - [ ] 2.3: Check for `<operatingsystem>` element in output (indicates a boot disk)
  - [ ] 2.4: Log and skip disks where virt-inspector finds no OS (data disks)
  - [ ] 2.5: Exit non-zero with error if no disk contains an OS
- [ ] Task 3: Implement OS detection and version extraction from `virt-inspector` output (AC: 3)
  - [ ] 3.1: Extract `<name>` element value (`linux` or `windows`) using `xmllint --xpath`
  - [ ] 3.2: Extract `<distro>` element value (e.g., `rhel`, `windows`)
  - [ ] 3.3: Extract `<major_version>` and `<minor_version>` elements
  - [ ] 3.4: Extract `<product_name>` for logging (e.g., `Red Hat Enterprise Linux release 9.4`, `Windows Server 2022 Standard`)
  - [ ] 3.5: Log detected OS info: family, distro, version, product name
- [ ] Task 4: Implement dispatch logic to RHEL and Windows handlers (AC: 4, 5, 6)
  - [ ] 4.1: If `<name>` is `linux` AND `<distro>` is `rhel` → source/exec `rhel-handler.sh` with parsed config and disk path
  - [ ] 4.2: If `<name>` is `windows` → source/exec `windows-handler.sh` with parsed config and disk path
  - [ ] 4.3: Any other OS → exit 1 with error listing supported OSes (RHEL 7/8/9/10, Windows Server 2016/2019/2022/2025, Windows 10/11)
  - [ ] 4.4: Pass parsed IP config to handlers via exported environment variables or function arguments
- [ ] Task 5: Handle edge cases (AC: 6, 7)
  - [ ] 5.1: No `SOTERIA_*_IP` vars set → exit 0 (no-op)
  - [ ] 5.2: No OS found on any disk → exit 1 with descriptive error
  - [ ] 5.3: Unsupported OS detected → exit 1 with list of supported OSes
  - [ ] 5.4: `virt-inspector` command failure → exit 1 with error details
  - [ ] 5.5: Malformed env var value (missing `;`, missing `/`) → exit 1 with parse error
- [ ] Task 6: Create placeholder handler scripts (for dispatch testing before 18.3/18.4)
  - [ ] 6.1: Create `build/ip-rewrite/scripts/rhel-handler.sh` stub that logs received args and exits 0
  - [ ] 6.2: Create `build/ip-rewrite/scripts/windows-handler.sh` stub that logs received args and exits 0
- [ ] Task 7: Update `build/ip-rewrite/Containerfile` to include scripts (AC: all)
  - [ ] 7.1: Add `COPY scripts/ /scripts/` directive
  - [ ] 7.2: Add `RUN chmod +x /scripts/*.sh`
  - [ ] 7.3: Set `ENTRYPOINT ["/scripts/entrypoint.sh"]`
  - [ ] 7.4: Install `libxml2` package for `xmllint` (if not already a dependency of `guestfs-tools`)

## Dev Notes

### Story Intelligence Chain

**Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9**

What 18.1 accomplished:
- Created `build/ip-rewrite/Containerfile` based on `registry.access.redhat.com/ubi9/ubi`
- Installed `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport` via dnf
- Set `LIBGUESTFS_BACKEND=direct` environment variable (required for guestfish in containers)
- Created placeholder `/scripts/` directory via `RUN mkdir -p /scripts`
- Left `ENTRYPOINT` and `CMD` unset — deliberately deferred to this story (18.2)
- Created `build/ip-rewrite/README.md` documenting the image

What 18.1 deferred to this story:
- Entrypoint script creation and `COPY` into image
- Setting `ENTRYPOINT` in the Containerfile
- Any handler scripts (rhel-handler.sh, windows-handler.sh)

Patterns established by 18.1:
- **Directory convention**: All ip-rewrite build files live under `build/ip-rewrite/`
- **Package source**: ONLY `ubi-9-appstream-rpms` and `ubi-9-baseos-rpms` — NO EPEL, no third-party repos
- **NTFS support**: Use `libguestfs-winsupport` (NOT `ntfs-3g` which requires EPEL)
- **Image minimization**: `--nodocs`, `--setopt=install_weak_deps=False`, `dnf clean all`
- **Runtime requirement**: `SYS_ADMIN` capability needed for guestfish appliance

What this story (18.2) establishes for downstream:
- The entrypoint script that Stories 18.3 (RHEL handler) and 18.4 (Windows handler) will extend
- The handler interface contract: how handlers receive IP config and disk path
- The environment variable parsing convention used across the pipeline
- OS detection logic that determines which handler to invoke

**Dependencies downstream from this story:** 18.3 (RHEL handler), 18.4 (Windows handler) both depend on this story's entrypoint and handler interface.

### Critical Technical Details

#### virt-inspector XML Output Structure

The entrypoint parses `virt-inspector --xml -a <disk>` output. Key XML elements under `<operatingsystems><operatingsystem>`:

| Element | RHEL Example | Windows Example |
|---------|-------------|-----------------|
| `<name>` | `linux` | `windows` |
| `<distro>` | `rhel` | `windows` |
| `<major_version>` | `9` | `10` |
| `<minor_version>` | `4` | `0` |
| `<product_name>` | `Red Hat Enterprise Linux release 9.4 (Plow)` | `Windows Server 2022 Standard` |
| `<windows_systemroot>` | *(absent)* | `/Windows` |
| `<root>` | `/dev/sda2` or `/dev/VolGroup/lv_root` | `/dev/sda2` |

**RHEL XML example:**
```xml
<operatingsystems>
  <operatingsystem>
    <name>linux</name>
    <distro>rhel</distro>
    <product_name>Red Hat Enterprise Linux Server release 9.4</product_name>
    <major_version>9</major_version>
    <minor_version>4</minor_version>
    <root>/dev/VolGroup/lv_root</root>
    <mountpoints>
      <mountpoint dev="/dev/VolGroup/lv_root">/</mountpoint>
      <mountpoint dev="/dev/sda1">/boot</mountpoint>
    </mountpoints>
  </operatingsystem>
</operatingsystems>
```

**Windows XML example:**
```xml
<operatingsystems>
  <operatingsystem>
    <name>windows</name>
    <distro>windows</distro>
    <product_name>Windows Server 2022 Standard</product_name>
    <major_version>10</major_version>
    <minor_version>0</minor_version>
    <windows_systemroot>/Windows</windows_systemroot>
    <windows_current_control_set>ControlSet001</windows_current_control_set>
  </operatingsystem>
</operatingsystems>
```

**Important Windows version caveat:** Windows Server 2016/2019/2022/2025 and Windows 10/11 all report `<major_version>10</major_version>` and `<minor_version>0</minor_version>`. Distinguish them via `<product_name>` string. For dispatch, this distinction is NOT needed — all Windows versions use the same handler. Log the `<product_name>` for operator visibility.

#### XML Parsing — Use `xmllint`, NOT `xmlstarlet`

`xmlstarlet` is EPEL-only on RHEL 9 / UBI9. **DO NOT install it.** Use `xmllint` from the `libxml2` package (available in standard UBI9 repos). `libxml2` is likely already installed as a dependency of `guestfs-tools` — verify at build time and only add an explicit `dnf install libxml2` if needed.

**`xmllint --xpath` examples for the entrypoint:**
```bash
# Extract OS name (linux/windows)
OS_NAME=$(xmllint --xpath 'string(//operatingsystem/name)' "$INSPECT_XML")

# Extract distro (rhel/windows/fedora)
OS_DISTRO=$(xmllint --xpath 'string(//operatingsystem/distro)' "$INSPECT_XML")

# Extract version
OS_MAJOR=$(xmllint --xpath 'string(//operatingsystem/major_version)' "$INSPECT_XML")
OS_MINOR=$(xmllint --xpath 'string(//operatingsystem/minor_version)' "$INSPECT_XML")

# Extract product name for logging
OS_PRODUCT=$(xmllint --xpath 'string(//operatingsystem/product_name)' "$INSPECT_XML")
```

The `string()` XPath function returns the text content directly (no XML tags in output). If `virt-inspector` finds no OS on a disk, the `<operatingsystems>` element will be empty — `xmllint --xpath` will return an empty string or error, which the script should handle.

#### Environment Variable Convention

Webhook (Story 18.5) transforms annotations → env vars:
```
Annotation:  soteria.io/eth0-ip → Env var: SOTERIA_ETH0_IP
Annotation:  soteria.io/eth1-ip → Env var: SOTERIA_ETH1_IP
Annotation:  soteria.io/dns     → Env var: SOTERIA_DNS
```

**Transformation rule**: Strip `soteria.io/` prefix, uppercase everything, replace `-` with `_`, prepend `SOTERIA_`.

**Value format for IP vars:**
```
SOTERIA_ETH0_IP="10.0.2.100/24;10.0.2.1"
                 │          │  │
                 │          │  └── gateway
                 │          └── prefix length
                 └── IP address
```

**Parsing pseudocode:**
```bash
for var in $(env | grep '^SOTERIA_.*_IP=' | sort); do
  varname="${var%%=*}"       # e.g., SOTERIA_ETH0_IP
  value="${var#*=}"           # e.g., 10.0.2.100/24;10.0.2.1

  # Extract interface name: strip SOTERIA_ prefix and _IP suffix, lowercase
  iface="${varname#SOTERIA_}"
  iface="${iface%_IP}"
  iface=$(echo "$iface" | tr '[:upper:]' '[:lower:]')  # e.g., eth0

  # Split on ';' for address and gateway
  addr_part="${value%;*}"    # e.g., 10.0.2.100/24
  gateway="${value#*;}"      # e.g., 10.0.2.1

  # Split address on '/' for IP and prefix
  ip="${addr_part%/*}"       # e.g., 10.0.2.100
  prefix="${addr_part#*/}"   # e.g., 24
done
```

#### Boot Disk Discovery

The init container receives PVC-backed volumes mounted at `/disks/<volumeName>/`. Each mount point contains a raw disk image file (the PVC's block device). The disk path for `virt-inspector` is the block device path, not a file within the mount.

**Discovery approach:**
```bash
# Iterate disk devices under /disks/
for disk in /disks/*/disk.img; do
  # Alternative: KubeVirt may use block devices directly
  # Check both disk.img files and block device nodes
done
```

**Important:** KubeVirt virt-launcher pods mount PVC data volumes at paths like `/disks/<volumeName>`. The actual disk image may be a regular file (`disk.img`) or a block device. The script should handle both by checking what's available. Run `virt-inspector -a <path>` on each candidate.

When `virt-inspector` finds an OS, stop scanning (use the first boot disk found). If multiple disks have OSes (rare, multi-boot), use the first one and log a warning.

#### Handler Interface Contract

The entrypoint passes configuration to handler scripts. Handlers receive context via exported environment variables:

```bash
# Exported by entrypoint before calling handler:
export REWRITE_DISK="/disks/rootdisk/disk.img"     # Path to boot disk
export REWRITE_OS_NAME="linux"                       # OS family
export REWRITE_OS_DISTRO="rhel"                      # Distribution
export REWRITE_OS_MAJOR="9"                          # Major version
export REWRITE_OS_MINOR="4"                          # Minor version
export REWRITE_OS_PRODUCT="Red Hat Enterprise Linux release 9.4"
export REWRITE_DNS="10.0.2.10,10.0.2.11"            # Optional, from SOTERIA_DNS

# Per-interface config exported as indexed arrays or repeated vars:
export REWRITE_IFACE_0="eth0"
export REWRITE_IP_0="10.0.2.100"
export REWRITE_PREFIX_0="24"
export REWRITE_GATEWAY_0="10.0.2.1"
export REWRITE_IFACE_1="eth1"
export REWRITE_IP_1="192.168.1.50"
export REWRITE_PREFIX_1="16"
export REWRITE_GATEWAY_1="192.168.1.1"
export REWRITE_IFACE_COUNT="2"
```

Then the handler is called:
```bash
source /scripts/rhel-handler.sh    # or windows-handler.sh
```

Using `source` (not `exec`) allows the handler to access all exported variables and the entrypoint to check the handler's return code.

#### Logging Convention

Use structured, consistent log output. All log lines should be prefixed for easy parsing:

```bash
log_info()  { echo "[INFO]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"; }
log_warn()  { echo "[WARN]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }
log_error() { echo "[ERROR] $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }
```

Log key events:
- Script start with env var count
- Each parsed interface config
- Each disk scanned (found OS / no OS)
- Detected OS details
- Dispatch target (which handler)
- Handler completion status

### Project Structure Notes

After this story, the `build/ip-rewrite/` directory looks like:

```
build/
└── ip-rewrite/
    ├── Containerfile             ← Modified (add COPY, ENTRYPOINT)
    ├── README.md                 ← Created by 18.1
    └── scripts/
        ├── entrypoint.sh         ← NEW (this story, main logic)
        ├── rhel-handler.sh       ← NEW (this story, placeholder stub)
        └── windows-handler.sh    ← NEW (this story, placeholder stub)
```

**Containerfile modifications** (on top of 18.1's version):
```dockerfile
# ... existing FROM, RUN dnf install, ENV LIBGUESTFS_BACKEND=direct ...

# Copy entrypoint and handler scripts
COPY scripts/ /scripts/
RUN chmod +x /scripts/*.sh

ENTRYPOINT ["/scripts/entrypoint.sh"]
```

**Remove** the `RUN mkdir -p /scripts` line from 18.1 since `COPY scripts/ /scripts/` creates the directory implicitly.

### Anti-Patterns / DO NOT

- **DO NOT install `xmlstarlet`** — it requires EPEL, which is prohibited in UBI9. Use `xmllint` from `libxml2` (standard UBI9 repo, likely already a guestfs-tools dependency).
- **DO NOT add EPEL or any third-party repositories** — this image must only use official UBI9 repos.
- **DO NOT implement the RHEL handler logic** — that is Story 18.3. Create only a placeholder stub that logs received arguments and exits 0.
- **DO NOT implement the Windows handler logic** — that is Story 18.4. Create only a placeholder stub.
- **DO NOT modify `.github/workflows/ci.yml` or `release.yml`** — CI integration is Story 18.9.
- **DO NOT modify the `Makefile`** — build targets are Story 18.9.
- **DO NOT use `virt-inspector --format=json`** — the `--format` flag is for the old Perl virt-inspector (deprecated). The current C version uses `--xml` for XML output (default). There is no JSON output mode.
- **DO NOT use `grep`/`sed`/`awk` to parse XML** — use `xmllint --xpath` for reliable XML extraction. XML parsing with text tools is brittle and error-prone.
- **DO NOT hardcode disk paths** — discover disks dynamically from `/disks/*/`.
- **DO NOT attempt to modify disk images in the entrypoint** — the entrypoint only detects and dispatches. All disk modification is done by the handler scripts (18.3, 18.4).
- **DO NOT use `#!/bin/bash`** — UBI9 has bash at `/bin/bash`, but use `#!/usr/bin/env bash` for portability within the container.
- **DO NOT create unit tests for the shell script** — shell script testing is handled in Story 18.7 via disk image fixtures. This story focuses on the script itself.

### Verification Commands

After building, run these to validate the entrypoint behavior:

```bash
# Build updated image (AC: all)
podman build -t soteria-ip-rewrite:dev build/ip-rewrite/

# AC7: No-op mode (no SOTERIA_*_IP vars)
podman run --rm soteria-ip-rewrite:dev
# Expected: exits 0 with "[INFO] No IP rewrite requested" message

# AC1: Env var parsing (will fail at disk scan since no disks, but parse output visible)
podman run --rm \
  -e SOTERIA_ETH0_IP="10.0.2.100/24;10.0.2.1" \
  -e SOTERIA_DNS="10.0.2.10,10.0.2.11" \
  soteria-ip-rewrite:dev
# Expected: logs parsed config, then fails with "no disks found" error

# Verify xmllint is available
podman run --rm soteria-ip-rewrite:dev xmllint --version

# Verify scripts are executable
podman run --rm --entrypoint /bin/bash soteria-ip-rewrite:dev \
  -c "ls -la /scripts/"
```

**Note:** Full AC2-AC6 validation requires actual disk images mounted as volumes. That testing is deferred to Story 18.7 (disk image fixtures). The entrypoint script should be written so that the parsing and no-op paths can be verified without disks.

### References

- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18"]
- [virt-inspector XML format: https://libguestfs.org/virt-inspector.1.html]
- [virt-inspector RHEL 7 docs with example output: https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/7/html/virtualization_deployment_and_administration_guide/sect-guest_virtual_machine_disk_access_with_offline_tools-virt_inspector_inspecting_guest_virtual_machines]
- [libguestfs Windows Server 2025 detection patch: https://git.almalinux.org/rpms/libguestfs/commit/146a6b6f2f641b903e8e9adab8df6aea30d3f5b7]
- [xmllint manual: https://gnome.pages.gitlab.gnome.org/libxml2/xmllint.html]

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
