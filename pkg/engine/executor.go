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
	"errors"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/events"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

// GroupError carries structured error context from a handler step failure.
// Handlers SHOULD return *GroupError when a step fails so the executor can
// record step-level detail (step name + affected resource) without parsing
// error strings. The executor gracefully falls back to err.Error() for
// non-GroupError errors.
type GroupError struct {
	StepName string // e.g. "SetSource", "StopReplication", "StartVM", "DriverResolution", "PVCResolution"
	Target   string // volume group name or "namespace/vmName"
	Err      error  // underlying driver or system error
}

func (e *GroupError) Error() string {
	return fmt.Sprintf("%s for %s: %v", e.StepName, e.Target, e.Err)
}

func (e *GroupError) Unwrap() error { return e.Err }

// DRGroupHandler abstracts the per-DRGroup workflow logic. Stories 4.3
// (planned migration) and 4.4 (disaster failover) provide real
// implementations; Story 4.2 provides a NoOpHandler for testing.
// Handlers SHOULD return *GroupError when a step fails.
type DRGroupHandler interface {
	ExecuteGroup(ctx context.Context, group ExecutionGroup) error
}

// StepHandler is an optional extension of DRGroupHandler that returns
// per-step execution details alongside the error. Handlers that implement
// this interface get their steps recorded in DRGroupExecutionStatus.
type StepHandler interface {
	DRGroupHandler
	ExecuteGroupWithSteps(
		ctx context.Context, group ExecutionGroup,
	) ([]soteriav1alpha1.StepStatus, error)
}

// StepRecorder enables real-time DRGroupStatus updates without the handler
// knowing about Kubernetes resources. The executor's implementation writes
// to the DRGroupStatus status subresource; tests can inject a no-op or
// recording mock.
type StepRecorder interface {
	RecordStep(ctx context.Context, step soteriav1alpha1.StepStatus) error
}

// noopStepRecorder discards step recordings (used when DRGroupStatus is not configured).
type noopStepRecorder struct{}

func (noopStepRecorder) RecordStep(context.Context, soteriav1alpha1.StepStatus) error { return nil }

// drgroupStatusRecorder writes step updates to a DRGroupStatus resource.
type drgroupStatusRecorder struct {
	client    client.Client
	statusKey client.ObjectKey
	mu        sync.Mutex
}

func (r *drgroupStatusRecorder) RecordStep(ctx context.Context, step soteriav1alpha1.StepStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var dgs soteriav1alpha1.DRGroupStatus
	if err := r.client.Get(ctx, r.statusKey, &dgs); err != nil {
		return fmt.Errorf("re-fetching DRGroupStatus %s: %w", r.statusKey.Name, err)
	}
	dgs.Status.Steps = append(dgs.Status.Steps, step)
	return r.client.Status().Update(ctx, &dgs)
}

// ExecutionGroup bundles a DRGroupChunk with its resolved StorageProvider
// driver(s) so the handler does not need to resolve drivers itself.
// Drivers maps VolumeGroup name → driver for per-VG resolution. Driver is a
// fallback used when Drivers is nil or a VG has no entry.
type ExecutionGroup struct {
	Chunk        DRGroupChunk
	Driver       drivers.StorageProvider
	Drivers      map[string]drivers.StorageProvider
	WaveIndex    int
	StepRecorder StepRecorder
	PVCResolver  PVCResolver
}

