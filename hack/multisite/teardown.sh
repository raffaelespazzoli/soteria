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

# Tears down the multisite Minikube KVM2 cluster environment. Tolerant of
# missing clusters (safe to run even if setup was incomplete or already
# torn down).
#
# Usage:
#   ./hack/multisite/teardown.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"
ensure_minikube

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
KUBECONFIG_DIR="${KUBECONFIG_DIR:-${SCRIPT_DIR}/.kubeconfigs}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC}  $*"; }

# ---------------------------------------------------------------------------
# Cluster deletion
# ---------------------------------------------------------------------------
delete_cluster() {
  local name="$1"

  if minikube status -p "${name}" &>/dev/null 2>&1; then
    info "Deleting Minikube cluster '${name}'..."
    minikube delete -p "${name}"
    info "Cluster '${name}' deleted"
  else
    warn "Cluster '${name}' not found or already stopped, skipping"
  fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
info "=== Multisite Minikube Cluster Teardown ==="

delete_cluster "${EAST_CLUSTER_NAME}"
delete_cluster "${WEST_CLUSTER_NAME}"

# Clean up generated kubeconfig files
for f in "${KUBECONFIG_DIR}/east.kubeconfig" "${KUBECONFIG_DIR}/west.kubeconfig"; do
  if [[ -f "${f}" ]]; then
    rm -f "${f}"
    info "Removed ${f}"
  fi
done
if [[ -d "${KUBECONFIG_DIR}" ]] && [[ -z "$(ls -A "${KUBECONFIG_DIR}" 2>/dev/null)" ]]; then
  rmdir "${KUBECONFIG_DIR}"
  info "Removed empty directory: ${KUBECONFIG_DIR}"
fi

info "=== Teardown Complete ==="
