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
	v1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&RemoteMachine{}, &RemoteMachineList{}, &PooledRemoteMachine{}, &PooledRemoteMachineList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"

type RemoteMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteMachineSpec   `json:"spec,omitempty"`
	Status RemoteMachineStatus `json:"status,omitempty"`
}

// RemoteMachineSpec defines the desired state of RemoteMachine
type RemoteMachineSpec struct {
	// Pool is the name of the pool where the machine belongs to.
	// +kubebuilder:validation:Optional
	Pool string `json:"pool,omitempty"`

	// ProviderID is the ID of the machine in the provider.
	// +kubebuilder:validation:Optional
	ProviderID string `json:"providerID,omitempty"`

	// Address is the IP address or DNS name of the remote machine.
	// +kubebuilder:validation:Optional
	Address string `json:"address,omitempty"`

	// Port is the SSH port of the remote machine.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=22
	Port int `json:"port,omitempty"`

	// User is the user to use when connecting to the remote machine.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="root"
	User string `json:"user,omitempty"`

	// +kubebuilder:validation:Optional
	UseSudo bool `json:"useSudo,omitempty"`

	// SSHKeyRef is a reference to a secret that contains the SSH private key.
	// The key must be placed on the secret using the key "value".
	// +kubebuilder:validation:Optional
	SSHKeyRef SecretRef `json:"sshKeyRef,omitempty"`

	// ProvisionJob describes the kubernetes Job to use to provision the machine.
	ProvisionJob *ProvisionJob `json:"provisionJob,omitempty"`
}

type ProvisionJob struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="ssh"
	SSHCommand string `json:"sshCommand,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="scp"
	SCPCommand string `json:"scpCommand,omitempty"`
	// JobTemplate is the job template to use to provision the machine.
	JobTemplate *v1.JobTemplateSpec `json:"jobSpecTemplate,omitempty"`
}

// RemoteMachineStatus defines the observed state of RemoteMachine
type RemoteMachineStatus struct {
	// Ready denotes that the remote machine is ready to be used.
	// +kubebuilder:validation:Optional
	Ready bool `json:"ready,omitempty"`

	FailureReason  string `json:"failureReason,omitempty"`
	FailureMessage string `json:"failureMessage,omitempty"`
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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"

type PooledRemoteMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PooledRemoteMachineSpec   `json:"spec,omitempty"`
	Status PooledRemoteMachineStatus `json:"status,omitempty"`
}

type PooledRemoteMachineSpec struct {
	Pool    string            `json:"pool"`
	Machine PooledMachineSpec `json:"machine"`
}

type PooledMachineSpec struct {
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

	// +kubebuilder:validation:Optional
	UseSudo bool `json:"useSudo,omitempty"`

	// SSHKeyRef is a reference to a secret that contains the SSH private key.
	// The key must be placed on the secret using the key "value".
	// +kubebuilder:validation:Required
	SSHKeyRef SecretRef `json:"sshKeyRef"`
}

type PooledRemoteMachineStatus struct {
	Reserved   bool             `json:"reserved"`
	MachineRef RemoteMachineRef `json:"machineRef"`
}

type RemoteMachineRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// +kubebuilder:object:root=true

type PooledRemoteMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PooledRemoteMachine `json:"items"`
}
