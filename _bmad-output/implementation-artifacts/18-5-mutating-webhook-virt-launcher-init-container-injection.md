# Story 18.5: Mutating Webhook — virt-launcher Init Container Injection

Status: ready-for-dev

## Story

As a developer,
I want a mutating admission webhook that intercepts virt-launcher pod creation and injects an IP rewrite init container when the appropriate label and annotations are present,
so that VM disks are modified with the correct network configuration before the VM boots.

## Acceptance Criteria

### AC1: Webhook intercepts virt-launcher pod CREATE
Given a `MutatingWebhookConfiguration` targeting `pods` with `CREATE` operations
When a pod with label `soteria.io/ip-rewrite: "true"` is created
Then the webhook handler is invoked

### AC2: Init container injection
Given the webhook receives a pod with `soteria.io/ip-rewrite: "true"` label and `soteria.io/<iface>-ip` annotations
When the handler processes the admission request
Then it injects an init container named `ip-rewrite` before all existing init containers
And the init container image is configurable (defaulting to `quay.io/raffaelespazzoli/soteria-ip-rewrite:latest`)
And the init container has environment variables derived from annotations (e.g., `SOTERIA_ETH0_IP`, `SOTERIA_DNS`)
And the init container has volume mounts for all PVC-backed volumes from the pod spec
And the init container has `securityContext.capabilities.add: [SYS_ADMIN]`

### AC3: Migration skip
Given a pod with both `soteria.io/ip-rewrite: "true"` and `kubevirt.io/migrationJobLabel` labels
When the webhook receives the admission request
Then no init container is injected (migration pods must not have their disks modified)
And the pod is admitted unmodified

### AC4: Label-based filtering via objectSelector
Given the `MutatingWebhookConfiguration` has `objectSelector.matchLabels: {"soteria.io/ip-rewrite": "true"}`
When a pod without the label is created
Then the webhook is not invoked at all (Kubernetes API server filters it out)

### AC5: Fail-open policy
Given the webhook deployment is unavailable (scaled to 0, crashed, etc.)
When a pod with the IP rewrite label is created
Then the pod is admitted unmodified (`failurePolicy: Ignore`)
And no IP rewrite occurs (degraded behavior, not blocking)

### AC6: TLS via cert-manager
Given the webhook server requires TLS
When deployed with cert-manager
Then a `Certificate` and `Issuer` are created
And the `MutatingWebhookConfiguration` has `caBundle` injected via `cert-manager.io/inject-ca-from` annotation

### AC7: Go webhook server structure
Given the webhook handler at `internal/webhook/iprewrite/handler.go`
When the server starts
Then it listens on port 9443 (configurable)
And serves the mutating webhook at `/mutate-v1-pod`
And serves a health check at `/healthz`

## Tasks / Subtasks

- [ ] Task 1: Create `cmd/ip-rewrite-webhook/main.go` with webhook server setup (AC: 6, 7)
  - [ ] 1.1: Set up controller-runtime manager with webhook server on port 9443
  - [ ] 1.2: Add TLS cert-dir flag for cert-manager mounted certificate
  - [ ] 1.3: Add `--init-container-image` flag (default `quay.io/raffaelespazzoli/soteria-ip-rewrite:latest`)
  - [ ] 1.4: Register webhook handler at `/mutate-v1-pod`
  - [ ] 1.5: Add `/healthz` and `/readyz` health probes
- [ ] Task 2: Create `internal/webhook/iprewrite/handler.go` with admission handler (AC: 1, 2, 3)
  - [ ] 2.1: Implement `Handle(ctx, admission.Request) admission.Response` — the core mutating logic
  - [ ] 2.2: Parse `soteria.io/*-ip` and `soteria.io/dns` annotations into init container env vars
  - [ ] 2.3: Detect migration pods via `kubevirt.io/migrationJobLabel` and skip injection
  - [ ] 2.4: Build init container spec with image, env vars, volume mounts, security context
  - [ ] 2.5: Prepend init container to `pod.Spec.InitContainers`
  - [ ] 2.6: Return JSON patch response
