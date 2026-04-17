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

import "errors"

// Sentinel errors for StorageProvider operations. Driver implementations must
// return these (or wrap them with fmt.Errorf %w) so the workflow engine can
// make branching decisions via errors.Is without coupling to driver internals.
//
// Credential-related errors (ErrVaultNotImplemented, ErrNoCredentialSource, etc.)
// live in credentials_secret.go and are intentionally kept separate.
var (
	ErrVolumeNotFound      = errors.New("volume not found")
	ErrVolumeGroupNotFound = errors.New("volume group not found")
	ErrReplicationNotReady = errors.New("replication not ready")
	ErrInvalidTransition   = errors.New("invalid replication state transition")
	ErrDriverNotFound      = errors.New("storage driver not found for provisioner")
)
