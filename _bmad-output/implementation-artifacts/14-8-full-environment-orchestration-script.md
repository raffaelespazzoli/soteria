# Story 14.8: Full Environment Orchestration Script

Status: ready-for-dev

## Story

As a platform engineer,
I want a single script that provisions the entire multi-site test infrastructure in sequence,
so that the complete environment can be set up with one command.

## Acceptance Criteria

**AC1: Sequential execution**
Given the `hack/multisite/` directory with individual setup scripts
When `setup-all.sh` is executed
Then it calls the following scripts in order:
  1. `setup-clusters.sh` — Minikube KVM2 clusters + Cilium Cluster Mesh
  2. `setup-rook-ceph.sh` — Rook-Ceph + RBD mirroring
  3. `setup-kubevirt.sh` — KubeVirt + CDI
  4. `validate-fedora-vm.sh` — Fedora VM validation + node sizing
  5. `setup-scylladb.sh` — ScyllaDB cross-DC deployment

**AC2: Fail-fast behavior**
Given the orchestration script is running
When any individual setup script exits with a non-zero code
Then execution halts immediately with a clear error message indicating which step failed
And the exit code is propagated

**AC3: Skip support**
Given the orchestration script
When invoked with `--skip <step-name>` flags (e.g., `--skip clusters --skip rook-ceph`)
Then the specified steps are skipped
And remaining steps execute in order

**AC4: Teardown support**
Given the orchestration script
When invoked with `teardown` subcommand
Then teardown is performed in reverse order (ScyllaDB → KubeVirt → Rook-Ceph → clusters)
And each teardown step tolerates partial state

**AC5: Timing and summary**
Given the orchestration script completes
When all steps succeed
Then a summary is printed showing each step name and elapsed time
And total elapsed time is displayed

**AC6: Idempotent**
Given the orchestration script
When run multiple times
Then it is idempotent (each underlying script is already idempotent)

## Tasks / Subtasks

- [ ] Task 1: Create `hack/multisite/setup-all.sh` (AC: 1, 2, 5, 6)
  - [ ] 1.1: Apache 2.0 license header + description comment block matching existing scripts
  - [ ] 1.2: `set -euo pipefail`, `SCRIPT_DIR` derivation
  - [ ] 1.3: Color helpers (`info`, `warn`, `error`, `fatal`) — same as other scripts
  - [ ] 1.4: Define ordered step list as an array: `(clusters rook-ceph kubevirt fedora-vm scylladb)` with corresponding script paths
  - [ ] 1.5: `run_step()` function — sources SCRIPT_DIR, records `SECONDS` before/after, calls script, captures exit code, stores elapsed time
  - [ ] 1.6: Main loop iterating steps, checking skip list, calling `run_step()`, halting on failure (AC: 1, 2)
  - [ ] 1.7: Summary table at completion — step name, elapsed time (mm:ss), pass/skip status, total elapsed (AC: 5)

- [ ] Task 2: Implement `--skip` flag parsing (AC: 3)
  - [ ] 2.1: Parse CLI args — `--skip <name>` adds to a skip set (associative array)
  - [ ] 2.2: Valid step names: `clusters`, `rook-ceph`, `kubevirt`, `fedora-vm`, `scylladb`
  - [ ] 2.3: Warn on unknown step names, don't fail
  - [ ] 2.4: Print which steps are being skipped at start

- [ ] Task 3: Implement `teardown` subcommand (AC: 4)
  - [ ] 3.1: Detect `teardown` as first positional arg
  - [ ] 3.2: Define teardown order as reverse of setup: `(scylladb kubevirt rook-ceph clusters)`
  - [ ] 3.3: For `clusters` teardown — delegate to existing `teardown.sh`
  - [ ] 3.4: For component teardown (`scylladb`, `kubevirt`, `rook-ceph`) — use namespace/resource deletion that tolerates missing resources
  - [ ] 3.5: Each teardown step runs inside `set +e` block — failures are logged but don't halt teardown
  - [ ] 3.6: `--skip` flags also apply to teardown (skip specific teardown steps)

