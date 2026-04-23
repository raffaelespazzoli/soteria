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

package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/spf13/pflag"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	// Register all built-in StorageProvider drivers.
	_ "github.com/soteria-project/soteria/pkg/drivers/all"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/soteria-project/soteria/internal/preflight"
	"github.com/soteria-project/soteria/pkg/admission"
	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/apiserver"
	"github.com/soteria-project/soteria/pkg/controller/drexecution"
	"github.com/soteria-project/soteria/pkg/controller/drplan"
	"github.com/soteria-project/soteria/pkg/drivers"
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
	utilruntime.Must(soteriav1alpha1.AddToScheme(scheme))
	utilruntime.Must(kubevirtv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var leaderElectLeaseDuration time.Duration
	var leaderElectRenewDeadline time.Duration
	var leaderElectRetryPeriod time.Duration
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var noopFallback bool
	var tlsOpts []func(*tls.Config)

	// Aggregated API server + ScyllaDB flags are registered via SoteriaServerOptions.
	// Controller-runtime flags follow the standard kubebuilder convention below.
	apiserverOpts := apiserver.NewSoteriaServerOptions()
	fs := pflag.NewFlagSet("soteria", pflag.ExitOnError)
	apiserverOpts.AddFlags(fs)

	fs.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.DurationVar(&leaderElectLeaseDuration, "leader-elect-lease-duration", 15*time.Second,
		"The duration that non-leader candidates will wait to force acquire leadership.")
	fs.DurationVar(&leaderElectRenewDeadline, "leader-elect-renew-deadline", 10*time.Second,
		"The duration that the acting leader will retry refreshing leadership before giving up.")
	fs.DurationVar(&leaderElectRetryPeriod, "leader-elect-retry-period", 2*time.Second,
		"The duration the LeaderElector clients should wait between tries of actions.")
	fs.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	fs.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	fs.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	fs.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	fs.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	fs.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	fs.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	fs.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.BoolVar(&noopFallback, "noop-fallback", false,
		"When enabled, uses no-op implementations for PVC resolution, VM health validation, and VM management. "+
			"Intended for dev/CI environments without real KubeVirt infrastructure. "+
			"Note: unregistered CSI provisioners always fall back to the noop storage driver.")

	zapOpts := zap.Options{Development: true}
	goFS := flag.NewFlagSet("", flag.ExitOnError)
	zapOpts.BindFlags(goFS)
	fs.AddGoFlagSet(goFS)

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	// Disable HTTP/2 to mitigate HTTP/2 Stream Cancellation and Rapid Reset CVEs.
	// See: https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			setupLog.Info("Disabling HTTP/2")
			c.NextProtos = []string{"http/1.1"}
		})
	}

	webhookServerOptions := webhook.Options{
		TLSOpts: tlsOpts,
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

	// ---- Aggregated API Server ----

	apiserverConfig, err := apiserverOpts.Config()
	if err != nil {
		setupLog.Error(err, "Failed to build API server config")
		os.Exit(1)
	}

	session, err := waitForScyllaDB(apiserverOpts)
	if err != nil {
		setupLog.Error(err, "Failed to connect to ScyllaDB")
		os.Exit(1)
	}
	defer session.Close()

	codec := soteriainstall.Codecs.LegacyCodec(
		soteriainstall.Scheme.PrioritizedVersionsForGroup("soteria.io")...,
	)

	apiserverConfig.ScyllaStoreFactory = &apiserver.ScyllaStoreFactory{
		StoreConfig: scylladb.StoreConfig{
			Session:  session,
			Codec:    codec,
			Keyspace: apiserverOpts.ScyllaDBKeyspace,
		},
		Codec:                  codec,
		UseCacher:              true,
		CriticalFieldDetectors: apiserver.DefaultCriticalFieldDetectors(),
	}

	completed := apiserverConfig.Complete()
	server, err := completed.New()
	if err != nil {
		setupLog.Error(err, "Failed to create API server")
		os.Exit(1)
	}

	// ---- Controller-Runtime Manager ----

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "soteria.io",
		LeaseDuration:          &leaderElectLeaseDuration,
		RenewDeadline:          &leaderElectRenewDeadline,
		RetryPeriod:            &leaderElectRetryPeriod,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	// ---- Signal handler (shared by all subsystems) ----

	ctx := ctrl.SetupSignalHandler()

	// ---- Controllers ----

	restCfg := ctrl.GetConfigOrDie()
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		setupLog.Error(err, "Failed to create Kubernetes clientset")
		os.Exit(1)
	}

	setupLog.Info("Registered storage drivers", "drivers", drivers.ListRegistered())

	vmDiscoverer := engine.NewTypedVMDiscoverer(mgr.GetClient())
	nsLookup := &engine.DefaultNamespaceLookup{Client: clientset.CoreV1()}
	scLister := &preflight.KubeStorageClassLister{Client: clientset.StorageV1()}
	storageResolver := &preflight.TypedStorageBackendResolver{
		Client:     mgr.GetClient(),
		CoreClient: clientset.CoreV1(),
		Registry:   drivers.DefaultRegistry,
		SCLister:   scLister,
	}

	var pvcResolver engine.PVCResolver
	if noopFallback {
		pvcResolver = engine.NoOpPVCResolver{}
		setupLog.Info("Using NoOpPVCResolver for dev/CI (noop-fallback enabled)")
	} else {
		pvcResolver = &engine.KubeVirtPVCResolver{Client: mgr.GetClient()}
	}

	eventBroadcaster := events.NewEventBroadcasterAdapterWithContext(ctx, clientset)
	defer eventBroadcaster.Shutdown()
	eventBroadcaster.StartRecordingToSink(ctx.Done())
	eventRecorder := eventBroadcaster.NewRecorder("drplan-controller")

	if err := (&drplan.DRPlanReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		VMDiscoverer:            vmDiscoverer,
		NamespaceLookup:         nsLookup,
		StorageResolver:         storageResolver,
		Recorder:                eventRecorder,
		Registry:                drivers.DefaultRegistry,
		SCLister:                scLister,
		PVCResolver:             pvcResolver,
		UnprotectedVMDiscoverer: vmDiscoverer,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "DRPlan")
		os.Exit(1)
	}

	drexecRecorder := eventBroadcaster.NewRecorder("drexecution-controller")

	var vmHealthValidator engine.VMHealthValidator
	if noopFallback {
		vmHealthValidator = engine.NoOpVMHealthValidator{}
		setupLog.Info("Using NoOpVMHealthValidator for dev/CI (noop-fallback enabled)")
	} else {
		vmHealthValidator = &engine.KubeVirtVMHealthValidator{Client: mgr.GetClient()}
	}

	checkpointer := &engine.KubeCheckpointer{Client: mgr.GetClient()}

	waveExecutor := &engine.WaveExecutor{
		Client:            mgr.GetClient(),
		CoreClient:        clientset.CoreV1(),
		VMDiscoverer:      vmDiscoverer,
		NamespaceLookup:   nsLookup,
		Registry:          drivers.DefaultRegistry,
		SCLister:          scLister,
		Recorder:          drexecRecorder,
		PVCResolver:       pvcResolver,
		VMHealthValidator: vmHealthValidator,
		Checkpointer:      checkpointer,
	}

	var vmManager engine.VMManager
	if noopFallback {
		vmManager = &engine.NoOpVMManager{}
		setupLog.Info("Using NoOpVMManager for dev/CI (noop-fallback enabled)")
	} else {
		vmManager = &engine.KubeVirtVMManager{Client: mgr.GetClient()}
	}

	resumeAnalyzer := &engine.ResumeAnalyzer{}

	reprotectHandler := &engine.ReprotectHandler{
		Checkpointer:       checkpointer,
		HealthPollInterval: 30 * time.Second,
		HealthTimeout:      24 * time.Hour,
	}

	if enableLeaderElection {
		setupLog.Info("Leader election configured",
			"leaseDuration", leaderElectLeaseDuration,
			"renewDeadline", leaderElectRenewDeadline,
			"retryPeriod", leaderElectRetryPeriod)
	}

	if err := (&drexecution.DRExecutionReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Recorder:         drexecRecorder,
		WaveExecutor:     waveExecutor,
		Handler:          &engine.NoOpHandler{},
		VMManager:        vmManager,
		ResumeAnalyzer:   resumeAnalyzer,
		ReprotectHandler: reprotectHandler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "DRExecution")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// ---- Webhooks ----

	if err := admission.SetupDRPlanWebhook(mgr); err != nil {
		setupLog.Error(err, "Failed to create webhook", "webhook", "DRPlan")
		os.Exit(1)
	}

	if err := admission.SetupDRExecutionWebhook(mgr); err != nil {
		setupLog.Error(err, "Failed to create webhook", "webhook", "DRExecution")
		os.Exit(1)
	}

	if err := admission.SetupVMWebhook(mgr, nsLookup, vmDiscoverer); err != nil {
		setupLog.Error(err, "Failed to create webhook", "webhook", "VirtualMachine")
		os.Exit(1)
	}

	// ---- Health probes ----

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	// ---- Start ----

	go func() {
		setupLog.Info("Starting manager")
		if err := mgr.Start(ctx); err != nil {
			setupLog.Error(err, "Failed to run manager")
			os.Exit(1)
		}
	}()

	setupLog.Info("Starting aggregated API server")
	if err := server.GenericAPIServer.PrepareRun().RunWithContext(ctx); err != nil {
		setupLog.Error(err, "Failed to run aggregated API server")
		os.Exit(1)
	}
}

