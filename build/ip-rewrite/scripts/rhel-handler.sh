#!/usr/bin/env bash
set -euo pipefail
#
# RHEL IP Rewrite Handler — Augeas-Based
#
# Rewrites static IP configuration on RHEL 7/8/9/10 guest filesystems
# using guestfish and Augeas. Supports both ifcfg (Shellvars.lns) and
# NetworkManager keyfile (NetworkManager.lns) config formats.
#
# Called by entrypoint.sh via 'source'. All configuration is available
# through exported REWRITE_* environment variables:
#   REWRITE_DISK          - Path to the boot disk
#   REWRITE_OS_NAME       - OS family (linux)
#   REWRITE_OS_DISTRO     - Distribution (rhel)
#   REWRITE_OS_MAJOR      - Major version (7, 8, 9, 10)
#   REWRITE_OS_MINOR      - Minor version
#   REWRITE_OS_PRODUCT    - Full product name
#   REWRITE_DNS           - Comma-separated DNS servers (optional)
#   REWRITE_IFACE_COUNT   - Number of interfaces to configure
#   REWRITE_IFACE_<N>     - Interface name (e.g., eth0)
#   REWRITE_IP_<N>        - IP address
#   REWRITE_PREFIX_<N>    - Prefix length (e.g., 24)
#   REWRITE_GATEWAY_<N>   - Gateway address
#
# Interface detection strategies (tried in order):
#   1. NM keyfile — Augeas match on /etc/NetworkManager/system-connections/
#   2. ifcfg by DEVICE — Augeas match on /etc/sysconfig/network-scripts/ifcfg-*/DEVICE
#   3. ifcfg by filename — file check for /etc/sysconfig/network-scripts/ifcfg-<iface>
#   4. nm_create — create a new NM keyfile from scratch (RHEL 8+ cloud-init VMs)
#
# Two-phase approach:
#   Phase 1 (read-only):  Detect config format for each interface
#   Phase 2 (read-write): Apply aug-set commands or create new keyfiles

# ---------------------------------------------------------------------------
# Validate required environment variables
# ---------------------------------------------------------------------------

if [[ -z "${REWRITE_DISK:-}" ]]; then
    log_error "REWRITE_DISK is not set"
    return 1
fi

if [[ ! -e "${REWRITE_DISK}" ]]; then
    log_error "REWRITE_DISK does not exist: ${REWRITE_DISK}"
    return 1
fi

if [[ -z "${REWRITE_IFACE_COUNT:-}" ]] || (( REWRITE_IFACE_COUNT < 1 )); then
    log_error "REWRITE_IFACE_COUNT is not set or is zero"
    return 1
fi

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    iface_var="REWRITE_IFACE_${i}"
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"

    if [[ -z "${!iface_var:-}" || -z "${!ip_var:-}" || -z "${!prefix_var:-}" || -z "${!gw_var:-}" ]]; then
        log_error "Missing required variables for interface index ${i}: ${iface_var}, ${ip_var}, ${prefix_var}, ${gw_var}"
        return 1
    fi
done

log_info "RHEL handler invoked"
log_info "  Disk: ${REWRITE_DISK}"
log_info "  OS: ${REWRITE_OS_PRODUCT:-unknown} (${REWRITE_OS_DISTRO:-unknown} ${REWRITE_OS_MAJOR:-?}.${REWRITE_OS_MINOR:-?})"
log_info "  Interfaces: ${REWRITE_IFACE_COUNT}"

# Temp directory for RHEL handler scratch files (cleaned up on EXIT)
RHEL_HANDLER_TMPDIR=$(mktemp -d /tmp/rhel-handler-XXXXXX)
_RHEL_PREV_TRAP=$(trap -p EXIT 2>/dev/null || true)
rhel_handler_cleanup() {
    rm -rf "${RHEL_HANDLER_TMPDIR}" 2>/dev/null || true
}
trap rhel_handler_cleanup EXIT

# ---------------------------------------------------------------------------
# Phase 1: Discovery (read-only guestfish session)
#
# Discover all NM keyfile interface-name paths, ifcfg DEVICE paths, and
# ifcfg BOOTPROTO paths in a single read-only session.
# ---------------------------------------------------------------------------

log_info "Phase 1: Detecting config format for each interface..."

discovery_commands="aug-init / 0
aug-match '/files/etc/NetworkManager/system-connections/*/connection/interface-name'
aug-match '/files/etc/sysconfig/network-scripts/ifcfg-*/DEVICE'
aug-match '/files/etc/sysconfig/network-scripts/ifcfg-*/BOOTPROTO'
"

