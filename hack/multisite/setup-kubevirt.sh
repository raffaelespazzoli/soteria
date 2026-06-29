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

# Deploys KubeVirt with hardware-accelerated KVM (nested virtualization) and
# CDI (Containerized Data Importer) on both Minikube KVM2 clusters. Validates
# via container disk and PVC-backed disk smoke tests.
#
# Prerequisites:
#   - Minikube KVM2 clusters created by setup-clusters.sh (east + west)
#   - Rook-Ceph deployed by setup-rook-ceph.sh (StorageClass rook-ceph-block)
#   - Nested virtualization enabled on the host
#
# Usage:
#   ./hack/multisite/setup-kubevirt.sh
#
# Environment Variables:
#   EAST_CLUSTER_NAME   Name of the east Minikube profile (default: east)
#   WEST_CLUSTER_NAME   Name of the west Minikube profile (default: west)
#   KUBEVIRT_VERSION    KubeVirt release to deploy (default: fetched from stable.txt)
#   CDI_VERSION         CDI release to deploy (default: fetched from GitHub)
#   SMOKE_TEST          Set to "0" to skip smoke tests

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
KUBEVIRT_VERSION="${KUBEVIRT_VERSION:-}"
CDI_VERSION="${CDI_VERSION:-}"
SMOKE_TEST="${SMOKE_TEST:-1}"

BIN_DIR="${SCRIPT_DIR}/.bin"
EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { error "$@"; exit 1; }

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
check_prerequisites() {
  info "Checking prerequisites..."

  command -v kubectl &>/dev/null || fatal "kubectl not found"
  command -v minikube &>/dev/null || fatal "minikube not found. Install: https://minikube.sigs.k8s.io/"
  command -v curl &>/dev/null || fatal "curl not found"

  if ! minikube status -p "${EAST_CLUSTER_NAME}" &>/dev/null 2>&1; then
    fatal "Minikube cluster '${EAST_CLUSTER_NAME}' not running. Run setup-clusters.sh first."
  fi
  if ! minikube status -p "${WEST_CLUSTER_NAME}" &>/dev/null 2>&1; then
    fatal "Minikube cluster '${WEST_CLUSTER_NAME}' not running. Run setup-clusters.sh first."
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
# Nested virtualization verification
# ---------------------------------------------------------------------------
verify_nested_virt() {
  local profile="$1"
  info "Checking nested virtualization on Minikube profile '${profile}'..."

  local nodes
  nodes=$(minikube node list -p "${profile}" 2>/dev/null | awk 'NR>1 && $1!="" {print $1}')
  if [[ -z "${nodes}" ]]; then
    fatal "No nodes found for Minikube profile '${profile}'"
  fi
  for node in ${nodes}; do
    if ! minikube ssh -p "${profile}" -n "${node}" -- "test -c /dev/kvm" 2>/dev/null; then
      fatal "Nested virtualization not available on node '${node}' in profile '${profile}'.
  Enable nested virt on host:
    Intel: echo 'options kvm_intel nested=1' | sudo tee /etc/modprobe.d/kvm.conf && sudo modprobe -r kvm_intel && sudo modprobe kvm_intel
    AMD:   echo 'options kvm_amd nested=1' | sudo tee /etc/modprobe.d/kvm.conf && sudo modprobe -r kvm_amd && sudo modprobe kvm_amd"
    fi
  done
  info "Nested virtualization confirmed on all nodes in '${profile}'"
}

# ---------------------------------------------------------------------------
# Fetch KubeVirt version
# ---------------------------------------------------------------------------
fetch_kubevirt_version() {
  if [[ -z "${KUBEVIRT_VERSION}" ]]; then
    info "Fetching latest stable KubeVirt version..."
    KUBEVIRT_VERSION="$(curl -s https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)"
    if [[ -z "${KUBEVIRT_VERSION}" ]]; then
      fatal "Could not fetch KubeVirt stable version"
    fi
  fi
  info "KubeVirt version: ${KUBEVIRT_VERSION}"
}

# ---------------------------------------------------------------------------
# Deploy KubeVirt operator and CR
# ---------------------------------------------------------------------------
deploy_kubevirt() {
  local context="$1" cluster_name="$2"

  info "Deploying KubeVirt operator on '${cluster_name}'..."
  kubectl --context "${context}" apply --server-side --force-conflicts \
    -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml"

  # Single-node dev clusters don't need HA — reduce to 1 replica to save resources
  info "Scaling virt-operator to 1 replica on '${cluster_name}'..."
  kubectl --context "${context}" -n kubevirt scale deployment virt-operator --replicas=1

  info "Waiting for KubeVirt CRD to be established on '${cluster_name}'..."
  kubectl --context "${context}" wait --for=condition=Established --timeout=120s \
    crd/kubevirts.kubevirt.io

  info "Deploying KubeVirt CR on '${cluster_name}' (hardware KVM required — no emulation)..."
  kubectl --context "${context}" apply --server-side --force-conflicts -f - <<'EOF'
apiVersion: kubevirt.io/v1
kind: KubeVirt
metadata:
  name: kubevirt
  namespace: kubevirt
spec:
  infra:
    replicas: 1
  configuration:
    developerConfiguration:
      featureGates:
        - LiveMigration
        - Snapshot
EOF

  info "KubeVirt operator and CR applied on '${cluster_name}'"
}

wait_kubevirt_deployed() {
  local context="$1" cluster_name="$2"

  info "Waiting for KubeVirt to reach 'Deployed' phase on '${cluster_name}'..."
  local attempts=0 max_attempts=90
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local phase
    phase="$(kubectl --context "${context}" -n kubevirt get kubevirt kubevirt \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${phase}" == "Deployed" ]]; then
      info "KubeVirt is Deployed on '${cluster_name}'"
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 30
  done
  fatal "KubeVirt did not reach 'Deployed' phase on '${cluster_name}' within 5 minutes (current: ${phase:-unknown})"
}

