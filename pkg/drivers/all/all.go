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

// Package all registers every built-in StorageProvider driver via init()
// side-effect imports. Import this package from main.go to activate all
// drivers:
//
//	import _ "github.com/soteria-project/soteria/pkg/drivers/all"
package all

import (
	_ "github.com/soteria-project/soteria/pkg/drivers/noop"
)
