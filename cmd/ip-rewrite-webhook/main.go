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

// ip-rewrite-webhook is a standalone mutating admission webhook server that
// intercepts virt-launcher pod creation and injects an IP rewrite init
// container when the appropriate label and annotations are present.
//
// This binary is independent of the main Soteria controller manager. It
// requires only controller-runtime for the webhook server — no ScyllaDB,
// aggregated API server, or Soteria-specific CRDs.
package main

import (
	"flag"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/soteria-project/soteria/internal/webhook/iprewrite"
)

func main() {
	var certDir string
	var port int
	var initContainerImage string

	flag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs",
		"Directory containing the TLS certificate and key (tls.crt, tls.key)")
	flag.IntVar(&port, "port", 9443, "Webhook server port")
	flag.StringVar(&initContainerImage, "init-container-image",
		"quay.io/raffaelespazzoli/soteria-ip-rewrite:latest",
		"Image for the IP rewrite init container")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    port,
			CertDir: certDir,
		}),
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		setupLog.Error(err, "Failed to create manager")
		os.Exit(1)
	}

	handler := &iprewrite.Handler{
		InitContainerImage: initContainerImage,
	}

	mgr.GetWebhookServer().Register(iprewrite.MutatePodPath,
		&webhook.Admission{Handler: handler})

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting IP rewrite webhook server",
		"port", port,
		"certDir", certDir,
		"initContainerImage", initContainerImage)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run webhook server")
		os.Exit(1)
	}
}