- [ ] Task 4: Environment variable passthrough (AC: 6)
  - [ ] 4.1: Pass through `EAST_CLUSTER_NAME` and `WEST_CLUSTER_NAME` if set (all sub-scripts read these)
  - [ ] 4.2: Document all env vars from sub-scripts in the header comment

- [ ] Task 5: Make script executable and test (AC: 1-6)
  - [ ] 5.1: `chmod +x hack/multisite/setup-all.sh`
  - [ ] 5.2: Verify `--help` or no-arg invocation prints usage
  - [ ] 5.3: Verify `--skip clusters --skip rook-ceph` skips the right steps
  - [ ] 5.4: Verify `teardown` runs in reverse order

## Dev Notes

### Scope and Context

This is a **pure infrastructure story** — a single shell script (~80-120 lines). No Go code, no Kustomize changes, no overlay modifications. The output is:
- `hack/multisite/setup-all.sh` — orchestration script

The script wraps the 5 existing setup scripts from Stories 14.1-14.5 into a single sequential runner with fail-fast semantics, skip support, and timing.

**Explicitly excluded:** `deploy-soteria.sh` (Story 14.6) is NOT called by this script. Soteria deployment belongs in the e2e test `BeforeSuite` for last-minute recompilation — the operator image may be rebuilt immediately before testing.

### Script Convention Compliance

Follow the exact patterns established by Stories 14.1-14.5. All existing scripts share:

**Header pattern:**
```bash
#!/usr/bin/env bash

# Copyright 2026 The Soteria Authors.
# (Apache 2.0 license block — 13 lines)

# <description comment block>
#
# Usage:
#   ./hack/multisite/setup-all.sh [--skip <step>]... [teardown]
#
# Environment Variables:
#   EAST_CLUSTER_NAME   Name of the east cluster (default: east)
#   WEST_CLUSTER_NAME   Name of the west cluster (default: west)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
```

**Color helpers (identical across all scripts):**
```bash
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { error "$@"; exit 1; }
```

**Section separators:** Use `# ---------------------------------------------------------------------------` comment blocks between logical sections, matching the existing style.

### Step Registry Design

Define steps as parallel arrays (bash 3 compatible — no associative array for step definitions):

```bash
STEP_NAMES=(clusters rook-ceph kubevirt fedora-vm scylladb)
STEP_SCRIPTS=(
  "${SCRIPT_DIR}/setup-clusters.sh"
  "${SCRIPT_DIR}/setup-rook-ceph.sh"
  "${SCRIPT_DIR}/setup-kubevirt.sh"
  "${SCRIPT_DIR}/validate-fedora-vm.sh"
  "${SCRIPT_DIR}/setup-scylladb.sh"
)
STEP_LABELS=(
  "Minikube KVM2 clusters + Cilium Cluster Mesh"
  "Rook-Ceph + RBD mirroring"
  "KubeVirt + CDI"
  "Fedora VM validation + node sizing"
  "ScyllaDB cross-DC deployment"
)
```

For the skip set, use a `declare -A SKIP_SET` associative array (bash 4+ is available on Fedora).

### Timing Pattern

Use bash `SECONDS` variable or `date +%s` for per-step timing:

```bash
run_step() {
  local name="$1" script="$2" label="$3"
  local start_seconds=$SECONDS
  info "=== Step: ${label} ==="
  "${script}"
  local elapsed=$(( SECONDS - start_seconds ))
  STEP_TIMES+=("${elapsed}")
  STEP_RESULTS+=("done")
  info "=== Step '${name}' completed in $(format_time ${elapsed}) ==="
}
```

Format elapsed time as `Xm Ys` (e.g., `3m 42s`):
```bash
format_time() {
  local secs="$1"
  printf '%dm %ds' $((secs / 60)) $((secs % 60))
}
```

### Teardown Design

