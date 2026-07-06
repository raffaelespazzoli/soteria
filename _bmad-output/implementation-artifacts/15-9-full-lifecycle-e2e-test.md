# Story 15.9: Full Lifecycle E2E Test with Real Storage (Moved from 14.7)

Status: done

## Story

As a platform engineer,
I want a parameterized E2E test suite validating the full DR lifecycle across a matrix of failover modes (planned migration vs disaster) and mirroring types (snapshot vs streaming) with real Ceph RBD replication,
so that the platform is proven to work end-to-end with real storage, ShadowPV cross-cluster PV provisioning, and actual cluster failure scenarios.

## Acceptance Criteria

**AC1: Test infrastructure and parameterized framework**
Given the test suite
When implemented
Then Go test code lives in `test/multisite/` using Ginkgo/Gomega
And `suite_test.go` bootstraps the Ginkgo suite with `//go:build multisite`
And `setup_test.go` handles `BeforeSuite` (minimal: clients + namespace) and `AfterSuite` (cleanup)
And `convergence_test.go` contains the DRPlan-to-healthy-state test
And `lifecycle_test.go` contains the parameterized 4-scenario lifecycle matrix
And `helpers_test.go` contains shared factories, assertions, polling, and minikube control helpers
And kubeconfigs are configurable via env vars (`EAST_KUBECONFIG`, `WEST_KUBECONFIG`)
And timeouts are configurable for real-storage timing variability
And each scenario uses a unique VM/DRPlan prefix for isolation
And VMs are created and destroyed per test (resource constraints: max 6 VMs at a time)

**AC2: DRPlan convergence to healthy state via ShadowPV pipeline (Test 1)**
Given Soteria and KubeVirt are running on both clusters
When the convergence test runs
Then on east: 6 data PVCs (`rook-ceph-block`, 1Gi) and 6 VMs (container disk boot + PVC data disk, `runStrategy: Always`) are created
And on west: 6 VMs (container disk boot, reference to PVC, `runStrategy: Halted`) are created WITHOUT data PVCs
And DRPlan is created with `primarySite: east, secondarySite: west`
And DRPlan initially has `DisksConsistent=False` (expected: west has no matching PVCs yet)
And ShadowPV publisher creates ShadowPV resources from east VR/VGR PVC→PV chain
And ShadowPV consumer on west creates PVs from remote ShadowPV entries (with pool-ID rewrite)
And the test creates west PVCs with `spec.volumeName: <shadowpv-created-pv-name>` to bind to those PVs
And DRPlan converges to all conditions healthy: Ready=True, SitesInSync=True, DisksConsistent=True, ReplicationHealthy=True
And east VMs reach Running state (VMI phase=Running)
And VR CRs on east = primary, on west = secondary
And VMs are torn down after the test

**AC3: Planned migration full lifecycle with snapshot mirroring (Test 2)**
Given a healthy DRPlan with `volumeReplicationClass: rook-ceph-rbd-vrc-snapshot`
When the planned lifecycle test runs
Then 4 transitions complete successfully:
  - T1: planned_migration east→west (ResyncPending observed, FailedOver, VMs flipped)
  - T2: reprotect on west (DRedSteadyState, conditions healthy)
  - T3: planned_migration west→east (ResyncPending observed, FailedBack, VMs flipped)
  - T4: reprotect on east (SteadyState, conditions healthy)
And system returns to initial state (SteadyState, activeSite=east)
And per-transition assertions pass (Duration, Phase=Succeeded, IsActive=false, conditions healthy)
And real-storage assertions pass (non-zero lastSyncTime on VR CRs)
And no checkpoint conflicts or immutability violations in controller logs

**AC4: Planned migration full lifecycle with streaming mirroring (Test 3)**
Given a healthy DRPlan with `volumeReplicationClass: rook-ceph-rbd-vrc-journal`
When the planned lifecycle test runs
Then the same 4 transitions complete successfully as AC3
And streaming-specific timing is recorded for calibration

