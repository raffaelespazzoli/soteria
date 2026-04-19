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

// executor.go implements the wave executor — the runtime orchestration engine
// that drives DR operations wave by wave. The execution pipeline re-discovers
// VMs at execution time (not relying on stale DRPlan status), then processes
// waves sequentially with concurrent DRGroups within each wave. A pluggable
// DRGroupHandler interface abstracts per-group workflow steps (planned migration,
// disaster failover) so the executor is workflow-agnostic.
//
// Pipeline: discover → group by wave → resolve consistency → chunk → execute waves → complete.

package engine

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

// DRGroupHandler abstracts the per-DRGroup workflow logic. Stories 4.3
// (planned migration) and 4.4 (disaster failover) provide real
// implementations; Story 4.2 provides a NoOpHandler for testing.
type DRGroupHandler interface {
	ExecuteGroup(ctx context.Context, group ExecutionGroup) error
}

// ExecutionGroup bundles a DRGroupChunk with its resolved StorageProvider
// driver so the handler does not need to resolve drivers itself.
type ExecutionGroup struct {
	Chunk     DRGroupChunk
	Driver    drivers.StorageProvider
	WaveIndex int
}

// WaveExecutor orchestrates sequential wave execution with concurrent DRGroups.
// Status updates are serialized via statusMu to prevent races when multiple
// goroutines update the same DRExecution concurrently.
type WaveExecutor struct {
	Client          client.Client
	CoreClient      corev1client.CoreV1Interface
	VMDiscoverer    VMDiscoverer
	NamespaceLookup NamespaceLookup
	Registry        *drivers.Registry
	SCLister        drivers.StorageClassLister

	statusMu sync.Mutex
}

// ExecuteInput holds the inputs for a single executor invocation.
type ExecuteInput struct {
	Execution *soteriav1alpha1.DRExecution
	Plan      *soteriav1alpha1.DRPlan
	Handler   DRGroupHandler
}

// Execute runs the full execution pipeline: discover → group → chunk → execute
// waves → compute result. It returns nil on success; errors indicate
// infrastructure failures (not DRGroup failures, which are recorded in status).
func (e *WaveExecutor) Execute(ctx context.Context, input ExecuteInput) error {
	logger := log.FromContext(ctx)
	exec := input.Execution
	plan := input.Plan

	logger.Info("Starting wave execution", "plan", plan.Name, "execution", exec.Name)

	// Step 1: Discover VMs at execution time — do NOT rely on stale plan status.
	vms, err := e.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
	if err != nil {
		logger.Error(err, "VM discovery failed during execution")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultFailed,
			fmt.Sprintf("VM discovery failed: %v", err))
	}

	// Empty plan: no VMs → Succeeded with zero waves.
	if len(vms) == 0 {
		logger.Info("No VMs discovered, completing with Succeeded")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultSucceeded, "")
	}

	// Step 2: Group by wave.
	discovery := GroupByWave(vms, plan.Spec.WaveLabel)

	// Step 3: Resolve volume groups.
	consistency, err := ResolveVolumeGroups(ctx, vms, plan.Spec.WaveLabel, e.NamespaceLookup)
	if err != nil {
		logger.Error(err, "Volume group resolution failed during execution")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultFailed,
			fmt.Sprintf("Volume group resolution failed: %v", err))
	}

	// Step 4: Chunk waves using existing chunker.
	chunkInput := buildChunkInput(discovery, consistency, vms, plan.Spec.WaveLabel)
	chunkResult := ChunkWaves(chunkInput, plan.Spec.MaxConcurrentFailovers)

	// Step 5: Initialize DRExecution.Status.Waves with Pending entries.
	exec.Status.Waves = make([]soteriav1alpha1.WaveStatus, len(chunkResult.Waves))
	for i, wc := range chunkResult.Waves {
		groups := make([]soteriav1alpha1.DRGroupExecutionStatus, len(wc.Chunks))
		for j, chunk := range wc.Chunks {
			vmNames := make([]string, len(chunk.VMs))
			for k, vm := range chunk.VMs {
				vmNames[k] = vm.Name
			}
			groups[j] = soteriav1alpha1.DRGroupExecutionStatus{
				Name:    chunk.Name,
				Result:  soteriav1alpha1.DRGroupResultPending,
				VMNames: vmNames,
			}
		}
		exec.Status.Waves[i] = soteriav1alpha1.WaveStatus{
			WaveIndex: i,
			Groups:    groups,
		}
	}
	if err := e.persistStatus(ctx, exec); err != nil {
		return fmt.Errorf("writing initial wave status: %w", err)
	}

	// Step 6: Execute waves sequentially.
	for i, wc := range chunkResult.Waves {
		if ctx.Err() != nil {
			logger.Info("Context cancelled, stopping execution")
			return e.finishExecution(ctx, exec, plan, e.computeResult(exec), "Context cancelled")
		}
		e.executeWave(ctx, i, wc.Chunks, input.Handler, exec)
	}

	// Step 7: Compute overall result and complete.
	result := e.computeResult(exec)
	logger.Info("Wave execution completed", "result", result)
	return e.finishExecution(ctx, exec, plan, result, "")
}

