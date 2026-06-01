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
	"errors"
	"fmt"
	"sync"
	"testing"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testDriverWithClient(t *testing.T, objs ...client.Object) (*Driver, client.Client) {
	t.Helper()
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		Build()
	return New(c), c
}

func assertVRCountAndState(
	t *testing.T, c client.Client, namespace, vgName string,
	wantCount int, wantState replicationv1alpha1.ReplicationState,
) {
	t.Helper()
	var vrList replicationv1alpha1.VolumeReplicationList
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	if err := c.List(context.Background(), &vrList, opts...); err != nil {
		t.Fatalf("List VRs: %v", err)
	}
	if len(vrList.Items) != wantCount {
		t.Fatalf("expected %d VR CRs for %s, got %d",
			wantCount, vgName, len(vrList.Items))
	}
	for _, vr := range vrList.Items {
		if vr.Spec.ReplicationState != wantState {
			t.Errorf("VR %s: spec.replicationState = %q, want %q",
				vr.Name, vr.Spec.ReplicationState, wantState)
		}
	}
}

func assertVGRCountAndState(
	t *testing.T, c client.Client, namespace, vgName string,
	wantCount int, wantState replicationv1alpha1.ReplicationState,
) {
	t.Helper()
	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	if err := c.List(context.Background(), &vgrList, opts...); err != nil {
		t.Fatalf("List VGRs: %v", err)
	}
	if len(vgrList.Items) != wantCount {
		t.Fatalf("expected %d VGR CRs for %s, got %d",
			wantCount, vgName, len(vgrList.Items))
	}
	for _, vgr := range vgrList.Items {
		if vgr.Spec.ReplicationState != wantState {
			t.Errorf("VGR %s: spec.replicationState = %q, want %q",
				vgr.Name, vgr.Spec.ReplicationState, wantState)
		}
	}
}

// simulateReconciliation sets status.state and conditions on VR or VGR CRs
// for a volume group, mimicking what the CSI addons controller does after
// observing a spec change. Dispatches to VR or VGR based on isMultiVM.
func simulateReconciliation(
	t *testing.T, c client.Client, namespace, vgName string,
	state replicationv1alpha1.State, conditions []metav1.Condition,
) {
	t.Helper()
	ctx := context.Background()
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	if isMultiVM(vgName) {
		var list replicationv1alpha1.VolumeGroupReplicationList
		if err := c.List(ctx, &list, opts...); err != nil {
			t.Fatalf("List VGRs for reconciliation: %v", err)
		}
		for i := range list.Items {
			list.Items[i].Status.State = state
			list.Items[i].Status.Conditions = conditions
			if state == "" {
				list.Items[i].Status.LastSyncTime = nil
			}
			if err := c.Update(ctx, &list.Items[i]); err != nil {
				t.Fatalf("Update VGR %s status: %v", list.Items[i].Name, err)
			}
		}
		return
	}

	var list replicationv1alpha1.VolumeReplicationList
	if err := c.List(ctx, &list, opts...); err != nil {
		t.Fatalf("List VRs for reconciliation: %v", err)
	}
	for i := range list.Items {
		list.Items[i].Status.State = state
		list.Items[i].Status.Conditions = conditions
		if state == "" {
			list.Items[i].Status.LastSyncTime = nil
		}
		if err := c.Update(ctx, &list.Items[i]); err != nil {
			t.Fatalf("Update VR %s status: %v", list.Items[i].Name, err)
		}
	}
}

func completedConditions() []metav1.Condition {
	return []metav1.Condition{{
		Type:               replicationv1alpha1.ConditionCompleted,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "Completed",
	}}
}

