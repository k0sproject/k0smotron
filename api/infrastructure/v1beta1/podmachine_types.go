/*
Copyright 2023.

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

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func init() {
	SchemeBuilder.Register(&PodMachine{}, &PodMachineList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"

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
	// Ready denotes that the pod machine is ready to be used.
	// +kubebuilder:validation:Optional
	Ready bool `json:"ready,omitempty"`

	// Addresses contains the associated addresses for the machine.
	// +optional
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`

	// FailureReason indicates the reason for any failure that occurred.
	// +kubebuilder:validation:Optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage provides detailed information about any failure that occurred.
	// +kubebuilder:validation:Optional
	FailureMessage string `json:"failureMessage,omitempty"`

	// PodRef is a reference to the created pod.
	// +kubebuilder:validation:Optional
	PodRef *corev1.ObjectReference `json:"podRef,omitempty"`
}

// +kubebuilder:object:root=true

type PodMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodMachine `json:"items"`
}
