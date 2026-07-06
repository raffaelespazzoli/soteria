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
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// ---------------------------------------------------------------------------
// Scenario definition
// ---------------------------------------------------------------------------

type lifecycleScenario struct {
	name                   string
	failoverMode           soteriav1alpha1.ExecutionMode
	volumeReplicationClass string
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

	By("creating east data PVCs")
	for _, vm := range baseVMs {
		createDataPVC(ctx, eastClient, s.vmPrefix+vm.name)
	}

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
	pvNames := waitForShadowPVConsumerPVs(ctx, westClient, planName)

	By("creating west PVCs bound to ShadowPV-provisioned PVs")
	createWestPVCsFromShadowPVs(ctx, westClient, s.vmPrefix, pvNames)

	By("waiting for DRPlan to reach healthy state")
	waitForDRPlanHealthy(ctx, eastClient, planName)

	By("waiting for east VMs to reach Running state")
	waitForVMsRunning(ctx, eastClient, s.vmPrefix)
}

//nolint:unparam
func teardownScenario(ctx context.Context, s *lifecycleScenario) {
	planName := s.planName()

	for _, vm := range baseVMs {
		deleteIfExists(ctx, eastClient, &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name, Namespace: testNamespace},
		})
		deleteIfExists(ctx, westClient, &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name, Namespace: testNamespace},
		})
		deleteIfExists(ctx, eastClient, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name + "-data", Namespace: testNamespace},
		})
		deleteIfExists(ctx, westClient, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: s.vmPrefix + vm.name + "-data", Namespace: testNamespace},
		})
	}

	deleteIfExists(ctx, eastClient, &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: planName},
	})

	cleanupShadowPVConsumerPVs(ctx, westClient, planName)
	cleanupShadowPVs(ctx, eastClient, planName)
	cleanupShadowPVs(ctx, westClient, planName)
}

// ---------------------------------------------------------------------------
// Resource factories
// ---------------------------------------------------------------------------

func createDataPVC(ctx context.Context, cl client.Client, vmName string) {
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
	err := cl.Create(ctx, pvc)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "creating PVC %s", pvc.Name)
	}
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
									Name: "bootdisk",
									DiskDevice: kubevirtv1.DiskDevice{
										Disk: &kubevirtv1.DiskTarget{Bus: kubevirtv1.DiskBusVirtio},
									},
								},
								{
									Name: "datadisk",
									DiskDevice: kubevirtv1.DiskDevice{
										Disk: &kubevirtv1.DiskTarget{Bus: kubevirtv1.DiskBusVirtio},
									},
								},
							},
						},
					},
					Volumes: []kubevirtv1.Volume{
						{
							Name: "bootdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								ContainerDisk: &kubevirtv1.ContainerDiskSource{
									Image: "quay.io/kubevirt/cirros-container-disk-demo:latest",
								},
							},
						},
						{
							Name: "datadisk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: name + "-data",
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

// waitForShadowPVConsumerPVs polls west for PVs created by the ShadowPV consumer
// for the current plan. Returns a map of VM data PVC name -> PV name for binding.
func waitForShadowPVConsumerPVs(
	ctx context.Context, cl client.Client, planName string,
) map[string]string {
	pvMap := make(map[string]string)
	Eventually(func(g Gomega) {
		var pvList corev1.PersistentVolumeList
		g.Expect(cl.List(ctx, &pvList)).To(Succeed())
		pvMap = make(map[string]string)
		for _, pv := range pvList.Items {
			if pv.Labels[soteriav1alpha1.DRPlanLabel] != planName {
				continue
			}
			if pv.Labels["soteria.io/shadowpv-consumer"] == "" {
				continue
			}
			if pv.Spec.ClaimRef != nil {
				pvMap[pv.Spec.ClaimRef.Name] = pv.Name
			} else {
				pvMap[pv.Name] = pv.Name
			}
		}
		g.Expect(pvMap).NotTo(BeEmpty(),
			"ShadowPV consumer PVs should exist on west for plan %s", planName)
	}).WithTimeout(shadowPVTimeout).WithPolling(5 * time.Second).Should(Succeed())
	return pvMap
}

func createWestPVCsFromShadowPVs(
	ctx context.Context, cl client.Client, vmPrefix string, pvMap map[string]string,
) {
	for _, vm := range baseVMs {
		pvcName := vmPrefix + vm.name + "-data"
		pvName, found := pvMap[pvcName]
		if !found {
			for k, v := range pvMap {
				if strings.Contains(k, vm.name) {
					pvName = v
					found = true
					break
				}
			}
		}
		Expect(found).To(BeTrue(),
			"ShadowPV consumer PV for %s not found in PV map", pvcName)

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				VolumeName:  pvName,
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
				"creating west PVC %s bound to PV %s", pvcName, pvName)
		}
	}
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

func waitForAPIServer(ctx context.Context, cl client.Client) {
	Eventually(func() error {
		var nsList corev1.NamespaceList
		return cl.List(ctx, &nsList)
	}).WithTimeout(clusterRestartTimeout).WithPolling(5*time.Second).Should(Succeed(),
		"API server did not become ready within timeout")
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
	var plan soteriav1alpha1.DRPlan
	ExpectWithOffset(1, cl.Get(ctx, client.ObjectKey{Name: planName}, &plan)).To(Succeed())
	for _, ct := range []string{
		"Ready", "SitesInSync", "DisksConsistent", "ReplicationHealthy",
	} {
		cond := meta.FindStatusCondition(plan.Status.Conditions, ct)
		ExpectWithOffset(1, cond).NotTo(BeNil(),
			"DRPlan %s missing condition %s", planName, ct)
		ExpectWithOffset(1, cond.Status).To(Equal(metav1.ConditionTrue),
			"DRPlan %s condition %s: got %s, want True", planName, ct, cond.Status)
	}
}

