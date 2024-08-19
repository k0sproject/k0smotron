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
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&K0sWorkerConfig{}, &K0sWorkerConfigList{})
	SchemeBuilder.Register(&K0sControllerConfig{}, &K0sControllerConfigList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=bootstrap-k0smotron"

type K0sWorkerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0sWorkerConfigSpec   `json:"spec,omitempty"`
	Status K0sWorkerConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type K0sWorkerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sWorkerConfig `json:"items"`
}

type K0sWorkerConfigSpec struct {
	// JoinTokenSecretRef is a reference to a secret that contains the join token.
	// This should be only set in the case you want to use a pre-generated join token.
	// +kubebuilder:validation:Optional
	JoinTokenSecretRef *JoinTokenSecretRef `json:"joinTokenSecretRef,omitempty"`

	// Version is the version of k0s to use. In case this is not set, k0smotron will use
	// a version field of the Machine object. If it's empty, the latest version is used.
	// Make sure the version is compatible with the k0s version running on the control plane.
	// For reference see the Kubernetes version skew policy: https://kubernetes.io/docs/setup/release/version-skew-policy/
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	// UseSystemHostname specifies whether to use the system hostname for the kubernetes node name.
	// By default, k0smotron will use Machine name as a node name. If true, it will pick it from `hostname` command output.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	UseSystemHostname bool `json:"useSystemHostname,omitempty"`

	// Files specifies extra files to be passed to user_data upon creation.
	// +kubebuilder:validation:Optional
	Files []cloudinit.File `json:"files,omitempty"`

	// Args specifies extra arguments to be passed to k0s worker.
	// See: https://docs.k0sproject.io/stable/worker-node-config/
	// See: https://docs.k0sproject.io/stable/cli/k0s_worker/
	Args []string `json:"args,omitempty"`

	// PreStartCommands specifies commands to be run before starting k0s worker.
	// +kubebuilder:validation:Optional
	PreStartCommands []string `json:"preStartCommands,omitempty"`

	// PostStartCommands specifies commands to be run after starting k0s worker.
	// +kubebuilder:validation:Optional
	PostStartCommands []string `json:"postStartCommands,omitempty"`

	// PreInstallK0s specifies whether k0s binary is pre-installed on the node.
	// +kubebuilder:validation:Optional
	PreInstalledK0s bool `json:"preInstalledK0s,omitempty"`

	// DownloadURL specifies the URL to download k0s binary from.
	// If specified the version field is ignored and what ever version is downloaded from the URL is used.
	// +kubebuilder:validation:Optional
	DownloadURL string `json:"downloadURL,omitempty"`
}

type JoinTokenSecretRef struct {
	// Name is the name of the secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Key is the key in the secret that contains the join token
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

type K0sWorkerConfigStatus struct {
	// Ready indicates the Bootstrapdata field is ready to be consumed
	Ready bool `json:"ready,omitempty"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`
	// TODO Conditions etc
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=bootstrap-k0smotron"

type K0sControllerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0sControllerConfigSpec   `json:"spec,omitempty"`
	Status K0sControllerConfigStatus `json:"status,omitempty"`
}

type K0sControllerConfigStatus struct {
	// Ready indicates the Bootstrapdata field is ready to be consumed
	Ready bool `json:"ready,omitempty"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`
}

// +kubebuilder:object:root=true

type K0sControllerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sControllerConfig `json:"items"`
}

type K0sControllerConfigSpec struct {
	// Version is the version of k0s to use. In case this is not set, k0smotron will use
	// a version field of the Machine object. If it's empty, the latest version is used.
	// Make sure the version is compatible with the k0s version running on the control plane.
	// For reference see the Kubernetes version skew policy: https://kubernetes.io/docs/setup/release/version-skew-policy/
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	*K0sConfigSpec `json:",inline"`
}

type K0sConfigSpec struct {
	// K0s defines the k0s configuration. Note, that some fields will be overwritten by k0smotron.
	// If empty, will be used default configuration. @see https://docs.k0sproject.io/stable/configuration/
	//+kubebuilder:validation:Optional
	//+kubebuilder:pruning:PreserveUnknownFields
	K0s *unstructured.Unstructured `json:"k0s,omitempty"`

	// Files specifies extra files to be passed to user_data upon creation.
	// +kubebuilder:validation:Optional
	Files []cloudinit.File `json:"files,omitempty"`

	// UseSystemHostname specifies whether to use the system hostname for the kubernetes node name.
	// By default, k0smotron will use Machine name as a node name. If true, it will pick it from `hostname` command output.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	UseSystemHostname bool `json:"useSystemHostname,omitempty"`

	// Args specifies extra arguments to be passed to k0s controller.
	// See: https://docs.k0sproject.io/stable/cli/k0s_controller/
	Args []string `json:"args,omitempty"`

	// PreStartCommands specifies commands to be run before starting k0s worker.
	// +kubebuilder:validation:Optional
	PreStartCommands []string `json:"preStartCommands,omitempty"`

	// PostStartCommands specifies commands to be run after starting k0s worker.
	// +kubebuilder:validation:Optional
	PostStartCommands []string `json:"postStartCommands,omitempty"`

	// PreInstallK0s specifies whether k0s binary is pre-installed on the node.
	// +kubebuilder:validation:Optional
	PreInstalledK0s bool `json:"preInstalledK0s,omitempty"`

	// DownloadURL specifies the URL from which to download the k0s binary.
	// If the version field is specified, it is ignored, and whatever version is downloaded from the URL is used.
	// +kubebuilder:validation:Optional
	DownloadURL string `json:"downloadURL,omitempty"`

	// Tunneling defines the tunneling configuration for the cluster.
	//+kubebuilder:validation:Optional
	Tunneling TunnelingSpec `json:"tunneling,omitempty"`
}

type TunnelingSpec struct {
	// Enabled specifies whether tunneling is enabled.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	// Server address of the tunneling server.
	// If empty, k0smotron will try to detect worker node address for.
	//+kubebuilder:validation:Optional
	ServerAddress string `json:"serverAddress,omitempty"`
	// NodePort to publish for server port of the tunneling server.
	// If empty, k0smotron will use the default one.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=31700
	ServerNodePort int32 `json:"serverNodePort,omitempty"`
	// NodePort to publish for tunneling port.
	// If empty, k0smotron will use the default one.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=31443
	TunnelingNodePort int32 `json:"tunnelingNodePort,omitempty"`
	// Mode describes tunneling mode.
	// If empty, k0smotron will use the default one.
	//+kubebuilder:validation:Enum=tunnel;proxy
	//+kubebuilder:default=tunnel
	Mode string `json:"mode,omitempty"`
}
