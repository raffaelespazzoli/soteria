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

// health.go implements per-volume-group replication health polling for the
// DRPlan controller (FR31/FR32). On each reconcile cycle the controller
// resolves a storage driver and VolumeGroupID for every resolved VG, polls
// GetReplicationStatus, and maps the result into VolumeGroupHealth entries
// on DRPlanStatus. Health transitions emit Kubernetes events and a degraded
// aggregate triggers shorter requeue intervals (30s vs 10min).

package drplan

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

const (
	conditionTypeReplicationHealthy = "ReplicationHealthy"
	reasonAllHealthy                = "AllHealthy"
	reasonDegraded                  = "Degraded"

	degradedRequeueInterval = 30 * time.Second

	rpoUnknown = "unknown"
)

// pollReplicationHealth resolves a driver and VolumeGroupID for each VG in
// the plan's waves, calls GetReplicationStatus, and returns per-VG health.
// Errors are handled gracefully per-VG rather than failing the entire poll.
func (r *DRPlanReconciler) pollReplicationHealth(
	ctx context.Context,
	_ *soteriav1alpha1.DRPlan,
	waves []soteriav1alpha1.WaveInfo,
) []soteriav1alpha1.VolumeGroupHealth {
	logger := log.FromContext(ctx)
	now := metav1.Now()

	var totalVGs int
	for _, wave := range waves {
		totalVGs += len(wave.Groups)
	}
	results := make([]soteriav1alpha1.VolumeGroupHealth, 0, totalVGs)
	for _, wave := range waves {
		for _, vg := range wave.Groups {
			health := r.pollSingleVG(ctx, vg, now)
			results = append(results, health)
			logger.V(1).Info("Polled replication health",
				"vg", vg.Name, "namespace", vg.Namespace,
				"health", health.Health, "rpo", health.EstimatedRPO)
		}
	}
	return results
}

// pollSingleVG resolves the driver and VG ID for a single volume group, polls
// replication status, and maps the result to VolumeGroupHealth.
func (r *DRPlanReconciler) pollSingleVG(
	ctx context.Context,
	vg soteriav1alpha1.VolumeGroupInfo,
	now metav1.Time,
) soteriav1alpha1.VolumeGroupHealth {
	logger := log.FromContext(ctx)

	drv, fallback, err := r.resolveDriverForVG(ctx, vg)
	if err != nil {
		logger.V(1).Info("Could not resolve driver for volume group",
			"vg", vg.Name, "error", err)
		return soteriav1alpha1.VolumeGroupHealth{
			Name:         vg.Name,
			Namespace:    vg.Namespace,
			Health:       soteriav1alpha1.HealthStatusUnknown,
			EstimatedRPO: rpoUnknown,
			LastChecked:  now,
			Message:      fmt.Sprintf("driver resolution failed: %v", err),
		}
	}

	vgID, err := r.resolveVolumeGroupID(ctx, drv, vg)
	if err != nil {
		logger.V(1).Info("Could not resolve volume group ID",
			"vg", vg.Name, "error", err)
		return soteriav1alpha1.VolumeGroupHealth{
			Name:         vg.Name,
			Namespace:    vg.Namespace,
			Health:       soteriav1alpha1.HealthStatusUnknown,
			EstimatedRPO: rpoUnknown,
			LastChecked:  now,
			Message:      fmt.Sprintf("volume group ID resolution failed: %v", err),
		}
	}

	status, err := drv.GetReplicationStatus(ctx, vgID)
	if err != nil {
		logger.V(1).Info("GetReplicationStatus failed",
			"vg", vg.Name, "error", err)
		return soteriav1alpha1.VolumeGroupHealth{
			Name:         vg.Name,
			Namespace:    vg.Namespace,
			Health:       soteriav1alpha1.HealthStatusError,
			EstimatedRPO: rpoUnknown,
			LastChecked:  now,
			Message:      err.Error(),
		}
	}

	h := mapReplicationStatus(vg, status, now)
	if fallback {
		h.Message = "no PVC storage class found, using fallback driver"
	}
	return h
}

