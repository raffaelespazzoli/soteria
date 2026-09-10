#!/usr/bin/env bash
#
# ip-rewrite-test.sh
#
# Step 1.d of the ip-rewrite golden-image pipeline (after provision-test-vms.sh).
#
# Tests the IP rewrite webhook against VMs created by provision-test-vms.sh.
# It does not provision or clone disks — those VMs already have static IPs
# (cloud-init on RHEL, sysprep on Windows) on the L2 cUDN.
#
# Flow (one VM at a time — the cluster typically cannot run all guests):
#   1. Stop every test VM
#   2. For each VM:
#        a. Apply soteria.io/ip-rewrite label + IP/DNS annotations
#           on both VM metadata and spec.template.metadata
#        b. Start the VM and wait for the ip-rewrite init container
#        c. Wait for the guest agent to report the rewritten IP
#        d. Stop the VM
#
# Usage:
#   ./ip-rewrite-test.sh                         # All VMs
#   ./ip-rewrite-test.sh --only rhel7,win-11
#   ./ip-rewrite-test.sh --cleanup               # Remove rewrite labels only
#
# Prerequisites:
#   - kubectl/oc credentials for the target cluster (or KUBECONFIG set)
#   - IP rewrite webhook deployed
#   - Test VMs already provisioned (provision-test-vms.sh)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_KUBECONFIG="${SCRIPT_DIR}/../martin/kubeconfig-dr-poc-1"

# ~/.kube/config on this machine often has an expired token for the same
# cluster. Prefer an explicit KUBECONFIG, then the repo's martin kubeconfig.
if [[ -z "${KUBECONFIG:-}" && -f "${DEFAULT_KUBECONFIG}" ]]; then
  export KUBECONFIG="${DEFAULT_KUBECONFIG}"
fi

NAMESPACE="${NAMESPACE:-ip-rewrite-test}"
GATEWAY="${GATEWAY:-192.168.100.1}"
DNS_SERVER="${DNS_SERVER:-8.8.8.8}"

VM_READY_TIMEOUT="${VM_READY_TIMEOUT:-600}"
GUEST_AGENT_TIMEOUT="${GUEST_AGENT_TIMEOUT:-900}"
INIT_TIMEOUT="${INIT_TIMEOUT:-900}"
STOP_TIMEOUT="${STOP_TIMEOUT:-180}"

# vm_name  nic  initial_ip  rewrite_ip
declare -a VM_DEFS=(
  "rhel7-iprewrite-test            eth0      192.168.100.50  192.168.100.70"
  "rhel8-iprewrite-test            eth0      192.168.100.51  192.168.100.71"
  "rhel9-iprewrite-test            eth0      192.168.100.52  192.168.100.72"
  "rhel10-iprewrite-test           enp1s0    192.168.100.53  192.168.100.73"
  "win-server-2016-iprewrite-test  Ethernet  192.168.100.80  192.168.100.90"
  "win-server-2019-iprewrite-test  Ethernet  192.168.100.81  192.168.100.91"
  "win-server-2022-iprewrite-test  Ethernet  192.168.100.82  192.168.100.92"
  "win-server-2025-iprewrite-test  Ethernet  192.168.100.83  192.168.100.93"
  "win-11-iprewrite-test           Ethernet  192.168.100.84  192.168.100.94"
)

info()  { echo "[INFO]  $(date '+%H:%M:%S') $*"; }
warn()  { echo "[WARN]  $(date '+%H:%M:%S') $*" >&2; }
error() { echo "[ERROR] $(date '+%H:%M:%S') $*" >&2; }
fatal() { error "$@"; exit 1; }
pass()  { echo "[PASS]  $(date '+%H:%M:%S') $*"; }
fail()  { echo "[FAIL]  $(date '+%H:%M:%S') $*" >&2; FAILURES=$((FAILURES + 1)); }

FAILURES=0
TESTS=0
ONLY=()
CLEANUP_ONLY=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --only) IFS=',' read -ra ONLY <<< "$2"; shift 2 ;;
    --namespace) NAMESPACE="$2"; shift 2 ;;
    --cleanup) CLEANUP_ONLY=true; shift ;;
    -h|--help)
      sed -n '2,28p' "$0"
      exit 0
      ;;
    *) fatal "Unknown argument: $1" ;;
  esac
done

