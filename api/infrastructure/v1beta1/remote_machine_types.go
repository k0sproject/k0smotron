/*
Copyright 2023.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&RemoteMachine{}, &RemoteMachineList{}, &RemoteCluster{}, &RemoteClusterList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"

type RemoteCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteClusterSpec   `json:"spec,omitempty"`
	Status RemoteClusterStatus `json:"status,omitempty"`
}

// RemoteClusterSpec defines the desired state of RemoteCluster
type RemoteClusterSpec struct {
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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"

type RemoteMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteMachineSpec   `json:"spec,omitempty"`
	Status RemoteMachineStatus `json:"status,omitempty"`
}

// RemoteMachineSpec defines the desired state of RemoteMachine
type RemoteMachineSpec struct {
	// ProviderID is the ID of the machine in the provider.
	// +kubebuilder:validation:Optional
	ProviderID string `json:"providerID,omitempty"`

	// Address is the IP address or DNS name of the remote machine.
	// +kubebuilder:validation:Required
	Address string `json:"address"`

	// Port is the SSH port of the remote machine.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=22
	Port int `json:"port"`

	// User is the user to use when connecting to the remote machine.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="root"
	User string `json:"user"`

	// SSHKeyRef is a reference to a secret that contains the SSH private key.
	// The key must be placed on the secret using the key "value".
	// +kubebuilder:validation:Required
	SSHKeyRef SecretRef `json:"sshKeyRef"`
}

// RemoteMachineStatus defines the observed state of RemoteMachine
type RemoteMachineStatus struct {
	// Ready denotes that the remote machine is ready to be used.
	// +kubebuilder:validation:Optional
	Ready bool `json:"ready,omitempty"`

	FailureReason  string `json:"failureReason,omitempty"`
	FailureMessage string `json:"failureMessage,omitempty"`

	// TODO Add conditions
}

type SecretRef struct {
	// Name is the name of the secret.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// +kubebuilder:object:root=true

type RemoteMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteMachine `json:"items"`
}
