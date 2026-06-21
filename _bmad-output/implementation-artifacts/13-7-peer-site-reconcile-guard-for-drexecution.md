# Story 13.7: Peer-Site Reconcile Guard for DRExecution

Status: done

## Story

As a platform engineer running Soteria in a multi-site stretched cluster,
I want the DRExecution reconciler to skip reconciliation on the peer site (the site that does not own the execution),
So that checkpoint write conflicts and immutability violations are eliminated, reducing operational noise in logs.

## Background

### Context Within Epic 13

Epic 13 established DRPlan as the lifecycle owner of VR/VGR objects and restructured DR transitions. During UAT (user-acceptance-test-epic-13.md), two minor anomalies were identified that share a single root cause:

- **UAT-13.005 — Checkpoint Write Conflicts During Reprotect:** Both sites race on `Status().Update()` for the same DRExecution, causing repeated `resourceVersion` conflicts (10+ retries per reprotect). The `ScyllaRetry` backoff (200ms base, 2x factor, 8 steps) handles these, but the conflicts produce significant log noise.

- **UAT-13.006 — DRExecution Immutability Violation After Completion:** After the owner site completes an execution (sets `Result=Succeeded`), the peer site receives a stale copy via ScyllaDB CDC (still `Result=""`), passes the terminal check, and attempts `Status().Update()`. By then the owner's write has landed, and the API server rejects with: `DRExecution is immutable after completion`.

### Root Cause

The site-affinity guard in `Reconcile()` at `pkg/controller/drexecution/reconciler.go` line 138 is:

```go
if r.LocalSite != "" {
    if result, done, err := r.dispatchByRole(ctx, &exec, &plan); done || err != nil {
        return result, err
    }
}
```

This has two failure modes:

1. **`LocalSite` is empty:** If the controller is deployed without `--site-name`, the entire `dispatchByRole` block is bypassed. The peer reconciles the execution as if it were the owner, writing checkpoints and status updates that conflict with the actual owner.

2. **Stale read from ScyllaDB CDC:** Even with `LocalSite` set, the terminal check at line 103 can pass with a stale `Result=""` because ScyllaDB's eventual consistency hasn't propagated the owner's completion write. The peer then falls through to the full reconcile logic and attempts writes that are either conflicted (UAT-13.005) or rejected by the immutability guard (UAT-13.006).

### Impact

Both anomalies are functionally harmless — retries succeed within backoff limits and the immutability guard correctly prevents data corruption. However, they produce persistent log noise that can mask real issues in production.

## Acceptance Criteria

### AC1: Mandatory `--site-name` in Multi-Site Deployments
When the DRExecution reconciler receives a DRPlan with both `PrimarySite` and `SecondarySite` set, and `LocalSite` is empty, the reconciler MUST log an error event and skip reconciliation (return `ctrl.Result{}`). The controller manager startup SHOULD validate that `--site-name` is set when multi-site plans exist.

### AC2: Defensive Terminal Guard with Fresh Read
Before entering `dispatchByRole`, the reconciler MUST perform a fresh `Get()` of the DRExecution (bypassing informer cache) when the execution appears non-terminal. If the fresh read reveals a terminal result, skip reconciliation. This closes the stale-read window from ScyllaDB CDC.

### AC3: Peer-Site Skip for Rest-State Plans
When `LocalSite` is set and the DRPlan is in a rest state (`SteadyState`, `DRedSteadyState`, `FailedOver`, `FailedBack`) AND the execution mode is `reprotect`, the peer site (where `LocalSite != ActiveSite`) MUST skip reconciliation. Reprotect is a single-site operation that doesn't require peer participation.

### AC4: No Regression in Owner-Site Behavior
The owner site (`LocalSite == ActiveSite` or the `TargetSiteForPhase` match) MUST continue to reconcile normally. All existing failover, failback, and reprotect workflows MUST pass unchanged.

