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
# Required tools: guestfish, python3, hivexregedit

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
# Step 1: Create a minimal valid SYSTEM registry hive.
#
# hivexsh cannot create a hive from scratch (it only opens existing files).
# We use python3 to write the minimal binary structure: a 4 KB regf header
# page followed by a 4 KB hbin block containing an allocated root key cell.
# hivexregedit --merge can then add keys to this valid hive.
# ---------------------------------------------------------------------------

python3 -c '
import struct, sys

PAGESZ = 4096

# --- regf header (first 4096 bytes) ---
hdr = bytearray(PAGESZ)
hdr[0:4] = b"regf"                              # signature
struct.pack_into("<II", hdr, 4, 1, 1)            # primary + secondary sequence
struct.pack_into("<II", hdr, 20, 1, 5)           # major=1, minor=5
struct.pack_into("<I", hdr, 36, 32)              # root cell offset in hbin data
struct.pack_into("<I", hdr, 40, PAGESZ)          # total hive bins data size

# checksum = sum of first 127 uint32s, truncated to 32 bits
cksum = 0
for i in range(0, 508, 4):
    cksum = (cksum + struct.unpack_from("<I", hdr, i)[0]) & 0xFFFFFFFF
struct.pack_into("<I", hdr, 508, cksum)

# --- hbin block (next 4096 bytes) ---
hbin = bytearray(PAGESZ)
hbin[0:4] = b"hbin"                              # signature
struct.pack_into("<I", hbin, 4, 0)               # offset from start of hive bins
struct.pack_into("<I", hbin, 8, PAGESZ)          # size of this hbin

# Root key cell at offset 32 within hbin
co = 32
cell_size = 80
struct.pack_into("<i", hbin, co, -cell_size)     # negative = allocated
hbin[co+4:co+6] = b"nk"                          # node key signature
struct.pack_into("<H", hbin, co + 6, 0x20)       # KEY_HIVE_ENTRY flag

# Free cell fills the rest of the hbin (offset 32+80=112 to end at 4096)
free_off = co + cell_size
free_size = PAGESZ - 32 - free_off               # 32 = hbin header size
# Actually, free cell goes from (hbin_header=32 + root_cell=80) to end
free_start = 32 + co + cell_size                  # absolute offset in hbin data
# Simpler: remaining bytes after our root cell in the hbin data area
data_start = 32                                   # hbin header is 32 bytes
remaining = PAGESZ - data_start - cell_size       # free space
free_cell_off = data_start + cell_size
struct.pack_into("<i", hbin, free_cell_off, remaining)  # positive = free

sys.stdout.buffer.write(hdr + hbin)
' > "${HIVE}"

echo "Minimal SYSTEM hive created (python3)"

# Add the key tree via hivexregedit --merge
cat > "${REG_FILE}" <<'REGEOF'
Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\SYSTEM\Select]
"Current"=dword:00000001

REGEOF

hivexregedit --merge --prefix 'HKEY_LOCAL_MACHINE\SYSTEM' "${HIVE}" "${REG_FILE}"

# Add the TCP/IP interface subtree
# Build UTF-16LE REG_MULTI_SZ hex for each value.
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

cat > "${REG_FILE}" <<REGEOF
Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\\SYSTEM\\ControlSet001\\Services\\Tcpip\\Parameters\\Interfaces\\{12345678-1234-1234-1234-123456789abc}]
"EnableDHCP"=dword:00000001
"IPAddress"=hex(7):${IP_HEX}
"SubnetMask"=hex(7):${MASK_HEX}
"DefaultGateway"=hex(7):${GW_HEX}

REGEOF

hivexregedit --merge --prefix 'HKEY_LOCAL_MACHINE\SYSTEM' "${HIVE}" "${REG_FILE}"

echo "Adapter IP values merged into hive"

# ---------------------------------------------------------------------------
# Step 2: Create the NTFS disk image, upload the hive, and add the
# directory structure needed by libguestfs inspect-os.
# ---------------------------------------------------------------------------

guestfish -N "${OUTPUT}=fs:ntfs:200M" <<GFEOF
mkdir-p /Windows/System32/config
upload ${HIVE} /Windows/System32/config/system
touch /Windows/System32/cmd.exe
GFEOF

echo "Windows NTFS fixture created: ${OUTPUT}"
