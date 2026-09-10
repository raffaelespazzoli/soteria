#!/usr/bin/env bash
#
# upload-golden-images.sh
#
# Step 1.b of the ip-rewrite golden-image pipeline:
#   - Upload Windows qcow2 files from golden-images/ via virtctl image-upload
#   - Import or upload RHEL 7 (HTTP URL from the Customer Portal, or a local qcow2)
#   - Create/update DataSources in openshift-virtualization-os-images
#
# Usage:
#   ./upload-golden-images.sh
#   ./upload-golden-images.sh --only win-11
#   ./upload-golden-images.sh --only rhel7 --rhel7-url '<portal-url>'
#   ./upload-golden-images.sh --rhel7-url '<portal-url>'   # Windows + RHEL7
#
# Previous: ./build-windows-images-locally.sh
# Next:     ./provision-test-vms.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOLDEN_DIR="${SCRIPT_DIR}/golden-images"

GOLDEN_NS="${GOLDEN_NS:-openshift-virtualization-os-images}"
STORAGE_CLASS="${STORAGE_CLASS:-ocs-storagecluster-ceph-rbd-virtualization}"
PVC_SIZE="${PVC_SIZE:-60Gi}"
ACCESS_MODE="${ACCESS_MODE:-ReadWriteMany}"
VOLUME_MODE="${VOLUME_MODE:-block}"
UPLOAD_TIMEOUT="${UPLOAD_TIMEOUT:-600}"
RHEL7_STORAGE="${RHEL7_STORAGE:-30Gi}"
RHEL7_TIMEOUT="${RHEL7_TIMEOUT:-600}"
RHEL7_URL="${RHEL7_URL:-}"

WINDOWS_EDITIONS=(win-server-2016 win-server-2019 win-server-2022 win-server-2025 win-11)

info()  { echo "[INFO]  $(date '+%Y-%m-%d %H:%M:%S') $*"; }
warn()  { echo "[WARN]  $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }
error() { echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }
fatal() { error "$@"; exit 1; }

ONLY_EDITIONS=()
DRY_RUN=false
SKIP_RHEL7=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --only)
            IFS=',' read -ra ONLY_EDITIONS <<< "$2"
            shift 2
            ;;
        --rhel7-url)
            RHEL7_URL="$2"
            shift 2
            ;;
        --rhel7-storage)
            RHEL7_STORAGE="$2"
            shift 2
            ;;
        --skip-rhel7)
            SKIP_RHEL7=true
            shift
            ;;
        --storage-class)
            STORAGE_CLASS="$2"
            shift 2
            ;;
        --namespace)
            GOLDEN_NS="$2"
            shift 2
            ;;
        --pvc-size)
            PVC_SIZE="$2"
            shift 2
            ;;
        --access-mode)
            ACCESS_MODE="$2"
            shift 2
            ;;
        --volume-mode)
            VOLUME_MODE="$2"
            shift 2
            ;;
        --timeout)
            UPLOAD_TIMEOUT="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        -h|--help)
            cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Upload Windows golden QCOW2 images and optionally RHEL 7 to OpenShift.

Options:
  --only <list>          Editions: Windows names and/or rhel7
  --rhel7-url <url>      HTTP import URL (Red Hat Customer Portal, time-limited)
  --rhel7-storage <size> RHEL 7 PVC size (default: ${RHEL7_STORAGE})
  --skip-rhel7           Do not import/upload RHEL 7
  --storage-class <sc>   StorageClass for Windows PVCs (default: ${STORAGE_CLASS})
  --namespace <ns>       Target namespace (default: ${GOLDEN_NS})
  --pvc-size <size>      Windows PVC size (default: ${PVC_SIZE})
  --access-mode <mode>   PVC access mode (default: ${ACCESS_MODE})
  --volume-mode <mode>   PVC volume mode (default: ${VOLUME_MODE})
  --timeout <s>          virtctl upload timeout (default: ${UPLOAD_TIMEOUT})
  --dry-run              Show what would be done
  -h, --help             Show this help

RHEL 7: uses --rhel7-url (CDI HTTP import) or golden-images/rhel7.qcow2
(virtctl upload). Skipped if neither is present unless --only rhel7.

Environment:
  GOLDEN_NS, STORAGE_CLASS, PVC_SIZE, RHEL7_URL, RHEL7_STORAGE
