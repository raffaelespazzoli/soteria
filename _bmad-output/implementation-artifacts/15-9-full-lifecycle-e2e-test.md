# Story 15.9: Full Lifecycle E2E Test with Real Storage (Moved from 14.7)

Status: ready-for-dev

## Story

As a platform engineer,
I want an automated full lifecycle E2E test validating 4 DR transitions with real Ceph RBD replication including Epic 15 features (resync guard, ShadowPV cross-cluster PV provisioning, reprotect simplification),
so that the platform is proven to work end-to-end with real storage and cross-cluster PV management.

## Acceptance Criteria

**AC1: Test namespace and VM creation**
Given Soteria and KubeVirt are running on both clusters (deployed by Story 15-8)
When the test `BeforeSuite` runs
Then namespace `soteria-dr-test` is created on both clusters
And 6 test VMs are created following the `stretched-local-test.sh` wave structure:
  - Wave 1: `fedora-db`
  - Wave 2: `fedora-appserver-1`, `fedora-appserver-2`
  - Wave 3: `fedora-webserver-1`, `fedora-webserver-2`, `fedora-webserver-3`
And east VMs have `runStrategy: Always`, west VMs have `runStrategy: Halted`
And VMs use container disk for boot + `rook-ceph-block` PVC for data disk (PVC is what gets volume-replicated)
And VMs are labeled with `soteria.io/drplan: fedora-app` and `soteria.io/wave: "<N>"`

**AC2: DRPlan creation**
Given VMs are deployed
When the DRPlan `fedora-app` is created on both clusters
Then `primarySite: east, secondarySite: west, maxConcurrentFailovers: 2`
And `volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}`

**AC3: Pre-test sanity verification**
Given the test scenario is deployed
When the test verifies initial state
Then DRPlan phase = SteadyState, activeSite = east
And Conditions: Ready=True, SitesInSync=True, DisksConsistent=True, ReplicationHealthy=True
And VR CRs on east = primary, on west = secondary
And East VMs running, west VMs stopped

**AC4: ShadowPV initial state verification**
Given the system is in SteadyState with VR CRs active
When the ShadowPV publisher has processed VR/VGR CRs
Then ShadowPV resources exist (one per VG, named `fedora-app-<vgName>`)
And each ShadowPV has entries from the local cluster with `clusterName` matching the site
And the ShadowPV consumer has created corresponding PVs on the remote cluster
And remote PVs have pool-ID rewritten for the local Ceph cluster (or as-is if non-Ceph format not detected)

**AC5: Planned migration east → west (T1: SteadyState → FailedOver) with resync guard**
Given the system is in SteadyState
When a `planned_migration` DRExecution is created on east (active site)
Then the execution enters Step 0: StopVM → ResyncVolume on target VGs
And a `ResyncPending` condition appears on the DRExecution (resync guard from Story 15-2)
And the `ResyncPending` condition resolves after VR resync completes (event-driven, not polled)
And StopReplication demotes source VGs after resync completion
And wave execution proceeds (SetSource + StartVM on target)
And the execution completes with result = Succeeded
And DRPlan phase = FailedOver, activeSite = west
And VR CRs on west = primary, on east = secondary
And West VMs running, east VMs stopped

**AC6: ShadowPV state after failover**
Given the system is in FailedOver
When the ShadowPV publisher processes the new VR state
Then ShadowPV entries are updated to reflect the new site ownership
And PVs on the east cluster include the formerly-remote entries (now local after failover)

**AC7: Reprotect (T2: FailedOver → DRedSteadyState) with simplified handler**
Given the system is in FailedOver
When a `reprotect` DRExecution is created on west (active site)
Then the reprotect handler verifies VR state via `GetReplicationStatus` (Story 15-3)
And stale primaries are detected and `ResyncVolume` is called on them
And health monitoring proceeds
And the execution completes with result = Succeeded
And DRPlan phase = DRedSteadyState
And conditions return to healthy (Ready, SitesInSync, DisksConsistent, ReplicationHealthy)

**AC8: Planned migration west → east (T3: DRedSteadyState → FailedBack) with resync guard**
Given the system is in DRedSteadyState
When a `planned_migration` DRExecution is created on west (active site)
Then the resync guard activates again (ResyncPending condition lifecycle)
And the execution completes with result = Succeeded
And DRPlan phase = FailedBack, activeSite = east
And VR CRs on east = primary, on west = secondary
And East VMs running, west VMs stopped