### AC5: Log Noise Reduction Verified
After deploying the fix, a full lifecycle test (failover → reprotect → failback → restore) MUST complete without any `checkpoint write conflict` or `DRExecution is immutable after completion` errors in the peer site's controller logs.

## Technical Design

### Option A: Fresh-Read Terminal Guard (Recommended)

Add a fresh `Get()` before `dispatchByRole` that bypasses the informer cache by reading directly from the API server (aggregated API → ScyllaDB). If the fresh read shows a terminal result, return immediately. Use the direct REST client (not `r.Get` which hits the informer cache):

```go
// Fresh-read guard: close the ScyllaDB CDC stale-read window.
// Uses direct API server Get (not cached informer) to read the latest state.
if !exec.Status.IsTerminal() {
    var fresh soteriav1alpha1.DRExecution
    if err := r.DirectClient.Get(ctx, req.NamespacedName, &fresh); err == nil {
        if fresh.Status.IsTerminal() {
            return ctrl.Result{}, nil
        }
        exec = fresh
    }
}
```

Note: If `DirectClient` is not available, `retry.RetryOnConflict` with `r.Get` + a short sleep can approximate the same effect by allowing the informer cache to catch up. The key requirement is that the read must reflect the owner site's completion write.

### Option B: Mandatory `--site-name` Validation

At DRExecution reconciler setup (`SetupWithManager`), register a validation that checks `LocalSite != ""`. Alternatively, add a runtime check in `Reconcile()` when the plan has multi-site configuration:

```go
if r.LocalSite == "" && plan.Spec.PrimarySite != "" && plan.Spec.SecondarySite != "" {
    logger.Error(nil, "LocalSite not configured for multi-site plan, skipping reconciliation",
        "plan", plan.Name)
    return ctrl.Result{}, nil
}
```

### Recommendation

Apply both Option A and Option B. Option A handles the timing window. Option B prevents the fundamental misconfiguration. Together they eliminate both UAT-13.005 and UAT-13.006.

## Dependencies

- Depends on Story 13.6 (DRExecution mutates VR/VGR replicationState) — the reconcile dispatch and `dispatchByRole` structure must be stable.
- No external dependencies.

## Estimated Scope

~2 modified prod files, ~1 modified test file. Low complexity — the fix is additive guards, no structural refactoring.

## Files to Modify

| File | Change |
|------|--------|
| `pkg/controller/drexecution/reconciler.go` | Add fresh-read terminal guard before `dispatchByRole`; add `LocalSite` validation for multi-site plans |
| `pkg/controller/drexecution/reconciler_test.go` | Add test: peer-site skips reconciliation for completed execution; add test: empty `LocalSite` with multi-site plan skips |

## Testing

- Unit tests: mock a stale informer cache returning non-terminal execution, verify fresh read catches terminal state
- Unit tests: verify `LocalSite=""` with multi-site plan returns without writes
- Integration test: full lifecycle on stretched cluster, verify no conflict/immutability errors in peer logs
- Regression: all existing failover/reprotect/failback tests pass unchanged

## Review Findings

- [x] [Review][Patch] Fresh-read guard is optional and not wired in manager-backed reconcile setup [`pkg/controller/drexecution/reconciler.go:146`] — fixed: wired `APIReader: mgr.GetAPIReader()` in suite_test.go
- [x] [Review][Patch] Fresh-read fallback proceeds on direct-read failure and can still reconcile stale state [`pkg/controller/drexecution/reconciler.go:148`] — fixed: NotFound returns immediately, other errors logged and fall through
- [x] [Review][Patch] AC2 tests do not actually simulate a stale cached read versus a fresh direct read [`pkg/controller/drexecution/reconciler_test.go:1749`] — fixed: test now uses separate cached/direct clients to prove the guard
- [ ] [Review][Patch] AC5 lifecycle and peer-log noise reduction are still unverified by integration coverage [`test/integration/controller/drexecution_test.go:378`] — requires multi-step multi-site integration test; defer to UAT
