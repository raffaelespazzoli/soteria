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
	"fmt"
	"strings"
)

// KeyComponents holds the parsed parts of a storage key that map directly to
// the kv_store primary key columns.
type KeyComponents struct {
	APIGroup     string
	ResourceType string
	Namespace    string
	Name         string
}

// KeyToComponents parses a full storage key into its constituent parts.
//
// Expected formats:
//
//	/soteria.io/drplans/default/my-plan  → (soteria.io, drplans, default, my-plan)
//	/soteria.io/drplans/my-plan          → (soteria.io, drplans, "", my-plan) — cluster-scoped
func KeyToComponents(key string) (KeyComponents, error) {
	trimmed := strings.TrimPrefix(key, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")

	parts := strings.SplitN(trimmed, "/", 4)
	switch len(parts) {
	case 4:
		return KeyComponents{
			APIGroup:     parts[0],
			ResourceType: parts[1],
			Namespace:    parts[2],
			Name:         parts[3],
		}, nil
	case 3:
		return KeyComponents{
			APIGroup:     parts[0],
			ResourceType: parts[1],
			Name:         parts[2],
		}, nil
	default:
		return KeyComponents{}, fmt.Errorf("invalid storage key %q: expected 3-4 path components", key)
	}
}

// KeyPrefixToComponents parses a storage key prefix used by list operations.
//
// Expected formats:
//
//	/soteria.io/drplans/          → (soteria.io, drplans, "")   — all namespaces
//	/soteria.io/drplans/default/  → (soteria.io, drplans, default) — single namespace
func KeyPrefixToComponents(key string) (apiGroup, resourceType, namespace string, err error) {
	trimmed := strings.TrimPrefix(key, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")

	parts := strings.Split(trimmed, "/")
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2], nil
	case 2:
		return parts[0], parts[1], "", nil
	default:
		return "", "", "", fmt.Errorf("invalid key prefix %q: expected 2-3 path components", key)
	}
}

// ComponentsToKey constructs a storage key from its individual components.
func ComponentsToKey(apiGroup, resourceType, namespace, name string) string {
	if namespace == "" {
		return "/" + apiGroup + "/" + resourceType + "/" + name
	}
	return "/" + apiGroup + "/" + resourceType + "/" + namespace + "/" + name
}
