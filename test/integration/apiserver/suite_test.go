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

package apiserver_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/rest"
	basecompatibility "k8s.io/component-base/compatibility"

	apiopenapi "k8s.io/apiserver/pkg/endpoints/openapi"

	soteriaopenapi "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/apiserver"
	scylladb "github.com/soteria-project/soteria/pkg/storage/scylladb"
)

var (
	restConfig      *rest.Config
	testCleanup     func()
	testCodec       runtime.Codec
	testScheme      *runtime.Scheme
	testCodecFatory serializer.CodecFactory
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// 1. Start ScyllaDB via testcontainers
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "scylladb/scylla:latest",
			ExposedPorts: []string{"9042/tcp"},
			WaitingFor:   wait.ForLog("Starting listening for CQL clients").WithStartupTimeout(90 * time.Second),
			Cmd:          []string{"--smp", "1", "--memory", "256M", "--overprovisioned", "1"},
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("starting scylladb container: %v", err))
	}

	host, err := container.Host(ctx)
	if err != nil {
		panic(fmt.Sprintf("getting container host: %v", err))
	}
	mappedPort, err := container.MappedPort(ctx, "9042")
	if err != nil {
		panic(fmt.Sprintf("getting mapped port: %v", err))
	}

	// 2. Create ScyllaDB session
	cluster := gocql.NewCluster(host)
	cluster.Port = mappedPort.Int()
	cluster.Consistency = gocql.LocalOne
	cluster.ConnectTimeout = 30 * time.Second
	cluster.Timeout = 10 * time.Second

	var session *gocql.Session
	for range 10 {
		session, err = cluster.CreateSession()
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		panic(fmt.Sprintf("creating session after retries: %v", err))
	}

	// 3. Ensure schema
	keyspace := "soteria_test"
	if err := scylladb.EnsureSchema(session, scylladb.SchemaConfig{
		Keyspace:          keyspace,
		Strategy:          "SimpleStrategy",
		ReplicationFactor: 1,
	}); err != nil {
		panic(fmt.Sprintf("ensuring schema: %v", err))
	}

	// 4. Set up scheme and codec
	testScheme = soteriainstall.Scheme
	testCodecFatory = soteriainstall.Codecs
	testCodec = soteriainstall.Codecs.LegacyCodec(
		testScheme.PrioritizedVersionsForGroup("soteria.io")...,
	)

	// 5. Build and start the extension API server (no cacher for integration tests)
	serverConfig := genericapiserver.NewRecommendedConfig(testCodecFatory)
	serverConfig.EffectiveVersion = basecompatibility.NewEffectiveVersionFromString("1.35", "", "")

	namer := apiopenapi.NewDefinitionNamer(testScheme)
	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		soteriaopenapi.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIConfig.Info.Title = "Soteria"
	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(
		soteriaopenapi.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIV3Config.Info.Title = "Soteria"

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("finding free port: %v", err))
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Configure secure serving with loopback
	secureOpts := genericoptions.NewSecureServingOptions().WithLoopback()
	secureOpts.BindPort = port
	secureOpts.BindAddress = net.ParseIP("127.0.0.1")
	secureOpts.ServerCert.CertDirectory = os.TempDir()

	if err := secureOpts.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		panic(fmt.Sprintf("generating self-signed certs: %v", err))
	}

	if err := secureOpts.ApplyTo(&serverConfig.SecureServing, &serverConfig.LoopbackClientConfig); err != nil {
		panic(fmt.Sprintf("applying secure serving options: %v", err))
	}

	soteriaConfig := &apiserver.Config{
		GenericConfig: serverConfig,
		ScyllaStoreFactory: &apiserver.ScyllaStoreFactory{
			StoreConfig: scylladb.StoreConfig{
				Session:  session,
				Codec:    testCodec,
				Keyspace: keyspace,
			},
			Codec:     testCodec,
			UseCacher: false,
		},
	}

	completed := soteriaConfig.Complete()
	server, err := completed.New()
	if err != nil {
		panic(fmt.Sprintf("creating API server: %v", err))
	}

	// Start the server in background
	stopCh := make(chan struct{})
	go func() {
		if err := server.GenericAPIServer.PrepareRun().Run(stopCh); err != nil {
			fmt.Fprintf(os.Stderr, "API server error: %v\n", err)
		}
	}()

	// Build rest.Config for test clients
	restConfig = &rest.Config{
		Host: fmt.Sprintf("https://127.0.0.1:%d", port),
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Group: soteriav1alpha1.GroupName, Version: "v1alpha1"},
			NegotiatedSerializer: testCodecFatory,
		},
	}

	// Use loopback config if available (has auth token)
	if serverConfig.LoopbackClientConfig != nil {
		restConfig = rest.CopyConfig(serverConfig.LoopbackClientConfig)
		restConfig.ContentConfig = rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Group: soteriav1alpha1.GroupName, Version: "v1alpha1"},
			NegotiatedSerializer: testCodecFatory,
		}
	}

	// Wait for server to be ready
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpClient.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	testCleanup = func() {
		close(stopCh)
		session.Close()
		_ = container.Terminate(ctx)
	}

	exitCode := m.Run()

	testCleanup()
	os.Exit(exitCode)
}
