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
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// ---------------------------------------------------------------------------
// Stale test resource cleanup guard
// ---------------------------------------------------------------------------

// cleanupStaleTestResources removes left-over resources from a previous test
// run under normal (non-crash) conditions. It is called in BeforeSuite after
// verifyInfrastructureHealth and before namespace creation.
//
// Cleanup order matters:
//  1. Cluster-scoped DR resources (DRPlans, DRExecutions, ShadowPVs)
//  2. Stuck test namespaces (strip finalizers on blocking sub-resources)
//  3. Stale VolumeAttachments referencing deleted PVs
//  4. Orphaned PersistentVolumes (Released/Failed or test-labeled)
//  5. Orphaned RBD mirror images (not referenced by any live PV)
func cleanupStaleTestResources() {
	ctx := context.Background()
	cleaned := false

	for _, pair := range []struct {
		name string
		cl   client.Client
	}{
		{"east", eastClient},
		{"west", westClient},
	} {
		if cleanupClusterScopedDRResources(ctx, pair.cl, pair.name) {
			cleaned = true
		}
		if cleanupStaleNamespace(ctx, pair.cl, pair.name) {
			cleaned = true
		}
		if cleanupStaleVolumeAttachments(pair.name) {
			cleaned = true
		}
		if cleanupOrphanedPVs(ctx, pair.cl, pair.name) {
			cleaned = true
		}
		if cleanupOrphanedRBDImages(ctx, pair.cl, pair.name) {
			cleaned = true
		}
	}

	if cleaned {
		GinkgoWriter.Printf("  [guard] Stale test resources cleaned up\n")
	} else {
		GinkgoWriter.Printf("  [guard] No stale test resources found\n")
	}
}

// cleanupClusterScopedDRResources deletes DRPlans, DRExecutions, and ShadowPVs,
// stripping finalizers if they block deletion.
func cleanupClusterScopedDRResources(ctx context.Context, cl client.Client, site string) bool {
	cleaned := false

	var plans soteriav1alpha1.DRPlanList
	if err := cl.List(ctx, &plans); err == nil {
		for i := range plans.Items {
			GinkgoWriter.Printf("  [cleanup:%s] Deleting stale DRPlan %s\n", site, plans.Items[i].Name)
			forceDelete(ctx, cl, &plans.Items[i])
			cleaned = true
		}
	}

	var execs soteriav1alpha1.DRExecutionList
	if err := cl.List(ctx, &execs); err == nil {
		for i := range execs.Items {
			GinkgoWriter.Printf("  [cleanup:%s] Deleting stale DRExecution %s\n", site, execs.Items[i].Name)
			forceDelete(ctx, cl, &execs.Items[i])
			cleaned = true
		}
	}

	var spvs soteriav1alpha1.ShadowPVList
	if err := cl.List(ctx, &spvs); err == nil {
		for i := range spvs.Items {
			GinkgoWriter.Printf("  [cleanup:%s] Deleting stale ShadowPV %s\n", site, spvs.Items[i].Name)
			forceDelete(ctx, cl, &spvs.Items[i])
			cleaned = true
		}
	}

	return cleaned
}

// forceDelete deletes an object and strips its finalizers if it gets stuck.
func forceDelete(ctx context.Context, cl client.Client, obj client.Object) {
	_ = cl.Delete(ctx, obj)

	// Give the controller a moment to process the deletion.
	time.Sleep(2 * time.Second)

	fresh := obj.DeepCopyObject().(client.Object)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
		return
	}
	if fresh.GetDeletionTimestamp().IsZero() {
		return
	}
	if len(fresh.GetFinalizers()) > 0 {
		GinkgoWriter.Printf("  [cleanup] Stripping finalizers from %s %s\n",
			fresh.GetObjectKind().GroupVersionKind().Kind, fresh.GetName())
		fresh.SetFinalizers(nil)
		_ = cl.Update(ctx, fresh)
	}
}

// cleanupStaleNamespace handles a test namespace stuck in Terminating by
// stripping finalizers on resources that block deletion.
func cleanupStaleNamespace(ctx context.Context, cl client.Client, site string) bool {
	var ns corev1.Namespace
	if err := cl.Get(ctx, client.ObjectKey{Name: testNamespace}, &ns); err != nil {
		return false
	}
	if ns.DeletionTimestamp.IsZero() && ns.Status.Phase != corev1.NamespaceTerminating {
		// Namespace exists but is not stuck — delete it normally for a clean slate.
		GinkgoWriter.Printf("  [cleanup:%s] Deleting existing namespace %s\n", site, testNamespace)
		_ = cl.Delete(ctx, &ns)
		waitForNamespaceDeletion(ctx, cl, site)
		return true
	}

	GinkgoWriter.Printf("  [cleanup:%s] Namespace %s is stuck in Terminating, stripping finalizers\n",
		site, testNamespace)

	// VolumeReplication CRs are the most common blocker.
	var vrList replicationv1alpha1.VolumeReplicationList
	if err := cl.List(ctx, &vrList, client.InNamespace(testNamespace)); err == nil {
		for i := range vrList.Items {
			if len(vrList.Items[i].GetFinalizers()) > 0 {
				vrList.Items[i].SetFinalizers(nil)
				_ = cl.Update(ctx, &vrList.Items[i])
			}
		}
	}

	// VolumeGroupReplication CRs
	stripFinalizersByShell(site, "volumegroupreplications.replication.storage.openshift.io")

	// PVCs with protection finalizers
	var pvcList corev1.PersistentVolumeClaimList
	if err := cl.List(ctx, &pvcList, client.InNamespace(testNamespace)); err == nil {
		for i := range pvcList.Items {
			if len(pvcList.Items[i].GetFinalizers()) > 0 {
				pvcList.Items[i].SetFinalizers(nil)
				_ = cl.Update(ctx, &pvcList.Items[i])
			}
		}
	}

	waitForNamespaceDeletion(ctx, cl, site)
	return true
}

func waitForNamespaceDeletion(ctx context.Context, cl client.Client, site string) {
	Eventually(func(g Gomega) {
		var ns corev1.Namespace
		err := cl.Get(ctx, client.ObjectKey{Name: testNamespace}, &ns)
		g.Expect(errors.IsNotFound(err)).To(BeTrue(),
			"namespace %s on %s should be fully deleted", testNamespace, site)
	}).WithTimeout(2 * time.Minute).WithPolling(3 * time.Second).Should(Succeed())
}

func stripFinalizersByShell(site, resourceType string) {
	ctx := "--context=" + site
	out := runOutput("kubectl", ctx, "-n", testNamespace, "get", resourceType,
		"-o", "jsonpath={.items[*].metadata.name}")
	names := strings.Fields(strings.TrimSpace(out))
	for _, name := range names {
		if name == "" {
			continue
		}
		runOutput("kubectl", ctx, "-n", testNamespace, "patch", resourceType, name,
			"--type=json", "-p", `[{"op":"remove","path":"/metadata/finalizers"}]`)
	}
}

