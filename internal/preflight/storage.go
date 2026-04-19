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

// storage.go resolves storage backends for VMs by inspecting their PVC references,
// mapping PVC storage classes to CSI provisioners via a StorageClassLister, and
// verifying driver availability through the driver registry. The DRPlan reconciler
// calls ResolveBackends during preflight composition to show which storage driver
// handles each VM's volumes — giving platform engineers visibility into the
// storage layer without triggering execution.

package preflight

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/engine"
)

// StorageBackendResolver resolves the storage driver name for each VM based on
// its PVC storage class.
type StorageBackendResolver interface {
	ResolveBackends(ctx context.Context, vms []engine.VMReference) (map[string]string, []string, error)
}

// TypedStorageBackendResolver resolves storage backends by reading typed
// kubevirtv1.VirtualMachine objects for volume specs, then looking up PVCs
// to determine storage class → CSI provisioner → driver mappings via the
// driver registry. SCLister resolves a StorageClass name to its CSI
// provisioner; Registry resolves the provisioner to a StorageProvider.
type TypedStorageBackendResolver struct {
	Client     client.Reader
	CoreClient corev1client.CoreV1Interface
	Registry   *drivers.Registry
	SCLister   drivers.StorageClassLister
}

// Compile-time interface check.
var _ StorageBackendResolver = (*TypedStorageBackendResolver)(nil)

// ResolveBackends returns a map of "namespace/vmName" → storage backend name
// and a list of warnings for non-fatal issues (missing PVCs, unknown classes).
//
// Storage backend is resolved from PVC storage classes because driver selection
// is implicit in the architecture (FR21) — there is no StorageProviderConfig
// CRD; the orchestrator discovers which driver handles each VM's volumes by
// inspecting existing cluster state.
func (r *TypedStorageBackendResolver) ResolveBackends(
	ctx context.Context, vms []engine.VMReference,
) (map[string]string, []string, error) {
	backends := make(map[string]string, len(vms))
	warnings := make([]string, 0, len(vms))

	for _, vmRef := range vms {
		key := vmRef.Namespace + "/" + vmRef.Name
		backend, vmWarnings := r.resolveVM(ctx, vmRef)
		backends[key] = backend
		warnings = append(warnings, vmWarnings...)
	}

	return backends, warnings, nil
}

func (r *TypedStorageBackendResolver) resolveVM(
	ctx context.Context, vmRef engine.VMReference,
) (string, []string) {
	var vm kubevirtv1.VirtualMachine
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name: vmRef.Name, Namespace: vmRef.Namespace,
	}, &vm); err != nil {
		return backendUnknown, []string{
			fmt.Sprintf("VM %s/%s: could not fetch VirtualMachine: %v", vmRef.Namespace, vmRef.Name, err),
		}
	}

	if vm.Spec.Template == nil || len(vm.Spec.Template.Spec.Volumes) == 0 {
		return backendNone, []string{
			fmt.Sprintf("VM %s/%s: no PVC volumes found", vmRef.Namespace, vmRef.Name),
		}
	}

	var claimNames []string
	for _, vol := range vm.Spec.Template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			claimNames = append(claimNames, vol.PersistentVolumeClaim.ClaimName)
		}
		if vol.DataVolume != nil {
			claimNames = append(claimNames, vol.DataVolume.Name)
		}
	}

	if len(claimNames) == 0 {
		return backendNone, []string{
			fmt.Sprintf("VM %s/%s: no PVC volumes found", vmRef.Namespace, vmRef.Name),
		}
	}

	var resolvedBackend string
	var warnings []string
	var seenClass string

	for _, claimName := range claimNames {
		pvc, err := r.CoreClient.PersistentVolumeClaims(vmRef.Namespace).Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("VM %s/%s: PVC %q not found: %v",
				vmRef.Namespace, vmRef.Name, claimName, err))
			if resolvedBackend == "" {
				resolvedBackend = backendUnknown
			}
			continue
		}

		scName := ""
		if pvc.Spec.StorageClassName != nil {
			scName = *pvc.Spec.StorageClassName
		}

		if seenClass == "" {
			seenClass = scName
		} else if scName != seenClass {
			warnings = append(warnings, fmt.Sprintf(
				"VM %s/%s has PVCs across multiple storage classes; using %s",
				vmRef.Namespace, vmRef.Name, seenClass))
		}

		if resolvedBackend != "" {
			continue
		}

		backend, warn := r.resolveProvisioner(ctx, vmRef, scName)
		resolvedBackend = backend
		if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	if resolvedBackend == "" {
		resolvedBackend = backendUnknown
	}

	return resolvedBackend, warnings
}

// resolveProvisioner maps a storage class name to a CSI provisioner via the
// SCLister and verifies a driver exists in the Registry. Returns the
// provisioner name as the backend string or "unknown" with a warning.
func (r *TypedStorageBackendResolver) resolveProvisioner(
	ctx context.Context, vmRef engine.VMReference, scName string,
) (string, string) {
	logger := log.FromContext(ctx)

	if r.SCLister == nil {
		return backendUnknown, fmt.Sprintf(
			"VM %s/%s: nil StorageClassLister, cannot resolve storage class %q",
			vmRef.Namespace, vmRef.Name, scName)
	}
	if r.Registry == nil {
		return backendUnknown, fmt.Sprintf(
			"VM %s/%s: nil Registry, cannot resolve storage class %q",
			vmRef.Namespace, vmRef.Name, scName)
	}

	provisioner, err := r.SCLister.GetProvisioner(ctx, scName)
	if err != nil {
		return backendUnknown, fmt.Sprintf(
			"VM %s/%s: could not resolve provisioner for storage class %q: %v",
			vmRef.Namespace, vmRef.Name, scName, err)
	}
	if provisioner == "" {
		return backendUnknown, fmt.Sprintf(
			"VM %s/%s: empty provisioner for storage class %q",
			vmRef.Namespace, vmRef.Name, scName)
	}

	_, err = r.Registry.GetDriver(provisioner)
	if err != nil {
		if errors.Is(err, drivers.ErrDriverNotFound) {
			return backendUnknown, fmt.Sprintf(
				"VM %s/%s: no storage driver registered for provisioner %q (storage class %q)",
				vmRef.Namespace, vmRef.Name, provisioner, scName)
		}
		return backendUnknown, fmt.Sprintf(
			"VM %s/%s: error looking up driver for provisioner %q: %v",
			vmRef.Namespace, vmRef.Name, provisioner, err)
	}

	logger.V(1).Info("Resolved storage backend via registry",
		"storageClass", scName, "provisioner", provisioner)
	return provisioner, ""
}
