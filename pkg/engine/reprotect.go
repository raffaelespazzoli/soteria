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
// reprotect.go implements the ReprotectHandler — the storage-only workflow for
// re-establishing replication after failover (re-protect) or after failback
// (restore). The same handler serves both paths: re-protect from FailedOver
// (→ Reprotecting → DRedSteadyState) and restore from FailedBack
// (→ ReprotectingBack → SteadyState). The handler does not distinguish
// direction — it receives volume groups and establishes replication.
//
// Unlike failover/failback (wave-based with VM stop/start), re-protect and
// restore are storage-only: they change replication roles without moving
// workloads. No waves, no VMManager dependency. All volume groups are
// processed in a single pass.
//
// Workflow:
//
//	Phase 1 — Role setup: for each VG, StopReplication then SetSource.
//	  StopReplication failures are tolerated (old active site may be
//	  unreachable). SetSource failures mark the VG as failed; if ALL VGs
//	  fail SetSource, the execution fails.
//
//	Phase 2 — Health monitoring: poll GetReplicationStatus at configurable
//	  intervals until all VGs report HealthHealthy or the timeout expires.
//	  Timeout → PartiallySucceeded (replication is active, just not fully
//	  synced yet). Context cancellation is respected for leader election loss.

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
	"github.com/soteria-project/soteria/pkg/metrics"
)

const (
	StepReprotectStopReplication  = "StopReplication"
	StepReprotectSetSource        = "SetSource"
	StepReprotectHealthMonitoring = "HealthMonitoring"

	reprotectStatusSucceeded = "Succeeded"
	reprotectStatusFailed    = "Failed"
	reprotectStatusWarning   = "Warning"

	defaultHealthPollInterval = 30 * time.Second
	defaultHealthTimeout      = 24 * time.Hour
)

// ErrReprotectHealthTimeout is returned when health monitoring times out before
// all volume groups report HealthHealthy. The execution is marked
// PartiallySucceeded — role setup succeeded and replication is in progress.
var ErrReprotectHealthTimeout = errors.New("re-protect health monitoring timed out")

// VolumeGroupEntry bundles a VolumeGroupInfo with its resolved driver and
// volume group ID for re-protect execution.
type VolumeGroupEntry struct {
	Info   soteriav1alpha1.VolumeGroupInfo
	Driver drivers.StorageProvider
	VGID   drivers.VolumeGroupID
}

// ReprotectInput holds the inputs for a re-protect execution.
type ReprotectInput struct {
	Execution    *soteriav1alpha1.DRExecution
	Plan         *soteriav1alpha1.DRPlan
	VolumeGroups []VolumeGroupEntry
}

// ReprotectResult summarises the outcome of a re-protect execution.
type ReprotectResult struct {
	SetupSucceeded int
	SetupFailed    int
	HealthyVGs     int
	TotalVGs       int
	TimedOut       bool
	FailedVGs      []string
	Steps          []soteriav1alpha1.StepStatus
}

// Result returns the ExecutionResult corresponding to the re-protect outcome.
// PartiallySucceeded when some VGs failed SetSource or health monitoring
// timed out; Failed only when ALL SetSource calls failed.
func (r *ReprotectResult) Result() soteriav1alpha1.ExecutionResult {
	if r.TotalVGs == 0 {
		return soteriav1alpha1.ExecutionResultSucceeded
	}
	if r.SetupSucceeded == 0 {
		return soteriav1alpha1.ExecutionResultFailed
	}
	if r.TimedOut || r.SetupFailed > 0 {
		return soteriav1alpha1.ExecutionResultPartiallySucceeded
	}
	return soteriav1alpha1.ExecutionResultSucceeded
}

// ReprotectHandler implements the storage-only re-protect/restore workflow.
// It is NOT a DRGroupHandler (not wave-based) — the controller calls Execute
// directly instead of dispatching through the WaveExecutor.
type ReprotectHandler struct {
	Checkpointer       Checkpointer
	HealthPollInterval time.Duration
	HealthTimeout      time.Duration
}

func (h *ReprotectHandler) healthPollInterval() time.Duration {
	if h.HealthPollInterval > 0 {
		return h.HealthPollInterval
	}
	return defaultHealthPollInterval
}

func (h *ReprotectHandler) healthTimeout() time.Duration {
	if h.HealthTimeout > 0 {
		return h.HealthTimeout
	}
	return defaultHealthTimeout
}

