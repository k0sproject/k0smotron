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

// K0smotronClusterSpec defines the desired state of K0smotronCluster
type K0smotronClusterSpec struct {
	// K0sVersion defines the k0s version to be deployed. If empty k0smotron
	// will pick it automatically.
	K0sVersion string `json:"namespace,omitempty"`
}

// K0smotronClusterStatus defines the observed state of K0smotronCluster
type K0smotronClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// K0smotronCluster is the Schema for the k0smotronclusters API
type K0smotronCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0smotronClusterSpec   `json:"spec,omitempty"`
	Status K0smotronClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// K0smotronClusterList contains a list of K0smotronCluster
type K0smotronClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0smotronCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&K0smotronCluster{}, &K0smotronClusterList{})
}
