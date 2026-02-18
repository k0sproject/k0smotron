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
	"fmt"

	v2 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &Cluster{}

// ConvertTo converts this Cluster (v1beta1) to the hub version (v1beta2).
func (kmc *Cluster) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*v2.Cluster)
	if !ok {
		return fmt.Errorf("expected *v2.Cluster, got %T", dstRaw)
	}

	dst.ObjectMeta = kmc.ObjectMeta
	dst.Spec = ClusterSpecToV2(kmc.Spec)

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
	kmc.Spec = ClusterSpecFromV2(src.Spec)

	kmc.Status.ReconciliationStatus = src.GetReconciliationStatus()
	kmc.Status.Ready = src.GetReadyStatus()

	return nil
}

// ClusterSpecToV2 converts a v1beta1 ClusterSpec to a v1beta2 ClusterSpec.
func ClusterSpecToV2(spec ClusterSpec) v2.ClusterSpec {
	return v2.ClusterSpec{
		KubeconfigRef:             spec.KubeconfigRef,
		Replicas:                  spec.Replicas,
		Image:                     spec.Image,
		ServiceAccount:            spec.ServiceAccount,
		Version:                   spec.Version,
		ExternalAddress:           spec.ExternalAddress,
		Ingress:                   spec.Ingress,
		Service:                   spec.Service,
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
}

// ClusterSpecFromV2 converts a v1beta2 ClusterSpec to a v1beta1 ClusterSpec.
// NATS storage type has no equivalent in v1beta1 and is silently dropped.
func ClusterSpecFromV2(src v2.ClusterSpec) ClusterSpec {
	spec := ClusterSpec{
		KubeconfigRef:             src.KubeconfigRef,
		Replicas:                  src.Replicas,
		Image:                     src.Image,
		ServiceAccount:            src.ServiceAccount,
		Version:                   src.Version,
		ExternalAddress:           src.ExternalAddress,
		Ingress:                   src.Ingress,
		Service:                   src.Service,
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
	return spec
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
