package v1beta1

import (
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func init() {
	SchemeBuilder.Register(&K0sControlPlaneTemplate{}, &K0sControlPlaneTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"

// K0sControlPlaneTemplate is the template for creating K0s control planes.
type K0sControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0sControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// K0sControlPlaneTemplateSpec defines the desired state of K0sControlPlaneTemplate.
type K0sControlPlaneTemplateSpec struct {
	Template K0sControlPlaneTemplateResource `json:"template,omitempty"`
}

// K0sControlPlaneTemplateResource describes the data needed to create a K0sControlPlane from a template.
type K0sControlPlaneTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta                   `json:"metadata,omitempty"`
	Spec       K0sControlPlaneTemplateResourceSpec `json:"spec,omitempty"`
}

// K0sControlPlaneTemplateResourceSpec defines the desired state of K0sControlPlaneTemplateResource.
type K0sControlPlaneTemplateResourceSpec struct {
	K0sConfigSpec   bootstrapv1.K0sConfigSpec               `json:"k0sConfigSpec"`
	MachineTemplate *K0sControlPlaneTemplateMachineTemplate `json:"machineTemplate,omitempty"`
	Version         string                                  `json:"version,omitempty"`
	// UpdateStrategy defines the strategy to use when updating the control plane.
	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Enum=InPlace;Recreate;RecreateDeleteFirst
	//+kubebuilder:default=InPlace
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`
}

// K0sControlPlaneTemplateMachineTemplate defines the template for Machines
// in a K0sControlPlaneMachineTemplate object.
// NOTE: K0sControlPlaneTemplateMachineTemplate is similar to K0sControlPlaneMachineTemplate but
// omits ObjectMeta and InfrastructureRef fields. These fields do not make sense on the K0sControlPlaneTemplate,
// because they are calculated by the Cluster topology reconciler during reconciliation and thus cannot
// be configured on the K0sControlPlaneTemplate.
type K0sControlPlaneTemplateMachineTemplate struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`

	// NodeDrainTimeout is the total amount of time that the controller will spend on draining a controlplane node
	// The default value is 0, meaning that the node can be drained without any time limitations.
	// NOTE: NodeDrainTimeout is different from `kubectl drain --timeout`
	// +optional
	NodeDrainTimeout *metav1.Duration `json:"nodeDrainTimeout,omitempty"`

	// NodeVolumeDetachTimeout is the total amount of time that the controller will spend on waiting for all volumes
	// to be detached. The default value is 0, meaning that the volumes can be detached without any time limitations.
	// +optional
	NodeVolumeDetachTimeout *metav1.Duration `json:"nodeVolumeDetachTimeout,omitempty"`

	// NodeDeletionTimeout defines how long the machine controller will attempt to delete the Node that the Machine
	// hosts after the Machine is marked for deletion. A duration of 0 will retry deletion indefinitely.
	// If no value is provided, the default value for this property of the Machine resource will be used.
	// +optional
	NodeDeletionTimeout *metav1.Duration `json:"nodeDeletionTimeout,omitempty"`
}

// +kubebuilder:object:root=true

// K0sControlPlaneTemplateList contains a list of K0sControlPlaneTemplate.
type K0sControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sControlPlaneTemplate `json:"items"`
}
