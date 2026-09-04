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

# Deploys or cleans up the 6-VM DR test scenario on both clusters.
#
# Clones per-VM rootdisk DataVolumes from the golden Fedora PVC (created by
# validate-fedora-vm.sh) into PVCs on the mirrored StorageClass.  Creates VMs
# on east (Always) and west (Halted).  After the DRPlan converges, the
# ShadowPV pipeline provisions mirror PVs on west; this script then creates
# west PVCs pre-bound to those PVs so the DR lifecycle can proceed.
#
# Prerequisites:
#   - Minikube KVM2 clusters running (east + west)
#   - Rook-Ceph with mirrored-pool and StorageClasses deployed
#   - KubeVirt + CDI deployed (setup-kubevirt.sh)
#   - Golden Fedora image imported (validate-fedora-vm.sh)
#   - Soteria operator deployed on both clusters (deploy-soteria.sh)
#
# Usage:
#   ./hack/multisite/setup-test-vms.sh deploy snapshot   # snapshot-based mirroring
#   ./hack/multisite/setup-test-vms.sh deploy journal    # journal-based mirroring
#   ./hack/multisite/setup-test-vms.sh cleanup           # remove everything
#
# Environment Variables:
#   EAST_CLUSTER_NAME   Name of the east Minikube profile (default: east)
#   WEST_CLUSTER_NAME   Name of the west Minikube profile (default: west)
#   FEDORA_IMAGE        Fedora container disk image (default: quay.io/containerdisks/fedora:latest)
#   VM_MEMORY           Memory per VM (default: 256Mi)
#   DISK_SIZE           Root disk size per VM (default: 5Gi)
#   TEST_NAMESPACE      Namespace for test resources (default: soteria-dr-test)
#   PLAN_NAME           DRPlan name (default: ps-app)
#   DV_TIMEOUT          Timeout in seconds for DataVolume import (default: 600)
#   CONVERGE_TIMEOUT    Timeout in seconds for DRPlan convergence (default: 900)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
EAST_CLUSTER_NAME="${EAST_CLUSTER_NAME:-east}"
WEST_CLUSTER_NAME="${WEST_CLUSTER_NAME:-west}"
FEDORA_IMAGE="${FEDORA_IMAGE:-quay.io/containerdisks/fedora:latest}"
VM_MEMORY="${VM_MEMORY:-256Mi}"
DISK_SIZE="${DISK_SIZE:-5Gi}"
TEST_NAMESPACE="${TEST_NAMESPACE:-soteria-dr-test}"
PLAN_NAME="${PLAN_NAME:-ps-app}"
DV_TIMEOUT="${DV_TIMEOUT:-600}"
CONVERGE_TIMEOUT="${CONVERGE_TIMEOUT:-900}"

GOLDEN_NS="kubevirt-golden-images"
GOLDEN_DV="fedora-golden"

EAST_CONTEXT="${EAST_CLUSTER_NAME}"
WEST_CONTEXT="${WEST_CLUSTER_NAME}"

# VM definitions: name wave
VM_DEFS=(
  "ps-db 1"
  "ps-appserver-1 2"
  "ps-appserver-2 2"
  "ps-webserver-1 3"
  "ps-webserver-2 3"
  "ps-webserver-3 3"
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { error "$@"; exit 1; }

keast() { kubectl --context "${EAST_CONTEXT}" "$@"; }
kwest() { kubectl --context "${WEST_CONTEXT}" "$@"; }

# ---------------------------------------------------------------------------
# Resolve mirroring-mode parameters
# ---------------------------------------------------------------------------
resolve_mirroring_mode() {
  local mode="$1"
  case "${mode}" in
    snapshot)
      STORAGE_CLASS="rook-ceph-block"
      VRC_NAME="rook-ceph-rbd-vrc-snapshot"
      ;;
    journal)
      STORAGE_CLASS="rook-ceph-block-journal"
      VRC_NAME="rook-ceph-rbd-vrc-journal"
      ;;
    *)
      fatal "Unknown mirroring mode '${mode}'. Use 'snapshot' or 'journal'."
      ;;
  esac
  info "Mirroring mode: ${mode}"
  info "  StorageClass:           ${STORAGE_CLASS}"
  info "  VolumeReplicationClass: ${VRC_NAME}"
}

