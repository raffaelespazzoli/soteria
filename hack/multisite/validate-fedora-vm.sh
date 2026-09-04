#!/usr/bin/env bash

# Copyright 2026 The Soteria Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Validates that a Fedora VM boots successfully on KubeVirt with Rook-Ceph
# storage and calculates node sizing for the full 6-VM integration test.
#
# Prerequisites:
#   - Minikube KVM2 clusters created by setup-clusters.sh (east + west)
#   - Rook-Ceph deployed by setup-rook-ceph.sh (StorageClass rook-ceph-block)
#   - KubeVirt + CDI deployed by setup-kubevirt.sh
#   - Nested virtualization enabled on the host
#
# Usage:
#   ./hack/multisite/validate-fedora-vm.sh
#
# Environment Variables:
#   EAST_CLUSTER_NAME   Name of the east Minikube profile (default: east)
#   WEST_CLUSTER_NAME   Name of the west Minikube profile (default: west)
#   FEDORA_IMAGE        Fedora container disk image (default: quay.io/containerdisks/fedora:latest)
#   VM_MEMORY           Memory for the test VM (default: 256Mi)
#   DISK_SIZE           Root disk size (default: 5Gi)
#   VM_BOOT_TIMEOUT     Timeout in seconds for VM to reach Running (default: 300)
#   GUEST_AGENT_TIMEOUT Timeout in seconds for guest agent check (default: 300)
#   SKIP_CLEANUP        Set to "1" to skip cleanup of test resources

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
FEDORA_IMAGE="${FEDORA_IMAGE:-quay.io/containerdisks/fedora:latest}"
VM_MEMORY="${VM_MEMORY:-256Mi}"
DISK_SIZE="${DISK_SIZE:-5Gi}"
VM_BOOT_TIMEOUT="${VM_BOOT_TIMEOUT:-300}"
GUEST_AGENT_TIMEOUT="${GUEST_AGENT_TIMEOUT:-300}"
SKIP_CLEANUP="${SKIP_CLEANUP:-0}"

BIN_DIR="${SCRIPT_DIR}/.bin"
EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"

GOLDEN_NS="kubevirt-golden-images"
GOLDEN_DV="fedora-golden"
VM_NAME="fedora-validation"
DV_NAME="fedora-validation-rootdisk"
VM_NAMESPACE="fedora-validation"
RESOURCE_PROFILE_SOAK_SECONDS=30
ACTIVE_CONTEXT=""
ACTIVE_CLUSTER_NAME=""

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { error "$@"; exit 1; }

# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }

# ---------------------------------------------------------------------------
# Prerequisite checks (Task 1.2)
# ---------------------------------------------------------------------------
check_prerequisites() {
  info "Checking prerequisites..."

  command -v kubectl &>/dev/null || fatal "kubectl not found"
  ensure_minikube
  command -v minikube &>/dev/null || fatal "minikube not available after download attempt"
  command -v jq &>/dev/null || fatal "jq not found. Install: https://jqlang.org/download/"
  command -v bc &>/dev/null || fatal "bc not found. Install it via your package manager."

  local virtctl_path="${BIN_DIR}/virtctl"
  if [[ ! -x "${virtctl_path}" ]]; then
    fatal "virtctl not found at ${virtctl_path}. Run setup-kubevirt.sh first."
  fi

  if ! minikube status -p "${EAST_CLUSTER_NAME}" &>/dev/null 2>&1; then
    fatal "Minikube cluster '${EAST_CLUSTER_NAME}' not running. Run setup-clusters.sh first."
  fi
  if ! minikube status -p "${WEST_CLUSTER_NAME}" &>/dev/null 2>&1; then
    fatal "Minikube cluster '${WEST_CLUSTER_NAME}' not running. Run setup-clusters.sh first."
  fi

  # Verify KubeVirt is deployed
  local east_phase
  east_phase="$(keast -n kubevirt get kubevirt kubevirt -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
  if [[ "${east_phase}" != "Deployed" ]]; then
    fatal "KubeVirt not deployed on east (phase: ${east_phase:-not found}). Run setup-kubevirt.sh first."
  fi
  local west_phase
  west_phase="$(kwest -n kubevirt get kubevirt kubevirt -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
  if [[ "${west_phase}" != "Deployed" ]]; then
    fatal "KubeVirt not deployed on west (phase: ${west_phase:-not found}). Run setup-kubevirt.sh first."
  fi

  # Verify Rook-Ceph StorageClass is available
  if ! keast get storageclass rook-ceph-block &>/dev/null; then
    fatal "StorageClass 'rook-ceph-block' not found on east. Run setup-rook-ceph.sh first."
  fi
  if ! kwest get storageclass rook-ceph-block &>/dev/null; then
    fatal "StorageClass 'rook-ceph-block' not found on west. Run setup-rook-ceph.sh first."
  fi

  info "All prerequisites satisfied"
}

