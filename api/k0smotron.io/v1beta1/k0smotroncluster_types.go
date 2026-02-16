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

	"github.com/k0sproject/version"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ClusterSpec defines the desired state of K0smotronCluster
type ClusterSpec struct {
	// KubeconfigRef is the reference to the kubeconfig of the hosting cluster.
	// This kubeconfig will be used to deploy the k0s control plane.
	//+kubebuilder:validation:Optional
	KubeconfigRef *KubeconfigRef `json:"kubeconfigRef,omitempty"`
	// Replicas is the desired number of replicas of the k0s control planes.
	// If unspecified, defaults to 1. If the value is above 1, k0smotron requires kine datasource URL to be set.
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
	Ingress *IngressSpec `json:"ingress,omitempty"`
	// Service defines the service configuration.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default={"type":"ClusterIP","apiPort":30443,"konnectivityPort":30132}
	Service ServiceSpec `json:"service,omitempty"`
	// Persistence defines the persistence configuration. If empty k0smotron
	// will use emptyDir as a volume. See https://docs.k0smotron.io/stable/configuration/#persistence
	//+kubebuilder:validation:Optional
	Persistence PersistenceSpec `json:"persistence,omitempty"`
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
	CertificateRefs []CertificateRef `json:"certificateRefs,omitempty"`
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
	Mounts []Mount `json:"mounts,omitempty"`
	// ControlPlaneFlags allows to configure additional flags for k0s
	// control plane and to override existing ones. The default flags are
	// kept unless they are overriden explicitly. Flags with arguments must
	// be specified as a single string, e.g. --some-flag=argument
	//+kubebuilder:validation:Optional
	ControlPlaneFlags []string `json:"controllerPlaneFlags,omitempty"`
	// Monitoring defines the monitoring configuration.
	//+kubebuilder:validation:Optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`
	// Etcd defines the etcd configuration.
	//+kubebuilder:default={"image":"quay.io/k0sproject/etcd:v3.5.13","persistence":{}}
	Etcd EtcdSpec `json:"etcd,omitempty"`

	// TopologySpreadConstraints will be passed directly to BOTH etcd and k0s pods.
	// See https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/ for more details.
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// Resources describes the compute resource requirements for the control plane pods.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// KubeconfigSecretMetadata specifies metadata (labels and annotations) to be propagated to the kubeconfig Secret
	// created for the workload cluster.
	// Note: This metadata will have precedence over default labels/annotations on the Secret.
	// +kubebuilder:validation:Optional
	KubeconfigSecretMetadata SecretMetadata `json:"kubeconfigSecretMetadata,omitempty,omitzero"`
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

// SecretMetadata defines metadata to be propagated to the bootstrap Secret
type SecretMetadata struct {
	// Labels to be added to the bootstrap Secret
	// +kubebuilder:validation:Optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations to be added to the bootstrap Secret
	// +kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// IngressSpec defines the ingress configuration for accessing the Kubernetes API and konnectivity server via host names
type IngressSpec struct {
	// Deploy defines whether to deploy an ingress resource for the cluster or let the user do it manually.
	// Default: true
	Deploy *bool `json:"deploy,omitempty"`
	// Port defines the port used by the ingress controller
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=443
	Port int64 `json:"port,omitempty"`
	//+kubebuilder:validation:Required
	APIHost string `json:"apiHost,omitempty"`
	//+kubebuilder:validation:Required
	KonnectivityHost string `json:"konnectivityHost,omitempty"`
	// ClassName defines the ingress class name to be used by the ingress controller.
	//+kubebuilder:validation:Optional
	ClassName *string `json:"className,omitempty"`
	// Annotations defines extra annotations to be added to the ingress controller service.
	//+kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

var ingressCompatibleVersions = []*version.Version{
	version.MustParse("v1.34.1+k0s.0"),
}

// Validate checks if the ingress controller is compatible with the given k0s version
func (i *IngressSpec) Validate(clusterVersion string) (admission.Warnings, error) {
	warnings := admission.Warnings{}
	v, err := version.NewVersion(clusterVersion)
	if err != nil {
		return warnings, fmt.Errorf("failed to parse k0s version %s: %w", clusterVersion, err)
	}

	for _, iv := range ingressCompatibleVersions {
		if v.Segments()[1] == iv.Segments()[1] {
			if v.Core().LessThan(iv.Core()) {
				return warnings, fmt.Errorf("ingress controller is not supported with k0s version %s, minimum supported version for ingress is %s", clusterVersion, iv.String())
			}
		}
	}

	if i.Deploy != nil && *i.Deploy && len(i.Annotations) == 0 {
		warnings = append(warnings, "no annotations specified for the ingress controller, make sure that ingress controller supports tls passthrough")
	}

	return warnings, nil
}

type Mount struct {
	Path string `json:"path"`
	// ReadOnly specifies whether the volume should be mounted as read-only. (default: false, except for ConfigMaps and Secrets)
	//+kubebuilder:validation:Optional
	ReadOnly        bool `json:"readOnly,omitempty"`
	v1.VolumeSource `json:",inline"`
}

const (
	defaultK0SImage   = "quay.io/k0sproject/k0s"
	DefaultK0SVersion = "v1.27.9-k0s.0"
	DefaultK0SSuffix  = "k0s.0"
	DefaultEtcdImage  = "quay.io/k0sproject/etcd:v3.5.13"
)

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

// Cluster is the Schema for the k0smotronclusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	//+kubebuilder:validation:Optional
	//+kubebuilder:default={service:{type:NodePort}}
	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

type ServiceSpec struct {
	//+kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	//+kubebuilder:default=ClusterIP
	Type v1.ServiceType `json:"type"`
	// APIPort defines the kubernetes API port. If empty k0smotron
	// will pick it automatically.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=30443
	APIPort int `json:"apiPort,omitempty"`
	// KonnectivityPort defines the konnectivity port. If empty k0smotron
	// will pick it automatically.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=30132
	KonnectivityPort int `json:"konnectivityPort,omitempty"`

	// Annotations defines extra annotations to be added to the service.
	//+kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Labels defines extra labels to be added to the service.
	//+kubebuilder:validation:Optional
	Labels map[string]string `json:"labels,omitempty"`
	// LoadBalancerClass defines the load balancer class to be used for the service. Used only when service type is LoadBalancer.
	//+kubebuilder:validation:Optional
	LoadBalancerClass *string `json:"loadBalancerClass,omitempty"`
	// ExternalTrafficPolicy defines the external traffic policy for the service. Used only when service type is NodePort or LoadBalancer.
	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Enum=Cluster;Local
	ExternalTrafficPolicy v1.ServiceExternalTrafficPolicyType `json:"externalTrafficPolicy,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterList contains a list of K0smotronCluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

type PersistenceSpec struct {
	//+kubebuilder:validation:Enum:emptyDir;hostPath;pvc
	//+kubebuilder:default=emptyDir
	Type string `json:"type"`
	// PersistentVolumeClaim defines the PVC configuration. Will be used as is in case of .spec.persistence.type is pvc.
	//+kubebuilder:validation:Optional
	PersistentVolumeClaim *PersistentVolumeClaim `json:"persistentVolumeClaim,omitempty"`
	// AutoDeletePVCs defines whether the PVC should be deleted when the cluster is deleted.
	//+kubebuilder:default=false
	//+kubebuilder:validation:Optional
	AutoDeletePVCs bool `json:"autoDeletePVCs,omitempty"`
	// HostPath defines the host path configuration. Will be used as is in case of .spec.persistence.type is hostPath.
	//+kubebuilder:validation:Optional
	HostPath string `json:"hostPath,omitempty"`
}

// PersistentVolumeClaim is a user's request for and claim to a persistent volume
type PersistentVolumeClaim struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// spec defines the desired characteristics of a volume requested by a pod author.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Spec v1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// status represents the current information/status of a persistent volume claim.
	// Read-only.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Status v1.PersistentVolumeClaimStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type ObjectMeta struct {
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// +optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,3,opt,name=namespace"`

	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`

	// +optional
	// +patchStrategy=merge
	Finalizers []string `json:"finalizers,omitempty" patchStrategy:"merge" protobuf:"bytes,14,rep,name=finalizers"`
}

type MonitoringSpec struct {
	// Enabled enables prometheus sidecar that scrapes metrics from the child cluster system components and expose
	// them as usual kubernetes pod metrics.
	Enabled bool `json:"enabled"`
	// PrometheusImage defines the image used for the prometheus sidecar.
	//+kubebuilder:default="quay.io/k0sproject/prometheus:v2.44.0"
	PrometheusImage string `json:"prometheusImage"`
	// ProxyImage defines the image used for the nginx proxy sidecar.
	//+kubebuilder:default="nginx:1.19.10"
	ProxyImage string `json:"proxyImage"`
}

type EtcdSpec struct {
	// Image defines the etcd image to be deployed.
	//+kubebuilder:default="quay.io/k0sproject/etcd:v3.5.13"
	Image string `json:"image"`
	// Args defines the etcd arguments.
	//+kubebuilder:validation:Optional
	Args []string `json:"args,omitempty"`
	// Persistence defines the persistence configuration.
	//+kubebuilder:validation:Optional
	Persistence EtcdPersistenceSpec `json:"persistence"`
	// AutoDeletePVCs defines whether the PVC should be deleted when the etcd cluster is deleted.
	//+kubebuilder:default=false
	//+kubebuilder:validation:Optional
	AutoDeletePVCs bool `json:"autoDeletePVCs,omitempty"`
	// DefragJob defines the etcd defragmentation job configuration.
	//+kubebuilder:validation:Optional
	DefragJob DefragJob `json:"defragJob"`
	// Resources defines the compute resource requirements for the etcd container.
	//+kubebuilder:validation:Optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
}

type DefragJob struct {
	// Enabled enables the etcd defragmentation job.
	//+kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// Schedule defines the etcd defragmentation job schedule.
	//+kubebuilder:default="0 12 * * *"
	Schedule string `json:"schedule"`
	// Rule defines the etcd defragmentation job defrag-rule.
	// For more information check: https://github.com/ahrtr/etcd-defrag/tree/main?tab=readme-ov-file#defragmentation-rule
	//+kubebuilder:default="dbQuotaUsage > 0.8 || dbSize - dbSizeInUse > 200*1024*1024"
	Rule string `json:"rule"`
	// Image defines the etcd defragmentation job image.
	//+kubebuilder:default="ghcr.io/ahrtr/etcd-defrag:v0.16.0"
	Image string `json:"image"`
}

type EtcdPersistenceSpec struct {
	// StorageClass defines the storage class to be used for etcd persistence. If empty, will be used the default storage class.
	//+kubebuilder:validation:Optional
	StorageClass string `json:"storageClass"`
	// Size defines the size of the etcd volume. Default: 1Gi
	//+kubebuilder:default="1Gi"
	//+kubebuilder:validation:Optional
	Size resource.Quantity `json:"size"`
}

type CertificateRef struct {
	//+kubebuilder:validation:Enum=ca;sa;proxy;etcd;apiserver-etcd-client;etcd-peer;etcd-server
	Type string `json:"type"`
	//+kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`
}

// KubeconfigRef defines the reference to the kubeconfig of the hosting cluster.
type KubeconfigRef struct {
	// Name is the name of the secret containing the kubeconfig of the hosting cluster.
	//+kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace is the namespace of the secret containing the kubeconfig of the hosting cluster.
	//+kubebuilder:validation:Required
	Namespace string `json:"namespace,omitempty"`
	// Key is the key in the secret containing the kubeconfig of the hosting cluster.
	//+kubebuilder:validation:Optional
	//+kubebuilder:default="value"
	Key string `json:"key,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}

func GetStatefulSetName(clusterName string) string {
	return shortName(fmt.Sprintf("kmc-%s", clusterName))
}

func (kmc *Cluster) GetStatefulSetName() string {
	return GetStatefulSetName(kmc.Name)
}

func (kmc *Cluster) GetEtcdStatefulSetName() string {
	return kmc.getObjectName("kmc-%s-etcd")
}

func (kmc *Cluster) GetEtcdDefragJobName() string {
	return kmc.getObjectName("kmc-%s-defrag")
}

func (kmc *Cluster) GetAdminConfigSecretName() string {
	// This is the form CAPI expects the secret to be named, don't try to shorten it
	return fmt.Sprintf("%s-kubeconfig", kmc.Name)
}

func (kmc *Cluster) GetEntrypointConfigMapName() string {
	return kmc.getObjectName("kmc-entrypoint-%s-config")
}

func (kmc *Cluster) GetMonitoringConfigMapName() string {
	return kmc.getObjectName("kmc-prometheus-%s-config")
}

func (kmc *Cluster) GetMonitoringNginxConfigMapName() string {
	return kmc.getObjectName("kmc-prometheus-%s-config-nginx")
}

func (kmc *Cluster) GetConfigMapName() string {
	return kmc.getObjectName("kmc-%s-config")
}

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

func (kmc *Cluster) GetClusterIPServiceName() string {
	return kmc.getObjectName("kmc-%s")
}

func (kmc *Cluster) GetEtcdServiceName() string {
	return kmc.getObjectName("kmc-%s-etcd")
}

func (kmc *Cluster) GetLoadBalancerServiceName() string {
	return kmc.getObjectName("kmc-%s-lb")
}

func (kmc *Cluster) GetNodePortServiceName() string {
	return kmc.getObjectName("kmc-%s-nodeport")
}

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
