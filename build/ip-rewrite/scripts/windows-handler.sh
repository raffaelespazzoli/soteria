#!/usr/bin/env bash
#
# Windows IP Rewrite Handler
#
# Rewrites static IP configuration in the Windows registry SYSTEM hive
# for Windows Server 2016/2019/2022/2025 and Windows 11 guest VMs.
#
# The hive is edited in place inside a single libguestfs session
# (build/ip-rewrite/scripts/virt-windows-ip-rewrite). The hive is never
# downloaded to or uploaded from the container.
#
# Called by entrypoint.sh via 'source'. Configuration is in REWRITE_* env
# vars. Use `return` (not `exit`) because this file is sourced.

set -euo pipefail

log_info "Windows handler starting for ${REWRITE_OS_PRODUCT:-unknown}"

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

_WIN_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WIN_HELPER="${_WIN_SCRIPT_DIR}/virt-windows-ip-rewrite"

if [[ ! -x "${WIN_HELPER}" ]]; then
    log_error "Windows hive helper not found or not executable: ${WIN_HELPER}"
    return 1
fi

if ! command -v guestfish >/dev/null 2>&1; then
    log_error "Required command not found: guestfish"
    return 1
fi
if ! command -v python3 >/dev/null 2>&1; then
    log_error "Required command not found: python3 (python3-libguestfs)"
    return 1
fi

log_info "Dispatching in-place hive rewrite: ${WIN_HELPER}"
if ! "${WIN_HELPER}"; then
    log_error "In-place Windows hive rewrite failed"
    return 1
fi

log_info "Windows handler completed successfully"
return 0
