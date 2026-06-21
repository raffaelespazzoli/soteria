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

# Creates two Kind clusters (east and west) with Cilium Cluster Mesh
# for cross-cluster pod-to-pod networking. This is the foundation for
# the multi-site integration test stack (Epic 14).
#
# Usage:
#   ./hack/multisite/setup-clusters.sh
#
# Environment Variables:
#   EAST_CLUSTER_NAME   Name of the east Kind cluster (default: east)
#   WEST_CLUSTER_NAME   Name of the west Kind cluster (default: west)
#   CILIUM_VERSION      Cilium version to install (default: auto-detect from CLI)
#   KUBECONFIG_DIR      Directory for generated kubeconfigs (default: ./hack/multisite/.kubeconfigs)
#   CONNECTIVITY_TEST   Set to "0" to skip the cross-cluster connectivity smoke test

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
CILIUM_VERSION="${CILIUM_VERSION:-}"
KUBECONFIG_DIR="${KUBECONFIG_DIR:-${SCRIPT_DIR}/.kubeconfigs}"
CONNECTIVITY_TEST="${CONNECTIVITY_TEST:-1}"

BIN_DIR="${SCRIPT_DIR}/.bin"
DATA_DIR="${SCRIPT_DIR}/.data"
EAST_CONFIG="${SCRIPT_DIR}/kind-east.yaml"
WEST_CONFIG="${SCRIPT_DIR}/kind-west.yaml"

EAST_CONTEXT="kind-${EAST_CLUSTER_NAME}"
WEST_CONTEXT="kind-${WEST_CLUSTER_NAME}"

# Validate cluster names are distinct
if [[ "${EAST_CLUSTER_NAME}" == "${WEST_CLUSTER_NAME}" ]]; then
  echo "[ERROR] EAST_CLUSTER_NAME and WEST_CLUSTER_NAME must differ (both set to '${EAST_CLUSTER_NAME}')" >&2
  exit 1
fi

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

