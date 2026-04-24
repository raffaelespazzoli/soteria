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

package engine

import "context"

// NoOpVMManager implements VMManager as a no-op for testing and dev/CI
// environments where KubeVirt is not available. VMsReady controls IsVMReady
// behavior: when true (the default after a nil check), VMs are immediately
// reported as ready. Tests can set VMsReady=false to simulate delayed readiness.
type NoOpVMManager struct {
	// VMsReady controls IsVMReady return value. Nil means true (ready).
	VMsReady *bool
}

func (m *NoOpVMManager) StopVM(_ context.Context, _, _ string) error              { return nil }
func (m *NoOpVMManager) StartVM(_ context.Context, _, _ string) error             { return nil }
func (m *NoOpVMManager) IsVMRunning(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (m *NoOpVMManager) IsVMReady(_ context.Context, _, _ string) (bool, error) {
	if m.VMsReady != nil {
		return *m.VMsReady, nil
	}
	return true, nil
}

// Compile-time interface check.
var _ VMManager = (*NoOpVMManager)(nil)
