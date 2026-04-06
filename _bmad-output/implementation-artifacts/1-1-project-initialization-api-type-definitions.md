# Story 1.1: Project Initialization & API Type Definitions

Status: ready-for-dev

## Story

As a developer,
I want the project scaffolded with kubebuilder and core API types defined,
so that all subsequent development has a consistent foundation with build tooling, linting, and codegen.

## Acceptance Criteria

1. **Given** an empty repository, **When** the project is initialized with `kubebuilder init --domain soteria.io --repo github.com/soteria-project/soteria --plugins go/v4`, **Then** the project compiles with `make build`, the Makefile includes targets for build, test, lint, manifests, and docker-build, **And** `.golangci.yml` is configured with the K8s logging linter.

2. **Given** the initialized project, **When** API types are defined in `pkg/apis/soteria.io/v1alpha1/types.go`, **Then**:
   - DRPlan, DRExecution, and DRGroupStatus structs exist with spec and status substructures
   - DRPlan.Spec includes `vmSelector` (LabelSelector), `waveLabel` (string), and `maxConcurrentFailovers` (int)
   - DRExecution.Spec includes `planName` (string) and `mode` (enum: planned_migration, disaster)
   - DRExecution.Status includes `result` (enum: Succeeded, PartiallySucceeded, Failed), `waves[]` with per-group status, `startTime`, and `completionTime`
   - DRGroupStatus includes per-group state tracking fields
   - All status conditions use `metav1.Condition`
   - CRD JSON tags use camelCase per Kubernetes convention

3. **Given** the type definitions, **When** `hack/update-codegen.sh` is run (deepcopy-gen, openapi-gen), **Then** `zz_generated_deepcopy.go` files are generated without errors, **And** `hack/verify-codegen.sh` passes confirming generated code is up to date.

4. **Given** the project structure, **When** reviewing the directory layout, **Then** it follows the architecture: `cmd/soteria/`, `pkg/apis/`, `pkg/apiserver/`, `pkg/registry/`, `pkg/storage/`, `pkg/drivers/`, `pkg/engine/`, `pkg/controller/`, `pkg/admission/`, `pkg/metrics/`, `internal/`, `console-plugin/`, `config/`, `hack/`, `test/`, `bundle/`, **And** a multi-stage Dockerfile exists for the single Go binary, **And** `bundle.Dockerfile` exists for the OLM bundle image.

## Tasks / Subtasks