**AC5: Disaster failover lifecycle with snapshot mirroring (Test 4)**
Given a healthy DRPlan with `volumeReplicationClass: rook-ceph-rbd-vrc-snapshot`
When the disaster lifecycle test runs
Then `minikube stop -p east` is executed (source cluster goes offline)
And the west API server remains responsive (sanity check)
And a `disaster` DRExecution is created on west (surviving site)
And the execution completes with result = Succeeded
And DRPlan phase = FailedOver, activeSite = west on the west cluster
And `minikube start -p east` is executed (source cluster recovers)
And the test waits for east API server to become ready
And a `reprotect` DRExecution is created on west to re-establish replication
And DRPlan converges to DRedSteadyState with all conditions healthy
And VMs are torn down

**AC6: Disaster failover lifecycle with streaming mirroring (Test 5)**
Given a healthy DRPlan with `volumeReplicationClass: rook-ceph-rbd-vrc-journal`
When the disaster lifecycle test runs
Then the same disaster + recovery flow as AC5 completes successfully

**AC7: Two VolumeReplicationClasses in infrastructure**
Given the Rook-Ceph infrastructure
When `setup-rook-ceph.sh` deploys storage resources
Then VolumeReplicationClass `rook-ceph-rbd-vrc-snapshot` exists (mirroringMode: snapshot, schedulingInterval: 1m)
And VolumeReplicationClass `rook-ceph-rbd-vrc-journal` exists (mirroringMode: journal)
And both are deployed on both clusters

**AC8: ScyllaDB symmetric seeds for disaster resilience**
Given the ScyllaDB cross-DC deployment
When scyllacluster patches are applied
Then east has externalSeeds pointing to west (`soteria-scylladb-west-rack1-0.soteria.svc.clusterset.local`)
And west has externalSeeds pointing to east (`soteria-scylladb-east-rack1-0.soteria.svc.clusterset.local`)
And either DC can go offline and the surviving DC continues to operate
And the recovering DC can rejoin the cluster via its external seed

**AC9: Per-transition assertions (all lifecycle tests)**
Given each transition completes
When the test validates post-transition state
Then DRExecution Duration field is populated
And DRExecution Phase field = Succeeded, IsActive = false
And all conditions remain healthy (Ready, SitesInSync, DisksConsistent, ReplicationHealthy)
And no checkpoint conflicts in controller logs
And no immutability violations in controller logs
And VolumeReplication status shows non-zero `lastSyncTime`

**AC10: ShadowPV state verification (all lifecycle tests)**
Given each lifecycle test runs
When transitions occur
Then ShadowPV entries update to reflect site ownership changes
And ShadowPV resources remain consistent after the lifecycle completes

## Tasks / Subtasks

- [x] Task 1: Infrastructure changes (AC: 7, 8)
  - [x] 1.1: Rename `hack/multisite/manifests/rook-ceph/volume-replication-class.yaml` to contain both VRCs: `rook-ceph-rbd-vrc-snapshot` (mirroringMode: snapshot, schedulingInterval: 1m) and `rook-ceph-rbd-vrc-journal` (mirroringMode: journal)
  - [x] 1.2: Update `hack/multisite/setup-rook-ceph.sh` to apply the updated VRC manifest
  - [x] 1.3: Update `hack/multisite/overlays/east/scyllacluster-patch.yaml` to add symmetric external seed pointing to west
  - [x] 1.4: Update any references to the old VRC name `rook-ceph-rbd-vrc` across the codebase (story specs, deploy scripts, smoke tests)

- [x] Task 2: Refactor test suite scaffold (AC: 1)
  - [x] 2.1: Update `suite_test.go` — add `EAST_MINIKUBE_PROFILE` and `WEST_MINIKUBE_PROFILE` env vars for minikube control
  - [x] 2.2: Refactor `setup_test.go` — `BeforeSuite` becomes minimal (clients + namespace creation only), `AfterSuite` cleans up namespace
  - [x] 2.3: Define `lifecycleScenario` struct in `helpers_test.go` with fields: name, failoverMode, volumeReplicationClass, simulateDisaster, vmPrefix

