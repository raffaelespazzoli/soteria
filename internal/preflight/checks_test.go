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

package preflight

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/engine"
)

func TestComposeReport(t *testing.T) {
	now := metav1.Now()

	tests := []struct {
		name          string
		input         CompositionInput
		wantTotalVMs  int
		wantWaveCount int
		wantWarnings  int
		verify        func(t *testing.T, report *soteriav1alpha1.PreflightReport)
	}{
		{
			name: "Single wave, all VM-level consistency, known storage",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 2,
					Waves: []engine.WaveGroup{{
						WaveKey: "1",
						VMs: []engine.VMReference{
							{Name: "vm-1", Namespace: "ns1"},
							{Name: "vm-2", Namespace: "ns1"},
						},
					}},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						{Name: "vm-ns1-vm-1", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-1"}},
						{Name: "vm-ns1-vm-2", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-2"}},
					},
				},
				ChunkResult: &engine.ChunkResult{
					Waves: []engine.WaveChunks{{
						WaveKey: "1",
						Chunks: []engine.DRGroupChunk{{
							Name: "wave-1-group-0",
							VMs: []engine.VMReference{
								{Name: "vm-1", Namespace: "ns1"},
								{Name: "vm-2", Namespace: "ns1"},
							},
							VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
								{Name: "vm-ns1-vm-1"},
								{Name: "vm-ns1-vm-2"},
							},
						}},
					}},
				},
				StorageBackends: map[string]string{"ns1/vm-1": "odf", "ns1/vm-2": "odf"},
			},
			wantTotalVMs:  2,
			wantWaveCount: 1,
			wantWarnings:  0,
			verify: func(t *testing.T, r *soteriav1alpha1.PreflightReport) {
				t.Helper()
				w := r.Waves[0]
				if w.VMCount != 2 {
					t.Errorf("Wave VMCount = %d, want 2", w.VMCount)
				}
				if len(w.VMs) != 2 {
					t.Errorf("Wave VMs = %d, want 2", len(w.VMs))
				}
				if w.VMs[0].StorageBackend != "odf" {
					t.Errorf("VM[0].StorageBackend = %q, want odf", w.VMs[0].StorageBackend)
				}
				if w.VMs[0].ConsistencyLevel != "vm" {
					t.Errorf("VM[0].ConsistencyLevel = %q, want vm", w.VMs[0].ConsistencyLevel)
				}
				if len(w.Chunks) != 1 {
					t.Errorf("Wave chunks = %d, want 1", len(w.Chunks))
				}
				if w.Chunks[0].VMCount != 2 {
					t.Errorf("Chunk VMCount = %d, want 2", w.Chunks[0].VMCount)
				}
			},
		},
		{
			name: "Multiple waves sorted by key",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 3,
					Waves: []engine.WaveGroup{
						{WaveKey: "1", VMs: []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}, {Name: "vm-2", Namespace: "ns1"}}},
						{WaveKey: "2", VMs: []engine.VMReference{{Name: "vm-3", Namespace: "ns1"}}},
					},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						{Name: "vm-ns1-vm-1", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-1"}},
						{Name: "vm-ns1-vm-2", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-2"}},
						{Name: "vm-ns1-vm-3", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-3"}},
					},
				},
				ChunkResult:     &engine.ChunkResult{},
				StorageBackends: map[string]string{"ns1/vm-1": "odf", "ns1/vm-2": "odf", "ns1/vm-3": "odf"},
			},
			wantTotalVMs:  3,
			wantWaveCount: 2,
			wantWarnings:  0,
			verify: func(t *testing.T, r *soteriav1alpha1.PreflightReport) {
				t.Helper()
				if r.Waves[0].WaveKey != "1" || r.Waves[1].WaveKey != "2" {
					t.Errorf("WaveKeys = [%q, %q], want [1, 2]", r.Waves[0].WaveKey, r.Waves[1].WaveKey)
				}
				if r.Waves[0].VMCount != 2 || r.Waves[1].VMCount != 1 {
					t.Errorf("VM counts = [%d, %d], want [2, 1]", r.Waves[0].VMCount, r.Waves[1].VMCount)
				}
			},
		},
		{
			name: "Namespace-level VMs reflected correctly",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 2,
					Waves: []engine.WaveGroup{{
						WaveKey: "1",
						VMs: []engine.VMReference{
							{Name: "vm-1", Namespace: "ns1"},
							{Name: "vm-2", Namespace: "ns1"},
						},
					}},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						{Name: "ns-ns1", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace, VMNames: []string{"vm-1", "vm-2"}},
					},
				},
				ChunkResult: &engine.ChunkResult{
					Waves: []engine.WaveChunks{{
						WaveKey: "1",
						Chunks: []engine.DRGroupChunk{{
							Name: "wave-1-group-0",
							VMs: []engine.VMReference{
								{Name: "vm-1", Namespace: "ns1"},
								{Name: "vm-2", Namespace: "ns1"},
							},
							VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
								{Name: "ns-ns1"},
							},
						}},
					}},
				},
				StorageBackends: map[string]string{"ns1/vm-1": "odf", "ns1/vm-2": "odf"},
			},
			wantTotalVMs:  2,
			wantWaveCount: 1,
			wantWarnings:  0,
			verify: func(t *testing.T, r *soteriav1alpha1.PreflightReport) {
				t.Helper()
				for _, vm := range r.Waves[0].VMs {
					if vm.ConsistencyLevel != "namespace" {
						t.Errorf("VM %s.ConsistencyLevel = %q, want namespace", vm.Name, vm.ConsistencyLevel)
					}
					if vm.VolumeGroupName != "ns-ns1" {
						t.Errorf("VM %s.VolumeGroupName = %q, want ns-ns1", vm.Name, vm.VolumeGroupName)
					}
				}
				chunk := r.Waves[0].Chunks[0]
				if len(chunk.VolumeGroups) != 1 || chunk.VolumeGroups[0] != "ns-ns1" {
					t.Errorf("Chunk VolumeGroups = %v, want [ns-ns1]", chunk.VolumeGroups)
				}
			},
		},
		{
			name: "Unknown storage backend generates warning",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 1,
					Waves: []engine.WaveGroup{{
						WaveKey: "1",
						VMs:     []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
					}},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						{Name: "vm-ns1-vm-1", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-1"}},
					},
				},
				ChunkResult:     &engine.ChunkResult{},
				StorageBackends: map[string]string{"ns1/vm-1": "unknown"},
			},
			wantTotalVMs:  1,
			wantWaveCount: 1,
			wantWarnings:  1,
		},
		{
			name: "No-volume VMs generate warning",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 1,
					Waves: []engine.WaveGroup{{
						WaveKey: "1",
						VMs:     []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}},
					}},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						{Name: "vm-ns1-vm-1", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelVM, VMNames: []string{"vm-1"}},
					},
				},
				ChunkResult:     &engine.ChunkResult{},
				StorageBackends: map[string]string{"ns1/vm-1": "none"},
			},
			wantTotalVMs:  1,
			wantWaveCount: 1,
			wantWarnings:  1,
		},
		{
			name: "Chunk errors in report produce warnings",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 3,
					Waves: []engine.WaveGroup{{
						WaveKey: "1",
						VMs: []engine.VMReference{
							{Name: "vm-1", Namespace: "ns1"},
							{Name: "vm-2", Namespace: "ns1"},
							{Name: "vm-3", Namespace: "ns1"},
						},
					}},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					VolumeGroups: []soteriav1alpha1.VolumeGroupInfo{
						{Name: "ns-ns1", Namespace: "ns1", ConsistencyLevel: soteriav1alpha1.ConsistencyLevelNamespace, VMNames: []string{"vm-1", "vm-2", "vm-3"}},
					},
				},
				ChunkResult: &engine.ChunkResult{
					Errors: []engine.ChunkError{{
						WaveKey:       "1",
						Namespace:     "ns1",
						GroupSize:     3,
						MaxConcurrent: 2,
					}},
				},
				StorageBackends: map[string]string{"ns1/vm-1": "odf", "ns1/vm-2": "odf", "ns1/vm-3": "odf"},
			},
			wantTotalVMs:  3,
			wantWaveCount: 1,
			wantWarnings:  1,
		},
		{
			name: "Wave conflicts in report produce warnings",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 2,
					Waves: []engine.WaveGroup{
						{WaveKey: "1", VMs: []engine.VMReference{{Name: "vm-1", Namespace: "ns1"}}},
						{WaveKey: "2", VMs: []engine.VMReference{{Name: "vm-2", Namespace: "ns1"}}},
					},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					WaveConflicts: []engine.WaveConflict{{
						Namespace: "ns1",
						VMNames:   []string{"vm-1", "vm-2"},
						WaveKeys:  []string{"1", "2"},
					}},
				},
				ChunkResult:     &engine.ChunkResult{},
				StorageBackends: map[string]string{"ns1/vm-1": "odf", "ns1/vm-2": "odf"},
			},
			wantTotalVMs:  2,
			wantWaveCount: 2,
			wantWarnings:  1,
		},
		{
			name: "Empty plan - no VMs",
			input: CompositionInput{
				Plan: &soteriav1alpha1.DRPlan{},
				DiscoveryResult: &engine.DiscoveryResult{
					TotalVMs: 0,
					Waves:    nil,
				},
				ConsistencyResult: &engine.ConsistencyResult{},
				ChunkResult:       &engine.ChunkResult{},
				StorageBackends:   map[string]string{},
			},
			wantTotalVMs:  0,
			wantWaveCount: 0,
			wantWarnings:  0,
		},
		{
			name: "Nil DiscoveryResult - graceful degradation",
			input: CompositionInput{
				Plan:              &soteriav1alpha1.DRPlan{},
				DiscoveryResult:   nil,
				ConsistencyResult: nil,
				ChunkResult:       nil,
				StorageBackends:   map[string]string{},
			},
			wantTotalVMs:  0,
			wantWaveCount: 0,
			wantWarnings:  0,
			verify: func(t *testing.T, r *soteriav1alpha1.PreflightReport) {
				t.Helper()
				if r.GeneratedAt == nil {
					t.Error("GeneratedAt should always be set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := ComposeReport(tt.input, now)

			if report.TotalVMs != tt.wantTotalVMs {
				t.Errorf("TotalVMs = %d, want %d", report.TotalVMs, tt.wantTotalVMs)
			}
			if len(report.Waves) != tt.wantWaveCount {
				t.Errorf("len(Waves) = %d, want %d", len(report.Waves), tt.wantWaveCount)
			}
			if len(report.Warnings) != tt.wantWarnings {
				t.Errorf("len(Warnings) = %d, want %d; warnings: %v",
					len(report.Warnings), tt.wantWarnings, report.Warnings)
			}
			if report.GeneratedAt == nil {
				t.Error("GeneratedAt should not be nil")
			}

			if tt.verify != nil {
				tt.verify(t, report)
			}
		})
	}
}

