# Story 14.7: Test Scenario Setup + Full Lifecycle Integration Test

Status: ready-for-dev

## Story

As a platform engineer,
I want the same test scenario from `hack/stretched-local-test.sh` deployed in the Minikube KVM2 environment and an automated full lifecycle test validating 4 DR transitions with real storage,
so that the platform is proven to work with real Ceph RBD volume replication.

## Acceptance Criteria

**AC1: Test namespace and VM creation**
Given Soteria and KubeVirt are running on both clusters
When the test setup runs (either via script or Go test `BeforeSuite`)
Then namespace `soteria-dr-test` is created on both clusters
And 6 test VMs are created following the `stretched-local-test.sh` wave structure:
  - Wave 1: `fedora-db`
  - Wave 2: `fedora-appserver-1`, `fedora-appserver-2`
  - Wave 3: `fedora-webserver-1`, `fedora-webserver-2`, `fedora-webserver-3`
And east VMs have `runStrategy: Always`, west VMs have `runStrategy: Halted`
And VMs use container disk for boot + Rook-Ceph PVC for data disk (PVC is what gets volume-replicated)
And VMs are labeled with `soteria.io/drplan: fedora-app` and `soteria.io/wave: "<N>"`

**AC2: DRPlan creation**
Given VMs are deployed
When the DRPlan `fedora-app` is created on both clusters
Then `primarySite: east, secondarySite: west, maxConcurrentFailovers: 2`
And `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}`

**AC3: Pre-test sanity verification**
Given the test scenario is deployed
When the test verifies initial state (mirrors UAT-13 sanity checks)
Then DRPlan phase = SteadyState, activeSite = east
And Conditions: Ready=True, SitesInSync=True, DisksConsistent=True, ReplicationHealthy=True
And VR CRs on east = primary, on west = secondary
And East VMs running, west VMs stopped

**AC4: Planned migration east → west (T1: SteadyState → FailedOver)**
Given the system is in SteadyState
When a planned_migration DRExecution is created
Then the execution completes with result = Succeeded
And DRPlan phase = FailedOver, activeSite = west
And VR CRs on west = primary, on east = secondary
And West VMs running, east VMs stopped

**AC5: Reprotect (T2: FailedOver → DRedSteadyState)**
Given the system is in FailedOver
When a reprotect DRExecution is created
Then the execution completes with result = Succeeded
And DRPlan phase = DRedSteadyState

**AC6: Planned migration west → east (T3: DRedSteadyState → FailedBack)**
Given the system is in DRedSteadyState
When a planned_migration DRExecution is created
Then the execution completes with result = Succeeded
And DRPlan phase = FailedBack, activeSite = east
And VR CRs on east = primary, on west = secondary
And East VMs running, west VMs stopped

**AC7: Reprotect (T4: FailedBack → SteadyState)**
Given the system is in FailedBack
When a reprotect DRExecution is created
Then the execution completes with result = Succeeded
And DRPlan phase = SteadyState, activeSite = east

**AC8: Per-transition assertions**
Given each transition completes
When the test validates post-transition state
Then DRExecution Duration field is populated
And all conditions remain healthy (Ready, SitesInSync, DisksConsistent, ReplicationHealthy)
And no checkpoint conflicts in controller logs
And no immutability violations in controller logs

**AC9: Real-storage assertions**
Given real Ceph RBD mirroring is in use (not noop)
When the test checks replication state
Then VolumeReplication status shows non-zero `lastSyncTime` delta between primary and secondary
And measurable RPO from actual Ceph mirroring lag (not noop-instant)

**AC10: Post-lifecycle verification**
Given the full 4-transition lifecycle is complete
When the test verifies final state
Then the system is back to its initial state (SteadyState, activeSite=east, all conditions healthy)

