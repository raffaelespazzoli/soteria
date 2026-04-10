/*
Copyright 2026.

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

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"strings"

	"github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"k8s.io/client-go/kubernetes"
	kubevirtv1 "kubevirt.io/api/core/v1"

	soteriaadmission "github.com/soteria-project/soteria/pkg/admission"
	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	"github.com/soteria-project/soteria/pkg/apiserver"
	"github.com/soteria-project/soteria/pkg/controller/drplan"
	"github.com/soteria-project/soteria/pkg/engine"
	scylladb "github.com/soteria-project/soteria/pkg/storage/scylladb"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kubevirtv1.AddToScheme(scheme))
	soteriainstall.Install(scheme)

	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	var enableAPIServer bool

	// Controller-runtime flags (stdlib flag)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	// Aggregated API server flags (pflag) — includes secure serving,
	// delegated authn/authz, ScyllaDB connection, audit, and admission.
	serverOpts := apiserver.NewSoteriaServerOptions()
	serverOpts.AddFlags(pflag.CommandLine)
	pflag.CommandLine.BoolVar(&enableAPIServer, "enable-apiserver", true,
		"Enable the aggregated API server component")

	// Bridge stdlib flags into pflag and parse everything once.
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	ctx := ctrl.SetupSignalHandler()

	// Initialize ScyllaDB and start the aggregated API server
	var scyllaClient *scylladb.Client
	if enableAPIServer {
		contactPoints := strings.Split(serverOpts.ScyllaDBContactPoints, ",")
		var err error
		scyllaClient, err = scylladb.NewClient(scylladb.ClientConfig{
			ContactPoints: contactPoints,
			Keyspace:      serverOpts.ScyllaDBKeyspace,
			Datacenter:    serverOpts.ScyllaDBLocalDC,
			CertPath:      serverOpts.ScyllaDBTLSCert,
			KeyPath:       serverOpts.ScyllaDBTLSKey,
			CAPath:        serverOpts.ScyllaDBTLSCA,
		})
		if err != nil {
			setupLog.Error(err, "Failed to connect to ScyllaDB")
			os.Exit(1)
		}
		defer scyllaClient.Close()

		if serverOpts.ScyllaDBLocalDC != "" {
			if err := scylladb.ValidateKeyspaceTopology(
				scyllaClient.Session(), serverOpts.ScyllaDBKeyspace, serverOpts.ScyllaDBLocalDC,
			); err != nil {
				setupLog.Error(err, "Multi-DC keyspace validation failed")
				os.Exit(1)
			}
			setupLog.Info("Multi-DC keyspace validated",
				"localDC", serverOpts.ScyllaDBLocalDC)
		} else {
			schemaCfg := scylladb.SchemaConfig{
				Keyspace:          serverOpts.ScyllaDBKeyspace,
				Strategy:          "SimpleStrategy",
				ReplicationFactor: 1,
			}
			if err := scylladb.EnsureSchema(scyllaClient.Session(), schemaCfg); err != nil {
				setupLog.Error(err, "Failed to ensure ScyllaDB schema")
				os.Exit(1)
			}
		}

		setupLog.Info("ScyllaDB connected and schema initialized",
			"contactPoints", contactPoints, "keyspace", serverOpts.ScyllaDBKeyspace,
			"localDC", serverOpts.ScyllaDBLocalDC)

		codec := soteriainstall.Codecs.LegacyCodec(
			soteriainstall.Scheme.PrioritizedVersionsForGroup("soteria.io")...,
		)

		serverConfig, err := serverOpts.Config()
		if err != nil {
			setupLog.Error(err, "Failed to build API server config")
			os.Exit(1)
		}

		serverConfig.ScyllaStoreFactory = &apiserver.ScyllaStoreFactory{
			StoreConfig: scylladb.StoreConfig{
				Session:  scyllaClient.Session(),
				Codec:    codec,
				Keyspace: serverOpts.ScyllaDBKeyspace,
			},
			Codec:                  codec,
			UseCacher:              true,
			CriticalFieldDetectors: apiserver.DefaultCriticalFieldDetectors(),
		}

		completed := serverConfig.Complete()
		server, err := completed.New()
		if err != nil {
			setupLog.Error(err, "Failed to create API server")
			os.Exit(1)
		}

		go func() {
			setupLog.Info("Starting API server")
			if err := server.GenericAPIServer.PrepareRun().RunWithContext(ctx); err != nil {
				setupLog.Error(err, "API server failed")
				os.Exit(1)
			}
		}()
	}

	restConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "soteria-controller",
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	vmDiscoverer := engine.NewTypedVMDiscoverer(mgr.GetClient())

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		setupLog.Error(err, "Failed to create Kubernetes clientset")
		os.Exit(1)
	}
	nsLookup := &engine.DefaultNamespaceLookup{Client: clientset.CoreV1()}

	if err := soteriaadmission.SetupDRPlanWebhook(mgr, vmDiscoverer, nsLookup); err != nil {
		setupLog.Error(err, "Failed to set up DRPlan webhook")
		os.Exit(1)
	}

	// Controllers that watch soteria.io resources require the aggregated API
	// server to be reachable (in-process or external). When --enable-apiserver
	// is false and no external server is registered, the informer would fail
	// to discover the soteria.io/v1alpha1 group.
	if enableAPIServer {
		if err := (&drplan.DRPlanReconciler{
			Client:          mgr.GetClient(),
			Scheme:          mgr.GetScheme(),
			VMDiscoverer:    vmDiscoverer,
			NamespaceLookup: nsLookup,
			Recorder:        mgr.GetEventRecorderFor("drplan-controller"),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create DRPlan controller")
			os.Exit(1)
		}
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}
