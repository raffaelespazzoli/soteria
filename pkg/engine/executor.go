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
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

// ScyllaRetry is a retry backoff tuned for ScyllaDB's eventual consistency
// window. The default client-go retry (10ms, 5 steps, factor 1.0) is too
// aggressive — reads immediately after a write may return stale data.
// This backoff uses 200ms base with exponential growth and jitter to ride
// out the replication lag.
var ScyllaRetry = wait.Backoff{
	Steps:    8,
	Duration: 200 * time.Millisecond,
	Factor:   2.0,
	Jitter:   0.3,
}

// RetryGroupsAnnotation is the annotation key that operators use to trigger
// retry of failed DRGroups. The value is a comma-separated list of group names
// or "all-failed" to retry every group with Result == Failed.
const RetryGroupsAnnotation = "soteria.io/retry-groups"

// RetryAllFailed is the sentinel annotation value that means "retry all
// DRGroups that have Result == Failed".
const RetryAllFailed = "all-failed"

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

// VMHealthValidator checks whether a VM is in a known, healthy state before
// allowing a retry operation. Retry is rejected when any VM in the group fails
// validation — this prevents retry from an unpredictable starting point (FR15).
type VMHealthValidator interface {
	ValidateVMHealth(ctx context.Context, vmName, namespace string) error
}

// WaveExecutor orchestrates sequential wave execution with concurrent DRGroups.
// Status updates are serialized via statusMu to prevent races when multiple
// goroutines update the same DRExecution concurrently.
type WaveExecutor struct {
	Client            client.Client
	CoreClient        corev1client.CoreV1Interface
	VMDiscoverer      VMDiscoverer
	NamespaceLookup   NamespaceLookup
	Registry          *drivers.Registry
	SCLister          drivers.StorageClassLister
	Recorder          events.EventRecorder
	PVCResolver       PVCResolver
	VMHealthValidator VMHealthValidator
	Checkpointer      Checkpointer

	statusMu sync.Mutex
}

// ExecuteInput holds the inputs for a single executor invocation.
type ExecuteInput struct {
	Execution *soteriav1alpha1.DRExecution
	Plan      *soteriav1alpha1.DRPlan
	Handler   DRGroupHandler
}

// Execute runs the full execution pipeline: discover → group → chunk → execute
// waves → compute result. This method runs all waves synchronously and is used
// by the existing retry/resume paths. The reconciler's reconcileWaveExecution
// drives wave-by-wave execution with VM readiness gates.
func (e *WaveExecutor) Execute(ctx context.Context, input ExecuteInput) error {
	logger := log.FromContext(ctx)
	exec := input.Execution
	plan := input.Plan

	logger.Info("Starting wave execution", "plan", plan.Name, "execution", exec.Name)

	vms, err := e.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
	if err != nil {
		logger.Error(err, "VM discovery failed during execution")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultFailed,
			fmt.Sprintf("VM discovery failed: %v", err))
	}
	if len(vms) == 0 {
		logger.Info("No VMs discovered, completing with Succeeded")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultSucceeded, "")
	}

	discovery := GroupByWave(vms)
	consistency, err := ResolveVolumeGroups(ctx, vms, e.NamespaceLookup)
	if err != nil {
		logger.Error(err, "Volume group resolution failed during execution")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultFailed,
			fmt.Sprintf("Volume group resolution failed: %v", err))
	}

	chunkInput := buildChunkInput(discovery, consistency, vms)
	chunkResult := ChunkWaves(chunkInput, plan.Spec.MaxConcurrentFailovers)

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

	for i, wc := range chunkResult.Waves {
		if ctx.Err() != nil {
			logger.Info("Context cancelled, stopping execution")
			return e.finishExecution(ctx, exec, plan, e.computeResult(exec), "Context cancelled")
		}
		e.executeWave(ctx, i, wc.Chunks, input.Handler, exec)
	}

	result := e.computeResult(exec)
	logger.Info("Wave execution completed", "result", result)
	return e.finishExecution(ctx, exec, plan, result, "")
}

