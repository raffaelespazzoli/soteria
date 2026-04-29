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

// suite_test.go boots an envtest environment with kubevirt VM CRDs and DRPlan
// CRDs, starts a controller-runtime manager with the DRPlan reconciler, and
// exposes a test client for integration tests. DRPlan is normally served by the
// aggregated API server backed by ScyllaDB; here we register it as a CRD in
// envtest so the full controller-runtime watch/reconcile machinery is exercised
// without requiring ScyllaDB — the controller logic is storage-backend agnostic.

package controller_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/soteria-project/soteria/internal/preflight"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/controller/drexecution"
	"github.com/soteria-project/soteria/pkg/controller/drplan"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/noop"
	"github.com/soteria-project/soteria/pkg/engine"
)

var (
	testClient    client.Client
	testScheme    *runtime.Scheme
	testEnv       *envtest.Environment
	testClientset *kubernetes.Clientset
	cancelFunc    context.CancelFunc
)

func TestMain(m *testing.M) {
	ctrl.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	testScheme = runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(testScheme)
	_ = soteriav1alpha1.AddToScheme(testScheme)
	_ = kubevirtv1.AddToScheme(testScheme)
	_ = apiextensionsv1.AddToScheme(testScheme)

	testEnv = &envtest.Environment{
		Scheme: testScheme,
		CRDInstallOptions: envtest.CRDInstallOptions{
			CRDs: []*apiextensionsv1.CustomResourceDefinition{
				drplanCRD(),
				drexecutionCRD(),
				virtualMachineCRD(),
			},
		},
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
	}

	cfg, err := testEnv.Start()
	if err != nil {
		panic(fmt.Sprintf("starting envtest: %v", err))
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testScheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		panic(fmt.Sprintf("creating manager: %v", err))
	}

	vmDiscoverer := engine.NewTypedVMDiscoverer(mgr.GetClient())

	testClientset, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("creating kubernetes clientset: %v", err))
	}
	clientset := testClientset
	nsLookup := &engine.DefaultNamespaceLookup{Client: clientset.CoreV1()}

	testReg := newNoopRegistry()
	testRegistry := testReg
	scLister := &preflight.KubeStorageClassLister{Client: clientset.StorageV1()}
	storageResolver := &preflight.TypedStorageBackendResolver{
		Client:     mgr.GetClient(),
		CoreClient: clientset.CoreV1(),
		Registry:   testRegistry,
		SCLister:   scLister,
	}

	ctx, cancel := context.WithCancel(context.Background())

	eventBroadcaster := events.NewEventBroadcasterAdapterWithContext(ctx, clientset)
	eventBroadcaster.StartRecordingToSink(ctx.Done())
	eventRecorder := eventBroadcaster.NewRecorder("drplan-controller")

	testPVCResolver := engine.NoOpPVCResolver{}
	if err := (&drplan.DRPlanReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		VMDiscoverer:            vmDiscoverer,
		NamespaceLookup:         nsLookup,
		StorageResolver:         storageResolver,
		Recorder:                eventRecorder,
		Registry:                testRegistry,
		SCLister:                scLister,
		PVCResolver:             testPVCResolver,
	}).SetupWithManager(mgr); err != nil {
		panic(fmt.Sprintf("setting up DRPlan controller: %v", err))
	}

	drexecRecorder := eventBroadcaster.NewRecorder("drexecution-controller")
	waveExecutor := &engine.WaveExecutor{
		Client:          mgr.GetClient(),
		CoreClient:      clientset.CoreV1(),
		VMDiscoverer:    vmDiscoverer,
		NamespaceLookup: nsLookup,
		Registry:        testRegistry,
		SCLister:        scLister,
	}
	if err := (&drexecution.DRExecutionReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		Recorder:        drexecRecorder,
		WaveExecutor:    waveExecutor,
		Handler:         &engine.NoOpHandler{},
		ResumeAnalyzer:  &engine.ResumeAnalyzer{},
	}).SetupWithManager(mgr); err != nil {
		panic(fmt.Sprintf("setting up DRExecution controller: %v", err))
	}
	cancelFunc = cancel

	go func() {
		if err := mgr.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "manager error: %v\n", err)
		}
	}()

	testClient = mgr.GetClient()

	// Wait for caches to sync
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		panic("cache sync failed")
	}

	// Create StorageClass objects so KubeStorageClassLister can resolve
	// storage class → CSI provisioner for integration test PVCs.
	for _, sc := range []storagev1.StorageClass{
		{
			ObjectMeta:  metav1.ObjectMeta{Name: "test-odf"},
			Provisioner: noop.ProvisionerName,
		},
		{
			ObjectMeta:  metav1.ObjectMeta{Name: "noop-storage"},
			Provisioner: noop.ProvisionerName,
		},
	} {
		if _, err := clientset.StorageV1().StorageClasses().Create(ctx, &sc, metav1.CreateOptions{}); err != nil {
			panic(fmt.Sprintf("creating StorageClass %q: %v", sc.Name, err))
		}
	}

	exitCode := m.Run()

	cancelFunc()
	eventBroadcaster.Shutdown()
	if err := testEnv.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "stopping envtest: %v\n", err)
	}
	os.Exit(exitCode)
}

func drplanCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "drplans.soteria.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "soteria.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "drplans",
				Singular: "drplan",
				Kind:     "DRPlan",
				ListKind: "DRPlanList",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {
								Type:                   "object",
								XPreserveUnknownFields: boolPtr(true),
							},
							"status": {
								Type:                   "object",
								XPreserveUnknownFields: boolPtr(true),
							},
						},
					},
				},
			}},
		},
	}
}

func drexecutionCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "drexecutions.soteria.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "soteria.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "drexecutions",
				Singular: "drexecution",
				Kind:     "DRExecution",
				ListKind: "DRExecutionList",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {
								Type:                   "object",
								XPreserveUnknownFields: boolPtr(true),
							},
							"status": {
								Type:                   "object",
								XPreserveUnknownFields: boolPtr(true),
							},
						},
					},
				},
			}},
		},
	}
}

func virtualMachineCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "virtualmachines.kubevirt.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "kubevirt.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "virtualmachines",
				Singular: "virtualmachine",
				Kind:     "VirtualMachine",
				ListKind: "VirtualMachineList",
				ShortNames: []string{"vm", "vms"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {
								Type:                   "object",
								XPreserveUnknownFields: boolPtr(true),
							},
							"status": {
								Type:                   "object",
								XPreserveUnknownFields: boolPtr(true),
							},
						},
					},
				},
			}},
		},
	}
}

func boolPtr(b bool) *bool { return &b }

// waitForCondition polls until the DRPlan has the given condition or timeout.
func waitForCondition(ctx context.Context, name, namespace, condType string, status metav1.ConditionStatus, timeout time.Duration) (*soteriav1alpha1.DRPlan, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, c := range plan.Status.Conditions {
			if c.Type == condType && c.Status == status {
				return &plan, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for condition %s=%s on %s/%s", condType, status, namespace, name)
}

// waitForConditionReason polls until the DRPlan has the given condition reason.
func waitForConditionReason(ctx context.Context, name, namespace, condType, reason string, timeout time.Duration) (*soteriav1alpha1.DRPlan, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, c := range plan.Status.Conditions {
			if c.Type == condType && c.Reason == reason {
				return &plan, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for condition %s reason=%s on %s/%s", condType, reason, namespace, name)
}

// waitForWaveGroups polls until the DRPlan has groups populated in at least one wave.
func waitForWaveGroups(ctx context.Context, name, namespace string, timeout time.Duration) (*soteriav1alpha1.DRPlan, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, w := range plan.Status.Waves {
			if len(w.Groups) > 0 {
				return &plan, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for wave groups on %s/%s", namespace, name)
}

// waitForVMCount polls until the DRPlan's DiscoveredVMCount matches.
func waitForVMCount(ctx context.Context, name, namespace string, count int, timeout time.Duration) (*soteriav1alpha1.DRPlan, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if plan.Status.DiscoveredVMCount == count {
			return &plan, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for DiscoveredVMCount=%d on %s/%s", count, namespace, name)
}

// newTestDRPlan constructs a DRPlan with the given site topology, reducing
// boilerplate across integration tests that only need a valid plan skeleton.
func newTestDRPlan(name, primarySite, secondarySite string) *soteriav1alpha1.DRPlan {
	return &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: soteriav1alpha1.DRPlanSpec{
			PrimarySite:            primarySite,
			SecondarySite:          secondarySite,
			WaveLabel:              "soteria.io/wave",
			MaxConcurrentFailovers: 5,
		},
	}
}

// setPlanPhase sets the DRPlan's status.phase with a retry loop to handle
// conflicts from concurrent DRPlan controller reconciliation and cache lag.
func setPlanPhase(ctx context.Context, name, phase string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		plan.Status.Phase = phase
		plan.Status.ActiveSite = engine.ActiveSiteForPhase(phase, plan.Spec.PrimarySite, plan.Spec.SecondarySite)
		if err := testClient.Status().Update(ctx, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return nil
	}
	return fmt.Errorf("timed out retrying setPlanPhase(%s, %s)", name, phase)
}

// waitForExecStartTime polls until the DRExecution has a non-nil startTime.
func waitForExecStartTime(ctx context.Context, name string, timeout time.Duration) (*soteriav1alpha1.DRExecution, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var exec soteriav1alpha1.DRExecution
		if err := testClient.Get(ctx, client.ObjectKey{Name: name}, &exec); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if exec.Status.StartTime != nil {
			return &exec, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for startTime on DRExecution %q", name)
}

// waitForExecResult polls until the DRExecution has the given result.
func waitForExecResult(ctx context.Context, name string, result soteriav1alpha1.ExecutionResult, timeout time.Duration) (*soteriav1alpha1.DRExecution, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var exec soteriav1alpha1.DRExecution
		if err := testClient.Get(ctx, client.ObjectKey{Name: name}, &exec); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if exec.Status.Result == result {
			return &exec, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for result=%s on DRExecution %q", result, name)
}

// waitForPlanPhase polls until the DRPlan has the given phase.
func waitForPlanPhase(ctx context.Context, name, phase string, timeout time.Duration) (*soteriav1alpha1.DRPlan, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if plan.Status.Phase == phase {
			return &plan, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for phase=%s on DRPlan %q", phase, name)
}

// waitForPreflight polls until the DRPlan has a populated preflight report.
func waitForPreflight(ctx context.Context, name, namespace string, timeout time.Duration) (*soteriav1alpha1.DRPlan, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var plan soteriav1alpha1.DRPlan
		if err := testClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &plan); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if plan.Status.Preflight != nil && plan.Status.Preflight.TotalVMs > 0 {
			return &plan, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for preflight report on %s/%s", namespace, name)
}

// newNoopRegistry creates a driver registry with only the noop driver
// registered and set as the fallback. Reusable across suite setup and
// individual test reconcilers.
func newNoopRegistry() *drivers.Registry {
	reg := drivers.NewRegistry()
	reg.RegisterDriver(noop.ProvisionerName, func() drivers.StorageProvider { return noop.New() })
	reg.SetFallbackDriver(func() drivers.StorageProvider { return noop.New() })
	return reg
}
