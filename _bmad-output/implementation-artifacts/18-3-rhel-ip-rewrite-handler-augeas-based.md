# Story 18.3: RHEL IP Rewrite Handler — Augeas-Based

Status: ready-for-dev

## Story

As a developer,
I want a handler script that uses guestfish and Augeas to rewrite static IP configuration on RHEL 7/8/9/10 guest filesystems,
so that RHEL VMs boot with the correct network configuration after relocation to a new subnet.

## Acceptance Criteria

### AC1: RHEL 7 — ifcfg format
Given a RHEL 7 disk image with a static IP configured in `/etc/sysconfig/network-scripts/ifcfg-<interface>`
When the handler is invoked with interface `eth0`, IP `10.0.2.100`, prefix `24`, gateway `10.0.2.1`
Then the `IPADDR`, `PREFIX`, and `GATEWAY` fields are updated in the ifcfg file via Augeas
And `BOOTPROTO` remains `none` (static)
And if DNS is provided, `DNS1` and `DNS2` are updated

### AC2: RHEL 8 — ifcfg or NM keyfile
Given a RHEL 8 disk image with static IP configured in either ifcfg format or NM keyfile format
When the handler is invoked
Then it detects which format is in use (check `/etc/NetworkManager/system-connections/` first, fall back to `/etc/sysconfig/network-scripts/`)
And updates the correct file via Augeas

### AC3: RHEL 9/10 — NM keyfile format
Given a RHEL 9 or 10 disk image with static IP in `/etc/NetworkManager/system-connections/<connection>.nmconnection`
When the handler is invoked with interface `eth0`, IP `10.0.2.100`, prefix `24`, gateway `10.0.2.1`
Then the `[ipv4]` section is updated: `address1=10.0.2.100/24,10.0.2.1`, `method=manual`
And if DNS is provided, `dns=10.0.2.10;10.0.2.11;` is updated

### AC4: Multi-NIC support
Given a RHEL disk image with two interfaces (eth0, eth1) each with static IPs
When the handler is invoked with IP configuration for both interfaces
Then both interface config files are updated independently
And interfaces not listed in the annotation set are left untouched

### AC5: Interface name matching
Given the annotation specifies interface name `eth0`
When the handler searches for config files
Then it matches against the interface name in the config file (`DEVICE=eth0` in ifcfg, or `[connection] interface-name=eth0` in NM keyfile)
And reports an error if the specified interface is not found in any config file

### AC6: Idempotency
Given the handler has already rewritten the IP on a previous run
When the handler is run again with the same IP configuration
Then the operation completes successfully without errors (idempotent)

### AC7: Filesystem is cleanly unmounted
Given the handler completes (success or failure)
When guestfish exits
Then the guest filesystem is properly unmounted and synced
And no corruption is introduced to the disk image

## Tasks / Subtasks

- [ ] Task 1: Create `build/ip-rewrite/scripts/rhel-handler.sh` skeleton (AC: all)
  - [ ] 1.1: Add shebang (`#!/usr/bin/env bash`), `set -euo pipefail`
  - [ ] 1.2: Import logging helpers from entrypoint (or redefine — the handler is `source`d by entrypoint, so `log_info`/`log_warn`/`log_error` are already available)
  - [ ] 1.3: Read all `REWRITE_*` environment variables set by the entrypoint (see Handler Interface Contract below)
  - [ ] 1.4: Validate required variables are set (`REWRITE_DISK`, `REWRITE_IFACE_COUNT`, at least one `REWRITE_IFACE_0`)
- [ ] Task 2: Implement config format auto-detection (AC: 2, 3)
  - [ ] 2.1: Inside the guestfish session, after `aug-init / 0`, scan for NM keyfiles first: `aug-match /files/etc/NetworkManager/system-connections/*/connection/interface-name`
  - [ ] 2.2: If a `.nmconnection` file exists for the target interface → use NM keyfile path
  - [ ] 2.3: Fall back to ifcfg: check if `aug-get /files/etc/sysconfig/network-scripts/ifcfg-*/DEVICE` matches the interface name
  - [ ] 2.4: If neither found, try matching ifcfg file by filename convention (`ifcfg-<iface>`)
  - [ ] 2.5: Log which format was detected for each interface