// executeWave runs all DRGroup chunks in a wave concurrently using
// sync.WaitGroup (NOT errgroup, which cancels siblings on first error —
// opposite of fail-forward semantics).
func (e *WaveExecutor) executeWave(
	ctx context.Context, waveIdx int, chunks []DRGroupChunk,
	handler DRGroupHandler, exec *soteriav1alpha1.DRExecution,
) {
	logger := log.FromContext(ctx)
	logger.Info("Starting wave execution", "wave", waveIdx, "chunks", len(chunks))

	now := metav1.Now()
	e.setWaveStartTime(exec, waveIdx, &now)

	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, c DRGroupChunk) {
			defer wg.Done()
			e.executeGroup(ctx, waveIdx, idx, c, handler, exec)
		}(i, chunk)
	}
	wg.Wait()

	completionTime := metav1.Now()
	e.setWaveCompletionTime(exec, waveIdx, &completionTime)
	logger.Info("Wave execution completed", "wave", waveIdx)
}

// executeGroup processes a single DRGroup chunk: resolves the driver, calls
// the handler, and records the result.
func (e *WaveExecutor) executeGroup(
	ctx context.Context, waveIdx, groupIdx int, chunk DRGroupChunk,
	handler DRGroupHandler, exec *soteriav1alpha1.DRExecution,
) {
	logger := log.FromContext(ctx)

	startTime := metav1.Now()
	vmNames := e.getGroupVMNames(exec, waveIdx, groupIdx)

	e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
		Name:      chunk.Name,
		Result:    soteriav1alpha1.DRGroupResultInProgress,
		VMNames:   vmNames,
		StartTime: &startTime,
	})

	// Resolve the storage driver for this group.
	driver, err := e.resolveDriver(ctx, chunk)
	if err != nil {
		logger.Error(err, "Driver resolution failed", "wave", waveIdx, "group", chunk.Name)
		completionTime := metav1.Now()
		e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
			Name:           chunk.Name,
			Result:         soteriav1alpha1.DRGroupResultFailed,
			VMNames:        vmNames,
			Error:          fmt.Sprintf("driver resolution failed: %v", err),
			StartTime:      &startTime,
			CompletionTime: &completionTime,
		})
		return
	}

	execGroup := ExecutionGroup{
		Chunk:     chunk,
		Driver:    driver,
		WaveIndex: waveIdx,
	}
	err = handler.ExecuteGroup(ctx, execGroup)

	completionTime := metav1.Now()
	if err != nil {
		logger.Error(err, "DRGroup failed", "wave", waveIdx, "group", chunk.Name)
		e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
			Name:           chunk.Name,
			Result:         soteriav1alpha1.DRGroupResultFailed,
			VMNames:        vmNames,
			Error:          err.Error(),
			StartTime:      &startTime,
			CompletionTime: &completionTime,
		})
		return
	}

	logger.Info("DRGroup completed", "wave", waveIdx, "group", chunk.Name, "result", "Completed")
	e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
		Name:           chunk.Name,
		Result:         soteriav1alpha1.DRGroupResultCompleted,
		VMNames:        vmNames,
		StartTime:      &startTime,
		CompletionTime: &completionTime,
	})
}

// resolveDriver resolves the StorageProvider for a DRGroup chunk by reading
// the first VM's PVC storage class and mapping it through the driver registry:
// VM → kubevirt volumes → PVC → storageClassName → SCLister → provisioner → Registry.
// All volumes within the chunk must use the same storage class — homogeneous
// storage is an architectural invariant (Dell-to-Dell, ODF-to-ODF).
func (e *WaveExecutor) resolveDriver(ctx context.Context, chunk DRGroupChunk) (drivers.StorageProvider, error) {
	if e.Registry == nil {
		return nil, fmt.Errorf("driver registry not configured")
	}
	if len(chunk.VMs) == 0 {
		return nil, fmt.Errorf("chunk %q has no VMs", chunk.Name)
	}

	logger := log.FromContext(ctx)

	storageClass, err := e.resolveChunkStorageClass(ctx, chunk)
	if err != nil {
		return nil, err
	}
	if storageClass == "" {
		logger.V(1).Info("No PVC storage class found, using fallback driver", "chunk", chunk.Name)
		return e.Registry.GetDriver("")
	}

	return e.Registry.GetDriverForPVC(ctx, storageClass, e.SCLister)
}

