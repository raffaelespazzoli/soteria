#!/usr/bin/env bash
#
# build-windows-images-locally.sh
#
# Step 1.a of the ip-rewrite golden-image pipeline:
#   1. Unattended Windows install from ISOs in windows-images/
#   2. After setup shuts down, start the VM with the virtio-win CD attached
#   3. Print console + login credentials; wait for you to install guest tools
#   4. ACPI stop and extract a compressed qcow2 into golden-images/
#
# One edition at a time, with enough RAM/CPU for a smooth desktop install.
#
# Usage:
#   ./build-windows-images-locally.sh
#   ./build-windows-images-locally.sh --only win-11
#   ./build-windows-images-locally.sh --ram 8192 --vcpus 8
#
# Next: ./upload-golden-images.sh
#
# Prerequisites:
#   - virt-install, qemu-img, virsh, genisoimage (or mkisofs/xorrisofs)
#   - Windows evaluation ISOs in hack/ip-rewriter/windows-images/
#   - libvirt qemu:///system (libvirt group membership)

set -euo pipefail

export LIBVIRT_DEFAULT_URI="qemu:///system"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ISO_DIR="${SCRIPT_DIR}/windows-images"
GOLDEN_DIR="${SCRIPT_DIR}/golden-images"
AUTOUNATTEND_DIR="${SCRIPT_DIR}/autounattend"

VIRTIO_WIN_URL="https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso"
VIRTIO_WIN_ISO="${ISO_DIR}/virtio-win.iso"

# Plenty of resources so the desktop install and guest-tools MSI are usable.
RAM_MB="${RAM_MB:-8192}"
VCPUS="${VCPUS:-8}"
DISK_SIZE="${DISK_SIZE:-60G}"
INSTALL_TIMEOUT="${INSTALL_TIMEOUT:-5400}"
STOP_TIMEOUT="${STOP_TIMEOUT:-180}"

WIN_PASSWORD="${WIN_PASSWORD:-Passw0rd!}"

declare -A EDITIONS
EDITIONS[win-server-2016]="win-server-2016.iso|win2k16|win-server-2016.xml"
EDITIONS[win-server-2019]="win-server-2019.iso|win2k19|win-server-2019.xml"
EDITIONS[win-server-2022]="win-server-2022.iso|win2k22|win-server-2022.xml"
EDITIONS[win-server-2025]="win-server-2025.iso|win2k25|win-server-2025.xml"
EDITIONS[win-11]="win-11.iso|win11|win-11.xml"

EDITION_ORDER=(win-server-2016 win-server-2019 win-server-2022 win-server-2025 win-11)

info()  { echo "[INFO]  $(date '+%Y-%m-%d %H:%M:%S') $*"; }
warn()  { echo "[WARN]  $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }
error() { echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }
fatal() { error "$@"; exit 1; }

guest_username() {
    case "$1" in
        win-11) echo "User" ;;
        *) echo "Administrator" ;;
    esac
}

DRY_RUN=false
ONLY_EDITIONS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --only)
            IFS=',' read -ra ONLY_EDITIONS <<< "$2"
            shift 2
            ;;
        --ram)
            RAM_MB="$2"
            shift 2
            ;;
        --vcpus)
            VCPUS="$2"
            shift 2
            ;;
        --disk-size)
            DISK_SIZE="$2"
            shift 2
            ;;
        --timeout)
            INSTALL_TIMEOUT="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        -h|--help)
            cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Build Windows golden QCOW2 images locally (unattended OS + interactive guest tools).

Options:
  --only <list>     Comma-separated editions (e.g. win-server-2022,win-11)
  --ram <MB>        RAM per VM in MB (default: ${RAM_MB})
  --vcpus <N>       vCPUs per VM (default: ${VCPUS})
  --disk-size <S>   Disk size (default: ${DISK_SIZE})
  --timeout <S>     Install timeout in seconds (default: ${INSTALL_TIMEOUT})
  --dry-run         Show what would be done without executing
  -h, --help        Show this help

Editions: ${EDITION_ORDER[*]}

After unattended setup the VM is started with the virtio-win CD attached.
Log in from virt-viewer / virt-manager, install guest tools, then press Enter.
EOF
            exit 0
            ;;
        *)
            fatal "Unknown argument: $1 (use --help for usage)"
            ;;
    esac
done