**AC11: Test infrastructure**
Given the test suite
When implemented
Then Go test code lives in `test/multisite/` using Ginkgo/Gomega
And `suite_test.go` bootstraps the Ginkgo suite
And `setup_test.go` handles `BeforeSuite` (scenario setup) and `AfterSuite` (cleanup)
And `lifecycle_test.go` contains the 4-transition lifecycle test
And kubeconfigs are configurable via env vars (`EAST_KUBECONFIG`, `WEST_KUBECONFIG`)
And timeouts are configurable for real-storage timing variability

## Tasks / Subtasks

- [ ] Task 1: Create test suite scaffold `test/multisite/` (AC: 11)
  - [ ] 1.1: Create `test/multisite/suite_test.go` — Ginkgo `TestMultisite` entry with `//go:build multisite` build tag, `RegisterFailHandler(Fail)`, `RunSpecs(t, "Multisite Integration Suite")`
  - [ ] 1.2: Create configurable env vars: `EAST_KUBECONFIG` (default `~/.kube/config`), `WEST_KUBECONFIG` (default `~/.kube/config`), `EAST_CONTEXT` (default `east`), `WEST_CONTEXT` (default `west`), `DR_TEST_NS` (default `soteria-dr-test`), `TRANSITION_TIMEOUT` (default `5m`), `SETUP_TIMEOUT` (default `10m`)
  - [ ] 1.3: Build two `client.Client` instances (east + west) from the respective kubeconfigs/contexts with Soteria + KubeVirt + CSI Addons schemes registered

- [ ] Task 2: Create `test/multisite/setup_test.go` — `BeforeSuite` / `AfterSuite` (AC: 1, 2)
  - [ ] 2.1: `BeforeSuite` creates `soteria-dr-test` namespace on both clusters (idempotent via `--dry-run=client -o yaml | apply`)
  - [ ] 2.2: Create 6 VMs on east (runStrategy=Always) and 6 on west (runStrategy=Halted) — container disk boot (`quay.io/containerdisks/fedora:latest` or `quay.io/kubevirt/cirros-container-disk-demo`) + `rook-ceph-block` PVC for data disk
  - [ ] 2.3: Label all VMs and pod templates with `soteria.io/drplan: fedora-app` and `soteria.io/wave: "<N>"` matching `stretched-local-test.sh` wave structure
  - [ ] 2.4: Create DRPlan `fedora-app` on both clusters: `{primarySite: east, secondarySite: west, maxConcurrentFailovers: 2, volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}}`
  - [ ] 2.5: Wait for DRPlan conditions to stabilize: Ready=True, SitesInSync=True, DisksConsistent=True, ReplicationHealthy=True (poll with configurable timeout)
  - [ ] 2.6: Wait for east VMs to reach Running state
  - [ ] 2.7: `AfterSuite` deletes the `soteria-dr-test` namespace on both clusters, then deletes the cluster-scoped DRPlan `fedora-app`

- [ ] Task 3: Create `test/multisite/lifecycle_test.go` — 4-transition lifecycle test (AC: 3-10)
  - [ ] 3.1: Pre-test sanity check (AC3): assert DRPlan phase=SteadyState, activeSite=east, all 4 conditions healthy, VR CRs correct (east=primary, west=secondary), east VMs Running, west VMs stopped
  - [ ] 3.2: T1 — Planned migration east→west (AC4): create DRExecution `{planName: fedora-app, mode: planned_migration}` on east, poll for result=Succeeded, assert phase=FailedOver, activeSite=west, VR/VM state flipped
  - [ ] 3.3: T2 — Reprotect (AC5): create DRExecution `{planName: fedora-app, mode: reprotect}` on west, poll for result=Succeeded, assert phase=DRedSteadyState
  - [ ] 3.4: T3 — Planned migration west→east (AC6): create DRExecution on west, poll for result=Succeeded, assert phase=FailedBack, activeSite=east
  - [ ] 3.5: T4 — Reprotect (AC7): create DRExecution on east, poll for result=Succeeded, assert phase=SteadyState, activeSite=east
  - [ ] 3.6: Per-transition assertions (AC8): after each transition, verify DRExecution.Status.Duration is non-empty, all conditions remain healthy
  - [ ] 3.7: Real-storage assertions (AC9): after T1, verify VR status has non-zero lastSyncTime on at least one VR CR
  - [ ] 3.8: Post-lifecycle verification (AC10): final state matches initial state