// deleteAllCRs deletes all VR and VGR CRs from the client (simulates external
// deletion). Used by the externally-deleted tests.
func deleteAllCRs(t *testing.T, c client.Client) {
	t.Helper()
	ctx := context.Background()

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := c.List(ctx, &vrList); err != nil {
		t.Fatalf("List VRs: %v", err)
	}
	for i := range vrList.Items {
		if err := c.Delete(ctx, &vrList.Items[i]); err != nil {
			t.Fatalf("Delete VR: %v", err)
		}
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := c.List(ctx, &vgrList); err != nil {
		t.Fatalf("List VGRs: %v", err)
	}
	for i := range vgrList.Items {
		if err := c.Delete(ctx, &vgrList.Items[i]); err != nil {
			t.Fatalf("Delete VGR: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Lifecycle tests (Story 12.6, AC2, AC3)
// ---------------------------------------------------------------------------

func TestLifecycle_SingleVM(t *testing.T) {
	const (
		vgName    = "vm-integ-web01"
		namespace = "integ-default"
	)
	drv, c := testDriverWithClient(t)
	ctx := context.Background()

	info, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      vgName,
		Namespace: namespace,
		PVCNames:  []string{"data", "logs"},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}
	vgID := info.ID
	assertVRCountAndState(t, c, namespace, vgName, 2, ReplicationStatePrimary)

	simulateReconciliation(t, c, namespace, vgName,
		replicationv1alpha1.PrimaryState, completedConditions())
	status, err := drv.GetReplicationStatus(ctx, vgID)
	if err != nil {
		t.Fatalf("GetReplicationStatus after create: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Errorf("Role after create = %q, want Source", status.Role)
	}
	if status.Health != drivers.HealthHealthy {
		t.Errorf("Health after create = %q, want Healthy", status.Health)
	}

	if err := drv.StopReplication(ctx, vgID); err != nil {
		t.Fatalf("StopReplication: %v", err)
	}
	assertVRCountAndState(t, c, namespace, vgName, 2, ReplicationStateSecondary)

	simulateReconciliation(t, c, namespace, vgName, "", nil)
	status, err = drv.GetReplicationStatus(ctx, vgID)
	if err != nil {
		t.Fatalf("GetReplicationStatus after stop: %v", err)
	}
	if status.Role != drivers.RoleNonReplicated {
		t.Errorf("Role after stop = %q, want NonReplicated", status.Role)
	}

	if err := drv.SetSource(ctx, vgID); err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	assertVRCountAndState(t, c, namespace, vgName, 2, ReplicationStatePrimary)

	simulateReconciliation(t, c, namespace, vgName,
		replicationv1alpha1.PrimaryState, completedConditions())
	status, err = drv.GetReplicationStatus(ctx, vgID)
	if err != nil {
		t.Fatalf("GetReplicationStatus after set-source: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Errorf("Role after set-source = %q, want Source", status.Role)
	}

	if err := drv.DeleteVolumeGroup(ctx, vgID); err != nil {
		t.Fatalf("DeleteVolumeGroup: %v", err)
	}
	assertVRCountAndState(t, c, namespace, vgName, 0, "")

	_, err = drv.GetVolumeGroup(ctx, vgID)
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("GetVolumeGroup after delete: got %v, want ErrVolumeGroupNotFound", err)
	}
}

func TestLifecycle_MultiVM(t *testing.T) {
	const (
		vgName    = "ns-integ-db"
		namespace = "integ-db"
	)
	drv, c := testDriverWithClient(t,
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-a", Namespace: namespace},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-b", Namespace: namespace},
		},
	)
	ctx := context.Background()

	info, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      vgName,
		Namespace: namespace,
		PVCNames:  []string{"pvc-a", "pvc-b"},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}
	vgID := info.ID
	assertVGRCountAndState(t, c, namespace, vgName, 1, ReplicationStatePrimary)

	simulateReconciliation(t, c, namespace, vgName,
		replicationv1alpha1.PrimaryState, completedConditions())
	status, err := drv.GetReplicationStatus(ctx, vgID)
	if err != nil {
		t.Fatalf("GetReplicationStatus after create: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Errorf("Role after create = %q, want Source", status.Role)
	}
	if status.Health != drivers.HealthHealthy {
		t.Errorf("Health after create = %q, want Healthy", status.Health)
	}

	if err := drv.StopReplication(ctx, vgID); err != nil {
		t.Fatalf("StopReplication: %v", err)
	}
	assertVGRCountAndState(t, c, namespace, vgName, 1, ReplicationStateSecondary)

	simulateReconciliation(t, c, namespace, vgName, "", nil)
	status, err = drv.GetReplicationStatus(ctx, vgID)
	if err != nil {
		t.Fatalf("GetReplicationStatus after stop: %v", err)
	}
	if status.Role != drivers.RoleNonReplicated {
		t.Errorf("Role after stop = %q, want NonReplicated", status.Role)
	}

	if err := drv.SetSource(ctx, vgID); err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	assertVGRCountAndState(t, c, namespace, vgName, 1, ReplicationStatePrimary)

	simulateReconciliation(t, c, namespace, vgName,
		replicationv1alpha1.PrimaryState, completedConditions())
	status, err = drv.GetReplicationStatus(ctx, vgID)
	if err != nil {
		t.Fatalf("GetReplicationStatus after set-source: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Errorf("Role after set-source = %q, want Source", status.Role)
	}

	if err := drv.DeleteVolumeGroup(ctx, vgID); err != nil {
		t.Fatalf("DeleteVolumeGroup: %v", err)
	}
	assertVGRCountAndState(t, c, namespace, vgName, 0, "")

	_, err = drv.GetVolumeGroup(ctx, vgID)
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("GetVolumeGroup after delete: got %v, want ErrVolumeGroupNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Health mapping integration tests (Story 12.6, AC4)
// ---------------------------------------------------------------------------

func TestHealthMapping_VR(t *testing.T) {
	tests := []struct {
		name       string
		state      replicationv1alpha1.State
		conditions []metav1.Condition
		wantRole   drivers.VolumeRole
		wantHealth drivers.ReplicationHealth
	}{
		{"Primary+Completed", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
			drivers.RoleSource, drivers.HealthHealthy},
		{"Primary+Degraded", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionDegraded)},
			drivers.RoleSource, drivers.HealthDegraded},
		{"Primary+Resyncing", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionResyncing)},
			drivers.RoleSource, drivers.HealthSyncing},
		{"Primary+Empty", replicationv1alpha1.PrimaryState,
			nil, drivers.RoleSource, drivers.HealthUnknown},
		{"Secondary+Completed", replicationv1alpha1.SecondaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
			drivers.RoleTarget, drivers.HealthHealthy},
		{"Secondary+Degraded", replicationv1alpha1.SecondaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionDegraded)},
			drivers.RoleTarget, drivers.HealthDegraded},
		{"Secondary+Resyncing", replicationv1alpha1.SecondaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionResyncing)},
			drivers.RoleTarget, drivers.HealthSyncing},
		{"Secondary+Empty", replicationv1alpha1.SecondaryState,
			nil, drivers.RoleTarget, drivers.HealthUnknown},
		{"Empty+Empty", "",
			nil, drivers.RoleNonReplicated, drivers.HealthUnknown},
		{"Unknown+Empty", replicationv1alpha1.UnknownState,
			nil, drivers.RoleNonReplicated, drivers.HealthUnknown},
		{"Degraded>Resyncing", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{
				conditionTrue(replicationv1alpha1.ConditionDegraded),
				conditionTrue(replicationv1alpha1.ConditionResyncing),
			}, drivers.RoleSource, drivers.HealthDegraded},
		{"Degraded>Completed", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{
				conditionTrue(replicationv1alpha1.ConditionDegraded),
				conditionTrue(replicationv1alpha1.ConditionCompleted),
			}, drivers.RoleSource, drivers.HealthDegraded},
		{"Resyncing>Completed", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{
				conditionTrue(replicationv1alpha1.ConditionResyncing),
				conditionTrue(replicationv1alpha1.ConditionCompleted),
			}, drivers.RoleSource, drivers.HealthSyncing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vgName := "vm-health-" + tt.name
			vr := makeVRWithStatus(
				vgIDPrefix+vgName+"-data", "health-ns", vgName,
				tt.state, tt.conditions, nil,
			)
			drv := testDriver(t, vr)

			status, err := drv.GetReplicationStatus(
				context.Background(), vgIDFromNamespace("health-ns", vgName))
			if err != nil {
				t.Fatalf("GetReplicationStatus: %v", err)
			}
			if status.Role != tt.wantRole {
				t.Errorf("Role = %q, want %q", status.Role, tt.wantRole)
			}
			if status.Health != tt.wantHealth {
				t.Errorf("Health = %q, want %q", status.Health, tt.wantHealth)
			}
		})
	}
}

