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

// Package noop implements a no-op StorageProvider that fulfils the full 7-method
// driver interface without performing actual storage operations.
//
// The driver tracks volume groups and replication roles (NonReplicated, Source,
// Target) in memory so that Create → Get → Delete lifecycles and role
// transitions behave realistically. State is lost on process restart — this is
// by design as there is no persistent backend.
//
// Primary uses:
//   - Local development without storage infrastructure (make dev-cluster)
//   - CI pipeline testing of workflow engine logic in isolation
//   - Reference implementation for external driver authors (Journey 4)
//
// The driver registers itself under the provisioner name "noop.soteria.io" via
// init(). Import the package for side-effect registration:
//
//	import _ "github.com/soteria-project/soteria/pkg/drivers/noop"
package noop
