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

// Tier 2 – Architecture:
// failover.go implements the unified FailoverHandler that drives both planned
// migration and disaster failover through the StorageProvider driver. The same
// handler is also used for failback (from DRedSteadyState) — the state machine
// encodes direction, not the handler.
//
// Behavior is controlled entirely by FailoverConfig, not the execution mode
// string. The controller maps mode → config at dispatch time:
//
//   planned_migration → {GracefulShutdown: true}
//   disaster          → {GracefulShutdown: false}
//
// The failover handler only moves volumes to NonReplicated and starts VMs.
// Re-establishing replication (SetSource) is the reprotect handler's job.
//
// Per-group execution is a single unified path for both planned and disaster:
//
//	StopReplication → StartVM
//
// StopReplication is idempotent — in the planned case, Step 0 already stopped
// origin VMs, and per-group StopReplication transitions volumes to
// NonReplicated. In the disaster case, StopReplication breaks the replication
// link and promotes target disks to writable.
//
// When GracefulShutdown=true (planned migration), PreExecute runs Step 0:
//
//	Stop all origin VMs (graceful shutdown).
//
// When GracefulShutdown=false (disaster), PreExecute is a no-op because the
// origin site may be unreachable.

package engine

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

const (
	StepStopReplication = "StopReplication"
	StepStartVM         = "StartVM"
	StepWaitVMReady     = "WaitVMReady"
)

// FailoverConfig drives FailoverHandler behavior without mode-string switching.
type FailoverConfig struct {
	// GracefulShutdown enables Step 0 (stop VMs).
	// When true (planned migration), PreExecute stops all origin VMs.
	// When false (disaster), PreExecute is a no-op because the origin site
	// may be unreachable. Per-group execution is identical in both modes:
	// StopReplication → StartVM.
	GracefulShutdown bool
}

// FailoverHandler implements DRGroupHandler for both planned migration and
// disaster failover. It also exposes PreExecute for the global Step 0 phase
// that runs before the wave executor dispatches any groups.
type FailoverHandler struct {
	VMManager VMManager
	Config    FailoverConfig
}

// resolveVolumeGroupID resolves a VolumeGroupInfo to a driver-level VolumeGroupID
// by calling CreateVolumeGroup (idempotent — returns existing if matched).
// When pvcResolver is non-nil it populates PVCNames from the VG's VMs.
func resolveVolumeGroupID(
	ctx context.Context, driver drivers.StorageProvider, vg soteriav1alpha1.VolumeGroupInfo,
	pvcResolver PVCResolver,
) (drivers.VolumeGroupID, error) {
	var pvcNames []string
	if pvcResolver != nil {
		for _, vmName := range vg.VMNames {
			names, err := pvcResolver.ResolvePVCNames(ctx, vmName, vg.Namespace)
			if err != nil {
				return "", fmt.Errorf("resolving PVC names for VM %s/%s: %w", vg.Namespace, vmName, err)
			}
			pvcNames = append(pvcNames, names...)
		}
	}

	info, err := driver.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      vg.Name,
		Namespace: vg.Namespace,
		PVCNames:  pvcNames,
	})
	if err != nil {
		return "", fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
	}
	return info.ID, nil
}

// PreExecute runs Step 0 — the global pre-execution phase that must complete
// for ALL VMs BEFORE any wave starts.
//
// When GracefulShutdown=false (disaster), returns nil immediately — there is
// no Step 0 because the origin site may be unreachable.
//
// When GracefulShutdown=true (planned migration):
//
//	Stop all origin VMs (graceful shutdown).
func (h *FailoverHandler) PreExecute(ctx context.Context, groups []ExecutionGroup) error {
	if !h.Config.GracefulShutdown {
		return nil
	}

	logger := log.FromContext(ctx)

	if len(groups) == 0 {
		return nil
	}

	type vmKey struct{ name, namespace string }
	seen := make(map[vmKey]bool)
	var uniqueVMs []vmKey
	for _, g := range groups {
		for _, vm := range g.Chunk.VMs {
			k := vmKey{name: vm.Name, namespace: vm.Namespace}
			if !seen[k] {
				seen[k] = true
				uniqueVMs = append(uniqueVMs, k)
			}
		}
	}

	logger.Info("Starting Step 0: stopping origin VMs", "vmCount", len(uniqueVMs))
	for _, vm := range uniqueVMs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := h.VMManager.StopVM(ctx, vm.name, vm.namespace); err != nil {
			return fmt.Errorf("stopping origin VM %s/%s: %w", vm.namespace, vm.name, err)
		}
	}

	return nil
}

