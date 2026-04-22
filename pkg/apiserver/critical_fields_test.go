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
	"testing"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestDetectDRPlanCriticalFields_PhaseChange(t *testing.T) {
	old := &soteriav1alpha1.DRPlan{
		Status: soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseSteadyState, ActiveSite: "dc-west"},
	}
	updated := &soteriav1alpha1.DRPlan{
		Status: soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseFailingOver, ActiveSite: "dc-west"},
	}
	if !detectDRPlanCriticalFields(old, updated) {
		t.Error("expected critical=true when phase changes")
	}
}

func TestDetectDRPlanCriticalFields_ActiveSiteChange(t *testing.T) {
	old := &soteriav1alpha1.DRPlan{
		Status: soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseFailedOver, ActiveSite: "dc-west"},
	}
	updated := &soteriav1alpha1.DRPlan{
		Status: soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseFailedOver, ActiveSite: "dc-east"},
	}
	if !detectDRPlanCriticalFields(old, updated) {
		t.Error("expected critical=true when activeSite changes")
	}
}

func TestDetectDRPlanCriticalFields_NoChange(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		Status: soteriav1alpha1.DRPlanStatus{Phase: soteriav1alpha1.PhaseSteadyState, ActiveSite: "dc-west"},
	}
	if detectDRPlanCriticalFields(plan, plan) {
		t.Error("expected critical=false when nothing changes")
	}
}
