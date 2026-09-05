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

# Helm chart smoke test.
#
# Creates a temporary Kind cluster, installs prerequisites (cert-manager,
# scylla-operator), deploys the Helm chart with CI-friendly values, and
# verifies that the core components start correctly.
#
# Designed to run locally (with Podman or Docker) and in GitHub Actions.
#
# Environment variables:
#   HELM_SMOKE_CLUSTER      Kind cluster name          (default: soteria-helm-test)
#   HELM_SMOKE_NS           Target namespace            (default: soteria)
#   HELM_SMOKE_CLEANUP      Delete cluster on exit      (default: 1; set 0 to keep)
#   HELM_SMOKE_TIMEOUT      Helm install --timeout      (default: 15m)
#   IMG                     Controller image:tag        (default: soteria:smoke)
#   CONTAINER_TOOL          Container runtime           (default: podman)
#   CERT_MANAGER_VERSION    cert-manager chart version  (default: v1.20.3)
#   SCYLLA_OPERATOR_VERSION scylla-operator version     (default: v1.21.0)
#   KIND                    Kind binary                 (default: kind)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
CLUSTER_NAME="${HELM_SMOKE_CLUSTER:-soteria-helm-test}"
NAMESPACE="${HELM_SMOKE_NS:-soteria}"
IMG="${IMG:-soteria:smoke}"
CONTAINER_TOOL="${CONTAINER_TOOL:-podman}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.20.3}"
SCYLLA_OPERATOR_VERSION="${SCYLLA_OPERATOR_VERSION:-v1.21.0}"
TIMEOUT="${HELM_SMOKE_TIMEOUT:-15m}"
CLEANUP="${HELM_SMOKE_CLEANUP:-1}"
KIND="${KIND:-kind}"
RELEASE_NAME="soteria"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

step()  { echo -e "\n${CYAN}▸ $*${NC}"; }
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fatal() { echo -e "${RED}[FAIL]${NC} $*" >&2; exit 1; }

k() { kubectl --context "kind-${CLUSTER_NAME}" "$@"; }

cleanup() {
  if [[ "${CLEANUP}" == "1" ]]; then
    step "Cleaning up Kind cluster '${CLUSTER_NAME}'"
    "${KIND}" delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
  else
    info "Skipping cleanup (HELM_SMOKE_CLEANUP=0)"
    info "Connect: kubectl --context kind-${CLUSTER_NAME}"
  fi
}

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
step "Pre-flight checks"
command -v "${KIND}"  >/dev/null 2>&1 || fatal "kind not found — install from https://kind.sigs.k8s.io"
command -v helm       >/dev/null 2>&1 || fatal "helm not found"
command -v kubectl    >/dev/null 2>&1 || fatal "kubectl not found"
command -v "${CONTAINER_TOOL}" >/dev/null 2>&1 || fatal "${CONTAINER_TOOL} not found"
info "Using container tool: ${CONTAINER_TOOL}"
info "Controller image:     ${IMG}"

# ---------------------------------------------------------------------------
# 1. Kind cluster
# ---------------------------------------------------------------------------
step "Creating Kind cluster '${CLUSTER_NAME}'"
trap cleanup EXIT

if "${KIND}" get clusters 2>/dev/null | grep -qw "${CLUSTER_NAME}"; then
  info "Cluster already exists — reusing"
else
  # Podman: Kind auto-detects via KIND_EXPERIMENTAL_PROVIDER or rootless podman socket.
  "${KIND}" create cluster --name "${CLUSTER_NAME}" --wait 60s
fi

# ---------------------------------------------------------------------------
# 2. Build and load controller image
# ---------------------------------------------------------------------------
step "Building controller image (${CONTAINER_TOOL})"
"${CONTAINER_TOOL}" build -t "${IMG}" -f "${ROOT_DIR}/Dockerfile" "${ROOT_DIR}"

