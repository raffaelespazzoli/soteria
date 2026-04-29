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

// Package metrics exposes Prometheus metrics for Soteria operations.
//
// All metrics use the soteria_ prefix, snake_case names, and standard unit
// suffixes (_total, _seconds) per OpenShift monitoring conventions.
//
// # Checkpoint metrics (instrumented by pkg/engine/checkpoint.go)
//
//   - soteria_checkpoint_writes_total       CounterVec  (execution, result)   — checkpoint write operations
//   - soteria_checkpoint_write_duration_seconds  Histogram                    — checkpoint write latency
//   - soteria_checkpoint_retries_total      Counter                           — checkpoint write retry attempts
//
// # Re-protect metrics (instrumented by pkg/engine/reprotect.go)
//
//   - soteria_reprotect_duration_seconds          Histogram                   — total re-protect execution time
//   - soteria_reprotect_vg_setup_duration_seconds Histogram                   — role setup phase duration
//   - soteria_reprotect_health_polls_total         Counter                    — health poll iterations
//
// # DRPlan metrics (instrumented by pkg/controller/drplan)
//
//   - soteria_drplan_vms_total              GaugeVec    (plan)                — VMs discovered per DRPlan
//
// # DRExecution metrics (instrumented by pkg/controller/drexecution)
//
//   - soteria_failover_duration_seconds     HistogramVec (mode)               — DR execution duration
//   - soteria_execution_total               CounterVec   (mode, result)       — completed DR execution count
package metrics
