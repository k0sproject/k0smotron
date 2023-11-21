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
	"k8s.io/apimachinery/pkg/types"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// JoinTokenRequestSpec defines the desired state of K0smotronJoinTokenRequest
type JoinTokenRequestSpec struct {
	// ClusterRef is the reference to the cluster for which the join token is requested.
	ClusterRef ClusterRef `json:"clusterRef"`
	// Expiration time of the token. Format 1.5h, 2h45m or 300ms.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default="0s"
	Expiry string `json:"expiry,omitempty"`
	// Role of the node for which the token is requested (worker or controller).
	//+kubebuilder:validation:Enum=worker;controller
	//+kubebuilder:default=worker
	Role string `json:"role,omitempty"`
}

type ClusterRef struct {
	// Name of the cluster.
	Name string `json:"name"`
	// Namespace of the cluster.
	Namespace string `json:"namespace"`
}

// JoinTokenRequestStatus defines the observed state of K0smotronJoinTokenRequest
type JoinTokenRequestStatus struct {
	ReconciliationStatus string    `json:"reconciliationStatus"`
	TokenID              string    `json:"tokenID,omitempty"`
	ClusterUID           types.UID `json:"clusterUID,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=jtr

// JoinTokenRequest is the Schema for the join token request API
type JoinTokenRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	//+kubebuilder:validation:Optional
	Spec   JoinTokenRequestSpec   `json:"spec,omitempty"`
	Status JoinTokenRequestStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// JoinTokenRequestList contains a list of K0smotronJoinTokenRequest
type JoinTokenRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JoinTokenRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&JoinTokenRequest{}, &JoinTokenRequestList{})
}
