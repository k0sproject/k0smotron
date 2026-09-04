/*
Copyright 2026.

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

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func init() {
	SchemeBuilder.Register(&PodCluster{}, &PodClusterList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"
// +kubebuilder:storageversion

// PodCluster is the Schema for the podclusters API
type PodCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodClusterSpec   `json:"spec,omitempty"`
	Status PodClusterStatus `json:"status,omitempty"`
}

// PodClusterSpec defines the desired state of PodCluster
type PodClusterSpec struct {
	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint"`
}

// PodClusterStatus defines the observed state of PodCluster
type PodClusterStatus struct {
	// initialization provides observations of the PodCluster initialization process.
	// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate initial Cluster provisioning.
	// +optional
	Initialization PodClusterInitializationStatus `json:"initialization,omitempty,omitzero"`
	// conditions contains the conditions of the PodCluster, which represent the current state of the cluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PodClusterInitializationStatus provides observations of the PodCluster initialization process.
// +kubebuilder:validation:MinProperties=1
type PodClusterInitializationStatus struct {
	// provisioned is true when the infrastructure provider reports that the Cluster's infrastructure is fully provisioned.
	// NOTE: this field is part of the Cluster API contract, and it is used to orchestrate initial Cluster provisioning.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (pc *PodCluster) GetConditions() []metav1.Condition {
	return pc.Status.Conditions
}

// SetConditions sets the conditions on the PodCluster status.
func (pc *PodCluster) SetConditions(conditions []metav1.Condition) {
	pc.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// PodClusterList contains a list of PodCluster
type PodClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodCluster `json:"items"`
}
