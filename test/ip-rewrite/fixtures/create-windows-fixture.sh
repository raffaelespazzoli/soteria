#!/usr/bin/env bash
#
# Create a synthetic Windows NTFS disk image fixture with SYSTEM registry hive.
#
# Produces a 200 MB NTFS disk with a minimal SYSTEM hive containing a
# TCP/IP adapter with static IP configuration. Used by the ip-rewrite
# integration tests to verify that windows-handler.sh rewrites registry
# IP config correctly.
#
# Usage: create-windows-fixture.sh [OUTPUT_PATH] [--force]
#
# The script is idempotent: it skips creation if the output file already
# exists, unless --force is passed.
#
# Required tools: guestfish, hivexregedit

set -euo pipefail

OUTPUT="${1:-/tmp/windows-fixture.img}"
FORCE=false

for arg in "$@"; do
    [[ "${arg}" == "--force" ]] && FORCE=true
done

if [[ -f "${OUTPUT}" && "${FORCE}" != "true" ]]; then
    echo "Fixture already exists: ${OUTPUT} (use --force to recreate)"
    exit 0
fi

echo "Creating Windows NTFS fixture: ${OUTPUT}"

TMPDIR=$(mktemp -d /tmp/win-fixture-XXXXXX)
trap 'rm -rf "${TMPDIR}"' EXIT

HIVE="${TMPDIR}/system.hive"
REG_FILE="${TMPDIR}/setup.reg"

# ---------------------------------------------------------------------------
# Step 1: Create a minimal SYSTEM hive using hivexregedit.
#
# We first create a blank hive with hivexsh (it can create a new hive with
# the `add` command at the root level), then populate values with
# hivexregedit --merge which correctly handles REG_MULTI_SZ hex encoding.
# ---------------------------------------------------------------------------

# Create minimal hive structure using hivexsh
hivexsh -w "${HIVE}" <<'HIVEOF'
add Select
cd \Select
setval 1
Current
dword:00000001
cd \
add ControlSet001
cd \ControlSet001
add Services
cd \ControlSet001\Services
add Tcpip
cd \ControlSet001\Services\Tcpip
add Parameters
cd \ControlSet001\Services\Tcpip\Parameters
add Interfaces
cd \ControlSet001\Services\Tcpip\Parameters\Interfaces
add {12345678-1234-1234-1234-123456789abc}
commit
HIVEOF

echo "Minimal SYSTEM hive structure created"

# Build UTF-16LE REG_MULTI_SZ hex for each value.
# Format: hex(7):<utf16le bytes comma-separated>,00,00,00,00
string_to_utf16le_multisz() {
    local str="$1"
    local hex=""
    local i
    for (( i = 0; i < ${#str}; i++ )); do
        hex+=$(printf '%02x,00,' "'${str:$i:1}")
    done
    hex+="00,00,00,00"
    echo "${hex}"
}

IP_HEX=$(string_to_utf16le_multisz "10.0.1.50")
MASK_HEX=$(string_to_utf16le_multisz "255.255.255.0")
GW_HEX=$(string_to_utf16le_multisz "10.0.1.1")

# Write the .reg file with adapter values
cat > "${REG_FILE}" <<REGEOF
Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\\SYSTEM\\ControlSet001\\Services\\Tcpip\\Parameters\\Interfaces\\{12345678-1234-1234-1234-123456789abc}]
"EnableDHCP"=dword:00000000
"IPAddress"=hex(7):${IP_HEX}
"SubnetMask"=hex(7):${MASK_HEX}
"DefaultGateway"=hex(7):${GW_HEX}

REGEOF

# Merge values into the hive
hivexregedit --merge --prefix 'HKEY_LOCAL_MACHINE\SYSTEM' "${HIVE}" "${REG_FILE}"

echo "Adapter IP values merged into hive"

# ---------------------------------------------------------------------------
# Step 2: Create the NTFS disk image and upload the hive.
# ---------------------------------------------------------------------------

guestfish -N "${OUTPUT}=fs:ntfs:200M" <<GFEOF
mkdir-p /Windows/System32/config
upload ${HIVE} /Windows/System32/config/system
GFEOF

echo "Windows NTFS fixture created: ${OUTPUT}"
