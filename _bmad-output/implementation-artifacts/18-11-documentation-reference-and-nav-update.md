# Story 18.11: Documentation — Reference & Nav Update

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a user,
I want a reference page for the IP rewrite annotation contract and Helm values, and the documentation navigation updated to include all IP rewrite pages,
so that I can quickly look up annotation syntax, supported OSes, and chart configuration.

## Acceptance Criteria

### AC1: Reference page
Given a new page at `docs/reference/ip-rewrite.md`
When published
Then it contains:
- Complete annotation and label reference table (name, type, required/optional, format, example)
- Supported guest OS matrix (OS, versions, architecture, config method, config file paths)
- SCC requirements and capabilities
- Helm `values.yaml` reference for `charts/soteria-ip-rewrite/` (all configurable parameters with defaults and descriptions)
- Known limitations and deferred features (IPv6, DHCP, hostname, ARM64)

### AC2: mkdocs.yml navigation updated
Given the `mkdocs.yml` file
When the `nav` section is updated
Then the IP rewrite pages appear in the appropriate sections:
- `Architecture` → `IP Rewrite` (after existing architecture pages)
- `Usage` → `IP Rewrite` (after existing usage pages)
- `Reference` → `IP Rewrite` (after existing reference pages)

### AC3: Landing page updated
Given the landing page at `docs/index.md`
When the feature list or link map is reviewed
Then IP rewrite is mentioned as a standalone add-on feature
And links to the architecture and usage pages are included

### AC4: Docs build clean
Given all new and modified documentation files
When `mkdocs build --strict` runs
Then the build completes with zero warnings and zero errors

## Tasks / Subtasks

- [x] Task 1: Create `docs/reference/ip-rewrite.md` — Reference page (AC: 1, 4)
  - [x] 1.1: Write annotation and label reference table — name, type, required/optional, format, example for each annotation
  - [x] 1.2: Write supported guest OS matrix table — OS, versions, architecture, config method, config file paths
  - [x] 1.3: Write SCC requirements section — capability, volume types, binding mechanism
  - [x] 1.4: Write Helm `values.yaml` reference tables — all parameters from `charts/soteria-ip-rewrite/values.yaml` with defaults and descriptions, organized by section (Global, Webhook, Init Container, TLS, SCC, Webhook Config)
  - [x] 1.5: Write known limitations and deferred features section
- [x] Task 2: Update `mkdocs.yml` nav to include all three IP rewrite pages (AC: 2, 4)
  - [x] 2.1: Add `IP Rewrite: architecture/ip-rewrite.md` under `Architecture` section after `Storage Drivers`
  - [x] 2.2: Add `IP Rewrite: usage/ip-rewrite.md` under `Usage` section after `Executing Failover` (before UI Guides)
  - [x] 2.3: Add `IP Rewrite: reference/ip-rewrite.md` under `Reference` section after `Helm Values`
- [x] Task 3: Update `docs/index.md` landing page (AC: 3, 4)
  - [x] 3.1: Add an IP Rewrite card to the "Key Capabilities" grid (standalone add-on feature)
  - [x] 3.2: Add IP Rewrite row to the "Architecture at a Glance" component table
  - [x] 3.3: Add IP Rewrite entry to the "Documentation Guide" table with links
- [x] Task 4: Verify docs build without errors (AC: 4)
  - [x] 4.1: Run `mkdocs build --strict` and confirm zero warnings and zero errors

## Dev Notes

### Story Intelligence Chain

#### Predecessor: Story 18.6 — Helm Sub-Chart & SCC

Story 18.6 defines the Helm chart that this reference page must document. Key context:

- **Chart location**: `charts/soteria-ip-rewrite/` — standalone chart, sibling to `charts/soteria/`
- **Two separate images** in `values.yaml`:
  - `webhook.image.*` — webhook server binary (Go), deployed by this chart's Deployment
  - `initContainer.image.*` — guestfs-tools init container, injected by webhook into virt-launcher pods
- **SCC is conditional**: `scc.enabled: true` by default (OpenShift), set `false` for vanilla K8s
- **values.yaml schema** (canonical, from 18.6 spec):