func TestCollectWarnings(t *testing.T) {
	tests := []struct {
		name         string
		input        CompositionInput
		wantWarnings int
	}{
		{
			name: "Unknown storage backend",
			input: CompositionInput{
				StorageBackends:   map[string]string{"ns1/vm-1": "unknown"},
				ChunkResult:       &engine.ChunkResult{},
				ConsistencyResult: &engine.ConsistencyResult{},
			},
			wantWarnings: 1,
		},
		{
			name: "No PVC volumes",
			input: CompositionInput{
				StorageBackends:   map[string]string{"ns1/vm-1": "none"},
				ChunkResult:       &engine.ChunkResult{},
				ConsistencyResult: &engine.ConsistencyResult{},
			},
			wantWarnings: 1,
		},
		{
			name: "Chunk error",
			input: CompositionInput{
				StorageBackends: map[string]string{},
				ChunkResult: &engine.ChunkResult{
					Errors: []engine.ChunkError{{
						WaveKey: "1", Namespace: "ns1", GroupSize: 5, MaxConcurrent: 3,
					}},
				},
				ConsistencyResult: &engine.ConsistencyResult{},
			},
			wantWarnings: 1,
		},
		{
			name: "Wave conflict",
			input: CompositionInput{
				StorageBackends: map[string]string{},
				ChunkResult:     &engine.ChunkResult{},
				ConsistencyResult: &engine.ConsistencyResult{
					WaveConflicts: []engine.WaveConflict{{
						Namespace: "ns1", VMNames: []string{"vm-1"}, WaveKeys: []string{"1", "2"},
					}},
				},
			},
			wantWarnings: 1,
		},
		{
			name: "No issues - no warnings",
			input: CompositionInput{
				StorageBackends:   map[string]string{"ns1/vm-1": "odf"},
				ChunkResult:       &engine.ChunkResult{},
				ConsistencyResult: &engine.ConsistencyResult{},
			},
			wantWarnings: 0,
		},
		{
			name: "Multiple issues combined",
			input: CompositionInput{
				StorageBackends: map[string]string{"ns1/vm-1": "unknown", "ns1/vm-2": "none"},
				ChunkResult: &engine.ChunkResult{
					Errors: []engine.ChunkError{{WaveKey: "1", Namespace: "ns1", GroupSize: 5, MaxConcurrent: 3}},
				},
				ConsistencyResult: &engine.ConsistencyResult{
					WaveConflicts: []engine.WaveConflict{{Namespace: "ns1"}},
				},
			},
			wantWarnings: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := collectWarnings(tt.input)
			if len(warnings) != tt.wantWarnings {
				t.Errorf("len(warnings) = %d, want %d; warnings: %v",
					len(warnings), tt.wantWarnings, warnings)
			}
		})
	}
}

