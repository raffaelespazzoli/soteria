#!/usr/bin/env bash
#
# provision-test-vms.sh
#
# Step 1.c of the ip-rewrite golden-image pipeline (after upload-golden-images.sh).
# Next: ./ip-rewrite-test.sh
#
# Provisions ip-rewrite test VMs from golden-image DataSources:
#   - RHEL 7/8/9/10: cloud-init network-config (v1 on RHEL 7, v2 on 8+)
#   - Windows:       KubeVirt sysprep volume (unattend.xml) + guest sysprep
#
# Disks: ODF virtualization StorageClass, ReadWriteMany, Block.
# Golden images: RHEL boot sources from SSP stay on gp3-csi (RWO). This
# script clones them to dedicated DataSources named <os>-odf-virt on
# ocs-storagecluster-ceph-rbd-virtualization (RWX/Block) so VM clones are
# fast. Windows goldens are already on that class.
# Network: Layer-2 cUDN only (no pod network).
#
# Usage:
#   ./provision-test-vms.sh                  # Create, start, verify IPs
#   ./provision-test-vms.sh --delete-only    # Delete existing test VMs
#   ./provision-test-vms.sh --ensure-goldens # Only migrate golden PVCs to ODF
#   ./provision-test-vms.sh --only rhel7,win-11
#
# Prerequisites:
#   - oc/kubectl logged in
#   - DataSources in openshift-virtualization-os-images
#   - cUDN ip-rewrite-l2 (created if missing)

set -euo pipefail

NAMESPACE="${NAMESPACE:-ip-rewrite-test}"
CUDN_NAME="${CUDN_NAME:-ip-rewrite-l2}"
NS_LABEL="${NS_LABEL:-network=ip-rewrite-cudn}"
GOLDEN_NS="${GOLDEN_NS:-openshift-virtualization-os-images}"
STORAGE_CLASS="${STORAGE_CLASS:-ocs-storagecluster-ceph-rbd-virtualization}"
ACCESS_MODE="${ACCESS_MODE:-ReadWriteMany}"
VOLUME_MODE="${VOLUME_MODE:-Block}"
GATEWAY="${GATEWAY:-192.168.100.1}"
DNS_SERVER="${DNS_SERVER:-8.8.8.8}"
WIN_ADMIN_PASS="${WIN_ADMIN_PASS:-Passw0rd!}"

RHEL_PVC_SIZE="${RHEL_PVC_SIZE:-32Gi}"
WIN_PVC_SIZE="${WIN_PVC_SIZE:-60Gi}"
RHEL_INSTANCE="${RHEL_INSTANCE:-u1.medium}"

DV_TIMEOUT="${DV_TIMEOUT:-1200}"
VM_READY_TIMEOUT="${VM_READY_TIMEOUT:-600}"
GUEST_AGENT_TIMEOUT="${GUEST_AGENT_TIMEOUT:-600}"
IP_TIMEOUT="${IP_TIMEOUT:-900}"
SYSPREP_TIMEOUT="${SYSPREP_TIMEOUT:-1200}"

# name  kind   datasource  preference         instance   nic     ip               mac
declare -a VM_DEFS=(
  "rhel7-iprewrite-test            rhel     rhel7-odf-virt   rhel.7            ${RHEL_INSTANCE}  eth0     192.168.100.50  -"
  "rhel8-iprewrite-test            rhel     rhel8-odf-virt   rhel.8            ${RHEL_INSTANCE}  eth0     192.168.100.51  -"
  "rhel9-iprewrite-test            rhel     rhel9-odf-virt   rhel.9            ${RHEL_INSTANCE}  eth0     192.168.100.52  -"
  "rhel10-iprewrite-test           rhel     rhel10-odf-virt  rhel.10           ${RHEL_INSTANCE}  enp1s0   192.168.100.53  -"
  "win-server-2016-iprewrite-test  windows  win-server-2016  windows.2k16      u1.medium         Ethernet 192.168.100.80  02:00:00:00:01:16"
  "win-server-2019-iprewrite-test  windows  win-server-2019  windows.2k19      u1.medium         Ethernet 192.168.100.81  02:00:00:00:01:19"
  "win-server-2022-iprewrite-test  windows  win-server-2022  windows.2k22      u1.medium         Ethernet 192.168.100.82  02:00:00:00:01:22"
  "win-server-2025-iprewrite-test  windows  win-server-2025  windows.2k25      u1.medium         Ethernet 192.168.100.83  02:00:00:00:01:25"
  "win-11-iprewrite-test           windows  win-11           windows.11        u1.large          Ethernet 192.168.100.84  02:00:00:00:01:11"
)

