# Story 5.6: Event-Driven Wave Gate with VM Readiness Verification

Status: done

## Story

As a DR operator,
I want each wave to wait until all VMs in the previous wave have reached Running state (or timed out) before starting the next wave,
So that application dependency ordering defined by waves is actually enforced at runtime, preventing dependent services from starting before their prerequisites are ready.

## Background

During E2E disaster failover testing on etl6/etl7, we discovered that the wave executor fires `StartVM` (patches `spec.runStrategy: Always`) and immediately proceeds to the next wave without verifying VMs actually reach `Running` state. This violates the core wave contract: wave N+1 should only start after all wave N VMs are confirmed healthy.

**Current behavior:** `FailoverHandler.ExecuteGroupWithSteps` → `StartVM` → returns immediately → next wave starts.

**Desired behavior:** After all VMs in a wave are started, the controller yields the reconcile and waits for VM state change events. When all VMs in the wave reach `Running` (or a configurable timeout expires), the next wave begins.

This story implements **Option B (event-driven)**: refactor the wave executor from a synchronous long-running reconcile into a state machine that tracks wave/group progress in `DRExecution.Status` and is driven by reconcile events from both `DRExecution` and `VirtualMachine` watches.

## Acceptance Criteria

1. **AC1 — VM readiness gate between waves:** When all `StartVM` operations in wave N complete, the controller does NOT immediately start wave N+1. Instead, it persists wave N's status with a new phase `WaitingForVMReady`, yields the reconcile, and waits for VM state change events or a periodic requeue to verify readiness.

2. **AC2 — VM readiness signal:** A VM is considered "ready" when its `VirtualMachine.Status.PrintableStatus == "Running"`. The controller checks this by reading the VM object. `KubeVirtVMManager.IsVMRunning` already exists and checks `spec.runStrategy == Always` — add a new method `IsVMReady(ctx, name, namespace) (bool, error)` that checks `status.printableStatus == Running`.

3. **AC3 — Configurable VM readiness timeout:** `DRPlanSpec` gains a new optional field `VMReadyTimeout` (`*metav1.Duration`, default 5 minutes). When the timeout expires for any VM in a wave, that VM's group is marked `Failed` with error `"VM did not reach Running state within timeout"`. The timeout is per-wave, starting when all StartVM operations in the wave complete.

4. **AC4 — Mode-dependent timeout policy:**
   - `disaster` mode: fail-forward — mark the timed-out group as `Failed`, continue to next wave (consistent with existing fail-forward semantics).
   - `planned_migration` mode: fail-fast — abort the entire execution with `Failed` result if any VM fails to reach readiness.

5. **AC5 — VirtualMachine watch on DRExecution controller:** `SetupWithManager` adds a `.Watches()` for `kubevirtv1.VirtualMachine` with a predicate that filters to `status.printableStatus` changes. The event handler maps VMs to their owning DRExecution via the `soteria.io/drplan` label → DRPlan → active execution.

6. **AC6 — Wave execution state machine:** The `DRExecution.Status.Waves[].Groups[]` status tracks per-group execution phase. A new `DRGroupResult` value `WaitingForVMReady` is added. The reconcile loop:
   - Finds the current in-progress wave
   - For groups in `WaitingForVMReady`: checks VM readiness
   - If all VMs ready → mark group `Completed`, proceed
   - If timeout expired → mark group `Failed` (apply mode policy from AC4)
   - If all groups in wave are terminal → advance to next wave
   - If still waiting → `RequeueAfter(10s)` as a safety net

7. **AC7 — New step in execution status:** A `WaitVMReady` step is recorded in `DRGroupExecutionStatus.Steps[]` for each VM, showing whether it reached `Running` and how long it took. Example: `{Name: "WaitVMReady", Status: "Succeeded", Message: "VM fedora-db reached Running in 45s"}`.

8. **AC8 — Checkpoint compatibility:** The existing `KubeCheckpointer` continues to work. The `WaitingForVMReady` state is checkpointed so that on pod restart, the controller resumes waiting (not re-executing StartVM operations that already succeeded).

9. **AC9 — Backward compatibility:** If `VMReadyTimeout` is not set on the DRPlan, the default of 5 minutes applies. Existing DRPlans without this field work unchanged (with the new readiness gate behavior and default timeout).

10. **AC10 — Predicate reduces reconcile noise:** The `specOrAnnotationChanged()` predicate (already added to `SetupWithManager` in this sprint) prevents status-only DRExecution updates from re-triggering reconciles. The VM watch only fires on `printableStatus` changes, not every VM status update.