step "Loading image into Kind"
if [[ "${CONTAINER_TOOL}" == "podman" ]]; then
  _archive=$(mktemp --suffix=.tar)
  "${CONTAINER_TOOL}" save -o "${_archive}" "${IMG}"
  "${KIND}" load image-archive "${_archive}" --name "${CLUSTER_NAME}"
  rm -f "${_archive}"
else
  "${KIND}" load docker-image "${IMG}" --name "${CLUSTER_NAME}"
fi

# ---------------------------------------------------------------------------
# 3. Install cert-manager
# ---------------------------------------------------------------------------
step "Installing cert-manager ${CERT_MANAGER_VERSION}"
if k get namespace cert-manager >/dev/null 2>&1; then
  info "cert-manager namespace exists — skipping install"
else
  helm upgrade --install cert-manager \
    oci://quay.io/jetstack/charts/cert-manager \
    --version "${CERT_MANAGER_VERSION}" \
    --namespace cert-manager \
    --create-namespace \
    --set crds.enabled=true \
    --kube-context "kind-${CLUSTER_NAME}" \
    --wait --timeout 5m
fi

info "Waiting for cert-manager webhook..."
k -n cert-manager rollout status deployment/cert-manager-webhook --timeout=3m

# ---------------------------------------------------------------------------
# 4. Install scylla-operator
# ---------------------------------------------------------------------------
step "Installing scylla-operator ${SCYLLA_OPERATOR_VERSION}"
if k get namespace scylla-operator >/dev/null 2>&1; then
  info "scylla-operator namespace exists — skipping install"
else
  helm repo add scylla https://scylla-operator-charts.storage.googleapis.com/stable 2>/dev/null || true
  helm repo update scylla 2>/dev/null || true

  helm upgrade --install scylla-operator scylla/scylla-operator \
    --version "${SCYLLA_OPERATOR_VERSION}" \
    --namespace scylla-operator \
    --create-namespace \
    --kube-context "kind-${CLUSTER_NAME}" \
    --wait --timeout 5m
fi

info "Waiting for scylla-operator..."
k -n scylla-operator rollout status deployment/scylla-operator --timeout=3m
k -n scylla-operator rollout status deployment/webhook-server --timeout=3m

# ---------------------------------------------------------------------------
# 5. Bootstrap cert-manager CA (self-signed → CA cert → namespace Issuer)
# ---------------------------------------------------------------------------
step "Bootstrapping cert-manager CA in namespace '${NAMESPACE}'"
k create namespace "${NAMESPACE}" --dry-run=client -o yaml | k apply -f -

k apply -f - <<'EOF'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: soteria-selfsigned
spec:
  selfSigned: {}
EOF

k -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: soteria-ca
  namespace: ${NAMESPACE}
spec:
  isCA: true
  commonName: soteria-ca
  secretName: soteria-ca-key-pair
  issuerRef:
    name: soteria-selfsigned
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: soteria-ca
  namespace: ${NAMESPACE}
spec:
  ca:
    secretName: soteria-ca-key-pair
EOF

