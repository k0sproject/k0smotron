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
package v1beta1

import (
	kmapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&K0smotronControlPlaneTemplate{}, &K0smotronControlPlaneTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"

// K0smotronControlPlaneTemplate is the Schema for the k0smotroncontrolplanetemplates API
type K0smotronControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0smotronControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// K0smotronControlPlaneTemplateSpec defines the desired state of K0smotronControlPlaneTemplate
type K0smotronControlPlaneTemplateSpec struct {
	Template K0smotronControlPlaneTemplateResource `json:"template,omitempty"`
}

// K0smotronControlPlaneTemplateResource defines the template for the control plane resource
type K0smotronControlPlaneTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec       kmapi.ClusterSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// K0smotronControlPlaneTemplateList contains a list of K0smotronControlPlaneTemplate
type K0smotronControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0smotronControlPlaneTemplate `json:"items"`
}
