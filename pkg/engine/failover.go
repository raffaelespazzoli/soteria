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
// Per-group execution promotes the target volume to writable and starts VMs:
//
//	SetSource → StartVM
//
// SetSource sets spec.replicationState = primary (writable). After Story 13.2,
// StopReplication always sets secondary (read-only), so the per-group path
// must use SetSource to make the target volume writable before starting VMs.
//
// When GracefulShutdown=true (planned migration), PreExecute runs Step 0:
//
//  1. Stop all origin VMs (graceful shutdown).
//  2. Call ResyncVolume on all target VGs to pull un-replicated data.
//  3. Return ErrResyncRequested — the reconciler waits for VR/VGR status
//     watches to confirm resync completion before calling StopReplication
//     (demote source to secondary). This event-driven gate guarantees zero
//     data loss during planned migrations with asynchronous replication.
//
// StopReplication is NOT called in PreExecute — it moves to the reconciler
// and is invoked only after all target VRs confirm resync completion.
//
// When GracefulShutdown=false (disaster), PreExecute is a no-op because the
// origin site may be unreachable.

package engine

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

const (
	StepSetSource   = "SetSource"
	StepStartVM     = "StartVM"
	StepWaitVMReady = "WaitVMReady"
)

// ErrResyncRequested is returned by PreExecute when ResyncVolume has been called
// on target VGs and the reconciler must wait for VR/VGR status watches to
// confirm resync completion before calling StopReplication (demote source).
// The reconciler checks errors.Is(err, ErrResyncRequested) and sets the
// ResyncPending condition instead of failing the execution.
var ErrResyncRequested = errors.New("resync requested, awaiting completion")

// FailoverConfig drives FailoverHandler behavior without mode-string switching.
type FailoverConfig struct {
	// GracefulShutdown enables Step 0 (stop VMs + resync target VGs).
	// When true (planned migration), PreExecute stops all origin VMs,
	// calls ResyncVolume on each target VG to pull un-replicated data,
	// and returns ErrResyncRequested so the reconciler waits for resync
	// completion before calling StopReplication (demote source).
	// When false (disaster), PreExecute is a no-op because the origin site
	// may be unreachable. Per-group execution is identical in both modes:
	// SetSource → StartVM.
	GracefulShutdown bool

	// SkipResync makes PreExecute skip ResyncVolume calls on target VGs.
	// Used in multi-site mode where the source site cannot access target
	// VR/VGR CRs — the target site calls ResyncVolume on its own local VRs.
	// PreExecute still calls StopReplication on LOCAL source VGs to demote
	// the primary images (creating the final mirror snapshot that the
	// target needs to replay), then returns ErrResyncRequested.
	SkipResync bool
}

// FailoverHandler implements DRGroupHandler for both planned migration and
// disaster failover. It also exposes PreExecute for the global Step 0 phase
// that runs before the wave executor dispatches any groups.
type FailoverHandler struct {
	VMManager VMManager
	Config    FailoverConfig
}

// resolveVolumeGroupID computes a deterministic VolumeGroupID and validates
// that the VR/VGR exists via GetVolumeGroup. The DRPlan reconciler is
// responsible for creating VR/VGR (Story 13.2); this path is read-only.
func resolveVolumeGroupID(
	ctx context.Context, driver drivers.StorageProvider,
	driverType string, vg soteriav1alpha1.VolumeGroupInfo,
) (drivers.VolumeGroupID, error) {
	vgID := drivers.VolumeGroupIDFor(driverType, vg.Namespace, vg.Name)
	if _, err := driver.GetVolumeGroup(ctx, vgID); err != nil {
		if errors.Is(err, drivers.ErrVolumeGroupNotFound) {
			return "", fmt.Errorf("VR/VGR not yet created by DRPlan reconciler for volume group %s: %w", vg.Name, err)
		}
		return "", fmt.Errorf("resolving volume group %s: %w", vg.Name, err)
	}
	return vgID, nil
}