- [ ] Task 1: kubebuilder project scaffolding (AC: #1)
  - [ ] 1.1 Run `kubebuilder init --domain soteria.io --repo github.com/soteria-project/soteria --plugins go/v4`
  - [ ] 1.2 Restructure entry point from `cmd/main.go` to `cmd/soteria/main.go`
  - [ ] 1.3 Update Makefile: add `integration`, `helmchart-test`, `dev-cluster` targets alongside kubebuilder defaults
  - [ ] 1.4 Configure `.golangci.yml` with K8s logging linter (logcheck)
  - [ ] 1.5 Verify `make build` and `make lint` pass

- [ ] Task 2: Create full directory layout (AC: #4)
  - [ ] 2.1 Create `pkg/apis/soteria.io/v1alpha1/` — type definitions home
  - [ ] 2.2 Create `pkg/apis/soteria.io/install/` — scheme registration
  - [ ] 2.3 Create stub directories with `doc.go` placeholder: `pkg/apiserver/`, `pkg/registry/drplan/`, `pkg/registry/drexecution/`, `pkg/registry/drgroupstatus/`, `pkg/storage/scylladb/`, `pkg/drivers/`, `pkg/drivers/noop/`, `pkg/drivers/odf/`, `pkg/drivers/fake/`, `pkg/drivers/conformance/`, `pkg/engine/`, `pkg/controller/drplan/`, `pkg/controller/drexecution/`, `pkg/admission/`, `pkg/metrics/`
  - [ ] 2.4 Create stub `internal/preflight/`
  - [ ] 2.5 Create `console-plugin/` placeholder (README only — full scaffold in Story 6.1)
  - [ ] 2.6 Create `test/integration/storage/`, `test/integration/engine/`, `test/integration/apiserver/`, `test/e2e/`
  - [ ] 2.7 Create `config/apiservice/`, `config/scylladb/`, `config/certmanager/`
  - [ ] 2.8 Create `bundle/` placeholder for OLM bundle
  - [ ] 2.9 Create `.github/workflows/` with placeholder workflow files

- [ ] Task 3: Define API types (AC: #2)
  - [ ] 3.1 Create `pkg/apis/soteria.io/v1alpha1/doc.go` with `+groupName=soteria.io` marker
  - [ ] 3.2 Create `pkg/apis/soteria.io/v1alpha1/types.go` with all three resource types (see Dev Notes for complete field spec)
  - [ ] 3.3 Create `pkg/apis/soteria.io/v1alpha1/register.go` — GVR registration + SchemeBuilder
  - [ ] 3.4 Create `pkg/apis/soteria.io/v1alpha1/defaults.go` — defaulting stubs
  - [ ] 3.5 Create `pkg/apis/soteria.io/v1alpha1/validation.go` — type-level validation stubs
  - [ ] 3.6 Create `pkg/apis/soteria.io/install/install.go` — scheme registration for all versions

- [ ] Task 4: Set up codegen (AC: #3)
  - [ ] 4.1 Create `hack/update-codegen.sh` using `k8s.io/code-generator` kube_codegen.sh — run deepcopy-gen, openapi-gen
  - [ ] 4.2 Create `hack/verify-codegen.sh` that runs update-codegen.sh in check mode
  - [ ] 4.3 Run codegen, verify `zz_generated_deepcopy.go` generated
  - [ ] 4.4 Verify `hack/verify-codegen.sh` passes

- [ ] Task 5: Dockerfiles (AC: #4)
  - [ ] 5.1 Update kubebuilder-generated Dockerfile for multi-stage Go build targeting `cmd/soteria/`
  - [ ] 5.2 Create `bundle.Dockerfile` for OLM bundle image

- [ ] Task 6: Final validation
  - [ ] 6.1 `make build` passes
  - [ ] 6.2 `make test` passes (no tests yet, but no errors)
  - [ ] 6.3 `make lint` passes
  - [ ] 6.4 `hack/verify-codegen.sh` passes

## Dev Notes

### API Type Definitions — Complete Field Specification

These types follow the `kubernetes/sample-apiserver` pattern, NOT kubebuilder CRD patterns. They live in `pkg/apis/` (not `api/` which is kubebuilder's default for CRDs). They will be served via an Aggregated API Server backed by ScyllaDB — not via controller-runtime CRD registration.

**API group:** `soteria.io/v1alpha1`
**Resources:** `drplans`, `drexecutions`, `drgroupstatuses`

#### DRPlan

```go
// DRPlan defines a disaster recovery plan for a set of VMs selected by labels.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRPlan struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   DRPlanSpec   `json:"spec"`
    Status DRPlanStatus `json:"status,omitempty"`
}

type DRPlanSpec struct {
    // VMSelector selects VMs to include in this DR plan.
    VMSelector metav1.LabelSelector `json:"vmSelector"`
    // WaveLabel is the label key used to assign VMs to execution waves.
    WaveLabel string `json:"waveLabel"`
    // MaxConcurrentFailovers limits concurrent VM failovers per wave chunk.
    MaxConcurrentFailovers int `json:"maxConcurrentFailovers"`
}

type DRPlanStatus struct {
    // Phase represents the current DR lifecycle state.
    // Valid values: SteadyState, FailingOver, FailedOver, Reprotecting, DRedSteadyState, FailingBack
    Phase string `json:"phase,omitempty"`
    // Conditions represent the latest observations of the DRPlan's state.
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // ObservedGeneration is the most recent generation observed.
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// DRPlanList contains a list of DRPlans.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRPlanList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items []DRPlan `json:"items"`
}
```

#### DRExecution

```go
// DRExecution records an immutable execution of a DRPlan.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRExecution struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   DRExecutionSpec   `json:"spec"`
    Status DRExecutionStatus `json:"status,omitempty"`
}

// ExecutionMode defines how a DRPlan is executed.
type ExecutionMode string

const (
    ExecutionModePlannedMigration ExecutionMode = "PlannedMigration"
    ExecutionModeDisaster         ExecutionMode = "Disaster"
)

type DRExecutionSpec struct {
    // PlanName references the DRPlan being executed.
    PlanName string `json:"planName"`
    // Mode specifies the execution type — chosen at runtime, not on the plan.
    Mode ExecutionMode `json:"mode"`
}

// ExecutionResult is the overall outcome of a DRExecution.
type ExecutionResult string

const (
    ExecutionResultSucceeded          ExecutionResult = "Succeeded"
    ExecutionResultPartiallySucceeded ExecutionResult = "PartiallySucceeded"
    ExecutionResultFailed             ExecutionResult = "Failed"
)

// DRGroupResult is the outcome of a single DRGroup within a wave.
type DRGroupResult string

const (
    DRGroupResultPending    DRGroupResult = "Pending"
    DRGroupResultInProgress DRGroupResult = "InProgress"
    DRGroupResultCompleted  DRGroupResult = "Completed"
    DRGroupResultFailed     DRGroupResult = "Failed"
)

type DRExecutionStatus struct {
    // Result is the overall execution outcome.
    Result ExecutionResult `json:"result,omitempty"`
    // Waves contains per-wave execution status.
    Waves []WaveStatus `json:"waves,omitempty"`
    // StartTime is when execution began.
    StartTime *metav1.Time `json:"startTime,omitempty"`
    // CompletionTime is when execution finished.
    CompletionTime *metav1.Time `json:"completionTime,omitempty"`
    // Conditions represent the latest observations.
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type WaveStatus struct {
    // WaveIndex is the 0-based wave ordinal.
    WaveIndex int `json:"waveIndex"`
    // Groups contains per-DRGroup status within this wave.
    Groups []DRGroupExecutionStatus `json:"groups,omitempty"`
    // StartTime is when this wave began.
    StartTime *metav1.Time `json:"startTime,omitempty"`
    // CompletionTime is when this wave finished.
    CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

type DRGroupExecutionStatus struct {
    // Name identifies this DRGroup within the wave.
    Name string `json:"name"`
    // Result is the outcome of this DRGroup.
    Result DRGroupResult `json:"result,omitempty"`
    // VMNames lists VMs in this DRGroup.
    VMNames []string `json:"vmNames,omitempty"`
    // Error contains error details if the group failed.
    Error string `json:"error,omitempty"`
    // StartTime is when this group began processing.
    StartTime *metav1.Time `json:"startTime,omitempty"`
    // CompletionTime is when this group finished.
    CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// DRExecutionList contains a list of DRExecutions.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRExecutionList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items []DRExecution `json:"items"`
}
```

#### DRGroupStatus

```go
// DRGroupStatus tracks the real-time state of a DRGroup during execution.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRGroupStatus struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   DRGroupStatusSpec   `json:"spec"`
    Status DRGroupStatusState  `json:"status,omitempty"`
}

type DRGroupStatusSpec struct {
    // ExecutionName references the parent DRExecution.
    ExecutionName string `json:"executionName"`
    // WaveIndex is the wave this group belongs to.
    WaveIndex int `json:"waveIndex"`
    // GroupName is the name of this DRGroup within the wave.
    GroupName string `json:"groupName"`
    // VMNames lists VMs in this group.
    VMNames []string `json:"vmNames,omitempty"`
}

type DRGroupStatusState struct {
    // Phase is the current processing state.
    Phase DRGroupResult `json:"phase,omitempty"`
    // Conditions represent the latest observations.
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Steps records per-step execution details.
    Steps []StepStatus `json:"steps,omitempty"`
    // LastTransitionTime is when the phase last changed.
    LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

type StepStatus struct {
    // Name describes this step (e.g., "PromoteVolume", "StartVM").
    Name string `json:"name"`
    // Status is the step outcome.
    Status string `json:"status,omitempty"`
    // Message provides human-readable detail.
    Message string `json:"message,omitempty"`
    // Timestamp is when this step completed.
    Timestamp *metav1.Time `json:"timestamp,omitempty"`
}

// DRGroupStatusList contains a list of DRGroupStatuses.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DRGroupStatusList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items []DRGroupStatus `json:"items"`
}
```

### DRPlan Phase Values

These are the valid values for `DRPlanStatus.Phase` — define as string constants:

| Phase | Meaning |
|---|---|
| `SteadyState` | Normal operation, VMs active on primary site |
| `FailingOver` | Failover in progress |
| `FailedOver` | VMs running on DR site after failover |
| `Reprotecting` | Re-establishing replication in reverse direction |
| `DRedSteadyState` | Normal operation on DR site, reverse replication healthy |
| `FailingBack` | Failback in progress (returning to primary) |

### kubebuilder init — What You Get vs What You Modify

kubebuilder v4.13.1 generates:
- `cmd/main.go` → **Move** to `cmd/soteria/main.go` (architecture: single binary entry point)
- `Makefile` → **Extend** with custom targets (integration, helmchart-test, dev-cluster)
- `Dockerfile` → **Update** build path to target `cmd/soteria/`
- `.golangci.yml` → **Extend** with K8s logcheck linter
- `config/` → **Keep** kubebuilder Kustomize manifests, **add** `apiservice/`, `scylladb/`, `certmanager/`
- `go.mod` → **Add** dependencies: `k8s.io/apiserver`, `k8s.io/apimachinery`, `k8s.io/code-generator`
- `PROJECT` → **Keep** as-is (kubebuilder project metadata)
- `hack/boilerplate.go.txt` → **Update** with Apache 2.0 header

kubebuilder does NOT generate `pkg/apis/` — that follows sample-apiserver patterns. Do NOT use `kubebuilder create api` for the aggregated API types.

### Codegen Setup

The `hack/update-codegen.sh` script must:
1. Use `k8s.io/code-generator`'s `kube_codegen.sh` helper
2. Run `kube::codegen::gen_helpers` for deepcopy generation on `pkg/apis/soteria.io/v1alpha1/`
3. Produce `zz_generated_deepcopy.go` for all types with `+k8s:deepcopy-gen` markers
4. Make both scripts executable (`chmod +x`)

`hack/verify-codegen.sh` must:
1. Run update-codegen.sh
2. Diff the output against the committed files
3. Exit non-zero if generated code differs from committed code

### .golangci.yml Configuration

Extend the kubebuilder-generated config to include:
- Standard kubebuilder linters (errcheck, gosimple, govet, ineffassign, staticcheck, unused)
- Add `logcheck` (K8s structured logging linter) if available, OR configure `govet` with K8s logging checks
- Configure exclusions for generated files (`zz_generated_*.go`)

### Dockerfile Details

**Main Dockerfile** (multi-stage):
- Builder stage: Go build targeting `./cmd/soteria/`
- Runtime stage: distroless/static or UBI minimal
- Matches kubebuilder pattern but with updated binary path

**bundle.Dockerfile**:
- Copies OLM bundle manifests from `bundle/` directory
- Standard OLM bundle image pattern from operator-sdk
- Add LABEL annotations for OLM (bundle.mediatype, bundle.manifests, bundle.metadata, bundle.package)

### go.mod Dependencies

Beyond kubebuilder defaults, add:
- `k8s.io/apiserver` — for Aggregated API Server (storage.Interface, API registration)
- `k8s.io/apimachinery` — for API types, runtime.Object, metav1 types
- `k8s.io/code-generator` — for deepcopy-gen, openapi-gen (typically as a tools dependency)
- `k8s.io/client-go` — kubebuilder already adds this

Use versions matching the kubebuilder-selected Kubernetes dependency set (check `go.mod` after `kubebuilder init` for the correct k8s dependency version line, then align all k8s.io modules).

### Stub Package Pattern

For directories that are created now but implemented in later stories, create a `doc.go` with the package declaration and a brief comment:

```go
// Package apiserver implements the Aggregated API Server for Soteria.
package apiserver
```

This ensures the Go compiler doesn't complain about empty packages and provides intent documentation.

### Project Structure Notes

Complete directory structure — every path listed here must exist after this story:

```
soteria/
├── README.md
├── LICENSE                                  # Apache 2.0
├── Makefile
├── Dockerfile
├── bundle.Dockerfile
├── go.mod
├── go.sum
├── .golangci.yml
├── .gitignore
├── PROJECT                                  # kubebuilder project metadata
│
├── .github/workflows/
│   ├── pr-operator.yml                      # placeholder
│   └── release-operator.yml                 # placeholder
│
├── cmd/soteria/
│   └── main.go
│
├── pkg/apis/soteria.io/
│   ├── install/
│   │   └── install.go
│   └── v1alpha1/
│       ├── doc.go
│       ├── types.go
│       ├── register.go
│       ├── defaults.go
│       ├── validation.go
│       └── zz_generated_deepcopy.go         # generated
│
├── pkg/apiserver/
│   └── doc.go                               # stub
├── pkg/registry/
│   ├── drplan/doc.go                        # stub
│   ├── drexecution/doc.go                   # stub
│   └── drgroupstatus/doc.go                 # stub
├── pkg/storage/scylladb/
│   └── doc.go                               # stub
├── pkg/drivers/
│   ├── doc.go                               # stub
│   ├── noop/doc.go                          # stub
│   ├── odf/doc.go                           # stub
│   ├── fake/doc.go                          # stub
│   └── conformance/doc.go                   # stub
├── pkg/engine/
│   └── doc.go                               # stub
├── pkg/controller/
│   ├── drplan/doc.go                        # stub
│   └── drexecution/doc.go                   # stub
├── pkg/admission/
│   └── doc.go                               # stub
├── pkg/metrics/
│   └── doc.go                               # stub
│
├── internal/preflight/
│   └── doc.go                               # stub
│
├── console-plugin/
│   └── README.md                            # placeholder — scaffolded in Story 6.1
│
├── config/
│   ├── default/                             # kubebuilder generated
│   ├── rbac/                                # kubebuilder generated
│   ├── webhook/                             # kubebuilder generated
│   ├── apiservice/                          # stub — for APIService registration
│   ├── scylladb/                            # stub — ScyllaDB ScyllaCluster CR reference
│   └── certmanager/                         # stub — cert-manager Certificate CRs
│
├── hack/
│   ├── boilerplate.go.txt                   # Apache 2.0 header
│   ├── update-codegen.sh                    # deepcopy-gen, openapi-gen
│   └── verify-codegen.sh                    # CI: verify generated code is current
│
├── test/
│   ├── integration/
│   │   ├── storage/                         # stub
│   │   ├── engine/                          # stub
│   │   └── apiserver/                       # stub
│   └── e2e/                                 # stub
│
└── bundle/                                  # stub — OLM bundle manifests
```

### Architecture Compliance

- **API group**: `soteria.io/v1alpha1` — set via kubebuilder `--domain soteria.io`
- **JSON tags**: camelCase only — `vmSelector`, `waveLabel`, `maxConcurrentFailovers`, `planName`
- **Status conditions**: `metav1.Condition` exclusively — no custom condition types
- **Timestamps**: `*metav1.Time` for all time fields
- **Deep copy markers**: `+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object` on all top-level types
- **Package naming**: lowercase single word — `engine`, `drivers`, `storage`, `apiserver`, `admission`, `metrics`
- **License**: Apache 2.0 in LICENSE file and boilerplate header

### Critical Warnings

1. Do NOT use `kubebuilder create api` for DRPlan/DRExecution/DRGroupStatus — these are aggregated API server resources, not CRDs. kubebuilder's `create api` generates CRD YAML and controller-runtime reconcilers, which is wrong for this architecture.

2. The `pkg/apis/` directory follows `kubernetes/sample-apiserver` conventions, NOT kubebuilder's `api/` convention. kubebuilder puts CRD types in `api/v1alpha1/` — we put aggregated API types in `pkg/apis/soteria.io/v1alpha1/`.

3. Do NOT create internal/ directory under kubebuilder's default `internal/controller/` for controller code. Our controllers go in `pkg/controller/` because driver authors need to understand the code organization. The `internal/` directory is reserved for truly internal packages like `internal/preflight/`.

4. Ensure all k8s.io module versions are aligned (apiserver, apimachinery, client-go, code-generator must use the same Kubernetes release version). Check after `kubebuilder init` and align manually if needed.

5. The `hack/boilerplate.go.txt` file should contain the Apache 2.0 copyright header — update the kubebuilder default to reference Soteria project.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 1.1 (lines 316-349)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Project Structure (lines 358-478)]
- [Source: _bmad-output/planning-artifacts/architecture.md — CRD Status Patterns (lines 299-307)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Naming Patterns (lines 244-297)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Starter Template (lines 83-153)]
- [Source: _bmad-output/planning-artifacts/prd.md — FR1, FR2 (DRPlan creation and viewing)]
- [Source: _bmad-output/planning-artifacts/prd.md — FR19 (execution mode at runtime)]
- [Source: _bmad-output/planning-artifacts/prd.md — FR41 (DRExecution as audit record)]
- [Source: _bmad-output/project-context.md — Complete project rules and patterns]
- [External: kubebuilder v4.13.1 — https://pkg.go.dev/sigs.k8s.io/kubebuilder/v4]
- [External: k8s.io/code-generator v0.35.x — kube_codegen.sh for deepcopy/openapi generation]
- [External: kubernetes/sample-apiserver — Reference for aggregated API server type patterns]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
