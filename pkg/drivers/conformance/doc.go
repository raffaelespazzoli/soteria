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

// Package conformance provides a conformance test suite that validates any
// [drivers.StorageProvider] implementation against the full DR lifecycle
// contract defined by the Soteria orchestrator.
//
// # What the Suite Validates
//
// The suite consists of four test groups, each exercising a different aspect
// of the StorageProvider contract:
//
//   - Lifecycle — the complete DR lifecycle sequence: CreateVolumeGroup →
//     SetSource → GetReplicationStatus(Source) → StopReplication → SetTarget →
//     GetReplicationStatus(Target) → StopReplication → DeleteVolumeGroup →
//     GetVolumeGroup(deleted). Verifies correct role transitions and that
//     deletion is confirmed via [drivers.ErrVolumeGroupNotFound].
//
//   - Idempotency — every interface method is called twice in succession with
//     identical arguments. The second call must succeed without error, proving
//     the driver is safe to retry after crashes or restarts.
//
//   - ContextCancellation — every method is called with a pre-cancelled
//     [context.Context]. The method must return an error immediately; it must
//     not block or succeed.
//
//   - ErrorConditions — operations on a nonexistent volume group ID must return
//     [drivers.ErrVolumeGroupNotFound] (verified via [errors.Is]).
//
// # How to Wire a Driver
//
// Create a _test.go file in your driver's package (or any test package) and
// call [RunConformance] with a fresh instance of your driver:
//
//	package mydriver_test
//
//	import (
//	    "testing"
//
//	    "github.com/soteria-project/soteria/pkg/drivers/conformance"
//	    "example.com/mydriver"
//	)
//
//	func TestConformance(t *testing.T) {
//	    conformance.RunConformance(t, mydriver.New())
//	}
//
// # Running the Suite
//
//	go test ./pkg/drivers/conformance/...
//
// The suite uses only the standard [testing] package — no Ginkgo, Gomega, or
// other test framework dependencies. It works with any Go test runner, CI
// system, or IDE.
package conformance
