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
//   planned_migration → {GracefulShutdown: true,  Force: false}
//   disaster          → {GracefulShutdown: false, Force: true}
//
// When GracefulShutdown=true, the handler runs a global Step 0 pre-execution:
//   1. Stop all origin VMs (graceful shutdown)
//   2. Stop replication on all volume groups (initiates final flush)
//   3. Poll GetReplicationStatus until sync completion (RPO=0)
//
// When GracefulShutdown=false, PreExecute is a no-op and per-DRGroup skips
// StopReplication — SetSource(force=true) handles promotion directly.

package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

const (
	StepStopReplication = "StopReplication"
	StepSetSource       = "SetSource"
	StepStartVM         = "StartVM"
	StepWaitVMReady     = "WaitVMReady"

	defaultSyncPollInterval = 2 * time.Second
	defaultSyncTimeout      = 10 * time.Minute
)

// FailoverConfig drives FailoverHandler behavior without mode-string switching.
type FailoverConfig struct {
	// GracefulShutdown enables Step 0 (stop VMs, stop replication, sync wait)
	// and per-group StopReplication. False for disaster failover.
	GracefulShutdown bool
	// Force is passed to SetSourceOptions and StopReplicationOptions.
	// True for disaster (origin may be unreachable).
	Force bool
}

// FailoverHandler implements DRGroupHandler for both planned migration and
// disaster failover. It also exposes PreExecute for the global Step 0 phase
// that runs before the wave executor dispatches any groups.
type FailoverHandler struct {
	Driver           drivers.StorageProvider
	VMManager        VMManager
	Config           FailoverConfig
	SyncPollInterval time.Duration
	SyncTimeout      time.Duration

	cacheMu   sync.Mutex
	vgIDCache map[string]drivers.VolumeGroupID
}

func (h *FailoverHandler) syncPollInterval() time.Duration {
	if h.SyncPollInterval > 0 {
		return h.SyncPollInterval
	}
	return defaultSyncPollInterval
}

func (h *FailoverHandler) syncTimeout() time.Duration {
	if h.SyncTimeout > 0 {
		return h.SyncTimeout
	}
	return defaultSyncTimeout
}

func (h *FailoverHandler) initCacheLocked() {
	if h.vgIDCache == nil {
		h.vgIDCache = make(map[string]drivers.VolumeGroupID)
	}
}

// resolveVolumeGroupID resolves a VolumeGroupInfo to a driver-level VolumeGroupID
// by calling CreateVolumeGroup (idempotent — returns existing if matched).
// When pvcResolver is non-nil it populates PVCNames from the VG's VMs.
// Safe for concurrent use from multiple ExecuteGroup goroutines.
func (h *FailoverHandler) resolveVolumeGroupID(
	ctx context.Context, driver drivers.StorageProvider, vg soteriav1alpha1.VolumeGroupInfo,
	pvcResolver PVCResolver,
) (drivers.VolumeGroupID, error) {
	cacheKey := vg.Namespace + "/" + vg.Name

	h.cacheMu.Lock()
	h.initCacheLocked()
	if id, ok := h.vgIDCache[cacheKey]; ok {
		h.cacheMu.Unlock()
		return id, nil
	}
	h.cacheMu.Unlock()

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

	h.cacheMu.Lock()
	h.vgIDCache[cacheKey] = info.ID
	h.cacheMu.Unlock()
	return info.ID, nil
}

// PreExecute runs Step 0 — the global pre-execution phase that must complete
// for ALL VMs/volumes BEFORE any wave starts.
//
// When GracefulShutdown=false (disaster), returns nil immediately — there is
// no Step 0 because the origin site may be unreachable.
//
// When GracefulShutdown=true (planned migration):
//
//	Phase 1: Stop all origin VMs (graceful shutdown).
//	Phase 2: Stop replication on all volume groups (initiates final flush).
//	Phase 3: Poll GetReplicationStatus until all volumes are synced (RPO=0).
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

	type vgWithDriver struct {
		vg          soteriav1alpha1.VolumeGroupInfo
		driver      drivers.StorageProvider
		pvcResolver PVCResolver
	}
	seenVG := make(map[string]bool)
	var allVGs []vgWithDriver
	for _, g := range groups {
		for _, vg := range g.Chunk.VolumeGroups {
			key := vg.Namespace + "/" + vg.Name
			if !seenVG[key] {
				seenVG[key] = true
				allVGs = append(allVGs, vgWithDriver{vg: vg, driver: g.DriverForVG(vg.Name), pvcResolver: g.PVCResolver})
			}
		}
	}

	var resolved []resolvedVG
	for _, vgd := range allVGs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		vgID, err := h.resolveVolumeGroupID(ctx, vgd.driver, vgd.vg, vgd.pvcResolver)
		if err != nil {
			return err
		}
		resolved = append(resolved, resolvedVG{id: vgID, driver: vgd.driver, name: vgd.vg.Name})
	}

	logger.Info("Step 0: stopping replication on all volume groups", "volumeGroupCount", len(resolved))
	for _, rvg := range resolved {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := rvg.driver.StopReplication(ctx, rvg.id, drivers.StopReplicationOptions{Force: false}); err != nil {
			return fmt.Errorf("stopping replication for volume group %s: %w", rvg.name, err)
		}
	}

	logger.Info("Step 0: waiting for replication sync", "volumeGroupCount", len(resolved))
	return h.waitForSync(ctx, resolved)
}

