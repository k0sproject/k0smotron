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
	"path/filepath"
	"strings"

	"github.com/k0sproject/k0smotron/internal/provisioner"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&K0sWorkerConfig{}, &K0sWorkerConfigList{})
	SchemeBuilder.Register(&K0sControllerConfig{}, &K0sControllerConfigList{})
}

const (
	// ConfigReadyCondition is true if the Config resource is not deleted,
	// and both DataSecretCreated, CertificatesAvailable conditions are true.
	ConfigReadyCondition = clusterv1.ReadyCondition

	// ConfigReadyReason surfaces when the Config resource is ready.
	ConfigReadyReason = clusterv1.ReadyReason

	// ConfigNotReadyReason surfaces when the Config resource is not ready.
	ConfigNotReadyReason = clusterv1.NotReadyReason

	// ConfigReadyUnknownReason surfaces when Config resource readiness is unknown.
	ConfigReadyUnknownReason = clusterv1.ReadyUnknownReason
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=bootstrap-k0smotron"

// K0sWorkerConfig is the Schema for the k0sworkerconfigs API
type K0sWorkerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0sWorkerConfigSpec   `json:"spec,omitempty"`
	Status K0sWorkerConfigStatus `json:"status,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (c *K0sWorkerConfig) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the conditions on the K0sWorkerConfig status.
func (c *K0sWorkerConfig) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// K0sWorkerConfigList contains a list of K0sWorkerConfig
type K0sWorkerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sWorkerConfig `json:"items"`
}

// K0sWorkerConfigSpec defines the desired state of K0sWorkerConfig
type K0sWorkerConfigSpec struct {
	// Ignition defines the ignition configuration. If empty, k0smotron will use cloud-init.
	// +kubebuilder:validation:Optional
	Ignition *IgnitionSpec `json:"ignition,omitempty"`
	// K0sInstallDir specifies the directory where k0s binary will be installed.
	// If empty, k0smotron will use /usr/local/bin, which is the default install path used by k0s get script.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=/usr/local/bin
	K0sInstallDir string `json:"k0sInstallDir,omitempty"`
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
	Files []File `json:"files,omitempty"`

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

	// CustomUserDataRef is a reference to a secret or a configmap that contains the custom user data.
	// Provided user-data will be merged with the one generated by k0smotron. Note that you may want to specify the merge type.
	// See: https://cloudinit.readthedocs.io/en/latest/reference/merging.html
	// +kubebuilder:validation:Optional
	CustomUserDataRef *ContentSource `json:"customUserDataRef,omitempty"`

	// SecretMetadata specifies metadata (labels and annotations) to be propagated to the bootstrap Secret.
	// +kubebuilder:validation:Optional
	SecretMetadata *SecretMetadata `json:"secretMetadata,omitempty"`

	// WorkingDir specifies the working directory where k0smotron will place its files.
	WorkingDir string `json:"workingDir,omitempty"`
}

// SecretMetadata defines metadata to be propagated to the bootstrap Secret
type SecretMetadata struct {
	// Labels to be added to the bootstrap Secret
	// +kubebuilder:validation:Optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations to be added to the bootstrap Secret
	// +kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// JoinTokenSecretRef is a reference to a secret that contains the join token for k0s worker nodes.
type JoinTokenSecretRef struct {
	// Name is the name of the secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Key is the key in the secret that contains the join token
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

// K0sWorkerConfigStatus defines the observed state of K0sWorkerConfig
type K0sWorkerConfigStatus struct {
	// Ready indicates the Bootstrapdata field is ready to be consumed
	Ready bool `json:"ready,omitempty"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`

	// Conditions defines current service state of the K0sWorkerConfig.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=bootstrap-k0smotron"

// K0sControllerConfig is the Schema for the k0scontrollerconfigs API
type K0sControllerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0sControllerConfigSpec   `json:"spec,omitempty"`
	Status K0sControllerConfigStatus `json:"status,omitempty"`
}

// K0sControllerConfigStatus defines the observed state of K0sControllerConfig
type K0sControllerConfigStatus struct {
	// Ready indicates the Bootstrapdata field is ready to be consumed
	Ready bool `json:"ready,omitempty"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`

	// Conditions defines current service state of the K0sControllerConfig.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (c *K0sControllerConfig) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the conditions on the K0sControllerConfig status.
func (c *K0sControllerConfig) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// K0sControllerConfigList contains a list of K0sControllerConfig
type K0sControllerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sControllerConfig `json:"items"`
}

// K0sControllerConfigSpec defines the desired state of K0sControllerConfig
type K0sControllerConfigSpec struct {
	// Version is the version of k0s to use. In case this is not set, k0smotron will use
	// a version field of the Machine object. If it's empty, the latest version is used.
	// Make sure the version is compatible with the k0s version running on the control plane.
	// For reference see the Kubernetes version skew policy: https://kubernetes.io/docs/setup/release/version-skew-policy/
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`

	*K0sConfigSpec `json:",inline"`
}