// Execute runs the two-phase re-protect workflow: role setup followed by
// health monitoring. Returns a ReprotectResult and nil on normal completion
// (including timeout). Returns a non-nil error only for context cancellation
// or when all VGs fail role setup.
func (h *ReprotectHandler) Execute(ctx context.Context, input ReprotectInput) (*ReprotectResult, error) {
	logger := log.FromContext(ctx)
	setupStart := time.Now()

	logger.Info("Re-protect started",
		"plan", input.Plan.Name, "volumeGroups", len(input.VolumeGroups))

	if len(input.VolumeGroups) == 0 {
		result := &ReprotectResult{TotalVGs: 0}
		logger.Info("Re-protect completed with no volume groups")
		return result, nil
	}

	// Phase 1: Role setup — StopReplication + SetSource per VG.
	var successfulVGs []VolumeGroupEntry
	var failedVGNames []string
	var steps []soteriav1alpha1.StepStatus

	for _, vg := range input.VolumeGroups {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		stopErr := vg.Driver.StopReplication(ctx, vg.VGID)
		now := metav1.Now()
		if stopErr != nil {
			logger.V(1).Info("StopReplication failed for volume group, proceeding",
				"vg", vg.Info.Name, "error", stopErr)
			steps = append(steps, soteriav1alpha1.StepStatus{
				Name:      StepReprotectStopReplication,
				Status:    reprotectStatusWarning,
				Message:   fmt.Sprintf("StopReplication failed for %s (tolerated): %v", vg.Info.Name, stopErr),
				Timestamp: &now,
			})
		} else {
			logger.Info("StopReplication completed", "vg", vg.Info.Name)
			steps = append(steps, soteriav1alpha1.StepStatus{
				Name:      StepReprotectStopReplication,
				Status:    reprotectStatusSucceeded,
				Message:   fmt.Sprintf("Stopped replication for %s", vg.Info.Name),
				Timestamp: &now,
			})
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		setErr := vg.Driver.SetSource(ctx, vg.VGID)
		now = metav1.Now()
		if setErr != nil {
			logger.Info("SetSource failed for volume group",
				"vg", vg.Info.Name, "error", setErr)
			steps = append(steps, soteriav1alpha1.StepStatus{
				Name:      StepReprotectSetSource,
				Status:    reprotectStatusFailed,
				Message:   fmt.Sprintf("SetSource failed for %s: %v", vg.Info.Name, setErr),
				Timestamp: &now,
			})
			failedVGNames = append(failedVGNames, vg.Info.Name)
			continue
		}
		logger.Info("SetSource completed", "vg", vg.Info.Name)
		steps = append(steps, soteriav1alpha1.StepStatus{
			Name:      StepReprotectSetSource,
			Status:    reprotectStatusSucceeded,
			Message:   fmt.Sprintf("Set source for %s", vg.Info.Name),
			Timestamp: &now,
		})
		successfulVGs = append(successfulVGs, vg)

		// Checkpoint after each VG's role setup (AC8).
		h.writeCheckpoint(ctx, input.Execution)
	}

	metrics.ReprotectVGSetupDuration.Observe(time.Since(setupStart).Seconds())

	if len(successfulVGs) == 0 {
		result := &ReprotectResult{
			SetupFailed: len(input.VolumeGroups),
			TotalVGs:    len(input.VolumeGroups),
			FailedVGs:   failedVGNames,
			Steps:       steps,
		}
		return result, fmt.Errorf("all volume groups failed SetSource during re-protect")
	}

	// Phase 2: Health monitoring.
	healthResult, healthErr := h.monitorHealth(ctx, input, successfulVGs)

	now := metav1.Now()
	healthStep := soteriav1alpha1.StepStatus{
		Name:      StepReprotectHealthMonitoring,
		Timestamp: &now,
	}
	if healthErr != nil && errors.Is(healthErr, ErrReprotectHealthTimeout) {
		healthStep.Status = reprotectStatusWarning
		healthStep.Message = fmt.Sprintf("Health monitoring timed out: %d/%d healthy",
			healthResult.HealthyVGs, len(successfulVGs))
	} else if healthErr != nil {
		healthStep.Status = reprotectStatusFailed
		healthStep.Message = fmt.Sprintf("Health monitoring error: %v", healthErr)
	} else {
		healthStep.Status = reprotectStatusSucceeded
		healthStep.Message = fmt.Sprintf("All %d volume groups healthy", len(successfulVGs))
	}
	steps = append(steps, healthStep)

	result := &ReprotectResult{
		SetupSucceeded: len(successfulVGs),
		SetupFailed:    len(failedVGNames),
		TotalVGs:       len(input.VolumeGroups),
		FailedVGs:      failedVGNames,
		Steps:          steps,
	}

	if healthResult != nil {
		result.HealthyVGs = healthResult.HealthyVGs
		result.TimedOut = healthResult.TimedOut
	}

	totalDuration := time.Since(setupStart)
	metrics.ReprotectDuration.Observe(totalDuration.Seconds())

	if healthErr != nil && !errors.Is(healthErr, ErrReprotectHealthTimeout) {
		return result, healthErr
	}

	logger.Info("Re-protect completed",
		"plan", input.Plan.Name,
		"succeeded", result.SetupSucceeded,
		"failed", result.SetupFailed,
		"healthy", result.HealthyVGs,
		"timedOut", result.TimedOut)

	return result, nil
}

// monitorHealth polls GetReplicationStatus for each successful VG at
// configured intervals until all report HealthHealthy or the timeout expires.
// Writes a checkpoint after each poll iteration (AC8).
func (h *ReprotectHandler) monitorHealth(
	ctx context.Context, input ReprotectInput, vgs []VolumeGroupEntry,
) (*ReprotectResult, error) {
	logger := log.FromContext(ctx)
	interval := h.healthPollInterval()
	timeout := h.healthTimeout()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			healthy := h.countHealthy(ctx, vgs)
			logger.Info("Re-protect health monitoring timed out",
				"plan", input.Plan.Name,
				"healthy", healthy, "total", len(vgs),
				"timeout", timeout)
			metrics.ReprotectHealthPollsTotal.Inc()
			h.writeCheckpoint(ctx, input.Execution)
			return &ReprotectResult{
				HealthyVGs: healthy,
				TotalVGs:   len(vgs),
				TimedOut:   true,
			}, ErrReprotectHealthTimeout
		case <-ticker.C:
			metrics.ReprotectHealthPollsTotal.Inc()
			healthy := h.countHealthy(ctx, vgs)
			logger.V(1).Info("Replication health check",
				"healthy", healthy, "total", len(vgs))

			h.updateHealthConditions(input, healthy, len(vgs))
			h.writeCheckpoint(ctx, input.Execution)

			if healthy == len(vgs) {
				logger.Info("Re-protect health monitoring complete",
					"plan", input.Plan.Name,
					"healthy", healthy, "total", len(vgs))
				return &ReprotectResult{
					HealthyVGs: healthy,
					TotalVGs:   len(vgs),
				}, nil
			}
		}
	}
}