**AC9: Reprotect (T4: FailedBack → SteadyState)**
Given the system is in FailedBack
When a `reprotect` DRExecution is created on east (active site)
Then the execution completes with result = Succeeded
And DRPlan phase = SteadyState, activeSite = east

**AC10: Per-transition assertions**
Given each transition completes
When the test validates post-transition state
Then DRExecution Duration field is populated
And DRExecution Phase field = Succeeded, IsActive = false
And all conditions remain healthy (Ready, SitesInSync, DisksConsistent, ReplicationHealthy)
And no checkpoint conflicts in controller logs
And no immutability violations in controller logs

**AC11: Real-storage assertions**
Given real Ceph RBD mirroring is in use
When the test checks replication state
Then VolumeReplication status shows non-zero `lastSyncTime`
And measurable RPO from actual Ceph mirroring lag (not noop-instant)

**AC12: Post-lifecycle verification**
Given the full 4-transition lifecycle is complete
When the test verifies final state
Then the system is back to its initial state (SteadyState, activeSite=east, all conditions healthy)
And ShadowPV resources still exist with correct entries matching the final state

**AC13: Test infrastructure**
Given the test suite
When implemented
Then Go test code lives in `test/multisite/` using Ginkgo/Gomega
And `suite_test.go` bootstraps the Ginkgo suite with `//go:build multisite`
And `setup_test.go` handles `BeforeSuite` (scenario setup) and `AfterSuite` (cleanup)
And `lifecycle_test.go` contains the 4-transition lifecycle test
And `helpers_test.go` contains shared polling/assertion helpers
And kubeconfigs are configurable via env vars (`EAST_KUBECONFIG`, `WEST_KUBECONFIG`)
And timeouts are configurable for real-storage timing variability

## Tasks / Subtasks

- [ ] Task 1: Create test suite scaffold `test/multisite/` (AC: 13)
  - [ ] 1.1: Create `test/multisite/suite_test.go` — Ginkgo `TestMultisite` entry with `//go:build multisite` build tag, `RegisterFailHandler(Fail)`, `RunSpecs(t, "Multisite Integration Suite")`
  - [ ] 1.2: Create configurable env vars: `EAST_KUBECONFIG` (default `~/.kube/config`), `WEST_KUBECONFIG` (default `~/.kube/config`), `EAST_CONTEXT` (default `east`), `WEST_CONTEXT` (default `west`), `DR_TEST_NS` (default `soteria-dr-test`), `TRANSITION_TIMEOUT` (default `5m`), `SETUP_TIMEOUT` (default `10m`), `SHADOWPV_TIMEOUT` (default `2m`)
  - [ ] 1.3: Build two `client.Client` instances (east + west) from the respective kubeconfigs/contexts with Soteria + KubeVirt + CSI Addons + CephBlockPool schemes registered
  - [ ] 1.4: Build two `kubernetes.Clientset` instances for pod log capture

- [ ] Task 2: Create `test/multisite/setup_test.go` — `BeforeSuite` / `AfterSuite` (AC: 1, 2)
  - [ ] 2.1: `BeforeSuite` creates `soteria-dr-test` namespace on both clusters (idempotent via `client.Create` + `IgnoreAlreadyExists`)
  - [ ] 2.2: Create 6 data PVCs on east cluster (`rook-ceph-block` SC, 1Gi each, named `<vm-name>-data`)
  - [ ] 2.3: Create 6 VMs on east (`runStrategy: Always`, container disk boot + PVC data disk) and 6 on west (`runStrategy: Halted`, container disk boot, NO data PVC — PVs provisioned by ShadowPV consumer)
  - [ ] 2.4: Label all VMs and pod templates with `soteria.io/drplan: fedora-app` and `soteria.io/wave: "<N>"`
  - [ ] 2.5: Create DRPlan `fedora-app` on east: `{primarySite: east, secondarySite: west, maxConcurrentFailovers: 2, volumeReplicationDriver: {type: csi-extension, volumeReplicationClass: rook-ceph-rbd-vrc}}`
  - [ ] 2.6: Wait for DRPlan conditions to stabilize: Ready=True, SitesInSync=True, DisksConsistent=True, ReplicationHealthy=True (poll with configurable timeout)
  - [ ] 2.7: Wait for east VMs to reach Running state (VMI exists + phase=Running)
  - [ ] 2.8: Wait for ShadowPV resources to appear and have entries from east cluster
  - [ ] 2.9: `AfterSuite` deletes the `soteria-dr-test` namespace on both clusters, then deletes the cluster-scoped DRPlan `fedora-app` and any ShadowPV resources with `soteria.io/drplan: fedora-app`

