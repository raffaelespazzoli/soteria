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
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/soteria-project/soteria/pkg/storage/scylladb"
	"github.com/testcontainers/testcontainers-go"
)

func TestNewClient_Success(t *testing.T) {
	client, err := scylladb.NewClient(scylladb.ClientConfig{
		ContactPoints:  []string{scyllaHost},
		Port:           scyllaPort,
		Consistency:    gocql.LocalOne,
		ConnectTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer client.Close()

	if client.Session() == nil {
		t.Fatal("expected non-nil session")
	}
}

func TestNewClient_NoContactPoints(t *testing.T) {
	_, err := scylladb.NewClient(scylladb.ClientConfig{})
	if err == nil {
		t.Fatal("expected error for empty contact points")
	}
}

func TestClient_HealthCheck(t *testing.T) {
	client, err := scylladb.NewClient(scylladb.ClientConfig{
		ContactPoints:  []string{scyllaHost},
		Port:           scyllaPort,
		Consistency:    gocql.LocalOne,
		ConnectTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}

func TestClient_Close(t *testing.T) {
	client, err := scylladb.NewClient(scylladb.ClientConfig{
		ContactPoints:  []string{scyllaHost},
		Port:           scyllaPort,
		Consistency:    gocql.LocalOne,
		ConnectTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	client.Close()

	if !client.Session().Closed() {
		t.Fatal("expected session to be closed")
	}
}

func TestClient_DefaultConsistency(t *testing.T) {
	client, err := scylladb.NewClient(scylladb.ClientConfig{
		ContactPoints:  []string{scyllaHost},
		Port:           scyllaPort,
		ConnectTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("health check with default consistency failed: %v", err)
	}
}

func TestClient_ReconnectAfterPause(t *testing.T) {
	client, err := scylladb.NewClient(scylladb.ClientConfig{
		ContactPoints:  []string{scyllaHost},
		Port:           scyllaPort,
		Consistency:    gocql.LocalOne,
		ConnectTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer client.Close()

	// Confirm healthy before disruption
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("initial health check failed: %v", err)
	}

	// Pause the container to simulate network partition
	dockerClient, err := testcontainers.NewDockerClientWithOpts(context.Background())
	if err != nil {
		t.Fatalf("creating docker client: %v", err)
	}
	defer dockerClient.Close()

	containerID := scyllaContainer.GetContainerID()

	if err := dockerClient.ContainerPause(context.Background(), containerID); err != nil {
		t.Fatalf("pausing container: %v", err)
	}

	// Health check should fail while paused
	failCtx, failCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer failCancel()
	if err := client.HealthCheck(failCtx); err == nil {
		// Unpause before failing the test to avoid leaving container in bad state
		_ = dockerClient.ContainerUnpause(context.Background(), containerID)
		t.Fatal("expected health check to fail while container is paused")
	}

	// Unpause — gocql should reconnect automatically
	if err := dockerClient.ContainerUnpause(context.Background(), containerID); err != nil {
		t.Fatalf("unpausing container: %v", err)
	}

	// Poll until health check passes again (gocql reconnects in background)
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		pollCtx, pollCancel := context.WithTimeout(context.Background(), 2*time.Second)
		lastErr = client.HealthCheck(pollCtx)
		pollCancel()
		if lastErr == nil {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("health check did not recover within 30s after unpause: %v", lastErr)
}
