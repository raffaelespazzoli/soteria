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

# Creates two Minikube KVM2 clusters (east and west) with Cilium Cluster Mesh
# for cross-cluster pod-to-pod networking. This is the foundation for
# the multi-site integration test stack (Epic 14).
#
# Each cluster has 1 control-plane + 3 worker nodes backed by real VMs with
# an extra raw block disk per node for Rook-Ceph OSDs.
#
# Usage:
#   ./hack/multisite/setup-clusters.sh
#
# Environment Variables:
#   EAST_CLUSTER_NAME   Name of the east cluster profile (default: east)
#   WEST_CLUSTER_NAME   Name of the west cluster profile (default: west)
#   CILIUM_VERSION      Cilium Helm chart version to install (default: 1.19.5)
#   NODE_CPUS           vCPUs per node (default: 2)
#   MASTER_MEMORY       Memory for control-plane node in MB (default: 7168)
#   WORKER_MEMORY       Memory for worker nodes in MB (default: 5120)
#   DISK_SIZE           Disk size per node — applies to both system and extra disks (default: 30g)
#   KUBECONFIG_DIR      Directory for generated kubeconfigs (default: ./hack/multisite/.kubeconfigs)
#   CONNECTIVITY_TEST   Set to "0" to skip the cross-cluster connectivity smoke test

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
CILIUM_VERSION="${CILIUM_VERSION:-1.19.5}"
NODE_CPUS="${NODE_CPUS:-2}"
MASTER_MEMORY="${MASTER_MEMORY:-7168}"
WORKER_MEMORY="${WORKER_MEMORY:-6144}"
DISK_SIZE="${DISK_SIZE:-30g}"
KUBECONFIG_DIR="${KUBECONFIG_DIR:-${SCRIPT_DIR}/.kubeconfigs}"
CONNECTIVITY_TEST="${CONNECTIVITY_TEST:-1}"

BIN_DIR="${SCRIPT_DIR}/.bin"
MANIFESTS_DIR="${SCRIPT_DIR}/manifests"

EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"

# Non-overlapping CIDRs required for Cluster Mesh
EAST_POD_CIDR="10.10.0.0/16"
WEST_POD_CIDR="10.20.0.0/16"
EAST_SERVICE_CIDR="10.96.0.0/16"
WEST_SERVICE_CIDR="10.97.0.0/16"

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
ensure_minikube() {
  if command -v minikube &>/dev/null; then
    return 0
  fi

  if [[ -x "${BIN_DIR}/minikube" ]]; then
    export PATH="${BIN_DIR}:${PATH}"
    return 0
  fi

  info "minikube not found — downloading to ${BIN_DIR}/..."
  mkdir -p "${BIN_DIR}"

  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "${arch}" in
    x86_64)  arch="amd64" ;;
    aarch64) arch="arm64" ;;
  esac

  curl -fsSL "https://storage.googleapis.com/minikube/releases/latest/minikube-${os}-${arch}" \
    -o "${BIN_DIR}/minikube"
  chmod +x "${BIN_DIR}/minikube"

  export PATH="${BIN_DIR}:${PATH}"
  info "minikube installed: $(minikube version --short 2>/dev/null)"
}

ensure_cilium_cli() {
  if command -v cilium &>/dev/null; then
    return 0
  fi

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

  ensure_minikube
  command -v minikube &>/dev/null || fatal "minikube not available after download attempt"
  command -v kubectl &>/dev/null || fatal "kubectl not found. Install: https://kubernetes.io/docs/tasks/tools/"
  command -v helm &>/dev/null || fatal "helm not found. Install: https://helm.sh/docs/intro/install/"

  # Verify KVM is available
  if [[ ! -e /dev/kvm ]]; then
    fatal "/dev/kvm not found. Install: sudo dnf install qemu-kvm && sudo modprobe kvm"
  fi
  if [[ ! -r /dev/kvm ]] || [[ ! -w /dev/kvm ]]; then
    fatal "/dev/kvm not accessible. Ensure your user is in the appropriate group ($(stat -c '%G' /dev/kvm))"
  fi

  ensure_cilium_cli
  command -v cilium &>/dev/null || fatal "cilium CLI not available after download attempt"

  info "All prerequisites satisfied"
}