build_editions=()
if [[ ${#ONLY_EDITIONS[@]} -gt 0 ]]; then
    for ed in "${ONLY_EDITIONS[@]}"; do
        if [[ -z "${EDITIONS[${ed}]+x}" ]]; then
            fatal "Unknown edition: ${ed} (available: ${EDITION_ORDER[*]})"
        fi
        build_editions+=("${ed}")
    done
else
    build_editions=("${EDITION_ORDER[@]}")
fi

preflight() {
    info "=== Pre-flight checks ==="

    local missing=()
    for cmd in virt-install qemu-img virsh; do
        if ! command -v "${cmd}" >/dev/null 2>&1; then
            missing+=("${cmd}")
        fi
    done

    local iso_tool=""
    for cmd in genisoimage mkisofs xorrisofs; do
        if command -v "${cmd}" >/dev/null 2>&1; then
            iso_tool="${cmd}"
            break
        fi
    done
    if [[ -z "${iso_tool}" ]]; then
        missing+=("genisoimage/mkisofs/xorrisofs")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        fatal "Missing required tools: ${missing[*]}"
    fi
    info "All required tools found (ISO tool: ${iso_tool})"

    if ! virsh version >/dev/null 2>&1; then
        fatal "Cannot connect to libvirt. Ensure libvirtd or virtqemud is running"
    fi
    info "libvirt connection OK"

    if virsh net-info default >/dev/null 2>&1 && \
       virsh net-info default 2>/dev/null | grep -q "Active:.*yes"; then
        NETWORK_ARG="--network network=default,model=e1000e"
        info "Default network available"
    else
        NETWORK_ARG="--network none"
        warn "Default libvirt network not active — VMs will build without NIC"
        warn "To enable: sudo virsh net-start default"
    fi

    if [[ ! -d "${ISO_DIR}" ]]; then
        fatal "ISO directory not found: ${ISO_DIR}"
    fi

    for ed in "${build_editions[@]}"; do
        IFS='|' read -r iso_file _ _ <<< "${EDITIONS[${ed}]}"
        if [[ ! -f "${ISO_DIR}/${iso_file}" ]]; then
            fatal "ISO not found for ${ed}: ${ISO_DIR}/${iso_file}"
        fi
    done
    info "All required ISOs found"

    for ed in "${build_editions[@]}"; do
        IFS='|' read -r _ _ answer_file <<< "${EDITIONS[${ed}]}"
        if [[ ! -f "${AUTOUNATTEND_DIR}/${answer_file}" ]]; then
            fatal "Autounattend file not found for ${ed}: ${AUTOUNATTEND_DIR}/${answer_file}"
        fi
    done
    info "All autounattend files found"

    local needed_gb=$(( ${#build_editions[@]} * 60 + 10 ))
    local avail_kb
    avail_kb=$(df --output=avail "${SCRIPT_DIR}" | tail -1 | tr -d ' ')
    local avail_gb=$(( avail_kb / 1024 / 1024 ))
    if (( avail_gb < needed_gb )); then
        warn "Low disk space: ${avail_gb}GB available, ~${needed_gb}GB needed"
    else
        info "Disk space OK: ${avail_gb}GB available (need ~${needed_gb}GB)"
    fi
}

ensure_libvirt_pool() {
    mkdir -p "${GOLDEN_DIR}"
    if virsh pool-info golden-images >/dev/null 2>&1; then
        virsh pool-start golden-images >/dev/null 2>&1 || true
        virsh pool-refresh golden-images >/dev/null 2>&1 || true
        return 0
    fi
    info "Defining libvirt dir pool golden-images -> ${GOLDEN_DIR}"
    virsh pool-define-as golden-images dir --target "${GOLDEN_DIR}"
    virsh pool-build golden-images
    virsh pool-start golden-images
    virsh pool-autostart golden-images
}

download_virtio_win() {
    if [[ -f "${VIRTIO_WIN_ISO}" ]]; then
        info "VirtIO drivers ISO already present: ${VIRTIO_WIN_ISO}"
        return 0
    fi

    info "Downloading VirtIO drivers ISO..."
    if [[ "${DRY_RUN}" == "true" ]]; then
        info "[DRY RUN] Would download virtio-win.iso"
        return 0
    fi

    mkdir -p "${ISO_DIR}"
    if ! curl -fSL --progress-bar -o "${VIRTIO_WIN_ISO}.tmp" "${VIRTIO_WIN_URL}"; then
        rm -f "${VIRTIO_WIN_ISO}.tmp"
        fatal "Failed to download VirtIO drivers ISO"
    fi
    mv "${VIRTIO_WIN_ISO}.tmp" "${VIRTIO_WIN_ISO}"
    info "VirtIO ISO downloaded: $(du -h "${VIRTIO_WIN_ISO}" | cut -f1)"
}

make_answer_iso() {
    local edition="$1"
    local answer_xml="$2"
    local output_iso="$3"

    local tmpdir
    tmpdir=$(mktemp -d "/tmp/autounattend-${edition}-XXXXXX")

    cp "${answer_xml}" "${tmpdir}/autounattend.xml"

    local setup_complete="${SCRIPT_DIR}/SetupComplete.cmd"
    if [[ -f "${setup_complete}" ]]; then
        mkdir -p "${tmpdir}/\$OEM\$/\$\$/Setup/Scripts"
        cp "${setup_complete}" "${tmpdir}/\$OEM\$/\$\$/Setup/Scripts/SetupComplete.cmd"
        cp "${setup_complete}" "${tmpdir}/SetupComplete.cmd"
    fi

    if command -v genisoimage >/dev/null 2>&1; then
        genisoimage -quiet -o "${output_iso}" -joliet -rock -volid "OEMDRV" "${tmpdir}" 2>/dev/null
    elif command -v mkisofs >/dev/null 2>&1; then
        mkisofs -quiet -o "${output_iso}" -joliet -rock -volid "OEMDRV" "${tmpdir}" 2>/dev/null
    elif command -v xorrisofs >/dev/null 2>&1; then
        xorrisofs -quiet -o "${output_iso}" -joliet -rock -volid "OEMDRV" "${tmpdir}" 2>/dev/null
    fi

    rm -rf "${tmpdir}"
}

dom_state() {
    local vm_name="$1"
    if virsh domstate "${vm_name}" >/dev/null 2>&1; then
        virsh domstate "${vm_name}" 2>/dev/null | xargs
    else
        echo "gone"
    fi
}

wait_shut_off() {
    local vm_name="$1"
    local timeout="$2"
    local elapsed=0
    while (( elapsed < timeout )); do
        local state
        state=$(dom_state "${vm_name}")
        if [[ "${state}" == "shut off" || "${state}" == "gone" ]]; then
            return 0
        fi
        sleep 5
        elapsed=$(( elapsed + 5 ))
    done
    return 1
}

acpi_stop() {
    local vm_name="$1"
    local state
    state=$(dom_state "${vm_name}")
    if [[ "${state}" == "shut off" || "${state}" == "gone" ]]; then
        info "VM '${vm_name}' already shut off"
        return 0
    fi
    info "Sending ACPI shutdown to ${vm_name}"
    virsh shutdown "${vm_name}" >/dev/null 2>&1 || true
    if wait_shut_off "${vm_name}" 60; then
        return 0
    fi
    warn "ACPI stop timed out — forcing destroy"
    virsh destroy "${vm_name}" >/dev/null 2>&1 || true
    wait_shut_off "${vm_name}" "${STOP_TIMEOUT}" || true
}

prompt_guest_tools() {
    local edition="$1"
    local vm_name="$2"
    local user
    user=$(guest_username "${edition}")

    cat <<EOF

=================================================================
  ${edition}: install VirtIO guest tools
=================================================================
  The VM is running with the virtio-win CD attached.

  Console:   virt-viewer --connect qemu:///system ${vm_name}
             (or open virt-manager and select ${vm_name})

  Username:  ${user}
  Password:  ${WIN_PASSWORD}

  In the guest, run virtio-win-guest-tools.exe from the CD
  (often D:). Reboot in the guest if the installer asks.

  When guest tools are installed, press Enter here to ACPI-stop
  the VM and extract the qcow2.
=================================================================
EOF

    if [[ ! -t 0 && ! -r /dev/tty ]]; then
        fatal "No TTY to wait for guest-tools confirmation"
    fi
    # Read from the controlling terminal so this works if stdin is redirected.
    read -r -p "Press Enter when guest tools are installed... " < /dev/tty
}

wait_unattended_install() {
    local vm_name="$1"
    local elapsed=0
    local poll_interval=15
    local phase=1
    local max_restart_phases=5

    while (( elapsed < INSTALL_TIMEOUT )); do
        local state
        state=$(dom_state "${vm_name}")

        if [[ "${state}" == "shut off" ]]; then
            if (( phase <= max_restart_phases )); then
                phase=$(( phase + 1 ))
                info "VM shut off at ${elapsed}s — restarting for install phase ${phase}..."
                virsh start "${vm_name}" 2>/dev/null || true
                sleep 10
                elapsed=$(( elapsed + 10 ))
                continue
            fi
            info "Unattended install finished after ${elapsed}s (phase ${phase})"
            return 0
        fi

        if [[ "${state}" == "gone" ]]; then
            error "Domain disappeared during install at ${elapsed}s"
            return 1
        fi

        if (( elapsed % 120 == 0 )); then
            info "  Waiting... elapsed=$(( elapsed / 60 ))m state=${state} phase=${phase}"
        fi

        if (( elapsed == 1200 )); then
            warn "VM running for 20min — sending ACPI shutdown in case autounattend did not fire"
            virsh shutdown "${vm_name}" 2>/dev/null || true
        fi

        sleep "${poll_interval}"
        elapsed=$(( elapsed + poll_interval ))
    done

    error "Install timed out after ${INSTALL_TIMEOUT}s"
    error "Inspect with: virt-viewer --connect qemu:///system ${vm_name}"
    return 1
}

extract_qcow() {
    local edition="$1"
    local disk_raw="$2"
    local disk_path="$3"

    if [[ ! -f "${disk_raw}" ]]; then
        error "Raw disk not found: ${disk_raw}"
        return 1
    fi

    virsh pool-refresh golden-images >/dev/null 2>&1 || true
    info "Downloading raw image from libvirt pool..."
    local raw_tmp="${GOLDEN_DIR}/${edition}-raw-tmp.qcow2"
    if ! virsh vol-download --pool golden-images "${edition}-raw.qcow2" "${raw_tmp}" 2>/dev/null; then
        warn "vol-download failed — trying direct copy"
        cp "${disk_raw}" "${raw_tmp}" || {
            error "Cannot read raw disk: ${disk_raw}"
            return 1
        }
    fi

    info "Compressing QCOW2 image..."
    qemu-img convert -c -O qcow2 "${raw_tmp}" "${disk_path}"
    rm -f "${raw_tmp}"
    virsh vol-delete --pool golden-images "${edition}-raw.qcow2" 2>/dev/null || rm -f "${disk_raw}"

    local size
    size=$(du -h "${disk_path}" | cut -f1)
    info "Golden image ready: ${disk_path} (${size})"
}

verify_guest_agent() {
    local disk_path="$1"
    local edition="$2"

    info "Verifying QEMU guest agent in ${edition}..."
    if ! command -v virt-ls >/dev/null 2>&1; then
        warn "virt-ls not available — skipping guest agent verification"
        return 0
    fi

    local ga_paths=(
        '/Program Files/QEMU Guest Agent'
        '/Program Files/Virtio-Win/Qemu-GA'
        '/Program Files (x86)/QEMU Guest Agent'
    )
    local ga_path
    for ga_path in "${ga_paths[@]}"; do
        if virt-ls -a "${disk_path}" "${ga_path}" 2>/dev/null | grep -qi "qemu-ga"; then
            info "QEMU Guest Agent found at: ${ga_path}"
            return 0
        fi
    done

    local qemu_ga_hit
    qemu_ga_hit=$(virt-ls -R -a "${disk_path}" '/Program Files/' 2>/dev/null | grep -i "qemu-ga.exe" | head -1 || true)
    if [[ -n "${qemu_ga_hit}" ]]; then
        info "QEMU Guest Agent found: /Program Files/${qemu_ga_hit}"
        return 0
    fi

    warn "QEMU Guest Agent not detected in ${edition} — guest IP reporting may fail"
    return 1
}

build_edition() {
    local edition="$1"
    IFS='|' read -r iso_file os_variant answer_file <<< "${EDITIONS[${edition}]}"

    local iso_path="${ISO_DIR}/${iso_file}"
    local answer_path="${AUTOUNATTEND_DIR}/${answer_file}"
    local disk_path="${GOLDEN_DIR}/${edition}.qcow2"
    local disk_raw="${GOLDEN_DIR}/${edition}-raw.qcow2"
    local answer_iso="${GOLDEN_DIR}/${edition}-autounattend.iso"
    local vm_name="golden-${edition}"

    info "================================================================="
    info "  Building: ${edition}"
    info "  ISO: ${iso_path}"
    info "  VM: ${vm_name}"
    info "  Resources: ${VCPUS} vCPUs, ${RAM_MB}MB RAM, ${DISK_SIZE} disk"
    info "================================================================="

    if [[ "${DRY_RUN}" == "true" ]]; then
        info "[DRY RUN] Would build ${edition}"
        info "[DRY RUN] Login would be $(guest_username "${edition}") / ${WIN_PASSWORD}"
        return 0
    fi

    if [[ -f "${disk_path}" ]]; then
        info "Golden image already exists: ${disk_path}"
        info "Delete it to rebuild: rm ${disk_path}"
        return 0
    fi

    virsh destroy "${vm_name}" 2>/dev/null || true
    virsh undefine "${vm_name}" --nvram 2>/dev/null || true
    rm -f "${disk_raw}" "${answer_iso}"

    info "Creating autounattend ISO..."
    make_answer_iso "${edition}" "${answer_path}" "${answer_iso}"

    info "Creating ${DISK_SIZE} thin-provisioned QCOW2 disk via libvirt pool..."
    virsh vol-delete --pool golden-images "${edition}-raw.qcow2" 2>/dev/null || true
    virsh vol-create-as golden-images "${edition}-raw.qcow2" "${DISK_SIZE}" \
        --format qcow2 --prealloc-metadata 2>/dev/null \
        || virsh vol-create-as golden-images "${edition}-raw.qcow2" "${DISK_SIZE}" \
            --format qcow2
    virsh pool-refresh golden-images >/dev/null 2>&1 || true

    info "Starting unattended Windows install (typically 30-60 minutes)..."
    # Disk is SATA so WindowsPE needs no VirtIO driver. virtio-win CD stays
    # attached for the interactive guest-tools step after setup.
    # shellcheck disable=SC2086
    virt-install \
        --connect qemu:///system \
        --name "${vm_name}" \
        --ram "${RAM_MB}" \
        --vcpus "${VCPUS}" \
        --os-variant "${os_variant}" \
        --disk "path=${disk_raw},bus=sata,format=qcow2" \
        --cdrom "${iso_path}" \
        --disk "path=${VIRTIO_WIN_ISO},device=cdrom,readonly=on" \
        --disk "path=${answer_iso},device=cdrom,readonly=on" \
        ${NETWORK_ARG} \
        --graphics vnc,listen=127.0.0.1 \
        --boot uefi \
        --noautoconsole
    info "virt-install started VM '${vm_name}'"

    sleep 5
    local _
    for _ in 1 2 3; do
        virsh send-key "${vm_name}" KEY_ENTER 2>/dev/null || true
        sleep 2
    done
    info "Sent boot keystrokes to bypass 'Press any key' CD prompt"

    if ! wait_unattended_install "${vm_name}"; then
        virsh destroy "${vm_name}" 2>/dev/null || true
        virsh undefine "${vm_name}" --nvram 2>/dev/null || true
        return 1
    fi

    info "Starting ${vm_name} for guest-tools install (virtio-win CD attached)"
    virsh start "${vm_name}"
    sleep 8
    prompt_guest_tools "${edition}" "${vm_name}"
    acpi_stop "${vm_name}"

    virsh undefine "${vm_name}" --nvram 2>/dev/null || true
    rm -f "${answer_iso}"

    if ! extract_qcow "${edition}" "${disk_raw}" "${disk_path}"; then
        return 1
    fi
    verify_guest_agent "${disk_path}" "${edition}" || true
}

info "================================================================="
info "  Windows local golden image builder"
info "================================================================="
info "Editions:         ${build_editions[*]}"
info "Resources per VM: ${VCPUS} vCPUs, ${RAM_MB}MB RAM, ${DISK_SIZE} disk"
info "Install timeout:  ${INSTALL_TIMEOUT}s"
info "Output directory: ${GOLDEN_DIR}"

if [[ "${DRY_RUN}" == "true" ]]; then
    info "*** DRY RUN MODE — no changes will be made ***"
fi

echo ""
preflight
download_virtio_win
ensure_libvirt_pool

SUCCESSES=0
FAILURES=0
for edition in "${build_editions[@]}"; do
    echo ""
    if build_edition "${edition}"; then
        SUCCESSES=$(( SUCCESSES + 1 ))
    else
        FAILURES=$(( FAILURES + 1 ))
        error "Failed to build ${edition} — continuing with next edition"
    fi
done

echo ""
info "================================================================="
info "  Build Summary"
info "================================================================="
info "  Succeeded: ${SUCCESSES}/${#build_editions[@]}"
if (( FAILURES > 0 )); then
    info "  Failed:    ${FAILURES}/${#build_editions[@]}"
fi

echo ""
if [[ "${DRY_RUN}" != "true" ]]; then
    info "Golden images:"
    for edition in "${build_editions[@]}"; do
        img_path="${GOLDEN_DIR}/${edition}.qcow2"
        if [[ -f "${img_path}" ]]; then
            size=$(du -h "${img_path}" | cut -f1)
            sha=$(sha256sum "${img_path}" | cut -d' ' -f1)
            printf "  %-20s  size=%-8s  sha256=%s\n" "${edition}" "${size}" "${sha}"
        else
            printf "  %-20s  (not built)\n" "${edition}"
        fi
    done
fi

echo ""
if (( FAILURES > 0 )); then
    error "Some builds failed. Re-run with --only <editions> to retry."
    exit 1
fi

info "All builds completed successfully."
info "Next: ./upload-golden-images.sh"
