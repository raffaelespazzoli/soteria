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

// Package v1alpha1 defines the API types for the soteria.io API group. It
// contains the Go structs for DRPlan (disaster recovery plan with VM selectors
// and wave configuration), DRExecution (an in-progress or completed failover
// operation), and DRGroupStatus (per-group progress within an execution wave).
// These types are served by the aggregated API server and stored in ScyllaDB
// via the storage.Interface — they are not CRDs backed by etcd.
//
// +k8s:deepcopy-gen=package
// +k8s:openapi-gen=true
// +groupName=soteria.io
package v1alpha1