should_include() {
  local name="$1"
  if [[ ${#ONLY[@]} -eq 0 ]]; then
    return 0
  fi
  local token
  for token in "${ONLY[@]}"; do
    if [[ "${name}" == *"${token}"* ]]; then
      return 0
    fi
  done
  return 1
}

parse_def() {
  # shellcheck disable=SC2086
  read -r VM_NAME NIC INITIAL_IP REWRITE_IP <<< $1
}

guest_ip() {
  kubectl get vmi "${1}" -n "${NAMESPACE}" \
    -o jsonpath='{.status.interfaces[0].ipAddress}' 2>/dev/null || true
}

assert_ip() {
  local vm="$1" expected="$2" phase="$3"
  TESTS=$((TESTS + 1))
  local actual
  actual=$(guest_ip "${vm}")
  if [[ "${actual}" == "${expected}" ]]; then
    pass "${vm}: ${phase} IP = ${actual}"
  else
    fail "${vm}: ${phase} IP expected=${expected} actual=${actual:-none}"
  fi
}

wait_vm_status() {
  local vm="$1" want="$2" timeout="$3"
  local elapsed=0
  while (( elapsed < timeout )); do
    local status
    status=$(kubectl get vm "${vm}" -n "${NAMESPACE}" \
      -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")
    if [[ "${status}" == "${want}" ]]; then
      return 0
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done
  warn "${vm} did not reach ${want} within ${timeout}s (last: $(kubectl get vm "${vm}" -n "${NAMESPACE}" -o jsonpath='{.status.printableStatus}' 2>/dev/null))"
  return 1
}

stop_vm() {
  local vm="$1"
  # ACPI shutdown first so Windows NTFS is clean; force only if it hangs.
  virtctl stop "${vm}" -n "${NAMESPACE}" >/dev/null 2>&1 || true
  if wait_vm_status "${vm}" "Stopped" 60; then
    return 0
  fi
  warn "${vm}: ACPI stop timed out — forcing"
  virtctl stop "${vm}" -n "${NAMESPACE}" --force --grace-period=0 >/dev/null 2>&1 || true
  wait_vm_status "${vm}" "Stopped" "${STOP_TIMEOUT}" || true
}

start_vm() {
  virtctl start "${1}" -n "${NAMESPACE}" >/dev/null
}

wait_guest_ip() {
  local vm="$1" expected="$2" timeout="$3"
  local elapsed=0 ip
  while (( elapsed < timeout )); do
    ip=$(guest_ip "${vm}")
    if [[ -n "${ip}" && "${ip}" != 169.254.* ]]; then
      if [[ -z "${expected}" || "${ip}" == "${expected}" ]]; then
        echo "${ip}"
        return 0
      fi
    fi
    sleep 10
    elapsed=$((elapsed + 10))
  done
  guest_ip "${vm}"
  return 1
}

virt_launcher_pod() {
  kubectl get pod -n "${NAMESPACE}" -l "vm.kubevirt.io/name=${1}" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true
}

wait_init_container() {
  local vm="$1"
  local elapsed=0 pod reason
  while (( elapsed < 60 )); do
    pod=$(virt_launcher_pod "${vm}")
    if [[ -n "${pod}" ]]; then
      break
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  if [[ -z "${pod}" ]]; then
    error "${vm}: virt-launcher pod never appeared"
    return 1
  fi

  info "  Pod ${pod} — waiting for ip-rewrite init container"
  elapsed=0
  while (( elapsed < INIT_TIMEOUT )); do
    reason=$(kubectl get pod "${pod}" -n "${NAMESPACE}" \
      -o jsonpath='{.status.initContainerStatuses[?(@.name=="ip-rewrite")].state.terminated.reason}' \
      2>/dev/null || true)
    if [[ "${reason}" == "Completed" ]]; then
      info "  ip-rewrite init container completed"
      kubectl logs "${pod}" -n "${NAMESPACE}" -c ip-rewrite --tail=20 2>/dev/null || true
      return 0
    fi
    if [[ "${reason}" == "Error" ]]; then
      error "${vm}: ip-rewrite init container failed"
      kubectl logs "${pod}" -n "${NAMESPACE}" -c ip-rewrite --tail=40 2>/dev/null || true
      return 1
    fi
    local injected
    injected=$(kubectl get pod "${pod}" -n "${NAMESPACE}" \
      -o jsonpath='{.spec.initContainers[?(@.name=="ip-rewrite")].name}' 2>/dev/null || true)
    if [[ -z "${injected}" ]] && (( elapsed > 30 )); then
      error "${vm}: ip-rewrite init container was not injected (missing template label/annotation?)"
      return 1
    fi
    sleep 10
    elapsed=$((elapsed + 10))
  done
  error "${vm}: ip-rewrite init container timed out"
  kubectl logs "${pod}" -n "${NAMESPACE}" -c ip-rewrite --tail=40 2>/dev/null || true
  return 1
}

apply_rewrite() {
  local vm="$1" nic="$2" ip="$3"
  info "  Annotating ${vm}: ${nic}-ip → ${ip}/24;${GATEWAY}"

  kubectl label vm "${vm}" -n "${NAMESPACE}" soteria.io/ip-rewrite=true --overwrite >/dev/null

  kubectl patch vm "${vm}" -n "${NAMESPACE}" --type=merge -p "{
    \"metadata\": {
      \"labels\": {\"soteria.io/ip-rewrite\": \"true\"},
      \"annotations\": {
        \"soteria.io/dns\": \"${DNS_SERVER}\",
        \"soteria.io/${nic}-ip\": \"${ip}/24;${GATEWAY}\"
      }
    },
    \"spec\": {
      \"template\": {
        \"metadata\": {
          \"labels\": {\"soteria.io/ip-rewrite\": \"true\"},
          \"annotations\": {
            \"soteria.io/dns\": \"${DNS_SERVER}\",
            \"soteria.io/${nic}-ip\": \"${ip}/24;${GATEWAY}\"
          }
        }
      }
    }
  }" >/dev/null
}

