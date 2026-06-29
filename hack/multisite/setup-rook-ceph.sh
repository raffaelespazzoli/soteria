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

# Deploys Rook-Ceph with RBD mirroring and VolumeReplication CRDs on both
# Minikube KVM2 clusters. Rook discovers the extra block disk (/dev/vdb) on
# each worker node via deviceFilter.
#
# Prerequisites:
#   - Minikube KVM2 clusters created by setup-clusters.sh (east + west)
#   - Cilium running as CNI on both clusters
#   - helm CLI available
#
# Usage:
#   ./hack/multisite/setup-rook-ceph.sh
#
# Environment Variables:
#   EAST_CLUSTER_NAME   Name of the east Minikube profile (default: east)
#   WEST_CLUSTER_NAME   Name of the west Minikube profile (default: west)
#   ROOK_CHART_VERSION  Rook Helm chart version (default: latest)
#   SMOKE_TEST          Set to "0" to skip the replication smoke test
#   CSI_ADDONS_TAG      CSI Addons release tag (default: v0.14.0)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
ROOK_CHART_VERSION="${ROOK_CHART_VERSION:-}"
SMOKE_TEST="${SMOKE_TEST:-1}"
CSI_ADDONS_TAG="${CSI_ADDONS_TAG:-v0.14.0}"

MANIFESTS_DIR="${SCRIPT_DIR}/manifests/rook-ceph"
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

# Render a YAML template by substituting only the listed env vars.
# Usage: render_template <file> VAR1 VAR2 ...
render_template() {
  local file="$1"; shift
  local vars=""
  for v in "$@"; do
    vars+='$'"${v} "
  done
  envsubst "${vars}" < "${file}"
}

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
check_prerequisites() {
  info "Checking prerequisites..."

  command -v kubectl &>/dev/null || fatal "kubectl not found"
  command -v helm &>/dev/null || fatal "helm not found. Install: https://helm.sh/docs/intro/install/"
  command -v minikube &>/dev/null || fatal "minikube not found. Install: https://minikube.sigs.k8s.io/"
  command -v envsubst &>/dev/null || fatal "envsubst not found (part of gettext)"

  # Verify Minikube clusters are running
  if ! minikube status -p "${EAST_CLUSTER_NAME}" &>/dev/null 2>&1; then
    fatal "Minikube cluster '${EAST_CLUSTER_NAME}' not running. Run setup-clusters.sh first."
  fi
  if ! minikube status -p "${WEST_CLUSTER_NAME}" &>/dev/null 2>&1; then
    fatal "Minikube cluster '${WEST_CLUSTER_NAME}' not running. Run setup-clusters.sh first."
  fi

  # Verify nodes are ready
  local east_nodes west_nodes
  east_nodes="$(keast get nodes --no-headers 2>/dev/null | wc -l)"
  west_nodes="$(kwest get nodes --no-headers 2>/dev/null | wc -l)"
  [[ "${east_nodes}" -ge 2 ]] || fatal "East cluster has fewer than 2 nodes"
  [[ "${west_nodes}" -ge 2 ]] || fatal "West cluster has fewer than 2 nodes"

  info "All prerequisites satisfied"
}

# ---------------------------------------------------------------------------
# Ensure /data/rook exists on all cluster nodes (persistent across restarts)
# ---------------------------------------------------------------------------
ensure_data_dir() {
  local cluster_name="$1"

  info "Ensuring /data/rook directory exists on all '${cluster_name}' nodes..."

  local nodes
  nodes="$(minikube profile list -o json 2>/dev/null \
    | python3 -c "
import json, sys
for p in json.load(sys.stdin)['valid']:
  if p['Name'] == '${cluster_name}':
    for n in p.get('Nodes', [{'Name': '${cluster_name}'}]):
      print(n['Name'])
" 2>/dev/null || echo "${cluster_name}")"

  if [[ -z "${nodes}" ]]; then
    nodes="${cluster_name}"
  fi

  for node in ${nodes}; do
    minikube ssh -p "${cluster_name}" -n "${node}" -- \
      "sudo mkdir -p /data/rook" 2>/dev/null || true
  done
}

# ---------------------------------------------------------------------------
# Task 2: Deploy Rook operator
# ---------------------------------------------------------------------------
deploy_rook_operator() {
  local context="$1" cluster_name="$2"

  info "Deploying Rook operator on '${cluster_name}'..."

  # Add Helm repo (idempotent)
  helm repo add rook-release https://charts.rook.io/release 2>/dev/null || true
  helm repo update rook-release

  local version_args=()
  if [[ -n "${ROOK_CHART_VERSION}" ]]; then
    version_args=(--version "${ROOK_CHART_VERSION}")
  fi

  helm upgrade --install --create-namespace --namespace rook-ceph \
    rook-ceph rook-release/rook-ceph \
    --kube-context "${context}" \
    --values "${MANIFESTS_DIR}/operator-values.yaml" \
    "${version_args[@]+"${version_args[@]}"}" \
    --wait --timeout 5m

  info "Rook operator deployed on '${cluster_name}'"
}

