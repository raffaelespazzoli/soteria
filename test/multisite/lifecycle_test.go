//go:build multisite

/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package multisite_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

var lifecycleScenarios = []lifecycleScenario{
	{
		name:                   "planned-snapshot",
		failoverMode:           soteriav1alpha1.ExecutionModePlannedMigration,
		volumeReplicationClass: "rook-ceph-rbd-vrc-snapshot",
		storageClass:           "rook-ceph-block",
		simulateDisaster:       false,
		vmPrefix:               "ps-",
	},
	{
		name:                   "planned-journal",
		failoverMode:           soteriav1alpha1.ExecutionModePlannedMigration,
		volumeReplicationClass: "rook-ceph-rbd-vrc-journal",
		storageClass:           "rook-ceph-block-journal",
		simulateDisaster:       false,
		vmPrefix:               "pj-",
	},
	{
		name:                   "disaster-snapshot",
		failoverMode:           soteriav1alpha1.ExecutionModeDisaster,
		volumeReplicationClass: "rook-ceph-rbd-vrc-snapshot",
		storageClass:           "rook-ceph-block",
		simulateDisaster:       true,
		vmPrefix:               "ds-",
	},
	{
		name:                   "disaster-journal",
		failoverMode:           soteriav1alpha1.ExecutionModeDisaster,
		volumeReplicationClass: "rook-ceph-rbd-vrc-journal",
		storageClass:           "rook-ceph-block-journal",
		simulateDisaster:       true,
		vmPrefix:               "dj-",
	},
}

var _ = Describe("Lifecycle Matrix", Serial, func() {
	for i := range lifecycleScenarios {
		s := &lifecycleScenarios[i]

		Describe(s.name, Ordered, func() {
			var ctx context.Context

			BeforeAll(func() {
				ctx = context.Background()
				By("deploying scenario " + s.name)
				deployScenario(ctx, s)
				By("converging scenario " + s.name)
				convergeScenario(ctx, s)
			})

			AfterAll(func() {
				teardownScenario(ctx, s)
			})

			if s.simulateDisaster {
				runDisasterLifecycle(s)
			} else {
				runPlannedLifecycle(s)
			}
		})
	}
})

func runPlannedLifecycle(s *lifecycleScenario) {
	planName := s.planName()

	It("T1: planned migration east→west", func() {
		ctx := context.Background()
		execName := s.vmPrefix + "t1-migrate"
		createDRExecution(ctx, eastClient, execName, planName,
			soteriav1alpha1.ExecutionModePlannedMigration)

		observeResyncPending(ctx, eastClient, execName, transitionTimeout)
		waitForExecResult(ctx, eastClient, execName,
			soteriav1alpha1.ExecutionResultSucceeded, transitionTimeout)

		assertPerTransition(ctx, eastClient, execName, planName)
		assertPlanState(ctx, eastClient, planName,
			soteriav1alpha1.PhaseFailedOver, "west")
		assertVMRunState(ctx, westClient, testNamespace, planName, true)
		assertVMRunState(ctx, eastClient, testNamespace, planName, false)
		assertRealStorageVR(ctx, westClient, testNamespace, planName)
		assertShadowPVEntries(ctx, eastClient, planName, "west")
	})

	It("T2: reprotect on west", func() {
		ctx := context.Background()
		execName := s.vmPrefix + "t2-reprotect"
		createDRExecution(ctx, activeClient(eastClient, westClient, "west"),
			execName, planName, soteriav1alpha1.ExecutionModeReprotect)

		waitForExecResult(ctx, activeClient(eastClient, westClient, "west"),
			execName, soteriav1alpha1.ExecutionResultSucceeded, transitionTimeout)

		assertPerTransition(ctx, activeClient(eastClient, westClient, "west"),
			execName, planName)
		assertPlanState(ctx, activeClient(eastClient, westClient, "west"),
			planName, soteriav1alpha1.PhaseDRedSteadyState, "west")
		assertConditionsHealthy(ctx, activeClient(eastClient, westClient, "west"),
			planName)
		assertRealStorageVR(ctx, westClient, testNamespace, planName)
	})

	It("T3: planned migration west→east", func() {
		ctx := context.Background()
		execName := s.vmPrefix + "t3-migrate"
		cl := activeClient(eastClient, westClient, "west")
		createDRExecution(ctx, cl, execName, planName,
			soteriav1alpha1.ExecutionModePlannedMigration)

		observeResyncPending(ctx, cl, execName, transitionTimeout)
		waitForExecResult(ctx, cl, execName,
			soteriav1alpha1.ExecutionResultSucceeded, transitionTimeout)

		assertPerTransition(ctx, cl, execName, planName)
		assertPlanState(ctx, cl, planName,
			soteriav1alpha1.PhaseFailedBack, "east")
		assertVMRunState(ctx, eastClient, testNamespace, planName, true)
		assertVMRunState(ctx, westClient, testNamespace, planName, false)
		assertRealStorageVR(ctx, eastClient, testNamespace, planName)
		assertShadowPVEntries(ctx, eastClient, planName, "east")
	})

	It("T4: reprotect on east", func() {
		ctx := context.Background()
		execName := s.vmPrefix + "t4-reprotect"
		createDRExecution(ctx, eastClient, execName, planName,
			soteriav1alpha1.ExecutionModeReprotect)

		waitForExecResult(ctx, eastClient, execName,
			soteriav1alpha1.ExecutionResultSucceeded, transitionTimeout)

		assertPerTransition(ctx, eastClient, execName, planName)
		assertPlanState(ctx, eastClient, planName,
			soteriav1alpha1.PhaseSteadyState, "east")
		assertConditionsHealthy(ctx, eastClient, planName)
		assertRealStorageVR(ctx, eastClient, testNamespace, planName)
	})

	It("ShadowPV entries consistent after lifecycle", func() {
		ctx := context.Background()
		assertShadowPVEntries(ctx, eastClient, planName, "east")
	})

	It("controller logs have no conflicts or violations", func() {
		ctx := context.Background()
		scanControllerLogsOnBothSites(ctx)
	})
}