# ---------------------------------------------------------------------------
# Binary download helpers
# ---------------------------------------------------------------------------
ensure_cilium_cli() {
  # If cilium is already on PATH, nothing to do
  if command -v cilium &>/dev/null; then
    return 0
  fi

  # If we previously downloaded it into .bin/, add to PATH
  if [[ -x "${BIN_DIR}/cilium" ]]; then
    export PATH="${BIN_DIR}:${PATH}"
    return 0
  fi

  info "cilium CLI not found — downloading to ${BIN_DIR}/..."
  mkdir -p "${BIN_DIR}"

  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "${arch}" in
    x86_64)  arch="amd64" ;;
    aarch64) arch="arm64" ;;
  esac

  local release_url="https://github.com/cilium/cilium-cli/releases/latest/download"
  local tarball="cilium-${os}-${arch}.tar.gz"
  local checksum_file="cilium-${os}-${arch}.tar.gz.sha256sum"

  curl -fsSL "${release_url}/${tarball}" -o "${BIN_DIR}/${tarball}"
  curl -fsSL "${release_url}/${checksum_file}" -o "${BIN_DIR}/${checksum_file}"

  # Verify checksum
  (cd "${BIN_DIR}" && sha256sum --check "${checksum_file}") || fatal "cilium CLI checksum verification failed"

  tar -xzf "${BIN_DIR}/${tarball}" -C "${BIN_DIR}"
  rm -f "${BIN_DIR}/${tarball}" "${BIN_DIR}/${checksum_file}"
  chmod +x "${BIN_DIR}/cilium"

  export PATH="${BIN_DIR}:${PATH}"
  info "cilium CLI installed: $(cilium version --client 2>/dev/null | head -1)"
}

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
check_prerequisites() {
  info "Checking prerequisites..."

  command -v kind &>/dev/null || fatal "kind CLI not found. Install: https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
  command -v kubectl &>/dev/null || fatal "kubectl not found. Install: https://kubernetes.io/docs/tasks/tools/"

  # Container runtime — prefer podman over docker
  if command -v podman &>/dev/null && podman info &>/dev/null 2>&1; then
    info "Container runtime: podman"
    export KIND_EXPERIMENTAL_PROVIDER="${KIND_EXPERIMENTAL_PROVIDER:-podman}"
    info "Set KIND_EXPERIMENTAL_PROVIDER=${KIND_EXPERIMENTAL_PROVIDER}"
  elif command -v docker &>/dev/null && docker info &>/dev/null 2>&1; then
    info "Container runtime: docker"
  else
    fatal "Neither podman nor docker is running. Kind requires a container runtime."
  fi

  # Download cilium CLI if missing
  ensure_cilium_cli
  command -v cilium &>/dev/null || fatal "cilium CLI not available after download attempt"

  local cilium_ver
  cilium_ver="$(cilium version --client 2>/dev/null | grep -oP 'cilium-cli: v\K[0-9]+\.[0-9]+' || echo "0.0")"
  local major minor
  major="${cilium_ver%%.*}"
  minor="${cilium_ver#*.}"
  if (( major == 0 && minor < 15 )); then
    warn "cilium CLI version ${cilium_ver} detected; v0.15.0+ recommended"
  fi

  # Disk space check — 20 GB minimum for 8 containers + images
  local avail_kb
  avail_kb="$(df --output=avail / 2>/dev/null | tail -1 | tr -d ' ')" || avail_kb=0
  if [[ -n "${avail_kb}" ]] && (( avail_kb < 20971520 )); then
    warn "Less than 20 GB free disk space detected ($(( avail_kb / 1048576 )) GB available). Setup may fail."
  fi

  # inotify limits — multi-node Kind clusters need many inotify instances.
  # Each Kind node container runs kubelet + containerd + cAdvisor, each consuming
  # inotify instances. Default of 128 is exhausted with 8 containers.
  local max_instances
  max_instances="$(cat /proc/sys/fs/inotify/max_user_instances 2>/dev/null)" || max_instances=0
  if (( max_instances < 512 )); then
    info "fs.inotify.max_user_instances is ${max_instances} (need ≥512 for 8 Kind nodes)..."
    if sysctl -w fs.inotify.max_user_instances=1024 &>/dev/null; then
      info "Increased via sysctl (running as root)"
    elif podman run --rm --privileged alpine sysctl -w fs.inotify.max_user_instances=1024 &>/dev/null 2>&1; then
      info "Increased via privileged podman container"
    elif docker run --rm --privileged alpine sysctl -w fs.inotify.max_user_instances=1024 &>/dev/null 2>&1; then
      info "Increased via privileged docker container"
    else
      warn "Could not increase inotify limits automatically."
      warn "Worker kubelets will likely fail with 'too many open files'."
      warn "Fix manually:  sudo sysctl -w fs.inotify.max_user_instances=1024"
      warn "Or persist:    echo 'fs.inotify.max_user_instances=1024' | sudo tee /etc/sysctl.d/99-kind.conf && sudo sysctl --system"
      fatal "Cannot proceed with fs.inotify.max_user_instances=${max_instances}"
    fi
  fi

  info "All prerequisites satisfied"
}

# ---------------------------------------------------------------------------
# Kind cluster creation (idempotent)
# ---------------------------------------------------------------------------
create_cluster() {
  local name="$1" config="$2"

  if kind get clusters 2>/dev/null | grep -qx "${name}"; then
    info "Kind cluster '${name}' already exists, skipping creation"
    return 0
  fi

  # Create OSD host directories so extraMounts can bind them
  mkdir -p "${DATA_DIR}/${name}/osd0" "${DATA_DIR}/${name}/osd1" "${DATA_DIR}/${name}/osd2"

  # Resolve __DATA_DIR__ placeholder in the Kind config template
  local resolved_config
  resolved_config="$(mktemp)"
  sed "s|__DATA_DIR__|${DATA_DIR}|g" "${config}" > "${resolved_config}"

  info "Creating Kind cluster '${name}'..."
  kind create cluster --name "${name}" --config "${resolved_config}"
  rm -f "${resolved_config}"
  info "Kind cluster '${name}' created"
}

