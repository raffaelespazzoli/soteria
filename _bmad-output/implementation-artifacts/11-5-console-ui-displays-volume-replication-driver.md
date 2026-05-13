# Story 11.5: Console UI Displays Volume Replication Driver

Status: ready-for-dev

## Story

As a platform engineer viewing a DRPlan in the OCP console,
I want to see the declared volume replication driver in the plan configuration view,
So that I can verify which driver handles replication for this plan without inspecting the raw YAML.

## Background

### Current State

The `PlanConfiguration` component in the console plugin displays plan spec fields (maxConcurrentFailovers, primarySite, secondarySite, vmReadyTimeout) via a PatternFly `DescriptionList`. The `DRPlanSpec` TypeScript interface in `console-plugin/src/models/types.ts` mirrors the Go spec struct.

The `WaveCompositionTree` already displays `storageBackend` per VM from the preflight report. After Stories 11.1–11.3, this value comes from the declared driver rather than derived resolution, but the field name and shape are unchanged — so `WaveCompositionTree` needs no code changes.

### Scope

This story adds the `volumeReplicationDriver` field to the TypeScript interface and displays it in the plan configuration component. Minimal changes.

## Acceptance Criteria

1. **AC1 — TypeScript type updated:** `DRPlanSpec` interface in `console-plugin/src/models/types.ts` gains `volumeReplicationDriver: string`.

2. **AC2 — PlanConfiguration displays driver:** The `PlanConfiguration` component in the plan detail page displays "Volume Replication Driver" in the `DescriptionList` showing the plan's `spec.volumeReplicationDriver` value.

3. **AC3 — WaveCompositionTree unchanged:** The `WaveCompositionTree` component continues to render `storageBackend` per VM from the preflight report. No changes needed — the field shape is unchanged, only the value source changed server-side.

4. **AC4 — Test fixtures updated:** All test files that construct `DRPlanSpec` or `DRPlan` objects in the console plugin include `volumeReplicationDriver: 'noop'`.

5. **AC5 — jest-axe accessibility:** The new `DescriptionList` entry passes jest-axe checks along with the rest of `PlanConfiguration`.

## Tasks / Subtasks

- [ ] Task 1: Update TypeScript types (AC: #1)
  - [ ] 1.1 In `console-plugin/src/models/types.ts`, add `volumeReplicationDriver: string` to `DRPlanSpec` interface

- [ ] Task 2: Update PlanConfiguration component (AC: #2)
  - [ ] 2.1 Add a `DescriptionListGroup` with term "Volume Replication Driver" and description `plan.spec.volumeReplicationDriver`

- [ ] Task 3: Update test fixtures (AC: #4)
  - [ ] 3.1 Search all console-plugin test files for `DRPlanSpec` or spec object constructions
  - [ ] 3.2 Add `volumeReplicationDriver: 'noop'` to all fixtures

- [ ] Task 4: Verify (AC: #3, #5)
  - [ ] 4.1 Verify `WaveCompositionTree` tests still pass without changes
  - [ ] 4.2 Run jest-axe on `PlanConfiguration` to verify accessibility
  - [ ] 4.3 Run full console-plugin test suite — all tests pass

## Dev Notes

### Key Locations

| File | Change |
|------|--------|
| `console-plugin/src/models/types.ts` | Add `volumeReplicationDriver` to `DRPlanSpec` |
| `console-plugin/src/components/DRPlanDetail/PlanConfiguration.tsx` | Add DescriptionListGroup |
| `console-plugin/src/components/DRPlanDetail/__tests__/PlanConfiguration.test.tsx` | Update fixtures, verify display |

### What NOT to Change

- `WaveCompositionTree` — no changes needed; `storageBackend` field shape unchanged
- `DRDashboard` — no dashboard-level display of driver needed
- `DRLifecycleDiagram` — lifecycle states unrelated to driver

### Dependency

- **Depends on Story 11.1** — the `volumeReplicationDriver` field must exist in the API.

### Previous Story Intelligence

- **Story 6.5b (Waves, History & Configuration Tabs):** Original implementation of `PlanConfiguration` with `DescriptionList`. Follow the existing pattern for adding a new field.
- **Story 8.4 (Console UI Site-Aware Plan Status):** Added site discovery display — similar pattern of adding new data to existing components.

### Build Commands

```bash
cd console-plugin && npm test   # Console plugin tests
cd console-plugin && npm run lint # Console lint
```
