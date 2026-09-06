# Story 18.7: Unit & Integration Tests — Disk Image Fixtures

Status: ready-for-dev

## Story

As a developer,
I want unit tests for the webhook handler and integration tests that verify IP rewriting on synthetic RHEL and Windows disk images,
so that I can confidently refactor and extend the IP rewrite feature.

## Acceptance Criteria

### AC1: Webhook handler unit tests
Given the webhook handler in `internal/webhook/iprewrite/handler.go`
When unit tests run
Then the following scenarios are covered:
- Pod with `soteria.io/ip-rewrite: "true"` label + IP annotations → init container injected
- Pod with `kubevirt.io/migrationJobLabel` → no injection (migration skip)
- Pod without `soteria.io/ip-rewrite` label → no injection (handler returns allowed, unmodified)
- Pod with label but no `soteria.io/*-ip` annotations → no-op init container injected (graceful)
- Multi-NIC: two `*-ip` annotations → two env vars in init container
- DNS annotation present → `SOTERIA_DNS` env var set
- PVC volumes correctly shared with init container

### AC2: Annotation parsing unit tests
Given the annotation-to-env-var transformation logic
When unit tests run
Then `soteria.io/eth0-ip` → `SOTERIA_ETH0_IP` is correct
And `soteria.io/ens3-ip` → `SOTERIA_ENS3_IP` is correct
And `soteria.io/my-custom-nic-ip` → `SOTERIA_MY_CUSTOM_NIC_IP` is correct
And malformed annotations (missing prefix, no `-ip` suffix) are ignored

### AC3: RHEL integration test with synthetic disk image
Given a synthetic ext4/xfs disk image containing:
- Partition table (GPT)
- ext4 filesystem with `/etc/sysconfig/network-scripts/ifcfg-eth0` (RHEL 7/8 style) pre-populated with IP `10.0.1.50`
- Or xfs filesystem with `/etc/NetworkManager/system-connections/eth0.nmconnection` (RHEL 9/10 style)
When the RHEL handler script runs with target IP `10.0.2.100/24;10.0.2.1`
Then re-reading the disk with guestfish shows the IP is changed to `10.0.2.100`
And gateway is `10.0.2.1`
And prefix is `24`

### AC4: Windows integration test with synthetic NTFS disk image
Given a synthetic NTFS disk image containing:
- Partition table (GPT)
- NTFS filesystem with `Windows/System32/config/SYSTEM` containing a valid registry hive
- The hive has one adapter GUID under `ControlSet001\Services\Tcpip\Parameters\Interfaces\{GUID}` with `IPAddress=10.0.1.50`, `EnableDHCP=0`
When the Windows handler script runs with target IP `10.0.2.100/24;10.0.2.1`
Then re-reading the SYSTEM hive shows `IPAddress` is `10.0.2.100`
And `SubnetMask` is `255.255.255.0`
And `DefaultGateway` is `10.0.2.1`

### AC5: Test fixtures are reproducibly created in CI
Given the integration test suite
When CI runs
Then synthetic disk images are created programmatically (not committed to git as large binaries)
And created using `guestfish` scripting (e.g., `guestfish -N fs:ext4:200M` for RHEL, `guestfish -N fs:ntfs:200M` for Windows)
And the SYSTEM hive fixture is created using hivex CLI tools
And fixture creation scripts are idempotent

### AC6: All tests pass in CI without special hardware
Given the GitHub Actions CI environment
When tests run
Then no nested virtualization or special devices are required
And guestfish uses the `LIBGUESTFS_BACKEND=direct` mode (no KVM appliance, uses user-mode QEMU)

## Tasks / Subtasks

- [ ] Task 1: Write Go unit tests for webhook handler (AC: 1, 2)
  - [ ] 1.1: Create `internal/webhook/iprewrite/handler_test.go` with standard Go testing
  - [ ] 1.2: Test init container injection (label + annotations → init container with correct env vars, volume mounts, security context)
  - [ ] 1.3: Test migration skip (`kubevirt.io/migrationJobLabel` present → no injection)
  - [ ] 1.4: Test no-label case (handler returns allowed, no patches)
  - [ ] 1.5: Test label-only, no IP annotations (graceful no-op init container)
  - [ ] 1.6: Test multi-NIC: two `*-ip` annotations → two `SOTERIA_*_IP` env vars
  - [ ] 1.7: Test DNS annotation → `SOTERIA_DNS` env var
  - [ ] 1.8: Test PVC volume mounts injected into init container at `/disks/<volumeName>`
  - [ ] 1.9: Test non-PVC volumes are NOT mounted into init container
  - [ ] 1.10: Test init container is prepended (first in `InitContainers` list)
