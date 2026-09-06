# Story 18.10: Documentation — Architecture & Usage

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a user,
I want documentation pages explaining the IP rewrite feature's architecture and how to use it,
so that I can understand how the feature works and configure it for my VMs.

## Acceptance Criteria

### AC1: Architecture page
Given a new page at `docs/architecture/ip-rewrite.md`
When published
Then it explains:
- The mutating webhook interception mechanism (virt-launcher pod CREATE)
- Annotation and label contract (with examples)
- OS detection flow via virt-inspector
- Linux rewrite path (Augeas for ifcfg and NM keyfile)
- Windows rewrite path (hivex for registry hive)
- Migration detection via `kubevirt.io/migrationJobLabel`
- A Mermaid sequence diagram showing: VM annotation → pod CREATE → API server → webhook → init container injection → guestfish OS detection → handler dispatch → VM boot with new IP

### AC2: Usage guide page
Given a new page at `docs/usage/ip-rewrite.md`
When published
Then it covers:
- Prerequisites (cert-manager, OpenShift Virtualization or KubeVirt)
- Installing the IP rewrite webhook via Helm (`helm install soteria-ip-rewrite charts/soteria-ip-rewrite/`)
- Annotating a VM for single-NIC IP rewrite (step-by-step with kubectl commands)
- Annotating a VM for multi-NIC IP rewrite
- Optional DNS configuration
- Verifying the rewrite (checking init container logs, QEMU guest agent output)
- Troubleshooting section (SCC issues, guestfish errors, unsupported OS)

### AC3: Pages follow existing style
Given the existing documentation pages in `docs/`
When the new pages are compared
Then they follow the same heading structure, admonition style (mkdocs-material), and cross-linking patterns
And code examples use the same YAML/shell formatting

### AC4: Mermaid diagram renders
Given the architecture page includes a Mermaid sequence diagram
When the mkdocs site is built with `mkdocs build --strict`
Then the diagram renders correctly in the documentation site

## Tasks / Subtasks

- [x] Task 1: Create `docs/architecture/ip-rewrite.md` — Architecture page (AC: 1, 3, 4)
  - [x] 1.1: Write overview section — standalone add-on, no Soteria CRD dependency
  - [x] 1.2: Write annotation and label contract section with YAML examples
  - [x] 1.3: Write mutating webhook flow section — objectSelector, CREATE interception, init container injection
  - [x] 1.4: Write OS detection section — virt-inspector, boot disk identification
  - [x] 1.5: Write Linux rewrite section — Augeas, ifcfg (RHEL 7/8), NM keyfile (RHEL 8/9/10)
  - [x] 1.6: Write Windows rewrite section — hivex, SYSTEM hive, ControlSet, adapter GUID
  - [x] 1.7: Write migration detection section — `kubevirt.io/migrationJobLabel` skip logic
  - [x] 1.8: Create Mermaid sequence diagram — full flow from annotation to VM boot
  - [x] 1.9: Write fail-open policy section — `failurePolicy: Ignore`
- [x] Task 2: Create `docs/usage/ip-rewrite.md` — Usage guide page (AC: 2, 3)
  - [x] 2.1: Write prerequisites section — cert-manager, OCP Virt / KubeVirt, SCC
  - [x] 2.2: Write installation section — `helm install` with chart path, namespace, values
  - [x] 2.3: Write single-NIC usage section — step-by-step with kubectl commands
  - [x] 2.4: Write multi-NIC usage section — multiple interface annotations
  - [x] 2.5: Write DNS configuration section — optional `soteria.io/dns` annotation
  - [x] 2.6: Write verification section — init container logs, QEMU guest agent
  - [x] 2.7: Write troubleshooting section — SCC, guestfish errors, unsupported OS, migration skip
- [x] Task 3: Add cross-links from existing architecture overview page (AC: 3)
  - [x] 3.1: Add IP rewrite mention to `docs/architecture/overview.md` component table under "Other Components" or as a new standalone section
