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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&RemoteMachineTemplate{}, &RemoteMachineTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"
// +kubebuilder:storageversion

// RemoteMachineTemplate is the Schema for the remotemachinetemplates API
type RemoteMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteMachineTemplateSpec   `json:"spec,omitempty"`
	Status RemoteMachineTemplateStatus `json:"status,omitempty"`
}

// RemoteMachineTemplateSpec defines the desired state of RemoteMachineTemplate
type RemoteMachineTemplateSpec struct {
	Template RemoteMachineTemplateResource `json:"template"`
}

// RemoteMachineTemplateStatus defines the observed state of RemoteMachineTemplate.
type RemoteMachineTemplateStatus struct {
	// capacity defines the resource capacity of the machines this template draws from.
	// The pool holds machines that already exist, so k0smotron cannot measure them and
	// leaves this for the operator to declare.
	// Autoscaling from zero reads this value, as described by the proposal at
	// https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20210310-opt-in-autoscaling-from-zero.md
	// +optional
	Capacity corev1.ResourceList `json:"capacity,omitempty"`

	// nodeInfo describes the architecture and the operating system of those machines.
	// +optional
	NodeInfo NodeInfo `json:"nodeInfo,omitempty,omitzero"`
}

// Architecture represents the CPU architecture of the node.
// +kubebuilder:validation:Enum=amd64;arm64;s390x;ppc64le
// +enum
type Architecture string

const (
	// ArchitectureAmd64 is the amd64 CPU architecture.
	ArchitectureAmd64 Architecture = "amd64"
	// ArchitectureArm64 is the arm64 CPU architecture.
	ArchitectureArm64 Architecture = "arm64"
	// ArchitectureS390x is the s390x CPU architecture.
	ArchitectureS390x Architecture = "s390x"
	// ArchitecturePpc64le is the ppc64le CPU architecture.
	ArchitecturePpc64le Architecture = "ppc64le"
)

// NodeInfo describes the architecture and the operating system a machine runs.
// +kubebuilder:validation:MinProperties=1
type NodeInfo struct {
	// architecture is the CPU architecture of the node.
	// +optional
	Architecture Architecture `json:"architecture,omitempty"`

	// operatingSystem is a string representing the operating system of the node.
	// This may be a string like 'linux' or 'windows'.
	// +optional
	OperatingSystem string `json:"operatingSystem,omitempty"`
}

// RemoteMachineTemplateResource describes the data needed to create a RemoteMachine from a template
type RemoteMachineTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta                 `json:"metadata,omitempty"`
	Spec       RemoteMachineTemplateResourceSpec `json:"spec,omitempty"`
}

// RemoteMachineTemplateResourceSpec defines the desired state of RemoteMachineTemplateResource
type RemoteMachineTemplateResourceSpec struct {
	Pool string `json:"pool"`
	// ProvisionJob describes the kubernetes Job to use to provision the machine.
	ProvisionJob *ProvisionJob `json:"provisionJob,omitempty"`
}

// +kubebuilder:object:root=true

// RemoteMachineTemplateList contains a list of RemoteMachineTemplate
type RemoteMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteMachineTemplate `json:"items"`
}
