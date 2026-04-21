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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// CheckpointWritesTotal counts checkpoint write operations by execution
	// name and result (success or failure).
	CheckpointWritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "soteria_checkpoint_writes_total",
			Help: "Total number of checkpoint write operations",
		},
		[]string{"execution", "result"},
	)

	// CheckpointWriteDuration tracks the duration of checkpoint write
	// operations in seconds. Buckets span from 10ms to 10s to capture
	// the full range from fast writes to retried operations.
	CheckpointWriteDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "soteria_checkpoint_write_duration_seconds",
			Help:    "Duration of checkpoint write operations in seconds",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
	)

	// CheckpointRetriesTotal counts the total number of checkpoint write
	// retry attempts (each individual retry, not per-checkpoint).
	CheckpointRetriesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "soteria_checkpoint_retries_total",
			Help: "Total number of checkpoint write retry attempts",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		CheckpointWritesTotal,
		CheckpointWriteDuration,
		CheckpointRetriesTotal,
	)
}