- [ ] Task 3: Implement ifcfg rewrite path (RHEL 7/8) using guestfish + Augeas (AC: 1, 2)
  - [ ] 3.1: Use `aug-set /files/etc/sysconfig/network-scripts/ifcfg-<iface>/IPADDR <ip>` to set IP
  - [ ] 3.2: Use `aug-set .../PREFIX <prefix>` to set prefix length
  - [ ] 3.3: Use `aug-set .../GATEWAY <gateway>` to set gateway
  - [ ] 3.4: Ensure `BOOTPROTO` is `none` (static) — set it explicitly
  - [ ] 3.5: If `REWRITE_DNS` is set, update `DNS1` and `DNS2` fields (split comma-separated list)
- [ ] Task 4: Implement NM keyfile rewrite path (RHEL 8/9/10) using guestfish + Augeas (AC: 2, 3)
  - [ ] 4.1: Use `aug-set /files/etc/NetworkManager/system-connections/<name>.nmconnection/ipv4/method manual`
  - [ ] 4.2: Use `aug-set .../ipv4/address1 <ip>/<prefix>,<gateway>` (NM keyfile combined format)
  - [ ] 4.3: If `REWRITE_DNS` is set, update `dns` field: `aug-set .../ipv4/dns <ip1>;<ip2>;`
- [ ] Task 5: Implement interface name matching and multi-NIC iteration (AC: 4, 5)
  - [ ] 5.1: Loop over `REWRITE_IFACE_COUNT` interfaces (index 0..N-1)
  - [ ] 5.2: For each interface, run the auto-detection logic (Task 2) to find the config file
  - [ ] 5.3: Apply the appropriate rewrite (ifcfg or NM keyfile) for each interface
  - [ ] 5.4: Log each interface rewrite: interface name, old config location, new IP
  - [ ] 5.5: Exit non-zero if any specified interface is not found in the guest filesystem
- [ ] Task 6: Implement guestfish session management (AC: 6, 7)
  - [ ] 6.1: Open a single guestfish session for the disk: `guestfish -a "$REWRITE_DISK" -i <<'GUESTFISH_SCRIPT'`
  - [ ] 6.2: Initialize Augeas: `aug-init / 0`
  - [ ] 6.3: Perform all interface rewrites within the same session
  - [ ] 6.4: Commit all changes: `aug-save`
  - [ ] 6.5: Guestfish auto-unmounts and syncs on exit (closing the heredoc)
  - [ ] 6.6: Check guestfish exit code and propagate errors

## Dev Notes

### Story Intelligence Chain

**Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9**

What 18.1 accomplished:
- Created `build/ip-rewrite/Containerfile` based on `registry.access.redhat.com/ubi9/ubi`
- Installed `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport` via dnf
- Set `LIBGUESTFS_BACKEND=direct` environment variable (required for guestfish in containers)
- Created placeholder `/scripts/` directory
- Left `ENTRYPOINT` and `CMD` unset (deferred to 18.2)

Patterns established by 18.1:
- **Package source**: ONLY `ubi-9-appstream-rpms` and `ubi-9-baseos-rpms` — no EPEL, no third-party repos
- **NTFS support**: Use `libguestfs-winsupport` (NOT `ntfs-3g`)
- **Image minimization**: `--nodocs`, `--setopt=install_weak_deps=False`, `dnf clean all`
- **Runtime requirement**: `SYS_ADMIN` capability needed for guestfish appliance

**Predecessor: Story 18.2 — IP Rewrite Entrypoint Script: OS Detection & Dispatch**

What 18.2 accomplished:
- Created `build/ip-rewrite/scripts/entrypoint.sh` — parses `SOTERIA_*_IP` env vars and dispatches to handlers
- Created placeholder stubs for `rhel-handler.sh` and `windows-handler.sh`
- Updated `Containerfile` to `COPY scripts/ /scripts/`, set `ENTRYPOINT ["/scripts/entrypoint.sh"]`
- Installed `libxml2` for `xmllint` (XML parsing of `virt-inspector` output)
- Implemented boot disk scanning via `virt-inspector` and OS detection

