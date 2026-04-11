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

package sanitize

import (
	"reflect"
	"testing"
)

func TestSanitizeMap(t *testing.T) {
	tests := []struct {
		name          string
		fields        map[string]any
		sensitiveKeys []string
		want          map[string]any
	}{
		{
			name:          "sensitive key value is redacted",
			fields:        map[string]any{"password": "s3cret", "host": "db.example.com"},
			sensitiveKeys: []string{"password"},
			want:          map[string]any{"password": "[REDACTED]", "host": "db.example.com"},
		},
		{
			name:          "non-sensitive key value is preserved",
			fields:        map[string]any{"host": "db.example.com", "port": 5432},
			sensitiveKeys: []string{"password"},
			want:          map[string]any{"host": "db.example.com", "port": 5432},
		},
		{
			name: "nested map with sensitive key is redacted",
			fields: map[string]any{
				"config": map[string]any{
					"token": "abc123",
					"url":   "https://api.example.com",
				},
			},
			sensitiveKeys: []string{"token"},
			want: map[string]any{
				"config": map[string]any{
					"token": "[REDACTED]",
					"url":   "https://api.example.com",
				},
			},
		},
		{
			name:          "empty map returns empty map",
			fields:        map[string]any{},
			sensitiveKeys: []string{"password"},
			want:          map[string]any{},
		},
		{
			name:          "nil map returns nil",
			fields:        nil,
			sensitiveKeys: []string{"password"},
			want:          nil,
		},
		{
			name:          "multiple sensitive keys all redacted",
			fields:        map[string]any{"password": "pw", "token": "tk", "host": "h"},
			sensitiveKeys: []string{"password", "token"},
			want:          map[string]any{"password": "[REDACTED]", "token": "[REDACTED]", "host": "h"},
		},
		{
			name:          "case-insensitive key matching",
			fields:        map[string]any{"Password": "pw", "TOKEN": "tk", "Secret": "sc"},
			sensitiveKeys: []string{"password", "token", "secret"},
			want:          map[string]any{"Password": "[REDACTED]", "TOKEN": "[REDACTED]", "Secret": "[REDACTED]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeMap(tt.fields, tt.sensitiveKeys)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SanitizeMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeMap_DoesNotMutateOriginal(t *testing.T) {
	original := map[string]any{"password": "s3cret"}
	_ = SanitizeMap(original, []string{"password"})
	if original["password"] != "s3cret" {
		t.Fatal("SanitizeMap mutated the original map")
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		secrets []string
		want    string
	}{
		{
			name:    "string containing secret is redacted",
			value:   "connection failed with token=abc123 on host",
			secrets: []string{"abc123"},
			want:    "connection failed with token=[REDACTED] on host",
		},
		{
			name:    "string without secret is unchanged",
			value:   "all systems operational",
			secrets: []string{"abc123"},
			want:    "all systems operational",
		},
		{
			name:    "multiple occurrences of same secret all replaced",
			value:   "key=abc123 retry key=abc123",
			secrets: []string{"abc123"},
			want:    "key=[REDACTED] retry key=[REDACTED]",
		},
		{
			name:    "multiple different secrets all replaced",
			value:   "user=admin pass=s3cret token=xyz789",
			secrets: []string{"s3cret", "xyz789"},
			want:    "user=admin pass=[REDACTED] token=[REDACTED]",
		},
		{
			name:    "empty string returns empty",
			value:   "",
			secrets: []string{"secret"},
			want:    "",
		},
		{
			name:    "empty secrets list returns string unchanged",
			value:   "some data",
			secrets: []string{},
			want:    "some data",
		},
		{
			name:    "nil secrets list returns string unchanged",
			value:   "some data",
			secrets: nil,
			want:    "some data",
		},
		{
			name:    "empty string in secrets list is skipped",
			value:   "data",
			secrets: []string{""},
			want:    "data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeString(tt.value, tt.secrets)
			if got != tt.want {
				t.Fatalf("SanitizeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultSensitiveKeys_NotEmpty(t *testing.T) {
	if len(DefaultSensitiveKeys) == 0 {
		t.Fatal("DefaultSensitiveKeys should not be empty")
	}
}