// countHealthy checks replication health for all VGs and returns the count
// that report HealthHealthy.
func (h *ReprotectHandler) countHealthy(ctx context.Context, vgs []VolumeGroupEntry) int {
	logger := log.FromContext(ctx)
	healthy := 0
	for _, vg := range vgs {
		status, err := vg.Driver.GetReplicationStatus(ctx, vg.VGID)
		if err != nil {
			logger.V(1).Info("Could not check replication health",
				"vg", vg.Info.Name, "error", err)
			continue
		}
		if status.Health == drivers.HealthHealthy {
			healthy++
		}
	}
	return healthy
}

// updateHealthConditions sets the Replicating condition on both DRExecution
// and DRPlan to reflect resync progress (AC5).
func (h *ReprotectHandler) updateHealthConditions(input ReprotectInput, healthy, total int) {
	msg := fmt.Sprintf("%d/%d volume groups healthy", healthy, total)

	setCondition(&input.Execution.Status.Conditions, metav1.Condition{
		Type:    "Replicating",
		Status:  metav1.ConditionTrue,
		Reason:  "SyncInProgress",
		Message: msg,
	})

	setCondition(&input.Plan.Status.Conditions, metav1.Condition{
		Type:    "Replicating",
		Status:  metav1.ConditionTrue,
		Reason:  "SyncInProgress",
		Message: msg,
	})
}

func setCondition(conditions *[]metav1.Condition, c metav1.Condition) {
	for i, existing := range *conditions {
		if existing.Type == c.Type {
			now := metav1.Now()
			c.LastTransitionTime = now
			(*conditions)[i] = c
			return
		}
	}
	now := metav1.Now()
	c.LastTransitionTime = now
	*conditions = append(*conditions, c)
}

// writeCheckpoint persists execution status via the Checkpointer. Errors are
// logged but do not halt re-protect — the execution can resume from the
// checkpoint on restart.
func (h *ReprotectHandler) writeCheckpoint(ctx context.Context, exec *soteriav1alpha1.DRExecution) {
	if h.Checkpointer == nil {
		return
	}
	if err := h.Checkpointer.WriteCheckpoint(ctx, exec); err != nil {
		logger := log.FromContext(ctx)
		logger.V(1).Info("Re-protect checkpoint write failed",
			"execution", exec.Name, "error", err)
	}
}
