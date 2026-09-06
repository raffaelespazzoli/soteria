#!/usr/bin/env bash
#
# IP Rewrite Entrypoint Script
#
# Parses IP configuration from SOTERIA_* environment variables, detects
# the guest OS on PVC-backed disks via virt-inspector, and dispatches
# to the appropriate OS-specific handler (RHEL or Windows).
#
# Environment variables (set by mutating webhook from annotations):
#   SOTERIA_<IFACE>_IP  - e.g. SOTERIA_ETH0_IP="10.0.2.100/24;10.0.2.1"
#   SOTERIA_DNS         - e.g. SOTERIA_DNS="10.0.2.10,10.0.2.11"
#
# Exit codes:
#   0 - Success (or no-op when no SOTERIA_*_IP vars set)
#   1 - Error (unsupported OS, no boot disk, parse failure, etc.)
#
# Handler contract: handlers are sourced (not exec'd) and must use
# `return` (not `exit`) so the entrypoint can inspect the return code.

set -euo pipefail

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log_info()  { echo "[INFO]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"; }
log_warn()  { echo "[WARN]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }
log_error() { echo "[ERROR] $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }

# ---------------------------------------------------------------------------
# If arguments are passed, exec them (allows: podman run image cmd ...)
# ---------------------------------------------------------------------------
if [[ $# -gt 0 ]]; then
    exec "$@"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log_info "IP rewrite entrypoint starting"

# ---------------------------------------------------------------------------
# Dependency check — fail fast if required tools are missing
# ---------------------------------------------------------------------------
for cmd in virt-inspector xmllint; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        log_error "Required command not found: ${cmd}"
        exit 1
    fi
done

# ---------------------------------------------------------------------------
# Parse SOTERIA_*_IP environment variables (AC1, AC7)
# ---------------------------------------------------------------------------

# Collect all SOTERIA_*_IP env vars
IP_VARS=$(env | grep '^SOTERIA_.*_IP=' | sort || true)

if [[ -z "${IP_VARS}" ]]; then
    log_info "No SOTERIA_*_IP environment variables found — no IP rewrite requested"
    exit 0
fi

IP_VAR_COUNT=$(echo "${IP_VARS}" | wc -l)
log_info "Found ${IP_VAR_COUNT} IP configuration variable(s)"

# trim: strip leading/trailing whitespace from a value
trim() { local v="$1"; v="${v#"${v%%[![:space:]]*}"}"; v="${v%"${v##*[![:space:]]}"}"; printf '%s' "$v"; }

# Parse each SOTERIA_<IFACE>_IP variable
REWRITE_IFACE_COUNT=0

while IFS= read -r var; do
    varname="${var%%=*}"
    value="${var#*=}"

    # Extract interface name: strip SOTERIA_ prefix and _IP suffix, lowercase
    iface="${varname#SOTERIA_}"
    iface="${iface%_IP}"
    iface=$(echo "${iface}" | tr '[:upper:]' '[:lower:]')

    # Validate value format: must contain ';' separator
    if [[ "${value}" != *";"* ]]; then
        log_error "Malformed value for ${varname}: missing ';' separator (expected 'IP/PREFIX;GATEWAY', got '${value}')"
        exit 1
    fi

    # Split on ';' for address/prefix and gateway
    addr_part=$(trim "${value%;*}")
    gateway=$(trim "${value#*;}")

    # Validate address part: must contain '/' separator
    if [[ "${addr_part}" != *"/"* ]]; then
        log_error "Malformed value for ${varname}: missing '/' in address (expected 'IP/PREFIX', got '${addr_part}')"
        exit 1
    fi

    # Split address on '/' for IP and prefix length
    ip=$(trim "${addr_part%/*}")
    prefix=$(trim "${addr_part#*/}")

    # Validate extracted values are non-empty
    if [[ -z "${ip}" || -z "${prefix}" || -z "${gateway}" ]]; then
        log_error "Malformed value for ${varname}: empty IP, prefix, or gateway (ip='${ip}', prefix='${prefix}', gateway='${gateway}')"
        exit 1
    fi

    # Validate prefix is an integer in 0-32
    if ! [[ "${prefix}" =~ ^[0-9]+$ ]] || (( prefix < 0 || prefix > 32 )); then
        log_error "Malformed value for ${varname}: prefix must be an integer 0-32 (got '${prefix}')"
        exit 1
    fi

    # Export per-interface config as indexed variables
    export "REWRITE_IFACE_${REWRITE_IFACE_COUNT}=${iface}"
    export "REWRITE_IP_${REWRITE_IFACE_COUNT}=${ip}"
    export "REWRITE_PREFIX_${REWRITE_IFACE_COUNT}=${prefix}"
    export "REWRITE_GATEWAY_${REWRITE_IFACE_COUNT}=${gateway}"

    log_info "Parsed interface ${iface}: ip=${ip} prefix=${prefix} gateway=${gateway}"

    REWRITE_IFACE_COUNT=$((REWRITE_IFACE_COUNT + 1))
done <<< "${IP_VARS}"

export REWRITE_IFACE_COUNT

# Parse optional SOTERIA_DNS
if [[ -n "${SOTERIA_DNS:-}" ]]; then
    export REWRITE_DNS=$(trim "${SOTERIA_DNS}")
    log_info "Parsed DNS servers: ${REWRITE_DNS}"
else
    export REWRITE_DNS=""
    log_info "No DNS configuration provided"
fi

# ---------------------------------------------------------------------------
# Boot disk scanning (AC2)
# ---------------------------------------------------------------------------

log_info "Scanning disks for operating system..."

# Iterate disk mount points under /disks/
if [[ ! -d /disks ]]; then
    log_error "No /disks directory found — no PVC volumes mounted"
    exit 1
fi

DISK_CANDIDATES=()

# Check for block devices mounted directly at /disks/<volumeName> (volumeDevices)
for node in /disks/*; do
    [[ -e "${node}" ]] || continue
    if [[ -b "${node}" ]]; then
        DISK_CANDIDATES+=("${node}")
    fi
done

# Check for disk image files and block devices under /disks/*/
for disk_dir in /disks/*/; do
    [[ -d "${disk_dir}" ]] || continue

    # Check for disk.img file
    if [[ -f "${disk_dir}disk.img" ]]; then
        DISK_CANDIDATES+=("${disk_dir}disk.img")
    fi

    # Check for block device nodes in the directory
    for node in "${disk_dir}"*; do
        if [[ -b "${node}" ]]; then
            DISK_CANDIDATES+=("${node}")
        fi
    done
done

if [[ ${#DISK_CANDIDATES[@]} -eq 0 ]]; then
    log_error "No disk images or block devices found under /disks/"
    exit 1
fi

log_info "Found ${#DISK_CANDIDATES[@]} disk candidate(s)"

# Scan ALL disks first, then select boot disk
declare -A OS_DISKS=()        # disk_path -> inspect XML tmpfile
INSPECTOR_ERRORS=()           # stderr from failed inspector runs
INSPECTOR_ALL_FAILED=true     # track whether any inspector succeeded

for disk in "${DISK_CANDIDATES[@]}"; do
    # Determine volume name for logging
    parent_dir=$(dirname "${disk}")
    if [[ "${parent_dir}" == "/disks" ]]; then
        volume_name=$(basename "${disk}")
    else
        volume_name=$(basename "${parent_dir}")
    fi
    log_info "Inspecting disk: ${disk} (volume: ${volume_name})"

    STDERR_FILE=$(mktemp /tmp/inspector-stderr-XXXXXX.txt)
    INSPECT_OUTPUT=""
    inspector_rc=0
    INSPECT_OUTPUT=$(virt-inspector --xml --no-applications --no-icon -a "${disk}" 2>"${STDERR_FILE}") || inspector_rc=$?

    # Log any stderr regardless of exit code
    if [[ -s "${STDERR_FILE}" ]]; then
        log_warn "virt-inspector stderr for ${disk}: $(cat "${STDERR_FILE}")"
    fi

    if [[ ${inspector_rc} -ne 0 ]]; then
        INSPECTOR_ERRORS+=("${disk}: exit ${inspector_rc} — $(cat "${STDERR_FILE}")")
        rm -f "${STDERR_FILE}"
        log_warn "virt-inspector failed on ${disk} (exit code ${inspector_rc})"
        continue
    fi

    rm -f "${STDERR_FILE}"
    INSPECTOR_ALL_FAILED=false

    # Check if virt-inspector found an OS
    if echo "${INSPECT_OUTPUT}" | xmllint --xpath '//operatingsystem' - >/dev/null 2>&1; then
        TMPXML=$(mktemp /tmp/inspect-XXXXXX.xml)
        echo "${INSPECT_OUTPUT}" > "${TMPXML}"
        OS_DISKS["${disk}"]="${TMPXML}"
        log_info "Operating system found on ${disk}"
    else
        log_info "No operating system found on ${disk} (data disk) — skipping"
    fi
done

# If all disks failed the inspector command, report the real errors
if [[ "${INSPECTOR_ALL_FAILED}" == "true" ]]; then
    log_error "virt-inspector failed on all disk candidates"
    for err in "${INSPECTOR_ERRORS[@]}"; do
        log_error "  ${err}"
    done
    exit 1
fi

# Select boot disk from OS candidates
OS_DISK_COUNT=${#OS_DISKS[@]}

if [[ ${OS_DISK_COUNT} -eq 0 ]]; then
    log_error "No operating system detected on any disk — cannot proceed with IP rewrite"
    exit 1
elif [[ ${OS_DISK_COUNT} -eq 1 ]]; then
    BOOT_DISK="${!OS_DISKS[@]}"
    INSPECT_XML="${OS_DISKS[${BOOT_DISK}]}"
    log_info "Boot disk identified: ${BOOT_DISK}"
else
    # Multiple OS disks found — prefer volume named "rootdisk"
    log_warn "Multiple disks with operating systems detected (${OS_DISK_COUNT})"
    BOOT_DISK=""
    for candidate in "${!OS_DISKS[@]}"; do
        if [[ "${candidate}" == *"/rootdisk/"* || "${candidate}" == *"/rootdisk" ]]; then
            BOOT_DISK="${candidate}"
            INSPECT_XML="${OS_DISKS[${candidate}]}"
            log_info "Preferring rootdisk volume: ${BOOT_DISK}"
            break
        fi
    done
    if [[ -z "${BOOT_DISK}" ]]; then
        # No rootdisk — pick first alphabetically and warn
        for candidate in $(echo "${!OS_DISKS[@]}" | tr ' ' '\n' | sort); do
            BOOT_DISK="${candidate}"
            INSPECT_XML="${OS_DISKS[${candidate}]}"
            break
        done
        log_warn "No 'rootdisk' volume found — using ${BOOT_DISK}"
    fi

    # Clean up unused XML files
    for candidate in "${!OS_DISKS[@]}"; do
        if [[ "${candidate}" != "${BOOT_DISK}" ]]; then
            rm -f "${OS_DISKS[${candidate}]}"
        fi
    done
fi

export REWRITE_DISK="${BOOT_DISK}"

# ---------------------------------------------------------------------------
# OS detection and version extraction (AC3)
# ---------------------------------------------------------------------------

log_info "Extracting OS information from virt-inspector output..."

OS_NAME=$(xmllint --xpath 'string(//operatingsystem/name)' "${INSPECT_XML}" 2>/dev/null || true)
OS_DISTRO=$(xmllint --xpath 'string(//operatingsystem/distro)' "${INSPECT_XML}" 2>/dev/null || true)
OS_MAJOR=$(xmllint --xpath 'string(//operatingsystem/major_version)' "${INSPECT_XML}" 2>/dev/null || true)
OS_MINOR=$(xmllint --xpath 'string(//operatingsystem/minor_version)' "${INSPECT_XML}" 2>/dev/null || true)
OS_PRODUCT=$(xmllint --xpath 'string(//operatingsystem/product_name)' "${INSPECT_XML}" 2>/dev/null || true)

if [[ -z "${OS_NAME}" ]]; then
    log_error "Could not determine OS family from virt-inspector output"
    rm -f "${INSPECT_XML}"
    exit 1
fi

export REWRITE_OS_NAME="${OS_NAME}"
export REWRITE_OS_DISTRO="${OS_DISTRO}"
export REWRITE_OS_MAJOR="${OS_MAJOR}"
export REWRITE_OS_MINOR="${OS_MINOR}"
export REWRITE_OS_PRODUCT="${OS_PRODUCT}"

log_info "Detected OS: family=${OS_NAME} distro=${OS_DISTRO} version=${OS_MAJOR}.${OS_MINOR}"
log_info "Product name: ${OS_PRODUCT}"

# Clean up temp file
rm -f "${INSPECT_XML}"

# ---------------------------------------------------------------------------
# Dispatch to OS-specific handler (AC4, AC5, AC6)
# ---------------------------------------------------------------------------

SUPPORTED_OS_MSG="Supported operating systems: RHEL 7/8/9/10, Windows Server 2016/2019/2022/2025, Windows 10/11"

if [[ "${OS_NAME}" == "linux" && "${OS_DISTRO}" == "rhel" ]]; then
    # Gate on supported RHEL major versions
    case "${OS_MAJOR}" in
        7|8|9|10) ;;
        *)
            log_error "Unsupported RHEL version: ${OS_MAJOR} (product: ${OS_PRODUCT})"
            log_error "${SUPPORTED_OS_MSG}"
            exit 1
            ;;
    esac

    HANDLER="${SCRIPT_DIR}/rhel-handler.sh"
    log_info "Dispatching to RHEL handler: ${HANDLER}"

    if [[ ! -x "${HANDLER}" ]]; then
        log_error "RHEL handler not found or not executable: ${HANDLER}"
        exit 1
    fi

    # Handlers must use `return`, not `exit`, so set -e doesn't bypass our check.
    if ! source "${HANDLER}"; then
        handler_rc=$?
        log_error "RHEL handler failed with code ${handler_rc}"
        exit "${handler_rc}"
    fi

    log_info "RHEL handler completed successfully"

elif [[ "${OS_NAME}" == "windows" ]]; then
    HANDLER="${SCRIPT_DIR}/windows-handler.sh"
    log_info "Dispatching to Windows handler: ${HANDLER}"

    if [[ ! -x "${HANDLER}" ]]; then
        log_error "Windows handler not found or not executable: ${HANDLER}"
        exit 1
    fi

    if ! source "${HANDLER}"; then
        handler_rc=$?
        log_error "Windows handler failed with code ${handler_rc}"
        exit "${handler_rc}"
    fi

    log_info "Windows handler completed successfully"

else
    log_error "Unsupported operating system: family=${OS_NAME} distro=${OS_DISTRO}"
    log_error "${SUPPORTED_OS_MSG}"
    exit 1
fi

log_info "IP rewrite entrypoint completed successfully"
