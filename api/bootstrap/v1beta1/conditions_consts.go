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

package v1beta1

import clusterv2 "sigs.k8s.io/cluster-api/api/core/v1beta2"

// Conditions and condition Reasons for the K0sControllerConfig and K0sWorkerConfig objects
// FROM: https://github.com/kubernetes-sigs/cluster-api/blob/main/bootstrap/kubeadm/api/v1beta1/condition_consts.go

const (
	// DataSecretAvailableCondition documents the status of the bootstrap secret generation process.
	//
	// NOTE: When the DataSecret generation starts the process completes immediately and within the
	// same reconciliation, so the user will always see a transition from Wait to Generated without having
	// evidence that BootstrapSecret generation is started/in progress.
	DataSecretAvailableCondition clusterv2.ConditionType = "DataSecretAvailable"

	// DataSecretGenerationFailedReason (Severity=Warning) documents a BootstrapConfig controller detecting
	// an error while generating a data secret; those kind of errors are usually due to misconfigurations
	// and user intervention is required to get them fixed.
	DataSecretGenerationFailedReason = "DataSecretGenerationFailed"

	// WaitingForControlPlaneInitializationReason (Severity=Info) documents a bootstrap secret generation process
	// waiting for the control plane to be initialized.
	//
	// NOTE: This is a pre-condition for starting to create worker machines.
	WaitingForControlPlaneInitializationReason = "WaitingForControlPlaneInitialization"

	// WaitingForInfrastructureInitializationReason (Severity=Info) documents a bootstrap secret generation process
	// waiting for the infrastructure to be initialized.
	//
	// NOTE: This is a pre-condition for starting to create controller machines.
	WaitingForInfrastructureInitializationReason = "WaitingForControlPlaneInitialization"
)
