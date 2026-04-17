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

package fake_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/drivers/fake"
)

// TestDriver_CompileTimeInterfaceCheck verifies at compile time that *Driver
// satisfies the StorageProvider interface (AC1).
func TestDriver_CompileTimeInterfaceCheck(t *testing.T) {
	var _ drivers.StorageProvider = (*fake.Driver)(nil)
}

// TestDriver_DefaultBehavior_ReturnsSuccess verifies that all 7 StorageProvider
// methods succeed with zero/default values when no reactions are programmed (AC4).
func TestDriver_DefaultBehavior_ReturnsSuccess(t *testing.T) {
	ctx := context.Background()
	d := fake.New()

	_, err := d.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{Name: "vg"})
	if err != nil {
		t.Errorf("CreateVolumeGroup: expected nil error, got %v", err)
	}

	if err := d.DeleteVolumeGroup(ctx, "vg-1"); err != nil {
		t.Errorf("DeleteVolumeGroup: expected nil error, got %v", err)
	}

	_, err = d.GetVolumeGroup(ctx, "vg-1")
	if err != nil {
		t.Errorf("GetVolumeGroup: expected nil error, got %v", err)
	}

	if err := d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{}); err != nil {
		t.Errorf("SetSource: expected nil error, got %v", err)
	}

	if err := d.SetTarget(ctx, "vg-1", drivers.SetTargetOptions{}); err != nil {
		t.Errorf("SetTarget: expected nil error, got %v", err)
	}

	if err := d.StopReplication(ctx, "vg-1", drivers.StopReplicationOptions{}); err != nil {
		t.Errorf("StopReplication: expected nil error, got %v", err)
	}

	_, err = d.GetReplicationStatus(ctx, "vg-1")
	if err != nil {
		t.Errorf("GetReplicationStatus: expected nil error, got %v", err)
	}
}

// TestDriver_CreateVolumeGroup_DefaultReturnsFakeID verifies the default CreateVolumeGroup
// response has a "fake-" prefixed ID (AC4).
func TestDriver_CreateVolumeGroup_DefaultReturnsFakeID(t *testing.T) {
	d := fake.New()
	info, err := d.CreateVolumeGroup(context.Background(), drivers.VolumeGroupSpec{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.HasPrefix(string(info.ID), "fake-") {
		t.Errorf("expected ID to have 'fake-' prefix, got %q", info.ID)
	}
}

// TestDriver_CreateVolumeGroup_UniqueIDs verifies that successive default calls
// return distinct IDs (AC4).
func TestDriver_CreateVolumeGroup_UniqueIDs(t *testing.T) {
	d := fake.New()
	info1, _ := d.CreateVolumeGroup(context.Background(), drivers.VolumeGroupSpec{})
	info2, _ := d.CreateVolumeGroup(context.Background(), drivers.VolumeGroupSpec{})
	if info1.ID == info2.ID {
		t.Errorf("expected unique IDs, both were %q", info1.ID)
	}
}

// TestDriver_OnSetSource_ReturnError verifies that a programmed error is returned
// and the call is recorded (AC1, AC3).
func TestDriver_OnSetSource_ReturnError(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)

	err := d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{})
	if !errors.Is(err, drivers.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
	if d.CallCount("SetSource") != 1 {
		t.Errorf("expected 1 SetSource call, got %d", d.CallCount("SetSource"))
	}
}

// TestDriver_OnSetSource_ReturnNil verifies that a programmed nil response is returned (AC1).
func TestDriver_OnSetSource_ReturnNil(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnSetSource("vg-1").Return(nil)

	if err := d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{}); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// TestDriver_OnGetReplicationStatus_ReturnResult verifies that a programmed
// ReplicationStatus is returned via ReturnResult (AC1).
func TestDriver_OnGetReplicationStatus_ReturnResult(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	want := drivers.ReplicationStatus{Role: drivers.RoleSource, Health: drivers.HealthHealthy}
	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
		ReplicationStatus: &want,
	})

	got, err := d.GetReplicationStatus(ctx, "vg-1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got.Role != want.Role || got.Health != want.Health {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

// TestDriver_OnGetVolumeGroup_ReturnError verifies that a programmed error
// is returned by GetVolumeGroup and the call is still recorded (AC3).
func TestDriver_OnGetVolumeGroup_ReturnError(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnGetVolumeGroup("vg-1").Return(drivers.ErrVolumeGroupNotFound)

	_, err := d.GetVolumeGroup(ctx, "vg-1")
	if !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("expected ErrVolumeGroupNotFound, got %v", err)
	}
	if d.CallCount("GetVolumeGroup") != 1 {
		t.Errorf("expected 1 GetVolumeGroup call, got %d", d.CallCount("GetVolumeGroup"))
	}
}