verify_virt_handler_kvm() {
  local context="$1" cluster_name="$2"

  info "Verifying virt-handler can access /dev/kvm on '${cluster_name}'..."
  local pods
  pods="$(kubectl --context "${context}" -n kubevirt get pods \
    -l kubevirt.io=virt-handler --no-headers -o custom-columns=NAME:.metadata.name 2>/dev/null)"

  if [[ -z "${pods}" ]]; then
    fatal "No virt-handler pods found on '${cluster_name}'"
  fi

  for pod in ${pods}; do
    if ! kubectl --context "${context}" -n kubevirt exec "${pod}" -- test -c /dev/kvm 2>/dev/null; then
      fatal "virt-handler '${pod}' on '${cluster_name}' cannot access /dev/kvm. Check nested virtualization."
    fi
  done
  info "virt-handler confirmed /dev/kvm access on '${cluster_name}'"
}

wait_kubevirt_pods() {
  local context="$1" cluster_name="$2"

  info "Waiting for all KubeVirt pods to be Running on '${cluster_name}'..."
  local attempts=0 max_attempts=60
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local not_ready
    not_ready="$(kubectl --context "${context}" -n kubevirt get pods --no-headers 2>/dev/null \
      | { grep -v "Running\|Completed" || true; } | wc -l)"
    if [[ "${not_ready}" -eq 0 ]]; then
      local total
      total="$(kubectl --context "${context}" -n kubevirt get pods --no-headers 2>/dev/null | wc -l)"
      if [[ "${total}" -ge 4 ]]; then
        info "All KubeVirt pods (${total}) Running on '${cluster_name}'"
        return 0
      fi
    fi
    attempts=$((attempts + 1))
    sleep 5
  done
  warn "Some KubeVirt pods are not Running on '${cluster_name}' after timeout:"
  kubectl --context "${context}" -n kubevirt get pods
  fatal "KubeVirt pods not ready on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Deploy CDI (Containerized Data Importer)
# ---------------------------------------------------------------------------
fetch_cdi_version() {
  if [[ -z "${CDI_VERSION}" ]]; then
    info "Fetching latest stable CDI version..."
    CDI_VERSION="$(basename "$(curl -s -w '%{redirect_url}' \
      https://github.com/kubevirt/containerized-data-importer/releases/latest)")"
    if [[ -z "${CDI_VERSION}" || "${CDI_VERSION}" == "latest" ]]; then
      fatal "Could not fetch CDI version from GitHub redirect"
    fi
  fi
  info "CDI version: ${CDI_VERSION}"
}

deploy_cdi() {
  local context="$1" cluster_name="$2"

  info "Deploying CDI operator on '${cluster_name}'..."
  kubectl --context "${context}" apply --server-side --force-conflicts \
    -f "https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-operator.yaml"

  info "Waiting for CDI CRD to be established on '${cluster_name}'..."
  kubectl --context "${context}" wait --for=condition=Established --timeout=120s \
    crd/cdis.cdi.kubevirt.io

  info "Deploying CDI CR on '${cluster_name}'..."
  kubectl --context "${context}" apply --server-side --force-conflicts \
    -f "https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-cr.yaml"

  info "CDI operator and CR applied on '${cluster_name}'"
}

wait_cdi_deployed() {
  local context="$1" cluster_name="$2"

  info "Waiting for CDI to reach 'Deployed' phase on '${cluster_name}'..."
  local attempts=0 max_attempts=60
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local phase
    phase="$(kubectl --context "${context}" -n cdi get cdi cdi \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${phase}" == "Deployed" ]]; then
      info "CDI is Deployed on '${cluster_name}'"
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 5
  done
  fatal "CDI did not reach 'Deployed' phase on '${cluster_name}' within 5 minutes (current: ${phase:-unknown})"
}

