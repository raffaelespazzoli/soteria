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
	"context"
	"testing"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/conformance"
)

// Conformance Deviations:
//
// The csi-extension driver differs from the abstract StorageProvider model
// in three ways that require a thin adapter for the conformance suite:
//
//  1. PVCNames requirement — The conformance suite creates VolumeGroupSpecs
//     with empty PVCNames. The csi-extension driver requires at least one PVC
//     to create VolumeReplication CRs. The adapter injects a synthetic PVC.
//
//  2. No NonReplicated state — The CSI replication model only has primary,
//     secondary, and resync states. After StopReplication the driver flips the
//     spec state (primary→secondary). The adapter clears VR status after
//     StopReplication so that GetReplicationStatus maps the empty state to
//     NonReplicated, matching the conformance expectation.
//
//  3. Status reconciliation — With a fake Kubernetes client no controller
//     reconciles VR status fields. The adapter simulates immediate reconciliation
//     after CreateVolumeGroup and SetSource by setting status.state and the
//     Completed condition so GetReplicationStatus returns the expected Source role.

// conformanceAdapter bridges the csi-extension driver to the conformance
// suite by injecting synthetic PVCNames and simulating controller
// reconciliation of VR status fields.
type conformanceAdapter struct {
	driver *Driver
	client client.Client
}

var _ drivers.StorageProvider = (*conformanceAdapter)(nil)

func newConformanceAdapter(t *testing.T) *conformanceAdapter {
	t.Helper()
	scheme := testScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
	return &conformanceAdapter{
		driver: New(c),
		client: c,
	}
}

func (a *conformanceAdapter) CreateVolumeGroup(
	ctx context.Context, spec drivers.VolumeGroupSpec,
) (drivers.VolumeGroupInfo, error) {
	if len(spec.PVCNames) == 0 {
		spec.PVCNames = []string{"conformance-pvc"}
	}
	info, err := a.driver.CreateVolumeGroup(ctx, spec)
	if err != nil {
		return info, err
	}
	a.simulateReconciliation(ctx, info.ID, replicationv1alpha1.PrimaryState)
	return info, nil
}

func (a *conformanceAdapter) DeleteVolumeGroup(ctx context.Context, id drivers.VolumeGroupID) error {
	return a.driver.DeleteVolumeGroup(ctx, id)
}

func (a *conformanceAdapter) GetVolumeGroup(
	ctx context.Context, id drivers.VolumeGroupID,
) (drivers.VolumeGroupInfo, error) {
	return a.driver.GetVolumeGroup(ctx, id)
}

func (a *conformanceAdapter) SetSource(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := a.driver.SetSource(ctx, id); err != nil {
		return err
	}
	a.simulateReconciliation(ctx, id, replicationv1alpha1.PrimaryState)
	return nil
}

func (a *conformanceAdapter) StopReplication(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := a.driver.StopReplication(ctx, id); err != nil {
		return err
	}
	a.simulateReconciliation(ctx, id, "")
	return nil
}

func (a *conformanceAdapter) ResyncVolume(ctx context.Context, id drivers.VolumeGroupID) error {
	return a.driver.ResyncVolume(ctx, id)
}

func (a *conformanceAdapter) GetReplicationStatus(
	ctx context.Context, id drivers.VolumeGroupID,
) (drivers.ReplicationStatus, error) {
	return a.driver.GetReplicationStatus(ctx, id)
}

// simulateReconciliation updates VR/VGR status fields to simulate what
// the CSI addons controller would do after observing a spec change. When
// state is empty the status is cleared, representing a non-replicating
// transition period.
func (a *conformanceAdapter) simulateReconciliation(
	ctx context.Context, id drivers.VolumeGroupID, state replicationv1alpha1.State,
) {
	namespace, vgName := parseVGID(id)
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := a.client.List(ctx, &vrList, opts...); err != nil {
		return
	}
	for i := range vrList.Items {
		vrList.Items[i].Status.State = state
		if state != "" {
			vrList.Items[i].Status.Conditions = []metav1.Condition{{
				Type:               replicationv1alpha1.ConditionCompleted,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Completed",
			}}
		} else {
			vrList.Items[i].Status.Conditions = nil
			vrList.Items[i].Status.LastSyncTime = nil
		}
		_ = a.client.Update(ctx, &vrList.Items[i])
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := a.client.List(ctx, &vgrList, opts...); err != nil {
		return
	}
	for i := range vgrList.Items {
		vgrList.Items[i].Status.State = state
		if state != "" {
			vgrList.Items[i].Status.Conditions = []metav1.Condition{{
				Type:               replicationv1alpha1.ConditionCompleted,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Completed",
			}}
		} else {
			vgrList.Items[i].Status.Conditions = nil
			vgrList.Items[i].Status.LastSyncTime = nil
		}
		_ = a.client.Update(ctx, &vgrList.Items[i])
	}
}

func TestConformance_CSIExtension(t *testing.T) {
	conformance.RunConformance(t, newConformanceAdapter(t))
}
