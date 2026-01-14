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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func init() {
	SchemeBuilder.Register(&PodMachine{}, &PodMachineList{})
}

const (
	// PodMachineReadyCondition is the condition type that indicates whether the PodMachine is ready.
	PodMachineReadyCondition = "Ready"
	// PodMachineReadyReason is the reason used when the PodMachine's pod is running and ready.
	PodMachineReadyReason = "Ready"
	// PodMachineNotReadyReason is the reason used when the PodMachine's pod is not yet ready.
	PodMachineNotReadyReason = "NotReady"
)

// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"
// +kubebuilder:storageversion

// PodMachine is the Schema for the podmachines API
type PodMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodMachineSpec   `json:"spec,omitempty"`
	Status PodMachineStatus `json:"status,omitempty"`
}

// PodMachineSpec defines the desired state of PodMachine
type PodMachineSpec struct {
	// ProviderID is the ID of the machine in the provider.
	// +kubebuilder:validation:Optional
	ProviderID string `json:"providerID,omitempty"`

	// PodSpec is the pod specification to use for creating the machine pod.
	// This defines the container configuration, volumes, and other pod settings.
	// +kubebuilder:validation:Required
	PodTemplate corev1.PodTemplateSpec `json:"podTemplate"`
}

// PodMachineStatus defines the observed state of PodMachine
type PodMachineStatus struct {
	// initialization provides observations of the PodMachine initialization process.
	// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate initial Machine provisioning.
	// +optional
	Initialization PodMachineInitializationStatus `json:"initialization,omitempty,omitzero"`

	// addresses contains the associated addresses for the machine.
	// +optional
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`

	// conditions contains the conditions of the PodMachine, which represent the current state of the machine.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// podRef is a reference to the created pod.
	// +optional
	PodRef *corev1.ObjectReference `json:"podRef,omitempty"`
}

// PodMachineInitializationStatus provides observations of the PodMachine initialization process.
// +kubebuilder:validation:MinProperties=1
type PodMachineInitializationStatus struct {
	// provisioned is true when the PodMachine's infrastructure is fully provisioned.
	// NOTE: this field is part of the Cluster API contract, and it is used to orchestrate initial Machine provisioning.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (pm *PodMachine) GetConditions() []metav1.Condition {
	return pm.Status.Conditions
}

// SetConditions sets the conditions on the PodMachine status.
func (pm *PodMachine) SetConditions(conditions []metav1.Condition) {
	pm.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// PodMachineList contains a list of PodMachine
type PodMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodMachine `json:"items"`
}