# ---------------------------------------------------------------------------
# Golden image management (Task 3)
#
# Imports the Fedora container disk image into a well-known "golden" PVC
# once per cluster. Subsequent VM creation clones from this PVC, which is
# near-instant with Ceph CSI cloning instead of re-downloading from the
# registry each time.
# ---------------------------------------------------------------------------
ensure_golden_image_on_cluster() {
  local context="$1" cluster_name="$2"

  kubectl --context "${context}" create namespace "${GOLDEN_NS}" \
    --dry-run=client -o yaml | kubectl --context "${context}" apply -f - >/dev/null

  local phase
  phase="$(kubectl --context "${context}" -n "${GOLDEN_NS}" get datavolume "${GOLDEN_DV}" \
    -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
  if [[ "${phase}" == "Succeeded" ]]; then
    info "Golden image already exists on '${cluster_name}' (phase: Succeeded)"
    return 0
  fi

  info "Importing golden Fedora image on '${cluster_name}'..."
  kubectl --context "${context}" apply -f - <<EOF
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: ${GOLDEN_DV}
  namespace: ${GOLDEN_NS}
spec:
  source:
    registry:
      url: "docker://${FEDORA_IMAGE}"
  storage:
    accessModes: ["ReadWriteOnce"]
    storageClassName: rook-ceph-block
    resources:
      requests:
        storage: ${DISK_SIZE}
EOF

  info "Waiting for golden image import on '${cluster_name}'..."
  local attempts=0 max_attempts=120
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    phase="$(kubectl --context "${context}" -n "${GOLDEN_NS}" get datavolume "${GOLDEN_DV}" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${phase}" == "Succeeded" ]]; then
      info "Golden image import completed on '${cluster_name}'"
      return 0
    fi
    if [[ "${phase}" == "Failed" ]]; then
      kubectl --context "${context}" -n "${GOLDEN_NS}" describe datavolume "${GOLDEN_DV}"
      fatal "Golden image import FAILED on '${cluster_name}'"
    fi
    attempts=$((attempts + 1))
    sleep 5
  done
  kubectl --context "${context}" -n "${GOLDEN_NS}" describe datavolume "${GOLDEN_DV}"
  fatal "Golden image import did not complete on '${cluster_name}' within timeout"
}

ensure_golden_images() {
  info "============================================================"
  info "Ensuring Fedora golden image exists on both clusters"
  info "============================================================"

  ensure_golden_image_on_cluster "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  ensure_golden_image_on_cluster "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  info "Golden image ready on both clusters"
}

# ---------------------------------------------------------------------------
# Namespace management
# ---------------------------------------------------------------------------
ensure_namespace() {
  local context="$1" cluster_name="$2"
  info "Ensuring namespace '${VM_NAMESPACE}' exists on '${cluster_name}'..."
  kubectl --context "${context}" create namespace "${VM_NAMESPACE}" \
    --dry-run=client -o yaml | kubectl --context "${context}" apply -f -
}