// cleanupStaleVolumeAttachments deletes VolumeAttachments whose referenced PV
// no longer exists or is in a terminal state.
func cleanupStaleVolumeAttachments(site string) bool {
	ctxFlag := "--context=" + site
	out := runOutput("kubectl", ctxFlag, "get", "volumeattachments",
		"-o", "jsonpath={range .items[*]}{.metadata.name}={.spec.source.persistentVolumeName}\n{end}")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	cleaned := false
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		vaName, pvName := parts[0], parts[1]
		if pvName == "" {
			continue
		}
		checkOut := runOutput("kubectl", ctxFlag, "get", "pv", pvName,
			"--no-headers", "--ignore-not-found")
		checkOut = strings.TrimSpace(checkOut)
		pvGone := checkOut == ""
		pvTerminal := strings.Contains(checkOut, "Released") ||
			strings.Contains(checkOut, "Terminating") ||
			strings.Contains(checkOut, "Failed")
		if !pvGone && !pvTerminal {
			continue
		}
		GinkgoWriter.Printf("  [cleanup:%s] Deleting stale VolumeAttachment %s (PV %s gone)\n",
			site, vaName, pvName)
		runOutput("kubectl", ctxFlag, "patch", "volumeattachment", vaName,
			"--type=json", "-p", `[{"op":"remove","path":"/metadata/finalizers"}]`)
		runOutput("kubectl", ctxFlag, "delete", "volumeattachment", vaName,
			"--ignore-not-found", "--wait=false")
		cleaned = true
	}
	return cleaned
}

// cleanupOrphanedPVs deletes PVs in Released/Failed state or stuck Terminating.
func cleanupOrphanedPVs(ctx context.Context, cl client.Client, site string) bool {
	var pvList corev1.PersistentVolumeList
	if err := cl.List(ctx, &pvList); err != nil {
		return false
	}
	cleaned := false
	for i := range pvList.Items {
		pv := &pvList.Items[i]
		phase := pv.Status.Phase
		isTestPV := pv.Spec.ClaimRef != nil && pv.Spec.ClaimRef.Namespace == testNamespace

		if phase == corev1.VolumeReleased || phase == corev1.VolumeFailed {
			if !isTestPV {
				continue
			}
			GinkgoWriter.Printf("  [cleanup:%s] Deleting %s PV %s\n", site, phase, pv.Name)
			forceDelete(ctx, cl, pv)
			cleaned = true
		} else if !pv.DeletionTimestamp.IsZero() && isTestPV {
			GinkgoWriter.Printf("  [cleanup:%s] Force-deleting stuck PV %s\n", site, pv.Name)
			forceDelete(ctx, cl, pv)
			cleaned = true
		}
	}
	return cleaned
}

// cleanupOrphanedRBDImages removes RBD images from mirrored-pool that are
// not referenced by any live PV on the cluster. This handles images orphaned
// when PVs are force-deleted (reclaimPolicy: Retain on west, or finalizer
// stripping on either side).
func cleanupOrphanedRBDImages(ctx context.Context, cl client.Client, site string) bool {
	ctxFlag := "--context=" + site

	// Build set of RBD image names referenced by live PVs.
	livePVImages := make(map[string]bool)
	var pvList corev1.PersistentVolumeList
	if err := cl.List(ctx, &pvList); err == nil {
		for _, pv := range pvList.Items {
			if pv.Spec.CSI == nil {
				continue
			}
			if imgName, ok := pv.Spec.CSI.VolumeAttributes["imageName"]; ok {
				livePVImages[imgName] = true
			}
		}
	}

	// List all images in mirrored-pool.
	out := runOutput("kubectl", ctxFlag, "-n", "rook-ceph",
		"exec", "deploy/rook-ceph-tools", "--",
		"rbd", "ls", "mirrored-pool")
	images := strings.Split(strings.TrimSpace(out), "\n")
	if len(images) == 0 || (len(images) == 1 && images[0] == "") {
		return false
	}

	cleaned := false
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		if livePVImages[img] {
			continue
		}
		GinkgoWriter.Printf("  [cleanup:%s] Removing orphaned RBD image %s\n", site, img)

		// Disable mirroring first (required before removal of mirrored images).
		runOutput("kubectl", ctxFlag, "-n", "rook-ceph",
			"exec", "deploy/rook-ceph-tools", "--",
			"rbd", "mirror", "image", "disable", "mirrored-pool/"+img, "--force")

		// Purge snapshots.
		runOutput("kubectl", ctxFlag, "-n", "rook-ceph",
			"exec", "deploy/rook-ceph-tools", "--",
			"rbd", "snap", "purge", "mirrored-pool/"+img)

		// Remove the image.
		rmOut := runOutput("kubectl", ctxFlag, "-n", "rook-ceph",
			"exec", "deploy/rook-ceph-tools", "--",
			"rbd", "rm", "mirrored-pool/"+img)
		if strings.Contains(rmOut, "error") || strings.Contains(rmOut, "No such file") {
			// Fallback: trash the image.
			runOutput("kubectl", ctxFlag, "-n", "rook-ceph",
				"exec", "deploy/rook-ceph-tools", "--",
				"rbd", "trash", "mv", "mirrored-pool/"+img)
		}
		cleaned = true
	}

	// Purge the trash.
	if cleaned {
		runOutput("kubectl", ctxFlag, "-n", "rook-ceph",
			"exec", "deploy/rook-ceph-tools", "--",
			"rbd", "trash", "purge", "mirrored-pool")
	}
	return cleaned
}

// ---------------------------------------------------------------------------
// Scenario definition
// ---------------------------------------------------------------------------

type lifecycleScenario struct {
	name                   string
	failoverMode           soteriav1alpha1.ExecutionMode
	volumeReplicationClass string
	storageClass           string
	simulateDisaster       bool
	vmPrefix               string
}

func (s *lifecycleScenario) planName() string {
	return s.vmPrefix + "app"
}

type vmDef struct {
	name string
	wave string
}

var baseVMs = []vmDef{
	{name: "db", wave: "1"},
	{name: "appserver-1", wave: "2"},
	{name: "appserver-2", wave: "2"},
	{name: "webserver-1", wave: "3"},
	{name: "webserver-2", wave: "3"},
	{name: "webserver-3", wave: "3"},
}

// ---------------------------------------------------------------------------
// Per-test scenario deployment and teardown
// ---------------------------------------------------------------------------

