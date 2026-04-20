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

// NoOpHandler is a DRGroupHandler that returns nil immediately. It serves as
// a placeholder for execution modes that do not yet have a real handler (e.g.,
// reprotect until Story 4.8) and for testing the executor loop.
type NoOpHandler struct{}

func (h *NoOpHandler) ExecuteGroup(_ context.Context, _ ExecutionGroup) error {
	return nil
}

// Compile-time interface check.
var _ DRGroupHandler = (*NoOpHandler)(nil)
