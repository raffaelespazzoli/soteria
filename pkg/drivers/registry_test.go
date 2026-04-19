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

package drivers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// stubProvider is a trivial StorageProvider for registry tests. Each instance
// carries an ID so tests can verify the correct driver is returned.
type stubProvider struct {
	provisionerID string
}

func (s *stubProvider) CreateVolumeGroup(_ context.Context, _ VolumeGroupSpec) (VolumeGroupInfo, error) {
	return VolumeGroupInfo{}, nil
}
func (s *stubProvider) DeleteVolumeGroup(_ context.Context, _ VolumeGroupID) error { return nil }
func (s *stubProvider) GetVolumeGroup(_ context.Context, _ VolumeGroupID) (VolumeGroupInfo, error) {
	return VolumeGroupInfo{}, nil
}
func (s *stubProvider) SetSource(_ context.Context, _ VolumeGroupID, _ SetSourceOptions) error {
	return nil
}
func (s *stubProvider) SetTarget(_ context.Context, _ VolumeGroupID, _ SetTargetOptions) error {
	return nil
}
func (s *stubProvider) StopReplication(_ context.Context, _ VolumeGroupID, _ StopReplicationOptions) error {
	return nil
}
func (s *stubProvider) GetReplicationStatus(_ context.Context, _ VolumeGroupID) (ReplicationStatus, error) {
	return ReplicationStatus{}, nil
}

func newStubFactory(id string) DriverFactory {
	return func() StorageProvider {
		return &stubProvider{provisionerID: id}
	}
}

// mockSCLister implements StorageClassLister for testing.
type mockSCLister struct {
	provisioners map[string]string
}

func (m *mockSCLister) GetProvisioner(_ context.Context, scName string) (string, error) {
	p, ok := m.provisioners[scName]
	if !ok {
		return "", fmt.Errorf("storage class %q not found", scName)
	}
	return p, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.RegisterDriver("csi.example.com", newStubFactory("example"))

	driver, err := r.GetDriver("csi.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stub, ok := driver.(*stubProvider)
	if !ok {
		t.Fatal("expected *stubProvider")
	}
	if stub.provisionerID != "example" {
		t.Fatalf("got provisionerID %q, want %q", stub.provisionerID, "example")
	}
}

func TestRegistry_GetDriver_NotFound(t *testing.T) {
	r := NewRegistry()

	_, err := r.GetDriver("nonexistent.csi.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound, got: %v", err)
	}
}

func TestRegistry_RegisterDriver_Duplicate_Panics(t *testing.T) {
	r := NewRegistry()
	r.RegisterDriver("csi.example.com", newStubFactory("first"))

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic on duplicate registration")
		}
		msg, ok := rec.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", rec, rec)
		}
		if msg == "" {
			t.Fatal("panic message should not be empty")
		}
	}()

	r.RegisterDriver("csi.example.com", newStubFactory("second"))
}

func TestRegistry_GetDriverForPVC(t *testing.T) {
	r := NewRegistry()
	r.RegisterDriver("rook-ceph.rbd.csi.ceph.com", newStubFactory("ceph"))

	scLister := &mockSCLister{
		provisioners: map[string]string{
			"ocs-storagecluster-ceph-rbd": "rook-ceph.rbd.csi.ceph.com",
		},
	}

	driver, err := r.GetDriverForPVC(context.Background(), "ocs-storagecluster-ceph-rbd", scLister)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stub, ok := driver.(*stubProvider)
	if !ok {
		t.Fatal("expected *stubProvider")
	}
	if stub.provisionerID != "ceph" {
		t.Fatalf("got provisionerID %q, want %q", stub.provisionerID, "ceph")
	}
}

func TestRegistry_GetDriverForPVC_UnknownStorageClass(t *testing.T) {
	r := NewRegistry()
	scLister := &mockSCLister{provisioners: map[string]string{}}

	_, err := r.GetDriverForPVC(context.Background(), "nonexistent-sc", scLister)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRegistry_GetDriverForPVC_UnknownProvisioner(t *testing.T) {
	r := NewRegistry()
	scLister := &mockSCLister{
		provisioners: map[string]string{
			"some-sc": "unregistered.csi.com",
		},
	}

	_, err := r.GetDriverForPVC(context.Background(), "some-sc", scLister)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound, got: %v", err)
	}
}