# ---------------------------------------------------------------------------
# Minikube cluster creation (idempotent)
# ---------------------------------------------------------------------------
create_cluster() {
  local name="$1" pod_cidr="$2" service_cidr="$3"

  if minikube status -p "${name}" &>/dev/null 2>&1; then
    info "Minikube cluster '${name}' already exists, skipping creation"
    return 0
  fi

  info "Creating Minikube KVM2 cluster '${name}' (4 nodes, ${NODE_CPUS} vCPU, master=${MASTER_MEMORY}MB / workers=${WORKER_MEMORY}MB RAM, ${DISK_SIZE} disk + extra disk)..."

  # Create with worker memory; control-plane node is resized after creation
  minikube start \
    --profile "${name}" \
    --driver=kvm2 \
    --nodes=4 \
    --kvm-network=default \
    --network=soteria-network \
    --cpus="${NODE_CPUS}" \
    --memory="${WORKER_MEMORY}" \
    --disk-size="${DISK_SIZE}" \
    --extra-disks=1 \
    --cni=false \
    --extra-config=kubeadm.pod-network-cidr="${pod_cidr}" \
    --service-cluster-ip-range="${service_cidr}" \
    --extra-config=kubeadm.skip-phases=addon/kube-proxy

  # Resize control-plane node to MASTER_MEMORY if different from workers.
  # virsh operates on the libvirt system URI where minikube KVM2 VMs live.
  # After changing the VM config, a normal minikube start brings everything
  # back with Kubernetes running.
  if [[ "${MASTER_MEMORY}" != "${WORKER_MEMORY}" ]]; then
    info "Resizing control-plane node '${name}' to ${MASTER_MEMORY}MB RAM..."
    local domain="${name}"
    minikube stop -p "${name}" --keep-context-active
    virsh -c qemu:///system setmaxmem "${domain}" "${MASTER_MEMORY}M" --config
    virsh -c qemu:///system setmem "${domain}" "${MASTER_MEMORY}M" --config
    minikube start -p "${name}"
  fi

  info "Minikube cluster '${name}' created"
}

# ---------------------------------------------------------------------------
# Cilium installation via Helm
# ---------------------------------------------------------------------------
install_cilium() {
  local context="$1" cluster_name="$2" cluster_id="$3"

  info "Installing Cilium via Helm on '${cluster_name}' (cluster.id=${cluster_id})..."

  helm repo add cilium https://helm.cilium.io/ 2>/dev/null || true
  helm repo update cilium

  local version_args=()
  if [[ -n "${CILIUM_VERSION}" ]]; then
    version_args=(--version "${CILIUM_VERSION}")
  fi

  # Get the IP directly from minikube
  local API_SERVER_IP=$(minikube -p "${cluster_name}" ip)

  # Default minikube API port
  local API_SERVER_PORT=8443

  info "Installing Gateway API CRDs (v1.4.1)..."
  local gw_crd_base="https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/v1.4.1/config/crd/standard"
  for crd in gatewayclasses gateways httproutes referencegrants grpcroutes; do
    kubectl --context "${context}" apply -f \
      "${gw_crd_base}/gateway.networking.k8s.io_${crd}.yaml"
  done

  helm upgrade --install --namespace kube-system \
    cilium cilium/cilium \
    --kube-context "${context}" \
    --values "${MANIFESTS_DIR}/cilium/values.yaml" \
    --set cluster.name="${cluster_name}" \
    --set cluster.id="${cluster_id}" \
    --set k8sServiceHost=${API_SERVER_IP} \
    --set k8sServicePort=${API_SERVER_PORT} \
    "${version_args[@]+"${version_args[@]}"}" \
    --wait --timeout 10m

  info "Waiting for Cilium readiness on '${cluster_name}'..."
  cilium status --wait --context "${context}"
  info "Cilium healthy on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# MetalLB (LoadBalancer IPs for Cluster Mesh apiserver)
# ---------------------------------------------------------------------------
deploy_metallb() {
  local cluster_name="$1"

  info "Intsalling MetalLB on '${cluster_name}'..."
  kubectl --context "${cluster_name}" apply -f https://raw.githubusercontent.com/metallb/metallb/v0.16.1/config/manifests/metallb-native.yaml

  info "Waiting for MetalLB to be ready on '${cluster_name}'..."
  kubectl --context "${cluster_name}" wait --for=condition=ready pod -l app=metallb -n metallb-system --timeout=180s

  # Derive an IP range from the cluster's node IP subnet.
  # Minikube's private network assigns IPs in a /24; we reserve .200-.220 for LBs.
  local subnet_prefix
  subnet_prefix="192.168.122"
  local lb_start_east="${subnet_prefix}.230"
  local lb_end_east="${subnet_prefix}.254"  
  local lb_start_west="${subnet_prefix}.215"
  local lb_end_west="${subnet_prefix}.229"

  info "Configuring MetalLB on '${cluster_name}' with range ${lb_start_east}-${lb_end_east} and ${lb_start_west}-${lb_end_west}..."



if [[ "${cluster_name}" == "${EAST_CLUSTER_NAME}" ]]; then
  kubectl --context "${cluster_name}" apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default-pool
  namespace: metallb-system
spec:
  addresses:
    - ${lb_start_east}-${lb_end_east}
EOF
else
  kubectl --context "${cluster_name}" apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default-pool
  namespace: metallb-system
spec:
  addresses:
    - ${lb_start_west}-${lb_end_west}
EOF
fi

kubectl --context "${cluster_name}" apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: default-l2
  namespace: metallb-system
spec:
  ipAddressPools:
    - default-pool
EOF

  info "MetalLB configured on '${cluster_name}'"
}


# ---------------------------------------------------------------------------
# Cluster Mesh
# ---------------------------------------------------------------------------
enable_cluster_mesh() {
  local context="$1" cluster_name="$2"

  info "Enabling Cluster Mesh on '${cluster_name}'..."
  cilium clustermesh enable --context "${context}" --service-type LoadBalancer

  info "Waiting for clustermesh-apiserver readiness on '${cluster_name}'..."
  kubectl --context "${context}" -n kube-system rollout status deployment/clustermesh-apiserver \
    --timeout=180s
  info "Cluster Mesh enabled on '${cluster_name}'"
}

connect_cluster_mesh() {
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
# Cross-cluster connectivity smoke test
# ---------------------------------------------------------------------------
connectivity_smoke_test() {
  if [[ "${CONNECTIVITY_TEST}" == "0" ]]; then
    info "Skipping connectivity smoke test (CONNECTIVITY_TEST=0)"
    return 0
  fi

  local test_ns="cilium-test-connectivity"

  cleanup_smoke_test() {
    kubectl --context "${EAST_CONTEXT}" delete namespace "${test_ns}" --ignore-not-found 2>/dev/null
    kubectl --context "${WEST_CONTEXT}" delete namespace "${test_ns}" --ignore-not-found 2>/dev/null
  }
  trap cleanup_smoke_test EXIT

  info "Running cross-cluster connectivity smoke test..."

  kubectl --context "${EAST_CONTEXT}" create namespace "${test_ns}" --dry-run=client -o yaml | \
    kubectl --context "${EAST_CONTEXT}" apply -f -
  kubectl --context "${WEST_CONTEXT}" create namespace "${test_ns}" --dry-run=client -o yaml | \
    kubectl --context "${WEST_CONTEXT}" apply -f -

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

  kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" wait --for=condition=Ready pod/east-probe --timeout=60s
  kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" wait --for=condition=Ready pod/west-probe --timeout=60s

  local east_ip west_ip
  east_ip="$(kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" get pod east-probe -o jsonpath='{.status.podIP}')"
  west_ip="$(kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" get pod west-probe -o jsonpath='{.status.podIP}')"

  info "East pod IP: ${east_ip}, West pod IP: ${west_ip}"

  info "Testing east -> west pod-IP connectivity..."
  kubectl --context "${EAST_CONTEXT}" -n "${test_ns}" exec east-probe -- \
    ping -c 3 -W 5 "${west_ip}" || fatal "East -> West pod-IP connectivity FAILED"
  info "East -> West pod-IP: OK"

  info "Testing west -> east pod-IP connectivity..."
  kubectl --context "${WEST_CONTEXT}" -n "${test_ns}" exec west-probe -- \
    ping -c 3 -W 5 "${east_ip}" || fatal "West -> East pod-IP connectivity FAILED"
  info "West -> East pod-IP: OK"

  info "Verifying Cluster Mesh connectivity report..."
  cilium clustermesh status --context "${EAST_CONTEXT}" --wait 2>&1 | grep -q "connected" \
    || fatal "Cluster Mesh not reporting connected state after pod-to-pod test passed"
  info "Cluster Mesh status: connected"

  cleanup_smoke_test
  trap - EXIT

  info "Cross-cluster connectivity smoke test PASSED"
}

