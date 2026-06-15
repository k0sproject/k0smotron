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

package v1beta1

import (
	"encoding/json"
	"fmt"

	v2 "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// Annotation keys used to persist v1beta1 Service fields that have no equivalent in v1beta2.
const (
	ServiceAnnotationLabels                = "k0smotron.io/conversion-dropped-service.labels"
	ServiceAnnotationAnnotations           = "k0smotron.io/conversion-dropped-service.annotations"
	ServiceAnnotationExternalTrafficPolicy = "k0smotron.io/conversion-dropped-service.externalTrafficPolicy"
	ServiceAnnotationLoadBalancerClass     = "k0smotron.io/conversion-dropped-service.loadBalancerClass"
)

var _ conversion.Convertible = &Cluster{}

// ConvertTo converts this Cluster (v1beta1) to the hub version (v1beta2).
func (kmc *Cluster) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*v2.Cluster)
	if !ok {
		return fmt.Errorf("expected *v2.Cluster, got %T", dstRaw)
	}

	dst.ObjectMeta = kmc.ObjectMeta
	v1beta2Spec, nonSupportedFields := ClusterSpecToV2(kmc.Spec)
	dst.Spec = v1beta2Spec

	if len(nonSupportedFields) > 0 {
		if dst.Annotations == nil {
			dst.Annotations = make(map[string]string)
		}
		for key, value := range nonSupportedFields {
			encoded, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("error encoding annotation %s: %w", key, err)
			}
			dst.Annotations[key] = string(encoded)
		}
	}

	dst.SetReconciliationStatus(kmc.Status.ReconciliationStatus)
	dst.SetReadyStatus(kmc.Status.Ready)

	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this Cluster (v1beta1).
// NATS storage type and Patches have no equivalent in v1beta1 and are silently dropped.
func (kmc *Cluster) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*v2.Cluster)
	if !ok {
		return fmt.Errorf("expected *v2.Cluster, got %T", srcRaw)
	}

	kmc.ObjectMeta = src.ObjectMeta

	spec, err := ClusterSpecFromV2(src.Spec, src.Annotations)
	if err != nil {
		return fmt.Errorf("error converting ClusterSpec from v1beta2 to v1beta1: %w", err)
	}
	kmc.Spec = spec

	kmc.Status.ReconciliationStatus = src.GetReconciliationStatus()
	kmc.Status.Ready = src.GetReadyStatus()

	return nil
}

// ClusterSpecToV2 converts a v1beta1 ClusterSpec to a v1beta2 ClusterSpec.
func ClusterSpecToV2(spec ClusterSpec) (v2.ClusterSpec, map[string]any) {
	v1beta2Spec := v2.ClusterSpec{
		Replicas:                  spec.Replicas,
		Image:                     spec.Image,
		ServiceAccount:            spec.ServiceAccount,
		Version:                   spec.Version,
		ExternalAddress:           spec.ExternalAddress,
		Ingress:                   spec.Ingress,
		Persistence:               spec.Persistence,
		Storage:                   convertStorageV1toV2(spec),
		K0sConfig:                 spec.K0sConfig,
		CertificateRefs:           spec.CertificateRefs,
		Manifests:                 spec.Manifests,
		Mounts:                    spec.Mounts,
		ControlPlaneFlags:         spec.ControlPlaneFlags,
		Monitoring:                spec.Monitoring,
		TopologySpreadConstraints: spec.TopologySpreadConstraints,
		Resources:                 spec.Resources,
		KubeconfigSecretMetadata:  spec.KubeconfigSecretMetadata,
	}

	svcSpec := v2.ServiceSpec{
		Type:             spec.Service.Type,
		APIPort:          spec.Service.APIPort,
		KonnectivityPort: spec.Service.KonnectivityPort,
	}
	v1beta2Spec.Service = svcSpec

	nonSupportedFields := map[string]any{}
	if spec.Service.Labels != nil {
		nonSupportedFields[ServiceAnnotationLabels] = spec.Service.Labels
	}
	if spec.Service.Annotations != nil {
		nonSupportedFields[ServiceAnnotationAnnotations] = spec.Service.Annotations
	}
	if spec.Service.ExternalTrafficPolicy != "" {
		nonSupportedFields[ServiceAnnotationExternalTrafficPolicy] = spec.Service.ExternalTrafficPolicy
	}
	if spec.Service.LoadBalancerClass != nil {
		nonSupportedFields[ServiceAnnotationLoadBalancerClass] = spec.Service.LoadBalancerClass
	}

	if spec.KubeconfigRef != nil {
		v1beta2Spec.RemoteHostCluster = &v2.RemoteHostClusterSpec{
			KubeconfigRef: spec.KubeconfigRef,
		}
	}

	return v1beta2Spec, nonSupportedFields

}

// ClusterSpecFromV2 converts a v1beta2 ClusterSpec to a v1beta1 ClusterSpec.
// NATS storage type and Patches have no equivalent in v1beta1 and are silently dropped.
func ClusterSpecFromV2(src v2.ClusterSpec, srcAnnotations map[string]string) (ClusterSpec, error) {
	spec := ClusterSpec{
		Replicas:                  src.Replicas,
		Image:                     src.Image,
		ServiceAccount:            src.ServiceAccount,
		Version:                   src.Version,
		ExternalAddress:           src.ExternalAddress,
		Ingress:                   src.Ingress,
		Persistence:               src.Persistence,
		Etcd:                      src.Storage.Etcd,
		K0sConfig:                 src.K0sConfig,
		CertificateRefs:           src.CertificateRefs,
		Manifests:                 src.Manifests,
		Mounts:                    src.Mounts,
		ControlPlaneFlags:         src.ControlPlaneFlags,
		Monitoring:                src.Monitoring,
		TopologySpreadConstraints: src.TopologySpreadConstraints,
		Resources:                 src.Resources,
		KubeconfigSecretMetadata:  src.KubeconfigSecretMetadata,
	}
	if src.Storage.Type == v2.StorageTypeKine {
		spec.KineDataSourceURL = src.Storage.Kine.DataSourceURL
		spec.KineDataSourceSecretName = src.Storage.Kine.DataSourceSecretName
	}
	if src.RemoteHostCluster != nil && src.RemoteHostCluster.KubeconfigRef != nil {
		spec.KubeconfigRef = src.RemoteHostCluster.KubeconfigRef
	}

	spec.Service = ServiceSpec{
		Type:             src.Service.Type,
		APIPort:          src.Service.APIPort,
		KonnectivityPort: src.Service.KonnectivityPort,
	}
	if len(srcAnnotations) > 0 {
		spec.Service.Labels = GetServiceLabels(srcAnnotations)
		spec.Service.Annotations = GetServiceAnnotations(srcAnnotations)
		spec.Service.ExternalTrafficPolicy = GetServiceExternalTrafficPolicy(srcAnnotations)
		spec.Service.LoadBalancerClass = GetServiceLoadBalancerClass(srcAnnotations)
	}
	return spec, nil
}

func convertStorageV1toV2(spec ClusterSpec) v2.StorageSpec {
	storage := v2.StorageSpec{
		Etcd: spec.Etcd,
	}
	if spec.KineDataSourceURL != "" || spec.KineDataSourceSecretName != "" {
		storage.Type = v2.StorageTypeKine
		storage.Kine = v2.KineSpec{
			DataSourceURL:        spec.KineDataSourceURL,
			DataSourceSecretName: spec.KineDataSourceSecretName,
		}
	} else {
		storage.Type = v2.StorageTypeEtcd
	}
	return storage
}
