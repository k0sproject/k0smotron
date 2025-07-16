package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func init() {
	SchemeBuilder.Register(&RemoteCluster{}, &RemoteClusterList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"

type RemoteCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteClusterSpec   `json:"spec,omitempty"`
	Status RemoteClusterStatus `json:"status,omitempty"`
}

// RemoteClusterSpec defines the desired state of RemoteCluster
type RemoteClusterSpec struct {
	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint"`
}

// RemoteClusterStatus defines the observed state of RemoteCluster
type RemoteClusterStatus struct {
	// Ready denotes that the remote cluster is ready to be used.
	// +kubebuilder:validation:Required
	// +kubebuilder:default=false
	Ready bool `json:"ready"`
}

// +kubebuilder:object:root=true

type RemoteClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteCluster `json:"items"`
}