delete_namespace() {
  local context="$1" cluster_name="$2"
  info "Deleting namespace '${VM_NAMESPACE}' on '${cluster_name}'..."
  kubectl --context "${context}" delete namespace "${VM_NAMESPACE}" \
    --ignore-not-found --timeout=180s 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Clone golden image into validation rootdisk (Task 1.4)
# ---------------------------------------------------------------------------
create_datavolume() {
  local context="$1" cluster_name="$2"

  info "Creating DataVolume '${DV_NAME}' on '${cluster_name}' (cloning from golden image)..."

  kubectl --context "${context}" apply -f - <<EOF
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: ${DV_NAME}
  namespace: ${VM_NAMESPACE}
spec:
  source:
    pvc:
      namespace: ${GOLDEN_NS}
      name: ${GOLDEN_DV}
  storage:
    accessModes: ["ReadWriteOnce"]
    storageClassName: rook-ceph-block
    resources:
      requests:
        storage: ${DISK_SIZE}
EOF

  info "Waiting for DataVolume '${DV_NAME}' clone to complete on '${cluster_name}'..."
  local attempts=0 max_attempts=60
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local phase
    phase="$(kubectl --context "${context}" -n "${VM_NAMESPACE}" get datavolume "${DV_NAME}" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${phase}" == "Succeeded" ]]; then
      info "DataVolume '${DV_NAME}' clone completed on '${cluster_name}'"
      return 0
    fi
    if [[ "${phase}" == "Failed" ]]; then
      kubectl --context "${context}" -n "${VM_NAMESPACE}" describe datavolume "${DV_NAME}"
      fatal "DataVolume '${DV_NAME}' clone FAILED on '${cluster_name}'"
    fi
    attempts=$((attempts + 1))
    sleep 5
  done
  kubectl --context "${context}" -n "${VM_NAMESPACE}" describe datavolume "${DV_NAME}"
  fatal "DataVolume '${DV_NAME}' clone did not complete on '${cluster_name}' within timeout"
}

# ---------------------------------------------------------------------------
# Create Fedora VM (Task 1.5)
# ---------------------------------------------------------------------------
create_fedora_vm() {
  local context="$1" cluster_name="$2"

  info "Creating Fedora VM '${VM_NAME}' on '${cluster_name}' (memory: ${VM_MEMORY})..."

  kubectl --context "${context}" apply -f - <<EOF
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: ${VM_NAME}
  namespace: ${VM_NAMESPACE}
spec:
  runStrategy: Always
  template:
    spec:
      domain:
        resources:
          requests:
            memory: ${VM_MEMORY}
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: ${DV_NAME}
EOF

  info "Fedora VM '${VM_NAME}' created on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Wait for VM to reach Running (Task 1.6)
# ---------------------------------------------------------------------------
wait_vm_running() {
  local context="$1" cluster_name="$2"

  info "Waiting for VM '${VM_NAME}' to reach Running on '${cluster_name}' (timeout: ${VM_BOOT_TIMEOUT}s)..."
  local attempts=0
  local max_attempts=$(( VM_BOOT_TIMEOUT / 5 ))
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local phase
    phase="$(kubectl --context "${context}" -n "${VM_NAMESPACE}" get vmi "${VM_NAME}" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${phase}" == "Running" ]]; then
      info "VM '${VM_NAME}' is Running on '${cluster_name}'"
      return 0
    fi
    if [[ "${phase}" == "Failed" ]]; then
      kubectl --context "${context}" -n "${VM_NAMESPACE}" describe vmi "${VM_NAME}"
      fatal "VM '${VM_NAME}' FAILED on '${cluster_name}'"
    fi
    attempts=$((attempts + 1))
    sleep 5
  done
  kubectl --context "${context}" -n "${VM_NAMESPACE}" describe vmi "${VM_NAME}" 2>/dev/null || true
  kubectl --context "${context}" -n "${VM_NAMESPACE}" describe vm "${VM_NAME}" 2>/dev/null || true
  fatal "VM '${VM_NAME}' did not reach Running on '${cluster_name}' within ${VM_BOOT_TIMEOUT}s"
}