EOF
            exit 0
            ;;
        *)
            fatal "Unknown argument: $1 (use --help for usage)"
            ;;
    esac
done

is_windows_edition() {
    local ed="$1"
    local valid
    for valid in "${WINDOWS_EDITIONS[@]}"; do
        [[ "${ed}" == "${valid}" ]] && return 0
    done
    return 1
}

upload_editions=()
WANT_RHEL7=false

if [[ ${#ONLY_EDITIONS[@]} -gt 0 ]]; then
    for ed in "${ONLY_EDITIONS[@]}"; do
        if [[ "${ed}" == "rhel7" ]]; then
            WANT_RHEL7=true
            continue
        fi
        if ! is_windows_edition "${ed}"; then
            fatal "Unknown edition: ${ed} (available: ${WINDOWS_EDITIONS[*]} rhel7)"
        fi
        upload_editions+=("${ed}")
    done
else
    upload_editions=("${WINDOWS_EDITIONS[@]}")
    WANT_RHEL7=true
fi

if [[ "${SKIP_RHEL7}" == "true" ]]; then
    WANT_RHEL7=false
fi

get_upload_proxy_url() {
    local route_url svc_url ns
    for ns in openshift-cnv cdi; do
        route_url=$(kubectl get route -n "${ns}" cdi-uploadproxy \
            -o jsonpath='{.spec.host}' 2>/dev/null || echo "")
        if [[ -n "${route_url}" ]]; then
            echo "https://${route_url}"
            return 0
        fi
    done
    for ns in openshift-cnv cdi; do
        svc_url=$(kubectl get svc -n "${ns}" cdi-uploadproxy \
            -o jsonpath='{.metadata.name}.{.metadata.namespace}.svc' 2>/dev/null || echo "")
        if [[ -n "${svc_url}" ]]; then
            echo "https://${svc_url}:443"
            return 0
        fi
    done
    echo ""
}

ensure_datasource() {
    local name="$1"
    kubectl apply -f - <<EOF
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataSource
metadata:
  name: ${name}
  namespace: ${GOLDEN_NS}
spec:
  source:
    pvc:
      name: ${name}
      namespace: ${GOLDEN_NS}
EOF
}

upload_windows_edition() {
    local edition="$1"
    local qcow2_path="${GOLDEN_DIR}/${edition}.qcow2"
    local pvc_name="${edition}"

    info "-----------------------------------------------------------------"
    info "  Uploading Windows: ${edition}"
    info "  Source: ${qcow2_path} ($(du -h "${qcow2_path}" | cut -f1))"
    info "  PVC: ${pvc_name} in ${GOLDEN_NS}"
    info "-----------------------------------------------------------------"

    if [[ "${DRY_RUN}" == "true" ]]; then
        info "[DRY RUN] Would upload ${edition}"
        return 0
    fi

    local existing_phase
    existing_phase=$(kubectl get pvc "${pvc_name}" -n "${GOLDEN_NS}" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

    if [[ "${existing_phase}" == "Bound" ]]; then
        warn "PVC '${pvc_name}' already Bound in ${GOLDEN_NS} — skipping"
        warn "Delete to re-upload: kubectl delete pvc ${pvc_name} -n ${GOLDEN_NS}"
        ensure_datasource "${pvc_name}"
        return 0
    fi

    if [[ "${existing_phase}" != "NotFound" ]]; then
        info "Deleting existing PVC in state '${existing_phase}'..."
        kubectl delete pvc "${pvc_name}" -n "${GOLDEN_NS}" --wait=true 2>/dev/null || true
        sleep 5
    fi

    info "Uploading via virtctl..."
    local proxy_url
    proxy_url="$(get_upload_proxy_url)"
    local -a upload_args=(
        image-upload pvc "${pvc_name}"
        --size="${PVC_SIZE}"
        --image-path="${qcow2_path}"
        --storage-class="${STORAGE_CLASS}"
        --namespace="${GOLDEN_NS}"
        --access-mode="${ACCESS_MODE}"
        --volume-mode="${VOLUME_MODE}"
        --insecure
    )
    if [[ -n "${proxy_url}" ]]; then
        upload_args+=(--uploadproxy-url="${proxy_url}")
    fi

    if ! ${VIRTCTL} "${upload_args[@]}"; then
        error "virtctl image-upload failed for ${edition}"
        return 1
    fi

    ensure_datasource "${pvc_name}"
    info "DataSource '${pvc_name}' ready"
}

import_rhel7_http() {
    local dv_name="rhel7"
    info "-----------------------------------------------------------------"
    info "  Importing RHEL 7 via HTTP"
    info "  Storage: ${RHEL7_STORAGE}"
    info "-----------------------------------------------------------------"

    if [[ "${DRY_RUN}" == "true" ]]; then
        info "[DRY RUN] Would HTTP-import RHEL 7"
        return 0
    fi

    local phase
    phase=$(kubectl get dv "${dv_name}" -n "${GOLDEN_NS}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [[ "${phase}" == "Succeeded" ]]; then
        info "RHEL 7 DataVolume already Succeeded — skipping"
        ensure_datasource "${dv_name}"
        return 0
    fi
    if [[ "${phase}" != "NotFound" ]]; then
        warn "RHEL 7 DataVolume in phase '${phase}' — recreating"
        kubectl delete dv "${dv_name}" -n "${GOLDEN_NS}" --wait=true 2>/dev/null || true
        kubectl delete pvc "${dv_name}" -n "${GOLDEN_NS}" --wait=true 2>/dev/null || true
        sleep 5
    fi

    kubectl apply -f - <<EOF
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: ${dv_name}
  namespace: ${GOLDEN_NS}
  annotations:
    cdi.kubevirt.io/storage.bind.immediate.requested: "true"
spec:
  source:
    http:
      url: "${RHEL7_URL}"
  storage:
    storageClassName: ${STORAGE_CLASS}
    resources:
      requests:
        storage: ${RHEL7_STORAGE}
EOF

    info "Waiting for RHEL 7 import (timeout: ${RHEL7_TIMEOUT}s)..."
    local elapsed=0
    while (( elapsed < RHEL7_TIMEOUT )); do
        phase=$(kubectl get dv "${dv_name}" -n "${GOLDEN_NS}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
        local progress
        progress=$(kubectl get dv "${dv_name}" -n "${GOLDEN_NS}" -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")
        if [[ "${phase}" == "Succeeded" ]]; then
            info "RHEL 7 import completed (${progress})"
            ensure_datasource "${dv_name}"
            return 0
        fi
        if [[ "${phase}" == "Failed" ]]; then
            error "RHEL 7 import failed — kubectl describe dv ${dv_name} -n ${GOLDEN_NS}"
            return 1
        fi
        if (( elapsed % 30 == 0 )); then
            info "  Import in progress: phase=${phase} progress=${progress}"
        fi
        sleep 10
        elapsed=$(( elapsed + 10 ))
    done
    error "RHEL 7 import timed out after ${RHEL7_TIMEOUT}s"
    return 1
}

upload_rhel7_local() {
    local qcow2_path="${GOLDEN_DIR}/rhel7.qcow2"
    info "-----------------------------------------------------------------"
    info "  Uploading RHEL 7 from ${qcow2_path}"
    info "-----------------------------------------------------------------"

    if [[ "${DRY_RUN}" == "true" ]]; then
        info "[DRY RUN] Would upload local rhel7.qcow2"
        return 0
    fi

    local existing_phase
    existing_phase=$(kubectl get pvc rhel7 -n "${GOLDEN_NS}" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [[ "${existing_phase}" == "Bound" ]]; then
        warn "PVC 'rhel7' already Bound — skipping"
        ensure_datasource rhel7
        return 0
    fi
    if [[ "${existing_phase}" != "NotFound" ]]; then
        kubectl delete pvc rhel7 -n "${GOLDEN_NS}" --wait=true 2>/dev/null || true
        sleep 5
    fi

    local proxy_url
    proxy_url="$(get_upload_proxy_url)"
    local -a upload_args=(
        image-upload pvc rhel7
        --size="${RHEL7_STORAGE}"
        --image-path="${qcow2_path}"
        --storage-class="${STORAGE_CLASS}"
        --namespace="${GOLDEN_NS}"
        --access-mode="${ACCESS_MODE}"
        --volume-mode="${VOLUME_MODE}"
        --insecure
    )
    if [[ -n "${proxy_url}" ]]; then
        upload_args+=(--uploadproxy-url="${proxy_url}")
    fi
    if ! ${VIRTCTL} "${upload_args[@]}"; then
        error "virtctl image-upload failed for rhel7"
        return 1
    fi
    ensure_datasource rhel7
}

handle_rhel7() {
    if [[ -n "${RHEL7_URL}" ]]; then
        import_rhel7_http
        return $?
    fi
    if [[ -f "${GOLDEN_DIR}/rhel7.qcow2" ]]; then
        upload_rhel7_local
        return $?
    fi
    if [[ ${#ONLY_EDITIONS[@]} -gt 0 ]]; then
        error "RHEL 7 requested but no --rhel7-url and no ${GOLDEN_DIR}/rhel7.qcow2"
        return 1
    fi
    warn "Skipping RHEL 7 (set --rhel7-url or place golden-images/rhel7.qcow2)"
    return 0
}

info "================================================================="
info "  Golden image uploader (Windows + RHEL 7)"
info "================================================================="
info "Target namespace: ${GOLDEN_NS}"
info "Storage class:    ${STORAGE_CLASS}"
info "Windows:          ${upload_editions[*]:-(none)}"
info "RHEL 7:           $([[ "${WANT_RHEL7}" == "true" ]] && echo yes || echo no)"

kubectl cluster-info >/dev/null 2>&1 \
    || fatal "Cannot reach cluster — are you logged in?"
kubectl get namespace "${GOLDEN_NS}" >/dev/null 2>&1 \
    || fatal "Namespace ${GOLDEN_NS} not found"

VIRTCTL=""
if command -v virtctl >/dev/null 2>&1; then
    VIRTCTL="virtctl"
elif kubectl plugin list 2>/dev/null | grep -q "kubectl-virt"; then
    VIRTCTL="kubectl virt"
else
    fatal "virtctl not found"
fi
info "Using: ${VIRTCTL}"

for ed in "${upload_editions[@]}"; do
    if [[ ! -f "${GOLDEN_DIR}/${ed}.qcow2" ]]; then
        fatal "Golden image not found: ${GOLDEN_DIR}/${ed}.qcow2 — run build-windows-images-locally.sh first"
    fi
done

if [[ "${DRY_RUN}" == "true" ]]; then
    info "*** DRY RUN MODE ***"
fi

SUCCESSES=0
FAILURES=0
TOTAL=0

for edition in "${upload_editions[@]}"; do
    echo ""
    TOTAL=$(( TOTAL + 1 ))
    if upload_windows_edition "${edition}"; then
        SUCCESSES=$(( SUCCESSES + 1 ))
    else
        FAILURES=$(( FAILURES + 1 ))
        error "Failed to upload ${edition}"
    fi
done

if [[ "${WANT_RHEL7}" == "true" ]]; then
    echo ""
    TOTAL=$(( TOTAL + 1 ))
    if handle_rhel7; then
        SUCCESSES=$(( SUCCESSES + 1 ))
    else
        FAILURES=$(( FAILURES + 1 ))
        error "Failed to import/upload RHEL 7"
    fi
fi

echo ""
info "================================================================="
info "  Upload Summary"
info "================================================================="
info "  Succeeded: ${SUCCESSES}/${TOTAL}"
if (( FAILURES > 0 )); then
    info "  Failed:    ${FAILURES}/${TOTAL}"
fi

if [[ "${DRY_RUN}" != "true" ]]; then
    echo ""
    info "DataSource status:"
    local_list=("${upload_editions[@]}")
    if [[ "${WANT_RHEL7}" == "true" ]]; then
        local_list+=("rhel7")
    fi
    for ed in "${local_list[@]}"; do
        ready=$(kubectl get datasource "${ed}" -n "${GOLDEN_NS}" \
            -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "N/A")
        pvc_phase=$(kubectl get pvc "${ed}" -n "${GOLDEN_NS}" \
            -o jsonpath='{.status.phase}' 2>/dev/null || echo "N/A")
        printf "  %-20s  datasource.ready=%-5s  pvc.phase=%s\n" "${ed}" "${ready}" "${pvc_phase}"
    done
fi

echo ""
if (( FAILURES > 0 )); then
    error "Some uploads failed. Re-run with --only <editions> to retry."
    exit 1
fi

info "All uploads completed successfully."
info "Next: ./provision-test-vms.sh"
