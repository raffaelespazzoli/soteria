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
// planned.go implements the planned migration workflow — the first real
// DRGroupHandler that drives actual DR operations through the StorageProvider
// driver. The workflow has two phases:
//
//  1. Step 0 (global pre-execution via PreExecute): stops all origin VMs,
//     stops replication on all volume groups, and polls replication status
//     until all volumes reach sync completion. This guarantees RPO=0 because
//     no new writes can arrive after VMs are stopped and replication is flushed.
//
//  2. Per-DRGroup (via ExecuteGroup, called by the wave executor): for each
//     group within a wave, calls SetSource(force=false) on each volume group
//     to promote target volumes to Source, then starts VMs via VMManager.
//     StopReplication is called per-group as well (idempotent after Step 0)
//     to support retry scenarios where Step 0 succeeded but per-group failed.
//
// The disaster failover handler (Story 4.4) uses force=true, skips Step 0,
// and ignores origin errors — the key differentiator.

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

	defaultSyncPollInterval = 2 * time.Second
	defaultSyncTimeout      = 10 * time.Minute
)

// PlannedMigrationHandler implements DRGroupHandler for planned migration.
// It also exposes PreExecute for the global Step 0 phase that runs before
// the wave executor dispatches any groups.
type PlannedMigrationHandler struct {
	Driver           drivers.StorageProvider
	VMManager        VMManager
	SyncPollInterval time.Duration
	SyncTimeout      time.Duration

	// cacheMu guards vgIDCache for concurrent ExecuteGroup goroutines.
	cacheMu   sync.Mutex
	vgIDCache map[string]drivers.VolumeGroupID
}

func (h *PlannedMigrationHandler) syncPollInterval() time.Duration {
	if h.SyncPollInterval > 0 {
		return h.SyncPollInterval
	}
	return defaultSyncPollInterval
}

func (h *PlannedMigrationHandler) syncTimeout() time.Duration {
	if h.SyncTimeout > 0 {
		return h.SyncTimeout
	}
	return defaultSyncTimeout
}

// initCacheLocked initializes the cache map. Must be called under cacheMu.
func (h *PlannedMigrationHandler) initCacheLocked() {
	if h.vgIDCache == nil {
		h.vgIDCache = make(map[string]drivers.VolumeGroupID)
	}
}

// resolveVolumeGroupID resolves a VolumeGroupInfo to a driver-level VolumeGroupID
// by calling CreateVolumeGroup (idempotent — returns existing if matched).
// Safe for concurrent use from multiple ExecuteGroup goroutines.
func (h *PlannedMigrationHandler) resolveVolumeGroupID(
	ctx context.Context, driver drivers.StorageProvider, vg soteriav1alpha1.VolumeGroupInfo,
) (drivers.VolumeGroupID, error) {
	cacheKey := vg.Namespace + "/" + vg.Name

	h.cacheMu.Lock()
	h.initCacheLocked()
	if id, ok := h.vgIDCache[cacheKey]; ok {
		h.cacheMu.Unlock()
		return id, nil
	}
	h.cacheMu.Unlock()

	info, err := driver.CreateVolumeGroup(ctx, drivers.VolumeGroupSpec{
		Name:      vg.Name,
		Namespace: vg.Namespace,
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
// Phase 1: Stop all origin VMs (graceful shutdown).
// Phase 2: Stop replication on all volume groups (initiates final flush).
// Phase 3: Poll GetReplicationStatus until all volumes are synced (RPO=0).
func (h *PlannedMigrationHandler) PreExecute(ctx context.Context, groups []ExecutionGroup) error {
	logger := log.FromContext(ctx)

	if len(groups) == 0 {
		return nil
	}

	// Collect unique VMs across all groups (deduplicate by namespace/name).
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

	// Phase 1: Stop all origin VMs.
	logger.Info("Starting Step 0: stopping origin VMs", "vmCount", len(uniqueVMs))
	for _, vm := range uniqueVMs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := h.VMManager.StopVM(ctx, vm.name, vm.namespace); err != nil {
			return fmt.Errorf("stopping origin VM %s/%s: %w", vm.namespace, vm.name, err)
		}
	}

	// Collect all volume groups across all execution groups with their drivers.
	type vgWithDriver struct {
		vg     soteriav1alpha1.VolumeGroupInfo
		driver drivers.StorageProvider
	}
	seenVG := make(map[string]bool)
	var allVGs []vgWithDriver
	for _, g := range groups {
		for _, vg := range g.Chunk.VolumeGroups {
			key := vg.Namespace + "/" + vg.Name
			if !seenVG[key] {
				seenVG[key] = true
				allVGs = append(allVGs, vgWithDriver{vg: vg, driver: g.Driver})
			}
		}
	}

	// Resolve volume group IDs.
	var resolved []resolvedVG
	for _, vgd := range allVGs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		vgID, err := h.resolveVolumeGroupID(ctx, vgd.driver, vgd.vg)
		if err != nil {
			return err
		}
		resolved = append(resolved, resolvedVG{id: vgID, driver: vgd.driver, name: vgd.vg.Name})
	}

	// Phase 2: Stop replication on all volume groups.
	logger.Info("Step 0: stopping replication on all volume groups", "volumeGroupCount", len(resolved))
	for _, rvg := range resolved {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := rvg.driver.StopReplication(ctx, rvg.id, drivers.StopReplicationOptions{Force: false}); err != nil {
			return fmt.Errorf("stopping replication for volume group %s: %w", rvg.name, err)
		}
	}

	// Phase 3: Wait for sync completion.
	logger.Info("Step 0: waiting for replication sync", "volumeGroupCount", len(resolved))
	return h.waitForSync(ctx, resolved)
}

// waitForSync polls GetReplicationStatus at configured intervals until all
// volume groups report sync completion or the timeout expires.
func (h *PlannedMigrationHandler) waitForSync(ctx context.Context, vgs []resolvedVG) error {
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

// isSynced returns true when a volume group has completed its final sync.
func isSynced(status drivers.ReplicationStatus) bool {
	return status.Role == drivers.RoleNonReplicated || status.Health == drivers.HealthHealthy
}

// ExecuteGroup implements DRGroupHandler for a single DRGroup within a wave.
// Steps: StopReplication → SetSource → StartVM (for each VM).
func (h *PlannedMigrationHandler) ExecuteGroup(ctx context.Context, group ExecutionGroup) error {
	logger := log.FromContext(ctx)
	driver := group.Driver

	// Step a: StopReplication on each volume group (idempotent after Step 0).
	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		vgID, err := h.resolveVolumeGroupID(ctx, driver, vg)
		if err != nil {
			return err
		}

		logger.V(1).Info("Stopping replication for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.StopReplication(ctx, vgID, drivers.StopReplicationOptions{Force: false}); err != nil {
			return fmt.Errorf("step %s failed for volume group %s: %w", StepStopReplication, vg.Name, err)
		}
	}

	// Step b: SetSource on each volume group.
	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		vgID, err := h.resolveVolumeGroupID(ctx, driver, vg)
		if err != nil {
			return err
		}

		logger.V(1).Info("Setting source for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.SetSource(ctx, vgID, drivers.SetSourceOptions{Force: false}); err != nil {
			return fmt.Errorf("step %s failed for volume group %s: %w", StepSetSource, vg.Name, err)
		}
	}

	// Step c: Start each VM.
	for _, vm := range group.Chunk.VMs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.V(1).Info("Starting VM for DRGroup",
			"vm", vm.Name, "namespace", vm.Namespace, "wave", group.WaveIndex)
		if err := h.VMManager.StartVM(ctx, vm.Name, vm.Namespace); err != nil {
			return fmt.Errorf("step %s failed for VM %s/%s: %w", StepStartVM, vm.Namespace, vm.Name, err)
		}
	}

	return nil
}