- [ ] Task 2: Write annotation parsing unit tests (AC: 2)
  - [ ] 2.1: Table-driven tests for annotation-to-env-var transformation
  - [ ] 2.2: Test `soteria.io/eth0-ip` → `SOTERIA_ETH0_IP`
  - [ ] 2.3: Test `soteria.io/ens3-ip` → `SOTERIA_ENS3_IP`
  - [ ] 2.4: Test `soteria.io/my-custom-nic-ip` → `SOTERIA_MY_CUSTOM_NIC_IP`
  - [ ] 2.5: Test `soteria.io/dns` → `SOTERIA_DNS`
  - [ ] 2.6: Test non-soteria annotations are ignored
  - [ ] 2.7: Test `soteria.io/ip-rewrite` (label, no `-ip` suffix) is ignored
  - [ ] 2.8: Test malformed annotations are skipped gracefully
- [ ] Task 3: Create synthetic RHEL disk fixture scripts (AC: 3, 5)
  - [ ] 3.1: Create `test/ip-rewrite/fixtures/create-rhel-ifcfg-fixture.sh` — RHEL 7/8 ifcfg format
  - [ ] 3.2: Create `test/ip-rewrite/fixtures/create-rhel-nmkeyfile-fixture.sh` — RHEL 9/10 NM keyfile format
  - [ ] 3.3: Each script creates a small disk image (~200MB) with the appropriate config files
  - [ ] 3.4: Each script is idempotent (skips if output file already exists, unless `--force`)
- [ ] Task 4: Create synthetic Windows disk fixture script (AC: 4, 5)
  - [ ] 4.1: Create `test/ip-rewrite/fixtures/create-windows-fixture.sh` — NTFS with SYSTEM hive
  - [ ] 4.2: Build a valid SYSTEM hive with `Select\Current=1`, `ControlSet001\Services\Tcpip\Parameters\Interfaces\{GUID}` containing `EnableDHCP=0`, `IPAddress`, `SubnetMask`, `DefaultGateway`
  - [ ] 4.3: Use `hivexregedit --merge` to populate the hive with adapter values
- [ ] Task 5: Write RHEL integration tests (AC: 3, 6)
  - [ ] 5.1: Create `test/ip-rewrite/integration_test.sh` (or a wrapper script calling fixtures + handlers + verification)
  - [ ] 5.2: Test ifcfg format: run `rhel-handler.sh` on ifcfg fixture → verify IP/prefix/gateway rewritten
  - [ ] 5.3: Test NM keyfile format: run `rhel-handler.sh` on NM keyfile fixture → verify `address1`/`method`/`dns` rewritten
  - [ ] 5.4: Verify idempotency: run handler twice, second run succeeds without errors
  - [ ] 5.5: Verify DNS rewrite when `REWRITE_DNS` is set
- [ ] Task 6: Write Windows integration test (AC: 4, 6)
  - [ ] 6.1: Test registry hive rewrite: run `windows-handler.sh` on NTFS fixture → verify `IPAddress`/`SubnetMask`/`DefaultGateway` rewritten
  - [ ] 6.2: Verify `EnableDHCP` is set to 0
  - [ ] 6.3: Verify DNS rewrite when `REWRITE_DNS` is set
  - [ ] 6.4: Verify idempotency: run handler twice, second run succeeds
- [ ] Task 7: Add integration test target to Makefile (AC: 6)
  - [ ] 7.1: Add `test-ip-rewrite` target that runs fixture creation + integration tests
  - [ ] 7.2: Target sets `LIBGUESTFS_BACKEND=direct` and depends on `guestfish` being available
  - [ ] 7.3: Add `test-ip-rewrite` to CI path filtering for the `test` job

## Dev Notes

### Story Intelligence Chain

This story is the **testing story** for Epic 18's IP rewrite feature. It depends on three predecessor stories that implemented the code being tested.

**Predecessor: Story 18.3 — RHEL IP Rewrite Handler: Augeas-Based**