// InitializeWaves discovers VMs, groups them by wave, chunks them, and writes
// the initial Pending wave status entries to DRExecution. This separates the
// discovery/chunking pipeline from execution so the reconciler can drive waves
// one at a time with VM readiness gates.
func (e *WaveExecutor) InitializeWaves(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) error {
	logger := log.FromContext(ctx)

	vms, err := e.VMDiscoverer.DiscoverVMs(ctx, plan.Name)
	if err != nil {
		logger.Error(err, "VM discovery failed during execution")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultFailed,
			fmt.Sprintf("VM discovery failed: %v", err))
	}

	if len(vms) == 0 {
		logger.Info("No VMs discovered, completing with Succeeded")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultSucceeded, "")
	}

	discovery := GroupByWave(vms)
	consistency, err := ResolveVolumeGroups(ctx, vms, e.NamespaceLookup)
	if err != nil {
		logger.Error(err, "Volume group resolution failed during execution")
		return e.finishExecution(ctx, exec, plan, soteriav1alpha1.ExecutionResultFailed,
			fmt.Sprintf("Volume group resolution failed: %v", err))
	}

	chunkInput := buildChunkInput(discovery, consistency, vms)
	chunkResult := ChunkWaves(chunkInput, plan.Spec.MaxConcurrentFailovers)

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
	return e.persistStatus(ctx, exec)
}

// ExecuteWaveHandler runs handler operations for a single wave: creates
// DRGroupStatus resources, resolves drivers, and executes groups concurrently.
// Unlike executeWave, this is exported for the reconciler to drive wave-by-wave
// execution with VM readiness gates.
func (e *WaveExecutor) ExecuteWaveHandler(
	ctx context.Context, waveIdx int, handler DRGroupHandler,
	exec *soteriav1alpha1.DRExecution, plan *soteriav1alpha1.DRPlan,
) {
	wave := exec.Status.Waves[waveIdx]
	var chunks []DRGroupChunk
	for _, group := range wave.Groups {
		if group.Result == soteriav1alpha1.DRGroupResultPending {
			chunks = append(chunks, e.reconstructChunkFromStatus(group, plan, waveIdx))
		}
	}
	if len(chunks) > 0 {
		e.executeWave(ctx, waveIdx, chunks, handler, exec)
	}
}

// ExecuteFromWave starts execution from a specific wave index, skipping groups
// whose names appear in skipGroups. For the resume wave, only pending and
// in-flight (retried) groups are executed. Subsequent waves execute normally.
// This method is the core execution loop shared by both new executions and
// resume-after-restart.
func (e *WaveExecutor) ExecuteFromWave(
	ctx context.Context, input ExecuteInput, startWaveIndex int, skipGroups map[string]bool,
) error {
	logger := log.FromContext(ctx)
	exec := input.Execution
	plan := input.Plan

	for i := startWaveIndex; i < len(exec.Status.Waves); i++ {
		if ctx.Err() != nil {
			logger.Info("Context cancelled, stopping execution")
			return e.finishExecution(ctx, exec, plan, e.computeResult(exec), "Context cancelled")
		}

		wave := exec.Status.Waves[i]

		// Build the chunk list for this wave, skipping completed/failed groups
		// when resuming the first wave.
		var chunks []DRGroupChunk
		for _, group := range wave.Groups {
			if i == startWaveIndex && skipGroups != nil && skipGroups[group.Name] {
				continue
			}
			chunks = append(chunks, e.reconstructChunkFromStatus(group, plan, i))
		}

		if len(chunks) > 0 {
			e.executeWave(ctx, i, chunks, input.Handler, exec)
		}
	}

	result := e.computeResult(exec)
	logger.Info("Wave execution completed", "result", result)
	return e.finishExecution(ctx, exec, plan, result, "")
}

