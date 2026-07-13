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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

var _ = Describe("DRPlan Convergence via ShadowPV Pipeline", Ordered, Serial, func() {
	var (
		ctx      context.Context
		scenario *lifecycleScenario
	)

	BeforeAll(func() {
		ctx = context.Background()
		scenario = &lifecycleScenario{
			name:                   "convergence",
			volumeReplicationClass: "rook-ceph-rbd-vrc-snapshot",
			storageClass:           "rook-ceph-block",
			vmPrefix:               "conv-",
		}
	})

	AfterAll(func() {
		teardownScenario(ctx, scenario)
	})

	It("deploys east PVCs + VMs and west VMs (no PVCs)", func() {
		deployScenario(ctx, scenario)
	})

	It("DRPlan initially has DisksConsistent=False", func() {
		Eventually(func(g Gomega) {
			var plan soteriav1alpha1.DRPlan
			g.Expect(eastClient.Get(ctx, client.ObjectKey{
				Name: scenario.planName(),
			}, &plan)).To(Succeed())

			cond := meta.FindStatusCondition(plan.Status.Conditions, "DisksConsistent")
			g.Expect(cond).NotTo(BeNil(), "DisksConsistent condition should be present")
			g.Expect(cond.Status).To(Equal(metav1.ConditionFalse),
				"DisksConsistent should initially be False (west has no PVCs yet)")
		}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
	})

	It("ShadowPV publisher creates ShadowPV resources", func() {
		waitForShadowPVResources(ctx, eastClient, scenario.planName())
	})

	It("ShadowPV consumer creates PVs on west", func() {
		pvNames := waitForShadowPVConsumerPVs(ctx, eastClient, westClient, scenario.planName())
		Expect(pvNames).NotTo(BeEmpty())
	})

	It("west PVCs are created bound to ShadowPV-provisioned PVs", func() {
		pvNames := waitForShadowPVConsumerPVs(ctx, eastClient, westClient, scenario.planName())
		createWestPVCsFromShadowPVs(ctx, westClient, scenario.vmPrefix, pvNames)
	})

	It("DRPlan converges to all conditions healthy", func() {
		waitForDRPlanHealthy(ctx, eastClient, scenario.planName())
		assertConditionsHealthy(ctx, eastClient, scenario.planName())
	})

	It("east VMs are Running and west VMs are Halted", func() {
		assertVMRunState(ctx, eastClient, testNamespace, scenario.planName(), true)
		assertVMRunState(ctx, westClient, testNamespace, scenario.planName(), false)
	})

	It("VR CRs: east=primary, west=secondary", func() {
		assertVRState(ctx, eastClient, testNamespace, scenario.planName(), "primary")
		assertVRState(ctx, westClient, testNamespace, scenario.planName(), "secondary")
	})

	It("ShadowPV entries reference the east cluster", func() {
		assertShadowPVEntries(ctx, eastClient, scenario.planName(), "east")
	})

	It("controller logs have no conflicts or violations", func() {
		scanControllerLogsOnBothSites(ctx)
	})
})
