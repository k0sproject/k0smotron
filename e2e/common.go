//go:build e2e

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

package e2e

// Test suite constants for e2e config variables.
const (
	KubernetesVersion                = "KUBERNETES_VERSION"
	KubernetesVersionManagement      = "KUBERNETES_VERSION_MANAGEMENT"
	KubernetesVersionFirstUpgradeTo  = "KUBERNETES_VERSION_FIRST_UPGRADE_TO"
	KubernetesVersionSecondUpgradeTo = "KUBERNETES_VERSION_SECOND_UPGRADE_TO"
	ControlPlaneMachineCount         = "CONTROL_PLANE_MACHINE_COUNT"
	IPFamily                         = "IP_FAMILY"
)
