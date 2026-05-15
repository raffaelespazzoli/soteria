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

package csiextension

import (
	"context"
	"encoding/json"
	"testing"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNew_WithClient(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	drv := New(c)

	if drv == nil {
		t.Fatal("New() returned nil")
	}
	if drv.client == nil {
		t.Fatal("New() did not set client field")
	}
}

func TestNew_NilClient(t *testing.T) {
	drv := New(nil)
	if drv == nil {
		t.Fatal("New(nil) returned nil — should return a Driver even with nil client")
	}
}

func TestReplicationStateConstants(t *testing.T) {
	tests := []struct {
		name    string
		got     replicationv1alpha1.ReplicationState
		wantVal string
	}{
		{"Primary", ReplicationStatePrimary, "primary"},
		{"Secondary", ReplicationStateSecondary, "secondary"},
		{"Resync", ReplicationStateResync, "resync"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.wantVal {
				t.Errorf("ReplicationState%s = %q, want %q", tt.name, tt.got, tt.wantVal)
			}
		})
	}
}

func TestCRDTypes_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name          string
		obj           runtime.Object
		wantName      string
		wantState     replicationv1alpha1.ReplicationState
		classJSONKey  string
		wantClassName string
	}{
		{
			name: "VolumeReplication",
			obj: &replicationv1alpha1.VolumeReplication{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "replication.storage.openshift.io/v1alpha1",
					Kind:       "VolumeReplication",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vr",
					Namespace: "default",
				},
				Spec: replicationv1alpha1.VolumeReplicationSpec{
					VolumeReplicationClass: "ceph-rbd-replication",
					ReplicationState:       replicationv1alpha1.Primary,
				},
			},
			wantName:      "test-vr",
			wantState:     replicationv1alpha1.Primary,
			classJSONKey:  "volumeReplicationClass",
			wantClassName: "ceph-rbd-replication",
		},
		{
			name: "VolumeGroupReplication",
			obj: &replicationv1alpha1.VolumeGroupReplication{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "replication.storage.openshift.io/v1alpha1",
					Kind:       "VolumeGroupReplication",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vgr",
					Namespace: "default",
				},
				Spec: replicationv1alpha1.VolumeGroupReplicationSpec{
					VolumeGroupReplicationClassName: "ceph-rbd-group-replication",
					ReplicationState:                replicationv1alpha1.Secondary,
				},
			},
			wantName:      "test-vgr",
			wantState:     replicationv1alpha1.Secondary,
			classJSONKey:  "volumeGroupReplicationClassName",
			wantClassName: "ceph-rbd-group-replication",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.obj)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map: %v", err)
			}

			meta, _ := raw["metadata"].(map[string]any)
			if got := meta["name"]; got != tt.wantName {
				t.Errorf("metadata.name = %v, want %q", got, tt.wantName)
			}

			spec, _ := raw["spec"].(map[string]any)
			if got := spec["replicationState"]; got != string(tt.wantState) {
				t.Errorf("spec.replicationState = %v, want %q", got, tt.wantState)
			}
			if got := spec[tt.classJSONKey]; got != tt.wantClassName {
				t.Errorf("spec.%s = %v, want %q", tt.classJSONKey, got, tt.wantClassName)
			}
		})
	}
}

func TestSchemeRegistration_VolumeReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}

	gvks, _, err := scheme.ObjectKinds(&replicationv1alpha1.VolumeReplication{})
	if err != nil {
		t.Fatalf("ObjectKinds(VolumeReplication) error: %v", err)
	}
	if len(gvks) == 0 {
		t.Fatal("VolumeReplication not registered in scheme")
	}

	found := false
	for _, gvk := range gvks {
		if gvk.Kind == "VolumeReplication" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected kind VolumeReplication in scheme, got %v", gvks)
	}
}

func TestSchemeRegistration_VolumeGroupReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}

	gvks, _, err := scheme.ObjectKinds(&replicationv1alpha1.VolumeGroupReplication{})
	if err != nil {
		t.Fatalf("ObjectKinds(VolumeGroupReplication) error: %v", err)
	}
	if len(gvks) == 0 {
		t.Fatal("VolumeGroupReplication not registered in scheme")
	}

	found := false
	for _, gvk := range gvks {
		if gvk.Kind == "VolumeGroupReplication" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected kind VolumeGroupReplication in scheme, got %v", gvks)
	}
}

func TestFakeClient_CRUD_VolumeReplication(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := replicationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	drv := New(c)
	ctx := context.Background()

	vr := &replicationv1alpha1.VolumeReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "crud-vr",
			Namespace: "default",
		},
		Spec: replicationv1alpha1.VolumeReplicationSpec{
			VolumeReplicationClass: "test-class",
			ReplicationState:       replicationv1alpha1.Primary,
		},
	}

	if err := drv.client.Create(ctx, vr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var got replicationv1alpha1.VolumeReplication
	if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.ReplicationState != replicationv1alpha1.Primary {
		t.Errorf("Get replicationState = %q, want %q", got.Spec.ReplicationState, replicationv1alpha1.Primary)
	}

	got.Spec.ReplicationState = replicationv1alpha1.Secondary
	if err := drv.client.Update(ctx, &got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var updated replicationv1alpha1.VolumeReplication
	if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &updated); err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if updated.Spec.ReplicationState != replicationv1alpha1.Secondary {
		t.Errorf("Updated replicationState = %q, want %q", updated.Spec.ReplicationState, replicationv1alpha1.Secondary)
	}

	if err := drv.client.Delete(ctx, &updated); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var deleted replicationv1alpha1.VolumeReplication
	if err := drv.client.Get(ctx, client.ObjectKeyFromObject(vr), &deleted); err == nil {
		t.Fatal("Get after Delete: expected NotFound error, got nil")
	}
}

func TestVolumeReplicationClassLabel(t *testing.T) {
	if VolumeReplicationClassLabel == "" {
		t.Fatal("VolumeReplicationClassLabel must not be empty")
	}
}

func TestDriverName(t *testing.T) {
	if DriverName != "csi-extension" {
		t.Errorf("DriverName = %q, want %q", DriverName, "csi-extension")
	}
}