info()  { echo "[INFO]  $(date '+%H:%M:%S') $*"; }
warn()  { echo "[WARN]  $(date '+%H:%M:%S') $*" >&2; }
error() { echo "[ERROR] $(date '+%H:%M:%S') $*" >&2; }
fatal() { error "$@"; exit 1; }

ONLY=()
DELETE_ONLY=false
SKIP_START=false
ENSURE_GOLDENS_ONLY=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --only) IFS=',' read -ra ONLY <<< "$2"; shift 2 ;;
    --delete-only) DELETE_ONLY=true; shift ;;
    --skip-start) SKIP_START=true; shift ;;
    --ensure-goldens) ENSURE_GOLDENS_ONLY=true; shift ;;
    --namespace) NAMESPACE="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,24p' "$0"
      exit 0
      ;;
    *) fatal "Unknown argument: $1" ;;
  esac
done

should_include() {
  local name="$1"
  if [[ ${#ONLY[@]} -eq 0 ]]; then
    return 0
  fi
  local token
  for token in "${ONLY[@]}"; do
    if [[ "${name}" == *"${token}"* ]]; then
      return 0
    fi
  done
  return 1
}

parse_def() {
  # shellcheck disable=SC2086
  read -r VM_NAME KIND DATASOURCE PREFERENCE INSTANCE NIC IP MAC <<< $1
}

# ---------------------------------------------------------------------------
# Cluster helpers
# ---------------------------------------------------------------------------
ensure_cudn() {
  if kubectl get clusteruserdefinednetwork "${CUDN_NAME}" >/dev/null 2>&1; then
    info "cUDN '${CUDN_NAME}' already exists"
    return 0
  fi
  info "Creating cUDN '${CUDN_NAME}'"
  kubectl apply -f - <<EOF
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: ${CUDN_NAME}
spec:
  namespaceSelector:
    matchLabels:
      ${NS_LABEL%%=*}: ${NS_LABEL#*=}
  network:
    topology: Layer2
    layer2:
      role: Secondary
      ipam:
        mode: Disabled
EOF
}

ensure_namespace() {
  if ! kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
    kubectl create namespace "${NAMESPACE}"
  fi
  kubectl label namespace "${NAMESPACE}" ${NS_LABEL} --overwrite >/dev/null
  local elapsed=0
  while (( elapsed < 30 )); do
    if kubectl get net-attach-def "${CUDN_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  warn "NAD '${CUDN_NAME}' not yet visible in ${NAMESPACE}"
}

delete_test_vms() {
  info "Deleting existing test VMs in ${NAMESPACE}"
  local def vm
  for def in "${VM_DEFS[@]}"; do
    parse_def "${def}"
    should_include "${VM_NAME}" || continue
    kubectl delete vm "${VM_NAME}" -n "${NAMESPACE}" --wait=false --ignore-not-found >/dev/null 2>&1 || true
  done

  local remaining=1 elapsed=0
  while (( remaining > 0 && elapsed < 180 )); do
    remaining=0
    for def in "${VM_DEFS[@]}"; do
      parse_def "${def}"
      should_include "${VM_NAME}" || continue
      if kubectl get vm "${VM_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
        remaining=$((remaining + 1))
      fi
    done
    if (( remaining > 0 )); then
      sleep 5
      elapsed=$((elapsed + 5))
    fi
  done

  for def in "${VM_DEFS[@]}"; do
    parse_def "${def}"
    should_include "${VM_NAME}" || continue
    kubectl delete vm "${VM_NAME}" -n "${NAMESPACE}" --wait=true --timeout=60s --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete dv "${VM_NAME}-rootdisk" -n "${NAMESPACE}" --wait=false --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete pvc "${VM_NAME}-rootdisk" -n "${NAMESPACE}" --wait=false --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete configmap "sysprep-${VM_NAME}" -n "${NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
  done

  sleep 5
  info "Existing test VMs removed"
}

wait_dv() {
  local dv="$1"
  local ns="${2:-${NAMESPACE}}"
  local elapsed=0
  while (( elapsed < DV_TIMEOUT )); do
    local phase
    phase=$(kubectl get dv "${dv}" -n "${ns}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
    if [[ "${phase}" == "Succeeded" ]]; then
      info "  ${ns}/${dv}: clone complete"
      return 0
    fi
    if [[ "${phase}" == "Failed" ]]; then
      error "${ns}/${dv}: clone failed"
      kubectl get dv "${dv}" -n "${ns}" -o jsonpath='{.status.conditions[*].message}' 2>/dev/null; echo
      return 1
    fi
    sleep 10
    elapsed=$((elapsed + 10))
  done
  error "${ns}/${dv}: clone timed out after ${DV_TIMEOUT}s"
  return 1
}

# Clone a golden-image PVC onto the ODF virtualization StorageClass (RWX/Block)
# and retarget its DataSource. Boot-source imports land on gp3-csi (RWO); CSI
# clones from that class are slow. Same-class RWX clones on ODF are fast.
# Point (or create) a DataSource at an ODF golden PVC. Uses a dedicated
# DataSource name so SSP/DataImportCron cannot retarget it back to gp3-csi.
upsert_datasource() {
  local ds="$1" pvc="$2"
  kubectl apply -f - <<EOF
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataSource
metadata:
  name: ${ds}
  namespace: ${GOLDEN_NS}
  labels:
    app: ip-rewrite-test
spec:
  source:
    pvc:
      name: ${pvc}
      namespace: ${GOLDEN_NS}
EOF
  info "  DataSource ${ds} → PVC ${pvc}"
}

ensure_one_golden_on_odf() {
  local ds="$1" kind="$2"
  local src_ds src_pvc sc access mode dest size dest_sc dest_phase
  GOLDEN_CLONE_DV=""
  GOLDEN_CLONE_DS=""
  GOLDEN_CLONE_DEST=""

  dest="${ds}"
  if [[ "${ds}" != *-odf-virt ]]; then
    dest="${ds}-odf-virt"
  fi
  src_ds="${ds%-odf-virt}"

  if [[ "${kind}" == "windows" ]]; then
    size="${WIN_PVC_SIZE}"
  else
    size="${RHEL_PVC_SIZE}"
  fi

  # Already have an ODF DataSource?
  src_pvc=$(kubectl get datasource "${ds}" -n "${GOLDEN_NS}" \
    -o jsonpath='{.spec.source.pvc.name}' 2>/dev/null || echo "")
  if [[ -n "${src_pvc}" ]]; then
    sc=$(kubectl get pvc "${src_pvc}" -n "${GOLDEN_NS}" \
      -o jsonpath='{.spec.storageClassName}' 2>/dev/null || echo "")
    access=$(kubectl get pvc "${src_pvc}" -n "${GOLDEN_NS}" \
      -o jsonpath='{.spec.accessModes[*]}' 2>/dev/null || echo "")
    mode=$(kubectl get pvc "${src_pvc}" -n "${GOLDEN_NS}" \
      -o jsonpath='{.spec.volumeMode}' 2>/dev/null || echo "")
    if [[ "${sc}" == "${STORAGE_CLASS}" && "${access}" == *"${ACCESS_MODE}"* ]]; then
      info "  ${ds}: ${src_pvc} already on ${STORAGE_CLASS} (${access}, ${mode:-Filesystem})"
      return 0
    fi
  fi

  if kubectl get pvc "${dest}" -n "${GOLDEN_NS}" >/dev/null 2>&1; then
    dest_sc=$(kubectl get pvc "${dest}" -n "${GOLDEN_NS}" -o jsonpath='{.spec.storageClassName}')
    dest_phase=$(kubectl get pvc "${dest}" -n "${GOLDEN_NS}" -o jsonpath='{.status.phase}')
    if [[ "${dest_sc}" == "${STORAGE_CLASS}" && "${dest_phase}" == "Bound" ]]; then
      info "  ${dest} already Bound on ${STORAGE_CLASS} — ensuring DataSource"
      upsert_datasource "${dest}" "${dest}"
      return 0
    fi
  fi

  src_pvc=$(kubectl get datasource "${src_ds}" -n "${GOLDEN_NS}" \
    -o jsonpath='{.spec.source.pvc.name}' 2>/dev/null || echo "")
  [[ -n "${src_pvc}" ]] || fatal "DataSource ${GOLDEN_NS}/${src_ds} has no PVC source to clone from"

  sc=$(kubectl get pvc "${src_pvc}" -n "${GOLDEN_NS}" -o jsonpath='{.spec.storageClassName}')
  access=$(kubectl get pvc "${src_pvc}" -n "${GOLDEN_NS}" -o jsonpath='{.spec.accessModes[*]}')
  mode=$(kubectl get pvc "${src_pvc}" -n "${GOLDEN_NS}" -o jsonpath='{.spec.volumeMode}')
  info "  ${ds}: cloning ${src_pvc} (${sc}/${access}/${mode}) → ${dest} (${STORAGE_CLASS}, ${ACCESS_MODE}, ${VOLUME_MODE}, ${size})"

  kubectl delete dv "${dest}" -n "${GOLDEN_NS}" --ignore-not-found --wait=false >/dev/null 2>&1 || true

  kubectl apply -f - <<EOF
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: ${dest}
  namespace: ${GOLDEN_NS}
  annotations:
    cdi.kubevirt.io/storage.bind.immediate.requested: "true"
  labels:
    app: ip-rewrite-test
    golden-image: "${src_ds}"
spec:
  source:
    pvc:
      name: ${src_pvc}
      namespace: ${GOLDEN_NS}
  storage:
    accessModes:
    - ${ACCESS_MODE}
    volumeMode: ${VOLUME_MODE}
    storageClassName: ${STORAGE_CLASS}
    resources:
      requests:
        storage: ${size}
EOF
  GOLDEN_CLONE_DV="${dest}"
  GOLDEN_CLONE_DS="${dest}"
  GOLDEN_CLONE_DEST="${dest}"
}

ensure_golden_pvcs_on_odf() {
  info "Ensuring golden-image PVCs are on ${STORAGE_CLASS} (${ACCESS_MODE}, ${VOLUME_MODE})"
  local seen=" "
  local pending_dvs=()
  local pending_ds=()
  local pending_dest=()

  for def in "${VM_DEFS[@]}"; do
    parse_def "${def}"
    should_include "${VM_NAME}" || continue
    if [[ "${seen}" == *" ${DATASOURCE} "* ]]; then
      continue
    fi
    seen="${seen}${DATASOURCE} "
    ensure_one_golden_on_odf "${DATASOURCE}" "${KIND}"
    if [[ -n "${GOLDEN_CLONE_DV}" ]]; then
      pending_dvs+=("${GOLDEN_CLONE_DV}")
      pending_ds+=("${GOLDEN_CLONE_DS}")
      pending_dest+=("${GOLDEN_CLONE_DEST}")
    fi
  done

  local i
  for i in "${!pending_dvs[@]}"; do
    wait_dv "${pending_dvs[$i]}" "${GOLDEN_NS}"
    upsert_datasource "${pending_ds[$i]}" "${pending_dest[$i]}"
  done
}

wait_vm_running() {
  local vm="$1"
  local elapsed=0
  while (( elapsed < VM_READY_TIMEOUT )); do
    local status
    status=$(kubectl get vm "${vm}" -n "${NAMESPACE}" -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")
    if [[ "${status}" == "Running" ]]; then
      return 0
    fi
    if [[ "${status}" == "ErrorUnschedulable" || "${status}" == "CrashLoopBackOff" ]]; then
      error "${vm} in bad state: ${status}"
      return 1
    fi
    sleep 10
    elapsed=$((elapsed + 10))
  done
  error "${vm} did not reach Running (last: $(kubectl get vm "${vm}" -n "${NAMESPACE}" -o jsonpath='{.status.printableStatus}' 2>/dev/null))"
  return 1
}

guest_ip() {
  local vm="$1"
  kubectl get vmi "${vm}" -n "${NAMESPACE}" -o jsonpath='{.status.interfaces[0].ipAddress}' 2>/dev/null || true
}

wait_guest_ip() {
  local vm="$1" expected="$2" timeout="$3"
  local elapsed=0
  while (( elapsed < timeout )); do
    local ip
    ip=$(guest_ip "${vm}")
    if [[ -n "${ip}" && "${ip}" != 169.254.* ]]; then
      if [[ -z "${expected}" || "${ip}" == "${expected}" ]]; then
        echo "${ip}"
        return 0
      fi
      # IP present but not yet the expected one — keep waiting
      if [[ -n "${expected}" ]]; then
        :
      else
        echo "${ip}"
        return 0
      fi
    fi
    sleep 10
    elapsed=$((elapsed + 10))
  done
  guest_ip "${vm}"
  return 1
}

win_launcher() {
  local vm="$1"
  kubectl get pod -n "${NAMESPACE}" -l "vm.kubevirt.io/name=${vm}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true
}

win_guest_exec() {
  local vm="$1"
  local cmd="$2"
  local launcher domain result pid status payload
  launcher=$(win_launcher "${vm}")
  [[ -n "${launcher}" ]] || return 1
  domain="${NAMESPACE}_${vm}"

  payload=$(python3 -c "import json,sys; print(json.dumps({'execute':'guest-exec','arguments':{'path':'cmd.exe','arg':['/c', sys.argv[1]],'capture-output':True}}))" "${cmd}")

  result=$(kubectl exec -n "${NAMESPACE}" "${launcher}" -c compute -- \
    virsh -c qemu:///session qemu-agent-command "${domain}" --timeout 60 \
    "${payload}" 2>/dev/null || true)

  pid=$(python3 -c "import sys,json; print(json.load(sys.stdin)['return']['pid'])" <<<"${result}" 2>/dev/null || true)
  [[ -n "${pid}" ]] || return 1

  sleep 8
  status=$(kubectl exec -n "${NAMESPACE}" "${launcher}" -c compute -- \
    virsh -c qemu:///session qemu-agent-command "${domain}" --timeout 30 \
    "{\"execute\":\"guest-exec-status\",\"arguments\":{\"pid\":${pid}}}" 2>/dev/null || true)

  python3 - <<'PY' <<<"${status}" 2>/dev/null || true
import sys, json, base64
raw = sys.stdin.read().strip()
if not raw:
    sys.exit(0)
try:
    data = json.loads(raw)
except Exception:
    sys.exit(0)
ret = data.get("return", {})
out = ret.get("out-data", "")
if out:
    decoded = base64.b64decode(out)
    for enc in ("utf-16-le", "utf-8"):
        try:
            print(decoded.decode(enc).strip())
            break
        except Exception:
            continue
PY
}

# ---------------------------------------------------------------------------
# Cloud-init (RHEL)
# ---------------------------------------------------------------------------
rhel_network_data() {
  local nic="$1" ip="$2" major="$3"
  if [[ "${major}" == "7" ]]; then
    cat <<EOF
version: 1
config:
  - type: physical
    name: ${nic}
    subnets:
      - type: static
        address: ${ip}/24
        gateway: ${GATEWAY}
        dns_nameservers:
          - ${DNS_SERVER}
EOF
  else
    cat <<EOF
version: 2
ethernets:
  ${nic}:
    dhcp4: false
    addresses:
      - ${ip}/24
    gateway4: ${GATEWAY}
    nameservers:
      addresses:
        - ${DNS_SERVER}
EOF
  fi
}

create_rhel_vm() {
  local major="${DATASOURCE#rhel}"
  local netdata userdata
  netdata=$(rhel_network_data "${NIC}" "${IP}" "${major}")
  userdata=$(cat <<EOF
#cloud-config
user: cloud-user
password: redhat123
chpasswd:
  expire: false
ssh_pwauth: true
runcmd:
  - systemctl enable --now qemu-guest-agent || true
EOF
)

  info "Creating RHEL VM ${VM_NAME} (${DATASOURCE}, ${NIC}=${IP}/24, cloud-init v$([ "${major}" = "7" ] && echo 1 || echo 2))"

  kubectl apply -f - <<EOF
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: ${VM_NAME}
  namespace: ${NAMESPACE}
  labels:
    app: ip-rewrite-test
    os-family: rhel
spec:
  runStrategy: Halted
  instancetype:
    name: ${INSTANCE}
  preference:
    name: ${PREFERENCE}
  dataVolumeTemplates:
  - apiVersion: cdi.kubevirt.io/v1beta1
    kind: DataVolume
    metadata:
      name: ${VM_NAME}-rootdisk
      annotations:
        cdi.kubevirt.io/storage.bind.immediate.requested: "true"
    spec:
      sourceRef:
        kind: DataSource
        name: ${DATASOURCE}
        namespace: ${GOLDEN_NS}
      storage:
        accessModes:
        - ${ACCESS_MODE}
        volumeMode: ${VOLUME_MODE}
        storageClassName: ${STORAGE_CLASS}
        resources:
          requests:
            storage: ${RHEL_PVC_SIZE}
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: ${CUDN_NAME}
      labels:
        app: ip-rewrite-test
        os-family: rhel
    spec:
      domain:
        devices:
          interfaces:
          - name: cudn-l2
            bridge: {}
      networks:
      - name: cudn-l2
        multus:
          networkName: ${CUDN_NAME}
      volumes:
      - name: rootdisk
        dataVolume:
          name: ${VM_NAME}-rootdisk
      - name: cloudinitdisk
        cloudInitNoCloud:
          userData: |
$(printf '%s\n' "${userdata}" | sed 's/^/            /')
          networkData: |
$(printf '%s\n' "${netdata}" | sed 's/^/            /')
EOF
}

# ---------------------------------------------------------------------------
# Sysprep (Windows)
# ---------------------------------------------------------------------------
mac_unattend_id() {
  echo "$1" | tr '[:lower:]' '[:upper:]' | tr ':' '-'
}

create_windows_sysprep() {
  local mac_id
  mac_id=$(mac_unattend_id "${MAC}")
  local cm="sysprep-${VM_NAME}"

  info "Creating sysprep ConfigMap ${cm} (${IP}/24, identifier ${mac_id})"

  kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${cm}
  namespace: ${NAMESPACE}
  labels:
    app: ip-rewrite-test
data:
  unattend.xml: |
    <?xml version="1.0" encoding="utf-8"?>
    <unattend xmlns="urn:schemas-microsoft-com:unattend">
      <settings pass="specialize">
        <component name="Microsoft-Windows-Security-SPP" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
          <SkipRearm>1</SkipRearm>
        </component>
        <component name="Microsoft-Windows-TCPIP" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
          <Interfaces>
            <Interface wcm:action="add">
              <Identifier>${mac_id}</Identifier>
              <Ipv4Settings>
                <DhcpEnabled>false</DhcpEnabled>
              </Ipv4Settings>
              <UnicastIpAddresses>
                <IpAddress wcm:action="add" wcm:keyValue="1">${IP}/24</IpAddress>
              </UnicastIpAddresses>
              <Routes>
                <Route wcm:action="add">
                  <Identifier>0</Identifier>
                  <Metric>10</Metric>
                  <NextHopAddress>${GATEWAY}</NextHopAddress>
                  <Prefix>0.0.0.0/0</Prefix>
                </Route>
              </Routes>
            </Interface>
          </Interfaces>
        </component>
        <component name="Microsoft-Windows-DNS-Client" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
          <Interfaces>
            <Interface wcm:action="add">
              <Identifier>${mac_id}</Identifier>
              <DNSServerSearchOrder>
                <IpAddress wcm:action="add" wcm:keyValue="1">${DNS_SERVER}</IpAddress>
              </DNSServerSearchOrder>
            </Interface>
          </Interfaces>
        </component>
        <component name="Microsoft-Windows-Deployment" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
          <RunSynchronous>
            <RunSynchronousCommand wcm:action="add">
              <Order>1</Order>
              <Path>reg add HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\OOBE /v BypassNRO /t REG_DWORD /d 1 /f</Path>
            </RunSynchronousCommand>
          </RunSynchronous>
        </component>
      </settings>
      <settings pass="oobeSystem">
        <component name="Microsoft-Windows-International-Core" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
          <InputLocale>en-US</InputLocale>
          <SystemLocale>en-US</SystemLocale>
          <UILanguage>en-US</UILanguage>
          <UserLocale>en-US</UserLocale>
        </component>
        <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
          <AutoLogon>
            <Enabled>true</Enabled>
            <LogonCount>2</LogonCount>
            <Username>Administrator</Username>
            <Password>
              <Value>${WIN_ADMIN_PASS}</Value>
              <PlainText>true</PlainText>
            </Password>
          </AutoLogon>
          <UserAccounts>
            <AdministratorPassword>
              <Value>${WIN_ADMIN_PASS}</Value>
              <PlainText>true</PlainText>
            </AdministratorPassword>
          </UserAccounts>
          <OOBE>
            <HideEULAPage>true</HideEULAPage>
            <HideLocalAccountScreen>true</HideLocalAccountScreen>
            <HideOEMRegistrationScreen>true</HideOEMRegistrationScreen>
            <HideOnlineAccountScreens>true</HideOnlineAccountScreens>
            <HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE>
            <NetworkLocation>Work</NetworkLocation>
            <ProtectYourPC>3</ProtectYourPC>
            <SkipMachineOOBE>true</SkipMachineOOBE>
            <SkipUserOOBE>true</SkipUserOOBE>
          </OOBE>
          <FirstLogonCommands>
            <SynchronousCommand wcm:action="add">
              <Order>1</Order>
              <CommandLine>cmd /c netsh interface ipv4 set address name="${NIC}" static ${IP} 255.255.255.0 ${GATEWAY}</CommandLine>
            </SynchronousCommand>
            <SynchronousCommand wcm:action="add">
              <Order>2</Order>
              <CommandLine>cmd /c netsh interface ipv4 set dns name="${NIC}" static ${DNS_SERVER}</CommandLine>
            </SynchronousCommand>
            <SynchronousCommand wcm:action="add">
              <Order>3</Order>
              <CommandLine>cmd /c netsh advfirewall set allprofiles state off</CommandLine>
            </SynchronousCommand>
          </FirstLogonCommands>
        </component>
      </settings>
    </unattend>
EOF
}

create_windows_vm() {
  create_windows_sysprep

  info "Creating Windows VM ${VM_NAME} (${DATASOURCE}, sysprep IP=${IP}/24)"

  kubectl apply -f - <<EOF
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: ${VM_NAME}
  namespace: ${NAMESPACE}
  labels:
    app: ip-rewrite-test
    os-family: windows
spec:
  runStrategy: Halted
  instancetype:
    name: ${INSTANCE}
  preference:
    name: ${PREFERENCE}
  dataVolumeTemplates:
  - apiVersion: cdi.kubevirt.io/v1beta1
    kind: DataVolume
    metadata:
      name: ${VM_NAME}-rootdisk
      annotations:
        cdi.kubevirt.io/storage.bind.immediate.requested: "true"
    spec:
      sourceRef:
        kind: DataSource
        name: ${DATASOURCE}
        namespace: ${GOLDEN_NS}
      storage:
        accessModes:
        - ${ACCESS_MODE}
        volumeMode: ${VOLUME_MODE}
        storageClassName: ${STORAGE_CLASS}
        resources:
          requests:
            storage: ${WIN_PVC_SIZE}
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: ${CUDN_NAME}
      labels:
        app: ip-rewrite-test
        os-family: windows
    spec:
      domain:
        devices:
          disks:
          - name: rootdisk
            disk:
              bus: sata
          - name: sysprep
            cdrom:
              bus: sata
          interfaces:
          - name: cudn-l2
            bridge: {}
            macAddress: "${MAC}"
      networks:
      - name: cudn-l2
        multus:
          networkName: ${CUDN_NAME}
      volumes:
      - name: rootdisk
        dataVolume:
          name: ${VM_NAME}-rootdisk
      - name: sysprep
        sysprep:
          configMap:
            name: sysprep-${VM_NAME}
EOF
}

trigger_windows_sysprep() {
  local vm="$1"
  info "  Triggering sysprep inside ${vm}"
  # Locate unattend.xml on the sysprep CD and generalize. Connection drop is expected.
  win_guest_exec "${vm}" \
    "for %d in (D E F G H) do @if exist %d:\unattend.xml (C:\Windows\System32\Sysprep\sysprep.exe /generalize /oobe /reboot /quiet /mode:vm /unattend:%d:\unattend.xml)" \
    >/dev/null 2>&1 || true
}

fallback_windows_ip() {
  local vm="$1" ip="$2"
  warn "  Sysprep IP not observed on ${vm} — setting via netsh"
  win_guest_exec "${vm}" \
    "netsh interface ipv4 set address name=\"Ethernet\" static ${ip} 255.255.255.0 ${GATEWAY} & netsh interface ipv4 set dns name=\"Ethernet\" static ${DNS_SERVER}" \
    >/dev/null 2>&1 || true
}

start_vm() {
  virtctl start "${1}" -n "${NAMESPACE}" >/dev/null
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
kubectl cluster-info >/dev/null 2>&1 || fatal "Cannot reach cluster"

if [[ "${ENSURE_GOLDENS_ONLY}" == "true" ]]; then
  info "Verifying DataSources in ${GOLDEN_NS}"
  for def in "${VM_DEFS[@]}"; do
    parse_def "${def}"
    should_include "${VM_NAME}" || continue
    kubectl get datasource "${DATASOURCE}" -n "${GOLDEN_NS}" >/dev/null 2>&1 \
      || fatal "DataSource ${GOLDEN_NS}/${DATASOURCE} not found"
  done
  ensure_golden_pvcs_on_odf
  info "Golden-image PVCs are on ${STORAGE_CLASS}"
  exit 0
fi

ensure_cudn
ensure_namespace
delete_test_vms

if [[ "${DELETE_ONLY}" == "true" ]]; then
  info "Delete-only complete"
  exit 0
fi

info "Verifying DataSources in ${GOLDEN_NS}"
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  kubectl get datasource "${DATASOURCE}" -n "${GOLDEN_NS}" >/dev/null 2>&1 \
    || fatal "DataSource ${GOLDEN_NS}/${DATASOURCE} not found"
done
ensure_golden_pvcs_on_odf

info "Creating VMs"
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  if [[ "${KIND}" == "rhel" ]]; then
    create_rhel_vm
  else
    create_windows_vm
  fi
done

info "Waiting for disk clones"
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  wait_dv "${VM_NAME}-rootdisk"
done

if [[ "${SKIP_START}" == "true" ]]; then
  info "VMs created (Halted). Skipping start (--skip-start)."
  exit 0
fi

FAILURES=0
declare -A RESULTS

info "Starting RHEL VMs and verifying cloud-init IPs"
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  [[ "${KIND}" == "rhel" ]] || continue
  info "Starting ${VM_NAME}"
  start_vm "${VM_NAME}"
done

for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  [[ "${KIND}" == "rhel" ]] || continue
  if ! wait_vm_running "${VM_NAME}"; then
    RESULTS["${VM_NAME}"]="FAIL (not running)"
    FAILURES=$((FAILURES + 1))
    continue
  fi
  actual=""
  if actual=$(wait_guest_ip "${VM_NAME}" "${IP}" "${IP_TIMEOUT}"); then
    if [[ "${actual}" == "${IP}" ]]; then
      info "  ${VM_NAME}: ${actual} OK"
      RESULTS["${VM_NAME}"]="PASS ${actual}"
    else
      error "  ${VM_NAME}: expected ${IP} got ${actual}"
      RESULTS["${VM_NAME}"]="FAIL expected=${IP} actual=${actual}"
      FAILURES=$((FAILURES + 1))
    fi
  else
    actual=$(guest_ip "${VM_NAME}")
    error "  ${VM_NAME}: no matching IP (got '${actual:-none}')"
    RESULTS["${VM_NAME}"]="FAIL expected=${IP} actual=${actual:-none}"
    FAILURES=$((FAILURES + 1))
  fi
done

info "Starting Windows VMs one at a time (sysprep + IP verify)"
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  [[ "${KIND}" == "windows" ]] || continue

  info "Starting ${VM_NAME}"
  start_vm "${VM_NAME}"
  if ! wait_vm_running "${VM_NAME}"; then
    RESULTS["${VM_NAME}"]="FAIL (not running)"
    FAILURES=$((FAILURES + 1))
    continue
  fi

  info "  Waiting for guest agent on ${VM_NAME}"
  local_elapsed=0
  agent_ok=false
  while (( local_elapsed < GUEST_AGENT_TIMEOUT )); do
    if kubectl get vmi "${VM_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.interfaces[0].infoSource}' 2>/dev/null | grep -q 'guest-agent'; then
      agent_ok=true
      break
    fi
    sleep 10
    local_elapsed=$((local_elapsed + 10))
  done
  if [[ "${agent_ok}" != "true" ]]; then
    warn "  Guest agent not ready after ${GUEST_AGENT_TIMEOUT}s — continuing anyway"
  fi

  already=$(guest_ip "${VM_NAME}")
  if [[ "${already}" == "${IP}" ]]; then
    info "  ${VM_NAME}: already at ${IP} (sysprep volume applied without extra step)"
    RESULTS["${VM_NAME}"]="PASS ${IP}"
    continue
  fi

  trigger_windows_sysprep "${VM_NAME}"

  info "  Waiting for ${VM_NAME} to come back with ${IP} (sysprep reboot)"
  actual=""
  if actual=$(wait_guest_ip "${VM_NAME}" "${IP}" "${SYSPREP_TIMEOUT}"); then
    info "  ${VM_NAME}: ${actual} OK"
    RESULTS["${VM_NAME}"]="PASS ${actual}"
    continue
  fi

  fallback_windows_ip "${VM_NAME}" "${IP}"
  if actual=$(wait_guest_ip "${VM_NAME}" "${IP}" 120); then
    warn "  ${VM_NAME}: ${actual} via netsh fallback"
    RESULTS["${VM_NAME}"]="PASS ${actual} (netsh fallback)"
  else
    actual=$(guest_ip "${VM_NAME}")
    error "  ${VM_NAME}: expected ${IP} got '${actual:-none}'"
    RESULTS["${VM_NAME}"]="FAIL expected=${IP} actual=${actual:-none}"
    FAILURES=$((FAILURES + 1))
  fi
done

echo ""
info "================================================================="
info "  Provision results"
info "================================================================="
for def in "${VM_DEFS[@]}"; do
  parse_def "${def}"
  should_include "${VM_NAME}" || continue
  printf "  %-40s  %s\n" "${VM_NAME}" "${RESULTS[${VM_NAME}]:-SKIPPED}"
done
info "================================================================="

exit "${FAILURES}"
