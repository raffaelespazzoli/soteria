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

# Builds and deploys the Soteria operator and console UI on both Minikube
# KVM2 clusters.
#
# Builds the operator and console-standalone images locally, loads them into
# both Minikube clusters, applies Kustomize overlays with ScyllaDB connection
# + TLS + site-name configuration, waits for controller-manager rollout,
# verifies APIService availability, deploys the console-standalone UI with
# Cilium Gateway API ingress, and runs a cross-DC replication smoke test.
#
# Prerequisites:
#   - Minikube KVM2 clusters created by setup-clusters.sh (east + west)
#   - Rook-Ceph deployed by setup-rook-ceph.sh (StorageClass rook-ceph-block)
#   - KubeVirt deployed by setup-kubevirt.sh (VM webhook requires CRDs)
#   - ScyllaDB deployed by setup-scylladb.sh (cross-DC + mTLS + cert-manager)
#   - podman or docker available for image build
#
# Usage:
#   ./hack/multisite/deploy-soteria.sh
#
# Environment Variables:
#   EAST_CLUSTER_NAME     Name of the east Minikube profile (default: east)
#   WEST_CLUSTER_NAME     Name of the west Minikube profile (default: west)
#   NAMESPACE             Namespace for Soteria deployment (default: soteria)
#   IMG                   Soteria container image (default: localhost/soteria:dev)
#   CONTAINER_TOOL        Container build tool (default: podman)
#   SKIP_BUILD            Set to "1" to skip image build (default: 0)
#   SKIP_LOAD             Set to "1" to skip image load into Minikube (default: 0)
#   CONSOLE_IMG           Console standalone image (default: localhost/soteria-console:dev)
#   SMOKE_TEST            Set to "0" to skip the cross-DC replication smoke test
#   SMOKE_TEST_PLAN_NAME  Name for the temporary smoke-test DRPlan

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"
NAMESPACE="${NAMESPACE:-soteria}"
IMG="${IMG:-localhost/soteria:dev}"
CONTAINER_TOOL="${CONTAINER_TOOL:-podman}"
SKIP_BUILD="${SKIP_BUILD:-0}"
SKIP_LOAD="${SKIP_LOAD:-0}"
CONSOLE_IMG="${CONSOLE_IMG:-localhost/soteria-console:dev}"
SMOKE_TEST="${SMOKE_TEST:-1}"
SMOKE_TEST_PLAN_NAME="${SMOKE_TEST_PLAN_NAME:-smoke-test-plan-$(date +%s)-$$}"

OVERLAYS_DIR="${SCRIPT_DIR}/overlays/soteria"

# Detect kustomize binary
if [[ -x "${REPO_ROOT}/bin/kustomize" ]]; then
  KUSTOMIZE="${REPO_ROOT}/bin/kustomize"
elif command -v kustomize &>/dev/null; then
  KUSTOMIZE="kustomize"
else
  echo "ERROR: kustomize not found. Run 'make kustomize' or install kustomize." >&2
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
fatal() { error "$*"; exit 1; }

# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }

kustomize_build() {
  "${KUSTOMIZE}" build --load-restrictor LoadRestrictionsNone "$@"
}

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
info "=== Checking prerequisites ==="

command -v kubectl &>/dev/null || fatal "kubectl not found"
ensure_minikube
command -v minikube &>/dev/null || fatal "minikube not available after download attempt"

for profile in "${EAST_CLUSTER_NAME}" "${WEST_CLUSTER_NAME}"; do
  status=$(minikube status -p "${profile}" -f '{{.Host}}' 2>/dev/null) || true
  if [[ "${status}" != "Running" ]]; then
    fatal "Minikube cluster '${profile}' is not running (status: ${status:-unknown})"
  fi
done

