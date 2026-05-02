//go:build integration

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

package controller_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/controller/drplan"
	"github.com/soteria-project/soteria/pkg/engine"
)

const (
	testTimeout = 30 * time.Second
)

func createNamespace(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := testClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace %s: %v", name, err)
	}
}

func createVM(t *testing.T, ctx context.Context, name, namespace string, labels map[string]string) *kubevirtv1.VirtualMachine {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{},
		},
	}
	if err := testClient.Create(ctx, vm); err != nil {
		t.Fatalf("Failed to create VM %s/%s: %v", namespace, name, err)
	}
	return vm
}

func createDRPlan(t *testing.T, ctx context.Context, name string) *soteriav1alpha1.DRPlan {
	t.Helper()
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            "dc-west",
			SecondarySite:          "dc-east",
			MaxConcurrentFailovers: 5,
		},
	}
	if err := testClient.Create(ctx, plan); err != nil {
		t.Fatalf("Failed to create DRPlan %s: %v", name, err)
	}
	return plan
}

func TestDRPlanReconciler_DiscoverVMs_WavesPopulated(t *testing.T) {
	ctx := context.Background()
	ns := "test-discover"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-w1-a", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-discover", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-w1-b", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-discover", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-w2-a", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-discover", "soteria.io/wave": "2"})

	createDRPlan(t, ctx, "plan-discover")

	plan, err := waitForCondition(ctx, "plan-discover", "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Status.DiscoveredVMCount != 3 {
		t.Errorf("DiscoveredVMCount = %d, want 3", plan.Status.DiscoveredVMCount)
	}
	if len(plan.Status.Waves) != 2 {
		t.Fatalf("len(Waves) = %d, want 2", len(plan.Status.Waves))
	}
	if plan.Status.Waves[0].WaveKey != "1" || plan.Status.Waves[1].WaveKey != "2" {
		t.Errorf("WaveKeys = [%q, %q], want [\"1\", \"2\"]",
			plan.Status.Waves[0].WaveKey, plan.Status.Waves[1].WaveKey)
	}
}

