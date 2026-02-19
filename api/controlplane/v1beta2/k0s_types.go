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

package v1beta2

import (
	"slices"

	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func init() {
	SchemeBuilder.Register(&K0sControlPlane{}, &K0sControlPlaneList{})
}

// UpdateStrategy defines the strategy to use when updating control plane nodes.
type UpdateStrategy string

const (
	// UpdateInPlace is the default update strategy and it updates the control plane nodes in place using k0s autopilot.
	UpdateInPlace UpdateStrategy = "InPlace"
	// UpdateRecreate recreates control plane nodes one by one starting from creating a spare node.
	UpdateRecreate UpdateStrategy = "Recreate"
	// UpdateRecreateDeleteFirst recreates control plane nodes one by one starting from deleting an existing node.
	UpdateRecreateDeleteFirst UpdateStrategy = "RecreateDeleteFirst"
)

const (
	// ControlPlaneAvailableCondition denotes that the control plane is available
	ControlPlaneAvailableCondition = "Available"

	// RemediationInProgressAnnotation is used to keep track that a remediation is in progress,
	// and more specifically it tracks that the system is in between having deleted an unhealthy machine
	// and recreating its replacement.
	RemediationInProgressAnnotation = "controlplane.cluster.x-k8s.io/remediation-in-progress"

	// ControlPlanePausedCondition documents the reconciliation of the control plane is paused.
	ControlPlanePausedCondition clusterv1.ConditionType = "Paused"

	// K0sControlPlaneFinalizer is the finalizer applied to KubeadmControlPlane resources
	// by its managing controller.
	K0sControlPlaneFinalizer = "k0s.controlplane.cluster.x-k8s.io"

	// K0ControlPlanePreTerminateHookCleanupAnnotation is the annotation used to mark the Machine associated with
	// the K0sControlPlane for k0s node resources cleanup: controlnode and etcdmember. This annotation will prevent
	// Machine controller from deleting the Machine before the cleanup is done.
	K0ControlPlanePreTerminateHookCleanupAnnotation = clusterv1.PreTerminateDeleteHookAnnotationPrefix + "/kcp-cleanup"

	// MachineK0sConfigAnnotation is the annotation used to store the K0sConfigSpec on the Machine object.
	MachineK0sConfigAnnotation = "k0s.controlplane.cluster.x-k8s.io/k0s-config"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=control-plane-k0smotron"
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels['cluster\\.x-k8s\\.io/cluster-name']",description="Cluster"
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=".spec.replicas",description="Total number of machines desired by this control plane",priority=10
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".status.replicas",description="Total number of non-terminated machines targeted by this control plane"
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=".status.readyReplicas",description="Total number of fully running and ready control plane instances"
// +kubebuilder:printcolumn:name="UpToDate",type=integer,JSONPath=".status.upToDateReplicas",description="Total number of up-to-date replicas targeted by this ControlPlane"
// +kubebuilder:printcolumn:name="Available",type=integer,JSONPath=".status.availableReplicas",description="Total number of available replicas for this ControlPlane"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp",description="Time duration since creation of K0sControlPlane"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version",description="Kubernetes version associated with this control plane"

// K0sControlPlane describes a k0s control plane for a Cluster API managed cluster.
type K0sControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0sControlPlaneSpec `json:"spec,omitempty"`

	// +kubebuilder:default={version:"",initialization:{controlPlaneInitialized:false}}
	Status K0sControlPlaneStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the K0sControlPlane status.
func (k *K0sControlPlane) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the K0sControlPlane status.
func (k *K0sControlPlane) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

// WorkerEnabled returns true if the control plane is configured to also run worker nodes.
func (k *K0sControlPlane) WorkerEnabled() bool {
	return slices.Contains(k.Spec.K0sConfigSpec.Args, "--enable-worker")
}

// K0sControlPlaneSpec defines the desired state of K0sControlPlane
type K0sControlPlaneSpec struct {
	K0sConfigSpec   bootstrapv2.K0sConfigSpec       `json:"k0sConfigSpec"`
	MachineTemplate *K0sControlPlaneMachineTemplate `json:"machineTemplate"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
	// UpdateStrategy defines the strategy to use when updating the control plane.
	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Enum=InPlace;Recreate;RecreateDeleteFirst
	//+kubebuilder:default=InPlace
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`
	// Version defines the k0s version to be deployed. You can use a specific k0s version (e.g. v1.27.1+k0s.0) or
	// just the Kubernetes version (e.g. v1.27.1). If left empty, k0smotron will select one automatically.
	//+kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`
	// KubeconfigSecretMetadata specifies metadata (labels and annotations) to be propagated to the kubeconfig Secret
	// created for the workload cluster.
	// Note: This metadata will have precedence over default labels/annotations on the Secret.
	// +kubebuilder:validation:Optional
	KubeconfigSecretMetadata bootstrapv2.SecretMetadata `json:"kubeconfigSecretMetadata,omitempty,omitzero"`
}

// K0sControlPlaneMachineTemplate describes the data needed to create a Machine from a template.
type K0sControlPlaneMachineTemplate struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta clusterv1.ObjectMeta               `json:"metadata,omitempty,omitzero"`
	Spec       K0sControlPlaneMachineTemplateSpec `json:"spec,omitempty,omitzero"`
}

// K0sControlPlaneMachineTemplateSpec defines the spec of a K0sControlPlaneMachineTemplate.
type K0sControlPlaneMachineTemplateSpec struct {
	// InfrastructureRef is a required reference to a custom resource
	// offered by an infrastructure provider.
	InfrastructureRef clusterv1.ContractVersionedObjectReference `json:"infrastructureRef,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// K0sControlPlaneList contains a list of K0sControlPlane.
type K0sControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sControlPlane `json:"items"`
}

// K0sControlPlaneStatus defines the observed state of K0sControlPlane
type K0sControlPlaneStatus struct {
	// initialization represents the initialization status of the control plane
	// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate initial Machine provisioning.
	// +optional
	Initialization Initialization `json:"initialization,omitempty,omitzero"`

	// externalManagedControlPlane is a bool that should be set to true if the Node objects do not exist in the cluster.
	// +optional
	ExternalManagedControlPlane bool `json:"externalManagedControlPlane"`

	// replicas is the total number of non-terminated machines targeted by this control plane
	// (their labels match the selector).
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// version represents the minimum Kubernetes version for the control plane machines
	// in the cluster.
	// +optional
	Version string `json:"version"`

	// selector is the label selector in string format to avoid introspection
	// by clients, and is used to provide the CRD-based integration for the
	// scale subresource and additional integrations for things like kubectl
	// describe.. The string will be in the same format as the query-param syntax.
	// More info about label selectors: http://kubernetes.io/docs/user-guide/labels#label-selectors
	// +optional
	Selector string `json:"selector"`

	// readyReplicas is the total number of fully running and ready control plane machines.
	// +optional
	ReadyReplicas *int32 `json:"readyReplicas,omitempty"`

	// availableReplicas is the number of available replicas for this ControlPlane. A machine is considered available when Machine's Available condition is true.
	// +optional
	AvailableReplicas *int32 `json:"availableReplicas,omitempty"`

	// upToDateReplicas is the number of up-to-date replicas targeted by this ControlPlane. A machine is considered available when Machine's UpToDate condition is true.
	// +optional
	UpToDateReplicas *int32 `json:"upToDateReplicas,omitempty"`

	// Conditions defines current service state of the K0sControlPlane.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
