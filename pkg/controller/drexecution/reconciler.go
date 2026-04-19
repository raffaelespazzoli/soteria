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
// The DRExecution reconciler validates newly created DRExecution resources,
// applies state machine transitions on the referenced DRPlan, sets initial
// execution status, and dispatches the wave executor to orchestrate DRGroup
// execution across waves. Idempotency is two-layered: terminal results
// (Succeeded/PartiallySucceeded/Failed) cause an immediate skip, while a
// set startTime gates the setup phase so plan transitions are never repeated
// on re-reconcile.

package drexecution

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=drplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list

// DRExecutionReconciler watches DRExecution resources and drives the DR
// workflow engine. It validates execution requests against the state machine,
// transitions the referenced DRPlan to an in-progress phase, dispatches the
// wave executor, and records the final result.
type DRExecutionReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     events.EventRecorder
	WaveExecutor *engine.WaveExecutor
	Handler      engine.DRGroupHandler
}

func (r *DRExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", req.Name)
	logger.V(1).Info("Reconciling DRExecution")

	var exec soteriav1alpha1.DRExecution
	if err := r.Get(ctx, req.NamespacedName, &exec); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Idempotency: skip if the execution has reached a terminal result.
	// We check Result (not just StartTime) so that the reconciler can
	// retry when the executor returned an infrastructure error after
	// StartTime was set but before a terminal result was written.
	if exec.Status.Result == soteriav1alpha1.ExecutionResultSucceeded ||
		exec.Status.Result == soteriav1alpha1.ExecutionResultPartiallySucceeded ||
		exec.Status.Result == soteriav1alpha1.ExecutionResultFailed {
		logger.V(1).Info("DRExecution already completed, skipping", "result", exec.Status.Result)
		return ctrl.Result{}, nil
	}

	// Fetch the referenced DRPlan (needed by both setup and executor paths).
	var plan soteriav1alpha1.DRPlan
	if err := r.Get(ctx, client.ObjectKey{Name: exec.Spec.PlanName}, &plan); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Referenced DRPlan not found", "plan", exec.Spec.PlanName)
			return r.failExecution(ctx, &exec, "PlanNotFound",
				fmt.Sprintf("DRPlan %q not found", exec.Spec.PlanName))
		}
		return ctrl.Result{}, err
	}

	// Setup phase: validate, set startTime, transition the plan.
	// Gated on startTime so these steps never repeat on re-reconcile.
	if exec.Status.StartTime == nil {
		if exec.Spec.Mode != soteriav1alpha1.ExecutionModePlannedMigration &&
			exec.Spec.Mode != soteriav1alpha1.ExecutionModeDisaster {
			return r.failExecution(ctx, &exec, "InvalidMode",
				fmt.Sprintf("unsupported execution mode %q", exec.Spec.Mode))
		}

		previousPhase := plan.Status.Phase
		targetPhase, err := engine.Transition(previousPhase, exec.Spec.Mode)
		if err != nil {
			validPhases := engine.ValidStartingPhases(exec.Spec.Mode)
			sort.Strings(validPhases)
			logger.Info("Invalid phase transition",
				"plan", plan.Name, "currentPhase", previousPhase, "mode", exec.Spec.Mode)
			return r.failExecution(ctx, &exec, "InvalidPhaseTransition",
				fmt.Sprintf("cannot %s from phase %q on plan %q; valid starting phases: %s",
					exec.Spec.Mode, previousPhase, plan.Name, strings.Join(validPhases, ", ")))
		}

		now := metav1.Now()
		execPatch := client.MergeFrom(exec.DeepCopy())
		exec.Status.StartTime = &now
		meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
			Type:               "Progressing",
			Status:             metav1.ConditionTrue,
			Reason:             "ExecutionStarted",
			Message:            fmt.Sprintf("Execution started for plan %s in %s mode", plan.Name, exec.Spec.Mode),
			ObservedGeneration: exec.Generation,
		})

		if err := r.Status().Patch(ctx, &exec, execPatch); err != nil {
			logger.Error(err, "Failed to update DRExecution status")
			return ctrl.Result{}, err
		}

		planPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = targetPhase
		if err := r.Status().Patch(ctx, &plan, planPatch); err != nil {
			logger.Error(err, "Failed to update DRPlan phase", "plan", plan.Name, "targetPhase", targetPhase)
			return ctrl.Result{}, err
		}

		logger.Info("Transitioned DRPlan phase",
			"plan", plan.Name, "from", previousPhase, "to", targetPhase)

		eventReason, eventAction, eventVerb := "FailoverStarted", "FailoverAction", "Failover"
		if targetPhase == soteriav1alpha1.PhaseFailingBack {
			eventReason, eventAction, eventVerb = "FailbackStarted", "FailbackAction", "Failback"
		}
		r.event(&plan, corev1.EventTypeNormal, eventReason, eventAction,
			fmt.Sprintf("%s started for plan %s in %s mode via execution %s",
				eventVerb, plan.Name, exec.Spec.Mode, exec.Name))

		logger.Info("DRExecution setup complete",
			"plan", plan.Name, "mode", exec.Spec.Mode, "targetPhase", targetPhase)
	}

	// Dispatch (or re-dispatch) the wave executor.
	if r.WaveExecutor != nil {
		handler := r.Handler
		if handler == nil {
			handler = &engine.NoOpHandler{}
		}
		execInput := engine.ExecuteInput{
			Execution: &exec,
			Plan:      &plan,
			Handler:   handler,
		}
		if err := r.WaveExecutor.Execute(ctx, execInput); err != nil {
			logger.Error(err, "Wave execution failed", "plan", plan.Name, "execution", exec.Name)
			return ctrl.Result{}, err
		}

		r.event(&exec, corev1.EventTypeNormal, "ExecutionCompleted", "WaveExecution",
			fmt.Sprintf("Execution completed: %s", exec.Status.Result))
		r.event(&plan, corev1.EventTypeNormal, "ExecutionCompleted", "WaveExecution",
			fmt.Sprintf("Execution completed for plan %s: %s", plan.Name, exec.Status.Result))
	}

	return ctrl.Result{}, nil
}

// failExecution marks a DRExecution as Failed with a descriptive condition.
func (r *DRExecutionReconciler) failExecution(
	ctx context.Context,
	exec *soteriav1alpha1.DRExecution,
	reason, message string,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("drexecution", exec.Name, "reason", reason)

	now := metav1.Now()
	patch := client.MergeFrom(exec.DeepCopy())
	exec.Status.Result = soteriav1alpha1.ExecutionResultFailed
	exec.Status.StartTime = &now
	exec.Status.CompletionTime = &now
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: exec.Generation,
	})

	if err := r.Status().Patch(ctx, exec, patch); err != nil {
		logger.Error(err, "Failed to update DRExecution failure status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DRExecutionReconciler) event(
	obj client.Object, eventType, reason, action, msg string,
) {
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, eventType, reason, action, msg)
	}
}

func (r *DRExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&soteriav1alpha1.DRExecution{}).
		Complete(r)
}
