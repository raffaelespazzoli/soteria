#!/usr/bin/env bash
#
# E2E Test: Webhook Fail-Open — VM Starts When Webhook Is Down (AC5)
#
# Validates that when the IP rewrite webhook deployment is scaled to 0,
# VMs with the soteria.io/ip-rewrite label still start successfully
# (failurePolicy: Ignore on the MutatingWebhookConfiguration), and no
# ip-rewrite init container is injected.
#
# Sequence:
#   1. Record current webhook replica count
#   2. Scale webhook deployment to 0
#   3. Ensure VM is stopped, then start it with IP rewrite label
#   4. Verify VM starts successfully without ip-rewrite init container
#   5. Restore webhook replicas
#
# Prerequisites:
#   - OCP Virt cluster with webhook chart installed
#   - virtctl, kubectl, jq installed
#
# Usage:
#   bash test-webhook-failopen.sh
#
# Configuration via environment variables (see lib/helpers.sh for defaults).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/helpers.sh"

require_tools

: "${FAILOPEN_VM_NAME:=${RHEL_VM_NAME}}"
: "${WEBHOOK_DEPLOYMENT_LABEL:=app.kubernetes.io/name=soteria-ip-rewrite}"

PASSED=0
FAILED=0
ORIGINAL_REPLICAS=""

pass() { PASSED=$((PASSED + 1)); log_pass "$1"; }
fail() { FAILED=$((FAILED + 1)); log_fail "$1: $2"; }

cleanup() {
    # Always restore the webhook, even on failure
    if [[ -n "${ORIGINAL_REPLICAS}" ]]; then
        log_info "Restoring webhook replicas to ${ORIGINAL_REPLICAS}"
        kubectl scale deployment -n "${WEBHOOK_NAMESPACE}" \
            -l "${WEBHOOK_DEPLOYMENT_LABEL}" --replicas="${ORIGINAL_REPLICAS}" 2>/dev/null || true
    fi
    if [[ "${SKIP_CLEANUP}" == "true" ]]; then
        log_warn "SKIP_CLEANUP=true — leaving VM in place"
        return
    fi
    virtctl stop "${FAILOPEN_VM_NAME}" -n "${E2E_NAMESPACE}" 2>/dev/null || true
}
trap cleanup EXIT

# =========================================================================
# Step 1: Record original webhook replica count
# =========================================================================
log_info "=== Webhook Fail-Open E2E Test (AC5) ==="
log_info "Step 1: Recording current webhook replica count"

ensure_namespace "${E2E_NAMESPACE}"

ORIGINAL_REPLICAS=$(kubectl get deployment -n "${WEBHOOK_NAMESPACE}" \
    -l "${WEBHOOK_DEPLOYMENT_LABEL}" \
    -o jsonpath='{.items[0].spec.replicas}' 2>/dev/null)

if [[ -z "${ORIGINAL_REPLICAS}" ]]; then
    log_error "Could not find webhook deployment in namespace ${WEBHOOK_NAMESPACE} with label ${WEBHOOK_DEPLOYMENT_LABEL}"
    log_error "Is the IP rewrite webhook chart installed?"
    exit 1
fi

log_info "Current webhook replicas: ${ORIGINAL_REPLICAS}"

# =========================================================================
# Step 2: Scale webhook to 0
# =========================================================================
log_info "Step 2: Scaling webhook deployment to 0 replicas"

kubectl scale deployment -n "${WEBHOOK_NAMESPACE}" \
    -l "${WEBHOOK_DEPLOYMENT_LABEL}" --replicas=0

# Wait for pods to terminate
log_info "Waiting for webhook pods to terminate"
local_timeout=60
end=$((SECONDS + local_timeout))
while [[ ${SECONDS} -lt ${end} ]]; do
    pod_count=$(kubectl get pods -n "${WEBHOOK_NAMESPACE}" \
        -l "${WEBHOOK_DEPLOYMENT_LABEL}" \
        --field-selector=status.phase=Running \
        -o name 2>/dev/null | wc -l)
    if [[ "${pod_count}" -eq 0 ]]; then
        log_info "All webhook pods terminated"
        break
    fi
    sleep 5
done

# =========================================================================
# Step 3: Ensure VM is stopped, label it, then start
# =========================================================================
log_info "Step 3: Preparing and starting VM with IP rewrite label (webhook down)"

# Stop VM if running
if kubectl get vmi "${FAILOPEN_VM_NAME}" -n "${E2E_NAMESPACE}" &>/dev/null; then
    virtctl stop "${FAILOPEN_VM_NAME}" -n "${E2E_NAMESPACE}"
    wait_for_vmi_deleted "${FAILOPEN_VM_NAME}" "${E2E_NAMESPACE}" "${VM_STOP_TIMEOUT}"
fi

# Ensure the VM has the IP rewrite label
kubectl label vm "${FAILOPEN_VM_NAME}" -n "${E2E_NAMESPACE}" \
    soteria.io/ip-rewrite=true --overwrite
kubectl annotate vm "${FAILOPEN_VM_NAME}" -n "${E2E_NAMESPACE}" \
    "soteria.io/eth0-ip=${RHEL_TARGET_ANNOTATION}" --overwrite

# Start VM — should succeed because failurePolicy: Ignore
virtctl start "${FAILOPEN_VM_NAME}" -n "${E2E_NAMESPACE}"

# =========================================================================
# Step 4: Verify VM starts and has NO init container
# =========================================================================
log_info "Step 4: Verifying VM starts successfully without ip-rewrite init container"

if wait_for_vmi_running "${FAILOPEN_VM_NAME}" "${E2E_NAMESPACE}" "${RHEL_BOOT_TIMEOUT}"; then
    pass "VM started successfully with webhook unavailable (AC5)"
else
    fail "VM start with webhook down (AC5)" "VM did not reach Running state"
    exit 1
fi

if verify_no_init_container "${FAILOPEN_VM_NAME}" "${E2E_NAMESPACE}"; then
    pass "No ip-rewrite init container injected when webhook is down (AC5)"
else
    fail "Fail-open check (AC5)" "ip-rewrite init container was injected despite webhook being down"
fi

# =========================================================================
# Step 5: Restore webhook (handled by cleanup trap, but do it explicitly)
# =========================================================================
log_info "Step 5: Restoring webhook deployment to ${ORIGINAL_REPLICAS} replicas"

kubectl scale deployment -n "${WEBHOOK_NAMESPACE}" \
    -l "${WEBHOOK_DEPLOYMENT_LABEL}" --replicas="${ORIGINAL_REPLICAS}"

# Clear so cleanup trap does not double-restore
ORIGINAL_REPLICAS=""

log_info "Waiting for webhook pods to be ready"
kubectl rollout status deployment -n "${WEBHOOK_NAMESPACE}" \
    -l "${WEBHOOK_DEPLOYMENT_LABEL}" --timeout=120s 2>/dev/null || \
    log_warn "Webhook rollout did not complete within 120s — check manually"

# =========================================================================
# Summary
# =========================================================================
echo ""
log_info "=== Webhook Fail-Open E2E Results ==="
echo -e "Passed: ${GREEN}${PASSED}${NC}"
echo -e "Failed: ${RED}${FAILED}${NC}"

[[ "${FAILED}" -eq 0 ]] || exit 1
