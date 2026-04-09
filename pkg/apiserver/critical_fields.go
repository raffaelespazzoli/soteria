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

package apiserver

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	scylladb "github.com/soteria-project/soteria/pkg/storage/scylladb"
)

// DefaultCriticalFieldDetectors returns per-resource CriticalFieldDetectors
// for resources whose state-machine fields require cross-DC LWT.
//
// Without cross-DC serialization, concurrent writers in different datacenters
// could both CAS-succeed on a phase transition (e.g., SteadyState → Failover)
// because LOCAL_SERIAL only serializes within a single DC. SERIAL (full Paxos
// quorum across all DCs) prevents this at the cost of cross-DC latency. We
// only pay that cost for the specific fields that are state-machine transitions
// — all other updates remain LOCAL_SERIAL for performance.
func DefaultCriticalFieldDetectors() map[schema.GroupResource]scylladb.CriticalFieldDetector {
	return map[schema.GroupResource]scylladb.CriticalFieldDetector{
		{Group: soteriav1alpha1.GroupName, Resource: "drplans"}:      detectDRPlanCriticalFields,
		{Group: soteriav1alpha1.GroupName, Resource: "drexecutions"}: detectDRExecutionCriticalFields,
	}
}

// detectDRPlanCriticalFields returns true when the DRPlan phase has changed.
// Phase transitions (SteadyState ↔ Failover ↔ Relocating) must be globally
// unique: two DCs must never simultaneously believe they are the active site.
func detectDRPlanCriticalFields(old, updated runtime.Object) bool {
	oldPlan, ok := old.(*soteriav1alpha1.DRPlan)
	if !ok {
		return false
	}
	newPlan, ok := updated.(*soteriav1alpha1.DRPlan)
	if !ok {
		return false
	}
	return oldPlan.Status.Phase != newPlan.Status.Phase
}

// detectDRExecutionCriticalFields returns true when the DRExecution result
// has changed. Terminal outcomes (Succeeded, PartiallySucceeded, Failed) are
// write-once: a second DC must not overwrite or race with the first DC's
// verdict, as downstream consumers (UI, alerting) rely on result finality.
func detectDRExecutionCriticalFields(old, updated runtime.Object) bool {
	oldExec, ok := old.(*soteriav1alpha1.DRExecution)
	if !ok {
		return false
	}
	newExec, ok := updated.(*soteriav1alpha1.DRExecution)
	if !ok {
		return false
	}
	return oldExec.Status.Result != newExec.Status.Result
}
