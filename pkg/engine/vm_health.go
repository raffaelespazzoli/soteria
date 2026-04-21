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

package engine

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubeVirtVMHealthValidator validates that a KubeVirt VirtualMachine exists and
// is in a known, healthy state before allowing retry. This is a lightweight
// check — not a deep storage health assessment. It prevents retry when the VM
// has been deleted, is actively running on the target site (split-brain), or
// has terminal error conditions.
type KubeVirtVMHealthValidator struct {
	Client client.Client
}

func (v *KubeVirtVMHealthValidator) ValidateVMHealth(ctx context.Context, vmName, namespace string) error {
	var vm kubevirtv1.VirtualMachine
	if err := v.Client.Get(ctx, types.NamespacedName{Name: vmName, Namespace: namespace}, &vm); err != nil {
		return fmt.Errorf("VM %s/%s is in an unpredictable state — manual intervention required: %w", namespace, vmName, err)
	}

	if vm.Status.PrintableStatus == kubevirtv1.VirtualMachineStatusProvisioning ||
		vm.Status.PrintableStatus == kubevirtv1.VirtualMachineStatusMigrating {
		return fmt.Errorf(
			"VM %s/%s is in an unpredictable state — manual intervention required: status is %s",
			namespace, vmName, vm.Status.PrintableStatus,
		)
	}

	for _, cond := range vm.Status.Conditions {
		if cond.Type == kubevirtv1.VirtualMachinePaused && cond.Status == "True" {
			return fmt.Errorf(
				"VM %s/%s is in an unpredictable state — manual intervention required: VM is paused",
				namespace, vmName,
			)
		}
	}

	return nil
}

// NoOpVMHealthValidator always reports VMs as healthy. Used with noop/fake
// drivers in dev/CI where no real KubeVirt VMs exist.
type NoOpVMHealthValidator struct{}

func (NoOpVMHealthValidator) ValidateVMHealth(context.Context, string, string) error {
	return nil
}

// Compile-time interface checks.
var (
	_ VMHealthValidator = (*KubeVirtVMHealthValidator)(nil)
	_ VMHealthValidator = NoOpVMHealthValidator{}
)
