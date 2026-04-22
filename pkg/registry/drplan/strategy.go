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

package drplan

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

type drplanStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var Strategy = drplanStrategy{soteriainstall.Scheme, names.SimpleNameGenerator}

// DRPlan is cluster-scoped: plans manage VMs across namespaces, so the plan
// name must be globally unique to avoid soteria.io/drplan label collisions.
func (drplanStrategy) NamespaceScoped() bool { return false }

func (drplanStrategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	plan := obj.(*soteriav1alpha1.DRPlan)
	plan.Status = soteriav1alpha1.DRPlanStatus{}
	plan.Status.Phase = soteriav1alpha1.PhaseSteadyState
	plan.Status.ActiveSite = plan.Spec.PrimarySite
	plan.Generation = 1
}

func (drplanStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newPlan := obj.(*soteriav1alpha1.DRPlan)
	oldPlan := old.(*soteriav1alpha1.DRPlan)
	newPlan.Status = oldPlan.Status
}

func (drplanStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	plan := obj.(*soteriav1alpha1.DRPlan)
	return soteriav1alpha1.ValidateDRPlan(plan)
}

func (drplanStrategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	newPlan := obj.(*soteriav1alpha1.DRPlan)
	oldPlan := old.(*soteriav1alpha1.DRPlan)
	return soteriav1alpha1.ValidateDRPlanUpdate(newPlan, oldPlan)
}

func (drplanStrategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string    { return nil }
func (drplanStrategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }
func (drplanStrategy) AllowCreateOnUpdate() bool                                        { return false }
func (drplanStrategy) AllowUnconditionalUpdate() bool                                   { return false }
func (drplanStrategy) Canonicalize(_ runtime.Object)                                    {}

// GetAttrs returns labels and fields of a DRPlan for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	plan, ok := obj.(*soteriav1alpha1.DRPlan)
	if !ok {
		return nil, nil, field.Invalid(field.NewPath(""), obj, "expected DRPlan")
	}
	return plan.Labels, fields.Set{
		"metadata.name": plan.Name,
	}, nil
}

// MatchDRPlan returns a SelectionPredicate for DRPlan.
func MatchDRPlan(label labels.Selector, fieldSel fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fieldSel,
		GetAttrs: GetAttrs,
	}
}

// ---------- Status subresource strategy ----------

type drplanStatusStrategy struct {
	drplanStrategy
}

var StatusStrategy = drplanStatusStrategy{Strategy}

func (drplanStatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newPlan := obj.(*soteriav1alpha1.DRPlan)
	oldPlan := old.(*soteriav1alpha1.DRPlan)
	newPlan.Spec = oldPlan.Spec
}

func (drplanStatusStrategy) ValidateUpdate(_ context.Context, _, _ runtime.Object) field.ErrorList {
	return field.ErrorList{}
}
