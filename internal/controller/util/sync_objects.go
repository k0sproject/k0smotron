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

	err := syncObjects(ctx, src, dst, kmc, externalOwner, "secret", refs.secrets,
		func() *corev1.Secret { return &corev1.Secret{} },
		func(dstSecret, srcSecret *corev1.Secret, creating bool) {
			if creating {
				// The Secret type is immutable, so it is only set on creation.
				dstSecret.Type = srcSecret.Type
			}
			dstSecret.Data = maps.Clone(srcSecret.Data)
		})
	if err != nil {
		return err
	}

	return syncObjects(ctx, src, dst, kmc, externalOwner, "configmap", refs.configMaps,
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(dstConfigMap, srcConfigMap *corev1.ConfigMap, _ bool) {
			dstConfigMap.Data = maps.Clone(srcConfigMap.Data)
			dstConfigMap.BinaryData = maps.Clone(srcConfigMap.BinaryData)
		})
}

// A missing optional object is skipped, any other failure is returned. copyContent copies the
// kind-specific content of the source object to the destination one; creating is true when the
// destination object does not exist yet.
func syncObjects[T client.Object](
	ctx context.Context,
	src, dst client.Client,
	kmc *km.Cluster,
	externalOwner client.Object,
	kind string,
	refs map[string]bool,
	newObject func() T,
	copyContent func(dstObj, srcObj T, creating bool),
) error {
	for _, name := range slices.Sorted(maps.Keys(refs)) {
		srcObj := newObject()
		if err := src.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: name}, srcObj); err != nil {
			if apierrors.IsNotFound(err) && refs[name] {
				continue
			}
			return fmt.Errorf("failed to get %s %s/%s from the management cluster: %w", kind, kmc.Namespace, name, err)
		}

		if err := syncObject(ctx, dst, srcObj, newObject, copyContent, kmc, externalOwner); err != nil {
			return fmt.Errorf("failed to sync %s %s/%s to the remote host cluster: %w", kind, kmc.Namespace, name, err)
		}
	}

	return nil
}

func syncObject[T client.Object](
	ctx context.Context,
	dst client.Client,
	srcObj T,
	newObject func() T,
	copyContent func(dstObj, srcObj T, creating bool),
	kmc *km.Cluster,
	externalOwner client.Object,
) error {
	dstObj := newObject()
	err := dst.Get(ctx, client.ObjectKeyFromObject(srcObj), dstObj)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	exists := err == nil

	if !exists {
		dstObj.SetName(srcObj.GetName())
		dstObj.SetNamespace(srcObj.GetNamespace())
	}
	copyContent(dstObj, srcObj, !exists)

	// The management cluster is the source of truth for metadata, so whatever is on an existing copy is replaced.
	dstObj.SetLabels(maps.Clone(srcObj.GetLabels()))
	dstObj.SetAnnotations(maps.Clone(srcObj.GetAnnotations()))
	// dst.Scheme() must have corev1 registered, which is the case for all manager-managed clients.
	if err := SetExternalOwnerReference(kmc, dstObj, dst.Scheme(), externalOwner); err != nil {
		return err
	}

	if !exists {
		return dst.Create(ctx, dstObj)
	}
	return dst.Update(ctx, dstObj)
}
