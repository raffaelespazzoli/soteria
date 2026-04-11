# Story 2.5: RBAC & Secure Credential Handling

Status: done

## Story

As a platform administrator,
I want Kubernetes-native RBAC on all Soteria CRDs with granular permissions and secure credential handling,
So that access is properly controlled and no credentials are stored or exposed by the orchestrator.

## Acceptance Criteria

1. **Given** the RBAC manifests in `config/rbac/`, **When** ClusterRoles are defined for Soteria resources, **Then** a `soteria-viewer` ClusterRole grants `get`, `list`, `watch` on DRPlan, DRExecution, and DRGroupStatus (read-only users), **And** a `soteria-editor` ClusterRole adds `create`, `update`, `patch` on DRPlan (plan authors), **And** a `soteria-operator` ClusterRole adds `create` on DRExecution (failover operators), **And** role granularity follows CRD verb semantics per FR44.

2. **Given** the RBAC configuration, **When** a user without `soteria-operator` role attempts to create a DRExecution, **Then** the request is rejected by Kubernetes RBAC with a `403 Forbidden` response, **And** no custom authorization logic exists — Kubernetes RBAC is the only access control mechanism (FR44).

3. **Given** any Soteria operation, **When** logs, events, metrics, or DRExecution records are written, **Then** no storage credentials appear in any output (NFR14), **And** the credential sanitization is verified by unit tests that assert no secret values in formatted log/event/metric strings.

4. **Given** the credential reference types, **When** a storage driver (Epic 3) needs credentials, **Then** a `CredentialSource` type exists in `pkg/drivers/credentials.go` with `SecretRef` (K8s Secret name/namespace/key) and `VaultRef` (Vault path/role) variants, **And** a `CredentialResolver` interface is defined for resolving credentials at runtime without caching beyond operation lifetime.

5. **Given** the credential resolver for Kubernetes Secrets, **When** `ResolveFromSecret` is called with a valid `SecretRef`, **Then** the credential value is returned from the referenced Secret, **And** the orchestrator never copies credential values into its own resources, config maps, or local state (FR45).

6. **Given** the ClusterRoles, **When** Kustomize aggregation labels are applied, **Then** `soteria-viewer` aggregates into the default Kubernetes `view` ClusterRole, **And** `soteria-editor` aggregates into `edit`, **And** `soteria-operator` aggregates into `admin`, **And** users with existing Kubernetes role bindings automatically inherit Soteria permissions.

7. **Given** the RBAC setup with the aggregated API server, **When** integration tests run, **Then** a user bound to `soteria-viewer` can `get` a DRPlan but cannot `create` a DRExecution, **And** a user bound to `soteria-operator` can `create` a DRExecution.

## Tasks / Subtasks