// TestDriver_MultipleReactions_ConsumedInOrder verifies that reactions for the same
// method are consumed in FIFO order, and subsequent calls get the default (AC1, AC2, AC4).
func TestDriver_MultipleReactions_ConsumedInOrder(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnSetSource("vg-1").Return(nil)
	d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)

	// First call: nil
	if err := d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{}); err != nil {
		t.Errorf("first call: expected nil, got %v", err)
	}
	// Second call: error
	if err := d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{}); !errors.Is(err, drivers.ErrInvalidTransition) {
		t.Errorf("second call: expected ErrInvalidTransition, got %v", err)
	}
	// Third call: default (nil — no more reactions)
	if err := d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{}); err != nil {
		t.Errorf("third call: expected default nil, got %v", err)
	}
}

// TestDriver_ArgMatching_SpecificVgID verifies that a reaction programmed for a
// specific vgID only matches calls with that ID (AC1, reaction matching).
func TestDriver_ArgMatching_SpecificVgID(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnSetSource("vg-match").Return(drivers.ErrInvalidTransition)

	// Non-matching call gets default (nil)
	if err := d.SetSource(ctx, "vg-other", drivers.SetSourceOptions{}); err != nil {
		t.Errorf("non-matching call: expected nil, got %v", err)
	}
	// Matching call gets programmed error
	if err := d.SetSource(ctx, "vg-match", drivers.SetSourceOptions{}); !errors.Is(err, drivers.ErrInvalidTransition) {
		t.Errorf("matching call: expected ErrInvalidTransition, got %v", err)
	}
}

// TestDriver_ArgMatching_AnyVgID verifies that a reaction with no vgID matcher
// matches any vgID argument.
func TestDriver_ArgMatching_AnyVgID(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnDeleteVolumeGroup().Return(drivers.ErrVolumeGroupNotFound)

	if err := d.DeleteVolumeGroup(ctx, "vg-anything"); !errors.Is(err, drivers.ErrVolumeGroupNotFound) {
		t.Errorf("expected ErrVolumeGroupNotFound, got %v", err)
	}
}

// TestDriver_CallRecording verifies Calls, CallsTo, CallCount, and Called (AC2).
func TestDriver_CallRecording(t *testing.T) {
	ctx := context.Background()
	d := fake.New()

	_ = d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{Force: true})
	_ = d.SetSource(ctx, "vg-2", drivers.SetSourceOptions{})
	_ = d.DeleteVolumeGroup(ctx, "vg-1")

	all := d.Calls()
	if len(all) != 3 {
		t.Errorf("Calls(): expected 3, got %d", len(all))
	}

	setSrc := d.CallsTo("SetSource")
	if len(setSrc) != 2 {
		t.Errorf("CallsTo(SetSource): expected 2, got %d", len(setSrc))
	}

	if d.CallCount("SetSource") != 2 {
		t.Errorf("CallCount(SetSource): expected 2, got %d", d.CallCount("SetSource"))
	}
	if d.CallCount("DeleteVolumeGroup") != 1 {
		t.Errorf("CallCount(DeleteVolumeGroup): expected 1, got %d", d.CallCount("DeleteVolumeGroup"))
	}

	if !d.Called("SetSource") {
		t.Error("Called(SetSource): expected true")
	}
	if d.Called("GetVolumeGroup") {
		t.Error("Called(GetVolumeGroup): expected false")
	}
}

// TestDriver_Reset verifies that Reset clears all calls and all reactions (AC4).
func TestDriver_Reset(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)
	_ = d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{})

	d.Reset()

	if d.CallCount("SetSource") != 0 {
		t.Error("after Reset: expected 0 SetSource calls")
	}
	// Reaction should be gone — next call should get default nil
	if err := d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{}); err != nil {
		t.Errorf("after Reset: expected nil error (default), got %v", err)
	}
}

