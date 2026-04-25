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

package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// compile-time interface check
var _ drivers.StorageProvider = (*Driver)(nil)

// Call records a single invocation of a StorageProvider method.
type Call struct {
	// Method is the name of the StorageProvider method that was invoked.
	Method string
	// Args holds the arguments passed to the method (excluding context).
	Args []any
}

// Response holds the programmed return values for a StorageProvider method.
// Test code constructs this directly and passes it to ReturnResult.
type Response struct {
	// VolumeGroupID is used for methods that return a VolumeGroupID (unused currently;
	// VolumeGroupInfo.ID carries the identifier for CreateVolumeGroup/GetVolumeGroup).
	VolumeGroupID drivers.VolumeGroupID
	// VolumeGroupInfo is the programmed return value for CreateVolumeGroup and GetVolumeGroup.
	// When nil and no error is set, the default for CreateVolumeGroup is a VolumeGroupInfo
	// with a generated "fake-<uuid>" ID; for GetVolumeGroup the default is a zero-value.
	VolumeGroupInfo *drivers.VolumeGroupInfo
	// ReplicationStatus is the programmed return value for GetReplicationStatus.
	// When nil and no error is set, a zero-value ReplicationStatus is returned.
	ReplicationStatus *drivers.ReplicationStatus
	// Err is the programmed error to return. Nil means success.
	Err error
}

// reaction is an unexported bookkeeping struct that holds a single programmed
// response for a method invocation. Reactions are consumed in FIFO order.
type reaction struct {
	// vgID is the optional argument matcher. nil means "match any VolumeGroupID".
	vgID *drivers.VolumeGroupID
	// resp is the response to return when this reaction matches.
	resp Response
	// consumed is set to true once this reaction has been matched and returned.
	consumed bool
}

// Driver is a programmable fake StorageProvider for unit testing.
// It follows the Kubernetes <package>fake naming convention
// (e.g. k8s.io/client-go/kubernetes/fake).
//
// Usage:
//
//	d := fake.New()
//	d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)
//	d.OnGetReplicationStatus("vg-1").ReturnResult(fake.Response{
//	    ReplicationStatus: &drivers.ReplicationStatus{Role: drivers.RoleSource},
//	})
//	err := d.SetSource(ctx, "vg-1")
//	// err == drivers.ErrInvalidTransition
//
// All public methods are protected by a single sync.Mutex and are safe for
// concurrent use from multiple goroutines.
type Driver struct {
	mu        sync.Mutex
	calls     []Call
	reactions map[string][]*reaction
}

// New creates a new Driver ready for use in tests.
func New() *Driver {
	return &Driver{
		calls:     []Call{},
		reactions: make(map[string][]*reaction),
	}
}

// CallStub is returned by On* methods to enable fluent response programming.
//
//	d.OnSetSource("vg-1").Return(drivers.ErrInvalidTransition)
//	d.OnCreateVolumeGroup().ReturnResult(fake.Response{VolumeGroupInfo: &info})
type CallStub struct {
	driver   *Driver
	reaction *reaction
}

// Return programs the stub to return err (or nil for success). Returns the Driver
// for method chaining.
func (s *CallStub) Return(err error) *Driver {
	s.driver.mu.Lock()
	defer s.driver.mu.Unlock()
	s.reaction.resp.Err = err
	return s.driver
}

// ReturnResult programs the stub to return the full Response (values + error).
// Returns the Driver for method chaining.
func (s *CallStub) ReturnResult(resp Response) *Driver {
	s.driver.mu.Lock()
	defer s.driver.mu.Unlock()
	s.reaction.resp = resp
	return s.driver
}

// onMethod is an internal helper that registers a new reaction for the given
// method and optional vgID matcher, and returns a *CallStub for chaining.
func (d *Driver) onMethod(method string, vgID *drivers.VolumeGroupID) *CallStub {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := &reaction{vgID: vgID}
	d.reactions[method] = append(d.reactions[method], r)
	return &CallStub{driver: d, reaction: r}
}