What 18.2 deferred to this story (18.3):
- **The entire RHEL handler implementation** — the current `rhel-handler.sh` is a placeholder stub that only logs received arguments and exits 0
- This story replaces that stub with the full Augeas-based IP rewrite logic

Patterns established by 18.2 — handler interface contract:
- **The handler is invoked via `source /scripts/rhel-handler.sh`** (NOT `exec`), so all exported variables from the entrypoint are available
- **Logging helpers** (`log_info`, `log_warn`, `log_error`) are defined in the entrypoint and available to the handler
- **Script convention**: `#!/usr/bin/env bash`, `set -euo pipefail`

**What this story (18.3) establishes for downstream:**
- Completes the RHEL handler — no placeholder stubs remain for RHEL
- Augeas patterns (ifcfg + NM keyfile) that inform Story 18.7 (test fixtures with RHEL disk images)
- Guestfish session management patterns reusable by Story 18.4 (Windows handler)

**Dependencies downstream:** Story 18.7 (Unit & Integration Tests — Disk Image Fixtures) depends on this handler for RHEL test cases.

### Critical Technical Details

#### Handler Interface Contract (from Story 18.2)

The entrypoint exports these environment variables before `source`ing the handler:

```bash
REWRITE_DISK="/disks/rootdisk/disk.img"         # Path to boot disk
REWRITE_OS_NAME="linux"                          # OS family
REWRITE_OS_DISTRO="rhel"                         # Distribution
REWRITE_OS_MAJOR="9"                             # Major version
REWRITE_OS_MINOR="4"                             # Minor version
REWRITE_OS_PRODUCT="Red Hat Enterprise Linux release 9.4"
REWRITE_DNS="10.0.2.10,10.0.2.11"               # Optional, from SOTERIA_DNS

# Per-interface config (indexed):
REWRITE_IFACE_0="eth0"
REWRITE_IP_0="10.0.2.100"
REWRITE_PREFIX_0="24"
REWRITE_GATEWAY_0="10.0.2.1"
REWRITE_IFACE_1="eth1"
REWRITE_IP_1="192.168.1.50"
REWRITE_PREFIX_1="16"
REWRITE_GATEWAY_1="192.168.1.1"
REWRITE_IFACE_COUNT="2"
```

The handler reads these variables — it does NOT re-parse annotations or env vars. The handler's sole responsibility is disk modification.

#### Augeas Lens Selection — Critical

Two different Augeas lenses auto-load for the two config formats:

| Config Format | Augeas Lens | Auto-load Path | Used By |
|---------------|-------------|----------------|---------|
| ifcfg (`KEY=value`) | `Shellvars.lns` | `/etc/sysconfig/network-scripts/ifcfg-*` | RHEL 7, RHEL 8 (legacy) |
| NM keyfile (`.ini` format) | `NetworkManager.lns` | `/etc/NetworkManager/system-connections/*` | RHEL 8 (modern), RHEL 9, RHEL 10 |

Both lenses auto-load when `aug-init / 0` is called inside guestfish. The developer does NOT need to specify the lens manually — Augeas detects the file path and applies the correct lens automatically.

#### Augeas Path Syntax for ifcfg Files (Shellvars.lns)

The `Shellvars.lns` lens parses shell-style `KEY=value` files. Each variable becomes a direct child node.

**Augeas tree for `/etc/sysconfig/network-scripts/ifcfg-eth0`:**
```
/files/etc/sysconfig/network-scripts/ifcfg-eth0/TYPE = "Ethernet"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/BOOTPROTO = "none"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/DEVICE = "eth0"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/IPADDR = "192.168.1.10"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/PREFIX = "24"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/GATEWAY = "192.168.1.1"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/DNS1 = "10.0.2.10"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/DNS2 = "10.0.2.11"
/files/etc/sysconfig/network-scripts/ifcfg-eth0/ONBOOT = "yes"
```

