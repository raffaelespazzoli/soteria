#!/usr/bin/env bash
#
# Windows IP Rewrite Handler (placeholder stub)
#
# This script will be replaced by Story 18.4 with hivex-based IP rewriting
# logic for Windows Server 2016/2019/2022/2025 and Windows 10/11 guest VMs.
#
# Called by entrypoint.sh via 'source'. All configuration is available
# through exported REWRITE_* environment variables:
#   REWRITE_DISK          - Path to the boot disk
#   REWRITE_OS_NAME       - OS family (windows)
#   REWRITE_OS_DISTRO     - Distribution (windows)
#   REWRITE_OS_MAJOR      - Major version (10)
#   REWRITE_OS_MINOR      - Minor version (0)
#   REWRITE_OS_PRODUCT    - Full product name
#   REWRITE_DNS           - Comma-separated DNS servers (optional)
#   REWRITE_IFACE_COUNT   - Number of interfaces to configure
#   REWRITE_IFACE_<N>     - Interface name (e.g., eth0)
#   REWRITE_IP_<N>        - IP address
#   REWRITE_PREFIX_<N>    - Prefix length (e.g., 24)
#   REWRITE_GATEWAY_<N>   - Gateway address

log_info "Windows handler invoked (placeholder stub)"
log_info "  Disk: ${REWRITE_DISK}"
log_info "  OS: ${REWRITE_OS_PRODUCT} (${REWRITE_OS_DISTRO} ${REWRITE_OS_MAJOR}.${REWRITE_OS_MINOR})"
log_info "  Interfaces: ${REWRITE_IFACE_COUNT}"

for ((i = 0; i < REWRITE_IFACE_COUNT; i++)); do
    iface_var="REWRITE_IFACE_${i}"
    ip_var="REWRITE_IP_${i}"
    prefix_var="REWRITE_PREFIX_${i}"
    gw_var="REWRITE_GATEWAY_${i}"
    log_info "  Interface ${i}: iface=${!iface_var} ip=${!ip_var}/${!prefix_var} gw=${!gw_var}"
done

if [[ -n "${REWRITE_DNS}" ]]; then
    log_info "  DNS: ${REWRITE_DNS}"
fi

log_info "Windows handler stub completed — no disk modifications performed"

return 0