// optionalVgID converts a variadic VolumeGroupID slice to a pointer for reaction
// matching. If no IDs are provided, returns nil (match any).
func optionalVgID(ids []drivers.VolumeGroupID) *drivers.VolumeGroupID {
	if len(ids) > 0 {
		id := ids[0]
		return &id
	}
	return nil
}

// OnCreateVolumeGroup programs a reaction for the next CreateVolumeGroup call.
// CreateVolumeGroup takes no VolumeGroupID argument, so all reactions are matched in order.
func (d *Driver) OnCreateVolumeGroup() *CallStub {
	return d.onMethod("CreateVolumeGroup", nil)
}

// OnDeleteVolumeGroup programs a reaction for DeleteVolumeGroup. If vgID is provided,
// only calls with that ID match; otherwise the reaction matches any call.
func (d *Driver) OnDeleteVolumeGroup(vgID ...drivers.VolumeGroupID) *CallStub {
	return d.onMethod("DeleteVolumeGroup", optionalVgID(vgID))
}

// OnGetVolumeGroup programs a reaction for GetVolumeGroup. If vgID is provided,
// only calls with that ID match; otherwise the reaction matches any call.
func (d *Driver) OnGetVolumeGroup(vgID ...drivers.VolumeGroupID) *CallStub {
	return d.onMethod("GetVolumeGroup", optionalVgID(vgID))
}

// OnSetSource programs a reaction for SetSource. If vgID is provided,
// only calls with that ID match; otherwise the reaction matches any call.
func (d *Driver) OnSetSource(vgID ...drivers.VolumeGroupID) *CallStub {
	return d.onMethod("SetSource", optionalVgID(vgID))
}

// OnStopReplication programs a reaction for StopReplication. If vgID is provided,
// only calls with that ID match; otherwise the reaction matches any call.
func (d *Driver) OnStopReplication(vgID ...drivers.VolumeGroupID) *CallStub {
	return d.onMethod("StopReplication", optionalVgID(vgID))
}

// OnGetReplicationStatus programs a reaction for GetReplicationStatus. If vgID is provided,
// only calls with that ID match; otherwise the reaction matches any call.
func (d *Driver) OnGetReplicationStatus(vgID ...drivers.VolumeGroupID) *CallStub {
	return d.onMethod("GetReplicationStatus", optionalVgID(vgID))
}

// findReaction scans the reaction list for method, skipping consumed entries,
// and returns the first unconsumed reaction that matches vgID (or any if vgID matcher is nil).
// The returned reaction is marked consumed. Must be called with d.mu held.
func (d *Driver) findReaction(method string, vgID drivers.VolumeGroupID) *reaction {
	for _, r := range d.reactions[method] {
		if r.consumed {
			continue
		}
		if r.vgID == nil || *r.vgID == vgID {
			r.consumed = true
			return r
		}
	}
	return nil
}

// findReactionAny finds the first unconsumed reaction for a method that takes no
// VolumeGroupID argument (CreateVolumeGroup). Must be called with d.mu held.
func (d *Driver) findReactionAny(method string) *reaction {
	for _, r := range d.reactions[method] {
		if r.consumed {
			continue
		}
		r.consumed = true
		return r
	}
	return nil
}

// --- StorageProvider implementation ---

// CreateVolumeGroup records the call and returns the programmed response.
// Default: returns a VolumeGroupInfo with ID "fake-<uuid>" and nil error.
func (d *Driver) CreateVolumeGroup(_ context.Context, spec drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) {
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: "CreateVolumeGroup", Args: []any{spec}})
	r := d.findReactionAny("CreateVolumeGroup")
	d.mu.Unlock()

	if r != nil {
		if r.resp.VolumeGroupInfo != nil {
			return *r.resp.VolumeGroupInfo, r.resp.Err
		}
		return drivers.VolumeGroupInfo{}, r.resp.Err
	}
	return drivers.VolumeGroupInfo{ID: drivers.VolumeGroupID(fmt.Sprintf("fake-%s", uuid.NewString()))}, nil
}

