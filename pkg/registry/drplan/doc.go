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

// Package drplan implements the REST storage registry for DRPlan resources.
// It provides create/update/delete strategies following the k8s.io/apiserver
// generic registry pattern, including validation of waveLabel and
// maxConcurrentFailovers, defaulting the initial phase to SteadyState, and
// a separate StatusStrategy that freezes spec on status-subresource updates.
// NewREST wires the strategy to a genericregistry.Store backed by the
// ScyllaDB storage.Interface (optionally cacher-wrapped).
package drplan
