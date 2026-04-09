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

// Package apiserver implements the Soteria extension API server using the
// k8s.io/apiserver generic server framework. It registers the soteria.io/v1alpha1
// API group with DRPlan, DRExecution, and DRGroupStatus resources, each served
// with separate spec and status subresources. The server is wired to a
// ScyllaDB-backed storage.Interface through a custom RESTOptionsGetter that
// optionally wraps storage with the k8s.io/apiserver cacher for in-memory
// caching and watch fan-out. The package also defines CriticalFieldDetectors
// that signal when an update touches a state-machine field requiring cross-DC
// LWT consistency.
package apiserver