func deployScenario(ctx context.Context, s *lifecycleScenario) {
	planName := s.planName()

	By("ensuring golden Fedora image exists on east")
	ensureGoldenImage(ctx, eastClient)

	By("cloning rootdisk DataVolumes from golden image")
	for _, vm := range baseVMs {
		createRootDiskDV(ctx, eastClient, s.vmPrefix+vm.name, s.storageClass)
	}

	By("waiting for DataVolume clones to complete")
	waitForDataVolumes(ctx, eastClient, s.vmPrefix)

	By("creating east VMs (runStrategy: Always)")
	for _, vm := range baseVMs {
		createVM(ctx, eastClient, s.vmPrefix+vm.name, vm.wave,
			kubevirtv1.RunStrategyAlways, planName)
	}

	By("creating west VMs (runStrategy: Halted, no PVCs)")
	for _, vm := range baseVMs {
		createVM(ctx, westClient, s.vmPrefix+vm.name, vm.wave,
			kubevirtv1.RunStrategyHalted, planName)
	}

	By("creating DRPlan " + planName)
	createDRPlan(ctx, eastClient, planName, s.volumeReplicationClass)
}

func convergeScenario(ctx context.Context, s *lifecycleScenario) {
	planName := s.planName()

	By("waiting for ShadowPV resources from publisher")
	waitForShadowPVResources(ctx, eastClient, planName)

	By("waiting for ShadowPV consumer PVs on west")
	pvNames := waitForShadowPVConsumerPVs(ctx, eastClient, westClient, planName)

	By("creating west PVCs bound to ShadowPV-provisioned PVs")
	createWestPVCsFromShadowPVs(ctx, westClient, s.vmPrefix, pvNames)

	By("waiting for DRPlan to reach healthy state")
	waitForDRPlanHealthy(ctx, eastClient, planName)

	By("waiting for VolumeReplication CRs on both sites")
	waitForVRsOnBothSites(ctx, planName)

	By("waiting for east VMs to reach Running state")
	waitForVMsRunning(ctx, eastClient, s.vmPrefix)
}

//nolint:unparam
func teardownScenario(ctx context.Context, s *lifecycleScenario) {
	planName := s.planName()

	// After disaster scenarios, east may still be stopped. Bring it
	// back and fully heal the infrastructure (Cilium Cluster Mesh, Ceph,
	// CSI Addons, ScyllaDB, controller webhook) before attempting cleanup.
	if s.simulateDisaster {
		ensureMinikubeRunning(eastMinikubeProfile)
		healClusterAfterRestart(ctx, eastMinikubeProfile)
	}

	// Delete the DRPlan first so the controller can clean up VolumeReplication
	// finalizers while PVCs still exist (CSI Addons needs the PVC to remove
	// its replication.storage.openshift.io finalizer from VRs).
	deleteIfExists(ctx, eastClient, &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
	})

	for _, vm := range baseVMs {
		deleteIfExists(ctx, eastClient, &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name, Namespace: testNamespace},
		})
		deleteIfExists(ctx, westClient, &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name, Namespace: testNamespace},
		})
		deleteIfExists(ctx, eastClient, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name + "-rootdisk", Namespace: testNamespace},
		})
		deleteIfExists(ctx, westClient, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name + "-rootdisk", Namespace: testNamespace},
		})
	}

	cleanupShadowPVConsumerPVs(ctx, westClient, planName)
	cleanupShadowPVs(ctx, eastClient, planName)
	cleanupShadowPVs(ctx, westClient, planName)

	// Wait until VMs are fully gone so the next scenario doesn't pile up
	// resources on the minikube cluster which can only run one DRPlan at a time.
	waitForVMsGone(ctx, eastClient, s.vmPrefix)
	waitForVMsGone(ctx, westClient, s.vmPrefix)
}

func waitForVMsGone(ctx context.Context, cl client.Client, vmPrefix string) {
	Eventually(func(g Gomega) {
		for _, vm := range baseVMs {
			var obj kubevirtv1.VirtualMachine
			err := cl.Get(ctx, client.ObjectKey{
				Name: vmPrefix + vm.name, Namespace: testNamespace,
			}, &obj)
			g.Expect(errors.IsNotFound(err)).To(BeTrue(),
				"VM %s should be deleted", vmPrefix+vm.name)
		}
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// ---------------------------------------------------------------------------
// Resource factories
// ---------------------------------------------------------------------------

const (
	goldenImageNS   = "kubevirt-golden-images"
	goldenImageName = "fedora-golden"
)

func ensureGoldenImage(ctx context.Context, cl client.Client) {
	goldenNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: goldenImageNS},
	}
	err := cl.Create(ctx, goldenNS)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "creating golden image namespace")
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "cdi.kubevirt.io", Version: "v1beta1", Kind: "DataVolume",
	})
	err = cl.Get(ctx, client.ObjectKey{
		Name: goldenImageName, Namespace: goldenImageNS,
	}, existing)
	if err == nil {
		phase, _, _ := unstructured.NestedString(existing.Object, "status", "phase")
		if phase == "Succeeded" {
			return
		}
	}

	dv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cdi.kubevirt.io/v1beta1",
			"kind":       "DataVolume",
			"metadata": map[string]interface{}{
				"name":      goldenImageName,
				"namespace": goldenImageNS,
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"registry": map[string]interface{}{
						"url":        "docker://quay.io/containerdisks/fedora:latest",
						"pullMethod": "node",
					},
				},
				"storage": map[string]interface{}{
					"accessModes":      []interface{}{"ReadWriteOnce"},
					"storageClassName": "rook-ceph-block",
					"resources": map[string]interface{}{
						"requests": map[string]interface{}{
							"storage": "5Gi",
						},
					},
				},
			},
		},
	}
	err = cl.Create(ctx, dv)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "creating golden image DataVolume")
	}

	Eventually(func(g Gomega) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "cdi.kubevirt.io", Version: "v1beta1", Kind: "DataVolume",
		})
		g.Expect(cl.Get(ctx, client.ObjectKey{
			Name: goldenImageName, Namespace: goldenImageNS,
		}, obj)).To(Succeed())
		phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
		g.Expect(phase).To(Equal("Succeeded"),
			"golden image DV phase: got %s, want Succeeded", phase)
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

func createRootDiskDV(ctx context.Context, cl client.Client, vmName, sc string) {
	dvName := vmName + "-rootdisk"
	dv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cdi.kubevirt.io/v1beta1",
			"kind":       "DataVolume",
			"metadata": map[string]interface{}{
				"name":      dvName,
				"namespace": testNamespace,
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"pvc": map[string]interface{}{
						"namespace": goldenImageNS,
						"name":      goldenImageName,
					},
				},
				"storage": map[string]interface{}{
					"accessModes":      []interface{}{"ReadWriteOnce"},
					"storageClassName": sc,
					"resources": map[string]interface{}{
						"requests": map[string]interface{}{
							"storage": "5Gi",
						},
					},
				},
			},
		},
	}
	err := cl.Create(ctx, dv)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "creating DataVolume %s", dvName)
	}
}

