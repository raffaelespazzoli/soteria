# Story 18.4: Windows IP Rewrite Handler — hivex-Based

Status: review

## Story

As a developer,
I want a handler script that uses guestfish and hivex to rewrite static IP configuration in the Windows registry hive offline,
so that Windows VMs boot with the correct network configuration after relocation to a new subnet.

## Acceptance Criteria

### AC1: Locate and open the SYSTEM registry hive
Given a Windows disk image mounted via guestfish
When the handler starts
Then it locates the SYSTEM hive at `<systemroot>/system32/config/system` (path discovered via guestfish `inspect-get-windows-systemroot`)
And opens it for modification

### AC2: Identify the active ControlSet
Given the SYSTEM hive is accessible
When the handler reads the active ControlSet
Then it determines the correct ControlSet (e.g., `ControlSet001`) via guestfish `inspect-get-windows-current-control-set`
And navigates to `<ControlSet>\Services\Tcpip\Parameters\Interfaces\`

### AC3: Match network adapter by existing IP or description
Given the `Interfaces\` key contains multiple `{GUID}` subkeys (one per adapter)
When the handler searches for the target interface
Then it matches by: (a) checking the `IPAddress` value for an existing IP in the same subnet, or (b) iterating adapters and selecting the one with `EnableDHCP=0` (static IP configured)
And reports an error if no matching adapter is found

### AC4: Write IP configuration values
Given a matching adapter GUID is found
When the handler writes the new IP configuration
Then it sets `EnableDHCP` = `REG_DWORD 0`
And `IPAddress` = `REG_MULTI_SZ` containing the new IP address
And `SubnetMask` = `REG_MULTI_SZ` containing the subnet mask derived from prefix length (e.g., prefix 24 → `255.255.255.0`)
And `DefaultGateway` = `REG_MULTI_SZ` containing the gateway address
And if DNS is provided, `NameServer` = `REG_SZ` containing comma-separated DNS addresses

### AC5: REG_MULTI_SZ encoding
Given the handler writes `IPAddress`, `SubnetMask`, and `DefaultGateway` values
When encoding as `REG_MULTI_SZ`
Then values are encoded as UTF-16LE with double null termination (per Windows registry specification)
And hivexsh handles the encoding correctly via its type syntax

### AC6: Commit and close the hive
Given all values are written
When the handler commits the hive changes
Then the SYSTEM hive file on disk is updated atomically
And no partial writes or corruption occur

### AC7: Multi-NIC support
Given a Windows VM with two network adapters
When IP configuration is provided for both interfaces
Then both adapters are located and updated independently in the same hive session

### AC8: All supported Windows versions
Given a Windows Server 2016, 2019, 2022, or 2025 disk image (or Windows 10/11)
When the handler runs
Then the registry path structure is consistent across all versions (`Services\Tcpip\Parameters\Interfaces\{GUID}` is stable)
And the handler works identically on all supported versions

## Tasks / Subtasks

- [x] Task 1: Create `build/ip-rewrite/scripts/windows-handler.sh` (AC: 1–8)
  - [x] 1.1: Add shebang (`#!/usr/bin/env bash`), `set -euo pipefail`, and reuse the logging helper functions from `entrypoint.sh` (available via `source`)
  - [x] 1.2: Validate that all required `REWRITE_*` environment variables are set (fail-fast with descriptive error)
  - [x] 1.3: Implement `prefix_to_mask()` function for CIDR-to-dotted-decimal subnet mask conversion
- [x] Task 2: Implement Windows metadata discovery via guestfish (AC: 1, 2)
  - [x] 2.1: Open a remote guestfish session (`guestfish --listen`, then `--remote` commands) or use `guestfish -a "$REWRITE_DISK" -i` with command piping to get `inspect-os` root device
  - [x] 2.2: Call `inspect-get-windows-systemroot "$ROOT_DEV"` to get the system root path (e.g., `/Windows`)
  - [x] 2.3: Call `inspect-get-windows-current-control-set "$ROOT_DEV"` to get the active ControlSet (e.g., `ControlSet001`)
  - [x] 2.4: Construct the SYSTEM hive path: `${SYSTEMROOT}/system32/config/system` (case-insensitive NTFS)
  - [x] 2.5: Download the SYSTEM hive to a temp file via guestfish `download` command