// resolveChunkStorageClass reads the kubevirt VMs in a chunk, extracts PVC
// claim names, reads the PVCs, and returns the common storage class name.
// Returns an error if volumes in the chunk use different storage classes
// (heterogeneous storage is not supported).
func (e *WaveExecutor) resolveChunkStorageClass(
	ctx context.Context, chunk DRGroupChunk,
) (string, error) {
	var commonSC string

	for _, vmRef := range chunk.VMs {
		var vm kubevirtv1.VirtualMachine
		if err := e.Client.Get(ctx, types.NamespacedName{
			Name: vmRef.Name, Namespace: vmRef.Namespace,
		}, &vm); err != nil {
			return "", fmt.Errorf("fetching VM %s/%s: %w",
				vmRef.Namespace, vmRef.Name, err)
		}

		if vm.Spec.Template == nil {
			continue
		}

		for _, vol := range vm.Spec.Template.Spec.Volumes {
			claimName := ""
			if vol.PersistentVolumeClaim != nil {
				claimName = vol.PersistentVolumeClaim.ClaimName
			} else if vol.DataVolume != nil {
				claimName = vol.DataVolume.Name
			}
			if claimName == "" {
				continue
			}

			if e.CoreClient == nil {
				return "", fmt.Errorf(
					"nil CoreClient, cannot read PVC %s/%s",
					vmRef.Namespace, claimName)
			}
			pvc, err := e.CoreClient.PersistentVolumeClaims(
				vmRef.Namespace,
			).Get(ctx, claimName, metav1.GetOptions{})
			if err != nil {
				return "", fmt.Errorf(
					"fetching PVC %s/%s for VM %s: %w",
					vmRef.Namespace, claimName, vmRef.Name, err)
			}

			scName := ""
			if pvc.Spec.StorageClassName != nil {
				scName = *pvc.Spec.StorageClassName
			}
			if scName == "" {
				continue
			}

			if commonSC == "" {
				commonSC = scName
			} else if scName != commonSC {
				return "", fmt.Errorf(
					"heterogeneous storage classes in chunk %q: "+
						"found %q on PVC %s/%s but expected %q",
					chunk.Name, scName, vmRef.Namespace,
					claimName, commonSC)
			}
		}
	}

	return commonSC, nil
}

// computeResult scans all groups across all waves and determines the overall
// execution result. Groups still in Pending or InProgress (e.g. after context
// cancellation) are treated as incomplete — if any exist alongside completed
// groups, the result is PartiallySucceeded; if no group completed, Failed.
func (e *WaveExecutor) computeResult(exec *soteriav1alpha1.DRExecution) soteriav1alpha1.ExecutionResult {
	var completed, failed, pending, total int
	for _, wave := range exec.Status.Waves {
		for _, group := range wave.Groups {
			total++
			switch group.Result {
			case soteriav1alpha1.DRGroupResultCompleted:
				completed++
			case soteriav1alpha1.DRGroupResultFailed:
				failed++
			default:
				pending++
			}
		}
	}
	if total == 0 {
		return soteriav1alpha1.ExecutionResultSucceeded
	}
	if completed == 0 {
		return soteriav1alpha1.ExecutionResultFailed
	}
	if failed > 0 || pending > 0 {
		return soteriav1alpha1.ExecutionResultPartiallySucceeded
	}
	return soteriav1alpha1.ExecutionResultSucceeded
}