// ExecuteGroup implements DRGroupHandler for a single DRGroup within a wave.
// Returns *GroupError for step failures to enable structured error propagation.
//
// Unified path for both planned and disaster: StopReplication → StartVM.
// StopReplication is idempotent — when Step 0 has already moved volumes to
// NonReplicated (planned), it is a no-op.
func (h *FailoverHandler) ExecuteGroup(ctx context.Context, group ExecutionGroup) error {
	logger := log.FromContext(ctx)

	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		driver := group.DriverForVG(vg.Name)
		vgID, err := resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
		if err != nil {
			return &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
		}

		logger.V(1).Info("Stopping replication for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.StopReplication(ctx, vgID); err != nil {
			return &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
		}
	}

	for _, vm := range group.Chunk.VMs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.V(1).Info("Starting VM for DRGroup",
			"vm", vm.Name, "namespace", vm.Namespace, "wave", group.WaveIndex)
		if err := h.VMManager.StartVM(ctx, vm.Name, vm.Namespace); err != nil {
			return &GroupError{StepName: StepStartVM, Target: vm.Namespace + "/" + vm.Name, Err: err}
		}
	}

	return nil
}

// ExecuteGroupWithSteps executes a single DRGroup and returns step statuses
// for per-step recording in DRGroupStatus. Returns *GroupError for step failures.
// Also forwards steps to group.StepRecorder for real-time DRGroupStatus updates.
func (h *FailoverHandler) ExecuteGroupWithSteps(
	ctx context.Context, group ExecutionGroup,
) ([]soteriav1alpha1.StepStatus, error) {
	logger := log.FromContext(ctx)
	var steps []soteriav1alpha1.StepStatus

	sr := group.StepRecorder
	if sr == nil {
		sr = noopStepRecorder{}
	}

	recordStep := func(name, status, message string) {
		now := metav1.Now()
		step := soteriav1alpha1.StepStatus{
			Name:      name,
			Status:    status,
			Message:   message,
			Timestamp: &now,
		}
		steps = append(steps, step)
		_ = sr.RecordStep(ctx, step)
	}

	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return steps, ctx.Err()
		}
		driver := group.DriverForVG(vg.Name)
		vgID, err := resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
		if err != nil {
			recordStep(StepStopReplication, "Failed", err.Error())
			return steps, &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
		}
		logger.V(1).Info("Stopping replication for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.StopReplication(ctx, vgID); err != nil {
			recordStep(StepStopReplication, "Failed",
				fmt.Sprintf("Failed to stop replication for volume group %s: %v", vg.Name, err))
			return steps, &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
		}
		recordStep(StepStopReplication, "Succeeded", fmt.Sprintf("Stopped replication for volume group %s", vg.Name))
	}

	for _, vm := range group.Chunk.VMs {
		if ctx.Err() != nil {
			return steps, ctx.Err()
		}
		logger.V(1).Info("Starting VM for DRGroup",
			"vm", vm.Name, "namespace", vm.Namespace, "wave", group.WaveIndex)
		if err := h.VMManager.StartVM(ctx, vm.Name, vm.Namespace); err != nil {
			recordStep(StepStartVM, "Failed", fmt.Sprintf("Failed to start VM %s: %v", vm.Name, err))
			return steps, &GroupError{StepName: StepStartVM, Target: vm.Namespace + "/" + vm.Name, Err: err}
		}
		recordStep(StepStartVM, "Succeeded", fmt.Sprintf("Started VM %s", vm.Name))
	}

	return steps, nil
}

var (
	_ DRGroupHandler = (*FailoverHandler)(nil)
	_ StepHandler    = (*FailoverHandler)(nil)
)
