# Story 1.5: Aggregated API Server & API Registration

Status: ready-for-dev

## Story

As a platform engineer,
I want to interact with DRPlan, DRExecution, and DRGroupStatus resources via kubectl through the kube-apiserver aggregation layer,
so that DR resources feel like native Kubernetes resources with standard CRUD, watch, and API discovery.

## Acceptance Criteria

1. **Given** the storage.Interface implementation from Stories 1.3–1.4, **When** the extension API server is configured in `pkg/apiserver/apiserver.go`, **Then** the `soteria.io/v1alpha1` API group is registered with the kube-apiserver aggregation layer, **And** `kubectl api-resources` lists `drplans.soteria.io`, `drexecutions.soteria.io`, and `drgroupstatuses.soteria.io`, **And** `kubectl explain drplan` returns the OpenAPI schema.

2. **Given** the registered API group, **When** registry wiring in `pkg/registry/` connects resource types to storage, **Then** each resource type (DRPlan, DRExecution, DRGroupStatus) has a strategy file defining create/update/delete validation, **And** DRExecution enforces append-only semantics (immutable after completion), **And** status and spec subresources are served separately.

3. **Given** a running extension API server with kube-apiserver proxy, **When** `kubectl create -f drplan.yaml` is executed, **Then** the DRPlan is created in ScyllaDB via the storage.Interface, **And** `kubectl get drplans` returns the created plan with correct status, **And** `kubectl get drplan <name> -o yaml` returns the full resource with metadata, spec, and status, **And** `kubectl delete drplan <name>` removes the resource.

4. **Given** the aggregated API server, **When** the server starts as part of the single binary (`cmd/soteria/main.go`), **Then** the binary runs both the API server and controller-runtime manager in one process, **And** leader election is configured via `ctrl.Options{LeaderElection: true}` controlling the workflow engine only, **And** all replicas serve API requests (active/active for reads).

5. **Given** the APIService configuration in `config/apiservice/`, **When** the APIService resource is applied to the cluster, **Then** kube-apiserver proxies all `soteria.io` requests to the extension API server, **And** Kubernetes RBAC is enforced on all proxied requests.

## Tasks / Subtasks

