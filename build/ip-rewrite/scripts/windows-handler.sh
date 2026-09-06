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

# ---------------------------------------------------------------------------
# Temp file tracking and cleanup
# ---------------------------------------------------------------------------
WIN_HANDLER_TMPDIR=$(mktemp -d /tmp/win-handler-XXXXXX)

win_handler_cleanup() {
    rm -rf "${WIN_HANDLER_TMPDIR}" 2>/dev/null || true
}
trap win_handler_cleanup EXIT

HIVE_LOCAL="${WIN_HANDLER_TMPDIR}/system.hive"
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

# Read adapter properties for matching
declare -A ADAPTER_DHCP=()      # GUID -> EnableDHCP value
declare -A ADAPTER_IPADDR=()    # GUID -> IPAddress value (first entry)
declare -A ADAPTER_SUBNET=()    # GUID -> SubnetMask value (first entry)

for guid in "${GUID_LIST[@]}"; do
    log_info "  Reading adapter ${guid}"

    ADAPTER_PATH="${INTERFACES_PATH}\\${guid}"

    # Read EnableDHCP (REG_DWORD). hivexsh lsval prints "name"=type:value
    ENABLE_DHCP=""
    DHCP_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval EnableDHCP
HIVEOF
    )
    if [[ -n "${DHCP_OUTPUT}" ]]; then
        # Extract the DWORD value — hivexsh outputs: "EnableDHCP"=dword:00000000
        ENABLE_DHCP=$(echo "${DHCP_OUTPUT}" | sed -n 's/.*dword:\([0-9a-fA-F]*\).*/\1/p')
        ENABLE_DHCP=$((16#${ENABLE_DHCP:-0}))
    fi
    ADAPTER_DHCP["${guid}"]="${ENABLE_DHCP:-}"

    # Read IPAddress (REG_MULTI_SZ). hivexsh prints hex or decoded form
    IP_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval IPAddress
HIVEOF
    )

    # Parse REG_MULTI_SZ — hivexsh outputs hex bytes for type 7
    # We need to decode UTF-16LE hex to extract the IP string
    IP_ADDR=""
    if [[ -n "${IP_OUTPUT}" ]]; then
        # Extract hex bytes after "hex(7):" from lsval output
        HEX_BYTES=$(echo "${IP_OUTPUT}" | sed -n 's/.*hex(7):\(.*\)/\1/p' | tr -d ' \\\n\r')
        if [[ -n "${HEX_BYTES}" ]]; then
            # Decode UTF-16LE hex to ASCII (take bytes before first double-null)
            IP_ADDR=""
            IFS=',' read -ra BYTE_ARR <<< "${HEX_BYTES}"
            local_i=0
            while (( local_i < ${#BYTE_ARR[@]} - 1 )); do
                low="${BYTE_ARR[$local_i]}"
                high="${BYTE_ARR[$((local_i + 1))]}"
                # Check for null terminator (00,00)
                if [[ "${low}" == "00" && "${high}" == "00" ]]; then
                    break
                fi
                # ASCII char is the low byte for UTF-16LE (high should be 00)
                if [[ "${high}" == "00" ]]; then
                    IP_ADDR+=$(printf "\\x${low}")
                fi
                local_i=$((local_i + 2))
            done
        fi
    fi
    ADAPTER_IPADDR["${guid}"]="${IP_ADDR}"

    # Read SubnetMask similarly
    MASK_OUTPUT=$(hivexsh "${HIVE_LOCAL}" 2>/dev/null <<HIVEOF || true
cd \\${ADAPTER_PATH}
lsval SubnetMask
HIVEOF
    )

    SUBNET_MASK=""
    if [[ -n "${MASK_OUTPUT}" ]]; then
        HEX_BYTES=$(echo "${MASK_OUTPUT}" | sed -n 's/.*hex(7):\(.*\)/\1/p' | tr -d ' \\\n\r')
        if [[ -n "${HEX_BYTES}" ]]; then
            IFS=',' read -ra BYTE_ARR <<< "${HEX_BYTES}"
            local_i=0
            while (( local_i < ${#BYTE_ARR[@]} - 1 )); do
                low="${BYTE_ARR[$local_i]}"
                high="${BYTE_ARR[$((local_i + 1))]}"
                if [[ "${low}" == "00" && "${high}" == "00" ]]; then
                    break
                fi
                if [[ "${high}" == "00" ]]; then
                    SUBNET_MASK+=$(printf "\\x${low}")
                fi
                local_i=$((local_i + 2))
            done
        fi
    fi
    ADAPTER_SUBNET["${guid}"]="${SUBNET_MASK}"

    log_info "    EnableDHCP=${ADAPTER_DHCP["${guid}"]:-N/A} IPAddress=${ADAPTER_IPADDR["${guid}"]:-N/A} SubnetMask=${ADAPTER_SUBNET["${guid}"]:-N/A}"
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

# ip_to_network: compute network address from IP and mask for subnet matching
ip_to_network() {
    local ip="$1" mask="$2"
    IFS='.' read -r a1 a2 a3 a4 <<< "${ip}"
    IFS='.' read -r m1 m2 m3 m4 <<< "${mask}"
    printf "%d.%d.%d.%d" $(( a1 & m1 )) $(( a2 & m2 )) $(( a3 & m3 )) $(( a4 & m4 ))
}

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
            # All adapters are DHCP — pick first and warn
            matched_guid="${ALL_GUIDS_SORTED[0]}"
            log_warn "  No static-IP adapter found — selecting DHCP adapter ${matched_guid} (will convert to static)"
        fi
    else
        # Multi-NIC: try subnet matching first
        for guid in "${ALL_GUIDS_SORTED[@]}"; do
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
                already_used=false
                for assigned in "${IFACE_TO_GUID[@]}"; do
                    if [[ "${assigned}" == "${guid}" ]]; then
                        already_used=true
                        break
                    fi
                done
                if [[ "${already_used}" == "false" ]]; then
                    matched_guid="${guid}"
                    dhcp="${ADAPTER_DHCP["${guid}"]:-}"
                    if [[ "${dhcp}" != "0" ]]; then
                        log_warn "  Fallback selected DHCP adapter ${guid} (will convert to static)"
                    else
                        log_info "  Fallback selected adapter ${guid}"
                    fi
                    break
                fi
            done
        fi
    fi

    if [[ -z "${matched_guid}" ]]; then
        log_error "No matching adapter found for interface ${i} (${iface})"
        log_error "Available adapters:"
        for guid in "${ALL_GUIDS_SORTED[@]}"; do
            log_error "  ${guid}: EnableDHCP=${ADAPTER_DHCP["${guid}"]:-N/A} IPAddress=${ADAPTER_IPADDR["${guid}"]:-N/A}"
        done
        return 1
    fi

    IFACE_TO_GUID["${i}"]="${matched_guid}"
    log_info "  Matched: interface ${i} (${iface}) → adapter ${matched_guid}"
done

# =========================================================================
# Phase 3: Write IP configuration via hivexregedit --merge
# =========================================================================
log_info "Phase 3: Writing IP configuration to SYSTEM hive"

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

    # Append registry key and values to .reg file
    # .reg file format uses single backslashes in key paths
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
        log_info "Writing DNS servers on ${target_iface}: ${REWRITE_DNS}"
        cat >> "${REG_FILE}" <<REGDNS
"NameServer"="${REWRITE_DNS}"
REGDNS
    fi

    # Blank line between registry key blocks
    echo "" >> "${REG_FILE}"
done

# Merge .reg file into the SYSTEM hive
log_info "Merging registry changes into SYSTEM hive"

MERGE_STDERR="${WIN_HANDLER_TMPDIR}/hivexregedit-merge.stderr"
if ! hivexregedit --merge "${HIVE_LOCAL}" "${REG_FILE}" 2>"${MERGE_STDERR}"; then
    log_error "hivexregedit --merge failed"
    if [[ -s "${MERGE_STDERR}" ]]; then
        log_error "hivexregedit stderr: $(cat "${MERGE_STDERR}")"
    fi
    return 1
fi

log_info "Registry changes merged successfully"

# =========================================================================
# Phase 4: Upload modified hive (guestfish)
# =========================================================================
log_info "Phase 4: Uploading modified SYSTEM hive to disk"

UPLOAD_STDERR="${WIN_HANDLER_TMPDIR}/gfish-upload.stderr"
if ! guestfish -a "${REWRITE_DISK}" -i -- \
    upload "${HIVE_LOCAL}" "${HIVE_GUEST_PATH}" 2>"${UPLOAD_STDERR}"; then
    log_error "Failed to upload modified SYSTEM hive to ${HIVE_GUEST_PATH}"
    if [[ -s "${UPLOAD_STDERR}" ]]; then
        log_error "guestfish stderr: $(cat "${UPLOAD_STDERR}")"
    fi
    return 1
fi

log_info "SYSTEM hive uploaded successfully"

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

return 0