- [x] Task 4: Verify pages build without errors (AC: 4)
  - [x] 4.1: Run `mkdocs build --strict` and confirm zero warnings and zero errors

## Dev Notes

### Story Intelligence Chain

**Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9**

Story 18.1 defines the init container image this documentation must describe. Key context:

- **Image**: `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION` — UBI9 base with `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport`
- **Package correction**: `libguestfs-winsupport` replaces `ntfs-3g` (EPEL-only). Document the correct package name, not `ntfs-3g`
- **Runtime env var**: `LIBGUESTFS_BACKEND=direct` baked into image — explain this in architecture page (why: no libvirtd in container)
- **SYS_ADMIN capability**: Required for guestfish appliance — document in both architecture (why) and usage (troubleshooting)
- **Build location**: `build/ip-rewrite/Containerfile` — reference but don't document build steps (that's contributing/dev-setup territory)
- **Image size**: ~500-800MB (expected, due to kernel + appliance). Worth a note in architecture

**Predecessor: Story 18.5 — Mutating Webhook: virt-launcher Init Container Injection**

Story 18.5 defines the webhook that is the core of what this documentation must explain. Key context:

- **Standalone binary**: `cmd/ip-rewrite-webhook/main.go` — separate from Soteria manager. Emphasize this independence in docs
- **Handler location**: `internal/webhook/iprewrite/handler.go`
- **Webhook path**: `/mutate-v1-pod` on port 9443
- **objectSelector**: `matchLabels: {"soteria.io/ip-rewrite": "true"}` — Kubernetes API server filters; webhook never invoked for unlabeled pods
- **failurePolicy**: `Ignore` — fail-open. VMs start even if webhook is down (no IP rewrite)
- **Migration skip**: Checks for `kubevirt.io/migrationJobLabel` label — set only by KubeVirt's `RenderMigrationManifest`
- **Annotation→env var transformation**: `soteria.io/eth0-ip` → `SOTERIA_ETH0_IP`, `soteria.io/dns` → `SOTERIA_DNS`
- **PVC volume mounts**: All PVC-backed volumes mounted at `/disks/<volumeName>` in init container
- **Init container spec**: Named `ip-rewrite`, prepended before all other init containers, `SYS_ADMIN` capability
- **MWC manifest**: `config/ip-rewrite-webhook/mutatingwebhookconfiguration.yaml` — reference for documentation accuracy
- **TLS via cert-manager**: `cert-manager.io/inject-ca-from` annotation on MWC

**Epic 18 overall context for documentation scope:**

| Story | What to Document | Documented Where |
|-------|-----------------|-----------------|
| 18.1 | Init container image contents and purpose | Architecture page (components section) |
| 18.2 | Entrypoint script — OS detection and dispatch | Architecture page (OS detection flow) |
| 18.3 | RHEL handler — Augeas ifcfg/NM keyfile | Architecture page (Linux rewrite path) |
| 18.4 | Windows handler — hivex registry hive | Architecture page (Windows rewrite path) |
| 18.5 | Mutating webhook — interception and injection | Architecture page (webhook flow + diagram) |
| 18.6 | Helm chart and SCC | Usage page (installation + troubleshooting) |

**Cross-story dependencies for doc accuracy**: Stories 18.2–18.4 (scripts) may not be implemented yet when this story runs. Document the *designed* behavior from the epic spec. The architecture and flow are stable even if scripts aren't written yet — the epic spec is the authoritative source for OS detection, handler dispatch, and config editing mechanics.

### Critical Technical Details

#### Documentation Framework — mkdocs-material

The project uses mkdocs-material. All documentation conventions are already established in Epic 17's code-driven documentation sprint.

**mkdocs.yml** — Key settings the dev agent must know:

