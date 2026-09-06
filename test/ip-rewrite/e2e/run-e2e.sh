#!/usr/bin/env bash
#
# IP Rewrite E2E Test Runner
#
# Orchestrates all E2E tests for the IP rewrite feature and produces a
# pass/fail summary. Designed for manual execution on an OCP Virt cluster.
#
# Tests executed:
#   1. RHEL 9 IP rewrite (AC1 + AC4 implicit)
#   2. Windows Server 2022 IP rewrite (AC2 + AC4 implicit)
#   3. Live migration skip (AC3)
#   4. Webhook fail-open (AC5)
#
# Prerequisites: see README.md for full list.
#
# Usage:
#   bash run-e2e.sh                    # Run all tests
#   bash run-e2e.sh --skip-windows     # Skip Windows tests (no licensed image)
#   bash run-e2e.sh --test <name>      # Run a single test (rhel|windows|migration|failopen)
#
# Environment variables: see lib/helpers.sh for full list.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/helpers.sh"

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
SKIP_WINDOWS=false
RUN_SINGLE=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-windows)
            SKIP_WINDOWS=true
            shift
            ;;
        --test)
            if [[ $# -lt 2 || "$2" == -* ]]; then
                log_error "--test requires an argument: rhel, windows, migration, failopen"
                exit 1
            fi
            RUN_SINGLE="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: run-e2e.sh [--skip-windows] [--test <name>]"
            echo ""
            echo "Options:"
            echo "  --skip-windows     Skip the Windows Server 2022 test (AC2)"
            echo "  --test <name>      Run a single test: rhel, windows, migration, failopen"
            echo "  -h, --help         Show this help"
            echo ""
            echo "Environment variables:"
            echo "  E2E_NAMESPACE          Test namespace (default: ip-rewrite-e2e)"
            echo "  WEBHOOK_NAMESPACE      Webhook chart namespace (default: soteria-ip-rewrite)"
            echo "  RHEL_VM_NAME           RHEL 9 VM name (default: rhel9-ip-rewrite-test)"
            echo "  WINDOWS_VM_NAME        Windows VM name (default: win2022-ip-rewrite-test)"
            echo "  SKIP_CLEANUP           Keep test resources after run (default: false)"
            echo "  GUEST_AGENT_TIMEOUT    Guest agent wait timeout in seconds (default: 300)"
            echo ""
            echo "See lib/helpers.sh for the complete list of configuration variables."
            exit 0
            ;;
        *)
            log_error "Unknown argument: $1"
            exit 1
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
require_tools

echo "================================================================"
echo "  IP Rewrite E2E Test Suite"
echo "================================================================"
echo ""
log_info "Namespace:          ${E2E_NAMESPACE}"
log_info "Webhook namespace:  ${WEBHOOK_NAMESPACE}"
log_info "RHEL VM:            ${RHEL_VM_NAME}"
log_info "Windows VM:         ${WINDOWS_VM_NAME}"
log_info "Skip Windows:       ${SKIP_WINDOWS}"
log_info "Skip cleanup:       ${SKIP_CLEANUP}"
echo ""

# ---------------------------------------------------------------------------
# Test execution
# ---------------------------------------------------------------------------
SUITE_PASSED=0
SUITE_FAILED=0
SUITE_SKIPPED=0

run_test() {
    local name="$1" script="$2"
    echo ""
    echo "================================================================"
    echo "  Test: ${name}"
    echo "================================================================"
    if bash "${script}"; then
        SUITE_PASSED=$((SUITE_PASSED + 1))
        log_pass "Suite: ${name}"
    else
        SUITE_FAILED=$((SUITE_FAILED + 1))
        log_fail "Suite: ${name}"
    fi
}

skip_test() {
    local name="$1" reason="$2"
    SUITE_SKIPPED=$((SUITE_SKIPPED + 1))
    echo ""
    echo -e "${YELLOW}[SKIP]${NC} ${name}: ${reason}"
}

should_run() {
    local name="$1"
    [[ -z "${RUN_SINGLE}" ]] || [[ "${RUN_SINGLE}" == "${name}" ]]
}

# --- AC1: RHEL 9 IP rewrite ---
if should_run "rhel"; then
    run_test "RHEL 9 IP Rewrite (AC1)" "${SCRIPT_DIR}/test-rhel-ip-rewrite.sh"
fi

# --- AC2: Windows Server 2022 IP rewrite ---
if should_run "windows"; then
    if [[ "${SKIP_WINDOWS}" == "true" ]]; then
        skip_test "Windows Server 2022 IP Rewrite (AC2)" "Skipped via --skip-windows"
    else
        run_test "Windows Server 2022 IP Rewrite (AC2)" "${SCRIPT_DIR}/test-windows-ip-rewrite.sh"
    fi
fi

# --- AC3: Migration skip ---
if should_run "migration"; then
    run_test "Migration Skip (AC3)" "${SCRIPT_DIR}/test-migration-skip.sh"
fi

# --- AC5: Webhook fail-open ---
if should_run "failopen"; then
    run_test "Webhook Fail-Open (AC5)" "${SCRIPT_DIR}/test-webhook-failopen.sh"
fi

# Catch unknown --test names
if [[ -n "${RUN_SINGLE}" ]] && \
   [[ "${RUN_SINGLE}" != "rhel" ]] && \
   [[ "${RUN_SINGLE}" != "windows" ]] && \
   [[ "${RUN_SINGLE}" != "migration" ]] && \
   [[ "${RUN_SINGLE}" != "failopen" ]]; then
    log_error "Unknown test name: '${RUN_SINGLE}'. Valid names: rhel, windows, migration, failopen"
    SUITE_FAILED=$((SUITE_FAILED + 1))
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
SUITE_TOTAL=$((SUITE_PASSED + SUITE_FAILED + SUITE_SKIPPED))

echo ""
echo "================================================================"
echo "  E2E Test Suite Summary"
echo "================================================================"
echo -e "  Passed:  ${GREEN}${SUITE_PASSED}${NC}"
echo -e "  Failed:  ${RED}${SUITE_FAILED}${NC}"
echo -e "  Skipped: ${YELLOW}${SUITE_SKIPPED}${NC}"
echo "  Total:   ${SUITE_TOTAL}"
echo "================================================================"

[[ "${SUITE_FAILED}" -eq 0 ]] || exit 1