- [x] Task 3: Refactor helpers for per-test VM lifecycle (AC: 1, 2)
  - [x] 3.1: Create `deployScenario(ctx, scenario)` — creates east PVCs + VMs (Always) + west VMs (Halted, no PVCs) + DRPlan with scenario VRC
  - [x] 3.2: Create `waitForShadowPVConsumerPVs(ctx, westClient, planName)` — polls for PVs created by ShadowPV consumer on west
  - [x] 3.3: Create `createWestPVCsFromShadowPVs(ctx, westClient, planName)` — creates west PVCs with `spec.volumeName` binding to ShadowPV-provisioned PVs
  - [x] 3.4: Create `waitForDRPlanHealthy(ctx, client, planName)` — polls for all 4 conditions healthy
  - [x] 3.5: Create `teardownScenario(ctx, scenario)` — deletes DRPlan, VMs, PVCs, ShadowPVs for the scenario
  - [x] 3.6: Create `minikubeStop(profile string)` and `minikubeStart(profile string)` — shells out to minikube CLI
  - [x] 3.7: Create `waitForAPIServer(ctx, client)` — polls until client can reach the API server

- [x] Task 4: Create `convergence_test.go` — DRPlan healthy state test (AC: 2)
  - [x] 4.1: Deploy scenario (east PVCs + VMs, west VMs without PVCs, DRPlan)
  - [x] 4.2: Assert DRPlan initially has `DisksConsistent=False`
  - [x] 4.3: Wait for ShadowPV publisher to create ShadowPV resources
  - [x] 4.4: Wait for ShadowPV consumer to create PVs on west
  - [x] 4.5: Create west PVCs with `spec.volumeName` binding
  - [x] 4.6: Assert DRPlan converges to all conditions healthy
  - [x] 4.7: Assert east VMs Running, west VMs Halted, VR CRs correct
  - [x] 4.8: Assert ShadowPV entries have correct cluster name
  - [x] 4.9: Teardown scenario

- [x] Task 5: Rewrite `lifecycle_test.go` — parameterized matrix (AC: 3, 4, 5, 6, 9, 10)
  - [x] 5.1: Define 4 scenarios: planned-snapshot, planned-journal, disaster-snapshot, disaster-journal
  - [x] 5.2: For each planned scenario: deploy → converge → T1 (planned_migration, observe ResyncPending) → T2 (reprotect) → T3 (planned_migration) → T4 (reprotect) → verify initial state → teardown
  - [x] 5.3: For each disaster scenario: deploy → converge → minikube stop east → disaster exec on west → assert FailedOver → minikube start east → wait API server → reprotect → assert DRedSteadyState → teardown
  - [x] 5.4: Per-transition assertions: Duration, Phase=Succeeded, IsActive=false, conditions healthy
  - [x] 5.5: Real-storage assertions: non-zero lastSyncTime on VR CRs
  - [x] 5.6: ShadowPV assertions: entries update after transitions
  - [x] 5.7: Log scanning: no checkpoint conflicts, no immutability violations

### Review Findings

- [x] [Review][Patch] Story marks the rewrite complete while the same file still says code rework is pending [`_bmad-output/implementation-artifacts/15-9-full-lifecycle-e2e-test.md:408`]
- [x] [Review][Patch] `setup_test.go` does not contain the `BeforeSuite`/`AfterSuite` hooks the story marks complete [`test/multisite/suite_test.go:64`]
- [x] [Review][Patch] Convergence test does not require `DisksConsistent=False`; it passes if the condition is missing [`test/multisite/convergence_test.go:62`]
- [x] [Review][Patch] ShadowPV consumer PV lookup and teardown are not isolated per scenario, so stale west PVs can satisfy later runs [`test/multisite/helpers_test.go:290`]
- [x] [Review][Patch] Disaster failover does not run the real-storage and log checks on the failover transition itself, despite AC9 being marked complete [`test/multisite/lifecycle_test.go:185`]
- [x] [Review][Patch] Task 1.4 is checked complete even though stale `rook-ceph-rbd-vrc` references remain in the repo [`hack/multisite/setup-all.sh:290`]

## Dev Notes

### Scope and Context

This is the **capstone story of Epic 15** — the integration test that validates the entire DR lifecycle against real Ceph RBD volume replication. The test suite exercises a 2×2 matrix plus a convergence test:

| # | Test | Failover Mode | Mirroring | Disaster Sim |
|---|------|---------------|-----------|--------------|
| 1 | DRPlan convergence | N/A | snapshot (default) | No |
| 2 | Full lifecycle | planned_migration | snapshot | No |
| 3 | Full lifecycle | planned_migration | journal (streaming) | No |
| 4 | Full lifecycle | disaster | snapshot | minikube stop east |
| 5 | Full lifecycle | disaster | journal (streaming) | minikube stop east |

