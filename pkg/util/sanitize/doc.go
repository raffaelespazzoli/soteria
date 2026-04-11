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

// Package sanitize provides credential sanitization for log messages, events,
// and metric labels. It ensures no Secret values appear in any orchestrator
// output (NFR14). Sanitization is applied at the formatting boundary, not at
// the storage layer, to catch all output paths.
package sanitize
