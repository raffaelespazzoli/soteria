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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateDRPlan validates per-object field constraints on a DRPlan.
// This runs in the aggregated API server's strategy pipeline as defense-in-depth;
// cross-resource checks (VM exclusivity, namespace consistency) are handled by
// the admission webhook in pkg/admission.
func ValidateDRPlan(plan *DRPlan) field.ErrorList {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")

	allErrs = append(allErrs, validateVMSelector(plan.Spec.VMSelector, specPath.Child("vmSelector"))...)

	if plan.Spec.WaveLabel == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("waveLabel"), ""))
	}

	if plan.Spec.MaxConcurrentFailovers <= 0 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("maxConcurrentFailovers"),
			plan.Spec.MaxConcurrentFailovers,
			"must be greater than 0",
		))
	}

	return allErrs
}

func validateVMSelector(sel metav1.LabelSelector, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0 {
		allErrs = append(allErrs, field.Required(fldPath,
			"at least one matchLabels entry or matchExpressions entry is required"))
		return allErrs
	}

	if _, err := metav1.LabelSelectorAsSelector(&sel); err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, sel, err.Error()))
	}

	return allErrs
}

// ValidateDRPlanUpdate validates an update to a DRPlan.
func ValidateDRPlanUpdate(newPlan, _ *DRPlan) field.ErrorList {
	return ValidateDRPlan(newPlan)
}

// ValidateDRExecution validates a DRExecution object.
func ValidateDRExecution(exec *DRExecution) field.ErrorList {
	allErrs := field.ErrorList{}
	return allErrs
}