// reconstructChunkFromStatus builds a DRGroupChunk from a group's execution
// status and plan data for resume execution.
func (e *WaveExecutor) reconstructChunkFromStatus(
	group soteriav1alpha1.DRGroupExecutionStatus,
	plan *soteriav1alpha1.DRPlan,
	waveIdx int,
) DRGroupChunk {
	vms := make([]VMReference, len(group.VMNames))
	vmNameSet := make(map[string]bool, len(group.VMNames))
	for i, name := range group.VMNames {
		ns := ""
		if waveIdx < len(plan.Status.Waves) {
			for _, dvm := range plan.Status.Waves[waveIdx].VMs {
				if dvm.Name == name {
					ns = dvm.Namespace
					break
				}
			}
		}
		vms[i] = VMReference{Name: name, Namespace: ns}
		vmNameSet[name] = true
	}

	var volumeGroups []soteriav1alpha1.VolumeGroupInfo
	if waveIdx < len(plan.Status.Waves) {
		for _, vg := range plan.Status.Waves[waveIdx].Groups {
			for _, vmName := range vg.VMNames {
				if vmNameSet[vmName] {
					volumeGroups = append(volumeGroups, vg)
					break
				}
			}
		}
	}

	return DRGroupChunk{
		Name:         group.Name,
		VMs:          vms,
		VolumeGroups: volumeGroups,
	}
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
	e.writeCheckpoint(ctx, exec, fmt.Sprintf("wave-%d", waveIdx))
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
		e.writeCheckpoint(ctx, exec, chunk.Name)
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
		e.writeCheckpoint(ctx, exec, chunk.Name)
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
	if !e.writeCheckpoint(ctx, exec, chunk.Name) {
		logger.Info("Marking group Failed due to checkpoint exhaustion",
			"wave", waveIdx, "group", chunk.Name)
		e.setGroupStatus(ctx, exec, waveIdx, groupIdx, soteriav1alpha1.DRGroupExecutionStatus{
			Name:           chunk.Name,
			Result:         soteriav1alpha1.DRGroupResultFailed,
			VMNames:        vmNames,
			Steps:          steps,
			Error:          "checkpoint write failed after retries",
			StartTime:      &startTime,
			CompletionTime: &completionTime,
		})
	}
}

