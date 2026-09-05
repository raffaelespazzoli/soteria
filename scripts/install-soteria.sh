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

# Multi-cluster install script for Soteria.
#
# Orchestrates a two-cluster Helm deployment with CA propagation:
#   1. Install on seed cluster (east) with cert-manager CA bootstrap
#   2. Wait for CA Secret to be ready
#   3. Copy CA Secret to joining cluster (west)
#   4. Install on joining cluster with externalSeeds pointing to seed
#
# Prerequisites:
#   - helm, kubectl, jq CLIs available
#   - cert-manager installed on both clusters
#   - scylla-operator CRDs on both clusters (when scylladb.mode=managed)
#   - Submariner MCS CRDs on both clusters (when --networking=submariner)
#
# Usage:
#   scripts/install-soteria.sh --east-context east --west-context west --chart ./charts/soteria
#   scripts/install-soteria.sh --east-context east --west-context west --chart ./charts/soteria --uninstall
#   scripts/install-soteria.sh --help

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
readonly CA_SECRET_NAME="soteria-ca-key-pair"
readonly CA_CERT_NAME="soteria-ca"
readonly RELEASE_NAME="soteria"
readonly CA_WAIT_TIMEOUT=300   # 5 minutes
readonly ROLLOUT_TIMEOUT="300s"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
EAST_CONTEXT=""
WEST_CONTEXT=""
NETWORKING="submariner"
UI_MODE="console-plugin"
VALUES_FILE=""
CHART=""
NAMESPACE="soteria"
UNINSTALL=false
DRY_RUN=false

# ---------------------------------------------------------------------------
# Colors and output helpers
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
# Usage
# ---------------------------------------------------------------------------
usage() {
  cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Orchestrate a two-cluster Soteria deployment via Helm.

Installs Soteria on the seed (east) cluster first, waits for the CA cert-manager
Certificate to become Ready, copies the CA Secret to the joining (west) cluster,
then installs Soteria on the joining cluster with externalSeeds pointing to the
seed ScyllaDB service.

Required flags:
  --east-context <ctx>    kubectl context for the seed (east) cluster
  --west-context <ctx>    kubectl context for the joining (west) cluster
  --chart <path|url>      Helm chart reference (local path or repo URL)

Optional flags:
  --networking <mode>     Cross-cluster networking: cilium | submariner (default: submariner)
  --ui-mode <mode>        UI mode: console-plugin | standalone | none (default: console-plugin)
  --values-file <path>    Path to a Helm values file (passed as -f to helm)
  --namespace <ns>        Kubernetes namespace (default: soteria)
  --uninstall             Uninstall Soteria from both clusters (reverse order)
  --dry-run               Print commands without executing them
  --help                  Show this help message

Examples:
  # Install from a local chart
  $(basename "$0") --east-context east --west-context west --chart ./charts/soteria

  # Install with custom values
  $(basename "$0") --east-context east --west-context west \\
    --chart oci://ghcr.io/soteria-project/charts/soteria \\
    --values-file my-values.yaml --networking cilium

  # Uninstall
  $(basename "$0") --east-context east --west-context west --chart ./charts/soteria --uninstall

  # Dry run (print commands only)
  $(basename "$0") --east-context east --west-context west --chart ./charts/soteria --dry-run
EOF
}

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
parse_flags() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --east-context)
        [[ $# -ge 2 ]] || fatal "--east-context requires a value"
        EAST_CONTEXT="$2"; shift 2 ;;
      --west-context)
        [[ $# -ge 2 ]] || fatal "--west-context requires a value"
        WEST_CONTEXT="$2"; shift 2 ;;
      --networking)
        [[ $# -ge 2 ]] || fatal "--networking requires a value"
        NETWORKING="$2"; shift 2 ;;
      --ui-mode)
        [[ $# -ge 2 ]] || fatal "--ui-mode requires a value"
        UI_MODE="$2"; shift 2 ;;
      --values-file)
        [[ $# -ge 2 ]] || fatal "--values-file requires a value"
        VALUES_FILE="$2"; shift 2 ;;
      --chart)
        [[ $# -ge 2 ]] || fatal "--chart requires a value"
        CHART="$2"; shift 2 ;;
      --namespace)
        [[ $# -ge 2 ]] || fatal "--namespace requires a value"
        NAMESPACE="$2"; shift 2 ;;
      --uninstall)
        UNINSTALL=true; shift ;;
      --dry-run)
        DRY_RUN=true; shift ;;
      --help|-h)
        usage; exit 0 ;;
      *)
        fatal "Unknown flag: $1. Use --help for usage." ;;
    esac
  done

  # Validate required flags
  if [[ -z "${EAST_CONTEXT}" ]]; then
    fatal "--east-context is required"
  fi
  if [[ -z "${WEST_CONTEXT}" ]]; then
    fatal "--west-context is required"
  fi
  if [[ -z "${CHART}" ]]; then
    fatal "--chart is required"
  fi

  # Validate enum values
  case "${NETWORKING}" in
    cilium|submariner) ;;
    *) fatal "--networking must be 'cilium' or 'submariner', got '${NETWORKING}'" ;;
  esac
  case "${UI_MODE}" in
    console-plugin|standalone|none) ;;
    *) fatal "--ui-mode must be 'console-plugin', 'standalone', or 'none', got '${UI_MODE}'" ;;
  esac
}

