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

package drplan

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func TestStrategy_NamespaceScoped_ReturnsFalse(t *testing.T) {
	if Strategy.NamespaceScoped() {
		t.Error("DRPlan strategy must be cluster-scoped (NamespaceScoped() == false)")
	}
}

func TestGetAttrs_ReturnsNameField(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-plan",
			Labels: map[string]string{"tier": "frontend"},
		},
	}

	lbls, flds, err := GetAttrs(plan)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if lbls["tier"] != "frontend" {
		t.Errorf("expected label tier=frontend, got %v", lbls)
	}

	if flds["metadata.name"] != "my-plan" {
		t.Errorf("expected metadata.name=my-plan, got %q", flds["metadata.name"])
	}
}

func TestGetAttrs_DoesNotIncludeNamespace(t *testing.T) {
	plan := &soteriav1alpha1.DRPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-plan",
			Namespace: "leftover-ns",
		},
	}

	_, flds, err := GetAttrs(plan)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}

	if _, ok := flds["metadata.namespace"]; ok {
		t.Error("cluster-scoped DRPlan GetAttrs must not include metadata.namespace")
	}
}

func TestGetAttrs_WrongType_ReturnsError(t *testing.T) {
	wrong := &soteriav1alpha1.DRExecution{}
	_, _, err := GetAttrs(wrong)
	if err == nil {
		t.Error("GetAttrs should return an error for non-DRPlan objects")
	}
}

func TestMatchDRPlan_UsesGetAttrs(t *testing.T) {
	pred := MatchDRPlan(nil, nil)
	if pred.GetAttrs == nil {
		t.Error("MatchDRPlan predicate must have GetAttrs set")
	}
}
