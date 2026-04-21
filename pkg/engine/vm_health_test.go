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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newVMHealthTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = kubevirtv1.AddToScheme(s)
	return s
}

func TestKubeVirtVMHealthValidator_HealthyVM(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-db01",
			Namespace: "ns-erp",
		},
		Status: kubevirtv1.VirtualMachineStatus{
			PrintableStatus: kubevirtv1.VirtualMachineStatusStopped,
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(newVMHealthTestScheme()).
		WithObjects(vm).
		Build()

	validator := &KubeVirtVMHealthValidator{Client: cl}
	err := validator.ValidateVMHealth(context.Background(), "vm-db01", "ns-erp")
	if err != nil {
		t.Errorf("expected nil for healthy VM, got: %v", err)
	}
}

func TestKubeVirtVMHealthValidator_VMNotFound(t *testing.T) {
	cl := fake.NewClientBuilder().
		WithScheme(newVMHealthTestScheme()).
		Build()

	validator := &KubeVirtVMHealthValidator{Client: cl}
	err := validator.ValidateVMHealth(context.Background(), "vm-missing", "ns-erp")
	if err == nil {
		t.Fatal("expected error for missing VM")
	}
	if !strings.Contains(err.Error(), "unpredictable state") {
		t.Errorf("expected 'unpredictable state' error, got: %v", err)
	}
}

func TestKubeVirtVMHealthValidator_VMInMigratingState(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-db01",
			Namespace: "ns-erp",
		},
		Status: kubevirtv1.VirtualMachineStatus{
			PrintableStatus: kubevirtv1.VirtualMachineStatusMigrating,
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(newVMHealthTestScheme()).
		WithObjects(vm).
		Build()

	validator := &KubeVirtVMHealthValidator{Client: cl}
	err := validator.ValidateVMHealth(context.Background(), "vm-db01", "ns-erp")
	if err == nil {
		t.Fatal("expected error for VM in Migrating state")
	}
	if !strings.Contains(err.Error(), "unpredictable state") {
		t.Errorf("expected 'unpredictable state' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Migrating") {
		t.Errorf("expected error to mention Migrating, got: %v", err)
	}
}

func TestKubeVirtVMHealthValidator_VMPaused(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-db01",
			Namespace: "ns-erp",
		},
		Status: kubevirtv1.VirtualMachineStatus{
			PrintableStatus: kubevirtv1.VirtualMachineStatusRunning,
			Conditions: []kubevirtv1.VirtualMachineCondition{
				{
					Type:   kubevirtv1.VirtualMachinePaused,
					Status: "True",
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(newVMHealthTestScheme()).
		WithObjects(vm).
		Build()

	validator := &KubeVirtVMHealthValidator{Client: cl}
	err := validator.ValidateVMHealth(context.Background(), "vm-db01", "ns-erp")
	if err == nil {
		t.Fatal("expected error for paused VM")
	}
	if !strings.Contains(err.Error(), "paused") {
		t.Errorf("expected error to mention 'paused', got: %v", err)
	}
}

func TestNoOpVMHealthValidator_AlwaysHealthy(t *testing.T) {
	validator := NoOpVMHealthValidator{}
	err := validator.ValidateVMHealth(context.Background(), "any-vm", "any-ns")
	if err != nil {
		t.Errorf("NoOpVMHealthValidator should always return nil, got: %v", err)
	}
}