- [ ] Task 3: Create `test/multisite/lifecycle_test.go` — 4-transition lifecycle test (AC: 3-12)
  - [ ] 3.1: Pre-test sanity check (AC3, AC4): assert DRPlan phase=SteadyState, activeSite=east, all 4 conditions healthy, VR CRs correct (east=primary, west=secondary), east VMs Running, west VMs stopped, ShadowPV resources exist with expected entries
  - [ ] 3.2: T1 — Planned migration east→west (AC5, AC6): create DRExecution on east, observe ResyncPending condition, poll for result=Succeeded, assert phase=FailedOver, activeSite=west, VR/VM state flipped, ShadowPV entries updated
  - [ ] 3.3: T2 — Reprotect (AC7): create DRExecution on west, poll for result=Succeeded, assert phase=DRedSteadyState, conditions healthy
  - [ ] 3.4: T3 — Planned migration west→east (AC8): create DRExecution on west, observe ResyncPending, poll for result=Succeeded, assert phase=FailedBack, activeSite=east
  - [ ] 3.5: T4 — Reprotect (AC9): create DRExecution on east, poll for result=Succeeded, assert phase=SteadyState, activeSite=east
  - [ ] 3.6: Per-transition assertions (AC10): after each transition, verify Duration non-empty, Phase=Succeeded, IsActive=false, conditions healthy
  - [ ] 3.7: Real-storage assertions (AC11): after T1, verify VR status has non-zero lastSyncTime on at least one VR CR
  - [ ] 3.8: Post-lifecycle verification (AC12): final state matches initial state, ShadowPV resources consistent

- [ ] Task 4: Create `test/multisite/helpers_test.go` — shared helper functions (AC: 3-12)
  - [ ] 4.1: `createDRExecution(ctx, cl, name, planName, mode)` — creates DRExecution CR with `soteria.io/plan-name` label
  - [ ] 4.2: `waitForExecResult(ctx, cl, name, expectedResult, timeout)` — polls DRExecution for terminal result
  - [ ] 4.3: `assertPlanState(ctx, cl, name, expectedPhase, expectedActiveSite)` — verifies DRPlan phase and activeSite
  - [ ] 4.4: `assertConditionsHealthy(ctx, cl, planName)` — checks Ready, SitesInSync, DisksConsistent, ReplicationHealthy all True
  - [ ] 4.5: `assertVRState(ctx, cl, namespace, expectedState)` — lists VR CRs with `soteria.io/drplan: fedora-app` and checks spec.replicationState
  - [ ] 4.6: `assertVMRunState(ctx, cl, namespace, expectRunning)` — checks VM runStrategy and VMI existence
  - [ ] 4.7: `assertShadowPVEntries(ctx, cl, planName, expectedClusterName)` — lists ShadowPV resources and verifies entries
  - [ ] 4.8: `observeResyncPending(ctx, cl, execName, timeout)` — polls DRExecution conditions for ResyncPending=True appearance then resolution
  - [ ] 4.9: `captureControllerLogs(ctx, clientset, namespace)` — captures manager pod logs for error pattern scanning
  - [ ] 4.10: `scanLogsForErrors(logs string)` — checks for checkpoint conflicts, immutability violations, unexpected errors
  - [ ] 4.11: `activeClient(eastCl, westCl, activeSite)` — returns the client for the current active site
  - [ ] 4.12: `waitForCondition(ctx, cl, planName, condType, expectedStatus, timeout)` — generic condition poller

## Dev Notes

### Scope and Context

This is the **capstone story of Epic 15** — the integration test that validates the entire DR lifecycle against real Ceph RBD volume replication, including all Epic 15 features. All outputs are Go test files:

- `test/multisite/suite_test.go` — Ginkgo test suite bootstrap + two-client setup
- `test/multisite/setup_test.go` — test scenario deployment/teardown
- `test/multisite/lifecycle_test.go` — 4-transition lifecycle test with Epic 15 feature assertions
- `test/multisite/helpers_test.go` — shared helper functions

No shell scripts, no Kustomize files, no operator code changes. This is purely test code exercising the infrastructure built by Stories 14.1-14.5 + 15.8, and the operator code from Stories 15.1-15.7.

### Critical: Epic 15 Features Under Test

This E2E test validates three Epic 15 feature categories against real storage:

**1. Resync Guard (Stories 15-1, 15-2):**
- Planned failover Step 0 calls `ResyncVolume` on target VGs before promotion
- `ResyncPending` condition appears on DRExecution during sync wait
- Event-driven completion (VR/VGR watch triggers reconcile)
- `StopReplication` (source demotion) only after resync completes
- Observable assertion: `ResyncPending=True` → `ResyncPending` removed → `Step0Complete`

**2. ShadowPV Cross-Cluster PV Provisioning (Stories 15-4, 15-5, 15-6):**
- ShadowPV publisher creates ShadowPV resources from VR/VGR PVC→PV chain
- ShadowPV consumer creates local PVs from remote entries with Ceph pool-ID rewrite
- ShadowPV entries update as VR ownership changes during failover
- Observable assertions: ShadowPV resources exist, remote PVs created, pool-ID rewritten

**3. Reprotect Simplification (Story 15-3):**
- Reprotect Phase 1 calls `GetReplicationStatus` instead of `SetSource`
- Stale primaries get `ResyncVolume` call (fire-and-forget)
- Health monitoring proceeds immediately
- Observable assertion: reprotect completes Succeeded (not PartiallySucceeded)

### Critical: VM Specification for Minikube KVM2 + Rook-Ceph

Use container disk for boot (NOT DataVolume/DataSource — those are OCP-specific). The data PVC on `rook-ceph-block` is what gets volume-replicated.

```go
vm := &kubevirtv1.VirtualMachine{
    ObjectMeta: metav1.ObjectMeta{
        Name:      vmName,
        Namespace: testNamespace,
        Labels: map[string]string{
            "soteria.io/drplan": "fedora-app",
            "soteria.io/wave":   waveNum,
        },
    },
    Spec: kubevirtv1.VirtualMachineSpec{
        RunStrategy: ptr.To(kubevirtv1.VirtualMachineRunStrategyAlways),
        Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
            ObjectMeta: metav1.ObjectMeta{
                Labels: map[string]string{
                    "soteria.io/drplan": "fedora-app",
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
                                ClaimName: vmName + "-data",
                            },
                        },
                    }},
                },
            },
        },
    },
}
```

Pre-create data PVCs:
```go
pvc := &corev1.PersistentVolumeClaim{
    ObjectMeta: metav1.ObjectMeta{
        Name:      vmName + "-data",
        Namespace: testNamespace,
    },
    Spec: corev1.PersistentVolumeClaimSpec{
        AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
        StorageClassName: ptr.To("rook-ceph-block"),
        Resources: corev1.VolumeResourceRequirements{
            Requests: corev1.ResourceList{
                corev1.ResourceStorage: resource.MustParse("1Gi"),
            },
        },
    },
}
```

### Critical: West Cluster VM Data Disks

On the west cluster, data PVCs should NOT be pre-created manually — they should be provisioned by the ShadowPV consumer controller. The ShadowPV publisher on east discovers PVs backing VR CRs and publishes them to ShadowPV resources (stored in ScyllaDB, visible cross-cluster). The consumer on west reads remote entries and creates local PVs with pool-ID rewrite. The west VMs reference data PVCs that will bind to these ShadowPV-provisioned PVs.

**However**, PVCs must still be created on west because PVs alone are not sufficient — PVCs need to exist to bind to the PVs. The ShadowPV consumer creates PVs, not PVCs. Create west PVCs with the same names but do NOT specify `storageClassName` — let them bind to the ShadowPV-provisioned PVs via `volumeName` if available, or create them after ShadowPV PVs appear.

**Practical approach for MVP:** Create data PVCs on both clusters with `rook-ceph-block` StorageClass. Rook-Ceph with RBD mirroring handles the underlying volume replication transparently. ShadowPV verification is an observation-only assertion (verify ShadowPV resources exist and have correct entries) rather than relying on ShadowPV for actual PV provisioning in the test flow. The ShadowPV feature enables PV discovery across clusters — verify it works, but don't depend on it for the test's VM setup.

### Critical: DRExecution Creation Pattern

**Rule: always create DRExecution on the current active site.**

Per UAT-13 verification log:
- T1 (SteadyState→FailedOver, east→west): execution created on **east** (active site)
- T2 (FailedOver→DRedSteadyState, reprotect): execution created on **west** (active site)
- T3 (DRedSteadyState→FailedBack, west→east): execution created on **west** (active site)
- T4 (FailedBack→SteadyState, reprotect): execution created on **east** (active site)