// writeCheckpoint persists the current DRExecution status as a checkpoint.
// Returns true on success (or nil Checkpointer), false on failure. On
// ErrCheckpointFailed the success-path caller must mark the group Failed
// per AC3; failure-path callers can ignore the return since the group is
// already Failed. The snapshot is taken under statusMu to prevent concurrent
// group completions from overwriting each other's checkpoint data.
func (e *WaveExecutor) writeCheckpoint(ctx context.Context, exec *soteriav1alpha1.DRExecution, groupName string) bool {
	if e.Checkpointer == nil {
		return true
	}
	e.statusMu.Lock()
	snapshot := exec.DeepCopy()
	e.statusMu.Unlock()
	if err := e.Checkpointer.WriteCheckpoint(ctx, snapshot); err != nil {
		logger := log.FromContext(ctx)
		if errors.Is(err, ErrCheckpointFailed) {
			logger.Info("Checkpoint write exhausted retries, continuing fail-forward",
				"execution", exec.Name, "group", groupName, "error", err)
		} else {
			logger.Error(err, "Checkpoint write failed",
				"execution", exec.Name, "group", groupName)
		}
		return false
	}
	return true
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
		drv, err := e.ResolveVGDriver(ctx, vg)
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

// ResolveVGDriver resolves the StorageProvider for a single VolumeGroup.
func (e *WaveExecutor) ResolveVGDriver(
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
// execution result. Groups still in Pending, InProgress, or WaitingForVMReady
// (e.g. after context cancellation) are treated as incomplete — if any exist
// alongside completed groups, the result is PartiallySucceeded; if no group
// completed, Failed.
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

// FinishExecution is an exported wrapper around finishExecution for the
// reconciler to call when driving wave-by-wave execution.
func (e *WaveExecutor) FinishExecution(
	ctx context.Context, exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan, result soteriav1alpha1.ExecutionResult,
	message string,
) error {
	return e.finishExecution(ctx, exec, plan, result, message)
}

// ComputeResult is an exported wrapper around computeResult for the reconciler.
func (e *WaveExecutor) ComputeResult(exec *soteriav1alpha1.DRExecution) soteriav1alpha1.ExecutionResult {
	return e.computeResult(exec)
}

// PersistStatus is an exported wrapper around persistStatus for the reconciler.
func (e *WaveExecutor) PersistStatus(ctx context.Context, exec *soteriav1alpha1.DRExecution) error {
	return e.persistStatus(ctx, exec)
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

	// Advance DRPlan phase and clear ActiveExecution on success/partial success.
	// On failure, clear ActiveExecution only — phase stays at current rest state.
	if result == soteriav1alpha1.ExecutionResultSucceeded ||
		result == soteriav1alpha1.ExecutionResultPartiallySucceeded {
		previousPhase := plan.Status.Phase
		newPhase, err := RestStateAfterCompletion(plan.Status.Phase, exec.Spec.Mode)
		if err != nil {
			logger.Error(err, "Could not complete phase transition", "currentPhase", plan.Status.Phase)
		} else {
			planPatch := client.MergeFrom(plan.DeepCopy())
			plan.Status.Phase = newPhase
			plan.Status.ActiveExecution = ""
			plan.Status.ActiveExecutionMode = ""
			plan.Status.ActiveSite = ActiveSiteForPhase(newPhase, plan.Spec.PrimarySite, plan.Spec.SecondarySite)
			if err := e.Client.Status().Patch(ctx, plan, planPatch); err != nil {
				logger.Error(err, "Failed to advance DRPlan phase", "plan", plan.Name, "targetPhase", newPhase)
				return fmt.Errorf("advancing DRPlan phase: %w", err)
			}
			logger.Info("Advanced DRPlan phase", "plan", plan.Name, "from", previousPhase, "to", newPhase,
				"activeSite", plan.Status.ActiveSite)
		}
	}

	// Always clear ActiveExecution when it wasn't already cleared above.
	if plan.Status.ActiveExecution != "" {
		planPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.ActiveExecution = ""
		plan.Status.ActiveExecutionMode = ""
		if err := e.Client.Status().Patch(ctx, plan, planPatch); err != nil {
			logger.Error(err, "Failed to clear ActiveExecution on DRPlan", "plan", plan.Name)
			return fmt.Errorf("clearing ActiveExecution: %w", err)
		}
		logger.Info("Cleared ActiveExecution on DRPlan", "plan", plan.Name)
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
// ensure the latest resourceVersion and retries on conflict (the informer
// cache may lag behind the API server after a recent Patch).
//
// Conditions set by other reconcilers (e.g. Step0Complete from the source-site
// controller) are preserved: any condition present in the freshly-fetched
// object but absent from the in-memory copy is carried forward.
func (e *WaveExecutor) persistStatus(ctx context.Context, exec *soteriav1alpha1.DRExecution) error {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()

	statusCopy := exec.Status.DeepCopy()
	return retry.RetryOnConflict(ScyllaRetry, func() error {
		if err := e.Client.Get(ctx, client.ObjectKeyFromObject(exec), exec); err != nil {
			return fmt.Errorf("re-fetching DRExecution before status update: %w", err)
		}
		fetchedConditions := exec.Status.Conditions
		exec.Status = *statusCopy
		mergeConditions(&exec.Status.Conditions, fetchedConditions)
		return e.Client.Status().Update(ctx, exec)
	})
}

// mergeConditions copies conditions present in fetched but missing from dst
// into dst so that conditions set by other controllers are not lost when
// the WaveExecutor writes wave/group progress.
func mergeConditions(dst *[]metav1.Condition, fetched []metav1.Condition) {
	have := make(map[string]struct{}, len(*dst))
	for _, c := range *dst {
		have[c.Type] = struct{}{}
	}
	for _, c := range fetched {
		if _, ok := have[c.Type]; !ok {
			*dst = append(*dst, c)
		}
	}
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

	discovery := GroupByWave(vms)
	consistency, err := ResolveVolumeGroups(ctx, vms, e.NamespaceLookup)
	if err != nil {
		return nil, fmt.Errorf("volume group resolution failed: %w", err)
	}

	chunkInput := buildChunkInput(discovery, consistency, vms)
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

// --- Retry support ---

// RetryTarget identifies a single DRGroup to be retried.
type RetryTarget struct {
	WaveIndex  int
	GroupIndex int
	GroupName  string
}

// RetryInput holds the inputs for a retry invocation.
type RetryInput struct {
	Execution    *soteriav1alpha1.DRExecution
	Plan         *soteriav1alpha1.DRPlan
	Handler      DRGroupHandler
	RetryTargets []RetryTarget
}

// parseRetryAnnotation splits the annotation value into deduplicated group names.
// Returns nil for the "all-failed" sentinel — the caller resolves those.
func parseRetryAnnotation(value string) []string {
	if value == RetryAllFailed {
		return nil
	}
	groups := strings.Split(value, ",")
	seen := make(map[string]struct{}, len(groups))
	result := make([]string, 0, len(groups))
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		result = append(result, g)
	}
	return result
}

// ResolveRetryGroups parses the annotation value, validates each group exists
// in the execution status and has Result == Failed, returns structured retry
// targets sorted by wave index. Already-Completed groups are silently skipped.
func ResolveRetryGroups(
	exec *soteriav1alpha1.DRExecution, annotation string,
) ([]RetryTarget, error) {
	requestedNames := parseRetryAnnotation(annotation)

	var targets []RetryTarget

	if requestedNames == nil {
		// all-failed: scan all waves/groups for Result == Failed.
		for wi, wave := range exec.Status.Waves {
			for gi, group := range wave.Groups {
				if group.Result == soteriav1alpha1.DRGroupResultFailed {
					targets = append(targets, RetryTarget{
						WaveIndex:  wi,
						GroupIndex: gi,
						GroupName:  group.Name,
					})
				}
			}
		}
		return targets, nil
	}

	// Build index of group names → location.
	type groupLocation struct {
		waveIdx  int
		groupIdx int
		result   soteriav1alpha1.DRGroupResult
	}
	index := make(map[string]groupLocation)
	for wi, wave := range exec.Status.Waves {
		for gi, group := range wave.Groups {
			index[group.Name] = groupLocation{
				waveIdx:  wi,
				groupIdx: gi,
				result:   group.Result,
			}
		}
	}

	for _, name := range requestedNames {
		loc, found := index[name]
		if !found {
			return nil, fmt.Errorf("retry group %q not found in execution", name)
		}
		if loc.result == soteriav1alpha1.DRGroupResultCompleted {
			continue
		}
		if loc.result != soteriav1alpha1.DRGroupResultFailed {
			return nil, fmt.Errorf("retry group %q has result %q, expected Failed", name, loc.result)
		}
		targets = append(targets, RetryTarget{
			WaveIndex:  loc.waveIdx,
			GroupIndex: loc.groupIdx,
			GroupName:  name,
		})
	}

	sort.Slice(targets, func(i, j int) bool {
		if targets[i].WaveIndex != targets[j].WaveIndex {
			return targets[i].WaveIndex < targets[j].WaveIndex
		}
		return targets[i].GroupIndex < targets[j].GroupIndex
	})

	return targets, nil
}

// ExecuteRetry re-executes failed DRGroups respecting wave ordering. Groups
// from wave N are retried before groups from wave N+1. Within a wave, groups
// are retried concurrently (same fail-forward semantics as initial execution).
// Does NOT call CompleteTransition — the plan phase was already advanced during
// initial execution.
func (e *WaveExecutor) ExecuteRetry(ctx context.Context, input RetryInput) error {
	logger := log.FromContext(ctx)
	exec := input.Execution

	logger.Info("Starting retry execution",
		"execution", exec.Name, "retryGroups", len(input.RetryTargets))

	// Group targets by wave index.
	waveGroups := make(map[int][]RetryTarget)
	for _, t := range input.RetryTargets {
		waveGroups[t.WaveIndex] = append(waveGroups[t.WaveIndex], t)
	}

	// Sort wave indices for sequential execution.
	waveIndices := make([]int, 0, len(waveGroups))
	for wi := range waveGroups {
		waveIndices = append(waveIndices, wi)
	}
	sort.Ints(waveIndices)

	// Execute waves sequentially.
	for _, wi := range waveIndices {
		if ctx.Err() != nil {
			logger.Info("Context cancelled during retry, stopping")
			break
		}
		e.executeRetryWave(ctx, wi, waveGroups[wi], input.Handler, exec, input.Plan)
	}

	// Recompute overall result.
	result := e.computeResult(exec)
	logger.Info("Retry execution completed", "result", result)

	exec.Status.Result = result
	now := metav1.Now()
	exec.Status.CompletionTime = &now
	if err := e.persistStatus(ctx, exec); err != nil {
		return fmt.Errorf("writing retry execution status: %w", err)
	}

	return nil
}

// executeRetryWave runs retry groups within a wave concurrently.
func (e *WaveExecutor) executeRetryWave(
	ctx context.Context, waveIdx int, targets []RetryTarget,
	handler DRGroupHandler, exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) {
	logger := log.FromContext(ctx)
	logger.Info("Starting retry wave", "wave", waveIdx, "groups", len(targets))

	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(t RetryTarget) {
			defer wg.Done()
			e.executeRetryGroup(ctx, t, handler, exec, plan)
		}(target)
	}
	wg.Wait()

	logger.Info("Retry wave completed", "wave", waveIdx)
}

// executeRetryGroup re-executes a single failed DRGroup: increments RetryCount,
// resets status to InProgress, clears DRGroupStatus steps, calls the handler,
// and records the result.
func (e *WaveExecutor) executeRetryGroup(
	ctx context.Context, target RetryTarget,
	handler DRGroupHandler, exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) {
	logger := log.FromContext(ctx)

	e.statusMu.Lock()
	groupStatus := &exec.Status.Waves[target.WaveIndex].Groups[target.GroupIndex]
	groupStatus.RetryCount++
	retryCount := groupStatus.RetryCount
	groupStatus.Result = soteriav1alpha1.DRGroupResultInProgress
	groupStatus.Error = ""
	startTime := metav1.Now()
	groupStatus.StartTime = &startTime
	groupStatus.CompletionTime = nil
	groupStatus.Steps = nil
	vmNames := make([]string, len(groupStatus.VMNames))
	copy(vmNames, groupStatus.VMNames)
	e.statusMu.Unlock()

	if err := e.persistStatus(ctx, exec); err != nil {
		logger.Error(err, "Failed to persist retry InProgress status", "group", target.GroupName)
	}

	// Reset DRGroupStatus resource for the retry.
	e.resetDRGroupStatus(ctx, exec, target)

	// Reconstruct the chunk from execution status and plan.
	chunk := e.reconstructChunk(target, exec, plan)

	// Resolve drivers for the retry group.
	driverMap, fallbackDriver, err := e.resolveDrivers(ctx, chunk)
	if err != nil {
		logger.Error(err, "Driver resolution failed during retry", "group", target.GroupName)
		completionTime := metav1.Now()
		e.setGroupStatus(ctx, exec, target.WaveIndex, target.GroupIndex, soteriav1alpha1.DRGroupExecutionStatus{
			Name:           target.GroupName,
			Result:         soteriav1alpha1.DRGroupResultFailed,
			VMNames:        vmNames,
			RetryCount:     retryCount,
			Error:          fmt.Sprintf("step DriverResolution failed for %s: %v", target.GroupName, err),
			StartTime:      &startTime,
			CompletionTime: &completionTime,
		})
		return
	}

	recorder := e.getDRGroupStatusRecorder(ctx, exec, target)

	execGroup := ExecutionGroup{
		Chunk:        chunk,
		Driver:       fallbackDriver,
		Drivers:      driverMap,
		WaveIndex:    target.WaveIndex,
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
			logger.Error(err, "DRGroup retry failed", "group", target.GroupName,
				"step", ge.StepName, "target", ge.Target, "retryCount", retryCount)
		} else {
			logger.Error(err, "DRGroup retry failed", "group", target.GroupName, "retryCount", retryCount)
		}
		e.finishDRGroupStatus(ctx, recorder, soteriav1alpha1.DRGroupResultFailed, &completionTime)
		e.setGroupStatus(ctx, exec, target.WaveIndex, target.GroupIndex, soteriav1alpha1.DRGroupExecutionStatus{
			Name:           target.GroupName,
			Result:         soteriav1alpha1.DRGroupResultFailed,
			VMNames:        vmNames,
			RetryCount:     retryCount,
			Error:          errMsg,
			Steps:          steps,
			StartTime:      &startTime,
			CompletionTime: &completionTime,
		})
		return
	}

	e.finishDRGroupStatus(ctx, recorder, soteriav1alpha1.DRGroupResultCompleted, &completionTime)
	logger.Info("DRGroup retry completed", "group", target.GroupName,
		"result", "Completed", "retryCount", retryCount)
	e.setGroupStatus(ctx, exec, target.WaveIndex, target.GroupIndex, soteriav1alpha1.DRGroupExecutionStatus{
		Name:           target.GroupName,
		Result:         soteriav1alpha1.DRGroupResultCompleted,
		VMNames:        vmNames,
		RetryCount:     retryCount,
		Steps:          steps,
		StartTime:      &startTime,
		CompletionTime: &completionTime,
	})
}

// resetDRGroupStatus resets an existing DRGroupStatus resource for retry:
// clears steps, sets phase to InProgress.
func (e *WaveExecutor) resetDRGroupStatus(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, target RetryTarget,
) {
	logger := log.FromContext(ctx)
	dgsName := fmt.Sprintf("%s-%s", exec.Name, target.GroupName)

	var dgs soteriav1alpha1.DRGroupStatus
	if err := e.Client.Get(ctx, client.ObjectKey{Name: dgsName}, &dgs); err != nil {
		logger.V(1).Info("Could not fetch DRGroupStatus for retry reset", "name", dgsName, "error", err)
		return
	}

	dgs.Status.Phase = soteriav1alpha1.DRGroupResultInProgress
	dgs.Status.Steps = nil
	now := metav1.Now()
	dgs.Status.LastTransitionTime = &now
	if err := e.Client.Status().Update(ctx, &dgs); err != nil {
		logger.V(1).Info("Could not reset DRGroupStatus for retry", "name", dgsName, "error", err)
	}
}

// getDRGroupStatusRecorder returns a StepRecorder for an existing DRGroupStatus.
func (e *WaveExecutor) getDRGroupStatusRecorder(
	ctx context.Context, exec *soteriav1alpha1.DRExecution, target RetryTarget,
) StepRecorder {
	logger := log.FromContext(ctx)
	dgsName := fmt.Sprintf("%s-%s", exec.Name, target.GroupName)

	var dgs soteriav1alpha1.DRGroupStatus
	if err := e.Client.Get(ctx, client.ObjectKey{Name: dgsName}, &dgs); err != nil {
		logger.V(1).Info("Could not fetch DRGroupStatus for retry recorder", "name", dgsName, "error", err)
		return noopStepRecorder{}
	}

	return &drgroupStatusRecorder{
		client:    e.Client,
		statusKey: client.ObjectKey{Name: dgsName},
	}
}

// reconstructChunk builds a DRGroupChunk from execution status and plan data
// for retry. Uses VMNames from the group status and VolumeGroups from the
// plan's wave info.
func (e *WaveExecutor) reconstructChunk(
	target RetryTarget, exec *soteriav1alpha1.DRExecution,
	plan *soteriav1alpha1.DRPlan,
) DRGroupChunk {
	groupStatus := exec.Status.Waves[target.WaveIndex].Groups[target.GroupIndex]

	// Build VM references from the group's VM names.
	vms := make([]VMReference, len(groupStatus.VMNames))
	vmNameSet := make(map[string]bool, len(groupStatus.VMNames))
	for i, name := range groupStatus.VMNames {
		ns := ""
		if target.WaveIndex < len(plan.Status.Waves) {
			for _, dvm := range plan.Status.Waves[target.WaveIndex].VMs {
				if dvm.Name == name {
					ns = dvm.Namespace
					break
				}
			}
		}
		vms[i] = VMReference{Name: name, Namespace: ns}
		vmNameSet[name] = true
	}

	// Find matching VolumeGroups from the plan's wave info.
	var volumeGroups []soteriav1alpha1.VolumeGroupInfo
	if target.WaveIndex < len(plan.Status.Waves) {
		for _, vg := range plan.Status.Waves[target.WaveIndex].Groups {
			for _, vmName := range vg.VMNames {
				if vmNameSet[vmName] {
					volumeGroups = append(volumeGroups, vg)
					break
				}
			}
		}
	}

	return DRGroupChunk{
		Name:         target.GroupName,
		VMs:          vms,
		VolumeGroups: volumeGroups,
	}
}

// buildChunkInput constructs the ChunkInput by matching VolumeGroups to waves.
func buildChunkInput(
	discovery DiscoveryResult,
	consistency *ConsistencyResult,
	vms []VMReference,
) ChunkInput {
	vmWave := make(map[string]string, len(vms))
	for _, vm := range vms {
		key := vm.Namespace + "/" + vm.Name
		vmWave[key] = vm.Labels[soteriav1alpha1.WaveLabel]
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
