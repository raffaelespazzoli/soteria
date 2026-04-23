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

func TestRecordPlanReplicationHealth(t *testing.T) {
	entries := []ReplicationLagEntry{
		{VolumeGroup: "vg-db", LagSeconds: 47.0},
		{VolumeGroup: "vg-app", LagSeconds: 12.5},
	}
	RecordPlanReplicationHealth("plan-a", entries)

	if got := testutil.ToFloat64(ReplicationLagSeconds.WithLabelValues("plan-a", "vg-db")); got != 47.0 {
		t.Errorf("plan-a/vg-db lag = %v, want 47.0", got)
	}
	if got := testutil.ToFloat64(ReplicationLagSeconds.WithLabelValues("plan-a", "vg-app")); got != 12.5 {
		t.Errorf("plan-a/vg-app lag = %v, want 12.5", got)
	}

	// Update with only 1 VG — the old vg-app series should be cleaned up.
	RecordPlanReplicationHealth("plan-a", []ReplicationLagEntry{
		{VolumeGroup: "vg-db", LagSeconds: 50.0},
	})
	if got := testutil.ToFloat64(ReplicationLagSeconds.WithLabelValues("plan-a", "vg-db")); got != 50.0 {
		t.Errorf("plan-a/vg-db lag after update = %v, want 50.0", got)
	}

	// Verify stale series was cleaned: collecting the metric should not
	// include a vg-app series for plan-a. We use CollectAndCount to check
	// that the total series count decreased. Set a known baseline with
	// plan-b to ensure we can count correctly.
	RecordPlanReplicationHealth("plan-b", []ReplicationLagEntry{
		{VolumeGroup: "vg-x", LagSeconds: 1.0},
	})
	count := testutil.CollectAndCount(ReplicationLagSeconds)
	// plan-a/vg-db + plan-b/vg-x = 2 (plan-a/vg-app was deleted)
	if count != 2 {
		t.Errorf("ReplicationLagSeconds series count = %d, want 2", count)
	}
}

func TestRecordUnprotectedVMs(t *testing.T) {
	RecordUnprotectedVMs(7)
	if got := testutil.ToFloat64(UnprotectedVMsTotal); got != 7 {
		t.Errorf("unprotected VMs = %v, want 7", got)
	}

	RecordUnprotectedVMs(0)
	if got := testutil.ToFloat64(UnprotectedVMsTotal); got != 0 {
		t.Errorf("unprotected VMs after reset = %v, want 0", got)
	}
}

func TestDeletePlanMetrics(t *testing.T) {
	// Use isolated labels to avoid cross-test pollution.
	RecordPlanVMs("plan-del-test", 8)
	RecordPlanReplicationHealth("plan-del-test", []ReplicationLagEntry{
		{VolumeGroup: "vg-del-1", LagSeconds: 30.0},
	})

	// Sanity: series exist.
	if got := testutil.ToFloat64(DRPlanVMsTotal.WithLabelValues("plan-del-test")); got != 8 {
		t.Fatalf("pre-delete VM count = %v, want 8", got)
	}

	countBefore := testutil.CollectAndCount(DRPlanVMsTotal)
	lagCountBefore := testutil.CollectAndCount(ReplicationLagSeconds)

	DeletePlanMetrics("plan-del-test")

	countAfter := testutil.CollectAndCount(DRPlanVMsTotal)
	lagCountAfter := testutil.CollectAndCount(ReplicationLagSeconds)

	if countAfter >= countBefore {
		t.Errorf("DRPlanVMsTotal series count did not decrease: before=%d, after=%d", countBefore, countAfter)
	}
	if lagCountAfter >= lagCountBefore {
		t.Errorf("ReplicationLagSeconds series count did not decrease: before=%d, after=%d", lagCountBefore, lagCountAfter)
	}
}

func TestRecordPlanReplicationHealth_NilClearsStale(t *testing.T) {
	RecordPlanReplicationHealth("plan-nil-test", []ReplicationLagEntry{
		{VolumeGroup: "vg-stale", LagSeconds: 99.0},
	})
	if got := testutil.ToFloat64(ReplicationLagSeconds.WithLabelValues("plan-nil-test", "vg-stale")); got != 99.0 {
		t.Fatalf("pre-clear lag = %v, want 99.0", got)
	}

	countBefore := testutil.CollectAndCount(ReplicationLagSeconds)

	// Passing nil entries should clear stale series via DeletePartialMatch.
	RecordPlanReplicationHealth("plan-nil-test", nil)

	countAfter := testutil.CollectAndCount(ReplicationLagSeconds)
	if countAfter >= countBefore {
		t.Errorf("ReplicationLagSeconds series count did not decrease after nil clear: "+
			"before=%d, after=%d", countBefore, countAfter)
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
		ReplicationLagSeconds,
		ExecutionTotal,
		UnprotectedVMsTotal,
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
		ReplicationLagSeconds,
		ExecutionTotal,
		UnprotectedVMsTotal,
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
