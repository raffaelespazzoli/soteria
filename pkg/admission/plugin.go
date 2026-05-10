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

package admission

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

const PluginName = "SoteriaValidation"

// SoteriaAdmissionPlugin validates DRExecution CREATE and DRPlan
// CREATE/UPDATE requests in-process within the aggregated API server.
// It replaces the controller-runtime ValidatingWebhookConfiguration path
// which does not fire for aggregated API resources.
type SoteriaAdmissionPlugin struct {
	*admission.Handler
	drplanStorage      rest.Getter
	drexecutionStorage rest.Lister
}

// NewSoteriaAdmissionPlugin creates a new uninitialized admission plugin.
// Call SetDRPlanStorage and SetDRExecutionStorage before the plugin is used.
func NewSoteriaAdmissionPlugin() *SoteriaAdmissionPlugin {
	return &SoteriaAdmissionPlugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
	}
}

// SetDRPlanStorage injects the DRPlan REST storage for cross-object lookups.
func (p *SoteriaAdmissionPlugin) SetDRPlanStorage(s rest.Getter) {
	p.drplanStorage = s
}

// SetDRExecutionStorage injects the DRExecution REST storage for concurrency
// checks (listing non-terminal executions for a plan).
func (p *SoteriaAdmissionPlugin) SetDRExecutionStorage(s rest.Lister) {
	p.drexecutionStorage = s
}

var (
	drexecutionResource = soteriav1alpha1.Resource("drexecutions")
	drplanResource      = soteriav1alpha1.Resource("drplans")
)

func (p *SoteriaAdmissionPlugin) Validate(
	ctx context.Context, a admission.Attributes, _ admission.ObjectInterfaces,
) error {
	if a.GetSubresource() != "" {
		return nil
	}

	gr := a.GetResource().GroupResource()
	switch gr {
	case drexecutionResource:
		return p.validateDRExecution(ctx, a)
	case drplanResource:
		return p.validateDRPlan(a)
	default:
		return nil
	}
}

func (p *SoteriaAdmissionPlugin) validateDRExecution(ctx context.Context, a admission.Attributes) error {
	if a.GetOperation() != admission.Create {
		return nil
	}

	exec, ok := a.GetObject().(*soteriav1alpha1.DRExecution)
	if !ok {
		return admission.NewForbidden(a, fmt.Errorf("expected DRExecution, got %T", a.GetObject()))
	}

	if exec.Spec.PlanName == "" {
		return admission.NewForbidden(a, fmt.Errorf("spec.planName is required"))
	}

	if exec.Spec.Mode != soteriav1alpha1.ExecutionModePlannedMigration &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeDisaster &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeReprotect {
		return admission.NewForbidden(a, fmt.Errorf(
			"spec.mode must be %q, %q, or %q, got %q",
			soteriav1alpha1.ExecutionModePlannedMigration,
			soteriav1alpha1.ExecutionModeDisaster,
			soteriav1alpha1.ExecutionModeReprotect,
			exec.Spec.Mode,
		))
	}

	if p.drplanStorage == nil {
		return fmt.Errorf("soteria admission plugin not initialized: drplan storage is nil")
	}

	obj, err := p.drplanStorage.Get(ctx, exec.Spec.PlanName, &metav1.GetOptions{})
	if err != nil {
		if statusErr, ok := err.(interface{ Status() metav1.Status }); ok {
			if statusErr.Status().Reason == metav1.StatusReasonNotFound {
				return admission.NewForbidden(a,
					fmt.Errorf("DRPlan %q not found", exec.Spec.PlanName))
			}
		}
		return fmt.Errorf("looking up DRPlan %q: %w", exec.Spec.PlanName, err)
	}

	plan, ok := obj.(*soteriav1alpha1.DRPlan)
	if !ok {
		return fmt.Errorf("unexpected object type from storage: %T", obj)
	}

	// Concurrency gate: list DRExecutions for this plan and reject if any
	// non-terminal execution exists. This replaces the old
	// plan.Status.ActiveExecution check with a derived query so that the
	// DRPlan status no longer needs a concurrency pointer.
	if p.drexecutionStorage != nil {
		if err := p.checkNoConcurrentExecution(ctx, a, exec.Spec.PlanName); err != nil {
			return err
		}
	} else {
		klog.Warningf("DRExecution admission concurrency gate disabled: storage not injected")
	}

	if _, err := engine.Transition(plan.Status.Phase, exec.Spec.Mode); err != nil {
		validPhases := engine.ValidStartingPhases(exec.Spec.Mode)
		sort.Strings(validPhases)
		return admission.NewForbidden(a, fmt.Errorf(
			"DRPlan %q is in phase %q; %s is only valid from phases: %s",
			exec.Spec.PlanName,
			plan.Status.Phase,
			exec.Spec.Mode,
			strings.Join(validPhases, ", "),
		))
	}

	if sisCond := meta.FindStatusCondition(plan.Status.Conditions, "SitesInSync"); sisCond != nil &&
		sisCond.Status == metav1.ConditionFalse {
		return admission.NewForbidden(a,
			fmt.Errorf("cannot start execution: sites do not agree on VM inventory. Resolve VM differences first"))
	}

	if dcCond := meta.FindStatusCondition(plan.Status.Conditions, "DisksConsistent"); dcCond != nil &&
		dcCond.Status == metav1.ConditionFalse {
		msg := "disk validation failed"
		if dcCond.Message != "" {
			msg = dcCond.Message
		}
		return admission.NewForbidden(a,
			fmt.Errorf("cannot start execution: %s", msg))
	}

	return nil
}