# ---------------------------------------------------------------------------
# Kubeconfig export
# ---------------------------------------------------------------------------
export_kubeconfigs() {
  mkdir -p "${KUBECONFIG_DIR}"

  minikube -p "${EAST_CLUSTER_NAME}" kubectl -- config view --flatten > "${KUBECONFIG_DIR}/east.kubeconfig"
  minikube -p "${WEST_CLUSTER_NAME}" kubectl -- config view --flatten > "${KUBECONFIG_DIR}/west.kubeconfig"

  info "Kubeconfigs exported to ${KUBECONFIG_DIR}/"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
  info "=== Multisite Minikube KVM2 Cluster Setup ==="
  info "East cluster: ${EAST_CLUSTER_NAME}"
  info "West cluster: ${WEST_CLUSTER_NAME}"
  info "Resources: ${NODE_CPUS} vCPU, master=${MASTER_MEMORY}MB / workers=${WORKER_MEMORY}MB RAM, ${DISK_SIZE} disk + 1 extra disk"
  echo

  check_prerequisites

  # Create clusters with non-overlapping CIDRs (idempotent)
  create_cluster "${EAST_CLUSTER_NAME}" "${EAST_POD_CIDR}" "${EAST_SERVICE_CIDR}"
  create_cluster "${WEST_CLUSTER_NAME}" "${WEST_POD_CIDR}" "${WEST_SERVICE_CIDR}"

  # Install Cilium with unique cluster IDs
  install_cilium "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}" 1
  install_cilium "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}" 2

  # Deploy MetalLB for LoadBalancer service support (needed by Cluster Mesh)
  deploy_metallb "${EAST_CLUSTER_NAME}"
  deploy_metallb "${WEST_CLUSTER_NAME}"

  # Enable Cluster Mesh and connect
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
  info ""
  info "Extra disks for Rook-Ceph OSDs appear as /dev/vdb on each node (${DISK_SIZE} each)."
}

main "$@"
