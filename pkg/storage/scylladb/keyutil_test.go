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
)

func TestKeyToComponents_NamespaceScoped(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected KeyComponents
	}{
		{
			name: "DRPlan",
			key:  "/soteria.io/drplans/default/erp-full-stack",
			expected: KeyComponents{
				APIGroup: "soteria.io", ResourceType: "drplans",
				Namespace: "default", Name: "erp-full-stack",
			},
		},
		{
			name: "DRExecution",
			key:  "/soteria.io/drexecutions/production/exec-001",
			expected: KeyComponents{
				APIGroup: "soteria.io", ResourceType: "drexecutions",
				Namespace: "production", Name: "exec-001",
			},
		},
		{
			name: "DRGroupStatus",
			key:  "/soteria.io/drgroupstatuses/default/exec-001-wave0-group0",
			expected: KeyComponents{
				APIGroup: "soteria.io", ResourceType: "drgroupstatuses",
				Namespace: "default", Name: "exec-001-wave0-group0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := KeyToComponents(tt.key)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Fatalf("got %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func TestKeyToComponents_ClusterScoped(t *testing.T) {
	kc, err := KeyToComponents("/soteria.io/drplans/my-plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kc.APIGroup != "soteria.io" || kc.ResourceType != "drplans" || kc.Namespace != "" || kc.Name != "my-plan" {
		t.Fatalf("unexpected result: %+v", kc)
	}
}

func TestKeyToComponents_Invalid(t *testing.T) {
	invalid := []string{
		"/",
		"/soteria.io",
		"soteria.io",
		"",
	}
	for _, key := range invalid {
		t.Run("key="+key, func(t *testing.T) {
			_, err := KeyToComponents(key)
			if err == nil {
				t.Fatal("expected error for invalid key")
			}
		})
	}
}

func TestKeyPrefixToComponents(t *testing.T) {
	tests := []struct {
		name                        string
		key                         string
		wantGroup, wantType, wantNS string
	}{
		{
			name:      "all namespaces with trailing slash",
			key:       "/soteria.io/drplans/",
			wantGroup: "soteria.io", wantType: "drplans", wantNS: "",
		},
		{
			name:      "all namespaces without trailing slash",
			key:       "/soteria.io/drplans",
			wantGroup: "soteria.io", wantType: "drplans", wantNS: "",
		},
		{
			name:      "single namespace with trailing slash",
			key:       "/soteria.io/drplans/default/",
			wantGroup: "soteria.io", wantType: "drplans", wantNS: "default",
		},
		{
			name:      "single namespace without trailing slash",
			key:       "/soteria.io/drplans/default",
			wantGroup: "soteria.io", wantType: "drplans", wantNS: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, rt, ns, err := KeyPrefixToComponents(tt.key)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if g != tt.wantGroup || rt != tt.wantType || ns != tt.wantNS {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", g, rt, ns, tt.wantGroup, tt.wantType, tt.wantNS)
			}
		})
	}
}

func TestKeyPrefixToComponents_Invalid(t *testing.T) {
	invalid := []string{"/", "/soteria.io", ""}
	for _, key := range invalid {
		t.Run("key="+key, func(t *testing.T) {
			_, _, _, err := KeyPrefixToComponents(key)
			if err == nil {
				t.Fatal("expected error for invalid prefix")
			}
		})
	}
}

func TestComponentsToKey_NamespaceScoped(t *testing.T) {
	key := ComponentsToKey("soteria.io", "drplans", "default", "my-plan")
	expected := "/soteria.io/drplans/default/my-plan"
	if key != expected {
		t.Fatalf("got %q, want %q", key, expected)
	}
}

func TestComponentsToKey_ClusterScoped(t *testing.T) {
	key := ComponentsToKey("soteria.io", "drplans", "", "my-plan")
	expected := "/soteria.io/drplans/my-plan"
	if key != expected {
		t.Fatalf("got %q, want %q", key, expected)
	}
}

func TestKeyToComponents_Roundtrip(t *testing.T) {
	original := "/soteria.io/drplans/production/erp-full-stack"
	kc, err := KeyToComponents(original)
	if err != nil {
		t.Fatalf("KeyToComponents failed: %v", err)
	}

	reconstructed := ComponentsToKey(kc.APIGroup, kc.ResourceType, kc.Namespace, kc.Name)
	if reconstructed != original {
		t.Fatalf("roundtrip failed: got %q, want %q", reconstructed, original)
	}
}
