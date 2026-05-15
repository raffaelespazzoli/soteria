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

package csiextension

import (
	"time"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// mapRole converts a CSI Addons status.state to a Soteria VolumeRole.
func mapRole(state replicationv1alpha1.State) drivers.VolumeRole {
	switch state {
	case replicationv1alpha1.PrimaryState:
		return drivers.RoleSource
	case replicationv1alpha1.SecondaryState:
		return drivers.RoleTarget
	default:
		return drivers.RoleNonReplicated
	}
}

// mapHealth determines the Soteria ReplicationHealth from CSI Addons
// status conditions. The mapping uses condition priorities: Degraded and
// Resyncing take precedence over Completed because they represent
// active problems or transitions.
func mapHealth(conditions []metav1.Condition) drivers.ReplicationHealth {
	var completed, degraded, resyncing *metav1.Condition
	for i := range conditions {
		switch conditions[i].Type {
		case replicationv1alpha1.ConditionCompleted:
			completed = &conditions[i]
		case replicationv1alpha1.ConditionDegraded:
			degraded = &conditions[i]
		case replicationv1alpha1.ConditionResyncing:
			resyncing = &conditions[i]
		}
	}

	if degraded != nil && degraded.Status == metav1.ConditionTrue {
		return drivers.HealthDegraded
	}
	if resyncing != nil && resyncing.Status == metav1.ConditionTrue {
		return drivers.HealthSyncing
	}
	if completed != nil && completed.Status == metav1.ConditionTrue {
		return drivers.HealthHealthy
	}
	return drivers.HealthUnknown
}

// healthPriority assigns a numeric priority to each ReplicationHealth value.
// Lower values are "worse" — used by worstHealth to aggregate across CRs.
var healthPriority = map[drivers.ReplicationHealth]int{
	drivers.HealthUnknown:        0,
	drivers.HealthDegraded:       1,
	drivers.HealthSyncing:        2,
	drivers.HealthNotReplicating: 3,
	drivers.HealthHealthy:        4,
}

// worstHealth returns the worst health across a set of health values.
// "Worst" means the lowest priority — Unknown < Degraded < Syncing <
// NotReplicating < Healthy.
func worstHealth(healths []drivers.ReplicationHealth) drivers.ReplicationHealth {
	worst := drivers.HealthHealthy
	for _, h := range healths {
		if healthPriority[h] < healthPriority[worst] {
			worst = h
		}
	}
	return worst
}

// oldestSyncTime returns the oldest (minimum) time from a set of sync
// time pointers. Nil entries are skipped. Returns nil if all are nil.
func oldestSyncTime(times []*metav1.Time) *time.Time {
	var oldest *time.Time
	for _, t := range times {
		if t == nil {
			continue
		}
		tt := t.Time
		if oldest == nil || tt.Before(*oldest) {
			oldest = &tt
		}
	}
	return oldest
}

// statusFromVR maps a single VolumeReplication's status to a Soteria
// ReplicationStatus.
func statusFromVR(vr *replicationv1alpha1.VolumeReplication) drivers.ReplicationStatus {
	var syncTime *time.Time
	if vr.Status.LastSyncTime != nil {
		t := vr.Status.LastSyncTime.Time
		syncTime = &t
	}
	return drivers.ReplicationStatus{
		Role:         mapRole(vr.Status.State),
		Health:       mapHealth(vr.Status.Conditions),
		LastSyncTime: syncTime,
	}
}

// aggregateVRStatus aggregates status across multiple VolumeReplication CRs
// belonging to the same single-VM volume group. Health uses worst-wins,
// role comes from the first CR, and LastSyncTime is the oldest.
func aggregateVRStatus(vrs []replicationv1alpha1.VolumeReplication) drivers.ReplicationStatus {
	if len(vrs) == 0 {
		return drivers.ReplicationStatus{
			Role:   drivers.RoleNonReplicated,
			Health: drivers.HealthNotReplicating,
		}
	}

	if len(vrs) == 1 {
		return statusFromVR(&vrs[0])
	}

	healths := make([]drivers.ReplicationHealth, len(vrs))
	syncTimes := make([]*metav1.Time, len(vrs))
	for i := range vrs {
		healths[i] = mapHealth(vrs[i].Status.Conditions)
		syncTimes[i] = vrs[i].Status.LastSyncTime
	}

	return drivers.ReplicationStatus{
		Role:         mapRole(vrs[0].Status.State),
		Health:       worstHealth(healths),
		LastSyncTime: oldestSyncTime(syncTimes),
	}
}

// statusFromVGR maps a single VolumeGroupReplication's status to a Soteria
// ReplicationStatus. VGR embeds VolumeReplicationStatus, so the same
// fields are available.
func statusFromVGR(vgr *replicationv1alpha1.VolumeGroupReplication) drivers.ReplicationStatus {
	var syncTime *time.Time
	if vgr.Status.LastSyncTime != nil {
		t := vgr.Status.LastSyncTime.Time
		syncTime = &t
	}
	return drivers.ReplicationStatus{
		Role:         mapRole(vgr.Status.State),
		Health:       mapHealth(vgr.Status.Conditions),
		LastSyncTime: syncTime,
	}
}
