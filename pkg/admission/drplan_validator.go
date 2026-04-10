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
// DRPlanValidator is a validating admission webhook that intercepts DRPlan
// CREATE and UPDATE requests. It enforces cross-resource constraints that
// require external state beyond the single object being admitted:
//
//   - VM exclusivity: a VM can belong to only one DRPlan (prevents conflicting
//     storage promote/demote operations that would corrupt data)
//   - Namespace consistency: namespace-level VMs must share a single wave value
//     (crash-consistent snapshots require atomic operations; different waves
//     execute at different times, breaking the consistency guarantee)
//   - Throttle capacity: namespace+wave group size must not exceed
//     maxConcurrentFailovers (catches plans that appear valid but would fail
//     at execution time when they cannot be chunked into DRGroups)
//
// Per-object field validation (vmSelector syntax, waveLabel, maxConcurrentFailovers)
// is handled by the aggregated API server's strategy layer and is also checked
// here as defense-in-depth.

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// DRPlanValidator validates DRPlan CREATE and UPDATE operations.
type DRPlanValidator struct {
	Client       client.Reader
	VMDiscoverer engine.VMDiscoverer
	NSLookup     engine.NamespaceLookup
	decoder      admission.Decoder
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

	var allDenials []string

	// Defense-in-depth: run per-object field validation (same as strategy layer).
	if errs := soteriav1alpha1.ValidateDRPlan(plan); len(errs) > 0 {
		for _, e := range errs {
			allDenials = append(allDenials, e.Error())
		}
	}

	sel, err := metav1.LabelSelectorAsSelector(&plan.Spec.VMSelector)
	if err != nil {
		allDenials = append(allDenials, fmt.Sprintf("spec.vmSelector: %s", err.Error()))
	}
	_ = sel

	if len(allDenials) > 0 {
		logger.Info("Admission denied", "reasons", len(allDenials))
		return admission.Denied(strings.Join(allDenials, "; "))
	}

	discoveredVMs, err := v.VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("discovering VMs: %w", err))
	}

	if len(discoveredVMs) == 0 {
		logger.Info("Admission allowed")
		return admission.Allowed("")
	}

	// VM exclusivity: a VM can only belong to one DRPlan because two plans
	// trying to promote/demote the same VM's storage would cause data
	// corruption or conflicting operations.
	checker := &ExclusivityChecker{Client: v.Client, VMDiscoverer: v.VMDiscoverer}
	exclusivityConflicts, err := checker.CheckDRPlanExclusivity(ctx, plan, discoveredVMs)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("checking VM exclusivity: %w", err))
	}
	allDenials = append(allDenials, exclusivityConflicts...)

	if len(exclusivityConflicts) > 0 {
		logger.Info("VM exclusivity check completed", "conflictCount", len(exclusivityConflicts))
	}

	// Namespace consistency: namespace-level VMs must share a wave because
	// crash-consistent snapshots require atomic storage operations across all
	// VMs in the namespace — different waves execute at different times,
	// breaking consistency.
	nsLevels, waveConflicts, err := v.checkNamespaceConsistency(ctx, plan, discoveredVMs)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("checking namespace consistency: %w", err))
	}
	allDenials = append(allDenials, waveConflicts...)

	// Throttle capacity: validating at admission time prevents plans that
	// appear valid but fail at execution time when they can't be chunked
	// into DRGroups.
	capacityErrors := checkMaxConcurrentCapacity(plan, discoveredVMs, nsLevels)
	allDenials = append(allDenials, capacityErrors...)

	if len(allDenials) > 0 {
		logger.Info("Admission denied", "reasons", len(allDenials))
		return admission.Denied(strings.Join(allDenials, "; "))
	}

	logger.Info("Admission allowed")
	return admission.Allowed("")
}

func (v *DRPlanValidator) checkNamespaceConsistency(
	ctx context.Context,
	plan *soteriav1alpha1.DRPlan,
	discoveredVMs []engine.VMReference,
) (map[string]soteriav1alpha1.ConsistencyLevel, []string, error) {
	byNamespace := make(map[string][]engine.VMReference)
	for _, vm := range discoveredVMs {
		byNamespace[vm.Namespace] = append(byNamespace[vm.Namespace], vm)
	}

	nsLevels := make(map[string]soteriav1alpha1.ConsistencyLevel, len(byNamespace))
	var conflicts []string

	namespaces := make([]string, 0, len(byNamespace))
	for ns := range byNamespace {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	for _, ns := range namespaces {
		vms := byNamespace[ns]
		level, err := v.NSLookup.GetConsistencyLevel(ctx, ns)
		if err != nil {
			return nil, nil, fmt.Errorf("looking up consistency level for namespace %s: %w", ns, err)
		}
		nsLevels[ns] = level

		if level != soteriav1alpha1.ConsistencyLevelNamespace {
			continue
		}

		waveSet := make(map[string][]string)
		for _, vm := range vms {
			waveVal := vm.Labels[plan.Spec.WaveLabel]
			waveSet[waveVal] = append(waveSet[waveVal], vm.Name)
		}

		if len(waveSet) <= 1 {
			continue
		}

		var details []string
		waveKeys := make([]string, 0, len(waveSet))
		for k := range waveSet {
			waveKeys = append(waveKeys, k)
		}
		sort.Strings(waveKeys)

		for _, wk := range waveKeys {
			for _, vmName := range waveSet[wk] {
				details = append(details, fmt.Sprintf("%s=%s", vmName, wk))
			}
		}

		conflicts = append(conflicts,
			fmt.Sprintf("namespace %s has consistency-level 'namespace' but VMs have conflicting wave labels: %s",
				ns, strings.Join(details, ", ")))
	}

	return nsLevels, conflicts, nil
}

func checkMaxConcurrentCapacity(
	plan *soteriav1alpha1.DRPlan,
	discoveredVMs []engine.VMReference,
	nsLevels map[string]soteriav1alpha1.ConsistencyLevel,
) []string {
	type groupKey struct {
		namespace string
		wave      string
	}

	groups := make(map[groupKey]int)
	for _, vm := range discoveredVMs {
		ns := vm.Namespace
		if nsLevels[ns] != soteriav1alpha1.ConsistencyLevelNamespace {
			continue
		}
		waveVal := vm.Labels[plan.Spec.WaveLabel]
		groups[groupKey{namespace: ns, wave: waveVal}]++
	}

	var errors []string
	type sortableKey struct {
		ns, wave string
	}
	keys := make([]sortableKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, sortableKey{ns: k.namespace, wave: k.wave})
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ns != keys[j].ns {
			return keys[i].ns < keys[j].ns
		}
		return keys[i].wave < keys[j].wave
	})

	for _, k := range keys {
		count := groups[groupKey{namespace: k.ns, wave: k.wave}]
		if count > plan.Spec.MaxConcurrentFailovers {
			errors = append(errors,
				fmt.Sprintf("maxConcurrentFailovers (%d) is less than namespace+wave group size (%d) for namespace %s wave %s",
					plan.Spec.MaxConcurrentFailovers, count, k.ns, k.wave))
		}
	}

	return errors
}