func assertVRState(
	ctx context.Context, cl client.Client, ns, planName, expectedState string,
) {
	var vrList replicationv1alpha1.VolumeReplicationList
	ExpectWithOffset(1, cl.List(ctx, &vrList,
		client.InNamespace(ns),
		client.MatchingLabels{soteriav1alpha1.DRPlanLabel: planName},
	)).To(Succeed())
	ExpectWithOffset(1, vrList.Items).NotTo(BeEmpty(), "VR CRs should exist in %s", ns)
	for _, vr := range vrList.Items {
		ExpectWithOffset(1, string(vr.Spec.ReplicationState)).To(Equal(expectedState),
			"VR %s/%s should be %s", ns, vr.Name, expectedState)
	}
}

func assertVMRunState(
	ctx context.Context, cl client.Client, ns, planName string, expectRunning bool,
) {
	var vmList kubevirtv1.VirtualMachineList
	ExpectWithOffset(1, cl.List(ctx, &vmList,
		client.InNamespace(ns),
		client.MatchingLabels{soteriav1alpha1.DRPlanLabel: planName},
	)).To(Succeed())
	ExpectWithOffset(1, vmList.Items).NotTo(BeEmpty(), "VMs should exist in %s", ns)

	for _, vm := range vmList.Items {
		if expectRunning {
			ExpectWithOffset(1, ptr.Deref(vm.Spec.RunStrategy, "")).To(
				Equal(kubevirtv1.RunStrategyAlways),
				"VM %s should have runStrategy Always", vm.Name)
			var vmi kubevirtv1.VirtualMachineInstance
			ExpectWithOffset(1, cl.Get(ctx, client.ObjectKey{
				Name: vm.Name, Namespace: ns,
			}, &vmi)).To(Succeed(), "VMI %s should exist", vm.Name)
			ExpectWithOffset(1, vmi.Status.Phase).To(Equal(kubevirtv1.Running),
				"VMI %s should be Running", vm.Name)
		} else {
			ExpectWithOffset(1, ptr.Deref(vm.Spec.RunStrategy, "")).To(
				Equal(kubevirtv1.RunStrategyHalted),
				"VM %s should have runStrategy Halted", vm.Name)
		}
	}
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

func observeResyncPending(
	ctx context.Context, cl client.Client, execName string, timeout time.Duration,
) {
	resyncSeen := false
	Eventually(func(g Gomega) {
		var exec soteriav1alpha1.DRExecution
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())
		cond := meta.FindStatusCondition(exec.Status.Conditions, "ResyncPending")
		if cond != nil && cond.Status == metav1.ConditionTrue {
			resyncSeen = true
		}
		g.Expect(resyncSeen || exec.Status.Result != "").To(BeTrue(),
			"waiting for ResyncPending or execution completion")
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())

	if !resyncSeen {
		GinkgoWriter.Printf("  [info] ResyncPending resolved too fast to observe\n")
		return
	}

	Eventually(func(g Gomega) {
		var exec soteriav1alpha1.DRExecution
		g.Expect(cl.Get(ctx, client.ObjectKey{Name: execName}, &exec)).To(Succeed())
		cond := meta.FindStatusCondition(exec.Status.Conditions, "ResyncPending")
		g.Expect(cond == nil || cond.Status == metav1.ConditionFalse).To(BeTrue(),
			"ResyncPending should resolve")
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
	var vrList replicationv1alpha1.VolumeReplicationList
	ExpectWithOffset(1, cl.List(ctx, &vrList,
		client.InNamespace(ns),
		client.MatchingLabels{soteriav1alpha1.DRPlanLabel: planName},
	)).To(Succeed())
	ExpectWithOffset(1, vrList.Items).NotTo(BeEmpty())

	hasNonZeroSyncTime := false
	for _, vr := range vrList.Items {
		if vr.Status.LastSyncTime != nil && !vr.Status.LastSyncTime.IsZero() {
			hasNonZeroSyncTime = true
			GinkgoWriter.Printf("  [storage] VR %s lastSyncTime: %v\n",
				vr.Name, vr.Status.LastSyncTime.Time)
			break
		}
	}
	ExpectWithOffset(1, hasNonZeroSyncTime).To(BeTrue(),
		fmt.Sprintf("at least one VR in %s should have non-zero lastSyncTime", ns))
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
	var conflicts, violations []string
	for _, line := range lines {
		if strings.Contains(line, "the object has been modified") {
			conflicts = append(conflicts, line)
		}
		if strings.Contains(line, "is immutable after completion") {
			violations = append(violations, line)
		}
	}
	ExpectWithOffset(1, conflicts).To(BeEmpty(),
		"checkpoint conflicts in controller logs:\n%s",
		strings.Join(conflicts, "\n"))
	ExpectWithOffset(1, violations).To(BeEmpty(),
		"immutability violations in controller logs:\n%s",
		strings.Join(violations, "\n"))
}

func scanControllerLogsOnBothSites(ctx context.Context) {
	eastLogs := captureControllerLogs(ctx, eastClientset, "soteria-system")
	if eastLogs != "" {
		scanLogsForErrors(eastLogs)
	}
	westLogs := captureControllerLogs(ctx, westClientset, "soteria-system")
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
