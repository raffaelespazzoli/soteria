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

package drivers

// VolumeGroupIDFor computes the deterministic VolumeGroupID for a driver.
// Each driver produces IDs from (namespace, name) in a fixed format that
// matches what CreateVolumeGroup would return. This allows callers to derive
// the ID without calling CreateVolumeGroup, enabling read-only resolution
// via GetVolumeGroup.
func VolumeGroupIDFor(driverType, namespace, name string) VolumeGroupID {
	switch driverType {
	case "csi-extension":
		return VolumeGroupID("csi-ext-" + namespace + "/" + name)
	default:
		return VolumeGroupID("noop-" + namespace + "/" + name)
	}
}
