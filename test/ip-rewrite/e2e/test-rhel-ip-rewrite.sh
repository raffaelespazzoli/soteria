#!/usr/bin/env bash
#
# E2E Test: RHEL 9 IP Rewrite via Guest Agent Verification (AC1)
#
# Validates that a RHEL 9 VM annotated with a new IP actually boots with
# that IP after the ip-rewrite init container modifies the guest disk.
#
# Sequence:
#   1. Apply RHEL 9 VM manifest → start → verify initial IP
#   2. Stop VM → annotate + label for IP rewrite → start
#   3. Verify init container completed (exit code 0)
#   4. Verify guest agent reports the new IP
#   5. Clean up
#
# Prerequisites:
#   - OCP Virt cluster with webhook chart installed
#   - RHEL 9 image available (see manifests/rhel9-test-vm.yaml)
#   - virtctl, kubectl, jq installed
#
# Usage:
#   bash test-rhel-ip-rewrite.sh
#
# Configuration via environment variables (see lib/helpers.sh for defaults).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/helpers.sh"

require_tools

PASSED=0
FAILED=0

pass() { PASSED=$((PASSED + 1)); log_pass "$1"; }
fail() { FAILED=$((FAILED + 1)); log_fail "$1: $2"; }

cleanup() {
    if [[ "${SKIP_CLEANUP}" == "true" ]]; then
        log_warn "SKIP_CLEANUP=true — leaving resources in place"
        return
    fi
    log_info "Cleaning up RHEL test resources"
    virtctl stop "${RHEL_VM_NAME}" -n "${E2E_NAMESPACE}" 2>/dev/null || true
    remove_ip_rewrite_template_patch "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" 2>/dev/null || true
}
trap cleanup EXIT

# =========================================================================
# Step 1: Apply VM manifest and start with initial IP (no label)
# =========================================================================
log_info "=== RHEL 9 IP Rewrite E2E Test ==="
log_info "Step 1: Applying RHEL 9 VM manifest and starting VM"

ensure_namespace "${E2E_NAMESPACE}"

if ! kubectl get vm "${RHEL_VM_NAME}" -n "${E2E_NAMESPACE}" &>/dev/null; then
    export VM_NAME="${RHEL_VM_NAME}" DV_ACCESS_MODE="${RHEL_DV_ACCESS_MODE:-ReadWriteMany}"
    envsubst < "${SCRIPT_DIR}/manifests/rhel9-test-vm.yaml" \
        | kubectl apply -n "${E2E_NAMESPACE}" -f -
fi

# Wait for the DataVolume import to complete before starting
DV_NAME="${RHEL_VM_NAME}-dv"
if kubectl get dv "${DV_NAME}" -n "${E2E_NAMESPACE}" &>/dev/null; then
    wait_for_dv_ready "${DV_NAME}" "${E2E_NAMESPACE}" 600
fi

# Start the VM (running: false in manifest)
safe_start_vm "${RHEL_VM_NAME}" "${E2E_NAMESPACE}"
wait_for_vmi_running "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" "${RHEL_BOOT_TIMEOUT}"

# =========================================================================
# Step 2: Verify initial IP and no init container (AC4 implicit)
# =========================================================================
log_info "Step 2: Verifying initial IP and no ip-rewrite init container"

if verify_no_init_container "${RHEL_VM_NAME}" "${E2E_NAMESPACE}"; then
    pass "No ip-rewrite init container on initial boot (AC4 implicit)"
else
    fail "Initial boot check" "ip-rewrite init container found on initial boot without label"
fi

if wait_for_guest_agent_ip "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" "${RHEL_INITIAL_IP}" "${GUEST_AGENT_TIMEOUT}"; then
    pass "Initial IP ${RHEL_INITIAL_IP} confirmed via guest agent"
else
    fail "Initial IP verification" "Guest agent did not report initial IP ${RHEL_INITIAL_IP}"
    log_error "Cannot continue without confirmed initial IP"
    exit 1
fi

# =========================================================================
# Step 3: Stop VM, annotate + label, start again
# =========================================================================
log_info "Step 3: Stopping VM for IP rewrite"

virtctl stop "${RHEL_VM_NAME}" -n "${E2E_NAMESPACE}"
wait_for_vmi_deleted "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" "${VM_STOP_TIMEOUT}"

# Remove cloud-init volume so it does not override the rewritten IP on boot
remove_cloud_init_volume "${RHEL_VM_NAME}" "${E2E_NAMESPACE}"

log_info "Step 3: Patching VM template metadata with IP rewrite label and annotations"
apply_ip_rewrite_template_patch "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" "${RHEL_TARGET_ANNOTATION}"

log_info "Step 3: Starting VM with IP rewrite"
safe_start_vm "${RHEL_VM_NAME}" "${E2E_NAMESPACE}"
wait_for_vmi_running "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" "${RHEL_BOOT_TIMEOUT}"

# =========================================================================
# Step 4: Verify init container completed
# =========================================================================
log_info "Step 4: Verifying ip-rewrite init container completed"

if verify_init_container_completed "${RHEL_VM_NAME}" "${E2E_NAMESPACE}"; then
    pass "ip-rewrite init container completed successfully"
else
    fail "Init container verification" "ip-rewrite init container did not complete successfully"
fi

# Verify the virt-launcher pod actually has the label and annotations
if verify_pod_label "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" "soteria.io/ip-rewrite" "true"; then
    pass "Virt-launcher pod has soteria.io/ip-rewrite=true label"
else
    fail "Pod label check" "Virt-launcher pod missing soteria.io/ip-rewrite label"
fi

# =========================================================================
# Step 5: Verify rewritten IP via guest agent
# =========================================================================
log_info "Step 5: Verifying rewritten IP via guest agent"

if wait_for_guest_agent_ip "${RHEL_VM_NAME}" "${E2E_NAMESPACE}" "${RHEL_TARGET_IP}" "${GUEST_AGENT_TIMEOUT}"; then
    pass "Rewritten IP ${RHEL_TARGET_IP} confirmed via guest agent (AC1)"
else
    fail "Rewritten IP verification (AC1)" "Guest agent did not report IP ${RHEL_TARGET_IP}"
fi

# =========================================================================
# Summary
# =========================================================================
echo ""
log_info "=== RHEL 9 IP Rewrite E2E Results ==="
echo -e "Passed: ${GREEN}${PASSED}${NC}"
echo -e "Failed: ${RED}${FAILED}${NC}"

[[ "${FAILED}" -eq 0 ]] || exit 1