func TestHealthMapping_VGR(t *testing.T) {
	tests := []struct {
		name       string
		state      replicationv1alpha1.State
		conditions []metav1.Condition
		wantRole   drivers.VolumeRole
		wantHealth drivers.ReplicationHealth
	}{
		{"Primary+Completed", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
			drivers.RoleSource, drivers.HealthHealthy},
		{"Primary+Degraded", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionDegraded)},
			drivers.RoleSource, drivers.HealthDegraded},
		{"Primary+Resyncing", replicationv1alpha1.PrimaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionResyncing)},
			drivers.RoleSource, drivers.HealthSyncing},
		{"Primary+Empty", replicationv1alpha1.PrimaryState,
			nil, drivers.RoleSource, drivers.HealthUnknown},
		{"Secondary+Completed", replicationv1alpha1.SecondaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
			drivers.RoleTarget, drivers.HealthHealthy},
		{"Secondary+Degraded", replicationv1alpha1.SecondaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionDegraded)},
			drivers.RoleTarget, drivers.HealthDegraded},
		{"Secondary+Resyncing", replicationv1alpha1.SecondaryState,
			[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionResyncing)},
			drivers.RoleTarget, drivers.HealthSyncing},
		{"Secondary+Empty", replicationv1alpha1.SecondaryState,
			nil, drivers.RoleTarget, drivers.HealthUnknown},
		{"Empty+Empty", "",
			nil, drivers.RoleNonReplicated, drivers.HealthUnknown},
		{"Unknown+Empty", replicationv1alpha1.UnknownState,
			nil, drivers.RoleNonReplicated, drivers.HealthUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vgName := "ns-health-" + tt.name
			vgr := makeVGRWithStatus(
				vgIDPrefix+vgName, "health-ns", vgName,
				tt.state, tt.conditions, nil,
			)
			drv := testDriver(t, vgr)

			status, err := drv.GetReplicationStatus(
				context.Background(), vgIDFromNamespace("health-ns", vgName))
			if err != nil {
				t.Fatalf("GetReplicationStatus: %v", err)
			}
			if status.Role != tt.wantRole {
				t.Errorf("Role = %q, want %q", status.Role, tt.wantRole)
			}
			if status.Health != tt.wantHealth {
				t.Errorf("Health = %q, want %q", status.Health, tt.wantHealth)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error handling tests (Story 12.6, AC5)
// ---------------------------------------------------------------------------

func TestCreateVolumeGroup_APIError_VR(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(
				ctx context.Context, cl client.WithWatch,
				obj client.Object, opts ...client.CreateOption,
			) error {
				if _, ok := obj.(*replicationv1alpha1.VolumeReplication); ok {
					return fmt.Errorf("simulated API server failure")
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).
		Build()

	drv := New(c)
	_, err := drv.CreateVolumeGroup(context.Background(), drivers.VolumeGroupSpec{
		Name:      "vm-api-err",
		Namespace: "default",
		PVCNames:  []string{"data"},
		Labels:    primaryLabels(),
	})
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
}

func TestCreateVolumeGroup_APIError_VGR(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-1", Namespace: "err-ns"},
	}
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(pvc).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(
				ctx context.Context, cl client.WithWatch,
				obj client.Object, opts ...client.CreateOption,
			) error {
				if _, ok := obj.(*replicationv1alpha1.VolumeGroupReplication); ok {
					return fmt.Errorf("simulated API server failure")
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).
		Build()

	drv := New(c)
	_, err := drv.CreateVolumeGroup(context.Background(), drivers.VolumeGroupSpec{
		Name:      "ns-api-err",
		Namespace: "err-ns",
		PVCNames:  []string{"pvc-1"},
		Labels:    primaryLabels(),
	})
	if err == nil {
		t.Fatal("expected error for VGR API failure, got nil")
	}
}

func TestExternallyDeleted(t *testing.T) {
	stopFn := func(drv *Driver, ctx context.Context, id drivers.VolumeGroupID) error {
		return drv.StopReplication(ctx, id)
	}
	srcFn := func(drv *Driver, ctx context.Context, id drivers.VolumeGroupID) error {
		return drv.SetSource(ctx, id)
	}

	tests := []struct {
		name    string
		multiVM bool
		op      func(*Driver, context.Context, drivers.VolumeGroupID) error
	}{
		{"StopReplication/VR", false, stopFn},
		{"StopReplication/VGR", true, stopFn},
		{"SetSource/VR", false, srcFn},
		{"SetSource/VGR", true, srcFn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var drv *Driver
			var c client.Client
			var vgID drivers.VolumeGroupID

			if tt.multiVM {
				ns := "ext-del-" + tt.name
				vgName := "ns-ext-del"
				drv, c = testDriverWithClient(t,
					&corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc-1", Namespace: ns,
						},
					},
				)
				_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
					Name: vgName, Namespace: ns,
					PVCNames: []string{"pvc-1"},
					Labels:   primaryLabels(),
				})
				if err != nil {
					t.Fatalf("CreateVolumeGroup: %v", err)
				}
				vgID = vgIDFromNamespace(ns, vgName)
			} else {
				vgName := "vm-ext-del"
				drv, c = testDriverWithClient(t)
				_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
					Name: vgName, Namespace: "default",
					PVCNames: []string{"data"},
					Labels:   primaryLabels(),
				})
				if err != nil {
					t.Fatalf("CreateVolumeGroup: %v", err)
				}
				vgID = vgIDFromNamespace("default", vgName)
			}

			deleteAllCRs(t, c)

			err := tt.op(drv, ctx, vgID)
			if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
				t.Errorf("got %v, want ErrVolumeGroupNotFound", err)
			}
		})
	}
}