// DeleteVolumeGroup records the call and returns the programmed response.
// Default: returns nil.
func (d *Driver) DeleteVolumeGroup(_ context.Context, id drivers.VolumeGroupID) error {
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: "DeleteVolumeGroup", Args: []any{id}})
	r := d.findReaction("DeleteVolumeGroup", id)
	d.mu.Unlock()

	if r != nil {
		return r.resp.Err
	}
	return nil
}

// GetVolumeGroup records the call and returns the programmed response.
// Default: returns zero-value VolumeGroupInfo and nil error.
func (d *Driver) GetVolumeGroup(_ context.Context, id drivers.VolumeGroupID) (drivers.VolumeGroupInfo, error) {
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: "GetVolumeGroup", Args: []any{id}})
	r := d.findReaction("GetVolumeGroup", id)
	d.mu.Unlock()

	if r != nil {
		if r.resp.VolumeGroupInfo != nil {
			return *r.resp.VolumeGroupInfo, r.resp.Err
		}
		return drivers.VolumeGroupInfo{}, r.resp.Err
	}
	return drivers.VolumeGroupInfo{}, nil
}

// SetSource records the call and returns the programmed response.
// Default: returns nil.
func (d *Driver) SetSource(_ context.Context, id drivers.VolumeGroupID) error {
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: "SetSource", Args: []any{id}})
	r := d.findReaction("SetSource", id)
	d.mu.Unlock()

	if r != nil {
		return r.resp.Err
	}
	return nil
}

// StopReplication records the call and returns the programmed response.
// Default: returns nil.
func (d *Driver) StopReplication(_ context.Context, id drivers.VolumeGroupID) error {
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: "StopReplication", Args: []any{id}})
	r := d.findReaction("StopReplication", id)
	d.mu.Unlock()

	if r != nil {
		return r.resp.Err
	}
	return nil
}

// GetReplicationStatus records the call and returns the programmed response.
// Default: returns zero-value ReplicationStatus and nil error.
func (d *Driver) GetReplicationStatus(_ context.Context, id drivers.VolumeGroupID) (drivers.ReplicationStatus, error) {
	d.mu.Lock()
	d.calls = append(d.calls, Call{Method: "GetReplicationStatus", Args: []any{id}})
	r := d.findReaction("GetReplicationStatus", id)
	d.mu.Unlock()

	if r != nil {
		if r.resp.ReplicationStatus != nil {
			return *r.resp.ReplicationStatus, r.resp.Err
		}
		return drivers.ReplicationStatus{}, r.resp.Err
	}
	return drivers.ReplicationStatus{}, nil
}

// --- Call recording helpers ---

// Calls returns a snapshot of all recorded method invocations in order.
// Each returned Call has its own Args slice so mutations to the snapshot
// cannot alter the driver's internal call history.
func (d *Driver) Calls() []Call {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]Call, len(d.calls))
	for i, c := range d.calls {
		result[i] = Call{Method: c.Method, Args: append([]any{}, c.Args...)}
	}
	return result
}

// CallsTo returns a snapshot of all recorded invocations of the named method.
// Each returned Call has its own Args slice (see Calls for rationale).
func (d *Driver) CallsTo(method string) []Call {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []Call
	for _, c := range d.calls {
		if c.Method == method {
			result = append(result, Call{Method: c.Method, Args: append([]any{}, c.Args...)})
		}
	}
	return result
}

// CallCount returns the number of times the named method was invoked.
func (d *Driver) CallCount(method string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for _, c := range d.calls {
		if c.Method == method {
			count++
		}
	}
	return count
}

// Called returns true if the named method was invoked at least once.
func (d *Driver) Called(method string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.calls {
		if c.Method == method {
			return true
		}
	}
	return false
}

// Reset clears all recorded calls and all programmed reactions.
// It enables reuse of the same Driver across multiple sub-tests.
func (d *Driver) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = []Call{}
	d.reactions = make(map[string][]*reaction)
}