// checkNoConcurrentExecution lists DRExecutions with the plan-name label and
// rejects the request if any non-terminal execution exists. This is a
// best-effort admission gate (layer 1 of the three-layer concurrency model);
// the SERIAL INSERT (layer 2) and reconciler exclusivity check (layer 3)
// provide stronger guarantees.
func (p *SoteriaAdmissionPlugin) checkNoConcurrentExecution(
	ctx context.Context, a admission.Attributes, planName string,
) error {
	listObj, err := p.drexecutionStorage.List(ctx, &metainternalversion.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			soteriav1alpha1.PlanNameLabel: planName,
		}),
	})
	if err != nil {
		return fmt.Errorf("listing DRExecutions for plan %q: %w", planName, err)
	}

	execList, ok := listObj.(*soteriav1alpha1.DRExecutionList)
	if !ok {
		return fmt.Errorf("unexpected list type from storage: %T", listObj)
	}

	for i := range execList.Items {
		e := &execList.Items[i]
		if e.Status.Result == "" {
			return admission.NewForbidden(a, fmt.Errorf(
				"DRPlan %q has active execution %q; concurrent executions not permitted",
				planName, e.Name))
		}
	}
	return nil
}

func (p *SoteriaAdmissionPlugin) validateDRPlan(a admission.Attributes) error {
	op := a.GetOperation()
	if op != admission.Create && op != admission.Update {
		return nil
	}

	plan, ok := a.GetObject().(*soteriav1alpha1.DRPlan)
	if !ok {
		return admission.NewForbidden(a, fmt.Errorf("expected DRPlan, got %T", a.GetObject()))
	}

	var allErrs = soteriav1alpha1.ValidateDRPlan(plan)

	if op == admission.Update {
		oldPlan, ok := a.GetOldObject().(*soteriav1alpha1.DRPlan)
		if !ok {
			return admission.NewForbidden(a, fmt.Errorf("expected DRPlan for old object, got %T", a.GetOldObject()))
		}
		allErrs = soteriav1alpha1.ValidateDRPlanUpdate(plan, oldPlan)
	}

	if len(allErrs) > 0 {
		return admission.NewForbidden(a, allErrs.ToAggregate())
	}

	return nil
}
