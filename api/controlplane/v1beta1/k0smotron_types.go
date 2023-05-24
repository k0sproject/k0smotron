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
	kmapi "github.com/k0sproject/k0smotron/api/k0smotron/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const K0smotronControlPlaneFinalizer = "k0smotron.controlplane.cluster.x-k8s.io"

func init() {
	SchemeBuilder.Register(&K0smotronControlPlane{}, &K0smotronControlPlaneList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"

type K0smotronControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              kmapi.ClusterSpec `json:"spec,omitempty"`

	Status K0smotronControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type K0smotronControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0smotronControlPlane `json:"items"`
}

type K0smotronControlPlaneStatus struct {
	// Ready denotes that the control plane is ready
	Ready                       bool `json:"ready"`
	ControlPlaneReady           bool `json:"controlPlaneReady"`
	Inititalized                bool `json:"initialized"`
	ExternalManagedControlPlane bool `json:"externalManagedControlPlane"`
}
