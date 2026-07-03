# Story 15.4: ShadowPV CRD Definition & ScyllaDB Storage

Status: done

## Story

As a developer,
I want a cluster-scoped ShadowPV CRD stored in ScyllaDB (same backend as DRPlan/DRExecution),
so that PV manifests can be shared between clusters for cross-site volume provisioning.

## Acceptance Criteria

**AC1: ShadowPV API type**
Given the `pkg/apis/soteria.io/v1alpha1/` package
When the ShadowPV type is defined
Then it is cluster-scoped
And the spec contains `PVs []ShadowPVEntry` with each entry having `ClusterName`, `PVName`, and `PV corev1.PersistentVolumeSpec`
And the status contains `Conditions []metav1.Condition`
And `ShadowPV` and `ShadowPVList` are registered in the scheme

**AC2: DRPlan OwnerReference via PrepareForCreate**
Given a ShadowPV is created with a `soteria.io/drplan` label
When `PrepareForCreate` runs in the strategy
Then it resolves the DRPlan UID via the injected `rest.Getter`
And sets an OwnerReference to the DRPlan (`Controller=true`, `BlockOwnerDeletion=true`)
And deleting the DRPlan cascades to delete the ShadowPV

**AC3: ScyllaDB storage registration**
Given the aggregated API server
When ShadowPV storage is registered
Then it uses the same ScyllaDB-backed REST storage as DRPlan and DRExecution
And CRUD operations, watch, and list work correctly
And cross-site replication via ScyllaDB CDC is automatic

**AC4: Label indexing**
Given ShadowPV resources
When queried by label
Then the `soteria.io/drplan` label is indexed for efficient lookup
And ShadowPVs for a given DRPlan can be listed via label selector

**AC5: Printer columns**
Given the ShadowPV custom TableConvertor
When `kubectl get shadowpvs` is run
Then columns show: NAME, PLAN, PV-COUNT, AGE

**AC6: Validation**
Given a ShadowPV create/update request
When validation runs
Then `spec.pvs[].clusterName` is required and non-empty
And `spec.pvs[].pvName` is required and non-empty
And no duplicate `(clusterName, pvName)` pairs within a single ShadowPV
And the `spec.pvs` list is required (at least one entry on create)

## Tasks / Subtasks

- [x] Task 1: Define ShadowPV API types (AC: 1)
  - [x] 1.1: Create `pkg/apis/soteria.io/v1alpha1/types_shadowpv.go` with `ShadowPV`, `ShadowPVList`, `ShadowPVSpec`, `ShadowPVStatus`, `ShadowPVEntry` types
  - [x] 1.2: Add kubebuilder markers: `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`, `+kubebuilder:resource:scope=Cluster`
  - [x] 1.3: Register `ShadowPV` and `ShadowPVList` in `register.go`'s `addKnownTypes`
  - [x] 1.4: Run `make manifests generate` to regenerate DeepCopy and OpenAPI

- [x] Task 2: Add ShadowPV validation (AC: 6)
  - [x] 2.1: Add `ValidateShadowPV(spv *ShadowPV) field.ErrorList` to `validation.go`
  - [x] 2.2: Add `ValidateShadowPVUpdate(new, old *ShadowPV) field.ErrorList` to `validation.go`
  - [x] 2.3: Validate each entry's `clusterName` and `pvName` are non-empty
  - [x] 2.4: Validate no duplicate `(clusterName, pvName)` pairs
  - [x] 2.5: Add validation unit tests in `validation_test.go`