- [ ] Task 1: Implement API server configuration (AC: #1, #4)
  - [ ] 1.1 Create `pkg/apiserver/apiserver.go` with `SoteriaServer` struct embedding `genericapiserver.GenericAPIServer`
  - [ ] 1.2 Implement `Config` struct holding `GenericConfig`, ScyllaDB storage backend, and codec
  - [ ] 1.3 Implement `CompletedConfig` struct returned by `Config.Complete()`
  - [ ] 1.4 Implement `CompletedConfig.New()` — creates the `GenericAPIServer`, installs the `soteria.io/v1alpha1` API group
  - [ ] 1.5 Register all three resource types (DRPlan, DRExecution, DRGroupStatus) with the API group via `InstallAPIGroup`
  - [ ] 1.6 Configure the k8s.io/apiserver cacher to wrap the ScyllaDB `storage.Interface` — one cacher per resource type
  - [ ] 1.7 Wire OpenAPI schema generation using the registered types from `pkg/apis/soteria.io/v1alpha1/`

- [ ] Task 2: Implement server startup options (AC: #1, #4)
  - [ ] 2.1 Create `pkg/apiserver/options.go` with `SoteriaServerOptions` struct
  - [ ] 2.2 Add ScyllaDB connection flags: `--scylladb-contact-points`, `--scylladb-keyspace`, `--scylladb-tls-cert`, `--scylladb-tls-key`, `--scylladb-tls-ca`
  - [ ] 2.3 Implement `SoteriaServerOptions.Config()` — builds `Config` from flags and recommended server config
  - [ ] 2.4 Implement `SoteriaServerOptions.AddFlags(fs *pflag.FlagSet)` to register all flags
  - [ ] 2.5 Use `genericapiserver.NewRecommendedConfig(codecs)` as the base server configuration
  - [ ] 2.6 Configure `Serializer` chain using the scheme from `pkg/apis/soteria.io/install/`

- [ ] Task 3: Implement DRPlan registry (AC: #2, #3)
  - [ ] 3.1 Create `pkg/registry/drplan/strategy.go` with `drplanStrategy` implementing `rest.RESTCreateStrategy`, `rest.RESTUpdateStrategy`, `rest.RESTDeleteStrategy`
  - [ ] 3.2 Implement `PrepareForCreate(ctx, obj)` — clear status, set default phase to `SteadyState`
  - [ ] 3.3 Implement `PrepareForUpdate(ctx, obj, old)` — preserve status (status is updated via subresource)
  - [ ] 3.4 Implement `Validate(ctx, obj)` — validate spec fields (vmSelector, waveLabel, maxConcurrentFailovers > 0)
  - [ ] 3.5 Implement `ValidateUpdate(ctx, obj, old)` — validate updates, reject immutable field changes if any
  - [ ] 3.6 Implement `WarningsOnCreate` and `WarningsOnUpdate` — return nil (no warnings)
  - [ ] 3.7 Implement `Canonicalize(obj)` — no-op
  - [ ] 3.8 Implement `NamespaceScoped() bool` — return true
  - [ ] 3.9 Create `pkg/registry/drplan/storage.go` with `NewREST()` — builds `registry.Store` wired to cacher-wrapped `storage.Interface`
  - [ ] 3.10 Configure status subresource with separate `StatusREST` backed by `StatusStrategy`

- [ ] Task 4: Implement DRExecution registry (AC: #2, #3)
  - [ ] 4.1 Create `pkg/registry/drexecution/strategy.go` with `drexecutionStrategy`
  - [ ] 4.2 Implement `PrepareForCreate(ctx, obj)` — clear status, set startTime
  - [ ] 4.3 Implement `PrepareForUpdate(ctx, obj, old)` — enforce append-only: reject updates to completed executions (status.result is set and non-empty)
  - [ ] 4.4 Implement `Validate(ctx, obj)` — validate planName non-empty, mode is valid enum
  - [ ] 4.5 Implement `ValidateUpdate(ctx, obj, old)` — enforce immutability of spec fields after creation, validate append-only status
  - [ ] 4.6 Implement `NamespaceScoped() bool` — return true
  - [ ] 4.7 Create `pkg/registry/drexecution/storage.go` with `NewREST()` — builds `registry.Store` wired to cacher-wrapped `storage.Interface`
  - [ ] 4.8 Configure status subresource with separate `StatusREST`

- [ ] Task 5: Implement DRGroupStatus registry (AC: #2, #3)
  - [ ] 5.1 Create `pkg/registry/drgroupstatus/strategy.go` with `drgroupstatusStrategy`
  - [ ] 5.2 Implement `PrepareForCreate(ctx, obj)` — clear status
  - [ ] 5.3 Implement `PrepareForUpdate(ctx, obj, old)` — preserve spec (spec is immutable after creation)
  - [ ] 5.4 Implement `Validate(ctx, obj)` — validate executionName, waveIndex >= 0, groupName non-empty
  - [ ] 5.5 Implement `ValidateUpdate(ctx, obj, old)` — reject spec changes, validate status transitions
  - [ ] 5.6 Implement `NamespaceScoped() bool` — return true
  - [ ] 5.7 Create `pkg/registry/drgroupstatus/storage.go` with `NewREST()` — builds `registry.Store` wired to cacher-wrapped `storage.Interface`
  - [ ] 5.8 Configure status subresource with separate `StatusREST`

- [ ] Task 6: Implement single binary entry point (AC: #4)
  - [ ] 6.1 Update `cmd/soteria/main.go` — construct and run both the extension API server and controller-runtime manager
  - [ ] 6.2 Start the API server via `GenericAPIServer.PrepareRun().Run(stopCh)`
  - [ ] 6.3 Start controller-runtime manager with `ctrl.Options{LeaderElection: true, LeaderElectionID: "soteria-controller"}` in a separate goroutine
  - [ ] 6.4 Wire signal handling — unified `stopCh` propagated to both components
  - [ ] 6.5 Initialize ScyllaDB client (Story 1.2) and schema (Story 1.2) before starting the API server
  - [ ] 6.6 Create the ScyllaDB `Store` (Story 1.3) and pass it to the API server config
  - [ ] 6.7 Controller-runtime manager starts with no controllers yet (empty — controllers added in later stories)

- [ ] Task 7: Create APIService manifests (AC: #5)
  - [ ] 7.1 Create `config/apiservice/apiservice.yaml` — APIService resource for `v1alpha1.soteria.io`
  - [ ] 7.2 Set `spec.service` to reference the Soteria extension API server Service
  - [ ] 7.3 Set `spec.group: soteria.io`, `spec.version: v1alpha1`, `spec.groupPriorityMinimum: 1000`, `spec.versionPriority: 100`
  - [ ] 7.4 Set `spec.insecureSkipTLSVerify: false` with `spec.caBundle` placeholder for cert-manager
  - [ ] 7.5 Create `config/apiservice/kustomization.yaml` to include the APIService resource
  - [ ] 7.6 Create `config/apiservice/service.yaml` — Kubernetes Service exposing the extension API server on port 443

- [ ] Task 8: Configure cacher layer (AC: #1, #3)
  - [ ] 8.1 Import `k8s.io/apiserver/pkg/storage/cacher` and `k8s.io/apiserver/pkg/storage/storagebackend`
  - [ ] 8.2 Create `newCachedStorage()` helper that wraps the ScyllaDB `storage.Interface` with `cacher.NewCacherFromConfig()`
  - [ ] 8.3 Configure `cacher.Config` with the correct `storage.Interface`, `Versioner`, `NewFunc`, `NewListFunc`, `GetAttrsFunc`, `Codec`
  - [ ] 8.4 One cacher instance per resource type (DRPlan, DRExecution, DRGroupStatus)
  - [ ] 8.5 The cacher calls `Watch()` once at startup per resource type — validates the CDC-based Watch from Story 1.4

- [ ] Task 9: Integration tests (AC: #1, #2, #3)
  - [ ] 9.1 Create `test/integration/apiserver/suite_test.go` — setup testcontainers ScyllaDB + extension API server
  - [ ] 9.2 Create `test/integration/apiserver/apiserver_test.go` with `//go:build integration` tag
  - [ ] 9.3 Test API discovery — verify `soteria.io/v1alpha1` group is discoverable
  - [ ] 9.4 Test DRPlan CRUD via client-go — Create, Get, List, Update, Delete
  - [ ] 9.5 Test DRExecution CRUD — Create, verify spec immutability after creation, status subresource updates
  - [ ] 9.6 Test DRGroupStatus CRUD — Create, verify spec immutability, status updates
  - [ ] 9.7 Test DRExecution append-only — attempt update on completed execution, verify rejection
  - [ ] 9.8 Test status subresource — update status separately from spec
  - [ ] 9.9 Test watch events — create/update/delete and verify watch channel receives events
  - [ ] 9.10 Test OpenAPI schema available for all resource types

- [ ] Task 10: Final validation
  - [ ] 10.1 `make build` passes
  - [ ] 10.2 `make test` passes (unit tests only)
  - [ ] 10.3 `make lint` passes
  - [ ] 10.4 `make integration` passes (testcontainers)

## Dev Notes

### Architecture Overview

Story 1.5 is where the ScyllaDB storage backend (Stories 1.2–1.4) meets the Kubernetes API layer. The extension API server registers `soteria.io/v1alpha1` with the kube-apiserver aggregation layer, making DRPlan, DRExecution, and DRGroupStatus indistinguishable from native Kubernetes resources from the client's perspective.

```
kubectl / Console / Controller
        │
        ▼
kube-apiserver (proxy for soteria.io via APIService)
        │
        ▼
Soteria Extension API Server (GenericAPIServer)
        │
        ▼
k8s.io/apiserver cacher (in-memory cache + fan-out)
        │
        ▼
storage.Interface (pkg/storage/scylladb/store.go)
        │
        ▼
ScyllaDB (generic KV store)
```

The kube-apiserver aggregation layer delegates all requests for the `soteria.io` API group to the extension API server via the `APIService` resource. Kubernetes RBAC is enforced at the kube-apiserver layer before the request reaches the extension server — standard `Role`/`ClusterRole` bindings on `drplans.soteria.io`, `drexecutions.soteria.io`, and `drgroupstatuses.soteria.io` work out of the box.

### Key Reference: kubernetes/sample-apiserver

The primary reference for this story is `kubernetes/sample-apiserver`. The architecture follows the same patterns:

- `pkg/apiserver/apiserver.go` → `SoteriaServer` (analogous to `WardleServer` in sample-apiserver)
- `pkg/registry/<resource>/strategy.go` → REST strategy per resource type
- `pkg/registry/<resource>/storage.go` → REST storage wiring
- `cmd/soteria/main.go` → binary entry point starting the server

The key difference from sample-apiserver is the storage backend: sample-apiserver uses etcd, Soteria uses ScyllaDB via a custom `storage.Interface`. The cacher layer from `k8s.io/apiserver` sits between the registry and the storage backend, providing the same in-memory caching and watch fan-out that etcd-backed servers get.

### pkg/apiserver/apiserver.go — Server Configuration

```go
package apiserver

import (
    "k8s.io/apiserver/pkg/registry/rest"
    genericapiserver "k8s.io/apiserver/pkg/server"
    serverstorage "k8s.io/apiserver/pkg/server/storage"

    soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
    "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
    drplanregistry "github.com/soteria-project/soteria/pkg/registry/drplan"
    drexecutionregistry "github.com/soteria-project/soteria/pkg/registry/drexecution"
    drgroupstatusregistry "github.com/soteria-project/soteria/pkg/registry/drgroupstatus"
)

// Config holds the configuration for the Soteria extension API server.
type Config struct {
    GenericConfig *genericapiserver.RecommendedConfig
    // StorageFactory provides storage.Interface instances for each resource type,
    // already wrapped with the cacher layer.
    StorageFactory StorageFactory
}

// StorageFactory creates cacher-wrapped storage.Interface instances per resource.
type StorageFactory interface {
    NewDRPlanStorage() (rest.Storage, rest.Storage, error)       // (main, status)
    NewDRExecutionStorage() (rest.Storage, rest.Storage, error)
    NewDRGroupStatusStorage() (rest.Storage, rest.Storage, error)
}

type completedConfig struct {
    GenericConfig genericapiserver.CompletedConfig
    StorageFactory StorageFactory
}

// CompletedConfig is the result of calling Config.Complete().
type CompletedConfig struct {
    *completedConfig
}

func (cfg *Config) Complete() CompletedConfig {
    c := completedConfig{
        GenericConfig:  cfg.GenericConfig.Complete(),
        StorageFactory: cfg.StorageFactory,
    }
    return CompletedConfig{&c}
}

// SoteriaServer wraps the generic API server.
type SoteriaServer struct {
    GenericAPIServer *genericapiserver.GenericAPIServer
}

func (c CompletedConfig) New() (*SoteriaServer, error) {
    genericServer, err := c.GenericConfig.New("soteria-apiserver", genericapiserver.NewEmptyDelegate())
    if err != nil {
        return nil, err
    }

    s := &SoteriaServer{
        GenericAPIServer: genericServer,
    }

    // Build the API group info and install it
    apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(
        soteriav1alpha1.GroupName,
        install.Scheme,
        /* parameterCodec */ ,
        install.Codecs,
    )

    // Wire storage for each resource
    v1alpha1storage := map[string]rest.Storage{}

    drplanStore, drplanStatusStore, err := c.StorageFactory.NewDRPlanStorage()
    // ... error handling ...
    v1alpha1storage["drplans"] = drplanStore
    v1alpha1storage["drplans/status"] = drplanStatusStore

    drexecStore, drexecStatusStore, err := c.StorageFactory.NewDRExecutionStorage()
    // ... error handling ...
    v1alpha1storage["drexecutions"] = drexecStore
    v1alpha1storage["drexecutions/status"] = drexecStatusStore

    drgroupStore, drgroupStatusStore, err := c.StorageFactory.NewDRGroupStatusStorage()
    // ... error handling ...
    v1alpha1storage["drgroupstatuses"] = drgroupStore
    v1alpha1storage["drgroupstatuses/status"] = drgroupStatusStore

    apiGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = v1alpha1storage

    if err := s.GenericAPIServer.InstallAPIGroup(&apiGroupInfo); err != nil {
        return nil, err
    }

    return s, nil
}
```

**Critical pattern:** `NewDefaultAPIGroupInfo` expects the scheme, parameter codec, and codecs from the install package. The scheme must have all three resource types registered. The `install` package from Story 1.1 provides this.

### pkg/apiserver/options.go — Server Startup Options

```go
package apiserver

import (
    "github.com/spf13/pflag"
    genericapiserver "k8s.io/apiserver/pkg/server"
    genericoptions "k8s.io/apiserver/pkg/server/options"
)

// SoteriaServerOptions holds the options for starting the Soteria API server.
type SoteriaServerOptions struct {
    RecommendedOptions *genericoptions.RecommendedOptions

    // ScyllaDB connection options
    ScyllaDBContactPoints string
    ScyllaDBKeyspace      string
    ScyllaDBTLSCert       string
    ScyllaDBTLSKey        string
    ScyllaDBTLSCA         string
}

func NewSoteriaServerOptions() *SoteriaServerOptions {
    o := &SoteriaServerOptions{
        RecommendedOptions: genericoptions.NewRecommendedOptions(
            "",  // empty prefix — we don't use etcd
            install.Codecs.LegacyCodec(soteriav1alpha1.SchemeGroupVersion),
        ),
    }
    // Disable etcd-related options since we use ScyllaDB
    o.RecommendedOptions.Etcd = nil
    return o
}

func (o *SoteriaServerOptions) AddFlags(fs *pflag.FlagSet) {
    o.RecommendedOptions.AddFlags(fs)
    fs.StringVar(&o.ScyllaDBContactPoints, "scylladb-contact-points", "localhost:9042", "Comma-separated ScyllaDB contact points")
    fs.StringVar(&o.ScyllaDBKeyspace, "scylladb-keyspace", "soteria", "ScyllaDB keyspace name")
    fs.StringVar(&o.ScyllaDBTLSCert, "scylladb-tls-cert", "", "Path to ScyllaDB TLS client certificate")
    fs.StringVar(&o.ScyllaDBTLSKey, "scylladb-tls-key", "", "Path to ScyllaDB TLS client key")
    fs.StringVar(&o.ScyllaDBTLSCA, "scylladb-tls-ca", "", "Path to ScyllaDB TLS CA certificate")
}

func (o *SoteriaServerOptions) Config() (*Config, error) {
    serverConfig := genericapiserver.NewRecommendedConfig(install.Codecs)

    if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
        return nil, err
    }

    config := &Config{
        GenericConfig: serverConfig,
        // StorageFactory is built in main.go using the ScyllaDB client
    }
    return config, nil
}
```

**Important:** The `RecommendedOptions.Etcd` field is set to `nil` because Soteria does not use etcd. The RecommendedOptions provides secure serving, authentication, authorization, admission, and audit — all of which are needed. Only etcd is replaced.

### Registry Strategy Pattern

Each resource type needs a strategy that implements validation and mutation hooks for create, update, and delete operations. The strategy follows the `k8s.io/apiserver/pkg/registry/rest` interfaces.

#### DRPlan Strategy

```go
package drplan

import (
    "context"

    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/util/validation/field"
    "k8s.io/apiserver/pkg/storage/names"

    soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

type drplanStrategy struct {
    runtime.ObjectTyper
    names.NameGenerator
}

var Strategy = drplanStrategy{install.Scheme, names.SimpleNameGenerator}

func (drplanStrategy) NamespaceScoped() bool { return true }

func (drplanStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
    plan := obj.(*soteriav1alpha1.DRPlan)
    plan.Status = soteriav1alpha1.DRPlanStatus{}
    if plan.Status.Phase == "" {
        plan.Status.Phase = soteriav1alpha1.PhaseSteadyState
    }
    plan.Generation = 1
}

func (drplanStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
    newPlan := obj.(*soteriav1alpha1.DRPlan)
    oldPlan := old.(*soteriav1alpha1.DRPlan)
    // Status is managed via the status subresource — preserve it on spec updates
    newPlan.Status = oldPlan.Status
}

func (drplanStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
    plan := obj.(*soteriav1alpha1.DRPlan)
    allErrs := field.ErrorList{}

    fldPath := field.NewPath("spec")
    if plan.Spec.WaveLabel == "" {
        allErrs = append(allErrs, field.Required(fldPath.Child("waveLabel"), ""))
    }
    if plan.Spec.MaxConcurrentFailovers <= 0 {
        allErrs = append(allErrs, field.Invalid(
            fldPath.Child("maxConcurrentFailovers"),
            plan.Spec.MaxConcurrentFailovers,
            "must be greater than 0",
        ))
    }
    return allErrs
}

func (drplanStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
    return drplanStrategy{}.Validate(ctx, obj)
}

func (drplanStrategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string  { return nil }
func (drplanStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
    return nil
}
func (drplanStrategy) AllowCreateOnUpdate() bool       { return false }
func (drplanStrategy) AllowUnconditionalUpdate() bool  { return false }
func (drplanStrategy) Canonicalize(obj runtime.Object)  {}
```

**Status subresource strategy:**

```go
type drplanStatusStrategy struct {
    drplanStrategy
}

var StatusStrategy = drplanStatusStrategy{Strategy}

func (drplanStatusStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
    newPlan := obj.(*soteriav1alpha1.DRPlan)
    oldPlan := old.(*soteriav1alpha1.DRPlan)
    // On status update, preserve the spec — only status changes
    newPlan.Spec = oldPlan.Spec
}

func (drplanStatusStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
    return field.ErrorList{}
}
```

The status subresource pattern ensures:
- `PUT /apis/soteria.io/v1alpha1/namespaces/{ns}/drplans/{name}` — updates spec, preserves status
- `PUT /apis/soteria.io/v1alpha1/namespaces/{ns}/drplans/{name}/status` — updates status, preserves spec

This is the standard Kubernetes spec/status split convention.

#### DRExecution Strategy — Append-Only Semantics

```go
package drexecution

func (drexecutionStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
    exec := obj.(*soteriav1alpha1.DRExecution)
    exec.Status = soteriav1alpha1.DRExecutionStatus{}
    exec.Generation = 1
}

func (drexecutionStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
    newExec := obj.(*soteriav1alpha1.DRExecution)
    oldExec := old.(*soteriav1alpha1.DRExecution)
    // Spec is immutable after creation
    newExec.Spec = oldExec.Spec
    // Status is managed via status subresource
    newExec.Status = oldExec.Status
}

func (drexecutionStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
    exec := obj.(*soteriav1alpha1.DRExecution)
    allErrs := field.ErrorList{}

    fldPath := field.NewPath("spec")
    if exec.Spec.PlanName == "" {
        allErrs = append(allErrs, field.Required(fldPath.Child("planName"), ""))
    }
    if exec.Spec.Mode != soteriav1alpha1.ExecutionModePlannedMigration &&
        exec.Spec.Mode != soteriav1alpha1.ExecutionModeDisaster {
        allErrs = append(allErrs, field.NotSupported(
            fldPath.Child("mode"),
            exec.Spec.Mode,
            []string{string(soteriav1alpha1.ExecutionModePlannedMigration), string(soteriav1alpha1.ExecutionModeDisaster)},
        ))
    }
    return allErrs
}
```

**Append-only enforcement on status subresource:**

```go
type drexecutionStatusStrategy struct {
    drexecutionStrategy
}

func (drexecutionStatusStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
    newExec := obj.(*soteriav1alpha1.DRExecution)
    oldExec := old.(*soteriav1alpha1.DRExecution)
    // Preserve spec on status updates
    newExec.Spec = oldExec.Spec
}

func (drexecutionStatusStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
    oldExec := old.(*soteriav1alpha1.DRExecution)
    allErrs := field.ErrorList{}

    // Reject updates to completed executions (append-only after completion)
    if oldExec.Status.Result == soteriav1alpha1.ExecutionResultSucceeded ||
        oldExec.Status.Result == soteriav1alpha1.ExecutionResultPartiallySucceeded ||
        oldExec.Status.Result == soteriav1alpha1.ExecutionResultFailed {
        allErrs = append(allErrs, field.Forbidden(
            field.NewPath("status"),
            "DRExecution is immutable after completion",
        ))
    }
    return allErrs
}
```

The append-only enforcement means:
- Spec is always immutable (set at creation, never changed)
- Status can be updated only while the execution is in progress
- Once `status.result` is set to a terminal value, the entire resource is frozen

#### DRGroupStatus Strategy

```go
package drgroupstatus

func (drgroupstatusStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
    gs := obj.(*soteriav1alpha1.DRGroupStatus)
    gs.Status = soteriav1alpha1.DRGroupStatusState{}
    gs.Generation = 1
}

func (drgroupstatusStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
    newGS := obj.(*soteriav1alpha1.DRGroupStatus)
    oldGS := old.(*soteriav1alpha1.DRGroupStatus)
    // Spec is immutable after creation
    newGS.Spec = oldGS.Spec
    // Status is managed via status subresource
    newGS.Status = oldGS.Status
}

func (drgroupstatusStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
    gs := obj.(*soteriav1alpha1.DRGroupStatus)
    allErrs := field.ErrorList{}

    fldPath := field.NewPath("spec")
    if gs.Spec.ExecutionName == "" {
        allErrs = append(allErrs, field.Required(fldPath.Child("executionName"), ""))
    }
    if gs.Spec.GroupName == "" {
        allErrs = append(allErrs, field.Required(fldPath.Child("groupName"), ""))
    }
    if gs.Spec.WaveIndex < 0 {
        allErrs = append(allErrs, field.Invalid(
            fldPath.Child("waveIndex"),
            gs.Spec.WaveIndex,
            "must be >= 0",
        ))
    }
    return allErrs
}
```

### Registry Storage Wiring

Each resource type needs a `storage.go` that creates the REST endpoints backed by the cacher-wrapped ScyllaDB storage.

```go
package drplan

import (
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apiserver/pkg/registry/generic"
    genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
    "k8s.io/apiserver/pkg/registry/rest"

    soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// NewREST creates the REST storage for DRPlan.
func NewREST(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter) (*genericregistry.Store, *StatusREST, error) {
    store := &genericregistry.Store{
        NewFunc:                   func() runtime.Object { return &soteriav1alpha1.DRPlan{} },
        NewListFunc:              func() runtime.Object { return &soteriav1alpha1.DRPlanList{} },
        DefaultQualifiedResource: soteriav1alpha1.Resource("drplans"),
        SingularQualifiedResource: soteriav1alpha1.Resource("drplan"),

        CreateStrategy:      Strategy,
        UpdateStrategy:      Strategy,
        DeleteStrategy:      Strategy,
        TableConvertor:      rest.NewDefaultTableConvertor(soteriav1alpha1.Resource("drplans")),
    }

    options := &generic.StoreOptions{
        RESTOptions: optsGetter,
    }
    if err := store.CompleteWithOptions(options); err != nil {
        return nil, nil, err
    }

    statusStore := *store
    statusStore.UpdateStrategy = StatusStrategy
    statusStore.ResetFieldsStrategy = StatusStrategy

    return store, &StatusREST{store: &statusStore}, nil
}

// StatusREST implements the REST endpoint for DRPlan status subresource.
type StatusREST struct {
    store *genericregistry.Store
}

func (r *StatusREST) New() runtime.Object {
    return &soteriav1alpha1.DRPlan{}
}

func (r *StatusREST) Destroy() {}

func (r *StatusREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
    return r.store.Get(ctx, name, options)
}

func (r *StatusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo,
    createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc,
    forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
    return r.store.Update(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
}

func (r *StatusREST) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
    return r.store.GetResetFields()
}
```

**Pattern:** The `genericregistry.Store` from `k8s.io/apiserver/pkg/registry/generic/registry` provides the full CRUD implementation. It wraps `storage.Interface` (via the cacher) with the strategy's validation and mutation hooks. The `RESTOptionsGetter` provides the decorated storage backend.

### Cacher Layer Wiring

The cacher wraps the raw ScyllaDB `storage.Interface` to provide in-memory caching, watch fan-out, and efficient list operations. This is the same cacher that wraps etcd in standard Kubernetes — it's storage-backend-agnostic.

```go
func newRESTOptionsGetter(scyllaStore storage.Interface, codec runtime.Codec) generic.RESTOptionsGetter {
    return &soteriaRESTOptionsGetter{
        store: scyllaStore,
        codec: codec,
    }
}

type soteriaRESTOptionsGetter struct {
    store storage.Interface
    codec runtime.Codec
}

func (g *soteriaRESTOptionsGetter) GetRESTOptions(resource schema.GroupResource) (generic.RESTOptions, error) {
    return generic.RESTOptions{
        StorageConfig: &storagebackend.ConfigForResource{
            Config: storagebackend.Config{
                // Custom transport wrapping our ScyllaDB store
                Transport: storagebackend.TransportConfig{},
                Codec:     g.codec,
            },
            GroupResource: resource,
        },
        Decorator: func(config *storagebackend.ConfigForResource,
            resourcePrefix string,
            keyFunc func(obj runtime.Object) (string, error),
            newFunc func() runtime.Object,
            newListFunc func() runtime.Object,
            getAttrsFunc storage.AttrFunc,
            trigger storage.IndexerFuncs,
            indexers *cache.Indexers,
        ) (storage.Interface, factory.DestroyFunc, error) {
            // Wrap the ScyllaDB storage.Interface with the cacher
            cacherConfig := cacherstorage.Config{
                Storage:        g.store,
                Versioner:      g.store.Versioner(),
                ResourcePrefix: resourcePrefix,
                KeyFunc:        keyFunc,
                NewFunc:        newFunc,
                NewListFunc:    newListFunc,
                GetAttrsFunc:   getAttrsFunc,
                Codec:          g.codec,
            }
            cacher, err := cacherstorage.NewCacherFromConfig(cacherConfig)
            if err != nil {
                return nil, nil, err
            }
            destroyFunc := func() { cacher.Stop() }
            return cacher, destroyFunc, nil
        },
    }, nil
}
```

**How the cacher uses Watch from Story 1.4:**

1. The cacher calls `storage.Interface.Watch()` with `resourceVersion=0` and `SendInitialEvents=true`
2. The Watch implementation from Story 1.4 delivers a snapshot of all existing objects (ADDED events), followed by a BOOKMARK, then streams CDC changes
3. The cacher maintains an in-memory ring buffer of recent events
4. When a client calls `kubectl get drplans`, the cacher serves from its in-memory cache — no ScyllaDB roundtrip
5. When a client opens a watch, the cacher fans out from its single underlying Watch — one CDC consumer serves all clients

This is why Story 1.4's Watch must be correct: the cacher depends on it for the entire API server's correctness.

**Alternative approach — StorageFactoryAdapter:** If the `Decorator` approach proves complex, consider implementing `StorageFactory` directly:

```go
type soteriaStorageFactory struct {
    scyllaStore storage.Interface
    codec       runtime.Codec
}

func (f *soteriaStorageFactory) NewDRPlanStorage() (rest.Storage, rest.Storage, error) {
    optsGetter := newRESTOptionsGetter(f.scyllaStore, f.codec)
    return drplanregistry.NewREST(install.Scheme, optsGetter)
}
```

The exact wiring depends on how `genericregistry.Store.CompleteWithOptions()` discovers its storage backend. Study `k8s.io/apiserver/pkg/registry/generic/registry/store.go`'s `CompleteWithOptions` method closely.

### Single Binary Architecture

```go
// cmd/soteria/main.go
package main

import (
    "context"
    "os"

    "k8s.io/klog/v2"
    ctrl "sigs.k8s.io/controller-runtime"

    scylladb "github.com/soteria-project/soteria/pkg/storage/scylladb"
    "github.com/soteria-project/soteria/pkg/apiserver"
)

func main() {
    ctx := ctrl.SetupSignalHandler()

    // 1. Parse flags (API server options + ScyllaDB options)
    opts := apiserver.NewSoteriaServerOptions()
    // ... flag parsing via cobra or pflag ...

    // 2. Initialize ScyllaDB client
    scyllaClient, err := scylladb.NewClient(scylladb.ClientConfig{
        ContactPoints: opts.ScyllaDBContactPoints,
        Keyspace:      opts.ScyllaDBKeyspace,
        TLSCert:       opts.ScyllaDBTLSCert,
        TLSKey:        opts.ScyllaDBTLSKey,
        TLSCA:         opts.ScyllaDBTLSCA,
    })
    // ... error handling ...
    defer scyllaClient.Close()

    // 3. Ensure schema exists
    if err := scylladb.EnsureSchema(scyllaClient.Session(), scylladb.SchemaConfig{
        Keyspace: opts.ScyllaDBKeyspace,
    }); err != nil {
        klog.Fatalf("ensuring schema: %v", err)
    }

    // 4. Create ScyllaDB storage.Interface
    store := scylladb.NewStore(scyllaClient.Session(), codec, opts.ScyllaDBKeyspace)

    // 5. Build API server config
    serverConfig, err := opts.Config()
    // ... error handling ...
    serverConfig.StorageFactory = &soteriaStorageFactory{store: store, codec: codec}
    completed := serverConfig.Complete()

    // 6. Create API server
    server, err := completed.New()
    // ... error handling ...

    // 7. Start controller-runtime manager (no controllers yet — added in later stories)
    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        LeaderElection:   true,
        LeaderElectionID: "soteria-controller",
    })
    // ... error handling ...

    // 8. Run both components
    go func() {
        if err := mgr.Start(ctx); err != nil {
            klog.Fatalf("controller manager: %v", err)
        }
    }()

    // API server blocks on the main goroutine
    stopCh := ctx.Done()
    if err := server.GenericAPIServer.PrepareRun().Run(stopCh); err != nil {
        klog.Fatalf("api server: %v", err)
    }
}
```

**Key design points:**

- **Single process, two components:** The API server and controller-runtime manager share a process but are otherwise independent. The API server handles all HTTP requests (CRUD, watch, OpenAPI). The controller manager runs reconcilers (added in later stories).
- **Leader election scope:** `ctrl.Options{LeaderElection: true}` controls only the controller-runtime manager. All replicas serve API requests (active/active for reads and writes — writes go to ScyllaDB via LOCAL_ONE). Only the leader runs workflow controllers.
- **Signal handling:** `ctrl.SetupSignalHandler()` returns a context that cancels on SIGTERM/SIGINT. Both components respect this context.
- **Startup order:** ScyllaDB client → schema → storage.Interface → API server → controller manager. The API server must start before the controller manager because controllers use client-go to talk to the API server.

### APIService Configuration

```yaml
# config/apiservice/apiservice.yaml
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.soteria.io
spec:
  group: soteria.io
  version: v1alpha1
  service:
    namespace: soteria-system
    name: soteria-apiserver
  groupPriorityMinimum: 1000
  versionPriority: 100
  caBundle: "${CA_BUNDLE}"  # Injected by cert-manager or kustomize
```

```yaml
# config/apiservice/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: soteria-apiserver
  namespace: soteria-system
spec:
  ports:
  - port: 443
    targetPort: 6443
    protocol: TCP
  selector:
    app: soteria
```

**Priority values:**
- `groupPriorityMinimum: 1000` — places `soteria.io` in the standard extension range (built-in groups use higher values)
- `versionPriority: 100` — standard for the only version in the group

**caBundle:** The APIService needs the CA that signed the extension API server's serving certificate. In production, cert-manager annotations on the APIService trigger automatic `caBundle` injection. For development/testing, `insecureSkipTLSVerify: true` can be used temporarily.

### OpenAPI Schema Generation

The extension API server exposes OpenAPI schemas for all registered resources. This powers `kubectl explain drplan` and enables client-side validation. The schema is auto-generated from the Go struct tags and validation markers defined in Story 1.1's `pkg/apis/soteria.io/v1alpha1/types.go`.

The `genericapiserver.GenericAPIServer` automatically serves:
- `/openapi/v2` — full OpenAPI v2 spec
- `/openapi/v3` — full OpenAPI v3 spec (if supported by the k8s.io/apiserver version)
- `/apis/soteria.io/v1alpha1` — API resource discovery

For `kubectl explain` to work, the OpenAPI spec must include the types. This requires the scheme to have proper type metadata. The `+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object` markers and proper `doc.go` with `+groupName=soteria.io` from Story 1.1 enable this.

If openapi-gen was run in Story 1.1 (via `hack/update-codegen.sh`), the generated OpenAPI definitions are already available. If not, `k8s.io/apiserver` generates basic schemas from struct reflection — functional but less detailed. Full OpenAPI generation with `+kubebuilder:validation` markers can be added later.

### File Organization

After this story, the following files are created or updated:

```
pkg/apiserver/
├── apiserver.go          # NEW — SoteriaServer, Config, CompletedConfig, New()
└── options.go            # NEW — SoteriaServerOptions, flags, Config()

pkg/registry/
├── drplan/
│   ├── strategy.go       # NEW — drplanStrategy + StatusStrategy
│   └── storage.go        # NEW — NewREST() + StatusREST
├── drexecution/
│   ├── strategy.go       # NEW — drexecutionStrategy + StatusStrategy (append-only)
│   └── storage.go        # NEW — NewREST() + StatusREST
└── drgroupstatus/
    ├── strategy.go       # NEW — drgroupstatusStrategy + StatusStrategy
    └── storage.go        # NEW — NewREST() + StatusREST

cmd/soteria/
└── main.go               # UPDATED — single binary: API server + controller-runtime manager

config/apiservice/
├── apiservice.yaml       # NEW — APIService registration
├── service.yaml          # NEW — Service for extension API server
└── kustomization.yaml    # NEW — Kustomize references
```

Integration tests:

```
test/integration/apiserver/
├── suite_test.go         # NEW — testcontainers ScyllaDB + extension API server setup
└── apiserver_test.go     # NEW — CRUD, watch, status subresource, append-only tests
```

### Dependencies

This story uses dependencies already added in Stories 1.1 and 1.2:
- `k8s.io/apiserver` — `genericapiserver`, `registry`, `storage/cacher`, `storage/storagebackend`
- `k8s.io/apimachinery` — `runtime`, `field`, `schema`
- `k8s.io/apiserver/pkg/registry/generic` — `RESTOptionsGetter`, `StoreOptions`
- `k8s.io/apiserver/pkg/registry/generic/registry` — `Store`
- `k8s.io/apiserver/pkg/registry/rest` — strategy interfaces
- `k8s.io/apiserver/pkg/server/options` — `RecommendedOptions`
- `sigs.k8s.io/controller-runtime` — controller manager (already from kubebuilder)

**New dependencies:**
- `github.com/spf13/pflag` — flag parsing (typically already present via kubebuilder/controller-runtime transitive dependency)
- `github.com/spf13/cobra` — command structure (optional — can use pflag directly)

### Testing Strategy

**Integration tests** (`test/integration/apiserver/`):

The integration tests start a full extension API server backed by a real ScyllaDB (testcontainers). This validates the complete chain: client-go → API server → cacher → storage.Interface → ScyllaDB.

```go
// test/integration/apiserver/suite_test.go
// Sets up:
// 1. ScyllaDB via testcontainers (reuse from Story 1.2)
// 2. Extension API server using the testing framework from k8s.io/apiserver/pkg/server/testing
// 3. client-go client configured to talk to the extension API server

// Alternatives for test infrastructure:
// Option A: Use k8s.io/apiserver/pkg/server/options.NewRecommendedOptions + test loopback
// Option B: Use envtest-like pattern with the extension API server
```

Test cases:

- `TestAPIServer_Discovery_SoteriaGroupRegistered` — verify `soteria.io/v1alpha1` appears in API discovery
- `TestAPIServer_DRPlan_Create` — create via client-go, verify in ScyllaDB
- `TestAPIServer_DRPlan_Get` — create then get, verify fields
- `TestAPIServer_DRPlan_List` — create multiple, list all, verify count
- `TestAPIServer_DRPlan_Update` — update spec, verify new resourceVersion
- `TestAPIServer_DRPlan_Delete` — delete, verify not found
- `TestAPIServer_DRPlan_StatusSubresource` — update status separately from spec
- `TestAPIServer_DRExecution_SpecImmutable` — create, attempt spec change, verify rejection
- `TestAPIServer_DRExecution_AppendOnly` — complete execution, attempt status update, verify rejection
- `TestAPIServer_DRGroupStatus_SpecImmutable` — same pattern
- `TestAPIServer_DRPlan_Watch` — open watch, create resource, verify ADDED event received
- `TestAPIServer_DRPlan_Validation_MissingWaveLabel` — submit invalid DRPlan, verify error
- `TestAPIServer_DRExecution_Validation_InvalidMode` — submit invalid mode, verify error

All integration tests use `//go:build integration` tag and generous timeouts (watch events may take several seconds due to CDC polling).

### Integration Test Setup Pattern

```go
func setupTestAPIServer(t *testing.T) (*rest.Config, func()) {
    // 1. Start ScyllaDB testcontainer (from Story 1.2 patterns)
    scyllaContainer := startScyllaContainer(t)

    // 2. Create ScyllaDB client and ensure schema
    client := scylladb.NewClient(...)
    scylladb.EnsureSchema(client.Session(), ...)

    // 3. Create storage.Interface
    store := scylladb.NewStore(client.Session(), codec, keyspace)

    // 4. Build API server options
    opts := apiserver.NewSoteriaServerOptions()
    // Configure with test-specific settings (loopback, test certs)

    // 5. Start the API server
    config, _ := opts.Config()
    config.StorageFactory = &soteriaStorageFactory{store: store, codec: codec}
    server, _ := config.Complete().New()

    // 6. Start serving (in background goroutine)
    // Use server.GenericAPIServer.PrepareRun().Run(stopCh)

    // 7. Return rest.Config pointing to the test server
    return restConfig, cleanup
}
```

For integration tests, consider using the `k8s.io/apiserver/pkg/server/options.SecureServingOptionsWithLoopback` for the test server's serving configuration. This avoids needing real TLS certificates in tests.

### Scheme and Codec Setup

The scheme and codec chain must be configured correctly for the API server to serialize/deserialize resources. The install package from Story 1.1 provides the scheme:

```go
// pkg/apis/soteria.io/install/install.go
package install

import (
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/serializer"

    soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

var (
    Scheme = runtime.NewScheme()
    Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
    soteriav1alpha1.AddToScheme(Scheme)
    // Add other versions as they are created
}
```

The `Codecs` codec factory is passed to:
1. `genericapiserver.NewRecommendedConfig(Codecs)` — for API server serialization
2. The ScyllaDB `Store` constructor — for object serialization to/from the KV blob
3. The cacher — for event serialization

**Critical:** The same scheme and codecs must be used consistently across the API server, storage, and cacher. Mismatched schemes cause silent serialization failures.

### Handling the k8s.io/apiserver RESTOptionsGetter

The `genericregistry.Store.CompleteWithOptions()` method expects a `RESTOptionsGetter` that provides storage configuration. In standard Kubernetes, this comes from the `StorageFactory` backed by etcd. For Soteria, we provide a custom `RESTOptionsGetter` that returns our ScyllaDB-backed storage wrapped in the cacher.

The key interface:

```go
// k8s.io/apiserver/pkg/registry/generic
type RESTOptionsGetter interface {
    GetRESTOptions(resource schema.GroupResource) (RESTOptions, error)
}

type RESTOptions struct {
    StorageConfig           *storagebackend.ConfigForResource
    Decorator               StorageDecorator
    EnableGarbageCollection bool
    DeleteCollectionWorkers int
    CountMetricPollPeriod   time.Duration
    ResourcePrefix          string
}
```

The `Decorator` field is the key — it's the function that wraps raw storage with the cacher. In standard k8s, this is `generic.UndecoratedStorage` (no cacher) or `cacherstorage.NewCacherFromConfig`. For Soteria, the decorator must:
1. Accept the storage config
2. Return the ScyllaDB `storage.Interface` wrapped with the cacher
3. Return a `DestroyFunc` that stops the cacher on shutdown

### Architecture Compliance

- **Storage boundary:** `pkg/apiserver/` never touches ScyllaDB directly — it receives a `storage.Interface` from the caller
- **Single binary:** `cmd/soteria/main.go` runs API server + controller-runtime manager in one process
- **Leader election:** Controls only the controller-runtime manager (workflow engine). All replicas serve API requests
- **RBAC:** Standard Kubernetes RBAC via kube-apiserver proxy — no custom authorization
- **API group:** `soteria.io/v1alpha1` — matches architecture and project-context.md
- **Error wrapping:** lowercase, no punctuation, wrap with `%w`
- **Structured logging:** `klog.V(2).InfoS(...)` or `log.FromContext(ctx).V(2).Info(...)` — no fmt.Println
- **Context propagation:** All methods accept and propagate `ctx`
- **Test naming:** `TestFunction_Scenario_Expected`
- **Integration test tag:** `//go:build integration`

### Critical Warnings

1. **Do NOT use etcd.** The `RecommendedOptions` includes `EtcdOptions` by default. Set `o.RecommendedOptions.Etcd = nil` to disable etcd. All storage goes through the ScyllaDB `storage.Interface`.

2. **Scheme must be complete.** All three resource types (DRPlan, DRExecution, DRGroupStatus) and their List types must be registered in the scheme before the API server starts. Missing types cause runtime panics when the server tries to serialize responses.

3. **The cacher calls Watch() at startup.** When the cacher is created, it immediately calls `Watch()` with `resourceVersion=0` on the underlying storage. If the ScyllaDB storage.Interface's Watch is not working, the cacher will fail to initialize. This is why Stories 1.3 and 1.4 must be complete before this story.

4. **Do NOT implement controllers in this story.** The controller-runtime manager starts empty — no reconcilers. Controllers are added in Epic 2 (DRPlan controller) and Epic 4 (DRExecution controller). This story only proves the API server works.

5. **Spec/status split is mandatory.** Without the status subresource, controllers and users compete for the same `Update` path. The status subresource pattern ensures controllers update status without triggering spec validation, and users update spec without overwriting status. This is standard Kubernetes practice.

6. **DRExecution append-only is enforced in the status strategy, not the main strategy.** The main strategy's `PrepareForUpdate` preserves both spec and status (making spec immutable). The status strategy's `ValidateUpdate` rejects updates to completed executions. Both are needed.

7. **API server and controller-runtime use different HTTP servers.** The extension API server uses `k8s.io/apiserver`'s HTTP server (port 6443 by default). Controller-runtime uses its own HTTP server for health checks and metrics (port 8081 by default). Both are needed in the single binary. Ensure the ports don't conflict.

8. **The `GenericAPIServer.PrepareRun().Run()` call blocks.** It serves HTTP until the stop channel is closed. Run the controller-runtime manager in a separate goroutine, and let the API server run on the main goroutine (or vice versa, but one must block).

9. **Test server setup requires careful handling of TLS.** For integration tests, use loopback configuration with self-signed certs. The `k8s.io/apiserver/pkg/server/options.SecureServingOptionsWithLoopback` helper provides this. Do not try to configure the test server with production TLS settings.

10. **Do NOT implement admission webhooks in this story.** Admission webhooks are Story 2.3. The strategy-level validation in this story provides basic field validation. Webhook-based validation (VM exclusivity, namespace consistency) comes later.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 1.5 (lines 463-500)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Aggregated API Server (lines 397-399)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Registry (lines 401-410)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Single binary architecture (lines 164, 381)]
- [Source: _bmad-output/planning-artifacts/architecture.md — API Boundary (lines 516-530)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Data Flow (lines 557-576)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Cacher layer (line 184)]
- [Source: _bmad-output/planning-artifacts/architecture.md — APIService config (lines 464, 486)]
- [Source: _bmad-output/project-context.md — Single binary rule (line 70)]
- [Source: _bmad-output/project-context.md — Leader election scope (line 70)]
- [Source: _bmad-output/project-context.md — Controller ↔ API server communication (line 71)]
- [Source: _bmad-output/implementation-artifacts/1-1-project-initialization-api-type-definitions.md — API types, scheme, codegen]
- [Source: _bmad-output/implementation-artifacts/1-2-scylladb-connection-generic-kv-schema.md — ScyllaDB client, schema]
- [Source: _bmad-output/implementation-artifacts/1-3-scylladb-storage-interface-crud-operations.md — storage.Interface CRUD]
- [Source: _bmad-output/implementation-artifacts/1-4-scylladb-storage-interface-watch-via-cdc.md — Watch via CDC, cacher integration notes]
- [External: kubernetes/sample-apiserver — Reference for Aggregated API Server patterns]
- [External: k8s.io/apiserver/pkg/server — GenericAPIServer, NewRecommendedConfig]
- [External: k8s.io/apiserver/pkg/registry/generic/registry — Store, CompleteWithOptions]
- [External: k8s.io/apiserver/pkg/registry/rest — Strategy interfaces]
- [External: k8s.io/apiserver/pkg/storage/cacher — NewCacherFromConfig]
- [External: k8s.io/apiserver/pkg/server/options — RecommendedOptions, SecureServingOptions]
- [External: Daniel Mangum — K8s ASA: API Registration — https://danielmangum.com/posts/k8s-asa-api-registration/]
- [External: Daniel Mangum — K8s ASA: The Storage Interface — https://danielmangum.com/posts/k8s-asa-the-storage-interface/]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
