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
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// VMManager abstracts KubeVirt VM lifecycle control, enabling the planned
// migration handler to stop origin VMs (Step 0) and start target VMs
// (per-DRGroup) without coupling to the KubeVirt API directly.
type VMManager interface {
	StopVM(ctx context.Context, name, namespace string) error
	StartVM(ctx context.Context, name, namespace string) error
	IsVMRunning(ctx context.Context, name, namespace string) (bool, error)
	// IsVMReady returns true when the VM has reached Running state
	// (status.printableStatus == Running). Unlike IsVMRunning which checks
	// spec.runStrategy, this checks the actual observed VM state.
	IsVMReady(ctx context.Context, name, namespace string) (bool, error)
}

// KubeVirtVMManager implements VMManager using a controller-runtime client to
// patch VirtualMachine.Spec.RunStrategy. Uses merge patches (not strategic
// merge) because KubeVirt types are external and may lack strategic merge
// patch metadata.
type KubeVirtVMManager struct {
	Client client.Client
}

func (m *KubeVirtVMManager) StopVM(ctx context.Context, name, namespace string) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Stopping VM", "name", name, "namespace", namespace)

	var vm kubevirtv1.VirtualMachine
	if err := m.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &vm); err != nil {
		return fmt.Errorf("fetching VM %s/%s: %w", namespace, name, err)
	}

	if vm.Spec.RunStrategy != nil && *vm.Spec.RunStrategy == kubevirtv1.RunStrategyHalted {
		return nil
	}

	return m.patchRunStrategy(ctx, name, namespace, kubevirtv1.RunStrategyHalted)
}

func (m *KubeVirtVMManager) StartVM(ctx context.Context, name, namespace string) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Starting VM", "name", name, "namespace", namespace)

	var vm kubevirtv1.VirtualMachine
	if err := m.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &vm); err != nil {
		return fmt.Errorf("fetching VM %s/%s: %w", namespace, name, err)
	}

	if vm.Spec.RunStrategy != nil && *vm.Spec.RunStrategy == kubevirtv1.RunStrategyAlways {
		return nil
	}

	return m.patchRunStrategy(ctx, name, namespace, kubevirtv1.RunStrategyAlways)
}

func (m *KubeVirtVMManager) IsVMRunning(ctx context.Context, name, namespace string) (bool, error) {
	var vm kubevirtv1.VirtualMachine
	if err := m.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &vm); err != nil {
		return false, fmt.Errorf("fetching VM %s/%s: %w", namespace, name, err)
	}

	if vm.Spec.RunStrategy != nil {
		switch *vm.Spec.RunStrategy {
		case kubevirtv1.RunStrategyAlways, kubevirtv1.RunStrategyRerunOnFailure:
			return true, nil
		}
	}

	return false, nil
}

func (m *KubeVirtVMManager) IsVMReady(ctx context.Context, name, namespace string) (bool, error) {
	var vm kubevirtv1.VirtualMachine
	if err := m.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &vm); err != nil {
		return false, fmt.Errorf("fetching VM %s/%s: %w", namespace, name, err)
	}
	return vm.Status.PrintableStatus == kubevirtv1.VirtualMachineStatusRunning, nil
}

func (m *KubeVirtVMManager) patchRunStrategy(
	ctx context.Context, name, namespace string, strategy kubevirtv1.VirtualMachineRunStrategy,
) error {
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"runStrategy": strategy,
		},
	})
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	vm := &kubevirtv1.VirtualMachine{}
	vm.Name = name
	vm.Namespace = namespace
	return m.Client.Patch(ctx, vm, client.RawPatch(types.MergePatchType, patch))
}

// Compile-time interface check.
var _ VMManager = (*KubeVirtVMManager)(nil)
