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

package shadowpv

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/csiextension"
	"github.com/soteria-project/soteria/pkg/engine"
)

const publisherFinalizer = "soteria.io/shadowpv-publisher"

// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumereplications,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=replication.storage.openshift.io,resources=volumegroupreplications,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=shadowpvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=soteria.io,resources=drplans,verbs=get

// ShadowPVPublisherReconciler watches VolumeReplication and
// VolumeGroupReplication CRs bearing a soteria.io/drplan label, resolves
// their PVC→PV chain, and publishes the backing PV specs into cluster-scoped
// ShadowPV resources for cross-site volume pre-provisioning.
type ShadowPVPublisherReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	LocalSite string
	APIReader client.Reader
}

func (r *ShadowPVPublisherReconciler) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Try VolumeReplication first.
	var vr replicationv1alpha1.VolumeReplication
	vrErr := r.Get(ctx, req.NamespacedName, &vr)
	if vrErr == nil {
		return r.reconcileVR(ctx, &vr)
	}

	// Try VolumeGroupReplication.
	var vgr replicationv1alpha1.VolumeGroupReplication
	vgrErr := r.Get(ctx, req.NamespacedName, &vgr)
	if vgrErr == nil {
		return r.reconcileVGR(ctx, &vgr)
	}

	if errors.IsNotFound(vrErr) && errors.IsNotFound(vgrErr) {
		return ctrl.Result{}, nil
	}

	if errors.IsNotFound(vrErr) {
		logger.Error(vgrErr, "Could not get VGR", "name", req.NamespacedName)
		return ctrl.Result{}, vgrErr
	}
	logger.Error(vrErr, "Could not get VR or VGR", "name", req.NamespacedName)
	return ctrl.Result{}, vrErr
}

// pvResolver abstracts PV resolution for VR and VGR types.
type pvResolver func(context.Context) ([]soteriav1alpha1.ShadowPVEntry, error)

func (r *ShadowPVPublisherReconciler) reconcileVR(
	ctx context.Context, vr *replicationv1alpha1.VolumeReplication,
) (ctrl.Result, error) {
	resolver := func(ctx context.Context) ([]soteriav1alpha1.ShadowPVEntry, error) {
		return r.resolvePVsForVR(ctx, vr)
	}
	return r.reconcileReplicationObject(ctx, vr, false, resolver)
}

func (r *ShadowPVPublisherReconciler) reconcileVGR(
	ctx context.Context, vgr *replicationv1alpha1.VolumeGroupReplication,
) (ctrl.Result, error) {
	resolver := func(ctx context.Context) ([]soteriav1alpha1.ShadowPVEntry, error) {
		return r.resolvePVsForVGR(ctx, vgr)
	}
	return r.reconcileReplicationObject(ctx, vgr, true, resolver)
}

