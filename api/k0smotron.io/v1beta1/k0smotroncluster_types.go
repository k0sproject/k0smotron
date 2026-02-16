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
	"crypto/md5"
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
	// CustomizeComponents defines patches to apply to generated resources (StatefulSet, Service, ConfigMap, etc.).
	// Patches are applied after generation and before apply. Target resources are matched by Kind and app.kubernetes.io/component label.
	// For the full list of generated resources and their component labels, see https://docs.k0smotron.io/stable/generated-resources/.
	// +kubebuilder:validation:Optional
	CustomizeComponents CustomizeComponents `json:"customizeComponents,omitempty"`
}

// CustomizeComponents defines the customization of generated resources.
type CustomizeComponents struct {
	// Patches is a list of patches to apply to generated resources. Patches are applied in order.
	// +kubebuilder:validation:Optional
	Patches []ComponentPatch `json:"patches,omitempty"`
}

// ComponentPatch defines a patch to apply to a generated resource.
type ComponentPatch struct {
	// ResourceType is the Kubernetes Kind of the target resource (e.g. "StatefulSet", "Service", "ConfigMap").
	// +kubebuilder:validation:Required
	ResourceType string `json:"resourceType"`
	// Component is the value of the app.kubernetes.io/component label on the target resource.
	// +kubebuilder:validation:Required
	Component string `json:"component"`
	// Type is the patch type to apply:
	//   - "json": RFC 6902 JSON Patch, an array of add/remove/replace operations (https://datatracker.ietf.org/doc/html/rfc6902).
	//   - "merge": RFC 7386 JSON Merge Patch, a partial JSON object that is merged into the target (https://datatracker.ietf.org/doc/html/rfc7386).
	//   - "strategic": Kubernetes Strategic Merge Patch, like merge but with array merge semantics based on patchStrategy tags
	//     (https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/#use-a-strategic-merge-patch-to-update-a-deployment).
	// +kubebuilder:validation:Enum=json;strategic;merge
	// +kubebuilder:validation:Required
	Type PatchType `json:"type"`
	// Patch is the patch content. The format depends on the Type field:
	//   - For "json": a JSON array of operations, e.g. [{"op":"add","path":"/metadata/labels/foo","value":"bar"}].
	//   - For "merge" and "strategic": a partial YAML/JSON object that is merged into the target resource.
	// +kubebuilder:validation:Required
	Patch string `json:"patch"`
}

// PatchType defines the type of patch to apply.
// +kubebuilder:validation:Enum=json;strategic;merge
type PatchType string

const (
	// JSONPatchType is RFC 6902 JSON Patch (array of operations).
	// See https://datatracker.ietf.org/doc/html/rfc6902 for more details.
	JSONPatchType PatchType = "json"
	// StrategicMergePatchType is Kubernetes strategic merge patch.
	// See https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/#use-a-strategic-merge-patch-to-update-a-deployment for more details.
	StrategicMergePatchType PatchType = "strategic"
	// MergePatchType is RFC 7386 JSON Merge Patch.
	// See https://datatracker.ietf.org/doc/html/rfc7386 for more details.
	MergePatchType PatchType = "merge"
)

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
	Spec   ClusterSpec      `json:"spec,omitempty"`
	Status v2.ClusterStatus `json:"status,omitempty"`
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

// GetStatefulSetName returns the name of the statefulset for the k0s control plane.
// The name is generated based on the cluster name and is shortened if it exceeds the
// Kubernetes name length limit.
func GetStatefulSetName(clusterName string) string {
	return shortName(fmt.Sprintf("kmc-%s", clusterName))
}

// GetStatefulSetName returns the name of the statefulset for the k0s control plane.
func (kmc *Cluster) GetStatefulSetName() string {
	return GetStatefulSetName(kmc.Name)
}

// GetEtcdStatefulSetName returns the name of the statefulset for the etcd cluster.
func (kmc *Cluster) GetEtcdStatefulSetName() string {
	return kmc.getObjectName("kmc-%s-etcd")
}

// GetEtcdDefragJobName returns the name of the etcd defragmentation job.
func (kmc *Cluster) GetEtcdDefragJobName() string {
	return kmc.getObjectName("kmc-%s-defrag")
}

