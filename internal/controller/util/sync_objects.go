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

package util

import (
	"context"
	"fmt"
	"maps"
	"slices"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// referencedObjects holds the names of the objects referenced by a Cluster, mapped to whether
// every reference to them is optional.
type referencedObjects struct {
	secrets    map[string]bool
	configMaps map[string]bool
}

func (r *referencedObjects) addSecret(name string, optional bool) {
	addReference(r.secrets, name, optional)
}

func (r *referencedObjects) addConfigMap(name string, optional bool) {
	addReference(r.configMaps, name, optional)
}

// addReference records a reference. An object is optional only if every reference to it is.
func addReference(refs map[string]bool, name string, optional bool) {
	if prev, ok := refs[name]; ok {
		optional = optional && prev
	}
	refs[name] = optional
}

func (r *referencedObjects) addVolumeSource(vs corev1.VolumeSource) {
	if vs.Secret != nil && vs.Secret.SecretName != "" {
		r.addSecret(vs.Secret.SecretName, vs.Secret.Optional != nil && *vs.Secret.Optional)
	}
	if vs.ConfigMap != nil && vs.ConfigMap.Name != "" {
		r.addConfigMap(vs.ConfigMap.Name, vs.ConfigMap.Optional != nil && *vs.ConfigMap.Optional)
	}
}

func collectReferencedObjects(kmc *km.Cluster) *referencedObjects {
	refs := &referencedObjects{secrets: map[string]bool{}, configMaps: map[string]bool{}}
	for _, mount := range kmc.Spec.Mounts {
		refs.addVolumeSource(mount.VolumeSource)
	}
	for _, manifest := range kmc.Spec.Manifests {
		refs.addVolumeSource(manifest.VolumeSource)
	}
	if name := kmc.Spec.Storage.Kine.DataSourceSecretName; name != "" {
		refs.addSecret(name, false)
	}
	for _, certRef := range kmc.Spec.CertificateRefs {
		if certRef.Name != "" {
			refs.addSecret(certRef.Name, false)
		}
	}
	return refs
}

// SyncReferencedObjects mirrors the Secrets and ConfigMaps referenced by kmc (mounts, manifests, certificate
// references and kine datasource) from src to dst, creating or updating as needed. It does nothing unless
// spec.remoteHostCluster.syncReferencedObjects is enabled. Synced objects are owned by externalOwner
// for GC on deletion.
func SyncReferencedObjects(ctx context.Context, src, dst client.Client, kmc *km.Cluster, externalOwner client.Object) error {
	if kmc.Spec.RemoteHostCluster == nil || !kmc.Spec.RemoteHostCluster.SyncReferencedObjects {
		return nil
	}

	refs := collectReferencedObjects(kmc)

	for _, name := range slices.Sorted(maps.Keys(refs.secrets)) {
		srcSecret := &corev1.Secret{}
		found, err := getReferenced(ctx, src, kmc.Namespace, name, refs.secrets[name], srcSecret)
		if err != nil {
			return fmt.Errorf("failed to get secret %s/%s from the management cluster: %w", kmc.Namespace, name, err)
		}
		if !found {
			continue
		}
		if err := syncSecret(ctx, dst, srcSecret, kmc, externalOwner); err != nil {
			return fmt.Errorf("failed to sync secret %s/%s to the remote host cluster: %w", kmc.Namespace, name, err)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(refs.configMaps)) {
		srcConfigMap := &corev1.ConfigMap{}
		found, err := getReferenced(ctx, src, kmc.Namespace, name, refs.configMaps[name], srcConfigMap)
		if err != nil {
			return fmt.Errorf("failed to get configmap %s/%s from the management cluster: %w", kmc.Namespace, name, err)
		}
		if !found {
			continue
		}
		if err := syncConfigMap(ctx, dst, srcConfigMap, kmc, externalOwner); err != nil {
			return fmt.Errorf("failed to sync configmap %s/%s to the remote host cluster: %w", kmc.Namespace, name, err)
		}
	}

	return nil
}

// getReferenced gets the object from src. A missing optional object is not an error, it is reported as not found.
func getReferenced(ctx context.Context, src client.Client, namespace, name string, optional bool, obj client.Object) (found bool, err error) {
	if err := src.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) && optional {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// syncMetadata makes dst carry the labels and annotations of src, which is the source of truth, and
// the external owner reference.
func syncMetadata(dst, src client.Object, kmc *km.Cluster, externalOwner client.Object, dstClient client.Client) error {
	dst.SetLabels(maps.Clone(src.GetLabels()))
	dst.SetAnnotations(maps.Clone(src.GetAnnotations()))
	// dstClient.Scheme() must have corev1 registered, which is the case for all manager-managed clients.
	return SetExternalOwnerReference(kmc, dst, dstClient.Scheme(), externalOwner)
}

func syncSecret(ctx context.Context, dst client.Client, srcSecret *corev1.Secret, kmc *km.Cluster, externalOwner client.Object) error {
	dstSecret := &corev1.Secret{}
	err := dst.Get(ctx, client.ObjectKeyFromObject(srcSecret), dstSecret)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	exists := err == nil

	if !exists {
		dstSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: srcSecret.Name, Namespace: srcSecret.Namespace},
			// The Secret type is immutable, so it is only set on creation.
			Type: srcSecret.Type,
		}
	}
	dstSecret.Data = maps.Clone(srcSecret.Data)
	if err := syncMetadata(dstSecret, srcSecret, kmc, externalOwner, dst); err != nil {
		return err
	}

	if !exists {
		return dst.Create(ctx, dstSecret)
	}
	return dst.Update(ctx, dstSecret)
}

func syncConfigMap(ctx context.Context, dst client.Client, srcConfigMap *corev1.ConfigMap, kmc *km.Cluster, externalOwner client.Object) error {
	dstConfigMap := &corev1.ConfigMap{}
	err := dst.Get(ctx, client.ObjectKeyFromObject(srcConfigMap), dstConfigMap)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	exists := err == nil

	if !exists {
		dstConfigMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: srcConfigMap.Name, Namespace: srcConfigMap.Namespace},
		}
	}
	dstConfigMap.Data = maps.Clone(srcConfigMap.Data)
	dstConfigMap.BinaryData = maps.Clone(srcConfigMap.BinaryData)
	if err := syncMetadata(dstConfigMap, srcConfigMap, kmc, externalOwner, dst); err != nil {
		return err
	}

	if !exists {
		return dst.Create(ctx, dstConfigMap)
	}
	return dst.Update(ctx, dstConfigMap)
}
