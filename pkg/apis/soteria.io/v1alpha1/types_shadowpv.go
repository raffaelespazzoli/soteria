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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ShadowPV shares PersistentVolume manifests between clusters for cross-site
// volume provisioning. Each entry represents a PV discovered on one cluster
// that should be pre-provisioned on the peer cluster (with pool-ID rewrite
// for Ceph). Named after the DRPlan + VolumeGroup it represents, e.g.,
// "<plan-name>-<vg-name>".
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
type ShadowPV struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShadowPVSpec   `json:"spec"`
	Status ShadowPVStatus `json:"status,omitempty"`
}

type ShadowPVSpec struct {
	// PVs is the list of PV entries from different clusters.
	// +listType=atomic
	PVs []ShadowPVEntry `json:"pvs"`
}

type ShadowPVEntry struct {
	// ClusterName identifies which cluster published this PV entry.
	ClusterName string `json:"clusterName"`
	// PVName is the desired PV name for creation on remote clusters.
	PVName string `json:"pvName"`
	// PV holds the PersistentVolume spec (not full PV — avoids unnecessary metadata).
	PV corev1.PersistentVolumeSpec `json:"pv"`
}

type ShadowPVStatus struct {
	// Conditions represent the latest observations of the ShadowPV state.
	// Used by consumer controller to report PV conflicts and provisioning status.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ShadowPVList contains a list of ShadowPVs.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
type ShadowPVList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShadowPV `json:"items"`
}