# ---------------------------------------------------------------------------
# Namespace
# ---------------------------------------------------------------------------
ensure_namespace() {
  local context="$1" name="$2"
  kubectl --context "${context}" create namespace "${TEST_NAMESPACE}" \
    --dry-run=client -o yaml | kubectl --context "${context}" apply -f - >/dev/null
  info "Namespace '${TEST_NAMESPACE}' ready on ${name}"
}

# ---------------------------------------------------------------------------
# DataVolume creation (east only) — cloned from golden image
# ---------------------------------------------------------------------------
create_datavolumes() {
  info "Cloning DataVolumes from golden image on east (StorageClass: ${STORAGE_CLASS})..."
  for def in "${VM_DEFS[@]}"; do
    local vm_name wave
    vm_name="$(echo "${def}" | cut -d' ' -f1)"
    wave="$(echo "${def}" | cut -d' ' -f2)"

    keast apply -f - <<EOF
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: ${vm_name}-rootdisk
  namespace: ${TEST_NAMESPACE}
  labels:
    soteria.io/drplan: ${PLAN_NAME}
    soteria.io/wave: "${wave}"
spec:
  source:
    pvc:
      namespace: ${GOLDEN_NS}
      name: ${GOLDEN_DV}
  storage:
    accessModes: ["ReadWriteOnce"]
    storageClassName: ${STORAGE_CLASS}
    volumeMode: Block
    resources:
      requests:
        storage: ${DISK_SIZE}
EOF
    info "  DataVolume ${vm_name}-rootdisk created (clone)"
  done
}

wait_datavolumes() {
  info "Waiting for DataVolume imports to complete (timeout: ${DV_TIMEOUT}s)..."
  local deadline=$(( SECONDS + DV_TIMEOUT ))
  while (( SECONDS < deadline )); do
    local all_done=true
    for def in "${VM_DEFS[@]}"; do
      local vm_name
      vm_name="$(echo "${def}" | cut -d' ' -f1)"
      local phase
      phase="$(keast -n "${TEST_NAMESPACE}" get datavolume "${vm_name}-rootdisk" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
      if [[ "${phase}" != "Succeeded" ]]; then
        all_done=false
        break
      fi
    done
    if ${all_done}; then
      info "All 6 DataVolume imports completed"
      return 0
    fi
    sleep 10
    local remaining=$(( deadline - SECONDS ))
    local progress=""
    for def in "${VM_DEFS[@]}"; do
      local vm_name
      vm_name="$(echo "${def}" | cut -d' ' -f1)"
      local phase
      phase="$(keast -n "${TEST_NAMESPACE}" get datavolume "${vm_name}-rootdisk" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "?")"
      progress+="${vm_name}=${phase} "
    done
    info "  [${remaining}s remaining] ${progress}"
  done
  fatal "DataVolume imports did not complete within ${DV_TIMEOUT}s"
}

