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
)

func init() {
	SchemeBuilder.Register(&PodMachineTemplate{}, &PodMachineTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"
// +kubebuilder:storageversion

// PodMachineTemplate is the Schema for the podmachinetemplates API
type PodMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PodMachineTemplateSpec `json:"spec,omitempty"`
}

// PodMachineTemplateSpec defines the desired state of PodMachineTemplate
type PodMachineTemplateSpec struct {
	Template PodMachineTemplateResource `json:"template"`
}

// PodMachineTemplateResource describes the data needed to create a PodMachine from a template
type PodMachineTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta              `json:"metadata,omitempty"`
	Spec       PodMachineTemplateResourceSpec `json:"spec,omitempty"`
}

// PodMachineTemplateResourceSpec defines the desired state of PodMachineTemplateResource
type PodMachineTemplateResourceSpec struct {
	// PodTemplate specifies pod template for the machine.
	// This defines the container configuration, volumes, and other pod settings.
	// +kubebuilder:validation:Required
	PodTemplate corev1.PodTemplateSpec `json:"podTemplate"`
}

// +kubebuilder:object:root=true

// PodMachineTemplateList contains a list of PodMachineTemplate
type PodMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodMachineTemplate `json:"items"`
}