```yaml
nameOverride: ""
fullnameOverride: ""

webhook:
  image:
    repository: quay.io/raffaelespazzoli/soteria-ip-rewrite-webhook
    tag: ""          # defaults to Chart.appVersion
    pullPolicy: IfNotPresent
  replicas: 2
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi

initContainer:
  image:
    repository: quay.io/raffaelespazzoli/soteria-ip-rewrite
    tag: ""          # defaults to Chart.appVersion

tls:
  issuerRef:
    name: ""         # empty → use self-signed issuer created by chart
    kind: Issuer
  createSelfSigned: true

scc:
  enabled: true
  additionalSubjects: []
  namespaces: []

webhookConfig:
  failurePolicy: Ignore
  namespaceSelector: {}
```

- **cert-manager integration**: Self-signed Issuer by default; parent Soteria chart overrides with `tls.issuerRef.name: soteria-ca`
- **Sub-chart integration**: `ipRewrite.enabled: false` in parent `charts/soteria/values.yaml`; uses `file://../soteria-ip-rewrite` as dependency

**Note**: Story 18.6 status is `created` (spec written, not yet implemented). The reference page documents the *designed* values.yaml schema from the spec. If the chart doesn't exist yet when this story runs, base documentation on the 18.6 spec — it is the authoritative source.

#### Predecessor: Story 18.10 — Documentation: Architecture & Usage

Story 18.10 creates the pages that this story adds to navigation. Key context:

- **Architecture page**: `docs/architecture/ip-rewrite.md` — mutating webhook flow, OS detection, Augeas/hivex paths, Mermaid diagram, component table
- **Usage page**: `docs/usage/ip-rewrite.md` — prerequisites, Helm install, single/multi-NIC annotation, verification, troubleshooting
- **Story 18.10 explicitly defers to this story**: nav update, reference page, landing page update, and Helm values documentation
- **Cross-links from 18.10 pages**: The usage page links forward to `docs/reference/ip-rewrite.md` for Helm values detail. The architecture page contains the full OS matrix; the reference page should include it as the canonical lookup table

**Note**: Story 18.10 status is `ready-for-dev` (spec written, not yet implemented). The pages may or may not exist when this story runs. If they don't exist yet, create the reference page and nav entries anyway — `mkdocs build --strict` will fail on broken nav entries pointing to missing files. **If the architecture and usage pages don't exist, create minimal stub files** at `docs/architecture/ip-rewrite.md` and `docs/usage/ip-rewrite.md` with a placeholder heading so nav entries resolve. Add a comment noting they are stubs for Story 18.10.

#### Stories 18.1 and 18.5 — Annotation & Label Contract

The annotation/label contract is defined in the Epic 18 spec and refined in Stories 18.1 and 18.5:

- **Label** (webhook filter + opt-in): `soteria.io/ip-rewrite: "true"` — set on VirtualMachine. KubeVirt propagates to VMI and virt-launcher pod
- **Annotations** (per-interface IP): `soteria.io/<interface>-ip: "<address>/<prefix>;<gateway>"` — one per NIC
- **DNS annotation** (optional, global): `soteria.io/dns: "10.0.2.10,10.0.2.11"` — comma-separated, applies to all interfaces
- **Annotation→env var**: `soteria.io/eth0-ip` → `SOTERIA_ETH0_IP`, `soteria.io/dns` → `SOTERIA_DNS`
- **Migration skip label**: `kubevirt.io/migrationJobLabel` — presence causes webhook to skip injection

### Critical Technical Details

#### mkdocs-material Framework

The project uses mkdocs-material with these key settings relevant to this story:

| Setting | Value | Impact |
|---------|-------|--------|
| `pymdownx.superfences` + custom fence | `name: mermaid` | Mermaid diagrams via ` ```mermaid ` |
| `admonition` + `pymdownx.details` | Enabled | Use `!!! tip`, `!!! warning`, `!!! note`, `!!! info` |
| `pymdownx.tabbed` | `alternate_style: true` | Use `=== "Tab Title"` for tabbed content |
| `content.code.copy` | Enabled | Code blocks get copy buttons automatically |
| `toc.permalink` | `true` | Heading anchors auto-generated |

#### Current mkdocs.yml Nav Structure

The nav must be updated precisely. Current structure:

```yaml
nav:
  - Home: index.md
  - Installation:
      - Prerequisites: installation/prerequisites.md
      - ScyllaDB & Network:
          - Overview: installation/scylladb/overview.md
          - Submariner: installation/scylladb/submariner.md
          - Cilium Cluster Mesh: installation/scylladb/cilium.md
      - Helm Installation: installation/helm.md
  - Architecture:
      - Overview: architecture/overview.md
      - DR Lifecycle: architecture/dr-lifecycle.md
      - Storage Drivers: architecture/storage-drivers.md
      # ADD HERE: - IP Rewrite: architecture/ip-rewrite.md
  - Usage:
      - Creating a DRPlan: usage/creating-a-drplan.md
      - Waves & Throttling: usage/waves.md
      - Volume Grouping: usage/volumes.md
      - Executing Failover: usage/failover.md
      # ADD HERE: - IP Rewrite: usage/ip-rewrite.md
      - UI Guides:
          - Dashboard: usage/ui-guide/dashboard.md
          - Plan Detail: usage/ui-guide/plan-detail.md
          - Execution Monitor: usage/ui-guide/execution-monitor.md
  - Reference:
      - API Reference:
          - DRPlan: reference/api/drplan.md
          - DRExecution: reference/api/drexecution.md
      - Helm Values: reference/helm-values.md
      # ADD HERE: - IP Rewrite: reference/ip-rewrite.md
  - Contributing:
      - Developer Setup: contributing/dev-setup.md
      - Writing Storage Drivers: contributing/storage-drivers.md
```

**Placement rules:**
- Architecture: after `Storage Drivers` (last current entry) — IP Rewrite is a standalone add-on, listed last
- Usage: after `Executing Failover` and BEFORE `UI Guides` — IP Rewrite is a usage topic, not a UI guide
- Reference: after `Helm Values` — IP Rewrite reference is a top-level entry alongside `Helm Values`, NOT nested under `API Reference`

#### Reference Page Structure — `docs/reference/ip-rewrite.md`

Follow the existing reference page patterns from `docs/reference/helm-values.md` (section-organized tables with `---` separators) and `docs/reference/api/drplan.md` (field tables with Name/Type/Required/Default/Description columns).

Recommended structure:

```
# IP Rewrite Reference

  (1-2 sentence overview — quick-lookup reference for the IP rewrite feature)

## Annotations & Labels

  ### Label
  Table: key, value, applies to, description

  ### IP Annotations
  Table: key, type, required/optional, format, example, description

  ### DNS Annotation
  Table: same format

  !!! info callout with full YAML example

## Supported Guest Operating Systems

  Table: OS, versions, architecture, config method, config path

  !!! warning callout for unsupported OSes

## SecurityContextConstraints

  ### Required Capabilities
  Table: capability, reason

  ### SCC Volume Types
  List of permitted volume types

  ### Binding Mechanism
  Explanation of ClusterRole/ClusterRoleBinding for SCC "use"

## Helm Values Reference

  ### Global
  Table (matching helm-values.md style): Key, Type, Default, Description

  ### Webhook
  Table: webhook.image.*, webhook.replicas, webhook.resources.*

  ### Init Container
  Table: initContainer.image.*

  ### TLS
  Table: tls.issuerRef.*, tls.createSelfSigned

  ### SecurityContextConstraints
  Table: scc.enabled, scc.additionalSubjects, scc.namespaces

  ### Webhook Configuration
  Table: webhookConfig.failurePolicy, webhookConfig.namespaceSelector

## Known Limitations

  Bullet list of deferred features
```

#### Landing Page Structure — `docs/index.md`

The landing page uses mkdocs-material grid cards for "Key Capabilities". Add a new card for IP Rewrite:

```markdown
-   :material-ip-network:{ .lg .middle } **Guest IP Rewrite**

    ---

    Standalone mutating webhook that rewrites VM network configuration offline before boot. Supports RHEL 7–10 and Windows Server 2016–2025 via guestfs-tools.

    [:octicons-arrow-right-24: Architecture](architecture/ip-rewrite.md) · [:octicons-arrow-right-24: Usage](usage/ip-rewrite.md)