- [ ] Task 4: Helper functions (AC: 3-10)
  - [ ] 4.1: `createDRExecution(ctx, client, name, planName, mode)` — creates DRExecution CR with `soteria.io/plan-name` label
  - [ ] 4.2: `waitForExecResult(ctx, client, name, expectedResult, timeout)` — polls DRExecution for terminal result
  - [ ] 4.3: `assertPlanState(ctx, client, name, expectedPhase, expectedActiveSite)` — verifies DRPlan phase and activeSite
  - [ ] 4.4: `assertConditionsHealthy(ctx, client, planName)` — checks Ready, SitesInSync, DisksConsistent, ReplicationHealthy all True
  - [ ] 4.5: `assertVRState(ctx, client, namespace, expectedState)` — lists VR CRs and checks spec.replicationState
  - [ ] 4.6: `assertVMRunState(ctx, client, namespace, expectedRunning)` — checks VM runStrategy and VMI existence
  - [ ] 4.7: `captureControllerLogs(ctx, clientset, namespace)` — captures controller logs for post-test analysis

## Dev Notes

### Scope and Context

This is the **final story in Epic 14** — the capstone integration test that validates Soteria's full DR lifecycle against real Ceph RBD volume replication. All outputs are Go test files:

- `test/multisite/suite_test.go` — Ginkgo test suite bootstrap
- `test/multisite/setup_test.go` — test scenario deployment/teardown
- `test/multisite/lifecycle_test.go` — 4-transition lifecycle test
- `test/multisite/helpers_test.go` — shared helper functions (optional, can inline)

No shell scripts, no Kustomize files, no operator code changes. This is purely test code exercising the infrastructure built by Stories 14.1-14.6.

### Critical: VM Specification for Minikube KVM2 + Rook-Ceph (NOT Stretched Cluster)

The existing `hack/stretched-local-test.sh` VM spec uses `dataVolumeTemplates` with `sourceRef` pointing to `openshift-virtualization-os-images` — this is an OpenShift-specific mechanism (CDI/DataSource). In Minikube without OpenShift Virtualization:

- **Boot disk:** Use a container disk (`containerDisk` volume) instead of DataVolume/DataSource. Container disks don't use PVCs — the VM image is pulled as a container image at boot. Use `quay.io/containerdisks/fedora:latest` (pre-cached in Story 14.4) or `quay.io/kubevirt/cirros-container-disk-demo` (lighter, faster for tests).
- **Data disk:** Use a separate PVC on `rook-ceph-block` StorageClass. This is the disk that gets volume-replicated. Create the PVC first (or use `dataVolumeTemplates` with `source: blank`), then reference it in the VM spec as a `persistentVolumeClaim` volume.

**The data disk PVC is what Soteria volume-replicates.** The boot disk (container disk) is NOT replicated — it's identical on both sides.

**Note:** The Fedora container disk image should already be cached on all nodes from Story 14.4 (Fedora VM Validation). This eliminates the ~2-3 minute image pull time per VM during test setup.

VM spec structure for Minikube KVM2:
```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: fedora-db
  namespace: soteria-dr-test
  labels:
    soteria.io/drplan: fedora-app
    soteria.io/wave: "1"
spec:
  runStrategy: Always  # or Halted for west cluster
  template:
    metadata:
      labels:
        soteria.io/drplan: fedora-app
        soteria.io/wave: "1"
    spec:
      domain:
        resources:
          requests:
            memory: 256Mi  # minimal for integration test
        devices:
          disks:
            - name: bootdisk
              disk:
                bus: virtio
            - name: datadisk
              disk:
                bus: virtio
      volumes:
        - name: bootdisk
          containerDisk:
            image: quay.io/kubevirt/cirros-container-disk-demo:latest
        - name: datadisk
          persistentVolumeClaim:
            claimName: fedora-db-data
```

