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
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	eastClient    client.Client
	westClient    client.Client
	eastClientset *kubernetes.Clientset
	westClientset *kubernetes.Clientset

	testNamespace string

	// Timeouts
	transitionTimeout       time.Duration
	setupTimeout            time.Duration
	shadowPVTimeout         time.Duration
	disasterRecoveryTimeout time.Duration
	clusterRestartTimeout   time.Duration

	// Minikube profiles for disaster simulation
	eastMinikubeProfile string
	westMinikubeProfile string
)

func TestMultisite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multisite Integration Suite")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDurationOrDefault(envKey string, fallback time.Duration) time.Duration {
	if v := os.Getenv(envKey); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return fallback
}