# ---------------------------------------------------------------------------
# Command execution (respects --dry-run)
# ---------------------------------------------------------------------------
run() {
  if [[ "${DRY_RUN}" == "true" ]]; then
    echo -e "${YELLOW}[DRY-RUN]${NC} $*"
    return 0
  fi
  "$@"
}

# ---------------------------------------------------------------------------
# Prerequisite validation
# ---------------------------------------------------------------------------
check_prerequisites() {
  info "Checking prerequisites..."
  local missing=()

  # Check required CLI tools
  for tool in helm kubectl jq; do
    if ! command -v "${tool}" &>/dev/null; then
      missing+=("${tool}")
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    fatal "Missing required tools: ${missing[*]}"
  fi
  info "  CLI tools: helm, kubectl, jq — found"

  # Verify cluster reachability
  for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
    if ! kubectl --context="${ctx}" cluster-info &>/dev/null; then
      fatal "Cannot reach cluster via context '${ctx}'"
    fi
    info "  Context ${ctx}: reachable"
  done

  # Check cert-manager CRDs on both clusters
  for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
    if ! kubectl --context="${ctx}" get crd certificates.cert-manager.io &>/dev/null; then
      fatal "cert-manager CRD 'certificates.cert-manager.io' not found on context '${ctx}'. Install cert-manager first."
    fi
  done
  info "  cert-manager: CRDs present on both clusters"

  # Check scylla-operator CRDs when mode=managed (detect from values or default)
  local scylladb_mode="managed"
  if [[ -n "${VALUES_FILE}" && -f "${VALUES_FILE}" ]]; then
    local detected_mode
    detected_mode=$(grep -E '^\s*mode:\s*' "${VALUES_FILE}" | head -1 | awk '{print $2}' | tr -d '"'"'" 2>/dev/null || echo "")
    if [[ -n "${detected_mode}" ]]; then
      scylladb_mode="${detected_mode}"
    fi
  fi

  if [[ "${scylladb_mode}" == "managed" ]]; then
    for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
      if ! kubectl --context="${ctx}" get crd scyllaclusters.scylla.scylladb.com &>/dev/null; then
        fatal "scylla-operator CRD 'scyllaclusters.scylla.scylladb.com' not found on context '${ctx}'. Install scylla-operator first."
      fi
    done
    info "  scylla-operator: CRDs present on both clusters"
  fi

  # Check Submariner MCS CRDs when networking=submariner
  if [[ "${NETWORKING}" == "submariner" ]]; then
    for ctx in "${EAST_CONTEXT}" "${WEST_CONTEXT}"; do
      if ! kubectl --context="${ctx}" get crd serviceexports.multicluster.x-k8s.io &>/dev/null; then
        fatal "Submariner MCS CRD 'serviceexports.multicluster.x-k8s.io' not found on context '${ctx}'. Ensure Submariner MCS is active."
      fi
    done
    info "  Submariner MCS: CRDs present on both clusters"
  fi

  info "Prerequisites satisfied"
}