```

Add to the "Architecture at a Glance" table:

```markdown
| **IP Rewrite Webhook** | Standalone mutating webhook that injects an init container into virt-launcher pods for offline guest filesystem IP reconfiguration. Supports RHEL (Augeas) and Windows (hivex). |
```

Add to the "Documentation Guide" table:

```markdown
| [**IP Rewrite**](architecture/ip-rewrite.md) | Standalone guest filesystem IP rewrite — architecture, usage guide, annotation & Helm reference |
```

#### Annotation Reference Table — Canonical Data

| Key | Type | Required | Applies To | Format | Example | Description |
|-----|------|----------|-----------|--------|---------|-------------|
| `soteria.io/ip-rewrite` | Label | **Yes** | VirtualMachine | `"true"` | `soteria.io/ip-rewrite: "true"` | Opt-in label. Enables webhook interception via objectSelector. KubeVirt propagates to VMI and virt-launcher pod. |
| `soteria.io/<interface>-ip` | Annotation | **Yes** (≥1) | VirtualMachine | `<addr>/<prefix>;<gateway>` | `soteria.io/eth0-ip: "10.0.2.100/24;10.0.2.1"` | Per-interface IP configuration. Interface name matches guest OS NIC name. |
| `soteria.io/dns` | Annotation | No | VirtualMachine | Comma-separated IPs | `soteria.io/dns: "10.0.2.10,10.0.2.11"` | Global DNS servers. Applies to all interfaces. If absent, DNS is left untouched. |

#### Supported OS Matrix — Canonical Data

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

#### Helm Values Reference — Canonical Data

Document every parameter from the 18.6 spec values.yaml. Organize tables by section matching `docs/reference/helm-values.md` style:

**Global:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `nameOverride` | string | `""` | Override the chart name used in resource names and labels. Truncated to 63 characters. |
| `fullnameOverride` | string | `""` | Override the full release name used in resource names. |

**Webhook:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `webhook.image.repository` | string | `"quay.io/raffaelespazzoli/soteria-ip-rewrite-webhook"` | Container image for the IP rewrite webhook server. |
| `webhook.image.tag` | string | `""` | Image tag. When empty, defaults to the chart's `appVersion`. |
| `webhook.image.pullPolicy` | string | `"IfNotPresent"` | Kubernetes image pull policy. |
| `webhook.replicas` | integer | `2` | Number of webhook server pods. |
| `webhook.resources.requests.cpu` | string | `"100m"` | CPU request. |
| `webhook.resources.requests.memory` | string | `"128Mi"` | Memory request. |
| `webhook.resources.limits.cpu` | string | `"500m"` | CPU limit. |
| `webhook.resources.limits.memory` | string | `"256Mi"` | Memory limit. |

**Init Container:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `initContainer.image.repository` | string | `"quay.io/raffaelespazzoli/soteria-ip-rewrite"` | Container image for the IP rewrite init container (guestfs-tools). Injected into virt-launcher pods by the webhook. |
| `initContainer.image.tag` | string | `""` | Image tag. When empty, defaults to the chart's `appVersion`. |

**TLS:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `tls.issuerRef.name` | string | `""` | cert-manager Issuer or ClusterIssuer name. When empty, the chart creates a self-signed Issuer. |
| `tls.issuerRef.kind` | string | `"Issuer"` | Kind of the cert-manager issuer — `"Issuer"` or `"ClusterIssuer"`. |
| `tls.createSelfSigned` | boolean | `true` | When `true` and `tls.issuerRef.name` is empty, create a self-signed Issuer. When `false`, `tls.issuerRef.name` must be set. |

**SecurityContextConstraints:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `scc.enabled` | boolean | `true` | Create OpenShift SCC resources. Set to `false` on vanilla Kubernetes (no SCC API). |
| `scc.additionalSubjects` | list | `[]` | Additional RBAC subjects granted SCC `use` permission beyond the default namespace binding. |
| `scc.namespaces` | list | `[]` | Namespaces whose ServiceAccounts can use the SCC. Must include namespaces where VMs run. Empty = release namespace only. |

**Webhook Configuration:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `webhookConfig.failurePolicy` | string | `"Ignore"` | MutatingWebhookConfiguration failure policy. `"Ignore"` (fail-open) or `"Fail"` (fail-closed). |
| `webhookConfig.namespaceSelector` | object | `{}` | Label selector for namespaces the webhook applies to. Empty = all namespaces. |

#### Known Limitations — Canonical Data

- **IPv6**: Not supported (deferred)
- **DHCP-to-static conversion**: Not supported — source VM must already have a static IP
- **Guest hostname rewrite**: Not supported (deferred)
- **ARM64 guests**: Not supported (deferred until OCP Virt certifies ARM Windows guests)
- **x86_64 only**: Both the init container image and webhook server are single-arch `linux/amd64`

#### Cross-Link Patterns

Use relative links consistent with existing documentation:
- From reference page to architecture: `[IP Rewrite Architecture](../architecture/ip-rewrite.md)`
- From reference page to usage: `[IP Rewrite Usage Guide](../usage/ip-rewrite.md)`
- From reference page to main Helm values: `[Soteria Helm Values](helm-values.md)`
- From landing page to architecture: `[Architecture](architecture/ip-rewrite.md)`

#### SCC Section Content

The SCC section should document (sourced from 18.6 spec):

- **Required capability**: `SYS_ADMIN` — needed for the guestfish appliance to launch its internal QEMU/KVM instance in the init container
- **Allowed volume types**: `persistentVolumeClaim`, `secret`, `configMap`, `projected`, `emptyDir`
- **Denied host access**: `allowHostDirVolumePlugin: false`, `allowHostNetwork: false`, `allowHostPorts: false`, `allowHostPID: false`, `allowHostIPC: false`, `allowPrivilegedContainer: false`
- **Binding mechanism**: ClusterRole granting `use` verb on the SCC → ClusterRoleBinding to ServiceAccounts. Default binding: all SAs in the release namespace. Configurable via `scc.namespaces` and `scc.additionalSubjects`

### Existing Page Patterns to Follow

Study these for consistent style:

| Pattern | Example File | What to Replicate |
|---------|-------------|-------------------|
| Reference tables (field-level) | `docs/reference/api/drplan.md` | Field tables: Name / Type / Required / Default / Description |
| Reference tables (Helm values) | `docs/reference/helm-values.md` | Section-organized tables: Key / Type / Default / Description with `---` separators and `!!! info` callouts |
| Landing page grid cards | `docs/index.md` | `:material-icon:{ .lg .middle }` card format, `---` separator, brief description + links |
| Landing page component table | `docs/index.md` | Component / Role two-column table |

**Heading conventions:**
- H1 (`#`): Page title — one per page
- H2 (`##`): Major sections
- H3 (`###`): Subsections
- No H4+ unless absolutely necessary

