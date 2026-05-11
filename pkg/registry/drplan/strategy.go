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

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/klog/v2"

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

// activeExecIndex maps plan name → active (non-terminal) DRExecution. Built
// once per ConvertToTable call via a single bulk LIST from the DRExecution
// cacher, giving O(plans+executions) cost instead of O(plans*executions).
type activeExecIndex map[string]*soteriav1alpha1.DRExecution

// DRPlanTableConvertor converts DRPlan objects to table rows with custom
// columns: PHASE, EFFECTIVE PHASE, ACTIVE SITE, VMs, ACTIVE EXECUTION.
// The "Effective Phase" and "Active Execution" columns are derived from
// DRExecution resources via drexecutionLister (injected by apiserver.go after
// both DRPlan and DRExecution storage are created). When the lister is nil
// (startup race or tests), the convertor falls back to plan status fields.
type DRPlanTableConvertor struct {
	drexecutionLister rest.Lister
}

// SetDRExecutionStorage injects the DRExecution REST storage so the table
// convertor can derive effective phase and active execution from DRExecution
// resources instead of plan status fields.
func (c *DRPlanTableConvertor) SetDRExecutionStorage(s rest.Lister) {
	c.drexecutionLister = s
}

var tableColumns = []metav1.TableColumnDefinition{
	{Name: "Name", Type: "string", Format: "name"},
	{Name: "Phase", Type: "string"},
	{Name: "Effective Phase", Type: "string"},
	{Name: "Active Site", Type: "string"},
	{Name: "VMs", Type: "integer"},
	{Name: "Active Execution", Type: "string"},
	{Name: "Age", Type: "string"},
}

func (c *DRPlanTableConvertor) ConvertToTable(
	ctx context.Context, object runtime.Object, tableOptions runtime.Object,
) (*metav1.Table, error) {
	table := &metav1.Table{ColumnDefinitions: tableColumns}
	index := c.buildActiveExecIndex(ctx)

	switch obj := object.(type) {
	case *soteriav1alpha1.DRPlan:
		table.Rows = append(table.Rows, planToRow(obj, index))
	case *soteriav1alpha1.DRPlanList:
		for i := range obj.Items {
			table.Rows = append(table.Rows, planToRow(&obj.Items[i], index))
		}
	}

	return table, nil
}

// buildActiveExecIndex performs a single bulk LIST of all DRExecutions from the
// cacher and returns a map keyed by spec.planName containing only non-terminal
// executions (status.result == ""). This avoids N per-plan queries for kubectl
// get drplans. On LIST error the index is empty (not nil) so planToRow treats
// every plan as idle rather than falling back to stale plan-status fields.
func (c *DRPlanTableConvertor) buildActiveExecIndex(ctx context.Context) activeExecIndex {
	if c == nil || c.drexecutionLister == nil {
		return nil
	}
	listObj, err := c.drexecutionLister.List(ctx, &metainternalversion.ListOptions{})
	if err != nil {
		klog.Warningf("Failed to list DRExecutions for table convertor: %v", err)
		return activeExecIndex{}
	}
	execList, ok := listObj.(*soteriav1alpha1.DRExecutionList)
	if !ok {
		klog.Warning("Unexpected list type from DRExecution lister")
		return activeExecIndex{}
	}
	index := make(activeExecIndex, len(execList.Items)/2+1)
	for i := range execList.Items {
		exec := &execList.Items[i]
		if exec.Status.Result == "" {
			if _, dup := index[exec.Spec.PlanName]; dup {
				klog.Warningf("Multiple non-terminal DRExecutions for plan %s; keeping first seen", exec.Spec.PlanName)
				continue
			}
			index[exec.Spec.PlanName] = exec
		}
	}
	return index
}

func planToRow(plan *soteriav1alpha1.DRPlan, index activeExecIndex) metav1.TableRow {
	var effectivePhase string
	var activeExecName string
	if exec, ok := index[plan.Name]; ok {
		effectivePhase = engine.EffectivePhase(plan.Status.Phase, exec.Spec.Mode)
		activeExecName = exec.Name
	} else if index == nil {
		// Fallback: lister not wired, read from plan status
		effectivePhase = engine.EffectivePhase(plan.Status.Phase, plan.Status.ActiveExecutionMode)
		activeExecName = plan.Status.ActiveExecution
	} else {
		effectivePhase = plan.Status.Phase
	}
	return metav1.TableRow{
		Object: runtime.RawExtension{Object: plan},
		Cells: []any{
			plan.Name,
			plan.Status.Phase,
			effectivePhase,
			plan.Status.ActiveSite,
			plan.Status.DiscoveredVMCount,
			activeExecName,
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