func TestEmptyStatus_ReturnsUnknown_VR(t *testing.T) {
	drv, c := testDriverWithClient(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      "vm-empty-status",
		Namespace: "default",
		PVCNames:  []string{"data"},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}
	assertVRCountAndState(t, c, "default", "vm-empty-status",
		1, ReplicationStatePrimary)

	status, err := drv.GetReplicationStatus(ctx,
		vgIDFromNamespace("default", "vm-empty-status"))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleNonReplicated {
		t.Errorf("Role = %q, want NonReplicated", status.Role)
	}
	if status.Health != drivers.HealthUnknown {
		t.Errorf("Health = %q, want Unknown", status.Health)
	}
}

func TestEmptyStatus_ReturnsUnknown_VGR(t *testing.T) {
	drv, c := testDriverWithClient(t,
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-1", Namespace: "empty-ns"},
		},
	)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      "ns-empty-status",
		Namespace: "empty-ns",
		PVCNames:  []string{"pvc-1"},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}
	assertVGRCountAndState(t, c, "empty-ns", "ns-empty-status",
		1, ReplicationStatePrimary)

	status, err := drv.GetReplicationStatus(ctx,
		vgIDFromNamespace("empty-ns", "ns-empty-status"))
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleNonReplicated {
		t.Errorf("Role = %q, want NonReplicated", status.Role)
	}
	if status.Health != drivers.HealthUnknown {
		t.Errorf("Health = %q, want Unknown", status.Health)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access tests (Story 12.6, AC6)
// ---------------------------------------------------------------------------

func TestConcurrentAccess_CreateAndGet(t *testing.T) {
	drv, _ := testDriverWithClient(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      "vm-concurrent-cg",
		Namespace: "default",
		PVCNames:  []string{"data"},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("Setup CreateVolumeGroup: %v", err)
	}
	vgID := vgIDFromNamespace("default", "vm-concurrent-cg")

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_, _ = drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
					Name:      "vm-concurrent-cg",
					Namespace: "default",
					PVCNames:  []string{"data"},
					Labels:    primaryLabels(),
				})
			} else {
				_, _ = drv.GetVolumeGroup(ctx, vgID)
			}
		}(i)
	}
	wg.Wait()

	_, err = drv.GetVolumeGroup(ctx, vgID)
	if err != nil {
		t.Errorf("GetVolumeGroup after concurrent access: %v", err)
	}
}

