#!/usr/bin/env bash
#
# Windows IP Rewrite Handler — hivex-Based
#
# Rewrites static IP configuration in the Windows registry SYSTEM hive
# for Windows Server 2016/2019/2022/2025 and Windows 10/11 guest VMs.
#
# Architecture: Download → Modify → Upload
#   Phase 1: Use guestfish to discover Windows metadata and download SYSTEM hive
#   Phase 2: Use hivexsh (read-only) to enumerate and match adapter GUIDs
#   Phase 3: Use hivexregedit --merge to write IP config values (non-destructive)
#   Phase 4: Use guestfish to upload the modified hive back to the disk
#
# Called by entrypoint.sh via 'source'. All configuration is available
# through exported REWRITE_* environment variables:
#   REWRITE_DISK          - Path to the boot disk
#   REWRITE_OS_NAME       - OS family (windows)
#   REWRITE_OS_DISTRO     - Distribution (windows)
#   REWRITE_OS_MAJOR      - Major version (10 for all modern Windows)
#   REWRITE_OS_MINOR      - Minor version (0)
#   REWRITE_OS_PRODUCT    - Full product name
#   REWRITE_DNS           - Comma-separated DNS servers (optional)
#   REWRITE_IFACE_COUNT   - Number of interfaces to configure
#   REWRITE_IFACE_<N>     - Interface name (e.g., eth0)
#   REWRITE_IP_<N>        - IP address
#   REWRITE_PREFIX_<N>    - Prefix length (e.g., 24)
#   REWRITE_GATEWAY_<N>   - Gateway address
#
# Logging: log_info, log_warn, log_error from entrypoint.sh are available.
# Exit convention: use `return` (not `exit`) since this is sourced.

set -euo pipefail

# ---------------------------------------------------------------------------
# Temp file tracking and cleanup (save/restore parent trap)
# ---------------------------------------------------------------------------
WIN_HANDLER_TMPDIR=$(mktemp -d /tmp/win-handler-XXXXXX)

_WIN_PREV_TRAP=$(trap -p EXIT 2>/dev/null || true)

win_handler_cleanup() {
    rm -rf "${WIN_HANDLER_TMPDIR}" 2>/dev/null || true
}
trap win_handler_cleanup EXIT

HIVE_LOCAL="${WIN_HANDLER_TMPDIR}/system.hive"
HIVE_BACKUP="${WIN_HANDLER_TMPDIR}/system.hive.orig"
REG_FILE="${WIN_HANDLER_TMPDIR}/ip-rewrite.reg"

# ---------------------------------------------------------------------------
# Validate required environment variables
# ---------------------------------------------------------------------------
log_info "Windows handler starting for ${REWRITE_OS_PRODUCT}"

for required_var in REWRITE_DISK REWRITE_OS_NAME REWRITE_IFACE_COUNT; do
    if [[ -z "${!required_var:-}" ]]; then
        log_error "Required environment variable not set: ${required_var}"
        return 1
    fi
done

if [[ "${REWRITE_IFACE_COUNT}" -lt 1 ]]; then
    log_error "REWRITE_IFACE_COUNT must be >= 1, got: ${REWRITE_IFACE_COUNT}"
    return 1
fi

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    for suffix in IFACE IP PREFIX GATEWAY; do
        varname="REWRITE_${suffix}_${i}"
        if [[ -z "${!varname:-}" ]]; then
            log_error "Required environment variable not set: ${varname}"
            return 1
        fi
    done
done

# Verify required tools are available
for cmd in guestfish hivexsh hivexregedit; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        log_error "Required command not found: ${cmd}"
        return 1
    fi
done

# ---------------------------------------------------------------------------
# Utility: Convert CIDR prefix length to dotted-decimal subnet mask
# ---------------------------------------------------------------------------
prefix_to_mask() {
    local prefix=$1

    if ! [[ "${prefix}" =~ ^[0-9]+$ ]] || (( prefix < 0 || prefix > 32 )); then
        log_error "Invalid prefix length: '${prefix}' (must be integer 0-32)"
        return 1
    fi

    local mask=$(( 0xFFFFFFFF << (32 - prefix) & 0xFFFFFFFF ))
    printf "%d.%d.%d.%d\n" \
        $(( (mask >> 24) & 0xFF )) \
        $(( (mask >> 16) & 0xFF )) \
        $(( (mask >> 8) & 0xFF )) \
        $(( mask & 0xFF ))
}

