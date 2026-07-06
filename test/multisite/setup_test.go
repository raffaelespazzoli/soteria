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
	"os"
	"time"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

var _ = BeforeSuite(func() {
	testNamespace = envOrDefault("DR_TEST_NS", "soteria-dr-test")
	transitionTimeout = parseDurationOrDefault("TRANSITION_TIMEOUT", 5*time.Minute)
	setupTimeout = parseDurationOrDefault("SETUP_TIMEOUT", 10*time.Minute)
	shadowPVTimeout = parseDurationOrDefault("SHADOWPV_TIMEOUT", 2*time.Minute)
	disasterRecoveryTimeout = parseDurationOrDefault("DISASTER_RECOVERY_TIMEOUT", 3*time.Minute)
	clusterRestartTimeout = parseDurationOrDefault("CLUSTER_RESTART_TIMEOUT", 3*time.Minute)
	eastMinikubeProfile = envOrDefault("EAST_MINIKUBE_PROFILE", "east")
	westMinikubeProfile = envOrDefault("WEST_MINIKUBE_PROFILE", "west")

	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(soteriav1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(kubevirtv1.AddToScheme(scheme)).To(Succeed())
	Expect(replicationv1alpha1.AddToScheme(scheme)).To(Succeed())

	eastKubeconfig := envOrDefault("EAST_KUBECONFIG", os.Getenv("HOME")+"/.kube/config")
	westKubeconfig := envOrDefault("WEST_KUBECONFIG", os.Getenv("HOME")+"/.kube/config")
	eastContext := envOrDefault("EAST_CONTEXT", "east")
	westContext := envOrDefault("WEST_CONTEXT", "west")

	eastCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: eastKubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: eastContext},
	).ClientConfig()
	Expect(err).NotTo(HaveOccurred(), "building east cluster config")

	eastClient, err = client.New(eastCfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred(), "creating east client")

	eastClientset, err = kubernetes.NewForConfig(eastCfg)
	Expect(err).NotTo(HaveOccurred(), "creating east clientset")

	westCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: westKubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: westContext},
	).ClientConfig()
	Expect(err).NotTo(HaveOccurred(), "building west cluster config")

	westClient, err = client.New(westCfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred(), "creating west client")

	westClientset, err = kubernetes.NewForConfig(westCfg)
	Expect(err).NotTo(HaveOccurred(), "creating west clientset")

	createNamespaceIfNeeded(eastClient)
	createNamespaceIfNeeded(westClient)
})

var _ = AfterSuite(func() {
	deleteNamespaceIfExists(eastClient)
	deleteNamespaceIfExists(westClient)
})

func createNamespaceIfNeeded(cl client.Client) {
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
	}
	err := cl.Create(ctx, ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		By("warning: failed to create namespace " + testNamespace)
	}
}

func deleteNamespaceIfExists(cl client.Client) {
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
	}
	err := cl.Delete(ctx, ns)
	if err != nil && !errors.IsNotFound(err) {
		GinkgoWriter.Printf("  [warn] failed to delete namespace %s: %v\n", testNamespace, err)
	}
}
