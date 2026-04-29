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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
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
	plan.Status.ActiveExecution = ""
	plan.Status.ActiveExecutionMode = ""
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

// ---------- Custom table convertor ----------

// DRPlanTableConvertor converts DRPlan objects to table rows with custom
// columns: PHASE, EFFECTIVE PHASE, ACTIVE SITE, VMs, ACTIVE EXECUTION.
type DRPlanTableConvertor struct{}

var tableColumns = []metav1.TableColumnDefinition{
	{Name: "Name", Type: "string", Format: "name"},
	{Name: "Phase", Type: "string"},
	{Name: "Effective Phase", Type: "string"},
	{Name: "Active Site", Type: "string"},
	{Name: "VMs", Type: "integer"},
	{Name: "Active Execution", Type: "string"},
	{Name: "Age", Type: "string"},
}

func (DRPlanTableConvertor) ConvertToTable(
	ctx context.Context, object runtime.Object, tableOptions runtime.Object,
) (*metav1.Table, error) {
	table := &metav1.Table{ColumnDefinitions: tableColumns}

	switch obj := object.(type) {
	case *soteriav1alpha1.DRPlan:
		table.Rows = append(table.Rows, planToRow(obj))
	case *soteriav1alpha1.DRPlanList:
		for i := range obj.Items {
			table.Rows = append(table.Rows, planToRow(&obj.Items[i]))
		}
	}

	return table, nil
}

func planToRow(plan *soteriav1alpha1.DRPlan) metav1.TableRow {
	effectivePhase := engine.EffectivePhase(plan.Status.Phase, plan.Status.ActiveExecutionMode)
	return metav1.TableRow{
		Object: runtime.RawExtension{Object: plan},
		Cells: []any{
			plan.Name,
			plan.Status.Phase,
			effectivePhase,
			plan.Status.ActiveSite,
			plan.Status.DiscoveredVMCount,
			plan.Status.ActiveExecution,
			translateTimestampSince(plan.CreationTimestamp),
		},
	}
}

func translateTimestampSince(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}
	return duration.HumanDuration(metav1.Now().Sub(timestamp.Time))
}