- [ ] Task 3: Implement annotation parsing and env var generation (AC: 2)
  - [ ] 3.1: `soteria.io/eth0-ip` → `SOTERIA_ETH0_IP` (strip `soteria.io/` prefix, remove trailing `-ip`, uppercase, replace `-` with `_`, re-add `_IP` suffix)
  - [ ] 3.2: `soteria.io/dns` → `SOTERIA_DNS`
  - [ ] 3.3: Ignore non-`soteria.io/` annotations and non-IP soteria annotations
- [ ] Task 4: Implement PVC volume mount injection (AC: 2)
  - [ ] 4.1: Iterate `pod.Spec.Volumes`, find volumes with `PersistentVolumeClaim` source
  - [ ] 4.2: Create corresponding `VolumeMount` entries at `/disks/<volumeName>` in the init container
- [ ] Task 5: Implement migration detection (AC: 3)
  - [ ] 5.1: Check for `kubevirt.io/migrationJobLabel` label on the pod
  - [ ] 5.2: If present, return `admission.Allowed("")` immediately — no patch
- [ ] Task 6: Create `MutatingWebhookConfiguration` manifest (AC: 1, 4, 5, 6)
  - [ ] 6.1: Create `config/ip-rewrite-webhook/mutatingwebhookconfiguration.yaml`
  - [ ] 6.2: Set `objectSelector.matchLabels: {"soteria.io/ip-rewrite": "true"}`
  - [ ] 6.3: Set `failurePolicy: Ignore`
  - [ ] 6.4: Set `sideEffects: None`
  - [ ] 6.5: Add `cert-manager.io/inject-ca-from` annotation for CA bundle injection

## Dev Notes

### Story Intelligence Chain

**Predecessor: Story 18.1 — Init Container Image: guestfs-tools on UBI9**

Story 18.1 created the container image that this webhook injects as an init container. Key context inherited:

- **Image location**: `quay.io/raffaelespazzoli/soteria-ip-rewrite:$VERSION` (default tag: `latest`)
- **Build context**: `build/ip-rewrite/Containerfile` using UBI9 base with `guestfs-tools`, `augeas`, `hivex`, `libguestfs-winsupport`
- **Runtime requirements**: `LIBGUESTFS_BACKEND=direct` is baked into the image. The init container needs `SYS_ADMIN` capability for the guestfish appliance
- **No entrypoint yet**: Story 18.1 did NOT set `ENTRYPOINT` or `CMD` — Stories 18.2–18.4 will add scripts. The webhook should set the command/entrypoint on the init container spec (or leave it to default once 18.2 adds it)
- **Package correction**: `libguestfs-winsupport` replaces `ntfs-3g` (EPEL-only). This is already in the image — no action needed here

**What 18.1 deferred to this story:** Nothing directly — 18.1 is the image, this story is the webhook that injects it. They are parallel workstreams (both depend on nothing before them, though 18.5 references the image 18.1 produces).

**Parallel stories NOT yet implemented (18.2, 18.3, 18.4):** The entrypoint script and handler scripts are NOT in the image yet. The webhook should still inject the init container with the correct env vars and volume mounts — the container will simply fail until the scripts are added. This is by design: the webhook and scripts are independent deliverables.

**Epic 18 overall context:** This epic creates a standalone component (no Soteria CRD dependencies) for VM IP rewriting via mutating webhook + init container. The webhook is the integration point between KubeVirt's virt-launcher pod lifecycle and the IP rewrite tooling.

**What this story establishes for downstream stories:**
- Story 18.6 (Helm Sub-Chart) wraps the webhook deployment, service, MWC, SCC, and cert-manager resources into a Helm chart
- Story 18.7 (Unit & Integration Tests) writes Go unit tests for `handler.go` — the handler must be testable with pure admission.Request objects (no envtest needed)
- Story 18.10 (Documentation) documents the webhook architecture

### Critical Technical Details

#### Webhook Architecture — Standalone Binary

The webhook runs as a **separate binary** (`cmd/ip-rewrite-webhook/main.go`), NOT integrated into the main Soteria manager (`cmd/soteria/main.go`). Rationale:
- The IP rewrite feature is standalone (no Soteria CRD dependency)
- It can be deployed independently via its own Helm chart (Story 18.6)
- It does not need ScyllaDB, aggregated API server, or any Soteria-specific infrastructure
- Simpler deployment: a lightweight webhook server with no controller-runtime controllers