// GetAdminConfigSecretName returns the name of the secret containing the admin kubeconfig
// for the workload cluster.
func (kmc *Cluster) GetAdminConfigSecretName() string {
	// This is the form CAPI expects the secret to be named, don't try to shorten it
	return fmt.Sprintf("%s-kubeconfig", kmc.Name)
}

// GetEntrypointConfigMapName returns the name of the configmap containing the k0s entrypoint script.
func (kmc *Cluster) GetEntrypointConfigMapName() string {
	return kmc.getObjectName("kmc-entrypoint-%s-config")
}

// GetMonitoringConfigMapName returns the name of the configmap containing the prometheus
// configuration for monitoring the cluster.
func (kmc *Cluster) GetMonitoringConfigMapName() string {
	return kmc.getObjectName("kmc-prometheus-%s-config")
}

// GetMonitoringNginxConfigMapName returns the name of the configmap containing the nginx
// configuration for the prometheus sidecar.
func (kmc *Cluster) GetMonitoringNginxConfigMapName() string {
	return kmc.getObjectName("kmc-prometheus-%s-config-nginx")
}

// GetConfigMapName returns the name of the configmap containing the k0s configuration for the cluster.
func (kmc *Cluster) GetConfigMapName() string {
	return kmc.getObjectName("kmc-%s-config")
}

// GetServiceName returns the name of the service for the k0s control plane based on the
// service type specified in the ClusterSpec.
func (kmc *Cluster) GetServiceName() string {
	switch kmc.Spec.Service.Type {
	case v1.ServiceTypeNodePort:
		return kmc.GetNodePortServiceName()
	case v1.ServiceTypeLoadBalancer:
		return kmc.GetLoadBalancerServiceName()
	case v1.ServiceTypeClusterIP:
		return kmc.GetClusterIPServiceName()
	default:
		// The list of service types is limited and defined as enum in the CRD, so the default case should never be reached
		panic("unknown service type")
	}
}

// GetClusterIPServiceName returns the name of the service for the k0s control plane
// when service type is ClusterIP.
func (kmc *Cluster) GetClusterIPServiceName() string {
	return kmc.getObjectName("kmc-%s")
}

// GetEtcdServiceName returns the name of the service for the etcd cluster.
func (kmc *Cluster) GetEtcdServiceName() string {
	return kmc.getObjectName("kmc-%s-etcd")
}

// GetLoadBalancerServiceName returns the name of the service for the k0s control plane
// when service type is LoadBalancer.
func (kmc *Cluster) GetLoadBalancerServiceName() string {
	return kmc.getObjectName("kmc-%s-lb")
}

// GetNodePortServiceName returns the name of the service for the k0s control plane
// when service type is NodePort.
func (kmc *Cluster) GetNodePortServiceName() string {
	return kmc.getObjectName("kmc-%s-nodeport")
}

// GetVolumeName returns the name of the volume for the k0s control plane.
func (kmc *Cluster) GetVolumeName() string {
	return kmc.getObjectName("kmc-%s")
}

// GetIngressName returns the name of the ingress resource
func (kmc *Cluster) GetIngressName() string {
	return kmc.getObjectName("kmc-%s")
}

// GetIngressManifestsConfigMapName returns the name of the configmap containing the manifests needed for the ingress
func (kmc *Cluster) GetIngressManifestsConfigMapName() string {
	return kmc.getObjectName("kmc-%s-ingress")
}

// GetEndpointConfigMapName returns the name of the configmap containing the API server endpoint manifest
func (kmc *Cluster) GetEndpointConfigMapName() string {
	return kmc.getObjectName("kmc-%s-endpoint")
}

const kubeNameLengthLimit = 63

func (kmc *Cluster) getObjectName(pattern string) string {
	return shortName(fmt.Sprintf(pattern, kmc.Name))
}

func shortName(name string) string {
	if len(name) > kubeNameLengthLimit {
		return fmt.Sprintf("%s-%s", name[:kubeNameLengthLimit-6], fmt.Sprintf("%x", md5.Sum([]byte(name)))[:5])
	}
	return name
}