// DriverForVG returns the driver for the named volume group.
// Falls back to Driver when no per-VG mapping exists.
func (g ExecutionGroup) DriverForVG(vgName string) drivers.StorageProvider {
	if g.Drivers != nil {
		if d, ok := g.Drivers[vgName]; ok {
			return d
		}
	}
	return g.Driver
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
	Recorder        events.EventRecorder
	PVCResolver     PVCResolver

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

// executeGroup processes a single DRGroup chunk: creates DRGroupStatus,
// resolves the driver, calls the handler with a StepRecorder, records the
// result, and emits events.
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

	// Create DRGroupStatus resource for real-time tracking.
	recorder := e.createDRGroupStatus(ctx, exec, waveIdx, chunk, vmNames)

	// Resolve storage drivers per volume group.
	driverMap, fallbackDriver, err := e.resolveDrivers(ctx, chunk)
	if err != nil {
		logger.Error(err, "Driver resolution failed", "wave", waveIdx, "group", chunk.Name)
		completionTime := metav1.Now()
		e.finishDRGroupStatus(ctx, recorder, soteriav1alpha1.DRGroupResultFailed, &completionTime)
		e.emitGroupEvent(exec, waveIdx, chunk.Name,
			&GroupError{StepName: "DriverResolution", Target: chunk.Name, Err: err})
		e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
			Name:           chunk.Name,
			Result:         soteriav1alpha1.DRGroupResultFailed,
			VMNames:        vmNames,
			Error:          fmt.Sprintf("step DriverResolution failed for %s: %v", chunk.Name, err),
			StartTime:      &startTime,
			CompletionTime: &completionTime,
		})
		return
	}

	execGroup := ExecutionGroup{
		Chunk:        chunk,
		Driver:       fallbackDriver,
		Drivers:      driverMap,
		WaveIndex:    waveIdx,
		StepRecorder: recorder,
		PVCResolver:  e.PVCResolver,
	}

	var steps []soteriav1alpha1.StepStatus
	if sh, ok := handler.(StepHandler); ok {
		steps, err = sh.ExecuteGroupWithSteps(ctx, execGroup)
	} else {
		err = handler.ExecuteGroup(ctx, execGroup)
	}

	completionTime := metav1.Now()
	if err != nil {
		errMsg := err.Error()
		var ge *GroupError
		if errors.As(err, &ge) {
			errMsg = fmt.Sprintf("step %s failed for %s: %v", ge.StepName, ge.Target, ge.Err)
			logger.Error(err, "DRGroup failed", "wave", waveIdx, "group", chunk.Name,
				"step", ge.StepName, "target", ge.Target)
		} else {
			logger.Error(err, "DRGroup failed", "wave", waveIdx, "group", chunk.Name)
		}
		e.finishDRGroupStatus(ctx, recorder, soteriav1alpha1.DRGroupResultFailed, &completionTime)
		e.emitGroupEvent(exec, waveIdx, chunk.Name, err)
		e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
			Name:           chunk.Name,
			Result:         soteriav1alpha1.DRGroupResultFailed,
			VMNames:        vmNames,
			Error:          errMsg,
			Steps:          steps,
			StartTime:      &startTime,
			CompletionTime: &completionTime,
		})
		return
	}

	e.finishDRGroupStatus(ctx, recorder, soteriav1alpha1.DRGroupResultCompleted, &completionTime)
	e.emitGroupCompletedEvent(exec, waveIdx, chunk.Name)
	logger.Info("DRGroup completed", "wave", waveIdx, "group", chunk.Name, "result", "Completed")
	e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
		Name:           chunk.Name,
		Result:         soteriav1alpha1.DRGroupResultCompleted,
		VMNames:        vmNames,
		Steps:          steps,
		StartTime:      &startTime,
		CompletionTime: &completionTime,
	})
}

// createDRGroupStatus creates a DRGroupStatus resource for real-time tracking
// and returns the StepRecorder for it. On AlreadyExists (requeue), it reuses
// the existing resource. Returns noopStepRecorder only for unexpected errors.
func (e *WaveExecutor) createDRGroupStatus(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
	waveIdx int, chunk DRGroupChunk, vmNames []string,
) StepRecorder {
	logger := log.FromContext(ctx)
	dgsName := fmt.Sprintf("%s-%s", exec.Name, chunk.Name)
	dgs := &soteriav1alpha1.DRGroupStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name: dgsName,
		},
		Spec: soteriav1alpha1.DRGroupStatusSpec{
			ExecutionName: exec.Name,
			WaveIndex:     waveIdx,
			GroupName:     chunk.Name,
			VMNames:       vmNames,
		},
	}

	if err := controllerutil.SetOwnerReference(exec, dgs, e.Client.Scheme()); err != nil {
		logger.V(1).Info("Could not set owner reference on DRGroupStatus", "name", dgsName, "error", err)
	}

	if err := e.Client.Create(ctx, dgs); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			logger.V(1).Info("Could not create DRGroupStatus", "name", dgsName, "error", err)
			return noopStepRecorder{}
		}
		// Reuse existing resource on retry/requeue.
		if getErr := e.Client.Get(ctx, client.ObjectKey{Name: dgsName}, dgs); getErr != nil {
			logger.V(1).Info("Could not fetch existing DRGroupStatus", "name", dgsName, "error", getErr)
			return noopStepRecorder{}
		}
		logger.V(1).Info("Reusing existing DRGroupStatus", "name", dgsName)
	}

	// PrepareForCreate zeroes Status, so set InProgress via the status
	// subresource to ensure the phase is persisted.
	dgs.Status.Phase = soteriav1alpha1.DRGroupResultInProgress
	if err := e.Client.Status().Update(ctx, dgs); err != nil {
		logger.V(1).Info("Could not set initial InProgress status on DRGroupStatus", "name", dgsName, "error", err)
	}

	logger.V(1).Info("Created DRGroupStatus", "name", dgsName, "execution", exec.Name)
	return &drgroupStatusRecorder{
		client:    e.Client,
		statusKey: client.ObjectKey{Name: dgsName},
	}
}

