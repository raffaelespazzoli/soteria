# Story 17.11: Creating a DRPlan

Status: ready-for-dev

## Story

As a DR operator,
I want a clear guide for creating and configuring a DRPlan,
so that I can define which VMs are protected and how they fail over.

## Acceptance Criteria

**AC1: Annotated YAML example is provided**
Given the DRPlan creation guide
When a reader reviews the DRPlan spec
Then they find an annotated YAML example with every field explained inline

**AC2: VM discovery via labels is explained**
Given the guide
When a reader wants to add a VM to a plan
Then they understand the 2 labels required to assign a VM to a DRPlan

**AC3: All spec fields are documented**
Given the guide
When a reader reviews the field descriptions
Then they find: label selector, wave label, maxConcurrentFailovers, volumeReplicationDriver, primarySite, secondarySite — with type, purpose, and constraints

**AC4: Validation rules are documented**
Given the guide
When a reader wants to understand what values are valid
Then they find the admission webhook validation rules for each field

**AC5: Field documentation matches actual types**
Given the documented fields
When compared against `api/soteria/v1alpha1/types.go` and admission webhooks
Then every field, type, and constraint is accurate

## Tasks / Subtasks

- [ ] Task 1: Extract DRPlan spec from code (AC: 3, 5)
  - [ ] 1.1: Read PRD FR1-FR8 for the conceptual DRPlan design
  - [ ] 1.2: Walk `pkg/apis/soteria.io/v1alpha1/types.go` for the actual DRPlan spec and status structs
  - [ ] 1.3: Walk admission webhooks in `pkg/admission/` for validation rules
- [ ] Task 2: Document VM discovery (AC: 2)
  - [ ] 2.1: Document the 2 labels: `soteria.io/drplan` (plan membership) and `soteria.io/wave` (wave assignment)
  - [ ] 2.2: Review sample CRs in `config/samples/` for example usage
- [ ] Task 3: Write the usage guide (AC: 1, 2, 3, 4, 5)
  - [ ] 3.1: Write `docs/usage/creating-a-drplan.md` covering: DRPlan CRD spec walkthrough (annotated YAML), label selector, wave label, maxConcurrentFailovers, volumeReplicationDriver, primarySite/secondarySite, VM assignment labels, field validation rules
  - [ ] 3.2: Create annotated YAML example from actual sample CRs
  - [ ] 3.3: Verify all fields and validation rules match the code

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR1-FR8 DR plan management requirements]
- [Source: _bmad-output/planning-artifacts/architecture.md — DRPlan CRD design, label-driven discovery]

### Code to Verify Against

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlan struct, DRPlanSpec struct fields: VolumeReplicationDriver (VolumeReplicationDriverConfig{Type, VolumeReplicationClass}), MaxConcurrentFailovers (int, min 1), PrimarySite (string, immutable), SecondarySite (string, immutable, must differ from PrimarySite), VMReadyTimeout (default "5m"), ResyncTimeout (default "10m")]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — label constants: DRPlanLabel = "soteria.io/drplan", WaveLabel = "soteria.io/wave", PlanNameLabel = "soteria.io/plan-name", TriggeredByAnnotation = "soteria.io/triggered-by"]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — VolumeReplicationDriverConfig: Type (Required, Enum=noop;csi-extension), VolumeReplicationClass (optional, only for csi-extension)]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — ConsistencyLevel type: "namespace" or "vm", controlled via namespace annotation "soteria.io/consistency-level"]
- [Source: pkg/admission/drplan_validator.go — DRPlanValidator validates CREATE and UPDATE: field-level validation (maxConcurrentFailovers, site names, vmReadyTimeout). Cross-resource constraints delegated to controller (WaveConflict, NamespaceGroupExceedsThrottle)]
- [Source: pkg/admission/plugin.go — SoteriaAdmissionPlugin: in-process admission plugin (active validation path)]
- [Source: config/samples/soteria_v1alpha1_drplan.yaml — sample DRPlan CR "erp-full-stack": noop driver, maxConcurrentFailovers=4, primarySite=dc-west, secondarySite=dc-east, vmReadyTimeout=5m, resyncTimeout=10m]
- [Source: config/samples/vm_with_drplan_label.yaml — sample VM with DRPlan labels]

### Implementation Pattern

- **VM discovery**: VMs declare plan membership via label `soteria.io/drplan: <plan-name>` — one label key = one value = one-plan-per-VM exclusivity (structurally enforced, no runtime check needed)
- **Wave assignment**: VMs declare wave via label `soteria.io/wave: <wave-number>` — fixed convention, not configurable
- **Annotated YAML**: start from `config/samples/soteria_v1alpha1_drplan.yaml`, expand with inline comments for every field
- **Validation layers**: (1) kubebuilder markers (Required, Minimum, Enum, default), (2) admission webhook (field-level), (3) aggregated API server strategy layer (defense-in-depth), (4) controller reconciliation (cross-resource constraints via Ready=False conditions)
- **Immutable fields**: VolumeReplicationDriver, PrimarySite, SecondarySite — immutable after creation
- **Consistency level**: namespace annotation `soteria.io/consistency-level` controls VolumeGroup granularity (namespace vs vm level)

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `docs/usage/creating-a-drplan.md` | NEW | DRPlan creation and configuration guide with annotated YAML, labels, validation rules |

### Key Constraints

- Two labels needed to add a VM to DR protection: `soteria.io/drplan: <name>` and `soteria.io/wave: <number>`
- VolumeReplicationDriver.Type is an enum: `noop` or `csi-extension` — document both
- PrimarySite and SecondarySite are immutable after creation and must differ
- MaxConcurrentFailovers minimum is 1 (kubebuilder validation marker)
- Active validation has moved from DRPlanValidator to SoteriaAdmissionPlugin (in-process) — DRPlanValidator is retained as legacy reference
- Depends on: 17.1

### Project Structure Notes

- Types: `pkg/apis/soteria.io/v1alpha1/types.go` — DRPlan, DRPlanSpec, DRPlanStatus, VolumeReplicationDriverConfig, label/annotation constants
- Admission: `pkg/admission/` — 10 files: drplan_validator, drexecution_validator, vm_validator, plugin, setup, doc, tests
- Samples: `config/samples/` — 4 files: drplan, drexecution, vm_with_drplan_label, kustomization

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — lines 53-62, DRPlanLabel and WaveLabel constants]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — lines 76-120, DRPlan and DRPlanSpec structs]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — lines 85-100, VolumeReplicationDriverConfig with kubebuilder markers]
- [Source: pkg/admission/drplan_validator.go — lines 18-30, validation scope documentation]
- [Source: config/samples/soteria_v1alpha1_drplan.yaml — complete sample DRPlan CR]
- [Source: config/samples/vm_with_drplan_label.yaml — sample VM with labels]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
