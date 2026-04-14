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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestStrategy_NamespaceScoped_ReturnsFalse(t *testing.T) {
	if Strategy.NamespaceScoped() {
		t.Error("DRExecution strategy must be cluster-scoped (NamespaceScoped() == false)")
	}
}

func TestGetAttrs_ReturnsNameField(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "exec-1",
			Labels: map[string]string{"mode": "planned"},
		},
	}

	lbls, flds, err := GetAttrs(exec)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if lbls["mode"] != "planned" {
		t.Errorf("expected label mode=planned, got %v", lbls)
	}

	if flds["metadata.name"] != "exec-1" {
		t.Errorf("expected metadata.name=exec-1, got %q", flds["metadata.name"])
	}
}

func TestGetAttrs_DoesNotIncludeNamespace(t *testing.T) {
	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "exec-1",
			Namespace: "leftover-ns",
		},
	}

	_, flds, err := GetAttrs(exec)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if _, ok := flds["metadata.namespace"]; ok {
		t.Error("cluster-scoped DRExecution GetAttrs must not include metadata.namespace")
	}
}

func TestGetAttrs_WrongType_ReturnsError(t *testing.T) {
	wrong := &soteriav1alpha1.DRPlan{}
	_, _, err := GetAttrs(wrong)
	if err == nil {
		t.Error("GetAttrs should return an error for non-DRExecution objects")
	}
}

func TestMatchDRExecution_UsesGetAttrs(t *testing.T) {
	pred := MatchDRExecution(nil, nil)
	if pred.GetAttrs == nil {
		t.Error("MatchDRExecution predicate must have GetAttrs set")
	}
}