# ---------------------------------------------------------------------------
# Helm arguments builder — populates the caller's array via nameref
# ---------------------------------------------------------------------------
build_helm_args() {
  local -n _out_args=$1
  local context="$2"
  local site_name="$3"
  local site_role="$4"
  shift 4

  _out_args=(
    upgrade --install "${RELEASE_NAME}" "${CHART}"
    --kube-context "${context}"
    --namespace "${NAMESPACE}"
    --create-namespace
    --set "site.name=${site_name}"
    --set "site.role=${site_role}"
    --set "networking.mode=${NETWORKING}"
    --set "ui.mode=${UI_MODE}"
    --set "scylladb.localDC=${site_name}"
    --wait
    --timeout 10m
  )

  if [[ -n "${VALUES_FILE}" ]]; then
    _out_args+=(-f "${VALUES_FILE}")
  fi

  # Append any extra --set overrides passed as remaining arguments
  while [[ $# -gt 0 ]]; do
    _out_args+=("--set" "$1")
    shift
  done
}

# ---------------------------------------------------------------------------
# Resolve external seeds DNS based on networking mode
# ---------------------------------------------------------------------------
resolve_external_seeds() {
  case "${NETWORKING}" in
    submariner)
      echo "soteria-scylladb-client.${NAMESPACE}.svc.clusterset.local"
      ;;
    cilium)
      # Cilium ClusterMesh uses direct PodIP routing — seed discovery via
      # the headless client service in the remote cluster.
      echo "soteria-scylladb-client.${NAMESPACE}.svc.clusterset.local"
      ;;
  esac
}

# ---------------------------------------------------------------------------
# Wait for CA Certificate to become Ready
# ---------------------------------------------------------------------------
wait_for_ca() {
  local ctx="$1"
  info "Waiting for CA Certificate '${CA_CERT_NAME}' to become Ready on ${ctx} (timeout ${CA_WAIT_TIMEOUT}s)..."

  if [[ "${DRY_RUN}" == "true" ]]; then
    echo -e "${YELLOW}[DRY-RUN]${NC} Would wait for CA Certificate '${CA_CERT_NAME}' on ${ctx}"
    return 0
  fi

  local elapsed=0
  local interval=5
  while [[ ${elapsed} -lt ${CA_WAIT_TIMEOUT} ]]; do
    local ready
    ready=$(kubectl --context="${ctx}" -n "${NAMESPACE}" \
      get certificate "${CA_CERT_NAME}" \
      -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [[ "${ready}" == "True" ]]; then
      info "  CA Certificate is Ready on ${ctx}"
      return 0
    fi
    sleep "${interval}"
    elapsed=$((elapsed + interval))
  done
  fatal "CA Certificate '${CA_CERT_NAME}' did not become Ready on ${ctx} within ${CA_WAIT_TIMEOUT}s"
}

# ---------------------------------------------------------------------------
# Copy CA Secret from seed to joining cluster
# ---------------------------------------------------------------------------
copy_ca_secret() {
  local src_ctx="$1"
  local dst_ctx="$2"
  info "Copying CA Secret '${CA_SECRET_NAME}' from ${src_ctx} to ${dst_ctx}..."

  if [[ "${DRY_RUN}" == "true" ]]; then
    echo -e "${YELLOW}[DRY-RUN]${NC} kubectl --context ${src_ctx} -n ${NAMESPACE} get secret ${CA_SECRET_NAME} -o json | jq 'del(...)' | kubectl --context ${dst_ctx} -n ${NAMESPACE} apply --server-side --force-conflicts -f -"
    return 0
  fi

  # Ensure target namespace exists
  kubectl --context "${dst_ctx}" create namespace "${NAMESPACE}" --dry-run=client -o yaml \
    | kubectl --context "${dst_ctx}" apply --server-side -f -

  kubectl --context "${src_ctx}" -n "${NAMESPACE}" get secret "${CA_SECRET_NAME}" -o json \
    | jq 'del(.metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp,
              .metadata.managedFields, .metadata.annotations["cert-manager.io/certificate-name"])' \
    | kubectl --context "${dst_ctx}" -n "${NAMESPACE}" apply --server-side --force-conflicts -f -

  info "  CA Secret copied to ${dst_ctx}"
}