11. **AC11 — Unit tests:** Tests covering: (a) wave gate — wave N+1 does not start until wave N VMs are ready; (b) VM reaches Running — group transitions to Completed; (c) timeout — group marked Failed after VMReadyTimeout; (d) disaster mode fail-forward — next wave starts despite timeout; (e) planned_migration fail-fast — execution aborted on timeout; (f) checkpoint/resume — WaitingForVMReady state survives pod restart; (g) VM watch predicate — only printableStatus changes trigger reconcile; (h) default timeout — 5m applied when DRPlan field is nil.

12. **AC12 — Integration tests:** At least one integration test verifying the full wave-gate flow: create DRExecution → wave 1 VMs started → wait for readiness → wave 2 starts only after wave 1 ready.

## Tasks / Subtasks

- [ ] Task 1: Add `VMReadyTimeout` to DRPlanSpec (AC: #3, #9)
  - [ ] 1.1 Add `VMReadyTimeout *metav1.Duration` field to `DRPlanSpec` in `pkg/apis/soteria.io/v1alpha1/types.go` with `+kubebuilder:default="5m"` and `+optional`
  - [ ] 1.2 Run `make manifests generate` to regenerate CRDs and DeepCopy
  - [ ] 1.3 Update sample CRs in `config/samples/` to show the new field
  - [ ] 1.4 Update admission webhook if needed (validate duration is positive)

- [ ] Task 2: Add `WaitingForVMReady` group result and `WaitVMReady` step (AC: #6, #7)
  - [ ] 2.1 Add `DRGroupResultWaitingForVMReady DRGroupResult = "WaitingForVMReady"` to `types.go`
  - [ ] 2.2 Define `StepWaitVMReady = "WaitVMReady"` constant in `pkg/engine/failover.go` alongside existing step constants
  - [ ] 2.3 Add `VMReadyStartTime *metav1.Time` field to `WaveStatus` to track when readiness waiting began for the wave

- [ ] Task 3: Add `IsVMReady` to VMManager interface (AC: #2)
  - [ ] 3.1 Add `IsVMReady(ctx context.Context, name, namespace string) (bool, error)` to `VMManager` interface in `pkg/engine/vm.go`
  - [ ] 3.2 Implement in `KubeVirtVMManager`: Get VM, return `vm.Status.PrintableStatus == kubevirtv1.VirtualMachineStatusRunning`
  - [ ] 3.3 Implement in `NoOpVMManager` (test double): return true immediately (configurable)
  - [ ] 3.4 Update `pkg/engine/vm_health.go` `KubeVirtVMHealthValidator` if there's overlap

- [ ] Task 4: Add VM watch to DRExecution controller (AC: #5, #10)
  - [ ] 4.1 Add `.Watches(&kubevirtv1.VirtualMachine{}, ...)` to `SetupWithManager` in `pkg/controller/drexecution/reconciler.go`
  - [ ] 4.2 Create predicate `vmPrintableStatusChanged()` that only passes Update events where `status.printableStatus` changed between old and new
  - [ ] 4.3 Create event handler `mapVMToDRExecution()`: read `soteria.io/drplan` label → Get DRPlan → if `ActiveExecution != ""` → enqueue that DRExecution
  - [ ] 4.4 Add RBAC marker: `// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch`
  - [ ] 4.5 Import `kubevirtv1 "kubevirt.io/api/core/v1"` and wire dependencies

- [ ] Task 5: Refactor wave executor to state machine (AC: #1, #6, #8)
  - [ ] 5.1 After `executeGroup` completes StartVM, set group result to `WaitingForVMReady` instead of `Completed`
  - [ ] 5.2 After all groups in a wave finish their handler execution, persist status with `WaitingForVMReady` groups and `VMReadyStartTime` on the wave, then **return from Reconcile** (do not block)
  - [ ] 5.3 Add `reconcileWaveProgress(ctx, exec, plan)` method to the DRExecution reconciler that:
    - Finds the first wave with any non-terminal group
    - For `WaitingForVMReady` groups: calls `VMManager.IsVMReady` for each VM
    - If all VMs ready: transition group to `Completed`, add `WaitVMReady` step with duration
    - If timeout exceeded: transition group to `Failed`, add `WaitVMReady` step with timeout error
    - If all groups in wave terminal: start next wave (call handler for next wave's groups)
    - If still waiting: return `RequeueAfter(10s)`
  - [ ] 5.4 The Reconcile entry point dispatches to `reconcileWaveProgress` when `StartTime != nil` AND `Result == ""` AND waves exist with `WaitingForVMReady` groups
  - [ ] 5.5 Ensure `persistStatus` and checkpoint calls use `retry.RetryOnConflict` (already the case for `persistStatus`)

- [ ] Task 6: Implement mode-dependent timeout policy (AC: #4)
  - [ ] 6.1 In `reconcileWaveProgress`, when a group times out:
    - If `exec.Spec.Mode == disaster`: mark group Failed, continue to next group/wave (fail-forward)
    - If `exec.Spec.Mode == planned_migration`: mark group Failed, call `failExecution` to abort
  - [ ] 6.2 Parse `plan.Spec.VMReadyTimeout` (default 5m) and compare against `wave.VMReadyStartTime`

- [ ] Task 7: Update tests (AC: #11, #12)
  - [ ] 7.1 Unit tests for `IsVMReady` on `KubeVirtVMManager` and `NoOpVMManager`
  - [ ] 7.2 Unit tests for `vmPrintableStatusChanged` predicate
  - [ ] 7.3 Unit tests for `mapVMToDRExecution` handler
  - [ ] 7.4 Unit tests for `reconcileWaveProgress`: ready → Completed, timeout → Failed, mode policy
  - [ ] 7.5 Unit tests for checkpoint/resume with `WaitingForVMReady` state
  - [ ] 7.6 Integration test: full wave gate flow with NoOp VM manager
  - [ ] 7.7 Update existing executor tests that assume synchronous wave completion

## Dev Notes

### Architecture: Synchronous → Event-Driven Transition

The current `WaveExecutor.Execute` runs the entire pipeline (discover → chunk → execute all waves) in a single synchronous Reconcile call. This story breaks that into:

1. **First reconcile**: Setup + execute wave 0 handler (StartVM) → persist `WaitingForVMReady` → return
2. **VM event reconcile**: Check readiness → if ready, execute wave 1 handler → persist → return
3. **Repeat** until all waves complete → `finishExecution`

The `WaveExecutor.Execute` method will be refactored to `ExecuteNextWave` that processes one wave at a time and returns, or a new `reconcileWaveProgress` path is added alongside the existing `Execute` flow.

### Key Existing Patterns to Reuse

- **`waitForSync` in `failover.go`**: Existing polling pattern for replication sync. The new approach is event-driven (not polling), but the timeout logic is similar.
- **`IsVMRunning` in `vm.go`**: Already checks `runStrategy`. New `IsVMReady` checks `status.printableStatus`.
- **`vmEventHandler` in DRPlan controller**: Pattern for VM watch with label-based mapping. DRExecution controller uses a similar approach but maps through DRPlan.ActiveExecution.
- **`specOrAnnotationChanged` predicate**: Already on the DRExecution `.For()` watch (added this sprint). Prevents status writes from re-triggering.
- **`ResumeAnalyzer` in `resume.go`**: Already handles reconstructing state from `DRExecution.Status.Waves[]`. The `WaitingForVMReady` state integrates naturally.

### Files to Touch

| File | Changes |
|------|---------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Add `VMReadyTimeout` to `DRPlanSpec`, `WaitingForVMReady` result, `VMReadyStartTime` to `WaveStatus` |
| `pkg/engine/vm.go` | Add `IsVMReady` to interface + `KubeVirtVMManager` impl |
| `pkg/engine/failover.go` | Add `StepWaitVMReady` constant, modify group completion to use `WaitingForVMReady` |
| `pkg/engine/executor.go` | Refactor wave loop to yield after handler, support per-wave reconcile |
| `pkg/controller/drexecution/reconciler.go` | Add VM watch, `reconcileWaveProgress`, `mapVMToDRExecution`, VM predicate |
| `pkg/engine/resume.go` | Handle `WaitingForVMReady` state in resume analysis |
| `pkg/admission/*.go` | Validate `VMReadyTimeout` if present |
| `config/samples/*.yaml` | Update samples with `vmReadyTimeout` |
| Test files | New + updated tests across engine and controller packages |

### Critical Constraints

- **Do NOT remove** the existing `Execute` flow entirely — the retry and resume paths depend on it. Extend or wrap it.
- **`WaitingForVMReady` must be checkpointed** — on pod restart, the new leader must resume waiting, not re-execute StartVM.
- **The VM watch predicate must be narrow** — only `printableStatus` changes, not every VM status field update (conditions churn frequently).
- **`RequeueAfter(10s)` safety net** — don't rely solely on VM watch events. The safety requeue handles missed events or edge cases.
- **Preserve existing step recording** — `WaitVMReady` is an additional step after `StartVM`, not a replacement.

### Testing Standards

- Ginkgo + Gomega BDD style for new tests
- Unit tests use `NoOpVMManager` with configurable `IsVMReady` behavior
- Integration tests use envtest with real API server
- Minimum 80% coverage for new code
- All existing tests must continue to pass

### References

- [Source: pkg/engine/executor.go — WaveExecutor.Execute, executeWave, executeGroup]
- [Source: pkg/engine/failover.go — FailoverHandler.ExecuteGroupWithSteps, waitForSync pattern]
- [Source: pkg/engine/vm.go — VMManager interface, KubeVirtVMManager, IsVMRunning]
- [Source: pkg/engine/checkpoint.go — KubeCheckpointer, ErrCheckpointFailed]
- [Source: pkg/engine/resume.go — ResumeAnalyzer.AnalyzeExecution]
- [Source: pkg/controller/drexecution/reconciler.go — SetupWithManager, specOrAnnotationChanged, ensurePlanNameLabel]
- [Source: pkg/controller/drplan/reconciler.go — vmEventHandler, vmRelevantChangePredicate (pattern reference)]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlanSpec, WaveStatus, DRGroupExecutionStatus, StepStatus]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor Agent)

### Debug Log References

- Implemented reactively after UAT Run 1 revealed VMs not verified as Running between waves — no dedicated dev-story workflow run
- Significant refactor: wave executor transitioned from synchronous long-running reconcile to event-driven state machine
- All 12 acceptance criteria verified via `make test` + integration tests

### Completion Notes List

- All 12 acceptance criteria satisfied
- `WaitingForVMReady` group result added to track per-group VM readiness gate
- `WaitVMReady` step recorded for each VM showing ready duration or timeout error
- `IsVMReady(ctx, name, namespace)` added to `VMManager` interface — checks `status.printableStatus == Running`
- `VMReadyTimeout` field added to `DRPlanSpec` (default 5m, validated positive by admission webhook)
- VirtualMachine watch added to DRExecution controller with `vmPrintableStatusChanged` predicate
- `mapVMToDRExecution` handler maps VMs to owning DRExecution via `soteria.io/drplan` label → DRPlan → ActiveExecution
- `reconcileWaveProgress` method drives the state machine: checks readiness, handles timeouts, advances waves
- Mode-dependent timeout policy: disaster = fail-forward (continue), planned_migration = fail-fast (abort)
- `RequeueAfter(10s)` safety net alongside VM watch events
- `VMReadyStartTime` on `WaveStatus` tracks when readiness waiting began
- Checkpoint/resume compatible: `WaitingForVMReady` state survives pod restart
- 1,586 lines added, 98 removed across 17 files

### Change Log

- 2026-04-23: Story 5.6 implemented — event-driven wave gate with VM readiness verification, state machine refactor, VM watch, configurable timeout, mode-dependent policy

### File List

**New files:**
- `pkg/engine/vm_test.go` — `IsVMReady` unit tests for KubeVirt and NoOp managers (81 lines)

**Modified files:**
- `pkg/apis/soteria.io/v1alpha1/types.go` — `VMReadyTimeout` on `DRPlanSpec`, `WaitingForVMReady` result, `VMReadyStartTime` on `WaveStatus`
- `pkg/apis/soteria.io/v1alpha1/validation.go` — `VMReadyTimeout` positive duration validation
- `pkg/apis/soteria.io/v1alpha1/zz_generated.deepcopy.go` — auto-generated
- `pkg/apis/soteria.io/v1alpha1/zz_generated.openapi.go` — auto-generated
- `pkg/engine/vm.go` — `IsVMReady` on `VMManager` interface + `KubeVirtVMManager` implementation
- `pkg/engine/vm_noop.go` — `IsVMReady` on `NoOpVMManager` (configurable)
- `pkg/engine/executor.go` — wave loop yields after handler, `WaitingForVMReady` state tracking
- `pkg/engine/failover.go` — `StepWaitVMReady` constant
- `pkg/engine/failover_test.go` — updated for event-driven flow
- `pkg/engine/resume.go` — `WaitingForVMReady` state in resume analysis
- `pkg/engine/resume_test.go` — resume tests with VM readiness state
- `pkg/controller/drexecution/reconciler.go` — VM watch, `reconcileWaveProgress`, `mapVMToDRExecution`, VM predicate, RBAC markers
- `pkg/controller/drexecution/reconciler_test.go` — comprehensive wave gate tests (789 lines added)
- `config/samples/soteria_v1alpha1_drplan.yaml` — `vmReadyTimeout` field
