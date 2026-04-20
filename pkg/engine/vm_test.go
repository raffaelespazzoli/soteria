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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func keyFor(name, namespace string) types.NamespacedName {
	return types.NamespacedName{Name: name, Namespace: namespace}
}

func strategyPtr(s kubevirtv1.VirtualMachineRunStrategy) *kubevirtv1.VirtualMachineRunStrategy {
	return &s
}

func newVM(name, namespace string, strategy *kubevirtv1.VirtualMachineRunStrategy) *kubevirtv1.VirtualMachine {
	return &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: kubevirtv1.VirtualMachineSpec{
			RunStrategy: strategy,
		},
	}
}

func TestKubeVirtVMManager_StopVM_Succeeds(t *testing.T) {
	vm := newVM("vm1", "ns1", strategyPtr(kubevirtv1.RunStrategyAlways))
	cl := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(vm).Build()
	mgr := &KubeVirtVMManager{Client: cl}

	if err := mgr.StopVM(context.Background(), "vm1", "ns1"); err != nil {
		t.Fatalf("StopVM failed: %v", err)
	}

	var updated kubevirtv1.VirtualMachine
	if err := cl.Get(context.Background(), keyFor("vm1", "ns1"), &updated); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if updated.Spec.RunStrategy == nil || *updated.Spec.RunStrategy != kubevirtv1.RunStrategyHalted {
		t.Errorf("RunStrategy = %v, want Halted", updated.Spec.RunStrategy)
	}
}

func TestKubeVirtVMManager_StopVM_AlreadyStopped(t *testing.T) {
	vm := newVM("vm-halted", "ns1", strategyPtr(kubevirtv1.RunStrategyHalted))
	cl := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(vm).Build()
	mgr := &KubeVirtVMManager{Client: cl}

	if err := mgr.StopVM(context.Background(), "vm-halted", "ns1"); err != nil {
		t.Fatalf("StopVM should be idempotent: %v", err)
	}
}

func TestKubeVirtVMManager_StartVM_Succeeds(t *testing.T) {
	vm := newVM("vm-start", "ns1", strategyPtr(kubevirtv1.RunStrategyHalted))
	cl := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(vm).Build()
	mgr := &KubeVirtVMManager{Client: cl}

	if err := mgr.StartVM(context.Background(), "vm-start", "ns1"); err != nil {
		t.Fatalf("StartVM failed: %v", err)
	}

	var updated kubevirtv1.VirtualMachine
	if err := cl.Get(context.Background(), keyFor("vm-start", "ns1"), &updated); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if updated.Spec.RunStrategy == nil || *updated.Spec.RunStrategy != kubevirtv1.RunStrategyAlways {
		t.Errorf("RunStrategy = %v, want Always", updated.Spec.RunStrategy)
	}
}

func TestKubeVirtVMManager_StartVM_AlreadyRunning(t *testing.T) {
	vm := newVM("vm-running", "ns1", strategyPtr(kubevirtv1.RunStrategyAlways))
	cl := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(vm).Build()
	mgr := &KubeVirtVMManager{Client: cl}

	if err := mgr.StartVM(context.Background(), "vm-running", "ns1"); err != nil {
		t.Fatalf("StartVM should be idempotent: %v", err)
	}
}

func TestKubeVirtVMManager_StartVM_NotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	mgr := &KubeVirtVMManager{Client: cl}

	err := mgr.StartVM(context.Background(), "no-such-vm", "ns1")
	if err == nil {
		t.Fatal("StartVM should fail for non-existent VM")
	}
}

func TestKubeVirtVMManager_StopVM_NotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	mgr := &KubeVirtVMManager{Client: cl}

	err := mgr.StopVM(context.Background(), "missing-vm", "ns2")
	if err == nil {
		t.Fatal("StopVM should fail for non-existent VM")
	}
}

func TestKubeVirtVMManager_IsVMRunning(t *testing.T) {
	tests := []struct {
		name      string
		vmName    string
		namespace string
		strategy  *kubevirtv1.VirtualMachineRunStrategy
		want      bool
	}{
		{"Always is running", "vm-always", "ns1", strategyPtr(kubevirtv1.RunStrategyAlways), true},
		{"RerunOnFailure is running", "vm-rerun", "ns1", strategyPtr(kubevirtv1.RunStrategyRerunOnFailure), true},
		{"Halted is not running", "vm-halted", "ns2", strategyPtr(kubevirtv1.RunStrategyHalted), false},
		{"Manual is not running", "vm-manual", "ns2", strategyPtr(kubevirtv1.RunStrategyManual), false},
		{"nil strategy is not running", "vm-nil", "ns1", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := newVM(tt.vmName, tt.namespace, tt.strategy)
			cl := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(vm).Build()
			mgr := &KubeVirtVMManager{Client: cl}

			got, err := mgr.IsVMRunning(context.Background(), tt.vmName, tt.namespace)
			if err != nil {
				t.Fatalf("IsVMRunning failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("IsVMRunning = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNoOpVMManager_AllMethods(t *testing.T) {
	mgr := &NoOpVMManager{}
	ctx := context.Background()

	if err := mgr.StopVM(ctx, "vm", "ns"); err != nil {
		t.Errorf("StopVM returned error: %v", err)
	}
	if err := mgr.StartVM(ctx, "vm", "ns"); err != nil {
		t.Errorf("StartVM returned error: %v", err)
	}
	running, err := mgr.IsVMRunning(ctx, "vm", "ns")
	if err != nil {
		t.Errorf("IsVMRunning returned error: %v", err)
	}
	if running {
		t.Error("IsVMRunning should return false")
	}
}