# ---------------------------------------------------------------------------
# Verify guest OS responsiveness (Task 1.7)
# ---------------------------------------------------------------------------
verify_guest_responsive() {
  local context="$1" cluster_name="$2"

  info "Waiting for guest OS to become responsive on '${cluster_name}'..."
  local attempts=0
  local max_attempts=$(( GUEST_AGENT_TIMEOUT / 5 ))
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local agent_connected
    agent_connected=$(kubectl --context "${context}" -n "${VM_NAMESPACE}" get vmi "${VM_NAME}" \
      -o jsonpath='{.status.conditions[?(@.type=="AgentConnected")].status}' 2>/dev/null) || true
    if [[ "${agent_connected}" == "True" ]]; then
      info "Guest agent connected on '${VM_NAME}' ('${cluster_name}')"
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 5
  done

  info "Guest agent not detected, probing serial console for login prompt..."
  if timeout 30s bash -lc "printf '\n' | \"${BIN_DIR}/virtctl\" --context \"${context}\" -n \"${VM_NAMESPACE}\" console \"${VM_NAME}\" 2>/dev/null" | grep -Eiq 'login:|localhost login:'; then
    info "Serial console reported a login prompt on '${VM_NAME}' ('${cluster_name}')"
    return 0
  fi

  fatal "Guest OS did not become responsive on '${cluster_name}' (no guest agent or console login prompt detected)"
}

validate_vm_resource_profile() {
  local context="$1" cluster_name="$2"

  info "Checking resource-profile stability on '${cluster_name}'..."
  sleep "${RESOURCE_PROFILE_SOAK_SECONDS}"

  local launcher_pod
  launcher_pod="$(kubectl --context "${context}" -n "${VM_NAMESPACE}" get pod \
    -l "vm.kubevirt.io/name=${VM_NAME}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "${launcher_pod}" ]] || fatal "Could not find virt-launcher pod for '${VM_NAME}' on '${cluster_name}'"

  local restart_count terminated_reason waiting_reason
  restart_count="$(kubectl --context "${context}" -n "${VM_NAMESPACE}" get pod "${launcher_pod}" \
    -o jsonpath='{.status.containerStatuses[?(@.name=="compute")].restartCount}' 2>/dev/null || echo "")"
  terminated_reason="$(kubectl --context "${context}" -n "${VM_NAMESPACE}" get pod "${launcher_pod}" \
    -o jsonpath='{.status.containerStatuses[?(@.name=="compute")].lastState.terminated.reason}' 2>/dev/null || echo "")"
  waiting_reason="$(kubectl --context "${context}" -n "${VM_NAMESPACE}" get pod "${launcher_pod}" \
    -o jsonpath='{.status.containerStatuses[?(@.name=="compute")].state.waiting.reason}' 2>/dev/null || echo "")"

  if [[ "${terminated_reason}" == "OOMKilled" ]]; then
    kubectl --context "${context}" -n "${VM_NAMESPACE}" describe pod "${launcher_pod}" 2>/dev/null || true
    fatal "virt-launcher pod for '${VM_NAME}' was OOMKilled on '${cluster_name}'"
  fi

  if [[ -n "${restart_count}" && "${restart_count}" != "0" ]]; then
    kubectl --context "${context}" -n "${VM_NAMESPACE}" describe pod "${launcher_pod}" 2>/dev/null || true
    fatal "virt-launcher pod for '${VM_NAME}' restarted on '${cluster_name}' (restartCount=${restart_count})"
  fi

  if [[ -n "${waiting_reason}" ]]; then
    kubectl --context "${context}" -n "${VM_NAMESPACE}" describe pod "${launcher_pod}" 2>/dev/null || true
    fatal "virt-launcher pod for '${VM_NAME}' is not stable on '${cluster_name}' (waiting=${waiting_reason})"
  fi

  info "Resource profile stable on '${cluster_name}' for ${RESOURCE_PROFILE_SOAK_SECONDS}s"
}