# ---------------------------------------------------------------------------
# Cilium installation
# ---------------------------------------------------------------------------
install_cilium() {
  local context="$1" cluster_name="$2" cluster_id="$3"

  info "Installing Cilium on '${cluster_name}' (cluster.id=${cluster_id})..."

  local version_args=()
  if [[ -n "${CILIUM_VERSION}" ]]; then
    version_args=(--version "${CILIUM_VERSION}")
  fi

  cilium install --context "${context}" \
    --set cluster.name="${cluster_name}" \
    --set cluster.id="${cluster_id}" \
    "${version_args[@]+"${version_args[@]}"}"

  info "Waiting for Cilium readiness on '${cluster_name}'..."
  cilium status --wait --context "${context}"
  info "Cilium healthy on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Cluster Mesh
# ---------------------------------------------------------------------------
enable_cluster_mesh() {
  local context="$1" cluster_name="$2"

  info "Enabling Cluster Mesh on '${cluster_name}'..."
  cilium clustermesh enable --context "${context}" --service-type NodePort
  info "Cluster Mesh enabled on '${cluster_name}'"
}

connect_cluster_mesh() {
  # Wait for clustermesh-apiserver to be fully ready on both clusters
  info "Waiting for clustermesh-apiserver readiness..."
  kubectl --context "${EAST_CONTEXT}" -n kube-system rollout status deployment/clustermesh-apiserver --timeout=120s
  kubectl --context "${WEST_CONTEXT}" -n kube-system rollout status deployment/clustermesh-apiserver --timeout=120s

  info "Connecting Cluster Mesh: ${EAST_CLUSTER_NAME} <-> ${WEST_CLUSTER_NAME}..."
  cilium clustermesh connect \
    --context "${EAST_CONTEXT}" \
    --destination-context "${WEST_CONTEXT}" \
    --allow-mismatching-ca

  info "Waiting for Cluster Mesh health on '${EAST_CLUSTER_NAME}'..."
  cilium clustermesh status --wait --context "${EAST_CONTEXT}"

  info "Waiting for Cluster Mesh health on '${WEST_CLUSTER_NAME}'..."
  cilium clustermesh status --wait --context "${WEST_CONTEXT}"

  info "Cluster Mesh connected and healthy"
}

# ---------------------------------------------------------------------------
# Cross-cluster connectivity smoke test (AC4)
# ---------------------------------------------------------------------------
connectivity_smoke_test() {
  if [[ "${CONNECTIVITY_TEST}" == "0" ]]; then
    info "Skipping connectivity smoke test (CONNECTIVITY_TEST=0)"
    return 0
  fi

  local test_ns="cilium-test-connectivity"

  # Ensure cleanup on failure or interruption
  cleanup_smoke_test() {
    kubectl --context "${EAST_CONTEXT}" delete namespace "${test_ns}" --ignore-not-found 2>/dev/null
    kubectl --context "${WEST_CONTEXT}" delete namespace "${test_ns}" --ignore-not-found 2>/dev/null
  }
  trap cleanup_smoke_test EXIT

  info "Running cross-cluster connectivity smoke test..."

  # Create test namespace on both clusters
  kubectl --context "${EAST_CONTEXT}" create namespace "${test_ns}" --dry-run=client -o yaml | \
    kubectl --context "${EAST_CONTEXT}" apply -f -
  kubectl --context "${WEST_CONTEXT}" create namespace "${test_ns}" --dry-run=client -o yaml | \
    kubectl --context "${WEST_CONTEXT}" apply -f -

  # Deploy test pods (tolerate AlreadyExists from prior incomplete runs)
  if ! kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" get pod east-probe &>/dev/null; then
    kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" run east-probe \
      --image=busybox:1.36 --restart=Never --overrides='{"spec":{"terminationGracePeriodSeconds":0}}' \
      --command -- sleep 300
  fi
  if ! kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" get pod west-probe &>/dev/null; then
    kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" run west-probe \
      --image=busybox:1.36 --restart=Never --overrides='{"spec":{"terminationGracePeriodSeconds":0}}' \
      --command -- sleep 300
  fi

  # Wait for pods to be ready
  kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" wait --for=condition=Ready pod/east-probe --timeout=60s
  kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" wait --for=condition=Ready pod/west-probe --timeout=60s

  # Get pod IPs
  local east_ip west_ip
  east_ip="$(kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" get pod east-probe -o jsonpath='{.status.podIP}')"
  west_ip="$(kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" get pod west-probe -o jsonpath='{.status.podIP}')"

  info "East pod IP: ${east_ip}, West pod IP: ${west_ip}"

  # --- Direct pod-IP reachability (basic L3 check) ---
  info "Testing east -> west pod-IP connectivity..."
  kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" exec east-probe -- \
    ping -c 3 -W 5 "${west_ip}" || fatal "East -> West pod-IP connectivity FAILED"
  info "East -> West pod-IP: OK"

  info "Testing west -> east pod-IP connectivity..."
  kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" exec west-probe -- \
    ping -c 3 -W 5 "${east_ip}" || fatal "West -> East pod-IP connectivity FAILED"
  info "West -> East pod-IP: OK"

  # --- Verify Cluster Mesh status programmatically ---
  info "Verifying Cluster Mesh connectivity report..."
  cilium clustermesh status --context "${EAST_CONTEXT}" --wait 2>&1 | grep -q "connected" \
    || fatal "Cluster Mesh not reporting connected state after pod-to-pod test passed"
  info "Cluster Mesh status: connected"

  # Cleanup
  cleanup_smoke_test
  trap - EXIT

  info "Cross-cluster connectivity smoke test PASSED"
}

