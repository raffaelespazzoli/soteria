# Story 4.1: DR State Machine & Execution Controller

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the orchestrator to enforce valid state transitions for the DR lifecycle and dispatch executions via a controller,
So that plans progress through well-defined states, invalid operations are rejected, and DRExecution creation triggers the workflow engine.

## Acceptance Criteria

1. **AC1 — State machine definition:** `pkg/engine/statemachine.go` defines the 6 DRPlan phases (`SteadyState`, `FailingOver`, `FailedOver`, `Reprotecting`, `DRedSteadyState`, `FailingBack`) and their valid transitions. Valid transitions: SteadyState→FailingOver, FailingOver→FailedOver, FailedOver→Reprotecting, Reprotecting→DRedSteadyState, DRedSteadyState→FailingBack, FailingBack→SteadyState. Invalid transitions return a typed error containing the current and requested phases.

2. **AC2 — Transition function:** `Transition(currentPhase, executionMode) (newPhase, error)` validates whether the requested execution mode is legal given the current phase, and returns the target in-progress phase. `planned_migration` from `SteadyState` → `FailingOver`; `disaster` from `SteadyState` → `FailingOver`; `planned_migration` from `DRedSteadyState` → `FailingBack`; `disaster` from `DRedSteadyState` → `FailingBack`. All other combinations are invalid.

3. **AC3 — Completion transitions:** `CompleteTransition(currentPhase) (newPhase, error)` advances from in-progress phases to their completion targets: `FailingOver` → `FailedOver`, `Reprotecting` → `DRedSteadyState`, `FailingBack` → `SteadyState`. Invalid completion requests return a typed error.

4. **AC4 — DRExecution controller reconciliation:** The DRExecution controller in `pkg/controller/drexecution/reconciler.go` handles newly created DRExecution resources by: (a) fetching the referenced DRPlan; (b) validating the execution mode is `planned_migration` or `disaster`; (c) calling the state machine to validate and apply the phase transition on the DRPlan; (d) setting `DRExecution.Status.StartTime` and initial status; (e) returning `ctrl.Result{}` after setup (engine dispatch is Story 4.2+).

5. **AC5 — Plan not found:** When the referenced DRPlan does not exist, the controller sets `DRExecution.Status.Result = Failed` with a descriptive error condition and does not requeue.

6. **AC6 — Invalid state rejection:** When the DRPlan is not in a valid phase for the requested execution mode, the controller sets `DRExecution.Status.Result = Failed` with a condition explaining the current phase and valid options. The DRPlan phase is not modified.

7. **AC7 — Admission webhook:** `pkg/admission/drexecution_validator.go` validates DRExecution CREATE requests: (a) `.spec.planName` must be non-empty; (b) `.spec.mode` must be `planned_migration` or `disaster`; (c) the referenced DRPlan must exist and be in a valid starting phase for the requested mode. The webhook uses a `client.Reader` (not the controller cache) for plan lookup. Invalid requests are rejected with descriptive messages.

8. **AC8 — Human-triggered only:** No automatic failure detection or auto-failover exists. All failover requires explicit human initiation via DRExecution creation (FR18).

9. **AC9 — Idempotent reconciliation:** If the DRExecution has already been processed (`.status.startTime` is set), the controller does not re-transition the DRPlan or reset state. Re-entrant reconcile calls are safe.

10. **AC10 — Unit tests:** State machine has table-driven tests covering: all valid transitions succeed, all invalid transitions are rejected, concurrent transitions (two goroutines racing on the same phase), completion transitions, error message includes both current and requested phase. Controller tests use envtest to verify: successful execution setup, plan-not-found, invalid-phase rejection, idempotent re-reconcile, mode validation. Admission webhook tests verify: valid CREATE accepted, missing planName rejected, invalid mode rejected, plan in wrong phase rejected.

## Tasks / Subtasks