func TestComposeReport_SiteTopologyFields(t *testing.T) {
	now := metav1.Now()
	input := CompositionInput{
		Plan: &soteriav1alpha1.DRPlan{
			Spec: soteriav1alpha1.DRPlanSpec{
				PrimarySite:   "dc-west",
				SecondarySite: "dc-east",
			},
			Status: soteriav1alpha1.DRPlanStatus{
				ActiveSite: "dc-west",
			},
		},
	}

	report := ComposeReport(input, now)

	if report.PrimarySite != "dc-west" {
		t.Errorf("PrimarySite = %q, want %q", report.PrimarySite, "dc-west")
	}
	if report.SecondarySite != "dc-east" {
		t.Errorf("SecondarySite = %q, want %q", report.SecondarySite, "dc-east")
	}
	if report.ActiveSite != "dc-west" {
		t.Errorf("ActiveSite = %q, want %q", report.ActiveSite, "dc-west")
	}
}

func TestComposeReport_ActiveExecution(t *testing.T) {
	now := metav1.Now()
	input := CompositionInput{
		Plan: &soteriav1alpha1.DRPlan{
			Spec: soteriav1alpha1.DRPlanSpec{
				PrimarySite:   "dc-west",
				SecondarySite: "dc-east",
			},
			Status: soteriav1alpha1.DRPlanStatus{
				ActiveSite:      "dc-west",
				ActiveExecution: "exec-failover-1",
			},
		},
	}

	report := ComposeReport(input, now)

	if report.ActiveExecution != "exec-failover-1" {
		t.Errorf("ActiveExecution = %q, want %q", report.ActiveExecution, "exec-failover-1")
	}

	hasWarning := slices.Contains(report.Warnings, "execution exec-failover-1 is active; new execution blocked")
	if !hasWarning {
		t.Errorf("expected active execution warning, got %v", report.Warnings)
	}
}

func TestComposeReport_NoActiveExecution_NoWarning(t *testing.T) {
	now := metav1.Now()
	input := CompositionInput{
		Plan: &soteriav1alpha1.DRPlan{
			Spec: soteriav1alpha1.DRPlanSpec{
				PrimarySite:   "dc-west",
				SecondarySite: "dc-east",
			},
			Status: soteriav1alpha1.DRPlanStatus{
				ActiveSite: "dc-west",
			},
		},
	}

	report := ComposeReport(input, now)

	if report.ActiveExecution != "" {
		t.Errorf("ActiveExecution = %q, want empty", report.ActiveExecution)
	}

	for _, w := range report.Warnings {
		if w == "execution  is active; new execution blocked" {
			t.Error("should not emit active execution warning when no active execution")
		}
	}
}