**guestfish commands to rewrite ifcfg:**
```bash
aug-set /files/etc/sysconfig/network-scripts/ifcfg-eth0/IPADDR 10.0.2.100
aug-set /files/etc/sysconfig/network-scripts/ifcfg-eth0/PREFIX 24
aug-set /files/etc/sysconfig/network-scripts/ifcfg-eth0/GATEWAY 10.0.2.1
aug-set /files/etc/sysconfig/network-scripts/ifcfg-eth0/BOOTPROTO none
# DNS (optional):
aug-set /files/etc/sysconfig/network-scripts/ifcfg-eth0/DNS1 10.0.2.10
aug-set /files/etc/sysconfig/network-scripts/ifcfg-eth0/DNS2 10.0.2.11
```

#### Augeas Path Syntax for NM Keyfile (NetworkManager.lns)

The `NetworkManager.lns` lens parses GLib key-value (`.ini`) files. Section names are top-level nodes; keys are children.

**Augeas tree for `/etc/NetworkManager/system-connections/eth0.nmconnection`:**
```
/files/etc/NetworkManager/system-connections/eth0.nmconnection/connection/id = "eth0"
/files/etc/NetworkManager/system-connections/eth0.nmconnection/connection/uuid = "..."
/files/etc/NetworkManager/system-connections/eth0.nmconnection/connection/type = "802-3-ethernet"
/files/etc/NetworkManager/system-connections/eth0.nmconnection/connection/interface-name = "eth0"
/files/etc/NetworkManager/system-connections/eth0.nmconnection/ipv4/method = "manual"
/files/etc/NetworkManager/system-connections/eth0.nmconnection/ipv4/address1 = "192.168.1.10/24,192.168.1.1"
/files/etc/NetworkManager/system-connections/eth0.nmconnection/ipv4/dns = "10.0.2.10;10.0.2.11;"
```

**guestfish commands to rewrite NM keyfile:**
```bash
aug-set /files/etc/NetworkManager/system-connections/eth0.nmconnection/ipv4/method manual
aug-set /files/etc/NetworkManager/system-connections/eth0.nmconnection/ipv4/address1 10.0.2.100/24,10.0.2.1
# DNS (optional):
aug-set /files/etc/NetworkManager/system-connections/eth0.nmconnection/ipv4/dns 10.0.2.10;10.0.2.11;
```

**NM keyfile address format:** `address1=<IP>/<prefix>,<gateway>` — the gateway is appended after a comma, NOT a semicolon. This differs from the annotation format (`<IP>/<prefix>;<gateway>` with semicolon). The handler must translate the format.

#### Config Format Detection Logic

The handler must determine whether each interface uses ifcfg or NM keyfile format. Detection strategy:

```
For each interface name (e.g., "eth0"):
  1. FIRST: Search NM keyfile directory
     - Use aug-match to find: /files/etc/NetworkManager/system-connections/*/connection/interface-name
     - For each match, aug-get the value and compare to the target interface name
     - If found → use NM keyfile path for this interface
  2. FALLBACK: Search ifcfg directory
     - Check if /files/etc/sysconfig/network-scripts/ifcfg-<iface> exists (by filename convention)
     - If exists, verify DEVICE field matches: aug-get .../DEVICE
     - If found → use ifcfg path for this interface
  3. EXTENDED FALLBACK: Scan all ifcfg-* files
     - aug-match /files/etc/sysconfig/network-scripts/ifcfg-*/DEVICE
     - For each match, aug-get the value and compare to target interface name
     - This catches cases where the ifcfg filename doesn't match the DEVICE name
  4. NOT FOUND: exit 1 with error
```

This order (NM keyfile first, ifcfg second) is correct because:
- RHEL 9/10 only uses NM keyfile
- RHEL 8 may use either, but if both exist, NM keyfile takes precedence
- RHEL 7 only has ifcfg

The `REWRITE_OS_MAJOR` variable is available but detection by file presence is more robust than version branching.

#### NM Keyfile File Naming — CRITICAL

NM keyfile filenames are NOT always `<interface-name>.nmconnection`. They can be:
- `eth0.nmconnection` (matches interface name)
- `Wired connection 1.nmconnection` (auto-generated by NetworkManager)
- `my-custom-profile.nmconnection` (user-defined)

**The `[connection] interface-name=<iface>` field inside the file is the only reliable way to match interfaces.** Never match by filename alone.

#### guestfish Session Structure

The handler runs within a single guestfish heredoc session. The `-i` flag auto-mounts all guest filesystems and handles LVM (runs `vgscan` + `vgchange -ay` internally).