func (r *ShadowPVPublisherReconciler) reconcileReplicationObject(
	ctx context.Context, obj client.Object, fullReplace bool, resolve pvResolver,
) (ctrl.Result, error) {
	planName := obj.GetLabels()[drivers.LabelDRPlan]
	vgName := obj.GetLabels()[csiextension.LabelVolumeGroup]
	if planName == "" || vgName == "" {
		return ctrl.Result{}, nil
	}

	if !obj.GetDeletionTimestamp().IsZero() {
		return r.handleDeletion(ctx, obj, planName, vgName, resolve, fullReplace)
	}

	if !controllerutil.ContainsFinalizer(obj, publisherFinalizer) {
		patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
		controllerutil.AddFinalizer(obj, publisherFinalizer)
		if err := r.Patch(ctx, obj, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer to %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	}

	entries, err := resolve(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = retry.RetryOnConflict(engine.ScyllaRetry, func() error {
		return r.reconcileShadowPV(ctx, planName, vgName, entries, fullReplace)
	})
	return ctrl.Result{}, err
}

func (r *ShadowPVPublisherReconciler) handleDeletion(
	ctx context.Context, obj client.Object, planName, vgName string,
	resolve pvResolver, fullReplace bool,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(obj, publisherFinalizer) {
		return ctrl.Result{}, nil
	}

	shadowPVName := planName + "-" + vgName

	// Attempt to resolve published PVs for targeted entry removal.
	// Falls back to removing all local entries if resolution fails.
	resolvedEntries, resolveErr := resolve(ctx)
	if resolveErr != nil {
		logger.V(1).Info("Could not resolve PVs during deletion, will remove all local entries",
			"shadowpv", shadowPVName, "error", resolveErr)
	}

	err := retry.RetryOnConflict(engine.ScyllaRetry, func() error {
		var spv soteriav1alpha1.ShadowPV
		if err := r.Get(ctx, client.ObjectKey{Name: shadowPVName}, &spv); err != nil {
			if errors.IsNotFound(err) {
				logger.Info("ShadowPV already deleted", "shadowpv", shadowPVName)
				return nil
			}
			return fmt.Errorf("getting ShadowPV %s for deletion: %w", shadowPVName, err)
		}

		var merged []soteriav1alpha1.ShadowPVEntry
		if fullReplace || resolveErr != nil || len(resolvedEntries) == 0 {
			merged = removeLocalEntries(spv.Spec.PVs, r.LocalSite)
		} else {
			pvNames := make(map[string]struct{}, len(resolvedEntries))
			for _, e := range resolvedEntries {
				pvNames[e.PVName] = struct{}{}
			}
			merged = removeEntriesByPVName(spv.Spec.PVs, r.LocalSite, pvNames)
		}

		if len(merged) == 0 {
			if err := r.Delete(ctx, &spv); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("deleting empty ShadowPV %s: %w", shadowPVName, err)
			}
			logger.Info("Deleted ShadowPV (no entries remain)", "shadowpv", shadowPVName)
			return nil
		}

		if len(merged) != len(spv.Spec.PVs) {
			patch := client.MergeFrom(spv.DeepCopy())
			spv.Spec.PVs = merged
			if err := r.Patch(ctx, &spv, patch); err != nil {
				return fmt.Errorf("patching ShadowPV %s: %w", shadowPVName, err)
			}
			logger.Info("Removed entries from ShadowPV", "shadowpv", shadowPVName, "remaining", len(merged))
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	controllerutil.RemoveFinalizer(obj, publisherFinalizer)
	if err := r.Patch(ctx, obj, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *ShadowPVPublisherReconciler) resolvePVsForVR(
	ctx context.Context, vr *replicationv1alpha1.VolumeReplication,
) ([]soteriav1alpha1.ShadowPVEntry, error) {
	logger := log.FromContext(ctx)

	pvcName := vr.Spec.DataSource.Name
	var pvc corev1.PersistentVolumeClaim
	if err := r.APIReader.Get(ctx, client.ObjectKey{
		Namespace: vr.Namespace, Name: pvcName,
	}, &pvc); err != nil {
		return nil, fmt.Errorf("getting PVC %s/%s: %w", vr.Namespace, pvcName, err)
	}

	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		logger.Info("PVC not bound, skipping", "pvc", pvcName)
		return nil, nil
	}

	var pv corev1.PersistentVolume
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: pvName}, &pv); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PV not found, skipping", "pv", pvName)
			return nil, nil
		}
		return nil, fmt.Errorf("getting PV %s: %w", pvName, err)
	}

	return []soteriav1alpha1.ShadowPVEntry{{
		ClusterName: r.LocalSite,
		PVName:      pv.Name,
		PV:          pv.Spec,
	}}, nil
}

func (r *ShadowPVPublisherReconciler) resolvePVsForVGR(
	ctx context.Context, vgr *replicationv1alpha1.VolumeGroupReplication,
) ([]soteriav1alpha1.ShadowPVEntry, error) {
	logger := log.FromContext(ctx)

	vgName := vgr.Labels[csiextension.LabelVolumeGroup]
	if vgName == "" {
		return nil, nil
	}

	var pvcList corev1.PersistentVolumeClaimList
	if err := r.APIReader.List(ctx, &pvcList,
		client.InNamespace(vgr.Namespace),
		client.MatchingLabels{csiextension.LabelVolumeGroup: vgName},
	); err != nil {
		return nil, fmt.Errorf("listing PVCs for VG %s: %w", vgName, err)
	}

	var entries []soteriav1alpha1.ShadowPVEntry
	for _, pvc := range pvcList.Items {
		if pvc.Spec.VolumeName == "" {
			logger.Info("PVC not bound, skipping", "pvc", pvc.Name)
			continue
		}
		var pv corev1.PersistentVolume
		if err := r.APIReader.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, &pv); err != nil {
			if errors.IsNotFound(err) {
				logger.Info("PV not found, skipping", "pv", pvc.Spec.VolumeName, "pvc", pvc.Name)
				continue
			}
			return nil, fmt.Errorf("getting PV %s for PVC %s: %w", pvc.Spec.VolumeName, pvc.Name, err)
		}
		entries = append(entries, soteriav1alpha1.ShadowPVEntry{
			ClusterName: r.LocalSite,
			PVName:      pv.Name,
			PV:          pv.Spec,
		})
	}
	return entries, nil
}

