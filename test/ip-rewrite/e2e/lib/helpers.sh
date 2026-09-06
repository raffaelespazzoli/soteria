#!/usr/bin/env bash
#
# Shared helper functions for IP rewrite E2E tests.
#
# Source this file from individual test scripts:
#   SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   source "${SCRIPT_DIR}/lib/helpers.sh"

set -euo pipefail

# ---------------------------------------------------------------------------
# Colours (disabled if stdout is not a terminal)
# ---------------------------------------------------------------------------
if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

# ---------------------------------------------------------------------------
# Default configuration — override via environment variables
# ---------------------------------------------------------------------------
: "${E2E_NAMESPACE:=ip-rewrite-e2e}"
: "${WEBHOOK_NAMESPACE:=soteria-ip-rewrite}"

: "${RHEL_VM_NAME:=rhel9-ip-rewrite-test}"
: "${WINDOWS_VM_NAME:=win2022-ip-rewrite-test}"

: "${RHEL_INITIAL_IP:=10.0.1.50}"
: "${RHEL_TARGET_IP:=10.0.2.100}"
: "${RHEL_TARGET_ANNOTATION:=10.0.2.100/24;10.0.2.1}"

: "${WINDOWS_INITIAL_IP:=10.0.1.60}"
: "${WINDOWS_TARGET_IP:=10.0.2.110}"
: "${WINDOWS_TARGET_ANNOTATION:=10.0.2.110/24;10.0.2.1}"

: "${RHEL_BOOT_TIMEOUT:=120}"
: "${WINDOWS_BOOT_TIMEOUT:=360}"
: "${GUEST_AGENT_TIMEOUT:=300}"
: "${GUEST_AGENT_POLL_INTERVAL:=10}"
: "${MIGRATION_TIMEOUT:=300}"
: "${VM_STOP_TIMEOUT:=120}"