func runDisasterLifecycle(s *lifecycleScenario) {
	planName := s.planName()

	It("minikube stop east (simulate disaster)", func() {
		minikubeStop(eastMinikubeProfile)
	})

	It("west API server remains responsive", func() {
		ctx := context.Background()
		waitForAPIServer(ctx, westClient)
	})

	It("disaster execution on west succeeds", func() {
		ctx := context.Background()
		execName := s.vmPrefix + "disaster-exec"
		createDRExecution(ctx, westClient, execName, planName,
			soteriav1alpha1.ExecutionModeDisaster)

		waitForExecResult(ctx, westClient, execName,
			soteriav1alpha1.ExecutionResultSucceeded, disasterRecoveryTimeout)

		assertPerTransition(ctx, westClient, execName, planName)
		assertPlanState(ctx, westClient, planName,
			soteriav1alpha1.PhaseFailedOver, "west")
		assertVMRunState(ctx, westClient, testNamespace, planName, true)
		assertRealStorageVR(ctx, westClient, testNamespace, planName)
		assertShadowPVEntries(ctx, westClient, planName, "west")
	})

	It("minikube start east (recover source)", func() {
		minikubeStart(eastMinikubeProfile)
	})

	It("east infrastructure recovers", func() {
		ctx := context.Background()
		healClusterAfterRestart(ctx, eastMinikubeProfile)
	})

	It("reprotect after disaster recovery", func() {
		ctx := context.Background()
		execName := s.vmPrefix + "reprotect-after-dr"
		createDRExecution(ctx, westClient, execName, planName,
			soteriav1alpha1.ExecutionModeReprotect)

		waitForExecResult(ctx, westClient, execName,
			soteriav1alpha1.ExecutionResultSucceeded, transitionTimeout)

		assertPerTransition(ctx, westClient, execName, planName)
		assertPlanState(ctx, westClient, planName,
			soteriav1alpha1.PhaseDRedSteadyState, "west")
		assertConditionsHealthy(ctx, westClient, planName)
	})

	It("ShadowPV entries updated after disaster lifecycle", func() {
		ctx := context.Background()
		assertShadowPVEntries(ctx, westClient, planName, "west")
	})

	It("controller logs have no conflicts or violations after recovery", func() {
		ctx := context.Background()
		scanControllerLogsOnBothSites(ctx)
	})
}
