#!/usr/bin/env bash
#
# E2E Test: Live Migration Does Not Trigger IP Rewrite (AC3)
#
# Validates that when a VM with IP rewrite label and annotations undergoes
# live migration, the migration target virt-launcher pod does NOT have an
# ip-rewrite init container, and the migration completes successfully.
#
# The webhook handler checks for the kubevirt.io/migrationJobUID label on
# the pod (set by KubeVirt's RenderMigrationManifest) and skips injection.
#
# Sequence:
#   1. Ensure a VM with IP rewrite label + annotations is running
#   2. Trigger live migration via VirtualMachineInstanceMigration CR
#   3. Wait for migration to complete
#   4. Verify migration target pod has NO ip-rewrite init container
#   5. Clean up migration CR
#
# Prerequisites:
#   - OCP Virt cluster with webhook chart installed
#   - At least 2 schedulable nodes (live migration requires a target node)
#   - A running VM with soteria.io/ip-rewrite=true label
#   - virtctl, kubectl, jq installed
#
# Usage:
#   bash test-migration-skip.sh
#
# Configuration via environment variables (see lib/helpers.sh for defaults).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/helpers.sh"

require_tools

: "${MIGRATION_VM_NAME:=${RHEL_VM_NAME}}"
: "${MIGRATION_CR_NAME:=e2e-test-migration}"

PASSED=0
FAILED=0

pass() { PASSED=$((PASSED + 1)); log_pass "$1"; }
fail() { FAILED=$((FAILED + 1)); log_fail "$1: $2"; }

cleanup() {
    if [[ "${SKIP_CLEANUP}" == "true" ]]; then
        log_warn "SKIP_CLEANUP=true — leaving resources in place"
        return
    fi
    log_info "Cleaning up migration test resources"
    kubectl delete vmim "${MIGRATION_CR_NAME}" -n "${E2E_NAMESPACE}" 2>/dev/null || true
}
trap cleanup EXIT

# =========================================================================
# Step 1: Ensure VM is running with label and annotations
# =========================================================================
log_info "=== Migration Skip E2E Test (AC3) ==="
log_info "Step 1: Ensuring VM ${MIGRATION_VM_NAME} is running with IP rewrite label"

ensure_namespace "${E2E_NAMESPACE}"

# Ensure the VM has the label and annotation
kubectl label vm "${MIGRATION_VM_NAME}" -n "${E2E_NAMESPACE}" \
    soteria.io/ip-rewrite=true --overwrite
kubectl annotate vm "${MIGRATION_VM_NAME}" -n "${E2E_NAMESPACE}" \
    "soteria.io/eth0-ip=${RHEL_TARGET_ANNOTATION}" --overwrite

# If VM is not running, start it
if ! kubectl get vmi "${MIGRATION_VM_NAME}" -n "${E2E_NAMESPACE}" &>/dev/null; then
    log_info "VM is not running — starting it"
    virtctl start "${MIGRATION_VM_NAME}" -n "${E2E_NAMESPACE}" 2>/dev/null || true
fi
wait_for_vmi_running "${MIGRATION_VM_NAME}" "${E2E_NAMESPACE}" "${RHEL_BOOT_TIMEOUT}"

# =========================================================================
# Step 2: Trigger live migration
# =========================================================================
log_info "Step 2: Triggering live migration"

# Delete any stale migration CR
kubectl delete vmim "${MIGRATION_CR_NAME}" -n "${E2E_NAMESPACE}" 2>/dev/null || true

# Apply migration CR from template with substituted values
sed -e "s/MIGRATION_NAME/${MIGRATION_CR_NAME}/g" \
    -e "s/NAMESPACE/${E2E_NAMESPACE}/g" \
    -e "s/VMI_NAME/${MIGRATION_VM_NAME}/g" \
    "${SCRIPT_DIR}/manifests/migration-cr.yaml" | kubectl apply -f -

# =========================================================================
# Step 3: Wait for migration to complete
# =========================================================================
log_info "Step 3: Waiting for migration to complete (timeout: ${MIGRATION_TIMEOUT}s)"

if kubectl wait vmim "${MIGRATION_CR_NAME}" -n "${E2E_NAMESPACE}" \
        --for=jsonpath='{.status.phase}'=Succeeded --timeout="${MIGRATION_TIMEOUT}s"; then
    pass "Live migration completed successfully"
else
    fail "Migration completion" "Migration did not reach Succeeded phase within ${MIGRATION_TIMEOUT}s"
    # Dump migration status for debugging
    kubectl get vmim "${MIGRATION_CR_NAME}" -n "${E2E_NAMESPACE}" -o yaml 2>&1 || true
    exit 1
fi

# =========================================================================
# Step 4: Verify no ip-rewrite init container on migration target pod
# =========================================================================
log_info "Step 4: Verifying migration target pod has no ip-rewrite init container"

# After migration, the "current" virt-launcher pod is the migration target.
# The original pod will have been terminated or be in a completed state.
# Get the active (non-completed) virt-launcher pod.
target_pod=$(kubectl get pods -n "${E2E_NAMESPACE}" \
    -l "kubevirt.io/vm=${MIGRATION_VM_NAME}" \
    --field-selector=status.phase=Running \
    --sort-by=.metadata.creationTimestamp \
    -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null)

if [[ -z "${target_pod}" ]]; then
    fail "Migration target pod lookup" "Could not find running virt-launcher pod after migration"
    exit 1
fi

log_info "Migration target pod: ${target_pod}"

init_names=$(kubectl get pod "${target_pod}" -n "${E2E_NAMESPACE}" \
    -o jsonpath='{.spec.initContainers[*].name}' 2>/dev/null)

if echo "${init_names}" | grep -q "ip-rewrite"; then
    fail "Migration skip (AC3)" "ip-rewrite init container found on migration target pod ${target_pod}"
else
    pass "Migration target pod has no ip-rewrite init container (AC3)"
fi

# =========================================================================
# Summary
# =========================================================================
echo ""
log_info "=== Migration Skip E2E Results ==="
echo -e "Passed: ${GREEN}${PASSED}${NC}"
echo -e "Failed: ${RED}${FAILED}${NC}"

[[ "${FAILED}" -eq 0 ]] || exit 1