### Critical: ShadowPV-Driven West Cluster PV Provisioning

The correct flow for west cluster data disks:

**East side (source of truth):**
1. Create VM and PVC on east
2. PVC triggers dynamic provisioner → PV created → PVC binds
3. When VM/PVC/PV become part of a DRPlan, ShadowPV publisher creates ShadowPV resource
4. West ShadowPV consumer reads remote entries and creates PV with pool-ID rewrite

**West side (DR target — initially incomplete):**
1. Create VM with reference to PVC name but do NOT create PVC
2. Create DRPlan — will initially show `DisksConsistent=False` (expected)
3. Wait for ShadowPV consumer to create PV on west
4. Create PVC with `spec.volumeName: <pv-name>` to bind to the ShadowPV-provisioned PV

This validates the entire ShadowPV provisioning pipeline end-to-end.

### Critical: Disaster Simulation via Minikube Stop

For disaster failover tests, the east minikube cluster is actually stopped:

```bash
minikube stop -p east    # Source cluster goes offline
# ... disaster failover on west ...
minikube start -p east   # Source cluster recovers
```

**Key behaviors during disaster:**
- East API server unreachable — controller-runtime client gets connection errors
- ScyllaDB east DC offline — west ScyllaDB continues operating (symmetric seeds ensure rejoin)
- Ceph RBD mirrors on east go stale — west promotes volumes without source acknowledgment
- After restart: kubeconfig unchanged (same VM IP, same certs), client reconnects automatically
- After restart: ScyllaDB east rejoins via `externalSeeds` pointing to west
- After restart: Ceph mirrors re-establish from last known state

**No client rebuild needed** — minikube uses static kubeconfig. The existing controller-runtime client reconnects when the API server is available.

### Critical: Per-Test VM Lifecycle

Due to resource constraints (Minikube KVM2 nodes have limited memory), VMs are created and destroyed per test. Each scenario uses a unique `vmPrefix` (e.g., `conv-`, `ps-`, `pj-`, `ds-`, `dj-`) to avoid collisions:

```go
type lifecycleScenario struct {
    name                   string
    failoverMode           soteriav1alpha1.ExecutionMode
    volumeReplicationClass string
    simulateDisaster       bool
    vmPrefix               string
}
```

Tests run sequentially (Ginkgo `Ordered` + `Serial`) — only one set of 6 VMs exists at a time.

### Critical: Two VolumeReplicationClasses

| VRC Name | mirroringMode | Notes |
|----------|---------------|-------|
| `rook-ceph-rbd-vrc-snapshot` | snapshot | Periodic snapshot-based, schedulingInterval: 1m |
| `rook-ceph-rbd-vrc-journal` | journal | Continuous journal-based (streaming), near-zero RPO |

Both deployed by `setup-rook-ceph.sh` as static infrastructure. DRPlan references the VRC via `volumeReplicationDriver.volumeReplicationClass`.

### Critical: ScyllaDB Symmetric Seeds

Both DCs must have external seeds pointing to each other so either can survive independently:

```yaml
# East patch:
- op: replace
  path: /spec/externalSeeds
  value:
    - soteria-scylladb-west-rack1-0.soteria.svc.clusterset.local

# West patch (unchanged):
- op: replace
  path: /spec/externalSeeds
  value:
    - soteria-scylladb-east-rack1-0.soteria.svc.clusterset.local
```

### Critical: Disaster Lifecycle Shape

Disaster tests have a shorter lifecycle than planned tests:

**Planned (4 transitions):** SteadyState → FailedOver → DRedSteadyState → FailedBack → SteadyState

**Disaster (2 transitions):** SteadyState → [minikube stop] → FailedOver → [minikube start] → Reprotect → DRedSteadyState

Disaster tests do NOT attempt failback because the scenario is "east failed, we recovered on west." A full round-trip (disaster failover + planned failback) would be a separate future test.

### VM Specification for Minikube KVM2 + Rook-Ceph

Use container disk for boot (NOT DataVolume/DataSource). The data PVC on `rook-ceph-block` is what gets volume-replicated.

