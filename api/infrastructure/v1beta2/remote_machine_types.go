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
	v1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
)

func init() {
	SchemeBuilder.Register(&RemoteMachine{}, &RemoteMachineList{}, &PooledRemoteMachine{}, &PooledRemoteMachineList{})
}

const (
	// RemoteMachineBootstrapExecSucceededCondition is the condition type that indicates the success of the bootstrap commands execution on the remote machine.
	RemoteMachineBootstrapExecSucceededCondition = "BootstrapExecSucceeded"
	// RemoteMachineBootstrapExecSucceededReason is the reason used when the bootstrap commands were executed successfully on the remote machine.
	RemoteMachineBootstrapExecSucceededReason = "BootstrapExecSucceeded"
	// RemoteMachineReadyCondition is the condition type that indicates whether the RemoteMachine is ready.
	RemoteMachineReadyCondition = "Ready"
	// RemoteMachineReadyReason is the reason used when the RemoteMachine is ready after the bootstrap commands were executed successfully on the remote machine.
	RemoteMachineReadyReason = "Ready"
	// InternalErrorReason indicates that an internal error occurred during the provisioning process.
	InternalErrorReason = "InternalError"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"
// +kubebuilder:storageversion

// RemoteMachine is the Schema for the remotemachines API
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

	// CommandsAsScript indicates if the commands should be executed as a script.
	// If true, the commands will be written to a file and executed as a script.
	// If false, the commands will be executed one by one.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	CommandsAsScript bool `json:"commandsAsScript,omitempty"`

	// WorkingDir is the directory to use as working directory when connecting to the remote machine.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="/etc/k0smotron"
	WorkingDir string `json:"workingDir,omitempty"`

	// SSHKeyRef is a reference to a secret that contains the SSH private key.
	// The key must be placed on the secret using the key "value".
	// +kubebuilder:validation:Optional
	SSHKeyRef SecretRef `json:"sshKeyRef,omitempty"`

	// CleanUpCommands allows the user to run custom commands during the machine cleanup process.
	// If CleanUpCommands is set and k0s is used as the bootstrap provider,
	// the user is responsible for the complete cleanup of the k0s installation.
	// See https://docs.k0sproject.io/stable/reset/ for more details.
	// +kubebuilder:validation:Optional
	CleanUpCommands []string `json:"cleanUpCommands,omitempty"`

	// ProvisionJob describes the kubernetes Job to use to provision the machine.
	ProvisionJob *ProvisionJob `json:"provisionJob,omitempty"`
}