The binary needs only:
- `sigs.k8s.io/controller-runtime` for the webhook server (already in `go.mod` at `v0.24.1`)
- `k8s.io/api` for Pod types (already in `go.mod` at `v0.36.3`)
- No additional Go module dependencies needed

#### Existing Webhook Pattern in This Project

The project has an existing validating webhook for VirtualMachines. Study these files for patterns:

| File | Pattern to Follow |
|------|-------------------|
| `pkg/admission/vm_validator.go` | `Handle(ctx, admission.Request) admission.Response` signature, JSON unmarshaling of request, structured logging |
| `pkg/admission/setup.go` | `mgr.GetWebhookServer().Register(path, &webhook.Admission{Handler: handler})` registration pattern |
| `pkg/admission/vm_validator_test.go` | Test pattern: `json.Marshal` pod → `admission.Request` construction → call `Handle` → assert response |
| `config/webhook/manifests.yaml` | Existing VWC manifest structure (but ours is a MWC, not VWC) |

**Key difference**: The existing webhook is a **validating** webhook (`admission.Allowed`/`admission.Denied`). Ours is a **mutating** webhook that returns a JSON patch. Use `admission.PatchResponseFromRaw(req.Object.Raw, marshaledMutatedPod)` from `sigs.k8s.io/controller-runtime/pkg/webhook/admission` to generate the patch response.

#### controller-runtime Admission Package API

The `sigs.k8s.io/controller-runtime/pkg/webhook/admission` package (v0.24.1) provides:

```go
// Handler interface — implement this
type Handler interface {
    Handle(ctx context.Context, req Request) Response
}

// Key functions for mutating webhooks:
admission.PatchResponseFromRaw(original, mutated []byte) Response
// Generates a JSON patch from original → mutated raw bytes

// For the standalone server (cmd/ip-rewrite-webhook/main.go):
mgr.GetWebhookServer().Register("/mutate-v1-pod", &webhook.Admission{Handler: h})
```

#### Annotation-to-Environment-Variable Transformation

```
Annotation key                    → Environment variable
─────────────────────────────────────────────────────────
soteria.io/eth0-ip                → SOTERIA_ETH0_IP
soteria.io/ens3-ip                → SOTERIA_ENS3_IP
soteria.io/my-custom-nic-ip       → SOTERIA_MY_CUSTOM_NIC_IP
soteria.io/dns                    → SOTERIA_DNS
```

**Algorithm:**
1. Filter annotations: keep only keys starting with `soteria.io/`
2. For `soteria.io/dns` → env var `SOTERIA_DNS` with the annotation value
3. For `soteria.io/<name>-ip` → strip `soteria.io/` prefix, strip trailing `-ip`, uppercase the middle part, replace `-` with `_`, prefix with `SOTERIA_`, suffix with `_IP`
4. Ignore other `soteria.io/*` annotations (e.g., `soteria.io/ip-rewrite` label, `soteria.io/drplan`, etc.)

#### PVC Volume Mount Injection

```go
// Pseudocode for volume mount injection:
for _, vol := range pod.Spec.Volumes {
    if vol.PersistentVolumeClaim != nil {
        initContainer.VolumeMounts = append(initContainer.VolumeMounts, corev1.VolumeMount{
            Name:      vol.Name,
            MountPath: "/disks/" + vol.Name,
        })
    }
}
```

The init container runs BEFORE KubeVirt's own containers, so the PVC disks are available and not yet locked by QEMU.

#### Migration Detection

KubeVirt sets `kubevirt.io/migrationJobLabel` only via `RenderMigrationManifest` (never on regular VM starts). Check:

```go
const migrationJobLabel = "kubevirt.io/migrationJobLabel"

if _, isMigration := pod.Labels[migrationJobLabel]; isMigration {
    return admission.Allowed("") // Skip injection for migration pods
}
```

#### Init Container Spec

