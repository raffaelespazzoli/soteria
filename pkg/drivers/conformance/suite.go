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

package conformance

import (
	"context"
	"errors"
	"testing"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// RunConformance validates that a StorageProvider implementation correctly
// implements the full DR lifecycle contract. It exercises lifecycle transitions,
// idempotency, context cancellation, and error conditions. Any _test.go file
// can call this function with any driver instance:
//
//	func TestConformance(t *testing.T) {
//	    RunConformance(t, mydriver.New())
//	}
func RunConformance(t *testing.T, provider drivers.StorageProvider) {
	t.Helper()

	t.Run("Lifecycle", func(t *testing.T) {
		runLifecycleTest(t, provider)
	})

	t.Run("Idempotency", func(t *testing.T) {
		runIdempotencyTest(t, provider)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		runContextCancellationTest(t, provider)
	})

	t.Run("ErrorConditions", func(t *testing.T) {
		runErrorConditionsTest(t, provider)
	})
}

func testSpec(suffix string) drivers.VolumeGroupSpec {
	return drivers.VolumeGroupSpec{
		Name:      "conformance-" + suffix,
		Namespace: "conformance-test",
		Labels:    map[string]string{"conformance": "true"},
	}
}

// runLifecycleTest exercises the complete DR lifecycle in sequence:
// Create → SetSource → GetReplicationStatus(Source) → StopReplication →
// SetTarget → GetReplicationStatus(Target) → StopReplication → Delete → Get(deleted).
//
// Each step depends on the state left by the previous step. If any step fails,
// subsequent steps are skipped because they would produce misleading results.
func runLifecycleTest(t *testing.T, provider drivers.StorageProvider) {
	t.Helper()
	ctx := context.Background()

	var vgID drivers.VolumeGroupID

	t.Run("CreateVolumeGroup", func(t *testing.T) {
		info, err := provider.CreateVolumeGroup(ctx, testSpec("lifecycle"))
		if err != nil {
			t.Fatalf("CreateVolumeGroup failed: %v", err)
		}
		if info.ID == "" {
			t.Fatal("CreateVolumeGroup returned empty VolumeGroupID")
		}
		vgID = info.ID
	})

	if vgID == "" {
		t.Fatal("Skipping remaining lifecycle steps: CreateVolumeGroup did not return a valid ID")
	}

	t.Run("SetSource", func(t *testing.T) {
		if err := provider.SetSource(ctx, vgID, drivers.SetSourceOptions{Force: false}); err != nil {
			t.Fatalf("SetSource failed for volume group %s: %v", vgID, err)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("GetReplicationStatus_Source", func(t *testing.T) {
		status, err := provider.GetReplicationStatus(ctx, vgID)
		if err != nil {
			t.Fatalf("GetReplicationStatus failed for volume group %s: %v", vgID, err)
		}
		if status.Role != drivers.RoleSource {
			t.Fatalf("Expected role %s, got %s for volume group %s",
				drivers.RoleSource, status.Role, vgID)
		}
		if status.Health != drivers.HealthHealthy && status.Health != drivers.HealthSyncing {
			t.Logf("GetReplicationStatus: source health is %s for volume group %s"+
				" (Healthy or Syncing expected when driver reports immediately)",
				status.Health, vgID)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("StopReplication_FromSource", func(t *testing.T) {
		if err := provider.StopReplication(ctx, vgID, drivers.StopReplicationOptions{Force: false}); err != nil {
			t.Fatalf("StopReplication failed for volume group %s: %v", vgID, err)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("SetTarget", func(t *testing.T) {
		if err := provider.SetTarget(ctx, vgID, drivers.SetTargetOptions{Force: false}); err != nil {
			t.Fatalf("SetTarget failed for volume group %s: %v", vgID, err)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("GetReplicationStatus_Target", func(t *testing.T) {
		status, err := provider.GetReplicationStatus(ctx, vgID)
		if err != nil {
			t.Fatalf("GetReplicationStatus failed for volume group %s: %v", vgID, err)
		}
		if status.Role != drivers.RoleTarget {
			t.Fatalf("Expected role %s, got %s for volume group %s",
				drivers.RoleTarget, status.Role, vgID)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("StopReplication_FromTarget", func(t *testing.T) {
		if err := provider.StopReplication(ctx, vgID, drivers.StopReplicationOptions{Force: false}); err != nil {
			t.Fatalf("StopReplication failed for volume group %s: %v", vgID, err)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("DeleteVolumeGroup", func(t *testing.T) {
		if err := provider.DeleteVolumeGroup(ctx, vgID); err != nil {
			t.Fatalf("DeleteVolumeGroup failed for volume group %s: %v", vgID, err)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("GetVolumeGroup_Deleted", func(t *testing.T) {
		_, err := provider.GetVolumeGroup(ctx, vgID)
		if err == nil {
			t.Fatalf("Expected error for deleted volume group %s, got nil", vgID)
		}
		if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			t.Fatalf("Expected error wrapping %v for deleted volume group %s, got: %v",
				drivers.ErrVolumeGroupNotFound, vgID, err)
		}
	})
}

// runIdempotencyTest verifies that every StorageProvider method is safe to call
// twice in succession with identical arguments.
func runIdempotencyTest(t *testing.T, provider drivers.StorageProvider) {
	t.Helper()
	ctx := context.Background()

	spec := testSpec("idempotency")

	info, err := provider.CreateVolumeGroup(ctx, spec)
	if err != nil {
		t.Fatalf("Setup: CreateVolumeGroup failed: %v", err)
	}
	vgID := info.ID

	t.Run("CreateVolumeGroup", func(t *testing.T) {
		_, err1 := provider.CreateVolumeGroup(ctx, spec)
		if err1 != nil {
			t.Fatalf("CreateVolumeGroup first call failed for volume group %s: %v", vgID, err1)
		}
		_, err2 := provider.CreateVolumeGroup(ctx, spec)
		if err2 != nil {
			t.Fatalf("CreateVolumeGroup second call failed for volume group %s: %v", vgID, err2)
		}
	})

	t.Run("GetVolumeGroup", func(t *testing.T) {
		info1, err1 := provider.GetVolumeGroup(ctx, vgID)
		if err1 != nil {
			t.Fatalf("GetVolumeGroup first call failed for volume group %s: %v", vgID, err1)
		}
		info2, err2 := provider.GetVolumeGroup(ctx, vgID)
		if err2 != nil {
			t.Fatalf("GetVolumeGroup second call failed for volume group %s: %v", vgID, err2)
		}
		if info1.ID != info2.ID {
			t.Fatalf("GetVolumeGroup returned different IDs for volume group %s: %s vs %s",
				vgID, info1.ID, info2.ID)
		}
		if info1.Name != info2.Name {
			t.Fatalf("GetVolumeGroup returned different Names for volume group %s: %q vs %q",
				vgID, info1.Name, info2.Name)
		}
		if len(info1.PVCNames) != len(info2.PVCNames) {
			t.Fatalf("GetVolumeGroup returned different PVCNames lengths for volume group %s: %d vs %d",
				vgID, len(info1.PVCNames), len(info2.PVCNames))
		}
	})

	t.Run("SetSource", func(t *testing.T) {
		opts := drivers.SetSourceOptions{Force: false}
		if err := provider.SetSource(ctx, vgID, opts); err != nil {
			t.Fatalf("SetSource first call failed for volume group %s: %v", vgID, err)
		}
		if err := provider.SetSource(ctx, vgID, opts); err != nil {
			t.Fatalf("SetSource second call failed (idempotency) for volume group %s: %v", vgID, err)
		}
	})

	if err := provider.StopReplication(ctx, vgID, drivers.StopReplicationOptions{Force: false}); err != nil {
		t.Fatalf("Setup: StopReplication (before SetTarget idempotency) failed for volume group %s: %v", vgID, err)
	}

	t.Run("SetTarget", func(t *testing.T) {
		opts := drivers.SetTargetOptions{Force: false}
		if err := provider.SetTarget(ctx, vgID, opts); err != nil {
			t.Fatalf("SetTarget first call failed for volume group %s: %v", vgID, err)
		}
		if err := provider.SetTarget(ctx, vgID, opts); err != nil {
			t.Fatalf("SetTarget second call failed (idempotency) for volume group %s: %v", vgID, err)
		}
	})

	t.Run("StopReplication", func(t *testing.T) {
		opts := drivers.StopReplicationOptions{Force: false}
		if err := provider.StopReplication(ctx, vgID, opts); err != nil {
			t.Fatalf("StopReplication first call failed for volume group %s: %v", vgID, err)
		}
		if err := provider.StopReplication(ctx, vgID, opts); err != nil {
			t.Fatalf("StopReplication second call failed (idempotency) for volume group %s: %v", vgID, err)
		}
	})

	t.Run("GetReplicationStatus", func(t *testing.T) {
		_, err1 := provider.GetReplicationStatus(ctx, vgID)
		if err1 != nil {
			t.Fatalf("GetReplicationStatus first call failed for volume group %s: %v", vgID, err1)
		}
		_, err2 := provider.GetReplicationStatus(ctx, vgID)
		if err2 != nil {
			t.Fatalf("GetReplicationStatus second call failed for volume group %s: %v", vgID, err2)
		}
	})

	t.Run("DeleteVolumeGroup", func(t *testing.T) {
		if err := provider.DeleteVolumeGroup(ctx, vgID); err != nil {
			t.Fatalf("DeleteVolumeGroup first call failed for volume group %s: %v", vgID, err)
		}
		if err := provider.DeleteVolumeGroup(ctx, vgID); err != nil {
			t.Fatalf("DeleteVolumeGroup second call failed (idempotency) for volume group %s: %v", vgID, err)
		}
	})
}

// runContextCancellationTest verifies that every StorageProvider method returns
// an error when called with a pre-cancelled context.
func runContextCancellationTest(t *testing.T, provider drivers.StorageProvider) {
	t.Helper()

	validCtx := context.Background()
	info, err := provider.CreateVolumeGroup(validCtx, testSpec("ctx-cancel"))
	if err != nil {
		t.Fatalf("Setup: CreateVolumeGroup failed: %v", err)
	}
	vgID := info.ID

	t.Cleanup(func() {
		// Best-effort teardown so we don't leak resources on real backends.
		_ = provider.StopReplication(context.Background(), vgID, drivers.StopReplicationOptions{Force: true})
		_ = provider.DeleteVolumeGroup(context.Background(), vgID)
	})

	if err := provider.SetSource(validCtx, vgID, drivers.SetSourceOptions{Force: false}); err != nil {
		t.Fatalf("Setup: SetSource failed for volume group %s: %v", vgID, err)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("CreateVolumeGroup", func(t *testing.T) {
		_, err := provider.CreateVolumeGroup(cancelledCtx, testSpec("ctx-cancel-create"))
		if err == nil {
			t.Fatalf("CreateVolumeGroup: expected error with cancelled context for volume group %s, got nil", vgID)
		}
	})

	t.Run("GetVolumeGroup", func(t *testing.T) {
		_, err := provider.GetVolumeGroup(cancelledCtx, vgID)
		if err == nil {
			t.Fatalf("GetVolumeGroup: expected error with cancelled context for volume group %s, got nil", vgID)
		}
	})

	t.Run("SetSource", func(t *testing.T) {
		err := provider.SetSource(cancelledCtx, vgID, drivers.SetSourceOptions{Force: false})
		if err == nil {
			t.Fatalf("SetSource: expected error with cancelled context for volume group %s, got nil", vgID)
		}
	})

	t.Run("SetTarget", func(t *testing.T) {
		err := provider.SetTarget(cancelledCtx, vgID, drivers.SetTargetOptions{Force: false})
		if err == nil {
			t.Fatalf("SetTarget: expected error with cancelled context for volume group %s, got nil", vgID)
		}
	})

	t.Run("StopReplication", func(t *testing.T) {
		err := provider.StopReplication(cancelledCtx, vgID, drivers.StopReplicationOptions{Force: false})
		if err == nil {
			t.Fatalf("StopReplication: expected error with cancelled context for volume group %s, got nil", vgID)
		}
	})

	t.Run("GetReplicationStatus", func(t *testing.T) {
		_, err := provider.GetReplicationStatus(cancelledCtx, vgID)
		if err == nil {
			t.Fatalf("GetReplicationStatus: expected error with cancelled context for volume group %s, got nil", vgID)
		}
	})

	t.Run("DeleteVolumeGroup", func(t *testing.T) {
		err := provider.DeleteVolumeGroup(cancelledCtx, vgID)
		if err == nil {
			t.Fatalf("DeleteVolumeGroup: expected error with cancelled context for volume group %s, got nil", vgID)
		}
	})
}

// runErrorConditionsTest verifies that the driver returns ErrVolumeGroupNotFound
// for operations on nonexistent volume group IDs.
func runErrorConditionsTest(t *testing.T, provider drivers.StorageProvider) {
	t.Helper()
	ctx := context.Background()
	nonexistentID := drivers.VolumeGroupID("conformance-nonexistent-vgid")

	t.Run("GetVolumeGroup_NotFound", func(t *testing.T) {
		_, err := provider.GetVolumeGroup(ctx, nonexistentID)
		if err == nil {
			t.Fatal("Expected error for nonexistent volume group, got nil")
		}
		if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			t.Fatalf("Expected error wrapping %v, got: %v", drivers.ErrVolumeGroupNotFound, err)
		}
	})

	t.Run("SetSource_NotFound", func(t *testing.T) {
		err := provider.SetSource(ctx, nonexistentID, drivers.SetSourceOptions{Force: false})
		if err == nil {
			t.Fatal("Expected error for nonexistent volume group, got nil")
		}
		if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			t.Fatalf("Expected error wrapping %v, got: %v", drivers.ErrVolumeGroupNotFound, err)
		}
	})

	t.Run("SetTarget_NotFound", func(t *testing.T) {
		err := provider.SetTarget(ctx, nonexistentID, drivers.SetTargetOptions{Force: false})
		if err == nil {
			t.Fatal("Expected error for nonexistent volume group, got nil")
		}
		if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			t.Fatalf("Expected error wrapping %v, got: %v", drivers.ErrVolumeGroupNotFound, err)
		}
	})

	t.Run("StopReplication_NotFound", func(t *testing.T) {
		err := provider.StopReplication(ctx, nonexistentID, drivers.StopReplicationOptions{Force: false})
		if err == nil {
			t.Fatal("Expected error for nonexistent volume group, got nil")
		}
		if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			t.Fatalf("Expected error wrapping %v, got: %v", drivers.ErrVolumeGroupNotFound, err)
		}
	})

	t.Run("GetReplicationStatus_NotFound", func(t *testing.T) {
		_, err := provider.GetReplicationStatus(ctx, nonexistentID)
		if err == nil {
			t.Fatal("Expected error for nonexistent volume group, got nil")
		}
		if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			t.Fatalf("Expected error wrapping %v, got: %v", drivers.ErrVolumeGroupNotFound, err)
		}
	})
}