GF_STDERR=$(mktemp /tmp/gf-stderr-XXXXXX.txt)
discovery_output=""
discovery_rc=0
discovery_output=$(echo -e "${discovery_commands}" | guestfish --ro -a "${REWRITE_DISK}" -i 2>"${GF_STDERR}") || discovery_rc=$?

if [[ -s "${GF_STDERR}" ]]; then
    log_warn "Phase 1 guestfish stderr: $(cat "${GF_STDERR}")"
fi
: > "${GF_STDERR}"

if (( discovery_rc != 0 )); then
    log_error "Phase 1 guestfish discovery failed (exit code ${discovery_rc})"
    log_error "Output: ${discovery_output}"
    rm -f "${GF_STDERR}"
    return 1
fi

log_info "Phase 1: Discovery output received"

# Parse discovery output into path categories.
# Filter to lines starting with /files/ to exclude any stray guestfish output.
nm_paths=()
ifcfg_device_paths=()
ifcfg_bootproto_paths=()

while IFS= read -r line; do
    [[ -z "${line}" ]] && continue
    [[ "${line}" == /files/* ]] || continue
    if [[ "${line}" == */connection/interface-name ]]; then
        nm_paths+=("${line}")
    elif [[ "${line}" == */DEVICE ]]; then
        ifcfg_device_paths+=("${line}")
    elif [[ "${line}" == */BOOTPROTO ]]; then
        ifcfg_bootproto_paths+=("${line}")
    fi
done <<< "${discovery_output}"

log_info "  Found ${#nm_paths[@]} NM keyfile(s), ${#ifcfg_device_paths[@]} ifcfg DEVICE(s), ${#ifcfg_bootproto_paths[@]} ifcfg BOOTPROTO(s)"

# ---------------------------------------------------------------------------
# Phase 1b: Resolve values from discovered paths
# ---------------------------------------------------------------------------

declare -A NM_IFACE_PATHS=()     # iface_name -> augeas file path prefix
declare -A IFCFG_IFACE_PATHS=()  # iface_name -> augeas file path prefix
declare -A IFCFG_BOOTPROTO=()    # augeas file path prefix -> bootproto value
declare -A KNOWN_IFCFG_FILES=()  # augeas file path prefix -> 1 (all discovered ifcfg paths)

