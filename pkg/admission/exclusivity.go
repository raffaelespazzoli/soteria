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
// exclusivity.go provides shared VM exclusivity checking logic used by both the
// DRPlan webhook (Story 2.3) and the VM webhook (Story 2.3.1). The core question
// is "given a VM's labels, which DRPlans select it?" — answered by
// FindMatchingPlans which lists all DRPlans, parses their vmSelector, and returns
// those whose selector matches the given label set.
//
// CheckVMExclusivity wraps FindMatchingPlans for the VM webhook: it checks
// whether a VM's labels match more than one DRPlan.
//
// CheckDRPlanExclusivity wraps FindMatchingPlans for the DRPlan webhook: for
// each discovered VM, it checks whether any other plan also selects that VM.

package admission

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// ExclusivityChecker validates that VMs belong to at most one DRPlan.
//
// VM exclusivity prevents two DRPlans from issuing conflicting storage
// operations (promote/demote) on the same VM's volumes, which would cause
// data corruption or split-brain.
type ExclusivityChecker struct {
	Client       client.Reader
	VMDiscoverer engine.VMDiscoverer
}

// FindMatchingPlans returns all DRPlans whose vmSelector matches the given
// label set, optionally excluding a specific plan (used when validating the
// plan itself to avoid self-conflict).
func (c *ExclusivityChecker) FindMatchingPlans(
	ctx context.Context,
	vmLabels labels.Set,
	excludePlan *types.NamespacedName,
) ([]types.NamespacedName, error) {
	var planList soteriav1alpha1.DRPlanList
	if err := c.Client.List(ctx, &planList); err != nil {
		return nil, fmt.Errorf("listing DRPlans: %w", err)
	}

	var matching []types.NamespacedName
	for i := range planList.Items {
		plan := &planList.Items[i]
		if excludePlan != nil && plan.Namespace == excludePlan.Namespace && plan.Name == excludePlan.Name {
			continue
		}

		sel, err := metav1.LabelSelectorAsSelector(&plan.Spec.VMSelector)
		if err != nil {
			continue
		}

		if sel.Matches(vmLabels) {
			matching = append(matching, types.NamespacedName{
				Namespace: plan.Namespace,
				Name:      plan.Name,
			})
		}
	}

	return matching, nil
}

// CheckVMExclusivity checks whether a VM's labels match more than one DRPlan.
// Returns error messages listing all matching plans if a violation exists.
func (c *ExclusivityChecker) CheckVMExclusivity(
	ctx context.Context,
	vmName, vmNamespace string,
	vmLabels labels.Set,
) ([]string, error) {
	matchingPlans, err := c.FindMatchingPlans(ctx, vmLabels, nil)
	if err != nil {
		return nil, err
	}

	if len(matchingPlans) <= 1 {
		return nil, nil
	}

	planNames := make([]string, len(matchingPlans))
	for i, p := range matchingPlans {
		planNames[i] = p.String()
	}

	return []string{
		fmt.Sprintf("VM %s/%s would belong to multiple DRPlans: %s",
			vmNamespace, vmName, strings.Join(planNames, ", ")),
	}, nil
}

// CheckDRPlanExclusivity checks whether any of the candidate plan's discovered
// VMs also match another existing DRPlan. The candidate plan is excluded from
// matching so that self-updates don't trigger false positives.
func (c *ExclusivityChecker) CheckDRPlanExclusivity(
	ctx context.Context,
	plan *soteriav1alpha1.DRPlan,
	discoveredVMs []engine.VMReference,
) ([]string, error) {
	if len(discoveredVMs) == 0 {
		return nil, nil
	}

	excludePlan := types.NamespacedName{Namespace: plan.Namespace, Name: plan.Name}

	var conflicts []string
	for _, vm := range discoveredVMs {
		matchingPlans, err := c.FindMatchingPlans(ctx, labels.Set(vm.Labels), &excludePlan)
		if err != nil {
			return nil, err
		}
		for _, p := range matchingPlans {
			conflicts = append(conflicts,
				fmt.Sprintf("VM %s/%s already belongs to DRPlan %s/%s",
					vm.Namespace, vm.Name, p.Namespace, p.Name))
		}
	}

	return conflicts, nil
}