```go
vm := &kubevirtv1.VirtualMachine{
    ObjectMeta: metav1.ObjectMeta{
        Name:      vmPrefix + vmName,
        Namespace: testNamespace,
        Labels: map[string]string{
            "soteria.io/drplan": planName,
            "soteria.io/wave":   waveNum,
        },
    },
    Spec: kubevirtv1.VirtualMachineSpec{
        RunStrategy: ptr.To(kubevirtv1.RunStrategyAlways),
        Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
            ObjectMeta: metav1.ObjectMeta{
                Labels: map[string]string{
                    "soteria.io/drplan": planName,
                    "soteria.io/wave":   waveNum,
                },
            },
            Spec: kubevirtv1.VirtualMachineInstanceSpec{
                Domain: kubevirtv1.DomainSpec{
                    Resources: kubevirtv1.ResourceRequirements{
                        Requests: corev1.ResourceList{
                            corev1.ResourceMemory: resource.MustParse("256Mi"),
                        },
                    },
                    Devices: kubevirtv1.Devices{
                        Disks: []kubevirtv1.Disk{
                            {Name: "bootdisk", DiskDevice: kubevirtv1.DiskDevice{Disk: &kubevirtv1.DiskTarget{Bus: kubevirtv1.DiskBusVirtio}}},
                            {Name: "datadisk", DiskDevice: kubevirtv1.DiskDevice{Disk: &kubevirtv1.DiskTarget{Bus: kubevirtv1.DiskBusVirtio}}},
                        },
                    },
                },
                Volumes: []kubevirtv1.Volume{
                    {Name: "bootdisk", VolumeSource: kubevirtv1.VolumeSource{
                        ContainerDisk: &kubevirtv1.ContainerDiskSource{
                            Image: "quay.io/kubevirt/cirros-container-disk-demo:latest",
                        },
                    }},
                    {Name: "datadisk", VolumeSource: kubevirtv1.VolumeSource{
                        PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
                            PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
                                ClaimName: vmPrefix + vmName + "-data",
                            },
                        },
                    }},
                },
            },
        },
    },
}
```

### DRExecution Creation Pattern

**Rule: always create DRExecution on the current active site.**

- Planned T1 (east→west): execution on **east**
- Planned T2 (reprotect): execution on **west**
- Planned T3 (west→east): execution on **west**
- Planned T4 (reprotect): execution on **east**
- Disaster failover: execution on **west** (east is offline)
- Disaster reprotect: execution on **west** (active site after failover)

### Timeout Calibration for Real Storage

| Operation | Snapshot Timing | Journal Timing | Notes |
|-----------|----------------|----------------|-------|
| VR state transition | 5-30s | 2-10s | Journal is faster |
| ResyncVolume completion | 5-60s | 2-30s | Small test data |
| VM start after SetSource | 10-30s | 10-30s | Same (container disk) |
| Health convergence | 30-120s | 15-60s | Journal converges faster |
| Full planned migration | 3-6m | 2-4m | |
| Full reprotect | 1-3m | 1-2m | |
| minikube stop | 5-15s | 5-15s | |
| minikube start + cluster ready | 60-120s | 60-120s | Kubelet + Ceph + ScyllaDB |

Default `TRANSITION_TIMEOUT=5m`, `DISASTER_RECOVERY_TIMEOUT=3m`, `CLUSTER_RESTART_TIMEOUT=3m`.

### Test File Structure

```
test/multisite/
├── suite_test.go           # Ginkgo bootstrap, env vars, scheme, two-client setup
├── setup_test.go           # BeforeSuite (clients + namespace), AfterSuite (cleanup)
├── convergence_test.go     # Test 1: DRPlan → healthy via ShadowPV pipeline
├── lifecycle_test.go       # Tests 2-5: parameterized matrix (planned/disaster × snapshot/journal)
└── helpers_test.go         # Scenario factories, assertions, polling, minikube control
```

All files use `package multisite_test` and `//go:build multisite` tag.

### Dependencies