What 18.3 implements (that this story tests):
- `build/ip-rewrite/scripts/rhel-handler.sh` — full Augeas-based IP rewrite for RHEL 7/8/9/10
- Two-phase guestfish approach: Phase 1 read-only detection (aug-match for interface discovery), Phase 2 read-write rewrite (aug-set commands)
- Config format auto-detection: NM keyfile first (`/etc/NetworkManager/system-connections/*.nmconnection`), ifcfg fallback (`/etc/sysconfig/network-scripts/ifcfg-*`)
- Interface matching by `[connection] interface-name=<iface>` in NM keyfile or `DEVICE=<iface>` in ifcfg (never by filename)
- ifcfg rewrite: `aug-set .../IPADDR`, `.../PREFIX`, `.../GATEWAY`, `.../BOOTPROTO none`, optionally `.../DNS1`, `.../DNS2`
- NM keyfile rewrite: `aug-set .../ipv4/method manual`, `.../ipv4/address1 <ip>/<prefix>,<gateway>`, optionally `.../ipv4/dns <ip1>;<ip2>;`
- Multi-NIC iteration over `REWRITE_IFACE_0..N-1`
- Idempotent operation

Handler interface contract (inputs the handler reads):
```bash
REWRITE_DISK="/disks/rootdisk/disk.img"
REWRITE_IFACE_COUNT="1"
REWRITE_IFACE_0="eth0"
REWRITE_IP_0="10.0.2.100"
REWRITE_PREFIX_0="24"
REWRITE_GATEWAY_0="10.0.2.1"
REWRITE_DNS="10.0.2.10,10.0.2.11"   # Optional
REWRITE_OS_MAJOR="9"                  # Available but handler detects by file presence
```

**Predecessor: Story 18.4 — Windows IP Rewrite Handler: hivex-Based**

What 18.4 implements (that this story tests):
- `build/ip-rewrite/scripts/windows-handler.sh` — hivex-based registry hive rewrite for all Windows versions
- Three-phase approach: Phase 1 (guestfish) download SYSTEM hive + get `windows_systemroot` + `windows_current_control_set`, Phase 2 (hivexsh/hivexregedit) modify hive locally, Phase 3 (guestfish) upload modified hive
- Adapter GUID matching: enumerate `<ControlSet>\Services\Tcpip\Parameters\Interfaces\{GUID}`, match by `EnableDHCP=0` + non-empty `IPAddress`
- Value writing via `hivexregedit --merge` with `.reg` file (safe — updates individual values, preserves others)
- `REG_MULTI_SZ` encoding: UTF-16LE with double null termination via `hex(7):` format
- `prefix_to_mask()` function for CIDR-to-dotted-decimal conversion
- `string_to_utf16le_multisz()` function for ASCII-to-hex encoding
- DNS written as `NameServer` REG_SZ (comma-separated)

Handler interface contract (same as RHEL handler — inputs from entrypoint):
```bash
REWRITE_DISK="/disks/rootdisk/disk.img"
REWRITE_IFACE_COUNT="1"
REWRITE_IFACE_0="eth0"
REWRITE_IP_0="10.0.2.100"
REWRITE_PREFIX_0="24"
REWRITE_GATEWAY_0="10.0.2.1"
REWRITE_DNS="10.0.2.10,10.0.2.11"   # Optional
REWRITE_OS_NAME="windows"
REWRITE_OS_PRODUCT="Windows Server 2022 Standard"
```

**Predecessor: Story 18.5 — Mutating Webhook: virt-launcher Init Container Injection**

What 18.5 implements (that this story tests):
- `internal/webhook/iprewrite/handler.go` — Go mutating admission handler
- Handler struct with `InitContainerImage` field (configurable, default `quay.io/raffaelespazzoli/soteria-ip-rewrite:latest`)
- `Handle(ctx, admission.Request) admission.Response` — core mutation logic
- Annotation-to-env-var transformation: `soteria.io/eth0-ip` → `SOTERIA_ETH0_IP`
- PVC volume mount injection: `pod.Spec.Volumes` with `PersistentVolumeClaim` → mount at `/disks/<volumeName>`
- Migration detection: `kubevirt.io/migrationJobLabel` label → skip injection
- Init container prepended to `pod.Spec.InitContainers`
- `securityContext.capabilities.add: [SYS_ADMIN]`
- JSON patch response via `admission.PatchResponseFromRaw()`

**Predecessor: Story 18.2 — IP Rewrite Entrypoint Script: OS Detection & Dispatch**

What 18.2 provides (context for integration tests):
- `build/ip-rewrite/scripts/entrypoint.sh` — parses `SOTERIA_*_IP` env vars, scans disks with `virt-inspector`, detects OS, dispatches to handler
- Logging helpers: `log_info()`, `log_warn()`, `log_error()` — available to handlers via `source`
- Environment variable convention: `SOTERIA_<IFACE>_IP="<ip>/<prefix>;<gateway>"`

**Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9**

What 18.1 provides (the runtime for integration tests):
- `build/ip-rewrite/Containerfile` — UBI9 with `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport`
- `LIBGUESTFS_BACKEND=direct` baked in
- `SYS_ADMIN` capability required at runtime
- `hivexsh`, `hivexregedit`, `hivexget`, `hivexml` CLIs available
- `guestfish` with built-in `aug-*` and `hivex-*` commands

### Critical Technical Details

#### Two Distinct Test Categories

This story produces two fundamentally different types of tests:

**Category A — Go unit tests** (webhook handler):
- Standard Go `*_test.go` files in `internal/webhook/iprewrite/`
- Use `admission.Request` objects — no envtest, no running cluster needed
- Follow the pattern established in `pkg/admission/vm_validator_test.go`
- Run as part of `make test` (standard `go test`)
- Fast, deterministic, no external dependencies

**Category B — Shell integration tests** (disk image fixtures + handler scripts):
- Bash test scripts in `test/ip-rewrite/`
- Require `guestfish`, `hivexsh`, `hivexregedit` installed on the host (or run inside the ip-rewrite container)
- Create synthetic disk images, run handler scripts, verify results
- Run via `make test-ip-rewrite` (separate target, not part of `make test`)
- Slower (~30-60 seconds per test due to guestfish appliance boot)
- Require `LIBGUESTFS_BACKEND=direct` (set by the Makefile target)

#### Go Unit Test Pattern — Follow Existing vm_validator_test.go

The existing webhook test pattern in `pkg/admission/vm_validator_test.go` provides the exact template:

```go
// Build a Pod, marshal it, construct an admission.Request:
func makePodRequest(pod *corev1.Pod, op admissionv1.Operation) admission.Request {
    raw, _ := json.Marshal(pod)
    return admission.Request{
        AdmissionRequest: admissionv1.AdmissionRequest{
            Operation: op,
            Name:      pod.Name,
            Namespace: pod.Namespace,
            Object:    runtime.RawExtension{Raw: raw},
        },
    }
}

// Then test the handler:
handler := &iprewrite.Handler{InitContainerImage: "test-image:latest"}
resp := handler.Handle(context.Background(), makePodRequest(pod, admissionv1.Create))
// Assert: resp.Allowed, resp.Patches, etc.
```

**Key adaptation for mutating webhook tests:**
- The existing tests are for a **validating** webhook (checks `resp.Allowed` and `resp.Warnings`)
- Our tests are for a **mutating** webhook — must assert the JSON patch contains the init container injection
- Use `admission.PatchResponseFromRaw()` internally — tests verify the response patches

To verify patches, unmarshal `resp.Patches` (type `[]jsonpatch.JsonPatchOperation`) and assert:
- Patch operation adds init container at path `/spec/initContainers/0` (prepend)
- Init container has correct `name`, `image`, `env`, `volumeMounts`, `securityContext`

Alternatively, apply the patches to the original pod JSON and unmarshal the result to verify the final pod spec.

#### Annotation-to-Env-Var Transformation Algorithm

The transformation logic in `handler.go` (from Story 18.5):

```
soteria.io/eth0-ip          → SOTERIA_ETH0_IP
soteria.io/ens3-ip          → SOTERIA_ENS3_IP
soteria.io/my-custom-nic-ip → SOTERIA_MY_CUSTOM_NIC_IP
soteria.io/dns              → SOTERIA_DNS
```

Algorithm:
1. Filter: only annotations starting with `soteria.io/`
2. Special case: `soteria.io/dns` → `SOTERIA_DNS`
3. For `soteria.io/<name>-ip`: strip `soteria.io/` prefix, strip trailing `-ip`, uppercase, replace `-` with `_`, prefix `SOTERIA_`, suffix `_IP`
4. Ignore `soteria.io/ip-rewrite` (label key, not an annotation for env vars)
5. Ignore any `soteria.io/*` annotation that doesn't end in `-ip` and isn't `dns`

Test cases must verify all of these rules including edge cases.

#### Synthetic RHEL Disk Image — ifcfg Fixture

Create a minimal ext4 disk image with an ifcfg file for RHEL 7/8 style testing:

