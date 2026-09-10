#!/usr/bin/env bash
#
# Create a synthetic RHEL 7/8-style ifcfg disk image fixture.
#
# Produces a 200 MB ext4 disk with a pre-populated ifcfg-eth0 network
# configuration file. Used by the ip-rewrite integration tests to verify
# that rhel-handler.sh rewrites ifcfg IP config correctly.
#
# Usage: create-rhel-ifcfg-fixture.sh [OUTPUT_PATH] [--force]
#
# The script is idempotent: it skips creation if the output file already
# exists, unless --force is passed.

set -euo pipefail

FORCE=false
OUTPUT=""

for arg in "$@"; do
    if [[ "${arg}" == "--force" ]]; then
        FORCE=true
    elif [[ -z "${OUTPUT}" ]]; then
        OUTPUT="${arg}"
    fi
done

OUTPUT="${OUTPUT:-/tmp/rhel-ifcfg-fixture.img}"

if [[ -f "${OUTPUT}" && "${FORCE}" != "true" ]]; then
    echo "Fixture already exists: ${OUTPUT} (use --force to recreate)"
    exit 0
fi

echo "Creating RHEL ifcfg fixture: ${OUTPUT}"

WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' EXIT

cat > "${WORKDIR}/os-release" <<'EOF'
ID=rhel
VERSION_ID=8
NAME=Red Hat Enterprise Linux
EOF
cat > "${WORKDIR}/redhat-release" <<'EOF'
Red Hat Enterprise Linux release 8.9 (Ootpa)
EOF
cat > "${WORKDIR}/ifcfg-eth0" <<'EOF'
TYPE=Ethernet
BOOTPROTO=none
DEVICE=eth0
IPADDR=10.0.1.50
PREFIX=16
GATEWAY=10.0.1.1
DNS1=8.8.8.8
ONBOOT=yes
EOF
printf '%s\n' '# stub' > "${WORKDIR}/fstab"

guestfish -N "${OUTPUT}=fs:ext4:200M" <<GFEOF
mount /dev/sda1 /
mkdir-p /bin
mkdir-p /etc/sysconfig/network-scripts
upload ${WORKDIR}/fstab /etc/fstab
upload ${WORKDIR}/os-release /etc/os-release
upload ${WORKDIR}/redhat-release /etc/redhat-release
upload ${WORKDIR}/ifcfg-eth0 /etc/sysconfig/network-scripts/ifcfg-eth0
GFEOF

echo "RHEL ifcfg fixture created: ${OUTPUT}"