DRExecution is cluster-scoped (not namespaced):
```go
exec := &soteriav1alpha1.DRExecution{
    ObjectMeta: metav1.ObjectMeta{
        Name: execName,
        Labels: map[string]string{
            soteriav1alpha1.PlanNameLabel: "fedora-app",
        },
    },
    Spec: soteriav1alpha1.DRExecutionSpec{
        PlanName: "fedora-app",
        Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
    },
}
```

### Critical: ResyncPending Condition Observation

The resync guard (Story 15-2) introduces a `ResyncPending` condition on DRExecution during planned failover. The E2E test should observe this condition's lifecycle:

```go
func observeResyncPending(ctx context.Context, cl client.Client, execName string, timeout time.Duration) {
    // Phase 1: Wait for ResyncPending=True to appear
    Eventually(func(g Gomega) {
        var exec soteriav1alpha1.DRExecution
        g.Expect(cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())
        cond := meta.FindStatusCondition(exec.Status.Conditions, "ResyncPending")
        g.Expect(cond).NotTo(BeNil(), "ResyncPending condition should appear")
        g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
    }).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())

    // Phase 2: Wait for ResyncPending to be removed (resync completed)
    Eventually(func(g Gomega) {
        var exec soteriav1alpha1.DRExecution
        g.Expect(cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())
        cond := meta.FindStatusCondition(exec.Status.Conditions, "ResyncPending")
        g.Expect(cond).To(BeNil(), "ResyncPending should be removed after resync completes")
    }).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
}
```

**Note:** This is a best-effort observation — the resync may complete very quickly with small test data volumes, making the ResyncPending window narrow. If the condition appears and resolves too fast to observe, the test should still pass (the successful completion of the planned migration implicitly proves the resync guard worked). Use `Consistently` with a short duration if needed to verify the condition appeared at some point.

**Fallback approach:** If timing makes ResyncPending observation unreliable, verify instead that the DRExecution completed Succeeded AND VR CRs show correct state transitions (secondary→resync→secondary(completed)→primary on target side). The absence of data loss is the real assertion.

### Critical: ShadowPV Assertion Strategy

ShadowPV resources are cluster-scoped and stored in ScyllaDB (visible on both clusters). After DRPlan stabilization:

```go
func assertShadowPVEntries(ctx context.Context, cl client.Client, planName, expectedCluster string) {
    var shadowPVList soteriav1alpha1.ShadowPVList
    Expect(cl.List(ctx, &shadowPVList, client.MatchingLabels{
        "soteria.io/drplan": planName,
    })).To(Succeed())
    Expect(shadowPVList.Items).NotTo(BeEmpty(), "ShadowPV resources should exist")
    
    for _, spv := range shadowPVList.Items {
        hasLocalEntry := false
        for _, entry := range spv.Spec.PVs {
            if entry.ClusterName == expectedCluster {
                hasLocalEntry = true
                Expect(entry.PVName).NotTo(BeEmpty())
                Expect(entry.PV).NotTo(BeZero())
            }
        }
        Expect(hasLocalEntry).To(BeTrue(),
            "ShadowPV %s should have entry from cluster %s", spv.Name, expectedCluster)
    }
}
```

### Critical: Build Tag and Test Isolation

Use `//go:build multisite` build tag on ALL test files. These tests require real Minikube KVM2 clusters with real infrastructure — they must NOT run during `make test` or in CI.

Run command: `go test -tags=multisite -v ./test/multisite/ -timeout 30m`

### Critical: Two-Client Architecture

```go
var (
    eastClient client.Client
    westClient client.Client
    eastClientset *kubernetes.Clientset
    westClientset *kubernetes.Clientset
    testNamespace string
    transitionTimeout time.Duration
    setupTimeout time.Duration
    shadowPVTimeout time.Duration
)

var _ = BeforeSuite(func() {
    scheme := runtime.NewScheme()
    Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
    Expect(soteriav1alpha1.AddToScheme(scheme)).To(Succeed())
    Expect(kubevirtv1.AddToScheme(scheme)).To(Succeed())
    Expect(replicationv1alpha1.AddToScheme(scheme)).To(Succeed())

    eastCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
        &clientcmd.ClientConfigLoadingRules{ExplicitPath: os.Getenv("EAST_KUBECONFIG")},
        &clientcmd.ConfigOverrides{CurrentContext: os.Getenv("EAST_CONTEXT")},
    ).ClientConfig()
    Expect(err).NotTo(HaveOccurred())
    eastClient, err = client.New(eastCfg, client.Options{Scheme: scheme})
    Expect(err).NotTo(HaveOccurred())
    // ... same for west ...
})
```

