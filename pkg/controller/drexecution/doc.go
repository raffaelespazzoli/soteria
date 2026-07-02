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

// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch

// Package drexecution implements the DRExecution controller for workflow orchestration.
//
// The controller watches VirtualMachine, VolumeReplication, and VolumeGroupReplication
// resources in addition to DRExecution objects. VM watches drive the wave readiness
// gate (detecting when VMs reach Running). VR/VGR watches drive the event-driven
// resync gate for planned migration Step 0: after PreExecute calls ResyncVolume,
// the controller sets a ResyncPending condition and waits for VR/VGR status changes
// (state=Secondary + health=Healthy) instead of polling. A configurable resync
// timeout (DRPlan.Spec.ResyncTimeout, default 10m) acts as a safety net. In multi-site
// mode, the target site monitors local VR/VGR status and sets ResyncComplete for the
// source site, which then calls StopReplication and sets Step0Complete.
package drexecution