for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  if ! kubectl --context "${ctx}" get namespace "${NAMESPACE}" &>/dev/null; then
    fatal "Namespace '${NAMESPACE}' does not exist on context '${ctx}'. Run setup-scylladb.sh first."
  fi

  ready=$(kubectl --context "${ctx}" -n "${NAMESPACE}" get scyllacluster soteria-scylladb \
    -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null) || true
  if [[ "${ready}" != "True" ]]; then
    fatal "ScyllaDB is not ready on '${ctx}' (Available=${ready:-unknown}). Run setup-scylladb.sh first."
  fi

  kubevirt_phase="$(kubectl --context "${ctx}" -n kubevirt get kubevirt kubevirt \
    -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
  if [[ "${kubevirt_phase}" != "Deployed" ]]; then
    fatal "KubeVirt is not deployed on context '${ctx}' (phase: ${kubevirt_phase:-not found}). Run setup-kubevirt.sh first."
  fi

  if ! kubectl --context "${ctx}" get volumereplicationclass rook-ceph-rbd-vrc-snapshot &>/dev/null; then
    fatal "VolumeReplicationClass 'rook-ceph-rbd-vrc-snapshot' not found on context '${ctx}'. Run setup-rook-ceph.sh first."
  fi
done

info "Prerequisites satisfied"

# ---------------------------------------------------------------------------
# Task 1: Build Soteria container image
# ---------------------------------------------------------------------------
if [[ "${SKIP_BUILD}" != "1" ]]; then
  info "=== Building Soteria image: ${IMG} ==="
  make -C "${REPO_ROOT}" docker-build IMG="${IMG}" CONTAINER_TOOL="${CONTAINER_TOOL}"
  info "Image built successfully"
else
  info "Skipping image build (SKIP_BUILD=1)"
fi

# ---------------------------------------------------------------------------
# Task 1b: Build console-standalone container image
# ---------------------------------------------------------------------------
if [[ "${SKIP_BUILD}" != "1" ]]; then
  info "=== Building console-standalone image: ${CONSOLE_IMG} ==="
  ${CONTAINER_TOOL} build -t "${CONSOLE_IMG}" \
    -f "${REPO_ROOT}/console-plugin/Dockerfile.standalone" "${REPO_ROOT}"
  info "Console image built successfully"
else
  info "Skipping console image build (SKIP_BUILD=1)"
fi

# ---------------------------------------------------------------------------
# Task 2: Load images into both Minikube clusters
# ---------------------------------------------------------------------------
load_image() {
  local image="$1" label="$2"
  local img_tar
  img_tar="$(mktemp -t "${label}-XXXXXX.tar")"

  info "Saving ${label} image to tarball for minikube import..."
  ${CONTAINER_TOOL} save "${image}" -o "${img_tar}"

  for profile in "${EAST_CLUSTER_NAME}" "${WEST_CLUSTER_NAME}"; do
    info "Loading ${label} image into Minikube cluster: ${profile}"
    minikube image load "${img_tar}" -p "${profile}"
  done
  rm -f "${img_tar}"
}

if [[ "${SKIP_LOAD}" != "1" ]]; then
  load_image "${IMG}" "soteria-operator"
  load_image "${CONSOLE_IMG}" "console-standalone"
  info "Images loaded into both clusters"
else
  info "Skipping image load (SKIP_LOAD=1)"
fi

# ---------------------------------------------------------------------------
# Idempotency: check if Soteria is already deployed and healthy
# ---------------------------------------------------------------------------
already_deployed=false
if keast -n "${NAMESPACE}" get deployment soteria-controller-manager &>/dev/null && \
   kwest -n "${NAMESPACE}" get deployment soteria-controller-manager &>/dev/null; then
  east_ready=$(keast -n "${NAMESPACE}" get deployment soteria-controller-manager \
    -o jsonpath='{.status.readyReplicas}' 2>/dev/null) || true
  west_ready=$(kwest -n "${NAMESPACE}" get deployment soteria-controller-manager \
    -o jsonpath='{.status.readyReplicas}' 2>/dev/null) || true
  if [[ "${east_ready:-0}" -ge 1 && "${west_ready:-0}" -ge 1 ]]; then
    already_deployed=true
    info "Soteria already deployed and healthy on both clusters (re-applying for idempotency)"
  fi
fi

# ---------------------------------------------------------------------------
# Task 3.3: Set image in kustomize config
# ---------------------------------------------------------------------------
info "=== Setting controller image: ${IMG} ==="
pushd "${REPO_ROOT}/config/manager" >/dev/null
"${KUSTOMIZE}" edit set image "controller=${IMG}"
popd >/dev/null

# ---------------------------------------------------------------------------
# Task 3.4-3.5: Deploy Soteria on east cluster
# ---------------------------------------------------------------------------
info "=== Deploying Soteria on east ==="
kustomize_build "${OVERLAYS_DIR}/east" | keast apply --server-side --force-conflicts -f -