Each Minikube profile has its own kubeconfig. Use the merged kubeconfig with context overrides (`EAST_CONTEXT=east`, `WEST_CONTEXT=west`).

### Critical: Timeout Calibration for Real Storage

UAT-13 timings with noop driver: planned migration ~55-70s, reprotect ~40s. Real Ceph RBD mirroring adds:
- VR state transitions: 5-30s per VR CR (Ceph mirror daemon response)
- ResyncVolume completion: 5-60s (depends on data delta — small test data should be fast)
- VM start after SetSource: 10-30s (container disk boot)
- Health convergence after reprotect: 30-120s (Ceph mirror establishment)
- Full planned migration with resync guard: ~3-6 minutes
- Full reprotect with simplified handler: ~1-3 minutes

Default `TRANSITION_TIMEOUT=5m` (300s). Report actual transition times in test output for calibration.

### Critical: Assertions After Reprotect

Reprotect does NOT change which VMs are running or stopped — it only sets up replication. After reprotect:
- VMs remain on the active site (unchanged from post-failover state)
- VR CRs on the active site verified as secondary (GetReplicationStatus), stale primaries get ResyncVolume
- DRPlan phase advances to the next rest state
- Health conditions converge to healthy

Do NOT assert VM state changes after reprotect — only VR state, DRPlan phase, and conditions.

### Critical: VR CR State Assertions

VolumeReplication CRs are namespaced (in `soteria-dr-test`). List VR CRs with label `soteria.io/drplan: fedora-app`:

```go
func assertVRState(ctx context.Context, cl client.Client, ns, expectedState string) {
    var vrList replicationv1alpha1.VolumeReplicationList
    Expect(cl.List(ctx, &vrList,
        client.InNamespace(ns),
        client.MatchingLabels{"soteria.io/drplan": "fedora-app"},
    )).To(Succeed())
    Expect(vrList.Items).NotTo(BeEmpty(), "VR CRs should exist in %s", ns)
    for _, vr := range vrList.Items {
        Expect(string(vr.Spec.ReplicationState)).To(Equal(expectedState),
            "VR %s should be %s", vr.Name, expectedState)
    }
}
```

### Critical: VM Running State Detection

```go
func assertVMRunState(ctx context.Context, cl client.Client, ns string, expectRunning bool) {
    var vmList kubevirtv1.VirtualMachineList
    Expect(cl.List(ctx, &vmList,
        client.InNamespace(ns),
        client.MatchingLabels{"soteria.io/drplan": "fedora-app"},
    )).To(Succeed())

    for _, vm := range vmList.Items {
        if expectRunning {
            Expect(ptr.Deref(vm.Spec.RunStrategy, "")).To(Equal(
                kubevirtv1.VirtualMachineRunStrategyAlways))
            // Verify VMI exists and is Running
            var vmi kubevirtv1.VirtualMachineInstance
            Expect(cl.Get(ctx, client.ObjectKey{
                Name: vm.Name, Namespace: ns,
            }, &vmi)).To(Succeed())
            Expect(vmi.Status.Phase).To(Equal(kubevirtv1.Running))
        } else {
            Expect(ptr.Deref(vm.Spec.RunStrategy, "")).To(Equal(
                kubevirtv1.VirtualMachineRunStrategyHalted))
        }
    }
}
```

### Ginkgo Test Structure

```go
var _ = Describe("Full Lifecycle E2E Test", Ordered, func() {
    It("pre-test sanity: initial state is correct", func() { ... })

    It("pre-test sanity: ShadowPV resources exist with correct entries", func() { ... })

    It("T1: planned migration east→west with resync guard", func() {
        execName := "t1-planned-migration"
        cl := activeClient(eastClient, westClient, "east")
        createDRExecution(ctx, cl, execName, "fedora-app",
            soteriav1alpha1.ExecutionModePlannedMigration)

        By("observing ResyncPending condition lifecycle")
        observeResyncPending(ctx, cl, execName, transitionTimeout)

        By("waiting for execution to succeed")
        waitForExecResult(ctx, cl, execName,
            soteriav1alpha1.ExecutionResultSucceeded, transitionTimeout)

        By("verifying post-failover state")
        assertPlanState(ctx, eastClient, "fedora-app", "FailedOver", "west")
        assertVRState(ctx, westClient, testNamespace, "primary")
        assertVRState(ctx, eastClient, testNamespace, "secondary")
        assertVMRunState(ctx, westClient, testNamespace, true)
        assertVMRunState(ctx, eastClient, testNamespace, false)
        assertConditionsHealthy(ctx, eastClient, "fedora-app")

        By("verifying ShadowPV entries updated")
        assertShadowPVEntries(ctx, eastClient, "fedora-app", "west")
    })

    It("T2: reprotect (FailedOver → DRedSteadyState)", func() { ... })
    It("T3: planned migration west→east with resync guard", func() { ... })
    It("T4: reprotect (FailedBack → SteadyState)", func() { ... })
    It("post-lifecycle: system returns to initial state", func() { ... })
})
```