// Compile-time interface checks.
var (
	_ DRGroupHandler = (*PlannedMigrationHandler)(nil)
	_ StepHandler    = (*PlannedMigrationHandler)(nil)
)

// resolvedVG bundles a resolved volume group ID with its driver for sync polling.
type resolvedVG struct {
	id     drivers.VolumeGroupID
	driver drivers.StorageProvider
	name   string
}

// ExecuteGroupWithSteps executes a single DRGroup and returns step statuses
// for per-step recording in DRGroupStatus. This variant records each operation.
func (h *PlannedMigrationHandler) ExecuteGroupWithSteps(
	ctx context.Context, group ExecutionGroup,
) ([]soteriav1alpha1.StepStatus, error) {
	logger := log.FromContext(ctx)
	driver := group.Driver
	var steps []soteriav1alpha1.StepStatus

	recordStep := func(name, status, message string) {
		now := metav1.Now()
		steps = append(steps, soteriav1alpha1.StepStatus{
			Name:      name,
			Status:    status,
			Message:   message,
			Timestamp: &now,
		})
	}

	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return steps, ctx.Err()
		}
		vgID, err := h.resolveVolumeGroupID(ctx, driver, vg)
		if err != nil {
			recordStep(StepStopReplication, "Failed", err.Error())
			return steps, err
		}
		logger.V(1).Info("Stopping replication for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.StopReplication(ctx, vgID, drivers.StopReplicationOptions{Force: false}); err != nil {
			recordStep(StepStopReplication, "Failed",
				fmt.Sprintf("Failed to stop replication for volume group %s: %v", vg.Name, err))
			return steps, fmt.Errorf("step %s failed for volume group %s: %w", StepStopReplication, vg.Name, err)
		}
		recordStep(StepStopReplication, "Succeeded", fmt.Sprintf("Stopped replication for volume group %s", vg.Name))
	}

	for _, vg := range group.Chunk.VolumeGroups {
		if ctx.Err() != nil {
			return steps, ctx.Err()
		}
		vgID, err := h.resolveVolumeGroupID(ctx, driver, vg)
		if err != nil {
			recordStep(StepSetSource, "Failed", err.Error())
			return steps, err
		}
		logger.V(1).Info("Setting source for DRGroup volume group",
			"volumeGroup", vg.Name, "wave", group.WaveIndex)
		if err := driver.SetSource(ctx, vgID, drivers.SetSourceOptions{Force: false}); err != nil {
			recordStep(StepSetSource, "Failed", fmt.Sprintf("Failed to set source for volume group %s: %v", vg.Name, err))
			return steps, fmt.Errorf("step %s failed for volume group %s: %w", StepSetSource, vg.Name, err)
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
			return steps, fmt.Errorf("step %s failed for VM %s/%s: %w", StepStartVM, vm.Namespace, vm.Name, err)
		}
		recordStep(StepStartVM, "Succeeded", fmt.Sprintf("Started VM %s", vm.Name))
	}

	return steps, nil
}
