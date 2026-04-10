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

// Package engine implements the DR workflow execution engine. It provides VM
// discovery and wave-grouping logic used by the DRPlan controller to organize
// VMs into ordered execution waves. The discovery layer abstracts Kubernetes
// API access behind the VMDiscoverer interface, enabling deterministic unit
// tests via mock injection while the production path uses controller-runtime's
// cached client to list kubevirt VirtualMachine resources.
package engine