func TestConcurrentAccess_StopAndSetSource(t *testing.T) {
	drv, _ := testDriverWithClient(t)
	ctx := context.Background()

	_, err := drv.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      "vm-concurrent-ss",
		Namespace: "default",
		PVCNames:  []string{"data", "logs"},
		Labels:    primaryLabels(),
	})
	if err != nil {
		t.Fatalf("Setup CreateVolumeGroup: %v", err)
	}
	vgID := vgIDFromNamespace("default", "vm-concurrent-ss")

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = drv.StopReplication(ctx, vgID)
			} else {
				_ = drv.SetSource(ctx, vgID)
			}
		}(i)
	}
	wg.Wait()

	_, err = drv.GetVolumeGroup(ctx, vgID)
	if err != nil {
		t.Errorf("GetVolumeGroup after concurrent stop/set-source: %v", err)
	}
}

func TestConcurrentAccess_GetReplicationStatus(t *testing.T) {
	vr := makeVRWithStatus(
		vgIDPrefix+"vm-concurrent-grs-data", "default", "vm-concurrent-grs",
		replicationv1alpha1.PrimaryState,
		[]metav1.Condition{conditionTrue(replicationv1alpha1.ConditionCompleted)},
		nil,
	)
	drv := testDriver(t, vr)
	ctx := context.Background()
	vgID := vgIDFromNamespace("default", "vm-concurrent-grs")

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			status, err := drv.GetReplicationStatus(ctx, vgID)
			if err != nil {
				t.Errorf("GetReplicationStatus: %v", err)
				return
			}
			if status.Role != drivers.RoleSource {
				t.Errorf("Role = %q, want Source", status.Role)
			}
		}()
	}
	wg.Wait()
}
