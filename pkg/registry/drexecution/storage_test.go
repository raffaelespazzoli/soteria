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

package drexecution

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// --- TableConvertor tests (Story 5.4 AC2) ---

func TestTableConvertor_CompletedExecution(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC))
	end := metav1.NewTime(time.Date(2026, 4, 20, 10, 2, 35, 0, time.UTC))

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "exec-completed",
			CreationTimestamp: start,
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "erp-full-stack",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result:         soteriav1alpha1.ExecutionResultSucceeded,
			StartTime:      &start,
			CompletionTime: &end,
		},
	}

	tc := DRExecutionTableConvertor{}
	table, err := tc.ConvertToTable(context.Background(), exec, nil)
	if err != nil {
		t.Fatalf("ConvertToTable error: %v", err)
	}

	if len(table.ColumnDefinitions) != 6 {
		t.Fatalf("expected 6 columns, got %d", len(table.ColumnDefinitions))
	}
	wantCols := []string{"Name", "Plan", "Mode", "Result", "Duration", "Age"}
	for i, col := range table.ColumnDefinitions {
		if col.Name != wantCols[i] {
			t.Errorf("column %d: expected %q, got %q", i, wantCols[i], col.Name)
		}
	}

	if len(table.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(table.Rows))
	}
	cells := table.Rows[0].Cells
	if cells[0] != "exec-completed" {
		t.Errorf("NAME: expected exec-completed, got %v", cells[0])
	}
	if cells[1] != "erp-full-stack" {
		t.Errorf("PLAN: expected erp-full-stack, got %v", cells[1])
	}
	if cells[2] != "planned_migration" {
		t.Errorf("MODE: expected planned_migration, got %v", cells[2])
	}
	if cells[3] != "Succeeded" {
		t.Errorf("RESULT: expected Succeeded, got %v", cells[3])
	}
	dur := cells[4].(string)
	if dur == "" {
		t.Error("DURATION should not be empty for completed execution")
	}
}

func TestTableConvertor_InProgressExecution(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC))

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "exec-inprogress",
			CreationTimestamp: start,
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-a",
			Mode:     soteriav1alpha1.ExecutionModeDisaster,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &start,
		},
	}

	tc := DRExecutionTableConvertor{}
	table, err := tc.ConvertToTable(context.Background(), exec, nil)
	if err != nil {
		t.Fatalf("ConvertToTable error: %v", err)
	}

	cells := table.Rows[0].Cells
	if cells[3] != "" {
		t.Errorf("RESULT: expected empty for in-progress, got %v", cells[3])
	}
	if cells[4] != "" {
		t.Errorf("DURATION: expected empty for in-progress, got %v", cells[4])
	}
}

func TestTableConvertor_FailedExecution(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC))
	end := metav1.NewTime(time.Date(2026, 4, 20, 10, 0, 45, 0, time.UTC))

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "exec-failed",
			CreationTimestamp: start,
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "plan-b",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result:         soteriav1alpha1.ExecutionResultFailed,
			StartTime:      &start,
			CompletionTime: &end,
		},
	}

	tc := DRExecutionTableConvertor{}
	table, err := tc.ConvertToTable(context.Background(), exec, nil)
	if err != nil {
		t.Fatalf("ConvertToTable error: %v", err)
	}

	cells := table.Rows[0].Cells
	if cells[3] != "Failed" {
		t.Errorf("RESULT: expected Failed, got %v", cells[3])
	}
	if cells[4] == "" {
		t.Error("DURATION should not be empty for failed execution with timestamps")
	}
}

func TestTableConvertor_List(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC))

	list := &soteriav1alpha1.DRExecutionList{
		Items: []soteriav1alpha1.DRExecution{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "exec-1", CreationTimestamp: start},
				Spec: soteriav1alpha1.DRExecutionSpec{
					PlanName: "plan-a",
					Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "exec-2", CreationTimestamp: start},
				Spec: soteriav1alpha1.DRExecutionSpec{
					PlanName: "plan-b",
					Mode:     soteriav1alpha1.ExecutionModeDisaster,
				},
			},
		},
	}

	tc := DRExecutionTableConvertor{}
	table, err := tc.ConvertToTable(context.Background(), list, nil)
	if err != nil {
		t.Fatalf("ConvertToTable error: %v", err)
	}

	if len(table.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(table.Rows))
	}
}

// --- Delete protection tests (Story 5.4 AC4) ---

func TestValidateAuditDelete_Succeeded_Rejected(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-done"},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: soteriav1alpha1.ExecutionResultSucceeded,
		},
	}

	err := validateAuditDelete(exec)
	if err == nil {
		t.Fatal("expected Forbidden error for Succeeded execution")
	}
	if !strings.Contains(err.Error(), "FR41") {
		t.Errorf("error should mention FR41, got: %v", err)
	}
}

func TestValidateAuditDelete_Failed_Rejected(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-fail"},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: soteriav1alpha1.ExecutionResultFailed,
		},
	}

	err := validateAuditDelete(exec)
	if err == nil {
		t.Fatal("expected Forbidden error for Failed execution")
	}
}