// finishDRGroupStatus sets the terminal phase and lastTransitionTime on a DRGroupStatus.
func (e *WaveExecutor) finishDRGroupStatus(
	ctx context.Context, recorder StepRecorder,
	phase soteriav1alpha1.DRGroupResult, completionTime *metav1.Time,
) {
	r, ok := recorder.(*drgroupStatusRecorder)
	if !ok {
		return
	}
	logger := log.FromContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()

	var dgs soteriav1alpha1.DRGroupStatus
	if err := r.client.Get(ctx, r.statusKey, &dgs); err != nil {
		logger.V(1).Info("Could not fetch DRGroupStatus for finalization", "name", r.statusKey.Name, "error", err)
		return
	}
	dgs.Status.Phase = phase
	dgs.Status.LastTransitionTime = completionTime
	if err := r.client.Status().Update(ctx, &dgs); err != nil {
		logger.V(1).Info("Could not finalize DRGroupStatus", "name", r.statusKey.Name, "error", err)
	}
}

// emitGroupEvent emits a GroupFailed event on the DRExecution.
func (e *WaveExecutor) emitGroupEvent(
	exec *soteriav1alpha1.DRExecution, waveIdx int, groupName string, err error,
) {
	if e.Recorder == nil {
		return
	}
	var ge *GroupError
	if errors.As(err, &ge) {
		e.Recorder.Eventf(exec, nil, corev1.EventTypeWarning, "GroupFailed", "WaveExecution",
			"DRGroup %s in wave %d failed at step %s for %s: %v",
			groupName, waveIdx, ge.StepName, ge.Target, ge.Err)
	} else {
		e.Recorder.Eventf(exec, nil, corev1.EventTypeWarning, "GroupFailed", "WaveExecution",
			"DRGroup %s in wave %d failed: %v", groupName, waveIdx, err)
	}
}

// emitGroupCompletedEvent emits a GroupCompleted event on the DRExecution.
func (e *WaveExecutor) emitGroupCompletedEvent(
	exec *soteriav1alpha1.DRExecution, waveIdx int, groupName string,
) {
	if e.Recorder == nil {
		return
	}
	e.Recorder.Eventf(exec, nil, corev1.EventTypeNormal, "GroupCompleted", "WaveExecution",
		"DRGroup %s in wave %d completed", groupName, waveIdx)
}