The `teardown` subcommand runs in reverse order. For the `clusters` step, delegate to the existing `teardown.sh`. For component teardown:

- **ScyllaDB teardown:** Delete the `soteria` namespace (ScyllaDB + certs live there), uninstall scylla-operator Helm release, uninstall cert-manager Helm release. Tolerate errors.
- **KubeVirt teardown:** Delete KubeVirt CR, delete KubeVirt operator, delete CDI CR, delete CDI operator. Tolerate errors.
- **Rook-Ceph teardown:** Delete CephCluster CRs, delete rook-ceph namespace, uninstall Rook operator Helm release, clean up CSI Addons. Tolerate errors (Rook cleanup can be finicky).
- **Clusters teardown:** Call `${SCRIPT_DIR}/teardown.sh` (already handles Minikube deletion + kubeconfig cleanup).

Each teardown step wraps in `set +e` / `set -e` so that failures in one component don't prevent teardown of others. Print warnings for failures but continue.

**Simpler alternative (recommended):** Since `teardown.sh` already deletes the Minikube clusters entirely (destroying all components), the teardown subcommand can simply call `teardown.sh` directly. Component-level teardown is only useful if you want to tear down a single layer without destroying clusters. Implement the simple version first (just delegate to `teardown.sh`), then add component-level teardown if needed.

### Argument Parsing

Keep it simple — no `getopt`/`getopts` (matches the simplicity of other scripts which use env vars):

```bash
SKIP_SET=()
ACTION="setup"

while [[ $# -gt 0 ]]; do
  case "$1" in
    teardown)
      ACTION="teardown"
      shift
      ;;
    --skip)
      [[ $# -lt 2 ]] && fatal "--skip requires a step name"
      SKIP_SET+=("$2")
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fatal "Unknown argument: $1. Use --help for usage."
      ;;
  esac
done
```

### Summary Table Format

Print a table at the end:

```
=== Setup Complete ===

Step               Status   Elapsed
─────────────────────────────────────
clusters           done     3m 42s
rook-ceph          done     8m 15s
kubevirt           done     2m 30s
fedora-vm          skipped  —
scylladb           done     5m 10s
─────────────────────────────────────
Total                       19m 37s
```

Use `printf` for column alignment.

### Environment Variable Passthrough

The script itself only adds `EAST_CLUSTER_NAME` and `WEST_CLUSTER_NAME` as configurable. All other env vars are passed through transparently to sub-scripts (since they are exported or read from the environment). The header comment should list the env vars from all sub-scripts for discoverability.

### File Existence Validation

Before running any step, verify the script file exists:

```bash
for i in "${!STEP_SCRIPTS[@]}"; do
  [[ -x "${STEP_SCRIPTS[$i]}" ]] || fatal "Script not found or not executable: ${STEP_SCRIPTS[$i]}"
done
```

### Dependencies

| Dependency | Story | What's Needed |
|-----------|-------|---------------|
| setup-clusters.sh | 14.1 | Minikube + Cilium Cluster Mesh setup |
| setup-rook-ceph.sh | 14.2 | Rook-Ceph + RBD mirroring + CSI Addons |
| setup-kubevirt.sh | 14.3 | KubeVirt + CDI with KVM acceleration |
| validate-fedora-vm.sh | 14.4 | Fedora VM validation + node sizing |
| setup-scylladb.sh | 14.5 | ScyllaDB cross-DC with mTLS |
| teardown.sh | 14.1 | Cluster deletion (for teardown subcommand) |

### Previous Story Intelligence

**From Story 14.1 (setup-clusters.sh):**
- ~494 lines, env-var driven, idempotent
- Creates `east` and `west` Minikube profiles with 1 CP + 3 workers
- Takes ~3-5 minutes per cluster
- Uses `EAST_CLUSTER_NAME` / `WEST_CLUSTER_NAME` env vars consistently