# ---------------------------------------------------------------------------
# Wait for controller Deployment rollout
# ---------------------------------------------------------------------------
wait_for_rollout() {
  local ctx="$1"
  info "Waiting for controller-manager rollout on ${ctx} (timeout ${ROLLOUT_TIMEOUT})..."

  if [[ "${DRY_RUN}" == "true" ]]; then
    echo -e "${YELLOW}[DRY-RUN]${NC} kubectl --context ${ctx} -n ${NAMESPACE} rollout status deployment/soteria-controller-manager --timeout=${ROLLOUT_TIMEOUT}"
    return 0
  fi

  if ! kubectl --context="${ctx}" -n "${NAMESPACE}" \
    rollout status deployment/soteria-controller-manager --timeout="${ROLLOUT_TIMEOUT}"; then
    error "Controller-manager rollout failed on ${ctx}"
    kubectl --context="${ctx}" -n "${NAMESPACE}" describe deployment/soteria-controller-manager || true
    kubectl --context="${ctx}" -n "${NAMESPACE}" logs deployment/soteria-controller-manager -c manager --tail=30 2>/dev/null || true
    fatal "Deployment rollout failed on ${ctx}"
  fi
  info "  Controller-manager is running on ${ctx}"
}

# ---------------------------------------------------------------------------
# Install sequence
# ---------------------------------------------------------------------------
do_install() {
  info "=== Installing Soteria ==="
  info "  Seed cluster:    ${EAST_CONTEXT}"
  info "  Joining cluster: ${WEST_CONTEXT}"
  info "  Chart:           ${CHART}"
  info "  Namespace:       ${NAMESPACE}"
  info "  Networking:      ${NETWORKING}"
  info "  UI mode:         ${UI_MODE}"
  echo ""

  # -----------------------------------------------------------------------
  # Step 1: Install on seed cluster (east)
  # -----------------------------------------------------------------------
  info "--- Step 1: Installing on seed cluster (${EAST_CONTEXT}) ---"
  local -a seed_args
  build_helm_args seed_args "${EAST_CONTEXT}" "${EAST_CONTEXT}" "seed"
  run helm "${seed_args[@]}"
  wait_for_rollout "${EAST_CONTEXT}"
  echo ""

  # -----------------------------------------------------------------------
  # Step 2: Wait for CA Certificate and copy to joining cluster
  # -----------------------------------------------------------------------
  info "--- Step 2: CA propagation ---"
  wait_for_ca "${EAST_CONTEXT}"
  copy_ca_secret "${EAST_CONTEXT}" "${WEST_CONTEXT}"
  echo ""

  # -----------------------------------------------------------------------
  # Step 3: Install on joining cluster (west)
  # -----------------------------------------------------------------------
  info "--- Step 3: Installing on joining cluster (${WEST_CONTEXT}) ---"
  local external_seeds
  external_seeds="$(resolve_external_seeds)"

  local -a join_args
  build_helm_args join_args "${WEST_CONTEXT}" "${WEST_CONTEXT}" "joining" \
    "scylladb.managed.externalSeeds[0]=${external_seeds}"
  run helm "${join_args[@]}"
  wait_for_rollout "${WEST_CONTEXT}"
  echo ""

  # -----------------------------------------------------------------------
  # Summary
  # -----------------------------------------------------------------------
  info "============================================"
  info "  Soteria installation complete!"
  info "============================================"
  info ""
  info "  Seed cluster:    ${EAST_CONTEXT} (role=seed)"
  info "  Joining cluster: ${WEST_CONTEXT} (role=joining)"
  info "  Namespace:       ${NAMESPACE}"
  info "  Networking:      ${NETWORKING}"
  info "  External seeds:  ${external_seeds}"
  info ""
  info "  Verify:"
  info "    kubectl --context ${EAST_CONTEXT} -n ${NAMESPACE} get pods"
  info "    kubectl --context ${WEST_CONTEXT} -n ${NAMESPACE} get pods"
  info "    kubectl --context ${EAST_CONTEXT} get apiservice v1alpha1.soteria.io"
  info ""
}