wait_rook_operator() {
  local context="$1" cluster_name="$2"

  info "Waiting for Rook operator readiness on '${cluster_name}'..."
  kubectl --context "${context}" -n rook-ceph rollout status deployment/rook-ceph-operator \
    --timeout=300s
  info "Rook operator ready on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Task 1b: Deploy ceph-csi-drivers chart (creates OperatorConfig + Driver CRs)
# ---------------------------------------------------------------------------
deploy_csi_drivers() {
  local context="$1" cluster_name="$2"

  info "Deploying ceph-csi-drivers chart on '${cluster_name}'..."

  helm repo add ceph-csi-operator https://ceph.github.io/ceph-csi-operator 2>/dev/null || true
  helm repo update ceph-csi-operator

  helm upgrade --install --namespace rook-ceph \
    ceph-csi-drivers ceph-csi-operator/ceph-csi-drivers \
    --kube-context "${context}" \
    --values "${MANIFESTS_DIR}/csi-drivers-values.yaml" \
    --wait --timeout 5m

  info "ceph-csi-drivers chart deployed on '${cluster_name}'"
}

wait_csi_drivers() {
  local context="$1" cluster_name="$2"

  info "Waiting for CSI driver pods on '${cluster_name}'..."

  local retries=30
  local delay=10
  for ((i=1; i<=retries; i++)); do
    local rbd_ready
    rbd_ready="$(kubectl --context "${context}" -n rook-ceph get deployment \
      rook-ceph.rbd.csi.ceph.com-ctrlplugin \
      -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")"

    if [[ "${rbd_ready:-0}" -ge 1 ]]; then
      info "CSI RBD driver ready on '${cluster_name}'"
      return 0
    fi

    if [[ $((i % 6)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: waiting for CSI driver pods..."
    fi
    sleep "${delay}"
  done

  fatal "CSI driver pods did not become ready on '${cluster_name}' within $((retries * delay))s"
}

# ---------------------------------------------------------------------------
# Task 2: Create CephCluster (direct device discovery via deviceFilter)
# ---------------------------------------------------------------------------
deploy_ceph_cluster() {
  local context="$1" cluster_name="$2"

  info "Applying CephCluster on '${cluster_name}'..."
  export CLUSTER_ID="${cluster_name}"
  render_template "${MANIFESTS_DIR}/ceph-cluster.yaml" CLUSTER_ID \
    | kubectl --context "${context}" apply -f -
  info "CephCluster CR applied on '${cluster_name}' (device discovery: /dev/vdb)"
}

wait_ceph_cluster() {
  local context="$1" cluster_name="$2"

  info "Waiting for CephCluster to reach Ready phase on '${cluster_name}'..."

  local retries=60
  local delay=60
  for ((i=1; i<=retries; i++)); do
    local phase
    phase="$(kubectl --context "${context}" -n rook-ceph get cephcluster rook-ceph \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"

    local health
    health="$(kubectl --context "${context}" -n rook-ceph get cephcluster rook-ceph \
      -o jsonpath='{.status.ceph.health}' 2>/dev/null || echo "")"

    if [[ "${phase}" == "Ready" ]]; then
      if [[ "${health}" == "HEALTH_ERR" ]]; then
        fatal "CephCluster reached Ready phase but Ceph health is HEALTH_ERR on '${cluster_name}'"
      fi
      info "CephCluster is Ready on '${cluster_name}' (health: ${health:-OK})"
      return 0
    fi

    if [[ $((i % 6)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: phase=${phase:-pending}, health=${health:-unknown}"
    fi
    sleep "${delay}"
  done

  fatal "CephCluster did not reach Ready phase on '${cluster_name}' within $((retries * delay))s"
}

# ---------------------------------------------------------------------------
# Task 3: Export mon and OSD services via MCS-API (ServiceExport)
# ---------------------------------------------------------------------------
export_ceph_services() {
  local context="$1" cluster_name="$2"

  info "Exporting Ceph mon and OSD services via MCS-API on '${cluster_name}'..."

  # Discover all mon and OSD services
  local services
  services="$(kubectl --context "${context}" -n rook-ceph get svc \
    -l "app in (rook-ceph-mon, rook-ceph-osd)" \
    -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)"

  if [[ -z "${services}" ]]; then
    warn "No mon/OSD services found on '${cluster_name}' — skipping ServiceExport"
    return 0
  fi

  local count=0
  for svc in ${services}; do
    export SERVICE_NAME="${svc}"
    render_template "${MANIFESTS_DIR}/service-export.yaml" SERVICE_NAME \
      | kubectl --context "${context}" apply -f -
    count=$((count + 1))
  done

  info "Exported ${count} Ceph service(s) on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Task 4: CephBlockPool with mirroring
# ---------------------------------------------------------------------------
deploy_ceph_blockpool() {
  local context="$1" cluster_name="$2"

  info "Applying CephBlockPool (mirrored-pool) on '${cluster_name}'..."
  kubectl --context "${context}" apply -f "${MANIFESTS_DIR}/ceph-blockpool.yaml"

  info "Deploying CephRBDMirror on '${cluster_name}'..."
  kubectl --context "${context}" apply -f "${MANIFESTS_DIR}/ceph-rbd-mirror.yaml"
}

wait_blockpool_ready() {
  local context="$1" cluster_name="$2"

  info "Waiting for CephBlockPool mirroring readiness on '${cluster_name}'..."

  local retries=30
  local delay=10
  for ((i=1; i<=retries; i++)); do
    local pool_phase
    pool_phase="$(kubectl --context "${context}" -n rook-ceph get cephblockpool mirrored-pool \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"

    if [[ "${pool_phase}" == "Ready" ]]; then
      info "CephBlockPool ready on '${cluster_name}'"
      return 0
    fi

    if [[ $((i % 6)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: phase=${pool_phase:-pending}"
    fi
    sleep "${delay}"
  done

  fatal "CephBlockPool did not become Ready on '${cluster_name}' within $((retries * delay))s"
}

# ---------------------------------------------------------------------------
# Task 5: Bootstrap peer exchange for RBD mirroring
# ---------------------------------------------------------------------------
get_bootstrap_peer_secret_name() {
  local context="$1"

  local retries=30
  local delay=5
  for ((i=1; i<=retries; i++)); do
    local secret_name
    secret_name="$(kubectl --context "${context}" -n rook-ceph get cephblockpool mirrored-pool \
      -o jsonpath='{.status.info.rbdMirrorBootstrapPeerSecretName}' 2>/dev/null || echo "")"

    if [[ -n "${secret_name}" ]]; then
      echo "${secret_name}"
      return 0
    fi
    sleep "${delay}"
  done

  fatal "Bootstrap peer secret name not found on context ${context}"
}

exchange_peer_secrets() {
  info "Exchanging RBD mirroring bootstrap peer secrets..."

  # Get east bootstrap secret
  info "  Extracting bootstrap peer from east..."
  local east_secret_name
  east_secret_name="$(get_bootstrap_peer_secret_name "${EAST_CONTEXT}")"
  local east_token
  east_token="$(keast -n rook-ceph get secret "${east_secret_name}" \
    -o jsonpath='{.data.token}' | base64 -d)"

  if [[ -z "${east_token}" ]]; then
    fatal "East bootstrap peer token is empty"
  fi

  # Get west bootstrap secret
  info "  Extracting bootstrap peer from west..."
  local west_secret_name
  west_secret_name="$(get_bootstrap_peer_secret_name "${WEST_CONTEXT}")"
  local west_token
  west_token="$(kwest -n rook-ceph get secret "${west_secret_name}" \
    -o jsonpath='{.data.token}' | base64 -d)"

  if [[ -z "${west_token}" ]]; then
    fatal "West bootstrap peer token is empty"
  fi

  # Import east's token into west (if not already present)
  if ! kwest -n rook-ceph get secret rbd-mirror-peer-token-east &>/dev/null; then
    info "  Importing east's peer secret into west..."
    export PEER_SECRET_NAME="rbd-mirror-peer-token-east"
    export PEER_TOKEN_B64="$(echo -n "${east_token}" | base64 -w0)"
    export PEER_POOL_B64="$(echo -n "mirrored-pool" | base64 -w0)"
    render_template "${MANIFESTS_DIR}/peer-secret.yaml" PEER_SECRET_NAME PEER_TOKEN_B64 PEER_POOL_B64 \
      | kwest apply -f -
  else
    info "  East peer secret already exists on west, skipping"
  fi

  # Patch west CephBlockPool with east peer secret
  kwest -n rook-ceph patch cephblockpool mirrored-pool --type merge -p \
    '{"spec":{"mirroring":{"peers":{"secretNames":["rbd-mirror-peer-token-east"]}}}}'

  # Import west's token into east (if not already present)
  if ! keast -n rook-ceph get secret rbd-mirror-peer-token-west &>/dev/null; then
    info "  Importing west's peer secret into east..."
    export PEER_SECRET_NAME="rbd-mirror-peer-token-west"
    export PEER_TOKEN_B64="$(echo -n "${west_token}" | base64 -w0)"
    export PEER_POOL_B64="$(echo -n "mirrored-pool" | base64 -w0)"
    render_template "${MANIFESTS_DIR}/peer-secret.yaml" PEER_SECRET_NAME PEER_TOKEN_B64 PEER_POOL_B64 \
      | keast apply -f -
  else
    info "  West peer secret already exists on east, skipping"
  fi

  # Patch east CephBlockPool with west peer secret
  keast -n rook-ceph patch cephblockpool mirrored-pool --type merge -p \
    '{"spec":{"mirroring":{"peers":{"secretNames":["rbd-mirror-peer-token-west"]}}}}'

  info "Peer secrets exchanged"
}

wait_mirror_healthy() {
  info "Waiting for RBD mirror pool status to be healthy on both clusters..."

  local retries=30
  local delay=10
  for ((i=1; i<=retries; i++)); do
    local east_mirror_ready
    east_mirror_ready="$(keast -n rook-ceph get pods -l app=rook-ceph-rbd-mirror \
      -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "")"
    local west_mirror_ready
    west_mirror_ready="$(kwest -n rook-ceph get pods -l app=rook-ceph-rbd-mirror \
      -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "")"

    if [[ "${east_mirror_ready}" == "Running" && "${west_mirror_ready}" == "Running" ]]; then
      local east_mirror_status
      east_mirror_status="$(keast -n rook-ceph get cephblockpool mirrored-pool \
        -o jsonpath='{.status.mirroringStatus.summary.health}' 2>/dev/null || echo "")"
      local west_mirror_status
      west_mirror_status="$(kwest -n rook-ceph get cephblockpool mirrored-pool \
        -o jsonpath='{.status.mirroringStatus.summary.health}' 2>/dev/null || echo "")"

      if [[ "${east_mirror_status}" == "OK" && "${west_mirror_status}" == "OK" ]]; then
        info "RBD mirroring is healthy on both clusters"
        return 0
      fi

      if [[ $((i % 6)) -eq 0 ]]; then
        info "  Attempt ${i}/${retries}: daemons running, east-health=${east_mirror_status:-pending}, west-health=${west_mirror_status:-pending}"
      fi
    else
      if [[ $((i % 6)) -eq 0 ]]; then
        info "  Attempt ${i}/${retries}: east-mirror=${east_mirror_ready:-pending}, west-mirror=${west_mirror_ready:-pending}"
      fi
    fi
    sleep "${delay}"
  done

  fatal "RBD mirroring did not become healthy on both clusters within $((retries * delay))s. Check: kubectl --context ${EAST_CONTEXT} -n rook-ceph get cephblockpool mirrored-pool -o yaml"
}

# ---------------------------------------------------------------------------
# Task 6: CSI Addons deployment
# ---------------------------------------------------------------------------
deploy_csi_addons() {
  local context="$1" cluster_name="$2"

  info "Configuring CSI Addons on '${cluster_name}'..."

  # Deploy CSI Addons controller from pinned release (CRDs + RBAC + controller)
  # Note: the CSI Addons sidecars in the RBD/CephFS pods are enabled via
  # deployCsiAddons: true in csi-drivers-values.yaml. This controller is the
  # "brain" that processes VolumeReplication CRs and coordinates with those sidecars.
  local csi_addons_base="https://github.com/csi-addons/kubernetes-csi-addons/releases/download/${CSI_ADDONS_TAG}"

  info "  Ensuring csi-addons-system namespace exists on '${cluster_name}'..."
  kubectl --context "${context}" create namespace csi-addons-system --dry-run=client -o yaml \
    | kubectl --context "${context}" apply -f -

  info "  Applying CSI Addons CRDs on '${cluster_name}'..."
  kubectl --context "${context}" apply -f "${csi_addons_base}/crds.yaml"

  info "  Applying CSI Addons RBAC on '${cluster_name}'..."
  kubectl --context "${context}" apply -f "${csi_addons_base}/rbac.yaml"

  info "  Applying CSI Addons controller on '${cluster_name}'..."
  kubectl --context "${context}" apply -f "${csi_addons_base}/setup-controller.yaml"

  info "CSI Addons deployed on '${cluster_name}'"
}

wait_csi_addons() {
  local context="$1" cluster_name="$2"

  info "Waiting for CSI Addons controller readiness on '${cluster_name}'..."

  local retries=30
  local delay=10
  for ((i=1; i<=retries; i++)); do
    if kubectl --context "${context}" -n csi-addons-system get deployment csi-addons-controller-manager &>/dev/null 2>&1; then
      local ready
      ready="$(kubectl --context "${context}" -n csi-addons-system get deployment csi-addons-controller-manager \
        -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")"
      if [[ "${ready}" -ge 1 ]]; then
        info "CSI Addons controller ready on '${cluster_name}'"
        return 0
      fi
    fi

    if [[ $((i % 6)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: waiting for csi-addons-controller-manager..."
    fi
    sleep "${delay}"
  done

  warn "CSI Addons controller readiness timed out on '${cluster_name}'"
}

verify_csi_addons_crds() {
  local context="$1" cluster_name="$2"

  info "Verifying VolumeReplication CRDs on '${cluster_name}'..."

  local missing=0
  for crd in volumereplications.replication.storage.openshift.io \
             volumereplicationclasses.replication.storage.openshift.io \
             volumegroupreplications.replication.storage.openshift.io; do
    if ! kubectl --context "${context}" get crd "${crd}" &>/dev/null; then
      error "  CRD ${crd} not found on '${cluster_name}'"
      missing=$((missing + 1))
    fi
  done

  if [[ ${missing} -gt 0 ]]; then
    fatal "${missing} VolumeReplication CRDs missing on '${cluster_name}'"
  fi

  info "  All VolumeReplication CRDs registered on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Task 7: VolumeReplicationClass and StorageClass
# ---------------------------------------------------------------------------
deploy_storage_resources() {
  local context="$1" cluster_name="$2"

  info "Applying VolumeReplicationClass on '${cluster_name}'..."
  kubectl --context "${context}" apply -f "${MANIFESTS_DIR}/volume-replication-class.yaml"

  info "Applying StorageClass on '${cluster_name}'..."
  kubectl --context "${context}" apply -f "${MANIFESTS_DIR}/storage-class.yaml"

  info "Storage resources applied on '${cluster_name}'"
}

# ---------------------------------------------------------------------------
# Task 8: Replication smoke test
# ---------------------------------------------------------------------------
replication_smoke_test() {
  if [[ "${SMOKE_TEST}" == "0" ]]; then
    info "Skipping replication smoke test (SMOKE_TEST=0)"
    return 0
  fi

  info "Running replication smoke test..."

  local test_ns="rook-ceph-test"

  # --- East side: primary PVC + VolumeReplication ---

  keast create namespace "${test_ns}" --dry-run=client -o yaml | keast apply -f -

  info "  Creating test PVC on east..."
  export TEST_NAMESPACE="${test_ns}"
  render_template "${MANIFESTS_DIR}/test-pvc.yaml" TEST_NAMESPACE \
    | keast apply -f -

  # Wait for PVC to bind
  info "  Waiting for test PVC to bind on east..."
  local retries=30
  local delay=5
  for ((i=1; i<=retries; i++)); do
    local pvc_phase
    pvc_phase="$(keast -n "${test_ns}" get pvc test-pvc -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${pvc_phase}" == "Bound" ]]; then
      info "  Test PVC bound successfully on east"
      break
    fi
    if [[ ${i} -eq ${retries} ]]; then
      error "  Test PVC did not bind on east within $((retries * delay))s"
      info "  Resources left in place for troubleshooting in namespace '${test_ns}'"
      return 1
    fi
    sleep "${delay}"
  done

  # Create VolumeReplication CR on east (primary)
  info "  Creating VolumeReplication CR (primary) on east..."
  render_template "${MANIFESTS_DIR}/test-volume-replication.yaml" TEST_NAMESPACE \
    | keast apply -f -

  # Verify east VolumeReplication reaches Primary state
  info "  Checking VolumeReplication status on east..."
  local vr_ready=false
  retries=30
  delay=10
  for ((i=1; i<=retries; i++)); do
    local vr_state
    vr_state="$(keast -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"

    local completed
    completed="$(keast -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.conditions[?(@.type=="Completed")].status}' 2>/dev/null || echo "")"

    if [[ "${vr_state}" == "Primary" && "${completed}" == "True" ]]; then
      info "  East VolumeReplication state: Primary, Completed: True"
      vr_ready=true
      break
    fi

    if [[ $((i % 3)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: state=${vr_state:-pending}, completed=${completed:-unknown}"
    fi
    sleep "${delay}"
  done

  if [[ "${vr_ready}" != "true" ]]; then
    error "  Replication smoke test FAILED — east VolumeReplication did not reach Primary state"
    info "  Resources left in place for troubleshooting in namespace '${test_ns}'"
    info "  Check: kubectl --context ${EAST_CONTEXT} -n ${test_ns} get volumereplication test-vr -o yaml"
    return 1
  fi

  # --- West side: secondary pre-provisioned PV/PVC + VolumeReplication ---

  info "  Setting up secondary (west) side..."
  kwest create namespace "${test_ns}" --dry-run=client -o yaml | kwest apply -f -

  # Retrieve PV details from east to create a pre-provisioned PV on west
  local pv_name volume_handle image_name
  pv_name="$(keast -n "${test_ns}" get pvc test-pvc -o jsonpath='{.spec.volumeName}')"
  volume_handle="$(keast get pv "${pv_name}" -o jsonpath='{.spec.csi.volumeHandle}')"
  image_name="$(keast get pv "${pv_name}" -o jsonpath='{.spec.csi.volumeAttributes.imageName}')"
  local pv_size
  pv_size="$(keast get pv "${pv_name}" -o jsonpath='{.spec.capacity.storage}')"

  # Rewrite the pool ID in the volume handle for the west cluster.
  # Handle format: <ver>-<clusterID-len-hex>-<clusterID>-<poolID-hex-16>-<uuid>
  # Pool IDs may differ between clusters (e.g., east pool 1, west pool 2).
  local west_pool_id
  west_pool_id="$(kwest exec -n rook-ceph deployment/rook-ceph-tools -- \
    ceph osd pool ls detail --format json 2>/dev/null \
    | python3 -c "import json,sys; pools=json.load(sys.stdin); print(next(p['pool_id'] for p in pools if p['pool_name']=='mirrored-pool'))")"
  local west_pool_hex
  west_pool_hex="$(printf '%016x' "${west_pool_id}")"
  # Replace the 16-hex-char pool field (characters after the clusterID segment)
  volume_handle="$(echo "${volume_handle}" | sed -E "s/^(0001-0009-rook-ceph-)[0-9a-f]{16}(-.*)/\1${west_pool_hex}\2/")"
  info "  Adjusted volume handle for west pool ID ${west_pool_id}: ${volume_handle}"

  info "  Creating pre-provisioned PV on west (image: ${image_name})..."
  kwest apply -f - <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: ${pv_name}
spec:
  capacity:
    storage: ${pv_size}
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  storageClassName: rook-ceph-block
  volumeMode: Filesystem
  csi:
    driver: rook-ceph.rbd.csi.ceph.com
    volumeHandle: ${volume_handle}
    volumeAttributes:
      clusterID: rook-ceph
      imageFeatures: "layering,exclusive-lock"
      imageFormat: "2"
      imageName: ${image_name}
      journalPool: mirrored-pool
      pool: mirrored-pool
    nodeStageSecretRef:
      name: rook-csi-rbd-node
      namespace: rook-ceph
    controllerExpandSecretRef:
      name: rook-csi-rbd-provisioner
      namespace: rook-ceph
EOF

  info "  Creating PVC on west (binding to pre-provisioned PV)..."
  kwest apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: test-pvc
  namespace: ${test_ns}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: ${pv_size}
  storageClassName: rook-ceph-block
  volumeName: ${pv_name}
EOF

  # Wait for west PVC to bind
  info "  Waiting for west PVC to bind..."
  retries=12
  delay=5
  for ((i=1; i<=retries; i++)); do
    local west_pvc_phase
    west_pvc_phase="$(kwest -n "${test_ns}" get pvc test-pvc -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ "${west_pvc_phase}" == "Bound" ]]; then
      info "  West PVC bound successfully"
      break
    fi
    if [[ ${i} -eq ${retries} ]]; then
      error "  West PVC did not bind within $((retries * delay))s"
      info "  Resources left in place for troubleshooting in namespace '${test_ns}'"
      return 1
    fi
    sleep "${delay}"
  done

  # Create VolumeReplication CR on west (secondary)
  info "  Creating VolumeReplication CR (secondary) on west..."
  kwest apply -f - <<EOF
apiVersion: replication.storage.openshift.io/v1alpha1
kind: VolumeReplication
metadata:
  name: test-vr
  namespace: ${test_ns}
spec:
  volumeReplicationClass: rook-ceph-rbd-vrc
  replicationState: secondary
  dataSource:
    kind: PersistentVolumeClaim
    name: test-pvc
EOF

  # Verify west VolumeReplication reaches Secondary state
  info "  Checking VolumeReplication status on west..."
  local west_vr_ready=false
  retries=30
  delay=10
  for ((i=1; i<=retries; i++)); do
    local west_vr_state
    west_vr_state="$(kwest -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"

    local west_completed
    west_completed="$(kwest -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.conditions[?(@.type=="Completed")].status}' 2>/dev/null || echo "")"

    if [[ "${west_vr_state}" == "Secondary" && "${west_completed}" == "True" ]]; then
      info "  West VolumeReplication state: Secondary, Completed: True"
      west_vr_ready=true
      break
    fi

    if [[ $((i % 3)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: state=${west_vr_state:-pending}, completed=${west_completed:-unknown}"
    fi
    sleep "${delay}"
  done

  if [[ "${west_vr_ready}" != "true" ]]; then
    error "  Replication smoke test FAILED — west VolumeReplication did not reach Secondary state"
    info "  Resources left in place for troubleshooting in namespace '${test_ns}'"
    info "  Check: kubectl --context ${WEST_CONTEXT} -n ${test_ns} get volumereplication test-vr -o yaml"
    return 1
  fi

  # Verify image is replaying on west via Ceph CLI
  info "  Verifying RBD image replication via Ceph CLI..."
  local mirror_status
  mirror_status="$(kwest exec -n rook-ceph deployment/rook-ceph-tools -- \
    rbd mirror pool status mirrored-pool 2>/dev/null | grep -c "replaying" || echo "0")"

  if [[ "${mirror_status}" -ge 1 ]]; then
    info "  RBD mirror confirms image replaying on west"
  else
    warn "  RBD mirror pool shows no replaying images (may need more time)"
  fi

  info "  Replication smoke test PASSED (east=Primary, west=Secondary, image replaying)"
  info ""
  info "  Test resources remain in namespace '${test_ns}' on both clusters."
  info "  To clean up manually:"
  info "    kubectl --context ${EAST_CONTEXT} delete ns ${test_ns}"
  info "    kubectl --context ${WEST_CONTEXT} delete ns ${test_ns}"
  info "    kubectl --context ${WEST_CONTEXT} delete pv ${pv_name}"
  return 0
}

# ---------------------------------------------------------------------------
# Task 9: Failover smoke test
# ---------------------------------------------------------------------------
failover_smoke_test() {
  if [[ "${SMOKE_TEST}" == "0" ]]; then
    info "Skipping failover smoke test (SMOKE_TEST=0)"
    return 0
  fi

  info "Running failover smoke test..."

  local test_ns="rook-ceph-test"
  local retries delay

  # Verify starting state: east=Primary, west=Secondary
  local east_state west_state
  east_state="$(keast -n "${test_ns}" get volumereplication test-vr \
    -o jsonpath='{.status.state}' 2>/dev/null || echo "")"
  west_state="$(kwest -n "${test_ns}" get volumereplication test-vr \
    -o jsonpath='{.status.state}' 2>/dev/null || echo "")"

  if [[ "${east_state}" != "Primary" || "${west_state}" != "Secondary" ]]; then
    error "  Cannot run failover test: expected east=Primary, west=Secondary but got east=${east_state:-missing}, west=${west_state:-missing}"
    info "  Run replication_smoke_test first to set up the initial state"
    return 1
  fi

  info "  Starting state confirmed: east=Primary, west=Secondary"

  # --- Step 1: Planned failover (east → secondary, west → primary) ---

  info "  Step 1: Demoting east to secondary..."
  keast -n "${test_ns}" patch volumereplication test-vr \
    --type=merge -p '{"spec":{"replicationState":"secondary"}}'

  # Wait for east to reach Secondary state
  retries=30
  delay=10
  for ((i=1; i<=retries; i++)); do
    east_state="$(keast -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"
    if [[ "${east_state}" == "Secondary" ]]; then
      info "  East demoted to Secondary"
      break
    fi
    if [[ ${i} -eq ${retries} ]]; then
      error "  Failover FAILED — east did not reach Secondary within $((retries * delay))s"
      info "  Resources left for troubleshooting in namespace '${test_ns}'"
      return 1
    fi
    if [[ $((i % 3)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: east state=${east_state:-pending}"
    fi
    sleep "${delay}"
  done

  info "  Step 2: Promoting west to primary..."
  kwest -n "${test_ns}" patch volumereplication test-vr \
    --type=merge -p '{"spec":{"replicationState":"primary"}}'

  # Wait for west to reach Primary state
  retries=30
  delay=10
  for ((i=1; i<=retries; i++)); do
    west_state="$(kwest -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"

    local west_completed
    west_completed="$(kwest -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.conditions[?(@.type=="Completed")].status}' 2>/dev/null || echo "")"

    if [[ "${west_state}" == "Primary" && "${west_completed}" == "True" ]]; then
      info "  West promoted to Primary"
      break
    fi
    if [[ ${i} -eq ${retries} ]]; then
      error "  Failover FAILED — west did not reach Primary within $((retries * delay))s"
      info "  Resources left for troubleshooting in namespace '${test_ns}'"
      return 1
    fi
    if [[ $((i % 3)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: west state=${west_state:-pending}, completed=${west_completed:-unknown}"
    fi
    sleep "${delay}"
  done

  info "  Failover complete: east=Secondary, west=Primary"

  # --- Step 2: Failback (east resync while west is still primary, then swap) ---
  # Resync east BEFORE demoting west — east needs a primary peer to resync from.

  info "  Step 3: Requesting resync on east (west is still primary)..."
  keast -n "${test_ns}" patch volumereplication test-vr \
    --type=merge -p '{"spec":{"replicationState":"resync"}}'

  # Wait for east to complete resync (state goes back to Secondary with Completed=True)
  info "  Waiting for east resync to complete..."
  retries=30
  delay=10
  for ((i=1; i<=retries; i++)); do
    east_state="$(keast -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"

    local east_completed
    east_completed="$(keast -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.conditions[?(@.type=="Completed")].status}' 2>/dev/null || echo "")"

    if [[ "${east_state}" == "Secondary" && "${east_completed}" == "True" ]]; then
      info "  East resync completed (state: Secondary, ready for promotion)"
      break
    fi
    if [[ ${i} -eq ${retries} ]]; then
      error "  Failback FAILED — east resync did not complete within $((retries * delay))s"
      info "  Resources left for troubleshooting in namespace '${test_ns}'"
      return 1
    fi
    if [[ $((i % 3)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: east state=${east_state:-pending}, completed=${east_completed:-unknown}"
    fi
    sleep "${delay}"
  done

  info "  Step 4: Demoting west to secondary..."
  kwest -n "${test_ns}" patch volumereplication test-vr \
    --type=merge -p '{"spec":{"replicationState":"secondary"}}'

  # Wait for west to reach Secondary state
  retries=30
  delay=10
  for ((i=1; i<=retries; i++)); do
    west_state="$(kwest -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"
    if [[ "${west_state}" == "Secondary" ]]; then
      info "  West demoted to Secondary"
      break
    fi
    if [[ ${i} -eq ${retries} ]]; then
      error "  Failback FAILED — west did not reach Secondary within $((retries * delay))s"
      info "  Resources left for troubleshooting in namespace '${test_ns}'"
      return 1
    fi
    if [[ $((i % 3)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: west state=${west_state:-pending}"
    fi
    sleep "${delay}"
  done

  info "  Step 5: Promoting east back to primary..."
  keast -n "${test_ns}" patch volumereplication test-vr \
    --type=merge -p '{"spec":{"replicationState":"primary"}}'

  # Wait for east to reach Primary state
  retries=30
  delay=10
  for ((i=1; i<=retries; i++)); do
    east_state="$(keast -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"

    local east_completed
    east_completed="$(keast -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.conditions[?(@.type=="Completed")].status}' 2>/dev/null || echo "")"

    if [[ "${east_state}" == "Primary" && "${east_completed}" == "True" ]]; then
      info "  East promoted back to Primary"
      break
    fi
    if [[ ${i} -eq ${retries} ]]; then
      error "  Failback FAILED — east did not reach Primary within $((retries * delay))s"
      info "  Resources left for troubleshooting in namespace '${test_ns}'"
      return 1
    fi
    if [[ $((i % 3)) -eq 0 ]]; then
      info "  Attempt ${i}/${retries}: east state=${east_state:-pending}, completed=${east_completed:-unknown}"
    fi
    sleep "${delay}"
  done

  # Wait for west to start replaying from east
  info "  Waiting for west to resume replaying..."
  retries=12
  delay=10
  for ((i=1; i<=retries; i++)); do
    west_state="$(kwest -n "${test_ns}" get volumereplication test-vr \
      -o jsonpath='{.status.state}' 2>/dev/null || echo "")"
    if [[ "${west_state}" == "Secondary" ]]; then
      break
    fi
    sleep "${delay}"
  done

  info "  Failover smoke test PASSED"
  info "  Completed full cycle: east=Primary→Secondary→Resync→Primary, west=Secondary→Primary→Secondary"
  info ""
  info "  Final state: east=Primary, west=Secondary (original state restored)"
  return 0
}

# ---------------------------------------------------------------------------
# Cleanup / Teardown
# ---------------------------------------------------------------------------
teardown_rook_ceph() {
  info "Tearing down Rook-Ceph from both clusters..."

  for context in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
    local cluster_name="${context}"

    info "  Removing Rook-Ceph from '${cluster_name}'..."

    # Delete VolumeReplicationClass and StorageClass
    kubectl --context "${context}" delete -f "${MANIFESTS_DIR}/volume-replication-class.yaml" --ignore-not-found 2>/dev/null || true
    kubectl --context "${context}" delete -f "${MANIFESTS_DIR}/storage-class.yaml" --ignore-not-found 2>/dev/null || true

    # Delete CephRBDMirror, CephBlockPool, CephCluster
    kubectl --context "${context}" delete -f "${MANIFESTS_DIR}/ceph-rbd-mirror.yaml" --ignore-not-found 2>/dev/null || true
    kubectl --context "${context}" delete -f "${MANIFESTS_DIR}/ceph-blockpool.yaml" --ignore-not-found 2>/dev/null || true
    kubectl --context "${context}" -n rook-ceph delete cephcluster rook-ceph --ignore-not-found 2>/dev/null || true

    # Wait for CephCluster deletion
    kubectl --context "${context}" -n rook-ceph wait --for=delete cephcluster/rook-ceph --timeout=120s 2>/dev/null || true

    # Remove peer secrets
    kubectl --context "${context}" -n rook-ceph delete secret rbd-mirror-peer-token-east --ignore-not-found 2>/dev/null || true
    kubectl --context "${context}" -n rook-ceph delete secret rbd-mirror-peer-token-west --ignore-not-found 2>/dev/null || true

    # Uninstall Rook operator
    helm uninstall rook-ceph --namespace rook-ceph --kube-context "${context}" 2>/dev/null || true

    # Remove CSI Addons controller and namespace
    kubectl --context "${context}" delete deployment csi-addons-controller-manager -n csi-addons-system --ignore-not-found 2>/dev/null || true
    kubectl --context "${context}" delete namespace csi-addons-system --ignore-not-found --timeout=60s 2>/dev/null || true

    # Delete namespace (will hang until finalizers clear)
    kubectl --context "${context}" delete namespace rook-ceph --ignore-not-found --timeout=60s 2>/dev/null || true
  done

  info "Rook-Ceph teardown complete"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
  info "=== Rook-Ceph Deployment with RBD Mirroring ==="
  info "East cluster: ${EAST_CLUSTER_NAME}"
  info "West cluster: ${WEST_CLUSTER_NAME}"
  echo

  # Handle teardown subcommand
  if [[ "${1:-}" == "teardown" ]]; then
    teardown_rook_ceph
    return 0
  fi

  check_prerequisites

  # Task 1a: Deploy Rook operator
  deploy_rook_operator "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_rook_operator "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"
  wait_rook_operator "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_rook_operator "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Task 1b: CSI Addons CRDs + controller (must precede ceph-csi-drivers because
  # the csi-addons sidecars create CSIAddonsNode CRs on startup)
  deploy_csi_addons "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_csi_addons "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"
  wait_csi_addons "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_csi_addons "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"
  verify_csi_addons_crds "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  verify_csi_addons_crds "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Task 1c: Deploy ceph-csi-drivers (RBD + CephFS CSI provisioners)
  deploy_csi_drivers "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_csi_drivers "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Ensure persistent data directory exists on all nodes
  ensure_data_dir "${EAST_CLUSTER_NAME}"
  ensure_data_dir "${WEST_CLUSTER_NAME}"

  # Task 2: CephCluster (discovers /dev/vdb on worker nodes via deviceFilter)
  deploy_ceph_cluster "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_ceph_cluster "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"
  wait_ceph_cluster "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_ceph_cluster "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Wait for CSI drivers to become ready (needs CephCluster connection info)
  wait_csi_drivers "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_csi_drivers "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Deploy toolbox for Ceph CLI access
  info "Deploying Ceph toolbox on both clusters..."
  kubectl --context "${EAST_CONTEXT}" apply -f "${MANIFESTS_DIR}/ceph-toolbox.yaml"
  kubectl --context "${WEST_CONTEXT}" apply -f "${MANIFESTS_DIR}/ceph-toolbox.yaml"

  # Task 3: Export mon and OSD services via MCS-API
  export_ceph_services "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  export_ceph_services "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Task 4: CephBlockPool + CephRBDMirror
  deploy_ceph_blockpool "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_ceph_blockpool "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"
  wait_blockpool_ready "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  wait_blockpool_ready "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Task 5: Peer exchange
  exchange_peer_secrets
  wait_mirror_healthy

  # Task 7: VolumeReplicationClass + StorageClass
  deploy_storage_resources "${EAST_CONTEXT}" "${EAST_CLUSTER_NAME}"
  deploy_storage_resources "${WEST_CONTEXT}" "${WEST_CLUSTER_NAME}"

  # Task 8: Replication smoke test
  replication_smoke_test

  # Task 9: Failover smoke test
  failover_smoke_test

  echo
  info "=== Rook-Ceph Setup Complete ==="
  info ""
  info "Ceph cluster status:"
  info "  kubectl --context ${EAST_CONTEXT} -n rook-ceph get cephcluster"
  info "  kubectl --context ${WEST_CONTEXT} -n rook-ceph get cephcluster"
  info ""
  info "RBD mirror status:"
  info "  kubectl --context ${EAST_CONTEXT} -n rook-ceph get cephblockpool mirrored-pool -o jsonpath='{.status.mirroringStatus}'"
  info ""
  info "VolumeReplicationClass:"
  info "  kubectl --context ${EAST_CONTEXT} get volumereplicationclass rook-ceph-rbd-vrc"
  info ""
  info "StorageClass:"
  info "  kubectl --context ${EAST_CONTEXT} get storageclass rook-ceph-block"
  info ""
  info "Teardown:"
  info "  ./hack/multisite/setup-rook-ceph.sh teardown"
}

main "$@"
