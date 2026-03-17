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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func init() {
	SchemeBuilder.Register(&K0sConfigTemplate{}, &K0sConfigTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:deprecatedversion
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=bootstrap-k0smotron"

// K0sConfigTemplate is the Schema for the k0sconfigtemplates API
type K0sConfigTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0sConfigTemplateSpec `json:"spec,omitempty"`
}

// K0sConfigTemplateSpec defines the desired state of K0sConfigTemplate
type K0sConfigTemplateSpec struct {
	Template K0sConfigTemplateResource `json:"template,omitempty"`
}

// K0sConfigTemplateResource defines the template for the config resource
type K0sConfigTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec       K0sConfigSpec     `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// K0sConfigTemplateList contains a list of K0sConfigTemplate
type K0sConfigTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sConfigTemplate `json:"items"`
}