// ProvisionJob describes the kubernetes Job to use to provision the machine.
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
	// initialization provides observations of the RemoteMachine initialization process.
	// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate initial Machine provisioning.
	// +optional
	Initialization RemoteMachineInitializationStatus `json:"initialization,omitempty,omitzero"`
	// addresses contains the associated addresses for the machine.
	// +optional
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`
	// conditions contains the conditions of the RemoteMachine, which represent the current state of the machine.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// deprecated groups all the status fields that are deprecated and will be removed when all the nested field are removed.
	// +optional
	Deprecated *RemoteMachineStatusDeprecated `json:"deprecated,omitempty"`
}

// RemoteMachineStatusDeprecated defines the observed state of RemoteMachine for deprecated fields, which will be removed in future versions.
type RemoteMachineStatusDeprecated struct {
	// v1beta1 groups all the status fields that are deprecated and will be removed when support for v1beta1 will be dropped.
	// +optional
	V1Beta1 *RemoteMachineStatusV1beta1Deprecated `json:"v1beta1,omitempty"`
}

// RemoteMachineStatusV1beta1Deprecated defines the observed state of RemoteMachine for v1beta1, which is deprecated and will be removed in future versions.
type RemoteMachineStatusV1beta1Deprecated struct {
	FailureReason  string `json:"failureReason,omitempty"`
	FailureMessage string `json:"failureMessage,omitempty"`
}

// SetFailures sets the failure reason and message in the RemoteMachine status.
func (rms *RemoteMachineStatus) SetFailures(reason, message string) {
	if reason != "" || message != "" {
		if rms.Deprecated == nil {
			rms.Deprecated = &RemoteMachineStatusDeprecated{}
		}
		if rms.Deprecated.V1Beta1 == nil {
			rms.Deprecated.V1Beta1 = &RemoteMachineStatusV1beta1Deprecated{}
		}
		rms.Deprecated.V1Beta1.FailureReason = reason
		rms.Deprecated.V1Beta1.FailureMessage = message
	}
}

// GetFailureReason gets the failure reason from the RemoteMachine status.
func (rms *RemoteMachineStatus) GetFailureReason() string {
	if rms.Deprecated != nil && rms.Deprecated.V1Beta1 != nil {
		return rms.Deprecated.V1Beta1.FailureReason
	}
	return ""
}

// GetFailureMessage gets the failure message from the RemoteMachine status.
func (rms *RemoteMachineStatus) GetFailureMessage() string {
	if rms.Deprecated != nil && rms.Deprecated.V1Beta1 != nil {
		return rms.Deprecated.V1Beta1.FailureMessage
	}
	return ""
}

// GetConditions returns the set of conditions for this object.
func (rm *RemoteMachine) GetConditions() []metav1.Condition {
	return rm.Status.Conditions
}

// SetConditions sets the conditions on the RemoteMachine status.
func (rm *RemoteMachine) SetConditions(conditions []metav1.Condition) {
	rm.Status.Conditions = conditions
}

// RemoteMachineInitializationStatus provides observations of the RemoteMachine initialization process.
type RemoteMachineInitializationStatus struct {
	// provisioned is true when the RemoteMachine's infrastructure is fully provisioned.
	// NOTE: this field is part of the Cluster API contract, and it is used to orchestrate initial Machine provisioning.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// SecretRef is a reference to a secret that contains a value.
type SecretRef struct {
	// Name is the name of the secret.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// +kubebuilder:object:root=true

// RemoteMachineList contains a list of RemoteMachine
type RemoteMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteMachine `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=infrastructure-k0smotron"
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=".spec.machine.address",description="IP address or DNS name of the remote machine"
// +kubebuilder:printcolumn:name="Reserved",type=string,JSONPath=".status.reserved",description="Indicates if the machine is reserved"
// +kubebuilder:printcolumn:name="Remote Machine",type=string,JSONPath=".status.machineRef.name",description="Reference to the RemoteMachine"
// +kubebuilder:storageversion

// PooledRemoteMachine represents a RemoteMachine that is part of a pool and can be reserved for use.
type PooledRemoteMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PooledRemoteMachineSpec   `json:"spec,omitempty"`
	Status PooledRemoteMachineStatus `json:"status,omitempty"`
}

// PooledRemoteMachineSpec defines the desired state of PooledRemoteMachine
type PooledRemoteMachineSpec struct {
	Pool    string            `json:"pool"`
	Machine PooledMachineSpec `json:"machine"`
}

// PooledMachineSpec defines the connection details and provisioning information for a machine in a pool.
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
	// CommandsAsScript indicates if the commands should be executed as a script.
	// If true, the commands will be written to a file and executed as a script.
	// If false, the commands will be executed one by one.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	CommandsAsScript bool `json:"commandsAsScript,omitempty"`
	// WorkingDir is the directory to use as working directory when connecting to the remote machine.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="/etc/k0smotron"
	WorkingDir string `json:"workingDir,omitempty"`
	// CleanUpCommands allow the user to run custom command for the clean up process of the machine.
	// +kubebuilder:validation:Optional
	CleanUpCommands []string `json:"cleanUpCommands,omitempty"`

	// SSHKeyRef is a reference to a secret that contains the SSH private key.
	// The key must be placed on the secret using the key "value".
	// +kubebuilder:validation:Required
	SSHKeyRef SecretRef `json:"sshKeyRef"`
}

// PooledRemoteMachineStatus defines the observed state of PooledRemoteMachine
type PooledRemoteMachineStatus struct {
	Reserved   bool             `json:"reserved"`
	MachineRef RemoteMachineRef `json:"machineRef"`
}

// RemoteMachineRef is a reference to a RemoteMachine that has been reserved for use.
type RemoteMachineRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// +kubebuilder:object:root=true

// PooledRemoteMachineList contains a list of PooledRemoteMachine
type PooledRemoteMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PooledRemoteMachine `json:"items"`
}

// SetupRemoteMachineWebhookWithManager registers the webhook for remote machines in the manager.
func SetupRemoteMachineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &RemoteMachine{}).
		Complete()
}

// SetupPooledRemoteMachineWebhookWithManager registers the webhook for pooled remote machines in the manager.
func SetupPooledRemoteMachineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &PooledRemoteMachine{}).
		Complete()
}
