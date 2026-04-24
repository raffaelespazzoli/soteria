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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

const testGroup1 = "group-1"
const testGroup2 = "group-2"

func newResumeTestExec(
	waves []soteriav1alpha1.WaveStatus, result soteriav1alpha1.ExecutionResult,
) *soteriav1alpha1.DRExecution {
	now := metav1.Now()
	return &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-resume"},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "test-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Result:    result,
			Waves:     waves,
		},
	}
}

func TestResumeAnalyzer_MidWave_IdentifiesResumePoint(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted},
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultCompleted},
			},
		},
		{
			WaveIndex: 1,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: testGroup2, Result: soteriav1alpha1.DRGroupResultCompleted},
				{Name: "group-3", Result: soteriav1alpha1.DRGroupResultInProgress},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	if rp.IsComplete {
		t.Fatal("expected resume needed, got IsComplete")
	}
	if rp.WaveIndex != 1 {
		t.Errorf("expected WaveIndex=1, got %d", rp.WaveIndex)
	}
	if len(rp.InFlightGroups) != 1 || rp.InFlightGroups[0] != "group-3" {
		t.Errorf("expected 1 in-flight group (group-3), got %v", rp.InFlightGroups)
	}
	if len(rp.CompletedGroups) != 1 || rp.CompletedGroups[0] != testGroup2 {
		t.Errorf("expected 1 completed group (group-2), got %v", rp.CompletedGroups)
	}
}

func TestResumeAnalyzer_BetweenWaves_StartsNextWave(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted},
			},
		},
		{
			WaveIndex: 1,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultPending},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	if rp.IsComplete {
		t.Fatal("expected resume needed")
	}
	if rp.WaveIndex != 1 {
		t.Errorf("expected WaveIndex=1, got %d", rp.WaveIndex)
	}
	if len(rp.PendingGroups) != 1 || rp.PendingGroups[0] != testGroup1 {
		t.Errorf("expected 1 pending group, got %v", rp.PendingGroups)
	}
}

func TestResumeAnalyzer_NoInProgress_NoResume(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted},
			},
		},
	}, soteriav1alpha1.ExecutionResultSucceeded)

	rp := analyzer.AnalyzeExecution(exec)

	if !rp.IsComplete {
		t.Error("expected IsComplete for Succeeded execution")
	}
}

func TestResumeAnalyzer_PartiallySucceeded_NoResume(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted},
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultFailed},
			},
		},
	}, soteriav1alpha1.ExecutionResultPartiallySucceeded)

	rp := analyzer.AnalyzeExecution(exec)

	if !rp.IsComplete {
		t.Error("expected IsComplete for PartiallySucceeded (retry is story 4.6, not resume)")
	}
}

func TestResumeAnalyzer_AllGroupsPending_StartsFromBeginning(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultPending},
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultPending},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	if rp.IsComplete {
		t.Fatal("expected resume needed")
	}
	if rp.WaveIndex != 0 {
		t.Errorf("expected WaveIndex=0, got %d", rp.WaveIndex)
	}
	if len(rp.PendingGroups) != 2 {
		t.Errorf("expected 2 pending groups, got %d", len(rp.PendingGroups))
	}
}

func TestResumeAnalyzer_MultipleInFlightGroups_AllRetried(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultInProgress},
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultInProgress},
				{Name: testGroup2, Result: soteriav1alpha1.DRGroupResultInProgress},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	if rp.IsComplete {
		t.Fatal("expected resume needed")
	}
	if len(rp.InFlightGroups) != 3 {
		t.Errorf("expected 3 in-flight groups, got %d", len(rp.InFlightGroups))
	}
}

