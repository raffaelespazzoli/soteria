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

package replication_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	v1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	scyllastore "github.com/soteria-project/soteria/pkg/storage/scylladb"
)

const (
	testKeyspace    = "soteria_repl_test"
	dc1Name         = "dc1"
	dc2Name         = "dc2"
	dc3Name         = "dc3"
	scyllaImage     = "scylladb/scylla:2025.4"
	cqlPort         = "9042/tcp"
	startupTimeout  = 120 * time.Second
	sessionTimeout  = 10 * time.Second
	sessionRetries  = 20
	sessionRetryGap = 3 * time.Second
)

var (
	dc1Container        testcontainers.Container
	dc2Container        testcontainers.Container
	tiebreakerContainer testcontainers.Container
	dc1Session          *gocql.Session
	dc2Session          *gocql.Session
	dc1Host             string
	dc1Port             int
	dc2Host             string
	dc2Port             int
	testCodec           runtime.Codec
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// DC1 seed node — starts first, forms the cluster.
	// All containers share the testcontainers default network
	// (reaper_default under podman) for inter-node gossip.
	var err error
	dc1Container, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        scyllaImage,
			ExposedPorts: []string{cqlPort},
			WaitingFor:   wait.ForLog("Starting listening for CQL clients").WithStartupTimeout(startupTimeout),
			Cmd: []string{
				"--smp", "1",
				"--memory", "256M",
				"--overprovisioned", "1",
				"--cluster-name", "soteria-test",
				"--dc", dc1Name,
				"--rack", "rack1",
			},
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("starting DC1 container: %v", err))
	}

	dc1IP, err := dc1Container.ContainerIP(ctx)
	if err != nil {
		panic(fmt.Sprintf("getting DC1 container IP: %v", err))
	}

	// DC2 node — joins the cluster via DC1 seed.
	dc2Container, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        scyllaImage,
			ExposedPorts: []string{cqlPort},
			WaitingFor:   wait.ForLog("Starting listening for CQL clients").WithStartupTimeout(startupTimeout),
			Cmd: []string{
				"--smp", "1",
				"--memory", "256M",
				"--overprovisioned", "1",
				"--cluster-name", "soteria-test",
				"--seeds", dc1IP,
				"--dc", dc2Name,
				"--rack", "rack1",
			},
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("starting DC2 container: %v", err))
	}

	// DC3 tiebreaker — provides Raft quorum (3 voters) so that losing
	// either dc1 or dc2 still leaves 2/3 majority for cluster management.
	tiebreakerContainer, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        scyllaImage,
			ExposedPorts: []string{cqlPort},
			WaitingFor:   wait.ForLog("Starting listening for CQL clients").WithStartupTimeout(startupTimeout),
			Cmd: []string{
				"--smp", "1",
				"--memory", "256M",
				"--overprovisioned", "1",
				"--cluster-name", "soteria-test",
				"--seeds", dc1IP,
				"--dc", dc3Name,
				"--rack", "rack1",
			},
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("starting tiebreaker container: %v", err))
	}

	dc1Host, dc1Port = hostEndpoint(ctx, dc1Container, "9042")
	dc2Host, dc2Port = hostEndpoint(ctx, dc2Container, "9042")

	dc1Session = createSession(dc1Host, dc1Port, "", dc1Name)
	dc2Session = createSession(dc2Host, dc2Port, "", dc2Name)

	waitForCluster(dc1Session, 3)

	// Create keyspace with NetworkTopologyStrategy (RF=1 per DC for tests).
	// dc3 is omitted: it participates in Raft but holds no data replicas.
	createNTSKeyspace(dc1Session, testKeyspace)
	if err := scyllastore.EnsureTable(dc1Session, testKeyspace); err != nil {
		panic(fmt.Sprintf("ensuring kv_store table: %v", err))
	}
	if err := scyllastore.EnsureLabelsTable(dc1Session, testKeyspace); err != nil {
		panic(fmt.Sprintf("ensuring kv_store_labels table: %v", err))
	}

	// Re-create sessions with the keyspace set.
	dc1Session.Close()
	dc2Session.Close()
	dc1Session = createSession(dc1Host, dc1Port, testKeyspace, dc1Name)
	dc2Session = createSession(dc2Host, dc2Port, testKeyspace, dc2Name)

	// Build shared codec.
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("AddToScheme: %v", err))
	}
	codecs := serializer.NewCodecFactory(scheme)
	testCodec = codecs.LegacyCodec(v1alpha1.SchemeGroupVersion)

	exitCode := m.Run()

	dc1Session.Close()
	dc2Session.Close()
	_ = dc1Container.Terminate(ctx)
	_ = dc2Container.Terminate(ctx)
	_ = tiebreakerContainer.Terminate(ctx)

	os.Exit(exitCode)
}

