# Story 2b.3: Test Suite, Documentation & Requirement Updates

Status: ready-for-dev

## Story

As a team member,
I want all tests, documentation, and requirements to reflect the label convention change,
So that the codebase is consistent and future contributors understand the new design.

## Acceptance Criteria

1. **AC1 — Integration tests outside admission/engine/controller scope:** All test files in `test/integration/apiserver/`, `test/integration/rbac/`, `test/integration/storage/`, and `test/integration/replication/` construct `DRPlanSpec` without `VMSelector`. They use the new API shape where VM-to-plan association is via the `soteria.io/drplan` label on VMs, not a selector on the plan.

2. **AC2 — Controller integration tests updated:** Tests in `test/integration/controller/` (`drplan_test.go`, `drplan_consistency_test.go`) construct `DRPlanSpec` without `VMSelector` and associate VMs to plans using the `soteria.io/drplan: <planName>` label.

3. **AC3 — Codebase-wide zero references:** `grep -r 'vmSelector\|VMSelector\|ExclusivityChecker\|FindMatchingPlans\|CheckVMExclusivity\|CheckDRPlanExclusivity'` across all `pkg/`, `test/`, `cmd/`, and `config/` returns zero hits.

4. **AC4 — PRD updated:** FR1 reads: "Platform engineer can create a DRPlan by defining a wave label key and a max concurrent failovers parameter. VMs are associated to the plan by setting the `soteria.io/drplan: <planName>` label." FR3 reads: "Orchestrator automatically discovers VMs with the `soteria.io/drplan` label matching the plan name and groups them into waves based on the wave label value." FR4 reads: "VM exclusivity is structurally enforced — a Kubernetes label key can have only one value, so a VM can belong to at most one DRPlan." FR5 reads: "Platform engineer can add a VM to an existing DRPlan by setting `soteria.io/drplan: <planName>` on the VM — no plan editing required." The YAML example removes `vmSelector` and adds a comment showing the label convention. NFR15 is updated for simplified webhooks.

5. **AC5 — Architecture doc updated:** The admission webhook section reflects the simplified design (DRPlan webhook is field-only, VM webhook checks plan existence + wave consistency). The CRD JSON fields naming convention table removes `vmSelector` as an example. The `discovery.go` description says "plan-name-based" not "label selector". The package layout comments for `pkg/admission/` reflect the new responsibilities.

6. **AC6 — Project context updated:** The CRD JSON fields row removes `vmSelector`. The Labels/annotations table includes `soteria.io/drplan`. The naming conventions table is updated accordingly.

7. **AC7 — Sample CRs updated:** DRPlan samples in `config/samples/` do not contain `vmSelector`. VM samples include `soteria.io/drplan: <sample-plan-name>` in their labels. If `config/samples/` does not exist, create it with representative samples.

8. **AC8 — `make test` passes:** All unit and integration tests pass with the updated test fixtures.

## Tasks / Subtasks