func waitForDataVolumes(ctx context.Context, cl client.Client, vmPrefix string) {
	Eventually(func(g Gomega) {
		for _, vm := range baseVMs {
			dvName := vmPrefix + vm.name + "-rootdisk"
			dv := &unstructured.Unstructured{}
			dv.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "cdi.kubevirt.io",
				Version: "v1beta1",
				Kind:    "DataVolume",
			})
			err := cl.Get(ctx, client.ObjectKey{
				Name: dvName, Namespace: testNamespace,
			}, dv)
			g.Expect(err).NotTo(HaveOccurred(), "DataVolume %s not found", dvName)
			phase, _, _ := unstructured.NestedString(dv.Object, "status", "phase")
			g.Expect(phase).To(Equal("Succeeded"),
				"DataVolume %s phase: got %s, want Succeeded", dvName, phase)
		}
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

func createVM(
	ctx context.Context, cl client.Client, name, wave string,
	strategy kubevirtv1.VirtualMachineRunStrategy, planName string,
) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: planName,
				soteriav1alpha1.WaveLabel:   wave,
			},
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			RunStrategy: ptr.To(strategy),
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						soteriav1alpha1.DRPlanLabel: planName,
						soteriav1alpha1.WaveLabel:   wave,
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
								{
									Name: "rootdisk",
									DiskDevice: kubevirtv1.DiskDevice{
										Disk: &kubevirtv1.DiskTarget{Bus: kubevirtv1.DiskBusVirtio},
									},
								},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "rootdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: name + "-rootdisk",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	err := cl.Create(ctx, vm)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "creating VM %s", name)
	}
}

func createDRPlan(
	ctx context.Context, cl client.Client, planName, vrc string,
) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:   "east",
			SecondarySite: "west",
			VolumeReplicationDriver: soteriav1alpha1.VolumeReplicationDriverConfig{
				Type:                   "csi-extension",
				VolumeReplicationClass: vrc,
			},
			MaxConcurrentFailovers: 2,
		},
	}
	err := cl.Create(ctx, plan)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "creating DRPlan %s", planName)
	}
}

// ---------------------------------------------------------------------------
// ShadowPV provisioning flow
// ---------------------------------------------------------------------------

func waitForShadowPVResources(ctx context.Context, cl client.Client, planName string) {
	Eventually(func(g Gomega) {
		var list soteriav1alpha1.ShadowPVList
		g.Expect(cl.List(ctx, &list, client.MatchingLabels{
			soteriav1alpha1.DRPlanLabel: planName,
		})).To(Succeed())
		g.Expect(list.Items).NotTo(BeEmpty(),
			"ShadowPV resources should exist for plan %s", planName)
	}).WithTimeout(shadowPVTimeout).WithPolling(5 * time.Second).Should(Succeed())
}

type pvBindInfo struct {
	pvName       string
	storageClass string
}

// waitForShadowPVConsumerPVs builds a PVC-name → pvBindInfo mapping by reading
// ShadowPV resources for the plan from the source cluster, then verifying the
// consumer-created PVs exist on the target cluster. We read ShadowPVs from
// sourceClient (the publisher's cluster) to avoid dependency on cross-DC
// ScyllaDB replication latency, and verify PVs via targetClient (the
// consumer's cluster).
func waitForShadowPVConsumerPVs(
	ctx context.Context, sourceClient, targetClient client.Client, planName string,
) map[string]pvBindInfo {
	pvMap := make(map[string]pvBindInfo)
	Eventually(func(g Gomega) {
		var spvList soteriav1alpha1.ShadowPVList
		g.Expect(sourceClient.List(ctx, &spvList, client.MatchingLabels{
			soteriav1alpha1.DRPlanLabel: planName,
		})).To(Succeed())
		g.Expect(spvList.Items).NotTo(BeEmpty(),
			"ShadowPV resources should exist for plan %s", planName)

		pvMap = make(map[string]pvBindInfo)
		for _, spv := range spvList.Items {
			for _, entry := range spv.Spec.PVs {
				if entry.PV.ClaimRef == nil || entry.PV.ClaimRef.Name == "" {
					continue
				}
				pvMap[entry.PV.ClaimRef.Name] = pvBindInfo{
					pvName:       entry.PVName,
					storageClass: entry.PV.StorageClassName,
				}
			}
		}
		g.Expect(pvMap).NotTo(BeEmpty(),
			"ShadowPV entries should have ClaimRef for plan %s", planName)

		for pvcName, info := range pvMap {
			var pv corev1.PersistentVolume
			g.Expect(targetClient.Get(ctx, client.ObjectKey{Name: info.pvName}, &pv)).To(Succeed(),
				"consumer PV %s (for PVC %s) should exist on target", info.pvName, pvcName)
		}
	}).WithTimeout(shadowPVTimeout).WithPolling(5 * time.Second).Should(Succeed())
	return pvMap
}

func createWestPVCsFromShadowPVs(
	ctx context.Context, cl client.Client, vmPrefix string, pvMap map[string]pvBindInfo,
) {
	for _, vm := range baseVMs {
		pvcName := vmPrefix + vm.name + "-rootdisk"
		info, found := pvMap[pvcName]
		Expect(found).To(BeTrue(),
			"ShadowPV consumer PV for %s not found in PV map (available keys: %v)",
			pvcName, mapKeys(pvMap))

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: ptr.To(info.storageClass),
				VolumeName:       info.pvName,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}
		err := cl.Create(ctx, pvc)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred(),
				"creating west PVC %s bound to PV %s", pvcName, info.pvName)
		}
	}
}

func mapKeys(m map[string]pvBindInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ---------------------------------------------------------------------------
// Infrastructure health guard
// ---------------------------------------------------------------------------

// verifyInfrastructureHealth runs a fast, non-healing check against all
// critical shared infrastructure. It collects every failure and reports
// them together so operators can fix everything in one pass. Call this at
// the top of BeforeSuite to fail fast before any test resources are created.
func verifyInfrastructureHealth() {
	var failures []string

	for _, profile := range []string{eastMinikubeProfile, westMinikubeProfile} {
		ctx := "--context=" + profile

		// 1. Cilium Cluster Mesh
		out := runOutput("kubectl", ctx, "exec", "-n", "kube-system",
			"ds/cilium", "--", "cilium-dbg", "status")
		if !strings.Contains(out, "remote clusters ready") {
			// Extract the cluster-mesh line for diagnostics.
			meshLine := ""
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(strings.ToLower(l), "cluster") {
					meshLine = strings.TrimSpace(l)
					break
				}
			}
			failures = append(failures,
				fmt.Sprintf("[%s] Cilium Cluster Mesh not healthy: %s", profile, meshLine))
		}

		// 2. Ceph cluster health
		out = runOutput("kubectl", ctx, "-n", "rook-ceph",
			"exec", "deploy/rook-ceph-tools", "--", "ceph", "health")
		out = strings.TrimSpace(out)
		if !strings.HasPrefix(out, "HEALTH_OK") {
			failures = append(failures,
				fmt.Sprintf("[%s] Ceph not healthy: %s", profile, out))
		}

		// 3. Ceph RBD mirroring pool health
		out = runOutput("kubectl", ctx, "-n", "rook-ceph",
			"exec", "deploy/rook-ceph-tools", "--",
			"rbd", "mirror", "pool", "status", "mirrored-pool")
		out = strings.TrimSpace(out)
		if !strings.Contains(out, "health: OK") {
			healthLine := ""
			for _, l := range strings.Split(out, "\n") {
				l = strings.TrimSpace(l)
				if strings.HasPrefix(l, "health:") {
					healthLine = l
					break
				}
			}
			failures = append(failures,
				fmt.Sprintf("[%s] Ceph RBD mirroring not healthy: %s", profile, healthLine))
		}
	}

	// 4. ScyllaDB cross-DC gossip
	out := runOutput("kubectl", "--context="+eastMinikubeProfile,
		"-n", "soteria", "exec", "soteria-scylladb-east-rack1-0",
		"-c", "scylla", "--", "nodetool", "status")
	unCount := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "UN ") {
			unCount++
		}
	}
	if unCount < 2 {
		failures = append(failures,
			fmt.Sprintf("[scylladb] Expected >=2 UN nodes across DCs, found %d", unCount))
	}

	if len(failures) > 0 {
		msg := "Infrastructure health check failed — fix these before running tests:\n"
		for _, f := range failures {
			msg += "  • " + f + "\n"
		}
		Fail(msg)
	}

	GinkgoWriter.Printf("  [guard] Infrastructure health verified: Cilium, Ceph, mirroring, ScyllaDB all OK\n")
}