# ---------------------------------------------------------------------------
# Utility: Convert ASCII string to hex-encoded UTF-16LE for REG_MULTI_SZ
# ---------------------------------------------------------------------------
string_to_utf16le_multisz() {
    local str="$1"
    local hex=""
    local i

    for (( i = 0; i < ${#str}; i++ )); do
        hex+=$(printf '%02x,00,' "'${str:$i:1}")
    done
    # String null terminator + multi-string list terminator (double null)
    hex+="00,00,00,00"
    echo "${hex}"
}

# ---------------------------------------------------------------------------
# Utility: Compute network address from IP and subnet mask
# Uses 10# prefix to force decimal parsing (prevents octal on 08/09)
# ---------------------------------------------------------------------------
ip_to_network() {
    local ip="$1" mask="$2"
    local a1 a2 a3 a4 m1 m2 m3 m4
    IFS='.' read -r a1 a2 a3 a4 <<< "${ip}"
    IFS='.' read -r m1 m2 m3 m4 <<< "${mask}"
    printf "%d.%d.%d.%d" \
        $(( 10#${a1} & 10#${m1} )) \
        $(( 10#${a2} & 10#${m2} )) \
        $(( 10#${a3} & 10#${m3} )) \
        $(( 10#${a4} & 10#${m4} ))
}

# ---------------------------------------------------------------------------
# Utility: Escape a string for .reg file REG_SZ value (double any backslashes
# and escape embedded double-quotes)
# ---------------------------------------------------------------------------
reg_escape_sz() {
    local s="$1"
    s="${s//\\/\\\\}"
    s="${s//\"/\\\"}"
    printf '%s' "$s"
}

# =========================================================================
# Phase 1: Discovery & Download (guestfish)
# =========================================================================
log_info "Phase 1: Discovering Windows metadata and downloading SYSTEM hive"

# Get root device
ROOT_DEV=$(guestfish --ro -a "${REWRITE_DISK}" -i -- inspect-os 2>/dev/null | head -1) || {
    log_error "Failed to inspect OS on disk: ${REWRITE_DISK}"
    return 1
}

if [[ -z "${ROOT_DEV}" ]]; then
    log_error "No OS root device found on disk: ${REWRITE_DISK}"
    return 1
fi

log_info "Root device: ${ROOT_DEV}"

# Get Windows system root path (e.g., /Windows)
SYSTEMROOT=$(guestfish --ro -a "${REWRITE_DISK}" -i -- \
    inspect-get-windows-systemroot "${ROOT_DEV}" 2>/dev/null) || {
    log_error "Failed to get Windows system root from ${ROOT_DEV}"
    return 1
}

# Trim whitespace/newlines
SYSTEMROOT=$(echo "${SYSTEMROOT}" | tr -d '[:space:]')

if [[ -z "${SYSTEMROOT}" ]]; then
    log_error "Windows system root path is empty"
    return 1
fi

log_info "Windows system root: ${SYSTEMROOT}"

# Get active ControlSet (e.g., ControlSet001)
CONTROLSET=$(guestfish --ro -a "${REWRITE_DISK}" -i -- \
    inspect-get-windows-current-control-set "${ROOT_DEV}" 2>/dev/null) || {
    log_error "Failed to get Windows current control set from ${ROOT_DEV}"
    return 1
}

CONTROLSET=$(echo "${CONTROLSET}" | tr -d '[:space:]')

if [[ -z "${CONTROLSET}" ]]; then
    log_error "Windows current control set is empty"
    return 1
fi

log_info "Active control set: ${CONTROLSET}"

# Construct hive path (case-insensitive NTFS handles this)
HIVE_GUEST_PATH="${SYSTEMROOT}/system32/config/system"

# Download SYSTEM hive to local temp file
log_info "Downloading SYSTEM hive: ${HIVE_GUEST_PATH}"

GFISH_STDERR="${WIN_HANDLER_TMPDIR}/gfish-download.stderr"
if ! guestfish --ro -a "${REWRITE_DISK}" -i -- \
    download "${HIVE_GUEST_PATH}" "${HIVE_LOCAL}" 2>"${GFISH_STDERR}"; then
    log_error "Failed to download SYSTEM hive from ${HIVE_GUEST_PATH}"
    if [[ -s "${GFISH_STDERR}" ]]; then
        log_error "guestfish stderr: $(cat "${GFISH_STDERR}")"
    fi
    return 1
fi

if [[ ! -s "${HIVE_LOCAL}" ]]; then
    log_error "Downloaded SYSTEM hive is empty"
    return 1
fi

HIVE_SIZE=$(stat -c %s "${HIVE_LOCAL}" 2>/dev/null || stat -f %z "${HIVE_LOCAL}" 2>/dev/null)
log_info "SYSTEM hive downloaded (${HIVE_SIZE} bytes)"

# Check for Fast Startup / hiberfil.sys that would ignore offline edits
HIBERFIL_CHECK=$(guestfish --ro -a "${REWRITE_DISK}" -i -- \
    is-file "${SYSTEMROOT}/hiberfil.sys" 2>/dev/null || echo "false")
if [[ "${HIBERFIL_CHECK}" == "true" ]]; then
    log_warn "hiberfil.sys detected — Windows Fast Startup may ignore offline registry changes"
    log_warn "If the VM boots with old IP settings, disable Fast Startup and try again"
fi

# =========================================================================
# Phase 2: Adapter Discovery & Matching (hivexsh read-only)
# =========================================================================
log_info "Phase 2: Discovering and matching network adapters"

INTERFACES_PATH="${CONTROLSET}\\Services\\Tcpip\\Parameters\\Interfaces"

# Enumerate adapter GUIDs
HIVEX_STDERR="${WIN_HANDLER_TMPDIR}/hivexsh-enum.stderr"
ADAPTER_GUIDS=$(hivexsh "${HIVE_LOCAL}" 2>"${HIVEX_STDERR}" <<HIVEOF
cd \\${INTERFACES_PATH}
ls
HIVEOF
) || {
    log_error "Failed to enumerate adapter GUIDs at ${INTERFACES_PATH}"
    if [[ -s "${HIVEX_STDERR}" ]]; then
        log_error "hivexsh stderr: $(cat "${HIVEX_STDERR}")"
    fi
    return 1
}

if [[ -z "${ADAPTER_GUIDS}" ]]; then
    log_error "No adapter GUIDs found under ${INTERFACES_PATH}"
    return 1
fi

# Filter to only {GUID} entries (curly-brace wrapped)
GUID_LIST=()
while IFS= read -r line; do
    line=$(echo "${line}" | tr -d '[:space:]')
    if [[ "${line}" == "{"*"}" ]]; then
        GUID_LIST+=("${line}")
    fi
done <<< "${ADAPTER_GUIDS}"

if [[ ${#GUID_LIST[@]} -eq 0 ]]; then
    log_error "No network adapter GUIDs found (expected {GUID} entries)"
    log_error "Raw adapter listing: ${ADAPTER_GUIDS}"
    return 1
fi

log_info "Found ${#GUID_LIST[@]} network adapter(s)"

# Read adapter properties for matching.
# Named `lsval <key>` in hivexsh prints the decoded value as plain text:
#   DWORD  → decimal number (e.g. "0" or "1")
#   MULTI_SZ → one string per line
#   SZ → the string
declare -A ADAPTER_DHCP=()      # GUID -> EnableDHCP value (0 or 1)
declare -A ADAPTER_IPADDR=()    # GUID -> effective IP for matching
declare -A ADAPTER_SUBNET=()    # GUID -> effective subnet for matching

for guid in "${GUID_LIST[@]}"; do
    log_info "  Reading adapter ${guid}"

    ADAPTER_PATH="${INTERFACES_PATH}\\${guid}"

    # Read EnableDHCP (REG_DWORD) — named lsval returns plain decimal
    ENABLE_DHCP=""
    DHCP_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval EnableDHCP
HIVEOF
    )
    if [[ -n "${DHCP_OUTPUT}" ]]; then
        ENABLE_DHCP=$(echo "${DHCP_OUTPUT}" | tr -d '[:space:]')
    fi
    ADAPTER_DHCP["${guid}"]="${ENABLE_DHCP:-}"

    if [[ "${ENABLE_DHCP}" == "0" ]]; then
        # Static IP — read IPAddress and SubnetMask (REG_MULTI_SZ plain text)
        IP_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval IPAddress
HIVEOF
        )
        IP_ADDR=""
        if [[ -n "${IP_OUTPUT}" ]]; then
            IP_ADDR=$(echo "${IP_OUTPUT}" | head -1 | tr -d '[:space:]')
        fi
        ADAPTER_IPADDR["${guid}"]="${IP_ADDR}"

        MASK_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval SubnetMask
HIVEOF
        )
        SUBNET_MASK=""
        if [[ -n "${MASK_OUTPUT}" ]]; then
            SUBNET_MASK=$(echo "${MASK_OUTPUT}" | head -1 | tr -d '[:space:]')
        fi
        ADAPTER_SUBNET["${guid}"]="${SUBNET_MASK}"
    else
        # DHCP adapter — read DhcpIPAddress/DhcpSubnetMask for subnet matching
        DHCP_IP_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval DhcpIPAddress
HIVEOF
        )
        DHCP_IP=""
        if [[ -n "${DHCP_IP_OUTPUT}" ]]; then
            DHCP_IP=$(echo "${DHCP_IP_OUTPUT}" | head -1 | tr -d '[:space:]')
        fi
        ADAPTER_IPADDR["${guid}"]="${DHCP_IP}"

        DHCP_MASK_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval DhcpSubnetMask
HIVEOF
        )
        DHCP_MASK=""
        if [[ -n "${DHCP_MASK_OUTPUT}" ]]; then
            DHCP_MASK=$(echo "${DHCP_MASK_OUTPUT}" | head -1 | tr -d '[:space:]')
        fi
        ADAPTER_SUBNET["${guid}"]="${DHCP_MASK}"
    fi

    log_info "    EnableDHCP=${ADAPTER_DHCP["${guid}"]:-N/A} IP=${ADAPTER_IPADDR["${guid}"]:-N/A} Mask=${ADAPTER_SUBNET["${guid}"]:-N/A}"
done

# ---------------------------------------------------------------------------
# Adapter matching: map annotation interfaces to adapter GUIDs
# ---------------------------------------------------------------------------

# Collect static-IP adapters (EnableDHCP=0 with non-empty, non-0.0.0.0 IP)
STATIC_GUIDS=()
ALL_GUIDS_SORTED=()

# Sort GUIDs alphabetically for deterministic ordering
while IFS= read -r g; do
    ALL_GUIDS_SORTED+=("${g}")
done < <(printf '%s\n' "${GUID_LIST[@]}" | sort)

for guid in "${ALL_GUIDS_SORTED[@]}"; do
    dhcp="${ADAPTER_DHCP["${guid}"]:-}"
    ip="${ADAPTER_IPADDR["${guid}"]:-}"
    if [[ "${dhcp}" == "0" && -n "${ip}" && "${ip}" != "0.0.0.0" ]]; then
        STATIC_GUIDS+=("${guid}")
    fi
done

log_info "Static-IP adapters: ${#STATIC_GUIDS[@]}, Total adapters: ${#ALL_GUIDS_SORTED[@]}"

# Track assigned GUIDs across all interface iterations
declare -A ASSIGNED_GUIDS=()

# Map each annotation interface to a GUID
declare -A IFACE_TO_GUID=()

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    iface_var="REWRITE_IFACE_${i}"
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"
    iface="${!iface_var}"
    target_ip="${!ip_var}"
    target_prefix="${!prefix_var}"
    target_gw="${!gw_var}"
    target_mask=$(prefix_to_mask "${target_prefix}")
    target_network=$(ip_to_network "${target_ip}" "${target_mask}")

    log_info "Matching interface ${i} (${iface}): target=${target_ip}/${target_prefix} gw=${target_gw} network=${target_network}"

    matched_guid=""

    if [[ "${REWRITE_IFACE_COUNT}" -eq 1 ]]; then
        # Single-NIC: use the first static-IP adapter, or first DHCP adapter if none static
        if [[ ${#STATIC_GUIDS[@]} -ge 1 ]]; then
            matched_guid="${STATIC_GUIDS[0]}"
            log_info "  Single-NIC match: using first static-IP adapter ${matched_guid}"
        elif [[ ${#ALL_GUIDS_SORTED[@]} -ge 1 ]]; then
            matched_guid="${ALL_GUIDS_SORTED[0]}"
            log_warn "  No static-IP adapter found — selecting DHCP adapter ${matched_guid} (will convert to static)"
        fi
    else
        # Multi-NIC: try subnet matching first (skip already-assigned GUIDs)
        for guid in "${ALL_GUIDS_SORTED[@]}"; do
            if [[ -n "${ASSIGNED_GUIDS["${guid}"]:-}" ]]; then
                continue
            fi

            existing_ip="${ADAPTER_IPADDR["${guid}"]:-}"
            existing_mask="${ADAPTER_SUBNET["${guid}"]:-}"

            if [[ -z "${existing_ip}" || "${existing_ip}" == "0.0.0.0" || -z "${existing_mask}" ]]; then
                continue
            fi

            existing_network=$(ip_to_network "${existing_ip}" "${existing_mask}")

            if [[ "${existing_network}" == "${target_network}" ]]; then
                matched_guid="${guid}"
                log_info "  Subnet match: adapter ${guid} (existing ${existing_ip}/${existing_mask}, network=${existing_network})"
                break
            fi
        done

        # Fallback: adapter index order (skip already-assigned GUIDs)
        if [[ -z "${matched_guid}" ]]; then
            log_info "  No subnet match — falling back to adapter index order"
            for guid in "${ALL_GUIDS_SORTED[@]}"; do
                if [[ -n "${ASSIGNED_GUIDS["${guid}"]:-}" ]]; then
                    continue
                fi
                matched_guid="${guid}"
                dhcp="${ADAPTER_DHCP["${guid}"]:-}"
                if [[ "${dhcp}" != "0" ]]; then
                    log_warn "  Fallback selected DHCP adapter ${guid} (will convert to static)"
                else
                    log_info "  Fallback selected adapter ${guid}"
                fi
                break
            done
        fi
    fi

    if [[ -z "${matched_guid}" ]]; then
        log_error "No matching adapter found for interface ${i} (${iface})"
        log_error "Available adapters:"
        for guid in "${ALL_GUIDS_SORTED[@]}"; do
            log_error "  ${guid}: EnableDHCP=${ADAPTER_DHCP["${guid}"]:-N/A} IP=${ADAPTER_IPADDR["${guid}"]:-N/A}"
        done
        return 1
    fi

    IFACE_TO_GUID["${i}"]="${matched_guid}"
    ASSIGNED_GUIDS["${matched_guid}"]=1
    log_info "  Matched: interface ${i} (${iface}) → adapter ${matched_guid}"
done

# =========================================================================
# Phase 3: Write IP configuration via hivexregedit --merge
# =========================================================================
log_info "Phase 3: Writing IP configuration to SYSTEM hive"

# Backup hive before modification (safety net for upload failure)
cp -a "${HIVE_LOCAL}" "${HIVE_BACKUP}"
log_info "Hive backed up to ${HIVE_BACKUP}"

# Build .reg file header
cat > "${REG_FILE}" <<'REGHEADER'
Windows Registry Editor Version 5.00

REGHEADER

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"
    iface_var="REWRITE_IFACE_${i}"

    target_ip="${!ip_var}"
    target_prefix="${!prefix_var}"
    target_gw="${!gw_var}"
    target_iface="${!iface_var}"
    adapter_guid="${IFACE_TO_GUID["${i}"]}"

    target_mask=$(prefix_to_mask "${target_prefix}")
    ip_hex=$(string_to_utf16le_multisz "${target_ip}")
    mask_hex=$(string_to_utf16le_multisz "${target_mask}")
    gw_hex=$(string_to_utf16le_multisz "${target_gw}")

    log_info "Writing config for ${target_iface} → ${adapter_guid}: ip=${target_ip} mask=${target_mask} gw=${target_gw}"

    # .reg key path: HKEY_LOCAL_MACHINE\SYSTEM is the hive root prefix
    # hivexregedit --prefix strips this so paths resolve inside the hive
    REG_KEY_PATH="HKEY_LOCAL_MACHINE\\SYSTEM\\${CONTROLSET}\\Services\\Tcpip\\Parameters\\Interfaces\\${adapter_guid}"

    cat >> "${REG_FILE}" <<REGBLOCK
[${REG_KEY_PATH}]
"EnableDHCP"=dword:00000000
"IPAddress"=hex(7):${ip_hex}
"SubnetMask"=hex(7):${mask_hex}
"DefaultGateway"=hex(7):${gw_hex}
REGBLOCK

    # Add DNS if provided (per-adapter — Windows uses metric to select)
    if [[ -n "${REWRITE_DNS:-}" ]]; then
        dns_escaped=$(reg_escape_sz "${REWRITE_DNS}")
        log_info "Writing DNS servers on ${target_iface}: ${REWRITE_DNS}"
        cat >> "${REG_FILE}" <<REGDNS
"NameServer"="${dns_escaped}"
REGDNS
    fi

    # Blank line between registry key blocks
    echo "" >> "${REG_FILE}"
done

# Merge .reg file into the SYSTEM hive
# --prefix strips the HKEY_LOCAL_MACHINE\SYSTEM prefix so keys resolve at hive root
log_info "Merging registry changes into SYSTEM hive"

MERGE_STDERR="${WIN_HANDLER_TMPDIR}/hivexregedit-merge.stderr"
if ! hivexregedit --merge --prefix 'HKEY_LOCAL_MACHINE\SYSTEM' \
    "${HIVE_LOCAL}" "${REG_FILE}" 2>"${MERGE_STDERR}"; then
    log_error "hivexregedit --merge failed"
    if [[ -s "${MERGE_STDERR}" ]]; then
        log_error "hivexregedit stderr: $(cat "${MERGE_STDERR}")"
    fi
    log_info "Restoring original hive from backup"
    cp -a "${HIVE_BACKUP}" "${HIVE_LOCAL}"
    return 1
fi

log_info "Registry changes merged successfully"

# =========================================================================
# Phase 4: Upload modified hive and clean transaction logs (guestfish)
# =========================================================================
log_info "Phase 4: Uploading modified SYSTEM hive to disk"

UPLOAD_STDERR="${WIN_HANDLER_TMPDIR}/gfish-upload.stderr"
if ! guestfish -a "${REWRITE_DISK}" -i -- \
    upload "${HIVE_LOCAL}" "${HIVE_GUEST_PATH}" 2>"${UPLOAD_STDERR}"; then
    log_error "Failed to upload modified SYSTEM hive to ${HIVE_GUEST_PATH}"
    if [[ -s "${UPLOAD_STDERR}" ]]; then
        log_error "guestfish stderr: $(cat "${UPLOAD_STDERR}")"
    fi
    # Attempt to restore original hive to prevent bricked VM
    log_warn "Attempting to restore original SYSTEM hive from backup"
    if guestfish -a "${REWRITE_DISK}" -i -- \
        upload "${HIVE_BACKUP}" "${HIVE_GUEST_PATH}" 2>/dev/null; then
        log_info "Original SYSTEM hive restored successfully"
    else
        log_error "CRITICAL: Failed to restore original SYSTEM hive — disk may be in inconsistent state"
    fi
    return 1
fi

log_info "SYSTEM hive uploaded successfully"

# Truncate transaction logs so Windows does not replay stale journals.
# The LOG/LOG1/LOG2 siblings beside the SYSTEM hive can undo our changes
# on next boot. Use a single guestfish session; the "-" prefix makes
# individual commands non-fatal (file may not exist).
HIVE_DIR=$(dirname "${HIVE_GUEST_PATH}")
LOG_STDERR="${WIN_HANDLER_TMPDIR}/gfish-logs.stderr"
if guestfish -a "${REWRITE_DISK}" -i 2>"${LOG_STDERR}" <<GFEOF
-truncate ${HIVE_DIR}/system.LOG
-truncate ${HIVE_DIR}/system.LOG1
-truncate ${HIVE_DIR}/system.LOG2
GFEOF
then
    log_info "Transaction logs truncated (system.LOG/LOG1/LOG2)"
else
    log_warn "Could not truncate some transaction logs (non-fatal)"
fi

# =========================================================================
# Success summary
# =========================================================================
log_info "Windows IP rewrite completed successfully"
log_info "  Product: ${REWRITE_OS_PRODUCT}"
log_info "  Control set: ${CONTROLSET}"
log_info "  Interfaces modified: ${REWRITE_IFACE_COUNT}"

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    iface_var="REWRITE_IFACE_${i}"
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"
    log_info "    ${!iface_var} → ${IFACE_TO_GUID["${i}"]}: ${!ip_var}/${!prefix_var} gw=${!gw_var}"
done

if [[ -n "${REWRITE_DNS:-}" ]]; then
    log_info "  DNS: ${REWRITE_DNS}"
fi

# Restore previous EXIT trap (avoid clobbering entrypoint's cleanup)
win_handler_cleanup
eval "${_WIN_PREV_TRAP:-trap - EXIT}"

return 0
