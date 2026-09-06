#!/usr/bin/env bash
#
# Create a synthetic RHEL 9/10-style NetworkManager keyfile disk image fixture.
#
# Produces a 200 MB ext4 disk with a pre-populated NM keyfile
# (eth0.nmconnection). Used by the ip-rewrite integration tests to verify
# that rhel-handler.sh rewrites NM keyfile IP config correctly.
#
# Usage: create-rhel-nmkeyfile-fixture.sh [OUTPUT_PATH] [--force]
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

OUTPUT="${OUTPUT:-/tmp/rhel-nmkeyfile-fixture.img}"

if [[ -f "${OUTPUT}" && "${FORCE}" != "true" ]]; then
    echo "Fixture already exists: ${OUTPUT} (use --force to recreate)"
    exit 0
fi

echo "Creating RHEL NM keyfile fixture: ${OUTPUT}"

guestfish -N "${OUTPUT}=fs:ext4:200M" <<'GFEOF'
mkdir-p /bin
mkdir-p /etc/NetworkManager/system-connections

write /etc/fstab "# stub\n"
write /etc/os-release "ID=rhel\nVERSION_ID=9\nNAME=\"Red Hat Enterprise Linux\"\n"
write /etc/redhat-release "Red Hat Enterprise Linux release 9.4 (Plow)\n"

write /etc/NetworkManager/system-connections/eth0.nmconnection "[connection]
id=eth0
uuid=12345678-1234-1234-1234-123456789abc
type=802-3-ethernet
interface-name=eth0

[ipv4]
method=auto
address1=10.0.1.50/16,10.0.1.1
dns=8.8.8.8;

[ipv6]
method=disabled
"

chmod 0600 /etc/NetworkManager/system-connections/eth0.nmconnection
GFEOF

echo "RHEL NM keyfile fixture created: ${OUTPUT}"