// newStoreForDC creates a Store pointing at the given DC's session.
func newStoreForDC(session *gocql.Session, resource string, newFunc, newListFunc func() runtime.Object) *scyllastore.Store {
	return scyllastore.NewStore(scyllastore.StoreConfig{
		Session:        session,
		Codec:          testCodec,
		Keyspace:       testKeyspace,
		GroupResource:  schema.GroupResource{Group: "soteria.io", Resource: resource},
		ResourcePrefix: "/soteria.io/" + resource,
		NewFunc:        newFunc,
		NewListFunc:    newListFunc,
	})
}

func newDRPlanStoreForDC(session *gocql.Session) *scyllastore.Store {
	return newStoreForDC(session, "drplans",
		func() runtime.Object { return &v1alpha1.DRPlan{} },
		func() runtime.Object { return &v1alpha1.DRPlanList{} },
	)
}

func newDRExecutionStoreForDC(session *gocql.Session) *scyllastore.Store {
	return newStoreForDC(session, "drexecutions",
		func() runtime.Object { return &v1alpha1.DRExecution{} },
		func() runtime.Object { return &v1alpha1.DRExecutionList{} },
	)
}

// ---------- helpers ----------

func createSession(contactIP string, port int, keyspace, localDC string) *gocql.Session {
	cluster := gocql.NewCluster(contactIP)
	cluster.Port = port
	cluster.Consistency = gocql.LocalOne
	cluster.ConnectTimeout = sessionTimeout
	cluster.Timeout = 10 * time.Second
	cluster.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy(localDC)
	// Under rootless podman, containers' internal IPs aren't routable from
	// the host. Pin ALL connections (including those discovered via gossip
	// events) to the mapped host endpoint via an address translator.
	cluster.DisableInitialHostLookup = true
	cluster.IgnorePeerAddr = true
	cluster.AddressTranslator = &pinnedTranslator{ip: net.ParseIP(contactIP), port: port}
	cluster.Events.DisableNodeStatusEvents = true
	cluster.Events.DisableTopologyEvents = true
	if keyspace != "" {
		cluster.Keyspace = keyspace
	}

	var session *gocql.Session
	var err error
	for i := 0; i < sessionRetries; i++ {
		session, err = cluster.CreateSession()
		if err == nil {
			return session
		}
		time.Sleep(sessionRetryGap)
	}
	panic(fmt.Sprintf("creating session for %s:%d (dc=%s) after %d retries: %v", contactIP, port, localDC, sessionRetries, err))
}

// pinnedTranslator forces gocql to always connect to the configured host
// endpoint, ignoring internal container IPs discovered via gossip or topology
// events that aren't routable from the host under rootless podman.
type pinnedTranslator struct {
	ip   net.IP
	port int
}

func (t *pinnedTranslator) Translate(_ net.IP, _ int) (net.IP, int) {
	return t.ip, t.port
}

// hostEndpoint returns the host-accessible address and mapped port for a container.
func hostEndpoint(ctx context.Context, c testcontainers.Container, port string) (string, int) {
	host, err := c.Host(ctx)
	if err != nil {
		panic(fmt.Sprintf("getting container host: %v", err))
	}
	mapped, err := c.MappedPort(ctx, nat.Port(port))
	if err != nil {
		panic(fmt.Sprintf("getting mapped port: %v", err))
	}
	return host, mapped.Int()
}

// waitForCluster polls system.peers until expectedNodes are visible.
func waitForCluster(session *gocql.Session, expectedNodes int) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		iter := session.Query("SELECT peer FROM system.peers").Iter()
		var count int
		var peer string
		for iter.Scan(&peer) {
			count++
		}
		_ = iter.Close()
		// +1 for the local node
		if count+1 >= expectedNodes {
			return
		}
		time.Sleep(2 * time.Second)
	}
	panic(fmt.Sprintf("cluster did not reach %d nodes within timeout", expectedNodes))
}

func createNTSKeyspace(session *gocql.Session, keyspace string) {
	cql := fmt.Sprintf(
		`CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'NetworkTopologyStrategy', '%s': 1, '%s': 1} AND tablets = {'enabled': false}`,
		keyspace, dc1Name, dc2Name,
	)
	if err := session.Query(cql).Exec(); err != nil {
		panic(fmt.Sprintf("creating NTS keyspace: %v", err))
	}
}