- [x] Task 3: Create ShadowPV registry package (AC: 2, 3, 4, 5)
  - [x] 3.1: Create `pkg/registry/shadowpv/doc.go`
  - [x] 3.2: Create `pkg/registry/shadowpv/strategy.go` with `shadowpvStrategy`, `GetAttrs`, `MatchShadowPV`, status strategy
  - [x] 3.3: Implement `PrepareForCreate` — initialize status, set Generation, resolve DRPlan OwnerReference via `planGetter`
  - [x] 3.4: Implement `PrepareForUpdate` — preserve status on main-resource updates
  - [x] 3.5: Implement `Validate` and `ValidateUpdate` — delegate to `ValidateShadowPV`/`ValidateShadowPVUpdate`
  - [x] 3.6: Implement `ShadowPVTableConvertor` with NAME, PLAN, PV-COUNT, AGE columns
  - [x] 3.7: Create `pkg/registry/shadowpv/storage.go` with `NewREST` (main store + StatusREST)
  - [x] 3.8: Add `SetPlanStorage(rest.Getter)` for OwnerReference resolution injection

- [x] Task 4: Register ShadowPV in the aggregated API server (AC: 3)
  - [x] 4.1: Add `shadowpvregistry` import to `pkg/apiserver/apiserver.go`
  - [x] 4.2: Call `shadowpvregistry.NewREST` after DRPlan storage creation
  - [x] 4.3: Inject DRPlan storage into ShadowPV strategy via `SetPlanStorage`
  - [x] 4.4: Register `"shadowpvs"` and `"shadowpvs/status"` in `v1alpha1storage` map

- [x] Task 5: Write tests (AC: 1, 2, 3, 4, 5, 6)
  - [x] 5.1: Add strategy unit tests in `pkg/registry/shadowpv/strategy_test.go` — PrepareForCreate (OwnerReference, status init, generation), PrepareForUpdate (status preserved), GetAttrs, table convertor
  - [x] 5.2: Add storage integration test in `pkg/registry/shadowpv/storage_test.go` — CRUD, list with label selector, status subresource update
  - [x] 5.3: Add OwnerReference tests (correct UID, nil getter graceful degradation, plan-not-found graceful degradation)

- [x] Task 6: Update documentation and finalize
  - [x] 6.1: Update `pkg/apis/soteria.io/v1alpha1/doc.go` — add ShadowPV to package overview
  - [x] 6.2: Run `make lint-fix` — fix any issues
  - [x] 6.3: Run `make test` — all unit + envtest tests pass
  - [x] 6.4: Verify Tier 1/2/3 doc compliance

## Dev Notes

### ShadowPV Type Definition

Create `pkg/apis/soteria.io/v1alpha1/types_shadowpv.go`:

```go
// ShadowPV shares PersistentVolume manifests between clusters for cross-site
// volume provisioning. Each entry represents a PV discovered on one cluster
// that should be pre-provisioned on the peer cluster (with pool-ID rewrite
// for Ceph). Named after the DRPlan + VolumeGroup it represents, e.g.,
// "<plan-name>-<vg-name>".
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
type ShadowPV struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   ShadowPVSpec   `json:"spec"`
    Status ShadowPVStatus `json:"status,omitempty"`
}

type ShadowPVSpec struct {
    // PVs is the list of PV entries from different clusters.
    // +listType=atomic
    PVs []ShadowPVEntry `json:"pvs"`
}

type ShadowPVEntry struct {
    // ClusterName identifies which cluster published this PV entry.
    ClusterName string `json:"clusterName"`
    // PVName is the desired PV name for creation on remote clusters.
    PVName string `json:"pvName"`
    // PV holds the PersistentVolume spec (not full PV — avoids unnecessary metadata).
    PV corev1.PersistentVolumeSpec `json:"pv"`
}

type ShadowPVStatus struct {
    // Conditions represent the latest observations of the ShadowPV state.
    // Used by consumer controller to report PV conflicts and provisioning status.
    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type ShadowPVList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []ShadowPV `json:"items"`
}
```