```go
initContainer := corev1.Container{
    Name:  "ip-rewrite",
    Image: h.InitContainerImage, // configurable, default "quay.io/raffaelespazzoli/soteria-ip-rewrite:latest"
    Env:   envVars,              // derived from annotations
    VolumeMounts: volumeMounts,  // PVC volumes at /disks/<name>
    SecurityContext: &corev1.SecurityContext{
        Capabilities: &corev1.Capabilities{
            Add: []corev1.Capability{"SYS_ADMIN"},
        },
    },
}

// Prepend — must run BEFORE all other init containers
pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)
```

#### Standalone `main.go` Structure

```go
// cmd/ip-rewrite-webhook/main.go
// Minimal controller-runtime manager — webhook server only, no controllers.
package main

import (
    "flag"
    "os"

    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/healthz"
    "sigs.k8s.io/controller-runtime/pkg/log/zap"
    "sigs.k8s.io/controller-runtime/pkg/webhook"

    iprewrite "github.com/soteria-project/soteria/internal/webhook/iprewrite"
)

func main() {
    var certDir string
    var port int
    var initContainerImage string

    flag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory with TLS cert/key")
    flag.IntVar(&port, "port", 9443, "Webhook server port")
    flag.StringVar(&initContainerImage, "init-container-image",
        "quay.io/raffaelespazzoli/soteria-ip-rewrite:latest",
        "Image for the IP rewrite init container")

    opts := zap.Options{Development: true}
    opts.BindFlags(flag.CommandLine)
    flag.Parse()
    ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        WebhookServer: webhook.NewServer(webhook.Options{
            Port:    port,
            CertDir: certDir,
        }),
        HealthProbeBindAddress: ":8081",
    })
    // ... error handling, register handler, add health checks, start ...
}
```

#### File Placement — `internal/webhook/iprewrite/`

This project currently places webhook code in `pkg/admission/`. However, the IP rewrite webhook is standalone and should NOT be in `pkg/admission/` (that package is for Soteria-specific admission logic integrated into the main manager).

Place the handler in `internal/webhook/iprewrite/` following the kubebuilder multi-group convention and the epic spec's explicit guidance (AC7: "the webhook handler at `internal/webhook/iprewrite/handler.go`").

```
internal/
└── webhook/
    └── iprewrite/
        ├── handler.go       ← Mutating admission handler
        └── handler_test.go  ← Unit tests (Story 18.7, but structure established here)
```

#### MutatingWebhookConfiguration Manifest

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: soteria-ip-rewrite
  annotations:
    cert-manager.io/inject-ca-from: "${NAMESPACE}/soteria-ip-rewrite-cert"
webhooks:
  - name: ip-rewrite.soteria.io
    admissionReviewVersions: ["v1"]
    clientConfig:
      service:
        name: soteria-ip-rewrite-webhook
        namespace: system  # placeholder — Helm chart (18.6) will template this
        path: /mutate-v1-pod
    failurePolicy: Ignore
    sideEffects: None
    objectSelector:
      matchLabels:
        soteria.io/ip-rewrite: "true"
    rules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE"]
        resources: ["pods"]
```

**Key choices:**
- `failurePolicy: Ignore` — fail-open, VMs start even if webhook is down
- `objectSelector` — Kubernetes filters at the API server level; webhook is never invoked for pods without the label
- `sideEffects: None` — the webhook only patches the admission response, no external state changes
- `operations: ["CREATE"]` only — no need to intercept UPDATE/DELETE

### Project Structure Notes

**New files created by this story:**

```
cmd/
└── ip-rewrite-webhook/
    └── main.go                              ← Standalone webhook server entry point

internal/
└── webhook/
    └── iprewrite/
        └── handler.go                       ← Mutating admission handler

config/
└── ip-rewrite-webhook/
    └── mutatingwebhookconfiguration.yaml    ← MWC manifest (reference, Helm chart templates in 18.6)
