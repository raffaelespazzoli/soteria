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

package scylladb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/gocql/gocql"
)

// ClientConfig holds configuration for connecting to a ScyllaDB cluster.
type ClientConfig struct {
	// ContactPoints is a list of ScyllaDB node addresses.
	ContactPoints []string
	// Port is the CQL native transport port (default: 9042).
	Port int
	// Keyspace is the ScyllaDB keyspace to use.
	Keyspace string
	// Datacenter is the local DC name for DC-aware routing.
	Datacenter string
	// CertPath is the path to the client TLS certificate (PEM).
	CertPath string
	// KeyPath is the path to the client TLS private key (PEM).
	KeyPath string
	// CAPath is the path to the CA certificate (PEM).
	CAPath string
	// ConnectTimeout is the initial connection timeout.
	ConnectTimeout time.Duration
	// Consistency is the default consistency level.
	Consistency gocql.Consistency
}

// Client wraps a gocql session for ScyllaDB communication.
type Client struct {
	session *gocql.Session
	cluster *gocql.ClusterConfig
}

// NewClient creates a new ScyllaDB client with the given configuration.
// When CertPath, KeyPath, and CAPath are all empty, TLS is disabled
// for local development and testcontainers environments.
func NewClient(cfg ClientConfig) (*Client, error) {
	if len(cfg.ContactPoints) == 0 {
		return nil, fmt.Errorf("at least one contact point is required")
	}

	cluster := gocql.NewCluster(cfg.ContactPoints...)

	if cfg.Port > 0 {
		cluster.Port = cfg.Port
	}

	if cfg.Keyspace != "" {
		cluster.Keyspace = cfg.Keyspace
	}

	if cfg.Consistency == 0 {
		cluster.Consistency = gocql.LocalOne
	} else {
		cluster.Consistency = cfg.Consistency
	}

	if cfg.ConnectTimeout > 0 {
		cluster.ConnectTimeout = cfg.ConnectTimeout
	} else {
		cluster.ConnectTimeout = 10 * time.Second
	}

	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: 10,
		Min:        100 * time.Millisecond,
		Max:        10 * time.Second,
	}
	cluster.ReconnectInterval = 1 * time.Second

	if cfg.Datacenter != "" {
		cluster.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy(cfg.Datacenter)
	}

	if cfg.CertPath != "" && cfg.KeyPath != "" && cfg.CAPath != "" {
		tlsConfig, err := buildTLSConfig(cfg.CertPath, cfg.KeyPath, cfg.CAPath)
		if err != nil {
			return nil, fmt.Errorf("building TLS config: %w", err)
		}
		cluster.SslOpts = &gocql.SslOptions{
			Config:                 tlsConfig,
			EnableHostVerification: true,
		}
	}

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("creating scylladb session: %w", err)
	}

	return &Client{
		session: session,
		cluster: cluster,
	}, nil
}

// Session returns the underlying gocql session.
func (c *Client) Session() *gocql.Session {
	return c.session
}

// HealthCheck verifies ScyllaDB connectivity by executing a lightweight CQL query.
func (c *Client) HealthCheck(ctx context.Context) error {
	return c.session.Query("SELECT now() FROM system.local").
		WithContext(ctx).
		Exec()
}

// Close gracefully shuts down the ScyllaDB session.
func (c *Client) Close() {
	if c.session != nil && !c.session.Closed() {
		c.session.Close()
	}
}

func buildTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading client certificate: %w", err)
	}

	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("parsing CA certificate")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
