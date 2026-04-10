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

package storage_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	scyllaContainer testcontainers.Container
	scyllaHost      string
	scyllaPort      int
	testSession     *gocql.Session
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "scylladb/scylla:2025.4",
			ExposedPorts: []string{"9042/tcp"},
			WaitingFor:   wait.ForLog("Starting listening for CQL clients").WithStartupTimeout(90 * time.Second),
			Cmd:          []string{"--smp", "1", "--memory", "256M", "--overprovisioned", "1"},
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("starting scylladb container: %v", err))
	}
	scyllaContainer = container

	host, err := container.Host(ctx)
	if err != nil {
		panic(fmt.Sprintf("getting container host: %v", err))
	}
	scyllaHost = host

	mappedPort, err := container.MappedPort(ctx, "9042")
	if err != nil {
		panic(fmt.Sprintf("getting mapped port: %v", err))
	}
	scyllaPort = mappedPort.Int()

	cluster := gocql.NewCluster(scyllaHost)
	cluster.Port = scyllaPort
	cluster.Consistency = gocql.LocalOne
	cluster.ConnectTimeout = 30 * time.Second
	cluster.Timeout = 10 * time.Second

	// ScyllaDB may need extra time after CQL port is ready
	var sess *gocql.Session
	for i := 0; i < 10; i++ {
		sess, err = cluster.CreateSession()
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		panic(fmt.Sprintf("creating session after retries: %v", err))
	}
	testSession = sess

	exitCode := m.Run()

	testSession.Close()
	_ = scyllaContainer.Terminate(ctx)

	os.Exit(exitCode)
}