```bash
#!/usr/bin/env bash
set -euo pipefail

OUTPUT="${1:-/tmp/rhel-ifcfg-fixture.img}"

# Create a 200MB disk with ext4 filesystem
guestfish -N "$OUTPUT"=fs:ext4:200M <<'EOF'
# Create the directory structure
mkdir-p /etc/sysconfig/network-scripts

# Write an ifcfg file with pre-existing static IP
write /etc/sysconfig/network-scripts/ifcfg-eth0 "TYPE=Ethernet
BOOTPROTO=none
DEVICE=eth0
IPADDR=10.0.1.50
PREFIX=24
GATEWAY=10.0.1.1
DNS1=8.8.8.8
ONBOOT=yes
"
EOF
```

**Important:** `guestfish -N` creates a new disk image. The syntax `guestfish -N <path>=fs:<type>:<size>` creates a formatted filesystem. Inside the guestfish session, write the network config files.

#### Synthetic RHEL Disk Image — NM Keyfile Fixture

Create for RHEL 9/10 style testing:

```bash
#!/usr/bin/env bash
set -euo pipefail

OUTPUT="${1:-/tmp/rhel-nmkeyfile-fixture.img}"

guestfish -N "$OUTPUT"=fs:ext4:200M <<'EOF'
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
EOF
```

#### Synthetic Windows Disk Image — NTFS + SYSTEM Hive Fixture

This is the most complex fixture. Steps:

1. Create a blank NTFS disk image via guestfish
2. Create the `Windows/System32/config/` directory structure
3. Create a minimal SYSTEM hive with the required registry structure using hivex tools
4. Upload the hive into the disk image

**Creating a minimal SYSTEM hive:**

There is no `hivex-create` command that creates a hive from scratch. The approach is:

**Option A — Use an empty hive template created by hivexsh:**
```bash
# Create minimal hive with required structure
hivexsh -w /tmp/system.hive <<'EOF'
add SYSTEM
cd SYSTEM
# Select key: CurrentControlSet pointer
add Select
cd Select
setval 1
Current
dword:00000001
cd ..
# ControlSet001 tree
add ControlSet001
cd ControlSet001
add Services
cd Services
add Tcpip
cd Tcpip
add Parameters
cd Parameters
add Interfaces
cd Interfaces
# Add an adapter GUID
add {12345678-1234-1234-1234-123456789abc}
cd {12345678-1234-1234-1234-123456789abc}
setval 4
EnableDHCP
dword:00000000
IPAddress
hex(7):<utf16le hex for "10.0.1.50" + double null>
SubnetMask
hex(7):<utf16le hex for "255.255.255.0" + double null>
DefaultGateway
hex(7):<utf16le hex for "10.0.1.1" + double null>
commit
EOF
```

**CRITICAL NOTE:** `hivexsh -w <file>` with a non-existent file may not create a new hive. Test this. If it doesn't work, use `hivexregedit --merge` on a minimal hive blob created by another method.

**Option B — Generate hive via hivexregedit from a .reg file:**
```bash
# 1. Use a tool that can create blank hives, or start with a dummy hive
# 2. hivexregedit --merge to populate it
```

**Practical approach:** Create a small Python script using the `hivex` Python module to create the hive structure. **BUT** Story 18.1 does NOT install `python3-hivex`. Alternative: use `hivexsh` to construct the hive (it CAN create new hives with `add` for new keys at the root level).

The fixture creation script should:
1. Create a blank NTFS disk: `guestfish -N "$OUTPUT"=fs:ntfs:200M`
2. Create directory structure: `mkdir-p /Windows/System32/config`
3. Build the SYSTEM hive locally using hivexsh
4. Upload the hive via guestfish: `upload /tmp/system.hive /Windows/System32/config/system`

#### LIBGUESTFS_BACKEND=direct in CI

GitHub Actions runners do NOT have KVM. The `LIBGUESTFS_BACKEND=direct` environment variable forces guestfish to use the QEMU TCG (software emulation) backend instead of KVM. This is:
- **Slower** (~5-10 seconds per appliance boot instead of ~1 second with KVM)
- **No special hardware needed** — works on any Linux system with QEMU installed
- **Already baked into the container image** (from Story 18.1) but must also be set for host-level test execution

The Makefile target should set this:
```makefile
.PHONY: test-ip-rewrite
test-ip-rewrite: ## Run IP rewrite integration tests (requires guestfish).
	LIBGUESTFS_BACKEND=direct bash test/ip-rewrite/run-tests.sh
```

#### Integration Test Verification Pattern

After running a handler script on a fixture disk, verify the results by re-reading with guestfish:

