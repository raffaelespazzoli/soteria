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

package noop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/soteria-project/soteria/pkg/drivers"
)

var _ drivers.StorageProvider = (*Driver)(nil)

func testCtx() context.Context {
	return context.Background()
}

func TestDriver_CreateAndGetVolumeGroup(t *testing.T) {
	d := New()
	spec := drivers.VolumeGroupSpec{
		Name:      "test-group",
		Namespace: "default",
		PVCNames:  []string{"pvc-1", "pvc-2"},
		Labels:    map[string]string{"app": "test"},
	}

	info, err := d.CreateVolumeGroup(testCtx(), spec)
	if err != nil {
		t.Fatalf("CreateVolumeGroup: unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(info.ID), "noop-") {
		t.Fatalf("expected ID with noop- prefix, got %q", info.ID)
	}
	if info.Name != spec.Name {
		t.Fatalf("expected Name %q, got %q", spec.Name, info.Name)
	}
	if len(info.PVCNames) != 2 || info.PVCNames[0] != "pvc-1" || info.PVCNames[1] != "pvc-2" {
		t.Fatalf("unexpected PVCNames: %v", info.PVCNames)
	}

	got, err := d.GetVolumeGroup(testCtx(), info.ID)
	if err != nil {
		t.Fatalf("GetVolumeGroup: unexpected error: %v", err)
	}
	if got.ID != info.ID {
		t.Fatalf("expected ID %q, got %q", info.ID, got.ID)
	}
	if got.Name != info.Name {
		t.Fatalf("expected Name %q, got %q", info.Name, got.Name)
	}
}

func TestDriver_DeleteVolumeGroup(t *testing.T) {
	d := New()
	info, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "to-delete"})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: unexpected error: %v", err)
	}

	if err := d.DeleteVolumeGroup(testCtx(), info.ID); err != nil {
		t.Fatalf("DeleteVolumeGroup: unexpected error: %v", err)
	}

	_, err = d.GetVolumeGroup(testCtx(), info.ID)
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Fatalf("expected ErrVolumeGroupNotFound after delete, got: %v", err)
	}
}

func TestDriver_DeleteVolumeGroup_NotFound(t *testing.T) {
	d := New()
	if err := d.DeleteVolumeGroup(testCtx(), "nonexistent-id"); err != nil {
		t.Fatalf("DeleteVolumeGroup on missing ID should return nil, got: %v", err)
	}
}

func TestDriver_GetVolumeGroup_NotFound(t *testing.T) {
	d := New()
	_, err := d.GetVolumeGroup(testCtx(), "unknown-id")
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Fatalf("expected ErrVolumeGroupNotFound, got: %v", err)
	}
}

func TestDriver_ReplicationLifecycle(t *testing.T) {
	d := New()
	info, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "repl-test"})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	steps := []struct {
		name       string
		action     func() error
		wantRole   drivers.VolumeRole
		wantHealth drivers.ReplicationHealth
	}{
		{
			name:       "initial state is source",
			action:     func() error { return nil },
			wantRole:   drivers.RoleSource,
			wantHealth: drivers.HealthHealthy,
		},
		{
			name:       "stop replication from source",
			action:     func() error { return d.StopReplication(testCtx(), info.ID) },
			wantRole:   drivers.RoleNonReplicated,
			wantHealth: drivers.HealthNotReplicating,
		},
		{
			name:       "re-establish source after stop",
			action:     func() error { return d.SetSource(testCtx(), info.ID) },
			wantRole:   drivers.RoleSource,
			wantHealth: drivers.HealthHealthy,
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			if err := step.action(); err != nil {
				t.Fatalf("%s: unexpected error: %v", step.name, err)
			}

			status, err := d.GetReplicationStatus(testCtx(), info.ID)
			if err != nil {
				t.Fatalf("GetReplicationStatus after %s: %v", step.name, err)
			}
			if status.Role != step.wantRole {
				t.Fatalf("after %s: expected role %q, got %q", step.name, step.wantRole, status.Role)
			}
			if status.Health != step.wantHealth {
				t.Fatalf("after %s: expected health %q, got %q", step.name, step.wantHealth, status.Health)
			}
			if step.wantRole != drivers.RoleNonReplicated {
				if status.LastSyncTime == nil {
					t.Fatalf("after %s: expected non-nil LastSyncTime for replicating role", step.name)
				}
			}
		})
	}
}

func TestDriver_InvalidTransition_SetSourceWhenSource(t *testing.T) {
	d := New()
	info, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "invalid-src"})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	if err := d.SetSource(testCtx(), info.ID); err != nil {
		t.Fatalf("SetSource: %v", err)
	}

	// SetSource is idempotent — calling again on Source returns nil.
	if err := d.SetSource(testCtx(), info.ID); err != nil {
		t.Fatalf("idempotent SetSource should succeed, got: %v", err)
	}
}

func TestDriver_Idempotency_Create(t *testing.T) {
	d := New()
	spec := drivers.VolumeGroupSpec{Name: "idem-create", Namespace: "ns", PVCNames: []string{"pvc-a"}}

	info1, err := d.CreateVolumeGroup(testCtx(), spec)
	if err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}
	info2, err := d.CreateVolumeGroup(testCtx(), spec)
	if err != nil {
		t.Fatalf("second CreateVolumeGroup: %v", err)
	}

	if info1.ID != info2.ID {
		t.Fatalf("idempotent create should return same ID, got %q and %q", info1.ID, info2.ID)
	}
	if info1.Name != info2.Name {
		t.Fatalf("idempotent create should return same Name, got %q and %q", info1.Name, info2.Name)
	}
}

