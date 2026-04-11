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

package drivers

import "context"

/*
Credential Reference Architecture

This module defines the credential reference types and resolver interface for
storage drivers. Credentials are always external (Kubernetes Secrets or Vault)
and resolved at operation time — the orchestrator never stores credential values
in its own resources, config maps, or local state (FR45).

The CredentialSource type holds a reference to one external credential store.
At operation time (failover, reprotect), the controller calls
CredentialResolver.Resolve to fetch the raw credential bytes and passes them to
the storage driver. The Vault resolver is defined in the type system but
implementation is deferred to a dedicated story after Epic 3.
*/

// SecretRef references a specific key within a Kubernetes Secret.
type SecretRef struct {
	Name      string
	Namespace string
	Key       string
}

// VaultRef references a Vault KV secret using Kubernetes auth method.
type VaultRef struct {
	Path string
	Role string
	Key  string
}

// CredentialSource holds a reference to exactly one external credential store.
// Exactly one of SecretRef or VaultRef must be set; admission validation
// enforces this constraint when StorageProviderConfig exists (Epic 3).
type CredentialSource struct {
	SecretRef *SecretRef
	VaultRef  *VaultRef
}

// CredentialResolver resolves raw credential bytes from an external source.
//
// Credentials are resolved at operation time rather than cached because storage
// operations are infrequent (seconds per failover) and Vault leases / Secret
// rotations must be respected; the performance overhead of per-operation
// resolution is negligible compared to storage replication latency.
type CredentialResolver interface {
	Resolve(ctx context.Context, source CredentialSource) ([]byte, error)
}