**Critical:** Import `corev1 "k8s.io/api/core/v1"` for `PersistentVolumeSpec`. The `corev1` package is already imported in `types.go` (used by DRPlan's VM types). Using `PersistentVolumeSpec` (not full `PersistentVolume`) avoids storing unnecessary ObjectMeta — the consumer controller creates a new PV with this spec.

### Strategy Pattern — Following DRExecution OwnerReference

The ShadowPV strategy's `PrepareForCreate` resolves the DRPlan UID and sets OwnerReference, identical to `pkg/registry/drexecution/strategy.go` lines 60-118. Key differences:

- ShadowPV reads the plan name from the `soteria.io/drplan` **label** (not `spec.planName` like DRExecution)
- ShadowPV's plan name is always in the label because the publisher controller sets it (Story 15.5)

```go
func (s shadowpvStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
    spv := obj.(*soteriav1alpha1.ShadowPV)
    spv.Status = soteriav1alpha1.ShadowPVStatus{}
    spv.Generation = 1

    setOwnerReference(ctx, spv)
}

func setOwnerReference(ctx context.Context, spv *soteriav1alpha1.ShadowPV) {
    if Strategy.planGetter == nil {
        return
    }

    planName := spv.Labels[drivers.LabelDRPlan]
    if planName == "" {
        return
    }

    planObj, err := Strategy.planGetter.Get(ctx, planName, &metav1.GetOptions{})
    if err != nil {
        logger := log.FromContext(ctx)
        logger.Error(err, "Could not fetch DRPlan for OwnerReference, proceeding without it",
            "planName", planName)
        return
    }

    plan, ok := planObj.(*soteriav1alpha1.DRPlan)
    if !ok {
        logger := log.FromContext(ctx)
        logger.Error(nil, "Plan storage returned unexpected type, proceeding without OwnerReference",
            "planName", planName, "type", fmt.Sprintf("%T", planObj))
        return
    }

    spv.OwnerReferences = []metav1.OwnerReference{
        {
            APIVersion:         soteriav1alpha1.SchemeGroupVersion.String(),
            Kind:               "DRPlan",
            Name:               plan.Name,
            UID:                plan.UID,
            Controller:         ptr.To(true),
            BlockOwnerDeletion: ptr.To(true),
        },
    }
}
```

### Storage Pattern — Following DRPlan/DRExecution

`pkg/registry/shadowpv/storage.go` follows the exact `pkg/registry/drplan/storage.go` pattern:

```go
func NewREST(
    scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter,
) (*genericregistry.Store, *StatusREST, error) {
    tc := ShadowPVTableConvertor{}
    store := &genericregistry.Store{
        NewFunc:                   func() runtime.Object { return &soteriav1alpha1.ShadowPV{} },
        NewListFunc:               func() runtime.Object { return &soteriav1alpha1.ShadowPVList{} },
        DefaultQualifiedResource:  soteriav1alpha1.Resource("shadowpvs"),
        SingularQualifiedResource: soteriav1alpha1.Resource("shadowpv"),

        CreateStrategy: Strategy,
        UpdateStrategy: Strategy,
        DeleteStrategy: Strategy,
        TableConvertor: tc,
    }

    options := &generic.StoreOptions{
        RESTOptions: optsGetter,
        AttrFunc:    GetAttrs,
    }
    if err := store.CompleteWithOptions(options); err != nil {
        return nil, nil, err
    }

    statusStore := *store
    statusStore.UpdateStrategy = StatusStrategy

    return store, &StatusREST{store: &statusStore}, nil
}
```

**No `AuditProtectedREST` wrapper** — ShadowPVs are operational resources (not audit records), so standard delete is fine. Cascade delete via OwnerReference is the primary deletion path.

**No `DRPlanTableConvertor`-style cross-resource lookup** — the ShadowPV table convertor is self-contained (PLAN column from label, PV-COUNT from `len(spec.pvs)`).

### Table Convertor

```go
type ShadowPVTableConvertor struct{}

var shadowpvTableColumns = []metav1.TableColumnDefinition{
    {Name: "Name", Type: "string", Format: "name"},
    {Name: "Plan", Type: "string"},
    {Name: "PV Count", Type: "integer"},
    {Name: "Age", Type: "string"},
}

func (ShadowPVTableConvertor) ConvertToTable(
    _ context.Context, object runtime.Object, _ runtime.Object,
) (*metav1.Table, error) {
    table := &metav1.Table{ColumnDefinitions: shadowpvTableColumns}

    switch obj := object.(type) {
    case *soteriav1alpha1.ShadowPV:
        table.Rows = append(table.Rows, shadowpvToRow(obj))
    case *soteriav1alpha1.ShadowPVList:
        for i := range obj.Items {
            table.Rows = append(table.Rows, shadowpvToRow(&obj.Items[i]))
        }
    }

    return table, nil
}

func shadowpvToRow(spv *soteriav1alpha1.ShadowPV) metav1.TableRow {
    return metav1.TableRow{
        Object: runtime.RawExtension{Object: spv},
        Cells: []any{
            spv.Name,
            spv.Labels[drivers.LabelDRPlan],
            len(spv.Spec.PVs),
            translateTimestampSince(spv.CreationTimestamp),
        },
    }
}
```

### GetAttrs — Label Indexing

```go
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
    spv, ok := obj.(*soteriav1alpha1.ShadowPV)
    if !ok {
        return nil, nil, field.Invalid(field.NewPath(""), obj, "expected ShadowPV")
    }
    return spv.Labels, fields.Set{
        "metadata.name": spv.Name,
    }, nil
}
```

Returning `spv.Labels` enables label-based filtering via the standard `storage.SelectionPredicate`. The ScyllaDB storage layer indexes label selectors automatically — the `soteria.io/drplan` label is efficiently queryable via `client.MatchingLabels{drivers.LabelDRPlan: planName}` or equivalently via `label=soteria.io/drplan=<planName>` in kubectl.

### Validation

```go
func ValidateShadowPV(spv *ShadowPV) field.ErrorList {
    allErrs := field.ErrorList{}
    specPath := field.NewPath("spec")
    pvsPath := specPath.Child("pvs")

    if len(spv.Spec.PVs) == 0 {
        allErrs = append(allErrs, field.Required(pvsPath, "at least one PV entry required"))
    }

    seen := make(map[string]struct{})
    for i, entry := range spv.Spec.PVs {
        entryPath := pvsPath.Index(i)
        if entry.ClusterName == "" {
            allErrs = append(allErrs, field.Required(entryPath.Child("clusterName"), ""))
        }
        if entry.PVName == "" {
            allErrs = append(allErrs, field.Required(entryPath.Child("pvName"), ""))
        }
        key := entry.ClusterName + "/" + entry.PVName
        if _, dup := seen[key]; dup {
            allErrs = append(allErrs, field.Duplicate(
                entryPath,
                fmt.Sprintf("(%s, %s)", entry.ClusterName, entry.PVName),
            ))
        }
        seen[key] = struct{}{}
    }

    return allErrs
}

func ValidateShadowPVUpdate(newSPV, oldSPV *ShadowPV) field.ErrorList {
    allErrs := ValidateShadowPV(newSPV)

    const drplanLabel = "soteria.io/drplan"
    if newSPV.Labels[drplanLabel] != oldSPV.Labels[drplanLabel] {
        allErrs = append(allErrs, field.Forbidden(
            field.NewPath("metadata", "labels").Key(drplanLabel),
            "field is immutable",
        ))
    }

    return allErrs
}
```

**Immutability:** The `soteria.io/drplan` label is immutable after creation — it is bound to the OwnerReference set during `PrepareForCreate`, and changing it would break garbage-collection cascade semantics. ShadowPV spec entries (PVs list) remain freely mutable (the publisher adds/removes entries as VR/VGR CRs appear and disappear). `PrepareForUpdate` preserves `OwnerReferences` from the old object, and the status strategy preserves both `Labels` and `OwnerReferences`.

### API Server Registration

Add to `pkg/apiserver/apiserver.go` after the DRExecution storage block:

```go
import shadowpvregistry "github.com/soteria-project/soteria/pkg/registry/shadowpv"

// In New(), after drexecStore creation:

shadowpvregistry.Strategy.SetPlanStorage(drplanStore)

shadowpvStore, shadowpvStatusStore, err := shadowpvregistry.NewREST(soteriainstall.Scheme, optsGetter)
if err != nil {
    return nil, fmt.Errorf("creating ShadowPV storage: %w", err)
}
v1alpha1storage["shadowpvs"] = shadowpvStore
v1alpha1storage["shadowpvs/status"] = shadowpvStatusStore
```

**Ordering matters:** `SetPlanStorage(drplanStore)` must run before `NewREST` because `Strategy` is a package-level var that needs the getter before any create operations.

### Scheme Registration

Add to `pkg/apis/soteria.io/v1alpha1/register.go` in `addKnownTypes`:

```go
scheme.AddKnownTypes(SchemeGroupVersion,
    &DRPlan{},
    &DRPlanList{},
    &DRExecution{},
    &DRExecutionList{},
    &ShadowPV{},
    &ShadowPVList{},
)
```

### codegen After Type Changes

After creating `types_shadowpv.go` and updating `register.go`:

```bash
make manifests generate
```

This regenerates:
- `zz_generated.deepcopy.go` — DeepCopy methods for `ShadowPV`, `ShadowPVList`, `ShadowPVSpec`, `ShadowPVStatus`, `ShadowPVEntry`
- `zz_generated.openapi.go` — OpenAPI definitions for the new types

### File Inventory

| File | Action | Lines Changed (est.) |
|------|--------|---------------------|
| `pkg/apis/soteria.io/v1alpha1/types_shadowpv.go` | NEW — ShadowPV, ShadowPVList, ShadowPVSpec, ShadowPVStatus, ShadowPVEntry | ~60 |
| `pkg/apis/soteria.io/v1alpha1/register.go` | MODIFY — add ShadowPV/ShadowPVList to addKnownTypes | ~4 |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | MODIFY — add ValidateShadowPV, ValidateShadowPVUpdate | ~45 |
| `pkg/apis/soteria.io/v1alpha1/validation_test.go` | MODIFY — add ShadowPV validation tests | ~80 |
| `pkg/apis/soteria.io/v1alpha1/doc.go` | MODIFY — mention ShadowPV in package overview | ~2 |
| `pkg/registry/shadowpv/doc.go` | NEW — package documentation | ~10 |
| `pkg/registry/shadowpv/strategy.go` | NEW — strategy, GetAttrs, MatchShadowPV, table convertor | ~180 |
| `pkg/registry/shadowpv/storage.go` | NEW — NewREST, StatusREST | ~80 |
| `pkg/registry/shadowpv/strategy_test.go` | NEW — strategy unit tests | ~120 |
| `pkg/registry/shadowpv/storage_test.go` | NEW — storage integration tests | ~100 |
| `pkg/apiserver/apiserver.go` | MODIFY — register ShadowPV storage, inject planGetter | ~15 |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | AUTO — regenerated | auto |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` | AUTO — regenerated | auto |

**Total: ~5 new files, ~4 modified files, ~696 lines (excluding auto-generated)**

### Key Constraints

- **Do NOT use kubebuilder scaffolding** — this project uses `k8s.io/apiserver` with ScyllaDB backend, not kubebuilder's controller-runtime CRD approach. The kubebuilder markers are only for codegen (DeepCopy, OpenAPI), not for CRD generation
- **Do NOT create a controller** — Story 15.4 is API type + storage only. Controllers come in Stories 15.5 (publisher) and 15.6 (consumer)
- **Do NOT create CRD YAML** — ShadowPV is served by the aggregated API server, not registered as a standalone CRD. `config/crd/bases/` does not apply
- **Do NOT add RBAC markers** — no controller in this story; RBAC markers will be added by the publisher/consumer controllers in 15.5/15.6
- **Do NOT implement admission plugin changes** — ShadowPV validation is in-strategy only (via `Validate`/`ValidateUpdate` on the strategy). No admission plugin cross-object checks needed for ShadowPV in Epic 15
- **Do NOT add `corev1.PersistentVolume` to scheme** — `corev1.PersistentVolumeSpec` is a plain struct (not a runtime.Object), so no scheme registration needed. The `corev1` scheme registration is already done via `k8s.io/client-go/kubernetes/scheme` in the API server initialization
- **Error condition bubbling to DRPlan is out of scope** — ShadowPV status conditions are for the consumer controller (15.6) to record PV conflicts. DRPlan does not watch ShadowPV conditions in Epic 15
- **ShadowPV is NOT a Kubernetes CRD** — it is an aggregated API resource backed by ScyllaDB, like DRPlan and DRExecution. The resource type name is `shadowpvs` (plural), kind is `ShadowPV`
- **`+listType=atomic` on PVs** — the PVs list is replaced atomically on each update (publisher sends the full list). This avoids strategic merge patch complexity on individual entries

### Naming Convention

ShadowPV resources are named `<plan-name>-<vg-name>` by the publisher controller (Story 15.5). Examples:
- `erp-full-stack-vm-billing` (plan "erp-full-stack", VG "vm-billing")
- `erp-full-stack-ns-accounting` (plan "erp-full-stack", VG "ns-accounting")

The `soteria.io/drplan` label value is the plan name (e.g., `erp-full-stack`).

### Label Constants

The `soteria.io/drplan` label constant already exists at `pkg/drivers/constants.go` as `LabelDRPlan`. Import from `pkg/drivers` — do NOT duplicate the constant.

### Downstream Impact on Stories 15.5 and 15.6

Story 15.5 (Publisher Controller):
- Creates ShadowPV resources with `soteria.io/drplan` label → triggers OwnerReference via PrepareForCreate
- Calls `PATCH` on `shadowpvs` to add/remove entries
- Uses `client.MatchingLabels{drivers.LabelDRPlan: planName}` to find ShadowPVs for a plan

Story 15.6 (Consumer Controller):
- Watches ShadowPV resources via controller-runtime `.Watches()`
- Reads `spec.pvs` entries, filters by `clusterName != localSite`
- Records conflicts in `status.conditions` via status subresource update
- Uses `shadowpvs/status` endpoint for condition updates

### Testing Strategy

**Validation tests (`validation_test.go`):**

| Test | Input | Expected |
|------|-------|----------|
| `TestValidateShadowPV_Valid` | Valid spec with 2 entries from different clusters | No errors |
| `TestValidateShadowPV_EmptyPVs` | Empty PVs list | Required error on `spec.pvs` |
| `TestValidateShadowPV_EmptyClusterName` | Entry with empty clusterName | Required error on `spec.pvs[0].clusterName` |
| `TestValidateShadowPV_EmptyPVName` | Entry with empty pvName | Required error on `spec.pvs[0].pvName` |
| `TestValidateShadowPV_DuplicateEntries` | Two entries with same (clusterName, pvName) | Duplicate error |
| `TestValidateShadowPV_SameClusterDifferentPVs` | Same cluster, different pvNames | No errors |
| `TestValidateShadowPV_DifferentClustersSamePV` | Different clusters, same pvName | No errors (valid — same PV on both sites) |

**Strategy tests (`strategy_test.go`):**

| Test | Scenario | Assertions |
|------|----------|------------|
| `TestPrepareForCreate_InitializesStatus` | Valid ShadowPV | Status zeroed, Generation=1 |
| `TestPrepareForCreate_SetsOwnerReference` | ShadowPV with drplan label, plan exists | OwnerReference set with correct UID, Controller=true, BlockOwnerDeletion=true |
| `TestPrepareForCreate_NilPlanGetter` | No plan getter injected | No OwnerReference, no panic |
| `TestPrepareForCreate_PlanNotFound` | Plan getter returns NotFound | No OwnerReference, log warning |
| `TestPrepareForCreate_NoDRPlanLabel` | ShadowPV without drplan label | No OwnerReference |
| `TestPrepareForUpdate_PreservesStatus` | Update with modified status | Status unchanged from old object |
| `TestGetAttrs_ReturnsLabels` | ShadowPV with labels | Labels returned for filtering |
| `TestTableConvertor_SingleResource` | Single ShadowPV | Correct row: name, plan label, PV count, age |
| `TestTableConvertor_List` | ShadowPVList with 3 items | 3 rows with correct data |
| `TestNamespaceScoped_ReturnsFalse` | Strategy method | Returns false (cluster-scoped) |

**Storage integration tests (`storage_test.go`):**

| Test | Scenario | Assertions |
|------|----------|------------|
| `TestShadowPV_CRUD` | Create, Get, Update, Delete | All operations succeed, status subresource works |
| `TestShadowPV_ListByLabel` | Create 2 ShadowPVs for different plans, list by label | Only matching ShadowPVs returned |
| `TestShadowPV_StatusSubresource` | Update status conditions | Status updated, spec unchanged |
| `TestShadowPV_ValidationReject` | Create with empty pvs | Validation error returned |
| `TestShadowPV_OwnerReference` | Create ShadowPV when DRPlan exists | OwnerReference set correctly |

### Previous Story Intelligence

Stories 15.1-15.3 modified the StorageProvider driver framework and engine layer — they did not touch the API types or registry packages. The last changes to `pkg/apis/` were in Story 15.2 (added `ResyncTimeout` to DRPlanSpec). The last registry changes were in Story 13.1 (DRExecution OwnerReference).

Key patterns reused from prior stories:
- **OwnerReference via PrepareForCreate** — Story 13.1 established this pattern for DRExecution→DRPlan. ShadowPV reuses the exact same `rest.Getter` injection + graceful degradation approach
- **Label indexing** — DRPlan and DRExecution both return `labels.Set` from `GetAttrs`, enabling label-based queries via the ScyllaDB storage layer
- **Table convertor** — both DRPlan and DRExecution have custom `TableConvertor` implementations. ShadowPV follows the simpler DRExecution pattern (no cross-resource lookup needed)
- **StatusREST** — identical pattern across DRPlan and DRExecution
- **Cluster-scoped** — DRPlan is cluster-scoped (`NamespaceScoped() bool { return false }`). ShadowPV follows the same pattern since PVs are cluster-scoped

### Project Structure Notes

- `pkg/apis/soteria.io/v1alpha1/types_shadowpv.go` — separate type file follows existing separation (main types in `types.go`, ShadowPV types in new file). This keeps the existing `types.go` untouched and reduces merge conflicts
- `pkg/registry/shadowpv/` — new package follows `pkg/registry/drplan/` and `pkg/registry/drexecution/` structure (doc.go, strategy.go, storage.go)
- `pkg/apiserver/apiserver.go` — the only existing file that needs modification outside tests and types
- No `config/crd/bases/` file — ShadowPV is aggregated API, not standalone CRD
- No `internal/controller/` files — controller work is in Stories 15.5 and 15.6

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.4 — acceptance criteria and technical notes]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.5 — downstream publisher requirements]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 15.6 — downstream consumer requirements, PV conflict conditions]
- [Source: pkg/registry/drplan/storage.go — NewREST pattern with StatusREST]
- [Source: pkg/registry/drplan/strategy.go — strategy, GetAttrs, table convertor, status strategy]
- [Source: pkg/registry/drexecution/strategy.go — OwnerReference via PrepareForCreate, SetPlanStorage]
- [Source: pkg/registry/drexecution/storage.go — DRExecutionTableConvertor, StatusREST]
- [Source: pkg/apiserver/apiserver.go — resource registration, planGetter injection ordering]
- [Source: pkg/apis/soteria.io/v1alpha1/register.go — addKnownTypes scheme registration]
- [Source: pkg/apis/soteria.io/v1alpha1/validation.go — ValidateDRPlan field validation pattern]
- [Source: pkg/drivers/constants.go — LabelDRPlan constant]
- [Source: _bmad-output/project-context.md — aggregated API server architecture, ScyllaDB storage, testing rules]
- [Source: _bmad-output/implementation-artifacts/15-1-resyncvolume-driver-method-csi-extension.md — Epic 15 context]
- [Source: _bmad-output/implementation-artifacts/15-3-reprotect-handler-simplification-real-storage.md — Epic 15 context]

## Dev Agent Record

### Agent Model Used

Opus 4.6

### Debug Log References

None — clean implementation with no debugging required.

### Completion Notes List

- All 6 tasks and 28 subtasks completed successfully
- ShadowPV API type defined as cluster-scoped with ShadowPVSpec (PVs list), ShadowPVStatus (Conditions), and ShadowPVEntry (ClusterName, PVName, PV corev1.PersistentVolumeSpec)
- Validation: required PVs list, required clusterName/pvName per entry, duplicate (clusterName, pvName) detection, drplan label immutability on update
- Registry package follows existing DRPlan/DRExecution patterns: strategy with PrepareForCreate (OwnerReference via rest.Getter), PrepareForUpdate (status + OwnerReferences preservation), StatusStrategy (spec + labels + OwnerReferences preservation), GetAttrs (label indexing), ShadowPVTableConvertor (NAME, PLAN, PV-COUNT, AGE)
- OwnerReference graceful degradation: nil planGetter, plan-not-found, and missing label all skip OwnerReference without error
- Registered in apiserver.go with SetPlanStorage injection before NewREST
- 36 unit tests total: 13 validation tests, 23 strategy/storage tests
- 4 integration tests added: ShadowPV CRUD, label-selector list, status subresource, drplan label immutability
- Zero lint issues on changed packages, zero test regressions across full suite
- DeepCopy and OpenAPI regenerated via `make manifests generate`

### Change Log

- 2026-07-03: Story 15.4 implemented — ShadowPV CRD definition and ScyllaDB storage
- 2026-07-03: Review fixes applied — drplan label immutability on update, OwnerReferences preservation in PrepareForUpdate and status strategy, integration tests added

### File List

**New files:**
- `pkg/apis/soteria.io/v1alpha1/types_shadowpv.go` — ShadowPV, ShadowPVList, ShadowPVSpec, ShadowPVStatus, ShadowPVEntry types
- `pkg/registry/shadowpv/doc.go` — package documentation
- `pkg/registry/shadowpv/strategy.go` — strategy, GetAttrs, MatchShadowPV, status strategy, table convertor
- `pkg/registry/shadowpv/storage.go` — NewREST, StatusREST
- `pkg/registry/shadowpv/strategy_test.go` — strategy unit tests (14 tests)
- `pkg/registry/shadowpv/storage_test.go` — storage-level tests (6 tests)

**Modified files:**
- `pkg/apis/soteria.io/v1alpha1/register.go` — added ShadowPV/ShadowPVList to addKnownTypes
- `pkg/apis/soteria.io/v1alpha1/validation.go` — added ValidateShadowPV, ValidateShadowPVUpdate (drplan label immutability)
- `pkg/apis/soteria.io/v1alpha1/validation_test.go` — added ShadowPV validation tests (13 tests)
- `pkg/apis/soteria.io/v1alpha1/doc.go` — added ShadowPV to package overview
- `pkg/apiserver/apiserver.go` — registered ShadowPV storage, injected planGetter
- `test/integration/apiserver/apiserver_test.go` — added ShadowPV to discovery/OpenAPI checks, added CRUD/label-selector/status/immutability integration tests (4 tests)

**Auto-generated files:**
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — DeepCopy methods for ShadowPV types
- `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` — OpenAPI definitions for ShadowPV types

### Review Findings

- [x] [Review][Patch] ShadowPV plan linkage can drift after creation [pkg/registry/shadowpv/strategy.go:105]
- [x] [Review][Patch] Missing real storage-path coverage for CRUD, label-selector list, and status updates [pkg/registry/shadowpv/storage_test.go:34]