# ---------------------------------------------------------------------------
# RBD image flattening — CDI clones produce images with a -temp parent that
# blocks RBD mirroring. Flatten removes the parent dependency.
# ---------------------------------------------------------------------------
flatten_rbd_images() {
  info "Flattening cloned RBD images to enable mirroring..."
  local pool
  pool="$(keast get storageclass "${STORAGE_CLASS}" -o jsonpath='{.parameters.pool}')"

  local toolbox_pod
  toolbox_pod="$(keast -n rook-ceph get pod -l app=rook-ceph-tools \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"

  if [[ -z "${toolbox_pod}" ]]; then
    fatal "Ceph toolbox pod not found — cannot flatten RBD images"
  fi

  local count=0
  for def in "${VM_DEFS[@]}"; do
    local vm_name
    vm_name="$(echo "${def}" | cut -d' ' -f1)"

    local pvc_name="${vm_name}-rootdisk"
    local vol_handle
    vol_handle="$(keast -n "${TEST_NAMESPACE}" get pvc "${pvc_name}" \
      -o jsonpath='{.spec.volumeName}' 2>/dev/null || true)"
    if [[ -z "${vol_handle}" ]]; then
      warn "PVC ${pvc_name} has no bound volume, skipping flatten"
      continue
    fi

    local image_name
    image_name="$(keast get pv "${vol_handle}" \
      -o jsonpath='{.spec.csi.volumeAttributes.imageName}' 2>/dev/null || true)"
    if [[ -z "${image_name}" ]]; then
      warn "PV ${vol_handle} has no imageName attribute, skipping flatten"
      continue
    fi

    local parent
    parent="$(keast -n rook-ceph exec "${toolbox_pod}" -- \
      rbd info "${pool}/${image_name}" 2>/dev/null | grep "parent:" || true)"
    if [[ -z "${parent}" ]]; then
      continue
    fi

    info "  Flattening ${image_name}..."
    keast -n rook-ceph exec "${toolbox_pod}" -- rbd flatten "${pool}/${image_name}"
    (( count++ )) || true
  done

  if (( count > 0 )); then
    info "Flattened ${count} cloned RBD image(s)"
  else
    info "No images needed flattening"
  fi
}

# ---------------------------------------------------------------------------
# VM creation
# ---------------------------------------------------------------------------
create_vms() {
  local context="$1" cluster_name="$2" run_strategy="$3"

  info "Creating VMs on ${cluster_name} (runStrategy: ${run_strategy})..."
  for def in "${VM_DEFS[@]}"; do
    local vm_name wave
    vm_name="$(echo "${def}" | cut -d' ' -f1)"
    wave="$(echo "${def}" | cut -d' ' -f2)"

    kubectl --context "${context}" apply -f - <<EOF
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: ${vm_name}
  namespace: ${TEST_NAMESPACE}
  labels:
    soteria.io/drplan: ${PLAN_NAME}
    soteria.io/wave: "${wave}"
spec:
  runStrategy: ${run_strategy}
  template:
    metadata:
      labels:
        soteria.io/drplan: ${PLAN_NAME}
        soteria.io/wave: "${wave}"
    spec:
      domain:
        resources:
          requests:
            memory: ${VM_MEMORY}
        devices:
          disks:
          - name: rootdisk
            disk:
              bus: virtio
      volumes:
      - name: rootdisk
        persistentVolumeClaim:
          claimName: ${vm_name}-rootdisk
EOF
    info "  VM ${vm_name} created on ${cluster_name}"
  done
}

