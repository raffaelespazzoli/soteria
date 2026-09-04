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
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/csiextension"
	"github.com/soteria-project/soteria/pkg/engine"
)

const consumerLabel = "soteria.io/shadowpv-consumer"

// +kubebuilder:rbac:groups=soteria.io,resources=shadowpvs,verbs=get;list;watch
// +kubebuilder:rbac:groups=soteria.io,resources=shadowpvs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// ShadowPVConsumerReconciler watches ShadowPV resources, identifies entries
// from remote clusters, and creates local PVs with Ceph pool-ID rewrite so
// that mirrored RBD images have corresponding PVs on the target cluster.
type ShadowPVConsumerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	LocalSite     string
	APIReader     client.Reader
	EventRecorder record.EventRecorder
}

func (r *ShadowPVConsumerReconciler) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var spv soteriav1alpha1.ShadowPV
	if err := r.Get(ctx, req.NamespacedName, &spv); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting ShadowPV %s: %w", req.Name, err)
	}

	poolCache := make(map[string]int)
	var reconcileErr error
	for _, entry := range spv.Spec.PVs {
		if entry.ClusterName == r.LocalSite {
			continue
		}
		if err := r.reconcilePV(ctx, &spv, entry, poolCache); err != nil {
			logger.Error(err, "Could not reconcile PV for remote entry",
				"pv", entry.PVName, "sourceCluster", entry.ClusterName)
			reconcileErr = err
		}
	}

	return ctrl.Result{}, reconcileErr
}

func (r *ShadowPVConsumerReconciler) reconcilePV(
	ctx context.Context, spv *soteriav1alpha1.ShadowPV,
	entry soteriav1alpha1.ShadowPVEntry, poolCache map[string]int,
) error {
	logger := log.FromContext(ctx).WithValues("pv", entry.PVName)

	var existingPV corev1.PersistentVolume
	err := r.APIReader.Get(ctx, client.ObjectKey{Name: entry.PVName}, &existingPV)
	if err == nil {
		if existingPV.Labels[consumerLabel] == spv.Name {
			return nil
		}
		r.EventRecorder.Eventf(spv, corev1.EventTypeWarning, "PVConflict",
			"PV %s exists but was not created by ShadowPV consumer (missing label %s=%s)",
			entry.PVName, consumerLabel, spv.Name)
		return r.setConflictConditionWithRetry(ctx, spv, entry.PVName)
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("checking PV %s existence: %w", entry.PVName, err)
	}

	pvSpec := entry.PV.DeepCopy()
	if pvSpec.CSI != nil && pvSpec.CSI.VolumeHandle != "" {
		if _, parseErr := csiextension.ParseVolumeHandle(pvSpec.CSI.VolumeHandle); parseErr != nil {
			r.EventRecorder.Eventf(spv, corev1.EventTypeWarning, "PoolIDRewriteSkipped",
				"PV %s has non-Ceph volume handle format — creating with original handle",
				entry.PVName)
		} else {
			rewritten, rewriteErr := r.rewriteVolumeHandle(ctx, pvSpec.CSI.VolumeHandle, pvSpec.CSI.VolumeAttributes, poolCache)
			if rewriteErr != nil {
				return fmt.Errorf("rewriting pool-ID for PV %s: %w", entry.PVName, rewriteErr)
			}
			pvSpec.CSI.VolumeHandle = rewritten
		}
	}

	// Clear the source cluster's ClaimRef UID so the PV starts as Available
	// rather than Released (which would cause the PV controller to delete it
	// when ReclaimPolicy is Delete). Keep Name+Namespace to reserve the PV
	// for the intended PVC on the target cluster.
	if pvSpec.ClaimRef != nil {
		pvSpec.ClaimRef.UID = ""
		pvSpec.ClaimRef.ResourceVersion = ""
	}
	pvSpec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: entry.PVName,
			Labels: map[string]string{
				consumerLabel:       spv.Name,
				drivers.LabelDRPlan: spv.Labels[drivers.LabelDRPlan],
			},
		},
		Spec: *pvSpec,
	}
	if err := r.Create(ctx, pv); err != nil {
		if errors.IsAlreadyExists(err) {
			var racePV corev1.PersistentVolume
			if getErr := r.APIReader.Get(ctx, client.ObjectKey{Name: entry.PVName}, &racePV); getErr != nil {
				return fmt.Errorf("checking PV %s after create race: %w", entry.PVName, getErr)
			}
			if racePV.Labels[consumerLabel] == spv.Name {
				return nil
			}
			r.EventRecorder.Eventf(spv, corev1.EventTypeWarning, "PVConflict",
				"PV %s exists but was not created by ShadowPV consumer (missing label %s=%s)",
				entry.PVName, consumerLabel, spv.Name)
			return r.setConflictConditionWithRetry(ctx, spv, entry.PVName)
		}
		return fmt.Errorf("creating PV %s: %w", entry.PVName, err)
	}

	logger.Info("Created PV from ShadowPV entry",
		"shadowpv", spv.Name, "sourceCluster", entry.ClusterName)
	return nil
}

