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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/controller/drplan"
	"github.com/soteria-project/soteria/pkg/engine"
)

var (
	testClient client.Client
	testScheme *runtime.Scheme
	testEnv    *envtest.Environment
	cancelFunc context.CancelFunc
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

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("creating kubernetes clientset: %v", err))
	}
	nsLookup := &engine.DefaultNamespaceLookup{Client: clientset.CoreV1()}

	if err := (&drplan.DRPlanReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		VMDiscoverer:    vmDiscoverer,
		NamespaceLookup: nsLookup,
		Recorder:        mgr.GetEventRecorderFor("drplan-controller"),
	}).SetupWithManager(mgr); err != nil {
		panic(fmt.Sprintf("setting up DRPlan controller: %v", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
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

	exitCode := m.Run()

	cancelFunc()
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
			Scope: apiextensionsv1.NamespaceScoped,
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