**RHEL ifcfg verification:**
```bash
# Read back the modified ifcfg file
RESULT=$(guestfish --ro -a "$FIXTURE" -i -- cat /etc/sysconfig/network-scripts/ifcfg-eth0)
# Assert IPADDR, PREFIX, GATEWAY values changed
echo "$RESULT" | grep -q 'IPADDR=10.0.2.100' || fail "IPADDR not rewritten"
echo "$RESULT" | grep -q 'PREFIX=24'          || fail "PREFIX not rewritten"
echo "$RESULT" | grep -q 'GATEWAY=10.0.2.1'   || fail "GATEWAY not rewritten"
```

**RHEL NM keyfile verification:**
```bash
RESULT=$(guestfish --ro -a "$FIXTURE" -i -- cat /etc/NetworkManager/system-connections/eth0.nmconnection)
echo "$RESULT" | grep -q 'address1=10.0.2.100/24,10.0.2.1' || fail "address1 not rewritten"
echo "$RESULT" | grep -q 'method=manual'                     || fail "method not manual"
```

**Windows hive verification:**
```bash
# Download the modified hive and read with hivexget
guestfish --ro -a "$FIXTURE" -i -- download /Windows/System32/config/system /tmp/verify.hive
# Read IPAddress value
hivexget /tmp/verify.hive \
  'ControlSet001\Services\Tcpip\Parameters\Interfaces\{12345678-1234-1234-1234-123456789abc}' \
  IPAddress
# Parse REG_MULTI_SZ output to verify IP changed to 10.0.2.100
```

#### Test Runner Script Structure

Create a top-level test runner at `test/ip-rewrite/run-tests.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FIXTURE_DIR="$SCRIPT_DIR/fixtures"
PASSED=0
FAILED=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

pass() { ((PASSED++)); echo -e "${GREEN}PASS${NC}: $1"; }
fail() { ((FAILED++)); echo -e "${RED}FAIL${NC}: $1: $2"; }

# Phase 1: Create fixtures
echo "=== Creating test fixtures ==="
bash "$FIXTURE_DIR/create-rhel-ifcfg-fixture.sh" /tmp/rhel-ifcfg.img
bash "$FIXTURE_DIR/create-rhel-nmkeyfile-fixture.sh" /tmp/rhel-nmkeyfile.img
bash "$FIXTURE_DIR/create-windows-fixture.sh" /tmp/windows.img

# Phase 2: Run handler tests
echo "=== Running RHEL ifcfg handler test ==="
# ... set REWRITE_* vars, source handler, verify ...

echo "=== Running RHEL NM keyfile handler test ==="
# ... set REWRITE_* vars, source handler, verify ...

echo "=== Running Windows handler test ==="
# ... set REWRITE_* vars, source handler, verify ...

# Summary
echo "========================"
echo "Passed: $PASSED, Failed: $FAILED"
[ "$FAILED" -eq 0 ] || exit 1
```

#### Guestfish `-N` Syntax Reference

The `guestfish -N` flag creates a new disk image with a prepared filesystem:

| Syntax | Result |
|--------|--------|
| `guestfish -N fs:ext4:200M` | 200MB disk with ext4 filesystem |
| `guestfish -N fs:ntfs:200M` | 200MB disk with NTFS filesystem |
| `guestfish -N fs:xfs:200M` | 200MB disk with XFS filesystem |

The `-N` flag outputs the temporary path to stdout. To control the output path, use:
```bash
guestfish -N /path/to/output.img=fs:ext4:200M
```

Inside the session opened by `-N`, the disk is already added and the filesystem is mounted at `/`. Write files directly.

#### Container-Based vs Host-Based Integration Tests

There are two strategies for running integration tests:

**Option A — Host-based (recommended for CI simplicity):**
- Install `guestfs-tools`, `hivex` on the CI runner (Ubuntu packages: `libguestfs-tools`, `hivex`)
- Run fixture scripts and handler scripts directly on the runner
- Set `LIBGUESTFS_BACKEND=direct`
- Pros: simpler CI workflow, no container build dependency
- Cons: requires package installation step in CI

**Option B — Container-based:**
- Build the ip-rewrite container image first
- Run tests inside the container with disk images mounted as volumes
- Pros: tests exactly mirror production environment
- Cons: requires building the container image before testing (dependency on CI build step)

**Recommended:** Option A for `make test-ip-rewrite`. The fixture scripts and verification are standalone — they don't need the full container. The handler scripts (`rhel-handler.sh`, `windows-handler.sh`) are plain bash scripts that run guestfish commands.

For CI (Story 18.9), add a step that installs `libguestfs-tools` and `hivex` packages before running `make test-ip-rewrite`.

