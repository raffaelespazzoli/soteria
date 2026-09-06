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

OUTPUT="${1:-/tmp/rhel-nmkeyfile-fixture.img}"
FORCE=false

for arg in "$@"; do
    [[ "${arg}" == "--force" ]] && FORCE=true
done

if [[ -f "${OUTPUT}" && "${FORCE}" != "true" ]]; then
    echo "Fixture already exists: ${OUTPUT} (use --force to recreate)"
    exit 0
fi

echo "Creating RHEL NM keyfile fixture: ${OUTPUT}"

guestfish -N "${OUTPUT}=fs:ext4:200M" <<'GFEOF'
mkdir-p /etc/NetworkManager/system-connections

write /etc/NetworkManager/system-connections/eth0.nmconnection "[connection]
id=eth0
uuid=12345678-1234-1234-1234-123456789abc
type=802-3-ethernet
interface-name=eth0

[ipv4]
method=manual
address1=10.0.1.50/24,10.0.1.1
dns=8.8.8.8;

[ipv6]
method=disabled
"

chmod 0600 /etc/NetworkManager/system-connections/eth0.nmconnection
GFEOF

echo "RHEL NM keyfile fixture created: ${OUTPUT}"
