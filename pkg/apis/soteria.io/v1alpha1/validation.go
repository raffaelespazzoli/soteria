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
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateDRPlan validates per-object field constraints on a DRPlan.
// This runs in the aggregated API server's strategy pipeline as defense-in-depth;
// cross-resource checks (VM exclusivity, namespace consistency) are handled by
// the admission webhook in pkg/admission.
func ValidateDRPlan(plan *DRPlan) field.ErrorList {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")

	if plan.Spec.MaxConcurrentFailovers <= 0 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("maxConcurrentFailovers"),
			plan.Spec.MaxConcurrentFailovers,
			"must be greater than 0",
		))
	}

	if plan.Spec.PrimarySite == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("primarySite"), ""))
	}
	if plan.Spec.SecondarySite == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("secondarySite"), ""))
	}
	if plan.Spec.PrimarySite != "" && plan.Spec.PrimarySite == plan.Spec.SecondarySite {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("secondarySite"),
			plan.Spec.SecondarySite,
			"must differ from primarySite",
		))
	}

	if plan.Spec.VMReadyTimeout != nil && plan.Spec.VMReadyTimeout.Duration <= 0 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("vmReadyTimeout"),
			plan.Spec.VMReadyTimeout.Duration.String(),
			"must be a positive duration",
		))
	}

	return allErrs
}

// ValidateDRPlanUpdate validates an update to a DRPlan.
// PrimarySite and SecondarySite are immutable after creation.
func ValidateDRPlanUpdate(newPlan, oldPlan *DRPlan) field.ErrorList {
	allErrs := ValidateDRPlan(newPlan)
	specPath := field.NewPath("spec")

	if newPlan.Spec.PrimarySite != oldPlan.Spec.PrimarySite {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("primarySite"), "field is immutable"))
	}
	if newPlan.Spec.SecondarySite != oldPlan.Spec.SecondarySite {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("secondarySite"), "field is immutable"))
	}

	return allErrs
}

// ValidateDRExecution validates per-object field constraints on a DRExecution.
func ValidateDRExecution(exec *DRExecution) field.ErrorList {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")

	if exec.Spec.PlanName == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("planName"), ""))
	}

	if exec.Spec.Mode != ExecutionModePlannedMigration &&
		exec.Spec.Mode != ExecutionModeDisaster &&
		exec.Spec.Mode != ExecutionModeReprotect {
		allErrs = append(allErrs, field.NotSupported(
			specPath.Child("mode"),
			exec.Spec.Mode,
			[]string{
				string(ExecutionModePlannedMigration),
				string(ExecutionModeDisaster),
				string(ExecutionModeReprotect),
			},
		))
	}

	return allErrs
}

// ValidateDRExecutionUpdate validates an update to a DRExecution.
// DRExecution spec is immutable once created.
func ValidateDRExecutionUpdate(newExec, oldExec *DRExecution) field.ErrorList {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")

	if newExec.Spec.PlanName != oldExec.Spec.PlanName {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("planName"), "field is immutable"))
	}
	if newExec.Spec.Mode != oldExec.Spec.Mode {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("mode"), "field is immutable"))
	}

	return allErrs
}
