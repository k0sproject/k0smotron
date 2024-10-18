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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func init() {
	SchemeBuilder.Register(&K0sControlPlane{}, &K0sControlPlaneList{})
}

type UpdateStrategy string

const (
	UpdateInPlace  UpdateStrategy = "InPlace"
	UpdateRecreate UpdateStrategy = "Recreate"
)

const (
	// ControlPlaneReadyCondition documents the status of the control plane
	ControlPlaneReadyCondition clusterv1.ConditionType = "ControlPlaneReady"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=control-plane-k0smotron"

type K0sControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0sControlPlaneSpec   `json:"spec,omitempty"`
	Status K0sControlPlaneStatus `json:"status,omitempty"`
}

type K0sControlPlaneSpec struct {
	K0sConfigSpec   bootstrapv1.K0sConfigSpec       `json:"k0sConfigSpec"`
	MachineTemplate *K0sControlPlaneMachineTemplate `json:"machineTemplate"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
	// UpdateStrategy defines the strategy to use when updating the control plane.
	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Enum=InPlace;Recreate
	//+kubebuilder:default=InPlace
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`
	// Version defines the k0s version to be deployed. You can use a specific k0s version (e.g. v1.27.1+k0s.0) or
	// just the Kubernetes version (e.g. v1.27.1). If left empty, k0smotron will select one automatically.
	//+kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`
}

type K0sControlPlaneMachineTemplate struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`

	// InfrastructureRef is a required reference to a custom resource
	// offered by an infrastructure provider.
	InfrastructureRef corev1.ObjectReference `json:"infrastructureRef"`
}

// +kubebuilder:object:root=true

type K0sControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sControlPlane `json:"items"`
}

type K0sControlPlaneStatus struct {
	// Ready denotes that the control plane is ready
	Ready                       bool   `json:"ready"`
	ControlPlaneReady           bool   `json:"controlPlaneReady"`
	Inititalized                bool   `json:"initialized"`
	ExternalManagedControlPlane bool   `json:"externalManagedControlPlane"`
	Replicas                    int32  `json:"replicas"`
	Version                     string `json:"version"`
	Selector                    string `json:"selector"`
	UnavailableReplicas         int32  `json:"unavailableReplicas"`
	ReadyReplicas               int32  `json:"readyReplicas"`
	UpdatedReplicas             int32  `json:"updatedReplicas"`

	// Conditions defines current service state of the K0sControlPlane.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

func (k *K0sControlPlane) GetConditions() clusterv1.Conditions {
	return k.Status.Conditions
}

func (k *K0sControlPlane) SetConditions(conditions clusterv1.Conditions) {
	k.Status.Conditions = conditions
}

func (k *K0sControlPlane) WorkerEnabled() bool {
	return slices.Contains(k.Spec.K0sConfigSpec.Args, "--enable-worker")
}