- [x] Task 1: Implement the DR state machine (AC: #1, #2, #3)
  - [x] 1.1 Create `pkg/engine/statemachine.go` with `ErrInvalidPhaseTransition` typed error that wraps current and requested phases
  - [x] 1.2 Define `validTransitions` map: `map[string]map[ExecutionMode]string` mapping `(currentPhase, mode) → targetPhase`
  - [x] 1.3 Implement `Transition(currentPhase string, mode ExecutionMode) (string, error)` — validates and returns the in-progress target phase
  - [x] 1.4 Define `completionTransitions` map: `map[string]string` mapping in-progress phase → completion phase
  - [x] 1.5 Implement `CompleteTransition(currentPhase string) (string, error)` — returns the next phase after completion
  - [x] 1.6 Implement `ValidStartingPhases(mode ExecutionMode) []string` — returns phases that accept the given mode (for webhook error messages)
  - [x] 1.7 Implement `IsTerminalPhase(phase string) bool` — returns true for `FailedOver`, `DRedSteadyState`, `SteadyState` (steady states where no execution is in progress)
  - [x] 1.8 Add `doc.go` comment or expand existing `pkg/engine/doc.go` to cover state machine

- [x] Task 2: State machine unit tests (AC: #10)
  - [x] 2.1 Create `pkg/engine/statemachine_test.go`
  - [x] 2.2 Table-driven test: `TestTransition_ValidTransitions` — all 4 valid `(phase, mode)` combinations return expected target phase
  - [x] 2.3 Table-driven test: `TestTransition_InvalidTransitions` — all invalid `(phase, mode)` combinations return `ErrInvalidPhaseTransition`
  - [x] 2.4 Test: `TestTransition_InvalidPhase_ReturnsError` — unknown phase string returns error
  - [x] 2.5 Table-driven test: `TestCompleteTransition_ValidCompletions` — all 3 valid completions return expected phase
  - [x] 2.6 Test: `TestCompleteTransition_InvalidPhase_ReturnsError` — non-in-progress phase returns error
  - [x] 2.7 Test: `TestTransition_ErrorMessage_ContainsPhases` — error message includes both current and requested phases
  - [x] 2.8 Test: `TestValidStartingPhases` — returns correct phases per mode
  - [x] 2.9 Test: `TestIsTerminalPhase` — returns true/false correctly for all 6 phases

- [x] Task 3: Implement DRExecution admission webhook (AC: #7)
  - [x] 3.1 Create `pkg/admission/drexecution_validator.go` with `DRExecutionValidator` struct holding `client.Reader` and `admission.Decoder`
  - [x] 3.2 Add `+kubebuilder:webhook` marker for `/validate-soteria-io-v1alpha1-drexecution` path, `failurePolicy=Fail`, CREATE only
  - [x] 3.3 Implement `Handle(ctx, req) admission.Response`: decode DRExecution, validate `.spec.planName` non-empty, validate `.spec.mode` is `planned_migration` or `disaster`
  - [x] 3.4 Fetch the referenced DRPlan using `client.Reader` — if not found, deny with "DRPlan %q not found"
  - [x] 3.5 Call `engine.Transition(plan.Status.Phase, exec.Spec.Mode)` — if error, deny with descriptive message including current phase and valid starting phases
  - [x] 3.6 If all checks pass, allow the request

- [x] Task 4: Register DRExecution webhook (AC: #7)
  - [x] 4.1 Add `ValidateDRExecutionPath` constant to `pkg/admission/setup.go`
  - [x] 4.2 Add `SetupDRExecutionWebhook(mgr ctrl.Manager) error` in `setup.go` — creates `DRExecutionValidator` with `mgr.GetAPIReader()` (uncached) and registers it
  - [x] 4.3 Wire `SetupDRExecutionWebhook` in `cmd/soteria/main.go` alongside existing webhook setup calls

- [x] Task 5: Enhance DRExecution validation in `validation.go` (AC: #7)
  - [x] 5.1 In `pkg/apis/soteria.io/v1alpha1/validation.go`, update `ValidateDRExecution` to check: `.spec.planName` required, `.spec.mode` must be `planned_migration` or `disaster`
  - [x] 5.2 Add `ValidateDRExecutionUpdate` — DRExecution is immutable once created, reject spec changes
  - [x] 5.3 Update validation tests in `validation_test.go`

- [x] Task 6: Implement DRExecution controller reconciliation (AC: #4, #5, #6, #8, #9)
  - [x] 6.1 In `pkg/controller/drexecution/reconciler.go`, add `Recorder events.EventRecorder` field to `DRExecutionReconciler`
  - [x] 6.2 Implement `Reconcile`: fetch DRExecution, check if already processed (idempotency via `.status.startTime` non-nil)
  - [x] 6.3 Fetch the referenced DRPlan — if not found, set `Result=Failed`, add condition `type=Ready, status=False, reason=PlanNotFound`, emit event, return no requeue
  - [x] 6.4 Call `engine.Transition(plan.Status.Phase, exec.Spec.Mode)` — if error, set `Result=Failed`, add condition `type=Ready, status=False, reason=InvalidPhaseTransition`, emit event, return no requeue
  - [x] 6.5 On success: update `plan.Status.Phase` to the returned in-progress phase via status subresource
  - [x] 6.6 Set `exec.Status.StartTime = metav1.Now()`, set initial condition `type=Progressing, status=True, reason=ExecutionStarted`
  - [x] 6.7 Update DRExecution status via status subresource
  - [x] 6.8 Emit `FailoverStarted` event on the DRPlan with execution name and mode
  - [x] 6.9 Return `ctrl.Result{}, nil` — actual engine dispatch happens in Story 4.2+

- [x] Task 7: Update main.go wiring (AC: #4, #7)
  - [x] 7.1 Pass `Recorder` to `DRExecutionReconciler` (create dedicated event recorder for drexecution-controller)
  - [x] 7.2 Call `SetupDRExecutionWebhook(mgr)` after existing webhook setup

- [x] Task 8: Controller unit tests with envtest (AC: #10)
  - [x] 8.1 Create DRExecution CRD fixture in `test/integration/controller/suite_test.go` (or extend existing setup)
  - [x] 8.2 Register DRExecution controller in the test manager alongside DRPlan controller
  - [x] 8.3 Test: `TestDRExecutionReconciler_SuccessfulSetup` — create DRPlan in SteadyState + DRExecution with planned_migration → plan transitions to FailingOver, execution gets startTime
  - [x] 8.4 Test: `TestDRExecutionReconciler_PlanNotFound` — execution references nonexistent plan → Result=Failed, no requeue
  - [x] 8.5 Test: `TestDRExecutionReconciler_InvalidPhase` — DRPlan in FailedOver + planned_migration → Result=Failed, plan phase unchanged
  - [x] 8.6 Test: `TestDRExecutionReconciler_IdempotentRereconcile` — reconcile same execution twice → no double-transition, no error
  - [x] 8.7 Test: `TestDRExecutionReconciler_DisasterMode` — DRPlan in SteadyState + disaster → FailingOver

- [x] Task 9: Admission webhook tests (AC: #10)
  - [x] 9.1 Create `pkg/admission/drexecution_validator_test.go`
  - [x] 9.2 Test: valid DRExecution CREATE accepted (plan exists, correct phase)
  - [x] 9.3 Test: missing `.spec.planName` → denied
  - [x] 9.4 Test: invalid `.spec.mode` → denied
  - [x] 9.5 Test: plan not found → denied with descriptive message
  - [x] 9.6 Test: plan in wrong phase → denied with message explaining valid phases

- [x] Task 10: Documentation and verification
  - [x] 10.1 Update `pkg/engine/doc.go` to cover state machine
  - [x] 10.2 Add/update RBAC markers on DRExecution reconciler for drplans/status update access
  - [x] 10.3 Run `make manifests` to regenerate RBAC/webhook configs
  - [x] 10.4 Run `make generate` if types changed
  - [x] 10.5 Run `make test` — all unit tests pass
  - [x] 10.6 Run `make lint-fix` followed by `make lint` — no new lint errors
  - [x] 10.7 Run `make build` — compiles cleanly

## Dev Notes

### Architecture Context

This is Story 4.1 of Epic 4 (DR Workflow Engine — Full Lifecycle). It establishes the DR state machine and the DRExecution controller that drives the workflow engine. Story 4.05 (Driver Registry Fallback & Preflight Convergence) is a prerequisite — it must be complete so the driver registry is settled before execution stories.

**Story 4.1 scope is intentionally narrow:** it implements the state machine, the controller's initial setup phase (validate + transition + set start time), and the admission webhook. The actual wave execution (calling drivers, DRGroup processing) is Story 4.2+. The controller returns `ctrl.Result{}, nil` after setup — the engine dispatch hook point is left for Story 4.2 to fill in.

### Epic 4 Story Chain

| Story | Deliverable | Relationship |
|-------|-------------|-------------|
| 4.05 | Registry fallback + preflight convergence | Prerequisite — must be done |
| **4.1** | **State machine + execution controller + admission webhook** | **This story** |
| 4.2 | DRGroup chunking & wave executor | Uses state machine; fills in engine dispatch |
| 4.3 | Planned migration workflow | Implements planned.go |
| 4.4 | Disaster failover workflow | Implements disaster.go |
| 4.5 | Fail-forward error handling | Handles driver errors per DRGroup |
| 4.6 | Failed DRGroup retry | Retry mechanism |
| 4.7 | Checkpoint, resume & HA | Persistence across pod restarts |
| 4.8 | Re-protect & failback | Reprotecting + FailingBack paths |

### Existing Code to Modify

| File | Current State | Changes Required |
|------|--------------|-----------------|
| `pkg/controller/drexecution/reconciler.go` | Skeleton — returns `ctrl.Result{}, nil` | Full reconciliation logic: fetch plan, validate, transition, set status |
| `pkg/admission/setup.go` | Has `SetupDRPlanWebhook` and `SetupVMWebhook` | Add `ValidateDRExecutionPath` constant, `SetupDRExecutionWebhook` function |
| `pkg/apis/soteria.io/v1alpha1/validation.go` | `ValidateDRExecution` is a no-op stub | Add field validation for planName and mode; add `ValidateDRExecutionUpdate` |
| `cmd/soteria/main.go` | Wires DRExecution controller with `Client` + `Scheme` only | Add `Recorder`, wire `SetupDRExecutionWebhook` |
| `pkg/engine/doc.go` | Documents discovery, consistency, chunker | Add state machine description |

### New Files to Create

| File | Purpose |
|------|---------|
| `pkg/engine/statemachine.go` | DR lifecycle state machine — phases, transitions, completion |
| `pkg/engine/statemachine_test.go` | State machine unit tests |
| `pkg/admission/drexecution_validator.go` | DRExecution validating admission webhook |
| `pkg/admission/drexecution_validator_test.go` | Webhook tests |

### Existing Code to Preserve (Do NOT Modify)

| File | Reason |
|------|--------|
| `pkg/apis/soteria.io/v1alpha1/types.go` | Phase constants, ExecutionMode, ExecutionResult already defined — use them, don't redefine |
| `pkg/engine/discovery.go` | VM discovery — no changes |
| `pkg/engine/consistency.go` | Consistency resolution — no changes |
| `pkg/engine/chunker.go` | DRGroup chunking — no changes |
| `pkg/controller/drplan/reconciler.go` | DRPlan controller — no changes |
| `pkg/admission/drplan_validator.go` | DRPlan webhook — no changes |
| `pkg/admission/vm_validator.go` | VM webhook — no changes |
| `pkg/drivers/*` | Driver framework — no changes |

### Key Implementation Decisions

**1. State machine is pure functions, not a struct with state.**
The state machine is a collection of pure functions (`Transition`, `CompleteTransition`, `IsTerminalPhase`) that operate on phase strings. No mutable state. The DRPlan's `.status.phase` field is the authoritative state — the state machine validates transitions, it does not hold state.

**2. Phase constants are already defined in `types.go`.**
`PhaseSteadyState`, `PhaseFailingOver`, `PhaseFailedOver`, `PhaseReprotecting`, `PhaseDRedSteadyState`, `PhaseFailingBack` — use these directly. Do NOT create new constants. `ExecutionMode` (`planned_migration`, `disaster`) and `ExecutionResult` (`Succeeded`, `PartiallySucceeded`, `Failed`) are also already defined.

**3. Admission webhook uses `mgr.GetAPIReader()`, not `mgr.GetClient()`.**
The webhook needs to read the DRPlan to validate phase transitions. Using `GetAPIReader()` (uncached reader) ensures the webhook sees the latest state, not stale cache data. This is critical for preventing race conditions where two DRExecutions target the same plan.

**4. Controller uses status subresource for all status updates.**
Both `DRPlan.Status.Phase` and `DRExecution.Status` must be updated via the status subresource (`r.Status().Update(ctx, obj)`). This follows the Kubernetes convention and requires separate RBAC permissions for `status` subresources.

**5. Idempotency check: `.status.startTime != nil`.**
If `DRExecution.Status.StartTime` is already set, the reconciler skips setup. This prevents double phase transitions when the controller re-reconciles (which happens after every status update).

**6. The controller does NOT dispatch the engine yet.**
Story 4.1 validates and transitions the plan, sets initial execution status, then returns. Story 4.2 adds `engine.Execute(...)` call. The controller will evolve to: validate → transition → dispatch engine → checkpoint → complete transition.

**7. Re-protect transitions (FailedOver→Reprotecting) are not triggered by this controller.**
Re-protect is a different action type that Story 4.8 implements. The state machine defines the transitions now, but the controller only handles `planned_migration` and `disaster` modes.

### DRPlan Phase Transition Diagram

```
SteadyState ──(planned_migration/disaster)──► FailingOver ──(complete)──► FailedOver
                                                                              │
                                                                    (reprotect - Story 4.8)
                                                                              ▼
SteadyState ◄──(complete)── FailingBack ◄──(planned_migration/disaster)── DRedSteadyState ◄──(complete)── Reprotecting
```

### RBAC Requirements

The DRExecution controller needs additional RBAC markers beyond the existing skeleton:

```go
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
```

The DRPlan controller already has events RBAC. The DRExecution controller needs its own event recorder.

### Code Patterns to Follow

**State machine error pattern:**

```go
var ErrInvalidPhaseTransition = errors.New("invalid phase transition")

func Transition(currentPhase string, mode v1alpha1.ExecutionMode) (string, error) {
    targets, ok := validTransitions[currentPhase]
    if !ok {
        return "", fmt.Errorf("%w: unknown phase %q", ErrInvalidPhaseTransition, currentPhase)
    }
    target, ok := targets[mode]
    if !ok {
        return "", fmt.Errorf("%w: cannot %s from phase %q", ErrInvalidPhaseTransition, mode, currentPhase)
    }
    return target, nil
}
```

**Controller reconcile pattern (from DRPlan reconciler):**

```go
func (r *DRExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    var exec soteriav1alpha1.DRExecution
    if err := r.Get(ctx, req.NamespacedName, &exec); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Idempotency: already processed
    if exec.Status.StartTime != nil {
        return ctrl.Result{}, nil
    }

    // ... validation, transition, status update ...
}
```

**Admission webhook pattern (from DRPlan validator):**

```go
func (v *DRExecutionValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
    logger := log.FromContext(ctx)
    logger.Info("Validating DRExecution admission", "name", req.Name, "operation", req.Operation)

    if req.Operation != admissionv1.Create {
        return admission.Allowed("")
    }

    exec := &soteriav1alpha1.DRExecution{}
    if err := json.Unmarshal(req.Object.Raw, exec); err != nil {
        return admission.Errored(http.StatusBadRequest, fmt.Errorf("decoding DRExecution: %w", err))
    }

    // field validation + plan lookup + phase check
}
```

**Status condition pattern:**

```go
meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
    Type:               "Ready",
    Status:             metav1.ConditionFalse,
    Reason:             "PlanNotFound",
    Message:            fmt.Sprintf("DRPlan %q not found", exec.Spec.PlanName),
    ObservedGeneration: exec.Generation,
})
```

**Event recording pattern:**

```go
r.Recorder.Eventf(&plan, nil, corev1.EventTypeNormal, "FailoverStarted", "FailoverAction",
    "Failover started for plan %s in %s mode via execution %s", plan.Name, exec.Spec.Mode, exec.Name)
```

Note: The project uses `k8s.io/client-go/tools/events` (not `k8s.io/client-go/tools/record`) — this is the newer events API that creates `events.k8s.io/v1` Event resources. The signature is `Eventf(regarding, related, eventtype, reason, action, messageFmt, args...)`.

### Webhook Configuration

The DRExecution webhook needs a `ValidatingWebhookConfiguration` entry. Since this project uses controller-runtime webhook registration (not kubebuilder-generated manifests for aggregated resources), the webhook is registered manually in `setup.go`. The `config/webhook/manifests.yaml` must be updated with the new path — run `make manifests` after adding the kubebuilder marker.

The webhook should use `failurePolicy: Fail` — blocking invalid executions is better than allowing them and failing at runtime. Use `matchPolicy: Exact` consistent with existing webhooks.

### envtest Test Strategy

Integration tests for the DRExecution controller follow the same pattern as `test/integration/controller/`:

1. The envtest suite already boots an environment with DRPlan CRD and VM CRD
2. Add a DRExecution CRD fixture to the suite
3. Register the DRExecution controller alongside the DRPlan controller in the test manager
4. Create DRPlan objects with specific `.status.phase` values, then create DRExecution objects and verify status transitions

DRPlan is cluster-scoped (no namespace) — use `client.ObjectKey{Name: planName}` without namespace. DRExecution is also cluster-scoped.

### Previous Story Intelligence

**From Story 4.05 (Driver Registry Fallback & Preflight Convergence):**
- Driver resolution is now unified through the registry — the execution controller will use `drivers.DefaultRegistry.GetDriver()` in Story 4.2+ (not needed in this story)
- `--noop-fallback` flag enables dev/CI without real storage

**From Story 3.4 (Conformance Test Suite):**
- Conformance tests validate `StorageProvider` lifecycle — state machine transitions in this story are plan-level, not driver-level
- The conformance suite's lifecycle order (create → setTarget → setSource → stopReplication → delete) will guide the planned migration workflow in Story 4.3

**From Epic 3 Retrospective:**
- DRExecution controller skeleton was wired as a prep task (commit `c284494`)
- The skeleton watches `DRExecution` resources and returns `ctrl.Result{}, nil`
- Event recording pattern established by DRPlan controller: `events.NewEventBroadcasterAdapterWithContext`

### Git Intelligence

Recent commits follow pattern: `Implement Story X.Y: Short Description`. The DRExecution controller skeleton was added in `c284494` (Epic 3 retrospective). The DRPlan controller (`pkg/controller/drplan/reconciler.go`) demonstrates the full reconcile pattern including event recording, status updates, and structured logging.

### Important: DRExecution is Cluster-Scoped

Both DRPlan and DRExecution are cluster-scoped resources (Epic 2b.1.5 migrated all CRDs to cluster scope). The controller uses `req.Name` (no namespace). The `NamespacedName` in reconcile requests will have an empty namespace. In envtest tests, use `client.ObjectKey{Name: name}` without namespace.

### Backwards Compatibility

- No existing API contract changes — DRExecution types already exist in `types.go`
- DRExecution controller currently does nothing (skeleton) — adding logic is additive
- New admission webhook adds a gate but existing clients don't create DRExecutions yet
- DRPlan controller is unaffected — it continues to reconcile discovery, consistency, chunking, and preflight

### Build Commands

```bash
make manifests    # Regenerate RBAC + webhook configs after adding markers
make generate     # Regenerate deepcopy if types changed
make test         # All unit tests (new + existing)
make lint-fix     # Auto-fix code style
make lint         # Verify lint passes
make build        # Verify compilation
make integration  # Integration tests
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.1] — Story acceptance criteria (BDD format)
- [Source: _bmad-output/planning-artifacts/architecture.md#CRD Status Patterns] — DRPlan phase values, DRExecution result enum
- [Source: _bmad-output/planning-artifacts/architecture.md#Controller Patterns] — Reconcile return patterns, structured logging, context propagation
- [Source: _bmad-output/planning-artifacts/architecture.md#API & Communication Patterns] — Checkpointing, error model, execution modes
- [Source: _bmad-output/project-context.md#Critical Implementation Rules] — No in-memory state across reconciles, use CRD status
- [Source: _bmad-output/project-context.md#Testing Rules] — envtest over fake client, test naming conventions
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — Phase constants, ExecutionMode, ExecutionResult, DRExecution type definition
- [Source: pkg/controller/drexecution/reconciler.go] — Current skeleton controller
- [Source: pkg/controller/drplan/reconciler.go] — Reference reconciler pattern (events, status updates, logging)
- [Source: pkg/admission/setup.go] — Webhook registration pattern
- [Source: pkg/admission/drplan_validator.go] — Admission webhook implementation pattern
- [Source: pkg/apis/soteria.io/v1alpha1/validation.go] — Validation function pattern, `ValidateDRExecution` stub
- [Source: cmd/soteria/main.go] — Controller wiring, event broadcaster setup
- [Source: test/integration/controller/suite_test.go] — envtest suite setup, CRD fixtures, test manager wiring

### Review Findings

- [x] [Review][Patch] Log "from" value is wrong after mutation — captured `previousPhase` before mutation, log now uses it
- [x] [Review][Patch] Exec/plan patch ordering risk — reordered: exec status patched before plan phase, so re-reconcile is idempotent
- [x] [Review][Patch] Controller failure condition omits valid phases (AC6) — added `ValidStartingPhases` to failure message, matching webhook
- [x] [Review][Patch] Missing concurrent transition test (AC10) — added `TestTransition_ConcurrentCalls` with 100 goroutines
- [x] [Review][Patch] Event reason says "FailoverStarted" even for failback — now emits `FailbackStarted`/`FailbackAction` when target is FailingBack
- [x] [Review][Patch] `failExecution` log missing WithValues — added `WithValues("drexecution", exec.Name, "reason", reason)`
- [x] [Review][Defer] FailedOver→Reprotecting not in state machine — deferred, Story 4.8 designs the reprotect mechanism and mode
- [x] [Review][Defer] Pre-existing test patterns (StorageClass AlreadyExists guard, manager goroutine error propagation) — deferred, pre-existing

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (Cursor Agent)

### Debug Log References

- Integration test `TestDRExecutionReconciler_IdempotentRereconcile` initially failed due to resourceVersion conflict — DRPlan controller reconciles concurrently with test setup. Fixed by adding `setPlanPhase()` retry helper that re-fetches before status update.

### Completion Notes List

- State machine implemented as pure functions (no mutable state) — `Transition`, `CompleteTransition`, `ValidStartingPhases`, `IsTerminalPhase`
- 4 valid transitions wired: SteadyState→FailingOver (planned_migration|disaster), DRedSteadyState→FailingBack (planned_migration|disaster)
- 3 completion transitions: FailingOver→FailedOver, Reprotecting→DRedSteadyState, FailingBack→SteadyState
- DRExecution controller validates mode, fetches plan, calls state machine, transitions plan phase, sets execution startTime + Progressing condition
- Idempotency via `.status.startTime != nil` check — re-reconcile is safe
- Failed executions (plan not found, invalid phase) get `Result=Failed` + descriptive `Ready=False` condition + startTime + completionTime
- Admission webhook uses `mgr.GetAPIReader()` (uncached) for plan lookups to prevent stale-cache races
- Webhook validates: planName required, mode is planned_migration|disaster, plan exists, plan in valid starting phase
- `ValidateDRExecution` and `ValidateDRExecutionUpdate` added to aggregated API server strategy layer (defense-in-depth)
- 9 state machine unit tests, 6 webhook unit tests, 5 controller integration tests (envtest), 7 validation tests — all pass
- No new lint errors introduced (pre-existing `goconst` in `storage_test.go` unchanged)
- `make manifests` regenerated RBAC (drexecutions verbs now include update/patch; drplans accessible from DRExecution controller) and webhook config (new ValidatingWebhookConfiguration entry)

### File List

New files:
- pkg/engine/statemachine.go
- pkg/engine/statemachine_test.go
- pkg/admission/drexecution_validator.go
- pkg/admission/drexecution_validator_test.go
- test/integration/controller/drexecution_test.go

Modified files:
- pkg/controller/drexecution/reconciler.go
- pkg/admission/setup.go
- pkg/apis/soteria.io/v1alpha1/validation.go
- pkg/apis/soteria.io/v1alpha1/validation_test.go
- cmd/soteria/main.go
- pkg/engine/doc.go
- test/integration/controller/suite_test.go
- config/rbac/role.yaml (auto-generated)
- config/webhook/manifests.yaml (auto-generated)
- _bmad-output/implementation-artifacts/sprint-status.yaml
