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

# Fixes Ceph OSD authentication after a monitor database reset.
#
# When the Ceph monitor loses its state (e.g. after a Minikube restart where
# the mon's hostPath data is recreated), Rook bootstraps a fresh monitor with
# new auth keys. The OSDs on disk still have their original keys, so they fail
# to authenticate and remain "down".
#
# This script reads each OSD's auth key from its bluestore on-disk label and
# imports it into the monitor, then restarts the OSD pods. It also clears the
# blocklist to remove stale entries from previous incarnations.
#
# If the OSDs were already purged from the monitor (osd tree is empty), the
# script restarts the Rook operator to trigger fresh OSD rediscovery.
#
# Prerequisites:
#   - Minikube KVM2 clusters with Rook-Ceph deployed
#   - Ceph toolbox pod running (rook-ceph-tools)
#   - kubectl context matching the cluster name
#
# Usage:
#   ./hack/multisite/fix-ceph-osd-auth.sh [cluster-name]
#
# Environment Variables:
#   CLUSTER_NAME   Cluster to fix (default: east)

set -euo pipefail

CLUSTER_NAME="${1:-${CLUSTER_NAME:-east}}"
CONTEXT="${CLUSTER_NAME}"
NAMESPACE="rook-ceph"

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

kctl() { kubectl --context "${CONTEXT}" -n "${NAMESPACE}" "$@"; }
ceph_cmd() { kctl exec deploy/rook-ceph-tools -- "$@"; }

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------
preflight() {
  info "Checking prerequisites for cluster '${CLUSTER_NAME}'..."

  command -v kubectl &>/dev/null || fatal "kubectl not found"
  kubectl --context "${CONTEXT}" cluster-info &>/dev/null 2>&1 \
    || fatal "Cannot reach cluster '${CLUSTER_NAME}' (context: ${CONTEXT})"

  kctl get deploy/rook-ceph-tools &>/dev/null 2>&1 \
    || fatal "Ceph toolbox not deployed in ${NAMESPACE}"

  ceph_cmd ceph status &>/dev/null 2>&1 \
    || fatal "Cannot run 'ceph status' via toolbox"
}

