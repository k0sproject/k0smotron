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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&RemoteMachineTemplate{}, &RemoteMachineTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"

type RemoteMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RemoteMachineTemplateSpec `json:"spec,omitempty"`
}

type RemoteMachineTemplateSpec struct {
	Template RemoteMachineTemplateResource `json:"template"`
}

type RemoteMachineTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta                 `json:"metadata,omitempty"`
	Spec       RemoteMachineTemplateResourceSpec `json:"spec,omitempty"`
}

type RemoteMachineTemplateResourceSpec struct {
	Pool string `json:"pool"`
	// ProvisionJob describes the kubernetes Job to use to provision the machine.
	ProvisionJob *ProvisionJob `json:"provisionJob,omitempty"`
}

// +kubebuilder:object:root=true

type RemoteMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteMachineTemplate `json:"items"`
}