- [ ] Task 1: Update integration test fixtures — apiserver (AC: #1, #3)
  - [ ] 1.1 In `test/integration/apiserver/apiserver_test.go`, remove `"vmSelector"` from all `DRPlanSpec` JSON map literals (lines ~118, ~197, ~244, ~533). The spec should only have `"waveLabel"` and `"maxConcurrentFailovers"`
  - [ ] 1.2 Where tests create VMs associated with plans, add `soteria.io/drplan: <planName>` to VM labels

- [ ] Task 2: Update integration test fixtures — rbac (AC: #1, #3)
  - [ ] 2.1 In `test/integration/rbac/rbac_test.go`, remove `"vmSelector"` from the DRPlanSpec map (line ~47). Keep `"waveLabel"` and `"maxConcurrentFailovers"` only

- [ ] Task 3: Update integration test fixtures — controller (AC: #2, #3)
  - [ ] 3.1 In `test/integration/controller/drplan_test.go`, remove `VMSelector: metav1.LabelSelector{...}` from the DRPlanSpec literal (line ~73). Instead, ensure test VMs carry `soteria.io/drplan: <planName>` labels — the DRPlan controller discovers VMs via label, not via selector
  - [ ] 3.2 In `test/integration/controller/drplan_consistency_test.go`, same change — remove `VMSelector` from DRPlanSpec (line ~54), ensure VMs have the `soteria.io/drplan` label
  - [ ] 3.3 Verify test logic still exercises discovery, wave grouping, and namespace consistency correctly with the new label-based association

- [ ] Task 4: Update integration test fixtures — storage (AC: #1, #3)
  - [ ] 4.1 In `test/integration/storage/store_test.go`, remove `VMSelector` from `DRPlanSpec` literal (line ~149)
  - [ ] 4.2 In `test/integration/storage/watch_test.go`, remove `VMSelector` from `DRPlanSpec` literal (line ~127)

- [ ] Task 5: Update integration test fixtures — replication (AC: #1, #3)
  - [ ] 5.1 In `test/integration/replication/replication_test.go`, remove `VMSelector` from `DRPlanSpec` literal (line ~540). Ensure VMs in the test carry the `soteria.io/drplan` label

- [ ] Task 6: Update PRD (AC: #4)
  - [ ] 6.1 In `_bmad-output/planning-artifacts/prd.md`, update the YAML example (lines ~78–89): remove the `vmSelector` block from the DRPlan spec, add a comment showing that VMs are associated via the `soteria.io/drplan: erp-full-stack` label
  - [ ] 6.2 Update the DRPlan CRD description (line ~286): change "vmSelector, waveLabel, maxConcurrentFailovers" to "waveLabel, maxConcurrentFailovers. VMs associated via `soteria.io/drplan` label"
  - [ ] 6.3 Update FR1 (line ~343): "Platform engineer can create a DRPlan by defining a wave label key and a max concurrent failovers parameter. VMs are associated to the plan by setting the `soteria.io/drplan: <planName>` label"
  - [ ] 6.4 Update FR3 (line ~345): "Orchestrator automatically discovers VMs with the `soteria.io/drplan` label matching the plan name and groups them into waves based on the wave label value"
  - [ ] 6.5 Update FR4 (line ~346): "VM exclusivity is structurally enforced — a Kubernetes label key can have only one value, so a VM can belong to at most one DRPlan"
  - [ ] 6.6 Update FR5 (line ~347): "Platform engineer can add a VM to an existing DRPlan by setting `soteria.io/drplan: <planName>` on the VM — no plan editing required"
  - [ ] 6.7 Update NFR15 (line ~438): "Admission webhooks validate DRPlan field constraints (waveLabel, maxConcurrentFailovers) and warn when a VM references a nonexistent DRPlan. VM exclusivity is structurally enforced by label semantics"
  - [ ] 6.8 Search for any remaining `vmSelector`, `label selector`, or `VM label selector` references in the PRD and update

- [ ] Task 7: Update architecture doc (AC: #5)
  - [ ] 7.1 In `_bmad-output/planning-artifacts/architecture.md`, update the CRD JSON fields naming convention row (line ~263): remove `vmSelector` example, replace with `waveLabel` or another current field
  - [ ] 7.2 Update the `discovery.go` description (line ~437): change "VM discovery via label selector" to "VM discovery via `soteria.io/drplan` plan-name label"
  - [ ] 7.3 Update the `pkg/admission/` package description (lines ~448–449): change "VM exclusivity, namespace consistency, label validation" to "DRPlan field validation (waveLabel, maxConcurrentFailovers). VM plan existence warning, namespace-level wave consistency"
  - [ ] 7.4 Update admission validation row in constraints table (line ~78): reflect simplified design
  - [ ] 7.5 Update the FR/NFR architectural implication table (line ~29): change "admission webhooks for exclusivity and consistency validation" to "admission webhooks for field validation and wave consistency"
  - [ ] 7.6 Search for any remaining `vmSelector` or `ExclusivityChecker` references in the architecture doc and update

- [ ] Task 8: Update project context (AC: #6)
  - [ ] 8.1 In `_bmad-output/project-context.md`, update the CRD JSON fields row (line ~79): remove `vmSelector` from the examples, replace with current fields (e.g., `waveLabel`, `maxConcurrentFailovers`)
  - [ ] 8.2 Update the Labels/annotations row (line ~80): add `soteria.io/drplan` to the examples alongside `soteria.io/wave`
  - [ ] 8.3 Update the naming conventions table CRD JSON fields row (line ~160): remove `vmSelector`, replace with `waveLabel`
  - [ ] 8.4 Update the Labels/annotations naming convention row (line ~161): add `soteria.io/drplan` to examples
  - [ ] 8.5 Search for any remaining `vmSelector` or `label selector` references and update

- [ ] Task 9: Create/update config/samples/ (AC: #7)
  - [ ] 9.1 If `config/samples/` does not exist, create the directory
  - [ ] 9.2 Create `config/samples/soteria_v1alpha1_drplan.yaml` — a DRPlan sample with only `waveLabel` and `maxConcurrentFailovers` in spec (no `vmSelector`). Add a comment explaining that VMs are associated via the `soteria.io/drplan` label
  - [ ] 9.3 Create `config/samples/vm_with_drplan_label.yaml` — a VirtualMachine sample with `soteria.io/drplan: erp-full-stack` and `soteria.io/wave: "1"` labels
  - [ ] 9.4 Create `config/samples/kustomization.yaml` listing the sample resources

- [ ] Task 10: Full codebase scan and verification (AC: #3, #8)
  - [ ] 10.1 Run `rg 'vmSelector|VMSelector' pkg/ test/ cmd/ config/` — must return zero hits (excluding `_bmad-output/` and generated files)
  - [ ] 10.2 Run `rg 'ExclusivityChecker|FindMatchingPlans|CheckVMExclusivity|CheckDRPlanExclusivity' pkg/ test/ cmd/` — must return zero hits
  - [ ] 10.3 Run `make test` — all tests pass
  - [ ] 10.4 Run `make lint-fix` followed by `make lint`
  - [ ] 10.5 Run `make manifests && make generate` — should be no-ops but verify

## Dev Notes

### Previous Story Learnings (from Stories 2b.1 and 2b.2)

- Story 2b.1 removed `VMSelector` from `DRPlanSpec` in `types.go` and updated `DiscoverVMs` to accept `planName string` instead of `metav1.LabelSelector`. All callers in `pkg/engine/`, `pkg/controller/`, and `pkg/admission/` were updated
- Story 2b.2 deleted `exclusivity.go` and `exclusivity_test.go`, simplified both webhooks, and rewrote all `test/integration/admission/` tests
- The `DRPlanSpec` Go struct no longer has a `VMSelector` field after Story 2b.1. Any test constructing a `DRPlanSpec{VMSelector: ...}` will fail to compile. This story fixes the remaining test files
- For tests that construct DRPlans via untyped JSON maps (apiserver, rbac tests), the `"vmSelector"` key is silently ignored by ScyllaDB (blob storage) but should be removed for consistency
- `apierrors.IsNotFound()` is the preferred Kubernetes API error check
- Code review pattern: always run the full codebase grep after changes to confirm zero leftover references

### Architecture Context

This is Story 3 of a 4-story refactoring epic (2b). Stories 2b.1 and 2b.2 performed the code-level refactoring. This story is the "sweep" — ensuring the entire codebase (tests, docs, samples) is consistent with the new `soteria.io/drplan` label convention. No production code changes are expected — only test fixtures, documentation, and sample files.

**The DRPlanSpec after 2b.1:**
```go
type DRPlanSpec struct {
    WaveLabel              string `json:"waveLabel"`
    MaxConcurrentFailovers int32  `json:"maxConcurrentFailovers"`
}
```
No `VMSelector` field. VMs declare plan membership via `soteria.io/drplan: <planName>` label.

**Files modified by 2b.1 (already done, do NOT touch):**
- `pkg/apis/soteria.io/v1alpha1/types.go` — VMSelector removed, DRPlanLabel constant added
- `pkg/apis/soteria.io/v1alpha1/validation.go` — validateVMSelector removed
- `pkg/engine/discovery.go` — DiscoverVMs(ctx, planName string)
- `pkg/engine/discovery_test.go` — updated
- `pkg/controller/drplan/reconciler.go` — mapVMToDRPlans O(1), vmRelevantChangePredicate updated
- `pkg/controller/drplan/reconciler_test.go` — updated
- `pkg/admission/exclusivity.go` — minimal adaptation for compilation
- `pkg/admission/drplan_validator.go` — minimal adaptation for compilation
- `pkg/admission/vm_validator.go` — minimal adaptation for compilation
- Corresponding `*_test.go` files — adapted for compilation

**Files modified by 2b.2 (already done, do NOT touch):**
- `pkg/admission/exclusivity.go` — DELETED
- `pkg/admission/exclusivity_test.go` — DELETED
- `pkg/admission/drplan_validator.go` — rewritten to field-only validator
- `pkg/admission/drplan_validator_test.go` — rewritten
- `pkg/admission/vm_validator.go` — rewritten (plan existence + wave check)
- `pkg/admission/vm_validator_test.go` — rewritten
- `pkg/admission/setup.go` — simplified wiring
- `pkg/admission/doc.go` — updated
- `cmd/soteria/main.go` — updated webhook setup calls
- `test/integration/admission/` — all three files rewritten

### Critical Implementation Details

**Typed test fixtures (controller, replication, storage tests):**

These tests construct `DRPlanSpec` using the Go struct. Since `VMSelector` is removed from the struct by Story 2b.1, these will fail to compile. Fix by removing the `VMSelector` field initialization:

Before (will not compile after 2b.1):
```go
Spec: soteriav1alpha1.DRPlanSpec{
    VMSelector: metav1.LabelSelector{
        MatchLabels: map[string]string{"app": "erp"},
    },
    WaveLabel:              "soteria.io/wave",
    MaxConcurrentFailovers: 4,
},
```

After:
```go
Spec: soteriav1alpha1.DRPlanSpec{
    WaveLabel:              "soteria.io/wave",
    MaxConcurrentFailovers: 4,
},
```

**Untyped JSON test fixtures (apiserver, rbac tests):**

These tests construct DRPlans as `map[string]any`. The `"vmSelector"` key won't cause a compile error but is now an unknown field:

Before:
```go
"spec": map[string]any{
    "vmSelector":             map[string]any{"matchLabels": map[string]any{"app": "test"}},
    "waveLabel":              "wave",
    "maxConcurrentFailovers": int64(2),
},
```

After:
```go
"spec": map[string]any{
    "waveLabel":              "wave",
    "maxConcurrentFailovers": int64(2),
},
```

**VM test fixtures — add `soteria.io/drplan` label:**

Any test that expects a DRPlan controller to discover VMs must label those VMs with `soteria.io/drplan: <planName>`. The controller no longer uses a selector — it queries VMs by exact label match.

Before (VM without plan label):
```go
vm := &kubevirtv1.VirtualMachine{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "vm-1",
        Namespace: "default",
        Labels:    map[string]string{"app": "erp", "soteria.io/wave": "1"},
    },
}
```

After (VM with plan label):
```go
vm := &kubevirtv1.VirtualMachine{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "vm-1",
        Namespace: "default",
        Labels: map[string]string{
            soteriav1alpha1.DRPlanLabel: "erp-full-stack",
            "soteria.io/wave":          "1",
        },
    },
}
```

**Controller integration tests — plan discovery depends on labels now:**

In `test/integration/controller/drplan_test.go` and `drplan_consistency_test.go`, tests create VMs with labels matching the old `vmSelector`. After the refactoring, the DRPlan controller calls `DiscoverVMs(ctx, plan.Name)` which queries `soteria.io/drplan=<planName>`. Tests must:
1. Remove `VMSelector` from DRPlanSpec
2. Add `soteria.io/drplan: <planName>` to all VM labels that should be discovered
3. Ensure VMs that should NOT be discovered do NOT have the label

**Validation tests — `validateVMSelector` is gone:**

Story 2b.1 removed `validateVMSelector` from `validation.go`. If `pkg/apis/soteria.io/v1alpha1/validation_test.go` was already updated in 2b.1 (it's in the 2b.1 task list as Task 4.8), then no changes are needed. However, verify — if any test cases still reference `vmSelector` in field paths or test names, update them.

### Files to Modify

| File | Change |
|------|--------|
| `test/integration/apiserver/apiserver_test.go` | Remove `"vmSelector"` from all DRPlanSpec JSON maps (~4 occurrences) |
| `test/integration/rbac/rbac_test.go` | Remove `"vmSelector"` from DRPlanSpec JSON map (~1 occurrence) |
| `test/integration/controller/drplan_test.go` | Remove `VMSelector` from DRPlanSpec, add `soteria.io/drplan` label to VMs |
| `test/integration/controller/drplan_consistency_test.go` | Remove `VMSelector` from DRPlanSpec, add `soteria.io/drplan` label to VMs |
| `test/integration/replication/replication_test.go` | Remove `VMSelector` from DRPlanSpec, add label to VMs |
| `test/integration/storage/store_test.go` | Remove `VMSelector` from DRPlanSpec |
| `test/integration/storage/watch_test.go` | Remove `VMSelector` from DRPlanSpec |
| `_bmad-output/planning-artifacts/prd.md` | Update YAML example, FR1, FR3, FR4, FR5, NFR15, DRPlan CRD description |
| `_bmad-output/planning-artifacts/architecture.md` | Update naming conventions, discovery desc, admission desc, FR table |
| `_bmad-output/project-context.md` | Update CRD JSON fields, Labels/annotations, naming conventions |
| `config/samples/soteria_v1alpha1_drplan.yaml` | Create — DRPlan sample without vmSelector |
| `config/samples/vm_with_drplan_label.yaml` | Create — VM sample with soteria.io/drplan label |
| `config/samples/kustomization.yaml` | Create — list sample resources |

### Files NOT to Modify (already handled by 2b.1 or 2b.2)

- `pkg/apis/soteria.io/v1alpha1/types.go` — VMSelector already removed (2b.1)
- `pkg/apis/soteria.io/v1alpha1/validation.go` — validateVMSelector already removed (2b.1)
- `pkg/engine/discovery.go` — already updated (2b.1)
- `pkg/engine/discovery_test.go` — already updated (2b.1)
- `pkg/controller/drplan/reconciler.go` — already updated (2b.1)
- `pkg/controller/drplan/reconciler_test.go` — already updated (2b.1)
- `pkg/admission/` — all files already updated or deleted (2b.1 + 2b.2)
- `test/integration/admission/` — all files already rewritten (2b.2)
- `cmd/soteria/main.go` — already updated (2b.2)

### Existing Code Patterns to Follow

- **Import alias:** `soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"` — use for `DRPlanLabel` constant
- **DRPlanLabel constant:** `soteriav1alpha1.DRPlanLabel` = `"soteria.io/drplan"` — prefer the constant over the string literal in Go test files. In YAML samples and docs, use the string `soteria.io/drplan`
- **Test naming:** `TestFunction_Scenario_Expected` — preserve existing test function names unless they reference vmSelector in the name
- **JSON map tests:** apiserver and rbac tests use `map[string]any` — just remove the `"vmSelector"` key, no type imports needed
- **Logging style:** `logger.Info("msg", "key", val)` — only relevant if test helpers log

### Build Commands

```bash
make test         # Unit + envtest tests (primary verification)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make manifests    # Should be a no-op — run for safety
make generate     # Should be a no-op — run for safety
```

### Project Structure Notes

- `test/integration/` — integration tests organized by domain (apiserver, rbac, controller, admission, storage, replication)
- `_bmad-output/planning-artifacts/` — PRD, architecture, and other planning docs
- `_bmad-output/project-context.md` — AI agent context file, must stay accurate
- `config/samples/` — example CRs for users (currently does not exist — create it)
- No production Go code changes expected in this story — only test fixtures, docs, and samples
- `pkg/apis/soteria.io/v1alpha1/validation_test.go` may need updates if Story 2b.1 left vmSelector references — verify and fix if needed

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2b.3] — Full acceptance criteria
- [Source: _bmad-output/implementation-artifacts/2b-1-label-convention-api-discovery-controller.md] — Story 2b.1 changes (VMSelector removal, DRPlanLabel constant, DiscoverVMs signature)
- [Source: _bmad-output/implementation-artifacts/2b-2-webhook-simplification.md] — Story 2b.2 changes (ExclusivityChecker deletion, webhook simplification)
- [Source: _bmad-output/planning-artifacts/prd.md#FR1-FR5] — Current FR wording to update (lines 339-350)
- [Source: _bmad-output/planning-artifacts/prd.md#YAML-example] — DRPlan YAML example to update (lines 78-89)
- [Source: _bmad-output/planning-artifacts/architecture.md#naming-conventions] — CRD JSON fields table (line 263)
- [Source: _bmad-output/planning-artifacts/architecture.md#package-layout] — pkg/admission/ description (lines 448-449)
- [Source: _bmad-output/project-context.md#CRD-JSON-fields] — vmSelector reference to update (line 79)
- [Source: test/integration/apiserver/apiserver_test.go] — vmSelector JSON keys at lines 118, 197, 244, 533
- [Source: test/integration/rbac/rbac_test.go] — vmSelector JSON key at line 47
- [Source: test/integration/controller/drplan_test.go] — VMSelector struct field at line 73
- [Source: test/integration/controller/drplan_consistency_test.go] — VMSelector struct field at line 54
- [Source: test/integration/replication/replication_test.go] — VMSelector struct field at line 540
- [Source: test/integration/storage/store_test.go] — VMSelector struct field at line 149
- [Source: test/integration/storage/watch_test.go] — VMSelector struct field at line 127

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