remove_rewrite() {
  local vm="$1" nic="$2"
  kubectl label vm "${vm}" -n "${NAMESPACE}" soteria.io/ip-rewrite- --overwrite >/dev/null 2>&1 || true
  kubectl patch vm "${vm}" -n "${NAMESPACE}" --type=json -p "[
    {\"op\":\"remove\",\"path\":\"/metadata/labels/soteria.io~1ip-rewrite\"},
    {\"op\":\"remove\",\"path\":\"/spec/template/metadata/labels/soteria.io~1ip-rewrite\"}
  ]" >/dev/null 2>&1 || true
  kubectl annotate vm "${vm}" -n "${NAMESPACE}" \
    "soteria.io/${nic}-ip-" "soteria.io/dns-" --overwrite >/dev/null 2>&1 || true
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
if ! kubectl cluster-info >/dev/null; then
  fatal "Cannot reach cluster (unauthorized or unreachable). Export KUBECONFIG to a valid kubeconfig, e.g. hack/martin/kubeconfig-dr-poc-1"
fi
kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1 \
  || fatal "Namespace ${NAMESPACE} not found — run provision-test-vms.sh first"

if [[ "${CLEANUP_ONLY}" == "true" ]]; then
  info "Removing ip-rewrite labels/annotations (VMs are kept)"
  for def in "${VM_DEFS[@]}"; do
    parse_def "${def}"
    should_include "${VM_NAME}" || continue
    kubectl get vm "${VM_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1 || continue
    remove_rewrite "${VM_NAME}" "${NIC}"
    info "  ${VM_NAME}: rewrite labels cleared"
  done
  exit 0
fi

info "================================================================="
info "  IP rewrite test (existing VMs)"
info "================================================================="
info "Namespace: ${NAMESPACE}"
info "Gateway:   ${GATEWAY}"
info "DNS:       ${DNS_SERVER}"

missing=0
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  if ! kubectl get vm "${VM_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
    error "VM ${VM_NAME} not found — run provision-test-vms.sh"
    missing=$((missing + 1))
  fi
done
(( missing == 0 )) || fatal "${missing} test VM(s) missing"

info "Stopping all test VMs"
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  stop_vm "${VM_NAME}"
done

for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue

  echo ""
  info "=========================================="
  info "Testing ${VM_NAME}"
  info "  initial=${INITIAL_IP}  rewrite=${REWRITE_IP}  nic=${NIC}"
  info "=========================================="

  apply_rewrite "${VM_NAME}" "${NIC}" "${REWRITE_IP}"
  info "Starting ${VM_NAME}"
  start_vm "${VM_NAME}"

  if ! wait_vm_status "${VM_NAME}" "Running" "${VM_READY_TIMEOUT}"; then
    fail "${VM_NAME}: did not reach Running"
    kubectl get vm "${VM_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.printableStatus}{"\n"}' || true
    stop_vm "${VM_NAME}"
    continue
  fi

  if ! wait_init_container "${VM_NAME}"; then
    fail "${VM_NAME}: init container did not succeed"
    stop_vm "${VM_NAME}"
    continue
  fi

  actual=""
  if actual=$(wait_guest_ip "${VM_NAME}" "${REWRITE_IP}" "${GUEST_AGENT_TIMEOUT}"); then
    assert_ip "${VM_NAME}" "${REWRITE_IP}" "ip-rewrite"
  else
    actual=$(guest_ip "${VM_NAME}")
    fail "${VM_NAME}: ip-rewrite IP expected=${REWRITE_IP} actual=${actual:-none}"
  fi

  stop_vm "${VM_NAME}"
done

echo ""
info "================================================================="
if [[ ${FAILURES} -eq 0 ]]; then
  info "  ALL ${TESTS} TESTS PASSED"
else
  error "  ${FAILURES}/${TESTS} TESTS FAILED"
fi
info "================================================================="

exit "${FAILURES}"