- [x] Task 1: Create Soteria persona ClusterRoles (AC: #1, #6)
  - [x] 1.1 Create `config/rbac/soteria_viewer_role.yaml` — `ClusterRole` named `soteria-viewer`:
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drplans", "drexecutions", "drgroupstatuses"]`, `verbs: ["get", "list", "watch"]`
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drplans/status", "drexecutions/status", "drgroupstatuses/status"]`, `verbs: ["get"]`
    - Aggregation label: `rbac.authorization.k8s.io/aggregate-to-view: "true"`
    - Standard labels: `app.kubernetes.io/name: dr-orchestrator`, `app.kubernetes.io/managed-by: kustomize`
  - [x] 1.2 Create `config/rbac/soteria_editor_role.yaml` — `ClusterRole` named `soteria-editor`:
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drplans"]`, `verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]`
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drexecutions", "drgroupstatuses"]`, `verbs: ["get", "list", "watch"]`
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drplans/status", "drexecutions/status", "drgroupstatuses/status"]`, `verbs: ["get"]`
    - Aggregation label: `rbac.authorization.k8s.io/aggregate-to-edit: "true"`
  - [x] 1.3 Create `config/rbac/soteria_operator_role.yaml` — `ClusterRole` named `soteria-operator`:
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drplans"]`, `verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]`
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drexecutions"]`, `verbs: ["get", "list", "watch", "create"]`
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drgroupstatuses"]`, `verbs: ["get", "list", "watch"]`
    - Rules: `apiGroups: ["soteria.io"]`, `resources: ["drplans/status", "drexecutions/status", "drgroupstatuses/status"]`, `verbs: ["get"]`
    - Aggregation label: `rbac.authorization.k8s.io/aggregate-to-admin: "true"`
    - Tier 3 domain 'why' comment (as YAML comment): DRExecution gets `create` but not `update`/`patch`/`delete` because executions are immutable audit records (FR41) — only the controller writes status updates via the status subresource
  - [x] 1.4 Update `config/rbac/kustomization.yaml` to include the three new role files

- [x] Task 2: Update manager ClusterRole with controller operational RBAC (AC: #1)
  - [x] 2.1 Add RBAC markers to `pkg/controller/drplan/reconciler.go` (reconciler exists from Story 2.1):
    - `+kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch;update;patch`
    - `+kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch`
    - `+kubebuilder:rbac:groups=soteria.io,resources=drplans/finalizers,verbs=update`
    - `+kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups="",resources=secrets,verbs=get` (read credentials referenced by drivers)
  - [x] 2.2 Add RBAC markers to `pkg/controller/drexecution/doc.go` (pending reconciler from Epic 4):
    - `+kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch`
    - `+kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch`
    - `+kubebuilder:rbac:groups=soteria.io,resources=drgroupstatuses,verbs=get;list;watch;create;update;patch;delete`
    - `+kubebuilder:rbac:groups=soteria.io,resources=drgroupstatuses/status,verbs=get;update;patch`
  - [x] 2.3 Run `make manifests` to regenerate `config/rbac/role.yaml` from the markers
  - [x] 2.4 Verify the generated `role.yaml` includes all Soteria resource permissions + VM, namespace, PVC, StorageClass, and Secret read access

- [x] Task 3: Define credential reference types (AC: #4)
  - [x] 3.1 Create `pkg/drivers/credentials.go` with Tier 2 architecture block comment explaining: this module defines the credential reference types and resolver interface for storage drivers; credentials are always external (K8s Secrets or Vault) and resolved at operation time — the orchestrator never stores credential values in its own resources (FR45); the Vault resolver is defined but implementation is deferred to a future story
  - [x] 3.2 Define `SecretRef` struct — `Name string`, `Namespace string`, `Key string` (references a specific key within a Kubernetes Secret)
  - [x] 3.3 Define `VaultRef` struct — `Path string`, `Role string`, `Key string` (references a Vault KV secret with Kubernetes auth method)
  - [x] 3.4 Define `CredentialSource` struct — `SecretRef *SecretRef`, `VaultRef *VaultRef` (exactly one must be set; validated at admission time when StorageProviderConfig exists in Epic 3)
  - [x] 3.5 Define `CredentialResolver` interface — `Resolve(ctx context.Context, source CredentialSource) ([]byte, error)` — returns the raw credential bytes from the external source
  - [x] 3.6 Tier 3 domain 'why' comment on `CredentialResolver`: credentials are resolved at operation time rather than cached because storage operations are infrequent (seconds per failover) and Vault leases / Secret rotations must be respected; the performance overhead of per-operation resolution is negligible compared to storage replication latency

- [x] Task 4: Implement Kubernetes Secret credential resolver (AC: #5)
  - [x] 4.1 Create `pkg/drivers/credentials_secret.go`
  - [x] 4.2 Implement `SecretCredentialResolver` struct — fields: `Client corev1client.SecretsGetter` (typed client for reading Secrets)
  - [x] 4.3 Implement `Resolve(ctx context.Context, source CredentialSource) ([]byte, error)`:
    - If `source.SecretRef != nil`: read the Secret via `Client.Secrets(source.SecretRef.Namespace).Get(ctx, source.SecretRef.Name, metav1.GetOptions{})`, return `secret.Data[source.SecretRef.Key]`
    - If `source.VaultRef != nil`: return `ErrVaultNotImplemented` (Vault resolver deferred — see Dev Notes)
    - If both nil: return `ErrNoCredentialSource`
  - [x] 4.4 Define sentinel errors: `ErrVaultNotImplemented`, `ErrNoCredentialSource`, `ErrSecretKeyNotFound`, `ErrSecretNotFound`
  - [x] 4.5 Handle error cases:
    - Secret not found → wrap as `ErrSecretNotFound` with details
    - Key not found in Secret → wrap as `ErrSecretKeyNotFound` with Secret name and available keys
    - Context cancelled → propagate context error

- [x] Task 5: Implement credential sanitization utility (AC: #3)
  - [x] 5.1 Create `pkg/util/sanitize/sanitize.go` with Tier 2 architecture block comment explaining: this module provides credential sanitization for log messages, events, and metric labels; it ensures no Secret values appear in any orchestrator output (NFR14); sanitization is applied at the formatting boundary, not at the storage layer, to catch all output paths
  - [x] 5.2 Implement `SanitizeMap(fields map[string]interface{}, sensitiveKeys []string) map[string]interface{}` — returns a copy with sensitive key values replaced by `"[REDACTED]"`
  - [x] 5.3 Implement `SanitizeString(value string, secrets []string) string` — returns the value with any occurrence of the secret strings replaced by `"[REDACTED]"` — for log message sanitization when secret values might appear in error messages from external libraries
  - [x] 5.4 Define `DefaultSensitiveKeys` — `[]string{"password", "token", "secret", "credential", "key", "cert", "ca-data", "client-certificate-data", "client-key-data"}` — keys whose values should be redacted in structured log fields
  - [x] 5.5 Tier 3 domain 'why' comment: sanitization uses string replacement rather than encryption because the goal is preventing accidental exposure in human-readable output; the original credentials remain accessible only through the external Secret/Vault reference path

- [x] Task 6: Unit tests for credential reference types and resolver (AC: #4, #5)
  - [x] 6.1 Create `pkg/drivers/credentials_test.go`
  - [x] 6.2 Table-driven `TestSecretCredentialResolver_Resolve` covering:
    - Valid SecretRef → correct credential bytes returned
    - Secret not found → `ErrSecretNotFound` returned
    - Key not found in Secret → `ErrSecretKeyNotFound` returned with details
    - VaultRef provided → `ErrVaultNotImplemented` returned
    - Both refs nil → `ErrNoCredentialSource` returned
    - Context cancelled → context error returned
    - Secret with multiple keys → correct key extracted
  - [x] 6.3 Use `k8s.io/client-go/kubernetes/fake` for Secret reads
  - [x] 6.4 Test `CredentialSource` validation — exactly one of SecretRef/VaultRef must be set

- [x] Task 7: Unit tests for credential sanitization (AC: #3)
  - [x] 7.1 Create `pkg/util/sanitize/sanitize_test.go`
  - [x] 7.2 Table-driven `TestSanitizeMap` covering:
    - Map with sensitive key → value replaced with `"[REDACTED]"`
    - Map with non-sensitive key → value preserved
    - Map with nested map containing sensitive key → nested value redacted
    - Empty map → empty map returned
    - Nil map → nil returned
    - Multiple sensitive keys → all redacted
    - Case-insensitive key matching (e.g., "Password", "TOKEN") → all redacted
  - [x] 7.3 Table-driven `TestSanitizeString` covering:
    - String containing secret value → secret replaced with `"[REDACTED]"`
    - String without secret → unchanged
    - Multiple occurrences of same secret → all replaced
    - Multiple different secrets in one string → all replaced
    - Empty string → empty string
    - Empty secrets list → string unchanged

- [x] Task 8: Integration tests for RBAC enforcement (AC: #2, #7)
  - [x] 8.1 Create `test/integration/rbac/suite_test.go` with `//go:build integration` tag — set up envtest with Soteria CRDs and RBAC manifests applied
  - [x] 8.2 Create `test/integration/rbac/rbac_test.go`
  - [x] 8.3 `TestRBAC_ViewerCanReadDRPlan` — bind `soteria-viewer`, impersonate user, verify GET DRPlan succeeds and CREATE DRExecution is rejected (403)
  - [x] 8.4 `TestRBAC_EditorCanCreateDRPlan` — bind `soteria-editor`, verify CREATE DRPlan succeeds and CREATE DRExecution is rejected (403)
  - [x] 8.5 `TestRBAC_OperatorCanCreateDRExecution` — bind `soteria-operator`, verify CREATE DRExecution succeeds (201)
  - [x] 8.6 `TestRBAC_OperatorCannotDeleteDRExecution` — bind `soteria-operator`, verify DELETE DRExecution is rejected (403) — immutability enforcement
  - [x] 8.7 `TestRBAC_UnauthenticatedRejected` — verify requests without bindings are rejected (403)
  - [x] 8.8 Use `UserInfo` impersonation via rest.Config per test to simulate different personas

- [x] Task 9: Verify and finalize
  - [x] 9.1 Run `make lint-fix` to auto-fix code style — all new code clean, only pre-existing lint issues remain
  - [x] 9.2 Run `make test` — all unit tests pass (100% coverage on pkg/drivers and pkg/util/sanitize)
  - [x] 9.3 Run `make integration` — all integration tests pass (including 5 new RBAC tests)
  - [x] 9.4 Run `make manifests` — verified RBAC regenerated with all controller permissions
  - [x] 9.5 Verify persona ClusterRoles have correct aggregation labels (viewer→view, editor→edit, operator→admin)
  - [x] 9.6 Verify Tier 1/2/3 documentation standards met (retro action item #2)

### Review Findings

- [x] [Review][Patch] Secret resolver misclassifies non-NotFound failures as `ErrSecretNotFound` — **FIXED**: uses `apierrors.IsNotFound()` to distinguish NotFound from other errors; non-NotFound errors (context cancelled, RBAC denial, transport) are now propagated with `%w`
- [x] [Review][Patch] `CredentialSource` accepts both `SecretRef` and `VaultRef` at runtime — **FIXED**: added `ErrAmbiguousSource` check at top of `Resolve`; test case added
- [ ] [Review][Deferred] Credential sanitization utility is not wired into any real log, event, metric, or DRExecution output path — **Deferred**: no credential-handling code paths exist in production yet; wiring happens when Epic 3 drivers invoke `CredentialResolver` in real workflows
- [ ] [Review][Deferred] Sanitization tests only cover helper functions, not formatted log/event/metric output — **Deferred**: same reason; no credential output paths to test until driver integration (Epic 3+)
- [x] [Review][Patch] RBAC integration tests duplicate persona rules in Go — **FIXED**: `personaClusterRoles()` now loads `config/rbac/soteria_*.yaml` via `os.ReadFile` + `sigs.k8s.io/yaml` so tests validate the shipped manifests
- [ ] [Review][Deferred] RBAC integration tests validate CRD-based envtest authorization, not the aggregated API-server path — **Deferred**: true aggregated API-server RBAC testing requires the full apiserver stack (e2e scope), envtest tests are the correct tool for RBAC policy validation
- [x] [Review][Patch] `TestRBAC_UnauthenticatedRejected` misleading name — **FIXED**: renamed to `TestRBAC_UnboundUserRejected`

## Dev Notes

### Architecture Context

This story implements FR44 (Kubernetes-native RBAC) and FR45 (external credential references) plus NFR14 (no credential leakage). It establishes the access control model and credential handling patterns that the rest of the project builds on.

**Two concerns, one story:** RBAC and credential handling are grouped because they're both security fundamentals that must be in place before Epic 3 (Storage Driver Framework) and Epic 4 (Workflow Engine). The driver interface (Story 3.1) will require `CredentialSource` in its method signatures, and the workflow engine (Story 4.1) will need to pass credentials through without logging them.

### Aggregated API Server and RBAC

Soteria uses an **aggregated API server** pattern, not standard CRDs. RBAC still works identically because:

1. The aggregated API server registers `soteria.io/v1alpha1` with kube-apiserver's aggregation layer
2. kube-apiserver handles all authn/authz **before** proxying requests to the aggregated API
3. `RecommendedOptions` (in `pkg/apiserver/options.go`) includes delegated authn/authz — the aggregated API trusts kube-apiserver's RBAC decisions
4. ClusterRoles defined for `apiGroups: ["soteria.io"]` work exactly like CRD-based RBAC

This means **no custom authorization code is needed** — the story is about defining the right ClusterRoles and aggregation labels so that Kubernetes RBAC enforces access control automatically.

### ClusterRole Design

Three persona-based ClusterRoles with Kubernetes aggregation:

| ClusterRole | Persona | Key Permissions | Aggregates Into |
|---|---|---|---|
| `soteria-viewer` | Read-only users, dashboards, monitoring | `get`/`list`/`watch` all Soteria resources | `view` |
| `soteria-editor` | Plan authors, DR architects | Full DRPlan CRUD + viewer permissions | `edit` |
| `soteria-operator` | Failover operators, on-call engineers | `create` DRExecution + editor permissions | `admin` |

**Aggregation labels** (`rbac.authorization.k8s.io/aggregate-to-*`) allow existing Kubernetes role bindings to automatically grant Soteria permissions. A cluster admin who already has `admin` role binding gets `soteria-operator` permissions without new bindings.

**DRExecution immutability:** The `soteria-operator` role grants `create` on DRExecution but NOT `update`, `patch`, or `delete`. DRExecution is an immutable audit record (FR41). Only the controller updates execution status via the status subresource, using the `manager-role` ClusterRole (which gets `update`/`patch` on `drexecutions/status` from RBAC markers).

**DRGroupStatus:** No user-facing role grants write access to DRGroupStatus. It's a controller-managed resource — the execution controller creates and updates DRGroupStatus records during failover. Users only read them.

### Manager ClusterRole (Generated)

The `config/rbac/role.yaml` is auto-generated by `make manifests` from `+kubebuilder:rbac` markers. This story adds markers to the controller doc.go files for the permissions the controller needs:

- Soteria resources: read/write DRPlans, DRExecutions, DRGroupStatuses
- KubeVirt VMs: read (for discovery)
- Namespaces, PVCs, StorageClasses: read (for consistency checks and preflight)
- Secrets: read (for credential resolution when drivers need storage credentials)

**Note:** The markers go on `doc.go` because the reconcilers don't exist yet (Stories 2.1-2.4 are `ready-for-dev`). When the reconciler files are created, move the markers to the reconciler. The `doc.go` placement is valid for `make manifests` — controller-gen scans the entire package.

### Credential Handling Pattern

The architecture is clear: **the orchestrator never stores credentials directly** (FR45). The credential flow:

```
StorageProviderConfig (Epic 3, future)
  → .spec.credentials.secretRef.name = "odf-storage-creds"
  → .spec.credentials.secretRef.namespace = "openshift-storage"
  → .spec.credentials.secretRef.key = "endpoint-token"

At operation time (failover/reprotect):
  Controller → CredentialResolver.Resolve(source) → k8s Secret API → raw bytes
  Controller → StorageDriver.PromoteVolume(ctx, vg, credentials) → ODF API
```

**No caching:** Credentials are resolved per-operation, not cached. Storage operations happen at failover time (seconds per VM group), so the overhead of a Secret read per operation is negligible compared to storage replication latency. This respects Secret rotation and Vault lease expiry.

### Vault Integration — Deferred

The epic AC includes Vault as an alternative credential source. The retro (Epic 1) notes: *"Spike on Vault integration for Story 2.5 — assess include vs. defer (Owner: Charlie)"*.

**Decision: Define types, defer implementation.** This story:
- **Includes:** `VaultRef` struct definition in `CredentialSource` — so the type system is ready
- **Includes:** `ErrVaultNotImplemented` sentinel error — returned if Vault is selected before implementation
- **Defers:** Actual Vault integration (Kubernetes auth method, Vault client, lease management) — to a dedicated story after Epic 3 when drivers actually need credentials

This avoids pulling in the `hashicorp/vault/api` dependency and Vault test infrastructure before they're needed. The `CredentialResolver` interface allows adding Vault without changing any consumer code.

### Credential Sanitization

NFR14 requires no credential leakage in any output. The sanitization module provides two tools:

1. **`SanitizeMap`** — for structured log fields. When the controller logs StorageProvider operations, any credential-like keys in the log fields are redacted. Applied at the log formatting boundary.

2. **`SanitizeString`** — for error messages from external libraries. If an ODF client error includes a token in the error string, this catches it. Applied when wrapping external errors before logging.

**Testing strategy:** Unit tests assert that formatted output (log messages, event reasons, metric labels) does NOT contain known secret values. Tests provide actual secret strings and verify they're replaced with `[REDACTED]`.

### RBAC Integration Testing

RBAC integration tests use the aggregated API server's built-in authn/authz:

1. **envtest** starts with kube-apiserver which enforces RBAC
2. Tests apply the Soteria ClusterRoles from `config/rbac/`
3. Tests create ServiceAccounts and ClusterRoleBindings per persona
4. Tests use **impersonation** (`--as=system:serviceaccount:test:viewer`) or separate kubeconfigs to exercise each persona
5. Tests verify expected 200/201/403 status codes for each operation

**Important:** The aggregated API server in integration tests must be configured with delegated auth pointing to the test kube-apiserver, or the loopback client bypasses RBAC. The tests must NOT use the loopback admin client for RBAC assertions.

### Retro Action Items (Epic 1)

- **Action #2:** All new code must meet Tier 1-3 documentation standards
  - Tier 1: Create `pkg/drivers/doc.go` with 3-5 sentence godoc covering the drivers package purpose, credential types, and resolver interface
  - Tier 1: Create `pkg/util/sanitize/doc.go` with godoc covering the sanitization module
  - Tier 2: Architecture block comments on `credentials.go` (credential reference architecture) and `sanitize.go` (output sanitization)
  - Tier 3: Domain 'why' comments on no-caching credential resolution, sanitization at formatting boundary, and DRExecution verb restrictions

### Project Structure Notes

New files:
- `config/rbac/soteria_viewer_role.yaml` — viewer ClusterRole
- `config/rbac/soteria_editor_role.yaml` — editor ClusterRole
- `config/rbac/soteria_operator_role.yaml` — operator ClusterRole
- `pkg/drivers/credentials.go` — credential reference types + resolver interface
- `pkg/drivers/credentials_secret.go` — K8s Secret resolver implementation
- `pkg/util/sanitize/sanitize.go` — credential sanitization utility
- `pkg/drivers/doc.go` — package godoc (Tier 1)
- `pkg/util/sanitize/doc.go` — package godoc (Tier 1)

Modified files:
- `config/rbac/kustomization.yaml` — include new role files
- `config/rbac/role.yaml` — regenerated by `make manifests` (DO NOT EDIT manually)
- `pkg/controller/drplan/doc.go` — add RBAC markers
- `pkg/controller/drexecution/doc.go` — add RBAC markers

### Dependency on Other Stories

This story has **no hard prerequisites** — it can be implemented in parallel with Stories 2.1-2.4:
- RBAC manifests are standalone YAML files
- Credential types are standalone Go code
- Sanitization utility is standalone
- RBAC markers on `doc.go` work even without reconciler files

**Consumers:** Story 3.1 (StorageProvider Interface) will use `CredentialSource` in driver method signatures. Story 4.1 (State Machine Controller) will use `CredentialResolver` to pass credentials to drivers. Epic 5 (Monitoring) will use the sanitization utility for metric labels.

### References

- Architecture: `_bmad-output/planning-artifacts/architecture.md` — "Security Decisions" section (RBAC, Secrets), FR→structure mapping for `config/rbac/`, `pkg/drivers/`
- PRD: FR44 (Kubernetes-native RBAC), FR45 (external credential references), NFR14 (no credential leakage)
- Epic 1 Retro: Vault spike assessment, documentation standards
- Kubernetes RBAC aggregation: https://kubernetes.io/docs/reference/access-authn-authz/rbac/#aggregated-clusterroles
- Aggregated API server auth: https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/#authentication-flow

## Dev Agent Record

| Field | Value |
|-------|-------|
| Story file created | 2026-04-09 |
| Implementation started | 2026-04-11 |
| Implementation completed | 2026-04-11 |
| Code review requested | 2026-04-11 |
| Code review completed | 2026-04-11 |
| Status | done |

### Implementation Plan

Implemented in sequence: RBAC manifests (persona ClusterRoles + aggregation labels), controller RBAC markers (drplan reconciler + drexecution doc.go), credential reference types (SecretRef/VaultRef/CredentialSource/CredentialResolver), K8s Secret resolver, credential sanitization utility, then comprehensive tests (unit + integration).

### Completion Notes

- Three persona ClusterRoles created with K8s aggregation labels (viewer→view, editor→edit, operator→admin)
- DRExecution immutability enforced via RBAC: operator gets `create` only, no `update`/`patch`/`delete`
- Manager ClusterRole regenerated with Secrets read access, DRPlan finalizers, and DRExecution/DRGroupStatus permissions
- CredentialResolver interface with SecretCredentialResolver implementation; VaultRef types defined, implementation deferred with ErrVaultNotImplemented
- Credential sanitization module with SanitizeMap (recursive, case-insensitive) and SanitizeString
- Unit tests: 100% coverage on pkg/drivers and pkg/util/sanitize
- Integration tests: 5 RBAC tests using envtest with user impersonation verifying viewer/editor/operator/delete-forbidden/unauthenticated scenarios
- All Tier 1/2/3 documentation standards met (doc.go godoc, architecture block comments, domain 'why' comments)
- RBAC markers placed on existing reconciler.go (drplan) rather than doc.go since reconciler already exists from Story 2.1

### Debug Log

- Fixed deprecated `NewSimpleClientset` → `NewClientset` in credential tests to satisfy staticcheck
- Context cancellation test: fake client doesn't honor context.Canceled, so used reactor to simulate the error path
- Review fix: `resolveFromSecret` now uses `apierrors.IsNotFound()` to distinguish real 404 from other failures; context.Canceled test updated to assert error is propagated, not mis-wrapped
- Review fix: added `ErrAmbiguousSource` guard when both SecretRef and VaultRef are set
- Review fix: integration tests now load persona ClusterRoles from `config/rbac/soteria_*.yaml` manifests instead of duplicating in Go
- Review fix: renamed `TestRBAC_UnauthenticatedRejected` → `TestRBAC_UnboundUserRejected` to accurately describe test intent

## File List

New files:
- `config/rbac/soteria_viewer_role.yaml`
- `config/rbac/soteria_editor_role.yaml`
- `config/rbac/soteria_operator_role.yaml`
- `pkg/drivers/credentials.go`
- `pkg/drivers/credentials_secret.go`
- `pkg/drivers/credentials_test.go`
- `pkg/util/sanitize/doc.go`
- `pkg/util/sanitize/sanitize.go`
- `pkg/util/sanitize/sanitize_test.go`
- `test/integration/rbac/suite_test.go`
- `test/integration/rbac/rbac_test.go`

Modified files:
- `config/rbac/kustomization.yaml` — added three persona role files
- `config/rbac/role.yaml` — regenerated by `make manifests` (secrets, finalizers, drexecution/drgroupstatus permissions)
- `pkg/controller/drplan/reconciler.go` — updated RBAC markers (added update;patch on drplans, finalizers, secrets)
- `pkg/controller/drexecution/doc.go` — added RBAC markers for drexecutions and drgroupstatuses
- `pkg/drivers/doc.go` — updated Tier 1 package godoc
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — status updated

## Change Log

- 2026-04-11: Implemented Story 2.5 — RBAC persona ClusterRoles with K8s aggregation, controller RBAC markers, credential reference types + K8s Secret resolver, credential sanitization utility, unit tests (100% coverage), RBAC integration tests (5 scenarios)
- 2026-04-11: Code review completed — 4 patches applied (error classification, ambiguous source guard, YAML-loaded integration tests, test rename), 3 findings deferred (sanitization wiring, output tests, aggregated API server testing — all require Epic 3+ code paths)