func (r *ShadowPVPublisherReconciler) reconcileShadowPV(
	ctx context.Context, planName, vgName string,
	entries []soteriav1alpha1.ShadowPVEntry,
	fullReplace bool,
) error {
	shadowPVName := planName + "-" + vgName
	logger := log.FromContext(ctx).WithValues("shadowpv", shadowPVName)

	var spv soteriav1alpha1.ShadowPV
	err := r.Get(ctx, client.ObjectKey{Name: shadowPVName}, &spv)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("getting ShadowPV %s: %w", shadowPVName, err)
	}

	if errors.IsNotFound(err) {
		if len(entries) == 0 {
			return nil
		}
		spv = soteriav1alpha1.ShadowPV{
			ObjectMeta: metav1.ObjectMeta{
				Name: shadowPVName,
				Labels: map[string]string{
					drivers.LabelDRPlan: planName,
				},
			},
			Spec: soteriav1alpha1.ShadowPVSpec{
				PVs: entries,
			},
		}
		if err := r.Create(ctx, &spv); err != nil {
			return fmt.Errorf("creating ShadowPV %s: %w", shadowPVName, err)
		}
		logger.Info("Created ShadowPV", "entryCount", len(entries))
		return nil
	}

	merged := mergeEntries(spv.Spec.PVs, entries, r.LocalSite, fullReplace)

	if len(merged) == 0 {
		if err := r.Delete(ctx, &spv); err != nil {
			return client.IgnoreNotFound(fmt.Errorf("deleting empty ShadowPV %s: %w", shadowPVName, err))
		}
		logger.Info("Deleted ShadowPV (no entries remain)")
		return nil
	}

	if entriesEqual(spv.Spec.PVs, merged) {
		return nil
	}

	patch := client.MergeFrom(spv.DeepCopy())
	spv.Spec.PVs = merged
	if err := r.Patch(ctx, &spv, patch); err != nil {
		return fmt.Errorf("patching ShadowPV %s: %w", shadowPVName, err)
	}
	logger.Info("Updated ShadowPV entries", "entryCount", len(merged))
	return nil
}

func mergeEntries(
	existing []soteriav1alpha1.ShadowPVEntry,
	localEntries []soteriav1alpha1.ShadowPVEntry,
	localSite string,
	fullReplace bool,
) []soteriav1alpha1.ShadowPVEntry {
	if fullReplace {
		var merged []soteriav1alpha1.ShadowPVEntry
		for _, e := range existing {
			if e.ClusterName != localSite {
				merged = append(merged, e)
			}
		}
		merged = append(merged, localEntries...)
		return merged
	}
	// Keyed upsert: only replace local entries whose pvName appears in the new
	// set, preserving sibling entries from other VRs in the same volume group.
	updating := make(map[string]struct{}, len(localEntries))
	for _, e := range localEntries {
		updating[e.PVName] = struct{}{}
	}
	var merged []soteriav1alpha1.ShadowPVEntry
	for _, e := range existing {
		if e.ClusterName == localSite {
			if _, replaced := updating[e.PVName]; replaced {
				continue
			}
		}
		merged = append(merged, e)
	}
	merged = append(merged, localEntries...)
	return merged
}

func removeLocalEntries(
	existing []soteriav1alpha1.ShadowPVEntry,
	localSite string,
) []soteriav1alpha1.ShadowPVEntry {
	var remaining []soteriav1alpha1.ShadowPVEntry
	for _, e := range existing {
		if e.ClusterName != localSite {
			remaining = append(remaining, e)
		}
	}
	return remaining
}

func removeEntriesByPVName(
	existing []soteriav1alpha1.ShadowPVEntry,
	localSite string,
	pvNames map[string]struct{},
) []soteriav1alpha1.ShadowPVEntry {
	var remaining []soteriav1alpha1.ShadowPVEntry
	for _, e := range existing {
		if e.ClusterName == localSite {
			if _, remove := pvNames[e.PVName]; remove {
				continue
			}
		}
		remaining = append(remaining, e)
	}
	return remaining
}

func entriesEqual(a, b []soteriav1alpha1.ShadowPVEntry) bool {
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]soteriav1alpha1.ShadowPVEntry, len(a))
	for _, e := range a {
		am[e.ClusterName+"/"+e.PVName] = e
	}
	for _, e := range b {
		existing, ok := am[e.ClusterName+"/"+e.PVName]
		if !ok {
			return false
		}
		if !equality.Semantic.DeepEqual(existing.PV, e.PV) {
			return false
		}
	}
	return true
}

// SetupWithManager registers the ShadowPV publisher controller. It watches
// VolumeReplication and VolumeGroupReplication resources filtered by the
// presence of a soteria.io/drplan label. No .For() is used because neither
// VR nor VGR is the "primary" type.
func (r *ShadowPVPublisherReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasDRPlanLabel := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetLabels()[drivers.LabelDRPlan] != ""
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("shadowpv-publisher").
		Watches(
			&replicationv1alpha1.VolumeReplication{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(hasDRPlanLabel),
		).
		Watches(
			&replicationv1alpha1.VolumeGroupReplication{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(hasDRPlanLabel),
		).
		Complete(r)
}