Pre-create the data PVC:
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: fedora-db-data
  namespace: soteria-dr-test
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: rook-ceph-block
  resources:
    requests:
      storage: 1Gi
```

### Critical: DRExecution Creation Pattern

DRExecutions are cluster-scoped (not namespaced). Create on the **owning site** (the site that will run the transition):
- Planned migration: create on the **target** site (east→west = create on east, west→east = create on west). Actually no — per the UAT-13 log, planned migrations are created on the current active site.
- Reprotect: create on the **current active site**.

Per UAT-13 verification log:
- T1 (SteadyState→FailedOver, etl6→etl7): execution created on etl6 (active site)
- T2 (FailedOver→DRedSteadyState, reprotect): execution created on etl7 (active site)
- T3 (DRedSteadyState→FailedBack, etl7→etl6): execution created on etl7 (active site)
- T4 (FailedBack→SteadyState, reprotect): execution created on etl6 (active site)

**Rule: always create DRExecution on the current active site.**

DRExecution spec:
```go
exec := &soteriav1alpha1.DRExecution{
    ObjectMeta: metav1.ObjectMeta{
        Name: execName,
        Labels: map[string]string{
            "soteria.io/plan-name": "fedora-app",
        },
    },
    Spec: soteriav1alpha1.DRExecutionSpec{
        PlanName: "fedora-app",
        Mode:     soteriav1alpha1.ExecutionModePlannedMigration, // or ExecutionModeReprotect
    },
}
```

### Critical: Build Tag and Test Isolation

Use `//go:build multisite` build tag on ALL test files. These tests require real Minikube KVM2 clusters with real infrastructure — they must NOT run in CI or during `make test`. They are separate from:
- Unit tests (no build tag, `make test`)
- Integration tests (`//go:build integration`, `make integration`)
- E2E tests (`//go:build e2e`, `test/e2e/`)

Run command: `go test -tags=multisite -v ./test/multisite/ -timeout 30m`

### Critical: Two-Client Architecture

The test needs two separate Kubernetes clients — one for east, one for west. Both need the full scheme (Soteria types + KubeVirt types + CSI Addons VolumeReplication types).

```go
scheme := runtime.NewScheme()
_ = clientgoscheme.AddToScheme(scheme)
_ = soteriav1alpha1.AddToScheme(scheme)
_ = kubevirtv1.AddToScheme(scheme)
_ = replicationv1alpha1.AddToScheme(scheme)

eastCfg, _ := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
    &clientcmd.ClientConfigLoadingRules{ExplicitPath: os.Getenv("EAST_KUBECONFIG")},
    &clientcmd.ConfigOverrides{CurrentContext: os.Getenv("EAST_CONTEXT")},
).ClientConfig()

eastClient, _ := client.New(eastCfg, client.Options{Scheme: scheme})
// same for west
```

With Minikube, each profile has its own kubeconfig at `~/.minikube/profiles/<name>/config`. Alternatively, use the merged kubeconfig with context overrides (`EAST_CONTEXT=east`, `WEST_CONTEXT=west`).

### Critical: Timeout Calibration for Real Storage

UAT-13 timings with noop driver: planned migration ~55-70s, reprotect ~40s. Real Ceph RBD mirroring will be slower:
- VR state transitions take longer (actual Ceph RBD mirror operations)
- Health polling needs time for Ceph to report healthy replication
- DRPlan condition convergence after transition is storage-dependent

Default `TRANSITION_TIMEOUT` should be `5m` (300s) to give ample room. The test should report actual transition times in test output for calibration.

### Critical: VR CR State Assertions

