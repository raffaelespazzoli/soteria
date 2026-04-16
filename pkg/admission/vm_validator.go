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
// VMValidator is a validating admission webhook that intercepts VirtualMachine
// CREATE and UPDATE requests. It validates two concerns:
//
//  1. Plan existence: if a VM carries a soteria.io/drplan label, the referenced
//     DRPlan must exist. A missing plan produces a warning (not a rejection) to
//     avoid ordering issues during GitOps apply where VMs may land before their
//     DRPlan.
//
//  2. Namespace-level wave consistency: in namespaces annotated with
//     soteria.io/consistency-level=namespace, all VMs belonging to the same
//     plan must share a single wave label value (required for crash-consistent
//     snapshots).
//
// VM exclusivity is structurally guaranteed by Kubernetes label semantics — a
// label key can have only one value, so a VM belongs to at most one DRPlan.

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// +kubebuilder:webhook:path=/validate-kubevirt-io-v1-virtualmachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=kubevirt.io,resources=virtualmachines,verbs=create;update,versions=v1,name=vvm.kb.io,admissionReviewVersions=v1,matchPolicy=Exact

// VMValidator validates VirtualMachine CREATE and UPDATE operations against
// DRPlan constraints.
type VMValidator struct {
	NSLookup     engine.NamespaceLookup
	Client       client.Reader
	VMDiscoverer engine.VMDiscoverer
	decoder      admission.Decoder
}

// Handle processes an admission request for a VirtualMachine resource.
func (v *VMValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)
	logger.Info("Validating VM admission",
		"name", req.Name, "namespace", req.Namespace, "operation", req.Operation)

	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	vm := &kubevirtv1.VirtualMachine{}
	if err := json.Unmarshal(req.Object.Raw, vm); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decoding VirtualMachine: %w", err))
	}

	planName := vm.Labels[soteriav1alpha1.DRPlanLabel]
	if planName == "" {
		logger.Info("VM admission allowed")
		return admission.Allowed("")
	}

	// Check that the referenced DRPlan exists. Issue a warning (not rejection)
	// when it doesn't — during GitOps apply the DRPlan CR may not exist yet.
	plan := &soteriav1alpha1.DRPlan{}
	err := v.Client.Get(ctx, types.NamespacedName{Name: planName}, plan)
	if apierrors.IsNotFound(err) {
		logger.Info("Referenced DRPlan not found, issuing warning",
			"plan", planName, "vm", vm.Name, "namespace", vm.Namespace)
		return admission.Allowed("").WithWarnings(
			fmt.Sprintf("referenced DRPlan %q does not exist", planName))
	}
	if err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("getting DRPlan %q: %w", planName, err))
	}

	// Wave consistency: in namespace-level namespaces, all VMs under the same
	// plan must share a single wave value for crash-consistent snapshots.
	waveConflicts, err := v.checkWaveConflictForPlan(ctx, vm.Name, vm.Namespace, vm.Labels, plan)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("checking wave conflict: %w", err))
	}

	if len(waveConflicts) > 0 {
		logger.Info("VM admission denied", "reasons", len(waveConflicts))
		return admission.Denied(strings.Join(waveConflicts, "; "))
	}

	logger.Info("VM admission allowed")
	return admission.Allowed("")
}

func (v *VMValidator) checkWaveConflictForPlan(
	ctx context.Context,
	vmName, vmNamespace string,
	vmLabels map[string]string,
	plan *soteriav1alpha1.DRPlan,
) ([]string, error) {
	level, err := v.NSLookup.GetConsistencyLevel(ctx, vmNamespace)
	if err != nil {
		return nil, fmt.Errorf("looking up consistency level for namespace %s: %w", vmNamespace, err)
	}

	if level != soteriav1alpha1.ConsistencyLevelNamespace {
		return nil, nil
	}

	waveLabel := plan.Spec.WaveLabel
	thisVMWave := vmLabels[waveLabel]

	siblingVMs, err := v.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
	if err != nil {
		return nil, fmt.Errorf("discovering sibling VMs for plan %s: %w", plan.Name, err)
	}

	var conflicts []string
	for _, sibling := range siblingVMs {
		if sibling.Namespace != vmNamespace {
			continue
		}
		if sibling.Name == vmName {
			continue
		}

		siblingWave := sibling.Labels[waveLabel]
		if siblingWave != thisVMWave {
			conflicts = append(conflicts, fmt.Sprintf(
				"VM %s/%s wave label '%s' conflicts with existing VMs in "+
					"namespace-level namespace %s under DRPlan %s (expected wave '%s')",
				vmNamespace, vmName, thisVMWave, vmNamespace,
				plan.Name, siblingWave))
			break
		}
	}

	return conflicts, nil
}
