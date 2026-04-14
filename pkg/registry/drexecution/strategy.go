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

package drexecution

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

type drexecutionStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var Strategy = drexecutionStrategy{soteriainstall.Scheme, names.SimpleNameGenerator}

// DRExecution is cluster-scoped: it references a cluster-scoped DRPlan by name,
// so the execution must also be cluster-scoped to avoid cross-scope references.
func (drexecutionStrategy) NamespaceScoped() bool { return false }

func (drexecutionStrategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	exec := obj.(*soteriav1alpha1.DRExecution)
	exec.Status = soteriav1alpha1.DRExecutionStatus{}
	exec.Generation = 1
}

// PrepareForUpdate freezes both spec and status on main-resource updates.
// Spec is immutable because a DRExecution represents a single invocation
// with fixed parameters (planName, mode). Changing spec mid-flight would
// invalidate in-progress group statuses. Status is managed exclusively
// via the status subresource to prevent spec-update races from overwriting
// controller-driven progress.
func (drexecutionStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newExec := obj.(*soteriav1alpha1.DRExecution)
	oldExec := old.(*soteriav1alpha1.DRExecution)
	newExec.Status = oldExec.Status
}

func (drexecutionStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	exec := obj.(*soteriav1alpha1.DRExecution)
	allErrs := field.ErrorList{}

	fldPath := field.NewPath("spec")
	if exec.Spec.PlanName == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("planName"), ""))
	}
	if exec.Spec.Mode != soteriav1alpha1.ExecutionModePlannedMigration &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeDisaster {
		allErrs = append(allErrs, field.NotSupported(
			fldPath.Child("mode"),
			exec.Spec.Mode,
			[]string{string(soteriav1alpha1.ExecutionModePlannedMigration), string(soteriav1alpha1.ExecutionModeDisaster)},
		))
	}
	return allErrs
}

func (s drexecutionStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	newExec := obj.(*soteriav1alpha1.DRExecution)
	oldExec := old.(*soteriav1alpha1.DRExecution)
	allErrs := s.Validate(ctx, obj)

	if newExec.Spec != oldExec.Spec {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec"),
			"spec is immutable after creation",
		))
	}
	return allErrs
}

func (drexecutionStrategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string { return nil }
func (drexecutionStrategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string {
	return nil
}
func (drexecutionStrategy) AllowCreateOnUpdate() bool      { return false }
func (drexecutionStrategy) AllowUnconditionalUpdate() bool { return false }
func (drexecutionStrategy) Canonicalize(_ runtime.Object)  {}

// GetAttrs returns labels and fields of a DRExecution for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	exec, ok := obj.(*soteriav1alpha1.DRExecution)
	if !ok {
		return nil, nil, field.Invalid(field.NewPath(""), obj, "expected DRExecution")
	}
	return exec.Labels, fields.Set{
		"metadata.name": exec.Name,
	}, nil
}

// MatchDRExecution returns a SelectionPredicate for DRExecution.
func MatchDRExecution(label labels.Selector, fieldSel fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fieldSel,
		GetAttrs: GetAttrs,
	}
}

// ---------- Status subresource strategy ----------

type drexecutionStatusStrategy struct {
	drexecutionStrategy
}

var StatusStrategy = drexecutionStatusStrategy{Strategy}

func (drexecutionStatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newExec := obj.(*soteriav1alpha1.DRExecution)
	oldExec := old.(*soteriav1alpha1.DRExecution)
	newExec.Spec = oldExec.Spec
}

// ValidateUpdate enforces append-only semantics: once an execution reaches
// a terminal result, the entire resource is frozen. This guarantees that
// audit trails, DRGroupStatus rollups, and alerting integrations can treat
// the result as final — no retroactive edits are possible.
func (drexecutionStatusStrategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	oldExec := old.(*soteriav1alpha1.DRExecution)
	allErrs := field.ErrorList{}

	if oldExec.Status.Result == soteriav1alpha1.ExecutionResultSucceeded ||
		oldExec.Status.Result == soteriav1alpha1.ExecutionResultPartiallySucceeded ||
		oldExec.Status.Result == soteriav1alpha1.ExecutionResultFailed {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("status"),
			"DRExecution is immutable after completion",
		))
	}
	return allErrs
}
