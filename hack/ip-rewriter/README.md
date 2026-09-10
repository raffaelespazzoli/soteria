# IP rewrite test images

Build Windows goldens locally, upload them to OpenShift, provision test VMs on
the L2 cUDN, then exercise the IP rewrite webhook.

```
windows-images/*.iso
        │
        ▼
build-windows-images-locally.sh     # libvirt, serial, virtio-win CD + guest tools
        │  golden-images/*.qcow2
        ▼
upload-golden-images.sh             # virtctl upload + optional RHEL 7
        │  DataSources in openshift-virtualization-os-images
        ▼
provision-test-vms.sh               # cUDN, virt SC, RWM Block PVCs, static IPs
        ▼
ip-rewrite-test.sh                  # one VM at a time, guest-agent IP check
```

All scripts live in this directory. `--only` takes a comma-separated subset
(`win-11`, `rhel7`, `win-server-2022`, …).

## 1. Build Windows images locally

Put evaluation ISOs in `windows-images/` (gitignored):

| File | Edition |
|---|---|
| `win-server-2016.iso` | Server 2016 |
| `win-server-2019.iso` | Server 2019 |
| `win-server-2022.iso` | Server 2022 |
| `win-server-2025.iso` | Server 2025 |
| `win-11.iso` | Windows 11 |

Needs `virt-install`, `qemu-img`, `virsh`, and `genisoimage` (or `mkisofs` /
`xorrisofs`), plus membership in the `libvirt` group.

```bash
./build-windows-images-locally.sh                 # all editions, one at a time
./build-windows-images-locally.sh --only win-11
```

Each edition: unattended install from `autounattend/`, then the VM is started
with the virtio-win CD attached. The script prints console and login:

| Edition | User | Password |
|---|---|---|
| Windows 11 | `User` | `Passw0rd!` |
| Server 2016–2025 | `Administrator` | `Passw0rd!` |

Open `virt-viewer --connect qemu:///system golden-<edition>`, install
`virtio-win-guest-tools.exe` from the CD, then press Enter. The script
ACPI-stops the VM and writes `golden-images/<edition>.qcow2`.

`SetupComplete.cmd` turns the firewall off, disables Device Encryption
(BitLocker) so offline hive rewrite can mount NTFS, and shuts down when setup
finishes. Do **not** skip guest tools: the later test waits on the QEMU guest
agent.

Answer files keep BitLocker off on new Win 11 goldens. An already-encrypted
disk must be decrypted inside the guest (`manage-bde -off C:` from an elevated
prompt) before ip-rewrite can inspect it.

## 2. Upload to OpenShift

```bash
./upload-golden-images.sh
./upload-golden-images.sh --only win-11
./upload-golden-images.sh --only rhel7 --rhel7-url '<portal-qcow2-url>'
```

Windows qcow2 files are uploaded with `virtctl image-upload` as RWM Block PVCs
on `ocs-storagecluster-ceph-rbd-virtualization` in
`openshift-virtualization-os-images`.

RHEL 7 is not an SSP boot source. Pass `--rhel7-url` (Customer Portal HTTP
import) or place `golden-images/rhel7.qcow2`. `--skip-rhel7` skips it. RHEL
8/9/10 come from OpenShift Virtualization boot sources; `provision-test-vms.sh`
clones those onto the virt StorageClass.

## 3. Provision test VMs

```bash
export KUBECONFIG=/path/to/working/kubeconfig   # do not rely on a stale token
./provision-test-vms.sh
./provision-test-vms.sh --only rhel9,win-11
./provision-test-vms.sh --delete-only
```

Creates namespace `ip-rewrite-test`, cUDN `ip-rewrite-l2` (192.168.100.0/24),
and one VM per OS with a static IP (cloud-init on RHEL, sysprep on Windows).

If `ip-rewrite-test.sh` reports missing credentials, the script may be using
`hack/martin/kubeconfig-dr-poc-1`. Point `KUBECONFIG` at a login that
`oc whoami` accepts.

## 4. Run IP rewrite

Requires the IP rewrite webhook chart deployed. VMs must already exist.

```bash
./ip-rewrite-test.sh
./ip-rewrite-test.sh --only win-11
```

One guest at a time. Label/annotate, start, wait for the `ip-rewrite` init
container, then wait for the guest agent (several minutes on Windows).

| VM | Initial IP | Rewrite IP |
|---|---|---|
| rhel7–10 | .50–.53 | .70–.73 |
| win-server-2016–2025 | .80–.83 | .90–.93 |
| win-11 | .84 | .94 |

## Layout

| Path | Role |
|---|---|
| `autounattend/` | Unattended Windows setup (BitLocker off on Win 11) |
| `SetupComplete.cmd` | Firewall + BitLocker-off + setup-complete shutdown |
| `windows-images/` | Input ISOs (gitignored) |
| `golden-images/` | Output qcow2 (gitignored) |
| `screenshots/` | Local captures (gitignored) |