verify_storage_profile() {
  local context="$1" cluster_name="$2"

  info "Verifying StorageProfile for 'rook-ceph-block' on '${cluster_name}'..."
  local attempts=0 max_attempts=12
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local claim_sets
    claim_sets="$(kubectl --context "${context}" get storageprofile rook-ceph-block \
      -o jsonpath='{.status.claimPropertySets}' 2>/dev/null || echo "")"
    if [[ -n "${claim_sets}" && "${claim_sets}" != "[]" ]]; then
      info "StorageProfile for 'rook-ceph-block' is populated on '${cluster_name}'"
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 5
  done
  fatal "StorageProfile for 'rook-ceph-block' not populated on '${cluster_name}' within 60s. CDI may need manual StorageProfile configuration."
}

# ---------------------------------------------------------------------------
# Install virtctl
# ---------------------------------------------------------------------------
install_virtctl() {
  mkdir -p "${BIN_DIR}"

  local virtctl_path="${BIN_DIR}/virtctl"

  # Skip if already installed with correct version
  if [[ -x "${virtctl_path}" ]]; then
    local existing_version
    existing_version="$("${virtctl_path}" version --client 2>/dev/null | grep -oP 'v[\d.]+' | head -1 || echo "")"
    if [[ "${existing_version}" == "${KUBEVIRT_VERSION}" ]]; then
      info "virtctl ${KUBEVIRT_VERSION} already installed at ${virtctl_path}"
      return 0
    fi
  fi

  info "Installing virtctl ${KUBEVIRT_VERSION}..."
  local arch
  arch="$(uname -m)"
  case "${arch}" in
    x86_64)  arch="amd64" ;;
    aarch64) arch="arm64" ;;
    *)       fatal "Unsupported architecture: ${arch}" ;;
  esac

  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"

  curl -sL -o "${virtctl_path}" \
    "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/virtctl-${KUBEVIRT_VERSION}-${os}-${arch}"
  chmod +x "${virtctl_path}"

  info "virtctl installed at ${virtctl_path}"
}

verify_virtctl() {
  local virtctl_path="${BIN_DIR}/virtctl"

  info "Verifying virtctl against both clusters..."
  "${virtctl_path}" version --client >/dev/null 2>&1 || fatal "virtctl --client failed"
  "${virtctl_path}" version --context "${EAST_CONTEXT}" >/dev/null 2>&1 \
    || fatal "virtctl cannot reach east cluster '${EAST_CONTEXT}'"
  "${virtctl_path}" version --context "${WEST_CONTEXT}" >/dev/null 2>&1 \
    || fatal "virtctl cannot reach west cluster '${WEST_CONTEXT}'"
  info "virtctl is functional against both clusters"
}

# ---------------------------------------------------------------------------
# Smoke test helpers
# ---------------------------------------------------------------------------
SMOKE_NS="kubevirt-smoke-test"

setup_smoke_ns() {
  local context="$1" cluster_name="$2"
  info "Creating namespace '${SMOKE_NS}' on '${cluster_name}'..."
  kubectl --context "${context}" create namespace "${SMOKE_NS}" --dry-run=client -o yaml \
    | kubectl --context "${context}" apply -f -
}