**Complete guestfish session template:**
```bash
guestfish -a "$REWRITE_DISK" -i <<'GUESTFISH_SCRIPT'
  aug-init / 0

  # --- Interface eth0 ---
  # (auto-detect format, then apply aug-set commands)

  # --- Interface eth1 ---
  # (repeat for each interface)

  aug-save
GUESTFISH_SCRIPT
```

**Critical: heredoc quoting.** Use `<<'GUESTFISH_SCRIPT'` (single-quoted delimiter) to prevent shell expansion inside the heredoc. However, this means shell variables (like `$ip`, `$prefix`) cannot be interpolated directly. Solutions:

1. **Use an unquoted heredoc** (`<<GUESTFISH_SCRIPT`) and carefully escape any guestfish syntax that looks like shell variables. Since guestfish commands don't use `$`, this is safe.
2. **Build the guestfish commands as a string** in bash, then pipe to guestfish: `echo "$commands" | guestfish -a "$REWRITE_DISK" -i`
3. **Write commands to a temp file** and use `guestfish -a "$REWRITE_DISK" -i -f /tmp/commands.gf`

**Recommended approach: Option 2 (pipe).** Build the complete guestfish command string in bash (where variable interpolation works), then pipe to `guestfish -a "$REWRITE_DISK" -i`. This keeps the script logic clean and avoids heredoc escaping issues.

```bash
gf_commands="aug-init / 0\n"

for i in $(seq 0 $((REWRITE_IFACE_COUNT - 1))); do
  # Build aug-set commands using bash variables
  # ... detection and rewrite logic ...
  gf_commands+="aug-set /files/.../IPADDR ${ip}\n"
done

gf_commands+="aug-save\n"
echo -e "$gf_commands" | guestfish -a "$REWRITE_DISK" -i
```

**Alternative: two-phase approach.** Use a read-only guestfish session first to detect config file locations and formats, capture results in bash, then run a write guestfish session with the exact `aug-set` commands. This is cleaner but requires two guestfish invocations (more overhead but more debuggable).

#### Interface Name Matching via guestfish aug-match

To find which `.nmconnection` file owns a given interface, use the `aug-match` command inside guestfish:

```bash
# List all interface-name values across all NM keyfiles:
aug-match /files/etc/NetworkManager/system-connections/*/connection/interface-name

# Returns paths like:
# /files/etc/NetworkManager/system-connections/eth0.nmconnection/connection/interface-name
# /files/etc/NetworkManager/system-connections/Wired connection 1.nmconnection/connection/interface-name

# Then aug-get each path to find the interface name value
```

For ifcfg files:
```bash
# List all DEVICE values across all ifcfg files:
aug-match /files/etc/sysconfig/network-scripts/ifcfg-*/DEVICE

# Returns paths like:
# /files/etc/sysconfig/network-scripts/ifcfg-eth0/DEVICE
```

**Problem: capturing aug-match output.** When using the pipe approach (`echo commands | guestfish`), guestfish output goes to stdout. The handler needs to capture this output to make decisions. This requires the two-phase approach:

**Phase 1 — Discovery (read-only):**
```bash
# Capture the detection output
detect_output=$(guestfish --ro -a "$REWRITE_DISK" -i <<'EOF'
aug-init / 0
aug-match /files/etc/NetworkManager/system-connections/*/connection/interface-name
aug-match /files/etc/sysconfig/network-scripts/ifcfg-*/DEVICE
EOF
)
# Parse detect_output to determine file paths for each interface
```

**Phase 2 — Rewrite (read-write):**
```bash
# Build and execute aug-set commands based on Phase 1 results
echo -e "$gf_commands" | guestfish -a "$REWRITE_DISK" -i
```

This two-phase approach is **recommended** because:
- Phase 1 is read-only (`--ro` flag) — safe, no risk of partial writes
- Phase 2 is a simple sequence of `aug-set` commands — deterministic, no conditional logic inside guestfish
- Easier to debug — detection results are logged in bash between phases
- guestfish handles unmount/sync on each session exit

**Tradeoff:** Two guestfish invocations means the appliance boots twice (~2-5 seconds each). This is acceptable for an init container that runs once before VM boot.

