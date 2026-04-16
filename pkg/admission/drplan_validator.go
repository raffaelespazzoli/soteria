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
// DRPlanValidator is a lightweight validating admission webhook that intercepts
// DRPlan CREATE and UPDATE requests and performs field-level validation only
// (waveLabel, maxConcurrentFailovers). Cross-resource constraints — VM
// exclusivity, namespace wave consistency, and throttle capacity — are
// delegated to the controller's reconciliation loop, which enforces them via
// Ready=False status conditions (WaveConflict, NamespaceGroupExceedsThrottle).
// VM exclusivity is structurally guaranteed by the soteria.io/drplan label
// convention: a label key can have only one value, so a VM belongs to at most
// one plan.
//
// Per-object field validation is also checked by the aggregated API server's
// strategy layer; the webhook provides defense-in-depth for clusters where the
// aggregated API server is not yet deployed.

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// +kubebuilder:webhook:path=/validate-soteria-io-v1alpha1-drplan,mutating=false,failurePolicy=fail,sideEffects=None,groups=soteria.io,resources=drplans,verbs=create;update,versions=v1alpha1,name=vdrplan.kb.io,admissionReviewVersions=v1,matchPolicy=Exact

// DRPlanValidator validates DRPlan CREATE and UPDATE operations.
type DRPlanValidator struct {
	decoder admission.Decoder
}

// Handle processes an admission request for a DRPlan resource.
func (v *DRPlanValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)
	logger.Info("Validating DRPlan admission",
		"name", req.Name, "namespace", req.Namespace, "operation", req.Operation)

	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	plan := &soteriav1alpha1.DRPlan{}
	if err := json.Unmarshal(req.Object.Raw, plan); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decoding DRPlan: %w", err))
	}

	var allErrs = soteriav1alpha1.ValidateDRPlan(plan)
	if req.Operation == admissionv1.Update {
		oldPlan := &soteriav1alpha1.DRPlan{}
		if err := json.Unmarshal(req.OldObject.Raw, oldPlan); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("decoding old DRPlan: %w", err))
		}
		allErrs = soteriav1alpha1.ValidateDRPlanUpdate(plan, oldPlan)
	}

	if len(allErrs) > 0 {
		logger.Info("Admission denied", "reasons", len(allErrs))
		return admission.Denied(allErrs.ToAggregate().Error())
	}

	logger.Info("Admission allowed")
	return admission.Allowed("")
}