# ---------------------------------------------------------------------------
# Discover which OSDs exist in the monitor
# ---------------------------------------------------------------------------
get_mon_osd_ids() {
  ceph_cmd ceph osd ls 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Discover OSD pods currently running
# ---------------------------------------------------------------------------
get_osd_pod_ids() {
  kctl get pods -l app=rook-ceph-osd \
    -o jsonpath='{range .items[*]}{.metadata.labels.ceph-osd-id}{"\n"}{end}' 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Read the auth key from an OSD's bluestore on-disk label
# ---------------------------------------------------------------------------
read_osd_disk_key() {
  local osd_id="$1"
  local pod
  pod=$(kctl get pod -l "ceph-osd-id=${osd_id}" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null) || return 1

  kctl exec "${pod}" -c osd -- \
    ceph-bluestore-tool show-label --path "/var/lib/ceph/osd/ceph-${osd_id}" 2>/dev/null \
    | grep '"osd_key"' | sed 's/.*"osd_key": "\(.*\)".*/\1/' || return 1
}

# ---------------------------------------------------------------------------
# Import an OSD key into the monitor auth database
# ---------------------------------------------------------------------------
import_osd_key() {
  local osd_id="$1" key="$2"

  info "  Importing key for osd.${osd_id}..."
  ceph_cmd bash -c "
    ceph auth get osd.${osd_id} -o /tmp/osd.${osd_id}.key 2>/dev/null
    sed -i 's|key = .*|key = ${key}|' /tmp/osd.${osd_id}.key
    ceph auth import -i /tmp/osd.${osd_id}.key
    rm -f /tmp/osd.${osd_id}.key
  " 2>&1
}

# ---------------------------------------------------------------------------
# Sync all OSD keys from disk into the monitor
# ---------------------------------------------------------------------------
sync_osd_keys() {
  local osd_ids
  osd_ids=$(get_osd_pod_ids)

  if [[ -z "${osd_ids}" ]]; then
    warn "No OSD pods found — nothing to sync"
    return 1
  fi

  local count=0
  for osd_id in ${osd_ids}; do
    local key
    key=$(read_osd_disk_key "${osd_id}") || {
      warn "  Could not read key for osd.${osd_id} — skipping"
      continue
    }
    import_osd_key "${osd_id}" "${key}"
    count=$((count + 1))
  done

  if [[ ${count} -eq 0 ]]; then
    warn "No OSD keys could be read from disk"
    return 1
  fi

  info "Imported ${count} OSD key(s) into the monitor"
}

# ---------------------------------------------------------------------------
# Clear the OSD blocklist (stale entries from prior incarnations)
# ---------------------------------------------------------------------------
clear_blocklist() {
  local n
  n=$(ceph_cmd ceph osd blocklist ls 2>/dev/null | tail -1 | grep -oP '\d+(?= entries)') || n=0
  if [[ ${n} -gt 0 ]]; then
    info "Clearing ${n} blocklist entries..."
    ceph_cmd ceph osd blocklist clear 2>&1
  fi
}

# ---------------------------------------------------------------------------
# Restart OSD deployments so they reconnect with the corrected keys
# ---------------------------------------------------------------------------
restart_osds() {
  local deploys
  deploys=$(kctl get deploy -l app=rook-ceph-osd \
    -o jsonpath='{range .items[*]}{.metadata.name}{" "}{end}' 2>/dev/null || true)

  if [[ -z "${deploys}" ]]; then
    warn "No OSD deployments found to restart"
    return 1
  fi

  info "Restarting OSD deployments: ${deploys}"
  # shellcheck disable=SC2086
  kctl rollout restart deploy ${deploys}
}

# ---------------------------------------------------------------------------
# Wait for OSDs to come up
# ---------------------------------------------------------------------------
wait_osds_up() {
  local timeout=180 interval=10 elapsed=0
  info "Waiting up to ${timeout}s for OSDs to come up..."

  while [[ ${elapsed} -lt ${timeout} ]]; do
    local status
    status=$(ceph_cmd ceph osd stat 2>/dev/null) || true
    local up
    up=$(echo "${status}" | grep -oP '\d+(?= up)') || up=0
    local total
    total=$(echo "${status}" | grep -oP '^\d+') || total=0

    if [[ ${up} -gt 0 && ${up} -eq ${total} ]]; then
      info "All ${up} OSDs are up"
      return 0
    fi

    echo -n "."
    sleep "${interval}"
    elapsed=$((elapsed + interval))
  done

  echo ""
  warn "Timed out waiting for OSDs — current status:"
  ceph_cmd ceph status 2>&1
  return 1
}

# ---------------------------------------------------------------------------
# Handle the case where OSDs were already purged from the monitor
# ---------------------------------------------------------------------------
handle_no_osds_in_mon() {
  warn "No OSDs registered in the monitor"
  info "Checking if OSD disks need to be wiped for rediscovery..."

  local nodes
  nodes=$(kubectl --context "${CONTEXT}" get nodes --no-headers \
    -l 'node-role.kubernetes.io/control-plane!=' \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null)

  if [[ -z "${nodes}" ]]; then
    nodes=$(kubectl --context "${CONTEXT}" get nodes --no-headers \
      -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null | tail -n +2)
  fi

  for node in ${nodes}; do
    info "Wiping OSD disk (vdb) on ${node}..."
    minikube ssh -p "${CLUSTER_NAME}" -n "${node}" -- \
      "sudo dd if=/dev/zero of=/dev/vdb bs=1M count=100 2>/dev/null" || {
      warn "  Could not wipe vdb on ${node} — may not have the disk"
    }
    minikube ssh -p "${CLUSTER_NAME}" -n "${node}" -- \
      "sudo rm -rf /data/rook/osd* /var/lib/rook/osd* 2>/dev/null" || true
  done

  info "Restarting Rook operator to trigger OSD rediscovery..."
  kctl rollout restart deploy/rook-ceph-operator
  info "Operator restarted — OSDs will be provisioned automatically"
  info "Monitor progress: kubectl --context ${CONTEXT} -n ${NAMESPACE} logs -f deploy/rook-ceph-operator | grep -i osd"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
  info "=== Ceph OSD Auth Fix for cluster '${CLUSTER_NAME}' ==="

  preflight

  info "Checking Ceph health..."
  ceph_cmd ceph status 2>&1

  local mon_osds
  mon_osds=$(get_mon_osd_ids)

  if [[ -z "${mon_osds}" ]]; then
    handle_no_osds_in_mon
    return 0
  fi

  local osd_pods
  osd_pods=$(get_osd_pod_ids)

  if [[ -z "${osd_pods}" ]]; then
    warn "OSDs are registered in the monitor but no OSD pods are running"
    warn "Restart the operator: kubectl --context ${CONTEXT} -n ${NAMESPACE} rollout restart deploy/rook-ceph-operator"
    return 1
  fi

  local up_count
  up_count=$(ceph_cmd ceph osd stat 2>/dev/null | grep -oP '\d+(?= up)') || up_count=0

  if [[ ${up_count} -gt 0 ]]; then
    local total
    total=$(ceph_cmd ceph osd stat 2>/dev/null | grep -oP '^\d+') || total=0
    if [[ ${up_count} -eq ${total} ]]; then
      info "All ${up_count} OSDs are already up — no fix needed"
      return 0
    fi
    warn "${up_count}/${total} OSDs up — attempting key sync for the remaining"
  fi

  info "Syncing OSD auth keys from disk into the monitor..."
  sync_osd_keys || {
    error "Key sync failed — try the full OSD rebuild:"
    error "  1. Purge OSDs:  ceph osd purge <id> --yes-i-really-mean-it"
    error "  2. Then re-run: $0 ${CLUSTER_NAME}"
    return 1
  }

  clear_blocklist
  restart_osds || true

  if ! wait_osds_up; then
    warn ""
    warn "OSDs did not come up after key sync. The OSD map epoch mismatch may"
    warn "prevent boot. The full rebuild path is recommended:"
    warn ""
    warn "  # Purge OSDs from the monitor"
    warn "  ceph osd purge 0 --yes-i-really-mean-it"
    warn "  ceph osd purge 1 --yes-i-really-mean-it"
    warn "  ceph osd purge 2 --yes-i-really-mean-it"
    warn ""
    warn "  # Then re-run this script to wipe disks and rediscover"
    warn "  $0 ${CLUSTER_NAME}"
    return 1
  fi

  info ""
  info "=== Final Ceph status ==="
  ceph_cmd ceph status 2>&1
  info "OSD auth fix complete for cluster '${CLUSTER_NAME}'"
}

main