# ---------------------------------------------------------------------------
# Uninstall sequence (reverse order: joining first, then seed)
# ---------------------------------------------------------------------------
do_uninstall() {
  info "=== Uninstalling Soteria ==="
  info "  Joining cluster: ${WEST_CONTEXT} (uninstall first)"
  info "  Seed cluster:    ${EAST_CONTEXT} (uninstall second)"
  info "  Namespace:       ${NAMESPACE}"
  echo ""

  # -----------------------------------------------------------------------
  # Step 1: Uninstall from joining cluster
  # -----------------------------------------------------------------------
  info "--- Step 1: Uninstalling from joining cluster (${WEST_CONTEXT}) ---"
  run helm uninstall "${RELEASE_NAME}" \
    --kube-context "${WEST_CONTEXT}" \
    --namespace "${NAMESPACE}" \
    --wait 2>/dev/null || warn "Helm release '${RELEASE_NAME}' not found on ${WEST_CONTEXT} (already removed?)"
  echo ""

  # -----------------------------------------------------------------------
  # Step 2: Uninstall from seed cluster
  # -----------------------------------------------------------------------
  info "--- Step 2: Uninstalling from seed cluster (${EAST_CONTEXT}) ---"
  run helm uninstall "${RELEASE_NAME}" \
    --kube-context "${EAST_CONTEXT}" \
    --namespace "${NAMESPACE}" \
    --wait 2>/dev/null || warn "Helm release '${RELEASE_NAME}' not found on ${EAST_CONTEXT} (already removed?)"
  echo ""

  # -----------------------------------------------------------------------
  # Step 3: Clean up CA Secret and namespace (optional)
  # -----------------------------------------------------------------------
  info "--- Step 3: Cleaning up CA Secret ---"
  for ctx in "${WEST_CONTEXT}" "${EAST_CONTEXT}"; do
    run kubectl --context="${ctx}" -n "${NAMESPACE}" delete secret "${CA_SECRET_NAME}" --ignore-not-found 2>/dev/null || true
    info "  ${ctx}: CA Secret cleaned up"
  done
  echo ""

  info "============================================"
  info "  Soteria uninstall complete!"
  info "============================================"
  info ""
  info "  Namespaces were preserved. To remove:"
  info "    kubectl --context ${EAST_CONTEXT} delete namespace ${NAMESPACE}"
  info "    kubectl --context ${WEST_CONTEXT} delete namespace ${NAMESPACE}"
  info ""
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
  parse_flags "$@"

  if [[ "${UNINSTALL}" == "true" ]]; then
    if [[ "${DRY_RUN}" != "true" ]]; then
      check_prerequisites
    fi
    do_uninstall
  else
    if [[ "${DRY_RUN}" != "true" ]]; then
      check_prerequisites
    fi
    do_install
  fi
}

main "$@"