info "Waiting for controller-manager rollout on east (timeout 5m)..."
if ! keast -n "${NAMESPACE}" rollout status deployment/soteria-controller-manager --timeout=300s; then
  error "Controller-manager rollout failed on east"
  keast -n "${NAMESPACE}" describe deployment/soteria-controller-manager
  keast -n "${NAMESPACE}" logs deployment/soteria-controller-manager -c manager --tail=50 2>/dev/null || true
  fatal "Deployment rollout failed on east"
fi
info "Soteria running on east"

# ---------------------------------------------------------------------------
# Task 3.6-3.7: Deploy Soteria on west cluster
# ---------------------------------------------------------------------------
info "=== Deploying Soteria on west ==="
kustomize_build "${OVERLAYS_DIR}/west" | kwest apply --server-side --force-conflicts -f -

info "Waiting for controller-manager rollout on west (timeout 5m)..."
if ! kwest -n "${NAMESPACE}" rollout status deployment/soteria-controller-manager --timeout=300s; then
  error "Controller-manager rollout failed on west"
  kwest -n "${NAMESPACE}" describe deployment/soteria-controller-manager
  kwest -n "${NAMESPACE}" logs deployment/soteria-controller-manager -c manager --tail=50 2>/dev/null || true
  fatal "Deployment rollout failed on west"
fi
info "Soteria running on west"

# ---------------------------------------------------------------------------
# Task 3.8: Verify APIService availability
# ---------------------------------------------------------------------------
info "=== Verifying APIService availability ==="

check_apiservice() {
  local ctx="$1"
  local status
  status=$(kubectl --context="${ctx}" get apiservice v1alpha1.soteria.io \
    -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null) || true
  if [[ "${status}" == "True" ]]; then
    return 0
  fi
  return 1
}

for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  info "Checking APIService on ${ctx}..."
  retries=0
  while ! check_apiservice "${ctx}"; do
    retries=$((retries + 1))
    if [[ ${retries} -ge 30 ]]; then
      error "APIService v1alpha1.soteria.io not Available on ${ctx} after 60s"
      kubectl --context="${ctx}" get apiservice v1alpha1.soteria.io -o yaml
      fatal "APIService check failed"
    fi
    sleep 2
  done
  info "APIService v1alpha1.soteria.io is Available on ${ctx}"
done

# ---------------------------------------------------------------------------
# Task 4: Deploy console-standalone on both clusters
# ---------------------------------------------------------------------------
CONSOLE_MANIFEST="${REPO_ROOT}/hack/overlays/base/console-standalone.yaml"
CONSOLE_NS="soteria-console"

info "=== Deploying console-standalone ==="
for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  info "Applying console-standalone on ${ctx}..."
  sed "s|CONSOLE_STANDALONE_IMG_PLACEHOLDER|${CONSOLE_IMG}|g" "${CONSOLE_MANIFEST}" | \
    kubectl --context="${ctx}" apply --server-side --force-conflicts -f -

  info "Waiting for console-standalone rollout on ${ctx} (timeout 3m)..."
  if ! kubectl --context="${ctx}" -n "${CONSOLE_NS}" rollout status \
      deployment/soteria-console-standalone --timeout=180s; then
    error "Console-standalone rollout failed on ${ctx}"
    kubectl --context="${ctx}" -n "${CONSOLE_NS}" describe deployment/soteria-console-standalone
    kubectl --context="${ctx}" -n "${CONSOLE_NS}" logs deployment/soteria-console-standalone --tail=30 2>/dev/null || true
    fatal "Console deployment failed on ${ctx}"
  fi
  info "Console-standalone running on ${ctx}"
done

# ---------------------------------------------------------------------------
# Task 5: Deploy Gateway + HTTPRoute for console UI
# ---------------------------------------------------------------------------
GATEWAY_MANIFEST="${SCRIPT_DIR}/manifests/console-gateway.yaml"

info "=== Deploying Gateway + HTTPRoute for console UI ==="
for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  kubectl --context="${ctx}" apply --server-side -f "${GATEWAY_MANIFEST}"
done

