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
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v2 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
)

// ClusterSpec defines the desired state of K0smotronCluster
type ClusterSpec struct {
	// KubeconfigRef is the reference to the kubeconfig of the hosting cluster.
	// This kubeconfig will be used to deploy the k0s control plane.
	//+kubebuilder:validation:Optional
	KubeconfigRef *v2.KubeconfigRef `json:"kubeconfigRef,omitempty"`
	// Replicas is the desired number of replicas of the k0s control planes.
	// If unspecified, defaults to 1. If the value is above 1, k0smotron requires kine datasource URL to be set
	// (spec.kineDataSourceURL or spec.kineDataSourceSecretName).
	// Recommended value is 3.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
	// Image defines the k0s image to be deployed. If empty k0smotron
	// will pick it automatically. Must not include the image tag.
	//+kubebuilder:default=quay.io/k0sproject/k0s
	Image string `json:"image,omitempty"`
	// ServiceAccount defines the service account to be used by both k0s and etcd StatefulSets.
	//+kubebuilder:validation:Optional
	ServiceAccount string `json:"serviceAccount,omitempty"`
	// Version defines the k0s version to be deployed. If empty k0smotron
	// will pick it automatically.
	//+kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`
	// ExternalAddress defines k0s external address. See https://docs.k0sproject.io/stable/configuration/#specapi
	// Will be detected automatically for service type LoadBalancer.
	//+kubebuilder:validation:Optional
	ExternalAddress string `json:"externalAddress,omitempty"`
	// Ingress defines the ingress configuration.
	//+kubebuilder:validation:Optional
	Ingress *v2.IngressSpec `json:"ingress,omitempty"`
	// Service defines the service configuration.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default={"type":"ClusterIP","apiPort":30443,"konnectivityPort":30132}
	Service v2.ServiceSpec `json:"service,omitempty"`
	// Persistence defines the persistence configuration. If empty k0smotron
	// will use emptyDir as a volume. See https://docs.k0smotron.io/stable/configuration/#persistence
	//+kubebuilder:validation:Optional
	//+kubebuilder:default={"type":"emptyDir"}
	Persistence v2.PersistenceSpec `json:"persistence,omitempty"`
	// KineDataSourceURL defines the kine datasource URL.
	//+kubebuilder:validation:Optional
	KineDataSourceURL string `json:"kineDataSourceURL,omitempty"`
	// KineDataSourceSecretName defines the name of kine datasource URL secret.
	//+kubebuilder:validation:Optional
	KineDataSourceSecretName string `json:"kineDataSourceSecretName,omitempty"`
	// k0sConfig defines the k0s configuration. Note, that some fields will be overwritten by k0smotron.
	// If empty, will be used default configuration. @see https://docs.k0sproject.io/stable/configuration/
	//+kubebuilder:validation:Optional
	//+kubebuilder:pruning:PreserveUnknownFields
	K0sConfig *unstructured.Unstructured `json:"k0sConfig,omitempty"`
	// CertificateRefs defines the certificate references.
	CertificateRefs []v2.CertificateRef `json:"certificateRefs,omitempty"`
	// Manifests allows to specify list of volumes with manifests to be
	// deployed in the cluster. The volumes will be mounted
	// in /var/lib/k0s/manifests/<manifests.name>, for this reason each
	// manifest is a stack. K0smotron allows any kind of volume, but the
	// recommendation is to use secrets and configmaps.
	// For more information check:
	// https://docs.k0sproject.io/stable/manifests/ and
	// https://kubernetes.io/docs/concepts/storage/volumes
	//+kubebuilder:validation:Optional
	Manifests []v1.Volume `json:"manifests,omitempty"`
	// Mounts allows to specify list of volumes with any files to be
	// mounted in the controlplane pod. K0smotron allows any kind of volume, but the
	// recommendation is to use secrets and configmaps.
	// For more information check:
	// https://kubernetes.io/docs/concepts/storage/volumes
	//+kubebuilder:validation:Optional
	Mounts []v2.Mount `json:"mounts,omitempty"`
	// ControlPlaneFlags allows to configure additional flags for k0s
	// control plane and to override existing ones. The default flags are
	// kept unless they are overriden explicitly. Flags with arguments must
	// be specified as a single string, e.g. --some-flag=argument
	//+kubebuilder:validation:Optional
	ControlPlaneFlags []string `json:"controllerPlaneFlags,omitempty"`
	// Monitoring defines the monitoring configuration.
	//+kubebuilder:validation:Optional
	Monitoring v2.MonitoringSpec `json:"monitoring,omitempty"`
	// Etcd defines the etcd configuration.
	//+kubebuilder:default={"image":"quay.io/k0sproject/etcd:v3.5.13","persistence":{}}
	Etcd v2.EtcdSpec `json:"etcd,omitempty"`

	// TopologySpreadConstraints will be passed directly to BOTH etcd and k0s pods.
	// See https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/ for more details.
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// Resources describes the compute resource requirements for the control plane pods.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// KubeconfigSecretMetadata specifies metadata (labels and annotations) to be propagated to the kubeconfig Secret
	// created for the workload cluster.
	// Note: This metadata will have precedence over default labels/annotations on the Secret.
	// +kubebuilder:validation:Optional
	KubeconfigSecretMetadata v2.SecretMetadata `json:"kubeconfigSecretMetadata,omitempty,omitzero"`
}

const (
	defaultK0SImage = "quay.io/k0sproject/k0s"
	// DefaultK0SVersion is the default k0s version used by k0smotron if no version is specified in the ClusterSpec.
	DefaultK0SVersion = "v1.27.9-k0s.0"
	// DefaultK0SSuffix is the default suffix added to the k0s version if no suffix is specified.
	// This is needed because k0s images are tagged with -k0s.X suffix.
	DefaultK0SSuffix = "k0s.0"
)

// GetImage returns the full image name with tag for the k0s image to be deployed,
// based on the ClusterSpec.
func (c *ClusterSpec) GetImage() string {
	k0sVersion := c.Version
	if k0sVersion == "" {
		k0sVersion = DefaultK0SVersion
	}

	// Ensure any version with +k0s. is converted to -k0s. for image tagging
	k0sVersion = strings.Replace(k0sVersion, "+k0s.", "-k0s.", 1)

	if !strings.Contains(k0sVersion, "-k0s.") {
		k0sVersion = fmt.Sprintf("%s-%s", k0sVersion, DefaultK0SSuffix)
	}

	if c.Image == "" {
		return fmt.Sprintf("%s:%s", defaultK0SImage, k0sVersion)
	}

	return fmt.Sprintf("%s:%s", c.Image, k0sVersion)
}

// ClusterStatus defines the observed state of K0smotronCluster
type ClusterStatus struct {
	ReconciliationStatus string `json:"reconciliationStatus"`
	Ready                bool   `json:"ready,omitempty"`
	Replicas             int32  `json:"replicas,omitempty"`
	// selector is the label selector for pods that should match the replicas count.
	Selector string `json:"selector,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
//+kubebuilder:resource:shortName=kmc
// +kubebuilder:deprecatedversion

// Cluster is the Schema for the k0smotronclusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:default={service:{type:NodePort}}
	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterList contains a list of K0smotronCluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