func TestRegistry_ListRegistered(t *testing.T) {
	r := NewRegistry()
	r.RegisterDriver("z-provisioner", newStubFactory("z"))
	r.RegisterDriver("a-provisioner", newStubFactory("a"))
	r.RegisterDriver("m-provisioner", newStubFactory("m"))

	names := r.ListRegistered()
	if len(names) != 3 {
		t.Fatalf("expected 3 registered drivers, got %d", len(names))
	}
	expected := []string{"a-provisioner", "m-provisioner", "z-provisioner"}
	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("position %d: got %q, want %q", i, name, expected[i])
		}
	}
}

func TestRegistry_ListRegistered_Empty(t *testing.T) {
	r := NewRegistry()
	names := r.ListRegistered()
	if len(names) != 0 {
		t.Fatalf("expected empty list, got %v", names)
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	const numDrivers = 50

	var wg sync.WaitGroup

	// Register drivers concurrently (each with a unique name).
	for i := range numDrivers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("csi-%d.example.com", idx)
			r.RegisterDriver(name, newStubFactory(name))
		}(i)
	}
	wg.Wait()

	// Read drivers concurrently.
	for i := range numDrivers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("csi-%d.example.com", idx)
			driver, err := r.GetDriver(name)
			if err != nil {
				t.Errorf("GetDriver(%q) failed: %v", name, err)
				return
			}
			stub, ok := driver.(*stubProvider)
			if !ok {
				t.Errorf("expected *stubProvider for %q", name)
				return
			}
			if stub.provisionerID != name {
				t.Errorf("got provisionerID %q, want %q", stub.provisionerID, name)
			}
		}(i)
	}
	wg.Wait()

	names := r.ListRegistered()
	if len(names) != numDrivers {
		t.Fatalf("expected %d registered drivers, got %d", numDrivers, len(names))
	}
}

func TestRegistry_RegisterDriver_EmptyName_Panics(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty provisioner name")
		}
	}()
	r.RegisterDriver("", newStubFactory("bad"))
}

func TestRegistry_RegisterDriver_NilFactory_Panics(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil factory")
		}
	}()
	r.RegisterDriver("csi.example.com", nil)
}

func TestRegistry_GetDriverForPVC_NilLister(t *testing.T) {
	r := NewRegistry()
	_, err := r.GetDriverForPVC(context.Background(), "some-sc", nil)
	if err == nil {
		t.Fatal("expected error for nil StorageClassLister, got nil")
	}
}

func TestDefaultRegistry_PackageLevelFunctions(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	RegisterDriver("pkg-level.csi.com", newStubFactory("pkg"))

	driver, err := GetDriver("pkg-level.csi.com")
	if err != nil {
		t.Fatalf("GetDriver via package-level function failed: %v", err)
	}
	stub, ok := driver.(*stubProvider)
	if !ok {
		t.Fatal("expected *stubProvider")
	}
	if stub.provisionerID != "pkg" {
		t.Fatalf("got provisionerID %q, want %q", stub.provisionerID, "pkg")
	}

	names := ListRegistered()
	if len(names) != 1 || names[0] != "pkg-level.csi.com" {
		t.Fatalf("ListRegistered via package-level function: got %v", names)
	}

	scLister := &mockSCLister{
		provisioners: map[string]string{"sc-pkg": "pkg-level.csi.com"},
	}
	driver2, err := GetDriverForPVC(context.Background(), "sc-pkg", scLister)
	if err != nil {
		t.Fatalf("GetDriverForPVC via package-level function failed: %v", err)
	}
	stub2, ok := driver2.(*stubProvider)
	if !ok {
		t.Fatal("expected *stubProvider")
	}
	if stub2.provisionerID != "pkg" {
		t.Fatalf("got provisionerID %q, want %q", stub2.provisionerID, "pkg")
	}
}

func TestRegistry_ResetForTesting(t *testing.T) {
	r := NewRegistry()
	r.RegisterDriver("csi.example.com", newStubFactory("example"))

	if len(r.ListRegistered()) != 1 {
		t.Fatal("expected 1 registered driver before reset")
	}

	r.ResetForTesting()

	if len(r.ListRegistered()) != 0 {
		t.Fatal("expected 0 registered drivers after reset")
	}

	_, err := r.GetDriver("csi.example.com")
	if !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound after reset, got: %v", err)
	}
}

func TestRegistry_GetDriver_FallbackEnabled_UnknownProvisioner(t *testing.T) {
	r := NewRegistry()
	r.SetFallbackDriver(newStubFactory("fallback"))

	provider, err := r.GetDriver("unknown-provisioner")
	if err != nil {
		t.Fatalf("expected fallback driver, got error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider from fallback")
	}
	stub, ok := provider.(*stubProvider)
	if !ok {
		t.Fatal("expected *stubProvider")
	}
	if stub.provisionerID != "fallback" {
		t.Fatalf("got provisionerID %q, want %q", stub.provisionerID, "fallback")
	}
}

