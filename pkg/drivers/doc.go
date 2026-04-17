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

// Package drivers defines the StorageProvider interface, typed errors, driver
// registry, and credential resolution for DR storage backends.
//
// StorageProvider is a 7-method interface (CreateVolumeGroup, DeleteVolumeGroup,
// GetVolumeGroup, SetSource, SetTarget, StopReplication, GetReplicationStatus)
// that every storage driver must implement. The replication model uses three
// volume roles (NonReplicated, Source, Target) with all transitions routed
// through NonReplicated. All methods are idempotent and accept context.Context
// for cancellation. Implementations return typed sentinel errors from errors.go
// so the workflow engine can branch on error type without coupling to driver
// internals.
//
// The driver registry (registry.go) maps CSI provisioner names to driver
// factories. Drivers register via init() functions at import time, and the
// registry resolves PVC storage class → provisioner → StorageProvider at
// runtime. This enables heterogeneous storage support where different VMs in
// the same DRPlan use different storage backends.
//
// Credential resolution (credentials.go, credentials_secret.go) fetches storage
// credentials from Kubernetes Secrets or Vault at operation time — the
// orchestrator never stores credential values in its own resources (FR45).
//
// External storage vendor engineers import this package to implement the
// StorageProvider interface in their own driver packages under
// pkg/drivers/<vendor>/. All drivers must pass the conformance test suite in
// pkg/drivers/conformance/.
package drivers