# ---------------------------------------------------------------------------
# Node sizing calculation (Task 2)
# ---------------------------------------------------------------------------
print_sizing_report() {
  info "============================================================"
  info "Node Sizing Report for 6-VM Integration Test"
  info "============================================================"
  echo

  echo -e "${BOLD}Per-VM Resource Budget:${NC}"
  printf "  %-30s %10s %15s\n" "Component" "CPU" "Memory"
  printf "  %-30s %10s %15s\n" "------------------------------" "----------" "---------------"
  printf "  %-30s %10s %15s\n" "VM (guest)" "100m" "${VM_MEMORY}"
  printf "  %-30s %10s %15s\n" "virt-launcher (per VM)" "100m" "128Mi"
  printf "  %-30s %10s %15s\n" "Per-VM total" "200m" "384Mi"
  echo
  printf "  %-30s %10s %15s\n" "6 VMs total" "1200m" "2304Mi"
  echo

  echo -e "${BOLD}Per-Node System Overhead (3 workers):${NC}"
  printf "  %-30s %10s %15s\n" "Component" "CPU" "Memory"
  printf "  %-30s %10s %15s\n" "------------------------------" "----------" "---------------"
  printf "  %-30s %10s %15s\n" "Cilium agent" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "virt-handler" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "Rook OSD (1 per node)" "100m" "512Mi"
  printf "  %-30s %10s %15s\n" "kubelet/system reserved" "-" "512Mi"
  printf "  %-30s %10s %15s\n" "Per-node overhead total" "300m" "1536Mi"
  echo

  echo -e "${BOLD}Per-Cluster Shared Components:${NC}"
  printf "  %-30s %10s %15s\n" "Component" "CPU" "Memory"
  printf "  %-30s %10s %15s\n" "------------------------------" "----------" "---------------"
  printf "  %-30s %10s %15s\n" "Rook operator" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "Ceph mon" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "Ceph mgr" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "virt-operator" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "virt-api" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "virt-controller" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "CDI operator" "100m" "128Mi"
  printf "  %-30s %10s %15s\n" "Cilium operator" "100m" "128Mi"
  printf "  %-30s %10s %15s\n" "CSI Addons controller" "50m" "128Mi"
  printf "  %-30s %10s %15s\n" "ScyllaDB (1 member)" "100m" "512Mi"
  printf "  %-30s %10s %15s\n" "scylla-operator" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "cert-manager" "50m" "128Mi"
  printf "  %-30s %10s %15s\n" "Soteria operator" "100m" "256Mi"
  printf "  %-30s %10s %15s\n" "MetalLB" "50m" "128Mi"
  printf "  %-30s %10s %15s\n" "Shared total" "~1350m" "~3200Mi"
  echo

  echo -e "${BOLD}Total Per-Cluster (3 workers, 6 VMs):${NC}"
  printf "  %-30s %15s\n" "Node overhead (3 x 1536Mi)" "4608Mi"
  printf "  %-30s %15s\n" "VM workloads" "2304Mi"
  printf "  %-30s %15s\n" "Shared components" "~3200Mi"
  printf "  %-30s %15s\n" "Total memory needed" "~10.1 GiB"
  printf "  %-30s %15s\n" "Total CPU needed" "~4.5 cores"
  echo

  echo -e "${BOLD}${CYAN}Recommended Node Sizing:${NC}"
  echo -e "  NODE_CPUS=4      (4 vCPUs per node x 4 nodes = 16 vCPUs per cluster)"
  echo -e "  WORKER_MEMORY=6144  (6 GiB per worker x 3 workers = 18 GiB per cluster)"
  echo -e "  MASTER_MEMORY=7168  (7 GiB for control-plane)"
  echo -e "  This provides ~50% headroom over calculated minimums."
  echo
  echo -e "  To resize clusters:"
  echo -e "    1. Tear down: ./hack/multisite/teardown.sh"
  echo -e "    2. Recreate:  NODE_CPUS=4 WORKER_MEMORY=6144 MASTER_MEMORY=7168 ./hack/multisite/setup-clusters.sh"
  echo
}

