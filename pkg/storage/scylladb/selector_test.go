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
	"testing"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

func TestClassifySelector_EqualityOnly(t *testing.T) {
	sel, _ := labels.Parse("app=nginx")
	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable requirement")
	}
	if cs.primary.Key() != "app" {
		t.Fatalf("expected primary key 'app', got %q", cs.primary.Key())
	}
	if cs.primary.Operator() != selection.Equals {
		t.Fatalf("expected Equals operator, got %v", cs.primary.Operator())
	}
	if len(cs.residual) != 0 {
		t.Fatalf("expected no residual, got %d", len(cs.residual))
	}
}

func TestClassifySelector_MultiLabel(t *testing.T) {
	sel, _ := labels.Parse("app=nginx,tier=frontend")
	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable requirement")
	}
	if cs.primary.Operator() != selection.Equals {
		t.Fatalf("expected Equals primary, got %v", cs.primary.Operator())
	}
	if len(cs.residual) != 1 {
		t.Fatalf("expected 1 residual, got %d", len(cs.residual))
	}
}

func TestClassifySelector_NegativeOnly(t *testing.T) {
	sel, _ := labels.Parse("tier!=backend")
	cs := classifySelector(sel)
	if cs.hasPushable {
		t.Fatal("expected no pushable requirement for negative-only selector")
	}
	if len(cs.residual) != 1 {
		t.Fatalf("expected 1 residual, got %d", len(cs.residual))
	}
}

func TestClassifySelector_InOperator(t *testing.T) {
	sel, _ := labels.Parse("tier in (frontend,backend)")
	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable requirement for 'in'")
	}
	if cs.primary.Operator() != selection.In {
		t.Fatalf("expected In operator, got %v", cs.primary.Operator())
	}
}

func TestClassifySelector_Exists(t *testing.T) {
	sel, _ := labels.Parse("canary")
	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable requirement for 'exists'")
	}
	if cs.primary.Operator() != selection.Exists {
		t.Fatalf("expected Exists operator, got %v", cs.primary.Operator())
	}
}

func TestClassifySelector_MixedPositiveNegative(t *testing.T) {
	sel, _ := labels.Parse("app=nginx,tier!=backend")
	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable requirement")
	}
	if cs.primary.Key() != "app" {
		t.Fatalf("expected primary key 'app', got %q", cs.primary.Key())
	}
	if len(cs.residual) != 1 {
		t.Fatalf("expected 1 residual, got %d", len(cs.residual))
	}
	if cs.residual[0].Operator() != selection.NotEquals {
		t.Fatalf("expected NotEquals in residual, got %v", cs.residual[0].Operator())
	}
}

func TestClassifySelector_Everything(t *testing.T) {
	cs := classifySelector(labels.Everything())
	if cs.hasPushable {
		t.Fatal("expected no pushable for Everything selector")
	}
	if len(cs.residual) != 0 {
		t.Fatalf("expected no residual, got %d", len(cs.residual))
	}
}

func TestClassifySelector_PrefersEqualityOverIn(t *testing.T) {
	reqIn, _ := labels.NewRequirement("tier", selection.In, []string{"a", "b"})
	reqEq, _ := labels.NewRequirement("app", selection.Equals, []string{"web"})
	sel := labels.NewSelector().Add(*reqIn, *reqEq)

	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable")
	}
	if cs.primary.Operator() != selection.Equals {
		t.Fatalf("expected Equals as primary (most selective), got %v", cs.primary.Operator())
	}
	if len(cs.residual) != 1 {
		t.Fatalf("expected 1 residual, got %d", len(cs.residual))
	}
	if cs.residual[0].Operator() != selection.In {
		t.Fatalf("expected In in residual, got %v", cs.residual[0].Operator())
	}
}

func TestClassifySelector_PrefersInOverExists(t *testing.T) {
	reqExists, _ := labels.NewRequirement("canary", selection.Exists, nil)
	reqIn, _ := labels.NewRequirement("tier", selection.In, []string{"a", "b"})
	sel := labels.NewSelector().Add(*reqExists, *reqIn)

	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable")
	}
	if cs.primary.Operator() != selection.In {
		t.Fatalf("expected In as primary (more selective than Exists), got %v", cs.primary.Operator())
	}
	if len(cs.residual) != 1 {
		t.Fatalf("expected 1 residual, got %d", len(cs.residual))
	}
	if cs.residual[0].Operator() != selection.Exists {
		t.Fatalf("expected Exists in residual, got %v", cs.residual[0].Operator())
	}
}

func TestClassifySelector_PrefersEqualityOverExists(t *testing.T) {
	reqExists, _ := labels.NewRequirement("canary", selection.Exists, nil)
	reqEq, _ := labels.NewRequirement("app", selection.Equals, []string{"web"})
	reqNeg, _ := labels.NewRequirement("tier", selection.NotEquals, []string{"dev"})
	sel := labels.NewSelector().Add(*reqExists, *reqEq, *reqNeg)

	cs := classifySelector(sel)
	if !cs.hasPushable {
		t.Fatal("expected pushable")
	}
	if cs.primary.Operator() != selection.Equals {
		t.Fatalf("expected Equals as primary, got %v", cs.primary.Operator())
	}
	if len(cs.residual) != 2 {
		t.Fatalf("expected 2 residual, got %d", len(cs.residual))
	}
}

func TestPushablePriority(t *testing.T) {
	tests := []struct {
		op       selection.Operator
		expected int
	}{
		{selection.Equals, 3},
		{selection.DoubleEquals, 3},
		{selection.In, 2},
		{selection.Exists, 1},
		{selection.NotEquals, -1},
		{selection.NotIn, -1},
		{selection.DoesNotExist, -1},
	}
	for _, tt := range tests {
		if got := pushablePriority(tt.op); got != tt.expected {
			t.Errorf("pushablePriority(%v) = %d, want %d", tt.op, got, tt.expected)
		}
	}
}

func TestResidualMatches_AllMatch(t *testing.T) {
	req1, _ := labels.NewRequirement("tier", selection.Equals, []string{"frontend"})
	objLabels := map[string]string{"app": "web", "tier": "frontend"}
	if !residualMatches(objLabels, []labels.Requirement{*req1}) {
		t.Fatal("expected residual to match")
	}
}

func TestResidualMatches_NotMatch(t *testing.T) {
	req1, _ := labels.NewRequirement("tier", selection.Equals, []string{"frontend"})
	objLabels := map[string]string{"app": "web", "tier": "backend"}
	if residualMatches(objLabels, []labels.Requirement{*req1}) {
		t.Fatal("expected residual not to match")
	}
}

func TestResidualMatches_EmptyResidual(t *testing.T) {
	if !residualMatches(map[string]string{"app": "web"}, nil) {
		t.Fatal("expected empty residual to always match")
	}
}

func TestResidualMatches_NegativeRequirement(t *testing.T) {
	req1, _ := labels.NewRequirement("tier", selection.NotEquals, []string{"backend"})
	objLabels := map[string]string{"app": "web", "tier": "frontend"}
	if !residualMatches(objLabels, []labels.Requirement{*req1}) {
		t.Fatal("expected negative requirement to match")
	}

	objLabels2 := map[string]string{"app": "web", "tier": "backend"}
	if residualMatches(objLabels2, []labels.Requirement{*req1}) {
		t.Fatal("expected negative requirement to reject backend")
	}
}