teardown_smoke_ns() {
  local context="$1" cluster_name="$2"
  info "Deleting namespace '${SMOKE_NS}' on '${cluster_name}'..."
  kubectl --context "${context}" delete namespace "${SMOKE_NS}" --ignore-not-found --timeout=120s 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Smoke test: Container disk VMI
# ---------------------------------------------------------------------------
smoke_test_container_disk() {
  local context="$1" cluster_name="$2"

  info "Running container disk smoke test on '${cluster_name}'..."

  kubectl --context "${context}" -n "${SMOKE_NS}" delete vmi smoke-test-vmi --ignore-not-found --timeout=60s 2>/dev/null || true

  kubectl --context "${context}" apply -f - <<EOF
apiVersion: kubevirt.io/v1
kind: VirtualMachineInstance
metadata:
  name: smoke-test-vmi
  namespace: ${SMOKE_NS}
spec:
  domain:
    resources:
      requests:
        memory: 128Mi
    devices:
      disks:
        - name: containerdisk
          disk:
            bus: virtio
  volumes:
    - name: containerdisk
      containerDisk:
        image: quay.io/kubevirt/cirros-container-disk-demo
EOF

  info "Waiting for smoke-test-vmi to reach 'Running' on '${cluster_name}'..."
  local attempts=0 max_attempts=60
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local phase
    phase="$(kubectl --context "${context}" -n "${SMOKE_NS}" get vmi smoke-test-vmi \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${phase}" == "Running" ]]; then
      info "smoke-test-vmi is Running on '${cluster_name}'"
      break
    fi
    if [[ "${phase}" == "Failed" ]]; then
      kubectl --context "${context}" -n "${SMOKE_NS}" get vmi smoke-test-vmi -o yaml
      fatal "smoke-test-vmi FAILED on '${cluster_name}'"
    fi
    attempts=$((attempts + 1))
    sleep 5
  done

  if [[ ${attempts} -ge ${max_attempts} ]]; then
    kubectl --context "${context}" -n "${SMOKE_NS}" describe vmi smoke-test-vmi
    fatal "smoke-test-vmi did not reach Running on '${cluster_name}' within timeout"
  fi

  # Verify KVM acceleration — hard fail if unproven (no emulation fallback)
  local launcher_pod
  launcher_pod="$(kubectl --context "${context}" -n "${SMOKE_NS}" get pods \
    -l vm.kubevirt.io/name=smoke-test-vmi \
    --no-headers -o custom-columns=NAME:.metadata.name 2>/dev/null | head -1)"
  if [[ -z "${launcher_pod}" ]]; then
    fatal "No launcher pod found for smoke-test-vmi on '${cluster_name}' — cannot verify KVM acceleration"
  fi
  local qemu_args
  qemu_args="$(kubectl --context "${context}" -n "${SMOKE_NS}" exec "${launcher_pod}" -- \
    cat /proc/1/cmdline 2>/dev/null | tr '\0' ' ' || echo "")"
  if echo "${qemu_args}" | grep -q "kvm"; then
    info "KVM acceleration confirmed for smoke-test-vmi on '${cluster_name}'"
  else
    fatal "Could not confirm KVM acceleration from QEMU args on '${cluster_name}'. Emulation fallback is not allowed."
  fi

  kubectl --context "${context}" -n "${SMOKE_NS}" delete vmi smoke-test-vmi --ignore-not-found --timeout=60s 2>/dev/null || true
  info "Container disk smoke test passed on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Smoke test: PVC-backed disk VM
# ---------------------------------------------------------------------------
smoke_test_pvc_disk() {
  local context="$1" cluster_name="$2"

  info "Running PVC-backed disk smoke test on '${cluster_name}'..."

  kubectl --context "${context}" -n "${SMOKE_NS}" delete vm smoke-test-vm --ignore-not-found --timeout=60s 2>/dev/null || true
  kubectl --context "${context}" -n "${SMOKE_NS}" delete pvc smoke-test-pvc --ignore-not-found --timeout=60s 2>/dev/null || true
  sleep 2

  kubectl --context "${context}" apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: smoke-test-pvc
  namespace: ${SMOKE_NS}
spec:
  accessModes: ["ReadWriteOnce"]
  storageClassName: rook-ceph-block
  resources:
    requests:
      storage: 1Gi
EOF

  info "Waiting for smoke-test-pvc to bind on '${cluster_name}'..."
  local attempts=0 max_attempts=30
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local pvc_phase
    pvc_phase="$(kubectl --context "${context}" -n "${SMOKE_NS}" get pvc smoke-test-pvc \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${pvc_phase}" == "Bound" ]]; then
      info "smoke-test-pvc is Bound on '${cluster_name}'"
      break
    fi
    attempts=$((attempts + 1))
    sleep 5
  done

  if [[ ${attempts} -ge ${max_attempts} ]]; then
    kubectl --context "${context}" -n "${SMOKE_NS}" describe pvc smoke-test-pvc
    fatal "smoke-test-pvc did not bind on '${cluster_name}' within timeout"
  fi

  kubectl --context "${context}" apply -f - <<EOF
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: smoke-test-vm
  namespace: ${SMOKE_NS}
