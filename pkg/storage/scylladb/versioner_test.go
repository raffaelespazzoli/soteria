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
	"strconv"
	"testing"
	"time"

	"github.com/gocql/gocql"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeObject implements runtime.Object for testing the versioner.
type fakeObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

func (f *fakeObject) DeepCopyObject() runtime.Object {
	clone := *f
	return &clone
}

func (f *fakeObject) GetObjectKind() schema.ObjectKind { return &f.TypeMeta }

func TestTimeuuidToResourceVersion_Roundtrip(t *testing.T) {
	before := time.Now()
	id := gocql.TimeUUID()
	after := time.Now()

	rv := TimeuuidToResourceVersion(id)

	if rv < uint64(before.UnixMicro()) || rv > uint64(after.UnixMicro()) {
		t.Fatalf("resourceVersion %d outside expected range [%d, %d]", rv, before.UnixMicro(), after.UnixMicro())
	}
}

func TestTimeuuidToResourceVersion_Monotonicity(t *testing.T) {
	const count = 100
	prev := uint64(0)
	for range count {
		id := gocql.TimeUUID()
		rv := TimeuuidToResourceVersion(id)
		if rv < prev {
			t.Fatalf("resourceVersion not monotonically increasing: %d < %d", rv, prev)
		}
		prev = rv
	}
}

func TestResourceVersionToMinTimeuuid_Roundtrip(t *testing.T) {
	id := gocql.TimeUUID()
	rv := TimeuuidToResourceVersion(id)

	reconstructed := ResourceVersionToMinTimeuuid(rv)
	originalTime := id.Time()

	if reconstructed.UnixMicro() != originalTime.UnixMicro() {
		t.Fatalf("roundtrip failed: got %d, want %d", reconstructed.UnixMicro(), originalTime.UnixMicro())
	}
}

func TestResourceVersionToMinTimeuuid_ZeroValue(t *testing.T) {
	result := ResourceVersionToMinTimeuuid(0)
	if result.UnixMicro() != 0 {
		t.Fatalf("expected Unix epoch, got %v", result)
	}
}

func TestNewVersioner_UpdateObject(t *testing.T) {
	v := NewVersioner()
	obj := &fakeObject{}

	if err := v.UpdateObject(obj, 12345); err != nil {
		t.Fatalf("UpdateObject failed: %v", err)
	}
	if obj.ResourceVersion != "12345" {
		t.Fatalf("expected resourceVersion '12345', got %q", obj.ResourceVersion)
	}
}

func TestNewVersioner_ObjectResourceVersion(t *testing.T) {
	v := NewVersioner()
	obj := &fakeObject{}
	obj.ResourceVersion = "67890"

	rv, err := v.ObjectResourceVersion(obj)
	if err != nil {
		t.Fatalf("ObjectResourceVersion failed: %v", err)
	}
	if rv != 67890 {
		t.Fatalf("expected 67890, got %d", rv)
	}
}

func TestNewVersioner_PrepareObjectForStorage(t *testing.T) {
	v := NewVersioner()
	obj := &fakeObject{}
	obj.ResourceVersion = "12345"

	if err := v.PrepareObjectForStorage(obj); err != nil {
		t.Fatalf("PrepareObjectForStorage failed: %v", err)
	}
	if obj.ResourceVersion != "" {
		t.Fatalf("expected empty resourceVersion, got %q", obj.ResourceVersion)
	}
}

func TestNewVersioner_ParseResourceVersion(t *testing.T) {
	v := NewVersioner()

	tests := []struct {
		input    string
		expected uint64
		wantErr  bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"12345", 12345, false},
		{strconv.FormatUint(^uint64(0), 10), ^uint64(0), false},
		{"not-a-number", 0, true},
		{"-1", 0, true},
	}

	for _, tt := range tests {
		t.Run("input="+tt.input, func(t *testing.T) {
			rv, err := v.ParseResourceVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rv != tt.expected {
				t.Fatalf("expected %d, got %d", tt.expected, rv)
			}
		})
	}
}
