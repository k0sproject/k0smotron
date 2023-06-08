/*


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
	SchemeBuilder.Register(&K0sWorkerConfig{}, &K0sWorkerConfigList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"

type K0sWorkerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0sWorkerConfigSpec   `json:"spec,omitempty"`
	Status K0sWorkerConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type K0sWorkerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sWorkerConfig `json:"items"`
}

type K0sWorkerConfigSpec struct {
	// JoinTokenSecretRef is a reference to a secret that contains the join token
	// +kubebuilder:validation:Required
	JoinTokenSecretRef *JoinTokenSecretRef `json:"joinTokenSecretRef,omitempty"`
}

type JoinTokenSecretRef struct {
	// Name is the name of the secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Key is the key in the secret that contains the join token
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

type K0sWorkerConfigStatus struct {
	// Ready indicates the Bootstrapdata field is ready to be consumed
	Ready bool `json:"ready,omitempty"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`
	// TODO Conditions etc
}
