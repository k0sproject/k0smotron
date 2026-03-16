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

const (
	// ControlPlaneAvailableReason documents the fact that the control plane is reachable.
	ControlPlaneAvailableReason = "Available"

	// K0smotronClusterReconciledCondition documents the reconciliation status of the
	// k0smotron.io/v1beta1 Cluster that manages the generated resources.
	K0smotronClusterReconciledCondition = "K0smotronClusterReconciled"

	// ReconciliationSucceededReason documents that the k0smotron Cluster reconciled successfully.
	ReconciliationSucceededReason = "ReconciliationSucceeded"

	// ReconciliationFailedReason documents that the k0smotron Cluster encountered a reconciliation error.
	ReconciliationFailedReason = "ReconciliationFailed"

	// ReconciliationInProgressReason documents that the k0smotron Cluster is not yet fully reconciled.
	ReconciliationInProgressReason = "ReconciliationInProgress"
)
