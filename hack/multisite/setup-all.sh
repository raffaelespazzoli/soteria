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

# Orchestrates the full multi-site test environment setup by calling individual
# setup scripts in sequence. Provides fail-fast semantics, --skip support for
# individual steps, a teardown subcommand, and a timing summary.
#
# Does NOT include deploy-soteria.sh — Soteria operator deployment belongs in
# the e2e test BeforeSuite for last-minute recompilation.
#
# Usage:
#   ./hack/multisite/setup-all.sh [--skip <step>]... [--help]
#   ./hack/multisite/setup-all.sh teardown [--skip <step>]...
#
# Steps (setup order):
#   clusters    — Minikube KVM2 clusters + Cilium Cluster Mesh (setup-clusters.sh)
#   rook-ceph   — Rook-Ceph + RBD mirroring (setup-rook-ceph.sh)
#   kubevirt    — KubeVirt + CDI (setup-kubevirt.sh)
#   fedora-vm   — Fedora VM validation + node sizing (validate-fedora-vm.sh)
#   scylladb    — ScyllaDB cross-DC deployment (setup-scylladb.sh)
#
# Environment Variables (aggregated from all sub-scripts):
#   EAST_CLUSTER_NAME     Name of the east cluster profile (default: east)
#   WEST_CLUSTER_NAME     Name of the west cluster profile (default: west)
#   KUBECONFIG_DIR        Directory for generated kubeconfigs (default: ./hack/multisite/.kubeconfigs)
#
#   setup-clusters.sh:
#   CILIUM_VERSION        Cilium Helm chart version
#   NODE_CPUS             vCPUs per node (default: 2)
#   MASTER_MEMORY         Memory for control-plane node in MB (default: 7168)
#   WORKER_MEMORY         Memory for worker nodes in MB (default: 5120)
#   DISK_SIZE             Disk size per node (default: 30g)
#   CONNECTIVITY_TEST     Set to "0" to skip cross-cluster connectivity test
#
#   setup-rook-ceph.sh:
#   ROOK_CHART_VERSION    Rook Helm chart version (default: latest)
#   CSI_ADDONS_TAG        CSI Addons release tag (default: v0.14.0)
#   SMOKE_TEST            Set to "0" to skip smoke tests (also used by kubevirt, scylladb)
#
#   setup-kubevirt.sh:
#   KUBEVIRT_VERSION      KubeVirt release to deploy (default: fetched from stable.txt)
#   CDI_VERSION           CDI release to deploy (default: fetched from GitHub)
#
#   validate-fedora-vm.sh:
#   FEDORA_IMAGE          Fedora container disk image (default: quay.io/containerdisks/fedora:latest)
#   CIRROS_IMAGE          Cirros container disk for pre-caching
#   VM_MEMORY             Memory for the test VM (default: 256Mi)
#   VM_BOOT_TIMEOUT       Timeout in seconds for VM to reach Running (default: 300)
#   GUEST_AGENT_TIMEOUT   Timeout in seconds for guest agent check (default: 300)
#   SKIP_CLEANUP          Set to "1" to skip cleanup of test resources
#
#   setup-scylladb.sh:
#   NAMESPACE             Namespace for ScyllaDB deployment (default: soteria)
#   CERT_MANAGER_VERSION  cert-manager Helm chart version (default: v1.20.2)
#   SCYLLA_OPERATOR_NS    scylla-operator namespace (default: scylla-operator)
#   MEMBERS_PER_RACK      ScyllaDB members per rack (default: 1)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

format_time() {
  local secs="$1"
  printf '%dm %ds' $((secs / 60)) $((secs % 60))
}

usage() {
  cat <<EOF
Usage: $(basename "$0") [--skip <step>]... [--help]
       $(basename "$0") teardown [--skip <step>]...

Orchestrates the full multi-site test environment.

Actions:
  (default)   Run all setup steps in sequence
  teardown    Tear down environment in reverse order

Options:
  --skip <step>   Skip one or more steps (repeatable)
  --help, -h      Show this help

Steps: clusters, rook-ceph, kubevirt, fedora-vm, scylladb
EOF
}

# ---------------------------------------------------------------------------
# Step Registry
# ---------------------------------------------------------------------------
STEP_NAMES=(clusters rook-ceph kubevirt fedora-vm scylladb)
STEP_SCRIPTS=(
  "${SCRIPT_DIR}/setup-clusters.sh"
  "${SCRIPT_DIR}/setup-rook-ceph.sh"
  "${SCRIPT_DIR}/setup-kubevirt.sh"
  "${SCRIPT_DIR}/validate-fedora-vm.sh"
  "${SCRIPT_DIR}/setup-scylladb.sh"
)
STEP_LABELS=(
  "Minikube KVM2 clusters + Cilium Cluster Mesh"
  "Rook-Ceph + RBD mirroring"
  "KubeVirt + CDI"
  "Fedora VM validation + node sizing"
  "ScyllaDB cross-DC deployment"
)

