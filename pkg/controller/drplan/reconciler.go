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

// reconciler.go implements the DRPlan reconciliation loop.
//
// Architecture: On every reconcile the controller fetches the DRPlan, calls the
// VMDiscoverer to list VMs matching the plan's label selector, partitions them
// into waves via engine.GroupByWave, and writes the result to .status.waves.
// A secondary watch on kubevirt VirtualMachines (filtered by label-change
// predicates) triggers reconciliation when VMs are created, deleted, or
// relabeled, making VM-to-plan membership event-driven rather than polled.

package drplan

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

const (
	conditionTypeReady = "Ready"
	reasonDiscovered   = "VMsDiscovered"
	reasonNoVMs        = "NoVMsDiscovered"
	reasonError        = "DiscoveryError"

	requeueInterval = 10 * time.Minute
)

// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// DRPlanReconciler reconciles DRPlan objects by discovering VMs and grouping
// them into execution waves.
type DRPlanReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	VMDiscoverer engine.VMDiscoverer
	Recorder     record.EventRecorder
}

func (r *DRPlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drplan", req.NamespacedName)

	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, req.NamespacedName, &plan); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("DRPlan not found, likely deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Starting reconciliation")

	vms, err := r.VMDiscoverer.DiscoverVMs(ctx, plan.Spec.VMSelector)
	if err != nil {
		logger.Error(err, "Failed to discover VMs")
		r.event(&plan, "Warning", "DiscoveryFailed", err.Error())
		if statusErr := r.setCondition(ctx, &plan, metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             reasonError,
			Message:            err.Error(),
			ObservedGeneration: plan.Generation,
		}); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after discovery error")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	result := engine.GroupByWave(vms, plan.Spec.WaveLabel)

	waves := make([]soteriav1alpha1.WaveInfo, len(result.Waves))
	for i, wg := range result.Waves {
		discoveredVMs := make([]soteriav1alpha1.DiscoveredVM, len(wg.VMs))
		for j, vm := range wg.VMs {
			discoveredVMs[j] = soteriav1alpha1.DiscoveredVM{
				Name:      vm.Name,
				Namespace: vm.Namespace,
			}
		}
		waves[i] = soteriav1alpha1.WaveInfo{
			WaveKey: wg.WaveKey,
			VMs:     discoveredVMs,
		}
	}

	condition := metav1.Condition{
		ObservedGeneration: plan.Generation,
	}
	if result.TotalVMs > 0 {
		condition.Type = conditionTypeReady
		condition.Status = metav1.ConditionTrue
		condition.Reason = reasonDiscovered
		condition.Message = "VMs discovered and grouped into waves"
	} else {
		condition.Type = conditionTypeReady
		condition.Status = metav1.ConditionFalse
		condition.Reason = reasonNoVMs
		condition.Message = "No VMs match the plan's vmSelector"
	}

	if err := r.Get(ctx, req.NamespacedName, &plan); err != nil {
		return ctrl.Result{}, err
	}

	oldWaves := plan.Status.Waves
	plan.Status.Waves = waves
	plan.Status.DiscoveredVMCount = result.TotalVMs
	plan.Status.ObservedGeneration = plan.Generation
	meta.SetStatusCondition(&plan.Status.Conditions, condition)

	if err := r.Status().Update(ctx, &plan); err != nil {
		logger.Error(err, "Failed to update DRPlan status")
		return ctrl.Result{}, err
	}

	if !reflect.DeepEqual(oldWaves, waves) {
		logger.Info("Discovery completed", "totalVMs", result.TotalVMs, "waves", len(result.Waves))
		r.event(&plan, "Normal", "DiscoveryCompleted",
			fmt.Sprintf("Discovered %d VMs across %d waves", result.TotalVMs, len(result.Waves)))
	}

	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

func (r *DRPlanReconciler) setCondition(
	ctx context.Context, plan *soteriav1alpha1.DRPlan, condition metav1.Condition,
) error {
	if err := r.Get(ctx, types.NamespacedName{Name: plan.Name, Namespace: plan.Namespace}, plan); err != nil {
		return err
	}
	meta.SetStatusCondition(&plan.Status.Conditions, condition)
	return r.Status().Update(ctx, plan)
}

func (r *DRPlanReconciler) event(
	plan *soteriav1alpha1.DRPlan, eventType, reason, msg string,
) {
	if r.Recorder != nil {
		r.Recorder.Event(plan, eventType, reason, msg)
	}
}

func (r *DRPlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&soteriav1alpha1.DRPlan{}).
		Watches(
			&kubevirtv1.VirtualMachine{},
			handler.EnqueueRequestsFromMapFunc(r.mapVMToDRPlans),
			builder.WithPredicates(vmRelevantChangePredicate()),
		).
		Complete(r)
}

// mapVMToDRPlans returns reconcile requests for every DRPlan whose vmSelector
// matches the changed VM. The DRPlan list is served from the informer cache
// (O(N) where N = number of DRPlans, capped at ~100 by NFR9).
func (r *DRPlanReconciler) mapVMToDRPlans(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	vmLabels := obj.GetLabels()

	var planList soteriav1alpha1.DRPlanList
	if err := r.List(ctx, &planList); err != nil {
		logger.Error(err, "Failed to list DRPlans for VM mapping")
		return nil
	}

	var requests []reconcile.Request
	for i := range planList.Items {
		plan := &planList.Items[i]
		sel, err := metav1.LabelSelectorAsSelector(&plan.Spec.VMSelector)
		if err != nil {
			logger.V(1).Info("Skipping DRPlan with invalid selector",
				"drplan", plan.Name, "namespace", plan.Namespace, "error", err)
			continue
		}
		if sel.Matches(labels.Set(vmLabels)) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      plan.Name,
					Namespace: plan.Namespace,
				},
			})
		}
	}

	logger.V(2).Info("VM change mapped to DRPlans",
		"vm", obj.GetName(), "namespace", obj.GetNamespace(), "matchedPlans", len(requests))

	return requests
}

// vmRelevantChangePredicate filters VM events to only those that affect DRPlan
// composition: creates, deletes, and label changes. Status-only updates are
// ignored to avoid unnecessary reconciliation cycles.
func vmRelevantChangePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return true
		},
	}
}
