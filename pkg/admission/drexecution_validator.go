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

// Tier 2 – Architecture:
// DRExecutionValidator is a validating admission webhook that intercepts
// DRExecution CREATE requests. It validates field-level constraints (planName
// required, mode must be planned_migration or disaster) and cross-resource
// constraints (referenced DRPlan must exist and be in a valid starting phase
// for the requested execution mode). The webhook uses mgr.GetAPIReader()
// (uncached) for plan lookups to prevent stale-cache race conditions when
// two DRExecutions target the same plan simultaneously.

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// +kubebuilder:webhook:path=/validate-soteria-io-v1alpha1-drexecution,mutating=false,failurePolicy=fail,sideEffects=None,groups=soteria.io,resources=drexecutions,verbs=create,versions=v1alpha1,name=vdrexecution.kb.io,admissionReviewVersions=v1,matchPolicy=Exact

// DRExecutionValidator validates DRExecution CREATE operations.
type DRExecutionValidator struct {
	reader client.Reader
}

// Handle processes an admission request for a DRExecution resource.
func (v *DRExecutionValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)
	logger.Info("Validating DRExecution admission", "name", req.Name, "operation", req.Operation)

	if req.Operation != admissionv1.Create {
		return admission.Allowed("")
	}

	exec := &soteriav1alpha1.DRExecution{}
	if err := json.Unmarshal(req.Object.Raw, exec); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decoding DRExecution: %w", err))
	}

	if exec.Spec.PlanName == "" {
		return admission.Denied("spec.planName is required")
	}

	if exec.Spec.Mode != soteriav1alpha1.ExecutionModePlannedMigration &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeDisaster &&
		exec.Spec.Mode != soteriav1alpha1.ExecutionModeReprotect {
		return admission.Denied(fmt.Sprintf(
			"spec.mode must be %q, %q, or %q, got %q",
			soteriav1alpha1.ExecutionModePlannedMigration,
			soteriav1alpha1.ExecutionModeDisaster,
			soteriav1alpha1.ExecutionModeReprotect,
			exec.Spec.Mode,
		))
	}

	var plan soteriav1alpha1.DRPlan
	if err := v.reader.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return admission.Denied(fmt.Sprintf("DRPlan %q not found", exec.Spec.PlanName))
		}
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("looking up DRPlan %q: %w", exec.Spec.PlanName, err))
	}

	// Concurrency gate: reject if another execution is already active.
	if plan.Status.ActiveExecution != "" {
		return admission.Denied(fmt.Sprintf(
			"DRPlan %q has active execution %q; concurrent executions not permitted",
			exec.Spec.PlanName, plan.Status.ActiveExecution))
	}

	if _, err := engine.Transition(plan.Status.Phase, exec.Spec.Mode); err != nil {
		validPhases := engine.ValidStartingPhases(exec.Spec.Mode)
		sort.Strings(validPhases)
		return admission.Denied(fmt.Sprintf(
			"DRPlan %q is in phase %q; %s is only valid from phases: %s",
			exec.Spec.PlanName,
			plan.Status.Phase,
			exec.Spec.Mode,
			strings.Join(validPhases, ", "),
		))
	}

	// Reject when sites do not agree on VM inventory.
	if sisCond := meta.FindStatusCondition(plan.Status.Conditions, "SitesInSync"); sisCond != nil &&
		sisCond.Status == metav1.ConditionFalse {
		return admission.Denied(
			"Cannot start execution: sites do not agree on VM inventory. Resolve VM differences first.")
	}

	logger.Info("Admission allowed")
	return admission.Allowed("")
}