// ---------------------------------------------------------------------------
// Minikube control
// ---------------------------------------------------------------------------

func minikubeStop(profile string) {
	cmd := exec.Command("minikube", "stop", "-p", profile)
	output, err := cmd.CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(),
		"minikube stop -p %s failed: %s", profile, string(output))
	GinkgoWriter.Printf("  [minikube] stopped profile %s\n", profile)
}

func minikubeStart(profile string) {
	cmd := exec.Command("minikube", "start", "-p", profile)
	output, err := cmd.CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(),
		"minikube start -p %s failed: %s", profile, string(output))
	GinkgoWriter.Printf("  [minikube] started profile %s\n", profile)
}

func ensureMinikubeRunning(profile string) {
	cmd := exec.Command("minikube", "status", "-p", profile, "-f", "{{.Host}}")
	output, _ := cmd.CombinedOutput()
	if strings.TrimSpace(string(output)) == "Running" {
		return
	}
	GinkgoWriter.Printf("  [minikube] %s not running, starting...\n", profile)
	minikubeStart(profile)
}

func waitForAPIServer(ctx context.Context, cl client.Client) {
	Eventually(func() error {
		var nsList corev1.NamespaceList
		return cl.List(ctx, &nsList)
	}).WithTimeout(clusterRestartTimeout).WithPolling(5*time.Second).Should(Succeed(),
		"API server did not become ready within timeout")
}

// waitForControllerReady waits until the controller-manager pod in the given
// namespace has all containers ready. This ensures the webhook endpoint is
// reachable (the webhook Service routes to this pod).
func waitForControllerReady(ctx context.Context, cs *kubernetes.Clientset, ns string) {
	Eventually(func(g Gomega) {
		pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			LabelSelector: "control-plane=controller-manager",
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty(), "controller pod should exist")
		for _, c := range pods.Items[0].Status.ContainerStatuses {
			g.Expect(c.Ready).To(BeTrue(),
				"container %s should be ready", c.Name)
		}
	}).WithTimeout(8 * time.Minute).WithPolling(5 * time.Second).Should(Succeed(),
		"controller-manager in %s did not become ready", ns)
	GinkgoWriter.Printf("  [infra] controller-manager ready in %s\n", ns)
}

// ---------------------------------------------------------------------------
// Infrastructure recovery after minikube restart
// ---------------------------------------------------------------------------

// healClusterAfterRestart brings all infrastructure components back to a
// healthy state after a minikube cluster restart. This is needed because
// minikube stop/start changes pod IPs, breaking cached connections in
// Cilium Cluster Mesh, CSI Addons, and ScyllaDB gossip.
func healClusterAfterRestart(ctx context.Context, profile string) {
	By("waiting for API server on " + profile)
	cl := eastClient
	cs := eastClientset
	if profile == westMinikubeProfile {
		cl = westClient
		cs = westClientset
	}
	waitForAPIServer(ctx, cl)

	By("healing Cilium Cluster Mesh")
	healCiliumClusterMesh(profile)

	By("waiting for Ceph health")
	waitForCephHealthy(profile)

	By("healing CSI Addons (stale connections)")
	healCSIAddons(profile)

	By("waiting for controller-manager readiness")
	waitForControllerReady(ctx, cs, "soteria")

	By("waiting for ScyllaDB multi-DC convergence")
	waitForScyllaDBConvergence()
}

