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

// Package fake provides a programmable fake StorageProvider for unit testing.
//
// It follows the Kubernetes <package>fake naming convention
// (e.g. k8s.io/client-go/kubernetes/fake) — the package lives at
// pkg/drivers/fake/ and is instantiated directly in test code rather than
// registered in the global driver registry.
//
// # API overview
//
// Create a Driver and pre-program responses using the fluent On* API:
//
//	d := fake.New()
//
//	// Program an error response for a specific volume group ID
//	d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)
//
//	// Program a success response (nil) for any SetSource call
//	d.OnSetSource().Return(nil)
//
//	// Program full result + error for value-returning methods
//	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
//	    ReplicationStatus: &drivers.ReplicationStatus{
//	        Role:   drivers.RoleSource,
//	        Health: drivers.HealthHealthy,
//	    },
//	})
//
// After exercising the code under test, assert on recorded calls:
//
//	calls := d.CallsTo("SetSource")
//	// calls[0].Args == []interface{}{"vg-1"}
//
//	d.CallCount("SetSource") // 1
//	d.Called("SetSource")    // true
//
// # Thread safety
//
// All public methods (On*, Return, ReturnResult, StorageProvider methods,
// Calls, CallsTo, CallCount, Called, Reset) are protected by a single
// sync.Mutex and are safe for concurrent use from multiple goroutines.
//
// # Contrast with the no-op driver
//
// The no-op driver (pkg/drivers/noop/) is a stateful in-memory simulation
// registered under "noop.soteria.io". It tracks real volume group state and
// replication roles and is intended for dev clusters, CI without real storage,
// and the driver conformance test suite.
//
// The fake driver is stateless: it returns whatever responses are pre-programmed
// and is the primary test primitive for the workflow engine (pkg/engine/) and
// controllers. It is NOT registered in the global driver registry and does NOT
// pass the conformance test suite.
package fake
