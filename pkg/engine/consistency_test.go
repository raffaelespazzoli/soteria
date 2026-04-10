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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclient "k8s.io/client-go/kubernetes/fake"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// MockNamespaceLookup implements NamespaceLookup with configurable per-namespace levels.
type MockNamespaceLookup struct {
	Levels map[string]soteriav1alpha1.ConsistencyLevel
}

func (m *MockNamespaceLookup) GetConsistencyLevel(
	_ context.Context, namespace string,
) (soteriav1alpha1.ConsistencyLevel, error) {
	if level, ok := m.Levels[namespace]; ok {
		return level, nil
	}
	return soteriav1alpha1.ConsistencyLevelVM, nil
}

func TestResolveVolumeGroups(t *testing.T) {
	tests := []struct {
		name              string
		vms               []VMReference
		waveLabel         string
		nsLevels          map[string]soteriav1alpha1.ConsistencyLevel
		wantGroupCount    int
		wantConflictCount int
		wantGroupNames    []string
		wantConflictNS    []string
	}{
		{
			name: "all VM-level (no annotation) — individual VolumeGroups",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns-a", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-2", Namespace: "ns-a", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-3", Namespace: "ns-b", Labels: map[string]string{"wave": "1"}},
			},
			waveLabel:         "wave",
			nsLevels:          map[string]soteriav1alpha1.ConsistencyLevel{},
			wantGroupCount:    3,
			wantConflictCount: 0,
			wantGroupNames:    []string{"vm-ns-a-vm-1", "vm-ns-a-vm-2", "vm-ns-b-vm-3"},
		},
		{
			name: "namespace-level — single VolumeGroup per namespace",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "erp-db", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-2", Namespace: "erp-db", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-3", Namespace: "erp-db", Labels: map[string]string{"wave": "1"}},
			},
			waveLabel: "wave",
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{
				"erp-db": soteriav1alpha1.ConsistencyLevelNamespace,
			},
			wantGroupCount:    1,
			wantConflictCount: 0,
			wantGroupNames:    []string{"ns-erp-db"},
		},
		{
			name: "mixed: 1 namespace-level, 2 VM-level namespaces",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns-level", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-2", Namespace: "ns-level", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-3", Namespace: "vm-a", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-4", Namespace: "vm-b", Labels: map[string]string{"wave": "2"}},
			},
			waveLabel: "wave",
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{
				"ns-level": soteriav1alpha1.ConsistencyLevelNamespace,
			},
			wantGroupCount:    3,
			wantConflictCount: 0,
			wantGroupNames:    []string{"ns-ns-level", "vm-vm-a-vm-3", "vm-vm-b-vm-4"},
		},
		{
			name: "namespace-level, same wave — no conflicts",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "erp-db", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-2", Namespace: "erp-db", Labels: map[string]string{"wave": "1"}},
			},
			waveLabel: "wave",
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{
				"erp-db": soteriav1alpha1.ConsistencyLevelNamespace,
			},
			wantGroupCount:    1,
			wantConflictCount: 0,
		},
		{
			name: "namespace-level, different waves — WaveConflict",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "erp-db", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-2", Namespace: "erp-db", Labels: map[string]string{"wave": "2"}},
			},
			waveLabel: "wave",
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{
				"erp-db": soteriav1alpha1.ConsistencyLevelNamespace,
			},
			wantGroupCount:    1,
			wantConflictCount: 1,
			wantConflictNS:    []string{"erp-db"},
		},
		{
			name: "multiple namespace-level namespaces with different-wave conflicts",
			vms: []VMReference{
				{Name: "vm-1", Namespace: "ns-a", Labels: map[string]string{"wave": "1"}},
				{Name: "vm-2", Namespace: "ns-a", Labels: map[string]string{"wave": "2"}},
				{Name: "vm-3", Namespace: "ns-b", Labels: map[string]string{"wave": "3"}},
				{Name: "vm-4", Namespace: "ns-b", Labels: map[string]string{"wave": "4"}},
			},
			waveLabel: "wave",
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{
				"ns-a": soteriav1alpha1.ConsistencyLevelNamespace,
				"ns-b": soteriav1alpha1.ConsistencyLevelNamespace,
			},
			wantGroupCount:    2,
			wantConflictCount: 2,
			wantConflictNS:    []string{"ns-a", "ns-b"},
		},
		{
			name:              "empty VM list — empty result",
			vms:               nil,
			waveLabel:         "wave",
			nsLevels:          map[string]soteriav1alpha1.ConsistencyLevel{},
			wantGroupCount:    0,
			wantConflictCount: 0,
		},
		{
			name: "single VM in namespace-level namespace — 1 VolumeGroup with 1 VM",
			vms: []VMReference{
				{Name: "vm-solo", Namespace: "erp-db", Labels: map[string]string{"wave": "1"}},
			},
			waveLabel: "wave",
			nsLevels: map[string]soteriav1alpha1.ConsistencyLevel{
				"erp-db": soteriav1alpha1.ConsistencyLevelNamespace,
			},
			wantGroupCount:    1,
			wantConflictCount: 0,
			wantGroupNames:    []string{"ns-erp-db"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockNamespaceLookup{Levels: tt.nsLevels}
			result, err := ResolveVolumeGroups(context.Background(), tt.vms, tt.waveLabel, mock)
			if err != nil {
				t.Fatalf("ResolveVolumeGroups() error: %v", err)
			}

			if len(result.VolumeGroups) != tt.wantGroupCount {
				t.Errorf("VolumeGroups count = %d, want %d", len(result.VolumeGroups), tt.wantGroupCount)
			}
			if len(result.WaveConflicts) != tt.wantConflictCount {
				t.Errorf("WaveConflicts count = %d, want %d", len(result.WaveConflicts), tt.wantConflictCount)
			}

			if tt.wantGroupNames != nil {
				for i, wantName := range tt.wantGroupNames {
					if i >= len(result.VolumeGroups) {
						t.Errorf("missing VolumeGroup[%d] %q", i, wantName)
						continue
					}
					if result.VolumeGroups[i].Name != wantName {
						t.Errorf("VolumeGroups[%d].Name = %q, want %q", i, result.VolumeGroups[i].Name, wantName)
					}
				}
			}

			if tt.wantConflictNS != nil {
				for i, wantNS := range tt.wantConflictNS {
					if i >= len(result.WaveConflicts) {
						t.Errorf("missing WaveConflict[%d] for namespace %q", i, wantNS)
						continue
					}
					if result.WaveConflicts[i].Namespace != wantNS {
						t.Errorf("WaveConflicts[%d].Namespace = %q, want %q",
							i, result.WaveConflicts[i].Namespace, wantNS)
					}
				}
			}
		})
	}
}