func healCiliumClusterMesh(profile string) {
	ctx := "--context=" + profile

	run("kubectl", ctx, "-n", "kube-system",
		"rollout", "restart", "deployment/clustermesh-apiserver")
	run("kubectl", ctx, "-n", "kube-system",
		"rollout", "status", "deployment/clustermesh-apiserver", "--timeout=120s")

	run("kubectl", ctx, "-n", "kube-system",
		"rollout", "restart", "daemonset/cilium")
	run("kubectl", ctx, "-n", "kube-system",
		"rollout", "status", "daemonset/cilium", "--timeout=180s")

	Eventually(func(g Gomega) {
		out := runOutput("kubectl", ctx, "exec", "-n", "kube-system",
			"ds/cilium", "--", "cilium-dbg", "status")
		g.Expect(out).To(ContainSubstring("remote clusters ready"),
			"Cilium Cluster Mesh should reconnect")
	}).WithTimeout(2 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	GinkgoWriter.Printf("  [infra] Cilium Cluster Mesh healed on %s\n", profile)
}

func waitForCephHealthy(profile string) {
	ctx := "--context=" + profile
	Eventually(func(g Gomega) {
		out := runOutput("kubectl", ctx, "-n", "rook-ceph",
			"exec", "deploy/rook-ceph-tools", "--", "ceph", "health")
		g.Expect(out).To(ContainSubstring("HEALTH_OK"),
			"Ceph should be HEALTH_OK")
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	GinkgoWriter.Printf("  [infra] Ceph healthy on %s\n", profile)
}

func healCSIAddons(profile string) {
	ctx := "--context=" + profile

	// Delete stale CSIAddonsNode entries so the controller re-discovers
	// endpoints with current pod IPs.
	out := runOutput("kubectl", ctx, "get", "csiaddonsnodes",
		"-n", "rook-ceph", "-o", "name")
	for _, node := range strings.Split(strings.TrimSpace(out), "\n") {
		if node == "" {
			continue
		}
		run("kubectl", ctx, "-n", "rook-ceph", "delete", node, "--ignore-not-found")
	}

	// Restart the RBD ctrlplugin pod so CSI Addons gets a fresh gRPC endpoint.
	out = runOutput("kubectl", ctx, "-n", "rook-ceph", "get", "pods",
		"--no-headers", "-o", "custom-columns=:metadata.name")
	for _, pod := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(pod, "rbd") && strings.Contains(pod, "ctrlplugin") {
			run("kubectl", ctx, "-n", "rook-ceph", "delete", "pod", pod, "--wait=false")
		}
	}

	// Restart CSI Addons node-plugin DaemonSets to get fresh pod IPs.
	out = runOutput("kubectl", ctx, "-n", "rook-ceph", "get", "ds", "-o", "name")
	for _, ds := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(ds, "csi-addons") {
			run("kubectl", ctx, "-n", "rook-ceph", "rollout", "restart", ds)
			run("kubectl", ctx, "-n", "rook-ceph", "rollout", "status", ds, "--timeout=180s")
		}
	}

	// Restart the CSI Addons controller itself so it picks up the new endpoints.
	run("kubectl", ctx, "-n", "csi-addons-system",
		"rollout", "restart", "deployment/csi-addons-controller-manager")
	run("kubectl", ctx, "-n", "csi-addons-system",
		"rollout", "status", "deployment/csi-addons-controller-manager", "--timeout=120s")

	GinkgoWriter.Printf("  [infra] CSI Addons healed on %s\n", profile)
}

func waitForScyllaDBConvergence() {
	Eventually(func(g Gomega) {
		out := runOutput("kubectl", "--context="+eastMinikubeProfile,
			"-n", "soteria", "exec", "soteria-scylladb-east-rack1-0",
			"-c", "scylla", "--", "nodetool", "status")
		lines := strings.Split(out, "\n")
		unCount := 0
		for _, l := range lines {
			if strings.HasPrefix(l, "UN ") {
				unCount++
			}
		}
		g.Expect(unCount).To(BeNumerically(">=", 2),
			"ScyllaDB should have >=2 UN nodes across DCs, got %d", unCount)
	}).WithTimeout(5 * time.Minute).WithPolling(15 * time.Second).Should(Succeed())

	GinkgoWriter.Printf("  [infra] ScyllaDB multi-DC convergence confirmed\n")
}

// run executes a command and fails the test on error.
func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("  [cmd] %s %s failed: %s\n", name, strings.Join(args, " "), string(output))
	}
	ExpectWithOffset(2, err).NotTo(HaveOccurred(),
		"%s %s failed: %s", name, strings.Join(args, " "), string(output))
}

// runOutput executes a command and returns stdout+stderr. Failures are non-fatal.
func runOutput(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	output, _ := cmd.CombinedOutput()
	return string(output)
}

// ---------------------------------------------------------------------------
// Polling / wait helpers
// ---------------------------------------------------------------------------

func waitForDRPlanHealthy(ctx context.Context, cl client.Client, planName string) {
	Eventually(func(g Gomega) {
		var plan soteriav1alpha1.DRPlan
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: planName}, &plan)).To(Succeed())
		g.Expect(plan.Status.Phase).To(Equal(soteriav1alpha1.PhaseSteadyState))
		for _, ct := range []string{
			"Ready", "SitesInSync", "DisksConsistent", "ReplicationHealthy",
		} {
			cond := meta.FindStatusCondition(plan.Status.Conditions, ct)
			g.Expect(cond).NotTo(BeNil(), "missing condition %s", ct)
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue),
				"condition %s should be True", ct)
		}
	}).WithTimeout(setupTimeout).WithPolling(10*time.Second).Should(Succeed(),
		"DRPlan %s did not become healthy", planName)
}

func waitForVMsRunning(ctx context.Context, cl client.Client, vmPrefix string) {
	Eventually(func(g Gomega) {
		for _, vm := range baseVMs {
			var vmi kubevirtv1.VirtualMachineInstance
			g.Expect(cl.Get(ctx, client.ObjectKey{
				Name: vmPrefix + vm.name, Namespace: testNamespace,
			}, &vmi)).To(Succeed(), "VMI %s should exist", vmPrefix+vm.name)
			g.Expect(vmi.Status.Phase).To(Equal(kubevirtv1.Running),
				"VMI %s should be Running", vmPrefix+vm.name)
		}
	}).WithTimeout(setupTimeout).WithPolling(10 * time.Second).Should(Succeed())
}

//nolint:unparam
func createDRExecution(
	ctx context.Context, cl client.Client, name, planName string,
	mode soteriav1alpha1.ExecutionMode,
) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				soteriav1alpha1.PlanNameLabel: planName,
			},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: planName,
			Mode:     mode,
		},
	}
	ExpectWithOffset(1, cl.Create(ctx, exec)).To(Succeed(),
		"creating DRExecution %s", name)
}

//nolint:unparam
func waitForExecResult(
	ctx context.Context, cl client.Client, name string,
	expectedResult soteriav1alpha1.ExecutionResult, timeout time.Duration,
) {
	Eventually(func(g Gomega) {
		var exec soteriav1alpha1.DRExecution
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: name}, &exec)).To(Succeed())
		g.Expect(exec.Status.Result).To(Equal(expectedResult),
			"DRExecution %s result: got %s, want %s",
			name, exec.Status.Result, expectedResult)
	}).WithTimeout(timeout).WithPolling(5 * time.Second).Should(Succeed())
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

//nolint:unparam
func assertPlanState(
	ctx context.Context, cl client.Client,
	name, expectedPhase, expectedActiveSite string,
) {
	var plan soteriav1alpha1.DRPlan
	ExpectWithOffset(1, cl.Get(ctx, client.ObjectKey{Name: name}, &plan)).To(Succeed())
	ExpectWithOffset(1, plan.Status.Phase).To(Equal(expectedPhase),
		"DRPlan %s phase: got %s, want %s", name, plan.Status.Phase, expectedPhase)
	ExpectWithOffset(1, plan.Status.ActiveSite).To(Equal(expectedActiveSite),
		"DRPlan %s activeSite: got %s, want %s",
		name, plan.Status.ActiveSite, expectedActiveSite)
}

func assertConditionsHealthy(ctx context.Context, cl client.Client, planName string) {
	EventuallyWithOffset(1, func(g Gomega) {
		var plan soteriav1alpha1.DRPlan
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: planName}, &plan)).To(Succeed())
		for _, ct := range []string{
			"Ready", "SitesInSync", "DisksConsistent", "ReplicationHealthy",
		} {
			cond := meta.FindStatusCondition(plan.Status.Conditions, ct)
			g.Expect(cond).NotTo(BeNil(),
				"DRPlan %s missing condition %s", planName, ct)
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue),
				"DRPlan %s condition %s: got %s, want True", planName, ct, cond.Status)
		}
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

