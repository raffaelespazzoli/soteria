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

// Package drgroupstatus implements the REST storage registry for DRGroupStatus
// resources. DRGroupStatus tracks the progress of a single group of VMs within
// a DR execution wave. Spec is immutable after creation (executionName,
// groupName, waveIndex are fixed); only status may be updated to reflect
// per-group failover progress. A separate StatusStrategy permits permissive
// status updates while preserving spec.
package drgroupstatus