// File defines a file to be passed to user_data upon creation.
type File struct {
	provisioner.File `json:",inline"`
	// ContentFrom specifies the source of the content.
	// +kubebuilder:validation:Optional
	ContentFrom *ContentSource `json:"contentFrom,omitempty"`
}

// ContentSource defines the source of the content.
type ContentSource struct {
	// SecretRef is a reference to a secret that contains the content.
	// +kubebuilder:validation:Optional
	SecretRef *ContentSourceRef `json:"secretRef,omitempty"`
	// ConfigMapRef is a reference to a configmap that contains the content.
	// +kubebuilder:validation:Optional
	ConfigMapRef *ContentSourceRef `json:"configMapRef,omitempty"`
}

// ContentSourceRef is a reference to a secret or a configmap that contains the content.
type ContentSourceRef struct {
	// Name is the name of the source
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Key is the key in the source that contains the content
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

// K0sConfigSpec defines the common configuration for both K0sControllerConfig and K0sWorkerConfig.
type K0sConfigSpec struct {
	// Ignition defines the ignition configuration. If empty, k0smotron will use cloud-init.
	// +kubebuilder:validation:Optional
	Ignition *IgnitionSpec `json:"ignition,omitempty"`
	// K0sInstallDir specifies the directory where k0s binary will be installed.
	// If empty, k0smotron will use /usr/local/bin, which is the default install path used by k0s get script.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=/usr/local/bin
	K0sInstallDir string `json:"k0sInstallDir,omitempty"`
	// K0s defines the k0s configuration. Note, that some fields will be overwritten by k0smotron.
	// If empty, will be used default configuration. @see https://docs.k0sproject.io/stable/configuration/
	//+kubebuilder:validation:Optional
	//+kubebuilder:pruning:PreserveUnknownFields
	K0s *unstructured.Unstructured `json:"k0s,omitempty"`

	// Files specifies extra files to be passed to user_data upon creation.
	// +kubebuilder:validation:Optional
	Files []File `json:"files,omitempty"`

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
	// Supported protocols are: http, https, oci. Using 'oci' scheme requires 'oras' to be installed on the target system.
	//
	// If 'oci' schema is used and the OCI registry requires authentication, make sure to set up the authentication beforehand
	// by adding a file to the Files section that contains the necessary config for ORAS. See: https://oras.land/docs/how_to_guides/authentication/
	// The file must be placed at `/root` directory (HOME for cloud-init execution time) and named `config.json`.
	// NOTE: use `.preStartCommands` to set DOCKER_CONFIG environment variable in order to let ORAS pick up your custom config file.
	DownloadURL string `json:"downloadURL,omitempty"`

	// Tunneling defines the tunneling configuration for the cluster.
	//+kubebuilder:validation:Optional
	Tunneling TunnelingSpec `json:"tunneling,omitempty"`

	// CustomUserDataRef is a reference to a secret or a configmap that contains the custom user data.
	// Provided user-data will be merged with the one generated by k0smotron. Note that you may want to specify the merge type.
	// See: https://cloudinit.readthedocs.io/en/latest/reference/merging.html
	// +kubebuilder:validation:Optional
	CustomUserDataRef *ContentSource `json:"customUserDataRef,omitempty"`

	// WorkingDir specifies the working directory where k0smotron will place its files.
	WorkingDir string `json:"workingDir,omitempty"`
}

// TunnelingSpec defines the tunneling configuration for the cluster.
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

// IgnitionSpec defines the configuration for the Ignition provisioner.
type IgnitionSpec struct {
	// Variant declares which distribution variant the generated config is for.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=fcos;flatcar;openshift;r4e;fiot
	// Check the supported variants and versions here:
	// https://coreos.github.io/butane/specs/#butane-specifications-and-ignition-specifications
	Variant string `json:"variant,omitempty"`
	// Version is the schema version of the Butane config to use
	// +kubebuilder:validation:Required
	// Check the supported variants and versions here:
	// https://coreos.github.io/butane/specs/#butane-specifications-and-ignition-specifications
	Version string `json:"version,omitempty"`
	// AdditionalConfig is an unstructured object that contains additional config to be merged
	// with the generated one. The format follows Butane spec: https://coreos.github.io/butane/
	// +kubebuilder:validation:Optional
	AdditionalConfig string `json:"additionalConfig,omitempty"`
}

// GetK0sConfigPath returns the full path to the k0s.yaml file in the working directory.
func (kcs *K0sConfigSpec) GetK0sConfigPath() string {
	if kcs.WorkingDir == "" {
		return "/etc/k0s.yaml"
	}
	return filepath.Join(kcs.WorkingDir, "k0s.yaml")
}

// GetJoinTokenPath returns the full path to the k0s token file in the working directory.
func (kcs *K0sConfigSpec) GetJoinTokenPath() string {
	if kcs.WorkingDir == "" {
		return "/etc/k0s.token"
	}
	return filepath.Join(kcs.WorkingDir, "k0s.token")
}

// GetK0sConfigPath returns the full path to the k0s.yaml file in the working directory.
func (c *K0sWorkerConfig) GetK0sConfigPath() string {
	if c.Spec.WorkingDir == "" {
		return "/etc/k0s.yaml"
	}
	return filepath.Join(c.Spec.WorkingDir, "k0s.yaml")
}

// GetJoinTokenPath returns the full path to the k0s token file in the working directory.
func (c *K0sWorkerConfig) GetJoinTokenPath() string {
	if c.Spec.WorkingDir == "" {
		return "/etc/k0s.token"
	}
	return filepath.Join(c.Spec.WorkingDir, "k0s.token")
}

// Validate validates the K0sWorkerConfigSpec.
func (cs *K0sWorkerConfigSpec) Validate(pathPrefix *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// TODO: validate Ignition
	allErrs = append(allErrs, cs.validateVersion(pathPrefix)...)
	allErrs = append(allErrs, cs.validateFiles(pathPrefix)...)

	return allErrs
}

func (cs *K0sWorkerConfigSpec) validateFiles(pathPrefix *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	knownPaths := map[string]struct{}{}

	for i, file := range cs.Files {
		if file.Content != "" && file.ContentFrom != nil {
			allErrs = append(
				allErrs,
				field.Invalid(
					pathPrefix.Child("files").Index(i),
					file,
					conflictingFileSourceMsg,
				),
			)
		}

		if file.ContentFrom == nil && file.Content == "" {
			allErrs = append(
				allErrs,
				field.Invalid(
					pathPrefix.Child("files").Index(i),
					file,
					noContentMsg,
				),
			)
		}

		if file.ContentFrom != nil {
			if file.ContentFrom.SecretRef != nil && file.ContentFrom.ConfigMapRef != nil {
				allErrs = append(
					allErrs,
					field.Invalid(
						pathPrefix.Child("files").Index(i).Child("contentFrom"),
						file.ContentFrom,
						conflictingContentFromMsg,
					),
				)
			}

			if file.ContentFrom.SecretRef != nil && file.ContentFrom.SecretRef.Name == "" {
				allErrs = append(
					allErrs,
					field.Required(
						pathPrefix.Child("files").Index(i).Child("contentFrom").Child("secretRef").Child("name"),
						"name is required",
					),
				)
			}

			if file.ContentFrom.ConfigMapRef != nil && file.ContentFrom.ConfigMapRef.Name == "" {
				allErrs = append(
					allErrs,
					field.Required(
						pathPrefix.Child("files").Index(i).Child("contentFrom").Child("configMapRef").Child("name"),
						"name is required",
					),
				)
			}
		}
		_, conflict := knownPaths[file.Path]
		if conflict {
			allErrs = append(
				allErrs,
				field.Invalid(
					pathPrefix.Child("files").Index(i).Child("path"),
					file,
					pathConflictMsg,
				),
			)
		}
		knownPaths[file.Path] = struct{}{}
	}

	return allErrs
}

func (cs *K0sWorkerConfigSpec) validateVersion(pathPrefix *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if strings.Contains(cs.Version, "-k0s.") {
		allErrs = append(
			allErrs,
			field.Invalid(
				pathPrefix.Child("version"),
				cs.Version,
				"k0s specific versions must be specified using the '+k0s' suffix",
			),
		)
		return allErrs
	}

	return allErrs
}