Use `Ordered` to ensure sequential execution — each transition depends on the previous one.

### Polling Strategy

Use Gomega `Eventually` with 5s polling interval and configurable timeout:

```go
Eventually(func(g Gomega) {
    var exec soteriav1alpha1.DRExecution
    g.Expect(cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())
    g.Expect(exec.Status.Result).To(Equal(soteriav1alpha1.ExecutionResultSucceeded))
}).WithTimeout(transitionTimeout).WithPolling(5 * time.Second).Should(Succeed())
```

### UAT-13 Lessons Learned — All Fixed, Must Still Hold

1. **Reprotect double-flip race (UAT-13.001/002):** Fixed in Epic 13. Story 15-3 further simplifies reprotect — verify both reprotects complete Succeeded
2. **Health count (UAT-13.003):** Fixed — verify health count matches total VG count
3. **Duration field (UAT-13.004):** Fixed — verify Duration populated for all executions
4. **Checkpoint conflicts (UAT-13.005):** Fixed by Story 13.7 — scan logs for zero conflicts
5. **Immutability violations (UAT-13.006):** Fixed by Story 13.7 — scan logs for zero violations

### Log Capture Strategy

After each transition, capture controller-manager logs and scan for:
- `"the object has been modified"` → checkpoint conflicts (should be zero)
- `"is immutable after completion"` → immutability violations (should be zero)
- `ERROR` level entries (exclude expected ScyllaDB connection retries)

```go
func captureControllerLogs(ctx context.Context, cs *kubernetes.Clientset, ns string) string {
    pods, _ := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
        LabelSelector: "control-plane=controller-manager",
    })
    if len(pods.Items) == 0 {
        return ""
    }
    logs, _ := cs.CoreV1().Pods(ns).GetLogs(pods.Items[0].Name,
        &corev1.PodLogOptions{Container: "manager"}).Stream(ctx)
    defer logs.Close()
    buf := new(bytes.Buffer)
    io.Copy(buf, logs)
    return buf.String()
}
```

### Dependencies

| Dependency | Story | What's Needed |
|------------|-------|---------------|
| Minikube KVM2 clusters + Cilium | 14.1 | Both `east` and `west` clusters running with Cluster Mesh |
| Rook-Ceph | 14.2 | StorageClass `rook-ceph-block`, VolumeReplicationClass `rook-ceph-rbd-vrc`, CSI Addons running |
| KubeVirt + CDI | 14.3 | KubeVirt Deployed on both clusters with nested virtualization |
| Fedora image cached | 14.4 | Container disk images pre-pulled on all nodes |
| ScyllaDB | 14.5 | Cross-DC ScyllaDB cluster running on both clusters |
| Soteria deployed | 15.8 | Soteria operator deployed on both clusters with `--site-name=east/west` and all Epic 15 code changes built in |
| ResyncVolume driver method | 15.1 | StorageProvider `ResyncVolume` method available in CSI extension |
| Resync guard | 15.2 | Planned failover Step 0 calls ResyncVolume, ResyncPending condition |
| Reprotect simplification | 15.3 | Reprotect uses GetReplicationStatus + conditional ResyncVolume |
| ShadowPV CRD | 15.4 | ShadowPV type registered in aggregated API server |
| ShadowPV publisher | 15.5 | Publisher creates ShadowPV resources from VR/VGR PVC→PV chain |
| ShadowPV consumer | 15.6 | Consumer creates PVs from remote ShadowPV entries with pool-ID rewrite |

### Existing Test Patterns to Follow

**From `test/integration/controller/suite_test.go`:**
- `waitForCondition`, `waitForExecResult`, `waitForPlanPhase` polling helpers
- `newTestDRPlan` factory function
- Adapt these to accept a `client.Client` parameter (two-client architecture)

**From `test/e2e/e2e_suite_test.go`:**
- Ginkgo `BeforeSuite`/`AfterSuite` with `RegisterFailHandler(Fail)`
- Build tag gating (`//go:build e2e`)

### Project Structure Notes

