# Deferred Work

## Deferred from: code review of 4-05-driver-registry-fallback-preflight-convergence (2026-04-19)

- CSI vs in-tree provisioner ambiguity — `KubeStorageClassLister.GetProvisioner` returns `sc.Provisioner` verbatim; legacy/migrated clusters with in-tree volume types may have provisioner strings that don't match CSI driver registry keys. Normalization or aliases may be needed for non-CSI environments.
