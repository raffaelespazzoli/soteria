#!/usr/bin/env bash
#
# IP Rewrite Integration Test Runner
#
# Creates synthetic disk image fixtures and runs the RHEL and Windows
# handler scripts against them, verifying that IP configuration is
# rewritten correctly.
#
# Prerequisites:
#   - guestfish, guestfs-tools (libguestfs)
#   - python3 + python3-libguestfs (Windows in-place hive writes)
#   - LIBGUESTFS_BACKEND=direct (set by Makefile target)
#
# Usage: run-tests.sh

set -euo pipefail

export LIBGUESTFS_BACKEND="${LIBGUESTFS_BACKEND:-direct}"
export HOME="${HOME:-/tmp}"
export TMPDIR="${TMPDIR:-/tmp}"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp}"

# python3-libguestfs may be present system-wide, or unpacked for local tests.
if ! python3 -c "import guestfs" >/dev/null 2>&1; then
    extracted=$(find /tmp/pyguestfs-extract -name guestfs.py 2>/dev/null | head -1 || true)
    if [[ -n "${extracted}" ]]; then
        export PYTHONPATH="$(dirname "${extracted}")${PYTHONPATH:+:${PYTHONPATH}}"
    fi
fi
if ! python3 -c "import guestfs" >/dev/null 2>&1; then
    echo "python3-libguestfs is required (import guestfs). Install with: sudo dnf install python3-libguestfs" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FIXTURE_DIR="${SCRIPT_DIR}/fixtures"
HANDLER_DIR="${PROJECT_ROOT}/build/ip-rewrite/scripts"

# Temp directory for test disk images
TEST_TMPDIR=$(mktemp -d /tmp/ip-rewrite-test-XXXXXX)
trap 'rm -rf "${TEST_TMPDIR}"' EXIT

PASSED=0
FAILED=0
TOTAL=0

# Colours (disabled if stdout is not a terminal)
if [[ -t 1 ]]; then
    GREEN='\033[0;32m'
    RED='\033[0;31m'
    YELLOW='\033[0;33m'
    NC='\033[0m'
else
    GREEN=''
    RED=''
    YELLOW=''
    NC=''
fi

pass() {
    PASSED=$((PASSED + 1))
    TOTAL=$((TOTAL + 1))
    echo -e "${GREEN}PASS${NC}: $1"
}

fail() {
    FAILED=$((FAILED + 1))
    TOTAL=$((TOTAL + 1))
    echo -e "${RED}FAIL${NC}: $1: $2"
}

skip() {
    TOTAL=$((TOTAL + 1))
    echo -e "${YELLOW}SKIP${NC}: $1: $2"
}