func TestRegistry_GetDriver_FallbackDisabled_UnknownProvisioner(t *testing.T) {
	r := NewRegistry()

	_, err := r.GetDriver("unknown-provisioner")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound, got: %v", err)
	}
}

func TestRegistry_GetDriver_FallbackEnabled_RegisteredProvisioner(t *testing.T) {
	r := NewRegistry()
	r.RegisterDriver("csi.example.com", newStubFactory("explicit"))
	r.SetFallbackDriver(newStubFactory("fallback"))

	provider, err := r.GetDriver("csi.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stub, ok := provider.(*stubProvider)
	if !ok {
		t.Fatal("expected *stubProvider")
	}
	if stub.provisionerID != "explicit" {
		t.Fatalf("got provisionerID %q, want %q — fallback should not override explicit registration",
			stub.provisionerID, "explicit")
	}
}

func TestRegistry_ListRegistered_ExcludesFallback(t *testing.T) {
	r := NewRegistry()
	r.RegisterDriver("csi.example.com", newStubFactory("explicit"))
	r.SetFallbackDriver(newStubFactory("fallback"))

	names := r.ListRegistered()
	if len(names) != 1 {
		t.Fatalf("expected 1 registered driver (fallback excluded), got %d: %v", len(names), names)
	}
	if names[0] != "csi.example.com" {
		t.Fatalf("expected %q, got %q", "csi.example.com", names[0])
	}
}

func TestRegistry_SetFallbackDriver_PanicOnDouble(t *testing.T) {
	r := NewRegistry()
	r.SetFallbackDriver(newStubFactory("first"))

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic on second SetFallbackDriver call")
		}
		msg, ok := rec.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", rec, rec)
		}
		if msg == "" {
			t.Fatal("panic message should not be empty")
		}
	}()

	r.SetFallbackDriver(newStubFactory("second"))
}

func TestRegistry_SetFallbackDriver_PanicOnNil(t *testing.T) {
	r := NewRegistry()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil factory")
		}
	}()

	r.SetFallbackDriver(nil)
}

func TestRegistry_ResetForTesting_ClearsFallback(t *testing.T) {
	r := NewRegistry()
	r.SetFallbackDriver(newStubFactory("fallback"))

	r.ResetForTesting()

	_, err := r.GetDriver("unknown-provisioner")
	if !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound after reset (fallback should be cleared), got: %v", err)
	}

	// After reset, setting fallback again should not panic.
	r.SetFallbackDriver(newStubFactory("new-fallback"))
	provider, err := r.GetDriver("any-provisioner")
	if err != nil {
		t.Fatalf("expected fallback driver after re-set, got error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestDefaultRegistry_SetFallbackDriver_PackageLevel(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	SetFallbackDriver(newStubFactory("pkg-fallback"))

	provider, err := GetDriver("unregistered-provisioner")
	if err != nil {
		t.Fatalf("expected fallback driver via package-level function, got error: %v", err)
	}
	stub, ok := provider.(*stubProvider)
	if !ok {
		t.Fatal("expected *stubProvider")
	}
	if stub.provisionerID != "pkg-fallback" {
		t.Fatalf("got provisionerID %q, want %q", stub.provisionerID, "pkg-fallback")
	}

	ResetForTesting()

	_, err = GetDriver("unregistered-provisioner")
	if !errors.Is(err, ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound after reset, got: %v", err)
	}
}

func TestRegistry_GetDriverForPVC_FallbackEnabled(t *testing.T) {
	r := NewRegistry()
	r.SetFallbackDriver(newStubFactory("fallback"))

	scLister := &mockSCLister{
		provisioners: map[string]string{
			"some-sc": "unregistered.csi.com",
		},
	}

	provider, err := r.GetDriverForPVC(context.Background(), "some-sc", scLister)
	if err != nil {
		t.Fatalf("expected fallback driver via GetDriverForPVC, got error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider from fallback")
	}
}

func TestRegistry_GetDriverForPVC_EmptyProvisioner(t *testing.T) {
	r := NewRegistry()
	r.SetFallbackDriver(newStubFactory("fallback"))

	scLister := &mockSCLister{
		provisioners: map[string]string{
			"misconfigured-sc": "",
		},
	}

	_, err := r.GetDriverForPVC(context.Background(), "misconfigured-sc", scLister)
	if err == nil {
		t.Fatal("expected error for empty provisioner, got nil")
	}
	if !strings.Contains(err.Error(), "empty provisioner") {
		t.Errorf("expected empty-provisioner error, got: %v", err)
	}
}
