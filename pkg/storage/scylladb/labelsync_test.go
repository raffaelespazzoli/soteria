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

package scylladb

import (
	"reflect"
	"testing"
)

const testLabelValueWeb = "web"

func TestLabelDiff_AddLabels(t *testing.T) {
	added, removed := labelDiff(nil, map[string]string{"app": testLabelValueWeb, "tier": "frontend"})
	if len(added) != 2 || added["app"] != testLabelValueWeb || added["tier"] != "frontend" {
		t.Fatalf("expected 2 additions, got %v", added)
	}
	if len(removed) != 0 {
		t.Fatalf("expected 0 removals, got %v", removed)
	}
}

func TestLabelDiff_RemoveLabels(t *testing.T) {
	added, removed := labelDiff(map[string]string{"app": testLabelValueWeb, "tier": "frontend"}, nil)
	if len(added) != 0 {
		t.Fatalf("expected 0 additions, got %v", added)
	}
	if len(removed) != 2 || removed["app"] != testLabelValueWeb || removed["tier"] != "frontend" {
		t.Fatalf("expected 2 removals, got %v", removed)
	}
}

func TestLabelDiff_ChangeValue(t *testing.T) {
	old := map[string]string{"app": testLabelValueWeb, "tier": "frontend"}
	new := map[string]string{"app": testLabelValueWeb, "tier": "backend"}

	added, removed := labelDiff(old, new)
	if !reflect.DeepEqual(added, map[string]string{"tier": "backend"}) {
		t.Fatalf("expected tier=backend in added, got %v", added)
	}
	if !reflect.DeepEqual(removed, map[string]string{"tier": "frontend"}) {
		t.Fatalf("expected tier=frontend in removed, got %v", removed)
	}
}

func TestLabelDiff_NoChange(t *testing.T) {
	labels := map[string]string{"app": testLabelValueWeb, "tier": "frontend"}
	added, removed := labelDiff(labels, labels)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("expected no diff, got added=%v removed=%v", added, removed)
	}
}

func TestLabelDiff_MixedOperations(t *testing.T) {
	old := map[string]string{"app": testLabelValueWeb, "tier": "frontend", "version": "v1"}
	new := map[string]string{"app": testLabelValueWeb, "tier": "backend", "env": "prod"}

	added, removed := labelDiff(old, new)
	expectedAdded := map[string]string{"tier": "backend", "env": "prod"}
	expectedRemoved := map[string]string{"tier": "frontend", "version": "v1"}

	if !reflect.DeepEqual(added, expectedAdded) {
		t.Fatalf("expected added %v, got %v", expectedAdded, added)
	}
	if !reflect.DeepEqual(removed, expectedRemoved) {
		t.Fatalf("expected removed %v, got %v", expectedRemoved, removed)
	}
}

func TestLabelDiff_BothNil(t *testing.T) {
	added, removed := labelDiff(nil, nil)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("expected empty diff for nil/nil, got added=%v removed=%v", added, removed)
	}
}

func TestExtractLabels_WithLabels(t *testing.T) {
	obj := &fakeObject{}
	obj.Labels = map[string]string{"app": testLabelValueWeb}
	labels := extractLabels(obj)
	if labels["app"] != testLabelValueWeb {
		t.Fatalf("expected labels {app: %s}, got %v", testLabelValueWeb, labels)
	}
}

func TestExtractLabels_NoLabels(t *testing.T) {
	obj := &fakeObject{}
	labels := extractLabels(obj)
	if len(labels) != 0 {
		t.Fatalf("expected empty labels, got %v", labels)
	}
}