| Setting | Value | Impact on This Story |
|---------|-------|---------------------|
| `pymdownx.superfences` custom fence | `name: mermaid` | Mermaid diagrams render via ` ```mermaid ` fenced blocks |
| `admonition` + `pymdownx.details` | Enabled | Use `!!! tip`, `!!! warning`, `!!! note`, `!!! info` |
| `pymdownx.tabbed` | `alternate_style: true` | Use `=== "Tab Title"` for tabbed content blocks |
| `content.code.copy` | Enabled | Code blocks get copy buttons automatically |
| `toc.permalink` | `true` | Heading anchors auto-generated |

**Nav structure** — This story creates pages but does NOT update `mkdocs.yml` nav (that's Story 18.11). The pages must be created at the correct paths so 18.11 can simply add nav entries.

#### Existing Documentation Patterns to Follow

Study these existing pages for consistent style:

| Pattern | Example File | What to Replicate |
|---------|-------------|-------------------|
| Architecture page with Mermaid | `docs/architecture/overview.md` | Heading hierarchy (H1 title → H2 sections → H3 subsections), Mermaid `sequenceDiagram` and `graph` syntax, component tables |
| Architecture page with state diagram | `docs/architecture/dr-lifecycle.md` | Mermaid `stateDiagram-v2`, `!!! warning` admonitions, field reference tables |
| Usage guide with step-by-step | `docs/usage/failover.md` | Numbered steps, kubectl command blocks, YAML examples with annotations |
| Installation guide with tabs | `docs/installation/helm.md` | `=== "Tab"` tabbed content, `!!! info` prereq callouts, Helm install commands |
| API reference with tables | `docs/reference/api/drplan.md` | Field tables (Name / Type / Required / Default / Description), `!!! info` callouts |

**Heading conventions:**
- H1 (`#`): Page title — one per page
- H2 (`##`): Major sections
- H3 (`###`): Subsections
- No H4+ unless absolutely necessary

