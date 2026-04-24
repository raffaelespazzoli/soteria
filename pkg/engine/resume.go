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
// resume.go implements execution state reconstruction for crash recovery. On
// startup, controller-runtime syncs informer caches and queues a reconcile for
// every existing DRExecution. The reconciler calls ResumeAnalyzer.AnalyzeExecution
// to determine whether an execution needs resume and where to pick up.
//
// The algorithm walks DRExecution.Status.Waves[] to find:
//   - The first wave with any non-terminal group (InProgress or Pending)
//   - All groups by result within that wave (Completed, Failed, InProgress, Pending)
//   - Whether all prior waves are complete
//
// Groups with Result == InProgress at analysis time were in-flight at crash time
// and need retry. All driver operations are idempotent, making this safe.
// Groups with no result (Pending) haven't started yet. A fully completed
// execution (terminal Result) returns IsComplete: true — no resume needed.

package engine

import (
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// ResumePoint describes where to resume a DRExecution after a restart.
// It identifies the wave index and the groups that need attention.
type ResumePoint struct {
	// WaveIndex is the 0-based index of the wave to resume from.
	WaveIndex int
	// CompletedGroups lists group names that completed successfully.
	CompletedGroups []string
	// FailedGroups lists group names that failed.
	FailedGroups []string
	// InFlightGroups lists group names that were InProgress at crash time
	// and need to be retried.
	InFlightGroups []string
	// PendingGroups lists group names that haven't started yet.
	PendingGroups []string
	// IsComplete is true when the execution has a terminal result and no
	// resume is needed.
	IsComplete bool
}

// ResumeAnalyzer examines DRExecution status and determines the resume point.
type ResumeAnalyzer struct{}

// AnalyzeExecution walks a DRExecution's status to determine whether it needs
// resume and where to pick up. Terminal results (Succeeded, PartiallySucceeded,
// Failed) return IsComplete: true. For in-progress executions, it finds the
// first wave with non-terminal groups and categorizes each group.
func (a *ResumeAnalyzer) AnalyzeExecution(exec *soteriav1alpha1.DRExecution) ResumePoint {
	// Terminal results — execution is done, no resume needed.
	// PartiallySucceeded is handled by retry (Story 4.6), not resume.
	if exec.Status.Result != "" {
		return ResumePoint{IsComplete: true}
	}

	// No waves in status — execution was dispatched but waves weren't
	// initialized yet. Resume from the beginning.
	if len(exec.Status.Waves) == 0 {
		return ResumePoint{WaveIndex: 0}
	}

	// Walk waves to find the resume point: the first wave that has any
	// non-terminal group (InProgress, Pending, or WaitingForVMReady).
	for i, wave := range exec.Status.Waves {
		var completed, failed, inFlight, pending []string

		for _, group := range wave.Groups {
			switch group.Result {
			case soteriav1alpha1.DRGroupResultCompleted:
				completed = append(completed, group.Name)
			case soteriav1alpha1.DRGroupResultFailed:
				failed = append(failed, group.Name)
			case soteriav1alpha1.DRGroupResultInProgress:
				inFlight = append(inFlight, group.Name)
			case soteriav1alpha1.DRGroupResultWaitingForVMReady:
				// WaitingForVMReady groups are treated as completed for
				// skip purposes — their handler already ran successfully.
				// The reconciler will pick up readiness checking.
				completed = append(completed, group.Name)
			default:
				// Pending or empty result — not started.
				pending = append(pending, group.Name)
			}
		}

		if len(inFlight) > 0 || len(pending) > 0 {
			return ResumePoint{
				WaveIndex:       i,
				CompletedGroups: completed,
				FailedGroups:    failed,
				InFlightGroups:  inFlight,
				PendingGroups:   pending,
			}
		}
	}

	// All waves have only terminal groups (Completed or Failed) but the
	// overall Result wasn't set. The crash happened after the last group
	// completed but before finishExecution. Return the last wave with its
	// groups categorized so the reconciler can skip them and just compute
	// the final result via ExecuteFromWave → finishExecution.
	lastWave := len(exec.Status.Waves) - 1
	wave := exec.Status.Waves[lastWave]
	var completed, failed []string
	for _, group := range wave.Groups {
		switch group.Result {
		case soteriav1alpha1.DRGroupResultCompleted:
			completed = append(completed, group.Name)
		case soteriav1alpha1.DRGroupResultFailed:
			failed = append(failed, group.Name)
		}
	}
	return ResumePoint{
		WaveIndex:       lastWave,
		CompletedGroups: completed,
		FailedGroups:    failed,
	}
}