// resolveDriverForVG resolves the StorageProvider for a VG by iterating all
// VM PVCs to find a storage class and looking up the driver in the registry.
// Follows the same iteration pattern as WaveExecutor.resolveVGStorageClass:
// walk every VM in the group, every PVC in that VM, and return the first
// non-empty storage class match. All VMs in a VG share the same storage class
// (validated upstream by ResolveVolumeGroups). The fallback return indicates
// that no PVC storage class was found and the registry fallback was used.
func (r *DRPlanReconciler) resolveDriverForVG(
	ctx context.Context,
	vg soteriav1alpha1.VolumeGroupInfo,
) (drv drivers.StorageProvider, fallback bool, err error) {
	if len(vg.VMNames) == 0 {
		return nil, false, fmt.Errorf("volume group %q has no VMs", vg.Name)
	}

	if r.PVCResolver == nil {
		d, e := r.Registry.GetDriver("")
		return d, true, e
	}

	for _, vmName := range vg.VMNames {
		pvcNames, pvcErr := r.PVCResolver.ResolvePVCNames(ctx, vmName, vg.Namespace)
		if pvcErr != nil {
			return nil, false, fmt.Errorf("resolving PVCs for VM %s/%s: %w", vg.Namespace, vmName, pvcErr)
		}
		for _, pvcName := range pvcNames {
			pvc, pvcErr := r.getPVC(ctx, vg.Namespace, pvcName)
			if pvcErr != nil {
				return nil, false, fmt.Errorf("fetching PVC %s/%s: %w", vg.Namespace, pvcName, pvcErr)
			}
			scName := ""
			if pvc.Spec.StorageClassName != nil {
				scName = *pvc.Spec.StorageClassName
			}
			if scName != "" {
				d, e := r.Registry.GetDriverForPVC(ctx, scName, r.SCLister)
				return d, false, e
			}
		}
	}

	d, e := r.Registry.GetDriver("")
	return d, true, e
}

// getPVC fetches a PVC by namespace/name using the controller-runtime client.
func (r *DRPlanReconciler) getPVC(
	ctx context.Context, namespace, name string,
) (*corev1.PersistentVolumeClaim, error) {
	var pvc corev1.PersistentVolumeClaim
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, &pvc); err != nil {
		return nil, err
	}
	return &pvc, nil
}

// resolveVolumeGroupID obtains the driver-level VolumeGroupID via the
// idempotent CreateVolumeGroup call, following the same pattern as
// FailoverHandler.resolveVolumeGroupID.
func (r *DRPlanReconciler) resolveVolumeGroupID(
	ctx context.Context,
	drv drivers.StorageProvider,
	vg soteriav1alpha1.VolumeGroupInfo,
) (drivers.VolumeGroupID, error) {
	var pvcNames []string
	if r.PVCResolver != nil {
		for _, vmName := range vg.VMNames {
			names, err := r.PVCResolver.ResolvePVCNames(ctx, vmName, vg.Namespace)
			if err != nil {
				return "", fmt.Errorf("resolving PVC names for VM %s/%s: %w", vg.Namespace, vmName, err)
			}
			pvcNames = append(pvcNames, names...)
		}
	}

	info, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      vg.Name,
		Namespace: vg.Namespace,
		PVCNames:  pvcNames,
	})
	if err != nil {
		return "", fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
	}
	return info.ID, nil
}

// mapReplicationStatus converts a driver ReplicationStatus into a
// VolumeGroupHealth entry with health enum mapping and RPO computation.
func mapReplicationStatus(
	vg soteriav1alpha1.VolumeGroupInfo,
	status drivers.ReplicationStatus,
	now metav1.Time,
) soteriav1alpha1.VolumeGroupHealth {
	h := soteriav1alpha1.VolumeGroupHealth{
		Name:        vg.Name,
		Namespace:   vg.Namespace,
		LastChecked: now,
	}

	switch status.Health {
	case drivers.HealthHealthy:
		h.Health = soteriav1alpha1.HealthStatusHealthy
	case drivers.HealthDegraded:
		h.Health = soteriav1alpha1.HealthStatusDegraded
	case drivers.HealthSyncing:
		h.Health = soteriav1alpha1.HealthStatusSyncing
	default:
		h.Health = soteriav1alpha1.HealthStatusUnknown
	}

	if status.LastSyncTime != nil {
		t := metav1.NewTime(*status.LastSyncTime)
		h.LastSyncTime = &t
	}

	h.EstimatedRPO = computeRPO(status)

	return h
}