VolumeReplication CRs are namespaced (in `soteria-dr-test`). Their `spec.replicationState` field indicates the desired state:
- `primary` — volume is writable, source of replication
- `secondary` — volume is read-only, target of replication

List VR CRs with label `soteria.io/drplan: fedora-app` to find the ones managed by Soteria.

### Critical: VM Running State Detection

In KubeVirt:
- `runStrategy: Always` + VMI exists and `status.phase=Running` → VM is running
- `runStrategy: Halted` OR no VMI → VM is stopped

Check both the VM's `spec.runStrategy` AND whether a VMI (VirtualMachineInstance) with the same name exists. During transitions, Soteria changes the runStrategy and the KubeVirt controller creates/stops VMIs accordingly.

### Critical: Assertions After Reprotect

Reprotect does NOT change which VMs are running or stopped — it only sets up replication. After reprotect:
- VMs remain on the active site (unchanged from post-failover state)
- VR CRs on the active site become primary (SetSource), passive site remains secondary
- The DRPlan phase advances to the next rest state

Do NOT assert VM state changes after reprotect — only VR state and DRPlan phase/conditions.

### Dependencies

| Dependency | Story | What's Needed |
|------------|-------|---------------|
| Minikube KVM2 clusters + Cilium | 14.1 | Both `east` and `west` clusters running with Cluster Mesh |
| Rook-Ceph | 14.2 | StorageClass `rook-ceph-block`, VolumeReplicationClass `rook-ceph-rbd-vrc`, CSI Addons running |
| KubeVirt + CDI | 14.3 | KubeVirt Deployed on both clusters with nested virtualization, CDI for DataVolume support |
| Fedora image cached | 14.4 | Fedora container disk pre-pulled on all nodes, node sizing validated |
| ScyllaDB | 14.5 | Cross-DC ScyllaDB cluster running on both clusters |
| Soteria | 14.6 | Soteria operator deployed on both clusters with `--site-name=east/west` |

### Existing Test Patterns to Follow

**From `test/integration/controller/suite_test.go`:**
- Standard `TestMain` pattern with `envtest.Environment`
- `waitForCondition`, `waitForExecResult`, `waitForPlanPhase` polling helpers
- `newTestDRPlan` factory function

**From `test/e2e/e2e_suite_test.go`:**
- Ginkgo `BeforeSuite`/`AfterSuite` with `RegisterFailHandler(Fail)`
- Build tag gating (`//go:build e2e`)

**For this story:** Adapt the polling helpers from `test/integration/controller/suite_test.go` to accept a `client.Client` parameter (since we have two clients). The Ginkgo `BeforeSuite`/`AfterSuite` pattern from the e2e suite is the right scaffold.

### Ginkgo Test Structure

```go
var _ = Describe("Full Lifecycle Integration Test", Ordered, func() {
    It("pre-test sanity: initial state is correct", func() { ... })

    It("T1: planned migration east→west (SteadyState → FailedOver)", func() { ... })

    It("T2: reprotect (FailedOver → DRedSteadyState)", func() { ... })

    It("T3: planned migration west→east (DRedSteadyState → FailedBack)", func() { ... })

    It("T4: reprotect (FailedBack → SteadyState)", func() { ... })

    It("post-lifecycle: system returns to initial state", func() { ... })
})
```

Use `Ordered` to ensure sequential execution — each transition depends on the previous one's completion.

### Polling Strategy

Use Gomega `Eventually` with appropriate polling intervals and timeouts:

```go
Eventually(func(g Gomega) {
    var exec soteriav1alpha1.DRExecution
    g.Expect(eastClient.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())
    g.Expect(exec.Status.Result).To(Equal(soteriav1alpha1.ExecutionResultSucceeded))
}).WithTimeout(transitionTimeout).WithPolling(5 * time.Second).Should(Succeed())
```

Use 5s polling interval (matches UAT-13 pattern) with configurable timeout.

### UAT-13 Lessons Learned

