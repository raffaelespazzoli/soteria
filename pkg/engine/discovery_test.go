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
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestGroupByWave(t *testing.T) {
	tests := []struct {
		name           string
		vms            []VMReference
		waveLabel      string
		wantWaveCount  int
		wantTotalVMs   int
		wantWaveKeys   []string
		wantWaveVMCnts []int
	}{
		{
			name: "single wave",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-2", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-3", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-4", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-5", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
			},
			waveLabel:      "soteria.io/wave",
			wantWaveCount:  1,
			wantTotalVMs:   5,
			wantWaveKeys:   []string{"1"},
			wantWaveVMCnts: []int{5},
		},
		{
			name: "multiple waves",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-2", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-3", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "2"}},
				{Name: "vm-4", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "2"}},
				{Name: "vm-5", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "2"}},
				{Name: "vm-6", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "3"}},
				{Name: "vm-7", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "3"}},
				{Name: "vm-8", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "3"}},
				{Name: "vm-9", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "3"}},
				{Name: "vm-10", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "3"}},
			},
			waveLabel:      "soteria.io/wave",
			wantWaveCount:  3,
			wantTotalVMs:   10,
			wantWaveKeys:   []string{"1", "2", "3"},
			wantWaveVMCnts: []int{2, 3, 5},
		},
		{
			name: "VMs without wave labels go to empty-key wave",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns", Labels: map[string]string{"app": "erp"}},
				{Name: "vm-2", Namespace: "ns", Labels: map[string]string{"app": "erp"}},
				{Name: "vm-3", Namespace: "ns", Labels: nil},
			},
			waveLabel:      "soteria.io/wave",
			wantWaveCount:  1,
			wantTotalVMs:   3,
			wantWaveKeys:   []string{""},
			wantWaveVMCnts: []int{3},
		},
		{
			name:           "empty input",
			vms:            nil,
			waveLabel:      "soteria.io/wave",
			wantWaveCount:  0,
			wantTotalVMs:   0,
			wantWaveKeys:   nil,
			wantWaveVMCnts: nil,
		},
		{
			name: "mixed: some with wave label, some without",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-2", Namespace: "ns", Labels: map[string]string{"app": "erp"}},
				{Name: "vm-3", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "2"}},
				{Name: "vm-4", Namespace: "ns", Labels: nil},
			},
			waveLabel:      "soteria.io/wave",
			wantWaveCount:  3,
			wantTotalVMs:   4,
			wantWaveKeys:   []string{"", "1", "2"},
			wantWaveVMCnts: []int{2, 1, 1},
		},
		{
			name: "deterministic ordering: waves sorted lexicographically",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "3"}},
				{Name: "vm-2", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "1"}},
				{Name: "vm-3", Namespace: "ns", Labels: map[string]string{"soteria.io/wave": "2"}},
			},
			waveLabel:      "soteria.io/wave",
			wantWaveCount:  3,
			wantTotalVMs:   3,
			wantWaveKeys:   []string{"1", "2", "3"},
			wantWaveVMCnts: []int{1, 1, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GroupByWave(tt.vms, tt.waveLabel)

			if result.TotalVMs != tt.wantTotalVMs {
				t.Errorf("TotalVMs = %d, want %d", result.TotalVMs, tt.wantTotalVMs)
			}
			if len(result.Waves) != tt.wantWaveCount {
				t.Fatalf("len(Waves) = %d, want %d", len(result.Waves), tt.wantWaveCount)
			}

			for i, w := range result.Waves {
				if w.WaveKey != tt.wantWaveKeys[i] {
					t.Errorf("Waves[%d].WaveKey = %q, want %q", i, w.WaveKey, tt.wantWaveKeys[i])
				}
				if len(w.VMs) != tt.wantWaveVMCnts[i] {
					t.Errorf("Waves[%d] VM count = %d, want %d", i, len(w.VMs), tt.wantWaveVMCnts[i])
				}
			}
		})
	}
}

func TestTypedVMDiscoverer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := kubevirtv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add kubevirt to scheme: %v", err)
	}

	matchingVM1 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-match-1",
			Namespace: "default",
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-a",
				"soteria.io/wave":           "1",
			},
		},
	}
	matchingVM2 := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-match-2",
			Namespace: "default",
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-a",
				"soteria.io/wave":           "2",
			},
		},
	}
	nonMatchingVM := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-other",
			Namespace: "default",
			Labels: map[string]string{
				soteriav1alpha1.DRPlanLabel: "plan-b",
			},
		},
	}
	noLabelVM := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-no-label",
			Namespace: "default",
			Labels: map[string]string{
				"app": "standalone",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(matchingVM1, matchingVM2, nonMatchingVM, noLabelVM).
		Build()

	discoverer := NewTypedVMDiscoverer(fakeClient)

	t.Run("discovers VMs with matching drplan label", func(t *testing.T) {
		refs, err := discoverer.DiscoverVMs(context.Background(), "plan-a")
		if err != nil {
			t.Fatalf("DiscoverVMs() error: %v", err)
		}
		if len(refs) != 2 {
			t.Fatalf("expected 2 VMs, got %d", len(refs))
		}

		names := map[string]bool{}
		for _, ref := range refs {
			names[ref.Name] = true
			if ref.Namespace != "default" {
				t.Errorf("expected namespace 'default', got %q", ref.Namespace)
			}
			if ref.Labels == nil {
				t.Error("expected non-nil labels")
			}
		}
		if !names["vm-match-1"] || !names["vm-match-2"] {
			t.Errorf("expected vm-match-1 and vm-match-2 in results, got %v", names)
		}
	})

	t.Run("VMs without drplan label are not discovered", func(t *testing.T) {
		refs, err := discoverer.DiscoverVMs(context.Background(), "plan-a")
		if err != nil {
			t.Fatalf("DiscoverVMs() error: %v", err)
		}
		for _, ref := range refs {
			if ref.Name == "vm-no-label" {
				t.Error("VM without drplan label should not be discovered")
			}
		}
	})

	t.Run("VMs with different plan name are not discovered", func(t *testing.T) {
		refs, err := discoverer.DiscoverVMs(context.Background(), "plan-a")
		if err != nil {
			t.Fatalf("DiscoverVMs() error: %v", err)
		}
		for _, ref := range refs {
			if ref.Name == "vm-other" {
				t.Error("VM with different plan name should not be discovered")
			}
		}
	})

	t.Run("no VMs match nonexistent plan", func(t *testing.T) {
		refs, err := discoverer.DiscoverVMs(context.Background(), "nonexistent-plan")
		if err != nil {
			t.Fatalf("DiscoverVMs() error: %v", err)
		}
		if len(refs) != 0 {
			t.Errorf("expected 0 VMs, got %d", len(refs))
		}
	})

	t.Run("labels are correctly extracted", func(t *testing.T) {
		refs, err := discoverer.DiscoverVMs(context.Background(), "plan-a")
		if err != nil {
			t.Fatalf("DiscoverVMs() error: %v", err)
		}
		for _, ref := range refs {
			if ref.Name == "vm-match-1" {
				if ref.Labels["soteria.io/wave"] != "1" {
					t.Errorf("expected wave label '1', got %q", ref.Labels["soteria.io/wave"])
				}
			}
		}
	})
}