# ---------------------------------------------------------------------------
# Node capacity check (Task 1.8 / AC5)
# ---------------------------------------------------------------------------
check_node_capacity() {
  local context="$1" cluster_name="$2"
  info "Checking node capacity on '${cluster_name}'..."

  echo -e "\n${BOLD}Node Allocatable Resources — ${cluster_name}:${NC}"

  # Per-node allocatable
  kubectl --context "${context}" get nodes -o json 2>/dev/null | \
    jq -r '.items[] |
      "\(.metadata.name): CPU=\(.status.allocatable.cpu) Memory=\(.status.allocatable.memory)"' | \
    while IFS= read -r line; do
      echo "  ${line}"
    done

  # Total allocatable across worker nodes (exclude control-plane)
  local total_cpu_m total_mem_ki
  total_cpu_m=$(kubectl --context "${context}" get nodes -o json 2>/dev/null | \
    jq '[.items[] |
      select(
        (.metadata.labels["node-role.kubernetes.io/control-plane"] == null) and
        (.metadata.labels["node-role.kubernetes.io/master"] == null)
      ) |
      .status.allocatable.cpu |
      if test("m$") then rtrimstr("m") | tonumber
      else tonumber * 1000
      end
    ] | add // 0')

  total_mem_ki=$(kubectl --context "${context}" get nodes -o json 2>/dev/null | \
    jq '[.items[] |
      select(
        (.metadata.labels["node-role.kubernetes.io/control-plane"] == null) and
        (.metadata.labels["node-role.kubernetes.io/master"] == null)
      ) |
      .status.allocatable.memory |
      if test("Ki$") then rtrimstr("Ki") | tonumber
      elif test("Mi$") then rtrimstr("Mi") | tonumber * 1024
      elif test("Gi$") then rtrimstr("Gi") | tonumber * 1048576
      else tonumber
      end
    ] | add // 0')

  local total_mem_gib
  total_mem_gib=$(echo "scale=1; ${total_mem_ki} / 1048576" | bc)

  echo
  info "Total allocatable on '${cluster_name}' workers: ${total_cpu_m}m CPU, ${total_mem_gib} GiB memory"

  local required_mem_gib=10.1
  local required_cpu_m=4500

  if (( $(echo "${total_mem_gib} < ${required_mem_gib}" | bc -l) )); then
    warn "INSUFFICIENT MEMORY for 6 VMs on '${cluster_name}'!"
    warn "  Need ~${required_mem_gib} GiB, have ${total_mem_gib} GiB"
    warn "  Resize with: WORKER_MEMORY=6144 MASTER_MEMORY=7168 ./hack/multisite/setup-clusters.sh (after teardown)"
  else
    info "Memory capacity sufficient for 6 VMs on '${cluster_name}' (${total_mem_gib} GiB >= ${required_mem_gib} GiB)"
  fi

  if (( total_cpu_m < required_cpu_m )); then
    warn "INSUFFICIENT CPU for 6 VMs on '${cluster_name}'!"
    warn "  Need ~${required_cpu_m}m, have ${total_cpu_m}m"
    warn "  Resize with: NODE_CPUS=4 ./hack/multisite/setup-clusters.sh (after teardown)"
  else
    info "CPU capacity sufficient for 6 VMs on '${cluster_name}' (${total_cpu_m}m >= ${required_cpu_m}m)"
  fi
}

