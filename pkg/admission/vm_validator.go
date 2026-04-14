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
// CREATE and UPDATE requests. It validates VM mutations against DRPlan constraints
// from the VM side, complementing the DRPlan webhook (Story 2.3) which validates
// from the DRPlan side. Together they enforce VM exclusivity (FR4) and namespace
// wave consistency (FR7) regardless of which resource is mutated.
//
// DRPlan-side validation only catches conflicts when a DRPlan is created/updated.
// VM label changes can bypass that check — e.g., adding a label to a VM after two
// non-overlapping DRPlans exist can create a new overlap. Only the controller would
// catch this asynchronously on the next reconcile. This webhook provides synchronous
// rejection at admission time.

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// +kubebuilder:webhook:path=/validate-kubevirt-io-v1-virtualmachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=kubevirt.io,resources=virtualmachines,verbs=create;update,versions=v1,name=vvm.kb.io,admissionReviewVersions=v1,matchPolicy=Equivalent

// VMValidator validates VirtualMachine CREATE and UPDATE operations against
// DRPlan constraints.
type VMValidator struct {
	ExclusivityChecker *ExclusivityChecker
	NSLookup           engine.NamespaceLookup
	Client             client.Reader
	VMDiscoverer       engine.VMDiscoverer
	decoder            admission.Decoder
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

	vmName := vm.Name
	vmNamespace := vm.Namespace
	vmLabels := labels.Set(vm.Labels)

	if len(vmLabels) == 0 {
		logger.Info("VM admission allowed")
		return admission.Allowed("")
	}

	var allDenials []string

	exclusivityErrors, err := v.ExclusivityChecker.CheckVMExclusivity(ctx, vmName, vmNamespace, vmLabels)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("checking VM exclusivity: %w", err))
	}
	allDenials = append(allDenials, exclusivityErrors...)

	if len(exclusivityErrors) > 0 {
		logger.Info("VM exclusivity check completed", "matchingPlans", len(exclusivityErrors))
	}

	// When a VM's wave label changes in a namespace-level namespace, it can
	// break the same-wave constraint required for crash-consistent snapshots.
	// The DRPlan webhook only checks this when the DRPlan is mutated, not when
	// individual VMs change their wave labels.
	waveConflicts, err := v.checkWaveConflict(ctx, vmName, vmNamespace, vmLabels)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("checking wave conflict: %w", err))
	}
	allDenials = append(allDenials, waveConflicts...)

	if len(allDenials) > 0 {
		logger.Info("VM admission denied", "reasons", len(allDenials))
		return admission.Denied(strings.Join(allDenials, "; "))
	}

	logger.Info("VM admission allowed")
	return admission.Allowed("")
}

func (v *VMValidator) checkWaveConflict(
	ctx context.Context,
	vmName, vmNamespace string,
	vmLabels labels.Set,
) ([]string, error) {
	level, err := v.NSLookup.GetConsistencyLevel(ctx, vmNamespace)
	if err != nil {
		return nil, fmt.Errorf("looking up consistency level for namespace %s: %w", vmNamespace, err)
	}

	if level != soteriav1alpha1.ConsistencyLevelNamespace {
		return nil, nil
	}

	matchingPlans, err := v.ExclusivityChecker.FindMatchingPlans(ctx, vmLabels, nil)
	if err != nil {
		return nil, err
	}

	if len(matchingPlans) == 0 {
		return nil, nil
	}

	var conflicts []string
	for _, planRef := range matchingPlans {
		planConflicts, err := v.checkWaveConflictForPlan(ctx, vmName, vmNamespace, vmLabels, planRef)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, planConflicts...)
	}

	return conflicts, nil
}

func (v *VMValidator) checkWaveConflictForPlan(
	ctx context.Context,
	vmName, vmNamespace string,
	vmLabels labels.Set,
	planRef types.NamespacedName,
) ([]string, error) {
	var plan soteriav1alpha1.DRPlan
	if err := v.Client.Get(ctx, planRef, &plan); err != nil {
		return nil, fmt.Errorf("getting DRPlan %s: %w", planRef, err)
	}

	waveLabel := plan.Spec.WaveLabel
	thisVMWave := vmLabels[waveLabel]

	siblingVMs, err := v.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
	if err != nil {
		return nil, fmt.Errorf("discovering sibling VMs for plan %s: %w", planRef, err)
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
				planRef.Name, siblingWave))
			break
		}
	}

	return conflicts, nil
}