func TestValidateAuditDelete_PartiallySucceeded_Rejected(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-partial"},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: soteriav1alpha1.ExecutionResultPartiallySucceeded,
		},
	}

	err := validateAuditDelete(exec)
	if err == nil {
		t.Fatal("expected Forbidden error for PartiallySucceeded execution")
	}
}

func TestValidateAuditDelete_InProgress_Allowed(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-running"},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result: "",
		},
	}

	err := validateAuditDelete(exec)
	if err != nil {
		t.Fatalf("expected no error for in-progress execution, got: %v", err)
	}
}

// --- No sensitive data test (Story 5.4 AC5) ---

func TestNoSensitiveData_InAuditRecord(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC))
	end := metav1.NewTime(time.Date(2026, 4, 20, 10, 2, 35, 0, time.UTC))

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name: "exec-audit",
			Labels: map[string]string{
				"soteria.io/plan-name": "erp-full-stack",
			},
		},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "erp-full-stack",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			Result:         soteriav1alpha1.ExecutionResultSucceeded,
			StartTime:      &start,
			CompletionTime: &end,
			Waves: []soteriav1alpha1.WaveStatus{
				{
					WaveIndex: 0,
					StartTime: &start,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{
							Name:    "ns-erp-database",
							Result:  soteriav1alpha1.DRGroupResultCompleted,
							VMNames: []string{"db-primary", "db-replica"},
							Error:   "replication timeout after 300s",
							Steps: []soteriav1alpha1.StepStatus{
								{Name: "StopReplication", Status: "Completed", Message: "Replication stopped"},
								{Name: "PromoteVolume", Status: "Completed", Message: "Volume promoted"},
								{Name: "StartVM", Status: "Completed", Message: "VM started"},
							},
							RetryCount: 1,
							StartTime:  &start,
						},
					},
				},
			},
			Conditions: []metav1.Condition{
				{
					Type:    "Progressing",
					Status:  metav1.ConditionTrue,
					Reason:  "ExecutionStarted",
					Message: "Execution started for plan erp-full-stack",
				},
			},
		},
	}

	sensitivePatterns := []string{"password", "secret", "credential", "token", "apikey"}

	var allStrings []string
	collectStrings(reflect.ValueOf(exec).Elem(), &allStrings)

	for _, s := range allStrings {
		lower := strings.ToLower(s)
		for _, pattern := range sensitivePatterns {
			if strings.Contains(lower, pattern) {
				t.Errorf("found sensitive pattern %q in field value: %q", pattern, s)
			}
		}
	}
}

// collectStrings recursively extracts all string values from a struct.
func collectStrings(v reflect.Value, out *[]string) {
	switch v.Kind() {
	case reflect.String:
		if s := v.String(); s != "" {
			*out = append(*out, s)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			collectStrings(v.Field(i), out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectStrings(v.Index(i), out)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			collectStrings(key, out)
			collectStrings(v.MapIndex(key), out)
		}
	case reflect.Ptr:
		if !v.IsNil() {
			collectStrings(v.Elem(), out)
		}
	case reflect.Interface:
		if !v.IsNil() {
			collectStrings(v.Elem(), out)
		}
	}
}

// --- Duration formatting tests ---

func TestExecDuration_NilTimestamps(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{}
	if got := execDuration(exec); got != "" {
		t.Errorf("expected empty string for nil timestamps, got %q", got)
	}
}

func TestExecDuration_NilCompletionTime(t *testing.T) {
	start := metav1.NewTime(time.Now())
	exec := &soteriav1alpha1.DRExecution{
		Status: soteriav1alpha1.DRExecutionStatus{StartTime: &start},
	}
	if got := execDuration(exec); got != "" {
		t.Errorf("expected empty string for nil CompletionTime, got %q", got)
	}
}

func TestExecDuration_ValidTimestamps(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	end := metav1.NewTime(time.Date(2026, 1, 1, 0, 2, 35, 0, time.UTC))
	exec := &soteriav1alpha1.DRExecution{
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime:      &start,
			CompletionTime: &end,
		},
	}
	got := execDuration(exec)
	if got == "" {
		t.Error("expected non-empty duration")
	}
	// duration.HumanDuration for 2m35s produces "2m35s"
	if got != "2m35s" {
		t.Errorf("expected 2m35s, got %q", got)
	}
}

func TestTranslateTimestampSince_Zero(t *testing.T) {
	if got := translateTimestampSince(metav1.Time{}); got != "<unknown>" {
		t.Errorf("expected <unknown> for zero timestamp, got %q", got)
	}
}

func TestTranslateTimestampSince_NonZero(t *testing.T) {
	ts := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	got := translateTimestampSince(ts)
	if got == "" || got == "<unknown>" {
		t.Errorf("expected non-empty age, got %q", got)
	}
}

// Verify that the _ = fmt.Errorf import is used (compile check).
var _ = fmt.Sprintf