info "Waiting for CA certificate to be ready..."
for i in $(seq 1 60); do
  ready=$(k -n "${NAMESPACE}" get certificate soteria-ca \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
  if [[ "${ready}" == "True" ]]; then
    info "CA certificate ready"
    break
  fi
  if [[ "${i}" -eq 60 ]]; then
    fatal "CA certificate did not become ready within 2 minutes"
  fi
  sleep 2
done

info "Waiting for Issuer to be ready..."
for i in $(seq 1 30); do
  ready=$(k -n "${NAMESPACE}" get issuer soteria-ca \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
  if [[ "${ready}" == "True" ]]; then
    info "Issuer ready"
    break
  fi
  if [[ "${i}" -eq 30 ]]; then
    fatal "Issuer 'soteria-ca' did not become ready within 1 minute"
  fi
  sleep 2
done

# ---------------------------------------------------------------------------
# 6. Lint chart
# ---------------------------------------------------------------------------
step "Linting Helm chart"
helm lint "${ROOT_DIR}/charts/soteria" -f "${ROOT_DIR}/charts/soteria/ci-values.yaml"

# ---------------------------------------------------------------------------
# 7. Deploy chart
# ---------------------------------------------------------------------------
step "Deploying chart (release: ${RELEASE_NAME}, timeout: ${TIMEOUT})"
helm upgrade --install "${RELEASE_NAME}" "${ROOT_DIR}/charts/soteria" \
  --namespace "${NAMESPACE}" \
  --kube-context "kind-${CLUSTER_NAME}" \
  -f "${ROOT_DIR}/charts/soteria/ci-values.yaml" \
  --set "controller.image.repository=${IMG%%:*}" \
  --set "controller.image.tag=${IMG##*:}" \
  --timeout "${TIMEOUT}"

# ---------------------------------------------------------------------------
# 8. Verify: ScyllaCluster readiness
# ---------------------------------------------------------------------------
step "Waiting for ScyllaCluster to be ready"
for i in $(seq 1 180); do
  members=$(k -n "${NAMESPACE}" get scyllaclusters.scylla.scylladb.com \
    "${RELEASE_NAME}-scylladb" \
    -o jsonpath='{.status.racks.rack1.readyMembers}' 2>/dev/null || echo "0")
  if [[ "${members}" -ge 1 ]]; then
    info "ScyllaCluster ready (${members} member(s))"
    break
  fi
  if [[ "${i}" -eq 180 ]]; then
    warn "ScyllaCluster status:"
    k -n "${NAMESPACE}" describe scyllaclusters.scylla.scylladb.com \
      "${RELEASE_NAME}-scylladb" 2>/dev/null || true
    fatal "ScyllaCluster did not become ready within 15 minutes"
  fi
  if (( i % 30 == 0 )); then
    info "  Still waiting for ScyllaDB (attempt ${i}/180)..."
  fi
  sleep 5
done

# ---------------------------------------------------------------------------
# 9. Verify: Controller deployment readiness
# ---------------------------------------------------------------------------
step "Waiting for controller deployment"
k -n "${NAMESPACE}" rollout status \
  "deployment/${RELEASE_NAME}-controller-manager" --timeout=300s \
  || {
    warn "Controller pod status:"
    k -n "${NAMESPACE}" describe pods \
      -l "app.kubernetes.io/name=soteria,control-plane=controller-manager" 2>/dev/null || true
    warn "Controller logs:"
    k -n "${NAMESPACE}" logs \
      -l "control-plane=controller-manager" --tail=50 2>/dev/null || true
    fatal "Controller deployment did not become ready"
  }

# ---------------------------------------------------------------------------
# 10. Verify: All Certificates issued
# ---------------------------------------------------------------------------
step "Checking cert-manager Certificates"
CERTS_NOT_READY=""
while IFS= read -r cert; do
  status=$(k -n "${NAMESPACE}" get certificate "${cert}" \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
  if [[ "${status}" != "True" ]]; then
    CERTS_NOT_READY="${CERTS_NOT_READY} ${cert}"
  fi
done < <(k -n "${NAMESPACE}" get certificate -o jsonpath='{.items[*].metadata.name}' 2>/dev/null | tr ' ' '\n')

if [[ -n "${CERTS_NOT_READY}" ]]; then
  warn "Not-ready certificates:${CERTS_NOT_READY}"
  k -n "${NAMESPACE}" get certificate 2>/dev/null || true
  fatal "Some certificates are not Ready"
fi
info "All certificates issued successfully"

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
echo ""
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo -e "${GREEN}  Helm chart smoke test PASSED${NC}"
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo ""
info "Release: ${RELEASE_NAME}  Namespace: ${NAMESPACE}  Cluster: kind-${CLUSTER_NAME}"
