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
	dst.Status = kmc.Status
	dst.Spec = v2.ClusterSpec{
		KubeconfigRef:             kmc.Spec.KubeconfigRef,
		Replicas:                  kmc.Spec.Replicas,
		Image:                     kmc.Spec.Image,
		ServiceAccount:            kmc.Spec.ServiceAccount,
		Version:                   kmc.Spec.Version,
		ExternalAddress:           kmc.Spec.ExternalAddress,
		Ingress:                   kmc.Spec.Ingress,
		Service:                   kmc.Spec.Service,
		Persistence:               kmc.Spec.Persistence,
		Storage:                   convertStorageV1toV2(kmc.Spec),
		K0sConfig:                 kmc.Spec.K0sConfig,
		CertificateRefs:           kmc.Spec.CertificateRefs,
		Manifests:                 kmc.Spec.Manifests,
		Mounts:                    kmc.Spec.Mounts,
		ControlPlaneFlags:         kmc.Spec.ControlPlaneFlags,
		Monitoring:                kmc.Spec.Monitoring,
		TopologySpreadConstraints: kmc.Spec.TopologySpreadConstraints,
		Resources:                 kmc.Spec.Resources,
		KubeconfigSecretMetadata:  kmc.Spec.KubeconfigSecretMetadata,
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this Cluster (v1beta1).
// NATS storage type has no equivalent in v1beta1 and is silently dropped.
func (kmc *Cluster) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*v2.Cluster)
	if !ok {
		return fmt.Errorf("expected *v2.Cluster, got %T", srcRaw)
	}

	kmc.ObjectMeta = src.ObjectMeta
	kmc.Status = src.Status
	kmc.Spec = ClusterSpec{
		KubeconfigRef:             src.Spec.KubeconfigRef,
		Replicas:                  src.Spec.Replicas,
		Image:                     src.Spec.Image,
		ServiceAccount:            src.Spec.ServiceAccount,
		Version:                   src.Spec.Version,
		ExternalAddress:           src.Spec.ExternalAddress,
		Ingress:                   src.Spec.Ingress,
		Service:                   src.Spec.Service,
		Persistence:               src.Spec.Persistence,
		Etcd:                      src.Spec.Storage.Etcd,
		K0sConfig:                 src.Spec.K0sConfig,
		CertificateRefs:           src.Spec.CertificateRefs,
		Manifests:                 src.Spec.Manifests,
		Mounts:                    src.Spec.Mounts,
		ControlPlaneFlags:         src.Spec.ControlPlaneFlags,
		Monitoring:                src.Spec.Monitoring,
		TopologySpreadConstraints: src.Spec.TopologySpreadConstraints,
		Resources:                 src.Spec.Resources,
		KubeconfigSecretMetadata:  src.Spec.KubeconfigSecretMetadata,
	}
	if src.Spec.Storage.Type == v2.StorageTypeKine {
		kmc.Spec.KineDataSourceURL = src.Spec.Storage.Kine.DataSourceURL
		kmc.Spec.KineDataSourceSecretName = src.Spec.Storage.Kine.DataSourceSecretName
	}
	return nil
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