spec:
  runStrategy: Always
  template:
    spec:
      domain:
        resources:
          requests:
            memory: 128Mi
        devices:
          disks:
            - name: containerdisk
              disk:
                bus: virtio
            - name: datadisk
              disk:
                bus: virtio
      volumes:
        - name: containerdisk
          containerDisk:
            image: quay.io/kubevirt/cirros-container-disk-demo
        - name: datadisk
          persistentVolumeClaim:
            claimName: smoke-test-pvc
EOF

  info "Waiting for smoke-test-vm to reach 'Running' on '${cluster_name}'..."
  attempts=0
  max_attempts=60
  while [[ ${attempts} -lt ${max_attempts} ]]; do
    local ready
    ready="$(kubectl --context "${context}" -n "${SMOKE_NS}" get vm smoke-test-vm \
      -o jsonpath='{.status.ready}' 2>/dev/null || echo "")"
    if [[ "${ready}" == "true" ]]; then
      info "smoke-test-vm is Running on '${cluster_name}'"
      break
    fi
    attempts=$((attempts + 1))
    sleep 5
  done

  if [[ ${attempts} -ge ${max_attempts} ]]; then
    kubectl --context "${context}" -n "${SMOKE_NS}" describe vm smoke-test-vm
    kubectl --context "${context}" -n "${SMOKE_NS}" get vmi smoke-test-vm -o yaml 2>/dev/null || true
    fatal "smoke-test-vm did not reach Running on '${cluster_name}' within timeout"
  fi

  kubectl --context "${context}" -n "${SMOKE_NS}" delete vm smoke-test-vm --ignore-not-found --timeout=60s 2>/dev/null || true
  kubectl --context "${context}" -n "${SMOKE_NS}" delete pvc smoke-test-pvc --ignore-not-found --timeout=60s 2>/dev/null || true
  info "PVC-backed disk smoke test passed on '${cluster_name}'"
}

# ===========================================================================
# Main
# ===========================================================================
main() {
  info "============================================================"
  info "KubeVirt + CDI Setup for Minikube KVM2 Multisite"
  info "============================================================"

  check_prerequisites

  # --- Task 1: Verify nested virt + deploy KubeVirt ---
  verify_nested_virt "${EAST_CLUSTER_NAME}"
  verify_nested_virt "${WEST_CLUSTER_NAME}"

  fetch_kubevirt_version

  deploy_kubevirt "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_kubevirt "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  wait_kubevirt_deployed "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_kubevirt_deployed "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  verify_virt_handler_kvm "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  verify_virt_handler_kvm "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  wait_kubevirt_pods "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_kubevirt_pods "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # --- Task 2: Deploy CDI ---
  fetch_cdi_version

  deploy_cdi "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_cdi "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  wait_cdi_deployed "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_cdi_deployed "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  verify_storage_profile "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  verify_storage_profile "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # --- Task 3: Install virtctl ---
  install_virtctl
  verify_virtctl

  # --- Tasks 4 & 5: Smoke tests ---
  if [[ "${SMOKE_TEST}" != "0" ]]; then
    setup_smoke_ns "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
    setup_smoke_ns "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

    smoke_test_container_disk "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
    smoke_test_container_disk "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

    smoke_test_pvc_disk "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
    smoke_test_pvc_disk "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

    teardown_smoke_ns "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
    teardown_smoke_ns "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"
  else
    info "Skipping smoke tests (SMOKE_TEST=0)"
  fi

  info "============================================================"
  info "KubeVirt + CDI setup complete!"
  info "  KubeVirt: ${KUBEVIRT_VERSION}"
  info "  CDI:      ${CDI_VERSION}"
  info "  virtctl:  ${BIN_DIR}/virtctl"
  info "============================================================"
}

main "$@"