func (r *ShadowPVConsumerReconciler) rewriteVolumeHandle(
	ctx context.Context,
	volumeHandle string, volumeAttributes map[string]string,
	poolCache map[string]int,
) (string, error) {
	poolName := volumeAttributes["pool"]
	if poolName == "" {
		return "", fmt.Errorf("no pool name in volumeAttributes for handle %s", volumeHandle)
	}

	cephNS := volumeAttributes["clusterID"]
	if cephNS == "" {
		return "", fmt.Errorf("no clusterID in volumeAttributes for handle %s", volumeHandle)
	}

	localPoolID, err := r.resolveLocalPoolID(ctx, poolName, cephNS, poolCache)
	if err != nil {
		return "", err
	}

	rewritten, err := csiextension.RewritePoolID(volumeHandle, localPoolID)
	if err != nil {
		return "", fmt.Errorf("rewriting pool-ID: %w", err)
	}
	return rewritten, nil
}

func (r *ShadowPVConsumerReconciler) resolveLocalPoolID(
	ctx context.Context, poolName string, cephNamespace string, cache map[string]int,
) (int, error) {
	cacheKey := cephNamespace + "/" + poolName
	if id, ok := cache[cacheKey]; ok {
		return id, nil
	}

	var cbp unstructured.Unstructured
	cbp.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "ceph.rook.io", Version: "v1", Kind: "CephBlockPool",
	})
	ns := cephNamespace
	if ns == "" {
		ns = "rook-ceph"
	}
	if err := r.APIReader.Get(ctx, client.ObjectKey{
		Namespace: ns, Name: poolName,
	}, &cbp); err != nil {
		return 0, fmt.Errorf("getting CephBlockPool %s: %w", poolName, err)
	}

	// Prefer status.poolID (int, newer Rook versions)
	if poolID, found, err := unstructured.NestedInt64(cbp.Object, "status", "poolID"); err == nil && found {
		cache[cacheKey] = int(poolID)
		return int(poolID), nil
	}

	// Fall back to status.info.poolNumber (string, Rook v1.14+)
	info, _, _ := unstructured.NestedStringMap(cbp.Object, "status", "info")
	if poolNumStr, ok := info["poolNumber"]; ok {
		poolID, err := strconv.Atoi(poolNumStr)
		if err == nil {
			cache[cacheKey] = poolID
			return poolID, nil
		}
	}

	return 0, fmt.Errorf("CephBlockPool %s/%s has no pool ID in status", ns, poolName)
}

func (r *ShadowPVConsumerReconciler) setConflictConditionWithRetry(
	ctx context.Context, spv *soteriav1alpha1.ShadowPV, pvName string,
) error {
	return retry.RetryOnConflict(engine.ScyllaRetry, func() error {
		var fresh soteriav1alpha1.ShadowPV
		if err := r.Get(ctx, client.ObjectKeyFromObject(spv), &fresh); err != nil {
			return err
		}
		return r.setConflictCondition(ctx, &fresh, pvName)
	})
}

func (r *ShadowPVConsumerReconciler) setConflictCondition(
	ctx context.Context, spv *soteriav1alpha1.ShadowPV, pvName string,
) error {
	patch := client.MergeFrom(spv.DeepCopy())
	meta.SetStatusCondition(&spv.Status.Conditions, metav1.Condition{
		Type:               "PVConflict",
		Status:             metav1.ConditionTrue,
		Reason:             "ExistingPVNotOwnedByConsumer",
		Message:            fmt.Sprintf("PV %s exists but was not created by the ShadowPV consumer controller", pvName),
		ObservedGeneration: spv.Generation,
	})
	return r.Status().Patch(ctx, spv, patch)
}

// SetupWithManager registers the ShadowPV consumer controller. It watches
// ShadowPV resources as the primary type.
func (r *ShadowPVConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("shadowpv-consumer").
		For(&soteriav1alpha1.ShadowPV{}).
		Complete(r)
}