// parseDCReplication parses a "dc1:rf,dc2:rf" string into a map.
func parseDCReplication(raw string) (map[string]int, error) {
	m := make(map[string]int)
	for pair := range strings.SplitSeq(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid dc:rf pair %q", pair)
		}
		var rf int
		if _, err := fmt.Sscanf(parts[1], "%d", &rf); err != nil {
			return nil, fmt.Errorf("invalid replication factor in %q: %w", pair, err)
		}
		m[strings.TrimSpace(parts[0])] = rf
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("no dc:rf pairs found")
	}
	return m, nil
}

// waitForScyllaDB retries connecting to ScyllaDB and ensuring the keyspace
// and tables exist. When --scylladb-dc-replication is set the keyspace is
// auto-created with NetworkTopologyStrategy; otherwise Soteria waits for
// an externally created keyspace.
func waitForScyllaDB(opts *apiserver.SoteriaServerOptions) (*gocql.Session, error) {
	const retryInterval = 60 * time.Second

	var schemaCfg *scylladb.SchemaConfig
	if opts.ScyllaDBDCReplication != "" {
		dcMap, err := parseDCReplication(opts.ScyllaDBDCReplication)
		if err != nil {
			return nil, fmt.Errorf("parsing --scylladb-dc-replication: %w", err)
		}
		schemaCfg = &scylladb.SchemaConfig{
			Keyspace:       opts.ScyllaDBKeyspace,
			Strategy:       "NetworkTopologyStrategy",
			DCReplication:  dcMap,
			DisableTablets: true,
		}
	}

	newClusterConfig := func() *gocql.ClusterConfig {
		hosts := strings.Split(opts.ScyllaDBContactPoints, ",")
		cluster := gocql.NewCluster(hosts...)
		cluster.Consistency = gocql.LocalOne
		cluster.ConnectTimeout = 30 * time.Second
		cluster.Timeout = 10 * time.Second

		if opts.ScyllaDBLocalDC != "" {
			cluster.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy(opts.ScyllaDBLocalDC)
		}

		if opts.ScyllaDBTLSCert != "" && opts.ScyllaDBTLSKey != "" {
			cert, err := tls.LoadX509KeyPair(opts.ScyllaDBTLSCert, opts.ScyllaDBTLSKey)
			if err != nil {
				setupLog.Error(err, "Could not load ScyllaDB TLS client cert/key")
				return nil
			}
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{cert},
				ServerName:   opts.ScyllaDBTLSServerName,
				MinVersion:   tls.VersionTLS12,
			}
			if opts.ScyllaDBTLSCA != "" {
				caCert, err := os.ReadFile(opts.ScyllaDBTLSCA)
				if err != nil {
					setupLog.Error(err, "Could not read ScyllaDB TLS CA")
					return nil
				}
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM(caCert) {
					setupLog.Error(nil, "ScyllaDB TLS CA contains no valid certificates")
					return nil
				}
				tlsConfig.RootCAs = pool
			}
			cluster.SslOpts = &gocql.SslOptions{
				Config: tlsConfig,
			}
			cluster.Port = 9142
		}
		return cluster
	}

	for {
		cluster := newClusterConfig()
		if cluster == nil {
			setupLog.Info("Retrying ScyllaDB connection", "retryInterval", retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		session, err := cluster.CreateSession()
		if err != nil {
			setupLog.Error(err, "Could not connect to ScyllaDB, will retry", "retryInterval", retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		if schemaCfg != nil {
			if err := scylladb.EnsureSchema(session, *schemaCfg); err != nil {
				setupLog.Error(err, "Could not ensure schema, will retry",
					"keyspace", opts.ScyllaDBKeyspace, "retryInterval", retryInterval)
				session.Close()
				time.Sleep(retryInterval)
				continue
			}
		} else {
			exists, err := scylladb.KeyspaceExists(session, opts.ScyllaDBKeyspace)
			if err != nil {
				setupLog.Error(err, "Could not check keyspace existence, will retry",
					"keyspace", opts.ScyllaDBKeyspace, "retryInterval", retryInterval)
				session.Close()
				time.Sleep(retryInterval)
				continue
			}
			if !exists {
				setupLog.Info("Keyspace does not exist yet, will retry",
					"keyspace", opts.ScyllaDBKeyspace, "retryInterval", retryInterval)
				session.Close()
				time.Sleep(retryInterval)
				continue
			}

			if err := scylladb.EnsureTable(session, opts.ScyllaDBKeyspace); err != nil {
				setupLog.Error(err, "Could not ensure kv_store table, will retry", "retryInterval", retryInterval)
				session.Close()
				time.Sleep(retryInterval)
				continue
			}
			if err := scylladb.EnsureLabelsTable(session, opts.ScyllaDBKeyspace); err != nil {
				setupLog.Error(err, "Could not ensure kv_store_labels table, will retry", "retryInterval", retryInterval)
				session.Close()
				time.Sleep(retryInterval)
				continue
			}
		}

		setupLog.Info("ScyllaDB connection established and schema verified", "keyspace", opts.ScyllaDBKeyspace)
		return session, nil
	}
}
