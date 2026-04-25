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

// Tier 2 – Architecture:
// checkpoint.go implements per-DRGroup checkpointing for crash recovery. After
// each DRGroup completes (success or failure), the executor writes the updated
// DRExecution.Status to the Kubernetes API server via the status subresource.
// This ensures that on pod restart, the new leader can reconstruct execution
// state from the persisted status and resume from the last checkpoint — losing
// at most one in-flight DRGroup.
//
// The checkpoint write path: controller → kube-apiserver → aggregated API
// server → ScyllaDB. The controller never bypasses the Kubernetes API chain.
// Optimistic concurrency is handled by re-fetching the DRExecution before each
// retry attempt to obtain the latest resourceVersion.

package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/metrics"
)

// ErrCheckpointFailed is returned when a checkpoint write exhausts all retry
// attempts. The executor handles this by marking the DRGroup as Failed and
// continuing fail-forward — a persistent API server outage should not hang
// the execution indefinitely.
var ErrCheckpointFailed = errors.New("checkpoint write failed after retries")

// Checkpointer persists DRExecution status to the Kubernetes API server as a
// checkpoint. The checkpoint is the sole source of truth for resume decisions
// after a pod restart — no in-memory state survives across reconcile calls.
//
// Tier 3 – Domain: checkpointing via kube-apiserver (not direct ScyllaDB)
// ensures consistency through the standard Kubernetes optimistic concurrency
// model (resourceVersion). This aligns with the architectural boundary: the
// engine writes checkpoints via the Kubernetes API and never touches ScyllaDB
// directly.
type Checkpointer interface {
	WriteCheckpoint(ctx context.Context, exec *soteriav1alpha1.DRExecution) error
}

// KubeCheckpointer writes DRExecution status checkpoints via the Kubernetes
// API server's status subresource. It re-fetches the resource before each
// retry to avoid resourceVersion conflicts.
type KubeCheckpointer struct {
	Client client.Client
}

// WriteCheckpoint patches the DRExecution status via the Kubernetes API server
// with exponential backoff retry. Re-fetches the resource before each attempt
// to get the latest resourceVersion (avoiding conflict errors).
func (c *KubeCheckpointer) WriteCheckpoint(ctx context.Context, exec *soteriav1alpha1.DRExecution) error {
	logger := log.FromContext(ctx)
	start := time.Now()

	backoff := wait.Backoff{
		Duration: ScyllaRetry.Duration,
		Factor:   ScyllaRetry.Factor,
		Jitter:   ScyllaRetry.Jitter,
		Cap:      10 * time.Second,
		Steps:    ScyllaRetry.Steps,
	}

	statusCopy := exec.Status.DeepCopy()
	var lastErr error
	attempt := 0

	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		attempt++

		fresh := &soteriav1alpha1.DRExecution{}
		if err := c.Client.Get(ctx, client.ObjectKeyFromObject(exec), fresh); err != nil {
			lastErr = err
			metrics.CheckpointRetriesTotal.Inc()
			logger.V(1).Info("Checkpoint re-fetch failed, retrying",
				"execution", exec.Name, "attempt", attempt, "error", err)
			return false, nil
		}

		fresh.Status = *statusCopy
		if err := c.Client.Status().Update(ctx, fresh); err != nil {
			lastErr = err
			metrics.CheckpointRetriesTotal.Inc()
			logger.V(1).Info("Checkpoint write failed, retrying",
				"execution", exec.Name, "attempt", attempt, "error", err)
			return false, nil
		}

		return true, nil
	})

	duration := time.Since(start)
	metrics.CheckpointWriteDuration.Observe(duration.Seconds())

	if err != nil {
		metrics.CheckpointWritesTotal.WithLabelValues(exec.Name, "failure").Inc()
		logger.Info("Checkpoint write exhausted retries",
			"execution", exec.Name, "attempts", attempt, "lastError", lastErr)
		return fmt.Errorf("%w: %v", ErrCheckpointFailed, lastErr)
	}

	metrics.CheckpointWritesTotal.WithLabelValues(exec.Name, "success").Inc()
	logger.V(1).Info("Checkpoint written",
		"execution", exec.Name, "duration", duration)
	return nil
}

// NoOpCheckpointer records checkpoint calls without persisting anything.
// Used in unit tests to verify checkpoint integration without Kubernetes API
// access. Configurable to fail for error-path testing.
type NoOpCheckpointer struct {
	mu       sync.Mutex
	Calls    []string
	FailNext bool
	FailErr  error
}

func (c *NoOpCheckpointer) WriteCheckpoint(_ context.Context, exec *soteriav1alpha1.DRExecution) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Calls = append(c.Calls, exec.Name)

	if c.FailNext {
		c.FailNext = false
		if c.FailErr != nil {
			return c.FailErr
		}
		return ErrCheckpointFailed
	}
	return nil
}

// GetCalls returns a copy of the recorded call list for assertions.
func (c *NoOpCheckpointer) GetCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.Calls))
	copy(out, c.Calls)
	return out
}

// Compile-time interface checks.
var (
	_ Checkpointer = (*KubeCheckpointer)(nil)
	_ Checkpointer = (*NoOpCheckpointer)(nil)
)