| Dependency | Story | What's Needed |
|------------|-------|---------------|
| Minikube KVM2 clusters + Cilium | 14.1 | Both `east` and `west` clusters running |
| Rook-Ceph with two VRCs | 14.2 | `rook-ceph-rbd-vrc-snapshot` + `rook-ceph-rbd-vrc-journal` |
| KubeVirt + CDI | 14.3 | KubeVirt on both clusters |
| ScyllaDB symmetric seeds | 14.5 | Both DCs can survive independently |
| Soteria deployed | 15.8 | Operator on both clusters with all Epic 15 code |
| ResyncVolume driver | 15.1 | StorageProvider `ResyncVolume` available |
| Resync guard | 15.2 | Planned failover ResyncPending condition |
| Reprotect simplification | 15.3 | GetReplicationStatus + conditional ResyncVolume |
| ShadowPV CRD | 15.4 | ShadowPV type registered |
| ShadowPV publisher | 15.5 | Publisher creates ShadowPV from VR/VGR |
| ShadowPV consumer | 15.6 | Consumer creates PVs from remote entries |

### Existing Code Assessment (from first implementation pass)

The current `test/multisite/` files from the first pass are ~70% reusable:
- `suite_test.go` — 90% reusable (add minikube env vars)
- `helpers_test.go` — 80% reusable (assertions are parameterized, add scenario factories + minikube helpers)
- `setup_test.go` — 30% reusable (shrink BeforeSuite, move factories out)
- `lifecycle_test.go` — 20% reusable (completely restructured as matrix)

Recommendation: **rework** existing code rather than discard.

### References

- [Source: epics.md#Epic 15, Story 15.9]
- [Source: hack/multisite/setup-rook-ceph.sh — VRC deployment]
- [Source: hack/multisite/overlays/east/scyllacluster-patch.yaml — ScyllaDB seed config]
- [Source: hack/multisite/overlays/west/scyllacluster-patch.yaml — ScyllaDB seed config]
- [Source: pkg/apis/soteria.io/v1alpha1/types.go — DRPlanSpec, DRExecutionSpec, ShadowPVSpec]
- [Source: test/integration/controller/suite_test.go — polling helpers pattern]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- First pass implementation completed (all 4 files, lint clean, tests pass)
- User correction: west PVCs should NOT be pre-created; ShadowPV consumer creates PVs, test creates PVCs to bind
- User decision: test matrix (planned/disaster × snapshot/journal) = 5 tests total
- User decision: per-test VM lifecycle (create + destroy) due to resource constraints
- User decision: disaster simulation via actual `minikube stop` (not just mode flag)
- User decision: ScyllaDB symmetric seeds for disaster resilience
- User decision: VRC renamed to `rook-ceph-rbd-vrc-snapshot` + new `rook-ceph-rbd-vrc-journal`
- User decision: `spec.volumeName` for west PVC binding to ShadowPV-created PVs

### Completion Notes List

- First pass: 4 Go test files created, lint clean, all existing tests pass
- Story rewritten to capture corrected requirements (test matrix, disaster sim, ShadowPV flow)
- Review fixes aligned the rewritten story, multisite test suite, and active docs/scripts

### File List

- `test/multisite/suite_test.go` (new) — Ginkgo suite entry, two-client setup
- `test/multisite/setup_test.go` (new) — BeforeSuite/AfterSuite and namespace helpers
- `test/multisite/convergence_test.go` (new) — DRPlan convergence test via ShadowPV pipeline
- `test/multisite/lifecycle_test.go` (new) — parameterized lifecycle matrix
- `test/multisite/helpers_test.go` (new) — shared helpers, polling, cleanup, log scanning
- `hack/multisite/manifests/rook-ceph/volume-replication-class.yaml` (updated) — two VRCs
- `hack/multisite/setup-rook-ceph.sh` (updated) — apply both VRCs
- `hack/multisite/overlays/east/scyllacluster-patch.yaml` (updated) — symmetric seeds
- `hack/multisite/setup-all.sh` (updated) — teardown old/new VRCs
- `hack/multisite/README.md` (updated) — two-VRC docs and smoke-test example
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (modified) — status tracking
- `_bmad-output/implementation-artifacts/15-9-full-lifecycle-e2e-test.md` (modified) — this file

### Change Log

- 2026-07-06 (first pass): Implemented initial 4-file test suite (single lifecycle, observation-only ShadowPV)
- 2026-07-06 (story rewrite): Rewrote story spec to capture: test matrix (5 tests), ShadowPV-driven provisioning, disaster simulation via minikube stop, per-test VM lifecycle, two VRCs, symmetric ScyllaDB seeds