if (( ${#nm_paths[@]} + ${#ifcfg_device_paths[@]} + ${#ifcfg_bootproto_paths[@]} > 0 )); then
    resolve_commands="aug-init / 0\n"
    for p in "${nm_paths[@]}"; do
        resolve_commands+="aug-get '${p}'\n"
    done
    for p in "${ifcfg_device_paths[@]}"; do
        resolve_commands+="aug-get '${p}'\n"
    done
    for p in "${ifcfg_bootproto_paths[@]}"; do
        resolve_commands+="aug-get '${p}'\n"
    done

    resolve_output=""
    resolve_rc=0
    resolve_output=$(echo -e "${resolve_commands}" | guestfish --ro -a "${REWRITE_DISK}" -i 2>"${GF_STDERR}") || resolve_rc=$?

    if [[ -s "${GF_STDERR}" ]]; then
        log_warn "Phase 1b guestfish stderr: $(cat "${GF_STDERR}")"
    fi
    : > "${GF_STDERR}"

    if (( resolve_rc != 0 )); then
        log_error "Phase 1b guestfish resolve failed (exit code ${resolve_rc})"
        log_error "Output: ${resolve_output}"
        rm -f "${GF_STDERR}"
        return 1
    fi

    # Parse resolve output: values come back one per line in order
    resolve_values=()
    while IFS= read -r line; do
        [[ -z "${line}" ]] && continue
        resolve_values+=("${line}")
    done <<< "${resolve_output}"

    val_idx=0

    # Map NM keyfile paths to interface names (with duplicate detection)
    for p in "${nm_paths[@]}"; do
        iface_name="${resolve_values[${val_idx}]:-}"
        if [[ -n "${iface_name}" ]]; then
            file_prefix="${p%/connection/interface-name}"
            if [[ -n "${NM_IFACE_PATHS[${iface_name}]+isset}" ]]; then
                existing="${NM_IFACE_PATHS[${iface_name}]}"
                log_warn "Duplicate NM profiles for interface '${iface_name}': ${existing} and ${file_prefix}"
                log_warn "Using first discovered profile: ${existing}"
            else
                NM_IFACE_PATHS["${iface_name}"]="${file_prefix}"
                log_info "  NM keyfile: interface '${iface_name}' at ${file_prefix}"
            fi
        fi
        val_idx=$((val_idx + 1))
    done

    # Map ifcfg DEVICE paths to interface names
    for p in "${ifcfg_device_paths[@]}"; do
        iface_name="${resolve_values[${val_idx}]:-}"
        if [[ -n "${iface_name}" ]]; then
            file_prefix="${p%/DEVICE}"
            IFCFG_IFACE_PATHS["${iface_name}"]="${file_prefix}"
            KNOWN_IFCFG_FILES["${file_prefix}"]=1
            log_info "  ifcfg: interface '${iface_name}' at ${file_prefix}"
        fi
        val_idx=$((val_idx + 1))
    done

    # Map ifcfg BOOTPROTO values to file prefixes
    for p in "${ifcfg_bootproto_paths[@]}"; do
        bootproto_val="${resolve_values[${val_idx}]:-}"
        file_prefix="${p%/BOOTPROTO}"
        IFCFG_BOOTPROTO["${file_prefix}"]="${bootproto_val}"
        KNOWN_IFCFG_FILES["${file_prefix}"]=1
        val_idx=$((val_idx + 1))
    done
fi

# ---------------------------------------------------------------------------
# Match each requested interface to its config file and format
# ---------------------------------------------------------------------------

declare -a IFACE_NAMES=()
declare -a IFACE_FORMATS=()    # "nm" or "ifcfg"
declare -a IFACE_AUG_PATHS=()  # Augeas file path prefix

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    iface_var="REWRITE_IFACE_${i}"
    target_iface="${!iface_var}"

    format=""
    aug_path=""

    # Strategy 1: Check NM keyfile (preferred, works for RHEL 8/9/10)
    if [[ -n "${NM_IFACE_PATHS[${target_iface}]+isset}" ]]; then
        format="nm"
        aug_path="${NM_IFACE_PATHS[${target_iface}]}"
        log_info "  Interface '${target_iface}': matched NM keyfile at ${aug_path}"

    # Strategy 2: Check ifcfg by DEVICE field (handles DEVICE != filename)
    elif [[ -n "${IFCFG_IFACE_PATHS[${target_iface}]+isset}" ]]; then
        aug_path="${IFCFG_IFACE_PATHS[${target_iface}]}"

        # Refuse DHCP-configured interfaces (DHCP-to-static not supported)
        bootproto="${IFCFG_BOOTPROTO[${aug_path}]:-}"
        if [[ "${bootproto}" == "dhcp" || "${bootproto}" == "bootp" ]]; then
            log_error "Interface '${target_iface}' has BOOTPROTO=${bootproto} (DHCP) — refusing to convert to static"
            log_error "DHCP-to-static conversion is not supported; configure the interface as static first"
            rm -f "${GF_STDERR}"
            return 1
        fi

        format="ifcfg"
        log_info "  Interface '${target_iface}': matched ifcfg (DEVICE) at ${aug_path}"

    else
        # Strategy 3: Check ifcfg by filename convention (ifcfg-<iface>)
        candidate_path="/files/etc/sysconfig/network-scripts/ifcfg-${target_iface}"

        # If this path was discovered with a different DEVICE, skip it
        if [[ -n "${KNOWN_IFCFG_FILES[${candidate_path}]+isset}" ]]; then
            log_warn "  ifcfg file at ${candidate_path} exists but DEVICE differs from '${target_iface}' — skipping filename match"
        else
            # Verify the file exists in the guest filesystem
            fs_candidate="/etc/sysconfig/network-scripts/ifcfg-${target_iface}"
            verify_output=""
            verify_rc=0
            verify_output=$(echo "is-file '${fs_candidate}'" | guestfish --ro -a "${REWRITE_DISK}" -i 2>"${GF_STDERR}") || verify_rc=$?
            if [[ -s "${GF_STDERR}" ]]; then
                log_warn "Strategy 3 guestfish stderr: $(cat "${GF_STDERR}")"
            fi
            : > "${GF_STDERR}"

            if (( verify_rc == 0 )) && [[ "${verify_output}" == "true" ]]; then
                # File exists — check BOOTPROTO isn't DHCP
                bp_cmd="aug-init / 0\naug-get '${candidate_path}/BOOTPROTO'"
                bp_output=""
                bp_rc=0
                bp_output=$(echo -e "${bp_cmd}" | guestfish --ro -a "${REWRITE_DISK}" -i 2>"${GF_STDERR}") || bp_rc=$?
                : > "${GF_STDERR}"

                if (( bp_rc == 0 )); then
                    bp_val="${bp_output}"
                    if [[ "${bp_val}" == "dhcp" || "${bp_val}" == "bootp" ]]; then
                        log_error "Interface '${target_iface}' has BOOTPROTO=${bp_val} (DHCP) — refusing to convert to static"
                        rm -f "${GF_STDERR}"
                        return 1
                    fi
                fi
                # bp_rc != 0 means BOOTPROTO field absent — treat as static (OK)

                format="ifcfg"
                aug_path="${candidate_path}"
                log_info "  Interface '${target_iface}': matched ifcfg (filename convention) at ${aug_path}"
            fi
        fi
    fi

    # Strategy 4: Create a new NM keyfile from scratch (RHEL 8+)
    # Cloud-init VMs typically have no persistent network config on disk;
    # cloud-init generates connections at runtime in /run/. We create a
    # persistent keyfile so the IP rewrite takes effect before cloud-init.
    if [[ -z "${format}" && "${REWRITE_OS_MAJOR:-0}" -ge 8 ]]; then
        log_info "  No existing config found for '${target_iface}' — will create NM keyfile (Strategy 4)"
        format="nm_create"
        aug_path=""   # not applicable — we write the file directly via guestfish
    fi

    if [[ -z "${format}" ]]; then
        log_error "Interface '${target_iface}' not found in any config file on the guest filesystem"
        log_error "Searched: NM keyfiles in /etc/NetworkManager/system-connections/, ifcfg files in /etc/sysconfig/network-scripts/"
        rm -f "${GF_STDERR}"
        return 1
    fi

    IFACE_NAMES+=("${target_iface}")
    IFACE_FORMATS+=("${format}")
    IFACE_AUG_PATHS+=("${aug_path}")
done

# ---------------------------------------------------------------------------
# Phase 2: Rewrite (read-write guestfish session)
# ---------------------------------------------------------------------------

log_info "Phase 2: Applying IP configuration..."

# ---------------------------------------------------------------------------
# Phase 2a: Handle nm_create interfaces (write new NM keyfiles to disk)
# ---------------------------------------------------------------------------
NM_CREATE_FILES=()   # local temp files to upload

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    if [[ "${IFACE_FORMATS[${i}]}" != "nm_create" ]]; then
        continue
    fi

    iface_var="REWRITE_IFACE_${i}"
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"

    target_iface="${!iface_var}"
    target_ip="${!ip_var}"
    target_prefix="${!prefix_var}"
    target_gw="${!gw_var}"

    log_info "  Creating NM keyfile for '${target_iface}': ${target_ip}/${target_prefix} gw ${target_gw}"

    # Build NM keyfile content
    nm_keyfile_content="[connection]
id=${target_iface}
type=ethernet
interface-name=${target_iface}
autoconnect=true
autoconnect-priority=1

[ipv4]
method=manual
address1=${target_ip}/${target_prefix},${target_gw}"

    # DNS (optional) — semicolon-separated with trailing semicolon
    if [[ -n "${REWRITE_DNS:-}" ]]; then
        dns_value="${REWRITE_DNS//,/;}"
        dns_value="${dns_value};"
        nm_keyfile_content+="
dns=${dns_value}"
    fi

    nm_keyfile_content+="

[ipv6]
method=disabled
"

    # Write keyfile to a local temp file
    NM_KEYFILE_LOCAL=$(mktemp "${RHEL_HANDLER_TMPDIR}/nm-keyfile-${target_iface}-XXXXXX.nmconnection")
    echo "${nm_keyfile_content}" > "${NM_KEYFILE_LOCAL}"

    # Record the guest path and local file for upload
    NM_GUEST_PATH="/etc/NetworkManager/system-connections/${target_iface}.nmconnection"
    NM_CREATE_FILES+=("${NM_KEYFILE_LOCAL}:${NM_GUEST_PATH}")

    log_info "  NM keyfile prepared: ${NM_GUEST_PATH}"
done

# Upload any new NM keyfiles via guestfish (ext4/xfs mounts are not blocked)
if [[ ${#NM_CREATE_FILES[@]} -gt 0 ]]; then
    log_info "Phase 2a: Uploading ${#NM_CREATE_FILES[@]} new NM keyfile(s) to disk..."

    upload_cmds=""
    for entry in "${NM_CREATE_FILES[@]}"; do
        local_file="${entry%%:*}"
        guest_path="${entry#*:}"
        upload_cmds+="upload '${local_file}' '${guest_path}'\n"
        upload_cmds+="chmod 0600 '${guest_path}'\n"
    done

    upload_output=""
    upload_rc=0
    upload_output=$(echo -e "${upload_cmds}" | guestfish -a "${REWRITE_DISK}" -i 2>"${GF_STDERR}") || upload_rc=$?

    if [[ -s "${GF_STDERR}" ]]; then
        log_warn "Phase 2a guestfish stderr: $(cat "${GF_STDERR}")"
    fi
    : > "${GF_STDERR}"

    if (( upload_rc != 0 )); then
        log_error "Phase 2a guestfish upload failed (exit code ${upload_rc})"
        log_error "Output: ${upload_output}"
        rm -f "${GF_STDERR}"
        return 1
    fi

    log_info "Phase 2a: NM keyfiles uploaded successfully"
fi

# ---------------------------------------------------------------------------
# Phase 2b: Augeas-based rewrite for existing configs (nm / ifcfg formats)
# ---------------------------------------------------------------------------

gf_commands="aug-init / 0\n"
has_augeas_changes=false

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    iface_var="REWRITE_IFACE_${i}"
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"

    target_iface="${!iface_var}"
    target_ip="${!ip_var}"
    target_prefix="${!prefix_var}"
    target_gw="${!gw_var}"
    aug_path="${IFACE_AUG_PATHS[${i}]}"
    format="${IFACE_FORMATS[${i}]}"

    # nm_create is already handled above
    if [[ "${format}" == "nm_create" ]]; then
        continue
    fi

    has_augeas_changes=true
    log_info "  Rewriting interface '${target_iface}' (${format}): ${target_ip}/${target_prefix} gw ${target_gw}"

    if [[ "${format}" == "ifcfg" ]]; then
        # Set primary IP fields
        gf_commands+="aug-set '${aug_path}/IPADDR' '${target_ip}'\n"
        gf_commands+="aug-set '${aug_path}/PREFIX' '${target_prefix}'\n"
        gf_commands+="aug-set '${aug_path}/GATEWAY' '${target_gw}'\n"
        gf_commands+="aug-set '${aug_path}/BOOTPROTO' 'none'\n"

        # Clear stale NETMASK (conflicts with PREFIX)
        gf_commands+="aug-rm '${aug_path}/NETMASK'\n"

        # Clear numbered variants (IPADDR0/PREFIX0/GATEWAY0 conflict with unnumbered)
        gf_commands+="aug-rm '${aug_path}/IPADDR0'\n"
        gf_commands+="aug-rm '${aug_path}/PREFIX0'\n"
        gf_commands+="aug-rm '${aug_path}/GATEWAY0'\n"

        # DNS (optional) — individual DNS1, DNS2, ... fields
        if [[ -n "${REWRITE_DNS:-}" ]]; then
            IFS=',' read -ra dns_servers <<< "${REWRITE_DNS}"
            for ((d = 0; d < ${#dns_servers[@]}; d++)); do
                dns_idx=$((d + 1))
                gf_commands+="aug-set '${aug_path}/DNS${dns_idx}' '${dns_servers[${d}]}'\n"
            done
            # Clear stale DNS slots beyond what we set
            for ((d = ${#dns_servers[@]} + 1; d <= 10; d++)); do
                gf_commands+="aug-rm '${aug_path}/DNS${d}'\n"
            done
        fi

    elif [[ "${format}" == "nm" ]]; then
        # Set method and combined address1 (IP/prefix,gateway)
        gf_commands+="aug-set '${aug_path}/ipv4/method' 'manual'\n"
        gf_commands+="aug-set '${aug_path}/ipv4/address1' '${target_ip}/${target_prefix},${target_gw}'\n"

        # Clear standalone gateway key (would override address1 combined form)
        gf_commands+="aug-rm '${aug_path}/ipv4/gateway'\n"
        # Clear stale route1 if present
        gf_commands+="aug-rm '${aug_path}/ipv4/route1'\n"

        # DNS (optional) — semicolon-separated with trailing semicolon
        if [[ -n "${REWRITE_DNS:-}" ]]; then
            dns_value="${REWRITE_DNS//,/;}"
            dns_value="${dns_value};"
            gf_commands+="aug-set '${aug_path}/ipv4/dns' '${dns_value}'\n"
        fi
    fi
done

if [[ "${has_augeas_changes}" == "true" ]]; then
    gf_commands+="aug-save\n"

    # chmod 0600 on modified NM keyfiles (NetworkManager ignores files without 0600)
    for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
        if [[ "${IFACE_FORMATS[${i}]}" == "nm" ]]; then
            fs_path="${IFACE_AUG_PATHS[${i}]#/files}"
            gf_commands+="chmod 0600 '${fs_path}'\n"
        fi
    done

    log_info "Phase 2b: Executing Augeas rewrite commands..."

    rewrite_output=""
    rewrite_rc=0
    rewrite_output=$(echo -e "${gf_commands}" | guestfish -a "${REWRITE_DISK}" -i 2>"${GF_STDERR}") || rewrite_rc=$?

    if [[ -s "${GF_STDERR}" ]]; then
        log_warn "Phase 2b guestfish stderr: $(cat "${GF_STDERR}")"
    fi
    rm -f "${GF_STDERR}"

    if (( rewrite_rc != 0 )); then
        log_error "Phase 2b guestfish rewrite failed (exit code ${rewrite_rc})"
        log_error "Output: ${rewrite_output}"
        return 1
    fi

    log_info "Phase 2b: Rewrite completed successfully"
else
    rm -f "${GF_STDERR}"
    log_info "Phase 2b: No Augeas changes needed (all interfaces used nm_create)"
fi

# ---------------------------------------------------------------------------
# Phase 3: Disable cloud-init network management (RHEL 8+)
# ---------------------------------------------------------------------------
# Cloud-init caches network configuration on the root disk and can override
# our changes at boot time — even if networkData is removed from the VM spec.
# Writing this drop-in tells cloud-init to leave networking alone.

if (( ${REWRITE_OS_MAJOR:-0} >= 8 )); then
    CLOUD_DISABLE_FILE="${RHEL_HANDLER_TMPDIR}/99-disable-network-config.cfg"
    echo "network: {config: disabled}" > "${CLOUD_DISABLE_FILE}"

    CLOUD_GUEST_PATH="/etc/cloud/cloud.cfg.d/99-disable-network-config.cfg"
    log_info "Phase 3: Disabling cloud-init network management (${CLOUD_GUEST_PATH})"

    cloud_cmds=""
    cloud_cmds+="mkdir-p /etc/cloud/cloud.cfg.d\n"
    cloud_cmds+="upload '${CLOUD_DISABLE_FILE}' '${CLOUD_GUEST_PATH}'\n"
    cloud_cmds+="chmod 0644 '${CLOUD_GUEST_PATH}'\n"

    # Wipe all cloud-init state so it starts fresh and honors the disable file.
    # Without this, cloud-init replays cached network config from /var/lib/cloud/seed/
    # or /var/lib/cloud/instances/ that was saved during a previous boot.
    cloud_cmds+="-rm-rf /var/lib/cloud\n"

    cloud_output=""
    cloud_rc=0
    cloud_output=$(echo -e "${cloud_cmds}" | guestfish -a "${REWRITE_DISK}" -i 2>"${GF_STDERR}") || cloud_rc=$?

    if [[ -s "${GF_STDERR}" ]]; then
        log_warn "Phase 3 guestfish stderr: $(cat "${GF_STDERR}")"
    fi
    rm -f "${GF_STDERR}"

    if (( cloud_rc != 0 )); then
        log_warn "Phase 3 failed (non-fatal, exit code ${cloud_rc}): ${cloud_output}"
    else
        log_info "Phase 3: Cloud-init network disabled successfully"
    fi
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    iface_var="REWRITE_IFACE_${i}"
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"
    fmt="${IFACE_FORMATS[${i}]}"
    if [[ "${fmt}" == "nm_create" ]]; then
        fmt="nm (created)"
    fi
    log_info "  Updated: ${!iface_var} → ${!ip_var}/${!prefix_var} gw ${!gw_var} (${fmt})"
done

if [[ -n "${REWRITE_DNS:-}" ]]; then
    log_info "  DNS: ${REWRITE_DNS}"
fi

log_info "RHEL handler completed — ${REWRITE_IFACE_COUNT} interface(s) rewritten"

# Restore previous EXIT trap
rhel_handler_cleanup
eval "${_RHEL_PREV_TRAP:-trap - EXIT}"

return 0