# ---------------------------------------------------------------------------
# Logging helpers (same interface as entrypoint.sh — handlers source these)
# ---------------------------------------------------------------------------
log_info()  { echo "[INFO]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"; }
log_warn()  { echo "[WARN]  $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }
log_error() { echo "[ERROR] $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >&2; }
export -f log_info log_warn log_error

# Read a guest file from a single-partition fixture (no inspect / -i).
fixture_cat() {
    guestfish --ro -a "$1" -m /dev/sda1:/ -- cat "$2"
}

# Read a UTF-8 registry value from the fixture hive via guestfish (no hivexget).
_win_hive_prep() {
    local disk="$1"
    guestfish --remote -- add-drive "${disk}" readonly:true
    guestfish --remote -- run
    if ! guestfish --remote -- mount /dev/sda1 /; then
        guestfish --remote -- mount /dev/sda /
    fi
    guestfish --remote -- hivex-open /Windows/System32/config/system
}

_win_hive_adapter_node() {
    local guid='{12345678-1234-1234-1234-123456789abc}'
    local root cs svc tcp par ifs
    root=$(guestfish --remote -- hivex-root)
    cs=$(guestfish --remote -- hivex-node-get-child "${root}" ControlSet001)
    svc=$(guestfish --remote -- hivex-node-get-child "${cs}" Services)
    tcp=$(guestfish --remote -- hivex-node-get-child "${svc}" Tcpip)
    par=$(guestfish --remote -- hivex-node-get-child "${tcp}" Parameters)
    ifs=$(guestfish --remote -- hivex-node-get-child "${par}" Interfaces)
    guestfish --remote -- hivex-node-get-child "${ifs}" "${guid}"
}

win_hive_utf8() {
    local disk="$1" value_name="$2" node vh
    export HOME=/tmp XDG_RUNTIME_DIR=/tmp
    eval "$(guestfish --listen)"
    _win_hive_prep "${disk}"
    node=$(_win_hive_adapter_node)
    vh=$(guestfish --remote -- hivex-node-get-value "${node}" "${value_name}")
    # hivex-value-utf8 rejects REG_MULTI_SZ; strip UTF-16LE NULs from the raw blob.
    guestfish --remote -- hivex-value-value "${vh}" | tr -d '\0'
    guestfish --remote -- exit >/dev/null || true
    unset GUESTFISH_PID
}

win_hive_dword() {
    local disk="$1" value_name="$2" node vh first
    export HOME=/tmp XDG_RUNTIME_DIR=/tmp
    eval "$(guestfish --listen)"
    _win_hive_prep "${disk}"
    node=$(_win_hive_adapter_node)
    vh=$(guestfish --remote -- hivex-node-get-value "${node}" "${value_name}")
    first=$(guestfish --remote -- hivex-value-value "${vh}" 2>/dev/null | od -An -tu1 | awk '{print $1; exit}')
    echo "${first:-0}"
    guestfish --remote -- exit >/dev/null || true
    unset GUESTFISH_PID
}

# =========================================================================
# Phase 1: Create test fixtures
# =========================================================================
echo "================================================================"
echo "Phase 1: Creating test fixtures"
echo "================================================================"

RHEL_IFCFG_IMG="${TEST_TMPDIR}/rhel-ifcfg.img"
RHEL_NM_IMG="${TEST_TMPDIR}/rhel-nmkeyfile.img"
WINDOWS_IMG="${TEST_TMPDIR}/windows.img"

bash "${FIXTURE_DIR}/create-rhel-ifcfg-fixture.sh" "${RHEL_IFCFG_IMG}" --force
bash "${FIXTURE_DIR}/create-rhel-nmkeyfile-fixture.sh" "${RHEL_NM_IMG}" --force
bash "${FIXTURE_DIR}/create-windows-fixture.sh" "${WINDOWS_IMG}" --force

echo ""

# =========================================================================
# Phase 2: RHEL ifcfg handler tests
# =========================================================================
echo "================================================================"
echo "Phase 2: RHEL ifcfg handler tests"
echo "================================================================"

run_rhel_handler() {
    local disk="$1"
    (
        trap - EXIT
        export REWRITE_DISK="${disk}"
        export REWRITE_OS_NAME="linux"
        export REWRITE_OS_DISTRO="rhel"
        export REWRITE_OS_MAJOR="8"
        export REWRITE_OS_MINOR="9"
        export REWRITE_OS_PRODUCT="Red Hat Enterprise Linux 8.9"
        export REWRITE_IFACE_COUNT="${2:-1}"
        export REWRITE_IFACE_0="eth0"
        export REWRITE_IP_0="10.0.2.100"
        export REWRITE_PREFIX_0="24"
        export REWRITE_GATEWAY_0="10.0.2.1"
        export REWRITE_DNS="${3:-}"
        source "${HANDLER_DIR}/rhel-handler.sh"
    )
}

# Test 2.1: ifcfg IP rewrite
echo ""
echo "--- Test 2.1: ifcfg IP/prefix/gateway rewrite ---"
cp "${RHEL_IFCFG_IMG}" "${TEST_TMPDIR}/ifcfg-test.img"

if run_rhel_handler "${TEST_TMPDIR}/ifcfg-test.img"; then
    RESULT=$(fixture_cat "${TEST_TMPDIR}/ifcfg-test.img" /etc/sysconfig/network-scripts/ifcfg-eth0)

    ifcfg_ok=true
    echo "${RESULT}" | grep -q 'IPADDR=10.0.2.100' || { fail "ifcfg IPADDR rewrite" "IPADDR not rewritten"; ifcfg_ok=false; }
    echo "${RESULT}" | grep -q 'PREFIX=24'          || { fail "ifcfg PREFIX rewrite" "PREFIX not rewritten"; ifcfg_ok=false; }
    echo "${RESULT}" | grep -q 'GATEWAY=10.0.2.1'   || { fail "ifcfg GATEWAY rewrite" "GATEWAY not rewritten"; ifcfg_ok=false; }
    echo "${RESULT}" | grep -q 'BOOTPROTO=none'      || { fail "ifcfg BOOTPROTO rewrite" "BOOTPROTO not set to none"; ifcfg_ok=false; }

    if [[ "${ifcfg_ok}" == "true" ]]; then
        pass "ifcfg IP/prefix/gateway rewrite"
    fi
else
    fail "ifcfg handler execution" "rhel-handler.sh returned non-zero"
fi

# Test 2.2: ifcfg DNS rewrite
echo ""
echo "--- Test 2.2: ifcfg DNS rewrite ---"
cp "${RHEL_IFCFG_IMG}" "${TEST_TMPDIR}/ifcfg-dns-test.img"

if run_rhel_handler "${TEST_TMPDIR}/ifcfg-dns-test.img" 1 "10.0.2.10,10.0.2.11"; then
    RESULT=$(fixture_cat "${TEST_TMPDIR}/ifcfg-dns-test.img" /etc/sysconfig/network-scripts/ifcfg-eth0)

    dns_ok=true
    echo "${RESULT}" | grep -q 'DNS1=10.0.2.10' || { fail "ifcfg DNS1 rewrite" "DNS1 not rewritten"; dns_ok=false; }
    echo "${RESULT}" | grep -q 'DNS2=10.0.2.11' || { fail "ifcfg DNS2 rewrite" "DNS2 not rewritten"; dns_ok=false; }

    if [[ "${dns_ok}" == "true" ]]; then
        pass "ifcfg DNS rewrite"
    fi
else
    fail "ifcfg DNS handler execution" "rhel-handler.sh returned non-zero"
fi

# Test 2.3: ifcfg idempotency
echo ""
echo "--- Test 2.3: ifcfg idempotency (second run) ---"

if run_rhel_handler "${TEST_TMPDIR}/ifcfg-test.img"; then
    RESULT=$(fixture_cat "${TEST_TMPDIR}/ifcfg-test.img" /etc/sysconfig/network-scripts/ifcfg-eth0)
    echo "${RESULT}" | grep -q 'IPADDR=10.0.2.100' && pass "ifcfg idempotency" || fail "ifcfg idempotency" "IPADDR changed on second run"
else
    fail "ifcfg idempotency" "handler failed on second run"
fi

# =========================================================================
# Phase 3: RHEL NM keyfile handler tests
# =========================================================================
echo ""
echo "================================================================"
echo "Phase 3: RHEL NM keyfile handler tests"
echo "================================================================"

run_rhel_nm_handler() {
    local disk="$1"
    (
        trap - EXIT
        export REWRITE_DISK="${disk}"
        export REWRITE_OS_NAME="linux"
        export REWRITE_OS_DISTRO="rhel"
        export REWRITE_OS_MAJOR="9"
        export REWRITE_OS_MINOR="4"
        export REWRITE_OS_PRODUCT="Red Hat Enterprise Linux 9.4"
        export REWRITE_IFACE_COUNT="1"
        export REWRITE_IFACE_0="eth0"
        export REWRITE_IP_0="10.0.2.100"
        export REWRITE_PREFIX_0="24"
        export REWRITE_GATEWAY_0="10.0.2.1"
        export REWRITE_DNS="${2:-}"
        source "${HANDLER_DIR}/rhel-handler.sh"
    )
}

# Test 3.1: NM keyfile IP rewrite
echo ""
echo "--- Test 3.1: NM keyfile address/method rewrite ---"
cp "${RHEL_NM_IMG}" "${TEST_TMPDIR}/nm-test.img"

if run_rhel_nm_handler "${TEST_TMPDIR}/nm-test.img"; then
    RESULT=$(fixture_cat "${TEST_TMPDIR}/nm-test.img" /etc/NetworkManager/system-connections/eth0.nmconnection)

    nm_ok=true
    echo "${RESULT}" | grep -q 'address1=10.0.2.100/24,10.0.2.1' || { fail "NM address1 rewrite" "address1 not rewritten"; nm_ok=false; }
    echo "${RESULT}" | grep -q 'method=manual'                     || { fail "NM method rewrite" "method not set to manual"; nm_ok=false; }

    if [[ "${nm_ok}" == "true" ]]; then
        pass "NM keyfile address/method rewrite"
    fi
else
    fail "NM handler execution" "rhel-handler.sh returned non-zero"
fi

# Test 3.2: NM keyfile DNS rewrite
echo ""
echo "--- Test 3.2: NM keyfile DNS rewrite ---"
cp "${RHEL_NM_IMG}" "${TEST_TMPDIR}/nm-dns-test.img"

if run_rhel_nm_handler "${TEST_TMPDIR}/nm-dns-test.img" "10.0.2.10,10.0.2.11"; then
    RESULT=$(fixture_cat "${TEST_TMPDIR}/nm-dns-test.img" /etc/NetworkManager/system-connections/eth0.nmconnection)

    echo "${RESULT}" | grep -q 'dns=10.0.2.10;10.0.2.11;' && pass "NM keyfile DNS rewrite" || fail "NM keyfile DNS rewrite" "dns not rewritten"
else
    fail "NM DNS handler execution" "rhel-handler.sh returned non-zero"
fi

# Test 3.3: NM keyfile idempotency
echo ""
echo "--- Test 3.3: NM keyfile idempotency ---"

if run_rhel_nm_handler "${TEST_TMPDIR}/nm-test.img"; then
    RESULT=$(fixture_cat "${TEST_TMPDIR}/nm-test.img" /etc/NetworkManager/system-connections/eth0.nmconnection)
    echo "${RESULT}" | grep -q 'address1=10.0.2.100/24,10.0.2.1' && pass "NM keyfile idempotency" || fail "NM keyfile idempotency" "address1 changed on second run"
else
    fail "NM keyfile idempotency" "handler failed on second run"
fi

# =========================================================================
# Phase 4: Windows handler tests
# =========================================================================
echo ""
echo "================================================================"
echo "Phase 4: Windows handler tests"
echo "================================================================"

run_windows_handler() {
    local disk="$1"
    (
        trap - EXIT
        export REWRITE_DISK="${disk}"
        export REWRITE_OS_NAME="windows"
        export REWRITE_OS_DISTRO="windows"
        export REWRITE_OS_MAJOR="10"
        export REWRITE_OS_MINOR="0"
        export REWRITE_OS_PRODUCT="Windows Server 2022 Standard"
        export REWRITE_IFACE_COUNT="1"
        export REWRITE_IFACE_0="eth0"
        export REWRITE_IP_0="10.0.2.100"
        export REWRITE_PREFIX_0="24"
        export REWRITE_GATEWAY_0="10.0.2.1"
        export REWRITE_DNS="${2:-}"
        source "${HANDLER_DIR}/windows-handler.sh"
    )
}

# Test 4.1: Windows registry IP rewrite
echo ""
echo "--- Test 4.1: Windows registry IP/mask/gateway rewrite ---"
cp "${WINDOWS_IMG}" "${TEST_TMPDIR}/win-test.img"

if run_windows_handler "${TEST_TMPDIR}/win-test.img"; then
    win_ok=true

    DHCP_VAL=$(win_hive_dword "${TEST_TMPDIR}/win-test.img" EnableDHCP)
    if [[ "${DHCP_VAL}" != "0" ]]; then
        fail "Windows EnableDHCP" "EnableDHCP not 0 (got: ${DHCP_VAL})"
        win_ok=false
    fi

    IP_RAW=$(win_hive_utf8 "${TEST_TMPDIR}/win-test.img" IPAddress)
    if ! echo "${IP_RAW}" | grep -q '10.0.2.100'; then
        fail "Windows IPAddress rewrite" "IPAddress not rewritten to 10.0.2.100 (got: ${IP_RAW})"
        win_ok=false
    fi

    MASK_RAW=$(win_hive_utf8 "${TEST_TMPDIR}/win-test.img" SubnetMask)
    if ! echo "${MASK_RAW}" | grep -q '255.255.255.0'; then
        fail "Windows SubnetMask rewrite" "SubnetMask not 255.255.255.0 (got: ${MASK_RAW})"
        win_ok=false
    fi

    GW_RAW=$(win_hive_utf8 "${TEST_TMPDIR}/win-test.img" DefaultGateway)
    if ! echo "${GW_RAW}" | grep -q '10.0.2.1'; then
        fail "Windows DefaultGateway rewrite" "DefaultGateway not 10.0.2.1 (got: ${GW_RAW})"
        win_ok=false
    fi

    if [[ "${win_ok}" == "true" ]]; then
        pass "Windows registry IP/mask/gateway rewrite"
    fi
else
    fail "Windows handler execution" "windows-handler.sh returned non-zero"
fi

# Test 4.2: Windows DNS rewrite
echo ""
echo "--- Test 4.2: Windows DNS rewrite ---"
cp "${WINDOWS_IMG}" "${TEST_TMPDIR}/win-dns-test.img"

if run_windows_handler "${TEST_TMPDIR}/win-dns-test.img" "10.0.2.10,10.0.2.11"; then
    DNS_RAW=$(win_hive_utf8 "${TEST_TMPDIR}/win-dns-test.img" NameServer)
    if echo "${DNS_RAW}" | grep -q '10.0.2.10,10.0.2.11'; then
        pass "Windows DNS rewrite"
    else
        fail "Windows DNS rewrite" "NameServer not rewritten (got: ${DNS_RAW})"
    fi
else
    fail "Windows DNS handler execution" "windows-handler.sh returned non-zero"
fi

# Test 4.3: Windows idempotency
echo ""
echo "--- Test 4.3: Windows idempotency ---"

if run_windows_handler "${TEST_TMPDIR}/win-test.img"; then
    IP_RAW=$(win_hive_utf8 "${TEST_TMPDIR}/win-test.img" IPAddress)
    if echo "${IP_RAW}" | grep -q '10.0.2.100'; then
        pass "Windows idempotency"
    else
        fail "Windows idempotency" "IPAddress changed on second run (got: ${IP_RAW})"
    fi
else
    fail "Windows idempotency" "handler failed on second run"
fi

# =========================================================================
# Summary
# =========================================================================
echo ""
echo "================================================================"
echo "Test Summary"
echo "================================================================"
echo -e "Passed: ${GREEN}${PASSED}${NC}"
echo -e "Failed: ${RED}${FAILED}${NC}"
echo "Total:  ${TOTAL}"
echo "================================================================"

[[ "${FAILED}" -eq 0 ]] || exit 1
