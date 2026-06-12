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
	"testing"

	"github.com/soteria-project/soteria/pkg/drivers"
)

func TestGetDriver_PlanLevelName(t *testing.T) {
	drv, err := drivers.GetDriver(PlanDriverName)
	if err != nil {
		t.Fatalf("GetDriver(%q) returned error: %v", PlanDriverName, err)
	}
	if drv == nil {
		t.Fatalf("GetDriver(%q) returned nil provider", PlanDriverName)
	}
}

func TestGetDriver_ProvisionerName(t *testing.T) {
	drv, err := drivers.GetDriver(ProvisionerName)
	if err != nil {
		t.Fatalf("GetDriver(%q) returned error: %v", ProvisionerName, err)
	}
	if drv == nil {
		t.Fatalf("GetDriver(%q) returned nil provider", ProvisionerName)
	}
}

func TestGetDriver_DefaultRegistry_SharesStateAcrossLookups(t *testing.T) {
	t.Helper()

	creator, err := drivers.GetDriver(PlanDriverName)
	if err != nil {
		t.Fatalf("GetDriver(%q) returned error: %v", PlanDriverName, err)
	}

	reader, err := drivers.GetDriver(PlanDriverName)
	if err != nil {
		t.Fatalf("second GetDriver(%q) returned error: %v", PlanDriverName, err)
	}

	spec := drivers.VolumeGroupSpec{
		Name:      "shared-state-" + t.Name(),
		Namespace: "default",
	}
	info, err := creator.CreateVolumeGroup(context.Background(), spec)
	if err != nil {
		t.Fatalf("CreateVolumeGroup returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = creator.DeleteVolumeGroup(context.Background(), info.ID)
	})

	got, err := reader.GetVolumeGroup(context.Background(), info.ID)
	if err != nil {
		t.Fatalf("GetVolumeGroup returned error: %v", err)
	}
	if got.ID != info.ID {
		t.Fatalf("GetVolumeGroup returned ID %q, want %q", got.ID, info.ID)
	}
}
