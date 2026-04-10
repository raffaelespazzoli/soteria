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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// ExclusivityChecker validates that VMs belong to at most one DRPlan.
// A VM must not appear in two plans because concurrent promote/demote
// operations on the same storage would cause data corruption.
type ExclusivityChecker struct {
	Client       client.Reader
	VMDiscoverer engine.VMDiscoverer
}

// CheckDRPlanExclusivity lists all DRPlans, discovers VMs for each, and
// returns conflict messages for any VM that appears in both the candidate
// plan and an existing plan.
func (c *ExclusivityChecker) CheckDRPlanExclusivity(
	ctx context.Context,
	plan *soteriav1alpha1.DRPlan,
	discoveredVMs []engine.VMReference,
) ([]string, error) {
	if len(discoveredVMs) == 0 {
		return nil, nil
	}

	candidateVMs := make(map[string]bool, len(discoveredVMs))
	for _, vm := range discoveredVMs {
		candidateVMs[vm.Namespace+"/"+vm.Name] = true
	}

	var planList soteriav1alpha1.DRPlanList
	if err := c.Client.List(ctx, &planList); err != nil {
		return nil, fmt.Errorf("listing DRPlans for exclusivity check: %w", err)
	}

	var conflicts []string
	for i := range planList.Items {
		existing := &planList.Items[i]
		if existing.Namespace == plan.Namespace && existing.Name == plan.Name {
			continue
		}

		if _, err := metav1.LabelSelectorAsSelector(&existing.Spec.VMSelector); err != nil {
			return nil, fmt.Errorf("parsing vmSelector of existing DRPlan %s/%s: %w",
				existing.Namespace, existing.Name, err)
		}

		existingVMs, err := c.VMDiscoverer.DiscoverVMs(ctx, existing.Spec.VMSelector)
		if err != nil {
			return nil, fmt.Errorf("discovering VMs for existing DRPlan %s/%s: %w",
				existing.Namespace, existing.Name, err)
		}

		for _, vm := range existingVMs {
			key := vm.Namespace + "/" + vm.Name
			if candidateVMs[key] {
				conflicts = append(conflicts,
					fmt.Sprintf("VM %s already belongs to DRPlan %s/%s", key, existing.Namespace, existing.Name))
			}
		}
	}

	return conflicts, nil
}
