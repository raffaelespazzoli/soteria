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

// Package csiextension implements a StorageProvider that manages volume
// replication through CSI Addons VolumeReplication and VolumeGroupReplication
// Kubernetes CRDs. The csi-addons sidecar container reconciles these CRDs into
// actual storage-level replication operations.
//
// This driver is progressively implemented across Stories 12.1–12.5.
// Story 12.1 creates the skeleton with stub methods that return
// "not yet implemented" errors; subsequent stories fill in real logic.
//
// The driver registers itself under the plan-level name "csi-extension" via
// init(). Import the package for side-effect registration:
//
//	import _ "github.com/soteria-project/soteria/pkg/drivers/csiextension"
package csiextension