func TestDRPlanReconciler_NewVMAdded_WatchTriggersReconcile(t *testing.T) {
	ctx := context.Background()
	ns := "test-vm-add"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-initial", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-add", "soteria.io/wave": "1"})
	createDRPlan(t, ctx, "plan-add")

	_, err := waitForVMCount(ctx, "plan-add", "", 1, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	createVM(t, ctx, "vm-new", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-add", "soteria.io/wave": "1"})

	plan, err := waitForVMCount(ctx, "plan-add", "", 2, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Status.Waves) != 1 {
		t.Errorf("len(Waves) = %d, want 1", len(plan.Status.Waves))
	}
}

func TestDRPlanReconciler_WaveValueChanged_WatchTriggersReconcile(t *testing.T) {
	ctx := context.Background()
	ns := "test-wave-change"
	createNamespace(t, ctx, ns)

	vm := createVM(t, ctx, "vm-move", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-wave", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-stay", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-wave", "soteria.io/wave": "1"})
	createDRPlan(t, ctx, "plan-wave")

	plan, err := waitForCondition(ctx, "plan-wave", "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Status.Waves) != 1 {
		t.Fatalf("Initial waves count = %d, want 1", len(plan.Status.Waves))
	}

	vm.Labels["soteria.io/wave"] = "2"
	if err := testClient.Update(ctx, vm); err != nil {
		t.Fatalf("Failed to update VM labels: %v", err)
	}

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		var updated soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: "plan-wave"}, &updated); err == nil {
			if len(updated.Status.Waves) == 2 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Timed out waiting for VM to move to wave 2")
}

func TestDRPlanReconciler_VMDeleted_WatchTriggersReconcile(t *testing.T) {
	ctx := context.Background()
	ns := "test-vm-delete"
	createNamespace(t, ctx, ns)

	vm := createVM(t, ctx, "vm-delete-me", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-delete", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-keep", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-delete", "soteria.io/wave": "1"})
	createDRPlan(t, ctx, "plan-delete")

	_, err := waitForVMCount(ctx, "plan-delete", "", 2, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if err := testClient.Delete(ctx, vm); err != nil {
		t.Fatalf("Failed to delete VM: %v", err)
	}

	_, err = waitForVMCount(ctx, "plan-delete", "", 1, testTimeout)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDRPlanReconciler_ReadyCondition_ReflectsDiscovery(t *testing.T) {
	ctx := context.Background()
	ns := "test-ready-cond"
	createNamespace(t, ctx, ns)

	createDRPlan(t, ctx, "plan-empty")

	plan, err := waitForCondition(ctx, "plan-empty", "", "Ready", metav1.ConditionFalse, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range plan.Status.Conditions {
		if c.Type == "Ready" {
			if c.Reason != "NoVMsDiscovered" {
				t.Errorf("Ready.Reason = %q, want NoVMsDiscovered", c.Reason)
			}
			return
		}
	}
	t.Error("Ready condition not found")
}

func TestDRPlanReconciler_SiteAware_BothSitesWriteDiscovery(t *testing.T) {
	ctx := context.Background()
	ns := "test-site-both"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-site-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-site-both", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-site-2", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-site-both", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-site-3", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-site-both", "soteria.io/wave": "2"})

	createDRPlan(t, ctx, "plan-site-both")

	// Wait for default reconciler (no LocalSite) to populate waves.
	_, err := waitForCondition(ctx, "plan-site-both", "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Reconcile as active site (dc-west = PrimarySite = ActiveSite).
	activeReconciler := &drplan.DRPlanReconciler{
		Client:          testClient,
		Scheme:          testScheme,
		VMDiscoverer:    engine.NewTypedVMDiscoverer(testClient),
		NamespaceLookup: &engine.DefaultNamespaceLookup{Client: testClientset.CoreV1()},
		Recorder:        nil,
		LocalSite:       "dc-west",
	}
	_, err = activeReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: client.ObjectKey{Name: "plan-site-both"},
	})
	if err != nil {
		t.Fatalf("Active site Reconcile() error: %v", err)
	}

	// Wait for PrimarySiteDiscovery to appear in the cache.
	plan, err := waitForSiteDiscovery(ctx, "plan-site-both", "primary", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Reconcile as passive site (dc-east = SecondarySite).
	passiveReconciler := &drplan.DRPlanReconciler{
		Client:          testClient,
		Scheme:          testScheme,
		VMDiscoverer:    engine.NewTypedVMDiscoverer(testClient),
		NamespaceLookup: &engine.DefaultNamespaceLookup{Client: testClientset.CoreV1()},
		Recorder:        nil,
		LocalSite:       "dc-east",
	}
	_, err = passiveReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: client.ObjectKey{Name: "plan-site-both"},
	})
	if err != nil {
		t.Fatalf("Passive site Reconcile() error: %v", err)
	}

	// Wait for SecondarySiteDiscovery to appear in the cache.
	plan, err = waitForSiteDiscovery(ctx, "plan-site-both", "secondary", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Both SiteDiscovery fields populated.
	if plan.Status.PrimarySiteDiscovery == nil {
		t.Fatal("PrimarySiteDiscovery should be populated by active site")
	}
	if plan.Status.SecondarySiteDiscovery == nil {
		t.Fatal("SecondarySiteDiscovery should be populated by passive site")
	}
	if plan.Status.PrimarySiteDiscovery.DiscoveredVMCount != 3 {
		t.Errorf("PrimarySiteDiscovery.DiscoveredVMCount = %d, want 3",
			plan.Status.PrimarySiteDiscovery.DiscoveredVMCount)
	}
	if plan.Status.SecondarySiteDiscovery.DiscoveredVMCount != 3 {
		t.Errorf("SecondarySiteDiscovery.DiscoveredVMCount = %d, want 3",
			plan.Status.SecondarySiteDiscovery.DiscoveredVMCount)
	}

	// Waves only populated by active site reconcile (verified by default reconciler earlier).
	if len(plan.Status.Waves) == 0 {
		t.Error("Waves should be populated by active-site reconcile")
	}
}

func TestDRPlanReconciler_PassiveSite_DiscoversVMsLocally(t *testing.T) {
	ctx := context.Background()
	ns := "test-site-passive"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-passive-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-site-passive", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-passive-2", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-site-passive", "soteria.io/wave": "2"})

	plan := createDRPlan(t, ctx, "plan-site-passive")

	// Wait for default reconciler to process (sets Ready condition).
	_, err := waitForCondition(ctx, "plan-site-passive", "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Reconcile as passive site (dc-east = SecondarySite, plan ActiveSite defaults to PrimarySite=dc-west).
	passiveReconciler := &drplan.DRPlanReconciler{
		Client:          testClient,
		Scheme:          testScheme,
		VMDiscoverer:    engine.NewTypedVMDiscoverer(testClient),
		NamespaceLookup: &engine.DefaultNamespaceLookup{Client: testClientset.CoreV1()},
		Recorder:        nil,
		LocalSite:       "dc-east",
	}
	result, err := passiveReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: client.ObjectKey{Name: plan.Name},
	})
	if err != nil {
		t.Fatalf("Passive Reconcile() error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %v, want 30s", result.RequeueAfter)
	}

	// Wait for SecondarySiteDiscovery to appear in the cache.
	updated, err := waitForSiteDiscovery(ctx, plan.Name, "secondary", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	if updated.Status.SecondarySiteDiscovery.DiscoveredVMCount != 2 {
		t.Errorf("SecondarySiteDiscovery.DiscoveredVMCount = %d, want 2",
			updated.Status.SecondarySiteDiscovery.DiscoveredVMCount)
	}
	if updated.Status.SecondarySiteDiscovery.LastDiscoveryTime.IsZero() {
		t.Error("LastDiscoveryTime should not be zero")
	}

	// Passive site must not overwrite or form waves — verify waves remain
	// exactly as the prior active-site reconcile left them (2 waves from 2 VMs).
	if len(updated.Status.Waves) != 2 {
		t.Errorf("Waves should remain unchanged from active-site reconcile, got %d want 2",
			len(updated.Status.Waves))
	}

	// PrimarySiteDiscovery must NOT be set by the passive reconcile.
	if updated.Status.PrimarySiteDiscovery != nil {
		t.Error("PrimarySiteDiscovery should not be populated by passive-site reconcile")
	}
}

func TestDRPlanReconciler_50VMs_CompletesWithin10s(t *testing.T) {
	ctx := context.Background()
	ns := "test-perf"
	createNamespace(t, ctx, ns)

	for i := range 50 {
		wave := fmt.Sprintf("%d", (i%5)+1)
		createVM(t, ctx, fmt.Sprintf("vm-perf-%03d", i), ns,
			map[string]string{soteriav1alpha1.DRPlanLabel: "plan-perf", "soteria.io/wave": wave})
	}

	start := time.Now()
	createDRPlan(t, ctx, "plan-perf")

	plan, err := waitForVMCount(ctx, "plan-perf", "", 50, testTimeout)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}

	if elapsed > 10*time.Second {
		t.Errorf("Discovery took %v, exceeds NFR10 bound of 10s", elapsed)
	}
	t.Logf("50 VM discovery completed in %v", elapsed)

	if len(plan.Status.Waves) != 5 {
		t.Errorf("len(Waves) = %d, want 5", len(plan.Status.Waves))
	}
}

func TestDRPlanReconciler_CrossSiteAgreement_BlocksOnMismatch(t *testing.T) {
	ctx := context.Background()
	ns := "test-xsite-block"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-xb-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-xsite-block", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-xb-2", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-xsite-block", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-xb-3", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-xsite-block", "soteria.io/wave": "2"})

	plan := createDRPlan(t, ctx, "plan-xsite-block")

	// Wait for default reconciler (no LocalSite) to populate waves.
	_, err := waitForCondition(ctx, plan.Name, "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Pre-populate SiteDiscovery fields with a mismatch:
	// Primary has 3 VMs, secondary only has 2 (missing vm-xb-3).
	patchSiteDiscoveryWithRetry(t, ctx, plan.Name,
		&soteriav1alpha1.SiteDiscovery{
			VMs: []soteriav1alpha1.DiscoveredVM{
				{Name: "vm-xb-1", Namespace: ns},
				{Name: "vm-xb-2", Namespace: ns},
				{Name: "vm-xb-3", Namespace: ns},
			},
			DiscoveredVMCount: 3,
			LastDiscoveryTime: metav1.Now(),
		},
		&soteriav1alpha1.SiteDiscovery{
			VMs: []soteriav1alpha1.DiscoveredVM{
				{Name: "vm-xb-1", Namespace: ns},
				{Name: "vm-xb-2", Namespace: ns},
			},
			DiscoveredVMCount: 2,
			LastDiscoveryTime: metav1.Now(),
		},
	)

	// Wait for cache to reflect both SiteDiscovery fields before reconciling.
	_, err = waitForSiteDiscovery(ctx, plan.Name, "primary", testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	_, err = waitForSiteDiscovery(ctx, plan.Name, "secondary", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Reconcile as active site — should detect mismatch and block.
	activeReconciler := &drplan.DRPlanReconciler{
		Client:          testClient,
		Scheme:          testScheme,
		VMDiscoverer:    engine.NewTypedVMDiscoverer(testClient),
		NamespaceLookup: &engine.DefaultNamespaceLookup{Client: testClientset.CoreV1()},
		Recorder:        nil,
		LocalSite:       "dc-west",
	}
	_, err = activeReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: client.ObjectKey{Name: plan.Name},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	// Wait for SitesInSync=False to propagate through the cache.
	updated, err := waitForConditionReason(ctx, plan.Name, "", "SitesInSync", "VMsMismatch", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Verify Ready=False.
	for _, c := range updated.Status.Conditions {
		if c.Type == "Ready" {
			if c.Status != metav1.ConditionFalse {
				t.Errorf("Ready.Status = %v, want False", c.Status)
			}
			if c.Reason != "SitesOutOfSync" {
				t.Errorf("Ready.Reason = %q, want SitesOutOfSync", c.Reason)
			}
			break
		}
	}

	// Verify waves are cleared.
	if len(updated.Status.Waves) != 0 {
		t.Errorf("Waves should be cleared on mismatch, got %d", len(updated.Status.Waves))
	}
}

func TestDRPlanReconciler_CrossSiteAgreement_ProceedsOnMatch(t *testing.T) {
	ctx := context.Background()
	ns := "test-xsite-match"
	createNamespace(t, ctx, ns)

	createVM(t, ctx, "vm-xm-1", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-xsite-match", "soteria.io/wave": "1"})
	createVM(t, ctx, "vm-xm-2", ns, map[string]string{soteriav1alpha1.DRPlanLabel: "plan-xsite-match", "soteria.io/wave": "1"})

	plan := createDRPlan(t, ctx, "plan-xsite-match")

	// Wait for default reconciler to populate waves.
	_, err := waitForCondition(ctx, plan.Name, "", "Ready", metav1.ConditionTrue, testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Pre-populate both SiteDiscovery fields with matching VM sets.
	matchingVMs := []soteriav1alpha1.DiscoveredVM{
		{Name: "vm-xm-1", Namespace: ns},
		{Name: "vm-xm-2", Namespace: ns},
	}
	discovery := &soteriav1alpha1.SiteDiscovery{
		VMs:               matchingVMs,
		DiscoveredVMCount: 2,
		LastDiscoveryTime: metav1.Now(),
	}
	patchSiteDiscoveryWithRetry(t, ctx, plan.Name, discovery, discovery)

	// Wait for cache to reflect both SiteDiscovery fields.
	_, err = waitForSiteDiscovery(ctx, plan.Name, "primary", testTimeout)
	if err != nil {
		t.Fatal(err)
	}
	_, err = waitForSiteDiscovery(ctx, plan.Name, "secondary", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Reconcile as active site — should detect agreement and proceed normally.
	activeReconciler := &drplan.DRPlanReconciler{
		Client:          testClient,
		Scheme:          testScheme,
		VMDiscoverer:    engine.NewTypedVMDiscoverer(testClient),
		NamespaceLookup: &engine.DefaultNamespaceLookup{Client: testClientset.CoreV1()},
		Recorder:        nil,
		LocalSite:       "dc-west",
	}
	_, err = activeReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: client.ObjectKey{Name: plan.Name},
	})
	if err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	// Wait for SitesInSync=True.
	updated, err := waitForConditionReason(ctx, plan.Name, "", "SitesInSync", "VMsAgreed", testTimeout)
	if err != nil {
		t.Fatal(err)
	}

	// Verify Ready=True.
	for _, c := range updated.Status.Conditions {
		if c.Type == "Ready" {
			if c.Status != metav1.ConditionTrue {
				t.Errorf("Ready.Status = %v, want True", c.Status)
			}
			break
		}
	}

	// Verify waves are populated.
	if len(updated.Status.Waves) == 0 {
		t.Error("Waves should be populated when sites agree")
	}
}