#### DNS Rewrite Details

**ifcfg format:** DNS servers are individual fields: `DNS1=10.0.2.10`, `DNS2=10.0.2.11`, etc. Split the comma-separated `REWRITE_DNS` value and assign to `DNS1`, `DNS2`, etc. Maximum practical: `DNS1` through `DNS3` (most configs use 2).

**NM keyfile format:** DNS servers are a single semicolon-separated field: `dns=10.0.2.10;10.0.2.11;` (trailing semicolon is required by NM keyfile format). Convert the comma-separated `REWRITE_DNS` value: replace commas with semicolons and append a trailing semicolon.

**DNS is optional.** If `REWRITE_DNS` is empty/unset, do NOT modify DNS fields. Leave the existing DNS configuration untouched.

#### Augeas Version on UBI9

The UBI9 container image (from Story 18.1) installs the `augeas` package from RHEL 9 AppStream. The current version is **augeas-1.14.1-3.el9**, which includes the fix for the [double-quote bug](https://github.com/hercules-team/augeas/issues/719) when creating new entries with the `NetworkManager.lns` lens.

This means both **editing existing entries** and **creating new entries** (e.g., adding a `dns` field that didn't exist) via `aug-set` will work correctly without spurious double quotes.

The Augeas version used by guestfish's `aug-*` commands is the one inside the libguestfs appliance, which is built from the container's packages via supermin. Since `guestfs-tools` depends on `augeas-libs`, the appliance inherits the container's augeas version.

#### RHEL Version vs Config Format Matrix

| RHEL Version | `REWRITE_OS_MAJOR` | Default Config Format | ifcfg Support | NM Keyfile Support |
|-------------|--------------------|-----------------------|---------------|---------------------|
| RHEL 7 | `7` | ifcfg only | ✅ Primary | ❌ Not used |
| RHEL 8 | `8` | ifcfg (default) or NM keyfile | ✅ Common | ✅ Available |
| RHEL 9 | `9` | NM keyfile only | ⚠️ Deprecated, may exist from upgrades | ✅ Primary |
| RHEL 10 | `10` | NM keyfile only | ❌ Removed | ✅ Primary |

**Do NOT use version branching.** Detect by file presence (NM keyfile first, ifcfg fallback) regardless of RHEL version. This handles upgrade scenarios where RHEL 9 still has ifcfg files from a RHEL 7/8 upgrade.

#### Error Handling

The handler must exit non-zero on any of these conditions:
- `REWRITE_DISK` not set or file doesn't exist
- `REWRITE_IFACE_COUNT` is 0 or not set
- An interface specified in `REWRITE_IFACE_<N>` has no matching config file in the guest
- guestfish exits non-zero (appliance failure, disk access error)
- `aug-save` fails (write error, disk full)

On success, exit 0. The entrypoint checks the handler's return code after `source`.

### Project Structure Notes

After this story, `build/ip-rewrite/scripts/rhel-handler.sh` changes from a placeholder stub to a complete implementation. No new files are created — only the existing stub is replaced.

```
build/
└── ip-rewrite/
    ├── Containerfile                ← No changes (already COPYs scripts/)
    ├── README.md                    ← No changes
    └── scripts/
        ├── entrypoint.sh            ← No changes (created by 18.2)
        ├── rhel-handler.sh          ← MODIFIED: stub → full implementation
        └── windows-handler.sh       ← No changes (still a stub, for Story 18.4)
```

**No Containerfile changes needed.** Story 18.2 already added `COPY scripts/ /scripts/` and `RUN chmod +x /scripts/*.sh`. The handler script is picked up automatically.

**No new packages needed.** All required tools (`guestfish`, `augeas` / `aug-*` commands) are already installed by Story 18.1's Containerfile. The `aug-*` commands are built into guestfish — they run inside the libguestfs appliance, not as standalone binaries.

### Anti-Patterns / DO NOT

- **DO NOT implement Windows handler logic** — that is Story 18.4's responsibility. Only modify `rhel-handler.sh`.
- **DO NOT modify `entrypoint.sh`** — the entrypoint already handles dispatch. If the handler interface needs adjustment, document the change for a follow-up, but do not modify the entrypoint.
- **DO NOT modify `Containerfile`** — no new packages or COPY directives are needed.
- **DO NOT modify `.github/workflows/ci.yml` or `release.yml`** — CI integration is Story 18.9.
- **DO NOT modify the `Makefile`** — build targets are Story 18.9.
- **DO NOT use `augtool` CLI** — use guestfish's built-in `aug-*` commands (`aug-init`, `aug-set`, `aug-get`, `aug-match`, `aug-save`). The `augtool` binary runs on the host, not inside the guestfish appliance where the guest filesystem is mounted.
- **DO NOT use `grep`/`sed`/`awk` to edit config files** — use Augeas exclusively. Augeas preserves file structure, comments, and formatting. Text manipulation tools are brittle and error-prone for structured config files.
- **DO NOT branch on `REWRITE_OS_MAJOR` to choose config format** — detect by file presence (NM keyfile first, ifcfg fallback). Version branching breaks on upgraded systems.
- **DO NOT match NM keyfile by filename** — use the `[connection] interface-name=<iface>` field inside the file. Filenames can be arbitrary (e.g., "Wired connection 1.nmconnection").
- **DO NOT install any packages** — all tools are already in the container from Story 18.1.
- **DO NOT add IPv6 support** — deferred to future (per Epic 18 scope).
- **DO NOT convert DHCP-to-static** — deferred to future. The handler assumes the guest already has a static IP configured and only changes the address/prefix/gateway values.
- **DO NOT handle `systemd-networkd` configs** — RHEL uses NetworkManager exclusively. `systemd-networkd` is used by other distros (Fedora CoreOS, Amazon Linux) which are out of scope.
- **DO NOT create unit tests** — shell script testing is handled in Story 18.7 via disk image fixtures.
- **DO NOT use `#!/bin/bash`** — use `#!/usr/bin/env bash` for consistency with Story 18.2.

### Verification Commands

After replacing the stub, build the image and test:

```bash
# Build updated image
podman build -t soteria-ip-rewrite:dev build/ip-rewrite/

# Verify the handler is no longer a stub
podman run --rm --entrypoint /bin/bash soteria-ip-rewrite:dev \
  -c "wc -l /scripts/rhel-handler.sh"
# Expected: significantly more than the ~5-line stub

# Verify guestfish aug-* commands are available
podman run --rm --cap-add SYS_ADMIN --entrypoint /bin/bash soteria-ip-rewrite:dev \
  -c "echo 'aug-init / 0' | guestfish --ro -a /dev/null"
# Expected: no error (aug-init succeeds on empty disk)
```

**Full AC validation requires disk image fixtures (Story 18.7).** The handler modifies real guest filesystems, which requires test disk images with pre-configured RHEL networking. The handler script should be reviewed for correctness via code inspection and then validated end-to-end in Story 18.7.

### References

- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Story 18.2 spec: `_bmad-output/implementation-artifacts/18-2-ip-rewrite-entrypoint-script-os-detection-dispatch.md`]
- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18", Story 18.3 at ~line 4390]
- [Augeas `Shellvars.lns` documentation: https://augeas.net/docs/references/lenses/files/shellvars-aug.html]
- [Augeas `NetworkManager.lns` documentation: https://augeas.net/docs/references/lenses/files/networkmanager-aug.html]
- [Augeas double-quote bug fix (PR #723): https://github.com/hercules-team/augeas/pull/723]
- [NM keyfile format reference (address, dns, method): https://networkmanager.dev/docs/api/latest/nm-settings-keyfile.html]
- [RHEL 9 NM keyfile chapter: https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/configuring_and_managing_networking/assembly_networkmanager-connection-profiles-in-keyfile-format_configuring-and-managing-networking]
- [guestfish Augeas scripting (RHEL 7 docs): https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/7/html/virtualization_deployment_and_administration_guide/sect-Guest_virtual_machine_disk_access_with_offline_tools-The_guestfish_shell]
- [guestfish man page: https://libguestfs.org/guestfish.1.html]
- [Red Hat blog — guestfish static IP example: https://www.redhat.com/en/blog/customize-vm-cloud-images-guestfish]

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