// emitResultEvent emits a final execution result event on the DRExecution.
func (e *WaveExecutor) emitResultEvent(
	exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
	result soteriav1alpha1.ExecutionResult, failedCount, totalCount int, topErr string,
) {
	if e.Recorder == nil {
		return
	}
	switch result {
	case soteriav1alpha1.ExecutionResultSucceeded:
		e.Recorder.Eventf(exec, nil, corev1.EventTypeNormal, "ExecutionSucceeded", "Execution",
			"Execution completed successfully for plan %s", plan.Name)
	case soteriav1alpha1.ExecutionResultPartiallySucceeded:
		e.Recorder.Eventf(exec, nil, corev1.EventTypeWarning, "ExecutionPartiallySucceeded", "Execution",
			"Execution partially succeeded for plan %s: %d of %d groups failed", plan.Name, failedCount, totalCount)
	case soteriav1alpha1.ExecutionResultFailed:
		msg := fmt.Sprintf("Execution failed for plan %s", plan.Name)
		if topErr != "" {
			msg += ": " + topErr
		}
		e.Recorder.Eventf(exec, nil, corev1.EventTypeWarning, "ExecutionFailed", "Execution", msg)
	}
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

// resolveDrivers resolves one StorageProvider per VolumeGroup within the chunk.
// Each VolumeGroup must have homogeneous storage classes (same driver), but
// different VolumeGroups in the same chunk may use different drivers.
// When no VolumeGroups exist it falls back to the chunk-level resolveDriver.
func (e *WaveExecutor) resolveDrivers(
	ctx context.Context, chunk DRGroupChunk,
) (driverMap map[string]drivers.StorageProvider, fallback drivers.StorageProvider, err error) {
	if e.Registry == nil {
		return nil, nil, fmt.Errorf("driver registry not configured")
	}
	if len(chunk.VolumeGroups) == 0 {
		drv, err := e.resolveDriver(ctx, chunk)
		return nil, drv, err
	}

	driverMap = make(map[string]drivers.StorageProvider, len(chunk.VolumeGroups))
	for _, vg := range chunk.VolumeGroups {
		drv, err := e.resolveVGDriver(ctx, vg)
		if err != nil {
			return nil, nil, fmt.Errorf("volume group %s: %w", vg.Name, err)
		}
		driverMap[vg.Name] = drv
		if fallback == nil {
			fallback = drv
		}
	}
	return driverMap, fallback, nil
}

// resolveVGDriver resolves the StorageProvider for a single VolumeGroup.
func (e *WaveExecutor) resolveVGDriver(
	ctx context.Context, vg soteriav1alpha1.VolumeGroupInfo,
) (drivers.StorageProvider, error) {
	logger := log.FromContext(ctx)
	sc, err := e.resolveVGStorageClass(ctx, vg)
	if err != nil {
		return nil, err
	}
	if sc == "" {
		logger.V(1).Info("No PVC storage class found for volume group, using fallback driver", "vg", vg.Name)
		return e.Registry.GetDriver("")
	}
	return e.Registry.GetDriverForPVC(ctx, sc, e.SCLister)
}

// resolveVGStorageClass extracts the common storage class for a single
// VolumeGroup by reading the PVCs attached to its VMs. Returns an error if
// PVCs within the VolumeGroup use different storage classes.
func (e *WaveExecutor) resolveVGStorageClass(
	ctx context.Context, vg soteriav1alpha1.VolumeGroupInfo,
) (string, error) {
	var commonSC string

	for _, vmName := range vg.VMNames {
		var vm kubevirtv1.VirtualMachine
		if err := e.Client.Get(ctx, types.NamespacedName{
			Name: vmName, Namespace: vg.Namespace,
		}, &vm); err != nil {
			return "", fmt.Errorf("fetching VM %s/%s: %w", vg.Namespace, vmName, err)
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
					vg.Namespace, claimName)
			}
			pvc, err := e.CoreClient.PersistentVolumeClaims(
				vg.Namespace,
			).Get(ctx, claimName, metav1.GetOptions{})
			if err != nil {
				return "", fmt.Errorf(
					"fetching PVC %s/%s for VM %s: %w",
					vg.Namespace, claimName, vmName, err)
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
					"heterogeneous storage classes in volume group %q: "+
						"found %q on PVC %s/%s but expected %q",
					vg.Name, scName, vg.Namespace,
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
// advances the DRPlan phase via CompleteTransition. Emits a final result event.
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

	// Emit result event.
	failed, total := e.countGroups(exec)
	e.emitResultEvent(exec, plan, result, failed, total, message)

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

// countGroups tallies failed and total groups across all waves.
func (e *WaveExecutor) countGroups(exec *soteriav1alpha1.DRExecution) (failed, total int) {
	for _, wave := range exec.Status.Waves {
		for _, g := range wave.Groups {
			total++
			if g.Result == soteriav1alpha1.DRGroupResultFailed {
				failed++
			}
		}
	}
	return
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

// BuildExecutionGroups runs the discover → group → chunk pipeline and returns
// all ExecutionGroups across all waves. Used by the controller to pass groups
// to PreExecute before the wave executor runs.
func (e *WaveExecutor) BuildExecutionGroups(
	ctx context.Context, plan *soteriav1alpha1.DRPlan,
) ([]ExecutionGroup, error) {
	vms, err := e.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
	if err != nil {
		return nil, fmt.Errorf("VM discovery failed: %w", err)
	}
	if len(vms) == 0 {
		return nil, nil
	}

	discovery := GroupByWave(vms, plan.Spec.WaveLabel)
	consistency, err := ResolveVolumeGroups(ctx, vms, plan.Spec.WaveLabel, e.NamespaceLookup)
	if err != nil {
		return nil, fmt.Errorf("volume group resolution failed: %w", err)
	}

	chunkInput := buildChunkInput(discovery, consistency, vms, plan.Spec.WaveLabel)
	chunkResult := ChunkWaves(chunkInput, plan.Spec.MaxConcurrentFailovers)

	var groups []ExecutionGroup
	for waveIdx, wc := range chunkResult.Waves {
		for _, chunk := range wc.Chunks {
			driverMap, fallbackDriver, err := e.resolveDrivers(ctx, chunk)
			if err != nil {
				return nil, fmt.Errorf("resolving drivers for chunk %s: %w", chunk.Name, err)
			}
			groups = append(groups, ExecutionGroup{
				Chunk:       chunk,
				Driver:      fallbackDriver,
				Drivers:     driverMap,
				WaveIndex:   waveIdx,
				PVCResolver: e.PVCResolver,
			})
		}
	}
	return groups, nil
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
