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
func DefaultCriticalFieldDetectors() map[schema.GroupResource]scylladb.CriticalFieldDetector {
	return map[schema.GroupResource]scylladb.CriticalFieldDetector{
		{Group: soteriav1alpha1.GroupName, Resource: "drplans"}:      detectDRPlanCriticalFields,
		{Group: soteriav1alpha1.GroupName, Resource: "drexecutions"}: detectDRExecutionCriticalFields,
	}
}

// detectDRPlanCriticalFields returns true when the DRPlan phase has changed,
// indicating a state-machine transition that must be serialized across DCs.
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
// has changed, indicating a terminal outcome that must be set exactly once.
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