#### Makefile Target Design

```makefile
# IP rewrite integration tests — requires guestfish and hivex tools installed
IP_REWRITE_TEST_DIR ?= test/ip-rewrite

.PHONY: test-ip-rewrite
test-ip-rewrite: ## Run IP rewrite handler integration tests (requires guestfish).
	@command -v guestfish >/dev/null 2>&1 || { echo "guestfish not found. Install guestfs-tools."; exit 1; }
	@command -v hivexregedit >/dev/null 2>&1 || { echo "hivexregedit not found. Install hivex."; exit 1; }
	LIBGUESTFS_BACKEND=direct bash $(IP_REWRITE_TEST_DIR)/run-tests.sh
```

This target:
- Checks prerequisites (`guestfish`, `hivexregedit`)
- Sets `LIBGUESTFS_BACKEND=direct`
- Runs the test runner script
- Is NOT part of `make test` (integration tests are separate)

### Project Structure Notes

After this story, new files:

```
internal/
└── webhook/
    └── iprewrite/
        └── handler_test.go      ← NEW: Go unit tests for webhook handler

test/
└── ip-rewrite/
    ├── run-tests.sh             ← NEW: Top-level integration test runner
    └── fixtures/
        ├── create-rhel-ifcfg-fixture.sh      ← NEW: RHEL 7/8 ifcfg disk fixture
        ├── create-rhel-nmkeyfile-fixture.sh   ← NEW: RHEL 9/10 NM keyfile disk fixture
        └── create-windows-fixture.sh          ← NEW: Windows NTFS + SYSTEM hive fixture
```

Modified files:

```
Makefile                     ← MODIFIED: add test-ip-rewrite target
```

**Alignment with existing project patterns:**
- Go unit tests in the same package directory: `internal/webhook/iprewrite/handler_test.go` alongside `handler.go` (standard Go convention, follows `pkg/admission/vm_validator_test.go` pattern)
- Integration test scripts in `test/` directory: follows the existing `test/e2e/` pattern
- Fixture scripts in `test/ip-rewrite/fixtures/` as specified by the epic

**Dependencies on other story artifacts:**
- `internal/webhook/iprewrite/handler.go` must exist (from Story 18.5) before `handler_test.go` can compile
- `build/ip-rewrite/scripts/rhel-handler.sh` must exist (from Story 18.3) before RHEL integration tests can run
- `build/ip-rewrite/scripts/windows-handler.sh` must exist (from Story 18.4) before Windows integration tests can run
- `build/ip-rewrite/scripts/entrypoint.sh` must exist (from Story 18.2) because handlers use `source` to inherit functions

If running this story before 18.3/18.4/18.5 are complete, the Go unit tests (Task 1-2) can be written against the expected handler interface, and integration tests (Tasks 3-6) can be written as scripts that will pass once the handlers exist.

### Anti-Patterns / DO NOT

- **DO NOT use envtest for webhook unit tests** — the webhook handler is pure logic. Use standard `admission.Request` objects like `pkg/admission/vm_validator_test.go`. envtest adds unnecessary complexity and startup time.
- **DO NOT commit binary disk images to git** — all fixture images must be created programmatically by scripts. Disk images are large (~200MB each) and should be created on-demand in `/tmp/` or a build directory.
- **DO NOT add integration tests to `make test`** — integration tests require `guestfish` and `hivex` tools that are not standard Go tooling. Keep them in a separate `make test-ip-rewrite` target.
- **DO NOT use Ginkgo/Gomega for webhook unit tests** — the existing webhook tests in `pkg/admission/` use standard `testing.T` with table-driven tests. Follow the same pattern for consistency.
- **DO NOT require KVM or nested virtualization** — all tests must work with `LIBGUESTFS_BACKEND=direct` (software QEMU emulation). Do not use `guestfish` commands that require hardware acceleration.
- **DO NOT modify handler scripts** (`rhel-handler.sh`, `windows-handler.sh`, `entrypoint.sh`) — if tests reveal bugs, document them but do not fix handler code. This story is purely about tests.
- **DO NOT modify the webhook handler** (`handler.go`) — if tests reveal issues, document them. Handler code is Story 18.5.
- **DO NOT modify `Containerfile`** — no changes to the container image.
- **DO NOT modify `.github/workflows/ci.yml` or `release.yml`** — CI integration is Story 18.9's responsibility. Only add the `test-ip-rewrite` Makefile target.
- **DO NOT use `python3-hivex`** for fixture creation — Python hivex bindings are not installed in the container (per Story 18.1 decision). Use `hivexsh` and `hivexregedit` CLI tools.
- **DO NOT create full OS disk images** — synthetic fixtures contain only the minimum files needed for testing (network config files for RHEL, SYSTEM hive for Windows). No kernel, no bootloader, no actual OS.
- **DO NOT install EPEL or third-party packages** in any fixture creation script.
- **DO NOT use `#!/bin/bash`** — use `#!/usr/bin/env bash` for consistency with the rest of Epic 18.
- **DO NOT create E2E tests** — that is Story 18.8 (VM boot with rewritten IP on a real cluster).