From the UAT-13 acceptance testing, these issues were **already resolved** before Epic 14:
1. **Reprotect double-flip race (UAT-13.001/002):** Fixed by removing StopReplication from reprotect + retry-on-conflict
2. **Health count 5/5 instead of 6/6 (UAT-13.003):** Fixed by stale ReplicationHealthy condition correction
3. **Duration field not populated (UAT-13.004):** Fixed by adding Duration persistence
4. **Checkpoint write conflicts (UAT-13.005):** Fixed by Story 13.7 peer-site reconcile guard
5. **Immutability violation (UAT-13.006):** Fixed by Story 13.7 fresh-read terminal guard

The lifecycle test should **verify all these fixes hold** against real storage (not noop):
- Both reprotects return Succeeded (not PartiallySucceeded)
- Health count matches total VG count
- Duration field populated for all executions
- No checkpoint conflicts or immutability violations in logs

### Log Capture Strategy

After each transition, optionally capture controller-manager logs from both clusters. Look for:
- `ERROR` level entries (exclude expected ScyllaDB connection retries during pod startup)
- `"the object has been modified"` (checkpoint conflicts — should be zero with Story 13.7 guard)
- `"is immutable after completion"` (immutability violations — should be zero)

Implementation: use `kubernetes.Clientset` to read pod logs via `corev1.Pods().GetLogs()`. Parse log output line-by-line for error patterns. This is optional but valuable for diagnosing failures.

### Real Storage Timing Expectations

With real Ceph RBD mirroring (not noop):
- VR state transition (SetSource/StopReplication): 5-30s per VR CR (depends on Ceph mirror daemon response)
- VM start after SetSource: 10-30s (container disk boot + KubeVirt VMI scheduling)
- Replication health convergence: 30-120s after reprotect (Ceph mirror needs to establish sync)
- Full planned migration: ~2-4 minutes (Step0 + per-wave sequential with VM readiness)
- Full reprotect: ~1-3 minutes (SetSource per VG + health polling)

Set `TRANSITION_TIMEOUT=5m` as default to accommodate worst-case scenarios.

### Project Structure Notes

```
test/multisite/
├── suite_test.go        # Ginkgo bootstrap, env var config, two-client setup
├── setup_test.go        # BeforeSuite (deploy scenario), AfterSuite (cleanup)
├── lifecycle_test.go    # 4-transition lifecycle test with per-transition assertions
└── helpers_test.go      # Shared polling/assertion helpers (optional)
```

All files use `package multisite_test` and `//go:build multisite` tag.

### References

- [Source: epics.md#Story 14.7] — acceptance criteria and technical notes
- [Source: hack/stretched-local-test.sh lines 430-514] — VM creation pattern, wave structure, DRPlan creation
- [Source: user-acceptance-test-epic-13.md] — UAT-13 lifecycle test pattern, timing expectations, anomaly resolutions
- [Source: test/integration/controller/suite_test.go] — polling helpers (waitForCondition, waitForExecResult, waitForPlanPhase), test plan factory
- [Source: test/e2e/e2e_suite_test.go] — Ginkgo suite scaffold, build tag pattern
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — DRPlanSpec (VolumeReplicationDriverConfig), DRExecutionSpec, ExecutionResult, ExecutionPhase
- [Source: Story 14.1] — Minikube KVM2 cluster contexts (`east`, `west`)
- [Source: Story 14.2] — VolumeReplicationClass `rook-ceph-rbd-vrc`, StorageClass `rook-ceph-block`
- [Source: Story 14.3] — KubeVirt + CDI deployment with nested virtualization
- [Source: Story 14.4] — Fedora VM validation, pre-cached container disk image, node sizing
- [Source: Story 14.5] — ScyllaDB cross-DC deployment
- [Source: Story 14.6] — Soteria deployment, DRPlan spec with csi-extension driver, cross-DC smoke test pattern
- [Source: project-context.md] — DRPlan 8-phase lifecycle, site-aware reconciliation, unified handler model, testing pyramid

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
