#!/usr/bin/env bash
#
# Create a synthetic Windows NTFS disk image fixture with SYSTEM registry hive.
#
# Produces a 200 MB NTFS disk with a SYSTEM hive containing a TCP/IP adapter.
# Used by the ip-rewrite integration tests to verify windows-handler.sh.
#
# Usage: create-windows-fixture.sh [OUTPUT_PATH] [--force]
#
# Required tools: guestfish
#
# The base hive is hivex's images/minimal (libguestfs/hivex), vendored as
# minimal-system.hive — hivex cannot create a hive from scratch.

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

OUTPUT="${OUTPUT:-/tmp/windows-fixture.img}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MINIMAL_HIVE="${SCRIPT_DIR}/minimal-system.hive"

if [[ ! -f "${MINIMAL_HIVE}" ]]; then
    echo "Missing ${MINIMAL_HIVE}" >&2
    exit 1
fi

if [[ -f "${OUTPUT}" && "${FORCE}" != "true" ]]; then
    echo "Fixture already exists: ${OUTPUT} (use --force to recreate)"
    exit 0
fi

echo "Creating Windows NTFS fixture: ${OUTPUT}"

export HOME=/tmp XDG_RUNTIME_DIR=/tmp TMPDIR=/tmp
export LIBGUESTFS_BACKEND="${LIBGUESTFS_BACKEND:-direct}"

HELPER="$(cd "${SCRIPT_DIR}/../../../build/ip-rewrite/scripts" && pwd)/virt-windows-ip-rewrite"
if [[ ! -x "${HELPER}" ]]; then
    echo "Windows hive helper not found or not executable: ${HELPER}" >&2
    exit 1
fi

guestfish -N "${OUTPUT}=fs:ntfs:200M" <<GFEOF
mount /dev/sda1 /
mkdir-p /Windows/System32/config
upload ${MINIMAL_HIVE} /Windows/System32/config/system
touch /Windows/System32/cmd.exe
GFEOF

"${HELPER}" --populate-fixture "${OUTPUT}"

echo "Windows NTFS fixture created: ${OUTPUT}"