// TestDriver_ConcurrentAccess verifies that concurrent call recording,
// response programming, and reading are race-free (AC5).
// Run with: go test -race ./pkg/drivers/fake/...
func TestDriver_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	d := fake.New()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			d.OnSetSource().Return(nil)
			_ = d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{})
			_ = d.CallCount("SetSource")
			_ = d.Calls()
		}()
	}
	wg.Wait()

	if d.CallCount("SetSource") != goroutines {
		t.Errorf("expected %d SetSource calls, got %d", goroutines, d.CallCount("SetSource"))
	}
}

// TestDriver_ErrorInjection_AllMethods verifies that each of the 7 methods can have
// a typed error injected and that it is returned via errors.Is (AC3).
func TestDriver_ErrorInjection_AllMethods(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(d *fake.Driver)
		call    func(d *fake.Driver) error
		wantErr error
	}{
		{
			name:    "CreateVolumeGroup returns ErrVolumeGroupNotFound",
			setup:   func(d *fake.Driver) { d.OnCreateVolumeGroup().Return(drivers.ErrVolumeGroupNotFound) },
			call:    func(d *fake.Driver) error { _, err := d.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{}); return err },
			wantErr: drivers.ErrVolumeGroupNotFound,
		},
		{
			name:    "DeleteVolumeGroup returns ErrVolumeGroupNotFound",
			setup:   func(d *fake.Driver) { d.OnDeleteVolumeGroup("vg-1").Return(drivers.ErrVolumeGroupNotFound) },
			call:    func(d *fake.Driver) error { return d.DeleteVolumeGroup(ctx, "vg-1") },
			wantErr: drivers.ErrVolumeGroupNotFound,
		},
		{
			name:    "GetVolumeGroup returns ErrVolumeGroupNotFound",
			setup:   func(d *fake.Driver) { d.OnGetVolumeGroup("vg-1").Return(drivers.ErrVolumeGroupNotFound) },
			call:    func(d *fake.Driver) error { _, err := d.GetVolumeGroup(ctx, "vg-1"); return err },
			wantErr: drivers.ErrVolumeGroupNotFound,
		},
		{
			name:    "SetSource returns ErrInvalidTransition",
			setup:   func(d *fake.Driver) { d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition) },
			call:    func(d *fake.Driver) error { return d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{}) },
			wantErr: drivers.ErrInvalidTransition,
		},
		{
			name:    "SetTarget returns ErrInvalidTransition",
			setup:   func(d *fake.Driver) { d.OnSetTarget("vg-1").Return(drivers.ErrInvalidTransition) },
			call:    func(d *fake.Driver) error { return d.SetTarget(ctx, "vg-1", drivers.SetTargetOptions{}) },
			wantErr: drivers.ErrInvalidTransition,
		},
		{
			name:    "StopReplication returns ErrReplicationNotReady",
			setup:   func(d *fake.Driver) { d.OnStopReplication("vg-1").Return(drivers.ErrReplicationNotReady) },
			call:    func(d *fake.Driver) error { return d.StopReplication(ctx, "vg-1", drivers.StopReplicationOptions{}) },
			wantErr: drivers.ErrReplicationNotReady,
		},
		{
			name:    "GetReplicationStatus returns ErrVolumeGroupNotFound",
			setup:   func(d *fake.Driver) { d.OnGetReplicationStatus("vg-1").Return(drivers.ErrVolumeGroupNotFound) },
			call:    func(d *fake.Driver) error { _, err := d.GetReplicationStatus(ctx, "vg-1"); return err },
			wantErr: drivers.ErrVolumeGroupNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := fake.New()
			tt.setup(d)
			err := tt.call(d)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error wrapping %v, got %v", tt.wantErr, err)
			}
		})
	}
}

// TestDriver_CallArgs_Recorded verifies that method arguments (including opts)
// are captured in the recorded Call (AC2).
func TestDriver_CallArgs_Recorded(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	opts := drivers.SetSourceOptions{Force: true}

	_ = d.SetSource(ctx, "vg-42", opts)

	calls := d.CallsTo("SetSource")
	if len(calls) != 1 {
		t.Fatalf("expected 1 SetSource call, got %d", len(calls))
	}
	if calls[0].Args[0] != drivers.VolumeGroupID("vg-42") {
		t.Errorf("expected Args[0] == %q, got %v", "vg-42", calls[0].Args[0])
	}
	if calls[0].Args[1] != opts {
		t.Errorf("expected Args[1] == %+v, got %+v", opts, calls[0].Args[1])
	}
}