// PreExecute runs Step 0 — the global pre-execution phase that must complete
// for ALL VMs BEFORE any wave starts.
//
// When GracefulShutdown=false (disaster), returns nil immediately — there is
// no Step 0 because the origin site may be unreachable.
//
// When GracefulShutdown=true (planned migration):
//
//  1. Stop all origin VMs (graceful shutdown).
//  2. Call ResyncVolume on each target VG to pull un-replicated data from the
//     current primary. The driver sets spec.replicationState=resync on the
//     target VR/VGR CRs — the actual data sync is performed asynchronously
//     by the CSI Addons sidecar.
//  3. Return ErrResyncRequested so the reconciler sets the ResyncPending
//     condition and waits for VR/VGR status watches to confirm completion.
//     StopReplication (demote source) is called by the reconciler only after
//     resync completes — never in PreExecute.
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

	if h.Config.SkipResync {
		// Demote local primary VRs so the mirroring daemon creates a final
		// mirror snapshot. The target secondary must replay this snapshot
		// before it can be promoted. Without this demote the secondary stays
		// "not ready" forever because the primary keeps the image locked.
		type vgKey struct{ name, namespace string }
		seenVG := make(map[vgKey]bool)
		for _, g := range groups {
			for _, vg := range g.Chunk.VolumeGroups {
				k := vgKey{name: vg.Name, namespace: vg.Namespace}
				if seenVG[k] {
					continue
				}
				seenVG[k] = true

				driver := g.DriverForVG(vg.Name)
				vgID, err := resolveVolumeGroupID(ctx, driver, g.DriverType, vg)
				if err != nil {
					return fmt.Errorf("resolving volume group %s for StopReplication: %w", vg.Name, err)
				}
				if err := driver.StopReplication(ctx, vgID); err != nil {
					return fmt.Errorf("demoting volume group %s in Step 0: %w", vg.Name, err)
				}
			}
		}
		logger.Info("Step 0: VMs stopped, local VRs demoted (target site will resync)")
		return ErrResyncRequested
	}

	// Request data resync on target VGs. The driver patches
	// spec.replicationState=resync on the target VR/VGR CRs; the actual data
	// pull is asynchronous. The reconciler waits for VR/VGR status watches
	// before calling StopReplication.
	type vgKey struct{ name, namespace string }
	seenVG := make(map[vgKey]bool)
	for _, g := range groups {
		for _, vg := range g.Chunk.VolumeGroups {
			k := vgKey{name: vg.Name, namespace: vg.Namespace}
			if seenVG[k] {
				continue
			}
			seenVG[k] = true

			if ctx.Err() != nil {
				return ctx.Err()
			}

			driver := g.DriverForVG(vg.Name)
			vgID, err := resolveVolumeGroupID(ctx, driver, g.DriverType, vg)
			if err != nil {
				return fmt.Errorf("resolving volume group %s in Step 0: %w", vg.Name, err)
			}

			if err := driver.ResyncVolume(ctx, vgID); err != nil {
				return fmt.Errorf("requesting resync for volume group %s in Step 0: %w", vg.Name, err)
			}
		}
	}

	logger.Info("Step 0: resync requested on target VGs, awaiting completion")
	return ErrResyncRequested
}

// ExecuteGroup implements DRGroupHandler for a single DRGroup within a wave.
// Returns *GroupError for step failures to enable structured error propagation.
//
// Unified path for both planned and disaster: SetSource → StartVM.
// SetSource promotes the target VR/VGR to primary (writable) so VMs can
// start on writable volumes.
func (h *FailoverHandler) ExecuteGroup(ctx context.Context, group ExecutionGroup) error {
	logger := log.FromContext(ctx)

	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		driver := group.DriverForVG(vg.Name)
		vgID, err := resolveVolumeGroupID(ctx, driver, group.DriverType, vg)
		if err != nil {
			return &GroupError{StepName: StepSetSource, Target: vg.Name, Err: err}
		}

		logger.V(1).Info("Promoting volume group to primary (SetSource)",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.SetSource(ctx, vgID); err != nil {
			return &GroupError{StepName: StepSetSource, Target: vg.Name, Err: err}
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
// for per-step recording in DRGroupExecutionStatus. Returns *GroupError for
// step failures. Also forwards steps to group.StepRecorder.
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
		vgID, err := resolveVolumeGroupID(ctx, driver, group.DriverType, vg)
		if err != nil {
			recordStep(StepSetSource, "Failed", err.Error())
			return steps, &GroupError{StepName: StepSetSource, Target: vg.Name, Err: err}
		}
		logger.V(1).Info("Promoting volume group to primary (SetSource)",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.SetSource(ctx, vgID); err != nil {
			recordStep(StepSetSource, "Failed",
				fmt.Sprintf("Failed to set source for volume group %s: %v", vg.Name, err))
			return steps, &GroupError{StepName: StepSetSource, Target: vg.Name, Err: err}
		}
		recordStep(StepSetSource, "Succeeded", fmt.Sprintf("Set source for volume group %s", vg.Name))
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
