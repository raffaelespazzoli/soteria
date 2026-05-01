# Story 8.1: Remove `waveLabel` from `DRPlanSpec`

Status: ready-for-dev

## Story

As a platform engineer,
I want the wave label key to be a fixed convention (`soteria.io/wave`) rather than a configurable spec field,
So that the API is simpler and there is no ambiguity about which label assigns VMs to waves.

## Acceptance Criteria

1. **AC1 — Constant added, field removed:** `DRPlanSpec.WaveLabel` field is deleted from `pkg/apis/soteria.io/v1alpha1/types.go`. A new exported constant `WaveLabel = "soteria.io/wave"` is added adjacent to `DRPlanLabel`. `make manifests && make generate` succeed.

2. **AC2 — `GroupByWave` uses constant:** `GroupByWave` in `pkg/engine/discovery.go` no longer accepts a `waveLabel` parameter — it uses the `WaveLabel` constant directly. All callers updated.

3. **AC3 — `ResolveVolumeGroups` uses constant:** `ResolveVolumeGroups` in `pkg/engine/consistency.go` no longer accepts a `waveLabel` parameter — it uses the `WaveLabel` constant directly. All callers updated.

4. **AC4 — `buildChunkInput` uses constant:** The private `buildChunkInput` in `pkg/engine/executor.go` no longer accepts a `waveLabel` parameter — it uses the `WaveLabel` constant directly.

5. **AC5 — Admission webhook uses constant:** `pkg/admission/vm_validator.go` uses the `WaveLabel` constant instead of reading `plan.Spec.WaveLabel`.

6. **AC6 — Strategy strips field (backward compat):** `PrepareForCreate` and `PrepareForUpdate` in `pkg/registry/drplan/strategy.go` silently zero out `plan.Spec.WaveLabel` so old clients sending the field do not see validation errors or stale data persisted.

7. **AC7 — Validation removed:** The `waveLabel == ""` required check in `pkg/apis/soteria.io/v1alpha1/validation.go` is deleted (field no longer exists).

8. **AC8 — Sample YAML updated:** `config/samples/soteria_v1alpha1_drplan.yaml` no longer contains `waveLabel`. A comment explains the wave label is always `soteria.io/wave`.

9. **AC9 — All tests pass:** All unit and integration tests pass with zero regressions.

## Tasks / Subtasks

