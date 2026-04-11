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

import "strings"

/*
Output Sanitization

This module provides credential sanitization for log messages, events, and
metric labels. It ensures no Secret values appear in any orchestrator output
(NFR14). Sanitization is applied at the formatting boundary, not at the storage
layer, to catch all output paths including error messages from external libraries.
*/

const redacted = "[REDACTED]"

// DefaultSensitiveKeys lists field names whose values should be redacted in
// structured log output.
var DefaultSensitiveKeys = []string{
	"password",
	"token",
	"secret",
	"credential",
	"key",
	"cert",
	"ca-data",
	"client-certificate-data",
	"client-key-data",
}

// SanitizeMap returns a shallow copy of fields with values replaced by
// "[REDACTED]" for any key that case-insensitively matches a sensitiveKeys
// entry. Nested maps are sanitized recursively.
func SanitizeMap(fields map[string]any, sensitiveKeys []string) map[string]any {
	if fields == nil {
		return nil
	}

	lookup := make(map[string]struct{}, len(sensitiveKeys))
	for _, k := range sensitiveKeys {
		lookup[strings.ToLower(k)] = struct{}{}
	}

	return sanitizeMapInner(fields, lookup)
}

func sanitizeMapInner(fields map[string]any, lookup map[string]struct{}) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if _, sensitive := lookup[strings.ToLower(k)]; sensitive {
			out[k] = redacted
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = sanitizeMapInner(nested, lookup)
			continue
		}
		out[k] = v
	}
	return out
}

// SanitizeString replaces every occurrence of each secret in the value with
// "[REDACTED]". This catches credential values that may appear in error
// messages from external libraries.
//
// Sanitization uses string replacement rather than encryption because the goal
// is preventing accidental exposure in human-readable output; the original
// credentials remain accessible only through the external Secret/Vault
// reference path.
func SanitizeString(value string, secrets []string) string {
	for _, s := range secrets {
		if s == "" {
			continue
		}
		value = strings.ReplaceAll(value, s, redacted)
	}
	return value
}
