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

package drgroupstatus

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

type drgroupstatusStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var Strategy = drgroupstatusStrategy{soteriainstall.Scheme, names.SimpleNameGenerator}

// DRGroupStatus is cluster-scoped: it references a cluster-scoped DRExecution
// by name, so it must also be cluster-scoped to avoid cross-scope references.
func (drgroupstatusStrategy) NamespaceScoped() bool { return false }

func (drgroupstatusStrategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	gs := obj.(*soteriav1alpha1.DRGroupStatus)
	gs.Status = soteriav1alpha1.DRGroupStatusState{}
	gs.Generation = 1
}

func (drgroupstatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newGS := obj.(*soteriav1alpha1.DRGroupStatus)
	oldGS := old.(*soteriav1alpha1.DRGroupStatus)
	// Status is managed via status subresource only
	newGS.Status = oldGS.Status
}

func (drgroupstatusStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	gs := obj.(*soteriav1alpha1.DRGroupStatus)
	allErrs := field.ErrorList{}

	fldPath := field.NewPath("spec")
	if gs.Spec.ExecutionName == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("executionName"), ""))
	}
	if gs.Spec.GroupName == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("groupName"), ""))
	}
	if gs.Spec.WaveIndex < 0 {
		allErrs = append(allErrs, field.Invalid(
			fldPath.Child("waveIndex"),
			gs.Spec.WaveIndex,
			"must be >= 0",
		))
	}
	return allErrs
}

func (s drgroupstatusStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	newGS := obj.(*soteriav1alpha1.DRGroupStatus)
	oldGS := old.(*soteriav1alpha1.DRGroupStatus)
	allErrs := s.Validate(ctx, obj)

	if !reflect.DeepEqual(newGS.Spec, oldGS.Spec) {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec"),
			"spec is immutable after creation",
		))
	}
	return allErrs
}

func (drgroupstatusStrategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string {
	return nil
}
func (drgroupstatusStrategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string {
	return nil
}
func (drgroupstatusStrategy) AllowCreateOnUpdate() bool      { return false }
func (drgroupstatusStrategy) AllowUnconditionalUpdate() bool { return false }
func (drgroupstatusStrategy) Canonicalize(_ runtime.Object)  {}

// GetAttrs returns labels and fields of a DRGroupStatus for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	gs, ok := obj.(*soteriav1alpha1.DRGroupStatus)
	if !ok {
		return nil, nil, field.Invalid(field.NewPath(""), obj, "expected DRGroupStatus")
	}
	return gs.Labels, fields.Set{
		"metadata.name": gs.Name,
	}, nil
}

// MatchDRGroupStatus returns a SelectionPredicate for DRGroupStatus.
func MatchDRGroupStatus(label labels.Selector, fieldSel fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fieldSel,
		GetAttrs: GetAttrs,
	}
}

// ---------- Status subresource strategy ----------

type drgroupstatusStatusStrategy struct {
	drgroupstatusStrategy
}

var StatusStrategy = drgroupstatusStatusStrategy{Strategy}

func (drgroupstatusStatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newGS := obj.(*soteriav1alpha1.DRGroupStatus)
	oldGS := old.(*soteriav1alpha1.DRGroupStatus)
	newGS.Spec = oldGS.Spec
}

func (drgroupstatusStatusStrategy) ValidateUpdate(_ context.Context, _, _ runtime.Object) field.ErrorList {
	return field.ErrorList{}
}