- [x] Task 3: Implement adapter GUID discovery and matching (AC: 3, 7)
  - [x] 3.1: Use `hivexsh` (read-only) or `hivexget` to enumerate `{GUID}` subkeys under `<ControlSet>\Services\Tcpip\Parameters\Interfaces\`
  - [x] 3.2: For each `{GUID}`, read `EnableDHCP` (REG_DWORD) and `IPAddress` (REG_MULTI_SZ) values
  - [x] 3.3: Match by static IP configuration: `EnableDHCP=0` and `IPAddress` containing a non-empty, non-`0.0.0.0` value
  - [x] 3.4: For single-NIC VMs: use the first adapter matching the static IP criteria
  - [x] 3.5: For multi-NIC VMs: match by existing IP subnet overlap with the annotation's interface, or by adapter index order
  - [x] 3.6: Exit with error if no matching adapter is found for any requested interface
- [x] Task 4: Implement IP/subnet/gateway/DNS value writing via hivexregedit --merge (AC: 4, 5, 6)
  - [x] 4.1: Generate .reg file with registry key path for matched adapter's `{GUID}`
  - [x] 4.2: Write `dword:00000000` for `EnableDHCP`
  - [x] 4.3: Write `hex(7):<utf16le-hex>` for `IPAddress` (REG_MULTI_SZ)
  - [x] 4.4: Write `hex(7):<utf16le-hex>` for `SubnetMask` (REG_MULTI_SZ)
  - [x] 4.5: Write `hex(7):<utf16le-hex>` for `DefaultGateway` (REG_MULTI_SZ)
  - [x] 4.6: If `REWRITE_DNS` is set, write `NameServer` as REG_SZ per adapter
  - [x] 4.7: Merge via `hivexregedit --merge` (non-destructive, preserves other values)
  - [x] 4.8: Implement helper function `string_to_utf16le_multisz()` to convert an ASCII string to hex-encoded UTF-16LE with proper null termination for REG_MULTI_SZ
- [x] Task 5: Implement hive upload and cleanup (AC: 6)
  - [x] 5.1: Upload the modified SYSTEM hive back to the guest disk via guestfish `upload` command
  - [x] 5.2: Verify upload success
  - [x] 5.3: Clean up temp files (trap-based cleanup on EXIT)
  - [x] 5.4: Log success summary with adapter GUID and new IP configuration
- [x] Task 6: Implement prefix-to-subnet-mask conversion (AC: 4)
  - [x] 6.1: Pure bash function: convert CIDR prefix (0–32) to dotted-decimal (e.g., 24 → `255.255.255.0`)
  - [x] 6.2: Handle all valid prefix lengths (0 through 32)
  - [x] 6.3: Validate prefix is a number in range; exit with error if not

## Dev Notes

### Story Intelligence Chain

**Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9**

What 18.1 accomplished:
- Created `build/ip-rewrite/Containerfile` based on `registry.access.redhat.com/ubi9/ubi`
- Installed `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport` via dnf
- Set `LIBGUESTFS_BACKEND=direct` environment variable (required for guestfish in containers)
- Created placeholder `/scripts/` directory
- The `hivex` package provides `hivexsh`, `hivexregedit`, `hivexget`, and `hivexml` CLIs — all available in the container for this story to use
- The hivex *library* is built into libguestfs (since v1.19.35) for in-guestfish `hivex-*` commands
- `libguestfs-winsupport` provides NTFS support in the guestfish appliance — required for mounting Windows disks

What 18.1 deferred to this story:
- Windows handler script creation

Patterns established by 18.1:
- **Package source**: ONLY `ubi-9-appstream-rpms` and `ubi-9-baseos-rpms` — NO EPEL, no third-party repos
- **DO NOT install `python3-hivex`** — Python bindings are not needed. Use guestfish built-in `hivex-*` commands and standalone `hivexsh` CLI
- **Runtime requirement**: `SYS_ADMIN` capability needed for guestfish appliance

**Predecessor: Story 18.2 — IP Rewrite Entrypoint Script: OS Detection & Dispatch**

What 18.2 accomplished:
- Created `build/ip-rewrite/scripts/entrypoint.sh` with annotation parsing, boot disk scanning, OS detection, and dispatch
- Created placeholder stubs for `rhel-handler.sh` and `windows-handler.sh` in `build/ip-rewrite/scripts/`
- Updated `build/ip-rewrite/Containerfile` with `COPY scripts/ /scripts/`, `RUN chmod +x /scripts/*.sh`, and `ENTRYPOINT ["/scripts/entrypoint.sh"]`
- Installed `libxml2` for `xmllint` (XML parsing of virt-inspector output)
- Established the **handler interface contract** (see below)
- Established the **logging convention** (`log_info`, `log_warn`, `log_error`)
- The handler is invoked via `source /scripts/windows-handler.sh` (NOT `exec`), so all exported variables and functions from the entrypoint are available

What 18.2 deferred to this story:
- Replacing the `windows-handler.sh` placeholder stub with the full implementation
- All Windows registry manipulation logic

**Handler Interface Contract (from 18.2)**

The entrypoint exports these variables before sourcing the handler:

```bash
# Boot disk and OS metadata
export REWRITE_DISK="/disks/rootdisk/disk.img"     # Path to boot disk
export REWRITE_OS_NAME="windows"                    # OS family
export REWRITE_OS_DISTRO="windows"                  # Distribution
export REWRITE_OS_MAJOR="10"                         # Major version (all modern Windows = 10)
export REWRITE_OS_MINOR="0"                          # Minor version
export REWRITE_OS_PRODUCT="Windows Server 2022 Standard"

# Optional DNS
export REWRITE_DNS="10.0.2.10,10.0.2.11"            # Comma-separated, from SOTERIA_DNS

# Per-interface config (indexed)
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

The entrypoint's `log_info`, `log_warn`, `log_error` functions are available since the handler is `source`d.

**Windows-Specific Note:** The `REWRITE_OS_MAJOR` is `10` and `REWRITE_OS_MINOR` is `0` for ALL modern Windows versions (Server 2016/2019/2022/2025, Windows 10/11). Use `REWRITE_OS_PRODUCT` for logging but NOT for behavioral branching — all versions use the same registry path structure.

**Sibling: Story 18.3 — RHEL IP Rewrite Handler: Augeas-Based**

Story 18.3 (currently backlog) is the RHEL equivalent of this story. It uses guestfish with Augeas for structured config file editing. This story should follow parallel patterns:
- Same handler interface (reads `REWRITE_*` environment variables)
- Same error handling conventions (non-zero exit on failure)
- Same logging style (`log_info`, `log_warn`, `log_error`)
- Same idempotency guarantee (re-running with same config produces no errors)

### Critical Technical Details

#### Architecture: Download → Modify → Upload Pattern

The handler uses a three-phase approach to modify the SYSTEM hive:

**Phase 1: Discovery & Download** (guestfish)
- Open the disk with `guestfish -a "$REWRITE_DISK" -i` (auto-inspect and mount)
- Use `inspect-os` to get the root device path
- Use `inspect-get-windows-systemroot "$ROOT_DEV"` to get the system root (e.g., `/Windows`)
- Use `inspect-get-windows-current-control-set "$ROOT_DEV"` to get the active ControlSet (e.g., `ControlSet001`)
- Download the SYSTEM hive: `download "<systemroot>/system32/config/system" /tmp/system.hive`
- Close guestfish

**Phase 2: Hive Modification** (hivexsh)
- Open the hive with `hivexsh -w /tmp/system.hive`
- Navigate to `<ControlSet>\Services\Tcpip\Parameters\Interfaces\`
- Enumerate and match adapter GUIDs
- Write IP configuration values using hivexsh's typed value syntax
- Commit changes

**Phase 3: Upload** (guestfish)
- Reopen the disk with guestfish
- Upload the modified hive: `upload /tmp/system.hive "<systemroot>/system32/config/system"`
- Close guestfish

**Why download/upload?** The guestfish `hivex-node-set-value` command takes raw binary data for the `val` parameter (`BufferIn` type), which requires constructing binary blobs in shell. The standalone `hivexsh` tool has a much friendlier type syntax (`dword:`, `string:`, `hex:type:hexbytes`) that is practical for shell scripting.

#### Windows Registry Path Structure

All supported Windows versions (Server 2016/2019/2022/2025, Windows 10/11) use the same registry path:

```
HKLM\SYSTEM\<ControlSet>\Services\Tcpip\Parameters\Interfaces\{GUID}
```

Each `{GUID}` subkey represents a network adapter. Relevant values under each `{GUID}`:

| Value Name | Type | Description |
|------------|------|-------------|
| `EnableDHCP` | REG_DWORD | `0` = static IP, `1` = DHCP |
| `IPAddress` | REG_MULTI_SZ | List of IP addresses (usually one) |
| `SubnetMask` | REG_MULTI_SZ | List of subnet masks (parallel to IPAddress) |
| `DefaultGateway` | REG_MULTI_SZ | List of gateways (usually one) |
| `NameServer` | REG_SZ | Comma-separated DNS server addresses |
| `DhcpIPAddress` | REG_SZ | DHCP-assigned IP (present when EnableDHCP=1) |

**Key insight:** When `EnableDHCP=0`, the static IP configuration is read from `IPAddress`/`SubnetMask`/`DefaultGateway`. When `EnableDHCP=1`, these fields may contain stale data or `0.0.0.0` — the active config comes from `DhcpIPAddress`/`DhcpSubnetMask`/`DhcpDefaultGateway`.

#### ControlSet Discovery

The `HKLM\SYSTEM\Select\Current` value (REG_DWORD) contains the number of the active ControlSet (typically `1` for `ControlSet001`). However, **guestfish provides a shortcut**: `inspect-get-windows-current-control-set` reads this value and returns the string directly (e.g., `ControlSet001`). Use this instead of manual hive navigation to `Select\Current`.

**Runtime-only alias:** `CurrentControlSet` is a registry symlink created by Windows at boot time. It does NOT exist in the offline hive. Always use the concrete `ControlSet00N` path.

#### hivexsh Value Type Syntax

The `hivexsh` tool supports these type formats for `setval`:

```
dword:0x00000000     REG_DWORD (type 4), hex value
string:sometext      REG_SZ (type 1), UTF-16LE encoded automatically
hex:<type>:<bytes>   Raw hex bytes with explicit type number
```

For REG_MULTI_SZ (type 7), use the `hex:7:` format with manually constructed UTF-16LE bytes:

```
hex:7:31,00,30,00,2e,00,30,00,2e,00,32,00,2e,00,31,00,30,00,30,00,00,00,00,00
```

This encodes `"10.0.2.100"` as UTF-16LE: each ASCII char is followed by `00`, the string is terminated by `00,00` (null terminator), and the multi-string list is terminated by an additional `00,00` (double null).

#### UTF-16LE Hex Encoding Helper

Implement a bash function to convert an ASCII string to UTF-16LE hex for REG_MULTI_SZ:

```bash
# Convert ASCII string to UTF-16LE hex for REG_MULTI_SZ (single value)
# Usage: string_to_utf16le_multisz "10.0.2.100"
# Output: 31,00,30,00,2e,00,30,00,2e,00,32,00,2e,00,31,00,30,00,30,00,00,00,00,00
string_to_utf16le_multisz() {
    local str="$1"
    local hex=""
    for (( i=0; i<${#str}; i++ )); do
        hex+=$(printf '%02x,00,' "'${str:$i:1}")
    done
    # String null terminator + multi-string list terminator
    hex+="00,00,00,00"
    echo "$hex"
}
```

**Important:** This function works ONLY for 7-bit ASCII strings. IP addresses, subnet masks, and DNS addresses are always ASCII, so this is safe. Do NOT use for arbitrary Unicode strings.

#### Prefix-to-Subnet-Mask Conversion

Implement in pure bash (no external tools needed):

```bash
# Convert CIDR prefix length to dotted-decimal subnet mask
# Usage: prefix_to_mask 24
# Output: 255.255.255.0
prefix_to_mask() {
    local prefix=$1
    local mask=$((0xFFFFFFFF << (32 - prefix) & 0xFFFFFFFF))
    printf "%d.%d.%d.%d\n" \
        $(( (mask >> 24) & 0xFF )) \
        $(( (mask >> 16) & 0xFF )) \
        $(( (mask >> 8) & 0xFF )) \
        $(( mask & 0xFF ))
}
```

Common mappings for quick reference:
| Prefix | Mask |
|--------|------|
| 8 | 255.0.0.0 |
| 16 | 255.255.0.0 |
| 24 | 255.255.255.0 |
| 25 | 255.255.255.128 |
| 28 | 255.255.255.240 |
| 32 | 255.255.255.255 |

#### Adapter Matching Strategy

The handler needs to match annotation interface names (e.g., `eth0`, `eth1`) to Windows adapter GUIDs. Unlike Linux where interface names are visible in config files, Windows uses opaque GUIDs.

**Pragmatic matching approach:**

1. **Single-NIC VM (most common case):** If `REWRITE_IFACE_COUNT=1`, find the adapter with `EnableDHCP=0` (static IP). If only one adapter has a static IP, use it regardless of the annotation's interface name.

2. **Multi-NIC VM:** When multiple adapters have static IPs, match by **existing IP subnet**. For each annotation interface, compute the network address from the existing `IPAddress`/`SubnetMask`, and match it against the target network.

3. **Fallback: adapter index order.** If subnet matching fails, match adapters to annotations in the order they appear in the registry (GUID alphabetical order = adapter creation order in most cases).

4. **All adapters are DHCP:** If the target adapter is DHCP-enabled (EnableDHCP=1), the handler should still proceed — set EnableDHCP=0 and write the static IP values. Log a warning that DHCP is being disabled.

**Enumeration with hivexsh:**
```bash
# List adapter GUIDs
hivexsh /tmp/system.hive <<EOF
cd ${CONTROLSET}\Services\Tcpip\Parameters\Interfaces
ls
EOF
# Output: list of {GUID-...} keys, one per line
```

Then for each GUID, read its values:
```bash
hivexsh /tmp/system.hive <<EOF
cd ${CONTROLSET}\Services\Tcpip\Parameters\Interfaces\{GUID}
lsval EnableDHCP
lsval IPAddress
EOF
```

#### Guestfish Session Management

**Option A — Multiple short-lived sessions (simpler, recommended for v1):**

```bash
# Phase 1: Discovery + download (read-only)
GUESTFISH_OUT=$(guestfish --ro -a "$REWRITE_DISK" -i <<'EOF'
inspect-os
EOF
)
ROOT_DEV=$(echo "$GUESTFISH_OUT" | head -1)

SYSTEMROOT=$(guestfish --ro -a "$REWRITE_DISK" -i -- inspect-get-windows-systemroot "$ROOT_DEV")
CONTROLSET=$(guestfish --ro -a "$REWRITE_DISK" -i -- inspect-get-windows-current-control-set "$ROOT_DEV")

guestfish --ro -a "$REWRITE_DISK" -i -- download "${SYSTEMROOT}/system32/config/system" /tmp/system.hive

# Phase 2: hivexsh modification (local, no guestfish)

# Phase 3: Upload (read-write)
guestfish -a "$REWRITE_DISK" -i -- upload /tmp/system.hive "${SYSTEMROOT}/system32/config/system"
```

**Option B — Remote guestfish session (single appliance boot, more efficient):**

```bash
eval "$(guestfish --listen)"
guestfish --remote add-drive "$REWRITE_DISK"
guestfish --remote run
ROOT_DEV=$(guestfish --remote inspect-os)
SYSTEMROOT=$(guestfish --remote inspect-get-windows-systemroot "$ROOT_DEV")
CONTROLSET=$(guestfish --remote inspect-get-windows-current-control-set "$ROOT_DEV")
guestfish --remote mount-local-run  # or just mount
guestfish --remote download "${SYSTEMROOT}/system32/config/system" /tmp/system.hive
# ... hivexsh modification ...
guestfish --remote upload /tmp/system.hive "${SYSTEMROOT}/system32/config/system"
guestfish --remote exit
```

Option B boots the libguestfs appliance once (~5 seconds) instead of 3–4 times. Use Option B if performance matters. Use Option A for simplicity.

#### hivexsh Scripted Modification Example

Complete example of modifying a single adapter:

```bash
hivexsh -w /tmp/system.hive <<EOF
cd ${CONTROLSET}\Services\Tcpip\Parameters\Interfaces\${ADAPTER_GUID}
setval 5
EnableDHCP
dword:00000000
IPAddress
hex:7:${IP_HEX}
SubnetMask
hex:7:${MASK_HEX}
DefaultGateway
hex:7:${GW_HEX}
NameServer
string:${DNS_CSV}
commit
EOF
```

**Without DNS (4 values instead of 5):**

```bash
hivexsh -w /tmp/system.hive <<EOF
cd ${CONTROLSET}\Services\Tcpip\Parameters\Interfaces\${ADAPTER_GUID}
setval 4
EnableDHCP
dword:00000000
IPAddress
hex:7:${IP_HEX}
SubnetMask
hex:7:${MASK_HEX}
DefaultGateway
hex:7:${GW_HEX}
commit
EOF
```

**CRITICAL:** `setval N` replaces ALL values at the current node. This means any existing values not explicitly re-set are deleted. For the Interfaces `{GUID}` key, there are many other values (InterfaceMetric, RegistrationEnabled, etc.). 

**DO NOT use `setval N` to replace all values.** Instead, run multiple `setval 1` calls (or multiple hivexsh sessions) to set individual values. Each `setval 1` sets only the named value, preserving others:

```bash
# Correct approach: set each value individually
hivexsh -w /tmp/system.hive <<EOF
cd ${CONTROLSET}\Services\Tcpip\Parameters\Interfaces\${ADAPTER_GUID}
setval 1
EnableDHCP
dword:00000000
commit
EOF

hivexsh -w /tmp/system.hive <<EOF
cd ${CONTROLSET}\Services\Tcpip\Parameters\Interfaces\${ADAPTER_GUID}
setval 1
IPAddress
hex:7:${IP_HEX}
commit
EOF
# ... repeat for SubnetMask, DefaultGateway, NameServer
```

**WAIT — CORRECTION:** `setval N` replaces ALL `(key, value)` pairs at the current node. So `setval 1` with `IPAddress` would DELETE `EnableDHCP`, `SubnetMask`, and every other value under that key. This is DESTRUCTIVE.

**SAFE APPROACH:** Read all existing values first, then write them all back with the modified ones. OR use the guestfish `hivex-node-set-value` command instead, which sets a SINGLE value without affecting others.

**REVISED STRATEGY — Use guestfish hivex commands for writing:**

Since `hivexsh setval` is destructive (replaces ALL values), use guestfish's `hivex-node-set-value` for the write phase instead. This sets individual values without affecting siblings.

```bash
# Phase 2 (revised): Use guestfish hivex commands for writing
guestfish -a "$REWRITE_DISK" -i <<EOF
hivex-open "${SYSTEMROOT}/system32/config/system" write:true
# Navigate to adapter key
!HIVE_ROOT=\$(hivex-root)
# ... navigate with hivex-node-get-child ...
hivex-node-set-value \$ADAPTER_NODE EnableDHCP 4 /tmp/enabledhcp.bin
hivex-node-set-value \$ADAPTER_NODE IPAddress 7 /tmp/ipaddress.bin
hivex-node-set-value \$ADAPTER_NODE SubnetMask 7 /tmp/subnetmask.bin
hivex-node-set-value \$ADAPTER_NODE DefaultGateway 7 /tmp/defaultgateway.bin
hivex-commit ""
hivex-close
EOF
```

For this approach, pre-create binary value files:
```bash
# REG_DWORD 0 (EnableDHCP = 0, little-endian)
printf '\x00\x00\x00\x00' > /tmp/enabledhcp.bin

# REG_MULTI_SZ "10.0.2.100" (UTF-16LE + double null)
printf '1\x000\x00.\x000\x00.\x002\x00.\x001\x000\x000\x00\x00\x00\x00\x00' > /tmp/ipaddress.bin
```

**HOWEVER**, there's a complication: guestfish's `hivex-node-set-value` requires node handles (integers returned by `hivex-root`, `hivex-node-get-child`). In a heredoc script, capturing these return values and using them in subsequent commands is awkward.

#### FINAL RECOMMENDED APPROACH — Hybrid (hivexsh read + enumerate, guestfish write)

Given the above constraints, the cleanest approach is:

1. **Use guestfish** to download the hive and get Windows metadata
2. **Use hivexsh** (read-only) to enumerate adapters and match GUIDs
3. **Use hivexregedit --merge** to write values (it updates individual values without destroying others)
4. **Use guestfish** to upload the modified hive

The `hivexregedit --merge` command imports a `.reg` file and merges values into an existing hive:

```bash
# Create a .reg file for the changes
cat > /tmp/ip-rewrite.reg <<REGEOF
Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\\SYSTEM\\${CONTROLSET}\\Services\\Tcpip\\Parameters\\Interfaces\\${ADAPTER_GUID}]
"EnableDHCP"=dword:00000000
"IPAddress"=hex(7):${IP_HEX}
"SubnetMask"=hex(7):${MASK_HEX}
"DefaultGateway"=hex(7):${GW_HEX}
"NameServer"="${DNS_CSV}"
REGEOF

# Merge into hive (updates only specified values, preserves everything else)
hivexregedit --merge /tmp/system.hive /tmp/ip-rewrite.reg
```

**This is the safest approach because:**
- `hivexregedit --merge` sets only the specified values, leaving all other values intact
- The `.reg` file format is well-documented and straightforward to generate from shell
- `hex(7):` is the standard .reg file syntax for REG_MULTI_SZ
- No complex guestfish command chaining or node handle management needed
- The hive is downloaded locally, so file I/O is fast

#### .reg File Format for REG_MULTI_SZ

In `.reg` file format, `hex(7):` denotes REG_MULTI_SZ. The value is hex-encoded UTF-16LE bytes:

```reg
"IPAddress"=hex(7):31,00,30,00,2e,00,30,00,2e,00,32,00,2e,00,31,00,30,00,30,00,00,00,00,00
```

Breaking this down for `"10.0.2.100"`:
```
31,00  = '1'  (0x31 in UTF-16LE)
30,00  = '0'
2e,00  = '.'
30,00  = '0'
2e,00  = '.'
32,00  = '2'
2e,00  = '.'
31,00  = '1'
30,00  = '0'
30,00  = '0'
00,00  = string null terminator
00,00  = multi-string list terminator (double null)
```

For long hex values, `.reg` files support line continuation with backslash:
```reg
"IPAddress"=hex(7):31,00,30,00,2e,00,30,00,2e,00,32,00,\
  2e,00,31,00,30,00,30,00,00,00,00,00
```

#### Idempotency

The handler must be idempotent (AC6 from 18.3 applies conceptually — running twice with the same config must succeed). Since `hivexregedit --merge` overwrites values, running it again with the same values produces the same result. Adapter matching must also be idempotent — after the first run changes the IP, the adapter's old IP is gone. The matching logic should handle this:

- After rewrite, the adapter's `IPAddress` contains the NEW IP
- If re-run with the same config, matching by IP subnet should still work (the new IP is in the target subnet)
- If re-run with a DIFFERENT config, the handler matches by the previously-written IP

#### Error Handling

- **No boot disk**: Entrypoint handles this (handler never called)
- **Hive not found**: Exit non-zero with clear error (`SYSTEM hive not found at <path>`)
- **No matching adapter**: Exit non-zero listing all adapter GUIDs found and their configs
- **hivexregedit failure**: Capture stderr, exit non-zero
- **Upload failure**: Exit non-zero, original hive on disk remains unchanged (download/modify/upload is safe — if upload fails, the on-disk hive is the original unmodified version)
- **Temp file cleanup**: Use `trap cleanup EXIT` to ensure `/tmp/system.hive` and `/tmp/ip-rewrite.reg` are removed

### Project Structure Notes

After this story, the `build/ip-rewrite/scripts/` directory contains:

```
build/
└── ip-rewrite/
    ├── Containerfile             ← Created by 18.1, modified by 18.2 (no changes in this story)
    ├── README.md                 ← Created by 18.1 (no changes in this story)
    └── scripts/
        ├── entrypoint.sh         ← Created by 18.2 (no changes in this story)
        ├── rhel-handler.sh       ← Placeholder stub from 18.2 (no changes in this story)
        └── windows-handler.sh    ← REPLACE placeholder stub with full implementation (this story)
```

**Only one file is created/modified in this story:** `build/ip-rewrite/scripts/windows-handler.sh`

**No other files should be touched.** The Containerfile, entrypoint, Makefile, CI workflows — all remain unchanged.

### Anti-Patterns / DO NOT

- **DO NOT install additional packages** — all required tools (`guestfish`, `hivexsh`, `hivexregedit`, `hivexget`, `xmllint`) are already present in the container image from Stories 18.1 and 18.2. No Containerfile changes needed.
- **DO NOT install `python3-hivex`** — the Python bindings are unnecessary. Use `hivexsh` and `hivexregedit` CLIs for shell-scriptable hive modification.
- **DO NOT use `hivexsh setval N` (N > 1) to set multiple values at once** — `setval` REPLACES ALL values at the current node, destroying existing registry values like `InterfaceMetric`, `RegistrationEnabled`, etc. Use `hivexregedit --merge` which updates only specified values.
- **DO NOT modify the entrypoint script (`entrypoint.sh`)** — that is Story 18.2's file. This handler receives all context via exported `REWRITE_*` environment variables.
- **DO NOT modify the Containerfile** — no additional packages or changes needed for this story.
- **DO NOT modify `.github/workflows/ci.yml` or `release.yml`** — CI integration is Story 18.9.
- **DO NOT modify the `Makefile`** — build targets are Story 18.9.
- **DO NOT create unit tests** — testing is Story 18.7 (disk image fixtures with synthetic hives).
- **DO NOT implement RHEL handler logic** — that is Story 18.3's responsibility.
- **DO NOT use `CurrentControlSet` registry path** — it is a runtime-only symlink that does NOT exist in offline hives. Always use the concrete `ControlSet00N` path from `inspect-get-windows-current-control-set`.
- **DO NOT attempt IPv6 support** — deferred per epic scope (IPv4 only).
- **DO NOT handle DHCP-to-static conversion logic beyond setting `EnableDHCP=0`** — complex DHCP migration scenarios are deferred to future work.
- **DO NOT use `#!/bin/bash`** — use `#!/usr/bin/env bash` for portability (matches 18.2 convention).
- **DO NOT use `grep`/`sed`/`awk` to parse binary hive data** — use `hivexsh`, `hivexget`, or `hivexml` for safe hive access.

### Verification Commands

After replacing the placeholder stub, test with:

```bash
# Build updated image (should succeed without changes since only a script file changed)
podman build -t soteria-ip-rewrite:dev build/ip-rewrite/

# Verify script is present and executable
podman run --rm --entrypoint /bin/bash soteria-ip-rewrite:dev \
  -c "ls -la /scripts/windows-handler.sh && head -5 /scripts/windows-handler.sh"

# Verify hivexsh and hivexregedit are available
podman run --rm --entrypoint /bin/bash soteria-ip-rewrite:dev \
  -c "hivexsh --version && hivexregedit --version"

# Verify prefix_to_mask function (if extracted as sourceable)
podman run --rm --entrypoint /bin/bash soteria-ip-rewrite:dev \
  -c "source /scripts/windows-handler.sh --test-only 2>/dev/null; prefix_to_mask 24"
# Expected: 255.255.255.0
```

**Full AC validation requires Windows disk images** mounted as volumes — that testing is deferred to Story 18.7 (disk image fixtures with synthetic SYSTEM hives).

### References

- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Story 18.2 spec: `_bmad-output/implementation-artifacts/18-2-ip-rewrite-entrypoint-script-os-detection-dispatch.md`]
- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18", story at ~line 4456]
- [guestfish hivex examples (DHCP address extraction): https://libguestfs.org/guestfs-examples.3.html]
- [guestfish API reference (hivex-* commands): https://libguestfs.org/guestfs.3.html]
- [hivexsh manual (setval, value type syntax): https://libguestfs.org/hivexsh.1.html]
- [hivex library reference: https://www.libguestfs.org/hivex.3.html]
- [Windows registry value types (REG_MULTI_SZ spec): https://learn.microsoft.com/en-us/windows/win32/sysinfo/registry-value-types]
- [Windows Tcpip\Parameters\Interfaces registry structure: https://superuser.com/questions/1338775]

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

Claude Opus 4.6 (2026-09-06)

### Debug Log References

No debug issues encountered.

### Completion Notes List

- Replaced placeholder `windows-handler.sh` stub with full hivex-based Windows IP rewrite handler (~545 lines)
- **Architecture**: 4-phase download→discover→modify→upload pattern using guestfish + hivexsh + hivexregedit
- **Phase 1**: guestfish `inspect-os`, `inspect-get-windows-systemroot`, `inspect-get-windows-current-control-set` for metadata, then `download` SYSTEM hive
- **Phase 2**: hivexsh read-only to enumerate `{GUID}` subkeys, read `EnableDHCP`/`IPAddress`/`SubnetMask` per adapter, decode UTF-16LE REG_MULTI_SZ values
- **Phase 3**: Generate `.reg` file and merge via `hivexregedit --merge` — non-destructive (preserves all existing registry values not being modified)
- **Phase 4**: guestfish `upload` to write modified hive back to disk
- **Adapter matching**: Single-NIC uses first static-IP adapter; multi-NIC uses subnet matching then alphabetical GUID fallback; DHCP adapters converted to static with warning
- **Encoding helpers**: `prefix_to_mask()` for CIDR→dotted-decimal (0–32), `string_to_utf16le_multisz()` for ASCII→UTF-16LE hex (REG_MULTI_SZ)
- **Error handling**: Fail-fast on missing env vars, missing tools, hive download/upload failure, hivexregedit merge failure, no matching adapter
- **Cleanup**: trap-based EXIT cleanup removes all temp files
- **DNS**: Set per-adapter (all modified interfaces receive NameServer value)
- **Deviation from task descriptions**: Task 4 originally described hivexsh `setval` — implemented as `hivexregedit --merge` instead, per the story's own Dev Notes "FINAL RECOMMENDED APPROACH" which explicitly warns against `setval` (destructive to sibling values)
- Tests deferred to Story 18.7 per story spec ("DO NOT create unit tests")
- All existing Go tests pass with no regressions

### File List

- `build/ip-rewrite/scripts/windows-handler.sh` — **MODIFIED** (replaced placeholder stub with full implementation)