```

**Alignment with existing project:**
- `cmd/ip-rewrite-webhook/` follows the pattern of `cmd/soteria/` and `cmd/console-proxy/` — separate binaries for separate concerns
- `internal/webhook/iprewrite/` is a new package. The `internal/` directory doesn't exist yet in this project (controllers are in `pkg/controller/`). However, the epic spec explicitly names this path (AC7), and it follows kubebuilder convention for webhook code that should not be importable by external packages
- The Go module is `github.com/soteria-project/soteria` — the handler import path will be `github.com/soteria-project/soteria/internal/webhook/iprewrite`

**Dockerfile for the webhook binary (Story 18.6 will create this — DO NOT create here):**
The webhook binary will need its own Dockerfile (separate from the Soteria controller Dockerfile and the IP rewrite image Containerfile). Story 18.6's Helm chart will define the deployment. This story only creates the Go code and the reference MWC manifest.

### Go Module & Dependencies

All required dependencies are already in `go.mod`:

| Dependency | Version | Used For |
|-----------|---------|----------|
| `sigs.k8s.io/controller-runtime` | `v0.24.1` | `pkg/webhook/admission` — webhook handler framework |
| `k8s.io/api` | `v0.36.3` | `core/v1` — Pod, Container, VolumeMount types |
| `k8s.io/apimachinery` | `v0.36.3` | `runtime` — scheme, raw extension |

**No new Go dependencies needed.** Do not add any.

### Testing Strategy (for Story 18.7 — but design handler for testability NOW)

The handler MUST be testable with pure `admission.Request` objects — no envtest, no running cluster needed:

```go
// Test pattern (from existing vm_validator_test.go):
pod := &corev1.Pod{...}
raw, _ := json.Marshal(pod)
req := admission.Request{
    AdmissionRequest: admissionv1.AdmissionRequest{
        Operation: admissionv1.Create,
        Object:    runtime.RawExtension{Raw: raw},
    },
}
resp := handler.Handle(ctx, req)
// Assert: resp.Allowed == true, resp.Patches contains init container injection
```

**Design the handler struct to accept `InitContainerImage` as a field** (not hardcoded) so tests can inject a test image name.

### References

- [Epic 18 full specification: `_bmad-output/planning-artifacts/epics.md` — search "Epic 18" (~line 4198)]
- [Story 18.1 spec: `_bmad-output/implementation-artifacts/18-1-init-container-image-guestfs-tools-ubi9.md`]
- [Existing VM webhook: `pkg/admission/vm_validator.go`]
- [Existing webhook setup: `pkg/admission/setup.go`]
- [Existing webhook tests: `pkg/admission/vm_validator_test.go`]
- [Main Soteria binary: `cmd/soteria/main.go` — webhook server registration pattern at lines 160-170, 397-405]
- [controller-runtime webhook/admission package: `sigs.k8s.io/controller-runtime/pkg/webhook/admission`]
- [KubeVirt migration label: `kubevirt.io/migrationJobLabel` — set only by `RenderMigrationManifest`]
- [Annotation contract: Epic 18 spec — "Annotation & Label Contract" section]

### Anti-Patterns / DO NOT

- **DO NOT integrate into `cmd/soteria/main.go`** — the webhook is a standalone binary. Do not modify the Soteria manager.
- **DO NOT add code to `pkg/admission/`** — that package is for Soteria-specific admission logic (DRPlan/DRExecution/VM validation). The IP rewrite webhook belongs in `internal/webhook/iprewrite/`.
- **DO NOT create a Helm chart** — that is Story 18.6. Only create the Go code and reference MWC manifest in `config/ip-rewrite-webhook/`.
- **DO NOT create a Dockerfile for the webhook binary** — Story 18.6 handles deployment packaging.
- **DO NOT modify `Makefile`** — build targets for the webhook binary are Story 18.9's responsibility.
- **DO NOT modify `.github/workflows/ci.yml` or `release.yml`** — CI integration is Story 18.9.
- **DO NOT modify `config/webhook/manifests.yaml`** — that is the existing Soteria VM webhook. Create a new manifest directory `config/ip-rewrite-webhook/`.
- **DO NOT add new Go module dependencies** — everything needed is already in `go.mod`.
- **DO NOT write unit tests** — Story 18.7 is responsible for tests. However, design the handler struct to be easily testable (exported fields, no global state).
- **DO NOT implement entrypoint scripts or handler scripts** — those are Stories 18.2, 18.3, 18.4.
- **DO NOT add E2E tests** — Story 18.8 covers E2E validation.
- **DO NOT create SecurityContextConstraints** — that is part of Story 18.6 (Helm chart).
- **DO NOT add namespace selector to the MWC** — Story 18.6 will template this in the Helm chart with configurable namespace filtering.

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