for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
  info "Waiting for Gateway to be programmed on ${ctx}..."
  retries=0
  while true; do
    programmed=$(kubectl --context="${ctx}" -n "${CONSOLE_NS}" get gateway soteria-console-gateway \
      -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' 2>/dev/null) || true
    if [[ "${programmed}" == "True" ]]; then
      break
    fi
    retries=$((retries + 1))
    if [[ ${retries} -ge 30 ]]; then
      warn "Gateway not Programmed on ${ctx} after 60s — continuing anyway"
      break
    fi
    sleep 2
  done
done
info "Gateway deployed on both clusters"

# ---------------------------------------------------------------------------
# Task 7: Cross-DC replication smoke test
# ---------------------------------------------------------------------------
if [[ "${SMOKE_TEST}" != "0" ]]; then
  info "=== Running cross-DC replication smoke test ==="

  if keast get drplan "${SMOKE_TEST_PLAN_NAME}" &>/dev/null; then
    fatal "Smoke test plan '${SMOKE_TEST_PLAN_NAME}' already exists on east. Set SMOKE_TEST_PLAN_NAME to a unique value."
  fi

  # Task 4.1: Create DRPlan on east
  info "Creating ${SMOKE_TEST_PLAN_NAME} DRPlan on east..."
  keast apply -f - <<EOF
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: ${SMOKE_TEST_PLAN_NAME}
spec:
  maxConcurrentFailovers: 2
  primarySite: east
  secondarySite: west
  volumeReplicationDriver:
    type: csi-extension
    volumeReplicationClass: rook-ceph-rbd-vrc-snapshot
EOF

  # Task 4.2: Verify DRPlan visible on west via ScyllaDB cross-DC replication
  info "Waiting for cross-DC replication (up to 60s)..."
  replicated=false
  for i in $(seq 1 30); do
    if kwest get drplan "${SMOKE_TEST_PLAN_NAME}" &>/dev/null; then
      replicated=true
      break
    fi
    sleep 2
  done

  if [[ "${replicated}" == "true" ]]; then
    info "Cross-DC replication verified: DRPlan visible on west"
  else
    error "DRPlan not visible on west after 60s"
    warn "This may indicate ScyllaDB replication delay or connectivity issues"
    keast get drplan "${SMOKE_TEST_PLAN_NAME}" -o yaml 2>/dev/null || true
    kwest get drplan 2>/dev/null || true
  fi

  # Task 4.3: Cleanup
  info "Cleaning up smoke test resources..."
  keast delete drplan "${SMOKE_TEST_PLAN_NAME}" --ignore-not-found 2>/dev/null || true
  kwest delete drplan "${SMOKE_TEST_PLAN_NAME}" --ignore-not-found 2>/dev/null || true

  if [[ "${replicated}" != "true" ]]; then
    fatal "Cross-DC replication smoke test failed"
  fi
else
  info "Skipping smoke test (SMOKE_TEST=0)"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
east_gw_ip=$(kubectl --context "${EAST_CONTEXT}" -n "${CONSOLE_NS}" get gateway soteria-console-gateway \
  -o jsonpath='{.status.addresses[0].value}' 2>/dev/null) || true
west_gw_ip=$(kubectl --context "${WEST_CONTEXT}" -n "${CONSOLE_NS}" get gateway soteria-console-gateway \
  -o jsonpath='{.status.addresses[0].value}' 2>/dev/null) || true

echo ""
info "============================================"
info "  Soteria deployment complete!"
info "============================================"
info ""
info "  Operator:   ${IMG}"
info "  Console:    ${CONSOLE_IMG}"
info "  East:       controller-manager running, APIService Available"
info "  West:       controller-manager running, APIService Available"
if [[ "${SMOKE_TEST}" != "0" ]]; then
  info "  Smoke test: Cross-DC replication verified"
fi
info ""
info "  Console UI:"
info "    East: http://${east_gw_ip:-<pending>}"
info "    West: http://${west_gw_ip:-<pending>}"
info ""
info "  Verify:"
info "    kubectl --context east get apiservice v1alpha1.soteria.io"
info "    kubectl --context west get apiservice v1alpha1.soteria.io"
info "    kubectl --context east -n soteria logs deploy/soteria-controller-manager -c manager"
info "    kubectl --context east -n soteria-console get gateway,httproute"
info ""
