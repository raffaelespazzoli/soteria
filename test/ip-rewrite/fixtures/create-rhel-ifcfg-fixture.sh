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

OUTPUT="${1:-/tmp/rhel-ifcfg-fixture.img}"
FORCE=false

for arg in "$@"; do
    [[ "${arg}" == "--force" ]] && FORCE=true
done

if [[ -f "${OUTPUT}" && "${FORCE}" != "true" ]]; then
    echo "Fixture already exists: ${OUTPUT} (use --force to recreate)"
    exit 0
fi

echo "Creating RHEL ifcfg fixture: ${OUTPUT}"

guestfish -N "${OUTPUT}=fs:ext4:200M" <<'GFEOF'
mkdir-p /etc/sysconfig/network-scripts

write /etc/sysconfig/network-scripts/ifcfg-eth0 "TYPE=Ethernet
BOOTPROTO=none
DEVICE=eth0
IPADDR=10.0.1.50
PREFIX=24
GATEWAY=10.0.1.1
DNS1=8.8.8.8
ONBOOT=yes
"
GFEOF

echo "RHEL ifcfg fixture created: ${OUTPUT}"