```
test/multisite/
├── suite_test.go        # Ginkgo bootstrap, env var config, two-client setup
├── setup_test.go        # BeforeSuite (deploy scenario), AfterSuite (cleanup)
├── lifecycle_test.go    # 4-transition lifecycle test with Epic 15 assertions
└── helpers_test.go      # Shared polling/assertion/log-capture helpers
```

All files use `package multisite_test` and `//go:build multisite` tag.

### Scheme Registration

The test client scheme must include:
- `k8s.io/client-go/kubernetes/scheme` — core types
- `soteria.io/v1alpha1` — DRPlan, DRExecution, ShadowPV
- `kubevirt.io/api/core/v1` — VirtualMachine, VirtualMachineInstance
- `github.com/csi-addons/kubernetes-csi-addons/api/replication.storage.openshift.io/v1alpha1` — VolumeReplication, VolumeGroupReplication

### Real Storage Timing Expectations

| Operation | Noop Timing | Real Ceph Timing | Notes |
|-----------|-------------|-------------------|-------|
| VR state transition | instant | 5-30s | Ceph mirror daemon response |
| ResyncVolume completion | instant | 5-60s | Small test data = fast |
| VM start after SetSource | 5-10s | 10-30s | Container disk boot |
| Health convergence | instant | 30-120s | Ceph mirror establishment |
| Full planned migration | 55-70s | 3-6m | Includes resync guard |
| Full reprotect | 40s | 1-3m | Simplified handler |

### Previous Story Intelligence

**From Story 15-8 (Soteria Operator Deployment):**
- Deployment via `hack/multisite/deploy-soteria.sh`
- Image built locally + `minikube image load`
- `--site-name=east/west` and `--scylladb-local-dc=east/west` configured per cluster
- APIService `v1alpha1.soteria.io` Available on both clusters
- Cross-DC DRPlan replication smoke test validates ScyllaDB connectivity
- Console plugin excluded (no OCP Console)

**From Epic 14 (Infrastructure):**
- Cluster profiles/contexts: `east` and `west`
- 4 nodes per cluster (1 CP + 3 workers)
- `rook-ceph-block` StorageClass with RBD mirroring
- VolumeReplicationClass `rook-ceph-rbd-vrc` with snapshot-mode, 1m schedule
- KubeVirt with nested virtualization (hardware KVM)
- Fedora container disk pre-cached on all nodes
- ScyllaDB cross-DC with mTLS, Cilium global services

**From Epic 13 (VolumeReplication Lifecycle):**
- Story 13.7 peer-site reconcile guard eliminates checkpoint conflicts and immutability violations
- DRPlan creates/updates VR/VGR with site-aware replication state
- DRExecution mutates VR/VGR during transitions
- All UAT-13 issues resolved before Epic 15

### References

- [Source: epics.md#Epic 15, Story 15.9]
- [Source: _bmad-output/implementation-artifacts/14-7-test-scenario-setup-full-lifecycle-integration-test.md — Original story spec]
- [Source: _bmad-output/implementation-artifacts/15-8-soteria-operator-deployment.md — Deployment story]
- [Source: _bmad-output/implementation-artifacts/15-1-resyncvolume-driver-method-csi-extension.md — ResyncVolume method]
- [Source: _bmad-output/implementation-artifacts/15-2-planned-failover-resync-guard-event-driven.md — Resync guard]
- [Source: _bmad-output/implementation-artifacts/15-3-reprotect-handler-simplification-real-storage.md — Reprotect simplification]
- [Source: _bmad-output/implementation-artifacts/15-5-shadowpv-publisher-controller.md — ShadowPV publisher]
- [Source: _bmad-output/implementation-artifacts/15-6-shadowpv-consumer-controller-pv-creation-pool-id-rewrite.md — ShadowPV consumer]
- [Source: hack/stretched-local-test.sh lines 430-514] — VM creation pattern, wave structure
- [Source: user-acceptance-test-epic-13.md] — UAT-13 lifecycle test pattern, timing expectations
- [Source: test/integration/controller/suite_test.go] — polling helpers, test plan factory
- [Source: test/e2e/e2e_suite_test.go] — Ginkgo suite scaffold, build tag pattern
- [Source: pkg/apis/soteria.io/v1alpha1/types.go] — DRPlanSpec, DRExecutionSpec, ShadowPVSpec, ExecutionResult, ExecutionPhase
- [Source: project-context.md] — DRPlan 8-phase lifecycle, site-aware reconciliation, ShadowPV architecture

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
