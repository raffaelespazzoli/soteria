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

// Package preflight assembles pre-flight composition reports for DRPlans.
// It consumes outputs from the discovery, consistency, chunking, and storage
// backend resolution pipeline stages and formats them into a user-facing
// PreflightReport struct. The DRPlan reconciler calls into this package on
// every reconcile cycle to populate .status.preflight, giving platform
// engineers full visibility into plan structure before execution.
//
// Storage backend resolution uses the driver registry (pkg/drivers) and a
// StorageClassLister to map PVC storage classes to CSI provisioners and then
// verify a driver is available. KubeStorageClassLister provides the production
// implementation backed by the Kubernetes StorageClass API.
package preflight
