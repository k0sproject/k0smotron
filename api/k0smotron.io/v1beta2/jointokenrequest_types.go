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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// JoinTokenRequestSecretCondition is the condition type used to indicate the status of the join token secret creation.
	JoinTokenRequestSecretCondition = "JoinTokenSecretCreated"
	// JoinTokenRequestSecretCreatedReason is the reason used in the condition when the join token secret has been successfully created.
	JoinTokenRequestSecretCreatedReason = "Created"
	// JoinTokenRequestInternalErrorReason is the reason used in the condition when the join token request controller encounters an internal error
	// while processing the request.
	JoinTokenRequestInternalErrorReason = "InternalError"
	// V1Beta1ClusterRefNamespaceAnnotation is the temporary annotation used to store the namespace of the cluster reference, since in v1beta1 the namespace was part
	// of the spec and in v1beta2 it's not.
	V1Beta1ClusterRefNamespaceAnnotation = "k0smotron.io/cluster-ref-namespace"
)

// JoinTokenRequestSpec defines the desired state of K0smotronJoinTokenRequest
type JoinTokenRequestSpec struct {
	// clusterName is the name of the k0smotron Cluster this object belongs to.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	ClusterName string `json:"clusterName,omitempty"`
	// Expiration time of the token. Format 1.5h, 2h45m or 300ms.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default="0s"
	Expiry string `json:"expiry,omitempty"`
	// Role of the node for which the token is requested (worker or controller).
	//+kubebuilder:validation:Enum=worker;controller
	//+kubebuilder:default=worker
	Role string `json:"role,omitempty"`
}

// ClusterRef is a reference to a cluster for which a join token is requested.
type ClusterRef struct {
	// Name of the cluster.
	Name string `json:"name"`
	// Namespace of the cluster.
	Namespace string `json:"namespace"`
}

// JoinTokenRequestStatus defines the observed state of K0smotronJoinTokenRequest
type JoinTokenRequestStatus struct {
	TokenID string `json:"tokenID,omitempty"`
	// Conditions represents the observations of the k0smotron cluster's state.
	// Known condition types are Available, Deleting.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// deprecated groups all the status fields that are deprecated and will be removed when all the nested field are removed.
	// +optional
	Deprecated *JTRStatusDeprecated `json:"deprecated,omitempty"`
}

// JTRStatusDeprecated defines the observed state of K0smotronJoinTokenRequest for deprecated fields, which will be removed in future versions.
type JTRStatusDeprecated struct {
	// v1beta1 groups all the status fields that are deprecated and will be removed when support for v1beta1 will be dropped.
	// +optional
	V1Beta1 *JTRStatusV1beta1Deprecated `json:"v1beta1,omitempty"`
}

// JTRStatusV1beta1Deprecated defines the observed state of K0smotronJoinTokenRequest for v1beta1, which is deprecated and will be removed in future versions.
type JTRStatusV1beta1Deprecated struct {
	ReconciliationStatus string    `json:"reconciliationStatus,omitempty"`
	ClusterUID           types.UID `json:"clusterUID,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:storageversion
//+kubebuilder:resource:shortName=jtr

// JoinTokenRequest is the Schema for the join token request API
type JoinTokenRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	//+kubebuilder:validation:Optional
	Spec   JoinTokenRequestSpec   `json:"spec,omitempty"`
	Status JoinTokenRequestStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the JoinTokenRequest status.
func (jtr *JoinTokenRequest) GetConditions() []metav1.Condition {
	return jtr.Status.Conditions
}

// SetConditions sets the conditions on the JoinTokenRequest status.
func (jtr *JoinTokenRequest) SetConditions(conditions []metav1.Condition) {
	jtr.Status.Conditions = conditions
}

//+kubebuilder:object:root=true

// JoinTokenRequestList contains a list of K0smotronJoinTokenRequest
type JoinTokenRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JoinTokenRequest `json:"items"`
}

// SetDeprecatedStatus sets the deprecated  status of the JoinTokenRequest.
func (jtr *JoinTokenRequest) SetDeprecatedStatus(reconcileStatus string, clusterUID types.UID) {
	if jtr.Status.Deprecated == nil {
		jtr.Status.Deprecated = &JTRStatusDeprecated{}
	}
	if jtr.Status.Deprecated.V1Beta1 == nil {
		jtr.Status.Deprecated.V1Beta1 = &JTRStatusV1beta1Deprecated{}
	}
	jtr.Status.Deprecated.V1Beta1.ReconciliationStatus = reconcileStatus
	jtr.Status.Deprecated.V1Beta1.ClusterUID = clusterUID
}

// GetDeprecatedReconciliationStatus returns the deprecated reconciliation status of the JoinTokenRequest.
func (jtr *JoinTokenRequest) GetDeprecatedReconciliationStatus() string {
	if jtr.Status.Deprecated != nil && jtr.Status.Deprecated.V1Beta1 != nil {
		return jtr.Status.Deprecated.V1Beta1.ReconciliationStatus
	}
	return ""
}

// GetDeprecatedClusterUUID returns the deprecated cluster UUID of the JoinTokenRequest.
func (jtr *JoinTokenRequest) GetDeprecatedClusterUUID() types.UID {
	if jtr.Status.Deprecated != nil && jtr.Status.Deprecated.V1Beta1 != nil {
		return jtr.Status.Deprecated.V1Beta1.ClusterUID
	}
	return ""
}

func init() {
	SchemeBuilder.Register(&JoinTokenRequest{}, &JoinTokenRequestList{})
}
