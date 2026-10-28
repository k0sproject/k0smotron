/*
Copyright 2025.

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

package featuregate

const (
	// CloudInitVars enables storing k0smotron-generated command and files in cloud-init variables
	CloudInitVars Feature = "CloudInitVars"
	// InPlaceUpdates enables in-place updates of the Kuebernetes nodes
	InPlaceUpdates Feature = "InPlaceUpdates"
	// InPlaceVersionWithDeleteFirst is an experimental feature that combines the InPlace and RecreateDeleteFirst
	// update strategies of K0sControlPlane: k0s version-only changes are applied in place and any other change
	// that replaces a machine is done following the current RecreateDeleteFirst strategy. It applies to both
	// strategies and may be removed once the control plane API can express this combination.
	// Ref: https://github.com/k0sproject/k0smotron/issues/1731
	InPlaceVersionWithDeleteFirst Feature = "InPlaceVersionWithDeleteFirst"
)

var effectiveFeatureGates *FeatureGates

var defaultFeatureMap = map[Feature]FeatureGate{
	CloudInitVars:                 {Enabled: false},
	InPlaceUpdates:                {Enabled: false},
	InPlaceVersionWithDeleteFirst: {Enabled: false},
}