**From Story 14.2 (setup-rook-ceph.sh):**
- ~1136 lines, longest script
- Deploys Rook operator + CephCluster + CSI Addons + mirroring
- Takes ~8-15 minutes (Ceph cluster convergence)
- Smoke test at end validates replication

**From Story 14.3 (setup-kubevirt.sh):**
- ~622 lines, deploys KubeVirt + CDI
- Takes ~2-5 minutes
- Validates container disk + PVC-backed VM smoke tests

**From Story 14.4 (validate-fedora-vm.sh):**
- ~634 lines, validates Fedora VM boot
- Takes ~5-10 minutes (Fedora image download + VM boot)
- Calculates node sizing for the full 6-VM test

**From Story 14.5 (setup-scylladb.sh):**
- ~648 lines, deploys cross-DC ScyllaDB
- Takes ~5-10 minutes (operator + ScyllaCluster convergence)
- Validates multi-DC CQL replication

**From teardown.sh:**
- ~81 lines, simple — just deletes Minikube profiles + kubeconfigs
- Uses `minikube delete -p` with existence check
- Tolerant of missing clusters

**Total estimated setup time:** ~25-45 minutes end-to-end (varies with download speeds and hardware).

### Git Intelligence

Recent commits show:
- `0d8a58e`: Removed `fix-ceph-osd-auth.sh` and `prepare-docker.sh` (obsolete helper scripts)
- `6031d82`: Defined Epic 15 and Story 14-8 in sprint status + epics
- `a9302b9`-`665414b`: Stories 14.3-14.5 implemented — all scripts follow the same conventions
- Scripts have been stable since initial implementation — no major refactoring

### Potential Failure Modes

1. **Sub-script not executable** — `setup-all.sh` should verify all scripts are executable before starting
2. **Partial setup state** — If step 3 fails, steps 1-2 remain. Re-running is safe (idempotent scripts) but `--skip clusters --skip rook-ceph` can save time
3. **Teardown after partial setup** — `teardown.sh` handles missing clusters gracefully. Component-level teardown should also tolerate missing resources
4. **Env var conflicts** — All sub-scripts read `EAST_CLUSTER_NAME` and `WEST_CLUSTER_NAME` from environment. If set inconsistently between runs, clusters may not be found

### Testing Standards

No Go tests — validation is via manual execution:
1. Run `setup-all.sh` end-to-end and verify all steps complete
2. Run `setup-all.sh --skip clusters --skip rook-ceph` and verify only kubevirt, fedora-vm, scylladb run
3. Run `setup-all.sh teardown` and verify clusters are destroyed
4. Run `setup-all.sh --help` and verify usage is printed
5. Kill a sub-script mid-execution and verify fail-fast works (clean error message)

### Project Structure Notes

```
hack/multisite/
├── setup-all.sh              # NEW — this story (orchestration script)
├── setup-clusters.sh          # From Story 14.1
├── setup-rook-ceph.sh         # From Story 14.2
├── setup-kubevirt.sh          # From Story 14.3
├── validate-fedora-vm.sh      # From Story 14.4
├── setup-scylladb.sh          # From Story 14.5
├── teardown.sh                # From Story 14.1
├── manifests/                 # From Story 14.2
├── overlays/                  # From Stories 14.4-14.5
└── README.md                  # Updated in Stories 14.1-14.5
```

### References

- [Source: epics.md#Story 14.8] — acceptance criteria and technical notes
- [Source: hack/multisite/setup-clusters.sh] — script convention reference (license header, helpers, env vars, SCRIPT_DIR)
- [Source: hack/multisite/setup-rook-ceph.sh] — same conventions, longest script
- [Source: hack/multisite/setup-kubevirt.sh] — same conventions
- [Source: hack/multisite/validate-fedora-vm.sh] — same conventions
- [Source: hack/multisite/setup-scylladb.sh] — same conventions
- [Source: hack/multisite/teardown.sh] — teardown pattern (Minikube deletion, tolerance of missing clusters)
- [Source: project-context.md] — Soteria architecture context

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