// waitForSync polls GetReplicationStatus at configured intervals until all
// volume groups report sync completion or the timeout expires.
func (h *FailoverHandler) waitForSync(ctx context.Context, vgs []resolvedVG) error {
	if len(vgs) == 0 {
		return nil
	}

	logger := log.FromContext(ctx)
	timeout := h.syncTimeout()
	interval := h.syncPollInterval()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		synced := 0
		for _, rvg := range vgs {
			status, err := rvg.driver.GetReplicationStatus(ctx, rvg.id)
			if err != nil {
				return fmt.Errorf("getting replication status for volume group %s: %w", rvg.name, err)
			}
			if isSynced(status) {
				synced++
			}
		}

		logger.V(1).Info("Polling replication status", "synced", synced, "total", len(vgs))

		if synced == len(vgs) {
			logger.Info("Step 0 complete: replication sync verified", "volumeGroups", len(vgs))
			return nil
		}

		if time.Now().After(deadline) {
			remaining := len(vgs) - synced
			return fmt.Errorf("sync timeout: %d of %d volume groups not synced after %v",
				remaining, len(vgs), timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func isSynced(status drivers.ReplicationStatus) bool {
	return status.Role == drivers.RoleNonReplicated || status.Health == drivers.HealthHealthy
}

// ExecuteGroup implements DRGroupHandler for a single DRGroup within a wave.
// Returns *GroupError for step failures to enable structured error propagation.
//
// GracefulShutdown=true:  StopReplication → SetSource(force=false) → StartVM
// GracefulShutdown=false: SetSource(force=true) → StartVM
func (h *FailoverHandler) ExecuteGroup(ctx context.Context, group ExecutionGroup) error {
	logger := log.FromContext(ctx)

	if h.Config.GracefulShutdown {
		for _, vg := range group.Chunk.VolumeGroups {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			driver := group.DriverForVG(vg.Name)
			vgID, err := h.resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
			if err != nil {
				return &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
			}

			logger.V(1).Info("Stopping replication for DRGroup volume group",
				"volumeGroup", vg.Name, "wave", group.WaveIndex)
			if err := driver.StopReplication(ctx, vgID, drivers.StopReplicationOptions{Force: false}); err != nil {
				return &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
			}
		}
	}

	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		driver := group.DriverForVG(vg.Name)
		vgID, err := h.resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
		if err != nil {
			return &GroupError{StepName: StepSetSource, Target: vg.Name, Err: err}
		}

		logger.V(1).Info("Setting source for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.SetSource(ctx, vgID, drivers.SetSourceOptions{Force: h.Config.Force}); err != nil {
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

	if h.Config.GracefulShutdown {
		for _, vg := range group.Chunk.VolumeGroups {
			if ctx.Err() != nil {
				return steps, ctx.Err()
			}
			driver := group.DriverForVG(vg.Name)
			vgID, err := h.resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
			if err != nil {
				recordStep(StepStopReplication, "Failed", err.Error())
				return steps, &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
			}
			logger.V(1).Info("Stopping replication for DRGroup volume group",
				"volumeGroup", vg.Name, "wave", group.WaveIndex)
			if err := driver.StopReplication(ctx, vgID, drivers.StopReplicationOptions{Force: false}); err != nil {
				recordStep(StepStopReplication, "Failed",
					fmt.Sprintf("Failed to stop replication for volume group %s: %v", vg.Name, err))
				return steps, &GroupError{StepName: StepStopReplication, Target: vg.Name, Err: err}
			}
			recordStep(StepStopReplication, "Succeeded", fmt.Sprintf("Stopped replication for volume group %s", vg.Name))
		}
	}

	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return steps, ctx.Err()
		}
		driver := group.DriverForVG(vg.Name)
		vgID, err := h.resolveVolumeGroupID(ctx, driver, vg, group.PVCResolver)
		if err != nil {
			recordStep(StepSetSource, "Failed", err.Error())
			return steps, &GroupError{StepName: StepSetSource, Target: vg.Name, Err: err}
		}
		logger.V(1).Info("Setting source for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.SetSource(ctx, vgID, drivers.SetSourceOptions{Force: h.Config.Force}); err != nil {
			recordStep(StepSetSource, "Failed", fmt.Sprintf("Failed to set source for volume group %s: %v", vg.Name, err))
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

// resolvedVG bundles a resolved volume group ID with its driver for sync polling.
type resolvedVG struct {
	id     drivers.VolumeGroupID
	driver drivers.StorageProvider
	name   string
}