VALID_STEP_NAMES=("${STEP_NAMES[@]}")

# ---------------------------------------------------------------------------
# Argument Parsing
# ---------------------------------------------------------------------------
declare -A SKIP_SET=()
ACTION="setup"

while [[ $# -gt 0 ]]; do
  case "$1" in
    teardown)
      ACTION="teardown"
      shift
      ;;
    --skip)
      [[ $# -lt 2 ]] && fatal "--skip requires a step name"
      local_step="$2"
      # Warn on unknown step names
      valid=false
      for s in "${VALID_STEP_NAMES[@]}"; do
        if [[ "$s" == "$local_step" ]]; then
          valid=true
          break
        fi
      done
      if [[ "$valid" == "false" ]]; then
        warn "Unknown step name '${local_step}' — ignoring (valid: ${VALID_STEP_NAMES[*]})"
      else
        SKIP_SET["$local_step"]=1
      fi
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fatal "Unknown argument: $1. Use --help for usage."
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Skip announcement
# ---------------------------------------------------------------------------
if [[ ${#SKIP_SET[@]} -gt 0 ]]; then
  info "Skipping steps: ${!SKIP_SET[*]}"
fi

# ---------------------------------------------------------------------------
# Setup Action
# ---------------------------------------------------------------------------
run_step() {
  local name="$1" script="$2" label="$3"
  local start_seconds=$SECONDS

  info "━━━ Step: ${label} ━━━"
  local rc=0
  "${script}" || rc=$?
  local elapsed=$(( SECONDS - start_seconds ))

  if [[ $rc -ne 0 ]]; then
    error "Step '${name}' failed with exit code ${rc} after $(format_time ${elapsed})"
    exit "$rc"
  fi

  STEP_TIMES+=("${elapsed}")
  STEP_RESULTS+=("done")
  info "━━━ Step '${name}' completed in $(format_time ${elapsed}) ━━━"
  echo
}

run_setup() {
  # Validate all scripts exist and are executable before starting
  for i in "${!STEP_NAMES[@]}"; do
    if [[ -v "SKIP_SET[${STEP_NAMES[$i]}]" ]]; then
      continue
    fi
    [[ -x "${STEP_SCRIPTS[$i]}" ]] || fatal "Script not found or not executable: ${STEP_SCRIPTS[$i]}"
  done

  local total_start=$SECONDS
  STEP_TIMES=()
  STEP_RESULTS=()

  for i in "${!STEP_NAMES[@]}"; do
    local name="${STEP_NAMES[$i]}"
    if [[ -v "SKIP_SET[$name]" ]]; then
      STEP_TIMES+=("0")
      STEP_RESULTS+=("skipped")
      continue
    fi
    run_step "$name" "${STEP_SCRIPTS[$i]}" "${STEP_LABELS[$i]}"
  done

  local total_elapsed=$(( SECONDS - total_start ))
  print_summary "$total_elapsed"
}

# ---------------------------------------------------------------------------
# Teardown Action
# ---------------------------------------------------------------------------
TEARDOWN_NAMES=(scylladb kubevirt rook-ceph clusters)

teardown_scylladb() {
  info "Tearing down ScyllaDB..."
  local kubeconfig_dir="${KUBECONFIG_DIR:-${SCRIPT_DIR}/.kubeconfigs}"

  for site in east west; do
    local kc="${kubeconfig_dir}/${site}.kubeconfig"
    if [[ ! -f "$kc" ]]; then
      warn "Kubeconfig not found for '${site}', skipping ScyllaDB teardown on this cluster"
      continue
    fi
    info "Removing ScyllaDB resources from cluster '${site}'..."
    kubectl --kubeconfig="$kc" delete namespace soteria --ignore-not-found --timeout=120s 2>/dev/null || true
    helm --kubeconfig="$kc" uninstall scylla-operator -n scylla-operator 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete namespace scylla-operator --ignore-not-found --timeout=60s 2>/dev/null || true
    helm --kubeconfig="$kc" uninstall cert-manager -n cert-manager 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete namespace cert-manager --ignore-not-found --timeout=60s 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete clusterissuer soteria-selfsigned --ignore-not-found 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete storageclass rook-ceph-block-xfs --ignore-not-found 2>/dev/null || true
  done
}

teardown_kubevirt() {
  info "Tearing down KubeVirt + CDI..."
  local kubeconfig_dir="${KUBECONFIG_DIR:-${SCRIPT_DIR}/.kubeconfigs}"

  for site in east west; do
    local kc="${kubeconfig_dir}/${site}.kubeconfig"
    if [[ ! -f "$kc" ]]; then
      warn "Kubeconfig not found for '${site}', skipping KubeVirt teardown on this cluster"
      continue
    fi
    info "Removing KubeVirt resources from cluster '${site}'..."
    kubectl --kubeconfig="$kc" delete kubevirt kubevirt -n kubevirt --ignore-not-found --timeout=120s 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete namespace kubevirt --ignore-not-found --timeout=120s 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete cdi cdi -n cdi --ignore-not-found --timeout=120s 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete namespace cdi --ignore-not-found --timeout=120s 2>/dev/null || true
  done
}

teardown_rook_ceph() {
  info "Tearing down Rook-Ceph..."
  local kubeconfig_dir="${KUBECONFIG_DIR:-${SCRIPT_DIR}/.kubeconfigs}"

  for site in east west; do
    local kc="${kubeconfig_dir}/${site}.kubeconfig"
    if [[ ! -f "$kc" ]]; then
      warn "Kubeconfig not found for '${site}', skipping Rook-Ceph teardown on this cluster"
      continue
    fi
    info "Removing Rook-Ceph resources from cluster '${site}'..."
    kubectl --kubeconfig="$kc" delete cephcluster --all -n rook-ceph --ignore-not-found --timeout=180s 2>/dev/null || true
    helm --kubeconfig="$kc" uninstall rook-ceph -n rook-ceph 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete namespace rook-ceph --ignore-not-found --timeout=120s 2>/dev/null || true
    helm --kubeconfig="$kc" uninstall csi-addons -n csi-addons-system 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete namespace csi-addons-system --ignore-not-found --timeout=60s 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete volumereplicationclass rook-ceph-rbd-vrc-snapshot rook-ceph-rbd-vrc-journal --ignore-not-found 2>/dev/null || true
    kubectl --kubeconfig="$kc" delete storageclass rook-ceph-block --ignore-not-found 2>/dev/null || true
  done
}

teardown_clusters() {
  info "Tearing down Minikube clusters..."
  "${SCRIPT_DIR}/teardown.sh"
}

run_teardown() {
  local total_start=$SECONDS
  STEP_TIMES=()
  STEP_RESULTS=()

  # Build the result arrays for all steps (for summary table)
  # Initialize with placeholder entries for all setup steps
  for i in "${!STEP_NAMES[@]}"; do
    STEP_TIMES+=("0")
    STEP_RESULTS+=("—")
  done

  info "=== Multisite Environment Teardown ==="
  echo

  for name in "${TEARDOWN_NAMES[@]}"; do
    if [[ -v "SKIP_SET[$name]" ]]; then
      warn "Skipping teardown of '${name}'"
      continue
    fi

    local start_seconds=$SECONDS
    info "━━━ Teardown: ${name} ━━━"

    # Run in error-tolerant mode
    set +e
    case "$name" in
      scylladb)   teardown_scylladb ;;
      kubevirt)   teardown_kubevirt ;;
      rook-ceph)  teardown_rook_ceph ;;
      clusters)   teardown_clusters ;;
    esac
    local rc=$?
    set -e

    local elapsed=$(( SECONDS - start_seconds ))
    if [[ $rc -ne 0 ]]; then
      warn "Teardown of '${name}' had errors (exit code: ${rc}) — continuing"
    fi
    info "━━━ Teardown '${name}' finished in $(format_time ${elapsed}) ━━━"
    echo
  done

  local total_elapsed=$(( SECONDS - total_start ))
  info "=== Teardown Complete (total: $(format_time ${total_elapsed})) ==="
}

# ---------------------------------------------------------------------------
# Summary Table
# ---------------------------------------------------------------------------
print_summary() {
  local total_elapsed="$1"

  echo
  info "=== Setup Complete ==="
  echo
  printf '  %-16s %-10s %s\n' "Step" "Status" "Elapsed"
  printf '  %s\n' "─────────────────────────────────────────"

  for i in "${!STEP_NAMES[@]}"; do
    local name="${STEP_NAMES[$i]}"
    local result="${STEP_RESULTS[$i]}"
    local elapsed_str="—"
    if [[ "$result" == "done" ]]; then
      elapsed_str="$(format_time "${STEP_TIMES[$i]}")"
    fi
    printf '  %-16s %-10s %s\n' "$name" "$result" "$elapsed_str"
  done

  printf '  %s\n' "─────────────────────────────────────────"
  printf '  %-16s %-10s %s\n' "Total" "" "$(format_time ${total_elapsed})"
  echo
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
case "$ACTION" in
  setup)    run_setup ;;
  teardown) run_teardown ;;
esac
