package v1beta2

import (
	kmapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&K0smotronControlPlaneTemplate{}, &K0smotronControlPlaneTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:storageversion
// +kubebuilder:conversion-gen=./api/controlplane/v1beta2

// K0smotronControlPlaneTemplate is the Schema for the K0smotronControlPlaneTemplates API.
type K0smotronControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0smotronControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// K0smotronControlPlaneTemplateSpec defines the desired state of K0smotronControlPlaneTemplate.
type K0smotronControlPlaneTemplateSpec struct {
	Template K0smotronControlPlaneTemplateResource `json:"template,omitempty"`
}

// K0smotronControlPlaneTemplateResource describes the data needed to create a K0smotronControlPlane from a template.
type K0smotronControlPlaneTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec       kmapi.ClusterSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// K0smotronControlPlaneTemplateList contains a list of K0smotronControlPlaneTemplate.
type K0smotronControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0smotronControlPlaneTemplate `json:"items"`
}