**Code block conventions:**
- YAML blocks: ` ```yaml `
- Shell commands: ` ```bash `

### Build Verification

```bash
# From repo root
mkdocs build --strict
```

Install mkdocs-material if not present:
```bash
pip install mkdocs-material pymdown-extensions
```

The `--strict` flag treats warnings as errors. Common failures to watch for:
- **Broken nav entries**: Nav references a file that doesn't exist → ERROR
- **Broken cross-links**: Relative links to non-existent pages → WARNING (promoted to error by `--strict`)
- **Invalid YAML in mkdocs.yml**: Indentation errors in nav → parse failure

**If Story 18.10 pages don't exist**: Create minimal stubs (see Anti-Patterns section) so nav entries resolve.

### Project Structure Notes

**New files created by this story:**

```
docs/
└── reference/
    └── ip-rewrite.md          ← NEW — Reference page (Task 1)
```

**Files modified by this story:**

```
mkdocs.yml                     ← MODIFIED — Add 3 nav entries (Task 2)
docs/index.md                  ← MODIFIED — Add IP Rewrite feature card + table entries (Task 3)
```

**Potentially created (stubs only, if 18.10 pages don't exist yet):**

```
docs/architecture/ip-rewrite.md   ← STUB if missing (for nav to resolve)
docs/usage/ip-rewrite.md          ← STUB if missing (for nav to resolve)
```

### Anti-Patterns / DO NOT

- **DO NOT create the architecture or usage pages with full content** — that is Story 18.10's scope. If the pages don't exist, create **minimal stubs** with a single H1 heading and a placeholder note. Example stub:
  ```markdown
  # IP Rewrite Architecture

  !!! note "Placeholder"
      This page will be populated by Story 18.10.
  ```
- **DO NOT restructure the existing mkdocs.yml nav** — only add IP Rewrite entries at the specified positions. Do not reorder, rename, or restructure existing entries.
- **DO NOT restructure the existing `docs/index.md` layout** — only add new entries (card, table rows). Do not modify existing cards, table rows, Mermaid diagrams, or text.
- **DO NOT nest IP Rewrite under the `API Reference` sub-nav** — it is a top-level entry under `Reference`, at the same level as `Helm Values`.
- **DO NOT duplicate Helm values from the main Soteria chart** — only document `charts/soteria-ip-rewrite/values.yaml` parameters. For the parent chart's `ipRewrite.enabled` integration, add a brief note with a link to the main Helm Values page.
- **DO NOT document build/CI processes** — build instructions belong in contributing docs, not user-facing reference.
- **DO NOT use H4 (`####`) or deeper headings** — existing pages use H1/H2/H3 only.
- **DO NOT add custom CSS or JavaScript** — mkdocs-material theme handles all styling.
- **DO NOT invent new admonition types** — use only `!!! tip`, `!!! warning`, `!!! note`, `!!! info`.
- **DO NOT modify `docs/architecture/overview.md`** — Story 18.10 handles that cross-link. This story only touches `mkdocs.yml`, `docs/index.md`, and creates `docs/reference/ip-rewrite.md`.
- **DO NOT document internal script implementation details** — the reference page audience is users/platform engineers. Document annotations, OS support, Helm config, and SCC — not script internals.
- **DO NOT modify `.github/workflows/` or `Makefile`** — out of scope.

### References

- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18" (~line 4198)]
- [Story 18.6 spec: `_bmad-output/implementation-artifacts/18-6-helm-sub-chart-and-scc.md` — values.yaml schema, SCC details, chart structure]
- [Story 18.10 spec: `_bmad-output/implementation-artifacts/18-10-documentation-architecture-and-usage.md` — pages this story navigates to]
- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md` — init container image details]
- [Story 18.5 spec: `_bmad-output/implementation-artifacts/18-5-mutating-webhook-virt-launcher-init-container-injection.md` — annotation contract, webhook behavior]
- [Existing Helm values reference: `docs/reference/helm-values.md` — table format pattern to follow]
- [Existing API reference: `docs/reference/api/drplan.md` — field table format pattern]
- [Existing landing page: `docs/index.md` — grid card and table patterns]
- [mkdocs.yml: `mkdocs.yml` — current nav structure, extensions, theme config]
- [Existing usage page: `docs/usage/failover.md` — step-by-step format, admonition style]

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

Claude Opus 4.6 (Cursor)

### Debug Log References

No debug issues encountered — documentation-only story.

### Completion Notes List

- Created `docs/reference/ip-rewrite.md` with complete annotation/label reference tables, supported guest OS matrix, SCC requirements, full Helm values reference (all parameters from actual `charts/soteria-ip-rewrite/values.yaml`), and known limitations section.
- Helm values documented from actual `values.yaml` — includes `scc.create`, `scc.serviceAccountNames` fields present in implementation but not in original spec. `scc.enabled` defaults to `false` (actual) not `true` (spec).
- Updated `mkdocs.yml` nav with three entries: Architecture → IP Rewrite (after Storage Drivers), Usage → IP Rewrite (after Executing Failover, before UI Guides), Reference → IP Rewrite (after Helm Values, top-level not nested under API Reference).
- Updated `docs/index.md`: added Guest IP Rewrite grid card to Key Capabilities, added IP Rewrite Webhook row to Architecture at a Glance table, added IP Rewrite entry to Documentation Guide table.
- `mkdocs build --strict` passes with zero warnings and zero errors.
- Migration skip label is `kubevirt.io/migrationJobUID` (from `virtv1.MigrationJobLabel` constant in handler.go).
- Init container base image is CentOS Stream 9 (confirmed from `build/ip-rewrite/Containerfile`).
- Architecture and usage pages from Story 18.10 already exist — no stubs needed.

### File List

- `docs/reference/ip-rewrite.md` — NEW — Complete reference page
- `mkdocs.yml` — MODIFIED — Added 3 nav entries for IP rewrite pages
- `docs/index.md` — MODIFIED — Added grid card, component table row, documentation guide entry
