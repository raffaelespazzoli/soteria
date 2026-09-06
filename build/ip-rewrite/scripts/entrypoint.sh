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

set -euo pipefail

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log_info()  { echo "[INFO]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"; }
log_warn()  { echo "[WARN]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }
log_error() { echo "[ERROR] $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log_info "IP rewrite entrypoint starting"

# ---------------------------------------------------------------------------
# Task 1: Parse SOTERIA_*_IP environment variables (AC1, AC7)
# ---------------------------------------------------------------------------

# Collect all SOTERIA_*_IP env vars
IP_VARS=$(env | grep '^SOTERIA_.*_IP=' | sort || true)

if [[ -z "${IP_VARS}" ]]; then
    log_info "No SOTERIA_*_IP environment variables found — no IP rewrite requested"
    exit 0
fi

IP_VAR_COUNT=$(echo "${IP_VARS}" | wc -l)
log_info "Found ${IP_VAR_COUNT} IP configuration variable(s)"

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
    addr_part="${value%;*}"
    gateway="${value#*;}"

    # Validate address part: must contain '/' separator
    if [[ "${addr_part}" != *"/"* ]]; then
        log_error "Malformed value for ${varname}: missing '/' in address (expected 'IP/PREFIX', got '${addr_part}')"
        exit 1
    fi

    # Split address on '/' for IP and prefix length
    ip="${addr_part%/*}"
    prefix="${addr_part#*/}"

    # Validate extracted values are non-empty
    if [[ -z "${ip}" || -z "${prefix}" || -z "${gateway}" ]]; then
        log_error "Malformed value for ${varname}: empty IP, prefix, or gateway (ip='${ip}', prefix='${prefix}', gateway='${gateway}')"
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
    export REWRITE_DNS="${SOTERIA_DNS}"
    log_info "Parsed DNS servers: ${REWRITE_DNS}"
else
    export REWRITE_DNS=""
    log_info "No DNS configuration provided"
fi

# ---------------------------------------------------------------------------
# Task 2: Boot disk scanning (AC2)
# ---------------------------------------------------------------------------

log_info "Scanning disks for operating system..."

BOOT_DISK=""
INSPECT_XML=""

# Iterate disk mount points under /disks/
if [[ ! -d /disks ]]; then
    log_error "No /disks directory found — no PVC volumes mounted"
    exit 1
fi

DISK_CANDIDATES=()

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
    log_error "No disk images or block devices found under /disks/*/"
    exit 1
fi

log_info "Found ${#DISK_CANDIDATES[@]} disk candidate(s)"

for disk in "${DISK_CANDIDATES[@]}"; do
    volume_name=$(basename "$(dirname "${disk}")")
    log_info "Inspecting disk: ${disk} (volume: ${volume_name})"

    # Run virt-inspector; capture output and exit code
    INSPECT_OUTPUT=""
    if INSPECT_OUTPUT=$(virt-inspector --xml -a "${disk}" 2>&1); then
        # Check if virt-inspector found an OS
        if echo "${INSPECT_OUTPUT}" | xmllint --xpath '//operatingsystem' - >/dev/null 2>&1; then
            if [[ -n "${BOOT_DISK}" ]]; then
                log_warn "Multiple boot disks detected — using first: ${BOOT_DISK} (skipping ${disk})"
                continue
            fi
            BOOT_DISK="${disk}"
            INSPECT_XML=$(mktemp /tmp/inspect-XXXXXX.xml)
            echo "${INSPECT_OUTPUT}" > "${INSPECT_XML}"
            log_info "Boot disk identified: ${disk}"
        else
            log_info "No operating system found on ${disk} (data disk) — skipping"
        fi
    else
        log_warn "virt-inspector failed on ${disk}: ${INSPECT_OUTPUT}"
    fi
done

if [[ -z "${BOOT_DISK}" ]]; then
    log_error "No operating system detected on any disk — cannot proceed with IP rewrite"
    exit 1
fi

export REWRITE_DISK="${BOOT_DISK}"

# ---------------------------------------------------------------------------
# Task 3: OS detection and version extraction (AC3)
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
# Task 4: Dispatch to OS-specific handler (AC4, AC5, AC6)
# ---------------------------------------------------------------------------

SUPPORTED_OS_MSG="Supported operating systems: RHEL 7/8/9/10, Windows Server 2016/2019/2022/2025, Windows 10/11"

if [[ "${OS_NAME}" == "linux" && "${OS_DISTRO}" == "rhel" ]]; then
    HANDLER="${SCRIPT_DIR}/rhel-handler.sh"
    log_info "Dispatching to RHEL handler: ${HANDLER}"

    if [[ ! -x "${HANDLER}" ]]; then
        log_error "RHEL handler not found or not executable: ${HANDLER}"
        exit 1
    fi

    source "${HANDLER}"
    handler_rc=$?

    if [[ ${handler_rc} -ne 0 ]]; then
        log_error "RHEL handler exited with code ${handler_rc}"
        exit ${handler_rc}
    fi

    log_info "RHEL handler completed successfully"

elif [[ "${OS_NAME}" == "windows" ]]; then
    HANDLER="${SCRIPT_DIR}/windows-handler.sh"
    log_info "Dispatching to Windows handler: ${HANDLER}"

    if [[ ! -x "${HANDLER}" ]]; then
        log_error "Windows handler not found or not executable: ${HANDLER}"
        exit 1
    fi

    source "${HANDLER}"
    handler_rc=$?

    if [[ ${handler_rc} -ne 0 ]]; then
        log_error "Windows handler exited with code ${handler_rc}"
        exit ${handler_rc}
    fi

    log_info "Windows handler completed successfully"

else
    log_error "Unsupported operating system: family=${OS_NAME} distro=${OS_DISTRO}"
    log_error "${SUPPORTED_OS_MSG}"
    exit 1
fi

log_info "IP rewrite entrypoint completed successfully"