### Go Module Dependencies for Unit Tests

All required dependencies are already in `go.mod`:

| Dependency | Version | Used For |
|-----------|---------|----------|
| `sigs.k8s.io/controller-runtime` | `v0.24.1` | `pkg/webhook/admission` — `Request`, `Response`, `PatchResponseFromRaw` |
| `k8s.io/api` | `v0.36.3` | `core/v1` — Pod, Container, VolumeMount, PersistentVolumeClaimVolumeSource |
| `k8s.io/apimachinery` | `v0.36.3` | `runtime.RawExtension`, `admissionv1.AdmissionRequest` |

**No new Go dependencies needed.** Do not add any.

### Testing the Annotation Parsing Logic

If Story 18.5 extracts the annotation parsing into a separate exported function (e.g., `AnnotationsToEnvVars(annotations map[string]string) []corev1.EnvVar`), test it directly.

If the parsing is embedded in the `Handle` method, test it indirectly by creating pods with specific annotations and verifying the resulting init container's env vars in the JSON patch.

**Recommended approach:** Suggest that the handler exposes a testable function. If it doesn't, test via the full `Handle` method (still straightforward).

### Verification Commands

```bash
# Run Go unit tests (webhook handler)
go test ./internal/webhook/iprewrite/ -v -count=1

# Run integration tests (requires guestfish + hivex tools)
make test-ip-rewrite

# Verify fixture creation scripts work individually
LIBGUESTFS_BACKEND=direct bash test/ip-rewrite/fixtures/create-rhel-ifcfg-fixture.sh /tmp/test-rhel.img
LIBGUESTFS_BACKEND=direct bash test/ip-rewrite/fixtures/create-windows-fixture.sh /tmp/test-win.img

# Verify fixtures contain expected files
guestfish --ro -a /tmp/test-rhel.img -i -- cat /etc/sysconfig/network-scripts/ifcfg-eth0
guestfish --ro -a /tmp/test-win.img -i -- ls /Windows/System32/config/
```

### References

- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Story 18.2 spec: `_bmad-output/implementation-artifacts/18-2-ip-rewrite-entrypoint-script-os-detection-dispatch.md`]
- [Story 18.3 spec: `_bmad-output/implementation-artifacts/18-3-rhel-ip-rewrite-handler-augeas-based.md`]
- [Story 18.4 spec: `_bmad-output/implementation-artifacts/18-4-windows-ip-rewrite-handler-hivex-based.md`]
- [Story 18.5 spec: `_bmad-output/implementation-artifacts/18-5-mutating-webhook-virt-launcher-init-container-injection.md`]
- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18", Story 18.7 at ~line 4674]
- [Existing webhook test pattern: `pkg/admission/vm_validator_test.go`]
- [Existing test Makefile targets: `Makefile` lines 69-95]
- [guestfish -N disk creation: https://libguestfs.org/guestfish.1.html — "PREPARED DISK IMAGES"]
- [hivexsh manual (setval, add, cd): https://libguestfs.org/hivexsh.1.html]
- [hivexregedit --merge: https://libguestfs.org/hivexregedit.1.html]
- [hivexget for reading values: https://libguestfs.org/hivexget.1.html]
- [LIBGUESTFS_BACKEND=direct documentation: https://libguestfs.org/guestfs.3.html#backend]
- [admission.PatchResponseFromRaw: controller-runtime pkg/webhook/admission]
- [Windows Tcpip\Parameters\Interfaces registry: https://superuser.com/questions/1338775]

## Code Review Record

### Review Model Used
*(To be filled during code review — must differ from dev model)*

### Review Findings
*(To be filled during code review)*

### Decisions Needed / Decisions Taken
*(To be filled during code review)*

### Fixes Applied
*(To be filled during code review)*

## Dev Agent Record

### Agent Model Used

*(To be filled by dev agent)*

### Debug Log References

### Completion Notes List

### File List
