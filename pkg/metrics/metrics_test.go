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

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordPlanVMs(t *testing.T) {
	RecordPlanVMs("plan-a", 5)
	RecordPlanVMs("plan-b", 10)

	if got := testutil.ToFloat64(DRPlanVMsTotal.WithLabelValues("plan-a")); got != 5 {
		t.Errorf("plan-a VM count = %v, want 5", got)
	}
	if got := testutil.ToFloat64(DRPlanVMsTotal.WithLabelValues("plan-b")); got != 10 {
		t.Errorf("plan-b VM count = %v, want 10", got)
	}

	// Update plan-a to verify gauge overwrite.
	RecordPlanVMs("plan-a", 3)
	if got := testutil.ToFloat64(DRPlanVMsTotal.WithLabelValues("plan-a")); got != 3 {
		t.Errorf("plan-a VM count after update = %v, want 3", got)
	}
}

func TestRecordExecutionCompletion(t *testing.T) {
	RecordExecutionCompletion("disaster", "Succeeded", 42.5)

	// Verify histogram observation via CollectAndCount (the HistogramVec
	// now contains at least one label set with sum/count/buckets).
	histMetrics := testutil.CollectAndCount(FailoverDurationSeconds)
	if histMetrics == 0 {
		t.Errorf("FailoverDurationSeconds has no metric families after observation")
	}

	counterVal := testutil.ToFloat64(ExecutionTotal.WithLabelValues("disaster", "Succeeded"))
	if counterVal != 1 {
		t.Errorf("counter value = %v, want 1", counterVal)
	}

	// Record a second execution with different labels.
	RecordExecutionCompletion("planned_migration", "Failed", 120.0)
	if got := testutil.ToFloat64(ExecutionTotal.WithLabelValues("planned_migration", "Failed")); got != 1 {
		t.Errorf("planned_migration/Failed counter = %v, want 1", got)
	}

	// Verify first counter was not affected by second call.
	if got := testutil.ToFloat64(ExecutionTotal.WithLabelValues("disaster", "Succeeded")); got != 1 {
		t.Errorf("disaster/Succeeded counter changed unexpectedly = %v, want 1", got)
	}
}

func TestDeletePlanMetrics(t *testing.T) {
	RecordPlanVMs("plan-del-test", 8)

	if got := testutil.ToFloat64(DRPlanVMsTotal.WithLabelValues("plan-del-test")); got != 8 {
		t.Fatalf("pre-delete VM count = %v, want 8", got)
	}

	countBefore := testutil.CollectAndCount(DRPlanVMsTotal)

	DeletePlanMetrics("plan-del-test")

	countAfter := testutil.CollectAndCount(DRPlanVMsTotal)

	if countAfter >= countBefore {
		t.Errorf("DRPlanVMsTotal series count did not decrease: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestAllMetricsRegistered(t *testing.T) {
	// Verifying that the init() registration did not panic is covered by
	// the test binary starting at all. Additionally, Describe each
	// collector to confirm they produce valid descriptors.
	collectors := []prometheus.Collector{
		CheckpointWritesTotal,
		CheckpointWriteDuration,
		CheckpointRetriesTotal,
		ReprotectDuration,
		ReprotectVGSetupDuration,
		ReprotectHealthPollsTotal,
		DRPlanVMsTotal,
		FailoverDurationSeconds,
		ExecutionTotal,
	}
	for _, c := range collectors {
		ch := make(chan *prometheus.Desc, 10)
		c.Describe(ch)
		close(ch)
		count := 0
		for range ch {
			count++
		}
		if count == 0 {
			t.Errorf("collector %T produced no descriptors", c)
		}
	}
}

func TestMetricDescriptions_NoSensitiveData(t *testing.T) {
	sensitiveWords := []string{"password", "secret", "credential", "token", "key"}

	collectors := []prometheus.Collector{
		CheckpointWritesTotal,
		CheckpointWriteDuration,
		CheckpointRetriesTotal,
		ReprotectDuration,
		ReprotectVGSetupDuration,
		ReprotectHealthPollsTotal,
		DRPlanVMsTotal,
		FailoverDurationSeconds,
		ExecutionTotal,
	}

	for _, c := range collectors {
		ch := make(chan *prometheus.Desc, 10)
		c.Describe(ch)
		close(ch)
		for desc := range ch {
			descStr := desc.String()
			lower := strings.ToLower(descStr)
			for _, word := range sensitiveWords {
				if strings.Contains(lower, word) {
					t.Errorf("metric descriptor contains sensitive word %q: %s", word, descStr)
				}
			}
		}
	}
}