- [ ] Task 1: Add constant, remove field from DRPlanSpec (AC: #1)
  - [ ] 1.1 In `pkg/apis/soteria.io/v1alpha1/types.go` (line ~57), add `WaveLabel = "soteria.io/wave"` constant below `DRPlanLabel`
  - [ ] 1.2 Remove `WaveLabel string \`json:"waveLabel"\`` field from `DRPlanSpec` struct (line 74-75)
  - [ ] 1.3 Run `make generate` to regenerate deepcopy (field removal changes DeepCopyInto)
  - [ ] 1.4 Run `make manifests` to regenerate CRD/RBAC (CRD schema will drop `waveLabel` property)

- [ ] Task 2: Remove validation for removed field (AC: #7)
  - [ ] 2.1 In `pkg/apis/soteria.io/v1alpha1/validation.go` (lines 31-33), delete the `WaveLabel == ""` required check

- [ ] Task 3: Update `GroupByWave` signature (AC: #2)
  - [ ] 3.1 In `pkg/engine/discovery.go` (line 73), change signature from `func GroupByWave(vms []VMReference, waveLabel string) DiscoveryResult` to `func GroupByWave(vms []VMReference) DiscoveryResult`
  - [ ] 3.2 Replace `vm.Labels[waveLabel]` (line 80) with `vm.Labels[soteriav1alpha1.WaveLabel]`
  - [ ] 3.3 Add import for `soteriav1alpha1` if not already present

- [ ] Task 4: Update `ResolveVolumeGroups` signature (AC: #3)
  - [ ] 4.1 In `pkg/engine/consistency.go` (lines 100-105), remove the `waveLabel string` parameter
  - [ ] 4.2 Replace all internal usages of `waveLabel` with `soteriav1alpha1.WaveLabel`

- [ ] Task 5: Update `buildChunkInput` and executor (AC: #4)
  - [ ] 5.1 In `pkg/engine/executor.go` (line 1592), remove `waveLabel string` parameter from `buildChunkInput`
  - [ ] 5.2 Replace `vm.Labels[waveLabel]` (line 1597) with `vm.Labels[soteriav1alpha1.WaveLabel]`
  - [ ] 5.3 Update 3 call sites in executor.go (lines ~220-228, ~288-296, ~1182-1188):
    - `GroupByWave(vms)` (drop second arg)
    - `ResolveVolumeGroups(ctx, vms, e.NamespaceLookup)` (drop waveLabel arg)
    - `buildChunkInput(discovery, consistency, vms)` (drop waveLabel arg)

- [ ] Task 6: Update DRPlan reconciler (AC: #2, #3)
  - [ ] 6.1 In `pkg/controller/drplan/reconciler.go` (line 167), change `engine.GroupByWave(vms, plan.Spec.WaveLabel)` to `engine.GroupByWave(vms)`
  - [ ] 6.2 (line 196) Change `engine.ResolveVolumeGroups(ctx, vms, plan.Spec.WaveLabel, r.NamespaceLookup)` to `engine.ResolveVolumeGroups(ctx, vms, r.NamespaceLookup)`

- [ ] Task 7: Update VM admission webhook (AC: #5)
  - [ ] 7.1 In `pkg/admission/vm_validator.go` (line 138), replace `waveLabel := plan.Spec.WaveLabel` with `waveLabel := soteriav1alpha1.WaveLabel`

- [ ] Task 8: Add backward-compat stripping in strategy (AC: #6)
  - [ ] 8.1 In `pkg/registry/drplan/strategy.go` `PrepareForCreate` (line 47-55), add `plan.Spec.WaveLabel = ""` after getting the plan
  - [ ] 8.2 In `PrepareForUpdate` (line 57-61), add `newPlan.Spec.WaveLabel = ""` after getting the plan
  - [ ] 8.3 Add unit tests for both methods verifying that a non-empty WaveLabel is stripped

- [ ] Task 9: Update sample YAML (AC: #8)
  - [ ] 9.1 In `config/samples/soteria_v1alpha1_drplan.yaml`, remove the `waveLabel: soteria.io/wave` line and its comment
  - [ ] 9.2 Add a comment: `# Wave label is always soteria.io/wave (fixed convention, not configurable).`

- [ ] Task 10: Update tests (AC: #9)
  - [ ] 10.1 `pkg/engine/discovery_test.go` — update `TestGroupByWave` calls to drop second arg; test still asserts same wave grouping logic
  - [ ] 10.2 `pkg/engine/executor_test.go` — remove `WaveLabel` field from plan fixtures (line 149), update direct `GroupByWave` call (line 2079)
  - [ ] 10.3 `pkg/controller/drplan/reconciler_test.go` — remove `WaveLabel` from plan fixture (line 83), rename/update `TestReconcile_WaveLabelChanged_VMMoved` (line 261) to reflect wave label is now constant-based
  - [ ] 10.4 `pkg/controller/drexecution/reconciler_test.go` — remove `WaveLabel` field from all plan fixtures (lines 84, 173, 309, 373, 421, 914, 1307, 1350)
  - [ ] 10.5 `pkg/apis/soteria.io/v1alpha1/validation_test.go` — remove `WaveLabel` from all test fixtures, delete the empty-WaveLabel test case (was line 45-56), delete `TestDRPlanWebhook_InvalidWaveLabel_Rejected` equivalent if present
  - [ ] 10.6 `pkg/admission/drplan_validator_test.go` — remove `WaveLabel` from all plan fixtures (currently set to `"wave"`)
  - [ ] 10.7 `test/integration/admission/drplan_webhook_test.go` — remove `WaveLabel` from all plan fixtures, delete `TestDRPlanWebhook_InvalidWaveLabel_Rejected` test (line 96)
  - [ ] 10.8 `test/integration/admission/vm_webhook_test.go` — remove `WaveLabel` from all plan fixtures (lines 92, 184, 241, 292)
  - [ ] 10.9 `test/integration/controller/drplan_test.go` — remove `WaveLabel` from plan fixture (line 74), update `TestDRPlanReconciler_WaveLabelChanged_WatchTriggersReconcile` (line 137) to test that changing a VM's `soteria.io/wave` value triggers reconcile
  - [ ] 10.10 `test/integration/controller/drplan_consistency_test.go` — remove `WaveLabel` from plan fixture (line 55)
  - [ ] 10.11 `test/integration/controller/suite_test.go` — remove `WaveLabel` from shared plan fixture (line 402)
  - [ ] 10.12 `test/integration/controller/drexecution_test.go` — remove `WaveLabel` from all plan fixtures (lines 45, 137, 197, 261, 338, 395)
  - [ ] 10.13 `test/integration/storage/watch_test.go` — remove `WaveLabel` from plan fixture (line 128)
  - [ ] 10.14 `pkg/registry/drplan/strategy_test.go` — add tests verifying PrepareForCreate/PrepareForUpdate strip WaveLabel; remove WaveLabel from existing test fixtures
  - [ ] 10.15 Run `make test` — verify all unit tests pass
  - [ ] 10.16 Run `make integration` — verify all integration tests pass

- [ ] Task 11: Verify build and lint (AC: #1, #9)
  - [ ] 11.1 Run `make lint` — zero new lint errors
  - [ ] 11.2 Verify generated OpenAPI in `zz_generated.openapi.go` no longer has `waveLabel` (automatic from `make manifests`)

## Dev Notes

### Scope & Approach

This is a mechanical API simplification story. The wave label has always been `soteria.io/wave` in practice (every test fixture, every sample uses this value). Making it a constant eliminates a configurable field that was never varied and reduces API surface.

**Change pattern:** Remove field → add constant → update all consumers from parameter-based to constant-based → strip field server-side for backward compat.

### Critical: Leave `WaveLabel` Field in Go Struct Temporarily

The Go struct still needs the `WaveLabel string` field to compile during the transition because `PrepareForCreate`/`PrepareForUpdate` will zero it. However, the JSON tag `json:"waveLabel"` ensures old clients sending this field get it stripped silently. **After stripping is in place,** the field can remain as an unexported/ignored field or stay with `json:"waveLabel,omitempty"` so it serializes as absent. The cleanest approach:

1. Keep the field but add `// Deprecated: wave label is always soteria.io/wave. This field is stripped on write.`
2. Change tag to `json:"waveLabel,omitempty"` so it never appears in GET responses after stripping
3. The CRD schema will still contain it (optional) for one release cycle → remove entirely in next breaking change window

**Alternative (simpler, preferred for this story):** Remove the field entirely from the struct. The strategy stripping becomes a no-op safety net (zeroing a field that no longer exists in the struct won't compile). Instead, handle backward compat purely via validation: old YAML with `waveLabel` set will simply have that field ignored by Go's JSON unmarshaler (unknown fields are silently dropped with standard `encoding/json`). **This is the preferred approach** — Go's JSON decoder drops unknown fields, so no PrepareForCreate stripping is needed if the field doesn't exist in the struct.

**Final approach:** Remove field from struct entirely. Backward compat is automatic (JSON ignores unknown fields). AC6 simplifies to: "old clients sending `waveLabel` in JSON get it silently ignored by the API server." Add an integration test that creates a DRPlan with `waveLabel` in raw JSON and verifies it succeeds without error.

### Architecture Compliance

- **API group:** `soteria.io/v1alpha1` — this is a pre-stable API, breaking changes are acceptable
- **Labels/annotations:** `soteria.io/wave` follows the project's `soteria.io/<kebab-case>` convention
- **Constant placement:** Adjacent to `DRPlanLabel` in `types.go` (same pattern)
- **CRD regeneration:** `make manifests` handles CRD schema; `make generate` handles deepcopy

### File Structure & Impact Map

| File | Change Type | Impact |
|------|------------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Remove field, add constant | Core schema change |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | Delete WaveLabel check | Validation simplification |
| `pkg/apis/soteria.io/v1alpha1/validation_test.go` | Update fixtures, delete test case | Test update |
| `pkg/engine/discovery.go` | Signature change | Remove param, use constant |
| `pkg/engine/discovery_test.go` | Update call sites | Test update |
| `pkg/engine/consistency.go` | Signature change | Remove param, use constant |
| `pkg/engine/executor.go` | 3 call sites + buildChunkInput | Remove waveLabel args |
| `pkg/engine/executor_test.go` | Update fixtures + call | Test update |
| `pkg/controller/drplan/reconciler.go` | 2 call sites | Drop args |
| `pkg/controller/drplan/reconciler_test.go` | Update fixture | Test update |
| `pkg/controller/drexecution/reconciler_test.go` | Update 8 fixtures | Test update |
| `pkg/admission/vm_validator.go` | Use constant | 1 line change |
| `pkg/admission/drplan_validator_test.go` | Update fixtures | Test update |
| `pkg/registry/drplan/strategy.go` | No change needed (field removed) | N/A |
| `pkg/registry/drplan/strategy_test.go` | Add backward-compat test | Test addition |
| `config/samples/soteria_v1alpha1_drplan.yaml` | Remove line | Sample update |
| `test/integration/admission/drplan_webhook_test.go` | Update fixtures, delete test | Test update |
| `test/integration/admission/vm_webhook_test.go` | Update 4 fixtures | Test update |
| `test/integration/controller/drplan_test.go` | Update fixture, rename test | Test update |
| `test/integration/controller/drplan_consistency_test.go` | Update fixture | Test update |
| `test/integration/controller/suite_test.go` | Update fixture | Test update |
| `test/integration/controller/drexecution_test.go` | Update 6 fixtures | Test update |
| `test/integration/storage/watch_test.go` | Update fixture | Test update |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` | Auto-regenerated | DO NOT EDIT |
| `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` | Auto-regenerated | DO NOT EDIT |

### Testing Strategy

- **No new behavior to test** — this is a removal/simplification
- **Regression focus:** Every existing test that passed with `WaveLabel: "soteria.io/wave"` must pass without the field
- **Backward compat test:** Add one integration test that sends raw JSON with `waveLabel` field and confirms the API server accepts it silently (the field is unknown and dropped)
- **Validation test deletion:** The `InvalidWaveLabel_Rejected` tests become invalid — delete them

### Execution Order

1. Tasks 1-2 (types + validation) — establishes the schema change
2. Tasks 3-5 (engine functions) — updates internal APIs
3. Tasks 6-7 (reconciler + webhook) — updates consumers
4. Task 8 (strategy) — backward compat (may simplify to integration test only)
5. Task 9 (sample) — documentation
6. Tasks 10-11 (tests + build) — verification

### Project Structure Notes

- All changes are within `pkg/` and `test/` — standard Go operator layout
- `config/crd/bases/` will be auto-regenerated by `make manifests` — DO NOT EDIT
- Console plugin (`console-plugin/`) is unaffected — it reads plan status, not spec.waveLabel

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go#L54-91] — DRPlanLabel constant and DRPlanSpec struct
- [Source: pkg/engine/discovery.go#L69-96] — GroupByWave function
- [Source: pkg/engine/consistency.go#L100-105] — ResolveVolumeGroups signature
- [Source: pkg/engine/executor.go#L218-228,#L286-296,#L1180-1189,#L1588-1597] — Executor call sites and buildChunkInput
- [Source: pkg/controller/drplan/reconciler.go#L167,#L196] — Reconciler call sites
- [Source: pkg/admission/vm_validator.go#L138] — Webhook waveLabel usage
- [Source: pkg/apis/soteria.io/v1alpha1/validation.go#L31-33] — WaveLabel required check
- [Source: pkg/registry/drplan/strategy.go#L47-61] — PrepareForCreate/Update
- [Source: config/samples/soteria_v1alpha1_drplan.yaml] — Sample YAML
- [Source: _bmad-output/project-context.md] — CRD JSON fields camelCase, labels convention, testing rules

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