wait_vms_running() {
  info "Waiting for VMs to reach Running on east (timeout: 300s)..."
  local deadline=$(( SECONDS + 300 ))
  while (( SECONDS < deadline )); do
    local running_count
    running_count="$(keast -n "${TEST_NAMESPACE}" get vmi --no-headers 2>/dev/null \
      | grep -c 'Running' || true)"
    if (( running_count == ${#VM_DEFS[@]} )); then
      info "All ${#VM_DEFS[@]} VMs running on east"
      return 0
    fi
    sleep 5
  done
  keast -n "${TEST_NAMESPACE}" get vmi 2>/dev/null || true
  fatal "Not all VMs reached Running on east within 300s"
}

# ---------------------------------------------------------------------------
# DRPlan
# ---------------------------------------------------------------------------
create_drplan() {
  info "Creating DRPlan '${PLAN_NAME}'..."
  keast apply -f - <<EOF
apiVersion: soteria.io/v1alpha1
kind: DRPlan
metadata:
  name: ${PLAN_NAME}
spec:
  primarySite: east
  secondarySite: west
  volumeReplicationDriver:
    type: csi-extension
    volumeReplicationClass: ${VRC_NAME}
  maxConcurrentFailovers: 2
EOF
  info "DRPlan '${PLAN_NAME}' created"
}

# ---------------------------------------------------------------------------
# ShadowPV convergence: wait for west PVs and create west PVCs
# ---------------------------------------------------------------------------
wait_shadowpv_pvs() {
  info "Waiting for ShadowPV consumer to create PVs on west (timeout: ${CONVERGE_TIMEOUT}s)..."
  local expected=${#VM_DEFS[@]}
  local deadline=$(( SECONDS + CONVERGE_TIMEOUT ))
  while (( SECONDS < deadline )); do
    local pv_count
    pv_count="$(kwest get pv -o json 2>/dev/null | python3 -c "
import json, sys
data = json.load(sys.stdin)
count = 0
for pv in data.get('items', []):
    ref = pv.get('spec', {}).get('claimRef', {})
    if ref.get('namespace') == '${TEST_NAMESPACE}' and 'rootdisk' in ref.get('name', ''):
        count += 1
print(count)
" 2>/dev/null || echo 0)"
    if (( pv_count >= expected )); then
      info "All ${expected} ShadowPV consumer PVs created on west"
      return 0
    fi
    sleep 10
    info "  [$(( deadline - SECONDS ))s remaining] ${pv_count}/${expected} PVs on west"
  done
  fatal "ShadowPV consumer PVs did not appear on west within ${CONVERGE_TIMEOUT}s"
}

create_west_pvcs() {
  info "Creating west PVCs pre-bound to ShadowPV PVs..."
  kwest get pv -o json 2>/dev/null | python3 -c "
import json, sys
data = json.load(sys.stdin)
for pv in data.get('items', []):
    ref = pv.get('spec', {}).get('claimRef', {})
    ns = ref.get('namespace', '')
    name = ref.get('name', '')
    pvname = pv['metadata']['name']
    if ns == '${TEST_NAMESPACE}' and 'rootdisk' in name:
        mode = pv.get('spec', {}).get('volumeMode', 'Block')
        print(f'''---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {name}
  namespace: ${TEST_NAMESPACE}
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: ${STORAGE_CLASS}
  volumeMode: {mode}
  volumeName: {pvname}
  resources:
    requests:
      storage: ${DISK_SIZE}''')
" | kwest apply -f -
  info "West PVCs created and binding to ShadowPV PVs"

  info "Waiting for west PVCs to bind..."
  local deadline=$(( SECONDS + 120 ))
  while (( SECONDS < deadline )); do
    local pending
    pending="$(kwest -n "${TEST_NAMESPACE}" get pvc --no-headers 2>/dev/null \
      | grep -cv 'Bound' || true)"
    if (( pending == 0 )); then
      info "All west PVCs bound"
      return 0
    fi
    sleep 5
  done
  kwest -n "${TEST_NAMESPACE}" get pvc 2>/dev/null || true
  fatal "West PVCs did not bind within 120s"
}

wait_drplan_healthy() {
  info "Waiting for DRPlan to converge (all conditions True, timeout: ${CONVERGE_TIMEOUT}s)..."
  local deadline=$(( SECONDS + CONVERGE_TIMEOUT ))
  while (( SECONDS < deadline )); do
    local phase conditions
    phase="$(keast get drplan "${PLAN_NAME}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    conditions="$(keast get drplan "${PLAN_NAME}" \
      -o jsonpath='{range .status.conditions[*]}{.type}={.status} {end}' 2>/dev/null || echo "")"

    local all_true=true
    for required in Ready SitesInSync DisksConsistent ReplicationHealthy; do
      if ! echo "${conditions}" | grep -q "${required}=True"; then
        all_true=false
        break
      fi
    done

    if ${all_true}; then
      info "DRPlan '${PLAN_NAME}' converged: ${phase} | ${conditions}"
      return 0
    fi
    sleep 10
    info "  [$(( deadline - SECONDS ))s remaining] ${phase} | ${conditions}"
  done
  fatal "DRPlan did not converge within ${CONVERGE_TIMEOUT}s"
}

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------
do_cleanup() {
  info "============================================================"
  info "Cleaning up test resources on both clusters"
  info "============================================================"

  info "Deleting DRPlan '${PLAN_NAME}'..."
  keast delete drplan "${PLAN_NAME}" --ignore-not-found 2>/dev/null || true

  info "Deleting DRExecutions for plan '${PLAN_NAME}'..."
  keast get drexecution --no-headers 2>/dev/null \
    | awk '{print $1}' \
    | grep "^${PLAN_NAME}" \
    | xargs -r keast delete drexecution --ignore-not-found 2>/dev/null || true

  info "Deleting ShadowPVs for plan '${PLAN_NAME}'..."
  keast get shadowpv --no-headers 2>/dev/null \
    | awk '{print $1}' \
    | grep "^${PLAN_NAME}" \
    | xargs -r keast delete shadowpv --ignore-not-found 2>/dev/null || true

  for context_name in "${EAST_CONTEXT} east" "${WEST_CONTEXT} west"; do
    local ctx cluster
    ctx="$(echo "${context_name}" | cut -d' ' -f1)"
    cluster="$(echo "${context_name}" | cut -d' ' -f2)"

    info "Cleaning ${cluster}..."

    info "  Deleting VMs..."
    kubectl --context "${ctx}" -n "${TEST_NAMESPACE}" delete vm --all --timeout=60s 2>/dev/null || true

    info "  Stripping VR finalizers..."
    kubectl --context "${ctx}" -n "${TEST_NAMESPACE}" get volumereplication --no-headers 2>/dev/null \
      | awk '{print $1}' \
      | while read -r vr; do
          kubectl --context "${ctx}" -n "${TEST_NAMESPACE}" patch volumereplication "${vr}" \
            --type=merge -p '{"metadata":{"finalizers":null}}' 2>/dev/null || true
        done

    info "  Deleting VolumeReplications..."
    kubectl --context "${ctx}" -n "${TEST_NAMESPACE}" delete volumereplication --all --timeout=30s 2>/dev/null || true

    info "  Deleting DataVolumes..."
    kubectl --context "${ctx}" -n "${TEST_NAMESPACE}" delete datavolume --all --timeout=60s 2>/dev/null || true

    info "  Deleting PVCs..."
    kubectl --context "${ctx}" -n "${TEST_NAMESPACE}" delete pvc --all --timeout=60s 2>/dev/null || true

    info "  Cleaning orphaned PVs..."
    kubectl --context "${ctx}" get pv -o json 2>/dev/null | python3 -c "
import json, sys
data = json.load(sys.stdin)
for pv in data.get('items', []):
    ref = pv.get('spec', {}).get('claimRef', {})
    if ref.get('namespace') == '${TEST_NAMESPACE}':
        print(pv['metadata']['name'])
" 2>/dev/null | while read -r pvname; do
      kubectl --context "${ctx}" patch pv "${pvname}" \
        --type=merge -p '{"metadata":{"finalizers":null}}' 2>/dev/null || true
      kubectl --context "${ctx}" delete pv "${pvname}" --ignore-not-found 2>/dev/null || true
    done

    info "  Deleting namespace '${TEST_NAMESPACE}'..."
    kubectl --context "${ctx}" delete namespace "${TEST_NAMESPACE}" \
      --ignore-not-found --timeout=120s 2>/dev/null || true
  done

  info "============================================================"
  info "Cleanup complete"
  info "============================================================"
}

# ---------------------------------------------------------------------------
# Deploy
# ---------------------------------------------------------------------------
do_deploy() {
  local mode="$1"
  resolve_mirroring_mode "${mode}"

  info "============================================================"
  info "Deploying 6-VM DR test scenario"
  info "  Mirroring:  ${mode}"
  info "  Namespace:  ${TEST_NAMESPACE}"
  info "  Plan:       ${PLAN_NAME}"
  info "  Fedora:     ${FEDORA_IMAGE}"
  info "  Disk size:  ${DISK_SIZE}"
  info "  VM memory:  ${VM_MEMORY}"
  info "============================================================"
  echo

  # Verify prerequisites
  info "Verifying prerequisites..."
  local golden_phase
  golden_phase="$(keast -n "${GOLDEN_NS}" get datavolume "${GOLDEN_DV}" \
    -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
  [[ "${golden_phase}" == "Succeeded" ]] \
    || fatal "Golden image '${GOLDEN_NS}/${GOLDEN_DV}' not found or not ready on east (phase: ${golden_phase:-not found}). Run validate-fedora-vm.sh first."
  keast get storageclass "${STORAGE_CLASS}" >/dev/null 2>&1 \
    || fatal "StorageClass '${STORAGE_CLASS}' not found on east"
  kwest get storageclass "${STORAGE_CLASS}" >/dev/null 2>&1 \
    || fatal "StorageClass '${STORAGE_CLASS}' not found on west"
  keast get volumereplicationclass "${VRC_NAME}" >/dev/null 2>&1 \
    || fatal "VolumeReplicationClass '${VRC_NAME}' not found on east"
  kwest get volumereplicationclass "${VRC_NAME}" >/dev/null 2>&1 \
    || fatal "VolumeReplicationClass '${VRC_NAME}' not found on west"
  keast -n soteria get deploy/soteria-controller-manager >/dev/null 2>&1 \
    || fatal "Soteria controller not deployed on east"
  kwest -n soteria get deploy/soteria-controller-manager >/dev/null 2>&1 \
    || fatal "Soteria controller not deployed on west"
  info "Prerequisites OK"

  # Create namespace on both clusters
  ensure_namespace "${EAST_CONTEXT}" "east"
  ensure_namespace "${WEST_CONTEXT}" "west"

  # Create DataVolumes on east (import Fedora image)
  create_datavolumes
  wait_datavolumes

  # Flatten cloned RBD images so mirroring can be enabled
  flatten_rbd_images

  # Create VMs
  create_vms "${EAST_CONTEXT}" "east" "Always"
  create_vms "${WEST_CONTEXT}" "west" "Halted"

  # Wait for east VMs to boot
  wait_vms_running

  # Create DRPlan
  create_drplan

  # Wait for ShadowPV pipeline to create west PVs, then create west PVCs
  wait_shadowpv_pvs
  create_west_pvcs

  # Wait for full DRPlan convergence
  wait_drplan_healthy

  info "============================================================"
  info "Deployment complete — ready for DR lifecycle testing"
  info "============================================================"
  echo
  info "VMs:"
  keast -n "${TEST_NAMESPACE}" get vmi --no-headers 2>/dev/null | sed 's/^/  /'
  echo
  info "DRPlan:"
  keast get drplan "${PLAN_NAME}" \
    -o jsonpath='  {.status.phase} activeSite={.status.activeSite} | {range .status.conditions[*]}{.type}={.status} {end}' 2>/dev/null
  echo
  echo
  info "Run a DR lifecycle with:"
  info "  kubectl --context east apply -f - <<< '{\"apiVersion\":\"soteria.io/v1alpha1\",\"kind\":\"DRExecution\",\"metadata\":{\"name\":\"<name>\"},\"spec\":{\"planName\":\"${PLAN_NAME}\",\"mode\":\"planned_migration\"}}'"
}

# ===========================================================================
# Main
# ===========================================================================
usage() {
  echo "Usage: $0 <command> [args]"
  echo
  echo "Commands:"
  echo "  deploy <snapshot|journal>   Deploy 6-VM scenario with the given mirroring mode"
  echo "  cleanup                     Remove all test resources from both clusters"
  echo
  echo "Examples:"
  echo "  $0 deploy snapshot   # Snapshot-based mirroring (rook-ceph-block + vrc-snapshot)"
  echo "  $0 deploy journal    # Journal-based mirroring  (rook-ceph-block-journal + vrc-journal)"
  echo "  $0 cleanup           # Clean up everything"
}

main() {
  local command="${1:-}"
  shift || true

  case "${command}" in
    deploy)
      local mode="${1:-}"
      [[ -n "${mode}" ]] || { usage; fatal "Missing mirroring mode. Use 'snapshot' or 'journal'."; }
      do_deploy "${mode}"
      ;;
    cleanup)
      do_cleanup
      ;;
    *)
      usage
      [[ -z "${command}" ]] || fatal "Unknown command '${command}'"
      exit 1
      ;;
  esac
}

main "$@"