func waitForVRsOnBothSites(ctx context.Context, planName string) {
	expectedCount := len(baseVMs)
	for _, pair := range []struct {
		name string
		cl   client.Client
	}{
		{"east", eastClient},
		{"west", westClient},
	} {
		Eventually(func(g Gomega) {
			var vrList replicationv1alpha1.VolumeReplicationList
			g.Expect(pair.cl.List(ctx, &vrList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{soteriav1alpha1.DRPlanLabel: planName},
			)).To(Succeed())
			g.Expect(len(vrList.Items)).To(BeNumerically(">=", expectedCount),
				"%s: expected at least %d VRs for plan %s, got %d",
				pair.name, expectedCount, planName, len(vrList.Items))
		}).WithTimeout(setupTimeout).WithPolling(5 * time.Second).Should(Succeed(),
			"VolumeReplication CRs did not appear on %s for plan %s", pair.name, planName)
	}
}

func assertVRState(
	ctx context.Context, cl client.Client, ns, planName, expectedState string,
) {
	EventuallyWithOffset(1, func(g Gomega) {
		var vrList replicationv1alpha1.VolumeReplicationList
		g.Expect(cl.List(ctx, &vrList,
			client.InNamespace(ns),
			client.MatchingLabels{soteriav1alpha1.DRPlanLabel: planName},
		)).To(Succeed())
		g.Expect(vrList.Items).NotTo(BeEmpty(), "VR CRs should exist in %s", ns)
		for _, vr := range vrList.Items {
			g.Expect(string(vr.Spec.ReplicationState)).To(Equal(expectedState),
				"VR %s/%s should be %s", ns, vr.Name, expectedState)
		}
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

func assertVMRunState(
	ctx context.Context, cl client.Client, ns, planName string, expectRunning bool,
) {
	EventuallyWithOffset(1, func(g Gomega) {
		var vmList kubevirtv1.VirtualMachineList
		g.Expect(cl.List(ctx, &vmList,
			client.InNamespace(ns),
			client.MatchingLabels{soteriav1alpha1.DRPlanLabel: planName},
		)).To(Succeed())
		g.Expect(vmList.Items).NotTo(BeEmpty(), "VMs should exist in %s", ns)

		for _, vm := range vmList.Items {
			if expectRunning {
				g.Expect(ptr.Deref(vm.Spec.RunStrategy, "")).To(
					Equal(kubevirtv1.RunStrategyAlways),
					"VM %s should have runStrategy Always", vm.Name)
				var vmi kubevirtv1.VirtualMachineInstance
				g.Expect(cl.Get(ctx, client.ObjectKey{
					Name: vm.Name, Namespace: ns,
				}, &vmi)).To(Succeed(), "VMI %s should exist", vm.Name)
				g.Expect(vmi.Status.Phase).To(Equal(kubevirtv1.Running),
					"VMI %s should be Running", vm.Name)
			} else {
				g.Expect(ptr.Deref(vm.Spec.RunStrategy, "")).To(
					Equal(kubevirtv1.RunStrategyHalted),
					"VM %s should have runStrategy Halted", vm.Name)
			}
		}
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

func assertShadowPVEntries(
	ctx context.Context, cl client.Client, planName, expectedCluster string,
) {
	var list soteriav1alpha1.ShadowPVList
	ExpectWithOffset(1, cl.List(ctx, &list, client.MatchingLabels{
		soteriav1alpha1.DRPlanLabel: planName,
	})).To(Succeed())
	ExpectWithOffset(1, list.Items).NotTo(BeEmpty(), "ShadowPV resources should exist")

	for _, spv := range list.Items {
		hasEntry := false
		for _, entry := range spv.Spec.PVs {
			if entry.ClusterName == expectedCluster {
				hasEntry = true
				ExpectWithOffset(1, entry.PVName).NotTo(BeEmpty())
			}
		}
		ExpectWithOffset(1, hasEntry).To(BeTrue(),
			"ShadowPV %s should have entry from cluster %s", spv.Name, expectedCluster)
	}
}

func observeDemotionComplete(
	ctx context.Context, cl client.Client, execName string, timeout time.Duration,
) {
	demotionSeen := false
	lastPrinted := ""
	Eventually(func(g Gomega) {
		var exec soteriav1alpha1.DRExecution
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())

		summary := fmt.Sprintf("result=%q startTime=%v siteStatuses={", exec.Status.Result, exec.Status.StartTime != nil)
		for site, ss := range exec.Status.SiteStatuses {
			summary += fmt.Sprintf("%s: demotionComplete=%v step0=%v, ",
				site, ss.DemotionComplete, ss.Step0Complete)
		}
		summary += "}"
		if summary != lastPrinted {
			GinkgoWriter.Printf("  [poll] exec %s: %s\n", execName, summary)
			lastPrinted = summary
		}

		for _, ss := range exec.Status.SiteStatuses {
			if ss.DemotionComplete {
				demotionSeen = true
				break
			}
		}
		g.Expect(demotionSeen || exec.Status.Result != "").To(BeTrue(),
			"waiting for DemotionComplete in SiteStatuses or execution completion")
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())

	if !demotionSeen {
		GinkgoWriter.Printf("  [info] DemotionComplete resolved too fast to observe\n")
		return
	}

	Eventually(func(g Gomega) {
		var exec soteriav1alpha1.DRExecution
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())
		step0Done := false
		for _, ss := range exec.Status.SiteStatuses {
			if ss.Step0Complete {
				step0Done = true
				break
			}
		}
		g.Expect(step0Done).To(BeTrue(), "Step0Complete should become true in SiteStatuses")
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
}

//nolint:unparam
func assertPerTransition(
	ctx context.Context, cl client.Client, execName, planName string,
) {
	By(fmt.Sprintf("verifying per-transition assertions for %s", execName))

	var exec soteriav1alpha1.DRExecution
	ExpectWithOffset(1, cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())

	ExpectWithOffset(1, exec.Status.Duration).NotTo(BeEmpty(),
		"DRExecution %s should have Duration", execName)
	ExpectWithOffset(1, exec.Status.Phase).To(
		Equal(soteriav1alpha1.ExecutionPhaseSucceeded),
		"DRExecution %s Phase should be Succeeded", execName)
	ExpectWithOffset(1, exec.Status.IsActive).To(BeFalse(),
		"DRExecution %s IsActive should be false", execName)

	assertConditionsHealthy(ctx, cl, planName)
	GinkgoWriter.Printf("  [timing] %s duration: %s\n", execName, exec.Status.Duration)
}

func assertRealStorageVR(ctx context.Context, cl client.Client, ns, planName string) {
	Eventually(func(g Gomega) {
		var vrList replicationv1alpha1.VolumeReplicationList
		g.Expect(cl.List(ctx, &vrList,
			client.InNamespace(ns),
			client.MatchingLabels{soteriav1alpha1.DRPlanLabel: planName},
		)).To(Succeed())
		g.Expect(vrList.Items).NotTo(BeEmpty())

		// Check that at least one VR is either reporting a non-zero
		// lastSyncTime (journal mode) or has a Completed condition
		// (snapshot mode). Snapshot VRCs may not set lastSyncTime.
		hasEvidence := false
		for _, vr := range vrList.Items {
			if vr.Status.LastSyncTime != nil && !vr.Status.LastSyncTime.IsZero() {
				hasEvidence = true
				GinkgoWriter.Printf("  [storage] VR %s lastSyncTime: %v\n",
					vr.Name, vr.Status.LastSyncTime.Time)
				break
			}
			cond := meta.FindStatusCondition(vr.Status.Conditions, "Completed")
			if cond != nil && cond.Status == metav1.ConditionTrue {
				hasEvidence = true
				GinkgoWriter.Printf("  [storage] VR %s Completed=True\n", vr.Name)
				break
			}
			if string(vr.Status.State) == "Primary" || string(vr.Status.State) == "Secondary" {
				hasEvidence = true
				GinkgoWriter.Printf("  [storage] VR %s state: %s\n", vr.Name, vr.Status.State)
				break
			}
		}
		g.Expect(hasEvidence).To(BeTrue(),
			"at least one VR in %s should have non-zero lastSyncTime, Completed=True, or a valid state", ns)
	}).WithTimeout(2 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

// ---------------------------------------------------------------------------
// Log capture and scanning
// ---------------------------------------------------------------------------

func captureControllerLogs(
	ctx context.Context, cs *kubernetes.Clientset, ns string,
) string {
	pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "control-plane=controller-manager",
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	stream, err := cs.CoreV1().Pods(ns).GetLogs(pods.Items[0].Name,
		&corev1.PodLogOptions{Container: "manager"}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer func() { _ = stream.Close() }()
	buf := new(bytes.Buffer)
	_, _ = io.Copy(buf, stream)
	return buf.String()
}

func scanLogsForErrors(logs string) {
	lines := strings.Split(logs, "\n")
	var violations []string
	for _, line := range lines {
		if strings.Contains(line, "is immutable after completion") {
			violations = append(violations, line)
		}
	}
	persistentConflicts := filterConflictBursts(lines, 15, 10*time.Second)
	ExpectWithOffset(1, persistentConflicts).To(BeEmpty(),
		"persistent conflict bursts in controller logs (≥5 for same resource in 10s):\n%s",
		strings.Join(persistentConflicts, "\n"))
	ExpectWithOffset(1, violations).To(BeEmpty(),
		"immutability violations in controller logs:\n%s",
		strings.Join(violations, "\n"))
}

// filterConflictBursts returns conflict log lines only when the same resource
// has ≥threshold occurrences within the given window. Isolated optimistic
// concurrency retries are normal in Kubernetes controllers and are filtered out.
func filterConflictBursts(lines []string, threshold int, window time.Duration) []string {
	type entry struct {
		ts   time.Time
		line string
	}
	perResource := make(map[string][]entry)

	for _, line := range lines {
		if !strings.Contains(line, "the object has been modified") {
			continue
		}
		ts := parseLogTimestamp(line)
		res := parseLogResourceKey(line)
		perResource[res] = append(perResource[res], entry{ts: ts, line: line})
	}

	var flagged []string
	for _, entries := range perResource {
		for i := range entries {
			count := 0
			for j := i; j < len(entries) && entries[j].ts.Sub(entries[i].ts) <= window; j++ {
				count++
			}
			if count >= threshold {
				for j := i; j < len(entries) && entries[j].ts.Sub(entries[i].ts) <= window; j++ {
					flagged = append(flagged, entries[j].line)
				}
				break
			}
		}
	}
	return flagged
}

func parseLogTimestamp(line string) time.Time {
	if len(line) < 20 {
		return time.Time{}
	}
	// Controller log format: "2026-07-08T21:34:13Z\t..."
	ts, err := time.Parse(time.RFC3339, strings.Fields(line)[0])
	if err != nil {
		return time.Time{}
	}
	return ts
}

func parseLogResourceKey(line string) string {
	// Extract "controller"+"name" from structured JSON log.
	// e.g. "controller": "drplan", ... "name": "dj-app"
	ctrl := extractJSONField(line, `"controller"`)
	name := extractJSONField(line, `"name"`)
	return ctrl + "/" + name
}

func extractJSONField(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(key):]
	// skip `: "`
	qStart := strings.Index(rest, `"`)
	if qStart < 0 {
		return ""
	}
	rest = rest[qStart+1:]
	qEnd := strings.Index(rest, `"`)
	if qEnd < 0 {
		return ""
	}
	return rest[:qEnd]
}

func scanControllerLogsOnBothSites(ctx context.Context) {
	eastLogs := captureControllerLogs(ctx, eastClientset, "soteria")
	if eastLogs != "" {
		scanLogsForErrors(eastLogs)
	}
	westLogs := captureControllerLogs(ctx, westClientset, "soteria")
	if westLogs != "" {
		scanLogsForErrors(westLogs)
	}
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func activeClient(eastCl, westCl client.Client, activeSite string) client.Client {
	if activeSite == "west" {
		return westCl
	}
	return eastCl
}

func deleteIfExists(ctx context.Context, cl client.Client, obj client.Object) {
	err := cl.Delete(ctx, obj)
	if err != nil && !errors.IsNotFound(err) {
		GinkgoWriter.Printf("  [warn] delete %T %s: %v\n",
			obj, obj.GetName(), err)
	}
}

func cleanupShadowPVs(ctx context.Context, cl client.Client, planName string) {
	var list soteriav1alpha1.ShadowPVList
	if err := cl.List(ctx, &list, client.MatchingLabels{
		soteriav1alpha1.DRPlanLabel: planName,
	}); err != nil {
		return
	}
	for i := range list.Items {
		deleteIfExists(ctx, cl, &list.Items[i])
	}
}

func cleanupShadowPVConsumerPVs(ctx context.Context, cl client.Client, planName string) {
	var pvList corev1.PersistentVolumeList
	if err := cl.List(ctx, &pvList); err != nil {
		return
	}
	for i := range pvList.Items {
		pv := &pvList.Items[i]
		if pv.Labels[soteriav1alpha1.DRPlanLabel] != planName {
			continue
		}
		if pv.Labels["soteria.io/shadowpv-consumer"] == "" {
			continue
		}
		deleteIfExists(ctx, cl, pv)
	}
}

func waitForConditionsHealthy(
	ctx context.Context, cl client.Client, planName string, timeout time.Duration,
) {
	Eventually(func(g Gomega) {
		var plan soteriav1alpha1.DRPlan
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: planName}, &plan)).To(Succeed())
		for _, ct := range []string{
			"Ready", "SitesInSync", "DisksConsistent", "ReplicationHealthy",
		} {
			cond := meta.FindStatusCondition(plan.Status.Conditions, ct)
			g.Expect(cond).NotTo(BeNil(), "condition %s not found", ct)
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue),
				"condition %s: got %s, want True", ct, cond.Status)
		}
	}).WithTimeout(timeout).WithPolling(5 * time.Second).Should(Succeed())
}