func TestDriver_CreateVolumeGroup_DifferentNames(t *testing.T) {
	d := New()

	info1, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "group-a"})
	if err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}
	info2, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "group-b"})
	if err != nil {
		t.Fatalf("second CreateVolumeGroup: %v", err)
	}

	if info1.ID == info2.ID {
		t.Fatal("different names should produce different IDs")
	}
}

func TestDriver_CreateVolumeGroup_SameNameDifferentNamespace(t *testing.T) {
	d := New()

	info1, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "shared-name", Namespace: "ns-a"})
	if err != nil {
		t.Fatalf("first CreateVolumeGroup: %v", err)
	}
	info2, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "shared-name", Namespace: "ns-b"})
	if err != nil {
		t.Fatalf("second CreateVolumeGroup: %v", err)
	}

	if info1.ID == info2.ID {
		t.Fatal("same name in different namespaces must produce different IDs")
	}
}

func TestDriver_ContextCancellation(t *testing.T) {
	d := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	if _, err := d.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{Name: "cancelled"}); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if err := d.DeleteVolumeGroup(ctx, "any-id"); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if _, err := d.GetVolumeGroup(ctx, "any-id"); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if err := d.SetSource(ctx, "any-id"); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if err := d.StopReplication(ctx, "any-id"); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if _, err := d.GetReplicationStatus(ctx, "any-id"); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestDriver_Idempotency_SetSource(t *testing.T) {
	d := New()
	info, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "idem-source"})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	if err := d.SetSource(testCtx(), info.ID); err != nil {
		t.Fatalf("first SetSource: %v", err)
	}
	if err := d.SetSource(testCtx(), info.ID); err != nil {
		t.Fatalf("second SetSource (idempotent): %v", err)
	}

	status, err := d.GetReplicationStatus(testCtx(), info.ID)
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Fatalf("expected RoleSource after idempotent SetSource, got %q", status.Role)
	}
}

func TestDriver_Idempotency_StopReplication(t *testing.T) {
	d := New()
	info, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "idem-stop"})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	if err := d.StopReplication(testCtx(), info.ID); err != nil {
		t.Fatalf("first StopReplication: %v", err)
	}

	// StopReplication on NonReplicated is idempotent
	if err := d.StopReplication(testCtx(), info.ID); err != nil {
		t.Fatalf("second StopReplication (idempotent): %v", err)
	}

	status, err := d.GetReplicationStatus(testCtx(), info.ID)
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleNonReplicated {
		t.Fatalf("expected RoleNonReplicated, got %q", status.Role)
	}
}

func TestDriver_GetReplicationStatus_AfterCreate(t *testing.T) {
	d := New()
	info, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: "after-create"})
	if err != nil {
		t.Fatalf("CreateVolumeGroup: %v", err)
	}

	status, err := d.GetReplicationStatus(testCtx(), info.ID)
	if err != nil {
		t.Fatalf("GetReplicationStatus: %v", err)
	}
	if status.Role != drivers.RoleSource {
		t.Fatalf("expected RoleSource after create, got %q", status.Role)
	}
	if status.Health != drivers.HealthHealthy {
		t.Fatalf("expected HealthHealthy after create, got %q", status.Health)
	}
	if status.LastSyncTime == nil {
		t.Fatal("expected non-nil LastSyncTime for Source role")
	}
}

func TestDriver_UnknownVolumeGroup_ReplicationMethods(t *testing.T) {
	d := New()
	unknownID := drivers.VolumeGroupID("does-not-exist")

	tests := []struct {
		name string
		fn   func() error
	}{
		{"SetSource", func() error { return d.SetSource(testCtx(), unknownID) }},
		{"StopReplication", func() error { return d.StopReplication(testCtx(), unknownID) }},
		{"GetReplicationStatus", func() error {
			_, err := d.GetReplicationStatus(testCtx(), unknownID)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
				t.Fatalf("expected ErrVolumeGroupNotFound, got: %v", err)
			}
		})
	}
}

func TestDriver_ConcurrentAccess(t *testing.T) {
	d := New()
	const goroutines = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func() {
			defer wg.Done()

			info, err := d.CreateVolumeGroup(testCtx(), drivers.VolumeGroupSpec{Name: fmt.Sprintf("concurrent-%d", i)})
			if err != nil {
				t.Errorf("CreateVolumeGroup: %v", err)
				return
			}

			if _, err := d.GetVolumeGroup(testCtx(), info.ID); err != nil {
				t.Errorf("GetVolumeGroup: %v", err)
				return
			}

			if err := d.SetSource(testCtx(), info.ID); err != nil {
				t.Errorf("SetSource: %v", err)
				return
			}

			if _, err := d.GetReplicationStatus(testCtx(), info.ID); err != nil {
				t.Errorf("GetReplicationStatus: %v", err)
				return
			}

			if err := d.StopReplication(testCtx(), info.ID); err != nil {
				t.Errorf("StopReplication: %v", err)
				return
			}

			if err := d.DeleteVolumeGroup(testCtx(), info.ID); err != nil {
				t.Errorf("DeleteVolumeGroup: %v", err)
				return
			}
		}()
	}

	wg.Wait()
}

func TestDriver_CompileTimeInterfaceCheck(t *testing.T) {
	d := New()
	var _ drivers.StorageProvider = d
	t.Logf("Driver satisfies StorageProvider interface: %T", d)
}
