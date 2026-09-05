# Story 17.18: API Reference: DRPlan

Status: ready-for-dev

## Story

As a platform engineer or automation developer,
I want a complete API reference for the DRPlan CRD,
so that I can programmatically create and manage DRPlans with confidence in field types and validation rules.

## Acceptance Criteria

**AC1: All spec fields are documented**
Given the DRPlan API reference
When a reader reviews spec fields
Then every field in DRPlanSpec is documented with: name, type, required/optional, default value, validation rules, and description

**AC2: All status fields are documented**
Given the DRPlan API reference
When a reader reviews status fields
Then every field in DRPlanStatus is documented with: name, type, and description of what it represents

**AC3: Validation rules are complete**
Given the DRPlan API reference
When a reader checks validation
Then all kubebuilder validation markers and admission webhook rules are documented

**AC4: Examples are provided**
Given the DRPlan API reference
When a reader wants to see usage
Then there are complete YAML examples for common configurations

**AC5: Reference matches actual CRD schema**
Given the documented API reference
When compared against the generated CRD schema and types.go
Then every field, type, default, and validation rule is accurate

## Tasks / Subtasks

- [ ] Task 1: Extract spec field definitions (AC: 1, 3, 5)
  - [ ] 1.1: Walk `pkg/apis/soteria.io/v1alpha1/types.go` for DRPlan, DRPlanSpec, VolumeReplicationDriverConfig — all fields and kubebuilder markers
  - [ ] 1.2: Walk `pkg/apis/soteria.io/v1alpha1/validation.go` for ValidateDRPlan and ValidateDRPlanUpdate rules
  - [ ] 1.3: Walk `pkg/apis/soteria.io/v1alpha1/defaults.go` for defaulting logic
  - [ ] 1.4: Walk `pkg/admission/drplan_validator.go` for webhook-based validation (field-level)
- [ ] Task 2: Extract status field definitions (AC: 2, 5)
  - [ ] 2.1: Walk `pkg/apis/soteria.io/v1alpha1/types.go` for DRPlanStatus and all nested types (WaveInfo, VolumeGroupInfo, PreflightReport, VolumeGroupHealth, SiteDiscovery, etc.)
- [ ] Task 3: Write the reference page (AC: 1, 2, 3, 4)
  - [ ] 3.1: Write `docs/reference/api/drplan.md` with full field reference tables for spec and status
  - [ ] 3.2: Document validation rules (Required, Minimum, Enum, Immutable fields)
  - [ ] 3.3: Add complete YAML examples for minimal and full configurations
- [ ] Task 4: Verify accuracy (AC: 5)
  - [ ] 4.1: Cross-reference every documented field against the actual types.go

## Dev Notes

### Documentation Methodology

Start from the PRD (`_bmad-output/planning-artifacts/prd.md`), architecture doc (`_bmad-output/planning-artifacts/architecture.md`), or UX spec (`_bmad-output/planning-artifacts/ux-design-specification.md`) as the conceptual base. Then read the related implemented user stories (in `_bmad-output/implementation-artifacts/`) and walk the actual code to document *current behavior*, not aspirational specs. Where the implementation diverges from the PRD, the code is the truth.

### Source Documents

- [Source: _bmad-output/planning-artifacts/prd.md — FR2 (DRPlan concept), FR6–FR7 (volume grouping), FR11–FR12 (throttling)]
- [Source: _bmad-output/planning-artifacts/architecture.md — DRPlan API design]

### Code to Verify Against

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlan struct, DRPlanSpec fields: VolumeReplicationDriver (Required, Type enum: noop|csi-extension, VolumeReplicationClass optional), MaxConcurrentFailovers (Minimum=1), PrimarySite, SecondarySite, VMReadyTimeout (default "5m"), ResyncTimeout (default "10m")]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlanStatus fields: Phase (enum: SteadyState|FailedOver|DRedSteadyState|FailedBack), ActiveSite, Conditions, ObservedGeneration, Waves ([]WaveInfo), DiscoveredVMCount, Preflight (*PreflightReport), ReplicationHealth ([]VolumeGroupHealth), PrimarySiteDiscovery, SecondarySiteDiscovery]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — Nested types: VolumeReplicationDriverConfig, WaveInfo, VolumeGroupInfo (ConsistencyLevel enum: namespace|vm), DiscoveredVM, DiscoveredDisk, PreflightReport (Waves, TotalVMs, Warnings, SitesInSync, DisksConsistent), PreflightWave, PreflightVM, PreflightChunk, PreflightVolumeGroup, VolumeGroupHealth (Health enum: Healthy|Degraded|Syncing|NotReplicating|Error|Unknown), SiteDiscovery]
- [Source: pkg/apis/soteria.io/v1alpha1/validation.go — ValidateDRPlan (field-level validation), ValidateDRPlanUpdate (immutability checks)]
- [Source: pkg/apis/soteria.io/v1alpha1/defaults.go — defaulting logic for optional fields]
- [Source: pkg/admission/drplan_validator.go — webhook validation (defense-in-depth; active validation in SoteriaAdmissionPlugin)]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — Constants: DRPlanLabel ("soteria.io/drplan"), WaveLabel ("soteria.io/wave"), ConsistencyAnnotation ("soteria.io/consistency-level")]

### Implementation Pattern

- Structure as field reference tables with columns: Field, Type, Required, Default, Validation, Description
- Separate tables for Spec and Status
- Document immutable fields explicitly (VolumeReplicationDriver, PrimarySite, SecondarySite)
- Include YAML examples:
  - Minimal: just required fields
  - Full: all fields with comments explaining each
- Cross-reference with Story 17.11 (user-friendly guide) to avoid redundancy
- Document label conventions: `soteria.io/drplan`, `soteria.io/wave`

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| docs/reference/api/drplan.md | NEW | Exhaustive DRPlan field reference with types, defaults, validation, examples |

### Key Constraints

- No CRDs in `config/crd/bases/` — DRPlan is served by the aggregated API server (ScyllaDB-backed)
- Validation is two-layered: aggregated API server strategy + admission webhook (defense-in-depth)
- Immutable fields: VolumeReplicationDriver, PrimarySite, SecondarySite (checked in ValidateDRPlanUpdate)
- This is the exhaustive field reference; the user-friendly guide is Story 17.11

### Project Structure Notes

- API types: `pkg/apis/soteria.io/v1alpha1/types.go`
- Validation: `pkg/apis/soteria.io/v1alpha1/validation.go`
- Defaulting: `pkg/apis/soteria.io/v1alpha1/defaults.go`
- Webhook: `pkg/admission/drplan_validator.go`

### References

- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlan, DRPlanSpec, DRPlanStatus, all nested types]
- [Source: pkg/apis/soteria.io/v1alpha1/validation.go — validation rules]
- [Source: pkg/admission/drplan_validator.go — webhook validation]
- [Source: _bmad-output/planning-artifacts/prd.md — FR2, FR6, FR7, FR11, FR12]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