func TestResumeAnalyzer_MixedWaveStates(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted},
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultCompleted},
			},
		},
		{
			WaveIndex: 1,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: testGroup2, Result: soteriav1alpha1.DRGroupResultCompleted},
				{Name: "group-3", Result: soteriav1alpha1.DRGroupResultInProgress},
				{Name: "group-4", Result: soteriav1alpha1.DRGroupResultPending},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	if rp.IsComplete {
		t.Fatal("expected resume needed")
	}
	if rp.WaveIndex != 1 {
		t.Errorf("expected WaveIndex=1, got %d", rp.WaveIndex)
	}
	if len(rp.CompletedGroups) != 1 {
		t.Errorf("expected 1 completed group in wave 1, got %d", len(rp.CompletedGroups))
	}
	if len(rp.InFlightGroups) != 1 {
		t.Errorf("expected 1 in-flight group, got %d", len(rp.InFlightGroups))
	}
	if len(rp.PendingGroups) != 1 {
		t.Errorf("expected 1 pending group, got %d", len(rp.PendingGroups))
	}
}

func TestResumeAnalyzer_EmptyWaves_StartsFromBeginning(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-empty"},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &metav1.Time{Time: time.Now()},
		},
	}

	rp := analyzer.AnalyzeExecution(exec)

	if rp.IsComplete {
		t.Fatal("expected resume needed for empty waves")
	}
	if rp.WaveIndex != 0 {
		t.Errorf("expected WaveIndex=0, got %d", rp.WaveIndex)
	}
}

func TestResumeAnalyzer_FailedResult_IsComplete(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec(nil, soteriav1alpha1.ExecutionResultFailed)

	rp := analyzer.AnalyzeExecution(exec)

	if !rp.IsComplete {
		t.Error("expected IsComplete for Failed result")
	}
}

func TestResumeAnalyzer_WaitingForVMReady_TreatedAsCompleted(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady},
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultWaitingForVMReady},
			},
		},
		{
			WaveIndex: 1,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: testGroup2, Result: soteriav1alpha1.DRGroupResultPending},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	if rp.IsComplete {
		t.Fatal("expected resume needed — wave 1 has pending groups")
	}
	// WaitingForVMReady groups should be treated as Completed for resume skip logic.
	if rp.WaveIndex != 1 {
		t.Errorf("expected WaveIndex=1, got %d", rp.WaveIndex)
	}
	if len(rp.PendingGroups) != 1 || rp.PendingGroups[0] != testGroup2 {
		t.Errorf("expected 1 pending group (group-2), got %v", rp.PendingGroups)
	}
}

func TestResumeAnalyzer_WaitingForVMReady_AllWaves_NeedsResume(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultWaitingForVMReady},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	// Single wave with only WaitingForVMReady — it's "completed" for skip
	// logic, and the reconciler will re-check readiness on next reconcile.
	// The resume analyzer should see this as complete (all terminal) but
	// Result is empty, so it should return IsComplete=false to trigger
	// result recomputation.
	if rp.IsComplete {
		t.Fatal("expected IsComplete=false when Result is empty")
	}
}

func TestResumeAnalyzer_AllTerminalNoResult_NeedsResultComputation(t *testing.T) {
	analyzer := &ResumeAnalyzer{}
	exec := newResumeTestExec([]soteriav1alpha1.WaveStatus{
		{
			WaveIndex: 0,
			Groups: []soteriav1alpha1.DRGroupExecutionStatus{
				{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted},
				{Name: testGroup1, Result: soteriav1alpha1.DRGroupResultFailed},
			},
		},
	}, "")

	rp := analyzer.AnalyzeExecution(exec)

	// All groups are terminal but Result is empty — the crash happened
	// between the last group completing and finishExecution setting Result.
	// IsComplete should be false so the resume path recomputes the result.
	if rp.IsComplete {
		t.Error("expected IsComplete=false when Result is empty but all groups are terminal")
	}
	if len(rp.CompletedGroups) != 1 || rp.CompletedGroups[0] != "group-0" {
		t.Errorf("expected 1 completed group (group-0), got %v", rp.CompletedGroups)
	}
	if len(rp.FailedGroups) != 1 || rp.FailedGroups[0] != testGroup1 {
		t.Errorf("expected 1 failed group (group-1), got %v", rp.FailedGroups)
	}
}