// TestDriver_GetVolumeGroup_DefaultReturnsZeroValue verifies the default
// GetVolumeGroup response is a zero-value VolumeGroupInfo (AC4).
func TestDriver_GetVolumeGroup_DefaultReturnsZeroValue(t *testing.T) {
	info, err := fake.New().GetVolumeGroup(context.Background(), "vg-1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if info.ID != "" || info.Name != "" || len(info.PVCNames) != 0 {
		t.Errorf("expected zero-value VolumeGroupInfo, got %+v", info)
	}
}

// TestDriver_GetReplicationStatus_DefaultReturnsZeroValue verifies the default
// GetReplicationStatus response is a zero-value ReplicationStatus (AC4).
func TestDriver_GetReplicationStatus_DefaultReturnsZeroValue(t *testing.T) {
	status, err := fake.New().GetReplicationStatus(context.Background(), "vg-1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	var zero drivers.ReplicationStatus
	if status != zero {
		t.Errorf("expected zero-value ReplicationStatus, got %+v", status)
	}
}

// TestDriver_OnCreateVolumeGroup_ReturnResult verifies that ReturnResult programs
// a custom VolumeGroupInfo for CreateVolumeGroup (AC1).
func TestDriver_OnCreateVolumeGroup_ReturnResult(t *testing.T) {
	d := fake.New()
	want := &drivers.VolumeGroupInfo{ID: "custom-id", Name: "my-vg"}
	d.OnCreateVolumeGroup().ReturnResult(fake.Response{VolumeGroupInfo: want})

	got, err := d.CreateVolumeGroup(context.Background(), drivers.VolumeGroupSpec{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

// TestDriver_ErrorInjection_CallStillRecorded verifies that when a programmed error
// is returned, the call is still recorded in the call history (AC3).
func TestDriver_ErrorInjection_CallStillRecorded(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	d.OnDeleteVolumeGroup("vg-err").Return(drivers.ErrVolumeGroupNotFound)

	_ = d.DeleteVolumeGroup(ctx, "vg-err")

	if d.CallCount("DeleteVolumeGroup") != 1 {
		t.Errorf("expected call to be recorded even on error, got %d calls", d.CallCount("DeleteVolumeGroup"))
	}
}

// TestDriver_Calls_ReturnsCopy verifies that Calls() returns a deep-enough snapshot:
// mutations to the returned slice's Method field and Args backing array must not
// affect the driver's internal call history (AC2).
func TestDriver_Calls_ReturnsCopy(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	_ = d.SetSource(ctx, "vg-1", drivers.SetSourceOptions{})

	snapshot := d.Calls()

	// Mutate the Method string on the returned struct.
	snapshot[0].Method = "mutated-method"
	if d.Calls()[0].Method == "mutated-method" {
		t.Error("Calls(): mutating returned Call.Method altered driver state")
	}

	// Mutate an element inside the returned Args slice.
	snapshot[0].Args[0] = drivers.VolumeGroupID("mutated-id")
	if d.Calls()[0].Args[0] == drivers.VolumeGroupID("mutated-id") {
		t.Error("Calls(): mutating returned Call.Args element altered driver state (Args not deep-copied)")
	}
}

// TestDriver_CallsTo_ArgsNotAliased verifies that CallsTo() also returns
// independent Args slices so mutations cannot corrupt the driver's history (AC2).
func TestDriver_CallsTo_ArgsNotAliased(t *testing.T) {
	ctx := context.Background()
	d := fake.New()
	_ = d.DeleteVolumeGroup(ctx, "vg-1")

	result := d.CallsTo("DeleteVolumeGroup")
	if len(result) != 1 {
		t.Fatalf("expected 1 call, got %d", len(result))
	}
	result[0].Args[0] = drivers.VolumeGroupID("mutated")
	fresh := d.CallsTo("DeleteVolumeGroup")
	if fresh[0].Args[0] == drivers.VolumeGroupID("mutated") {
		t.Error("CallsTo(): mutating returned Call.Args element altered driver state")
	}
}