# ---------------------------------------------------------------------------
# Cleanup (Task 1.9)
# ---------------------------------------------------------------------------
cleanup_test_resources() {
  local context="$1" cluster_name="$2"

  if [[ "${SKIP_CLEANUP}" == "1" ]]; then
    info "Skipping cleanup on '${cluster_name}' (SKIP_CLEANUP=1)"
    return 0
  fi

  info "Cleaning up test resources on '${cluster_name}'..."
  delete_namespace "${context}" "${cluster_name}"
  info "Test resources cleaned up on '${cluster_name}'"
}

cleanup_on_exit() {
  local exit_code=$?
  trap - EXIT

  if [[ ${exit_code} -ne 0 && -n "${ACTIVE_CONTEXT}" ]]; then
    warn "Validation failed on '${ACTIVE_CLUSTER_NAME}' — leaving resources for debugging"
    warn "  Inspect:  kubectl --context ${ACTIVE_CONTEXT} -n ${VM_NAMESPACE} get vm,vmi,pvc,pods"
    warn "  Console:  ${BIN_DIR}/virtctl --context ${ACTIVE_CONTEXT} -n ${VM_NAMESPACE} console ${VM_NAME}"
    warn "  Cleanup:  kubectl --context ${ACTIVE_CONTEXT} delete namespace ${VM_NAMESPACE}"
  fi

  exit "${exit_code}"
}

# ---------------------------------------------------------------------------
# Run validation on a single cluster
# ---------------------------------------------------------------------------
validate_cluster() {
  local context="$1" cluster_name="$2"

  info "------------------------------------------------------------"
  info "Validating Fedora VM on '${cluster_name}'"
  info "------------------------------------------------------------"

  ACTIVE_CONTEXT="${context}"
  ACTIVE_CLUSTER_NAME="${cluster_name}"

  # Clean up any leftover resources from a previous run
  cleanup_test_resources "${context}" "${cluster_name}"

  # Create dedicated namespace for validation
  ensure_namespace "${context}" "${cluster_name}"

  # Create DataVolume (imports Fedora into Ceph PVC) and VM
  create_datavolume "${context}" "${cluster_name}"
  create_fedora_vm "${context}" "${cluster_name}"

  # Wait for VM to boot
  wait_vm_running "${context}" "${cluster_name}"

  # Verify guest responsiveness
  verify_guest_responsive "${context}" "${cluster_name}"
  validate_vm_resource_profile "${context}" "${cluster_name}"

  # Check node capacity for full 6-VM test
  check_node_capacity "${context}" "${cluster_name}"

  # Cleanup
  cleanup_test_resources "${context}" "${cluster_name}"
  ACTIVE_CONTEXT=""
  ACTIVE_CLUSTER_NAME=""

  info "Fedora VM validation PASSED on '${cluster_name}'"
}

# ===========================================================================
# Main
# ===========================================================================
main() {
  trap cleanup_on_exit EXIT

  info "============================================================"
  info "Fedora VM Validation & Node Sizing"
  info "============================================================"
  info "  Fedora image: ${FEDORA_IMAGE}"
  info "  VM memory:    ${VM_MEMORY}"
  info "  Clusters:     ${EAST_CLUSTER_NAME}, ${WEST_CLUSTER_NAME}"
  echo

  check_prerequisites

  # Ensure golden Fedora image exists on both clusters
  ensure_golden_images

  # Validate Fedora VM on each cluster
  validate_cluster "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  validate_cluster "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Print node sizing report
  print_sizing_report

  info "============================================================"
  info "Fedora VM Validation Complete!"
  info "  Both clusters validated: Fedora VM boots with Rook-Ceph PVC"
  info "  Resource profile validated: ${VM_MEMORY} memory"
  info "  Node sizing report printed above"
  info "============================================================"
}

main "$@"