**Code block conventions:**
- YAML blocks: ` ```yaml `
- Shell commands: ` ```bash `
- Go code: ` ```go `
- Always include enough context for copy-paste usability

**Cross-link conventions:**
- Relative links: `[Prerequisites](../installation/prerequisites.md)`, `[Architecture Overview](overview.md)`
- Anchor links: `[Annotation Contract](#annotation--label-contract)`

#### Architecture Page Structure — `docs/architecture/ip-rewrite.md`

Follow the pattern from `docs/architecture/overview.md`. Recommended structure:

```
# IP Rewrite Architecture

  (1-2 paragraph overview of what IP rewrite does and why it exists)

## Overview
  - Standalone add-on (no Soteria CRD dependency)
  - Use case: VM IP reconfiguration for DR, migration, subnet changes
  - Works with any tool that sets VM annotations (kubectl, ArgoCD, Ansible, Soteria DRExecution)

## Annotation & Label Contract
  - Label: soteria.io/ip-rewrite: "true"
  - Annotations: soteria.io/<interface>-ip, soteria.io/dns
  - YAML examples (single-NIC, multi-NIC, with DNS)

## How It Works
  ### Mermaid Sequence Diagram
  (Full flow from annotation through VM boot)

  ### Webhook Interception
  - objectSelector filtering
  - Init container injection details
  - Fail-open policy

  ### OS Detection
  - virt-inspector XML output
  - Boot disk identification across multiple PVC volumes
  - OS family detection (linux vs windows)

  ### Linux IP Rewrite (Augeas)
  - RHEL 7: ifcfg format
  - RHEL 8: ifcfg or NM keyfile (auto-detected)
  - RHEL 9/10: NM keyfile format

  ### Windows IP Rewrite (hivex)
  - SYSTEM registry hive location
  - Active ControlSet discovery
  - Adapter GUID matching
  - Registry value types (REG_MULTI_SZ, REG_DWORD)

  ### Migration Detection
  - kubevirt.io/migrationJobLabel check
  - Why migration pods must not be modified

## Components
  - Table: webhook server, init container image, entrypoint script, handlers

## Supported Guest Operating Systems
  - OS matrix table (OS, versions, architecture, config method)
```

#### Mermaid Sequence Diagram — Required Content

The epic spec (AC1) requires a specific Mermaid sequence diagram. Here is the flow to diagram:

```
Participants:
  User, VirtualMachine, KubeAPI (kube-apiserver), Webhook (IP Rewrite Webhook),
  VirtLauncher (virt-launcher Pod), InitContainer (ip-rewrite init container),
  GuestFS (guestfish/virt-inspector)

Flow:
  1. User annotates VM with soteria.io/eth0-ip and labels soteria.io/ip-rewrite: "true"
  2. User stops and starts VM (or VM is started by Soteria DRExecution)
  3. KubeVirt creates virt-launcher Pod (inherits VM annotations/labels)
  4. KubeAPI intercepts CREATE (objectSelector matches label)
  5. KubeAPI sends admission request to Webhook
  6. Webhook parses annotations → env vars
  7. Webhook detects PVC volumes
  8. Webhook injects init container (ip-rewrite) with env vars + volume mounts
  9. Webhook returns JSON patch to KubeAPI
  10. KubeAPI creates mutated Pod
  11. Init container starts (before QEMU)
  12. Init container scans disks with virt-inspector
  13. Init container detects OS (Linux/Windows)
  14. Init container dispatches to correct handler
  15. Handler rewrites IP config on guest filesystem
  16. Init container exits 0
  17. virt-launcher starts QEMU → VM boots with new IP
```

Use the Mermaid `sequenceDiagram` syntax consistent with `docs/architecture/overview.md`:
- Participant aliases for readability
- `Note over` blocks for important context
- `alt`/`else` for Linux vs Windows path

#### Usage Page Structure — `docs/usage/ip-rewrite.md`

Follow the pattern from `docs/usage/failover.md` and `docs/installation/helm.md`. Recommended structure:

```
# IP Rewrite Usage Guide

  (1-2 paragraph overview)

## Prerequisites
  !!! info callout listing: cert-manager, OCP Virt or KubeVirt,
  the IP rewrite webhook deployed via Helm

## Installation
  helm install command with namespace, chart path
  Link to docs/installation/helm.md for general Helm setup
  Link forward to docs/reference/ip-rewrite.md for all values (Story 18.11)

## Configuring IP Rewrite

  ### Single-NIC Rewrite
  Step 1: Add the label
  Step 2: Add the IP annotation
  Step 3: Stop and start the VM
  Step 4: Verify
  (kubectl commands and YAML at each step)

  ### Multi-NIC Rewrite
  (Same pattern, two annotations)

  ### DNS Configuration
  Optional soteria.io/dns annotation

## Verifying the Rewrite
  ### Checking Init Container Logs
  kubectl logs <pod> -c ip-rewrite
  ### Checking Guest Agent Output
  virtctl guestosinfo <vm>

## Troubleshooting

  ### SCC Issues
  !!! warning — SYS_ADMIN capability required, check SCC binding

  ### Guestfish Appliance Errors
  LIBGUESTFS_BACKEND, /dev/kvm availability

  ### Unsupported Guest OS
  Error message format, supported OS list link

  ### Webhook Unavailable
  Fail-open behavior, how to verify webhook is running

  ### Migration Pods Skip IP Rewrite
  Expected behavior, not a bug
```

#### Cross-Links to Add to `docs/architecture/overview.md`

Add a row to the "Other Components" table (or create a new section) mentioning the IP rewrite webhook as a standalone add-on. Pattern from the existing table in overview.md:

```markdown
| **IP Rewrite Webhook** | `cmd/ip-rewrite-webhook` | Standalone mutating webhook that injects an init container into virt-launcher pods for offline guest filesystem IP reconfiguration. See [IP Rewrite Architecture](ip-rewrite.md). |
```

Do NOT restructure the existing overview page. Only add a small reference.

#### Annotation & Label Contract — Canonical Reference

Use these exact values in all documentation (sourced from Epic 18 spec):

**Label (webhook filter + opt-in):**
```yaml
soteria.io/ip-rewrite: "true"
```

**Annotations (per-interface IP configuration):**
```yaml
soteria.io/<interface>-ip: "<address>/<prefix>;<gateway>"
```

**Examples:**
```yaml
labels:
  soteria.io/ip-rewrite: "true"
annotations:
  soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"
  soteria.io/eth1-ip: "192.168.1.50/16;192.168.1.1"
  soteria.io/dns: "10.0.2.10,10.0.2.11"
```

**DNS annotation:** Optional. If absent, DNS is left untouched. If present, applies to all interfaces. Comma-separated DNS server addresses.

#### Supported Guest OS Matrix — Canonical Reference

| OS | Versions | Architecture | Config Method | Config Path |
|----|----------|-------------|---------------|-------------|
| RHEL 7 | 7.x | x86_64 | `ifcfg-*` via Augeas | `/etc/sysconfig/network-scripts/ifcfg-<iface>` |
| RHEL 8 | 8.x | x86_64 | `ifcfg-*` or NM keyfile via Augeas | Auto-detected: NM keyfile preferred, ifcfg fallback |
| RHEL 9 | 9.x | x86_64 | NM keyfile via Augeas | `/etc/NetworkManager/system-connections/<conn>.nmconnection` |
| RHEL 10 | 10.x | x86_64 | NM keyfile via Augeas | `/etc/NetworkManager/system-connections/<conn>.nmconnection` |
| Windows Server 2016 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2019 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2022 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows Server 2025 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows 10 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |
| Windows 11 | — | x86_64 | Registry hive via hivex | `<systemroot>\system32\config\system` |

#### Known Limitations to Document

Include these in both architecture and usage pages where relevant:
- **IPv6**: Not supported (deferred)
- **DHCP-to-static**: Not supported — source VM must already have a static IP
- **Hostname rewrite**: Not supported (deferred)
- **ARM64 guests**: Not supported (deferred until OCP Virt certifies ARM Windows guests)
- **x86_64 only**: Init container image is single-arch

### Project Structure Notes

**New files created by this story:**

```
docs/
├── architecture/
│   └── ip-rewrite.md          ← NEW — Architecture page (Task 1)
└── usage/
    └── ip-rewrite.md          ← NEW — Usage guide page (Task 2)
```

**Files modified by this story:**

```
docs/
└── architecture/
    └── overview.md            ← MODIFIED — Add IP rewrite cross-link (Task 3)
```

**Files NOT modified by this story (Story 18.11 scope):**

- `mkdocs.yml` — Nav update is Story 18.11
- `docs/index.md` — Landing page update is Story 18.11
- `docs/reference/ip-rewrite.md` — Reference page is Story 18.11

**Verification command:**
```bash
mkdocs build --strict
```
Run from repo root. Must complete with zero warnings and zero errors.

### Build Verification

To validate AC4, install mkdocs-material if not already present and build:

```bash
pip install mkdocs-material pymdown-extensions
mkdocs build --strict
```

The `--strict` flag treats warnings as errors. Common failures:
- Broken cross-links (wrong relative path)
- Invalid Mermaid syntax (unclosed blocks, wrong participant names)
- Missing referenced pages (only catches pages in nav — since we don't update nav, only cross-links within our pages will be checked)

### Epic 17 Code-Driven Documentation Methodology

Epic 17 established the documentation methodology for this project. Key principles inherited:
- **Start from story specs**: Use implementation artifact specs as the source of truth for technical details
- **Verify against implemented code**: Cross-reference specs with actual code when code exists
- **mkdocs-material conventions**: Consistent admonition types, heading levels, code block languages
- **Cross-link generously**: Every page should link to related architecture, usage, and reference pages
- **No orphan pages**: Every page must be reachable from the nav (Story 18.11 handles this)

### References

- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18" (~line 4198)]
- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Story 18.5 spec: `_bmad-output/implementation-artifacts/18-5-mutating-webhook-virt-launcher-init-container-injection.md`]
- [Existing architecture overview: `docs/architecture/overview.md`]
- [Existing DR lifecycle page: `docs/architecture/dr-lifecycle.md`]
- [Existing usage failover page: `docs/usage/failover.md`]
- [Existing installation Helm page: `docs/installation/helm.md`]
- [Existing API reference pattern: `docs/reference/api/drplan.md`]
- [mkdocs.yml: `mkdocs.yml` — nav structure, extensions, theme config]
- [Epic 17 documentation stories: Sprint status entries 17-1 through 17-22]

### Anti-Patterns / DO NOT

- **DO NOT update `mkdocs.yml` nav** — that is Story 18.11's scope. Create the pages at the correct paths; 18.11 will add nav entries.
- **DO NOT create `docs/reference/ip-rewrite.md`** — the reference page (annotation table, Helm values, OS matrix) is Story 18.11.
- **DO NOT update `docs/index.md`** — the landing page mention is Story 18.11.
- **DO NOT document Helm `values.yaml` parameters** — that level of detail belongs in the reference page (Story 18.11). The usage page should cover basic `helm install` only.
- **DO NOT document build/CI processes** — build instructions for the container image or webhook binary belong in `contributing/dev-setup.md`, not in user-facing docs.
- **DO NOT use H4 (`####`) or deeper headings** — existing pages use H1/H2/H3 only. Keep consistent.
- **DO NOT add custom CSS or JavaScript** — the mkdocs-material theme handles all styling.
- **DO NOT create a separate page for the init container image** — document it as a component within the architecture page. One page per concern (architecture vs usage), not per component.
- **DO NOT invent new admonition types** — use only `!!! tip`, `!!! warning`, `!!! note`, `!!! info` as established in existing docs.
- **DO NOT document internal implementation details** — the audience is users/platform engineers, not developers. Focus on "what it does" and "how to use it", not "how the Go code works".
- **DO NOT duplicate the full supported OS matrix in both pages** — put the full matrix in the architecture page, and in the usage page link to it or include a brief summary. The canonical matrix will be in the reference page (Story 18.11).
- **DO NOT document Stories 18.2–18.4 script internals** — document the *user-visible behavior* (OS detection, config editing) without referencing script filenames or internal implementation.

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

Claude Opus 4.6

### Debug Log References

No debug issues encountered.

### Completion Notes List

- Architecture page (`docs/architecture/ip-rewrite.md`) created with all AC1 content: overview, annotation/label contract with tabbed YAML examples, full Mermaid sequence diagram, webhook interception flow, OS detection, Linux (Augeas) and Windows (hivex) rewrite paths, migration detection, fail-open policy, components table, supported OS matrix, and known limitations.
- Usage guide page (`docs/usage/ip-rewrite.md`) created with all AC2 content: prerequisites, Helm installation, single-NIC and multi-NIC step-by-step with kubectl commands, DNS configuration, verification (init container logs + guest agent), and troubleshooting (SCC, guestfish, unsupported OS, webhook unavailable, migration skip).
- Both pages follow existing mkdocs-material conventions (AC3): H1/H2/H3 hierarchy, admonitions (`!!! warning`, `!!! note`, `!!! info`, `!!! tip`), tabbed content (`=== "Tab"`), YAML/bash code blocks, relative cross-links, and table formatting matching `docs/architecture/overview.md` and `docs/usage/failover.md`.
- Cross-link added to `docs/architecture/overview.md` Other Components table (Task 3).
- `mkdocs build --strict` passes with zero warnings and zero errors (AC4).
- `mkdocs.yml` nav NOT modified (Story 18.11 scope).
- All existing tests pass — zero regressions (documentation-only story, no Go code changes).

### File List

- `docs/architecture/ip-rewrite.md` — NEW — Architecture page
- `docs/usage/ip-rewrite.md` — NEW — Usage guide page
- `docs/architecture/overview.md` — MODIFIED — Added IP Rewrite Webhook row to Other Components table
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — MODIFIED — 18-10 status → review
- `_bmad-output/implementation-artifacts/18-10-documentation-architecture-and-usage.md` — MODIFIED — Tasks checked, status → review