func TestDefaultNamespaceLookup(t *testing.T) {
	tests := []struct {
		name      string
		ns        *corev1.Namespace
		wantLevel soteriav1alpha1.ConsistencyLevel
	}{
		{
			name: "annotation set to namespace",
			ns: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "erp-db",
					Annotations: map[string]string{
						soteriav1alpha1.ConsistencyAnnotation: "namespace",
					},
				},
			},
			wantLevel: soteriav1alpha1.ConsistencyLevelNamespace,
		},
		{
			name: "annotation missing — defaults to VM",
			ns: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "plain-ns",
				},
			},
			wantLevel: soteriav1alpha1.ConsistencyLevelVM,
		},
		{
			name: "annotation with invalid value — defaults to VM",
			ns: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bad-value",
					Annotations: map[string]string{
						soteriav1alpha1.ConsistencyAnnotation: "invalid",
					},
				},
			},
			wantLevel: soteriav1alpha1.ConsistencyLevelVM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fakeclient.NewClientset(tt.ns)
			lookup := &DefaultNamespaceLookup{Client: clientset.CoreV1()}

			level, err := lookup.GetConsistencyLevel(context.Background(), tt.ns.Name)
			if err != nil {
				t.Fatalf("GetConsistencyLevel() error: %v", err)
			}
			if level != tt.wantLevel {
				t.Errorf("level = %q, want %q", level, tt.wantLevel)
			}
		})
	}
}