# ---------------------------------------------------------------------------
# Kubeconfig export
# ---------------------------------------------------------------------------
export_kubeconfigs() {
  mkdir -p "${KUBECONFIG_DIR}"

  kind get kubeconfig --name "${EAST_CLUSTER_NAME}" > "${KUBECONFIG_DIR}/east.kubeconfig"
  kind get kubeconfig --name "${WEST_CLUSTER_NAME}" > "${KUBECONFIG_DIR}/west.kubeconfig"

  info "Kubeconfigs exported to ${KUBECONFIG_DIR}/"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
  info "=== Multisite Kind Cluster Setup ==="
  info "East cluster: ${EAST_CLUSTER_NAME}"
  info "West cluster: ${WEST_CLUSTER_NAME}"
  echo

  check_prerequisites

  # Create clusters (idempotent)
  create_cluster "${EAST_CLUSTER_NAME}" "${EAST_CONFIG}"
  create_cluster "${WEST_CLUSTER_NAME}" "${WEST_CONFIG}"

  # Install Cilium with unique cluster IDs
  install_cilium "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}" 1
  install_cilium "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}" 2

  # Enable and connect Cluster Mesh
  enable_cluster_mesh "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  enable_cluster_mesh "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"
  connect_cluster_mesh

  # Connectivity test
  connectivity_smoke_test

  # Export kubeconfigs
  export_kubeconfigs

  echo
  info "=== Setup Complete ==="
  info ""
  info "Access clusters:"
  info "  kubectl --context ${EAST_CONTEXT} get nodes"
  info "  kubectl --context ${WEST_CONTEXT} get nodes"
  info ""
  info "Or use dedicated kubeconfigs:"
  info "  KUBECONFIG=${KUBECONFIG_DIR}/east.kubeconfig kubectl get nodes"
  info "  KUBECONFIG=${KUBECONFIG_DIR}/west.kubeconfig kubectl get nodes"
  info ""
  info "Cluster Mesh status:"
  info "  cilium clustermesh status --context ${EAST_CONTEXT}"
  info "  cilium clustermesh status --context ${WEST_CONTEXT}"
}

main "$@"