// computeRPO determines the RPO string from driver-provided data.
// Priority: driver EstimatedRPO > computed from LastSyncTime > "unknown".
func computeRPO(status drivers.ReplicationStatus) string {
	if status.EstimatedRPO != nil {
		return formatDuration(*status.EstimatedRPO)
	}
	if status.LastSyncTime != nil {
		return formatDuration(time.Since(*status.LastSyncTime))
	}
	return rpoUnknown
}

// formatDuration formats a duration as a human-readable Go duration string,
// truncating to seconds for readability.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}

// computeReplicationCondition builds the aggregate ReplicationHealthy
// condition from a set of VolumeGroupHealth entries. Returns nil when no
// VGs were polled (condition should be omitted).
func computeReplicationCondition(
	health []soteriav1alpha1.VolumeGroupHealth,
	generation int64,
) *metav1.Condition {
	if len(health) == 0 {
		return nil
	}

	var degradedVGs []string
	for _, h := range health {
		if h.Health != soteriav1alpha1.HealthStatusHealthy {
			degradedVGs = append(degradedVGs, h.Namespace+"/"+h.Name)
		}
	}

	if len(degradedVGs) == 0 {
		return &metav1.Condition{
			Type:               conditionTypeReplicationHealthy,
			Status:             metav1.ConditionTrue,
			Reason:             reasonAllHealthy,
			Message:            "All volume groups report healthy replication",
			ObservedGeneration: generation,
		}
	}

	msg := fmt.Sprintf("Non-healthy volume groups: %s", joinMax(degradedVGs, 5))
	return &metav1.Condition{
		Type:               conditionTypeReplicationHealthy,
		Status:             metav1.ConditionFalse,
		Reason:             reasonDegraded,
		Message:            msg,
		ObservedGeneration: generation,
	}
}

// joinMax joins up to max strings with ", " and appends "... and N more"
// if the list is truncated.
func joinMax(items []string, max int) string {
	if len(items) <= max {
		var result strings.Builder
		for i, item := range items {
			if i > 0 {
				result.WriteString(", ")
			}
			result.WriteString(item)
		}
		return result.String()
	}
	var result strings.Builder
	for i := range max {
		if i > 0 {
			result.WriteString(", ")
		}
		result.WriteString(items[i])
	}
	return fmt.Sprintf("%s ... and %d more", result.String(), len(items)-max)
}

// detectHealthTransitions compares old and new VG health to find VGs whose
// health status changed. Returns lists of VGs that degraded and VGs that
// recovered, for event emission.
func detectHealthTransitions(
	oldHealth, newHealth []soteriav1alpha1.VolumeGroupHealth,
) (degraded, recovered []soteriav1alpha1.VolumeGroupHealth) {
	if len(oldHealth) == 0 {
		return nil, nil
	}

	oldMap := make(map[string]soteriav1alpha1.VolumeGroupHealthStatus, len(oldHealth))
	for _, h := range oldHealth {
		oldMap[h.Namespace+"/"+h.Name] = h.Health
	}

	for _, h := range newHealth {
		key := h.Namespace + "/" + h.Name
		prev, existed := oldMap[key]
		if !existed {
			continue
		}
		if prev == soteriav1alpha1.HealthStatusHealthy && h.Health != soteriav1alpha1.HealthStatusHealthy {
			degraded = append(degraded, h)
		}
		if prev != soteriav1alpha1.HealthStatusHealthy && h.Health == soteriav1alpha1.HealthStatusHealthy {
			recovered = append(recovered, h)
		}
	}
	return degraded, recovered
}

// anyNonHealthy returns true if any VG reports a non-Healthy status.
func anyNonHealthy(health []soteriav1alpha1.VolumeGroupHealth) bool {
	for _, h := range health {
		if h.Health != soteriav1alpha1.HealthStatusHealthy {
			return true
		}
	}
	return false
}

// allHealthy returns true if every VG reports Healthy status.
func allHealthy(health []soteriav1alpha1.VolumeGroupHealth) bool {
	for _, h := range health {
		if h.Health != soteriav1alpha1.HealthStatusHealthy {
			return false
		}
	}
	return true
}