// finishExecution sets the final result, completion time, and conditions on the
// DRExecution. If the result is Succeeded or PartiallySucceeded, it also
// advances the DRPlan phase via CompleteTransition.
func (e *WaveExecutor) finishExecution(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan, result soteriav1alpha1.ExecutionResult,
	message string,
) error {
	logger := log.FromContext(ctx)

	now := metav1.Now()
	exec.Status.Result = result
	exec.Status.CompletionTime = &now

	condStatus := metav1.ConditionTrue
	condReason := "ExecutionSucceeded"
	condMessage := fmt.Sprintf("Execution completed: %s", result)
	if result == soteriav1alpha1.ExecutionResultFailed {
		condStatus = metav1.ConditionFalse
		condReason = "ExecutionFailed"
		if message != "" {
			condMessage = message
		}
	}
	if result == soteriav1alpha1.ExecutionResultPartiallySucceeded {
		condReason = "ExecutionPartiallySucceeded"
	}

	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionFalse,
		Reason:             condReason,
		Message:            condMessage,
		ObservedGeneration: exec.Generation,
	})
	meta.SetStatusCondition(&exec.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus,
		Reason:             condReason,
		Message:            condMessage,
		ObservedGeneration: exec.Generation,
	})

	if err := e.persistStatus(ctx, exec); err != nil {
		return fmt.Errorf("writing final execution status: %w", err)
	}

	// Advance DRPlan phase on success or partial success.
	if result == soteriav1alpha1.ExecutionResultSucceeded ||
		result == soteriav1alpha1.ExecutionResultPartiallySucceeded {
		previousPhase := plan.Status.Phase
		newPhase, err := CompleteTransition(plan.Status.Phase)
		if err != nil {
			logger.Error(err, "Could not complete phase transition", "currentPhase", plan.Status.Phase)
		} else {
			planPatch := client.MergeFrom(plan.DeepCopy())
			plan.Status.Phase = newPhase
			if err := e.Client.Status().Patch(ctx, plan, planPatch); err != nil {
				logger.Error(err, "Failed to advance DRPlan phase", "plan", plan.Name, "targetPhase", newPhase)
				return fmt.Errorf("advancing DRPlan phase: %w", err)
			}
			logger.Info("Advanced DRPlan phase", "plan", plan.Name, "from", previousPhase, "to", newPhase)
		}
	}

	return nil
}

// persistStatus serializes status writes to prevent concurrent goroutines from
// racing on the DRExecution status subresource. Re-fetches before update to
// ensure the latest resourceVersion.
func (e *WaveExecutor) persistStatus(ctx context.Context, exec *soteriav1alpha1.DRExecution) error {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()

	statusCopy := exec.Status.DeepCopy()
	if err := e.Client.Get(ctx, client.ObjectKeyFromObject(exec), exec); err != nil {
		return fmt.Errorf("re-fetching DRExecution before status update: %w", err)
	}
	exec.Status = *statusCopy
	return e.Client.Status().Update(ctx, exec)
}

// setGroupStatus updates a single DRGroup's status in memory and persists it.
func (e *WaveExecutor) setGroupStatus(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
	waveIdx, groupIdx int, status soteriav1alpha1.DRGroupExecutionStatus,
) {
	e.statusMu.Lock()
	exec.Status.Waves[waveIdx].Groups[groupIdx] = status
	e.statusMu.Unlock()

	if err := e.persistStatus(ctx, exec); err != nil {
		logger := log.FromContext(ctx)
		logger.Error(err, "Failed to persist group status update",
			"wave", waveIdx, "group", status.Name)
	}
}

// setWaveStartTime sets wave StartTime in memory (persisted with next group update).
func (e *WaveExecutor) setWaveStartTime(
	exec *soteriav1alpha1.DRExecution, waveIdx int, t *metav1.Time,
) {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	exec.Status.Waves[waveIdx].StartTime = t
}

// setWaveCompletionTime sets wave CompletionTime in memory.
func (e *WaveExecutor) setWaveCompletionTime(
	exec *soteriav1alpha1.DRExecution, waveIdx int, t *metav1.Time,
) {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	exec.Status.Waves[waveIdx].CompletionTime = t
}

// getGroupVMNames returns the VM names for a group (snapshot under lock).
func (e *WaveExecutor) getGroupVMNames(
	exec *soteriav1alpha1.DRExecution, waveIdx, groupIdx int,
) []string {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	names := exec.Status.Waves[waveIdx].Groups[groupIdx].VMNames
	out := make([]string, len(names))
	copy(out, names)
	return out
}

// buildChunkInput constructs the ChunkInput by matching VolumeGroups to waves.
func buildChunkInput(
	discovery DiscoveryResult,
	consistency *ConsistencyResult,
	vms []VMReference,
	waveLabel string,
) ChunkInput {
	vmWave := make(map[string]string, len(vms))
	for _, vm := range vms {
		key := vm.Namespace + "/" + vm.Name
		vmWave[key] = vm.Labels[waveLabel]
	}

	waveGroups := make(map[string][]soteriav1alpha1.VolumeGroupInfo)
	for _, vg := range consistency.VolumeGroups {
		if len(vg.VMNames) == 0 {
			continue
		}
		key := vg.Namespace + "/" + vg.VMNames[0]
		wave := vmWave[key]
		waveGroups[wave] = append(waveGroups[wave], vg)
	}

	input := ChunkInput{
		WaveGroups: make([]WaveGroupWithVolumes, len(discovery.Waves)),
	}
	for i, wg := range discovery.Waves {
		input.WaveGroups[i] = WaveGroupWithVolumes{
			WaveKey:      wg.WaveKey,
			VolumeGroups: waveGroups[wg.WaveKey],
		}
	}
	return input
}
