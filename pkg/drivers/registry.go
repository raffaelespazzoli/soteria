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
	"fmt"
	"sort"
	"sync"
)

// DriverFactory creates a new StorageProvider instance. Drivers register a
// factory via RegisterDriver; the registry calls it on first use to avoid
// allocating resources until a PVC actually references the driver.
type DriverFactory func() StorageProvider

// StorageClassLister abstracts the lookup of a StorageClass provisioner name.
// The registry uses this interface to resolve a PVC's storage class to a
// provisioner without requiring a full Kubernetes client, keeping unit tests
// free of k8s dependencies.
type StorageClassLister interface {
	GetProvisioner(ctx context.Context, storageClassName string) (string, error)
}

// Registry holds a mapping from CSI provisioner names (e.g.,
// "rook-ceph.rbd.csi.ceph.com") to driver factories. It is safe for
// concurrent use by multiple goroutines — reads use an RWMutex so parallel
// reconcile loops do not contend.
type Registry struct {
	mu              sync.RWMutex
	drivers         map[string]DriverFactory
	fallbackFactory DriverFactory
}

// NewRegistry creates an empty driver registry.
func NewRegistry() *Registry {
	return &Registry{
		drivers: make(map[string]DriverFactory),
	}
}

// RegisterDriver associates a provisioner name with a driver factory. It panics
// if the same provisioner is registered twice — fail-fast at startup rather than
// silent override (same pattern as prometheus.MustRegister).
func (r *Registry) RegisterDriver(provisionerName string, factory DriverFactory) {
	if provisionerName == "" {
		panic("RegisterDriver called with empty provisioner name")
	}
	if factory == nil {
		panic("RegisterDriver called with nil factory")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.drivers[provisionerName]; exists {
		panic(fmt.Sprintf("storage driver already registered for provisioner %q", provisionerName))
	}
	r.drivers[provisionerName] = factory
}

// SetFallbackDriver sets a fallback factory that is used when GetDriver cannot
// find an explicitly registered driver. This enables noop-driver fallback for
// dev/CI environments. Panics if called twice or with a nil factory (same
// fail-fast-at-startup pattern as RegisterDriver).
func (r *Registry) SetFallbackDriver(factory DriverFactory) {
	if factory == nil {
		panic("SetFallbackDriver called with nil factory")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fallbackFactory != nil {
		panic("fallback driver already set")
	}
	r.fallbackFactory = factory
}

// GetDriver returns a StorageProvider for the given provisioner name. When no
// driver is explicitly registered for the provisioner and a fallback factory
// has been set via SetFallbackDriver, the fallback driver is returned instead
// of ErrDriverNotFound.
func (r *Registry) GetDriver(provisionerName string) (StorageProvider, error) {
	r.mu.RLock()
	factory, ok := r.drivers[provisionerName]
	fallback := r.fallbackFactory
	r.mu.RUnlock()

	if !ok {
		if fallback != nil {
			return fallback(), nil
		}
		return nil, fmt.Errorf("%w: %s", ErrDriverNotFound, provisionerName)
	}
	return factory(), nil
}

// GetDriverForPVC resolves a PVC's storage class name to a provisioner via the
// StorageClassLister, then returns the registered driver for that provisioner.
// This is the primary lookup path during DR execution: PVC → StorageClass →
// provisioner → StorageProvider.
func (r *Registry) GetDriverForPVC(
	ctx context.Context, storageClassName string, scLister StorageClassLister,
) (StorageProvider, error) {
	if scLister == nil {
		return nil, fmt.Errorf("resolving provisioner for storage class %q: nil StorageClassLister", storageClassName)
	}
	provisioner, err := scLister.GetProvisioner(ctx, storageClassName)
	if err != nil {
		return nil, fmt.Errorf("resolving provisioner for storage class %q: %w", storageClassName, err)
	}
	if provisioner == "" {
		return nil, fmt.Errorf("resolving provisioner for storage class %q: empty provisioner string", storageClassName)
	}
	return r.GetDriver(provisioner)
}

// ListRegistered returns a sorted list of all registered provisioner names.
// Useful for diagnostics and startup logging.
func (r *Registry) ListRegistered() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.drivers))
	for name := range r.drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResetForTesting clears all registered drivers and the fallback factory. This
// must only be called from tests to ensure isolation between test cases.
func (r *Registry) ResetForTesting() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.drivers = make(map[string]DriverFactory)
	r.fallbackFactory = nil
}

// DefaultRegistry is the process-wide driver registry. Driver packages register
// themselves via init() functions that call RegisterDriver. Mirrors the
// http.DefaultServeMux and prometheus.DefaultRegisterer patterns.
var DefaultRegistry = NewRegistry()

// SetFallbackDriver sets a fallback factory on the DefaultRegistry.
func SetFallbackDriver(factory DriverFactory) {
	DefaultRegistry.SetFallbackDriver(factory)
}

// RegisterDriver registers a driver factory in the DefaultRegistry.
func RegisterDriver(provisionerName string, factory DriverFactory) {
	DefaultRegistry.RegisterDriver(provisionerName, factory)
}

// GetDriver returns a StorageProvider from the DefaultRegistry.
func GetDriver(provisionerName string) (StorageProvider, error) {
	return DefaultRegistry.GetDriver(provisionerName)
}

// GetDriverForPVC resolves a PVC storage class to a driver in the DefaultRegistry.
func GetDriverForPVC(
	ctx context.Context, storageClassName string, scLister StorageClassLister,
) (StorageProvider, error) {
	return DefaultRegistry.GetDriverForPVC(ctx, storageClassName, scLister)
}

// ListRegistered returns registered provisioner names from the DefaultRegistry.
func ListRegistered() []string {
	return DefaultRegistry.ListRegistered()
}

// ResetForTesting clears the DefaultRegistry. Must only be called from tests.
func ResetForTesting() {
	DefaultRegistry.ResetForTesting()
}
