package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func init() {
	SchemeBuilder.Register(&RemoteClusterTemplate{}, &RemoteClusterTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=remoteclustertemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"

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
