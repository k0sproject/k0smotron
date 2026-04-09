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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func init() {
	SchemeBuilder.Register(&RemoteClusterTemplate{}, &RemoteClusterTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=remoteclustertemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"
// +kubebuilder:deprecatedversion

// RemoteClusterTemplate is the Schema for the remoteclustertemplates API.
type RemoteClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RemoteClusterTemplateSpec `json:"spec,omitempty"`
}

// RemoteClusterTemplateSpec defines the desired state of RemoteClusterTemplate.
type RemoteClusterTemplateSpec struct {
	Template RemoteClusterTemplateResource `json:"template"`
}

// RemoteClusterTemplateResource describes the data needed to create a RemoteCluster from a template.
type RemoteClusterTemplateResource struct {
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`
	Spec       RemoteClusterSpec    `json:"spec"`
}

// +kubebuilder:object:root=true

// RemoteClusterTemplateList contains a list of RemoteClusterTemplate.
type RemoteClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteClusterTemplate `json:"items"`
}
