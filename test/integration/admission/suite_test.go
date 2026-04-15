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

// suite_test.go boots an envtest environment with DRPlan and VirtualMachine
// CRDs, starts a controller-runtime manager with the DRPlan validating webhook
// registered, and exposes a test client for admission integration tests.

package admission_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	soteriaadmission "github.com/soteria-project/soteria/pkg/admission"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

var (
	testClient client.Client
	testCfg    *rest.Config
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
	_ = admissionregistrationv1.AddToScheme(testScheme)

	drplanWebhookPath := soteriaadmission.ValidateDRPlanPath
	vmWebhookPath := soteriaadmission.ValidateVMPath
	fail := admissionregistrationv1.Fail
	sideEffects := admissionregistrationv1.SideEffectClassNone
	equivalent := admissionregistrationv1.Equivalent

	testEnv = &envtest.Environment{
		Scheme: testScheme,
		CRDInstallOptions: envtest.CRDInstallOptions{
			CRDs: []*apiextensionsv1.CustomResourceDefinition{
				drplanCRD(),
				virtualMachineCRD(),
			},
		},
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			ValidatingWebhooks: []*admissionregistrationv1.ValidatingWebhookConfiguration{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "soteria-validating-webhook-configuration"},
					Webhooks: []admissionregistrationv1.ValidatingWebhook{
						{
							Name:                    "vdrplan.kb.io",
							AdmissionReviewVersions: []string{"v1"},
							FailurePolicy:           &fail,
							MatchPolicy:             &equivalent,
							SideEffects:             &sideEffects,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Path: &drplanWebhookPath,
								},
							},
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.Create,
										admissionregistrationv1.Update,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{"soteria.io"},
										APIVersions: []string{"v1alpha1"},
										Resources:   []string{"drplans"},
									},
								},
							},
						},
						{
							Name:                    "vvm.kb.io",
							AdmissionReviewVersions: []string{"v1"},
							FailurePolicy:           &fail,
							MatchPolicy:             &equivalent,
							SideEffects:             &sideEffects,
							ClientConfig: admissionregistrationv1.WebhookClientConfig{
								Service: &admissionregistrationv1.ServiceReference{
									Path: &vmWebhookPath,
								},
							},
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.Create,
										admissionregistrationv1.Update,
									},
									Rule: admissionregistrationv1.Rule{
										APIGroups:   []string{"kubevirt.io"},
										APIVersions: []string{"v1"},
										Resources:   []string{"virtualmachines"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg, err := testEnv.Start()
	if err != nil {
		panic(fmt.Sprintf("starting envtest: %v", err))
	}
	testCfg = cfg

	webhookInstallOpts := &testEnv.WebhookInstallOptions
	webhookOpts := webhook.Options{
		Host:    webhookInstallOpts.LocalServingHost,
		Port:    webhookInstallOpts.LocalServingPort,
		CertDir: webhookInstallOpts.LocalServingCertDir,
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:        testScheme,
		WebhookServer: webhook.NewServer(webhookOpts),
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

	if err := soteriaadmission.SetupDRPlanWebhook(mgr); err != nil {
		panic(fmt.Sprintf("setting up DRPlan webhook: %v", err))
	}

	if err := soteriaadmission.SetupVMWebhook(mgr, nsLookup, vmDiscoverer); err != nil {
		panic(fmt.Sprintf("setting up VM webhook: %v", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelFunc = cancel

	go func() {
		if err := mgr.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "manager error: %v\n", err)
		}
	}()

	testClient = mgr.GetClient()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		panic("cache sync failed")
	}

	// Wait for webhook server to be ready
	time.Sleep(2 * time.Second)

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
				Plural:     "virtualmachines",
				Singular:   "virtualmachine",
				Kind:       "VirtualMachine",
				ListKind:   "VirtualMachineList",
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
