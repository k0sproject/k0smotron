package v1beta1

import (
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&K0sControlPlaneTemplate{}, &K0sControlPlaneTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"

type K0sControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0sControlPlaneTemplateSpec `json:"spec,omitempty"`
}

type K0sControlPlaneTemplateSpec struct {
	Template K0sControlPlaneTemplateResource `json:"template,omitempty"`
}

type K0sControlPlaneTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta                   `json:"metadata,omitempty"`
	Spec       K0sControlPlaneTemplateResourceSpec `json:"spec,omitempty"`
}

type K0sControlPlaneTemplateResourceSpec struct {
	K0sConfigSpec   bootstrapv1.K0sConfigSpec       `json:"k0sConfigSpec"`
	MachineTemplate *K0sControlPlaneMachineTemplate `json:"machineTemplate,omitempty"`
	Version         string                          `json:"version,omitempty"`
	// UpdateStrategy defines the strategy to use when updating the control plane.
	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Enum=InPlace;Recreate
	//+kubebuilder:default=InPlace
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`
}

// +kubebuilder:object:root=true

type K0sControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sControlPlaneTemplate `json:"items"`
}
