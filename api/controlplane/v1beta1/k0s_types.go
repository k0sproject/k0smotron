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
	"slices"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func init() {
	SchemeBuilder.Register(&K0sControlPlane{}, &K0sControlPlaneList{})
}

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
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=control-plane-k0smotron"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels['cluster\\.x-k8s\\.io/cluster-name']",description="Cluster"
// +kubebuilder:printcolumn:name="API Server Available",type=boolean,JSONPath=".status.ready",description="This denotes that the target API Server is ready to receive requests"
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=".spec.replicas",description="Total number of machines desired by this control plane",priority=10
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".status.replicas",description="Total number of non-terminated machines targeted by this control plane"
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=".status.readyReplicas",description="Total number of fully running and ready control plane instances"
// +kubebuilder:printcolumn:name="Updated",type=integer,JSONPath=".status.updatedReplicas",description="Total number of non-terminated machines targeted by this control plane that have the desired template spec"
// +kubebuilder:printcolumn:name="Unavailable",type=integer,JSONPath=".status.unavailableReplicas",description="Total number of unavailable control plane instances targeted by this control plane"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of K0sControlPlane"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version",description="Kubernetes version associated with this control plane"

type K0sControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0sControlPlaneSpec `json:"spec,omitempty"`

	// +kubebuilder:default={version:"",ready:false,initialized:false,initialization:{controlPlaneInitialized:false}}
	Status K0sControlPlaneStatus `json:"status,omitempty"`
}

type K0sControlPlaneSpec struct {
	K0sConfigSpec   bootstrapv1.K0sConfigSpec            `json:"k0sConfigSpec"`
	MachineTemplate *cpv2.K0sControlPlaneMachineTemplate `json:"machineTemplate"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
	// UpdateStrategy defines the strategy to use when updating the control plane.
	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Enum=InPlace;Recreate;RecreateDeleteFirst
	//+kubebuilder:default=InPlace
	UpdateStrategy cpv2.UpdateStrategy `json:"updateStrategy,omitempty"`
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

// +kubebuilder:object:root=true

type K0sControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sControlPlane `json:"items"`
}

type K0sControlPlaneStatus struct {
	// Ready denotes that the control plane is ready
	// +optional
	Ready bool `json:"ready"`

	// initialized denotes that the K0sControlPlane API Server is initialized and thus
	// it can accept requests.
	// NOTE: this field is part of the Cluster API contract and it is used to orchestrate provisioning.
	// The value of this field is never updated after provisioning is completed. Please use conditions
	// to check the operational state of the control plane.
	// +optional
	Initialized bool `json:"initialized"`

	// initialization represents the initialization status of the control plane
	// +optional
	Initialization Initialization `json:"initialization,omitempty"`

	// externalManagedControlPlane is a bool that should be set to true if the Node objects do not exist in the cluster.
	// +optional
	ExternalManagedControlPlane bool `json:"externalManagedControlPlane"`

	// replicas is the total number of non-terminated machines targeted by this control plane
	// (their labels match the selector).
	// +optional
	Replicas int32 `json:"replicas"`

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

	// unavailableReplicas is the total number of unavailable machines targeted by this control plane.
	// This is the total number of machines that are still required for
	// the deployment to have 100% available capacity. They may either
	// be machines that are running but not yet ready or machines
	// that still have not been created.
	// +optional
	UnavailableReplicas int32 `json:"unavailableReplicas"`

	// readyReplicas is the total number of fully running and ready control plane machines.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas"`

	// updatedReplicas is the total number of non-terminated machines targeted by this control plane
	// that have the desired template spec.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas"`

	// Conditions defines current service state of the K0sControlPlane.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the conditions of the K0sControlPlane status.
func (k *K0sControlPlane) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the K0sControlPlane status.
func (k *K0sControlPlane) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func (k *K0sControlPlane) WorkerEnabled() bool {
	return slices.Contains(k.Spec.K0sConfigSpec.Args, "--enable-worker")
}
