# Deferred Work

## Deferred from: code review of 4-05-driver-registry-fallback-preflight-convergence (2026-04-19)

- CSI vs in-tree provisioner ambiguity — `KubeStorageClassLister.GetProvisioner` returns `sc.Provisioner` verbatim; legacy/migrated clusters with in-tree volume types may have provisioner strings that don't match CSI driver registry keys. Normalization or aliases may be needed for non-CSI environments.

## Deferred from: code review of 4-1-dr-state-machine-execution-controller (2026-04-19)

- FailedOver→Reprotecting transition not yet defined in state machine — Story 4.8 will design the reprotect mechanism and mode, then add this transition edge to `validTransitions`.
- Pre-existing test patterns: `StorageClass` creation in `suite_test.go` lacks AlreadyExists guard; manager Start goroutine error not propagated to test runner.