: "${SKIP_CLEANUP:=false}"

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
log_info()  { echo -e "${GREEN}[INFO]${NC}  $(date -u '+%H:%M:%S') $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date -u '+%H:%M:%S') $*" >&2; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date -u '+%H:%M:%S') $*" >&2; }
log_pass()  { echo -e "${GREEN}[PASS]${NC}  $*"; }
log_fail()  { echo -e "${RED}[FAIL]${NC}  $*"; }

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
require_tools() {
    local missing=()
    for tool in kubectl virtctl jq; do
        if ! command -v "${tool}" &>/dev/null; then
            missing+=("${tool}")
        fi
    done
    if [[ ${#missing[@]} -gt 0 ]]; then
        log_error "Missing required tools: ${missing[*]}"
        log_error "Install them before running E2E tests."
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# VM lifecycle helpers
# ---------------------------------------------------------------------------

# Wait for a VMI to reach the Running phase.
wait_for_vmi_running() {
    local vm="$1" ns="$2" timeout="${3:-${RHEL_BOOT_TIMEOUT}}"
    log_info "Waiting for VMI ${vm} to reach Running (timeout: ${timeout}s)"
    if ! kubectl wait vmi "${vm}" -n "${ns}" \
            --for=jsonpath='{.status.phase}'=Running --timeout="${timeout}s"; then
        log_error "VMI ${vm} did not reach Running within ${timeout}s"
        return 1
    fi
}

# Wait for a VMI to be fully deleted (after virtctl stop).
wait_for_vmi_deleted() {
    local vm="$1" ns="$2" timeout="${3:-${VM_STOP_TIMEOUT}}"
    log_info "Waiting for VMI ${vm} to be deleted (timeout: ${timeout}s)"
    local end=$((SECONDS + timeout))
    while [[ ${SECONDS} -lt ${end} ]]; do
        if ! kubectl get vmi "${vm}" -n "${ns}" &>/dev/null; then
            log_info "VMI ${vm} deleted"
            return 0
        fi
        sleep 5
    done
    log_error "VMI ${vm} was not deleted within ${timeout}s"
    return 1
}

# ---------------------------------------------------------------------------
# Guest agent helpers
# ---------------------------------------------------------------------------

# Wait for the QEMU guest agent to report a specific IP address.
wait_for_guest_agent_ip() {
    local vm="$1" ns="$2" expected_ip="$3" timeout="${4:-${GUEST_AGENT_TIMEOUT}}"
    log_info "Waiting for guest agent to report IP ${expected_ip} on ${vm} (timeout: ${timeout}s)"
    local end=$((SECONDS + timeout))
    while [[ ${SECONDS} -lt ${end} ]]; do
        local ips
        ips=$(virtctl guestosinfo "${vm}" -n "${ns}" -o json 2>/dev/null \
            | jq -r '.interfaces[]?.ipAddresses[]? // empty' 2>/dev/null) || true
        if echo "${ips}" | grep -qF "${expected_ip}"; then
            log_info "Guest agent reports IP: ${expected_ip}"
            return 0
        fi
        sleep "${GUEST_AGENT_POLL_INTERVAL}"
    done
    log_error "Guest agent did not report IP ${expected_ip} within ${timeout}s"
    log_error "Last guest agent output:"
    virtctl guestosinfo "${vm}" -n "${ns}" -o json 2>&1 || true
    return 1
}

# ---------------------------------------------------------------------------
# Init container verification
# ---------------------------------------------------------------------------

# Find the virt-launcher pod for a VM.
get_virt_launcher_pod() {
    local vm="$1" ns="$2"
    kubectl get pods -n "${ns}" \
        -l "kubevirt.io/vm=${vm}" \
        --field-selector=status.phase!=Succeeded \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null
}

# Verify the ip-rewrite init container exists and completed with exit code 0.
verify_init_container_completed() {
    local vm="$1" ns="$2"
    local pod
    pod=$(get_virt_launcher_pod "${vm}" "${ns}")
    if [[ -z "${pod}" ]]; then
        log_fail "No virt-launcher pod found for VM ${vm}"
        return 1
    fi

    local exit_code
    exit_code=$(kubectl get pod "${pod}" -n "${ns}" \
        -o jsonpath='{.status.initContainerStatuses[?(@.name=="ip-rewrite")].state.terminated.exitCode}' 2>/dev/null)
    if [[ "${exit_code}" != "0" ]]; then
        log_fail "ip-rewrite init container exit code: ${exit_code:-not found}"
        kubectl get pod "${pod}" -n "${ns}" \
            -o jsonpath='{.status.initContainerStatuses}' 2>/dev/null || true
        return 1
    fi

    log_pass "ip-rewrite init container completed (exit code 0)"
    return 0
}

# Verify NO ip-rewrite init container exists on the virt-launcher pod.
verify_no_init_container() {
    local vm="$1" ns="$2"
    local pod
    pod=$(get_virt_launcher_pod "${vm}" "${ns}")
    if [[ -z "${pod}" ]]; then
        log_fail "No virt-launcher pod found for VM ${vm}"
        return 1
    fi

    local init_names
    init_names=$(kubectl get pod "${pod}" -n "${ns}" \
        -o jsonpath='{.spec.initContainers[*].name}' 2>/dev/null)
    if echo "${init_names}" | grep -q "ip-rewrite"; then
        log_fail "ip-rewrite init container found on pod ${pod} (should not be present)"
        return 1
    fi

    log_pass "No ip-rewrite init container on pod ${pod}"
    return 0
}

# ---------------------------------------------------------------------------
# Namespace helpers
# ---------------------------------------------------------------------------

# Ensure the E2E test namespace exists.
ensure_namespace() {
    local ns="$1"
    if ! kubectl get namespace "${ns}" &>/dev/null; then
        log_info "Creating namespace ${ns}"
        kubectl create namespace "${ns}"
    fi
}
